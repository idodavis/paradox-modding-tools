// jomini_test.go covers the parser/lexer: assignments, GUI templates, error
// recovery, tagged blocks, byte-offset decoding, the line index and the walk;
// and the name shapes, literals, cites and interpolations of the shared grammar.

package jomini

import (
	"reflect"
	"regexp"
	"testing"
)

var (
	defNameRe = regexp.MustCompile(`^[\p{L}\p{Nd}_][\p{L}\p{Nd}_.-]*$`)
	eventIDRe = regexp.MustCompile(`^[\p{L}\p{Nd}_-]*[\p{L}_][\p{L}\p{Nd}_-]*\.\d+$`)
)

// The words the fixtures repeat.
const (
	fixtureKey   = "foo"     // an assignment's key
	fixtureTrait = "brave"   // a plain name that is no literal and no cite
	fixtureDate  = "867.1.1" // a date
	scopePrefix  = "scope"   // the prefix of a scope cite
)

// topKeys returns the keys of a source's top-level Assignment statements.
func topKeys(src string) []string {
	result := Parse(src)
	var keys []string
	for _, statement := range result.Root.Statements {
		if assignment, ok := statement.(*Assignment); ok {
			keys = append(keys, assignment.Key.Text)
		}
	}
	return keys
}

// TestParseTree pins the statements a parse builds: plain and quoted keys, a GUI template, a tagged block.
func TestParseTree(t *testing.T) {
	t.Run("basic_assignments", func(t *testing.T) {
		src := "# header\nfoo = {\n\tid = 1\n}\nbar = 2\n"
		got := topKeys(src)
		if len(got) != 2 || got[0] != fixtureKey || got[1] != "bar" {
			t.Fatalf("top keys = %v", got)
		}
		quoted := topKeys("\"my key\" = 1\n")
		if len(quoted) != 1 || quoted[0] != "my key" {
			t.Fatalf("quoted = %+v", quoted)
		}
	})

	t.Run("gui_template_is_one_assignment", func(t *testing.T) {
		// `template` is a bare ValueStmt (skipped by topKeys); `Foo { ... }` is one Op=="" assignment.
		src := "template Foo {\n\tsize = { 10 20 }\n}\n"
		result := Parse(src)
		if got := topKeys(src); len(got) != 1 || got[0] != "Foo" {
			t.Fatalf("top keys = %v", got)
		}
		var foo *Assignment
		for _, statement := range result.Root.Statements {
			if assignment, ok := statement.(*Assignment); ok && assignment.Key.Text == "Foo" {
				foo = assignment
			}
		}
		if foo == nil || foo.Op != "" {
			t.Fatalf("Foo op = %+v", foo)
		}
		if _, ok := foo.Value.(*Block); !ok {
			t.Fatalf("Foo value is not a block: %T", foo.Value)
		}
	})

	t.Run("tagged_block", func(t *testing.T) {
		result := Parse("color = rgb { 255 0 0 }\n")
		assignment, _ := result.Root.Statements[0].(*Assignment)
		tagged, ok := assignment.Value.(*TaggedBlock)
		if !ok || tagged.Tag.Text != "rgb" {
			t.Fatalf("tagged = %T %+v", assignment.Value, assignment)
		}
	})
}

// TestParseComments pins that a comment is recorded with its range and no statement.
func TestParseComments(t *testing.T) {
	result := Parse("# header\nfoo = {\n\tid = 1\n}\nbar = 2\n")
	if len(result.Comments) != 1 || result.Comments[0].Range.Start != 0 {
		t.Fatalf("comments = %+v", result.Comments)
	}
}

