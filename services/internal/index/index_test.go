package index

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"paradox-modding-tools/services/internal/appdata"
	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/inventory"
	"paradox-modding-tools/services/internal/schema"
)

// the strings the tests name more than twice
const (
	ck3         = "ck3"
	testVersion = "test"
	trigger     = "trigger"
	events      = "events"
	vic3        = "vic3"
	inline      = "v_inline_trigger"
)

var scans sync.Map

// emptyHome points every home and config variable at a temp dir, so no dump folder, wiki cache or
// cache file of this machine is read or written.
func emptyHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	for _, name := range []string{"HOME", "USERPROFILE", "APPDATA", "LOCALAPPDATA", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME"} {
		t.Setenv(name, home)
	}
}

// fixtureInstall scans a fixture install as the goldens' generator does: an emptied home, so no
// dump folder or wiki cache of this machine is read, and no cache written (App ""). The scan is
// shared across tests.
func fixtureInstall(t *testing.T, id string) *Install {
	t.Helper()
	emptyHome(t)
	scan, _ := scans.LoadOrStore(id, sync.OnceValues(func() (*Install, error) {
		return Scan(t.Context(), Request{Game: id, Dir: filepath.Join("..", "testdata", id), Version: testVersion})
	}))
	install, err := scan.(func() (*Install, error))()
	if err != nil {
		t.Fatal(err)
	}
	return install
}

// fixtureMods indexes the fixture mod of a game over its install.
func fixtureMods(t *testing.T, id string, install *Install) *Index {
	t.Helper()
	mods, err := ScanMods(t.Context(), install, []Mod{{ID: "mod", Dir: filepath.Join("..", "testdata", "mods", id)}})
	if err != nil {
		t.Fatal(err)
	}
	return mods
}

// dumpRecords writes a file's occurrences as the goldens print them: by role, each role in the file's order.
func dumpRecords(found []At) string {
	var out strings.Builder
	for _, role := range []schema.Role{schema.Defines, schema.Refers, schema.Localizes} {
		for _, at := range found {
			o := at.Occurrence()
			switch {
			case o.Role != role:
			case role == schema.Defines:
				fmt.Fprintf(&out, "def %s %q mode=%q %d-%d @%d:%d local=%t named=%t\n",
					o.Database, o.ID, o.Mode, o.Range.Start, o.Range.End, o.Line, o.Col, o.Local, o.Named)
			case role == schema.Refers:
				fmt.Fprintf(&out, "ref %s %q called=%q %d-%d @%d:%d from=%d trigger=%t\n",
					o.Database, o.ID, o.Caller, o.Range.Start, o.Range.End, o.Line, o.Col, o.Entry, o.InTrigger)
			default:
				fmt.Fprintf(&out, "loc %s %q lang=%s line=%d\n", o.ID, at.Text(), at.Language(), o.Line)
			}
		}
	}
	return out.String()
}

// firstDifference shows the first line that differs and its line number.
func firstDifference(want, got string) string {
	wanted, gotten := strings.Split(want, "\n"), strings.Split(got, "\n")
	for i := range max(len(wanted), len(gotten)) {
		line := func(lines []string) string {
			if i < len(lines) {
				return lines[i]
			}
			return "(end)"
		}
		if line(wanted) != line(gotten) {
			return fmt.Sprintf("line %d\nwant: %s\ngot:  %s", i+1, line(wanted), line(gotten))
		}
	}
	return "(none)"
}

func TestGoldenRecords(t *testing.T) {
	golden := filepath.Join("..", "testdata", "golden")
	visited := map[string]bool{}
	for _, id := range []string{ck3, vic3, "eu5"} {
		install := fixtureInstall(t, id)
		for _, index := range []*Index{install.Index, fixtureMods(t, id, install)} {
			for _, file := range index.Files {
				found := index.InFile(file.Path)
				if dumpRecords(found) == "" {
					continue
				}
				origin := "vanilla"
				if file.Origin != game.OriginVanilla {
					origin = "mod"
				}
				flat := strings.ReplaceAll(file.Rel, "/", "__")
				name := fmt.Sprintf("records-%s-%s-%s.txt", id, origin, strings.TrimSuffix(flat, filepath.Ext(flat)))
				visited[name] = true
				want, err := os.ReadFile(filepath.Join(golden, name))
				if err != nil {
					t.Errorf("%s: %v", name, err)
				} else if got := dumpRecords(found); got != string(want) {
					t.Errorf("%s differs from the golden:\n%s", name, firstDifference(string(want), got))
				}
			}
		}
	}
	goldens, _ := filepath.Glob(filepath.Join(golden, "records-*.txt"))
	for _, path := range goldens {
		if !visited[filepath.Base(path)] {
			t.Errorf("%s: no file of the fixtures has these records", filepath.Base(path))
		}
	}
}

