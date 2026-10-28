package workspace

import (
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/services/app"
)

// The games' ids.
const (
	ck3  = "ck3"
	vic3 = "vic3"
	eu5  = "eu5"
)

// fixtureModID is the id the tests give the fixture mod.
const fixtureModID = "mod-a"

// createApp makes the Wails app that a config's update emits its event through, once for all the tests.
var createApp = sync.OnceFunc(func() { application.New(application.Options{}) })

// warmStages are the stages an open reports when both caches are there: only the mods are scanned.
var warmStages = []string{"mods"}

// resolveFixture returns the absolute path of a folder under internal/testdata.
func resolveFixture(t *testing.T, parts ...string) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join(append([]string{"..", "..", "internal", "testdata"}, parts...)...))
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// copyMod copies a game's fixture mod into a temporary folder, so that a test may change its files, and returns it.
func copyMod(t *testing.T, gameID string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS(resolveFixture(t, "mods", gameID))); err != nil {
		t.Fatal(err)
	}
	return dir
}

// openConfig returns a config file in a temporary folder that records one install of a game and one workspace on it
// with the given mods. The workspace's language is English.
func openConfig(t *testing.T, gameID, installPath string, mods ...app.Mod) *app.ConfigFile {
	t.Helper()
	createApp()
	// the folders PMT uses come from the XDG variables, read again now and once more when the test ends
	for _, variable := range []string{"APPDATA", "LOCALAPPDATA", "XDG_CONFIG_HOME", "XDG_CACHE_HOME"} {
		t.Setenv(variable, t.TempDir())
	}
	config, err := app.OpenConfig()
	if err != nil {
		t.Fatal(err)
	}
	err = config.Update("", func(records *app.Config) error {
		records.Installs = []app.Install{{ID: "i1", Game: gameID, Path: installPath}}
		records.Workspaces = []app.Workspace{{ID: "w1", Name: "Test", Install: "i1", Mods: mods, Language: "english"}}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return config
}

// listOpenStages opens the config's workspace, closes it when the test ends, and returns it with the stages the open
// reported and the time each took. The last timing is the whole open's.
func listOpenStages(t *testing.T, config *app.ConfigFile) (*Session, []string, []string) {
	t.Helper()
	var stages, timings []string
	var stage string
	var started time.Time
	finish := func() {
		if stage != "" {
			timings = append(timings, fmt.Sprintf("%s %s", stage, time.Since(started).Round(time.Millisecond)))
		}
	}
	opening := time.Now()
	wsp := &Service{Config: config, Build: "test"}
	ws, err := wsp.openSession("w1", func(next string, percent int) {
		if percent > 0 {
			return
		}
		finish()
		stage, started = next, time.Now()
		stages = append(stages, next)
	}, func([]FileChange) {})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ws.Close)
	finish()
	return ws, stages, append(timings, fmt.Sprintf("open %s", time.Since(opening).Round(time.Millisecond)))
}

// openFixture opens the CK3 fixture install with a copy of the CK3 fixture mod, whose id is mod-a.
func openFixture(t *testing.T) (*Session, string) {
	t.Helper()
	dir := copyMod(t, ck3)
	config := openConfig(t, ck3, resolveFixture(t, ck3), app.Mod{ID: fixtureModID, Dir: dir})
	ws, _, _ := listOpenStages(t, config)
	return ws, dir
}

// TestOpenLoadsCachesOnSecondOpen checks that a first open scans the install and writes both caches, that a second
// open reads them and scans only the mods, that the caches of other versions are deleted but a file beside them is
// not, and that a mod whose folder is missing is broken and not scanned.
func TestOpenLoadsCachesOnSecondOpen(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "gone")
	config := openConfig(t, ck3, resolveFixture(t, ck3),
		app.Mod{ID: fixtureModID, Dir: copyMod(t, ck3)}, app.Mod{ID: "mod-b", Dir: missing})
	older := filepath.Join(config.Dirs.Cache, ck3, "older")
	stray := filepath.Join(config.Dirs.Cache, ck3, "stray.txt")
	if err := os.MkdirAll(older, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stray, []byte("kept"), 0o644); err != nil {
		t.Fatal(err)
	}
	ws, stages, _ := listOpenStages(t, config)
	if want := []string{"listing", "schema", "indexing", "postings", "mods"}; !slices.Equal(stages, want) {
		t.Errorf("first open stages: %v, want %v", stages, want)
	}
	// the folder is built from the session's own version and dumps, as the install record update builds it
	dumpsAt := ws.Schema.Dumps.WrittenAt.Unix()
	folder := filepath.Join(config.Dirs.Cache, ck3, fmt.Sprintf("%s-%d-test", ws.Schema.Version, dumpsAt))
	for _, name := range []string{schemaFile, indexFile} {
		if _, err := os.Stat(filepath.Join(folder, name)); err != nil {
			t.Errorf("no cache: %v", err)
		}
	}
	if _, err := os.Stat(older); err == nil {
		t.Error("the cache of another version was kept")
	}
	if _, err := os.Stat(stray); err != nil {
		t.Error("a file beside the caches' folder was deleted")
	}
	if len(ws.Order) != 2 || ws.Order[0].Descriptor.Name != "Fixture Mod" || ws.Order[0].Broken || !ws.Order[1].Broken {
		t.Errorf("mods: %+v", ws.Order)
	}
	if mods := ws.Mods.Load(); len(mods.Roots) != len(ws.Install.Roots)+1 || len(mods.Files) <= len(ws.Install.Files) {
		t.Errorf("the mods' index has %d roots and %d files over the install's %d and %d",
			len(mods.Roots), len(mods.Files), len(ws.Install.Roots), len(ws.Install.Files))
	}
	ws.Close()

	warm, stages, _ := listOpenStages(t, config)
	if !slices.Equal(stages, warmStages) {
		t.Errorf("second open stages: %v", stages)
	}
	if len(warm.Install.Files) != len(ws.Install.Files) || len(warm.Schema.Databases) != len(ws.Schema.Databases) {
		t.Error("the caches loaded another install")
	}
}

