// index_test.go covers what one walk of a file writes down (every way a definition is named, the
// references, the edges), the records and lookups, load order and contests, the scan and the mod
// workspace over its install, the cache round trip, and the gate over the real installs. Every test
// shares each fixture install, scanned once.
package index

import (
	"bytes"
	"encoding/gob"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"unsafe"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/loc"
	"paradox-modding-tools/services/internal/lang/schema"
	"paradox-modding-tools/services/internal/lang/source"
)

var fixtures sync.Map

// fixture is one of the fixture installs under testdata, scanned once for every test that asks.
func fixture(t *testing.T, id string) *Install {
	t.Helper()
	scan, _ := fixtures.LoadOrStore(id, sync.OnceValues(func() (*Install, error) {
		dir := filepath.Join("..", "testdata", id)
		return Scan(t.Context(), Request{Game: id, Dir: dir, Version: "test", Docs: []string{filepath.Join(dir, "docs")}})
	}))
	install, err := scan.(func() (*Install, error))()
	if err != nil {
		t.Fatal(err)
	}
	return install
}

// at is a file of an origin at a rel, for load-order rows.
func at(origin, rel string) source.File {
	return source.File{Path: origin + "/" + rel, Rel: rel, Origin: origin, Format: source.Script}
}

// workspace builds a game's fixture mod over its fixture install, from a copy the test may edit.
func workspace(t *testing.T, id string) (*Index, *schema.Schema) {
	t.Helper()
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS(filepath.Join("..", "testdata", "mods", id))); err != nil {
		t.Fatal(err)
	}
	idx, sch, err := BuildWorkspace(t.Context(), []Mod{{ID: "mod", Dir: dir}}, fixture(t, id))
	if err != nil {
		t.Fatal(err)
	}
	return idx, sch
}

