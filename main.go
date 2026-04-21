package main

import (
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
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	ftpserver "github.com/fclairamb/ftpserverlib"
	"github.com/spf13/afero"
	"gopkg.in/natefinch/lumberjack.v2"
)

const (
	ftpUser    = "vmsync"
	ftpPass    = "vmsync"
	ftpPort    = 2121
	pasvStart  = 2122
	pasvEnd    = 2130
	folderName = "graphiques"
	logsDir    = "logs"
	logFile    = "auto-ftp.log"
	appName    = "Serveur FTP EasyVIEW"
	startupKey = "auto-ftp.vbs"
)

var (
	accentGreen   = color.NRGBA{R: 0x1e, G: 0x96, B: 0x3f, A: 0xff}
	accentGreenLt = color.NRGBA{R: 0xe5, G: 0xf6, B: 0xea, A: 0xff}
	textDark      = color.NRGBA{R: 0x1c, G: 0x26, B: 0x35, A: 0xff}
	textMuted     = color.NRGBA{R: 0x64, G: 0x70, B: 0x82, A: 0xff}
	bgSoft        = color.NRGBA{R: 0xf6, G: 0xf8, B: 0xfb, A: 0xff}
	cardBg        = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	cardBorder    = color.NRGBA{R: 0xe3, G: 0xe8, B: 0xef, A: 0xff}
)

type driver struct {
	rootFs afero.Fs
	logger *slog.Logger
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

func exeDir() string {
	p, err := os.Executable()
	if err != nil {
		p, _ = os.Getwd()
		return p
	}
	return filepath.Dir(p)
}

func ensureGraphiquesDir() (string, error) {
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

func makeStatusBadge() fyne.CanvasObject {
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
	bg.StrokeColor = color.NRGBA{R: 0x1e, G: 0x96, B: 0x3f, A: 0x33}
	bg.StrokeWidth = 1

	stack := container.NewStack(bg, padded)
	return container.NewCenter(stack)
}

func makeCard(content fyne.CanvasObject) fyne.CanvasObject {
	bg := canvas.NewRectangle(cardBg)
	bg.CornerRadius = 10
	bg.StrokeColor = cardBorder
	bg.StrokeWidth = 1
	return container.NewStack(bg, container.NewPadded(container.NewPadded(content)))
}

func buildGUI(a fyne.App, graphiquesDir, logPath string, ips []string, onClose func()) fyne.Window {
	w := a.NewWindow(appName)
	w.Resize(fyne.NewSize(620, 560))

	title := canvas.NewText(appName, textDark)
	title.TextSize = 26
	title.TextStyle = fyne.TextStyle{Bold: true}
	title.Alignment = fyne.TextAlignCenter

	subtitle := canvas.NewText("Laissez cette fenêtre ouverte pendant l'utilisation d'EasyVIEW.", textMuted)
	subtitle.TextSize = 13
	subtitle.Alignment = fyne.TextAlignCenter

	header := container.NewVBox(
		title,
		container.NewPadded(subtitle),
		makeStatusBadge(),
	)

	mkCopyBtn := func(value string) *widget.Button {
		var btn *widget.Button
		btn = widget.NewButton("Copier", func() {
			a.Clipboard().SetContent(value)
			btn.SetText("Copié !")
			btn.Importance = widget.SuccessImportance
			btn.Refresh()
			go func(b *widget.Button) {
				time.Sleep(1500 * time.Millisecond)
				fyne.Do(func() {
					b.SetText("Copier")
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
		t.Alignment = fyne.TextAlignTrailing
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

	openBtn := widget.NewButton("Ouvrir le dossier", func() {
		openFolder(graphiquesDir)
	})
	logsBtn := widget.NewButton("Voir les logs", func() {
		openInNotepad(logPath)
	})
	quitBtn := widget.NewButton("Arrêter le serveur", func() {
		onClose()
		a.Quit()
	})
	quitBtn.Importance = widget.WarningImportance

	folderCard := makeCard(container.NewVBox(
		folderLabel,
		folderValue,
		container.NewGridWithColumns(3, openBtn, logsBtn, quitBtn),
	))

	firewall := canvas.NewText("Au 1er lancement, Windows peut demander une autorisation réseau : cliquez sur « Autoriser l'accès ».", textMuted)
	firewall.TextSize = 11
	firewall.Alignment = fyne.TextAlignCenter

	root := container.NewVBox(
		container.NewPadded(header),
		card,
		folderCard,
		firewall,
	)

	w.SetContent(container.NewPadded(container.NewPadded(root)))
	w.SetOnClosed(onClose)
	return w
}

func main() {
	logger, logPath, err := setupLogger()
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot init logger: %v\n", err)
		os.Exit(1)
	}
	logger.Info("auto-ftp starting", "port", ftpPort, "user", ftpUser, "folder", folderName)

	graphiquesDir, err := ensureGraphiquesDir()
	if err != nil {
		logger.Error("cannot create graphiques folder", "error", err)
		os.Exit(1)
	}
	logger.Info("folder ready", "path", graphiquesDir)

	ensureAutostart()

	ips := localIPv4s()
	logger.Info("local ips detected", "ips", ips)

	rootFs := afero.NewBasePathFs(afero.NewOsFs(), graphiquesDir)
	drv := &driver{rootFs: rootFs, logger: logger}
	server := ftpserver.NewFtpServer(drv)
	server.Logger = logger

	go func() {
		logger.Info("ftp server listening", "addr", fmt.Sprintf("0.0.0.0:%d", ftpPort))
		if err := server.ListenAndServe(); err != nil {
			logger.Error("ftp server stopped", "error", err)
		}
	}()
	time.Sleep(150 * time.Millisecond)

	a := app.NewWithID("com.franck.auto-ftp")
	a.Settings().SetTheme(customTheme{})

	stopped := false
	onClose := func() {
		if stopped {
			return
		}
		stopped = true
		logger.Info("stopping ftp server")
		_ = server.Stop()
	}
	w := buildGUI(a, graphiquesDir, logPath, ips, onClose)
	w.ShowAndRun()
	logger.Info("gui closed, bye")
}
