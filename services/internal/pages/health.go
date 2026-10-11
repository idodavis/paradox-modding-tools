// health.go is the Health page: the names two origins define, what a mod needs from another, the names nothing
// defines, the localization a language lacks, and the editor's own findings, every row naming its file by its number
// in Sources.

// Package pages is what each of the app's pages asks of an open workspace: the Health report, the event graph with
// one definition's detail, and the searches over the workspace's folders. Each answer is a function of the session
// (or the folders) and the page's parameters; the services open the session and call, and each answer over a
// session takes its read lock itself.
package pages

import (
	"cmp"
	"slices"

	"paradox-modding-tools/services/internal/index"
	"paradox-modding-tools/services/internal/inventory"
	"paradox-modding-tools/services/internal/jomini"
	"paradox-modding-tools/services/internal/lsp"
	"paradox-modding-tools/services/internal/schema"
	"paradox-modding-tools/services/internal/workspace"
)

// HealthParams is what the page asks for: the load order to preview.
type HealthParams struct {
	Order []string `json:"order"` // mod ids in the order to judge; empty for the workspace's own
}

// HealthReport is the Health page: every row names its file by its number in Sources.
type HealthReport struct {
	Sources   []inventory.File `json:"sources"`   // every file a row names, at the number it names it by
	Contests  []Contest        `json:"contests"`  // names two or more mods define
	Overrides []Contest        `json:"overrides"` // names one mod defines over, or adds to, another origin's
	Depends   []Dependency     `json:"depends"`
	Dangling  []Dangling       `json:"dangling"`
	Loc       []Language       `json:"loc"`
	Problems  []Problem        `json:"problems"` // what the editor's checks find that no card above shows
}

// Contest is one name two origins define, its definitions in load order, with the game's rule.
type Contest struct {
	Database  string `json:"database"`
	Kind      string `json:"kind"` // what one of its database is called (game.KindName)
	Name      string `json:"name"`
	Winner    int    `json:"winner"` // the position in Sites of the definition the game keeps
	FirstWins bool   `json:"firstWins"`
	Rule      string `json:"rule"` // FIOS (first wins) or LIOS (last wins), or the deciding entry mode (REPLACE, INJECT)
	Sites     []Site `json:"sites"`
}

// Site is where a definition or a reference is written.
type Site struct {
	File     int      `json:"file"` // its number in Sources
	Line     int      `json:"line"`
	Col      int      `json:"col"`
	Database string   `json:"database,omitempty"` // a dependency's use: the database and name written there
	Name     string   `json:"name,omitempty"`
	Injected bool     `json:"injected,omitempty"` // a definition added to the kept one (an inject, a merged on_action)
	Removed  bool     `json:"removed,omitempty"`  // a dependency's use of a name the other mod's file left out
	Defines  []string `json:"defines,omitempty"`  // a replaced file's row: the names the file defines
	Changed  []string `json:"changed,omitempty"`  // and those it and the kept file both define but write otherwise
}

// Dependency is one mod using a name another mod's definitions decide, and each place it does: it depends on the
// other where only that mod defines the name, and is affected by it where the other's definition differs from the
// one it hides, adds to it, or leaves it out.
type Dependency struct {
	From     string `json:"from"`
	To       string `json:"to"`
	Affected bool   `json:"affected"`
	Sites    []Site `json:"sites"`
}

// Dangling is one name a mod writes that nothing defines, and where.
type Dangling struct {
	Code     string `json:"code"` // the check that found it: a code shownBy lists under the dangling card
	Database string `json:"database"`
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	Sites    []Site `json:"sites"`
}

// Language is one language's localization gaps.
type Language struct {
	Language     string   `json:"language"`
	Untranslated []LocRow `json:"untranslated"` // keys a mod has in the workspace language but not in this one
}

// LocRow is one key and the file it is in.
type LocRow struct {
	Key    string `json:"key"`
	File   int    `json:"file"` // its number in Sources
	Line   int    `json:"line"`
	Target int    `json:"target"` // the mod's file in the missing language it goes in
}

// Problem is one finding of the editor's checks, at its site.
type Problem struct {
	Code     string `json:"code"`
	Message  string `json:"message"`
	Site     Site   `json:"site"`
	Language string `json:"language,omitempty"` // a localization file's language
}

