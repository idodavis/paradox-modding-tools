// persist_test.go covers the cache / data / loc sidecar round trip, path
// interning, version rejection, and the header peek the install list reads.

package derived

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"paradox-modding-tools/services/internal/lang/declared"
)

func internFixture() *VanillaCache {
	const a = `C:\Games\CK3\game\common\traits\00_traits.txt`
	const b = `C:\Games\CK3\game\events\yearly_events.txt`
	return &VanillaCache{
		FormatVersion: CacheFormatVersion,
		InstallID:     "i", GameID: "ck3", GameVersion: "1.0",
		Defs: []Def{
			{Kind: "traits", Key: "brave", Path: a, Line: 1},
			{Kind: "traits", Key: "craven", Path: a, Line: 9},
			{Kind: "event", Key: "y.1", Path: b, Line: 3},
		},
		LocRefs:  []Ref{{Key: "brave", Kind: "loc", Path: a, Line: 2}},
		CallRefs: []Ref{{Key: "my_effect", Kind: "scripted_effects", Path: b, Line: 4}},
		Edges:    []Edge{{From: "y.1", To: "y.2", Via: "trigger_event", Path: b, Line: 5}},
	}
}

func TestCache(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vanilla-inst-1_0.json")
	in := &VanillaCache{
		FormatVersion:   CacheFormatVersion,
		InstallID:       "inst",
		GameID:          "ck3",
		GameVersion:     "1.0",
		Defs:            []Def{{Kind: "trait", Key: "brave", Path: "a.txt", Line: 3}},
		Vocabulary:      []string{"add_gold"},
		LocRefs:         []Ref{{Key: "k.t", Kind: "loc", Path: "e.txt", Line: 1, Start: 2, End: 5}},
		FieldValueKinds: map[string]string{"theme": "event_theme"},
		StructureBlocks: map[string][]string{"event": {"immediate"}},
	}
	if err := saveCacheFile(path, in); err != nil {
		t.Fatal(err)
	}
	out, err := loadCacheFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Defs) != 1 || out.Defs[0].Key != "brave" || out.InstallID != "inst" {
		t.Errorf("round-trip mismatch: %+v", out)
	}
	if len(out.LocRefs) != 1 || out.LocRefs[0].Key != "k.t" ||
		out.FieldValueKinds["theme"] != "event_theme" {
		t.Errorf("refs/kinds: refs=%+v kinds=%v", out.LocRefs, out.FieldValueKinds)
	}
	if len(out.StructureBlocks["event"]) != 1 || out.StructureBlocks["event"][0] != "immediate" {
		t.Errorf("structureBlocks: %v", out.StructureBlocks)
	}

	// Paths must survive the round trip exactly: they are what go-to-definition navigates to, and the install path must be written once rather than once per record.
	t.Run("path interning", func(t *testing.T) {
		src := internFixture()
		p := filepath.Join(t.TempDir(), "vanilla-test.json")
		want := src.Defs[0].Path
		if err := saveCacheFile(p, src); err != nil {
			t.Fatal(err)
		}
		// Saving must not disturb the in-memory cache: a live session may hold it while the scan writes.
		if src.Defs[0].Path != want || len(src.Paths) != 0 {
			t.Errorf("source mutated: path %q, table %v", src.Defs[0].Path, src.Paths)
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if n := strings.Count(string(raw), `common\\traits\\00_traits.txt`); n != 1 {
			t.Errorf("traits path written %d times, want 1", n)
		}
		got, err := loadCacheFile(p)
		if err != nil {
			t.Fatal(err)
		}
		for i, d := range src.Defs {
			if got.Defs[i].Path != d.Path || got.Defs[i].Key != d.Key || got.Defs[i].Line != d.Line {
				t.Errorf("def %d = %+v, want %+v", i, got.Defs[i], d)
			}
		}
		if got.LocRefs[0].Path != src.LocRefs[0].Path ||
			got.CallRefs[0].Path != src.CallRefs[0].Path ||
			got.Edges[0].Path != src.Edges[0].Path {
			t.Errorf("ref/edge paths: %+v %+v %+v", got.LocRefs[0], got.CallRefs[0], got.Edges[0])
		}
		if len(got.Paths) != 0 {
			t.Errorf("Paths table leaked into the loaded cache: %v", got.Paths)
		}
	})

	for _, tt := range []struct{ name, body string }{
		{"wrong version", `{"formatVersion":999,"gameId":"ck3"}`},
		{"missing version", `{"gameId":"ck3"}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "stale.json")
			if err := os.WriteFile(p, []byte(tt.body), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := loadCacheFile(p); err == nil {
				t.Fatal("expected error")
			}
		})
	}

	prep := &VanillaCache{
		Schema: &declared.Schema{
			Effects:  map[string]declared.EngineToken{"add_gold": {}},
			Triggers: map[string]declared.EngineToken{"is_adult": {}},
		},
		Structures:      map[string][]string{"event": {"immediate"}},
		FieldInfo:       map[string]string{"Type": "global"},
		FieldInfoByKind: map[string]map[string]string{"event": {"Title": "kind"}},
	}
	PrepareCache(prep)
	if !prep.effectSet["add_gold"] || !prep.triggerSet["is_adult"] ||
		!prep.structureSets["event"]["immediate"] {
		t.Fatalf("sets not built")
	}
	if prep.FieldInfo["type"] != "global" || prep.FieldInfoByKind["event"]["title"] != "kind" {
		t.Fatalf("docs not normalized: %+v %+v", prep.FieldInfo, prep.FieldInfoByKind)
	}

	p, err := vanillaJSON("vanilla", "inst-1", "1.19.0")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(p) != "vanilla-inst-1-1_19_0.json" {
		t.Fatalf("cache path = %s", p)
	}

	locIn := &VanillaLoc{
		FormatVersion: LocFormatVersion,
		Sites:         map[string]LocEntry{"k": {Path: "a.yml", Line: 1, Value: "v"}},
	}
	locPath := filepath.Join(t.TempDir(), "sidecar.json")
	if err := SaveJSON(locPath, locIn); err != nil {
		t.Fatal(err)
	}
	gotLoc, err := LoadJSON[VanillaLoc](locPath, LocFormatVersion)
	if err != nil {
		t.Fatal(err)
	}
	if gotLoc.Sites["k"].Value != "v" || gotLoc.Sites["k"].Line != 1 {
		t.Fatalf("loc sidecar = %+v", gotLoc)
	}

	staleLoc := filepath.Join(t.TempDir(), "loc-v2.json")
	if err := os.WriteFile(staleLoc, []byte(`{"formatVersion":2,"sites":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// errors.Is, not the message: store.go decides whether to start empty from this sentinel, and a string equality here would pass while that broke.
	_, err = LoadJSON[VanillaLoc](staleLoc, LocFormatVersion)
	if !errors.Is(err, ErrFormat) {
		t.Fatalf("mismatch error = %v", err)
	}
	if !strings.Contains(err.Error(), fmt.Sprintf("2 != %d", LocFormatVersion)) {
		t.Fatalf("mismatch error must name both versions: %v", err)
	}

	// The header read is bounded, so it sees a truncated document: sonic must still answer from it, and formatVersion and scannedAt must stay leading fields of every struct LoadJSON reads. Reordering them, or pushing them past peekWindow, breaks the install list and every version check without breaking a round trip.
	t.Run("peek reads only the head of a large file", func(t *testing.T) {
		big := &VanillaCache{
			FormatVersion: CacheFormatVersion, InstallID: "big", GameID: "ck3",
			GameVersion: "1.0", ScannedAt: "2026-09-09T00:00:00Z",
		}
		for i := range 4000 {
			big.Defs = append(big.Defs, Def{
				Kind: "traits", Key: "trait_" + strconv.Itoa(i),
				Path: `C:\Games\CK3\game\common\traits\00_traits.txt`, Line: i,
			})
		}
		p := filepath.Join(t.TempDir(), "big.json")
		if err := saveCacheFile(p, big); err != nil {
			t.Fatal(err)
		}
		st, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if st.Size() <= peekWindow {
			t.Fatalf("fixture is %d bytes, not past the %d-byte window: it no longer exercises truncation", st.Size(), peekWindow)
		}
		got, at, err := peekHeader(p)
		if err != nil {
			t.Fatalf("peekHeader on a %d-byte file: %v", st.Size(), err)
		}
		if got != CacheFormatVersion || at != big.ScannedAt {
			t.Errorf("peekHeader = (%d, %q), want (%d, %q)", got, at, CacheFormatVersion, big.ScannedAt)
		}
	})

	// A cache the engine will refuse must not be reported as a scan that happened: PeekCacheScannedAt feeds the install list's "last scanned" date and used to answer from the file's existence alone, so after a format bump the UI showed a recent scan for a model LoadCache had already discarded.
	t.Run("peek refuses a wrong format", func(t *testing.T) {
		dir := t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", dir)
		t.Setenv("AppData", dir)
		const id, ver = "peek-test", "1.0"
		c := &VanillaCache{
			FormatVersion: CacheFormatVersion, InstallID: id, GameID: "ck3",
			GameVersion: ver, ScannedAt: "2026-09-09T00:00:00Z",
		}
		// SaveCache also drops leftover legacy cache-*.json beside the new file.
		cacheDir, err := CacheDir()
		if err != nil {
			t.Fatal(err)
		}
		legacy := filepath.Join(cacheDir, "cache-ws-a.json")
		if err := os.WriteFile(legacy, []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := SaveCache(c); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(legacy); !errors.Is(err, os.ErrNotExist) {
			t.Error("legacy cache-*.json should be removed")
		}
		if got := PeekCacheScannedAt(id, ver); got != c.ScannedAt {
			t.Fatalf("current format: got %q, want %q", got, c.ScannedAt)
		}
		path, err := vanillaJSON("vanilla", id, ver)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		old := strings.Replace(string(raw),
			`"formatVersion":`+strconv.Itoa(CacheFormatVersion),
			`"formatVersion":`+strconv.Itoa(CacheFormatVersion-1), 1)
		if old == string(raw) {
			t.Fatal("could not rewrite the format version; the field's spelling changed")
		}
		if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadCache(id, ver); err == nil {
			t.Fatal("LoadCache accepted the old format; this test's premise is gone")
		}
		if got := PeekCacheScannedAt(id, ver); got != "" {
			t.Errorf("reported %q for a cache LoadCache refuses", got)
		}
		if err := DropVanillaFiles(id); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("cache file survived DropVanillaFiles: %v", err)
		}
	})
}
