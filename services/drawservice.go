// drawservice.go is the visual editors' service: the open workspace, the last render kept across requests, and its two
// calls, each under the workspace's read lock.

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
