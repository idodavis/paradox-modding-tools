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

	"github.com/adrg/xdg"
	"github.com/samber/lo"
	"github.com/wailsapp/wails/v3/pkg/application"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/services/app"
	"paradox-modding-tools/services/lsp"
	"paradox-modding-tools/services/workspace"
)

// The ids and descriptor names of the fixture mods, the languages of the scaffolds and the sections the tests use.
const (
	modA            = "mod-a"
	modB            = "mod-b"
	titleA          = "Fixture Mod"
	titleB          = "Second Mod"
	french          = "french"
	german          = "german"
	sectionDangling = "dangling"
	sectionErrorLog = "errorlog"
)

// modAFiles are the files the first mod adds: traits, events, a file with an unclosed block, a file that replaces a
// file of the game, and messages in two languages.
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

// modBFiles are the files the second mod adds: its own descriptor name, and a trait the first mod writes otherwise.
var modBFiles = map[string]string{
	"descriptor.mod":             "version=\"1.0\"\nname=\"" + titleB + "\"\nsupported_version=\"1.*\"\n",
	"common/traits/b_shared.txt": "shared = { category = personality diplomacy = 2 }\n",
}

// errorLogText is an error log in Victoria 3's format. Its first message has continuation lines, the last of which
// names a place in a mod's file.
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

// createApp makes the application the workspace service sends its events through, once for the whole test run.
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

// createMod copies a game's fixture mod into a temporary folder, writes the given files over it and returns its
// record.
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

// createInstall copies a game's fixture install into a temporary folder, with the game's user data in another folder
// that holds the given error log. It returns the install's folder.
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

