// states.go works out which widgets of a window the picks hide, and which picks show a hidden widget.

package draw

import (
	"cmp"
	"fmt"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/samber/lo"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/loc"
	"paradox-modding-tools/internal/schema"
)

// A Group is one choice the page lets the user switch: a data call a window's conditions read, or a template list a
// coat's value is drawn from.
type Group struct {
	ID        string            `json:"id"`                  // the group's call; a call that reads the object of a data context is written from the window down to that object, so the same call about two objects is two groups
	Subject   string            `json:"subject,omitempty"`   // the kind of object it asks about, such as Character or Relation; Game for a call of one step, Pictures for a list of pictures, and "" for a coat's list
	Key       string            `json:"key,omitempty"`       // the key that picks its object out of its kind, such as a relation's lover
	Label     string            `json:"label"`               // what it asks, in words
	Exists    bool              `json:"exists,omitempty"`    // it asks whether an object further on than its subject exists
	Options   []string          `json:"options"`             // the values it can take: the values the window asks of it, then every value at once where it has none or several, then none
	Captions  map[string]string `json:"captions,omitempty"`  // by option, how the panel writes it
	Box       Box               `json:"box"`                 // the box of the States tab that it starts in
	Picked    string            `json:"picked,omitempty"`    // the option a coat's list draws now; empty in a window's group, whose value is in States.Values
	Clickable bool              `json:"clickable,omitempty"` // a press of a widget of the window sets it
	Reach     int               `json:"reach,omitempty"`     // how many widgets that draw something it shows or hides
	Item      int32             `json:"item,omitempty"`      // the data model item whose rows alone read it; 0 for the window
	Readers   []int32           `json:"readers,omitempty"`   // the widgets whose own condition reads it
	Pictures  *PictureList      `json:"pictures,omitempty"`  // where the pictures it picks among are listed, for a group of a picture binding; nil otherwise
}

// A Box is one of the boxes the States tab sorts a window's groups into: the window's own, what an object has, an
// object's, or the rows of a list.
type Box struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Icon  string `json:"icon"`
	Look  string `json:"look"` // "rows" with a control each, "chips", or "window" for the window's own groups
}

// States is one way of drawing a window: as it opens, changed by the picks.
type States struct {
	Values  map[string]string  `json:"values"`  // by group, the value each of the window's groups is at
	Hidden  []int32            `json:"hidden"`  // the IDs of the widgets that the values hide
	Applies map[string][]int32 `json:"applies"` // by group, the widgets under no hidden widget whose own condition its value decides
	Choices map[int32][]Choice `json:"choices"` // by widget, the values the groups give its properties
}

// A context is what a widget's bindings read in: by data type, the call that reaches the object of that type from
// the window.
type context struct {
	paths  map[string]jomini.Call
	latest string // the data type of the data context written last
}

// An evaluator works out whether the conditions of a window's widgets hold under the values of the groups, or which
// values would make them hold.
type evaluator struct {
	schema   *schema.Schema
	window   Window
	contexts []context
	groups   []Group
	objects  map[string][]Group // the groups by the call before their last step
	strips   map[int32][]int32  // for each widget in a strip, the widgets of its strip in order
	values   map[string]string  // the value each group is at
	rows     int                // how many rows a data model's list draws, which is its size as the window opens
	parsed   map[string]parsedCondition
}

// A parsedCondition is a condition's text parsed once: its call, and whether it is a call at all.
type parsedCondition struct {
	call   jomini.Call
	isCall bool
}

// An opening is one window as it opens, with the index, file and name it was resolved from.
type opening struct {
	mods       *index.Index
	path       string
	name       string
	evaluator  *evaluator
	library    *library                // what the window's definitions show, with what it has looked up
	colorLists map[int32][]PictureList // by widget, the lists of its bindings of a definition's color
	pictures   []Group
	tiers      []Group
}

// The picks that stand for every value of a group and for none.
const (
	pickEvery = "*every"
	pickNone  = "none"
)

// pickCaptions is how the panel writes the picks that stand for every value of a group and for none.
var pickCaptions = map[string]string{pickEvery: "All", pickNone: "None"}

// directSteps is the most steps a call has when a window or a row reads its object directly: the start of the call, the
// step to the object, and the check on the object.
const directSteps = 3

