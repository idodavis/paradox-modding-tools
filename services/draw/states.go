package draw

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"
	"unicode"

	"github.com/samber/lo"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/schema"
	"paradox-modding-tools/services/workspace"
)

// A Group is one choice the page lets the user switch. It is either a data call that a window's conditions read, or
// a template list that a coat's value is drawn from.
type Group struct {
	// ID is the group's call. A call that reads the object of a data context is written from the window down to that
	// object, so the same call about two objects is two groups.
	ID string `json:"id"`
	// Subject is the kind of object it asks about, in words, such as Character or Relation; Game for a call of one
	// step, and "" for a coat's list.
	Subject string `json:"subject,omitempty"`
	Key     string `json:"key,omitempty"` // the key that picks its object out of its kind, such as a relation's lover
	Label   string `json:"label"`         // what it asks, in words
	// Exists is true when it asks whether an object further on than its subject exists, which its label names.
	Exists bool `json:"exists,omitempty"`
	// Options is the values it can take: the values the window asks of it, then every value at once where it has
	// none or several, then none.
	Options   []string          `json:"options"`
	Captions  map[string]string `json:"captions,omitempty"`  // by value: the text of the widget whose press sets it
	Picked    string            `json:"picked"`              // the value it has now
	Clickable bool              `json:"clickable,omitempty"` // a press of a widget of the window sets it
	Reach     int               `json:"reach,omitempty"`     // how many widgets that draw something it shows or hides
	Item      int32             `json:"item,omitempty"`      // the data model item whose rows alone read it; 0 where the window does
	Readers   []int32           `json:"readers,omitempty"`   // the widgets whose own condition reads it
	// Pictures is where the pictures it picks among are listed, for a group of a picture binding; nil otherwise.
	Pictures *PictureList `json:"pictures,omitempty"`
}

// States is one way of drawing a window: as it opens, changed by the picks.
type States struct {
	Groups []Group `json:"groups"` // what the panel offers, each with its value
	Hidden []int32 `json:"hidden"` // the IDs of the widgets that the values hide
	// Applies is, by group, the widgets whose own condition its value decides, under no hidden widget. A group that
	// decides none is left out.
	Applies map[string][]int32 `json:"applies"`
	// Conditions is, by widget, the conditions it is shown on, as written. A widget shown on none is left out.
	Conditions map[int32][]string `json:"conditions"`
	// Choices is, by widget, the values that the values of the groups give its properties. A widget given none is
	// left out.
	Choices map[int32][]Choice `json:"choices"`
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
	context  context            // what the condition being worked out reads in
	values   map[string]string  // the value each group is at
	required map[string]string  // for a reveal or a press: the value each group has to be at
}

// The picks that stand for every value of a group and for none.
const (
	pickEvery = "*every"
	pickNone  = "none"
)

// directSteps is how many steps a check of an object that a window or a row reads directly has at most: where its
// call starts, the step to the object, and the check.
const directSteps = 3

// EvaluateStates works out which widgets of one window in a GUI file are hidden. Each group is at the value the window
// opens with, unless the given picks pick another: a pick is the value chosen for the ID of a group. It returns an
// error if no workspace is open, or if the file is not in the workspace or does not define the window.
func (drw *Service) EvaluateStates(path, name string, picks map[string]string) (States, error) {
	ws, window, err := drw.findWindow(path, name)
	if err != nil {
		return States{}, err
	}
	return evaluate(ws, window, picks), nil
}

