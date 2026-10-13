// drawservice.go is the visual editors' service: a GUI file's entries and the list of elements Go resolves one into, under
// the open workspace's read lock for each call.

package services

import (
	"paradox-modding-tools/services/internal/draw"
	"paradox-modding-tools/services/internal/lsp"
)

// DrawService answers the Visual view of a file.
type DrawService struct {
	workspaces *WorkspaceService
}

// NewDrawService is the Visual view's service over the open workspace.
func NewDrawService(workspaces *WorkspaceService) *DrawService {
	return &DrawService{workspaces: workspaces}
}

// Entries is what a GUI file can draw: its windows, types, templates, text icons and formats, in file order.
func (s *DrawService) Entries(workspaceID, path string) ([]lsp.Symbol, error) {
	ws, err := s.workspaces.openWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	return draw.Entries(ws, path)
}

// GUI is a GUI file's entry drawn as a list at the request's resolution, scale and language.
func (s *DrawService) GUI(workspaceID string, request draw.Request) (*draw.List, error) {
	ws, err := s.workspaces.openWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	ws.RLock()
	defer ws.RUnlock()
	return draw.GUI(ws, request)
}

// Properties is an element's properties as the Widgets panel lists them: each key's last value and who wrote it.
func (s *DrawService) Properties(workspaceID string, request draw.Request, element int) ([]draw.Property, error) {
	ws, err := s.workspaces.openWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	ws.RLock()
	defer ws.RUnlock()
	return draw.Properties(ws, request, element)
}
