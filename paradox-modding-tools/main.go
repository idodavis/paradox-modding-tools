package main

import (
	"embed"
	_ "embed"
	"log"

	"paradox-modding-tools/services"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// Wails uses Go's `embed` package to embed the frontend files into the binary.
// Any files in the frontend/dist folder will be embedded into the binary and
// made available to the frontend.
// See https://pkg.go.dev/embed for more information.

//go:embed all:frontend/dist
var assets embed.FS

func init() {
}

// main function serves as the application's entry point. It initializes the application, creates a window,
// and starts a goroutine that emits a time-based event every second. It subsequently runs the application and
// logs any error that might occur.
func main() {
	dbSvc := &services.DbService{}
	if err := dbSvc.ServiceStartup(); err != nil {
		log.Fatalf("db startup: %v", err)
	}

	logSvc := &services.LogService{}
	if err := logSvc.ServiceStartup(); err != nil {
		log.Fatalf("log startup: %v", err)
	}

	fileSvc := &services.FileService{}
	mergeSvc := &services.MergeService{FileService: fileSvc}
	modDocSvc := &services.ModDocService{FileService: fileSvc, DB: dbSvc.DB}
	settingsSvc := &services.SettingsService{DB: dbSvc.DB}
	steamSvc := &services.SteamService{DB: dbSvc.DB}
	invSvc := &services.InventoryService{DB: dbSvc.DB}

	app := application.New(application.Options{
		Name:        "paradox-modding-tools",
		Description: "A demo of using raw HTML & CSS",
		Services: []application.Service{
			application.NewService(logSvc),
			application.NewService(dbSvc),
			application.NewService(fileSvc),
			application.NewService(&services.BrowserService{}),
			application.NewService(&services.CompareService{}),
			application.NewService(modDocSvc),
			application.NewService(settingsSvc),
			application.NewService(steamSvc),
			application.NewService(&services.ClipboardService{}),
			application.NewService(invSvc),
			application.NewService(mergeSvc),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:  "Paradox Modding Tools",
		Width:  1300,
		Height: 900,
		Mac: application.MacWindow{
			InvisibleTitleBarHeight: 50,
			Backdrop:                application.MacBackdropTranslucent,
			TitleBar:                application.MacTitleBarHiddenInset,
		},
		BackgroundColour: application.NewRGB(15, 20, 25),
		URL:              "/",
	})

	// Run the application. This blocks until the application has been exited.
	err := app.Run()
	// If an error occurred while running the application, log it and exit.
	if err != nil {
		log.Fatal(err)
	}
}
