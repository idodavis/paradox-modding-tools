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

// Indentation returns the indentation unit a text uses, taken from its first indented line. It returns "" if no
// line is indented.
func Indentation(text string) string {
	for line := range strings.Lines(text) {
		content := strings.TrimLeft(line, " \t")
		if len(content) < len(line) && strings.TrimSpace(content) != "" {
			return line[:len(line)-len(content)]
		}
	}
	return ""
}
