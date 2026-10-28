package health

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/samber/lo"
	"github.com/wailsapp/wails/v3/pkg/application"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/services/app"
	"paradox-modding-tools/services/lsp"
	"paradox-modding-tools/services/workspace"
)

// The ids and descriptor names of the fixture mods, and the languages of the scaffolds.
const (
	modA   = "mod-a"
	modB   = "mod-b"
	titleA = "Fixture Mod"
	titleB = "Second Mod"
	french = "french"
	german = "german"
)

// modAFiles are the files the first mod adds.
var modAFiles = map[string]string{
	"common/traits/a_shared.txt": "shared = { category = personality diplomacy = 1 }\n",
	"common/traits/m_unused.txt": "unloved = { category = personality }\n",
	"events/m_title_events.txt": "namespace = m\nm.3 = { type = character_event title = m.3.t }\n" +
		"m.4 = { type = character_event immediate = { trigger_event = m.99 } }\n",
	"events/m_broken.txt":                  "broken = {\n",
	"events/v_events.txt":                  "namespace = v\nv.1 = { type = character_event }\nv.2 = { type = character_event }\n",
	"localization/english/m_l_english.yml": "l_english:\n m_orphan:0 \"Alone\"\n m_kept:0 \"Kept\"\n",
	"localization/german/m_l_german.yml":   "l_german:\n m_kept:0 \"Behalten\"\n",
}

// modBFiles are the files the second mod adds.
var modBFiles = map[string]string{
	"descriptor.mod":             "version=\"1.0\"\nname=\"" + titleB + "\"\nsupported_version=\"1.*\"\n",
	"common/traits/b_shared.txt": "shared = { category = personality diplomacy = 2 }\n",
}

// errorLogText is an error log in Victoria 3's format. Its first message goes on over lines that name a mod's file.
const errorLogText = "[22:07:20][jomini_script_system.cpp:247]: Script system error!\n" +
	"  Error: custom_tooltip trigger\n\n" +
	"  Script location: common/country_definitions/m_countries.txt:2\n" +
	"[22:07:21][x.cpp:1]: Second message\n"

// devMods are the mods the real-install test loads on each game, in load order.
var devMods = map[string][]string{
	"ck3": {
		`C:\Program Files (x86)\Steam\steamapps\workshop\content\1158310\2243307127`,
		`C:\Dev\repos\More Historicity - CK3`, `C:\Dev\repos\ck3-quieter-events`, `C:\Dev\repos\Shut Up About Dynasty Bastards`,
	},
	"vic3": {
		`C:\Program Files (x86)\Steam\steamapps\workshop\content\529340\3385002128`,
		`C:\Dev\repos\Vic3\Hail, Columbia`, `C:\Dev\repos\Vic3\Morgenroete`,
	},
	"eu5": {
		`C:\Dev\repos\EU5\Community Flavor Pack`, `C:\Dev\repos\EU5\Fix Trade Companies`, `C:\Dev\repos\EU5\Urban Rights`,
	},
}

// createApp makes the application that carries the workspace's events, once per test run.
var createApp = sync.OnceFunc(func() { application.New(application.Options{}) })

// resolveFixture returns the absolute path of a folder under internal/testdata.
func resolveFixture(t *testing.T, parts ...string) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join(append([]string{"..", "..", "internal", "testdata"}, parts...)...))
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// writeFile writes a file under a folder, making the folders above it.
func writeFile(t *testing.T, dir, rel, text string) {
	t.Helper()
	target := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// createMod copies a game's fixture mod into a temporary folder and writes files over it. It returns the mod's record.
func createMod(t *testing.T, gameID, id string, added map[string]string) app.Mod {
	t.Helper()
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS(resolveFixture(t, "mods", gameID))); err != nil {
		t.Fatal(err)
	}
	for rel, text := range added {
		writeFile(t, dir, rel, text)
	}
	return app.Mod{ID: id, Dir: dir}
}

// createInstall copies a game's fixture install into a temporary folder, with user data that holds an error log. It
// returns the install's folder.
func createInstall(t *testing.T, gameID, errorLog string) string {
	t.Helper()
	install, userData := t.TempDir(), t.TempDir()
	if err := os.CopyFS(install, os.DirFS(resolveFixture(t, gameID))); err != nil {
		t.Fatal(err)
	}
	writeFile(t, userData, "logs/error.log", errorLog)
	settings, err := json.Marshal(map[string]string{"gameDataPath": userData})
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, install, "launcher/launcher-settings.json", string(settings))
	return install
}