// EvaluateStates works out which widgets of one window in a GUI file are hidden. Each group is at the value the window
// opens with, unless the given picks, by the ID of a group, pick another. It returns an error if no workspace is open
// or the file does not define the window.
func (drw *Service) EvaluateStates(path, name string, picks map[string]string) (States, error) {
	drw.mu.Lock()
	defer drw.mu.Unlock()
	opened, err := drw.findWindow(path, name)
	if err != nil {
		return States{}, err
	}
	evaluator, library := opened.evaluator.applyPicks(picks), opened.library
	window := &evaluator.window
	hidden := lo.FilterMap(window.Widgets[1:], func(widget Widget, _ int) (int32, bool) {
		return widget.ID, evaluator.isFalse(widget.ID)
	})
	applies, choices := listApplies(evaluator, hidden), evaluator.listChoices(library)
	values := map[string]string{}
	for _, group := range evaluator.groups {
		values[group.ID] = evaluator.values[group.ID]
	}
	for _, group := range slices.Concat(opened.pictures, opened.tiers) {
		applies[group.ID] = group.Readers
		if picked := picks[group.ID]; picked != "" {
			values[group.ID] = picked
		}
	}
	for _, group := range opened.pictures {
		for _, reader := range group.Readers {
			if picked := picks[group.ID]; picked != "" {
				choices[reader] = append(choices[reader], library.pickPicture(*window.Widgets[reader].Pictures, picked))
			}
		}
	}
	pickColors(opened, picks, choices)
	evaluator.pickTiers(library.ws.Game.GUI.Tiers, opened.tiers, picks, choices)
	return States{Values: values, Hidden: hidden, Applies: applies, Choices: choices}, nil
}

// Reveal returns the picks that make a hidden widget of one window in a GUI file visible under the given picks, leaving
// every other pick as it is. It returns nil if the conditions that hide the widget contradict each other or the widget
// stays hidden anyway. It returns an error if no workspace is open, the file does not define the window, or the window
// has no widget with the given ID.
func (drw *Service) Reveal(path, name string, picks map[string]string, widget int32) (map[string]string, error) {
	drw.mu.Lock()
	defer drw.mu.Unlock()
	opened, err := drw.findWindow(path, name)
	if err != nil {
		return nil, err
	}
	window := opened.evaluator.window
	if widget < 0 || int(widget) >= len(window.Widgets) {
		return nil, fmt.Errorf("the window %q has no widget %d", name, widget)
	}
	revealer, required := opened.evaluator.applyPicks(picks), map[string]string{}
	for id := widget; id > 0; id = window.Widgets[id].Parent {
		if !revealer.isFalse(id) {
			continue
		}
		for _, condition := range window.Conditions[id] {
			if !revealer.revealCondition(required, revealer.contexts[id], condition, true) {
				return nil, nil
			}
		}
	}
	merged := map[string]string{}
	maps.Copy(merged, picks)
	maps.Copy(merged, required)
	checker := opened.evaluator.applyPicks(merged)
	for id := widget; id > 0; id = window.Widgets[id].Parent {
		if checker.isFalse(id) {
			return nil, nil
		}
	}
	return required, nil
}

// openEvaluator returns the evaluator of a resolved window at the values it opens with. The given contexts are what its
// widgets read their bindings in, and each data model's list draws the given number of rows.
func openEvaluator(schema *schema.Schema, window Window, contexts []context, count int) *evaluator {
	groups := listGroups(schema, window, contexts, count)
	objects := lo.GroupBy(groups, func(group Group) string {
		call, ok := jomini.ParseCall(group.ID, 0)
		if !ok || len(call.Steps) < 2 {
			return ""
		}
		return toCallText(jomini.Call{Steps: call.Steps[:len(call.Steps)-1]})
	})
	evaluator := &evaluator{
		schema: schema, window: window, contexts: contexts, groups: groups, values: map[string]string{}, rows: count,
		objects: objects, parsed: map[string]parsedCondition{},
	}
	evaluator.strips = evaluator.listStrips()
	evaluator.open()
	classifyGroups(evaluator)
	return evaluator
}

// applyPicks returns a copy of an evaluator with each group at the value the given picks hold for it, else its own.
func (evaluator *evaluator) applyPicks(picks map[string]string) *evaluator {
	picked := *evaluator
	picked.values = maps.Clone(evaluator.values)
	for _, group := range picked.groups {
		if value, ok := picks[group.ID]; ok {
			picked.values[group.ID] = value
		}
	}
	return &picked
}

// open sets the values the window opens with: every group at none, except what holds as a window opens at every value;
// then what the calls of the opening states set; then the first widget of each strip pressed.
func (evaluator *evaluator) open() {
	for _, group := range evaluator.groups {
		evaluator.values[group.ID] = lo.Ternary(holdsOnOpening(group.ID), pickEvery, pickNone)
	}
	required := map[string]string{}
	for _, widget := range evaluator.window.Widgets {
		for _, call := range listCalls(&evaluator.window, widget, game.GUI.OpeningCalls) {
			evaluator.click(required, evaluator.contexts[widget.ID], call)
		}
	}
	maps.Copy(evaluator.values, required)
	for _, widget := range evaluator.window.Widgets {
		if strip := evaluator.strips[widget.ID]; len(strip) > 0 && strip[0] == widget.ID {
			required = map[string]string{}
			evaluator.press(required, widget.ID)
			maps.Copy(evaluator.values, required)
		}
	}
}

