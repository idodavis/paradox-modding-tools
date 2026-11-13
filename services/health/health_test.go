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
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/internal/loc"
	"paradox-modding-tools/services/app"
	"paradox-modding-tools/services/lsp"
	"paradox-modding-tools/services/workspace"
)

// The ids and descriptor names of the fixture mods, and the languages of the
// scaffolds.
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
		"m.4 = { type = character_event immediate = { trigger_event = m.99 } }\n" +
		"m.5 = { type = character_event immediate = { trigger_event = m.99 } }\n",
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

// errorLogText is an error log in Victoria 3's format. Its first message goes
// on over lines that name a mod's file.
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

// createApp makes the application that carries the workspace's events, once per
// test run.
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

// createMod copies a game's fixture mod into a temporary folder and writes
// files over it. It returns the mod's record.
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

// createInstall copies a game's fixture install into a temporary folder, with
// user data that holds an error log, and returns the folder.
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

// openService opens a workspace on an install and returns a Health service over
// it.
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

// findFinding returns the finding of a code, name and database. It fails the
// test if there is none. An empty database matches any.
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

// TestBuildReportFindsContestsOverridesAndProblems checks a report over two
// fixture mods.
func TestBuildReportFindsContestsOverridesAndProblems(t *testing.T) {
	hlt := openService(t, "ck3", resolveFixture(t, "ck3"), createMod(t, "ck3", modA, modAFiles), createMod(t, "ck3", modB, modBFiles))
	report, err := hlt.BuildReport(nil)
	if err != nil {
		t.Fatal(err)
	}
	findings := listFindings(report)
	contest := findFinding(t, findings, "contest", "shared", "trait")
	if contest.Question != lsp.QuestionCompatibility || len(contest.Sites) != 2 || !contest.Sites[0].Kept ||
		contest.Sites[1].Kept || contest.Sites[0].Reason != lang.ReasonLast || contest.Sites[0].Place.Origin != modB {
		t.Errorf("contest of shared: %+v", contest)
	}
	shipped := findFinding(t, findings, "file-contest", "common/traits/m_traits.txt", lang.File)
	if len(shipped.Sites) != 2 || !shipped.Sites[0].Kept || shipped.Sites[1].Kept ||
		shipped.Sites[0].Place.Origin != modB || shipped.Sites[0].Range.StartLine != 1 {
		t.Errorf("contest of the file m_traits.txt: %+v", shipped)
	}
	if lo.ContainsBy(findings, func(finding Finding) bool { return finding.Name == "descriptor.mod" }) {
		t.Error("a descriptor, which the game does not read as a file, is a finding")
	}
	// the game's own v_traits.txt loads after the mods' m_traits.txt
	unused := findFinding(t, findings, "unused-copy", "brave", "trait")
	if unused.Facts["beaten"] != titleB || unused.Facts["winner"] == "" || len(unused.Sites) != 2 ||
		unused.Sites[0].Reason != lang.ReasonLast || !unused.Sites[0].Place.ReadOnly || !unused.Sites[0].Kept || unused.Sites[1].Kept {
		t.Errorf("unused copy of brave: %+v", unused)
	}
	replaced := findFinding(t, findings, "override", "events/v_events.txt", lang.File)
	if replaced.Facts["kept"] != "the game keeps the mod's" || len(replaced.Sites) != 2 || !replaced.Sites[0].Kept || replaced.Sites[1].Kept ||
		lo.ContainsBy(findings, func(finding Finding) bool { return finding.Code == "override" && finding.Name == "v.1" }) {
		t.Errorf("override of the file v_events.txt: %+v", replaced)
	}
	assertLocalization(t, findings)
	// the two references to m.99 are one finding that counts both
	dangling := findFinding(t, findings, "unresolved-reference", "m.99", "")
	if dangling.Question != lsp.QuestionHealth || len(dangling.Sites) != 1 || dangling.Sites[0].Range.StartLine != 3 ||
		dangling.Count != 2 || len(dangling.Repeats) != 1 || dangling.Repeats[0].Range.StartLine != 4 {
		t.Errorf("dangling reference: %+v", dangling)
	}
	if script := findFinding(t, findings, "unclosed-block", "", ""); script.Question != lsp.QuestionHealth {
		t.Errorf("script problem: %+v", script)
	}
	assertGroups(t, report, []string{modA, modB}, nil)
	if report.Compatibility.Worst != lsp.LevelCritical || report.Compatibility.Checks[0].Level != lsp.LevelCritical {
		t.Errorf("compatibility: worst %q, checks %+v", report.Compatibility.Worst, report.Compatibility.Checks)
	}
	previewed, err := hlt.BuildReport([]string{modB, modA})
	if err != nil {
		t.Fatal(err)
	}
	assertGroups(t, previewed, []string{modB, modA}, nil)
	kept := findFinding(t, listFindings(previewed), "file-contest", "common/traits/m_traits.txt", lang.File).Sites
	if len(kept) != 2 || !kept[0].Kept || kept[0].Place.Origin != modA {
		t.Errorf("contest of the file m_traits.txt with the first mod loaded last: %+v", kept)
	}
	if _, err := hlt.BuildReport([]string{modA}); err == nil {
		t.Error("an order that leaves out a mod was built")
	}
}

