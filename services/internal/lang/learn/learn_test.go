// learn_test.go covers the learner's two entries on a small install written to disk, the fixture schemas, and the gate over the real installs. It also holds what the package's other tests share: the vocabulary and the install on disk.
package learn

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/loc"
	"paradox-modding-tools/services/internal/lang/schema"
	"paradox-modding-tools/services/internal/source"
)

// vocab is a vocabulary as the dumps declare one: block takers by their usage, logic words with none, links with their outputs.
func vocab() *schema.Vocab {
	return &schema.Vocab{
		Effects: map[string]schema.EngineToken{
			"if":            {Name: "if", Usage: "if = { limit = { <triggers> } <effects> }"},
			"every_vassal":  {Name: "every_vassal", Usage: "every_vassal = { limit = { <triggers> } <effects> }", In: []string{"character"}, Out: "character"},
			"hidden_effect": {Name: "hidden_effect"},
			"add_gold":      {Name: "add_gold", In: []string{"character"}},
			"add_trait":     {Name: "add_trait", In: []string{"character"}},
			"set_father":    {Name: "set_father", In: []string{"character"}, Out: "character"},
			"save_scope_as": {Name: "save_scope_as"},
			"make_name":     {Name: "make_name"},
		},
		Triggers: map[string]schema.EngineToken{
			"any_child":  {Name: "any_child", Usage: "any_child = { <count=num/all> <triggers> }", In: []string{"character"}, Out: "character"},
			"trigger_if": {Name: "trigger_if", Usage: "trigger_if = { limit = { <display_triggers> } <triggers> }"},
			"not":        {Name: "not"},
			"or":         {Name: "or"},
			"has_trait":  {Name: "has_trait", In: []string{"character"}},
			"is_adult":   {Name: "is_adult", In: []string{"character"}},
			"gold":       {Name: "gold", In: []string{"character"}},
			"prestige":   {Name: "prestige", In: []string{"character"}},
			"martial":    {Name: "martial", In: []string{"character"}},
		},
		Modifiers: map[string]schema.Modifier{"monthly_prestige": {Name: "monthly_prestige"}},
		Links: map[string]schema.Link{
			"title":     {Name: "title", Prefix: true, Out: "landed_title"},
			"character": {Name: "character", Prefix: true, Out: "character"},
			"trait":     {Name: "trait", Prefix: true, Out: "trait"},
			"define":    {Name: "define", Prefix: true, Out: "value"},
			"scope":     {Name: "scope", Prefix: true},
			"father":    {Name: "father", Out: "character"},
			"liege":     {Name: "liege", Out: "character"},
			"value":     {Name: "value", Out: "value"},
			"root":      {Name: "root"},
		},
	}
}

