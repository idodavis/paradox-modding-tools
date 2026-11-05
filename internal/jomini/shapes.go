// shapes.go tells what shape a written key or value has, splits prefixed names and dotted chains, matches key paths
// and resolves the numbers a file writes.

package jomini

import (
	"path"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/samber/lo"
)

// precedences holds how tightly each operator of an expression binds.
var precedences = map[string]int{"+": 1, "-": 1, "*": 2, "/": 2}

// NumberKey is the key that matches a key written as a number or a date, such as random_list's branches.
const NumberKey = "<number>"

// IsIdentifier reports whether the text is a plain identifier: letters, digits and the characters `_ - . ' :`,
// with no spaces and no quotes, and not a number or a date.
func IsIdentifier(text string) bool {
	if text == "" || IsNumber(text) {
		return false
	}
	if _, ok := ParseDate(text); ok {
		return false
	}
	return strings.TrimLeftFunc(text, isIdentifierRune) == ""
}

// IsNumber reports whether the text is a number as the game writes one: an optional sign, digits, an optional
// fraction and an optional `%`, or a `0x` hexadecimal value.
func IsNumber(text string) bool {
	if hex, ok := strings.CutPrefix(text, "0x"); ok {
		return hex != "" && strings.Trim(hex, "0123456789abcdefABCDEF") == ""
	}
	text = strings.TrimSuffix(text, "%")
	if text != "" && (text[0] == '-' || text[0] == '+') {
		text = text[1:]
	}
	whole, fraction, dotted := strings.Cut(text, ".")
	return isDigits(whole) && (!dotted || isDigits(fraction))
}

// IsEventID reports whether the text is an event id: a namespace, a dot and a number.
func IsEventID(text string) bool {
	namespace, id, ok := strings.Cut(text, ".")
	return ok && IsIdentifier(text) && isAlphanumeric(namespace) && isDigits(id)
}

// IsParameter reports whether the text holds a scripted parameter, `$NAME$`, with or without a `|default`, anywhere
// in it.
func IsParameter(text string) bool {
	for {
		_, rest, ok := strings.Cut(text, "$")
		if !ok {
			return false
		}
		inner, _, ok := strings.Cut(rest, "$")
		if !ok {
			return false
		}
		if name, _, _ := strings.Cut(inner, "|"); isAlphanumeric(name) {
			return true
		}
		text = rest
	}
}

// IsConstant reports whether the text is a script constant, `@name`, or a constant expression, `@[…]`.
func IsConstant(text string) bool {
	if strings.HasPrefix(text, "@[") {
		return strings.HasSuffix(text, "]")
	}
	return strings.HasPrefix(text, "@") && IsIdentifier(text[1:])
}

// A Date is a date as the game writes one, such as `1066.9.15`.
type Date struct {
	Year  int `json:"year"`
	Month int `json:"month"`
	Day   int `json:"day"`
}

// ParseDate parses a text shaped like a date, `year.month.day`. A missing day is 0. It does not check the ranges,
// which IsValid does. It returns false for any other shape.
func ParseDate(text string) (Date, bool) {
	text, negative := strings.CutPrefix(text, "-")
	parts := strings.Split(text, ".")
	if len(parts) < 2 || len(parts) > 3 || !lo.EveryBy(parts, isDigits) {
		return Date{}, false
	}
	numbers := lo.Map(parts, func(part string, _ int) int {
		number, _ := strconv.Atoi(part)
		return number
	})
	date := Date{Year: numbers[0], Month: numbers[1]}
	if negative {
		date.Year = -date.Year
	}
	if len(numbers) == 3 {
		date.Day = numbers[2]
	}
	return date, true
}

// IsValid reports whether the date's month is from 1 to 12 and its day from 0 to 31, where 0 means no day.
func (date Date) IsValid() bool {
	return date.Month >= 1 && date.Month <= 12 && date.Day >= 0 && date.Day <= 31
}

// IsNumberKey reports whether a key is written as a number or a date, the keys that NumberKey stands for.
func IsNumberKey(key string) bool {
	_, date := ParseDate(key)
	return date || IsNumber(key)
}

// SplitPrefixed splits text written as `prefix:name` into the prefix and the name. It returns false if there is
// no prefix.
func SplitPrefixed(text string) (prefix, name string, ok bool) {
	prefix, name, ok = strings.Cut(text, ":")
	if !ok || prefix == "" || name == "" {
		return "", "", false
	}
	return prefix, name, true
}

