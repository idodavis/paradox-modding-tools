package draw

import (
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
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/services/app"
	"paradox-modding-tools/services/lsp"
	"paradox-modding-tools/services/workspace"
)

// The types, groups and names the tests use.
const (
	typeButton  = "v_button"
	typeWidget  = "widget"
	typeLabel   = "v_label"
	groupFlag   = "GetVariableSystem.Exists('')"
	groupAlive  = "GetPlayer.IsAlive"
	groupTrait  = "GetTrait('').IsValid"
	flagA       = "d_a"
	flagB       = "d_b"
	halfAlpha   = "0.5"
	traitBold   = "bold"
	cssRed      = "rgba(255, 0, 0, 1)"
	cssBlue     = "rgba(0, 0, 255, 1)"
	cssGreen    = "rgba(0, 255, 0, 1)"
	emblemKey   = "colored_emblem"
	slotOne     = "color1"
	slotTwo     = "color2"
	coatParent  = "d_parent"
	windowName  = "d_window"
	modID       = "mod-a"
	patternFile = "v_pattern.dds"
	otherFile   = "other.dds"
)

// installGames are the games whose real installs the gates run on.
var installGames = []string{"ck3", "vic3", "eu5"}

// The files the fixture mod adds, from the mod's folder.
var (
	modWindowPath = filepath.Join("gui", "d_window.gui")
	modCoatPath   = filepath.Join("common", "coat_of_arms", "coat_of_arms", "d_coats.txt")
)

// listsPath is the file the fixture install gets for its template lists, from the install's folder.
var listsPath = filepath.Join("game", "common", "coat_of_arms", "template_lists", "v_lists.txt")

// listsText is what the fixture install's template lists file holds: a list with a heaviest option and a special
// selection that is heavier still, and a list whose options weigh the same.
const listsText = `coat_of_arms_template_lists = {
	d_heavy = {
		10 = "other.dds"
		30 = "v_pattern.dds"
		special_selection = {
			trigger = { always = yes }
			99 = "special.dds"
		}
	}
	d_equal = {
		5 = "first.dds"
		5 = "second.dds"
	}
}
`

// modFiles are the files the fixture mod adds: windows whose widgets read groups in conditions of every depth and
// combination, a type with a block that a window overrides, widgets whose children come in every place, and coats with
// a parent, lists, a sub and named colors.
var modFiles = map[string]string{
	modWindowPath: `template d_tint {
	alpha = 0.5
}
type d_holder = vbox {
	block "d_slot" { widget = { name = "d_default" } }
}
type d_card = widget {
	d_holder = { }
}
type d_self = widget {
	d_self = { name = "d_inner_self" }
}
type d_twin = widget {
	name = "d_twin_type"
}
window = {
	name = "d_window"
	using = d_nowhere
	widget = {
		name = "d_both"
		visible = "[And2(GetVariableSystem.Exists('d_a'), GetPlayer.IsAlive)]"
	}
	widget = {
		name = "d_not"
		visible = "[Not( GetPlayer.IsAlive )]"
	}
	widget = {
		name = "d_outer"
		visible = "[GetVariableSystem.Exists('d_b')]"
		icon = { name = "d_inner" using = d_tint visible = "[GetPlayer.IsAlive]" }
	}
	widget = { name = "d_off" visible = no }
	widget = {
		name = "d_parent"
		visible = "[GetVariableSystem.Exists('d_a')]"
		widget = { name = "d_child" visible = "[GetVariableSystem.Exists('d_b')]" }
	}
	widget = { name = "d_trait_a" visible = "[GetTrait('brave').IsValid]" }
	widget = { name = "d_trait_b" visible = "[GetTrait('bold').IsValid]" }
}
window = {
	name = "d_override_window"
	d_card = {
		blockoverride "d_slot" { widget = { name = "d_passed" } }
	}
	D_HOLDER = { blockoverride "d_slot" { widget = { name = "d_own" } } }
}
window = {
	name = "d_order_window"
	widget = {
		name = "d_item_holder"
		item = { widget = { name = "d_item" } }
		widget = { name = "d_plain" }
		buttontext = { widget = { name = "d_label" } }
		tooltipwidget = { widget = { name = "d_tip" } }
		icon = { name = "d_slash" texture = "/gfx/interface/v_bg.dds" }
		widget = {
			name = "d_state"
			alpha = 1
			state = { name = _show alpha = 0.5 }
		}
	}
}
window = {
	name = "d_text_window"
	textbox = { name = "d_raw" raw_text = "Plain $v_key$ text" visible = "[GetPlayer.IsAlive]" }
	textbox = { name = "d_unknown" text = "no_such_key" }
}
window = {
	name = "d_self_window"
	d_self = { }
}
window = {
	name = "d_twin"
	alpha = 0.25
}
`,
	modCoatPath: `d_parent = {
	pattern = "v_pattern.dds"
	color1 = d_red
	color2 = "d_blue"
	colored_emblem = {
		texture = "v_emblem.dds"
		color1 = color2
		mask = { 1 2 }
		instance = { position = { 0.25 0.75 } scale = { 0.5 0.5 } rotation = 45 depth = 2 }
		instance = { depth = 1 }
	}
}
d_child = {
	parent = d_parent
	color1 = rgb { 0 255 0 }
}
d_listed = {
	pattern = list "d_heavy"
	color1 = list "d_nowhere_list"
	colored_emblem = { texture = list "d_equal" }
	colored_emblem = { texture = list "d_nowhere_list" }
	sub = {
		parent = d_parent
		instance = { offset = { 0.5 0 } scale = { 0.5 0.5 } }
	}
}
d_alias = d_parent
`,
	filepath.Join("common", "named_colors", "d_colors.txt"): `colors = {
	d_red = rgb { 255 0 0 }
	d_blue = rgb { 0 0 255 }
}
`,
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

// createMod copies the CK3 fixture mod into a temporary folder, writes the given files into it and returns its
// record.
func createMod(t *testing.T, added map[string]string) app.Mod {
	t.Helper()
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS(resolveFixture(t, "mods", "ck3"))); err != nil {
		t.Fatal(err)
	}
	writeFiles(t, dir, added)
	return app.Mod{ID: modID, Dir: dir}
}

