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
	t.Run("skip_object_rhs", func(t *testing.T) {
		if !SkipObjectRHS("$COA$") || !SkipObjectRHS("capital_province.culture") {
			t.Fatal("expected skip")
		}
		if SkipObjectRHS("english") || SkipObjectRHS("culture:english") {
			t.Fatal("ids must not skip")
		}
		// A bare number or boolean is a quantity, never an object name.
		for _, s := range []string{
			"0", "-10", "-20.5", "+3", "1836", "2.75", "yes", "no", "YES", "No",
			"", "$PARAM$", "ns.1",
		} {
			if !SkipObjectRHS(s) {
				t.Errorf("SkipObjectRHS(%q) = false, want skipped", s)
			}
		}
		for _, s := range []string{
			"english", "k_france", "my_effect", "_hidden", "a1", "west_africa",
		} {
			if SkipObjectRHS(s) {
				t.Errorf("SkipObjectRHS(%q) = true, want kept", s)
			}
		}
		// An engine define path and a GUI id with a trailing lexer-attached `;` both reached Workspace Health as dangling.
		for _, s := range []string{
			"define:NTaskContract|HIGH_TASK_CONTRACT_TIER",
			"define:NTaskContract|MEDIUM_TASK_CONTRACT_TIER",
			"struggle_tooltip;",
		} {
			if !SkipObjectRHS(s) {
				t.Errorf("SkipObjectRHS(%q) = false, want true", s)
			}
		}
		// The guard must stay narrow: an ordinary id still has to be harvested.
		for _, s := range []string{"english", "castle_holding", "k_magyar"} {
			if SkipObjectRHS(s) {
				t.Errorf("SkipObjectRHS(%q) = true, want false", s)
			}
		}
	})

	t.Run("skip_field_rhs", func(t *testing.T) {
		if !SkipFieldRHS("target_titles") || !SkipFieldRHS("none") {
			t.Fatal("CB target selectors must skip")
		}
		if SkipFieldRHS("e_hre") || SkipFieldRHS("catholic") {
			t.Fatal("real ids must not skip")
		}
	})

	t.Run("typed_spans", func(t *testing.T) {
		spans := TypedSpans("situation:foo.var:x")
		if len(spans) != 2 || spans[0].Prefix != "situation" || spans[0].ID != "foo" ||
			spans[1].Prefix != "var" || spans[1].ID != "x" {
			t.Fatalf("spans=%+v", spans)
		}
		sp, ok := TypedSpanAt("s:STATE_KENYA.region_state:OMA", len("s:STATE_KENYA."))
		if !ok || sp.Prefix != "region_state" || sp.ID != "OMA" {
			t.Fatalf("cursor pick=%+v ok=%v", sp, ok)
		}
	})
}
