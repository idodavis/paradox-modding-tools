// healthservice.go is the health page: the names two origins define, the references the editor
// finds nothing for, the localization a language lacks, and one localization key looked up. Every
// row is sent, naming its file by its number in Sources; a site's lines are read when the page
// opens its row.
package services

import (
	"cmp"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/index"
	"paradox-modding-tools/services/internal/lang/jomini"
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
	Overrides []Contest     `json:"overrides"` // names one mod defines over, or adds to, another origin's
	Depends   []Dependency  `json:"depends"`
	Dangling  []Dangling    `json:"dangling"`
	Loc       []Language    `json:"loc"`
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
	Injected bool   `json:"injected,omitempty"` // a definition added to the kept one (an inject, a merged on_action)
	Removed  bool   `json:"removed,omitempty"`  // a dependency's use of a name the other mod's file left out
}

// Dependency is one mod using a name another mod's definitions decide, and each place it does: it
// depends on the other where only that mod defines the name, and is affected by it where the other's
// definition differs from the one it hides, adds to it, or leaves it out.
type Dependency struct {
	From     string `json:"from"`
	To       string `json:"to"`
	Affected bool   `json:"affected"`
	Sites    []Site `json:"sites"`
}

// Dangling is one name a mod writes that nothing defines, and where.
type Dangling struct {
	Database string `json:"database"`
	Name     string `json:"name"`
	Sites    []Site `json:"sites"`
}

// Language is one language's localization gaps.
type Language struct {
	Language     string   `json:"language"`
	Untranslated []LocRow `json:"untranslated"` // not the workspace's language: a mod key that one has and this lacks
}

