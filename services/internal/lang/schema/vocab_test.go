package schema

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"paradox-modding-tools/services/internal/game"
)

// dumps writes dump files into a fresh folder and reads the folder.
func dumps(t *testing.T, files map[string]string) *Vocab {
	t.Helper()
	dir := t.TempDir()
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return ReadVocab([]string{filepath.Join(dir, "missing"), dir})
}

func TestReadVocabClassic(t *testing.T) {
	v := dumps(t, map[string]string{
		"effects.log": "Effect Documentation:\n\n--------------------\n\nadd_gold - Adds gold to the scoped character\nSupported Scopes: character\nSupported Targets: dynasty\n\n--------------------\n\n" +
			"add_title_law - add law to scoped title\nadd_title_law = princely_elective_succession_law\nSupported Scopes: landed_title\n\n--------------------\n\n" +
			"create_character - Creates a character\nsave_event_target_as = flag - save the character as an event target\nfaith, culture and dynasty are set from the template\nfaith = faith tag OR a faith scope\nTraits: yes\nSupported Scopes: character\n\n--------------------\n\n" +
			"add_to_variable_list - Adds the event target to a variable list\nadd_to_variable_list = { name = X target = Y }\nWhere X is the name of the variable\nNote: the list is made when it is first added to\nSupported Scopes: none\n\n--------------------\n\n" +
			"debug_log - Log a string\nThe message can be a localization string.\n\nUsage:\n\tdebug_log = \"This is a test message\"\n\nSupported Scopes: none\nOutput Type: text\n\n--------------------\n\n" +
			"switch - Switch on a trigger\nswitch = {\n\ttrigger = simple_assign_trigger\n\tcase_1 = { <effects> }\nSupported Scopes: none\n\n--------------------\n\n" +
			"add_dead_character_flag - add_dead_character_flag = {\n\tflag = <name> - name of the flag\n\tyears/weeks/days = { <min_number> <max_number> } - how long it lasts\n}\nAdds a flag to a dead character.\nSupported Scopes: character\n\n--------------------\n",
		"event_targets.log": "Event Target Documentation:\n\n--------------------\n\nculture - Get the culture with the specified key\nRequires Data: yes\nGlobal Link: yes\nOutput Scopes: culture\n\n--------------------\n\n" +
			"betrothed - Get the betrothed of the scoped character\nInput Scopes: character\nOutput Scopes: character\n\n--------------------\n\n" +
			"faith - Get the faith with the specified key\nRequires Data: yes\nGlobal Link: yes\nOutput Scopes: faith\n\n--------------------\n\n" +
			"faith - Get the faith of the scoped object\nInput Scopes: character, province\nOutput Scopes: faith\n\n--------------------\n\n" +
			"Event Targets Saved from Code:\n\ncontext\nactor\nrecipient\n",
		"on_actions.log": "On Action Documentation:\n\n--------------------\n\nstress_loss_confider:\nFrom Code: No\nExpected Scope: character\n\n--------------------\n\non_tradition_removed:\nFrom Code: Yes\nExpected Scope: none\n\n--------------------\n",
	})

	gold := v.Effects["add_gold"]
	if !slices.Equal(gold.In, []string{"character"}) || gold.Out != "dynasty" || gold.Doc != "Adds gold to the scoped character" || gold.Usage != "" || gold.Name != "add_gold" {
		t.Errorf("add_gold = %+v", gold)
	}
	if law := v.Effects["add_title_law"]; !slices.Equal(law.In, []string{"landed_title"}) || law.Usage != "add_title_law = princely_elective_succession_law" || law.Doc != "add law to scoped title" {
		t.Errorf("add_title_law = %+v", law)
	}
	create := v.Effects["create_character"]
	if create.Doc != "Creates a character\n\nfaith, culture and dynasty are set from the template" || !strings.Contains(create.Usage, "faith = faith tag") || strings.Contains(create.Usage, "Traits") || !slices.Equal(create.In, []string{"character"}) {
		t.Errorf("create_character = %+v", create)
	}
	if list := v.Effects["add_to_variable_list"]; !slices.Equal(list.In, []string{"none"}) || !strings.Contains(list.Doc, "Where X is the name") || !strings.Contains(list.Doc, "Note: the list") {
		t.Errorf("add_to_variable_list = %+v", list)
	}
	if log := v.Effects["debug_log"]; log.Usage != "\tdebug_log = \"This is a test message\"" || log.Doc != "Log a string\nThe message can be a localization string." {
		t.Errorf("debug_log = %+v", log)
	}
	if sw := v.Effects["switch"]; !slices.Equal(sw.In, []string{"none"}) || !strings.Contains(sw.Usage, "case_1") {
		t.Errorf("an unclosed signature swallowed its label: %+v", sw)
	}
	if flag := v.Effects["add_dead_character_flag"]; flag.Doc != "Adds a flag to a dead character." || !slices.Equal(flag.In, []string{"character"}) ||
		flag.Usage != "add_dead_character_flag = {\n\tflag = <name> - name of the flag\n\tyears/weeks/days = { <min_number> <max_number> } - how long it lasts\n}" {
		t.Errorf("a signature that opens on the header line: %+v", flag)
	}
	if len(v.Effects) != 7 {
		t.Errorf("%d effects, want 7: the banner is no entry", len(v.Effects))
	}

	// a label the reader does not know is counted when written as the games write theirs; a prose opener is not
	if !slices.Equal(v.Unknown, []Tally{{Name: "output type", Count: 1}}) {
		t.Errorf("Unknown = %+v", v.Unknown)
	}

	if l := v.Links["culture"]; !l.Prefix || l.Out != "culture" || len(l.In) != 0 || l.Doc != "Get the culture with the specified key" {
		t.Errorf("culture = %+v", l)
	}
	if l := v.Links["betrothed"]; l.Prefix || l.Out != "character" || !slices.Equal(l.In, []string{"character"}) {
		t.Errorf("betrothed = %+v", l)
	}
	if l := v.Links["faith"]; !l.Prefix || l.Out != "faith" || !slices.Equal(l.In, []string{"character", "province"}) {
		t.Errorf("a link declared twice is not one link: %+v", l)
	}
	if !slices.Equal(v.SavedScopes, []string{"context", "actor", "recipient"}) || len(v.Links) != 3 {
		t.Errorf("SavedScopes = %v, %d links", v.SavedScopes, len(v.Links))
	}

	if v.OnActions["stress_loss_confider"] != "character" || v.OnActions["on_tradition_removed"] != "none" || len(v.OnActions) != 2 {
		t.Errorf("OnActions = %v", v.OnActions)
	}
}

