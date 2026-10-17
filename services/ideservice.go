// ideservice.go is the Wails RPC shell for the editor: each request opens the workspace and calls
// package lsp. The per-code diagnostic settings (ide.diag.<code>) are applied here, so a toggle takes effect on
// the next keystroke and the checks stay ignorant of it.

package services

import (
	"strconv"
	"strings"

	"paradox-modding-tools/services/internal/game"
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
func (ide *IdeService) DiagnosticCodes() []lsp.Code {
	return lsp.Codes
}

// enabled is the codes on for this user: the defaults, then each ide.diag.<code> set to on or off;
// "" is no choice, as a setting cannot be removed.
func (ide *IdeService) enabled() map[string]bool {
	enabled := map[string]bool{}
	for _, code := range lsp.Codes {
		enabled[code.Code] = code.Default
	}
	ide.config.read(func(cfg *Config) {
		for key, value := range cfg.Settings["ide"] {
			if code, ok := strings.CutPrefix(key, "diag."); ok && value != "" {
				enabled[code] = value == "on"
			}
		}
	})
	return enabled
}

// Diagnose is the findings of the checks the user has on for a file, from lsp.Diagnose.
func (ide *IdeService) Diagnose(workspaceID, path string) ([]lsp.Diagnostic, error) {
	ws, err := ide.workspaces.openWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	return lsp.Diagnose(ws, path, ide.enabled())
}

// Hover is the card for the word at a position, from lsp.Hover.
func (ide *IdeService) Hover(workspaceID, path string, line, col int) (*lsp.Inspect, error) {
	ws, err := ide.workspaces.openWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	return lsp.Hover(ws, path, line, col)
}

// Complete is what may be written at a position, from lsp.Complete.
func (ide *IdeService) Complete(workspaceID, path string, line, col int) (*lsp.Completion, error) {
	ws, err := ide.workspaces.openWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	return lsp.Complete(ws, path, line, col)
}

// ResolveCompletion is the card of one completion item, from lsp.Resolve.
func (ide *IdeService) ResolveCompletion(workspaceID, path string, item lsp.Item) (*lsp.Inspect, error) {
	ws, err := ide.workspaces.openWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	return lsp.Resolve(ws, path, item)
}

// SignatureHelp is the call being written at a position and its parameter, from lsp.SignatureHelp.
func (ide *IdeService) SignatureHelp(workspaceID, path string, line, col int) (*lsp.Signature, error) {
	ws, err := ide.workspaces.openWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	return lsp.SignatureHelp(ws, path, line, col)
}

// Definition is where the word at a position is defined, from lsp.Definition.
func (ide *IdeService) Definition(workspaceID, path string, line, col int) ([]lsp.Location, error) {
	ws, err := ide.workspaces.openWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	return lsp.Definition(ws, path, line, col)
}

// References is where the word at a position is used, from lsp.References.
func (ide *IdeService) References(workspaceID, path string, line, col int) ([]lsp.Location, error) {
	ws, err := ide.workspaces.openWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	return lsp.References(ws, path, line, col)
}

// DocumentSymbols is the definitions a file writes, from lsp.DocumentSymbols.
func (ide *IdeService) DocumentSymbols(workspaceID, path string) ([]lsp.Symbol, error) {
	ws, err := ide.workspaces.openWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	return lsp.DocumentSymbols(ws, path)
}

// WorkspaceSymbols is the definitions across the workspace that match a query, from lsp.WorkspaceSymbols.
func (ide *IdeService) WorkspaceSymbols(workspaceID, query string) ([]lsp.Symbol, error) {
	ws, err := ide.workspaces.openWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	return lsp.WorkspaceSymbols(ws, query), nil
}

// FormatDocument indents a file by depth; a script file in the settings' unit: a tab, or with
// ide.indent "spaces", ide.indentSize spaces (4 unless 1 to 8).
func (ide *IdeService) FormatDocument(workspaceID, path string) ([]lsp.TextEdit, error) {
	ws, err := ide.workspaces.openWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	unit := "\t"
	ide.config.read(func(cfg *Config) {
		if settings := cfg.Settings["ide"]; settings["indent"] == "spaces" {
			size, err := strconv.Atoi(settings["indentSize"])
			if err != nil || size < 1 || size > 8 {
				size = 4
			}
			unit = strings.Repeat(" ", size)
		}
	})
	return lsp.Format(ws, path, unit)
}

// FoldingRanges is the blocks of a file that fold, from lsp.Folding.
func (ide *IdeService) FoldingRanges(workspaceID, path string) ([]lsp.FoldingRange, error) {
	ws, err := ide.workspaces.openWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	return lsp.Folding(ws, path)
}

// CodeActions is the fixes offered at a position, from lsp.CodeActions.
func (ide *IdeService) CodeActions(workspaceID, path string, line, col int) ([]lsp.CodeAction, error) {
	ws, err := ide.workspaces.openWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	return lsp.CodeActions(ws, path, line, col)
}

// Colors is the colors a file writes, each a swatch the editor draws, from lsp.Colors.
func (ide *IdeService) Colors(workspaceID, path string) ([]lsp.Swatch, error) {
	ws, err := ide.workspaces.openWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	return lsp.Colors(ws, path)
}

// Recolor is a color block's text written with another color in its own form, from lsp.Recolor.
func (ide *IdeService) Recolor(block string, color game.RGBA) (string, error) {
	return lsp.Recolor(block, color)
}

// InlayHints is the hints drawn inside a file's text, from lsp.InlayHints.
func (ide *IdeService) InlayHints(workspaceID, path string) ([]lsp.InlayHint, error) {
	ws, err := ide.workspaces.openWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	return lsp.InlayHints(ws, path)
}

// Tokens is the keys, values and names of a file the engine colors over its grammar's colors, from lsp.Tokens.
func (ide *IdeService) Tokens(workspaceID, path string) ([]lsp.Token, error) {
	ws, err := ide.workspaces.openWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	return lsp.Tokens(ws, path)
}

// TextTokens is the keys, values and names the engine colors in another text of a file, a version of it a diff shows,
// from lsp.TextTokens.
func (ide *IdeService) TextTokens(workspaceID, path, text string) ([]lsp.Token, error) {
	ws, err := ide.workspaces.openWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	return lsp.TextTokens(ws, path, text), nil
}

// DidChange is the editor's buffer event for an opened or changed file: an opened buffer is a change.
func (ide *IdeService) DidChange(workspaceID, path, text string) error {
	ws, err := ide.workspaces.openWorkspace(workspaceID)
	if err != nil {
		return err
	}
	ws.DidChange(path, text)
	return nil
}

// DidClose is the editor's buffer event for a closed file: its text is forgotten and the disk's is read again.
func (ide *IdeService) DidClose(workspaceID, path string) error {
	ws, err := ide.workspaces.openWorkspace(workspaceID)
	if err != nil {
		return err
	}
	ws.DidClose(path)
	return nil
}

// DidSave is the editor's buffer event for a saved file: the saved text is indexed again.
func (ide *IdeService) DidSave(workspaceID, path string) error {
	ws, err := ide.workspaces.openWorkspace(workspaceID)
	if err != nil {
		return err
	}
	ws.DidSave(path)
	return nil
}

// Rename is the edits that rename the name at a place everywhere the mods write it, or why it cannot (F2).
func (ide *IdeService) Rename(workspaceID, path string, line, col int, name string) (*lsp.WorkspaceEdit, error) {
	ws, err := ide.workspaces.openWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	edit, err := lsp.Rename(ws, path, line, col, name)
	return &edit, err
}

// FindLoc is the localization keys that start with a prefix, at most limit, each at its entry (the palette's #).
func (ide *IdeService) FindLoc(workspaceID, prefix string, limit int) ([]lsp.Symbol, error) {
	ws, err := ide.workspaces.openWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	return lsp.FindLoc(ws, prefix, limit), nil
}

// ReplacedBy is the mods' files at a game file's path from its root, in load order: the copies the game loads in
// its place.
func (ide *IdeService) ReplacedBy(workspaceID, path string) ([]inventory.File, error) {
	ws, err := ide.workspaces.openWorkspace(workspaceID)
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
