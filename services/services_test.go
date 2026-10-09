// services_test.go holds the package's tests and what they share: one scan of the ck3 fixture
// install, a session over it with the mods a test writes, and a config in a temporary folder that
// APPDATA, XDG_CONFIG_HOME and HOME are sent to, so no test writes into the real user folders. It
// also holds the sweep, the engine's scoreboard over every mod of a game's mod folder, which runs
// only behind PMT_TEST_SWEEP (`task test:sweep`).

package services

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/inventory"
	"paradox-modding-tools/services/internal/lang/index"
	"paradox-modding-tools/services/internal/local"
	"paradox-modding-tools/services/internal/lsp"
	"paradox-modding-tools/services/internal/session"
)

var scanned = sync.OnceValues(func() (*index.Install, error) {
	dir := filepath.Join("internal", "lang", "testdata", "ck3")
	return index.Scan(context.Background(), index.Request{Game: "ck3", Dir: dir, Version: "test"})
})

// workspace is a session service holding, as workspace "ws", a session over the fixture install
// with the given mods, each a map of rel to text.
func workspace(t *testing.T, mods ...map[string]string) *SessionService {
	t.Helper()
	install, err := scanned()
	if err != nil {
		t.Fatal(err)
	}
	var playset []session.Mod
	for i, files := range mods {
		dir := t.TempDir()
		writeTree(t, dir, files)
		id := string(rune('a' + i))
		playset = append(playset, session.Mod{ID: "mod" + id, Dir: dir})
	}
	sess, err := session.New(t.Context(), game.Get("ck3"), install, playset, "")
	if err != nil {
		t.Fatal(err)
	}
	return serving(sess)
}

// serving is a session service holding sess as workspace "ws".
func serving(sess *session.Session) *SessionService {
	served := &SessionService{}
	served.start()
	served.sessions["ws"] = sess
	return served
}

