// health.go is the workspace's health: the names two origins define (contests, with what one mod needs from another), the mods' references nothing defines, the localization each language lacks, and the definitions without the file the game finds for them. Every count is whole; the rows are cut where the page says. A declared fact accuses (§6a); a learned one is listed.
package views

import (
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/index"
	"paradox-modding-tools/services/internal/lang/loc"
	"paradox-modding-tools/services/internal/lang/schema"
	"paradox-modding-tools/services/internal/lang/session"
	"paradox-modding-tools/services/internal/source"
)

// HealthParams is what the page asks for: the load order to preview, and how many rows of each list it draws.
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

// WorkspaceHealth is the report for a session.
func WorkspaceHealth(s *session.Session, p HealthParams) Health {
	s.RLock()
	defer s.RUnlock()
	order := s.Order()
	if len(p.Order) > 0 {
		order = append([]string{game.OriginVanilla}, p.Order...)
	}
	cut := func(n int) int {
		if p.Limit > 0 && n > p.Limit {
			return p.Limit
		}
		return n
	}
	h := Health{}

	// contests, and what each mod needs from another
	for _, c := range index.Contests(s.All(), s.Schema, func(kind string) bool { return game.IsFIOS(s.Game.ID, kind) }, s.Game.EntryModes, order) {
		rule := "LIOS"
		if c.FirstWins {
			rule = "FIOS"
		}
		h.Contests = append(h.Contests, Contest{Contest: c, Rule: rule})
	}
	needs := map[[2]string]int{}
	dangling := map[[2]string]*Dangling{}
	x := s.Workspace
	for _, r := range x.Refs {
		if r.File < 0 || r.Type != schema.TypeRef || r.Kind == "" {
			continue
		}
		if _, scriptMade := schema.ScriptKinds[r.Kind]; scriptMade {
			continue
		}
		from := x.Files[r.File].Origin
		var definers []string
		for _, d := range s.Defs(r.Name) {
			if d.Kind == r.Kind && (r.Parent == "" || d.Parent == r.Parent) {
				definers = append(definers, d.At.Origin)
			}
		}
		if len(definers) == 0 {
			key := [2]string{r.Kind, r.Name}
			if dangling[key] == nil {
				dangling[key] = &Dangling{Kind: r.Kind, Name: r.Name, Declared: r.Declared}
			}
			dangling[key].Count++
			dangling[key].Sites = append(dangling[key].Sites, index.Cited{Ref: r, At: x.Files[r.File]})
			continue
		}
		if !slices.Contains(definers, from) && !slices.Contains(definers, game.OriginVanilla) {
			needs[[2]string{from, definers[0]}]++
		}
	}
	for pair, n := range needs {
		h.Depends = append(h.Depends, Dependency{From: pair[0], To: pair[1], Count: n})
	}
	slices.SortFunc(h.Depends, func(a, b Dependency) int { return strings.Compare(a.From+a.To, b.From+b.To) })
	for _, d := range dangling {
		h.DanglingCount++
		d.Sites = d.Sites[:cut(len(d.Sites))]
		h.Dangling = append(h.Dangling, *d)
	}
	slices.SortFunc(h.Dangling, func(a, b Dangling) int { return strings.Compare(a.Kind+a.Name, b.Kind+b.Name) })
	h.Dangling = h.Dangling[:cut(len(h.Dangling))]

	h.Loc = localization(s, cut)
	h.Files, h.FilesCount = files(s, cut)
	return h
}

