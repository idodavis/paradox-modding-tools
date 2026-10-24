package index

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/schema"
)

// parseFile parses a file from disk, as the workspace does.
func parseFile(path string) (*jomini.Tree, error) {
	text, err := files.ReadText(path)
	return jomini.Parse(text.Text), err
}

// scanInstall builds the schema of an install, scans the install with it, and returns both and the scan's time.
func scanInstall(t *testing.T, gameID, install string) (*schema.Schema, *Index, time.Duration) {
	t.Helper()
	roots, err := game.ListInstallRoots(install)
	if err != nil {
		t.Fatal(err)
	}
	listing, err := ListFiles(roots)
	if err != nil {
		t.Fatal(err)
	}
	built, err := schema.Build(game.LookupGame(gameID), install, listing, parseFile)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	scanned := Scan(built, roots, listing, func(string) {})
	return built, scanned, time.Since(start)
}

// scanFixture scans the CK3 fixture install.
func scanFixture(t *testing.T) (*schema.Schema, *Index) {
	t.Helper()
	install, err := filepath.Abs(filepath.Join("..", "testdata", "ck3"))
	if err != nil {
		t.Fatal(err)
	}
	built, scanned, _ := scanInstall(t, "ck3", install)
	return built, scanned
}

// findFile returns the position of the file with the given relative path.
func findFile(t *testing.T, index *Index, rel string) int32 {
	t.Helper()
	position := slices.IndexFunc(index.Files, func(file files.File) bool { return file.Rel == rel })
	if position < 0 {
		t.Fatalf("no file %s", rel)
	}
	return int32(position)
}

// TestScanFixtureIndexesFiles checks that the fixture's files are listed once each, and that a script file's
// definitions, a localization message and a table's rows are found.
func TestScanFixtureIndexesFiles(t *testing.T) {
	_, scanned := scanFixture(t)
	keys := map[string]bool{}
	for _, file := range scanned.Files {
		if keys[file.Key] {
			t.Errorf("%s is listed twice", file.Path)
		}
		keys[file.Key] = true
	}
	events := findFile(t, scanned, "events/v_events.txt")
	if count := scanned.CountDefinitions(events)["events"]; count < 2 {
		t.Errorf("events defined in events/v_events.txt: %d", count)
	}
	messages := scanned.ListMessages("v.1.t")
	if len(messages) != 1 || scanned.Text(messages[0]) != "Hello" {
		t.Errorf("message v.1.t: %v", messages)
	}
	rows := findFile(t, scanned, "map_data/definition.csv")
	if scanned.CountDefinitions(rows)["map_data/definition"] == 0 {
		t.Errorf("rows of map_data/definition.csv: %v", scanned.CountDefinitions(rows))
	}
}

// TestResolveReferenceFindsDefinition checks that a reference to an event resolves to the event's definition, with
// its database written and without, and that an unknown name resolves to nothing.
func TestResolveReferenceFindsDefinition(t *testing.T) {
	_, scanned := scanFixture(t)
	for _, database := range []string{"events", ""} {
		sites := scanned.Resolve(schema.Reference{Database: database, Name: "v.2"})
		if len(sites) != 1 || scanned.File(sites[0]).Rel != "events/v_events.txt" {
			t.Errorf("v.2 in %q: %v", database, sites)
		}
	}
	if sites := scanned.Resolve(schema.Reference{Database: "events", Name: "v.404"}); len(sites) != 0 {
		t.Errorf("v.404: %v", sites)
	}
}

// TestListUsesFindsReferences checks that the references to an event are the places the fixture names it, each
// inside a definition.
func TestListUsesFindsReferences(t *testing.T) {
	_, scanned := scanFixture(t)
	uses := scanned.ListUses("events", "v.2")
	if len(uses) < 3 {
		t.Fatalf("uses of v.2: %v", uses)
	}
	for _, use := range uses {
		if parent, ok := scanned.FindDefinition(use); !ok || scanned.Occurrence(parent).Role != RoleDefines {
			t.Errorf("use %v sits inside %v", use, parent)
		}
	}
}

