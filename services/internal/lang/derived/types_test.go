// types_test.go covers the predicates that live beside the persisted shapes.

package derived

import "testing"

// The rule, not a count: what counts as a citable definition is a property of
// the kind sentinels, so it survives a patch that renames every folder.
func TestIsCitableDef(t *testing.T) {
	cases := []struct {
		name string
		def  Def
		want bool
	}{
		{"an ordinary object", Def{Kind: "traits", Key: "brave"}, true},
		{"a game-derived kind nobody enumerated", Def{Kind: "harvest_condition_types", Key: "drought"}, true},
		{"an event", Def{Kind: "event", Key: "agot.1001"}, true},
		{"a GUI template", Def{Kind: "gui_type", Key: "widget"}, true},
		// A namespace occupies the name. IsObjectKind rejects it so a hover does
		// not land on an id prefix; this predicate keeps it so a derivation does
		// not treat the name as free and invent a database for it.
		{"an event namespace", Def{Kind: "namespace", Key: "agot"}, true},
		{"a kind written with spaces", Def{Kind: "casus belli types", Key: "cb_holy_war"}, true},

		{"a localization key", Def{Kind: "loc_key", Key: "brave"}, false},
		{"a saved scope", Def{Kind: "saved_scope", Key: "target"}, false},
		{"a script parameter", Def{Kind: "script_param", Key: "TYPE"}, false},
		{"a flag", Def{Kind: "flag", Key: "seen_intro"}, false},
		{"a nameless def", Def{Kind: "traits", Key: ""}, false},
		{"a kindless def", Def{Kind: "", Key: "brave"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsCitableDef(c.def); got != c.want {
				t.Errorf("IsCitableDef(%+v) = %v, want %v", c.def, got, c.want)
			}
		})
	}
}
