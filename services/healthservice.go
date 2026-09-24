// healthservice.go is the health page: the names two origins define, the mods' references nothing
// defines, the localization each language lacks, the definitions without the file the game finds
// for them, and one localization key looked up. Every count is whole; the rows are cut where the
// page says. A declared fact accuses; a learned one is only listed.
package services

import (
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/index"
	"paradox-modding-tools/services/internal/lang/loc"
	"paradox-modding-tools/services/internal/lang/schema"
	"paradox-modding-tools/services/internal/lang/source"
	"paradox-modding-tools/services/internal/session"
)

// HealthParams is what the page asks for: the load order to preview and the rows per list.
type HealthParams struct {
	Order []string `json:"order"` // mod ids in the order to judge; empty for the workspace's own
	Limit int      `json:"limit"` // rows per list; 0 for all
}

// Health is the report.
type Health struct {
	Contests      []Contest    `json:"contests"`
	Depends       []Dependency `json:"depends"`
	Dangling      []Dangling   `json:"dangling"`
	DanglingCount int          `json:"danglingCount"`
	Loc           []Language   `json:"loc"`
	Files         []FileRow    `json:"files"`
	FilesCount    int          `json:"filesCount"`
}

// Contest is one name two origins define, with the game's rule for its kind.
type Contest struct {
	index.Contest
	Rule string `json:"rule"` // FIOS or LIOS
}

// Dependency is one mod naming what only another mod defines.
type Dependency struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Count int    `json:"count"`
}

// Dangling is one name a mod writes that nothing defines, and where.
type Dangling struct {
	Kind, Name string
	Declared   bool          `json:"declared"` // a declared cite: the game itself would reject it
	Count      int           `json:"count"`
	Sites      []index.Cited `json:"sites"`
}

// Language is one language's localization gaps.
type Language struct {
	Language          string   `json:"language"`
	Missing           []LocRow `json:"missing"`      // a definition's conventional key, absent
	Untranslated      []LocRow `json:"untranslated"` // a mod key english has and this language lacks
	Orphaned          []LocRow `json:"orphaned"`     // a mod key nothing of its mod reads
	MissingCount      int      `json:"missingCount"`
	UntranslatedCount int      `json:"untranslatedCount"`
	OrphanedCount     int      `json:"orphanedCount"`
}

// LocRow is one key and, where it has one, the definition or the file it belongs to.
type LocRow struct {
	Key    string      `json:"key"`
	Kind   string      `json:"kind,omitempty"`
	Name   string      `json:"name,omitempty"`
	Origin string      `json:"origin"`
	At     source.File `json:"at"`
	Line   int         `json:"line"`
}

// FileRow is one definition without the file the game finds for it.
type FileRow struct {
	index.Found
	Template string `json:"template"`
}

// HealthService answers the health page.
type HealthService struct {
	Session *SessionService
}

// GetHealth is the workspace's health report.
func (v *HealthService) GetHealth(workspaceID string, params HealthParams) (*Health, error) {
	sess, err := v.Session.live(workspaceID)
	if err != nil {
		return nil, err
	}
	health := workspaceHealth(sess, params)
	return &health, nil
}

// LookupLoc looks one localization key up: its entries in the workspace and everything reading it.
func (v *HealthService) LookupLoc(workspaceID, key string) (*Lookup, error) {
	sess, err := v.Session.live(workspaceID)
	if err != nil {
		return nil, err
	}
	lookup := lookupLoc(sess, key)
	return &lookup, nil
}

func workspaceHealth(sess *session.Session, params HealthParams) Health {
	sess.RLock()
	defer sess.RUnlock()
	order := sess.Order()
	if len(params.Order) > 0 {
		order = append([]string{game.OriginVanilla}, params.Order...)
	}
	cut := func(n int) int {
		if params.Limit > 0 && n > params.Limit {
			return params.Limit
		}
		return n
	}
	health := Health{}

	// contests, and what each mod needs from another
	for _, contest := range index.Contests(sess.All(), sess.Schema, func(kind string) bool { return game.IsFIOS(sess.Game.ID, kind) }, sess.Game.EntryModes, order) {
		rule := "LIOS"
		if contest.FirstWins {
			rule = "FIOS"
		}
		health.Contests = append(health.Contests, Contest{Contest: contest, Rule: rule})
	}
	needs := map[[2]string]int{}
	dangling := map[[2]string]*Dangling{}
	idx := sess.Workspace
	for _, ref := range idx.Refs {
		if ref.File < 0 || ref.Type != schema.TypeRef || ref.Kind == "" {
			continue
		}
		if _, scriptMade := schema.ScriptKinds[ref.Kind]; scriptMade {
			continue
		}
		from := idx.Files[ref.File].Origin
		var definers []string
		for _, name := range sess.Defs(ref.Name) {
			if name.Kind == ref.Kind && (ref.Parent == "" || name.Parent == ref.Parent) {
				definers = append(definers, name.At.Origin)
			}
		}
		if len(definers) == 0 {
			key := [2]string{ref.Kind, ref.Name}
			if dangling[key] == nil {
				dangling[key] = &Dangling{Kind: ref.Kind, Name: ref.Name, Declared: ref.Declared}
			}
			dangling[key].Count++
			dangling[key].Sites = append(dangling[key].Sites, index.Cited{Ref: ref, At: idx.Files[ref.File]})
			continue
		}
		if !slices.Contains(definers, from) && !slices.Contains(definers, game.OriginVanilla) {
			needs[[2]string{from, definers[0]}]++
		}
	}
	for pair, count := range needs {
		health.Depends = append(health.Depends, Dependency{From: pair[0], To: pair[1], Count: count})
	}
	slices.SortFunc(health.Depends, func(first, second Dependency) int { return strings.Compare(first.From+first.To, second.From+second.To) })
	for _, row := range dangling {
		health.DanglingCount++
		row.Sites = row.Sites[:cut(len(row.Sites))]
		health.Dangling = append(health.Dangling, *row)
	}
	slices.SortFunc(health.Dangling, func(first, second Dangling) int {
		return strings.Compare(first.Kind+first.Name, second.Kind+second.Name)
	})
	health.Dangling = health.Dangling[:cut(len(health.Dangling))]

	health.Loc = localization(sess, cut)
	health.Files, health.FilesCount = files(sess, cut)
	return health
}