func TestReadVocabMarkdown(t *testing.T) {
	v := dumps(t, map[string]string{
		"EFFECTS.LOG": "\xef\xbb\xbf# Effect Documentation\r\n## activate_production_method\r\nActivates the named production method\r\n**Supported Scopes**: country, state  \r\n\r\n" +
			"## add_banned_goods\r\nAdds a total ban of a good to a country  \r\n**Supported Scopes**: country  \r\n**Supported Targets**: goods  \r\n\r\n" +
			"## ordered_hostile_country\nIterate through all hostile countries  \nordered_hostile_country = {  \nlimit = { <triggers> }  \norder_by = script_value  \n<effects> }\n**Supported Scopes**: country  \n**Supported Targets**: country  \n\n" +
			"## some_scripted_list\nmy_trigger = {  \n\texists = scope:pop  \n\t## a comment inside the quoted body\n}\n**Supported Scopes**: state  \n",
		"triggers.log": "# Trigger Documentation\n## has_region_stance\nChecks the stance  \nUsage:  \nregion_stance = {  \n\tregion = sr:some_region  \n}  \nReads gamestate for all scopes.\n**Supported Scopes**: country  \n",
		"event_targets.log": "# Event Target Documentation\n### combat_width\nScope to combat width multiplier of scope province\nInput Scopes: province\nOutput Scopes: value\n\n" +
			"### cu\nGet the culture with the specified key\nRequires Data: yes\nGlobal Link: yes\nOutput Scopes: culture\n\n### area\nUnknown, add something in code registration\nRequires Data: yes\nGlobal Link: yes\nOutput Scopes: area\n\n\n--------------------\n\nEvent Targets Saved from Code:\n\ncontext\nattacker\n",
		"error.log":  "## not_a_dump\nSupported Scopes: country\n",
		"readme.md":  "## not_a_dump_either\n",
		"debug.log":  "add_gold - nothing\n",
		"effects.md": "## wrong_name\n",
	})

	if e := v.Effects["activate_production_method"]; !slices.Equal(e.In, []string{"country", "state"}) || e.Doc != "Activates the named production method" {
		t.Errorf("activate_production_method = %+v", e)
	}
	if e := v.Effects["add_banned_goods"]; e.Out != "goods" || e.Doc != "Adds a total ban of a good to a country" {
		t.Errorf("add_banned_goods = %+v", e)
	}
	if e := v.Effects["ordered_hostile_country"]; e.Usage != "ordered_hostile_country = {\nlimit = { <triggers> }\norder_by = script_value\n<effects> }" || e.Out != "country" || e.Doc != "Iterate through all hostile countries" {
		t.Errorf("ordered_hostile_country = %+v", e)
	}
	if e := v.Effects["some_scripted_list"]; !slices.Equal(e.In, []string{"state"}) || len(v.Effects) != 4 {
		t.Errorf("an indented ## opened an entry: %d effects, %+v", len(v.Effects), e)
	}
	if tr := v.Triggers["has_region_stance"]; tr.Usage != "region_stance = {\n\tregion = sr:some_region\n}" || tr.Doc != "Checks the stance\n\nReads gamestate for all scopes." {
		t.Errorf("has_region_stance = %+v", tr)
	}

	if l := v.Links["combat_width"]; l.Out != "value" || l.Prefix || !slices.Equal(l.In, []string{"province"}) {
		t.Errorf("combat_width = %+v", l)
	}
	if l := v.Links["cu"]; l.Out != "culture" || !l.Prefix {
		t.Errorf("cu = %+v", l)
	}
	if l := v.Links["area"]; l.Doc != "" || l.Out != "area" {
		t.Errorf("the placeholder is no description: %+v", l)
	}
	if !slices.Equal(v.SavedScopes, []string{"context", "attacker"}) || len(v.Links) != 3 {
		t.Errorf("SavedScopes = %v, %d links", v.SavedScopes, len(v.Links))
	}
}

