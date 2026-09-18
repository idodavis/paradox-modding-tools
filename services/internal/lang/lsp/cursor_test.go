// cursor_test.go covers the probe's scope inference (probeAt(...).scope) and
// the file-wide scope-misuse walk (ScopeMisuses) that feeds wrong-scope.

package lsp

import (
	"os"
	"path/filepath"
	"testing"

	"paradox-modding-tools/services/internal/lang/declared"
	"paradox-modding-tools/services/internal/lang/derived"
	"paradox-modding-tools/services/internal/lang/session"
)

// slotBodies is what the install writes inside each slot; a block holding
// declared tokens is a body, and the scope walk enters a body and stops
// anywhere else.
var slotBodies = map[string][]string{
	"effect":            {"add_gold"},
	"immediate":         {"add_gold"},
	"trigger":           {"is_adult"},
	"limit":             {"is_adult"},
	"option":            {"add_gold"},
	"modifier":          {"is_adult"},
	"weight_multiplier": {"is_adult"},
	"capital_county":    {"add_title_law", "has_title_law"},
	"holder":            {"add_gold"},
}

// ck3Schema mirrors the shape of a real CK3 dump, trimmed to what the walk needs.
func ck3Schema() *declared.Schema {
	return &declared.Schema{
		Scopes: map[string]declared.ScopeType{
			"none":         {ChangeScopes: true},
			"character":    {ChangeScopes: true, ExecuteEffects: true, EvaluateTriggers: true},
			"landed_title": {ChangeScopes: true, ExecuteEffects: true, EvaluateTriggers: true},
			"faith":        {ChangeScopes: true, ExecuteEffects: true, EvaluateTriggers: true},
		},
		Links: map[string]declared.ScopeLink{
			"capital_county": {In: []string{"character"}, Out: "landed_title"},
			"holder":         {In: []string{"landed_title"}, Out: "character"},
		},
		Effects: map[string]declared.EngineToken{
			"add_gold":      {In: []string{"character"}},
			"add_title_law": {In: []string{"landed_title"}},
			"every_vassal":  {In: []string{"character"}, Target: "character"},
		},
		Triggers: map[string]declared.EngineToken{
			"is_adult":      {In: []string{"character"}},
			"has_title_law": {In: []string{"landed_title"}},
		},
		OnActions: map[string]string{
			"on_birth":        "character",
			"on_game_started": "none",
		},
	}
}

// scopedCache is ck3Schema's shape as a vanillaCache, for tests that build a
// session through buildSession rather than scopeSession.
func scopedCache() *vanillaCache {
	return &vanillaCache{Schema: ck3Schema(), NestedMembers: slotBodies}
}

func scopeSession(t *testing.T, sc *declared.Schema, rel, body string) (*session.Session, string) {
	t.Helper()
	root := t.TempDir()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "descriptor.mod"), []byte("name=\"t\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cache := &vanillaCache{Schema: sc, NestedMembers: slotBodies}
	s := session.New("ws", "ck3", "english", toIndex(cache, nil),
		[]derived.ModInput{{Origin: "mod", Root: root, Order: 0}})
	s.DidOpen(p, body)
	return s, p
}

func scopeAt(t *testing.T, s *session.Session, p, body, needle string) string {
	t.Helper()
	line, col := lineCol(body, needle)
	pr, _ := probeAt(s, p, line, col)
	return pr.scope
}

