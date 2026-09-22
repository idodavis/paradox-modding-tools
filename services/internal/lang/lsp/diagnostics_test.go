// diagnostics_test.go covers every code, the guards that keep a rule quiet (§6a), suppression by comment and by settings, the install's silence, and the gate over the real installs.
package lsp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/index"
	"paradox-modding-tools/services/internal/lang/session"
	"paradox-modding-tools/services/internal/source"
)

func TestDiagnose(t *testing.T) {
	s, dir := fixture(t, "ck3")
	codes := func(path string, enabled map[string]bool) []string {
		got, err := Diagnose(s, path, enabled)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, d := range got {
			out = append(out, d.Code)
		}
		slices.Sort(out)
		return slices.Compact(out)
	}

	for _, tt := range []struct {
		what, rel, text string
		want            []string
	}{
		{"a clean event", "events/x.txt", "namespace = m\nm.1 = {\n\ttype = character_event\n\ttrigger = { is_adult = yes }\n}\n", nil},
		{"the parser's errors", "events/x.txt", "namespace = m\nm.1 = {\n\ttype =\n}\n}\n", []string{"missing-value", "stray-close"}},
		{"an unclosed brace and an unterminated string", "events/x.txt", "namespace = m\nm.1 = {\n\ttitle = \"open\n", []string{"unclosed-brace", "unterminated-string"}},
		{"a declared cite nothing defines", "events/x.txt", "namespace = m\nm.1 = {\n\ttrigger = { exists = title:k_nowhere }\n}\n", []string{"unresolved-ref"}},
		{"a declared cite that resolves", "events/x.txt", "namespace = m\nm.1 = {\n\ttrigger = { exists = title:k_x }\n}\n", nil},
		{"a numeric cite nothing defines", "events/x.txt", "namespace = m\nm.1 = {\n\ttrigger = { exists = character:999999 }\n}\n", []string{"unresolved-ref"}},
		{"a declared prefix bound to no kind", "events/x.txt", "namespace = m\nm.1 = {\n\ttrigger = { exists = flag:whatever }\n}\n", nil},
		{"a script-made kind is never accused", "events/x.txt", "namespace = m\nm.1 = {\n\ttrigger = { exists = scope:nobody }\n}\n", nil},
		{"a bare word nothing declares is never accused", "events/x.txt", "namespace = m\nm.1 = {\n\ttheme = no_such_theme\n}\n", nil},
		{"a value of another type than the game declares", "events/x.txt", "namespace = m\nm.1 = {\n\tcooldown = { days = soon }\n}\n", []string{"wrong-value-type"}},
		{"a value of the declared type", "events/x.txt", "namespace = m\nm.1 = {\n\tcooldown = { days = 10 }\n}\n", nil},
		{"a key a learned structure never holds is not judged (§6a)", "events/x.txt", "namespace = m\nm.1 = {\n\tcooldown = { fortnights = 2 }\n}\n", nil},
		{"a declared token, a macro, a scope path and a weight in a body", "events/x.txt", "namespace = m\nm.1 = {\n\timmediate = {\n\t\tadd_gold = 1\n\t\tv_effect = { AMOUNT = 1 }\n\t\tcapital_county = { add_title_law = x }\n\t\trandom_list = { 10 = { add_gold = 1 } }\n\t}\n}\n", nil},
		{"a call's argument the macro never spells is not judged: the game writes such calls", "events/x.txt", "namespace = m\nm.1 = {\n\timmediate = { v_effect = { AMOUNTS = 1 } }\n}\n", nil},
		{"silent in a kind only the mod has", "common/mod_things/m.txt", "thing = {\n\twhatever = 1\n}\n", nil},
		{"a character effect in a title block", "events/x.txt", "namespace = m\nm.1 = {\n\timmediate = { capital_county = { add_gold = 1 } }\n}\n", []string{"wrong-scope"}},
		{"the same chain used correctly", "events/x.txt", "namespace = m\nm.1 = {\n\timmediate = { capital_county = { add_title_law = x } }\n}\n", nil},
		{"under a block nothing declares a scope for, silence", "events/x.txt", "namespace = m\nm.1 = {\n\timmediate = { weird = { add_gold = 1 } }\n}\n", nil},
		{"a definition without the loc key every vanilla one has", "common/traits/m.txt", "unnamed = {\n\tname = x\n}\n", []string{"required-loc"}},
		{"a definition with its key", "common/traits/m.txt", "brave = {\n\tname = x\n}\n", nil},
		{"a suppressed finding, by the comment above", "events/x.txt", "namespace = m\nm.1 = {\n\timmediate = { capital_county = {\n\t\t# pmt:ignore wrong-scope\n\t\tadd_gold = 1\n\t} }\n}\n", nil},
		{"a suppressed finding, on the line", "events/x.txt", "namespace = m\nm.1 = {\n\timmediate = { capital_county = { add_gold = 1 } } # pmt:ignore\n}\n", nil},
		{"a comment that suppresses another code leaves this one", "events/x.txt", "namespace = m\nm.1 = {\n\timmediate = { capital_county = {\n\t\t# pmt:ignore unresolved-ref\n\t\tadd_gold = 1\n\t} }\n}\n", []string{"wrong-scope"}},
		{"a path that exists nowhere is off by default", "gui/m.gui", "types m = {\n\ttype m_icon = icon {\n\t\ttexture = \"gfx/nowhere/absent.dds\"\n\t}\n}\n", nil},
	} {
		path := opened(s, dir, tt.rel, tt.text)
		if got := codes(path, nil); !slices.Equal(got, tt.want) {
			t.Errorf("%s: %v, want %v", tt.what, got, tt.want)
		}
	}

	// the settings turn a code off and on
	path := opened(s, dir, "events/x.txt", "namespace = m\nm.1 = {\n\timmediate = { capital_county = { add_gold = 1 } }\n}\n")
	if got := codes(path, map[string]bool{"wrong-scope": false}); len(got) != 0 {
		t.Errorf("a code turned off: %v", got)
	}
	gui := opened(s, dir, "gui/m.gui", "types m = {\n\ttype m_icon = icon {\n\t\ttexture = \"gfx/nowhere/absent.dds\"\n\t}\n}\n")
	if got := codes(gui, map[string]bool{"missing-file": true}); !slices.Equal(got, []string{"missing-file"}) {

		t.Errorf("missing-file turned on: %v", got)
	}

	// a name the workspace defines twice: the later definition is reported, the first is clean
	first := opened(s, dir, "common/traits/a.txt", "twice = { name = x }\n")
	second := opened(s, dir, "common/traits/b.txt", "twice = { name = x }\n")
	if got := codes(second, nil); !slices.Contains(got, "duplicate-definition") {
		t.Errorf("the second definition: %v", got)
	}
	if got := codes(first, nil); slices.Contains(got, "duplicate-definition") {
		t.Errorf("the first definition: %v", got)
	}
	if got := codes(opened(s, dir, "common/traits/m.txt", "brave = { name = x }\n"), nil); slices.Contains(got, "duplicate-definition") {
		t.Errorf("a mod's override of the install's definition is no duplicate: %v", got)
	}

	// the install's own files are silent, and never harvested from the editor
	for _, f := range s.Install.Index.Files {
		if strings.HasSuffix(f.Rel, "v_events.txt") {
			s.DidOpen(f.Path, "namespace = v\nv.1 = {\n\timmediate = { capital_county = { add_gold = 1 } }\n}\n")
			if got := codes(f.Path, nil); len(got) != 0 {
				t.Errorf("an install file: %v", got)
			}
			if len(s.Defs("v.2")) != 1 || s.Workspace.Def("events", "v.1") {
				t.Error("opening an install file harvests nothing")
			}
			s.DidClose(f.Path)
		}
	}
}

