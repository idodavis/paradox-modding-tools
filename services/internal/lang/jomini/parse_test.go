// parse_test.go covers the parser/lexer: assignments, GUI templates, error
// recovery, tagged blocks, byte-offset decoding, and the shared grammar
// (kind canonicalisation, script slots, ephemeral names).

package jomini

import (
	"testing"
)

// topKeys returns the keys of a source's top-level Assignment statements.
func topKeys(src string) []string {
	r := Parse(src)
	var keys []string
	for _, st := range r.Root.Statements {
		if a, ok := st.(*Assignment); ok {
			keys = append(keys, a.Key.Text)
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
		r := Parse(src)
		if len(r.Comments) != 1 || r.Comments[0].Range.Start != 0 {
			t.Fatalf("comments = %+v", r.Comments)
		}
		quoted := topKeys("\"my key\" = 1\n")
		if len(quoted) != 1 || quoted[0] != "my key" {
			t.Fatalf("quoted = %+v", quoted)
		}
	})

	t.Run("gui_template_is_one_assignment", func(t *testing.T) {
		// `template` is a bare ValueStmt (skipped by topKeys); `Foo { ... }` is one Op=="" assignment.
		src := "template Foo {\n\tsize = { 10 20 }\n}\n"
		r := Parse(src)
		if got := topKeys(src); len(got) != 1 || got[0] != "Foo" {
			t.Fatalf("top keys = %v", got)
		}
		var foo *Assignment
		for _, st := range r.Root.Statements {
			if a, ok := st.(*Assignment); ok && a.Key.Text == "Foo" {
				foo = a
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
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				r := Parse(tt.src)
				found := false
				for _, e := range r.Errors {
					if e.Code == tt.code {
						found = true
					}
				}
				if !found {
					t.Fatalf("want %s, got %+v", tt.code, r.Errors)
				}
				if tt.recoverBar {
					if keys := topKeys(tt.src); len(keys) != 2 || keys[1] != "bar" {
						t.Fatalf("recovery failed, keys = %v", keys)
					}
				}
			})
		}
	})

	t.Run("tagged_block", func(t *testing.T) {
		r := Parse("color = rgb { 255 0 0 }\n")
		a, _ := r.Root.Statements[0].(*Assignment)
		tb, ok := a.Value.(*TaggedBlock)
		if !ok || tb.Tag.Text != "rgb" {
			t.Fatalf("tagged = %T %+v", a.Value, a)
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
		text2, had2 := Decode([]byte{'x', 0xE9, 'y'})
		if had2 || text2 != "xéy" {
			t.Fatalf("latin1 fallback: had=%v text=%q", had2, text2)
		}
		got := LineIndents("a = {\n\tb = 1\n}\n")
		if len(got) < 3 || got[0] != 0 || got[1] != 1 || got[2] != 0 {
			t.Fatalf("LineIndents = %v, want [0 1 0 ...]", got)
		}
	})

	// LineIndex offset<->position math, including UTF-8 byte columns.
	t.Run("line_index", func(t *testing.T) {
		li := NewLineIndex("ab\ncde\nf")
		for _, c := range []struct{ offset, line, col int }{
			{0, 0, 0},
			{2, 0, 2}, // the \n itself sits at end of line 0
			{3, 1, 0},
			{6, 1, 3},
			{7, 2, 0},
		} {
			p := li.PositionAt(c.offset)
			if p.Line != c.line || p.Character != c.col {
				t.Errorf("PositionAt(%d) = %d,%d want %d,%d", c.offset, p.Line, p.Character, c.line, c.col)
			}
		}
		text := "ab\ncde\nf"
		li2 := NewLineIndex(text)
		for off := 0; off <= len(text); off++ {
			p := li2.PositionAt(off)
			if got := li2.OffsetAt(p.Line, p.Character); got != off {
				t.Errorf("round trip offset %d -> (%d,%d) -> %d", off, p.Line, p.Character, got)
			}
		}
		// "é" is two UTF-8 bytes; the column after it is byte column 3.
		if p := NewLineIndex("aéb").PositionAt(3); p.Line != 0 || p.Character != 3 {
			t.Fatalf("PositionAt(3) = %+v want line 0 col 3", p)
		}
	})

	t.Run("walk_visits_nested", func(t *testing.T) {
		r := Parse("a = { b = { c = 1 } }\n")
		want := map[string]int{"a": 0, "b": 1, "c": 2}
		var keys []string
		Walk(r.Root, func(st Statement, depth int, parent *Block) bool {
			if a, ok := st.(*Assignment); ok {
				keys = append(keys, a.Key.Text)
				if depth != want[a.Key.Text] {
					t.Errorf("%s depth=%d want %d", a.Key.Text, depth, want[a.Key.Text])
				}
				if a.Key.Text == "a" && parent != nil {
					t.Errorf("a parent=%v", parent)
				}
				if a.Key.Text == "b" && parent == nil {
					t.Error("b missing parent")
				}
			}
			return true
		})
		if len(keys) != 3 || keys[0] != "a" || keys[1] != "b" || keys[2] != "c" {
			t.Fatalf("walk keys = %v", keys)
		}
	})

	t.Run("node_at_offset_chain", func(t *testing.T) {
		src := "a = { b = 1 }\n"
		r := Parse(src)
		path := NodeAtOffset(r.Root, 6)
		if len(path) != 2 {
			t.Fatalf("path len = %d want 2 (%v)", len(path), path)
		}
		outer, ok := path[0].(*Assignment)
		if !ok || outer.Key.Text != "a" {
			t.Fatalf("outer = %+v", path[0])
		}
		inner, ok := path[1].(*Assignment)
		if !ok || inner.Key.Text != "b" {
			t.Fatalf("inner = %+v", path[1])
		}
	})
}

