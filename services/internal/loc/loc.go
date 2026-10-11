// loc.go is a line-by-line, never-panicking scanner emitting entries and typed errors; it also
// derives a file's language from its name, path or header.

// Package loc parses Paradox's localization "YAML" dialect (not real YAML). It is game-agnostic.
// Offsets are UTF-8 byte indices into the original text (a leading BOM is skipped, not stripped).
package loc

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"

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

// Interpolation is a `$key$` (optional `|filter`) reuse inside a loc value.
type Interpolation struct {
	Key      string
	KeyRange Range // inner key, not `$` or `|filter`
}

// ErrorCode is a parse error's code, the same type as script's.
type ErrorCode = jomini.ErrorCode

// Error is a parse error, the same type as script's.
type Error = jomini.ParseError

// Result is the parsed localization file.
type Result struct {
	Language string // "english" from `l_english:`; "" if none found
	Entries  []Entry
	Errors   []Error
	HadBOM   bool
}

// scanner holds accumulators across the line-by-line parse.
type scanner struct {
	entries   []Entry
	errs      []Error
	language  string
	hasHeader bool
	hadBOM    bool
	lineNo    int
}

// The codes loc's own errors carry; a parse error is otherwise script's (jomini.ParseError).
const (
	ErrNoHeader            ErrorCode = "no-header"
	ErrBadEntry            ErrorCode = "bad-entry"
	ErrTabIndent           ErrorCode = "tab-indent"
	ErrUnterminatedValue   ErrorCode = "unterminated-value"
	ErrContentBeforeHeader ErrorCode = "content-before-header"
)

// headerRe matches a header line: optional indent, `l_<name>:`, then only trailing
// whitespace/comment.
var headerRe = regexp.MustCompile(`^[ \t]*l_([A-Za-z_]+):[ \t]*(#.*)?$`)

// filenameLangRe matches the language between a loc filename's last `l_` token and its extension:
// `events_l_english.yml` -> "english", `events_l_simp_chinese.yml` -> "simp_chinese".
var filenameLangRe = regexp.MustCompile(`(?i)^(?:.*_)?l_([a-z_]+)\.(yml|yaml)$`)

// plainMarks is the markup a player never reads: [concept|E] links, $KEY$ interpolations, and the
// #style … #! formatting around text.
var plainMarks = regexp.MustCompile(`\[([A-Za-z0-9_.]+)\|[Ee]\]|\$([A-Za-z0-9_.]+)(?:\|[^$]*)?\$|#![ ]?|#[A-Za-z0-9_;]+ `)

// Parse parses localization text. A leading UTF-8 BOM is skipped; ranges stay
// in the original string. Never panics.
func Parse(text string) Result {
	scanner := &scanner{}
	index := jomini.NewLineIndex(text)
	for line := range index.LineCount() {
		start, end := index.OffsetAt(line, 0), index.OffsetAt(line+1, 0)
		scanner.lineNo = line
		scanner.processLine(strings.TrimRight(text[start:end], "\r\n"), start)
	}

	if !scanner.hasHeader {
		scanner.errs = append(scanner.errs, Error{
			Code:    ErrNoHeader,
			Message: "No localization header line (e.g. `l_english:`) found.",
			Range:   Range{Start: 0, End: 0},
		})
	}

	return Result{Language: scanner.language, Entries: scanner.entries, Errors: scanner.errs, HadBOM: scanner.hadBOM}
}

// processLine handles one line; base is the byte offset in text of line[0].
func (scanner *scanner) processLine(line string, base int) {
	if body, marked := strings.CutPrefix(line, jomini.BOM); scanner.lineNo == 0 && marked {
		scanner.hadBOM, line, base = true, body, base+len(jomini.BOM)
	}
	i := 0
	sawTab := false
indent:
	for i < len(line) {
		switch line[i] {
		case ' ':
			i++
		case '\t':
			sawTab = true
			i++
		default:
			break indent
		}
	}
	contentStart := i
	if contentStart >= len(line) || line[contentStart] == '#' {
		return // blank or comment-only
	}

	if !scanner.hasHeader {
		if match := headerRe.FindStringSubmatch(line); strings.Contains(line, "l_") && match != nil {
			scanner.hasHeader, scanner.language = true, match[1]
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
	pos := 0
	for pos < len(entry) && isKeyChar(entry[pos]) {
		pos++
	}
	if pos == 0 {
		scanner.badEntry(fullLine, lineBase)
		return
	}
	key := entry[:pos]
	keyRange := Range{Start: entryBase, End: entryBase + pos}

	if pos >= len(entry) || entry[pos] != ':' {
		scanner.badEntry(fullLine, lineBase)
		return
	}
	pos++ // consume ':'

	version := -1
	versionStart := pos
	for pos < len(entry) && entry[pos] >= '0' && entry[pos] <= '9' {
		pos++
	}
	if pos > versionStart {
		version, _ = strconv.Atoi(entry[versionStart:pos])
	}

	for pos < len(entry) && (entry[pos] == ' ' || entry[pos] == '\t') {
		pos++
	}

	if pos >= len(entry) || entry[pos] != '"' {
		scanner.badEntry(fullLine, lineBase)
		return
	}
	quoteOpen := pos
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

// badEntry records a malformed-entry error over the whole line.
func (scanner *scanner) badEntry(fullLine string, lineBase int) {
	scanner.errs = append(scanner.errs, Error{
		Code:    ErrBadEntry,
		Message: `Malformed localization entry; expected ` + "`key: \"value\"`" + `.`,
		Range:   Range{Start: lineBase, End: lineBase + len(fullLine)},
	})
}

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

// Interpolations finds `$key$` / `$key|filter$` interpolations in a loc value.
// valueStart is the UTF-8 offset of value[0] in the file text.
func Interpolations(value string, valueStart int) []Interpolation {
	var out []Interpolation
	for pos := 0; pos < len(value); {
		if value[pos] != '$' {
			pos++
			continue
		}
		end := pos + 1
		for end < len(value) && isKeyChar(value[end]) {
			end++
		}
		if end == pos+1 {
			pos++
			continue
		}
		close := end
		filter := ""
		if close < len(value) && value[close] == '|' {
			filterStart := close + 1
			close++
			for close < len(value) && value[close] != '$' {
				close++
			}
			if close < len(value) {
				filter = value[filterStart:close]
			}
		}
		if close >= len(value) || value[close] != '$' {
			pos++
			continue
		}
		key := value[pos+1 : end]
		if LooksLikeKey(key) || (strings.EqualFold(key, "VALUE") && filter != "") {
			out = append(out, Interpolation{Key: key, KeyRange: Range{Start: valueStart + pos + 1, End: valueStart + end}})
		}
		pos = close + 1
	}
	return out
}

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

// Plain is localization as a player reads it: a concept link as the concept's word, an interpolated
// key as its name, formatting dropped.
func Plain(text string) string {
	return plainMarks.ReplaceAllString(text, "$1$2")
}

// Stub is a new localization file's contents: the byte order mark the games want, and the header that names its
// language.
func Stub(language string) []byte {
	return []byte(jomini.BOM + "l_" + language + ":\n")
}
