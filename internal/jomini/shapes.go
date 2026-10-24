package jomini

import (
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/samber/lo"
)

// precedences holds how tightly each operator of an expression binds. An operator that is not here binds nothing.
var precedences = map[string]int{"+": 1, "-": 1, "*": 2, "/": 2}

// The predicates below tell what shape a written key or value has.

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

// ParseDate parses a date written as `year.month.day`. A minus before the year makes it negative. A missing day is 0.
// It returns false for anything else, including a month outside 1 to 12 and a day outside 1 to 31.
func ParseDate(text string) (Date, bool) {
	text, negative := strings.CutPrefix(text, "-")
	parts := strings.Split(text, ".")
	if len(parts) < 2 || len(parts) > 3 {
		return Date{}, false
	}
	if !lo.EveryBy(parts, isDigits) {
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
		if date.Day < 1 || date.Day > 31 {
			return Date{}, false
		}
	}
	if date.Month < 1 || date.Month > 12 {
		return Date{}, false
	}
	return date, true
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

// SplitChain splits a dotted chain such as `scope:x.var:y.z` into its links. Each link is a prefixed or a plain
// name. A dot inside brackets or quotes does not split.
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

// ColorParts returns the tag and the numbers of a block statement such as `rgb { 1 2 3 }`. The tag is "" for a block
// with none, which must hold three or four numbers. It returns false if the statement is not a block of plain numbers.
func ColorParts(tree *Tree, statement int32) (tag string, parts []float64, ok bool) {
	if !tree.Statements[statement].IsBlock() {
		return "", nil, false
	}
	for child := range tree.Children(statement) {
		part, ok := parseColorPart(tree, tree.Statements[child])
		if !ok {
			return "", nil, false
		}
		parts = append(parts, part)
	}
	tag = tree.Text(tree.Statements[statement].Tag)
	if tag == "" && (len(parts) < 3 || len(parts) > 4) {
		return "", nil, false
	}
	return tag, parts, true
}

// ResolveNumber returns the number that a value in the file stands for. The value is a number, a constant declared
// earlier in the file, or an expression @[…] of numbers, constants, + − * / and brackets. It returns false for any
// other value, for an undeclared constant and for an expression it cannot compute.
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

// ListConstants returns the span of each constant that an expression @[…] uses, without the @. It returns nothing for
// a text that is no expression, or that holds anything but numbers, names, + − * / and brackets, such as a data call.
func ListConstants(text string, base int) []Span {
	var spans []Span
	for _, token := range splitExpression(text, base) {
		word := text[token.Start-base : token.End-base]
		switch {
		case IsNumber(word), precedences[word] > 0, word == "(", word == ")":
		case isAlphanumeric(word):
			spans = append(spans, token)
		default:
			return nil
		}
	}
	return spans
}

// isIdentifierRune reports whether the rune may be written in an identifier: a letter, a digit, one of `_ - . ' :`
// or any rune beyond ASCII.
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

// parseColorPart parses one bare value of a color block as a number. It returns false for a keyed statement, a block
// or a value that is not a number.
func parseColorPart(tree *Tree, statement Statement) (float64, bool) {
	if !statement.IsListed() {
		return 0, false
	}
	return parseNumber(tree.Text(statement.Value))
}

// parseNumber parses a number as the game writes one, a `0x` value as its integer. It returns false for any other
// text, and for a number with a `%`.
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

// resolveConstant returns the number of the last constant with the given name, declared as `@name = value` at any
// depth, in a statement that ends before the given offset. It returns false if there is none or its value is no number.
func (tree *Tree) resolveConstant(name string, before int) (float64, bool) {
	declared := "@" + name
	after := sort.Search(len(tree.Statements), func(i int) bool { return tree.Statements[i].Span.Start >= before })
	for index := after - 1; index > 0; index-- {
		statement := tree.Statements[index]
		if statement.Span.End <= before && tree.Text(statement.Key) == declared && tree.Text(statement.Op) == "=" {
			return tree.ResolveNumber(statement.Value)
		}
	}
	return 0, false
}

// splitExpression splits an expression @[…] into the span of each token, without white space. A token is a run of
// digits and dots, a run of letters, digits and underscores, or one other character. The base is where the text's first
// byte sits in its file. It returns nothing for a text that is no expression.
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

// evaluate computes the tokens from the given token on, as far as the operators bind at least as tightly as the given
// precedence. It returns the number and the index of the token after them. It returns false for anything it cannot
// compute, such as a division by zero.
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
		case !ok:
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

// evaluateOperand computes one operand of an expression at the given token: a number, a constant, a bracketed
// expression or an operand with a minus before it. It returns the number and the index of the token after it.
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
