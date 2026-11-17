// texts.go finds the text a value in a GUI file shows, and the sample text
// drawn for a data call in it.

package draw

import (
	"hash/fnv"
	"iter"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/internal/loc"
)

// What a data call in a text writes.
const (
	kindNumber = "number"
	kindText   = "text"
	kindDate   = "date"
)

// minSample is the least number a sample number shows.
const minSample = 1.0

// loremSentences is the sample text drawn for a data call that writes text.
var loremSentences = []string{
	"Lorem ipsum dolor sit amet, consectetur adipiscing elit.",
	"Sed do eiusmod tempor incididunt ut labore et dolore magna aliqua.",
	"Ut enim ad minim veniam, quis nostrud exercitation ullamco laboris.",
	"Duis aute irure dolor in reprehenderit in voluptate velit esse.",
	"Excepteur sint occaecat cupidatat non proident, sunt in culpa qui officia.",
}

// callOutputs says whether a data call in a text writes a number or text, by
// the type its last function returns. A type with no row writes text.
var callOutputs = map[string]string{
	"CString": kindText, "CUTF8String": kindText, "CPdxStringView": kindText,
	"int8": kindNumber, "int16": kindNumber, "int32": kindNumber, "int64": kindNumber,
	"uint8": kindNumber, "uint16": kindNumber, "uint32": kindNumber, "uint64": kindNumber,
	"CFixedPoint": kindNumber, "float": kindNumber, "double": kindNumber,
}

// resolveText returns what a player sees of a text: each $key$ replaced by its
// message's text, and each line break as a new line. A data call stays as it
// is.
func (l *library) resolveText(text string, depth int) []loc.Segment {
	var resolved []loc.Segment
	for _, segment := range loc.Segments(text, 0) {
		if segment.Markup == loc.MarkupCall {
			resolved = append(resolved, segment)

			continue
		}

		if segment.Markup == loc.MarkupKey && depth < maxDepth {
			if message, _, ok := l.ws.LookupText(l.mods, segment.Text); ok {
				resolved = append(resolved, l.resolveText(message, depth+1)...)

				continue
			}
		}

		segment.Markup, segment.Text = loc.MarkupText, loc.ToPlain([]loc.Segment{segment})
		if segment.Text != "" {
			resolved = append(resolved, segment)
		}
	}

	return resolved
}

// openingBrackets are the characters after which a sample needs no space.
const openingBrackets = "([{"

// sampleText returns a text with each data call replaced by its sample. A
// sample gets a space before it unless the text before ends in a space or an
// opening bracket, since the game's own functions bring their spacing.
func (l *library) sampleText(segments []loc.Segment, context context, properties iter.Seq[Property]) []loc.Segment {
	segments = slices.Clone(segments)
	for position, segment := range segments {
		if segment.Markup != loc.MarkupCall {
			continue
		}

		sample, kind := l.sampleCall(segment.Text, context, properties)
		if position > 0 {
			last, _ := utf8.DecodeLastRuneInString(segments[position-1].Text)
			if !unicode.IsSpace(last) && !strings.ContainsRune(openingBrackets, last) {
				sample = " " + sample
			}
		}

		segments[position].Text, segments[position].Kind = sample, kind
	}

	return segments
}

// multilineKey lets a widget's text run over several lines, and resizeKey
// makes a widget as large as what it holds.
const (
	multilineKey = "multiline"
	resizeKey    = "autoresize"
)

// sampleCall returns the sample drawn for a data call and its kind: a number,
// a date or text. How much text fits depends on the given properties of the
// widget that shows it. The call's text seeds the sample, so the same call
// always gets the same one.
func (l *library) sampleCall(source string, context context, properties iter.Seq[Property]) (sample, kind string) {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(source))
	seed := hash.Sum32()
	call, _ := jomini.ParseCall(source, 0)
	path := context.expand(l.ws.Schema, call)
	function, _ := l.ws.Schema.FindFunction(path, "", len(path.Steps)-1)
	kind, known := callOutputs[function.Returns]
	switch {
	case kind == kindNumber:
		return sampleNumber(seed, call.Suffix), kind
	case isDate(function):
		return l.sampleDate(seed), kindDate
	case !known:
		return sampleWords(seed, 2), kindText
	case !isOn(properties, multilineKey):
		return sampleWords(seed, 2+int(seed%2)), kind
	case isOn(properties, resizeKey):
		return sampleSentences(seed, 1+int(seed%2)), kind
	}

	return sampleSentences(seed, len(loremSentences)), kind
}

