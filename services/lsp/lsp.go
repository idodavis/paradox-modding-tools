// Package lsp is the editor's language service. It answers what the editor asks
// about a file: hover cards, completions, definitions, diagnostics, tokens,
// folds and the rest. Every range is in the editor's own form.
package lsp

import (
	"cmp"
	"strings"

	"github.com/samber/lo"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/schema"
	"paradox-modding-tools/services/workspace"
)

// Service is the language service. It asks the workspace service for the
// open session at each call, and keeps nothing itself.
type Service struct {
	Workspace *workspace.Service
}

// An Edit is one change to a file's text: the range replaced and the text that
// replaces it.
type Edit struct {
	Range jomini.Range `json:"range"`
	Text  string       `json:"text"`
}

// A Site is one place in a file that an answer points at: the file's place
// and a line. Where the answer has them, it also has a range, the text of
// the line, and whether the game keeps the definition and why. A whole-file
// site lists the names the file defines.
type Site struct {
	Place workspace.Place `json:"place"`

	// Line counts from one. Range is the zero range where the answer has
	// none, and Text the text of the line where the answer read the file.
	Line  int          `json:"line"`
	Range jomini.Range `json:"range"`
	Text  string       `json:"text,omitempty"`

	// Kept is whether the game keeps the definition, and Reason why, as
	// index.ResolveSite gives it: unsettled where no source says. A copy in
	// a file the game does not read has no reason; its place says what
	// hides the file.
	Kept   bool        `json:"kept"`
	Reason game.Reason `json:"reason,omitempty"`

	// Label is the reason in words, or what hides the copy's file, naming
	// the mod that hides it.
	Label   string   `json:"label,omitempty"`
	Defines []string `json:"defines,omitempty"`

	// Holder is the name of the definition that holds a copy the game keeps
	// whole.
	Holder string `json:"holder,omitempty"`
}

// hidingLabels says in words why the game does not read a file, with the mod
// that hides it at {mod}.
var hidingLabels = map[files.Hiding]string{
	files.HidingShadowed:  "The copy of this file in {mod} replaces it",
	files.HidingDropped:   "A replace_path in {mod} drops this folder",
	files.HidingUnmounted: "The game reads no file in this folder",
}

// MakeSite returns the site of an occurrence in the mods index: its place
// and its line. It reads no file, so the site has no range and no text.
func MakeSite(ws *workspace.Session, mods *index.Index, site index.Site) Site {
	return Site{Place: ws.Locate(mods, mods.File(site).Path), Line: int(mods.Occurrence(site).Line) + 1}
}

// MakeSites returns the sites of a name's definitions, each with whether the
// winner keeps it, why, and the reason in words. A nil winner keeps none. A
// copy in a file the game does not read says what hides the file. A
// definition written inside another one, in a database whose copies the game
// all keeps, also has the name of the one that holds it.
func MakeSites(ws *workspace.Session, mods *index.Index, definitions []index.Site, winner *index.Winner) []Site {
	return lo.Map(definitions, func(definition index.Site, _ int) Site {
		site := MakeSite(ws, mods, definition)
		site.Holder = findHolder(ws, mods, definition)
		if winner != nil {
			outcome := mods.ResolveSite(*winner, definition)
			site.Kept, site.Reason = outcome.Kept, outcome.Reason
		}

		label := cmp.Or(hidingLabels[site.Place.Hiding], game.Reasons[site.Reason].Label)
		site.Label = strings.ReplaceAll(label, "{mod}", ws.DescribeMod(site.Place.Hider))

		return site
	})
}

// findHolder returns the name of the definition that holds a definition, where
// the game keeps every copy of the name. It returns "" for a definition at the
// top of its file or in a database that keeps one copy.
func findHolder(ws *workspace.Session, mods *index.Index, definition index.Site) string {
	database := ws.Schema.Databases[mods.ToReference(definition).Database]
	holder, nested := mods.FindDefinition(definition)
	if database == nil || !database.Precedence.KeepsEvery() || !nested {
		return ""
	}
	return mods.ToReference(holder).Name
}

// LocateSite returns the site of an occurrence with the range of its name and
// the text of its line. It reads the file's text and parses nothing. The
// range stays empty and the text "" if the file cannot be read, or is shorter
// than when it was scanned.
func LocateSite(ws *workspace.Session, mods *index.Index, handle index.Site) Site {
	site := MakeSite(ws, mods, handle)
	span := mods.Occurrence(handle).Span
	if source, err := ws.LoadSource(site.Place.Path); err == nil && span.End <= len(source) {
		lines := jomini.NewLines(source)
		line := lines.LineSpan(lines.ToPosition(span.Start).Line)
		site.Range, site.Text = jomini.ToRange(source, lines, span), source[line.Start:line.End]
	}
	return site
}

// ToEdits converts the engine's byte-offset edits to the editor's edits over
// a text.
func ToEdits(text string, lines jomini.Lines, edits []jomini.Edit) []Edit {
	return lo.Map(edits, func(edit jomini.Edit, _ int) Edit {
		return Edit{Range: jomini.ToRange(text, lines, edit.Span), Text: edit.Text}
	})
}

// SetText gives the session the text the editor holds for an open file. It
// returns the file's problems.
func (lsp *Service) SetText(path, text string) ([]Diagnostic, error) {
	ws, err := lsp.Workspace.Session()
	if err != nil {
		return nil, err
	}
	ws.SetText(path, text)
	return lsp.Diagnose(path)
}

// CloseFile drops an open file's text from the session. It does nothing if no
// workspace is open.
func (lsp *Service) CloseFile(path string) {
	if ws, err := lsp.Workspace.Session(); err == nil {
		ws.CloseBuffer(path)
	}
}

// loadDocument returns the open session and the document of a file, from the
// editor's text if the file is open. It returns an error if there is no
// session or the file cannot be read.
func (lsp *Service) loadDocument(path string) (*workspace.Session, schema.Document, error) {
	ws, err := lsp.Workspace.Session()
	if err != nil {
		return nil, schema.Document{}, err
	}
	document, err := ws.WalkDocument(path)
	return ws, document, err
}

// loadTree returns the open session and the tree of a file, from the editor's
// text if the file is open, for a reader that needs no contexts. It returns an
// error if there is no session or the file cannot be read.
func (lsp *Service) loadTree(path string) (*workspace.Session, *jomini.Tree, error) {
	ws, err := lsp.Workspace.Session()
	if err != nil {
		return nil, nil, err
	}
	tree, err := ws.LoadTree(path)
	return ws, tree, err
}
