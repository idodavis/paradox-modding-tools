// loccoverage_test.go covers strict-only missing loc, broad-field skips, mod-local orphans, and descriptor/metadata name refs.
package views

import (
	"fmt"
	"strings"
	"testing"
)

func coverageIssue(rows []HealthRow, lang, kind, key string) bool {
	for _, r := range rows {
		if r.Language == lang && r.Type == kind && r.Name == key {
			return true
		}
	}
	return false
}

func TestCoverageStrictAndSkips(t *testing.T) {
	s := buildSession(t, "ck3", oneMod(t, "ck3", map[string]string{
		"events/x.txt":                                      "namespace = ns\nns.1 = {\n\ttype = character_event\n\ttitle = missing_key\n\tname = some_var\n\tdesc = used_key\n}\n",
		"localization/english/a_l_english.yml":              "l_english:\n used_key:0 \"Hi\"\n orphan_key:0 \"Bye\"\n war:0 \"$ORDER$ $no_such_key$\"\n",
		"localization/english/game_rules_l_english.yml":     "l_english:\n setting_rule:0 \"Rule\"\n",
		"localization/english/message_filter_l_english.yml": "l_english:\n filter_key:0 \"Filter\"\n",
	}))
	_, cov := Coverage(s)
	for _, tt := range []struct {
		kind, key string
		want      bool
	}{
		{"missing", "missing_key", true}, {"missing", "some_var", false}, {"orphaned", "setting_rule", true},
		{"orphaned", "filter_key", true}, {"orphaned", "orphan_key", true}, {"missing", "ORDER", false}, {"missing", "no_such_key", true},
	} {
		if got := coverageIssue(cov, "english", tt.kind, tt.key); got != tt.want {
			t.Errorf("%s %s = %v, want %v", tt.kind, tt.key, got, tt.want)
		}
	}
	if lu := Lookup(s, "used_key"); lu == nil || lu.Text != "Hi" {
		t.Fatalf("Lookup used_key = %+v", lu)
	}
	if lu := Lookup(s, "vanilla_key"); lu == nil || lu.Origin != "vanilla" {
		t.Fatalf("Lookup vanilla_key = %+v", lu)
	}
}

// A convention over the mod's own definitions reads their keys; an encyclopedia key nothing names stays an orphan.
func TestCoverageConventionLocsInGenericFile(t *testing.T) {
	s := buildSession(t, "ck3", oneMod(t, "ck3", map[string]string{
		"common/decisions/00.txt":             "ai_mogyer_adopt_christianity = { }\n",
		"common/casus_belli_types/cb.txt":     "hre_conquest = { war_name = \"HRE_CONQUEST_WAR_NAME\" cb_name = \"HRE_CONQUEST_DUCHY_CB_NAME\" }\n",
		"common/character_interactions/i.txt": "demand_x = { notification_text = DEMAND_IMPERIAL_SUBJUGATION_NOTIFICATION }\n",
		"common/messages/00.txt":              "my_msg = { }\n",
		"localization/english/mod_l_english.yml": "l_english:\n my_msg:0 \"M\"\n ai_mogyer_adopt_christianity:0 \"A\"\n" +
			" ai_mogyer_adopt_christianity_desc:0 \"D\"\n ai_mogyer_adopt_christianity_confirm:0 \"C\"\n" +
			" HRE_CONQUEST_WAR_NAME:0 \"War\"\n HRE_CONQUEST_DUCHY_CB_NAME:0 \"CB\"\n DEMAND_IMPERIAL_SUBJUGATION_NOTIFICATION:0 \"N\"\n unused_key:0 \"U\"\n",
	}))
	_, cov := Coverage(s)
	for _, tt := range []struct {
		key  string
		want bool
	}{
		{"ai_mogyer_adopt_christianity", false}, {"ai_mogyer_adopt_christianity_desc", false}, {"ai_mogyer_adopt_christianity_confirm", true},
		{"HRE_CONQUEST_WAR_NAME", false}, {"HRE_CONQUEST_DUCHY_CB_NAME", false}, {"DEMAND_IMPERIAL_SUBJUGATION_NOTIFICATION", false},
		{"my_msg", true}, {"unused_key", true},
	} {
		if got := coverageIssue(cov, "english", "orphaned", tt.key); got != tt.want {
			t.Errorf("orphaned %s = %v, want %v", tt.key, got, tt.want)
		}
	}
}

func TestCoverageIgnoresVanillaCacheRefs(t *testing.T) {
	s := buildSession(t, "ck3", oneMod(t, "ck3", map[string]string{
		"events/x.txt":                          "namespace = ns\nns.1 = { title = event_message_title }\n",
		"common/messages/zz.txt":                "quieter_events_neutral = { title = event_message_title }\n",
		"localization/english/zz_l_english.yml": "l_english:\n event_message_title:0 \"T\"\n unused_mod:0 \"U\"\n",
	}))
	_, cov := Coverage(s)
	if coverageIssue(cov, "english", "missing", "v.1.t") {
		t.Fatal("vanilla cache loc refs must not be missing")
	}
	if coverageIssue(cov, "english", "missing", "quieter_events_neutral") || coverageIssue(cov, "english", "missing", "event_message_title") {
		t.Fatal("a defined or invented key reported missing")
	}
	if !coverageIssue(cov, "english", "orphaned", "unused_mod") {
		t.Fatal("unused mod loc should stay orphaned")
	}
}

