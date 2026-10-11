// pages_test.go covers the three pages over the ck3 fixture install with the mods a test writes: the Health report,
// the event graph and its detail, and the searches.

package pages

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/index"
	"paradox-modding-tools/services/internal/schema"
	"paradox-modding-tools/services/internal/workspace"
)

// eventsGlob keeps what is under an events folder, searchToken is what the fixture holds in most files, and modA and
// modB are the ids openWorkspace gives its first two mods.
const (
	eventsGlob  = "**/events/**"
	searchToken = "unique_search_token"
	modA        = "moda"
	modB        = "modb"
)

// The events the graph tests draw around: the second of a chain from a mod, its caller's stub, an event firing
// through a scripted effect and that effect, a wide fan-out, and the install's first event.
const (
	chainRoot    = "g.1"
	chainCaller  = "more-in:g.1"
	macroRoot    = "m.1"
	macroEffect  = "m_fire"
	wideRoot     = "w.0"
	installEvent = "v.1"
)

// writeTree writes each text at its slash-separated rel under root.
func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, text := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// searchTree is the Find-in-Files fixture: one token across folders and extensions, a .git file, a
// NUL file, and the word and regex lines.
var searchTree = map[string]string{
	"events/hit.txt":          "title = unique_search_token\n",
	"events/hit.yml":          "unique_search_token yml\n",
	"gfx/skip.dds":            "unique_search_token in a dds\n",
	"common/other.txt":        "unique_search_token common\n",
	".git/objects/secret.txt": "hidden_token\n",
	"visible.txt":             "hidden_token visible\n",
	"binary.dat":              "findme\x00tail",
	"ok.txt":                  "findme ok\n",
	"a.txt":                   "TOK only\npartialtoken\nanother TOK\n",
}

func TestTextSearch(t *testing.T) {
	t.Parallel()
	root, other := t.TempDir(), t.TempDir()
	writeTree(t, root, searchTree)
	writeTree(t, other, searchTree)
	whole := []TextSearchFolder{{Path: root}}
	for _, row := range []struct {
		name  string
		query TextSearchQuery
		hits  int
		fails bool
	}{
		{
			name:  "an include glob limits hits to events/",
			query: TextSearchQuery{Pattern: searchToken, Folders: []TextSearchFolder{{Path: root, Includes: []string{eventsGlob}}}},
			hits:  2,
		},
		{
			name:  "an exclude glob drops .txt",
			query: TextSearchQuery{Pattern: searchToken, Folders: []TextSearchFolder{{Path: root, Excludes: []string{"*.txt"}}}},
			hits:  1,
		},
		{
			name: "each folder's globs apply only to it",
			query: TextSearchQuery{Pattern: searchToken, Folders: []TextSearchFolder{
				{Path: root, Includes: []string{eventsGlob}},
				{Path: other, Includes: []string{"**/common/**"}},
			}},
			hits: 3,
		},
		{
			name:  ".git is not searched",
			query: TextSearchQuery{Pattern: "hidden_token", Folders: whole},
			hits:  1,
		},
		{
			name:  "a file with a NUL byte is skipped",
			query: TextSearchQuery{Pattern: "findme", Folders: whole},
			hits:  1,
		},
		{name: "case-sensitive word", query: TextSearchQuery{Pattern: "tok", IsCaseSensitive: true, IsWordMatch: true, Folders: whole}, hits: 0},
		{name: "case-insensitive word", query: TextSearchQuery{Pattern: "tok", IsWordMatch: true, Folders: whole}, hits: 2},
		{name: "regex", query: TextSearchQuery{Pattern: `tok.+only`, IsRegexp: true, Folders: whole}, hits: 1},
		{name: "an invalid regex is an error", query: TextSearchQuery{Pattern: "[", IsRegexp: true, Folders: whole}, fails: true},
	} {
		result, err := TextSearch(t.Context(), row.query, func(TextSearchHits) {})
		if (err != nil) != row.fails {
			t.Errorf("%s: error %v, want one %v", row.name, err, row.fails)
		}
		if err != nil || row.fails {
			continue
		}
		if result.Found != row.hits {
			t.Errorf("%s: %d found, want %d", row.name, result.Found, row.hits)
		}
	}
}

