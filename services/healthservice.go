// healthservice.go is the health page: the names two origins define, the mods' references nothing
// defines, the localization each language lacks, the definitions without the file the game finds
// for them, and one localization key looked up. Every row is sent, naming its file by its number in
// Sources; a site's lines are read when the page opens its row. A declared fact accuses; a learned
// one is only listed.
package services

import (
	"cmp"
	"maps"
	"slices"
	"strings"
	"sync"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/index"
	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/loc"
	"paradox-modding-tools/services/internal/lang/schema"
	"paradox-modding-tools/services/internal/lang/source"
	"paradox-modding-tools/services/internal/lsp"
	"paradox-modding-tools/services/internal/session"
)

// HealthParams is what the page asks for: the load order to preview.
type HealthParams struct {
	Order []string `json:"order"` // mod ids in the order to judge; empty for the workspace's own
}

// Health is the report.
type Health struct {
	Sources   []source.File `json:"sources"`   // every file a row names, at the number it names it by
	Contests  []Contest     `json:"contests"`  // names two or more mods define
	Overrides []Contest     `json:"overrides"` // names one mod defines over the install's
	Depends   []Dependency  `json:"depends"`
	Dangling  []Dangling    `json:"dangling"`
	Loc       []Language    `json:"loc"`
	Files     []FileRow     `json:"files"`
	Problems  []Problem     `json:"problems"` // what the editor's checks find that no card above shows
}

// Problem is one finding of the editor's checks, at its site.
type Problem struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Site    Site   `json:"site"`
}

// Contest is one name two origins define, its definitions in load order, with the game's rule.
type Contest struct {
	Database  string `json:"database"`
	Name      string `json:"name"`
	Winner    int    `json:"winner"` // the position in Sites of the definition the game keeps
	FirstWins bool   `json:"firstWins"`
	Rule      string `json:"rule"` // FIOS or LIOS, or the entry mode that decided it (REPLACE, INJECT)
	Sites     []Site `json:"sites"`
}

// Site is where a definition or a reference is written.
type Site struct {
	File     int    `json:"file"` // its number in Sources
	Line     int    `json:"line"`
	Col      int    `json:"col"`
	Database string `json:"database,omitempty"` // a dependency's use: the database and name written there
	Name     string `json:"name,omitempty"`
	Injected bool   `json:"injected,omitempty"` // a definition added to the kept one (INJECT), not replaced
}

// Dependency is one mod naming what only another mod defines, and each place it does.
type Dependency struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Sites []Site `json:"sites"`
}

// Dangling is one name a mod writes that nothing defines, and where.
type Dangling struct {
	Database string `json:"database"`
	Name     string `json:"name"`
	Declared bool   `json:"declared"` // a declared prefixed word: the game itself would reject it
	Sites    []Site `json:"sites"`
}

// Language is one language's localization gaps.
type Language struct {
	Language     string   `json:"language"`
	Missing      []LocRow `json:"missing"`      // the workspace's language: a definition's key, absent
	Untranslated []LocRow `json:"untranslated"` // another: a mod key the workspace's language has and it lacks
	Orphaned     []LocRow `json:"orphaned"`     // a mod key nothing of its mod reads
}

// LocRow is one key and, where it has one, the definition or the file it belongs to.
type LocRow struct {
	Key      string `json:"key"`
	Database string `json:"database,omitempty"`
	Name     string `json:"name,omitempty"`
	File     int    `json:"file"` // its number in Sources
	Line     int    `json:"line"`
}

// FileRow is one definition without images every definition of its database in the game has, which
// the game finds by the name.
type FileRow struct {
	Database string   `json:"database"`
	Name     string   `json:"name"`
	File     int      `json:"file"` // its number in Sources
	Line     int      `json:"line"`
	Missing  []string `json:"missing"` // each image it lacks, as the path the game looks for
	Of       int      `json:"of"`      // how many such images its database has
}

// SourceAt is one place a pane shows the source of: a definition's statement, or a reference's
// line.
type SourceAt struct {
	Path       string `json:"path"`
	Line       int    `json:"line"`
	Col        int    `json:"col"`
	Definition bool   `json:"definition"`
}

// HealthService answers the health page. It keeps each workspace's findings while its session's
// records are unchanged.
type HealthService struct {
	Session  *SessionService
	mu       sync.Mutex
	problems map[string]problemsAt
}

// problemsAt is a workspace's findings by path, and the session's changes they were found at.
type problemsAt struct {
	changes int64
	found   map[string][]lsp.Diagnostic
}

