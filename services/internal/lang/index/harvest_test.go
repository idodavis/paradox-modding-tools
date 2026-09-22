// harvest_test.go covers what one walk of a file writes down, read out of the scanned fixture installs: every way a definition is named, the references the reader finds, and the edges.
package index

import (
	"path"
	"slices"
	"testing"

	"paradox-modding-tools/services/internal/lang/schema"
)

func TestHarvestDefinitions(t *testing.T) {
	x := fixture(t, "ck3").Index
	for _, tt := range []struct {
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
		at := slices.IndexFunc(x.Defs, func(d Def) bool { return d.Kind == tt.kind && d.Name == tt.name })
		if at < 0 {
			t.Errorf("%s: no %s %s", tt.what, tt.kind, tt.name)
			continue
		}
		d := x.Defs[at]
		if file := path.Base(x.Files[d.File].Rel); d.Parent != tt.parent || file != tt.file || d.Label != tt.label || d.ScopeType != tt.scope {
			t.Errorf("%s: %+v in %s", tt.what, d, file)
		}
	}

	for _, tt := range []struct{ what, kind, name string }{
		{"namespace is a word of the file", "events", "namespace"},
		{"a subject kind's names are references", "history/titles", "k_x"},
		{"a name of history is no landed title", "landed_titles", "norse"},
	} {
		if slices.ContainsFunc(x.Defs, func(d Def) bool { return d.Kind == tt.kind && d.Name == tt.name }) {
			t.Errorf("%s: %s %s is a definition", tt.what, tt.kind, tt.name)
		}
	}
}

func TestHarvestReferences(t *testing.T) {
	x := fixture(t, "ck3").Index
	for _, tt := range []struct {
		what               string
		kind, name, parent string
		file               string
		typ                schema.ValueType
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
		{"a file named by its name alone, from another folder", "", "v_frame.dds", "", "v_window.gui", schema.TypeFile, false},
		{"a file written under another extension", "", "gfx/interface/v_bg.tga", "", "v_window.gui", schema.TypeFile, false},
	} {
		if !slices.ContainsFunc(x.Refs, func(r Ref) bool {
			return r.Type == tt.typ && r.Kind == tt.kind && r.Name == tt.name && r.Parent == tt.parent && r.Declared == tt.declared && path.Base(x.Files[r.File].Rel) == tt.file
		}) {
			t.Errorf("%s: no %s reference to %q %q in %s", tt.what, tt.typ, tt.kind, tt.name, tt.file)
		}
	}

	for _, name := range []string{"is_adult", "add_gold", "yes", "character_event"} {
		if slices.ContainsFunc(x.Refs, func(r Ref) bool { return r.Name == name }) {
			t.Errorf("%s names nothing: a declared token, a literal and a word nothing claims are no references", name)
		}
	}
}

func TestHarvestEdges(t *testing.T) {
	x := fixture(t, "ck3").Index
	for _, tt := range []struct{ what, from, toKind, to, via string }{
		{"an effect fires an event", "v.1", "events", "v.2", "trigger_event"},
		{"a macro written as a key is invoked", "v.1", "scripted_effects", "v_effect", "v_effect"},
		{"an on_action's list of events", "on_birth", "events", "v.1", "events"},
		{"a numbered entry of a list keeps the list's name", "on_birth", "events", "v.2", "random_events"},
		{"a scripted list is invoked behind its iterator", "v_list_effect", "scripted_lists", "adult_child", "every_adult_child"},
		{"a scripted trigger invoked inside a trigger", "v_rule_trigger", "scripted_triggers", "v_rich_trigger", "v_rich_trigger"},
	} {
		if !slices.ContainsFunc(x.Edges, func(e Edge) bool {
			return e.From == tt.from && e.ToKind == tt.toKind && e.To == tt.to && e.Via == tt.via
		}) {
			t.Errorf("%s: no edge %s -> %s %s via %s", tt.what, tt.from, tt.toKind, tt.to, tt.via)
		}
	}
	for _, tt := range []struct{ what, toKind string }{
		{"a trait runs nothing, so naming one fires nothing", "traits"},
		{"holder = 186004 is no entry of a list, and nothing fires a character", "characters"},
	} {
		if slices.ContainsFunc(x.Edges, func(e Edge) bool { return e.ToKind == tt.toKind }) {
			t.Errorf("%s: an edge to %s", tt.what, tt.toKind)
		}
	}
}

func TestHarvestOtherGames(t *testing.T) {
	vic3, eu5 := fixture(t, "vic3").Index, fixture(t, "eu5").Index
	if !vic3.Def("country_definitions", "GBR") || !slices.ContainsFunc(vic3.Refs, func(r Ref) bool { return r.Kind == "country_definitions" && r.Name == "GBR" && r.Declared }) {
		t.Error("vic3: a country, and c:GBR written as a key of a history file's wrapper")
	}
	if slices.ContainsFunc(vic3.Defs, func(d Def) bool { return d.Name == "COUNTRIES" || d.Name == "POPS" }) {
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