// SplitChain splits a dotted chain such as `scope:x.var:y.z` into its links. A dot inside brackets or quotes does not
// split.
func SplitChain(text string) []string {
	var links []string
	depth, quoted, start := 0, false, 0
	for i := 0; i < len(text); i++ {
		switch text[i] {
		case '"':
			quoted = !quoted
		case '[':
			depth++
		case ']':
			depth = max(depth-1, 0)
		case '.':
			if depth == 0 && !quoted {
				links = append(links, text[start:i])
				start = i + 1
			}
		}
	}
	return append(links, text[start:])
}

// MatchKeyPath reports whether the names above a statement, outermost first, fit a path of segments joined by "/".
// A segment is a glob, NumberKey, or ** for any depth. An empty path fits only the file's top level.
func MatchKeyPath(pattern string, keys []string) bool {
	if pattern == "" {
		return len(keys) == 0
	}
	return matchSegments(strings.Split(pattern, "/"), keys)
}

// ColorParts returns the tag and the parts of a block statement such as `rgb { 1 2 3 }`, each part as written. It
// returns false if the statement is not a block of plain numbers, or of one hex number after a hex tag, or has no tag
// and not three or four numbers.
func ColorParts(tree *Tree, statement int32) (tag string, parts []string, ok bool) {
	if !tree.Statements[statement].IsBlock() {
		return "", nil, false
	}
	for child := range tree.Children(statement) {
		if !tree.Statements[child].IsListed() {
			return "", nil, false
		}
		parts = append(parts, tree.Text(tree.Statements[child].Value))
	}
	tag = tree.Text(tree.Statements[statement].Tag)
	row, _ := lookupColorRow(tag)
	if _, ok := toNumbers(row, parts); !ok || tag == "" && (len(parts) < 3 || len(parts) > 4) {
		return "", nil, false
	}
	return tag, parts, true
}

// ResolveNumber returns the number the text of the given span stands for: a number, a constant declared earlier in the
// file, or an expression @[…] of them. It returns false for any other text and for an expression it cannot compute.
func (tree *Tree) ResolveNumber(span Span) (float64, bool) {
	text := tree.Text(span)
	if number, ok := parseNumber(text); ok {
		return number, true
	}
	if name, ok := strings.CutPrefix(text, "@"); ok && IsIdentifier(name) {
		return tree.resolveConstant(name, span.Start)
	}
	tokens := splitExpression(text, span.Start)
	if len(tokens) == 0 {
		return 0, false
	}
	number, cursor, ok := tree.evaluate(tokens, 0, 1)
	return number, ok && cursor == len(tokens)
}

// ListConstants returns the span of each constant that the expression @[…] uses, without the @. The given offset is
// where the text starts in its file. It returns nothing for a text that is no expression, or that holds a data call or
// anything else but numbers, names, operators and brackets.
func ListConstants(text string, base int) []Span {
	var spans []Span
	for _, token := range splitExpression(text, base) {
		word := text[token.Start-base : token.End-base]
		switch {
		case IsNumber(word), precedences[word] > 0, word == "(", word == ")": // no constant
		case isAlphanumeric(word):
			spans = append(spans, token)
		default:
			return nil
		}
	}
	return spans
}

// isIdentifierRune reports whether the rune may be written in an identifier.
func isIdentifierRune(r rune) bool {
	return r >= utf8.RuneSelf || unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("_-.':", r)
}

// isDigits reports whether the text is a non-empty run of decimal digits.
func isDigits(text string) bool {
	return text != "" && strings.Trim(text, "0123456789") == ""
}

// isAlphanumeric reports whether the text is a non-empty run of ASCII letters, digits and underscores.
func isAlphanumeric(text string) bool {
	return text != "" && strings.TrimLeftFunc(text, isAlphanumericRune) == ""
}

