package lsp

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/samber/lo"
	"github.com/wailsapp/wails/v3/pkg/application"
	"golang.org/x/sync/errgroup"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/schema"
	"paradox-modding-tools/services/app"
	"paradox-modding-tools/services/workspace"
)

// The games' ids.
const (
	ck3  = "ck3"
	vic3 = "vic3"
	eu5  = "eu5"
)

// fixtureModID is the id the tests give the fixture mod.
const fixtureModID = "mod-a"

// The paths and codes the tests name more than once.
const (
	eventsPath           = "events/t_events.txt"
	messagesPath         = "localization/english/t_l_english.yml"
	genesPath            = "common/genes/m_genes.txt"
	traitsPath           = "common/traits/m_traits.txt"
	unresolvedReference  = "unresolved-reference"
	wrongNamespace       = "wrong-namespace"
	duplicateDefinition  = "duplicate-definition"
	conflictingPriority  = "conflicting-priority"
	artifactsPath        = "common/artifacts/z_artifacts.txt"
	artifactsDocsPath    = "game/common/artifacts/_artifacts.info"
	artifactsShippedPath = "game/common/artifacts/v_artifacts.txt"
)

// startApp makes the application that the workspace service sends its events through, once for the whole run.
var startApp sync.Once

// resolveFixture returns the absolute path of a folder under internal/testdata.
func resolveFixture(t *testing.T, parts ...string) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join(append([]string{"..", "..", "internal", "testdata"}, parts...)...))
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// copyFolder copies a folder into a temporary one, so that a test may change its files, and returns it.
func copyFolder(t *testing.T, source string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS(source)); err != nil {
		t.Fatal(err)
	}
	return dir
}