func TestResolve(t *testing.T) {
	install := fixtureInstall(t, ck3)
	mods := fixtureMods(t, ck3, install)
	indexes := []*Index{install.Index, mods}
	pathOf := func(rel string) string {
		file, _ := FileAt(indexes, rel)
		return file.Path
	}
	cases := []struct {
		about string
		ref   schema.Occurrence
		from  string
		defs  int
		known bool
	}{
		{"a scope the dumps declare", schema.Occurrence{Database: game.SavedScopes, ID: "target"}, "", 0, true},
		{"a scope an event saves", schema.Occurrence{Database: game.SavedScopes, ID: "own_dynasty"}, "", 1, true},
		{"a scripted effect", schema.Occurrence{Caller: "effect", ID: "v_effect"}, "", 1, true},
		{"a local trigger from another file",
			schema.Occurrence{Caller: trigger, ID: inline}, pathOf("common/scripted_triggers/v_triggers.txt"), 0, false},
		{"a local trigger from its own file",
			schema.Occurrence{Caller: trigger, ID: inline}, pathOf("events/v_more_events.txt"), 1, true},
		{"a loc key", schema.Occurrence{Database: schema.Localization, ID: "v.1.t"}, "", 0, true},
		{"a file", schema.Occurrence{Database: schema.File, ID: "gfx/interface/icons/traits/brave.dds"}, "", 0, true},
		{"a trait vanilla and the mod define", schema.Occurrence{Database: "traits", ID: "brave"}, "", 2, true},
	}
	for _, c := range cases {
		defs, known := Resolve(install.Schema, indexes, c.ref, c.from)
		if len(defs) != c.defs || known != c.known {
			t.Errorf("%s: %d defs, known %t; want %d, %t", c.about, len(defs), known, c.defs, c.known)
		}
	}

	if defs, _ := Resolve(install.Schema, indexes, schema.Occurrence{Database: game.SavedScopes, ID: "own_dynasty"}, ""); len(defs) == 1 &&
		defs[0].File().Path != pathOf("events/v_events.txt") {
		t.Errorf("own_dynasty is defined in %s, want events/v_events.txt", defs[0].File().Path)
	}
	if defs, _ := Resolve(install.Schema, indexes, schema.Occurrence{Caller: "effect", ID: "v_effect"}, ""); len(defs) == 1 &&
		defs[0].Occurrence().Database != "scripted_effects" {
		t.Errorf("v_effect is in %s, want scripted_effects", defs[0].Occurrence().Database)
	}
	local, _ := Resolve(install.Schema, indexes, schema.Occurrence{Caller: trigger, ID: inline}, pathOf("events/v_more_events.txt"))
	if len(local) == 1 && !local[0].Occurrence().Local {
		t.Errorf("v_inline_trigger is not local")
	}
	brave, _ := Resolve(install.Schema, indexes, schema.Occurrence{Database: "traits", ID: "brave"}, "")
	if len(brave) == 2 {
		// load order is by Rel: the mod's m_traits.txt sorts before vanilla's v_traits.txt
		_, first := mods.At(brave[0].File().Path)
		_, second := install.Index.At(brave[1].File().Path)
		if !first || !second {
			t.Errorf("brave's defs are not the mod's then vanilla's: %s, %s", brave[0].File().Path, brave[1].File().Path)
		}
	}
}

func TestWinner(t *testing.T) {
	type pair struct {
		install *Install
		mods    *Index
	}
	pairs := map[string]pair{}
	for _, id := range []string{ck3, vic3, "eu5"} {
		install := fixtureInstall(t, id)
		pairs[id] = pair{install, fixtureMods(t, id, install)}
	}
	winner := func(id string, defs []At) (At, []At, []At) {
		mods := pairs[id].mods
		origin := func(path string) string {
			if file, listed := mods.At(path); listed {
				return file.Origin
			}
			return game.OriginVanilla
		}
		won, adds, orphans, _ := Winner(game.Get(id), defs, origin)
		return won, adds, orphans
	}
	lookup := func(id, database, name string) []At {
		return Lookup([]*Index{pairs[id].install.Index, pairs[id].mods}, schema.Defines, database, name)
	}
	fromMod := func(id string, def At) bool {
		_, listed := pairs[id].mods.At(def.File().Path)
		return listed
	}
	mode := func(def At) string { return def.Occurrence().Mode }
	path := func(def At) string {
		if def == (At{}) {
			return ""
		}
		return def.File().Path
	}

	won, adds, orphans := winner("eu5", lookup("eu5", "town_rights", "aga_tr"))
	if mode(won) != "REPLACE" || !strings.HasSuffix(filepath.ToSlash(path(won)), "m_rights.txt") || len(adds) != 0 || len(orphans) != 0 {
		t.Errorf("eu5 aga_tr: winner %s %s, %d adds, %d orphans; want the mod's REPLACE, none, none",
			mode(won), path(won), len(adds), len(orphans))
	}
	if won, _, orphans := winner("eu5", lookup("eu5", "town_rights", "missing_tr")); won != (At{}) || len(orphans) != 0 {
		t.Errorf("eu5 missing_tr: winner %q, %d orphans; want none: TRY_ does nothing where there is none", path(won), len(orphans))
	}
	won, adds, _ = winner(vic3, lookup(vic3, "country_definitions", "GBR"))
	if fromMod(vic3, won) || len(adds) != 1 || mode(adds[0]) != "INJECT" || !fromMod(vic3, adds[0]) {
		t.Errorf("vic3 GBR: winner %s, adds %v; want vanilla's, the mod's INJECT", path(won), adds)
	}
	won, adds, _ = winner(vic3, lookup(vic3, "country_definitions", "FRA"))
	if mode(won) != "REPLACE" || !fromMod(vic3, won) || len(adds) != 0 {
		t.Errorf("vic3 FRA: winner %s %s, %d adds; want the mod's REPLACE, none", mode(won), path(won), len(adds))
	}
	won, _, _ = winner(ck3, lookup(ck3, "traits", "brave"))
	if mode(won) != "" || fromMod(ck3, won) {
		t.Errorf("ck3 brave: winner %s %s; want vanilla's plain one, the last by Rel", mode(won), path(won))
	}
}

