// census_test.go covers the counting: what is recorded at a position, which keys share one, what a declaration is, and what is kept past a position's capacity.
package learn

import (
	"fmt"
	"strings"
	"testing"
)

func TestCensusCounts(t *testing.T) {
	c := counted(t, map[string]string{
		"common/things/a.txt": "@cost = 5\nalpha = { weight = 1 tags = { x y } 1000.1.1 = { on = yes } 12 = { on = no } title:k_a = { on = yes } color = hsv { 0.1 0.2 0.3 } }\nINJECT:beta = { weight > 2 weight = \"two\" }\n",
		"common/things/b.txt": "alpha = { weight = $W$ }\n7 = { weight = 1 }\n",
	})
	fo := c.folders["common/things"]
	alpha, weight := fo.root.members["alpha"], fo.root.members["alpha"].members["weight"]

	for _, tt := range []struct {
		what      string
		got, want any
	}{
		{"files of the folder", fo.files, 2},
		{"a constant is no member", fo.root.members["@cost"] == nil && c.constants["cost"] == 1, true},
		{"an entry mode is split from the name", fo.root.members["beta"] != nil && c.modes["INJECT"] == 1, true},
		{"a numeric top-level key is recorded", fo.numbered["7"], 1},
		{"alpha is written twice, in two files", [2]int{alpha.count, alpha.in}, [2]int{2, 2}},
		{"weight: times, blocks holding it, numbers, params", [4]int{weight.count, weight.in, weight.numbers, weight.params}, [4]int{2, 2, 1, 1}},
		{"an operator other than = is kept", fo.root.members["beta"].members["weight"].ops[">"], 1},
		{"a quoted value is counted", fo.root.members["beta"].members["weight"].quoted, 1},
		{"a date key, a number key and a cite key each share a position", alpha.members["<date>"] != nil && alpha.members["<number>"] != nil && alpha.members["<cite>"] != nil, true},
		{"the cite is counted under its prefix", c.cites["title"]["k_a"], 1},
		{"bare words stand at <word>", alpha.members["tags"].members["<word>"].count, 2},
		{"a block's tag is kept", alpha.members["color"].tags["hsv"], 1},
	} {
		if tt.got != tt.want {
			t.Errorf("%s = %v, want %v", tt.what, tt.got, tt.want)
		}
	}
}

func TestCensusDeclarations(t *testing.T) {
	c := counted(t, map[string]string{
		"gui/a.gui":              "types Shared { type big_button = button { size = { 1 1 } } }\ntemplate plain { alpha = 1 }\n",
		"events/e.txt":           "scripted_trigger inline_trigger = { is_adult = yes }\n",
		"common/on_action/o.txt": "on_birth = { events = { delay = { days = 1 } first.1 } }\n",
	})

	if got := c.folders["gui"].keywords["template"]["plain"]; got != 1 {
		t.Errorf("a bare word before a named block with no operator declares: %d", got)
	}
	if got := c.folders["events"].keywords["scripted_trigger"]["inline_trigger"]; got != 1 {
		t.Errorf("a bare word before a named block at the top of a file declares: %d", got)
	}
	if len(c.folders["common/on_action"].keywords) != 0 {
		t.Errorf("inside a block a bare word before delay = { } is a list's word: %v", c.folders["common/on_action"].keywords)
	}
}

func TestCensusTokens(t *testing.T) {
	c := counted(t, map[string]string{
		"common/scripted_effects/a.txt": "one_effect = { hidden_effect = { if = { limit = { NOT = { any_child = { is_adult = yes } } } add_gold = 1 } } martial = 5 }\n",
	})

	if !c.holds["hidden_effect"]["if"] || !c.holds["not"]["any_child"] {
		t.Errorf("what a token holds directly: %v", c.holds)
	}
	if c.blocked["not"] != c.written["not"] || c.blocked["martial"] != 0 {
		t.Errorf("not is always written with a block and martial never: %v of %v", c.blocked, c.written)
	}
}

func TestCensusCapacity(t *testing.T) {
	var many strings.Builder
	for i := range maxMembers + 10 {
		fmt.Fprintf(&many, "name_%d = { weight = %d }\n", i, i)
	}
	many.WriteString("add_gold = { weight = 1 }\n")
	c := counted(t, map[string]string{"common/many/a.txt": many.String()})
	root := c.folders["common/many"].root

	if len(root.members) != maxMembers+1 || len(root.names) != 11 || root.members["<name>"].count != 10 {
		t.Errorf("past capacity a key shares <name>: %d members, %d names, %d at <name>", len(root.members), len(root.names), root.members["<name>"].count)
	}
	if c.tokens["add_gold"] == nil {
		t.Error("past capacity a declared token is counted with the token")
	}
	if got := len(root.members["<name>"].members["weight"].words); got != 10 {
		t.Errorf("a position whose values are mostly their own keeps every word: %d", got)
	}
}
