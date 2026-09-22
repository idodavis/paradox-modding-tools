// edits_test.go covers formatting, folding, the two quick-fixes, the era hints, and that no request panics at any position of a file.
package lsp

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestFormat(t *testing.T) {
	s, dir := fixture(t, "ck3")
	path := opened(s, dir, "events/x.txt", "a = {\n b = 1\n\t\tc = {\n\td = 1\n\t}\n}\n")
	edits, err := Format(s, path)
	if err != nil || len(edits) == 0 || slices.ContainsFunc(edits, func(e TextEdit) bool { return strings.HasPrefix(e.NewText, " ") }) {
		t.Errorf("mixed indentation is fixed with tabs: %+v (%v)", edits, err)
	}
	if edits, _ := Format(s, opened(s, dir, "events/y.txt", "a = {\r\n\tb = 1\r\n}\r\n")); len(edits) != 0 {
		t.Errorf("a file indented right, with its own line endings, needs nothing: %+v", edits)
	}
	if edits, _ := Format(s, opened(s, dir, "localization/english/a_l_english.yml", "l_english:\n\tk:0 \"v\"\n")); len(edits) != 1 || edits[0].NewText != " " {
		t.Errorf("localization indents with one space: %+v", edits)
	}
	if edits, _ := Format(s, opened(s, dir, "descriptor.mod", "name = \"M\"\n")); edits != nil {
		t.Errorf("a descriptor is not formatted: %+v", edits)
	}
}

func TestFolding(t *testing.T) {
	s, dir := fixture(t, "ck3")
	got, err := Folding(s, opened(s, dir, "events/x.txt", "a = {\n\tb = {\n\t\tc = 1\n\t}\n}\nd = 1\n"))
	if err != nil || !slices.Equal(got, []FoldingRange{{0, 4}, {1, 3}}) {
		t.Errorf("one range per block: %+v (%v)", got, err)
	}
	if got, _ := Folding(s, opened(s, dir, "descriptor.mod", "tags = {\n\t\"x\"\n}\n")); got != nil {
		t.Errorf("a descriptor does not fold: %+v", got)
	}
}

func TestCodeActions(t *testing.T) {
	s, dir := fixture(t, "ck3")
	locPath := filepath.Join(dir, "localization", "english", "a_l_english.yml")
	if err := os.MkdirAll(filepath.Dir(locPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(locPath, []byte("l_english:\n k:0 \"v\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.Changed(locPath)
	s.DidOpen(locPath, "l_english:\n k:0 \"v\"\n")
	actions, _ := CodeActions(s, locPath, 0, 0)
	if len(actions) != 1 || actions[0].Title != "Add UTF-8 BOM" || actions[0].Edit.Changes[locPath][0].NewText != "\xEF\xBB\xBF" {
		t.Errorf("a loc file with no BOM: %+v", actions)
	}

	text := "namespace = m\nm.1 = {\n\ttitle = missing_key\n}\n"
	path := opened(s, dir, "events/x.txt", text)
	line, col := aim(t, text, "missing_key", 0)
	actions, _ = CodeActions(s, path, line, col)
	if len(actions) != 1 || actions[0].Title != `Create localization key "missing_key"` {
		t.Fatalf("a missing loc key offers to create it: %+v", actions)
	}
	if edits, ok := actions[0].Edit.Changes[locPath]; !ok || len(edits) != 1 || edits[0].Range.Start.Line != 2 || edits[0].NewText != " missing_key:0 \"\"\n" {
		t.Errorf("the key goes at the end of the mod's existing loc file: %+v", actions[0].Edit)
	}
	text = "namespace = m\nm.1 = {\n\ttitle = k\n}\n"
	path = opened(s, dir, "events/x.txt", text)
	line, col = aim(t, text, "k\n", 0)
	if actions, _ := CodeActions(s, path, line, col); len(actions) != 0 {
		t.Errorf("a key already defined offers nothing: %+v", actions)
	}
}

func TestInlayHints(t *testing.T) {
	s, dir := fixture(t, "ck3")
	text := "namespace = m\nm.1 = {\n\tstart = 8082.1.1\n\tcount = 5\n\tname = x\n}\n"
	got, err := InlayHints(s, opened(s, dir, "events/x.txt", text))
	if err != nil || len(got) != 1 || strings.TrimSpace(got[0].Label) != "300.1.1 AC" || got[0].Position.Line != 2 {
		t.Errorf("only a date earns a hint, in the mod's era: %+v (%v)", got, err)
	}
	got, _ = InlayHints(s, opened(s, dir, "history/characters/m.txt", "1000 = {\n\t8082.1.1 = {\n\t\tbirth = yes\n\t}\n\t100.1.1 = { }\n}\n"))
	if len(got) != 1 || strings.Contains(got[0].Label, "-") {
		t.Errorf("a date as a key earns a hint; a year the offset pushes below one earns none: %+v", got)
	}
	s.Mods[0].Calendar.Era = ""
	if got, _ := InlayHints(s, opened(s, dir, "events/y.txt", text)); len(got) != 0 {
		t.Errorf("silent without a calendar: %+v", got)
	}
}

func TestNoPanicAnywhere(t *testing.T) {
	s, dir := fixture(t, "ck3")
	for _, tt := range []struct{ rel, text string }{
		{"events/x.txt", "namespace = m\nm.1 = {\n\ttrigger = { has_trait = trait:br\n\timmediate = { v_effect = { AMOUNT = $X$ } }\n"},
		{"localization/english/m_l_english.yml", "l_english:\n k:0 \"$v.1.t$ and $VALUE|=+0$\n"},
		{"events/empty.txt", ""},
		{"common/traits/m.txt", "= {\n}}}\n\"\n"},
	} {
		path := opened(s, dir, tt.rel, tt.text)
		for line := range 4 {
			for col := range 24 {
				Hover(s, path, line, col)
				Complete(s, path, line, col)
				Definition(s, path, line, col)
				References(s, path, line, col)
				SignatureHelp(s, path, line, col)
				CodeActions(s, path, line, col)
			}
		}
		Diagnose(s, path, nil)
		Format(s, path)
		Folding(s, path)
		InlayHints(s, path)
		DocumentSymbols(s, path)
	}
}
