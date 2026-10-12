// ideservice.go is the Wails RPC shell for the editor: each request opens the workspace and calls
// package lsp. The per-code diagnostic settings (ide.diag.<code>) are applied here, so a toggle takes effect on
// the next keystroke and the checks stay ignorant of it.

package services

import (
	"strconv"
	"strings"

	"paradox-modding-tools/services/internal/inventory"
	"paradox-modding-tools/services/internal/lsp"
)

// IdeService is the editor's RPCs.
type IdeService struct {
	config     *ConfigFile
	workspaces *WorkspaceService
}

// NewIdeService is the editor's service over the config and the open workspace.
func NewIdeService(config *ConfigFile, workspaces *WorkspaceService) *IdeService {
	return &IdeService{config: config, workspaces: workspaces}
}

// DiagnosticCodes is every check a user may turn off, with its default.
func (s *IdeService) DiagnosticCodes() []lsp.Code {
	return lsp.Codes
}

// enabled is the codes on for this user: the defaults, then each ide.diag.<code> set to on or off;
// "" is no choice, as a setting cannot be removed.
func (s *IdeService) enabled() map[string]bool {
	enabled := map[string]bool{}
	for _, code := range lsp.Codes {
		enabled[code.Code] = code.Default
	}
	s.config.read(func(cfg *Config) {
		for key, value := range cfg.Settings["ide"] {
			if code, ok := strings.CutPrefix(key, "diag."); ok && value != "" {
				enabled[code] = value == "on"
			}
		}
	})
	return enabled
}

// Diagnose is the findings of the checks the user has on for a file, from lsp.Diagnose.
func (s *IdeService) Diagnose(workspaceID, path string) ([]lsp.Diagnostic, error) {
	ws, err := s.workspaces.openWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	return lsp.Diagnose(ws, path, s.enabled())
}

// Hover is the card for the word at a position, from lsp.Hover.
func (s *IdeService) Hover(workspaceID, path string, line, col int) (*lsp.Inspect, error) {
	ws, err := s.workspaces.openWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	return lsp.Hover(ws, path, line, col)
}

// Complete is what may be written at a position, from lsp.Complete.
func (s *IdeService) Complete(workspaceID, path string, line, col int) (*lsp.Completion, error) {
	ws, err := s.workspaces.openWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	return lsp.Complete(ws, path, line, col)
}

// ResolveCompletion is the card of one completion item, from lsp.Resolve.
func (s *IdeService) ResolveCompletion(workspaceID, path string, item lsp.Item) (*lsp.Inspect, error) {
	ws, err := s.workspaces.openWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	return lsp.Resolve(ws, path, item)
}

// SignatureHelp is the call being written at a position and its parameter, from lsp.SignatureHelp.
func (s *IdeService) SignatureHelp(workspaceID, path string, line, col int) (*lsp.Signature, error) {
	ws, err := s.workspaces.openWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	return lsp.SignatureHelp(ws, path, line, col)
}

// Definition is where the word at a position is defined, from lsp.Definition.
func (s *IdeService) Definition(workspaceID, path string, line, col int) ([]lsp.Location, error) {
	ws, err := s.workspaces.openWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	return lsp.Definition(ws, path, line, col)
}

// References is where the word at a position is used, from lsp.References.
func (s *IdeService) References(workspaceID, path string, line, col int) ([]lsp.Location, error) {
	ws, err := s.workspaces.openWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	return lsp.References(ws, path, line, col)
}

// DocumentSymbols is the definitions a file writes, from lsp.DocumentSymbols.
func (s *IdeService) DocumentSymbols(workspaceID, path string) ([]lsp.Symbol, error) {
	ws, err := s.workspaces.openWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	return lsp.DocumentSymbols(ws, path)
}

// WorkspaceSymbols is the definitions across the workspace that match a query, from lsp.WorkspaceSymbols.
func (s *IdeService) WorkspaceSymbols(workspaceID, query string) ([]lsp.Symbol, error) {
	ws, err := s.workspaces.openWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	return lsp.WorkspaceSymbols(ws, query), nil
}