// writeFiles writes each text at its path under a folder, making the folders it needs.
func writeFiles(t *testing.T, dir string, texts map[string]string) {
	t.Helper()
	for rel, text := range texts {
		target := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// openService opens a workspace of English on an install with the given mods, and returns the language service
// over it. The workspace is closed when the test ends.
func openService(t *testing.T, gameID, installPath string, mods ...app.Mod) *Service {
	t.Helper()
	startApp.Do(func() { application.New(application.Options{}) })
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
	wsp := &workspace.Service{Config: config, Build: "test"}
	wsp.Open("w1")
	t.Cleanup(wsp.Close)
	if _, err := wsp.Session(); err != nil {
		t.Fatal(err)
	}
	return &Service{Workspace: wsp}
}

// openFixtures opens the fixture install of a game with a copy of its fixture mod for each of the given file sets, in
// order, named mod-a, mod-b and so on. It returns the service and the folders of the copies.
func openFixtures(t *testing.T, gameID string, texts ...map[string]string) (*Service, []string) {
	t.Helper()
	var dirs []string
	var mods []app.Mod
	for position, contents := range texts {
		dir := copyFolder(t, resolveFixture(t, "mods", gameID))
		writeFiles(t, dir, contents)
		dirs = append(dirs, dir)
		mods = append(mods, app.Mod{ID: "mod-" + string(rune('a'+position)), Dir: dir})
	}
	return openService(t, gameID, resolveFixture(t, gameID), mods...), dirs
}

// openFixture opens the fixture install of a game with a copy of its fixture mod, mod-a, with the given files added. It
// returns the service and the copy's folder.
func openFixture(t *testing.T, gameID string, texts map[string]string) (*Service, string) {
	t.Helper()
	ide, dirs := openFixtures(t, gameID, texts)
	return ide, dirs[0]
}

// listCodes returns the codes of some diagnostics, sorted.
func listCodes(diagnostics []Diagnostic) []string {
	codes := lo.Map(diagnostics, func(diagnostic Diagnostic, _ int) string { return diagnostic.Code })
	slices.Sort(codes)
	return codes
}

// checkCodes diagnoses each file under a folder and checks that its codes are the ones listed for it.
func checkCodes(t *testing.T, ide *Service, dir string, want map[string][]string) {
	t.Helper()
	for rel, codes := range want {
		diagnostics, err := ide.Diagnose(filepath.Join(dir, filepath.FromSlash(rel)))
		if got := listCodes(diagnostics); err != nil || !slices.Equal(got, codes) {
			t.Errorf("%s: %v, %v, want %v", rel, got, err, codes)
		}
		for _, diagnostic := range diagnostics {
			if !lo.ContainsBy(Codes, func(row Code) bool { return row.ID == diagnostic.Code }) {
				t.Errorf("%s: %s is in no row of Codes", rel, diagnostic.Code)
			}
		}
	}
}

// TestToRangeCountsUtf16Units checks that a range counts its columns in UTF-16 code units, one for a character of
// two bytes and two for a character above U+FFFF, and that its lines and columns start from one.
func TestToRangeCountsUtf16Units(t *testing.T) {
	text := "a = \"é😀x\"\nnext = 1\n"
	lines := jomini.NewLines(text)
	start := strings.Index(text, "x")
	if got, want := ToRange(text, lines, jomini.Span{Start: start, End: start + 1}), (Range{1, 9, 1, 10}); got != want {
		t.Errorf("after a surrogate pair: %+v, want %+v", got, want)
	}
	start = strings.Index(text, "next")
	if got, want := ToRange(text, lines, jomini.Span{Start: start, End: start + 4}), (Range{2, 1, 2, 5}); got != want {
		t.Errorf("on the next line: %+v, want %+v", got, want)
	}
	if got, want := ToRange(text, lines, jomini.Span{Start: 0, End: len(text)}), (Range{1, 1, 3, 1}); got != want {
		t.Errorf("the whole text: %+v, want %+v", got, want)
	}
}

// TestToOffsetInvertsToRange checks that every character boundary of a text converts to a range start and back to
// the same offset, and that a position past the end of its line or of the text gives the nearest offset.
func TestToOffsetInvertsToRange(t *testing.T) {
	text := "a = \"é😀x\"\nnext = 1\n"
	lines := jomini.NewLines(text)
	for offset := range len(text) + 1 {
		if offset < len(text) && !strings.HasPrefix(text[offset:], string([]rune(text[offset:])[:1])) {
			continue
		}
		span := ToRange(text, lines, jomini.Span{Start: offset, End: offset})
		if got := ToOffset(text, lines, span.StartLine, span.StartColumn); got != offset {
			t.Errorf("offset %d came back as %d from %+v", offset, got, span)
		}
	}
	if got := ToOffset(text, lines, 1, 99); got != strings.Index(text, "\n") {
		t.Errorf("a column past the end of the line: %d", got)
	}
	if got := ToOffset(text, lines, 99, 1); got != len(text) {
		t.Errorf("a line past the end of the text: %d", got)
	}
}

// TestSetTextAnswersWithProblemsUntilFileCloses checks that SetText judges the editor's text, that Hover reads the same
// text, and that CloseFile gives back the file's own.
func TestSetTextAnswersWithProblemsUntilFileCloses(t *testing.T) {
	ide, dir := openFixture(t, ck3, nil)
	target := filepath.Join(dir, filepath.FromSlash(traitsPath))
	diagnostics, err := ide.SetText(target, "bold = {\n\tcategory = personality\n\tdiplomacy = \"very high\"\n}\n")
	if err != nil || !slices.Equal(listCodes(diagnostics), []string{"wrong-value"}) {
		t.Fatalf("SetText answered %+v, %v", diagnostics, err)
	}
	if want := (Range{3, 14, 3, 25}); diagnostics[0].Range != want {
		t.Errorf("the wrong value is at %+v, want %+v", diagnostics[0].Range, want)
	}
	if card, err := ide.Hover(target, 3, 3); err != nil || card == nil || card.Title != "diplomacy" || card.What != "field" {
		t.Errorf("Hover over the open text: %+v, %v", card, err)
	}
	if err := ide.CloseFile(target); err != nil {
		t.Fatal(err)
	}
	if diagnostics, err = ide.Diagnose(target); err != nil || len(diagnostics) != 0 {
		t.Errorf("the closed file's own text: %+v, %v", diagnostics, err)
	}
}

// TestDiagnoseReportsEachVerdict checks the code of each problem in the CK3 fixture's files and in two mods written
// for it, including the codes of the parser and of localization.
func TestDiagnoseReportsEachVerdict(t *testing.T) {
	ide, dirs := openFixtures(t, ck3, map[string]string{
		eventsPath: "namespace = t\n\nt.1 = {\n\ttype = character_event\n\ttrigger_event = t.99\n" +
			"\timmediate = {\n\t\tcapital_county = { add_gold = 1 }\n\t}\n}\nother.1 = {\n\ttype = character_event\n}\n",
		"events/t_tie.txt":           "namespace = v\n\nv.1 = {\n\tid_override_priority = 5\n\ttype = character_event\n}\n",
		"events/t_default.txt":       "namespace = n\n\nn.1 = {\n\ttype = character_event\n}\n",
		"common/traits/d_traits.txt": "dup_trait = {\n\tcategory = personality\n}\n",
		"common/traits/t_traits.txt": "bold = {\n\tcategory = personality\n\tdiplomacy = \"very high\"\n}\n",
		"common/traits/t_broken.txt": "broken = {\n\tcategory = personality\n",
		messagesPath:                 "l_english:\n a:0 \"x\"\n a:0 \"y\"\n broken\n",
	}, map[string]string{
		"events/u_tie.txt":           "namespace = v\n\nv.1 = {\n\tid_override_priority = 5\n\ttype = character_event\n}\n",
		"events/u_default.txt":       "namespace = n\n\nn.1 = {\n\ttype = character_event\n}\n",
		"common/traits/e_traits.txt": "dup_trait = {\n\tcategory = personality\n}\n",
	})
	checkCodes(t, ide, dirs[0], map[string][]string{
		eventsPath:                   {unresolvedReference, wrongNamespace, "wrong-scope"},
		"events/t_tie.txt":           {conflictingPriority},
		"events/t_default.txt":       {conflictingPriority},
		"common/traits/d_traits.txt": {},
		traitsPath:                   {},
		"common/traits/t_traits.txt": {"wrong-value"},
		"common/traits/t_broken.txt": {"unclosed-block"},
		messagesPath:                 {"duplicate-message", "duplicate-message", "malformed-line", "missing-bom"},
		"events/m_events.txt":        {},
	})
	checkCodes(t, ide, dirs[1], map[string][]string{
		"events/u_tie.txt":           {conflictingPriority},
		"events/u_default.txt":       {conflictingPriority},
		"common/traits/e_traits.txt": {},
	})
}

// TestDiagnoseGradesEntryModesAndFirstDefinitions checks in the Vic3 fixture that an entry mode in its place is quiet
// and one out of place is reported. It checks that the later of two definitions is a duplicate only within one mod.
func TestDiagnoseGradesEntryModesAndFirstDefinitions(t *testing.T) {
	event := "namespace = ww\n\nww.1 = {\n\ttype = country_event\n}\n"
	ide, dirs := openFixtures(t, vic3, map[string]string{
		genesPath:              "INJECT:hair = {\n\tgenes = { }\n}\n",
		"events/zz_events.txt": event, "events/zza_events.txt": event,
	}, map[string]string{"events/zzz_events.txt": event, "events/zzzz_events.txt": event})
	checkCodes(t, ide, dirs[0], map[string][]string{
		"common/country_definitions/m_countries.txt": {},
		genesPath:               {"misplaced-mode"},
		"events/zz_events.txt":  {},
		"events/zza_events.txt": {duplicateDefinition},
	})
	checkCodes(t, ide, dirs[1], map[string][]string{"events/zzz_events.txt": {}, "events/zzzz_events.txt": {}})
}

// TestDiagnoseReportsKeysEveryShippedDefinitionWrites checks that a missing key is reported only if every definition
// the game ships writes it.
func TestDiagnoseReportsKeysEveryShippedDefinitionWrites(t *testing.T) {
	docs := "weight = <int> # How heavy it is. Required.\n"
	for shipped, want := range map[string][]string{
		"anvil = {\n\tweight = 5\n}\n":              {"missing-key"},
		"anvil = {\n\tweight = 5\n}\nbell = {\n}\n": {},
	} {
		install := copyFolder(t, resolveFixture(t, ck3))
		writeFiles(t, install, map[string]string{artifactsDocsPath: docs, artifactsShippedPath: shipped})
		dir := copyFolder(t, resolveFixture(t, "mods", ck3))
		writeFiles(t, dir, map[string]string{artifactsPath: "rock = {\n}\npebble = {\n\tweight = 1\n}\n"})
		ide := openService(t, ck3, install, app.Mod{ID: fixtureModID, Dir: dir})
		diagnostics, err := ide.Diagnose(filepath.Join(dir, filepath.FromSlash(artifactsPath)))
		if got := listCodes(diagnostics); err != nil || !slices.Equal(got, want) ||
			len(want) > 0 && (diagnostics[0].Facts["key"] != "weight" || diagnostics[0].Facts["name"] != "rock") {
			t.Errorf("with %q shipped: %+v, %v", shipped, diagnostics, err)
		}
	}
}

// TestHoverNamesWhatItIs checks the card of each kind of thing a position can be on in the CK3 fixture: a definition, a
// reference, an effect, a keyword, a field, a saved scope, an image path and nothing.
func TestHoverNamesWhatItIs(t *testing.T) {
	ide, dir := openFixture(t, ck3, nil)
	target := filepath.Join(dir, "events", "m_events.txt")
	hover := func(line, column int) *Card {
		t.Helper()
		card, err := ide.Hover(target, line, column)
		if err != nil {
			t.Fatal(err)
		}
		return card
	}
	if card := hover(3, 1); card == nil || card.What != "event" || len(card.Sites) != 1 || !card.Sites[0].Kept ||
		card.Sites[0].Place.Origin != fixtureModID || card.Sites[0].Line != 3 || card.Range != (Range{3, 1, 3, 4}) {
		t.Errorf("a definition: %+v", card)
	}
	if card := hover(11, 19); card == nil || card.What != "event" || card.Title != "v.2" || len(card.Sites) != 1 ||
		card.Sites[0].Place.Origin != game.OriginVanilla || !card.Sites[0].Place.ReadOnly {
		t.Errorf("a reference: %+v", card)
	}
	if card := hover(14, 5); card == nil || card.What != "effect" || len(card.Scopes) == 0 ||
		card.Scopes[0].Link != "this" || card.Scopes[0].Type != "character" {
		t.Errorf("an effect: %+v", card)
	}
	if card := hover(13, 5); card == nil || card.What != "keyword" || card.Class != "saving" || card.Blurb == "" {
		t.Errorf("a keyword: %+v", card)
	}
	if card := hover(4, 3); card == nil || card.What != "field" || card.Takes.Type == "" || !card.Inferred {
		t.Errorf("a field: %+v", card)
	}
	if card := hover(15, 10); card == nil || card.What != "saved scope" || card.Title != "made" || len(card.Sites) != 1 {
		t.Errorf("a saved scope: %+v", card)
	}
	if card := hover(2, 1); card != nil {
		t.Errorf("a position on nothing: %+v", card)
	}
	window := filepath.Join(resolveFixture(t, ck3), "game", "gui", "v_window.gui")
	if card, err := ide.Hover(window, 20, 16); err != nil || card == nil || card.Image == nil || card.Image.Format != "dds" {
		t.Errorf("a written path: %+v, %v", card, err)
	}
}

// TestHoverReadsDatesAndColors checks that a date is read in the calendar of the mod that owns the file and that a
// color block gets its CSS color.
func TestHoverReadsDatesAndColors(t *testing.T) {
	install := copyFolder(t, resolveFixture(t, ck3))
	writeFiles(t, install, map[string]string{artifactsShippedPath: "anvil = {\n\tmade = 1000.1.1\n\tage = 7.5\n}\n"})
	dir := copyFolder(t, resolveFixture(t, "mods", ck3))
	months := []string{"A", "B", "C", "D", "E", "F", "G", "H", "I", "J", "K", "L"}
	calendar := app.Calendar{Eras: []app.Era{{Name: "Old", Year: 1}, {Name: "New", Year: 1000}}, Months: months}
	ide := openService(t, ck3, install, app.Mod{ID: fixtureModID, Dir: dir, Calendar: calendar})
	target := filepath.Join(dir, filepath.FromSlash(artifactsPath))
	if _, err := ide.SetText(target, "x = {\n\tmade = 1066.9.15\n\tcolor = rgb { 255 0 0 }\n\tage = 10.12\n}\n"); err != nil {
		t.Fatal(err)
	}
	card, err := ide.Hover(target, 2, 12)
	want := CardDate{Date: jomini.Date{Year: 1066, Month: 9, Day: 15}, Era: "New", Month: "I"}
	if err != nil || card == nil || card.Date == nil || *card.Date != want {
		t.Errorf("a date: %+v, %v, want %+v", card, err, want)
	}
	if card, err = ide.Hover(target, 4, 10); err != nil || card != nil && card.Date != nil {
		t.Errorf("a number that reads as a date: %+v, %v", card, err)
	}
	if card, err = ide.Hover(target, 3, 3); err != nil || card == nil || card.Color != "rgba(255, 0, 0, 1)" {
		t.Errorf("a color: %+v, %v", card, err)
	}
}

// TestDiagnoseJudgesOnlySyntaxOfGameFiles checks that a file of the game's own gets its parse errors and no
// verdict, whatever the text says.
func TestDiagnoseJudgesOnlySyntaxOfGameFiles(t *testing.T) {
	ide, _ := openFixture(t, ck3, nil)
	vanilla := filepath.Join(resolveFixture(t, ck3), "game", "common", "traits", "v_traits.txt")
	diagnostics, err := ide.SetText(vanilla, "brave = {\n\tdiplomacy = \"very high\"\n\ttrigger_event = nothing_defines_this\n")
	if err != nil || !slices.Equal(listCodes(diagnostics), []string{"unclosed-block"}) {
		t.Errorf("a game file: %+v, %v", diagnostics, err)
	}
}

// TestEncodeTokensWritesDeltas checks that tokens are sorted and written as deltas, and that a token on two lines is
// left out.
func TestEncodeTokensWritesDeltas(t *testing.T) {
	text := "ab cd\nef\n  gh😀i\n\"x\ny\" z\n"
	tokens := []token{
		{jomini.Span{Start: 11, End: 18}, schema.CategoryReference}, {jomini.Span{Start: 3, End: 5}, schema.CategoryCommand},
		{jomini.Span{Start: 0, End: 2}, schema.CategoryKeyword}, {jomini.Span{Start: 6, End: 8}, schema.CategoryField},
		{jomini.Span{Start: 19, End: 24}, schema.CategoryField}, {jomini.Span{Start: 9, End: 10}, schema.CategoryNone},
	}
	want := []uint32{0, 0, 2, 0, 0, 0, 3, 2, 1, 0, 1, 0, 2, 3, 0, 1, 2, 5, 5, 0}
	if got := encodeTokens(text, jomini.NewLines(text), tokens); !slices.Equal(got, want) {
		t.Errorf("encoded %v, want %v", got, want)
	}
}

// TestTokensEncodeWholeFile checks that Tokens writes five integers per token and that each type is in the legend.
func TestTokensEncodeWholeFile(t *testing.T) {
	ide, dir := openFixture(t, ck3, nil)
	tokens, err := ide.Tokens(filepath.Join(dir, "events", "m_events.txt"))
	if err != nil || len(tokens) < 50 || len(tokens)%5 != 0 {
		t.Fatalf("%d integers, %v", len(tokens), err)
	}
	for position := 3; position < len(tokens); position += 5 {
		if int(tokens[position]) >= len(ide.ListLegend().Types) {
			t.Errorf("token %d has type %d outside the legend", position/5, tokens[position])
		}
	}
}

// TestFormatKeepsLinesAndStaysIdempotent checks that Format only indents and spaces: every line break and comment
// stays where it is, and formatting the result changes nothing.
func TestFormatKeepsLinesAndStaysIdempotent(t *testing.T) {
	ide, dir := openFixture(t, ck3, nil)
	target := filepath.Join(dir, "common", "traits", "m_traits.txt")
	ws, _ := ide.Workspace.Session()
	format := func(text string) string {
		t.Helper()
		if _, err := ide.SetText(target, text); err != nil {
			t.Fatal(err)
		}
		edits, err := ide.Format(target, 4, true)
		tree, _ := ws.LoadTree(target)
		if err != nil {
			t.Fatal(err)
		}
		return jomini.ApplyEdits(tree.Src, toByteEdits(tree, edits))
	}
	text := "a = {b = 1   c=2}\n# note\nd = { # trailing\n  e = 1  f = 2\n}\n\n\ng = {}\ntypes Z {\nh = { i = 1 }\n}\n"
	want := "a = { b = 1 c = 2 }\n# note\nd = { # trailing\n  e = 1 f = 2\n}\n\n\ng = { }\ntypes Z {\n  h = { i = 1 }\n}\n"
	if got := format(text); got != want {
		t.Errorf("formatted %q as %q, want %q", text, got, want)
	}
	if again := format(want); again != want {
		t.Errorf("a second format changed %q to %q", want, again)
	}
	stray, fixed := "x = {\r\r\n\ta = 1\r\n}\r\n", "x = {\r\n\ta = 1\r\n}\r\n"
	strayTree := jomini.Parse(stray)
	if got := encodeTree(strayTree, listPieces(strayTree), "\t"); got != fixed {
		t.Errorf("a stray carriage return: %q, want %q", got, fixed)
	}
}

// toByteEdits converts Monaco edits over a tree's text back to byte edits.
func toByteEdits(tree *jomini.Tree, edits []Edit) []jomini.Edit {
	return lo.Map(edits, func(edit Edit, _ int) jomini.Edit {
		return jomini.Edit{
			Span: jomini.Span{
				Start: ToOffset(tree.Src, tree.Lines, edit.Range.StartLine, edit.Range.StartColumn),
				End:   ToOffset(tree.Src, tree.Lines, edit.Range.EndLine, edit.Range.EndColumn),
			},
			Text: edit.Text,
		}
	})
}

// TestCompleteListsKeysAndValues checks that a key gets the language's operators, commands and fields, and that a value
// gets the field's choices and the image files under the folder typed.
func TestCompleteListsKeysAndValues(t *testing.T) {
	ide, dir := openFixture(t, ck3, nil)
	install := copyFolder(t, resolveFixture(t, ck3))
	writeFiles(t, install, map[string]string{
		artifactsDocsPath: "icon = <texture> # Its icon.\n", artifactsShippedPath: "anvil = {\n\ticon = x\n}\n",
	})
	artifacts := openService(t, ck3, install, app.Mod{ID: fixtureModID, Dir: dir})
	things := filepath.Join(dir, "common", "artifacts", "t_artifacts.txt")
	if _, err := artifacts.SetText(things, "x = {\n\ticon = \"gfx/interface/\n}\n"); err != nil {
		t.Fatal(err)
	}
	paths, err := artifacts.Complete(things, 2, 24)
	if labels := lo.Map(paths, func(candidate Completion, _ int) string { return candidate.Label }); err != nil ||
		!slices.Contains(labels, "gfx/interface/v_bg.dds") || slices.Contains(labels, "gfx/coat_of_arms/patterns/v_pattern.dds") {
		t.Errorf("image paths: %v, %v", labels, err)
	}
	traits := filepath.Join(dir, "common", "traits", "m_traits.txt")
	if _, err := ide.SetText(traits, "x = {\n\tcategory = \n\t\n}\n"); err != nil {
		t.Fatal(err)
	}
	choices, _ := ide.Complete(traits, 2, 13)
	if labels := lo.Map(choices, func(candidate Completion, _ int) string { return candidate.Label }); !slices.Equal(labels, []string{"personality", "education"}) &&
		!slices.Equal(labels, []string{"education", "personality"}) {
		t.Errorf("choices: %v", labels)
	}
	// a value typed in a block written on one line is a value position too, and offers no keys
	if _, err := ide.SetText(traits, "x = { category = pe }\n"); err != nil {
		t.Fatal(err)
	}
	inline, _ := ide.Complete(traits, 1, 20)
	if labels := lo.Map(inline, func(candidate Completion, _ int) string { return candidate.Label }); len(labels) != 2 {
		t.Errorf("choices in a block on one line: %v", labels)
	}
	events := filepath.Join(dir, "events", "t_events.txt")
	if _, err := ide.SetText(events, "namespace = t\nt.1 = {\n\timmediate = {\n\t\t\n\t}\n}\n"); err != nil {
		t.Fatal(err)
	}
	keys, err := ide.Complete(events, 4, 3)
	if err != nil || len(keys) == 0 || keys[0].Category != schema.CategoryKeyword ||
		!lo.ContainsBy(keys, func(candidate Completion) bool {
			return candidate.Label == "add_gold" && candidate.Category == schema.CategoryCommand
		}) {
		t.Errorf("keys: %d, %v", len(keys), err)
	}
}

// TestNavigationFollowsNamesAcrossFiles checks that Definition puts the kept definition first, that References lists
// the definition and the uses, that PrepareRename refuses a name of the game, and that Rename edits each place in the
// mod.
func TestNavigationFollowsNamesAcrossFiles(t *testing.T) {
	ide, dir := openFixture(t, ck3, nil)
	target := filepath.Join(dir, "events", "m_events.txt")
	locations, err := ide.Definition(target, 11, 19)
	if err != nil || len(locations) != 1 || !locations[0].Place.ReadOnly || locations[0].Range.StartLine < 1 {
		t.Errorf("Definition: %+v, %v", locations, err)
	}
	uses, err := ide.References(target, 3, 1)
	if err != nil || len(uses) != 2 {
		t.Errorf("References: %+v, %v", uses, err)
	}
	if renamable, err := ide.PrepareRename(target, 3, 1); err != nil || renamable == nil || *renamable != (Range{3, 1, 3, 4}) {
		t.Errorf("PrepareRename on a mod's name: %+v, %v", renamable, err)
	}
	if refused, err := ide.PrepareRename(target, 11, 19); err != nil || refused != nil {
		t.Errorf("PrepareRename on the game's name: %+v, %v", refused, err)
	}
	edits, err := ide.Rename(target, 3, 1, "m.9")
	if got := edits[target]; err != nil || len(edits) != 1 || len(got) != 2 || got[0].Text != "m.9" {
		t.Errorf("Rename: %+v, %v", edits, err)
	}
}

// TestHoverReadsLocalizationFiles checks the cards of a localization file: a message's key, a key between dollar signs
// and a step of a data call.
func TestHoverReadsLocalizationFiles(t *testing.T) {
	ide, dir := openFixture(t, ck3, map[string]string{
		messagesPath: "l_english:\n a:0 \"Hello $b$ [Root.GetName]\"\n b:0 \"Bee\"\n",
	})
	target := filepath.Join(dir, "localization", "english", "t_l_english.yml")
	if card, err := ide.Hover(target, 3, 2); err != nil || card == nil || card.What != "localization key" || card.Blurb != "Bee" || len(card.Sites) != 1 {
		t.Errorf("a key: %+v, %v", card, err)
	}
	if card, err := ide.Hover(target, 2, 14); err != nil || card == nil || card.Title != "b" || card.Blurb != "Bee" {
		t.Errorf("a key in a text: %+v, %v", card, err)
	}
	if card, err := ide.Hover(target, 2, 19); err != nil || card == nil || card.What != "data function" {
		t.Errorf("a step: %+v, %v", card, err)
	}
}

// TestHoverGoldenPositions hovers over every golden position in the fixtures of each game and checks that none panics.
// It logs the positions where the golden has a card and Hover has none.
func TestHoverGoldenPositions(t *testing.T) {
	header := regexp.MustCompile(`^(\d+):(\d+) (.*)$`)
	for _, gameID := range []string{ck3, vic3, eu5} {
		ide, _ := openFixture(t, gameID, nil)
		ws, _ := ide.Workspace.Session()
		listing, _ := filepath.Glob(filepath.Join(resolveFixture(t, "golden"), "hover-"+gameID+"-*.txt"))
		positions, missed := 0, []string{}
		for _, golden := range listing {
			base := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(golden), "hover-"+gameID+"-"), ".txt")
			origin, rel, _ := strings.Cut(base, "-")
			// a golden is named for its file's path, with the extension left off
			rel = strings.ReplaceAll(rel, "__", "/")
			position := slices.IndexFunc(ws.Mods.Load().Files, func(file files.File) bool {
				stem := strings.TrimSuffix(file.Rel, path.Ext(file.Rel))
				return (file.Origin == game.OriginVanilla) == (origin == "vanilla") && (stem == rel || strings.HasSuffix(stem, "/"+rel))
			})
			if position < 0 {
				t.Errorf("%s: no file %s in the fixture", filepath.Base(golden), rel)
				continue
			}
			text, _ := os.ReadFile(golden)
			entries := strings.Split(string(text), "\n")
			for number, entry := range entries {
				match := header.FindStringSubmatch(entry)
				if match == nil {
					continue
				}
				line, _ := strconv.Atoi(match[1])
				column, _ := strconv.Atoi(match[2])
				// a golden position without a card ends its first line with a dash; one with a card is followed by its fields
				fields := strings.Join(entries[number+1:min(number+8, len(entries))], "\n")
				hasCard := !strings.HasSuffix(match[3], " -") && strings.Contains(fields, `"head"`)
				card, panicked := hoverSafely(ide, ws.Mods.Load().Files[position].Path, line+1, column+1)
				positions++
				switch {
				case panicked:
					t.Errorf("%s %s:%d:%d panicked", gameID, rel, line, column)
				case card == nil && hasCard:
					missed = append(missed, fmt.Sprintf("%s:%d:%d %s", rel, line, column, match[3]))
				}
			}
		}
		t.Logf("%s: %d positions, %d where the golden had a card and Hover has none", gameID, positions, len(missed))
		for _, miss := range missed {
			t.Logf("  none at %s", miss)
		}
	}
}

