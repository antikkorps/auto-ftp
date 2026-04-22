package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"image/color"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	ftpserver "github.com/fclairamb/ftpserverlib"
	"github.com/fsnotify/fsnotify"
	"github.com/spf13/afero"
	"gopkg.in/natefinch/lumberjack.v2"
)

// Overridable at build time via ldflags -X.
// See README / DEPLOYMENT for the full build command.
var (
	// CHANGE THESE CREDENTIALS BEFORE DEPLOYING OUTSIDE AN ISOLATED LAN.
	ftpUser = "vmsync"
	ftpPass = "vmsync"
	// Set by the build script from `git describe`; defaults to "dev" for
	// local non-tagged builds so support can tell them apart.
	version = "dev"
)

const (
	ftpPort    = 2121
	pasvStart  = 2122
	pasvEnd    = 2130
	folderName = "graphiques"
	logsDir    = "logs"
	logFile    = "auto-ftp.log"
	appName    = "Serveur FTP EasyVIEW"
	startupKey = "auto-ftp.vbs"
	configFile = "auto-ftp.cfg"
)

var (
	accentGreen    = color.NRGBA{R: 0x1e, G: 0x96, B: 0x3f, A: 0xff}
	accentGreenLt  = color.NRGBA{R: 0xe5, G: 0xf6, B: 0xea, A: 0xff}
	accentRed      = color.NRGBA{R: 0xc8, G: 0x2b, B: 0x3a, A: 0xff}
	accentRedLt    = color.NRGBA{R: 0xfb, G: 0xe9, B: 0xeb, A: 0xff}
	accentRedBord  = color.NRGBA{R: 0xc8, G: 0x2b, B: 0x3a, A: 0x33}
	accentGreenBrd = color.NRGBA{R: 0x1e, G: 0x96, B: 0x3f, A: 0x33}
	textDark       = color.NRGBA{R: 0x1c, G: 0x26, B: 0x35, A: 0xff}
	textMuted      = color.NRGBA{R: 0x64, G: 0x70, B: 0x82, A: 0xff}
	bgSoft         = color.NRGBA{R: 0xf6, G: 0xf8, B: 0xfb, A: 0xff}
	cardBg         = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	cardBorder     = color.NRGBA{R: 0xe3, G: 0xe8, B: 0xef, A: 0xff}
)

type driver struct {
	rootFs   afero.Fs
	logger   *slog.Logger
	settings *ftpserver.Settings
}

func buildSettings() *ftpserver.Settings {
	return &ftpserver.Settings{
		ListenAddr:               fmt.Sprintf("0.0.0.0:%d", ftpPort),
		Banner:                   "EasyVIEW FTP",
		PassiveTransferPortRange: ftpserver.PortRange{Start: pasvStart, End: pasvEnd},
		IdleTimeout:              900,
	}
}

func (d *driver) GetSettings() (*ftpserver.Settings, error) {
	if d.settings != nil {
		return d.settings, nil
	}
	return buildSettings(), nil
}

func checkCreds(user, pass string) error {
	if user != ftpUser || pass != ftpPass {
		return errors.New("invalid credentials")
	}
	return nil
}

func (d *driver) ClientConnected(cc ftpserver.ClientContext) (string, error) {
	d.logger.Info("client connected", "remote", cc.RemoteAddr().String(), "id", cc.ID())
	return "EasyVIEW FTP", nil
}

func (d *driver) ClientDisconnected(cc ftpserver.ClientContext) {
	d.logger.Info("client disconnected", "remote", cc.RemoteAddr().String(), "id", cc.ID())
}

func (d *driver) GetTLSConfig() (*tls.Config, error) {
	return nil, errors.New("TLS not enabled")
}

func (d *driver) AuthUser(cc ftpserver.ClientContext, user, pass string) (ftpserver.ClientDriver, error) {
	if err := checkCreds(user, pass); err != nil {
		d.logger.Warn("auth failed", "user", user, "remote", cc.RemoteAddr().String())
		return nil, err
	}
	d.logger.Info("auth ok", "user", user, "remote", cc.RemoteAddr().String())
	return d.rootFs, nil
}

type customTheme struct{}