// listFindings returns the findings of a report's groups, the mods' languages
// too.
func listFindings(report Report) []Finding {
	findings := report.Compatibility.Findings
	for _, group := range report.Mods {
		findings = slices.Concat(findings, group.Findings, group.Languages)
	}
	return findings
}

// assertGroups checks that a report has a group for each mod in the order, and
// that each group's tally, checks and sort say what its findings do. The tally
// of the user's own mods leaves out the groups of the given external mods.
func assertGroups(t *testing.T, report Report, order []string, external map[string]bool) {
	t.Helper()
	if ids := lo.Map(report.Mods, func(group Group, _ int) string { return group.Mod }); !slices.Equal(ids, order) {
		t.Fatalf("the mods' groups are %v, want %v", ids, order)
	}
	var own []Finding
	for _, group := range slices.Concat([]Group{report.Compatibility}, report.Mods) {
		want := makeTally(group.Findings)
		if !maps.Equal(group.Counts, want.Counts) || group.Worst != want.Worst || len(group.Counts) != len(lsp.Levels) {
			t.Errorf("group %q: tally %+v, want %+v", group.Mod, group.Tally, want)
		}
		if checked := lo.SumBy(group.Checks, func(check Check) int { return check.Count }); checked != lo.Sum(slices.Collect(maps.Values(group.Counts))) {
			t.Errorf("group %q: the checks count %d findings, the tally %v", group.Mod, checked, group.Counts)
		}
		levels := lo.Map(group.Findings, func(finding Finding, _ int) int { return rankLevel(finding.Level) })
		if !slices.IsSorted(levels) {
			t.Errorf("group %q: findings are not sorted worst first: %v", group.Mod, levels)
		}
		if group.Mod != "" && !external[group.Mod] {
			own = slices.Concat(own, group.Findings)
		}
	}
	if want := makeTally(own); report.Own.Worst == "" || !maps.Equal(report.Own.Counts, want.Counts) {
		t.Errorf("the own mods' tally is %+v, want %+v", report.Own, want)
	}
	every := lo.FlatMap(report.Mods, func(group Group, _ int) []Finding { return group.Findings })
	if want := makeTally(slices.Concat(report.Compatibility.Findings, every)); !maps.Equal(report.Counts, want.Counts) {
		t.Errorf("the report's tally is %+v, want %+v", report.Tally, want)
	}
}

// assertLocalization checks the findings of a missing title message and of a
// partly written language.
func assertLocalization(t *testing.T, findings []Finding) {
	t.Helper()
	missing := findFinding(t, findings, "missing-message", "m.3", "")
	coverage := findFinding(t, findings, "coverage", german, "")
	if missing.Facts["key"] != "m.3.t" || coverage.Facts["mod"] != titleA ||
		coverage.Facts["have"] != "1" || coverage.Facts["of"] != "3" || len(coverage.Sites) != 1 {
		t.Errorf("localization: %+v, %+v", missing, coverage)
	}
}

// TestListErrorLogAddsContinuationLines checks that the lines after a message
// are part of it, and that a location on one of them gives the message its
// place.
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

