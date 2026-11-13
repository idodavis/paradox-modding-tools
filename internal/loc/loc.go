// Package loc reads and writes the games' localization files. It parses a
// file into its messages, decides which language the file is for, splits a
// message's text into its markup and gives the plain text a player sees.
//
// It knows no game and no index: what a key names, and which message of a
// key the game keeps, are the index's. Its spans, line index and problems are
// jomini's, so a localization file and a script file share their offsets.
//
// # The format
//
// A file is read one line at a time, and a line ends at \n, \r\n or a lone
// \r. A line is one of these:
//
//   - A blank line, or a comment that starts with #.
//   - The header: l_, the language in the letters a to z and underscores,
//     and a colon, as in l_english:. It is read in any case, with blanks
//     before and after it and a comment after those. The first header gives
//     the file's language, and a later one is passed over.
//   - A message: a key, a colon, an optional version, blanks and the text in
//     quotes. A key is letters and numbers of any script and the characters
//     _ . ' -, and a version is the digits 0 to 9. The text runs to the last
//     quote on the line, so a quote inside it needs no escape.
//
// A file of two messages, the second with no version:
//
//	l_english:
//	 my_key:0 "My text"
//	 my_quote: "He said "hi" twice"
//
// # The language
//
// A file's language is the first of these that names one, in lower case:
//
//   - the header;
//   - the _l_<language>.yml ending of the file's name, in any case, as in
//     events_l_english.yml;
//   - the first folder after the last localization folder that is not
//     replace, as in localization/replace/english/events.yml.
//
// A localisation folder, spelled the British way, is no localization folder,
// since the games never read it.
//
// # Markup
//
// A message's text is plain text with these markups in it:
//
//   - $key$, another message's text or a value the game fills in. A | after
//     the key gives its format, as in $VALUE|+=0$.
//   - [call], a data call, which may hold brackets of its own.
//   - #format, a text format, which #! ends.
//   - @icon!, an icon.
//   - \n, a line break.
//
// A format's name runs to the first blank outside its calls and keys, and the
// calls and keys inside it are markup of their own:
//
//	#tooltippable;tooltip:[GetPlayer.GetTooltipTag],liberal_tt Liberal#!
//
// # Robustness
//
// Parse never fails. It keeps every message it could read, and reports each
// problem as a jomini.Problem with one of these codes:
//
//   - loc-content-before-header: a line above the header, which is not read.
//   - malformed-line: a line that is no header, comment or message.
//   - loc-unterminated-value: a message with no closing quote, whose text
//     runs to the end of its line.
//
// Segments reads a marker that opens nothing as plain text, as the $ of
// cost $5 or a [ ] with only blanks inside. ListMarkupFaults reports two
// mistakes in the plain text with the code loc-markup: a [ that starts a call
// and is never closed on its line, and a format ended with #. or #, where
// the game ends one with #!.
package loc

import (
	"cmp"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"paradox-modding-tools/internal/jomini"
)

// Message is one message of a localization file.
type Message struct {
	Key  string `json:"key"`
	Text string `json:"text"`

	// Version is the number after the key's colon, or 0.
	Version int `json:"version"`

	// Span runs from the key to the closing quote, or the line's end if none.
	// TextSpan excludes the quotes.
	Span     jomini.Span `json:"span"`
	KeySpan  jomini.Span `json:"keySpan"`
	TextSpan jomini.Span `json:"textSpan"`

	Segments []Segment `json:"segments"`
}

// File is one parsed localization file.
type File struct {
	// Language is in lower case, as english. It is "" if neither the header
	// nor the path names one.
	Language string `json:"language"`

	Messages []Message        `json:"messages"`
	Problems []jomini.Problem `json:"problems"`
	Lines    jomini.Lines     `json:"-"`
}

// Language is one localization language.
type Language struct {
	Folder string `json:"folder"`
	Label  string `json:"label"`

	// Base is set for the game's base language.
	Base bool `json:"base"`
}

// BaseLanguage is the language every other language is measured against.
const BaseLanguage = "english"

// languageSuffix matches a name ending in _l_<language>.yml, in any case.
var languageSuffix = regexp.MustCompile(`(?i)^(.*_l_)([a-z_]+)(\.yml)$`)

