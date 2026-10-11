// workspace_test.go covers a workspace over the ck3 fixture install: what edits, saves, closes and
// disk changes do to the records, the read side, how a path is placed, where a fire is written, and
// the lock contract under -race.

package workspace

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/index"
	"paradox-modding-tools/services/internal/schema"
)

// fixtureMod is the id of the fixture mod every session here is built over.
const fixtureMod = "mod"

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
	ws, err := Open(t.Context(), game.Get("ck3"), install, []Mod{{ID: fixtureMod, Dir: dir, Calendar: Calendar{{Name: "AC", Offset: 7782}}}}, "")
	if err != nil {
		t.Fatal(err)
	}
	return ws, dir
}

// defs is how many events of a name the session's indexes define.
func defs(ws *Session, name string) int {
	return len(index.Lookup(ws.Indexes(), schema.Defines, "events", name))
}

// TestEdits covers what the editor's text, a save, a close and a change on disk do to the records.
func TestEdits(t *testing.T) {
	ws, dir := workspace(t)
	events := filepath.Join(dir, "events", "m_events.txt")
	fresh := filepath.Join(dir, "events", "m_fresh.txt")

	// the editor's text replaces the disk's records, once per distinct text
	ws.DidChange(fresh, "namespace = f\nf.1 = { type = character_event }\n")
	ws.DidChange(fresh, "namespace = f\nf.1 = { type = character_event }\n")
	if defs(ws, "f.1") != 1 {
		t.Errorf("a file opened with a definition and changed to the same text defines it once: %d", defs(ws, "f.1"))
	}

	ws.DidChange(fresh, "namespace = f\nf.2 = { type = character_event }\n")
	if defs(ws, "f.1") != 0 || defs(ws, "f.2") != 1 {
		t.Error("a change replaces the file's definitions")
	}
	if len(index.Lookup(ws.Indexes(), schema.Defines, "events", "m.1")) != 1 {
		t.Fatalf("the mod's file on disk is indexed to begin with: %+v", index.Lookup(ws.Indexes(), schema.Defines, "events", "m.1"))
	}
	ws.DidChange(strings.ToUpper(events[:1])+events[1:], "namespace = m\nm.9 = { type = character_event }\n")
	if runtime.GOOS == "windows" && (defs(ws, "m.1") != 0 || defs(ws, "m.9") != 1) {
		t.Error("windows: a path opened under another spelling is the same file")
	}

	// on disk behind a closed buffer: an unreadable file keeps its records, a gone file loses them
	ws.DidClose(events)
	if err := os.WriteFile(events, []byte("namespace = m\nm.1 = { type = character_event }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ws.DidSave(events)
	if defs(ws, "m.1") != 1 || defs(ws, "m.9") != 0 {
		t.Error("closing goes back to the disk")
	}
	ws.DiskChanged(filepath.Join(dir, "events", "unreadable.txt"))
	if defs(ws, "m.1") != 1 {
		t.Error("stale beats empty")
	}
	if err := os.Remove(events); err != nil {
		t.Fatal(err)
	}
	ws.DiskChanged(events)
	if defs(ws, "m.1") != 0 {
		t.Error("a deleted file keeps no definitions")
	}
	if len(index.Lookup(ws.Indexes(), schema.Defines, "traits", "brave")) != 2 {
		t.Error("another file's records stay")
	}
}

// TestQueries covers what the session knows: its language, mods and game, the winner of no definitions, and a key's entry.
func TestQueries(t *testing.T) {
	ws, dir := workspace(t)

	if ws.Language != "english" || len(ws.Mods) != 1 || ws.Mods[0].ID != fixtureMod || ws.Game.Name != "Crusader Kings III" {
		t.Errorf("the session knows its language, its mods and its game: %s %+v", ws.Language, ws.Mods)
	}
	if winner, _, _, _ := ws.Winner(nil); winner != (index.At{}) {
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
	ws.DiskChanged(french)
	if entry, ok := ws.Loc("only_french", "french"); !ok || entry.Text() != "Salut" {
		t.Errorf("a key is found in the asked language: %+v", entry)
	}
	if _, ok := ws.Loc("nothing_at_all", ""); ok {
		t.Error("a key nothing writes")
	}
	if keys := ws.LocKeys("v.", 2); len(keys) != 2 || !strings.HasPrefix(keys[0], "v.") {
		t.Errorf("LocKeys by prefix, capped: %v", keys)
	}
	if keys := ws.LocKeys("zzz", 5); len(keys) != 0 {
		t.Errorf("no keys start so: %v", keys)
	}
}

// TestTextAndParse is a file's text and parse: the open buffer's, else the disk's, parsed once per text.
func TestTextAndParse(t *testing.T) {
	ws, dir := workspace(t)
	events := filepath.Join(dir, "events", "m_events.txt")

	if text, err := ws.Text(events); err != nil || !strings.Contains(text, "m.1") {
		t.Errorf("the disk's text: %v", err)
	}
	ws.DidChange(events, "namespace = m\nm.7 = {}\n")
	first, text, _ := ws.Parsed(events)
	again, _, _ := ws.Parsed(events)
	if text != "namespace = m\nm.7 = {}\n" || first != again {
		t.Error("an open buffer is parsed once per text")
	}
	if _, _, err := ws.Parsed(filepath.Join(dir, "nothing.txt")); err == nil {
		t.Error("a file that is nowhere")
	}

}

// TestPlaceOf is how a path is placed: its origin, its rel, the database its folder defines and its mod's calendar.
func TestPlaceOf(t *testing.T) {
	ws, dir := workspace(t)
	events := filepath.Join(dir, "events", "m_events.txt")
	installFile := ws.Install.Index.Files[0]
	for _, testCase := range []struct {
		what, path, origin, rel string
		defines                 bool
	}{
		{"a mod's file", events, fixtureMod, "events/m_events.txt", true},
		{"a mod's file nothing has walked", filepath.Join(dir, "common", "traits", "new.txt"), fixtureMod, "common/traits/new.txt", true},
		{"an install file", installFile.Path, "vanilla", installFile.Rel, ws.Install.Schema.DatabaseAt(installFile.Rel) != nil},
		{"a path under no root", filepath.Join(t.TempDir(), "x.txt"), "", "", false},
	} {
		place := ws.PlaceOf(testCase.path)
		if place.Origin != testCase.origin || place.Rel != testCase.rel || (place.Database != nil) != testCase.defines {
			t.Errorf("%s: %+v", testCase.what, place)
		}
	}
	if len(ws.PlaceOf(events).Calendar) != 1 || ws.PlaceOf(events).Calendar[0].Name != "AC" || len(ws.PlaceOf(installFile.Path).Calendar) != 0 {
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
	if place := eu5Session.PlaceOf(filepath.Join(eu5, "game", "in_game", "events", "nothing.txt")); place.Origin != "vanilla" || place.Rel != "events/nothing.txt" {
		t.Errorf("eu5: %+v", place)
	}
}

// TestLockContract covers readers and writers together, which -race checks.
func TestLockContract(t *testing.T) {
	ws, dir := workspace(t)
	if err := ws.Watch(func(string) {}); err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	events := filepath.Join(dir, "events", "m_events.txt")

	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			ws.RLock()
			defer ws.RUnlock()
			index.Lookup(ws.Indexes(), schema.Defines, "events", "m.1")
			ws.Loc("v.1.t", "")
			ws.PlaceOf(events)
			_, _, _ = ws.Parsed(events)
			ws.LocKeys("v", 3)
		}()
		go func() {
			defer wg.Done()
			if i%2 == 0 {
				ws.DidChange(events, "namespace = m\nm.1 = { type = character_event }\n")
			} else {
				ws.DidSave(events)
			}
		}()
	}
	wg.Wait()
}

// fireNamed is the fire of a name among fires.
func fireNamed(fires []Fire, name string) (Fire, bool) {
	i := slices.IndexFunc(fires, func(fire Fire) bool { return fire.Name == name })
	if i < 0 {
		return Fire{}, false
	}
	return fires[i], true
}

// TestFireSite covers where a fire is written: its block, the statement that fires and that statement's arguments.
func TestFireSite(t *testing.T) {
	ws, _ := workspace(t)
	first := index.Lookup(ws.Indexes(), schema.Defines, "events", "v.1")[0]
	second := index.Lookup(ws.Indexes(), schema.Defines, "events", "v.2")[0]

	// the first fire written wins by name: the direct one in immediate, the later two fold into it
	fire, ok := fireNamed(ws.Fires(first), "v.2")
	if !ok || fire.Block != "immediate" || fire.Command != "trigger_event" || fire.Args != "" {
		t.Errorf("v.1 fires v.2 from its immediate block: %+v", fire)
	}

	// a list's weight is an argument of the statement that fires
	fire, ok = fireNamed(ws.FiredBy(second, true), "on_birth")
	if !ok || fire.Command != "random_events" || fire.Args != "100" {
		t.Errorf("on_birth fires v.2 through random_events with its weight: %+v", fire)
	}
}
