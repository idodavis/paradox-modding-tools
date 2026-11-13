package draw

import (
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
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/internal/loc"
	"paradox-modding-tools/services/app"
	"paradox-modding-tools/services/lsp"
	"paradox-modding-tools/services/workspace"
)

// The types, groups and names the tests use.
const (
	typeButton  = "v_button"
	typeWidget  = "widget"
	typeLabel   = "v_label"
	groupFlagA  = "GetVariableSystem.Exists('d_a')"
	groupFlagB  = "GetVariableSystem.Exists('d_b')"
	groupAlive  = "GetPlayer.IsIndependentRuler"
	groupBrave  = "GetTrait('brave').IsValid"
	groupBold   = "GetTrait('bold').IsValid"
	halfAlpha   = "0.5"
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

// listsPath is the file the fixture install gets for its template lists, from
// the install's folder.
var listsPath = filepath.Join("game", "common", "coat_of_arms", "template_lists", "v_lists.txt")

// listsText is what the fixture install's template lists file holds.
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

// modFiles are the files the fixture mod adds: windows, types and templates,
// and coats.
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
		visible = "[And2(GetVariableSystem.Exists('d_a'), GetPlayer.IsIndependentRuler)]"
	}
	widget = {
		name = "d_not"
		visible = "[Not( GetPlayer.IsIndependentRuler )]"
		visible_at_creation = no
	}
	widget = {
		name = "d_outer"
		visible = "[GetVariableSystem.Exists('d_b')]"
		icon = { name = "d_inner" using = d_tint visible = "[GetPlayer.IsIndependentRuler]" }
	}
	widget = { name = "d_off" visible = no }
	widget = {
		name = "d_parent"
		visible = "[GetVariableSystem.Exists('d_a')]"
		widget = { name = "d_child" visible = "[Not( GetVariableSystem.Exists('d_a') )]" }
	}
	widget = { name = "d_trait_a" visible = "[GetTrait('brave').IsValid]" }
	widget = { name = "d_trait_b" visible = "[GetTrait('bold').IsValid]" }
	widget = {
		name = "d_choice"
		alpha = "[Select_float( GetVariableSystem.Exists('d_a'), '(float)1.0', '(float)0.5' )]"
		size = "[Select_CVector2f( GetPlayer.IsIndependentRuler, '(CVector2f)10,20', '(CVector2f)30,40' )]"
	}
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
		item = { widget = { name = "d_item" } block "d_row" { widget = { name = "d_row_item" } } }
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
	textbox = { name = "d_raw" text = "no_such_key" raw_text = "Plain $v_key$ text" visible = "[GetPlayer.IsIndependentRuler]" }
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

// createApp makes the application the workspace service sends its events
// through, once for the whole test run.
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

// createMod copies the CK3 fixture mod into a temporary folder, adds the given
// files and returns its record.
func createMod(t *testing.T, added map[string]string) app.Mod {
	t.Helper()
	dir := t.TempDir()
	if err := os.CopyFS(dir, os.DirFS(resolveFixture(t, "mods", "ck3"))); err != nil {
		t.Fatal(err)
	}
	writeFiles(t, dir, added)
	return app.Mod{ID: modID, Dir: dir}
}

// createInstall copies the CK3 fixture install into a temporary folder, adds a
// file of template lists and returns its folder.
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

// openService opens a workspace on an install with the given mods and returns
// the draw service over it.
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

// openFixture opens a copy of the CK3 fixture install with the fixture mod, and
// returns the service with the path of the install's GUI file and the folder of
// the mod.
func openFixture(t *testing.T) (drw *Service, installFile, modDir string) {
	t.Helper()
	mod, install := createMod(t, modFiles), createInstall(t)
	return openService(t, "ck3", install, mod), filepath.Join(install, "game", "gui", "v_window.gui"), mod.Dir
}

// findWidget returns the widget of a window with the given name, and fails the
// test if it has none.
func findWidget(t *testing.T, window Window, name string) Widget {
	t.Helper()
	widget, ok := lo.Find(window.Widgets, func(widget Widget) bool { return widget.Name == name })
	if !ok {
		t.Fatalf("window %s has no widget %s", window.Name, name)
	}
	return widget
}

// listChildNames returns the names of the widgets directly inside a widget, in
// order.
func listChildNames(window Window, parent int32) []string {
	return lo.FilterMap(window.Widgets, func(widget Widget, _ int) (string, bool) {
		return widget.Name, widget.Parent == parent
	})
}

