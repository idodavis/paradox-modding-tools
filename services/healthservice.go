// healthservice.go is the Health page's service: the open workspace, the editor's findings kept across
// requests, and three calls into pages.

package services

import "paradox-modding-tools/services/internal/pages"

// HealthService answers the Health page.
type HealthService struct {
	workspaces *WorkspaceService
	findings   pages.Findings
}

// NewHealthService is the Health page's service over the open workspace.
func NewHealthService(workspaces *WorkspaceService) *HealthService {
	return &HealthService{workspaces: workspaces}
}

// GetHealth is the workspace's health report: its contests and overrides in the load order asked for, what each
// mod depends on, the names nothing defines, each language's gaps and the editor's other findings.
func (s *HealthService) GetHealth(workspaceID string, params pages.HealthParams) (*pages.HealthReport, error) {
	ws, err := s.workspaces.openWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	findings := s.findings.Current(ws)
	return pages.Health(ws, params, findings), nil
}

// GetLines is the source a pane shows at each place: a definition's statement, or a reference's line.
func (s *HealthService) GetLines(workspaceID string, places []pages.SourceAt) ([][]string, error) {
	ws, err := s.workspaces.openWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	return pages.Lines(ws, places), nil
}

// LocReaders is everything that reads one localization key.
func (s *HealthService) LocReaders(workspaceID, key string) ([]pages.Cited, error) {
	ws, err := s.workspaces.openWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	return pages.LocReaders(ws, key), nil
}
