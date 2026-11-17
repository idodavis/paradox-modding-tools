package jomini

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// outline writes one numbered line per statement of the tree after the file,
// indented by nesting, with a block as `{}`.
func outline(tree *Tree) []string {
	var lines []string
	for i, statement := range tree.Statements[1:] {
		depth := 0
		for parent := statement.Parent; parent > 0; parent = tree.Statements[parent].Parent {
			depth++
		}
		parts := []string{tree.Text(statement.Key), tree.Text(statement.Op), tree.Text(statement.Tag), tree.Text(statement.Value)}
		if statement.IsBlock() {
			parts = append(parts, "{}")
		}
		parts = slices.DeleteFunc(parts, func(part string) bool { return part == "" })
		lines = append(lines, fmt.Sprintf("%d:%s%s", i, strings.Repeat("  ", depth), strings.Join(parts, " ")))
	}
	return lines
}

func TestParseOperators(t *testing.T) {
	tests := []struct {
		text string
		want []string
	}{
		{"a = 1", []string{"0:a = 1"}},
		{"a == 1", []string{"0:a == 1"}},
		{"a != 1", []string{"0:a != 1"}},
		{"a < 1", []string{"0:a < 1"}},
		{"a > 1", []string{"0:a > 1"}},
		{"a <= 1", []string{"0:a <= 1"}},
		{"a >= 1", []string{"0:a >= 1"}},
		{"a ?= { b = 1 }", []string{"0:a ?= {}", "1:  b = 1"}},
		{"a=b", []string{"0:a = b"}},
		{"a=\"b\"", []string{`0:a = "b"`}},
		{"a==b c!=d e?=f g<=h i>=j", []string{"0:a == b", "1:c != d", "2:e ?= f", "3:g <= h", "4:i >= j"}},
		{"text = [Root.GetName]", []string{"0:text = [Root.GetName]"}},
		{"value = @[a < b]", []string{"0:value = @[a < b]"}},
		{"a>b", []string{"0:a>b"}},
		{"= 1", []string{"0:1"}},
		{"\xEF\xBB\xBFnamespace = v", []string{"0:namespace = v"}},
	}
	for _, test := range tests {
		if got := outline(Parse(test.text)); !slices.Equal(got, test.want) {
			t.Errorf("Parse(%q) = %q, want %q", test.text, got, test.want)
		}
	}
}

func TestParseTaggedBlock(t *testing.T) {
	tests := []struct {
		text string
		want []string
	}{
		{"color = rgb { 255 0 0 }", []string{"0:color = rgb {}", "1:  255", "2:  0", "3:  0"}},
		{"color = hsv360 { 0 1 1 }", []string{"0:color = hsv360 {}", "1:  0", "2:  1", "3:  1"}},
		{"template v_plain { alpha = 1 }", []string{"0:template", "1:v_plain {}", "2:  alpha = 1"}},
		{"type v_button = button { size = { 10 10 } }", []string{"0:type", "1:v_button = button {}", "2:  size = {}", "3:    10", "4:    10"}},
		{"block \"rows\" { v_button = { } }", []string{"0:block", `1:"rows" {}`, "2:  v_button = {}"}},
		{"{ a b }", []string{"0:{}", "1:  a", "2:  b"}},
	}
	for _, test := range tests {
		if got := outline(Parse(test.text)); !slices.Equal(got, test.want) {
			t.Errorf("Parse(%q) = %q, want %q", test.text, got, test.want)
		}
	}
}

func TestParseList(t *testing.T) {
	tree := Parse(`pattern = list "faction_patterns_list"` + "\n" + `color1 = list`)
	want := []string{`0:pattern = list "faction_patterns_list"`, "1:color1 = list"}
	if got := outline(tree); !slices.Equal(got, want) {
		t.Fatalf("outline = %q, want %q", got, want)
	}
	if tree.Statements[1].IsBlock() || tree.Text(tree.Statements[1].Tag) != "list" || tree.Unquoted(tree.Statements[1].Value) != "faction_patterns_list" {
		t.Errorf("list statement = %+v", tree.Statements[1])
	}
}