// hoverSafely hovers at a position, and reports whether the hover panicked.
func hoverSafely(ide *Service, target string, line, column int) (card *Card, panicked bool) {
	defer func() { panicked = recover() != nil }()
	card, _ = ide.Hover(target, line, column)
	return card, false
}

// A problem is one diagnostic with the file it was found in.
type problem struct {
	code, rel string
	line      int
	facts     map[string]string
}

// TestDiagnoseInstalls diagnoses the files of each real install that PMT_TEST_INSTALL_<GAME> names, with no mod, and
// logs the problems by code. It fails only if a file panics.
func TestDiagnoseInstalls(t *testing.T) {
	for _, gameID := range []string{ck3, vic3, eu5} {
		t.Run(gameID, func(t *testing.T) {
			variable := "PMT_TEST_INSTALL_" + strings.ToUpper(gameID)
			install := os.Getenv(variable)
			if install == "" {
				t.Skipf("%s is not set", variable)
			}
			opening := time.Now()
			ide := openService(t, gameID, install)
			ws, _ := ide.Workspace.Session()
			started := time.Now()
			problems, checked, failures := diagnoseAll(ide, ws.Install.Files)
			t.Logf("opened in %s; diagnosed %d files in %s: %d problems, %d files unread or crashed", started.Sub(opening).Round(time.Second),
				checked, time.Since(started).Round(time.Millisecond), len(problems), len(failures))
			logProblems(t, problems)
			for _, failure := range failures {
				if errors.Is(failure, errCrash) {
					t.Error(failure)
				} else {
					t.Log(failure)
				}
			}
		})
	}
}

