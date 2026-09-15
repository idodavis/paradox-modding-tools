// loccoverage_test.go covers strict-only missing loc, broad-field skips,
// mod-local orphans, and descriptor/metadata name refs.
package views

import (
	"paradox-modding-tools/services/internal/lang/loc"

	"fmt"
	"strings"
	"testing"

	"paradox-modding-tools/services/internal/lang/derived"
	"paradox-modding-tools/services/internal/lang/session"
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
	s := buildSession(t, "ck3", nil, &derived.VanillaLoc{
		Sites: map[string]derived.LocEntry{"vanilla_key": {Value: "Hi"}},
	}, []derived.ModInput{oneMod(t, "ck3", map[string]string{
		"events/x.txt": `namespace = ns
ns.1 = {
	type = character_event
	title = missing_key
	name = some_var
	desc = used_key
}
`,
		"localization/english/a_l_english.yml": "" +
			"l_english:\n used_key:0 \"Hi\"\n orphan_key:0 \"Bye\"\n" +
			" war:0 \"$ORDER$ $no_such_key$\"\n",
		"localization/english/game_rules_l_english.yml": "" +
			"l_english:\n setting_rule:0 \"Rule\"\n",
		"localization/english/message_filter_l_english.yml": "" +
			"l_english:\n filter_key:0 \"Filter\"\n",
	})})
	_, cov := Coverage(s)
	if !coverageIssue(cov, "english", "missing", "missing_key") {
		t.Fatalf("missing_key not reported: %+v", cov)
	}
	if lu := Lookup(s, "used_key"); lu == nil || lu.Text != "Hi" {
		t.Fatalf("Lookup used_key = %+v", lu)
	}
	if lu := Lookup(s, "vanilla_key"); lu == nil || lu.Origin != "vanilla" {
		t.Fatalf("Lookup vanilla_key = %+v", lu)
	}
	if coverageIssue(cov, "english", "missing", "some_var") {
		t.Fatal("loc-broad name= should not be missing")
	}
	if !coverageIssue(cov, "english", "orphaned", "setting_rule") {
		t.Fatal("unused game_rules loc has no convention to explain it")
	}
	if !coverageIssue(cov, "english", "orphaned", "filter_key") {
		t.Fatal("unused message_filter loc has no convention to explain it")
	}
	if !coverageIssue(cov, "english", "orphaned", "orphan_key") {
		t.Fatal("unused loc should stay orphaned")
	}
	if coverageIssue(cov, "english", "missing", "ORDER") {
		t.Fatal("$ORDER$ is engine data, not a missing loc key")
	}
	if !coverageIssue(cov, "english", "missing", "no_such_key") {
		t.Fatal("$no_such_key$ reuse should stay missing")
	}
}

