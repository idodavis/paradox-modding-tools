package index

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"paradox-modding-tools/services/internal/lang/declared"
	"paradox-modding-tools/services/internal/lang/jomini"
)

var (
	fixtures   = map[string]*Index{}
	fixtureMu  sync.Mutex
	fixtureDir = filepath.Join("..", "testdata")
)

// fixture scans the testdata install for one game, once per test binary.
func fixture(t *testing.T, game string) *Index {
	t.Helper()
	fixtureMu.Lock()
	defer fixtureMu.Unlock()
	if x := fixtures[game]; x != nil {
		return x
	}
	root := filepath.Join(fixtureDir, game)
	var decl declared.Schema
	if raw, err := os.ReadFile(filepath.Join(root, "declared.json")); err == nil {
		if err := json.Unmarshal(raw, &decl); err != nil {
			t.Fatal(err)
		}
	}
	x, _, err := Scan(context.Background(), ScanRequest{GameID: game, InstallPath: root, LocLang: "english", Declared: &decl})
	if err != nil {
		t.Fatal(err)
	}
	fixtures[game] = x
	return x
}

func defOf(x *Index, key string) (Def, bool) {
	for _, d := range x.DefsAt(x.DefsByKey[key]) {
		return d, true
	}
	return Def{}, false
}

func TestScan(t *testing.T) {
	x := fixture(t, "ck3")
	t.Run("definitions", func(t *testing.T) {
		for _, tt := range []struct {
			key, kind string
			nested    bool
		}{
			{"v.1", "event", false}, {"v", "namespace", false}, {"on_birth", "on_action", false},
			{"brave", "traits", false}, {"english", "cultures", false}, {"v_effect", "scripted_effects", false},
			{"AMOUNT", "script_param", false}, {"catholic", "faith", true}, {"e_x", "landed_titles", false},
			{"k_x", "landed_titles", true}, {"c_x", "landed_titles", true}, {"my_rule", "game_rules", false},
		} {
			d, ok := defOf(x, tt.key)
			if !ok || d.Kind != tt.kind || d.Nested != tt.nested {
				t.Errorf("%s: got %+v ok=%v, want kind %s nested %v", tt.key, d, ok, tt.kind, tt.nested)
			}
		}
		// A game rule's options are rows of its own local database, never definitions or fields.
		if _, ok := defOf(x, "opt_a"); ok {
			t.Error("opt_a became a definition")
		}
		if d, _ := defOf(x, "v.1"); d.Doc != "The first vanilla event." {
			t.Errorf("leading comment = %q", d.Doc)
		}
		if d, _ := defOf(x, "AMOUNT"); d.OwnerKey != "v_effect" {
			t.Errorf("param owner = %q", d.OwnerKey)
		}
	})
	t.Run("edges", func(t *testing.T) {
		type want struct{ from, to, via, kind, name string }
		wants := []want{
			{"v.1", "v.2", "trigger_event", "effect", ""},
			{"v.1", "v.2", "trigger_event", "option", "v.1.a"},
			{"v.1", "v_effect", "", EdgeKindCall, ""},
			{"on_birth", "v.1", "events", "effect", ""},
			{"on_birth", "v.2", "random_events", "effect", ""},
		}
		for _, w := range wants {
			found := false
			for _, e := range x.EdgesAt(x.EdgesFrom[w.from]) {
				if e.To == w.to && e.Kind == w.kind && (w.via == "" || e.Via == w.via) && e.NameKey == w.name {
					found = true
				}
			}
			if !found {
				t.Errorf("edge %+v missing in %+v", w, x.EdgesAt(x.EdgesFrom[w.from]))
			}
		}
		// A fire site under a numbered branch is rescoped: the branch names a position, not a section of the scope around it.
		for _, e := range x.EdgesAt(x.EdgesFrom["v.1"]) {
			if e.To == "v.2" && e.Line == 25 && !e.Rescoped {
				t.Errorf("random_list branch fire not rescoped: %+v", e)
			}
		}
	})
	t.Run("references kept by the cache", func(t *testing.T) {
		kinds := map[string]bool{}
		for _, r := range x.RefsAt(x.RefsByKey["v.1.t"]) {
			kinds[r.Kind] = true
		}
		if !kinds["loc"] {
			t.Errorf("v.1.t loc ref missing")
		}
		if len(x.RefsByKey["v_effect"]) == 0 {
			t.Error("macro call ref missing")
		}
		if len(x.RefsByKey["is_adult"]) != 0 {
			t.Error("a declared token is not a reference the cache keeps")
		}
	})
	t.Run("schema", func(t *testing.T) {
		s := &x.Tree
		if n := s.At("event", "immediate"); n == nil || n.Class != Body || n.Body&BodyEffect == 0 {
			t.Errorf("event.immediate = %+v", n)
		}
		if n := s.At("event", "trigger"); n == nil || n.Class != Body || n.Body&BodyTrigger == 0 {
			t.Errorf("event.trigger = %+v", n)
		}
		if n := s.At("event", "cooldown"); n == nil || n.Class != Structure || !slices.Equal(s.Structs[n.Struct], []string{"days", "months"}) {
			t.Errorf("event.cooldown = %+v structs=%v", n, s.Structs["cooldown"])
		}
		if n := s.At("event", "option", "name"); n == nil || !slices.Equal(n.Kinds, []string{LabelLoc}) {
			t.Errorf("event.option.name = %+v", n)
		}
		if n := s.At("event", "immediate", "trigger_event"); n.Database() != "event" {
			t.Errorf("trigger_event kinds = %+v", n)
		}
		if n := s.At("event", "theme"); n.Database() != "event_themes" {
			t.Errorf("theme = %+v", n)
		}
		if n := s.At("religions", "faiths"); n == nil || n.Flags&Rows == 0 || n.Def != "faith" || n.Class != Database {
			t.Errorf("religions.faiths = %+v", n)
		}
		if n := s.Root("game_rules", false); n == nil || n.Flags&Rows == 0 {
			t.Errorf("game_rules root = %+v", n)
		}
		if n := s.Root("landed_titles", false); n == nil || n.Def != "landed_titles" {
			t.Errorf("landed_titles root = %+v", n)
		}
		if n := s.At("event", "immediate", "random_list"); n == nil || n.Struct != "random_list" {
			t.Errorf("random_list = %+v", n)
		}
		for _, key := range []string{"cooldown", "days", "category", "add_gold", "name"} {
			if !s.KnownKey(key) {
				t.Errorf("%s not known", key)
			}
		}
		if s.KnownKey("trigger_evnt") {
			t.Error("a key written nowhere is known")
		}
		for prefix, kind := range map[string]string{"title": "landed_titles", "culture": "cultures", "faith": "faith"} {
			if got := s.PrefixKind(prefix); got != kind {
				t.Errorf("PrefixKind(%s) = %q, want %s", prefix, got, kind)
			}
		}
		if got := s.Prefix("landed_titles"); got != "title" {
			t.Errorf("Prefix(landed_titles) = %q", got)
		}
		if r := s.Root("traits", false); len(r.Loc) == 0 || r.Loc[0].Coverage < 1 {
			t.Errorf("traits conventions = %+v", r.Loc)
		}
		if !slices.ContainsFunc(s.Nested, func(sh Shape) bool { return sh.ParentKind == "religions" && sh.ChildKind == "faith" }) {
			t.Errorf("nested shapes = %+v", s.Nested)
		}
		if got := s.Root("event", false).Members(); got[0] != "type" && !slices.Contains(got, "option") {
			t.Errorf("event members = %v", got)
		}
		if x.Docs.Fields["option"]["name"] == "" || x.Docs.Fields["traits"]["category"] == "" {
			t.Errorf("docs = %+v", x.Docs.Fields)
		}
	})
}

