package atlas

import (
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"

	"paradox-modding-tools/services/app"
	"paradox-modding-tools/services/workspace"
)

// gateSubjects are the subjects the gate builds on each game: a good, a
// building and an event or a technology.
var gateSubjects = map[string][]string{
	"ck3":  {"trade/trade_good_types:trade_good_cattle", "buildings:castle_01", "events:birth.0001"},
	"vic3": {"goods:coal", "buildings:building_tooling_workshop", "technology/technologies:mechanical_tools"},
	"eu5":  {"goods:lumber", "building_types:cathedral", "advances:artillery_institution_advance"},
}

// createApp makes the application that carries the workspace's events, once
// per test run.
var createApp = sync.OnceFunc(func() { application.New(application.Options{}) })

// openService opens a workspace of English on an install with the given
// mods, and returns the Atlas service over it.
func openService(t *testing.T, gameID, installPath string, mods ...app.Mod) *Service {
	t.Helper()
	createApp()
	for _, variable := range []string{"APPDATA", "LOCALAPPDATA", "XDG_CONFIG_HOME", "XDG_CACHE_HOME"} {
		t.Setenv(variable, t.TempDir())
	}

	config, err := app.OpenConfig()
	if err != nil {
		t.Fatal(err)
	}

	err = config.Update("", func(records *app.Config) error {
		records.Installs = []app.Install{{ID: "i1", Game: gameID, Path: installPath}}
		records.Workspaces = []app.Workspace{{ID: "w1", Name: "Test", Install: "i1", Mods: mods, Language: "english"}}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	wsp := &workspace.Service{Config: config, Build: "test"}
	t.Cleanup(wsp.Close)
	wsp.Open("w1")
	if _, err := wsp.Session(); err != nil {
		t.Fatal(err)
	}

	return &Service{Workspace: wsp}
}

// TestBuildInstalls builds the canvas and the detail of a good, a building
// and an event or a technology on each real install that
// PMT_TEST_INSTALL_<GAME> names. It fails if one answers with an error or
// with no node.
func TestBuildInstalls(t *testing.T) {
	for gameID, subjects := range gateSubjects {
		t.Run(gameID, func(t *testing.T) {
			variable := "PMT_TEST_INSTALL_" + strings.ToUpper(gameID)
			install := os.Getenv(variable)
			if install == "" {
				t.Skipf("%s is not set", variable)
			}

			atlas := openService(t, gameID, install)
			for _, subject := range subjects {
				graph, err := atlas.BuildGraph(subject, atlas.Defaults())
				if err != nil || len(graph.Nodes) == 0 {
					t.Errorf("BuildGraph(%s) drew %d nodes, %v", subject, len(graph.Nodes), err)
				}

				if _, err := atlas.BuildDetail(subject); err != nil {
					t.Errorf("BuildDetail(%s): %v", subject, err)
				}
			}
		})
	}
}
