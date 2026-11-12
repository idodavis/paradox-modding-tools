// shapes.go gives a written word its class and reads its parts. A base is
// where a text starts in its file, and the spans returned are the file's.

package jomini

import (
	"path"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Class is what a key, a tag or a value is written as, inside any quotes.
type Class uint8

// The package doc says what each class is written as.
const (
	// ClassNone is a part a statement does not write, and an empty text.
	ClassNone Class = iota

	ClassIdentifier
	ClassNumber
	ClassDate
	ClassEventID
	ClassChain
	ClassParenthesized
	ClassParameter
	ClassConstant
	ClassExpression
	ClassCall
	ClassText
)

// Link is one link of a chain, as scope:x in scope:x.father.
type Link struct {
	Span Span

	// Prefix and Name are the sides of the first colon, as scope and x in
	// scope:x. Both are empty if either side has no text.
	Prefix Span
	Name   Span

	// Argument is the text in the link's parentheses, as scope:ruler in
	// opinion(scope:ruler). It is empty for a link with none.
	Argument Span
}

// Parameter is one parameter in a text, as $AMOUNT$ or $AMOUNT|1$.
type Parameter struct {
	Span Span

	// Name is the name without the $, as AMOUNT.
	Name Span

	// Default is the text after the bar, as 1 in $AMOUNT|1$. It is empty
	// without a bar, and in $AMOUNT|$.
	Default Span
}

// Date is a date as the game writes one, such as 1066.9.15.
type Date struct {
	Year  int `json:"year"`
	Month int `json:"month"`
	Day   int `json:"day"`
}

// NumberKey matches a key written as a number or a date, as in random_list.
const NumberKey = "<number>"

var precedences = map[string]int{"+": 1, "-": 1, "*": 2, "/": 2}

// Classify returns what a text is written as, trying the classes in the
// package doc's order. It returns ClassNone for "".
func Classify(text string) Class {
	switch {
	case text == "":
		return ClassNone
	case len(Parameters(text, 0)) > 0:
		return ClassParameter
	case strings.HasPrefix(text, "@[") && strings.HasSuffix(text, "]"):
		return ClassExpression
	case strings.HasPrefix(text, "@") && Classify(text[1:]).IsIdentifier():
		return ClassConstant
	case strings.HasPrefix(text, "[") && strings.HasSuffix(text, "]"):
		return ClassCall
	case isNumber(text):
		return ClassNumber
	case isDate(text):
		return ClassDate
	case strings.TrimLeftFunc(text, isIdentifierRune) == "":
		return classifyIdentifier(text)
	}

	if _, ok := ParseParenthesized(text, 0); ok {
		return ClassParenthesized
	}

	return ClassText
}

// IsIdentifier reports whether the class is an identifier, chain or event id.
func (c Class) IsIdentifier() bool {
	return c == ClassIdentifier || c == ClassChain || c == ClassEventID
}

// IsNumberKey reports whether the class is one NumberKey stands for.
func (c Class) IsNumberKey() bool {
	return c == ClassNumber || c == ClassDate
}

// isNumber reports whether a text is a number, with 0x values, a % and C
// floats such as 3.0f.
func isNumber(text string) bool {
	if hex, ok := strings.CutPrefix(text, "0x"); ok {
		return hex != "" && strings.Trim(hex, "0123456789abcdefABCDEF") == ""
	}

	text = strings.TrimSuffix(text, "%")
	if text != "" && (text[0] == '-' || text[0] == '+') {
		text = text[1:]
	}

	whole, fraction, dotted := strings.Cut(text, ".")
	fraction = strings.TrimSuffix(fraction, "f")

	return isDigits(whole) && (!dotted || isDigits(fraction))
}

func isDate(text string) bool {
	_, ok := ParseDate(text)

	return ok
}

// classifyIdentifier returns an event id such as my_events.1, a chain if the
// text holds a dot or a colon, else an identifier.
func classifyIdentifier(text string) Class {
	namespace, id, dotted := strings.Cut(text, ".")

	switch {
	case dotted && isAlphanumeric(namespace) && isDigits(id):
		return ClassEventID
	case strings.ContainsAny(text, ".:"):
		return ClassChain
	}

	return ClassIdentifier
}

// ParseParenthesized parses one name with one argument in parentheses, as
// opinion(scope:ruler). It returns false for any other text.
func ParseParenthesized(text string, base int) (Step, bool) {
	if !strings.HasSuffix(text, ")") {
		return Step{}, false
	}

	call, ok := ParseCall(text, base)
	if !ok || len(call.Steps) != 1 || len(call.Steps[0].Arguments) != 1 || call.Span.End != base+len(text) {
		return Step{}, false
	}

	return call.Steps[0], true
}

// ParseDate parses year.month.day. A missing day is 0. The ranges are
// IsValid's. It returns false for any other shape.
func ParseDate(text string) (Date, bool) {
	text, negative := strings.CutPrefix(text, "-")
	parts := strings.Count(text, ".") + 1
	if parts < 2 || parts > 3 {
		return Date{}, false
	}

	var numbers [3]int
	for i := range parts {
		part, rest, _ := strings.Cut(text, ".")
		if !isDigits(part) {
			return Date{}, false
		}

		numbers[i], _ = strconv.Atoi(part)
		text = rest
	}

	date := Date{Year: numbers[0], Month: numbers[1], Day: numbers[2]}
	if negative {
		date.Year = -date.Year
	}

	return date, true
}

// IsValid reports whether the month is 1 to 12 and the day 0 to 31, where 0 is
// no day.
func (d Date) IsValid() bool {
	return d.Month >= 1 && d.Month <= 12 && d.Day >= 0 && d.Day <= 31
}

// Links splits a chain at its dots, as root.father into root and father. A dot
// in brackets or quotes does not split. "" is one empty link.
func Links(text string, base int) []Link {
	var links []Link
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
				links = append(links, parseLink(text[start:i], base+start))
				start = i + 1
			}
		}
	}

	return append(links, parseLink(text[start:], base+start))
}

