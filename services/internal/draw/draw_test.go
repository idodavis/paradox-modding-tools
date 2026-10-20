// draw_test.go covers the render on the ck3 fixture install: the properties reader, the opening state, a window
// resolved through its types, templates and blocks, the properties, roles and bindings its widgets carry, an entry
// drawn alone, an element's properties, a coat of arms resolved into layers and the designer's lists; and on the three
// installs, a window and a coat each.

package draw

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/index"
	"paradox-modding-tools/services/internal/jomini"
	"paradox-modding-tools/services/internal/workspace"
)

var installs sync.Map

const (
	ck3          = "ck3"
	layoutWindow = "v_layout_window"
	buttonType   = "button"
	coatBase     = "v_coat_base"
)

// fixture returns a session over the ck3 fixture install, scanned once under an empty home so the schema is the shipped
// one, with an empty mod folder per test.
func fixture(t *testing.T) (*workspace.Session, string) {
	t.Helper()
	home := t.TempDir()
	for _, name := range []string{"HOME", "USERPROFILE", "APPDATA", "LOCALAPPDATA", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME"} {
		t.Setenv(name, home)
	}
	scan, _ := installs.LoadOrStore(ck3, sync.OnceValues(func() (*index.Install, error) {
		dir, err := filepath.Abs(filepath.Join("..", "testdata", ck3))
		if err != nil {
			return nil, err
		}
		return index.Scan(t.Context(), index.Request{Game: ck3, Dir: dir, Version: "test"})
	}))
	install, err := scan.(func() (*index.Install, error))()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "common"), 0o755); err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.Open(t.Context(), game.Lookup(ck3), install, []workspace.Mod{{ID: "mod", Dir: dir}}, "")
	if err != nil {
		t.Fatal(err)
	}
	return ws, dir
}

// request returns the request for an entry of the fixture's window file.
func request(id string) Request {
	path, _ := filepath.Abs(filepath.Join("..", "testdata", ck3, "game", "gui", "v_window.gui"))
	return Request{Path: path, ID: id}
}

// modRequest returns the request for a window of a mod file written for the test.
func modRequest(ws *workspace.Session, dir, id, text string) Request {
	path := filepath.Join(dir, "gui", id+".gui")
	ws.DidChange(path, text)
	return Request{Path: path, ID: id}
}

// named returns the index of the widget with a name; -1 for none.
func named(list *List, name string) int {
	return slices.IndexFunc(list.Elements, func(widget Widget) bool { return widget.Name == name })
}

// childrenOf returns the elements under a parent, in order.
func childrenOf(list *List, parent int) []int {
	var children []int
	for i, element := range list.Elements {
		if element.Parent == parent && i > 0 {
			children = append(children, i)
		}
	}
	return children
}

// widgetOf returns a widget holding the fields a parsed statement's block writes.
func widgetOf(text string) *widget {
	parsed := jomini.Parse(text)
	widget := &widget{}
	for _, field := range jomini.Fields(jomini.ChildBlock(parsed.Root.Statements[0])) {
		widget.add(statement{assignment: field, parsed: &parsed})
	}
	return widget
}

func TestBlockReader(t *testing.T) {
	widget := widgetOf("x = {\n\tsize = { 1 1 }\n\talpha = 1\n\tsize = { 2 50% }\n\tparentanchor = bottom|hcenter\n}\n")
	if numbers, has := widget.numbers(sizeKey); has {
		t.Errorf("the last size wins, and a percent is no number Go reads: %v", numbers)
	}
	if choices, _ := widget.choices("parentanchor"); fmt.Sprint(choices) != "[bottom hcenter]" {
		t.Errorf("anchor choices: %v", choices)
	}
	if _, has := widget.last("max_width"); has {
		t.Error("a key never written is absent")
	}
	if size, _ := widget.last(sizeKey); strings.Join(size.scalars(), " ") != "2 50%" || len(widget.statements) != 4 {
		t.Errorf("the last of a key, its scalars as written: %v", size.scalars())
	}
}