func TestCoverageConventionLocsInGenericFile(t *testing.T) {
	s := buildSession(t, "ck3", &derived.VanillaCache{
		LocConventions: map[string][]loc.Convention{
			"decisions":  {{Template: "{id}", Coverage: 1}, {Template: "{id}_desc", Coverage: 1}},
			"game_rules": {{Template: "game_rule_{id}", Coverage: 1}},
		},
	}, nil, []derived.ModInput{oneMod(t, "ck3", map[string]string{
		"common/game_rules/00.txt": `my_rule = {
	default = opt_a
	opt_a = { }
	exists = game_rule_setting:opt_a
}
`,
		"common/decisions/00.txt": `ai_mogyer_adopt_christianity = { }
`,
		"common/casus_belli_types/cb.txt": "hre_conquest = { war_name = \"HRE_CONQUEST_WAR_NAME\" " +
			"cb_name = \"HRE_CONQUEST_DUCHY_CB_NAME\" }\n",
		"common/character_interactions/i.txt": "demand_x = { notification_text = DEMAND_IMPERIAL_SUBJUGATION_NOTIFICATION }\n",
		"common/flavorization/f.txt":          "hungarian_empire = { type = empire }\n",
		"common/messages/00.txt": `my_msg = { }
`,
		"localization/english/mod_l_english.yml": "" +
			"l_english:\n game_rules_my_rule:0 \"R\"\n rule_my_rule:0 \"R\"\n setting_opt_a:0 \"A\"\n my_msg:0 \"M\"\n" +
			" ai_mogyer_adopt_christianity:0 \"A\"\n" +
			" ai_mogyer_adopt_christianity_desc:0 \"D\"\n" +
			" ai_mogyer_adopt_christianity_tooltip:0 \"T\"\n" +
			" ai_mogyer_adopt_christianity_confirm:0 \"C\"\n" +
			" HRE_CONQUEST_WAR_NAME:0 \"War\"\n" +
			" HRE_CONQUEST_DUCHY_CB_NAME:0 \"CB\"\n" +
			" DEMAND_IMPERIAL_SUBJUGATION_NOTIFICATION:0 \"N\"\n" +
			" cn_hungarian_empire_adj:0 \"Hungarian\"\n unused_key:0 \"U\"\n",
	})})
	_, cov := Coverage(s)
	if !coverageIssue(cov, "english", "orphaned", "game_rules_my_rule") ||
		coverageIssue(cov, "english", "orphaned", "game_rule_my_rule") {
		t.Fatal("the learned prefix names the rule; the plural spelling names nothing")
	}
	if !coverageIssue(cov, "english", "orphaned", "rule_my_rule") {
		t.Fatal("encyclopedia rule_<id> is not a template")
	}
	if !coverageIssue(cov, "english", "orphaned", "setting_opt_a") {
		t.Fatal("encyclopedia setting_ key is not a template")
	}
	if coverageIssue(cov, "english", "orphaned", "ai_mogyer_adopt_christianity") {
		t.Fatal("decision id loc should not be orphaned")
	}
	if !coverageIssue(cov, "english", "orphaned", "ai_mogyer_adopt_christianity_tooltip") {
		t.Fatal("encyclopedia <id>_tooltip is not a template")
	}
	if !coverageIssue(cov, "english", "orphaned", "ai_mogyer_adopt_christianity_confirm") {
		t.Fatal("encyclopedia <id>_confirm is not a template")
	}
	if coverageIssue(cov, "english", "orphaned", "HRE_CONQUEST_WAR_NAME") {
		t.Fatal("war_name cite should not be orphaned")
	}
	if coverageIssue(cov, "english", "orphaned", "HRE_CONQUEST_DUCHY_CB_NAME") {
		t.Fatal("cb_name cite should not be orphaned")
	}
	if coverageIssue(cov, "english", "orphaned", "DEMAND_IMPERIAL_SUBJUGATION_NOTIFICATION") {
		t.Fatal("notification_text cite should not be orphaned")
	}
	if coverageIssue(cov, "english", "orphaned", "ai_mogyer_adopt_christianity_desc") {
		t.Fatal("decision id_desc loc should not be orphaned")
	}
	if !coverageIssue(cov, "english", "orphaned", "cn_hungarian_empire_adj") {
		t.Fatal("encyclopedia flavorization loc is not a template")
	}
	if !coverageIssue(cov, "english", "orphaned", "my_msg") {
		t.Fatal("message def id is not a loc key")
	}
	if !coverageIssue(cov, "english", "orphaned", "unused_key") {
		t.Fatal("unused loc should stay orphaned")
	}
}

func TestCoverageIgnoresVanillaCacheRefs(t *testing.T) {
	s := buildSession(t, "ck3", &derived.VanillaCache{
		LocRefs: []derived.Ref{{
			Key: "vanilla_title", Kind: "loc", Path: "game/events/x.txt", Line: 1,
		}},
	}, nil, []derived.ModInput{oneMod(t, "ck3", map[string]string{
		"events/x.txt": `namespace = ns
ns.1 = { title = event_message_title }
`,
		"common/messages/zz.txt": `quieter_events_neutral = { title = event_message_title }
`,
		"localization/english/zz_l_english.yml": "" +
			"l_english:\n event_message_title:0 \"T\"\n unused_mod:0 \"U\"\n",
	})})
	_, cov := Coverage(s)
	if coverageIssue(cov, "english", "missing", "vanilla_title") {
		t.Fatal("vanilla cache loc refs must not be missing")
	}
	if coverageIssue(cov, "english", "missing", "quieter_events_neutral") {
		t.Fatal("message id must not be invented as missing")
	}
	if coverageIssue(cov, "english", "missing", "event_message_title") {
		t.Fatal("defined title loc should not be missing")
	}
	if !coverageIssue(cov, "english", "orphaned", "unused_mod") {
		t.Fatal("unused mod loc should stay orphaned")
	}
}

