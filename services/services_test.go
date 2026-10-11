// services_test.go covers the services that stay in the package: the config, the log, the live session, the
// workspace service and the file service, over one scan of the ck3 fixture install and a config in a temporary
// folder that APPDATA, XDG_CONFIG_HOME and HOME are sent to, so no test writes into the real user folders. It also
// holds the sweep, the engine's scoreboard over every mod of a game's mod folder, which runs only behind
// PMT_TEST_SWEEP (`task test:sweep`).

package services

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"paradox-modding-tools/services/internal/appdata"
	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/index"
	"paradox-modding-tools/services/internal/inventory"
	"paradox-modding-tools/services/internal/lsp"
	"paradox-modding-tools/services/internal/pages"
	"paradox-modding-tools/services/internal/schema"
	"paradox-modding-tools/services/internal/wiki"
	"paradox-modding-tools/services/internal/workspace"
)

// The games the tests build workspaces for, the install the config tests name, and the format a binary file reads as.
const (
	gameCK3      = "ck3"
	gameEU5      = "eu5"
	installOne   = "inst1"
	formatBinary = "binary"
)

var scanned = sync.OnceValues(func() (*index.Install, error) {
	dir := filepath.Join("internal", "testdata", gameCK3)
	return index.Scan(context.Background(), index.Request{Game: gameCK3, Dir: dir, Version: "test"})
})

