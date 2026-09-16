// derive_test.go covers the facts no dump states, read from the corpus instead,
// and the evidence floors that keep a coincidence from becoming one.

package derived

import (
	"regexp"
	"slices"
	"testing"

	"paradox-modding-tools/services/internal/lang/declared"
	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/loc"
)

// Which templates a kind uses is read, not listed; a shape only half the kind shares is a habit.
func TestDeriveLocConventions(t *testing.T) {
	var defs []Def
	locKeys := map[string]bool{}
	for i := range 10 {
		id := "trait_" + string(rune('a'+i))
		defs = append(defs, Def{Kind: "traits", Key: id})
		locKeys[id] = true
		locKeys[id+"_desc"] = true
		if i < 5 {
			locKeys["trait_"+id] = true // half the kind: a habit
		}
	}
	for i := range 10 {
		id := "k_" + string(rune('a'+i))
		defs = append(defs, Def{Kind: "landed_titles", Key: id})
		locKeys[id] = true
		locKeys[id+"_adj"] = true
	}
	structures := map[string][]string{"landed_titles": {"adj", "color"}}

	got := deriveLocConventions(newDefIndex(defs), structures, loc.NewKeys(locKeys))
	for _, tc := range []struct {
		kind string
		want []loc.Template
	}{
		{"traits", []loc.Template{"{id}", "{id}_desc"}},
		{"landed_titles", []loc.Template{"{id}", "{id}_adj"}},
	} {
		var names []loc.Template
		for _, c := range got[tc.kind] {
			names = append(names, c.Template)
		}
		if !slices.Equal(names, tc.want) {
			t.Errorf("%s = %v, want %v", tc.kind, names, tc.want)
		}
	}
}

