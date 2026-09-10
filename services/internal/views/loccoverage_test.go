// loccoverage_test.go covers strict-only missing loc, broad-field skips,
// game-rule / message-filter orphans, and descriptor/metadata name refs.
package views

import (
	"fmt"
	"strings"
	"testing"

	"paradox-modding-tools/services/internal/catalog"
	"paradox-modding-tools/services/internal/session"
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
	s := buildSession(t, "ck3", nil, &catalog.VanillaLoc{
		Sites: map[string]catalog.LocEntry{"vanilla_key": {Value: "Hi"}},
	}, []catalog.ModInput{oneMod(t, "ck3", map[string]string{
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
		t.Fatal("unused game_rules loc is orphaned without a convention vote")
	}
	if !coverageIssue(cov, "english", "orphaned", "filter_key") {
		t.Fatal("unused message_filter loc is orphaned without a convention vote")
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
	s := buildSession(t, "ck3", &catalog.VanillaCache{
		LocAffixes: map[string][]catalog.LocAffix{
			"decisions":  {{Defs: 10, Of: 10}, {Suf: "_desc", Defs: 10, Of: 10}},
			"game_rules": {{Pre: "game_rule_", Defs: 10, Of: 10}},
		},
	}, nil, []catalog.ModInput{oneMod(t, "ck3", map[string]string{
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
	if coverageIssue(cov, "english", "orphaned", "game_rules_my_rule") &&
		coverageIssue(cov, "english", "orphaned", "game_rule_my_rule") {
		t.Fatal("voted game_rules kind_id loc should not be orphaned")
	}
	if !coverageIssue(cov, "english", "orphaned", "rule_my_rule") {
		t.Fatal("encyclopedia rule_<id> is unused without a vote")
	}
	if !coverageIssue(cov, "english", "orphaned", "setting_opt_a") {
		t.Fatal("encyclopedia setting_ key is unused without a vote")
	}
	if coverageIssue(cov, "english", "orphaned", "ai_mogyer_adopt_christianity") {
		t.Fatal("decision id loc should not be orphaned")
	}
	if !coverageIssue(cov, "english", "orphaned", "ai_mogyer_adopt_christianity_tooltip") {
		t.Fatal("encyclopedia <id>_tooltip is unused without a vote")
	}
	if !coverageIssue(cov, "english", "orphaned", "ai_mogyer_adopt_christianity_confirm") {
		t.Fatal("encyclopedia <id>_confirm is unused without a vote")
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
		t.Fatal("encyclopedia flavorization loc is unused without a vote")
	}
	if !coverageIssue(cov, "english", "orphaned", "my_msg") {
		t.Fatal("message def id is not a loc key")
	}
	if !coverageIssue(cov, "english", "orphaned", "unused_key") {
		t.Fatal("unused loc should stay orphaned")
	}
}

func TestCoverageIgnoresVanillaCacheRefs(t *testing.T) {
	s := buildSession(t, "ck3", &catalog.VanillaCache{
		LocRefs: []catalog.Ref{{
			Key: "vanilla_title", Kind: "loc", Path: "game/events/x.txt", Line: 1,
		}},
	}, nil, []catalog.ModInput{oneMod(t, "ck3", map[string]string{
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
	s := session.NewWithLoc("ws", "ck3", "english", nil, nil, []catalog.ModInput{
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
	s := buildSession(t, "ck3", nil, nil, []catalog.ModInput{oneMod(t, "ck3", map[string]string{
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
	s := buildSession(t, "vic3", nil, nil, []catalog.ModInput{oneMod(t, "vic3", map[string]string{
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

// A game rule is a definition and gets `rule_<id>`; its options are nested one
// level inside it and never become definitions, yet each gets `setting_<option>`
// and `setting_<option>_desc`. Nothing owned those, so every option a mod added
// produced two orphaned keys.
func TestCoverageGameRuleOptionLocIsNotOrphaned(t *testing.T) {
	s := buildSession(t, "ck3", &catalog.VanillaCache{
		// What a scan derives from vanilla: rules are definitions, options are
		// block members, and each has its own convention.
		LocAffixes: map[string][]catalog.LocAffix{
			"game_rules": {{Pre: "rule_", Defs: 81, Of: 81}},
		},
		LocMemberAffixes: map[string][]catalog.LocAffix{
			"game_rules": {
				{Pre: "setting_", Defs: 297, Of: 304},
				{Pre: "setting_", Suf: "_desc", Defs: 294, Of: 304},
			},
		},
	}, nil, []catalog.ModInput{oneMod(t, "ck3", map[string]string{
		"common/game_rules/00.txt": `secret_unbeliever_found_rule = {
	default = suf_quieter
	suf_vanilla = { flag = GG_can_change_rule }
	suf_quieter = { flag = GG_can_change_rule }
}
`,
		"localization/english/mod_l_english.yml": "" +
			"l_english:\n rule_secret_unbeliever_found_rule:0 \"R\"\n" +
			" setting_suf_quieter:0 \"Quieter\"\n" +
			" setting_suf_quieter_desc:0 \"Fewer events.\"\n" +
			" setting_suf_vanilla:0 \"Vanilla\"\n" +
			" genuinely_unused_key:0 \"U\"\n",
	})})
	_, cov := Coverage(s)
	// The option names exist in no install — the mod invented them — so this
	// only works because BuildIndex now carries the mods' own member names.
	for _, key := range []string{
		"rule_secret_unbeliever_found_rule",
		"setting_suf_quieter", "setting_suf_quieter_desc", "setting_suf_vanilla",
	} {
		if coverageIssue(cov, "english", "orphaned", key) {
			t.Errorf("%s is consumed by convention, not orphaned", key)
		}
	}
	// And a key nothing explains is still reported. Suppressing everything
	// would be no better than suppressing nothing.
	if !coverageIssue(cov, "english", "orphaned", "genuinely_unused_key") {
		t.Error("a key with no owner must still be orphaned")
	}
}

// TestCoverageCitedIdIsNotOrphaned pins the fix for the loudest remaining
// orphan false positive: a key that shares its name with an object script
// cites. The engine reads an object's id and its localization by the same name,
// so `friend_after_subjugation` sitting in an opinion modifier's block accounts
// for the key of that name — and go-to-references already found those uses
// while the orphan check called the key dead.
//
// The reference here is an ordinary field value, not a loc property, which is
// the whole point: UsedLocKeys only counts refs filed under a loc kind.
func TestCoverageCitedIdIsNotOrphaned(t *testing.T) {
	// The field type is what makes the reference exist at all; without it this
	// fixture harvests nothing and the test would pass for the wrong reason.
	c := &catalog.VanillaCache{
		FieldValueKinds: map[string]string{"modifier": "opinion_modifiers"},
	}
	catalog.PrepareCache(c)
	s := buildSession(t, "ck3", c, nil, []catalog.ModInput{oneMod(t, "ck3",
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

// TestCoverageAffixOverCitedNameIsNotOrphaned pins the second half of the same
// rule: a convention whose stem is a name script uses but never defines. A CK3
// game rule category is declared by the rules that cite it, so
// `game_rule_category_<id>` had no definition to own it and read as an orphan.
func TestCoverageAffixOverCitedNameIsNotOrphaned(t *testing.T) {
	c := &catalog.VanillaCache{
		FieldValueKinds: map[string]string{"category": "game_rule_categories"},
		// The convention as the install would have yielded it.
		LocAffixes: map[string][]catalog.LocAffix{
			"game_rule_categories": {{Pre: "game_rule_category_", Defs: 9, Of: 9}},
		},
	}
	catalog.PrepareCache(c)
	s := buildSession(t, "ck3", c, nil, []catalog.ModInput{oneMod(t, "ck3",
		map[string]string{
			"common/game_rules/00_r.txt": "my_rule = {\n\tcategory = my_category\n}\n",
			"localization/english/a_l_english.yml": "" +
				"l_english:\n game_rule_category_my_category:0 \"Mine\"\n" +
				" game_rule_category_nobody_cites_this:0 \"Dead\"\n",
		})})
	if coverageIssue(mustRows(t, s), "english", "orphaned", "game_rule_category_my_category") {
		t.Errorf("convention over a cited name reported as orphaned")
	}
	// Same convention, stem nothing names: still an orphan.
	if !coverageIssue(mustRows(t, s), "english", "orphaned",
		"game_rule_category_nobody_cites_this") {
		t.Errorf("convention over an unknown stem not reported")
	}
}

func mustRows(t *testing.T, s *session.Session) []HealthRow {
	t.Helper()
	_, rows := Coverage(s)
	return rows
}

// The KPI is how many there are, not how many the table can show. This is the
// sibling of TestCompatibilityCountsAreNotTheRowBudget: Coverage already
// increments its counter above the budget check, and nothing said so, which is
// how the same fault was written afresh in compat.go 180 lines away.
func TestCoverageCountsAreNotTheRowBudget(t *testing.T) {
	const want = healthLocCap + 37
	var body strings.Builder
	body.WriteString("namespace = ns\n")
	for i := 0; i < want; i++ {
		fmt.Fprintf(&body, "ns.%d = {\n\ttype = character_event\n\ttitle = miss_%d\n}\n",
			i+1, i)
	}
	// A language only appears once the mod ships a file for it, and the one key
	// in it is used so it does not become an orphan row spending the same budget.
	body.WriteString("ns.9999 = {\n\ttype = character_event\n\tdesc = anchor\n}\n")
	s := buildSession(t, "ck3", nil, nil, []catalog.ModInput{
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
