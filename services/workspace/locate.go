// locate.go says where a file sits in the workspace. It also lists a file's copies and the workspace's roots.

package workspace

import (
	"cmp"
	"path/filepath"
	"slices"
	"strings"

	"github.com/samber/lo"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
)

// A Place is where one file sits in the workspace, with the root it is under and what the workspace knows about it.
// Every answer that names a file carries one.
type Place struct {
	files.File
	Name     string `json:"name"`
	Folder   string `json:"folder"` // "" for a file at its root
	Root     string `json:"root"`
	Database string `json:"database"`
	ReadOnly bool   `json:"readOnly"` // true for the game's files and a Workshop mod's
	Position int32  `json:"-"`        // where the index lists the file; -1 if it does not
	IsDir    bool   `json:"isDir"`    // true for a folder
	Drawable bool   `json:"drawable"` // true for a file Visual draws
	Readme   string `json:"readme"`   // the path of the game's readme/info for its database; "" if none

	// Language is the editor language the file is shown in, plain text for a
	// format that has none of its own.
	Language string `json:"language"`

	// Hiding is why the game does not read the file, "" for a file it reads.
	// Hider is the id of the mod that hides it, "" where no mod does.
	Hiding files.Hiding `json:"hiding"`
	Hider  string       `json:"hider"`
}

// plainLanguage is the editor language of a file whose format has none of its
// own, and scriptLanguage that of a file of plain script.
const (
	plainLanguage  = "plaintext"
	scriptLanguage = "paradox-script"
)

// editorLanguages holds the editor language of each format that has one.
var editorLanguages = map[files.Format]string{
	files.FormatScript: scriptLanguage, files.FormatAsset: scriptLanguage, files.FormatFont: scriptLanguage,
	files.FormatGFX: scriptLanguage, files.FormatGUI: "paradox-gui", files.FormatLocalization: "paradox-localization",
	files.FormatReadme: "paradox-readme", files.FormatMarkdown: "markdown",
}

// Locate returns the place of the file with the given path, found in the given index of the mods. A path under none
// of the roots gives a place with only its path, key and name.
func (ws *Session) Locate(mods *index.Index, path string) Place {
	place := Place{
		File: files.File{Path: path, Key: files.ToKey(path)}, Name: filepath.Base(path), Position: -1, Language: plainLanguage,
	}
	root, ok := mods.FindRoot(path)
	if !ok {
		return place
	}
	// a root's own folder has no rel
	place.File, _ = files.NewFile(root, path)
	place.Language = cmp.Or(editorLanguages[place.Format], plainLanguage)
	if cut := strings.LastIndexByte(place.Rel, '/'); cut >= 0 {
		place.Folder = place.Rel[:cut]
	}
	place.Root = root.Dir
	place.ReadOnly = root.Origin == files.OriginVanilla ||
		lo.ContainsBy(*ws.Order.Load(), func(mod Mod) bool { return mod.ID == root.Origin && mod.Workshop })
	if database, ok := ws.Schema.FindDatabase(place.File); ok {
		place.Database = database.Name
		if database.Readme != "" {
			place.Readme = files.Join(ws.InstallPath, database.Readme)
		}
	}
	place.Drawable = place.Format == files.FormatGUI || place.Database == game.Coat.Database
	if position, ok := mods.FindFile(path); ok {
		hiding, hider := mods.FindHiding(position)
		place.Position, place.Hiding, place.Hider = position, hiding, hider.Origin
	}
	return place
}

// ListCopies returns every copy of a file in load order, the given one too, the game's first. It returns none for a
// file under no root or while no workspace is open.
func (wsp *Service) ListCopies(path string) []Place {
	ws, err := wsp.Session()
	if err != nil {
		return nil
	}
	mods := ws.Mods.Load()
	loaded, copies, ok := mods.FindLoaded(ws.Locate(mods, path).Rel)
	if ok {
		copies = append(slices.Clip(copies), loaded)
	}
	return lo.Map(copies, func(file files.File, _ int) Place { return ws.Locate(mods, file.Path) })
}

// ListRoots returns the place of each root of the workspace: the mods' in load order, then the game's own folder. It
// returns none if no workspace is open.
func (wsp *Service) ListRoots() []Place {
	ws, err := wsp.Session()
	if err != nil {
		return nil
	}
	roots := slices.Clone(ws.Mods.Load().Roots[len(ws.Install.Roots):])
	scriptRoot, ok := lo.Find(ws.Install.Roots, func(root files.Root) bool {
		return filepath.Base(root.Dir) == game.ScriptRoot
	})
	if ok {
		roots = append(roots, scriptRoot)
	}
	return lo.Map(roots, func(root files.Root, _ int) Place { return wsp.Locate(root.Dir) })
}
