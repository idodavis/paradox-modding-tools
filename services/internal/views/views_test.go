// views_test.go holds the fixtures every view test shares: a session over the testdata install scanned for real, and temp mods.

package views

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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

// fixture is the shared scan of one game's testdata install.
func fixture(t *testing.T, game string) *index.Index {
	t.Helper()
	fixtureMu.Lock()
	defer fixtureMu.Unlock()
	if x := fixtures[game]; x != nil {
		return x
	}
	root := filepath.Join("..", "lang", "testdata", game)
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
	fixtures[game] = x
	return x
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

// buildSession is a session over the game's fixture install and the given mods.
func buildSession(t *testing.T, game string, mods ...index.ModInput) *session.Session {
	t.Helper()
	if game == "" {
		game = "ck3"
	}
	return session.New("ws", game, "english", fixture(t, game), mods)
}

// oneMod writes files into a temp mod with the game's descriptor.
func oneMod(t *testing.T, game string, files map[string]string) index.ModInput {
	t.Helper()
	root := t.TempDir()
	if game == "ck3" || game == "" {
		write(t, root, "descriptor.mod", "name = \"t\"\n")
	} else {
		write(t, root, ".metadata/metadata.json", `{"name":"t"}`)
	}
	for rel, body := range files {
		write(t, root, rel, body)
	}
	return index.ModInput{Origin: "mod", Root: root, Order: 0}
}