// GetHealth is the workspace's health report.
func (v *HealthService) GetHealth(workspaceID string, params HealthParams) (*Health, error) {
	sess, err := v.Session.live(workspaceID)
	if err != nil {
		return nil, err
	}
	found := v.findings(workspaceID, sess)
	sess.RLock()
	defer sess.RUnlock()
	order := sess.Order()
	if len(params.Order) > 0 {
		order = append([]string{game.OriginVanilla}, params.Order...)
	}
	health := &Health{}
	numbers := map[string]int{}
	number := func(file source.File) int {
		if _, ok := numbers[file.Path]; !ok {
			numbers[file.Path] = len(health.Sources)
			health.Sources = append(health.Sources, file)
		}
		return numbers[file.Path]
	}

	// each name two origins define: a contest where two mods define it, else one mod's override of the
	// install's
	for _, contest := range index.Contests(sess.All(), sess.Schema, func(database string) bool { return game.FirstWinsAt(sess.Game.ID, database) }, sess.Game.EntryModes, order) {
		mods := map[string]bool{}
		for _, found := range contest.Found {
			if found.At.Origin != game.OriginVanilla {
				mods[found.At.Origin] = true
			}
		}
		row := Contest{Database: contest.Database, Name: contest.Name, Winner: contest.Winner, FirstWins: contest.FirstWins, Rule: "LIOS"}
		if contest.FirstWins {
			row.Rule = "FIOS"
		}
		// the sites are in the order the game takes them: one after the kept one, written with a mode
		// that adds rather than replaces (INJECT), is part of what is kept; a mode the kept one was
		// written with, or else an inject's, is the row's rule
		for i, found := range contest.Found {
			site := Site{File: number(found.At), Line: int(found.Line), Col: int(found.Col)}
			mode := slices.IndexFunc(sess.Game.EntryModes, func(mode game.EntryMode) bool { return mode.Word == found.Mode })
			if site.Injected = mode >= 0 && !sess.Game.EntryModes[mode].Replaces && i > contest.Winner; site.Injected {
				row.Rule = found.Mode
			}
			row.Sites = append(row.Sites, site)
		}
		if contest.Winner >= 0 && contest.Found[contest.Winner].Mode != "" {
			row.Rule = contest.Found[contest.Winner].Mode
		}
		if len(mods) < 2 {
			health.Overrides = append(health.Overrides, row)
		} else {
			health.Contests = append(health.Contests, row)
		}
	}

	// what each mod needs from another
	needs := map[[2]string]*Dependency{}
	dangling := map[[2]string]*Dangling{}
	idx, all := sess.Workspace, sess.All()
	for _, ref := range idx.Refs {
		if ref.Type != schema.WordReference || ref.Database == "" || !all.Loaded(idx, ref.File) {
			continue
		}
		if _, scriptMade := schema.ScriptMade[ref.Database]; scriptMade {
			continue
		}
		from := idx.Files[ref.File].Origin
		var definers []string
		for _, name := range sess.Defs(ref.Name) {
			if name.Database == ref.Database && (ref.Parent == "" || name.Parent == ref.Parent) {
				definers = append(definers, name.At.Origin)
			}
		}
		site := Site{File: number(idx.Files[ref.File]), Line: int(ref.Line), Col: int(ref.Col)}
		if len(definers) == 0 {
			key := [2]string{ref.Database, ref.Name}
			if dangling[key] == nil {
				dangling[key] = &Dangling{Database: ref.Database, Name: ref.Name, Declared: ref.Declared}
			}
			dangling[key].Sites = append(dangling[key].Sites, site)
			continue
		}
		if !slices.Contains(definers, from) && !slices.Contains(definers, game.OriginVanilla) {
			key := [2]string{from, definers[0]}
			if needs[key] == nil {
				needs[key] = &Dependency{From: from, To: definers[0]}
			}
			site.Database, site.Name = ref.Database, ref.Name
			needs[key].Sites = append(needs[key].Sites, site)
		}
	}
	for _, row := range needs {
		health.Depends = append(health.Depends, *row)
	}
	slices.SortFunc(health.Depends, func(first, second Dependency) int { return strings.Compare(first.From+first.To, second.From+second.To) })
	for _, row := range dangling {
		health.Dangling = append(health.Dangling, *row)
	}
	slices.SortFunc(health.Dangling, func(first, second Dangling) int {
		return strings.Compare(first.Database+first.Name, second.Database+second.Name)
	})

	health.Loc = localization(sess, number)
	health.Files = files(sess, number)

	// the findings no card above shows: a duplicate at a contest's site, an unresolved reference at a
	// dangling one's, and every required-loc (Loc gaps) are left out
	type place struct {
		path      string
		line, col int
	}
	shown := map[string]map[place]bool{"duplicate-definition": {}, "unresolved-ref": {}}
	for _, row := range health.Contests {
		for _, site := range row.Sites {
			shown["duplicate-definition"][place{health.Sources[site.File].Path, site.Line, site.Col}] = true
		}
	}
	for _, row := range health.Dangling {
		for _, site := range row.Sites {
			shown["unresolved-ref"][place{health.Sources[site.File].Path, site.Line, site.Col}] = true
		}
	}
	for _, path := range slices.Sorted(maps.Keys(found)) {
		file, _ := sess.Workspace.At(path)
		for _, diagnostic := range found[path] {
			at := place{path, diagnostic.Range.Start.Line, diagnostic.Range.Start.Character}
			if diagnostic.Code != "required-loc" && !shown[diagnostic.Code][at] {
				site := Site{File: number(file), Line: at.line, Col: at.col}
				health.Problems = append(health.Problems, Problem{Code: diagnostic.Code, Message: diagnostic.Message, Site: site})
			}
		}
	}
	return health, nil
}

