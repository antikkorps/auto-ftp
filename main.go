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
	window       fyne.Window
	badge        *statusBadge
	errorBanner  fyne.CanvasObject
	errorDetail  *canvas.Text
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
		errorBanner,
		card,
		folderCard,
		firewall,
	)

	w.SetContent(container.NewPadded(container.NewPadded(root)))
	w.SetOnClosed(onClose)
	return &gui{
		window:      w,
		badge:       badge,
		errorBanner: errorBanner,
		errorDetail: errorDetail,
	}
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

	bindErr := server.Listen()
	if bindErr != nil {
		logger.Error("ftp server bind failed", "error", bindErr)
	} else {
		logger.Info("ftp server listening", "addr", fmt.Sprintf("0.0.0.0:%d", ftpPort))
	}

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
	g := buildGUI(a, graphiquesDir, logPath, ips, onClose)

	if bindErr != nil {
		g.applyError(shortBindError(bindErr), bindErr.Error())
	} else {
		go func() {
			if err := server.Serve(); err != nil && !stopped {
				logger.Error("ftp server crashed", "error", err)
				g.showErrorFromGoroutine("SERVEUR ARRÊTÉ", err.Error())
			}
		}()
	}

	g.window.ShowAndRun()
	logger.Info("gui closed, bye")
}
