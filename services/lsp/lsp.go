// Package lsp is the editor's language service. It answers what the editor asks about a file: hover cards,
// completions, definitions, diagnostics, tokens, folds and the rest. Every range is in the editor's own form.
package lsp

import (
	"strings"
	"unicode/utf16"

	"github.com/samber/lo"

	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/services/workspace"
)

// Service is the language service. It asks the workspace service for the open session at each call and keeps nothing.
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

// A Site is one place in a file that an answer points at: the file's place and a line. Where the answer has them, it
// also has a range, the text of the line, and whether the game keeps the definition and why. A whole-file site lists
// the names the file defines.
type Site struct {
	Place   workspace.Place `json:"place"`
	Line    int             `json:"line"`           // counted from one
	Range   Range           `json:"range"`          // the zero range where the answer has none
	Text    string          `json:"text,omitempty"` // the text of the line, where the answer read the file
	Kept    bool            `json:"kept"`
	Reason  index.Reason    `json:"reason,omitempty"` // as index.ResolveSite gives it; unsettled where no source says
	Label   string          `json:"label,omitempty"`  // the reason in words, naming the mod that hides a copy
	Defines []string        `json:"defines,omitempty"`
	Holder  string          `json:"holder,omitempty"` // the name of the definition that holds a copy the game keeps whole
}

// MakeSite returns the site of an occurrence in the mods index: its place and its line. It reads no file, so the
// site has no range and no text.
func MakeSite(ws *workspace.Session, mods *index.Index, site index.Site) Site {
	return Site{Place: ws.Locate(mods, mods.File(site).Path), Line: int(mods.Occurrence(site).Line) + 1}
}

// MakeSites returns the sites of a name's definitions, each with whether the winner keeps it, why, and the reason in
// words. A nil winner keeps none. A definition written inside another one, in a database whose copies the game all
// keeps, also has the name of the one that holds it.
func MakeSites(ws *workspace.Session, mods *index.Index, definitions []index.Site, winner *index.Winner) []Site {
	return lo.Map(definitions, func(definition index.Site, _ int) Site {
		site := MakeSite(ws, mods, definition)
		site.Holder = findHolder(ws, mods, definition)
		var hider string
		if winner != nil {
			site.Kept, site.Reason, hider = mods.ResolveSite(*winner, definition)
		} else {
			site.Reason, hider = mods.FindHider(definition.File)
		}
		site.Label = strings.ReplaceAll(index.Reasons[site.Reason].Label, "{mod}", ws.DescribeMod(hider))
		return site
	})
}

// findHolder returns the name of the definition that holds a definition, where the game keeps every copy of the name.
// It returns "" for a definition at the top of its file or in a database that keeps one copy.
func findHolder(ws *workspace.Session, mods *index.Index, definition index.Site) string {
	database := ws.Schema.Databases[mods.ToReference(definition).Database]
	holder, nested := mods.FindDefinition(definition)
	if database == nil || !database.Precedence.KeepsEvery() || !nested {
		return ""
	}
	return mods.ToReference(holder).Name
}

// LocateSite returns the site of an occurrence with the range of its name and the text of its line. The range stays
// empty and the text "" if the file cannot be read.
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
		StartLine: start.Line + 1, StartColumn: countColumn(text, lines, start) + 1,
		EndLine: end.Line + 1, EndColumn: countColumn(text, lines, end) + 1,
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

// CountUnits returns the length of a text in UTF-16 code units, the units Monaco counts columns in. A character above
// U+FFFF counts two.
func CountUnits(text string) int {
	units := 0
	for _, character := range text {
		units += utf16.RuneLen(character)
	}
	return units
}

// countColumn returns the column of a position as Monaco counts it from zero: how many UTF-16 code units of its line
// lie before it.
func countColumn(text string, lines jomini.Lines, position jomini.Position) int {
	start := lines.LineSpan(position.Line).Start
	return CountUnits(text[start : start+position.Column])
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

// CloseFile drops an open file's text from the session. It does nothing if no workspace is open.
func (lsp *Service) CloseFile(path string) {
	if ws, err := lsp.Workspace.Session(); err == nil {
		ws.CloseBuffer(path)
	}
}

// loadTree returns the open session and the tree of a file, from the editor's text if the file is open. It returns an
// error if there is no session or the file cannot be read.
func (lsp *Service) loadTree(path string) (*workspace.Session, *jomini.Tree, error) {
	ws, err := lsp.Workspace.Session()
	if err != nil {
		return nil, nil, err
	}
	tree, err := ws.LoadTree(path)
	return ws, tree, err
}