// LocRow is one key and the file it is in.
type LocRow struct {
	Key  string `json:"key"`
	File int    `json:"file"` // its number in Sources
	Line int    `json:"line"`
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
	health := &Health{}
	numbers := map[string]int{}
	number := func(file source.File) int {
		if _, ok := numbers[file.Path]; !ok {
			numbers[file.Path] = len(health.Sources)
			health.Sources = append(health.Sources, file)
		}
		return numbers[file.Path]
	}
	sch, indexes := sess.Install.Schema, sess.Indexes()

	// the mods in the order to judge: the workspace's files again, a mod's together, in that order
	preview := indexes
	if len(params.Order) > 0 {
		ordered := *sess.Workspace
		ordered.Files = slices.SortedStableFunc(slices.Values(sess.Workspace.Files), func(first, second source.File) int {
			return cmp.Compare(slices.Index(params.Order, first.Origin), slices.Index(params.Order, second.Origin))
		})
		ordered.Ready()
		preview = []*index.Index{sess.Install.Index, &ordered}
	}

	// each name two origins define: a contest where two mods contend for it, else one mod's override
	// of another origin's; not in a database where the game repeats names, nor one no schema database
	// names (a parameter, a name script makes)
	names := map[[2]string]bool{}
	for _, records := range sess.Workspace.Records {
		for _, def := range records.Defs {
			if def.Name != "" && sch.Databases[def.Database] != nil && !sess.Install.Index.Repeated[def.Database] {
				names[[2]string{def.Database, def.Name}] = true
			}
		}
	}
	for _, name := range slices.SortedFunc(maps.Keys(names), func(first, second [2]string) int {
		return slices.Compare(first[:], second[:])
	}) {
		defs := slices.DeleteFunc(index.Lookup(preview, name[0], name[1]), func(def index.Def) bool {
			return def.Local || def.Named
		})
		files, origins := make([]source.File, len(defs)), map[string]bool{}
		for i, def := range defs {
			files[i], _, _ = sess.Located(def.Path)
			origins[files[i].Origin] = true
		}
		if len(origins) < 2 {
			continue
		}
		winner, adds, _, contested := sess.Winner(defs)
		row := Contest{
			Database: name[0], Name: name[1], Winner: slices.Index(defs, winner),
			FirstWins: game.FirstWinsAt(sess.Game.ID, name[0]), Rule: "LIOS",
		}
		if row.FirstWins {
			row.Rule = "FIOS"
		}

		// sites in the game's order: what the mode function counts as an add (an inject, a merged
		// on_action) is part of the kept one; the kept one's mode, else an add's, is the row's rule
		for i, def := range defs {
			site := Site{File: number(files[i]), Line: int(def.Line), Col: int(def.Col)}
			if site.Injected = slices.Contains(adds, def); site.Injected {
				row.Rule = cmp.Or(def.Mode, row.Rule)
			}
			row.Sites = append(row.Sites, site)
		}
		if row.Winner >= 0 && defs[row.Winner].Mode != "" {
			row.Rule = defs[row.Winner].Mode
		}
		if contested {
			health.Contests = append(health.Contests, row)
		} else {
			health.Overrides = append(health.Overrides, row)
		}
	}

	// what each mod needs from another, from the references its loaded files write; and what the
	// editor's checks find nothing for, from their findings, so a name dangles by the editor's rules
	type place struct {
		path      string
		line, col int
	}
	shown := map[string]map[place]bool{"duplicate-definition": {}, "unresolved-ref": {}, "unknown-event": {}}
	type need struct {
		from, to string
		affected bool
	}
	needs, dangling := map[need]*Dependency{}, map[[2]string]*Dangling{}
	resolved := map[[4]string][]index.Def{} // a name resolves alike in one file, where a local definition shows
	compared := map[[2]index.Def]bool{}     // whether a definition's statement differs from the one it hides
	installOnly := []*index.Index{sess.Install.Index}
	for _, file := range sess.Workspace.Files {
		records := sess.Workspace.Records[file.Path]
		if _, loaded := index.Loaded(indexes, file.Path); records == nil || !loaded {
			continue
		}
		for _, ref := range records.Refs {
			// a name no schema database holds (a loc key, a file, a parameter, a name script makes) is no
			// mod's entry; a call names none
			if ref.Database != "" && sch.Databases[ref.Database] == nil {
				continue
			}
			spelled := [4]string{ref.Database, ref.Called, ref.Name, file.Path}
			defs, resolvedOnce := resolved[spelled]
			if !resolvedOnce {
				defs, _ = index.Resolve(sch, indexes, ref)
				resolved[spelled] = defs
			}
			// each other mod's contribution to the name is a row: where no loaded file defines it but
			// the install does, the mod whose file loads at its path left it out; else the winner, and
			// each add (an inject, a merged on_action) whatever the winner's origin
			database, removed := ref.Database, len(defs) == 0
			var landed []need
			if removed {
				installed, _ := index.Resolve(sch, installOnly, ref)
				if len(installed) == 0 {
					continue
				}
				at, _, _ := sess.Located(installed[0].Path)
				remover, _ := index.FileAt(indexes, at.Rel)
				database = cmp.Or(database, installed[0].Database)
				if remover.Origin != "" && remover.Origin != file.Origin && remover.Origin != game.OriginVanilla {
					landed = append(landed, need{file.Origin, remover.Origin, true})
				}
			} else {
				winner, adds, _, _ := sess.Winner(defs)
				database = cmp.Or(database, winner.Database)
				for at, held := range slices.Concat([]index.Def{winner}, adds) {
					holder, _, _ := sess.Located(held.Path)
					if holder.Origin == "" || holder.Origin == file.Origin || holder.Origin == game.OriginVanilla {
						continue
					}

					// an add affects the mod; the winner does when its statement differs from the one it
					// hides (the loaded one before it, else the install's own), and with none it is only
					// that mod's, so the mod depends on it
					key := need{file.Origin, holder.Origin, at > 0}
					if at == 0 {
						var base index.Def
						if before := slices.Index(defs, winner); before > 0 {
							base = defs[before-1]
						} else {
							installed, _ := index.Resolve(sch, installOnly, ref)
							base, _, _, _ = sess.Winner(installed)
						}
						if base.Path != "" {
							pair := [2]index.Def{winner, base}
							differs, done := compared[pair]
							if !done {
								_, winning := sess.Entry(winner)
								_, hidden := sess.Entry(base)
								differs = !sameStatement(winning, hidden)
								compared[pair] = differs
							}
							if !differs {
								continue
							}
							key.affected = true
						}
					}
					// a mod's add to its own name, which only it defines, is part of what the mod depends on
					if !slices.Contains(landed, key) && !slices.Contains(landed, need{file.Origin, holder.Origin, false}) {
						landed = append(landed, key)
					}
				}
			}
			for _, key := range landed {
				if needs[key] == nil {
					needs[key] = &Dependency{From: key.from, To: key.to, Affected: key.affected}
				}
				needs[key].Sites = append(needs[key].Sites, Site{
					File: number(file), Line: int(ref.Line), Col: int(ref.Col), Database: database, Name: ref.Name, Removed: removed,
				})
			}
		}
		for _, diagnostic := range found[file.Path] {
			start := diagnostic.Range.Start
			at := slices.IndexFunc(records.Refs, func(ref index.Ref) bool {
				return int(ref.Line) == start.Line && int(ref.Col) == start.Character
			})
			if diagnostic.Code != "unresolved-ref" && diagnostic.Code != "unknown-event" || at < 0 {
				continue
			}
			ref := records.Refs[at]
			key := [2]string{ref.Database, ref.Name}
			if dangling[key] == nil {
				dangling[key] = &Dangling{Database: ref.Database, Name: ref.Name}
			}
			dangling[key].Sites = append(dangling[key].Sites, Site{File: number(file), Line: start.Line, Col: start.Character})
			shown[diagnostic.Code][place{file.Path, start.Line, start.Character}] = true
		}
	}
	for _, row := range needs {
		health.Depends = append(health.Depends, *row)
	}
	slices.SortFunc(health.Depends, func(first, second Dependency) int {
		return strings.Compare(
			first.From+first.To+strconv.FormatBool(first.Affected), second.From+second.To+strconv.FormatBool(second.Affected))
	})
	for _, row := range dangling {
		health.Dangling = append(health.Dangling, *row)
	}
	slices.SortFunc(health.Dangling, func(first, second Dangling) int {
		return strings.Compare(first.Database+first.Name, second.Database+second.Name)
	})

	health.Loc = localization(sess, number)

	// the findings no card above shows: a duplicate at a contest's site, and every other finding that
	// is not a dangling reference
	for _, row := range health.Contests {
		for _, site := range row.Sites {
			shown["duplicate-definition"][place{health.Sources[site.File].Path, site.Line, site.Col}] = true
		}
	}
	for _, path := range slices.Sorted(maps.Keys(found)) {
		file, _ := sess.Workspace.At(path)
		for _, diagnostic := range found[path] {
			at := place{path, diagnostic.Range.Start.Line, diagnostic.Range.Start.Character}
			if !shown[diagnostic.Code][at] {
				site := Site{File: number(file), Line: at.line, Col: at.col}
				health.Problems = append(health.Problems, Problem{Code: diagnostic.Code, Message: diagnostic.Message, Site: site})
			}
		}
	}
	return health, nil
}

