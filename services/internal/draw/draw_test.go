// draw_test.go covers the GUI render on the ck3 fixture install: the block reader, the opening state, a window
// resolved through its types, templates and blocks, the layout's branches and rules, an entry drawn alone, and a
// part's properties.

package draw

import (
	"fmt"
	"math"
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
)

// fixture is a session over the ck3 fixture install, scanned once under an empty home so the schema is the shipped
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
	ws, err := workspace.Open(t.Context(), game.Get(ck3), install, []workspace.Mod{{ID: "mod", Dir: dir}}, "")
	if err != nil {
		t.Fatal(err)
	}
	return ws, dir
}

// request is the fixture window drawn at the board's resolution.
func request(name string) Request {
	path, _ := filepath.Abs(filepath.Join("..", "testdata", ck3, "game", "gui", "v_window.gui"))
	return Request{Path: path, Name: name, Width: 1920, Height: 1080, Scale: 1, Language: game.BaseLanguage}
}

// modRequest is a window of a mod file written for the test, drawn at a small resolution.
func modRequest(ws *workspace.Session, dir, name, text string) Request {
	path := filepath.Join(dir, "gui", name+".gui")
	ws.DidChange(path, text)
	return Request{Path: path, Name: name, Width: 100, Height: 100, Scale: 1, Language: game.BaseLanguage}
}

func TestBlockReader(t *testing.T) {
	parsed := jomini.Parse("x = {\n\tsize = { 1 1 }\n\talpha = 1\n\tsize = { 2 50% }\n\tparentanchor = bottom|hcenter\n}\n")
	node := &widget{}
	for _, field := range jomini.Fields(jomini.ChildBlock(parsed.Root.Statements[0])) {
		node.statements = append(node.statements, statement{assign: field, parsed: &parsed})
	}
	if numbers, _ := node.numbers("size"); fmt.Sprint(numbers) != "[2 -0.5]" {
		t.Errorf("the last size wins, a percent is a negative fraction: %v", numbers)
	}
	if choices, _ := node.choices("parentanchor"); fmt.Sprint(choices) != "[bottom hcenter]" {
		t.Errorf("anchor choices: %v", choices)
	}
	if _, has := node.last("max_width"); has {
		t.Error("a key never written is absent")
	}
}

func TestOpeningState(t *testing.T) {
	parsed := jomini.Parse("w = {\n\tposition_x = -60\n\tstate = { name = _show position_x = 0 alpha = 1 }\n" +
		"\tstate = { name = _hide position_x = -60 }\n\tstate = { name = other trigger_on_create = yes alpha = 0.5 }\n}\n")
	node := &widget{}
	for _, field := range jomini.Fields(jomini.ChildBlock(parsed.Root.Statements[0])) {
		node.statements = append(node.statements, statement{assign: field, parsed: &parsed})
	}
	(&resolver{}).opening(node)
	if position, _ := node.numbers("position_x"); fmt.Sprint(position) != "[0]" {
		t.Errorf("_show's position applies: %v", position)
	}
	if alpha, _ := node.numbers("alpha"); fmt.Sprint(alpha) != "[0.5]" {
		t.Errorf("a trigger_on_create state applies after _show: %v", alpha)
	}
}