func TestDiagnoseLoc(t *testing.T) {
	s, dir := fixture(t, "ck3")
	codes := func(rel, text string) []string {
		got, err := Diagnose(s, opened(s, dir, rel, text), nil)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, d := range got {
			out = append(out, d.Code)
		}
		slices.Sort(out)
		return out
	}
	for _, tt := range []struct {
		what, rel, text string
		want            []string
	}{
		{"a clean file", "localization/english/m_l_english.yml", "\xEF\xBB\xBFl_english:\n m.1.t:0 \"Hi\"\n", nil},
		{"no header", "localization/english/m_l_english.yml", "\xEF\xBB\xBF m.1.t:0 \"Hi\"\n", []string{"loc-content-before-header", "loc-no-header"}},
		{"content before the header", "localization/english/m_l_english.yml", "\xEF\xBB\xBF m.1.t:0 \"Hi\"\nl_english:\n", []string{"loc-content-before-header"}},
		{"a tab indent", "localization/english/m_l_english.yml", "\xEF\xBB\xBFl_english:\n\tm.1.t:0 \"Hi\"\n", []string{"loc-tab-indent"}},
		{"an unterminated value", "localization/english/m_l_english.yml", "\xEF\xBB\xBFl_english:\n m.1.t:0 \"Hi\n", []string{"loc-unterminated-value"}},
		{"a bad entry", "localization/english/m_l_english.yml", "\xEF\xBB\xBFl_english:\n m.1.t \"Hi\"\n", []string{"loc-bad-entry"}},
		{"a header for another language", "localization/english/m_l_english.yml", "\xEF\xBB\xBFl_french:\n m.1.t:0 \"Hi\"\n", []string{"loc-header-mismatch"}},
		{"the folder spelled localisation", "localisation/english/m_l_english.yml", "\xEF\xBB\xBFl_english:\n m.1.t:0 \"Hi\"\n", []string{"wrong-localization-folder"}},
		{"no byte order mark anywhere", "localization/english/n_l_english.yml", "l_english:\n m.1.t:0 \"Hi\"\n", []string{"missing-bom"}},
	} {
		if got := codes(tt.rel, tt.text); !slices.Equal(got, tt.want) {
			t.Errorf("%s: %v, want %v", tt.what, got, tt.want)
		}
	}

	// a BOM on disk that the editor's buffer dropped is no missing BOM
	path := filepath.Join(dir, "localization", "english", "d_l_english.yml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("\xEF\xBB\xBFl_english:\n d:0 \"Hi\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.DidOpen(path, "l_english:\n d:0 \"Hi\"\n")
	if got, _ := Diagnose(s, path, nil); len(got) != 0 {
		t.Errorf("a BOM on disk: %+v", got)
	}
}

