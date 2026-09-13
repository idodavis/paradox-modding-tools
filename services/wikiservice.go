// wikiservice.go is the Wails RPC shell for wiki guides and patch notes.

package services

import (
	"path/filepath"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/session"
	"paradox-modding-tools/services/internal/wiki"
)

// WikiService exposes wiki sidecars over Wails. Scan still refreshes in-process.
type WikiService struct {
	Session *SessionService
}

// Guide returns Guide pane pages for the active file. Locate fail → empty, not an error.
func (s *WikiService) Guide(workspaceID, path string) (*wiki.Guide, error) {
	return withSession(s.Session, workspaceID, func(sess *session.Session) *wiki.Guide {
		origin, rel, ok := sess.Locate(path)
		if !ok {
			return &wiki.Guide{}
		}
		slash := filepath.ToSlash(rel)
		kind := game.MatchExtract(sess.GameID, slash).Kind
		return &wiki.Guide{
			Rel:         slash,
			Origin:      origin,
			OriginName:  sess.OriginName(origin),
			Kind:        kind,
			Pages:       wiki.PagesFor(sess.GameID, kind, slash),
			Attribution: wiki.Attribution(sess.GameID),
		}
	})
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
