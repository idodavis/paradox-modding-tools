// markup.go splits the text of a localization message into plain text, keys,
// data calls, formats, icons and line breaks, each with its spans in the
// file, and gives the text a player reads.

package loc

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"paradox-modding-tools/internal/jomini"
)

// Markup is the class of a segment of a localization text.
type Markup uint8

// The markups a segment can have, in order: plain text, $key$, [call],
// #format and its end #!, @icon! and \n.
const (
	MarkupText Markup = iota
	MarkupKey
	MarkupCall
	MarkupFormat
	MarkupIcon
	MarkupBreak
)

// Segment is one piece of a localization text, with its spans in the file.
type Segment struct {
	Markup Markup `json:"markup"`

	// Text is the piece without its markers: the key, the call, the format's
	// name or the icon. It is "" for a break and for the end of a format.
	Text string `json:"text"`

	// Span is the piece with its markers, and TextSpan is where Text sits
	// inside them. TextSpan is empty where the piece has no text.
	Span     jomini.Span `json:"span"`
	TextSpan jomini.Span `json:"textSpan"`

	// Kind is the kind of value a data call shows, number or text, where Text
	// holds a sample of that value in place of the call. It is "" for a call
	// as written and for any other piece.
	Kind string `json:"kind"`
}

// Segments splits a message's text into its pieces, in order. The given
// offset is where the text starts in its file, so the spans are the file's.
// A format's segment is followed by those of the markup inside its name.
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

// parseMarkup parses the markup that starts at the given offset of a text,
// which starts at the given base in its file. It returns the markup's
// segment, followed by those of any markup inside a format's name, or nil if
// no markup starts there.
func parseMarkup(text string, start, base int) []Segment {
	var markup Markup
	var inner string
	var end int
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
	}}
}

// findMark returns the offset of the next mark character after the given
// offset of a text. It returns false if there is none, or if the text
// between them is empty or holds a blank.
func findMark(text string, start int, mark byte) (int, bool) {
	closing := strings.IndexByte(text[start+1:], mark)
	if closing <= 0 || strings.IndexFunc(text[start+1:start+1+closing], unicode.IsSpace) >= 0 {
		return 0, false
	}

	return start + 1 + closing, true
}

// findCall returns the offset just past the ] that closes the [ at the given
// offset of a text. It returns false if the bracket is not closed on its
// line, or if only blanks are inside.
func findCall(text string, start int) (int, bool) {
	end := start + jomini.CountBrackets(text, start)
	if end-start < 2 || text[end-1] != ']' || strings.TrimSpace(text[start+1:end-1]) == "" {
		return 0, false
	}

	return end, true
}

// parseFormat parses the format that starts at the # at the given offset of
// a text, or the end of a format where #! is there. It returns the format's
// segment, followed by those of the calls and keys inside its name. It
// returns nil if no format follows the #, as when a . or a , does.
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

// findFormat returns the offset where the format that begins at the given
// offset of a text ends: at its first blank outside a call or a key, or at
// the end of the text.
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

// makeText returns the plain segment that covers a range of a text, which
// starts at the given base in its file.
func makeText(text string, start, end, base int) Segment {
	span := jomini.Span{Start: base + start, End: base + end}

	return Segment{Markup: MarkupText, Text: text[start:end], Span: span, TextSpan: span}
}

// ListCalls parses the call segments among the segments, those inside
// formats included. A segment whose text is no call is passed over.
func ListCalls(segments []Segment) []jomini.Call {
	var calls []jomini.Call

	for _, segment := range segments {
		if segment.Markup != MarkupCall {
			continue
		}

		if call, ok := jomini.ParseCall(segment.Text, segment.TextSpan.Start); ok {
			calls = append(calls, call)
		}
	}

	return calls
}

// ListNames returns the names that one segment of a text writes, each as a
// segment with its markup, its text and its TextSpan. A key and an icon write
// their own name. A format writes each of its parts that is an identifier,
// so #bold;italic writes bold and italic. A data call writes the keys inside
// its text. A format's part has no markers, so its Span is its TextSpan. Any
// other segment writes no name, and ListNames returns nil for it.
func ListNames(segment Segment) []Segment {
	switch segment.Markup {
	case MarkupKey, MarkupIcon:
		return []Segment{segment}

	case MarkupFormat:
		var formats []Segment
		start := segment.TextSpan.Start

		for part := range strings.SplitSeq(segment.Text, ";") {
			if jomini.IsIdentifier(part) {
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

// ListMarkupFaults returns a loc-markup error for each markup mistake in the
// plain text of the segments: a [ that starts a call but has no closing ],
// and a format ended with #. or #, where the game ends one with #!.
func ListMarkupFaults(segments []Segment) []jomini.Error {
	var faults []jomini.Error

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

// isCallStart reports whether a text starts as a data call does: with a
// letter, an underscore, the ( of a cast or another [.
func isCallStart(text string) bool {
	first, _ := utf8.DecodeRuneInString(text)

	return text != "" && (unicode.IsLetter(first) || strings.ContainsRune("_([", first))
}

// ToPlain returns the text a player reads: the plain pieces joined together,
// the markup dropped, and each break written as a newline.
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