func TestFileSearchNeedleAndGlob(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeTree(t, root, searchTree)
	res := FileSearch(t.Context(), FileSearchQuery{
		FilePattern: "hit",
		Folders: []TextSearchFolder{{
			Path:     root,
			Includes: []string{eventsGlob},
		}},
	})
	if len(res.Hits) != 2 {
		t.Fatalf("file hits = %+v", res.Hits)
	}
	for _, hit := range res.Hits {
		if !strings.Contains(filepath.ToSlash(hit.Path), "/events/") {
			t.Fatalf("leaked %s", hit.Path)
		}
	}
}

func TestTextSearchCancel(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	files := map[string]string{}
	for i := range 40 {
		files[fmt.Sprintf("dir/%s/f%d.txt", strings.Repeat("a", i%5+1), i)] = strings.Repeat("needle\n", 200)
	}
	writeTree(t, root, files)
	query := TextSearchQuery{
		Pattern: "needle",
		Folders: []TextSearchFolder{{Path: root}},
	}

	// the full result, so the canceled run has something real to be smaller than
	full, err := TextSearch(t.Context(), query, func(TextSearchHits) {})
	if err != nil {
		t.Fatalf("uncanceled search: %v", err)
	}
	if full.Found == 0 {
		t.Fatal("fixture produced no hits; the test would prove nothing")
	}

	// canceled before the call, not on a timer: on a fast machine the search would win the race
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	res, err := TextSearch(ctx, query, func(TextSearchHits) {})
	if err != nil {
		t.Fatalf("cancellation must not surface as an error: %v", err)
	}
	if res.Found >= full.Found {
		t.Fatalf("cancel did not stop the search: got %d hits, full run is %d", res.Found, full.Found)
	}
}

var scanned = sync.OnceValues(func() (*index.Install, error) {
	dir := filepath.Join("..", "testdata", "ck3")
	return index.Scan(context.Background(), index.Request{Game: "ck3", Dir: dir, Version: "test"})
})

// openWorkspace is a session over the fixture install with the given mods, each a map of rel to text, written to
// a temporary folder.
func openWorkspace(t *testing.T, mods ...map[string]string) *workspace.Session {
	t.Helper()
	install, err := scanned()
	if err != nil {
		t.Fatal(err)
	}
	var playset []workspace.Mod
	for i, files := range mods {
		dir := t.TempDir()
		writeTree(t, dir, files)
		playset = append(playset, workspace.Mod{ID: "mod" + string(rune('a'+i)), Dir: dir})
	}
	ws, err := workspace.Open(t.Context(), game.Get("ck3"), install, playset, "")
	if err != nil {
		t.Fatal(err)
	}
	return ws
}

// healthWorkspace is a session over the fixture install with two mods: mod a defines and uses names, ships english
// and french localization, and writes references that resolve, depend on mod b and dangle; mod b redefines some.
func healthWorkspace(t *testing.T) *workspace.Session {
	t.Helper()
	return openWorkspace(t,
		map[string]string{
			"common/traits/zz_a.txt":               "brave = { name = x }\nshared_a = { name = x }\nbold = { name = x }\ncraven = { name = x }\n",
			"events/a.txt":                         "namespace = a\na.1 = {\n\ttype = character_event\n\ttitle = a.1.t\n\ttrigger = {\n\t\thas_trait = brave\n\t\thas_trait = bold_b\n\t\texists = title:k_nowhere\n\t\texists = title:k_x\n\t\tfaith = faith:catholic\n\t\texists = scope:whatever\n\t}\n\timmediate = { add_gold = small_value }\n}\n",
			"localization/english/a_l_english.yml": "\xEF\xBB\xBFl_english:\n a.1.t:0 \"Hi $VALUE|=+0$\"\n a_dead:0 \"Nothing reads me\"\n a_reader:0 \"$a_read$\"\n a_read:0 \"Read by a key\"\n bold:0 \"Bold\"\n bold_extra:0 \"More\"\n",
			"localization/french/a_l_french.yml":   "\xEF\xBB\xBFl_french:\n a.1.t:0 \"Salut\"\n",
		},
		map[string]string{
			"common/traits/zz_b.txt":               "brave = {\n" + strings.Repeat("\tname = x\n", 11) + "}\nshared_a = { name = x }\nbold_b = { name = x }\n",
			"localization/english/b_l_english.yml": "\xEF\xBB\xBFl_english:\n b_only:0 \"B\"\n",
		})
}

