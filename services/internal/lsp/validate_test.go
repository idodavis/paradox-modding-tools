// validate_test.go covers the diagnostics that judge script against what the
// game declares. Each one must stay silent where the game says nothing.

package lsp

import (
	"strings"
	"testing"

	"paradox-modding-tools/services/internal/catalog"
	"paradox-modding-tools/services/internal/parser/jomini"
	"paradox-modding-tools/services/internal/parser/loc"
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

// EnabledCodes decides whether a check RUNS; ApplySeverities decides whether its
// findings are KEPT. They resolve a setting by the same rule, written twice, so
// they are pinned to each other here — a disagreement would be a check that
// silently stops running while the panel still claims to be watching for it.
func TestEnabledCodesAgreesWithApplySeverities(t *testing.T) {
	// Everything a settings map can hold, including the two shapes that mean
	// "back to the default": absent, and stored-but-empty.
	values := []string{"", SevOff, SevWarning, SevError, "nonsense"}
	for _, c := range DiagnosticCodes() {
		for _, v := range append([]string{"\x00absent"}, values...) {
			settings := map[string]string{}
			if v != "\x00absent" {
				settings[c.Code] = v
			}
			kept := len(ApplySeverities(
				[]Diagnostic{{Code: c.Code, Severity: sevWarning}}, settings)) == 1
			if got := EnabledCodes(settings)[c.Code]; got != kept {
				t.Errorf("%s with setting %q: EnabledCodes=%v but ApplySeverities "+
					"keeps=%v", c.Code, v, got, kept)
			}
		}
	}
}

// TestDiagnosticCodesRegistry pins that every code a check can emit is listed,
// because the registry is what the settings surface and pmt:ignore docs read.
// A code missing from it is a code nobody can turn off -- and, worse, one that
// can still be turned off by hand and never turned back on, since ApplySeverities
// honours a stored severity whether or not the registry has heard of the code.
func TestDiagnosticCodesRegistry(t *testing.T) {
	listed := map[string]bool{}
	for _, c := range DiagnosticCodes() {
		listed[c.Code] = true
		if c.Default == "" {
			t.Errorf("%s has no default severity", c.Code)
		}
		if c.Group != GroupCheck && c.Group != GroupParse {
			t.Errorf("%s has group %q, want %q or %q", c.Code, c.Group, GroupCheck, GroupParse)
		}
		if c.Summary == "" {
			t.Errorf("%s has no summary", c.Code)
		}
	}
	for _, code := range []string{
		"duplicate-definition", "unresolved-ref", "wrong-value-type",
		"wrong-scope", "unknown-event", "required-loc", "missing-required-loc",
		"unknown-key", "wrong-declared-value",
	} {
		if !listed[code] {
			t.Errorf("%s is emitted but not in the registry", code)
		}
	}
	// Parse codes, taken from the parsers rather than copied as strings, so the
	// spelling cannot drift. diagnostics.go emits loc codes with a "loc-"
	// prefix and script codes bare.
	for _, c := range []loc.ErrorCode{
		loc.ErrNoHeader, loc.ErrBadEntry, loc.ErrTabIndent,
		loc.ErrUnterminatedValue, loc.ErrContentBeforeHeader,
	} {
		if !listed["loc-"+string(c)] {
			t.Errorf("loc-%s is emitted but not in the registry", c)
		}
	}
	for _, c := range []jomini.ErrorCode{
		jomini.ErrUnclosedBrace, jomini.ErrStrayClose,
		jomini.ErrUnterminatedString, jomini.ErrMissingValue,
	} {
		if !listed[string(c)] {
			t.Errorf("%s is emitted but not in the registry", c)
		}
	}
}

// TestUnresolvedRefIgnoresEphemeral pins the fix for six findings on a real CK3
// mod. `flag:private_chamber` names a value the script invents as it runs — the
// same as a saved scope or a variable — so nothing defines it anywhere and an
// absence proves nothing.
func TestUnresolvedRefIgnoresEphemeral(t *testing.T) {
	c := citeCache()
	c.PrefixKinds["flag"] = "flag"
	catalog.PrepareCache(c)
	s, root := buildSession(t, "ck3", map[string]string{
		"common/flags/f.txt": "some_flag = { }\n",
		"common/scripted_effects/e.txt": "a = {\n" +
			"\tset_variable = { name = where value = flag:private_chamber }\n}\n",
	}, c, nil)
	if hasCode(Diagnose(s, root+"/common/scripted_effects/e.txt"), "unresolved-ref") {
		t.Errorf("a flag the script invents was reported as unresolved")
	}
}

// TestDuplicateDefinitionIgnoresNested pins what took this check from 1,614
// findings on vanilla CK3 to zero. A definition harvested from inside another
// one — a game rule's options, a script parameter, the members of a nested
// shape — is a real definition but not the file's subject, and every activity
// type in the game carries `special_guests`.
func TestDuplicateDefinitionIgnoresNested(t *testing.T) {
	c := &catalog.VanillaCache{}
	catalog.PrepareCache(c)
	s, root := buildSession(t, "ck3", map[string]string{
		"common/activity_types/a.txt": "camp_party = {\n\tspecial_guests = { host }\n}\n",
		"common/activity_types/b.txt": "chariot_race = {\n\tspecial_guests = { host }\n}\n",
	}, c, nil)
	for _, f := range []string{"a", "b"} {
		ds := Diagnose(s, root+"/common/activity_types/"+f+".txt")
		if hasCode(ds, "duplicate-definition") {
			t.Errorf("%s.txt: a member shared by two definitions is not a duplicate: %v",
				f, codesOf(ds))
		}
	}
}

// TestDuplicateDefinitionSkipsGraphics pins the other half. EU5 ships map data
// under dlc/<pack>/in_game/gfx/map/city_data in a different syntax, and every
// one of those files carries a `mesh_list` block. They are not definitions
// competing for a name.
func TestDuplicateDefinitionSkipsGraphics(t *testing.T) {
	c := &catalog.VanillaCache{}
	catalog.PrepareCache(c)
	s, root := buildSession(t, "eu5", map[string]string{
		"in_game/gfx/map/city_data/one.txt": "mesh_list = {\n\ta = 1\n}\n",
		"in_game/gfx/map/city_data/two.txt": "mesh_list = {\n\ta = 2\n}\n",
	}, c, nil)
	if hasCode(Diagnose(s, root+"/in_game/gfx/map/city_data/two.txt"), "duplicate-definition") {
		t.Errorf("map data reported as duplicate definitions")
	}
}

// TestUnknownKey covers the check tiger is best known for, and the gate that
// makes it shippable. Keyed by block name alone it fired 18,341 times on 400
// vanilla CK3 files, because `option` is a block in an event and another in
// accolade_names holding nothing in common. Only a name exactly one kind uses
// can have an absence judged. See GAME-SYNTAX §14.
func TestUnknownKey(t *testing.T) {
	c := &catalog.VanillaCache{
		NestedMembers: map[string][]string{
			"triggered_desc": {"desc", "trigger", "first_valid", "random_valid",
				"triggered_effect", "modifier"},
			"option": {"name", "trigger", "ai_chance", "custom_tooltip",
				"flavor", "show_as_unavailable"},
		},
		// `option` is a block name several kinds use; triggered_desc is not.
		UnambiguousBlocks: map[string]bool{"triggered_desc": true},
	}
	catalog.PrepareCache(c)
	body := "namespace = ns\nns.1 = {\n" +
		"\ttriggered_desc = {\n\t\tnot_a_real_key = yes\n\t}\n" +
		"\toption = {\n\t\talso_not_real = yes\n\t}\n}\n"
	s, root := buildSession(t, "ck3", map[string]string{"events/x.txt": body}, c, nil)
	var msgs []string
	for _, d := range Diagnose(s, root+"/events/x.txt") {
		if d.Code == "unknown-key" {
			msgs = append(msgs, d.Message)
		}
	}
	if len(msgs) != 1 {
		t.Fatalf("want exactly one unknown-key, got %v", msgs)
	}
	if !strings.Contains(msgs[0], "not_a_real_key") {
		t.Errorf("message = %q, want the key in the judgeable block", msgs[0])
	}
}

// TestUnknownKeySilentWithoutEvidence pins the floors. A block seen a few times
// tells you what it CAN hold, never what it cannot, and a name the model knows
// anywhere is not evidence of a typo.
func TestUnknownKeySilentWithoutEvidence(t *testing.T) {
	c := &catalog.VanillaCache{
		NestedMembers:     map[string][]string{"thin_block": {"a", "b"}},
		UnambiguousBlocks: map[string]bool{"thin_block": true},
	}
	catalog.PrepareCache(c)
	body := "d = {\n\tthin_block = {\n\t\tanything = yes\n\t}\n}\n"
	s, root := buildSession(t, "ck3",
		map[string]string{"common/decisions/d.txt": body}, c, nil)
	if hasCode(Diagnose(s, root+"/common/decisions/d.txt"), "unknown-key") {
		t.Errorf("two members is not enough shape to judge an absence")
	}
}

// TestDuplicateDefinitionAllowsInlineDeclarations is ck3-quieter-events, which
// carries one scripted_trigger in two event files under the comment
// "Duplicating because CK3 is dumb and can't find it in the birth_events.txt
// file". The engine resolves an inline declaration within its own file, so the
// repeat is required and calling it a collision accuses correct script.
func TestDuplicateDefinitionAllowsInlineDeclarations(t *testing.T) {
	const decl = "scripted_trigger allow_naming_trigger = {\n\tis_ai = no\n}\n"
	s, root := buildSession(t, "ck3", map[string]string{
		"events/birth_events.txt":   "namespace = birth\n\n" + decl,
		"events/quieter_events.txt": "namespace = qe\n\n" + decl,
	}, nil, nil)

	for _, rel := range []string{"events/birth_events.txt", "events/quieter_events.txt"} {
		if hasCode(Diagnose(s, root+"/"+rel), "duplicate-definition") {
			t.Errorf("%s: inline declaration reported as a duplicate", rel)
		}
	}
}

// declCache is a session cache carrying one documented value set, filed per
// kind the way the harvest files it.
func declCache() *catalog.VanillaCache {
	c := &catalog.VanillaCache{
		DeclaredFieldTypesByKind: map[string]map[string]string{
			"scripted_relations": {
				"uses_diplo_capacity": "none/mutual/giving/receiving",
				"break_on_war":        "yes/no",
				"type":                "diplomacy/subject/union",
			},
		},
		DeclaredFieldTypes: map[string]string{
			"type": "diplomacy/subject/union",
		},
	}
	catalog.PrepareCache(c)
	return c
}

// TestDeclaredValueRespectsSet fires only where the value is outside the set the
// game documents for that field, and only inside the kind that documents it.
func TestDeclaredValueRespectsSet(t *testing.T) {
	s, root := buildSession(t, "eu5", map[string]string{
		"common/scripted_relations/00_ok.txt": "alliance = {\n" +
			"\tbreak_on_war = yes\n\tuses_diplo_capacity = mutual\n}\n",
		"common/scripted_relations/01_bad.txt": "vassalage = {\n" +
			"\tuses_diplo_capacity = sometimes\n}\n",
		// GAME-SYNTAX 3f: `type` is a value set in a scripted relation and an
		// event presentation kind in an event. The per-kind map is what keeps
		// the two apart, and the global map must never reach this check.
		"events/00_e.txt": "namespace = e\n\ne.1 = {\n\ttype = character_event\n}\n",
	}, declCache(), nil)

	for _, tt := range []struct {
		rel  string
		want bool
	}{
		{"common/scripted_relations/00_ok.txt", false},
		{"common/scripted_relations/01_bad.txt", true},
		{"events/00_e.txt", false},
	} {
		t.Run(tt.rel, func(t *testing.T) {
			got := hasCode(Diagnose(s, root+"/"+tt.rel), "wrong-declared-value")
			if got != tt.want {
				t.Errorf("wrong-declared-value = %v, want %v (all=%v)",
					got, tt.want, codesOf(Diagnose(s, root+"/"+tt.rel)))
			}
		})
	}
}

// TestDeclaredValueSkipsComputed: a value the script computes cannot be matched
// against a literal set. A macro parameter is not knowable until the macro is
// called, and a citation or script value names something rather than being one.
func TestDeclaredValueSkipsComputed(t *testing.T) {
	s, root := buildSession(t, "eu5", map[string]string{
		"common/scripted_relations/00_m.txt": "x = {\n" +
			"\tuses_diplo_capacity = $CAP$\n\tbreak_on_war = scope:war_flag\n" +
			"\ttype = some.path\n}\n",
	}, declCache(), nil)
	if ds := Diagnose(s, root+"/common/scripted_relations/00_m.txt"); hasCode(ds, "wrong-declared-value") {
		t.Errorf("computed values reported: %v", codesOf(ds))
	}
}

// TestDeclaredValueIsExperimentalAndOff pins the shipping condition. The check
// runs, but ApplySeverities drops it until someone turns it on, because its
// vanilla gate has not been demonstrated.
func TestDeclaredValueIsExperimentalAndOff(t *testing.T) {
	var found bool
	for _, c := range DiagnosticCodes() {
		if c.Code != "wrong-declared-value" {
			continue
		}
		found = true
		if !c.Experimental {
			t.Errorf("not marked experimental")
		}
		if c.Default != SevOff {
			t.Errorf("default = %q, want %q", c.Default, SevOff)
		}
	}
	if !found {
		t.Fatalf("wrong-declared-value is emitted but not in the registry")
	}
	s, root := buildSession(t, "eu5", map[string]string{
		"common/scripted_relations/01_bad.txt": "vassalage = {\n" +
			"\tuses_diplo_capacity = sometimes\n}\n",
	}, declCache(), nil)
	raw := Diagnose(s, root+"/common/scripted_relations/01_bad.txt")
	if !hasCode(raw, "wrong-declared-value") {
		t.Fatalf("check did not fire at all: %v", codesOf(raw))
	}
	if hasCode(ApplySeverities(raw, nil), "wrong-declared-value") {
		t.Errorf("experimental code survived default severities")
	}
	on := ApplySeverities(raw, map[string]string{"wrong-declared-value": SevWarning})
	if !hasCode(on, "wrong-declared-value") {
		t.Errorf("toggling it on did not bring it back")
	}
}

// TestSeverityClearedIsUnset pins a bug the settings pane found by being run.
//
// SaveSettings merges keys and cannot remove one, so the pane clears a choice by
// writing "". Treating that as "keep the severity the check emitted" silently
// switched every experimental code ON the moment somebody pressed Reset to
// defaults -- the UI said Off while the engine reported findings.
func TestSeverityClearedIsUnset(t *testing.T) {
	ds := []Diagnostic{
		{Code: "wrong-declared-value", Severity: sevWarning},
		{Code: "duplicate-definition", Severity: sevWarning},
	}
	clone := func() []Diagnostic { return append([]Diagnostic(nil), ds...) }

	// Cleared: identical to never having chosen. The experimental code goes
	// back to off, the ordinary one stays reported.
	got := ApplySeverities(clone(), map[string]string{
		"wrong-declared-value": "",
		"duplicate-definition": "",
	})
	if hasCode(got, "wrong-declared-value") {
		t.Errorf("cleared experimental code reported: %v", codesOf(got))
	}
	if !hasCode(got, "duplicate-definition") {
		t.Errorf("cleared ordinary code lost its default: %v", codesOf(got))
	}

	// And clearing is not the same as turning it on.
	on := ApplySeverities(clone(), map[string]string{"wrong-declared-value": SevWarning})
	if !hasCode(on, "wrong-declared-value") {
		t.Errorf("explicit warning did not report: %v", codesOf(on))
	}
}

// TestUnknownToken fires on an effect nothing in the model recognises, and
// stays silent on everything that only looks unrecognised.
func TestUnknownToken(t *testing.T) {
	c := scopedCache()
	// add_activity_log_entry documents its arguments across several lines, the
	// shape that made `tags` 308 findings on vanilla CK3.
	c.Schema.Effects["add_activity_log_entry"] = catalog.EngineToken{
		In: []string{"character"},
		Usage: "add_activity_log_entry = {\nkey = <loc_key>\n" +
			"tags = { <a> <b> }\ncharacter = <character>\n}",
	}
	c.Schema.Effects["ordered_vassal"] = catalog.EngineToken{
		In: []string{"character"}, Target: "character",
		Usage: "ordered_vassal = { limit = { <triggers> } order_by = <script_value> <effects> }",
	}
	catalog.PrepareCache(c)

	for _, tt := range []struct {
		name, body string
		want       bool
	}{
		{"unknown effect", "on_birth = {\n\teffect = {\n\t\tnot_a_real_effect = yes\n\t}\n}\n", true},
		{"declared effect", "on_birth = {\n\teffect = {\n\t\tadd_gold = 5\n\t}\n}\n", false},
		// A declared token's block is its argument list, all the way down.
		{"declared arguments", "on_birth = {\n\teffect = {\n\t\tadd_activity_log_entry = {\n" +
			"\t\t\tkey = x\n\t\t\ttags = { departure }\n\t\t}\n\t}\n}\n", false},
		// An iterator opens a slot AND takes arguments; order_by is documented.
		{"iterator argument", "on_birth = {\n\teffect = {\n\t\tordered_vassal = {\n" +
			"\t\t\torder_by = 1\n\t\t\tadd_gold = 5\n\t\t}\n\t}\n}\n", false},
		// ...but real statements inside it are still judged.
		{"statement inside an iterator", "on_birth = {\n\teffect = {\n\t\tordered_vassal = {\n" +
			"\t\t\torder_by = 1\n\t\t\tnot_a_real_effect = yes\n\t\t}\n\t}\n}\n", true},
		// Script grammar is not a claim about the engine API.
		{"grammar", "on_birth = {\n\teffect = {\n\t\tif = {\n\t\t\tlimit = { is_adult = yes }\n" +
			"\t\t\tadd_gold = 5\n\t\t}\n\t}\n}\n", false},
		{"alternative_limit", "on_birth = {\n\teffect = {\n\t\tif = {\n" +
			"\t\t\talternative_limit = { is_adult = yes }\n\t\t}\n\t}\n}\n", false},
		// Outside a script slot there are no statements to judge; that is
		// unknown-key's question, not this one.
		{"outside a slot", "on_birth = {\n\tnot_a_real_effect = yes\n}\n", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s, root := buildSession(t, "ck3",
				map[string]string{"common/on_action/x.txt": tt.body}, c, nil)
			got := hasDiag(Diagnose(s, root+"/common/on_action/x.txt"), "unknown-token")
			if got != tt.want {
				t.Errorf("unknown-token = %v, want %v (all=%v)", got, tt.want,
					codesOf(Diagnose(s, root+"/common/on_action/x.txt")))
			}
		})
	}
}

