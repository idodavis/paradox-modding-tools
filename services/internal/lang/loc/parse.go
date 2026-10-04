// Package loc parses Paradox's localization "YAML" dialect (not real YAML). It is game-agnostic.
// Offsets are UTF-8 byte indices into the original text (a leading BOM is skipped, not stripped).
//
// parse.go is a line-by-line, never-panicking scanner emitting entries and typed errors; it
// also derives a file's language from its name, path or header.
package loc

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"paradox-modding-tools/services/internal/lang/jomini"
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

// Interp is a `$key$` (optional `|filter`) reuse inside a loc value.
type Interp struct {
	Key      string
	KeyRange Range // inner key, not `$` or `|filter`
}

// A parse error is the same as in script (code, message, range); the codes below are loc's own.
type (
	ErrorCode = jomini.ErrorCode
	Error     = jomini.ParseError
)

const (
	ErrNoHeader            ErrorCode = "no-header"
	ErrBadEntry            ErrorCode = "bad-entry"
	ErrTabIndent           ErrorCode = "tab-indent"
	ErrUnterminatedValue   ErrorCode = "unterminated-value"
	ErrContentBeforeHeader ErrorCode = "content-before-header"
)

// Result is the parsed localization file.
type Result struct {
	Language string // "english" from `l_english:`; "" if none found
	Entries  []Entry
	Errors   []Error
	HadBOM   bool
}

// headerRe matches a header line: optional indent, `l_<name>:`, then only trailing
// whitespace/comment.
var headerRe = regexp.MustCompile(`^[ \t]*l_([A-Za-z_]+):[ \t]*(#.*)?$`)

// isKeyChar reports the byte classes allowed in a loc key: letters, digits, `_ . - '`.
func isKeyChar(char byte) bool {
	switch {
	case char >= 'a' && char <= 'z', char >= 'A' && char <= 'Z', char >= '0' && char <= '9':
		return true
	case char == '_' || char == '.' || char == '-' || char == '\'':
		return true
	default:
		return false
	}
}

// Parse parses localization text. A leading UTF-8 BOM is skipped; ranges stay
// in the original string. Never panics.
func Parse(text string) Result {
	scanner := &scanner{}
	length := len(text)
	lineStart := 0
	for lineStart <= length {
		lineEnd := lineStart
		for lineEnd < length && text[lineEnd] != '\n' && text[lineEnd] != '\r' {
			lineEnd++
		}
		nextStart := lineEnd
		if nextStart < length {
			if text[nextStart] == '\r' && nextStart+1 < length && text[nextStart+1] == '\n' {
				nextStart += 2
			} else {
				nextStart++
			}
		} else {
			nextStart = length + 1
		}
		scanner.processLine(text[lineStart:lineEnd], lineStart)
		lineStart = nextStart
		scanner.lineNo++
		if lineStart > length {
			break
		}
	}

	if !scanner.headerFound {
		scanner.errs = append(scanner.errs, Error{
			Code:    ErrNoHeader,
			Message: "No localization header line (e.g. `l_english:`) found.",
			Range:   Range{Start: 0, End: 0},
		})
	}

	return Result{Language: scanner.language, Entries: scanner.entries, Errors: scanner.errs, HadBOM: scanner.hadBOM}
}

// Interps finds `$key$` / `$key|filter$` interpolations in a loc value.
// valueStart is the UTF-8 offset of value[0] in the file text.
func Interps(value string, valueStart int) []Interp {
	var out []Interp
	for i := 0; i < len(value); {
		if value[i] != '$' {
			i++
			continue
		}
		j := i + 1
		for j < len(value) && isKeyChar(value[j]) {
			j++
		}
		if j == i+1 {
			i++
			continue
		}
		closeAt := j
		filter := ""
		if closeAt < len(value) && value[closeAt] == '|' {
			filterStart := closeAt + 1
			closeAt++
			for closeAt < len(value) && value[closeAt] != '$' {
				closeAt++
			}
			if closeAt < len(value) {
				filter = value[filterStart:closeAt]
			}
		}
		if closeAt >= len(value) || value[closeAt] != '$' {
			i++
			continue
		}
		key := value[i+1 : j]
		if LooksLikeKey(key) || (strings.EqualFold(key, "VALUE") && filter != "") {
			out = append(out, Interp{Key: key, KeyRange: Range{Start: valueStart + i + 1, End: valueStart + j}})
		}
		i = closeAt + 1
	}
	return out
}

// scanner holds accumulators across the line-by-line parse.
type scanner struct {
	entries     []Entry
	errs        []Error
	language    string
	headerFound bool
	hadBOM      bool
	lineNo      int
}

// processLine handles one line; base is the byte offset in text of line[0].
func (scanner *scanner) processLine(line string, base int) {
	if scanner.lineNo == 0 && strings.HasPrefix(line, "\ufeff") {
		scanner.hadBOM = true
		line = line[len("\ufeff"):]
		base += len("\ufeff")
	}
	i := 0
	sawTab := false
	for i < len(line) {
		if line[i] == ' ' {
			i++
		} else if line[i] == '\t' {
			sawTab = true
			i++
		} else {
			break
		}
	}
	contentStart := i
	if contentStart >= len(line) || line[contentStart] == '#' {
		return // blank or comment-only
	}

	if !scanner.headerFound {
		if match := headerRe.FindStringSubmatch(line); strings.Contains(line, "l_") && match != nil {
			scanner.headerFound, scanner.language = true, match[1]
			return
		}
		scanner.errs = append(scanner.errs, Error{
			Code:    ErrContentBeforeHeader,
			Message: "Content appears before the localization header (e.g. `l_english:`).",
			Range:   Range{Start: base + contentStart, End: base + len(line)},
		})
		return
	}

	// A later header-looking line is ignored as content.
	if strings.Contains(line, "l_") && headerRe.MatchString(line) {
		return
	}

	if sawTab {
		scanner.errs = append(scanner.errs, Error{
			Code:    ErrTabIndent,
			Message: "Tabs are not allowed for indentation in localization files.",
			Range:   Range{Start: base, End: base + contentStart},
		})
		// Best-effort: keep parsing the entry.
	}

	scanner.parseEntry(line[contentStart:], base+contentStart, line, base)
}