func TestReadVocabBare(t *testing.T) {
	v := dumps(t, map[string]string{
		"event_scopes.log": "Scope Types:\n\nnone:\nEvaluate Triggers: yes\nExecute Effects: yes\nChange Scopes: yes\nSave Token: none\nStores Variables: no\n\ncharacter:\nEvaluate Triggers: yes\nExecute Effects: yes\nChange Scopes: yes\nSave Token: char\nStores Variables: yes\n",
		"modifiers.log": "Printing Modifier Definitions:\nTag: dynasty_opinion\nUse areas: character\n\nTag: $CULTURE$_opinion\nUse areas: character\n\n" +
			"Tag: ai_opinion_bias, Categories: Country, , All, \nTag: army_logistics_distance, Categories: Unit, , All, \n",
	})
	if !slices.Equal(v.ScopeTypes, []string{"none", "character"}) {
		t.Errorf("ScopeTypes = %v: a labelled body line opened an entry", v.ScopeTypes)
	}
	for name, want := range map[string][]string{"dynasty_opinion": {"character"}, "$CULTURE$_opinion": {"character"}, "ai_opinion_bias": {"country", "all"}, "army_logistics_distance": {"unit", "all"}} {
		if m := v.Modifiers[name]; !slices.Equal(m.Categories, want) || m.Name != name {
			t.Errorf("modifier %s = %+v, want %v", name, m, want)
		}
	}

	v = dumps(t, map[string]string{"modifiers.log": "--- Static modifier types ---\nbattle_casualties_mult:\n  Mask: battle\n  Name: aut!Casualties Taken\n  Description: A bonus or penalty to Casualties\nunit_offense_add:\n  Mask: unit\n  Name: Offense\n  Description: A Military Unit's Offense\n"})
	if m := v.Modifiers["battle_casualties_mult"]; !slices.Equal(m.Categories, []string{"battle"}) || m.Doc != "A bonus or penalty to Casualties" || len(v.Modifiers) != 2 {
		t.Errorf("battle_casualties_mult = %+v of %d", m, len(v.Modifiers))
	}
}

