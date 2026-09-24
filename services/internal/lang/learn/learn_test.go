// learn_test.go covers the learner on small installs written to disk: the census and its drift,
// how folders become kinds (words, side-by-side files, nested kinds, subjects), the class of a
// member, what a field accepts, hoisted containers, the fixture schemas, and the gate over the
// real installs. Its helpers are the vocabulary and the install on disk.
package learn

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/loc"
	"paradox-modding-tools/services/internal/lang/schema"
	"paradox-modding-tools/services/internal/lang/source"
)

// vocab is a vocabulary as the dumps declare one: block takers by their usage, logic words with
// none, links with their outputs.
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
	census := newCensus(vocab())
	have := map[string]bool{}
	for _, key := range keys {
		have[key] = true
	}
	census.keys = loc.NewKeys(have)
	inventory := install(t, files)
	for _, file := range inventory {
		rel := strings.ToLower(file.Rel)
		census.files[rel], census.files[filepath.Base(rel)] = true, true
	}
	if _, err := census.read(t.Context(), inventory, nil); err != nil {
		t.Fatal(err)
	}
	census.tables(inventory)
	census.witnesses()
	return census
}

// learned is the schema of an install.
func learned(t *testing.T, files map[string]string, subjects []string, keys ...string) *schema.Schema {
	t.Helper()
	have := map[string]bool{}
	for _, key := range keys {
		have[key] = true
	}
	sch, err := Learn(t.Context(), Input{Game: "ck3", Version: "test", Files: install(t, files), Vocab: vocab(), Loc: loc.NewKeys(have), EntryModes: []string{"INJECT"}, Subjects: subjects})
	if err != nil {
		t.Fatal(err)
	}
	return sch
}

