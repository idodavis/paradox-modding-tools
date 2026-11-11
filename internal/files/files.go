// Package files lists the files of an install, a mod or a dump folder, tells
// what format each one is, and decides which of them the game reads. It also
// reads and writes a file's text in its own encoding, reads a table's rows,
// compares and joins paths, unpacks a zip and applies ignore rules.
//
// It knows no game. Which folders a game reads, and what each one holds, are
// the rows of the game package; this one names a file by its place below a
// root and by its extension, never by its contents. A root carries what the
// game mounts below it: its stage folders, and a mod's replace_path folders.
//
// # Formats
//
// Classify gives a file its format from its extension and its folder:
//
//   - A .yml file is localization only below a localization folder.
//   - A .mod file is a descriptor only at the root, and metadata.json only in
//     .metadata, where Victoria 3 and Europa Universalis 5 keep it.
//   - A .txt file whose name starts with readme is a readme: the games' are
//     comments only.
//   - Script written directly at a root is text, since the games never read
//     it.
//   - An extension the table of formats does not know is text.
//
// # Stages
//
// A game may mount its files below stage folders in each root, as Europa
// Universalis 5 mounts loading_screen, main_menu and in_game, in that order
// (debug.log:5372-5394). A stage folder is no part of a file's path for the
// game, so in_game/common/traits and common/traits are one folder.
//
// # Mounts
//
// Mounts decides which file the game reads at each path:
//
//   - A file below a root inside another root mounted with it belongs to the
//     inner one, as a DLC's folder sits inside the game's.
//   - Roots mounted later, as a workspace's mods are on its install, load
//     after the roots mounted before them, whose files keep their positions.
//   - In a game with stages, a mod's file outside every stage is never
//     mounted: it is not read and hides no file (EU5 debug.log:80-83, 5392;
//     the wiki's Mod structure, l.46-48).
//   - Of the files at one path, the game reads the last, by root in load
//     order and then by stage, and the others are hidden.
//   - A mod's replace_path drops the game's files in exactly the folder it
//     names, not in its subfolders. No source says whether it also drops an
//     earlier mod's files there, so those are read, and IsReplacedLater tells
//     where that is open.
//   - CompareLoadOrder orders the paths of one folder: its files before its
//     subfolders', as Vic3's game.log and the EU5 wiki say (no CK3 source
//     says otherwise), and names byte by byte, with case.
//
// # Tables
//
// A table file, such as CK3's map_data/definition.csv, writes one row a
// line, with its columns separated by ; and a comment row opened by #. Its
// first column names the row.
//
// # Robustness
//
// ReadText refuses a file over TextLimit and one with a NUL byte in its first
// kilobyte, both with an error that wraps ErrBinary. Unzip refuses a zip that
// names a path outside its folder, and skips links. Every write replaces a
// file whole or not at all, through WriteAtomic. A gob cache that is missing
// or does not decode loads as false, so the caller builds it again.
//
// ListRows passes over a row whose first column is empty. Whether the game on
// Linux mounts a stage folder written in another letter case is not known: no
// install writes one, so a stage is matched in any case, as on Windows.
package files

import (
	"io/fs"
	"path"
	"runtime"
	"slices"
	"strings"
	"sync"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/charlievieth/fastwalk"
)

// Workers is how many goroutines a parallel pass over files runs: every core
// but two, and at least one.
var Workers = max(runtime.NumCPU()-2, 1)

// Format is the class of a file: script, image, localization and so on.
type Format string

// The formats a file can have. The table of extensions below says which
// files take each, and Classify's folder rules decide the rest.
const (
	FormatScript Format = "script"
	FormatGUI    Format = "gui"
	FormatAsset  Format = "asset"
	FormatFont   Format = "font"
	FormatGFX    Format = "gfx"

	// FormatLocalization is a .yml file below a localization folder.
	FormatLocalization Format = "localization"

	FormatImage Format = "image"

	// FormatWebImage is a picture the games do not read, such as a Workshop
	// preview: a .jpg, .jpeg, .gif or .webp.
	FormatWebImage Format = "web-image"

	FormatTable Format = "table"

	// FormatDescriptor is a mod's descriptor: a .mod file at its root, or
	// metadata.json in its .metadata folder.
	FormatDescriptor Format = "descriptor"

	FormatShader Format = "shader"

	// FormatReadme is a game's notes on its own folders, in prose: an .info
	// file, or a .txt whose name starts with readme.
	FormatReadme Format = "readme"

	FormatMarkdown Format = "markdown"

	// FormatText is any other text, such as .json, and script written
	// directly at a root, which the games never read.
	FormatText Format = "text"

	// FormatBinary is a file of an extension the games keep binary data in,
	// such as .mesh or .dll.
	FormatBinary Format = "binary"
)