// openService opens a workspace on an install and returns a Health service over it. The workspace has a config and
// caches of its own. It closes when the test ends.
func openService(t *testing.T, gameID, installPath string, mods ...app.Mod) *Service {
	t.Helper()
	createApp()
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
	wsp := &workspace.Service{Config: config, Build: "test"}
	t.Cleanup(wsp.Close)
	wsp.Open("w1")
	if _, err := wsp.Session(); err != nil {
		t.Fatal(err)
	}
	return &Service{Workspace: wsp}
}

// findFinding returns the finding of a code, name and database. It fails the test if there is none. An empty database
// matches any.
func findFinding(t *testing.T, findings []Finding, code, name, database string) Finding {
	t.Helper()
	finding, ok := lo.Find(findings, func(finding Finding) bool {
		return finding.Code == code && finding.Name == name && (database == "" || finding.Database == database)
	})
	if !ok {
		t.Fatalf("no %s finding for %q among %d findings", code, name, len(findings))
	}
	return finding
}

// TestBuildReportFindsContestsOverridesAndProblems checks a report over two fixture mods.
func TestBuildReportFindsContestsOverridesAndProblems(t *testing.T) {
	hlt := openService(t, "ck3", resolveFixture(t, "ck3"), createMod(t, "ck3", modA, modAFiles), createMod(t, "ck3", modB, modBFiles))
	report, err := hlt.BuildReport(nil)
	if err != nil {
		t.Fatal(err)
	}
	contest := findFinding(t, report.Findings, "contest", "shared", "trait")
	if contest.Question != lsp.QuestionCompatibility || len(contest.Sites) != 2 || !contest.Sites[0].Kept ||
		contest.Sites[1].Kept || contest.Sites[0].Reason != "last" || contest.Sites[0].Place.Origin != modB {
		t.Errorf("contest of shared: %+v", contest)
	}
	shipped := findFinding(t, report.Findings, "file-contest", "common/traits/m_traits.txt", game.File)
	if len(shipped.Sites) != 2 || !shipped.Sites[0].Kept || shipped.Sites[1].Kept ||
		shipped.Sites[0].Place.Origin != modB || shipped.Sites[0].Range.StartLine != 1 {
		t.Errorf("contest of the file m_traits.txt: %+v", shipped)
	}
	if lo.ContainsBy(report.Findings, func(finding Finding) bool { return finding.Name == "descriptor.mod" }) {
		t.Error("a descriptor, which the game does not read as a file, is a finding")
	}
	// the game's own v_traits.txt loads after the mods' m_traits.txt
	unused := findFinding(t, report.Findings, "unused-copy", "brave", "trait")
	if unused.Facts["mod"] != titleB || unused.Facts["reason"] != "its file is read after the mod's" ||
		len(unused.Sites) != 2 || !unused.Sites[0].Place.ReadOnly || !unused.Sites[0].Kept || unused.Sites[1].Kept {
		t.Errorf("unused copy of brave: %+v", unused)
	}
	replaced := findFinding(t, report.Findings, "override", "events/v_events.txt", game.File)
	if replaced.Facts["kept"] != "the mod's" || len(replaced.Sites) != 2 || !replaced.Sites[0].Kept || replaced.Sites[1].Kept ||
		lo.ContainsBy(report.Findings, func(finding Finding) bool { return finding.Code == "override" && finding.Name == "v.1" }) {
		t.Errorf("override of the file v_events.txt: %+v", replaced)
	}
	assertLocalization(t, report)
	dangling := findFinding(t, report.Findings, "unresolved-reference", "m.99", "")
	if dangling.Question != lsp.QuestionHealth || len(dangling.Sites) != 1 || dangling.Sites[0].Range.StartLine != 3 {
		t.Errorf("dangling reference: %+v", dangling)
	}
	if script := findFinding(t, report.Findings, "unclosed-block", "", ""); script.Question != lsp.QuestionHealth {
		t.Errorf("script problem: %+v", script)
	}
	previewed, err := hlt.BuildReport([]string{modB, modA})
	if err != nil {
		t.Fatal(err)
	}
	kept := findFinding(t, previewed.Findings, "file-contest", "common/traits/m_traits.txt", game.File).Sites
	if len(kept) != 2 || !kept[0].Kept || kept[0].Place.Origin != modA {
		t.Errorf("contest of the file m_traits.txt with the first mod loaded last: %+v", kept)
	}
	if _, err := hlt.BuildReport([]string{modA}); err == nil {
		t.Error("an order that leaves out a mod was built")
	}
}