// SourceAt is one place a pane shows the source of: a definition's statement, or a reference's line.
type SourceAt struct {
	Path       string `json:"path"`
	Line       int    `json:"line"`
	Col        int    `json:"col"`
	Definition bool   `json:"definition"`
}

// sourceIndex numbers the files a report's rows name, each once, in the order first named.
type sourceIndex struct {
	report  *HealthReport
	numbers map[string]int // by path
}

// Health is the workspace's report: the contests and overrides in the load order asked for, what each mod depends
// on, the names nothing defines, each language's gaps and the findings no card above lists. It takes the session's
// read lock; findings is Findings.Current's, which takes it itself, so Current is called before.
func Health(ws *workspace.Session, params HealthParams, findings map[string][]lsp.Diagnostic) *HealthReport {
	ws.RLock()
	defer ws.RUnlock()
	report := &HealthReport{}
	sources := &sourceIndex{report: report, numbers: map[string]int{}}
	preview := previewOrder(ws, params.Order)
	report.Contests, report.Overrides = contests(ws, preview, sources)
	files, replacing := fileOverrides(ws, preview, sources)
	report.Contests = append(report.Contests, files...)
	report.Overrides = append(report.Overrides, replacing...)
	report.Depends = dependencies(ws, sources)
	report.Dangling = dangling(ws, findings, sources)
	report.Loc = localization(ws, sources)
	report.Problems = problems(ws, report, findings, sources)
	return report
}

// Lines is the source a pane shows at each place: a definition's statement, its first ten lines and a "…" where it
// runs on, or a reference's line. It takes the session's read lock.
func Lines(ws *workspace.Session, places []SourceAt) [][]string {
	ws.RLock()
	defer ws.RUnlock()
	out := make([][]string, len(places))
	for i, place := range places {
		parsed, _, err := ws.Parsed(place.Path)
		if err != nil {
			continue
		}
		offset := parsed.Lines().OffsetAt(place.Line, 0)
		span, limit := jomini.Range{Start: offset, End: offset}, 0
		if statement := parsed.AssignmentAt(place.Line, place.Col); place.Definition && statement != nil {
			span, limit = statement.NodeRange(), 10
		}
		out[i] = spanLines(parsed, span, limit)
	}
	return out
}

// LocReaders is everything that reads one localization key. It takes the session's read lock.
func LocReaders(ws *workspace.Session, key string) []Cited {
	ws.RLock()
	defer ws.RUnlock()
	var readers []Cited
	for _, ref := range index.Lookup(ws.Indexes(), schema.Refers, schema.Localization, key) {
		occurrence := ref.Occurrence()
		readers = append(readers, Cited{Name: occurrence.ID, Line: int(occurrence.Line), Col: int(occurrence.Col), At: ref.File()})
	}
	return readers
}

// index is a file's number in Sources, which it takes at the end of the list the first time a row names it.
func (sources *sourceIndex) index(file inventory.File) int {
	if number, known := sources.numbers[file.Path]; known {
		return number
	}
	number := len(sources.report.Sources)
	sources.report.Sources = append(sources.report.Sources, file)
	sources.numbers[file.Path] = number
	return number
}

// previewOrder is the indexes in the order to judge: the workspace's files again, a mod's together, in that order.
func previewOrder(ws *workspace.Session, order []string) []*index.Index {
	if len(order) == 0 {
		return ws.Indexes()
	}
	preview := *ws.Workspace
	preview.Files = slices.Clone(ws.Workspace.Files)
	slices.SortStableFunc(preview.Files, func(first, second inventory.File) int {
		return cmp.Compare(slices.Index(order, first.Origin), slices.Index(order, second.Origin))
	})
	preview.PlaceFiles()
	return []*index.Index{ws.Install.Index, &preview}
}

// loadedFiles is the workspace's files that have records and that the game loads.
func loadedFiles(ws *workspace.Session) []inventory.File {
	var files []inventory.File
	for _, file := range ws.Workspace.Files {
		if len(ws.Workspace.InFile(file.Path)) == 0 {
			continue
		}
		if _, loaded := index.Loaded(ws.Indexes(), file.Path); loaded {
			files = append(files, file)
		}
	}
	return files
}
