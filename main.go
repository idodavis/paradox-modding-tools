// Package main is the entry point of Paradox Modding Tools. It opens the config and the log, makes each service,
// wires them with the updater and the main window into the Wails application, and runs it. Started with
// --steam-publish, it is the second copy of the app that publishes to the Workshop.
package main

import (
	"embed"
	"log"
	"os"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
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
	// the workspace service holds the one open workspace; every other service reads it through its Session
	wsp := &workspace.Service{Config: config}
	editor := &lsp.Service{Workspace: wsp}

	pmt := application.New(application.Options{
		Name:        "paradox-modding-tools",
		Description: "Desktop tools for Paradox Interactive game modding",
		Logger:      logger,
		Services: []application.Service{
			application.NewService(&app.Service{Config: config, Build: version}),
			application.NewService(logservice.New()),
			application.NewServiceWithOptions(wsp, application.ServiceOptions{Route: "/files"}),
			application.NewService(editor),
			application.NewService(&draw.Service{Workspace: wsp}),
			application.NewService(&health.Service{Workspace: wsp, Editor: editor}),
			application.NewService(&graph.Service{Workspace: wsp}),
			application.NewService(&search.Service{Workspace: wsp}),
			application.NewService(&wiki.Service{Config: config, Workspace: wsp}),
			application.NewService(&git.Service{}),
			application.NewService(&steam.Service{Config: config, Workspace: wsp}),
		},
		Assets: application.AssetOptions{
			Handler: application.BundledAssetFileServer(assets),
			// a line per file the page loads would push everything else out of pmt.log's tail
			DisableLogging: true,
		},
		// one instance per user: each reads config.json once and rewrites it whole on every change
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
	openMainWindow(pmt)

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

// openMainWindow opens the main window, named main, the only one whose close quits the app.
func openMainWindow(pmt *application.App) {
	window := pmt.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:   "main",
		Title:  "Paradox Modding Tools",
		Width:  1400,
		Height: 900,
		URL:    "/",
	})
	window.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) { pmt.Quit() })
}
