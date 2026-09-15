// session_test.go covers Reindex, resolution, the buffer overlay, path
// spelling, EnsureSession, and the lock contract.

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
	"paradox-modding-tools/services/internal/lang/derived"
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

func TestReindexCoalesces(t *testing.T) {
	t.Run("coalesces", func(t *testing.T) {
		root := writeFiles(t, map[string]string{
			"common/traits/00.txt": "brave = { category = personality }\n",
		})
		file := filepath.Join(root, "common", "traits", "00.txt")
		s := NewWithLoc("ws", "ck3", "english", nil, nil, []derived.ModInput{
			{Origin: "mod", Root: root, Order: 0},
		})
		s.DidSave(file)
		if n := len(s.DefsInFile(file)); n != 1 {
			t.Fatalf("defs after save = %d, want 1", n)
		}
		s.DidSave(file)
		if n := len(s.DefsInFile(file)); n != 1 {
			t.Fatalf("defs after no-op save = %d, want 1", n)
		}
		s.DidChange(file, "other = { category = education }\n")
		defs := s.DefsInFile(file)
		if len(defs) != 1 || defs[0].Key != "other" {
			t.Fatalf("defs after change = %+v, want other", defs)
		}
	})

	// A file that cannot be read is not a file that was deleted. The watcher
	// fires on a write while the writer still holds the handle, so on Windows a
	// sharing violation during save is the ordinary case — and treating it as
	// deletion dropped every definition in the file out of the model.
	t.Run("external_change_keeps_defs_when_unreadable", func(t *testing.T) {
		root := writeFiles(t, map[string]string{
			"common/traits/00.txt": "brave = { category = personality }\n",
		})
		file := filepath.Join(root, "common", "traits", "00.txt")
		s := NewWithLoc("ws", "ck3", "english", nil, nil, []derived.ModInput{
			{Origin: "mod", Root: root, Order: 0},
		})
		if s.Resolve("brave") == nil {
			t.Fatal("brave not indexed to begin with")
		}

		if err := os.Remove(file); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(file, 0o755); err != nil {
			t.Fatal(err)
		}
		s.externalChange(file)
		if s.Resolve("brave") == nil {
			t.Error("an unreadable file dropped its definitions; stale beats empty")
		}

		// Actually gone is still gone.
		if err := os.Remove(file); err != nil {
			t.Fatal(err)
		}
		s.externalChange(file)
		if d := s.Resolve("brave"); d != nil {
			t.Errorf("deleted file kept its definitions: %+v", d)
		}
	})
}

func TestDidOpenDropsDiskRefs(t *testing.T) {
	t.Run("drops_disk_refs", func(t *testing.T) {
		crlf := "namespace = test\r\n\r\ntest.1 = {\r\n\ttitle = k.t\r\n}\r\n"
		root := writeFiles(t, map[string]string{"events/x.txt": crlf})
		ev := filepath.Join(root, "events", "x.txt")
		s := NewWithLoc("ws", "ck3", "english", nil, nil, []derived.ModInput{
			{Origin: "mod", Root: root, Order: 0},
		})
		s.DidOpen(ev, jomini.Normalize(crlf))
		if n := len(s.RefsTo("k.t")); n != 1 {
			t.Fatalf("k.t refs after DidOpen = %d, want 1", n)
		}
	})

	t.Run("drops_case_mismatch", func(t *testing.T) {
		if runtime.GOOS != "windows" {
			t.Skip("case-insensitive path keys are Windows-only")
		}
		body := "namespace = test\n\ntest.1 = {\n\ttitle = k.t\n}\n"
		root := writeFiles(t, map[string]string{"events/x.txt": body})
		ev := filepath.Join(root, "events", "x.txt")
		s := NewWithLoc("ws", "ck3", "english", nil, nil, []derived.ModInput{
			{Origin: "mod", Root: root, Order: 0},
		})
		if n := len(s.ModDefsOf("test.1")); n != 1 {
			t.Fatalf("harvest defs = %d, want 1", n)
		}
		alt := strings.ToLower(ev)
		if alt == ev {
			alt = strings.ToUpper(ev[:1]) + ev[1:]
			if derived.SamePath(alt, ev) && alt == ev {
				t.Skip("could not build a case-mismatched path")
			}
		}
		s.DidOpen(alt, jomini.Normalize(body))
		if n := len(s.ModDefsOf("test.1")); n != 1 {
			t.Fatalf("defs after DidOpen = %d, want 1", n)
		}
		if n := len(s.RefsTo("k.t")); n != 1 {
			t.Fatalf("refs after DidOpen = %d, want 1", n)
		}
	})
}