// Reveal returns the picks that make a hidden widget of one window in a GUI file visible under the given picks. Each
// condition on the way down to the widget that hides it is set to values that make it hold, and every other pick
// stays as it is. It returns nil if those conditions contradict each other, or if the widget stays hidden anyway. It
// returns an error if no workspace is open, the file does not define the window, or the window has no widget with
// the given ID.
func (drw *Service) Reveal(path, name string, picks map[string]string, widget int32) (map[string]string, error) {
	ws, window, err := drw.findWidget(path, name, widget)
	if err != nil {
		return nil, err
	}
	revealer, _ := openWindow(ws, window, picks)
	revealer.required = map[string]string{}
	for id := widget; id > 0; id = window.Widgets[id].Parent {
		if !revealer.isFalse(id) {
			continue
		}
		revealer.context = revealer.contexts[id]
		for _, condition := range listConditions(window.Widgets[id]) {
			if !revealer.revealCondition(condition, true) {
				return nil, nil
			}
		}
	}
	merged := maps.Clone(picks)
	if merged == nil {
		merged = map[string]string{}
	}
	maps.Copy(merged, revealer.required)
	checker, _ := openWindow(ws, window, merged)
	for id := widget; id > 0; id = window.Widgets[id].Parent {
		if checker.isFalse(id) {
			return nil, nil
		}
	}
	return revealer.required, nil
}

// findWidget returns the open workspace and one window of a GUI file, resolved. It returns an error if no workspace is
// open, the file does not define the window, or the window has no widget with the given ID.
func (drw *Service) findWidget(path, name string, widget int32) (*workspace.Session, Window, error) {
	ws, window, err := drw.findWindow(path, name)
	if err == nil && (widget < 0 || int(widget) >= len(window.Widgets)) {
		err = fmt.Errorf("the window %q has no widget %d", name, widget)
	}
	return ws, window, err
}

// findWindow returns the open workspace and one window of a GUI file, resolved. It returns an error if no workspace is
// open, or if the file is not in the workspace or does not define the window.
func (drw *Service) findWindow(path, name string) (*workspace.Session, Window, error) {
	ws, err := drw.Workspace.Session()
	if err != nil {
		return nil, Window{}, err
	}
	window, err := drw.ResolveWindow(path, name)
	return ws, window, err
}

// evaluate works out the states of an already resolved window under the given picks.
func evaluate(ws *workspace.Session, window Window, picks map[string]string) States {
	evaluator, groups := openWindow(ws, window, picks)
	hidden := lo.FilterMap(window.Widgets[1:], func(widget Widget, _ int) (int32, bool) {
		return widget.ID, evaluator.isFalse(widget.ID)
	})
	classifyGroups(evaluator, groups)
	conditions := map[int32][]string{}
	for _, widget := range window.Widgets[1:] {
		if written := listConditions(widget); len(written) > 0 {
			conditions[widget.ID] = written
		}
	}
	library := &library{ws: ws, mods: ws.Mods.Load()}
	applies, choices, pictures := listApplies(evaluator, groups, hidden), evaluator.listChoices(library), listPictureGroups(window, picks)
	for _, group := range pictures {
		applies[group.ID] = group.Readers
		for _, reader := range group.Readers {
			if group.Picked != "" {
				choices[reader] = append(choices[reader], library.pickPicture(*window.Widgets[reader].Pictures, group.Picked))
			}
		}
	}
	return States{
		Groups: append(groups, pictures...), Hidden: hidden, Applies: applies, Conditions: conditions, Choices: choices,
	}
}

// openWindow returns the evaluator of a resolved window and its groups, each at its value: the one picked for it in
// the given picks, else the one the window opens with.
func openWindow(ws *workspace.Session, window Window, picks map[string]string) (*evaluator, []Group) {
	contexts := listContexts(ws.Schema, window)
	groups := listGroups(ws.Schema, window, contexts)
	evaluator := &evaluator{
		schema: ws.Schema, window: window, contexts: contexts, groups: groups, values: map[string]string{},
		objects: lo.GroupBy(groups, func(group Group) string { return toObject(group.ID) }),
	}
	evaluator.strips = evaluator.listStrips()
	evaluator.open()
	for position := range groups {
		group := &groups[position]
		if picked, ok := picks[group.ID]; ok {
			evaluator.values[group.ID] = picked
		}
		group.Picked = evaluator.values[group.ID]
	}
	return evaluator, groups
}

