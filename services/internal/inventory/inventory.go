// inventory.go is the files of an install or a mod: the format table, the roots and the files under them, the walk,
// and the scan's worker pool.

// Package inventory knows every file of a game install or a mod: which folders mirror the game
// layout, what each file is relative to, where it came from, and which reader its format needs.
// It also owns what every reader of a file shares: the file's text and how it was stored (text.go), how paths are
// compared and joined (paths.go), the ignore rules a folder is filtered by (ignore.go) and the one rule for a name
// made from a user's text (filenames.go). One format table serves every game: an extension it does not list is
// Unknown, kept in the inventory as such, so a new kind of file is seen rather than guessed at.
// The walk follows symlinks and Windows junctions (mods are often linked folders), keeps every path
// spelled under its root, refuses a link back to an ancestor, hides the names a dot starts (but .metadata), and reads
// folders in name order so two walks of one tree agree.
package inventory

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"golang.org/x/sync/errgroup"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/jomini"
)

// Format is which reader a file needs.
type Format uint8

// File is one file of an install or a mod.
type File struct {
	Path   string `json:"path"`   // absolute, as the walk spelled it
	Rel    string `json:"rel"`    // relative to its root, slash separated: common/traits/00_traits.txt
	Origin string `json:"origin"` // "vanilla", or a mod's id
	Format Format `json:"format"`
}

// Child is one item of a folder: its name, and whether it is a folder (a link or a junction counts as what it leads to).
type Child struct {
	Name  string      `json:"name"`
	IsDir bool        `json:"isDir"`
	info  os.FileInfo // what os.Stat says of an item that is not a regular file; Walk refuses a link back to a folder above
}

// The formats, by the reader each needs.
const (
	Unknown    Format = iota // an extension the table does not list
	Script                   // Jomini script: the tree
	Loc                      // localization entries
	Table                    // lines of ; separated columns
	List                     // sound/GUIDs.txt: one name per line
	JSON                     // a decoded value
	Prose                    // the prose a game ships beside its script
	Image                    // header facts, read on demand
	Skipped                  // a path and nothing more
	Descriptor               // a mod's .mod descriptor: modfiles reads it
	Binary                   // bytes the app never reads as text: a mesh, a sound bank, a font, an archive
)

// HoldsText reports whether a file of the format may hold text: every format but an image and the binary ones, so
// search and Replace never open a mesh, a bank or a font; a skipped text (a shader, a log) is still read.
func (format Format) HoldsText() bool {
	return format != Image && format != Binary
}

// BinaryHead is how many of a file's first bytes IsBinary reads.
const BinaryHead = 8000

// formats is the format table, by lower-case extension, for every game.
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
	".info": Prose, ".md": Prose,
	".mod": Descriptor,
	".dds": Image, ".png": Image, ".jpg": Image, ".jpeg": Image, ".tga": Image,

	// binary, or text nothing here reads: named in script by path, never opened
	".mesh": Binary, ".anim": Binary, ".bank": Binary, ".bnk": Binary, ".wem": Binary, ".bk2": Binary, ".ogg": Binary,
	".wav": Binary, ".mp3": Binary, ".flac": Binary, ".ttf": Binary, ".otf": Binary, ".fnt": Skipped, ".dat": Binary,
	".bin": Binary, ".bmp": Binary, ".cur": Binary, ".ani": Binary, ".shader": Skipped, ".fxh": Skipped, ".swatch": Binary,
	".splnet": Binary, ".svg": Skipped, ".ico": Binary,

	// the games' console dumps: listed so the dump folders inventory them
	".log": Skipped,
}

// ignored is what ships in an install and is no part of the game's content, with the reason.
var ignored = map[string]string{
	".dll": "program code", ".dylib": "program code", ".py": "a build script", ".lnk": "a Windows shortcut left in the install",
	".pdf": "a font license", ".psd": "source art", ".mb": "source art", ".jpf": "source art", ".obj": "source art",
	".mtl": "source art", ".xml": "Wwise bank metadata", ".ini": "a desktop.ini",
	".tmp": "a tool's leftover", ".cache": "a tool's cache", ".hash": "a tool's cache",
	".dmp": "a crash dump", ".disabled": "a file the game itself switched off", ".gitignore": "version control",
	".xnviewsort": "an image viewer's leftover", "": "no extension: license texts and tool leftovers",
}

