// compat.go builds contest, depends, and dangling rows for Workspace Health.

package views

import (
	"cmp"
	"slices"
	"strconv"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/derived"
	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/session"
)

const compatRefCap = 500
const snippetPad = 4

// maxRowSites caps how many example sites one Health row carries. Each site
// costs a file read and a full line split, and a row citing a thousand places
// tells a user no more than one citing a handful.
const maxRowSites = 20

const (
	compatConflict = "conflict"
	compatOverride = "override"
	compatDepends  = "depends"
	compatDangling = "dangling"
)

// HealthRow is one Workspace Health table row. Display labels are filled in Go.
type HealthRow struct {
	Type       string         `json:"type"`
	Kind       string         `json:"kind,omitempty"`
	Name       string         `json:"name"`
	Rule       string         `json:"rule,omitempty"`
	Winner     string         `json:"winner,omitempty"`
	WinnerName string         `json:"winnerName,omitempty"`
	Sites      []OverrideSite `json:"sites,omitempty"`
	From       string         `json:"from,omitempty"`
	FromName   string         `json:"fromName,omitempty"`
	To         string         `json:"to,omitempty"`
	ToName     string         `json:"toName,omitempty"`
	Rel        string         `json:"rel,omitempty"`
	Overlay    bool           `json:"overlay,omitempty"`
	Refs       int            `json:"refs,omitempty"`
	Language   string         `json:"language,omitempty"`
	Origin     string         `json:"origin,omitempty"`
	OriginName string         `json:"originName,omitempty"`
}

// OverrideSite is one place a key is defined or referenced.
type OverrideSite struct {
	Origin      string `json:"origin"`
	OriginName  string `json:"originName,omitempty"`
	Path        string `json:"path"`
	Rel         string `json:"rel,omitempty"`
	Line        int    `json:"line"`
	Snippet     string `json:"snippet,omitempty"`
	SnippetFrom int    `json:"snippetFrom,omitempty"`
}

// Compatibility returns contest rows plus grouped depends and dangling refs.
// Empty order uses session.Mods() origin ids. Order is used only for Contests.
func Compatibility(s *session.Session, order []string) HealthReport {
	var out HealthReport
	for _, row := range contestRows(s, order) {
		out.Rows = append(out.Rows, row)
		if row.Overlay {
			out.Overrides++
		} else {
			out.Conflicts++
		}
	}
	// From the counters, not from the slices: both are capped to the row budget,
	// so counting them reported "500 dangling" for a total conversion with
	// thousands. Coverage already had this right 180 lines away in
	// loccoverage.go; the comment there did not stop the same bug being written
	// here, which is the case for a test over both rather than a note on one.
	dep, dang, nDep, nDang := refRows(s)
	out.Depends, out.Dangling = nDep, nDang
	out.Rows = append(out.Rows, dep...)
	out.Rows = append(out.Rows, dang...)
	return out
}

func contestRows(s *session.Session, order []string) []HealthRow {
	if s.DefCount() == 0 {
		return nil
	}
	if len(order) == 0 {
		order = make([]string, 0, len(s.Mods))
		for _, m := range s.Mods {
			order = append(order, m.Origin)
		}
	}
	contests := s.Contests(order)
	out := make([]HealthRow, 0, len(contests))
	for _, c := range contests {
		sites := overrideSites(s, c.Defs)
		typ := compatConflict
		if c.Overlay {
			typ = compatOverride
		}
		out = append(out, HealthRow{
			Type: typ, Kind: c.Kind, Name: c.Name, Rule: c.Rule,
			Winner: originID(c.Winner), WinnerName: s.OriginName(originID(c.Winner)),
			Sites: sites, Overlay: c.Overlay, Refs: len(sites),
		})
	}
	slices.SortFunc(out, healthRowLess)
	return out
}

// overrideSites is one OverrideSite per origin+path+line. Same-line twins from
// a DidOpen path-key miss are dropped; distinct lines stay (two real blocks).
func overrideSites(s *session.Session, defs []derived.Def) []OverrideSite {
	seen := make(map[string]bool, len(defs))
	sites := make([]OverrideSite, 0, len(defs))
	for _, d := range defs {
		o := originID(d.Origin)
		id := o + "\x00" + derived.CanonPath(d.Path) + "\x00" + strconv.Itoa(d.Line)
		if seen[id] {
			continue
		}
		seen[id] = true
		site := OverrideSite{
			Origin:     o,
			OriginName: s.OriginName(o),
			Path:       d.Path,
			Rel:        s.DisplayRel(d.Path),
			Line:       d.Line,
		}
		fillSnippet(s, &site)
		sites = append(sites, site)
	}
	return sites
}