func (customTheme) Color(name fyne.ThemeColorName, _ fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNameBackground:
		return bgSoft
	case theme.ColorNameForeground:
		return textDark
	case theme.ColorNameForegroundOnPrimary:
		return color.White
	case theme.ColorNamePrimary:
		return accentGreen
	case theme.ColorNameFocus:
		return accentGreen
	case theme.ColorNameHover:
		return color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 0x14}
	case theme.ColorNameButton:
		return cardBg
	case theme.ColorNameInputBackground:
		return cardBg
	case theme.ColorNameInputBorder:
		return cardBorder
	case theme.ColorNameSeparator:
		return cardBorder
	case theme.ColorNameDisabled:
		return textMuted
	}
	return theme.DefaultTheme().Color(name, theme.VariantLight)
}

func (customTheme) Font(style fyne.TextStyle) fyne.Resource {
	return theme.DefaultTheme().Font(style)
}

func (customTheme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return theme.DefaultTheme().Icon(name)
}

func (customTheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNamePadding:
		return 6
	case theme.SizeNameInnerPadding:
		return 10
	case theme.SizeNameText:
		return 14
	case theme.SizeNameHeadingText:
		return 22
	}
	return theme.DefaultTheme().Size(name)
}

func localIPv4s() []string {
	out := []string{}
	ifaces, err := net.Interfaces()
	if err != nil {
		return out
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			ipnet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipnet.IP.To4()
			if ip == nil {
				continue
			}
			out = append(out, ip.String())
		}
	}
	return out
}

func uriToLocalPath(uri fyne.URI) string {
	p := uri.Path()
	if runtime.GOOS == "windows" && len(p) > 2 && p[0] == '/' && p[2] == ':' {
		p = p[1:]
	}
	return filepath.FromSlash(p)
}

func exeDir() string {
	p, err := os.Executable()
	if err != nil {
		p, _ = os.Getwd()
		return p
	}
	return filepath.Dir(p)
}

func configFilePath() string {
	return filepath.Join(exeDir(), configFile)
}

func loadConfiguredFolder() string {
	data, err := os.ReadFile(configFilePath())
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if s, ok := strings.CutPrefix(line, "folder="); ok {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

func saveConfiguredFolder(path string) error {
	return os.WriteFile(configFilePath(), []byte("folder="+path+"\r\n"), 0o644)
}

// resolveDestination returns the directory to serve via FTP.
// When auto-ftp.cfg specifies a folder, it must already exist (we don't
// create directories on removable media). Otherwise, the default
// ./graphiques next to the exe is created on demand.
func resolveDestination() (string, error) {
	if configured := loadConfiguredFolder(); configured != "" {
		info, err := os.Stat(configured)
		if err != nil {
			return configured, err
		}
		if !info.IsDir() {
			return configured, fmt.Errorf("%s n'est pas un dossier", configured)
		}
		return configured, nil
	}
	d := filepath.Join(exeDir(), folderName)
	return d, os.MkdirAll(d, 0o755)
}

func ensureAutostart() {
	if runtime.GOOS != "windows" {
		return
	}
	appData := os.Getenv("APPDATA")
	if appData == "" {
		return
	}
	startupDir := filepath.Join(appData, "Microsoft", "Windows", "Start Menu", "Programs", "Startup")
	if _, err := os.Stat(startupDir); err != nil {
		return
	}
	exePath, err := os.Executable()
	if err != nil {
		return
	}
	vbsPath := filepath.Join(startupDir, startupKey)
	escaped := strings.ReplaceAll(exePath, `"`, `""`)
	content := fmt.Sprintf("Set sh = CreateObject(\"WScript.Shell\")\r\nsh.Run \"\"\"%s\"\"\", 1, False\r\n", escaped)
	if existing, err := os.ReadFile(vbsPath); err == nil && string(existing) == content {
		return
	}
	_ = os.WriteFile(vbsPath, []byte(content), 0o644)
}

func openFolder(path string) {
	switch runtime.GOOS {
	case "windows":
		_ = exec.Command("explorer", path).Start()
	case "darwin":
		_ = exec.Command("open", path).Start()
	default:
		_ = exec.Command("xdg-open", path).Start()
	}
}

func openInNotepad(path string) {
	switch runtime.GOOS {
	case "windows":
		_ = exec.Command("notepad.exe", path).Start()
	case "darwin":
		_ = exec.Command("open", "-t", path).Start()
	default:
		_ = exec.Command("xdg-open", path).Start()
	}
}

func humanizeDuration(d time.Duration) string {
	switch {
	case d < 10*time.Second:
		return "à l'instant"
	case d < time.Minute:
		return fmt.Sprintf("il y a %ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("il y a %d min", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("il y a %dh", int(d.Hours()))
	default:
		return fmt.Sprintf("il y a %dj", int(d.Hours()/24))
	}
}

type activityTracker struct {
	mu       sync.Mutex
	lastFile string
	lastAt   time.Time
	label    *canvas.Text
}

func (a *activityTracker) record(name string) {
	a.mu.Lock()
	a.lastFile = name
	a.lastAt = time.Now()
	a.mu.Unlock()
	a.redraw()
}

func (a *activityTracker) redraw() {
	a.mu.Lock()
	name := a.lastFile
	at := a.lastAt
	a.mu.Unlock()

	text := "Aucun fichier reçu depuis le démarrage."
	if !at.IsZero() {
		text = fmt.Sprintf("Dernier fichier reçu : %s (%s)", name, humanizeDuration(time.Since(at)))
	}
	fyne.Do(func() {
		a.label.Text = text
		a.label.Refresh()
	})
}

func watchFolder(ctx context.Context, dir string, logger *slog.Logger, onEvent func(name string)) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		logger.Error("fsnotify init failed", "error", err)
		return
	}
	if err := w.Add(dir); err != nil {
		logger.Error("fsnotify add failed", "dir", dir, "error", err)
		_ = w.Close()
		return
	}
	go func() {
		defer w.Close()
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-w.Events:
				if !ok {
					return
				}
				if ev.Op&(fsnotify.Create|fsnotify.Write) != 0 {
					onEvent(filepath.Base(ev.Name))
				}
			case err, ok := <-w.Errors:
				if !ok {
					return
				}
				logger.Warn("fsnotify error", "error", err)
			}
		}
	}()
}

