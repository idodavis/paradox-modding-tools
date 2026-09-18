// health_test.go covers GetHealth grouping, dangling hygiene, and per-language loc.

package views

import "testing"

func TestHealthDanglingHygieneAndGroup(t *testing.T) {
	s := twoModTrees(t,
		map[string]string{
			"common/culture/cultures/c.txt": "english_m = { }\n",
			"common/script_values/v.txt":    "named_sv = { value = 1 }\n",
			"events/a.txt": "namespace = t\nt.1 = {\n\ttype = character_event\n\timmediate = {\n" +
				"\t\tculture = culture:english_m\n\t\tculture = capital_province.culture\n\t\tfaith = catholic\n\t\tfaith = faith:catholic\n" +
				"\t\tvalue = current_military_strength\n\t\tvalue = script_value:named_sv\n\t\tvalue = tier\n\t\ttitles = target_titles\n" +
				"\t\tcoat_of_arms = $COA$\n\t\ttrigger_event = t.2\n\t\ttrigger_event = t.ghost\n\t\ttrigger_event = t.ghost\n\t}\n}\n",
		},
		map[string]string{"events/b.txt": "namespace = t\nt.2 = { type = character_event }\n"})
	got := Health(s, nil)
	for _, name := range []string{"culture:english_m", "capital_province.culture", "$COA$", "catholic", "current_military_strength", "named_sv", "tier", "target_titles"} {
		if findCompat(got.Rows, compatDangling, name) != nil {
			t.Errorf("%s reported dangling", name)
		}
	}
	ghost := findCompat(got.Rows, compatDangling, "t.ghost")
	if ghost == nil || ghost.Refs != 2 || ghost.Sites[0].Snippet == "" {
		t.Fatalf("grouped dangling t.ghost = %+v", ghost)
	}
}

func TestHealthLocEveryLanguage(t *testing.T) {
	s := buildSession(t, "ck3", oneMod(t, "ck3", map[string]string{
		"events/x.txt":                         "namespace = ns\nns.1 = { type = character_event title = used_key }\n",
		"localization/english/a_l_english.yml": "l_english:\n used_key:0 \"Hello\"\n shared:0 \"Same\"\n war:0 \"$ORDER$\"\n",
		"localization/french/a_l_french.yml":   "l_french:\n shared:0 \"Same\"\n",
	}))
	s.SetDefaultLang("french")
	got := Health(s, nil)
	var french, english *HealthLang
	for i := range got.Languages {
		switch got.Languages[i].Language {
		case "french":
			french = &got.Languages[i]
		case "english":
			english = &got.Languages[i]
		}
	}
	if french == nil || english == nil || french.Missing < 1 {
		t.Fatalf("languages=%+v", got.Languages)
	}
	for _, tt := range []struct {
		typ, name, lang string
		want            bool
	}{
		{"missing", "used_key", "french", true}, {"missing", "used_key", "english", false},
		{"untranslated", "shared", "french", true}, {"untranslated", "shared", "english", false},
		{"missing", "ORDER", "english", false},
	} {
		if got := findHealthLoc(got.Rows, tt.typ, tt.name, tt.lang); got != tt.want {
			t.Errorf("%s %s %s = %v, want %v", tt.lang, tt.typ, tt.name, got, tt.want)
		}
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
