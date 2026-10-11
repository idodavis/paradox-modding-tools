// eventgraphservice.go is the event graph page's service: the session and three calls into pages.

package services

import "paradox-modding-tools/services/internal/pages"

// EventGraphService answers the event graph page.
type EventGraphService struct {
	sessions *SessionService
}

// NewEventGraphService is the event graph page's service over the live sessions.
func NewEventGraphService(sessions *SessionService) *EventGraphService {
	return &EventGraphService{sessions: sessions}
}

// SearchRoots is the events and on_actions the game fires, and what fires one, whose id holds the query.
func (s *EventGraphService) SearchRoots(workspaceID, query string) (*pages.Roots, error) {
	ws, err := s.sessions.openSession(workspaceID)
	if err != nil {
		return nil, err
	}
	ws.RLock()
	defer ws.RUnlock()
	return pages.SearchRoots(ws, query), nil
}

// GetEventGraph is the graph around a root, as the page's numbers draw it.
func (s *EventGraphService) GetEventGraph(workspaceID string, params pages.Params) (*pages.EventGraph, error) {
	ws, err := s.sessions.openSession(workspaceID)
	if err != nil {
		return nil, err
	}
	ws.RLock()
	defer ws.RUnlock()
	return pages.Graph(ws, params), nil
}

// GetEventDetail is one definition opened: where it is, its keys, its options, what it fires and what fires it.
func (s *EventGraphService) GetEventDetail(workspaceID, id string) (*pages.Detail, error) {
	ws, err := s.sessions.openSession(workspaceID)
	if err != nil {
		return nil, err
	}
	ws.RLock()
	defer ws.RUnlock()
	return pages.EventDetail(ws, id), nil
}
