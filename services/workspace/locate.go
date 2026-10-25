package workspace

import (
	"path/filepath"
	"slices"
	"strings"

	"github.com/samber/lo"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
)

// A Place is where one file sits in the workspace: the file as its root lists it, with the root it is under and what
// the workspace knows about it. Every answer that names a file carries one.
type Place struct {
	files.File
	Name     string `json:"name"`
	Folder   string `json:"folder"` // "" for a file at its root
	Root     string `json:"root"`
	Database string `json:"database"`
	ReadOnly bool   `json:"readOnly"` // true for the game's own files
	Loaded   bool   `json:"loaded"`   // the game reads it
}

// Locate returns the place of the file with the given path, as the given index of the mods lists its files. A path
// under none of the roots gives a place with only the path and its key set. A file of the game is read only.
func (ws *Session) Locate(mods *index.Index, path string) Place {
	place := Place{File: files.File{Path: path, Key: files.ToKey(path)}, Name: filepath.Base(path)}
	root, ok := mods.FindRoot(path)
	if !ok {
		return place
	}
	// the root's own folder has no path from itself, and its rel stays empty
	rel, _ := files.ToRel(root.Dir, path)
	place.File = files.File{Path: path, Key: place.Key, Rel: rel, Origin: root.Origin, Format: files.Classify(rel)}
	if cut := strings.LastIndexByte(rel, '/'); cut >= 0 {
		place.Folder = rel[:cut]
	}
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

// ListCopies returns every copy of a file in load order, the given one too, the game's first. The game reads the last,
// unless a replace_path drops them all. It returns none for a file under no root or while no workspace is open.
func (wsp *Service) ListCopies(path string) []Place {
	session, err := wsp.Session()
	if err != nil {
		return nil
	}
	mods := session.Mods.Load()
	loaded, copies, ok := mods.FindLoaded(session.Locate(mods, path).Rel)
	if ok {
		copies = append(slices.Clip(copies), loaded)
	}
	return lo.Map(copies, func(file files.File, _ int) Place { return session.Locate(mods, file.Path) })
}

// ListRoots returns the folders the workspace's files are listed from: the mods' in load order, then the game's own
// folder. The install's other folders are left out.
func (ws *Session) ListRoots() []files.Root {
	roots := slices.Clone(ws.Mods.Load().Roots[len(ws.Install.Roots):])
	if position := slices.IndexFunc(ws.Install.Roots, func(root files.Root) bool {
		return filepath.Base(root.Dir) == game.ScriptRoot
	}); position >= 0 {
		roots = append(roots, ws.Install.Roots[position])
	}
	return roots
}