func parseLink(text string, start int) Link {
	link := Link{Span: Span{Start: start, End: start + len(text)}}

	// a colon inside the parentheses is the argument's own
	head := text
	if open := strings.IndexByte(text, '('); open > 0 && strings.HasSuffix(text, ")") {
		link.Argument = Span{Start: start + open + 1, End: link.Span.End - 1}
		head = text[:open]
	}

	if prefix, name, ok := strings.Cut(head, ":"); ok && prefix != "" && name != "" {
		link.Prefix = Span{Start: start, End: start + len(prefix)}
		link.Name = Span{Start: link.Prefix.End + 1, End: link.Span.End}
	}

	return link
}

// Parameters returns each $NAME$ or $NAME|default$ in a text. A name is ASCII
// letters, digits, underscores and %, as in $SUCCESS_%$. A $ that starts no
// parameter is skipped.
func Parameters(text string, base int) []Parameter {
	var parameters []Parameter

	for cursor := 0; cursor < len(text); {
		start := strings.IndexByte(text[cursor:], '$')
		if start < 0 {
			break
		}

		start += cursor
		parameter, ok := parseParameter(text, start, base)
		if !ok {
			cursor = start + 1

			continue
		}

		parameters = append(parameters, parameter)
		cursor = parameter.Span.End - base
	}

	return parameters
}

// parseParameter parses the parameter whose $ is at the given offset, or
// returns false.
func parseParameter(text string, start, base int) (Parameter, bool) {
	end := len(text) - len(strings.TrimLeftFunc(text[start+1:], func(r rune) bool {
		return isAlphanumericRune(r) || r == '%'
	}))
	if end == start+1 || end == len(text) {
		return Parameter{}, false
	}

	parameter := Parameter{Name: Span{Start: base + start + 1, End: base + end}}

	switch text[end] {
	case '$':
		parameter.Span = Span{Start: base + start, End: base + end + 1}

	case '|':
		closing := strings.IndexByte(text[end+1:], '$')
		if closing < 0 {
			return Parameter{}, false
		}

		parameter.Default = Span{Start: base + end + 1, End: base + end + 1 + closing}
		parameter.Span = Span{Start: base + start, End: parameter.Default.End + 1}

	default:
		return Parameter{}, false
	}

	return parameter, true
}

// MatchKeyPath reports whether the keys above a statement fit a path of
// segments joined by /, each a glob, NumberKey, or ** for any depth. An empty
// path fits only the file's top.
func MatchKeyPath(pattern string, keys []string) bool {
	if pattern == "" {
		return len(keys) == 0
	}

	return matchSegments(strings.Split(pattern, "/"), keys)
}

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
		fits = Classify(keys[0]).IsNumberKey()
	}

	return fits && matchSegments(segments[1:], keys[1:])
}