// createInstall copies the CK3 fixture install into a temporary folder, with a file of template lists added, and
// returns its folder.
func createInstall(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS(resolveFixture(t, "ck3"))); err != nil {
		t.Fatal(err)
	}
	writeFiles(t, dir, map[string]string{listsPath: listsText})
	return dir
}

// writeFiles writes files into a folder, each at its path from the folder.
func writeFiles(t *testing.T, dir string, added map[string]string) {
	t.Helper()
	for path, text := range added {
		path = filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// openService opens a workspace on an install with the given mods, with a config and caches of its own, and returns
// the draw service over it. The workspace closes when the test ends.
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

// openFixture opens a copy of the CK3 fixture install with the fixture mod, and returns the service with the path of
// the install's GUI file and the folder of the mod.
func openFixture(t *testing.T) (drw *Service, installFile, modDir string) {
	t.Helper()
	mod, install := createMod(t, modFiles), createInstall(t)
	return openService(t, "ck3", install, mod), filepath.Join(install, "game", "gui", "v_window.gui"), mod.Dir
}

// findWidget returns the widget of a window with the given name, and fails the test if it has none.
func findWidget(t *testing.T, window Window, name string) Widget {
	t.Helper()
	widget, ok := lo.Find(window.Widgets, func(widget Widget) bool { return widget.Name == name })
	if !ok {
		t.Fatalf("window %s has no widget %s", window.Name, name)
	}
	return widget
}

// listChildNames returns the names of the widgets directly inside a widget, in order.
func listChildNames(window Window, parent int32) []string {
	return lo.FilterMap(window.Widgets, func(widget Widget, _ int) (string, bool) {
		return widget.Name, widget.Parent == parent
	})
}

// listNames returns the names of the widgets of a window.
func listNames(window Window) []string {
	return lo.Map(window.Widgets, func(widget Widget, _ int) string { return widget.Name })
}

// makeEditor returns a function that applies the editor's edits to a text, as the editor applies them, and parses the
// result again. The function fails the test if the edits are an error or leave the text with a parse error.
func makeEditor(t *testing.T, text string) func(edits []lsp.Edit, err error) (string, *jomini.Tree) {
	t.Helper()
	return func(edits []lsp.Edit, err error) (string, *jomini.Tree) {
		t.Helper()
		return applyEdits(t, text, edits, err)
	}
}

// applyEdits returns a text with the editor's edits applied and parsed again. It fails the test if the edits are an
// error or leave the text with a parse error.
func applyEdits(t *testing.T, text string, edits []lsp.Edit, err error) (string, *jomini.Tree) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	lines := jomini.NewLines(text)
	converted := lo.Map(edits, func(edit lsp.Edit, _ int) jomini.Edit {
		return jomini.Edit{
			Span: jomini.Span{
				Start: lsp.ToOffset(text, lines, edit.Range.StartLine, edit.Range.StartColumn),
				End:   lsp.ToOffset(text, lines, edit.Range.EndLine, edit.Range.EndColumn),
			},
			Text: edit.Text,
		}
	})
	edited := jomini.ApplyEdits(text, converted)
	tree := jomini.Parse(edited)
	if len(tree.Errors) != 0 {
		t.Fatalf("the edited file has errors %v: %s", tree.Errors, edited)
	}
	return edited, tree
}

// listKeys returns the keys written directly inside a top-level statement of a text.
func listKeys(tree *jomini.Tree, name string) []string {
	return lo.Map(slices.Collect(tree.Children(lo.Must(tree.FindChild(0, name)))), func(child int32, _ int) string {
		return tree.Text(tree.Statements[child].Key)
	})
}

// openCoatFile opens the fixture and returns the service, the path of the mod's coat file and its text.
func openCoatFile(t *testing.T) (drw *Service, coatFile, text string) {
	t.Helper()
	drw, _, modDir := openFixture(t)
	coatFile = filepath.Join(modDir, modCoatPath)
	data, err := os.ReadFile(coatFile)
	if err != nil {
		t.Fatal(err)
	}
	return drw, coatFile, string(data)
}

// TestResolveWindowAppliesTypesTemplatesAndOverrides checks a window whose widget is made of a type with a block that
// the widget overrides and a template, and whose properties are those last written, each with where it was written.
func TestResolveWindowAppliesTypesTemplatesAndOverrides(t *testing.T) {
	drw, installFile, _ := openFixture(t)
	window, err := drw.ResolveWindow(installFile, "v_layout_window")
	if err != nil {
		t.Fatal(err)
	}
	if len(window.Widgets) != 10 || window.Widgets[0].Parent != 0 || window.Widgets[0].Name != "v_layout_window" {
		t.Fatalf("window %s: %d widgets, root %+v", window.Name, len(window.Widgets), window.Widgets[0])
	}
	box := window.Widgets[1]
	inside := lo.FilterMap(window.Widgets, func(widget Widget, _ int) (string, bool) { return widget.Type, widget.Parent == box.ID })
	if want := []string{typeButton, typeButton, typeLabel, "icon", "vbox", typeWidget, typeWidget}; box.Type != "v_box" ||
		box.Row.Holding != "column" || !slices.Equal(inside, want) {
		t.Errorf("box: type %s, holding %s, children %v", box.Type, box.Row.Holding, inside)
	}
	size := findProperty(box.Properties, "size")
	if size.Value != "{ 100 20 }" || size.Origin != originTemplate || size.Place.Rel != "gui/v_window.gui" {
		t.Errorf("box size %+v", size)
	}
	button := window.Widgets[2]
	size, texture := findProperty(button.Properties, "size"), findProperty(button.Properties, "texture")
	if size.Value != "{ 50 20 }" || size.Origin != originWidget || texture.Origin != originType || texture.Value != "v_frame.dds" ||
		size.Range.StartLine != 42 {
		t.Errorf("overriding button size %+v, texture %+v", size, texture)
	}
	if findProperty(window.Widgets[3].Properties, "layoutpolicy_vertical").Value != "expanding" {
		t.Errorf("second overriding button %+v", window.Widgets[3].Properties)
	}
	icon := window.Widgets[5]
	if file := filepath.ToSlash(icon.Textures["texture"]); !strings.HasSuffix(file, "gfx/interface/v_frame.dds") ||
		len(icon.Overlays) != 1 || !strings.HasSuffix(filepath.ToSlash(icon.Overlays[0].Path), "gfx/interface/v_bg.dds") ||
		findProperty(icon.Overlays[0].Properties, "framesize").Value != "{ 2 2 }" {
		t.Errorf("icon textures %v, overlays %+v", icon.Textures, icon.Overlays)
	}
	if item := window.Widgets[7]; item.Type != typeLabel || item.Parent != 6 {
		t.Errorf("widget inside the grid's item: %+v", item)
	}
}

// TestResolveWindowPassesOverridesDown checks that a block a widget overrides is replaced in the types of its
// children too, that a type is found written in another case, and that a type that holds itself nests once.
func TestResolveWindowPassesOverridesDown(t *testing.T) {
	drw, _, modDir := openFixture(t)
	modFile := filepath.Join(modDir, modWindowPath)
	window, err := drw.ResolveWindow(modFile, "d_override_window")
	if err != nil {
		t.Fatal(err)
	}
	names := listNames(window)
	if !slices.Contains(names, "d_passed") || !slices.Contains(names, "d_own") || slices.Contains(names, "d_default") {
		t.Errorf("widgets %v", names)
	}
	self, err := drw.ResolveWindow(modFile, "d_self_window")
	if err != nil {
		t.Fatal(err)
	}
	if got := listNames(self); !slices.Equal(got, []string{"d_self_window", "", "d_inner_self"}) {
		t.Errorf("a type that holds itself: %v", got)
	}
}

// TestResolveWindowOrdersChildrenAndOpensStates checks that a label comes before the other children of a widget and an
// item after them, that an attachment is not a child, that a path written with a leading slash finds its file, and
// that the end values of an opening state are the widget's own.
func TestResolveWindowOrdersChildrenAndOpensStates(t *testing.T) {
	drw, _, modDir := openFixture(t)
	window, err := drw.ResolveWindow(filepath.Join(modDir, modWindowPath), "d_order_window")
	if err != nil {
		t.Fatal(err)
	}
	holder := findWidget(t, window, "d_item_holder")
	if got, want := listChildNames(window, holder.ID), []string{"d_label", "d_plain", "d_slash", "d_state", "d_item"}; !slices.Equal(got, want) {
		t.Errorf("children %v, want %v", got, want)
	}
	if label := findWidget(t, window, "d_label"); label.Role != roleLabel {
		t.Errorf("label %+v", label)
	}
	if file := filepath.ToSlash(findWidget(t, window, "d_slash").Textures["texture"]); !strings.HasSuffix(file, "gfx/interface/v_bg.dds") {
		t.Errorf("texture written with a leading slash: %q", file)
	}
	if alpha := findProperty(findWidget(t, window, "d_state").Properties, "alpha"); alpha.Value != halfAlpha || alpha.Origin != originWidget {
		t.Errorf("alpha after the opening state: %+v", alpha)
	}
}

// TestResolveWindowDrawsTypesAndTemplatesAlone checks that a window wins over a type of its name, that a type is drawn
// as a widget of it, and that a template is drawn as a plain widget that uses it.
func TestResolveWindowDrawsTypesAndTemplatesAlone(t *testing.T) {
	drw, _, modDir := openFixture(t)
	modFile := filepath.Join(modDir, modWindowPath)
	twin, err := drw.ResolveWindow(modFile, "d_twin")
	if err != nil {
		t.Fatal(err)
	}
	if findProperty(twin.Widgets[0].Properties, "alpha").Value != "0.25" {
		t.Errorf("the window of the same name as a type: %+v", twin.Widgets[0].Properties)
	}
	holder, err := drw.ResolveWindow(modFile, "d_holder")
	if err != nil {
		t.Fatal(err)
	}
	if got := listNames(holder); !slices.Equal(got, []string{"d_holder", "d_default"}) || holder.Widgets[0].Type != "d_holder" {
		t.Errorf("a type drawn alone: %v", got)
	}
	tint, err := drw.ResolveWindow(modFile, "d_tint")
	if err != nil {
		t.Fatal(err)
	}
	alpha := findProperty(tint.Widgets[0].Properties, "alpha")
	if alpha.Value != halfAlpha || alpha.Origin != originTemplate || tint.Widgets[0].Type != typeWidget {
		t.Errorf("a template drawn alone: %+v", tint.Widgets[0])
	}
}

// TestResolveWindowMarksMissingTemplate checks that a template nothing defines is a property marked missing with its
// name, and that a template the file defines is applied to the widget that uses it.
func TestResolveWindowMarksMissingTemplate(t *testing.T) {
	drw, _, modDir := openFixture(t)
	modFile := filepath.Join(modDir, modWindowPath)
	window, err := drw.ResolveWindow(modFile, windowName)
	if err != nil {
		t.Fatal(err)
	}
	missing := lo.Filter(window.Widgets[0].Properties, func(property Property, _ int) bool { return property.Origin == originMissing })
	if len(missing) != 1 || missing[0].Key != game.GUI.TemplateCalls || missing[0].Value != "d_nowhere" {
		t.Errorf("missing properties %+v", missing)
	}
	inner := findWidget(t, window, "d_inner")
	if alpha := findProperty(inner.Properties, "alpha"); alpha.Value != halfAlpha || alpha.Origin != originTemplate {
		t.Errorf("properties of the widget that uses a template: %+v", inner.Properties)
	}
	if _, err := drw.ResolveWindow(modFile, "nowhere"); err == nil {
		t.Error("a window the file does not define was resolved")
	}
}

// TestResolveWindowShowsTextsAndBindings checks that a localization key shows its text, a data call inside a text shows
// as […], a text that names no message shows as written, a raw text is read, and no other property has a text.
func TestResolveWindowShowsTextsAndBindings(t *testing.T) {
	drw, installFile, modDir := openFixture(t)
	window, err := drw.ResolveWindow(installFile, "v_concepts")
	if err != nil {
		t.Fatal(err)
	}
	if got := window.Widgets[1].Texts["text"]; got != "[…] rules" {
		t.Errorf("text with a call inside: %q", got)
	}
	button, err := drw.ResolveWindow(installFile, "v_other_window")
	if err != nil {
		t.Fatal(err)
	}
	if got := button.Widgets[1].Texts["text"]; got != "[…]" {
		t.Errorf("text a type gives a widget: %q", got)
	}
	texts, err := drw.ResolveWindow(filepath.Join(modDir, modWindowPath), "d_text_window")
	if err != nil {
		t.Fatal(err)
	}
	raw := findWidget(t, texts, "d_raw")
	if raw.Texts["raw_text"] != "Plain Replaced text text" || len(raw.Texts) != 1 {
		t.Errorf("raw text %v", raw.Texts)
	}
	if unknown := findWidget(t, texts, "d_unknown"); unknown.Texts["text"] != "no_such_key" {
		t.Errorf("text with no message %v", unknown.Texts)
	}
}

// TestEvaluateStatesHidesByPick checks that the groups of a window's conditions are read through And and Not, that a
// group that leads to a database offers its definitions, that the first option is picked where nothing is, and that a
// pick changes which widgets are hidden.
func TestEvaluateStatesHidesByPick(t *testing.T) {
	drw, _, modDir := openFixture(t)
	modFile := filepath.Join(modDir, modWindowPath)
	window, err := drw.ResolveWindow(modFile, windowName)
	if err != nil {
		t.Fatal(err)
	}
	states, err := drw.EvaluateStates(window, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantGroups := []Group{
		{ID: groupFlag, Label: "GetVariableSystem.Exists(…)", Options: []string{flagA, flagB, pickEvery, pickNone}, Picked: flagA},
		{ID: groupAlive, Label: groupAlive, Options: []string{pickEvery, pickNone}, Picked: pickEvery},
	}
	if len(states.Groups) != 3 || !slices.EqualFunc(states.Groups[:2], wantGroups, func(got, want Group) bool {
		return got.ID == want.ID && got.Label == want.Label && got.Picked == want.Picked && slices.Equal(got.Options, want.Options)
	}) {
		t.Errorf("groups %+v", states.Groups)
	}
	if trait := states.Groups[2]; trait.ID != groupTrait || trait.Picked != "brave" || trait.Options[0] != "brave" ||
		trait.Options[1] != traitBold || !slices.Contains(trait.Options, "craven") || slices.Contains(trait.Options, pickNone) {
		t.Errorf("the group that leads to the traits: %+v", trait)
	}
	names := func(hidden []int32) []string {
		return lo.Map(hidden, func(id int32, _ int) string { return window.Widgets[id].Name })
	}
	if got, want := names(states.Hidden), []string{"d_not", "d_outer", "d_off", "d_child", "d_trait_b"}; !slices.Equal(got, want) {
		t.Errorf("hidden by the first options: %v, want %v", got, want)
	}
	flipped, err := drw.EvaluateStates(window, map[string]string{groupAlive: pickNone, groupFlag: flagB, groupTrait: traitBold})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := names(flipped.Hidden), []string{"d_both", "d_inner", "d_off", "d_parent", "d_trait_a"}; !slices.Equal(got, want) {
		t.Errorf("hidden after the picks: %v, want %v", got, want)
	}
}

// TestRevealPicksShowWidget checks that the picks that reveal a widget satisfy the conditions on its way down, that
// Not asks for none, and that conditions which contradict give no picks.
func TestRevealPicksShowWidget(t *testing.T) {
	drw, _, modDir := openFixture(t)
	modFile := filepath.Join(modDir, modWindowPath)
	window, err := drw.ResolveWindow(modFile, windowName)
	if err != nil {
		t.Fatal(err)
	}
	reveal := func(name string) map[string]string {
		picks, err := drw.Reveal(window, findWidget(t, window, name).ID)
		if err != nil {
			t.Fatal(err)
		}
		return picks
	}
	if got := reveal("d_inner"); got[groupFlag] != flagB || got[groupAlive] != pickEvery {
		t.Errorf("picks that reveal the inner widget: %v", got)
	}
	if got := reveal("d_not"); got[groupAlive] != pickNone || got[groupFlag] != flagA {
		t.Errorf("picks that reveal the widget under Not: %v", got)
	}
	if got := reveal("d_trait_b"); got[groupTrait] != traitBold {
		t.Errorf("picks that reveal the second trait: %v", got)
	}
	if got := reveal("d_child"); got != nil {
		t.Errorf("picks for conditions that contradict: %v", got)
	}
	if got := reveal("d_off"); got != nil {
		t.Errorf("picks for a widget that is never visible: %v", got)
	}
}

// TestResolveCoatReadsLayersParentsAndLists checks a coat's pattern, emblem, sub, colors and instances, a coat with a
// parent, and one that is the name of another. It also checks the option a template list draws: the heaviest, the first
// of equal weights, a pick, and none for a list with no definition.
func TestResolveCoatReadsLayersParentsAndLists(t *testing.T) {
	drw, _, modDir := openFixture(t)
	coatFile := filepath.Join(modDir, modCoatPath)
	coat, err := drw.ResolveCoat(coatFile, coatParent, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(coat.Layers) != 2 || coat.Name != coatParent {
		t.Fatalf("coat %+v", coat)
	}
	pattern, emblem := coat.Layers[0], coat.Layers[1]
	wantColors := []string{game.Coat.Fallback, cssRed, cssBlue, "", "", ""}
	if pattern.Role != patternKey || pattern.Texture != patternFile || !strings.HasSuffix(filepath.ToSlash(pattern.Path), "patterns/v_pattern.dds") ||
		!slices.Equal(pattern.Colors, wantColors) {
		t.Errorf("pattern %+v", pattern)
	}
	if emblem.Role != emblemKey || emblem.Path == "" || emblem.Mask != pattern.Path || !slices.Equal(emblem.Regions, []int{1, 2}) ||
		emblem.Colors[1] != cssBlue || len(emblem.Instances) != 2 {
		t.Errorf("emblem %+v", emblem)
	}
	first, second := emblem.Instances[0], emblem.Instances[1]
	if first.Position != [2]float64{0.25, 0.75} || first.Scale != [2]float64{0.5, 0.5} || first.Rotation != 45 || first.Depth != 2 ||
		second.Position != [2]float64{0.5, 0.5} || second.Scale != [2]float64{1, 1} || second.Depth != 1 {
		t.Errorf("instances %+v", emblem.Instances)
	}
	child, err := drw.ResolveCoat(coatFile, "d_child", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(child.Layers) != 2 || child.Layers[0].Colors[1] != cssGreen || child.Layers[1].Colors[1] != cssBlue {
		t.Errorf("a coat with a parent: %+v", child.Layers)
	}
	alias, err := drw.ResolveCoat(coatFile, "d_alias", nil)
	if err != nil || alias.Name != "d_alias" || len(alias.Layers) != 2 {
		t.Errorf("a coat written as the name of another: %+v %v", alias, err)
	}
	listed, err := drw.ResolveCoat(coatFile, "d_listed", nil)
	if err != nil {
		t.Fatal(err)
	}
	heavy, equal := listed.Layers[0], listed.Layers[1]
	if heavy.Texture != patternFile || len(heavy.Lists) != 1 || heavy.Lists[0].Label != "d_heavy" ||
		!slices.Equal(heavy.Lists[0].Options, []string{otherFile, patternFile}) || heavy.Lists[0].Picked != patternFile {
		t.Errorf("the heaviest option outside the special selection: %+v", heavy)
	}
	if equal.Texture != "first.dds" || equal.Lists[0].Picked != "first.dds" || listed.Layers[2].Texture != "missing:d_nowhere_list" ||
		listed.Layers[2].Path != "" || listed.Layers[3].Role != subKey || listed.Layers[3].Sub.Name != coatParent ||
		len(listed.Layers[3].Sub.Layers) != 2 || listed.Layers[3].Instances[0].Position != [2]float64{0.5, 0} {
		t.Errorf("a list of equal weights, a list that is missing and a sub: %+v", listed.Layers)
	}
	if len(equal.Instances) != 1 || equal.Instances[0].Range != (lsp.Range{}) {
		t.Errorf("a default instance has no range: %+v", equal.Instances)
	}
	picked, err := drw.ResolveCoat(coatFile, "d_listed", map[string]string{heavy.Lists[0].ID: otherFile})
	if err != nil || picked.Layers[0].Texture != otherFile || picked.Layers[0].Lists[0].Picked != otherFile {
		t.Errorf("a pick: %+v %v", picked.Layers[0], err)
	}
	if _, err := drw.ResolveCoat(coatFile, "nowhere", nil); err == nil {
		t.Error("a coat the file does not define was resolved")
	}
}

// TestEditPropertyReplacesOrAddsKey checks that an edit of a key in a widget's, coat's, layer's or instance's block
// replaces the whole value where the block writes the key, and adds the key in the file's indentation where it does
// not. It also checks that a range where no block starts is an error.
func TestEditPropertyReplacesOrAddsKey(t *testing.T) {
	drw, _, modDir := openFixture(t)
	modFile, coatFile := filepath.Join(modDir, modWindowPath), filepath.Join(modDir, modCoatPath)
	window, err := drw.ResolveWindow(modFile, windowName)
	if err != nil {
		t.Fatal(err)
	}
	widget := findWidget(t, window, "d_trait_a")
	windowText, err := os.ReadFile(modFile)
	if err != nil {
		t.Fatal(err)
	}
	coatText, err := os.ReadFile(coatFile)
	if err != nil {
		t.Fatal(err)
	}
	edited, _ := makeEditor(t, string(windowText))(drw.EditProperty(widget.Place.Path, widget.Range, "visible", "no"))
	if !strings.Contains(edited, `name = "d_trait_a" visible = no }`) {
		t.Errorf("value replaced in a block on one line: %s", edited)
	}
	edit := makeEditor(t, string(coatText))
	listed := lo.Must(drw.ResolveCoat(coatFile, "d_listed", nil))
	_, replaced := edit(drw.EditProperty(coatFile, listed.Range, patternKey, `"v_pattern.dds"`))
	pattern := replaced.Statements[lo.Must(replaced.FindChild(lo.Must(replaced.FindChild(0, "d_listed")), patternKey))]
	if !pattern.Tag.IsEmpty() || replaced.Text(pattern.Value) != `"v_pattern.dds"` {
		t.Errorf("a list replaced by a plain value, tag and all: %q", replaced.Text(pattern.Span))
	}
	parent := lo.Must(drw.ResolveCoat(coatFile, coatParent, nil))
	emblem, instance := parent.Layers[1], parent.Layers[1].Instances[0]
	_, retextured := edit(drw.EditProperty(coatFile, emblem.Range, textureKey, `"v_textured.dds"`))
	block := lo.Must(retextured.FindChild(lo.Must(retextured.FindChild(0, coatParent)), emblemKey))
	if got := retextured.Unquoted(retextured.Statements[lo.Must(retextured.FindChild(block, textureKey))].Value); got != "v_textured.dds" {
		t.Errorf("texture after the edit: %s", got)
	}
	deeper, _ := edit(drw.EditProperty(coatFile, instance.Range, "depth", "5"))
	if !strings.Contains(deeper, "rotation = 45 depth = 5 }") {
		t.Errorf("depth after the edit: %s", deeper)
	}
	added, _ := edit(drw.EditProperty(coatFile, emblem.Range, slotTwo, `"white"`))
	if !strings.Contains(added, "\n\t\tcolor2 = \"white\"\n") {
		t.Errorf("a color added to the emblem, in the file's indentation:\n%s", added)
	}
	_, colored := edit(drw.EditProperty(coatFile, parent.Range, "color3", `"white"`))
	if _, ok := colored.FindChild(lo.Must(colored.FindChild(0, coatParent)), "color3"); !ok {
		t.Error("a color was not added to the coat")
	}
	if _, err := drw.EditProperty(coatFile, parent.Layers[0].Range, slotOne, "red"); err == nil {
		t.Error("a key was set in the statement of a pattern, which is no block")
	}
}

// TestEditCoatLayers checks that each edit of a coat's layers, applied to the file and parsed again, leaves the coat
// as the edit says, and that a layer a coat inherits, a second pattern and a coat written as another's name are errors.
func TestEditCoatLayers(t *testing.T) {
	drw, coatFile, text := openCoatFile(t)
	edit := makeEditor(t, text)
	_, added := edit(drw.AddLayer(coatFile, coatParent, emblemKey, 1))
	if got := listKeys(added, coatParent); !slices.Equal(got, []string{patternKey, emblemKey, slotOne, slotTwo, emblemKey}) {
		t.Errorf("keys after adding an emblem: %v", got)
	}
	_, removed := edit(drw.RemoveLayer(coatFile, coatParent, 1))
	if got := listKeys(removed, coatParent); !slices.Equal(got, []string{patternKey, slotOne, slotTwo}) {
		t.Errorf("keys after removing the emblem: %v", got)
	}
	_, moved := edit(drw.MoveLayer(coatFile, coatParent, 1, 0))
	if got := listKeys(moved, coatParent); !slices.Equal(got, []string{emblemKey, patternKey, slotOne, slotTwo}) {
		t.Errorf("keys after moving the emblem first: %v", got)
	}
	_, later := edit(drw.MoveLayer(coatFile, coatParent, 0, 1))
	if got := listKeys(later, coatParent); !slices.Equal(got, []string{slotOne, slotTwo, emblemKey, patternKey}) {
		t.Errorf("keys after moving the pattern last: %v", got)
	}
	for name, err := range map[string]error{
		"a second pattern":        errOf(drw.AddLayer(coatFile, coatParent, patternKey, 0)),
		"a layer after nothing":   errOf(drw.AddLayer(coatFile, coatParent, emblemKey, 9)),
		"a layer of an alias":     errOf(drw.AddLayer(coatFile, "d_alias", emblemKey, 0)),
		"a layer of the parent":   errOf(drw.RemoveLayer(coatFile, "d_child", 0)),
		"a layer that is missing": errOf(drw.RemoveLayer(coatFile, coatParent, 9)),
	} {
		if err == nil {
			t.Errorf("%s was edited", name)
		}
	}
}

// errOf returns the error of a call that returns edits and an error.
func errOf(_ []lsp.Edit, err error) error {
	return err
}

// TestResolveWindowsOnInstalls resolves every window of each real install that PMT_TEST_INSTALL_<GAME> names, with no
// mods, and evaluates its states. It prints the totals and the types and templates nothing defines. A window that fails
// to resolve fails the test.
func TestResolveWindowsOnInstalls(t *testing.T) {
	for _, gameID := range installGames {
		t.Run(gameID, func(t *testing.T) {
			variable := "PMT_TEST_INSTALL_" + strings.ToUpper(gameID)
			install := os.Getenv(variable)
			if install == "" {
				t.Skipf("%s is not set", variable)
			}
			drw := openService(t, gameID, install)
			ws, err := drw.Workspace.Session()
			if err != nil {
				t.Fatal(err)
			}
			started := time.Now()
			var totals gateTotals
			for _, file := range ws.Install.Files {
				if file.Format == files.FormatGUI {
					totals.add(t, drw, file.Path)
				}
			}
			totals.log(t, time.Since(started))
		})
	}
}

// TestResolveCoatsOnInstalls resolves every coat of each real install that PMT_TEST_INSTALL_<GAME> names, with no
// mods. It prints the totals. A coat that fails to resolve fails the test.
func TestResolveCoatsOnInstalls(t *testing.T) {
	for _, gameID := range installGames {
		t.Run(gameID, func(t *testing.T) {
			variable := "PMT_TEST_INSTALL_" + strings.ToUpper(gameID)
			install := os.Getenv(variable)
			if install == "" {
				t.Skipf("%s is not set", variable)
			}
			drw := openService(t, gameID, install)
			ws, err := drw.Workspace.Session()
			if err != nil {
				t.Fatal(err)
			}
			started := time.Now()
			var totals coatTotals
			for _, file := range ws.Install.Files {
				if file.Format.IsScript() && strings.Contains(file.Rel, "coat_of_arms/coat_of_arms/") {
					totals.add(t, drw, file.Path)
				}
			}
			t.Logf("%d coats, %d errors, %d layers (%d patterns, %d emblems, %d subs), %d instances, %d lists, %s", totals.coats,
				totals.errors, totals.layers, totals.patterns, totals.emblems, totals.subs, totals.instances, totals.lists,
				time.Since(started).Round(time.Millisecond))
			t.Logf("textures: %d found, %d not found; colors: %d found, %d not found; %d lists missing", totals.found, totals.lost,
				totals.colored, totals.uncolored, totals.missing)
		})
	}
}

// A gateTotals counts what a gate found over the windows of an install.
type gateTotals struct {
	windows, errors, widgets, groups, hidden, databases, decided  int
	texts, textsMissing, paths, pathsMissing, calls, callsMissing int
	missing                                                       map[string][]string
}

// add resolves every window of a GUI file, evaluates its states and counts the result.
func (totals *gateTotals) add(t *testing.T, drw *Service, path string) {
	t.Helper()
	if totals.missing == nil {
		totals.missing = map[string][]string{}
	}
	drawables, err := drw.ListDrawables(path)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	ws, _ := drw.Workspace.Session()
	mods := ws.Mods.Load()
	for _, drawable := range drawables {
		if drawable.Database != game.GUI.WidgetDatabase {
			continue
		}
		window, err := drw.ResolveWindow(path, drawable.Name)
		if err != nil {
			totals.errors++
			t.Errorf("%s %s: %v", path, drawable.Name, err)
			continue
		}
		totals.windows++
		totals.widgets += len(window.Widgets)
		for _, widget := range window.Widgets {
			totals.count(ws, mods, path, widget)
		}
		datacontexts := listDatacontexts(ws.Schema, window)
		states := evaluate(ws, window, datacontexts, nil)
		_, kept := listGroups(ws, mods, window, datacontexts)
		totals.groups += len(states.Groups)
		totals.hidden += len(states.Hidden)
		totals.databases += lo.CountBy(states.Groups, func(group Group) bool { return group.Database != "" })
		totals.decided += len(resolveDecided(ws, mods, states.Groups, kept))
	}
}

// count adds a widget's missing types and templates, texts and textures to the totals. A picture written as a path or
// an icon name is a texture to find. A picture written as a data call is one only the running game answers. A text is
// found where a message in the given index of the mods has its key.
func (totals *gateTotals) count(ws *workspace.Session, mods *index.Index, path string, widget Widget) {
	for _, property := range widget.Properties {
		_, textured := widget.Textures[property.Key]
		switch {
		case property.Origin == originMissing:
			label := property.Key + " " + property.Value
			totals.missing[label] = append(totals.missing[label], path)
		case slices.Contains(game.GUI.PictureKeys, property.Key) && !strings.HasPrefix(property.Value, "["):
			totals.paths++
			totals.pathsMissing += lo.Ternary(textured, 0, 1)
		case slices.Contains(game.GUI.PictureKeys, property.Key):
			totals.calls++
			totals.callsMissing += lo.Ternary(textured, 0, 1)
		case isLocalized(property.Key) && !strings.Contains(property.Value, "["):
			_, _, listed := ws.LookupText(mods, property.Value)
			totals.texts++
			totals.textsMissing += lo.Ternary(listed, 0, 1)
		}
	}
}

// log prints the totals, then each missing type or template with the files that use it.
func (totals *gateTotals) log(t *testing.T, elapsed time.Duration) {
	t.Helper()
	t.Logf("%d windows, %d errors, %d widgets, %d groups (%d lead to a database, %d decided by a definition), %d hidden by the "+
		"first picks, %s", totals.windows, totals.errors, totals.widgets, totals.groups, totals.databases, totals.decided,
		totals.hidden, elapsed.Round(time.Millisecond))
	t.Logf("texts: %d found, %d not found; paths and icon names: %d found, %d not found; data calls: %d found, %d not found",
		totals.texts-totals.textsMissing, totals.textsMissing, totals.paths-totals.pathsMissing, totals.pathsMissing,
		totals.calls-totals.callsMissing, totals.callsMissing)
	labels := lo.Keys(totals.missing)
	slices.Sort(labels)
	t.Logf("%d missing types and templates", len(labels))
	for _, label := range labels {
		paths := lo.Uniq(totals.missing[label])
		t.Logf("missing %s: %d times in %d files: %s", label, len(totals.missing[label]), len(paths), strings.Join(paths, ", "))
	}
}

// A coatTotals counts what a gate found over the coats of an install.
type coatTotals struct {
	coats, errors, layers, patterns, emblems, subs, instances, lists int
	found, lost, colored, uncolored, missing                         int
}

// add resolves every coat of a file and counts the result.
func (totals *coatTotals) add(t *testing.T, drw *Service, path string) {
	t.Helper()
	drawables, err := drw.ListDrawables(path)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	for _, drawable := range drawables {
		coat, err := drw.ResolveCoat(path, drawable.Name, nil)
		if err != nil {
			totals.errors++
			t.Errorf("%s %s: %v", path, drawable.Name, err)
			continue
		}
		totals.coats++
		totals.count(coat)
	}
}

// count adds the layers of a coat, and of the coats of its subs, to the totals.
func (totals *coatTotals) count(coat Coat) {
	for _, layer := range coat.Layers {
		totals.layers++
		totals.instances += len(layer.Instances)
		totals.lists += len(layer.Lists)
		switch layer.Role {
		case patternKey:
			totals.patterns++
		case subKey:
			totals.subs++
			totals.count(*layer.Sub)
		default:
			totals.emblems++
		}
		if layer.Role != subKey && layer.Texture != "" {
			totals.found += lo.Ternary(layer.Path != "", 1, 0)
			totals.lost += lo.Ternary(layer.Path == "", 1, 0)
		}
		totals.missing += lo.Ternary(strings.HasPrefix(layer.Texture, missingList), 1, 0)
		for _, color := range lo.Drop(layer.Colors, 1) {
			totals.colored += lo.Ternary(color != "", 1, 0)
			totals.uncolored += lo.Ternary(color == "", 1, 0)
		}
	}
}
