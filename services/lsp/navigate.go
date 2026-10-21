package lsp

import (
	"paradox-modding-tools/internal/schema"
	"paradox-modding-tools/services/workspace"
)

// A Symbol is one definition in a file as the outline shows it, with the symbols nested inside it.
type Symbol struct {
	Name      string          `json:"name"`
	Database  string          `json:"database"`
	Range     Range           `json:"range"`     // the whole statement
	NameRange Range           `json:"nameRange"` // the name alone
	Children  []Symbol        `json:"children,omitempty"`
	Category  schema.Category `json:"category"` // for the icon
}

// A WorkspaceSymbol is one definition found by the symbol search across the workspace.
type WorkspaceSymbol struct {
	Name     string          `json:"name"`
	Database string          `json:"database"`
	Location Location        `json:"location"`
	Category schema.Category `json:"category"`
}

// Definition returns where the name at a position is defined, the definition the game keeps first. For a written
// file path it returns the file itself.
func (lsp *Service) Definition(path string, line, column int) ([]Location, error) { panic("stub") }

// References returns every use of the name at a position.
func (lsp *Service) References(path string, line, column int) ([]Location, error) { panic("stub") }

// PrepareRename returns the range of the name at a position, or nil if the position is not on a name that a mod
// defines.
func (lsp *Service) PrepareRename(path string, line, column int) (*Range, error) { panic("stub") }

// Rename returns the edits that give the name at a position a new name, grouped by the path of each mod file that
// defines or uses it.
func (lsp *Service) Rename(path string, line, column int, name string) (map[string][]Edit, error) {
	panic("stub")
}

// ListSymbols returns a file's definitions for the outline, nested as they are written.
func (lsp *Service) ListSymbols(path string) ([]Symbol, error) { panic("stub") }

// FindSymbols returns the definitions whose name contains the query, the mods' first, up to the given limit.
func (lsp *Service) FindSymbols(query string, limit int) ([]WorkspaceSymbol, error) { panic("stub") }

// locate returns the location of a line in a file.
func locate(ws *workspace.Session, place workspace.Place, line int) Location { panic("stub") }