func TestReadVocabAbsent(t *testing.T) {
	v := ReadVocab([]string{filepath.Join(t.TempDir(), "nothing")})
	if v == nil || len(v.Effects)+len(v.Triggers)+len(v.Links)+len(v.Modifiers)+len(v.OnActions)+len(v.ScopeTypes) != 0 {
		t.Errorf("a game with no dumps gave %+v", v)
	}
	if got := v.TokenParams("every_vassal"); got != nil {
		t.Errorf("TokenParams on an empty vocabulary = %v", got)
	}
}

func TestTokenParams(t *testing.T) {
	v := &Vocab{
		Triggers: map[string]EngineToken{
			"any_trait_in_category": {Usage: "any_trait_in_category = { category = fame/health/etc }\nany_trait_in_category = { <count=num/all> / <percent=fixed_point> <triggers> }"},
			"any_county_in_region":  {Usage: "any_county_in_region = { <count=num/all> / <percent=fixed_point> <triggers> }"},
		},
		Effects: map[string]EngineToken{
			"random_trait_in_category": {Usage: "random_trait_in_category = { limit = { <triggers> } (optional) weight = { mtth } <effects> }"},
			"every_vassal":             {Usage: "every_vassal = { limit = { <triggers> } <effects> }"},
			"add_gold":                 {Usage: "add_gold = 5"},
			"set_variable":             {Usage: "set_variable = {\n\tname = X\n\tvalue = { value = 1 add = 2 }\n\tyears = 3\n}"},
		},
	}
	for name, want := range map[string][]string{
		"any_trait_in_category":    {"category", "count", "limit", "percent", "weight"},
		"random_trait_in_category": {"category", "count", "limit", "percent", "weight"},
		"every_vassal":             {"limit"},
		"any_county_in_region":     {"count", "percent"},
		"set_variable":             {"name", "value", "years"},
		"add_gold":                 nil,
		"not_a_token":              nil,
	} {
		if got := v.TokenParams(name); !slices.Equal(got, want) {
			t.Errorf("TokenParams(%s) = %v, want %v", name, got, want)
		}
	}
}

func TestUsableIn(t *testing.T) {
	for _, c := range []struct {
		in    []string
		scope string
		want  bool
	}{
		{nil, "country", true},
		{[]string{"none"}, "country", true},
		{[]string{"country"}, "country", true},
		{[]string{"country", "state"}, "state", true},
		{[]string{"country"}, "character", false},
		{[]string{"country"}, "", true},
	} {
		if got := UsableIn(c.in, c.scope); got != c.want {
			t.Errorf("UsableIn(%v, %q) = %v", c.in, c.scope, got)
		}
	}
}

// realVocab reads the dumps of one game, or reports why it cannot.
func realVocab(t *testing.T, id, env string) *Vocab {
	t.Helper()
	install := os.Getenv(env)
	if install == "" {
		t.Errorf("%s is not set: the gate cannot clear a game it never read", env)
		return nil
	}
	user := game.UserDataDir(id, install)
	return ReadVocab([]string{filepath.Join(user, "logs"), filepath.Join(user, "docs")})
}

