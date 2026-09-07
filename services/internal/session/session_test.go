// session_test.go covers reindex, resolution, and the exported query surface.

package session

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"paradox-modding-tools/services/internal/catalog"
	"paradox-modding-tools/services/internal/parser/jomini"
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
	root := writeFiles(t, map[string]string{
		"common/traits/00.txt": "brave = { category = personality }\n",
	})
	file := filepath.Join(root, "common", "traits", "00.txt")
	s := NewWithLoc("ws", "ck3", "english", nil, nil, []catalog.ModInput{
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
}

func TestDidOpenDropsDiskRefs(t *testing.T) {
	crlf := "namespace = test\r\n\r\ntest.1 = {\r\n\ttitle = k.t\r\n}\r\n"
	root := writeFiles(t, map[string]string{"events/x.txt": crlf})
	ev := filepath.Join(root, "events", "x.txt")
	s := NewWithLoc("ws", "ck3", "english", nil, nil, []catalog.ModInput{
		{Origin: "mod", Root: root, Order: 0},
	})
	s.DidOpen(ev, jomini.Normalize(crlf))
	if n := len(s.RefsTo("k.t")); n != 1 {
		t.Fatalf("k.t refs after DidOpen = %d, want 1", n)
	}
}

func TestDidOpenDropsCaseMismatch(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("case-insensitive path keys are Windows-only")
	}
	body := "namespace = test\n\ntest.1 = {\n\ttitle = k.t\n}\n"
	root := writeFiles(t, map[string]string{"events/x.txt": body})
	ev := filepath.Join(root, "events", "x.txt")
	s := NewWithLoc("ws", "ck3", "english", nil, nil, []catalog.ModInput{
		{Origin: "mod", Root: root, Order: 0},
	})
	if n := len(s.ModDefsOf("test.1")); n != 1 {
		t.Fatalf("harvest defs = %d, want 1", n)
	}
	alt := strings.ToLower(ev)
	if alt == ev {
		alt = strings.ToUpper(ev[:1]) + ev[1:]
		if SamePath(alt, ev) && alt == ev {
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
}

func TestQueries(t *testing.T) {
	body := "namespace = t\n\nt.1 = {\n\ttitle = k.t\n\timmediate = { trigger_event = t.2 }\n}\n" +
		"t.2 = { type = character_event }\n"
	root := writeFiles(t, map[string]string{
		"events/x.txt":                         body,
		"localization/english/a_l_english.yml": "l_english:\n k.t:0 \"Hi\"\n",
	})
	ev := filepath.Join(root, "events", "x.txt")
	loc := filepath.Join(root, "localization", "english", "a_l_english.yml")
	cache := &catalog.VanillaCache{
		InstallPath: "/game", GameVersion: "1.16",
		Structures: map[string][]string{"event": {"immediate"}},
		Vocabulary: []string{"add_gold"},
		Schema: &catalog.Schema{
			Effects: map[string]catalog.EngineToken{"add_gold": {}},
		},
		FieldInfo: map[string]string{"immediate": "runs first"},
	}
	s := NewWithLoc("ws", "ck3", "english", cache, nil, []catalog.ModInput{
		{Origin: "mod", Root: root, Name: "M", Order: 0},
	})
	s.locByLang = nil
	s.DidOpen(loc, "l_english:\n k.t:0 \"Hi\"\n")

	must(t, s.DefaultLang() == "english" && s.DefCount() >= 2 && s.Resolve("t.1") != nil, "defs")
	must(t, s.ResolveMatching("t.1", func(d catalog.Def) bool { return d.Kind == "event" }) != nil,
		"ResolveMatching")
	must(t, len(s.ModDefsOf("t.1")) > 0 && len(s.FindDefs("t.1", 10, true, false)) > 0 &&
		len(s.DefsInFile(ev)) > 0, "ModDefsOf/FindDefs/DefsInFile")
	_, _, _, locOK := s.LocSite("k.t")
	v, locValOK := s.DefaultLoc("k.t")
	_, locFileOK := s.LocFile("mod", "english")
	must(t, locOK && locValOK && v == "Hi" && locFileOK, "loc site/default/file")
	eachSaw := ""
	s.EachLoc(func(lang, k string, e catalog.LocEntry) {
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
		"locate")
	must(t, s.OriginName("") == "Crusader Kings III" &&
		s.OriginName("vanilla") == "Crusader Kings III", "OriginName vanilla")
	must(t, len(s.Mods()) == 1 && s.KindFor(ev) != "", "Mods/KindFor")
	picker, kinds, _ := s.GraphCatalog(nil)
	must(t, kinds["t.1"] != "" || picker["t.1"] != "", "GraphCatalog")
	must(t, len(s.Structures("event")) > 0 && len(s.Vocab("vocabulary")) > 0 &&
		s.FieldDoc("immediate", "event") != "", "Structures/Vocab/FieldDoc")
	_, effects, _ := s.MemberSets("event")
	must(t, effects != nil, "MemberSets")
	inst, _, ver, _ := s.CacheInfo()
	must(t, inst == "/game" && ver == "1.16", "CacheInfo")
	_ = s.Contests([]string{"mod"})
	s.ReplaceCache(&catalog.VanillaCache{InstallPath: "/y", GameVersion: "2"})
	inst, _, _, _ = s.CacheInfo()
	must(t, inst == "/y", "ReplaceCache")
}

func TestVanillaDefs(t *testing.T) {
	vroot := writeFiles(t, map[string]string{"events/v.txt": "v.1 = {}\n"})
	vf := filepath.Join(vroot, "events", "v.txt")
	root := writeFiles(t, map[string]string{"events/x.txt": "v.1 = {}\n"})
	s := NewWithLoc("ws", "ck3", "english", &catalog.VanillaCache{
		Defs: []catalog.Def{{Kind: "event", Key: "v.1", Path: vf, Line: 0}},
	}, &catalog.VanillaLoc{
		Sites: map[string]catalog.LocEntry{
			"v.1.t": {Path: "loc.yml", Line: 2, Value: "Hi"},
		},
	}, []catalog.ModInput{{Origin: "mod", Root: root, Order: 0}})
	defs := s.VanillaDefs("v.1")
	if len(defs) != 1 || defs[0].Kind != "event" || defs[0].Origin != "vanilla" {
		t.Fatalf("script=%v", defs)
	}
	locs := s.VanillaDefs("v.1.t")
	if len(locs) != 1 || locs[0].Kind != "loc_key" || locs[0].Path != "loc.yml" {
		t.Fatalf("loc=%v", locs)
	}
}

func TestLocateVanilla(t *testing.T) {
	install := writeFiles(t, map[string]string{
		"game/events/x.txt":                     "x.1 = {}\n",
		"game/in_game/events/character/foo.txt": "y.1 = {}\n",
	})
	ck3 := filepath.Join(install, "game", "events", "x.txt")
	eu5 := filepath.Join(install, "game", "in_game", "events", "character", "foo.txt")

	s := NewWithLoc("ws", "ck3", "english", &catalog.VanillaCache{
		InstallPath: install,
	}, nil, nil)
	origin, rel, ok := s.Locate(ck3)
	must(t, ok && origin == "vanilla" && filepath.ToSlash(rel) == "events/x.txt",
		"ck3 vanilla locate")

	s5 := NewWithLoc("ws", "eu5", "english", &catalog.VanillaCache{
		InstallPath: install,
	}, nil, nil)
	origin, rel, ok = s5.Locate(eu5)
	must(t, ok && origin == "vanilla" &&
		filepath.ToSlash(rel) == "in_game/events/character/foo.txt",
		"eu5 vanilla locate")
}

func TestEdgesFromConcurrent(t *testing.T) {
	body := "namespace = t\n\nt.1 = {\n\timmediate = { trigger_event = t.2 }\n}\n" +
		"t.2 = { type = character_event }\n"
	root := writeFiles(t, map[string]string{"events/x.txt": body})
	cache := &catalog.VanillaCache{
		InstallPath: "/game", GameVersion: "1",
		Edges: []catalog.Edge{{From: "t.1", To: "t.2"}},
	}
	s := NewWithLoc("ws", "ck3", "english", cache, nil, []catalog.ModInput{
		{Origin: "mod", Root: root, Order: 0},
	})
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = s.EdgesFrom("")
			_ = s.EdgesTo("t.2")
		}()
		go func() {
			defer wg.Done()
			s.ReplaceCache(cache)
		}()
	}
	wg.Wait()
}

// The three games phrase a modifier's category list differently: CK3 joins with
// "and", EU5 with commas plus a blanket "all" on every one of its 2,436
// modifiers, Vic3 uses a Mask value.
func TestModifierAreas(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"location, all", "location"},
		{"character and province", "character or province"},
		{"Country, , All,", "country"},
		{"all", ""},
		{"", ""},
		{"state/state_region", "state or state region"},
		{"country, country", "country"},
	}
	for _, c := range cases {
		if got := strings.Join(modifierAreas(c.in), " or "); got != c.want {
			t.Errorf("modifierAreas(%q) = %q want %q", c.in, got, c.want)
		}
	}
}