func TestPathSpelling(t *testing.T) {
	// The three games phrase a modifier's category list differently: CK3 joins
	// with "and", EU5 with commas plus a blanket "all" on every one of its
	// 2,436 modifiers, Vic3 uses a Mask value.
	t.Run("did_open_keeps_spelling", func(t *testing.T) {
		root := writeFiles(t, map[string]string{
			"descriptor.mod":               "name = \"t\"\n",
			"common/traits/Mixed_Case.txt": "brave = { }\n",
		})
		p := filepath.Join(root, "common", "traits", "Mixed_Case.txt")
		s := NewWithLoc("ws", "ck3", "english", nil, nil,
			[]derived.ModInput{{Origin: "mod", Root: root, Order: 0}})
		defer s.Close()
		s.DidOpen(p, "brave = { }\n")

		d := s.Resolve("brave")
		if d == nil {
			t.Fatal("definition not harvested")
		}
		if d.Path != filepath.Clean(p) {
			t.Fatalf("stored path %q, want the caller's spelling %q", d.Path, filepath.Clean(p))
		}
		// Re-opening under a different spelling must not double-index it.
		s.DidOpen(strings.ToLower(p), "brave = { }\n")
		if got := len(s.ModDefsOf("brave")); got != 1 {
			t.Fatalf("%d definitions after reopening under another spelling, want 1", got)
		}
	})

	t.Run("canon_path_windows_fold", func(t *testing.T) {
		if runtime.GOOS != "windows" {
			t.Skip("Windows path folding")
		}
		a := `C:\Steam\steamapps\common\Game\events\x.txt`
		b := `c:\steam\steamapps\common\game\events\x.txt`
		if derived.CanonPath(a) != derived.CanonPath(b) {
			t.Fatalf("derived.CanonPath(%q) = %q, derived.CanonPath(%q) = %q",
				a, derived.CanonPath(a), b, derived.CanonPath(b))
		}
		if !derived.SamePath(a, b) {
			t.Fatal("SamePath should treat drive/case variants as one file")
		}
	})

	t.Run("canon_path_unix_preserves_case", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("Unix case-sensitive paths")
		}
		a := "/tmp/Mod/events/X.txt"
		if derived.CanonPath(a) != filepath.Clean(a) {
			t.Fatalf("CanonPath = %q, want cleaned original case", derived.CanonPath(a))
		}
		if derived.SamePath(a, strings.ToLower(a)) {
			t.Fatal("SamePath must not fold case on Unix")
		}
	})

	t.Run("locate_vanilla", func(t *testing.T) {
		install := writeFiles(t, map[string]string{
			"game/events/x.txt":                     "x.1 = {}\n",
			"game/in_game/events/character/foo.txt": "y.1 = {}\n",
		})
		ck3 := filepath.Join(install, "game", "events", "x.txt")
		eu5 := filepath.Join(install, "game", "in_game", "events", "character", "foo.txt")

		s := NewWithLoc("ws", "ck3", "english", &derived.VanillaCache{
			InstallPath: install,
		}, nil, nil)
		origin, rel, ok := s.Locate(ck3)
		must(t, ok && origin == "vanilla" && filepath.ToSlash(rel) == "events/x.txt",
			"ck3 vanilla Locate")

		s5 := NewWithLoc("ws", "eu5", "english", &derived.VanillaCache{
			InstallPath: install,
		}, nil, nil)
		origin, rel, ok = s5.Locate(eu5)
		must(t, ok && origin == "vanilla" &&
			filepath.ToSlash(rel) == "in_game/events/character/foo.txt",
			"eu5 vanilla Locate")
	})
}