// tempConfig sends APPDATA, XDG_CONFIG_HOME and HOME to a temporary folder and opens a config.json
// there, unwritten, holding the given installs and workspaces.
func tempConfig(t *testing.T, installs []GameInstall, workspaces ...Workspace) *AppService {
	t.Helper()
	home := t.TempDir()
	t.Setenv("APPDATA", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("HOME", home)
	config := newApp(filepath.Join(home, "config.json"))
	config.cfg.Installs, config.cfg.Workspaces = installs, workspaces
	return config
}

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

func ids(graph *EventGraph) []string {
	var out []string
	for _, node := range graph.Nodes {
		out = append(out, node.ID)
	}
	return out
}

func edge(graph *EventGraph, from, to string) (Edge, bool) {
	at := slices.IndexFunc(graph.Edges, func(edge Edge) bool { return edge.From == from && edge.To == to })
	if at < 0 {
		return Edge{}, false
	}
	return graph.Edges[at], true
}

func dirNames(entries []DirEntry) map[string]bool {
	out := map[string]bool{}
	for _, entry := range entries {
		out[entry.Name] = true
	}
	return out
}

// fixtureService is a session service over a config holding the ck3 fixture install and one
// workspace with one mod, its cache folder sent to a temporary directory.
func fixtureService(t *testing.T) *SessionService {
	t.Helper()
	install, _ := filepath.Abs(filepath.Join("internal", "lang", "testdata", "ck3"))
	mod := t.TempDir()
	if err := os.CopyFS(mod, os.DirFS(filepath.Join("internal", "lang", "testdata", "mods", "ck3"))); err != nil {
		t.Fatal(err)
	}
	config := tempConfig(t,
		[]GameInstall{{ID: "i1", GameID: "ck3", Name: "CK3", Path: install}},
		Workspace{ID: "ws", GameID: "ck3", Name: "W", InstallID: "i1", Mods: []WorkspaceMod{{ID: "m", Name: "M", Path: mod}}})
	return &SessionService{App: config, Scan: func(context.Context, index.Request) (*index.Install, error) { return scanned() }}
}

// A config that cannot be read is not a first run: serving it empty and then saving would write
// nothing over every install, workspace and mod.
func TestConfig(t *testing.T) {
	t.Run("unreadable config refuses to write", func(t *testing.T) {
		path := tempConfig(t, nil).path
		body := `{"installs":"not-an-array"}`
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		config := newApp(path)
		if config.loadErr == nil {
			t.Fatal("want loadErr set for a corrupt config")
		}
		if err := config.mutate("", func(cfg *Config) error {
			cfg.Installs = append(cfg.Installs, GameInstall{ID: "new"})
			return nil
		}); err == nil {
			t.Fatal("mutate must refuse while the config is unreadable")
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(raw) != body {
			t.Fatalf("config was overwritten:\n got %s\nwant %s", raw, body)
		}
	})

	t.Run("missing config is a first run", func(t *testing.T) {
		config := tempConfig(t, nil)
		if config.loadErr != nil {
			t.Fatalf("absent config is not a failure: %v", config.loadErr)
		}
		if err := config.mutate("", func(cfg *Config) error {
			cfg.Installs = append(cfg.Installs, GameInstall{ID: "i1", Name: "CK3"})
			return nil
		}); err != nil {
			t.Fatalf("mutate on first run: %v", err)
		}
		var installs int
		newApp(config.path).read(func(cfg *Config) { installs = len(cfg.Installs) })
		if installs != 1 {
			t.Fatalf("got %d installs", installs)
		}
	})
}

// TestEventGraph covers the graph around a root: hops out, the one hop in, macros as hops, the
// stubs and their opening, the caps the page brings, and an empty root.
func TestEventGraph(t *testing.T) {
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
	graphs := &EventGraphService{Session: workspace(t, map[string]string{
		"events/chain.txt":              chain,
		"events/wide.txt":               wide,
		"events/macro.txt":              "namespace = m\nm.1 = {\n\ttype = character_event\n\timmediate = { m_fire = yes }\n}\nm.2 = { type = character_event }\n",
		"common/scripted_effects/m.txt": "m_fire = {\n\ttrigger_event = m.2\n}\n",
		"common/on_action/m.txt":        "on_birth = {\n\tevents = { g.3 }\n}\n",
	})}
	params := Params{Depth: 3, Fanout: 12, MaxNodes: 40}

	drawn, _ := graphs.GetEventGraph("ws", Params{})
	if len(drawn.Nodes) != 0 || drawn.Reason == "" {
		t.Errorf("an empty root is answered with a reason: %+v", drawn)
	}
	if got, _ := graphs.GetEventGraph("ws", Params{Root: "nothing.1"}); len(got.Nodes) != 0 || got.Reason == "" {
		t.Errorf("a root nothing defines: %+v", got)
	}

	// three hops out, the caller as a stub, the fourth hop not drawn
	params.Root = "g.1"
	drawn, _ = graphs.GetEventGraph("ws", params)
	for _, want := range []string{"g.1", "g.2", "g.3", "g.4"} {
		if !slices.Contains(ids(drawn), want) {
			t.Errorf("three hops out: no %s in %v", want, ids(drawn))
		}
	}
	if slices.Contains(ids(drawn), "g.5") || slices.Contains(ids(drawn), "g.9") || slices.Contains(ids(drawn), "g.0") || drawn.Truncated {
		t.Errorf("a fourth hop, an unconnected event and the caller are not drawn: %v", ids(drawn))
	}
	if drawn.Nodes[0].ID != "g.1" || !drawn.Nodes[0].Root || drawn.Nodes[0].Origin != "moda" {
		t.Errorf("the root and its origin: %+v", drawn.Nodes[0])
	}
	if at := slices.IndexFunc(drawn.Nodes, func(node Node) bool { return node.ID == "more-in:g.1" }); at < 0 || drawn.Nodes[at].Stub != "fired-by" || drawn.Nodes[at].Title != "+1 fired by" {
		t.Errorf("the fired-by stub: %+v", drawn.Nodes)
	}
	if found, ok := edge(drawn, "g.1", "g.2"); !ok || found.Via != "" {
		t.Errorf("a direct fire is an edge through no scripted effect: %+v", drawn.Edges)
	}

	// opening the stub draws the caller, and the on_action that fires g.3 is one hop in from g.3 only
	params.Expand = []string{"more-in:g.1"}
	drawn, _ = graphs.GetEventGraph("ws", params)
	if !slices.Contains(ids(drawn), "g.0") || slices.ContainsFunc(drawn.Nodes, func(node Node) bool { return node.Stub == "fired-by" }) {
		t.Errorf("opening the stub: %v", ids(drawn))
	}
	if got, _ := graphs.GetEventGraph("ws", Params{Root: "g.3", Depth: 1, Expand: []string{"more-in:g.3"}}); !slices.Contains(ids(got), "on_birth") || slices.Contains(ids(got), "g.1") {
		t.Errorf("one hop in stops at the caller: %v", ids(got))
	}

	// a macro is a hop: the edge goes through it, labelled by its chain, the same either way round;
	// with the page's macros shown it is a node between the two
	drawn, _ = graphs.GetEventGraph("ws", Params{Root: "m.1", Depth: 2, Follow: true})
	if found, ok := edge(drawn, "m.1", "m.2"); !ok || found.Via != "m_fire" || slices.Contains(ids(drawn), "m_fire") {
		t.Errorf("a fire through a scripted effect: %+v %v", drawn.Edges, ids(drawn))
	}
	drawn, _ = graphs.GetEventGraph("ws", Params{Root: "m.2", Depth: 1, Expand: []string{"more-in:m.2"}, Follow: true})
	if found, ok := edge(drawn, "m.1", "m.2"); !ok || found.Via != "m_fire" {
		t.Errorf("the same chain into it: %+v", drawn.Edges)
	}
	drawn, _ = graphs.GetEventGraph("ws", Params{Root: "m.1", Depth: 2, Scripted: true, Follow: true})
	into, _ := edge(drawn, "m_fire", "m.2")
	if _, ok := edge(drawn, "m.1", "m_fire"); !ok || into.Via != "" || !slices.Contains(ids(drawn), "m_fire") {
		t.Errorf("the macro drawn: %+v %v", drawn.Edges, ids(drawn))
	}
	if detail, _ := graphs.GetEventDetail("ws", "m.2"); len(detail.FiredBy) != 1 || detail.FiredBy[0].Name != "m.1" || detail.FiredBy[0].Kind != "event" || detail.FiredBy[0].Via != "m_fire" || detail.FiredBy[0].Through != 1 {
		t.Errorf("the detail's fired-by, as the graph draws it: %+v", detail.FiredBy)
	}

	// a wide fan-out: the page's number of targets, then a stub; opened, all; a node cap truncates
	drawn, _ = graphs.GetEventGraph("ws", Params{Root: "w.0", Depth: 1, Fanout: 12, MaxNodes: 40})
	if at := slices.IndexFunc(drawn.Nodes, func(node Node) bool { return node.ID == "more:w.0" }); at < 0 || drawn.Nodes[at].Title != "+3 more events" || len(drawn.Nodes) != 14 {
		t.Errorf("the more stub: %d nodes, %+v", len(drawn.Nodes), drawn.Nodes)
	}
	if got, _ := graphs.GetEventGraph("ws", Params{Root: "w.0", Depth: 1, Fanout: 12, MaxNodes: 40, Expand: []string{"more:w.0"}}); len(got.Nodes) != 16 {
		t.Errorf("opening the fan-out: %d nodes", len(got.Nodes))
	}
	if got, _ := graphs.GetEventGraph("ws", Params{Root: "w.0", Depth: 1, MaxNodes: 5}); !got.Truncated || got.Reached != 1 || len(got.Nodes) != 5 {
		t.Errorf("the node cap, at the hop it cut: %d nodes, truncated %v at %d", len(got.Nodes), got.Truncated, got.Reached)
	}

	// the install's own fires are drawn with or without a mod
	drawn, _ = graphs.GetEventGraph("ws", Params{Root: "v.1", Depth: 1})
	if _, ok := edge(drawn, "v.1", "v.2"); !ok {
		t.Errorf("the install's fire: %+v", drawn.Edges)
	}
	if got, _ := (&EventGraphService{Session: workspace(t)}).GetEventGraph("ws", Params{Root: "v.1", Depth: 1}); len(got.Nodes) == 0 {
		t.Error("with no mod at all")
	}

	// the page's origins: the root and its stub stay, a node of another origin goes with its edges
	if got, _ := graphs.GetEventGraph("ws", Params{Root: "g.1", Depth: 3, Origins: []string{game.OriginVanilla}}); !slices.Equal(ids(got), []string{"g.1", "more-in:g.1"}) || len(got.Edges) != 1 {
		t.Errorf("one origin: %v %+v", ids(got), got.Edges)
	}

	// the root search: an install event by its number and by its namespace, the ids that start with
	// the query first; never a name no edge has, nor a macro; and the count of each kind
	if got, _ := graphs.SearchRoots("ws", ".2"); !slices.Contains(ids(&EventGraph{Nodes: got.Hits}), "v.2") {
		t.Errorf("by number: %+v", got)
	}
	if got, _ := graphs.SearchRoots("ws", "V."); len(got.Hits) < 2 || got.Hits[0].ID != "v.1" || got.Hits[1].ID != "v.2" || got.Hits[0].Origin != game.OriginVanilla {
		t.Errorf("by namespace: %+v", got)
	}
	if got, _ := graphs.SearchRoots("ws", "vsub"); len(got.Hits) != 0 {
		t.Errorf("an event nothing fires and that fires nothing: %+v", got)
	}
	if got, _ := graphs.SearchRoots("ws", "m_fire"); len(got.Hits) != 0 {
		t.Errorf("a macro is a hop, not a root: %+v", got)
	}
	every, _ := graphs.SearchRoots("ws", "")
	if !slices.ContainsFunc(every.Databases, func(count DatabaseCount) bool { return count.Database == "events" && count.Count > 10 }) {
		t.Errorf("every kind's count: %+v", every)
	}
}

func TestDetail(t *testing.T) {
	graphs := &EventGraphService{Session: workspace(t, map[string]string{
		"events/m.txt":                         "namespace = m\nm.1 = {\n\ttype = character_event\n\ttitle = v.1.t\n\tdesc = v.1.desc\n\timmediate = { trigger_event = v.2 }\n\toption = {\n\t\tname = v.1.a\n\t}\n}\nm.2 = {\n\toption = {\n\t\tname = {\n\t\t\ttrigger = { always = yes }\n\t\t\ttext = v.1.a\n\t\t}\n\t}\n}\n",
		"localization/english/m_l_english.yml": "\xEF\xBB\xBFl_english:\n v.1.a:0 \"Go\"\n",
	})}
	detail, _ := graphs.GetEventDetail("ws", "m.1")
	if detail.Reason != "" || detail.Kind != "event" || detail.At.Origin != "moda" {
		t.Fatalf("the header: %+v", detail)
	}
	if drawn, _ := graphs.GetEventGraph("ws", Params{Root: "m.1", Depth: 1, Fanout: 5, MaxNodes: 10}); len(drawn.Nodes) == 0 || drawn.Nodes[0].Title != "Hello" {
		t.Errorf("a node's title: %+v", drawn)
	}
	if named, _ := graphs.GetEventDetail("ws", "m.2"); len(named.Options) != 1 || named.Options[0].Names[0].Key != "v.1.a" || named.Options[0].Names[0].Text != "Go" {
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
	if len(detail.Refs) == 0 || detail.Refs[0].Database != index.Localization || len(detail.Refs[0].Refs) != 3 || detail.Refs[0].Refs[1].Name != "v.1.desc" || detail.Refs[0].Refs[0].At.Origin != "moda" {
		t.Errorf("the block's references, by kind in the order written: %+v", detail.Refs)
	}
	if len(detail.Fires) != 1 || detail.Fires[0].Name != "v.2" {
		t.Errorf("what it fires: %+v", detail.Fires)
	}
	if detail, _ := graphs.GetEventDetail("ws", "v.2"); len(detail.FiredBy) == 0 {
		t.Errorf("what fires it: %+v", detail.FiredBy)
	}
	if on, _ := graphs.GetEventDetail("ws", "on_birth"); on.Kind != "on action" || len(on.Sections) == 0 {
		t.Errorf("an on_action's detail: %+v", on)
	}
	if none, _ := graphs.GetEventDetail("ws", "nothing.1"); none.Reason == "" {
		t.Error("a name nothing defines")
	}
}

func TestListDirectoryMetadata(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".metadata"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := &FileService{}
	hidden, err := svc.ListDirectory(root, false)
	if err != nil {
		t.Fatal(err)
	}
	names := dirNames(hidden)
	if !names[".metadata"] || !names["a.txt"] || names[".git"] {
		t.Fatalf("hide binaries list=%v", names)
	}
	all, err := svc.ListDirectory(root, true)
	if err != nil {
		t.Fatal(err)
	}
	names = dirNames(all)
	if !names[".git"] || !names[".metadata"] {
		t.Fatalf("show all list=%v", names)
	}
}

// The IDE bridge refuses everything binary, by content rather than by extension.
func TestReadText(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		head   []byte
		size   int64  // grown to this many bytes when set
		editor string // what the editor reader's refusal says; "" opens it
	}{
		// Paradox ships a lot of these; no browser decodes either.
		{"dds", append([]byte("DDS "), 0, 0, 0, 0x7C), 0, "binary"},
		{"tga", []byte{0x00, 0x00, 0x02, 0x00, 0x00, 0x00, 0x00, 0x00}, 0, "binary"},
		{"exe", append([]byte("MZ"), 0, 0, 0, 0), 0, "binary"},
		{"arbitrary", []byte{0x13, 0x37, 0x00, 0x42}, 0, "binary"},
		{"text", []byte("hello"), 0, ""},
		{"large", nil, maxOpenBytes + 1, "too large"},
	}
	for _, testCase := range cases {
		path := filepath.Join(t.TempDir(), testCase.name+".bin")
		if err := os.WriteFile(path, testCase.head, 0o644); err != nil {
			t.Fatal(err)
		}
		if testCase.size > 0 {
			if err := os.Truncate(path, testCase.size); err != nil {
				t.Fatal(err)
			}
		}
		got, err := (*FileService)(nil).ReadText(path)
		if err != nil {
			t.Fatal(err)
		}
		switch {
		case testCase.editor == "" && got.Reject != "":
			t.Errorf("%s: refused %q; text opens in the editor", testCase.name, got.Reject)
		case testCase.editor != "" && got.Reject == "":
			t.Errorf("%s: opened in the editor; nothing binary may be", testCase.name)
		case !strings.Contains(got.Reject, testCase.editor):
			t.Errorf("%s: reject %q, want %q", testCase.name, got.Reject, testCase.editor)
		}
	}
}

// TestWorkspaceHealth covers the report over two mods: contests and dependencies in a chosen order,
// dangling references and what must never be one, each language's untranslated keys, a site's
// lines, and the lookup of one key.
func TestWorkspaceHealth(t *testing.T) {
	reports := &HealthService{Session: workspace(t,
		map[string]string{
			"common/traits/zz_a.txt":               "brave = { name = x }\nshared_a = { name = x }\nbold = { name = x }\ncraven = { name = x }\n",
			"events/a.txt":                         "namespace = a\na.1 = {\n\ttype = character_event\n\ttitle = a.1.t\n\ttrigger = {\n\t\thas_trait = brave\n\t\thas_trait = bold_b\n\t\texists = title:k_nowhere\n\t\texists = title:k_x\n\t\tfaith = faith:catholic\n\t\texists = scope:whatever\n\t}\n\timmediate = { add_gold = small_value }\n}\n",
			"localization/english/a_l_english.yml": "\xEF\xBB\xBFl_english:\n a.1.t:0 \"Hi $VALUE|=+0$\"\n a_dead:0 \"Nothing reads me\"\n a_reader:0 \"$a_read$\"\n a_read:0 \"Read by a key\"\n bold:0 \"Bold\"\n bold_extra:0 \"More\"\n",
			"localization/french/a_l_french.yml":   "\xEF\xBB\xBFl_french:\n a.1.t:0 \"Salut\"\n",
		},
		map[string]string{
			"common/traits/zz_b.txt":               "brave = {\n" + strings.Repeat("\tname = x\n", 11) + "}\nshared_a = { name = x }\nbold_b = { name = x }\n",
			"localization/english/b_l_english.yml": "\xEF\xBB\xBFl_english:\n b_only:0 \"B\"\n",
		})}
	health, _ := reports.GetHealth("ws", HealthParams{})

	// contests: brave is defined three times, the last file loaded wins under LIOS; files load by
	// path, so reversing the mods' order leaves the winner of two differently named files alone
	brave := slices.IndexFunc(health.Contests, func(contest Contest) bool { return contest.Name == "brave" })
	origin := func(report *Health, site Site) string { return report.Sources[site.File].Origin }
	if brave < 0 || health.Contests[brave].Rule != "LIOS" || origin(health, health.Contests[brave].Sites[health.Contests[brave].Winner]) != "modb" || len(health.Contests[brave].Sites) != 3 {
		t.Fatalf("brave contested by three, won by the last: %+v", health.Contests)
	}
	reversed, _ := reports.GetHealth("ws", HealthParams{Order: []string{"modb", "moda"}})
	if at := slices.IndexFunc(reversed.Contests, func(contest Contest) bool { return contest.Name == "brave" }); at < 0 || origin(reversed, reversed.Contests[at].Sites[reversed.Contests[at].Winner]) != "modb" {
		t.Errorf("the reversed order: %+v", reversed.Contests)
	}

	// one mod over the install's name is an override; two mods on a name, with or without the
	// install's, are a contest
	named := func(name string) func(Contest) bool {
		return func(contest Contest) bool { return contest.Name == name }
	}
	if slices.ContainsFunc(health.Contests, named("craven")) || !slices.ContainsFunc(health.Overrides, named("craven")) {
		t.Errorf("mod a's craven is an override: %+v %+v", health.Contests, health.Overrides)
	}
	if !slices.ContainsFunc(health.Contests, named("shared_a")) {
		t.Errorf("two mods on a name the install lacks: %+v", health.Contests)
	}

	// a site's lines, read when its row opens: a definition's short block whole, a long one cut at
	// ten lines and a "…"; a reference's one line
	lines := func(sites []Site, definition bool) [][]string {
		var places []SourceAt
		for _, site := range sites {
			places = append(places, SourceAt{Path: health.Sources[site.File].Path, Line: site.Line, Col: site.Col, Definition: definition})
		}
		got, _ := reports.GetLines("ws", places)
		return got
	}
	braves := lines(health.Contests[brave].Sites, true)
	if !slices.Equal(braves[0], []string{"brave = {", "\tcategory = personality", "\tdiplomacy = 0", "\ticon = \"gfx/interface/icons/traits/brave.dds\"", "}"}) {
		t.Errorf("the install's brave, whole: %q", braves[0])
	}
	if cut := braves[2]; len(cut) != 11 || cut[0] != "brave = {" || cut[10] != "…" {
		t.Errorf("mod b's brave, cut: %q", cut)
	}
	if at := slices.IndexFunc(health.Depends, func(dep Dependency) bool { return dep.From == "moda" && dep.To == "modb" }); at < 0 {
		t.Errorf("mod a names bold_b, which only mod b defines: %+v", health.Depends)
	} else if uses := health.Depends[at].Sites; len(uses) != 1 || uses[0].Database != "traits" || uses[0].Name != "bold_b" || !slices.Equal(lines(uses, false)[0], []string{"\t\thas_trait = bold_b"}) {
		t.Errorf("each use, with its kind, name and line: %+v", uses)
	}

	// dangling: a declared cite nothing defines, with its sites; never what resolves or is unclaimed
	if len(health.Dangling) != 1 || health.Dangling[0].Name != "k_nowhere" || len(health.Dangling[0].Sites) != 1 {
		t.Fatalf("one dangling reference: %+v", health.Dangling)
	}
	if got := lines(health.Dangling[0].Sites, false)[0]; !slices.Equal(got, []string{"\t\texists = title:k_nowhere"}) {
		t.Errorf("a dangling site's line: %q", got)
	}

	// localization: both languages; untranslated
	langs := map[string]Language{}
	for _, loc := range health.Loc {
		langs[loc.Language] = loc
	}
	if len(langs) != 2 {
		t.Fatalf("both languages: %+v", health.Loc)
	}
	keys := func(rows []LocRow) []string {
		var out []string
		for _, row := range rows {
			out = append(out, row.Key)
		}
		slices.Sort(out)
		return out
	}
	if got := keys(langs["french"].Untranslated); !slices.Contains(got, "bold") || slices.Contains(got, "a.1.t") || slices.Contains(got, "b_only") {
		t.Errorf("french: an english key with no french entry is untranslated where its mod ships french; one with an entry is not, nor a key of a mod that ships none: %v", got)
	}
	if row := langs["french"].Untranslated[0]; !strings.HasSuffix(health.Sources[row.File].Rel, "a_l_english.yml") {
		t.Errorf("a row names its file by its number in Sources: %+v in %+v", row, health.Sources[row.File])
	}

	// what reads one key
	if readers, _ := reports.LocReaders("ws", "a.1.t"); len(readers) != 1 || readers[0].At.Origin != "moda" {
		t.Errorf("a key's readers: %+v", readers)
	}
}

func TestLog(t *testing.T) {
	// the child: this test binary run again as the app, which panics in a goroutine of its own
	if os.Getenv("PMT_LOG_PANIC") == "1" {
		if _, err := OpenLog("child"); err != nil {
			t.Fatal(err)
		}
		go func() { panic("pmt test panic") }()
		select {}
	}
	home := t.TempDir()
	t.Setenv("APPDATA", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("HOME", home)
	previous := slog.Default()
	t.Cleanup(func() {
		slog.SetDefault(previous)
		debug.SetCrashOutput(nil, debug.CrashOptions{}) // lets the temporary folder go
	})
	dir, err := local.AppDir()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "pmt.log")
	big := strings.Repeat("x", logRotateAt) + "\n"

	// a run that wrote its end is closed, and its log past 2 MB is moved aside
	if err := os.WriteFile(path, []byte("msg=\"session start\"\n"+big+"msg=\"session end\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	logs, err := OpenLog("test")
	if err != nil {
		t.Fatal(err)
	}
	if _, moved := os.Stat(path + ".1"); logs.LastRunCrashed() || moved != nil {
		t.Errorf("a closed run: crashed %v, moved aside %v", logs.LastRunCrashed(), moved == nil)
	}

	// the report holds the last scan's lines and the log's tail
	logs.Logger.Info("scan start", "game", "ck3")
	logs.Logger.Info("scan end", "took", "1s")
	if report := logs.diagnostics(); !strings.Contains(report, "Last scan:\n") || !strings.HasSuffix(strings.TrimSpace(report), `msg="scan end" took=1s`) {
		t.Errorf("the report: %s", report)
	}
	logs.ServiceShutdown()

	// a run with no end crashed, and its log stays whole however long it is
	if err := os.WriteFile(path, []byte("msg=\"session start\"\n"+big), 0o644); err != nil {
		t.Fatal(err)
	}
	os.Remove(path + ".1")
	if logs, err = OpenLog("test"); err != nil {
		t.Fatal(err)
	}
	if _, moved := os.Stat(path + ".1"); !logs.LastRunCrashed() || moved == nil {
		t.Errorf("a run with no end: crashed %v, moved aside %v", logs.LastRunCrashed(), moved == nil)
	}
	logs.ServiceShutdown()

	// a panic in a goroutine of the app's own lands in pmt.log, and the next run reads a crash
	child := exec.Command(os.Args[0], "-test.run=^TestLog$")
	child.Env = append(os.Environ(), "PMT_LOG_PANIC=1")
	if err := child.Run(); err == nil {
		t.Fatal("the child lived through its panic")
	}
	if raw, _ := os.ReadFile(path); !strings.Contains(string(raw), "panic: pmt test panic") {
		t.Errorf("the child's panic is not in pmt.log: %s", raw[max(0, len(raw)-400):])
	}
	if logs, err = OpenLog("test"); err != nil || !logs.LastRunCrashed() {
		t.Errorf("after the child's panic the last run crashed: %v", err)
	}
	logs.ServiceShutdown()
}

func TestTextSearch(t *testing.T) {
	t.Parallel()
	root, other := t.TempDir(), t.TempDir()
	writeTree(t, root, searchTree)
	writeTree(t, other, searchTree)
	whole := []TextSearchFolder{{Path: root}}
	svc := &SearchService{}
	for _, row := range []struct {
		name  string
		query TextSearchQuery
		hits  int
		fails bool
	}{
		{
			name:  "an include glob limits hits to events/",
			query: TextSearchQuery{Pattern: "unique_search_token", Folders: []TextSearchFolder{{Path: root, Includes: []string{"**/events/**"}}}},
			hits:  2,
		},
		{
			name:  "an exclude glob drops .txt",
			query: TextSearchQuery{Pattern: "unique_search_token", Folders: []TextSearchFolder{{Path: root, Excludes: []string{"*.txt"}}}},
			hits:  1,
		},
		{
			name: "each folder's globs apply only to it",
			query: TextSearchQuery{Pattern: "unique_search_token", Folders: []TextSearchFolder{
				{Path: root, Includes: []string{"**/events/**"}},
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
		result, err := svc.TextSearch(t.Context(), row.query)
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
	svc := &SearchService{}
	res := svc.FileSearch(t.Context(), FileSearchQuery{
		FilePattern: "hit",
		Folders: []TextSearchFolder{{
			Path:     root,
			Includes: []string{"**/events/**"},
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
	svc := &SearchService{}
	query := TextSearchQuery{
		Pattern: "needle",
		Folders: []TextSearchFolder{{Path: root}},
	}

	// the full result, so the cancelled run has something real to be smaller than
	full, err := svc.TextSearch(t.Context(), query)
	if err != nil {
		t.Fatalf("uncancelled search: %v", err)
	}
	if full.Found == 0 {
		t.Fatal("fixture produced no hits; the test would prove nothing")
	}

	// cancelled before the call, not on a timer: on a fast machine the search would win the race
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	res, err := svc.TextSearch(ctx, query)
	if err != nil {
		t.Fatalf("cancellation must not surface as an error: %v", err)
	}
	if res.Found >= full.Found {
		t.Fatalf("cancel did not stop the search: got %d hits, full run is %d", res.Found, full.Found)
	}
}

func TestEnsureSession(t *testing.T) {
	svc := fixtureService(t)
	before := svc.GetLanguageHealth("ws")
	if before.Ready || !before.InstallOk || before.InstallID == "" {
		t.Fatalf("before the first build: no session: %+v", before)
	}

	health, err := svc.EnsureSession("ws")
	if err != nil || !health.Ready {
		t.Fatalf("a session built from a scan: %+v (%v)", health, err)
	}
	live := svc.get("ws")
	if live == nil || len(live.Mods) != 1 || len(index.Lookup(live.Indexes(), "traits", "bold")) == 0 {
		t.Error("the mod is indexed over the install")
	}
	if _, err := svc.EnsureSession("nowhere"); err == nil {
		t.Error("a workspace the config lacks")
	}

	svc.dropAllExcept("")

	var wg sync.WaitGroup
	got := make([]*session.Session, 20)
	for i := range got {
		wg.Go(func() {
			got[i], _ = svc.live("ws")
		})
	}
	wg.Wait()
	if got[0] == nil || slices.ContainsFunc(got, func(live *session.Session) bool { return live != got[0] }) {
		t.Error("twenty callers at once share one build and one instance")
	}
	if _, err := svc.live(""); err == nil || svc.get("ws") != got[0] {
		t.Error("an empty id is refused and the live session is untouched")
	}

	svc.dropAllExcept("")
	if svc.get("ws") != nil {
		t.Error("dropping every session leaves none live")
	}
}

func TestWorkspaceService_AddReorderRemoveMods(t *testing.T) {
	config := tempConfig(t, nil, Workspace{ID: "ws1", GameID: "ck3", Name: "Test", InstallID: "inst1"})
	svc := &WorkspaceService{App: config, Session: &SessionService{App: config}}
	modDir := t.TempDir()
	writeTree(t, modDir, map[string]string{"a/descriptor.mod": "", "b/descriptor.mod": ""})

	// a folder with no descriptor, or no folder at all, is no mod
	if _, err := svc.AddWorkspaceMod("ws1", modDir); err == nil || !strings.Contains(err.Error(), "descriptor.mod") {
		t.Fatalf("a folder with no descriptor: %v", err)
	}
	if _, err := svc.AddWorkspaceMod("ws1", filepath.Join(modDir, "gone")); err == nil {
		t.Fatal("a path that is not there")
	}

	alpha, err := svc.AddWorkspaceMod("ws1", filepath.Join(modDir, "a"))
	if err != nil {
		t.Fatal(err)
	}
	beta, err := svc.AddWorkspaceMod("ws1", filepath.Join(modDir, "b"))
	if err != nil {
		t.Fatal(err)
	}
	ws, _, _ := svc.App.workspace("ws1")
	listed := ws.Mods
	if len(listed) != 2 || listed[0].ID != alpha.ID || listed[1].ID != beta.ID {
		t.Fatalf("initial order: %+v", listed)
	}
	if listed[0].SortOrder != 0 || listed[1].SortOrder != 1 {
		t.Fatalf("sort orders: %d %d", listed[0].SortOrder, listed[1].SortOrder)
	}

	if err := svc.ReorderWorkspaceMods("ws1", []string{alpha.ID}); err == nil {
		t.Fatal("want incomplete-set error")
	}
	if err := svc.ReorderWorkspaceMods("ws1", []string{beta.ID, alpha.ID}); err != nil {
		t.Fatal(err)
	}
	ws, _, _ = svc.App.workspace("ws1")
	listed = ws.Mods
	if len(listed) != 2 || listed[0].ID != beta.ID || listed[1].ID != alpha.ID {
		t.Fatalf("reordered: %+v", listed)
	}
	workspaces := svc.ListWorkspaces()
	if len(workspaces) != 1 {
		t.Fatalf("list: %+v", workspaces)
	}
	fromList := workspaces[0].Mods
	if len(fromList) != 2 || fromList[0].ID != beta.ID || fromList[1].ID != alpha.ID {
		t.Fatalf("list order: %+v", fromList)
	}

	if err := svc.RemoveWorkspaceMod("ws1", beta.ID); err != nil {
		t.Fatal(err)
	}
	ws, _, _ = svc.App.workspace("ws1")
	listed = ws.Mods
	if len(listed) != 1 || listed[0].ID != alpha.ID {
		t.Fatalf("after remove: %+v", listed)
	}

	if err := svc.DeleteWorkspace(""); err == nil {
		t.Fatal("empty id")
	}
	if err := svc.DeleteWorkspace("ws1"); err != nil {
		t.Fatal(err)
	}
	if workspaces = svc.ListWorkspaces(); len(workspaces) != 0 {
		t.Fatalf("listed: %+v", workspaces)
	}
	if err := svc.DeleteWorkspace("ws1"); err == nil {
		t.Fatal("want not found")
	}
}

func TestGetIdeRoots_OrderAndGameTitle(t *testing.T) {
	install := t.TempDir()
	modDir := t.TempDir()
	config := tempConfig(t,
		[]GameInstall{{ID: "inst1", GameID: "ck3", Name: "My CK3 nickname", Path: install}},
		Workspace{ID: "ws1", GameID: "ck3", Name: "Test", InstallID: "inst1"})
	svc := &WorkspaceService{App: config, Session: &SessionService{App: config}}
	writeTree(t, modDir, map[string]string{"descriptor.mod": ""})
	if _, err := svc.AddWorkspaceMod("ws1", modDir); err != nil {
		t.Fatal(err)
	}
	got, err := svc.GetIdeRoots("ws1")
	if err != nil {
		t.Fatal(err)
	}
	roots := got.Roots
	if len(roots) != 2 {
		t.Fatalf("len %d %+v", len(roots), roots)
	}
	if roots[0].Kind != "mod" || roots[1].Kind != "game" {
		t.Fatalf("kinds %+v", roots)
	}
	if roots[1].Label != "Crusader Kings III" || roots[1].Origin != "vanilla" {
		t.Fatalf("game root %+v", roots[1])
	}

	if err := svc.UpdateWorkspacePrefs("ws1", true, "event-graph", "", true); err != nil {
		t.Fatal(err)
	}
	if err := svc.UpdateWorkspacePrefs("ws1", false, "not-a-tool", "", true); err == nil {
		t.Fatal("want invalid default tool")
	}
	if err := svc.UpdateWorkspacePrefs("ws1", false, "conflicts", "", true); err == nil {
		t.Fatal("want invalid default tool")
	}
	if err := svc.UpdateWorkspacePrefs("ws1", true, "health", "", false); err != nil {
		t.Fatal(err)
	}
	ws, _, _ := svc.App.workspace("ws1")
	if !ws.ResetIdeOnOpen || ws.DefaultTool != "health" || ws.HideExplorerBinaries {
		t.Fatalf("prefs: %+v", ws)
	}
}

// An empty thumbnail is found on disk per game, EU5's in .metadata; only the backend knows where.
func TestAddWorkspaceModDiscoversTheThumbnail(t *testing.T) {
	for _, testCase := range []struct{ game, rel string }{
		{"ck3", "thumbnail.png"},
		{"vic3", "thumbnail.png"},
		{"eu5", ".metadata/thumbnail.png"},
		// A mod that put it somewhere else keeps its own choice.
		{"eu5", "thumbnail.gif"},
		// A mod with no thumbnail must not be given a path to one that is not there.
		{"ck3", ""},
	} {
		t.Run(testCase.game+"/"+testCase.rel, func(t *testing.T) {
			config := tempConfig(t, nil, Workspace{ID: "ws", GameID: testCase.game, Name: "T"})
			svc := &WorkspaceService{App: config, Session: &SessionService{App: config}}
			root, want := t.TempDir(), ""
			writeTree(t, root, map[string]string{game.DescriptorFile(testCase.game): "{}"})
			if testCase.rel != "" {
				want = filepath.Join(root, filepath.FromSlash(testCase.rel))
				writeTree(t, root, map[string]string{testCase.rel: "img"})
			}
			mod, err := svc.AddWorkspaceMod("ws", root)
			if err != nil {
				t.Fatal(err)
			}
			if mod.Thumbnail != want {
				t.Errorf("Thumbnail = %q, want %q", mod.Thumbnail, want)
			}
		})
	}
}

// The sample budgets: files diagnosed per mod, files hovered in, and words hovered in each.
const (
	diagnosed  = 300
	hoverFiles = 40
	hoverWords = 8
)

var modPath = regexp.MustCompile(`(?m)^\s*path\s*=\s*"([^"]+)"`)

// TestSweep is the scoreboard, not a gate: it asserts nothing, so a count that moves is news. Counts
// are not targets, and the samples are budgets, since one editor request walks its file. It walks
// every mod the folders PMT_TEST_MODS_<GAME> list (a game's Documents mod folder, whose .mod
// descriptors say where each mod is), over the install PMT_TEST_INSTALL_<GAME>.
func TestSweep(t *testing.T) {
	if os.Getenv("PMT_TEST_SWEEP") == "" {
		t.Skip("walks every mod of a game's mod folder; set PMT_TEST_SWEEP=1")
	}
	for _, id := range []string{"ck3", "vic3", "eu5"} {
		install, mods := os.Getenv("PMT_TEST_INSTALL_"+strings.ToUpper(id)), os.Getenv("PMT_TEST_MODS_"+strings.ToUpper(id))
		if install == "" || mods == "" {
			continue
		}
		user := game.UserDataDir(id, install)
		started := time.Now()
		scanned, err := index.Scan(t.Context(), index.Request{Game: id, Dir: install, Version: "sweep"})
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("SWEEP %s: install scanned in %.1fs", id, time.Since(started).Seconds())

		descriptors, _ := filepath.Glob(filepath.Join(mods, "*.mod"))
		for _, descriptor := range descriptors {
			raw, err := os.ReadFile(descriptor)
			if err != nil {
				continue
			}
			match := modPath.FindSubmatch(raw)
			if match == nil {
				continue
			}
			dir := string(match[1])
			if !filepath.IsAbs(dir) {
				dir = filepath.Join(user, dir)
			}
			sess, err := session.New(t.Context(), game.Get(id), scanned, []session.Mod{{ID: "mod", Dir: dir}}, "")
			if err != nil {
				t.Logf("SWEEP %s %s: %v", id, filepath.Base(descriptor), err)
				continue
			}
			sweep(t, id, filepath.Base(descriptor), sess)
		}
	}
}

// sweep reports one mod: the health counts, the findings over a sample of its script files, and
// the hover answer rate over a sample of the words in a sample of those files.
func sweep(t *testing.T, id, name string, sess *session.Session) {
	started := time.Now()
	health, _ := (&HealthService{Session: serving(sess)}).GetHealth("ws", HealthParams{})
	untranslated, defs := 0, 0
	for _, language := range health.Loc {
		untranslated += len(language.Untranslated)
	}
	for _, records := range sess.Workspace.Records {
		defs += len(records.Defs)
	}
	var files []inventory.File
	for _, file := range sess.Workspace.Files {
		if file.Format == inventory.Script {
			files = append(files, file)
		}
	}
	slices.SortFunc(files, func(first, second inventory.File) int { return strings.Compare(first.Rel, second.Rel) })

	findings := map[string]int{}
	for _, file := range spread(files, diagnosed) {
		for _, finding := range must(lsp.Diagnose(sess, file.Path, nil)) {
			findings[finding.Code]++
		}
	}
	hovers, answered := 0, 0
	for _, hoverFile := range spread(files, hoverFiles) {
		text, err := sess.Text(hoverFile.Path)
		if err != nil {
			continue
		}
		asked := 0
		for line, lineText := range strings.Split(text, "\n") {
			for _, word := range strings.Fields(lineText) {
				if strings.HasPrefix(word, "#") {
					break
				}
				col := strings.Index(lineText, word)
				if col < 0 || word == "=" || word == "{" || word == "}" {
					continue
				}
				asked++
				if asked > hoverWords {
					break
				}
				hovers++
				if card, _ := lsp.Hover(sess, hoverFile.Path, line, col); card != nil {
					answered++
				}
			}
			if asked > hoverWords {
				break
			}
		}
	}
	fmt.Printf("SWEEP %s %s: %d files, %d defs; contests %d, depends %d, dangling %d, loc untranslated %d; findings %v; hover answered %d of %d; %.1fs\n",
		id, name, len(sess.Workspace.Files), defs, len(health.Contests), len(health.Depends), len(health.Dangling), untranslated, findings, answered, hovers, time.Since(started).Seconds())
}

// spread takes at most n files evenly across the list, so a big mod's sample reaches every corner.
func spread(files []inventory.File, n int) []inventory.File {
	if len(files) <= n {
		return files
	}
	out := make([]inventory.File, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, files[i*len(files)/n])
	}
	return out
}

func must[T any](value T, err error) T {
	if err != nil {
		panic(err)
	}
	return value
}
