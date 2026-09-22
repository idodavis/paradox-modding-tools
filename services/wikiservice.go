// wikiservice.go is the Wails RPC shell for wiki guides and patch notes.

package services

import (
	"context"
	"slices"

	"paradox-modding-tools/services/internal/lang/session"
	"paradox-modding-tools/services/internal/wiki"
)

// WikiService exposes wiki sidecars over Wails. Scan still refreshes in-process.
type WikiService struct {
	Session *SessionService
}

// Guide returns Guide pane pages for the active file. A path under no root gives an empty guide, not an error.
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
	if at := slices.IndexFunc(sess.Mods, func(m session.Mod) bool { return m.ID == place.Origin }); at >= 0 {
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

// StartWikiWarm fetches, in the background, the sidecar of every install's game where it is missing or older than the game's version, then refreshes the ones the app already has, so the guides are current without a click.
func (s *WikiService) StartWikiWarm() {
	var installs []GameInstall
	s.Session.Store.Read(func(c *Config) { installs = append(installs, c.Installs...) })
	go func() {
		for _, inst := range installs {
			wiki.RefreshIfNeeded(context.Background(), inst.GameID, inst.Version, nil)
		}
		wiki.RefreshExisting(context.Background(), nil)
	}()
}

// Status returns sidecar presence and counts for a game.
func (s *WikiService) Status(gameID string) (*wiki.Status, error) {
	st := wiki.StatusOf(gameID)
	return &st, nil
}

// Patches returns the Patch Notes version list, newest first.
func (s *WikiService) Patches(gameID string) (*wiki.PatchList, error) {
	list := wiki.Patches(gameID)
	return &list, nil
}

// PatchPage returns one patch notes body by title or version label.
func (s *WikiService) PatchPage(gameID, version string) (*wiki.PatchPage, error) {
	return wiki.PatchByVersion(gameID, version), nil
}