// layout is the folder names that mark a folder as mirroring the game layout.
var layout = []string{
	"common", "events", "gfx", "gui", "history", "localization", "map_data", "music", "sound", "fonts", "data_binding",
	"input_profile", "interface", "setup",
}

// documentation is the words a .txt is named by when it is prose the games ship beside their
// script: EU5's ____Info.txt and _advances_template.txt, Vic3's 00_example.txt. A whole word of
// the name, never part of one: rainforest is no info file, 00_event_templates.txt is script.
var documentation = map[string]bool{"readme": true, "info": true, "example": true, "examples": true, "template": true, "help": true, "documentation": true}

// Files is every content file under the roots, in root then path order. A root inside another
// root (a DLC folder) owns its own files.
func Files(origin string, roots []string) []File {
	var files []File
	for _, root := range roots {
		// the path keys, each with its trailing separator, of the longer roots (a DLC folder is one under this root): a
		// file whose key starts with one belongs to that root
		var inner []string
		for _, other := range roots {
			if len(other) > len(root) {
				inner = append(inner, PathKey(other)+string(filepath.Separator))
			}
		}

		_ = Walk(root, func(abs string) error {
			key := PathKey(abs)
			rel, inside := RelOf(root, abs)
			if !inside || slices.ContainsFunc(inner, func(prefix string) bool { return strings.HasPrefix(key, prefix) }) {
				return nil
			}
			if _, skip := ignored[strings.ToLower(path.Ext(rel))]; !skip {
				files = append(files, File{Path: abs, Rel: rel, Origin: origin, Format: Classify(abs, rel)})
			}
			return nil
		})
	}
	return files
}

// Roots is the folders under dir that mirror the game layout, in the game's load order: the
// engine's base layers (clausewitz, then jomini), the game, then each DLC. A root is not searched
// further, except for its dlc folder.
func Roots(dir string) []string {
	var roots []string
	var visit func(dir string, depth int)
	visit = func(dir string, depth int) {
		entries, err := os.ReadDir(dir)
		if err != nil || depth > 4 {
			return
		}

		// a folder holding one of the layout's folders is a root
		isRoot := slices.ContainsFunc(entries, func(entry fs.DirEntry) bool {
			return entry.IsDir() && slices.Contains(layout, strings.ToLower(entry.Name()))
		})
		if isRoot {
			roots = append(roots, dir)
		}

		for _, entry := range entries {
			if entry.IsDir() && (!isRoot || strings.EqualFold(entry.Name(), "dlc")) {
				visit(filepath.Join(dir, entry.Name()), depth+1)
			}
		}
	}
	visit(dir, 0)

	slices.SortStableFunc(roots, func(first, second string) int { return rootRank(dir, first) - rootRank(dir, second) })
	return roots
}

// rootRank orders roots as the game layers them: clausewitz, jomini, everything else, DLC last.
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

// Classify is the format of a file, from its path relative to a root. A .txt named as documentation
// is read to see whether it is: its comments and sentences outnumber its script lines.
func Classify(abs, rel string) Format {
	name, dir := strings.ToLower(path.Base(rel)), strings.ToLower(path.Dir(rel))

	// some files are what they are by name and folder, not by extension: a sound GUID list, the
	// Wwise bank listing beside its .bnk, and a .txt directly in a root, which is no script
	if strings.HasSuffix(name, "guids.txt") && strings.HasSuffix(dir, "sound") {
		return List
	}
	if path.Ext(name) == ".txt" && (strings.Contains("/"+dir+"/", "/sound/banks/") || !strings.Contains(rel, "/")) {
		return Skipped
	}
	if strings.HasPrefix(name, "readme") {
		return Prose
	}
	format := formats[path.Ext(name)]
	if format == Script && path.Ext(name) == ".txt" {
		words := strings.FieldsFunc(strings.TrimSuffix(name, ".txt"), func(char rune) bool { return char == '_' || char == '-' || char == ' ' || char == '.' })
		if slices.ContainsFunc(words, func(word string) bool { return documentation[word] }) && prose(abs) {
			return Prose
		}
	}
	return format
}