// TestInstallsDiagnoseClean is the gate over the real installs: the game's own script, judged as a mod's, must be clean, because every rule accuses only on what the game declares (§6a). Each game is scanned, a fixed sample of its files (the first 400 under common and the first 250 events files, in name order, so two runs see the same) is copied into a temporary mod over it, and every one is diagnosed with every code on. A finding is a rule that is wrong, not a game that is; a parser error is a parser bug, counted apart. It runs only when asked: PMT_VANILLA_GATE=1 and the three PMT_TEST_INSTALL_ variables.
func TestInstallsDiagnoseClean(t *testing.T) {
	if testing.Short() || os.Getenv("PMT_VANILLA_GATE") == "" {
		t.Skip("diagnoses the three installs; set PMT_VANILLA_GATE=1")
	}
	on := map[string]bool{}
	for _, c := range Codes {
		on[c.Code] = c.Default
	}
	// a ceiling is a finding traced to the game's own files, never a filter: one more than recorded fails
	ceilings := map[string]int{
		"ck3/unresolved-ref":        1,   // coronation.txt: province:b_constantinople, a barony under the province prefix, which the game maps to the barony's province
		"ck3/wrong-scope":           2,   // legend_spread_events_8.txt: any_trait_in_category takes an argument named category, and the dump declares category as an artifact trigger; the reader takes the trigger
		"vic3/duplicate-definition": 39,  // coat_of_arms/01_subs.txt defines coats of arms twice in one file; the game keeps the last
		"eu5/unresolved-ref":        218, // a LEARNER gap, not the game's: setup/start wraps dynasties, country tags and characters under wrapper words the learner reads as one kind's names, so dynasty: and c: cites resolve against nothing (§19). This ceiling goes when the learner reads the wrappers
	}
	for _, g := range []struct{ id, env string }{{"ck3", "PMT_TEST_INSTALL_CK3"}, {"vic3", "PMT_TEST_INSTALL_VIC3"}, {"eu5", "PMT_TEST_INSTALL_EU5"}} {
		dir := os.Getenv(g.env)
		if dir == "" {
			t.Errorf("%s: %s is not set", g.id, g.env)
			continue
		}
		user := game.UserDataDir(g.id, dir)
		install, err := index.Scan(context.Background(), index.Request{Game: g.id, Dir: dir, Version: "gate", Docs: []string{filepath.Join(user, "logs"), filepath.Join(user, "docs")}})
		if err != nil {
			t.Fatalf("%s: %v", g.id, err)
		}

		// the sample, copied into a mod
		var common, events []source.File
		for _, f := range install.Index.Files {
			switch {
			case f.Format != source.Script || !strings.HasSuffix(f.Rel, ".txt"):
			case strings.HasPrefix(f.Rel, "events/") || strings.Contains(f.Rel, "/events/"):
				events = append(events, f)
			case strings.Contains(f.Rel, "common/"):
				common = append(common, f)
			}
		}
		slices.SortFunc(common, func(a, b source.File) int { return strings.Compare(a.Rel, b.Rel) })
		slices.SortFunc(events, func(a, b source.File) int { return strings.Compare(a.Rel, b.Rel) })
		sample := append(common[:min(400, len(common))], events[:min(250, len(events))]...)
		mod := t.TempDir()
		for _, f := range sample {
			raw, err := os.ReadFile(f.Path)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(mod, filepath.FromSlash(f.Rel))
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, raw, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		s, err := session.New(context.Background(), game.Get(g.id), install, []session.Mod{{ID: "sample", Name: "Sample", Dir: mod}}, "")
		if err != nil {
			t.Fatal(err)
		}

		// every file judged; a finding is shown with its first site
		counts, first := map[string]int{}, map[string]string{}
		parse := 0
		for _, f := range sample {
			path := filepath.Join(mod, filepath.FromSlash(f.Rel))
			found, err := Diagnose(s, path, on)
			if err != nil {
				t.Fatal(err)
			}
			for _, d := range found {
				if !on[d.Code] && !slices.ContainsFunc(Codes, func(c Code) bool { return c.Code == d.Code }) {
					parse++
					continue
				}
				counts[d.Code]++
				if _, seen := first[d.Code]; !seen {
					first[d.Code] = fmt.Sprintf("%s:%d %s", f.Rel, d.Range.Start.Line+1, d.Message)
				}
			}
		}
		t.Logf("DIAGNOSE %s: %d files, %d parse errors, findings %v", g.id, len(sample), parse, counts)
		for code, n := range counts {
			if n > ceilings[g.id+"/"+code] {
				t.Errorf("%s: %s fires %d times on the game's own files; first at %s", g.id, code, n, first[code])
			}
		}
	}
}
