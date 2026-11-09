// edit.go applies edits to a text, finds the edits between two texts and
// builds the edit that adds lines to a block.

package jomini

import (
	"cmp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/sergi/go-diff/diffmatchpatch"
)

// Edit is one change to a text. It replaces a span with a text, so an empty
// span inserts and an empty text deletes.
type Edit struct {
	Span Span   `json:"span"`
	Text string `json:"text"`
}

// ApplyEdits returns the text with the edits applied. It panics if two edits
// overlap.
func ApplyEdits(text string, edits []Edit) string {
	edits = slices.SortedStableFunc(slices.Values(edits), func(first, second Edit) int {
		return cmp.Or(cmp.Compare(first.Span.Start, second.Span.Start), cmp.Compare(first.Span.End, second.Span.End))
	})

	var result strings.Builder
	cursor := 0

	for _, edit := range edits {
		if edit.Span.Start < cursor {
			panic("jomini: edits overlap")
		}

		result.WriteString(text[cursor:edit.Span.Start])
		result.WriteString(edit.Text)
		cursor = edit.Span.End
	}

	result.WriteString(text[cursor:])

	return result.String()
}

// DiffEdits returns the smallest list of edits that turns the old text into
// the new one. Where either text is not valid UTF-8, it replaces the whole
// text, since the diff writes an invalid byte as U+FFFD and every later
// offset would move.
func DiffEdits(oldText, newText string) []Edit {
	if !utf8.ValidString(oldText) || !utf8.ValidString(newText) {
		return []Edit{{Span: Span{Start: 0, End: len(oldText)}, Text: newText}}
	}

	var edits []Edit
	offset := 0

	for _, diff := range diffmatchpatch.New().DiffMain(oldText, newText, true) {
		switch diff.Type {
		case diffmatchpatch.DiffDelete:
			edits = append(edits, Edit{Span: Span{Start: offset, End: offset + len(diff.Text)}})
			offset += len(diff.Text)

		case diffmatchpatch.DiffInsert:
			edits = append(edits, Edit{Span: Span{Start: offset, End: offset}, Text: diff.Text})

		case diffmatchpatch.DiffEqual:
			offset += len(diff.Text)
		}
	}

	return edits
}

// LineEnding returns the line ending a text uses: CRLF where it has one, else
// LF.
func LineEnding(text string) string {
	if strings.Contains(text, "\r\n") {
		return "\r\n"
	}

	return "\n"
}

// Indentation returns the string one level of nesting is indented with in the
// tree's text. It returns "" if no statement is indented.
func (t *Tree) Indentation() string {
	for statement := int32(1); int(statement) < len(t.Statements); statement++ {
		start := t.Statements[statement].Span.Start
		indent := t.Src[t.Lines.LineSpan(t.Lines.ToPosition(start).Line).Start:start]

		depth := 0
		for parent := t.Statements[statement].Parent; parent > 0; parent = t.Statements[parent].Parent {
			depth++
		}

		if depth > 0 && indent != "" && strings.TrimSpace(indent) == "" {
			return indent[:len(indent)/depth]
		}
	}

	return ""
}

// IndentLines joins the lines with the tree's line ending, each starting with
// the given indent and one more level for each brace open above it. A level
// is the tree's own indentation, or a tab if it has none.
func (t *Tree) IndentLines(lines []string, indent string) string {
	unit, depth := cmp.Or(t.Indentation(), "\t"), 0
	indented := make([]string, len(lines))

	for position, line := range lines {
		levels := depth
		if strings.HasPrefix(line, "}") {
			levels--
		}

		depth += strings.Count(line, "{") - strings.Count(line, "}")
		indented[position] = indent + strings.Repeat(unit, max(levels, 0)) + line
	}

	return strings.Join(indented, LineEnding(t.Src))
}

// BuildInsertion returns the edit that adds the lines as the last statements
// of a block statement. If other text comes before the closing brace on its
// line, the lines go on that line instead of their own.
func (t *Tree) BuildInsertion(block int32, lines []string) Edit {
	closing := t.Statements[block].Block.End - 1
	row := t.Lines.LineSpan(t.Lines.ToPosition(closing).Line)

	if leading := t.Src[row.Start:closing]; strings.TrimSpace(leading) != "" {
		text := strings.Join(lines, " ") + " "
		if strings.TrimRight(leading, " \t") == leading {
			text = " " + text
		}

		return Edit{Span: Span{Start: closing, End: closing}, Text: text}
	}

	indent := t.Src[row.Start:closing] + cmp.Or(t.Indentation(), "\t")

	return Edit{Span: Span{Start: row.Start, End: row.Start}, Text: t.IndentLines(lines, indent) + LineEnding(t.Src)}
}
