// fields_test.go covers the derivations that read a convention out of the
// corpus and the evidence floors that keep a coincidence from becoming one:
// field typing (a field takes one def kind, or a small closed set of scalars,
// and mods overlay vanilla) and which script properties hold a localization key.

package derived

import (
	"reflect"
	"regexp"
	"testing"

	"paradox-modding-tools/services/internal/lang/declared"
)

func TestDeriveFieldValueKinds(t *testing.T) {
	defs := []Def{
		{Kind: "event_theme", Key: "seduction"},
		{Kind: "event_theme", Key: "intrigue"},
		{Kind: "trait", Key: "brave"},
	}
	ok := deriveFieldValueKinds(map[string]map[string]bool{
		"theme": {"seduction": true, "intrigue": true},
	}, newDefIndex(defs))
	if ok["theme"] != "event_theme" {
		t.Fatalf("theme = %v", ok)
	}
	conflict := deriveFieldValueKinds(map[string]map[string]bool{
		"theme": {"seduction": true, "brave": true},
	}, newDefIndex(defs))
	if _, hit := conflict["theme"]; hit {
		t.Fatalf("conflict should omit theme: %v", conflict)
	}
}

// A value naming two kinds is no evidence and must not be a veto: 100 is a CK3 character AND a province, and that silenced `holder` and 116 more fields entirely.
func TestDeriveFieldValueKindsIgnoresAmbiguousValues(t *testing.T) {
	defs := []Def{
		{Kind: "characters", Key: "100"}, {Kind: "provinces", Key: "100"},
		{Kind: "characters", Key: "101"}, {Kind: "characters", Key: "102"},
	}
	got := deriveFieldValueKinds(map[string]map[string]bool{
		"holder": {"100": true, "101": true, "102": true},
	}, newDefIndex(defs))
	if got["holder"] != "characters" {
		t.Errorf("holder = %q, want characters (one shared id is not a veto)", got["holder"])
	}
}

// A mod that localizes its own objects must not lose the field types that name
// them, and a localization key must never BE a field type.
//
// Every object has a caption, so in a workspace with loc files most def keys are
// also loc keys. Admitting loc_key defs here made those keys ambiguous, and an
// ambiguous key drops the whole field: on A Game of Thrones 111 correct types
// went silent (`add_innovation` -> innovations, `add_perk` -> lifestyle_perks)
// while 183 fields were typed `loc_key` instead — among them `has_policy` and
// `add_random_innovation`, which the game DECLARES as tokens. Whether a field
// holds a caption is deriveLocFields' question and it refuses exactly those,
// because it checks declared.Schema.HasName first. See plan I.2f.
func TestDeriveFieldValueKindsIgnoresLocalization(t *testing.T) {
	defs := []Def{
		{Kind: "innovations", Key: "innovation_bannus"},
		{Kind: "innovations", Key: "innovation_motte"},
		// The mod's own localization of those two, plus an unrelated caption.
		{Kind: "loc_key", Key: "innovation_bannus"},
		{Kind: "loc_key", Key: "innovation_motte"},
		{Kind: "loc_key", Key: "my_tooltip_a"},
		{Kind: "loc_key", Key: "my_tooltip_b"},
	}
	got := deriveFieldValueKinds(map[string]map[string]bool{
		"add_innovation": {"innovation_bannus": true, "innovation_motte": true},
		"custom_tooltip": {"my_tooltip_a": true, "my_tooltip_b": true},
	}, newDefIndex(defs))
	if got["add_innovation"] != "innovations" {
		t.Errorf("add_innovation = %q, want innovations (localizing an object"+
			" must not untype the field that names it)", got["add_innovation"])
	}
	if k, hit := got["custom_tooltip"]; hit {
		t.Errorf("custom_tooltip typed %q; loc keys are deriveLocFields' job", k)
	}
}

