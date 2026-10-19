// markup.go reads the markup in a localization value: the segments that share one formatting, and what they answer: the
// text a player reads, the keys a value interpolates and the concepts its data calls name.

package loc

import (
	"cmp"
	"strings"

	"paradox-modding-tools/services/internal/jomini"
)

// Interpolation is a `$key$` (optional `|filter`) reuse inside a loc value.
type Interpolation struct {
	Key      string
	KeyRange Range // inner key, not `$` or `|filter`
}

// Concept is a game concept a data call may name, where its name is written, whether the call marks it as one, and
// the text its link shows where the call writes one. A call marks it with the format E or e after a call of one step
// ([faith|E]), or as a Concept('x', 'text') step, which shows its text; a call of one step with no arguments and any
// other format or none (Vic3's [concept_pop]) names one only where the concepts database defines it, which is the
// caller's to ask.
type Concept struct {
	Name     string
	Span     Range
	IsMarked bool
	Text     string // "" the concept's own name
}

// Segment is one stretch of a localization value under one formatting: where it is in the value, its text, the bold and
// italic marks, the named format (#high), a text icon (@gold!), an interpolated key ($NAME$) or a data call
// ([GetName]); a segment holds one of Text, Icon, Key and Call. IsOpen marks the text "[" of a call no "]" closes: the
// game reads it as text, while completion types inside it.
type Segment struct {
	Range        Range
	Text         string
	Bold, Italic bool
	Format       string
	Icon, Key    string
	Call         string
	IsOpen       bool
}

// formatMark is one format a value has opened and not yet closed with #!.
type formatMark struct {
	bold, italic bool
	format       string
}

// The marks of markup markupAt finds at the start of a value.
const (
	markText   = iota // none: the byte is text
	markBreak         // \n
	markClose         // #!
	markFormat        // #word
	markIcon          // @name!
	markKey           // $name$ or $name|filter$
	markCall          // [ ... ]
	markOpen          // a [ no ] closes
)

// Segments reads a value into its segments: #word opens a format (bold and italic set their marks, any other word is
// a named format, a ;-joined list sets each), #! closes the last one opened, @name! is a text icon, $name$ or
// $name|filter$ an interpolation, [ … ] a data call (nested brackets kept inside) and \n a line break (a segment of
// the text "\n"); a #, @ or $ that opens nothing is text, and so is a [ no ] closes, which the markup after it
// still reads.
func Segments(value string) []Segment {
	var segments []Segment
	var formats []formatMark
	textStart := 0
	put := func(segment Segment) {
		for _, opened := range formats {
			segment.Bold, segment.Italic = segment.Bold || opened.bold, segment.Italic || opened.italic
			segment.Format = cmp.Or(opened.format, segment.Format)
		}
		segments = append(segments, segment)
	}
	for i := 0; i < len(value); {
		mark, length, payload := markupAt(value[i:])
		if mark == markText {
			i++
			continue
		}
		if i > textStart {
			put(Segment{Range: Range{Start: textStart, End: i}, Text: value[textStart:i]})
		}
		span := Range{Start: i, End: i + length}
		switch mark {
		case markBreak:
			put(Segment{Range: span, Text: "\n"})
		case markClose:
			formats = formats[:max(len(formats)-1, 0)]
		case markFormat:
			formats = append(formats, formatOf(payload))
		case markIcon:
			put(Segment{Range: span, Icon: payload})
		case markKey:
			put(Segment{Range: span, Key: payload})
		case markCall:
			put(Segment{Range: span, Call: payload})
		case markOpen:
			put(Segment{Range: span, Text: "[", IsOpen: true})
		}
		i, textStart = span.End, span.End
	}
	if textStart < len(value) {
		put(Segment{Range: Range{Start: textStart, End: len(value)}, Text: value[textStart:]})
	}
	return segments
}

// markupAt reads the markup that starts a text: its mark, its length in bytes and what it carries (the format's
// words, the icon's or key's name, the call's inside).
func markupAt(text string) (mark, length int, payload string) {
	switch text[0] {
	case '\\':
		if strings.HasPrefix(text, `\n`) {
			return markBreak, 2, ""
		}
	case '#':
		if strings.HasPrefix(text, "#!") {
			return markClose, 2, ""
		}
		if end := formatEnd(text[1:]); end > 0 && end < len(text)-1 && text[1+end] == ' ' {
			return markFormat, end + 2, text[1 : 1+end]
		}
	case '@':
		if end := runEnd(text[1:], ""); end > 0 && end < len(text)-1 && text[1+end] == '!' {
			return markIcon, end + 2, text[1 : 1+end]
		}
	case '$':
		if end := runEnd(text[1:], keyExtra); end > 0 {
			if close := strings.IndexByte(text[1+end:], '$'); close >= 0 && (close == 0 || text[1+end] == '|') {
				return markKey, end + close + 2, text[1 : 1+end]
			}
		}
	case '[':
		if end := callEnd(text); end > 0 {
			return markCall, end, text[1 : end-1]
		}
		return markOpen, 1, ""
	}
	return markText, 1, ""
}

