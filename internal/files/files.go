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
// Universalis 5 mounts loading_screen, main_menu and in_game, in that order.
// A stage folder is no part of a file's path for the game, so
// in_game/common/traits and common/traits are one folder.
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
//     mounted: it is not read and hides no file.
//   - Of the files at one path, the game reads the last, by root in load
//     order and then by stage, and the others are hidden.
//   - A mod's replace_path drops the game's files in exactly the folder it
//     names, not in its subfolders. No source says whether it also drops an
//     earlier mod's files there, so those are read, and IsReplacedLater tells
//     where that is open.
//   - CompareLoadOrder orders the paths of one folder: its files before its
//     subfolders', and names byte by byte, with case.
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
// Walk passes over a folder below its root that cannot be read. ListRows
// passes over a row whose first column is empty. Whether the game on
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

// Workers is how many goroutines a parallel pass runs.
var Workers = max(runtime.NumCPU()-2, 1)

// Format is the class of a file.
type Format string

// The formats of a file.
const (
	FormatScript Format = "script"
	FormatGUI    Format = "gui"
	FormatAsset  Format = "asset"
	FormatFont   Format = "font"
	FormatGFX    Format = "gfx"

	// FormatLocalization is a .yml file below a localization folder.
	FormatLocalization Format = "localization"

	FormatImage Format = "image"

	// FormatWebImage is a picture the games do not read.
	FormatWebImage Format = "web-image"

	FormatTable Format = "table"

	// FormatDescriptor is a mod's .mod file or metadata.json.
	FormatDescriptor Format = "descriptor"

	FormatShader Format = "shader"

	// FormatReadme is a game's prose notes: an .info or a readme .txt.
	FormatReadme Format = "readme"

	FormatMarkdown Format = "markdown"

	// FormatText is any other text, and script directly at a root.
	FormatText Format = "text"

	FormatBinary Format = "binary"
)

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

// Classify returns the format of a file from its path below its root. An
// extension it does not know is text.
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

// IsScript reports whether files of the format are written in script.
func (f Format) IsScript() bool {
	return slices.Contains([]Format{FormatScript, FormatGUI, FormatAsset, FormatFont, FormatGFX}, f)
}

// DefaultEncoding returns the encoding a new file of the format is written
// in.
func (f Format) DefaultEncoding() Encoding {
	if f.IsScript() || f == FormatLocalization {
		return EncodingUTF8BOM
	}

	return EncodingUTF8
}

// IsText reports whether files of the format hold text.
func (f Format) IsText() bool {
	return !slices.Contains([]Format{FormatImage, FormatWebImage, FormatBinary}, f)
}

// Root is one folder laid out like the game's own: the game's, a DLC's or a
// mod's.
type Root struct {
	Dir string `json:"dir"`

	// Origin is OriginVanilla for the game's own folders, else the mod's id.
	Origin string `json:"origin"`

	// Stages are the stage folders the game mounts, in order, and none for a
	// game without stages.
	Stages []string `json:"stages"`

	// ReplacePaths are the folders the mod's replace_path names.
	ReplacePaths []string `json:"replacePaths"`
}

// OriginVanilla is the origin of the game's own files.
const OriginVanilla = "vanilla"

// File is one file below a root.
type File struct {
	Path string `json:"path"`

	// Key is the path as paths are compared: lower case, with slashes.
	Key string `json:"key"`

	// Rel is the path below the file's root, with slashes, and Origin is that
	// root's.
	Rel    string `json:"rel"`
	Origin string `json:"origin"`

	// Stage is the stage folder Rel opens with, or "" for none.
	Stage string `json:"stage"`

	Format Format `json:"format"`
}

// NewFile returns the file at a path below a root. It returns false, and a
// file with no Rel, if the path is not below the root.
func NewFile(root Root, path string) (File, bool) {
	rel, ok := ToRel(root.Dir, path)
	stage, _ := SplitStage(rel, root.Stages)
	file := File{Path: path, Key: ToKey(path), Rel: rel, Origin: root.Origin, Stage: stage, Format: Classify(rel)}

	return file, ok
}

// SplitStage splits a path below a root into its stage folder and the rest. A
// stage matches in any letter case. A path under none gives "" and the path.
func SplitStage(rel string, stages []string) (stage, rest string) {
	head, tail, ok := strings.Cut(rel, "/")
	if ok && slices.ContainsFunc(stages, func(folder string) bool { return strings.EqualFold(folder, head) }) {
		return head, tail
	}

	return "", rel
}

// StageRel returns the file's path below its stage folder, which is the path
// the game reads it at. It is Rel for a file under no stage.
func (f File) StageRel() string {
	if f.Stage == "" {
		return f.Rel
	}

	return f.Rel[len(f.Stage)+1:]
}

// Walk lists every file under a root, sorted by Rel. It skips hidden folders
// and follows links to folders. It returns an error only if the root itself
// cannot be read.
func Walk(root Root) ([]File, error) {
	var mu sync.Mutex
	var list []File

	visitor := func(filePath string, entry fs.DirEntry, err error) error {
		file, below := NewFile(root, filePath)

		switch {
		// A folder below the root that cannot be read is passed over, so one
		// locked folder does not fail a whole mod.
		case err != nil && below:
			return nil

		case err != nil:
			return err

		case !below:
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

// IsHiddenDir reports whether a folder name is a dot folder. .metadata is
// not, as Vic3 and EU5 keep a mod's descriptor there.
func IsHiddenDir(name string) bool {
	return strings.HasPrefix(name, ".") && name != ".metadata"
}

// IsBinary reports whether the bytes hold a NUL byte in the first kilobyte.
func IsBinary(bytes []byte) bool {
	return slices.Contains(bytes[:min(len(bytes), 1024)], 0)
}

// Ignore is a list of ignore rules with gitignore's meaning. A nil Ignore
// matches nothing.
type Ignore []pattern

type pattern struct {
	glob string

	// negates is set by a leading !, dirOnly by a trailing slash, and
	// anchored by a slash anywhere else, which matches the rule against the
	// whole path and not only the last name.
	negates  bool
	dirOnly  bool
	anchored bool
}

// ParseIgnore parses ignore rules, one per line, skipping blank lines and `#`
// comments.
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

		// gitignore anchors a rule with a slash anywhere in it.
		rule.anchored = rule.anchored || strings.Contains(rule.glob, "/")

		ignore = append(ignore, rule)
	}

	return ignore
}

// Match reports whether the rules cover a path below the root, given whether
// it is a folder. A path inside a covered folder is covered.
func (i Ignore) Match(rel string, dir bool) bool {
	for folder := path.Dir(rel); folder != "."; folder = path.Dir(folder) {
		if i.matchRel(folder, true) {
			return true
		}
	}

	return i.matchRel(rel, dir)
}

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

		// The last matching rule decides, as in gitignore.
		if ok, _ := doublestar.Match(rule.glob, subject); ok {
			result = !rule.negates
		}
	}

	return result
}
