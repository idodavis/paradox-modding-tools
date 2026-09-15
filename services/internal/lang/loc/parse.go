// parse.go implements the localization dialect parser: a line-by-line,
// never-panicking scanner emitting entries and typed errors with UTF-8 byte
// offsets, derives a file's language from its name or header, and lists the
// script properties whose right-hand side is a localization key (STRICT =
// unresolved means "missing loc", BROAD = resolved hint only). See doc.go for
// the package overview.

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
	Key       string
	Filter    string // text after | when present
	KeyRange  Range  // inner key, not `$` or `|filter`
	WrapRange Range  // opening `$` through closing `$` (exclusive end)
}

// ErrorCode classifies a localization parse error.
type ErrorCode string

const (
	ErrNoHeader            ErrorCode = "no-header"
	ErrBadEntry            ErrorCode = "bad-entry"
	ErrTabIndent           ErrorCode = "tab-indent"
	ErrUnterminatedValue   ErrorCode = "unterminated-value"
	ErrContentBeforeHeader ErrorCode = "content-before-header"
)

// Error is a recovered localization parse error.
type Error struct {
	Code    ErrorCode
	Message string
	Range   Range
}

// Result is the parsed localization file.
type Result struct {
	Language    string // "english" from `l_english:`; "" if none found
	HeaderRange *Range
	Entries     []Entry
	Errors      []Error
	HadBOM      bool
}

// headerRe matches a header line: optional indent, `l_<name>:`, then only trailing
// whitespace/comment.
var headerRe = regexp.MustCompile(`^[ \t]*l_([A-Za-z_]+):[ \t]*(#.*)?$`)

// isKeyChar reports the byte classes allowed in a loc key: letters, digits, `_ . - '`.
func isKeyChar(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	case c == '_' || c == '.' || c == '-' || c == '\'':
		return true
	default:
		return false
	}
}

// Parse parses localization text. A leading UTF-8 BOM is skipped; ranges stay
// in the original string. Never panics.
func Parse(text string) Result {
	s := &scanner{}
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
		s.processLine(text[lineStart:lineEnd], lineStart)
		lineStart = nextStart
		s.lineNo++
		if lineStart > length {
			break
		}
	}

	if !s.headerFound {
		s.errs = append(s.errs, Error{
			Code:    ErrNoHeader,
			Message: "No localization header line (e.g. `l_english:`) found.",
			Range:   Range{Start: 0, End: 0},
		})
	}

	return Result{
		Language:    s.language,
		HeaderRange: s.headerRange,
		Entries:     s.entries,
		Errors:      s.errs,
		HadBOM:      s.hadBOM,
	}
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
			out = append(out, Interp{
				Key:       key,
				Filter:    filter,
				KeyRange:  Range{Start: valueStart + i + 1, End: valueStart + j},
				WrapRange: Range{Start: valueStart + i, End: valueStart + closeAt + 1},
			})
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
	headerRange *Range
	headerFound bool
	hadBOM      bool
	lineNo      int
}

