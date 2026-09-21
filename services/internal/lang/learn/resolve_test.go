// resolve_test.go covers what a position is: the witnesses the vocabulary gives, the class they decide, the keys that are names, and the values a field accepts.
package learn

import (
	"maps"
	"slices"
	"strings"
	"testing"

	"paradox-modding-tools/services/internal/lang/schema"
)

func TestWitnesses(t *testing.T) {
	c := counted(t, miniInstall)

	for _, tt := range []struct {
		what      string
		got, want bool
	}{
		{"a usage with <effects> takes effects", c.effectBlocks["if"], true},
		{"a usage with <triggers> takes triggers", c.triggerBlocks["any_child"], true},
		{"no usage, always a block, holds a taker: takes what it holds", c.triggerBlocks["not"] && c.triggerBlocks["or"] && c.effectBlocks["hidden_effect"], true},
		{"no usage, written with a number: takes nothing", c.triggerBlocks["martial"] || c.triggerBlocks["gold"], false},
		{"a plain effect takes nothing", c.effectBlocks["add_gold"], false},
		{"the words of arithmetic are learned where a number is also a block with if", c.math["add"] && c.math["multiply"], true},
		{"a field word is no word of arithmetic", c.math["cost"] || c.math["base"], false},
		{"every value and cited name is known as written", c.valued["brave"] && c.valued["k_upper"] && c.valued["MAX_AGE"], true},
		{"a field word is a key at many positions, a name at one", c.keyedAt["color"] > 1 && c.keyedAt["SIEGE_DAYS"] == 1, true},
	} {
		if tt.got != tt.want {
			t.Errorf("%s: %v", tt.what, tt.got)
		}
	}
}

func TestClass(t *testing.T) {
	c := counted(t, map[string]string{
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
	for _, m := range c.folders["common/things"].root.members {
		shape.merge(m)
	}

	for _, tt := range []struct {
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
		if class, body := c.class(shape.members[tt.member]); class != tt.class || body != tt.body {
			t.Errorf("%s = %v %v, want %v %v", tt.member, class, body, tt.class, tt.body)
		}
	}
}

func TestNamed(t *testing.T) {
	c := counted(t, map[string]string{
		"common/rules/r.txt":  "first_rule = { default = on_setting on_setting = { flag = a } off_setting = { flag = b } }\nsecond_rule = { default = low_setting low_setting = { flag = c } high_setting = { flag = d } }\n",
		"common/use/u.txt":    "user = { rule = off_setting other = high_setting }\n",
		"common/small/s.txt":  "one = { icon = a size = 1 } \ntwo = { icon = b color = red }\nthree = { icon = c frame = 2 }\n",
		"common/groups/g.txt": "NOne = { ALPHA = 1 BETA = 2 }\nNTwo = { GAMMA = 3 }\n",
		"events/e.txt":        "namespace = e\ne.1 = { trigger = { martial > define:NOne|ALPHA martial < define:NTwo|GAMMA } }\n",
	})
	merged := func(folder string) keys {
		shape := &node{}
		for _, m := range c.folders[folder].root.members {
			shape.merge(m)
		}
		return c.keysOf(shape)
	}

	if k := merged("common/rules"); !slices.Equal(k.once, []string{"high_setting", "low_setting", "off_setting", "on_setting"}) || !c.named(k) {
		t.Errorf("keys written once that the install refers to are names: %v %v", k.once, c.named(k))
	}
	if k := merged("common/small"); c.named(k) {
		t.Errorf("optional members written once are no names, nothing refers to them: %v", k.once)
	}
	if k := merged("common/groups"); !c.named(k) {
		t.Errorf("names only the engine reads are names where a declared prefix mostly cites them: %v", k.once)
	}
}

func TestAccepts(t *testing.T) {
	s := learned(t, map[string]string{
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
		for _, a := range s.Kinds[kind].Shape.Members[member].Accepts {
			out = append(out, strings.Join(strings.Fields(a.Type.String()+" "+a.Kind+" "+strings.Join(a.Enum, ",")), " "))
		}
		return strings.Join(out, "; ")
	}

	for _, tt := range []struct{ kind, member, want string }{
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
		if got := accepts(tt.kind, tt.member); got != tt.want {
			t.Errorf("%s.%s accepts %q, want %q", tt.kind, tt.member, got, tt.want)
		}
	}
}

func TestHoist(t *testing.T) {
	c := counted(t, map[string]string{"gui/w.gui": "window = { name = \"a\" vbox = { hbox = { vbox = { text = \"x\" } } } }\nwindow = { name = \"b\" vbox = { text = \"y\" } }\n"})
	shape := c.folders["gui"].root.members["window"]
	c.hoist(shape)

	if shape.members["vbox"] == nil || shape.members["hbox"] == nil || shape.members["vbox"].members["hbox"] != nil {
		t.Errorf("a key that can hold itself is one member at the top of the kind: %v", slices.Sorted(maps.Keys(shape.members)))
	}
	if shape.members["vbox"].members["text"].count != 2 {
		t.Errorf("every place it stood is counted there: %d", shape.members["vbox"].members["text"].count)
	}
}
