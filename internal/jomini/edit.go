// edit.go applies changes to a text and finds the changes between two texts. It also reads a text's line ending and
// indentation, and builds the change that adds lines to a block.

package jomini

import (
	"cmp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/sergi/go-diff/diffmatchpatch"
)

// An Edit is one change to a text. It replaces a span with a text, so an empty span inserts and an empty text
// deletes.
type Edit struct {
	Span Span   `json:"span"`
	Text string `json:"text"`
}

// ApplyEdits returns the text with the edits applied. It panics if two edits overlap.
func ApplyEdits(text string, edits []Edit) string {
	edits = slices.SortedStableFunc(slices.Values(edits), func(a, b Edit) int {
		return cmp.Or(cmp.Compare(a.Span.Start, b.Span.Start), cmp.Compare(a.Span.End, b.Span.End))
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

// DiffEdits returns the smallest list of edits that turns the old text into the new one.
func DiffEdits(oldText, newText string) []Edit {
	if !utf8.ValidString(oldText) || !utf8.ValidString(newText) {
		// The diff rewrites an invalid byte as U+FFFD and every later offset drifts, so the whole text is replaced.
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

// LineEnding returns the line ending a text uses: CRLF where it has one, else LF.
func LineEnding(text string) string {
	if strings.Contains(text, "\r\n") {
		return "\r\n"
	}
	return "\n"
}

// Indentation returns the string that one level of nesting is indented with in the tree's text, such as a tab or four
// spaces. It returns "" if no statement is indented.
func (tree *Tree) Indentation() string {
	for statement := int32(1); int(statement) < len(tree.Statements); statement++ {
		start := tree.Statements[statement].Span.Start
		indent := tree.Src[tree.Lines.LineSpan(tree.Lines.ToPosition(start).Line).Start:start]
		depth := 0
		for parent := tree.Statements[statement].Parent; parent > 0; parent = tree.Statements[parent].Parent {
			depth++
		}
		if depth > 0 && indent != "" && strings.TrimSpace(indent) == "" {
			return indent[:len(indent)/depth]
		}
	}
	return ""
}

// IndentLines joins the lines with the tree's line ending, each starting with the given indent. A line inside braces
// goes one level deeper. A level is the tree's own indentation, or a tab if it has none.
func (tree *Tree) IndentLines(lines []string, indent string) string {
	unit, depth := cmp.Or(tree.Indentation(), "\t"), 0
	indented := make([]string, len(lines))
	for position, line := range lines {
		levels := depth
		if strings.HasPrefix(line, "}") {
			levels--
		}
		depth += strings.Count(line, "{") - strings.Count(line, "}")
		indented[position] = indent + strings.Repeat(unit, max(levels, 0)) + line
	}
	return strings.Join(indented, LineEnding(tree.Src))
}

// BuildInsertion returns the edit that adds the lines as the last statements of the given block statement. The lines
// go before the closing brace, each on its own line, one level deeper than the brace's line. If other text comes
// before the brace on its line, the lines go on that line instead, separated by spaces.
func (tree *Tree) BuildInsertion(block int32, lines []string) Edit {
	closing := tree.Statements[block].Block.End - 1
	row := tree.Lines.LineSpan(tree.Lines.ToPosition(closing).Line)
	if leading := tree.Src[row.Start:closing]; strings.TrimSpace(leading) != "" {
		text := strings.Join(lines, " ") + " "
		if strings.TrimRight(leading, " \t") == leading {
			text = " " + text
		}
		return Edit{Span: Span{Start: closing, End: closing}, Text: text}
	}
	indent := tree.Src[row.Start:closing] + cmp.Or(tree.Indentation(), "\t")
	return Edit{Span: Span{Start: row.Start, End: row.Start}, Text: tree.IndentLines(lines, indent) + LineEnding(tree.Src)}
}
