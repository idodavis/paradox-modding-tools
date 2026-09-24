// session_test.go covers a workspace over the ck3 fixture install: what edits, saves, closes, disk
// changes and a rescanned install do to the records, the read side, how a path is placed and
// spelled, and the lock contract under -race.
package session

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/index"
)

var scanned = sync.OnceValues(func() (*index.Install, error) {
	dir := filepath.Join("..", "lang", "testdata", "ck3")
	return index.Scan(context.Background(), index.Request{Game: "ck3", Dir: dir, Version: "test", Docs: []string{filepath.Join(dir, "docs")}})
})

// workspace is a session over the fixture install with the fixture mod copied to a temp dir.
func workspace(t *testing.T) (*Session, string) {
	t.Helper()
	install, err := scanned()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS(filepath.Join("..", "lang", "testdata", "mods", "ck3"))); err != nil {
		t.Fatal(err)
	}
	sess, err := New(t.Context(), game.Get("ck3"), install, []Mod{{ID: "mod", Name: "M", Dir: dir, Calendar: Calendar{Era: "AC", EpochOffset: 7782}}}, "")
	if err != nil {
		t.Fatal(err)
	}
	return sess, dir
}

func defs(sess *Session, name string) int {
	n := 0
	for _, definition := range sess.Defs(name) {
		if definition.At.Origin == "mod" {
			n++
		}
	}
	return n
}

