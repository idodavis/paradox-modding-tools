// texts.go finds the text a value in a GUI file shows, and the sample text a data call in it shows.

package draw

import (
	"hash/fnv"
	"iter"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/samber/lo"

	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/internal/loc"
	"paradox-modding-tools/services/workspace"
)

// loremSentences is the sample text of a data call that shows text, one sentence each.
var loremSentences = []string{
	"Lorem ipsum dolor sit amet, consectetur adipiscing elit.",
	"Sed do eiusmod tempor incididunt ut labore et dolore magna aliqua.",
	"Ut enim ad minim veniam, quis nostrud exercitation ullamco laboris.",
	"Duis aute irure dolor in reprehenderit in voluptate velit esse.",
	"Excepteur sint occaecat cupidatat non proident, sunt in culpa qui officia.",
}

// callOutputs is what a data call in a text writes, number or text, by its
// last function's return type. A type with no row writes text.
var callOutputs = map[string]string{
	// the dumps' Return type of the last function of every data call in the
	// three installs' GUI texts; CPdxStringView is EU5's alone and int64
	// Vic3's. The four short integer types are the dumps' too, which a mod's
	// text may end a call in.
	"CString": kindText, "CUTF8String": kindText, "CPdxStringView": kindText,
	"int8": kindNumber, "int16": kindNumber, "int32": kindNumber, "int64": kindNumber,
	"uint8": kindNumber, "uint16": kindNumber, "uint32": kindNumber, "uint64": kindNumber,
	"CFixedPoint": kindNumber, "float": kindNumber, "double": kindNumber,
}

// resolveSegments returns what a player reads in a text: each $key$ as its message and each break as a newline.
func resolveSegments(ws *workspace.Session, mods *index.Index, segments []loc.Segment, depth int) []loc.Segment {
	return lo.FlatMap(segments, func(segment loc.Segment, _ int) []loc.Segment {
		switch segment.Markup {
		case loc.MarkupCall:
			return []loc.Segment{segment}
		case loc.MarkupKey:
			if text, _, ok := ws.LookupText(mods, segment.Text); ok && depth < maxDepth {
				return resolveSegments(ws, mods, loc.Segments(text, 0), depth+1)
			}
		}
		segment.Markup, segment.Text = loc.MarkupText, loc.ToPlain([]loc.Segment{segment})
		return lo.Ternary(segment.Text == "", nil, []loc.Segment{segment})
	})
}

// sampleText returns a text's segments with each data call replaced by its sample.
func (library *library) sampleText(segments []loc.Segment, context context, properties iter.Seq[Property]) []loc.Segment {
	segments = slices.Clone(segments)
	for position, segment := range segments {
		if segment.Markup != loc.MarkupCall {
			continue
		}
		sample, kind := library.sampleCall(segment.Text, context, properties)
		if position > 0 {
			last, _ := utf8.DecodeLastRuneInString(segments[position-1].Text)
			sample = lo.Ternary(unicode.IsSpace(last) || strings.ContainsRune(openingBrackets, last), "", " ") + sample
		}
		segments[position].Text, segments[position].Kind = sample, kind
	}
	return segments
}

// multilineKey is the key whose yes lets a widget's text run over several
// lines, and resizeKey the key whose yes resizes a widget to fit what it
// holds.
const (
	// the wiki's Interface page
	multilineKey = "multiline"

	// the wiki's Interface page
	resizeKey = "autoresize"
)

// sampleCall returns the sample text of a data call and its kind. The same call always shows the same sample.
func (library *library) sampleCall(source string, context context, properties iter.Seq[Property]) (sample, kind string) {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(source))
	seed := hash.Sum32()
	call, _ := jomini.ParseCall(source, 0)
	path := context.expand(library.ws.Schema, call)
	function, _ := library.ws.Schema.FindFunction(path, "", len(path.Steps)-1)
	kind, known := callOutputs[function.Returns]
	switch {
	case kind == kindNumber:
		return sampleNumber(seed, call.Suffix), kind
	case isDate(function):
		return library.sampleDate(seed), kindDate
	case !known:
		return sampleWords(seed, 2), kindText
	case !isOn(properties, multilineKey):
		return sampleWords(seed, 2+int(seed%2)), kind
	case isOn(properties, resizeKey):
		return sampleSentences(seed, 1+int(seed%2)), kind
	}
	return sampleSentences(seed, len(loremSentences)), kind
}