// press adds to the given required values what a press of a widget sets. A widget drawn down or checked by a condition
// makes the condition hold and the other widgets of its strip fail.
func (evaluator *evaluator) press(required map[string]string, widget int32) {
	context, window := evaluator.contexts[widget], &evaluator.window
	condition, hasCondition := findPressCondition(window, window.Widgets[widget])
	for _, call := range listCalls(window, window.Widgets[widget], game.GUI.ClickKeys...) {
		if hasCondition {
			evaluator.setVariable(required, context.expand(evaluator.schema, call))
		} else {
			evaluator.click(required, context, call)
		}
	}
	if !hasCondition {
		return
	}
	strip := evaluator.strips[widget]
	holds, known := evaluator.evaluateCondition(context, condition)
	evaluator.revealCondition(required, context, condition, len(strip) > 0 || !known || !holds)
	for _, other := range strip {
		if otherCondition, ok := findPressCondition(window, window.Widgets[other]); ok && other != widget {
			evaluator.revealCondition(required, evaluator.contexts[other], otherCondition, false)
		}
	}
}

// click adds to the given required values what a call a click runs sets, read in the given context: a value of a group
// on the same object, or a GUI variable.
func (evaluator *evaluator) click(required map[string]string, context context, call jomini.Call) {
	call = context.expand(evaluator.schema, call)
	if group, value, ok := evaluator.findSelected(call); ok {
		required[group] = value
		return
	}
	evaluator.setVariable(required, call)
}

// findSelected returns the group on the same object as a call of one quoted argument, and the value the argument
// names: SelectTab('x') selects x of the IsTabSelected group beside it. The last result is false if no group offers
// that value.
func (evaluator *evaluator) findSelected(call jomini.Call) (group, value string, ok bool) {
	last := len(call.Steps) - 1
	if last < 1 {
		return "", "", false
	}
	quoted := listQuoted(call.Steps[last])
	if len(quoted) != 1 {
		return "", "", false
	}
	for _, candidate := range evaluator.objects[toCallText(jomini.Call{Steps: call.Steps[:last]})] {
		if slices.Contains(candidate.Options, quoted[0]) {
			return candidate.ID, quoted[0], true
		}
	}
	return "", "", false
}

// setVariable adds to the given required values what a call to the GUI's variable system sets. Any other call sets
// nothing.
func (evaluator *evaluator) setVariable(required map[string]string, call jomini.Call) {
	variables := game.GUI.Variables
	last := len(call.Steps) - 1
	if last < 1 || !isVariables(evaluator.schema, call.Steps[:last]) {
		return
	}
	quoted := listQuoted(call.Steps[last])
	if len(quoted) == 0 {
		return
	}
	exists, _ := splitFact(evaluator.schema, replaceLastStep(call, variables.Exists, quoted[:1]))
	hasValue, _ := splitFact(evaluator.schema, replaceLastStep(call, variables.HasValue, []string{quoted[0], ""}))
	switch call.Steps[last].Text {
	case variables.Set:
		required[exists] = pickEvery
		required[hasValue] = lo.NthOr(quoted, 1, pickNone)
	case variables.Clear:
		required[exists], required[hasValue] = pickNone, pickNone
	case variables.Toggle:
		required[exists] = lo.Ternary(evaluator.values[exists] == pickEvery, pickNone, pickEvery)
	}
}

// listStrips returns, for each widget in a strip, the widgets of its strip in order. A strip is two or more siblings
// of one type, each a tab.
func (evaluator *evaluator) listStrips() map[int32][]int32 {
	tabs := lo.Filter(evaluator.window.Widgets[1:], func(widget Widget, _ int) bool { return evaluator.isTab(widget) })
	byKind := lo.GroupBy(tabs, func(widget Widget) string { return fmt.Sprint(widget.Parent, " ", widget.Type) })
	strips := map[int32][]int32{}
	for _, siblings := range byKind {
		if len(siblings) < 2 {
			continue
		}
		strip := lo.Map(siblings, func(widget Widget, _ int) int32 { return widget.ID })
		for _, id := range strip {
			strips[id] = strip
		}
	}
	return strips
}

// isTab reports whether a widget is a tab: a condition that reads a group of the window draws it down, or a click on it
// selects a value of one.
func (evaluator *evaluator) isTab(widget Widget) bool {
	context := evaluator.contexts[widget.ID]
	down := findProperty(evaluator.window.listProperties(widget), game.GUI.DownKey).Value
	if call, ok := jomini.ParseBracketed(down, 0); ok {
		return slices.ContainsFunc(listFacts(evaluator.schema, call, context, evaluator.rows), func(fact jomini.Call) bool {
			group, _ := splitFact(evaluator.schema, context.reach(fact))
			return slices.ContainsFunc(evaluator.groups, func(known Group) bool { return known.ID == group })
		})
	}
	return slices.ContainsFunc(listCalls(&evaluator.window, widget, game.GUI.ClickKeys...), func(call jomini.Call) bool {
		_, _, ok := evaluator.findSelected(context.expand(evaluator.schema, call))
		return ok
	})
}