// open sets the values the window opens with: every group at none, and a check that an object the window reads
// directly exists at every value; then what the calls of the window's opening states set; then the first widget of
// each strip pressed.
func (evaluator *evaluator) open() {
	for _, group := range evaluator.groups {
		evaluator.values[group.ID] = lo.Ternary(isDirect(group.ID), pickEvery, pickNone)
	}
	evaluator.required = map[string]string{}
	for _, widget := range evaluator.window.Widgets {
		evaluator.context = evaluator.contexts[widget.ID]
		for _, call := range listCalls(widget, game.GUI.OpeningCalls) {
			evaluator.click(call)
		}
	}
	maps.Copy(evaluator.values, evaluator.required)
	for _, widget := range evaluator.window.Widgets {
		if strip := evaluator.strips[widget.ID]; len(strip) > 0 && strip[0] == widget.ID {
			evaluator.required = map[string]string{}
			evaluator.press(widget.ID)
			maps.Copy(evaluator.values, evaluator.required)
		}
	}
}

// press adds to the required values what a press of a widget sets. A widget drawn down or checked by a condition
// runs only the GUI variable calls of its clicks, then has its condition held, or flipped where it is in no strip;
// the other widgets of its strip have theirs failed. Any other widget runs all its clicks.
func (evaluator *evaluator) press(widget int32) {
	evaluator.context = evaluator.contexts[widget]
	condition, hasCondition := findPressCondition(evaluator.window.Widgets[widget])
	for _, call := range listCalls(evaluator.window.Widgets[widget], game.GUI.ClickKeys...) {
		if hasCondition {
			evaluator.setVariable(evaluator.reach(call))
		} else {
			evaluator.click(call)
		}
	}
	if !hasCondition {
		return
	}
	strip := evaluator.strips[widget]
	evaluator.revealCondition(condition, len(strip) > 0 || !evaluator.isTrue(condition))
	for _, other := range strip {
		if otherCondition, ok := findPressCondition(evaluator.window.Widgets[other]); ok && other != widget {
			evaluator.context = evaluator.contexts[other]
			evaluator.revealCondition(otherCondition, false)
		}
	}
}

// click adds to the required values what a call that a click runs sets: the value of the group on the same object
// that the call names, or a GUI variable set, cleared or toggled.
func (evaluator *evaluator) click(call jomini.Call) {
	call = evaluator.reach(call)
	if group, value, ok := evaluator.findSelected(call); ok {
		evaluator.required[group] = value
		return
	}
	evaluator.setVariable(call)
}

// findSelected returns the group on the same object as a call of one quoted argument, and the value of it that the
// argument names: SelectTab('x') selects x of the IsTabSelected group beside it. The last result is false if no group
// of the window offers that value.
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

// setVariable adds to the required values what a call to the GUI's variable system sets: a variable set, with its
// value where one is given, cleared, or toggled between set and unset. Any other call sets nothing.
func (evaluator *evaluator) setVariable(call jomini.Call) {
	variables := game.GUI.Variables
	last := len(call.Steps) - 1
	if last < 1 || !isVariables(evaluator.schema, call.Steps[:last]) {
		return
	}
	quoted := listQuoted(call.Steps[last])
	if len(quoted) == 0 {
		return
	}
	exists, _, _, _ := splitFact(evaluator.schema, replaceLastStep(call, variables.Exists, quoted[:1]))
	hasValue, _, _, _ := splitFact(evaluator.schema, replaceLastStep(call, variables.HasValue, []string{quoted[0], ""}))
	switch call.Steps[last].Text {
	case variables.Set:
		evaluator.required[exists] = pickEvery
		evaluator.required[hasValue] = lo.NthOr(quoted, 1, pickNone)
	case variables.Clear:
		evaluator.required[exists], evaluator.required[hasValue] = pickNone, pickNone
	case variables.Toggle:
		evaluator.required[exists] = lo.Ternary(evaluator.values[exists] == pickEvery, pickNone, pickEvery)
	}
}

