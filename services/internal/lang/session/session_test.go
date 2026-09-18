// session_test.go covers Reindex, resolution, the buffer overlay, path spelling, EnsureSession, and the lock contract.

package session

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"paradox-modding-tools/services/internal/lang/declared"
	"paradox-modding-tools/services/internal/lang/index"
	"paradox-modding-tools/services/internal/lang/jomini"
)

func must(t *testing.T, ok bool, msg string) {
	t.Helper()
	if !ok {
		t.Fatal(msg)
	}
}

func writeFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// installIndex is an install index with nothing in it but its path, which is what Locate reads for a file no walk has seen.
func installIndex(path string) *index.Index {
	x := index.NewIndex("ck3", &declared.Schema{})
	x.InstallPath = path
	return x
}

func oneMod(root string) []index.ModInput {
	return []index.ModInput{{Origin: "mod", Root: root, Order: 0}}
}

func TestReindex(t *testing.T) {
	t.Run("coalesces", func(t *testing.T) {
		root := writeFiles(t, map[string]string{"common/traits/00.txt": "brave = { category = personality }\n"})
		file := filepath.Join(root, "common", "traits", "00.txt")
		s := New("ws", "ck3", "english", nil, oneMod(root))
		s.DidSave(file)
		s.DidSave(file)
		if n := len(s.DefsInFile(file)); n != 1 {
			t.Fatalf("defs after two saves = %d, want 1", n)
		}
		s.DidChange(file, "other = { category = education }\n")
		if defs := s.DefsInFile(file); len(defs) != 1 || defs[0].Key != "other" {
			t.Fatalf("defs after change = %+v, want other", defs)
		}
	})
	// A file that cannot be read is not a file that was deleted: on Windows a sharing violation during save is the ordinary case.
	t.Run("external change keeps defs when unreadable", func(t *testing.T) {
		root := writeFiles(t, map[string]string{"common/traits/00.txt": "brave = { category = personality }\n"})
		file := filepath.Join(root, "common", "traits", "00.txt")
		s := New("ws", "ck3", "english", nil, oneMod(root))
		must(t, s.Resolve("brave") != nil, "brave not indexed to begin with")
		if err := os.Remove(file); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(file, 0o755); err != nil {
			t.Fatal(err)
		}
		s.externalChange(file)
		must(t, s.Resolve("brave") != nil, "an unreadable file dropped its definitions; stale beats empty")
		if err := os.Remove(file); err != nil {
			t.Fatal(err)
		}
		s.externalChange(file)
		must(t, s.Resolve("brave") == nil, "deleted file kept its definitions")
	})
	t.Run("DidOpen drops disk refs", func(t *testing.T) {
		crlf := "namespace = test\r\n\r\ntest.1 = {\r\n\ttitle = k.t\r\n}\r\n"
		root := writeFiles(t, map[string]string{"events/x.txt": crlf})
		ev := filepath.Join(root, "events", "x.txt")
		s := New("ws", "ck3", "english", nil, oneMod(root))
		s.DidOpen(ev, jomini.Normalize(crlf))
		if n := len(s.RefsTo("k.t")); n != 1 {
			t.Fatalf("k.t refs after DidOpen = %d, want 1", n)
		}
		if runtime.GOOS == "windows" {
			s.DidOpen(strings.ToLower(ev), jomini.Normalize(crlf))
			if n := len(s.ModDefs("test.1")); n != 1 {
				t.Fatalf("defs after DidOpen under another spelling = %d, want 1", n)
			}
		}
	})
}