// healthOf is the report of a session in a load order, its findings made as the service makes them.
func healthOf(ws *workspace.Session, params HealthParams) *HealthReport {
	return Health(ws, params, (&Findings{}).Current(ws))
}

// linesAt is the source each site shows, a definition's statement or a reference's line.
func linesAt(ws *workspace.Session, health *HealthReport, sites []Site, definition bool) [][]string {
	var places []SourceAt
	for _, site := range sites {
		places = append(places, SourceAt{Path: health.Sources[site.File].Path, Line: site.Line, Col: site.Col, Definition: definition})
	}
	return Lines(ws, places)
}

// contestNamed is the test of a contest by its name.
func contestNamed(name string) func(Contest) bool {
	return func(contest Contest) bool { return contest.Name == name }
}

// TestHealthContests covers a name three origins define, won by the last file loaded, the load order a page may
// ask for, the difference between an override and a contest, and a site's lines cut at ten.
func TestHealthContests(t *testing.T) {
	ws := healthWorkspace(t)
	health := healthOf(ws, HealthParams{})

	// contests: brave is defined three times, the last file loaded wins under LIOS; files load by
	// path, so reversing the mods' order leaves the winner of two differently named files alone
	brave := slices.IndexFunc(health.Contests, contestNamed("brave"))
	origin := func(report *HealthReport, site Site) string { return report.Sources[site.File].Origin }
	if brave < 0 || health.Contests[brave].Rule != "LIOS" || origin(health, health.Contests[brave].Sites[health.Contests[brave].Winner]) != modB || len(health.Contests[brave].Sites) != 3 {
		t.Fatalf("brave contested by three, won by the last: %+v", health.Contests)
	}
	reversed := healthOf(ws, HealthParams{Order: []string{modB, modA}})
	if i := slices.IndexFunc(reversed.Contests, contestNamed("brave")); i < 0 || origin(reversed, reversed.Contests[i].Sites[reversed.Contests[i].Winner]) != modB {
		t.Errorf("the reversed order: %+v", reversed.Contests)
	}

	// one mod over the install's name is an override; two mods on a name, with or without the
	// install's, are a contest
	if slices.ContainsFunc(health.Contests, contestNamed("craven")) || !slices.ContainsFunc(health.Overrides, contestNamed("craven")) {
		t.Errorf("mod a's craven is an override: %+v %+v", health.Contests, health.Overrides)
	}
	if !slices.ContainsFunc(health.Contests, contestNamed("shared_a")) {
		t.Errorf("two mods on a name the install lacks: %+v", health.Contests)
	}

	// a site's lines, read when its row opens: a definition's short block whole, a long one cut at
	// ten lines and a "…"
	braves := linesAt(ws, health, health.Contests[brave].Sites, true)
	if !slices.Equal(braves[0], []string{"brave = {", "\tcategory = personality", "\tdiplomacy = 0", "\ticon = \"gfx/interface/icons/traits/brave.dds\"", "}"}) {
		t.Errorf("the install's brave, whole: %q", braves[0])
	}
	if cut := braves[2]; len(cut) != 11 || cut[0] != "brave = {" || cut[10] != "…" {
		t.Errorf("mod b's brave, cut: %q", cut)
	}
}