func TestHarvest(t *testing.T) {
	idx := fixture(t, "ck3").Index
	for _, testCase := range []struct {
		what, kind, name, parent, file string
		label, scope                   string
	}{
		{"a top-level key", "traits", "brave", "", "v_traits.txt", "", ""},
		{"an event id; namespace is no event", "events", "v.1", "", "v_events.txt", "", ""},
		{"an event in a subfolder is of the same kind", "events", "vsub.1", "", "v_sub_events.txt", "", ""},
		{"a number, labelled by the kind's label member", "characters", "186004", "", "v_characters.txt", "Rognvald", ""},
		{"a name nested by depth, inside its top definition", "landed_titles", "c_x", "e_x", "v_titles.txt", "", ""},
		{"a nested kind's name: a game rule's setting", "game_rules.setting", "opt_b", "my_rule", "v_rules.txt", "", ""},
		{"a define inside its group", "defines.define", "MAX_AGE", "NGame", "v_defines.txt", "", ""},
		{"a member of an opened block", "models.pdxmesh", "v_temple_mesh", "", "v_temple.asset", "", ""},
		{"a declared name inside a container", "gui.type", "v_label", "", "v_window.gui", "", ""},
		{"a declared name with no operator", "gui.template", "v_faded", "", "v_window.gui", "", ""},
		{"a kind declared inline by its keyword, in a file of another kind", "scripted_triggers", "v_inline_trigger", "", "v_more_events.txt", "", ""},
		{"a column of a table", "map_data.definition", "2", "", "definition.csv", "", ""},
		{"a line of a list", "sound.guids", "event:/SFX/UI/v_open", "", "GUIDs.txt", "", ""},
		{"a macro's parameter, inside its macro", "params", "AMOUNT", "v_effect", "v_effects.txt", "", ""},
		{"a saved scope, with the scope type its body runs in", "saved_scopes", "target", "v.1", "v_events.txt", "", "character"},
		{"a saved scope under a cite, with the cited kind's scope type", "saved_scopes", "far_title", "vmore.1", "v_more_events.txt", "", "landed_title"},
		{"a variable made by a token that takes a block", "variables", "seen", "v.1", "v_events.txt", "", ""},
		{"a variable list is a variable", "variables", "seen_list", "v_list_effect", "v_effects.txt", "", ""},
		{"a character flag", "flags", "seen_one", "v.1", "v_events.txt", "", ""},
	} {
		at := slices.IndexFunc(idx.Defs, func(def Def) bool { return def.Kind == testCase.kind && def.Name == testCase.name })
		if at < 0 {
			t.Errorf("%s: no %s %s", testCase.what, testCase.kind, testCase.name)
			continue
		}
		def := idx.Defs[at]
		if file := path.Base(idx.Files[def.File].Rel); def.Parent != testCase.parent || file != testCase.file || def.Label != testCase.label || def.ScopeType != testCase.scope {
			t.Errorf("%s: %+v in %s", testCase.what, def, file)
		}
	}

	for _, testCase := range []struct{ what, kind, name string }{
		{"namespace is a word of the file", "events", "namespace"},
		{"a subject kind's names are references", "history/titles", "k_x"},
		{"a name of history is no landed title", "landed_titles", "norse"},
	} {
		if slices.ContainsFunc(idx.Defs, func(def Def) bool { return def.Kind == testCase.kind && def.Name == testCase.name }) {
			t.Errorf("%s: %s %s is a definition", testCase.what, testCase.kind, testCase.name)
		}
	}

	// the references the reader finds
	for _, testCase := range []struct {
		what               string
		kind, name, parent string
		file               string
		valueType          schema.ValueType
		declared           bool
	}{
		{"a value at a field that accepts a kind", "event_themes", "seduction", "", "v_events.txt", schema.TypeRef, false},
		{"a quoted value is read as a bare one", "cultures", "norse", "", "v_characters.txt", schema.TypeRef, false},
		{"a number that names a character, by the link's declared output", "characters", "186004", "", "v_characters.txt", schema.TypeRef, false},
		{"a cite, declared by the vocabulary", "landed_titles", "k_x", "", "v_events.txt", schema.TypeRef, true},
		{"a cite with a member names the member inside its parent", "defines.define", "WAR_COST", "NWar", "v_more_events.txt", schema.TypeRef, true},
		{"a macro written as a key", "scripted_effects", "v_effect", "", "v_events.txt", schema.TypeRef, false},
		{"an argument key names the parameter of the macro called", "params", "AMOUNT", "v_effect", "v_events.txt", schema.TypeRef, false},
		{"a scripted list behind an iterator prefix", "scripted_lists", "adult_child", "", "v_effects.txt", schema.TypeRef, false},
		{"a name token that reads", "variables", "seen_list", "", "v_effects.txt", schema.TypeRef, false},
		{"a subject kind's key names the subject's definition", "landed_titles", "k_x", "", "v_titles.txt", schema.TypeRef, false},
		{"a file of a file-named subject kind names it by its base name", "cultures", "norse", "", "norse.txt", schema.TypeRef, false},
		{"a loc key", "", "v.1.t", "", "v_events.txt", schema.TypeLoc, false},
		{"a loc key at a body's field named like a link: a message's title", "", "v.1.toast", "", "v_events.txt", schema.TypeLoc, false},
		{"a loc key under that field written with a block", "", "v.1.toast_first", "", "v_events.txt", schema.TypeLoc, false},
		{"a file named by its name alone, from another folder", "", "v_frame.dds", "", "v_window.gui", schema.TypeFile, false},
		{"a file written under another extension", "", "gfx/interface/v_bg.tga", "", "v_window.gui", schema.TypeFile, false},
	} {
		if !slices.ContainsFunc(idx.Refs, func(ref Ref) bool {
			return ref.Type == testCase.valueType && ref.Kind == testCase.kind && ref.Name == testCase.name && ref.Parent == testCase.parent && ref.Declared == testCase.declared && path.Base(idx.Files[ref.File].Rel) == testCase.file
		}) {
			t.Errorf("%s: no %s reference to %q %q in %s", testCase.what, testCase.valueType, testCase.kind, testCase.name, testCase.file)
		}
	}

	for _, name := range []string{"is_adult", "add_gold", "yes", "character_event"} {
		if slices.ContainsFunc(idx.Refs, func(ref Ref) bool { return ref.Name == name }) {
			t.Errorf("%s names nothing: a declared token, a literal and a word nothing claims are no references", name)
		}
	}

	// the edges
	for _, testCase := range []struct{ what, from, toKind, to, via string }{
		{"an effect fires an event", "v.1", "events", "v.2", "trigger_event"},
		{"a macro written as a key is invoked", "v.1", "scripted_effects", "v_effect", "v_effect"},
		{"an on_action's list of events", "on_birth", "events", "v.1", "events"},
		{"a numbered entry of a list keeps the list's name", "on_birth", "events", "v.2", "random_events"},
		{"a scripted list is invoked behind its iterator", "v_list_effect", "scripted_lists", "adult_child", "every_adult_child"},
		{"a scripted trigger invoked inside a trigger", "v_rule_trigger", "scripted_triggers", "v_rich_trigger", "v_rich_trigger"},
	} {
		if !slices.ContainsFunc(idx.Edges, func(edge Edge) bool {
			return edge.From == testCase.from && edge.ToKind == testCase.toKind && edge.To == testCase.to && edge.Via == testCase.via
		}) {
			t.Errorf("%s: no edge %s -> %s %s via %s", testCase.what, testCase.from, testCase.toKind, testCase.to, testCase.via)
		}
	}
	for _, testCase := range []struct{ what, toKind string }{
		{"a trait runs nothing, so naming one fires nothing", "traits"},
		{"holder = 186004 is no entry of a list, and nothing fires a character", "characters"},
	} {
		if slices.ContainsFunc(idx.Edges, func(edge Edge) bool { return edge.ToKind == testCase.toKind }) {
			t.Errorf("%s: an edge to %s", testCase.what, testCase.toKind)
		}
	}

	// the other games: vic3's history wrapper keys, eu5's nested names
	vic3, eu5 := fixture(t, "vic3").Index, fixture(t, "eu5").Index
	if !vic3.Def("country_definitions", "GBR") || !slices.ContainsFunc(vic3.Refs, func(ref Ref) bool { return ref.Kind == "country_definitions" && ref.Name == "GBR" && ref.Declared }) {
		t.Error("vic3: a country, and c:GBR written as a key of a history file's wrapper")
	}
	if !slices.ContainsFunc(vic3.Refs, func(ref Ref) bool { return ref.Type == schema.TypeLoc && ref.Name == "vv.1.a" }) {
		t.Error("vic3: an option's name is a loc key, though the dump declares a link named name")
	}
	if slices.ContainsFunc(vic3.Defs, func(def Def) bool { return def.Name == "COUNTRIES" || def.Name == "POPS" }) {
		t.Error("vic3: a history file's wrapper word defines nothing")
	}
	for _, name := range []string{"europe", "western_europe", "france_region", "paris_area"} {
		if len(eu5.Named(name)) != 1 {
			t.Errorf("eu5: %s, nested by depth in definitions.txt, is one definition: %+v", name, eu5.Named(name))
		}
	}
	if found := eu5.Named("paris_area"); len(found) == 1 && found[0].Parent != "europe" {
		t.Errorf("eu5: a nested name's parent is its top definition: %+v", found[0])
	}
}

