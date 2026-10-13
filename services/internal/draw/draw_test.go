// draw_test.go covers the GUI render on the ck3 fixture install: the evaluator's answers and the facts it keeps, the
// block reader, the opening state, a window resolved through its types, templates and blocks, and the golden list.

package draw

import (
	"encoding/json"
	"flag"
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

var update = flag.Bool("update", false, "write the goldens instead of comparing")

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

// request is the fixture window drawn at the board's resolution with no facts.
func request(name string) Request {
	path, _ := filepath.Abs(filepath.Join("..", "testdata", ck3, "game", "gui", "v_window.gui"))
	return Request{Path: path, Name: name, Width: 1920, Height: 1080, Scale: 1, Language: game.BaseLanguage}
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

func TestEvaluate(t *testing.T) {
	ws, _ := fixture(t)
	facts := map[string]string{"Character.GetAge": "20", "Envoy.GetName": "Tlacaelel"}
	evaluator := newEvaluator(ws, facts, game.BaseLanguage)
	for _, tt := range []struct {
		call, context, want string
		row                 int
		answer              answer
		function            string // the Function the call's fact names; "" for no fact
	}{
		{"yes", "", "", -1, answer{kind: game.DataBool, yes: true, known: true}, ""},
		{"'(int32)16'", "", "", -1, answer{kind: game.DataNumber, number: 16, known: true}, ""},
		{"GetVariableSystem.Exists('family')", "", "", -1, answer{kind: game.DataBool, known: true}, "VariableSystem.Exists"},
		{"Not(GetVariableSystem.Exists('family'))", "", "", -1, answer{kind: game.DataBool, yes: true, known: true}, ""},
		{"And(GetPlayer.IsAlive, GetVariableSystem.Exists('x'))", "", "", -1, answer{kind: game.DataBool, known: true}, ""},
		{"Or(GetPlayer.IsAlive, GetPlayer.IsValid)", "", "", -1, answer{kind: game.DataBool, yes: true, known: true}, ""},
		{"GetPlayer.IsAlive", "", "", -1, answer{kind: game.DataBool}, "Character.IsAlive"},
		{"Character.IsValid", "Character", "", -1, answer{kind: game.DataBool, yes: true, known: true}, ""},
		{"GreaterThan_int32(GetPlayer.GetAge, '(int32)16')", "", "", -1, answer{kind: game.DataBool, yes: true, known: true}, ""},
		{"GreaterThan_int32(GetPlayer.GetAge, '(int32)30')", "", "", -1, answer{kind: game.DataBool, known: true}, ""},
		{"GetPlayer.GetName", "", game.DataText, -1, answer{kind: game.DataText, text: "Name"}, "Character.GetName"},
		{"Envoy.GetName", "", "", -1, answer{kind: game.DataText, text: "Tlacaelel", known: true}, ""},
		{"Select_CString(GetPlayer.IsAlive, 'A', 'B')", "", "", -1, answer{kind: game.DataText, text: "A"}, ""},
		{"Localize('v_key')", "", "", -1, answer{kind: game.DataText, text: "Fixture text", known: true}, ""},
		{"PdxGuiWidget.GetIndexInDataModel", "", "", 1, answer{kind: game.DataNumber, number: 1, known: true}, ""},
		{"IsDataModelEmpty(GetPlayer.GetVassals)", "", "", -1, answer{kind: game.DataBool, known: true}, ""},
		{"PdxGuiWidget.IsTooltipLocked", "", "", -1, answer{kind: game.DataBool, known: true}, "PdxGuiWidget.IsTooltipLocked"},
		{"NoSuchType.Char", "", "", -1, answer{kind: game.DataObject}, ""},
	} {
		evaluator.context, evaluator.row = tt.context, tt.row
		got := evaluator.evaluate(tt.call, tt.want)
		if got != tt.answer {
			t.Errorf("%s: %+v, want %+v", tt.call, got, tt.answer)
		}
		if fact, asked := evaluator.asked[fold(tt.call)]; tt.function != "" && (!asked || fact.Function != tt.function) {
			t.Errorf("%s: asked %+v, want the function %s", tt.call, fact, tt.function)
		}
	}
	if !evaluator.missing["data function NoSuchType.Char"] {
		t.Errorf("a step no type declares is missing: %v", evaluator.missing)
	}
}

func TestResolveFixture(t *testing.T) {
	ws, _ := fixture(t)
	resolved, root, err := resolve(ws, request(layoutWindow), nil)
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
	if label := box.children[2]; label.text == nil || label.text.Lines[0].Runs[0].Text != "Fixture text" {
		t.Errorf("the loc key's text: %+v", box.children[2].text)
	}
	if icon := box.children[3]; len(icon.textures) != 1 || icon.textures[0].Width != 4 {
		t.Errorf("the texture by its path: %+v", box.children[3].textures)
	}
	if list := box.children[4]; list.rows != 3 {
		t.Errorf("a data model draws three rows: %d", list.rows)
	}
	if variable := box.children[5]; variable.visibility != Hidden || variable.fact != "GetVariableSystem.Exists('v_flag')" {
		t.Errorf("a variable is unset as the window opens: %+v", variable.visibility)
	}
	if alive := box.children[6]; alive.visibility != Unknown {
		t.Errorf("game data is unknown: %+v", alive.visibility)
	}
	if !slices.Contains(resolved.list.Missing, "texture v_frame.dds") {
		t.Errorf("a texture by a path nothing holds is listed: %v", resolved.list.Missing)
	}
	if len(resolved.list.Fonts) != 1 || resolved.list.Fonts[0].Name != "v_font" || resolved.list.Fonts[0].Style != "regular" {
		t.Errorf("one font row: %+v", resolved.list.Fonts)
	}
	for i, part := range resolved.list.Parts {
		if part.Parent >= i && i > 0 || part.Parent != -1 && i == 0 {
			t.Errorf("part %d's parent %d is not before it", i, part.Parent)
		}
	}
	button, err := GUI(ws, request("v_button"), nil)
	if err != nil || len(button.Parts) != 1 || button.Parts[0].Kind != "button" {
		t.Errorf("a type is drawn alone as one widget of its kind: %+v, %v", button.Parts, err)
	}
	template, err := GUI(ws, request("v_faded"), nil)
	if err != nil || len(template.Parts) != 1 || template.Parts[0].Key != "widget" {
		t.Errorf("a template is drawn alone as a widget that uses it: %+v, %v", template.Parts, err)
	}
	if _, err := GUI(ws, request("no_such_window"), nil); err == nil {
		t.Error("a name the file does not hold is refused")
	}
}

// portable is a list's text with the fixtures' folder and the system's path separator taken out, as the goldens are.
func portable(text string) string {
	root, _ := filepath.Abs(filepath.Join("..", "testdata"))
	text = strings.ReplaceAll(text, strings.ReplaceAll(root, `\`, `\\`), "<fixtures>")
	text = strings.ReplaceAll(text, root, "<fixtures>")
	return strings.ReplaceAll(text, `\\`, "/")
}

// firstDifference is the first line where two texts differ, with both versions.
func firstDifference(want, got string) string {
	wantLines, gotLines := strings.Split(want, "\n"), strings.Split(got, "\n")
	for i := range max(len(wantLines), len(gotLines)) {
		wantLine, gotLine := "", ""
		if i < len(wantLines) {
			wantLine = wantLines[i]
		}
		if i < len(gotLines) {
			gotLine = gotLines[i]
		}
		if wantLine != gotLine {
			return fmt.Sprintf("line %d\nwant: %s\ngot:  %s", i+1, wantLine, gotLine)
		}
	}
	return ""
}

func TestGoldenList(t *testing.T) {
	ws, _ := fixture(t)
	golden := filepath.Join("..", "testdata", "golden", "draw-ck3-vanilla-gui__v_window.json")
	var lists []*List
	for _, name := range []string{"v_window", "v_other_window", layoutWindow} {
		list, err := GUI(ws, request(name), nil)
		if err != nil {
			t.Fatal(err)
		}
		lists = append(lists, list)
	}
	encoded, err := json.MarshalIndent(lists, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := portable(string(encoded)) + "\n"
	if *update {
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != got {
		t.Errorf("the draw golden differs:\n%s", firstDifference(string(want), got))
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
	list, err := GUI(ws, request(layoutWindow), nil)
	if err != nil {
		t.Fatal(err)
	}
	if list.Width != 1920 || list.Height != 1080 {
		t.Errorf("the scene is the resolution: %d by %d", list.Width, list.Height)
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
	hidden := slices.IndexFunc(list.Parts, func(part Part) bool { return part.Visibility == Hidden })
	if hidden < 0 || slices.ContainsFunc(list.Elements, func(element Element) bool { return element.Part == hidden }) {
		t.Errorf("a hidden part has no element: part %d", hidden)
	}
	hideUnknown := request(layoutWindow)
	hideUnknown.HideUnknown = true
	fewer, err := GUI(ws, hideUnknown, nil)
	if err != nil || len(fewer.Elements) >= len(list.Elements) {
		t.Errorf("hide unknown drops the unknown part's elements: %d of %d, %v", len(fewer.Elements), len(list.Elements), err)
	}
}

func TestPropertiesAndEdit(t *testing.T) {
	ws, dir := fixture(t)
	path := filepath.Join(dir, "gui", "m_window.gui")
	ws.DidChange(path, "window = {\n\tname = \"m\"\n\tv_box = {\n\t\tusing = v_sized\n\t\tspacing = 4\n\t}\n}\n")
	req := Request{Path: path, Name: "m", Width: 100, Height: 100, Scale: 1, Language: game.BaseLanguage}
	rows, err := Properties(ws, req, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	size := slices.IndexFunc(rows, func(row Property) bool { return row.Key == "size" })
	spacing := slices.IndexFunc(rows, func(row Property) bool { return row.Key == "spacing" })
	if size < 0 || spacing < 0 || rows[size].WrittenBy != "v_sized" || rows[size].Kind != "number" || rows[spacing].WrittenBy != "" {
		t.Errorf("the rows say who wrote each: %+v", rows)
	}
	edits, err := Edit(ws, req, nil, Change{Part: 1, Key: "spacing", Value: "8", Before: -1})
	if err != nil || len(edits) != 1 || edits[0].NewText != "8" || edits[0].Range.Start.Line != 4 {
		t.Errorf("an own key's value is replaced in place: %+v, %v", edits, err)
	}
	edits, err = Edit(ws, req, nil, Change{Part: 1, Key: "width", Value: "{ 50 50 }", Before: -1})
	if err != nil || len(edits) != 1 || edits[0].NewText != "\n\t\twidth = { 50 50 }" || edits[0].Range.Start.Line != 4 {
		t.Errorf("a key the block does not write is added after its last statement: %+v, %v", edits, err)
	}
	edits, err = Edit(ws, req, nil, Change{Part: 1, Remove: true, Before: -1})
	if err != nil || len(edits) != 1 || edits[0].NewText != "" || edits[0].Range.Start.Line != 1 {
		t.Errorf("a removed part takes its line with it: %+v, %v", edits, err)
	}
	if _, err := Edit(ws, request("v_window"), nil, Change{Part: 1, Key: "x", Before: -1}); err == nil {
		t.Error("the game's files are read only")
	}
}

func TestPruneFacts(t *testing.T) {
	ws, _ := fixture(t)
	remaining, dropped := PruneFacts(ws, map[string]string{"Character.GetName": "x", "Character.GetOldName": "y"})
	if len(remaining) != 1 || fmt.Sprint(dropped) != "[Character.GetOldName]" {
		t.Errorf("a fact no function backs is dropped: %v %v", remaining, dropped)
	}
}

// TestInstallsResolve is the gate: a named window of each of the three installs resolves with every type and
// template it names found, the text measured with the real fonts, and a render timed; what else is missing (a file
// the install lacks, a fact nobody answered) is logged by kind. It runs only with
// PMT_VANILLA_GATE=1 and the three PMT_TEST_INSTALL_ variables.
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
		list, err := GUI(ws, Request{Path: file.Path, Name: tt.name, Width: 1920, Height: 1080, Scale: 1, Language: game.BaseLanguage}, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("%s %s: %d parts, %d elements, %d facts, %d fonts, %d missing in %s", tt.id, tt.name, len(list.Parts),
			len(list.Elements), len(list.Facts), len(list.Fonts), len(list.Missing), time.Since(started).Round(time.Millisecond))
		kinds := map[string]int{}
		for _, missing := range list.Missing {
			kind, _, _ := strings.Cut(missing, " ")
			kinds[kind]++
			if kind == "type" || kind == "template" {
				t.Errorf("%s %s: the install declares no %s", tt.id, tt.name, missing)
			}
		}
		t.Logf("%s missing by kind: %v", tt.id, kinds)
		if len(list.Fonts) == 0 {
			t.Errorf("%s: texts measured with the real fonts", tt.id)
		}
	}
}