// localization is each language's gaps: a mod definition's conventional key absent (missing), a
// mod's english key the language lacks (untranslated), a key its own mod never reads (orphaned).
func localization(sess *session.Session, cut func(int) int) []Language {
	idx := sess.Workspace
	languages := map[string]bool{"english": true, sess.Language: true}
	read := map[string]map[string]bool{} // by origin: the keys something of that origin reads
	has := map[string]map[string]bool{}  // by language: the keys the workspace has
	for _, entry := range idx.Loc {
		if entry.File < 0 {
			continue
		}
		lang := loc.LanguageFromRel(idx.Files[entry.File].Rel)
		languages[lang] = true
		if has[lang] == nil {
			has[lang] = map[string]bool{}
		}
		has[lang][entry.Key] = true
	}
	for _, ref := range idx.Refs {
		if ref.File >= 0 && ref.Type == schema.TypeLoc {
			origin := idx.Files[ref.File].Origin
			if read[origin] == nil {
				read[origin] = map[string]bool{}
			}
			read[origin][ref.Name] = true
		}
	}
	var wanted []LocRow
	for _, def := range idx.Defs {
		if def.File < 0 {
			continue
		}
		kind := sess.Schema.Kinds[def.Kind]
		if kind == nil {
			continue
		}
		origin := idx.Files[def.File].Origin
		if read[origin] == nil {
			read[origin] = map[string]bool{}
		}
		for _, locTemplate := range kind.LocTemplates {
			key := loc.Expand(def.Name, []loc.Convention{locTemplate})[0]
			read[origin][key] = true
			if locTemplate.Coverage == 1 {
				wanted = append(wanted, LocRow{Key: key, Kind: def.Kind, Name: def.Name, Origin: origin, At: idx.Files[def.File], Line: int(def.Line)})
			}
		}
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
			if !has[lang][want.Key] && !slices.ContainsFunc(index.All(sess.All()).Entries(want.Key), func(said index.Said) bool { return loc.LanguageFromRel(said.At.Rel) == lang }) {
				coverage.MissingCount++
				coverage.Missing = append(coverage.Missing, want)
			}
		}
		for _, entry := range idx.Loc {
			if entry.File < 0 {
				continue
			}
			at := idx.Files[entry.File]
			from := loc.LanguageFromRel(at.Rel)
			row := LocRow{Key: entry.Key, Origin: at.Origin, At: at, Line: entry.Line}
			if from == "english" && lang != "english" && !has[lang][entry.Key] {
				coverage.UntranslatedCount++
				coverage.Untranslated = append(coverage.Untranslated, row)
			}
			if from == lang && !read[at.Origin][entry.Key] {
				coverage.OrphanedCount++
				coverage.Orphaned = append(coverage.Orphaned, row)
			}
		}
		coverage.Missing, coverage.Untranslated, coverage.Orphaned = coverage.Missing[:cut(len(coverage.Missing))], coverage.Untranslated[:cut(len(coverage.Untranslated))], coverage.Orphaned[:cut(len(coverage.Orphaned))]
		out = append(out, coverage)
	}
	return out
}

// files is each mod definition missing the templated file every vanilla one of its kind has.
func files(sess *session.Session, cut func(int) int) ([]FileRow, int) {
	var out []FileRow
	for _, def := range sess.Workspace.Defs {
		if def.File < 0 {
			continue
		}
		kind := sess.Schema.Kinds[def.Kind]
		if kind == nil {
			continue
		}
		for _, fileTemplate := range kind.FileTemplates {
			if path := strings.ReplaceAll(fileTemplate.Pattern, "{id}", def.Name); fileTemplate.Coverage == 1 && !sess.All().File(path) {
				out = append(out, FileRow{Def: def, At: sess.Workspace.Files[def.File], Template: fileTemplate.Pattern})
			}
		}
	}
	return out[:cut(len(out))], len(out)
}

// Lookup is one localization key: its entries in the workspace and everything that reads it.
type Lookup struct {
	Key     string        `json:"key"`
	Entries []index.Said  `json:"entries"`
	Readers []index.Cited `json:"readers"`
}

func lookupLoc(sess *session.Session, key string) Lookup {
	sess.RLock()
	defer sess.RUnlock()
	lookup := Lookup{Key: key, Entries: index.All(sess.All()).Entries(key)}
	for _, idx := range sess.All() {
		lookup.Readers = append(lookup.Readers, idx.Referring(key)...)
	}
	lookup.Readers = slices.DeleteFunc(lookup.Readers, func(cited index.Cited) bool { return cited.Type != schema.TypeLoc })
	return lookup
}
