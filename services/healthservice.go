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
	"paradox-modding-tools/services/internal/jomini"
	"paradox-modding-tools/services/internal/lang/index"
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
	Code     string `json:"code"`
	Message  string `json:"message"`
	Site     Site   `json:"site"`
	Language string `json:"language,omitempty"` // a localization file's language
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

// Dependency is one mod using a name another mod's definitions decide, and each place it does: it
// depends on the other where only that mod defines the name, and is affected by it where the
// other's definition differs from the one it hides, adds to it, or leaves it out.
type Dependency struct {
	From     string `json:"from"`
	To       string `json:"to"`
	Affected bool   `json:"affected"`
	Sites    []Site `json:"sites"`
}

// Dangling is one name a mod writes that nothing defines, and where.
type Dangling struct {
	Code     string `json:"code"` // the check that found it, one of danglingChecks
	Database string `json:"database"`
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	Sites    []Site `json:"sites"`
}

// danglingChecks are the editor's checks for a name nothing defines; Dangling lists their findings by name.
var danglingChecks = []string{"unresolved-ref", "unknown-event", "missing-file"}

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

// SourceAt is one place a pane shows the source of: a definition's statement, or a reference's
// line.
type SourceAt struct {
	Path       string `json:"path"`
	Line       int    `json:"line"`
	Col        int    `json:"col"`
	Definition bool   `json:"definition"`
}

// HealthService answers the health page. It keeps the live session's findings while its records are
// unchanged; Changes never repeats across sessions, so one set is enough.
type HealthService struct {
	Session *SessionService
	mu      sync.Mutex
	changes int64                       // the session's Changes the findings were made at
	found   map[string][]lsp.Diagnostic // by path
}

// GetHealth is the workspace's health report.
func (s *HealthService) GetHealth(workspaceID string, params HealthParams) (*Health, error) {
	sess, err := s.Session.live(workspaceID)
	if err != nil {
		return nil, err
	}
	found := s.findings(sess)
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

	// the mods in the order to judge: the workspace's files again, a mod's together, in that order
	preview := sess.Indexes()
	if len(params.Order) > 0 {
		ordered := *sess.Workspace
		ordered.Files = slices.SortedStableFunc(slices.Values(sess.Workspace.Files), func(first, second source.File) int {
			return cmp.Compare(slices.Index(params.Order, first.Origin), slices.Index(params.Order, second.Origin))
		})
		ordered.Place()
		preview = []*index.Index{sess.Install.Index, &ordered}
	}
	health.Contests, health.Overrides = contests(sess, preview, number)
	files, replacing := replaced(sess, preview, number)
	health.Contests, health.Overrides = append(health.Contests, files...), append(health.Overrides, replacing...)
	health.Depends = dependencies(sess, number)
	health.Dangling = dangling(sess, found, number)
	health.Loc = localization(sess, number)

	// the findings no card above shows: a duplicate at a contest's site, a dangling reference at a
	// dangling row's, and every other finding
	type place struct {
		path      string
		line, col int
	}
	shown := map[place][]string{}
	for _, row := range health.Contests {
		for _, site := range row.Sites {
			at := place{health.Sources[site.File].Path, site.Line, site.Col}
			shown[at] = append(shown[at], "duplicate-definition")
		}
	}
	for _, row := range health.Dangling {
		for _, site := range row.Sites {
			at := place{health.Sources[site.File].Path, site.Line, site.Col}
			shown[at] = append(shown[at], danglingChecks...)
		}
	}
	for _, path := range slices.Sorted(maps.Keys(found)) {
		file, _ := sess.Workspace.At(path)
		language := ""
		if records := sess.Workspace.Records[path]; records != nil && len(records.Loc) > 0 {
			language = records.Loc[0].Language
		}
		for _, diagnostic := range found[path] {
			at := place{path, diagnostic.Range.Start.Line, diagnostic.Range.Start.Character}
			if !slices.Contains(shown[at], diagnostic.Code) {
				site := Site{File: number(file), Line: at.line, Col: at.col}
				health.Problems = append(health.Problems, Problem{Code: diagnostic.Code, Message: diagnostic.Message, Site: site, Language: language})
			}
		}
	}
	return health, nil
}