// CanonPath is for comparison, never for a value anything downstream opens.
// reindexLocked used to overwrite the path with it, so every definition
// harvested from an open file carried a lower-cased path on Windows. That
// spelling reached the editor, which keys its tabs by exact URI: go-to-
// definition on a symbol in the file already on screen opened a second tab, and
// reveal-in-explorer went quiet because the workbench first asks whether the
// resource sits inside a workspace folder — and the folders are registered with
// their real casing.
func TestDidOpenKeepsThePathSpelling(t *testing.T) {
	root := writeFiles(t, map[string]string{
		"descriptor.mod":               "name = \"t\"\n",
		"common/traits/Mixed_Case.txt": "brave = { }\n",
	})
	p := filepath.Join(root, "common", "traits", "Mixed_Case.txt")
	s := NewWithLoc("ws", "ck3", "english", nil, nil,
		[]catalog.ModInput{{Origin: "mod", Root: root, Order: 0}})
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
}

// TestResolutionOrder pins the order itself. Every LSP bug found so far was a
// derived source ranked above a declared one, and each fix moved one branch of
// a 200-line if-ladder where the move was invisible in review. Naming the order
// here makes inserting a source a deliberate edit to this list.
func TestResolutionOrder(t *testing.T) {
	want := []string{
		"language-prefix",
		"typed-cite",
		"slot-declared",
		"slot-derived",
		"definition",
		"vocabulary",
		"localization",
		"data-function",
		"macro-kind",
		"field-doc",
		"unresolved",
	}
	if len(wordResolvers) != len(want) {
		t.Fatalf("resolver count = %d, want %d — add it to the list here too",
			len(wordResolvers), len(want))
	}
	for i, w := range want {
		if got := wordResolvers[i].name; got != w {
			t.Errorf("resolver %d = %q, want %q", i, got, w)
		}
	}
}

