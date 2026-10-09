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

// dumpRecords writes a file's records as the goldens print them.
func dumpRecords(records *Records) string {
	var out strings.Builder
	for _, def := range records.Defs {
		fmt.Fprintf(&out, "def %s %q mode=%q %d-%d @%d:%d local=%t named=%t\n",
			def.Database, def.Name, def.Mode, def.Range.Start, def.Range.End, def.Line, def.Col, def.Local, def.Named)
	}
	for _, ref := range records.Refs {
		fmt.Fprintf(&out, "ref %s %q called=%q %d-%d @%d:%d from=%d trigger=%t\n",
			ref.Database, ref.Name, ref.Called, ref.Range.Start, ref.Range.End, ref.Line, ref.Col, ref.From, ref.InTrigger)
	}
	for _, entry := range records.Loc {
		fmt.Fprintf(&out, "loc %s %q lang=%s line=%d\n", entry.Key, entry.Text, entry.Language, entry.Line)
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
		for _, idx := range []*Index{install.Index, fixtureMods(t, id, install)} {
			for _, file := range idx.Files {
				records := idx.Records[file.Path]
				if records == nil || dumpRecords(records) == "" {
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
				} else if got := dumpRecords(records); got != string(want) {
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
		ref   Ref
		defs  int
		known bool
	}{
		{"a scope the dumps declare", Ref{Database: game.SavedScopes, Name: "target"}, 0, true},
		{"a scope an event saves", Ref{Database: game.SavedScopes, Name: "own_dynasty"}, 1, true},
		{"a scripted effect", Ref{Called: "effect", Name: "v_effect"}, 1, true},
		{"a local trigger from another file",
			Ref{Called: trigger, Name: inline, Path: pathOf("common/scripted_triggers/v_triggers.txt")}, 0, false},
		{"a local trigger from its own file",
			Ref{Called: trigger, Name: inline, Path: pathOf("events/v_more_events.txt")}, 1, true},
		{"a loc key", Ref{Database: schema.Localization, Name: "v.1.t"}, 0, true},
		{"a file", Ref{Database: schema.File, Name: "gfx/interface/icons/traits/brave.dds"}, 0, true},
		{"a trait vanilla and the mod define", Ref{Database: "traits", Name: "brave"}, 2, true},
	}
	for _, c := range cases {
		defs, known := Resolve(install.Schema, indexes, c.ref)
		if len(defs) != c.defs || known != c.known {
			t.Errorf("%s: %d defs, known %t; want %d, %t", c.about, len(defs), known, c.defs, c.known)
		}
	}

	if defs, _ := Resolve(install.Schema, indexes, Ref{Database: game.SavedScopes, Name: "own_dynasty"}); len(defs) == 1 &&
		defs[0].Path != pathOf("events/v_events.txt") {
		t.Errorf("own_dynasty is defined in %s, want events/v_events.txt", defs[0].Path)
	}
	if defs, _ := Resolve(install.Schema, indexes, Ref{Called: "effect", Name: "v_effect"}); len(defs) == 1 &&
		defs[0].Database != "scripted_effects" {
		t.Errorf("v_effect is in %s, want scripted_effects", defs[0].Database)
	}
	local, _ := Resolve(install.Schema, indexes,
		Ref{Called: trigger, Name: inline, Path: pathOf("events/v_more_events.txt")})
	if len(local) == 1 && !local[0].Local {
		t.Errorf("v_inline_trigger is not local")
	}
	brave, _ := Resolve(install.Schema, indexes, Ref{Database: "traits", Name: "brave"})
	if len(brave) == 2 {
		// load order is by Rel: the mod's m_traits.txt sorts before vanilla's v_traits.txt
		_, first := mods.At(brave[0].Path)
		_, second := install.Index.At(brave[1].Path)
		if !first || !second {
			t.Errorf("brave's defs are not the mod's then vanilla's: %s, %s", brave[0].Path, brave[1].Path)
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
	winner := func(id string, defs []Def) (Def, []Def, []Def) {
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
	lookup := func(id, database, name string) []Def {
		return Lookup([]*Index{pairs[id].install.Index, pairs[id].mods}, database, name)
	}
	fromMod := func(id string, def Def) bool {
		_, listed := pairs[id].mods.At(def.Path)
		return listed
	}

	won, adds, orphans := winner("eu5", lookup("eu5", "town_rights", "aga_tr"))
	if won.Mode != "REPLACE" || !strings.HasSuffix(filepath.ToSlash(won.Path), "m_rights.txt") || len(adds) != 0 || len(orphans) != 0 {
		t.Errorf("eu5 aga_tr: winner %s %s, %d adds, %d orphans; want the mod's REPLACE, none, none",
			won.Mode, won.Path, len(adds), len(orphans))
	}
	if won, _, orphans := winner("eu5", lookup("eu5", "town_rights", "missing_tr")); won.Path != "" || len(orphans) != 0 {
		t.Errorf("eu5 missing_tr: winner %q, %d orphans; want none: TRY_ does nothing where there is none", won.Path, len(orphans))
	}
	won, adds, _ = winner(vic3, lookup(vic3, "country_definitions", "GBR"))
	if fromMod(vic3, won) || len(adds) != 1 || adds[0].Mode != "INJECT" || !fromMod(vic3, adds[0]) {
		t.Errorf("vic3 GBR: winner %s, adds %v; want vanilla's, the mod's INJECT", won.Path, adds)
	}
	won, adds, _ = winner(vic3, lookup(vic3, "country_definitions", "FRA"))
	if won.Mode != "REPLACE" || !fromMod(vic3, won) || len(adds) != 0 {
		t.Errorf("vic3 FRA: winner %s %s, %d adds; want the mod's REPLACE, none", won.Mode, won.Path, len(adds))
	}
	won, _, _ = winner(ck3, lookup(ck3, "traits", "brave"))
	if won.Mode != "" || fromMod(ck3, won) {
		t.Errorf("ck3 brave: winner %s %s; want vanilla's plain one, the last by Rel", won.Mode, won.Path)
	}
	won, _, _ = winner(vic3, []Def{{Database: events, Name: "x", Path: "a"}, {Database: events, Name: "x", Path: "b"}})
	if won.Path != "a" {
		t.Errorf("vic3 events: winner %q; want the first, events is first-wins", won.Path)
	}
	won, _, orphans = winner("eu5", []Def{{Database: events, Name: "x", Mode: "INJECT", Path: "a"}})
	if won.Path != "" || len(orphans) != 1 {
		t.Errorf("eu5 a lone INJECT: winner %q, %d orphans; want none, one", won.Path, len(orphans))
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
	eventV1 := func(install *Install) string { return fmt.Sprint(Lookup([]*Index{install.Index}, events, "v.1")) }
	if len(loaded.Index.Files) != len(scanned.Index.Files) || len(loaded.Index.Records) != len(scanned.Index.Records) ||
		eventV1(loaded) != eventV1(scanned) {
		t.Errorf("loaded: %d files, %d records, v.1 %s; scanned: %d, %d, %s", len(loaded.Index.Files),
			len(loaded.Index.Records), eventV1(loaded), len(scanned.Index.Files), len(scanned.Index.Records), eventV1(scanned))
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
