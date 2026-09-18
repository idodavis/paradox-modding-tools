// signature_test.go covers Monaco signature help: usage text keyed by the declared token.
package lsp

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestSignatureHelp(t *testing.T) {
	body := "ns.1 = {\n\timmediate = { add_gold = {  } }\n}\n"
	s, root := buildSession(t, "ck3", map[string]string{"events/x.txt": body})
	f := filepath.Join(root, "events", "x.txt")
	line, col := lineCol(body, "add_gold = {")
	h := SignatureHelp(s, f, line, col+len("add_gold = {"))
	if h == nil || !strings.Contains(h.Label, "add_gold") || len(h.Parameters) != 1 || h.Parameters[0] != "VALUE" {
		t.Fatalf("declared usage: %#v", h)
	}
	// Nothing usage-shaped names this position: no card, not a blank one.
	line, col = lineCol(body, "ns.1")
	if h := SignatureHelp(s, f, line, col+2); h != nil {
		t.Fatalf("unusaged word gave signature help: %#v", h)
	}
}