// classifyGroups fills in what each of the evaluator's groups is to its window: its item, its reach, and whether and by
// what text a press sets it. A widget its own condition hides as written counts for no reach.
func classifyGroups(evaluator *evaluator) {
	groups, widgets := evaluator.groups, evaluator.window.Widgets
	items, ends := listItems(widgets), make([]int32, len(widgets))
	hidden := make([]bool, len(widgets))
	for _, widget := range widgets {
		hidden[widget.ID] = slices.ContainsFunc(evaluator.window.Conditions[widget.ID], func(condition string) bool {
			holds, ok := game.GUI.Literals[strings.ToLower(condition)]
			return ok && !holds
		})
	}
	for _, widget := range slices.Backward(widgets) {
		ends[widget.ID] = max(ends[widget.ID], widget.ID+1)
		ends[widget.Parent] = max(ends[widget.Parent], ends[widget.ID])
	}
	captions := listPresses(evaluator, ends)
	for position := range groups {
		group := &groups[position]
		holders := lo.Uniq(lo.Map(group.Readers, func(reader int32, _ int) int32 { return items[reader] }))
		group.Item = lo.Ternary(len(holders) == 1, holders[0], 0)
		drawing := map[int32]bool{}
		for _, reader := range group.Readers {
			for id := reader; id < ends[reader]; id++ {
				if isDrawing(&evaluator.window, widgets[id]) && !hidden[id] {
					drawing[id] = true
				}
			}
		}
		group.Reach = len(drawing)
		group.Captions, group.Clickable = captions[group.ID], captions[group.ID] != nil
	}
}

// listItems returns, for each widget of a window, the item of a data model it is drawn in, or 0 for none.
func listItems(widgets []Widget) []int32 {
	items := make([]int32, len(widgets))
	for _, widget := range widgets {
		items[widget.ID] = lo.Ternary(widget.Item, widget.ID, items[widget.Parent])
	}
	return items
}