func TestResolveFixture(t *testing.T) {
	ws, _ := fixture(t)
	resolved, root, err := resolve(ws, request(layoutWindow))
	if err != nil {
		t.Fatal(err)
	}
	if root.kind != "window" || resolved.list.Parts[0].Key != "window" || resolved.list.Parts[0].Name != layoutWindow {
		t.Errorf("the root: %+v", resolved.list.Parts[0])
	}
	box := root.children[0]
	if box.kind != "vbox" || len(box.children) != 7 {
		t.Fatalf("v_box resolves to a vbox of seven children: %s, %d", box.kind, len(box.children))
	}
	if size, _ := box.numbers("size"); fmt.Sprint(size) != "[100 20]" {
		t.Errorf("the template's size applies: %v", size)
	}
	if spacing, _ := box.numbers("spacing"); fmt.Sprint(spacing) != "[4]" {
		t.Errorf("the own block's spacing applies: %v", spacing)
	}
	if first := box.children[0]; first.kind != "button" || first.statements[len(first.statements)-1].writtenBy != "" {
		t.Errorf("the override's button replaces the block's, written by the widget: %+v", first.kind)
	}
	if label := box.children[2]; label.text == nil || len(label.pieces) != 2 || label.pieces[0].Text != "Fixture " {
		t.Errorf("the loc key's text, measured by its words: %+v", box.children[2].pieces)
	}
	if icon := box.children[3]; len(icon.textures) != 1 || icon.textures[0].Width != 4 {
		t.Errorf("the texture by its path: %+v", box.children[3].textures)
	}
	if list := box.children[4]; list.rows != 3 {
		t.Errorf("a data model draws three rows: %d", list.rows)
	}
	if bound := box.children[6]; bound.hidden {
		t.Errorf("a visible that is a binding is the game's to answer, so the widget is drawn: %+v", bound)
	}
	if !resolved.missing["Texture not found: v_frame.dds"] {
		t.Errorf("a texture by a path nothing holds is listed: %v", resolved.missing)
	}
	if len(resolved.list.Fonts) != 1 || resolved.list.Fonts[0].Name != "v_font" || resolved.list.Fonts[0].Style != "regular" {
		t.Errorf("one font row: %+v", resolved.list.Fonts)
	}
	for i, part := range resolved.list.Parts {
		if part.Parent >= i && i > 0 || part.Parent != -1 && i == 0 {
			t.Errorf("part %d's parent %d is not before it", i, part.Parent)
		}
	}
	if _, err := GUI(ws, request("no_such_window")); err == nil {
		t.Error("a name the file does not hold is refused")
	}
}

func TestLayoutBranches(t *testing.T) {
	five := []item{{310, 310, 1e9, 0, false}, {0, 0, 1e9, 0, false}, {232, 232, 1e9, 0, false}, {0, 0, 1e9, 0, false},
		{350, 350, 1e9, 0, false}, {100, 100, 1e9, 0, true}, {100, 100, 1e9, 0, true}, {100, 100, 1e9, 0, true},
		{100, 100, 1e9, 0, true}, {100, 100, 1e9, 0, true}} // a main column of ten children at a height of 1080
	for _, tt := range []struct {
		what  string
		items []item
		room  float64
		want  []float64
	}{
		{"below the minimums every child keeps its minimum", five, 1080, []float64{310, 0, 232, 0, 350, 100, 100, 100, 100, 100}},
		{"above the hints the expanding children share the extra", five, 1500, []float64{310, 0, 232, 0, 350, 121.6, 121.6, 121.6, 121.6, 121.6}},
		{"between, each gives up in proportion", []item{{50, 100, 1e9, 0, true}, {50, 100, 1e9, 0, true}, {50, 100, 1e9, 0, true}}, 240, []float64{80, 80, 80}},
		{"a stretch factor takes the extra before an expanding child", []item{{0, 100, 1e9, 2, true}, {0, 100, 1e9, 1, true}, {0, 100, 1e9, 0, true}}, 600, []float64{300, 200, 100}},
		{"a maximum caps a child and the rest share again", []item{{0, 100, 150, 0, true}, {0, 100, 1e9, 0, true}}, 400, []float64{150, 250}},
		{"nobody grows when nothing expands", []item{{0, 100, 1e9, 0, false}, {0, 100, 1e9, 0, false}}, 400, []float64{100, 100}},
	} {
		hints, minimums := 0.0, 0.0
		for _, one := range tt.items {
			hints, minimums = hints+one.preferred, minimums+one.minimum
		}
		var got []float64
		switch {
		case tt.room < minimums:
			got = atMinimums(tt.items)
		case tt.room < hints:
			got = betweenMinimumAndHint(tt.items, tt.room)
		default:
			got = abovePreferred(tt.items, tt.room)
		}
		for i := range tt.want {
			if math.Abs(got[i]-tt.want[i]) > 0.05 {
				t.Errorf("%s: child %d is %.1f, want %.1f", tt.what, i, got[i], tt.want[i])
			}
		}
	}
}

