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

// Guide returns Guide pane pages for the active file. A path under no root gives an empty guide,
// not an error.
func (s *WikiService) Guide(workspaceID, path string) (*wiki.Guide, error) {
	sess, err := s.Session.live(workspaceID)
	if err != nil {
		return nil, err
	}
	sess.RLock()
	defer sess.RUnlock()
	place := sess.Where(path)
	if place.Origin == "" {
		return &wiki.Guide{}, nil
	}
	database, kind := "", ""
	if place.Database != nil {
		database, kind = place.Database.Name, game.KindName(place.Database.Name)
	}
	return &wiki.Guide{
		Database:    database,
		Kind:        kind,
		Pages:       wiki.PagesFor(sess.Game.ID, cmp.Or(database, strings.TrimPrefix(filepath.Ext(place.Rel), ".")), place.Rel),
		Attribution: wiki.Attribution(sess.Game.ID),
	}, nil
}

// ServiceStartup refreshes, in the background, the wiki pages of every installed game, so the
// guides are current without a click, and tells the frontend as each game is done; Wails calls it.
func (s *WikiService) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	games := map[string]bool{}
	s.Session.App.read(func(cfg *Config) {
		for _, inst := range cfg.Installs {
			games[inst.GameID] = true
		}
	})
	go func() {
		for gameID := range games {
			done := WikiUpdated{}
			if err := wiki.Refresh(ctx, gameID); err != nil {
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