// sameStatement is whether two statements are written alike: key text, operator, scalar text (quoted
// or not) and block statements in order. Comments live outside the tree, so they never count.
func sameStatement(first, second jomini.Node) bool {
	switch left := first.(type) {
	case *jomini.Assignment:
		right, ok := second.(*jomini.Assignment)
		return ok && left.Key.Text == right.Key.Text && left.Op == right.Op && sameStatement(left.Value, right.Value)
	case *jomini.ValueStmt:
		right, ok := second.(*jomini.ValueStmt)
		return ok && sameStatement(left.Value, right.Value)
	case *jomini.Scalar:
		right, ok := second.(*jomini.Scalar)
		return ok && left.Text == right.Text
	case *jomini.TaggedBlock:
		right, ok := second.(*jomini.TaggedBlock)
		return ok && left.Tag.Text == right.Tag.Text && sameStatement(&left.Block, &right.Block)
	case *jomini.Block:
		right, ok := second.(*jomini.Block)
		return ok && slices.EqualFunc(left.Statements, right.Statements, func(before, after jomini.Statement) bool {
			return sameStatement(before, after)
		})
	}
	return false
}

// findings is every mod file's findings under the editor's default codes, found again only when the
// session's records changed since. Diagnose takes the session's read lock itself.
func (v *HealthService) findings(workspaceID string, sess *session.Session) map[string][]lsp.Diagnostic {
	sess.RLock()
	changes, files := sess.Changes, slices.Clone(sess.Workspace.Files)
	sess.RUnlock()
	v.mu.Lock()
	defer v.mu.Unlock()
	if cached, ok := v.problems[workspaceID]; ok && cached.changes == changes {
		return cached.found
	}
	found := map[string][]lsp.Diagnostic{}
	for _, file := range files {
		if diagnostics, err := lsp.Diagnose(sess, file.Path, nil); err == nil && len(diagnostics) > 0 {
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
// and a "…" where it runs on, or a reference's line.
func (v *HealthService) GetLines(workspaceID string, places []SourceAt) ([][]string, error) {
	sess, err := v.Session.live(workspaceID)
	if err != nil {
		return nil, err
	}
	sess.RLock()
	defer sess.RUnlock()
	out := make([][]string, len(places))
	for i, place := range places {
		parsed, entry := sess.Entry(index.Def{Path: place.Path, Line: int32(place.Line), Col: int32(place.Col)})
		if parsed == nil {
			continue
		}
		offset := parsed.Lines().OffsetAt(place.Line, 0)
		span, limit := jomini.Range{Start: offset, End: offset}, 0
		if place.Definition && entry != nil {
			span, limit = entry.NodeRange(), 10
		}
		out[i] = spanLines(parsed, span, limit)
	}
	return out, nil
}

// localization is each language's gaps: in a language a mod ships other than the workspace's, each
// key of its workspace-language files no workspace file in that language has, once.
func localization(sess *session.Session, number func(source.File) int) []Language {
	indexes := sess.Indexes()
	workspaceLanguage := strings.ToLower(cmp.Or(sess.Language, "english"))
	languages := map[string]bool{workspaceLanguage: true}
	has := map[string]map[string]bool{}   // by language: the keys the workspace has
	ships := map[string]map[string]bool{} // by origin: the languages its loc files are in
	type written struct {
		file  source.File
		entry index.Loc
	}
	var inWorkspaceLanguage []written
	for _, file := range sess.Workspace.Files {
		records := sess.Workspace.Records[file.Path]
		if _, loaded := index.Loaded(indexes, file.Path); records == nil || !loaded {
			continue
		}
		for _, entry := range records.Loc {
			languages[entry.Language] = true
			if has[entry.Language] == nil {
				has[entry.Language] = map[string]bool{}
			}
			has[entry.Language][entry.Key] = true
			if ships[file.Origin] == nil {
				ships[file.Origin] = map[string]bool{}
			}
			ships[file.Origin][entry.Language] = true
			if entry.Language == workspaceLanguage {
				inWorkspaceLanguage = append(inWorkspaceLanguage, written{file, entry})
			}
		}
	}

	var out []Language
	for _, language := range slices.Sorted(maps.Keys(languages)) {
		coverage := Language{Language: language}
		for _, own := range inWorkspaceLanguage {
			if language != workspaceLanguage && ships[own.file.Origin][language] && !has[language][own.entry.Key] {
				coverage.Untranslated = append(coverage.Untranslated, LocRow{Key: own.entry.Key, File: number(own.file), Line: own.entry.Line})
			}
		}
		out = append(out, coverage)
	}
	return out
}

// Lookup is one localization key: its entries in the workspace and the install, and everything that
// reads it.
type Lookup struct {
	Key     string     `json:"key"`
	Entries []LocEntry `json:"entries"`
	Readers []Cited    `json:"readers"`
}

// LocEntry is one of a key's entries, with the language its file is written in.
type LocEntry struct {
	Value    string      `json:"value"`
	Line     int         `json:"line"` // 0-based
	At       source.File `json:"at"`
	Language string      `json:"language"`
}

// LookupLoc looks one localization key up: its entries and everything reading it.
func (v *HealthService) LookupLoc(workspaceID, key string) (*Lookup, error) {
	sess, err := v.Session.live(workspaceID)
	if err != nil {
		return nil, err
	}
	sess.RLock()
	defer sess.RUnlock()
	lookup, indexes := &Lookup{Key: key}, sess.Indexes()
	for _, entry := range index.LookupLocs(indexes, key) {
		file, _, _ := sess.Located(entry.Path)
		lookup.Entries = append(lookup.Entries, LocEntry{Value: entry.Text, Line: entry.Line, At: file, Language: entry.Language})
	}
	for _, ref := range index.LookupRefs(indexes, index.Localization, key) {
		file, _, _ := sess.Located(ref.Path)
		lookup.Readers = append(lookup.Readers, Cited{Name: ref.Name, Line: int(ref.Line), Col: int(ref.Col), At: file})
	}
	return lookup, nil
}
