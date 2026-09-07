// snippets_test.go covers the derived skeletons.

package catalog

import (
	"testing"

	"paradox-modding-tools/services/internal/parser/jomini"
)

func defsOf(kind string, n int) []Def {
	out := make([]Def, n)
	for i := range out {
		out[i] = Def{Kind: kind, Key: kind + string(rune('a'+i))}
	}
	return out
}

// TestDeriveSnippetsNeedsEvidence pins both floors. A shape is only a shape when
// enough definitions exist to judge it, and a member only belongs when most of
// them carry it — every member vanilla uses once would otherwise land in the
// skeleton, which is the failure mode this floor exists to stop.
func TestDeriveSnippetsNeedsEvidence(t *testing.T) {
	structures := map[string]map[string]int{
		"traits": {
			"diplomacy":  10, // every trait
			"martial":    6,  // most
			"desc":       2,  // a couple — noise
			"icon":       10,
			"ai_honor":   5, // exactly half, which clears the floor
			"rare_thing": 1,
		},
		// Below minSnippetDefs: no shape can be judged from three definitions.
		"tiny_kind": {"a": 3, "b": 3},
	}
	blocks := map[string]map[string]int{
		"traits": {"icon": 9},
	}
	defs := append(defsOf("traits", 10), defsOf("tiny_kind", 3)...)

	got := deriveSnippets(structures, blocks, defs)
	if _, ok := got["tiny_kind"]; ok {
		t.Errorf("tiny_kind has only 3 definitions; no shape should be derived")
	}
	members := got["traits"]
	if len(members) == 0 {
		t.Fatal("no traits skeleton")
	}
	have := map[string]SnippetMember{}
	for _, m := range members {
		have[m.Key] = m
	}
	for _, want := range []string{"diplomacy", "icon", "martial", "ai_honor"} {
		if _, ok := have[want]; !ok {
			t.Errorf("%s clears the coverage floor but is missing", want)
		}
	}
	for _, unwanted := range []string{"desc", "rare_thing"} {
		if _, ok := have[unwanted]; ok {
			t.Errorf("%s is below the floor and should not be in the skeleton", unwanted)
		}
	}
	// Written as a block more often than not, so the skeleton nests it.
	if !have["icon"].Block {
		t.Errorf("icon is a block in 9 of 10 uses; want Block")
	}
	if have["diplomacy"].Block {
		t.Errorf("diplomacy is never a block; want scalar")
	}
	// Most-used first is the closest thing the install gives to "required".
	if members[0].Key != "diplomacy" && members[0].Key != "icon" {
		t.Errorf("members[0] = %q, want the most-used member first", members[0].Key)
	}
}

// TestDeriveSnippetsCaps keeps a skeleton readable: a decision has dozens of
// members past the floor and a wall of them teaches nothing.
func TestDeriveSnippetsCaps(t *testing.T) {
	counts := map[string]int{}
	for i := 0; i < maxSnippetMembers*3; i++ {
		counts[string(rune('a'+i%26))+string(rune('a'+i/26))] = 20
	}
	got := deriveSnippets(map[string]map[string]int{"decisions": counts}, nil,
		defsOf("decisions", 20))
	if n := len(got["decisions"]); n != maxSnippetMembers {
		t.Errorf("skeleton has %d members, want the cap of %d", n, maxSnippetMembers)
	}
}

// TestHarvestNestedMembers pins the fix for a completion that offered an event's
// top-level keys no matter how deep the cursor was. The blocks worth knowing the
// inside of are exactly the ones the kind-vocabulary stoplist excludes.
func TestHarvestNestedMembers(t *testing.T) {
	src := "ns.1 = {\n" +
		"\ttype = character_event\n" +
		"\tdesc = {\n" +
		"\t\tfirst_valid = {\n" +
		"\t\t\ttriggered_desc = {\n" +
		"\t\t\t\ttrigger = { is_ai = yes }\n" +
		"\t\t\t\tdesc = a.desc\n" +
		"\t\t\t}\n" +
		"\t\t\tdesc = b.desc\n" +
		"\t\t}\n" +
		"\t}\n" +
		"}\n"
	_, _, vocab, nested := harvestStructVocab(jomini.Parse(jomini.Normalize(src)).Root)

	// first_valid, triggered_desc and limit are stoplisted out of a kind's
	// member vocabulary on purpose — and they are precisely the parents this
	// table exists for.
	if got := nested["triggered_desc"]; got["trigger"] == 0 || got["desc"] == 0 {
		t.Errorf("triggered_desc members = %v, want trigger and desc", got)
	}
	if got := nested["first_valid"]; got["triggered_desc"] == 0 {
		t.Errorf("first_valid members = %v, want triggered_desc", got)
	}
	if got := nested["desc"]; got["first_valid"] == 0 {
		t.Errorf("desc members = %v, want first_valid", got)
	}
	// A value is never a key, so it must not become one.
	if _, bad := nested["is_ai"]["yes"]; bad {
		t.Errorf("yes recorded as a key inside is_ai")
	}
	// The kind vocabulary is unchanged: control flow stays out of it.
	if vocab["first_valid"] || vocab["triggered_desc"] {
		t.Errorf("stoplisted keys leaked into the kind vocabulary: %v", vocab)
	}
}

// TestDeriveAmbiguousFields pins what makes a name-keyed description
// untrustworthy: the same field name used inside several different blocks.
func TestDeriveAmbiguousFields(t *testing.T) {
	got := deriveAmbiguousFields(map[string][]string{
		"triggered_desc": {"trigger", "desc"},
		"left_portrait":  {"trigger", "character"},
		"option":         {"name"},
	})
	if !got["trigger"] {
		t.Error("trigger is used in two blocks; want ambiguous")
	}
	for _, unambiguous := range []string{"desc", "character", "name"} {
		if got[unambiguous] {
			t.Errorf("%s appears in one block only; want unambiguous", unambiguous)
		}
	}
}