func TestClassify(t *testing.T) {
	s := &fixture(t, "ck3").Tree
	for _, tt := range []struct {
		key          string
		quoted, rows bool
		want         Class
	}{
		{"", false, false, ClassBare}, {"x", true, false, ClassVar}, {"30", false, false, ClassNumber},
		{"1066.1.1", false, false, ClassDate}, {"title:k_x", false, false, ClassCite}, {"scope:x", false, false, ClassEphemeral},
		{"$X$", false, false, ClassVar}, {"limit", false, false, ClassStop}, {"add_gold", false, false, ClassDeclared},
		{"v_effect", false, false, ClassMacroCall}, {"anything", false, true, ClassDefinition}, {"plain", false, false, ClassPlain},
		{"limit", false, true, ClassStop},
	} {
		if got := s.Classify(tt.key, tt.quoted, tt.rows, nil).Class; got != tt.want {
			t.Errorf("Classify(%q, %v, %v) = %d, want %d", tt.key, tt.quoted, tt.rows, got, tt.want)
		}
	}
	if !ClassPlain.Nameable() || ClassStop.Nameable() || ClassDeclared.Nameable() {
		t.Error("Nameable")
	}
}

func TestCacheRoundTrip(t *testing.T) {
	x := fixture(t, "ck3")
	path := filepath.Join(t.TempDir(), "cache.json")
	if err := SaveJSON(path, x); err != nil {
		t.Fatal(err)
	}
	y, err := LoadJSON[Index](path, CacheFormatVersion)
	if err != nil {
		t.Fatal(err)
	}
	y.wake()
	if len(y.Defs) != len(x.Defs) || len(y.Refs) != len(x.Refs) || len(y.Edges) != len(x.Edges) {
		t.Fatalf("counts %d/%d/%d vs %d/%d/%d", len(y.Defs), len(y.Refs), len(y.Edges), len(x.Defs), len(x.Refs), len(x.Edges))
	}
	if n := y.Tree.At("event", "immediate", "trigger_event"); n.Database() != "event" || !y.Tree.KnownKey("cooldown") {
		t.Errorf("schema after load: %+v", n)
	}
	if _, err := LoadJSON[Index](path, CacheFormatVersion+1); err == nil {
		t.Error("a mismatched format loaded")
	}
	if d, ok := defOf(y, "v.1"); !ok || !strings.HasSuffix(d.Path, "v_events.txt") {
		t.Errorf("path after load = %+v", d)
	}
}