// The format codes a data call in a text writes after |, as $VALUE|+2%$
// does. A digit among them is the number's decimals.
const (
	percentCode = "%"

	// signCode puts a plus sign before a number above zero.
	signCode = "="
)

// sampleNumber returns a sample number picked by the seed, written as the
// format codes say.
func sampleNumber(seed uint32, codes string) string {
	decimals := 0
	if position := strings.IndexFunc(codes, unicode.IsDigit); position >= 0 {
		decimals = int(codes[position] - '0')
	}

	number := strconv.FormatFloat(minSample+float64(seed%9900)/100, 'f', decimals, 64)
	if strings.Contains(codes, percentCode) {
		number += "%"
	}

	if strings.Contains(codes, signCode) {
		number = "+" + number
	}

	return number
}

// dateCallReturns is what a data call that writes a date returns. No call
// returns the dumps' Date type; a date is written as a CString, so the
// function's name decides.
const dateCallReturns = "CString"

// dateCallEndings are how the name of a function that writes a date ends.
var dateCallEndings = []string{"Date", "DateString"}

// isDate reports whether a data function writes a date.
func isDate(function lang.DataFunction) bool {
	return function.Returns == dateCallReturns && slices.ContainsFunc(dateCallEndings, func(ending string) bool {
		return strings.HasSuffix(function.Name, ending)
	})
}

// dateKey is the localization key of the games' date form, and dateDay,
// dateMonth and dateYear the names it fills in.
const (
	dateKey   = "DATE_FORMATTING_LONG"
	dateDay   = "DAY"
	dateMonth = "MONTH"
	dateYear  = "YEAR"
)

// dateMonths are the localization keys of the months' names, January first.
var dateMonths = []string{
	"CW_DATE_January", "CW_DATE_February", "CW_DATE_March", "CW_DATE_April", "CW_DATE_May", "CW_DATE_June",
	"CW_DATE_July", "CW_DATE_August", "CW_DATE_September", "CW_DATE_October", "CW_DATE_November",
	"CW_DATE_December",
}

// sampleDate returns a sample date picked by the seed, in the game's form and
// the workspace's language.
func (l *library) sampleDate(seed uint32) string {
	month := dateMonths[seed%uint32(len(dateMonths))]
	values := map[string]string{
		dateDay: strconv.Itoa(1 + int(seed%28)), dateMonth: l.lookupText(month),
		dateYear: strconv.Itoa(1000 + int(seed%900)),
	}

	var date strings.Builder
	for _, segment := range loc.Segments(l.lookupText(dateKey), 0) {
		if value, ok := values[segment.Text]; ok && segment.Markup == loc.MarkupKey {
			date.WriteString(value)
		} else {
			date.WriteString(loc.ToPlain([]loc.Segment{segment}))
		}
	}

	return date.String()
}

// lookupText returns the text of a message, or the key itself if there is no
// such message.
func (l *library) lookupText(key string) string {
	if text, _, ok := l.ws.LookupText(l.mods, key); ok {
		return text
	}

	return key
}

// sampleWords returns the first words of the lorem sentence the seed picks.
func sampleWords(seed uint32, count int) string {
	return strings.Join(strings.Fields(sampleSentences(seed, 1))[:count], " ")
}

// sampleSentences returns lorem sentences in order, starting at the one the
// seed picks.
func sampleSentences(seed uint32, count int) string {
	first := int(seed % uint32(len(loremSentences)))
	sentences := make([]string, count)
	for position := range sentences {
		sentences[position] = loremSentences[(first+position)%len(loremSentences)]
	}

	return strings.Join(sentences, " ")
}

// isOn reports whether the given properties write a key as yes.
func isOn(properties iter.Seq[Property], key string) bool {
	return literals[strings.ToLower(findProperty(properties, key).Value)]
}