// listStrips returns, for each widget in a strip, the widgets of its strip in order. A strip is two or more siblings
// of one type, each a tab.
func (evaluator *evaluator) listStrips() map[int32][]int32 {
	tabs := lo.Filter(evaluator.window.Widgets[1:], func(widget Widget, _ int) bool { return evaluator.isTab(widget) })
	strips := map[int32][]int32{}
	for _, siblings := range lo.GroupBy(tabs, func(widget Widget) string { return fmt.Sprint(widget.Parent, " ", widget.Type) }) {
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

// isTab reports whether a widget is a tab, whose page shows by the same state as the tab: it is drawn down by a
// condition that reads a group of the window, or selects a value of one with a click. The window's groups are the
// ones its widgets' visibility reads.
func (evaluator *evaluator) isTab(widget Widget) bool {
	evaluator.context = evaluator.contexts[widget.ID]
	if call, ok := jomini.ParseBracketed(findProperty(widget.Properties, game.GUI.DownKey).Value, 0); ok {
		return slices.ContainsFunc(listFacts(evaluator.schema, call, evaluator.context.latest), func(fact jomini.Call) bool {
			group, _, _, _ := splitFact(evaluator.schema, evaluator.context.reach(fact))
			return slices.ContainsFunc(evaluator.groups, func(known Group) bool { return known.ID == group })
		})
	}
	return slices.ContainsFunc(listCalls(widget, game.GUI.ClickKeys...), func(call jomini.Call) bool {
		_, _, ok := evaluator.findSelected(evaluator.reach(call))
		return ok
	})
}

// classifyGroups fills in what each group is to the evaluator's window: the data model item whose rows alone read
// it, how many widgets that draw something it shows or hides, whether a press sets it and the text of the widget
// whose press sets each value. A widget written hidden counts for no reach.
func classifyGroups(evaluator *evaluator, groups []Group) {
	widgets := evaluator.window.Widgets
	items, ends := make([]int32, len(widgets)), make([]int32, len(widgets))
	written := make([]bool, len(widgets))
	for _, widget := range widgets {
		items[widget.ID] = lo.Ternary(widget.Item, widget.ID, items[widget.Parent])
		written[widget.ID] = slices.ContainsFunc(listConditions(widget), func(condition string) bool {
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
				if isDrawing(widgets[id]) && !written[id] {
					drawing[id] = true
				}
			}
		}
		group.Reach = len(drawing)
		group.Captions, group.Clickable = captions[group.ID], captions[group.ID] != nil
	}
}

// listPresses returns, by group, the text of the widget whose press sets each value, the first written. A widget's
// text is the first that it or a widget under it draws, and a text that shows a data binding names nothing; the given
// ends are the ID past each widget's last descendant.
func listPresses(evaluator *evaluator, ends []int32) map[string]map[string]string {
	widgets := evaluator.window.Widgets
	captions := map[string]map[string]string{}
	for _, widget := range widgets {
		evaluator.required = map[string]string{}
		evaluator.press(widget.ID)
		if len(evaluator.required) == 0 {
			continue
		}
		text := lo.FirstOr(lo.FilterMap(widgets[widget.ID:ends[widget.ID]], func(under Widget, _ int) (string, bool) {
			return strings.Join(slices.Sorted(maps.Values(under.Texts)), " "), len(under.Texts) > 0
		}), "")
		for group, value := range evaluator.required {
			if captions[group] == nil {
				captions[group] = map[string]string{}
			}
			isText := text != "" && !strings.Contains(text, callText)
			if _, named := captions[group][value]; !named && isText && value != pickEvery && value != pickNone {
				captions[group][value] = text
			}
		}
	}
	return captions
}

// isDrawing reports whether a widget draws something of its own: a picture, found or bound, a portrait, a coat of arms
// or a text.
func isDrawing(widget Widget) bool {
	keys := append(slices.Clone(game.GUI.PictureKeys), game.GUI.PortraitKey, game.GUI.CoatKey)
	return len(widget.Texts) > 0 || slices.ContainsFunc(keys, func(key string) bool {
		return findProperty(widget.Properties, key).Value != ""
	})
}

// listApplies returns, by group, the widgets whose own condition or choices the group's value decides: each widget
// that reads the group and lies under no hidden widget, whose condition or choices give one answer with every value
// of the group and another with none, every other group at its own value.
func listApplies(evaluator *evaluator, groups []Group, hidden []int32) map[string][]int32 {
	widgets := evaluator.window.Widgets
	hides := lo.Keyify(hidden)
	under := make([]bool, len(widgets))
	for _, widget := range widgets[1:] {
		_, held := hides[widget.Parent]
		under[widget.ID] = under[widget.Parent] || held
	}
	applies := map[string][]int32{}
	for _, group := range groups {
		picked := evaluator.values[group.ID]
		for _, reader := range group.Readers {
			evaluator.values[group.ID] = pickEvery
			every, everyChoices := evaluator.isFalse(reader), evaluator.listWidgetChoices(reader)
			evaluator.values[group.ID] = pickNone
			isDecided := every != evaluator.isFalse(reader) || !slices.Equal(everyChoices, evaluator.listWidgetChoices(reader))
			if !under[reader] && isDecided {
				applies[group.ID] = append(applies[group.ID], reader)
			}
		}
		evaluator.values[group.ID] = picked
	}
	return applies
}

// isFalse reports whether the own condition of a widget is known not to hold under the values, which hides it.
func (evaluator *evaluator) isFalse(widget int32) bool {
	evaluator.context = evaluator.contexts[widget]
	return lo.SomeBy(listConditions(evaluator.window.Widgets[widget]), func(condition string) bool {
		holds, known := evaluator.evaluateCondition(condition)
		return known && !holds
	})
}

// isTrue reports whether a condition is known to hold under the values, read in the evaluator's context.
func (evaluator *evaluator) isTrue(condition string) bool {
	holds, known := evaluator.evaluateCondition(condition)
	return known && holds
}

// reach returns a call as the evaluator's context reads it: its macros expanded, then written from the window down.
func (evaluator *evaluator) reach(call jomini.Call) jomini.Call {
	return evaluator.context.reach(evaluator.schema.ResolveMacro(call, evaluator.context.latest))
}

// reach returns a call written from the window down: a first step that names the type of one of the context's
// objects becomes the call that reaches that object. Any other call is returned as it is.
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
// context it writes, in the order written, each reached from the ones before. A data context that opens with the
// name of a type reads the object of that type, as the game does.
func listContexts(schema *schema.Schema, window Window) []context {
	contexts := make([]context, len(window.Widgets))
	for _, widget := range window.Widgets {
		current := context{paths: map[string]jomini.Call{}}
		if widget.ID > 0 {
			holder := contexts[widget.Parent]
			current = context{paths: maps.Clone(holder.paths), latest: holder.latest}
		}
		for _, call := range listCalls(widget, game.GUI.ContextKey) {
			path := current.reach(schema.ResolveMacro(call, current.latest))
			dataType := cmp.Or(schema.ResolveDatacontext(path, ""), schema.ResolveDatacontext(call, current.latest))
			if dataType != "" {
				current.paths[dataType], current.latest = path, dataType
			}
		}
		contexts[widget.ID] = current
	}
	return contexts
}

// listCalls returns the data calls that a widget writes under the given keys, in the order written.
func listCalls(widget Widget, keys ...string) []jomini.Call {
	return lo.FilterMap(widget.Properties, func(property Property, _ int) (jomini.Call, bool) {
		call, ok := jomini.ParseBracketed(property.Value, 0)
		return call, ok && slices.ContainsFunc(keys, func(key string) bool { return strings.EqualFold(key, property.Key) })
	})
}

// findPressCondition returns the condition a widget is drawn down by, else the one it is drawn checked by. The last
// result is false if it writes neither as a data call.
func findPressCondition(widget Widget) (string, bool) {
	for _, key := range []string{game.GUI.DownKey, game.GUI.CheckedKey} {
		if value := findProperty(widget.Properties, key).Value; strings.HasPrefix(strings.TrimSpace(value), "[") {
			return value, true
		}
	}
	return "", false
}

// listConditions returns the condition that a widget is drawn by: the value of the first of the game's hiding keys
// that it writes. It returns none if the widget writes none of them.
func listConditions(widget Widget) []string {
	for _, key := range game.GUI.HiddenKeys {
		if property := findProperty(widget.Properties, key); property.Value != "" {
			return []string{property.Value}
		}
	}
	return nil
}

// listGroups returns the groups that the conditions of a window's widgets read, those their choices between two values
// are made by too, in the order they are first read, each with its readers. A group offers the values the window asks
// of it, then every value where it has none or several, then none. The window's own widget is always drawn, so its
// conditions are not read.
func listGroups(schema *schema.Schema, window Window, contexts []context) []Group {
	var groups []Group
	positions := map[string]int{}
	for _, widget := range window.Widgets[1:] {
		calls := lo.FilterMap(listConditions(widget), func(condition string, _ int) (jomini.Call, bool) {
			return jomini.ParseBracketed(condition, 0)
		})
		for _, call := range append(calls, listChoiceConditions(widget)...) {
			for _, fact := range listFacts(schema, call, contexts[widget.ID].latest) {
				id, value, _, _ := splitFact(schema, contexts[widget.ID].reach(fact))
				position, known := positions[id]
				if !known {
					position, positions[id] = len(groups), len(groups)
					groups = append(groups, makeGroup(id))
				}
				group := &groups[position]
				if value != "" && !slices.Contains(group.Options, value) {
					group.Options = append(group.Options, value)
				}
				if len(group.Readers) == 0 || group.Readers[len(group.Readers)-1] != widget.ID {
					group.Readers = append(group.Readers, widget.ID)
				}
			}
		}
	}
	for position := range groups {
		group := &groups[position]
		group.Options = append(group.Options, lo.Ternary(len(group.Options) == 1, []string{}, []string{pickEvery})...)
		group.Options = append(group.Options, pickNone)
	}
	return groups
}

// makeGroup returns the group of a call, with the object it asks about and what it asks, in words.
func makeGroup(id string) Group {
	call, _ := jomini.ParseCall(id, 0)
	group := describe(call)
	group.ID = id
	return group
}

// describe returns the words of a call's group: the object it asks about, as its kind and the key that picks one of
// that kind, and what it asks of it. The object is the last of the call's start and at most one step past it, as a
// window or a row reads it, and a call of one step asks about the game. What it asks is each step past the object,
// then the last step. A check that an object further on exists is labeled by that object alone.
func describe(call jomini.Call) Group {
	last := len(call.Steps) - 1
	switch {
	case last < 0:
		return Group{}
	case last == 0:
		given := lo.FilterMap(call.Steps[0].Arguments, func(argument jomini.Argument, _ int) (string, bool) {
			return toCallText(lo.FromPtr(argument.Call)), argument.Call != nil
		})
		return Group{Subject: "Game", Label: strings.Join(append([]string{toPhrase(call.Steps[0], false)}, given...), " ")}
	}
	end := min(last, directSteps-1)
	path := lo.Map(call.Steps[end:last], func(step jomini.Step, _ int) string { return toPhrase(step, true) })
	check := call.Steps[last]
	exists := len(path) > 0 && check.Text == game.GUI.ValidFunction && len(check.Arguments) == 0
	if !exists {
		path = append(path, toPhrase(check, false))
	}
	keys := lo.FlatMap(call.Steps[:end], func(step jomini.Step, _ int) []string { return listGiven(step) })
	runes := []rune(toPhrase(jomini.Step{Text: call.Steps[end-1].Text}, true))
	return Group{
		Subject: string(unicode.ToUpper(runes[0])) + string(runes[1:]), Key: strings.Join(keys, " "),
		Label: strings.Join(path, " › "), Exists: exists,
	}
}

// toPhrase returns one step of a call in words, with its quoted arguments. A getter's leading word is dropped from a
// step that reaches an object.
func toPhrase(step jomini.Step, reaches bool) string {
	name := step.Text
	for _, getter := range game.GUI.Getters {
		if rest, ok := strings.CutPrefix(name, getter); reaches && ok && rest != "" && unicode.IsUpper([]rune(rest)[0]) {
			name = rest
			break
		}
	}
	return strings.Join(append([]string{schema.ToPhrase(name)}, listGiven(step)...), " ")
}

// listGiven returns the quoted arguments of a step that are not empty, in order.
func listGiven(step jomini.Step) []string {
	return lo.Filter(listQuoted(step), func(text string, _ int) bool { return text != "" })
}

// listFacts returns the calls that a condition reads. A boolean function reads its operands, so it looks through And,
// Or and the rest, and any other call reads itself. A macro that expands into itself reads nothing.
func listFacts(schema *schema.Schema, call jomini.Call, datacontext string) []jomini.Call {
	call = expandVariableQuery(schema.ResolveMacro(call, datacontext))
	if len(call.Steps) == 0 {
		return nil
	}
	if _, _, operands, ok := splitBoolean(call); ok {
		return lo.FlatMap(operands, func(operand jomini.Call, _ int) []jomini.Call {
			return listFacts(schema, operand, datacontext)
		})
	}
	return []jomini.Call{call}
}

// expandVariableQuery returns a call that asks whether a GUI variable is unset or set to a value written out in the two
// facts it reads: NotExistOrHasValue('x', 'v') is Or(Not(Exists('x')), HasValue('x', 'v')) on the same variables. Any
// other call comes back as it is.
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
// flips the result, and its operands. The last result is false if the call is no boolean function. And3 counts as And.
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
	case "or":
	default:
		return false, false, nil, false
	}
	operands = lo.FilterMap(step.Arguments, func(argument jomini.Argument, _ int) (jomini.Call, bool) {
		return lo.FromPtr(argument.Call), argument.Call != nil
	})
	return conjunction, negated, operands, true
}

