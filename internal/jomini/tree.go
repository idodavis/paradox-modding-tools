// Package jomini parses Paradox script, GUI and asset files into a flat tree and tells what shape a written key
// or value has. It knows nothing about any game.
package jomini

import (
	"iter"
	"sort"
	"strings"
)

// A Span is a half-open range of bytes in a text. It includes its first byte and not the byte at its end.
type Span struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

// IsEmpty reports whether the span covers no bytes.
func (span Span) IsEmpty() bool { return span.Start == span.End }

// Contains reports whether the given byte offset lies inside the span.
func (span Span) Contains(offset int) bool { return span.Start <= offset && offset < span.End }

// A Position is a place in a text, as a line and a column. Both count from zero, and the column counts UTF-8 bytes.
type Position struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

// An Error is one error a parse found in a text, with the span it covers.
type Error struct {
	// Code is unclosed-block, unexpected-brace, unclosed-string or missing-value from a script parse. The loc parser's
	// codes are malformed-line, loc-content-before-header and loc-unterminated-value.
	Code    string `json:"code"`
	Message string `json:"message"`
	Span    Span   `json:"span"`
}

// Lines is the line index of a text. It records where each line starts, so a byte offset converts to a line and a
// column. A line ends at \n, \r\n or a lone \r.
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

// LineSpan returns the span of the given line, counted from zero, without its line ending.
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

// IsListed reports whether a statement is a bare value with no key, such as one name in a list. A bare block is
// not one.
func (statement Statement) IsListed() bool { return statement.Key.IsEmpty() && !statement.IsBlock() }

// ResolveValueSpan returns the span of what a statement writes after its operator: its tag, value and block. It is
// empty for a statement that writes none of them.
func (statement Statement) ResolveValueSpan() Span {
	for _, part := range []Span{statement.Tag, statement.Value, statement.Block} {
		if !part.IsEmpty() {
			return Span{Start: part.Start, End: max(statement.Value.End, statement.Block.End)}
		}
	}
	return Span{}
}

// A Tree is the parse of one text. Its statements are in document order, and a block's children sit right after it.
// It also keeps the text's comments, parse errors and line index.
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
func (tree *Tree) Unquoted(span Span) string { return tree.Text(tree.UnquotedSpan(span)) }

// UnquotedSpan returns the given span without its surrounding quotes, if it has any. A span of one quote gives an
// empty span.
func (tree *Tree) UnquotedSpan(span Span) Span {
	text := tree.Text(span)
	if strings.HasPrefix(text, `"`) {
		span.Start++
	}
	if strings.HasSuffix(text, `"`) && span.End > span.Start {
		span.End--
	}
	return span
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

// FindPrevious returns the statement written just before a statement in the block that holds it. It returns false if
// the statement is the block's first.
func (tree *Tree) FindPrevious(statement int32) (int32, bool) {
	previous := int32(0)
	for child := range tree.Children(tree.Statements[statement].Parent) {
		if child == statement {
			break
		}
		previous = child
	}
	return previous, previous > 0
}

// FindStatement returns the innermost statement that covers the given byte offset. It returns 0, the file, if no
// other statement does.
func (tree *Tree) FindStatement(offset int) int32 {
	// The statements are in pre-order, so the last one starting at or before the offset is the deepest candidate.
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

// FindChildValue returns the unquoted value of a statement's child with the given key. Where several children have the
// key, the last one counts. It returns false if there is none or the key is empty.
func (tree *Tree) FindChildValue(statement int32, key string) (string, bool) {
	if key == "" {
		return "", false
	}
	child, ok := tree.FindChild(statement, key)
	if !ok {
		return "", false
	}
	return tree.Unquoted(tree.Statements[child].Value), true
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
		// The first statement's parent lies outside what is compared.
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