func TestPathSpelling(t *testing.T) {
	t.Run("DidOpen keeps spelling", func(t *testing.T) {
		root := writeFiles(t, map[string]string{"descriptor.mod": "name = \"t\"\n", "common/traits/Mixed_Case.txt": "brave = { }\n"})
		p := filepath.Join(root, "common", "traits", "Mixed_Case.txt")
		s := New("ws", "ck3", "english", nil, oneMod(root))
		defer s.Close()
		s.DidOpen(p, "brave = { }\n")
		d := s.Resolve("brave")
		if d == nil || d.Path != filepath.Clean(p) {
			t.Fatalf("stored path %+v, want the caller's spelling %q", d, filepath.Clean(p))
		}
	})
	t.Run("canon path", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			a, b := `C:\Steam\steamapps\common\Game\events\x.txt`, `c:\steam\steamapps\common\game\events\x.txt`
			must(t, index.CanonPath(a) == index.CanonPath(b) && index.SamePath(a, b), "Windows folds case")
		} else {
			a := "/tmp/Mod/events/X.txt"
			must(t, index.CanonPath(a) == filepath.Clean(a) && !index.SamePath(a, strings.ToLower(a)), "Unix keeps case")
		}
	})
	t.Run("locate vanilla", func(t *testing.T) {
		install := writeFiles(t, map[string]string{"game/events/x.txt": "x.1 = {}\n", "game/in_game/events/character/foo.txt": "y.1 = {}\n"})
		s := New("ws", "ck3", "english", installIndex(install), nil)
		origin, rel, ok := s.Locate(filepath.Join(install, "game", "events", "x.txt"))
		must(t, ok && origin == "vanilla" && filepath.ToSlash(rel) == "events/x.txt", "ck3 vanilla Locate")
		s5 := New("ws", "eu5", "english", installIndex(install), nil)
		origin, rel, ok = s5.Locate(filepath.Join(install, "game", "in_game", "events", "character", "foo.txt"))
		must(t, ok && origin == "vanilla" && filepath.ToSlash(rel) == "in_game/events/character/foo.txt", "eu5 vanilla Locate")
	})
}

// EnsureSession's lifecycle: single build under concurrent callers, empty id refused, existing session reused, DropAll clears the pool.
func TestEnsureSessionOnce(t *testing.T) {
	var builds atomic.Int32
	var events []string
	pool := NewPool(func(id string) (*Session, error) {
		builds.Add(1)
		return New(id, "ck3", "english", nil, nil), nil
	}, func(event string, _ any) { events = append(events, event) })
	const n = 20
	var wg sync.WaitGroup
	sessions := make([]*Session, n)
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s, err := pool.EnsureSession("ws")
			if err != nil {
				t.Errorf("EnsureSession: %v", err)
				return
			}
			sessions[i] = s
		}(i)
	}
	wg.Wait()
	must(t, builds.Load() == 1, "built more than once")
	for i := 1; i < n; i++ {
		must(t, sessions[i] == sessions[0], "a caller got a different session instance")
	}
	must(t, len(events) == 1 && events[0] == EventReady, "ready event")
	if _, err := pool.EnsureSession(""); err == nil || pool.Get("ws") != sessions[0] {
		t.Fatal("empty id must error and keep the live session")
	}
	pool.DropAll()
	must(t, pool.Get("ws") == nil, "DropAll left a session")
}