// languageLabels holds each language's English name, by folder.
var languageLabels = map[string]string{
	"english": "English", "french": "French", "german": "German", "spanish": "Spanish", "russian": "Russian",
	"korean": "Korean", "simp_chinese": "Simplified Chinese", "polish": "Polish", "turkish": "Turkish",
	"braz_por": "Brazilian Portuguese", "japanese": "Japanese",
}

// Parse parses a localization file's text. It keeps the messages it can read
// and reports the rest as Problems. The language is the header's, else the
// file name's, else the folder's.
func Parse(text, path string) *File {
	file := &File{Lines: jomini.NewLines(text)}
	header := ""

	for line := range file.Lines.CountLines() {
		span := file.Lines.LineSpan(line)
		content := text[span.Start:span.End]

		body := strings.TrimSpace(content)
		if body == "" || strings.HasPrefix(body, "#") {
			continue
		}

		if language, ok := parseHeader(content); ok {
			header = cmp.Or(header, language)

			continue
		}

		message, ok := ParseMessage(content, span.Start)

		switch {
		case header == "":
			file.Problems = append(file.Problems, problem("loc-content-before-header",
				"line is above the header; it is not read", span))

		case !ok:
			file.Problems = append(file.Problems, problem("malformed-line",
				"line is not a header, a comment or a message; it is not read", span))

		// A message with no closing quote ends where its text does.
		case message.Span.End == message.TextSpan.End:
			unclosed := jomini.Span{Start: message.TextSpan.Start - 1, End: span.End}
			file.Messages = append(file.Messages, message)
			file.Problems = append(file.Problems, problem("loc-unterminated-value",
				"message has no closing quote; its text runs to the end of the line", unclosed))

		default:
			file.Messages = append(file.Messages, message)
		}
	}

	file.Language = cmp.Or(header, FindSuffixLanguage(path), FindFolderLanguage(path))

	return file
}

// parseHeader returns the language a header line names, as english for
// l_english:, or false if the line is no header.
func parseHeader(line string) (string, bool) {
	start := skip(line, 0, isBlank)
	end := skip(line, start, isLanguageRune)

	language, ok := strings.CutPrefix(strings.ToLower(line[start:end]), "l_")
	if !ok || language == "" || !strings.HasPrefix(line[end:], ":") {
		return "", false
	}

	// Only blanks and a comment may follow the colon.
	rest := line[skip(line, end+1, isBlank):]
	if rest != "" && rest[0] != '#' {
		return "", false
	}

	return language, true
}

// skip returns the offset of the first rune at or after an offset that the
// test rejects, or the line's length.
func skip(line string, offset int, accept func(rune) bool) int {
	return len(line) - len(strings.TrimLeftFunc(line[offset:], accept))
}

func isBlank(r rune) bool { return r == ' ' || r == '\t' }

func isLanguageRune(r rune) bool { return r == '_' || 'a' <= r && r <= 'z' || 'A' <= r && r <= 'Z' }

// ParseMessage parses one message line with its markup. Spans are offset by
// base, where the line starts in its file. It returns false if the line is no
// message.
func ParseMessage(line string, base int) (Message, bool) {
	start := skip(line, 0, isBlank)
	colon := skip(line, start, isKeyRune)
	if colon == start || !strings.HasPrefix(line[colon:], ":") {
		return Message{}, false
	}

	digits := line[colon+1 : skip(line, colon+1, isDigit)]
	opening := skip(line, colon+1+len(digits), isBlank)
	if !strings.HasPrefix(line[opening:], `"`) {
		return Message{}, false
	}

	// The text runs to the last quote, or to the line's end if that is the
	// opening one.
	closing := strings.LastIndexByte(line, '"')
	if closing == opening {
		closing = len(line)
	}

	version, _ := strconv.Atoi(digits)
	text, textStart := line[opening+1:closing], base+opening+1

	return Message{
		Key:      line[start:colon],
		Text:     text,
		Version:  version,
		Span:     jomini.Span{Start: base + start, End: base + min(closing+1, len(line))},
		KeySpan:  jomini.Span{Start: base + start, End: base + colon},
		TextSpan: jomini.Span{Start: textStart, End: base + closing},
		Segments: Segments(text, textStart),
	}, true
}

func isKeyRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsNumber(r) || strings.ContainsRune("_.'-", r)
}

func isDigit(r rune) bool { return '0' <= r && r <= '9' }

func problem(code, message string, span jomini.Span) jomini.Problem {
	return jomini.Problem{Code: code, Message: message, Span: span}
}

// FindSuffixLanguage returns the language in a file name's _l_<language>.yml
// ending, as english for events_l_english.yml, or "" for none.
func FindSuffixLanguage(filePath string) string {
	match := languageSuffix.FindStringSubmatch(filepath.Base(filePath))
	if match == nil {
		return ""
	}

	return strings.ToLower(match[2])
}

// FindFolderLanguage returns the language folder in a path, or "". A path
// ending in a slash counts its last part.
func FindFolderLanguage(filePath string) string {
	folders := strings.Split(filepath.ToSlash(filePath), "/")
	folders = folders[:len(folders)-1]

	if position, ok := findLanguageFolder(folders); ok {
		return strings.ToLower(folders[position])
	}

	return ""
}

// findLanguageFolder returns the position of the first folder after the last
// localization folder that is not replace, in any case, or false.
func findLanguageFolder(folders []string) (int, bool) {
	root := -1

	for i, folder := range folders {
		if strings.EqualFold(folder, "localization") {
			root = i
		}
	}

	for i := root + 1; root >= 0 && i < len(folders); i++ {
		if !strings.EqualFold(folders[i], "replace") {
			return i, true
		}
	}

	return 0, false
}

// MisspelledFolders maps a folder name the games never read, localisation, to
// the one they do.
var MisspelledFolders = map[string]string{"localisation": "localization"}

// ToLanguageRel returns the path of the file with the same messages in a
// language, setting it in the language folder and the _l_<language>.yml
// ending where the path has them.
func ToLanguageRel(rel, language string) string {
	folders := strings.Split(rel, "/")
	last := len(folders) - 1

	if position, ok := findLanguageFolder(folders[:last]); ok {
		folders[position] = language
	}

	if match := languageSuffix.FindStringSubmatch(folders[last]); match != nil {
		folders[last] = match[1] + language + match[3]
	}

	return strings.Join(folders, "/")
}

// ToLanguageFile returns a file name for a stem and language, as
// events_l_english.yml.
func ToLanguageFile(stem, language string) string {
	return stem + "_" + ToLanguageKey(language) + ".yml"
}

// Encode returns a localization file's text for one language.
func Encode(language string, messages []Message) string {
	return ToLanguageKey(language) + ":\n" + EncodeMessages(messages, "\n")
}

// EncodeMessages returns a line for each message, as ` my_key:0 "My text"`,
// each ended by the given ending.
func EncodeMessages(messages []Message, ending string) string {
	var text strings.Builder

	for _, message := range messages {
		fmt.Fprintf(&text, " %s:%d \"%s\"%s", message.Key, message.Version, message.Text, ending)
	}

	return text.String()
}

// EncodeAddition returns the text that adds messages to the end of a
// localization text, in its own line ending. It starts with one unless the
// text is empty or ends in \n.
func EncodeAddition(text string, messages []Message) string {
	ending := jomini.LineEnding(text)
	if text == "" || strings.HasSuffix(text, "\n") {
		return EncodeMessages(messages, ending)
	}

	return ending + EncodeMessages(messages, ending)
}

// ToLanguageKey returns the key a language's header writes, as l_english for
// english.
func ToLanguageKey(language string) string {
	return "l_" + language
}

// DescribeLanguage returns a language's English name, as Simplified Chinese
// for simp_chinese, or the folder if it is unknown.
func DescribeLanguage(folder string) string {
	if label, ok := languageLabels[folder]; ok {
		return label
	}

	return folder
}

// IsLanguage reports whether a folder holds a language, not something such as
// jomini that the games keep beside them.
func IsLanguage(folder string) bool {
	_, ok := languageLabels[folder]

	return ok
}
