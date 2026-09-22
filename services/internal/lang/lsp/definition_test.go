// definition_test.go covers go-to-definition and find-references from every kind of word, in script and in localization, and the outline of a file and of the workspace.
package lsp

import (
	"slices"
	"strings"
	"testing"
)

func TestDefinition(t *testing.T) {
	s, dir := fixture(t, "ck3")
	opened(s, dir, "common/scripted_triggers/x.txt", "m_trig = {\n\thas_trait = brave\n\tis_adult = yes\n}\n")
	for _, tt := range []struct {
		what, rel, text, needle string
		plus                    int
		files                   []string // a suffix of each location's rel, in order
	}{
		{"a macro from its call", "events/e.txt", "namespace = m\nm.1 = {\n\ttrigger = { m_trig = yes }\n}\n", "m_trig", 0, []string{"scripted_triggers/x.txt"}},
		{"a macro on its own name", "common/scripted_triggers/x.txt", "m_trig = {\n\thas_trait = brave\n}\n", "m_trig", 0, []string{"scripted_triggers/x.txt"}},
		{"an inline declaration, on the name", "events/i.txt", "namespace = i\nscripted_trigger i_trig = { is_adult = yes }\ni.1 = { trigger = { i_trig = yes } }\n", "i_trig = {", 0, []string{"events/i.txt"}},
		{"an inline declaration, from its call", "events/i.txt", "namespace = i\nscripted_trigger i_trig = { is_adult = yes }\ni.1 = { trigger = { i_trig = yes } }\n", "i_trig = yes", 0, []string{"events/i.txt"}},
		{"a dotted event, on either half", "events/e.txt", "namespace = m\nm.1 = {\n\timmediate = { trigger_event = v.2 }\n}\n", "v.2", 2, []string{"events/v_events.txt"}},
		{"a bare culture name", "history/characters/m.txt", "1000 = {\n\tculture = norse\n}\n", "norse", 0, []string{"cultures/v_cultures.txt"}},
		{"the same name cited", "events/e.txt", "namespace = m\nm.1 = {\n\ttrigger = { exists = culture:norse }\n}\n", "norse", 0, []string{"cultures/v_cultures.txt"}},
		{"a nested title", "events/e.txt", "namespace = m\nm.1 = {\n\ttrigger = { exists = title:c_x }\n}\n", "c_x", 0, []string{"landed_titles/v_titles.txt"}},
		{"a character by number", "events/e.txt", "namespace = m\nm.1 = {\n\ttrigger = { exists = character:186004 }\n}\n", "186004", 0, []string{"history/characters/v_characters.txt"}},
		{"a script value", "events/e.txt", "namespace = m\nm.1 = {\n\timmediate = { add_gold = small_value }\n}\n", "small_value", 0, []string{"script_values/v_values.txt"}},
		{"a variable read goes to where it is set", "events/e.txt", "namespace = m\nm.1 = {\n\timmediate = { set_variable = { name = foo value = 1 } }\n\ttrigger = { exists = var:foo }\n}\n", "var:foo", 4, []string{"events/e.txt"}},
		{"a saved scope", "events/e.txt", "namespace = m\nm.1 = {\n\timmediate = { save_scope_as = mine }\n\ttrigger = { exists = scope:mine }\n}\n", "scope:mine", 6, []string{"events/e.txt"}},
		{"a parameter, from the call site, goes to the macro that was called", "events/e.txt", "namespace = m\nm.1 = {\n\timmediate = { v_effect = { AMOUNT = 1 } }\n}\n", "AMOUNT", 0, []string{"scripted_effects/v_effects.txt"}},
		{"a parameter inside its body", "common/scripted_effects/p.txt", "p_effect = {\n\tadd_gold = $AMOUNT$\n\tadd_prestige = $AMOUNT$\n}\n", "$AMOUNT$\n\tadd_prestige", 1, []string{"scripted_effects/p.txt"}},
		{"a loc key from script", "events/e.txt", "namespace = m\nm.1 = {\n\ttitle = v.1.t\n}\n", "v.1.t", 0, []string{"v_l_english.yml"}},
		{"a mod's twin: the winner first", "events/e.txt", "namespace = v\nv.1 = {\n\ttype = character_event\n}\n", "v.1 =", 0, []string{"events/e.txt", "events/v_events.txt"}},
	} {
		path := opened(s, dir, tt.rel, tt.text)
		line, col := aim(t, tt.text, tt.needle, tt.plus)
		got, err := Definition(s, path, line, col)
		var rels []string
		for _, l := range got {
			rels = append(rels, l.Rel)
		}
		if err != nil || len(got) != len(tt.files) || slices.ContainsFunc(tt.files, func(want string) bool {
			return !slices.ContainsFunc(rels, func(rel string) bool { return strings.HasSuffix(rel, want) })
		}) || (len(tt.files) > 1 && !strings.HasSuffix(rels[0], tt.files[0])) {
			t.Errorf("%s: %v (%v)", tt.what, rels, err)
		}
	}

	for _, tt := range []struct{ what, rel, text, needle string }{
		{"an ephemeral prefix stays ephemeral", "events/e.txt", "namespace = m\nm.1 = {\n\ttrigger = { exists = flag:some_flag }\n}\n", "some_flag"},
		{"a literal", "events/e.txt", "namespace = m\nm.1 = {\n\ttrigger = { is_adult = yes }\n}\n", "yes"},
		{"a token", "events/e.txt", "namespace = m\nm.1 = {\n\ttrigger = { is_adult = yes }\n}\n", "is_adult"},
	} {
		path := opened(s, dir, tt.rel, tt.text)
		line, col := aim(t, tt.text, tt.needle, 0)
		if got, _ := Definition(s, path, line, col); len(got) != 0 {
			t.Errorf("%s: %v", tt.what, got)
		}
	}
}