// TestHealthReferences covers what a mod uses of another's, with each use's kind, name and line, and the names
// nothing defines, never what resolves or what no check claims.
func TestHealthReferences(t *testing.T) {
	ws := healthWorkspace(t)
	health := healthOf(ws, HealthParams{})

	if i := slices.IndexFunc(health.Depends, func(dep Dependency) bool { return dep.From == modA && dep.To == modB }); i < 0 {
		t.Errorf("mod a names bold_b, which only mod b defines: %+v", health.Depends)
	} else if uses := health.Depends[i].Sites; len(uses) != 1 || uses[0].Database != "traits" || uses[0].Name != "bold_b" || !slices.Equal(linesAt(ws, health, uses, false)[0], []string{"\t\thas_trait = bold_b"}) {
		t.Errorf("each use, with its kind, name and line: %+v", uses)
	}

	// dangling: a declared cite nothing defines, with its sites; never what resolves or is unclaimed
	if len(health.Dangling) != 1 || health.Dangling[0].Name != "k_nowhere" || len(health.Dangling[0].Sites) != 1 {
		t.Fatalf("one dangling reference: %+v", health.Dangling)
	}
	if got := linesAt(ws, health, health.Dangling[0].Sites, false)[0]; !slices.Equal(got, []string{"\t\texists = title:k_nowhere"}) {
		t.Errorf("a dangling site's line: %q", got)
	}
}

// TestHealthLocalization covers each language's untranslated keys, the file a row names by its number in Sources,
// and what reads one key.
func TestHealthLocalization(t *testing.T) {
	ws := healthWorkspace(t)
	health := healthOf(ws, HealthParams{})

	langs := map[string]Language{}
	for _, loc := range health.Loc {
		langs[loc.Language] = loc
	}
	if len(langs) != 2 {
		t.Fatalf("both languages: %+v", health.Loc)
	}
	var keys []string
	for _, row := range langs["french"].Untranslated {
		keys = append(keys, row.Key)
	}
	if !slices.Contains(keys, "bold") || slices.Contains(keys, "a.1.t") || slices.Contains(keys, "b_only") {
		t.Errorf("french: an english key with no french entry is untranslated where its mod ships french; one with an entry is not, nor a key of a mod that ships none: %v", keys)
	}
	if row := langs["french"].Untranslated[0]; !strings.HasSuffix(health.Sources[row.File].Rel, "a_l_english.yml") {
		t.Errorf("a row names its file by its number in Sources: %+v in %+v", row, health.Sources[row.File])
	}

	// what reads one key
	if readers := LocReaders(ws, "a.1.t"); len(readers) != 1 || readers[0].At.Origin != modA {
		t.Errorf("a key's readers: %+v", readers)
	}
}

// ids is the ids of a graph's nodes, in order.
func ids(graph *EventGraph) []string {
	var out []string
	for _, node := range graph.Nodes {
		out = append(out, node.ID)
	}
	return out
}

// edge is the edge a graph draws between two ids.
func edge(graph *EventGraph, from, to string) (Edge, bool) {
	i := slices.IndexFunc(graph.Edges, func(edge Edge) bool { return edge.From == from && edge.To == to })
	if i < 0 {
		return Edge{}, false
	}
	return graph.Edges[i], true
}

// graphWorkspace is a session over the fixture install with one mod: a chain g.0 to g.6 and an event nothing
// connects, a wide fan-out w.0, an event firing through a scripted effect, and an on_action firing g.3.
func graphWorkspace(t *testing.T) *workspace.Session {
	t.Helper()
	chain := "namespace = g\n"
	for i := range 6 {
		chain += fmt.Sprintf("g.%d = {\n\ttype = character_event\n\timmediate = { trigger_event = g.%d }\n}\n", i, i+1)
	}
	chain += "g.6 = { type = character_event }\ng.9 = { type = character_event }\n"
	wide := "namespace = w\nw.0 = {\n\ttype = character_event\n\timmediate = {\n"
	for i := range 15 {
		wide += fmt.Sprintf("\t\ttrigger_event = w.%d\n", i+1)
	}
	wide += "\t}\n}\n"
	for i := range 15 {
		wide += fmt.Sprintf("w.%d = { type = character_event }\n", i+1)
	}
	return openWorkspace(t, map[string]string{
		"events/chain.txt":              chain,
		"events/wide.txt":               wide,
		"events/macro.txt":              "namespace = m\nm.1 = {\n\ttype = character_event\n\timmediate = { m_fire = yes }\n}\nm.2 = { type = character_event }\n",
		"common/scripted_effects/m.txt": "m_fire = {\n\ttrigger_event = m.2\n}\n",
		"common/on_action/m.txt":        "on_birth = {\n\tevents = { g.3 }\n}\n",
	})
}

