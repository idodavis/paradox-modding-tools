// scan_test.go covers the three ways records come to be (an install scan, a workspace of mods over it, one file read again) and holds the gate over the real installs.
package index

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/schema"
	"paradox-modding-tools/services/internal/source"
)

func TestScan(t *testing.T) {
	if _, err := Scan(context.Background(), Request{Game: "ck3", Dir: t.TempDir()}); err == nil || !strings.Contains(err.Error(), "no script files") {
		t.Errorf("a folder with no script is the wrong folder, not an empty install: %v", err)
	}
	if _, err := Scan(context.Background(), Request{Game: "hoi4", Dir: t.TempDir()}); err == nil {
		t.Error("a game the registry does not know")
	}

	in := fixture(t, "ck3")
	x := in.Index
	if len(x.Roots) != 1 || in.Schema.Kinds["traits"] == nil || len(in.Vocab.Effects) == 0 || len(in.Info) == 0 {
		t.Errorf("a scan gives the roots, the schema, the vocabulary and the game info: %v, %d kinds, %d effects, %d info folders", x.Roots, len(in.Schema.Kinds), len(in.Vocab.Effects), len(in.Info))
	}
	if !x.File("gfx/interface/icons/traits/brave.dds") || !slices.ContainsFunc(x.Files, func(f source.File) bool { return f.Format == source.Image }) {
		t.Error("every file is in the inventory, read or not: the index is the catalogue of binary files")
	}
	if !x.HasLoc("v.1.t") || !x.HasLoc("setting_opt_a") {
		t.Error("the english localization is read")
	}
	if at := slices.IndexFunc(x.Loc, func(l Loc) bool { return l.Key == "v.1.t" }); at < 0 || !strings.HasSuffix(x.Files[x.Loc[at].File].Rel, "v_l_english.yml") {
		t.Error("a loc entry knows its file")
	}
}

// workspace builds the mod of a game's fixtures over its fixture install, from a copy the test may edit.
func workspace(t *testing.T, id string) (*Index, *schema.Schema, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS(filepath.Join("..", "testdata", "mods", id))); err != nil {
		t.Fatal(err)
	}
	x, s, err := BuildWorkspace(context.Background(), []Mod{{ID: "mod", Dir: dir}}, fixture(t, id), "")
	if err != nil {
		t.Fatal(err)
	}
	return x, s, dir
}

func TestBuildWorkspace(t *testing.T) {
	install := fixture(t, "ck3")
	x, s, _ := workspace(t, "ck3")

	for _, tt := range []struct{ what, kind, name string }{
		{"a mod's event", "events", "m.1"},
		{"a mod's trait", "traits", "bold"},
		{"a saved scope a mod makes", "saved_scopes", "mod_target"},
		{"a definition of a kind only the mod has", "mod_things", "first_thing"},
	} {
		if found := x.Named(tt.name); !x.Def(tt.kind, tt.name) || found[0].At.Origin != "mod" {
			t.Errorf("%s: %+v", tt.what, found)
		}
	}
	if s.Kinds["mod_things"] == nil || install.Schema.Kinds["mod_things"] != nil {
		t.Error("a folder the install has no kind for is a kind of the workspace, and the install's schema never changes")
	}
	if s.Kinds["traits"] != install.Schema.Kinds["traits"] || s.Kinds["traits"].Shape.Members["typo_membr"] != nil {
		t.Error("a mod never adds a member to a kind the install learned")
	}

	// a mod's words name the install's definitions as well as its own
	for _, tt := range []struct{ what, kind, name string }{
		{"the install's event", "events", "v.2"},
		{"the install's macro", "scripted_effects", "v_effect"},
		{"the install's scripted trigger", "scripted_triggers", "v_rich_trigger"},
		{"the mod's own trait, in a field the install learned", "traits", "bold"},
		{"the install's loc key", "", "v.1.t"},
	} {
		if !slices.ContainsFunc(x.Refs, func(r Ref) bool { return r.Kind == tt.kind && r.Name == tt.name }) {
			t.Errorf("no reference to %s", tt.what)
		}
	}
	if !slices.ContainsFunc(x.Edges, func(e Edge) bool { return e.From == "m.1" && e.To == "v.2" && e.Via == "trigger_event" }) {
		t.Error("a mod's event fires the install's")
	}
	if !x.File("bold.dds") || len(x.Roots) != 1 {
		t.Errorf("a mod's binary files are in its inventory: roots %v", x.Roots)
	}

	// the two indexes together are what the game loads: brave is contested, and the mod's wins
	contests := Contests(All{install.Index, x}, s, func(kind string) bool { return false }, nil, []string{game.OriginVanilla, "mod"})
	if len(contests) != 1 || contests[0].Name != "brave" || contests[0].Found[contests[0].Winner].At.Origin != "mod" {
		t.Errorf("contests: %+v", contests)
	}
}

