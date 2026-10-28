// Package lsp is the editor's language service. It answers what the editor asks about a file: hover cards,
// completions, definitions, diagnostics, tokens, folds and the rest. Every range is in the editor's own form.
package lsp

import (
	"unicode/utf16"

	"github.com/samber/lo"

	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/services/workspace"
)

// Service is the language service. Each method asks the workspace service for the open session when it is called
// and keeps nothing between calls.
type Service struct {
	Workspace *workspace.Service
}

// A Range is a span as Monaco counts it: lines and columns from one, the column in UTF-16 code units, the end
// exclusive.
type Range struct {
	StartLine   int `json:"startLineNumber"`
	StartColumn int `json:"startColumn"`
	EndLine     int `json:"endLineNumber"`
	EndColumn   int `json:"endColumn"`
}

// A Location is a range in a file, with where the file sits in the workspace.
type Location struct {
	Place workspace.Place `json:"place"`
	Range Range           `json:"range"`
	Text  string          `json:"text"` // the text of the line, "" where the file cannot be read
}

// An Edit is one change to a file's text: the range replaced and the text that replaces it.
type Edit struct {
	Range Range  `json:"range"`
	Text  string `json:"text"`
}

// ToRange converts a byte span in a text to a Monaco range, using the text's line index.
func ToRange(text string, lines jomini.Lines, span jomini.Span) Range {
	start, end := lines.ToPosition(span.Start), lines.ToPosition(span.End)
	return Range{
		StartLine: start.Line + 1, StartColumn: countUnits(text, lines, start) + 1,
		EndLine: end.Line + 1, EndColumn: countUnits(text, lines, end) + 1,
	}
}

// ToSpan converts a Monaco range to a byte span in a text, using the text's line index.
func ToSpan(text string, lines jomini.Lines, bounds Range) jomini.Span {
	return jomini.Span{
		Start: ToOffset(text, lines, bounds.StartLine, bounds.StartColumn),
		End:   ToOffset(text, lines, bounds.EndLine, bounds.EndColumn),
	}
}

// ToLineRange returns the empty range at the start of a line, counted from one.
func ToLineRange(line int) Range {
	return Range{StartLine: line, StartColumn: 1, EndLine: line, EndColumn: 1}
}

// ToOffset converts a line and a column as Monaco counts them to a byte offset in the text.
func ToOffset(text string, lines jomini.Lines, line, column int) int {
	span := lines.LineSpan(min(max(line-1, 0), lines.CountLines()-1))
	units := 0
	for offset, character := range text[span.Start:span.End] {
		if units >= column-1 {
			return span.Start + offset
		}
		units += utf16.RuneLen(character)
	}
	return span.End
}

// ToEdits converts the engine's byte-offset edits to Monaco edits over a text.
func ToEdits(text string, lines jomini.Lines, edits []jomini.Edit) []Edit {
	return lo.Map(edits, func(edit jomini.Edit, _ int) Edit {
		return Edit{Range: ToRange(text, lines, edit.Span), Text: edit.Text}
	})
}

// countUnits returns how many UTF-16 code units of its line lie before a position. A character above U+FFFF counts
// two.
func countUnits(text string, lines jomini.Lines, position jomini.Position) int {
	start := lines.LineSpan(position.Line).Start
	units := 0
	for _, character := range text[start : start+position.Column] {
		units += utf16.RuneLen(character)
	}
	return units
}

// SetText gives the session the text the editor holds for an open file. It returns the file's problems.
func (lsp *Service) SetText(path, text string) ([]Diagnostic, error) {
	ws, err := lsp.Workspace.Session()
	if err != nil {
		return nil, err
	}
	ws.SetText(path, text)
	return lsp.Diagnose(path)
}

// CloseFile drops an open file's text from the session.
func (lsp *Service) CloseFile(path string) error {
	ws, err := lsp.Workspace.Session()
	if err != nil {
		return err
	}
	ws.CloseBuffer(path)
	return nil
}

// loadTree returns the open session and the tree of a file. It parses the editor's text if the file is open. It returns
// an error if there is no session or the file cannot be read.
func (lsp *Service) loadTree(path string) (*workspace.Session, *jomini.Tree, error) {
	ws, err := lsp.Workspace.Session()
	if err != nil {
		return nil, nil, err
	}
	tree, err := ws.LoadTree(path)
	return ws, tree, err
}