func TestParseProblems(t *testing.T) {
	tests := []struct {
		text  string
		want  []Problem
		shape []string
	}{
		{"a = { b = 1", []Problem{{Code: "unclosed-block", Message: "{ is not closed; its block runs to the end of the text", Span: Span{Start: 4, End: 5}}}, []string{"0:a = {}", "1:  b = 1"}},
		{"a = 1 }\nb = 2", []Problem{{Code: "unexpected-brace", Message: "} has no { to close; it is skipped", Span: Span{Start: 6, End: 7}}}, []string{"0:a = 1", "1:b = 2"}},
		{"a = \"one\ntwo\"\nb = 2", nil, []string{"0:a = \"one\ntwo\"", "1:b = 2"}},
		{"a = \"open\nb = 2", []Problem{{Code: "unclosed-string", Message: "string is not closed; it runs to the end of the text", Span: Span{Start: 4, End: 15}}}, []string{"0:a = \"open\nb = 2"}},
		{"a = \"", []Problem{{Code: "unclosed-string", Message: "string is not closed; it runs to the end of the text", Span: Span{Start: 4, End: 5}}}, []string{`0:a = "`}},
	}
	for _, test := range tests {
		tree := Parse(test.text)
		if !slices.Equal(tree.Problems, test.want) {
			t.Errorf("Parse(%q).Problems = %+v, want %+v", test.text, tree.Problems, test.want)
		}
		if got := outline(tree); !slices.Equal(got, test.shape) {
			t.Errorf("Parse(%q) = %q, want %q", test.text, got, test.shape)
		}
	}
	tree := Parse("a = {\n\tb = { c = 1 }\n")
	if got := tree.Statements[1].Span; got != (Span{Start: 0, End: len(tree.Src)}) {
		t.Errorf("unclosed block's statement span = %+v, want to the text's end", got)
	}
}

func TestParseNested(t *testing.T) {
	tree := Parse("a = { b = { c = 1 d = 2 } e = 3 }\nf = 4")
	wantLast := []int32{7, 6, 5, 4, 5, 6, 7}
	wantParent := []int32{0, 0, 1, 2, 2, 1, 0}
	for i, statement := range tree.Statements {
		if statement.Last != wantLast[i] || statement.Parent != wantParent[i] {
			t.Errorf("statement %d: Last %d Parent %d, want %d %d", i, statement.Last, statement.Parent, wantLast[i], wantParent[i])
		}
	}
	if got := slices.Collect(tree.Children(0)); !slices.Equal(got, []int32{1, 6}) {
		t.Errorf("top children = %v", got)
	}
	if got := slices.Collect(tree.Children(1)); !slices.Equal(got, []int32{2, 5}) {
		t.Errorf("children of a = %v", got)
	}
	if got := slices.Collect(tree.Children(3)); len(got) != 0 {
		t.Errorf("children of c = %v", got)
	}
	if got := tree.Text(tree.Statements[2].Span); got != "b = { c = 1 d = 2 }" {
		t.Errorf("span of b = %q", got)
	}
}

func TestFindStatement(t *testing.T) {
	text := "a = { b = 1 }  c = 2"
	tree := Parse(text)
	tests := []struct {
		offset int
		want   int32
	}{
		{0, 1},
		{6, 2},
		{10, 2},
		{11, 1},
		{12, 1},
		{13, 0},
		{15, 3},
		{len(text), 0},
		{-1, 0},
	}
	for _, test := range tests {
		if got := tree.FindStatement(test.offset); got != test.want {
			t.Errorf("FindStatement(%d) = %d, want %d", test.offset, got, test.want)
		}
	}
}

func TestFindChild(t *testing.T) {
	tree := Parse("a = { b = 1 b = 2 c = 3 } b = 4")
	if got, ok := tree.FindChild(1, "b"); !ok || got != 3 {
		t.Errorf("FindChild(a, b) = %d, %v, want 3, true", got, ok)
	}
	if _, ok := tree.FindChild(1, "B"); ok {
		t.Error("FindChild(a, B) found a child")
	}
	if got, ok := tree.FindChild(0, "b"); !ok || got != 5 {
		t.Errorf("FindChild(file, b) = %d, %v, want 5, true", got, ok)
	}
}

func TestNewLines(t *testing.T) {
	lines := NewLines("ab\ncd\r\nef\rgh")
	if got := lines.CountLines(); got != 4 {
		t.Fatalf("CountLines = %d, want 4", got)
	}
	tests := []struct {
		offset   int
		position Position
	}{
		{0, Position{Line: 0, Column: 0}},
		{2, Position{Line: 0, Column: 2}},
		{3, Position{Line: 1, Column: 0}},
		{5, Position{Line: 1, Column: 2}},
		{7, Position{Line: 2, Column: 0}},
		{10, Position{Line: 3, Column: 0}},
		{11, Position{Line: 3, Column: 1}},
	}
	for _, test := range tests {
		if got := lines.ToPosition(test.offset); got != test.position {
			t.Errorf("ToPosition(%d) = %+v, want %+v", test.offset, got, test.position)
		}
	}
	for line, want := range []Span{{Start: 0, End: 2}, {Start: 3, End: 5}, {Start: 7, End: 9}, {Start: 10, End: 12}} {
		if got := lines.LineSpan(line); got != want {
			t.Errorf("LineSpan(%d) = %+v, want %+v", line, got, want)
		}
	}
	if got := lines.ToPosition(99); got != (Position{Line: 3, Column: 2}) {
		t.Errorf("ToPosition past the end = %+v", got)
	}
	if got := NewLines("").CountLines(); got != 1 {
		t.Errorf("CountLines of an empty text = %d", got)
	}
	if got := NewLines("a\n").CountLines(); got != 2 {
		t.Errorf("CountLines of a text ending in a line ending = %d", got)
	}
}

