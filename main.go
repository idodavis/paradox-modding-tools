// Package main is the entry point of Paradox Modding Tools. It opens the config and the log, makes the services and
// runs the Wails application with the updater and the main window. Started with --steam-publish, it runs as the
// second copy of the app that publishes to the Workshop.
package main

import (
	"embed"
	"log"
	"os"

	"github.com/wailsapp/wails/v3/pkg/application"
	logservice "github.com/wailsapp/wails/v3/pkg/services/log"
	"github.com/wailsapp/wails/v3/pkg/updater"
	"github.com/wailsapp/wails/v3/pkg/updater/providers/github"

	"paradox-modding-tools/services/app"
	"paradox-modding-tools/services/draw"
	"paradox-modding-tools/services/git"
	"paradox-modding-tools/services/graph"
	"paradox-modding-tools/services/health"
	"paradox-modding-tools/services/lsp"
	"paradox-modding-tools/services/search"
	"paradox-modding-tools/services/steam"
	"paradox-modding-tools/services/wiki"
	"paradox-modding-tools/services/workspace"
)

// assets is the built frontend, embedded so the app ships as one file.
//
//go:embed all:frontend/dist
var assets embed.FS

// version is the app's version, set by the build. It is "dev" in a dev run.
var version = "dev"

// main runs the publishing process when the app was started as one. Otherwise it opens the config and the log,
// makes the services and runs the app.
func main() {
	if exitCode, served := steam.ServePublish(os.Args[1:]); served {
		os.Exit(exitCode)
	}
	config, err := app.OpenConfig()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	logger, err := app.OpenLog(config, version)
	if err != nil {
		log.Fatalf("log: %v", err)
	}
	// the other services ask the workspace service for the open workspace
	wsp := &workspace.Service{Config: config, Build: version}
	editor := &lsp.Service{Workspace: wsp}
	appService := &app.Service{Config: config, Build: version}

	pmt := application.New(application.Options{
		Name:        "paradox-modding-tools",
		Description: "Desktop tools for Paradox Interactive game modding",
		Logger:      logger,
		Services: []application.Service{
			application.NewService(appService),
			application.NewService(logservice.New()),
			application.NewServiceWithOptions(wsp, application.ServiceOptions{Route: "/files"}),
			application.NewService(editor),
			application.NewService(&draw.Service{Workspace: wsp}),
			application.NewService(&health.Service{Workspace: wsp}),
			application.NewService(&graph.Service{Workspace: wsp}),
			application.NewService(&search.Service{Workspace: wsp}),
			application.NewService(&wiki.Service{Workspace: wsp}),
			application.NewService(&git.Service{Workspace: wsp}),
			application.NewService(&steam.Service{Workspace: wsp}),
		},
		Assets: application.AssetOptions{
			Handler: application.BundledAssetFileServer(assets),
			// a log line per loaded file would fill pmt.log
			DisableLogging: true,
		},
		// two copies would overwrite each other's config.json
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: "com.idodavis.paradox-modding-tools",
			OnSecondInstanceLaunch: func(application.SecondInstanceData) {
				if window := application.Get().Window.Current(); window != nil {
					window.Restore()
					window.Focus()
				}
			},
		},
	})
	initUpdater(pmt)
	appService.OpenMainWindow()

	if err := pmt.Run(); err != nil {
		log.Fatalf("app.Run: %v", err)
	}
}

// initUpdater points the updater at the project's GitHub releases, verified by SHA256SUMS.
func initUpdater(pmt *application.App) {
	gh, err := github.New(github.Config{
		Repository:    "idodavis/paradox-modding-tools",
		ChecksumAsset: "SHA256SUMS",
	})
	if err != nil {
		log.Fatalf("github.New: %v", err)
	}
	if err := pmt.Updater.Init(updater.Config{
		CurrentVersion: version,
		Providers:      []updater.Provider{gh},
	}); err != nil {
		log.Fatalf("updater.Init: %v", err)
	}
}
