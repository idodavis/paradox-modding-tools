// parse_test.go covers the parser/lexer: assignments, GUI templates, error
// recovery, tagged blocks, quoted keys, and byte-offset decoding.

package jomini

import (
	"testing"
)

// topKeys returns the keys of a source's top-level assignments.
func topKeys(src string) []string {
	r := Parse(src)
	tops := TopAssignments(r)
	keys := make([]string, len(tops))
	for i, a := range tops {
		keys[i] = a.Key
	}
	return keys
}

func TestParseBasicAssignments(t *testing.T) {
	src := "# header\nfoo = {\n\tid = 1\n}\nbar = 2\n"
	got := topKeys(src)
	if len(got) != 2 || got[0] != "foo" || got[1] != "bar" {
		t.Fatalf("top keys = %v", got)
	}
	r := Parse(src)
	if len(r.Comments) != 1 || r.Comments[0].Range.Start != 0 {
		t.Fatalf("comments = %+v", r.Comments)
	}
	quoted := TopAssignments(Parse("\"my key\" = 1\n"))
	if len(quoted) != 1 || quoted[0].Key != "my key" {
		t.Fatalf("quoted = %+v", quoted)
	}
}

func TestParseGUITemplateIsOneAssignment(t *testing.T) {
	// `template Foo { ... }`: `template` is a bare ValueStmt (skipped by
	// TopAssignments); `Foo { ... }` is one Op=="" assignment.
	src := "template Foo {\n\tsize = { 10 20 }\n}\n"
	r := Parse(src)
	tops := TopAssignments(r)
	if len(tops) != 1 || tops[0].Key != "Foo" {
		t.Fatalf("tops = %+v", tops)
	}
	// The Foo assignment must have Op == "".
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
}

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
}

func TestParseTaggedBlock(t *testing.T) {
	r := Parse("color = rgb { 255 0 0 }\n")
	a, _ := r.Root.Statements[0].(*Assignment)
	tb, ok := a.Value.(*TaggedBlock)
	if !ok || tb.Tag.Text != "rgb" {
		t.Fatalf("tagged = %T %+v", a.Value, a)
	}
}

func TestDecodeStripsBOMAndLatin1Fallback(t *testing.T) {
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
	if had2 || text2 != "x\u00e9y" {
		t.Fatalf("latin1 fallback: had=%v text=%q", had2, text2)
	}
	got := LineIndents("a = {\n\tb = 1\n}\n")
	if len(got) < 3 || got[0] != 0 || got[1] != 1 || got[2] != 0 {
		t.Fatalf("LineIndents = %v, want [0 1 0 ...]", got)
	}
}

// covers LineIndex offset<->position math (incl. UTF-8 byte columns)
// and the Walk / NodeAtOffset helpers.
func TestLineIndexPositionAt(t *testing.T) {
	li := NewLineIndex("ab\ncde\nf")
	cases := []struct {
		offset    int
		line, col int
	}{
		{0, 0, 0},
		{2, 0, 2}, // the \n itself sits at end of line 0
		{3, 1, 0},
		{6, 1, 3},
		{7, 2, 0},
	}
	for _, c := range cases {
		p := li.PositionAt(c.offset)
		if p.Line != c.line || p.Character != c.col {
			t.Errorf("PositionAt(%d) = %d,%d want %d,%d", c.offset, p.Line, p.Character, c.line, c.col)
		}
	}
}

func TestLineIndexOffsetAtRoundTrip(t *testing.T) {
	text := "ab\ncde\nf"
	li := NewLineIndex(text)
	for off := 0; off <= len(text); off++ {
		p := li.PositionAt(off)
		if got := li.OffsetAt(p.Line, p.Character); got != off {
			t.Errorf("round trip offset %d -> (%d,%d) -> %d", off, p.Line, p.Character, got)
		}
	}
}

func TestLineIndexUTF8ByteColumns(t *testing.T) {
	// Non-ASCII: "é" is two UTF-8 bytes. Column after it is byte column 3.
	li := NewLineIndex("a\u00e9b")
	if p := li.PositionAt(3); p.Line != 0 || p.Character != 3 {
		t.Fatalf("PositionAt(3) = %+v want line 0 col 3", p)
	}
}

func TestWalkVisitsNested(t *testing.T) {
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
}

func TestNodeAtOffsetChain(t *testing.T) {
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
}

// covers the shared grammar: kind canonicalisation, trigger and
// effect slots, and the assignment keys that name an ephemeral value.
func TestCanonicalKind(t *testing.T) {
	cases := []struct{ in, want string }{
		{"scripted_effects", "scripted_effects"},
		{"event namespace", "event_namespace"},
		{"landed_titles", "landed_titles"},
		{"title", "title"},
		{"culture", "culture"},
	}
	for _, c := range cases {
		if got := CanonicalKind(c.in); got != c.want {
			t.Errorf("CanonicalKind(%q) = %q want %q", c.in, got, c.want)
		}
	}
}

func TestScriptSlot(t *testing.T) {
	cases := []struct {
		key, want string
	}{
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
	}
	for _, c := range cases {
		if got := ScriptSlot(c.key); got != c.want {
			t.Errorf("ScriptSlot(%q) = %q want %q", c.key, got, c.want)
		}
	}
}

// These names are shared Jomini, not a per-game table: 17 of the 18 are declared
// in effects.log / triggers.log on CK3, Victoria 3 and EU5 alike, and the
// eighteenth (save_temporary_value_as) is used in all three games' vanilla
// script while being declared by none of them.
func TestScriptNameAndPrefixKind(t *testing.T) {
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
}

func TestIsEphemeral(t *testing.T) {
	for _, k := range []string{"saved_scope", "var", "global_var", "flag", "script_param"} {
		if !IsEphemeral(k) {
			t.Errorf("IsEphemeral(%q) = false", k)
		}
	}
	if IsEphemeral("traits") {
		t.Fatal("traits not ephemeral")
	}
}

func TestScriptParamSpan(t *testing.T) {
	src := `add_trait = $TRAIT$`
	name, start, end, ok := ScriptParamSpan(src, 14)
	if !ok || name != "TRAIT" || src[start:end] != "$TRAIT$" {
		t.Fatalf("span name=%q %d:%d ok=%v", name, start, end, ok)
	}
	if _, _, _, ok := ScriptParamSpan(`add_trait = brave`, 14); ok {
		t.Fatal("plain scalar is not a param span")
	}
}