// TestDeclaredOutranksDerived is the invariant the order exists to hold: what
// the game declares about a slot is consulted before what PMT inferred about
// it. The derived pass carries coverage floors, so it is silent exactly where
// the declared answer was available all along.
func TestDeclaredOutranksDerived(t *testing.T) {
	declared, derived := -1, -1
	for i, r := range wordResolvers {
		switch r.name {
		case "slot-declared":
			declared = i
		case "slot-derived":
			derived = i
		}
	}
	if declared < 0 || derived < 0 {
		t.Fatal("slot resolvers missing")
	}
	if declared > derived {
		t.Errorf("slot-declared at %d ranks below slot-derived at %d", declared, derived)
	}
}

// TestUnresolvedIsLast pins the floor. Every other resolver may decline; this
// one answers for anything left, so a source added below it would never run.
func TestUnresolvedIsLast(t *testing.T) {
	last := wordResolvers[len(wordResolvers)-1]
	if last.name != "unresolved" {
		t.Fatalf("last resolver = %q, want %q", last.name, "unresolved")
	}
	for i, r := range wordResolvers[:len(wordResolvers)-1] {
		if r.name == "unresolved" {
			t.Errorf("unresolved also at %d", i)
		}
	}
}

// TestVocabularyOutranksLocalization pins the fix for the most visible symptom
// of the ordering fault: a declared effect or trigger hovering as the quoted
// player-facing string, because Paradox localizes a great many ordinary words.
func TestVocabularyOutranksLocalization(t *testing.T) {
	vocab, locz := -1, -1
	for i, r := range wordResolvers {
		switch r.name {
		case "vocabulary":
			vocab = i
		case "localization":
			locz = i
		}
	}
	if vocab < 0 || locz < 0 {
		t.Fatal("resolvers missing")
	}
	if vocab > locz {
		t.Errorf("vocabulary at %d ranks below localization at %d", vocab, locz)
	}
}

