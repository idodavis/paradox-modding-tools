// locate.go says where a file sits in the workspace: the root it is under and its path relative to that root. It also
// lists a file's copies and the folders the workspace's files are listed from.

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
	ReadOnly bool   `json:"readOnly"` // true for a file no change is written to: the game's own, and a Workshop mod's
	Position int32  `json:"-"`        // where the index lists the file; -1 where it does not
	IsDir    bool   `json:"isDir"`    // true for a folder: a root, an entry of a folder's listing, or git's or an agent's
	Drawable bool   `json:"drawable"` // true for a GUI file and for the coats' files and folder, which Visual draws
	About    string `json:"about"`    // the first paragraph of the game's readme for its database; "" where it has none
	Readme   string `json:"readme"`   // the full path of that readme; "" where it has none
}

// Locate returns the place of the file with the given path, as the given index of the mods lists its files. A path
// under none of the roots gives a place with only the path and its key set. A file of the game or of a mod in Steam's
// Workshop folder is read only. A GUI file is drawable, and so is a file or folder of the coats' database. A file in a
// database the game documents has the first paragraph of that readme and its path. A file the index does not list has
// the position -1.
func (ws *Session) Locate(mods *index.Index, path string) Place {
	place := Place{File: files.File{Path: path, Key: files.ToKey(path)}, Name: filepath.Base(path), Position: -1}
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
	place.ReadOnly = root.Origin == game.OriginVanilla ||
		lo.ContainsBy(*ws.Order.Load(), func(mod Mod) bool { return mod.ID == root.Origin && mod.Workshop })
	if database, ok := ws.Schema.FindDatabase(rel); ok {
		place.Database, place.About = database.Name, database.Definition.Blurb.Value
		if database.Readme != "" {
			place.Readme = files.Join(ws.InstallPath, database.Readme)
		}
	}
	place.Drawable = place.Format == files.FormatGUI || place.Database == game.Coat.Database
	if position, ok := mods.FindFile(path); ok {
		place.Position = position
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

// ListRoots returns the place of each folder the workspace's files are listed from: the mods' in load order, then the
// game's own folder. The install's other folders are left out. It returns none if no workspace is open.
func (wsp *Service) ListRoots() []Place {
	session, err := wsp.Session()
	if err != nil {
		return nil
	}
	roots := slices.Clone(session.Mods.Load().Roots[len(session.Install.Roots):])
	if position := slices.IndexFunc(session.Install.Roots, func(root files.Root) bool {
		return filepath.Base(root.Dir) == game.ScriptRoot
	}); position >= 0 {
		roots = append(roots, session.Install.Roots[position])
	}
	return lo.Map(roots, func(root files.Root, _ int) Place { return wsp.Locate(root.Dir) })
}
