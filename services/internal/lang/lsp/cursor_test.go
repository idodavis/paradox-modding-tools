// cursor_test.go covers the probe's scope inference and the file-wide scope-misuse walk that feeds wrong-scope.

package lsp

import (
	"path/filepath"
	"testing"

	"paradox-modding-tools/services/internal/lang/session"
)

func scopeAt(t *testing.T, s *session.Session, p, body, needle string) string {
	t.Helper()
	line, col := lineCol(body, needle)
	pr, _ := probeAt(s, p, line, col)
	return pr.scope
}

func TestScopeAt(t *testing.T) {
	for _, tt := range []struct{ name, rel, body, needle, want string }{
		{"on_action root", "common/on_action/x.txt", "on_birth = {\n\teffect = {\n\t\tadd_gold = 5\n\t}\n}\n", "add_gold", "character"},
		{"iterator", "common/on_action/x.txt", "on_birth = {\n\teffect = {\n\t\tevery_vassal = {\n\t\t\tis_adult = yes\n\t\t}\n\t}\n}\n", "is_adult", "character"},
		// capital_county -> landed_title -> holder -> back to character.
		{"link", "common/on_action/x.txt", "on_birth = {\n\teffect = {\n\t\tcapital_county = {\n\t\t\tholder = {\n\t\t\t\tadd_gold = 50\n\t\t\t}\n\t\t}\n\t}\n}\n", "add_gold", "character"},
		{"unknown stops", "common/on_action/x.txt", "on_birth = {\n\teffect = {\n\t\tsome_unknown_block = {\n\t\t\tadd_gold = 1\n\t\t}\n\t}\n}\n", "add_gold", ""},
		// A link naming several output types names no single scope; a weight block opening `modifier = { … }` must not adopt it.
		{"multi-output link is not a hop", "common/on_action/x.txt", "on_birth = {\n\tweight_multiplier = {\n\t\tmodifier = {\n\t\t\tadd_gold = 5\n\t\t}\n\t}\n}\n", "add_gold", "character"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s, root := buildSession(t, "ck3", map[string]string{tt.rel: tt.body})
			if got := scopeAt(t, s, filepath.Join(root, filepath.FromSlash(tt.rel)), tt.body, tt.needle); got != tt.want {
				t.Errorf("scope = %q, want %q", got, tt.want)
			}
		})
	}
	// With no type system loaded nothing is known well enough to name a scope at all.
	t.Run("schema absent", func(t *testing.T) {
		body := "namespace = t\n\nt.1 = {\n\timmediate = { add_gold = 1 }\n}\n"
		s, root := bareSession(t, map[string]string{"events/x.txt": body})
		if got := scopeAt(t, s, filepath.Join(root, "events", "x.txt"), body, "add_gold"); got != "" {
			t.Errorf("scope = %q, want none", got)
		}
	})
	// An event's root scope comes from the on_action that fires it, but only from a fire site at the on_action's own level.
	t.Run("event root ignores a rescoped fire site", func(t *testing.T) {
		eventBody := "namespace = faith_at_root\n\nfaith_at_root.1 = {\n\timmediate = {\n\t\tadd_gold = 1\n\t}\n}\n" +
			"\nnamespace = faith_nested\n\nfaith_nested.1 = {\n\timmediate = {\n\t\tadd_gold = 2\n\t}\n}\n"
		s, root := buildSession(t, "ck3", map[string]string{
			"common/on_action/faith.txt": "on_faith_monthly = {\n\tevents = {\n\t\tfaith_at_root.1\n\t}\n" +
				"\teffect = {\n\t\tevery_vassal = {\n\t\t\ttrigger_event = faith_nested.1\n\t\t}\n\t}\n}\n",
			"events/faith.txt": eventBody,
		})
		ep := filepath.Join(root, "events", "faith.txt")
		if got := scopeAt(t, s, ep, eventBody, "add_gold = 1"); got != "faith" {
			t.Errorf("top-level fire: scope = %q, want faith", got)
		}
		if got := scopeAt(t, s, ep, eventBody, "add_gold = 2"); got != "" {
			t.Errorf("fire site nested under every_vassal: scope = %q, want none", got)
		}
	})
}

func TestScopeMisuses(t *testing.T) {
	t.Run("fires outside an on_action, with the right fields", func(t *testing.T) {
		body := "namespace = t\n\nt.1 = {\n\timmediate = {\n\t\tevery_vassal = {\n\t\t\tadd_title_law = x\n\t\t}\n\t}\n}\n"
		s, p := ck3Sess(t, body)
		got := ScopeMisuses(s, p, s.Parsed(p))
		if len(got) != 1 {
			t.Fatalf("misuses = %+v, want exactly one", got)
		}
		if m := got[0]; m.Key != "add_title_law" || m.Scope != "character" || len(m.Allowed) != 1 || m.Allowed[0] != "landed_title" {
			t.Errorf("misuse = %+v, want add_title_law/character/[landed_title]", m)
		}
	})
	t.Run("silent with no declared schema", func(t *testing.T) {
		body := "on_birth = {\n\teffect = {\n\t\tcapital_county = {\n\t\t\tadd_gold = 1\n\t\t}\n\t}\n}\n"
		s, root := bareSession(t, map[string]string{"common/on_action/x.txt": body})
		p := filepath.Join(root, "common", "on_action", "x.txt")
		if got := ScopeMisuses(s, p, s.Parsed(p)); len(got) != 0 {
			t.Errorf("misuses without a schema = %+v, want none", got)
		}
	})
	t.Run("silent under an unknown transition", func(t *testing.T) {
		body := "on_birth = {\n\teffect = {\n\t\tsome_unknown_block = {\n\t\t\tadd_title_law = x\n\t\t}\n\t}\n}\n"
		s, root := buildSession(t, "ck3", map[string]string{"common/on_action/x.txt": body})
		p := filepath.Join(root, "common", "on_action", "x.txt")
		if got := ScopeMisuses(s, p, s.Parsed(p)); len(got) != 0 {
			t.Errorf("misuse reported through an unknown transition: %+v", got)
		}
	})
}
