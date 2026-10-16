// drawservice.go is the visual editors' service: the open workspace, the last render kept across requests, and its
// calls, each under the workspace's read lock: a list, an element's properties, the pickers' names, a new coat's file.

package services

import "paradox-modding-tools/services/internal/draw"

// DrawService answers the Visual view of a file.
type DrawService struct {
	workspaces *WorkspaceService
	render     draw.Render
}

// NewDrawService is the Visual view's service over the open workspace.
func NewDrawService(workspaces *WorkspaceService) *DrawService {
	return &DrawService{workspaces: workspaces}
}

// List is a file's entry drawn as a list.
func (s *DrawService) List(workspaceID string, request draw.Request) (*draw.List, error) {
	ws, err := s.workspaces.openWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	ws.RLock()
	defer ws.RUnlock()
	return s.render.List(ws, request)
}

// Properties is an element's properties as the Widgets panel lists them: each key's last value and who wrote it.
func (s *DrawService) Properties(workspaceID string, request draw.Request, element int) ([]draw.Property, error) {
	ws, err := s.workspaces.openWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	ws.RLock()
	defer ws.RUnlock()
	return s.render.Properties(ws, request, element)
}

// Names are a database's entries with their colors, as the Layers panel's pickers list them.
func (s *DrawService) Names(workspaceID, database string) ([]draw.Name, error) {
	ws, err := s.workspaces.openWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	ws.RLock()
	defer ws.RUnlock()
	return draw.Names(ws, database), nil
}

// CoatFile is the file a mod's new coat of arms is written in.
func (s *DrawService) CoatFile(workspaceID, modID string) (string, error) {
	ws, err := s.workspaces.openWorkspace(workspaceID)
	if err != nil {
		return "", err
	}
	ws.RLock()
	defer ws.RUnlock()
	return draw.CoatFile(ws, modID)
}

// Frames are the frames the game cuts its coats to, as the coat toolbar's Frame picker lists them.
func (s *DrawService) Frames(workspaceID string) ([]draw.Name, error) {
	ws, err := s.workspaces.openWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	ws.RLock()
	defer ws.RUnlock()
	return draw.Frames(ws), nil
}

// Textures are the images a coat texture key's folder holds, as the Layers panel's tile picker lists them.
func (s *DrawService) Textures(workspaceID, key string) ([]draw.Name, error) {
	ws, err := s.workspaces.openWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	ws.RLock()
	defer ws.RUnlock()
	return draw.Textures(ws, key), nil
}
