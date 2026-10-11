// wikiservice.go is the Wails RPC shell for wiki guides and patch notes.

package services

import (
	"cmp"
	"context"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/wiki"
)

// WikiService serves the wiki guides and patch notes.
type WikiService struct {
	Session *SessionService
}

// Guide returns the Guide pane's tabs for the active file: each page's title, address and headings, its text
// fetched apart (GuidePage) for the one tab shown. A path under no root gives an empty guide, not an error.
func (s *WikiService) Guide(workspaceID, path string) (*wiki.Guide, error) {
	sess, err := s.Session.live(workspaceID)
	if err != nil {
		return nil, err
	}
	sess.RLock()
	defer sess.RUnlock()
	place := sess.PlaceOf(path)
	if place.Origin == "" {
		return &wiki.Guide{}, nil
	}
	database, kind := "", ""
	if place.Database != nil {
		database, kind = place.Database.Name, game.KindName(place.Database.Name)
	}
	pages := wiki.PagesFor(sess.Game.ID, cmp.Or(database, strings.TrimPrefix(filepath.Ext(place.Rel), ".")), place.Rel)
	for i := range pages {
		pages[i].HTML = ""
	}
	return &wiki.Guide{Database: database, Kind: kind, Pages: pages, Attribution: wiki.Attribution(sess.Game.ID)}, nil
}

// GuidePage returns one guide page whole, by its title; nil where the wiki has none so named.
func (s *WikiService) GuidePage(gameID, title string) *wiki.Page {
	return wiki.GuidePage(gameID, title)
}

// ServiceStartup refreshes, in the background, the wiki pages of every installed game, so the
// guides are current without a click, and tells the frontend each page fetched and each game done;
// Wails calls it.
func (s *WikiService) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	games := map[string]bool{}
	s.Session.App.read(func(cfg *Config) {
		for _, inst := range cfg.Installs {
			games[inst.GameID] = true
		}
	})
	go func() {
		for gameID := range games {
			done := WikiUpdated{GameID: gameID}
			if err := wiki.Refresh(ctx, gameID, func(stage string, fetched, total int) {
				emit(EventWikiProgress, WikiProgress{GameID: gameID, Stage: stage, Done: fetched, Total: total})
			}); err != nil {
				done.Error = err.Error()
			}
			emit(EventWikiUpdated, done)
		}
	}()
	return nil
}

// Patches returns the Patch Notes version list, newest first.
func (s *WikiService) Patches(gameID string) wiki.PatchList {
	return wiki.Patches(gameID)
}

// PatchPage returns one patch notes body by title or version label.
func (s *WikiService) PatchPage(gameID, version string) *wiki.Page {
	return wiki.PatchByVersion(gameID, version)
}