// contests is each name two origins define, by a load order: a contest where two mods contend for
// it, else one mod's override of another origin's; not in a database where the game repeats names,
// nor one no schema database names (a parameter, a name script makes).
func contests(sess *session.Session, preview []*index.Index, number func(source.File) int) (contested, overrides []Contest) {
	sch := sess.Install.Schema
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
		winner, adds, _, betweenMods := sess.Winner(defs)
		row := Contest{
			Database: name[0], Kind: game.KindName(name[0]), Name: name[1], Winner: slices.Index(defs, winner),
			FirstWins: slices.Contains(sess.Game.FirstWinsDatabases, name[0]), Rule: "LIOS",
		}
		if row.FirstWins {
			row.Rule = "FIOS"
		}

		// sites in the game's order: what Winner counts as an add (an inject, a merged on_action) is
		// part of the kept one; the kept one's mode, else an add's, is the row's rule
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
		if betweenMods {
			contested = append(contested, row)
		} else {
			overrides = append(overrides, row)
		}
	}
	return contested, overrides
}

// replaced is each path a mod ships a file at that another origin ships too, by a load order: the game
// loads one file of a path, so the loaded one replaces the others whole -- a contest where two mods
// ship it, else an override -- and each site lists the entries its file defines, as contests counts them,
// and those the kept file writes otherwise. Only a file in a folder the install ships files in is the
// game's: a mod's descriptor, readme, thumbnail or editor settings are not.
func replaced(sess *session.Session, preview []*index.Index, number func(source.File) int) (contested, overrides []Contest) {
	sch := sess.Install.Schema
	folders := map[string]bool{}
	for _, file := range sess.Install.Index.Files {
		if top, _, inFolder := strings.Cut(strings.ToLower(file.Rel), "/"); inFolder {
			folders[top] = true
		}
	}
	shipped := map[string][]source.File{}
	defines := map[string][]index.Def{}
	for _, idx := range preview {
		for _, file := range idx.Files {
			rel := strings.ToLower(file.Rel)
			if top, _, _ := strings.Cut(rel, "/"); !folders[top] {
				continue
			}
			shipped[rel] = append(shipped[rel], file)
			if records := idx.Records[file.Path]; records != nil {
				// its entries, not the names its script makes or declares inline
				for _, def := range records.Defs {
					if def.Name != "" && sch.Databases[def.Database] != nil && !def.Local && !def.Named {
						defines[file.Path] = append(defines[file.Path], def)
					}
				}
			}
		}
	}
	// each file with entries parsed once for its row, an image never
	parse := func(file source.File) *jomini.Result {
		if len(defines[file.Path]) == 0 {
			return nil
		}
		parsed, _, _ := sess.Parsed(file.Path)
		return parsed
	}
	for _, rel := range slices.Sorted(maps.Keys(shipped)) {
		files := shipped[rel]
		kept := slices.IndexFunc(files, func(file source.File) bool { _, loaded := index.Loaded(preview, file.Path); return loaded })
		origins := map[string]bool{}
		for _, file := range files {
			origins[file.Origin] = true
		}
		if len(origins) < 2 || kept < 0 {
			continue
		}
		row := Contest{Database: index.File, Kind: game.KindName(index.File), Name: files[kept].Rel, Winner: kept, Rule: "LIOS"}
		keptParse, keptDefs, changed := parse(files[kept]), map[string]index.Def{}, map[string]bool{}
		for _, def := range defines[files[kept].Path] {
			keptDefs[def.Name] = def
		}
		for i, file := range files {
			site, parsed := Site{File: number(file)}, parse(file)
			for _, def := range defines[file.Path] {
				site.Defines = append(site.Defines, def.Name)
				if other, both := keptDefs[def.Name]; both && i != kept && parsed != nil && keptParse != nil && !sameStatement(
					parsed.AssignmentAt(int(def.Line), int(def.Col)), keptParse.AssignmentAt(int(other.Line), int(other.Col))) {
					site.Changed, changed[def.Name] = append(site.Changed, def.Name), true
				}
			}
			row.Sites = append(row.Sites, site)
		}
		// the kept file's entries any replaced file writes otherwise
		for _, name := range row.Sites[kept].Defines {
			if changed[name] {
				row.Sites[kept].Changed = append(row.Sites[kept].Changed, name)
			}
		}
		// two mods between them, the game's file aside
		if delete(origins, game.OriginVanilla); len(origins) >= 2 {
			contested = append(contested, row)
		} else {
			overrides = append(overrides, row)
		}
	}
	return contested, overrides
}

