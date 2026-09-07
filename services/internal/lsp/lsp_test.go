// lsp_test.go holds the fixtures every LSP test shares: building a live session
// over a temp mod, and the assertion helpers. The tests themselves live beside
// the request they cover — hover_test.go, definition_test.go, complete_test.go,
// diagnostics_test.go, scope_test.go.

package lsp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"paradox-modding-tools/services/internal/catalog"
	"paradox-modding-tools/services/internal/session"
)

func write(t *testing.T, root, rel, body string) string {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// testCalendar is applied to the mod buildSession creates. Package-level so the
// common builder keeps one signature; set it with withCalendar.
var testCalendar catalog.ModCalendar

// withCalendar declares an era for the mod the next buildSession creates, and
// clears it when the test ends.
func withCalendar(t *testing.T, c catalog.ModCalendar) {
	t.Helper()
	testCalendar = c
	t.Cleanup(func() { testCalendar = catalog.ModCalendar{} })
}

func buildSession(
	t *testing.T, game string, files map[string]string,
	cache *catalog.VanillaCache, vloc *catalog.VanillaLoc,
) (*session.Session, string) {
	t.Helper()
	if game == "" {
		game = "ck3"
	}
	root := t.TempDir()
	if game == "ck3" {
		if _, ok := files["descriptor.mod"]; !ok {
			write(t, root, "descriptor.mod", "name = \"t\"\n")
		}
	}
	for rel, body := range files {
		write(t, root, rel, body)
	}
	s := session.NewWithLoc("ws", game, "english", cache, vloc, []catalog.ModInput{
		{Origin: "mod", Root: root, Order: 0, Calendar: testCalendar},
	})
	for rel, body := range files {
		s.DidOpen(filepath.Join(root, filepath.FromSlash(rel)), body)
	}
	return s, root
}

// ck3Typed is a cache that declares the CK3 scope types and links these tests
// rely on. Nested databases (titles under landed_titles, faiths under religions)
// are only harvested when the game declares the child kind, so a fixture with a
// nil cache harvests none.
func ck3Typed() *catalog.VanillaCache {
	return &catalog.VanillaCache{Schema: &catalog.Schema{
		Scopes: map[string]catalog.ScopeType{
			"faith": {}, "culture": {}, "character": {}, "landed_title": {},
		},
		Links: map[string]catalog.ScopeLink{
			"title": {Out: "landed_title", Global: true, Data: true},
			"faith": {Out: "faith", Global: true, Data: true},
		},
	}}
}

func ck3Sess(t *testing.T, body string) (*session.Session, string) {
	t.Helper()
	s, root := buildSession(t, "ck3", map[string]string{"events/x.txt": body}, nil, nil)
	return s, filepath.Join(root, "events", "x.txt")
}

func lineCol(src, needle string) (int, int) {
	i := strings.Index(src, needle)
	if i < 0 {
		return -1, -1
	}
	return strings.Count(src[:i], "\n"), i - (strings.LastIndex(src[:i], "\n") + 1)
}

func hasDiag(diags []Diagnostic, code string) bool {
	for _, d := range diags {
		if d.Code == code {
			return true
		}
	}
	return false
}

func wantHover(t *testing.T, s *session.Session, f string, line, col int, subs ...string) {
	t.Helper()
	h := Hover(s, f, line, col)
	if h == nil {
		t.Fatalf("hover nil at %d:%d want %v", line, col, subs)
	}
	blob := h.Kind + "\n" + h.Key + "\n" + h.Hint + "\n" + h.Docs + "\n" + h.Body + "\n" + h.Usage + "\n" + h.Owner
	for _, v := range h.Values {
		blob += "\n" + v.Text
	}
	for _, sub := range subs {
		if !strings.Contains(blob, sub) {
			t.Fatalf("hover kind=%#v key=%#v hint=%#v docs=%#v body=%#v usage=%#v missing %q",
				h.Kind, h.Key, h.Hint, h.Docs, h.Body, h.Usage, sub)
		}
	}
}

// `key = ` parses to an Assignment with a nil Value. Every request must survive
// it: this is the state the buffer is in on every keystroke while typing a
// value, so a panic here breaks the editor mid-word.
func TestZNilValueRequests(t *testing.T) {
	for _, body := range []string{
		"test.1 = {\n\ttype = \n}\n",
		"test.1 = {\n\timmediate = {\n\t\tadd_gold = \n\t}\n}\n",
		"test.1 = \n",
		"a = = \n",
		"test.1 = {\n\tculture_group:\n}\n",
		"test.1 = {\n\thas_culture_group = culture_group:\n}\n",
		"x = {\n\ty =",
	} {
		s, root := buildSession(t, "ck3", map[string]string{"events/x.txt": body}, nil, nil)
		f := filepath.Join(root, "events", "x.txt")
		for line := range 4 {
			for col := range 24 {
				func() {
					defer func() {
						if r := recover(); r != nil {
							t.Fatalf("panic at %d:%d in %q: %v", line, col, body, r)
						}
					}()
					_ = Hover(s, f, line, col)
					_ = Complete(s, f, line, col)
					_ = Definition(s, f, line, col)
					_ = References(s, f, line, col)
					_ = Diagnose(s, f)
					_ = FormatDocument(s, f)
				}()
			}
		}
	}
}
