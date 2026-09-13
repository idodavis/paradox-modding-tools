// persist_test.go covers VanillaCache / loc sidecar save-load and version rejection.

package derived

import (
	"errors"
	"fmt"
	"os"
	"paradox-modding-tools/services/internal/lang/declared"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

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
		t.Errorf("format-8 fields: refs=%+v kinds=%v", out.LocRefs, out.FieldValueKinds)
	}
	if len(out.StructureBlocks["event"]) != 1 || out.StructureBlocks["event"][0] != "immediate" {
		t.Errorf("structureBlocks: %v", out.StructureBlocks)
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
			if _, err := loadCacheFile(p); err == nil {
				t.Fatal("expected error")
			}
		})
	}

	thin := filepath.Join(t.TempDir(), "thin.json")
	body := fmt.Sprintf(`{"formatVersion":%d,"gameId":"ck3","gameVersion":"1"}`, CacheFormatVersion)
	if err := os.WriteFile(thin, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := loadCacheFile(thin)
	if err != nil {
		t.Fatal(err)
	}
	c.FieldInfo["k"] = "v"
	c.FieldInfoByKind["event"] = map[string]string{"type": "t"}
	if c.FieldInfo["k"] != "v" || c.FieldInfoByKind["event"]["type"] != "t" {
		t.Fatalf("nil maps not initialized: %+v", c)
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
	dir := t.TempDir()
	legacy := filepath.Join(dir, "cache-ws-a.json")
	keep := filepath.Join(dir, "vanilla-x-1.json")
	if err := os.WriteFile(legacy, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keep, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	dropLegacyWorkspaceCaches(dir)
	if _, err := os.Stat(legacy); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("legacy cache-*.json should be removed")
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("vanilla file should remain: %v", err)
	}

	locIn := &VanillaLoc{
		FormatVersion: LocFormatVersion,
		Sites:         map[string]LocEntry{"k": {Path: "a.yml", Line: 1, Value: "v"}},
	}
	locPath := filepath.Join(t.TempDir(), "sidecar.json")
	if err := SaveJSON(locPath, locIn); err != nil {
		t.Fatal(err)
	}
	got, err := LoadJSON[VanillaLoc](locPath, LocFormatVersion)
	if err != nil {
		t.Fatal(err)
	}
	if got.Sites["k"].Value != "v" || got.Sites["k"].Line != 1 {
		t.Fatalf("loc sidecar = %+v", got)
	}

	staleLoc := filepath.Join(t.TempDir(), "loc-v2.json")
	if err := os.WriteFile(staleLoc, []byte(`{"formatVersion":2,"sites":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// errors.Is, not the message: store.go decides whether to start empty from
	// this sentinel, and a string equality here would pass while that broke.
	_, err = LoadJSON[VanillaLoc](staleLoc, LocFormatVersion)
	if !errors.Is(err, ErrFormat) {
		t.Fatalf("mismatch error = %v", err)
	}
	if !strings.Contains(err.Error(), fmt.Sprintf("2 != %d", LocFormatVersion)) {
		t.Fatalf("mismatch error must name both versions: %v", err)
	}
}

// A cache the engine will refuse must not be reported as a scan that happened.
// PeekCacheScannedAt feeds the install list's "last scanned" date, and it used
// to answer from the file's existence alone — so after a format bump the UI
// showed a recent scan for a model LoadCache had already discarded, and the one
// visible signal that a rescan was needed said everything was fine.
func TestPeekCacheScannedAtRefusesAWrongFormat(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("AppData", dir)
	const id, ver = "peek-test", "1.0"
	c := &VanillaCache{
		FormatVersion: CacheFormatVersion, InstallID: id, GameID: "ck3",
		GameVersion: ver, ScannedAt: "2026-09-09T00:00:00Z",
	}
	if err := SaveCache(c); err != nil {
		t.Fatal(err)
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
}

// ReadSchemaStatus backs the Workspace Health strip, so it must answer without
// loading the model: health is polled, and script_docs is a couple of
// megabytes against the model's ninety.
func TestReadSchemaStatusIsSideEffectFree(t *testing.T) {
	const installID = "status-test"
	t.Cleanup(func() { _ = DropVanillaFiles(installID) })

	install := t.TempDir()
	docs := filepath.Join(install, "logs")
	if err := os.MkdirAll(docs, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docs, "effects.log"),
		[]byte(archiveEffects), 0o644); err != nil {
		t.Fatal(err)
	}
	pinUserData(t, install, install)

	st := ReadSchemaStatus("ck3", install)
	if st.Source != declared.SchemaLive {
		t.Fatalf("source = %q, want live", st.Source)
	}
	if st.Effects == 0 {
		t.Errorf("no effects counted: %+v", st)
	}
	if st.ReadAt == "" {
		t.Error("ReadAt empty; staleness cannot be judged")
	}
	if st.Archived {
		t.Error("reading status must not create an archive")
	}

	// With no dumps and no archive the strip must say so plainly rather than
	// report a half-loaded type system.
	if err := os.RemoveAll(docs); err != nil {
		t.Fatal(err)
	}
	gone := ReadSchemaStatus("ck3", install)
	if gone.Source != declared.SchemaMissing || gone.Effects != 0 {
		t.Errorf("no dumps and no archive: %+v", gone)
	}
	if gone.Folder == "" {
		t.Error("the walkthrough needs a folder to point at even when nothing is there")
	}
}

const archiveEffects = `Effect Documentation:

--------------------

add_gold - Adds gold to the scoped character
Supported Scopes: character

--------------------
`