// findings is every mod file's findings with every code on, found again only when the session's
// records changed since. Diagnose takes the session's read lock itself.
func (v *HealthService) findings(workspaceID string, sess *session.Session) map[string][]lsp.Diagnostic {
	sess.RLock()
	changes, files := sess.Changes, slices.Clone(sess.Workspace.Files)
	sess.RUnlock()
	v.mu.Lock()
	defer v.mu.Unlock()
	if cached, ok := v.problems[workspaceID]; ok && cached.changes == changes {
		return cached.found
	}
	every := map[string]bool{}
	for _, code := range lsp.Codes {
		every[code.Code] = true
	}
	found := map[string][]lsp.Diagnostic{}
	for _, file := range files {
		if diagnostics, err := lsp.Diagnose(sess, file.Path, every); err == nil && len(diagnostics) > 0 {
			found[file.Path] = diagnostics
		}
	}
	if v.problems == nil {
		v.problems = map[string]problemsAt{}
	}
	v.problems[workspaceID] = problemsAt{changes: changes, found: found}
	return found
}

// GetLines is the source a pane shows at each place: a definition's statement, its first ten lines
// and a "�" where it runs on, or a reference's line.
func (v *HealthService) GetLines(workspaceID string, places []SourceAt) ([][]string, error) {
	sess, err := v.Session.live(workspaceID)
	if err != nil {
		return nil, err
	}
	sess.RLock()
	defer sess.RUnlock()
	out := make([][]string, len(places))
	for i, place := range places {
		parsed, _, err := sess.Parsed(place.Path)
		if err != nil || parsed == nil {
			continue
		}
		if !place.Definition {
			offset := parsed.Lines().OffsetAt(place.Line, 0)
			out[i] = spanLines(parsed, jomini.Range{Start: offset, End: offset}, 0)
			continue
		}
		span := jomini.Range{Start: parsed.Lines().OffsetAt(place.Line, place.Col)}
		span.End = span.Start
		if keyed := keyedAt(parsed, place.Line, place.Col); keyed != nil {
			span = keyed.Range
		}
		out[i] = spanLines(parsed, span, 10)
	}
	return out, nil
}