func heartbeat(ctx context.Context, addr string, logger *slog.Logger, onFail func(err error), onOK func()) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
			if err != nil {
				logger.Warn("heartbeat failed", "addr", addr, "error", err)
				onFail(err)
				continue
			}
			_ = conn.Close()
			onOK()
		}
	}
}

func setupLogger() (*slog.Logger, string, error) {
	dir := filepath.Join(exeDir(), logsDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, "", err
	}
	logPath := filepath.Join(dir, logFile)
	rot := &lumberjack.Logger{
		Filename:   logPath,
		MaxSize:    5,
		MaxBackups: 3,
		MaxAge:     30,
	}
	h := slog.NewTextHandler(rot, &slog.HandlerOptions{Level: slog.LevelInfo})
	return slog.New(h), logPath, nil
}

type statusBadge struct {
	obj   fyne.CanvasObject
	dot   *canvas.Text
	label *canvas.Text
	bg    *canvas.Rectangle
}

func (s *statusBadge) setOnline() {
	s.dot.Color = accentGreen
	s.label.Color = accentGreen
	s.label.Text = "SERVEUR EN LIGNE"
	s.bg.FillColor = accentGreenLt
	s.bg.StrokeColor = accentGreenBrd
	s.dot.Refresh()
	s.label.Refresh()
	s.bg.Refresh()
}

func (s *statusBadge) setError(text string) {
	s.dot.Color = accentRed
	s.label.Color = accentRed
	s.label.Text = text
	s.bg.FillColor = accentRedLt
	s.bg.StrokeColor = accentRedBord
	s.dot.Refresh()
	s.label.Refresh()
	s.bg.Refresh()
}

func makeStatusBadge() *statusBadge {
	dot := canvas.NewText("●", accentGreen)
	dot.TextSize = 14
	dot.TextStyle = fyne.TextStyle{Bold: true}

	label := canvas.NewText("SERVEUR EN LIGNE", accentGreen)
	label.TextStyle = fyne.TextStyle{Bold: true}
	label.TextSize = 13

	inner := container.NewHBox(dot, label)
	padded := container.New(layout.NewCustomPaddedLayout(4, 4, 12, 12), inner)

	bg := canvas.NewRectangle(accentGreenLt)
	bg.CornerRadius = 14
	bg.StrokeColor = accentGreenBrd
	bg.StrokeWidth = 1

	stack := container.NewStack(bg, padded)
	return &statusBadge{
		obj:   container.NewCenter(stack),
		dot:   dot,
		label: label,
		bg:    bg,
	}
}