// TestGraphHops covers the graph around a root: an empty root, hops out, the one hop in as a stub and its opening,
// and the page's origins.
func TestGraphHops(t *testing.T) {
	ws := graphWorkspace(t)
	params := Params{Depth: 3, Fanout: 12, MaxNodes: 40}

	if drawn := Graph(ws, Params{}); len(drawn.Nodes) != 0 || drawn.Reason == "" {
		t.Errorf("an empty root is answered with a reason: %+v", drawn)
	}
	if got := Graph(ws, Params{Root: "nothing.1"}); len(got.Nodes) != 0 || got.Reason == "" {
		t.Errorf("a root nothing defines: %+v", got)
	}

	// three hops out, the caller as a stub, the fourth hop not drawn
	params.Root = chainRoot
	drawn := Graph(ws, params)
	for _, want := range []string{chainRoot, "g.2", "g.3", "g.4"} {
		if !slices.Contains(ids(drawn), want) {
			t.Errorf("three hops out: no %s in %v", want, ids(drawn))
		}
	}
	if slices.Contains(ids(drawn), "g.5") || slices.Contains(ids(drawn), "g.9") || slices.Contains(ids(drawn), "g.0") || drawn.Truncated {
		t.Errorf("a fourth hop, an unconnected event and the caller are not drawn: %v", ids(drawn))
	}
	if drawn.Nodes[0].ID != chainRoot || !drawn.Nodes[0].Root || drawn.Nodes[0].Origin != modA {
		t.Errorf("the root and its origin: %+v", drawn.Nodes[0])
	}
	if i := slices.IndexFunc(drawn.Nodes, func(node Node) bool { return node.ID == chainCaller }); i < 0 || drawn.Nodes[i].Stub != "fired-by" || drawn.Nodes[i].Title != "+1 fired by" {
		t.Errorf("the fired-by stub: %+v", drawn.Nodes)
	}
	if found, ok := edge(drawn, chainRoot, "g.2"); !ok || found.Via != "" {
		t.Errorf("a direct fire is an edge through no scripted effect: %+v", drawn.Edges)
	}

	// opening the stub draws the caller, and the on_action that fires g.3 is one hop in from g.3 only
	params.Expand = []string{chainCaller}
	drawn = Graph(ws, params)
	if !slices.Contains(ids(drawn), "g.0") || slices.ContainsFunc(drawn.Nodes, func(node Node) bool { return node.Stub == "fired-by" }) {
		t.Errorf("opening the stub: %v", ids(drawn))
	}
	if got := Graph(ws, Params{Root: "g.3", Depth: 1, Expand: []string{"more-in:g.3"}}); !slices.Contains(ids(got), "on_birth") || slices.Contains(ids(got), chainRoot) {
		t.Errorf("one hop in stops at the caller: %v", ids(got))
	}

	// the page's origins: the root and its stub stay, a node of another origin goes with its edges
	if got := Graph(ws, Params{Root: chainRoot, Depth: 3, Origins: []string{game.OriginVanilla}}); !slices.Equal(ids(got), []string{chainRoot, chainCaller}) || len(got.Edges) != 1 {
		t.Errorf("one origin: %v %+v", ids(got), got.Edges)
	}
}