// parseEntry parses `key(:version)? "value"`; entryBase is the offset of entry[0].
func (scanner *scanner) parseEntry(entry string, entryBase int, fullLine string, lineBase int) {
	j := 0
	for j < len(entry) && isKeyChar(entry[j]) {
		j++
	}
	if j == 0 {
		scanner.badEntry(fullLine, lineBase)
		return
	}
	key := entry[:j]
	keyRange := Range{Start: entryBase, End: entryBase + j}

	if j >= len(entry) || entry[j] != ':' {
		scanner.badEntry(fullLine, lineBase)
		return
	}
	j++ // consume ':'

	version := -1
	verStart := j
	for j < len(entry) && entry[j] >= '0' && entry[j] <= '9' {
		j++
	}
	if j > verStart {
		version, _ = strconv.Atoi(entry[verStart:j])
	}

	for j < len(entry) && (entry[j] == ' ' || entry[j] == '\t') {
		j++
	}

	if j >= len(entry) || entry[j] != '"' {
		scanner.badEntry(fullLine, lineBase)
		return
	}
	quoteOpen := j
	valueInnerStart := entryBase + quoteOpen + 1

	// an unterminated value runs to the end of the line
	quoteClose := strings.LastIndexByte(entry, '"')
	if quoteClose == quoteOpen {
		scanner.errs = append(scanner.errs, Error{
			Code:    ErrUnterminatedValue,
			Message: "Unterminated localization value (missing closing quote).",
			Range:   Range{Start: entryBase + quoteOpen, End: lineBase + len(fullLine)},
		})
		quoteClose = len(entry)
	}
	scanner.entries = append(scanner.entries, Entry{
		Key:        key,
		KeyRange:   keyRange,
		Version:    version,
		Value:      entry[quoteOpen+1 : quoteClose],
		ValueRange: Range{Start: valueInnerStart, End: entryBase + quoteClose},
		Line:       scanner.lineNo,
	})
}

func (scanner *scanner) badEntry(fullLine string, lineBase int) {
	scanner.errs = append(scanner.errs, Error{
		Code:    ErrBadEntry,
		Message: `Malformed localization entry; expected ` + "`key: \"value\"`" + `.`,
		Range:   Range{Start: lineBase, End: lineBase + len(fullLine)},
	})
}

// filenameLangRe matches the language between a loc filename's last `l_` token and its extension:
// `events_l_english.yml` -> "english", `events_l_simp_chinese.yml` -> "simp_chinese".
var filenameLangRe = regexp.MustCompile(`(?i)^(?:.*_)?l_([a-z_]+)\.(yml|yaml)$`)

// LanguageFromFilename returns the language id encoded in a loc filename, or ""
// if the name does not follow the `..._l_<language>.yml` convention.
func LanguageFromFilename(path string) string {
	base := filepath.Base(path)
	if match := filenameLangRe.FindStringSubmatch(base); match != nil {
		return strings.ToLower(match[1])
	}
	return ""
}

// LanguageFromRel returns the loc language from the filename, else from a
// `localization/<lang>/` path segment, or "".
func LanguageFromRel(path string) string {
	if language := LanguageFromFilename(path); language != "" {
		return language
	}
	// a rel starts with the folder, so a slash is put before it
	slash := "/" + strings.ReplaceAll(path, "\\", "/")
	lower := strings.ToLower(slash)
	const marker = "/localization/"
	i := strings.Index(lower, marker)
	if i < 0 {
		return ""
	}
	rest := slash[i+len(marker):]
	// a file straight under localization (languages.yml) is in no language's folder
	lang, _, inFolder := strings.Cut(rest, "/")
	if !inFolder {
		return ""
	}
	return strings.ToLower(lang)
}

// LooksLikeKey reports whether name is a plausible loc key: no colon, no whitespace, no keyword.
func LooksLikeKey(name string) bool {
	if name == "" || strings.Contains(name, ":") {
		return false
	}
	if strings.ContainsFunc(name, unicode.IsSpace) {
		return false
	}
	// A macro parameter's key is not knowable until called, so it must not be reported as missing.
	if strings.Contains(name, "$") {
		return false
	}
	switch strings.ToLower(name) {
	case "yes", "no", "none", "root", "prev", "this", "from":
		return false
	}
	return true
}

// plainMarks is the markup a player never reads: [concept|E] links, $KEY$ interpolations, and the
// #style … #! formatting around text.
var plainMarks = regexp.MustCompile(`\[([A-Za-z0-9_.]+)\|E\]|\$([A-Za-z0-9_.]+)(?:\|[^$]*)?\$|#![ ]?|#[A-Za-z0-9_;]+ `)

// Plain is localization as a player reads it: a concept link as the concept's word, an interpolated
// key as its name, formatting dropped.
func Plain(text string) string {
	return plainMarks.ReplaceAllString(text, "$1$2")
}
