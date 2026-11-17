// call.go parses data calls and fills in a macro's call with its arguments.

package jomini

import (
	"slices"
	"strings"
	"unicode"
)

// Call is one data call, such as Root.GetCharacter.GetName.
type Call struct {
	Steps []Step `json:"steps"`

	// Suffix is the format after |, such as U or %. It is "" for none.
	Suffix string `json:"suffix,omitempty"`

	Span Span `json:"span"`
}

// Step is one step of a call, such as GetName.
type Step struct {
	Text      string     `json:"text"`
	Span      Span       `json:"span"`
	Arguments []Argument `json:"arguments,omitempty"`

	// Cast is the type before the step, as int_bool in
	// (int_bool)GetPlayer.IsAlive. It is "" for none.
	Cast string `json:"cast,omitempty"`
}

// Argument is one argument of a step.
type Argument struct {
	Text string `json:"text"`

	// Span is where the text is written, without any quotes.
	Span Span `json:"span"`

	Quoted bool `json:"quoted"`

	// Call is the nested call, or nil for a text or a number.
	Call *Call `json:"call,omitempty"`
}

// ParseBracketed parses a data call between brackets, such as [Root.GetName].
// The given base is where the text starts in its file. It returns false if the
// text is no call.
func ParseBracketed(text string, base int) (Call, bool) {
	inner, opens := strings.CutPrefix(text, "[")
	inner, closes := strings.CutSuffix(inner, "]")
	if !opens || !closes {
		return Call{}, false
	}

	return ParseCall(inner, base+1)
}

// ParseCall parses a data call without brackets, as ParseBracketed does.
func ParseCall(text string, base int) (Call, bool) {
	cursor := countSpaces(text, 0)
	call := Call{Span: Span{Start: base + cursor}}

	for cursor < len(text) {
		step, end, ok := parseStep(text, base, cursor)
		if !ok {
			break
		}

		call.Steps = append(call.Steps, step)
		call.Span.End = base + end
		cursor = end + countSpaces(text, end)

		if cursor < len(text) && text[cursor] == '|' {
			call.Suffix = strings.TrimSpace(text[cursor+1:])
			call.Span.End = base + len(text)

			break
		}

		if cursor >= len(text) || text[cursor] != '.' {
			break
		}

		cursor += 1 + countSpaces(text, cursor+1)
	}

	if len(call.Steps) == 0 {
		return Call{}, false
	}

	return call, true
}

func countSpaces(text string, offset int) int {
	return len(text[offset:]) - len(strings.TrimLeftFunc(text[offset:], unicode.IsSpace))
}

// parseStep parses the step at the cursor. It returns false if no name starts
// there.
func parseStep(text string, base, cursor int) (Step, int, bool) {
	var step Step
	if cursor < len(text) && text[cursor] == '(' {
		closing := strings.IndexByte(text[cursor:], ')')
		if closing < 0 {
			return Step{}, cursor, false
		}

		step.Cast = text[cursor+1 : cursor+closing]
		cursor += closing + 1
	}

	end := len(text) - len(strings.TrimLeftFunc(text[cursor:], isAlphanumericRune))
	if end == cursor {
		return Step{}, cursor, false
	}

	step.Text, step.Span = text[cursor:end], Span{Start: base + cursor, End: base + end}

	if end < len(text) && text[end] == '(' {
		spans, next := splitArguments(text, end)
		for _, span := range spans {
			if argument, ok := parseArgument(text, span, base); ok {
				step.Arguments = append(step.Arguments, argument)
			}
		}

		end = next
	}

	return step, end, true
}

// splitArguments splits the list that opens at the given ( into one span per
// argument. A list that never closes runs to the text's end.
func splitArguments(text string, cursor int) ([]Span, int) {
	var spans []Span
	depth, quoted, start := 0, false, cursor+1

	for i := cursor; i < len(text); i++ {
		switch ch := text[i]; {
		case ch == '\'':
			quoted = !quoted

		// Single quotes hide commas and parentheses.
		case quoted:

		case ch == '(':
			depth++

		case ch == ')' && depth > 1:
			depth--

		case ch == ')', ch == ',' && depth == 1:
			spans = append(spans, Span{Start: start, End: i})
			start = i + 1

			if ch == ')' {
				return spans, i + 1
			}
		}
	}

	return append(spans, Span{Start: start, End: len(text)}), len(text)
}

// parseArgument parses one argument. It returns false for white space.
func parseArgument(text string, span Span, base int) (Argument, bool) {
	start := span.Start + countSpaces(text[:span.End], span.Start)
	raw := strings.TrimRightFunc(text[start:span.End], unicode.IsSpace)
	if raw == "" {
		return Argument{}, false
	}

	argument := Argument{Text: raw, Span: Span{Start: base + start, End: base + start + len(raw)}}
	quoted := len(raw) >= 2 && raw[0] == '\'' && raw[len(raw)-1] == '\''

	switch {
	case quoted:
		argument.Text, argument.Quoted = raw[1:len(raw)-1], true
		argument.Span = Span{Start: argument.Span.Start + 1, End: argument.Span.End - 1}

	case isNumber(raw):

	default:
		if call, ok := ParseCall(raw, base+start); ok {
			argument.Call = &call
		}
	}

	return argument, true
}

// SubstituteCall returns a macro's call with each parameter replaced by the
// argument of its name. The spans of the macro's own text are emptied, as they
// lie in another file. The given call is not changed.
func SubstituteCall(call Call, arguments map[string]Argument) Call {
	var steps []Step

	for _, step := range call.Steps {
		argument, ok := arguments[step.Text]

		switch {
		case ok && argument.Call != nil:
			steps = append(steps, argument.Call.Steps...)

		case ok:
			steps = append(steps, Step{Text: argument.Text, Span: argument.Span})

		default:
			step.Span = Span{}
			step.Arguments = slices.Clone(step.Arguments)
			for i, inner := range step.Arguments {
				step.Arguments[i] = substituteArgument(inner, arguments)
			}

			steps = append(steps, step)
		}
	}

	return Call{Steps: steps, Suffix: call.Suffix}
}

func substituteArgument(argument Argument, arguments map[string]Argument) Argument {
	if value, ok := arguments[argument.Text]; ok && !argument.Quoted {
		return value
	}

	argument.Span = Span{}
	if argument.Call != nil {
		call := SubstituteCall(*argument.Call, arguments)
		argument.Call = &call
	}

	return argument
}