// install writes files under a fresh folder and gives its inventory.
func install(t *testing.T, files map[string]string) []source.File {
	t.Helper()
	dir := t.TempDir()
	for rel, text := range files {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return source.Inventory("vanilla", []string{dir})
}

// counted is the census of an install, with the witnesses worked out.
func counted(t *testing.T, files map[string]string, keys ...string) *census {
	t.Helper()
	c := newCensus(vocab())
	have := map[string]bool{}
	for _, k := range keys {
		have[k] = true
	}
	c.keys = loc.NewKeys(have)
	inventory := install(t, files)
	for _, f := range inventory {
		rel := strings.ToLower(f.Rel)
		c.files[rel], c.files[filepath.Base(rel)] = true, true
	}
	if _, err := c.read(context.Background(), inventory); err != nil {
		t.Fatal(err)
	}
	c.tables(inventory)
	c.witnesses()
	return c
}

// learned is the schema of an install.
func learned(t *testing.T, files map[string]string, subjects []string, keys ...string) *schema.Schema {
	t.Helper()
	have := map[string]bool{}
	for _, k := range keys {
		have[k] = true
	}
	s, err := Learn(context.Background(), Input{Game: "ck3", Version: "test", Files: install(t, files), Vocab: vocab(), Loc: loc.NewKeys(have), EntryModes: []string{"INJECT"}, Subjects: subjects})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// names is the sorted names of a schema's kinds that files define: the script-made kinds are the same in every schema.
func names(s *schema.Schema) []string {
	var out []string
	for name, k := range s.Kinds {
		if k.Naming.Form != "token" && k.Naming.Form != "constant" && k.Naming.Form != "param" {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return out
}

// miniInstall is a small install that holds one of each thing the learner tells apart.
var miniInstall = map[string]string{
	"common/traits/00_traits.txt":                 "brave = { index = 1 opposites = { craven } monthly_prestige = 1 }\ncraven = { index = 2 opposites = { brave } }\nshy = { index = 3 }\n",
	"common/scripted_effects/00_effects.txt":      "pay_up_effect = { if = { limit = { has_trait = brave } add_gold = 10 } }\nreward_effect = { hidden_effect = { if = { limit = { is_adult = yes } add_gold = 5 } } pay_up_effect = yes }\nthird_effect = { reward_effect = yes pay_up_effect = yes }\n",
	"common/scripted_triggers/00_trig.txt":        "is_bold_trigger = { NOT = { any_child = { has_trait = craven } } }\nis_rich_trigger = { any_child = { gold > 5 } is_bold_trigger = yes big_value > 3 }\n",
	"common/script_values/00_values.txt":          "small_value = 5\nbig_value = { value = 10 if = { limit = { is_bold_trigger = yes } add = 5 } multiply = 2 }\nother_value = { value = small_value add = 1 }\n",
	"common/decisions/00_decisions.txt":           "pay_decision = { cost = { gold = 100 prestige = 50 } is_shown = { is_adult = yes is_rich_trigger = yes OR = { has_trait = brave trigger_if = { limit = { is_adult = yes } has_trait = shy } } } effect = { every_vassal = { add_trait = brave } reward_effect = yes } ai_will_do = { base = 10 } }\nrest_decision = { cost = { gold = 5 } is_shown = { NOT = { has_trait = shy } } effect = { if = { limit = { gold > big_value } add_gold = small_value } } ai_will_do = { base = 0 } }\n",
	"common/landed_titles/00_titles.txt":          "e_north = { color = hsv { 0.1 0.2 0.3 } k_upper = { color = hsv { 0.2 0.2 0.2 } d_mid = { color = hsv { 0.3 0.3 0.3 } c_low = { color = hsv { 0.4 0.4 0.4 } } } } }\ne_south = { color = hsv { 0.5 0.5 0.5 } k_lower = { color = hsv { 0.6 0.6 0.6 } } }\n",
	"history/titles/north.txt":                    "e_north = { 1000.1.1 = { holder = 1 } }\nk_upper = { 1000.1.1 = { holder = 2 liege = title:e_north } }\nc_low = { 1000.1.1 = { holder = 2 liege = title:k_upper } }\n",
	"history/characters/people.txt":               "1 = { name = \"Ada\" trait = brave 1000.1.1 = { birth = yes } }\n2 = { name = \"Bo\" father = 1 trait = shy 1000.1.1 = { birth = yes } }\n3 = { name = \"Cy\" father = 1 trait = craven 1001.1.1 = { birth = yes } }\n",
	"common/coat_of_arms/coat_of_arms/00.txt":     "e_north = { pattern = \"pattern_solid.dds\" color1 = red }\nk_upper = { pattern = \"pattern_solid.dds\" color1 = blue }\ne_south = { pattern = \"pattern_solid.dds\" color1 = red }\nk_lower = { pattern = \"pattern_solid.dds\" color1 = blue }\n",
	"common/defines/00_defines.txt":               "NGame = { START_DATE = \"1000.1.1\" MAX_AGE = 100 }\nNWar = { WAR_COST = 5 SIEGE_DAYS = 30 }\n",
	"events/first_events.txt":                     "namespace = first\nscripted_trigger first_inline_trigger = { is_adult = yes }\nfirst.1 = { type = character_event title = first.1.t trigger = { is_adult = yes first_inline_trigger = yes martial >= define:NGame|MAX_AGE NOT = { is_rich_trigger = yes } } immediate = { save_scope_as = hero character:1 = { add_gold = define:NWar|WAR_COST } title:c_low = { } } option = { name = first.1.a add_gold = 1 } }\nfirst.2 = { type = character_event title = first.2.t immediate = { scope:hero = { add_trait = brave } title:e_south = { } } option = { name = first.2.a trigger = { has_trait = trait:brave } add_gold = 2 } }\n",
	"events/more_events.txt":                      "namespace = more\nmore.1 = { type = character_event title = first.1.t option = { name = first.1.a add_gold = 4 } }\n",
	"events/sub/third_events.txt":                 "namespace = third\nthird.1 = { type = character_event title = second.1.t option = { name = second.1.a add_gold = 3 } }\nthird.2 = { type = character_event title = second.1.t option = { name = second.1.a add_gold = 3 } }\n",
	"events/sub/second_events.txt":                "namespace = second\nsecond.1 = { type = character_event title = second.1.t option = { name = second.1.a add_gold = 3 } }\nsecond.2 = { type = character_event title = second.1.t option = { name = second.1.a add_gold = 3 } }\n",
	"gfx/coat_of_arms/patterns/pattern_solid.dds": "DDS ",
}

func TestLearn(t *testing.T) {
	s := learned(t, miniInstall, []string{"history", "common/coat_of_arms"}, "brave", "craven", "shy", "first.1.t", "first.2.t", "second.1.t", "first.1.a", "first.2.a", "second.1.a")

	// the kinds, each under the name its folder gives it; history and arms keep their full folder
	want := []string{"characters", "common/coat_of_arms/coat_of_arms", "decisions", "defines", "defines.define", "events", "history/titles", "landed_titles", "script_values", "scripted_effects", "scripted_triggers", "traits"}
	if got := names(s); !slices.Equal(got, want) {
		t.Fatalf("kinds = %v\nwant    %v", got, want)
	}

	for _, tt := range []struct {
		kind, what string
		got, want  any
	}{
		{"events", "form", s.Kinds["events"].Naming.Form, "event_id"},
		{"events", "folders", strings.Join(s.Kinds["events"].Folders, " "), "events events/sub"},
		{"characters", "form", s.Kinds["characters"].Naming.Form, "number"},
		{"characters", "label member", s.Kinds["characters"].LabelMember, "name"},
		{"characters", "prefix", strings.Join(s.Kinds["characters"].Prefixes, " "), "character"},
		{"characters", "scope type", s.Kinds["characters"].ScopeType, "character"},
		{"history/titles", "subject", s.Kinds["history/titles"].Subject, "landed_titles"},
		{"coat of arms", "subject", s.Kinds["common/coat_of_arms/coat_of_arms"].Subject, "landed_titles"},
		{"coat of arms", "owns no prefix", len(s.Kinds["common/coat_of_arms/coat_of_arms"].Prefixes), 0},
		{"landed_titles", "prefix", strings.Join(s.Kinds["landed_titles"].Prefixes, " "), "title"},
		{"landed_titles", "nested in itself", s.Kinds["landed_titles"].Shape.Defines, "landed_titles"},
		{"defines.define", "parent", s.Kinds["defines.define"].Parent, "defines"},
		{"defines.define", "prefix", strings.Join(s.Kinds["defines.define"].Prefixes, " "), "define"},
		{"scripted_triggers", "keyword", s.Kinds["scripted_triggers"].Naming.Keyword, "scripted_trigger"},
		{"scripted_triggers", "macro", s.Kinds["scripted_triggers"].Macro, schema.BodyTrigger},
		{"scripted_effects", "macro", s.Kinds["scripted_effects"].Macro, schema.BodyEffect},
		{"script_values", "macro", s.Kinds["script_values"].Macro, schema.BodyTrigger},
		{"script_values", "body", s.Kinds["script_values"].Shape.Body, schema.BodyMath},
		{"traits", "class", s.Kinds["traits"].Shape.Class, schema.ClassStructure},
		{"decisions", "cost is a structure", s.Kinds["decisions"].Shape.Members["cost"].Class, schema.ClassStructure},
		{"decisions", "is_shown is a trigger body", s.Kinds["decisions"].Shape.Members["is_shown"].Body, schema.BodyTrigger},
		{"decisions", "is_shown runs in", s.Kinds["decisions"].Shape.Members["is_shown"].BodyScope, "character"},
		{"decisions", "effect is an effect body", s.Kinds["decisions"].Shape.Members["effect"].Body, schema.BodyEffect},
		{"arguments", "if is an effect body", s.Arguments["if"].Body, schema.BodyEffect},
		{"arguments", "if's limit is a trigger body", s.Arguments["if"].Members["limit"].Body, schema.BodyTrigger},
		{"arguments", "not is a trigger body", s.Arguments["not"].Body, schema.BodyTrigger},
		{"arguments", "hidden_effect is an effect body", s.Arguments["hidden_effect"].Body, schema.BodyEffect},
	} {
		if tt.got != tt.want {
			t.Errorf("%s %s = %v, want %v", tt.kind, tt.what, tt.got, tt.want)
		}
	}

	// the schema is a file: it reads back as it was written
	text := schema.Write(s)
	back, err := schema.Read(text)
	if err != nil || schema.Write(back) != text {
		t.Errorf("the schema does not read back: %v", err)
	}
}

func TestRelearn(t *testing.T) {
	base := learned(t, miniInstall, []string{"history", "common/coat_of_arms"})
	mod := map[string]string{
		"common/traits/zz_mod.txt":     "bold = { index = 9 typo_member = yes }\nmeek = { index = 10 }\n",
		"common/mod_things/things.txt": "first_thing = { weight = 1 }\nsecond_thing = { weight = 2 }\n",
	}
	s, err := Relearn(context.Background(), Input{Game: "ck3", Version: "test", Files: install(t, mod), Vocab: vocab(), Loc: loc.NewKeys(nil)}, base)
	if err != nil {
		t.Fatal(err)
	}
	if s.Kinds["mod_things"] == nil {
		t.Errorf("a folder the install has no kind for is a kind of the workspace: %v", names(s))
	}
	if s.Kinds["traits"] != base.Kinds["traits"] || s.Kinds["traits"].Shape.Members["typo_member"] != nil {
		t.Error("a mod never adds a member to a kind the install learned")
	}
}

// scanned learns an install as a scan does: its files, its english loc, its game info, the dumps in docs, and the registry's rows for the game.
func scanned(t *testing.T, id, install string, docs ...string) *schema.Schema {
	t.Helper()
	files, keys := source.Inventory("vanilla", source.Roots(install)), map[string]bool{}
	for _, f := range files {
		if f.Format == source.Loc && loc.LanguageFromRel(f.Rel) == "english" {
			text, _ := f.Text()
			for _, e := range loc.Parse(text).Entries {
				keys[e.Key] = true
			}
		}
	}
	var modes []string
	for _, m := range game.Get(id).EntryModes {
		modes = append(modes, m.Word)
	}
	s, err := Learn(context.Background(), Input{Game: id, Version: "test", Files: files, Vocab: schema.ReadVocab(docs), Info: schema.ReadInfo(files), Loc: loc.NewKeys(keys), EntryModes: modes, Subjects: game.Get(id).SubjectDirs})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// TestFixturesAreLearned learns the three fixture installs and compares each schema with the one committed beside it. A change to the learner that changes a schema shows in the diff of schema.txt: run with PMT_SCHEMA_UPDATE=1 to write them again, and read the diff.
func TestFixturesAreLearned(t *testing.T) {
	for _, id := range []string{"ck3", "vic3", "eu5"} {
		dir := filepath.Join("..", "testdata", id)
		got := schema.Write(scanned(t, id, dir, filepath.Join(dir, "docs")))
		if os.Getenv("PMT_SCHEMA_UPDATE") != "" {
			if err := os.WriteFile(filepath.Join(dir, "schema.txt"), []byte(got), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		want, err := os.ReadFile(filepath.Join(dir, "schema.txt"))
		if err != nil || got != string(want) {
			t.Errorf("%s: the learned schema is not testdata/%s/schema.txt (%v)", id, id, err)
		}
		if back, err := schema.Read(got); err != nil || schema.Write(back) != got {
			t.Errorf("%s: the schema does not read back: %v", id, err)
		}
	}
}

// TestInstallsAreLearned is the gate over the real installs: each game is learned with the one code path, its schema reads back, the kinds invoked as keys are the four script kinds and no other, two scans write the same file, and the facts the defect list of the plan's §19 names hold. It needs the installs and their script_docs dumps, so it runs only when asked: set PMT_LEARN_GATE=1 and the three PMT_TEST_INSTALL_ variables, each a game's root folder.
func TestInstallsAreLearned(t *testing.T) {
	if testing.Short() || os.Getenv("PMT_LEARN_GATE") == "" {
		t.Skip("learns the three installs; set PMT_LEARN_GATE=1")
	}
	written := map[string]string{}
	for _, g := range []struct {
		id, env  string
		prefixes map[string]string // a prefix to the kind that owns it
		subjects map[string]string // a subject kind to the kind it is about
		nested   map[string]string // a nested kind to its parent
	}{
		{"ck3", "PMT_TEST_INSTALL_CK3",
			map[string]string{"character": "characters", "title": "landed_titles", "dynasty": "dynasties", "house": "dynasty_houses", "trait": "traits", "culture": "cultures", "define": "defines.define"},
			map[string]string{"history/titles": "landed_titles", "history/provinces": "map_data.definition", "history/cultures": "cultures"},
			map[string]string{"game_rules.setting": "game_rules", "defines.define": "defines"}},
		{"vic3", "PMT_TEST_INSTALL_VIC3",
			map[string]string{"c": "country_definitions", "p": "map_data", "define": "defines.define"},
			map[string]string{"common/flag_definitions": "country_definitions"},
			map[string]string{"game_rules.setting": "game_rules"}},
		{"eu5", "PMT_TEST_INSTALL_EU5",
			map[string]string{"c": "countries", "culture": "cultures", "trait": "traits"},
			map[string]string{"common/flag_definitions": "countries"},
			map[string]string{}},
	} {
		install := os.Getenv(g.env)
		if install == "" {
			t.Errorf("%s: %s is not set", g.id, g.env)
			continue
		}
		user := game.UserDataDir(g.id, install)
		s := scanned(t, g.id, install, filepath.Join(user, "logs"), filepath.Join(user, "docs"))
		text := schema.Write(s)
		written[g.id] = text
		if back, err := schema.Read(text); err != nil || len(back.Kinds) != len(s.Kinds) {
			t.Errorf("%s: the schema does not read back: %v", g.id, err)
		}

		var macros []string
		for name, k := range s.Kinds {
			if k.Macro != 0 {
				macros = append(macros, name)
			}
		}
		slices.Sort(macros)
		if want := []string{"script_values", "scripted_effects", "scripted_lists", "scripted_triggers"}; !slices.Equal(macros, want) {
			t.Errorf("%s: macro kinds = %v, want %v", g.id, macros, want)
		}
		for prefix, kind := range g.prefixes {
			if k := s.Kinds[kind]; k == nil || !slices.Contains(k.Prefixes, prefix) {
				t.Errorf("%s: %s: is not %s's prefix", g.id, prefix, kind)
			}
		}
		for kind, about := range g.subjects {
			if k := s.Kinds[kind]; k == nil || k.Subject != about {
				t.Errorf("%s: %s is not about %s", g.id, kind, about)
			}
		}
		for kind, parent := range g.nested {
			if k := s.Kinds[kind]; k == nil || k.Parent != parent {
				t.Errorf("%s: %s is not nested in %s", g.id, kind, parent)
			}
		}
		if f := s.Arguments["if"]; f == nil || f.Body != schema.BodyEffect || f.Members["limit"] == nil || f.Members["limit"].Body != schema.BodyTrigger {
			t.Errorf("%s: if is an effect body whose limit is a trigger body: %+v", g.id, f)
		}
		if k := s.Kinds["script_values"]; k == nil || k.Shape.Body != schema.BodyMath {
			t.Errorf("%s: a script value computes a number", g.id)
		}
		t.Logf("%s: %d kinds, %d arguments, %d bytes", g.id, len(s.Kinds), len(s.Arguments), len(text))
	}

	// a second scan of one install writes the same file, and shows the defects of §19 that only CK3 has
	s := scanned(t, "ck3", os.Getenv("PMT_TEST_INSTALL_CK3"), filepath.Join(game.UserDataDir("ck3", os.Getenv("PMT_TEST_INSTALL_CK3")), "logs"))
	if schema.Write(s) != written["ck3"] {
		t.Error("ck3: two scans of one install wrote different schemas")
	}
	for _, tt := range []struct {
		what      string
		got, want any
	}{
		{"1: a character's history is a structure, labelled by its name", s.Kinds["characters"].Shape.Class.String() + " " + s.Kinds["characters"].LabelMember, "structure name"},
		{"1: a decision's cost is a structure though every key in it is a trigger's name", s.Kinds["decisions"].Shape.Members["cost"].Class, schema.ClassStructure},
		{"4: a message is a structure", s.Kinds["messages"].Shape.Class, schema.ClassStructure},
		{"7: scripted effects are declared inline by their keyword and take parameters", s.Kinds["scripted_effects"].Naming.Keyword + " " + fmt.Sprint(s.Kinds["scripted_effects"].Params), "scripted_effect true"},
		{"9: a message's display is a closed set of words", s.Kinds["messages"].Shape.Members["display"].Accepts[0].Type, schema.TypeEnum},
		{"12: history/cultures is named by its files", s.Kinds["history/cultures"].Naming.Form, "file"},
		{"landed titles are one kind nested in itself", s.Kinds["landed_titles"].Shape.Defines, "landed_titles"},
	} {
		if tt.got != tt.want {
			t.Errorf("ck3 defect %s: %v, want %v", tt.what, tt.got, tt.want)
		}
	}
}