// TestLoadIndexLoadsSavedIndexEqual checks that an index saved and loaded again has the same files, occurrences,
// texts and tables, and answers the same lookups.
func TestLoadIndexLoadsSavedIndexEqual(t *testing.T) {
	_, scanned := scanFixture(t)
	path := filepath.Join(t.TempDir(), "ck3", "index.gob")
	if err := files.SaveGob(path, scanned); err != nil {
		t.Fatal(err)
	}
	loaded, ok := LoadIndex(path)
	if !ok {
		t.Fatal("the saved index did not load")
	}
	if !slices.Equal(loaded.Files, scanned.Files) || !slices.Equal(loaded.Databases.Texts, scanned.Databases.Texts) ||
		!slices.Equal(loaded.Names.Texts, scanned.Names.Texts) {
		t.Error("files or tables differ")
	}
	for position := range scanned.Files {
		if !slices.Equal(loaded.Occurrences[position], scanned.Occurrences[position]) ||
			!slices.Equal(loaded.Texts[position], scanned.Texts[position]) {
			t.Errorf("%s differs", scanned.Files[position].Rel)
		}
	}
	if !slices.Equal(loaded.ListUses("events", "v.2"), scanned.ListUses("events", "v.2")) {
		t.Error("the loaded index finds other uses")
	}
	if position, ok := loaded.FindFile(scanned.Files[3].Path); !ok || position != 3 {
		t.Errorf("file 3 found at %d, %v", position, ok)
	}
}

// TestScanInstalls scans each real install that PMT_TEST_INSTALL_<GAME> names, and prints the scan's time, its
// occurrences by role and the cache's size and times. The counts are read, not asserted.
func TestScanInstalls(t *testing.T) {
	for _, gameID := range []string{"ck3", "vic3", "eu5"} {
		t.Run(gameID, func(t *testing.T) {
			variable := "PMT_TEST_INSTALL_" + strings.ToUpper(gameID)
			install := os.Getenv(variable)
			if install == "" {
				t.Skipf("%s is not set", variable)
			}
			_, scanned, elapsed := scanInstall(t, gameID, install)
			roles := map[Role]int{}
			for _, occurrences := range scanned.Occurrences {
				for _, occurrence := range occurrences {
					roles[occurrence.Role]++
				}
			}
			t.Logf("%d files in %s: %d definitions, %d references, %d messages", len(scanned.Files),
				elapsed.Round(time.Millisecond), roles[RoleDefines], roles[RoleRefers], roles[RoleLocalizes])
			path := filepath.Join(t.TempDir(), "index.gob")
			start := time.Now()
			if err := files.SaveGob(path, scanned); err != nil {
				t.Fatal(err)
			}
			saved := time.Since(start)
			stat, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			start = time.Now()
			if _, ok := LoadIndex(path); !ok {
				t.Fatal("the saved index did not load")
			}
			t.Logf("cache: %d bytes, saved in %s, loaded in %s", stat.Size(), saved.Round(time.Millisecond),
				time.Since(start).Round(time.Millisecond))
		})
	}
}

// scanFixtureMods scans the CK3 fixture mod over the fixture install, the mod replacing the opinion modifiers folder.
func scanFixtureMods(t *testing.T) (*schema.Schema, *Index, *Index) {
	t.Helper()
	built, install := scanFixture(t)
	dir, err := filepath.Abs(filepath.Join("..", "testdata", "mods", "ck3"))
	if err != nil {
		t.Fatal(err)
	}
	roots := []files.Root{{Dir: dir, Origin: "fixture-mod"}}
	mods, err := ScanMods(built, install, roots, [][]string{{"common/opinion_modifiers"}})
	if err != nil {
		t.Fatal(err)
	}
	return built, install, mods
}

// findDefinition returns the site of the definition with the given name in a database.
func findDefinition(t *testing.T, index *Index, database, name string) Site {
	t.Helper()
	sites := index.Resolve(schema.Reference{Database: database, Name: name})
	if len(sites) == 0 {
		t.Fatalf("no %s %s", database, name)
	}
	return sites[0]
}