func TestClassify(t *testing.T) {
	tests := map[Class][]string{
		ClassNone:       {""},
		ClassIdentifier: {"a", "is_adult", "k_x", "x0974E5", "d'Este", "été", "-x", "-", "0x", "1e3", "abc", "v", "x"},
		ClassNumber:     {"0", "5", "-1", "+1", "1.5", "-1.5", "-0.25", "100%", "-5%", "0xFF", "0xff0000", "1066.9", "867.1"},
		ClassDate:       {"1066.9.15", "1.2.3", "867.1.1"},
		ClassEventID:    {"v.1", "my_ns.1001", "ns2.0"},
		ClassChain:      {"scope:target", "c:FRA", ".5", "5.", "v.", ".1", "v.1.t", "scope:x.1", "v.a", "v-x.1"},
		ClassParameter:  {"$X$", "$AMOUNT$", "$TARGET|root$", "$x$", "$X|$", "$X$Y$"},
		ClassConstant:   {"@x", "@my_value"},
		ClassExpression: {"@[x * 2]", "@[a < b]"},
		ClassCall:       {"[x]"},
		ClassText:       {`"x"`, "a b", "a{", "5%%", "$", "$$", "$X", "X$", "$a b$", "$|x$", "@", "@5", "@[x", `@"x"`},
	}
	for want, texts := range tests {
		for _, text := range texts {
			if got := Classify(text); got != want {
				t.Errorf("Classify(%q) = %d, want %d", text, got, want)
			}
		}
	}
}

func TestParseDate(t *testing.T) {
	tests := []struct {
		text      string
		want      Date
		ok, valid bool
	}{
		{"1066.9.15", Date{Year: 1066, Month: 9, Day: 15}, true, true},
		{"867.1.1", Date{Year: 867, Month: 1, Day: 1}, true, true},
		{"1066.9", Date{Year: 1066, Month: 9}, true, true},
		{"1066", Date{}, false, false},
		{"1066.13.1", Date{Year: 1066, Month: 13, Day: 1}, true, false},
		{"1066.0.1", Date{Year: 1066, Day: 1}, true, false},
		{"1066.9.32", Date{Year: 1066, Month: 9, Day: 32}, true, false},
		{"1066.9.15.1", Date{}, false, false},
		{"year.1.1", Date{}, false, false},
		{"", Date{}, false, false},
	}
	for _, test := range tests {
		if got, ok := ParseDate(test.text); got != test.want || ok != test.ok || ok && got.IsValid() != test.valid {
			t.Errorf("ParseDate(%q) = %+v %v, want %+v %v, valid %v", test.text, got, ok, test.want, test.ok, test.valid)
		}
	}
}

func TestLinks(t *testing.T) {
	chains := []struct {
		text string
		want []string
	}{
		{"scope:x.var:y", []string{"scope:x", "var:y"}},
		{"scope:x.var:y.z", []string{"scope:x", "var:y", "z"}},
		{"root", []string{"root"}},
		{"@[a.b].c", []string{"@[a.b]", "c"}},
		{`"a.b".c`, []string{`"a.b"`, "c"}},
		{"", []string{""}},
	}
	for _, test := range chains {
		var got []string
		for _, link := range Links(test.text, 10) {
			got = append(got, test.text[link.Span.Start-10:link.Span.End-10])
		}
		if !slices.Equal(got, test.want) {
			t.Errorf("Links(%q) = %q, want %q", test.text, got, test.want)
		}
	}
	prefixed := []struct {
		text         string
		prefix, name string
	}{
		{"var:x", "var", "x"},
		{"scope:y:z", "scope", "y:z"},
		{"plain", "", ""},
		{":x", "", ""},
		{"scope:", "", ""},
	}
	for _, test := range prefixed {
		link := Links(test.text, 0)[0]
		prefix, name := test.text[link.Prefix.Start:link.Prefix.End], test.text[link.Name.Start:link.Name.End]
		if prefix != test.prefix || name != test.name {
			t.Errorf("Links(%q) splits into %q and %q, want %q and %q", test.text, prefix, name, test.prefix, test.name)
		}
	}
}

