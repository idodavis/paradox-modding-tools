// Package files lists the files of an install, a mod or a dump folder and tells what format each one is. It reads and
// writes a file's text in its own encoding, compares and joins paths, unpacks a zip and applies ignore rules.
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

// A Format is the class of a file: script, image, localization and so on.
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
	FormatWebImage     Format = "web-image"    // .jpg, .jpeg, .gif, .webp: an image the games do not read
	FormatTable        Format = "table"        // .csv
	FormatDescriptor   Format = "descriptor"   // .mod, metadata.json
	FormatShader       Format = "shader"       // .fxh, .shader
	FormatText         Format = "text"         // any other text: .info, .md, .json
	FormatBinary       Format = "binary"       // anything the binary test flags
)

// formats maps an extension to the format it gives a file, once Classify's folder rules have not decided.
var formats = map[string]Format{
	".txt": FormatScript, ".gui": FormatGUI, ".asset": FormatAsset, ".font": FormatFont, ".gfx": FormatGFX,
	".dds": FormatImage, ".png": FormatImage, ".tga": FormatImage,
	".jpg": FormatWebImage, ".jpeg": FormatWebImage, ".gif": FormatWebImage, ".webp": FormatWebImage,
	".csv": FormatTable, ".fxh": FormatShader, ".shader": FormatShader, ".hlsl": FormatShader,
	".dll": FormatBinary, ".exe": FormatBinary, ".so": FormatBinary, ".bin": FormatBinary, ".ogg": FormatBinary,
	".wav": FormatBinary, ".mp3": FormatBinary, ".bk2": FormatBinary, ".mesh": FormatBinary, ".anim": FormatBinary,
	".pdx": FormatBinary, ".ttf": FormatBinary, ".otf": FormatBinary, ".zip": FormatBinary, ".pak": FormatBinary,
}

// Classify returns the format of a file from its path relative to its root. It looks at the extension and the folder
// only. It returns FormatText for an extension it does not know, and for a script file directly in the root, which
// the games never read.
func Classify(rel string) Format {
	folder, base := path.Split(strings.ToLower(rel))
	ext := path.Ext(base)
	switch {
	case ext == ".yml" && slices.Contains(strings.Split(folder, "/"), "localization"):
		return FormatLocalization
	case ext == ".mod" && folder == "", base == "metadata.json" && path.Base(folder) == ".metadata":
		return FormatDescriptor
	}
	format, ok := formats[ext]
	if !ok || (folder == "" && format.IsScript()) {
		return FormatText
	}
	return format
}

// IsScript reports whether files of the format are written in the script language: .txt, .gui, .asset, .font and
// .gfx files.
func (format Format) IsScript() bool {
	return slices.Contains([]Format{FormatScript, FormatGUI, FormatAsset, FormatFont, FormatGFX}, format)
}

// IsText reports whether files of the format are text: every format but images and binaries.
func (format Format) IsText() bool {
	return !slices.Contains([]Format{FormatImage, FormatWebImage, FormatBinary}, format)
}

// A Root is one folder laid out like the game's own: the game's folder, a DLC's or a mod's.
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

// Walk lists every file under a root, sorted by relative path. It skips the folders IsHiddenDir reports and follows
// a link to a folder once. A link that leads nowhere lists nothing.
func Walk(root Root) ([]File, error) {
	var mu sync.Mutex
	var list []File
	visitor := func(filePath string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, ok := ToRel(root.Dir, filePath)
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
			list = append(list, File{Path: filePath, Key: ToKey(filePath), Rel: rel, Origin: root.Origin, Format: Classify(rel)})
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

// IsBinary reports whether the bytes are not text: they hold a NUL byte in the first kilobyte.
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

// ParseIgnore parses the ignore rules in a text, one per line. It skips blank lines and lines that start with `#`.
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

// Match reports whether the rules cover the given path, which is relative to the root, with forward slashes. The flag
// says whether the path is a folder.
func (ignore Ignore) Match(rel string, dir bool) bool {
	// A folder the rules cover takes everything inside it, so each folder above the path is judged first.
	for folder := path.Dir(rel); folder != "."; folder = path.Dir(folder) {
		if ignore.matchRel(folder, true) {
			return true
		}
	}
	return ignore.matchRel(rel, dir)
}

// matchRel reports whether the rules cover the given path alone, without judging the folders above it. The last
// matching rule wins. A folder-only rule never covers a file.
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
