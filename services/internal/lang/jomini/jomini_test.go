// jomini_test.go covers the parser/lexer: assignments, GUI templates, error
// recovery, tagged blocks, byte-offset decoding, the line index and the walk;
// and the name shapes, literals, cites, entry modes and interpolations of the shared grammar.

package jomini

import (
	"reflect"
	"regexp"
	"testing"
)

var (
	defNameRe = regexp.MustCompile(`^[\p{L}\p{Nd}_][\p{L}\p{Nd}_.-]*$`)
	eventIDRe = regexp.MustCompile(`^[\p{L}\p{Nd}_-]+\.\d+$`)
	nameOKRe  = regexp.MustCompile(`^\p{Ll}[\p{Ll}\p{Nd}_]*$`)
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

func TestParse(t *testing.T) {
	t.Run("basic_assignments", func(t *testing.T) {
		src := "# header\nfoo = {\n\tid = 1\n}\nbar = 2\n"
		got := topKeys(src)
		if len(got) != 2 || got[0] != "foo" || got[1] != "bar" {
			t.Fatalf("top keys = %v", got)
		}
		result := Parse(src)
		if len(result.Comments) != 1 || result.Comments[0].Range.Start != 0 {
			t.Fatalf("comments = %+v", result.Comments)
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

	t.Run("errors", func(t *testing.T) {
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
	})

	t.Run("tagged_block", func(t *testing.T) {
		result := Parse("color = rgb { 255 0 0 }\n")
		assignment, _ := result.Root.Statements[0].(*Assignment)
		tagged, ok := assignment.Value.(*TaggedBlock)
		if !ok || tagged.Tag.Text != "rgb" {
			t.Fatalf("tagged = %T %+v", assignment.Value, assignment)
		}
	})

	t.Run("decode", func(t *testing.T) {
		withBOM := append([]byte{0xEF, 0xBB, 0xBF}, []byte("foo = 1")...)
		text, had := Decode(withBOM)
		if !had || text != "foo = 1" {
			t.Fatalf("BOM decode: had=%v text=%q", had, text)
		}
		crlf, hadCR := Decode([]byte("a = 1\r\nb = 2\r\n"))
		if hadCR || crlf != "a = 1\nb = 2\n" {
			t.Fatalf("CRLF decode: had=%v text=%q", hadCR, crlf)
		}
		// 0xE9 alone is invalid UTF-8 -> latin1 fallback (é).
		latin1, hadLatin1 := Decode([]byte{'x', 0xE9, 'y'})
		if hadLatin1 || latin1 != "xéy" {
			t.Fatalf("latin1 fallback: had=%v text=%q", hadLatin1, latin1)
		}
	})

	// LineIndex offset<->position math, including UTF-8 byte columns.
	t.Run("line_index", func(t *testing.T) {
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
	})

	t.Run("walk_visits_nested", func(t *testing.T) {
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
	})

}

func TestNameShapes(t *testing.T) {
	// Real shapes from the three installs, plus the edges that decide the
	// boundaries: empty, leading punctuation, trailing dot, spaces — and the
	// non-ASCII identifiers the games and mods actually ship.
	inputs := []string{
		"", " ", ".", "-", "_", "a", "A", "0", "9",
		"brave", "k_england", "00_traits", "birth.1", "birth.0001",
		"namespace.1234", "a-b", "a.b", "a..b", "a.", ".a", "-a", "a-",
		"scope:foo", "title:k_england", "$PARAM$", "yes", "no",
		"867.1.1", "8082.1.1", "1.0", "1.", "x.1x", "x.1.2",
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
			if got, want := IsLowerName(input), nameOKRe.MatchString(input); got != want {
				t.Errorf("IsLowerName(%q) = %v, pattern says %v", input, got, want)
			}
		})
	}

	// The three shipped identifiers, stated as behaviour rather than as equivalence
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
		{"867.1.1", LitDate},
		{"1.1.1.12", LitDate},

		// two dotted parts is an event id or a decimal, never a date
		{"birth.1001", NotLiteral},
		{"1066.9", LitNumber},

		// what strconv would take and script does not write
		{"1e5", NotLiteral},
		{"1_000", NotLiteral},
		{"0x10", NotLiteral},
		{"inf", NotLiteral},

		{"brave", NotLiteral},
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
		{"867.1.1", 867, 1, 1, 0, true},
		{"1444.11.11.6", 1444, 11, 11, 6, true},
		{"8209.2.1", 8209, 2, 1, 0, true},
		{"birth.1001", 0, 0, 0, 0, false},
		{"1066.9", 0, 0, 0, 0, false},
		{"1066..15", 0, 0, 0, 0, false},
		{"-1.1.1", 0, 0, 0, 0, false},
		{"", 0, 0, 0, 0, false},
	} {
		year, month, day, hour, ok := ParseDate(testCase.text)
		if year != testCase.year || month != testCase.month || day != testCase.day || hour != testCase.hour || ok != testCase.ok {
			t.Errorf("ParseDate(%q) = %d %d %d %d %v; want %d %d %d %d %v", testCase.text, year, month, day, hour, ok, testCase.year, testCase.month, testCase.day, testCase.hour, testCase.ok)
		}
	}
}

func TestCite(t *testing.T) {
	for _, testCase := range []struct {
		text string
		want Cited
		ok   bool
	}{
		// plain cites, words that are none, and cites with a chain
		{"culture:english", Cited{Prefix: "culture", Name: "english"}, true},
		{"trait:brave", Cited{Prefix: "trait", Name: "brave"}, true},
		{"coat_of_arms:b_appleby", Cited{Prefix: "coat_of_arms", Name: "b_appleby"}, true},
		{"title:k_france", Cited{Prefix: "title", Name: "k_france"}, true},
		{"script_value:max_soldiers", Cited{Prefix: "script_value", Name: "max_soldiers"}, true},
		{"location:stockholm", Cited{Prefix: "location", Name: "stockholm"}, true},
		{"c:SWE", Cited{Prefix: "c", Name: "SWE"}, true},
		{"english", Cited{}, false},
		{"capital_province.culture", Cited{}, false},
		{"$COA$", Cited{}, false},
		{"situation:foo.var:x", Cited{Prefix: "situation", Name: "foo", Chain: "var:x"}, true},
		{"s:STATE_KENYA.region_state:OMA", Cited{Prefix: "s", Name: "STATE_KENYA", Chain: "region_state:OMA"}, true},

		// a prefix is one plain identifier: a dotted word is a scope path whose later step is the cite
		{"$SCOPE$.var:x", Cited{}, false},
		{"root.var:x", Cited{}, false},
		{"this.culture:english", Cited{}, false},

		// script-made names are cited like any other: no prefix is refused by name
		{"scope:foo", Cited{Prefix: "scope", Name: "foo"}, true},
		{"var:gold", Cited{Prefix: "var", Name: "gold"}, true},
		{"scope:a.var:b", Cited{Prefix: "scope", Name: "a", Chain: "var:b"}, true},
		{"type:character_event", Cited{Prefix: "type", Name: "character_event"}, true},
		{"scope:my_target.culture", Cited{Prefix: "scope", Name: "my_target", Chain: "culture"}, true},

		// a number is a legal name, and so is one that is not ASCII
		{"character:186004", Cited{Prefix: "character", Name: "186004"}, true},
		{"province:1234", Cited{Prefix: "province", Name: "1234"}, true},
		{"scope:Linnéa", Cited{Prefix: "scope", Name: "Linnéa"}, true},
		{"p:xB0D0E0.state.owner", Cited{Prefix: "p", Name: "xB0D0E0", Chain: "state.owner"}, true},

		// a define names its group and its key
		{"define:NGame|START_DATE", Cited{Prefix: "define", Name: "NGame", Member: "START_DATE"}, true},
		{"define:NGame|", Cited{}, false},

		// not a cite: a sound's name, a file, a path, an entry mode, and the broken spellings
		{"event:/SFX/UI/Generic/sfx_ui_click", Cited{}, false},
		{"file:/music/my_new_song.ogg", Cited{}, false},
		{"C:/Games/x.dds", Cited{}, false},
		{`C:\Games\x.dds`, Cited{}, false},
		{"INJECT:my_building", Cited{}, false},
		{"REPLACE_OR_CREATE:my_building", Cited{}, false},
		{"", Cited{}, false},
		{"scope", Cited{}, false},
		{"scope:", Cited{}, false},
		{":x", Cited{}, false},
		{"1a:x", Cited{}, false},
		{"_a:x", Cited{}, false},
		{"scope:a b", Cited{}, false},
		{"scope:a.", Cited{}, false},
	} {
		got, ok := Cite(testCase.text)
		if ok != testCase.ok || got != testCase.want {
			t.Errorf("Cite(%q) = %+v, %v; want %+v, %v", testCase.text, got, ok, testCase.want, testCase.ok)
		}
	}
}

func TestMode(t *testing.T) {
	for _, testCase := range []struct{ key, mode, name string }{
		{"INJECT:my_building", "INJECT", "my_building"},
		{"REPLACE:my_building", "REPLACE", "my_building"},
		{"TRY_INJECT:x", "TRY_INJECT", "x"},
		{"REPLACE_OR_CREATE:x", "REPLACE_OR_CREATE", "x"},

		// a word the registry has never heard of still splits, so the learner can report it as drift
		{"APPEND:x", "APPEND", "x"},

		// not a mode
		{"my_building", "", "my_building"},
		{"culture:english", "", "culture:english"},
		{"Inject:x", "", "Inject:x"},
		{"INJECT:", "", "INJECT:"},
		{":x", "", ":x"},
		{"", "", ""},
	} {
		if mode, name := Mode(testCase.key); mode != testCase.mode || name != testCase.name {
			t.Errorf("Mode(%q) = %q, %q; want %q, %q", testCase.key, mode, name, testCase.mode, testCase.name)
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
		{"brave", nil},

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
