// drawservice.go is the visual editors' service: the open workspace, the last GUI render kept across requests, and its
// calls, each under the workspace's read lock: a GUI list, a coat and the coats a feed shows, an element's properties,
// the designer's lists, a new coat's file.

package services

import (
	"paradox-modding-tools/services/internal/draw"
	"paradox-modding-tools/services/internal/workspace"
)

// DrawService answers the Visual view of a file.
type DrawService struct {
	workspaces *WorkspaceService
	render     draw.Render
}

// NewDrawService is the Visual view's service over the open workspace.
func NewDrawService(workspaces *WorkspaceService) *DrawService {
	return &DrawService{workspaces: workspaces}
}

// List is a file's GUI entry drawn as a list.
func (drw *DrawService) List(workspaceID string, request draw.Request) (*draw.List, error) {
	return locked(drw, workspaceID, func(ws *workspace.Session) (*draw.List, error) { return drw.render.List(ws, request) })
}

// Coat is a file's coat of arms drawn as its layers.
func (drw *DrawService) Coat(workspaceID string, request draw.Request) (*draw.Coat, error) {
	return locked(drw, workspaceID, func(ws *workspace.Session) (*draw.Coat, error) { return draw.CoatOf(ws, request) })
}

// Coats are coats of one file, as the Entries feed draws them; a coat the file does not hold is null.
func (drw *DrawService) Coats(workspaceID, path string, ids []string) ([]*draw.Coat, error) {
	return locked(drw, workspaceID, func(ws *workspace.Session) ([]*draw.Coat, error) {
		return draw.CoatsOf(ws, path, ids), nil
	})
}

// Properties is an element's properties as the panel lists them: each key's last value and who wrote it.
func (drw *DrawService) Properties(workspaceID string, request draw.Request, element int) ([]draw.Property, error) {
	return locked(drw, workspaceID, func(ws *workspace.Session) ([]draw.Property, error) {
		return drw.render.Properties(ws, request, element)
	})
}

// Colors are the named colors' CSS by id, as the Layers panel's color picker lists them.
func (drw *DrawService) Colors(workspaceID string) (map[string]string, error) {
	return locked(drw, workspaceID, func(ws *workspace.Session) (map[string]string, error) { return draw.Colors(ws), nil })
}

// Textures are the routes of the images a coat texture key's folder holds by file name, as the tile picker lists them.
func (drw *DrawService) Textures(workspaceID, key string) (map[string]string, error) {
	return locked(drw, workspaceID, func(ws *workspace.Session) (map[string]string, error) {
		return draw.Textures(ws, key), nil
	})
}

// Frames are the shapes the game's coat designer previews a coat in, as the coat toolbar's Frame picker lists them.
func (drw *DrawService) Frames(workspaceID string) ([]draw.Frame, error) {
	return locked(drw, workspaceID, func(ws *workspace.Session) ([]draw.Frame, error) { return draw.Frames(ws), nil })
}

// CoatFile is the file a mod's new coat of arms is written in, and what writing it makes first.
func (drw *DrawService) CoatFile(workspaceID, modID string) (draw.CoatTarget, error) {
	return locked(drw, workspaceID, func(ws *workspace.Session) (draw.CoatTarget, error) { return draw.CoatFile(ws, modID) })
}

// locked is a call's answer over the open workspace, made under its read lock.
func locked[T any](s *DrawService, workspaceID string, call func(*workspace.Session) (T, error)) (T, error) {
	ws, err := s.workspaces.openWorkspace(workspaceID)
	if err != nil {
		var none T
		return none, err
	}
	ws.RLock()
	defer ws.RUnlock()
	return call(ws)
}
