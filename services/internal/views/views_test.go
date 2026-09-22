// views_test.go holds what the package's tests share: one scan of the ck3 fixture install, and a session over it with a mod the test writes.
package views

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/index"
	"paradox-modding-tools/services/internal/lang/session"
)

var scanned = sync.OnceValues(func() (*index.Install, error) {
	dir := filepath.Join("..", "lang", "testdata", "ck3")
	return index.Scan(context.Background(), index.Request{Game: "ck3", Dir: dir, Version: "test", Docs: []string{filepath.Join(dir, "docs")}})
})

// workspace is a session over the fixture install with the given mods, each a map of rel to text.
func workspace(t *testing.T, mods ...map[string]string) *session.Session {
	t.Helper()
	install, err := scanned()
	if err != nil {
		t.Fatal(err)
	}
	var list []session.Mod
	for i, files := range mods {
		dir := t.TempDir()
		for rel, text := range files {
			path := filepath.Join(dir, filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		id := string(rune('a' + i))
		list = append(list, session.Mod{ID: "mod" + id, Name: "Mod " + id, Dir: dir})
	}
	s, err := session.New(context.Background(), game.Get("ck3"), install, list, "")
	if err != nil {
		t.Fatal(err)
	}
	return s
}