func write(t *testing.T, root, rel, body string) string {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestWorkspace(t *testing.T) {
	base := fixture(t, "ck3")
	root := t.TempDir()
	write(t, root, "descriptor.mod", "name = \"m\"\n")
	ev := write(t, root, "events/m.txt", "namespace = m\nm.1 = {\n\ttype = character_event\n\timmediate = {\n\t\ttrigger_event = v.2\n\t\tmy_block = { foo = 1 }\n\t}\n}\n")
	write(t, root, "common/traits/m.txt", "bold = { category = personality }\n")
	write(t, root, "common/religion/religions/m.txt", "islam_religion = {\n\tfaiths = {\n\t\tsunni = { icon = x }\n\t}\n}\n")
	w, err := BuildWorkspace(context.Background(), "ck3", []ModInput{{Origin: "mod", Root: root}}, base)
	if err != nil {
		t.Fatal(err)
	}
	for key, kind := range map[string]string{"m.1": "event", "bold": "traits", "sunni": "faith"} {
		if d, ok := defOf(w, key); !ok || d.Kind != kind || d.Origin != "mod" {
			t.Errorf("%s = %+v ok=%v", key, d, ok)
		}
	}
	if es := w.EdgesAt(w.EdgesFrom["m.1"]); len(es) != 1 || es[0].To != "v.2" {
		t.Errorf("mod edges = %+v", es)
	}
	// A workspace learns only where the install is silent, and the install's own tree never changes.
	if w.Tree.At("event", "immediate", "my_block") == nil || base.Tree.At("event", "immediate", "my_block") != nil {
		t.Error("my_block learned in the wrong tree")
	}
	if n := w.Tree.At("event", "immediate", "trigger_event"); n != nil && n.N > 0 {
		t.Error("a position the install wrote was learned again")
	}
	// An edit runs the same passes over one file, and Drop retracts everything it added.
	id := w.PathID(ev)
	w.Drop(id)
	if len(w.DefsAt(w.DefsByKey["m.1"])) != 0 || w.Tree.At("event", "immediate", "my_block") != nil {
		t.Error("Drop left records")
	}
	f := NewFile("ck3", ev, "events/m.txt", "mod", 0).Parse("namespace = m\nm.1 = {\n\timmediate = { trigger_event = v.1 }\n}\n", jomini.Result{})
	w.IndexFile(f, base)
	if es := w.EdgesAt(w.EdgesFrom["m.1"]); len(es) != 1 || es[0].To != "v.1" {
		t.Errorf("edges after reindex = %+v", es)
	}
}

func TestWinnerAndContests(t *testing.T) {
	defs := []Def{{Kind: "traits", Key: "brave"}, {Kind: "traits", Key: "brave", Origin: "a"}, {Kind: "traits", Key: "brave", Origin: "b"}}
	order := OrderMap([]string{"a", "b"})
	if w := Winner("ck3", defs, order); w.Origin != "b" {
		t.Errorf("LIOS winner = %+v", w)
	}
	if w := Winner("ck3", []Def{{Kind: "gui_type", Key: "w", Origin: "a"}, {Kind: "gui_type", Key: "w", Origin: "b"}}, order); w.Origin != "a" {
		t.Errorf("FIOS winner = %+v", w)
	}
	rows := Contests("ck3", defs[1:], defs[:1], []string{"a", "b"})
	if len(rows) != 1 || rows[0].Winner != "b" || rows[0].ModCount != 2 || rows[0].Overlay {
		t.Errorf("contests = %+v", rows)
	}
	if got := EraDate("8082.1.1", ModCalendar{Era: "AC", EpochOffset: 7782}); got != "300.1.1 AC" {
		t.Errorf("EraDate = %q", got)
	}
	if got := EraDate("8082.1.1", ModCalendar{}); got != "" {
		t.Errorf("undeclared calendar = %q", got)
	}
}