// FormatDocument indents a file by depth; a script file in the settings' unit: a tab, or with
// ide.indent "spaces", ide.indentSize spaces (4 unless 1 to 8).
func (s *IdeService) FormatDocument(workspaceID, path string) ([]lsp.TextEdit, error) {
	ws, err := s.workspaces.openWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	unit := "\t"
	s.config.read(func(cfg *Config) {
		if ide := cfg.Settings["ide"]; ide["indent"] == "spaces" {
			size, err := strconv.Atoi(ide["indentSize"])
			if err != nil || size < 1 || size > 8 {
				size = 4
			}
			unit = strings.Repeat(" ", size)
		}
	})
	return lsp.Format(ws, path, unit)
}

// FoldingRanges is the blocks of a file that fold, from lsp.Folding.
func (s *IdeService) FoldingRanges(workspaceID, path string) ([]lsp.FoldingRange, error) {
	ws, err := s.workspaces.openWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	return lsp.Folding(ws, path)
}

// CodeActions is the fixes offered at a position, from lsp.CodeActions.
func (s *IdeService) CodeActions(workspaceID, path string, line, col int) ([]lsp.CodeAction, error) {
	ws, err := s.workspaces.openWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	return lsp.CodeActions(ws, path, line, col)
}

// InlayHints is the hints drawn inside a file's text, from lsp.InlayHints.
func (s *IdeService) InlayHints(workspaceID, path string) ([]lsp.InlayHint, error) {
	ws, err := s.workspaces.openWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	return lsp.InlayHints(ws, path)
}

// DidChange is the editor's buffer event for an opened or changed file: an opened buffer is a change.
func (s *IdeService) DidChange(workspaceID, path, text string) error {
	ws, err := s.workspaces.openWorkspace(workspaceID)
	if err != nil {
		return err
	}
	ws.DidChange(path, text)
	return nil
}

// DidClose is the editor's buffer event for a closed file: its text is forgotten and the disk's is read again.
func (s *IdeService) DidClose(workspaceID, path string) error {
	ws, err := s.workspaces.openWorkspace(workspaceID)
	if err != nil {
		return err
	}
	ws.DidClose(path)
	return nil
}

// DidSave is the editor's buffer event for a saved file: the saved text is indexed again.
func (s *IdeService) DidSave(workspaceID, path string) error {
	ws, err := s.workspaces.openWorkspace(workspaceID)
	if err != nil {
		return err
	}
	ws.DidSave(path)
	return nil
}

// Rename is the edits that rename the name at a place everywhere the mods write it, or why it cannot (F2).
func (s *IdeService) Rename(workspaceID, path string, line, col int, name string) (*lsp.WorkspaceEdit, error) {
	ws, err := s.workspaces.openWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	edit, err := lsp.Rename(ws, path, line, col, name)
	return &edit, err
}

// FindLoc is the localization keys that start with a prefix, at most limit, each at its entry (the palette's #).
func (s *IdeService) FindLoc(workspaceID, prefix string, limit int) ([]lsp.Symbol, error) {
	ws, err := s.workspaces.openWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	return lsp.FindLoc(ws, prefix, limit), nil
}

// ReplacedBy is the mods' files at a game file's path from its root, in load order: the copies the game loads in
// its place.
func (s *IdeService) ReplacedBy(workspaceID, path string) ([]inventory.File, error) {
	ws, err := s.workspaces.openWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	ws.RLock()
	defer ws.RUnlock()
	file, mod, known := ws.Locate(path)
	if !known || mod != nil {
		return nil, nil
	}
	var copies []inventory.File
	for _, other := range ws.Workspace.Files {
		if strings.EqualFold(other.Rel, file.Rel) {
			copies = append(copies, other)
		}
	}
	return copies, nil
}