func TestAddDropAndLookups(t *testing.T) {
	idx := &Index{owned: map[int32]*owns{}}
	traits := at("mod", "common/traits/m_traits.txt")
	idx.Add(&Records{File: traits,
		Defs:  []Def{{Kind: "traits", Name: "bold", Line: 3}},
		Refs:  []Ref{{Type: schema.TypeRef, Kind: "traits", Name: "brave"}},
		Edges: []Edge{{FromKind: "events", From: "m.1", ToKind: "events", To: "m.2", Via: "trigger_event"}},
		Loc:   []loc.Entry{{Key: "bold", Value: "Bold"}}})
	idx.Add(&Records{File: at("mod", "gfx/interface/icons/traits/bold.dds")})

	for _, testCase := range []struct {
		what      string
		got, want bool
	}{
		{"a definition by kind and name", idx.Def("traits", "bold"), true},
		{"the same name of another kind", idx.Def("cultures", "bold"), false},
		{"a loc key", idx.HasLoc("bold"), true},
		{"a file by its path from a root, in any case", idx.File("GFX/interface/icons/traits/bold.dds"), true},
		{"a file by its name alone", idx.File("bold.dds"), true},
		{"a file under another extension", idx.File("gfx/other/bold.tga"), true},
		{"a name with no extension is no file", idx.File("bold"), false},
		{"a file that is nowhere", idx.File("missing.dds"), false},
		{"several indexes answer together", All{&Index{}, idx}.Def("traits", "bold") && All{&Index{}, idx}.Loc("bold") && All{&Index{}, idx}.File("bold.dds"), true},
	} {
		if testCase.got != testCase.want {
			t.Errorf("%s: %v", testCase.what, testCase.got)
		}
	}
	if found := idx.Named("bold"); len(found) != 1 || found[0].At.Rel != traits.Rel || found[0].Line != 3 {
		t.Errorf("Named gives the definition with its file: %+v", found)
	}

	// an edit takes back what the file added; the slots stay, so nothing moves under a reader
	idx.Drop(traits.Path)
	if idx.Def("traits", "bold") || idx.HasLoc("bold") || len(idx.Named("bold")) != 0 || len(idx.Defs) != 1 || idx.Defs[0].File != -1 || idx.Refs[0].File != -1 || idx.Edges[0].File != -1 || idx.Loc[0].File != -1 {
		t.Errorf("Drop left something: %+v", idx.Defs)
	}
	idx.Add(&Records{File: traits, Defs: []Def{{Kind: "traits", Name: "meek"}}})
	if !idx.Def("traits", "meek") || len(idx.Files) != 2 {
		t.Errorf("a file read again keeps its place in the inventory: %d files", len(idx.Files))
	}
}