// One coincidence types nothing. CK3's `body_part` takes exactly `head` and
// `torso`; `head` also names an artifact visual, so 1 of 2 cleared the 50%
// coverage ratio exactly and every `body_part = torso` became a dangling
// `visuals` reference — 859 on one mod. Across the three installs every field
// typed on a single hit was junk.
func TestDeriveFieldValueKindsNeedsCorroboration(t *testing.T) {
	defs := []Def{
		{Kind: "visuals", Key: "head"},
		{Kind: "visuals", Key: "helmet"},
	}
	// 1 of 2 values resolves: clears the ratio, but rests on one coincidence.
	if got := deriveFieldValueKinds(map[string]map[string]bool{
		"body_part": {"head": true, "torso": true},
	}, newDefIndex(defs)); got["body_part"] != "" {
		t.Fatalf("body_part typed on a single hit: %v", got)
	}
	// A second corroborating value is enough.
	if got := deriveFieldValueKinds(map[string]map[string]bool{
		"artifact_visual": {"head": true, "helmet": true},
	}, newDefIndex(defs)); got["artifact_visual"] != "visuals" {
		t.Fatalf("artifact_visual = %v, want visuals", got)
	}
}

// Unanimity is not evidence. Values that resolve to nothing are skipped, so
// without a coverage floor one coincidental hit typed the whole field — and
// every later use of it became a reference of that kind which could never
// resolve. On vanilla, `has_cultural_parameter` was typed `ep_3` on 1 of 432
// values and `remove_variable` was typed `achievements` on 2 of 392.
func TestDeriveFieldValueKindsNeedsCoverage(t *testing.T) {
	defs := []Def{{Kind: "achievements", Key: "some_achievement"}}
	thin := map[string]map[string]bool{"remove_variable": {"some_achievement": true}}
	for i := range 30 {
		thin["remove_variable"][string(rune('a'+i%26))+"_var"+string(rune('0'+i/26))] = true
	}
	if got := deriveFieldValueKinds(thin, newDefIndex(defs)); got["remove_variable"] != "" {
		t.Errorf("typed on 1 of %d values: %q", len(thin["remove_variable"]), got["remove_variable"])
	}

	// A field whose values really are that kind still types.
	rich := map[string]map[string]bool{"give_achievement": {}}
	var many []Def
	for i := range 10 {
		key := "ach_" + string(rune('a'+i))
		many = append(many, Def{Kind: "achievements", Key: key})
		rich["give_achievement"][key] = true
	}
	if got := deriveFieldValueKinds(rich, newDefIndex(many)); got["give_achievement"] != "achievements" {
		t.Errorf("10/10 coverage did not type the field: %v", got)
	}
}

// harvestGetSet replaced a regex that ran over every byte of every script file.
// The scanner must agree with the pattern it replaced, boundaries included.
func TestHarvestGetSetMatchesPattern(t *testing.T) {
	re := regexp.MustCompile(`\b((?:Get|Set)[A-Za-z0-9_]+)\b`)
	for _, src := range []string{
		"",
		"Get",
		"GetName",
		"[GetPlayer.GetName]",
		"desc = \"[ROOT.Char.GetTitledFirstName]\"",
		"xGetName and _SetFoo and forgetName",
		"SetVariable = { name = x } get_lower = 1",
		"GetName2|E] [SetY_1]",
		"Set",
		"GetA.SetB.GetC_9",
	} {
		want := map[string]bool{}
		for _, m := range re.FindAllStringSubmatch(src, -1) {
			want[m[1]] = true
		}
		got := harvestGetSet(src)
		if len(got) != len(want) {
			t.Errorf("harvestGetSet(%q) = %v, regex = %v", src, got, want)
			continue
		}
		for k := range want {
			if !got[k] {
				t.Errorf("harvestGetSet(%q) missing %q (regex = %v)", src, k, want)
			}
		}
	}
}

