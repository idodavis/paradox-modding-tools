// eventgraphservice.go is the event graph page's service: the open workspace and three calls into pages.

package services

import "paradox-modding-tools/services/internal/pages"

// EventGraphService answers the event graph page.
type EventGraphService struct {
	workspaces *WorkspaceService
}

// NewEventGraphService is the event graph page's service over the open workspace.
func NewEventGraphService(workspaces *WorkspaceService) *EventGraphService {
	return &EventGraphService{workspaces: workspaces}
}

// SearchRoots is the events and on_actions the game fires, and what fires one, whose id holds the query.
func (evg *EventGraphService) SearchRoots(workspaceID, query string) (*pages.Roots, error) {
	ws, err := evg.workspaces.openWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	return pages.SearchRoots(ws, query), nil
}

// GetEventGraph is the graph around a root, as the page's numbers draw it.
func (evg *EventGraphService) GetEventGraph(workspaceID string, params pages.Params) (*pages.EventGraph, error) {
	ws, err := evg.workspaces.openWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	return pages.Graph(ws, params), nil
}

// GetEventDetail is one definition opened: where it is, its keys, its options, what it fires and what fires it.
func (evg *EventGraphService) GetEventDetail(workspaceID, id string) (*pages.Detail, error) {
	ws, err := evg.workspaces.openWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	return pages.EventDetail(ws, id), nil
}