func TestScriptGrammar(t *testing.T) {
	t.Run("canonical_kind", func(t *testing.T) {
		for _, c := range []struct{ in, want string }{
			{"scripted_effects", "scripted_effects"},
			{"event namespace", "event_namespace"},
			{"landed_titles", "landed_titles"},
			{"title", "title"},
			{"culture", "culture"},
		} {
			if got := CanonicalKind(c.in); got != c.want {
				t.Errorf("CanonicalKind(%q) = %q want %q", c.in, got, c.want)
			}
		}
	})

	t.Run("script_slot", func(t *testing.T) {
		for _, c := range []struct{ key, want string }{
			{"trigger", "trigger"},
			{"limit", "trigger"},
			{"AND", "trigger"},
			{"any_courtier", "trigger"},
			{"immediate", "effect"},
			{"after", "effect"},
			{"every_child", "effect"},
			{"effect", "effect"},
			{"title", ""},
			{"type", ""},
			{"test.1", ""},
		} {
			if got := ScriptSlot(c.key); got != c.want {
				t.Errorf("ScriptSlot(%q) = %q want %q", c.key, got, c.want)
			}
		}
	})

	// These names are shared Jomini, not a per-game table: 17 of 18 are declared on CK3, Vic3 and EU5 alike.
	t.Run("script_name_and_prefix_kind", func(t *testing.T) {
		if r, ok := ScriptName("has_variable"); !ok || r.Kind != "var" {
			t.Fatalf("has_variable: %+v ok=%v", r, ok)
		}
		if r, ok := ScriptName("set_variable"); !ok || !r.IsDef || r.InnerKey != "name" {
			t.Fatalf("set_variable: %+v ok=%v", r, ok)
		}
		if PrefixKind("var") != "" || PrefixKind("scope") != "saved_scope" {
			t.Fatal("PrefixKind")
		}
		if !IsSaveScopeKey("save_scope_as") || IsSaveScopeKey("save_scope_value_as") {
			t.Fatal("IsSaveScopeKey")
		}
		if !IsSaveScopeValueKey("save_scope_value_as") {
			t.Fatal("IsSaveScopeValueKey")
		}
	})

	t.Run("is_ephemeral", func(t *testing.T) {
		for _, k := range []string{"saved_scope", "var", "global_var", "flag", "script_param"} {
			if !IsEphemeral(k) {
				t.Errorf("IsEphemeral(%q) = false", k)
			}
		}
		if IsEphemeral("traits") {
			t.Fatal("traits not ephemeral")
		}
	})

	t.Run("script_param_span", func(t *testing.T) {
		src := `add_trait = $TRAIT$`
		name, start, end, ok := ScriptParamSpan(src, 14)
		if !ok || name != "TRAIT" || src[start:end] != "$TRAIT$" {
			t.Fatalf("span name=%q %d:%d ok=%v", name, start, end, ok)
		}
		if _, _, _, ok := ScriptParamSpan(`add_trait = brave`, 14); ok {
			t.Fatal("plain scalar is not a param span")
		}
	})
}
