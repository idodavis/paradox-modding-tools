// lsp_test.go holds the fixtures every LSP test shares: a session over the
// testdata install scanned for real, a temp mod, and the assertion helpers.

package lsp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"paradox-modding-tools/services/internal/lang/declared"
	"paradox-modding-tools/services/internal/lang/index"
	"paradox-modding-tools/services/internal/lang/session"
)

var (
	fixtures  = map[string]*index.Index{}
	fixtureMu sync.Mutex
)

// scanFixture scans the testdata install for one game into a fresh index.
func scanFixture(t *testing.T, game string) *index.Index {
	t.Helper()
	root := filepath.Join("..", "testdata", game)
	var decl declared.Schema
	if raw, err := os.ReadFile(filepath.Join(root, "declared.json")); err == nil {
		if err := json.Unmarshal(raw, &decl); err != nil {
			t.Fatal(err)
		}
	}
	x, _, err := index.Scan(context.Background(), index.ScanRequest{GameID: game, InstallPath: root, LocLang: "english", Declared: &decl})
	if err != nil {
		t.Fatal(err)
	}
	return x
}

// fixture is the shared scan of one game's testdata install.
func fixture(t *testing.T, game string) *index.Index {
	t.Helper()
	fixtureMu.Lock()
	defer fixtureMu.Unlock()
	if fixtures[game] == nil {
		fixtures[game] = scanFixture(t, game)
	}
	return fixtures[game]
}

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

// testCalendar is applied to the mod buildSession creates.
var testCalendar index.ModCalendar

func withCalendar(t *testing.T, c index.ModCalendar) {
	t.Helper()
	testCalendar = c
	t.Cleanup(func() { testCalendar = index.ModCalendar{} })
}

// buildSession is a session over the game's fixture install and one temp mod holding files, every file opened in the editor.
func buildSession(t *testing.T, game string, files map[string]string) (*session.Session, string) {
	t.Helper()
	if game == "" {
		game = "ck3"
	}
	return sessionOver(t, game, fixture(t, game), files)
}

func sessionOver(t *testing.T, game string, cache *index.Index, files map[string]string) (*session.Session, string) {
	t.Helper()
	root := t.TempDir()
	if game == "ck3" {
		write(t, root, "descriptor.mod", "name = \"t\"\n")
	} else {
		write(t, root, ".metadata/metadata.json", `{"name":"t"}`)
	}
	for rel, body := range files {
		write(t, root, rel, body)
	}
	s := session.New("ws", game, "english", cache, []index.ModInput{{Origin: "mod", Root: root, Calendar: testCalendar}})
	for rel, body := range files {
		s.DidOpen(filepath.Join(root, filepath.FromSlash(rel)), body)
	}
	return s, root
}

// bareSession is a session over an install with no type system and nothing scanned.
func bareSession(t *testing.T, files map[string]string) (*session.Session, string) {
	t.Helper()
	return sessionOver(t, "ck3", index.NewIndex("ck3", &declared.Schema{}), files)
}

func ck3Sess(t *testing.T, body string) (*session.Session, string) {
	t.Helper()
	s, root := buildSession(t, "ck3", map[string]string{"events/x.txt": body})
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

func hasLocURI(locs []Location, path string) bool {
	for _, l := range locs {
		if index.SamePath(l.URI, path) {
			return true
		}
	}
	return false
}

// `key = ` parses to an Assignment with a nil Value, which is the buffer's state on every keystroke while typing one.
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
		s, f := ck3Sess(t, body)
		for line := range 4 {
			for col := range 24 {
				func() {
					defer func() {
						if r := recover(); r != nil {
							t.Fatalf("panic at %d:%d in %q: %v", line, col, body, r)
						}
					}()
					_ = Hover(s, f, line, col)
					_ = Complete(s, f, line, col).Items
					_ = Definition(s, f, line, col)
					_ = References(s, f, line, col)
					_ = Diagnose(s, f)
					_ = FormatDocument(s, f)
				}()
			}
		}
	}
}
