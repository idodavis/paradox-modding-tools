// names.go reads a single written word, whatever the word is for, the same way in every Jomini
// game. The name shapes the games key their objects by. The words that are values rather than
// names: a number, and a Clausewitz date (a two-part dotted word is an event id, never a date,
// and a date is never a name, because history files key their blocks by one). And the four
// spellings one word can carry inside it: a cite (prefix:name, with a |member and a .chain
// after it), an entry mode before a definition's name (REPLACE:name), a constant (@name), and
// the $NAME$ parameters a macro is called with.
package jomini

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The three name shapes below are tested on every key of every file, so they are written out
// rather than as regexes and allocate nothing; TestNameShapes pins each against its pattern.

// The games ship non-ASCII identifiers: Victoria 3's `mon_general_petar_vukotić` is a vanilla
// character template, CK3 saves a scope as `Linnéa`, a Victoria 3 mod names a journal entry
// `bpm_je_propaganda_of_the_deed_бомбометр`. A byte at or above utf8.RuneSelf opens a
// multi-byte sequence, so it is decoded and judged by unicode; ASCII costs one comparison.
// `country_event·` stays rejected: U+00B7 is punctuation, and that is a real mod typo.

// IsDefName reports a definition key: the ASCII shape
// `^[A-Za-z0-9_][A-Za-z0-9_.-]*$`, widened to any Unicode letter or digit.
func IsDefName(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); {
		char := name[i]
		if char < utf8.RuneSelf {
			if !isAlnumUnder(char) && !(i > 0 && (char == '.' || char == '-')) {
				return false
			}
			i++
			continue
		}
		decoded, size := utf8.DecodeRuneInString(name[i:])
		if !unicode.IsLetter(decoded) && !unicode.IsDigit(decoded) {
			return false
		}
		i += size
	}
	return true
}

// IsEventID reports `namespace.1234`: the ASCII shape `^[A-Za-z0-9_-]+\.\d+$`,
// with the namespace half widened the same way as IsDefName. The number after
// the dot stays ASCII digits — it is an engine-assigned index, not a name.
func IsEventID(name string) bool {
	dot := strings.LastIndexByte(name, '.')
	if dot <= 0 || dot == len(name)-1 {
		return false
	}
	for i := 0; i < dot; {
		char := name[i]
		if char < utf8.RuneSelf {
			if !isAlnumUnder(char) && char != '-' {
				return false
			}
			i++
			continue
		}
		decoded, size := utf8.DecodeRuneInString(name[i:dot])
		if !unicode.IsLetter(decoded) && !unicode.IsDigit(decoded) {
			return false
		}
		i += size
	}
	for i := dot + 1; i < len(name); i++ {
		if char := name[i]; char < '0' || char > '9' {
			return false
		}
	}
	return true
}

// IsLowerName reports a lowercase script identifier: the ASCII shape
// `^[a-z][a-z0-9_]*$`, widened to any lowercase Unicode letter. Lowercase, not any letter:
// this decides whether a word is an effect or trigger name, and the games write those in
// lower case.
func IsLowerName(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); {
		char := name[i]
		if char < utf8.RuneSelf {
			ok := (char >= 'a' && char <= 'z') ||
				(i > 0 && (char == '_' || (char >= '0' && char <= '9')))
			if !ok {
				return false
			}
			i++
			continue
		}
		decoded, size := utf8.DecodeRuneInString(name[i:])
		if !unicode.IsLower(decoded) && !(i > 0 && unicode.IsDigit(decoded)) {
			return false
		}
		i += size
	}
	return true
}