// TestParseErrors pins each structural error and that a missing value does not lose the next statement.
func TestParseErrors(t *testing.T) {
	tests := []struct {
		name, src  string
		code       ErrorCode
		recoverBar bool
	}{
		{"unclosed brace", "foo = {\n\tid = 1\n", ErrUnclosedBrace, false},
		{"unterminated string", "name = \"hello\n", ErrUnterminatedString, false},
		{"missing value recovers", "foo =\nbar = 1\n", ErrMissingValue, true},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			result := Parse(testCase.src)
			found := false
			for _, issue := range result.Errors {
				if issue.Code == testCase.code {
					found = true
				}
			}
			if !found {
				t.Fatalf("want %s, got %+v", testCase.code, result.Errors)
			}
			if testCase.recoverBar {
				if keys := topKeys(testCase.src); len(keys) != 2 || keys[1] != "bar" {
					t.Fatalf("recovery failed, keys = %v", keys)
				}
			}
		})
	}
}

// TestLineIndex pins offset and position math, including UTF-8 byte columns.
func TestConstantAt(t *testing.T) {
	last := Parse("@x = 5\n@x = 6\nk = @x\n")
	if value, at, isConstant := last.ConstantAt("@x", len(last.Src)); value != "6" || at != 7 || !isConstant {
		t.Errorf("the last top statement wins: %q at %d, %v", value, at, isConstant)
	}
	if _, _, isConstant := last.ConstantAt("@y", 0); isConstant {
		t.Error("a constant nothing writes")
	}
	block := Parse("@z = { a = 1 }\n")
	if _, _, isConstant := block.ConstantAt("@z", 0); isConstant {
		t.Error("a block is no value")
	}
	plain := Parse("k = 1\n")
	if _, _, isConstant := plain.ConstantAt("k", 0); isConstant {
		t.Error("a name without @ is no constant")
	}
}

func TestLineIndex(t *testing.T) {
	lines := NewLineIndex("ab\ncde\nf")
	for _, testCase := range []struct{ offset, line, col int }{
		{0, 0, 0},
		{2, 0, 2}, // the \n itself sits at end of line 0
		{3, 1, 0},
		{6, 1, 3},
		{7, 2, 0},
	} {
		position := lines.PositionAt(testCase.offset)
		if position.Line != testCase.line || position.Character != testCase.col {
			t.Errorf("PositionAt(%d) = %d,%d want %d,%d", testCase.offset, position.Line, position.Character, testCase.line, testCase.col)
		}
	}
	text := "ab\ncde\nf"
	roundTrip := NewLineIndex(text)
	for offset := 0; offset <= len(text); offset++ {
		position := roundTrip.PositionAt(offset)
		if got := roundTrip.OffsetAt(position.Line, position.Character); got != offset {
			t.Errorf("round trip offset %d -> (%d,%d) -> %d", offset, position.Line, position.Character, got)
		}
	}
	// "é" is two UTF-8 bytes; the column after it is byte column 3.
	if position := NewLineIndex("aéb").PositionAt(3); position.Line != 0 || position.Character != 3 {
		t.Fatalf("PositionAt(3) = %+v want line 0 col 3", position)
	}
}

// TestWalkVisitsNested pins the depth and parent Walk passes for each statement.
func TestWalkVisitsNested(t *testing.T) {
	result := Parse("a = { b = { c = 1 } }\n")
	want := map[string]int{"a": 0, "b": 1, "c": 2}
	var keys []string
	Walk(result.Root, func(statement Statement, depth int, parent *Block) bool {
		if assignment, ok := statement.(*Assignment); ok {
			keys = append(keys, assignment.Key.Text)
			if depth != want[assignment.Key.Text] {
				t.Errorf("%s depth=%d want %d", assignment.Key.Text, depth, want[assignment.Key.Text])
			}
			if assignment.Key.Text == "a" && parent != nil {
				t.Errorf("a parent=%v", parent)
			}
			if assignment.Key.Text == "b" && parent == nil {
				t.Error("b missing parent")
			}
		}
		return true
	})
	if len(keys) != 3 || keys[0] != "a" || keys[1] != "b" || keys[2] != "c" {
		t.Fatalf("walk keys = %v", keys)
	}
}