// ResolveNumber returns the number a span's text stands for: a number, a
// constant declared earlier in the file, or an expression @[…] of them. It
// returns false for any other text or an expression it cannot compute.
func (t *Tree) ResolveNumber(span Span) (float64, bool) {
	text := t.Text(span)
	if number, ok := parseNumber(text); ok {
		return number, true
	}

	if Classify(text) == ClassConstant {
		return t.resolveConstant(text[1:], span.Start)
	}

	tokens := splitExpression(text, span.Start)
	if len(tokens) == 0 {
		return 0, false
	}

	number, cursor, ok := t.evaluate(tokens, 0, 1)

	return number, ok && cursor == len(tokens)
}

// parseNumber parses a number. It returns false for other text and for a
// number with a %, which isNumber accepts.
func parseNumber(text string) (float64, bool) {
	if !isNumber(text) {
		return 0, false
	}

	if strings.HasPrefix(text, "0x") {
		integer, err := strconv.ParseInt(text, 0, 64)

		return float64(integer), err == nil
	}

	number, err := strconv.ParseFloat(strings.TrimSuffix(text, "f"), 64)

	return number, err == nil
}

// resolveConstant returns the number of the last @name = value that ends at
// or before the given offset. It returns false if there is none or it is no
// number.
func (t *Tree) resolveConstant(name string, offset int) (float64, bool) {
	declared := "@" + name
	count := sort.Search(len(t.Statements), func(i int) bool { return t.Statements[i].Span.Start >= offset })

	for statement := count - 1; statement > 0; statement-- {
		current := t.Statements[statement]
		if current.Span.End <= offset && t.Text(current.Key) == declared && t.Text(current.Op) == "=" {
			return t.ResolveNumber(current.Value)
		}
	}

	return 0, false
}

// ListConstants returns the span of each constant in an expression @[…],
// without the @. It returns nothing if the text holds anything but numbers,
// names, operators and brackets.
func ListConstants(text string, base int) []Span {
	var spans []Span

	for _, span := range splitExpression(text, base) {
		part := text[span.Start-base : span.End-base]

		switch {
		// A number, an operator or a bracket names no constant.
		case isNumber(part), precedences[part] > 0, part == "(", part == ")":
		case isAlphanumeric(part):
			spans = append(spans, span)
		default:
			return nil
		}
	}

	return spans
}

// splitExpression splits an expression @[…] into token spans, leaving out
// white space. It returns nothing for a text that is no expression.
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

// evaluate computes the expression from the given token while operators bind
// at least as tightly as the given precedence. It returns the number and the
// next token's index, or false for a part it cannot compute.
func (t *Tree) evaluate(tokens []Span, cursor, precedence int) (float64, int, bool) {
	left, cursor, ok := t.evaluateOperand(tokens, cursor)

	for ok && cursor < len(tokens) {
		operator := t.Text(tokens[cursor])
		if precedences[operator] < precedence {
			break
		}

		var right float64
		right, cursor, ok = t.evaluate(tokens, cursor+1, precedences[operator]+1)

		switch {
		// A part it cannot compute ends the loop.
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

// evaluateOperand computes the operand that starts at the given token. It
// returns the number and the next token's index.
func (t *Tree) evaluateOperand(tokens []Span, cursor int) (float64, int, bool) {
	if cursor >= len(tokens) {
		return 0, cursor, false
	}

	text := t.Text(tokens[cursor])
	switch text {
	case "-":
		number, next, ok := t.evaluateOperand(tokens, cursor+1)

		return -number, next, ok

	case "(":
		number, next, ok := t.evaluate(tokens, cursor+1, 1)
		if !ok || next >= len(tokens) || t.Text(tokens[next]) != ")" {
			return 0, next, false
		}

		return number, next + 1, true
	}

	if number, ok := parseNumber(text); ok {
		return number, cursor + 1, true
	}

	number, ok := t.resolveConstant(text, tokens[0].Start)

	return number, cursor + 1, ok
}

func isIdentifierRune(r rune) bool {
	return r >= utf8.RuneSelf || unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("_-.':", r)
}

func isDigits(text string) bool {
	return text != "" && strings.Trim(text, "0123456789") == ""
}

// isAlphanumeric reports whether the text is a run of ASCII letters, digits
// and underscores.
func isAlphanumeric(text string) bool {
	return text != "" && strings.TrimLeftFunc(text, isAlphanumericRune) == ""
}

func isAlphanumericRune(r rune) bool {
	return r < utf8.RuneSelf && (unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_')
}
