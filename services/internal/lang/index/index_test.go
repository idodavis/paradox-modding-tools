// index_test.go covers the records, the lookups and load order, and holds what the package's tests share: each fixture install scanned once.
package index

import (
	"context"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/loc"
	"paradox-modding-tools/services/internal/lang/schema"
	"paradox-modding-tools/services/internal/source"
)

var fixtures sync.Map

// fixture is one of the fixture installs under testdata, scanned once for every test that asks.
func fixture(t *testing.T, id string) *Install {
	t.Helper()
	scan, _ := fixtures.LoadOrStore(id, sync.OnceValues(func() (*Install, error) {
		dir := filepath.Join("..", "testdata", id)
		return Scan(context.Background(), Request{Game: id, Dir: dir, Version: "test", Docs: []string{filepath.Join(dir, "docs")}})
	}))
	in, err := scan.(func() (*Install, error))()
	if err != nil {
		t.Fatal(err)
	}
	return in
}

// at is a file of an origin at a rel, for load-order rows.
func at(origin, rel string) source.File {
	return source.File{Path: origin + "/" + rel, Rel: rel, Origin: origin, Format: source.Script}
}

func TestAddDropAndLookups(t *testing.T) {
	x := &Index{owned: map[int32]*owns{}}
	traits := at("mod", "common/traits/m_traits.txt")
	x.Add(&Records{File: traits,
		Defs:  []Def{{Kind: "traits", Name: "bold", Line: 3}},
		Refs:  []Ref{{Type: schema.TypeRef, Kind: "traits", Name: "brave"}},
		Edges: []Edge{{FromKind: "events", From: "m.1", ToKind: "events", To: "m.2", Via: "trigger_event"}},
		Loc:   []loc.Entry{{Key: "bold", Value: "Bold"}}})
	x.Add(&Records{File: at("mod", "gfx/interface/icons/traits/bold.dds")})

	for _, tt := range []struct {
		what      string
		got, want bool
	}{
		{"a definition by kind and name", x.Def("traits", "bold"), true},
		{"the same name of another kind", x.Def("cultures", "bold"), false},
		{"a loc key", x.HasLoc("bold"), true},
		{"a file by its path from a root, in any case", x.File("GFX/interface/icons/traits/bold.dds"), true},
		{"a file by its name alone", x.File("bold.dds"), true},
		{"a file under another extension", x.File("gfx/other/bold.tga"), true},
		{"a name with no extension is no file", x.File("bold"), false},
		{"a file that is nowhere", x.File("missing.dds"), false},
		{"several indexes answer together", All{&Index{}, x}.Def("traits", "bold") && All{&Index{}, x}.Loc("bold") && All{&Index{}, x}.File("bold.dds"), true},
	} {
		if tt.got != tt.want {
			t.Errorf("%s: %v", tt.what, tt.got)
		}
	}
	if found := x.Named("bold"); len(found) != 1 || found[0].At.Rel != traits.Rel || found[0].Line != 3 {
		t.Errorf("Named gives the definition with its file: %+v", found)
	}

	// an edit takes back what the file added; the slots stay, so nothing moves under a reader
	x.Drop(traits.Path)
	if x.Def("traits", "bold") || x.HasLoc("bold") || len(x.Named("bold")) != 0 || len(x.Defs) != 1 || x.Defs[0].File != -1 || x.Refs[0].File != -1 || x.Edges[0].File != -1 || x.Loc[0].File != -1 {
		t.Errorf("Drop left something: %+v", x.Defs)
	}
	x.Add(&Records{File: traits, Defs: []Def{{Kind: "traits", Name: "meek"}}})
	if !x.Def("traits", "meek") || len(x.Files) != 2 {
		t.Errorf("a file read again keeps its place in the inventory: %d files", len(x.Files))
	}
}

