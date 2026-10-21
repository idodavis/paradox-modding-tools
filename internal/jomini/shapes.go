package jomini

// The shapes a written key or value can have, one predicate each. Any package that needs to know what a piece of
// text looks like asks here.

// IsIdentifier reports whether the text is a plain identifier: letters, digits and the characters `_ - . ' :`,
// with no spaces and no quotes, and not a number.
func IsIdentifier(text string) bool { panic("stub") }

// IsNumber reports whether the text is a number as the game writes one: an optional sign, digits, an optional
// fraction, or a percentage.
func IsNumber(text string) bool { panic("stub") }

// IsEventID reports whether the text is an event id: a namespace, a dot and a number.
func IsEventID(text string) bool { panic("stub") }

// IsParameter reports whether the text is a scripted parameter, `$NAME$`, with or without a `|default`.
func IsParameter(text string) bool { panic("stub") }

// IsConstant reports whether the text is a script constant, `@name`, or a constant expression, `@[…]`.
func IsConstant(text string) bool { panic("stub") }

// A Date is a date as the game writes one, such as `1066.9.15`. Month runs from 1 to 12.
type Date struct {
	Year  int `json:"year"`
	Month int `json:"month"`
	Day   int `json:"day"`
}

// ParseDate reads a date written as `year.month.day`. The month and the day may be left out. It returns false for
// anything else.
func ParseDate(text string) (Date, bool) { panic("stub") }

// SplitPrefixed splits text written as `prefix:name` into the prefix and the name. It returns false if there is
// no prefix.
func SplitPrefixed(text string) (prefix, name string, ok bool) { panic("stub") }

// SplitChain splits a dotted chain such as `scope:x.var:y.z` into its links. Each link is a prefixed or a plain
// name.
func SplitChain(text string) []string { panic("stub") }

// ColorParts reads a statement as a color and returns its tag and its numbers. The tag is rgb, hsv, hsv360 or
// hex, or "" for a bare block of numbers. It returns false if the statement is not a color.
func ColorParts(tree *Tree, statement int32) (tag string, parts []float64, ok bool) { panic("stub") }