// formats maps an extension to its format, where Classify's folder rules have
// not decided.
var formats = map[string]Format{
	".txt": FormatScript, ".gui": FormatGUI, ".asset": FormatAsset, ".font": FormatFont, ".gfx": FormatGFX,
	".dds": FormatImage, ".png": FormatImage, ".tga": FormatImage,
	".jpg": FormatWebImage, ".jpeg": FormatWebImage, ".gif": FormatWebImage, ".webp": FormatWebImage,
	".csv": FormatTable, ".fxh": FormatShader, ".shader": FormatShader, ".hlsl": FormatShader,
	".info": FormatReadme, ".md": FormatMarkdown,
	".dll": FormatBinary, ".exe": FormatBinary, ".so": FormatBinary, ".bin": FormatBinary, ".ogg": FormatBinary,
	".wav": FormatBinary, ".mp3": FormatBinary, ".bk2": FormatBinary, ".mesh": FormatBinary, ".anim": FormatBinary,
	".pdx": FormatBinary, ".ttf": FormatBinary, ".otf": FormatBinary, ".zip": FormatBinary, ".pak": FormatBinary,
}

// Classify returns the format of a file from its path relative to its root.
// It returns FormatText for an unknown extension and for a script file
// directly in the root.
func Classify(rel string) Format {
	folder, base := path.Split(strings.ToLower(rel))
	ext := path.Ext(base)

	switch {
	case ext == ".yml" && slices.Contains(strings.Split(folder, "/"), "localization"):
		return FormatLocalization
	case ext == ".txt" && strings.HasPrefix(base, "readme"):
		return FormatReadme
	case ext == ".mod" && folder == "", base == "metadata.json" && path.Base(folder) == ".metadata":
		return FormatDescriptor
	}

	format, ok := formats[ext]
	if !ok || folder == "" && format.IsScript() {
		return FormatText
	}

	return format
}

// IsScript reports whether files of the format are written in the script
// language.
func (f Format) IsScript() bool {
	return slices.Contains([]Format{FormatScript, FormatGUI, FormatAsset, FormatFont, FormatGFX}, f)
}

// DefaultEncoding returns the encoding a file of the format is written in
// when nothing on disk says. The games' script and localization take UTF-8
// with a byte order mark; any other text takes UTF-8.
func (f Format) DefaultEncoding() Encoding {
	if f.IsScript() || f == FormatLocalization {
		return EncodingUTF8BOM
	}

	return EncodingUTF8
}

// IsText reports whether files of the format are text: all but images and
// binaries.
func (f Format) IsText() bool {
	return !slices.Contains([]Format{FormatImage, FormatWebImage, FormatBinary}, f)
}

// Root is one folder laid out like the game's own: the game's folder, a DLC's
// or a mod's.
type Root struct {
	Dir string `json:"dir"`

	// Origin is OriginVanilla for the game's own folders, or the mod's id.
	Origin string `json:"origin"`

	// Stages are the stage folders the game mounts below the root, in the
	// order it mounts them. They are none for a game without stages.
	Stages []string `json:"stages"`

	// ReplacePaths are the folders the mod's replace_path names, as paths
	// from the root. They are none for the game's own folders.
	ReplacePaths []string `json:"replacePaths"`
}

// OriginVanilla is the origin of the game's own files. A mod's files have the
// mod's id as their origin.
const OriginVanilla = "vanilla"

// File is one file below a root: where it is, the key it is compared by, and
// what format it is.
type File struct {
	Path string `json:"path"`

	// Key is the one key two paths are compared by: the path cleaned,
	// lower-cased and with forward slashes.
	Key string `json:"key"`

	// Rel is the path from the file's root, with forward slashes, and Origin
	// is that root's.
	Rel    string `json:"rel"`
	Origin string `json:"origin"`

	// Stage is the stage folder Rel opens with, as Rel writes it. It is ""
	// for a file under no stage.
	Stage string `json:"stage"`

	Format Format `json:"format"`
}

// NewFile returns the file at a path below a root. It returns false if the
// path is not below the root, as the root's own folder is not; the file then
// has its path, key and origin, and no Rel.
func NewFile(root Root, path string) (File, bool) {
	rel, ok := ToRel(root.Dir, path)
	stage, _ := SplitStage(rel, root.Stages)
	file := File{Path: path, Key: ToKey(path), Rel: rel, Origin: root.Origin, Stage: stage, Format: Classify(rel)}

	return file, ok
}