// processLine handles one line; base is the byte offset in text of line[0].
func (s *scanner) processLine(line string, base int) {
	if s.lineNo == 0 && strings.HasPrefix(line, "\ufeff") {
		s.hadBOM = true
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

	if !s.headerFound {
		if !strings.Contains(line, "l_") {
			s.errs = append(s.errs, Error{
				Code:    ErrContentBeforeHeader,
				Message: "Content appears before the localization header (e.g. `l_english:`).",
				Range:   Range{Start: base + contentStart, End: base + len(line)},
			})
			return
		}
		if m := headerRe.FindStringSubmatch(line); m != nil {
			s.headerFound = true
			s.language = m[1]
			idx := strings.Index(line, "l_")
			colon := strings.Index(line[max(idx, 0):], ":")
			if colon >= 0 {
				colon += max(idx, 0)
			}
			end := base + len(line)
			if colon >= 0 {
				end = base + colon + 1
			}
			s.headerRange = &Range{Start: base + idx, End: end}
			return
		}
		s.errs = append(s.errs, Error{
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
		s.errs = append(s.errs, Error{
			Code:    ErrTabIndent,
			Message: "Tabs are not allowed for indentation in localization files.",
			Range:   Range{Start: base, End: base + contentStart},
		})
		// Best-effort: keep parsing the entry.
	}

	s.parseEntry(line[contentStart:], base+contentStart, line, base)
}

// parseEntry parses `key(:version)? "value"`; entryBase is the offset of entry[0].
func (s *scanner) parseEntry(entry string, entryBase int, fullLine string, lineBase int) {
	j := 0
	for j < len(entry) && isKeyChar(entry[j]) {
		j++
	}
	if j == 0 {
		s.badEntry(fullLine, lineBase)
		return
	}
	key := entry[:j]
	keyRange := Range{Start: entryBase, End: entryBase + j}

	if j >= len(entry) || entry[j] != ':' {
		s.badEntry(fullLine, lineBase)
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
		s.badEntry(fullLine, lineBase)
		return
	}
	quoteOpen := j
	valueInnerStart := entryBase + quoteOpen + 1

	quoteClose := strings.LastIndexByte(entry, '"')
	if quoteClose == quoteOpen {
		s.errs = append(s.errs, Error{
			Code:    ErrUnterminatedValue,
			Message: "Unterminated localization value (missing closing quote).",
			Range:   Range{Start: entryBase + quoteOpen, End: lineBase + len(fullLine)},
		})
		s.entries = append(s.entries, Entry{
			Key:        key,
			KeyRange:   keyRange,
			Version:    version,
			Value:      entry[quoteOpen+1:],
			ValueRange: Range{Start: valueInnerStart, End: entryBase + len(entry)},
			Line:       s.lineNo,
		})
		return
	}

	s.entries = append(s.entries, Entry{
		Key:        key,
		KeyRange:   keyRange,
		Version:    version,
		Value:      entry[quoteOpen+1 : quoteClose],
		ValueRange: Range{Start: valueInnerStart, End: entryBase + quoteClose},
		Line:       s.lineNo,
	})
}

func (s *scanner) badEntry(fullLine string, lineBase int) {
	s.errs = append(s.errs, Error{
		Code:    ErrBadEntry,
		Message: `Malformed localization entry; expected ` + "`key: \"value\"`" + `.`,
		Range:   Range{Start: lineBase, End: lineBase + len(fullLine)},
	})
}

// filenameLangRe matches the `l_<language>` token that precedes the extension in
// a loc filename, e.g. `events_l_english.yml` -> "english".
var filenameLangRe = regexp.MustCompile(`(?i)l_([a-z]+)\.(yml|yaml)$`)

// LanguageFromFilename returns the language id encoded in a loc filename, or ""
// if the name does not follow the `..._l_<language>.yml` convention.
func LanguageFromFilename(path string) string {
	base := filepath.Base(path)
	if m := filenameLangRe.FindStringSubmatch(base); m != nil {
		return strings.ToLower(m[1])
	}
	return ""
}

// LanguageFromRel returns the loc language from the filename, else from a
// `localization/<lang>/` path segment, or "".
func LanguageFromRel(path string) string {
	if l := LanguageFromFilename(path); l != "" {
		return l
	}
	slash := strings.ReplaceAll(path, "\\", "/")
	lower := strings.ToLower(slash)
	const marker = "/localization/"
	i := strings.Index(lower, marker)
	if i < 0 {
		return ""
	}
	rest := slash[i+len(marker):]
	lang, _, _ := strings.Cut(rest, "/")
	return strings.ToLower(lang)
}

// LanguageOf returns the loc language from the filename, else the parsed header.
func LanguageOf(path, parsedLang string) string {
	if l := LanguageFromFilename(path); l != "" {
		return l
	}
	return strings.ToLower(parsedLang)
}

// strictProps virtually always hold a loc key.
var strictProps = map[string]bool{
	"title":             true,
	"desc":              true,
	"flavor":            true,
	"custom_tooltip":    true,
	"selection_tooltip": true,
	"confirm_text":      true,
	"confirm_title":     true,
	"prompt":            true,
	"failure_desc":      true,
	"success_desc":      true,
	"war_name":          true,
	"cb_name":           true,
	"notification_text": true,
	// trigger_localization / effect_localization person+tense slots
	"first": true, "third": true, "global": true, "none": true,
	"first_not": true, "third_not": true, "global_not": true, "none_not": true,
	"first_past": true, "third_past": true, "global_past": true,
	"first_neg": true, "third_neg": true, "global_neg": true,
	"first_past_neg": true, "third_past_neg": true, "global_past_neg": true,
}

// broadProps often hold a loc key (superset of strict). These also hold non-loc
// values, so only a resolved key produces a hint.
var broadProps = map[string]bool{
	"name": true, "text": true, "tooltip": true, "first_valid": true,
	"reason": true, "format": true, "header": true, "opinion_text": true,
	"what": true, "who": true, "notification": true,

	// localization_key was strict, and it is the single largest source of false
	// "missing localization" anywhere in the engine. Counted over vanilla, where
	// by definition nothing is missing, its value fails to resolve 2,607 times
	// on CK3, 11,597 on Victoria 3 and 46,481 on EU5 — 99.7% of every unresolved
	// strict property on that game.
	//
	// The reason is visible in the values: `localization_key = CustomLoc_BR_male_`
	// ends in an underscore because customizable localization completes the stem
	// at runtime. No key by that name exists or should. Broad still marks a
	// resolved key used and still shows its text on hover; it just stops
	// asserting that a key the game builds itself is missing.
	"localization_key": true,
}

// Property classifies a script property name as a loc-key holder.
type Property string

const (
	// PropNone is a property that does not hold a loc key.
	PropNone Property = ""
	// PropStrict flags an unresolved value as a missing-loc diagnostic.
	PropStrict Property = "strict"
	// PropBroad shows a resolved-loc hint only, silent when unresolved.
	PropBroad Property = "broad"
)

// Classify returns whether prop holds a loc key. Names match the listed
// lowercase engine fields (`title`, `desc`); ALL_CAPS trigger arguments
// (`TITLE = primary_title`) are not loc properties.
func Classify(prop string) Property {
	if strictProps[prop] {
		return PropStrict
	}
	if broadProps[prop] {
		return PropBroad
	}
	return PropNone
}

// LooksLikeKey reports whether s is a plausible loc key (not yes/no/none,
// not a `scope:` / `character:` prefix, not a built-in scope, not prose).
//
// The prose case is why quoting proves nothing either way. Paradox quotes real
// keys — `war_name = "HRE_CONQUEST_WAR_NAME"` — and also writes display text
// straight into the same properties, which the game shows verbatim:
//
//	desc = "Always make coronations!"
//	desc = "The Red Keep"
//
// What separates them is not the quotes but the space. A localization file is
// `key:0 "value"`, so a key is a single token and can never contain whitespace.
// Without this check every literal string in a strict property was demanded as
// a missing key: 537 of A Game of Thrones' 618 such diagnostics, and EU5's
// "Wrong culture" / "Wrong religion".
func LooksLikeKey(s string) bool {
	if s == "" || strings.Contains(s, ":") {
		return false
	}
	if strings.ContainsFunc(s, unicode.IsSpace) {
		return false
	}
	// A value carrying a macro parameter is not knowable until the macro is
	// called: the key is whatever the caller passed, or a stem the engine
	// completes at runtime. Reporting either as missing tells a modder that
	// correct script is wrong — `desc = $TT$` in a scripted effect is not a key
	// by that name, and no key by that name should exist.
	if strings.Contains(s, "$") {
		return false
	}
	switch strings.ToLower(s) {
	case "yes", "no", "none", "root", "prev", "this", "from":
		return false
	}
	return true
}

// IsLocEngineValue reports a loc $key$ / $key|filter$ that is engine data,
// not a loc-key reuse ($other_key$ / $INDEPENDENCE_WAR_NAME$ / $key|U$).
func IsLocEngineValue(key, filter string) bool {
	if isLocEngineToken(key) {
		return true
	}
	return strings.EqualFold(key, "VALUE") && isNumericLocFilter(filter)
}

// isLocEngineToken reports a single ALL_CAPS token ($ORDER$, $VALUE$, $NAME$).
// SNAKE_CASE ($INDEPENDENCE_WAR_NAME$) is loc-key reuse.
func isLocEngineToken(key string) bool {
	if key == "" || strings.Contains(key, "_") {
		return false
	}
	for i := 0; i < len(key); i++ {
		if c := key[i]; c < 'A' || c > 'Z' {
			return false
		}
	}
	return true
}

func isNumericLocFilter(filter string) bool {
	if filter == "" {
		return false
	}
	for _, r := range filter {
		switch r {
		case '=', '+', '-', '%', '.':
		default:
			if !unicode.IsDigit(r) {
				return false
			}
		}
	}
	return true
}

// DataFnKeys finds single-quoted identifiers in a `[...]` data-function call: `[Concept('faith')]`.
func DataFnKeys(value string, valueStart int) []Interp {
	var out []Interp
	for i := 0; i < len(value); i++ {
		if value[i] != '[' {
			continue
		}
		end := strings.IndexByte(value[i:], ']')
		if end < 0 {
			break
		}
		end += i
		for j := i; j < end; j++ {
			if value[j] != '\'' {
				continue
			}
			k := strings.IndexByte(value[j+1:end], '\'')
			if k < 0 {
				break
			}
			k += j + 1
			if key := value[j+1 : k]; LooksLikeKey(key) {
				out = append(out, Interp{
					Key:       key,
					KeyRange:  Range{Start: valueStart + j + 1, End: valueStart + k},
					WrapRange: Range{Start: valueStart + j, End: valueStart + k + 1},
				})
			}
			j = k
		}
		i = end
	}
	return out
}
