// Package jomini parses Paradox script, GUI and asset files into a flat tree and tells what shape a written key
// or value has. It knows nothing about any game: what a key means is the schema's job, and where a file lives is
// the files package's.
package jomini

import (
	"iter"
	"slices"
	"sort"
	"strings"
)

// A Span is a half-open range of byte offsets into a text: Start is the first byte and End is one past the last.
type Span struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// IsEmpty reports whether the span covers no bytes.
func (span Span) IsEmpty() bool { return span.Start == span.End }

// Contains reports whether the given byte offset lies inside the span.
func (span Span) Contains(offset int) bool { return span.Start <= offset && offset < span.End }

// A Position is a line and a column, both counted from zero, the column in UTF-8 bytes. Only lsp.Range counts in
// UTF-16.
type Position struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

// An Error is one parse error. Code is one of unclosed-block, unexpected-brace and unclosed-string, or
// malformed-line in a localization file.
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
	ends   []int
	length int
}

// NewLines builds the line index of a text.
func NewLines(text string) Lines {
	lines := Lines{starts: []int{0}, length: len(text)}
	for i := 0; i < len(text); i++ {
		if text[i] != '\n' && text[i] != '\r' {
			continue
		}
		lines.ends = append(lines.ends, i)
		if text[i] == '\r' && i+1 < len(text) && text[i+1] == '\n' {
			i++
		}
		lines.starts = append(lines.starts, i+1)
	}
	lines.ends = append(lines.ends, len(text))
	return lines
}

// CountLines returns how many lines the text has. An empty text has one.
func (lines Lines) CountLines() int { return len(lines.starts) }

// ToPosition converts a byte offset to its line and column, clamped to the text.
func (lines Lines) ToPosition(offset int) Position {
	offset = min(max(offset, 0), lines.length)
	line := sort.Search(len(lines.starts), func(i int) bool { return lines.starts[i] > offset }) - 1
	return Position{Line: line, Column: offset - lines.starts[line]}
}

// ToOffset converts a line and column to a byte offset, clamped to the line.
func (lines Lines) ToOffset(position Position) int {
	line := min(max(position.Line, 0), len(lines.starts)-1)
	return min(lines.starts[line]+max(position.Column, 0), lines.ends[line])
}

// LineSpan returns the span of the given line without its line ending.
func (lines Lines) LineSpan(line int) Span {
	return Span{Start: lines.starts[line], End: lines.ends[line]}
}

// A Statement is one statement of the file: `key op value`, a bare value inside a list, or a block with or
// without a tag. Each part is a span into the source, never a copied string.
type Statement struct {
	Parent int32 // the statement that holds it; 0, the file, for a top-level statement and for the file itself
	Last   int32 // one past the last descendant, so the children are Statements[i+1:Last]
	Span   Span  // the whole statement
	Key    Span  // empty for a bare value
	Op     Span  // =, ==, !=, <, >, <=, >=, ?=; empty for a bare value
	Tag    Span  // rgb, hsv, list and so on; empty when there is none
	Value  Span  // a scalar's text, quotes included; empty for a block
	Block  Span  // from the opening brace to the closing one; empty for a scalar
}

// IsBlock reports whether the statement's value is a block.
func (statement Statement) IsBlock() bool { return !statement.Block.IsEmpty() }

// IsListed reports whether the statement is a bare value with no key.
func (statement Statement) IsListed() bool { return statement.Key.IsEmpty() }

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
func (tree *Tree) Text(span Span) string { return tree.Src[span.Start:span.End] }

// Unquoted returns the source text the given span covers, with its surrounding quotes removed if it has any.
func (tree *Tree) Unquoted(span Span) string {
	return strings.TrimSuffix(strings.TrimPrefix(tree.Text(span), `"`), `"`)
}

// Children yields the direct children of a statement in document order. For the file, which is the first statement,
// they are the top-level statements.
func (tree *Tree) Children(statement int32) iter.Seq[int32] {
	return func(yield func(int32) bool) {
		child, last := statement+1, tree.Statements[statement].Last
		for child < last {
			if !yield(child) {
				return
			}
			child = tree.Statements[child].Last
		}
	}
}

// FindComments returns the comment lines directly above a statement, nearest last. A blank line or another
// statement in between ends the run, so there may be none.
func (tree *Tree) FindComments(statement int32) []Span {
	start := tree.Statements[statement].Span.Start
	line := tree.Lines.ToPosition(start).Line
	first := sort.Search(len(tree.Comments), func(i int) bool { return tree.Comments[i].Start >= start })
	var comments []Span
	for i := first - 1; i >= 0; i-- {
		span := tree.Comments[i]
		commentLine := tree.Lines.ToPosition(span.Start).Line
		// the comment must sit on the line just above the last one taken, with nothing but spaces before it
		lead := tree.Src[tree.Lines.ToOffset(Position{Line: commentLine}):span.Start]
		if commentLine != line-1 || strings.TrimSpace(lead) != "" {
			break
		}
		comments = append(comments, span)
		line = commentLine
	}
	slices.Reverse(comments)
	return comments
}

// FindStatement returns the innermost statement that covers the given byte offset. It returns 0, the file, if no
// other statement does.
func (tree *Tree) FindStatement(offset int) int32 {
	// the statements are in pre-order, so the last one starting at or before the offset is the deepest candidate
	statement := int32(sort.Search(len(tree.Statements)-1, func(i int) bool {
		return tree.Statements[i+1].Span.Start > offset
	}))
	for statement > 0 && !tree.Statements[statement].Span.Contains(offset) {
		statement = tree.Statements[statement].Parent
	}
	return statement
}

// LookupStatement returns the statement that starts at the given byte offset. It returns false if none does.
func (tree *Tree) LookupStatement(start int) (int32, bool) {
	statement := tree.FindStatement(start)
	return statement, statement > 0 && tree.Statements[statement].Span.Start == start
}

// FindChild returns the child of a statement whose key is the given text. If several children have that key, the
// last one wins. It returns false if there is none.
func (tree *Tree) FindChild(statement int32, key string) (int32, bool) {
	match, ok := int32(0), false
	for child := range tree.Children(statement) {
		if tree.Text(tree.Statements[child].Key) == key {
			match, ok = child, true
		}
	}
	return match, ok
}

// IsEqual reports whether a statement writes the same as a statement of another tree: the same keys, operators,
// tags and values, nested the same way. Comments, spacing and quotes do not count.
func (tree *Tree) IsEqual(statement int32, other *Tree, otherStatement int32) bool {
	count := tree.Statements[statement].Last - statement
	if other.Statements[otherStatement].Last-otherStatement != count {
		return false
	}
	for offset := range count {
		current, compared := tree.Statements[statement+offset], other.Statements[otherStatement+offset]
		// the first statement's parent lies outside what is compared
		if offset > 0 && current.Parent-statement != compared.Parent-otherStatement {
			return false
		}
		if current.IsBlock() != compared.IsBlock() || tree.Unquoted(current.Key) != other.Unquoted(compared.Key) ||
			tree.Text(current.Op) != other.Text(compared.Op) || tree.Text(current.Tag) != other.Text(compared.Tag) ||
			tree.Unquoted(current.Value) != other.Unquoted(compared.Value) {
			return false
		}
	}
	return true
}
