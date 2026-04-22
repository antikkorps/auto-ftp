package main

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	ftpserver "github.com/fclairamb/ftpserverlib"
	"github.com/spf13/afero"
	"github.com/wailsapp/wails/v2/pkg/options"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// State is the snapshot returned to the frontend for initial rendering.
type State struct {
	Version     string   `json:"version"`
	IPs         []string `json:"ips"`
	Port        int      `json:"port"`
	User        string   `json:"user"`
	Pass        string   `json:"pass"`
	Folder      string   `json:"folder"`
	Online      bool     `json:"online"`
	ErrorBadge  string   `json:"errorBadge"`
	ErrorDetail string   `json:"errorDetail"`
	LastFile    string   `json:"lastFile"`
	LastAtMs    int64    `json:"lastAtMs"`
}

// ActivityEvent is emitted when fsnotify sees a new/updated file.
type ActivityEvent struct {
	Name   string `json:"name"`
	AtMs   int64  `json:"atMs"`
	Folder string `json:"folder"`
}

// StatusEvent is emitted when the server state changes.
type StatusEvent struct {
	Online bool   `json:"online"`
	Badge  string `json:"badge"`
	Detail string `json:"detail"`
}

// App owns the FTP server lifecycle and bridges Go state to the Wails
// frontend via bound methods and runtime events.
type App struct {
	ctx     context.Context
	logger  *slog.Logger
	logPath string

	server        *ftpserver.FtpServer
	graphiquesDir string
	ips           []string
	activity      *activityTracker

	mu          sync.Mutex
	online      bool
	errorBadge  string
	errorDetail string

	cancel  context.CancelFunc
	stopped atomic.Bool
	hbFails atomic.Int32
}

// NewApp builds a minimal App. The actual FTP setup (destination folder,
// Listen(), goroutines) runs in OnStartup — this way the Wails
// SingleInstanceLock has already confirmed we are the sole instance
// before we touch port 2121.
func NewApp(logger *slog.Logger, logPath string) *App {
	return &App{
		logger:   logger,
		logPath:  logPath,
		activity: &activityTracker{},
	}
}

// OnStartup runs once Wails has confirmed single-instance and the
// frontend is ready. It resolves the destination folder, binds the FTP
// server, and starts watchers.
func (a *App) OnStartup(ctx context.Context) {
	a.ctx = ctx

	graphiquesDir, destErr := resolveDestination()
	a.graphiquesDir = graphiquesDir
	a.ips = localIPv4s()
	a.logger.Info("local ips detected", "ips", a.ips)

	ensureAutostart()

	if destErr != nil {
		a.logger.Error("destination folder unavailable", "path", graphiquesDir, "error", destErr)
		a.setErrorLocked("DOSSIER INTROUVABLE", destErr.Error())
		return
	}
	a.logger.Info("folder ready", "path", graphiquesDir)

	server, bindErr := serveWithFS(a.logger, graphiquesDir)
	a.server = server
	if bindErr != nil {
		a.logger.Error("ftp server bind failed", "error", bindErr)
		a.setErrorLocked(shortBindError(bindErr), bindErr.Error())
		return
	}
	a.logger.Info("ftp server listening", "addr", fmt.Sprintf("0.0.0.0:%d", ftpPort))

	a.mu.Lock()
	a.online = true
	a.mu.Unlock()

	bgCtx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel

	go func() {
		if err := a.server.Serve(); err != nil && !a.stopped.Load() {
			a.logger.Error("ftp server crashed", "error", err)
			a.setError("SERVEUR ARRÊTÉ", err.Error())
		}
	}()

	go watchFolder(bgCtx, graphiquesDir, a.logger, func(name string) {
		a.logger.Info("file activity", "name", name)
		a.activity.record(name)
		wailsruntime.EventsEmit(a.ctx, "activity", ActivityEvent{
			Name:   name,
			AtMs:   time.Now().UnixMilli(),
			Folder: graphiquesDir,
		})
	})

	go heartbeat(bgCtx, fmt.Sprintf("127.0.0.1:%d", ftpPort), a.logger,
		func(err error) {
			if a.stopped.Load() {
				return
			}
			if a.hbFails.Add(1) >= 2 {
				a.setError("SERVEUR NE RÉPOND PLUS", err.Error())
			}
		},
		func() {
			if a.hbFails.Swap(0) >= 2 {
				a.setOnline()
			}
		},
	)
}

