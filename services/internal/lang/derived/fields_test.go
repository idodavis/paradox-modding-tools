// fields_test.go covers the derivations that read a convention out of the
// corpus and the evidence floors that keep a coincidence from becoming one:
// field typing (a field takes one def kind, or a small closed set of scalars,
// and mods overlay vanilla) and localization naming conventions.

package derived

import (
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"testing"

	"paradox-modding-tools/services/internal/lang/declared"
	"paradox-modding-tools/services/internal/lang/loc"
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

// Localization conventions are read from the corpus, not chosen from a list.
// The predecessor voted between `id`, `id_desc` and `kind_id` and kept one
// winner per kind, so Victoria 3's `ACHIEVEMENT_DESC_<id>` and
// `notification_<id>_tooltip` were defined-but-never-cited and every one of
// them reported as an orphaned key.
func TestDeriveLocAffixes(t *testing.T) {
	var defs []Def
	locKeys := map[string]bool{}
	for i := range 10 {
		id := "achievement_" + string(rune('a'+i))
		defs = append(defs, Def{Kind: "achievements", Key: id})
		locKeys["ACHIEVEMENT_"+id] = true
		if i < 9 {
			locKeys["ACHIEVEMENT_DESC_"+id] = true
		}
	}
	// One definition with a shape nobody else shares is a coincidence.
	locKeys["SPECIAL_achievement_a_thing"] = true
	// A key that names nothing at all must contribute no convention.
	locKeys["some_unrelated_prose_key"] = true

	got := deriveLocAffixes(newDefIndex(defs), locKeys)["achievements"]
	want := []loc.Affix{
		{Pre: "ACHIEVEMENT_", Defs: 10, Of: 10},
		{Pre: "ACHIEVEMENT_DESC_", Defs: 9, Of: 10},
	}
	if len(got) != len(want) {
		t.Fatalf("affixes = %+v, want %+v", got, want)
	}
	for i, a := range got {
		if a != want[i] {
			t.Errorf("affix %d = %+v, want %+v", i, a, want[i])
		}
	}

	// Best evidence first, so a hover reading the first hit gets the name.
	if got[0].Coverage() != 100 || got[1].Coverage() != 90 {
		t.Errorf("coverage = %d/%d", got[0].Coverage(), got[1].Coverage())
	}
}

// The two questions asked of a convention are not equally demanding, so the
// floor is the caller's to choose: suppressing a false "orphaned" row needs the
// shape to be plausible, while warning a modder that a key is missing asserts
// the game requires it.
func TestConventionKeysCoverageFloor(t *testing.T) {
	affixes := []loc.Affix{
		{Pre: "ACHIEVEMENT_", Defs: 10, Of: 10}, // every definition: required
		{Suf: "_btn3", Defs: 9, Of: 10},         // nine in ten: a habit
		{Suf: "_hint", Defs: 3, Of: 10},         // plausible only
	}
	cases := []struct {
		name     string
		coverage int
		want     []string
	}{
		{"used takes every plausible shape", loc.ConventionUsed,
			[]string{"ACHIEVEMENT_x", "x_btn3", "x_hint"}},
		// Nine in ten is the case that matters. Anything short of every
		// definition means vanilla itself lacks the key somewhere, so demanding
		// it warns about correct script — EU5 gives 90% of diplomatic actions a
		// `_ACTION_BTN3` key, and an action with two buttons is not defective.
		{"required takes only the universal one", loc.ConventionRequired,
			[]string{"ACHIEVEMENT_x"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := loc.ConventionKeys("x", affixes, tc.coverage)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// loc.ConventionOwner is the reverse of loc.ConventionKeys and the reason none of this
// is stored as references: it answers "is this key consumed?" for the few keys
// that look orphaned, instead of expanding every convention over every
// definition to answer it in advance.
func TestConventionOwner(t *testing.T) {
	affixes := map[string][]loc.Affix{
		"achievements": {{Pre: "ACHIEVEMENT_", Defs: 10, Of: 10}},
		"messages":     {{Pre: "notification_", Suf: "_tooltip", Defs: 9, Of: 10}},
	}
	kinds := map[string][]string{
		"achievement_a": {"achievements"},
		"war_declared":  {"messages"},
		"long":          {"achievements"},
	}
	defKinds := func(id string, visit func(string) bool) {
		for _, k := range kinds[id] {
			if !visit(k) {
				return
			}
		}
	}
	cases := []struct {
		name, key, wantID, wantKind string
	}{
		{"prefix convention", "ACHIEVEMENT_achievement_a", "achievement_a", "achievements"},
		{"prefix and suffix", "notification_war_declared_tooltip", "war_declared", "messages"},
		// The span is a definition and the affix is a real convention, but of
		// the wrong kind — the message convention says nothing about
		// achievements.
		{"convention belongs to another kind", "notification_achievement_a_tooltip", "", ""},
		{"shape nothing declares", "ACHIEVEMENT_DESC_achievement_a", "", ""},
		{"names nothing", "ACHIEVEMENT_something_else", "", ""},
		// Longest span first: `long` is a definition, but taking it would
		// report the wrong owner for a key that is not a convention at all.
		{"no owner beats a short accidental span", "a_long_prose_key", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id, kind, ok := loc.ConventionOwner(affixes, tc.key, defKinds)
			if ok != (tc.wantID != "") || id != tc.wantID || kind != tc.wantKind {
				t.Fatalf("got (%q, %q, %v), want (%q, %q, %v)",
					id, kind, ok, tc.wantID, tc.wantKind, tc.wantID != "")
			}
		})
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

// loc.ConventionOwner runs over every key that survived every other orphan filter —
// tens of thousands on a total conversion, once per key — so its cost is per
// key, not per span of a key. Written the obvious way it built its visitor
// inside the span loop and paid 22 allocations and 448ns to answer one key;
// hoisting the closure and giving the token offsets a fixed buffer took that to
// 4 and 155ns. The four are the closure and the three strings it reads.
func BenchmarkConventionOwner(b *testing.B) {
	affixes := map[string][]loc.Affix{
		"achievements": {{Pre: "ACHIEVEMENT_", Defs: 141, Of: 141}},
		"messages": {
			{Pre: "notification_", Suf: "_tooltip", Defs: 446, Of: 469},
			{Pre: "notification_", Suf: "_name", Defs: 460, Of: 469},
		},
		"state_regions": {{Pre: "HUB_NAME_", Suf: "_farm", Defs: 675, Of: 781}},
	}
	defs := map[string][]string{
		"war_declared": {"messages"},
		"maine":        {"state_regions"},
	}
	eachKind := func(id string, visit func(string) bool) {
		for _, k := range defs[id] {
			if !visit(k) {
				return
			}
		}
	}
	// A hit, a near-miss that decomposes but matches no convention, and a key
	// that names nothing — the three shapes the orphan pass actually sees.
	keys := []string{
		"notification_war_declared_tooltip",
		"notification_maine_tooltip",
		"some_ordinary_prose_key_nobody_owns",
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		loc.ConventionOwner(affixes, keys[i%len(keys)], eachKind)
	}
}

// A CK3 game rule is a definition and gets `rule_<id>`, which the definition
// pass covers. Its options are nested one level inside it and never become
// definitions — yet vanilla writes 590 `setting_<option>` / `setting_<option>_desc`
// keys for its 380 options, and every one of them read as an orphaned key.
func TestDeriveLocMemberAffixes(t *testing.T) {
	structures := map[string][]string{"game_rules": {}}
	locKeys := map[string]bool{}
	for i := range 10 {
		opt := "opt_" + string(rune('a'+i))
		structures["game_rules"] = append(structures["game_rules"], opt)
		locKeys["setting_"+opt] = true
		locKeys["setting_"+opt+"_desc"] = true
	}
	// Ordinary field names sit in the same set and carry no convention.
	structures["game_rules"] = append(structures["game_rules"], "default", "categories")

	got := loc.DeriveMemberAffixes(structures, locKeys)["game_rules"]
	var shapes []string
	for _, a := range got {
		shapes = append(shapes, a.Key("<opt>"))
	}
	slices.Sort(shapes)
	want := []string{"setting_<opt>", "setting_<opt>_desc"}
	if !slices.Equal(shapes, want) {
		t.Fatalf("member affixes = %v, want %v", shapes, want)
	}

	// And the reverse lookup finds the option a key names, including one the
	// install never saw — a mod's new rule option is exactly that case.
	isMember := func(kind, name string) bool {
		return kind == "game_rules" &&
			(name == "opt_a" || name == "suf_quieter")
	}
	cases := []struct{ key, wantName string }{
		{"setting_opt_a", "opt_a"},
		{"setting_suf_quieter_desc", "suf_quieter"},
		{"setting_not_an_option", ""},
		{"unrelated_prose_key", ""},
	}
	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			name, _, ok := loc.MemberConventionOwner(
				map[string][]loc.Affix{"game_rules": got}, tc.key, isMember)
			if ok != (tc.wantName != "") || name != tc.wantName {
				t.Fatalf("got (%q, %v), want %q", name, ok, tc.wantName)
			}
		})
	}
}

// A convention that covers only a slice of its kind is still a convention.
// CK3 keys portrait modifiers `PORTRAIT_MODIFIER_custom_<group>_<accessory>`,
// one prefix per group, so each covers about a tenth of the `accessories` kind
// and none clears a fifth — 994 orphans on A Game of Thrones. What separates
// these from noise is not their share but how many definitions share them.
func TestDeriveLocAffixesAdmitsSliceConventions(t *testing.T) {
	var defs []Def
	locKeys := map[string]bool{}
	// 200 accessories in two groups of 100: each prefix covers half — no, a
	// quarter, once the two groups and the plain half are counted.
	for i := range 200 {
		id := fmt.Sprintf("acc_%03d", i)
		defs = append(defs, Def{Kind: "accessories", Key: id})
		switch {
		case i < 60:
			locKeys["PORTRAIT_MODIFIER_clothes_"+id] = true
		case i < 120:
			locKeys["PORTRAIT_MODIFIER_headgear_"+id] = true
		}
	}
	// A shape only a handful share stays out; the absolute floor is not a way
	// around the evidence requirement.
	for i := range 12 {
		locKeys[fmt.Sprintf("ODD_acc_%03d", i)] = true
	}

	got := deriveLocAffixes(newDefIndex(defs), locKeys)["accessories"]
	var shapes []string
	for _, a := range got {
		shapes = append(shapes, a.Key("<id>"))
	}
	slices.Sort(shapes)
	want := []string{
		"PORTRAIT_MODIFIER_clothes_<id>", "PORTRAIT_MODIFIER_headgear_<id>",
	}
	if !slices.Equal(shapes, want) {
		t.Fatalf("affixes = %v, want %v", shapes, want)
	}
	// 60 of 200 is 30%, below nothing here — the point is that it is admitted
	// on count, and Coverage still reports the honest share.
	for _, a := range got {
		if a.Coverage() != 30 {
			t.Errorf("%+v coverage = %d, want the true share", a, a.Coverage())
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
		LocAffixes:      map[string][]loc.Affix{"traits": {{Suf: "_desc"}}},
		LocFields:       map[string]bool{"name": true},
		LocListFields:   map[string]bool{"names": true},
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
