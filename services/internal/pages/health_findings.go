// health_findings.go is the editor's findings as Health rows: the findings over every mod file, kept while the
// records are unchanged; the names nothing defines, one row per name; and the findings no card lists, one row per
// place.

package pages

import (
	"cmp"
	"maps"
	"slices"
	"sync"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/index"
	"paradox-modding-tools/services/internal/lsp"
	"paradox-modding-tools/services/internal/schema"
	"paradox-modding-tools/services/internal/workspace"
)

// Findings is the editor's findings over every mod file of a session, found again only when the session's records
// changed since; Changes never repeats across sessions, so one set serves. Diagnose takes the session's read lock
// itself, so Current is called before the caller takes it.
type Findings struct {
	mu      sync.Mutex
	changes int64 // the session's Changes the findings were made at
	byPath  map[string][]lsp.Diagnostic
}

// The cards that list a finding by its code: the contests list a name defined twice at each site, the dangling rows
// a name nothing defines by name.
const (
	cardContests = "contests"
	cardDangling = "dangling"
)

// shownBy is the card a code's findings are listed by, so no finding is shown twice: the contests list a name
// defined twice at each site, the dangling rows list a name nothing defines by name; a code not here is listed
// among the problems.
var shownBy = map[string]string{
	"duplicate-definition": cardContests,
	"unresolved-ref":       cardDangling,
	"unknown-event":        cardDangling,
	"missing-file":         cardDangling,
}

// Current is the findings of every mod file by its path, found again where the records changed since the last.
func (findings *Findings) Current(ws *workspace.Session) map[string][]lsp.Diagnostic {
	ws.RLock()
	changes, files := ws.Changes, slices.Clone(ws.Workspace.Files)
	ws.RUnlock()
	findings.mu.Lock()
	defer findings.mu.Unlock()
	if findings.byPath != nil && findings.changes == changes {
		return findings.byPath
	}
	findings.changes, findings.byPath = changes, map[string][]lsp.Diagnostic{}
	for _, file := range files {
		if diagnostics, err := lsp.Diagnose(ws, file.Path, nil); err == nil && len(diagnostics) > 0 {
			findings.byPath[file.Path] = diagnostics
		}
	}
	return findings.byPath
}

// dangling is each name a mod's loaded file writes that the editor's checks find nothing for, from their findings,
// so a name dangles by the editor's rules.
func dangling(ws *workspace.Session, findings map[string][]lsp.Diagnostic, sources *sourceIndex) []Dangling {
	rows := map[[2]string]*Dangling{} // by database and name
	for _, file := range loadedFiles(ws) {
		occurrences := ws.Workspace.InFile(file.Path)
		for _, diagnostic := range findings[file.Path] {
			start := diagnostic.Range.Start
			i := slices.IndexFunc(occurrences, func(at index.At) bool {
				occurrence := at.Occurrence()
				return occurrence.Role == schema.Refers && int(occurrence.Line) == start.Line && int(occurrence.Col) == start.Character
			})
			if shownBy[diagnostic.Code] != cardDangling || i < 0 {
				continue
			}
			ref := occurrences[i].Occurrence()
			key := [2]string{ref.Database, ref.ID}
			row := rows[key]
			if row == nil {
				row = &Dangling{Database: ref.Database, Kind: game.KindName(ref.Database), Name: ref.ID}
				rows[key] = row
			}
			// the last check to find the name is the one the row names
			row.Code = diagnostic.Code
			row.Sites = append(row.Sites, Site{File: sources.index(file), Line: start.Line, Col: start.Character})
		}
	}
	var out []Dangling
	for _, row := range rows {
		out = append(out, *row)
	}
	slices.SortFunc(out, func(first, second Dangling) int {
		return cmp.Compare(first.Database+first.Name, second.Database+second.Name)
	})
	return out
}

// problems is the findings no card above lists, each at its site: the contests list their own sites, and the
// dangling rows the names nothing defines.
func problems(ws *workspace.Session, report *HealthReport, findings map[string][]lsp.Diagnostic, sources *sourceIndex) []Problem {
	type place struct {
		path      string
		line, col int
	}
	shown := map[place]map[string]bool{} // by place: the cards listing a finding there
	mark := func(card string, sites []Site) {
		for _, site := range sites {
			where := place{report.Sources[site.File].Path, site.Line, site.Col}
			if shown[where] == nil {
				shown[where] = map[string]bool{}
			}
			shown[where][card] = true
		}
	}
	for _, contest := range report.Contests {
		mark(cardContests, contest.Sites)
	}
	for _, row := range report.Dangling {
		mark(cardDangling, row.Sites)
	}
	var out []Problem
	for _, path := range slices.Sorted(maps.Keys(findings)) {
		file, _ := ws.Workspace.At(path)
		language := ""
		for _, entry := range ws.Workspace.InFile(path) {
			if entry.Occurrence().Role == schema.Localizes {
				language = entry.Language()
				break
			}
		}
		for _, diagnostic := range findings[path] {
			start := diagnostic.Range.Start
			if shown[place{path, start.Line, start.Character}][shownBy[diagnostic.Code]] {
				continue
			}
			out = append(out, Problem{
				Code: diagnostic.Code, Message: diagnostic.Message,
				Site:     Site{File: sources.index(file), Line: start.Line, Col: start.Character},
				Language: language,
			})
		}
	}
	return out
}
