package workspace

import (
	"path/filepath"
	"slices"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
)

// A Place says where a file sits in the workspace: the file as its root lists it, together with that root, its
// database, whether it is read only and whether the game loads it. Every answer that names a file carries one, so
// the frontend never works any of this out itself.
type Place struct {
	files.File
	Root     string `json:"root"`
	Database string `json:"database"`
	ReadOnly bool   `json:"readOnly"` // true for the game's own files
	Loaded   bool   `json:"loaded"`   // the game reads it
}

// Locate returns where a file sits in the workspace. The root is the mod's or the game's folder that holds it; a
// file of the game is read only; whether the game loads it comes from the index.
func (ws *Session) Locate(path string) Place {
	mods := ws.Mods.Load()
	place := Place{File: files.File{Path: path, Key: files.ToKey(path)}}
	root, ok := mods.FindRoot(path)
	if !ok {
		return place
	}
	// the root's own folder has no path from itself, and its rel stays empty
	rel, _ := files.ToRel(root.Dir, path)
	place.File = files.File{Path: path, Key: place.Key, Rel: rel, Origin: root.Origin, Format: files.Classify(rel)}
	place.Root = root.Dir
	place.ReadOnly = root.Origin == game.OriginVanilla
	if database, ok := ws.Schema.FindDatabase(rel); ok {
		place.Database = database.Name
	}
	if position, ok := mods.FindFile(path); ok {
		place.Loaded = mods.IsLoaded(position)
	}
	return place
}

// ListRoots returns the folders the explorer shows at the top: the mods in load order, then the game's.
func (ws *Session) ListRoots() []files.Root {
	roots := slices.Clone(ws.Mods.Load().Roots[len(ws.Install.Roots):])
	if position := slices.IndexFunc(ws.Install.Roots, func(root files.Root) bool {
		return filepath.Base(root.Dir) == game.ScriptRoot
	}); position >= 0 {
		roots = append(roots, ws.Install.Roots[position])
	}
	return roots
}