// listNames returns the names of the widgets of a window.
func listNames(window Window) []string {
	return lo.Map(window.Widgets, func(widget Widget, _ int) string { return widget.Name })
}

// makeEditor returns a function that applies the editor's edits to a text and
// parses the result again. It fails the test if the edits are an error or leave
// the text with a parse error.
func makeEditor(t *testing.T, text string) func(edits []lsp.Edit, err error) (string, *jomini.Tree) {
	t.Helper()
	return func(edits []lsp.Edit, err error) (string, *jomini.Tree) {
		t.Helper()
		return applyEdits(t, text, edits, err)
	}
}

// applyEdits returns a text with the editor's edits applied and parsed again.
// It fails the test if the edits are an error or leave the text with a parse
// error.
func applyEdits(t *testing.T, text string, edits []lsp.Edit, err error) (string, *jomini.Tree) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	lines := jomini.NewLines(text)
	converted := lo.Map(edits, func(edit lsp.Edit, _ int) jomini.Edit {
		return jomini.Edit{
			Span: jomini.Span{
				Start: jomini.ToOffset(text, lines, edit.Range.StartLine, edit.Range.StartColumn),
				End:   jomini.ToOffset(text, lines, edit.Range.EndLine, edit.Range.EndColumn),
			},
			Text: edit.Text,
		}
	})
	edited := jomini.ApplyEdits(text, converted)
	tree := jomini.Parse(edited)
	if len(tree.Problems) != 0 {
		t.Fatalf("the edited file has problems %v: %s", tree.Problems, edited)
	}
	return edited, tree
}

// listKeys returns the keys written directly inside a top-level statement of a
// text.
func listKeys(tree *jomini.Tree, name string) []string {
	return lo.Map(slices.Collect(tree.Children(lo.Must(tree.FindChild(0, name)))), func(child int32, _ int) string {
		return tree.Text(tree.Statements[child].Key)
	})
}

// openCoatFile opens the fixture and returns the service, the path of the mod's
// coat file and its text.
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

// TestResolveWindowAppliesTypesTemplatesAndOverrides checks a widget made of a
// type, a template and an override, and that each property is the last written,
// with where it was written.
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
	size := findProperty(window.listProperties(box), "size")
	if size.Value != "{ 100 20 }" || size.Origin != originTemplate || window.Places[size.Place].Rel != "gui/v_window.gui" {
		t.Errorf("box size %+v", size)
	}
	button := window.Widgets[2]
	size, texture := findProperty(window.listProperties(button), "size"), findProperty(window.listProperties(button), "texture")
	if size.Value != "{ 50 20 }" || size.Origin != originWidget || texture.Origin != originType || texture.Value != "v_frame.dds" ||
		size.Range.StartLine != 42 {
		t.Errorf("overriding button size %+v, texture %+v", size, texture)
	}
	if findProperty(window.listProperties(window.Widgets[3]), "layoutpolicy_vertical").Value != "expanding" {
		t.Errorf("second overriding button %+v", slices.Collect(window.listProperties(window.Widgets[3])))
	}
	icon := window.Widgets[5]
	var overlay Overlay
	if len(icon.Overlays) == 1 {
		overlay = window.Overlays[icon.Overlays[0]]
	}
	framesize := ""
	for _, position := range overlay.Properties {
		if property := window.Properties[position]; property.Key == "framesize" {
			framesize = property.Value
		}
	}
	if file := filepath.ToSlash(icon.Textures["texture"]); !strings.HasSuffix(file, "gfx/interface/v_frame.dds") ||
		!strings.HasSuffix(filepath.ToSlash(overlay.Path), "gfx/interface/v_bg.dds") || framesize != "{ 2 2 }" {
		t.Errorf("icon textures %v, overlays %+v", icon.Textures, icon.Overlays)
	}
	if item := window.Widgets[7]; item.Type != typeLabel || item.Parent != 6 {
		t.Errorf("widget inside the grid's item: %+v", item)
	}
}

// TestResolveWindowPassesOverridesDown checks that an override reaches the
// types of a widget's children, that a type is found written in another case,
// and that a type that holds itself nests once.
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