// TestFindWinnerKeepsByPrecedence checks the definition kept of a trait that the install and the mod both define:
// the last or the first loaded by file name, the highest priority, and an entry mode.
func TestFindWinnerKeepsByPrecedence(t *testing.T) {
	_, install, mods := scanFixtureMods(t)
	sites := mods.Resolve(schema.Reference{Database: "traits", Name: "brave"})
	if len(sites) != 2 || mods.File(sites[0]).Rel != "common/traits/v_traits.txt" ||
		mods.File(sites[1]).Rel != "common/traits/m_traits.txt" {
		t.Fatalf("brave is defined at %v", sites)
	}
	vanilla, modded := sites[0], sites[1]
	last, ok := mods.FindWinner(sites, game.Precedence{Keeps: "last"})
	if !ok || last.Site != vanilla || last.Reason != "order" {
		t.Errorf("last: %+v %v", last, ok)
	}
	first, ok := mods.FindWinner(sites, game.Precedence{Keeps: "first"})
	if !ok || first.Site != modded || first.Reason != "order" {
		t.Errorf("first: %+v %v", first, ok)
	}
	mods.Occurrences[modded.File][modded.Occurrence].Priority = 5
	highest, ok := mods.FindWinner(sites, game.Precedence{Keeps: "highest"})
	if !ok || highest.Site != modded || highest.Reason != "priority" {
		t.Errorf("highest: %+v %v", highest, ok)
	}
	install.Occurrences[vanilla.File][vanilla.Occurrence].Priority = 9
	if highest, ok = mods.FindWinner(sites, game.Precedence{Keeps: "highest"}); !ok || highest.Site != vanilla {
		t.Errorf("a higher priority in the other: %+v %v", highest, ok)
	}
	mods.Occurrences[modded.File][modded.Occurrence].Mode = uint8(mods.Modes.Intern("INJECT"))
	modes := game.Precedence{Keeps: "first", Modes: "entry"}
	// the mod's file loads before the install's, so the install's plain definition replaces what a mode acted on
	injected, ok := mods.FindWinner(sites, modes)
	if !ok || injected.Site != vanilla || injected.Reason != last.Reason || len(injected.Merged) != 1 {
		t.Errorf("inject: %+v %v", injected, ok)
	}
	mods.Occurrences[modded.File][modded.Occurrence].Mode = uint8(mods.Modes.Intern("REPLACE"))
	if replaced, ok := mods.FindWinner(sites, modes); !ok || replaced.Site != vanilla || replaced.Reason != last.Reason {
		t.Errorf("replace: %+v %v", replaced, ok)
	}
}

// TestFindLoadedDropsReplacedFolder checks that the mod's replace_path drops the install's files in that folder
// and no others, and that a file in a folder it does not replace is read.
func TestFindLoadedDropsReplacedFolder(t *testing.T) {
	_, _, mods := scanFixtureMods(t)
	loaded, hidden, ok := mods.FindLoaded("common/opinion_modifiers/v.txt")
	if ok || len(hidden) != 1 || hidden[0].Rel != "common/opinion_modifiers/v.txt" {
		t.Errorf("a replaced folder's file: %v %v %v", loaded.Rel, hidden, ok)
	}
	if loaded, _, ok = mods.FindLoaded("common/traits/v_traits.txt"); !ok || loaded.Origin != game.OriginVanilla {
		t.Errorf("the install's trait file: %v %v", loaded.Rel, ok)
	}
	if loaded, _, ok = mods.FindLoaded("common/traits/m_traits.txt"); !ok || loaded.Origin != "fixture-mod" {
		t.Errorf("the mod's trait file: %v %v", loaded.Rel, ok)
	}
	dropped, _ := mods.FindFile(hidden[0].Path)
	modded, _ := mods.FindFile(loaded.Path)
	if mods.IsLoaded(dropped) || !mods.IsLoaded(modded) {
		t.Error("IsLoaded disagrees with FindLoaded")
	}
}

// TestListFiresNamesEvents checks what an on_action and an event fire, and what fires an event.
func TestListFiresNamesEvents(t *testing.T) {
	built, install := scanFixture(t)
	first := findDefinition(t, install, "events", "v.1")
	second := findDefinition(t, install, "events", "v.2")
	onAction := findDefinition(t, install, "on_action", "on_birth")
	fires := install.ListFires(built, onAction, false)
	if len(fires) != 2 || fires[0].Target != first || fires[1].Target != second {
		t.Errorf("on_birth fires: %+v", fires)
	}
	if fires = install.ListFires(built, first, true); len(fires) < 3 || fires[0].Target != second {
		t.Errorf("v.1 fires: %+v", fires)
	}
	sources := map[Site]bool{}
	for _, fire := range install.ListFiredBy(built, second, true) {
		sources[fire.Source] = true
	}
	if !sources[first] || !sources[onAction] {
		t.Errorf("v.2 is fired by %v", sources)
	}
}
