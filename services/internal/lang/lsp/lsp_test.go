// lsp_test.go holds what the package's tests share: one session over the ck3 fixture install with a mod the tests write files into, and the way a request is aimed at a word of a file.
package lsp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/index"
	"paradox-modding-tools/services/internal/lang/session"
)

var installs sync.Map

// fixture is a session over one fixture install, scanned once, with an empty mod folder of the test's own.
func fixture(t *testing.T, id string) (*session.Session, string) {
	t.Helper()
	scan, _ := installs.LoadOrStore(id, sync.OnceValues(func() (*index.Install, error) {
		dir := filepath.Join("..", "testdata", id)
		return index.Scan(context.Background(), index.Request{Game: id, Dir: dir, Version: "test", Docs: []string{filepath.Join(dir, "docs")}})
	}))
	install, err := scan.(func() (*index.Install, error))()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "common"), 0o755); err != nil {
		t.Fatal(err)
	}
	s, err := session.New(context.Background(), game.Get(id), install, []session.Mod{{ID: "mod", Name: "M", Dir: dir, Calendar: session.Calendar{Era: "AC", EpochOffset: 7782}}}, "")
	if err != nil {
		t.Fatal(err)
	}
	return s, dir
}

// opened puts a file of the mod in the editor with a text, and gives its path.
func opened(s *session.Session, dir, rel, text string) string {
	path := filepath.Join(dir, filepath.FromSlash(rel))
	s.DidOpen(path, text)
	return path
}

// aim is the line and column of a needle in a text, plus some columns in.
func aim(t *testing.T, text, needle string, plus int) (int, int) {
	t.Helper()
	at := strings.Index(text, needle)
	if at < 0 {
		t.Fatalf("no %q in the text", needle)
	}
	at += plus
	line := strings.Count(text[:at], "\n")
	return line, at - (strings.LastIndex(text[:at], "\n") + 1)
}
