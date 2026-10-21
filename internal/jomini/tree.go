// Package jomini parses Paradox script, GUI and asset files into a flat tree and tells what shape a written key
// or value has. It knows nothing about any game: what a key means is the schema's job, and where a file lives is
// the files package's.
package jomini

import "iter"

// A Span is a half-open range of byte offsets into a text: Start is the first byte and End is one past the last.
type Span struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// IsEmpty reports whether the span covers no bytes.
func (span Span) IsEmpty() bool { panic("stub") }

// Contains reports whether the given byte offset lies inside the span.
func (span Span) Contains(offset int) bool { panic("stub") }

// A Position is a line and a column, both counted from zero, the column in UTF-8 bytes. Only lsp.Range counts in
// UTF-16.
type Position struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

// An Error is one parse error. Code is one of unclosed-block, unexpected-brace and unclosed-string.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Span    Span   `json:"span"`
}

// Lines is the line index of a text. It records where each line starts, so a byte offset converts to a line and
// column and back. A line ends at \n, \r\n or a lone \r. The tree and the loc file each carry one; nothing else
// builds one.
type Lines struct {
	starts []int
	length int
}

// NewLines builds the line index of a text.
func NewLines(text string) Lines { panic("stub") }

// CountLines returns how many lines the text has. An empty text has one.
func (lines Lines) CountLines() int { panic("stub") }

// ToPosition converts a byte offset to its line and column, clamped to the text.
func (lines Lines) ToPosition(offset int) Position { panic("stub") }

// ToOffset converts a line and column to a byte offset, clamped to the line.
func (lines Lines) ToOffset(position Position) int { panic("stub") }

// LineSpan returns the span of the given line without its line ending.
func (lines Lines) LineSpan(line int) Span { panic("stub") }

// A Statement is one statement of the file: `key op value`, a bare value inside a list, or a block with or
// without a tag. Each part is a span into the source, never a copied string.
type Statement struct {
	Parent int32 // -1 for a top-level statement
	Last   int32 // one past the last descendant, so the children are Statements[i+1:Last]
	Span   Span  // the whole statement
	Key    Span  // empty for a bare value
	Op     Span  // =, ==, !=, <, >, <=, >=, ?=; empty for a bare value
	Tag    Span  // rgb, hsv, list and so on; empty when there is none
	Value  Span  // a scalar's text, quotes included; empty for a block
	Block  Span  // from the opening brace to the closing one; empty for a scalar
}

// IsBlock reports whether the statement's value is a block.
func (statement Statement) IsBlock() bool { panic("stub") }

// IsListed reports whether the statement is a bare value with no key.
func (statement Statement) IsListed() bool { panic("stub") }

// A Tree is the parse of one text. Statements are in document order, and a block's children sit right after it
// in the slice. The tree also keeps the comments, the parse errors and the line index.
type Tree struct {
	Src        string
	Statements []Statement
	Comments   []Span
	Errors     []Error
	Lines      Lines
}

// Text returns the source text the given span covers.
func (tree *Tree) Text(span Span) string { panic("stub") }

// Unquoted returns the source text the given span covers, with its surrounding quotes removed if it has any.
func (tree *Tree) Unquoted(span Span) string { panic("stub") }

// Children yields the direct children of a statement in document order. Passing -1 yields the top-level
// statements.
func (tree *Tree) Children(statement int32) iter.Seq[int32] { panic("stub") }

// FindComments returns the comment lines directly above a statement, nearest last. A blank line or another
// statement in between ends the run, so there may be none.
func (tree *Tree) FindComments(statement int32) []Span { panic("stub") }

// FindStatement returns the innermost statement that covers the given byte offset, or -1 if no statement does.
func (tree *Tree) FindStatement(offset int) int32 { panic("stub") }

// FindChild returns the child of a statement whose key is the given text. If several children have that key, the
// last one wins. It returns -1 if there is none.
func (tree *Tree) FindChild(statement int32, key string) int32 { panic("stub") }