// splitFact splits a call into the group it reads, the value it asks of the group and the step that holds the value.
// The value is the last quoted argument of the call's last step, and the group is the call's text with it blanked. A
// quoted argument of an earlier step names the object the group asks about, and a GUI variable's name stays in its
// group too. A call with no such argument is its own group with no value, and the last result is false.
func splitFact(schema *schema.Schema, call jomini.Call) (group, value string, step int, quoted bool) {
	last := len(call.Steps) - 1
	if last < 0 {
		return "", "", 0, false
	}
	leading := lo.Ternary(last > 0 && isVariables(schema, call.Steps[:last]), 1, 0)
	blank := -1
	for position, argument := range call.Steps[last].Arguments {
		if argument.Quoted && position >= leading {
			blank, value = position, argument.Text
		}
	}
	if blank < 0 {
		return toCallText(call), "", 0, false
	}
	return toBlankedText(call, last, blank), value, last, true
}

// isVariables reports whether the given steps reach the GUI's variable system.
func isVariables(schema *schema.Schema, steps []jomini.Step) bool {
	types := schema.ResolveCallTypes(jomini.Call{Steps: steps}, "")
	return len(types) == len(steps) && types[len(types)-1] == game.GUI.Variables.Type
}

// isDirect reports whether a group asks whether an object exists that the window or a row reads directly: the object
// at most one step from where its call starts, such as the window's character or a row's own. Such an object exists as
// the window opens, and one reached further does not.
func isDirect(id string) bool {
	call, ok := jomini.ParseCall(id, 0)
	if !ok || len(call.Steps) == 0 || len(call.Steps) > directSteps {
		return false
	}
	last := call.Steps[len(call.Steps)-1]
	return last.Text == game.GUI.ValidFunction && len(last.Arguments) == 0
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

// toBlankedText returns the text of a call with the quoted argument at the given step and position blanked.
func toBlankedText(call jomini.Call, step, argument int) string {
	steps := slices.Clone(call.Steps)
	steps[step].Arguments = slices.Clone(steps[step].Arguments)
	steps[step].Arguments[argument].Text = ""
	return toCallText(jomini.Call{Steps: steps})
}

// toObject returns the call that a group's conditions read, up to its last step, as the group's text writes it with
// its blank kept. A group with a single step has none.
func toObject(group string) string {
	call, ok := jomini.ParseCall(group, 0)
	if !ok || len(call.Steps) < 2 {
		return ""
	}
	return toCallText(jomini.Call{Steps: call.Steps[:len(call.Steps)-1]})
}

// evaluateCondition works out whether a condition holds under the values of the groups. It is unknown, which the second
// result says, for a condition that is no data call.
func (evaluator *evaluator) evaluateCondition(condition string) (value, known bool) {
	if literal, ok := game.GUI.Literals[strings.ToLower(condition)]; ok {
		return literal, true
	}
	call, ok := jomini.ParseBracketed(condition, 0)
	if !ok {
		return false, false
	}
	return evaluator.evaluateCall(call)
}

// evaluateCall works out whether a call holds under the values of the groups. A fact holds when its group is at its
// value or at every value, and fails when the group is at none or at another value. A boolean function combines its
// operands. It is unknown when the known operands do not decide it, or for a fact of a group the window does not have.
func (evaluator *evaluator) evaluateCall(call jomini.Call) (value, known bool) {
	call = expandVariableQuery(evaluator.schema.ResolveMacro(call, evaluator.context.latest))
	if len(call.Steps) == 0 {
		return false, false
	}
	conjunction, negated, operands, ok := splitBoolean(call)
	if !ok {
		group, asked, _, _ := splitFact(evaluator.schema, evaluator.context.reach(call))
		picked, has := evaluator.values[group]
		return picked == pickEvery || picked != pickNone && picked == asked, has
	}
	var anyTrue, anyFalse, anyUnknown bool
	for _, operand := range operands {
		operandValue, operandKnown := evaluator.evaluateCall(operand)
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
	case conjunction && anyFalse, !conjunction && anyTrue:
		value = !conjunction
	case anyUnknown:
		return false, false
	default:
		value = conjunction
	}
	return value != negated, true
}

// revealCondition adds to the required values the ones that make a condition give the wanted result: hold or fail. It
// returns false if a group is already required to be at another value, which means the conditions contradict. A
// condition that is no data call asks for nothing.
func (evaluator *evaluator) revealCondition(condition string, holds bool) bool {
	if literal, ok := game.GUI.Literals[strings.ToLower(condition)]; ok {
		return literal == holds
	}
	call, ok := jomini.ParseBracketed(condition, 0)
	return !ok || evaluator.revealCall(call, holds)
}

// revealCall adds the values that make a call give the wanted result: hold or fail. A fact is required at its value,
// or at none to fail. A boolean function needs all its operands or just the first: And needs all to hold and one to
// fail, Or the reverse, and Not and Nor flip the result they ask of their operands.
func (evaluator *evaluator) revealCall(call jomini.Call, holds bool) bool {
	call = expandVariableQuery(evaluator.schema.ResolveMacro(call, evaluator.context.latest))
	if len(call.Steps) == 0 {
		return true
	}
	conjunction, negated, operands, ok := splitBoolean(call)
	if !ok {
		group, asked, _, _ := splitFact(evaluator.schema, evaluator.context.reach(call))
		pick := lo.Ternary(holds, lo.Ternary(asked == "", pickEvery, asked), pickNone)
		if existing, has := evaluator.required[group]; has && existing != pick {
			return false
		}
		evaluator.required[group] = pick
		return true
	}
	operandHolds := holds != negated
	every := conjunction == operandHolds
	for position, operand := range operands {
		if (position == 0 || every) && !evaluator.revealCall(operand, operandHolds) {
			return false
		}
	}
	return true
}
