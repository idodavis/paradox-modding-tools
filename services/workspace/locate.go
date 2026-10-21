package workspace

import "paradox-modding-tools/internal/files"

// A Place says where a file sits in the workspace: its path and key, its root and origin, its path relative to the
// root, its format, its database, whether it is read only and whether the game loads it. Every answer that names
// a file carries one, so the frontend never works any of this out itself.
type Place struct {
	Path     string       `json:"path"`
	Key      string       `json:"key"`
	Root     string       `json:"root"`
	Origin   string       `json:"origin"`
	Rel      string       `json:"rel"`
	Format   files.Format `json:"format"`
	Database string       `json:"database"`
	ReadOnly bool         `json:"readOnly"` // true for the game's own files
	Loaded   bool         `json:"loaded"`   // the game reads it
}

// Locate returns where a file sits in the workspace. The root is the mod's or the game's folder that holds it; a
// file of the game is read only; whether the game loads it comes from the index.
func (ws *Session) Locate(path string) Place { panic("stub") }

// ListRoots returns the folders the explorer shows at the top: the mods in load order, then the game's.
func (ws *Session) ListRoots() []files.Root { panic("stub") }