func TestReplacedFolders(t *testing.T) {
	install := fixture(t, "ck3")
	x, _, _ := workspace(t, "ck3")
	both := All{x, install.Index}

	if !slices.Equal(x.Replaced, []string{"common/opinion_modifiers"}) {
		t.Fatalf("a CK3 mod's replace_path lines: %v", x.Replaced)
	}
	if !install.Index.Def("opinion_modifiers", "v_opinion_1") || both.Def("opinion_modifiers", "v_opinion_1") || len(both.Named("v_opinion_1")) != 0 {
		t.Error("the game loads no install file of a folder a mod replaces: its definitions exist in the install and not in the workspace")
	}
	if both.File("common/opinion_modifiers/v.txt") || !both.File("common/traits/v_traits.txt") {
		t.Error("nor is a file of that folder found by its path")
	}
	if !both.Def("traits", "brave") || !both.Def("events", "v.2") {
		t.Error("every other folder of the install is loaded as it was")
	}

	vic3, _, _ := workspace(t, "vic3")
	if !slices.Equal(vic3.Replaced, []string{"common/genes"}) {
		t.Errorf("a Victoria 3 mod's game_custom_data.replace_paths: %v", vic3.Replaced)
	}
}

func TestBuildWorkspaceEntryModes(t *testing.T) {
	install := fixture(t, "vic3")
	x, _, _ := workspace(t, "vic3")
	modes, order := game.Get("vic3").EntryModes, []string{game.OriginVanilla, "mod"}

	for _, tt := range []struct{ name, mode, winner string }{
		{"GBR", "INJECT", game.OriginVanilla},
		{"FRA", "REPLACE", "mod"},
	} {
		found := append(install.Index.Named(tt.name), x.Named(tt.name)...)
		if len(found) != 2 || found[1].Mode != tt.mode {
			t.Fatalf("%s: the word before a name is its entry mode, and no part of the name: %+v", tt.name, found)
		}
		if kept, _ := Winner(found, false, modes, order); kept.At.Origin != tt.winner {
			t.Errorf("%s %s: kept %s", tt.mode, tt.name, kept.At.Origin)
		}
	}
}

func TestIndexFile(t *testing.T) {
	install := fixture(t, "ck3")
	x, s, dir := workspace(t, "ck3")
	file := x.Files[slices.IndexFunc(x.Files, func(f source.File) bool { return strings.HasSuffix(f.Rel, "m_events.txt") })]
	files := len(x.Files)

	edited := "namespace = m\n\nm.1 = {\n\ttype = character_event\n\timmediate = {\n\t\ttrigger_event = v.1\n\t}\n}\nm.3 = {\n\ttype = character_event\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "events", "m_events.txt"), []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	x.IndexFile(file, edited, schema.Reader{Schema: s, Vocab: install.Vocab, Found: All{x, install.Index}})

	if x.Def("events", "m.2") || x.Def("saved_scopes", "mod_target") || !x.Def("events", "m.3") || len(x.Named("m.1")) != 1 || len(x.Files) != files {
		t.Errorf("an edit takes back what the file held and reads it again: m.1 %+v", x.Named("m.1"))
	}
	var fired []string
	for _, e := range x.Edges {
		if e.File >= 0 && e.From == "m.1" {
			fired = append(fired, e.To)
		}
	}
	if !slices.Equal(fired, []string{"v.1"}) {
		t.Errorf("m.1 fires %v after the edit, want v.1 alone", fired)
	}
	if !x.Def("traits", "bold") {
		t.Error("another file's records are untouched")
	}
}

