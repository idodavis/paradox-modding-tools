// persist_test.go covers the index round trip, the version refusal and the
// header peek the install list reads without decoding the body.

package derived

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestSaveAndLoadIndex(t *testing.T) {
	const a = `C:\Games\CK3\game\common\traits\00_traits.txt`
	x := NewIndex("ck3")
	x.InstallID, x.GameVersion, x.ScannedAt = "inst", "1.0", "2026-09-16T00:00:00Z"
	x.Schema.at("event", "", "theme").add([]string{LabelDef + "event_theme"}, 3)
	x.Schema.at("event", "", "theme").settle()
	x.Schema.at("event", "", "immediate").add([]string{LabelBlock}, 2)
	x.Schema.at("event", "", "immediate").settle()
	x.Add(a, `common\traits\00_traits.txt`, "", "script", &FileHarvest{
		Defs: []Def{{Kind: "traits", Key: "brave", Line: 1}},
		Refs: []Ref{{Key: "brave", Kind: "loc", Line: 2}},
		Loc:  LocDelta{Lang: "english", Vals: map[string]LocEntry{"k": {Value: "v", Line: 1}}},
	})
	x.SortLocKeys()

	path := filepath.Join(t.TempDir(), "vanilla-inst-1_0.json")
	if err := SaveJSON(path, x); err != nil {
		t.Fatal(err)
	}
	// The install path is written once in the table, not once per record.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(raw), `C:\\Games\\CK3\\game\\common\\traits\\00_traits.txt`); n != 1 {
		t.Errorf("traits path written %d times, want 1", n)
	}
	out, err := loadIndex(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Defs) != 1 || out.Defs[0].Key != "brave" || out.InstallID != "inst" {
		t.Fatalf("round trip: %+v", out.Defs)
	}
	if got := out.defsAt(out.DefsByKey["brave"]); len(got) != 1 || got[0].Path != a {
		t.Fatalf("posting list / path: %+v", got)
	}
	if k := out.Schema.TypeOf(Cell{"event", "", "theme"}).Kinds; len(k) != 1 || k[0] != "event_theme" {
		t.Fatalf("cell kinds: %v", k)
	}
	if !out.Schema.TypeOf(Cell{"event", "", "immediate"}).IsBlock() {
		t.Fatalf("immediate is not a block cell")
	}
	if e, ok := out.locAt("english", "k"); !ok || e.Value != "v" || e.Path != a {
		t.Fatalf("loc entry = %+v ok=%v", e, ok)
	}
	if len(out.LocKeysSorted["english"]) != 1 {
		t.Fatalf("sorted loc keys = %v", out.LocKeysSorted)
	}

	for _, tt := range []struct{ name, body string }{
		{"wrong version", `{"formatVersion":999,"gameId":"ck3"}`},
		{"missing version", `{"gameId":"ck3"}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "stale.json")
			if err := os.WriteFile(p, []byte(tt.body), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := loadIndex(p); err == nil {
				t.Fatal("expected an error")
			}
		})
	}

	p, err := vanillaJSON("vanilla", "inst-1", "1.19.0")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(p) != "vanilla-inst-1-1_19_0.json" {
		t.Fatalf("cache path = %s", p)
	}

	staleLoc := filepath.Join(t.TempDir(), "old.json")
	if err := os.WriteFile(staleLoc, []byte(`{"formatVersion":2}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// errors.Is, not the message: store.go decides whether to start empty from this sentinel.
	_, err = LoadJSON[Index](staleLoc, CacheFormatVersion)
	if !errors.Is(err, ErrFormat) {
		t.Fatalf("mismatch error = %v", err)
	}
	if !strings.Contains(err.Error(), fmt.Sprintf("2 != %d", CacheFormatVersion)) {
		t.Fatalf("mismatch error must name both versions: %v", err)
	}
}

// The header read is bounded, so it sees a truncated document: sonic must still answer from it, and formatVersion and scannedAt must stay leading fields. Reordering them, or pushing them past peekWindow, breaks the install list and every version check without breaking a round trip.
func TestPeekHeader(t *testing.T) {
	big := NewIndex("ck3")
	big.InstallID, big.GameVersion = "big", "1.0"
	big.ScannedAt = "2026-09-09T00:00:00Z"
	for i := range 4000 {
		big.Add(`C:\Games\CK3\game\common\traits\`+strconv.Itoa(i)+".txt", "", "", "script",
			&FileHarvest{Defs: []Def{{Kind: "traits", Key: "trait_" + strconv.Itoa(i), Line: i}}})
	}
	p := filepath.Join(t.TempDir(), "big.json")
	if err := SaveJSON(p, big); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if st.Size() <= peekWindow {
		t.Fatalf("fixture is %d bytes, not past the %d-byte window", st.Size(), peekWindow)
	}
	got, at, err := peekHeader(p)
	if err != nil {
		t.Fatalf("peekHeader on a %d-byte file: %v", st.Size(), err)
	}
	if got != CacheFormatVersion || at != big.ScannedAt {
		t.Errorf("peekHeader = (%d, %q), want (%d, %q)", got, at, CacheFormatVersion, big.ScannedAt)
	}
}

// A cache the engine will refuse must not be reported as a scan that happened: PeekCacheScannedAt feeds the install list's "last scanned" date, so after a format bump the UI showed a recent scan for a model LoadCache had already discarded.
func TestPeekCacheScannedAt(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("AppData", dir)
	const id, ver = "peek-test", "1.0"
	c := NewIndex("ck3")
	c.InstallID, c.GameVersion, c.ScannedAt = id, ver, "2026-09-09T00:00:00Z"
	// SaveCache also drops leftover legacy cache-*.json beside the new file.
	cacheDir, err := CacheDir()
	if err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(cacheDir, "cache-ws-a.json")
	if err := os.WriteFile(legacy, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SaveCache(c, nil); err != nil {
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
}
