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

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/loc"
	"paradox-modding-tools/internal/schema"
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

// sampleCall returns the sample text of a data call and its kind. The same call always shows the same sample.
func (library *library) sampleCall(source string, context context, properties iter.Seq[Property]) (sample, kind string) {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(source))
	seed := hash.Sum32()
	call, _ := jomini.ParseCall(source, 0)
	path := context.expand(library.ws.Schema, call)
	function, _ := library.ws.Schema.FindFunction(path, "", len(path.Steps)-1)
	kind, known := game.GUI.CallKinds[function.Returns]
	switch {
	case kind == kindNumber:
		return sampleNumber(seed, call.Suffix), kind
	case isDate(function):
		return library.sampleDate(seed), kindDate
	case !known:
		return sampleWords(seed, 2), kindText
	case !isOn(properties, game.GUI.MultilineKey):
		return sampleWords(seed, 2+int(seed%2)), kind
	case isOn(properties, game.GUI.ResizeKey):
		return sampleSentences(seed, 1+int(seed%2)), kind
	}
	return sampleSentences(seed, len(loremSentences)), kind
}

// sampleNumber returns a sample number, picked by a seed and shaped by format codes.
func sampleNumber(seed uint32, codes string) string {
	decimals := 0
	if position := strings.IndexFunc(codes, unicode.IsDigit); position >= 0 {
		decimals = int(codes[position] - '0')
	}
	number := strconv.FormatFloat(minSample+float64(seed%9900)/100, 'f', decimals, 64)
	if strings.Contains(codes, game.GUI.PercentCode) {
		number += "%"
	}
	if strings.Contains(codes, game.GUI.SignCode) {
		number = "+" + number
	}
	return number
}

// isDate reports whether a data function writes a date, as game.GUI.DateCalls says.
func isDate(function schema.DataFunction) bool {
	calls := game.GUI.DateCalls
	return function.Returns == calls.Returns && slices.ContainsFunc(calls.Endings, func(ending string) bool {
		return strings.HasSuffix(function.Name, ending)
	})
}

// sampleDate returns a sample date, picked by a seed, in the game's form and the workspace's language.
func (library *library) sampleDate(seed uint32) string {
	form := game.GUI.DateForm
	month := form.Months[seed%uint32(len(form.Months))]
	values := map[string]string{
		form.Day: strconv.Itoa(1 + int(seed%28)), form.Month: library.lookupText(month),
		form.Year: strconv.Itoa(1000 + int(seed%900)),
	}
	var date strings.Builder
	for _, segment := range loc.Segments(library.lookupText(form.Key), 0) {
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
	return game.GUI.Literals[strings.ToLower(findProperty(properties, key).Value)]
}