// assertLocalization checks the findings of a missing title message and of a partly written language.
func assertLocalization(t *testing.T, report Report) {
	t.Helper()
	missing := findFinding(t, report.Findings, "missing-message", "m.3", "")
	coverage := findFinding(t, report.Findings, "coverage", german, "")
	if missing.Facts["key"] != "m.3.t" || coverage.Facts["mod"] != titleA ||
		coverage.Facts["have"] != "1" || coverage.Facts["of"] != "3" || len(coverage.Sites) != 1 {
		t.Errorf("localization: %+v, %+v", missing, coverage)
	}
}

// TestListErrorLogAddsContinuationLines checks that the lines after a message are part of it. A location on one of
// them gives the message its place. A blank line adds nothing.
func TestListErrorLogAddsContinuationLines(t *testing.T) {
	hlt := openService(t, "vic3", createInstall(t, "vic3", errorLogText), createMod(t, "vic3", modA, nil))
	gameLog, err := hlt.ListErrorLog()
	if err != nil {
		t.Fatal(err)
	}
	findings := gameLog.Findings
	if len(findings) != 2 || findings[0].Code != "game-error" || findings[0].Question != lsp.QuestionLog {
		t.Fatalf("findings %+v", findings)
	}
	wantMessage := "Script system error!\n  Error: custom_tooltip trigger\n  Script location: common/country_definitions/m_countries.txt:2"
	if findings[0].Facts["message"] != wantMessage || len(findings[0].Sites) != 1 ||
		findings[0].Sites[0].Place.Origin != modA || findings[0].Sites[0].Range.StartLine != 2 {
		t.Errorf("first message: %+v", findings[0])
	}
	if findings[1].Facts["message"] != "Second message" || len(findings[1].Sites) != 0 {
		t.Errorf("second message: %+v", findings[1])
	}
}

// TestWriteScaffoldWritesMissingKeys checks that a scaffold writes each key a language lacks. A missing file is written
// whole, with a byte order mark, and an existing one gets the keys at its end. A name that is no language writes
// nothing.
func TestWriteScaffoldWritesMissingKeys(t *testing.T) {
	mod := createMod(t, "ck3", modA, modAFiles)
	hlt := openService(t, "ck3", resolveFixture(t, "ck3"), mod)
	written, err := hlt.WriteScaffold(modA, french)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		filepath.Join(mod.Dir, "localization", french, "m_l_french.yml"),
		filepath.Join(mod.Dir, "localization", "replace", french, "m_replace_l_french.yml"),
	}
	if !slices.Equal(written, want) {
		t.Fatalf("written %v, want %v", written, want)
	}
	text, err := files.ReadText(want[0])
	if err != nil {
		t.Fatal(err)
	}
	if text.Encoding != files.EncodingUTF8BOM || text.Text != "l_french:\n m_orphan:0 \"Alone\"\n m_kept:0 \"Kept\"\n" {
		t.Errorf("scaffold %q in %s", text.Text, text.Encoding)
	}
	if again, err := hlt.WriteScaffold(modA, french); err != nil || len(again) != 0 {
		t.Errorf("scaffold of french again: %v, %v", again, err)
	}
	// german has m_kept: m_orphan joins its file, and the replace folder's file is written
	partial, err := hlt.WriteScaffold(modA, german)
	if err != nil || len(partial) != 2 ||
		!strings.HasSuffix(filepath.ToSlash(partial[1]), "localization/replace/german/m_replace_l_german.yml") {
		t.Fatalf("scaffold of german: %v, %v", partial, err)
	}
	if text, _ := files.ReadText(partial[0]); text.Text != "l_german:\n m_kept:0 \"Behalten\"\n m_orphan:0 \"Alone\"\n" {
		t.Errorf("german file %q", text.Text)
	}
	if _, err := hlt.WriteScaffold(modA, "../"+game.BaseLanguage); err == nil {
		t.Error("a scaffold was written for a name that is no language")
	}
}

