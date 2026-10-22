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

// A Message is one localized message: its key, the text between the quotes, its version number and where it is
// written.
type Message struct {
	Key      string      `json:"key"`
	Text     string      `json:"text"`
	Version  int         `json:"version"`
	Span     jomini.Span `json:"span"`     // the message, from its key to its closing quote
	KeySpan  jomini.Span `json:"keySpan"`  // the key
	TextSpan jomini.Span `json:"textSpan"` // the text, quotes excluded
}

// A File is one parsed localization file: its language, its messages in order, its parse errors and its line
// index.
type File struct {
	Language string         `json:"language"`
	Messages []Message      `json:"messages"`
	Errors   []jomini.Error `json:"errors"`
	Lines    jomini.Lines   `json:"-"`
}

var (
	// headerLine matches the `l_<language>:` line that opens a file, with an optional comment after it.
	headerLine = regexp.MustCompile(`(?i)^[ \t]*l_([a-z_]+):[ \t]*(#.*)?$`)
	// messageStart matches a message line up to its opening quote: the key, an optional version and the colon.
	messageStart = regexp.MustCompile(`^[ \t]*([\p{L}\p{N}_.'-]+):(\d*)[ \t]*"`)
)

// Parse reads the text of a localization file. The second argument is the file's path, which decides the language
// when the text does not: the `l_<language>:` header is checked first, then the `_l_<language>` suffix of the file
// name, then the folder's name, all without case. Language is "" if none of them says.
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
		} else if message, ok := parseMessage(content, span.Start); ok {
			file.Messages = append(file.Messages, message)
		} else {
			file.Errors = append(file.Errors, jomini.Error{
				Code:    "malformed-line",
				Message: "line is not a header, a comment or a message",
				Span:    span,
			})
		}
	}
	file.Language = cmp.Or(language, findSuffixLanguage(path), findFolderLanguage(path))
	return file
}

// parseMessage reads one message line. The second argument is the offset of the line's first byte in its file. It
// returns false if the line is not a message.
func parseMessage(line string, base int) (Message, bool) {
	match := messageStart.FindStringSubmatchIndex(line)
	if match == nil {
		return Message{}, false
	}
	opening, closing := match[1]-1, strings.LastIndexByte(line, '"')
	if closing <= opening {
		return Message{}, false
	}
	version, _ := strconv.Atoi(line[match[4]:match[5]])
	return Message{
		Key:      line[match[2]:match[3]],
		Text:     line[opening+1 : closing],
		Version:  version,
		Span:     jomini.Span{Start: base + match[2], End: base + closing + 1},
		KeySpan:  jomini.Span{Start: base + match[2], End: base + match[3]},
		TextSpan: jomini.Span{Start: base + opening + 1, End: base + closing},
	}, true
}

// findSuffixLanguage returns the language a file name ends in before `.yml`, as in `events_l_english.yml`. It
// returns "" if the name is not a `.yml` file or has no such suffix.
func findSuffixLanguage(filePath string) string {
	stem, ok := strings.CutSuffix(strings.ToLower(filepath.Base(filePath)), ".yml")
	suffix := strings.LastIndex(stem, "_l_")
	if !ok || suffix < 0 {
		return ""
	}
	return stem[suffix+len("_l_"):]
}

// findFolderLanguage returns the name of the folder under `localization` or `localisation` that holds the file,
// whether a `replace` folder comes before it or after it. It returns "" if the file has no such folder.
func findFolderLanguage(filePath string) string {
	folders := strings.Split(strings.ToLower(filepath.ToSlash(filePath)), "/")
	folders = folders[:len(folders)-1]
	root := max(lo.LastIndexOf(folders, "localization"), lo.LastIndexOf(folders, "localisation"))
	if root < 0 {
		return ""
	}
	folders = slices.DeleteFunc(folders[root+1:], func(folder string) bool { return folder == "replace" })
	if len(folders) == 0 {
		return ""
	}
	return folders[0]
}

// Encode returns the text of a localization file for one language: the header line, then each message as its
// key, its version and its quoted text, in the form the game reads.
func Encode(language string, messages []Message) string {
	var text strings.Builder
	fmt.Fprintf(&text, "l_%s:\n", language)
	for _, message := range messages {
		fmt.Fprintf(&text, " %s:%d \"%s\"\n", message.Key, message.Version, message.Text)
	}
	return text.String()
}