func TestNameShapes(t *testing.T) {
	// real shapes from the three games, the boundary edges, and the non-ASCII names they ship
	inputs := []string{
		"", " ", ".", "-", "_", "a", "A", "0", "9",
		fixtureTrait, "k_england", "00_traits", "birth.1", "birth.0001",
		"namespace.1234", "a-b", "a.b", "a..b", "a.", ".a", "-a", "a-",
		"scope:foo", "title:k_england", "$PARAM$", "yes", "no",
		fixtureDate, "8082.1.1", "1.0", "1.", "x.1x", "x.1.2",
		"UPPER", "Mixed_Case", "with space", "tab\there", "naïve", "café",
		"a_b_c", "_leading", "trailing_", "__", "a1_b2",
		"add_gold", "any_trait_in_category", "PRICE", "SCORE_VAR",
		"e.1", "e.01", "e.1a", "1.e", "a.b.1", "a-b.1", "a_b.12345",

		// Shipped non-ASCII. See the comment above IsDefName.
		"mon_general_petar_vukotić", "Linnéa", "RU_GL_КР_DEFAULT",
		"bpm_je_propaganda_of_the_deed_бомбометр",
		"gaudi_pavellons_güell_global_var", "religión",
		"vukotić.1", "бомбометр.42",
		// Punctuation that merely looks like a letter stays out: this one is a
		// real mod typo (`type = country_event·`) worth continuing to reject.
		"country_event·", "café·", "·",
	}
	for _, input := range inputs {
		t.Run("q_"+input, func(t *testing.T) {
			if got, want := IsDefName(input), defNameRe.MatchString(input); got != want {
				t.Errorf("IsDefName(%q) = %v, pattern says %v", input, got, want)
			}
			if got, want := IsEventID(input), eventIDRe.MatchString(input); got != want {
				t.Errorf("IsEventID(%q) = %v, pattern says %v", input, got, want)
			}
		})
	}

	// The three shipped identifiers, stated as behavior rather than as equivalence
	// to a pattern — so the intent survives someone rewriting the patterns.
	for _, testCase := range []struct {
		name string
		want bool
	}{
		{"mon_general_petar_vukotić", true},               // Vic3 vanilla character template
		{"bpm_je_propaganda_of_the_deed_бомбометр", true}, // Vic3 mod journal entry
		{"gaudi_pavellons_güell_global_var", true},        // CK3 mod global var
		{"country_event·", false},                         // U+00B7 is punctuation: this mod typo must stay rejected
	} {
		if got := IsDefName(testCase.name); got != testCase.want {
			t.Errorf("IsDefName(%q) = %v, want %v", testCase.name, got, testCase.want)
		}
	}
}

func TestLiteral(t *testing.T) {
	for _, testCase := range []struct {
		text string
		want LiteralKind
	}{
		{"12", LitNumber},
		{"-3", LitNumber},
		{"0.5", LitNumber},
		{"+2.25", LitNumber},
		{"1066.9.15", LitDate},
		{fixtureDate, LitDate},
		{"1.1.1.12", LitDate},

		// two dotted parts is an event id or a decimal, never a date
		{"birth.1001", NotLiteral},
		{"1066.9", LitNumber},

		// what strconv would take and script does not write
		{"1e5", NotLiteral},
		{"1_000", NotLiteral},
		{"0x10", NotLiteral},
		{"inf", NotLiteral},

		{fixtureTrait, NotLiteral},
		{"yes", NotLiteral},
		{"", NotLiteral},
		{"123456.1.1", NotLiteral},
		{"1.a.1", NotLiteral},
	} {
		if got := Literal(testCase.text); got != testCase.want {
			t.Errorf("Literal(%q) = %d; want %d", testCase.text, got, testCase.want)
		}
	}

	for _, testCase := range []struct {
		text                   string
		year, month, day, hour int
		ok                     bool
	}{
		{"1066.9.15", 1066, 9, 15, 0, true},
		{fixtureDate, 867, 1, 1, 0, true},
		{"1444.11.11.6", 1444, 11, 11, 6, true},
		{"8209.2.1", 8209, 2, 1, 0, true},
		{"birth.1001", 0, 0, 0, 0, false},
		{"1066.9", 0, 0, 0, 0, false},
		{"1066..15", 0, 0, 0, 0, false},
		{"-1.1.1", -1, 1, 1, 0, true},
		{"", 0, 0, 0, 0, false},
	} {
		year, month, day, hour, ok := ParseDate(testCase.text)
		if year != testCase.year || month != testCase.month || day != testCase.day || hour != testCase.hour || ok != testCase.ok {
			t.Errorf("ParseDate(%q) = %d %d %d %d %v; want %d %d %d %d %v", testCase.text, year, month, day, hour, ok, testCase.year, testCase.month, testCase.day, testCase.hour, testCase.ok)
		}
	}
}

