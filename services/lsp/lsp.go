// Package lsp is the editor's language service. Its methods answer what Monaco's providers ask about a file: hover
// cards, completions, definitions, diagnostics, tokens, folds and the rest, each computed by a visitor on the
// schema's walk. Every range is in Monaco's own form. The editor sends an open file's text through SetText, which
// answers with the file's problems.
package lsp

import (
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
func ToRange(text string, lines jomini.Lines, span jomini.Span) Range { panic("stub") }

// ToOffset converts a line and a column as Monaco counts them to a byte offset in the text.
func ToOffset(text string, lines jomini.Lines, line, column int) int { panic("stub") }

// ToEdits converts the engine's byte-offset edits to Monaco edits over a text.
func ToEdits(text string, lines jomini.Lines, edits []jomini.Edit) []Edit { panic("stub") }

// SetText gives the session an open file's text as the editor has it, and returns the file's problems: its parse
// errors and every verdict that fails on it.
func (lsp *Service) SetText(path, text string) ([]Diagnostic, error) { panic("stub") }

// CloseFile drops an open file's text from the session.
func (lsp *Service) CloseFile(path string) error { panic("stub") }