// TestBuildReportInstalls builds and logs the report of each real install that PMT_TEST_INSTALL_<GAME> names, with the
// dev mods. It fails if the report's counts of the editor's codes differ from the editor's.
func TestBuildReportInstalls(t *testing.T) {
	for gameID, dirs := range devMods {
		t.Run(gameID, func(t *testing.T) {
			variable := "PMT_TEST_INSTALL_" + strings.ToUpper(gameID)
			install := os.Getenv(variable)
			if install == "" {
				t.Skipf("%s is not set", variable)
			}
			mods := lo.Map(dirs, func(dir string, position int) app.Mod {
				return app.Mod{ID: fmt.Sprintf("dev-%d", position+1), Dir: dir}
			})
			hlt := openService(t, gameID, install, mods...)
			started := time.Now()
			report, err := hlt.BuildReport(nil)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("%d findings in %s", len(report.Findings), time.Since(started).Round(time.Millisecond))
			logCounts(t, report)
			started = time.Now()
			gameLog, err := hlt.ListErrorLog()
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("error log: %d findings, %d with a site, %d a mod's, %d repeats folded, in %s", len(gameLog.Findings),
				lo.CountBy(gameLog.Findings, func(finding Finding) bool { return len(finding.Sites) > 0 }),
				lo.CountBy(gameLog.Findings, func(finding Finding) bool { return finding.Origin != "" }),
				lo.SumBy(gameLog.Findings, func(finding Finding) int { return finding.Count - 1 }),
				time.Since(started).Round(time.Millisecond))
			compareDiagnostics(t, hlt, report)
		})
	}
}

// logCounts logs the findings of a report by code.
func logCounts(t *testing.T, report Report) {
	t.Helper()
	codes, levels := map[string]int{}, map[string]int{}
	for _, finding := range report.Findings {
		codes[finding.Code]++
		levels[finding.Question+"/"+finding.Level]++
	}
	for _, level := range slices.Sorted(maps.Keys(levels)) {
		t.Logf("question/level %s: %d", level, levels[level])
	}
	for _, code := range slices.Sorted(maps.Keys(codes)) {
		t.Logf("code %s: %d", code, codes[code])
	}
	for _, finding := range report.Findings {
		if finding.Code == "undeclared-dependency" || finding.Code == "ignored-pair" || len(finding.Order) > 0 {
			t.Logf("%s %s: %s, level %s, mods %v, facts %v, order %v", finding.Code, finding.Database, finding.Name,
				finding.Level, finding.Mods, finding.Facts, finding.Order)
		}
	}
	for _, code := range []string{"contest", "override"} {
		listed := lo.Filter(report.Findings, func(finding Finding, _ int) bool { return finding.Code == code })
		shipped := lo.CountBy(listed, func(finding Finding) bool { return finding.Database == game.File })
		t.Logf("%s: %d names, %d files", code, len(listed)-shipped, shipped)
	}
}

// compareDiagnostics fails the test if the report's counts of the editor's codes differ from what the editor finds.
func compareDiagnostics(t *testing.T, hlt *Service, report Report) {
	t.Helper()
	ws, err := hlt.Workspace.Session()
	if err != nil {
		t.Fatal(err)
	}
	mods := ws.Mods.Load()
	want := map[string]int{}
	for _, file := range mods.Files[len(ws.Install.Files):] {
		if _, known := mods.FindFile(file.Path); !known || !file.Format.IsScript() && file.Format != files.FormatLocalization {
			continue
		}
		diagnostics, _ := lsp.DiagnoseFile(ws, mods, file.Path)
		for _, diagnostic := range diagnostics {
			want[diagnostic.Code]++
		}
	}
	own := []string{"missing-message", "coverage", "mod-for-older-version"}
	got := map[string]int{}
	for _, finding := range report.Findings {
		if (finding.Question == lsp.QuestionHealth || slices.Contains(sharedCodes, finding.Code)) && !slices.Contains(own, finding.Code) {
			got[finding.Code] += len(finding.Sites)
		}
	}
	t.Logf("the editor's codes: %d places in the report, %d in the editor", lo.Sum(slices.Collect(maps.Values(got))),
		lo.Sum(slices.Collect(maps.Values(want))))
	if !maps.Equal(got, want) {
		t.Errorf("the report holds %v, the editor finds %v", got, want)
	}
}