// openService opens a workspace on an install with the given mods, with a config and caches of its own, and returns
// the Health service over it. The workspace closes when the test ends.
func openService(t *testing.T, gameID, installPath string, mods ...app.Mod) *Service {
	t.Helper()
	createApp()
	t.Cleanup(xdg.Reload)
	for _, variable := range []string{"XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME"} {
		t.Setenv(variable, t.TempDir())
	}
	xdg.Reload()
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

// findFinding returns the finding with the given code, name and database, and fails the test if there is none. An
// empty database matches any.
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

// countSection returns how many findings of a section a report holds.
func countSection(report Report, section string) int {
	return lo.CountBy(report.Findings, func(finding Finding) bool { return finding.Section == section })
}

// TestBuildReportFindsEachSection checks that a report over two fixture mods holds a finding of each section, that each
// section's count matches its findings, and that a file a mod replaces is a finding of the file and not of its names.
// It checks that another load order moves the loaded file and keeps the dependencies.
func TestBuildReportFindsEachSection(t *testing.T) {
	hlt := openService(t, "ck3", resolveFixture(t, "ck3"), createMod(t, "ck3", modA, modAFiles), createMod(t, "ck3", modB, modBFiles))
	report, err := hlt.BuildReport(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, section := range report.Sections {
		if section.Count != countSection(report, section.ID) || (section.Count == 0) != (section.ID == sectionErrorLog) {
			t.Errorf("section %s has the count %d", section.ID, section.Count)
		}
	}
	contest := findFinding(t, report.Findings, "contest", "shared", "trait")
	if contest.Section != "contests" || contest.Facts["rule"] != "LIOS" || len(contest.Sites) != 2 ||
		contest.Sites[0].Kept || !contest.Sites[1].Kept || contest.Sites[1].Reason != "order" || contest.Sites[1].Place.Origin != modB {
		t.Errorf("contest of shared: %+v", contest)
	}
	shipped := findFinding(t, report.Findings, "contest", "common/traits/m_traits.txt", game.File)
	if shipped.Facts["rule"] != "LIOS" || len(shipped.Sites) != 2 || shipped.Sites[0].Kept || !shipped.Sites[1].Kept ||
		shipped.Sites[1].Place.Origin != modB || shipped.Sites[1].Range.StartLine != 1 {
		t.Errorf("contest of the file m_traits.txt: %+v", shipped)
	}
	if lo.ContainsBy(report.Findings, func(finding Finding) bool { return finding.Name == "descriptor.mod" }) {
		t.Error("a descriptor, which the game does not read as a file, is a finding")
	}
	// the game loads the files of a folder by name, so its own v_traits.txt comes after the mods' m_traits.txt
	override := findFinding(t, report.Findings, "override", "brave", "trait")
	if override.Facts["kept"] != "its own" || len(override.Sites) != 2 || !override.Sites[0].Place.ReadOnly || !override.Sites[0].Kept ||
		override.Sites[1].Kept {
		t.Errorf("override of brave: %+v", override)
	}
	replaced := findFinding(t, report.Findings, "override", "events/v_events.txt", game.File)
	if replaced.Facts["kept"] != "the mod's" || len(replaced.Sites) != 2 || replaced.Sites[0].Kept || !replaced.Sites[1].Kept ||
		lo.ContainsBy(report.Findings, func(finding Finding) bool { return finding.Code == "override" && finding.Name == "v.1" }) {
		t.Errorf("override of the file v_events.txt: %+v", replaced)
	}
	assertDependencies(t, report)
	assertLocalization(t, report)
	dangling := findFinding(t, report.Findings, "unresolved-reference", "m.99", "")
	if dangling.Section != sectionDangling || len(dangling.Sites) != 1 || dangling.Sites[0].Range.StartLine != 3 {
		t.Errorf("dangling reference: %+v", dangling)
	}
	if script := findFinding(t, report.Findings, "unclosed-block", "", ""); script.Section != "script" {
		t.Errorf("script problem: %+v", script)
	}
	previewed, err := hlt.BuildReport([]string{modB, modA})
	if err != nil {
		t.Fatal(err)
	}
	kept := findFinding(t, previewed.Findings, "contest", "common/traits/m_traits.txt", game.File).Sites
	if len(kept) != 2 || !kept[1].Kept || kept[1].Place.Origin != modA {
		t.Errorf("contest of the file m_traits.txt with the first mod loaded last: %+v", kept)
	}
	if got, want := countSection(previewed, "dependencies"), countSection(report, "dependencies"); got != want || want != 2 {
		t.Errorf("%d dependencies with the first mod loaded last, %d otherwise", got, want)
	}
	if _, err := hlt.BuildReport([]string{modA}); err == nil {
		t.Error("an order that leaves out a mod was built")
	}
}

// assertDependencies checks that the second mod depends on the first and that the first is affected by it, each with
// one name and its two definitions.
func assertDependencies(t *testing.T, report Report) {
	t.Helper()
	depends := findFinding(t, report.Findings, "depends", titleB, "mod")
	affects := findFinding(t, report.Findings, "affects", titleA, "mod")
	if depends.Facts["other"] != titleA || affects.Facts["other"] != titleB || depends.Facts["count"] != "1" ||
		affects.Facts["count"] != "1" || len(depends.Sites) != 2 || len(affects.Sites) != 2 ||
		!depends.Sites[0].Kept || depends.Sites[0].Place.Origin != modB || affects.Sites[0].Place.Origin != modA {
		t.Errorf("dependencies: %+v, %+v", depends, affects)
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

// TestListErrorLogAddsContinuationLines checks that the lines after a message are part of it, that a location on one of
// them gives the message its place, and that a blank line adds nothing.
func TestListErrorLogAddsContinuationLines(t *testing.T) {
	hlt := openService(t, "vic3", createInstall(t, "vic3", errorLogText), createMod(t, "vic3", modA, nil))
	findings, err := hlt.ListErrorLog()
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 2 || findings[0].Code != "game-error" || findings[0].Section != sectionErrorLog {
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

// TestWriteScaffoldWritesMissingFiles checks that a scaffold writes a file for each base-language file, with the
// messages as placeholders and a byte order mark. It checks that an existing file is left as it is and that a name that
// is no language writes nothing.
func TestWriteScaffoldWritesMissingFiles(t *testing.T) {
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
	// the mod has german messages for its file already, so only the file under the replace folder is missing
	if partial, err := hlt.WriteScaffold(modA, german); err != nil || len(partial) != 1 ||
		!strings.HasSuffix(filepath.ToSlash(partial[0]), "localization/replace/german/m_replace_l_german.yml") {
		t.Errorf("scaffold of german: %v, %v", partial, err)
	}
	if _, err := hlt.WriteScaffold(modA, "../"+game.BaseLanguage); err == nil {
		t.Error("a scaffold was written for a name that is no language")
	}
}

// TestBuildReportInstalls builds the report of each real install that PMT_TEST_INSTALL_<GAME> names, with the dev mods,
// and logs it. It fails if the report holds other counts of the editor's codes than the editor finds in the same files.
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
			logDependencies(t, hlt, report, mods)
			started = time.Now()
			errorLog, err := hlt.ListErrorLog()
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("error log: %d findings, %d with a site, in %s", len(errorLog),
				lo.CountBy(errorLog, func(finding Finding) bool { return len(finding.Sites) > 0 }), time.Since(started).Round(time.Millisecond))
			compareDiagnostics(t, hlt, report)
		})
	}
}

// logCounts logs the findings of a report by section and by code.
func logCounts(t *testing.T, report Report) {
	t.Helper()
	for _, section := range report.Sections {
		t.Logf("section %s: %d", section.ID, section.Count)
	}
	codes := map[string]int{}
	for _, finding := range report.Findings {
		codes[finding.Code]++
	}
	for _, code := range slices.Sorted(maps.Keys(codes)) {
		t.Logf("code %s: %d", code, codes[code])
	}
	for _, code := range []string{"contest", "override"} {
		listed := lo.Filter(report.Findings, func(finding Finding, _ int) bool { return finding.Code == code })
		shipped := lo.CountBy(listed, func(finding Finding) bool { return finding.Database == game.File })
		t.Logf("%s: %d names, %d files", code, len(listed)-shipped, shipped)
	}
}

// logDependencies logs the dependency findings of a report, and those of the same mods in the reverse order.
func logDependencies(t *testing.T, hlt *Service, report Report, mods []app.Mod) {
	t.Helper()
	for _, finding := range report.Findings {
		if finding.Section == "dependencies" {
			t.Logf("%s: %s, other %s, %s names", finding.Code, finding.Name, finding.Facts["other"], finding.Facts["count"])
		}
	}
	order := lo.Map(mods, func(mod app.Mod, _ int) string { return mod.ID })
	slices.Reverse(order)
	previewed, err := hlt.BuildReport(order)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("dependencies under the reverse order: %d findings", lo.CountBy(previewed.Findings, func(finding Finding) bool {
		return finding.Section == "dependencies"
	}))
}

// compareDiagnostics fails the test if the report holds other counts of the editor's codes than the editor finds in the
// mod files.
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
	own := []string{"missing-message", "coverage"}
	got := map[string]int{}
	for _, finding := range report.Findings {
		if slices.Contains([]string{sectionDangling, "script", "localization"}, finding.Section) && !slices.Contains(own, finding.Code) {
			got[finding.Code] += lo.Ternary(finding.Section == sectionDangling, len(finding.Sites), 1)
		}
	}
	t.Logf("the editor's codes: %d places in the report, %d in the editor", lo.Sum(slices.Collect(maps.Values(got))),
		lo.Sum(slices.Collect(maps.Values(want))))
	if !maps.Equal(got, want) {
		t.Errorf("the report holds %v, the editor finds %v", got, want)
	}
}