// tempConfig sends APPDATA, XDG_CONFIG_HOME and HOME to a temporary folder and opens a config.json
// there, unwritten, holding the given installs and workspaces.
func tempConfig(t *testing.T, installs []GameInstall, workspaces ...Workspace) *ConfigFile {
	t.Helper()
	home := t.TempDir()
	t.Setenv("APPDATA", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("HOME", home)
	config := openConfigFile(filepath.Join(home, "config.json"))
	config.cfg.Installs, config.cfg.Workspaces = installs, workspaces
	return config
}

// writeTree writes each text at its slash-separated rel under root.
func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, text := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// dirNames is the names of a directory listing, as a set.
func dirNames(entries []DirEntry) map[string]bool {
	out := map[string]bool{}
	for _, entry := range entries {
		out[entry.Name] = true
	}
	return out
}

// fixtureService is a session service over a config holding the ck3 fixture install and one
// workspace with one mod, its cache folder sent to a temporary directory.
func fixtureService(t *testing.T) *SessionService {
	t.Helper()
	install, _ := filepath.Abs(filepath.Join("internal", "testdata", gameCK3))
	mod := t.TempDir()
	if err := os.CopyFS(mod, os.DirFS(filepath.Join("internal", "testdata", "mods", gameCK3))); err != nil {
		t.Fatal(err)
	}
	config := tempConfig(t,
		[]GameInstall{{ID: "i1", GameID: gameCK3, Name: "CK3", Path: install}},
		Workspace{ID: "ws", GameID: gameCK3, Name: "W", InstallID: "i1", Mods: []WorkspaceMod{{ID: "m", Name: "M", Path: mod}}})
	sessions := NewSessionService(config, "test", wiki.New(t.TempDir()))
	sessions.scan = func(context.Context, index.Request) (*index.Install, error) { return scanned() }
	return sessions
}

// A config that cannot be read is not a first run: serving it empty and then saving would write
// nothing over every install, workspace and mod.
func TestConfig(t *testing.T) {
	t.Run("unreadable config refuses to write", func(t *testing.T) {
		path := tempConfig(t, nil).path
		body := `{"installs":"not-an-array"}`
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		config := openConfigFile(path)
		if config.loadErr == nil {
			t.Fatal("want loadErr set for a corrupt config")
		}
		if err := config.mutate("", func(cfg *Config) error {
			cfg.Installs = append(cfg.Installs, GameInstall{ID: "new"})
			return nil
		}); err == nil {
			t.Fatal("mutate must refuse while the config is unreadable")
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(raw) != body {
			t.Fatalf("config was overwritten:\n got %s\nwant %s", raw, body)
		}
	})

	t.Run("missing config is a first run", func(t *testing.T) {
		config := tempConfig(t, nil)
		if config.loadErr != nil {
			t.Fatalf("absent config is not a failure: %v", config.loadErr)
		}
		if err := config.mutate("", func(cfg *Config) error {
			cfg.Installs = append(cfg.Installs, GameInstall{ID: "i1", Name: "CK3"})
			return nil
		}); err != nil {
			t.Fatalf("mutate on first run: %v", err)
		}
		var installs int
		openConfigFile(config.path).read(func(cfg *Config) { installs = len(cfg.Installs) })
		if installs != 1 {
			t.Fatalf("got %d installs", installs)
		}
	})
}

// TestListDirectoryMetadata covers what a directory listing hides: dotfiles, but not the metadata folder.
func TestListDirectoryMetadata(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".metadata"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := &FileService{}
	hidden, err := svc.ListDirectory(root, false)
	if err != nil {
		t.Fatal(err)
	}
	names := dirNames(hidden)
	if !names[".metadata"] || !names["a.txt"] || names[".git"] {
		t.Fatalf("hide binaries list=%v", names)
	}
	all, err := svc.ListDirectory(root, true)
	if err != nil {
		t.Fatal(err)
	}
	names = dirNames(all)
	if !names[".git"] || !names[".metadata"] {
		t.Fatalf("show all list=%v", names)
	}
}

// The IDE bridge refuses everything binary, by content rather than by extension.
func TestReadText(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		head   []byte
		size   int64  // grown to this many bytes when set
		editor string // what the editor reader's refusal says; "" opens it
	}{
		// Paradox ships a lot of these; no browser decodes either.
		{"dds", append([]byte("DDS "), 0, 0, 0, 0x7C), 0, formatBinary},
		{"tga", []byte{0x00, 0x00, 0x02, 0x00, 0x00, 0x00, 0x00, 0x00}, 0, formatBinary},
		{"exe", append([]byte("MZ"), 0, 0, 0, 0), 0, formatBinary},
		{"arbitrary", []byte{0x13, 0x37, 0x00, 0x42}, 0, formatBinary},
		{"text", []byte("hello"), 0, ""},
		{"large", nil, inventory.MaxTextBytes + 1, "too large"},
	}
	for _, testCase := range cases {
		path := filepath.Join(t.TempDir(), testCase.name+".bin")
		if err := os.WriteFile(path, testCase.head, 0o644); err != nil {
			t.Fatal(err)
		}
		if testCase.size > 0 {
			if err := os.Truncate(path, testCase.size); err != nil {
				t.Fatal(err)
			}
		}
		got, err := (*FileService)(nil).ReadText(path)
		if err != nil {
			t.Fatal(err)
		}
		switch {
		case testCase.editor == "" && got.Reject != "":
			t.Errorf("%s: refused %q; text opens in the editor", testCase.name, got.Reject)
		case testCase.editor != "" && got.Reject == "":
			t.Errorf("%s: opened in the editor; nothing binary may be", testCase.name)
		case !strings.Contains(got.Reject, testCase.editor):
			t.Errorf("%s: reject %q, want %q", testCase.name, got.Reject, testCase.editor)
		}
	}
}

// logHome sends APPDATA, XDG_CONFIG_HOME and HOME to a temporary folder, restores the default logger and the crash
// output when the test ends, and returns pmt.log's path there.
func logHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("APPDATA", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("HOME", home)
	previous := slog.Default()
	t.Cleanup(func() {
		slog.SetDefault(previous)
		_ = debug.SetCrashOutput(nil, debug.CrashOptions{}) // lets the temporary folder go
	})
	dir, err := appdata.Dir()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, "pmt.log")
}

// openLog is the log service over a log opened for a test.
func openLog(t *testing.T) *LogService {
	t.Helper()
	file, err := OpenLog("test")
	if err != nil {
		t.Fatal(err)
	}
	return NewLogService(file, nil)
}