// diagnoseAll diagnoses the files of a list in parallel. It returns the problems, sorted by code and place, how many
// files it checked and why each failed file failed.
func diagnoseAll(ide *Service, listing []files.File) (problems []problem, checked int, failures []error) {
	var mu sync.Mutex
	var group errgroup.Group
	group.SetLimit(max(runtime.NumCPU()-2, 1))
	for _, file := range listing {
		switch file.Format {
		case files.FormatScript, files.FormatGUI, files.FormatAsset, files.FormatFont, files.FormatGFX, files.FormatLocalization:
			checked++
			group.Go(func() error {
				diagnostics, err := diagnoseSafely(ide, file.Path)
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					failures = append(failures, fmt.Errorf("%s: %w", file.Rel, err))
				}
				for _, diagnostic := range diagnostics {
					problems = append(problems, problem{diagnostic.Code, file.Rel, diagnostic.Range.StartLine, diagnostic.Facts})
				}
				return nil
			})
		}
	}
	_ = group.Wait()
	slices.SortFunc(problems, func(first, second problem) int {
		return cmp.Or(strings.Compare(first.code, second.code), strings.Compare(first.rel, second.rel), cmp.Compare(first.line, second.line))
	})
	return problems, checked, failures
}

// errCrash is the error a panic becomes.
var errCrash = errors.New("panic")

// diagnoseSafely diagnoses a file, and turns a panic into an error.
func diagnoseSafely(ide *Service, target string) (diagnostics []Diagnostic, err error) {
	defer func() {
		if crash := recover(); crash != nil {
			err = fmt.Errorf("%w: %v", errCrash, crash)
		}
	}()
	return ide.Diagnose(target)
}

// logProblems logs the first problems of each code, then the count of each code.
func logProblems(t *testing.T, problems []problem) {
	t.Helper()
	counts := map[string]int{}
	for _, current := range problems {
		if counts[current.code]++; counts[current.code] <= 10 {
			t.Logf("%s %s:%d %v", current.code, current.rel, current.line, current.facts)
		}
	}
	for _, code := range slices.Sorted(maps.Keys(counts)) {
		t.Logf("%s: %d", code, counts[code])
	}
}
