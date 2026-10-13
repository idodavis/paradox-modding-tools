// drawservice.go is the visual editors' service: a GUI file's window as the flat list Go resolves, the facts a
// binding asks that the user answers once per workspace, and the open workspace's read lock for each call.

package services

import (
	"paradox-modding-tools/services/internal/draw"
	"paradox-modding-tools/services/internal/lsp"
)

// DrawService answers the Visual view of a file.
type DrawService struct {
	workspaces *WorkspaceService
	config     *ConfigFile
}

// NewDrawService is the Visual view's service over the open workspace and the saved facts.
func NewDrawService(workspaces *WorkspaceService, config *ConfigFile) *DrawService {
	return &DrawService{workspaces: workspaces, config: config}
}

// GUI is a GUI file's window drawn as a list at the request's resolution, scale and language, over the workspace's
// facts.
func (s *DrawService) GUI(workspaceID string, request draw.Request) (*draw.List, error) {
	ws, err := s.workspaces.openWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	facts, err := s.facts(workspaceID)
	if err != nil {
		return nil, err
	}
	ws.RLock()
	defer ws.RUnlock()
	return draw.GUI(ws, request, facts)
}

// Properties is a part's properties as the Widgets panel lists them: each key's last value, who wrote it and the
// choices its input offers.
func (s *DrawService) Properties(workspaceID string, request draw.Request, part int) ([]draw.Property, error) {
	ws, err := s.workspaces.openWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	facts, err := s.facts(workspaceID)
	if err != nil {
		return nil, err
	}
	ws.RLock()
	defer ws.RUnlock()
	return draw.Properties(ws, request, facts, part)
}

// Edit is the text edits one change from the Widgets panel makes, by file; the page applies them through the editor.
func (s *DrawService) Edit(workspaceID string, request draw.Request, change draw.Change) ([]lsp.TextEdit, error) {
	ws, err := s.workspaces.openWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	facts, err := s.facts(workspaceID)
	if err != nil {
		return nil, err
	}
	ws.RLock()
	defer ws.RUnlock()
	return draw.Edit(ws, request, facts, change)
}

// SetFact answers a binding's question for the workspace, or takes the answer back where the fact is not Known.
func (s *DrawService) SetFact(workspaceID string, fact draw.Fact) error {
	key := draw.FactKey(fact)
	return s.workspaces.mutateWorkspace(workspaceID, func(entry *Workspace) error {
		if !fact.Known {
			delete(entry.Facts, key)
			return nil
		}
		if entry.Facts == nil {
			entry.Facts = map[string]string{}
		}
		entry.Facts[key] = fact.Value
		return nil
	})
}

// facts is a copy of the workspace's answers, by fact key.
func (s *DrawService) facts(workspaceID string) (map[string]string, error) {
	entry, _, err := s.config.workspace(workspaceID)
	if err != nil {
		return nil, err
	}
	return entry.Facts, nil
}
