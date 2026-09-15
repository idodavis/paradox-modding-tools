// derive_test.go covers the facts no dump states, read from the corpus instead.

package derived

import (
	"slices"
	"testing"

	"paradox-modding-tools/services/internal/lang/loc"
)

// Which templates a kind uses is read, not listed; a shape only half the kind shares is a habit.
func TestDeriveLocConventions(t *testing.T) {
	var defs []Def
	locKeys := map[string]bool{}
	for i := range 10 {
		id := "trait_" + string(rune('a'+i))
		defs = append(defs, Def{Kind: "traits", Key: id})
		locKeys[id] = true
		locKeys[id+"_desc"] = true
		if i < 5 {
			locKeys["trait_"+id] = true // half the kind: a habit
		}
	}
	for i := range 10 {
		id := "k_" + string(rune('a'+i))
		defs = append(defs, Def{Kind: "landed_titles", Key: id})
		locKeys[id] = true
		locKeys[id+"_adj"] = true
	}
	structures := map[string][]string{"landed_titles": {"adj", "color"}}

	got := deriveLocConventions(newDefIndex(defs), structures, loc.NewKeys(locKeys))
	for _, tc := range []struct {
		kind string
		want []loc.Template
	}{
		{"traits", []loc.Template{"{id}", "{id}_desc"}},
		{"landed_titles", []loc.Template{"{id}", "{id}_adj"}},
	} {
		var names []loc.Template
		for _, c := range got[tc.kind] {
			names = append(names, c.Template)
		}
		if !slices.Equal(names, tc.want) {
			t.Errorf("%s = %v, want %v", tc.kind, names, tc.want)
		}
	}
}
