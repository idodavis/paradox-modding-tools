// health_test.go covers GetHealth grouping, dangling hygiene, and per-language loc.

package views

import (
	"testing"

	"paradox-modding-tools/services/internal/lang/declared"
	"paradox-modding-tools/services/internal/lang/derived"
	"paradox-modding-tools/services/internal/lang/session"
)

func TestHealthDanglingHygieneAndGroup(t *testing.T) {
	t.Parallel()
	// CK3 faiths have no folder of their own; they exist only nested under a
	// religion_type, and are harvested only because the game declares `faith`
	// as a scope type. A cache with no Schema harvests no nested databases.
	schema := &declared.Schema{
		Scopes: map[string]declared.ScopeType{"faith": {}, "culture": {}},
		Links: map[string]declared.ScopeLink{
			"faith": {Out: "faith", Global: true, Data: true},
		},
		// Declared, because since T-3 a dangling row must rest on something the
		// game states. CK3 declares trigger_event; a fixture that omits it is
		// asserting the behaviour of an install with no script_docs.
		Effects: map[string]declared.EngineToken{
			"trigger_event": {Target: "event"},
		},
	}
	// The install: one religion with the faith nested under it, as the shape vote harvests them.
	const rel = "common/religion/religion_types/r.txt"
	van := []derived.Def{
		{Kind: "religion_types", Key: "christianity_religion", Path: rel},
		{Kind: "faith", Key: "catholic", Path: rel, Nested: true},
	}
	s := twoModTreesWithCache(t, &vanillaCache{Defs: van, Schema: schema},
		map[string]string{
			"common/culture/cultures/c.txt": "english = { }\n",
			"common/script_values/v.txt":    "named_sv = { value = 1 }\n",
			"events/a.txt": `namespace = t
t.1 = {
	type = character_event
	immediate = {
		culture = culture:english
		culture = capital_province.culture
		faith = catholic
		faith = faith:catholic
		value = current_military_strength
		value = script_value:named_sv
		value = tier
		titles = target_titles
		coat_of_arms = $COA$
		trigger_event = t.2
		trigger_event = t.ghost
		trigger_event = t.ghost
	}
}
`,
		},
		map[string]string{
			"events/b.txt": "namespace = t\nt.2 = { type = character_event }\n",
		})
	got := Health(s, nil)
	if findCompat(got.Rows, compatDangling, "culture:english") != nil {
		t.Fatal("culture:english should resolve to english")
	}
	if findCompat(got.Rows, compatDangling, "capital_province.culture") != nil {
		t.Fatal("dotted link must not be dangling")
	}
	if findCompat(got.Rows, compatDangling, "$COA$") != nil {
		t.Fatal("$COA$ must not be dangling")
	}
	if findCompat(got.Rows, compatDangling, "catholic") != nil {
		t.Fatal("nested faith must resolve from vanilla religion_types")
	}
	if findCompat(got.Rows, compatDangling, "current_military_strength") != nil {
		t.Fatal("formula value= must not be a script_value ref")
	}
	if findCompat(got.Rows, compatDangling, "named_sv") != nil {
		t.Fatal("script_value:named_sv must resolve")
	}
	if findCompat(got.Rows, compatDangling, "tier") != nil {
		t.Fatal("formula token tier must not be dangling")
	}
	if findCompat(got.Rows, compatDangling, "target_titles") != nil {
		t.Fatal("titles = target_titles is a CB selector")
	}
	ghost := findCompat(got.Rows, compatDangling, "t.ghost")
	if ghost == nil || ghost.Refs != 2 {
		t.Fatalf("grouped dangling t.ghost = %+v", ghost)
	}
	if ghost.Sites[0].Snippet == "" {
		t.Fatal("expected snippet on dangling site")
	}
}

func TestHealthLocEveryLanguage(t *testing.T) {
	t.Parallel()
	s := session.New("ws", "ck3", "french", nil, []derived.ModInput{
		oneMod(t, "ck3", map[string]string{
			"events/x.txt": `namespace = ns
ns.1 = { type = character_event title = used_key }
`,
			"localization/english/a_l_english.yml": "" +
				"l_english:\n used_key:0 \"Hello\"\n shared:0 \"Same\"\n" +
				" war:0 \"$ORDER$\"\n",
			"localization/french/a_l_french.yml": "" +
				"l_french:\n shared:0 \"Same\"\n",
		}),
	})
	got := Health(s, nil)
	var french, english *HealthLang
	for i := range got.Languages {
		if got.Languages[i].Language == "french" {
			french = &got.Languages[i]
		}
		if got.Languages[i].Language == "english" {
			english = &got.Languages[i]
		}
	}
	if french == nil || english == nil {
		t.Fatalf("languages=%+v", got.Languages)
	}
	if french.Missing < 1 {
		t.Fatalf("french missing suppressed by default/english: %+v", french)
	}
	if !findHealthLoc(got.Rows, "missing", "used_key", "french") {
		t.Fatal("french missing used_key")
	}
	if findHealthLoc(got.Rows, "missing", "used_key", "english") {
		t.Fatal("english has used_key")
	}
	if !findHealthLoc(got.Rows, "untranslated", "shared", "french") {
		t.Fatal("french shared should be untranslated vs english")
	}
	if findHealthLoc(got.Rows, "untranslated", "shared", "english") {
		t.Fatal("english is the untranslated baseline")
	}
	if findHealthLoc(got.Rows, "missing", "ORDER", "english") {
		t.Fatal("$ORDER$ is engine data, not a missing loc key")
	}
}

func findHealthLoc(rows []HealthRow, typ, name, lang string) bool {
	for _, r := range rows {
		if r.Type == typ && r.Name == name && r.Language == lang {
			return true
		}
	}
	return false
}