// SplitStage splits a path from a root into the stage folder that opens it
// and the rest, both as the path writes them. The stages are the root's, and
// a stage is matched in any letter case. A path under no stage, or a root
// with no stages, gives "" and the path as it is.
func SplitStage(rel string, stages []string) (stage, rest string) {
	head, tail, ok := strings.Cut(rel, "/")
	if ok && slices.ContainsFunc(stages, func(folder string) bool { return strings.EqualFold(folder, head) }) {
		return head, tail
	}

	return "", rel
}

// StageRel returns the file's path from its stage folder:
// common/traits/00_traits.txt for in_game/common/traits/00_traits.txt. It is
// Rel for a file under no stage.
func (f File) StageRel() string {
	if f.Stage == "" {
		return f.Rel
	}

	return f.Rel[len(f.Stage)+1:]
}

// Walk lists every file under a root, sorted by relative path. It skips the
// folders IsHiddenDir reports, and follows a link to a folder. A link that
// leads nowhere lists nothing.
func Walk(root Root) ([]File, error) {
	var mu sync.Mutex
	var list []File

	visitor := func(filePath string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		file, ok := NewFile(root, filePath)
		if !ok {
			return nil
		}

		dir := entry.IsDir()
		if entry.Type()&fs.ModeSymlink != 0 {
			target, err := fastwalk.StatDirEntry(filePath, entry)
			if err != nil {
				return nil
			}

			dir = target.IsDir()
		}

		switch {
		case dir && IsHiddenDir(entry.Name()):
			return fastwalk.SkipDir

		case !dir:
			mu.Lock()
			defer mu.Unlock()

			list = append(list, file)
		}

		return nil
	}

	if err := fastwalk.Walk(&fastwalk.Config{Follow: true}, root.Dir, visitor); err != nil {
		return nil, err
	}

	slices.SortFunc(list, func(file, other File) int { return strings.Compare(file.Rel, other.Rel) })

	return list, nil
}

// IsHiddenDir reports whether a folder of the given name is one that Walk
// skips and the watcher leaves out: a dot folder, such as .git, except
// .metadata, where Victoria 3 and Europa Universalis 5 keep a mod's
// descriptor.
func IsHiddenDir(name string) bool {
	return strings.HasPrefix(name, ".") && name != ".metadata"
}

// IsBinary reports whether the bytes are not text: they hold a NUL byte in
// the first kilobyte.
func IsBinary(bytes []byte) bool {
	return slices.Contains(bytes[:min(len(bytes), 1024)], 0)
}

// Ignore is a list of ignore rules with gitignore's meaning: anchoring,
// negation and a trailing slash for folders. A nil Ignore has no rules.
type Ignore []pattern

// pattern is one ignore rule.
type pattern struct {
	glob string

	// negates is set for a rule written with a leading !, which takes back
	// what an earlier rule covered.
	negates bool

	// dirOnly is set for a rule written with a trailing slash, which covers
	// folders alone.
	dirOnly bool

	// anchored is set for a rule matched against the whole relative path
	// rather than its last part: one with a slash anywhere in it.
	anchored bool
}

// ParseIgnore parses the ignore rules in a text, one per line. It skips blank
// lines and lines that start with `#`.
func ParseIgnore(text string) Ignore {
	var ignore Ignore

	for line := range strings.Lines(text) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		rule := pattern{}
		rule.glob, rule.negates = strings.CutPrefix(line, "!")
		rule.glob, rule.dirOnly = strings.CutSuffix(rule.glob, "/")
		rule.glob, rule.anchored = strings.CutPrefix(rule.glob, "/")

		// gitignore anchors a rule with a slash anywhere inside it, not only
		// one that starts with a slash.
		rule.anchored = rule.anchored || strings.Contains(rule.glob, "/")

		ignore = append(ignore, rule)
	}

	return ignore
}

// Match reports whether the rules cover the given path, relative to the root
// with forward slashes. The flag says whether the path is a folder.
func (i Ignore) Match(rel string, dir bool) bool {
	for folder := path.Dir(rel); folder != "."; folder = path.Dir(folder) {
		if i.matchRel(folder, true) {
			return true
		}
	}

	return i.matchRel(rel, dir)
}

// matchRel reports whether the rules cover the given path alone, not the
// folders above it.
func (i Ignore) matchRel(rel string, dir bool) bool {
	result := false

	for _, rule := range i {
		if rule.dirOnly && !dir {
			continue
		}

		subject := rel
		if !rule.anchored {
			subject = path.Base(rel)
		}

		// The last rule that matches decides, as in gitignore, so a later rule
		// takes back an earlier one.
		if ok, _ := doublestar.Match(rule.glob, subject); ok {
			result = !rule.negates
		}
	}

	return result
}
