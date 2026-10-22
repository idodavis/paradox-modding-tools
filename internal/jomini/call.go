package jomini

import (
	"strings"
	"unicode"

	"github.com/samber/lo"
)

// A Call is one data call as a GUI file or a localization text writes it between brackets, such as
// `Root.GetCharacter.GetName`. Every span is in the file's own offsets.
type Call struct {
	Steps  []Step `json:"steps"`
	Suffix string `json:"suffix,omitempty"` // the format a localization call writes after `|`, such as U, E or %
	Span   Span   `json:"span"`
}

// A Step is one step of a data call, with its arguments and any cast written before it, such as `(int_bool)`.
type Step struct {
	Text      string     `json:"text"`
	Span      Span       `json:"span"`
	Arguments []Argument `json:"arguments,omitempty"`
	Cast      string     `json:"cast,omitempty"`
}

// An Argument is one argument of a step: a quoted text, a number or a nested call.
type Argument struct {
	Text   string `json:"text"`
	Span   Span   `json:"span"`
	Quoted bool   `json:"quoted"`
	Call   *Call  `json:"call,omitempty"`
}

// ParseCall reads the text of one data call, given without its brackets. The second argument is the offset of the
// text's first byte in its file, so the spans come out in the file's offsets. It returns false if the text is not
// a call.
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

// countSpaces returns how many bytes of white space run from the given offset.
func countSpaces(text string, offset int) int {
	return len(text[offset:]) - len(strings.TrimLeftFunc(text[offset:], unicode.IsSpace))
}

// parseStep reads one step at the given offset: a cast in parentheses if one is written, the step's name, and its
// argument list if one follows. It returns the step and the offset after it, or false where no name starts there.
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
		step.Arguments = lo.FilterMap(spans, func(span Span, _ int) (Argument, bool) {
			return parseArgument(text, span, base)
		})
		end = next
	}
	return step, end, true
}

// splitArguments splits the argument list at the given `(` into the span of each argument's text, commas and
// parentheses inside nested parentheses or single quotes kept, and returns the offset after the list's `)`. A list
// never closed runs to the text's end.
func splitArguments(text string, cursor int) ([]Span, int) {
	var spans []Span
	depth, quoted, start := 0, false, cursor+1
	for i := cursor; i < len(text); i++ {
		switch ch := text[i]; {
		case ch == '\'':
			quoted = !quoted
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

// parseArgument reads one argument from the given span of the text, the white space around it dropped: a text in
// single quotes, a number, or a nested call. It returns false where the span holds only white space.
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
	case IsNumber(raw):
	default:
		if call, ok := ParseCall(raw, base+start); ok {
			argument.Call = &call
		}
	}
	return argument, true
}

// SubstituteCall returns a call that a data binding macro's text makes, with each step that is a parameter replaced
// by the steps of the argument written for it, by the parameter's name, and the spans that are the macro's own
// emptied.
func SubstituteCall(call Call, given map[string]Argument) Call {
	var steps []Step
	for _, step := range call.Steps {
		argument, replaced := given[step.Text]
		switch {
		case replaced && argument.Call != nil:
			steps = append(steps, argument.Call.Steps...)
		case replaced:
			steps = append(steps, Step{Text: argument.Text, Span: argument.Span})
		default:
			step.Span = Span{}
			for i, inner := range step.Arguments {
				step.Arguments[i] = substituteArgument(inner, given)
			}
			steps = append(steps, step)
		}
	}
	return Call{Steps: steps, Suffix: call.Suffix}
}

// substituteArgument returns an argument of a macro's call with the argument written for it in its place if it is a
// parameter, else with the spans that are the macro's own emptied and the call inside it substituted.
func substituteArgument(argument Argument, given map[string]Argument) Argument {
	if value, ok := given[argument.Text]; ok && !argument.Quoted {
		return value
	}
	argument.Span = Span{}
	if argument.Call != nil {
		call := SubstituteCall(*argument.Call, given)
		argument.Call = &call
	}
	return argument
}
