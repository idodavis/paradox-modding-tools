// Package source knows every file of a game install or a mod: where the folders that mirror the game layout are, what each file is relative to, where it came from, and which reader its format needs. It interprets no content. The format table is one table for every game: a game is never asked for, and an extension the table does not list is Unknown, which everything reads as opaque and the learner reports as drift, so a game update that ships a new kind of file is seen rather than guessed at.
package source

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/lang/jomini"
)

// Format is which reader a file needs.
type Format uint8

const (
	Unknown Format = iota // an extension the table does not list
	Script                // Jomini script: the tree
	Loc                   // localization entries
	Table                 // lines of ; separated columns
	List                  // sound/GUIDs.txt: one name per line
	JSON                  // a decoded value
	Info                  // the prose a game ships beside its script
	Image                 // header facts, read on demand
	Opaque                // a path and nothing more
)

var formatNames = [...]string{"unknown", "script", "loc", "table", "list", "json", "info", "image", "opaque"}

func (f Format) String() string { return formatNames[f] }

// formats is the one table, by lower-case extension. Measured over the three installs on 2026-09-20; the comment on a row says what was seen.
var formats = map[string]Format{
	// script the games and mods define things in
	".txt": Script, ".gui": Script, ".asset": Script, ".gfx": Script, ".font": Script, ".map": Script, ".shortcuts": Script,
	".settings": Script, ".skin": Script, ".lookup": Script, ".dlc": Script, ".particle2": Script, ".particle": Script,

	// script the engine's own tools write: it parses, and a mod may ship it
	".editordata": Script, ".compound": Script, ".schematic": Script, ".animsm": Script, ".aggregate": Script, ".curves": Script,
	".layout": Script, ".lines": Script, ".profile": Script, ".bindings": Script, ".anchor": Script, ".terrain": Script,
	".vertical_borders": Script, ".modifierpack": Script, ".audio2": Script, ".guianimset": Script, ".guistateset": Script,
	".heightmap": Script, ".meta": Script, ".smv": Script, ".shadernodes": Script, ".shadergraph": Script, ".ugraph": Script,
	".dep": Script, ".config": Script,

	".yml":  Loc,
	".csv":  Table,
	".json": JSON,
	".info": Info, ".md": Info,
	".dds": Image, ".png": Image, ".jpg": Image, ".jpeg": Image, ".tga": Image,

	// binary, or text nothing here reads: named in script by path, never opened
	".mesh": Opaque, ".anim": Opaque, ".bank": Opaque, ".bnk": Opaque, ".wem": Opaque, ".bk2": Opaque, ".ogg": Opaque,
	".wav": Opaque, ".mp3": Opaque, ".flac": Opaque, ".ttf": Opaque, ".otf": Opaque, ".fnt": Opaque, ".dat": Opaque,
	".bin": Opaque, ".bmp": Opaque, ".cur": Opaque, ".ani": Opaque, ".shader": Opaque, ".fxh": Opaque, ".swatch": Opaque,
	".splnet": Opaque, ".svg": Opaque, ".ico": Opaque,
}

// ignored is what ships in an install and is no part of the game's content, with the reason.
var ignored = map[string]string{
	".dll": "program code", ".dylib": "program code", ".py": "a build script", ".lnk": "a Windows shortcut left in the install",
	".pdf": "a font licence", ".psd": "source art", ".mb": "source art", ".jpf": "source art", ".obj": "source art",
	".mtl": "source art", ".xml": "Wwise bank metadata; audio is deferred", ".ini": "a desktop.ini",
	".log": "a tool's log", ".tmp": "a tool's leftover", ".cache": "a tool's cache", ".hash": "a tool's cache",
	".dmp": "a crash dump", ".disabled": "a file the game itself switched off", ".gitignore": "version control",
	".xnviewsort": "an image viewer's leftover", "": "no extension: licence texts and tool leftovers",
}

// layout is the folder names that mark a folder as mirroring the game layout.
var layout = []string{"common", "events", "gfx", "gui", "history", "localization", "map_data", "music", "sound", "fonts"}

// File is one file of an install or a mod.
type File struct {
	Path   string // absolute, as the walk spelled it
	Rel    string // relative to its root, slash separated: common/traits/00_traits.txt
	Origin string // "vanilla", or a mod's id
	Format Format
}

// Text reads the file as text: BOM stripped, CRLF folded, Latin-1 where it is not UTF-8.
func (f File) Text() (string, error) {
	raw, err := os.ReadFile(f.Path)
	if err != nil {
		return "", err
	}

	text, _ := jomini.Decode(raw)
	return text, nil
}

// Classify is the format of a file, from its path relative to a root.
func Classify(rel string) Format {
	name := strings.ToLower(path.Base(rel))

	// two files are what they are by name, not by extension
	if name == "guids.txt" && strings.HasSuffix(strings.ToLower(path.Dir(rel)), "sound") {
		return List
	}
	if strings.HasPrefix(name, "readme") {
		return Info
	}
	return formats[path.Ext(name)]
}

// Roots is the folders under dir that mirror the game layout, in the order the game loads them: the engine's base layers (clausewitz, then jomini), the game, then each DLC. A folder that mirrors the layout is not searched further, except for its dlc folder.
func Roots(dir string) []string {
	var roots []string
	var visit func(dir string, depth int)
	visit = func(dir string, depth int) {
		entries, err := os.ReadDir(dir)
		if err != nil || depth > 4 {
			return
		}

		// a folder holding one of the layout's folders is a root
		isRoot := slices.ContainsFunc(entries, func(e fs.DirEntry) bool {
			return e.IsDir() && slices.Contains(layout, strings.ToLower(e.Name()))
		})
		if isRoot {
			roots = append(roots, dir)
		}

		for _, e := range entries {
			if e.IsDir() && (!isRoot || strings.EqualFold(e.Name(), "dlc")) {
				visit(filepath.Join(dir, e.Name()), depth+1)
			}
		}
	}
	visit(dir, 0)

	slices.SortStableFunc(roots, func(a, b string) int { return rootRank(dir, a) - rootRank(dir, b) })
	return roots
}

// rootRank orders roots the way the game layers them: clausewitz, jomini, everything else, DLC last.
func rootRank(dir, root string) int {
	rel, _ := filepath.Rel(dir, root)
	rel = strings.ToLower(filepath.ToSlash(rel))
	switch {
	case strings.Contains("/"+rel+"/", "/dlc/"):
		return 3
	case rel == "clausewitz" || strings.HasPrefix(rel, "clausewitz/"):
		return 0
	case rel == "jomini" || strings.HasPrefix(rel, "jomini/"):
		return 1
	}
	return 2
}

// Inventory is every file under the roots that is part of the game's content, in root order and then path order. A root inside another root (a DLC folder) owns its own files.
func Inventory(origin string, roots []string) []File {
	var files []File
	for _, root := range roots {
		Walk(root, func(p string) {
			owned := slices.ContainsFunc(roots, func(other string) bool {
				return len(other) > len(root) && strings.HasPrefix(p, other+string(filepath.Separator))
			})
			rel, err := filepath.Rel(root, p)
			if owned || err != nil {
				return
			}

			rel = filepath.ToSlash(rel)
			if _, skip := ignored[strings.ToLower(path.Ext(rel))]; skip {
				return
			}
			files = append(files, File{Path: p, Rel: rel, Origin: origin, Format: Classify(rel)})
		})
	}
	return files
}
