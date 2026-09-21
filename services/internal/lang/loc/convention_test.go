// convention_test.go covers learning a kind's loc-key templates and rendering them.

package loc

import (
	"slices"
	"testing"
)

func TestLearnAndExpand(t *testing.T) {
	keys := map[string]bool{}
	var traits, titles, rules []string
	for i := range 10 {
		s := string(rune('a' + i))
		traits = append(traits, "trait"+s)
		keys["trait"+s], keys["trait"+s+"_desc"] = true, true
		titles = append(titles, "k_"+s)
		keys["k_"+s], keys["k_"+s+"_adj"] = true, true
		rules = append(rules, "r"+s)
		keys["rule_r"+s] = true
	}
	idx := NewKeys(keys)
	for _, tt := range []struct {
		kind         string
		ids, members []string
		want         []Template
	}{
		{"traits", traits, nil, []Template{"{id}", "{id}_desc"}},
		{"titles", titles, []string{"adj"}, []Template{"{id}", "{id}_adj"}},
		{"game_rules", rules, nil, []Template{"rule_{id}"}},
		// A shape one definition has is a coincidence, not a convention: it has to repeat.
		{"one def", traits[:1], nil, nil},
		{"two defs", traits[:2], nil, []Template{"{id}", "{id}_desc"}},
	} {
		got := Learn(tt.ids, tt.members, idx)
		var names []Template
		for _, c := range got {
			names = append(names, c.Template)
		}
		if !slices.Equal(names, tt.want) {
			t.Errorf("%s: %v, want %v", tt.kind, names, tt.want)
		}
	}
	if got := Expand("bold", []Convention{{"{id}", 1}, {"{id}_desc", 1}}); !slices.Equal(got, []string{"bold", "bold_desc"}) {
		t.Errorf("Expand = %v", got)
	}
}
