package jomini

import "github.com/sergi/go-diff/diffmatchpatch"

// DiffEdits is built on this library.
var _ = diffmatchpatch.New

// An Edit is one change to a text: the span it replaces and the text that goes there. Every edit the engine
// produces is one of these, whether from a quick fix, a rename, the Visual view or Format, and the editor applies
// them to its model.
type Edit struct {
	Span Span   `json:"span"`
	Text string `json:"text"`
}

// ApplyEdits returns the text with the edits applied. The edits must not overlap.
func ApplyEdits(text string, edits []Edit) string { panic("stub") }

// DiffEdits returns the smallest set of edits that turns the first text into the second.
func DiffEdits(before, after string) []Edit { panic("stub") }

// Indentation returns the indentation unit a text uses, taken from its first indented line. It returns "" if no
// line is indented.
func Indentation(text string) string { panic("stub") }