func TestWinner(t *testing.T) {
	modes := game.Get("eu5").EntryModes
	order := []string{game.OriginVanilla, "a", "b"}
	def := func(origin, rel, mode string) Found {
		return Found{Def: Def{Kind: "traits", Name: "brave", Mode: mode}, At: at(origin, rel)}
	}

	for _, tt := range []struct {
		what      string
		firstWins bool
		found     []Found
		origin    string
		rel       string
	}{
		{"last in wins", false, []Found{def("b", "common/traits/x.txt", ""), def("vanilla", "common/traits/x.txt", ""), def("a", "common/traits/x.txt", "")}, "b", ""},
		{"first in wins where the kind says so", true, []Found{def("b", "common/traits/x.txt", ""), def("a", "common/traits/x.txt", "")}, "a", ""},
		{"within one origin files load in name order", false, []Found{def("a", "common/traits/b.txt", ""), def("a", "common/traits/a.txt", "")}, "a", "common/traits/b.txt"},
		{"a folder's own files load before its subfolders'", false, []Found{def("a", "common/traits/sub/a.txt", ""), def("a", "common/traits/z.txt", "")}, "a", "common/traits/sub/a.txt"},
		{"an inject adds to the definition and never displaces it", false, []Found{def("b", "common/traits/x.txt", "INJECT"), def("vanilla", "common/traits/x.txt", "")}, "vanilla", ""},
		{"a replace takes its place, even where the first wins", true, []Found{def("vanilla", "common/traits/x.txt", ""), def("a", "common/traits/x.txt", "REPLACE")}, "a", ""},
		{"a mode is processed after the plain definitions whatever the load order", false, []Found{def("a", "common/traits/x.txt", "REPLACE"), def("b", "common/traits/x.txt", "")}, "a", ""},
		{"an optional replace of nothing does nothing", false, []Found{def("a", "common/traits/x.txt", "TRY_REPLACE")}, "", ""},
		{"an inject that may create is kept where nothing was", false, []Found{def("a", "common/traits/x.txt", "INJECT_OR_CREATE")}, "a", ""},
		{"an inject of nothing is kept by nothing", false, []Found{def("a", "common/traits/x.txt", "INJECT")}, "", ""},
	} {
		got, position := Winner(tt.found, tt.firstWins, modes, order)
		if got.At.Origin != tt.origin || (tt.rel != "" && got.At.Rel != tt.rel) || (tt.origin == "") != (position < 0) {
			t.Errorf("%s: %s %s at %d", tt.what, got.At.Origin, got.At.Rel, position)
		}
	}

	// a game with no entry modes reads the word as part of nothing
	if got, _ := Winner([]Found{def("a", "x.txt", ""), def("b", "x.txt", "")}, false, nil, order); got.At.Origin != "b" {
		t.Errorf("no modes: %s", got.At.Origin)
	}
}

func TestContests(t *testing.T) {
	s := &schema.Schema{Kinds: map[string]*schema.Kind{
		"traits":         {Name: "traits", Folders: []string{"common/traits"}},
		"history/titles": {Name: "history/titles", Folders: []string{"history/titles"}, Subject: "landed_titles"},
		"saved_scopes":   {Name: "saved_scopes"},
	}}
	index := func(origin string, defs ...Def) *Index {
		x := &Index{}
		x.Add(&Records{File: at(origin, "common/traits/x.txt"), Defs: defs})
		return x
	}
	all := All{
		index("a", Def{Kind: "traits", Name: "brave"}, Def{Kind: "traits", Name: "only_a"}, Def{Kind: "saved_scopes", Name: "target"}, Def{Kind: "history/titles", Name: "k_x"}),
		index("b", Def{Kind: "traits", Name: "brave"}, Def{Kind: "saved_scopes", Name: "target"}, Def{Kind: "history/titles", Name: "k_x"}),
	}

	got := Contests(all, s, func(kind string) bool { return false }, nil, []string{"a", "b"})
	if len(got) != 1 || got[0].Kind != "traits" || got[0].Name != "brave" || len(got[0].Found) != 2 || got[0].Found[got[0].Winner].At.Origin != "b" || got[0].FirstWins {
		t.Errorf("one name that two origins define, won by the last; a name script makes and a name about another kind never contest: %+v", got)
	}
	if first := Contests(all, s, func(kind string) bool { return true }, nil, []string{"a", "b"}); len(first) != 1 || first[0].Found[first[0].Winner].At.Origin != "a" || !first[0].FirstWins {
		t.Errorf("first wins: %+v", first)
	}
	if slices.ContainsFunc(got, func(c Contest) bool { return c.Name == "only_a" }) {
		t.Error("a name one origin defines is no contest")
	}
}
