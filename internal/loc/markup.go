package loc

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"paradox-modding-tools/internal/jomini"
)

// A Markup is the class of a segment of a localization text: plain text, a key, a call, a format, an icon or a break.
type Markup uint8

// The markups a segment can have.
const (
	MarkupText   Markup = iota // plain text the player reads
	MarkupKey                  // `$key$`: another message's text, or a value the game fills in
	MarkupCall                 // `[call]`: a data call
	MarkupFormat               // `#bold … #!`: a text format and its end
	MarkupIcon                 // `@icon!`: an icon
	MarkupBreak                // `\n`
)

// A Segment is one piece of a localization text, with its span in the file.
type Segment struct {
	Markup   Markup      `json:"markup"`
	Text     string      `json:"text"` // the piece without its markers: the key, the call, the format's name, the icon
	Span     jomini.Span `json:"span"`
	TextSpan jomini.Span `json:"textSpan"` // the span of Text inside the markers; empty where there is no text
}

// Segments splits a message's text into its pieces. The offset is where the text's first byte sits in its file, so
// the spans come out in the file's offsets.
func Segments(text string, base int) []Segment {
	var segments []Segment
	plain := 0
	for i := 0; i < len(text); {
		parsed := parseMarkup(text, i, base)
		if parsed == nil {
			i++
			continue
		}
		if plain < i {
			segments = append(segments, makeText(text, plain, i, base))
		}
		segments = append(segments, parsed...)
		i = parsed[0].Span.End - base
		plain = i
	}
	if plain < len(text) {
		segments = append(segments, makeText(text, plain, len(text), base))
	}
	return segments
}

// parseMarkup parses the markup that starts at the given offset of a text. The base offset is where the text's first
// byte sits in its file. It returns the markup's segment first, and after it the segments of any markup inside a
// format. It returns nil if nothing starts there, so the character is plain text.
func parseMarkup(text string, start, base int) []Segment {
	var markup Markup
	var inner string
	var end int
	lead := 1
	switch text[start] {
	case '\\':
		if !strings.HasPrefix(text[start:], `\n`) {
			return nil
		}
		markup, end, lead = MarkupBreak, start+2, 2
	case '$':
		closing, ok := findMark(text, start, '$')
		if !ok || strings.HasPrefix(text[start+1:], "|") {
			return nil
		}
		inner, _, _ = strings.Cut(text[start+1:closing], "|")
		markup, end = MarkupKey, closing+1
	case '[':
		closing, ok := findCall(text, start)
		if !ok {
			return nil
		}
		markup, inner, end = MarkupCall, text[start+1:closing-1], closing
	case '#':
		return parseFormat(text, start, base)
	case '@':
		closing, ok := findMark(text, start, '!')
		if !ok {
			return nil
		}
		markup, inner, end = MarkupIcon, text[start+1:closing], closing+1
	default:
		return nil
	}
	textStart := base + start + lead
	return []Segment{{
		Markup: markup, Text: inner, Span: jomini.Span{Start: base + start, End: base + end},
		TextSpan: jomini.Span{Start: textStart, End: textStart + len(inner)},
	}}
}

// findMark returns the offset of the closing character of the markup that starts at the given offset of a text. It
// returns false if there is none, or if the characters between are empty or hold a space.
func findMark(text string, start int, mark byte) (int, bool) {
	closing := strings.IndexByte(text[start+1:], mark)
	if closing <= 0 || strings.IndexFunc(text[start+1:start+1+closing], unicode.IsSpace) >= 0 {
		return 0, false
	}
	return start + 1 + closing, true
}

// findCall returns the offset just past the `]` that closes the `[` at the given offset of a text. It returns false
// if the bracket is never closed on its line, or if nothing but white space is inside.
func findCall(text string, start int) (int, bool) {
	end := start + jomini.CountBrackets(text, start)
	if end-start < 2 || text[end-1] != ']' || strings.TrimSpace(text[start+1:end-1]) == "" {
		return 0, false
	}
	return end, true
}

// parseFormat parses the format that starts with the `#` at the given offset of a text, or the end of a format if
// `#!` is there. The base offset is where the text's first byte sits in its file. A format runs to the first white
// space outside a call or a key, and that one white space belongs to it. It returns the format's segment first, then
// the segments of the calls and keys inside it. It returns nil if no format follows the `#`.
func parseFormat(text string, start, base int) []Segment {
	if strings.HasPrefix(text[start:], "#!") {
		end := base + start + 2
		return []Segment{{Markup: MarkupFormat, Span: jomini.Span{Start: base + start, End: end}, TextSpan: jomini.Span{Start: end, End: end}}}
	}
	run := findFormat(text, start+1)
	if run == start+1 {
		return nil
	}
	_, space := utf8.DecodeRuneInString(text[run:])
	span := jomini.Span{Start: base + start, End: base + run + space}
	format := Segment{
		Markup: MarkupFormat, Text: text[start+1 : run], Span: span,
		TextSpan: jomini.Span{Start: base + start + 1, End: base + run},
	}
	inside := Segments(format.Text, base+start+1)
	inside = slices.DeleteFunc(inside, func(segment Segment) bool { return segment.Markup == MarkupText })
	return append([]Segment{format}, inside...)
}

// findFormat returns the offset where the format that begins at the given offset of a text ends: its first white
// space outside a call or a key, or the end of the text.
func findFormat(text string, start int) int {
	for i := start; i < len(text); {
		r, size := utf8.DecodeRuneInString(text[i:])
		if unicode.IsSpace(r) {
			return i
		}
		next := i + size
		switch r {
		case '[':
			if end, ok := findCall(text, i); ok {
				next = end
			}
		case '$':
			if closing, ok := findMark(text, i, '$'); ok {
				next = closing + 1
			}
		}
		i = next
	}
	return len(text)
}

// makeText returns the plain segment that covers the given range of a text. The base offset is where the text's first
// byte sits in its file.
func makeText(text string, start, end, base int) Segment {
	span := jomini.Span{Start: base + start, End: base + end}
	return Segment{Markup: MarkupText, Text: text[start:end], Span: span, TextSpan: span}
}

// ToPlain returns the text a player reads: the plain pieces joined together, the markup dropped, and each break
// written as a newline.
func ToPlain(segments []Segment) string {
	var plain strings.Builder
	for _, segment := range segments {
		switch segment.Markup {
		case MarkupText:
			plain.WriteString(segment.Text)
		case MarkupBreak:
			plain.WriteByte('\n')
		}
	}
	return plain.String()
}
