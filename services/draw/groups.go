// groups.go turns what a window's conditions ask about into the groups of the
// States panel: their options, captions and boxes, and what each decides.

package draw

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/internal/loc"
)

// Group is one thing the page lets the user switch: a data call that a
// window's conditions ask about, or a list a coat's value is drawn from.
type Group struct {
	// ID is the group's call as text. A call about the object of a data
	// context is written from the window down to that object, so the same
	// call about two objects is two groups.
	ID string `json:"id"`

	// Subject is the kind of object the group asks about, such as Character.
	// It is Game for a call of one step, Pictures for a list of pictures, and
	// "" for a coat's list.
	Subject string `json:"subject,omitempty"`

	// Key picks the group's object out of its kind, as lover picks a
	// relation.
	Key string `json:"key,omitempty"`

	// Label is what the group asks, in words.
	Label string `json:"label"`

	// Exists is whether the group asks if an object beyond its subject
	// exists.
	Exists bool `json:"exists,omitempty"`

	// Options are the values the window asks of the group, then every value
	// at once where it asks none or several, then none.
	Options []string `json:"options"`

	// Captions is how the panel writes each option.
	Captions map[string]string `json:"captions,omitempty"`

	// Box is the box of the States tab the group starts in.
	Box Box `json:"box"`

	// Picked is the option a coat's list draws now. A window's groups keep
	// their values in States.Values instead.
	Picked string `json:"picked,omitempty"`

	// Clickable is whether a press of one of the window's widgets sets the
	// group.
	Clickable bool `json:"clickable,omitempty"`

	// Reach is how many drawing widgets the group shows or hides.
	Reach int `json:"reach,omitempty"`

	// Item is the data model item whose rows alone ask about the group, 0 for
	// the window.
	Item int32 `json:"item,omitempty"`

	// Readers are the widgets whose own condition asks about the group.
	Readers []int32 `json:"readers,omitempty"`

	// Pictures is the list a group of a picture binding picks among, nil for
	// any other group.
	Pictures *PictureList `json:"pictures,omitempty"`

	// call is the group's call. A group of pictures has none.
	call jomini.Call
}

// Box is one of the boxes the States tab sorts a window's groups into.
type Box struct {
	ID    string `json:"id"`
	Title string `json:"title"`

	// Kind is "window" for the window's own groups, "has" for what an object
	// has, "object" for an object's, and "item" for a list's rows. The page
	// draws each kind its own way.
	Kind string `json:"kind"`
}

// pickCaptions is how the panel writes the picks for every value and none.
var pickCaptions = map[string]string{pickEvery: "All", pickNone: "None"}

// directSteps is the most steps a call has when a window or a row asks about
// its object directly: the start, the step to the object, and the check.
const directSteps = 3

// listStrips returns, for each widget in a strip, the widgets of its strip in
// order. A strip is two or more tabs of one type written side by side.
func (e *evaluator) listStrips() map[int32][]int32 {
	var kinds []string
	byKind := map[string][]int32{}
	for _, widget := range e.window.Widgets[1:] {
		if !e.isTab(widget) {
			continue
		}

		kind := fmt.Sprint(widget.Parent, " ", widget.Type)
		if byKind[kind] == nil {
			kinds = append(kinds, kind)
		}

		byKind[kind] = append(byKind[kind], widget.ID)
	}

	strips := map[int32][]int32{}
	for _, kind := range kinds {
		strip := byKind[kind]
		if len(strip) < 2 {
			continue
		}

		for _, id := range strip {
			strips[id] = strip
		}
	}

	return strips
}

// isTab reports whether a widget is a tab: a condition about one of the
// window's groups draws it down, or a click on it selects a value of one.
func (e *evaluator) isTab(widget Widget) bool {
	context := e.contexts[widget.ID]
	// a widget that writes down as a data call has it as its press condition
	_, down := e.window.findCall(widget, downKey)
	if press := e.rules[widget.ID].press; down != nil && press != nil {
		return slices.ContainsFunc(press.listFacts(nil), func(fact *condition) bool {
			return slices.ContainsFunc(e.groups, func(group Group) bool { return group.ID == fact.group })
		})
	}

	return slices.ContainsFunc(listCalls(&e.window, widget, clickKeys...), func(call jomini.Call) bool {
		_, _, ok := e.findSelected(context.expand(e.schema, call))

		return ok
	})
}