func TestPlaceFixture(t *testing.T) {
	ws, _ := fixture(t)
	list, err := GUI(ws, request(layoutWindow))
	if err != nil {
		t.Fatal(err)
	}
	if list.Width != 1920 || list.Height != 1080 {
		t.Errorf("a window's scene is the resolution: %d by %d", list.Width, list.Height)
	}
	root := list.Elements[0]
	if root.Part != 0 || root.Box.Width != 200 || root.Box.Height != 100 {
		t.Errorf("the window's box is its size: %+v", root)
	}
	// the column's children in paint order: two buttons 20 high with 4 between, then the rest
	var tops []float64
	for _, element := range list.Elements[1:] {
		if list.Parts[element.Part].Parent == 1 {
			tops = append(tops, element.Box.Y-root.Box.Y)
		}
	}
	if len(tops) < 2 || tops[1]-tops[0] != 24 {
		t.Errorf("the second button sits 20 + 4 below the first: %v", tops)
	}
	// the data model's item: one part, drawn once a row
	item := slices.IndexFunc(list.Parts, func(part Part) bool {
		return part.Key == "v_label" && list.Parts[part.Parent].Key == "vbox"
	})
	rows := 0
	for _, element := range list.Elements {
		if element.Part == item && element.Text == nil {
			rows++
		}
	}
	if item < 0 || rows != 3 {
		t.Errorf("a data model's item is drawn once a row: part %d drawn %d times", item, rows)
	}
}

func TestVisible(t *testing.T) {
	ws, dir := fixture(t)
	list, err := GUI(ws, modRequest(ws, dir, "h", "window = {\n\tname = \"h\"\n\tsize = { 20 20 }\n\tvisible = no\n"+
		"\twidget = { visible = no size = { 10 10 } }\n\twidget = { visible = \"[GetPlayer.IsAlive]\" size = { 10 10 } }\n}\n"))
	if err != nil {
		t.Fatal(err)
	}
	var drawn []int
	for _, element := range list.Elements {
		drawn = append(drawn, element.Part)
	}
	if fmt.Sprint(drawn) != "[0 2]" {
		t.Errorf("the entry is drawn whatever its visible says, visible = no hides, a binding draws: %v", drawn)
	}
}

func TestProperties(t *testing.T) {
	ws, dir := fixture(t)
	req := modRequest(ws, dir, "m", "window = {\n\tname = \"m\"\n\tv_box = {\n\t\tusing = v_sized\n\t\tspacing = 4\n\t}\n}\n")
	rows, err := Properties(ws, req, 1)
	if err != nil {
		t.Fatal(err)
	}
	size := slices.IndexFunc(rows, func(row Property) bool { return row.Key == "size" })
	spacing := slices.IndexFunc(rows, func(row Property) bool { return row.Key == "spacing" })
	if size < 0 || spacing < 0 || rows[size].WrittenBy != "v_sized" || rows[size].Value != "{ 100 20 }" || rows[spacing].WrittenBy != "" {
		t.Errorf("the rows say what each is written as and who wrote it: %+v", rows)
	}
	if _, err := Properties(ws, req, 9); err == nil {
		t.Error("a part the window has not is refused")
	}
}

// partNamed is the index of the part with a name; -1 for none.
func partNamed(list *List, name string) int {
	return slices.IndexFunc(list.Parts, func(part Part) bool { return part.Name == name })
}

// boxOf is the box of the first element a part draws; false for none.
func boxOf(list *List, part int) (Box, bool) {
	i := slices.IndexFunc(list.Elements, func(element Element) bool { return element.Part == part })
	if i < 0 {
		return Box{}, false
	}
	return list.Elements[i].Box, true
}

// childBoxes are the boxes of the parts under a part, in the order the list gives them.
func childBoxes(list *List, parent int) []Box {
	var boxes []Box
	for part := range list.Parts {
		if box, drawn := boxOf(list, part); drawn && list.Parts[part].Parent == parent {
			boxes = append(boxes, box)
		}
	}
	return boxes
}

func TestAnchors(t *testing.T) {
	ws, _ := fixture(t)
	rules, err := GUI(ws, request("v_layout_rules"))
	if err != nil {
		t.Fatal(err)
	}
	container := partNamed(rules, "v_container")
	box, _ := boxOf(rules, container)
	icons := childBoxes(rules, container)
	if len(icons) != 2 || icons[0].X+icons[0].Width != box.X+box.Width {
		t.Errorf("a child anchored right with no widgetanchor ends at the container's right edge: %+v in %+v", icons, box)
	}
	entry, err := GUI(ws, request("v_hidden_entry"))
	if err != nil || len(entry.Elements) == 0 || entry.Elements[0].Box.X+entry.Elements[0].Box.Width != 1920 {
		t.Errorf("a window anchored top right ends at the scene's right edge: %+v, %v", entry.Elements, err)
	}
}

