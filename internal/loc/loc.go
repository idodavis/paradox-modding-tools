// Package loc reads and writes localization files. It parses a file into its messages, decides the file's
// language, splits a message's text into its markup, and gives the plain text a player sees.
package loc

import (
	"cmp"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/samber/lo"

	"paradox-modding-tools/internal/jomini"
)

// A Message is one line of a localization file: a key, the text the game shows for it and a version number.
type Message struct {
	Key      string      `json:"key"`
	Text     string      `json:"text"`
	Version  int         `json:"version"`
	Span     jomini.Span `json:"span"`     // the message, from its key to its closing quote
	KeySpan  jomini.Span `json:"keySpan"`  // the key
	TextSpan jomini.Span `json:"textSpan"` // the text, quotes excluded
}

// A File is one parsed localization file.
type File struct {
	Language string         `json:"language"` // "" if neither the header nor the path says
	Messages []Message      `json:"messages"`
	Errors   []jomini.Error `json:"errors"`
	Lines    jomini.Lines   `json:"-"`
}

// A Language is one localization language: the folder the game keeps it in, and its name in English.
type Language struct {
	Folder string `json:"folder"`
	Label  string `json:"label"`
}

var (
	// headerLine matches the `l_<language>:` line that opens a file, with an optional comment after it.
	headerLine = regexp.MustCompile(`(?i)^[ \t]*l_([a-z_]+):[ \t]*(#.*)?$`)
	// messageStart matches a message line up to its opening quote: the key, an optional version and the colon.
	messageStart = regexp.MustCompile(`^[ \t]*([\p{L}\p{N}_.'-]+):(\d*)[ \t]*"`)
	// languageSuffix matches a file name ending in `_l_<language>.yml`, in any case. It captures the part up to and
	// including `_l_`, the language and the `.yml`.
	languageSuffix = regexp.MustCompile(`(?i)^(.*_l_)([a-z_]+)(\.yml)$`)
)

// languageLabels holds each localization language's name in English, by the folder the game keeps it in.
var languageLabels = map[string]string{
	"english": "English", "french": "French", "german": "German", "spanish": "Spanish", "russian": "Russian",
	"korean": "Korean", "simp_chinese": "Simplified Chinese", "polish": "Polish", "turkish": "Turkish",
	"braz_por": "Brazilian Portuguese", "japanese": "Japanese",
}

// Parse parses the text of the localization file at the given path. The language comes from the header, else from the
// file's name, else from its folder, in lower case. It is "" if none of them says. A line above the header and a
// message with no closing quote are errors.
func Parse(text, path string) *File {
	file := &File{Lines: jomini.NewLines(text)}
	language := ""
	for line := range file.Lines.CountLines() {
		span := file.Lines.LineSpan(line)
		content := text[span.Start:span.End]
		body := strings.TrimSpace(content)
		if body == "" || strings.HasPrefix(body, "#") {
			continue
		}
		if match := headerLine.FindStringSubmatch(content); match != nil {
			language = cmp.Or(language, strings.ToLower(match[1]))
			continue
		}
		message, isMessage := ParseMessage(content, span.Start)
		switch {
		case language == "":
			file.Errors = append(file.Errors, jomini.Error{
				Code:    "loc-content-before-header",
				Message: "line is above the header, so it is not read",
				Span:    span,
			})
		case !isMessage:
			file.Errors = append(file.Errors, jomini.Error{
				Code:    "malformed-line",
				Message: "line is not a header, a comment or a message",
				Span:    span,
			})
		default:
			file.Messages = append(file.Messages, message)
			if message.Span.End == message.TextSpan.End {
				file.Errors = append(file.Errors, jomini.Error{
					Code:    "loc-unterminated-value",
					Message: "message has no closing quote",
					Span:    jomini.Span{Start: message.TextSpan.Start - 1, End: span.End},
				})
			}
		}
	}
	file.Language = cmp.Or(language, FindSuffixLanguage(path), FindFolderLanguage(path))
	return file
}

