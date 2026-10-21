// Package files lists and reads the files of an install, a mod or a dump folder. It walks a folder, tells what
// format each file is, reads and writes a file's text in the encoding it was stored in, compares and joins paths,
// and applies ignore rules. It knows nothing about any particular game.
package files

import (
	"github.com/bmatcuk/doublestar/v4"
	"github.com/charlievieth/fastwalk"
)

// Walk is built on fastwalk and the ignore rules on doublestar.
var (
	_ = fastwalk.Walk
	_ = doublestar.Match
)

// A Format says what kind of file a file is.
type Format string

const (
	FormatScript       Format = "script"       // .txt under the game's script folders
	FormatGUI          Format = "gui"          // .gui
	FormatAsset        Format = "asset"        // .asset
	FormatFont         Format = "font"         // .font
	FormatGFX          Format = "gfx"          // .gfx
	FormatLocalization Format = "localization" // .yml under localization
	FormatImage        Format = "image"        // .dds, .png, .tga
	FormatTable        Format = "table"        // .csv
	FormatDescriptor   Format = "descriptor"   // .mod, metadata.json
	FormatShader       Format = "shader"       // .fxh, .shader
	FormatText         Format = "text"         // any other text: .info, .md, .json, .txt outside the script folders
	FormatBinary       Format = "binary"       // anything the binary test flags
)

// Classify returns the format of a file from its path alone, by its extension and the folder it sits in under its
// root.
func Classify(rel string) Format { panic("stub") }

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

// Walk lists every file under a root. It skips dot folders and anything the ignore rules cover, and it follows a
// link once.
func Walk(root Root, ignore *Ignore) ([]File, error) { panic("stub") }

// IsBinary reports whether the bytes are not text, which means a NUL byte in the first kilobyte.
func IsBinary(bytes []byte) bool { panic("stub") }

// An Ignore is a set of ignore rules with gitignore's meaning: anchoring, negation and a trailing slash for
// folders.
type Ignore struct {
	patterns []pattern
}

// A pattern is one ignore rule: its glob and the three flags gitignore gives a rule.
type pattern struct {
	glob     string
	negates  bool
	dirOnly  bool
	anchored bool
}

// ParseIgnore reads ignore rules from a text, one per line. Blank lines and `#` comments are skipped.
func ParseIgnore(text string) *Ignore { panic("stub") }

// Match reports whether the rules cover a path. The path is relative to the root with forward slashes, and the
// flag says whether it is a folder.
func (ignore *Ignore) Match(rel string, dir bool) bool { panic("stub") }