func makeErrorBanner() (fyne.CanvasObject, *canvas.Text) {
	title := canvas.NewText("Le serveur FTP n'a pas pu démarrer", accentRed)
	title.TextStyle = fyne.TextStyle{Bold: true}
	title.TextSize = 14

	detail := canvas.NewText("", textDark)
	detail.TextSize = 12

	hint := canvas.NewText("Ouvrez « Voir les logs » et contactez le support en leur lisant le message ci-dessus.", textMuted)
	hint.TextSize = 11

	content := container.NewVBox(title, detail, hint)

	bg := canvas.NewRectangle(accentRedLt)
	bg.CornerRadius = 10
	bg.StrokeColor = accentRedBord
	bg.StrokeWidth = 1

	banner := container.NewStack(bg, container.NewPadded(container.NewPadded(content)))
	return banner, detail
}

func shortBindError(err error) string {
	msg := err.Error()
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "address already in use"),
		strings.Contains(lower, "only one usage of each socket"),
		strings.Contains(lower, "bind: address in use"):
		return fmt.Sprintf("PORT %d DÉJÀ UTILISÉ", ftpPort)
	case strings.Contains(lower, "permission denied"),
		strings.Contains(lower, "access is denied"):
		return "PERMISSION REFUSÉE"
	default:
		return "DÉMARRAGE IMPOSSIBLE"
	}
}

func makeCard(content fyne.CanvasObject) fyne.CanvasObject {
	bg := canvas.NewRectangle(cardBg)
	bg.CornerRadius = 10
	bg.StrokeColor = cardBorder
	bg.StrokeWidth = 1
	return container.NewStack(bg, container.NewPadded(container.NewPadded(content)))
}

type gui struct {
	window      fyne.Window
	badge       *statusBadge
	errorBanner fyne.CanvasObject
	errorDetail *canvas.Text
	activity    *activityTracker
}

func (g *gui) applyError(badgeText, detail string) {
	g.badge.setError(badgeText)
	g.errorDetail.Text = detail
	g.errorDetail.Refresh()
	g.errorBanner.Show()
}

func (g *gui) showErrorFromGoroutine(badgeText, detail string) {
	fyne.Do(func() {
		g.applyError(badgeText, detail)
	})
}

func (g *gui) restoreOnline() {
	fyne.Do(func() {
		g.badge.setOnline()
		g.errorBanner.Hide()
	})
}