func TestOpeningState(t *testing.T) {
	widget := widgetOf("w = {\n\tposition_x = -60\n\tstate = { name = _show position_x = 0 alpha = 1 }\n" +
		"\tstate = { name = _hide position_x = -60 }\n\tstate = { name = other trigger_on_create = yes alpha = 0.5 }\n}\n")
	(&guiResolver{}).openingState(widget)
	if position, _ := widget.numbers("position_x"); fmt.Sprint(position) != "[0]" {
		t.Errorf("_show's position applies: %v", position)
	}
	if alpha, _ := widget.numbers("alpha"); fmt.Sprint(alpha) != "[0.5]" {
		t.Errorf("a trigger_on_create state applies after _show: %v", alpha)
	}
}

func TestResolveFixture(t *testing.T) {
	ws, _ := fixture(t)
	resolver, err := newGUIResolver(newResolver(ws, request(layoutWindow)))
	if err != nil {
		t.Fatal(err)
	}
	list := resolver.list
	elements := list.Elements
	if elements[0].Type != "window" || elements[0].Key != "window" || elements[0].Name != layoutWindow {
		t.Errorf("the root: %+v", elements[0])
	}
	box := childrenOf(list, 0)[0]
	children := childrenOf(list, box)
	if elements[box].Type != "vbox" || elements[box].Holding != game.HoldsColumn || !elements[box].DropsHidden || len(children) != 7 {
		t.Fatalf("v_box resolves to a vbox of seven children, holding a column: %+v, %d", elements[box], len(children))
	}
	if elements[box].Label != "v_box" || elements[box].Path != "window v_layout_window › v_box" ||
		fmt.Sprint(elements[box].Ancestors) != "[0]" || elements[box].Location.Rel != "gui/v_window.gui" {
		t.Errorf("an element is named and placed by the resolver: %+v", elements[box])
	}
	if size, _ := resolver.widgets[box].numbers(sizeKey); fmt.Sprint(size) != "[100 20]" || elements[box].Properties["spacing"] != "4" {
		t.Errorf("the template's size and the own block's spacing apply: %v %v", size, elements[box].Properties)
	}
	first := resolver.widgets[children[0]]
	if first.builtIn != buttonType || first.statements[len(first.statements)-1].writer != "" {
		t.Errorf("the override's button replaces the block's, written by the widget: %+v", first.builtIn)
	}
	if label := elements[children[2]]; label.Text == nil || len(label.Text.Runs) != 1 || label.Text.Runs[0].Text != "Fixture text" {
		t.Errorf("the loc key's text as runs: %+v", label.Text)
	}
	textures := list.Textures
	if icon := elements[children[3]]; len(icon.Textures) != 2 || textures[icon.Textures[0]].Width != 2 ||
		!strings.HasSuffix(textures[icon.Textures[0]].URL, "&frame=2,0,2,2") ||
		!strings.HasSuffix(textures[icon.Textures[1]].URL, "&frame=0,2,2,2") || textures[icon.Textures[1]].Mask != -1 ||
		textures[icon.Textures[1]].Alpha != 1 {
		t.Errorf("the texture by its path at its upframe, and a modify_texture's frame picked in its block, at full strength: %+v",
			textures)
	}
	if rows := childrenOf(list, children[4]); len(rows) != 1 || !elements[rows[0]].IsItem {
		t.Errorf("a data model's item is marked for the page to draw once a row: %+v", rows)
	}
	if conditional := elements[children[6]]; conditional.IsHidden || conditional.VisibleBinding != "[GetPlayer.IsAlive]" {
		t.Errorf("a visible that is a binding is the game's to answer, so the widget is drawn with it: %+v", conditional)
	}
	frame := slices.IndexFunc(list.Missing, func(absent Absent) bool { return absent.Text == "Texture not found: v_frame.dds" })
	if frame < 0 || elements[list.Missing[frame].Element].Key != "v_button" {
		t.Errorf("a texture by a path nothing holds is listed at the widget that asks for it: %+v", list.Missing)
	}
	if len(list.Fonts) != 1 || list.Fonts[0].Name != "v_font" || list.Fonts[0].Style != "regular" {
		t.Errorf("one font row: %+v", list.Fonts)
	}
	for i, element := range elements {
		if element.Parent >= i && i > 0 || element.Parent != -1 && i == 0 {
			t.Errorf("element %d's parent %d is not before it", i, element.Parent)
		}
	}
	if _, err := (&Render{}).List(ws, request("no_such_window")); err == nil {
		t.Error("an id the file does not hold is refused")
	}
}