// TestResolveWindowOrdersChildrenAndOpensStates checks the order of a widget's
// children, that an attachment is not a child, and that the end values of an
// opening state are the widget's own.
func TestResolveWindowOrdersChildrenAndOpensStates(t *testing.T) {
	drw, _, modDir := openFixture(t)
	window, err := drw.ResolveWindow(filepath.Join(modDir, modWindowPath), "d_order_window")
	if err != nil {
		t.Fatal(err)
	}
	holder := findWidget(t, window, "d_item_holder")
	if got, want := listChildNames(window, holder.ID), []string{"d_label", "d_plain", "d_slash", "d_state", "d_item", "d_row_item"}; !slices.Equal(got, want) {
		t.Errorf("children %v, want %v", got, want)
	}
	if label := findWidget(t, window, "d_label"); label.Role != widgetSlots["buttontext"].Role {
		t.Errorf("label %+v", label)
	}
	if file := filepath.ToSlash(findWidget(t, window, "d_slash").Textures["texture"]); !strings.HasSuffix(file, "gfx/interface/v_bg.dds") {
		t.Errorf("texture written with a leading slash: %q", file)
	}
	if alpha := findProperty(window.listProperties(findWidget(t, window, "d_state")), "alpha"); alpha.Value != halfAlpha || alpha.Origin != originWidget {
		t.Errorf("alpha after the opening state: %+v", alpha)
	}
}

