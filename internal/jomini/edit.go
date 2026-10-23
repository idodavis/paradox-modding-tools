package jomini

import (
	"cmp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/sergi/go-diff/diffmatchpatch"
)

// An Edit is one change to a text: the span it replaces and the text that goes there. Every edit the engine
// produces is one of these, whether from a quick fix, a rename, the Visual view or Format, and the editor applies
// them to its model.
type Edit struct {
	Span Span   `json:"span"`
	Text string `json:"text"`
}

// ApplyEdits returns the text with the edits applied. The edits must not overlap.
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

// DiffEdits returns the smallest set of edits that turns the first text into the second.
func DiffEdits(before, after string) []Edit {
	if !utf8.ValidString(before) || !utf8.ValidString(after) {
		// the diff rewrites an invalid byte as U+FFFD and every later offset drifts, so the whole text is replaced
		return []Edit{{Span: Span{Start: 0, End: len(before)}, Text: after}}
	}
	var edits []Edit
	offset := 0
	for _, diff := range diffmatchpatch.New().DiffMain(before, after, true) {
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

// Indentation returns the indentation unit a tree's text uses: the indentation of its first indented statement shared
// out over the blocks that statement is inside. It returns "" if no statement is indented.
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

// IndentLines returns lines joined by the line ending of the tree's text, each after the given indentation, and the
// lines inside braces one unit deeper. The unit is the tree's own, or a tab if it has none.
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

// BuildInsertion returns the edit that adds lines as the last statements of a block. The lines go before the closing
// brace on its own line, indented one unit deeper than the brace's line. If other text shares the brace's line, they
// go on that line instead, a space apart from what is before them.
func (tree *Tree) BuildInsertion(holder int32, lines []string) Edit {
	closing := tree.Statements[holder].Block.End - 1
	row := tree.Lines.LineSpan(tree.Lines.ToPosition(closing).Line)
	if before := tree.Src[row.Start:closing]; strings.TrimSpace(before) != "" {
		text := strings.Join(lines, " ") + " "
		if strings.TrimRight(before, " \t") == before {
			text = " " + text
		}
		return Edit{Span: Span{Start: closing, End: closing}, Text: text}
	}
	indent := tree.Src[row.Start:closing] + cmp.Or(tree.Indentation(), "\t")
	return Edit{Span: Span{Start: row.Start, End: row.Start}, Text: tree.IndentLines(lines, indent) + LineEnding(tree.Src)}
}
