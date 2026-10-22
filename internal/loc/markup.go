package loc

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"paradox-modding-tools/internal/jomini"
)

// Markup says what kind of piece a segment of a localization text is.
type Markup uint8

// The kinds of piece a localization text is made of.
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
	Markup Markup      `json:"markup"`
	Text   string      `json:"text"` // the piece without its markers: the key, the call, the format's name, the icon
	Span   jomini.Span `json:"span"`
}

// Segments splits a message's text into its pieces. The second argument is the offset of the text's first byte in
// its file, so the spans come out in the file's offsets.
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

// parseMarkup reads the markup that starts at the given offset of a text. The last argument is the offset of the
// text in its file. It returns the markup's segment first, and after it the segments of any markup inside a
// format. It returns nil if nothing starts there, so the character is plain text.
func parseMarkup(text string, start, base int) []Segment {
	var markup Markup
	var inner string
	var end int
	switch text[start] {
	case '\\':
		if !strings.HasPrefix(text[start:], `\n`) {
			return nil
		}
		markup, end = MarkupBreak, start+2
	case '$':
		closing := findMark(text, start, '$')
		if closing < 0 || strings.HasPrefix(text[start+1:], "|") {
			return nil
		}
		inner, _, _ = strings.Cut(text[start+1:closing], "|")
		markup, end = MarkupKey, closing+1
	case '[':
		end = findCall(text, start)
		if end < 0 {
			return nil
		}
		markup, inner = MarkupCall, text[start+1:end-1]
	case '#':
		return parseFormat(text, start, base)
	case '@':
		closing := findMark(text, start, '!')
		if closing < 0 {
			return nil
		}
		markup, inner, end = MarkupIcon, text[start+1:closing], closing+1
	default:
		return nil
	}
	return []Segment{{Markup: markup, Text: inner, Span: jomini.Span{Start: base + start, End: base + end}}}
}

// findMark returns the offset of the given closing character, looking after the one at the given offset of a text.
// It returns -1 if there is none, or if the characters between are empty or hold a space.
func findMark(text string, start int, mark byte) int {
	closing := strings.IndexByte(text[start+1:], mark)
	if closing <= 0 || strings.IndexFunc(text[start+1:start+1+closing], unicode.IsSpace) >= 0 {
		return -1
	}
	return start + 1 + closing
}

// findCall returns the offset just past the `]` that closes the `[` at the given offset of a text. It returns -1 if
// the bracket is never closed on its line, or if nothing but white space is inside.
func findCall(text string, start int) int {
	end := start + jomini.CountBrackets(text, start)
	if end-start < 2 || text[end-1] != ']' || strings.TrimSpace(text[start+1:end-1]) == "" {
		return -1
	}
	return end
}

// parseFormat reads the format that starts with the `#` at the given offset of a text, or the end of a format if
// `#!` is there. The last argument is the offset of the text in its file. A format runs to the first white space
// outside a call or a key, and that one white space belongs to it. It returns the format's segment first, then the
// segments of the calls and keys inside it. It returns nil if no format follows the `#`.
func parseFormat(text string, start, base int) []Segment {
	if strings.HasPrefix(text[start:], "#!") {
		return []Segment{{Markup: MarkupFormat, Span: jomini.Span{Start: base + start, End: base + start + 2}}}
	}
	run := findFormat(text, start+1)
	if run == start+1 {
		return nil
	}
	_, space := utf8.DecodeRuneInString(text[run:])
	span := jomini.Span{Start: base + start, End: base + run + space}
	format := Segment{Markup: MarkupFormat, Text: text[start+1 : run], Span: span}
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
			if end := findCall(text, i); end >= 0 {
				next = end
			}
		case '$':
			if closing := findMark(text, i, '$'); closing >= 0 {
				next = closing + 1
			}
		}
		i = next
	}
	return len(text)
}

// makeText returns the plain segment that covers the given range of a text. The last argument is the offset of the
// text in its file.
func makeText(text string, start, end, base int) Segment {
	return Segment{Markup: MarkupText, Text: text[start:end], Span: jomini.Span{Start: base + start, End: base + end}}
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
