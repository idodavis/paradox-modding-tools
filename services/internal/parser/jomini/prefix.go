// prefix.go parses `scope:name` / `var:name` language prefixes and
// first-class `type:id` object cites (`culture:english`).

package jomini

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// ephemeralPrefixes are the language prefixes that name a value living only for
// the duration of a script run, rather than a row in a database.
var ephemeralPrefixes = [...]string{"scope", "var", "local_var", "global_var"}

// Prefixed is one `prefix:name` token. NameOff is the byte offset of Name in Text.
type Prefixed struct {
	Prefix  string
	Name    string
	NameOff int
	Text    string
}

// ParsePrefixed splits a bare scalar/key. The segment after the first `.` is ignored.
//
// This is matched by hand rather than with `^(scope|var|…):([A-Za-z]\w*)`
// because it runs against every scalar in the install: the regexp form was 3% of
// a whole CK3 scan on its own.
func ParsePrefixed(text string) (Prefixed, bool) {
	colon := strings.IndexByte(text, ':')
	if colon <= 0 {
		return Prefixed{}, false
	}
	prefix := text[:colon]
	found := false
	for _, p := range ephemeralPrefixes {
		if prefix == p {
			found = true
			break
		}
	}
	if !found {
		return Prefixed{}, false
	}
	i := colon + 1
	if i >= len(text) || !startsName(text[i:]) {
		return Prefixed{}, false
	}
	// Non-ASCII does not end the name, it continues it. Stopping at the first
	// byte above ASCII did not fail — it returned ok=true with a TRUNCATED name,
	// so `scope:Linnéa` referenced `Linn` while `save_scope_as = Linnéa` defined
	// `Linnéa`. Two objects where the script has one: hover reads the wrong name,
	// go-to-definition looks for something nothing defines, and the real
	// definition looks unused. No check reports it, because saved scopes are
	// ephemeral and skipCompatRef keeps them out of dangling.
	for i < len(text) {
		c := text[i]
		if c < utf8.RuneSelf {
			if !isAlphaByte(c) && !isDigitByte(c) && c != '_' {
				break
			}
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(text[i:])
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			break
		}
		i += size
	}
	return Prefixed{
		Prefix:  prefix,
		Name:    text[colon+1 : i],
		NameOff: colon + 1,
		Text:    text,
	}, true
}

func isAlphaByte(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
}

// startsName reports that s opens with a letter, ASCII or not. A digit still
// does not start a name: `var:2` is arithmetic, not an identifier.
func startsName(s string) bool {
	if s == "" {
		return false
	}
	if s[0] < utf8.RuneSelf {
		return isAlphaByte(s[0])
	}
	r, _ := utf8.DecodeRuneInString(s)
	return unicode.IsLetter(r)
}

func isDigitByte(c byte) bool { return c >= '0' && c <= '9' }

// IsSavedScopePrefix reports whether p is a `scope:` reference (not var:).
func IsSavedScopePrefix(p Prefixed) bool {
	return p.Prefix == ScopePrefix
}

// SkipObjectRHS reports interpolation (`$COA$`) or a dotted link/scope chain
// (`capital_province.culture`) — not an object id.
func SkipObjectRHS(text string) bool {
	// '|' is an engine path separator, never part of an id: CK3 reads a tuning
	// constant as `value = define:NTaskContract|HIGH_TASK_CONTRACT_TIER`, and
	// harvesting that whole string as a reference reported it to the user as a
	// dangling province_terrain. ';' is a GUI statement terminator that the
	// lexer leaves attached, which produced a dangling `struggle_tooltip;`.
	if text == "" || strings.ContainsAny(text, "$.|;") {
		return true
	}
	return IsLiteralValue(text)
}

// IsLiteralValue reports a bare number or boolean — a quantity or a flag, never
// the name of anything.
//
// Both consumers of this rule learned it the hard way. In the catalog,
// `add_gold = -20` harvested `-20` as an object reference and Workspace Health
// reported `0`, `-10`, `-20`, `yes` and `no` to the user as dangling references
// — 176 of them on one real CK3 mod, 80 of 86 on one EU5 mod. In hover, the
// value in `is_ai = yes` fell through to the localization lookup, and because
// vanilla defines a loc key literally named `yes`, the card showed the player-
// facing string "Yes" as though it were the meaning of the line.
func IsLiteralValue(text string) bool {
	return isNumericLiteral(text) || IsDateLiteral(text) ||
		strings.EqualFold(text, "yes") || strings.EqualFold(text, "no")
}

// isNumericLiteral reports a bare integer or decimal, with an optional sign.
func isNumericLiteral(s string) bool {
	if s == "" {
		return false
	}
	i := 0
	if s[0] == '-' || s[0] == '+' {
		i = 1
	}
	digits, dots := 0, 0
	for ; i < len(s); i++ {
		switch c := s[i]; {
		case c >= '0' && c <= '9':
			digits++
		case c == '.':
			dots++
		default:
			return false
		}
	}
	return digits > 0 && dots <= 1
}