// localization is each language's gaps: a mod definition's conventional key absent (missing), a mod's english key the language lacks (untranslated), and a mod's key nothing of its mod reads (orphaned).
func localization(s *session.Session, cut func(int) int) []Language {
	x := s.Workspace
	languages := map[string]bool{"english": true, s.Language: true}
	read := map[string]map[string]bool{} // by origin: the keys something of that origin reads
	has := map[string]map[string]bool{}  // by language: the keys the workspace has
	for _, e := range x.Loc {
		if e.File < 0 {
			continue
		}
		lang := loc.LanguageFromRel(x.Files[e.File].Rel)
		languages[lang] = true
		if has[lang] == nil {
			has[lang] = map[string]bool{}
		}
		has[lang][e.Key] = true
	}
	for _, r := range x.Refs {
		if r.File >= 0 && r.Type == schema.TypeLoc {
			origin := x.Files[r.File].Origin
			if read[origin] == nil {
				read[origin] = map[string]bool{}
			}
			read[origin][r.Name] = true
		}
	}
	var wanted []LocRow
	for _, d := range x.Defs {
		if d.File < 0 {
			continue
		}
		k := s.Schema.Kinds[d.Kind]
		if k == nil {
			continue
		}
		origin := x.Files[d.File].Origin
		if read[origin] == nil {
			read[origin] = map[string]bool{}
		}
		for _, c := range k.LocTemplates {
			key := loc.Expand(d.Name, []loc.Convention{c})[0]
			read[origin][key] = true
			if c.Coverage == 1 {
				wanted = append(wanted, LocRow{Key: key, Kind: d.Kind, Name: d.Name, Origin: origin, At: x.Files[d.File], Line: int(d.Line)})
			}
		}
	}

	var out []Language
	for _, lang := range slices.Sorted(func(yield func(string) bool) {
		for l := range languages {
			if l != "" && !yield(l) {
				return
			}
		}
	}) {
		l := Language{Language: lang}
		for _, w := range wanted {
			if !has[lang][w.Key] && !slices.ContainsFunc(index.All(s.All()).Entries(w.Key), func(e index.Said) bool { return loc.LanguageFromRel(e.At.Rel) == lang }) {
				l.MissingCount++
				l.Missing = append(l.Missing, w)
			}
		}
		for _, e := range x.Loc {
			if e.File < 0 {
				continue
			}
			at := x.Files[e.File]
			from := loc.LanguageFromRel(at.Rel)
			row := LocRow{Key: e.Key, Origin: at.Origin, At: at, Line: e.Line}
			if from == "english" && lang != "english" && !has[lang][e.Key] {
				l.UntranslatedCount++
				l.Untranslated = append(l.Untranslated, row)
			}
			if from == lang && !read[at.Origin][e.Key] {
				l.OrphanedCount++
				l.Orphaned = append(l.Orphaned, row)
			}
		}
		l.Missing, l.Untranslated, l.Orphaned = l.Missing[:cut(len(l.Missing))], l.Untranslated[:cut(len(l.Untranslated))], l.Orphaned[:cut(len(l.Orphaned))]
		out = append(out, l)
	}
	return out
}

// files is every mod definition of a kind whose template every vanilla definition has a file for, that has none.
func files(s *session.Session, cut func(int) int) ([]FileRow, int) {
	var out []FileRow
	for _, d := range s.Workspace.Defs {
		if d.File < 0 {
			continue
		}
		k := s.Schema.Kinds[d.Kind]
		if k == nil {
			continue
		}
		for _, t := range k.FileTemplates {
			if path := strings.ReplaceAll(t.Pattern, "{id}", d.Name); t.Coverage == 1 && !s.All().File(path) {
				out = append(out, FileRow{Found: index.Found{Def: d, At: s.Workspace.Files[d.File]}, Template: t.Pattern})
			}
		}
	}
	return out[:cut(len(out))], len(out)
}

// Lookup is one localization key: every entry of it the workspace loads, and everything that reads it.
type Lookup struct {
	Key     string        `json:"key"`
	Entries []index.Said  `json:"entries"`
	Readers []index.Cited `json:"readers"`
}

// LookupLoc looks one key up.
func LookupLoc(s *session.Session, key string) Lookup {
	s.RLock()
	defer s.RUnlock()
	l := Lookup{Key: key, Entries: index.All(s.All()).Entries(key)}
	for _, x := range s.All() {
		l.Readers = append(l.Readers, x.Referring(key)...)
	}
	l.Readers = slices.DeleteFunc(l.Readers, func(c index.Cited) bool { return c.Type != schema.TypeLoc })
	return l
}
