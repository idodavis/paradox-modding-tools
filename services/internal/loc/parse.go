// parse.go reads a localization file into its header's language, its entries and its errors, decides the language a
// file is in, and writes the bytes of a new file.

// Package loc reads and writes Paradox's localization "YAML" dialect (not real YAML) and the markup inside a value. It
// is game-agnostic. Offsets are UTF-8 byte indices into the original text (a leading BOM is skipped, not stripped).
package loc

import (
	"cmp"
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"

	"paradox-modding-tools/services/internal/jomini"
)

// Range is a half-open span of UTF-8 byte offsets.
type Range = jomini.Range

// Entry is one `key:version "value"` localization entry. Version is -1 when absent.
// Value is the verbatim text between the opening quote and the LAST quote on the line.
type Entry struct {
	Key        string
	KeyRange   Range
	Version    int
	Value      string
	ValueRange Range
	Line       int // 0-based
}

// ErrorCode is a parse error's code, the same type as script's.
type ErrorCode = jomini.ErrorCode

// Error is a parse error, the same type as script's.
type Error = jomini.ParseError

// Result is a parsed localization file.
type Result struct {
	Language string // "english" from `l_english:`, as written; "" if none found
	Entries  []Entry
	Errors   []Error
	lines    *jomini.LineIndex
}

// The codes loc's own errors carry; a parse error is otherwise script's (jomini.ParseError).
const (
	ErrNoHeader            ErrorCode = "no-header"
	ErrBadEntry            ErrorCode = "bad-entry"
	ErrTabIndent           ErrorCode = "tab-indent"
	ErrUnterminatedValue   ErrorCode = "unterminated-value"
	ErrContentBeforeHeader ErrorCode = "content-before-header"
)

// keyExtra is the bytes besides letters, digits and _ that a key holds.
const keyExtra = ".-'"

// headerRe matches a header line: optional indent, `l_<language>:`, then only trailing whitespace/comment.
var headerRe = regexp.MustCompile(`^[ \t]*l_([A-Za-z_]+):[ \t]*(#.*)?$`)

// filenameLangRe matches the language between a localization file name's last `l_` and its extension:
// `events_l_english.yml` is "english", `events_l_simp_chinese.yml` is "simp_chinese".
var filenameLangRe = regexp.MustCompile(`(?i)^(?:.*_)?l_([a-z_]+)\.yml$`)

// folderRe matches the language folder a localization path is in: `localization/english/events.yml` is "english".
var folderRe = regexp.MustCompile(`(?i)(?:^|/)localization/([^/]+)/`)

// Parse parses localization text. A leading UTF-8 BOM is skipped; ranges stay in the original string. Never panics.
func Parse(text string) Result {
	result := Result{lines: jomini.NewLineIndex(text)}
	for number := range result.lines.LineCount() {
		start, end := result.lines.OffsetAt(number, 0), result.lines.OffsetAt(number+1, 0)
		if number == 0 && strings.HasPrefix(text, jomini.BOM) {
			start = len(jomini.BOM)
		}
		result.addLine(number, start, strings.TrimRight(text[start:end], "\r\n"))
	}
	if result.Language == "" {
		result.Errors = append(result.Errors, Error{
			Code:    ErrNoHeader,
			Message: "No localization header line (e.g. `l_english:`) found.",
		})
	}
	return result
}

// Lines returns the line index Parse built over the text, by which every position in the result is counted.
func (result *Result) Lines() *jomini.LineIndex { return result.lines }

