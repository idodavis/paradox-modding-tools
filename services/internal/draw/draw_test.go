// draw_test.go covers the GUI render on the ck3 fixture install: the block reader, the opening state, a window
// resolved through its types, templates and blocks, the layout words, roles and pairs its elements carry, an entry
// drawn alone, and an element's properties.

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
	buttonKind   = "button"
	sizeKey      = "size"
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
	return Request{Path: path, Name: name, Width: 1920, Height: 1080, Language: game.BaseLanguage}
}

// modRequest is a window of a mod file written for the test, drawn at a small resolution.
func modRequest(ws *workspace.Session, dir, name, text string) Request {
	path := filepath.Join(dir, "gui", name+".gui")
	ws.DidChange(path, text)
	return Request{Path: path, Name: name, Width: 100, Height: 100, Language: game.BaseLanguage}
}

// named is the index of the element with a name; -1 for none.
func named(list *List, name string) int {
	return slices.IndexFunc(list.Elements, func(element Element) bool { return element.Name == name })
}

func TestBlockReader(t *testing.T) {
	parsed := jomini.Parse("x = {\n\tsize = { 1 1 }\n\talpha = 1\n\tsize = { 2 50% }\n\tparentanchor = bottom|hcenter\n}\n")
	node := &widget{}
	for _, field := range jomini.Fields(jomini.ChildBlock(parsed.Root.Statements[0])) {
		node.statements = append(node.statements, statement{assign: field, parsed: &parsed})
	}
	if numbers, _ := node.numbers(sizeKey); fmt.Sprint(numbers) != "[2 -0.5]" {
		t.Errorf("the last size wins, a percent is a negative fraction: %v", numbers)
	}
	if choices, _ := node.choices("parentanchor"); fmt.Sprint(choices) != "[bottom hcenter]" {
		t.Errorf("anchor choices: %v", choices)
	}
	if _, has := node.last("max_width"); has {
		t.Error("a key never written is absent")
	}
	if words := layoutWords(node); words["size"] != "2 50%" || words["parentanchor"] != "bottom|hcenter" || len(words) != 3 {
		t.Errorf("the layout words as written, the last of each: %v", words)
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
	elements := resolved.list.Elements
	if root.kind != "window" || elements[0].Key != "window" || elements[0].Name != layoutWindow {
		t.Errorf("the root: %+v", elements[0])
	}
	box := root.children[0]
	if box.kind != "vbox" || len(box.children) != 7 {
		t.Fatalf("v_box resolves to a vbox of seven children: %s, %d", box.kind, len(box.children))
	}
	if size, _ := box.numbers(sizeKey); fmt.Sprint(size) != "[100 20]" {
		t.Errorf("the template's size applies: %v", size)
	}
	if spacing, _ := box.numbers("spacing"); fmt.Sprint(spacing) != "[4]" {
		t.Errorf("the own block's spacing applies: %v", spacing)
	}
	if first := box.children[0]; first.kind != buttonKind || first.statements[len(first.statements)-1].writtenBy != "" {
		t.Errorf("the override's button replaces the block's, written by the widget: %+v", first.kind)
	}
	if label := elements[box.children[2].element]; label.Text == nil || len(label.Text.Runs) != 1 || label.Text.Runs[0].Text != "Fixture text" {
		t.Errorf("the loc key's text as runs: %+v", label.Text)
	}
	if icon := elements[box.children[3].element]; len(icon.Textures) != 2 || icon.Textures[0].Width != 4 ||
		icon.Textures[0].Frame != (Box{X: 2, Width: 2, Height: 2}) ||
		icon.Textures[1].Frame != (Box{Y: 2, Width: 2, Height: 2}) || icon.Textures[1].Mask == nil ||
		icon.Textures[1].Mask.URL != icon.Textures[0].URL {
		t.Errorf("the texture by its path at its upframe, and a modify_texture's frame picked in its block, masked by the texture: %+v",
			icon.Textures)
	}
	if list := box.children[4]; len(list.children) != 1 || elements[list.children[0].element].Rows != 3 {
		t.Errorf("a data model's item is drawn three rows: %+v", list.children)
	}
	if bound := elements[box.children[6].element]; bound.Hidden || bound.Pair != -1 {
		t.Errorf("a visible that is a binding is the game's to answer, so the widget is drawn: %+v", bound)
	}
	if !resolved.missing["Texture not found: v_frame.dds"] {
		t.Errorf("a texture by a path nothing holds is listed: %v", resolved.missing)
	}
	if len(resolved.list.Fonts) != 1 || resolved.list.Fonts[0].Name != "v_font" || resolved.list.Fonts[0].Style != "regular" {
		t.Errorf("one font row: %+v", resolved.list.Fonts)
	}
	for i, element := range elements {
		if element.Parent >= i && i > 0 || element.Parent != -1 && i == 0 {
			t.Errorf("element %d's parent %d is not before it", i, element.Parent)
		}
	}
	if _, err := GUI(ws, request("no_such_window")); err == nil {
		t.Error("a name the file does not hold is refused")
	}
}

func TestLayoutWords(t *testing.T) {
	ws, _ := fixture(t)
	list, err := GUI(ws, request("v_layout_rules"))
	if err != nil {
		t.Fatal(err)
	}
	if list.Width != 1920 || list.Height != 1080 {
		t.Errorf("a window's scene is the resolution: %d by %d", list.Width, list.Height)
	}
	for _, tt := range []struct{ name, key, want string }{
		{"v_row", "layoutpolicy_horizontal", "expanding"},
		{"v_footer", "parentanchor", "bottom|hcenter"},
		{"v_multi", "multiline", "yes"},
		{"v_unset_icon", sizeKey, "-1 10"},
		{"v_scroll", sizeKey, "50 50"},
	} {
		if element := named(list, tt.name); element < 0 || list.Elements[element].Layout[tt.key] != tt.want {
			t.Errorf("%s carries %s as written: %+v", tt.name, tt.key, list.Elements[max(element, 0)].Layout)
		}
	}
	row := named(list, "v_row")
	tabs := slices.IndexFunc(list.Elements, func(element Element) bool { return element.Parent == row })
	if tabs < 0 || list.Elements[tabs].Layout["size"] != "100% 10" || list.Elements[tabs].Kind != buttonKind {
		t.Errorf("a percent is carried as written, through the type: %+v", list.Elements[max(tabs, 0)])
	}
	if background := list.Elements[named(list, "v_background")]; background.Role != game.RoleSkin {
		t.Errorf("a background is the window's skin: %+v", background)
	}
	if bar := list.Elements[named(list, "v_bar")]; bar.Role != game.RoleChrome || bar.Kind != "scrollbar" || bar.Parent != named(list, "v_scroll") {
		t.Errorf("a scroll bar is its area's chrome, a scrollbar: %+v", bar)
	}
	if named(list, "v_tip") >= 0 {
		t.Error("a tooltip is left out of the window")
	}
	yes, no, after := list.Elements[named(list, "v_yes")], list.Elements[named(list, "v_no")], list.Elements[named(list, "v_after")]
	if yes.Pair != -1 || no.Pair != named(list, "v_yes") || after.Pair != -1 {
		t.Errorf("siblings visible on a call and its negation share one place: %d %d %d", yes.Pair, no.Pair, after.Pair)
	}
}

func TestVisible(t *testing.T) {
	ws, dir := fixture(t)
	list, err := GUI(ws, modRequest(ws, dir, "h", "window = {\n\tname = \"h\"\n\tsize = { 20 20 }\n\tvisible = no\n"+
		"\twidget = { visible = no size = { 10 10 } }\n\twidget = { visible = \"[GetPlayer.IsAlive]\" size = { 10 10 } }\n}\n"))
	if err != nil {
		t.Fatal(err)
	}
	var hidden []bool
	for _, element := range list.Elements {
		hidden = append(hidden, element.Hidden)
	}
	if fmt.Sprint(hidden) != "[false true false]" {
		t.Errorf("the entry is drawn whatever its visible says, visible = no hides, a binding draws: %v", hidden)
	}
}

func TestProperties(t *testing.T) {
	ws, dir := fixture(t)
	req := modRequest(ws, dir, "m", "window = {\n\tname = \"m\"\n\tv_box = {\n\t\tusing = v_sized\n\t\tspacing = 4\n\t}\n}\n")
	rows, err := Properties(ws, req, 1)
	if err != nil {
		t.Fatal(err)
	}
	size := slices.IndexFunc(rows, func(row Property) bool { return row.Key == sizeKey })
	spacing := slices.IndexFunc(rows, func(row Property) bool { return row.Key == "spacing" })
	if size < 0 || spacing < 0 || rows[size].WrittenBy != "v_sized" || rows[size].Value != "{ 100 20 }" || rows[spacing].WrittenBy != "" {
		t.Errorf("the rows say what each is written as and who wrote it: %+v", rows)
	}
	if _, err := Properties(ws, req, 9); err == nil {
		t.Error("an element the window has not is refused")
	}
}

func TestEntriesAlone(t *testing.T) {
	ws, _ := fixture(t)
	button, err := GUI(ws, request("v_button"))
	if err != nil || len(button.Elements) != 1 || button.Elements[0].Kind != buttonKind || button.Elements[0].Layout["size"] != "10 10" ||
		button.Width != 0 || button.Height != 0 {
		t.Errorf("a type is drawn alone as one widget of its kind, on a scene of its own size: %+v, %d by %d, %v",
			button.Elements, button.Width, button.Height, err)
	}
	template, err := GUI(ws, request("v_faded"))
	if err != nil || len(template.Elements) != 1 || template.Elements[0].Key != "widget" ||
		!slices.Contains(template.Missing, "Nothing to draw: v_faded has no size, no texture, no text and no children of its own") {
		t.Errorf("a template is drawn alone as a widget that uses it, and one with nothing to show is listed: %+v, %v, %v",
			template.Elements, template.Missing, err)
	}
	icon, err := GUI(ws, request("v_icon"))
	if err != nil || len(icon.Elements) != 1 || icon.Elements[0].Layout["size"] != "8 8" || len(icon.Elements[0].Textures) != 1 ||
		!strings.Contains(icon.Elements[0].Textures[0].URL, "v_bg.dds") {
		t.Errorf("a text icon is its texture at its size: %+v, %v", icon.Elements, err)
	}
	format, err := GUI(ws, request("v_format"))
	if err != nil || len(format.Elements) != 1 || format.Elements[0].Text == nil || format.Elements[0].Text.Runs[0].Text != "Sample text" ||
		!format.Elements[0].Text.Runs[0].Bold {
		t.Errorf("a format is a sample text in it: %+v, %v", format.Elements, err)
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
			drawn = append(drawn, element.Text.Runs[0].Text)
		}
	}
	if fmt.Sprint(drawn) != "[[GetPlayer.GetName]]" {
		t.Errorf("a text that is a binding is drawn as written: %q", drawn)
	}
}

// TestInstallsResolve is the gate: a named window of each of the three installs resolves with every type and
// template it names found, its fonts listed, something drawn, and a render timed; what else is missing (a file the
// install lacks) is logged. It runs only with PMT_VANILLA_GATE=1 and the three PMT_TEST_INSTALL_ variables.
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
		list, err := GUI(ws, Request{Path: file.Path, Name: tt.name, Width: 1920, Height: 1080, Language: game.BaseLanguage})
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("%s %s: %d elements, %d fonts, %d missing in %s",
			tt.id, tt.name, len(list.Elements), len(list.Fonts), len(list.Missing), time.Since(started).Round(time.Millisecond))
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
			t.Errorf("%s: the texts' fonts are listed", tt.id)
		}
	}
}
