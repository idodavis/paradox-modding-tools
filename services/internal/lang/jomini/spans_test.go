package jomini

import (
	"reflect"
	"testing"
)

func TestCite(t *testing.T) {
	for _, c := range []struct {
		text string
		want Cited
		ok   bool
	}{
		// the old engine's rows, "Syntax: cites and spans" in rows.md
		{"culture:english", Cited{Prefix: "culture", Name: "english"}, true},
		{"trait:brave", Cited{Prefix: "trait", Name: "brave"}, true},
		{"coat_of_arms:b_appleby", Cited{Prefix: "coat_of_arms", Name: "b_appleby"}, true},
		{"title:k_france", Cited{Prefix: "title", Name: "k_france"}, true},
		{"script_value:max_soldiers", Cited{Prefix: "script_value", Name: "max_soldiers"}, true},
		{"location:stockholm", Cited{Prefix: "location", Name: "stockholm"}, true},
		{"c:SWE", Cited{Prefix: "c", Name: "SWE"}, true},
		{"english", Cited{}, false},
		{"capital_province.culture", Cited{}, false},
		{"$COA$", Cited{}, false},
		{"situation:foo.var:x", Cited{Prefix: "situation", Name: "foo", Chain: "var:x"}, true},
		{"s:STATE_KENYA.region_state:OMA", Cited{Prefix: "s", Name: "STATE_KENYA", Chain: "region_state:OMA"}, true},

		// a prefix is one plain identifier: a dotted word is a scope path whose later step is the cite
		{"$SCOPE$.var:x", Cited{}, false},
		{"root.var:x", Cited{}, false},
		{"this.culture:english", Cited{}, false},

		// script-made names are cited like any other, and the old engine's hand list of refused prefixes is gone
		{"scope:foo", Cited{Prefix: "scope", Name: "foo"}, true},
		{"var:gold", Cited{Prefix: "var", Name: "gold"}, true},
		{"scope:a.var:b", Cited{Prefix: "scope", Name: "a", Chain: "var:b"}, true},
		{"type:character_event", Cited{Prefix: "type", Name: "character_event"}, true},
		{"scope:my_target.culture", Cited{Prefix: "scope", Name: "my_target", Chain: "culture"}, true},

		// a number is a legal name, and so is one that is not ASCII
		{"character:186004", Cited{Prefix: "character", Name: "186004"}, true},
		{"province:1234", Cited{Prefix: "province", Name: "1234"}, true},
		{"scope:Linnéa", Cited{Prefix: "scope", Name: "Linnéa"}, true},
		{"p:xB0D0E0.state.owner", Cited{Prefix: "p", Name: "xB0D0E0", Chain: "state.owner"}, true},

		// a define names its group and its key
		{"define:NGame|START_DATE", Cited{Prefix: "define", Name: "NGame", Member: "START_DATE"}, true},
		{"define:NGame|", Cited{}, false},

		// not a cite: a sound's name, a file, a path, an entry mode, and the broken spellings
		{"event:/SFX/UI/Generic/sfx_ui_click", Cited{}, false},
		{"file:/music/my_new_song.ogg", Cited{}, false},
		{"C:/Games/x.dds", Cited{}, false},
		{`C:\Games\x.dds`, Cited{}, false},
		{"INJECT:my_building", Cited{}, false},
		{"REPLACE_OR_CREATE:my_building", Cited{}, false},
		{"", Cited{}, false},
		{"scope", Cited{}, false},
		{"scope:", Cited{}, false},
		{":x", Cited{}, false},
		{"1a:x", Cited{}, false},
		{"_a:x", Cited{}, false},
		{"scope:a b", Cited{}, false},
		{"scope:a.", Cited{}, false},
	} {
		got, ok := Cite(c.text)
		if ok != c.ok || got != c.want {
			t.Errorf("Cite(%q) = %+v, %v; want %+v, %v", c.text, got, ok, c.want, c.ok)
		}
	}
}

func TestMode(t *testing.T) {
	for _, c := range []struct{ key, mode, name string }{
		{"INJECT:my_building", "INJECT", "my_building"},
		{"REPLACE:my_building", "REPLACE", "my_building"},
		{"TRY_INJECT:x", "TRY_INJECT", "x"},
		{"REPLACE_OR_CREATE:x", "REPLACE_OR_CREATE", "x"},

		// a word the registry has never heard of still splits, so the learner can report it as drift
		{"APPEND:x", "APPEND", "x"},

		// not a mode
		{"my_building", "", "my_building"},
		{"culture:english", "", "culture:english"},
		{"Inject:x", "", "Inject:x"},
		{"INJECT:", "", "INJECT:"},
		{":x", "", ":x"},
		{"", "", ""},
	} {
		if mode, name := Mode(c.key); mode != c.mode || name != c.name {
			t.Errorf("Mode(%q) = %q, %q; want %q, %q", c.key, mode, name, c.mode, c.name)
		}
	}
}

func TestConstant(t *testing.T) {
	for _, c := range []struct {
		text, name string
		ok         bool
	}{
		{"@gold_cost", "gold_cost", true},
		{"@[gold_cost * 2]", "", true},
		{"@", "", false},
		{"gold_cost", "", false},
		{"", "", false},
	} {
		if name, ok := Constant(c.text); name != c.name || ok != c.ok {
			t.Errorf("Constant(%q) = %q, %v; want %q, %v", c.text, name, ok, c.name, c.ok)
		}
	}
}

func TestInterpolations(t *testing.T) {
	for _, c := range []struct {
		text string
		want []Span
	}{
		// the old engine's rows: a parameter at an offset, and a plain scalar
		{"$TRAIT$", []Span{{"TRAIT", 0, 7}}},
		{"brave", nil},

		{"scope:$TARGET$", []Span{{"TARGET", 6, 14}}},
		{"$A$_and_$B_2$", []Span{{"A", 0, 3}, {"B_2", 8, 13}}},
		{"cost is $VALUE$ gold", []Span{{"VALUE", 8, 15}}},

		// a dollar with no partner, two dollars with nothing between, and a name that is not word characters
		{"$", nil},
		{"$$", nil},
		{"$TRAIT", nil},
		{"100$ and 200$", nil},
		{"$A$$B$", []Span{{"A", 0, 3}, {"B", 3, 6}}},
	} {
		if got := Interpolations(c.text); !reflect.DeepEqual(got, c.want) {
			t.Errorf("Interpolations(%q) = %+v; want %+v", c.text, got, c.want)
		}
	}
}
