// codeactions_test.go covers the two quick-fixes: add a UTF-8 BOM, and create a
// missing localization key.
package lsp

import (
	"path/filepath"
	"testing"
)

func TestCodeActions(t *testing.T) {
	// A loc file with no BOM offers to add one.
	locRel := "localization/english/a_l_english.yml"
	s, root := buildSession(t, "ck3", map[string]string{locRel: "l_english:\n k:0 \"v\"\n"}, nil, nil)
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
	}, nil, nil)
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
	}, nil, nil)
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