func TestPropertiesAsWritten(t *testing.T) {
	ws, _ := fixture(t)
	list, err := (&Render{}).List(ws, request("v_layout_rules"))
	if err != nil {
		t.Fatal(err)
	}
	if list.IsAlone {
		t.Errorf("a window is drawn on the screen, not alone: %+v", list)
	}
	for _, tt := range []struct{ name, key, want string }{
		{"v_row", "layoutpolicy_horizontal", "expanding"},
		{"v_footer", "parentanchor", "bottom|hcenter"},
		{"v_multi", "multiline", "yes"},
		{"v_unset_icon", sizeKey, "-1 10"},
		{"v_scroll", sizeKey, "50 50"},
	} {
		if element := named(list, tt.name); element < 0 || list.Elements[element].Properties[tt.key] != tt.want {
			t.Errorf("%s carries %s as written: %+v", tt.name, tt.key, list.Elements[max(element, 0)].Properties)
		}
	}
	row := named(list, "v_row")
	tabs := slices.IndexFunc(list.Elements, func(widget Widget) bool { return widget.Parent == row })
	if tabs < 0 || list.Elements[tabs].Properties[sizeKey] != "100% 10" || list.Elements[tabs].Type != buttonType ||
		!list.Elements[tabs].CentersText {
		t.Errorf("a percent is carried as written, through the type, whose row the element carries: %+v", list.Elements[max(tabs, 0)])
	}
	if background := list.Elements[named(list, "v_background")]; background.Role != game.RoleSkin {
		t.Errorf("a background is the window's skin: %+v", background)
	}
	if bar := list.Elements[named(list, "v_bar")]; bar.Role != game.RoleChrome || bar.Type != "scrollbar" || bar.Parent != named(list, "v_scroll") {
		t.Errorf("a scroll bar is its area's chrome, a scrollbar: %+v", bar)
	}
	if scroll := list.Elements[named(list, "v_scroll")]; scroll.Holding != game.HoldsScroll || !scroll.Clips {
		t.Errorf("a scroll area holds a scroll and clips: %+v", scroll)
	}
	if named(list, "v_tip") >= 0 {
		t.Error("a tooltip is left out of the window")
	}
	alive, notAlive, always := list.Elements[named(list, "v_yes")], list.Elements[named(list, "v_no")],
		list.Elements[named(list, "v_after")]
	if alive.VisibleBinding != "[GetPlayer.IsAlive]" || notAlive.VisibleBinding != "[Not( GetPlayer.IsAlive )]" ||
		always.VisibleBinding != "" {
		t.Errorf("each sibling carries the binding it is shown on, as written: %q %q %q", alive.VisibleBinding,
			notAlive.VisibleBinding, always.VisibleBinding)
	}
	fact := Condition{Group: alive.Condition.Group, Value: yes}
	if fmt.Sprint(*alive.Condition, *notAlive.Condition) != fmt.Sprint(fact, Condition{Operator: "not", Operands: []Condition{fact}}) ||
		always.Condition != nil || list.Groups[fact.Group].Call != "GetPlayer.IsAlive" {
		t.Errorf("a negation is Not over the same group's fact: %+v %+v %+v", alive.Condition, notAlive.Condition, list.Groups)
	}
}

func TestVisible(t *testing.T) {
	ws, dir := fixture(t)
	list, err := (&Render{}).List(ws, modRequest(ws, dir, "h", "window = {\n\tname = \"h\"\n\tsize = { 20 20 }\n\tvisible = no\n"+
		"\twidget = { visible = no size = { 10 10 } }\n\twidget = { visible = \"[GetPlayer.IsAlive]\" size = { 10 10 } }\n}\n"))
	if err != nil {
		t.Fatal(err)
	}
	var hidden []bool
	for _, element := range list.Elements {
		hidden = append(hidden, element.IsHidden)
	}
	if fmt.Sprint(hidden) != "[false true false]" {
		t.Errorf("the entry is drawn whatever its visible says, visible = no hides, a binding draws: %v", hidden)
	}
}