// localization is each language's gaps: in the workspace's language, a mod definition's
// conventional key absent (missing); in another its mod ships, a mod key the workspace's language
// has and it lacks (untranslated); in any, a key its own mod never reads (orphaned). A key is
// listed once.
func localization(sess *session.Session, number func(source.File) int) []Language {
	idx, all := sess.Workspace, sess.All()
	base := cmp.Or(sess.Language, "english")
	languages := map[string]bool{base: true}
	read := map[string]map[string]bool{}  // by origin: the keys something of that origin reads
	has := map[string]map[string]bool{}   // by language: the keys the workspace has
	ships := map[string]map[string]bool{} // by origin: the languages its loc files are in
	for _, entry := range idx.Loc {
		if !all.Loaded(idx, entry.File) {
			continue
		}
		at := idx.Files[entry.File]
		lang := loc.LanguageFromRel(at.Rel)
		languages[lang] = true
		if has[lang] == nil {
			has[lang] = map[string]bool{}
		}
		has[lang][entry.Key] = true
		if ships[at.Origin] == nil {
			ships[at.Origin] = map[string]bool{}
		}
		ships[at.Origin][lang] = true
	}
	for _, ref := range idx.Refs {
		if ref.Type == schema.WordLoc && all.Loaded(idx, ref.File) {
			origin := idx.Files[ref.File].Origin
			if read[origin] == nil {
				read[origin] = map[string]bool{}
			}
			read[origin][ref.Name] = true
		}
	}
	// each definition's key every vanilla one of its database has
	type want struct {
		key string
		def index.Def
	}
	var wanted []want
	conventional := map[string]map[string]bool{} // by origin: its definitions' conventional keys
	for _, def := range idx.Defs {
		if !all.Loaded(idx, def.File) {
			continue
		}
		database := sess.Schema.Databases[def.Database]
		if database == nil {
			continue
		}
		origin := idx.Files[def.File].Origin
		if read[origin] == nil {
			read[origin] = map[string]bool{}
		}
		if conventional[origin] == nil {
			conventional[origin] = map[string]bool{}
		}
		for _, convention := range database.LocKeys {
			key := loc.Expand(def.Name, []loc.Convention{convention})[0]
			read[origin][key], conventional[origin][key] = true, true
			if convention.Coverage == 1 {
				wanted = append(wanted, want{key, def})
			}
		}
	}

	// a key that goes on from one of its mod's conventional keys (setting_x to setting_x_desc) is that
	// definition's, though too few vanilla ones have it for the engine to learn it
	extends := func(origin, key string) bool {
		for i, char := range key {
			if char == '_' && conventional[origin][key[:i]] {
				return true
			}
		}
		return false
	}

	var out []Language
	for _, lang := range slices.Sorted(func(yield func(string) bool) {
		for language := range languages {
			if language != "" && !yield(language) {
				return
			}
		}
	}) {
		coverage := Language{Language: lang}
		for _, want := range wanted {
			if lang == base && !has[lang][want.key] && !slices.ContainsFunc(all.Entries(want.key), func(said index.Said) bool { return loc.LanguageFromRel(said.At.Rel) == lang }) {
				coverage.Missing = append(coverage.Missing, LocRow{Key: want.key, Database: want.def.Database, Name: want.def.Name, File: number(idx.Files[want.def.File]), Line: int(want.def.Line)})
			}
		}
		for _, entry := range idx.Loc {
			if !all.Loaded(idx, entry.File) {
				continue
			}
			at := idx.Files[entry.File]
			from := loc.LanguageFromRel(at.Rel)
			if from == base && lang != base && ships[at.Origin][lang] && !has[lang][entry.Key] {
				coverage.Untranslated = append(coverage.Untranslated, LocRow{Key: entry.Key, File: number(at), Line: entry.Line})
			}
			if from == lang && !read[at.Origin][entry.Key] && !extends(at.Origin, entry.Key) {
				coverage.Orphaned = append(coverage.Orphaned, LocRow{Key: entry.Key, File: number(at), Line: entry.Line})
			}
		}
		out = append(out, coverage)
	}
	return out
}

// files is each mod definition missing an image every vanilla one of its database has, one row with
// every such image it lacks.
func files(sess *session.Session, number func(source.File) int) []FileRow {
	var out []FileRow
	all := sess.All()
	for _, def := range sess.Workspace.Defs {
		if !all.Loaded(sess.Workspace, def.File) {
			continue
		}
		database := sess.Schema.Databases[def.Database]
		if database == nil {
			continue
		}
		row := FileRow{Database: def.Database, Name: def.Name, Line: int(def.Line)}
		for _, pattern := range database.Files {
			if pattern.Coverage != 1 {
				continue
			}
			row.Of++
			if path := strings.ReplaceAll(pattern.Pattern, "{id}", def.Name); !all.File(path) {
				row.Missing = append(row.Missing, path)
			}
		}
		if len(row.Missing) > 0 {
			row.File = number(sess.Workspace.Files[def.File])
			out = append(out, row)
		}
	}
	return out
}

// Lookup is one localization key: its entries in the workspace and everything that reads it.
type Lookup struct {
	Key     string        `json:"key"`
	Entries []LocEntry    `json:"entries"`
	Readers []index.Cited `json:"readers"`
}

// LocEntry is one of a key's entries, with the language its file is written in.
type LocEntry struct {
	index.Said
	Language string `json:"language"`
}

// LookupLoc looks one localization key up: its entries in the workspace and everything reading it.
func (v *HealthService) LookupLoc(workspaceID, key string) (*Lookup, error) {
	sess, err := v.Session.live(workspaceID)
	if err != nil {
		return nil, err
	}
	sess.RLock()
	defer sess.RUnlock()
	lookup, all := &Lookup{Key: key}, sess.All()
	for _, said := range all.Entries(key) {
		lookup.Entries = append(lookup.Entries, LocEntry{Said: said, Language: loc.LanguageFromRel(said.At.Rel)})
	}
	for _, idx := range all {
		for _, cited := range idx.Referring(key) {
			if cited.Type == schema.WordLoc && all.Loaded(idx, cited.File) {
				lookup.Readers = append(lookup.Readers, cited)
			}
		}
	}
	return lookup, nil
}
