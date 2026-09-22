// Package lsp answers the editor's requests for one open file: what the word under the cursor is, where it is defined and used, what may be written here, what is wrong, and how the file is folded, formatted and annotated. Every answer starts from one descent to the cursor (index.Probe) and one reading by schema.Reader; the payloads carry that reading (schema.Value) and the frontend templates the text.
//
// protocol.go is the payloads, in the editor's terms: positions are 0-based lines and byte columns of the file's text.
package lsp

import (
	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/schema"
	"paradox-modding-tools/services/internal/source"
)

// Position and Range are the editor's: 0-based line and byte column.
type Position = jomini.Position

// Range is a span of the file.
type Range struct {
	Start, End Position
}

// Location is a place in a file of the workspace or the install.
type Location struct {
	Path   string `json:"path"`
	Origin string `json:"origin"` // game.OriginVanilla or a mod's id
	Rel    string `json:"rel"`
	Range  Range  `json:"range"`
}

// Inspect is the hover card: what the word is, as the reader read it, and what the workspace knows about that.
type Inspect struct {
	Value    schema.Value      `json:"value"`
	Key      string            `json:"key,omitempty"`      // the key the word is written at, or is
	Class    string            `json:"class,omitempty"`    // a key: what is written at it (value, list, structure, database, body)
	Accepts  []string          `json:"accepts,omitempty"`  // a key: the types of value it accepts, as the schema file spells them
	Scope    string            `json:"scope,omitempty"`    // the scope type the site runs in
	ScopeOut string            `json:"scopeOut,omitempty"` // a link or an iterator: the scope type it leads to
	Docs     string            `json:"docs,omitempty"`     // prose: a token's, a field's from the game info, a definition's comment
	Usage    string            `json:"usage,omitempty"`    // a token's declared usage
	Label    string            `json:"label,omitempty"`    // a definition's label (a character's name), a loc key's text, a constant's value
	Weight   int               `json:"weight,omitempty"`   // a numbered branch: its share of its siblings, in percent
	Date     string            `json:"date,omitempty"`     // a date, written out; with the mod's calendar, the era's date too
	Color    string            `json:"color,omitempty"`    // a colour, as #rrggbb
	Image    *source.ImageInfo `json:"image,omitempty"`    // a file that is an image: its header
	Sites    []Location        `json:"sites,omitempty"`    // where it is defined, in load order
	Winner   int               `json:"winner"`             // the site the game keeps; -1 for none
	Values   []Seen            `json:"values,omitempty"`   // a parameter: the words written for it where its macro is called
	Range    Range             `json:"range"`
}

// Seen is one word and how often it was written.
type Seen struct {
	Text  string `json:"text"`
	Count int    `json:"count"`
}

// Signature is a token's declared usage with its parameters.
type Signature struct {
	Label      string   `json:"label"`
	Parameters []string `json:"parameters"`
	Docs       string   `json:"docs,omitempty"`
}
