// datacall.go reads a data call, `[Root.GetCountry.GetName]`, into its steps and their arguments and casts.

package jomini

import "strings"

// Call is one data call of a text: the span of its brackets and the steps read inside them.
type Call struct {
	Range
	Steps []CallStep
}

// CallStep is one step of a data call: a name, then its arguments when it has parentheses. A cast literal,
// `(int32)3`, is one step of its own: Name is its value and Cast its type.
type CallStep struct {
	Name  string
	Cast  string
	Args  []CallArg
	Range Range
}

// CallArg is one argument of a step, as written; Text has no quotes when Quoted.
type CallArg struct {
	Text   string
	Quoted bool
	Range  Range
}

// DataCalls is every data call of a text, each split into its steps; offset is where the text starts, which every
// range is shifted by.
func DataCalls(text string, offset int) []Call {
	var calls []Call
	for _, span := range Calls(text, offset) {
		steps := ParseCall(text[span.Start-offset:span.End-offset], span.Start)
		calls = append(calls, Call{Range: span, Steps: steps})
	}
	return calls
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
	rest, opened := strings.CutPrefix(strings.TrimSuffix(strings.TrimPrefix(text, "'"), "'"), "(")
	if !opened {
		return "", "", false
	}
	typ, value, closed := strings.Cut(rest, ")")
	if !closed || typ == "" || value == "" || identifierEnd(typ, "") != len(typ) {
		return "", "", false
	}
	return typ, value, true
}

// ParseCall reads one data call, with or without its brackets, into its steps; what does not parse
// ends the steps, so a half-typed call gives the steps before it. The call ends at a | outside
// parentheses and quotes: [glory|E] is the step glory, written with the format E. A cast literal is no call but one
// step of its own, so an argument written as one reads as it is: [(int32)1|E] is the step 1 cast to int32.
func ParseCall(text string, offset int) []CallStep {
	if strings.HasPrefix(text, "[") {
		text, offset = text[1:], offset+1
	}
	text = strings.TrimSuffix(text, "]")
	pos := skipBlanks(text, 0)
	if step, isCast := castStep(text[pos:], offset+pos); isCast {
		return []CallStep{step}
	}
	var steps []CallStep
	for {
		start := pos
		pos += identifierEnd(text[pos:], "")
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

// castStep reads text, which starts at offset, as a cast literal that only a format after a | outside quotes follows,
// into the one step it is: Name its value, Cast its type; false for any other text.
func castStep(text string, offset int) (CallStep, bool) {
	end := 0
	for end < len(text) && text[end] != '|' {
		if text[end] == '\'' || text[end] == '"' {
			end = pastQuote(text, end)
		} else {
			end++
		}
	}
	literal := strings.TrimRight(text[:min(end, len(text))], " \t")
	typ, value, isCast := Cast(literal)
	if !isCast {
		return CallStep{}, false
	}
	return CallStep{Name: value, Cast: typ, Range: Range{Start: offset, End: offset + len(literal)}}, true
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