func TestSplitPrefixed(t *testing.T) {
	for _, testCase := range []struct {
		text string
		want Prefixed
		ok   bool
	}{
		// plain cites, words that are none, and cites with a chain
		{"culture:english", Prefixed{Prefix: "culture", Name: "english"}, true},
		{"trait:brave", Prefixed{Prefix: "trait", Name: fixtureTrait}, true},
		{"coat_of_arms:b_appleby", Prefixed{Prefix: "coat_of_arms", Name: "b_appleby"}, true},
		{"title:k_france", Prefixed{Prefix: "title", Name: "k_france"}, true},
		{"script_value:max_soldiers", Prefixed{Prefix: "script_value", Name: "max_soldiers"}, true},
		{"location:stockholm", Prefixed{Prefix: "location", Name: "stockholm"}, true},
		{"c:SWE", Prefixed{Prefix: "c", Name: "SWE"}, true},
		{"english", Prefixed{}, false},
		{"capital_province.culture", Prefixed{}, false},
		{"$COA$", Prefixed{}, false},
		{"situation:foo.var:x", Prefixed{Prefix: "situation", Name: fixtureKey, Chain: "var:x"}, true},
		{"s:STATE_KENYA.region_state:OMA", Prefixed{Prefix: "s", Name: "STATE_KENYA", Chain: "region_state:OMA"}, true},

		// a prefix is one plain identifier: a dotted word is a scope path whose later step is the cite
		{"$SCOPE$.var:x", Prefixed{}, false},
		{"root.var:x", Prefixed{}, false},
		{"this.culture:english", Prefixed{}, false},

		// script-made names are cited like any other: no prefix is refused by name
		{"scope:foo", Prefixed{Prefix: scopePrefix, Name: fixtureKey}, true},
		{"var:gold", Prefixed{Prefix: "var", Name: "gold"}, true},
		{"scope:a.var:b", Prefixed{Prefix: scopePrefix, Name: "a", Chain: "var:b"}, true},
		{"type:character_event", Prefixed{Prefix: "type", Name: "character_event"}, true},
		{"scope:my_target.culture", Prefixed{Prefix: scopePrefix, Name: "my_target", Chain: "culture"}, true},

		// a number is a legal name, and so is one that is not ASCII
		{"character:186004", Prefixed{Prefix: "character", Name: "186004"}, true},
		{"province:1234", Prefixed{Prefix: "province", Name: "1234"}, true},
		{"scope:Linnéa", Prefixed{Prefix: scopePrefix, Name: "Linnéa"}, true},
		{"p:xB0D0E0.state.owner", Prefixed{Prefix: "p", Name: "xB0D0E0", Chain: "state.owner"}, true},

		// a define names its group and its key
		{"define:NGame|START_DATE", Prefixed{Prefix: "define", Name: "NGame", Key: "START_DATE"}, true},
		{"define:NGame|", Prefixed{}, false},

		// not a cite: a sound's name, a file, a path, an entry mode, and the broken spellings
		{"event:/SFX/UI/Generic/sfx_ui_click", Prefixed{}, false},
		{"file:/music/my_new_song.ogg", Prefixed{}, false},
		{"C:/Games/x.dds", Prefixed{}, false},
		{`C:\Games\x.dds`, Prefixed{}, false},
		{"INJECT:my_building", Prefixed{}, false},
		{"REPLACE_OR_CREATE:my_building", Prefixed{}, false},
		{"", Prefixed{}, false},
		{scopePrefix, Prefixed{}, false},
		{"scope:", Prefixed{}, false},
		{":x", Prefixed{}, false},
		{"1a:x", Prefixed{}, false},
		{"_a:x", Prefixed{}, false},
		{"scope:a b", Prefixed{}, false},
		{"scope:a.", Prefixed{}, false},
	} {
		got, ok := SplitPrefixed(testCase.text)
		if ok != testCase.ok || got != testCase.want {
			t.Errorf("Cite(%q) = %+v, %v; want %+v, %v", testCase.text, got, ok, testCase.want, testCase.ok)
		}
	}
}

