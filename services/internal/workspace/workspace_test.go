// session_test.go covers a workspace over the ck3 fixture install: what edits, saves, closes and
// disk changes do to the records, the read side, how a path is placed, and the lock contract under
// -race.

package workspace

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/index"
)

var scanned = sync.OnceValues(func() (*index.Install, error) {
	dir := filepath.Join("..", "testdata", "ck3")
	return index.Scan(context.Background(), index.Request{Game: "ck3", Dir: dir, Version: "test"})
})

// workspace is a session over the fixture install with the fixture mod copied to a temp dir.
func workspace(t *testing.T) (*Session, string) {
	t.Helper()
	install, err := scanned()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS(filepath.Join("..", "testdata", "mods", "ck3"))); err != nil {
		t.Fatal(err)
	}
	sess, err := Open(t.Context(), game.Get("ck3"), install, []Mod{{ID: "mod", Dir: dir, Calendar: Calendar{{Name: "AC", Offset: 7782}}}}, "")
	if err != nil {
		t.Fatal(err)
	}
	return sess, dir
}

func defs(sess *Session, name string) int {
	return len(index.Lookup(sess.Indexes(), "events", name))
}

func TestEdits(t *testing.T) {
	sess, dir := workspace(t)
	events := filepath.Join(dir, "events", "m_events.txt")
	fresh := filepath.Join(dir, "events", "m_fresh.txt")

	// the editor's text replaces the disk's records, once per distinct text
	sess.DidChange(fresh, "namespace = f\nf.1 = { type = character_event }\n")
	sess.DidChange(fresh, "namespace = f\nf.1 = { type = character_event }\n")
	if defs(sess, "f.1") != 1 {
		t.Errorf("a file opened with a definition and changed to the same text defines it once: %d", defs(sess, "f.1"))
	}

	sess.DidChange(fresh, "namespace = f\nf.2 = { type = character_event }\n")
	if defs(sess, "f.1") != 0 || defs(sess, "f.2") != 1 {
		t.Error("a change replaces the file's definitions")
	}
	if len(index.Lookup(sess.Indexes(), "events", "m.1")) != 1 {
		t.Fatalf("the mod's file on disk is indexed to begin with: %+v", index.Lookup(sess.Indexes(), "events", "m.1"))
	}
	sess.DidChange(strings.ToUpper(events[:1])+events[1:], "namespace = m\nm.9 = { type = character_event }\n")
	if runtime.GOOS == "windows" && (defs(sess, "m.1") != 0 || defs(sess, "m.9") != 1) {
		t.Error("windows: a path opened under another spelling is the same file")
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
	if len(index.Lookup(sess.Indexes(), "traits", "brave")) != 2 {
		t.Error("another file's records stay")
	}
}

func TestQueries(t *testing.T) {
	sess, dir := workspace(t)
	events := filepath.Join(dir, "events", "m_events.txt")

	if sess.Language != "english" || len(sess.Mods) != 1 || sess.Mods[0].ID != "mod" || sess.Game.Name != "Crusader Kings III" {
		t.Errorf("the session knows its language, its mods and its game: %s %+v", sess.Language, sess.Mods)
	}
	if winner, _, _, _ := sess.Winner(nil); winner != (index.Def{}) {
		t.Error("no definitions, no winner")
	}

	// localization: a key is found in the asked language
	if err := os.MkdirAll(filepath.Join(dir, "localization", "french"), 0o755); err != nil {
		t.Fatal(err)
	}
	french := filepath.Join(dir, "localization", "french", "m_l_french.yml")
	if err := os.WriteFile(french, []byte("\xEF\xBB\xBFl_french:\n only_french:0 \"Salut\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sess.Changed(french)
	if entry, ok := sess.Loc("only_french", "french"); !ok || entry.Text != "Salut" {
		t.Errorf("a key is found in the asked language: %+v", entry)
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
	sess.DidChange(events, "namespace = m\nm.7 = {}\n")
	first, text, _ := sess.Parsed(events)
	again, _, _ := sess.Parsed(events)
	if text != "namespace = m\nm.7 = {}\n" || first != again {
		t.Error("an open buffer is parsed once per text")
	}
	if _, _, err := sess.Parsed(filepath.Join(dir, "nothing.txt")); err == nil {
		t.Error("a file that is nowhere")
	}

	installFile := sess.Install.Index.Files[0]
	for _, testCase := range []struct {
		what, path, origin, rel string
		defines                 bool
	}{
		{"a mod's file", events, "mod", "events/m_events.txt", true},
		{"a mod's file nothing has walked", filepath.Join(dir, "common", "traits", "new.txt"), "mod", "common/traits/new.txt", true},
		{"an install file", installFile.Path, "vanilla", installFile.Rel, sess.Install.Schema.DatabaseAt(installFile.Rel) != nil},
		{"a path under no root", filepath.Join(t.TempDir(), "x.txt"), "", "", false},
	} {
		place := sess.Where(testCase.path)
		if place.Origin != testCase.origin || place.Rel != testCase.rel || (place.Database != nil) != testCase.defines {
			t.Errorf("%s: %+v", testCase.what, place)
		}
	}
	if len(sess.Where(events).Calendar) != 1 || sess.Where(events).Calendar[0].Name != "AC" || len(sess.Where(installFile.Path).Calendar) != 0 {
		t.Error("a mod's calendar is the mod's alone")
	}

	// EU5: each stage folder is a root of its own (inventory.Roots), so a rel starts below in_game/
	eu5 := filepath.Join("..", "testdata", "eu5")
	install, err := index.Scan(t.Context(), index.Request{Game: "eu5", Dir: eu5, Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	eu5Session, err := Open(t.Context(), game.Get("eu5"), install, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if place := eu5Session.Where(filepath.Join(eu5, "game", "in_game", "events", "nothing.txt")); place.Origin != "vanilla" || place.Rel != "events/nothing.txt" {
		t.Errorf("eu5: %+v", place)
	}
}

func TestLockContract(t *testing.T) {
	sess, dir := workspace(t)
	if err := sess.Watch(func(string) {}); err != nil {
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
			index.Lookup(sess.Indexes(), "events", "m.1")
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