func TestCoverageMissingNotSuppressedByDefaultLang(t *testing.T) {
	s := buildSession(t, "ck3", oneMod(t, "ck3", map[string]string{
		"events/x.txt":                         "namespace = ns\nns.1 = { type = character_event title = used_key }\n",
		"localization/english/a_l_english.yml": "l_english:\n used_key:0 \"Hi\"\n",
		"localization/french/a_l_french.yml":   "l_french:\n other:0 \"Bonjour\"\n",
	}))
	_, cov := Coverage(s)
	if !coverageIssue(cov, "french", "missing", "used_key") || coverageIssue(cov, "english", "missing", "used_key") {
		t.Fatalf("french must miss used_key and english must not: %+v", cov)
	}
}

func TestCoverageSkipsDescriptorAndMetadataName(t *testing.T) {
	for _, game := range []string{"ck3", "vic3"} {
		s := buildSession(t, game, oneMod(t, game, map[string]string{
			"events/x.txt":                         "namespace = ns\nns.1 = { type = character_event title = used_key }\n",
			"localization/english/a_l_english.yml": "l_english:\n used_key:0 \"Hi\"\n",
		}))
		_, rows := Coverage(s)
		for _, m := range rows {
			path := m.Rel
			if len(m.Sites) > 0 {
				path = m.Sites[0].Path
			}
			if l := strings.ToLower(path); strings.Contains(l, "descriptor.mod") || strings.Contains(l, "metadata.json") || m.Name == "t" && m.Type == "missing" {
				t.Fatalf("%s: descriptor name leaked: %+v", game, m)
			}
		}
	}
}

// Orphaned is mod-local: a key is dead only when the mod that DEFINES it has nothing that reads it.
func TestCoverageOrphanIsModLocal(t *testing.T) {
	modA := oneMod(t, "ck3", map[string]string{
		"common/traits/a.txt": "brave_m = { }\n",
		"localization/english/a_l_english.yml": "l_english:\n brave_m:0 \"Brave\"\n a_nested:0 \"see $a_stem$\"\n" +
			" a_stem:0 \"Stem\"\n a_dead:0 \"Nothing reads this\"\n a_concept:0 \"see [Concept('my_concept')]\"\n my_concept:0 \"A concept\"\n",
	})
	modB := oneMod(t, "ck3", map[string]string{
		"events/b.txt":                         "namespace = ns\nns.1 = { title = a_dead }\n",
		"localization/english/b_l_english.yml": "l_english:\n b_dead:0 \"Also dead\"\n",
	})
	modB.Origin, modB.Order = "mod2", 1
	_, cov := Coverage(buildSession(t, "ck3", modA, modB))
	for _, key := range []string{"brave_m", "a_stem", "my_concept"} {
		if coverageIssue(cov, "english", "orphaned", key) {
			t.Errorf("%s is read by its own mod, not orphaned", key)
		}
	}
	for _, key := range []string{"a_nested", "a_dead", "b_dead", "a_concept"} {
		if !coverageIssue(cov, "english", "orphaned", key) {
			t.Errorf("%s has no reader in its own mod: %+v", key, cov)
		}
	}
}

// A key sharing its name with an object the mod defines and cites is read.
func TestCoverageCitedIdIsNotOrphaned(t *testing.T) {
	s := buildSession(t, "ck3", oneMod(t, "ck3", map[string]string{
		"common/opinion_modifiers/00_x.txt":    "friend_after_subjugation = {\n\topinion = 20\n}\n",
		"common/scripted_effects/e.txt":        "do_it = {\n\tadd_opinion = { modifier = friend_after_subjugation }\n}\n",
		"localization/english/a_l_english.yml": "l_english:\n friend_after_subjugation:0 \"Subjugated\"\n truly_unused_key:0 \"Nothing cites this\"\n",
	}))
	_, cov := Coverage(s)
	if coverageIssue(cov, "english", "orphaned", "friend_after_subjugation") {
		t.Errorf("cited id reported as orphaned")
	}
	if !coverageIssue(cov, "english", "orphaned", "truly_unused_key") {
		t.Errorf("genuinely unused key not reported: %+v", cov)
	}
}

// The KPI is how many there are, not how many the table can show.
func TestCoverageCountsAreNotTheRowBudget(t *testing.T) {
	const want = healthLocCap + 37
	var body strings.Builder
	body.WriteString("namespace = ns\n")
	for i := range want {
		fmt.Fprintf(&body, "ns.%d = {\n\ttype = character_event\n\ttitle = miss_%d\n}\n", i+1, i)
	}
	body.WriteString("ns.9999 = {\n\ttype = character_event\n\tdesc = anchor\n}\n")
	langs, rows := Coverage(buildSession(t, "ck3", oneMod(t, "ck3", map[string]string{
		"events/x.txt":                         body.String(),
		"localization/english/a_l_english.yml": "l_english:\n anchor:0 \"A\"\n",
	})))
	shown := 0
	for _, r := range rows {
		if r.Type == "missing" && r.Language == "english" {
			shown++
		}
	}
	if shown != healthLocCap {
		t.Errorf("missing rows = %d, want the full budget of %d", shown, healthLocCap)
	}
	for _, l := range langs {
		if l.Language == "english" && l.Missing != want {
			t.Errorf("Missing KPI = %d, want %d", l.Missing, want)
		}
	}
}