func TestWinner(t *testing.T) {
	modes := game.Get("eu5").EntryModes
	order := []string{game.OriginVanilla, "a", "b"}
	def := func(origin, rel, mode string) Found {
		return Found{Kind: "traits", Name: "brave", Mode: mode, At: at(origin, rel)}
	}

	for _, testCase := range []struct {
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
		got, position := Winner(testCase.found, testCase.firstWins, modes, order)
		if got.At.Origin != testCase.origin || (testCase.rel != "" && got.At.Rel != testCase.rel) || (testCase.origin == "") != (position < 0) {
			t.Errorf("%s: %s %s at %d", testCase.what, got.At.Origin, got.At.Rel, position)
		}
	}

	// a game with no entry modes reads the word as part of nothing
	if got, _ := Winner([]Found{def("a", "x.txt", ""), def("b", "x.txt", "")}, false, nil, order); got.At.Origin != "b" {
		t.Errorf("no modes: %s", got.At.Origin)
	}

	// contests: last or first wins; script-made and subject kinds never contest
	sch := &schema.Schema{Kinds: map[string]*schema.Kind{
		"traits":         {Name: "traits", Folders: []string{"common/traits"}},
		"history/titles": {Name: "history/titles", Folders: []string{"history/titles"}, Subject: "landed_titles"},
		"saved_scopes":   {Name: "saved_scopes"},
	}}
	index := func(origin string, defs ...Def) *Index {
		idx := &Index{}
		idx.Add(&Records{File: at(origin, "common/traits/x.txt"), Defs: defs})
		return idx
	}
	all := All{
		index("a", Def{Kind: "traits", Name: "brave"}, Def{Kind: "traits", Name: "only_a"}, Def{Kind: "saved_scopes", Name: "target"}, Def{Kind: "history/titles", Name: "k_x"}),
		index("b", Def{Kind: "traits", Name: "brave"}, Def{Kind: "saved_scopes", Name: "target"}, Def{Kind: "history/titles", Name: "k_x"}),
	}

	got := Contests(all, sch, func(kind string) bool { return false }, nil, []string{"a", "b"})
	if len(got) != 1 || got[0].Kind != "traits" || got[0].Name != "brave" || len(got[0].Found) != 2 || got[0].Found[got[0].Winner].At.Origin != "b" || got[0].FirstWins {
		t.Errorf("one name that two origins define, won by the last; a name script makes and a name about another kind never contest: %+v", got)
	}
	if contests := Contests(all, sch, func(kind string) bool { return true }, nil, []string{"a", "b"}); len(contests) != 1 || contests[0].Found[contests[0].Winner].At.Origin != "a" || !contests[0].FirstWins {
		t.Errorf("first wins: %+v", contests)
	}
	if slices.ContainsFunc(got, func(contest Contest) bool { return contest.Name == "only_a" }) {
		t.Error("a name one origin defines is no contest")
	}
}