// ParseMessage parses one message line. The given offset is where the line starts in its file. A message with no
// closing quote runs to the end of the line. It returns false if the line is not a message.
func ParseMessage(line string, base int) (Message, bool) {
	match := messageStart.FindStringSubmatchIndex(line)
	if match == nil {
		return Message{}, false
	}
	opening, closing := match[1]-1, strings.LastIndexByte(line, '"')
	if closing == opening {
		closing = len(line)
	}
	version, _ := strconv.Atoi(line[match[4]:match[5]])
	return Message{
		Key:      line[match[2]:match[3]],
		Text:     line[opening+1 : closing],
		Version:  version,
		Span:     jomini.Span{Start: base + match[2], End: base + min(closing+1, len(line))},
		KeySpan:  jomini.Span{Start: base + match[2], End: base + match[3]},
		TextSpan: jomini.Span{Start: base + opening + 1, End: base + closing},
	}, true
}

// ToLanguageRel returns the path, relative to a mod's root, of the file that holds the same messages as the given
// localization file, in the given language.
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

// FindSuffixLanguage returns the language in the `_l_<language>.yml` ending of the given file's name, as in
// `events_l_english.yml`, in lower case. It returns "" if the name has no such ending.
func FindSuffixLanguage(filePath string) string {
	match := languageSuffix.FindStringSubmatch(filepath.Base(filePath))
	if match == nil {
		return ""
	}
	return strings.ToLower(match[2])
}

// FindFolderLanguage returns the language folder in the given file path, in lower case: the first folder under
// `localization` that is not `replace`. A path that ends in a slash names a folder, so its last part counts too. It
// returns "" if the path has none.
func FindFolderLanguage(filePath string) string {
	folders := strings.Split(filepath.ToSlash(filePath), "/")
	folders = folders[:len(folders)-1]
	if position, ok := findLanguageFolder(folders); ok {
		return strings.ToLower(folders[position])
	}
	return ""
}

// findLanguageFolder returns the position of the language folder among the given folders of a path: the first folder
// after the last `localization` that is not `replace`. It returns false if there is none.
func findLanguageFolder(folders []string) (int, bool) {
	lower := lo.Map(folders, func(folder string, _ int) string { return strings.ToLower(folder) })
	root := lo.LastIndexOf(lower, "localization")
	if root < 0 {
		return 0, false
	}
	next := slices.IndexFunc(lower[root+1:], func(folder string) bool { return folder != "replace" })
	if next < 0 {
		return 0, false
	}
	return root + 1 + next, true
}

// Encode returns the text of a localization file for one language: the header line, then its messages.
func Encode(language string, messages []Message) string {
	return ToLanguageKey(language) + ":\n" + EncodeMessages(messages, "\n")
}

// EncodeMessages returns one line for each given message: its key, its version and its quoted text in the layout the
// game reads, ended by the given line ending.
func EncodeMessages(messages []Message, ending string) string {
	var text strings.Builder
	for _, message := range messages {
		fmt.Fprintf(&text, " %s:%d \"%s\"%s", message.Key, message.Version, message.Text, ending)
	}
	return text.String()
}

// EncodeAddition returns the text that adds the messages to the end of the given localization text, in the text's own
// line ending.
func EncodeAddition(text string, messages []Message) string {
	ending := jomini.LineEnding(text)
	if text == "" || strings.HasSuffix(text, "\n") {
		return EncodeMessages(messages, ending)
	}
	return ending + EncodeMessages(messages, ending)
}

// ToLanguageKey returns the key the game writes a language as: its name after l_, as in l_english.
func ToLanguageKey(language string) string {
	return "l_" + language
}

// DescribeLanguage returns a localization language's name in English, from the folder the game keeps it in. It
// returns the folder itself for a language it does not know.
func DescribeLanguage(folder string) string {
	return lo.ValueOr(languageLabels, folder, folder)
}
