// ideservice.go is the Wails RPC shell for the editor: each request resolves the workspace's
// session and calls package lsp. The per-code diagnostic settings (ide.diag.<code>) are applied
// here, so a toggle takes effect on the next keystroke and the checks stay ignorant of it.
package services

import (
	"strings"

	"paradox-modding-tools/services/internal/lsp"
)

// IdeService is the editor's RPCs.
type IdeService struct {
	Session *SessionService
	Config  *ConfigService
}

// DiagnosticCodes is every check a user may turn off, with its default.
func (s *IdeService) DiagnosticCodes() []lsp.Code {
	return lsp.Codes
}

// enabled is the codes on for this user: the defaults, then the settings under ide.diag.<code>.
func (s *IdeService) enabled() map[string]bool {
	on := map[string]bool{}
	for _, code := range lsp.Codes {
		on[code.Code] = code.Default
	}
	if s.Config == nil {
		return on
	}
	settings, err := s.Config.GetSettings()
	if err != nil {
		return on
	}
	for key, value := range settings {
		if code, ok := strings.CutPrefix(key, "ide.diag."); ok {
			on[code] = value == "on"
		}
	}
	return on
}

func (s *IdeService) Diagnose(workspaceID, path string) ([]lsp.Diagnostic, error) {
	sess, err := s.Session.live(workspaceID)
	if err != nil {
		return nil, err
	}
	return lsp.Diagnose(sess, path, s.enabled())
}

func (s *IdeService) Hover(workspaceID, path string, line, col int) (*lsp.Inspect, error) {
	sess, err := s.Session.live(workspaceID)
	if err != nil {
		return nil, err
	}
	return lsp.Hover(sess, path, line, col)
}

func (s *IdeService) Complete(workspaceID, path string, line, col int) (*lsp.Completion, error) {
	sess, err := s.Session.live(workspaceID)
	if err != nil {
		return nil, err
	}
	return lsp.Complete(sess, path, line, col)
}

func (s *IdeService) ResolveCompletion(workspaceID, path string, item lsp.Item) (string, error) {
	sess, err := s.Session.live(workspaceID)
	if err != nil {
		return "", err
	}
	return lsp.Resolve(sess, path, item)
}

func (s *IdeService) SignatureHelp(workspaceID, path string, line, col int) (*lsp.Signature, error) {
	sess, err := s.Session.live(workspaceID)
	if err != nil {
		return nil, err
	}
	return lsp.SignatureHelp(sess, path, line, col)
}

func (s *IdeService) Definition(workspaceID, path string, line, col int) ([]lsp.Location, error) {
	sess, err := s.Session.live(workspaceID)
	if err != nil {
		return nil, err
	}
	return lsp.Definition(sess, path, line, col)
}

func (s *IdeService) References(workspaceID, path string, line, col int) ([]lsp.Location, error) {
	sess, err := s.Session.live(workspaceID)
	if err != nil {
		return nil, err
	}
	return lsp.References(sess, path, line, col)
}

func (s *IdeService) DocumentSymbols(workspaceID, path string) ([]lsp.Symbol, error) {
	sess, err := s.Session.live(workspaceID)
	if err != nil {
		return nil, err
	}
	return lsp.DocumentSymbols(sess, path)
}

func (s *IdeService) WorkspaceSymbols(workspaceID, query string) ([]lsp.Symbol, error) {
	sess, err := s.Session.live(workspaceID)
	if err != nil {
		return nil, err
	}
	return lsp.WorkspaceSymbols(sess, query), nil
}

func (s *IdeService) FormatDocument(workspaceID, path string) ([]lsp.TextEdit, error) {
	sess, err := s.Session.live(workspaceID)
	if err != nil {
		return nil, err
	}
	return lsp.Format(sess, path)
}

func (s *IdeService) FoldingRanges(workspaceID, path string) ([]lsp.FoldingRange, error) {
	sess, err := s.Session.live(workspaceID)
	if err != nil {
		return nil, err
	}
	return lsp.Folding(sess, path)
}

func (s *IdeService) CodeActions(workspaceID, path string, line, col int) ([]lsp.CodeAction, error) {
	sess, err := s.Session.live(workspaceID)
	if err != nil {
		return nil, err
	}
	return lsp.CodeActions(sess, path, line, col)
}

func (s *IdeService) InlayHints(workspaceID, path string) ([]lsp.InlayHint, error) {
	sess, err := s.Session.live(workspaceID)
	if err != nil {
		return nil, err
	}
	return lsp.InlayHints(sess, path)
}

// DidOpen, DidChange, DidClose and DidSave are the editor's buffer events.
func (s *IdeService) DidOpen(workspaceID, path, text string) error {
	sess, err := s.Session.live(workspaceID)
	if err != nil {
		return err
	}
	sess.DidOpen(path, text)
	return nil
}

func (s *IdeService) DidChange(workspaceID, path, text string) error {
	sess, err := s.Session.live(workspaceID)
	if err != nil {
		return err
	}
	sess.DidChange(path, text)
	return nil
}

func (s *IdeService) DidClose(workspaceID, path string) error {
	sess, err := s.Session.live(workspaceID)
	if err != nil {
		return err
	}
	sess.DidClose(path)
	return nil
}

func (s *IdeService) DidSave(workspaceID, path string) error {
	sess, err := s.Session.live(workspaceID)
	if err != nil {
		return err
	}
	sess.DidSave(path)
	return nil
}