// formatEnd is the length of a format's words after its #: names joined by ;, the last one's argument after a colon
// running to the first space outside brackets (#tooltip:[Character.GetTooltipTag],sway_tt opposed to it#!).
func formatEnd(text string) int {
	end := runEnd(text, ";")
	if end == 0 || end >= len(text) || text[end] != ':' {
		return end
	}
	depth := 0
	for i := end; i < len(text); i++ {
		switch text[i] {
		case '[':
			depth++
		case ']':
			depth--
		case ' ':
			if depth == 0 {
				return i
			}
		}
	}
	return len(text)
}

// runEnd is the length of the run of letters, digits and _ a word or a name is made of, with the bytes of extra
// allowed in it too.
func runEnd(text, extra string) int {
	for i := 0; i < len(text); i++ {
		c := text[i]
		inside := c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
		if !inside && strings.IndexByte(extra, c) < 0 {
			return i
		}
	}
	return len(text)
}

// callEnd is the length of the data call a text starting with [ holds, through the ] that closes it, nested brackets
// counted; 0 where none closes it.
func callEnd(text string) int {
	span := jomini.Calls(text, 0)[0]
	call := text[:span.End]
	if strings.Count(call, "[") != strings.Count(call, "]") {
		return 0
	}
	return span.End
}

// formatOf is the mark a #word opens: its bold and italic words set the marks, any other word names the format.
func formatOf(opening string) formatMark {
	var opened formatMark
	for part := range strings.SplitSeq(opening, ";") {
		switch strings.ToLower(part) {
		case "bold":
			opened.bold = true
		case "italic":
			opened.italic = true
		case "":
		default:
			opened.format = part
		}
	}
	return opened
}

// Plain is a value as a player reads it: a key as its name, a concept link as the text it shows (itself read the same
// way), the formatting dropped and any other markup as written.
func Plain(text string) string {
	var builder strings.Builder
	for _, segment := range Segments(text) {
		plain := text[segment.Range.Start:segment.Range.End]
		switch {
		case segment.Key != "":
			plain = segment.Key
		case segment.Call != "":
			if concept, isConcept := markedConcept(text[segment.Range.Start:segment.Range.End]); isConcept {
				plain = Plain(cmp.Or(concept.Text, concept.Name))
			}
		}
		builder.WriteString(plain)
	}
	return builder.String()
}

// markedConcept is the first concept a data call, written with its brackets, marks as one; false for none.
func markedConcept(call string) (Concept, bool) {
	calls := jomini.DataCalls(call, 0)
	if len(calls) == 0 {
		return Concept{}, false
	}
	for _, concept := range Concepts(calls[0], call) {
		if concept.IsMarked {
			return concept, true
		}
	}
	return Concept{}, false
}

// Concepts are the game concepts a data call may name: its one step written with the |E format, or each
// Concept('name', 'text') among its steps; text is the string the call's ranges index.
func Concepts(call jomini.Call, text string) []Concept {
	steps := call.Steps
	if len(steps) == 0 {
		return nil
	}
	var concepts []Concept
	if len(steps) == 1 && len(steps[0].Args) == 0 {
		rest := text[steps[0].Range.End:call.End]
		format := strings.TrimSuffix(strings.TrimLeft(rest, " \t"), "]")
		isMarked := format == "|E" || format == "|e"
		concepts = append(concepts, Concept{Name: steps[0].Name, Span: steps[0].Range, IsMarked: isMarked})
	}
	for _, step := range steps {
		if step.Name == "Concept" && len(step.Args) > 0 && step.Args[0].Quoted {
			arg := step.Args[0]
			span := Range{Start: arg.Range.Start + 1, End: arg.Range.End - 1}
			concept := Concept{Name: arg.Text, Span: span, IsMarked: true}
			if len(step.Args) > 1 && step.Args[1].Quoted {
				concept.Text = step.Args[1].Text
			}
			concepts = append(concepts, concept)
		}
	}
	return concepts
}

// Interpolations finds the `$key$` and `$key|filter$` a loc value writes that may be keys, wherever it writes them: a
// data call's argument and a format's argument name keys too. valueStart is the UTF-8 offset of value[0] in the file
// text.
func Interpolations(value string, valueStart int) []Interpolation {
	var interpolations []Interpolation
	for i := 0; i < len(value); i++ {
		if value[i] != '$' {
			continue
		}
		mark, length, key := markupAt(value[i:])
		if mark != markKey {
			continue
		}
		if LooksLikeKey(key) {
			start := valueStart + i + 1
			interpolations = append(interpolations, Interpolation{
				Key:      key,
				KeyRange: Range{Start: start, End: start + len(key)},
			})
		}
		i += length - 1
	}
	return interpolations
}
