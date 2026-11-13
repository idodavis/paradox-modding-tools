// markup.go splits a message's text into its markup and gives the text a
// player reads.

package loc

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"paradox-modding-tools/internal/jomini"
)

// Markup is the class of a segment.
type Markup uint8

// The markups of a segment.
const (
	MarkupText Markup = iota
	MarkupKey
	MarkupCall
	MarkupFormat
	MarkupIcon
	MarkupBreak
)

// Segment is one piece of a localization text.
type Segment struct {
	Markup Markup `json:"markup"`

	// Text is the piece without its markers. It is "" for a break and for the
	// end of a format.
	Text string `json:"text"`

	// Span includes the markers and TextSpan does not. TextSpan is empty if
	// the piece has no text.
	Span     jomini.Span `json:"span"`
	TextSpan jomini.Span `json:"textSpan"`

	// Kind is number or text for a data call whose Text was replaced by a
	// sample of its value, and "" for any other piece.
	Kind string `json:"kind"`

	// Call is the parsed data call of a call piece. It is nil if the text is
	// no call, such as ['x'].
	Call *jomini.Call `json:"-"`
}

// Segments splits a message's text into pieces, in order, with spans offset by
// base. A format's segment is followed by those of the markup in its name.
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

// parseMarkup parses the markup that starts at an offset, followed by the
// markup inside a format's name. It returns nil if none starts there.
func parseMarkup(text string, start, base int) []Segment {
	var markup Markup
	var inner string
	var end int
	var call *jomini.Call
	opening := 1

	switch text[start] {
	case '\\':
		if !strings.HasPrefix(text[start:], `\n`) {
			return nil
		}

		markup, end, opening = MarkupBreak, start+2, 2

	case '$':
		closing, ok := findMark(text, start, '$')
		if !ok || strings.HasPrefix(text[start+1:], "|") {
			return nil
		}

		// The format after a key's | is no part of the key.
		inner, _, _ = strings.Cut(text[start+1:closing], "|")
		markup, end = MarkupKey, closing+1

	case '[':
		closing, ok := findCall(text, start)
		if !ok {
			return nil
		}

		markup, inner, end = MarkupCall, text[start+1:closing-1], closing
		if parsed, ok := jomini.ParseCall(inner, base+start+1); ok {
			call = &parsed
		}

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

	textStart := base + start + opening

	return []Segment{{
		Markup:   markup,
		Text:     inner,
		Span:     jomini.Span{Start: base + start, End: base + end},
		TextSpan: jomini.Span{Start: textStart, End: textStart + len(inner)},
		Call:     call,
	}}
}

// findMark returns the offset of the next mark, or false if there is none or
// the text before it is empty or holds a blank.
func findMark(text string, start int, mark byte) (int, bool) {
	closing := strings.IndexByte(text[start+1:], mark)
	if closing <= 0 || strings.IndexFunc(text[start+1:start+1+closing], unicode.IsSpace) >= 0 {
		return 0, false
	}

	return start + 1 + closing, true
}

// findCall returns the offset past the ] closing the [ at a text offset, or
// false if it is not closed on its line or holds only blanks.
func findCall(text string, start int) (int, bool) {
	end := start + jomini.CountBrackets(text, start)
	if end-start < 2 || text[end-1] != ']' || strings.TrimSpace(text[start+1:end-1]) == "" {
		return 0, false
	}

	return end, true
}

// parseFormat parses the format at a #, or its end #!, with the segments of
// the calls and keys in its name. It returns nil if a . or , follows the #.
func parseFormat(text string, start, base int) []Segment {
	if strings.HasPrefix(text[start:], "#!") {
		end := jomini.Span{Start: base + start + 2, End: base + start + 2}

		return []Segment{{Markup: MarkupFormat, Span: jomini.Span{Start: base + start, End: end.End}, TextSpan: end}}
	}

	run := findFormat(text, start+1)
	if run == start+1 || strings.ContainsAny(text[start+1:start+2], ".,") {
		return nil
	}

	// The blank that ends the format's name belongs to its segment.
	_, blank := utf8.DecodeRuneInString(text[run:])
	format := Segment{
		Markup:   MarkupFormat,
		Text:     text[start+1 : run],
		Span:     jomini.Span{Start: base + start, End: base + run + blank},
		TextSpan: jomini.Span{Start: base + start + 1, End: base + run},
	}

	nested := Segments(format.Text, base+start+1)
	nested = slices.DeleteFunc(nested, func(segment Segment) bool { return segment.Markup == MarkupText })

	return append([]Segment{format}, nested...)
}

// findFormat returns where a format ends: at its first blank outside a call or
// key, or at the text's end.
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

func makeText(text string, start, end, base int) Segment {
	span := jomini.Span{Start: base + start, End: base + end}

	return Segment{Markup: MarkupText, Text: text[start:end], Span: span, TextSpan: span}
}

// ListNames returns the names a segment writes, each as a segment of its own:
// a key's or an icon's name, each name in a format such as #bold;italic, and
// the keys inside a data call.
func ListNames(segment Segment) []Segment {
	switch segment.Markup {
	case MarkupKey, MarkupIcon:
		return []Segment{segment}

	case MarkupFormat:
		var formats []Segment
		start := segment.TextSpan.Start

		for part := range strings.SplitSeq(segment.Text, ";") {
			if jomini.Classify(part).IsIdentifier() {
				span := jomini.Span{Start: start, End: start + len(part)}
				formats = append(formats, Segment{Markup: MarkupFormat, Text: part, Span: span, TextSpan: span})
			}

			start += len(part) + 1
		}

		return formats

	case MarkupCall:
		pieces := Segments(segment.Text, segment.TextSpan.Start)

		return slices.DeleteFunc(pieces, func(piece Segment) bool { return piece.Markup != MarkupKey })
	}

	return nil
}

// ListMarkupFaults returns a loc-markup problem for each data call not closed
// on its line, and each format ended with #. or #, where the game wants #!.
func ListMarkupFaults(segments []Segment) []jomini.Problem {
	var faults []jomini.Problem

	for _, segment := range segments {
		if segment.Markup != MarkupText {
			continue
		}

		for offset := range len(segment.Text) {
			start, rest := segment.Span.Start+offset, segment.Text[offset:]

			switch {
			case rest[0] == '[' && isCallStart(rest[1:]):
				faults = append(faults, problem("loc-markup", "a data call has no closing ]",
					jomini.Span{Start: start, End: start + 1}))
			case strings.HasPrefix(rest, "#.") || strings.HasPrefix(rest, "#,"):
				faults = append(faults, problem("loc-markup", "a format ends with #!",
					jomini.Span{Start: start, End: start + 2}))
			}
		}
	}

	return faults
}

// isCallStart reports whether a text after a [ starts like a data call, so a
// lone [ in prose is not reported.
func isCallStart(text string) bool {
	first, _ := utf8.DecodeRuneInString(text)

	return text != "" && (unicode.IsLetter(first) || strings.ContainsRune("_([", first))
}

// ToPlain returns the text a player reads: the plain pieces, with each break
// as a newline.
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
