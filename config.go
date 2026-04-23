package main

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"gopkg.in/natefinch/lumberjack.v2"
)

const (
	folderName  = "graphiques"
	dataDirName = "af-data"
	logsDir     = "logs"
	webviewDir  = "webview"
	logFile     = "auto-ftp.log"
	startupKey  = "auto-ftp.vbs"
	configFile  = "auto-ftp.cfg"
)

func exeDir() string {
	p, err := os.Executable()
	if err != nil {
		p, _ = os.Getwd()
		return p
	}
	return filepath.Dir(p)
}

// dataDir is the opaque internal state directory next to the exe:
// app config, rotated logs, and WebView2 user data all live here.
// The user-facing graphiques/ folder intentionally stays at the top
// level so operators can find received files without spelunking.
func dataDir() string {
	return filepath.Join(exeDir(), dataDirName)
}

func configFilePath() string {
	return filepath.Join(dataDir(), configFile)
}

func loadConfiguredFolder() string {
	data, err := os.ReadFile(configFilePath())
	if err != nil {
		return ""
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if s, ok := strings.CutPrefix(line, "folder="); ok {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

func saveConfiguredFolder(path string) error {
	if err := os.MkdirAll(dataDir(), 0o755); err != nil {
		return err
	}
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

func setupLogger() (*slog.Logger, string, error) {
	dir := filepath.Join(dataDir(), logsDir)
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