func buildGUI(a fyne.App, graphiquesDir, logPath string, ips []string, onClose func()) *gui {
	w := a.NewWindow(appName)
	w.Resize(fyne.NewSize(620, 600))

	title := canvas.NewText(appName, textDark)
	title.TextSize = 26
	title.TextStyle = fyne.TextStyle{Bold: true}
	title.Alignment = fyne.TextAlignCenter

	subtitle := canvas.NewText("Laissez cette fenêtre ouverte pendant l'utilisation d'EasyVIEW.", textMuted)
	subtitle.TextSize = 13
	subtitle.Alignment = fyne.TextAlignCenter

	badge := makeStatusBadge()
	errorBanner, errorDetail := makeErrorBanner()
	errorBanner.Hide()

	header := container.NewVBox(
		title,
		container.NewPadded(subtitle),
		badge.obj,
	)

	mkCopyBtn := func(value string) *widget.Button {
		var btn *widget.Button
		btn = widget.NewButtonWithIcon("", theme.ContentCopyIcon(), func() {
			a.Clipboard().SetContent(value)
			btn.SetIcon(theme.ConfirmIcon())
			btn.Importance = widget.SuccessImportance
			btn.Refresh()
			go func(b *widget.Button) {
				time.Sleep(1500 * time.Millisecond)
				fyne.Do(func() {
					b.SetIcon(theme.ContentCopyIcon())
					b.Importance = widget.MediumImportance
					b.Refresh()
				})
			}(btn)
		})
		return btn
	}

	mkValue := func(text string) *canvas.Text {
		t := canvas.NewText(text, textDark)
		t.TextStyle = fyne.TextStyle{Monospace: true, Bold: true}
		t.TextSize = 15
		return t
	}

	mkLabel := func(text string) *canvas.Text {
		t := canvas.NewText(text, textMuted)
		t.TextStyle = fyne.TextStyle{Bold: true}
		t.TextSize = 13
		t.Alignment = fyne.TextAlignLeading
		return t
	}

	if len(ips) == 0 {
		ips = []string{"127.0.0.1"}
	}

	grid := container.New(layout.NewFormLayout())

	ipHeading := "Adresse IP"
	if len(ips) > 1 {
		ipHeading = "Adresses IP"
	}
	for i, ip := range ips {
		lbl := ""
		if i == 0 {
			lbl = ipHeading
		}
		row := container.NewBorder(nil, nil, nil, mkCopyBtn(ip), container.NewVBox(layout.NewSpacer(), mkValue(ip), layout.NewSpacer()))
		grid.Add(container.NewVBox(layout.NewSpacer(), mkLabel(lbl), layout.NewSpacer()))
		grid.Add(row)
	}

	addRow := func(label, value string) {
		row := container.NewBorder(nil, nil, nil, mkCopyBtn(value), container.NewVBox(layout.NewSpacer(), mkValue(value), layout.NewSpacer()))
		grid.Add(container.NewVBox(layout.NewSpacer(), mkLabel(label), layout.NewSpacer()))
		grid.Add(row)
	}
	addRow("Port", fmt.Sprintf("%d", ftpPort))
	addRow("Utilisateur", ftpUser)
	addRow("Mot de passe", ftpPass)

	cardTitle := canvas.NewText("À renseigner dans EasyVIEW (dans la VM)", textDark)
	cardTitle.TextStyle = fyne.TextStyle{Bold: true}
	cardTitle.TextSize = 15

	cardContent := container.NewVBox(
		cardTitle,
		widget.NewSeparator(),
		grid,
	)
	if len(ips) > 1 {
		hint := canvas.NewText("Si la première adresse ne fonctionne pas, essayez les suivantes.", textMuted)
		hint.TextSize = 12
		hint.TextStyle = fyne.TextStyle{Italic: true}
		cardContent.Add(hint)
	}

	card := makeCard(cardContent)

	folderLabel := canvas.NewText("Dossier de réception", textMuted)
	folderLabel.TextStyle = fyne.TextStyle{Bold: true}
	folderLabel.TextSize = 12

	folderValue := canvas.NewText(graphiquesDir, textDark)
	folderValue.TextStyle = fyne.TextStyle{Monospace: true}
	folderValue.TextSize = 12

	activityLabel := canvas.NewText("Aucun fichier reçu depuis le démarrage.", textMuted)
	activityLabel.TextSize = 12
	activity := &activityTracker{label: activityLabel}

	requestClose := func() {
		onClose()
		w.Close()
	}

	openBtn := widget.NewButton("Ouvrir le dossier", func() {
		openFolder(graphiquesDir)
	})
	logsBtn := widget.NewButton("Voir les logs", func() {
		openInNotepad(logPath)
	})
	changeBtn := widget.NewButton("Changer de dossier…", func() {
		fd := dialog.NewFolderOpen(func(uri fyne.ListableURI, err error) {
			if err != nil || uri == nil {
				return
			}
			path := uriToLocalPath(uri)
			if path == "" {
				return
			}
			if err := saveConfiguredFolder(path); err != nil {
				dialog.ShowError(err, w)
				return
			}
			dialog.ShowInformation(
				"Dossier modifié",
				"Nouveau dossier : "+path+"\n\nCliquez sur « Arrêter le serveur » puis relancez auto-ftp pour appliquer.",
				w,
			)
		}, w)
		if lister, err := storage.ListerForURI(storage.NewFileURI(graphiquesDir)); err == nil {
			fd.SetLocation(lister)
		}
		fd.Show()
	})
	quitBtn := widget.NewButton("Arrêter le serveur", requestClose)
	quitBtn.Importance = widget.WarningImportance

	folderCard := makeCard(container.NewVBox(
		folderLabel,
		folderValue,
		activityLabel,
		container.NewGridWithColumns(2, openBtn, changeBtn, logsBtn, quitBtn),
	))

	firewall := canvas.NewText("Au 1er lancement, Windows peut demander une autorisation réseau : cliquez sur « Autoriser l'accès ».", textMuted)
	firewall.TextSize = 11
	firewall.Alignment = fyne.TextAlignCenter

	versionLabel := canvas.NewText(version, textMuted)
	versionLabel.TextSize = 10
	versionLabel.Alignment = fyne.TextAlignTrailing

	root := container.NewVBox(
		container.NewPadded(header),
		errorBanner,
		card,
		folderCard,
		firewall,
		versionLabel,
	)

	w.SetContent(container.NewPadded(container.NewPadded(root)))
	w.SetCloseIntercept(requestClose)
	w.SetOnClosed(onClose)
	return &gui{
		window:      w,
		badge:       badge,
		errorBanner: errorBanner,
		errorDetail: errorDetail,
		activity:    activity,
	}
}