// prose reports a text file whose comment and sentence lines outnumber its script lines. A sentence: three words or
// more, no brace, no operator second.
func prose(abs string) bool {
	text, err := LoadText(abs)
	if err != nil {
		return false
	}
	script, comment := 0, 0
	lines := jomini.NewLineIndex(text.Content)
	for n := range lines.LineCount() {
		line := lines.LineText(text.Content, n)
		words := strings.Fields(line)
		switch trimmed := strings.TrimSpace(line); {
		case trimmed == "":
		case strings.HasPrefix(trimmed, "#"):
			comment++
		case len(words) >= 3 && !strings.ContainsAny(trimmed, "{}") && !strings.ContainsAny(words[1], "=<>"):
			comment++
		default:
			script++
		}
	}
	return comment >= script
}

// Walk calls visit with the path of every file under root, following links and junctions and leaving out every name
// Hidden says, files and folders alike. An error from visit stops the walk and is returned; filepath.SkipAll stops it
// quietly.
func Walk(root string, visit func(path string) error) error {
	info, err := os.Stat(root)
	if err != nil {
		return nil
	}
	if err := walk(root, []os.FileInfo{info}, visit); err != filepath.SkipAll {
		return err
	}
	return nil
}

// walk visits one folder's files and enters its folders; ancestors is the folders above it, which a link may not lead
// back to.
func walk(dir string, ancestors []os.FileInfo, visit func(path string) error) error {
	children, err := Children(dir)
	if err != nil {
		return nil
	}

	for _, child := range children {
		path := filepath.Join(dir, child.Name)
		switch {
		case Hidden(child.Name):
		case !child.IsDir:
			if err := visit(path); err != nil {
				return err
			}
		case slices.ContainsFunc(ancestors, func(ancestor os.FileInfo) bool { return os.SameFile(ancestor, child.info) }):
		default:
			if err := walk(path, append(ancestors, child.info), visit); err != nil {
				return err
			}
		}
	}
	return nil
}

// Hidden is whether a name is one the walk and the explorer leave out unless asked for everything: it starts with a
// dot, but is not game.MetadataDir, where an EU5 mod keeps its descriptor. A mod's .git, .vscode and .idea are no
// content of it.
func Hidden(name string) bool {
	return strings.HasPrefix(name, ".") && name != game.MetadataDir
}

// Children is every item of the folder dir in name order, each link followed once; a link that leads nowhere is left
// out.
func Children(dir string) ([]Child, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	children := make([]Child, 0, len(entries))
	for _, entry := range entries {
		child := Child{Name: entry.Name()}
		if !entry.Type().IsRegular() {
			// a folder, a link or a junction: Stat follows it
			info, err := os.Stat(filepath.Join(dir, child.Name))
			if err != nil {
				continue
			}
			child.IsDir, child.info = info.IsDir(), info
		}
		children = append(children, child)
	}
	return children, nil
}

// Ordered is the scan's worker pool: it runs work over files on a worker per CPU, a bounded window ahead, and calls
// keep with each result in file order on the calling goroutine, so what keep builds never depends on timing. The
// schema's vanilla pass and the index's walk run on it.
func Ordered[T any](ctx context.Context, files []File, work func(File) T, keep func(File, T)) error {
	var workers errgroup.Group
	workers.SetLimit(runtime.NumCPU())
	queue := make(chan chan T, 4*runtime.NumCPU())
	go func() {
		defer close(queue)
		for _, file := range files {
			ready := make(chan T, 1)
			select {
			case queue <- ready:
			case <-ctx.Done():
				return
			}
			workers.Go(func() error {
				ready <- work(file)
				return nil
			})
		}
	}()

	next := 0
	for ready := range queue {
		keep(files[next], <-ready)
		next++
	}
	return ctx.Err()
}

// String is a format's name, as the editor reads it to pick a file's language.
func (format Format) String() string {
	return [...]string{"unknown", "script", "loc", "table", "list", "json", "info", "image", "opaque", "descriptor", "binary"}[format]
}

// IsBinary reports whether a file's first bytes are binary: a NUL among the first BinaryHead, git's own test.
func IsBinary(head []byte) bool {
	return bytes.IndexByte(head[:min(len(head), BinaryHead)], 0) >= 0
}