// TestGraphScripted covers a scripted effect as a hop: the edge goes through it, labeled by its chain, the same
// either way round; with the page's macros shown it is a node between the two.
func TestGraphScripted(t *testing.T) {
	ws := graphWorkspace(t)

	drawn := Graph(ws, Params{Root: macroRoot, Depth: 2, Follow: true})
	if found, ok := edge(drawn, macroRoot, "m.2"); !ok || found.Via != macroEffect || slices.Contains(ids(drawn), macroEffect) {
		t.Errorf("a fire through a scripted effect: %+v %v", drawn.Edges, ids(drawn))
	}
	drawn = Graph(ws, Params{Root: "m.2", Depth: 1, Expand: []string{"more-in:m.2"}, Follow: true})
	if found, ok := edge(drawn, macroRoot, "m.2"); !ok || found.Via != macroEffect {
		t.Errorf("the same chain into it: %+v", drawn.Edges)
	}
	drawn = Graph(ws, Params{Root: macroRoot, Depth: 2, Scripted: true, Follow: true})
	into, _ := edge(drawn, macroEffect, "m.2")
	if _, ok := edge(drawn, macroRoot, macroEffect); !ok || into.Via != "" || !slices.Contains(ids(drawn), macroEffect) {
		t.Errorf("the macro drawn: %+v %v", drawn.Edges, ids(drawn))
	}
	if detail := EventDetail(ws, "m.2"); len(detail.FiredBy) != 1 || detail.FiredBy[0].Name != macroRoot || detail.FiredBy[0].Kind != "event" || detail.FiredBy[0].Via != macroEffect || detail.FiredBy[0].Through != 1 {
		t.Errorf("the detail's fired-by, as the graph draws it: %+v", detail.FiredBy)
	}
}

// TestGraphLimits covers a wide fan-out (the page's number of targets, then a stub; opened, all), the node cap's
// truncation, and the install's own fires, with a mod or none.
func TestGraphLimits(t *testing.T) {
	ws := graphWorkspace(t)

	drawn := Graph(ws, Params{Root: wideRoot, Depth: 1, Fanout: 12, MaxNodes: 40})
	if i := slices.IndexFunc(drawn.Nodes, func(node Node) bool { return node.ID == "more:w.0" }); i < 0 || drawn.Nodes[i].Title != "+3 more events" || len(drawn.Nodes) != 14 {
		t.Errorf("the more stub: %d nodes, %+v", len(drawn.Nodes), drawn.Nodes)
	}
	if got := Graph(ws, Params{Root: wideRoot, Depth: 1, Fanout: 12, MaxNodes: 40, Expand: []string{"more:w.0"}}); len(got.Nodes) != 16 {
		t.Errorf("opening the fan-out: %d nodes", len(got.Nodes))
	}
	if got := Graph(ws, Params{Root: wideRoot, Depth: 1, MaxNodes: 5}); !got.Truncated || got.Reached != 1 || len(got.Nodes) != 5 {
		t.Errorf("the node cap, at the hop it cut: %d nodes, truncated %v at %d", len(got.Nodes), got.Truncated, got.Reached)
	}

	// the install's own fires are drawn with or without a mod
	drawn = Graph(ws, Params{Root: installEvent, Depth: 1})
	if _, ok := edge(drawn, installEvent, "v.2"); !ok {
		t.Errorf("the install's fire: %+v", drawn.Edges)
	}
	if got := Graph(openWorkspace(t), Params{Root: installEvent, Depth: 1}); len(got.Nodes) == 0 {
		t.Error("with no mod at all")
	}
}

// TestSearchRoots covers the root search: an install event by its number and by its namespace, the ids that start
// with the query first; never a name no edge has, nor a macro; and the count of each kind.
func TestSearchRoots(t *testing.T) {
	ws := graphWorkspace(t)

	if got := SearchRoots(ws, ".2"); !slices.Contains(ids(&EventGraph{Nodes: got.Hits}), "v.2") {
		t.Errorf("by number: %+v", got)
	}
	if got := SearchRoots(ws, "V."); len(got.Hits) < 2 || got.Hits[0].ID != installEvent || got.Hits[1].ID != "v.2" || got.Hits[0].Origin != game.OriginVanilla {
		t.Errorf("by namespace: %+v", got)
	}
	if got := SearchRoots(ws, "vsub"); len(got.Hits) != 0 {
		t.Errorf("an event nothing fires and that fires nothing: %+v", got)
	}
	if got := SearchRoots(ws, macroEffect); len(got.Hits) != 0 {
		t.Errorf("a macro is a hop, not a root: %+v", got)
	}
	every := SearchRoots(ws, "")
	if !slices.ContainsFunc(every.Databases, func(count DatabaseCount) bool { return count.Database == "events" && count.Count > 10 }) {
		t.Errorf("every kind's count: %+v", every)
	}
}