func main() {
	logger, logPath, err := setupLogger()
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot init logger: %v\n", err)
		os.Exit(1)
	}
	logger.Info("auto-ftp starting", "version", version, "port", ftpPort, "user", ftpUser, "folder", folderName)

	if !acquireSingleton(logger) {
		if focusExistingWindow(appName) {
			logger.Warn("another instance is already running, focused existing window")
			return
		}
		logger.Warn("instance lock held but no visible window, attempting zombie recovery")
		if !killZombieInstances(logger) {
			logger.Error("zombie recovery failed — open Task Manager > Details and end every auto-ftp.exe, then relaunch")
			return
		}
		if !acquireSingleton(logger) {
			logger.Error("singleton still unavailable after zombie cleanup — open Task Manager > Details and end every auto-ftp.exe, then relaunch")
			return
		}
		logger.Info("zombie instances killed, continuing with fresh start")
	}

	graphiquesDir, destErr := resolveDestination()
	if destErr != nil {
		logger.Error("destination folder unavailable", "path", graphiquesDir, "error", destErr)
	} else {
		logger.Info("folder ready", "path", graphiquesDir)
	}

	ensureAutostart()

	ips := localIPv4s()
	logger.Info("local ips detected", "ips", ips)

	var server *ftpserver.FtpServer
	var bindErr error
	if destErr == nil {
		rootFs := afero.NewBasePathFs(afero.NewOsFs(), graphiquesDir)
		drv := &driver{rootFs: rootFs, logger: logger}
		server = ftpserver.NewFtpServer(drv)
		server.Logger = logger

		bindErr = server.Listen()
		if bindErr != nil {
			logger.Error("ftp server bind failed", "error", bindErr)
		} else {
			logger.Info("ftp server listening", "addr", fmt.Sprintf("0.0.0.0:%d", ftpPort))
		}
	}

	// Startup watchdog: if the UI doesn't reach the Fyne main loop within
	// 15s, force-exit. Otherwise a silent Fyne init hang leaves us as a
	// zombie holding the mutex and port 2121 — see the 2026-04 incident.
	var uiAlive atomic.Bool
	go func() {
		time.Sleep(15 * time.Second)
		if !uiAlive.Load() {
			logger.Error("UI failed to start within 15s, exiting to avoid zombie")
			os.Exit(2)
		}
	}()

	logger.Info("fyne: NewWithID starting")
	a := app.NewWithID("com.franck.auto-ftp")
	logger.Info("fyne: NewWithID done")
	a.Settings().SetTheme(customTheme{})
	logger.Info("fyne: theme applied")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var stopped atomic.Bool
	onClose := func() {
		if !stopped.CompareAndSwap(false, true) {
			return
		}
		logger.Info("stopping ftp server")
		cancel()
		if server != nil {
			_ = server.Stop()
		}
	}
	logger.Info("fyne: building GUI")
	g := buildGUI(a, graphiquesDir, logPath, ips, onClose)
	logger.Info("fyne: GUI built")

	switch {
	case destErr != nil:
		g.applyError("DOSSIER INTROUVABLE", destErr.Error())
	case bindErr != nil:
		g.applyError(shortBindError(bindErr), bindErr.Error())
	default:
		go func() {
			if err := server.Serve(); err != nil && !stopped.Load() {
				logger.Error("ftp server crashed", "error", err)
				g.showErrorFromGoroutine("SERVEUR ARRÊTÉ", err.Error())
			}
		}()

		go watchFolder(ctx, graphiquesDir, logger, func(name string) {
			logger.Info("file activity", "name", name)
			g.activity.record(name)
		})

		go func() {
			t := time.NewTicker(30 * time.Second)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					g.activity.redraw()
				}
			}
		}()

		var hbFails atomic.Int32
		go heartbeat(ctx, fmt.Sprintf("127.0.0.1:%d", ftpPort), logger,
			func(err error) {
				if stopped.Load() {
					return
				}
				if hbFails.Add(1) >= 2 {
					g.showErrorFromGoroutine("SERVEUR NE RÉPOND PLUS", err.Error())
				}
			},
			func() {
				if hbFails.Swap(0) >= 2 {
					g.restoreOnline()
				}
			},
		)
	}

	// Queued on the main UI thread; only fires once the Fyne event loop is
	// actually processing, which cancels the startup watchdog above.
	go func() {
		fyne.Do(func() {
			uiAlive.Store(true)
			logger.Info("fyne: main loop running")
		})
	}()

	logger.Info("fyne: ShowAndRun")
	g.window.ShowAndRun()
	cancel()
	logger.Info("gui closed, bye")
}
