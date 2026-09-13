// viewsservice.go is the Wails RPC shell for event graph and workspace health.
// It holds a pointer to SessionService and delegates to package views.

package services

import (
	"paradox-modding-tools/services/internal/lang/session"
	"paradox-modding-tools/services/internal/views"
)

// ViewsService exposes non-IDE session views over Wails.
type ViewsService struct {
	Session *SessionService
}

// GetEventGraph returns the defs-first event graph (no coordinates).
func (s *ViewsService) GetEventGraph(workspaceID string, params views.EventGraphParams) (views.EventGraph, error) {
	return withSession(s.Session, workspaceID, func(sess *session.Session) views.EventGraph {
		return views.Graph(sess, params)
	})
}

// GetEventDetail returns inspector content for one event id.
func (s *ViewsService) GetEventDetail(workspaceID, eventID string) (*views.EventDetail, error) {
	return withSession(s.Session, workspaceID, func(sess *session.Session) *views.EventDetail {
		return views.Detail(sess, eventID)
	})
}

// GetHealth returns grouped compatibility and loc rows for Workspace Health.
func (s *ViewsService) GetHealth(workspaceID string, order []string) (views.HealthReport, error) {
	return withSession(s.Session, workspaceID, func(sess *session.Session) views.HealthReport {
		return views.Health(sess, order)
	})
}

// LookupLoc returns the english loc text and site for key.
func (s *ViewsService) LookupLoc(workspaceID, key string) (*views.LocLookup, error) {
	return withSession(s.Session, workspaceID, func(sess *session.Session) *views.LocLookup {
		return views.Lookup(sess, key)
	})
}
