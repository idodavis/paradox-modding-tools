// kinds_test.go covers how folders become kinds: a word of a file format against a name, which folders are one database, the kinds nested in others, the kinds that are about others, and what each is called.
package learn

import (
	"slices"
	"strings"
	"testing"
)

func TestWords(t *testing.T) {
	c := counted(t, map[string]string{
		"gfx/models/a/a.asset": "pdxmesh = { name = \"a_mesh\" file = \"a.mesh\" }\npdxmesh = { name = \"a_low_mesh\" file = \"a_low.mesh\" }\nentity = { name = \"a_entity\" pdxmesh = \"a_mesh\" }\nentity = { name = \"a_low_entity\" pdxmesh = \"a_low_mesh\" }\n",
		"gfx/models/b/b.asset": "pdxmesh = { name = \"b_mesh\" file = \"b.mesh\" }\nentity = { name = \"b_entity\" pdxmesh = \"b_mesh\" }\n",
		"gfx/models/a/a.mesh":  "x", "gfx/models/a/a_low.mesh": "x", "gfx/models/b/b.mesh": "x",
		"history/wars/wars.txt":          "war = { name = \"first_war\" casus_belli = claim_cb }\nwar = { name = \"second_war\" casus_belli = claim_cb }\nwar = { name = \"third_war\" casus_belli = other_cb }\n",
		"common/casus_belli/cb.txt":      "claim_cb = { group = war }\nother_cb = { group = war }\nwar = { group = none }\n",
		"common/coat_of_arms/arms.txt":   "k_one = { pattern = \"p.dds\" color1 = red }\nk_two = { pattern = \"p.dds\" color1 = blue }\nk_one = { pattern = \"p.dds\" color1 = yellow }\n",
		"common/named_colors/colors.txt": "red = { 1 0 0 }\nblue = { 0 0 1 }\nyellow = { 1 1 0 }\n",
	})
	c.words()

	for _, tt := range []struct {
		what      string
		got, want any
	}{
		{"a word's blocks are named by the member whose values are their own new names", c.namedBy["pdxmesh"], "name"},
		{"of two members every block holds, the one that never repeats", c.namedBy["war"], "name"},
		{"a block that holds the naming member is a word's, though written once in its folder", c.isWord(c.folders["gfx/models/b"], "pdxmesh", c.folders["gfx/models/b"].root.members["pdxmesh"]), true},
		{"the same key with no such member is a name", c.isWord(c.folders["common/casus_belli"], "war", c.folders["common/casus_belli"].root.members["war"]), false},
		{"a name defined twice among names written once stays a name", c.isWord(c.folders["common/coat_of_arms"], "k_one", c.folders["common/coat_of_arms"].root.members["k_one"]), false},
		{"and gets no naming member: its colours are references", c.namedBy["k_one"], ""},
	} {
		if tt.got != tt.want {
			t.Errorf("%s: %v, want %v", tt.what, tt.got, tt.want)
		}
	}
}