func TestScan(t *testing.T) {
	if _, err := Scan(t.Context(), Request{Game: "ck3", Dir: t.TempDir()}); err == nil || !strings.Contains(err.Error(), "no script files") {
		t.Errorf("a folder with no script is the wrong folder, not an empty install: %v", err)
	}
	if _, err := Scan(t.Context(), Request{Game: "hoi4", Dir: t.TempDir()}); err == nil {
		t.Error("a game the registry does not know")
	}

	install := fixture(t, "ck3")
	idx := install.Index
	if len(idx.Roots) != 1 || install.Schema.Kinds["traits"] == nil || len(install.Vocab.Effects) == 0 || len(install.Info) == 0 {
		t.Errorf("a scan gives the roots, the schema, the vocabulary and the game info: %v, %d kinds, %d effects, %d info folders", idx.Roots, len(install.Schema.Kinds), len(install.Vocab.Effects), len(install.Info))
	}
	if !idx.File("gfx/interface/icons/traits/brave.dds") || !slices.ContainsFunc(idx.Files, func(file source.File) bool { return file.Format == source.Image }) {
		t.Error("every file is in the inventory, read or not: the index is the catalogue of binary files")
	}
	if !idx.HasLoc("v.1.t") || !idx.HasLoc("setting_opt_a") {
		t.Error("the english localization is read")
	}
	if at := slices.IndexFunc(idx.Loc, func(entry Loc) bool { return entry.Key == "v.1.t" }); at < 0 || !strings.HasSuffix(idx.Files[idx.Loc[at].File].Rel, "v_l_english.yml") {
		t.Error("a loc entry knows its file")
	}

	var stages []string
	dir := filepath.Join("..", "testdata", "ck3")
	progress := func(stage string, done, total int) {
		if done == total {
			stages = append(stages, stage)
		}
	}
	if _, err := Scan(t.Context(), Request{Game: "ck3", Dir: dir, Docs: []string{filepath.Join(dir, "docs")}, Progress: progress}); err != nil {
		t.Fatal(err)
	}
	if want := []string{"reading localization", "reading the install", "learning the schema", "reading definitions", "reading references"}; !slices.Equal(stages, want) {
		t.Errorf("progress runs each stage to its end, in order: %v", stages)
	}
}

