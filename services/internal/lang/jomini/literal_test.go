package jomini

import "testing"

func TestLiteral(t *testing.T) {
	for _, c := range []struct {
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
		if got := Literal(c.text); got != c.want {
			t.Errorf("Literal(%q) = %d; want %d", c.text, got, c.want)
		}
	}
}

func TestParseDate(t *testing.T) {
	for _, c := range []struct {
		text       string
		y, m, d, h int
		ok         bool
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
		y, m, d, h, ok := ParseDate(c.text)
		if y != c.y || m != c.m || d != c.d || h != c.h || ok != c.ok {
			t.Errorf("ParseDate(%q) = %d %d %d %d %v; want %d %d %d %d %v", c.text, y, m, d, h, ok, c.y, c.m, c.d, c.h, c.ok)
		}
	}
}

func TestIsDigits(t *testing.T) {
	for text, want := range map[string]bool{"0": true, "186004": true, "": false, "12a": false, "-1": false, "1.5": false} {
		if got := IsDigits(text); got != want {
			t.Errorf("IsDigits(%q) = %v; want %v", text, got, want)
		}
	}
}