func TestReferences(t *testing.T) {
	s, dir := fixture(t, "ck3")
	call := "namespace = m\nm.1 = {\n\ttrigger = { m_trig = yes }\n\ttitle = m.1.t\n\tdesc = only_script\n}\n"
	opened(s, dir, "events/e.txt", call)
	decl := "m_trig = {\n\thas_trait = brave\n}\n"
	trig := opened(s, dir, "common/scripted_triggers/x.txt", decl)
	locText := "l_english:\n m.1.t:0 \"Hi $only_loc|U$\"\n only_loc:0 \"There\"\n"
	locPath := opened(s, dir, "localization/english/m_l_english.yml", locText)

	rels := func(path, text, needle string, plus int) []string {
		line, col := aim(t, text, needle, plus)
		got, err := References(s, path, line, col)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, l := range got {
			out = append(out, l.Rel)
		}
		return out
	}
	has := func(got []string, want string) bool {
		return slices.ContainsFunc(got, func(rel string) bool { return strings.HasSuffix(rel, want) })
	}

	if got := rels(trig, decl, "m_trig", 0); !has(got, "events/e.txt") || !has(got, "scripted_triggers/x.txt") {
		t.Errorf("from the declaration, the call site and the declaration: %v", got)
	}
	if got := rels(locPath, locText, "m.1.t", 0); !has(got, "events/e.txt") || !has(got, "m_l_english.yml") {
		t.Errorf("a loc key: its script site and its own line: %v", got)
	}
	if got := rels(locPath, locText, "only_loc|U", 0); !has(got, "m_l_english.yml") || len(got) != 2 {
		t.Errorf("an interpolated key: its interpolation and its own line: %v", got)
	}
	if got := rels(locPath, locText, "There", 0); len(got) != 0 {
		t.Errorf("a value is no key: %v", got)
	}
	if got := rels(trig, decl, "brave", 0); !has(got, "events/v_events.txt") || !has(got, "traits/v_traits.txt") {
		t.Errorf("an install definition's references reach into the install's files: %v", got)
	}
	got, _ := References(s, trig, 0, 0)
	if len(slices.Compact(slices.Clone(got))) != len(got) {
		t.Errorf("no site twice: %v", got)
	}
}

func TestSymbols(t *testing.T) {
	s, dir := fixture(t, "ck3")
	text := "namespace = m\nscripted_trigger m_inline = { is_adult = yes }\nm.1 = {\n\timmediate = { save_scope_as = mine }\n}\nm.2 = { }\n"
	path := opened(s, dir, "events/e.txt", text)
	got, err := DocumentSymbols(s, path)
	var names []string
	for _, sym := range got {
		names = append(names, sym.Kind+":"+sym.Name)
	}
	if err != nil || !slices.Equal(names, []string{"scripted_triggers:m_inline", "events:m.1", "saved_scopes:mine", "events:m.2"}) {
		t.Errorf("a file's definitions in the file's order: %v (%v)", names, err)
	}
	if got[1].Location.Range.Start.Line != 2 {
		t.Errorf("each at its line: %+v", got[1].Location)
	}

	found := WorkspaceSymbols(s, "M.")
	if len(found) != 2 || found[0].Location.Origin != "mod" {
		t.Errorf("the workspace's definitions by a case-blind query: %+v", found)
	}
	if WorkspaceSymbols(s, "") != nil {
		t.Error("an empty query asks for nothing")
	}
}
