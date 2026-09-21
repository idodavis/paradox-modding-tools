// drift_test.go covers the report of what a scan saw and nothing explained.
package learn

import (
	"fmt"
	"strings"
	"testing"

	"paradox-modding-tools/services/internal/lang/schema"
)

func TestDrift(t *testing.T) {
	var made strings.Builder
	for i := range maxMembers + 1 {
		fmt.Fprintf(&made, "effect_%d = { add_gold = %d }\n", i, i)
	}
	made.WriteString("maker = { make_name = fresh_name make_name = other_name add_trait = brave }\nreader = { if = { limit = { scope:fresh_name = { is_adult = yes } scope:other_name = { is_adult = yes } scope:brave = { is_adult = yes } } } }\n")
	s := learned(t, map[string]string{
		"common/scripted_effects/many.txt": made.String(),
		"common/traits/t.txt":              "brave = { index = 1 }\nshy = { index = 2 }\n",
		"common/things/a.txt":              "INJECT:one = { weight = 1 }\nREPLACE:two = { weight = usr:custom }\nthree = { weight = 3 broken = { }\n",
		"stray/odd.xyz":                    "?",
	}, nil)
	tally := func(list []schema.Tally) string {
		var out []string
		for _, entry := range list {
			out = append(out, fmt.Sprintf("%s=%d", entry.Name, entry.Count))
		}
		return strings.Join(out, " ")
	}

	for _, tt := range []struct{ what, got, want string }{
		{"a file that does not parse", tally(s.Drift.ParseErrors), "common/things/a.txt=1"},
		{"an extension no format knows", tally(s.Drift.Extensions), ".xyz=1"},
		{"an entry mode the registry gives no meaning", tally(s.Drift.EntryModes), "REPLACE=1"},
		{"a prefix nothing declares and no kind owns", tally(s.Drift.Prefixes), "usr=1"},
		{"an effect outside the hand table whose values are names script makes", tally(s.Drift.NameTokens), "make_name=2"},
	} {
		if tt.got != tt.want {
			t.Errorf("%s: %q, want %q", tt.what, tt.got, tt.want)
		}
	}
}