// Which properties hold a localization key is partly declared (parser/loc lists
// the engine's own — `desc`, `title`, `custom_tooltip`) and partly not. Victoria
// 3 character templates write `last_name = Addams` against a real key, so a
// mod's localized surnames were defined, consumed by the game, and reported as
// orphaned.
func TestDeriveLocFields(t *testing.T) {
	locKeys := map[string]bool{}
	surnames := map[string]bool{}
	for i := range 10 {
		n := "surname_" + string(rune('a'+i))
		surnames[n] = true
		locKeys[n] = true
	}
	// An object reference field. Every culture has a key named after it, so
	// every value here is a key too — and none of that makes `culture` a
	// property that holds prose. Skipping values that name a definition is what
	// separates the two; without it CK3 elected 323 fields, `has_trait` and
	// `capital` among them, and doubled the reference table.
	culture := map[string]bool{}
	defKeys := map[string]bool{}
	for i := range 10 {
		c := "culture_" + string(rune('a'+i))
		culture[c] = true
		defKeys[c] = true
		locKeys[c] = true
	}
	// Enough coverage but not enough evidence: three of three.
	thin := map[string]bool{"surname_a": true, "surname_b": true, "surname_c": true}
	// A declared trigger. Its values are law ids, which are not harvested as
	// definitions here but do have keys named after them — so the corpus alone
	// says "this field holds localization keys" and the corpus alone is wrong.
	schema := &declared.Schema{Triggers: map[string]declared.EngineToken{"has_realm_law": {}}}

	got := deriveLocFields(map[string]map[string]bool{
		"last_name":       surnames,
		"culture":         culture,
		"rare_title_slot": thin,
		"has_realm_law":   surnames,
		"has_$TYPE$_law":  surnames,
		// Already declared by the engine list; deriving it again would say
		// nothing and would let a broad derivation weaken a strict declaration.
		"desc": surnames,
	}, locKeys, defKeys, schema)

	if !got["last_name"] {
		t.Error("last_name holds ten keys and was not derived")
	}
	for _, field := range []string{
		"culture", "rare_title_slot", "desc", "has_realm_law", "has_$TYPE$_law",
	} {
		if got[field] {
			t.Errorf("%s should not be a derived loc field: %v", field, got)
		}
	}
}

// TestMergeLocalCoversEveryDerivedField walks Derived by reflection so a field
// added later cannot be silently left out of mergeLocal.
//
// KindScope was: Derived had nine fields and mergeLocal handled eight. It and
// PrefixKinds come out of one bindKinds call and are written on adjacent lines,
// so the omission was invisible by reading. The consequence was not a missing
// map but a silent behaviour change — session/scope.go treats an empty
// KindScope as "do not filter", so scope checking turned itself off whenever
// `rebind` hit one of its early returns.
//
// Written as reflection rather than a field list because a field list is the
// same thing that already drifted.
func TestMergeLocalCoversEveryDerivedField(t *testing.T) {
	// A local with every field non-zero, so anything mergeLocal skips stays
	// zero in the destination and names itself.
	local := &Derived{
		PrefixKinds:     map[string]string{"p": "provinces"},
		KindScope:       map[string]string{"provinces": "province"},
		FireKeys:        map[string]string{"trigger_event": "event"},
		NestedShapes:    []NestedShape{{ParentKind: "game_rules", ChildKind: "game_rule"}},
		Wrappers:        map[string]bool{"COUNTRIES": true},
		FieldValueKinds: map[string]string{"has_trait": "traits"},
		LocFields:       map[string]bool{"name": true},
	}
	dst := derivedFromCache(nil)
	dst.mergeLocal(local)

	rv := reflect.ValueOf(*dst)
	rt := rv.Type()
	for i := range rt.NumField() {
		if rv.Field(i).Len() == 0 {
			t.Errorf("mergeLocal does not carry %s: a field added to Derived must "+
				"be merged, or it silently vanishes on the incremental path",
				rt.Field(i).Name)
		}
	}
}