func TestCoverageMissingNotSuppressedByDefaultLang(t *testing.T) {
	s := session.NewWithLoc("ws", "ck3", "english", nil, nil, []derived.ModInput{
		oneMod(t, "ck3", map[string]string{
			"events/x.txt": `namespace = ns
ns.1 = { type = character_event title = used_key }
`,
			"localization/english/a_l_english.yml": "l_english:\n used_key:0 \"Hi\"\n",
			"localization/french/a_l_french.yml":   "l_french:\n other:0 \"Bonjour\"\n",
		}),
	})
	_, cov := Coverage(s)
	if !coverageIssue(cov, "french", "missing", "used_key") {
		t.Fatalf("french missing used_key (english has it): %+v", cov)
	}
	if coverageIssue(cov, "english", "missing", "used_key") {
		t.Fatal("english defines used_key")
	}
}

func TestCoverageSkipsDescriptorName(t *testing.T) {
	s := buildSession(t, "ck3", nil, nil, []derived.ModInput{oneMod(t, "ck3", map[string]string{
		"events/x.txt": `namespace = ns
ns.1 = { type = character_event title = used_key }
`,
		"localization/english/a_l_english.yml": "l_english:\n used_key:0 \"Hi\"\n",
	})})
	_, rows := Coverage(s)
	for _, m := range rows {
		path := m.Rel
		if len(m.Sites) > 0 {
			path = m.Sites[0].Path
		}
		if strings.Contains(strings.ToLower(path), "descriptor.mod") ||
			m.Name == "t" && m.Type == "missing" {
			t.Fatalf("descriptor name leaked: %+v", m)
		}
	}
}

func TestCoverageSkipsMetadataName(t *testing.T) {
	s := buildSession(t, "vic3", nil, nil, []derived.ModInput{oneMod(t, "vic3", map[string]string{
		"events/x.txt": `namespace = ns
ns.1 = { type = character_event title = used_key }
`,
		"localization/english/a_l_english.yml": "l_english:\n used_key:0 \"Hi\"\n",
	})})
	_, rows := Coverage(s)
	for _, m := range rows {
		path := m.Rel
		if len(m.Sites) > 0 {
			path = m.Sites[0].Path
		}
		if strings.Contains(strings.ToLower(path), "metadata.json") {
			t.Fatalf("metadata leaked: %+v", m)
		}
	}
}

// Orphaned is mod-local: a key is dead only when the mod that DEFINES it has nothing that reads it.
func TestCoverageOrphanIsModLocal(t *testing.T) {
	c := &derived.VanillaCache{
		LocKeyPrefixes: []string{"NOT"},
		LocConventions: map[string][]loc.Convention{
			"traits": {{Template: "{id}", Coverage: 1}},
		},
	}
	derived.PrepareCache(c)
	modA := oneMod(t, "ck3", map[string]string{
		"common/traits/a.txt": "brave = { }\n",
		"localization/english/a_l_english.yml": "" +
			"l_english:\n brave:0 \"Brave\"\n a_nested:0 \"see $a_stem$\"\n" +
			" a_stem:0 \"Stem\"\n a_dead:0 \"Nothing reads this\"\n" +
			" NOT_brave:0 \"Not brave\"\n NOT_nothing:0 \"Stem is not a key\"\n" +
			" a_concept:0 \"see [Concept('my_concept')]\"\n my_concept:0 \"A concept\"\n",
	})
	modB := oneMod(t, "ck3", map[string]string{
		"events/b.txt":                         "namespace = ns\nns.1 = { title = a_dead }\n",
		"localization/english/b_l_english.yml": "l_english:\n b_dead:0 \"Also dead\"\n",
	})
	modB.Origin, modB.Order = "mod2", 1
	s := buildSession(t, "ck3", c, nil, []derived.ModInput{modA, modB})
	_, cov := Coverage(s)
	// Read by its own mod: a convention over its own def, a `$key$` and a `[Concept(…)]` in its own values.
	for _, key := range []string{"brave", "a_stem", "NOT_brave", "my_concept"} {
		if coverageIssue(cov, "english", "orphaned", key) {
			t.Errorf("%s is read by its own mod, not orphaned", key)
		}
	}
	// a_dead is cited only by the OTHER mod, so it is still dead in this one.
	for _, key := range []string{"a_nested", "a_dead", "b_dead", "NOT_nothing", "a_concept"} {
		if !coverageIssue(cov, "english", "orphaned", key) {
			t.Errorf("%s has no reader in its own mod: %+v", key, cov)
		}
	}
}