// EnsureSession's lifecycle: single build under concurrent callers, empty id refused, existing session reused, DropAll clears the pool.
func TestEnsureSessionOnce(t *testing.T) {
	t.Run("builds_once", func(t *testing.T) {
		var builds atomic.Int32
		var events []string
		pool := NewPool(func(id string) (*Session, error) {
			builds.Add(1)
			return NewWithLoc(id, "ck3", "english", nil, nil, nil), nil
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

		if got := builds.Load(); got != 1 {
			t.Fatalf("builds = %d, want 1", got)
		}
		for i := 1; i < n; i++ {
			if sessions[i] != sessions[0] {
				t.Fatalf("caller %d got a different session instance", i)
			}
		}
		if len(events) != 1 || events[0] != EventReady {
			t.Errorf("events = %v, want [%s]", events, EventReady)
		}
	})

	t.Run("empty_id_does_not_drop", func(t *testing.T) {
		pool := NewPool(func(id string) (*Session, error) {
			return NewWithLoc(id, "ck3", "english", nil, nil, nil), nil
		}, nil)
		live, err := pool.EnsureSession("ws")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.EnsureSession(""); err == nil {
			t.Fatal("empty id: want error")
		}
		if pool.Get("ws") != live {
			t.Fatal("empty EnsureSession dropped the live session")
		}
	})

	t.Run("returns_existing_before_drop", func(t *testing.T) {
		var builds atomic.Int32
		pool := NewPool(func(id string) (*Session, error) {
			builds.Add(1)
			return NewWithLoc(id, "ck3", "english", nil, nil, nil), nil
		}, nil)
		first, err := pool.EnsureSession("ws")
		if err != nil {
			t.Fatal(err)
		}
		second, err := pool.EnsureSession("ws")
		if err != nil {
			t.Fatal(err)
		}
		if first != second {
			t.Fatal("second EnsureSession rebuilt the live session")
		}
		if builds.Load() != 1 {
			t.Fatalf("builds = %d, want 1", builds.Load())
		}
	})

	t.Run("drop_all", func(t *testing.T) {
		pool := NewPool(func(id string) (*Session, error) {
			return NewWithLoc(id, "ck3", "english", nil, nil, nil), nil
		}, nil)
		if _, err := pool.EnsureSession("ws"); err != nil {
			t.Fatal(err)
		}
		pool.DropAll()
		if pool.Get("ws") != nil {
			t.Fatal("DropAll left a session")
		}
	})
}

// The lock contract under -race: readers hold RLock per request, writers lock themselves, and two readers may race the lazy via build.
func TestLockContract(t *testing.T) {
	t.Run("queries", func(t *testing.T) {
		body := "namespace = t\n\nt.1 = {\n\ttitle = k.t\n\timmediate = { trigger_event = t.2 }\n}\n" +
			"t.2 = { type = character_event }\n"
		root := writeFiles(t, map[string]string{
			"events/x.txt":                          body,
			"localization/english/a_l_english.yml":  "l_english:\n k.t:0 \"Hi\"\n",
			"localization/spanish/zz_l_spanish.yml": "l_spanish:\n k.t:0 \"Hola\"\n",
		})
		ev := filepath.Join(root, "events", "x.txt")
		loc := filepath.Join(root, "localization", "english", "a_l_english.yml")
		cache := &derived.VanillaCache{
			InstallPath: "/game", GameVersion: "1.16",
			Structures: map[string][]string{"event": {"immediate"}},
			Vocabulary: []string{"add_gold"},
			Schema: &declared.Schema{
				Effects: map[string]declared.EngineToken{"add_gold": {}},
			},
			FieldInfo: map[string]string{"immediate": "runs first"},
		}
		s := NewWithLoc("ws", "ck3", "english", cache, nil, []derived.ModInput{
			{Origin: "mod", Root: root, Name: "M", Order: 0},
		})
		s.DropLocIndex()
		s.DidOpen(loc, "l_english:\n k.t:0 \"Hi\"\n")
		s.DidOpen(filepath.Join(root, "localization", "spanish", "zz_l_spanish.yml"), "l_spanish:\n k.t:0 \"Hola\"\n")

		must(t, s.DefaultLang() == "english" && s.DefCount() >= 2 && s.Resolve("t.1") != nil, "defs")
		must(t, s.ResolveMatching("t.1", func(d derived.Def) bool { return d.Kind == "event" }) != nil,
			"ResolveMatching")
		must(t, len(s.ModDefsOf("t.1")) > 0 && len(s.FindDefsOf("t.1", "", 10, true, false)) > 0 &&
			len(s.DefsInFile(ev)) > 0, "ModDefsOf/FindDefsOf/DefsInFile")
		// LocSite must prefer the default language over a later-sorting spanish file.
		siteFile, _, _, _, locOK := s.LocSite("k.t")
		v, locValOK := s.DefaultLoc("k.t")
		_, locFileOK := s.LocFile("mod", "english")
		must(t, locOK && locValOK && v == "Hi" && locFileOK, "loc site/default/file")
		must(t, derived.SamePath(siteFile, loc), "LocSite language: "+siteFile)
		eachSaw := ""
		s.EachLoc(func(lang, k string, e derived.LocEntry) {
			if lang == "english" && k == "k.t" {
				eachSaw = e.Value
			}
		})
		must(t, len(s.LocKeys("", 80)) > 0 && len(s.LocKeys("k.", 80)) > 0 &&
			len(s.LocKeys("zzz", 80)) == 0 &&
			eachSaw == "Hi", "LocKeys/EachLoc")
		must(t, len(s.RefsTo("k.t")) > 0 && len(s.RefsInFile(ev)) > 0 && len(s.LocRefs()) > 0, "refs")
		must(t, len(s.EdgesFrom("t.1")) > 0 && len(s.EdgesTo("t.2")) > 0, "edges")
		must(t, s.FileText(ev) != "" && s.Parsed(ev).Root != nil, "FileText/Parsed")
		origin, _, located := s.Locate(ev)
		must(t, located && origin == "mod" && s.DisplayRel(ev) != "" && s.OriginName("mod") == "M",
			"Locate")
		must(t, s.OriginName("") == "Crusader Kings III" &&
			s.OriginName("vanilla") == "Crusader Kings III", "OriginName vanilla")
		must(t, len(s.Mods()) == 1 && s.KindFor(ev) != "", "Mods/KindFor")
		kinds, _ := s.GraphCatalog()
		must(t, kinds["t.1"] != "", "GraphCatalog")
		must(t, len(s.Structures("event")) > 0 && len(s.Vocab("vocabulary")) > 0 &&
			s.FieldDoc("immediate", "event", nil) != "", "Structures/Vocab/FieldDoc")
		_, effects, _ := s.MemberSets("event")
		must(t, effects != nil, "MemberSets")
		inst, _, ver, _ := s.CacheInfo()
		must(t, inst == "/game" && ver == "1.16", "CacheInfo")
		_ = s.Contests([]string{"mod"})
		s.ReplaceCache(&derived.VanillaCache{InstallPath: "/y", GameVersion: "2"})
		inst, _, _, _ = s.CacheInfo()
		must(t, inst == "/y", "ReplaceCache")
	})

	t.Run("vanilla_defs", func(t *testing.T) {
		vroot := writeFiles(t, map[string]string{"events/v.txt": "v.1 = {}\n"})
		vf := filepath.Join(vroot, "events", "v.txt")
		root := writeFiles(t, map[string]string{"events/x.txt": "v.1 = {}\n"})
		s := NewWithLoc("ws", "ck3", "english", &derived.VanillaCache{
			Defs: []derived.Def{{Kind: "event", Key: "v.1", Path: vf, Line: 0}},
		}, &derived.VanillaLoc{
			Sites: map[string]derived.LocEntry{
				"v.1.t": {Path: "loc.yml", Line: 2, Value: "Hi"},
			},
		}, []derived.ModInput{{Origin: "mod", Root: root, Order: 0}})
		defs := s.VanillaDefs("v.1")
		if len(defs) != 1 || defs[0].Kind != "event" || defs[0].Origin != "vanilla" {
			t.Fatalf("script=%v", defs)
		}
		locs := s.VanillaDefs("v.1.t")
		if len(locs) != 1 || locs[0].Kind != "loc_key" || locs[0].Path != "loc.yml" {
			t.Fatalf("loc=%v", locs)
		}
	})

	t.Run("concurrent", func(t *testing.T) {
		body := "namespace = t\n\nt.1 = {\n\timmediate = { trigger_event = t.2 }\n}\n" +
			"t.2 = { type = character_event }\n"
		root := writeFiles(t, map[string]string{"events/x.txt": body})
		ev := filepath.Join(root, "events", "x.txt")
		cache := &derived.VanillaCache{
			InstallPath: "/game", GameVersion: "1",
			Edges: []derived.Edge{{From: "t.1", To: "t.2"}},
		}
		s := NewWithLoc("ws", "ck3", "english", cache, nil, []derived.ModInput{
			{Origin: "mod", Root: root, Order: 0},
		})
		var wg sync.WaitGroup
		for i := range 16 {
			wg.Add(3)
			go func() {
				defer wg.Done()
				s.RLock()
				defer s.RUnlock()
				_ = s.EdgesFrom("t.1")
				_ = s.EdgesTo("t.2")
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
