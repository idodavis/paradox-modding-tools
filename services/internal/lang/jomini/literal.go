// literal.go reads the words that are values rather than names: a number, and a Clausewitz date. A two-part dotted word is an event id and never a date, and a date is never a name, because history files key their blocks by one.
package jomini

import (
	"strconv"
	"strings"
)

// LiteralKind is what a scalar is when it is not a name.
type LiteralKind uint8

const (
	NotLiteral LiteralKind = iota
	LitNumber              // an integer or decimal, signed or not
	LitDate                // Y.M.D, optionally Y.M.D.H
)

// Literal reads a scalar: a date through ParseDate, a number by its characters, else a name. A number is digits with at most one dot and a leading sign, which is all script writes; strconv would also take inf, nan, 0x10 and 1e5, and those are names.
func Literal(text string) LiteralKind {
	if _, _, _, _, ok := ParseDate(text); ok {
		return LitDate
	}

	digits, dots := 0, 0
	for i := 0; i < len(text); i++ {
		switch c := text[i]; {
		case c >= '0' && c <= '9':
			digits++
		case c == '.':
			dots++
		case (c == '-' || c == '+') && i == 0:
		default:
			return NotLiteral
		}
	}
	if digits > 0 && dots <= 1 {
		return LitNumber
	}
	return NotLiteral
}

// ParseDate reads a Clausewitz date, `Y.M.D` or `Y.M.D.H`, every part digits. Two parts is an event id (`ns.1`), never a date; history files key their blocks by a date, so a date is never a definition name.
func ParseDate(text string) (y, m, d, h int, ok bool) {
	parts := strings.Split(text, ".")
	if len(parts) != 3 && len(parts) != 4 {
		return 0, 0, 0, 0, false
	}
	out := [4]int{}
	for i, p := range parts {
		if p == "" || len(p) > 5 || !IsDigits(p) {
			return 0, 0, 0, 0, false
		}
		out[i], _ = strconv.Atoi(p)
	}
	return out[0], out[1], out[2], out[3], true
}

// IsDigits reports a non-empty run of ASCII digits.
func IsDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return s != ""
}