// TestWriteScaffoldWritesMissingKeys checks that a scaffold writes each key a
// language lacks, and nothing for a name that is no language.
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
	// german has m_kept: m_orphan joins its file, and the replace folder's file
	// is written
	partial, err := hlt.WriteScaffold(modA, german)
	if err != nil || len(partial) != 2 ||
		!strings.HasSuffix(filepath.ToSlash(partial[1]), "localization/replace/german/m_replace_l_german.yml") {
		t.Fatalf("scaffold of german: %v, %v", partial, err)
	}
	if text, _ := files.ReadText(partial[0]); text.Text != "l_german:\n m_kept:0 \"Behalten\"\n m_orphan:0 \"Alone\"\n" {
		t.Errorf("german file %q", text.Text)
	}
	if _, err := hlt.WriteScaffold(modA, "../"+loc.BaseLanguage); err == nil {
		t.Error("a scaffold was written for a name that is no language")
	}
}

// TestBuildReportInstalls builds and logs the report of each real install that
// PMT_TEST_INSTALL_<GAME> names, with the dev mods. It fails if the report's
// counts of the editor's codes differ from the editor's.
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
			t.Logf("%d findings, %v counted, in %s", len(listFindings(report)), report.Counts,
				time.Since(started).Round(time.Millisecond))
			assertGroups(t, report, lo.Map(mods, func(mod app.Mod, _ int) string { return mod.ID }), hlt.listExternal())
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
	findings := listFindings(report)
	for _, finding := range findings {
		codes[finding.Code] += finding.Count
		levels[finding.Question+"/"+finding.Level] += finding.Count
	}
	for _, level := range slices.Sorted(maps.Keys(levels)) {
		t.Logf("question/level %s: %d", level, levels[level])
	}
	for _, code := range slices.Sorted(maps.Keys(codes)) {
		t.Logf("code %s: %d", code, codes[code])
	}
	for _, finding := range findings {
		if finding.Code == "removed-names" || finding.Code == "ignored-pair" || len(finding.Order) > 0 {
			t.Logf("%s %s: %s, level %s, mods %v, facts %v, order %v", finding.Code, finding.Database, finding.Name,
				finding.Level, finding.Mods, finding.Facts, finding.Order)
		}
	}
	for _, code := range []string{"contest", "override"} {
		listed := lo.Filter(findings, func(finding Finding, _ int) bool { return finding.Code == code })
		shipped := lo.CountBy(listed, func(finding Finding) bool { return finding.Database == lang.File })
		t.Logf("%s: %d names, %d files", code, len(listed)-shipped, shipped)
	}
}

// compareDiagnostics fails the test if the report's counts of the codes the
// editor gives differ from what the editor finds. The codes only Health's
// report finds are left out, and so is a priority tie that the report shows
// as an undecided contest.
func compareDiagnostics(t *testing.T, hlt *Service, report Report) {
	t.Helper()
	ws, err := hlt.Workspace.Session()
	if err != nil {
		t.Fatal(err)
	}
	mods := ws.Mods.Load()
	undecided := map[string]bool{}
	for _, finding := range listFindings(report) {
		if finding.Code == undecidedCode {
			undecided[finding.Facts["database"]+":"+finding.Facts["name"]] = true
		}
	}
	want := map[string]int{}
	for _, file := range mods.Files[len(ws.Install.Files):] {
		judged := file.Format.IsScript() || file.Format == files.FormatLocalization || file.Format == files.FormatDescriptor
		if _, known := mods.FindFile(file.Path); !known || !judged {
			continue
		}
		diagnostics, _ := lsp.DiagnoseFile(ws, mods, file.Path)
		for _, diagnostic := range diagnostics {
			tied := diagnostic.Code == index.ProblemPriority && undecided[diagnostic.Facts["database"]+":"+diagnostic.Facts["name"]]
			if !tied {
				want[diagnostic.Code]++
			}
		}
	}
	got := map[string]int{}
	for _, finding := range listFindings(report) {
		if !lsp.LookupCode(finding.Code).HealthOnly {
			got[finding.Code] += len(finding.Sites) + len(finding.Repeats)
		}
	}
	t.Logf("the editor's codes: %d places in the report, %d in the editor", lo.Sum(slices.Collect(maps.Values(got))),
		lo.Sum(slices.Collect(maps.Values(want))))
	if !maps.Equal(got, want) {
		t.Errorf("the report holds %v, the editor finds %v", got, want)
	}
}
