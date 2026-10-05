// lsp_test.go covers the requests over a session on the ck3 fixture install, with a mod the tests
// write files into: completion (key and value positions, ranking, cap), definition, references and
// symbols, the diagnostic codes with their guards and suppression, formatting, folding,
// quick-fixes, era hints, hover misses, signature help, no panic at any position, and the gate
// over the real installs.

package lsp

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/index"
	"paradox-modding-tools/services/internal/lang/source"
	"paradox-modding-tools/services/internal/session"
)

var installs sync.Map

// fixture is a session over one fixture install, scanned once, with an empty mod folder per test.
func fixture(t *testing.T, id string) (*session.Session, string) {
	t.Helper()
	scan, _ := installs.LoadOrStore(id, sync.OnceValues(func() (*index.Install, error) {
		dir := filepath.Join("..", "lang", "testdata", id)
		return index.Scan(t.Context(), index.Request{Game: id, Dir: dir, Version: "test"})
	}))
	install, err := scan.(func() (*index.Install, error))()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "common"), 0o755); err != nil {
		t.Fatal(err)
	}
	sess, err := session.New(t.Context(), game.Get(id), install, []session.Mod{{ID: "mod", Dir: dir, Calendar: session.Calendar{{Name: "AC", Offset: 7782}}}}, "")
	if err != nil {
		t.Fatal(err)
	}
	return sess, dir
}

// opened puts a file of the mod in the editor with a text, and gives its path.
func opened(sess *session.Session, dir, rel, text string) string {
	path := filepath.Join(dir, filepath.FromSlash(rel))
	sess.DidChange(path, text)
	return path
}

// aim is the line and column of a needle in a text, plus some columns in.
func aim(t *testing.T, text, needle string, plus int) (int, int) {
	t.Helper()
	at := strings.Index(text, needle)
	if at < 0 {
		t.Fatalf("no %q in the text", needle)
	}
	at += plus
	line := strings.Count(text[:at], "\n")
	return line, at - (strings.LastIndex(text[:at], "\n") + 1)
}

