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

func main() {
	if exitCode, served := services.ServeSteamPublish(); served {
		os.Exit(exitCode)
	}

	// the log first, so every later failure, the standard log's and Wails' own, is written into it
	logSvc, err := services.OpenLog(version)
	if err != nil {
		log.Fatalf("log: %v", err)
	}
	appSvc, err := services.OpenApp()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	appSvc.Version = version
	logSvc.App = appSvc

	fileSvc := &services.FileService{}
	sessSvc := &services.SessionService{App: appSvc}
	appSvc.Session = sessSvc
	workspaceSvc := &services.WorkspaceService{App: appSvc, Session: sessSvc}
	ideSvc := &services.IdeService{Session: sessSvc, App: appSvc}
	graphSvc := &services.EventGraphService{Session: sessSvc}
	healthSvc := &services.HealthService{Session: sessSvc}
	wikiSvc := &services.WikiService{Session: sessSvc}
	searchSvc := &services.SearchService{}
	releaseSvc := &services.ReleaseService{App: appSvc}
	gitSvc := &services.GitService{App: appSvc}
	windowSvc := &services.WindowService{}

	app := application.New(application.Options{
		Name:        "paradox-modding-tools",
		Description: "Desktop tools for Paradox Interactive game modding",
		Logger:      logSvc.Logger,
		// registered first, so Wails shuts it down last and the others' shutdowns are logged
		Services: []application.Service{
			application.NewService(logSvc),
			application.NewService(appSvc),
			application.NewService(fileSvc),
			application.NewService(workspaceSvc),
			application.NewService(sessSvc),
			application.NewService(ideSvc),
			application.NewService(graphSvc),
			application.NewService(healthSvc),
			application.NewService(wikiSvc),
			application.NewService(searchSvc),
			application.NewService(releaseSvc),
			application.NewService(gitSvc),
			application.NewService(windowSvc),
		},
		Assets: application.AssetOptions{
			Handler: application.BundledAssetFileServer(assets),
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

	mainWindow := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:   "main",
		Title:  "Paradox Modding Tools",
		Width:  1300,
		Height: 900,
		Mac: application.MacWindow{
			InvisibleTitleBarHeight: 50,
			Backdrop:                application.MacBackdropTranslucent,
			TitleBar:                application.MacTitleBarHiddenInset,
		},
		URL: "/",
	})
	// the other windows close with it
	mainWindow.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) { app.Quit() })

	if err := app.Run(); err != nil {
		log.Fatalf("app.Run: %v", err)
	}
}