// dependencies is what each mod needs from another, from the references its loaded files write.
func dependencies(sess *session.Session, number func(source.File) int) []Dependency {
	sch, indexes := sess.Install.Schema, sess.Indexes()
	type need struct {
		from, to string
		affected bool
	}
	needs := map[need]Dependency{}
	resolved := map[[4]string][]index.Def{} // a name resolves alike in one file, where a local definition shows
	compared := map[[2]index.Def]bool{}     // whether a definition's statement differs from the one it hides
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
			if _, done := resolved[spelled]; !done {
				resolved[spelled], _ = index.Resolve(sch, indexes, ref)
			}
			defs := resolved[spelled]

			// a row per other mod with a part in the name: the winner and each add, or, where no loaded
			// file defines it but the install does, the mod whose file at its path left it out
			database, removed := ref.Database, len(defs) == 0
			var landed []need
			if removed {
				installed, _ := index.Resolve(sch, []*index.Index{sess.Install.Index}, ref)
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

					// an add affects the mod; the winner does when it differs from the one it hides, and
					// when it hides none the name is only that mod's, so the mod depends on it
					key := need{file.Origin, holder.Origin, at > 0}
					if at == 0 {
						base, differs := hidden(sess, ref, defs, winner, compared)
						if base.Path != "" && !differs {
							continue
						}
						key.affected = base.Path != ""
					}
					// a mod's add to its own name, which only it defines, is part of what the mod depends on
					if !slices.Contains(landed, key) && !slices.Contains(landed, need{file.Origin, holder.Origin, false}) {
						landed = append(landed, key)
					}
				}
			}
			for _, key := range landed {
				row := needs[key]
				row.From, row.To, row.Affected = key.from, key.to, key.affected
				row.Sites = append(row.Sites, Site{
					File: number(file), Line: int(ref.Line), Col: int(ref.Col), Database: database, Name: ref.Name, Removed: removed,
				})
				needs[key] = row
			}
		}
	}
	return slices.SortedFunc(maps.Values(needs), func(first, second Dependency) int {
		return strings.Compare(
			first.From+first.To+strconv.FormatBool(first.Affected), second.From+second.To+strconv.FormatBool(second.Affected))
	})
}

// hidden is the definition a winning one hides, the loaded one before it, else the install's own
// (the zero Def for none), and whether the two are written otherwise; each pair is compared once.
func hidden(sess *session.Session, ref index.Ref, defs []index.Def, winner index.Def, compared map[[2]index.Def]bool) (index.Def, bool) {
	var base index.Def
	if before := slices.Index(defs, winner); before > 0 {
		base = defs[before-1]
	} else {
		installed, _ := index.Resolve(sess.Install.Schema, []*index.Index{sess.Install.Index}, ref)
		base, _, _, _ = sess.Winner(installed)
	}
	pair := [2]index.Def{winner, base}
	if _, done := compared[pair]; !done && base.Path != "" {
		_, winning := sess.Entry(winner)
		_, hiding := sess.Entry(base)
		compared[pair] = !sameStatement(winning, hiding)
	}
	return base, compared[pair]
}

