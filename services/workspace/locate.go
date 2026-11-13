// locate.go says where a file sits in the workspace.

package workspace

import (
	"cmp"
	"path/filepath"
	"slices"
	"strings"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/lang"
)

// Place is where one file sits in the workspace, with the root it is under
// and what the workspace knows about it.
type Place struct {
	files.File
	Name string `json:"name"`

	// Folder is "" for a file at its root.
	Folder   string `json:"folder"`
	Root     string `json:"root"`
	Database string `json:"database"`
	ReadOnly bool   `json:"readOnly"`

	// Position is where the index lists the file, -1 if it does not.
	Position int32 `json:"-"`
	IsDir    bool  `json:"isDir"`

	// Drawable is true for a file the Visual view draws.
	Drawable bool `json:"drawable"`

	// Readme is the path of the readme of the file's database, "" if none.
	Readme string `json:"readme"`

	// Language is the editor language, plain text for a format with none.
	Language string `json:"language"`

	// Hiding is why the game does not read the file, "" for a file it reads.
	// Hider is the id of the mod that hides it, "" where no mod does.
	Hiding files.Hiding `json:"hiding"`
	Hider  string       `json:"hider"`
}

const (
	plainLanguage  = "plaintext"
	scriptLanguage = "paradox-script"
)

var editorLanguages = map[files.Format]string{
	files.FormatScript: scriptLanguage, files.FormatAsset: scriptLanguage, files.FormatFont: scriptLanguage,
	files.FormatGFX: scriptLanguage, files.FormatGUI: "paradox-gui", files.FormatLocalization: "paradox-localization",
	files.FormatReadme: "paradox-readme", files.FormatMarkdown: "markdown",
}

// Locate returns the place of a file in an index of the mods. A path under no
// root gets only its path, key and name.
func (s *Session) Locate(mods *index.Index, path string) Place {
	place := Place{
		File: files.File{Path: path, Key: files.ToKey(path)}, Name: filepath.Base(path), Position: -1, Language: plainLanguage,
	}
	root, ok := mods.FindRoot(path)
	if !ok {
		return place
	}

	// A root's own folder has no rel.
	place.File, _ = files.NewFile(root, path)
	place.Language = cmp.Or(editorLanguages[place.Format], plainLanguage)
	if cut := strings.LastIndexByte(place.Rel, '/'); cut >= 0 {
		place.Folder = place.Rel[:cut]
	}

	place.Root = root.Dir
	place.ReadOnly = root.Origin == files.OriginVanilla ||
		slices.ContainsFunc(*s.Order.Load(), func(mod Mod) bool { return mod.ID == root.Origin && mod.Workshop })
	if database, ok := s.Schema.FindDatabase(place.File); ok {
		place.Database = database.Name
		if database.Readme != "" {
			place.Readme = files.Join(s.InstallPath, database.Readme)
		}
	}

	place.Drawable = place.Format == files.FormatGUI || place.Database == lang.CoatDatabase
	if position, ok := mods.FindFile(path); ok {
		hiding, hider := mods.FindHiding(position)
		place.Position, place.Hiding, place.Hider = position, hiding, hider.Origin
	}

	return place
}

// ListCopies returns every copy of a file in load order, the game's first.
func (s *Service) ListCopies(path string) []Place {
	ws, err := s.Session()
	if err != nil {
		return nil
	}

	mods := ws.Mods.Load()
	loaded, copies, ok := mods.FindLoaded(ws.Locate(mods, path).Rel)
	if ok {
		copies = append(slices.Clip(copies), loaded)
	}

	places := make([]Place, len(copies))
	for position, file := range copies {
		places[position] = ws.Locate(mods, file.Path)
	}

	return places
}

// ListRoots returns the places of the workspace's roots: the mods' in load
// order, then the game's own folder.
func (s *Service) ListRoots() []Place {
	ws, err := s.Session()
	if err != nil {
		return nil
	}

	roots := slices.Clone(ws.Mods.Load().Roots[len(ws.Install.Roots):])
	for _, root := range ws.Install.Roots {
		if filepath.Base(root.Dir) == game.ScriptRoot {
			roots = append(roots, root)

			break
		}
	}

	places := make([]Place, len(roots))
	for position, root := range roots {
		places[position] = s.Locate(root.Dir)
	}

	return places
}
