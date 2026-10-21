// Package search is the IDE's search service: text search over the open workspace's files, replace, and the file
// search behind Ctrl P.
package search

import (
	"github.com/wailsapp/wails/v3/pkg/application"

	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/services/workspace"
)

// EventHits is sent with one file's hits as a text search finds them; the data is a Hits.
const EventHits = "search:hits"

func init() { application.RegisterEvent[Hits](EventHits) }

// Service is the search service. It runs one search at a time; starting a new one stops the last.
type Service struct {
	Workspace *workspace.Service
	stop      chan struct{} // closed to end the running search; nil while none runs
}

// ServiceShutdown stops a running search.
func (sch *Service) ServiceShutdown() error { panic("stub") }

// A Query is one text search: what to find and where.
type Query struct {
	Text      string   `json:"text"`
	Regex     bool     `json:"regex"`
	MatchCase bool     `json:"matchCase"`
	WholeWord bool     `json:"wholeWord"`
	Include   string   `json:"include"` // glob patterns, comma-separated
	Exclude   string   `json:"exclude"`
	Origins   []string `json:"origins"` // the mods and the game searched; empty for the mods
}

// A Hit is one match: the file, the line, and the line's text with the span of the match in it.
type Hit struct {
	Place  workspace.Place `json:"place"`
	Line   int             `json:"line"`
	Column int             `json:"column"`
	Span   jomini.Span     `json:"span"`
	Text   string          `json:"text"`
}

// Hits is the data of the search:hits event: one file's hits, sent as soon as that file is done so the view fills
// as the search runs.
type Hits struct {
	Place workspace.Place `json:"place"`
	Hits  []Hit           `json:"hits"`
}

// SearchText starts a text search over the files the query names, reading an open file's text from the editor.
// It runs in the background and sends each file's hits as a search:hits event. Starting a new search stops the
// last.
func (sch *Service) SearchText(query Query) error { panic("stub") }

// StopSearch ends the running search, if any.
func (sch *Service) StopSearch() { panic("stub") }

// ReplaceText replaces every match of a query in the mods' files and returns how many it replaced. Each file is
// written back in its own encoding and gets an fs:changed event. A file open in the editor is skipped, since the
// editor replaces in it; the error says how many were skipped.
func (sch *Service) ReplaceText(query Query, replacement string) (int, error) { panic("stub") }

// SearchFiles returns the files whose path contains the query's characters in order, the mods' files first, up
// to the given limit.
func (sch *Service) SearchFiles(query string, limit int) []workspace.Place { panic("stub") }

// run runs one text search until it finishes or the stop channel closes, sending each file's hits as it goes.
func run(ws *workspace.Session, query Query, stop <-chan struct{}) { panic("stub") }