func TestComplete(t *testing.T) {
	install, dir := fixture(t, "ck3")
	opened(install, dir, "common/scripted_effects/m.txt", "v_m_effect = {\n\tadd_gold = 1\n}\n")
	labels := func(items []Item) []string {
		var out []string
		for _, item := range items {
			out = append(out, item.Label)
		}
		return out
	}
	rank := func(items []Item, label string) int {
		if at := slices.IndexFunc(items, func(item Item) bool { return item.Label == label }); at >= 0 {
			return items[at].Rank
		}
		return -1
	}

	for _, tt := range []struct {
		what, rel, text string
		has, hasNot     []string
		check           func(items []Item) bool
	}{
		{"a trigger body offers triggers, not effects", "events/x.txt", "namespace = m\nm.1 = {\n\ttrigger = {\n\t\tadd_", []string{"add_to_temporary_list"}, []string{"add_gold"}, nil},
		{"an effect body offers effects, those in its scope first", "events/x.txt", "namespace = m\nm.1 = {\n\timmediate = {\n\t\tevery_vassal = {\n\t\t\tadd_t", []string{"add_trait", "add_tenet"}, nil,
			func(items []Item) bool { return rank(items, "add_trait") == 0 && rank(items, "add_tenet") == 2 }},
		{"a structure offers its members, from every position the key is written at", "events/x.txt", "namespace = m\nm.1 = {\n\tcooldown = {\n\t\t", []string{"days", "months"}, nil, nil},
		{"the top of a file offers the kind's own names", "events/x.txt", "namespace = m\nm.1 = { }\nm.2 = { }\nm.", []string{"m.1", "m.2"}, nil, nil},
		{"a value position offers definitions of the kind the field accepts", "events/x.txt", "namespace = m\nm.1 = {\n\ttheme = s", []string{"seduction"}, []string{"brave"}, nil},
		{"a loc field offers loc keys, not definitions", "events/x.txt", "namespace = m\nm.1 = {\n\ttitle = v.1", []string{"v.1.t"}, []string{"v.1"}, nil},
		{"a body offers the macros of its class beside its tokens", "events/x.txt", "namespace = m\nm.1 = {\n\timmediate = {\n\t\tv_", []string{"v_effect", "v_m_effect"}, []string{"v_trigger"}, nil},
		{"a name token offers the names the workspace makes", "events/x.txt", "namespace = m\nm.1 = {\n\timmediate = {\n\t\tadd_to_variable_list = { name = mine target = root }\n\t\tevery_in_list = { variable = ", []string{"mine", "seen_list"}, nil, nil},
		{"a block nothing classifies offers what the body around it offers", "common/on_action/x.txt", "on_birth = {\n\teffect = {\n\t\tweird = {\n\t\t\t", []string{"add_gold"}, nil, nil},
		{"a cite offers the kind's names spelled whole, over the whole cite", "events/x.txt", "namespace = m\nm.1 = {\n\ttrigger = {\n\t\thas_trait = trait:br", []string{"trait:brave"}, nil,
			func(items []Item) bool {
				at := slices.IndexFunc(items, func(item Item) bool { return item.Label == "trait:brave" })
				return at >= 0 && items[at].Range.Start.Character == 14 && items[at].Range.End.Character == 22
			}},
		{"a list offers what its field accepts", "common/on_action/m.txt", "on_birth = {\n\tevents = { v.", []string{"v.1"}, nil, nil},
		{"a bool", "events/x.txt", "namespace = m\nm.1 = {\n\ttrigger = {\n\t\tis_adult = ", []string{"yes", "no"}, nil, nil},
	} {
		path := opened(install, dir, tt.rel, tt.text)
		line := strings.Count(tt.text, "\n")
		col := len(tt.text) - strings.LastIndex(tt.text, "\n") - 1
		got, err := Complete(install, path, line, col)
		if err != nil {
			t.Fatal(err)
		}
		names := labels(got.Items)
		for _, want := range tt.has {
			if !slices.Contains(names, want) {
				t.Errorf("%s: no %q in %v", tt.what, want, names)
			}
		}
		for _, not := range tt.hasNot {
			if slices.Contains(names, not) {
				t.Errorf("%s: %q offered", tt.what, not)
			}
		}
		if tt.check != nil && !tt.check(got.Items) {
			t.Errorf("%s: %+v", tt.what, got.Items)
		}
	}

	// the cap cuts and marks the list
	var many strings.Builder
	for i := range 100 {
		fmt.Fprintf(&many, "eff_%03d = { add_gold = 1 }\n", i)
	}
	opened(install, dir, "common/scripted_effects/many.txt", many.String())
	text := "namespace = m\nm.1 = {\n\timmediate = {\n\t\t"
	path := opened(install, dir, "events/x.txt", text)
	got, _ := Complete(install, path, 3, 2)
	if len(got.Items) != maxComplete || !got.Incomplete {
		t.Errorf("the cap: %d items, incomplete %v", len(got.Items), got.Incomplete)
	}
}

func TestDefinition(t *testing.T) {
	install, dir := fixture(t, "ck3")
	opened(install, dir, "common/scripted_triggers/x.txt", "m_trig = {\n\thas_trait = brave\n\tis_adult = yes\n}\n")
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
		{"a mod's twin loaded last: the winner first", "events/zz_e.txt", "namespace = v\nv.1 = {\n\ttype = character_event\n}\n", "v.1 =", 0, []string{"events/zz_e.txt", "events/v_events.txt"}},
	} {
		path := opened(install, dir, tt.rel, tt.text)
		line, col := aim(t, tt.text, tt.needle, tt.plus)
		got, err := Definition(install, path, line, col)
		var rels []string
		for _, location := range got {
			rels = append(rels, location.Rel)
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
		path := opened(install, dir, tt.rel, tt.text)
		line, col := aim(t, tt.text, tt.needle, 0)
		if got, _ := Definition(install, path, line, col); len(got) != 0 {
			t.Errorf("%s: %v", tt.what, got)
		}
	}
}