func TestPercentSizes(t *testing.T) {
	ws, _ := fixture(t)
	list, err := GUI(ws, request("v_layout_rules"))
	if err != nil {
		t.Fatal(err)
	}
	row := partNamed(list, "v_row")
	box, _ := boxOf(list, row)
	tabs := childBoxes(list, row)
	if box.Width != 300 || len(tabs) != 3 {
		t.Fatalf("the row takes its parent's room: %+v with %d tabs", box, len(tabs))
	}
	for _, tab := range tabs {
		if tab.Width != 100 {
			t.Errorf("each tab is a third of the row: %+v", tabs)
		}
	}
}

func TestContentSizes(t *testing.T) {
	ws, _ := fixture(t)
	list, err := GUI(ws, request("v_layout_rules"))
	if err != nil {
		t.Fatal(err)
	}
	window, _ := boxOf(list, 0)
	for _, tt := range []struct {
		name string
		want Box
	}{
		{"v_container", Box{X: window.X, Y: window.Y, Width: 20, Height: 30}},
		{"v_background", window},
		{"v_flow", Box{Width: 40, Height: 10}},
		{"v_nothing", Box{}},
	} {
		got, drawn := boxOf(list, partNamed(list, tt.name))
		if !drawn || got.Width != tt.want.Width || got.Height != tt.want.Height ||
			tt.name != "v_flow" && tt.name != "v_nothing" && (got.X != tt.want.X || got.Y != tt.want.Y) {
			t.Errorf("%s: %+v, want %+v", tt.name, got, tt.want)
		}
	}
	backed, _ := boxOf(list, partNamed(list, "v_backed"))
	backing, _ := boxOf(list, partNamed(list, "v_backing"))
	if backed.Width != 20 || backed.Height != 10 || backing != backed {
		t.Errorf("a background fills its parent and takes no place among its siblings: %+v in %+v", backing, backed)
	}
	if unset, _ := boxOf(list, partNamed(list, "v_unset_icon")); unset.Width != 100 {
		t.Errorf("a size written negative is unset, so an expanding child takes the row: %+v", unset)
	}
	multi := partNamed(list, "v_multi")
	for _, element := range list.Elements {
		if element.Part == multi && element.Text != nil && (len(element.Text.Lines) < 2 || element.Box.Width != 60) {
			t.Errorf("a multiline text takes the width it is given and wraps there: %+v", element)
		}
	}
	spanning, _ := boxOf(list, partNamed(list, "v_spanning"))
	if spanning.Width != window.Width || spanning.Height != 10 {
		t.Errorf("a column under a plain widget with a child expanding across it takes the room on that axis: %+v", spanning)
	}
	footer, _ := boxOf(list, partNamed(list, "v_footer"))
	if footer.Width != 10 || footer.Y+footer.Height != window.Y+window.Height || footer.X != window.X+(window.Width-10)/2 {
		t.Errorf("a row with no expanding child keeps its content size and its anchor: %+v in %+v", footer, window)
	}
	either, _ := boxOf(list, partNamed(list, "v_either"))
	yes, _ := boxOf(list, partNamed(list, "v_yes"))
	no, _ := boxOf(list, partNamed(list, "v_no"))
	after, _ := boxOf(list, partNamed(list, "v_after"))
	if yes.Y != no.Y || after.Y != yes.Y+30 || either.Height != 40 {
		t.Errorf("siblings visible on a call and its negation share one place, as big as the bigger: %+v %+v %+v in %+v",
			yes, no, after, either)
	}
}

func TestEntriesAlone(t *testing.T) {
	ws, _ := fixture(t)
	button, err := GUI(ws, request("v_button"))
	if err != nil || len(button.Parts) != 1 || button.Parts[0].Kind != "button" || button.Width != 10 || button.Height != 10 {
		t.Errorf("a type is drawn alone as one widget of its kind, on a scene of its size: %+v, %d by %d, %v",
			button.Parts, button.Width, button.Height, err)
	}
	template, err := GUI(ws, request("v_faded"))
	if err != nil || len(template.Parts) != 1 || template.Parts[0].Key != "widget" ||
		!slices.Contains(template.Missing, "Nothing to draw: v_faded has no size and no texture of its own") {
		t.Errorf("a template is drawn alone as a widget that uses it, and one with no size is listed: %+v, %v, %v",
			template.Parts, template.Missing, err)
	}
	icon, err := GUI(ws, request("v_icon"))
	if err != nil || len(icon.Elements) != 1 || icon.Elements[0].Box.Width != 8 || icon.Elements[0].Box.Height != 8 ||
		icon.Elements[0].Texture == nil || !strings.Contains(icon.Elements[0].Texture.URL, "v_bg.dds") {
		t.Errorf("a text icon is its texture at its size: %+v, %v", icon.Elements, err)
	}
	format, err := GUI(ws, request("v_format"))
	texts := 0
	for _, element := range format.Elements {
		if element.Text != nil && element.Text.Lines[0].Runs[0].Text == "Sample text" {
			texts++
		}
	}
	if err != nil || texts != 1 || format.Width == 0 {
		t.Errorf("a format is a sample text in it, on a scene of the text's size: %+v, %v", format.Elements, err)
	}
}