func TestColorParts(t *testing.T) {
	tests := []struct {
		text  string
		tag   string
		parts []string
		ok    bool
	}{
		{"color = rgb { 255 0 0 }", "rgb", []string{"255", "0", "0"}, true},
		{"color = hsv { 0.66 1 1 }", "hsv", []string{"0.66", "1", "1"}, true},
		{"color = hsv360 { 240 100 100 }", "hsv360", []string{"240", "100", "100"}, true},
		{"color = hex { 0xffff0055 }", "hex", []string{"0xffff0055"}, true},
		{"color = { 1 0 0 }", "", []string{"1", "0", "0"}, true},
		{"color = { 200 30 30 128 }", "", []string{"200", "30", "30", "128"}, true},
		{"color = { 1 0 }", "", nil, false},
		{"color = { 1 0 0 0 0 }", "", nil, false},
		{"color = { r = 1 g = 0 b = 0 }", "", nil, false},
		{"color = rgb { 255 x 0 }", "", nil, false},
		{"color = { NaN 1 0 }", "", nil, false},
		{"color = v_red", "", nil, false},
		{"size = { 10 10 }", "", nil, false},
		{"color = cmyk { 1 0 0 }", "cmyk", []string{"1", "0", "0"}, true},
	}
	for _, test := range tests {
		tag, parts, ok := ColorParts(Parse(test.text), 1)
		if tag != test.tag || !slices.Equal(parts, test.parts) || ok != test.ok {
			t.Errorf("ColorParts(%q) = %q %v %v, want %q %v %v", test.text, tag, parts, ok, test.tag, test.parts, test.ok)
		}
	}
}

func TestParseCall(t *testing.T) {
	call, ok := ParseCall("Root.GetCharacter.GetName", 10)
	if !ok || len(call.Steps) != 3 || call.Steps[2].Text != "GetName" || call.Steps[2].Span != (Span{Start: 28, End: 35}) || call.Span != (Span{Start: 10, End: 35}) {
		t.Errorf("plain call = %+v %v", call, ok)
	}
	call, ok = ParseCall("Concat( 'a', GetPlayer.GetName, 5 )", 0)
	if !ok || len(call.Steps) != 1 || len(call.Steps[0].Arguments) != 3 {
		t.Fatalf("call with arguments = %+v %v", call, ok)
	}
	arguments := call.Steps[0].Arguments
	if arguments[0] != (Argument{Text: "a", Span: Span{Start: 9, End: 10}, Quoted: true}) {
		t.Errorf("quoted argument = %+v", arguments[0])
	}
	if arguments[1].Call == nil || len(arguments[1].Call.Steps) != 2 || arguments[1].Call.Steps[1].Span != (Span{Start: 23, End: 30}) || arguments[1].Text != "GetPlayer.GetName" {
		t.Errorf("nested call argument = %+v", arguments[1])
	}
	if arguments[2] != (Argument{Text: "5", Span: Span{Start: 32, End: 33}}) {
		t.Errorf("number argument = %+v", arguments[2])
	}
	if call.Span != (Span{Start: 0, End: 35}) {
		t.Errorf("call span = %+v", call.Span)
	}
	call, ok = ParseCall("(int_bool)GetPlayer.IsAlive", 0)
	if !ok || call.Steps[0].Cast != "int_bool" || call.Steps[0].Text != "GetPlayer" || call.Steps[0].Span != (Span{Start: 10, End: 19}) {
		t.Errorf("cast call = %+v %v", call, ok)
	}
	call, ok = ParseCall("Character.GetName|U", 0)
	if !ok || len(call.Steps) != 2 || call.Suffix != "U" || call.Span != (Span{Start: 0, End: 19}) {
		t.Errorf("suffixed call = %+v %v", call, ok)
	}
	call, ok = ParseCall("Not( GetPlayer.IsAlive )", 0)
	if !ok || len(call.Steps[0].Arguments) != 1 || call.Steps[0].Arguments[0].Call == nil || len(call.Steps[0].Arguments[0].Call.Steps) != 2 {
		t.Errorf("spaced arguments = %+v %v", call, ok)
	}
	for _, text := range []string{"", "   ", "|U", "'x'"} {
		if _, ok := ParseCall(text, 0); ok {
			t.Errorf("ParseCall(%q) = true", text)
		}
	}
}