// isAlphanumericRune reports whether the rune is an ASCII letter, a digit or an underscore.
func isAlphanumericRune(r rune) bool {
	return r < utf8.RuneSelf && (unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_')
}

// parseNumber parses a number as the game writes one, a `0x` value as its integer. It returns false for any other
// text, and for a number with a `%`, which IsNumber accepts.
func parseNumber(text string) (float64, bool) {
	if !IsNumber(text) {
		return 0, false
	}
	if strings.HasPrefix(text, "0x") {
		integer, err := strconv.ParseInt(text, 0, 64)
		return float64(integer), err == nil
	}
	number, err := strconv.ParseFloat(text, 64)
	return number, err == nil
}

// resolveConstant returns the number of the last `@name = value` with the given name that ends at or before the given
// offset. It returns false if there is none or its value is no number.
func (tree *Tree) resolveConstant(name string, offset int) (float64, bool) {
	declared := "@" + name
	count := sort.Search(len(tree.Statements), func(i int) bool { return tree.Statements[i].Span.Start >= offset })
	for index := count - 1; index > 0; index-- {
		statement := tree.Statements[index]
		if statement.Span.End <= offset && tree.Text(statement.Key) == declared && tree.Text(statement.Op) == "=" {
			return tree.ResolveNumber(statement.Value)
		}
	}
	return 0, false
}

// splitExpression splits an expression @[…] into the span of each token, leaving out white space. The given offset is
// where the text starts in its file. It returns nothing for a text that is no expression.
func splitExpression(text string, base int) []Span {
	inner, ok := strings.CutPrefix(text, "@[")
	if !ok {
		return nil
	}
	inner, ok = strings.CutSuffix(inner, "]")
	if !ok {
		return nil
	}
	var tokens []Span
	for offset := 0; offset < len(inner); {
		rest := inner[offset:]
		first, length := utf8.DecodeRuneInString(rest)
		switch {
		case unicode.IsSpace(first):
			offset += length
			continue
		case first == '.' || unicode.IsDigit(first):
			length = len(rest) - len(strings.TrimLeft(rest, "0123456789."))
		case isAlphanumericRune(first):
			length = len(rest) - len(strings.TrimLeftFunc(rest, isAlphanumericRune))
		}
		start := base + len("@[") + offset
		tokens = append(tokens, Span{Start: start, End: start + length})
		offset += length
	}
	return tokens
}

// evaluate computes the expression from the given token on, as far as its operators bind at least as tightly as the
// given precedence. It returns the number and the index of the token after the computed part, or false for anything it
// cannot compute, such as a division by zero.
func (tree *Tree) evaluate(tokens []Span, cursor, precedence int) (float64, int, bool) {
	left, cursor, ok := tree.evaluateOperand(tokens, cursor)
	for ok && cursor < len(tokens) {
		operator := tree.Text(tokens[cursor])
		if precedences[operator] < precedence {
			break
		}
		var right float64
		right, cursor, ok = tree.evaluate(tokens, cursor+1, precedences[operator]+1)
		switch {
		case !ok: // the loop ends on it
		case operator == "+":
			left += right
		case operator == "-":
			left -= right
		case operator == "*":
			left *= right
		case right == 0:
			ok = false
		default:
			left /= right
		}
	}
	return left, cursor, ok
}

// evaluateOperand computes the operand that starts at the given token. It returns the number and the index of the token
// after it.
func (tree *Tree) evaluateOperand(tokens []Span, cursor int) (float64, int, bool) {
	if cursor >= len(tokens) {
		return 0, cursor, false
	}
	text := tree.Text(tokens[cursor])
	switch text {
	case "-":
		number, next, ok := tree.evaluateOperand(tokens, cursor+1)
		return -number, next, ok
	case "(":
		number, next, ok := tree.evaluate(tokens, cursor+1, 1)
		if !ok || next >= len(tokens) || tree.Text(tokens[next]) != ")" {
			return 0, next, false
		}
		return number, next + 1, true
	}
	if number, ok := parseNumber(text); ok {
		return number, cursor + 1, true
	}
	number, ok := tree.resolveConstant(text, tokens[0].Start)
	return number, cursor + 1, ok
}

// matchSegments reports whether keys fit the segments of a key path.
func matchSegments(segments, keys []string) bool {
	if len(segments) == 0 {
		return len(keys) == 0
	}
	if segments[0] == "**" {
		for skip := 0; skip <= len(keys); skip++ {
			if matchSegments(segments[1:], keys[skip:]) {
				return true
			}
		}
		return false
	}
	if len(keys) == 0 {
		return false
	}
	fits, _ := path.Match(segments[0], keys[0])
	if segments[0] == NumberKey {
		fits = IsNumberKey(keys[0])
	}
	return fits && matchSegments(segments[1:], keys[1:])
}