// TestInstallsAreScanned is the gate over the real installs: each game is scanned with the one code path, saved, loaded back, and the loaded install is measured. It checks the facts the plan's §15c leaves to this phase: what a loaded CK3 install holds against Phase 0's 569 MB; that an install's roots load base layers first; that no relative path exists in two of EU5's stage folders; that vanilla writes no entry modes; and that a nested kind's names need no parent to be told apart. It needs the installs and their dumps, so it runs only when asked: set PMT_INDEX_GATE=1 and the three PMT_TEST_INSTALL_ variables, each a game's root folder.
func TestInstallsAreScanned(t *testing.T) {
	if testing.Short() || os.Getenv("PMT_INDEX_GATE") == "" {
		t.Skip("scans the three installs; set PMT_INDEX_GATE=1")
	}
	for _, g := range []struct {
		id, env string
		defs    map[string]string // a definition that must be found, to its kind
		heapMB  uint64            // what the loaded install may hold; 0 where Phase 0 measured nothing
	}{
		{"ck3", "PMT_TEST_INSTALL_CK3", map[string]string{"brave": "traits", "k_france": "landed_titles", "on_death": "on_action"}, 569},
		{"vic3", "PMT_TEST_INSTALL_VIC3", map[string]string{"GBR": "country_definitions", "law_monarchy": "laws"}, 0},
		{"eu5", "PMT_TEST_INSTALL_EU5", map[string]string{"FRA": "countries"}, 0},
	} {
		dir := os.Getenv(g.env)
		if dir == "" {
			t.Errorf("%s: %s is not set", g.id, g.env)
			continue
		}
		user := game.UserDataDir(g.id, dir)
		in, err := Scan(context.Background(), Request{Game: g.id, Dir: dir, Version: "gate", Docs: []string{filepath.Join(user, "logs"), filepath.Join(user, "docs")}})
		if err != nil {
			t.Fatalf("%s: %v", g.id, err)
		}
		x := in.Index
		for name, kind := range g.defs {
			if !x.Def(kind, name) {
				t.Errorf("%s: no %s %s", g.id, kind, name)
			}
		}

		// load order: the engine's base layers before the game, and no rel in two of EU5's stage folders
		layer := func(root string) int {
			rel, _ := filepath.Rel(dir, root)
			return map[string]int{"clausewitz": 0, "jomini": 1}[strings.ToLower(strings.Split(filepath.ToSlash(rel), "/")[0])]
		}
		if !slices.IsSortedFunc(x.Roots[:min(2, len(x.Roots))], func(a, b string) int { return layer(a) - layer(b) }) {
			t.Errorf("%s: the base layers do not load first: %v", g.id, x.Roots)
		}
		// a file's root is its path less its rel; two roots side by side in one folder are one layer's stage folders
		seen, twice, modes, collide := map[string]string{}, 0, 0, 0
		for _, f := range x.Files {
			rel := strings.ToLower(f.Rel)
			root := filepath.Clean(strings.TrimSuffix(filepath.ToSlash(f.Path), f.Rel))
			if first, ok := seen[rel]; ok && first != root && filepath.Dir(first) == filepath.Dir(root) && len(game.Get(g.id).StageRoots) > 0 {
				twice++
			}
			seen[rel] = root
		}
		if twice > 0 {
			t.Errorf("%s: %d rels in two stage folders of one layer, which makes their order matter", g.id, twice)
		}

		// how a word spelled as a file is found: by its path from a root, by its name alone, under another extension
		byPath, byName, byStem := 0, 0, 0
		for _, r := range x.Refs {
			name := strings.ToLower(r.Name)
			base := filepath.Base(name)
			switch _, isRel := x.rels[name]; {
			case r.Type != schema.TypeFile:
			case isRel:
				byPath++
			case len(x.bases[base]) > 0:
				byName++
			default:
				byStem++
			}
		}
		nested := map[[2]string]string{}
		for _, d := range x.Defs {
			if slices.ContainsFunc(game.Get(g.id).EntryModes, func(m game.EntryMode) bool { return m.Word == d.Mode }) {
				modes++
			}
			at := [2]string{d.Kind, d.Name}
			if k := in.Schema.Kinds[d.Kind]; k != nil && k.Parent != "" {
				if parent, ok := nested[at]; ok && parent != d.Parent {
					collide++
				}
				nested[at] = d.Parent
			}
		}

		// what the loaded install holds, measured by the procedure of Phase 0
		base := filepath.Join(t.TempDir(), "vanilla-"+g.id)
		if err := Save(base, "gate", in); err != nil {
			t.Fatal(err)
		}
		defs, refs, edges, locs := len(x.Defs), len(x.Refs), len(x.Edges), len(x.Loc)
		in, x = nil, nil
		var before, after runtime.MemStats
		runtime.GC()
		runtime.GC()
		runtime.ReadMemStats(&before)
		loaded, err := Load(base)
		if err != nil {
			t.Fatal(err)
		}
		loaded.Index.Ready()
		runtime.GC()
		runtime.GC()
		runtime.ReadMemStats(&after)
		held := (after.HeapAlloc - before.HeapAlloc) >> 20
		if len(loaded.Index.Defs) != defs || len(loaded.Index.Refs) != refs || len(loaded.Index.Edges) != edges || len(loaded.Index.Loc) != locs {
			t.Errorf("%s: the cache gave back %d definitions of %d", g.id, len(loaded.Index.Defs), defs)
		}
		if modes > 0 {
			t.Errorf("%s: vanilla writes %d entry modes", g.id, modes)
		}
		if g.heapMB > 0 && held > g.heapMB {
			t.Errorf("%s: the loaded install holds %d MB, more than the %d MB Phase 0 measured", g.id, held, g.heapMB)
		}
		t.Logf("INDEX %s: %d files, %d definitions, %d references, %d edges, %d loc entries; loaded install holds %d MB; files found %d by path, %d by name, %d under another extension; %d rels in two stage folders, %d entry modes in vanilla, %d nested names under two parents", g.id, len(loaded.Index.Files), defs, refs, edges, locs, held, byPath, byName, byStem, twice, modes, collide)
		runtime.KeepAlive(loaded)
	}
}