// The lock contract under -race: readers hold RLock per request, writers lock themselves.
func TestLockContract(t *testing.T) {
	t.Run("queries", func(t *testing.T) {
		body := "namespace = t\n\nt.1 = {\n\ttitle = k.t\n\timmediate = { trigger_event = t.2 }\n}\nt.2 = { type = character_event }\n"
		root := writeFiles(t, map[string]string{
			"events/x.txt":                          body,
			"localization/english/a_l_english.yml":  "l_english:\n k.t:0 \"Hi\"\n",
			"localization/spanish/zz_l_spanish.yml": "l_spanish:\n k.t:0 \"Hola\"\n",
		})
		ev := filepath.Join(root, "events", "x.txt")
		loc := filepath.Join(root, "localization", "english", "a_l_english.yml")
		cache := installIndex("/game")
		cache.GameVersion = "1.16"
		s := New("ws", "ck3", "english", cache, []index.ModInput{{Origin: "mod", Root: root, Name: "M"}})
		s.DidOpen(loc, "l_english:\n k.t:0 \"Hi\"\n")
		must(t, s.DefaultLang() == "english" && s.Work.Live() >= 2 && s.Resolve("t.1") != nil, "defs")
		must(t, s.ResolveMatching("t.1", func(d index.Def) bool { return d.Kind == "event" }) != nil, "ResolveMatching")
		must(t, len(s.ModDefs("t.1")) > 0 && len(s.FindDefsOf("t.1", "", 10, true, false)) > 0 && len(s.DefsInFile(ev)) > 0, "ModDefs/FindDefsOf/DefsInFile")
		siteFile, _, _, _, locOK := s.LocSite("k.t")
		v, locValOK := s.DefaultLoc("k.t")
		_, locFileOK := s.LocFile("mod", "english")
		must(t, locOK && locValOK && v == "Hi" && locFileOK && index.SamePath(siteFile, loc), "loc site/default/file")
		must(t, len(s.LocKeys("k.", 80)) > 0 && len(s.LocKeys("zzz", 80)) == 0, "LocKeys")
		must(t, len(s.RefsTo("k.t")) > 0 && len(s.RefsInFile(ev)) > 0 && len(s.WorkRefs()) > 0, "refs")
		must(t, len(s.Edges("t.1", true)) > 0 && len(s.Edges("t.2", false)) > 0, "edges")
		origin, _, located := s.Locate(ev)
		must(t, located && origin == "mod" && s.DisplayRel(ev) != "" && s.OriginName("mod") == "M", "Locate")
		must(t, s.OriginName("") == "Crusader Kings III" && s.OriginName("vanilla") == "Crusader Kings III", "OriginName vanilla")
		must(t, len(s.Mods) == 1 && s.KindFor(ev) != "", "Mods/KindFor")
		must(t, s.Install.InstallPath == "/game" && s.Install.GameVersion == "1.16", "install fields")
		_ = s.Contests([]string{"mod"})
		replacement := installIndex("/y")
		s.ReplaceCache(replacement)
		must(t, s.Install.InstallPath == "/y" && s.Resolve("t.1") != nil, "ReplaceCache keeps the workspace")
	})

	t.Run("vanilla defs", func(t *testing.T) {
		vroot := writeFiles(t, map[string]string{"events/v.txt": "v.1 = {}\n"})
		vf := filepath.Join(vroot, "events", "v.txt")
		root := writeFiles(t, map[string]string{"events/x.txt": "v.1 = {}\n"})
		van := installIndex("")
		van.Add(&index.Harvest{Path: vf, Rel: "events/v.txt", Kind: "script", Defs: []index.Def{{Kind: "event", Key: "v.1"}}})
		van.Add(&index.Harvest{Path: "loc.yml", Rel: "loc.yml", Kind: "loc", Loc: index.LocDelta{Lang: "english", Vals: map[string]index.LocEntry{"v.1.t": {Line: 2, Value: "Hi"}}}})
		s := New("ws", "ck3", "english", van, oneMod(root))
		if defs := s.VanillaDefs("v.1"); len(defs) != 1 || defs[0].Kind != "event" || defs[0].Origin != "vanilla" {
			t.Fatalf("script=%v", defs)
		}
		if locs := s.VanillaDefs("v.1.t"); len(locs) != 1 || locs[0].Kind != "loc_key" || locs[0].Path != "loc.yml" {
			t.Fatalf("loc=%v", locs)
		}
	})

	t.Run("concurrent", func(t *testing.T) {
		body := "namespace = t\n\nt.1 = {\n\timmediate = { trigger_event = t.2 }\n}\nt.2 = { type = character_event }\n"
		root := writeFiles(t, map[string]string{"events/x.txt": body})
		ev := filepath.Join(root, "events", "x.txt")
		cache := installIndex("/game")
		cache.Add(&index.Harvest{Path: "/game/e.txt", Rel: "e.txt", Kind: "script", Edges: []index.Edge{{From: "t.1", To: "t.2"}}})
		s := New("ws", "ck3", "english", cache, oneMod(root))
		var wg sync.WaitGroup
		for i := range 16 {
			wg.Add(3)
			go func() {
				defer wg.Done()
				s.RLock()
				defer s.RUnlock()
				_ = s.Edges("t.1", true)
				_ = s.Edges("t.2", false)
				_ = s.Resolve("t.1")
				_ = s.Parsed(ev)
			}()
			go func(i int) {
				defer wg.Done()
				s.DidChange(ev, body+"t.3 = { }\n"+strings.Repeat("#", i%3))
			}(i)
			go func() {
				defer wg.Done()
				s.ReplaceCache(cache)
			}()
		}
		wg.Wait()
	})
}