// listPresses returns, by group and then by value, the text of the first widget whose press sets that value. The given
// ends are, for each widget, the ID after its last descendant.
func listPresses(evaluator *evaluator, ends []int32) map[string]map[string]string {
	widgets := evaluator.window.Widgets
	captions := map[string]map[string]string{}
	for _, widget := range widgets {
		required := map[string]string{}
		evaluator.press(required, widget.ID)
		if len(required) == 0 {
			continue
		}
		under, _ := lo.Find(widgets[widget.ID:ends[widget.ID]], func(under Widget) bool { return len(under.Texts) > 0 })
		text := toCaption(under)
		for group, value := range required {
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

// toCaption returns the texts of a widget as one caption, or "" where one of them shows a data call, which names
// nothing.
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

// isDrawing reports whether one of a window's widgets draws something of its own: a picture, portrait, coat of arms or
// text.
func isDrawing(window *Window, widget Widget) bool {
	keys := append(slices.Clone(game.GUI.PictureKeys), game.GUI.PortraitKey, game.GUI.CoatKey)
	return len(widget.Texts) > 0 || slices.ContainsFunc(keys, func(key string) bool {
		return findProperty(window.listProperties(widget), key).Value != ""
	})
}

// listCaptions returns how the panel writes each of a group's options: the text of the widget whose press sets it, else
// All or None, else the value as written with a capital letter. A group of fewer than two values is a Yes or No.
func listCaptions(group Group) map[string]string {
	captions := map[string]string{}
	for _, option := range group.Options {
		captions[option] = cmp.Or(group.Captions[option], pickCaptions[option], toSentence(toWrittenValue(option)))
	}
	values := lo.Without(group.Options, pickEvery, pickNone)
	if len(values) < 2 && slices.Contains(group.Options, pickNone) {
		captions[lo.FirstOr(values, pickEvery)], captions[pickNone] = "Yes", "No"
	}
	return captions
}

// toSentence returns a text as the start of a sentence: its first letter in upper case.
func toSentence(text string) string {
	_, size := utf8.DecodeRuneInString(text)
	return strings.ToUpper(text[:size]) + text[size:]
}

// chooseBox returns the box of the States tab that a group starts in, given the window's widgets. Its order of choice
// is the rows of its item, the window's own box where a press sets it, what an object has, and its object's box.
func chooseBox(group Group, widgets []Widget) Box {
	object, title := group.Subject+"|"+group.Key, strings.Join(lo.Compact([]string{group.Subject, group.Key}), " ")
	switch {
	case group.Item != 0:
		item := widgets[group.Item]
		title = "Rows of " + cmp.Or(item.Name, item.Type)
		return Box{ID: fmt.Sprint("item:", group.Item), Title: title, Icon: "i-lucide-list", Look: "rows"}
	case group.Clickable:
		return Box{ID: "window", Title: "This window", Icon: "i-lucide-app-window", Look: "window"}
	case group.Exists:
		return Box{ID: "has:" + object, Title: title + " has", Icon: "i-lucide-check-check", Look: "chips"}
	}
	return Box{ID: "object:" + object, Title: title, Icon: "i-lucide-circle-dot", Look: "rows"}
}

// listApplies returns, by group, the widgets whose own condition or choices the group's value decides, leaving out
// those under the given hidden widgets.
func listApplies(evaluator *evaluator, hidden []int32) map[string][]int32 {
	widgets := evaluator.window.Widgets
	hides := lo.Keyify(hidden)
	covered := make([]bool, len(widgets))
	for _, widget := range widgets[1:] {
		_, held := hides[widget.Parent]
		covered[widget.ID] = covered[widget.Parent] || held
	}
	applies := map[string][]int32{}
	for _, group := range evaluator.groups {
		picked := evaluator.values[group.ID]
		for _, reader := range group.Readers {
			evaluator.values[group.ID] = pickEvery
			everyFalse, everyChoices := evaluator.isFalse(reader), evaluator.listWidgetChoices(reader)
			evaluator.values[group.ID] = pickNone
			sameChoice := func(first, second Choice) bool { return first.Key == second.Key && first.Value == second.Value }
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

// isFalse reports whether the own condition of a widget is known not to hold under the values, which hides it.
func (evaluator *evaluator) isFalse(widget int32) bool {
	return lo.SomeBy(evaluator.window.Conditions[widget], func(condition string) bool {
		holds, known := evaluator.evaluateCondition(evaluator.contexts[widget], condition)
		return known && !holds
	})
}

// expand returns a call as the context reads it: its macros expanded, then written from the window down.
func (context context) expand(schema *schema.Schema, call jomini.Call) jomini.Call {
	return context.reach(schema.ResolveMacro(call, context.latest))
}

// reach returns a call written from the window down: a first step that names the type of one of the context's objects
// becomes the call that reaches that object.
func (context context) reach(call jomini.Call) jomini.Call {
	if len(call.Steps) == 0 {
		return call
	}
	path, ok := context.paths[call.Steps[0].Text]
	if !ok {
		return call
	}
	return jomini.Call{Steps: slices.Concat(path.Steps, call.Steps[1:])}
}

// listContexts returns what each widget of a window reads its bindings in: what its holder reads, then each data
// context it writes.
func listContexts(schema *schema.Schema, window Window) []context {
	contexts := make([]context, len(window.Widgets))
	for _, widget := range window.Widgets {
		current := context{paths: map[string]jomini.Call{}}
		if widget.ID > 0 {
			holder := contexts[widget.Parent]
			current = context{paths: maps.Clone(holder.paths), latest: holder.latest}
		}
		for _, call := range listCalls(&window, widget, game.GUI.ContextKey) {
			path := current.expand(schema, call)
			dataType := cmp.Or(schema.ResolveDatacontext(path, ""), schema.ResolveDatacontext(call, current.latest))
			if dataType != "" {
				current.paths[dataType], current.latest = path, dataType
			}
		}
		contexts[widget.ID] = current
	}
	return contexts
}

// listCalls returns the data calls that one of a window's widgets writes under the given keys, in the order written.
func listCalls(window *Window, widget Widget, keys ...string) []jomini.Call {
	var calls []jomini.Call
	for property := range window.listProperties(widget) {
		call, ok := jomini.ParseBracketed(property.Value, 0)
		if ok && slices.ContainsFunc(keys, func(key string) bool { return strings.EqualFold(key, property.Key) }) {
			calls = append(calls, call)
		}
	}
	return calls
}

// findPressCondition returns the condition that draws a widget down, else the one that draws it checked. The last
// result is false if it writes neither as a data call.
func findPressCondition(window *Window, widget Widget) (string, bool) {
	for _, key := range []string{game.GUI.DownKey, game.GUI.CheckedKey} {
		value := findProperty(window.listProperties(widget), key).Value
		if strings.HasPrefix(strings.TrimSpace(value), "[") {
			return value, true
		}
	}
	return "", false
}

// listGroups returns the groups the widgets of a window read, each with its readers. A widget reads a group in its
// conditions and in the conditions of the choices its properties write. The window's own widget is always drawn, so its
// conditions are not read. Each data model's list draws the given number of rows.
func listGroups(schema *schema.Schema, window Window, contexts []context, count int) []Group {
	var groups table[string, Group]
	for _, widget := range window.Widgets[1:] {
		calls := lo.FilterMap(window.Conditions[widget.ID], func(condition string, _ int) (jomini.Call, bool) {
			return jomini.ParseBracketed(condition, 0)
		})
		for property := range window.listProperties(widget) {
			if call, ok := jomini.ParseBracketed(property.Value, 0); ok {
				calls = append(calls, listSelectConditions(call)...)
			}
		}
		for _, call := range calls {
			for _, fact := range listFacts(schema, call, contexts[widget.ID], count) {
				id, value := splitFact(schema, contexts[widget.ID].reach(fact))
				position := groups.add(id, func() Group { return makeGroup(id) })
				group := &groups.values[position]
				if value != "" && !slices.Contains(group.Options, value) {
					group.Options = append(group.Options, value)
				}
				if len(group.Readers) == 0 || group.Readers[len(group.Readers)-1] != widget.ID {
					group.Readers = append(group.Readers, widget.ID)
				}
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

// makeGroup returns the group of a call. The call's first steps reach the object the group asks about, giving its
// Subject and Key, and the Label is the rest of the call in words. A call of one step asks about the game.
func makeGroup(id string) Group {
	call, _ := jomini.ParseCall(id, 0)
	last := len(call.Steps) - 1
	switch {
	case last < 0:
		return Group{ID: id}
	case last == 0:
		given := lo.FilterMap(call.Steps[0].Arguments, func(argument jomini.Argument, _ int) (string, bool) {
			return toCallText(lo.FromPtr(argument.Call)), argument.Call != nil
		})
		parts := append([]string{schema.DescribeStep(call.Steps[0], false)}, given...)
		return Group{ID: id, Subject: "Game", Label: strings.Join(parts, " ")}
	}
	end := min(last, directSteps-1)
	path := lo.Map(call.Steps[end:last], func(step jomini.Step, _ int) string { return schema.DescribeStep(step, true) })
	check := call.Steps[last]
	exists := len(path) > 0 && check.Text == game.GUI.ValidFunction && len(check.Arguments) == 0
	if !exists {
		path = append(path, schema.DescribeStep(check, false))
	}
	keys := lo.Compact(lo.FlatMap(call.Steps[:end], func(step jomini.Step, _ int) []string { return listQuoted(step) }))
	subject := toSentence(schema.DescribeStep(jomini.Step{Text: call.Steps[end-1].Text}, true))
	return Group{ID: id, Subject: subject, Key: strings.Join(keys, " "), Label: strings.Join(path, " › "), Exists: exists}
}

// listFacts returns the calls a condition reads, looking through boolean functions to their operands. A fact the data
// models decide is no group. Each data model's list draws the given number of rows.
func listFacts(schema *schema.Schema, call jomini.Call, context context, count int) []jomini.Call {
	call = expandVariableQuery(schema.ResolveMacro(call, context.latest))
	if len(call.Steps) == 0 {
		return nil
	}
	if _, _, operands, ok := splitBoolean(call); ok {
		return lo.FlatMap(operands, func(operand jomini.Call, _ int) []jomini.Call {
			return listFacts(schema, operand, context, count)
		})
	}
	if _, computed := computeFact(schema, context, call, count); computed {
		return nil
	}
	return []jomini.Call{call}
}

// computeFact decides a fact about the window's data models as the window opens, each model drawing the given number of
// rows: whether a model is empty or has items, or a comparison of two numbers. The last result is false for any other
// fact, and for a comparison the numbers do not decide.
func computeFact(schema *schema.Schema, context context, call jomini.Call, count int) (holds, computed bool) {
	if len(call.Steps) != 1 {
		return false, false
	}
	step := call.Steps[0]
	switch step.Text {
	case game.GUI.ModelEmpty:
		return count == 0, true
	case game.GUI.ModelHas:
		return count > 0, true
	}
	name, _, _ := strings.Cut(step.Text, "_")
	results, ok := game.GUI.Comparisons[name]
	if !ok || len(step.Arguments) != 2 {
		return false, false
	}
	outcomes := map[bool]bool{}
	for _, first := range toNumbers(schema, context, step.Arguments[0], count) {
		for _, second := range toNumbers(schema, context, step.Arguments[1], count) {
			outcomes[slices.Contains(results, cmp.Compare(first, second))] = true
		}
	}
	return outcomes[true], len(outcomes) == 1
}

// toNumbers returns the numbers an argument of a comparison may be. A data model's size is the given number of rows. A
// number a data object holds is shown as a sample of at least one (sampleNumber), so it may be one or any number above.
// It returns none for any other argument.
func toNumbers(schema *schema.Schema, context context, argument jomini.Argument, count int) []float64 {
	if argument.Call != nil {
		steps := argument.Call.Steps
		path := context.expand(schema, *argument.Call)
		function, _ := schema.FindFunction(path, "", len(path.Steps)-1)
		switch {
		case len(steps) == 1 && steps[0].Text == game.GUI.ModelSize:
			return []float64{float64(count)}
		case len(steps) > 1 && game.GUI.CallOutputs[function.Returns] == kindNumber:
			return []float64{minSample, math.Inf(1)}
		}
		return nil
	}
	text := argument.Text
	if typed, after, cut := strings.Cut(text, ")"); cut && strings.HasPrefix(typed, "(") {
		text = after
	}
	number, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
	return lo.Ternary(err == nil, []float64{number}, nil)
}

// expandVariableQuery rewrites a call that asks whether a GUI variable is unset or set to a value, such as
// NotExistOrHasValue('x', 'v'), as Or(Not(Exists('x')), HasValue('x', 'v')) on the same variable.
func expandVariableQuery(call jomini.Call) jomini.Call {
	variables, last := game.GUI.Variables, len(call.Steps)-1
	if last < 1 || call.Steps[last].Text != variables.UnsetOrValue || len(listQuoted(call.Steps[last])) != 2 {
		return call
	}
	quoted := listQuoted(call.Steps[last])
	exists, hasValue := replaceLastStep(call, variables.Exists, quoted[:1]), replaceLastStep(call, variables.HasValue, quoted)
	or, _ := lo.FindKey(game.GUI.Booleans, "or")
	not, _ := lo.FindKey(game.GUI.Booleans, "not")
	unset := jomini.Call{Steps: []jomini.Step{{Text: not, Arguments: []jomini.Argument{{Call: &exists}}}}}
	return jomini.Call{Steps: []jomini.Step{{Text: or, Arguments: []jomini.Argument{{Call: &unset}, {Call: &hasValue}}}}}
}

// splitBoolean returns what a boolean function combines: whether it needs all its operands or just one, whether it
// flips the result, and its operands. The last result is false if the call is no boolean function.
func splitBoolean(call jomini.Call) (conjunction, negated bool, operands []jomini.Call, ok bool) {
	if len(call.Steps) != 1 {
		return false, false, nil, false
	}
	step := call.Steps[0]
	switch game.GUI.Booleans[strings.TrimRight(step.Text, "0123456789")] {
	case "and":
		conjunction = true
	case "not":
		conjunction, negated = true, true
	case "nor":
		negated = true
	case "or": // needs one operand and does not flip the result, which the zero values say
	default:
		return false, false, nil, false
	}
	operands = lo.FilterMap(step.Arguments, func(argument jomini.Argument, _ int) (jomini.Call, bool) {
		return lo.FromPtr(argument.Call), argument.Call != nil
	})
	return conjunction, negated, operands, true
}

// splitFact splits a call into the group it reads and the value it asks of the group, the last quoted argument of the
// call's last step. A call with no such argument is its own group with no value.
func splitFact(schema *schema.Schema, call jomini.Call) (group, value string) {
	last := len(call.Steps) - 1
	if last < 0 {
		return "", ""
	}
	leading := lo.Ternary(last > 0 && isVariables(schema, call.Steps[:last]), 1, 0)
	blank := -1
	for position, argument := range call.Steps[last].Arguments {
		if argument.Quoted && position >= leading {
			blank, value = position, argument.Text
		}
	}
	if blank < 0 {
		return toCallText(call), ""
	}
	steps := slices.Clone(call.Steps)
	steps[last].Arguments = slices.Clone(steps[last].Arguments)
	steps[last].Arguments[blank].Text = ""
	return toCallText(jomini.Call{Steps: steps}), value
}

// isVariables reports whether the given steps reach the GUI's variable system.
func isVariables(schema *schema.Schema, steps []jomini.Step) bool {
	types := schema.ResolveCallTypes(jomini.Call{Steps: steps}, "")
	return len(types) == len(steps) && types[len(types)-1] == game.GUI.Variables.Type
}

// holdsOnOpening reports whether a group asks what holds as the window opens: an object exists or a list has items
// (game.GUI.OpeningFacts), or a fact of the sample the window is drawn for (game.GUI.SampleFacts). It is false if the
// call reaches an object the sample lacks (game.GUI.SampleAbsent).
func holdsOnOpening(id string) bool {
	call, ok := jomini.ParseCall(id, 0)
	if !ok || len(call.Steps) == 0 {
		return false
	}
	last := call.Steps[len(call.Steps)-1]
	isAbsent := slices.ContainsFunc(call.Steps, func(step jomini.Step) bool {
		return slices.Contains(game.GUI.SampleAbsent, step.Text)
	})
	isFact := slices.Contains(game.GUI.OpeningFacts, last.Text) || slices.Contains(game.GUI.SampleFacts, last.Text)
	return isFact && !isAbsent && len(last.Arguments) == 0
}

// listQuoted returns the quoted arguments of a step, in order.
func listQuoted(step jomini.Step) []string {
	return lo.FilterMap(step.Arguments, func(argument jomini.Argument, _ int) (string, bool) {
		return argument.Text, argument.Quoted
	})
}

// replaceLastStep returns a call with its last step replaced by a function of the given quoted arguments.
func replaceLastStep(call jomini.Call, function string, quoted []string) jomini.Call {
	arguments := lo.Map(quoted, func(text string, _ int) jomini.Argument { return jomini.Argument{Text: text, Quoted: true} })
	steps := slices.Clone(call.Steps[:len(call.Steps)-1])
	return jomini.Call{Steps: append(steps, jomini.Step{Text: function, Arguments: arguments})}
}

// toCallText returns the text of a call.
func toCallText(call jomini.Call) string {
	steps := lo.Map(call.Steps, func(step jomini.Step, _ int) string {
		if len(step.Arguments) == 0 {
			return step.Text
		}
		arguments := lo.Map(step.Arguments, func(argument jomini.Argument, _ int) string {
			if argument.Call != nil {
				return toCallText(*argument.Call)
			}
			if !argument.Quoted {
				return argument.Text
			}
			return "'" + argument.Text + "'"
		})
		return step.Text + "(" + strings.Join(arguments, ", ") + ")"
	})
	return strings.Join(steps, ".")
}

// evaluateCondition works out whether a condition, read in the given context, holds under the values of the groups. The
// second result is false for a condition that is no data call.
func (evaluator *evaluator) evaluateCondition(context context, condition string) (value, known bool) {
	if literal, ok := game.GUI.Literals[strings.ToLower(condition)]; ok {
		return literal, true
	}
	call, ok := evaluator.parse(condition)
	if !ok {
		return false, false
	}
	return evaluator.evaluateCall(context, call)
}

// parse returns a condition's call, parsed once. The second result is false for a condition that is no data call.
func (evaluator *evaluator) parse(condition string) (jomini.Call, bool) {
	if parsed, ok := evaluator.parsed[condition]; ok {
		return parsed.call, parsed.isCall
	}
	call, ok := jomini.ParseBracketed(condition, 0)
	evaluator.parsed[condition] = parsedCondition{call: call, isCall: ok}
	return call, ok
}

// evaluateCall works out whether a call, read in the given context, holds under the values of the groups. The second
// result is false when the known operands do not decide it, or for a fact of a group the window does not have.
func (evaluator *evaluator) evaluateCall(context context, call jomini.Call) (value, known bool) {
	call = expandVariableQuery(evaluator.schema.ResolveMacro(call, context.latest))
	if len(call.Steps) == 0 {
		return false, false
	}
	conjunction, negated, operands, isBoolean := splitBoolean(call)
	if !isBoolean {
		if holds, computed := computeFact(evaluator.schema, context, call, evaluator.rows); computed {
			return holds, true
		}
		group, asked := splitFact(evaluator.schema, context.reach(call))
		picked, has := evaluator.values[group]
		return picked == pickEvery || (picked != pickNone && picked == asked), has
	}
	var anyTrue, anyFalse, anyUnknown bool
	for _, operand := range operands {
		operandValue, operandKnown := evaluator.evaluateCall(context, operand)
		switch {
		case !operandKnown:
			anyUnknown = true
		case operandValue:
			anyTrue = true
		default:
			anyFalse = true
		}
	}
	switch {
	case conjunction && anyFalse, !conjunction && anyTrue: // a false operand decides And, a true one decides Or
		value = !conjunction
	case anyUnknown:
		return false, false
	default:
		value = conjunction
	}
	return value != negated, true
}

// revealCondition adds to the given required values the ones that make a condition, read in the given context, hold or
// fail as wanted. It returns false if a group is already required to be at another value, so the conditions
// contradict. A condition that is no data call asks for nothing.
func (evaluator *evaluator) revealCondition(required map[string]string, context context, condition string, holds bool) bool {
	if literal, ok := game.GUI.Literals[strings.ToLower(condition)]; ok {
		return literal == holds
	}
	call, ok := evaluator.parse(condition)
	return !ok || evaluator.revealCall(required, context, call, holds)
}

// revealCall adds to the given required values those that make a call, read in the given context, hold or fail as
// wanted. It returns false if no values give that result.
func (evaluator *evaluator) revealCall(required map[string]string, context context, call jomini.Call, holds bool) bool {
	call = expandVariableQuery(evaluator.schema.ResolveMacro(call, context.latest))
	if len(call.Steps) == 0 {
		return true
	}
	conjunction, negated, operands, isBoolean := splitBoolean(call)
	if !isBoolean {
		if computedHolds, computed := computeFact(evaluator.schema, context, call, evaluator.rows); computed {
			return computedHolds == holds
		}
		group, asked := splitFact(evaluator.schema, context.reach(call))
		pick := lo.Ternary(holds, cmp.Or(asked, pickEvery), pickNone)
		if existing, has := required[group]; has && existing != pick {
			return false
		}
		required[group] = pick
		return true
	}
	operandHolds := holds != negated
	every := conjunction == operandHolds
	for position, operand := range operands {
		if (position == 0 || every) && !evaluator.revealCall(required, context, operand, operandHolds) {
			return false
		}
	}
	return true
}
