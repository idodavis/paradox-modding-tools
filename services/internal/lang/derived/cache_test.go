// cache_test.go covers the predicates and conversions that live beside the persisted shapes.

package derived

import "testing"

// The rule, not a count: what counts as a citable definition is a property of the kind sentinels, so it survives a patch that renames every folder.
func TestIsCitableDef(t *testing.T) {
	for _, c := range []struct {
		name string
		def  Def
		want bool
	}{
		{"an ordinary object", Def{Kind: "traits", Key: "brave"}, true},
		{"a game-derived kind nobody enumerated", Def{Kind: "harvest_condition_types", Key: "drought"}, true},
		{"an event", Def{Kind: "event", Key: "agot.1001"}, true},
		{"a GUI template", Def{Kind: "gui_type", Key: "widget"}, true},
		// A namespace occupies the name. IsObjectKind rejects it so a hover does not land on an id prefix; this predicate keeps it so a derivation does not treat the name as free and invent a database for it.
		{"an event namespace", Def{Kind: "namespace", Key: "agot"}, true},
		{"a kind written with spaces", Def{Kind: "casus belli types", Key: "cb_holy_war"}, true},

		{"a localization key", Def{Kind: "loc_key", Key: "brave"}, false},
		{"a saved scope", Def{Kind: "saved_scope", Key: "target"}, false},
		{"a script parameter", Def{Kind: "script_param", Key: "TYPE"}, false},
		{"a flag", Def{Kind: "flag", Key: "seen_intro"}, false},
		{"a nameless def", Def{Kind: "traits", Key: ""}, false},
		{"a kindless def", Def{Kind: "", Key: "brave"}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := IsCitableDef(c.def); got != c.want {
				t.Errorf("IsCitableDef(%+v) = %v, want %v", c.def, got, c.want)
			}
		})
	}
}

// TestEraDate uses A Game of Thrones' real offset: it writes start_date = 8082.1.1 for what the game shows as 300 AC. With no calendar declared nothing anywhere changes -- guessing an offset from bookmark dates would be a guess dressed as a fact.
func TestEraDate(t *testing.T) {
	agot := ModCalendar{Era: "AC", EpochOffset: 7782}
	for _, c := range []struct {
		in   string
		cal  ModCalendar
		want string
	}{
		{"8082.1.1", agot, "300.1.1 AC"},
		{"8209.2.1", agot, "427.2.1 AC"},
		{"8209.2.1.12", agot, "427.2.1.12 AC"},
		// Below the epoch: "year -12" is worse than no conversion.
		{"7000.1.1", agot, ""},
		{"7782.1.1", agot, ""},
		// Not a date.
		{"brave", agot, ""},
		{"ns.1", agot, ""},
		{"100", agot, ""},
		// Undeclared: vanilla CK3's 867 needs no help.
		{"867.1.1", ModCalendar{}, ""},
		// An era with no offset still counts: a mod may simply want its years labelled.
		{"300.1.1", ModCalendar{Era: "AC"}, "300.1.1 AC"},
	} {
		if got := EraDate(c.in, c.cal); got != c.want {
			t.Errorf("EraDate(%q, %+v) = %q, want %q", c.in, c.cal, got, c.want)
		}
	}
	if (ModCalendar{}).Declared() {
		t.Error("an empty calendar must not read as declared")
	}
}