// each game's English localization: CK3 core_l_english.yml:493
// $VALUE|V0%$, :830 $VALUE|V2$, and :831 its own plus before $VALUE|2$;
// Vic3 interfaces_l_english.yml:20 $VALUE|%0$ and :2040 $VALUE|v1$; EU5
// diplomacy_l_english.yml:380-383 $VALUE|+=2$, $VALUE|+2%$ and $VALUE|=$
const (
	// percentCode is the format code a data call in a text writes after | for
	// a percent. A digit among the codes is the number's decimals.
	percentCode = "%"

	// signCode is the format code a data call in a text writes after | for a
	// plus sign before a number above zero.
	signCode = "="
)

// sampleNumber returns a sample number, picked by a seed and shaped by format codes.
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

// dateCallReturns is the type the last function of a data call that writes a
// date returns.
//
// no data call in the three installs' GUI texts returns the dumps' Date
// type; the calls that write a date return CString, such as EU5's
// GetEndDate and GetDateString, so a name with one of these endings is
// taken to write one
const dateCallReturns = "CString"

// dateCallEndings are the endings of the name of that function.
var dateCallEndings = []string{"Date", "DateString"}

// isDate reports whether a data function writes a date, as dateCallReturns
// and dateCallEndings say.
func isDate(function lang.DataFunction) bool {
	return function.Returns == dateCallReturns && slices.ContainsFunc(dateCallEndings, func(ending string) bool {
		return strings.HasSuffix(function.Name, ending)
	})
}

// dateKey is the localization key of the form the games write a date in, and
// dateDay, dateMonth and dateYear the names the form fills in.
//
// jomini/localization/jomini/date_l_english.yml:4 and
// clausewitz/localization/cw_date_l_english.yml, the same in the three
// installs (EU5's under loading_screen/)
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

// sampleDate returns a sample date, picked by a seed, in the game's form and the workspace's language.
func (library *library) sampleDate(seed uint32) string {
	month := dateMonths[seed%uint32(len(dateMonths))]
	values := map[string]string{
		dateDay: strconv.Itoa(1 + int(seed%28)), dateMonth: library.lookupText(month),
		dateYear: strconv.Itoa(1000 + int(seed%900)),
	}
	var date strings.Builder
	for _, segment := range loc.Segments(library.lookupText(dateKey), 0) {
		if value, ok := values[segment.Text]; ok && segment.Markup == loc.MarkupKey {
			date.WriteString(value)
		} else {
			date.WriteString(loc.ToPlain([]loc.Segment{segment}))
		}
	}
	return date.String()
}

// lookupText returns the text of the message with the given key, or the key if there is no such message.
func (library *library) lookupText(key string) string {
	text, _, ok := library.ws.LookupText(library.mods, key)
	return lo.Ternary(ok, text, key)
}

// sampleWords returns the given number of first words of the lorem sentence the seed picks.
func sampleWords(seed uint32, count int) string {
	return strings.Join(strings.Fields(sampleSentences(seed, 1))[:count], " ")
}

// sampleSentences returns the given count of lorem sentences in order, starting at the one the seed picks.
func sampleSentences(seed uint32, count int) string {
	first := int(seed % uint32(len(loremSentences)))
	sentences := make([]string, count)
	for position := range sentences {
		sentences[position] = loremSentences[(first+position)%len(loremSentences)]
	}
	return strings.Join(sentences, " ")
}

// isOn reports whether the given properties of a widget write the given key as yes.
func isOn(properties iter.Seq[Property], key string) bool {
	return literals[strings.ToLower(findProperty(properties, key).Value)]
}
