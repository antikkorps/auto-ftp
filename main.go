package main

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
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
	appName          = "Serveur FTP EasyVIEW"
	singleInstanceID = "AutoFTP-EasyVIEW-v1"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	logger, logPath, err := setupLogger()
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot init logger: %v\n", err)
		os.Exit(1)
	}
	logger.Info("auto-ftp starting", "version", version, "port", ftpPort, "user", ftpUser)

	// Pre-gate run before wails.Run to close a race in Wails'
	// SingleInstanceLock (its mutex is registered inside wails.Run, so
	// two fast relaunches can both slip past it). Wails' lock is kept
	// below as a second line of defence.
	if !acquireSingleton(logger, singleInstanceID+"-pregate") {
		logger.Info("another instance already running, focusing its window and exiting")
		focusExistingWindow(appName)
		return
	}

	app := NewApp(logger, logPath)

	err = wails.Run(&options.App{
		Title:            appName,
		Width:            720,
		Height:           820,
		MinWidth:         620,
		MinHeight:        640,
		BackgroundColour: &options.RGBA{R: 0xfb, G: 0xfd, B: 0xff, A: 1},
		AssetServer:      &assetserver.Options{Assets: assets},
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId:               singleInstanceID,
			OnSecondInstanceLaunch: app.OnSecondInstance,
		},
		OnStartup:     app.OnStartup,
		OnBeforeClose: app.OnBeforeClose,
		Bind:          []any{app},
		Windows: &windows.Options{
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
			DisableWindowIcon:    false,
			WebviewUserDataPath:  filepath.Join(dataDir(), webviewDir),
		},
	})
	if err != nil {
		logger.Error("wails run failed", "error", err)
	}
	logger.Info("gui closed, bye")
}