func TestBuildWorkspace(t *testing.T) {
	install := fixture(t, "ck3")
	idx, sch := workspace(t, "ck3")

	for _, testCase := range []struct{ what, kind, name string }{
		{"a mod's event", "events", "m.1"},
		{"a mod's trait", "traits", "bold"},
		{"a saved scope a mod makes", "saved_scopes", "mod_target"},
		{"a definition of a kind only the mod has", "mod_things", "first_thing"},
	} {
		if found := idx.Named(testCase.name); !idx.Def(testCase.kind, testCase.name) || found[0].At.Origin != "mod" {
			t.Errorf("%s: %+v", testCase.what, found)
		}
	}
	if sch.Kinds["mod_things"] == nil || install.Schema.Kinds["mod_things"] != nil {
		t.Error("a folder the install has no kind for is a kind of the workspace, and the install's schema never changes")
	}
	if sch.Kinds["traits"] != install.Schema.Kinds["traits"] || sch.Kinds["traits"].Shape.Members["typo_membr"] != nil {
		t.Error("a mod never adds a member to a kind the install learned")
	}

	// a mod's words name the install's definitions as well as its own
	for _, testCase := range []struct{ what, kind, name string }{
		{"the install's event", "events", "v.2"},
		{"the install's macro", "scripted_effects", "v_effect"},
		{"the install's scripted trigger", "scripted_triggers", "v_rich_trigger"},
		{"the mod's own trait, in a field the install learned", "traits", "bold"},
		{"the install's loc key", "", "v.1.t"},
	} {
		if !slices.ContainsFunc(idx.Refs, func(ref Ref) bool { return ref.Kind == testCase.kind && ref.Name == testCase.name }) {
			t.Errorf("no reference to %s", testCase.what)
		}
	}
	if !slices.ContainsFunc(idx.Edges, func(edge Edge) bool { return edge.From == "m.1" && edge.To == "v.2" && edge.Via == "trigger_event" }) {
		t.Error("a mod's event fires the install's")
	}
	if !idx.File("bold.dds") || len(idx.Roots) != 1 {
		t.Errorf("a mod's binary files are in its inventory: roots %v", idx.Roots)
	}

	// replace_path(s) hides the install's folders
	both := All{idx, install.Index}
	if !slices.Equal(idx.Replaced, []string{"common/opinion_modifiers"}) {
		t.Fatalf("a CK3 mod's replace_path lines: %v", idx.Replaced)
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

	vic3, _ := workspace(t, "vic3")
	if !slices.Equal(vic3.Replaced, []string{"common/genes"}) {
		t.Errorf("a Victoria 3 mod's game_custom_data.replace_paths: %v", vic3.Replaced)
	}

	// INJECT: and REPLACE: are recorded as the entry mode
	for _, testCase := range []struct{ name, mode string }{
		{"GBR", "INJECT"},
		{"FRA", "REPLACE"},
	} {
		found := append(fixture(t, "vic3").Index.Named(testCase.name), vic3.Named(testCase.name)...)
		if len(found) != 2 || found[1].Mode != testCase.mode {
			t.Fatalf("%s: the word before a name is its entry mode, and no part of the name: %+v", testCase.name, found)
		}
	}

	// re-indexing an edited file
	file := idx.Files[slices.IndexFunc(idx.Files, func(file source.File) bool { return strings.HasSuffix(file.Rel, "m_events.txt") })]
	edited := "namespace = m\n\nm.1 = {\n\ttype = character_event\n\timmediate = {\n\t\ttrigger_event = v.1\n\t}\n}\nm.3 = {\n\ttype = character_event\n}\n"
	idx.IndexFile(file, edited, schema.Reader{Schema: sch, Vocab: install.Vocab, Found: All{idx, install.Index}})

	var fired []string
	for _, edge := range idx.Edges {
		if edge.File >= 0 && edge.From == "m.1" {
			fired = append(fired, edge.To)
		}
	}
	if !slices.Equal(fired, []string{"v.1"}) {
		t.Errorf("m.1 fires %v after the edit, want v.1 alone", fired)
	}
}

// TestInstallsAreScanned is the gate over the real installs: each game is scanned, saved, loaded
// back, and the loaded install is measured. It checks that a loaded CK3 install holds at most
// 569 MB, that an install's roots load base layers first, that no rel exists in two of EU5's stage
// folders, and that vanilla writes no entry modes. It runs only with PMT_INDEX_GATE=1 and the three
// PMT_TEST_INSTALL_ variables, each a game's root folder.
func TestInstallsAreScanned(t *testing.T) {
	if testing.Short() || os.Getenv("PMT_INDEX_GATE") == "" {
		t.Skip("scans the three installs; set PMT_INDEX_GATE=1")
	}
	for _, testCase := range []struct {
		id, env string
		defs    map[string]string // a definition that must be found, to its kind
		heapMB  uint64            // the most the loaded install may hold, in MB; 0 for no ceiling
	}{
		{"ck3", "PMT_TEST_INSTALL_CK3", map[string]string{"brave": "traits", "k_france": "landed_titles", "on_death": "on_action"}, 569},
		{"vic3", "PMT_TEST_INSTALL_VIC3", map[string]string{"GBR": "country_definitions", "law_monarchy": "laws"}, 0},
		{"eu5", "PMT_TEST_INSTALL_EU5", map[string]string{"FRA": "countries"}, 0},
	} {
		dir := os.Getenv(testCase.env)
		if dir == "" {
			t.Errorf("%s: %s is not set", testCase.id, testCase.env)
			continue
		}
		user := game.UserDataDir(testCase.id, dir)
		install, err := Scan(t.Context(), Request{Game: testCase.id, Dir: dir, Version: "gate", Docs: []string{filepath.Join(user, "logs"), filepath.Join(user, "docs")}})
		if err != nil {
			t.Fatalf("%s: %v", testCase.id, err)
		}
		idx := install.Index
		for name, kind := range testCase.defs {
			if !idx.Def(kind, name) {
				t.Errorf("%s: no %s %s", testCase.id, kind, name)
			}
		}

		// load order: the engine's base layers first, and no rel in two of EU5's stage folders
		layer := func(root string) int {
			rel, _ := filepath.Rel(dir, root)
			return map[string]int{"clausewitz": 0, "jomini": 1}[strings.ToLower(strings.Split(filepath.ToSlash(rel), "/")[0])]
		}
		if !slices.IsSortedFunc(idx.Roots[:min(2, len(idx.Roots))], func(first, second string) int { return layer(first) - layer(second) }) {
			t.Errorf("%s: the base layers do not load first: %v", testCase.id, idx.Roots)
		}
		// a file's root is its path less its rel; sibling roots in one folder are one layer's stages
		seen, twice, modes, collide := map[string]string{}, 0, 0, 0
		for _, file := range idx.Files {
			rel := strings.ToLower(file.Rel)
			root := filepath.Clean(strings.TrimSuffix(filepath.ToSlash(file.Path), file.Rel))
			if first, ok := seen[rel]; ok && first != root && filepath.Dir(first) == filepath.Dir(root) && len(game.Get(testCase.id).StageRoots) > 0 {
				twice++
			}
			seen[rel] = root
		}
		if twice > 0 {
			t.Errorf("%s: %d rels in two stage folders of one layer, which makes their order matter", testCase.id, twice)
		}

		// how a file-like word is found: by path from a root, by name alone, under another extension
		byPath, byName, byStem := 0, 0, 0
		for _, ref := range idx.Refs {
			name := strings.ToLower(ref.Name)
			base := filepath.Base(name)
			switch _, isRel := idx.rels[name]; {
			case ref.Type != schema.TypeFile:
			case isRel:
				byPath++
			case len(idx.bases[base]) > 0:
				byName++
			default:
				byStem++
			}
		}
		nested := map[[2]string]string{}
		for _, def := range idx.Defs {
			if slices.ContainsFunc(game.Get(testCase.id).EntryModes, func(mode game.EntryMode) bool { return mode.Word == def.Mode }) {
				modes++
			}
			at := [2]string{def.Kind, def.Name}
			if kind := install.Schema.Kinds[def.Kind]; kind != nil && kind.Parent != "" {
				if parent, ok := nested[at]; ok && parent != def.Parent {
					collide++
				}
				nested[at] = def.Parent
			}
		}

		// what the loaded install holds, measured as heap growth across the load
		base := filepath.Join(t.TempDir(), "vanilla-"+testCase.id+".gob")
		file, err := os.Create(base)
		if err != nil {
			t.Fatal(err)
		}
		if err := gob.NewEncoder(file).Encode(Body{Index: idx, Vocab: install.Vocab, Info: install.Info}); err != nil {
			t.Fatal(err)
		}
		file.Close()
		defs, refs, edges, locs := len(idx.Defs), len(idx.Refs), len(idx.Edges), len(idx.Loc)
		// the schema is parsed again in the window below: the app holds one beside the records
		schemaText := schema.Write(install.Schema)
		install, idx = nil, nil
		var before, after runtime.MemStats
		runtime.GC()
		runtime.GC()
		runtime.ReadMemStats(&before)
		back, err := os.Open(base)
		if err != nil {
			t.Fatal(err)
		}
		var loaded Body
		if err := gob.NewDecoder(back).Decode(&loaded); err != nil {
			t.Fatal(err)
		}
		back.Close()
		loaded.Restore()
		readBack, err := schema.Read(schemaText)
		if err != nil {
			t.Fatal(err)
		}
		runtime.GC()
		runtime.GC()
		runtime.ReadMemStats(&after)
		held := (after.HeapAlloc - before.HeapAlloc) >> 20
		if len(loaded.Index.Defs) != defs || len(loaded.Index.Refs) != refs || len(loaded.Index.Edges) != edges || len(loaded.Index.Loc) != locs {
			t.Errorf("%s: the cache gave back %d definitions of %d", testCase.id, len(loaded.Index.Defs), defs)
		}
		if modes > 0 {
			t.Errorf("%s: vanilla writes %d entry modes", testCase.id, modes)
		}
		if testCase.heapMB > 0 && held > testCase.heapMB {
			t.Errorf("%s: the loaded install holds %d MB, more than its %d MB ceiling", testCase.id, held, testCase.heapMB)
		}
		t.Logf("INDEX %s: %d files, %d definitions, %d references, %d edges, %d loc entries; loaded install holds %d MB; files found %d by path, %d by name, %d under another extension; %d rels in two stage folders, %d entry modes in vanilla, %d nested names under two parents", testCase.id, len(loaded.Index.Files), defs, refs, edges, locs, held, byPath, byName, byStem, twice, modes, collide)
		runtime.KeepAlive(loaded)
		runtime.KeepAlive(readBack)
	}
}

func TestEncodeDecode(t *testing.T) {
	install := fixture(t, "ck3")
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(Body{Index: install.Index, Vocab: install.Vocab, Info: install.Info}); err != nil {
		t.Fatal(err)
	}
	var back Body
	if err := gob.NewDecoder(&buf).Decode(&back); err != nil {
		t.Fatal(err)
	}
	back.Restore()

	idx, loaded := install.Index, back.Index
	if len(loaded.Files) != len(idx.Files) || len(loaded.Defs) != len(idx.Defs) || len(loaded.Refs) != len(idx.Refs) || len(loaded.Edges) != len(idx.Edges) || len(loaded.Loc) != len(idx.Loc) {
		t.Errorf("the records survive: %d files, %d definitions, %d references, %d edges, %d loc entries", len(loaded.Files), len(loaded.Defs), len(loaded.Refs), len(loaded.Edges), len(loaded.Loc))
	}
	if loaded.Defs[0] != idx.Defs[0] || loaded.Refs[len(loaded.Refs)-1] != idx.Refs[len(idx.Refs)-1] || loaded.Files[3] != idx.Files[3] || loaded.Loc[0].Key != idx.Loc[0].Key {
		t.Errorf("a record survives whole: %+v", loaded.Defs[0])
	}
	if !loaded.Def("traits", "brave") || !loaded.HasLoc("v.1.t") || !loaded.File("brave.dds") || len(loaded.Named("k_x")) != 1 {
		t.Error("the lookups are rebuilt on load")
	}
	if len(back.Vocab.Effects) != len(install.Vocab.Effects) || len(back.Info) != len(install.Info) {
		t.Error("the vocabulary and the game info survive")
	}

	// every word is one shared copy again, whatever the decoder made of it
	var traits []string
	for _, def := range loaded.Defs {
		if def.Kind == "traits" {
			traits = append(traits, def.Kind)
		}
	}
	if len(traits) < 2 || unsafe.StringData(traits[0]) != unsafe.StringData(traits[1]) {
		t.Errorf("two definitions of one kind share the kind's name: %d traits", len(traits))
	}
}