func isAlnumUnder(char byte) bool {
	return char == '_' || (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
		(char >= '0' && char <= '9')
}

// PeelComment strips leading `#` markers from a line and reports whether any
// were there. A commented-out template still parses as script once peeled,
// which is how a game's shipped README documents a shape it never runs.
func PeelComment(raw string) (body string, hashed bool) {
	text := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
	if text == "" {
		return "", false
	}
	hashed = strings.HasPrefix(text, "#")
	for strings.HasPrefix(text, "#") {
		text = strings.TrimSpace(text[1:])
	}
	return text, hashed
}

// LiteralKind is what a scalar is when it is not a name.
type LiteralKind uint8

const (
	NotLiteral LiteralKind = iota
	LitNumber              // an integer or decimal, signed or not
	LitDate                // Y.M.D, optionally Y.M.D.H
)

// Literal reads a scalar: a date through ParseDate, a number by its characters, else a name. A
// number is digits with at most one dot and a leading sign, which is all script writes;
// strconv would also take inf, nan, 0x10 and 1e5, and those are names.
func Literal(text string) LiteralKind {
	if _, _, _, _, ok := ParseDate(text); ok {
		return LitDate
	}

	digits, dots := 0, 0
	for i := 0; i < len(text); i++ {
		switch char := text[i]; {
		case char >= '0' && char <= '9':
			digits++
		case char == '.':
			dots++
		case (char == '-' || char == '+') && i == 0:
		default:
			return NotLiteral
		}
	}
	if digits > 0 && dots <= 1 {
		return LitNumber
	}
	return NotLiteral
}

// ParseDate reads a Clausewitz date, `Y.M.D` or `Y.M.D.H`, every part digits. Two parts is an
// event id (`ns.1`), never a date; history files key their blocks by a date, so a date is
// never a definition name.
func ParseDate(text string) (year, month, day, hour int, ok bool) {
	parts := strings.Split(text, ".")
	if len(parts) != 3 && len(parts) != 4 {
		return 0, 0, 0, 0, false
	}
	out := [4]int{}
	for i, part := range parts {
		if part == "" || len(part) > 5 || !IsDigits(part) {
			return 0, 0, 0, 0, false
		}
		out[i], _ = strconv.Atoi(part)
	}
	return out[0], out[1], out[2], out[3], true
}

// IsDigits reports a non-empty run of ASCII digits.
func IsDigits(text string) bool {
	for i := 0; i < len(text); i++ {
		if text[i] < '0' || text[i] > '9' {
			return false
		}
	}
	return text != ""
}

// Cited is a word written prefix:name, split.
type Cited struct {
	Prefix string // character, title, scope, define
	Name   string // 186004, k_france, target, NGame
	Member string // the key after a bar: define:NGame|START_DATE
	Chain  string // what follows the first dot: scope:target.culture -> culture
}

// Cite splits a word written prefix:name. It refuses a word whose prefix is not one plain
// lower-case identifier (root.var:x is a scope path, INJECT:x an entry mode), and a colon
// followed by a slash (event:/SFX/x is a sound's name, file:/x a file, C:/x a path).
func Cite(text string) (Cited, bool) {
	colon := strings.IndexByte(text, ':')
	if colon <= 0 || colon == len(text)-1 || strings.ContainsRune(`/\`, rune(text[colon+1])) {
		return Cited{}, false
	}

	// the prefix: a letter, then letters, digits and underscores, with at least one lower-case letter
	prefix, lower := text[:colon], false
	for i, char := range prefix {
		if !(unicode.IsLetter(char) || char == '_' || (i > 0 && unicode.IsDigit(char))) || (i == 0 && char == '_') {
			return Cited{}, false
		}
		lower = lower || unicode.IsLower(char)
	}
	if !lower {
		return Cited{}, false
	}

	// the name runs to a bar, a dot or the end
	rest := text[colon+1:]
	end := nameEnd(rest)
	if end == 0 {
		return Cited{}, false
	}
	cited := Cited{Prefix: prefix, Name: rest[:end]}
	rest = rest[end:]

	// a member after a bar, then a chain after a dot
	if strings.HasPrefix(rest, "|") {
		end = nameEnd(rest[1:])
		if end == 0 {
			return Cited{}, false
		}
		cited.Member, rest = rest[1:1+end], rest[1+end:]
	}
	if strings.HasPrefix(rest, ".") && len(rest) > 1 {
		cited.Chain, rest = rest[1:], ""
	}
	if rest != "" {
		return Cited{}, false
	}
	return cited, true
}

// nameEnd is how many bytes of name are a name: letters, digits, underscores and hyphens.
func nameEnd(name string) int {
	for i := 0; i < len(name); {
		char, size := utf8.DecodeRuneInString(name[i:])
		if !(unicode.IsLetter(char) || unicode.IsDigit(char) || char == '_' || char == '-') {
			return i
		}
		i += size
	}
	return len(name)
}

// Mode splits an entry mode off a definition's name: REPLACE:name gives REPLACE and name. A
// mode is upper-case letters and underscores; any other word comes back whole with no mode.
func Mode(key string) (mode, name string) {
	colon := strings.IndexByte(key, ':')
	if colon <= 0 || colon == len(key)-1 {
		return "", key
	}

	for _, char := range key[:colon] {
		if !(char >= 'A' && char <= 'Z') && char != '_' {
			return "", key
		}
	}
	return key[:colon], key[colon+1:]
}

// Constant is the name in @name. An inline expression, @[ … ], is a constant with no name.
func Constant(text string) (name string, ok bool) {
	if len(text) < 2 || text[0] != '@' {
		return "", false
	}
	if text[1] == '[' {
		return "", true
	}
	return text[1:], true
}

// Span is one $NAME$ inside a word or a string, by byte offset into it; Start is the first
// dollar and End is past the last.
type Span struct {
	Name       string
	Start, End int
}

// Interpolations is every $NAME$ in text, in order. A dollar with no partner, and two dollars
// with nothing between, are not parameters.
func Interpolations(text string) []Span {
	var spans []Span
	for i := 0; i < len(text); {
		open := strings.IndexByte(text[i:], '$')
		if open < 0 {
			break
		}
		open += i

		// the name is word characters up to the closing dollar
		j := open + 1
		for j < len(text) && isAlnumUnder(text[j]) {
			j++
		}
		if j > open+1 && j < len(text) && text[j] == '$' {
			spans = append(spans, Span{Name: text[open+1 : j], Start: open, End: j + 1})
			i = j + 1
			continue
		}
		i = open + 1
	}
	return spans
}
