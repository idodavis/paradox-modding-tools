// health_problems.go is the editor's findings over every mod file, kept while the records are unchanged, and the
// ones no card above lists.

package pages

import (
	"maps"
	"slices"
	"sync"

	"paradox-modding-tools/services/internal/lsp"
	"paradox-modding-tools/services/internal/schema"
	"paradox-modding-tools/services/internal/workspace"
)

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

// Findings is the editor's findings over every mod file of a session, found again only when the session's records
// changed since; Changes never repeats across sessions, so one set serves. Diagnose takes the session's read lock
// itself, so Current is called before the caller takes it.
type Findings struct {
	mu      sync.Mutex
	changes int64 // the session's Changes the findings were made at
	byPath  map[string][]lsp.Diagnostic
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
			at := place{report.Sources[site.File].Path, site.Line, site.Col}
			if shown[at] == nil {
				shown[at] = map[string]bool{}
			}
			shown[at][card] = true
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
		for _, at := range ws.Workspace.InFile(path) {
			if at.Occurrence().Role == schema.Localizes {
				language = at.Language()
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