// a run that wrote its end is closed, and its log past 2 MB is moved aside
func TestLogRotation(t *testing.T) {
	path := logHome(t)
	big := strings.Repeat("x", logRotateAt) + "\n"
	if err := os.WriteFile(path, []byte("msg=\"session start\"\n"+big+"msg=\"session end\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	logs := openLog(t)
	if _, moved := os.Stat(path + ".1"); logs.LastRunCrashed() || moved != nil {
		t.Errorf("a closed run: crashed %v, moved aside %v", logs.LastRunCrashed(), moved == nil)
	}

	// the report holds the last scan's lines and the log's tail
	logs.Logger.Info("scan start", "game", gameCK3)
	logs.Logger.Info("scan end", "took", "1s")
	if report := logs.diagnostics(); !strings.Contains(report, "Last scan:\n") || !strings.HasSuffix(strings.TrimSpace(report), `msg="scan end" took=1s`) {
		t.Errorf("the report: %s", report)
	}
	_ = logs.ServiceShutdown()
}

// a run with no end crashed, and its log stays whole however long it is
func TestLogAfterNoEnd(t *testing.T) {
	path := logHome(t)
	big := strings.Repeat("x", logRotateAt) + "\n"
	if err := os.WriteFile(path, []byte("msg=\"session start\"\n"+big), 0o644); err != nil {
		t.Fatal(err)
	}
	logs := openLog(t)
	if _, moved := os.Stat(path + ".1"); !logs.LastRunCrashed() || moved == nil {
		t.Errorf("a run with no end: crashed %v, moved aside %v", logs.LastRunCrashed(), moved == nil)
	}
	_ = logs.ServiceShutdown()
}

// a panic in a goroutine of the app's own lands in pmt.log, and the next run reads a crash
func TestLogPanic(t *testing.T) {
	// the child: this test binary run again as the app, which panics in a goroutine of its own
	if os.Getenv("PMT_LOG_PANIC") == "1" {
		if _, err := OpenLog("child"); err != nil {
			t.Fatal(err)
		}
		go func() { panic("pmt test panic") }()
		select {}
	}
	path := logHome(t)
	child := exec.Command(os.Args[0], "-test.run=^TestLogPanic$")
	child.Env = append(os.Environ(), "PMT_LOG_PANIC=1")
	if err := child.Run(); err == nil {
		t.Fatal("the child lived through its panic")
	}
	if raw, _ := os.ReadFile(path); !strings.Contains(string(raw), "panic: pmt test panic") {
		t.Errorf("the child's panic is not in pmt.log: %s", raw[max(0, len(raw)-400):])
	}
	logs := openLog(t)
	if !logs.LastRunCrashed() {
		t.Error("after the child's panic the last run crashed")
	}
	_ = logs.ServiceShutdown()
}

// TestEnsureSession covers the session a workspace gets: built once for many callers, kept, and dropped.
func TestEnsureSession(t *testing.T) {
	svc := fixtureService(t)
	before := svc.GetLanguageHealth("ws")
	if before.Ready || !before.InstallOk || before.InstallID == "" {
		t.Fatalf("before the first build: no session: %+v", before)
	}

	health, err := svc.EnsureSession("ws")
	if err != nil || !health.Ready {
		t.Fatalf("a session built from a scan: %+v (%v)", health, err)
	}
	ws := svc.cachedSession("ws")
	if ws == nil || len(ws.Mods) != 1 || len(index.Lookup(ws.Indexes(), schema.Defines, "traits", "bold")) == 0 {
		t.Error("the mod is indexed over the install")
	}
	if _, err := svc.EnsureSession("nowhere"); err == nil {
		t.Error("a workspace the config lacks")
	}

	svc.dropAllExcept("")

	var wg sync.WaitGroup
	got := make([]*workspace.Session, 20)
	for i := range got {
		wg.Go(func() {
			got[i], _ = svc.openSession("ws")
		})
	}
	wg.Wait()
	if got[0] == nil || slices.ContainsFunc(got, func(ws *workspace.Session) bool { return ws != got[0] }) {
		t.Error("twenty callers at once share one build and one instance")
	}
	if _, err := svc.openSession(""); err == nil || svc.cachedSession("ws") != got[0] {
		t.Error("an empty id is refused and the live session is untouched")
	}

	svc.dropAllExcept("")
	if svc.cachedSession("ws") != nil {
		t.Error("dropping every session leaves none live")
	}
}

// modsService is a workspace service over workspace "ws1" and a folder holding two mod folders, a and b.
func modsService(t *testing.T) (svc *WorkspaceService, modDir string) {
	t.Helper()
	config := tempConfig(t, nil, Workspace{ID: "ws1", GameID: gameCK3, Name: "Test", InstallID: installOne})
	svc = NewWorkspaceService(config, NewSessionService(config, "test", wiki.New(t.TempDir())))
	modDir = t.TempDir()
	writeTree(t, modDir, map[string]string{"a/descriptor.mod": "", "b/descriptor.mod": ""})
	return svc, modDir
}

// modIDs is the ids of workspace "ws1"'s mods, in load order.
func modIDs(t *testing.T, svc *WorkspaceService) []string {
	t.Helper()
	ws, _, err := svc.config.workspace("ws1")
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, mod := range ws.Mods {
		ids = append(ids, mod.ID)
	}
	return ids
}

// TestWorkspaceService_AddMods covers a folder that is no mod, and the order and sort numbers of mods added.
func TestWorkspaceService_AddMods(t *testing.T) {
	svc, modDir := modsService(t)

	// a folder with no descriptor, or no folder at all, is no mod
	if _, err := svc.AddWorkspaceMod("ws1", modDir); err == nil || !strings.Contains(err.Error(), "descriptor.mod") {
		t.Fatalf("a folder with no descriptor: %v", err)
	}
	if _, err := svc.AddWorkspaceMod("ws1", filepath.Join(modDir, "gone")); err == nil {
		t.Fatal("a path that is not there")
	}

	alpha, err := svc.AddWorkspaceMod("ws1", filepath.Join(modDir, "a"))
	if err != nil {
		t.Fatal(err)
	}
	beta, err := svc.AddWorkspaceMod("ws1", filepath.Join(modDir, "b"))
	if err != nil {
		t.Fatal(err)
	}
	if got := modIDs(t, svc); !slices.Equal(got, []string{alpha.ID, beta.ID}) {
		t.Fatalf("initial order: %v", got)
	}
	if alpha.SortOrder != 0 || beta.SortOrder != 1 {
		t.Fatalf("sort orders: %d %d", alpha.SortOrder, beta.SortOrder)
	}
}

// TestWorkspaceService_ReorderRemoveMods covers a reorder, the list in load order, a removal, and a workspace's deletion.
func TestWorkspaceService_ReorderRemoveMods(t *testing.T) {
	svc, modDir := modsService(t)
	alpha, err := svc.AddWorkspaceMod("ws1", filepath.Join(modDir, "a"))
	if err != nil {
		t.Fatal(err)
	}
	beta, err := svc.AddWorkspaceMod("ws1", filepath.Join(modDir, "b"))
	if err != nil {
		t.Fatal(err)
	}

	if err := svc.ReorderWorkspaceMods("ws1", []string{alpha.ID}); err == nil {
		t.Fatal("want incomplete-set error")
	}
	if err := svc.ReorderWorkspaceMods("ws1", []string{beta.ID, alpha.ID}); err != nil {
		t.Fatal(err)
	}
	if got := modIDs(t, svc); !slices.Equal(got, []string{beta.ID, alpha.ID}) {
		t.Fatalf("reordered: %v", got)
	}
	workspaces := svc.ListWorkspaces()
	if len(workspaces) != 1 {
		t.Fatalf("list: %+v", workspaces)
	}
	fromList := workspaces[0].Mods
	if len(fromList) != 2 || fromList[0].ID != beta.ID || fromList[1].ID != alpha.ID {
		t.Fatalf("list order: %+v", fromList)
	}

	if err := svc.RemoveWorkspaceMod("ws1", beta.ID); err != nil {
		t.Fatal(err)
	}
	if got := modIDs(t, svc); !slices.Equal(got, []string{alpha.ID}) {
		t.Fatalf("after remove: %v", got)
	}

	if err := svc.DeleteWorkspace(""); err == nil {
		t.Fatal("empty id")
	}
	if err := svc.DeleteWorkspace("ws1"); err != nil {
		t.Fatal(err)
	}
	if workspaces = svc.ListWorkspaces(); len(workspaces) != 0 {
		t.Fatalf("listed: %+v", workspaces)
	}
	if err := svc.DeleteWorkspace("ws1"); err == nil {
		t.Fatal("want not found")
	}
}

// TestGetIdeRoots_OrderAndGameTitle covers the IDE's roots: the mod then the game by its title, and the workspace's prefs.
func TestGetIdeRoots_OrderAndGameTitle(t *testing.T) {
	install := t.TempDir()
	modDir := t.TempDir()
	config := tempConfig(t,
		[]GameInstall{{ID: installOne, GameID: gameCK3, Name: "My CK3 nickname", Path: install}},
		Workspace{ID: "ws1", GameID: gameCK3, Name: "Test", InstallID: installOne})
	svc := NewWorkspaceService(config, NewSessionService(config, "test", wiki.New(t.TempDir())))
	writeTree(t, modDir, map[string]string{"descriptor.mod": ""})
	if _, err := svc.AddWorkspaceMod("ws1", modDir); err != nil {
		t.Fatal(err)
	}
	got, err := svc.GetIdeRoots("ws1")
	if err != nil {
		t.Fatal(err)
	}
	roots := got.Roots
	if len(roots) != 2 {
		t.Fatalf("len %d %+v", len(roots), roots)
	}
	if roots[0].Kind != "mod" || roots[1].Kind != "game" {
		t.Fatalf("kinds %+v", roots)
	}
	if roots[1].Label != "Crusader Kings III" || roots[1].Origin != "vanilla" {
		t.Fatalf("game root %+v", roots[1])
	}

	if err := svc.UpdateWorkspacePrefs("ws1", true, "event-graph", "", true); err != nil {
		t.Fatal(err)
	}
	if err := svc.UpdateWorkspacePrefs("ws1", false, "not-a-tool", "", true); err == nil {
		t.Fatal("want invalid default tool")
	}
	if err := svc.UpdateWorkspacePrefs("ws1", false, "conflicts", "", true); err == nil {
		t.Fatal("want invalid default tool")
	}
	if err := svc.UpdateWorkspacePrefs("ws1", true, "health", "", false); err != nil {
		t.Fatal(err)
	}
	ws, _, _ := svc.config.workspace("ws1")
	if !ws.ResetIdeOnOpen || ws.DefaultTool != "health" || ws.HideExplorerBinaries {
		t.Fatalf("prefs: %+v", ws)
	}
}

// An empty thumbnail is found on disk per game, EU5's in .metadata; only the backend knows where.
func TestAddWorkspaceModDiscoversTheThumbnail(t *testing.T) {
	for _, testCase := range []struct{ game, rel string }{
		{gameCK3, "thumbnail.png"},
		{"vic3", "thumbnail.png"},
		{gameEU5, ".metadata/thumbnail.png"},
		// A mod that put it somewhere else keeps its own choice.
		{gameEU5, "thumbnail.gif"},
		// A mod with no thumbnail must not be given a path to one that is not there.
		{gameCK3, ""},
	} {
		t.Run(testCase.game+"/"+testCase.rel, func(t *testing.T) {
			config := tempConfig(t, nil, Workspace{ID: "ws", GameID: testCase.game, Name: "T"})
			svc := NewWorkspaceService(config, NewSessionService(config, "test", wiki.New(t.TempDir())))
			root, want := t.TempDir(), ""
			writeTree(t, root, map[string]string{game.DescriptorFile(testCase.game): "{}"})
			if testCase.rel != "" {
				want = filepath.Join(root, filepath.FromSlash(testCase.rel))
				writeTree(t, root, map[string]string{testCase.rel: "img"})
			}
			mod, err := svc.AddWorkspaceMod("ws", root)
			if err != nil {
				t.Fatal(err)
			}
			if mod.Thumbnail != want {
				t.Errorf("Thumbnail = %q, want %q", mod.Thumbnail, want)
			}
		})
	}
}

// The sample budgets: files diagnosed per mod, files hovered in, and words hovered in each.
const (
	diagnosed  = 300
	hoverFiles = 40
	hoverWords = 8
)

var modPath = regexp.MustCompile(`(?m)^\s*path\s*=\s*"([^"]+)"`)

// TestSweep is the scoreboard, not a gate: it asserts nothing, so a count that moves is news. Counts
// are not targets, and the samples are budgets, since one editor request walks its file. It walks
// every mod the folders PMT_TEST_MODS_<GAME> list (a game's Documents mod folder, whose .mod
// descriptors say where each mod is), over the install PMT_TEST_INSTALL_<GAME>.
func TestSweep(t *testing.T) {
	if os.Getenv("PMT_TEST_SWEEP") == "" {
		t.Skip("walks every mod of a game's mod folder; set PMT_TEST_SWEEP=1")
	}
	for _, id := range []string{gameCK3, "vic3", gameEU5} {
		install, mods := os.Getenv("PMT_TEST_INSTALL_"+strings.ToUpper(id)), os.Getenv("PMT_TEST_MODS_"+strings.ToUpper(id))
		if install == "" || mods == "" {
			continue
		}
		user := game.UserDataDir(id, install)
		started := time.Now()
		scanned, err := index.Scan(t.Context(), index.Request{Game: id, Dir: install, Version: "sweep"})
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("SWEEP %s: install scanned in %.1fs", id, time.Since(started).Seconds())

		descriptors, _ := filepath.Glob(filepath.Join(mods, "*.mod"))
		for _, descriptor := range descriptors {
			raw, err := os.ReadFile(descriptor)
			if err != nil {
				continue
			}
			match := modPath.FindSubmatch(raw)
			if match == nil {
				continue
			}
			dir := string(match[1])
			if !filepath.IsAbs(dir) {
				dir = filepath.Join(user, dir)
			}
			ws, err := workspace.Open(t.Context(), game.Get(id), scanned, []workspace.Mod{{ID: "mod", Dir: dir}}, "")
			if err != nil {
				t.Logf("SWEEP %s %s: %v", id, filepath.Base(descriptor), err)
				continue
			}
			sweep(t, id, filepath.Base(descriptor), ws)
		}
	}
}

// sweep reports one mod: the health counts, the findings over a sample of its script files, and
// the hover answer rate over a sample of the words in a sample of those files.
func sweep(t *testing.T, id, name string, ws *workspace.Session) {
	started := time.Now()
	health := pages.Health(ws, pages.HealthParams{}, (&pages.Findings{}).Current(ws))
	untranslated, defs := 0, 0
	for _, language := range health.Loc {
		untranslated += len(language.Untranslated)
	}
	for _, found := range ws.Workspace.Occurrences {
		for _, occurrence := range found {
			if occurrence.Role == schema.Defines {
				defs++
			}
		}
	}
	var files []inventory.File
	for _, file := range ws.Workspace.Files {
		if file.Format == inventory.Script {
			files = append(files, file)
		}
	}
	slices.SortFunc(files, func(first, second inventory.File) int { return strings.Compare(first.Rel, second.Rel) })

	findings := map[string]int{}
	for _, file := range spread(files, diagnosed) {
		for _, finding := range must(lsp.Diagnose(ws, file.Path, nil)) {
			findings[finding.Code]++
		}
	}
	hovers, answered := 0, 0
	for _, hoverFile := range spread(files, hoverFiles) {
		text, err := ws.Text(hoverFile.Path)
		if err != nil {
			continue
		}
		asked := 0
		for line, lineText := range strings.Split(text, "\n") {
			for _, word := range strings.Fields(lineText) {
				if strings.HasPrefix(word, "#") {
					break
				}
				col := strings.Index(lineText, word)
				if col < 0 || word == "=" || word == "{" || word == "}" {
					continue
				}
				asked++
				if asked > hoverWords {
					break
				}
				hovers++
				if card, _ := lsp.Hover(ws, hoverFile.Path, line, col); card != nil {
					answered++
				}
			}
			if asked > hoverWords {
				break
			}
		}
	}
	fmt.Printf("SWEEP %s %s: %d files, %d defs; contests %d, depends %d, dangling %d, loc untranslated %d; findings %v; hover answered %d of %d; %.1fs\n",
		id, name, len(ws.Workspace.Files), defs, len(health.Contests), len(health.Depends), len(health.Dangling), untranslated, findings, answered, hovers, time.Since(started).Seconds())
}

// spread takes at most n files evenly across the list, so a big mod's sample reaches every corner.
func spread(files []inventory.File, n int) []inventory.File {
	if len(files) <= n {
		return files
	}
	out := make([]inventory.File, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, files[i*len(files)/n])
	}
	return out
}

// must is the value, failing the test on an error.
func must[T any](value T, err error) T {
	if err != nil {
		panic(err)
	}
	return value
}
