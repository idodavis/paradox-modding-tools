// Package lsp is the editor's language service. It answers what the editor
// asks about a file: hover cards, completions, definitions, renames, problems,
// tokens, folds, hints, colors, signatures, formatting and quick fixes.
//
// Every range is in Monaco's form: lines and columns count from 1, the column
// in UTF-16 code units, and the end is exclusive. Every other package keeps
// UTF-8 byte offsets.
//
// The package decides nothing about the language. The schema says what a key
// means, the index where a name is defined, and the workspace service holds the
// open session. An answer is computed when asked, from the editor's text if the
// file is open, else from the disk.
//
// It applies these rules:
//
//   - A hover card lists only what files define: no second name of a
//     definition, no name a command makes (a saved scope, a variable, a flag)
//     and no key that only switches a name on.
//   - Where no source says which copy of a name the game keeps, no site is
//     marked kept.
//   - A check accuses only on firm ground. A name the game's own files use is
//     never dangling, and a key is unknown only where a source that can accuse
//     describes the block.
//   - A file of the game has its parse errors only. A mod's file is judged
//     against its mod and the mods it depends on.
//   - Only a script file is walked to a position.
//
// # Robustness
//
// A call with no workspace open, or for a file that cannot be read, returns an
// error. Rename refuses an empty name. PrepareRename refuses a name nothing
// defines and one a read-only file defines too. Format gives no edits for a
// read-only file, a file that is not script and a file that does not parse.
//
// A position on nothing gives nil or an empty list. A file shorter than when it
// was scanned gives a site with no range and no text.
package lsp

import (
	"cmp"
	"strings"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/services/workspace"
)

// Service is the editor's language service.
type Service struct {
	Workspace *workspace.Service
}

// An Edit is one change to a file's text.
type Edit struct {
	Range jomini.Range `json:"range"`
	Text  string       `json:"text"`
}

// A Site is one place in a file that an answer points at.
type Site struct {
	Place workspace.Place `json:"place"`

	// Line counts from 1. Range and Text are empty where the answer did not
	// read the file.
	Line  int          `json:"line"`
	Range jomini.Range `json:"range"`
	Text  string       `json:"text,omitempty"`

	// Kept is whether the game keeps the definition, and Reason why. A copy in
	// a file the game does not read has no reason, and its place says what
	// hides the file.
	Kept   bool        `json:"kept"`
	Reason lang.Reason `json:"reason,omitempty"`

	// Label is the reason in words, or what hides the copy's file.
	Label string `json:"label,omitempty"`

	// Defines lists the names a whole-file site defines.
	Defines []string `json:"defines,omitempty"`

	// Holder is the name of the definition that holds a copy the game keeps
	// whole, or "".
	Holder string `json:"holder,omitempty"`
}

// MakeSite returns the site of an occurrence without reading its file, so the
// site has no range and no text.
func MakeSite(ws *workspace.Session, mods *index.Index, site index.Site) Site {
	return Site{Place: ws.Locate(mods, mods.File(site).Path), Line: int(mods.Occurrence(site).Line) + 1}
}

// hidingLabels says why the game does not read a file. {mod} stands for the
// mod that hides it.
var hidingLabels = map[files.Hiding]string{
	files.HidingShadowed:  "The copy of this file in {mod} replaces it",
	files.HidingDropped:   "A replace_path in {mod} drops this folder",
	files.HidingUnmounted: "The game reads no file in this folder",
}

// MakeSites returns the sites of a name's definitions, each with whether the
// winner keeps it and why. A nil winner keeps none.
func MakeSites(ws *workspace.Session, mods *index.Index, definitions []index.Site, winner *index.Winner) []Site {
	sites := make([]Site, len(definitions))
	for position, definition := range definitions {
		site := MakeSite(ws, mods, definition)
		site.Holder = findHolder(ws, mods, definition)
		if winner != nil {
			outcome := mods.ResolveSite(*winner, definition)
			site.Kept, site.Reason = outcome.Kept, outcome.Reason
		}

		label := cmp.Or(hidingLabels[site.Place.Hiding], lang.Reasons[site.Reason].Label)
		site.Label = strings.ReplaceAll(label, "{mod}", ws.DescribeMod(site.Place.Hider))
		sites[position] = site
	}

	return sites
}

// findHolder returns the name of the definition a nested copy is written in,
// for a database where the game keeps every copy. It returns "" otherwise.
func findHolder(ws *workspace.Session, mods *index.Index, definition index.Site) string {
	database := ws.Schema.Databases[mods.ToReference(definition).Database]
	holder, nested := mods.FindDefinition(definition)
	if database == nil || !database.Precedence.KeepsEvery() || !nested {
		return ""
	}

	return mods.ToReference(holder).Name
}

// LocateSite returns the site of an occurrence with the range of its name and
// the text of its line. They stay empty if the file cannot be read.
func LocateSite(ws *workspace.Session, mods *index.Index, handle index.Site) Site {
	site := MakeSite(ws, mods, handle)
	span := mods.Occurrence(handle).Span
	if source, err := ws.LoadSource(site.Place.Path); err == nil && span.End <= len(source) {
		site = LocateSpan(site.Place, source, jomini.NewLines(source), span)
	}

	return site
}

// LocateSpan returns the site of a span in a file's text, with its range and
// the text of its line.
func LocateSpan(place workspace.Place, text string, lines jomini.Lines, span jomini.Span) Site {
	line := lines.ToPosition(span.Start).Line
	row := lines.LineSpan(line)

	return Site{Place: place, Line: line + 1, Range: jomini.ToRange(text, lines, span), Text: text[row.Start:row.End]}
}

// ToEdits converts the engine's edits over a text to the editor's.
func ToEdits(text string, lines jomini.Lines, edits []jomini.Edit) []Edit {
	converted := make([]Edit, len(edits))
	for position, edit := range edits {
		converted[position] = Edit{Range: jomini.ToRange(text, lines, edit.Span), Text: edit.Text}
	}

	return converted
}

// SetText gives the session the editor's text of an open file and returns the
// file's problems.
func (s *Service) SetText(path, text string) ([]Diagnostic, error) {
	ws, err := s.Workspace.Session()
	if err != nil {
		return nil, err
	}

	ws.SetText(path, text)

	return s.Diagnose(path)
}

// CloseFile drops an open file's text from the session. It does nothing if no
// workspace is open.
func (s *Service) CloseFile(path string) {
	if ws, err := s.Workspace.Session(); err == nil {
		ws.CloseBuffer(path)
	}
}

func (s *Service) loadDocument(path string) (*workspace.Session, lang.Document, error) {
	ws, err := s.Workspace.Session()
	if err != nil {
		return nil, lang.Document{}, err
	}

	document, err := ws.WalkDocument(path)

	return ws, document, err
}

// loadTree is loadDocument without the walk, for an answer that needs only
// the file's tree.
func (s *Service) loadTree(path string) (*workspace.Session, *jomini.Tree, error) {
	ws, err := s.Workspace.Session()
	if err != nil {
		return nil, nil, err
	}

	tree, err := ws.LoadTree(path)

	return ws, tree, err
}
