// ideservice.go is the Wails RPC shell for Monaco LSP. It holds a pointer to
// SessionService and delegates to package lsp.

package services

import (
	"paradox-modding-tools/services/internal/lsp"
	"paradox-modding-tools/services/internal/session"
	"strings"
)

// IdeService exposes editor Did* and LSP methods over Wails.
type IdeService struct {
	Session *SessionService
	// Settings supplies per-diagnostic severities. Optional: a nil Settings
	// means every check reports at its own default, which is what the tests and
	// any embedder that does not care about configuration want.
	Settings *SettingsService
}

// DidOpen syncs an editor buffer into the session.
func (s *IdeService) DidOpen(workspaceID, path, text string) error {
	return withSessionDo(s.Session, workspaceID, func(sess *session.Session) {
		sess.DidOpen(path, text)
	})
}

// DidChange syncs buffer text into the session.
func (s *IdeService) DidChange(workspaceID, path, text string) error {
	return withSessionDo(s.Session, workspaceID, func(sess *session.Session) {
		sess.DidChange(path, text)
	})
}

// DidClose drops an editor buffer overlay.
func (s *IdeService) DidClose(workspaceID, path string) error {
	if live := s.Session.pool().Get(workspaceID); live != nil {
		live.DidClose(path)
	}
	return nil
}

// DidSave reindexes path from its buffer or disk.
func (s *IdeService) DidSave(workspaceID, path string) error {
	return withSessionDo(s.Session, workspaceID, func(sess *session.Session) {
		sess.DidSave(path)
	})
}

// Diagnose returns diagnostics for a file, with the user's per-code severities
// applied.
//
// Severities are applied here rather than inside Diagnose so a toggle takes
// effect on the next keystroke without a rescan, and so the checks themselves
// stay ignorant of configuration.
func (s *IdeService) Diagnose(workspaceID, path string) ([]lsp.Diagnostic, error) {
	return withSession(s.Session, workspaceID, func(sess *session.Session) []lsp.Diagnostic {
		return lsp.ApplySeverities(lsp.Diagnose(sess, path), s.diagSeverities())
	})
}

// InlayHints returns the era form of each date literal, for a mod that declares
// a calendar. Empty for every mod that does not, which is nearly all of them.
func (s *IdeService) InlayHints(workspaceID, path string) ([]lsp.InlayHint, error) {
	return withSession(s.Session, workspaceID, func(sess *session.Session) []lsp.InlayHint {
		return lsp.InlayHints(sess, path)
	})
}

// DiagnosticCodes lists every check, for the IDE settings surface.
func (s *IdeService) DiagnosticCodes() ([]lsp.DiagnosticCode, error) {
	return lsp.DiagnosticCodes(), nil
}

// diagSeverities reads the "ide" settings namespace, where each diagnostic code
// is a key whose value is off / warning / error.
func (s *IdeService) diagSeverities() map[string]string {
	out := map[string]string{}
	if s.Settings == nil {
		return out
	}
	all, err := s.Settings.GetSettings()
	if err != nil {
		return out
	}
	const prefix = "ide.diag."
	for k, v := range all {
		if code, ok := strings.CutPrefix(k, prefix); ok {
			out[code] = v
		}
	}
	return out
}

// Hover returns hover info at a 0-based UTF-8 position.
func (s *IdeService) Hover(workspaceID, path string, line, character int) (*lsp.HoverResult, error) {
	return withSession(s.Session, workspaceID, func(sess *session.Session) *lsp.HoverResult {
		return lsp.Hover(sess, path, line, character)
	})
}

// Complete returns completion items at a 0-based UTF-8 position.
func (s *IdeService) Complete(workspaceID, path string, line, character int) ([]lsp.CompletionItem, error) {
	return withSession(s.Session, workspaceID, func(sess *session.Session) []lsp.CompletionItem {
		return lsp.Complete(sess, path, line, character)
	})
}

// SignatureHelp returns usage-based signature help at a 0-based UTF-8 position.
func (s *IdeService) SignatureHelp(workspaceID, path string, line, character int) (*lsp.SignatureHelpResult, error) {
	return withSession(s.Session, workspaceID, func(sess *session.Session) *lsp.SignatureHelpResult {
		return lsp.SignatureHelp(sess, path, line, character)
	})
}

// Definition returns go-to-definition locations.
func (s *IdeService) Definition(workspaceID, path string, line, character int) ([]lsp.Location, error) {
	return withSession(s.Session, workspaceID, func(sess *session.Session) []lsp.Location {
		return lsp.Definition(sess, path, line, character)
	})
}

// References returns reference locations.
func (s *IdeService) References(workspaceID, path string, line, character int) ([]lsp.Location, error) {
	return withSession(s.Session, workspaceID, func(sess *session.Session) []lsp.Location {
		return lsp.References(sess, path, line, character)
	})
}

// Rename returns a workspace edit for the identifier at pos.
func (s *IdeService) Rename(workspaceID, path string, line, character int, newName string) (*lsp.WorkspaceEdit, error) {
	return withSession(s.Session, workspaceID, func(sess *session.Session) *lsp.WorkspaceEdit {
		return lsp.Rename(sess, path, line, character, newName)
	})
}

// FormatDocument returns conservative format edits.
func (s *IdeService) FormatDocument(workspaceID, path string) ([]lsp.TextEdit, error) {
	return withSession(s.Session, workspaceID, func(sess *session.Session) []lsp.TextEdit {
		return lsp.FormatDocument(sess, path)
	})
}

// FoldingRanges returns foldable block ranges.
func (s *IdeService) FoldingRanges(workspaceID, path string) ([]lsp.FoldingRange, error) {
	return withSession(s.Session, workspaceID, func(sess *session.Session) []lsp.FoldingRange {
		return lsp.FoldingRanges(sess, path)
	})
}

// CodeActions returns quick-fixes for the file.
func (s *IdeService) CodeActions(workspaceID, path string) ([]lsp.CodeAction, error) {
	return withSession(s.Session, workspaceID, func(sess *session.Session) []lsp.CodeAction {
		return lsp.CodeActions(sess, path)
	})
}

// DocumentSymbols returns top-level symbols in a file.
func (s *IdeService) DocumentSymbols(workspaceID, path string) ([]lsp.SymbolInformation, error) {
	return withSession(s.Session, workspaceID, func(sess *session.Session) []lsp.SymbolInformation {
		return lsp.DocumentSymbols(sess, path)
	})
}

// WorkspaceSymbols searches definitions in the session.
func (s *IdeService) WorkspaceSymbols(workspaceID, query string) ([]lsp.SymbolInformation, error) {
	return withSession(s.Session, workspaceID, func(sess *session.Session) []lsp.SymbolInformation {
		return lsp.WorkspaceSymbols(sess, query)
	})
}