func TestTextDefault(t *testing.T) {
	ws, _ := fixture(t)
	list, err := GUI(ws, request("v_default_text"))
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
	list, err := GUI(ws, request("v_other_window"))
	if err != nil {
		t.Fatal(err)
	}
	var drawn []string
	for _, element := range list.Elements {
		if element.Text != nil {
			drawn = append(drawn, element.Text.Lines[0].Runs[0].Text)
		}
	}
	if fmt.Sprint(drawn) != "[[GetPlayer.GetName]]" {
		t.Errorf("a text that is a binding is drawn as written: %q", drawn)
	}
}

// layoutCounts are what a render's layout is read by: the elements wholly off the scene and the content-sized holders
// of children with no size. A window drawn with every branch at once runs past the screen, so they are logged, never
// asserted.
func layoutCounts(list *List) (offScene, zeroSized int) {
	holders := map[int]bool{}
	for _, part := range list.Parts {
		holders[part.Parent] = true
	}
	for _, element := range list.Elements {
		box := element.Box
		if box.X+box.Width <= 0 || box.Y+box.Height <= 0 || box.X >= float64(list.Width) || box.Y >= float64(list.Height) {
			offScene++
		}
		if _, sized := game.Sizings[list.Parts[element.Part].Kind]; sized && holders[element.Part] && (box.Width == 0 || box.Height == 0) {
			zeroSized++
		}
	}
	return offScene, zeroSized
}

// TestInstallsResolve is the gate: a named window of each of the three installs resolves with every type and
// template it names found, the text measured with the real fonts, something drawn, and a render timed; what else is
// missing (a file the install lacks) and the layout's counts are logged. It runs only with PMT_VANILLA_GATE=1 and
// the three PMT_TEST_INSTALL_ variables.
func TestInstallsResolve(t *testing.T) {
	if testing.Short() || os.Getenv("PMT_VANILLA_GATE") == "" {
		t.Skip("resolves windows of the three installs; set PMT_VANILLA_GATE=1")
	}
	for _, tt := range []struct{ id, env, path, name string }{
		{ck3, "PMT_TEST_INSTALL_CK3", "gui/window_character.gui", "character_window"},
		{"vic3", "PMT_TEST_INSTALL_VIC3", "gui/politics_panel.gui", "politics_panel"},
		{"eu5", "PMT_TEST_INSTALL_EU5", "gui/eventwindow.gui", "event_window"},
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
		ws, err := workspace.Open(t.Context(), game.Get(tt.id), install, nil, "")
		if err != nil {
			t.Fatal(err)
		}
		file, listed := index.FileAt(ws.Indexes(), tt.path)
		if !listed {
			t.Errorf("%s: %s is not in the install", tt.id, tt.path)
			continue
		}
		started := time.Now()
		list, err := GUI(ws, Request{Path: file.Path, Name: tt.name, Width: 1920, Height: 1080, Scale: 1, Language: game.BaseLanguage})
		if err != nil {
			t.Fatal(err)
		}
		offScene, zeroSized := layoutCounts(list)
		t.Logf("%s %s: %d parts, %d elements (%d off the scene, %d holders with no size), %d fonts, %d missing in %s",
			tt.id, tt.name, len(list.Parts), len(list.Elements), offScene, zeroSized, len(list.Fonts), len(list.Missing),
			time.Since(started).Round(time.Millisecond))
		if len(list.Elements) == 0 {
			t.Errorf("%s %s: nothing is drawn", tt.id, tt.name)
		}
		kinds := map[string]int{}
		for _, missing := range list.Missing {
			kind, _, _ := strings.Cut(missing, ":")
			kinds[kind]++
			if kind == "Type not found" || kind == "Template not found" {
				t.Errorf("%s %s: the install declares no %s", tt.id, tt.name, missing)
			}
		}
		t.Logf("%s missing by kind: %v", tt.id, kinds)
		if len(list.Fonts) == 0 {
			t.Errorf("%s: texts measured with the real fonts", tt.id)
		}
	}
}
