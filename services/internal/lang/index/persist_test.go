// persist_test.go covers the cache: an install saved and loaded back, a file of another format refused, and the files' names.
package index

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"

	"paradox-modding-tools/services/internal/lang/schema"
	"paradox-modding-tools/services/internal/persist"
)

func TestSaveAndLoad(t *testing.T) {
	in := fixture(t, "ck3")
	base := filepath.Join(t.TempDir(), "vanilla-fixture-test")
	if err := Save(base, "2026-09-21T10:00:00Z", in); err != nil {
		t.Fatal(err)
	}
	back, err := Load(base)
	if err != nil {
		t.Fatal(err)
	}

	x, y := in.Index, back.Index
	if len(y.Files) != len(x.Files) || len(y.Defs) != len(x.Defs) || len(y.Refs) != len(x.Refs) || len(y.Edges) != len(x.Edges) || len(y.Loc) != len(x.Loc) {
		t.Errorf("the records survive: %d files, %d definitions, %d references, %d edges, %d loc entries", len(y.Files), len(y.Defs), len(y.Refs), len(y.Edges), len(y.Loc))
	}
	if y.Defs[0] != x.Defs[0] || y.Refs[len(y.Refs)-1] != x.Refs[len(x.Refs)-1] || y.Files[3] != x.Files[3] || y.Loc[0].Key != x.Loc[0].Key {
		t.Errorf("a record survives whole: %+v", y.Defs[0])
	}
	if !y.Def("traits", "brave") || !y.HasLoc("v.1.t") || !y.File("brave.dds") || len(y.Named("k_x")) != 1 {
		t.Error("the lookups are rebuilt on load")
	}
	if schema.Write(back.Schema) != schema.Write(in.Schema) || len(back.Vocab.Effects) != len(in.Vocab.Effects) || len(back.Info) != len(in.Info) {
		t.Error("the schema, the vocabulary and the game info survive")
	}
	if text, err := os.ReadFile(base + ".schema.txt"); err != nil || !strings.HasPrefix(string(text), "schema = {") {
		t.Errorf("the schema is kept as the text file a person reads: %v", err)
	}

	// every word is one shared copy again, whatever the decoder made of it
	var traits []string
	for _, d := range y.Defs {
		if d.Kind == "traits" {
			traits = append(traits, d.Kind)
		}
	}
	if len(traits) < 2 || unsafe.StringData(traits[0]) != unsafe.StringData(traits[1]) {
		t.Errorf("two definitions of one kind share the kind's name: %d traits", len(traits))
	}
}

func TestLoadRefusesAnotherFormat(t *testing.T) {
	base := filepath.Join(t.TempDir(), "vanilla-fixture-test")
	if err := Save(base, "now", fixture(t, "eu5")); err != nil {
		t.Fatal(err)
	}
	if err := persist.SaveJSON(base+".json", stored{FormatVersion: FormatVersion + 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(base); !errors.Is(err, persist.ErrFormat) {
		t.Errorf("a cache of another format is scanned again, never read: %v", err)
	}
	if _, err := Load(filepath.Join(t.TempDir(), "nothing")); err == nil {
		t.Error("no cache is an error")
	}
}

func TestCachePath(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	for _, tt := range []struct{ install, version, want string }{
		{"steam:1158310", "1.19.0.6", "vanilla-steam_1158310-1_19_0_6"},
		{"C:/Games/CK3", "", "vanilla-C_Games_CK3-latest"},
	} {
		got, err := CachePath(tt.install, tt.version)
		if err != nil || filepath.Base(got) != tt.want {
			t.Errorf("CachePath(%q, %q) = %q, %v", tt.install, tt.version, filepath.Base(got), err)
		}
	}

	base, _ := CachePath("steam:1158310", "1.0")
	other, _ := CachePath("steam:529340", "1.0")
	for _, p := range []string{base + ".json", base + ".schema.txt", other + ".json"} {
		if err := os.WriteFile(p, []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := Drop("steam:1158310"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(base + ".json"); err == nil {
		t.Error("Drop removes every file of the install")
	}
	if _, err := os.Stat(other + ".json"); err != nil {
		t.Error("Drop leaves another install's files")
	}
}
