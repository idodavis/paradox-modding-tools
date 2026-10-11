// wikiservice.go is the Wails RPC shell for wiki guides and patch notes, and the events of a wiki refresh.

package services

import (
	"cmp"
	"context"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"

	"paradox-modding-tools/services/internal/appdata"
	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/wiki"
)

// EventWikiUpdated is a game's wiki pages being refreshed: data is WikiUpdated.
const EventWikiUpdated = "wiki:updated"

// EventWikiProgress is a game's wiki refresh fetching a page: data is WikiProgress.
const EventWikiProgress = "wiki:progress"

// registers the events so the bindings generator types them
func init() {
	application.RegisterEvent[WikiUpdated](EventWikiUpdated)
	application.RegisterEvent[WikiProgress](EventWikiProgress)
}

// WikiUpdated is one game's wiki refresh, done; Error says why it failed.
type WikiUpdated struct {
	GameID string `json:"gameId"`
	Error  string `json:"error,omitempty"`
}

// WikiProgress is one page a game's wiki refresh fetches: its stage (guides, patch notes), how many
// of the stage's pages are done and how many it fetches.
type WikiProgress struct {
	GameID string `json:"gameId"`
	Stage  string `json:"stage"`
	Done   int    `json:"done"`
	Total  int    `json:"total"`
}

// WikiService serves the wiki guides and patch notes.
type WikiService struct {
	config   *ConfigFile
	sessions *SessionService
	wikis    *wiki.Client
}

// NewWikiService is the wiki service over the config (the installed games it refreshes), the live sessions and the
// wiki client.
func NewWikiService(config *ConfigFile, sessions *SessionService, wikis *wiki.Client) *WikiService {
	return &WikiService{config: config, sessions: sessions, wikis: wikis}
}

// OpenWiki is the wiki client keeping its sidecars in the app's cache folder; main hands it to the services that use
// it, since the wiki package is internal to this one.
func OpenWiki() (*wiki.Client, error) {
	dir, err := appdata.CacheDir()
	if err != nil {
		return nil, err
	}
	return wiki.New(dir), nil
}

// Guide returns the Guide pane's tabs for the active file: each page's title, address and headings, its text
// fetched apart (GuidePage) for the one tab shown. A path under no root gives an empty guide, not an error.
func (s *WikiService) Guide(workspaceID, path string) (*wiki.Guide, error) {
	ws, err := s.sessions.openSession(workspaceID)
	if err != nil {
		return nil, err
	}
	ws.RLock()
	defer ws.RUnlock()
	place := ws.PlaceOf(path)
	if place.Origin == "" {
		return &wiki.Guide{}, nil
	}
	database, kind := "", ""
	if place.Database != nil {
		database, kind = place.Database.Name, game.KindName(place.Database.Name)
	}
	pages := s.wikis.PagesFor(ws.Game.ID, cmp.Or(database, strings.TrimPrefix(filepath.Ext(place.Rel), ".")), place.Rel)
	for i := range pages {
		pages[i].HTML = ""
	}
	return &wiki.Guide{Database: database, Kind: kind, Pages: pages, Attribution: wiki.Attribution(ws.Game.ID)}, nil
}

// GuidePage returns one guide page whole, by its title; nil where the wiki has none so named.
func (s *WikiService) GuidePage(gameID, title string) *wiki.Page {
	return s.wikis.GuidePage(gameID, title)
}

// ServiceStartup refreshes, in the background, the wiki pages of every installed game, so the
// guides are current without a click, and tells the frontend each page fetched and each game done;
// Wails calls it.
func (s *WikiService) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	games := map[string]bool{}
	s.config.read(func(cfg *Config) {
		for _, inst := range cfg.Installs {
			games[inst.GameID] = true
		}
	})
	go refreshWikis(ctx, s.wikis, games)
	return nil
}

// refreshWikis refreshes the wiki pages of each game in turn, telling the frontend each page fetched and each game
// done.
func refreshWikis(ctx context.Context, wikis *wiki.Client, games map[string]bool) {
	for gameID := range games {
		done := WikiUpdated{GameID: gameID}
		if err := wikis.Refresh(ctx, gameID, func(stage string, fetched, total int) {
			emit(EventWikiProgress, WikiProgress{GameID: gameID, Stage: stage, Done: fetched, Total: total})
		}); err != nil {
			done.Error = err.Error()
		}
		emit(EventWikiUpdated, done)
	}
}

// Patches returns the Patch Notes version list, newest first.
func (s *WikiService) Patches(gameID string) wiki.PatchList {
	return s.wikis.Patches(gameID)
}

// PatchPage returns one patch notes body by title or version label.
func (s *WikiService) PatchPage(gameID, version string) *wiki.Page {
	return s.wikis.PatchByVersion(gameID, version)
}