// TestDumpsAreRead is the gate over the real console dumps: the script_docs output of all three games is read, every field of the vocabulary types is filled by at least one of them, the prefixes the games declare are there, and every token of the name-token table is one some game declares. It needs the dumps in each game's user folder (run script_docs in the console once), so it runs only when asked: set PMT_SCHEMA_GATE=1 and the three PMT_TEST_INSTALL_ variables, each a game's root folder.
func TestDumpsAreRead(t *testing.T) {
	if testing.Short() || os.Getenv("PMT_SCHEMA_GATE") == "" {
		t.Skip("reads the three games' dumps; set PMT_SCHEMA_GATE=1")
	}

	filled := map[string]int{}
	for _, g := range []struct {
		id, env                                          string
		effects, triggers, links, scopes, onActions, mod int
		prefixes                                         map[string]string
	}{
		{"ck3", "PMT_TEST_INSTALL_CK3", 1800, 1700, 250, 60, 800, 1, map[string]string{"culture": "culture", "faith": "faith", "title": "landed_title", "character": "character", "trait": "trait", "province": "province"}},
		{"vic3", "PMT_TEST_INSTALL_VIC3", 3000, 1500, 320, 60, 100, 1, map[string]string{"c": "country", "cu": "culture", "s": "state_region", "p": "province"}},
		{"eu5", "PMT_TEST_INSTALL_EU5", 1400, 1700, 280, 60, 100, 1, map[string]string{"c": "country", "culture": "culture", "location": "location"}},
	} {
		v := realVocab(t, g.id, g.env)
		if v == nil {
			continue
		}
		t.Logf("VOCAB %s: %d effects, %d triggers, %d links, %d scope types, %d on_actions, %d modifiers, %d engine-saved scopes, unknown labels %v",
			g.id, len(v.Effects), len(v.Triggers), len(v.Links), len(v.ScopeTypes), len(v.OnActions), len(v.Modifiers), len(v.SavedScopes), v.Unknown)
		if len(v.Effects) < g.effects || len(v.Triggers) < g.triggers || len(v.Links) < g.links || len(v.ScopeTypes) < g.scopes || len(v.OnActions) < g.onActions || len(v.Modifiers) < g.mod || len(v.SavedScopes) == 0 {
			t.Errorf("%s: fewer entries than the game is known to declare", g.id)
		}
		if len(v.Unknown) > 0 {
			t.Errorf("%s: labels the reader does not know: %v", g.id, v.Unknown)
		}

		// the prefixes the game declares, and what each leads to
		for prefix, out := range g.prefixes {
			if l := v.Links[prefix]; !l.Prefix || l.Out != out {
				t.Errorf("%s: link %s = %+v, want a prefix that leads to %s", g.id, prefix, l, out)
			}
		}

		// which fields this game's dumps fill
		count := func(field string, n int) {
			filled[field] += n
			t.Logf("  %s %-22s %d", g.id, field, n)
		}
		var doc, usage, in, out, several int
		for _, bag := range []map[string]EngineToken{v.Effects, v.Triggers} {
			for _, tok := range bag {
				doc, usage, in = doc+min(len(tok.Doc), 1), usage+min(len(tok.Usage), 1), in+min(len(tok.In), 1)
				out += min(len(tok.Out), 1)
				if len(commaList(tok.Out)) > 1 {
					several++
				}
			}
		}
		count("EngineToken.Doc", doc)
		count("EngineToken.Usage", usage)
		count("EngineToken.In", in)
		count("EngineToken.Out", out)
		t.Logf("  %s tokens with several targets: %d", g.id, several)

		var linkDoc, linkIn, linkOut, linkPrefix, linkSeveral int
		for _, l := range v.Links {
			linkDoc, linkIn, linkOut = linkDoc+min(len(l.Doc), 1), linkIn+min(len(l.In), 1), linkOut+min(len(l.Out), 1)
			if l.Prefix {
				linkPrefix++
			}
			if len(commaList(l.Out)) > 1 {
				linkSeveral++
			}
		}
		count("Link.Doc", linkDoc)
		count("Link.In", linkIn)
		count("Link.Out", linkOut)
		count("Link.Prefix", linkPrefix)
		t.Logf("  %s links with several outputs: %d", g.id, linkSeveral)

		var modDoc, modCategories int
		for _, m := range v.Modifiers {
			modDoc, modCategories = modDoc+min(len(m.Doc), 1), modCategories+min(len(m.Categories), 1)
		}
		count("Modifier.Doc", modDoc)
		count("Modifier.Categories", modCategories)

		// the prefix of every script-made kind is one the game declares
		for kind, prefix := range scriptKinds {
			if prefix != "" && !v.Links[prefix].Prefix {
				t.Errorf("%s: %s are cited by %s:, which the game does not declare as a prefix", g.id, kind, prefix)
			}
		}

		// every token of the hand table is one some game declares, checked over the three together below
		for _, row := range nameTokens {
			if _, ok := v.Effects[row.Token]; ok {
				filled["token "+row.Token]++
			} else if _, ok := v.Triggers[row.Token]; ok {
				filled["token "+row.Token]++
			}
		}
	}

	for _, field := range []string{"EngineToken.Doc", "EngineToken.Usage", "EngineToken.In", "EngineToken.Out", "Link.Doc", "Link.In", "Link.Out", "Link.Prefix", "Modifier.Doc", "Modifier.Categories"} {
		if filled[field] == 0 {
			t.Errorf("no dump of any game fills %s", field)
		}
	}
	for _, row := range nameTokens {
		if filled["token "+row.Token] == 0 {
			t.Errorf("no game declares the name token %s", row.Token)
		}
	}
}