// covers CanonPath identity on Windows vs Unix.
func TestCanonPathWindowsFold(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "windows" {
		t.Skip("Windows path folding")
	}
	a := `C:\Steam\steamapps\common\Game\events\x.txt`
	b := `c:\steam\steamapps\common\game\events\x.txt`
	if CanonPath(a) != CanonPath(b) {
		t.Fatalf("CanonPath(%q) = %q, CanonPath(%q) = %q",
			a, CanonPath(a), b, CanonPath(b))
	}
	if !SamePath(a, b) {
		t.Fatal("SamePath should treat drive/case variants as one file")
	}
}

func TestCanonPathUnixPreservesCase(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Unix case-sensitive paths")
	}
	a := "/tmp/Mod/events/X.txt"
	if CanonPath(a) != filepath.Clean(a) {
		t.Fatalf("CanonPath = %q, want cleaned original case", CanonPath(a))
	}
	if SamePath(a, strings.ToLower(a)) {
		t.Fatal("SamePath must not fold case on Unix")
	}
}

// verifies that concurrent EnsureSession calls for one id build the
// session exactly once (singleflight) and all callers share it.
func TestEnsureSession(t *testing.T) {
	var builds int32
	var events []string
	pool := NewPool(func(id string) (*Session, error) {
		atomic.AddInt32(&builds, 1)
		return NewWithLoc(id, "ck3", "english", nil, nil, nil), nil
	}, func(event string, _ any) { events = append(events, event) })

	const n = 20
	var wg sync.WaitGroup
	sessions := make([]*Session, n)
	for i := 0; i < n; i++ {
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

	if got := atomic.LoadInt32(&builds); got != 1 {
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
}

func TestEnsureSessionEmptyIDDoesNotDrop(t *testing.T) {
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
}

func TestEnsureSessionReturnsExistingBeforeDrop(t *testing.T) {
	var builds int32
	pool := NewPool(func(id string) (*Session, error) {
		atomic.AddInt32(&builds, 1)
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
	if atomic.LoadInt32(&builds) != 1 {
		t.Fatalf("builds = %d, want 1", atomic.LoadInt32(&builds))
	}
}

func TestDropAll(t *testing.T) {
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
}

// TestDottedSegmentAt covers splitting a scope chain so each hop can be named.
// `holder.culture = { … }` walks to the holder and then to their culture, and a
// reader hovering either half is asking about that half; before this the whole
// string resolved to nothing and no card appeared at all.
func TestDottedSegmentAt(t *testing.T) {
	const chain = "holder.culture"
	cases := []struct {
		off  int
		want string
		ok   bool
	}{
		{0, "holder", true},
		{3, "holder", true},
		{6, "holder", true},  // on the dot: the segment that ends there
		{7, "culture", true}, // first char after it
		{11, "culture", true},
		{len(chain), "culture", true},
	}
	for _, c := range cases {
		got, ok := dottedSegmentAt(chain, c.off)
		if ok != c.ok || got != c.want {
			t.Errorf("dottedSegmentAt(%q, %d) = %q,%v want %q,%v",
				chain, c.off, got, ok, c.want, c.ok)
		}
	}
	// Not a chain: leave it to the resolvers that handle whole names.
	if _, ok := dottedSegmentAt("holder", 2); ok {
		t.Error("a plain name is not a chain")
	}
	if _, ok := dottedSegmentAt("holder.culture", 99); ok {
		t.Error("an offset past the end is not a segment")
	}
}