func TestInterpolations(t *testing.T) {
	for _, testCase := range []struct {
		text string
		want []Span
	}{
		// a parameter alone, and a plain scalar
		{"$TRAIT$", []Span{{"TRAIT", 0, 7}}},
		{fixtureTrait, nil},

		{"scope:$TARGET$", []Span{{"TARGET", 6, 14}}},
		{"$A$_and_$B_2$", []Span{{"A", 0, 3}, {"B_2", 8, 13}}},
		{"cost is $VALUE$ gold", []Span{{"VALUE", 8, 15}}},

		// a lone dollar, two dollars with nothing between, and a name that is not word characters
		{"$", nil},
		{"$$", nil},
		{"$TRAIT", nil},
		{"100$ and 200$", nil},
		{"$A$$B$", []Span{{"A", 0, 3}, {"B", 3, 6}}},
	} {
		if got := Interpolations(testCase.text); !reflect.DeepEqual(got, testCase.want) {
			t.Errorf("Interpolations(%q) = %+v; want %+v", testCase.text, got, testCase.want)
		}
	}
}

func TestColorParts(t *testing.T) {
	parsed := Parse("a = rgb { 255 0 0 }\nb = { 0.8 0.3 0.3 1 }\nc = hex { 0xff8000ff }\nd = red\ne = { a b }\n")
	for i, want := range []struct {
		tag   string
		parts []float64
		ok    bool
	}{
		{"rgb", []float64{255, 0, 0}, true},
		{"", []float64{0.8, 0.3, 0.3, 1}, true},
		{"hex", []float64{255, 128, 0, 255}, true},
		{"", nil, false},
		{"", nil, false},
	} {
		tag, parts, ok := ColorParts(parsed.Root.Statements[i].(*Assignment).Value)
		if tag != want.tag || ok != want.ok || !reflect.DeepEqual(parts, want.parts) {
			t.Errorf("statement %d: %q %v %v, want %q %v %v", i, tag, parts, ok, want.tag, want.parts, want.ok)
		}
	}
}

func TestCast(t *testing.T) {
	for _, want := range []struct {
		text, typ, value string
		ok               bool
	}{
		{"'(int32)3'", "int32", "3", true},
		{"(CFixedPoint)0", "CFixedPoint", "0", true},
		{"'(float)0.4'", "float", "0.4", true},
		{"'x'", "", "", false},
	} {
		typ, value, ok := Cast(want.text)
		if typ != want.typ || value != want.value || ok != want.ok {
			t.Errorf("%s: %q %q %v, want %q %q %v", want.text, typ, value, ok, want.typ, want.value, want.ok)
		}
	}
}
