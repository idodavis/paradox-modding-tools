package search

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/services/app"
	"paradox-modding-tools/services/workspace"
)

// fixtureModID is the id the tests give the fixture mod.
const fixtureModID = "mod-a"

// newApp returns the one application the tests send events through. It is never run.
var newApp = sync.OnceValue(func() *application.App { return application.New(application.Options{}) })

// resolveFixture returns the absolute path of a folder under internal/testdata.
func resolveFixture(t *testing.T, parts ...string) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join(append([]string{"..", "..", "internal", "testdata"}, parts...)...))
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// writeFile writes a file of the given bytes under a folder, making the folders above it.
func writeFile(t *testing.T, dir, rel string, bytes []byte) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, bytes, 0o644); err != nil {
		t.Fatal(err)
	}
}

// openService opens a workspace on the CK3 fixture install with a copy of the fixture mod, after the given function
// adds files to it. It returns the search service, the open session and the mod's folder. The workspace closes when the
// test ends.
func openService(t *testing.T, prepare func(dir string)) (*Service, *workspace.Session, string) {
	t.Helper()
	newApp()
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS(resolveFixture(t, "mods", "ck3"))); err != nil {
		t.Fatal(err)
	}
	prepare(dir)
	// the folders PMT uses come from the XDG variables, read again now and once more when the test ends
	for _, variable := range []string{"APPDATA", "LOCALAPPDATA", "XDG_CONFIG_HOME", "XDG_CACHE_HOME"} {
		t.Setenv(variable, t.TempDir())
	}
	config, err := app.OpenConfig()
	if err != nil {
		t.Fatal(err)
	}
	err = config.Update("", func(records *app.Config) error {
		records.Installs = []app.Install{{ID: "i1", Game: "ck3", Path: resolveFixture(t, "ck3")}}
		records.Workspaces = []app.Workspace{{
			ID: "w1", Name: "Test", Install: "i1", Mods: []app.Mod{{ID: fixtureModID, Dir: dir}}, Language: "english",
		}}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	wsp := &workspace.Service{Config: config, Build: "test"}
	wsp.Open("w1")
	t.Cleanup(wsp.Close)
	ws, err := wsp.Session()
	if err != nil {
		t.Fatal(err)
	}
	return &Service{Workspace: wsp}, ws, dir
}

// listHits runs a search and returns its hits, which must be in the given number of files.
func listHits(t *testing.T, ws *workspace.Session, query Query, fileCount int) []Hit {
	t.Helper()
	collected, _, err := CollectText(ws, query, maxHits, make(chan struct{}))
	if err != nil {
		t.Fatal(err)
	}
	if len(collected) != fileCount {
		t.Fatalf("hits of %d files were found, want %d", len(collected), fileCount)
	}
	var hits []Hit
	for _, file := range collected {
		hits = append(hits, file.Hits...)
	}
	return hits
}

// TestRunFindsRegexAndWholeWordHits checks the ranges and lines of the hits of a whole-word search and of a regular
// expression. The whole-word search ignores case and counts a letter beyond ASCII as part of a word.
func TestRunFindsRegexAndWholeWordHits(t *testing.T) {
	include := "search_a.txt"
	_, ws, _ := openService(t, func(dir string) {
		writeFile(t, dir, "common/search_a.txt", []byte("add_gold = 1\nadd_gold_more = 2\n  Add_Gold = 3\ncafés café\n"))
	})
	whole := listHits(t, ws, Query{Text: "add_gold", WholeWord: true, Include: include}, 1)
	if len(whole) != 2 || whole[0].Range.StartLine != 1 || whole[1].Range.StartLine != 3 {
		t.Fatalf("whole-word hits: %+v", whole)
	}
	if whole[1].Text != "  Add_Gold = 3" || whole[1].Range.StartColumn != 3 || whole[1].Range.EndColumn != 11 ||
		whole[1].Place.Origin != fixtureModID {
		t.Errorf("whole-word hit: %+v", whole[1])
	}
	pattern := listHits(t, ws, Query{Text: `add_gold\w*`, Regex: true, MatchCase: true, Include: include}, 1)
	if len(pattern) != 2 || pattern[0].Range.StartLine != 1 || pattern[1].Range.StartLine != 2 {
		t.Errorf("regular expression hits: %+v", pattern)
	}
	accented := listHits(t, ws, Query{Text: "café", WholeWord: true, Include: include}, 1)
	if len(accented) != 1 || accented[0].Range.StartColumn != 7 || accented[0].Range.EndColumn != 11 {
		t.Errorf("accented hits: %+v", accented)
	}
}

// TestRunSearchesOpenBufferText checks that a search reads the editor's text of an open file.
func TestRunSearchesOpenBufferText(t *testing.T) {
	_, ws, dir := openService(t, func(dir string) {
		writeFile(t, dir, "common/search_b.txt", []byte("on_disk = yes\n"))
	})
	ws.SetText(filepath.Join(dir, "common", "search_b.txt"), "in_editor = yes\n")
	hits := listHits(t, ws, Query{Text: "in_editor", Include: "search_b.txt"}, 1)
	if len(hits) != 1 || hits[0].Text != "in_editor = yes" {
		t.Errorf("the editor's text was not searched: %+v", hits)
	}
}

// TestReplaceTextKeepsEncodingAndSkipsOpenFile checks that a Windows-1252 file keeps its encoding, that an open file is
// left as it is, and that the error counts it.
func TestReplaceTextKeepsEncodingAndSkipsOpenFile(t *testing.T) {
	sch, ws, dir := openService(t, func(dir string) {
		writeFile(t, dir, "common/closed.txt", []byte("caf\xe9 = 1\n"))
		writeFile(t, dir, "common/open.txt", []byte("caf\xe9 = 2\n"))
	})
	open := filepath.Join(dir, "common", "open.txt")
	ws.SetText(open, "café = 2\n")
	count, err := sch.ReplaceText(Query{Text: "café"}, "thé$1")
	if count != 1 || err == nil || !strings.HasSuffix(err.Error(), "skipped: 1") {
		t.Errorf("replaced %d, error %v", count, err)
	}
	if bytes, _ := os.ReadFile(filepath.Join(dir, "common", "closed.txt")); string(bytes) != "th\xe9$1 = 1\n" {
		t.Errorf("closed file: %q", bytes)
	}
	if bytes, _ := os.ReadFile(open); string(bytes) != "caf\xe9 = 2\n" {
		t.Errorf("open file: %q", bytes)
	}
}

// TestReplaceTextCountsTheHitsShown checks that a replace changes exactly what a search shows. An expression that
// matches empty text or spans two lines replaces nothing, and an end of line before a CRLF is a match.
func TestReplaceTextCountsTheHitsShown(t *testing.T) {
	sch, ws, dir := openService(t, func(dir string) {
		writeFile(t, dir, "common/empty.txt", []byte("ab\n"))
		writeFile(t, dir, "common/lines.txt", []byte("1\ny\n"))
		writeFile(t, dir, "common/crlf.txt", []byte("foo\r\n"))
	})
	empty := Query{Text: "x*", Regex: true, Include: "empty.txt"}
	for _, query := range []Query{empty, {Text: `1\s+y`, Regex: true, Include: "lines.txt"}} {
		if count, err := sch.ReplaceText(query, "-"); count != 0 || err != nil {
			t.Errorf("replaced %d for %q, error %v", count, query.Text, err)
		}
	}
	for rel, want := range map[string]string{"empty.txt": "ab\n", "lines.txt": "1\ny\n", "crlf.txt": "foo\r\n"} {
		if bytes, _ := os.ReadFile(filepath.Join(dir, "common", rel)); string(bytes) != want {
			t.Errorf("%s: %q", rel, bytes)
		}
	}
	query := Query{Text: "foo$", Regex: true, Include: "crlf.txt"}
	if hits := listHits(t, ws, query, 1); len(hits) != 1 || hits[0].Text != "foo" {
		t.Errorf("hits: %+v", hits)
	}
	if count, err := sch.ReplaceText(query, "bar"); count != 1 || err != nil {
		t.Errorf("replaced %d, error %v", count, err)
	}
	if bytes, _ := os.ReadFile(filepath.Join(dir, "common", "crlf.txt")); string(bytes) != "bar\r\n" {
		t.Errorf("crlf.txt: %q", bytes)
	}
}

// TestSearchFilesListsModsFirst checks that a path matches if it holds the characters in order, that the mods' files
// come first, and that the limit cuts the list.
func TestSearchFilesListsModsFirst(t *testing.T) {
	sch, _, _ := openService(t, func(string) {})
	places := sch.SearchFiles("EVT", 1000)
	if len(places) < 2 || places[0].Origin != fixtureModID {
		t.Fatalf("places: %+v", places)
	}
	vanilla := slices.IndexFunc(places, func(place workspace.Place) bool { return place.Origin == game.OriginVanilla })
	if vanilla < 0 || slices.ContainsFunc(places[vanilla:], func(place workspace.Place) bool {
		return place.Origin == fixtureModID
	}) {
		t.Errorf("the game's files are not after the mods': first of the game at %d", vanilla)
	}
	if shortList := sch.SearchFiles("EVT", 1); len(shortList) != 1 || shortList[0].Path != places[0].Path {
		t.Errorf("short list: %+v", shortList)
	}
	if none := sch.SearchFiles("zzzzzz", 10); len(none) != 0 {
		t.Errorf("a path matched: %+v", none)
	}
}

// TestSearchLeavesOutDeletedFile checks that a file deleted from a mod is neither offered by the file search nor
// searched and replaced in.
func TestSearchLeavesOutDeletedFile(t *testing.T) {
	sch, ws, dir := openService(t, func(dir string) {
		writeFile(t, dir, "common/gone.txt", []byte("needle = 1\n"))
	})
	path := filepath.Join(dir, "common", "gone.txt")
	offered := func() bool {
		return slices.ContainsFunc(sch.SearchFiles("gone", 100), func(place workspace.Place) bool { return place.Path == path })
	}
	if !offered() {
		t.Fatal("the file is not offered before the delete")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := ws.Rescan([]workspace.FileChange{{Path: path, Deleted: true}}); err != nil {
		t.Fatal(err)
	}
	if offered() {
		t.Error("the deleted file is offered")
	}
	query := Query{Text: "needle"}
	if listed := listFiles(ws.Mods.Load(), query); slices.ContainsFunc(listed, func(file files.File) bool {
		return file.Path == path
	}) {
		t.Error("the deleted file is listed")
	}
	if count, err := sch.ReplaceText(query, "x"); count != 0 || err != nil {
		t.Errorf("replaced %d, error %v", count, err)
	}
}
