// datacall.go reads a data call, `[Root.GetCountry.GetName]`, into its steps and their arguments.

package jomini

import "strings"

// CallStep is one step of a data call: a name, then its arguments when it has parentheses.
type CallStep struct {
	Name  string
	Args  []CallArg
	Range Range
}

// CallArg is one argument of a step, as written; Text has no quotes when Quoted.
type CallArg struct {
	Text   string
	Quoted bool
	Range  Range
}

// Calls is every [ … ] segment of text, brackets included, nested brackets kept inside their
// segment; an unclosed one runs to the end. Ranges are offset by offset.
func Calls(text string, offset int) []Range {
	var calls []Range
	depth, open := 0, 0
	for i := 0; i < len(text); i++ {
		switch text[i] {
		case '[':
			if depth == 0 {
				open = i
			}
			depth++
		case ']':
			if depth == 0 {
				continue
			}
			if depth--; depth == 0 {
				calls = append(calls, Range{Start: offset + open, End: offset + i + 1})
			}
		}
	}
	if depth > 0 {
		calls = append(calls, Range{Start: offset + open, End: offset + len(text)})
	}
	return calls
}

// Cast reads a cast literal, '(int32)3' or (CFixedPoint)0, into its type and value; false for any other text.
func Cast(text string) (typ, value string, ok bool) {
	text = strings.TrimSuffix(strings.TrimPrefix(text, "'"), "'")
	rest, opened := strings.CutPrefix(text, "(")
	typ, value, closed := strings.Cut(rest, ")")
	if !opened || !closed || typ == "" || value == "" {
		return "", "", false
	}
	for i := range len(typ) {
		if !isAlnumUnder(typ[i]) {
			return "", "", false
		}
	}
	return typ, value, true
}

// ParseCall reads one data call, with or without its brackets, into its steps; what does not parse
// ends the steps, so a half-typed call gives the steps before it. The call ends at a | outside
// parentheses and quotes: [glory|E] is the step glory, written with the format E.
func ParseCall(text string, offset int) []CallStep {
	if strings.HasPrefix(text, "[") {
		text, offset = text[1:], offset+1
	}
	text = strings.TrimSuffix(text, "]")
	var steps []CallStep
	pos := skipBlanks(text, 0)
	for {
		start := pos
		for pos < len(text) && isAlnumUnder(text[pos]) {
			pos++
		}
		if pos == start {
			return steps
		}
		step := CallStep{Name: text[start:pos], Range: Range{Start: offset + start, End: offset + pos}}

		// its arguments, when a parenthesis follows; one that does not close ends the call
		if next := skipBlanks(text, pos); next < len(text) && text[next] == '(' {
			var closed bool
			step.Args, pos, closed = callArgs(text, next+1, offset)
			step.Range.End = offset + pos
			if !closed {
				return append(steps, step)
			}
		}
		steps = append(steps, step)
		if pos = skipBlanks(text, pos); pos >= len(text) || text[pos] != '.' {
			return steps
		}
		pos = skipBlanks(text, pos+1)
	}
}

// callArgs reads a step's arguments from just after its ( to past its ); closed is false when the
// list runs out first.
func callArgs(text string, pos, offset int) (args []CallArg, end int, closed bool) {
	for {
		if pos = skipBlanks(text, pos); pos >= len(text) {
			return args, pos, false
		}
		if text[pos] == ')' {
			return args, pos + 1, true
		}

		// a quoted argument, or everything to a , or ) outside nested parentheses and quotes
		start := pos
		if quote := text[pos]; quote == '\'' || quote == '"' {
			if pos = pastQuote(text, pos); pos > len(text) {
				return args, len(text), false
			}
			args = append(args, CallArg{Text: text[start+1 : pos-1], Quoted: true, Range: Range{Start: offset + start, End: offset + pos}})
		} else {
			depth := 0
			for pos < len(text) && (depth > 0 || text[pos] != ',' && text[pos] != ')') {
				switch text[pos] {
				case '(':
					depth++
				case ')':
					depth--
				case '\'', '"':
					pos = pastQuote(text, pos) - 1
				}
				pos++
			}
			if pos >= len(text) {
				return args, len(text), false
			}
			arg := strings.TrimRight(text[start:pos], " \t")
			args = append(args, CallArg{Text: arg, Range: Range{Start: offset + start, End: offset + start + len(arg)}})
		}

		if pos = skipBlanks(text, pos); pos < len(text) && text[pos] == ',' {
			pos++
			continue
		}
		if pos < len(text) && text[pos] == ')' {
			return args, pos + 1, true
		}
		return args, pos, false
	}
}

// pastQuote is the offset past the quote that closes the one at text[pos], a backslash escaping the
// byte after it; past the end when none closes it.
func pastQuote(text string, pos int) int {
	quote := text[pos]
	for pos++; pos < len(text) && text[pos] != quote; pos++ {
		if text[pos] == '\\' {
			pos++
		}
	}
	return pos + 1
}

// skipBlanks is the offset of the first byte at or after pos that is no space or tab.
func skipBlanks(text string, pos int) int {
	for pos < len(text) && (text[pos] == ' ' || text[pos] == '\t') {
		pos++
	}
	return pos
}