// TestScopeAt is the scope inference every hover and completion request reads:
// where the walk enters, links, iterates, or has to decline.
func TestScopeAt(t *testing.T) {
	for _, tt := range []struct {
		name, rel, body, needle, want string
		schema                        *declared.Schema
	}{
		{"on_action root", "common/on_action/x.txt",
			"on_birth = {\n\teffect = {\n\t\tadd_gold = 5\n\t}\n}\n", "add_gold", "character", nil},
		{"iterator", "common/on_action/x.txt",
			"on_birth = {\n\teffect = {\n\t\tevery_vassal = {\n\t\t\tis_adult = yes\n\t\t}\n\t}\n}\n",
			"is_adult", "character", nil},
		{"link", "common/on_action/x.txt",
			"on_birth = {\n\teffect = {\n\t\tcapital_county = {\n\t\t\tholder = {\n" +
				"\t\t\t\tadd_gold = 50\n\t\t\t}\n\t\t}\n\t}\n}\n",
			"add_gold", "character", nil}, // capital_county -> landed_title -> holder -> back to character
		{"unknown stops", "common/on_action/x.txt",
			"on_birth = {\n\teffect = {\n\t\tsome_unknown_block = {\n\t\t\tadd_gold = 1\n\t\t}\n\t}\n}\n",
			"add_gold", "", nil},
		// EU5's modifier link outputs "value, boolean", naming no single scope; a
		// weight block opening `modifier = { … }` must not adopt that as a scope.
		{"multi-output link is not a hop", "common/on_action/x.txt",
			"on_birth = {\n\tweight_multiplier = {\n\t\tmodifier = {\n\t\t\tadd_gold = 5\n\t\t}\n\t}\n}\n",
			"add_gold", "character", func() *declared.Schema {
				sc := ck3Schema()
				sc.Links["modifier"] = declared.ScopeLink{Global: true, Out: "value, boolean"}
				return sc
			}()},
		// With no schema there is no engine API loaded, so nothing is known well
		// enough to name a scope at all.
		{"schema absent", "events/x.txt",
			"namespace = t\n\nt.1 = {\n\timmediate = { add_gold = 1 }\n}\n", "add_gold", "", &declared.Schema{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			sc := tt.schema
			if sc == nil {
				sc = ck3Schema()
			}
			s, p := scopeSession(t, sc, tt.rel, tt.body)
			if got := scopeAt(t, s, p, tt.body, tt.needle); got != tt.want {
				t.Errorf("scope = %q, want %q", got, tt.want)
			}
		})
	}

	// An event's root scope comes from the on_action that fires it, but only
	// when the fire site sits at the on_action's own level: CK3's
	// on_faith_monthly is faith-scoped and reaches a nested trigger_event
	// fourteen character-scoped blocks deep, and reading through that nesting
	// misreported 24 lines of correct vanilla CK3 as wrong-scope.
	t.Run("event root ignores a rescoped fire site", func(t *testing.T) {
		sc := ck3Schema()
		sc.OnActions["on_faith_monthly"] = "faith"
		root := t.TempDir()
		write(t, root, "descriptor.mod", "name=\"t\"\n")
		write(t, root, "common/on_action/faith.txt", "on_faith_monthly = {\n"+
			"\tevents = {\n\t\tfaith_at_root.1\n\t}\n"+
			"\teffect = {\n\t\tevery_vassal = {\n\t\t\ttrigger_event = faith_nested.1\n\t\t}\n\t}\n}\n")
		eventBody := "namespace = faith_at_root\n\nfaith_at_root.1 = {\n\timmediate = {\n\t\tadd_gold = 1\n\t}\n}\n" +
			"\nnamespace = faith_nested\n\nfaith_nested.1 = {\n\timmediate = {\n\t\tadd_gold = 2\n\t}\n}\n"
		ep := write(t, root, "events/faith.txt", eventBody)
		cache := &vanillaCache{Schema: sc, NestedMembers: slotBodies}
		s := session.New("ws", "ck3", "english", toIndex(cache, nil),
			[]derived.ModInput{{Origin: "mod", Root: root, Order: 0}})
		s.DidOpen(ep, eventBody)
		if got := scopeAt(t, s, ep, eventBody, "add_gold = 1"); got != "faith" {
			t.Errorf("top-level fire: scope = %q, want faith", got)
		}
		if got := scopeAt(t, s, ep, eventBody, "add_gold = 2"); got != "" {
			t.Errorf("fire site nested under every_vassal: scope = %q, want none", got)
		}
	})
}

// TestScopeMisuses covers the file-wide walk wrong-scope reads: it must find
// a real misuse wherever it sits (not only inside common/on_action, which was
// the only place it ever ran), and stay silent wherever the walk cannot be
// certain.
func TestScopeMisuses(t *testing.T) {
	t.Run("fires outside an on_action, with the right fields", func(t *testing.T) {
		body := "namespace = t\n\nt.1 = {\n\timmediate = {\n\t\tevery_vassal = {\n" +
			"\t\t\tadd_title_law = x\n\t\t}\n\t}\n}\n"
		s, p := scopeSession(t, ck3Schema(), "events/x.txt", body)
		got := ScopeMisuses(s, p, s.Parsed(p))
		if len(got) != 1 {
			t.Fatalf("misuses = %+v, want exactly one", got)
		}
		m := got[0]
		if m.Key != "add_title_law" || m.Scope != "character" || len(m.Allowed) != 1 || m.Allowed[0] != "landed_title" {
			t.Errorf("misuse = %+v, want add_title_law/character/[landed_title]", m)
		}
	})
	t.Run("silent with no declared schema", func(t *testing.T) {
		body := "on_birth = {\n\teffect = {\n\t\tcapital_county = {\n\t\t\tadd_gold = 1\n\t\t}\n\t}\n}\n"
		s, p := scopeSession(t, &declared.Schema{}, "common/on_action/x.txt", body)
		if got := ScopeMisuses(s, p, s.Parsed(p)); len(got) != 0 {
			t.Errorf("misuses without a schema = %+v, want none", got)
		}
	})
	t.Run("silent under an unknown transition", func(t *testing.T) {
		body := "on_birth = {\n\teffect = {\n\t\tsome_unknown_block = {\n\t\t\tadd_title_law = x\n\t\t}\n\t}\n}\n"
		s, p := scopeSession(t, ck3Schema(), "common/on_action/x.txt", body)
		if got := ScopeMisuses(s, p, s.Parsed(p)); len(got) != 0 {
			t.Errorf("misuse reported through an unknown transition: %+v", got)
		}
	})
}
