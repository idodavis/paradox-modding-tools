// Package files lists and reads the files of an install, a mod or a dump folder. It walks a folder, tells what
// format each file is, reads and writes a file's text in the encoding it was stored in, compares and joins paths,
// and applies ignore rules. It knows nothing about any particular game.
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

// Workers is how many goroutines a parallel pass over files runs: every core but two, which stay free for the rest of
// the machine, and at least one.
var Workers = max(runtime.NumCPU()-2, 1)

// A Format says what kind of file a file is.
type Format string

// The formats a file can have, each told by its extension and the folder it sits in.
const (
	FormatScript       Format = "script"       // .txt
	FormatGUI          Format = "gui"          // .gui
	FormatAsset        Format = "asset"        // .asset
	FormatFont         Format = "font"         // .font
	FormatGFX          Format = "gfx"          // .gfx
	FormatLocalization Format = "localization" // .yml under localization
	FormatImage        Format = "image"        // .dds, .png, .tga
	FormatTable        Format = "table"        // .csv
	FormatDescriptor   Format = "descriptor"   // .mod, metadata.json
	FormatShader       Format = "shader"       // .fxh, .shader
	FormatText         Format = "text"         // any other text: .info, .md, .json
	FormatBinary       Format = "binary"       // anything the binary test flags
)

// Classify returns the format of a file from its path alone, by its extension and the folder it sits in under its
// root.
func Classify(rel string) Format {
	folder, base := path.Split(strings.ToLower(rel))
	folders := strings.Split(folder, "/")
	switch path.Ext(base) {
	case ".yml":
		if slices.Contains(folders, "localization") || slices.Contains(folders, "localisation") {
			return FormatLocalization
		}
	case ".mod":
		if folder == "" {
			return FormatDescriptor
		}
	case ".json":
		if base == "metadata.json" && path.Base(folder) == ".metadata" {
			return FormatDescriptor
		}
	case ".txt":
		return FormatScript
	case ".gui":
		return FormatGUI
	case ".asset":
		return FormatAsset
	case ".font":
		return FormatFont
	case ".gfx":
		return FormatGFX
	case ".dds", ".png", ".tga":
		return FormatImage
	case ".csv":
		return FormatTable
	case ".fxh", ".shader", ".hlsl":
		return FormatShader
	case ".dll", ".exe", ".so", ".bin", ".ogg", ".wav", ".mp3", ".bk2", ".mesh", ".anim", ".pdx", ".ttf", ".otf",
		".zip", ".pak":
		return FormatBinary
	}
	return FormatText
}

// IsScript reports whether files of the format are written in the script language: .txt, .gui, .asset, .font and
// .gfx files.
func (format Format) IsScript() bool {
	return slices.Contains([]Format{FormatScript, FormatGUI, FormatAsset, FormatFont, FormatGFX}, format)
}

// A Root is one folder laid out like the game's: the game's own folder, a DLC's or a mod's.
type Root struct {
	Dir    string `json:"dir"`
	Origin string `json:"origin"` // game.OriginVanilla or a mod's id
}

// A File is one file under a root: where it is, the key it is compared by, and what format it is.
type File struct {
	Path   string `json:"path"`
	Key    string `json:"key"`    // the one key two paths are compared by: cleaned, lower-cased, forward slashes
	Rel    string `json:"rel"`    // from its root, forward slashes
	Origin string `json:"origin"` // its root's
	Format Format `json:"format"`
}

// Walk lists every file under a root. It skips the folders IsHiddenDir names and anything the ignore rules cover,
// and it follows a link once.
func Walk(root Root, ignore Ignore) ([]File, error) {
	var mu sync.Mutex
	var list []File
	visitor := func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, ok := ToRel(root.Dir, path)
		if !ok {
			return nil
		}
		dir := entry.IsDir()
		if entry.Type()&fs.ModeSymlink != 0 {
			target, err := fastwalk.StatDirEntry(path, entry)
			if err != nil {
				return nil // a link that leads nowhere lists nothing
			}
			dir = target.IsDir()
		}
		switch {
		case dir && IsHiddenDir(entry.Name()) || ignore.Match(rel, dir):
			if dir {
				return fastwalk.SkipDir
			}
		case !dir:
			mu.Lock()
			defer mu.Unlock()
			list = append(list, File{Path: path, Key: ToKey(path), Rel: rel, Origin: root.Origin, Format: Classify(rel)})
		}
		return nil
	}
	if err := fastwalk.Walk(&fastwalk.Config{Follow: true}, root.Dir, visitor); err != nil {
		return nil, err
	}
	slices.SortFunc(list, func(file, other File) int { return strings.Compare(file.Rel, other.Rel) })
	return list, nil
}

// IsHiddenDir reports whether a folder of the given name is one that nothing lists or watches: a dot folder, such as
// .git, except .metadata, where Vic3 and EU5 keep a mod's descriptor.
func IsHiddenDir(name string) bool {
	return strings.HasPrefix(name, ".") && name != ".metadata"
}

// IsBinary reports whether the bytes are not text, which means a NUL byte in the first kilobyte.
func IsBinary(bytes []byte) bool {
	return slices.Contains(bytes[:min(len(bytes), 1024)], 0)
}

// An Ignore is a list of ignore rules with gitignore's meaning: anchoring, negation and a trailing slash for
// folders. A nil Ignore has no rules.
type Ignore []pattern

// A pattern is one ignore rule: its glob and the three flags gitignore gives a rule.
type pattern struct {
	glob     string
	negates  bool
	dirOnly  bool
	anchored bool
}

// ParseIgnore reads ignore rules from a text, one per line. Blank lines and `#` comments are skipped.
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
		// gitignore anchors a rule with a slash anywhere inside it, not only one that starts with a slash.
		rule.anchored = rule.anchored || strings.Contains(rule.glob, "/")
		ignore = append(ignore, rule)
	}
	return ignore
}

// Match reports whether the rules cover a path. The path is relative to the root with forward slashes, and the
// flag says whether it is a folder.
func (ignore Ignore) Match(rel string, dir bool) bool {
	// A folder the rules cover takes everything inside it, so each folder above the path is judged first.
	for folder := path.Dir(rel); folder != "."; folder = path.Dir(folder) {
		if ignore.matchRel(folder, true) {
			return true
		}
	}
	return ignore.matchRel(rel, dir)
}

// matchRel reports whether the rules cover one path by itself, the last matching rule winning. A folder-only
// rule never covers a file.
func (ignore Ignore) matchRel(rel string, dir bool) bool {
	result := false
	for _, rule := range ignore {
		if rule.dirOnly && !dir {
			continue
		}
		subject := rel
		if !rule.anchored {
			subject = path.Base(rel)
		}
		if ok, _ := doublestar.Match(rule.glob, subject); ok {
			result = !rule.negates
		}
	}
	return result
}
