// viewsservice.go is the Wails RPC shell for the three views: each resolves the workspace's session and calls package views.
package services

import (
	"paradox-modding-tools/services/internal/views"
)

// ViewsService is the views' RPCs.
type ViewsService struct {
	Session *SessionService
}

func (s *ViewsService) GetEventGraph(workspaceID string, p views.Params) (*views.EventGraph, error) {
	sess, err := s.Session.live(workspaceID)
	if err != nil {
		return nil, err
	}
	g := views.Graph(sess, p)
	return &g, nil
}

func (s *ViewsService) GetEventDetail(workspaceID, id string) (*views.Detail, error) {
	sess, err := s.Session.live(workspaceID)
	if err != nil {
		return nil, err
	}
	return views.EventDetail(sess, id), nil
}

func (s *ViewsService) GetHealth(workspaceID string, p views.HealthParams) (*views.Health, error) {
	sess, err := s.Session.live(workspaceID)
	if err != nil {
		return nil, err
	}
	h := views.WorkspaceHealth(sess, p)
	return &h, nil
}

func (s *ViewsService) LookupLoc(workspaceID, key string) (*views.Lookup, error) {
	sess, err := s.Session.live(workspaceID)
	if err != nil {
		return nil, err
	}
	l := views.LookupLoc(sess, key)
	return &l, nil
}