func TestWinnerByHand(t *testing.T) {
	// definitions made by hand, one per file a and b of one index, no mod among them
	made := func(found ...schema.Occurrence) []At {
		index := &Index{Files: []inventory.File{{Path: "a", Rel: "a"}, {Path: "b", Rel: "b"}}}
		index.Rebuild()
		for i, one := range found {
			index.add(index.Files[i].Path, collected{found: []schema.Occurrence{one}})
		}
		return Lookup([]*Index{index}, schema.Defines, events, "x")
	}
	vanilla := func(string) string { return game.OriginVanilla }
	plain := schema.Occurrence{Role: schema.Defines, Database: events, ID: "x"}
	if won, _, _, _ := Winner(game.Get(vic3), made(plain, plain), vanilla); won.File().Path != "a" {
		t.Errorf("vic3 events: winner %q; want the first, events is first-wins", won.File().Path)
	}
	injected := schema.Occurrence{Role: schema.Defines, Database: events, ID: "x", Mode: "INJECT"}
	if won, _, orphans, _ := Winner(game.Get("eu5"), made(injected), vanilla); won != (At{}) || len(orphans) != 1 {
		t.Errorf("eu5 a lone INJECT: winner %q, %d orphans; want none, one", won.File().Path, len(orphans))
	}
}

func TestLoaded(t *testing.T) {
	cases := []struct {
		id, rel string
		loaded  bool
	}{
		{ck3, "common/opinion_modifiers/v.txt", false}, // the descriptor's replace_path
		{ck3, "common/traits/v_traits.txt", true},
		{vic3, "common/genes/v_genes.txt", false}, // metadata.json's replace_paths
	}
	for _, c := range cases {
		install := fixtureInstall(t, c.id)
		file, _ := FileAt([]*Index{install.Index}, c.rel)
		if _, loaded := Loaded([]*Index{install.Index, fixtureMods(t, c.id, install)}, file.Path); loaded != c.loaded {
			t.Errorf("%s %s with the mod: loaded %t, want %t", c.id, c.rel, loaded, c.loaded)
		}
		if _, loaded := Loaded([]*Index{install.Index}, file.Path); !loaded {
			t.Errorf("%s %s alone: not loaded", c.id, c.rel)
		}
	}
}

func TestWritesInScope(t *testing.T) {
	install := fixtureInstall(t, ck3)
	if !install.Index.WritesInScope("add_gold", "character") {
		t.Errorf("add_gold in character: false, want true (on_birth's effect)")
	}
	if install.Index.WritesInScope("add_gold", "province") {
		t.Errorf("add_gold in province: true, want false")
	}
}

func TestCacheRoundTrip(t *testing.T) {
	emptyHome(t)
	req := Request{Game: ck3, Dir: filepath.Join("..", "testdata", ck3), Version: testVersion, App: testVersion}
	scanned, err := Scan(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	path, _, err := cachePath(req)
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err != nil {
		t.Fatalf("no cache file: %v", err)
	} else {
		t.Logf("cache file: %d bytes", info.Size())
	}

	loaded, err := LoadInstall(req)
	if err != nil {
		t.Fatal(err)
	}
	eventV1 := func(install *Install) (found []schema.Occurrence) {
		for _, at := range Lookup([]*Index{install.Index}, schema.Defines, events, "v.1") {
			found = append(found, at.Occurrence())
		}
		return found
	}
	count := func(install *Install) (occurrences int) {
		for _, found := range install.Index.Occurrences {
			occurrences += len(found)
		}
		return occurrences
	}
	if len(loaded.Index.Files) != len(scanned.Index.Files) || count(loaded) != count(scanned) ||
		fmt.Sprint(eventV1(loaded)) != fmt.Sprint(eventV1(scanned)) {
		t.Errorf("loaded: %d files, %d occurrences, v.1 %v; scanned: %d, %d, %v", len(loaded.Index.Files),
			count(loaded), eventV1(loaded), len(scanned.Index.Files), count(scanned), eventV1(scanned))
	}

	if err := DropInstall(req); err != nil {
		t.Fatal(err)
	}
	dir, err := appdata.CacheDir()
	if err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "install-") {
			t.Errorf("DropInstall left %s", entry.Name())
		}
	}
}