// The loudest orphan false positive: a key sharing its name with an object the mod defines.
func TestCoverageCitedIdIsNotOrphaned(t *testing.T) {
	c := &derived.VanillaCache{
		FieldValueKinds: map[string]string{"modifier": "opinion_modifiers"},
		LocConventions: map[string][]loc.Convention{
			"opinion_modifiers": {{Template: "{id}", Coverage: 1}},
		},
	}
	derived.PrepareCache(c)
	s := buildSession(t, "ck3", c, nil, []derived.ModInput{oneMod(t, "ck3",
		map[string]string{
			"common/opinion_modifiers/00_x.txt": "friend_after_subjugation = {\n\topinion = 20\n}\n",
			"common/scripted_effects/e.txt": "do_it = {\n" +
				"\tadd_opinion = { modifier = friend_after_subjugation }\n}\n",
			"localization/english/a_l_english.yml": "" +
				"l_english:\n friend_after_subjugation:0 \"Subjugated\"\n" +
				" truly_unused_key:0 \"Nothing cites this\"\n",
		})})
	_, cov := Coverage(s)
	if coverageIssue(cov, "english", "orphaned", "friend_after_subjugation") {
		t.Errorf("cited id reported as orphaned")
	}
	// The check must stay useful: a key nothing names anywhere is still an
	// orphan. Suppressing everything would trade one wrong answer for another.
	if !coverageIssue(cov, "english", "orphaned", "truly_unused_key") {
		t.Errorf("genuinely unused key not reported: %+v", cov)
	}
}

// The KPI is how many there are, not how many the table can show. This is the
// sibling of TestCompatibilityCountsAreNotTheRowBudget: Coverage already
// increments its counter above the budget check, and nothing said so, which is
// how the same fault was written afresh in compat.go 180 lines away.
func TestCoverageCountsAreNotTheRowBudget(t *testing.T) {
	const want = healthLocCap + 37
	var body strings.Builder
	body.WriteString("namespace = ns\n")
	for i := range want {
		fmt.Fprintf(&body, "ns.%d = {\n\ttype = character_event\n\ttitle = miss_%d\n}\n",
			i+1, i)
	}
	// A language only appears once the mod ships a file for it, and the one key
	// in it is used so it does not become an orphan row spending the same budget.
	body.WriteString("ns.9999 = {\n\ttype = character_event\n\tdesc = anchor\n}\n")
	s := buildSession(t, "ck3", nil, nil, []derived.ModInput{
		oneMod(t, "ck3", map[string]string{
			"events/x.txt": body.String(),
			"localization/english/a_l_english.yml": "" +
				"l_english:\n anchor:0 \"A\"\n",
		}),
	})
	langs, rows := Coverage(s)

	shown := 0
	for _, r := range rows {
		if r.Type == "missing" && r.Language == "english" {
			shown++
		}
	}
	if shown != healthLocCap {
		t.Errorf("missing rows = %d, want the full budget of %d", shown, healthLocCap)
	}
	found := false
	for _, l := range langs {
		if l.Language != "english" {
			continue
		}
		found = true
		if l.Missing != want {
			t.Errorf("Missing KPI = %d, want %d (the count, not the %d rows shown)",
				l.Missing, want, shown)
		}
	}
	if !found {
		t.Fatal("no english language row at all")
	}
}
