// services_test.go holds the package's tests and what they share: one scan of the ck3 fixture
// install, a session over it with the mods a test writes, and a config in a temporary folder that
// APPDATA, XDG_CONFIG_HOME and HOME are sent to, so no test writes into the real user folders.
package services

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/index"
	"paradox-modding-tools/services/internal/local"
	"paradox-modding-tools/services/internal/session"
)

var scanned = sync.OnceValues(func() (*index.Install, error) {
	dir := filepath.Join("internal", "lang", "testdata", "ck3")
	return index.Scan(context.Background(), index.Request{Game: "ck3", Dir: dir, Version: "test", Dumps: []string{filepath.Join(dir, "docs")}})
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
		playset = append(playset, session.Mod{ID: "mod" + id, Name: "Mod " + id, Dir: dir})
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
func tempConfig(t *testing.T, installs []GameInstall, workspaces ...Workspace) *ConfigService {
	t.Helper()
	home := t.TempDir()
	t.Setenv("APPDATA", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("HOME", home)
	config := newConfig(filepath.Join(home, "config.json"))
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
		[]GameInstall{{ID: "i1", GameID: "ck3", Name: "CK3", Path: install, Version: "test"}},
		Workspace{ID: "ws", GameID: "ck3", Name: "W", InstallID: "i1", Mods: []WorkspaceMod{{ID: "m", Name: "M", Path: mod}}})
	return &SessionService{Config: config, Scan: func(context.Context, index.Request) (*index.Install, error) { return scanned() }}
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
		config := newConfig(path)
		if config.loadErr == nil {
			t.Fatal("want loadErr set for a corrupt config")
		}
		if err := config.mutate(func(cfg *Config) error {
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
		if err := config.mutate(func(cfg *Config) error {
			cfg.Installs = append(cfg.Installs, GameInstall{ID: "i1", Name: "CK3"})
			return nil
		}); err != nil {
			t.Fatalf("mutate on first run: %v", err)
		}
		var installs int
		newConfig(config.path).read(func(cfg *Config) { installs = len(cfg.Installs) })
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
	if drawn.Nodes[0].ID != "g.1" || !drawn.Nodes[0].Root || drawn.Nodes[0].Origin != "moda" || drawn.Nodes[0].Hidden != 1 {
		t.Errorf("the root, its origin, and its hidden caller: %+v", drawn.Nodes[0])
	}
	if at := slices.IndexFunc(drawn.Nodes, func(node Node) bool { return node.ID == "more-in:g.1" }); at < 0 || drawn.Nodes[at].Stub != "fired-by" || drawn.Nodes[at].Title != "+1 fired by" {
		t.Errorf("the fired-by stub: %+v", drawn.Nodes)
	}
	if found, ok := edge(drawn, "g.1", "g.2"); !ok || found.Via != "trigger_event" {
		t.Errorf("a fire is an edge via its token: %+v", drawn.Edges)
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
	drawn, _ = graphs.GetEventGraph("ws", Params{Root: "m.1", Depth: 2})
	if found, ok := edge(drawn, "m.1", "m.2"); !ok || found.Via != "m_fire → trigger_event" || slices.Contains(ids(drawn), "m_fire") {
		t.Errorf("a fire through a scripted effect: %+v %v", drawn.Edges, ids(drawn))
	}
	drawn, _ = graphs.GetEventGraph("ws", Params{Root: "m.2", Depth: 1, Expand: []string{"more-in:m.2"}})
	if found, ok := edge(drawn, "m.1", "m.2"); !ok || found.Via != "m_fire → trigger_event" {
		t.Errorf("the same chain into it: %+v", drawn.Edges)
	}
	drawn, _ = graphs.GetEventGraph("ws", Params{Root: "m.1", Depth: 2, Scripted: true})
	into, _ := edge(drawn, "m_fire", "m.2")
	if _, ok := edge(drawn, "m.1", "m_fire"); !ok || into.Via != "trigger_event" || !slices.Contains(ids(drawn), "m_fire") {
		t.Errorf("the macro drawn: %+v %v", drawn.Edges, ids(drawn))
	}
	if detail, _ := graphs.GetEventDetail("ws", "m.2"); len(detail.FiredBy) != 1 || detail.FiredBy[0] != (session.Fire{Name: "m.1", Via: "m_fire → trigger_event"}) {
		t.Errorf("the detail's fired-by, as the graph draws it: %+v", detail.FiredBy)
	}

	// a wide fan-out: the page's number of targets, then a stub; opened, all; a node cap truncates
	drawn, _ = graphs.GetEventGraph("ws", Params{Root: "w.0", Depth: 1, Fanout: 12, MaxNodes: 40})
	if at := slices.IndexFunc(drawn.Nodes, func(node Node) bool { return node.ID == "more:w.0" }); at < 0 || drawn.Nodes[at].Title != "+3 more" || len(drawn.Nodes) != 14 {
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
	// the query first; never a name no edge has, nor a macro; one kind, and the count of each
	if got, _ := graphs.SearchRoots("ws", ".2", ""); !slices.Contains(ids(&EventGraph{Nodes: got.Hits}), "v.2") {
		t.Errorf("by number: %+v", got)
	}
	if got, _ := graphs.SearchRoots("ws", "V.", ""); len(got.Hits) < 2 || got.Hits[0].ID != "v.1" || got.Hits[1].ID != "v.2" || got.Hits[0].Origin != game.OriginVanilla {
		t.Errorf("by namespace: %+v", got)
	}
	if got, _ := graphs.SearchRoots("ws", "vsub", ""); len(got.Hits) != 0 {
		t.Errorf("an event nothing fires and that fires nothing: %+v", got)
	}
	if got, _ := graphs.SearchRoots("ws", "m_fire", ""); len(got.Hits) != 0 {
		t.Errorf("a macro is a hop, not a root: %+v", got)
	}
	actions, _ := graphs.SearchRoots("ws", "", "on_action")
	if !slices.Equal(ids(&EventGraph{Nodes: actions.Hits}), []string{"on_birth"}) || !slices.ContainsFunc(actions.Databases, func(count DatabaseCount) bool { return count.Database == "events" && count.Count > 10 }) {
		t.Errorf("one kind's hits, every kind's count: %+v", actions)
	}
}

func TestDetail(t *testing.T) {
	graphs := &EventGraphService{Session: workspace(t, map[string]string{
		"events/m.txt":                         "namespace = m\nm.1 = {\n\ttype = character_event\n\ttitle = v.1.t\n\tdesc = v.1.desc\n\timmediate = { trigger_event = v.2 }\n\toption = {\n\t\tname = v.1.a\n\t}\n}\nm.2 = {\n\toption = {\n\t\tname = {\n\t\t\ttrigger = { always = yes }\n\t\t\ttext = v.1.a\n\t\t}\n\t}\n}\n",
		"localization/english/m_l_english.yml": "\xEF\xBB\xBFl_english:\n v.1.a:0 \"Go\"\n",
	})}
	detail, _ := graphs.GetEventDetail("ws", "m.1")
	if detail.Reason != "" || detail.Database != "events" || detail.At.Origin != "moda" || detail.Title != "Hello" {
		t.Fatalf("the header, its title from the title member's key: %+v", detail)
	}
	if drawn, _ := graphs.GetEventGraph("ws", Params{Root: "m.1", Depth: 1, Fanout: 5, MaxNodes: 10}); len(drawn.Nodes) == 0 || drawn.Nodes[0].Title != "Hello" {
		t.Errorf("a node's title: %+v", drawn)
	}
	if named, _ := graphs.GetEventDetail("ws", "m.2"); len(named.Options) != 1 || named.Options[0].Name != "v.1.a" || named.Options[0].Text != "Go" {
		t.Errorf("an option named by a block: %+v", named.Options)
	}
	sections := map[string]Section{}
	for _, section := range detail.Sections {
		sections[section.Key] = section
	}
	if sections["type"].Value != "character_event" || len(detail.Sections) != 5 {
		t.Errorf("the sections: %+v", detail.Sections)
	}
	if sections["desc"].Text != "Desc" {
		t.Errorf("a one-word section's loc text: %+v", sections["desc"])
	}
	if len(detail.Lines) != 9 || detail.Lines[0] != "m.1 = {" || detail.Lines[5] != "\toption = {" || detail.Lines[8] != "}" {
		t.Errorf("the definition as its file writes it: %q", detail.Lines)
	}
	if len(detail.Options) != 1 || detail.Options[0].Name != "v.1.a" || detail.Options[0].Text != "Go" {
		t.Errorf("the options: %+v", detail.Options)
	}
	if len(detail.Refs) == 0 || detail.Refs[0].Database != "loc" || len(detail.Refs[0].Refs) != 3 || detail.Refs[0].Refs[1].Name != "v.1.desc" || detail.Refs[0].Refs[0].At.Origin != "moda" {
		t.Errorf("the block's references, by kind in the order written: %+v", detail.Refs)
	}
	if len(detail.Fires) != 1 || detail.Fires[0].Name != "v.2" {
		t.Errorf("what it fires: %+v", detail.Fires)
	}
	if detail, _ := graphs.GetEventDetail("ws", "v.2"); len(detail.FiredBy) == 0 {
		t.Errorf("what fires it: %+v", detail.FiredBy)
	}
	if on, _ := graphs.GetEventDetail("ws", "on_birth"); on.Database != "on_action" || len(on.Sections) == 0 {
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

// The IDE bridge refuses everything binary, but names images apart so the caller can offer a
// preview instead of an error. Detection is by content: a .dds renamed to .png is still a .dds.
func TestReadBase64(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		head   []byte
		size   int64  // grown to this many bytes when set
		editor string // what the editor reader's refusal says; "" opens it
		raw    string // what the raw reader's refusal says; "" returns it whole
	}{
		// a PNG has a NUL at byte 8, yet the raw reader returns it whole for thumbnails
		{"png", []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0, 0, 0, 0x0D}, 0, RejectImage, ""},
		{"jpeg", []byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 0x10, 'J', 'F', 'I', 'F', 0}, 0, RejectImage, ""},
		{"gif", append([]byte("GIF89a"), 0, 0, 0), 0, RejectImage, ""},
		{"webp", append([]byte("RIFF\x00\x00\x00\x00WEBP"), 0), 0, RejectImage, ""},
		{"bmp", append([]byte("BM"), 0, 0, 0, 0), 0, RejectImage, ""},
		{"ico", []byte{0x00, 0x00, 0x01, 0x00, 0x01, 0x00}, 0, RejectImage, ""},
		// Paradox ships a lot of these; no browser decodes either.
		{"dds", append([]byte("DDS "), 0, 0, 0, 0x7C), 0, "binary", ""},
		{"tga", []byte{0x00, 0x00, 0x02, 0x00, 0x00, 0x00, 0x00, 0x00}, 0, "binary", ""},
		{"exe", append([]byte("MZ"), 0, 0, 0, 0), 0, "binary", ""},
		{"arbitrary", []byte{0x13, 0x37, 0x00, 0x42}, 0, "binary", ""},
		{"text", []byte("hello"), 0, "", ""},
		{"large", nil, maxOpenBytes + 1, "too large", "too large"},
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
		got, err := (*FileService)(nil).ReadEditableBase64(path)
		if err != nil {
			t.Fatal(err)
		}
		switch {
		case testCase.editor == "" && got.Reject != "":
			t.Errorf("%s: refused %q; text opens in the editor", testCase.name, got.Reject)
		case testCase.editor != "" && got.Reject == "":
			t.Errorf("%s: opened in the editor; nothing binary may be", testCase.name)
		case testCase.editor == RejectImage && got.Reject != RejectImage:
			t.Errorf("%s: reject %q, want %q so the UI can offer a preview",
				testCase.name, got.Reject, RejectImage)
		case testCase.editor != RejectImage && got.Reject == RejectImage:
			t.Errorf("%s: named an image, but nothing here can display it", testCase.name)
		case !strings.Contains(got.Reject, testCase.editor):
			t.Errorf("%s: reject %q, want %q", testCase.name, got.Reject, testCase.editor)
		}
		// the raw reader returns whole whatever the editor refuses, except a file too large to hold
		raw, err := (*FileService)(nil).ReadFileBase64(path)
		if err != nil {
			t.Fatal(err)
		}
		back, decodeErr := base64.StdEncoding.DecodeString(raw.B64)
		switch {
		case testCase.raw != "" && (!strings.Contains(raw.Reject, testCase.raw) || raw.B64 != ""):
			t.Errorf("%s: raw read %+v, want a refusal naming %q", testCase.name, raw, testCase.raw)
		case testCase.raw == "" && (!raw.Exists || raw.Reject != "" || decodeErr != nil || !bytes.Equal(back, testCase.head)):
			t.Errorf("%s: raw read did not round-trip (%+v)", testCase.name, raw)
		}
	}
	// a dds the editor refuses still reaches the webview through the raw reader
	path := filepath.Join(t.TempDir(), "x.dds")
	if err := os.WriteFile(path, append([]byte("DDS "), 0, 0, 0, 0x7C), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, _ := (*FileService)(nil).ReadFileBase64(path); got.Reject != "" || got.B64 == "" {
		t.Errorf("raw reader must not refuse a dds: %+v", got)
	}
}

// TestWorkspaceHealth covers the report over two mods: contests and dependencies in a chosen order,
// dangling references and what must never be one, each language's missing, untranslated and
// orphaned keys, the files a definition lacks, a site's lines, and the lookup of one key.
func TestWorkspaceHealth(t *testing.T) {
	reports := &HealthService{Session: workspace(t,
		map[string]string{
			"common/traits/a.txt":                  "brave = { name = x }\nshared_a = { name = x }\nbold = { name = x }\ncraven = { name = x }\n",
			"events/a.txt":                         "namespace = a\na.1 = {\n\ttype = character_event\n\ttitle = a.1.t\n\ttrigger = {\n\t\thas_trait = brave\n\t\thas_trait = bold_b\n\t\texists = title:k_nowhere\n\t\texists = title:k_x\n\t\tfaith = faith:catholic\n\t\texists = scope:whatever\n\t}\n\timmediate = { add_gold = small_value }\n}\n",
			"localization/english/a_l_english.yml": "\xEF\xBB\xBFl_english:\n a.1.t:0 \"Hi $VALUE|=+0$\"\n a_dead:0 \"Nothing reads me\"\n a_reader:0 \"$a_read$\"\n a_read:0 \"Read by a key\"\n bold:0 \"Bold\"\n bold_extra:0 \"More\"\n",
			"localization/french/a_l_french.yml":   "\xEF\xBB\xBFl_french:\n a.1.t:0 \"Salut\"\n",
		},
		map[string]string{
			"common/traits/b.txt":                  "brave = {\n" + strings.Repeat("\tname = x\n", 11) + "}\nshared_a = { name = x }\nbold_b = { name = x }\n",
			"localization/english/b_l_english.yml": "\xEF\xBB\xBFl_english:\n b_only:0 \"B\"\n",
		})}
	health, _ := reports.GetHealth("ws", HealthParams{})

	// contests: brave is defined three times, the last wins under LIOS; a reversed order flips it
	brave := slices.IndexFunc(health.Contests, func(contest Contest) bool { return contest.Name == "brave" })
	origin := func(report *Health, site Site) string { return report.Sources[site.File].Origin }
	if brave < 0 || health.Contests[brave].Rule != "LIOS" || origin(health, health.Contests[brave].Sites[health.Contests[brave].Winner]) != "modb" || len(health.Contests[brave].Sites) != 3 {
		t.Fatalf("brave contested by three, won by the last: %+v", health.Contests)
	}
	reversed, _ := reports.GetHealth("ws", HealthParams{Order: []string{"modb", "moda"}})
	if at := slices.IndexFunc(reversed.Contests, func(contest Contest) bool { return contest.Name == "brave" }); at < 0 || origin(reversed, reversed.Contests[at].Sites[reversed.Contests[at].Winner]) != "moda" {
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
	if !slices.Equal(braves[0], []string{"brave = {", "\tcategory = personality", "\tdiplomacy = 0", "}"}) {
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
	if len(health.Dangling) != 1 || health.Dangling[0].Name != "k_nowhere" || !health.Dangling[0].Declared || len(health.Dangling[0].Sites) != 1 {
		t.Fatalf("one dangling reference: %+v", health.Dangling)
	}
	if got := lines(health.Dangling[0].Sites, false)[0]; !slices.Equal(got, []string{"\t\texists = title:k_nowhere"}) {
		t.Errorf("a dangling site's line: %q", got)
	}

	// localization: both languages; conventional keys missing; untranslated; orphaned within the mod
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
	if got := keys(langs["english"].Missing); !slices.Equal(got, []string{"bold_b", "shared_a", "shared_a"}) {
		t.Errorf("english: a trait's own key is what every vanilla trait has; bold and brave have theirs; a name two mods define is two rows: %v", got)
	}
	if got := keys(langs["french"].Missing); len(got) != 0 {
		t.Errorf("french: missing is the workspace's language's alone, so no key is listed twice: %v", got)
	}
	if got := keys(langs["french"].Untranslated); !slices.Contains(got, "bold") || slices.Contains(got, "a.1.t") || slices.Contains(got, "b_only") {
		t.Errorf("french: an english key with no french entry is untranslated where its mod ships french; one with an entry is not, nor a key of a mod that ships none: %v", got)
	}
	if got := keys(langs["english"].Orphaned); !slices.Equal(got, []string{"a_dead", "a_reader", "b_only"}) {
		t.Errorf("orphaned: a key nothing reads, one that only reads; not one a definition names, one that goes on from it, one script reads, or one another key reads: %v", got)
	}
	if row := langs["english"].Orphaned[0]; !strings.HasSuffix(health.Sources[row.File].Rel, "a_l_english.yml") {
		t.Errorf("a row names its file by its number in Sources: %+v in %+v", row, health.Sources[row.File])
	}

	// files: the icon template covers 5 of 8 vanilla traits, so a trait lacking an icon is fine
	if len(health.Files) != 0 {
		t.Errorf("a template short of full coverage accuses nothing: %+v", health.Files)
	}

	// looking one key up: its entries in every language, and its readers
	look, _ := reports.LookupLoc("ws", "a.1.t")
	if len(look.Entries) != 2 || len(look.Readers) != 1 || look.Readers[0].At.Origin != "moda" {
		t.Errorf("a key's entries and readers: %+v", look)
	}
	if look, _ := reports.LookupLoc("ws", "v.1.t"); len(look.Entries) != 1 || look.Entries[0].At.Origin != "vanilla" || look.Entries[0].Language != "english" {
		t.Errorf("an install key: %+v", look)
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

func TestBumpVersion(t *testing.T) {
	svc := &ReleaseService{}
	for _, row := range []struct{ current, kind, want string }{
		{"1.2.0", "patch", "1.2.1"},
		{"1.2.3", "minor", "1.3.0"},
		{"", "patch", "1.0.0"},
	} {
		if got := svc.BumpVersion(row.current, row.kind); got != row.want {
			t.Errorf("BumpVersion(%q, %q) = %q, want %q", row.current, row.kind, got, row.want)
		}
	}
}

func TestTextSearch(t *testing.T) {
	t.Parallel()
	root, other := t.TempDir(), t.TempDir()
	writeTree(t, root, searchTree)
	writeTree(t, other, searchTree)
	whole := []TextSearchFolder{{Path: root}}
	var hits []TextSearchHit
	svc := &SearchService{found: func(sent TextSearchHits) { hits = append(hits, sent.Hits...) }}
	for _, row := range []struct {
		name  string
		query TextSearchQuery
		hits  int                    // -1 is any but none
		where func(path string) bool // what every hit's slash path satisfies; nil is anything
		fails bool
	}{
		{
			name:  "an include glob limits hits to events/",
			query: TextSearchQuery{Pattern: "unique_search_token", Folders: []TextSearchFolder{{Path: root, Includes: []string{"**/events/**"}}}},
			hits:  2,
			where: func(path string) bool { return strings.Contains(path, "/events/") },
		},
		{
			name:  "an exclude glob drops .txt",
			query: TextSearchQuery{Pattern: "unique_search_token", Folders: []TextSearchFolder{{Path: root, Excludes: []string{"*.txt"}}}},
			hits:  -1,
			where: func(path string) bool { return !strings.HasSuffix(path, ".txt") },
		},
		{
			name: "each folder's globs apply only to it",
			query: TextSearchQuery{Pattern: "unique_search_token", Folders: []TextSearchFolder{
				{Path: root, Includes: []string{"**/events/**"}},
				{Path: other, Includes: []string{"**/common/**"}},
			}},
			hits: 3,
			where: func(path string) bool {
				return strings.HasPrefix(path, filepath.ToSlash(root)) && strings.Contains(path, "/events/") ||
					strings.HasPrefix(path, filepath.ToSlash(other)) && strings.Contains(path, "/common/")
			},
		},
		{
			name:  ".git is not searched",
			query: TextSearchQuery{Pattern: "hidden_token", Folders: whole},
			hits:  1,
			where: func(path string) bool { return !strings.Contains(path, "/.git/") },
		},
		{
			name:  "a file with a NUL byte is skipped",
			query: TextSearchQuery{Pattern: "findme", Folders: whole},
			hits:  1,
			where: func(path string) bool { return !strings.HasSuffix(path, "binary.dat") },
		},
		{name: "case-sensitive word", query: TextSearchQuery{Pattern: "tok", IsCaseSensitive: true, IsWordMatch: true, Folders: whole}, hits: 0},
		{name: "case-insensitive word", query: TextSearchQuery{Pattern: "tok", IsWordMatch: true, Folders: whole}, hits: 2},
		{name: "regex", query: TextSearchQuery{Pattern: `tok.+only`, IsRegexp: true, Folders: whole}, hits: 1},
		{name: "an invalid regex is an error", query: TextSearchQuery{Pattern: "[", IsRegexp: true, Folders: whole}, fails: true},
	} {
		hits = nil
		result, err := svc.TextSearch(t.Context(), row.query)
		if (err != nil) != row.fails {
			t.Errorf("%s: error %v, want one %v", row.name, err, row.fails)
		}
		if err != nil || row.fails {
			continue
		}
		if row.hits < 0 && len(hits) == 0 || row.hits >= 0 && len(hits) != row.hits || result.Found != len(hits) {
			t.Errorf("%s: %d found, hits sent = %+v", row.name, result.Found, hits)
		}
		for _, hit := range hits {
			if row.where != nil && !row.where(filepath.ToSlash(hit.Path)) {
				t.Errorf("%s: leaked %s", row.name, hit.Path)
			}
		}
	}
}

func TestFileSearchNeedleAndGlob(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeTree(t, root, searchTree)
	svc := &SearchService{}
	res, err := svc.FileSearch(t.Context(), FileSearchQuery{
		FilePattern: "hit",
		Folders: []TextSearchFolder{{
			Path:     root,
			Includes: []string{"**/events/**"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
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
	var mu sync.Mutex
	var events []string
	svc.emit = func(event string, data any) {
		mu.Lock()
		events = append(events, event)
		mu.Unlock()
	}
	before, err := svc.GetLanguageHealth("ws")
	if err != nil || before.Ready || !before.CacheStale || !before.InstallOk || before.GameID != "ck3" {
		t.Fatalf("before the first build: no session, no cache: %+v (%v)", before, err)
	}

	health, err := svc.EnsureSession("ws")
	if err != nil || !health.Ready || health.CacheStale || health.ScannedAt == "" || health.Scanning {
		t.Fatalf("a session built from a scan, its cache written: %+v (%v)", health, err)
	}
	live := svc.get("ws")
	if live == nil || len(live.Mods) != 1 || !(index.All{live.Workspace}).Def("traits", "bold") {
		t.Error("the mod is indexed over the install")
	}

	// dropped and asked again, the session comes from the cache, not a scan
	scans := int32(0)
	svc.Scan = func(ctx context.Context, req index.Request) (*index.Install, error) {
		atomic.AddInt32(&scans, 1)
		return nil, os.ErrNotExist
	}
	svc.dropAllExcept("")
	if health, err := svc.EnsureSession("ws"); err != nil || !health.Ready || scans != 0 {
		t.Errorf("the cache serves the second build: %+v (%v), %d scans", health, err, scans)
	}
	if _, err := svc.EnsureSession("nowhere"); err == nil {
		t.Error("a workspace the config lacks")
	}

	// the first build writes the cache, so the concurrent ones below load instead of scanning
	svc.dropAllExcept("")
	mu.Lock()
	events = nil
	mu.Unlock()

	var wg sync.WaitGroup
	got := make([]*session.Session, 20)
	for i := range got {
		wg.Go(func() {
			got[i], _ = svc.live("ws")
		})
	}
	wg.Wait()

	mu.Lock()
	rang := append([]string(nil), events...)
	mu.Unlock()
	if got[0] == nil || slices.ContainsFunc(got, func(live *session.Session) bool { return live != got[0] }) {
		t.Error("twenty callers at once share one build and one instance")
	}
	if len(rang) != 1 || rang[0] != EventLangReady {
		t.Errorf("one build tells the frontend once: %v", rang)
	}
	if _, err := svc.live(""); err == nil || svc.get("ws") != got[0] {
		t.Error("an empty id is refused and the live session is untouched")
	}

	svc.dropAllExcept("")
	if svc.get("ws") != nil {
		t.Error("dropping every session leaves none live")
	}
}

func TestPredatesInstall(t *testing.T) {
	install := t.TempDir()
	writeTree(t, install, map[string]string{"binaries/game.exe": "x"})
	exe := filepath.Join(install, "binaries", "game.exe")
	scanned := time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339)
	if err := os.Chtimes(exe, time.Now(), time.Now()); err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		what, scannedAt, install string
		stale                    bool
	}{
		{"a binary newer than the scan: the game was patched since", scanned, install, true},
		{"a scan after the last patch is current", time.Now().UTC().Add(2 * time.Hour).Format(time.RFC3339), install, false},
		{"no scan timestamp is not stale: guessing would rescan on every poll", "", install, false},
		{"an unreadable install is not stale", scanned, filepath.Join(install, "gone"), false},
	} {
		if got := predatesInstall(row.scannedAt, row.install); got != row.stale {
			t.Errorf("%s: %v", row.what, got)
		}
	}
}

func TestScanQueue(t *testing.T) {
	svc := fixtureService(t)
	var mu sync.Mutex
	var order []string
	release := make(chan struct{})
	svc.Scan = func(ctx context.Context, req index.Request) (*index.Install, error) {
		<-release
		mu.Lock()
		order = append(order, req.Version)
		mu.Unlock()
		return nil, os.ErrNotExist
	}
	if err := svc.Config.mutate(func(cfg *Config) error {
		cfg.Installs = append(cfg.Installs, GameInstall{ID: "i2", GameID: "ck3", Path: t.TempDir(), Version: "two"}, GameInstall{ID: "i3", GameID: "ck3", Path: t.TempDir(), Version: "three"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// the first automatic scan runs, the others queue once each; the manual one waits for the slot
	svc.QueueScan("i1")
	svc.QueueScan("i2")
	svc.QueueScan("i3")
	svc.QueueScan("i2")
	queued, running := svc.Queued()
	if !running || len(queued) != 3 {
		t.Fatalf("one runs, three are queued once each: %v %v", queued, running)
	}
	done := make(chan error, 1)
	go func() { done <- svc.RebuildInstallSemantics("i1") }()
	time.Sleep(20 * time.Millisecond)
	select {
	case <-done:
		t.Fatal("a manual scan started while another ran")
	default:
	}
	for range 4 {
		release <- struct{}{}
	}
	<-done
	deadline := time.Now().Add(2 * time.Second)
	for {
		queued, running := svc.Queued()
		if len(queued) == 0 && !running {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the queue never drained: %v %v", queued, running)
		}
		time.Sleep(5 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(order) != 4 || order[0] != "test" || order[1] != "two" || order[2] != "three" || order[3] != "test" {
		t.Errorf("scans run one at a time, the queue in order, the manual one when the slot is free: %v", order)
	}
}

func TestWorkspaceService_AddReorderRemoveMods(t *testing.T) {
	config := tempConfig(t, nil, Workspace{ID: "ws1", GameID: "ck3", Name: "Test", InstallID: "inst1"})
	svc := &WorkspaceService{Config: config, Session: &SessionService{Config: config}}
	modDir := t.TempDir()
	writeTree(t, modDir, map[string]string{"a/descriptor.mod": "", "b/descriptor.mod": ""})

	// a folder with no descriptor, or no folder at all, is no mod
	if _, err := svc.AddWorkspaceMod("ws1", "None", modDir, ""); err == nil || !strings.Contains(err.Error(), "descriptor.mod") {
		t.Fatalf("a folder with no descriptor: %v", err)
	}
	if _, err := svc.AddWorkspaceMod("ws1", "Gone", filepath.Join(modDir, "gone"), ""); err == nil {
		t.Fatal("a path that is not there")
	}

	alpha, err := svc.AddWorkspaceMod("ws1", "Alpha", filepath.Join(modDir, "a"), "")
	if err != nil {
		t.Fatal(err)
	}
	beta, err := svc.AddWorkspaceMod("ws1", "Beta", filepath.Join(modDir, "b"), "")
	if err != nil {
		t.Fatal(err)
	}
	ws, err := svc.GetWorkspace("ws1")
	if err != nil {
		t.Fatal(err)
	}
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
	ws, err = svc.GetWorkspace("ws1")
	if err != nil {
		t.Fatal(err)
	}
	listed = ws.Mods
	if len(listed) != 2 || listed[0].ID != beta.ID || listed[1].ID != alpha.ID {
		t.Fatalf("reordered: %+v", listed)
	}
	workspaces, err := svc.ListWorkspaces("")
	if err != nil || len(workspaces) != 1 {
		t.Fatalf("list: %v %+v", err, workspaces)
	}
	fromList := workspaces[0].Mods
	if len(fromList) != 2 || fromList[0].ID != beta.ID || fromList[1].ID != alpha.ID {
		t.Fatalf("list order: %+v", fromList)
	}

	if err := svc.RemoveWorkspaceMod("ws1", beta.ID); err != nil {
		t.Fatal(err)
	}
	ws, err = svc.GetWorkspace("ws1")
	if err != nil {
		t.Fatal(err)
	}
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
	workspaces, err = svc.ListWorkspaces("")
	if err != nil {
		t.Fatal(err)
	}
	if len(workspaces) != 0 {
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
	svc := &WorkspaceService{Config: config, Session: &SessionService{Config: config}}
	writeTree(t, modDir, map[string]string{"descriptor.mod": ""})
	if _, err := svc.AddWorkspaceMod("ws1", "Alpha", modDir, ""); err != nil {
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
	if err := svc.SaveIdeSession("ws1", []string{"/a.txt"}, "/a.txt"); err != nil {
		t.Fatal(err)
	}
	ws, err := svc.GetWorkspace("ws1")
	if err != nil {
		t.Fatal(err)
	}
	if !ws.ResetIdeOnOpen || ws.DefaultTool != "health" || ws.HideExplorerBinaries {
		t.Fatalf("prefs: %+v", ws)
	}
	if len(ws.IdeOpenFiles) != 1 || ws.IdeActiveFile != "/a.txt" {
		t.Fatalf("ide session: %+v", ws)
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
			svc := &WorkspaceService{Config: config, Session: &SessionService{Config: config}}
			root, want := t.TempDir(), ""
			writeTree(t, root, map[string]string{game.DescriptorFile(testCase.game): ""})
			if testCase.rel != "" {
				want = filepath.Join(root, filepath.FromSlash(testCase.rel))
				writeTree(t, root, map[string]string{testCase.rel: "img"})
			}
			mod, err := svc.AddWorkspaceMod("ws", "M", root, "")
			if err != nil {
				t.Fatal(err)
			}
			if mod.Thumbnail != want {
				t.Errorf("Thumbnail = %q, want %q", mod.Thumbnail, want)
			}
		})
	}
}
