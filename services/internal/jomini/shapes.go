// shapes.go reads one written word the same way in every Jomini game: each shape the language has is one function
// that says what it matches, a name, an event id, a number, a date, `prefix:name`, `$PARAMETER$` and `@constant`.

package jomini

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// LiteralKind is what a scalar is when it is not a name.
type LiteralKind uint8

// Prefixed is a word written prefix:name, split.
type Prefixed struct {
	Prefix string // character, title, scope, define
	Name   string // 186004, k_france, target, NGame
	Key    string // the key after a bar: define:NGame|START_DATE
	Chain  string // what follows the first dot: scope:target.culture -> culture
}

// Span is one $NAME$ inside a word or a string, by byte offset into it; Start is the first
// dollar and End is past the last.
type Span struct {
	Name       string
	Start, End int
}

// inlineMath reads an inline expression's tokens in turn: sums of products of numbers, constants and brackets.
type inlineMath struct {
	result *Result
	tokens []string
	next   int
	place  int
	depth  int
}

// What a scalar is when it is not a name.
const (
	NotLiteral LiteralKind = iota
	LitNumber              // an integer or decimal, signed or not
	LitDate                // Y.M.D, optionally Y.M.D.H
)

// maxConstantDepth is how deep constants name one another before a cycle is assumed.
const maxConstantDepth = 8

// IsIdentifier reports a name: letters and digits of any script and underscores, with dots and hyphens allowed after
// the first character. It runs on every key, so it is hand-written rather than a regex and allocates nothing.
func IsIdentifier(text string) bool {
	return text != "" && text[0] != '.' && text[0] != '-' && identifierEnd(text, ".-") == len(text)
}

// IsEventID reports `namespace.1234`: a namespace of letters, digits, underscores and hyphens that is no number (1.5
// is none), a dot, and digits. The number after the dot stays ASCII digits: it is an engine-assigned index, not a
// name.
func IsEventID(text string) bool {
	dot := strings.LastIndexByte(text, '.')
	if dot <= 0 || !IsDigits(text[dot+1:]) {
		return false
	}
	namespace := text[:dot]
	return strings.Trim(namespace, "0123456789-") != "" && identifierEnd(namespace, "-") == len(namespace)
}

// identifierEnd is how many bytes of text, from its start, are identifier characters, the letters and digits of any
// script and the underscore, or one of joiners.
func identifierEnd(text, joiners string) int {
	for i, char := range text {
		isIdentifier := char == '_' || unicode.IsLetter(char) || unicode.IsDigit(char)
		if !isIdentifier && !strings.ContainsRune(joiners, char) {
			return i
		}
	}
	return len(text)
}

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

