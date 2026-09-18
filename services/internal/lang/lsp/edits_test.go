// edits_test.go covers formatting, folding, the quick-fixes and the era hints.
package lsp

import (
	"path/filepath"
	"strings"
	"testing"

	"paradox-modding-tools/services/internal/lang/index"
)

func TestFormat(t *testing.T) {
	body := "a = {\n b = 1\n\t\tc = {\n\td = 1\n\t}\n}\n"
	s, root := buildSession(t, "ck3", map[string]string{"events/x.txt": body})
	f := filepath.Join(root, "events", "x.txt")
	edits := FormatDocument(s, f)
	if len(edits) == 0 {
		t.Fatal("want edits for mixed indentation")
	}
	for _, e := range edits {
		if e.NewText != "" && e.NewText[0] == ' ' {
			t.Errorf("script indent used spaces: %q", e.NewText)
		}
	}

	// An empty file and a metadata file format to nothing.
	s2, root2 := buildSession(t, "vic3", map[string]string{".metadata/metadata.json": "{}"})
	if edits := FormatDocument(s2, filepath.Join(root2, ".metadata", "metadata.json")); edits != nil {
		t.Errorf("metadata file must not be formatted: %v", edits)
	}

	// Loc indents with one leading space after the header, never a tab.
	locBody := "l_english:\n\tk:0 \"v\"\n"
	s3, root3 := buildSession(t, "ck3", map[string]string{
		"localization/english/a_l_english.yml": locBody,
	})
	lf := filepath.Join(root3, "localization", "english", "a_l_english.yml")
	if edits := FormatDocument(s3, lf); len(edits) != 1 || edits[0].NewText != " " {
		t.Fatalf("loc format = %+v, want one edit to a single space", edits)
	}
}

func TestFolding(t *testing.T) {
	body := "a = {\n\tb = {\n\t\tc = 1\n\t}\n}\nd = 1\n"
	s, root := buildSession(t, "ck3", map[string]string{"events/x.txt": body})
	f := filepath.Join(root, "events", "x.txt")
	got := FoldingRanges(s, f)
	if len(got) != 2 {
		t.Fatalf("folding ranges = %+v, want 2 (one per block)", got)
	}
	// A one-line block folds to nothing: start and end coincide.
	if !hasFolding(got, 0, 4) {
		t.Errorf("outer block not folded 0..4: %+v", got)
	}
	if !hasFolding(got, 1, 3) {
		t.Errorf("inner block not folded 1..3: %+v", got)
	}
	if got := FoldingRanges(s, filepath.Join(root, "descriptor.mod")); got != nil {
		t.Errorf("meta file should not fold: %v", got)
	}
}

func hasFolding(rs []FoldingRange, start, end int) bool {
	for _, r := range rs {
		if r.StartLine == start && r.EndLine == end {
			return true
		}
	}
	return false
}

const eraSrc = "namespace = agot\n\nagot.1 = {\n\ttrigger = {\n\t\tgame_start_date >= 8082.1.1\n\t}\n" +
	"\timmediate = {\n\t\tset_variable = { name = x value = 5 }\n\t}\n}\n"

func TestInlay(t *testing.T) {
	t.Run("silent without a calendar", func(t *testing.T) {
		s, root := buildSession(t, "ck3", map[string]string{"events/agot_events.txt": eraSrc})
		if got := InlayHints(s, root+"/events/agot_events.txt"); len(got) != 0 {
			t.Fatalf("hints without a declared calendar: %+v", got)
		}
	})
	withCalendar(t, index.ModCalendar{Era: "AC", EpochOffset: 7782})
	s, root := buildSession(t, "ck3", map[string]string{
		"events/agot_events.txt": eraSrc,
		// A history file keys its block BY a date, which is the one a reader most needs converted.
		"history/characters/agot.txt": "1000 = {\n\t8082.1.1 = {\n\t\tbirth = yes\n\t}\n}\n",
	})
	for _, rel := range []string{"events/agot_events.txt", "history/characters/agot.txt"} {
		got := InlayHints(s, root+"/"+rel)
		found := false
		for _, h := range got {
			found = found || strings.TrimSpace(h.Label) == "300.1.1 AC"
			if strings.Contains(h.Label, "-") {
				t.Errorf("negative era year rendered: %q", h.Label)
			}
		}
		if !found {
			t.Fatalf("%s: no hint labelled 300.1.1 AC in %+v", rel, got)
		}
	}
	// A bare number and a quoted string are not dates: only the one date in eraSrc earns a hint.
	if got := InlayHints(s, root+"/events/agot_events.txt"); len(got) != 1 {
		t.Fatalf("got %d hints, want exactly the one date: %+v", len(got), got)
	}
}

func TestCodeActions(t *testing.T) {
	// A loc file with no BOM offers to add one.
	locRel := "localization/english/a_l_english.yml"
	s, root := buildSession(t, "ck3", map[string]string{locRel: "l_english:\n k:0 \"v\"\n"})
	lf := filepath.Join(root, filepath.FromSlash(locRel))
	acts := CodeActions(s, lf)
	if !hasAction(acts, "Add UTF-8 BOM") {
		t.Fatalf("no BOM action: %+v", acts)
	}

	// A script file citing a loc key nothing defines offers to create it, into
	// the existing localization file rather than a new one.
	scriptRel := "events/x.txt"
	s2, root2 := buildSession(t, "ck3", map[string]string{
		scriptRel: "ns.1 = {\n\ttitle = missing_key\n}\n",
		locRel:    "l_english:\n k:0 \"v\"\n",
	})
	sf := filepath.Join(root2, filepath.FromSlash(scriptRel))
	acts2 := CodeActions(s2, sf)
	var create *CodeAction
	for i, a := range acts2 {
		if a.Title == `Create localization key "missing_key"` {
			create = &acts2[i]
		}
	}
	if create == nil {
		t.Fatalf("no create-loc-key action: %+v", acts2)
	}
	target := filepath.Join(root2, filepath.FromSlash(locRel))
	if _, ok := create.Edit.Changes[target]; !ok {
		t.Fatalf("edit targets %v, want the existing loc file %s", create.Edit.Changes, target)
	}

	// A key already defined offers nothing.
	s3, root3 := buildSession(t, "ck3", map[string]string{
		scriptRel: "ns.1 = {\n\ttitle = k\n}\n",
		locRel:    "l_english:\n k:0 \"v\"\n",
	})
	if acts3 := CodeActions(s3, filepath.Join(root3, filepath.FromSlash(scriptRel))); len(acts3) != 0 {
		t.Errorf("defined key still offered a create action: %+v", acts3)
	}
}

func hasAction(acts []CodeAction, title string) bool {
	for _, a := range acts {
		if a.Title == title {
			return true
		}
	}
	return false
}