func TestProperties(t *testing.T) {
	ws, dir := fixture(t)
	window := modRequest(ws, dir, "m", "window = {\n\tname = \"m\"\n\tv_box = {\n\t\tusing = v_sized\n\t\tspacing = 4\n\t}\n}\n")
	render := &Render{}
	rows, err := render.Properties(ws, window, 1)
	if err != nil {
		t.Fatal(err)
	}
	size := slices.IndexFunc(rows, func(row Property) bool { return row.Key == sizeKey })
	spacing := slices.IndexFunc(rows, func(row Property) bool { return row.Key == "spacing" })
	if size < 0 || spacing < 0 || rows[size].Writer != "v_sized" || rows[size].Value != "{ 100 20 }" || rows[spacing].Writer != "" {
		t.Errorf("the rows say what each is written as and who wrote it: %+v", rows)
	}
	if _, err := render.Properties(ws, window, 9); err == nil {
		t.Error("an element the window has not is refused")
	}
}

func TestEntriesAlone(t *testing.T) {
	ws, _ := fixture(t)
	render := &Render{}
	button, err := render.List(ws, request("v_button"))
	if err != nil || len(button.Elements) != 1 || button.Elements[0].Type != buttonType || button.Elements[0].Properties[sizeKey] != "10 10" ||
		!button.IsAlone {
		t.Errorf("a type is drawn alone as one widget of its type: %+v, %v", button, err)
	}
	template, err := render.List(ws, request("v_faded"))
	if err != nil || len(template.Elements) != 1 || template.Elements[0].Key != "widget" || !template.IsAlone {
		t.Errorf("a template is drawn alone as a widget that uses it: %+v, %v", template, err)
	}
	icon, err := render.List(ws, request("v_icon"))
	if err != nil || len(icon.Elements) != 1 || icon.Elements[0].Properties[sizeKey] != "8 8" || len(icon.Elements[0].Textures) != 1 ||
		!strings.Contains(icon.Textures[icon.Elements[0].Textures[0]].URL, "v_bg.dds") {
		t.Errorf("a text icon is its texture at its size: %+v, %v", icon.Elements, err)
	}
	format, err := render.List(ws, request("v_format"))
	if err != nil || len(format.Elements) != 1 || format.Elements[0].Text == nil || format.Elements[0].Text.Runs[0].Text != "Sample text" ||
		!format.Elements[0].Text.Runs[0].IsBold {
		t.Errorf("a format is a sample text in it: %+v, %v", format.Elements, err)
	}
}

func TestTextDefault(t *testing.T) {
	ws, _ := fixture(t)
	list, err := (&Render{}).List(ws, request("v_default_text"))
	if err != nil {
		t.Fatal(err)
	}
	texts := 0
	for _, element := range list.Elements {
		if element.Text == nil {
			continue
		}
		texts++
		if element.Text.Font != "v_font" || element.Text.Size != 12 {
			t.Errorf("a text with no font size takes the install's textbox style: %+v", element.Text)
		}
	}
	if texts != 2 {
		t.Errorf("a textbox and a button, both with a text: %d drawn, missing %v", texts, list.Missing)
	}
}

func TestBindingsAsWritten(t *testing.T) {
	ws, _ := fixture(t)
	list, err := (&Render{}).List(ws, request("v_other_window"))
	if err != nil {
		t.Fatal(err)
	}
	var texts []string
	for _, element := range list.Elements {
		if element.Text != nil {
			texts = append(texts, element.Text.Runs[0].Text, element.Text.Runs[0].Call)
		}
	}
	if fmt.Sprint(texts) != "[[…] [GetPlayer.GetName]]" {
		t.Errorf("a text that is a binding is drawn as a value the game fills, the call kept as written: %q", texts)
	}
}

