// validate_test.go covers the diagnostics that judge script against what the
// game declares. Each one must stay silent where the game says nothing.

package lsp

import (
	"strings"
	"testing"

	"paradox-modding-tools/services/internal/catalog"
)

func codesOf(ds []Diagnostic) []string {
	out := make([]string, len(ds))
	for i, d := range ds {
		out[i] = d.Code
	}
	return out
}

func hasCode(ds []Diagnostic, code string) bool {
	for _, d := range ds {
		if d.Code == code {
			return true
		}
	}
	return false
}

// TestDuplicateDefinition fires only within one origin: a mod redefining a
// vanilla object is an override, which is the point of modding.
func TestDuplicateDefinition(t *testing.T) {
	s, root := buildSession(t, "ck3", map[string]string{
		"common/traits/00_a.txt": "brave = {\n\tdiplomacy = 1\n}\n",
		"common/traits/01_b.txt": "brave = {\n\tdiplomacy = 2\n}\n",
		"common/traits/02_c.txt": "craven = {\n\tdiplomacy = 1\n}\n",
	}, nil, nil)

	// Reported once, at the later definition, so the first file stays clean.
	if hasCode(Diagnose(s, root+"/common/traits/00_a.txt"), "duplicate-definition") {
		t.Errorf("first definition should be clean")
	}
	if !hasCode(Diagnose(s, root+"/common/traits/01_b.txt"), "duplicate-definition") {
		t.Errorf("second definition of brave not reported")
	}
	if hasCode(Diagnose(s, root+"/common/traits/02_c.txt"), "duplicate-definition") {
		t.Errorf("craven is defined once: %v",
			codesOf(Diagnose(s, root+"/common/traits/02_c.txt")))
	}
}

// citeCache binds a prefix to a database so typed citations can be judged.
func citeCache() *catalog.VanillaCache {
	c := &catalog.VanillaCache{
		PrefixKinds: map[string]string{"culture": "cultures"},
		// scopeKind is reversed from KindScope, and CiteFormOfScope reads it —
		// PrefixKinds alone leaves the declared target unbound to any database.
		KindScope: map[string]string{"cultures": "culture"},
		Schema: &catalog.Schema{
			Scopes: map[string]catalog.ScopeType{
				"character": {ChangeScopes: true, ExecuteEffects: true},
				"culture":   {},
			},
			Triggers: map[string]catalog.EngineToken{
				"has_culture": {In: []string{"character"}, Target: "culture"},
			},
		},
	}
	catalog.PrepareCache(c)
	return c
}

func TestUnresolvedRef(t *testing.T) {
	s, root := buildSession(t, "ck3", map[string]string{
		"common/cultures/00.txt":         "english = {\n\tcolor = { 1 2 3 }\n}\n",
		"common/scripted_triggers/t.txt": "a = { x = culture:english }\nb = { x = culture:nonexistent }\n",
	}, citeCache(), nil)
	ds := Diagnose(s, root+"/common/scripted_triggers/t.txt")
	var msgs []string
	for _, d := range ds {
		if d.Code == "unresolved-ref" {
			msgs = append(msgs, d.Message)
		}
	}
	if len(msgs) != 1 {
		t.Fatalf("want exactly one unresolved-ref, got %v (all: %v)", msgs, codesOf(ds))
	}
	if !strings.Contains(msgs[0], "nonexistent") {
		t.Errorf("message = %q, want it to name the missing key", msgs[0])
	}
}

// TestUnresolvedRefSilentWithoutBinding pins the floor. An unbound prefix means
// PMT does not know that database — which is not evidence the key is missing
// from it, and saying so would be a confident error about correct script.
func TestUnresolvedRefSilentWithoutBinding(t *testing.T) {
	s, root := buildSession(t, "ck3", map[string]string{
		"common/scripted_triggers/t.txt": "a = { x = faith:catholic }\n",
	}, citeCache(), nil)
	if hasCode(Diagnose(s, root+"/common/scripted_triggers/t.txt"), "unresolved-ref") {
		t.Errorf("faith: is not bound to a database PMT holds; want silence")
	}
}

