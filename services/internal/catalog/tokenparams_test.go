// tokenparams_test.go covers reading an iterator's declared arguments out of the
// usage lines script_docs prints beside it.

package catalog

import (
	"slices"
	"sort"
	"testing"
)

func TestTokenParams(t *testing.T) {
	s := &Schema{Triggers: map[string]EngineToken{
		// Verbatim from CK3 triggers.log. The `any_` spelling declares the
		// argument; the `random_` one does not, and they are one iterator.
		"any_trait_in_category": {Usage: "any_trait_in_category = { category = fame/health/etc }\n" +
			"any_trait_in_category = { <count=num/all> / <percent=fixed_point> <triggers> }"},
		"any_county_in_region": {Usage: "any_county_in_region = { <count=num/all> / <percent=fixed_point> <triggers> }"},
	}, Effects: map[string]EngineToken{
		"random_trait_in_category": {Usage: "random_trait_in_category = { limit = { <triggers> } (optional) weight = { mtth } <effects> }"},
		"every_vassal":             {Usage: "every_vassal = { limit = { <triggers> } <effects> }"},
		"add_gold":                 {Usage: "add_gold = 5"},
	}}

	for _, tt := range []struct {
		key  string
		want []string
	}{
		// The family is read as one and the union is symmetric: the random_
		// spelling gains `category` from the any_ spelling that documents it,
		// and the any_ spelling gains `limit` and `weight` back. That is the
		// point -- they are one iterator the game happens to document unevenly.
		{"any_trait_in_category", []string{"category", "count", "limit", "percent", "weight"}},
		{"random_trait_in_category", []string{"category", "count", "limit", "percent", "weight"}},
		{"every_vassal", []string{"limit"}},
		{"any_county_in_region", []string{"count", "percent"}},
		// No block in the usage line means no arguments.
		{"add_gold", nil},
		{"not_a_token", nil},
	} {
		t.Run(tt.key, func(t *testing.T) {
			got := s.TokenParams(tt.key)
			var keys []string
			for k := range got {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			if !slices.Equal(keys, tt.want) {
				t.Errorf("TokenParams(%q) = %v, want %v", tt.key, keys, tt.want)
			}
		})
	}
}

// A placeholder is not an argument: `<triggers>` inside `limit = { … }` sits at
// depth two and must not become a name.
func TestUsageParamsIgnoresNestedPlaceholders(t *testing.T) {
	got := usageParams("x = { limit = { <triggers> } weight = { mtth } }")
	sort.Strings(got)
	if !slices.Equal(got, []string{"limit", "weight"}) {
		t.Errorf("usageParams = %v, want [limit weight]", got)
	}
}