// ParseDate reads a Clausewitz date, `Y.M.D` or `Y.M.D.H`, every part digits; two parts is an event
// id (`ns.1`). A date is never a definition name: history files key their blocks by one. A year may
// be negative (`-221.1.1`) and one trailing dot is allowed (`867.1.1.`).
func ParseDate(text string) (year, month, day, hour int, ok bool) {
	parts := strings.Split(strings.TrimSuffix(text, "."), ".")
	if len(parts) != 3 && len(parts) != 4 {
		return 0, 0, 0, 0, false
	}
	out := [4]int{}
	for i, part := range parts {
		digits := part
		if i == 0 {
			digits = strings.TrimPrefix(part, "-")
		}
		if digits == "" || len(digits) > 5 || !IsDigits(digits) {
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

// SplitPrefixed splits a word written prefix:name. It refuses a word whose prefix is not one plain
// lower-case identifier (root.var:x is a scope path, INJECT:x an entry mode), and a colon
// followed by a slash (event:/SFX/x is a sound's name, file:/x a file, C:/x a path).
func SplitPrefixed(text string) (Prefixed, bool) {
	colon := strings.IndexByte(text, ':')
	if colon <= 0 || colon == len(text)-1 || strings.ContainsRune(`/\`, rune(text[colon+1])) {
		return Prefixed{}, false
	}

	// the prefix: a letter, then letters, digits and underscores, with at least one lower-case letter
	prefix := text[:colon]
	first, _ := utf8.DecodeRuneInString(prefix)
	if !unicode.IsLetter(first) || identifierEnd(prefix, "") != len(prefix) || strings.IndexFunc(prefix, unicode.IsLower) < 0 {
		return Prefixed{}, false
	}

	// the name runs to a bar, a dot or the end
	rest := text[colon+1:]
	end := identifierEnd(rest, "-")
	if end == 0 {
		return Prefixed{}, false
	}
	prefixed := Prefixed{Prefix: prefix, Name: rest[:end]}
	rest = rest[end:]

	// a key after a bar, then a chain after a dot
	if strings.HasPrefix(rest, "|") {
		end = identifierEnd(rest[1:], "-")
		if end == 0 {
			return Prefixed{}, false
		}
		prefixed.Key, rest = rest[1:1+end], rest[1+end:]
	}
	if strings.HasPrefix(rest, ".") && len(rest) > 1 {
		prefixed.Chain, rest = rest[1:], ""
	}
	if rest != "" {
		return Prefixed{}, false
	}
	return prefixed, true
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

// Parameters is every $NAME$ in text, in order. A dollar with no partner, and two dollars
// with nothing between, are not parameters.
func Parameters(text string) []Span {
	var spans []Span
	for i := 0; i < len(text); {
		open := strings.IndexByte(text[i:], '$')
		if open < 0 {
			break
		}
		open += i

		// the name is identifier characters up to the closing dollar
		end := open + 1 + identifierEnd(text[open+1:], "")
		if end > open+1 && end < len(text) && text[end] == '$' {
			spans = append(spans, Span{Name: text[open+1 : end], Start: open, End: end + 1})
			i = end + 1
			continue
		}
		i = open + 1
	}
	return spans
}

// ConstantAt is the value a file gives a constant (@panel_width) at a place in it, and where the declaration's key
// starts: the last declaration of the name with a word in the innermost block around the place that declares it, else
// in the file's top statements; false where the name is no constant or none declares it.
func (result *Result) ConstantAt(name string, place int) (value string, at int, ok bool) {
	if constant, isConstant := Constant(name); !isConstant || constant == "" {
		return "", 0, false
	}
	for statements := result.Root.Statements; statements != nil; {
		var inner []Statement
		for _, statement := range statements {
			assign, isAssign := statement.(*Assignment)
			if !isAssign {
				continue
			}
			if scalar, isScalar := assign.Value.(*Scalar); isScalar && assign.Key.Text == name {
				value, at, ok = scalar.Text, assign.Key.Range.Start, true
			}
			if block := ChildBlock(assign); block != nil && block.Range.Start <= place && place < block.Range.End {
				inner = block.Statements
			}
		}
		statements = inner
	}
	return value, at, ok
}

// Evaluated is a word written at a place with its file's constants put in: @name the constant's value, @[ expression ]
// the number its arithmetic (+ - * / and brackets) over numbers and the file's constants makes; the word as written
// where it is no constant, names one the file does not give there, or computes anything else (a data call).
func (result *Result) Evaluated(text string, place int) string {
	if value, ok := result.evaluate(text, place, 0); ok {
		return value
	}
	return text
}

// evaluate is a constant word's value at a place, the constants it names followed as deep as maxConstantDepth.
func (result *Result) evaluate(text string, place, depth int) (string, bool) {
	name, isConstant := Constant(text)
	if result == nil || !isConstant || depth > maxConstantDepth {
		return "", false
	}
	if name != "" {
		value, _, declared := result.ConstantAt(text, place)
		if _, nested := Constant(value); declared && nested {
			return result.evaluate(value, place, depth+1)
		}
		return value, declared
	}
	math := &inlineMath{result: result, place: place, depth: depth, tokens: mathTokens(strings.TrimSuffix(text[2:], "]"))}
	number, ok := math.sum()
	if !ok || math.next < len(math.tokens) {
		return "", false
	}
	return strconv.FormatFloat(number, 'f', -1, 64), true
}

// mathTokens are an inline expression's numbers and names, each a run of identifier characters and dots, and its
// operators and brackets, each one character; blanks separate tokens and are none.
func mathTokens(text string) []string {
	var tokens []string
	for pos := 0; pos < len(text); {
		char, size := utf8.DecodeRuneInString(text[pos:])
		if length := identifierEnd(text[pos:], "."); length > 0 {
			size = length
		} else if unicode.IsSpace(char) {
			pos += size
			continue
		}
		tokens = append(tokens, text[pos:pos+size])
		pos += size
	}
	return tokens
}

// sum is a run of products joined by + and -.
func (math *inlineMath) sum() (float64, bool) {
	total, ok := math.product()
	for ok && math.next < len(math.tokens) && (math.tokens[math.next] == "+" || math.tokens[math.next] == "-") {
		operator := math.tokens[math.next]
		math.next++
		var term float64
		term, ok = math.product()
		if operator == "-" {
			term = -term
		}
		total += term
	}
	return total, ok
}

// product is a run of factors joined by * and /.
func (math *inlineMath) product() (float64, bool) {
	total, ok := math.factor()
	for ok && math.next < len(math.tokens) && (math.tokens[math.next] == "*" || math.tokens[math.next] == "/") {
		operator := math.tokens[math.next]
		math.next++
		var factor float64
		factor, ok = math.factor()
		if operator == "/" {
			factor = 1 / factor
		}
		total *= factor
	}
	return total, ok
}

// factor is a number, a constant of the file, a bracketed sum, or one of them negated.
func (math *inlineMath) factor() (float64, bool) {
	if math.next >= len(math.tokens) {
		return 0, false
	}
	token := math.tokens[math.next]
	math.next++
	switch token {
	case "-":
		value, ok := math.factor()
		return -value, ok
	case "(":
		value, ok := math.sum()
		if !ok || math.next >= len(math.tokens) || math.tokens[math.next] != ")" {
			return 0, false
		}
		math.next++
		return value, true
	}
	if number, err := strconv.ParseFloat(token, 64); err == nil {
		return number, true
	}
	value, ok := math.result.evaluate("@"+token, math.place, math.depth+1)
	number, err := strconv.ParseFloat(value, 64)
	return number, ok && err == nil
}