// classifyGroups fills in what each group is to its window: the item it
// belongs to, how many widgets it shows or hides, and whether a press sets
// it. A widget hidden whatever the picks counts for nothing.
func classifyGroups(evaluator *evaluator) {
	groups, widgets := evaluator.groups, evaluator.window.Widgets
	items, ends := listItems(widgets), make([]int32, len(widgets))
	hidden := make([]bool, len(widgets))
	for _, widget := range widgets {
		holds, known := evaluator.rules[widget.ID].shown.evaluate(nil)
		hidden[widget.ID] = known && !holds
	}

	for _, widget := range slices.Backward(widgets) {
		ends[widget.ID] = max(ends[widget.ID], widget.ID+1)
		ends[widget.Parent] = max(ends[widget.Parent], ends[widget.ID])
	}

	captions := listPresses(evaluator, ends)
	for position := range groups {
		group := &groups[position]
		var holders []int32
		drawing := map[int32]bool{}
		for _, reader := range group.Readers {
			holders = appendNew(holders, items[reader])
			for id := reader; id < ends[reader]; id++ {
				if isDrawing(&evaluator.window, widgets[id]) && !hidden[id] {
					drawing[id] = true
				}
			}
		}

		if len(holders) == 1 {
			group.Item = holders[0]
		}

		group.Reach = len(drawing)
		group.Captions, group.Clickable = captions[group.ID], captions[group.ID] != nil
	}
}

// listItems returns, for each widget, the data model item it is drawn in, 0
// for none.
func listItems(widgets []Widget) []int32 {
	items := make([]int32, len(widgets))
	for _, widget := range widgets {
		items[widget.ID] = items[widget.Parent]
		if widget.Item {
			items[widget.ID] = widget.ID
		}
	}

	return items
}

// listPresses returns, by group and then by value, the text of the first
// widget whose press sets that value. ends holds, for each widget, the ID
// after the last widget inside it.
func listPresses(evaluator *evaluator, ends []int32) map[string]map[string]string {
	widgets := evaluator.window.Widgets
	captions := map[string]map[string]string{}
	for _, widget := range widgets {
		wanted := map[string]string{}
		evaluator.press(wanted, widget.ID)
		if len(wanted) == 0 {
			continue
		}

		text := ""
		inside := widgets[widget.ID:ends[widget.ID]]
		if position := slices.IndexFunc(inside, func(under Widget) bool { return len(under.Texts) > 0 }); position >= 0 {
			text = toCaption(inside[position])
		}

		for group, value := range wanted {
			if captions[group] == nil {
				captions[group] = map[string]string{}
			}

			if _, has := captions[group][value]; !has && text != "" && value != pickEvery && value != pickNone {
				captions[group][value] = text
			}
		}
	}

	return captions
}

// toCaption returns a widget's texts as one caption, or "" where one of them
// shows a data call.
func toCaption(widget Widget) string {
	captions := make([]string, 0, len(widget.Texts))
	for _, text := range widget.Texts {
		if slices.ContainsFunc(text, func(segment loc.Segment) bool { return segment.Markup == loc.MarkupCall }) {
			return ""
		}

		captions = append(captions, loc.ToPlain(text))
	}

	slices.Sort(captions)

	return strings.Join(captions, " ")
}

// isDrawing reports whether a widget draws something of its own: a picture,
// a portrait, a coat of arms or a text.
func isDrawing(window *Window, widget Widget) bool {
	return len(widget.Texts) > 0 || slices.ContainsFunc(drawingKeys, func(key string) bool {
		return findProperty(window.listProperties(widget), key).Value != ""
	})
}

// listCaptions returns how the panel writes a group's options: the text of
// the widget whose press sets one, else All or None, else the value as
// written, capitalized. A group of fewer than two values is a Yes or No.
func listCaptions(group Group) map[string]string {
	captions := map[string]string{}
	var values []string
	for _, option := range group.Options {
		captions[option] = cmp.Or(group.Captions[option], pickCaptions[option], toSentence(toWrittenValue(option)))
		if option != pickEvery && option != pickNone {
			values = append(values, option)
		}
	}

	if len(values) < 2 && slices.Contains(group.Options, pickNone) {
		yes := pickEvery
		if len(values) == 1 {
			yes = values[0]
		}

		captions[yes], captions[pickNone] = "Yes", "No"
	}

	return captions
}

// toSentence returns a text with its first letter in upper case.
func toSentence(text string) string {
	_, size := utf8.DecodeRuneInString(text)

	return strings.ToUpper(text[:size]) + text[size:]
}