// TestDeriveVotes covers every derivation that counts corpus evidence and the floor that stops one sighting from deciding: field typing, which properties hold a localization key, fire keys, and nested databases.
func TestDeriveVotes(t *testing.T) {
	t.Run("field value kinds", func(t *testing.T) {
		// A value naming two kinds is no evidence and must not be a veto: 100 is a CK3 character AND a province, and as a veto that silenced `holder` and 116 more fields entirely. Localizing an object must not untype the field that names it either -- admitting loc_key defs made most keys ambiguous, and on A Game of Thrones 111 correct types went silent while 183 fields were typed `loc_key` instead.
		for _, tt := range []struct {
			name  string
			defs  []Def
			rhs   map[string]map[string]bool
			field string
			want  string
		}{
			{
				"values agreeing on one kind type the field",
				[]Def{{Kind: "event_theme", Key: "seduction"}, {Kind: "event_theme", Key: "intrigue"}, {Kind: "trait", Key: "brave"}},
				map[string]map[string]bool{"theme": {"seduction": true, "intrigue": true}},
				"theme", "event_theme",
			},
			{
				"values naming two kinds drop the field",
				[]Def{{Kind: "event_theme", Key: "seduction"}, {Kind: "event_theme", Key: "intrigue"}, {Kind: "trait", Key: "brave"}},
				map[string]map[string]bool{"theme": {"seduction": true, "brave": true}},
				"theme", "",
			},
			{
				"one shared id is not a veto",
				[]Def{
					{Kind: "characters", Key: "100"}, {Kind: "provinces", Key: "100"},
					{Kind: "characters", Key: "101"}, {Kind: "characters", Key: "102"},
				},
				map[string]map[string]bool{"holder": {"100": true, "101": true, "102": true}},
				"holder", "characters",
			},
			{
				"a mod localizing its own objects keeps the type",
				[]Def{
					{Kind: "innovations", Key: "innovation_bannus"},
					{Kind: "innovations", Key: "innovation_motte"},
					{Kind: "loc_key", Key: "innovation_bannus"},
					{Kind: "loc_key", Key: "innovation_motte"},
				},
				map[string]map[string]bool{"add_innovation": {"innovation_bannus": true, "innovation_motte": true}},
				"add_innovation", "innovations",
			},
			{
				"a loc key is never a field type",
				[]Def{{Kind: "loc_key", Key: "my_tooltip_a"}, {Kind: "loc_key", Key: "my_tooltip_b"}},
				map[string]map[string]bool{"custom_tooltip": {"my_tooltip_a": true, "my_tooltip_b": true}},
				"custom_tooltip", "",
			},
			{
				// CK3's `body_part` takes exactly `head` and `torso`; `head` also names an artifact visual, so 1 of 2 cleared the ratio exactly and every `body_part = torso` became a dangling `visuals` reference.
				"one hit types nothing",
				[]Def{{Kind: "visuals", Key: "head"}, {Kind: "visuals", Key: "helmet"}},
				map[string]map[string]bool{"body_part": {"head": true, "torso": true}},
				"body_part", "",
			},
			{
				"a second corroborating value is enough",
				[]Def{{Kind: "visuals", Key: "head"}, {Kind: "visuals", Key: "helmet"}},
				map[string]map[string]bool{"artifact_visual": {"head": true, "helmet": true}},
				"artifact_visual", "visuals",
			},
		} {
			t.Run(tt.name, func(t *testing.T) {
				got := deriveFieldValueKinds(tt.rhs, newDefIndex(tt.defs))
				if got[tt.field] != tt.want {
					t.Errorf("%s = %q, want %q (%v)", tt.field, got[tt.field], tt.want, got)
				}
			})
		}

		// Unanimity is not evidence: values that resolve to nothing are skipped, so without a coverage floor one coincidental hit typed the whole field. On vanilla, `remove_variable` was typed `achievements` on 2 of 392.
		thin := map[string]map[string]bool{"remove_variable": {"some_achievement": true}}
		for i := range 30 {
			thin["remove_variable"][string(rune('a'+i%26))+"_var"+string(rune('0'+i/26))] = true
		}
		one := []Def{{Kind: "achievements", Key: "some_achievement"}}
		if got := deriveFieldValueKinds(thin, newDefIndex(one)); got["remove_variable"] != "" {
			t.Errorf("typed on 1 of %d values: %q", len(thin["remove_variable"]), got["remove_variable"])
		}
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
	})

	// Which properties hold a localization key is partly declared (loc lists the engine's own) and partly not: Victoria 3 character templates write `last_name = Addams` against a real key, so a mod's localized surnames were defined, consumed by the game, and reported as orphaned.
	t.Run("loc fields", func(t *testing.T) {
		locKeys := map[string]bool{}
		surnames := map[string]bool{}
		for i := range 10 {
			n := "surname_" + string(rune('a'+i))
			surnames[n] = true
			locKeys[n] = true
		}
		// An object reference field: every culture has a key named after it, so every value here is a key too -- and none of that makes `culture` a property that holds prose. Skipping values that name a definition is what separates the two; without it CK3 elected 323 fields.
		culture := map[string]bool{}
		defKeys := map[string]bool{}
		for i := range 10 {
			c := "culture_" + string(rune('a'+i))
			culture[c] = true
			defKeys[c] = true
			locKeys[c] = true
		}
		thin := map[string]bool{"surname_a": true, "surname_b": true, "surname_c": true}
		// A declared trigger: its values are law ids, which have keys named after them, so the corpus alone says "this holds keys" and the corpus alone is wrong.
		schema := &declared.Schema{Triggers: map[string]declared.EngineToken{"has_realm_law": {}}}

		got := deriveLocFields(map[string]map[string]bool{
			"last_name":       surnames,
			"culture":         culture,
			"rare_title_slot": thin,
			"has_realm_law":   surnames,
			"has_$TYPE$_law":  surnames,
			// Already declared by the engine list; deriving it again would let a broad derivation weaken a strict declaration.
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
	})

	t.Run("fire keys skip loc props", func(t *testing.T) {
		src := `namespace = ns
ns.1 = {
	title = ns.1
	desc = ns.1.t
	immediate = { trigger_event = ns.2 }
}
ns.2 = { title = ns.2.t }
`
		ff := FactsOf("ck3", "events/x.txt", "events/x.txt", "",
			jomini.Parse(jomini.Normalize(src)))
		got := resolveFireKinds(deriveFireKeys(ff, map[string]bool{
			"ns.1": true, "ns.2": true,
		}, nil))
		if got["title"] != "" || got["desc"] != "" {
			t.Fatalf("loc props voted as fire: %v", got)
		}
		if got["trigger_event"] != "event" {
			t.Fatalf("trigger_event: %v", got)
		}
	})

	// A fire key needs coverage, but only once there is enough evidence to read it: for fire keys a low hit rate is often the defect worth reporting. Without the floor, Vic3 keys like `alert` became on_action fire sites and produced 4,033 false dangling rows on one real mod.
	t.Run("fire key coverage", func(t *testing.T) {
		for _, tt := range []struct {
			name, key string
			counts    *fireCount
			want      string
		}{
			{"1 of 40 names is not a fire key", "alert",
				&fireCount{kinds: map[string]int{"event": 1}, named: 40}, ""},
			{"a small sample falls back to unanimity", "trigger_event",
				&fireCount{kinds: map[string]int{"event": 1}, named: 3}, "event"},
			{"30 of 32 types the key", "trigger_event",
				&fireCount{kinds: map[string]int{"event": 30}, named: 32}, "event"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				got := resolveFireKinds(map[string]*fireCount{tt.key: tt.counts})
				if got[tt.key] != tt.want {
					t.Errorf("%s = %q, want %q", tt.key, got[tt.key], tt.want)
				}
			})
		}
	})

	// A nested database is only harvested when the game DECLARES its child kind as a type. `faith` is a CK3 scope type, so religions yield faiths; `SKILL` is a script-parameter name that happens to repeat inside event blocks, and harvesting it minted 51,674 defs of a kind that does not exist.
	t.Run("nested shapes need a declared child", func(t *testing.T) {
		tree := jomini.Parse(jomini.Normalize(
			"christianity_religion = {\n\tfaiths = {\n\t\tcatholic = { color = { 1 1 1 } }\n\t}\n}\n"))
		cite := jomini.Parse(jomini.Normalize(
			"e.1 = { immediate = { exists = faith:catholic } }\n"))
		files := []FileFacts{
			FactsOf("ck3", "r.txt", "common/religion/religion_types/r.txt", "", tree),
			FactsOf("ck3", "x.txt", "events/x.txt", "", cite),
		}
		defs := []Def{{Kind: "religion_types", Key: "christianity_religion"}}
		keys := newDefIndex(defs).citableKeys()
		var cites []typedCite
		for _, ff := range files {
			t, _ := foldCites(ff)
			cites = append(cites, t...)
		}
		shapesFor := func(s *declared.Schema) []NestedShape {
			byChild := map[string]*nestedWanted{}
			idChild := nestedIDChild("ck3", keys, nil, cites, nil)
			for _, ff := range files {
				observeNestedFacts(ff, idChild, byChild)
			}
			return nestedShapesFrom(byChild, s)
		}
		got := shapesFor(&declared.Schema{Scopes: map[string]declared.ScopeType{"faith": {}}})
		if len(got) != 1 || got[0].ParentKind != "religion_types" ||
			got[0].ChildKind != "faith" || got[0].GroupKey != "faiths" {
			t.Fatalf("declared child kind should be harvested; shapes=%+v", got)
		}
		if got := shapesFor(&declared.Schema{}); len(got) != 0 {
			t.Errorf("undeclared child kind harvested anyway: %+v", got)
		}
		if got := shapesFor(nil); len(got) != 0 {
			t.Errorf("no schema must yield no nested databases: %+v", got)
		}
	})

	// harvestGetSet replaced a regex that ran over every byte of every script file; the scanner must agree with the pattern it replaced, boundaries included.
	t.Run("harvestGetSet matches the pattern it replaced", func(t *testing.T) {
		re := regexp.MustCompile(`\b((?:Get|Set)[A-Za-z0-9_]+)\b`)
		for _, src := range []string{
			"", "Get", "GetName", "[GetPlayer.GetName]",
			"desc = \"[ROOT.Char.GetTitledFirstName]\"",
			"xGetName and _SetFoo and forgetName",
			"SetVariable = { name = x } get_lower = 1",
			"GetName2|E] [SetY_1]", "Set", "GetA.SetB.GetC_9",
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
	})
}
