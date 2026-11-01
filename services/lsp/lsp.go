// Package lsp is the editor's language service. It answers what the editor asks about a file: hover cards,
// completions, definitions, diagnostics, tokens, folds and the rest. Every range is in the editor's own form.
package lsp

import (
	"unicode/utf16"

	"github.com/samber/lo"

	"paradox-modding-tools/internal/index"
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

// An Edit is one change to a file's text: the range replaced and the text that replaces it.
type Edit struct {
	Range Range  `json:"range"`
	Text  string `json:"text"`
}

// A Site is one place in a file that an answer points at: where the file sits and a line. Where the answer has them,
// it also has a range in the editor's form and the text of the line. Where the answer judged a definition, it says
// whether the game keeps it and why. A whole-file site lists the names the file defines.
type Site struct {
	Place   workspace.Place `json:"place"`
	Line    int             `json:"line"`           // counted from one
	Range   Range           `json:"range"`          // the zero range where the answer has none
	Text    string          `json:"text,omitempty"` // the text of the line, where the answer read the file
	Kept    bool            `json:"kept"`
	Reason  index.Reason    `json:"reason,omitempty"` // as index.ResolveSite gives it; unsettled where no source says
	Defines []string        `json:"defines,omitempty"`
}

// MakeSite returns the site of an occurrence, as the given index of the mods lists it: its place and its line. It
// reads no file, so it has no range and no text.
func MakeSite(ws *workspace.Session, mods *index.Index, site index.Site) Site {
	return Site{Place: ws.Locate(mods, mods.File(site).Path), Line: int(mods.Occurrence(site).Line) + 1}
}

// MakeSites returns the sites of a name's definitions, each with whether a winner keeps it and why, as
// index.ResolveSite says. A nil winner keeps none.
func MakeSites(ws *workspace.Session, mods *index.Index, definitions []index.Site, winner *index.Winner) []Site {
	return lo.Map(definitions, func(definition index.Site, _ int) Site {
		site := MakeSite(ws, mods, definition)
		if winner != nil {
			site.Kept, site.Reason = mods.ResolveSite(*winner, definition)
		}
		return site
	})
}

// LocateSite returns the site of an occurrence with the range of its name and the text of the line it starts on,
// read from the file's tree. The range stays empty and the text "" if the file cannot be read.
func LocateSite(ws *workspace.Session, mods *index.Index, handle index.Site) Site {
	site := MakeSite(ws, mods, handle)
	if tree, err := ws.LoadTree(site.Place.Path); err == nil {
		span := mods.Occurrence(handle).Span
		line := tree.Lines.LineSpan(tree.Lines.ToPosition(span.Start).Line)
		site.Range, site.Text = ToRange(tree.Src, tree.Lines, span), tree.Src[line.Start:line.End]
	}
	return site
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