// addLine takes the line numbered number, which starts at byte offset base: blank and comment lines hold nothing, a
// header names the language, any other line is an entry, and one before the header is an error.
func (result *Result) addLine(number, base int, line string) {
	indent := len(line) - len(strings.TrimLeft(line, " \t"))
	if indent == len(line) || line[indent] == '#' {
		return
	}
	if strings.Contains(line, "l_") { // the cheap test before the pattern
		if match := headerRe.FindStringSubmatch(line); match != nil {
			result.Language = cmp.Or(result.Language, match[1]) // a later header-looking line is ignored as content
			return
		}
	}
	if result.Language == "" {
		result.Errors = append(result.Errors, Error{
			Code:    ErrContentBeforeHeader,
			Message: "Content appears before the localization header (e.g. `l_english:`).",
			Range:   Range{Start: base + indent, End: base + len(line)},
		})
		return
	}
	if strings.Contains(line[:indent], "\t") {
		result.Errors = append(result.Errors, Error{
			Code:    ErrTabIndent,
			Message: "Tabs are not allowed for indentation in localization files.",
			Range:   Range{Start: base, End: base + indent},
		})
	}
	result.addEntry(number, base, line, indent)
}

// addEntry reads `key:version "value"` from a line past its indent: the value runs to the last quote on the line, or
// to the line's end when that quote opens it; a line that is no entry is an error.
func (result *Result) addEntry(number, base int, line string, indent int) {
	text, start := line[indent:], base+indent
	keyEnd := runEnd(text, keyExtra)
	rest, hasColon := strings.CutPrefix(text[keyEnd:], ":")
	if keyEnd == 0 || !hasColon {
		result.badEntry(base, line)
		return
	}
	version, digits := -1, len(rest)-len(strings.TrimLeft(rest, "0123456789"))
	if digits > 0 {
		version, _ = strconv.Atoi(rest[:digits])
	}
	rest = strings.TrimLeft(rest[digits:], " \t")
	if !strings.HasPrefix(rest, `"`) {
		result.badEntry(base, line)
		return
	}
	quoteOpen, quoteClose := len(text)-len(rest), strings.LastIndexByte(text, '"')
	if quoteClose == quoteOpen {
		result.Errors = append(result.Errors, Error{
			Code:    ErrUnterminatedValue,
			Message: "Unterminated localization value (missing closing quote).",
			Range:   Range{Start: start + quoteOpen, End: base + len(line)},
		})
		quoteClose = len(text)
	}
	result.Entries = append(result.Entries, Entry{
		Key:        text[:keyEnd],
		KeyRange:   Range{Start: start, End: start + keyEnd},
		Version:    version,
		Value:      text[quoteOpen+1 : quoteClose],
		ValueRange: Range{Start: start + quoteOpen + 1, End: start + quoteClose},
		Line:       number,
	})
}

// badEntry records a malformed-entry error over the whole line, which starts at byte offset base.
func (result *Result) badEntry(base int, line string) {
	result.Errors = append(result.Errors, Error{
		Code:    ErrBadEntry,
		Message: `Malformed localization entry; expected ` + "`key: \"value\"`" + `.`,
		Range:   Range{Start: base, End: base + len(line)},
	})
}

// Language decides the language a localization file is in, in lower case: its header's, else the one its file name
// ends with, else the one its folder names. A header is "" where the caller has none.
func Language(header, rel string) string {
	if header != "" {
		return strings.ToLower(header)
	}
	if match := filenameLangRe.FindStringSubmatch(path.Base(rel)); match != nil {
		return strings.ToLower(match[1])
	}
	if match := folderRe.FindStringSubmatch(rel); match != nil {
		return strings.ToLower(match[1])
	}
	return ""
}

// File writes the bytes of a localization file in a language: the byte order mark the games want, the header that
// names the language, then each entry as ` key:version "value"`, a version it lacks written as 0.
func File(language string, entries []Entry) []byte {
	var file strings.Builder
	file.WriteString(jomini.BOM + "l_" + language + ":\n")
	for _, entry := range entries {
		fmt.Fprintf(&file, " %s:%d \"%s\"\n", entry.Key, max(entry.Version, 0), entry.Value)
	}
	return []byte(file.String())
}

// LooksLikeKey reports whether name may be a loc key: a run of key characters that is no keyword. A key completed at
// call time (`evt_$TYPE$_desc`) holds a $, so it is never demanded.
func LooksLikeKey(name string) bool {
	switch strings.ToLower(name) {
	case "yes", "no", "none", "root", "prev", "this", "from":
		return false
	}
	return name != "" && runEnd(name, keyExtra) == len(name)
}