func TestKinds(t *testing.T) {
	s := learned(t, map[string]string{
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
	if got := names(s); !slices.Equal(got, want) {
		t.Fatalf("kinds = %v\nwant    %v", got, want)
	}
	for _, tt := range []struct{ what, got, want string }{
		{"a kind told by its opening word holds its whole tree", strings.Join(s.Kinds["models.pdxmesh"].Folders, " "), "gfx/models gfx/models/a gfx/models/b/deep"},
		{"a subfolder alike its parent joins it, one unlike it does not", strings.Join(s.Kinds["decisions"].Folders, " "), "common/decisions common/decisions/dlc"},
		{"a keyword that is a kind's name in the singular declares that kind inline", s.Kinds["scripted_triggers"].Naming.Keyword, "scripted_trigger"},
		{"the inline definition keeps the kind's own folders", strings.Join(s.Kinds["scripted_triggers"].Folders, " "), "common/scripted_triggers"},
		{"a declared kind is named by its keyword", s.Kinds["gui.type"].Naming.Form + " " + s.Kinds["gui.type"].Naming.Keyword, "declaration type"},
	} {
		if tt.got != tt.want {
			t.Errorf("%s: %q, want %q", tt.what, tt.got, tt.want)
		}
	}
}

func TestSideBySide(t *testing.T) {
	s := learned(t, map[string]string{
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

	want := []string{"laws", "laws.entry", "script_values", "scripted_effects", "traits", "traits.trait_conversion", "use"}
	if got := names(s); !slices.Equal(got, want) {
		t.Fatalf("kinds = %v\nwant    %v", got, want)
	}
	if got := strings.Join(s.Kinds["traits.trait_conversion"].Folders, " "); got != "common/traits/trait_conversion.lookup" {
		t.Errorf("a file that is a kind of its own is its kind's folder: %q", got)
	}
}

func TestNested(t *testing.T) {
	s := learned(t, map[string]string{
		"common/game_rules/rules.txt": "first_rule = { default = on_setting on_setting = { flag = a } off_setting = { flag = b } }\nsecond_rule = { default = low_setting low_setting = { flag = c } high_setting = { flag = d } }\nthird_rule = { default = slow_setting slow_setting = { flag = e } fast_setting = { flag = f } }\n",
		"common/goods/goods.txt":      "wheat = { cost = 1 }\niron = { cost = 2 }\nwood = { cost = 3 }\ncoal = { cost = 4 }\n",
		"common/buildings/b.txt":      "farm = { level = 1 output = { wheat = 1 } }\nmine = { level = 2 output = { iron = 2 coal = 1 } }\nmill = { level = 3 output = { wood = 3 } }\n",
		"common/small/s.txt":          "one = { icon = a size = 1 }\ntwo = { icon = b color = red }\nthree = { icon = c frame = 2 }\n",
		"common/use/u.txt":            "user = { rule = off_setting other = high_setting more = fast_setting }\n",
	}, nil, "setting_on_setting", "setting_off_setting", "setting_low_setting", "setting_high_setting", "setting_slow_setting", "setting_fast_setting")

	if k := s.Kinds["game_rules.setting"]; k == nil || k.Parent != "game_rules" || s.Kinds["game_rules"].Shape.Defines != "game_rules.setting" {
		t.Fatalf("names written once inside a definition, which the install refers to, are a kind nested in it, named by its loc stem: %v", names(s))
	}
	if got := s.Kinds["buildings"].Shape.Members["output"].KeyedBy; got != "goods" {
		t.Errorf("where the names are definitions made elsewhere the position cites them: %q", got)
	}
	if got := names(s); slices.ContainsFunc(got, func(n string) bool { return strings.HasPrefix(n, "small.") }) {
		t.Errorf("optional members written once are no nested kind: %v", got)
	}
}

func TestSubjects(t *testing.T) {
	s := learned(t, map[string]string{
		"common/cultures/c.txt":      "norse = { color = red }\nirish = { color = blue }\n",
		"history/cultures/norse.txt": "900.1.1 = { discover = yes }\n",
		"history/cultures/irish.txt": "900.1.1 = { discover = yes }\n",
		"common/terrain/t.txt":       "1 = plains\n2 = hills\n",
		"map_data/definition.csv":    "1;10;10;10;FIRST;x;\n2;20;20;20;SECOND;x;\n3;30;30;30;THIRD;x;\n",
		"history/provinces/p.txt":    "1 = { holding = castle }\n2 = { holding = city }\n",
		"gui/w.gui":                  "block \"first\" { size = 1 }\nblock \"second\" { size = 2 }\nblock \"third\" { size = 3 }\nblockoverride \"first\" { size = 4 }\nblockoverride \"second\" { size = 5 }\n",
	}, []string{"history"})

	for _, tt := range []struct{ kind, want string }{
		{"history/cultures", "cultures"},
		{"history/provinces", "map_data.definition"},
		{"gui.blockoverride", "gui.block"},
	} {
		k := s.Kinds[tt.kind]
		if tt.kind == "gui.blockoverride" {
			k = s.Kinds["gui"]
		}
		if k == nil || k.Subject != tt.want {
			t.Errorf("%s is about %v, want %s (kinds %v)", tt.kind, k, tt.want, names(s))
		}
	}
	if got := s.Kinds["history/cultures"].Naming.Form; got != "file" {
		t.Errorf("where nothing in the files is a name, the files are named after the subject: %q", got)
	}
	if got := s.Kinds["cultures"]; got == nil {
		t.Errorf("the thing keeps its short name beside its history: %v", names(s))
	}
}