// chooseBox returns the box of the States tab a group starts in: its item's
// rows, else the window's own where a press sets it, else what an object has,
// else the object's own.
func chooseBox(group Group, widgets []Widget) Box {
	object, title := group.Subject+"|"+group.Key, strings.TrimSpace(group.Subject+" "+group.Key)
	switch {
	case group.Item != 0:
		item := widgets[group.Item]
		title = "Rows of " + cmp.Or(item.Name, item.Type)

		return Box{ID: fmt.Sprint("item:", group.Item), Title: title, Kind: "item"}

	case group.Clickable:
		return Box{ID: "window", Title: "This window", Kind: "window"}

	case group.Exists:
		return Box{ID: "has:" + object, Title: title + " has", Kind: "has"}
	}

	return Box{ID: "object:" + object, Title: title, Kind: "object"}
}

// listApplies returns, by group, the widgets whose own condition or choices
// the group's value decides, leaving out those under a hidden widget.
func listApplies(evaluator *evaluator, hidden []int32) map[string][]int32 {
	widgets := evaluator.window.Widgets
	covered := make([]bool, len(widgets))
	for _, widget := range widgets[1:] {
		covered[widget.ID] = covered[widget.Parent] || slices.Contains(hidden, widget.Parent)
	}

	sameChoice := func(first, second Choice) bool { return first.Key == second.Key && first.Value == second.Value }
	applies := map[string][]int32{}
	for _, group := range evaluator.groups {
		picked := evaluator.values[group.ID]
		for _, reader := range group.Readers {
			evaluator.values[group.ID] = pickEvery
			everyFalse, everyChoices := evaluator.isFalse(reader), evaluator.listWidgetChoices(reader)
			evaluator.values[group.ID] = pickNone
			decided := everyFalse != evaluator.isFalse(reader) ||
				!slices.EqualFunc(everyChoices, evaluator.listWidgetChoices(reader), sameChoice)
			if !covered[reader] && decided {
				applies[group.ID] = append(applies[group.ID], reader)
			}
		}

		evaluator.values[group.ID] = picked
	}

	return applies
}

// listGroups returns the groups the window's widgets ask about in their
// conditions and choices, each with the widgets that ask. The window's own
// widget is always drawn, so it asks about none.
func (e *evaluator) listGroups() []Group {
	var groups table[string, Group]
	for id := range e.rules[1:] {
		widget, rule := int32(id+1), &e.rules[id+1]
		facts := rule.shown.listFacts(nil)
		for position := range rule.choices {
			facts = rule.choices[position].listFacts(facts)
		}

		for _, fact := range facts {
			group := addGroup(&groups, fact.group, widget, func() Group { return makeGroup(fact.group, fact.call) })
			if fact.asked != "" {
				group.Options = appendNew(group.Options, fact.asked)
			}
		}
	}

	for position := range groups.values {
		group := &groups.values[position]
		if len(group.Options) != 1 {
			group.Options = append(group.Options, pickEvery)
		}

		group.Options = append(group.Options, pickNone)
	}

	return groups.values
}

// validFunction is the data function that asks whether an object exists.
const validFunction = "IsValid"

// makeGroup returns the group of a call. The call's first steps lead to the
// object it asks about, which gives its Subject and Key, and the rest in
// words is its Label. A call of one step asks about the game.
func makeGroup(id string, call jomini.Call) Group {
	last := len(call.Steps) - 1
	switch {
	case last < 0:
		return Group{ID: id}

	case last == 0:
		parts := []string{lang.DescribeStep(call.Steps[0], false)}
		for _, argument := range call.Steps[0].Arguments {
			if argument.Call != nil {
				parts = append(parts, toCallText(*argument.Call))
			}
		}

		return Group{ID: id, Subject: "Game", Label: strings.Join(parts, " "), call: call}
	}

	end := min(last, directSteps-1)
	var path, keys []string
	for _, step := range call.Steps[end:last] {
		path = append(path, lang.DescribeStep(step, true))
	}

	check := call.Steps[last]
	exists := len(path) > 0 && check.Text == validFunction && len(check.Arguments) == 0
	if !exists {
		path = append(path, lang.DescribeStep(check, false))
	}

	for _, step := range call.Steps[:end] {
		for _, quoted := range listQuoted(step) {
			if quoted != "" {
				keys = append(keys, quoted)
			}
		}
	}

	subject := toSentence(lang.DescribeStep(jomini.Step{Text: call.Steps[end-1].Text}, true))

	return Group{
		ID: id, Subject: subject, Key: strings.Join(keys, " "), Label: strings.Join(path, " › "), Exists: exists,
		call: call,
	}
}