// dangling is each name a mod's loaded file writes that the editor's checks find nothing for, from
// their findings, so a name dangles by the editor's rules.
func dangling(sess *session.Session, found map[string][]lsp.Diagnostic, number func(source.File) int) []Dangling {
	indexes, rows := sess.Indexes(), map[[2]string]Dangling{}
	for _, file := range sess.Workspace.Files {
		records := sess.Workspace.Records[file.Path]
		if _, loaded := index.Loaded(indexes, file.Path); records == nil || !loaded {
			continue
		}
		for _, diagnostic := range found[file.Path] {
			start := diagnostic.Range.Start
			at := slices.IndexFunc(records.Refs, func(ref index.Ref) bool {
				return int(ref.Line) == start.Line && int(ref.Col) == start.Character
			})
			if !slices.Contains(danglingChecks, diagnostic.Code) || at < 0 {
				continue
			}
			ref := records.Refs[at]
			key := [2]string{ref.Database, ref.Name}
			row := rows[key]
			row.Code, row.Database, row.Kind, row.Name = diagnostic.Code, ref.Database, game.KindName(ref.Database), ref.Name
			row.Sites = append(row.Sites, Site{File: number(file), Line: start.Line, Col: start.Character})
			rows[key] = row
		}
	}
	return slices.SortedFunc(maps.Values(rows), func(first, second Dangling) int {
		return strings.Compare(first.Database+first.Name, second.Database+second.Name)
	})
}

// sameStatement is whether two statements are written alike: key text, operator, scalar text
// (quoted or not) and block statements in order. Comments are outside the tree and never count.
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
func (s *HealthService) findings(sess *session.Session) map[string][]lsp.Diagnostic {
	sess.RLock()
	changes, files := sess.Changes, slices.Clone(sess.Workspace.Files)
	sess.RUnlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.found != nil && s.changes == changes {
		return s.found
	}
	s.changes, s.found = changes, map[string][]lsp.Diagnostic{}
	for _, file := range files {
		if diagnostics, err := lsp.Diagnose(sess, file.Path, nil); err == nil && len(diagnostics) > 0 {
			s.found[file.Path] = diagnostics
		}
	}
	return s.found
}

// GetLines is the source a pane shows at each place: a definition's statement, its first ten lines
// and a "…" where it runs on, or a reference's line.
func (s *HealthService) GetLines(workspaceID string, places []SourceAt) ([][]string, error) {
	sess, err := s.Session.live(workspaceID)
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
// key of its workspace-language files no workspace file in that language has, once, and the mod's file
// in that language it goes in.
func localization(sess *session.Session, number func(source.File) int) []Language {
	indexes := sess.Indexes()
	workspaceLanguage := strings.ToLower(sess.Language)
	languages := map[string]bool{workspaceLanguage: true}
	has := map[string]map[string]bool{}      // by language: the keys the workspace has
	filesIn := map[[2]string][]source.File{} // by origin and language: its loc files
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
			if entry.Language == workspaceLanguage {
				inWorkspaceLanguage = append(inWorkspaceLanguage, written{file, entry})
			}
		}
		if len(records.Loc) > 0 {
			in := [2]string{file.Origin, records.Loc[0].Language}
			filesIn[in] = append(filesIn[in], file)
		}
	}
	// where a key goes in another language: the file named as its own in that language, else the first
	target := func(file source.File, language string) int {
		in := filesIn[[2]string{file.Origin, language}]
		want := strings.ReplaceAll(strings.ToLower(file.Rel), workspaceLanguage, language)
		return number(in[max(slices.IndexFunc(in, func(other source.File) bool { return strings.ToLower(other.Rel) == want }), 0)])
	}

	var out []Language
	for _, language := range slices.Sorted(maps.Keys(languages)) {
		coverage := Language{Language: language}
		for _, own := range inWorkspaceLanguage {
			if language != workspaceLanguage && len(filesIn[[2]string{own.file.Origin, language}]) > 0 && !has[language][own.entry.Key] {
				row := LocRow{Key: own.entry.Key, File: number(own.file), Line: own.entry.Line, Target: target(own.file, language)}
				coverage.Untranslated = append(coverage.Untranslated, row)
			}
		}
		out = append(out, coverage)
	}
	return out
}

// LocReaders lists everything that reads one localization key.
func (s *HealthService) LocReaders(workspaceID, key string) ([]Cited, error) {
	sess, err := s.Session.live(workspaceID)
	if err != nil {
		return nil, err
	}
	sess.RLock()
	defer sess.RUnlock()
	var readers []Cited
	for _, ref := range index.LookupRefs(sess.Indexes(), index.Localization, key) {
		file, _, _ := sess.Located(ref.Path)
		readers = append(readers, Cited{Name: ref.Name, Line: int(ref.Line), Col: int(ref.Col), At: file})
	}
	return readers, nil
}