// fillSnippet attaches FileText ± snippetPad lines around site.Line (0-based).
func fillSnippet(s *session.Session, site *OverrideSite) {
	src := s.FileText(site.Path)
	if src == "" {
		return
	}
	lines := strings.Split(src, "\n")
	if len(lines) == 0 {
		return
	}
	hit := max(site.Line, 0)
	if hit >= len(lines) {
		hit = len(lines) - 1
	}
	start := max(hit-snippetPad, 0)
	end := min(hit+snippetPad+1, len(lines))
	site.SnippetFrom = start
	site.Snippet = strings.Join(lines[start:end], "\n")
}

func skipCompatRef(r derived.Ref) bool {
	switch r.Kind {
	case "loc", "loc-broad", "namespace":
		return true
	}
	return jomini.IsEphemeral(r.Kind)
}

// refRows returns the depends and dangling rows, each capped at compatRefCap,
// plus how many there are in total. The counts are the KPI; the slices are what
// the table can show.
func refRows(s *session.Session) (depends, dangling []HealthRow, nDepends, nDangling int) {
	type dangID struct{ kind, name, from string }
	dangMap := map[dangID]*HealthRow{}
	seenDep := map[string]bool{}
	for _, r := range s.WorkspaceRefs() {
		if skipCompatRef(r) || r.Key == "" {
			continue
		}
		from, _, ok := s.Locate(r.Path)
		from = originID(from)
		if !ok || from == "" || from == game.OriginVanilla {
			continue
		}
		siteOnce := func() OverrideSite {
			return OverrideSite{
				Origin: from, OriginName: s.OriginName(from),
				Path: r.Path, Rel: s.DisplayRel(r.Path), Line: r.Line,
			}
		}
		d := s.Resolve(r.Key)
		if d == nil {
			if r.Derived {
				continue
			}
			if s.Install.Declared.HasName(r.Key) {
				// The game's own API, not a missing object.
				continue
			}
			// Nothing anywhere defines this kind, so the reference names an
			// engine concept rather than a row that has gone missing: Vic3's
			// `relations_threshold:cordial` is a declared scope link with no
			// database behind it.
			if !s.KindHasDefs(r.Kind) {
				continue
			}
			id := dangID{r.Kind, r.Key, from}
			if g := dangMap[id]; g != nil {
				if len(g.Sites) < maxRowSites {
					g.Sites = append(g.Sites, siteOnce())
				}
				g.Refs++
				continue
			}
			site := siteOnce()
			dangMap[id] = &HealthRow{
				Type: compatDangling, Kind: r.Kind, Name: r.Key,
				Sites: []OverrideSite{site}, From: from, FromName: s.OriginName(from),
				Rel: site.Rel, Refs: 1, Origin: from, OriginName: s.OriginName(from),
			}
			continue
		}
		to := originID(d.Origin)
		if to == "" || to == game.OriginVanilla || to == from {
			continue
		}
		if len(s.VanillaDefs(r.Key)) > 0 {
			continue
		}
		id := from + "\x00" + r.Kind + "\x00" + r.Key + "\x00" + to
		if seenDep[id] {
			continue
		}
		seenDep[id] = true
		nDepends++
		// Counted above the cap, materialised below it — and siteOnce still runs
		// only for a row that is kept, so the budget still bounds the disk reads.
		if len(depends) >= compatRefCap {
			continue
		}
		depends = append(depends, HealthRow{
			Type: compatDepends, Kind: r.Kind, Name: r.Key,
			Sites: []OverrideSite{siteOnce()}, Refs: 1,
			From: from, FromName: s.OriginName(from),
			To: to, ToName: s.OriginName(to),
		})
	}
	nDangling = len(dangMap)
	dangling = make([]HealthRow, 0, nDangling)
	for _, row := range dangMap {
		dangling = append(dangling, *row)
	}
	slices.SortFunc(depends, healthRowLess)
	slices.SortFunc(dangling, healthRowLess)
	if len(dangling) > compatRefCap {
		dangling = dangling[:compatRefCap]
	}
	fillSnippets(s, depends)
	fillSnippets(s, dangling)
	return depends, dangling, nDepends, nDangling
}

// fillSnippets reads the source lines for rows that survived the row budget.
func fillSnippets(s *session.Session, rows []HealthRow) {
	for i := range rows {
		for j := range rows[i].Sites {
			fillSnippet(s, &rows[i].Sites[j])
		}
	}
}

func healthRowLess(a, b HealthRow) int {
	if c := cmp.Compare(a.Kind, b.Kind); c != 0 {
		return c
	}
	if c := cmp.Compare(a.Name, b.Name); c != 0 {
		return c
	}
	if c := cmp.Compare(a.Language, b.Language); c != 0 {
		return c
	}
	return cmp.Compare(a.From, b.From)
}
