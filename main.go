// Package main is the entry point for the Paradox Modding Tools application.
package main

import (
	"embed"
	"log"
	"os"

	"paradox-modding-tools/services"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/updater"
	"github.com/wailsapp/wails/v3/pkg/updater/providers/github"
)

//go:embed all:frontend/dist
var assets embed.FS

var version = "dev"

// main serves a Workshop publish when started as one; otherwise it opens the log, the config and the services and
// runs the app.
func main() {
	if exitCode, served := services.ServeSteamPublish(); served {
		os.Exit(exitCode)
	}

	// the log first, so every later failure, the standard log's and Wails' own, is written into it
	logFile, err := services.OpenLog(version)
	if err != nil {
		log.Fatalf("log: %v", err)
	}
	config, err := services.OpenConfig()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	wikis, err := services.OpenWiki()
	if err != nil {
		log.Fatalf("cache: %v", err)
	}
	workspaces := services.NewWorkspaceService(config, version, wikis)
	logSvc := services.NewLogService(logFile, config)
	appSvc := services.NewAppService(config, workspaces, wikis, version)

	app := newApp(logSvc, []application.Service{
		// registered first, so Wails shuts it down last and the others' shutdowns are logged
		application.NewService(logSvc),
		application.NewService(appSvc),
		application.NewService(&services.FileService{}),
		application.NewService(workspaces),
		application.NewService(services.NewIdeService(config, workspaces)),
		application.NewService(services.NewDrawService(workspaces, config)),
		application.NewService(services.NewEventGraphService(workspaces)),
		application.NewService(services.NewHealthService(workspaces)),
		application.NewService(services.NewWikiService(config, workspaces, wikis)),
		application.NewService(services.NewSearchService()),
		application.NewService(services.NewReleaseService(config)),
		application.NewService(services.NewGitService(config)),
		application.NewService(&services.WindowService{}),
	})
	initUpdater(app)
	openMainWindow(app)

	if err := app.Run(); err != nil {
		log.Fatalf("app.Run: %v", err)
	}
}

// newApp makes the application over its services: the assets, the single-instance rule and the platform options.
func newApp(logSvc *services.LogService, registered []application.Service) *application.App {
	return application.New(application.Options{
		Name:        "paradox-modding-tools",
		Description: "Desktop tools for Paradox Interactive game modding",
		Logger:      logSvc.Logger,
		Services:    registered,
		Assets: application.AssetOptions{
			Handler:    application.BundledAssetFileServer(assets),
			Middleware: services.ServeFiles,
			// a line per file the page loads would push everything else out of pmt.log's tail
			DisableLogging: true,
		},
		// one instance per user: each reads config.json once and rewrites it whole on every change,
		// so two would erase each other's installs and workspaces
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: "com.idodavis.paradox-modding-tools",
			OnSecondInstanceLaunch: func(application.SecondInstanceData) {
				// raise the existing window; exiting silently reads as a failed start
				if window := application.Get().Window.Current(); window != nil {
					window.Restore()
					window.Focus()
				}
			},
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})
}

// initUpdater points the updater at the project's GitHub releases.
func initUpdater(app *application.App) {
	gh, err := github.New(github.Config{
		Repository:    "idodavis/paradox-modding-tools",
		ChecksumAsset: "SHA256SUMS",
	})
	if err != nil {
		log.Fatalf("github.New: %v", err)
	}

	if err := app.Updater.Init(updater.Config{
		CurrentVersion: version,
		Providers:      []updater.Provider{gh},
	}); err != nil {
		log.Fatalf("updater.Init: %v", err)
	}
}

// openMainWindow opens the main window and quits the app when it closes.
func openMainWindow(app *application.App) {
	mainWindow := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:       "main",
		Title:      "Paradox Modding Tools",
		Width:      1300,
		Height:     900,
		StartState: application.WindowStateMaximised,
		URL:        "/",
	})
	// its close asks about every window's unsaved files first, and the other windows close with it
	services.HoldClose(mainWindow)
	mainWindow.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) { app.Quit() })
}
