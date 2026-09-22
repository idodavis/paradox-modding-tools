// session_test.go covers a workspace over the ck3 fixture install: what an edit, a save, a close and a change on disk do to the records, how a path is placed and spelled, and the lock contract under -race.
package session

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
	"paradox-modding-tools/services/internal/lang/index"
)

var scanned = sync.OnceValues(func() (*index.Install, error) {
	dir := filepath.Join("..", "testdata", "ck3")
	return index.Scan(context.Background(), index.Request{Game: "ck3", Dir: dir, Version: "test", Docs: []string{filepath.Join(dir, "docs")}})
})

// workspace is a session over the fixture install with the fixture mod copied where the test may edit it.
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
	s, err := New(context.Background(), game.Get("ck3"), install, []Mod{{ID: "mod", Name: "M", Dir: dir, Calendar: Calendar{Era: "AC", EpochOffset: 7782}}}, "")
	if err != nil {
		t.Fatal(err)
	}
	return s, dir
}

func defs(s *Session, name string) int {
	n := 0
	for _, f := range s.Defs(name) {
		if f.At.Origin == "mod" {
			n++
		}
	}
	return n
}

func TestEdits(t *testing.T) {
	s, dir := workspace(t)
	events := filepath.Join(dir, "events", "m_events.txt")
	fresh := filepath.Join(dir, "events", "m_fresh.txt")

	// the editor's text replaces the disk's records, once per distinct text
	s.DidOpen(fresh, "namespace = f\nf.1 = { type = character_event }\n")
	s.DidChange(fresh, "namespace = f\nf.1 = { type = character_event }\n")
	if defs(s, "f.1") != 1 {
		t.Errorf("a file opened with a definition and changed to the same text defines it once: %d", defs(s, "f.1"))
	}
	s.DidChange(fresh, "namespace = f\nf.2 = { type = character_event }\n")
	if defs(s, "f.1") != 0 || defs(s, "f.2") != 1 {
		t.Error("a change replaces the file's definitions")
	}
	if len(s.Defs("m.1")) != 1 {
		t.Fatalf("the mod's file on disk is indexed to begin with: %+v", s.Defs("m.1"))
	}
	s.DidOpen(strings.ToUpper(events[:1])+events[1:], "namespace = m\nm.9 = { type = character_event }\n")
	if runtime.GOOS == "windows" && (defs(s, "m.1") != 0 || defs(s, "m.9") != 1) {
		t.Error("windows: a path opened under another spelling is the same file")
	}
	refs := 0
	for _, r := range s.Workspace.Refs {
		if r.File >= 0 && r.Name == "v.2" {
			refs++
		}
	}
	if refs != 0 {
		t.Errorf("the disk harvest's references went with its definitions: %d left", refs)
	}

	// a change on disk behind a closed buffer: a file that cannot be read keeps its records, a file that is gone loses them
	s.DidClose(events)
	if err := os.WriteFile(events, []byte("namespace = m\nm.1 = { type = character_event }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.DidSave(events)
	if defs(s, "m.1") != 1 || defs(s, "m.9") != 0 {
		t.Error("closing goes back to the disk")
	}
	s.Changed(filepath.Join(dir, "events", "unreadable.txt"))
	if defs(s, "m.1") != 1 {
		t.Error("stale beats empty")
	}
	if err := os.Remove(events); err != nil {
		t.Fatal(err)
	}
	s.Changed(events)
	if defs(s, "m.1") != 0 {
		t.Error("a deleted file keeps no definitions")
	}
	if len(s.Defs("brave")) != 2 {
		t.Error("another file's records stay")
	}
}

func TestPlaces(t *testing.T) {
	s, dir := workspace(t)
	events := filepath.Join(dir, "events", "m_events.txt")

	// the caller's spelling of a path is kept, cleaned
	spelled := filepath.Join(dir, "events", "..", "events", "m_events.txt")
	s.DidOpen(spelled, "namespace = m\nm.1 = { type = character_event }\n")
	if found := s.Defs("m.1"); len(found) != 1 || found[0].At.Path != events {
		t.Errorf("the definition's path is the caller's spelling, cleaned: %+v", found)
	}
	if runtime.GOOS == "windows" != (index.PathKey(strings.ToLower(events)) == index.PathKey(events)) {
		t.Error("windows folds case; unix keeps it")
	}

	for _, tt := range []struct {
		what, path, origin, rel string
		kinds                   int
	}{
		{"a mod's file", events, "mod", "events/m_events.txt", 1},
		{"a mod's file nothing has walked", filepath.Join(dir, "common", "traits", "new.txt"), "mod", "common/traits/new.txt", 1},
		{"an install file", s.Install.Index.Files[0].Path, "vanilla", s.Install.Index.Files[0].Rel, len(s.Schema.KindsAt(s.Install.Index.Files[0].Rel))},
		{"a path under no root", filepath.Join(t.TempDir(), "x.txt"), "", "", 0},
	} {
		place := s.Where(tt.path)
		if place.Origin != tt.origin || place.Rel != tt.rel || len(place.Kinds) != tt.kinds {
			t.Errorf("%s: %+v", tt.what, place)
		}
	}
	if s.Where(events).Calendar.Era != "AC" || s.Where(s.Install.Index.Files[0].Path).Calendar.Era != "" {
		t.Error("a mod's calendar is the mod's alone")
	}

	// EU5: each stage folder is a root of its own (source.Roots), so a rel starts below in_game/
	eu5 := filepath.Join("..", "testdata", "eu5")
	install, err := index.Scan(context.Background(), index.Request{Game: "eu5", Dir: eu5, Version: "test", Docs: []string{filepath.Join(eu5, "docs")}})
	if err != nil {
		t.Fatal(err)
	}
	e, err := New(context.Background(), game.Get("eu5"), install, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if place := e.Where(filepath.Join(eu5, "game", "in_game", "events", "nothing.txt")); place.Origin != "vanilla" || place.Rel != "events/nothing.txt" {
		t.Errorf("eu5: %+v", place)
	}
}

func TestQueries(t *testing.T) {
	s, dir := workspace(t)

	if s.Language != "english" || len(s.Mods) != 1 || s.Mods[0].Name != "M" || s.Game.Name != "Crusader Kings III" {
		t.Errorf("the session knows its language, its mods and its game: %s %+v", s.Language, s.Mods)
	}
	found := s.Defs("brave")
	if len(found) != 2 || found[0].At.Origin != "vanilla" || found[1].At.Origin != "mod" {
		t.Fatalf("a name both define, in load order: %+v", found)
	}
	if kept, at := s.Winner(found); kept.At.Origin != "mod" || at != 1 {
		t.Errorf("the mod's trait wins: %+v", kept)
	}
	if _, at := s.Winner(nil); at != -1 {
		t.Error("no definitions, no winner")
	}

	// localization: the mod's before the install's, the asked language before any other
	if said, ok := s.Loc("v.1.t", ""); !ok || !strings.HasSuffix(said.At.Rel, "v_l_english.yml") || said.Value == "" {
		t.Errorf("the install's key with its file: %+v", said)
	}
	if err := os.MkdirAll(filepath.Join(dir, "localization", "french"), 0o755); err != nil {
		t.Fatal(err)
	}
	french := filepath.Join(dir, "localization", "french", "m_l_french.yml")
	if err := os.WriteFile(french, []byte("\xEF\xBB\xBFl_french:\n only_french:0 \"Salut\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.Changed(french)
	if said, ok := s.Loc("only_french", "english"); !ok || said.Value != "Salut" {
		t.Errorf("a key with no entry in the asked language falls back to another: %+v", said)
	}
	if _, ok := s.Loc("nothing_at_all", ""); ok {
		t.Error("a key nothing writes")
	}
	if keys := s.LocKeys("v.", 2); len(keys) != 2 || !strings.HasPrefix(keys[0], "v.") {
		t.Errorf("LocKeys by prefix, capped: %v", keys)
	}
	if keys := s.LocKeys("zzz", 5); len(keys) != 0 {
		t.Errorf("no keys start so: %v", keys)
	}

	// text and parse: the open buffer's, else the disk's, parsed once per text
	events := filepath.Join(dir, "events", "m_events.txt")
	if text, err := s.Text(events); err != nil || !strings.Contains(text, "m.1") {
		t.Errorf("the disk's text: %v", err)
	}
	s.DidOpen(events, "namespace = m\nm.7 = {}\n")
	first, text, _ := s.Parsed(events)
	again, _, _ := s.Parsed(events)
	if text != "namespace = m\nm.7 = {}\n" || first != again {
		t.Error("an open buffer is parsed once per text")
	}
	if _, _, err := s.Parsed(filepath.Join(dir, "nothing.txt")); err == nil {
		t.Error("a file that is nowhere")
	}
}

func TestReplace(t *testing.T) {
	s, dir := workspace(t)
	fresh := filepath.Join(dir, "events", "m_fresh.txt")
	s.DidOpen(fresh, "namespace = f\nf.1 = { type = character_event }\n")
	install, _ := scanned()
	if err := s.Replace(context.Background(), install); err != nil {
		t.Fatal(err)
	}
	if defs(s, "m.1") != 1 || defs(s, "f.1") != 1 || !s.Workspace.Def("traits", "bold") {
		t.Error("a rescanned install under the workspace keeps the workspace's records and the open buffers")
	}
}

func TestPool(t *testing.T) {
	builds, events := 0, []string{}
	var mu sync.Mutex
	pool := NewPool(func(id string) (*Session, error) {
		mu.Lock()
		builds++
		mu.Unlock()
		s, _ := workspace(t)
		return s, nil
	}, func(event string, data any) {
		mu.Lock()
		events = append(events, event)
		mu.Unlock()
	})

	var wg sync.WaitGroup
	got := make([]*Session, 20)
	for i := range got {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got[i], _ = pool.EnsureSession("ws")
		}()
	}
	wg.Wait()
	if builds != 1 || slices.ContainsFunc(got, func(s *Session) bool { return s != got[0] }) || len(events) != 1 || events[0] != EventReady {
		t.Errorf("one build under concurrent callers, one instance, one ready event: %d builds, %v", builds, events)
	}
	if _, err := pool.EnsureSession(""); err == nil || pool.Get("ws") != got[0] {
		t.Error("an empty id is refused and the live session is untouched")
	}
	pool.DropAll()
	if pool.Get("ws") != nil {
		t.Error("DropAll clears the pool")
	}
}

func TestLockContract(t *testing.T) {
	s, dir := workspace(t)
	if err := s.Watch(); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	events := filepath.Join(dir, "events", "m_events.txt")

	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			s.RLock()
			defer s.RUnlock()
			s.Defs("m.1")
			s.Loc("v.1.t", "")
			s.Where(events)
			s.Parsed(events)
			s.LocKeys("v", 3)
		}()
		go func() {
			defer wg.Done()
			if i%2 == 0 {
				s.DidChange(events, "namespace = m\nm.1 = { type = character_event }\n")
			} else {
				s.DidSave(events)
			}
		}()
	}
	wg.Wait()
}
