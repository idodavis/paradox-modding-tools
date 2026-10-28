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

var (
	// headerLine matches the `l_<language>:` line that opens a file, with an optional comment after it.
	headerLine = regexp.MustCompile(`(?i)^[ \t]*l_([a-z_]+):[ \t]*(#.*)?$`)
	// messageStart matches a message line up to its opening quote: the key, an optional version and the colon.
	messageStart = regexp.MustCompile(`^[ \t]*([\p{L}\p{N}_.'-]+):(\d*)[ \t]*"`)
	// languageSuffix matches a file name that ends in `_l_<language>.yml`, without case. It captures what comes before
	// the language, the language, which is letters and underscores, and the ending.
	languageSuffix = regexp.MustCompile(`(?i)^(.*_l_)([a-z_]+)(\.yml)$`)
)

// Parse parses the text of a localization file. It takes the language from the `l_<language>:` header, else from the
// `_l_<language>` ending of the file's name, else from its folder, all without case. The language is "" if none of
// them says. A line above the header is an error. A message with no closing quote is an error, and its text runs to
// the end of the line.
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
	file.Language = cmp.Or(language, FindSuffixLanguage(path), findFolderLanguage(path))
	return file
}

// ParseMessage parses one message line. The offset is where the line's first byte sits in its file, so the spans of
// the message come out in the file's offsets. A message with no closing quote has its text to the end of the line,
// and its span ends there too. It returns false if the line is not a message.
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
// localization file in the given language. It sets the language in the file's folder and in the end of its name,
// where the file has them.
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

// FindSuffixLanguage returns the language that the name of the given file ends in before `.yml`, as in
// `events_l_english.yml`, without case. It returns "" if the name is not a `.yml` file or has no such suffix.
func FindSuffixLanguage(filePath string) string {
	match := languageSuffix.FindStringSubmatch(filepath.Base(filePath))
	if match == nil {
		return ""
	}
	return strings.ToLower(match[2])
}

// findFolderLanguage returns the name of the folder under `localization` that holds the file, whether a `replace`
// folder comes before it or after it, without case. It returns "" if the file has no such folder.
func findFolderLanguage(filePath string) string {
	folders := strings.Split(filepath.ToSlash(filePath), "/")
	folders = folders[:len(folders)-1]
	if position, ok := findLanguageFolder(folders); ok {
		return strings.ToLower(folders[position])
	}
	return ""
}

// findLanguageFolder returns the position of the folder that names the language among the given folders of a path: the
// first under the last `localization` folder that is not a `replace` folder, whatever their case. It returns false
// if there is none.
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

// ToLanguageKey returns the word the game writes a language as: its name after l_, as in l_english.
func ToLanguageKey(language string) string {
	return "l_" + language
}
