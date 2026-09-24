// wikiservice.go is the Wails RPC shell for wiki guides and patch notes.

package services

import (
	"context"
	"slices"

	"paradox-modding-tools/services/internal/session"
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
	kind, name := "", sess.Game.Name
	if len(place.Kinds) > 0 {
		kind = place.Kinds[0].Name
	}
	if at := slices.IndexFunc(sess.Mods, func(mod session.Mod) bool { return mod.ID == place.Origin }); at >= 0 {
		name = sess.Mods[at].Name
	}
	return &wiki.Guide{
		Rel:         place.Rel,
		Origin:      place.Origin,
		OriginName:  name,
		Kind:        kind,
		Pages:       wiki.PagesFor(sess.Game.ID, kind, place.Rel),
		Attribution: wiki.Attribution(sess.Game.ID),
	}, nil
}

// StartWikiWarm refreshes, in the background, the wiki pages of every installed game, so the
// guides are current without a click, and tells the frontend as each game is done.
func (s *WikiService) StartWikiWarm() {
	games := map[string]bool{}
	s.Session.Config.read(func(cfg *Config) {
		for _, inst := range cfg.Installs {
			games[inst.GameID] = true
		}
	})
	go func() {
		for gameID := range games {
			done := WikiUpdated{GameID: gameID}
			if err := wiki.Refresh(context.Background(), gameID); err != nil {
				done.Error = err.Error()
			}
			emit(EventWikiUpdated, done)
		}
	}()
}

// Patches returns the Patch Notes version list, newest first.
func (s *WikiService) Patches(gameID string) (*wiki.PatchList, error) {
	patches := wiki.Patches(gameID)
	return &patches, nil
}

// PatchPage returns one patch notes body by title or version label.
func (s *WikiService) PatchPage(gameID, version string) (*wiki.PatchPage, error) {
	return wiki.PatchByVersion(gameID, version), nil
}
