package jomini

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/samber/lo"
)

// The shapes a written key or value can have, one predicate each. Any package that needs to know what a piece of
// text looks like asks here.

// IsIdentifier reports whether the text is a plain identifier: letters, digits and the characters `_ - . ' :`,
// with no spaces and no quotes, and not a number.
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
// fraction, or a percentage.
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

// IsParameter reports whether the text is a scripted parameter, `$NAME$`, with or without a `|default`.
func IsParameter(text string) bool {
	inner, ok := strings.CutPrefix(text, "$")
	if !ok {
		return false
	}
	inner, ok = strings.CutSuffix(inner, "$")
	if !ok || strings.Contains(inner, "$") {
		return false
	}
	name, _, _ := strings.Cut(inner, "|")
	return isAlphanumeric(name)
}

// IsConstant reports whether the text is a script constant, `@name`, or a constant expression, `@[…]`.
func IsConstant(text string) bool {
	if strings.HasPrefix(text, "@[") {
		return strings.HasSuffix(text, "]")
	}
	return strings.HasPrefix(text, "@") && IsIdentifier(text[1:])
}

// A Date is a date as the game writes one, such as `1066.9.15`. Month runs from 1 to 12.
type Date struct {
	Year  int `json:"year"`
	Month int `json:"month"`
	Day   int `json:"day"`
}

// ParseDate reads a date written as `year.month.day`. The month and the day may be left out. It returns false for
// anything else.
func ParseDate(text string) (Date, bool) {
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
// name.
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

// ColorParts reads a statement as a color and returns its tag and its numbers. The tag is rgb, hsv, hsv360 or
// hex, or "" for a bare block of numbers. It returns false if the statement is not a color.
func ColorParts(tree *Tree, statement int32) (tag string, parts []float64, ok bool) {
	tag = tree.Text(tree.Statements[statement].Tag)
	switch tag {
	case "", "rgb", "hsv", "hsv360", "hex":
	default:
		return "", nil, false
	}
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
	if tag == "" && (len(parts) < 3 || len(parts) > 4) {
		return "", nil, false
	}
	return tag, parts, true
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

// parseColorPart reads one bare value of a color block as a number, a `0x` value as its integer. It returns false
// for a keyed statement, a block or a value that is not a number.
func parseColorPart(tree *Tree, statement Statement) (float64, bool) {
	text := tree.Text(statement.Value)
	if !statement.IsListed() || !IsNumber(text) {
		return 0, false
	}
	if strings.HasPrefix(text, "0x") {
		integer, err := strconv.ParseInt(text, 0, 64)
		return float64(integer), err == nil
	}
	number, err := strconv.ParseFloat(text, 64)
	return number, err == nil
}
