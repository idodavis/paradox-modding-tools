// Package main is the entry point for the Paradox Modding Tools application.
package main

import (
	"embed"
	"log"

	"paradox-modding-tools/services"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/updater"
	"github.com/wailsapp/wails/v3/pkg/updater/providers/github"
)

//go:embed all:frontend/dist
var assets embed.FS

var version = "dev"

func main() {
	store, err := services.OpenStore()
	if err != nil {
		log.Fatalf("store: %v", err)
	}

	fileSvc := &services.FileService{}
	sessSvc := &services.SessionService{Store: store}
	settingsSvc := &services.SettingsService{Store: store, Session: sessSvc, Version: version}
	workspaceSvc := &services.WorkspaceService{Store: store, Session: sessSvc}
	ideSvc := &services.IdeService{Session: sessSvc, Settings: settingsSvc}
	viewsSvc := &services.ViewsService{Session: sessSvc}
	wikiSvc := &services.WikiService{Session: sessSvc}
	searchSvc := &services.SearchService{}
	releaseSvc := &services.ReleaseService{Store: store}

	app := application.New(application.Options{
		Name:        "paradox-modding-tools",
		Description: "Desktop tools for Paradox Interactive game modding",
		Services: []application.Service{
			application.NewService(settingsSvc),
			application.NewService(fileSvc),
			application.NewService(workspaceSvc),
			application.NewService(sessSvc),
			application.NewService(ideSvc),
			application.NewService(viewsSvc),
			application.NewService(wikiSvc),
			application.NewService(searchSvc),
			application.NewService(releaseSvc),
		},
		Assets: application.AssetOptions{
			Handler: application.BundledAssetFileServer(assets),
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

	sessSvc.StartWikiWarm(version)

	app.Window.NewWithOptions(application.WebviewWindowOptions{
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

	if err := app.Run(); err != nil {
		log.Fatalf("app.Run: %v", err)
	}
}
