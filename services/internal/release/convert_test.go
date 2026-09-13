package release

import (
	"slices"
	"strings"
	"testing"
)

func TestMarkdownToBBCode(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
		note string
	}{
		{"h1", "# Title\n", "[h1]Title[/h1]\n", ""},
		{"h2", "## Title\n", "[h2]Title[/h2]\n", ""},
		{"h3", "### Title\n", "[h3]Title[/h3]\n", ""},
		{"h4", "#### Deep\n", "[b]Deep[/b]\n", ""},
		{"bold", "**x**\n", "[b]x[/b]\n", ""},
		{"italic", "*x*\n", "[i]x[/i]\n", ""},
		{"strike", "~~x~~\n", "[strike]x[/strike]\n", ""},
		{"link", "[t](https://e)\n", "[url=https://e]t[/url]\n", ""},
		{"img", "![](https://e/a.png)\n", "[img]https://e/a.png[/img]\n", ""},
		{"img http", "![](http://e/a.png)\n", "[img]http://e/a.png[/img]\n", ""},
		{"img local", "![](thumbnail.png)\n", "",
			"images need a public http(s) URL for Steam; local paths dropped"},
		{"img data", "![](data:image/png;base64,xx)\n", "",
			"images need a public http(s) URL for Steam; local paths dropped"},
		{"ul", "- a\n- b\n", "[list][*]a[*]b[/list]\n", ""},
		{"ol", "1. a\n2. b\n", "[olist][*]a[*]b[/olist]\n", ""},
		{"quote", "> hi\n", "[quote]hi[/quote]\n", ""},
		{"code", "```\nfoo\n```\n", "[code]foo\n[/code]\n", ""},
		{"inline", "use `x` please\n", "use [b]x[/b] please\n", ""},
		{"hr", "---\n", "[hr][/hr]\n", ""},
		{"table", "| a | b |\n| - | - |\n| 1 | 2 |\n", "", "tables dropped"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := MarkdownToBBCode(tc.in)
			if strings.TrimSpace(got.Text) != strings.TrimSpace(tc.want) {
				t.Fatalf("text:\n got %q\nwant %q", got.Text, tc.want)
			}
			if tc.note != "" && !hasNote(got.Notes, tc.note) {
				t.Fatalf("notes %v want %q", got.Notes, tc.note)
			}
		})
	}
}

func TestBBCodeRoundTripSubset(t *testing.T) {
	src := "# Hello\n\n**bold** and *i* and ~~s~~\n\n[link](https://e)\n\n- a\n- b\n"
	bb := MarkdownToBBCode(src)
	back := BBCodeToMarkdown(bb.Text)
	again := MarkdownToBBCode(back.Text)
	if strings.TrimSpace(bb.Text) != strings.TrimSpace(again.Text) {
		t.Fatalf("round-trip:\n%s\n--\n%s", bb.Text, again.Text)
	}
}

func TestUnknownBBCodeLiteral(t *testing.T) {
	got := BBCodeToMarkdown("keep [color=red]x[/color] please\n")
	if !strings.Contains(got.Text, "[color=red]x[/color]") {
		t.Fatalf("got %q", got.Text)
	}
}

func hasNote(notes []string, want string) bool {
	return slices.Contains(notes, want)
}
