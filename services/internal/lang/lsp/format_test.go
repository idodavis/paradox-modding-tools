// format_test.go covers whitespace formatting and folding ranges.
package lsp

import (
	"path/filepath"
	"testing"
)

func TestFormat(t *testing.T) {
	body := "a = {\n b = 1\n\t\tc = {\n\td = 1\n\t}\n}\n"
	s, root := buildSession(t, "ck3", map[string]string{"events/x.txt": body}, nil, nil)
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
	s2, root2 := buildSession(t, "vic3", map[string]string{".metadata/metadata.json": "{}"}, nil, nil)
	if edits := FormatDocument(s2, filepath.Join(root2, ".metadata", "metadata.json")); edits != nil {
		t.Errorf("metadata file must not be formatted: %v", edits)
	}

	// Loc indents with one leading space after the header, never a tab.
	locBody := "l_english:\n\tk:0 \"v\"\n"
	s3, root3 := buildSession(t, "ck3", map[string]string{
		"localization/english/a_l_english.yml": locBody,
	}, nil, nil)
	lf := filepath.Join(root3, "localization", "english", "a_l_english.yml")
	if edits := FormatDocument(s3, lf); len(edits) != 1 || edits[0].NewText != " " {
		t.Fatalf("loc format = %+v, want one edit to a single space", edits)
	}
}

func TestFolding(t *testing.T) {
	body := "a = {\n\tb = {\n\t\tc = 1\n\t}\n}\nd = 1\n"
	s, root := buildSession(t, "ck3", map[string]string{"events/x.txt": body}, nil, nil)
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