// typesOf returns a coat's layer types in order.
func typesOf(coat *Coat) string {
	var types []string
	for _, layer := range coat.Layers {
		types = append(types, layer.Type)
	}
	return strings.Join(types, " ")
}

func TestResolveCoat(t *testing.T) {
	ws, _ := fixture(t)
	path, _ := filepath.Abs(filepath.Join("..", "testdata", ck3, "game", "common", "coat_of_arms", "coat_of_arms", "v_coats.txt"))
	coatOf := func(id string) *Coat {
		coat, err := CoatOf(ws, Request{Path: path, ID: id})
		if err != nil {
			t.Fatal(err)
		}
		return coat
	}
	base := coatOf(coatBase)
	if got := typesOf(base); got != "coat pattern emblem instance emblem instance emblem instance" || len(base.Missing) != 0 {
		t.Errorf("the coat, its pattern, then each run of an emblem's instances back to front by depth: %s, %+v", got, base.Missing)
	}
	if base.Width != 230 || base.Height != 230 || !strings.Contains(base.Shading, "coa_mask_texture.dds") {
		t.Errorf("the coat is drawn on the first atlas' tile, shaded by the game's cloth: %d %d %q", base.Width, base.Height, base.Shading)
	}
	white, blue := "rgba(255, 255, 255, 1)", base.Layers[1].Colors[3]
	if pattern := base.Layers[1]; pattern.Recolor != patternKey || fmt.Sprint(pattern.Colors[:3]) != "[ rgba(255, 0, 0, 1) "+white+"]" ||
		blue == "" || !strings.Contains(pattern.URL, "v_pattern.dds") {
		t.Errorf("the pattern paints its file colored by the coat's named colors: %+v", pattern)
	}
	emblem, rotated := base.Layers[2], base.Layers[7]
	if fmt.Sprint(emblem.Regions) != "[1]" || !strings.Contains(emblem.Mask, "v_pattern.dds") {
		t.Errorf("a masked emblem is masked by its pattern's regions: %+v", emblem)
	}
	if colors := rotated.Colors; fmt.Sprint(colors[1:4]) != fmt.Sprint([]string{white, white, blue}) {
		t.Errorf("color1 = color2 is the coat's second slot, an unwritten slot the coat's: %v", colors)
	}
	if rotated.Label != "instance 2" || rotated.Properties["rotation"] != "45" || rotated.Properties["scale"] != "-0.5 0.5" {
		t.Errorf("an instance carries its properties as written, named by its number: %+v", rotated)
	}
	sub := coatOf("v_coat_sub")
	if got := typesOf(sub); got != "coat pattern sub pattern emblem instance emblem instance emblem instance" {
		t.Errorf("a sub is a coat under the root, its parent's layers its own: %s", got)
	}
	if placed := sub.Layers[2]; placed.Label != "sub "+coatBase || placed.Properties["offset"] != "0.5 0" {
		t.Errorf("a sub carries its instance's place, named for its parent: %+v", placed)
	}
	rows, err := (&Render{}).Properties(ws, Request{Path: path, ID: "v_coat_sub", Database: game.CoatDatabases.Coats}, 3)
	if err != nil || len(rows) == 0 || rows[0].Key != patternKey || rows[0].Writer != coatBase ||
		rows[0].ValueLocation.Range.Start.Character != 11 {
		t.Errorf("the sub's pattern is its parent's, written by the parent, its value's place kept: %+v %v", rows, err)
	}
	if colors := sub.Layers[3].Colors; colors[2] != "rgba(0, 255, 0, 1)" {
		t.Errorf("the sub's color is over its parent's: %v", colors)
	}
	if alias := coatOf("v_coat_alias"); typesOf(alias) != typesOf(base) || alias.Layers[0].Key != coatBase ||
		alias.Layers[0].Name != "v_coat_alias" {
		t.Errorf("a coat written as another's id draws that coat: %+v", alias.Layers)
	}
}