// TestWrongValueType uses the declared argument type only. The install-derived
// field types are deliberately not consulted: being wrong there costs a wrong
// suggestion, not a false accusation.
func TestWrongValueType(t *testing.T) {
	s, root := buildSession(t, "ck3", map[string]string{
		"common/cultures/00.txt":         "english = {\n\tcolor = { 1 2 3 }\n}\n",
		"common/scripted_triggers/t.txt": "a = { has_culture = english }\nb = { has_culture = not_a_culture }\n",
	}, citeCache(), nil)
	ds := Diagnose(s, root+"/common/scripted_triggers/t.txt")
	var msgs []string
	for _, d := range ds {
		if d.Code == "wrong-value-type" {
			msgs = append(msgs, d.Message)
		}
	}
	if len(msgs) != 1 {
		t.Fatalf("want exactly one wrong-value-type, got %v (all: %v)", msgs, codesOf(ds))
	}
	if !strings.Contains(msgs[0], "not_a_culture") {
		t.Errorf("message = %q, want it to name the offending value", msgs[0])
	}
}

// TestWrongValueTypeSilentWithoutSchema pins the design rule for the whole file:
// no declared type system means no declared violations.
func TestWrongValueTypeSilentWithoutSchema(t *testing.T) {
	s, root := buildSession(t, "ck3", map[string]string{
		"common/scripted_triggers/t.txt": "a = { has_culture = not_a_culture }\n",
	}, nil, nil)
	ds := Diagnose(s, root+"/common/scripted_triggers/t.txt")
	if hasCode(ds, "wrong-value-type") || hasCode(ds, "unresolved-ref") {
		t.Errorf("no schema should mean no declared violations: %v", codesOf(ds))
	}
}

// TestValidateSuppressible pins that every new code honours # pmt:ignore, the
// escape hatch a modder needs when the engine is wrong.
func TestValidateSuppressible(t *testing.T) {
	s, root := buildSession(t, "ck3", map[string]string{
		"common/cultures/00.txt": "english = {\n\tcolor = { 1 2 3 }\n}\n",
		"common/scripted_triggers/t.txt": "a = {\n" +
			"\t# pmt:ignore-next-line unresolved-ref\n" +
			"\tx = culture:nonexistent\n}\n",
	}, citeCache(), nil)
	if hasCode(Diagnose(s, root+"/common/scripted_triggers/t.txt"), "unresolved-ref") {
		t.Errorf("pmt:ignore-next-line did not suppress unresolved-ref")
	}
}

// TestApplySeverities covers the settings gate: a code can be turned off or
// re-levelled without the checks knowing anything about configuration.
func TestApplySeverities(t *testing.T) {
	ds := []Diagnostic{
		{Code: "wrong-scope", Severity: sevWarning},
		{Code: "unresolved-ref", Severity: sevWarning},
		{Code: "wrong-value-type", Severity: sevWarning},
	}
	got := ApplySeverities(append([]Diagnostic(nil), ds...), map[string]string{
		"unresolved-ref":   SevOff,
		"wrong-value-type": SevError,
	})
	if hasCode(got, "unresolved-ref") {
		t.Errorf("code set to off still reported: %v", codesOf(got))
	}
	if !hasCode(got, "wrong-scope") {
		t.Errorf("unconfigured code should keep its default: %v", codesOf(got))
	}
	for _, d := range got {
		if d.Code == "wrong-value-type" && d.Severity != sevError {
			t.Errorf("severity = %d, want error", d.Severity)
		}
	}
}

// TestDiagnosticCodesRegistry pins that every code a check can emit is listed,
// because the registry is what the settings surface and pmt:ignore docs read.
// A code missing from it is a code nobody can turn off.
func TestDiagnosticCodesRegistry(t *testing.T) {
	listed := map[string]bool{}
	for _, c := range DiagnosticCodes() {
		listed[c.Code] = true
		if c.Default == "" {
			t.Errorf("%s has no default severity", c.Code)
		}
	}
	for _, code := range []string{
		"duplicate-definition", "unresolved-ref", "wrong-value-type",
		"wrong-scope", "unknown-event", "required-loc", "missing-required-loc",
	} {
		if !listed[code] {
			t.Errorf("%s is emitted but not in the registry", code)
		}
	}
}
