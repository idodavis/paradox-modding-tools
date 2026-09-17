// index_test.go covers the one container: what Add records, what Drop retracts,
// and that both survive a save and a load.

package derived

import (
	"path/filepath"
	"testing"
)

func TestIndexAddAndDrop(t *testing.T) {
	const a, b = `C:\m\a.txt`, `C:\m\b.txt`
	x := NewIndex("ck3")
	x.Add(a, "a.txt", "mod", "script", &FileHarvest{
		Defs:  []Def{{Kind: "traits", Key: "brave", Line: 1, Origin: "mod"}},
		Refs:  []Ref{{Kind: "loc", Key: "brave_desc", Line: 2}},
		Edges: []Edge{{From: "brave", To: "x.1", Via: "trigger_event", Line: 3}},
		Loc:   LocDelta{Lang: "english", Vals: map[string]LocEntry{"brave_desc": {Value: "Brave"}}},
	})
	x.Add(b, "b.txt", "mod", "script", &FileHarvest{
		Defs: []Def{{Kind: "traits", Key: "craven", Line: 1, Origin: "mod"}},
	})
	x.SortLocKeys()

	if got := x.defsAt(x.DefsByKey["brave"]); len(got) != 1 || got[0].Path != a {
		t.Fatalf("defs by key = %+v", got)
	}
	if got := x.defsAt(x.DefsByKind["traits"]); len(got) != 2 {
		t.Fatalf("defs by kind = %+v", got)
	}
	if got := x.edgesAt(x.EdgesTo["x.1"]); len(got) != 1 || got[0].Path != a {
		t.Fatalf("edges to = %+v", got)
	}
	if f := x.FileOf(x.PathID(a)); f.Origin != "mod" || f.Rel != "a.txt" || f.Kind != "script" {
		t.Fatalf("file meta = %+v", f)
	}

	x.Drop(x.PathID(a))
	if got := x.defsAt(x.DefsByKey["brave"]); len(got) != 0 {
		t.Errorf("brave survived the drop: %+v", got)
	}
	if got := x.defsAt(x.DefsByKind["traits"]); len(got) != 1 || got[0].Key != "craven" {
		t.Errorf("defs by kind after drop = %+v", got)
	}
	if got := x.refsAt(x.RefsByKey["brave_desc"]); len(got) != 0 {
		t.Errorf("refs survived the drop: %+v", got)
	}
	if got := x.edgesAt(x.EdgesFrom["brave"]); len(got) != 0 {
		t.Errorf("edges survived the drop: %+v", got)
	}
	if _, ok := x.locAt("english", "brave_desc"); ok {
		t.Error("loc survived the drop")
	}
	// The other file is untouched, and a retracted slot never moves a live record.
	if got := x.defsAt(x.DefsByKey["craven"]); len(got) != 1 || got[0].Path != b {
		t.Fatalf("craven after drop = %+v", got)
	}

	path := filepath.Join(t.TempDir(), "idx.json")
	if err := SaveJSON(path, x); err != nil {
		t.Fatal(err)
	}
	out, err := loadIndex(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := out.defsAt(out.DefsByKey["craven"]); len(got) != 1 || got[0].Path != b {
		t.Fatalf("after load = %+v", got)
	}
	if out.PathID(b) < 0 {
		t.Error("the path lookup did not wake on load")
	}
}