// TestDetail covers one event opened: its header, a node's title, its sections and their loc text, its lines, its
// options, what its block names, and what it fires and is fired by.
func TestDetail(t *testing.T) {
	ws := openWorkspace(t, map[string]string{
		"events/m.txt":                         "namespace = m\nm.1 = {\n\ttype = character_event\n\ttitle = v.1.t\n\tdesc = v.1.desc\n\timmediate = { trigger_event = v.2 }\n\toption = {\n\t\tname = v.1.a\n\t}\n}\nm.2 = {\n\toption = {\n\t\tname = {\n\t\t\ttrigger = { always = yes }\n\t\t\ttext = v.1.a\n\t\t}\n\t}\n}\n",
		"localization/english/m_l_english.yml": "\xEF\xBB\xBFl_english:\n v.1.a:0 \"Go\"\n",
	})
	detail := EventDetail(ws, macroRoot)
	if detail.Reason != "" || detail.Kind != "event" || detail.At.Origin != modA {
		t.Fatalf("the header: %+v", detail)
	}
	if drawn := Graph(ws, Params{Root: macroRoot, Depth: 1, Fanout: 5, MaxNodes: 10}); len(drawn.Nodes) == 0 || drawn.Nodes[0].Title != "Hello" {
		t.Errorf("a node's title: %+v", drawn)
	}
	if named := EventDetail(ws, "m.2"); len(named.Options) != 1 || named.Options[0].Names[0].Key != "v.1.a" || named.Options[0].Names[0].Text != "Go" {
		t.Errorf("an option named by a block: %+v", named.Options)
	}
	sections := map[string]Section{}
	for _, section := range detail.Sections {
		sections[section.Key] = section
	}
	if sections["type"].Value != "character_event" || len(detail.Sections) != 5 {
		t.Errorf("the sections: %+v", detail.Sections)
	}
	if desc := sections["desc"].Parts; len(desc) == 0 || desc[0].Versions[0].Text != "Desc" {
		t.Errorf("a one-word section's loc text: %+v", sections["desc"])
	}
	if len(detail.Lines) != 9 || detail.Lines[0] != "m.1 = {" || detail.Lines[5] != "\toption = {" || detail.Lines[8] != "}" {
		t.Errorf("the definition as its file writes it: %q", detail.Lines)
	}
	if len(detail.Options) != 1 || detail.Options[0].Names[0].Key != "v.1.a" || detail.Options[0].Names[0].Text != "Go" {
		t.Errorf("the options: %+v", detail.Options)
	}
	if len(detail.Refs) == 0 || detail.Refs[0].Database != schema.Localization || len(detail.Refs[0].Refs) != 3 || detail.Refs[0].Refs[1].Name != "v.1.desc" || detail.Refs[0].Refs[0].At.Origin != modA {
		t.Errorf("the block's references, by kind in the order written: %+v", detail.Refs)
	}
	if len(detail.Fires) != 1 || detail.Fires[0].Name != "v.2" {
		t.Errorf("what it fires: %+v", detail.Fires)
	}
	if fired := EventDetail(ws, "v.2"); len(fired.FiredBy) == 0 {
		t.Errorf("what fires it: %+v", fired.FiredBy)
	}
	if on := EventDetail(ws, "on_birth"); on.Kind != "on action" || len(on.Sections) == 0 {
		t.Errorf("an on_action's detail: %+v", on)
	}
	if none := EventDetail(ws, "nothing.1"); none.Reason == "" {
		t.Error("a name nothing defines")
	}
}
