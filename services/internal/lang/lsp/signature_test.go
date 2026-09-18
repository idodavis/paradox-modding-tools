// signature_test.go covers Monaco signature help: usage text keyed by the
// declared token or, failing that, the macro it resolves to.
package lsp

import (
	"path/filepath"
	"strings"
	"testing"

	"paradox-modding-tools/services/internal/lang/declared"
)

func TestSignatureHelp(t *testing.T) {
	cache := &vanillaCache{Schema: &declared.Schema{Effects: map[string]declared.EngineToken{
		"add_gold": {Usage: "add_gold = { $VALUE$ }"},
	}}}
	body := "ns.1 = {\n\timmediate = { add_gold = {  } }\n}\n"
	s, root := buildSession(t, "ck3", map[string]string{"events/x.txt": body}, cache, nil)
	f := filepath.Join(root, "events", "x.txt")
	line, col := lineCol(body, "add_gold = {")
	h := SignatureHelp(s, f, line, col+len("add_gold = {"))
	if h == nil || !strings.Contains(h.Label, "add_gold") {
		t.Fatalf("declared usage: %#v", h)
	}
	if want := []string{"VALUE"}; len(h.Parameters) != 1 || h.Parameters[0] != want[0] {
		t.Fatalf("parameters = %v, want %v", h.Parameters, want)
	}

	// Nothing usage-shaped names this position: no card, not a blank one.
	line3, col3 := lineCol(body, "ns.1")
	if h3 := SignatureHelp(s, f, line3, col3+2); h3 != nil {
		t.Fatalf("unusaged word gave signature help: %#v", h3)
	}
}