func TestApplyEdits(t *testing.T) {
	edits := []Edit{
		{Span: Span{Start: 6, End: 11}, Text: "there"},
		{Span: Span{Start: 0, End: 0}, Text: "oh, "},
		{Span: Span{Start: 5, End: 5}, Text: "!"},
		{Span: Span{Start: 5, End: 6}, Text: " "},
		{Span: Span{Start: 11, End: 11}, Text: "?"},
	}
	if got := ApplyEdits("hello world", edits); got != "oh, hello! there?" {
		t.Errorf("ApplyEdits = %q", got)
	}
	defer func() {
		if recover() == nil {
			t.Errorf("overlapping edits did not panic")
		}
	}()
	ApplyEdits("hello world", []Edit{{Span: Span{Start: 0, End: 3}}, {Span: Span{Start: 2, End: 5}}})
}

func TestIndentation(t *testing.T) {
	tests := []struct {
		text string
		want string
	}{
		{"a = {\n\tb = 1\n}\n", "\t"},
		{"a = {\n    b = 1\n}\n", "    "},
		{"a = {\n  \n   b = 1\n}", "   "},
		{"a = 1\nb = 2\n", ""},
		{"", ""},
	}
	for _, test := range tests {
		if got := Parse(test.text).Indentation(); got != test.want {
			t.Errorf("Indentation of %q = %q, want %q", test.text, got, test.want)
		}
	}
}

func TestFormat(t *testing.T) {
	stray, fixed := "x = {\r\r\n\ta = 1\r\n}\r\n", "x = {\r\n\ta = 1\r\n}\r\n"
	if got := ApplyEdits(stray, Parse(stray).Format("\t")); got != fixed {
		t.Errorf("a stray carriage return: %q, want %q", got, fixed)
	}
}

// parseFiles parses every script, GUI and asset file under the root and
// returns the paths whose parse holds a problem, each with its first problem,
// and how many files it read.
func parseFiles(t *testing.T, root string) (failures []string, count int) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !slices.Contains([]string{".txt", ".gui", ".gfx", ".asset"}, filepath.Ext(path)) {
			return err
		}
		text, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		count++
		if tree := Parse(string(text)); len(tree.Problems) > 0 {
			position := tree.Lines.ToPosition(tree.Problems[0].Span.Start)
			failures = append(failures, fmt.Sprintf("%s:%d:%d: %s", path, position.Line+1, position.Column+1, tree.Problems[0].Code))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return failures, count
}

func TestParseFixtures(t *testing.T) {
	for _, fixture := range []string{"ck3", "vic3", "eu5", "mods"} {
		failures, count := parseFiles(t, filepath.Join("..", "testdata", fixture))
		if count == 0 {
			t.Errorf("%s: no fixture read", fixture)
		}
		for _, failure := range failures {
			t.Error(failure)
		}
	}
}

func TestParseInstall(t *testing.T) {
	for game, variable := range map[string]string{"ck3": "PMT_TEST_INSTALL_CK3", "vic3": "PMT_TEST_INSTALL_VIC3", "eu5": "PMT_TEST_INSTALL_EU5"} {
		t.Run(game, func(t *testing.T) {
			root := os.Getenv(variable)
			if root == "" {
				t.Skipf("%s is not set", variable)
			}
			failures, count := parseFiles(t, root)
			for _, failure := range failures {
				t.Log(failure)
			}
			t.Logf("%s: %d of %d files hold a parse error", game, len(failures), count)
		})
	}
}

func TestColorOf(t *testing.T) {
	tree := Parse("a = rgb { 255 0 0 }\nb = rgb { 1 0.5 0 }\nc = hsv { 0.5 1 1 }\nd = hsv360 { 180 100 100 }\n" +
		"e = hex { 0xff8000ff }\nf = rgb { 255 0 }\ng = { 1 2 3 }\nh = foo { 1 2 3 }\n")
	wants := []string{
		"rgba(255, 0, 0, 1)", "rgba(255, 128, 0, 1)", "rgba(0, 255, 255, 1)", "rgba(0, 255, 255, 1)", "rgba(255, 128, 0, 1)", "",
		"rgba(1, 2, 3, 1)", "",
	}
	i := 0
	for statement := range tree.Children(0) {
		want := wants[i]
		i++
		key := tree.Text(tree.Statements[statement].Key)
		tag, parts, isColor := ColorParts(tree, statement)
		color, known := ParseColor(tag, parts)
		if got := color.ToCSS(); !isColor || !known {
			got = ""
		} else if got != want {
			t.Errorf("%s: %q, want %q", key, got, want)
		} else {
			continue
		}
		if want != "" || isColor && known {
			t.Errorf("%s: no color, want %q", key, want)
		}
	}
}
