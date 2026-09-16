// data_test.go covers the data-row tier: the split at scan time, the sidecar
// file it is persisted in, the numeric ids only a data row answers to, and the
// lazy load that keeps 71,000 CK3 characters out of a session never asked for one.

package derived

import (
	"os"
	"testing"

	"paradox-modding-tools/services/internal/lang/jomini"
)

// dataInstall is ck3Install plus real history: three characters, a province reusing a character's id, and titles naming a holder by number.
func dataInstall(t *testing.T) string {
	t.Helper()
	base, _ := ck3Install(t)
	writeMod(t, base, "game/history/characters/x.txt", `100 = { name = "A" }
101 = { name = "B" }
102 = { name = "C" }
`)
	writeMod(t, base, "game/history/provinces/p.txt", `100 = { holding = castle_holding }
`)
	writeMod(t, base, "game/history/titles/t.txt", `k_a = { 1000.1.1 = { holder = 100 } }
k_b = { 1000.1.1 = { holder = 101 } }
k_c = { 1000.1.1 = { holder = 102 } }
`)
	return base
}

func TestDataTier(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("AppData", dir)
	base := dataInstall(t)
	c, vloc := scanCK3(t, base)

	// The split: a data kind leaves the script cache, a script kind stays.
	for _, tc := range []struct {
		kind, key string
		script    bool
	}{
		{"characters", "100", false},
		{"provinces", "100", false},
		{"titles", "k_a", false},
		{"traits", "brave", true},
		{"event", "test.1", true},
	} {
		if got := findDef(c.Defs, tc.kind, tc.key); got != tc.script {
			t.Errorf("cache.Defs has %s %s = %v, want %v", tc.kind, tc.key, got, tc.script)
		}
	}

	// `holder = 100` needs both halves: the field is typed only once an id naming two kinds stops vetoing the vote.
	if got := c.FieldValueKinds["holder"]; got != "characters" {
		t.Errorf("FieldValueKinds[holder] = %q, want characters", got)
	}
	src := "k_a = { 1000.1.1 = { holder = 100 } }\n"
	ex := ExtractParsed("ck3", "t.txt", "game/history/titles/t.txt", "", jomini.Parse(src), c)
	cited := false
	for _, r := range ex.Refs {
		cited = cited || (r.Key == "100" && r.Kind == "characters")
	}
	if !cited {
		t.Errorf("holder = 100 cited no character; refs=%+v", ex.Refs)
	}

	// Persisted beside the script cache, and dropped with it.
	if err := SaveCache(c); err != nil {
		t.Fatal(err)
	}
	tier, err := LoadData("inst", "1.0")
	if err != nil {
		t.Fatalf("LoadData: %v", err)
	}
	if !findDef(tier, "characters", "100") || !findDef(tier, "titles", "k_a") {
		t.Errorf("tier file missing data rows: %d defs", len(tier))
	}

	// Lazy: a model built from the loaded cache holds no data rows until a key the script maps cannot place asks.
	loaded, err := LoadCache("inst", "1.0")
	if err != nil {
		t.Fatal(err)
	}
	m := NewModel("ck3", "english", loaded, vloc, nil)
	if m.dataByKey != nil {
		t.Errorf("data tier indexed before anything asked for it")
	}
	if d := m.Resolve("brave"); d == nil || m.dataByKey != nil {
		t.Errorf("resolving a script key loaded the data tier")
	}
	d := m.Resolve("102")
	if d == nil || d.Kind != "characters" {
		t.Fatalf("Resolve(102) = %+v, want a characters def", d)
	}
	if m.dataByKey == nil {
		t.Errorf("data tier not indexed after a data-row lookup")
	}
	if got := m.FindDefsOf("k_", "titles", 10, true, true); len(got) != 3 {
		t.Errorf("FindDefsOf(titles) = %d defs, want 3", len(got))
	}
	// Sorted tier keys, so a limit cuts the same way every time.
	first := m.FindDefsOf("1", "characters", 1, true, true)
	again := m.FindDefsOf("1", "characters", 1, true, true)
	if len(first) != 1 || len(again) != 1 || first[0] != again[0] {
		t.Errorf("FindDefsOf over the tier is not stable: %+v then %+v", first, again)
	}

	path, err := vanillaJSON("vanilla-data", "inst", "1.0")
	if err != nil {
		t.Fatal(err)
	}
	if err := DropVanillaFiles("inst"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("tier file survived DropVanillaFiles: %v", err)
	}

	// A tier that will not load must say so, not answer "no such character".
	gone := NewModel("ck3", "english", loaded, vloc, nil)
	if gone.DataError() != "" {
		t.Errorf("DataError set before any load: %q", gone.DataError())
	}
	if d := gone.Resolve("102"); d != nil {
		t.Errorf("Resolve(102) = %+v with no tier file", d)
	}
	if gone.DataError() == "" {
		t.Errorf("a missing tier file loaded silently")
	}
}