func TestCoatLists(t *testing.T) {
	ws, dir := fixture(t)
	if red := Colors(ws)["v_red"]; red != "rgba(255, 0, 0, 1)" {
		t.Errorf("a named color is listed with its CSS: %q", red)
	}
	if textures := Textures(ws, patternKey); len(textures) != 1 || !strings.Contains(textures["v_pattern.dds"], "v_pattern.dds") {
		t.Errorf("a key's folder lists its images by file name: %+v", textures)
	}
	if file, err := CoatFile(ws, "mod"); err != nil || filepath.Dir(file.Path) != filepath.Join(dir, "common", "coat_of_arms", "coat_of_arms") {
		t.Errorf("a mod with no coat file writes a new one in its game's coat folder: %+v %v", file, err)
	}
}

// TestInstallsResolve gates the real installs: a named window of each resolves with every type and template found, and
// a named coat with nothing missing. It runs only with PMT_VANILLA_GATE=1 and the three PMT_TEST_INSTALL_ variables.
func TestInstallsResolve(t *testing.T) {
	if testing.Short() || os.Getenv("PMT_VANILLA_GATE") == "" {
		t.Skip("resolves windows of the three installs; set PMT_VANILLA_GATE=1")
	}
	coats := "common/coat_of_arms/coat_of_arms/"
	for _, tt := range []struct{ id, env, path, entry, coatPath, coat string }{
		{ck3, "PMT_TEST_INSTALL_CK3", "gui/window_character.gui", "character_window", coats + "01_landed_titles.txt", "k_papal_state"},
		{"vic3", "PMT_TEST_INSTALL_VIC3", "gui/politics_panel.gui", "politics_panel", coats + "02_countries.txt", "AUS_absolute_monarchy"},
		{"eu5", "PMT_TEST_INSTALL_EU5", "gui/eventwindow.gui", "event_window", coats + "pre_scripted_countries.txt", "ABW"},
	} {
		dir := os.Getenv(tt.env)
		if dir == "" {
			t.Errorf("%s: %s is not set", tt.id, tt.env)
			continue
		}
		install, err := index.Scan(t.Context(), index.Request{Game: tt.id, Dir: dir, Version: "gate"})
		if err != nil {
			t.Fatal(err)
		}
		ws, err := workspace.Open(t.Context(), game.Lookup(tt.id), install, nil, "")
		if err != nil {
			t.Fatal(err)
		}
		file, isListed := index.FileAt(ws.Indexes(), tt.path)
		if !isListed {
			t.Errorf("%s: %s is not in the install", tt.id, tt.path)
			continue
		}
		started := time.Now()
		list, err := (&Render{}).List(ws, Request{Path: file.Path, ID: tt.entry})
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("%s %s: %d elements, %d fonts, %d missing in %s",
			tt.id, tt.entry, len(list.Elements), len(list.Fonts), len(list.Missing), time.Since(started).Round(time.Millisecond))
		if len(list.Elements) == 0 {
			t.Errorf("%s %s: nothing is drawn", tt.id, tt.entry)
		}
		causes := map[string]int{}
		for _, missing := range list.Missing {
			cause, _, _ := strings.Cut(missing.Text, ":")
			causes[cause]++
			if cause == "Type not found" || cause == "Template not found" {
				t.Errorf("%s %s: the install declares no %s", tt.id, tt.entry, missing.Text)
			}
		}
		t.Logf("%s missing by cause: %v", tt.id, causes)
		if len(list.Fonts) == 0 {
			t.Errorf("%s: the texts' fonts are listed", tt.id)
		}
		coatResolves(t, ws, tt.coatPath, tt.coat)
	}
}

// coatResolves checks that an install's coat of arms resolves with nothing missing but what a list picks.
func coatResolves(t *testing.T, ws *workspace.Session, path, id string) {
	t.Helper()
	file, _ := index.FileAt(ws.Indexes(), path)
	coat, err := CoatOf(ws, Request{Path: file.Path, ID: id, Database: game.CoatDatabases.Coats})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s: %s", id, typesOf(coat))
	for _, missing := range coat.Missing {
		if !strings.HasPrefix(missing.Text, "Picked from a list") {
			t.Errorf("%s: %s", id, missing.Text)
		}
	}
}