// names is the sorted names of a schema's kinds that files define: the script-made kinds are
// the same in every schema.
func names(sch *schema.Schema) []string {
	var out []string
	for name, kind := range sch.Kinds {
		if kind.Naming.Form != "token" && kind.Naming.Form != "constant" && kind.Naming.Form != "param" {
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
	sch := learned(t, miniInstall, []string{"history", "common/coat_of_arms"}, "brave", "craven", "shy", "first.1.t", "first.2.t", "second.1.t", "first.1.a", "first.2.a", "second.1.a")

	// the kinds, each under the name its folder gives it; history and arms keep their full folder
	want := []string{"characters", "common/coat_of_arms/coat_of_arms", "decisions", "defines", "defines.define", "events", "history/titles", "landed_titles", "script_values", "scripted_effects", "scripted_triggers", "traits"}
	if got := names(sch); !slices.Equal(got, want) {
		t.Fatalf("kinds = %v\nwant    %v", got, want)
	}

	for _, testCase := range []struct {
		kind, what string
		got, want  any
	}{
		{"events", "form", sch.Kinds["events"].Naming.Form, "event_id"},
		{"events", "folders", strings.Join(sch.Kinds["events"].Folders, " "), "events events/sub"},
		{"characters", "form", sch.Kinds["characters"].Naming.Form, "number"},
		{"characters", "label member", sch.Kinds["characters"].LabelMember, "name"},
		{"characters", "prefix", strings.Join(sch.Kinds["characters"].Prefixes, " "), "character"},
		{"characters", "scope type", sch.Kinds["characters"].ScopeType, "character"},
		{"history/titles", "subject", sch.Kinds["history/titles"].Subject, "landed_titles"},
		{"coat of arms", "subject", sch.Kinds["common/coat_of_arms/coat_of_arms"].Subject, "landed_titles"},
		{"coat of arms", "owns no prefix", len(sch.Kinds["common/coat_of_arms/coat_of_arms"].Prefixes), 0},
		{"landed_titles", "prefix", strings.Join(sch.Kinds["landed_titles"].Prefixes, " "), "title"},
		{"landed_titles", "nested in itself", sch.Kinds["landed_titles"].Shape.Defines, "landed_titles"},
		{"defines.define", "parent", sch.Kinds["defines.define"].Parent, "defines"},
		{"defines.define", "prefix", strings.Join(sch.Kinds["defines.define"].Prefixes, " "), "define"},
		{"scripted_triggers", "keyword", sch.Kinds["scripted_triggers"].Naming.Keyword, "scripted_trigger"},
		{"scripted_triggers", "macro", sch.Kinds["scripted_triggers"].Macro, schema.BodyTrigger},
		{"scripted_effects", "macro", sch.Kinds["scripted_effects"].Macro, schema.BodyEffect},
		{"script_values", "macro", sch.Kinds["script_values"].Macro, schema.BodyTrigger},
		{"script_values", "body", sch.Kinds["script_values"].Shape.Body, schema.BodyMath},
		{"traits", "class", sch.Kinds["traits"].Shape.Class, schema.ClassStructure},
		{"decisions", "cost is a structure", sch.Kinds["decisions"].Shape.Members["cost"].Class, schema.ClassStructure},
		{"decisions", "is_shown is a trigger body", sch.Kinds["decisions"].Shape.Members["is_shown"].Body, schema.BodyTrigger},
		{"decisions", "is_shown runs in", sch.Kinds["decisions"].Shape.Members["is_shown"].BodyScope, "character"},
		{"decisions", "effect is an effect body", sch.Kinds["decisions"].Shape.Members["effect"].Body, schema.BodyEffect},
		{"arguments", "if is an effect body", sch.Arguments["if"].Body, schema.BodyEffect},
		{"arguments", "if's limit is a trigger body", sch.Arguments["if"].Members["limit"].Body, schema.BodyTrigger},
		{"arguments", "not is a trigger body", sch.Arguments["not"].Body, schema.BodyTrigger},
		{"arguments", "hidden_effect is an effect body", sch.Arguments["hidden_effect"].Body, schema.BodyEffect},
	} {
		if testCase.got != testCase.want {
			t.Errorf("%s %s = %v, want %v", testCase.kind, testCase.what, testCase.got, testCase.want)
		}
	}
}

func TestCensus(t *testing.T) {
	var many strings.Builder
	for i := range maxMembers + 10 {
		fmt.Fprintf(&many, "name_%d = { weight = %d }\n", i, i)
	}
	many.WriteString("add_gold = { weight = 1 }\n")
	counts := counted(t, map[string]string{
		"common/things/a.txt":    "@cost = 5\nalpha = { weight = 1 tags = { x y } 1000.1.1 = { on = yes } 12 = { on = no } title:k_a = { on = yes } color = hsv { 0.1 0.2 0.3 } }\nINJECT:beta = { weight > 2 weight = \"two\" }\n",
		"common/things/b.txt":    "alpha = { weight = $W$ }\n7 = { weight = 1 }\n",
		"common/on_action/o.txt": "on_birth = { events = { delay = { days = 1 } first.1 } }\n",
		"common/many/a.txt":      many.String(),
	})
	things := counts.folders["common/things"]
	alpha, weight := things.root.members["alpha"], things.root.members["alpha"].members["weight"]
	root := counts.folders["common/many"].root

	for _, testCase := range []struct {
		what      string
		got, want any
	}{
		{"files of the folder", things.files, 2},
		{"a constant is no member", things.root.members["@cost"] == nil && counts.constants["cost"] == 1, true},
		{"an entry mode is split from the name", things.root.members["beta"] != nil && counts.modes["INJECT"] == 1, true},
		{"a numeric top-level key is recorded", things.numbered["7"], 1},
		{"alpha is written twice, in two files", [2]int{alpha.count, alpha.in}, [2]int{2, 2}},
		{"weight: times, blocks holding it, numbers, params", [4]int{weight.count, weight.in, weight.numbers, weight.params}, [4]int{2, 2, 1, 1}},
		{"an operator other than = is kept", things.root.members["beta"].members["weight"].ops[">"], 1},
		{"a quoted value is counted", things.root.members["beta"].members["weight"].quoted, 1},
		{"a date key, a number key and a cite key each share a position", alpha.members["<date>"] != nil && alpha.members["<number>"] != nil && alpha.members["<cite>"] != nil, true},
		{"the cite is counted under its prefix", counts.cites["title"]["k_a"], 1},
		{"bare words stand at <word>", alpha.members["tags"].members["<word>"].count, 2},
		{"a block's tag is kept", alpha.members["color"].tags["hsv"], 1},
		{"inside a block a bare word before delay = { } is a list's word, no keyword", len(counts.folders["common/on_action"].keywords), 0},
		{"past capacity a key shares <name>: members, names, at <name>", [3]int{len(root.members), len(root.names), root.members["<name>"].count}, [3]int{maxMembers + 1, 11, 10}},
		{"past capacity a declared token is counted with the token", counts.tokens["add_gold"] != nil, true},
		{"a position whose values are mostly their own keeps every word", len(root.members["<name>"].members["weight"].words), 10},
	} {
		if testCase.got != testCase.want {
			t.Errorf("%s = %v, want %v", testCase.what, testCase.got, testCase.want)
		}
	}
}

func TestDrift(t *testing.T) {
	var made strings.Builder
	for i := range maxMembers + 1 {
		fmt.Fprintf(&made, "effect_%d = { add_gold = %d }\n", i, i)
	}
	made.WriteString("maker = { make_name = fresh_name make_name = other_name add_trait = brave }\nreader = { if = { limit = { scope:fresh_name = { is_adult = yes } scope:other_name = { is_adult = yes } scope:brave = { is_adult = yes } } } }\n")
	sch := learned(t, map[string]string{
		"common/scripted_effects/many.txt": made.String(),
		"common/traits/t.txt":              "brave = { index = 1 }\nshy = { index = 2 }\n",
		"common/things/a.txt":              "INJECT:one = { weight = 1 }\nREPLACE:two = { weight = usr:custom }\nthree = { weight = 3 broken = { }\n",
		"stray/odd.xyz":                    "?",
	}, nil)
	tally := func(tallies []schema.Tally) string {
		var out []string
		for _, entry := range tallies {
			out = append(out, fmt.Sprintf("%s=%d", entry.Name, entry.Count))
		}
		return strings.Join(out, " ")
	}

	for _, testCase := range []struct{ what, got, want string }{
		{"a file that does not parse", tally(sch.Drift.ParseErrors), "common/things/a.txt=1"},
		{"an extension no format knows", tally(sch.Drift.Extensions), ".xyz=1"},
		{"an entry mode the registry gives no meaning", tally(sch.Drift.EntryModes), "REPLACE=1"},
		{"a prefix nothing declares and no kind owns", tally(sch.Drift.Prefixes), "usr=1"},
		{"an effect outside the hand table whose values are names script makes", tally(sch.Drift.NameTokens), "make_name=2"},
	} {
		if testCase.got != testCase.want {
			t.Errorf("%s: %q, want %q", testCase.what, testCase.got, testCase.want)
		}
	}
}

func TestKinds(t *testing.T) {
	sch := learned(t, map[string]string{
		"gfx/models/a/a.asset":              "pdxmesh = { name = \"a_mesh\" }\npdxmesh = { name = \"a_low_mesh\" }\nentity = { name = \"a_entity\" }\nentity = { name = \"a_low_entity\" }\n",
		"gfx/models/b/deep/b.asset":         "pdxmesh = { name = \"b_mesh\" }\nentity = { name = \"b_entity\" }\n",
		"gfx/models/shared.asset":           "pdxmesh = { name = \"shared_mesh\" }\npdxmesh = { name = \"other_mesh\" }\nentity = { name = \"shared_entity\" }\nentity = { name = \"other_entity\" }\n",
		"common/decisions/base.txt":         "one_decision = { cost = { gold = 1 } ai_will_do = { base = 1 } }\ntwo_decision = { cost = { gold = 2 } ai_will_do = { base = 2 } }\n",
		"common/decisions/dlc/more.txt":     "three_decision = { cost = { gold = 3 } }\n",
		"common/decisions/other/unlike.txt": "first_group = { sort = 1 icon = a }\nsecond_group = { sort = 2 icon = b }\n",
		"common/struggle/catalysts/c.txt":   "struggle_catalyst = { weight = 1 }\n",
		"common/situation/catalysts/c.txt":  "situation_catalyst = { weight = 1 }\n",
		"gui/window.gui":                    "types Shared { type big_button = button { size = 1 } type small_button = button { size = 2 } }\ntemplate plain { alpha = 1 }\n",
		"common/scripted_triggers/trig.txt": "folder_trigger = { NOT = { any_child = { is_adult = yes } } }\nother_trigger = { folder_trigger = yes any_child = { is_adult = yes } }\n",
		"events/first.txt":                  "namespace = first\nscripted_trigger inline_trigger = { is_adult = yes }\nfirst.1 = { trigger = { folder_trigger = yes } }\nfirst.2 = { trigger = { other_trigger = yes } }\n",
		"events/second.txt":                 "namespace = second\nsecond.1 = { trigger = { inline_trigger = yes } }\nsecond.2 = { trigger = { inline_trigger = yes } }\n",
	}, nil)

	want := []string{"decisions", "events", "gui.type", "models.entity", "models.pdxmesh", "other", "scripted_triggers", "situation/catalysts", "struggle/catalysts"}
	if got := names(sch); !slices.Equal(got, want) {
		t.Fatalf("kinds = %v\nwant    %v", got, want)
	}

	// which top-level keys are words of a file format and which are names
	counts := counted(t, map[string]string{
		"gfx/models/a/a.asset": "pdxmesh = { name = \"a_mesh\" file = \"a.mesh\" }\npdxmesh = { name = \"a_low_mesh\" file = \"a_low.mesh\" }\nentity = { name = \"a_entity\" pdxmesh = \"a_mesh\" }\nentity = { name = \"a_low_entity\" pdxmesh = \"a_low_mesh\" }\n",
		"gfx/models/b/b.asset": "pdxmesh = { name = \"b_mesh\" file = \"b.mesh\" }\nentity = { name = \"b_entity\" pdxmesh = \"b_mesh\" }\n",
		"gfx/models/a/a.mesh":  "x", "gfx/models/a/a_low.mesh": "x", "gfx/models/b/b.mesh": "x",
		"history/wars/wars.txt":          "war = { name = \"first_war\" casus_belli = claim_cb }\nwar = { name = \"second_war\" casus_belli = claim_cb }\nwar = { name = \"third_war\" casus_belli = other_cb }\n",
		"common/casus_belli/cb.txt":      "claim_cb = { group = war }\nother_cb = { group = war }\nwar = { group = none }\n",
		"common/coat_of_arms/arms.txt":   "k_one = { pattern = \"p.dds\" color1 = red }\nk_two = { pattern = \"p.dds\" color1 = blue }\nk_one = { pattern = \"p.dds\" color1 = yellow }\n",
		"common/named_colors/colors.txt": "red = { 1 0 0 }\nblue = { 0 0 1 }\nyellow = { 1 1 0 }\n",
	})
	counts.words()

	for _, testCase := range []struct {
		what      string
		got, want any
	}{
		{"a kind told by its opening word holds its whole tree", strings.Join(sch.Kinds["models.pdxmesh"].Folders, " "), "gfx/models gfx/models/a gfx/models/b/deep"},
		{"a subfolder alike its parent joins it, one unlike it does not", strings.Join(sch.Kinds["decisions"].Folders, " "), "common/decisions common/decisions/dlc"},
		{"a keyword that is a kind's name in the singular declares that kind inline", sch.Kinds["scripted_triggers"].Naming.Keyword, "scripted_trigger"},
		{"the inline definition keeps the kind's own folders", strings.Join(sch.Kinds["scripted_triggers"].Folders, " "), "common/scripted_triggers"},
		{"a declared kind is named by its keyword", sch.Kinds["gui.type"].Naming.Form + " " + sch.Kinds["gui.type"].Naming.Keyword, "declaration type"},
		{"a word's blocks are named by the member whose values are their own new names", counts.namedBy["pdxmesh"], "name"},
		{"of two members every block holds, the one that never repeats", counts.namedBy["war"], "name"},
		{"a block that holds the naming member is a word's, though written once in its folder", counts.isWord(counts.folders["gfx/models/b"], "pdxmesh", counts.folders["gfx/models/b"].root.members["pdxmesh"]), true},
		{"the same key with no such member is a name", counts.isWord(counts.folders["common/casus_belli"], "war", counts.folders["common/casus_belli"].root.members["war"]), false},
		{"a name defined twice among names written once stays a name", counts.isWord(counts.folders["common/coat_of_arms"], "k_one", counts.folders["common/coat_of_arms"].root.members["k_one"]), false},
		{"and gets no naming member: its colours are references", counts.namedBy["k_one"], ""},
	} {
		if testCase.got != testCase.want {
			t.Errorf("%s: %v, want %v", testCase.what, testCase.got, testCase.want)
		}
	}

	// a .lookup file is its own kind; law groups nest
	sch = learned(t, map[string]string{
		"common/traits/00_traits.txt":           "brave = { index = 1 }\ncraven = { index = 2 }\nshy = { index = 3 }\n",
		"common/traits/trait_conversion.lookup": "old_brave = brave\nold_shy = shy\n",
		"common/scripted_effects/00_big.txt":    "one_effect = { if = { limit = { is_adult = yes } add_gold = 1 } }\ntwo_effect = { if = { limit = { is_adult = yes } add_gold = 2 } }\n",
		"common/scripted_effects/01_debug.txt":  "debug_effect = { add_trait = brave }\n",
		"common/scripted_effects/02_empty.txt":  "empty_effect = { }\n",
		"common/script_values/00_plain.txt":     "small_value = 1\nlarge_value = 9\n",
		"common/script_values/01_formulas.txt":  "sum_value = { value = 1 add = 2 if = { limit = { is_adult = yes } add = 3 } }\n",
		"common/laws/00_laws.txt":               "order_laws = { flag = order first_law = { cost = 1 } second_law = { cost = 2 } }\nrealm_laws = { flag = realm third_law = { cost = 3 } fourth_law = { cost = 4 } }\n",
		"common/laws/01_title_laws.txt":         "title_laws = { fifth_law = { cost = 5 } }\n",
		"common/use/u.txt":                      "user = { law = first_law other = third_law more = fifth_law again = second_law last = fourth_law }\n",
	}, nil)

	want = []string{"laws", "laws.entry", "script_values", "scripted_effects", "traits", "traits.trait_conversion", "use"}
	if got := names(sch); !slices.Equal(got, want) {
		t.Fatalf("kinds = %v\nwant    %v", got, want)
	}
	if got := strings.Join(sch.Kinds["traits.trait_conversion"].Folders, " "); got != "common/traits/trait_conversion.lookup" {
		t.Errorf("a file that is a kind of its own is its kind's folder: %q", got)
	}

	// nested kinds and their naming
	sch = learned(t, map[string]string{
		"common/game_rules/rules.txt": "first_rule = { default = on_setting on_setting = { flag = a } off_setting = { flag = b } }\nsecond_rule = { default = low_setting low_setting = { flag = c } high_setting = { flag = d } }\nthird_rule = { default = slow_setting slow_setting = { flag = e } fast_setting = { flag = f } }\n",
		"common/goods/goods.txt":      "wheat = { cost = 1 }\niron = { cost = 2 }\nwood = { cost = 3 }\ncoal = { cost = 4 }\n",
		"common/buildings/b.txt":      "farm = { level = 1 output = { wheat = 1 } }\nmine = { level = 2 output = { iron = 2 coal = 1 } }\nmill = { level = 3 output = { wood = 3 } }\n",
		"common/small/s.txt":          "one = { icon = a size = 1 }\ntwo = { icon = b color = red }\nthree = { icon = c frame = 2 }\n",
		"common/use/u.txt":            "user = { rule = off_setting other = high_setting more = fast_setting }\n",
	}, nil, "setting_on_setting", "setting_off_setting", "setting_low_setting", "setting_high_setting", "setting_slow_setting", "setting_fast_setting")

	if kind := sch.Kinds["game_rules.setting"]; kind == nil || kind.Parent != "game_rules" || sch.Kinds["game_rules"].Shape.Defines != "game_rules.setting" {
		t.Fatalf("names written once inside a definition, which the install refers to, are a kind nested in it, named by its loc stem: %v", names(sch))
	}
	if got := sch.Kinds["buildings"].Shape.Members["output"].KeyedBy; got != "goods" {
		t.Errorf("where the names are definitions made elsewhere the position cites them: %q", got)
	}
	if got := names(sch); slices.ContainsFunc(got, func(name string) bool { return strings.HasPrefix(name, "small.") }) {
		t.Errorf("optional members written once are no nested kind: %v", got)
	}

	// the kinds that are about others
	sch = learned(t, map[string]string{
		"common/cultures/c.txt":      "norse = { color = red }\nirish = { color = blue }\n",
		"history/cultures/norse.txt": "900.1.1 = { discover = yes }\n",
		"history/cultures/irish.txt": "900.1.1 = { discover = yes }\n",
		"common/terrain/t.txt":       "1 = plains\n2 = hills\n",
		"map_data/definition.csv":    "1;10;10;10;FIRST;x;\n2;20;20;20;SECOND;x;\n3;30;30;30;THIRD;x;\n",
		"history/provinces/p.txt":    "1 = { holding = castle }\n2 = { holding = city }\n",
		"gui/w.gui":                  "block \"first\" { size = 1 }\nblock \"second\" { size = 2 }\nblock \"third\" { size = 3 }\nblockoverride \"first\" { size = 4 }\nblockoverride \"second\" { size = 5 }\n",
	}, []string{"history"})

	for _, testCase := range []struct{ kind, want string }{
		{"history/cultures", "cultures"},
		{"history/provinces", "map_data.definition"},
		{"gui.blockoverride", "gui.block"},
	} {
		kind := sch.Kinds[testCase.kind]
		if testCase.kind == "gui.blockoverride" {
			kind = sch.Kinds["gui"]
		}
		if kind == nil || kind.Subject != testCase.want {
			t.Errorf("%s is about %v, want %s (kinds %v)", testCase.kind, kind, testCase.want, names(sch))
		}
	}
	if got := sch.Kinds["history/cultures"].Naming.Form; got != "file" {
		t.Errorf("where nothing in the files is a name, the files are named after the subject: %q", got)
	}
	if got := sch.Kinds["cultures"]; got == nil {
		t.Errorf("the thing keeps its short name beside its history: %v", names(sch))
	}
}

func TestClass(t *testing.T) {
	counts := counted(t, map[string]string{
		"common/things/a.txt": strings.Join([]string{
			"a = { check = { is_adult = yes NOT = { any_child = { is_adult = yes } } } }",
			"b = { check = yes run = { if = { limit = { is_adult = yes } add_gold = 1 } } }",
			"c = { cost = { gold = 100 prestige = 50 } tags = { x y z } label = one }",
			"d = { worth = 5 mixed = { trigger_if = { limit = { is_adult = yes } } if = { limit = { is_adult = yes } } } }",
			"e = { worth = { value = 1 add = 2 if = { limit = { is_adult = yes } add = 3 } } sum = { add = 1 liege = { add = 2 } } }",
			"f = { father = { is_adult = yes } range = { min = 1 } }",
		}, "\n"),
		"common/values/v.txt": "one = 1\ntwo = { value = 2 min = 1 if = { limit = { is_adult = yes } min = 0 } }\n",
	})
	shape := &node{}
	for _, member := range counts.folders["common/things"].root.members {
		shape.merge(member)
	}

	for _, testCase := range []struct {
		member string
		class  schema.Class
		body   schema.BodyClass
	}{
		{"check", schema.ClassBody, schema.BodyTrigger},
		{"run", schema.ClassBody, schema.BodyEffect},
		{"cost", schema.ClassStructure, 0},
		{"tags", schema.ClassList, 0},
		{"label", schema.ClassValue, 0},
		{"mixed", schema.ClassBody, schema.BodyTrigger | schema.BodyEffect},
		{"worth", schema.ClassBody, schema.BodyMath},
		{"sum", schema.ClassBody, schema.BodyMath},
		{"father", schema.ClassStructure, 0},
		{"range", schema.ClassBody, schema.BodyMath},
	} {
		if class, body := counts.class(shape.members[testCase.member]); class != testCase.class || body != testCase.body {
			t.Errorf("%s = %v %v, want %v %v", testCase.member, class, body, testCase.class, testCase.body)
		}
	}
}

func TestAccepts(t *testing.T) {
	sch := learned(t, map[string]string{
		"common/traits/t.txt":      "brave = { index = 1 }\ncraven = { index = 2 }\nshy = { index = 3 }\n",
		"common/anims/a.txt":       "popup = { speed = 1 }\nwave = { speed = 2 }\n",
		"common/colors/c.txt":      "one = { tint = hsv { 0.1 0.2 0.3 } }\ntwo = { tint = rgb { 1 2 3 } }\n",
		"history/characters/p.txt": "1 = { name = \"Ada\" spouse = character:3 }\n2 = { name = \"Bo\" father = 1 age = 1 }\n3 = { name = \"Cy\" father = 2 age = 2 }\n",
		"common/messages/m.txt": strings.Join([]string{
			"m1 = { display = popup style = good trait = brave icon = \"gfx/icons/a.dds\" title = m1_title when = 1000.1.1 shown = yes target = root.liege }",
			"m2 = { display = toast style = good trait = craven icon = \"gfx/icons/b.dds\" title = m2_title when = 1001.1.1 shown = no target = scope:actor.liege }",
			"m3 = { display = toast style = bad trait = trait:shy icon = \"gfx/icons/a.dds\" title = m3_title text = \"Some words\" }",
			"m4 = { display = feed style = bad trait = brave text = \"Other words\" }",
			"m5 = { display = feed }",
		}, "\n"),
		"gfx/icons/a.dds": "DDS ", "gfx/icons/b.dds": "DDS ",
	}, []string{"history"}, "m1_title", "m2_title", "m3_title")
	accepts := func(kind, member string) string {
		var out []string
		for _, accept := range sch.Kinds[kind].Shape.Members[member].Accepts {
			out = append(out, strings.Join(strings.Fields(accept.Type.String()+" "+accept.Kind+" "+strings.Join(accept.Enum, ",")), " "))
		}
		return strings.Join(out, "; ")
	}

	for _, testCase := range []struct{ kind, member, want string }{
		{"messages", "display", "enum feed,popup,toast"},
		{"messages", "style", "enum bad,good"},
		{"messages", "trait", "ref traits"},
		{"messages", "icon", "file"},
		{"messages", "title", "loc"},
		{"messages", "when", "date"},
		{"messages", "shown", "bool"},
		{"messages", "target", "scope_path"},
		{"messages", "text", "string"},
		{"colors", "tint", "color"},
		{"characters", "father", "ref characters"},
		{"characters", "age", "number"},
	} {
		if got := accepts(testCase.kind, testCase.member); got != testCase.want {
			t.Errorf("%s.%s accepts %q, want %q", testCase.kind, testCase.member, got, testCase.want)
		}
	}
}

func TestHoist(t *testing.T) {
	counts := counted(t, map[string]string{"gui/w.gui": "window = { name = \"a\" vbox = { hbox = { vbox = { text = \"x\" } } } }\nwindow = { name = \"b\" vbox = { text = \"y\" } }\n"})
	shape := counts.folders["gui"].root.members["window"]
	counts.hoist(shape)

	if shape.members["vbox"] == nil || shape.members["hbox"] == nil || shape.members["vbox"].members["hbox"] != nil {
		t.Errorf("a key that can hold itself is one member at the top of the kind: %v", slices.Sorted(maps.Keys(shape.members)))
	}
	if shape.members["vbox"].members["text"].count != 2 {
		t.Errorf("every place it stood is counted there: %d", shape.members["vbox"].members["text"].count)
	}
}

// scanned learns an install as a scan does: its files, its english loc, its game info, the
// dumps in docs, and the registry's rows for the game.
func scanned(t *testing.T, id, install string, docs ...string) *schema.Schema {
	t.Helper()
	files, keys := source.Inventory("vanilla", source.Roots(install)), map[string]bool{}
	for _, file := range files {
		if file.Format == source.Loc && loc.LanguageFromRel(file.Rel) == "english" {
			text, _ := file.Text()
			for _, entry := range loc.Parse(text).Entries {
				keys[entry.Key] = true
			}
		}
	}
	var modes []string
	for _, entryMode := range game.Get(id).EntryModes {
		modes = append(modes, entryMode.Word)
	}
	sch, err := Learn(t.Context(), Input{Game: id, Version: "test", Files: files, Vocab: schema.ReadVocab(docs), Info: schema.ReadInfo(files), Loc: loc.NewKeys(keys), EntryModes: modes, Subjects: game.Get(id).SubjectDirs})
	if err != nil {
		t.Fatal(err)
	}
	return sch
}

// TestFixturesAreLearned learns the three fixture installs and compares each schema with the
// one committed beside it. A change to the learner that changes a schema shows in the diff of
// schema.txt: run with PMT_SCHEMA_UPDATE=1 to write them again, and read the diff.
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

// TestInstallsAreLearned is the gate over the real installs: each game is learned, its schema
// reads back, the kinds invoked as keys are the four script kinds and no other, two scans write
// the same file, and the known facts of each game hold. It needs the installs and their
// script_docs dumps, so it runs only when asked: set PMT_LEARN_GATE=1 and the three
// PMT_TEST_INSTALL_ variables, each a game's root folder.
func TestInstallsAreLearned(t *testing.T) {
	if testing.Short() || os.Getenv("PMT_LEARN_GATE") == "" {
		t.Skip("learns the three installs; set PMT_LEARN_GATE=1")
	}
	written := map[string]string{}
	for _, testCase := range []struct {
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
		install := os.Getenv(testCase.env)
		if install == "" {
			t.Errorf("%s: %s is not set", testCase.id, testCase.env)
			continue
		}
		user := game.UserDataDir(testCase.id, install)
		sch := scanned(t, testCase.id, install, filepath.Join(user, "logs"), filepath.Join(user, "docs"))
		text := schema.Write(sch)
		written[testCase.id] = text
		if back, err := schema.Read(text); err != nil || len(back.Kinds) != len(sch.Kinds) {
			t.Errorf("%s: the schema does not read back: %v", testCase.id, err)
		}

		var macros []string
		for name, kind := range sch.Kinds {
			if kind.Macro != 0 {
				macros = append(macros, name)
			}
		}
		slices.Sort(macros)
		if want := []string{"script_values", "scripted_effects", "scripted_lists", "scripted_triggers"}; !slices.Equal(macros, want) {
			t.Errorf("%s: macro kinds = %v, want %v", testCase.id, macros, want)
		}
		for prefix, kind := range testCase.prefixes {
			if found := sch.Kinds[kind]; found == nil || !slices.Contains(found.Prefixes, prefix) {
				t.Errorf("%s: %s: is not %s's prefix", testCase.id, prefix, kind)
			}
		}
		for kind, about := range testCase.subjects {
			if found := sch.Kinds[kind]; found == nil || found.Subject != about {
				t.Errorf("%s: %s is not about %s", testCase.id, kind, about)
			}
		}
		for kind, parent := range testCase.nested {
			if found := sch.Kinds[kind]; found == nil || found.Parent != parent {
				t.Errorf("%s: %s is not nested in %s", testCase.id, kind, parent)
			}
		}
		if field := sch.Arguments["if"]; field == nil || field.Body != schema.BodyEffect || field.Members["limit"] == nil || field.Members["limit"].Body != schema.BodyTrigger {
			t.Errorf("%s: if is an effect body whose limit is a trigger body: %+v", testCase.id, field)
		}
		if kind := sch.Kinds["script_values"]; kind == nil || kind.Shape.Body != schema.BodyMath {
			t.Errorf("%s: a script value computes a number", testCase.id)
		}
		t.Logf("%s: %d kinds, %d arguments, %d bytes", testCase.id, len(sch.Kinds), len(sch.Arguments), len(text))
	}

	// a second scan of one install writes the same file, and the facts only CK3 shows hold
	install := os.Getenv("PMT_TEST_INSTALL_CK3")
	if install == "" {
		t.Fatal("ck3: PMT_TEST_INSTALL_CK3 is not set")
	}
	user := game.UserDataDir("ck3", install)
	sch := scanned(t, "ck3", install, filepath.Join(user, "logs"), filepath.Join(user, "docs"))
	if schema.Write(sch) != written["ck3"] {
		t.Error("ck3: two scans of one install wrote different schemas")
	}
	for _, testCase := range []struct {
		what      string
		got, want any
	}{
		{"1: a character's history is a structure, labelled by its name", sch.Kinds["characters"].Shape.Class.String() + " " + sch.Kinds["characters"].LabelMember, "structure name"},
		{"1: a decision's cost is a structure though every key in it is a trigger's name", sch.Kinds["decisions"].Shape.Members["cost"].Class, schema.ClassStructure},
		{"4: a message is a structure", sch.Kinds["messages"].Shape.Class, schema.ClassStructure},
		{"7: scripted effects are declared inline by their keyword and take parameters", sch.Kinds["scripted_effects"].Naming.Keyword + " " + fmt.Sprint(sch.Kinds["scripted_effects"].Params), "scripted_effect true"},
		{"9: a message's display is a closed set of words", sch.Kinds["messages"].Shape.Members["display"].Accepts[0].Type, schema.TypeEnum},
		{"12: history/cultures is named by its files", sch.Kinds["history/cultures"].Naming.Form, "file"},
		{"landed titles are one kind nested in itself", sch.Kinds["landed_titles"].Shape.Defines, "landed_titles"},
	} {
		if testCase.got != testCase.want {
			t.Errorf("ck3 defect %s: %v, want %v", testCase.what, testCase.got, testCase.want)
		}
	}
}