func TestReferences(t *testing.T) {
	install, dir := fixture(t, "ck3")
	call := "namespace = m\nm.1 = {\n\ttrigger = { m_trig = yes }\n\ttitle = m.1.t\n\tdesc = only_script\n}\n"
	opened(install, dir, "events/e.txt", call)
	decl := "m_trig = {\n\thas_trait = brave\n}\n"
	trig := opened(install, dir, "common/scripted_triggers/x.txt", decl)
	locText := "l_english:\n m.1.t:0 \"Hi $only_loc|U$\"\n only_loc:0 \"There\"\n"
	locPath := opened(install, dir, "localization/english/m_l_english.yml", locText)

	rels := func(path, text, needle string, plus int) []string {
		line, col := aim(t, text, needle, plus)
		got, err := References(install, path, line, col)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, location := range got {
			out = append(out, location.Rel)
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
	got, _ := References(install, trig, 0, 0)
	if len(slices.Compact(slices.Clone(got))) != len(got) {
		t.Errorf("no site twice: %v", got)
	}
}

func TestSymbols(t *testing.T) {
	install, dir := fixture(t, "ck3")
	text := "namespace = m\nscripted_trigger m_inline = { is_adult = yes }\nm.1 = {\n\timmediate = { save_scope_as = mine }\n}\nm.2 = { }\n"
	path := opened(install, dir, "events/e.txt", text)
	got, err := DocumentSymbols(install, path)
	var names []string
	for _, sym := range got {
		names = append(names, sym.Database+":"+sym.Name)
	}
	if err != nil || !slices.Equal(names, []string{"scripted_triggers:m_inline", "events:m.1", "scope:mine", "events:m.2"}) {
		t.Errorf("a file's definitions in the file's order: %v (%v)", names, err)
	}
	if got[1].Location.Range.Start.Line != 2 {
		t.Errorf("each at its line: %+v", got[1].Location)
	}

	found := WorkspaceSymbols(install, "M.")
	if len(found) != 2 || found[0].Location.Origin != "mod" {
		t.Errorf("the workspace's definitions by a case-blind query: %+v", found)
	}
	if WorkspaceSymbols(install, "") != nil {
		t.Error("an empty query asks for nothing")
	}
}

func TestDiagnose(t *testing.T) {
	install, dir := fixture(t, "ck3")
	codes := func(path string, enabled map[string]bool) []string {
		got, err := Diagnose(install, path, enabled)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, diagnostic := range got {
			out = append(out, diagnostic.Code)
		}
		slices.Sort(out)
		return out
	}

	for _, tt := range []struct {
		what, rel, text string
		want            []string
	}{
		{"a clean event", "events/x.txt", "namespace = m\nm.1 = {\n\ttype = character_event\n\ttrigger = { is_adult = yes }\n}\n", nil},
		{"the parser's errors", "events/x.txt", "namespace = m\nm.1 = {\n\ttype =\n}\n}\n", []string{"missing-value", "stray-close"}},
		{"an unclosed brace and an unterminated string", "events/x.txt", "namespace = m\nm.1 = {\n\ttitle = \"open\n", []string{"unclosed-brace", "unterminated-string"}},
		{"a declared cite that resolves", "events/x.txt", "namespace = m\nm.1 = {\n\ttrigger = { exists = title:k_x }\n}\n", nil},
		{"a declared prefix bound to no kind", "events/x.txt", "namespace = m\nm.1 = {\n\ttrigger = { exists = flag:whatever }\n}\n", nil},
		{"a bare word nothing declares is never accused", "events/x.txt", "namespace = m\nm.1 = {\n\ttheme = no_such_theme\n}\n", nil},
		{"a value of another type than the game declares", "events/x.txt", "namespace = m\nm.1 = {\n\tcooldown = { days = soon }\n}\n", []string{"wrong-value-type"}},
		{"a value of the declared type", "events/x.txt", "namespace = m\nm.1 = {\n\tcooldown = { days = 10 }\n}\n", nil},
		{"a key a learned structure never holds is not judged", "events/x.txt", "namespace = m\nm.1 = {\n\tcooldown = { fortnights = 2 }\n}\n", nil},
		{"a declared token, a macro, a scope path and a weight in a body", "events/x.txt", "namespace = m\nm.1 = {\n\timmediate = {\n\t\tadd_gold = 1\n\t\tv_effect = { AMOUNT = 1 }\n\t\tcapital_county = { add_title_law = x }\n\t\trandom_list = { 10 = { add_gold = 1 } }\n\t}\n}\n", nil},
		{"a call's argument the macro never spells is not judged: the game writes such calls", "events/x.txt", "namespace = m\nm.1 = {\n\timmediate = { v_effect = { AMOUNTS = 1 } }\n}\n", nil},
		{"silent in a kind only the mod has", "common/mod_things/m.txt", "thing = {\n\twhatever = 1\n}\n", nil},
		{"a character effect in a title block", "events/x.txt", "namespace = m\nm.1 = {\n\timmediate = { capital_county = { add_gold = 1 } }\n}\n", []string{"wrong-scope"}},
		{"the same chain used correctly", "events/x.txt", "namespace = m\nm.1 = {\n\timmediate = { capital_county = { add_title_law = x } }\n}\n", nil},
		{"under a block nothing declares a scope for, silence", "events/x.txt", "namespace = m\nm.1 = {\n\timmediate = { weird = { add_gold = 1 } }\n}\n", nil},
		{"a suppressed finding, by the comment above", "events/x.txt", "namespace = m\nm.1 = {\n\timmediate = { capital_county = {\n\t\t# pmt:ignore wrong-scope\n\t\tadd_gold = 1\n\t} }\n}\n", nil},
		{"a suppressed finding, on the line", "events/x.txt", "namespace = m\nm.1 = {\n\timmediate = { capital_county = { add_gold = 1 } } # pmt:ignore\n}\n", nil},
		{"a comment that suppresses another code leaves this one", "events/x.txt", "namespace = m\nm.1 = {\n\timmediate = { capital_county = {\n\t\t# pmt:ignore unresolved-ref\n\t\tadd_gold = 1\n\t} }\n}\n", []string{"wrong-scope"}},
		{"a path that exists nowhere is off by default", "gui/m.gui", "types m = {\n\ttype m_icon = icon {\n\t\ttexture = \"gfx/nowhere/absent.dds\"\n\t}\n}\n", nil},
		{"a clean loc file", "localization/english/m_l_english.yml", "\xEF\xBB\xBFl_english:\n m.1.t:0 \"Hi\"\n", nil},
		{"no header", "localization/english/m_l_english.yml", "\xEF\xBB\xBF m.1.t:0 \"Hi\"\n", []string{"loc-content-before-header", "loc-no-header"}},
		{"content before the header", "localization/english/m_l_english.yml", "\xEF\xBB\xBF m.1.t:0 \"Hi\"\nl_english:\n", []string{"loc-content-before-header"}},
		{"a tab indent", "localization/english/m_l_english.yml", "\xEF\xBB\xBFl_english:\n\tm.1.t:0 \"Hi\"\n", []string{"loc-tab-indent"}},
		{"an unterminated value", "localization/english/m_l_english.yml", "\xEF\xBB\xBFl_english:\n m.1.t:0 \"Hi\n", []string{"loc-unterminated-value"}},
		{"a bad entry", "localization/english/m_l_english.yml", "\xEF\xBB\xBFl_english:\n m.1.t \"Hi\"\n", []string{"loc-bad-entry"}},
		{"a header for another language", "localization/english/m_l_english.yml", "\xEF\xBB\xBFl_french:\n m.1.t:0 \"Hi\"\n", []string{"loc-header-mismatch"}},
		{"the folder spelled localisation", "localisation/english/m_l_english.yml", "\xEF\xBB\xBFl_english:\n m.1.t:0 \"Hi\"\n", []string{"wrong-localization-folder"}},
		{"no byte order mark anywhere", "localization/english/n_l_english.yml", "l_english:\n m.1.t:0 \"Hi\"\n", []string{"missing-bom"}},
	} {
		path := opened(install, dir, tt.rel, tt.text)
		if got := codes(path, nil); !slices.Equal(got, tt.want) {
			t.Errorf("%s: %v, want %v", tt.what, got, tt.want)
		}
	}

	// a BOM on disk that the editor's buffer dropped is no missing BOM
	locPath := filepath.Join(dir, "localization", "english", "d_l_english.yml")
	if err := os.MkdirAll(filepath.Dir(locPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(locPath, []byte("\xEF\xBB\xBFl_english:\n d:0 \"Hi\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	install.DidChange(locPath, "l_english:\n d:0 \"Hi\"\n")
	if got, _ := Diagnose(install, locPath, nil); len(got) != 0 {
		t.Errorf("a BOM on disk: %+v", got)
	}

	// the settings turn a code off and on
	path := opened(install, dir, "events/x.txt", "namespace = m\nm.1 = {\n\timmediate = { capital_county = { add_gold = 1 } }\n}\n")
	if got := codes(path, map[string]bool{"wrong-scope": false}); len(got) != 0 {
		t.Errorf("a code turned off: %v", got)
	}
	icon := opened(install, dir, "common/traits/m_icons.txt", "m_iconic = {\n\ticon = \"gfx/nowhere/absent.dds\"\n}\n")
	if got := codes(icon, map[string]bool{"missing-file": true}); !slices.Equal(got, []string{"missing-file"}) {
		t.Errorf("missing-file turned on: %v", got)
	}

	// a mod's override of the install's definition is no duplicate
	if got := codes(opened(install, dir, "common/traits/m.txt", "brave = { name = x }\n"), nil); slices.Contains(got, "duplicate-definition") {
		t.Errorf("a mod's override of the install's definition is no duplicate: %v", got)
	}

	// the install's own files are silent, and never indexed from the editor
	for _, file := range install.Install.Index.Files {
		if strings.HasSuffix(file.Rel, "v_events.txt") {
			install.DidChange(file.Path, "namespace = v\nv.1 = {\n\timmediate = { capital_county = { add_gold = 1 } }\n}\n")
			if got := codes(file.Path, nil); len(got) != 0 {
				t.Errorf("an install file: %v", got)
			}
			if len(index.Lookup(install.Indexes(), "events", "v.2")) != 1 || len(index.Lookup([]*index.Index{install.Workspace}, "events", "v.1")) != 0 {
				t.Error("opening an install file indexes nothing")
			}
			install.DidClose(file.Path)
		}
	}
}

// TestInstallsDiagnoseClean is the gate over the real installs: the game's own script, judged as a
// mod's, must be clean, because every rule accuses only on what the game declares. A fixed sample
// of each game's files (the first 400 under common and the first 250 events files, in name order)
// is copied into a temporary mod and diagnosed with the default codes on. A finding is a wrong
// rule, not a wrong game; parser errors are counted apart. It runs only with PMT_VANILLA_GATE=1 and
// the three PMT_TEST_INSTALL_ variables.
func TestInstallsDiagnoseClean(t *testing.T) {
	if testing.Short() || os.Getenv("PMT_VANILLA_GATE") == "" {
		t.Skip("diagnoses the three installs; set PMT_VANILLA_GATE=1")
	}
	on := map[string]bool{}
	for _, code := range Codes {
		on[code.Code] = code.Default
	}
	// a ceiling is findings traced to the game's own files, not a filter: one more fails
	ceilings := map[string]int{
		"ck3/unresolved-ref":        1,  // coronation.txt: province:b_constantinople is a barony, read as its province
		"ck3/wrong-scope":           1,  // legend_spread_events_8.txt: argument category is also an artifact trigger
		"vic3/duplicate-definition": 39, // coat_of_arms/01_subs.txt defines coats of arms twice; the game keeps the last
		"eu5/unresolved-ref":        218,
	}
	for _, testCase := range []struct{ id, env string }{{"ck3", "PMT_TEST_INSTALL_CK3"}, {"vic3", "PMT_TEST_INSTALL_VIC3"}, {"eu5", "PMT_TEST_INSTALL_EU5"}} {
		dir := os.Getenv(testCase.env)
		if dir == "" {
			t.Errorf("%s: %s is not set", testCase.id, testCase.env)
			continue
		}
		install, err := index.Scan(t.Context(), index.Request{Game: testCase.id, Dir: dir, Version: "gate"})
		if err != nil {
			t.Fatalf("%s: %v", testCase.id, err)
		}

		// the sample, copied into a mod
		var common, events []source.File
		for _, file := range install.Index.Files {
			switch {
			case file.Format != source.Script || !strings.HasSuffix(file.Rel, ".txt"):
			case strings.HasPrefix(file.Rel, "events/") || strings.Contains(file.Rel, "/events/"):
				events = append(events, file)
			case strings.Contains(file.Rel, "common/"):
				common = append(common, file)
			}
		}
		slices.SortFunc(common, func(first, second source.File) int { return strings.Compare(first.Rel, second.Rel) })
		slices.SortFunc(events, func(first, second source.File) int { return strings.Compare(first.Rel, second.Rel) })
		samples := append(common[:min(400, len(common))], events[:min(250, len(events))]...)
		mod := t.TempDir()
		for _, sample := range samples {
			raw, err := os.ReadFile(sample.Path)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(mod, filepath.FromSlash(sample.Rel))
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, raw, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		sess, err := session.New(t.Context(), game.Get(testCase.id), install, []session.Mod{{ID: "sample", Dir: mod}}, "")
		if err != nil {
			t.Fatal(err)
		}

		// every file judged; a finding is shown with its first site
		counts, first := map[string]int{}, map[string]string{}
		parse := 0
		var lines []string
		for _, sample := range samples {
			path := filepath.Join(mod, filepath.FromSlash(sample.Rel))
			found, err := Diagnose(sess, path, on)
			if err != nil {
				t.Fatal(err)
			}
			for _, finding := range found {
				if !on[finding.Code] && !slices.ContainsFunc(Codes, func(known Code) bool { return known.Code == finding.Code }) {
					parse++
					continue
				}
				counts[finding.Code]++
				site := fmt.Sprintf("%s:%d", sample.Rel, finding.Range.Start.Line+1)
				lines = append(lines, finding.Code+"\t"+site+"\t"+finding.Message)
				if _, seen := first[finding.Code]; !seen {
					first[finding.Code] = site + " " + finding.Message
				}
			}
		}
		gateOut(t, testCase.id+".findings.txt", lines)
		t.Logf("DIAGNOSE %s: %d files, %d parse errors, findings %v", testCase.id, len(samples), parse, counts)
		for code, count := range counts {
			if count > ceilings[testCase.id+"/"+code] {
				t.Errorf("%s: %s fires %d times on the game's own files; first at %s", testCase.id, code, count, first[code])
			}
		}
	}
}

// gateOut writes a gate's lines, sorted, to $PMT_GATE_OUT/name, so two runs can be compared.
func gateOut(t *testing.T, name string, lines []string) {
	t.Helper()
	dir := os.Getenv("PMT_GATE_OUT")
	if dir == "" {
		return
	}
	slices.Sort(lines)
	if err := os.WriteFile(filepath.Join(dir, name), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Error(err)
	}
}

func TestFormat(t *testing.T) {
	install, dir := fixture(t, "ck3")
	path := opened(install, dir, "events/x.txt", "a = {\n b = 1\n\t\tc = {\n\td = 1\n\t}\n}\n")
	edits, err := Format(install, path)
	if err != nil || len(edits) == 0 || slices.ContainsFunc(edits, func(edit TextEdit) bool { return strings.HasPrefix(edit.NewText, " ") }) {
		t.Errorf("mixed indentation is fixed with tabs: %+v (%v)", edits, err)
	}
	if edits, _ := Format(install, opened(install, dir, "events/y.txt", "a = {\r\n\tb = 1\r\n}\r\n")); len(edits) != 0 {
		t.Errorf("a file indented right, with its own line endings, needs nothing: %+v", edits)
	}
	if edits, _ := Format(install, opened(install, dir, "localization/english/a_l_english.yml", "l_english:\n\tk:0 \"v\"\n")); len(edits) != 1 || edits[0].NewText != " " {
		t.Errorf("localization indents with one space: %+v", edits)
	}
	if edits, _ := Format(install, opened(install, dir, "descriptor.mod", "name = \"M\"\n")); edits != nil {
		t.Errorf("a descriptor is not formatted: %+v", edits)
	}
}

func TestFolding(t *testing.T) {
	install, dir := fixture(t, "ck3")
	got, err := Folding(install, opened(install, dir, "events/x.txt", "a = {\n\tb = {\n\t\tc = 1\n\t}\n}\nd = 1\n"))
	if err != nil || !slices.Equal(got, []FoldingRange{{0, 4}, {1, 3}}) {
		t.Errorf("one range per block: %+v (%v)", got, err)
	}
	if got, _ := Folding(install, opened(install, dir, "descriptor.mod", "tags = {\n\t\"x\"\n}\n")); got != nil {
		t.Errorf("a descriptor does not fold: %+v", got)
	}
}

func TestCodeActions(t *testing.T) {
	install, dir := fixture(t, "ck3")
	locPath := filepath.Join(dir, "localization", "english", "a_l_english.yml")
	if err := os.MkdirAll(filepath.Dir(locPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(locPath, []byte("l_english:\n k:0 \"v\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	install.Changed(locPath)
	install.DidChange(locPath, "l_english:\n k:0 \"v\"\n")
	actions, _ := CodeActions(install, locPath, 0, 0)
	if len(actions) != 1 || actions[0].Title != "Add UTF-8 BOM" || actions[0].Edit.Changes[locPath][0].NewText != "\xEF\xBB\xBF" {
		t.Errorf("a loc file with no BOM: %+v", actions)
	}

	text := "namespace = m\nm.1 = {\n\ttitle = missing_key\n}\n"
	path := opened(install, dir, "events/x.txt", text)
	line, col := aim(t, text, "missing_key", 0)
	actions, _ = CodeActions(install, path, line, col)
	if len(actions) != 1 || actions[0].Title != `Create localization key "missing_key"` {
		t.Fatalf("a missing loc key offers to create it: %+v", actions)
	}
	if edits, ok := actions[0].Edit.Changes[locPath]; !ok || len(edits) != 1 || edits[0].Range.Start.Line != 2 || edits[0].NewText != " missing_key:0 \"\"\n" {
		t.Errorf("the key goes at the end of the mod's existing loc file: %+v", actions[0].Edit)
	}
	text = "namespace = m\nm.1 = {\n\ttitle = k\n}\n"
	path = opened(install, dir, "events/x.txt", text)
	line, col = aim(t, text, "k\n", 0)
	if actions, _ := CodeActions(install, path, line, col); len(actions) != 0 {
		t.Errorf("a key already defined offers nothing: %+v", actions)
	}
}

func TestInlayHints(t *testing.T) {
	install, dir := fixture(t, "ck3")
	text := "namespace = m\nm.1 = {\n\tstart = 8082.1.1\n\tcount = 5\n\tname = x\n}\n"
	got, err := InlayHints(install, opened(install, dir, "events/x.txt", text))
	if err != nil || len(got) != 1 || strings.TrimSpace(got[0].Label) != "300.1.1 AC" || got[0].Position.Line != 2 {
		t.Errorf("only a date earns a hint, in the mod's era: %+v (%v)", got, err)
	}
	got, _ = InlayHints(install, opened(install, dir, "history/characters/m.txt", "1000 = {\n\t8082.1.1 = {\n\t\tbirth = yes\n\t}\n\t100.1.1 = { }\n}\n"))
	if len(got) != 1 || strings.Contains(got[0].Label, "-") {
		t.Errorf("a date as a key earns a hint; a year the offset pushes below one earns none: %+v", got)
	}
	install.Mods[0].Calendar = nil
	if got, _ := InlayHints(install, opened(install, dir, "events/y.txt", text)); len(got) != 0 {
		t.Errorf("silent without a calendar: %+v", got)
	}
}

func TestNoPanicAnywhere(t *testing.T) {
	install, dir := fixture(t, "ck3")
	for _, tt := range []struct{ rel, text string }{
		{"events/x.txt", "namespace = m\nm.1 = {\n\ttrigger = { has_trait = trait:br\n\timmediate = { v_effect = { AMOUNT = $X$ } }\n"},
		{"localization/english/m_l_english.yml", "l_english:\n k:0 \"$v.1.t$ and $VALUE|=+0$\n"},
		{"events/empty.txt", ""},
		{"common/traits/m.txt", "= {\n}}}\n\"\n"},
	} {
		path := opened(install, dir, tt.rel, tt.text)
		for line := range 4 {
			for col := range 24 {
				Hover(install, path, line, col)
				Complete(install, path, line, col)
				Definition(install, path, line, col)
				References(install, path, line, col)
				SignatureHelp(install, path, line, col)
				CodeActions(install, path, line, col)
			}
		}
		Diagnose(install, path, nil)
		Format(install, path)
		Folding(install, path)
		InlayHints(install, path)
		DocumentSymbols(install, path)
	}
}

func TestHover(t *testing.T) {
	install, dir := fixture(t, "ck3")
	locText := "l_english:\n m.1.t:0 \"Cost: $VALUE|=+0$ and $v.1.t$\"\n"
	for _, tt := range []struct{ what, rel, text, needle string }{
		{"a literal", "events/x.txt", "namespace = m\nm.1 = {\n\ttrigger = {\n\t\tis_adult = yes\n\t}\n}\n", "yes"},
		{"a comment", "events/x.txt", "# event should not hover\nnamespace = m\nm.1 = { }\n", "event should"},
		{"a number that is no branch of a body", "common/traits/m.txt", "bold = {\n\tgenes = {\n\t\t20 = empty\n\t\t80 = empty\n\t}\n}\n", "20 = empty"},
		{"a brace", "events/x.txt", "namespace = m\nm.1 = {\n}\n", "{"},
		{"the language header is no key", "localization/english/m_l_english.yml", locText, "english"},
	} {
		path := opened(install, dir, tt.rel, tt.text)
		line, col := aim(t, tt.text, tt.needle, 0)
		if card, _ := Hover(install, path, line, col); card != nil {
			t.Errorf("%s: a card: %+v", tt.what, *card)
		}
	}
}

func TestSignatureHelp(t *testing.T) {
	install, dir := fixture(t, "ck3")
	text := "namespace = m\nm.1 = {\n\timmediate = {\n\t\ttrigger_event = { id = v.2 }\n\t\thidden_effect = { add_gold = 1 }\n\t}\n}\n"
	path := opened(install, dir, "events/x.txt", text)
	line, col := aim(t, text, "id = v.2", 0)
	if help, _ := SignatureHelp(install, path, line, col); help == nil || !strings.HasPrefix(help.Label, "trigger_event = {") ||
		!slices.Contains(help.Parameters, "id") || !slices.Contains(help.Parameters, "days") {
		t.Errorf("inside a declared token's block, its usage: %+v", help)
	}
	line, col = aim(t, text, "add_gold = 1", 0)
	if help, _ := SignatureHelp(install, path, line, col); help != nil {
		t.Errorf("a token with no usage: %+v", help)
	}
	nested := "namespace = m\nm.1 = {\n\timmediate = {\n\t\ttrigger_event = { id = v.2 scope:mine = { v_effect = yes } }\n\t}\n}\n"
	path = opened(install, dir, "events/y.txt", nested)
	line, col = aim(t, nested, "v_effect", 0)
	if help, _ := SignatureHelp(install, path, line, col); help != nil {
		t.Errorf("a scope block inside a token's block is no argument of it: %+v", help)
	}
}