// SkipFieldRHS reports a field value that is grammar, not an object id.
func SkipFieldRHS(text string) bool {
	if SkipObjectRHS(text) {
		return true
	}
	switch strings.ToLower(text) {
	case "none", "all", "target_titles":
		return true
	}
	return false
}

// ParseTyped splits `culture:english` into a harvested kind and id.
// Ephemeral prefixes (scope:/var:) are not typed.
//
// This is the grammar half only. A game may reserve further prefixes for its
// own purposes — EU5 spends `INJECT:` / `REPLACE:` on entry modes — so callers
// that know the game go through game.ParseTyped, which rejects those first.
func ParseTyped(text string) (kind, id string, ok bool) {
	i := strings.IndexByte(text, ':')
	if i <= 0 || i >= len(text)-1 {
		return "", "", false
	}
	prefix, rest := text[:i], text[i+1:]
	if _, ok := ParsePrefixed(text); ok {
		return "", "", false
	}
	id, _, _ = strings.Cut(rest, ".")
	if !typedIdent(id) || strings.ContainsAny(id, ":$") {
		return "", "", false
	}
	kind = typedKind(prefix)
	if kind == "" {
		return "", "", false
	}
	return kind, id, true
}

// TypedSpan is one `prefix:id` cite inside a dotted chain.
type TypedSpan struct {
	Prefix string
	ID     string
	Start  int // byte offset of Prefix in Text
	End    int // byte offset after ID
	Text   string
}

// TypedSpans walks prefix:id cites in a TokenAt word. Id runs until `.`.
// Cursor pick is Start<=off<End (or End at the last span).
func TypedSpans(text string) []TypedSpan {
	var out []TypedSpan
	i := 0
	for i < len(text) {
		colon := strings.IndexByte(text[i:], ':')
		if colon < 0 {
			break
		}
		colon += i
		prefix := text[i:colon]
		if prefix == "" || strings.ContainsAny(prefix, ".$: ") {
			i = colon + 1
			continue
		}
		j := colon + 1
		for j < len(text) && text[j] != '.' && text[j] != ':' &&
			text[j] != '$' && text[j] != ' ' {
			j++
		}
		id := text[colon+1 : j]
		if typedIdent(id) {
			out = append(out, TypedSpan{
				Prefix: prefix, ID: id,
				Start: i, End: j, Text: text,
			})
		}
		if j < len(text) && text[j] == '.' {
			i = j + 1
			continue
		}
		break
	}
	return out
}

// TypedSpanAt returns the cite covering off inside word, or the last span
// when off is at the word end.
func TypedSpanAt(text string, off int) (TypedSpan, bool) {
	spans := TypedSpans(text)
	if len(spans) == 0 {
		return TypedSpan{}, false
	}
	if off < 0 {
		off = 0
	}
	if off > len(text) {
		off = len(text)
	}
	for _, s := range spans {
		if off >= s.Start && off < s.End {
			return s, true
		}
	}
	last := spans[len(spans)-1]
	if off >= last.End {
		return last, true
	}
	return TypedSpan{}, false
}

func typedIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if i == 0 {
			if r != '_' && !unicode.IsLetter(r) {
				return false
			}
			continue
		}
		if r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func typedKind(prefix string) string {
	p := strings.ToLower(prefix)
	if PrefixKind(p) != "" {
		return ""
	}
	if typedKindOK(p) {
		return p
	}
	return ""
}

func typedKindOK(k string) bool {
	if k == "" {
		return false
	}
	// A prefix is a single identifier. Anything with a dot is the tail of a
	// dotted chain (`$SCOPE$.var:x`, `root.var:x`), and taking it as a prefix
	// invented kinds literally named `$scope$.var` and `root.var` — 183 false
	// dangling references on one real Vic3 mod.
	if strings.ContainsAny(k, ".$[]") {
		return false
	}
	switch k {
	case "effect", "trigger", "scope", "field", "loc_key", "loc_value",
		"data_function", "mod_descriptor", "animation", "type", "types":
		return false
	}
	return true
}

// IsDateLiteral reports a Clausewitz date: `Y.M.D`, optionally with an hour as
// `Y.M.D.H`. Every part is digits.
//
// Dates are not names, but the games key their history files by one —
// `history/struggles/iberian_struggle_history.txt` opens `718.1.1 = { … }`, and
// `history/cultures/afghan.txt` opens `867.1.1 = { … }`. Harvested as
// definitions, the same date became a `struggles` object and a `cultures`
// object, so hovering `game_start_date < 1178.1.1` produced a struggle card and
// go-to-definition jumped into a culture's history.
//
// Two dot-separated parts is an event id (`ns.1`), never a date, so the count is
// what separates them.
func IsDateLiteral(s string) bool {
	n := 0
	for part := range strings.SplitSeq(s, ".") {
		if part == "" || len(part) > 5 {
			return false
		}
		for i := 0; i < len(part); i++ {
			if part[i] < '0' || part[i] > '9' {
				return false
			}
		}
		n++
	}
	return n == 3 || n == 4
}