// TestOpenFailsOnUnknownRecords checks that a workspace, an install or a game that is not recorded is an error.
func TestOpenFailsOnUnknownRecords(t *testing.T) {
	config := openConfig(t, ck3, resolveFixture(t, ck3))
	wsp := &Service{Config: config, Build: "test"}
	if _, err := wsp.openSession("w2", func(string, int) {}, func([]FileChange) {}); err == nil {
		t.Error("an unknown workspace opened")
	}
	err := config.Update("", func(records *app.Config) error {
		records.Workspaces[0].Install = "i2"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wsp.openSession("w1", func(string, int) {}, func([]FileChange) {}); err == nil {
		t.Error("a workspace on an unknown install opened")
	}
	err = config.Update("", func(records *app.Config) error {
		records.Workspaces[0].Install = "i1"
		records.Installs[0].Game = "hoi4"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wsp.openSession("w1", func(string, int) {}, func([]FileChange) {}); err == nil {
		t.Error("an unknown game opened")
	}
}

// TestLocateSaysWhereFileSits checks the place of a mod's file, of a file of the game, of a file the index does not
// hold and of a path outside every root.
func TestLocateSaysWhereFileSits(t *testing.T) {
	ws, dir := openFixture(t)
	events := ws.Locate(ws.Mods.Load(), filepath.Join(dir, "events", "m_events.txt"))
	if events.Rel != "events/m_events.txt" || events.Origin != fixtureModID || events.Root != dir || events.ReadOnly ||
		events.Database != "events" || events.Format != files.FormatScript || !events.Loaded {
		t.Errorf("a mod's file: %+v", events.File)
	}
	vanilla := ws.Locate(ws.Mods.Load(), filepath.Join(resolveFixture(t, ck3), "game", "events", "v_events.txt"))
	if vanilla.Rel != "events/v_events.txt" || vanilla.Origin != game.OriginVanilla || !vanilla.ReadOnly || !vanilla.Loaded {
		t.Errorf("a game's file: %+v", vanilla)
	}
	if fresh := ws.Locate(ws.Mods.Load(), filepath.Join(dir, "events", "m_fresh.txt")); fresh.Root != dir || fresh.Loaded {
		t.Errorf("a file the index lacks: %+v", fresh)
	}
	if outside := ws.Locate(ws.Mods.Load(), filepath.Join(t.TempDir(), "x.txt")); outside.Root != "" || outside.Rel != "" || outside.Key == "" {
		t.Errorf("a path outside the roots: %+v", outside)
	}
}

// TestListRootsPutsModsBeforeGame checks that the explorer's roots are the mods in load order and then the game's
// own folder.
func TestListRootsPutsModsBeforeGame(t *testing.T) {
	ws, dir := openFixture(t)
	want := []files.Root{
		{Dir: dir, Origin: fixtureModID},
		{Dir: filepath.Join(resolveFixture(t, ck3), "game"), Origin: game.OriginVanilla},
	}
	if roots := ws.ListRoots(); !slices.Equal(roots, want) {
		t.Errorf("roots %v, want %v", roots, want)
	}
}

// TestLookupTextPrefersReplaceFolder checks that a message in a replace folder wins over the install's, that the
// base language answers where the session's language has none, that a key nobody writes is not found, and that a file
// a mod replaces gives no message.
func TestLookupTextPrefersReplaceFolder(t *testing.T) {
	ws, dir := openFixture(t)
	mods := ws.Mods.Load()
	text, winner, ok := ws.LookupText(mods, "v_key")
	if !ok || text != "Replaced text" || mods.File(winner.Site).Origin != fixtureModID {
		t.Errorf("v_key: %q %v %v", text, winner, ok)
	}
	if text, winner, ok = ws.LookupText(mods, "v.1.t"); !ok || text != "Hello" || mods.File(winner.Site).Origin != game.OriginVanilla {
		t.Errorf("v.1.t: %q %v %v", text, winner, ok)
	}
	ws.Language = "french"
	if text, _, ok = ws.LookupText(mods, "v_key"); !ok || text != "Replaced text" {
		t.Errorf("v_key in French: %q %v", text, ok)
	}
	if _, _, ok = ws.LookupText(mods, "no_such_key"); ok {
		t.Error("a key nobody writes was found")
	}
	// a mod's file at the install file's path replaces it whole, so the install's messages are no longer loaded
	replacing := filepath.Join(dir, "localization", "english", "v_l_english.yml")
	if err := os.MkdirAll(filepath.Dir(replacing), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(replacing, []byte("l_english:\n other:0 \"Other\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ws.Rescan([]FileChange{{Path: replacing}}); err != nil {
		t.Fatal(err)
	}
	if _, _, ok = ws.LookupText(ws.Mods.Load(), "v.1.t"); ok {
		t.Error("a message of a replaced file was found")
	}
}

// TestLookupTitleReadsTitleKey checks that a definition's title is the message its title key names where it writes
// one, else the message of its name, and a game concept's the message of its name after the concept prefix.
func TestLookupTitleReadsTitleKey(t *testing.T) {
	ws, _ := openFixture(t)
	mods := ws.Mods.Load()
	for _, want := range []struct{ database, name, title string }{
		{"traits", "brave", "Brave"},
		{ws.Schema.Concepts, "v_concept", "Fixture concept"},
		{ws.Schema.Events, "v.1", "Hello"}, // its title key names v.1.t
		{"on_action", "on_birth", ""},      // no title key, and no message of its name
	} {
		if title := ws.LookupTitle(mods, findDefinition(t, mods, want.database, want.name)); title != want.title {
			t.Errorf("title of %s %s: %q, want %q", want.database, want.name, title, want.title)
		}
	}
}

// findDefinition returns the site of the definition with the given name in a database of the mods' index.
func findDefinition(t *testing.T, mods *index.Index, database, name string) index.Site {
	t.Helper()
	for _, site := range mods.ListDefinitions(database) {
		if mods.ToReference(site).Name == name {
			return site
		}
	}
	t.Fatalf("no definition %s in %s", name, database)
	return index.Site{}
}

// TestFindStatementFindsDefinition checks that the statement found for a definition of a mod and of the install is
// the one that writes its name, and that a file the disk lacks is an error.
func TestFindStatementFindsDefinition(t *testing.T) {
	ws, dir := openFixture(t)
	mods := ws.Mods.Load()
	for _, name := range []string{"m.1", "v.1"} {
		tree, statement, err := ws.FindStatement(mods, findDefinition(t, mods, "events", name))
		if err != nil || tree.Text(tree.Statements[statement].Key) != name {
			t.Errorf("%s: statement %d, %v", name, statement, err)
		}
	}
	site := findDefinition(t, mods, "traits", "bold")
	if err := os.Remove(filepath.Join(dir, "common", "traits", "m_traits.txt")); err != nil {
		t.Fatal(err)
	}
	if _, statement, err := ws.FindStatement(mods, site); err == nil {
		t.Errorf("a missing file: statement %d, no error", statement)
	}
}

// TestLoadTreeUsesBufferThenCache checks that a tree comes from the open buffer while there is one, that it is read
// from the file when the buffer is closed, and that it is the same tree the next time.
func TestLoadTreeUsesBufferThenCache(t *testing.T) {
	ws, dir := openFixture(t)
	path := filepath.Join(dir, "events", "m_events.txt")
	ws.SetText(path, "buffered = yes")
	tree, err := ws.LoadTree(path)
	if err != nil || tree.Src != "buffered = yes" {
		t.Fatalf("buffer's tree: %v, %v", tree, err)
	}
	ws.CloseBuffer(path)
	read, err := ws.LoadTree(path)
	if err != nil || read.Src == "buffered = yes" || !strings.HasPrefix(read.Src, "namespace = m") {
		t.Fatalf("tree of the file: %v", err)
	}
	if again, _ := ws.LoadTree(path); again != read {
		t.Error("the file was parsed twice")
	}
	if _, err := ws.LoadTree(filepath.Join(dir, "events", "none.txt")); err == nil {
		t.Error("a missing file loaded")
	}
}

// TestRescanFollowsDisk checks that a new file and a folder copied into a mod each give a new mods' index, and that the
// copy a reader holds does not change. It also checks that the folder is indexed without its hidden folders, and that
// a change to a file of the install does nothing.
func TestRescanFollowsDisk(t *testing.T) {
	ws, dir := openFixture(t)
	before := ws.Mods.Load()
	fresh := filepath.Join(dir, "events", "m_fresh.txt")
	if err := os.WriteFile(fresh, []byte("namespace = m\n\nm.9 = { type = character_event }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ws.Rescan([]FileChange{{Path: fresh}}); err != nil {
		t.Fatal(err)
	}
	after := ws.Mods.Load()
	if after == before || len(after.ListDefinitions("events")) != len(before.ListDefinitions("events"))+1 {
		t.Error("a new file added no definition")
	}
	if _, ok := before.FindFile(fresh); ok {
		t.Error("the index a request holds changed")
	}
	if !ws.Locate(ws.Mods.Load(), fresh).Loaded {
		t.Error("a new file is not loaded")
	}
	copied := filepath.Join(dir, "events", "copied")
	for _, folder := range []string{"deep", ".hidden"} {
		if err := os.MkdirAll(filepath.Join(copied, folder), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(copied, folder, "m_copied.txt"), []byte("x = 1"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := ws.Rescan([]FileChange{{Path: copied}}); err != nil {
		t.Fatal(err)
	}
	if _, ok := ws.Mods.Load().FindFile(filepath.Join(copied, "deep", "m_copied.txt")); !ok {
		t.Error("a folder copied into the mod was not indexed")
	}
	if _, ok := ws.Mods.Load().FindFile(filepath.Join(copied, ".hidden", "m_copied.txt")); ok {
		t.Error("a file in a hidden folder was indexed")
	}
	after = ws.Mods.Load()
	if err := ws.Rescan([]FileChange{{Path: filepath.Join(resolveFixture(t, ck3), "game", "events", "v_events.txt")}}); err != nil ||
		ws.Mods.Load() != after {
		t.Errorf("a file of the install changed the index: %v", err)
	}

}

// TestRescanDropsDeletedFiles checks that a deleted file and a deleted folder leave the mods' index, with the files
// under the folder.
func TestRescanDropsDeletedFiles(t *testing.T) {
	ws, dir := openFixture(t)
	fresh := filepath.Join(dir, "events", "m_fresh.txt")
	if err := os.WriteFile(fresh, []byte("namespace = m\n\nm.9 = { type = character_event }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ws.Rescan([]FileChange{{Path: fresh}}); err != nil {
		t.Fatal(err)
	}
	traits := filepath.Join(dir, "common", "traits")
	if err := os.RemoveAll(traits); err != nil {
		t.Fatal(err)
	}
	if err := ws.Rescan([]FileChange{{Path: traits, Deleted: true}}); err != nil {
		t.Fatal(err)
	}
	if _, ok := ws.Mods.Load().FindFile(filepath.Join(traits, "m_traits.txt")); ok {
		t.Error("a deleted folder kept its file")
	}
	if err := os.Remove(fresh); err != nil {
		t.Fatal(err)
	}
	if err := ws.Rescan([]FileChange{{Path: fresh, Deleted: true}}); err != nil {
		t.Fatal(err)
	}
	if _, ok := ws.Mods.Load().FindFile(fresh); ok {
		t.Error("a deleted file stayed in the index")
	}
}

// TestReadDescriptorReadsEachGamesFixture checks the descriptor of each game's fixture mod, in the game's own form,
// and that a folder with none is an error.
func TestReadDescriptorReadsEachGamesFixture(t *testing.T) {
	want := map[string]Descriptor{
		ck3:  {Version: "1.0", SupportedVersion: "1.*", ReplacePaths: []string{"common/opinion_modifiers"}},
		vic3: {Version: "1.0.0", ReplacePaths: []string{"common/genes"}},
		eu5:  {Version: "1.0.0", ReplacePaths: []string{"in_game/common/town_rights"}},
	}
	for gameID, descriptor := range want {
		dir := resolveFixture(t, "mods", gameID)
		descriptor.Name = "Fixture Mod"
		descriptor.Path = files.Join(dir, game.LookupGame(gameID).ModDescriptor)
		got, err := ReadDescriptor(game.LookupGame(gameID), dir)
		if err != nil || got.Path != descriptor.Path || got.Name != descriptor.Name || got.Version != descriptor.Version ||
			got.SupportedVersion != descriptor.SupportedVersion || !slices.Equal(got.ReplacePaths, descriptor.ReplacePaths) ||
			got.WorkshopID != "" {
			t.Errorf("%s: %+v, %v", gameID, got, err)
		}
	}
	if _, err := ReadDescriptor(game.LookupGame(ck3), t.TempDir()); err == nil {
		t.Error("a folder with no descriptor was read")
	}
}

// watchReports watches a folder until the test ends and returns a function that gives, for each path, the reports so
// far: whether each said the path was deleted.
func watchReports(t *testing.T, dir string) func() map[string][]bool {
	t.Helper()
	var mu sync.Mutex
	reports := map[string][]bool{}
	watcher, err := startWatcher([]string{dir}, func(changes []FileChange) {
		mu.Lock()
		defer mu.Unlock()
		for _, change := range changes {
			reports[change.Path] = append(reports[change.Path], change.Deleted)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(watcher.Close)
	return func() map[string][]bool {
		mu.Lock()
		defer mu.Unlock()
		return maps.Clone(reports)
	}
}

// writeFile writes a small file, failing the test if it cannot.
func writeFile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("x = 1"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestStartWatcherReportsChangeOnce checks that two writes of a file are reported once, that a file in a folder made
// later is reported, and that a removed file is reported as deleted.
func TestStartWatcherReportsChangeOnce(t *testing.T) {
	dir := t.TempDir()
	reports := watchReports(t, dir)
	file := filepath.Join(dir, "a.txt")
	writeFile(t, file)
	writeFile(t, file)
	later := filepath.Join(dir, "later")
	if err := os.Mkdir(later, 0o755); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	inside := filepath.Join(later, "b.txt")
	writeFile(t, inside)
	time.Sleep(500 * time.Millisecond)
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	if got := reports()[file]; !slices.Equal(got, []bool{false, true}) {
		t.Errorf("reports of a file written twice and removed: %v", got)
	}
	if got := reports()[inside]; len(got) != 1 || got[0] {
		t.Errorf("reports of a file in a folder made later: %v", got)
	}
}

// TestStartWatcherSkipsHiddenFolders checks that nothing under a dot folder is reported, whether the folder was there
// at the start or made later, except under .metadata, which is watched.
func TestStartWatcherSkipsHiddenFolders(t *testing.T) {
	dir := t.TempDir()
	hidden, metadata := filepath.Join(dir, ".hidden"), filepath.Join(dir, ".metadata")
	for _, folder := range []string{hidden, metadata} {
		if err := os.Mkdir(folder, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	reports := watchReports(t, dir)
	hiddenLater := filepath.Join(dir, ".later")
	if err := os.Mkdir(hiddenLater, 0o755); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	for _, folder := range []string{hidden, metadata, hiddenLater} {
		writeFile(t, filepath.Join(folder, "f.txt"))
	}
	time.Sleep(500 * time.Millisecond)
	got := reports()
	for _, path := range []string{hidden, hiddenLater, filepath.Join(hidden, "f.txt"), filepath.Join(hiddenLater, "f.txt")} {
		if len(got[path]) != 0 {
			t.Errorf("reports of %s, a hidden folder or a file in one: %v", path, got[path])
		}
	}
	if len(got[filepath.Join(metadata, "f.txt")]) != 1 {
		t.Errorf("reports of a file in .metadata: %v", got)
	}
}

// TestWriteDescriptorKeepsOtherFields checks that a written descriptor reads back the same in each of the games'
// forms, and that the keys it does not model, and the thumbnail's folder, are kept.
func TestWriteDescriptorKeepsOtherFields(t *testing.T) {
	changed := Descriptor{
		Name: "Changed", Version: "2.0", SupportedVersion: "1.*", Tags: []string{"Gameplay", "Quoted tag"},
		ReplacePaths: []string{"common/traits", "events"}, WorkshopID: "123",
	}
	for gameID, kept := range map[string]string{ck3: "replace_path", vic3: `"multiplayer_synchronized": true`, eu5: `"id": "fixture.mod"`} {
		row := game.LookupGame(gameID)
		dir := copyMod(t, gameID)
		changed.Thumbnail = files.Join(dir, path.Join(row.ThumbnailDir, "thumb.png"))
		want := changed
		want.Path = files.Join(dir, row.ModDescriptor)
		if gameID != ck3 {
			want.WorkshopID, want.ID = "", "fixture.mod"
		}
		if gameID == ck3 {
			// a key the descriptor does not model, written by hand
			text, err := os.ReadFile(want.Path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(want.Path, append(text, []byte("\nunmodeled=\"kept\"\n")...), 0o644); err != nil {
				t.Fatal(err)
			}
			kept = `unmodeled="kept"`
		}
		if err := WriteDescriptor(row, dir, changed); err != nil {
			t.Fatal(err)
		}
		got, err := ReadDescriptor(row, dir)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("%s: read back %+v, %v, want %+v", gameID, got, err, want)
		}
		if text, _ := os.ReadFile(want.Path); !strings.Contains(string(text), kept) {
			t.Errorf("%s: the descriptor lost %s:\n%s", gameID, kept, text)
		}
	}
}

// TestCreateModFolderMakesSkeleton checks that a new mod gets its folder, the game's folders and a descriptor with
// version 1.0 and the supported release of the game version, that CK3 also gets the launcher's file beside the
// folder, and that an existing folder is an error.
func TestCreateModFolderMakesSkeleton(t *testing.T) {
	versions := map[string]string{ck3: "1.20.1.2", eu5: "build 12345"}
	supported := map[string]string{ck3: "1.20.*", eu5: ""}
	for _, gameID := range []string{ck3, eu5} {
		row := game.LookupGame(gameID)
		holder := filepath.Join(t.TempDir(), "mod")
		made, err := createModFolder(row, holder, "My New Mod", versions[gameID])
		folder := filepath.Join(holder, "my_new_mod")
		if err != nil || made != folder {
			t.Fatalf("%s: %q, %v", gameID, made, err)
		}
		for _, rel := range row.ModFolders {
			if stat, err := os.Stat(files.Join(folder, rel)); err != nil || !stat.IsDir() {
				t.Errorf("%s: no folder %s", gameID, rel)
			}
		}
		if read, err := ReadDescriptor(row, folder); err != nil || read.Name != "My New Mod" || read.Version != "1.0" ||
			read.SupportedVersion != supported[gameID] || read.Path != files.Join(folder, row.ModDescriptor) {
			t.Errorf("%s: descriptor %+v, %v", gameID, read, err)
		}
		launcher, err := os.ReadFile(filepath.Join(holder, "my_new_mod.mod"))
		if hasLauncher := err == nil; hasLauncher != (gameID == ck3) || hasLauncher &&
			(!strings.Contains(string(launcher), `path="`+filepath.ToSlash(folder)+`"`) ||
				!strings.Contains(string(launcher), `supported_version="1.20.*"`)) {
			t.Errorf("%s: launcher file %q, %v", gameID, launcher, err)
		}
		if _, err := createModFolder(row, holder, "My New Mod", versions[gameID]); err == nil {
			t.Errorf("%s: a folder that exists was made again", gameID)
		}
	}
	for version, want := range map[string]string{"1.3.11": "1.3.*", "1.20": "1.20.*", "build 7": "", "1.x": "", "": ""} {
		if got := toSupportedVersion(version); got != want {
			t.Errorf("supported version of %q: %q, want %q", version, got, want)
		}
	}
}

// TestServiceReadsOpenWorkspace checks the service's methods that read: with no workspace open they answer with
// nothing, and with one open they list a folder with its folders first and its binaries hidden by the setting, read a
// file with its place, and serve the files under the roots, a texture as PNG, and refuse the others.
func TestServiceReadsOpenWorkspace(t *testing.T) {
	config := openConfig(t, ck3, resolveFixture(t, ck3), app.Mod{ID: fixtureModID, Dir: copyMod(t, ck3)})
	wsp := &Service{Config: config}
	if _, err := wsp.Session(); err == nil || wsp.State().Open || wsp.ListRoots() != nil || wsp.Locate("x").Root != "" {
		t.Error("a service with no workspace answered")
	}
	ws, _, _ := listOpenStages(t, config)
	wsp.session = ws
	if state := wsp.State(); !state.Open || state.ID != "w1" || state.Game.ID != ck3 || len(state.Mods) != 1 || state.Install.ID != "i1" {
		t.Errorf("state %+v", state)
	}
	mod := ws.Order[0].Dir
	children, err := wsp.ListFolder(mod)
	if err != nil || len(children) != 5 || !children[0].Place.IsDir || children[4].Name != "descriptor.mod" ||
		children[4].Place.IsDir {
		t.Errorf("mod's entries %+v, %v", children, err)
	}
	fonts := filepath.Join(resolveFixture(t, ck3), "game", "fonts")
	if children, err = wsp.ListFolder(fonts); err != nil || len(children) != 2 {
		t.Errorf("fonts with binaries hidden: %+v, %v", children, err)
	}
	err = config.Update("", func(records *app.Config) error {
		records.Workspaces[0].ShowBinaries = true
		return nil
	})
	if children, _ = wsp.ListFolder(fonts); err != nil || len(children) != 3 {
		t.Errorf("fonts with binaries shown: %+v, %v", children, err)
	}
	read, err := wsp.Read(filepath.Join(mod, "events", "m_events.txt"))
	if err != nil || read.Place.Origin != fixtureModID || !strings.HasPrefix(read.Text.Text, "namespace = m") {
		t.Errorf("read %+v, %v", read.Place, err)
	}
	serve := func(path string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		wsp.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/?path="+url.QueryEscape(path), nil))
		return recorder
	}
	if served := serve(filepath.Join(mod, "descriptor.mod")); served.Code != http.StatusOK || !strings.Contains(served.Body.String(), "Fixture Mod") {
		t.Errorf("a mod's file: %d", served.Code)
	}
	texture := serve(filepath.Join(resolveFixture(t, ck3), "game", "gfx", "interface", "v_bg.dds"))
	if texture.Code != http.StatusOK || texture.Header().Get("Content-Type") != "image/png" || !strings.HasPrefix(texture.Body.String(), "\x89PNG") {
		t.Errorf("a texture: %d %s %s", texture.Code, texture.Header().Get("Content-Type"), texture.Body.String())
	}
	if outside := serve(filepath.Join(t.TempDir(), "x.txt")); outside.Code != http.StatusForbidden {
		t.Errorf("a path outside the roots: %d", outside.Code)
	}
}

// TestServiceRefusesGameFiles checks that the methods that change files refuse the game's own, and leave them and
// the mod's files as they were.
func TestServiceRefusesGameFiles(t *testing.T) {
	config := openConfig(t, ck3, resolveFixture(t, ck3), app.Mod{ID: fixtureModID, Dir: copyMod(t, ck3)})
	ws, _, _ := listOpenStages(t, config)
	wsp := &Service{Config: config, session: ws}
	mod := ws.Order[0].Dir
	vanilla := filepath.Join(resolveFixture(t, ck3), "game", "events", "v_events.txt")
	mine := filepath.Join(mod, "events", "m_events.txt")
	_, deleteErr := wsp.Delete(vanilla)
	_, deleteRootErr := wsp.Delete(filepath.Join(resolveFixture(t, ck3), "game"))
	refusals := map[string]error{
		"Write":         wsp.Write(vanilla, files.Text{}),
		"Create":        wsp.Create(filepath.Join(filepath.Dir(vanilla), "new.txt"), false),
		"Rename from":   wsp.Rename(vanilla, filepath.Join(mod, "moved.txt")),
		"Rename to":     wsp.Rename(mine, filepath.Join(filepath.Dir(vanilla), "moved.txt")),
		"Delete":        deleteErr,
		"Delete a root": deleteRootErr,
	}
	for method, err := range refusals {
		if err == nil || !strings.Contains(err.Error(), "game's own files") {
			t.Errorf("%s of the game's files: %v", method, err)
		}
	}
	for _, path := range []string{vanilla, mine} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("a refused change touched %s: %v", path, err)
		}
	}
}

// TestReorderModsRefusesRepeatedMod checks that an order is refused unless it names each of the workspace's mods
// exactly once, and that a refused order leaves the record as it was.
func TestReorderModsRefusesRepeatedMod(t *testing.T) {
	config := openConfig(t, ck3, resolveFixture(t, ck3), app.Mod{ID: "a", Dir: copyMod(t, ck3)}, app.Mod{ID: "b", Dir: copyMod(t, ck3)})
	wsp := &Service{Config: config}
	for _, order := range [][]string{{"a", "a"}, {"a"}, {"a", "b", "c"}, {"a", "b", "b"}, nil} {
		if err := wsp.ReorderMods("w1", order); err == nil {
			t.Errorf("the order %v was accepted", order)
		}
	}
	if mods := config.Config().Workspaces[0].Mods; len(mods) != 2 || mods[0].ID != "a" || mods[1].ID != "b" {
		t.Errorf("a refused order changed the mods: %+v", mods)
	}
}

// TestResolveSlotFindsFreeSlot checks that a new mod gets the first color slot no mod has, and that when every slot is
// taken the slot follows the mods' count around.
func TestResolveSlotFindsFreeSlot(t *testing.T) {
	var mods []app.Mod
	for number := 1; number <= 16; number++ {
		want := fmt.Sprintf("o%d", number)
		if slot := resolveSlot(mods); slot != want {
			t.Errorf("with %d mods the slot is %s, want %s", len(mods), slot, want)
		}
		mods = append(mods, app.Mod{Color: want})
	}
	if slot := resolveSlot(mods); slot != "o1" {
		t.Errorf("with sixteen mods the slot is %s, want o1", slot)
	}
	if slot := resolveSlot(append(mods, app.Mod{Color: "#ff0000"}, app.Mod{Color: "#00ff00"})); slot != "o3" {
		t.Errorf("with eighteen mods the slot is %s, want o3", slot)
	}
	if slot := resolveSlot([]app.Mod{{Color: "o1"}, {Color: "#123456"}, {Color: "o3"}}); slot != "o2" {
		t.Errorf("with a gap the slot is %s, want o2", slot)
	}
}

// TestOpenInstalls opens each real install that PMT_TEST_INSTALL_<GAME> names with that game's fixture mod, first
// with no caches and then from them. It checks that the second open scans only the mods.
func TestOpenInstalls(t *testing.T) {
	for _, gameID := range []string{ck3, vic3, eu5} {
		t.Run(gameID, func(t *testing.T) {
			variable := "PMT_TEST_INSTALL_" + strings.ToUpper(gameID)
			install := os.Getenv(variable)
			if install == "" {
				t.Skipf("%s is not set", variable)
			}
			config := openConfig(t, gameID, install, app.Mod{ID: fixtureModID, Dir: resolveFixture(t, "mods", gameID)})
			cold, stages, timings := listOpenStages(t, config)
			t.Logf("cold open: stages %v, times %v", stages, timings)
			for _, root := range cold.Install.Roots {
				t.Logf("install root %s (%s)", root.Dir, root.Origin)
			}
			t.Logf("cold: install index %d files, mods' index %d files", len(cold.Install.Files), len(cold.Mods.Load().Files))
			cold.Close()
			warm, stages, timings := listOpenStages(t, config)
			t.Logf("warm open: stages %v, times %v", stages, timings)
			t.Logf("warm: install index %d files, mods' index %d files", len(warm.Install.Files), len(warm.Mods.Load().Files))
			if !slices.Equal(stages, warmStages) || len(warm.Install.Files) != len(cold.Install.Files) {
				t.Errorf("the warm open scanned %v and holds %d files", stages, len(warm.Install.Files))
			}
		})
	}
}
