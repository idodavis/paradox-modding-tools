// Package main starts Paradox Modding Tools. Started with --steam-publish, it
// runs as the copy of the app that publishes to the Workshop.
package main

import (
	"embed"
	"log"
	"os"

	"github.com/wailsapp/wails/v3/pkg/application"
	logservice "github.com/wailsapp/wails/v3/pkg/services/log"

	"paradox-modding-tools/services/agent"
	"paradox-modding-tools/services/app"
	"paradox-modding-tools/services/draw"
	"paradox-modding-tools/services/git"
	"paradox-modding-tools/services/graph"
	"paradox-modding-tools/services/health"
	"paradox-modding-tools/services/lsp"
	"paradox-modding-tools/services/mcp"
	"paradox-modding-tools/services/search"
	"paradox-modding-tools/services/steam"
	"paradox-modding-tools/services/terminal"
	"paradox-modding-tools/services/wiki"
	"paradox-modding-tools/services/workspace"
)

// assets is the built frontend, embedded so the app ships as one file.
//
//go:embed all:frontend/dist
var assets embed.FS

// version is set by the build. A dev run keeps app.DevBuild.
var version = app.DevBuild

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

	appService := &app.Service{Config: config, Build: version}
	pmt := application.New(application.Options{
		Name:        "paradox-modding-tools",
		Description: "Desktop tools for Paradox Interactive game modding",
		Logger:      logger,
		Services:    listServices(config, appService),
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
	appService.OpenMainWindow()

	if err := pmt.Run(); err != nil {
		log.Fatalf("app.Run: %v", err)
	}
}

// listServices returns the app's services. Most of them reach the config and
// the open workspace through the workspace service.
func listServices(config *app.ConfigFile, appService *app.Service) []application.Service {
	wsp := &workspace.Service{Config: config, Build: version}
	gitService := &git.Service{Workspace: wsp}
	lspService := &lsp.Service{Workspace: wsp}
	healthService := &health.Service{Workspace: wsp}
	graphService := &graph.Service{Workspace: wsp}
	wikiService := &wiki.Service{Workspace: wsp}
	drawService := &draw.Service{Workspace: wsp}
	mcpService := &mcp.Service{
		Workspace: wsp, LSP: lspService, Health: healthService, Graph: graphService, Wiki: wikiService,
		Draw: drawService, Build: version,
	}

	return []application.Service{
		application.NewService(appService),
		application.NewService(logservice.New()),
		application.NewServiceWithOptions(wsp, application.ServiceOptions{Route: "/files"}),
		application.NewService(lspService),
		application.NewService(&terminal.Service{Workspace: wsp, Config: config}),
		application.NewService(drawService),
		application.NewService(healthService),
		application.NewService(graphService),
		application.NewService(&search.Service{Workspace: wsp}),
		application.NewService(wikiService),
		application.NewService(mcpService),
		application.NewService(&agent.Service{Workspace: wsp, MCP: mcpService}),
		application.NewService(gitService),
		application.NewService(&steam.Service{Workspace: wsp, Git: gitService}),
	}
}
