package jomini

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
func ParseCall(text string, base int) (Call, bool) { panic("stub") }
