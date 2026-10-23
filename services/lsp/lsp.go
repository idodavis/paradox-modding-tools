// Package lsp is the editor's language service. Its methods answer what Monaco's providers ask about a file: hover
// cards, completions, definitions, diagnostics, tokens, folds and the rest, each computed by a visitor on the
// schema's walk. Every range is in Monaco's own form. The editor sends an open file's text through SetText, which
// answers with the file's problems.
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
// exclusive. It is the only place UTF-16 exists; every other package uses byte offsets.
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

// SetText gives the session an open file's text as the editor has it, and returns the file's problems: its parse
// errors and every verdict that fails on it.
func (lsp *Service) SetText(path, text string) ([]Diagnostic, error) {
	ws, err := lsp.Workspace.Session()
	if err != nil {
		return nil, err
	}
	ws.SetText(path, text)
	diagnostics, err := DiagnoseFile(ws, ws.Mods.Load(), path)
	return lsp.listEnabled(diagnostics), err
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

// loadTree returns the open session and the tree of a file, the open text's where the editor has the file. It returns
// the error of the session or of reading the file.
func (lsp *Service) loadTree(path string) (*workspace.Session, *jomini.Tree, error) {
	ws, err := lsp.Workspace.Session()
	if err != nil {
		return nil, nil, err
	}
	tree, err := ws.LoadTree(path)
	return ws, tree, err
}