// TestResolveWindowDrawsTypesAndTemplatesAlone checks that a window wins over a
// type of its name, a type is drawn as a widget of it, and a template as a
// plain widget that uses it.
func TestResolveWindowDrawsTypesAndTemplatesAlone(t *testing.T) {
	drw, _, modDir := openFixture(t)
	modFile := filepath.Join(modDir, modWindowPath)
	twin, err := drw.ResolveWindow(modFile, "d_twin")
	if err != nil {
		t.Fatal(err)
	}
	if findProperty(twin.listProperties(twin.Widgets[0]), "alpha").Value != "0.25" {
		t.Errorf("the window of the same name as a type: %+v", slices.Collect(twin.listProperties(twin.Widgets[0])))
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
	alpha := findProperty(tint.listProperties(tint.Widgets[0]), "alpha")
	if alpha.Value != halfAlpha || alpha.Origin != originTemplate || tint.Widgets[0].Type != typeWidget {
		t.Errorf("a template drawn alone: %+v", tint.Widgets[0])
	}
}

// TestResolveWindowMarksMissingTemplate checks that a template nothing defines
// is a property marked missing, and one the file defines is applied.
func TestResolveWindowMarksMissingTemplate(t *testing.T) {
	drw, _, modDir := openFixture(t)
	modFile := filepath.Join(modDir, modWindowPath)
	window, err := drw.ResolveWindow(modFile, windowName)
	if err != nil {
		t.Fatal(err)
	}
	missing := lo.Filter(slices.Collect(window.listProperties(window.Widgets[0])), func(property Property, _ int) bool {
		return property.Origin == originMissing
	})
	if len(missing) != 1 || missing[0].Key != lang.GUITemplateCalls || missing[0].Value != "d_nowhere" {
		t.Errorf("missing properties %+v", missing)
	}
	inner := findWidget(t, window, "d_inner")
	if alpha := findProperty(window.listProperties(inner), "alpha"); alpha.Value != halfAlpha || alpha.Origin != originTemplate {
		t.Errorf("properties of the widget that uses a template: %+v", slices.Collect(window.listProperties(inner)))
	}
	if _, err := drw.ResolveWindow(modFile, "nowhere"); err == nil {
		t.Error("a window the file does not define was resolved")
	}
}

// TestResolveWindowShowsTextsAndBindings checks the texts a localization key, a
// data call and a raw text show.
func TestResolveWindowShowsTextsAndBindings(t *testing.T) {
	drw, installFile, modDir := openFixture(t)
	window, err := drw.ResolveWindow(installFile, "v_concepts")
	if err != nil {
		t.Fatal(err)
	}
	isSample := func(segment loc.Segment) bool {
		return segment.Markup == loc.MarkupCall && segment.Text != "" && !strings.Contains(segment.Text, "[")
	}
	if got := window.Widgets[1].Texts["text"]; len(got) != 2 || !isSample(got[0]) || loc.ToPlain(got) != " rules" {
		t.Errorf("text with a call inside: %+v", got)
	}
	button, err := drw.ResolveWindow(installFile, "v_other_window")
	if err != nil {
		t.Fatal(err)
	}
	if got := button.Widgets[1].Texts["text"]; len(got) != 1 || !isSample(got[0]) {
		t.Errorf("text a type gives a widget: %+v", got)
	}
	texts, err := drw.ResolveWindow(filepath.Join(modDir, modWindowPath), "d_text_window")
	if err != nil {
		t.Fatal(err)
	}
	raw := findWidget(t, texts, "d_raw")
	if loc.ToPlain(raw.Texts["raw_text"]) != "Plain Replaced text text" || len(raw.Texts) != 1 {
		t.Errorf("raw text %v", raw.Texts)
	}
	if unknown := findWidget(t, texts, "d_unknown"); loc.ToPlain(unknown.Texts["text"]) != "no_such_key" {
		t.Errorf("text with no message %v", unknown.Texts)
	}
}

// TestEvaluateStatesHidesByPick checks the groups a window's conditions make,
// the values the window opens with, that a pick changes which widgets are
// hidden, and that a choice between two values gives the one its condition
// chooses.
func TestEvaluateStatesHidesByPick(t *testing.T) {
	drw, _, modDir := openFixture(t)
	modFile := filepath.Join(modDir, modWindowPath)
	window, err := drw.ResolveWindow(modFile, windowName)
	if err != nil {
		t.Fatal(err)
	}
	states, err := drw.EvaluateStates(modFile, windowName, nil)
	if err != nil {
		t.Fatal(err)
	}
	yesOrNo := []string{pickEvery, pickNone}
	wantGroups := []Group{
		{ID: groupFlagA, Subject: "Variable system", Label: "exists d_a", Options: yesOrNo},
		{ID: groupAlive, Subject: "Player", Label: "is independent ruler", Options: yesOrNo},
		{ID: groupFlagB, Subject: "Variable system", Label: "exists d_b", Options: yesOrNo},
		{ID: groupBrave, Subject: "Trait", Key: "brave", Label: "is valid", Options: yesOrNo},
		{ID: groupBold, Subject: "Trait", Key: "bold", Label: "is valid", Options: yesOrNo},
	}
	if !slices.EqualFunc(window.Groups, wantGroups, func(got, want Group) bool {
		return got.ID == want.ID && got.Subject == want.Subject && got.Key == want.Key && got.Label == want.Label &&
			slices.Equal(got.Options, want.Options)
	}) {
		t.Errorf("groups %+v", window.Groups)
	}
	wantValues := map[string]string{groupFlagA: pickNone, groupAlive: pickNone, groupFlagB: pickNone, groupBrave: pickEvery,
		groupBold: pickEvery}
	if !maps.Equal(states.Values, wantValues) {
		t.Errorf("values %v", states.Values)
	}
	names := func(hidden []int32) []string {
		return lo.Map(hidden, func(id int32, _ int) string { return window.Widgets[id].Name })
	}
	want := []string{"d_both", "d_outer", "d_inner", "d_off", "d_parent"}
	if got := names(states.Hidden); !slices.Equal(got, want) {
		t.Errorf("hidden as the window opens: %v, want %v", got, want)
	}
	chosen := []Choice{{Key: "alpha", Value: "0.5"}, {Key: "size", Value: "30 40"}}
	if got := states.Choices[findWidget(t, window, "d_choice").ID]; !slices.EqualFunc(got, chosen, func(got, want Choice) bool {
		return got.Key == want.Key && got.Value == want.Value
	}) {
		t.Errorf("choices as the window opens: %+v, want %+v", got, chosen)
	}
	flipped, err := drw.EvaluateStates(modFile, windowName, map[string]string{groupFlagB: pickEvery, groupBrave: pickNone})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := names(flipped.Hidden), []string{"d_both", "d_inner", "d_off", "d_parent", "d_trait_a"}; !slices.Equal(got, want) {
		t.Errorf("hidden after the picks: %v, want %v", got, want)
	}
}

// TestRevealPicksShowWidget checks that the picks that reveal a widget satisfy
// the conditions above it, and that contradicting conditions give no picks.
func TestRevealPicksShowWidget(t *testing.T) {
	drw, _, modDir := openFixture(t)
	modFile := filepath.Join(modDir, modWindowPath)
	window, err := drw.ResolveWindow(modFile, windowName)
	if err != nil {
		t.Fatal(err)
	}
	reveal := func(name string, picks map[string]string) map[string]string {
		required, err := drw.Reveal(modFile, windowName, picks, findWidget(t, window, name).ID)
		if err != nil {
			t.Fatal(err)
		}
		return required
	}
	if got := reveal("d_inner", nil); got[groupFlagB] != pickEvery || got[groupAlive] != pickEvery {
		t.Errorf("picks that reveal the inner widget: %v", got)
	}
	if got := reveal("d_not", map[string]string{groupAlive: pickEvery}); got[groupAlive] != pickNone || len(got) != 1 {
		t.Errorf("picks that reveal the widget under Not: %v", got)
	}
	if got := reveal("d_trait_b", map[string]string{groupBold: pickNone}); got[groupBold] != pickEvery {
		t.Errorf("picks that reveal the second trait: %v", got)
	}
	if got := reveal("d_child", nil); got != nil {
		t.Errorf("picks for conditions that contradict: %v", got)
	}
	if got := reveal("d_off", nil); got != nil {
		t.Errorf("picks for a widget that is never visible: %v", got)
	}
}

// TestResolveCoatReadsLayersParentsAndLists checks a coat's layers, colors and
// instances, a coat with a parent, one that is the name of another, and the
// option a template list draws.
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
	wantColors := []string{fallbackColor, cssRed, cssBlue, "", "", ""}
	if pattern.Role != lang.CoatPatternKey || pattern.Texture != patternFile || !strings.HasSuffix(filepath.ToSlash(pattern.Path), "patterns/v_pattern.dds") ||
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
	if len(equal.Instances) != 1 || equal.Instances[0].Range != (jomini.Range{}) {
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

// TestEditPropertiesReplacesOrAddsKey checks that an edit replaces a key the
// block writes and adds one it does not, and that a range where no block starts
// is an error.
func TestEditPropertiesReplacesOrAddsKey(t *testing.T) {
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
	edited, _ := makeEditor(t, string(windowText))(drw.EditProperties(window.Places[widget.Place].Path, []Setting{{Block: widget.Range, Key: "visible", Value: "no"}}))
	if !strings.Contains(edited, `name = "d_trait_a" visible = no }`) {
		t.Errorf("value replaced in a block on one line: %s", edited)
	}
	edit := makeEditor(t, string(coatText))
	listed := lo.Must(drw.ResolveCoat(coatFile, "d_listed", nil))
	_, replaced := edit(drw.EditProperties(coatFile, []Setting{{Block: listed.Range, Key: lang.CoatPatternKey, Value: `"v_pattern.dds"`}}))
	pattern := replaced.Statements[lo.Must(replaced.FindChild(lo.Must(replaced.FindChild(0, "d_listed")), lang.CoatPatternKey))]
	if !pattern.Tag.IsEmpty() || replaced.Text(pattern.Value) != `"v_pattern.dds"` {
		t.Errorf("a list replaced by a plain value, tag and all: %q", replaced.Text(pattern.Span))
	}
	parent := lo.Must(drw.ResolveCoat(coatFile, coatParent, nil))
	emblem, instance := parent.Layers[1], parent.Layers[1].Instances[0]
	_, retextured := edit(drw.EditProperties(coatFile, []Setting{{Block: emblem.Range, Key: lang.CoatTextureKey, Value: `"v_textured.dds"`}}))
	block := lo.Must(retextured.FindChild(lo.Must(retextured.FindChild(0, coatParent)), emblemKey))
	if got := retextured.Unquoted(retextured.Statements[lo.Must(retextured.FindChild(block, lang.CoatTextureKey))].Value); got != "v_textured.dds" {
		t.Errorf("texture after the edit: %s", got)
	}
	deeper, _ := edit(drw.EditProperties(coatFile, []Setting{{Block: instance.Range, Key: "depth", Value: "5"}}))
	if !strings.Contains(deeper, "rotation = 45 depth = 5 }") {
		t.Errorf("depth after the edit: %s", deeper)
	}
	added, _ := edit(drw.EditProperties(coatFile, []Setting{{Block: emblem.Range, Key: slotTwo, Value: `"white"`}}))
	if !strings.Contains(added, "\n\t\tcolor2 = \"white\"\n") {
		t.Errorf("a color added to the emblem, in the file's indentation:\n%s", added)
	}
	_, colored := edit(drw.EditProperties(coatFile, []Setting{{Block: parent.Range, Key: "color3", Value: `"white"`}}))
	if _, ok := colored.FindChild(lo.Must(colored.FindChild(0, coatParent)), "color3"); !ok {
		t.Error("a color was not added to the coat")
	}
	if _, err := drw.EditProperties(coatFile, []Setting{{Block: parent.Layers[0].Range, Key: slotOne, Value: "red"}}); err == nil {
		t.Error("a key was set in the statement of a pattern, which is no block")
	}
}

// TestEditCoatLayers checks that each edit of a coat's layers leaves the coat
// as the edit says, and that a second pattern and a coat written as another's
// name are errors.
func TestEditCoatLayers(t *testing.T) {
	drw, coatFile, text := openCoatFile(t)
	edit := makeEditor(t, text)
	_, added := edit(drw.AddLayer(coatFile, coatParent, emblemKey, 1))
	if got := listKeys(added, coatParent); !slices.Equal(got, []string{lang.CoatPatternKey, emblemKey, slotOne, slotTwo, emblemKey}) {
		t.Errorf("keys after adding an emblem: %v", got)
	}
	emblem := lo.Must(drw.ResolveCoat(coatFile, coatParent, nil)).Layers[1]
	_, removed := edit(drw.RemoveBlock(coatFile, emblem.Range))
	if got := listKeys(removed, coatParent); !slices.Equal(got, []string{lang.CoatPatternKey, slotOne, slotTwo}) {
		t.Errorf("keys after removing the emblem: %v", got)
	}
	_, moved := edit(drw.MoveLayer(coatFile, coatParent, 1, 0))
	if got := listKeys(moved, coatParent); !slices.Equal(got, []string{emblemKey, lang.CoatPatternKey, slotOne, slotTwo}) {
		t.Errorf("keys after moving the emblem first: %v", got)
	}
	_, later := edit(drw.MoveLayer(coatFile, coatParent, 0, 1))
	if got := listKeys(later, coatParent); !slices.Equal(got, []string{slotOne, slotTwo, emblemKey, lang.CoatPatternKey}) {
		t.Errorf("keys after moving the pattern last: %v", got)
	}
	for name, err := range map[string]error{
		"a second pattern":      errOf(drw.AddLayer(coatFile, coatParent, lang.CoatPatternKey, 0)),
		"a layer after nothing": errOf(drw.AddLayer(coatFile, coatParent, emblemKey, 9)),
		"a layer of an alias":   errOf(drw.AddLayer(coatFile, "d_alias", emblemKey, 0)),
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

// TestResolveWindowsOnInstalls resolves every window of each real install that
// PMT_TEST_INSTALL_<GAME> names, with no mods, and evaluates its states. A
// window that fails to resolve fails the test.
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

// TestResolveCoatsOnInstalls resolves every coat of each real install that
// PMT_TEST_INSTALL_<GAME> names, with no mods. A coat that fails to resolve
// fails the test.
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
	windows, errors, widgets, groups, hidden                      int
	texts, textsMissing, paths, pathsMissing, calls, callsMissing int
	missing                                                       map[string][]string
}

// add resolves every window of a GUI file, evaluates its states and counts the
// result.
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
	for _, drawable := range drawables {
		if drawable.Database != lang.GUIWidgetDatabase {
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
			totals.count(ws, &window, path, widget)
		}
		states, _ := drw.EvaluateStates(path, drawable.Name, nil)
		totals.groups += len(window.Groups)
		totals.hidden += len(states.Hidden)
	}
}

// count adds the missing types and templates, texts and textures of one of a
// window's widgets to the totals. A picture written as a data call is one only
// the running game answers.
func (totals *gateTotals) count(ws *workspace.Session, window *Window, path string, widget Widget) {
	mods := resolveIndex(ws, path)
	for property := range window.listProperties(widget) {
		_, textured := widget.Textures[property.Key]
		switch {
		case property.Origin == originMissing:
			label := property.Key + " " + property.Value
			totals.missing[label] = append(totals.missing[label], path)
		case slices.Contains(pictureKeys, property.Key) && !strings.HasPrefix(property.Value, "["):
			totals.paths++
			totals.pathsMissing += lo.Ternary(textured, 0, 1)
		case slices.Contains(pictureKeys, property.Key):
			totals.calls++
			totals.callsMissing += lo.Ternary(textured, 0, 1)
		case isLocalized(property.Key) && !strings.Contains(property.Value, "["):
			_, _, listed := ws.LookupText(mods, property.Value)
			totals.texts++
			totals.textsMissing += lo.Ternary(listed, 0, 1)
		}
	}
}

// log prints the totals, then each missing type or template with the files that
// use it.
func (totals *gateTotals) log(t *testing.T, elapsed time.Duration) {
	t.Helper()
	t.Logf("%d windows, %d errors, %d widgets, %d groups, %d hidden as the windows open, %s", totals.windows, totals.errors,
		totals.widgets, totals.groups, totals.hidden, elapsed.Round(time.Millisecond))
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
		case lang.CoatPatternKey:
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