// A macro's call site passes parameters, not statements.
func TestUnknownTokenSkipsMacroParameters(t *testing.T) {
	files := map[string]string{
		"common/scripted_effects/m.txt": "my_macro_effect = {\n\tadd_gold = $AMOUNT$\n}\n",
		"common/on_action/x.txt": "on_birth = {\n\teffect = {\n\t\tmy_macro_effect = {\n" +
			"\t\t\tAMOUNT = 5\n\t\t}\n\t}\n}\n",
	}
	s, root := buildSession(t, "ck3", files, scopedCache(), nil)
	if hasDiag(Diagnose(s, root+"/common/on_action/x.txt"), "unknown-token") {
		t.Errorf("judged a macro's parameters as engine tokens")
	}
}

// With no type system every token is unknown, so the honest output is silence.
func TestUnknownTokenSilentWithoutSchema(t *testing.T) {
	s, root := buildSession(t, "ck3", map[string]string{
		"common/on_action/x.txt": "on_birth = {\n\teffect = {\n\t\tnot_a_real_effect = yes\n\t}\n}\n",
	}, nil, nil)
	if hasDiag(Diagnose(s, root+"/common/on_action/x.txt"), "unknown-token") {
		t.Errorf("claimed a token is unknown with nothing to know it against")
	}
}
