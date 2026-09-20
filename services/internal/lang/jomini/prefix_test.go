// prefix_test.go covers ParsePrefixed (ephemeral), ParseTyped (type:id), and
// the RHS-skip / typed-span helpers built on them.

package jomini

import (
	"regexp"
	"testing"
)

func TestParsePrefixedAndTyped(t *testing.T) {
	t.Run("prefixed_unchanged", func(t *testing.T) {
		p, ok := ParsePrefixed("scope:foo")
		if !ok || p.Prefix != "scope" || p.Name != "foo" {
			t.Fatalf("scope:foo = %+v ok=%v", p, ok)
		}
		if _, ok := ParsePrefixed("culture:english"); ok {
			t.Fatal("culture:english must not be ParsePrefixed")
		}
	})

	t.Run("typed", func(t *testing.T) {
		cases := []struct {
			name, in, kind, id string
			ok                 bool
		}{
			{"culture cite", "culture:english", "culture", "english", true},
			{"bare culture", "english", "", "", false},
			{"dotted link", "capital_province.culture", "", "", false},
			{"dollar interp", "$COA$", "", "", false},
			{"scope ephemeral", "scope:foo", "", "", false},
			{"var ephemeral", "var:gold", "", "", false},
			{"trait cite", "trait:brave", "trait", "brave", true},
			{"coa cite", "coat_of_arms:b_appleby", "coat_of_arms", "b_appleby", true},
			{"titles cite", "titles:e_hre", "titles", "e_hre", true},
			{"title cite", "title:k_france", "title", "k_france", true},
			{"faith cite", "faith:catholic", "faith", "catholic", true},
			{"script_value cite", "script_value:max_soldiers", "script_value", "max_soldiers", true},
			{"eu5 location", "location:stockholm", "location", "stockholm", true},
			{"eu5 country prefix", "c:SWE", "c", "SWE", true},
			{"id until dot", "situation:foo.var:x", "situation", "foo", true},
			{"type unbound", "type:character_event", "", "", false},
			// A prefix is a single identifier: a dotted chain tail is not one.
			{"dotted scope prefix", "$SCOPE$.var:x", "", "", false},
			{"dotted root prefix", "root.var:x", "", "", false},
			{"dotted chained ephemeral", "scope:a.var:b", "", "", false},
			{"dotted this prefix", "this.culture:english", "", "", false},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				kind, id, ok := ParseTyped(c.in)
				if ok != c.ok || kind != c.kind || id != c.id {
					t.Fatalf("ParseTyped(%q) = %q %q %v want %q %q %v",
						c.in, kind, id, ok, c.kind, c.id, c.ok)
				}
			})
		}
	})

	// ParsePrefixed is hand-matched for speed; it must still agree with the pattern it replaced.
	t.Run("prefixed_matches_pattern", func(t *testing.T) {
		re := regexp.MustCompile(`^(scope|var|local_var|global_var):(\p{L}[\p{L}\p{Nd}_]*)`)
		for _, s := range []string{
			"", "scope", "scope:", "scope:x", "scope:my_target.culture",
			"var:gold", "local_var:i", "global_var:g1", "globalvar:x",
			"scope:1bad", "scope:_bad", "culture:english", "xscope:y",
			"var:a.b:c", "scope:A_9z", "var:", "scope::x",
			"scope:Linnéa", "var:bomb_бомбометр", "scope:vukotić.culture",
			"scope:é", "scope:·", "var:x·y",
		} {
			m := re.FindStringSubmatch(s)
			p, ok := ParsePrefixed(s)
			if (m != nil) != ok {
				t.Fatalf("ParsePrefixed(%q) ok=%v, regex matched=%v", s, ok, m != nil)
			}
			if m == nil {
				continue
			}
			if p.Prefix != m[1] || p.Name != m[2] || p.NameOff != len(m[1])+1 || p.Text != s {
				t.Errorf("ParsePrefixed(%q) = %+v, regex = %q/%q", s, p, m[1], m[2])
			}
		}
	})
}

func TestSkipRHSAndSpans(t *testing.T) {
	t.Run("typed_spans", func(t *testing.T) {
		spans := TypedSpans("situation:foo.var:x")
		if len(spans) != 2 || spans[0].Prefix != "situation" || spans[0].ID != "foo" ||
			spans[1].Prefix != "var" || spans[1].ID != "x" {
			t.Fatalf("spans=%+v", spans)
		}
		sps := TypedSpans("s:STATE_KENYA.region_state:OMA")
		sp, ok := sps[len(sps)-1], len(sps) == 2
		if !ok || sp.Prefix != "region_state" || sp.ID != "OMA" {
			t.Fatalf("cursor pick=%+v ok=%v", sp, ok)
		}
	})
}