func TestEdits(t *testing.T) {
	sess, dir := workspace(t)
	events := filepath.Join(dir, "events", "m_events.txt")
	fresh := filepath.Join(dir, "events", "m_fresh.txt")

	// the editor's text replaces the disk's records, once per distinct text
	sess.DidOpen(fresh, "namespace = f\nf.1 = { type = character_event }\n")
	sess.DidChange(fresh, "namespace = f\nf.1 = { type = character_event }\n")
	if defs(sess, "f.1") != 1 {
		t.Errorf("a file opened with a definition and changed to the same text defines it once: %d", defs(sess, "f.1"))
	}

	// a rescanned install under the workspace keeps the workspace's records and the open buffers
	install, _ := scanned()
	if err := sess.Replace(t.Context(), install); err != nil {
		t.Fatal(err)
	}
	if defs(sess, "m.1") != 1 || defs(sess, "f.1") != 1 || !sess.Workspace.Def("traits", "bold") {
		t.Error("a rescanned install under the workspace keeps the workspace's records and the open buffers")
	}

	sess.DidChange(fresh, "namespace = f\nf.2 = { type = character_event }\n")
	if defs(sess, "f.1") != 0 || defs(sess, "f.2") != 1 {
		t.Error("a change replaces the file's definitions")
	}
	if len(sess.Defs("m.1")) != 1 {
		t.Fatalf("the mod's file on disk is indexed to begin with: %+v", sess.Defs("m.1"))
	}
	sess.DidOpen(strings.ToUpper(events[:1])+events[1:], "namespace = m\nm.9 = { type = character_event }\n")
	if runtime.GOOS == "windows" && (defs(sess, "m.1") != 0 || defs(sess, "m.9") != 1) {
		t.Error("windows: a path opened under another spelling is the same file")
	}
	refs := 0
	for _, ref := range sess.Workspace.Refs {
		if ref.File >= 0 && ref.Name == "v.2" {
			refs++
		}
	}
	if refs != 0 {
		t.Errorf("the disk harvest's references went with its definitions: %d left", refs)
	}

	// on disk behind a closed buffer: an unreadable file keeps its records, a gone file loses them
	sess.DidClose(events)
	if err := os.WriteFile(events, []byte("namespace = m\nm.1 = { type = character_event }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sess.DidSave(events)
	if defs(sess, "m.1") != 1 || defs(sess, "m.9") != 0 {
		t.Error("closing goes back to the disk")
	}
	sess.Changed(filepath.Join(dir, "events", "unreadable.txt"))
	if defs(sess, "m.1") != 1 {
		t.Error("stale beats empty")
	}
	if err := os.Remove(events); err != nil {
		t.Fatal(err)
	}
	sess.Changed(events)
	if defs(sess, "m.1") != 0 {
		t.Error("a deleted file keeps no definitions")
	}
	if len(sess.Defs("brave")) != 2 {
		t.Error("another file's records stay")
	}
}

func TestQueries(t *testing.T) {
	sess, dir := workspace(t)
	events := filepath.Join(dir, "events", "m_events.txt")

	if sess.Language != "english" || len(sess.Mods) != 1 || sess.Mods[0].Name != "M" || sess.Game.Name != "Crusader Kings III" {
		t.Errorf("the session knows its language, its mods and its game: %s %+v", sess.Language, sess.Mods)
	}
	if _, at := sess.Winner(nil); at != -1 {
		t.Error("no definitions, no winner")
	}

	// localization: the mod's before the install's, the asked language before any other
	if err := os.MkdirAll(filepath.Join(dir, "localization", "french"), 0o755); err != nil {
		t.Fatal(err)
	}
	french := filepath.Join(dir, "localization", "french", "m_l_french.yml")
	if err := os.WriteFile(french, []byte("\xEF\xBB\xBFl_french:\n only_french:0 \"Salut\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sess.Changed(french)
	if said, ok := sess.Loc("only_french", "english"); !ok || said.Value != "Salut" {
		t.Errorf("a key with no entry in the asked language falls back to another: %+v", said)
	}
	if _, ok := sess.Loc("nothing_at_all", ""); ok {
		t.Error("a key nothing writes")
	}
	if keys := sess.LocKeys("v.", 2); len(keys) != 2 || !strings.HasPrefix(keys[0], "v.") {
		t.Errorf("LocKeys by prefix, capped: %v", keys)
	}
	if keys := sess.LocKeys("zzz", 5); len(keys) != 0 {
		t.Errorf("no keys start so: %v", keys)
	}

	// text and parse: the open buffer's, else the disk's, parsed once per text
	if text, err := sess.Text(events); err != nil || !strings.Contains(text, "m.1") {
		t.Errorf("the disk's text: %v", err)
	}
	sess.DidOpen(events, "namespace = m\nm.7 = {}\n")
	first, text, _ := sess.Parsed(events)
	again, _, _ := sess.Parsed(events)
	if text != "namespace = m\nm.7 = {}\n" || first != again {
		t.Error("an open buffer is parsed once per text")
	}
	if _, _, err := sess.Parsed(filepath.Join(dir, "nothing.txt")); err == nil {
		t.Error("a file that is nowhere")
	}

	// the caller's spelling of a path is kept, cleaned
	spelled := filepath.Join(dir, "events", "..", "events", "m_events.txt")
	sess.DidOpen(spelled, "namespace = m\nm.1 = { type = character_event }\n")
	if found := sess.Defs("m.1"); len(found) != 1 || found[0].At.Path != events {
		t.Errorf("the definition's path is the caller's spelling, cleaned: %+v", found)
	}
	if runtime.GOOS == "windows" != (index.PathKey(strings.ToLower(events)) == index.PathKey(events)) {
		t.Error("windows folds case; unix keeps it")
	}

	for _, testCase := range []struct {
		what, path, origin, rel string
		kinds                   int
	}{
		{"a mod's file", events, "mod", "events/m_events.txt", 1},
		{"a mod's file nothing has walked", filepath.Join(dir, "common", "traits", "new.txt"), "mod", "common/traits/new.txt", 1},
		{"an install file", sess.Install.Index.Files[0].Path, "vanilla", sess.Install.Index.Files[0].Rel, len(sess.Schema.KindsAt(sess.Install.Index.Files[0].Rel))},
		{"a path under no root", filepath.Join(t.TempDir(), "x.txt"), "", "", 0},
	} {
		place := sess.Where(testCase.path)
		if place.Origin != testCase.origin || place.Rel != testCase.rel || len(place.Kinds) != testCase.kinds {
			t.Errorf("%s: %+v", testCase.what, place)
		}
	}
	if sess.Where(events).Calendar.Era != "AC" || sess.Where(sess.Install.Index.Files[0].Path).Calendar.Era != "" {
		t.Error("a mod's calendar is the mod's alone")
	}

	// EU5: each stage folder is a root of its own (source.Roots), so a rel starts below in_game/
	eu5 := filepath.Join("..", "lang", "testdata", "eu5")
	install, err := index.Scan(t.Context(), index.Request{Game: "eu5", Dir: eu5, Version: "test", Docs: []string{filepath.Join(eu5, "docs")}})
	if err != nil {
		t.Fatal(err)
	}
	eu5Session, err := New(t.Context(), game.Get("eu5"), install, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if place := eu5Session.Where(filepath.Join(eu5, "game", "in_game", "events", "nothing.txt")); place.Origin != "vanilla" || place.Rel != "events/nothing.txt" {
		t.Errorf("eu5: %+v", place)
	}
}

func TestLockContract(t *testing.T) {
	sess, dir := workspace(t)
	if err := sess.Watch(); err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	events := filepath.Join(dir, "events", "m_events.txt")

	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			sess.RLock()
			defer sess.RUnlock()
			sess.Defs("m.1")
			sess.Loc("v.1.t", "")
			sess.Where(events)
			sess.Parsed(events)
			sess.LocKeys("v", 3)
		}()
		go func() {
			defer wg.Done()
			if i%2 == 0 {
				sess.DidChange(events, "namespace = m\nm.1 = { type = character_event }\n")
			} else {
				sess.DidSave(events)
			}
		}()
	}
	wg.Wait()
}