// OnBeforeClose runs when the user closes the window. Stop the server
// cleanly so port 2121 and the UDF are released promptly.
func (a *App) OnBeforeClose(_ context.Context) bool {
	a.shutdown()
	return false
}

// OnSecondInstance is invoked (inside the ORIGINAL process) whenever
// another auto-ftp.exe is double-clicked. We simply bring the existing
// window back to the front.
func (a *App) OnSecondInstance(_ options.SecondInstanceData) {
	if a.ctx == nil {
		return
	}
	a.logger.Info("second instance attempted launch, focusing existing window")
	wailsruntime.WindowUnminimise(a.ctx)
	wailsruntime.WindowShow(a.ctx)
}

func (a *App) shutdown() {
	if !a.stopped.CompareAndSwap(false, true) {
		return
	}
	a.logger.Info("stopping ftp server")
	if a.cancel != nil {
		a.cancel()
	}
	if a.server != nil {
		_ = a.server.Stop()
	}
}

func (a *App) setErrorLocked(badge, detail string) {
	a.mu.Lock()
	a.online = false
	a.errorBadge = badge
	a.errorDetail = detail
	a.mu.Unlock()
}

func (a *App) setError(badge, detail string) {
	a.setErrorLocked(badge, detail)
	if a.ctx != nil {
		wailsruntime.EventsEmit(a.ctx, "status", StatusEvent{
			Online: false, Badge: badge, Detail: detail,
		})
	}
}

func (a *App) setOnline() {
	a.mu.Lock()
	a.online = true
	a.errorBadge = ""
	a.errorDetail = ""
	a.mu.Unlock()
	if a.ctx != nil {
		wailsruntime.EventsEmit(a.ctx, "status", StatusEvent{Online: true})
	}
}

// --- bound methods (callable from JS) ---

// GetState returns the current snapshot used by the frontend.
func (a *App) GetState() State {
	a.mu.Lock()
	online, badge, detail := a.online, a.errorBadge, a.errorDetail
	a.mu.Unlock()

	name, at := a.activity.snapshot()
	var atMs int64
	if !at.IsZero() {
		atMs = at.UnixMilli()
	}
	return State{
		Version:     version,
		IPs:         a.ips,
		Port:        ftpPort,
		User:        ftpUser,
		Pass:        ftpPass,
		Folder:      a.graphiquesDir,
		Online:      online,
		ErrorBadge:  badge,
		ErrorDetail: detail,
		LastFile:    name,
		LastAtMs:    atMs,
	}
}

// OpenFolder opens the reception folder in the OS file explorer.
func (a *App) OpenFolder() {
	openFolder(a.graphiquesDir)
}

// OpenLogs opens auto-ftp.log in the OS text editor.
func (a *App) OpenLogs() {
	openInNotepad(a.logPath)
}

// ChangeFolder shows a native directory picker and saves the chosen
// path to auto-ftp.cfg. Returns the chosen path (empty if cancelled).
func (a *App) ChangeFolder() (string, error) {
	if a.ctx == nil {
		return "", fmt.Errorf("app not started")
	}
	path, err := wailsruntime.OpenDirectoryDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title:            "Choisir le dossier de réception",
		DefaultDirectory: a.graphiquesDir,
	})
	if err != nil {
		return "", err
	}
	if path == "" {
		return "", nil
	}
	if err := saveConfiguredFolder(path); err != nil {
		return "", err
	}
	return path, nil
}

// QuitApp triggers a clean shutdown and closes the window.
func (a *App) QuitApp() {
	a.shutdown()
	if a.ctx != nil {
		wailsruntime.Quit(a.ctx)
	}
}

// serveWithFS wires an afero.BasePathFs to a fresh ftp driver and
// starts listening on the FTP port.
func serveWithFS(logger *slog.Logger, dir string) (*ftpserver.FtpServer, error) {
	rootFs := afero.NewBasePathFs(afero.NewOsFs(), dir)
	drv := &driver{rootFs: rootFs, logger: logger}
	server := ftpserver.NewFtpServer(drv)
	server.Logger = logger
	if err := server.Listen(); err != nil {
		return server, err
	}
	return server, nil
}
