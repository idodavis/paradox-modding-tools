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

// Message is one message of a localization file: a key, its version and the
// text the game shows for it, split into its markup.
type Message struct {
	Key  string `json:"key"`
	Text string `json:"text"`

	// Version is the number written after the key's colon, and 0 where none
	// is.
	Version int `json:"version"`

	// Span runs from the key to the closing quote, or to the end of the line
	// where there is none. KeySpan is the key, and TextSpan the text without
	// its quotes.
	Span     jomini.Span `json:"span"`
	KeySpan  jomini.Span `json:"keySpan"`
	TextSpan jomini.Span `json:"textSpan"`

	// Segments is the text split into its markup, with spans in the file.
	Segments []Segment `json:"segments"`
}

// File is one parsed localization file.
type File struct {
	// Language is the language the file is for, in lower case, as english.
	// It is "" where neither the header nor the path names one.
	Language string `json:"language"`

	Messages []Message        `json:"messages"`
	Problems []jomini.Problem `json:"problems"`
	Lines    jomini.Lines     `json:"-"`
}

// Language is one localization language: the folder the game keeps it in,
// and its name in English.
type Language struct {
	Folder string `json:"folder"`
	Label  string `json:"label"`

	// Base is set for the game's base language.
	Base bool `json:"base"`
}

// languageSuffix matches a file name that ends in _l_<language>.yml, in any
// case. It captures the name up to and including the _l_, the language and
// the .yml.
var languageSuffix = regexp.MustCompile(`(?i)^(.*_l_)([a-z_]+)(\.yml)$`)

// languageLabels holds each language's name in English, by the folder the
// game keeps it in.
var languageLabels = map[string]string{
	"english": "English", "french": "French", "german": "German", "spanish": "Spanish", "russian": "Russian",
	"korean": "Korean", "simp_chinese": "Simplified Chinese", "polish": "Polish", "turkish": "Turkish",
	"braz_por": "Brazilian Portuguese", "japanese": "Japanese",
}

// Parse parses the text of the localization file at the given path. A text
// that breaks the format gives the messages Parse could read, and its
// problems go into the file's Problems. The language is the header's, else
// the file name's, else the folder's, and "" where none of them names one.
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

// parseHeader returns the language that a header line names, in lower case,
// as english for l_english:. It returns false if the line is no header.
func parseHeader(line string) (string, bool) {
	start := skip(line, 0, isBlank)
	end := skip(line, start, isLanguageRune)

	language, ok := strings.CutPrefix(strings.ToLower(line[start:end]), "l_")
	if !ok || language == "" || !strings.HasPrefix(line[end:], ":") {
		return "", false
	}

	// Blanks and a comment may follow the colon, and nothing else.
	rest := line[skip(line, end+1, isBlank):]
	if rest != "" && rest[0] != '#' {
		return "", false
	}

	return language, true
}

// skip returns the offset of the first rune of a line, from the given offset
// on, that accept rejects. It returns the line's length if accept takes every
// rune to the end.
func skip(line string, offset int, accept func(rune) bool) int {
	return len(line) - len(strings.TrimLeftFunc(line[offset:], accept))
}

func isBlank(r rune) bool { return r == ' ' || r == '\t' }

// isLanguageRune reports whether a rune may be part of a header's language: a
// letter a to z in either case, or an underscore.
func isLanguageRune(r rune) bool { return r == '_' || 'a' <= r && r <= 'z' || 'A' <= r && r <= 'Z' }

// ParseMessage parses one message line, its text's markup included. The given
// offset is where the line starts in its file, so the spans are the file's. A
// message with no closing quote runs to the end of the line. It returns false
// if the line is no message.
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

	// The text runs to the last quote on the line, or to the line's end where
	// the opening quote is the last.
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

// isKeyRune reports whether a rune may be part of a message's key: a letter
// or a number of any script, or one of _ . ' -.
func isKeyRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsNumber(r) || strings.ContainsRune("_.'-", r)
}

func isDigit(r rune) bool { return '0' <= r && r <= '9' }

// problem returns the problem of the given code over a span of the file, with
// the message that says what is wrong.
func problem(code, message string, span jomini.Span) jomini.Problem {
	return jomini.Problem{Code: code, Message: message, Span: span}
}

// FindSuffixLanguage returns the language in the _l_<language>.yml ending of
// a file's name, in lower case, as english for events_l_english.yml. It
// returns "" if the name has no such ending.
func FindSuffixLanguage(filePath string) string {
	match := languageSuffix.FindStringSubmatch(filepath.Base(filePath))
	if match == nil {
		return ""
	}

	return strings.ToLower(match[2])
}

// FindFolderLanguage returns the language folder in a file's path, in lower
// case. A path that ends in a slash names a folder, so its last part counts
// too. It returns "" if the path has none.
func FindFolderLanguage(filePath string) string {
	folders := strings.Split(filepath.ToSlash(filePath), "/")
	folders = folders[:len(folders)-1]

	if position, ok := findLanguageFolder(folders); ok {
		return strings.ToLower(folders[position])
	}

	return ""
}

// findLanguageFolder returns the position of the language folder among the
// folders of a path: the first after the last localization folder that is
// not replace. Both are matched in any case. It returns false if there is
// none.
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

// MisspelledFolders maps a folder name the games never read to the one they
// read in its place: localisation, spelled the British way, to localization.
var MisspelledFolders = map[string]string{"localisation": "localization"}

// ToLanguageRel returns the path, relative to a mod's root, of the file that
// holds the same messages as a localization file, in the given language. It
// sets the language in the language folder and in the _l_<language>.yml
// ending, where the path has them.
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

// ToLanguageFile returns the file name of a localization file in the given
// language: the given stem, _l_, the language and .yml, as
// events_l_english.yml for the stem events in english.
func ToLanguageFile(stem, language string) string {
	return stem + "_" + ToLanguageKey(language) + ".yml"
}

// Encode returns the text of a localization file for one language: the
// header line, then a line for each message.
func Encode(language string, messages []Message) string {
	return ToLanguageKey(language) + ":\n" + EncodeMessages(messages, "\n")
}

// EncodeMessages returns a line for each message, ended by the given line
// ending: its key, its version and its quoted text, in the layout the game
// reads, as in  my_key:0 "My text".
func EncodeMessages(messages []Message, ending string) string {
	var text strings.Builder

	for _, message := range messages {
		fmt.Fprintf(&text, " %s:%d \"%s\"%s", message.Key, message.Version, message.Text, ending)
	}

	return text.String()
}

// EncodeAddition returns the text that adds the messages at the end of a
// localization text, in the text's own line ending. It starts with a line
// ending unless the text is empty or ends in \n.
func EncodeAddition(text string, messages []Message) string {
	ending := jomini.LineEnding(text)
	if text == "" || strings.HasSuffix(text, "\n") {
		return EncodeMessages(messages, ending)
	}

	return ending + EncodeMessages(messages, ending)
}

// ToLanguageKey returns the key the game writes a language as, l_english for
// english.
func ToLanguageKey(language string) string {
	return "l_" + language
}

// DescribeLanguage returns a language's name in English from the folder the
// game keeps it in, as Simplified Chinese for simp_chinese. It returns the
// folder itself for a language it does not know.
func DescribeLanguage(folder string) string {
	if label, ok := languageLabels[folder]; ok {
		return label
	}

	return folder
}

// IsLanguage reports whether a folder of the game's localization holds a
// language, and is not one such as jomini that the games keep beside the
// languages.
func IsLanguage(folder string) bool {
	_, ok := languageLabels[folder]

	return ok
}
