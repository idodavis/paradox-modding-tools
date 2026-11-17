// interpret.go reads a window's data calls as conditions and works out which
// widgets the picks hide, and which picks show a hidden widget.

package draw

import (
	"cmp"
	"fmt"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/lang"
)

// States is one way of drawing a window: as it opens, changed by the picks.
type States struct {
	// Values holds the value each group is at.
	Values map[string]string `json:"values"`

	// Hidden lists the widgets the values hide.
	Hidden []int32 `json:"hidden"`

	// Applies holds, by group, the shown widgets whose own condition or
	// choices the group's value decides.
	Applies map[string][]int32 `json:"applies"`

	// Choices holds, by widget, the values the groups give its properties.
	Choices map[int32][]Choice `json:"choices"`
}

// context is where a widget's data calls start from: for each data type, the
// call that leads from the window to the object of that type.
type context struct {
	paths map[string]jomini.Call

	// latest is the data type of the data context written last.
	latest string
}

// condition is one condition of a widget, worked out once as the window
// opens. The zero condition is one nothing can be said of.
type condition struct {
	kind conditionKind

	// negated is whether a boolean function flips its result.
	negated  bool
	operands []condition

	// holds is a decided condition's result.
	holds bool

	// group is the group a fact asks about, and asked the value it asks of
	// it, "" where it asks only whether the group holds. call is the group's
	// call.
	group string
	asked string
	call  jomini.Call
}

// conditionKind is what a condition is.
type conditionKind uint8

// An unknown condition is no data call, or an empty one. A decided one is
// settled as the window opens, by its data models or a plain yes or no. A
// fact asks a value of a group. All and any are boolean functions that need
// all their operands, or one.
const (
	conditionUnknown conditionKind = iota
	conditionDecided
	conditionFact
	conditionAll
	conditionAny
)

// selection is a property written as a choice between two values, as
// Select_float( condition, a, b ) is. A condition that is no data call is the
// zero condition, which chooses nothing.
type selection struct {
	key       string
	condition condition

	// branches are the value chosen where the condition holds, then the one
	// chosen where it fails.
	branches [2]branch
}

// branch is one of the two values of a selection.
type branch struct {
	value string

	// isCall is whether the value is a data call. Such a value is chosen only
	// where it is itself a selection, held in nested.
	isCall bool
	nested *selection
}

// rules is what one widget asks of the window's groups.
type rules struct {
	// shown is the condition the widget is shown on. The widget is hidden
	// where it is known not to hold.
	shown condition

	// press is the condition that draws the widget down or checked, nil for
	// none.
	press *condition

	choices []selection
}

// evaluator works out whether the conditions of a window's widgets hold at
// the groups' values, and which values would make them hold.
type evaluator struct {
	schema   *lang.Schema
	window   Window
	contexts []context
	groups   []Group

	// rules holds what each widget asks of the groups, by widget.
	rules []rules

	// objects holds the groups by their call without its last step.
	objects map[string][]Group

	// strips holds, for each widget in a strip, the strip's widgets in order.
	strips map[int32][]int32

	// values holds the value each group is at.
	values map[string]string

	// rows is how many rows a data model's list draws, which is the model's
	// size as the window opens.
	rows int
}

// The picks that stand for every value of a group and for none.
const (
	pickEvery = "*every"
	pickNone  = "none"
)

// clickKeys are the keys whose calls run as a widget is clicked.
var clickKeys = []string{"onclick"}

// downKey holds while a button is drawn down; buttons of one type written
// side by side with it are tabs. checkedKey holds while a box is checked.
const (
	downKey    = "down"
	checkedKey = "checked"
)

// The data type and the functions of the GUI's own variables. Each function
// takes the variable's name first.
const (
	variableType = "VariableSystem"

	// variableExists holds while a variable is set, variableHasValue while it
	// is set to the value, and variableUnsetOrValue while it is unset or set
	// to the value.
	variableExists       = "Exists"
	variableHasValue     = "HasValue"
	variableUnsetOrValue = "NotExistOrHasValue"

	// variableSet sets a variable, to the value if one is given.
	// variableClear unsets it, and variableToggle flips whether it is set.
	variableSet    = "Set"
	variableClear  = "Clear"
	variableToggle = "Toggle"
)

// The data functions that combine conditions, without the count of
// arguments some of them end in.
const (
	booleanAnd = "And"
	booleanOr  = "Or"
	booleanNor = "Nor"
	booleanNot = "Not"
)

// literals are the plain values of a condition, in lower case, each with
// whether it holds.
var literals = map[string]bool{"yes": true, "no": false}

// modelSize gives how many items a data model has. modelEmpty asks whether it
// has none, and modelHas whether it has some.
const (
	modelSize  = "GetDataModelSize"
	modelEmpty = "IsDataModelEmpty"
	modelHas   = "DataModelHasItems"
)

// EvaluateStates works out which widgets of one window in a GUI file are
// hidden. Each group is at the value the window opens with, unless the given
// picks, by group, pick another. It returns an error if no workspace is open
// or the file does not define the window.
func (s *Service) EvaluateStates(path, name string, picks map[string]string) (States, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	opened, err := s.findWindow(path, name)
	if err != nil {
		return States{}, err
	}

	evaluator, library := opened.evaluator.applyPicks(picks), opened.library
	window := &evaluator.window
	hidden := []int32{}
	for _, widget := range window.Widgets[1:] {
		if evaluator.isFalse(widget.ID) {
			hidden = append(hidden, widget.ID)
		}
	}

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
		picked := picks[group.ID]
		if picked == "" {
			continue
		}

		for _, reader := range group.Readers {
			choices[reader] = append(choices[reader], library.pickPicture(*window.Widgets[reader].Pictures, picked))
		}
	}

	pickColors(opened, picks, choices)
	evaluator.pickTiers(titleTiers[library.ws.Game.ID], opened.tiers, picks, choices)

	return States{Values: values, Hidden: hidden, Applies: applies, Choices: choices}, nil
}

// Reveal returns the picks that make a hidden widget of one window in a GUI
// file visible, on top of the given picks, which stay as they are. It returns
// nil if the conditions that hide the widget contradict each other or it
// stays hidden anyway. It returns an error if no workspace is open, the file
// does not define the window, or the window has no such widget.
func (s *Service) Reveal(path, name string, picks map[string]string, widget int32) (map[string]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	opened, err := s.findWindow(path, name)
	if err != nil {
		return nil, err
	}

	window := opened.evaluator.window
	if widget < 0 || int(widget) >= len(window.Widgets) {
		return nil, fmt.Errorf("the window %q has no widget %d", name, widget)
	}

	revealer, wanted := opened.evaluator.applyPicks(picks), map[string]string{}
	for id := widget; id > 0; id = window.Widgets[id].Parent {
		if revealer.isFalse(id) && !revealer.rules[id].shown.reveal(wanted, true) {
			return nil, nil
		}
	}

	merged := map[string]string{}
	maps.Copy(merged, picks)
	maps.Copy(merged, wanted)
	checker := opened.evaluator.applyPicks(merged)
	for id := widget; id > 0; id = window.Widgets[id].Parent {
		if checker.isFalse(id) {
			return nil, nil
		}
	}

	return wanted, nil
}

// openEvaluator returns the evaluator of a resolved window at the values it
// opens with, each data model's list drawing the given number of rows.
func openEvaluator(schema *lang.Schema, window Window, contexts []context, count int) *evaluator {
	evaluator := &evaluator{
		schema: schema, window: window, contexts: contexts, values: map[string]string{}, rows: count,
		objects: map[string][]Group{},
	}
	evaluator.rules = evaluator.listRules()
	evaluator.groups = evaluator.listGroups()
	for _, group := range evaluator.groups {
		object := ""
		if steps := group.call.Steps; len(steps) > 1 {
			object = toCallText(jomini.Call{Steps: steps[:len(steps)-1]})
		}

		evaluator.objects[object] = append(evaluator.objects[object], group)
	}

	evaluator.strips = evaluator.listStrips()
	evaluator.open()
	classifyGroups(evaluator)

	return evaluator
}

// listRules returns what each widget of the window asks of the groups, and
// fills in the window's Conditions as written.
func (e *evaluator) listRules() []rules {
	window := &e.window
	window.Conditions = map[int32][]string{}
	all := make([]rules, len(window.Widgets))
	for _, widget := range window.Widgets {
		context, rule := e.contexts[widget.ID], &all[widget.ID]
		if property, call := window.findCondition(widget); property.Value != "" {
			rule.shown = e.makeWritten(context, property.Value, call)
			window.Conditions[widget.ID] = []string{property.Value}
		}

		for _, key := range []string{downKey, checkedKey} {
			property, call := window.findCall(widget, key)
			if strings.HasPrefix(strings.TrimSpace(property.Value), "[") {
				rule.press = new(e.makeWritten(context, property.Value, call))

				break
			}
		}

		for _, position := range widget.Properties {
			if call := window.calls[position]; call != nil {
				if choice, ok := e.makeSelection(context, *call); ok {
					choice.key = window.Properties[position].Key
					rule.choices = append(rule.choices, choice)
				}
			}
		}
	}

	return all
}

// makeWritten returns the condition a property's value writes: a plain yes
// or no, or a data call, given as parsed. Any other value says nothing.
func (e *evaluator) makeWritten(context context, text string, call *jomini.Call) condition {
	if literal, ok := literals[strings.ToLower(text)]; ok {
		return condition{kind: conditionDecided, holds: literal}
	}

	if call == nil {
		return condition{}
	}

	return e.makeCondition(context, *call)
}

// makeCondition returns the condition a data call is, read from the given
// context. Macros are written out first. A fact the window's data models
// decide is decided here; any other fact asks a value of its group.
func (e *evaluator) makeCondition(context context, call jomini.Call) condition {
	call = expandVariableQuery(e.schema.ResolveMacro(call, context.latest))
	if len(call.Steps) == 0 {
		return condition{}
	}

	if kind, negated, operands, ok := splitBoolean(call); ok {
		made := condition{kind: kind, negated: negated}
		for _, operand := range operands {
			made.operands = append(made.operands, e.makeCondition(context, operand))
		}

		return made
	}

	if holds, computed := computeFact(e.schema, context, call, e.rows); computed {
		return condition{kind: conditionDecided, holds: holds}
	}

	blank, group, asked := splitFact(e.schema, context.reach(call))

	return condition{kind: conditionFact, group: group, asked: asked, call: blank}
}

// makeSelection returns the selection a data call is, read from the given
// context. It returns false for a call that is no choice between two values.
func (e *evaluator) makeSelection(context context, call jomini.Call) (selection, bool) {
	arguments, ok := splitSelect(call)
	if !ok {
		return selection{}, false
	}

	var made selection
	if arguments[0].Call != nil {
		made.condition = e.makeCondition(context, *arguments[0].Call)
	}

	for position, argument := range arguments[1:] {
		switch {
		case argument.Call != nil:
			made.branches[position].isCall = true
			if nested, isChoice := e.makeSelection(context, *argument.Call); isChoice {
				made.branches[position].nested = &nested
			}

		case argument.Quoted:
			made.branches[position].value = toWrittenValue(argument.Text)

		default:
			made.branches[position].value = argument.Text
		}
	}

	return made, true
}

// listFacts appends the facts a condition asks of the groups, leaving out
// those decided as the window opens.
func (c *condition) listFacts(facts []*condition) []*condition {
	switch c.kind {
	case conditionUnknown, conditionDecided:
		return facts

	case conditionFact:
		return append(facts, c)
	}

	for position := range c.operands {
		facts = c.operands[position].listFacts(facts)
	}

	return facts
}

// listFacts appends the facts a selection's condition asks of the groups,
// then those of the selections nested in it.
func (s *selection) listFacts(facts []*condition) []*condition {
	facts = s.condition.listFacts(facts)
	for _, branch := range s.branches {
		if branch.nested != nil {
			facts = branch.nested.listFacts(facts)
		}
	}

	return facts
}

// applyPicks returns a copy of the evaluator with each group at the value the
// picks give it, else at its own.
func (e *evaluator) applyPicks(picks map[string]string) *evaluator {
	picked := *e
	picked.values = maps.Clone(e.values)
	for _, group := range picked.groups {
		if value, ok := picks[group.ID]; ok {
			picked.values[group.ID] = value
		}
	}

	return &picked
}

// open sets the values the window opens with. Every group starts at none,
// except one that asks what holds whenever a window opens, which starts at
// every value. Then the opening states' calls set theirs, and the first
// widget of each strip is pressed.
func (e *evaluator) open() {
	for _, group := range e.groups {
		e.values[group.ID] = pickNone
		if holdsOnOpening(e.schema.Game, group.call) {
			e.values[group.ID] = pickEvery
		}
	}

	wanted := map[string]string{}
	for _, widget := range e.window.Widgets {
		for _, call := range listCalls(&e.window, widget, openingCalls) {
			e.click(wanted, e.contexts[widget.ID], call)
		}
	}

	maps.Copy(e.values, wanted)
	for _, widget := range e.window.Widgets {
		if strip := e.strips[widget.ID]; len(strip) > 0 && strip[0] == widget.ID {
			wanted = map[string]string{}
			e.press(wanted, widget.ID)
			maps.Copy(e.values, wanted)
		}
	}
}

// press adds to the wanted values what a press of a widget sets. A widget
// that a condition draws down makes that condition hold, and the conditions
// of the other widgets of its strip fail.
func (e *evaluator) press(wanted map[string]string, widget int32) {
	context, window := e.contexts[widget], &e.window
	condition := e.rules[widget].press
	for _, call := range listCalls(window, window.Widgets[widget], clickKeys...) {
		if condition != nil {
			e.setVariable(wanted, context.expand(e.schema, call))
		} else {
			e.click(wanted, context, call)
		}
	}

	if condition == nil {
		return
	}

	strip := e.strips[widget]
	holds, known := condition.evaluate(e.values)
	condition.reveal(wanted, len(strip) > 0 || !known || !holds)
	for _, other := range strip {
		if pressed := e.rules[other].press; pressed != nil && other != widget {
			pressed.reveal(wanted, false)
		}
	}
}

// click adds to the wanted values what a clicked call sets: a value of a
// group on the same object, or a GUI variable.
func (e *evaluator) click(wanted map[string]string, context context, call jomini.Call) {
	call = context.expand(e.schema, call)
	if group, value, ok := e.findSelected(call); ok {
		wanted[group] = value

		return
	}

	e.setVariable(wanted, call)
}

// findSelected returns the group beside a call of one quoted argument on the
// same object, and the value the argument names: SelectTab('x') selects x of
// the IsTabSelected group beside it. It returns false if no group offers it.
func (e *evaluator) findSelected(call jomini.Call) (group, value string, ok bool) {
	last := len(call.Steps) - 1
	if last < 1 {
		return "", "", false
	}

	quoted := listQuoted(call.Steps[last])
	if len(quoted) != 1 {
		return "", "", false
	}

	for _, candidate := range e.objects[toCallText(jomini.Call{Steps: call.Steps[:last]})] {
		if slices.Contains(candidate.Options, quoted[0]) {
			return candidate.ID, quoted[0], true
		}
	}

	return "", "", false
}

// setVariable adds to the wanted values what a call to the GUI's variables
// sets. Any other call sets nothing.
func (e *evaluator) setVariable(wanted map[string]string, call jomini.Call) {
	last := len(call.Steps) - 1
	if last < 1 || !isVariables(e.schema, call.Steps[:last]) {
		return
	}

	quoted := listQuoted(call.Steps[last])
	if len(quoted) == 0 {
		return
	}

	_, exists, _ := splitFact(e.schema, replaceLastStep(call, variableExists, quoted[:1]))
	_, hasValue, _ := splitFact(e.schema, replaceLastStep(call, variableHasValue, []string{quoted[0], ""}))
	switch call.Steps[last].Text {
	case variableSet:
		wanted[exists], wanted[hasValue] = pickEvery, pickNone
		if len(quoted) > 1 {
			wanted[hasValue] = quoted[1]
		}

	case variableClear:
		wanted[exists], wanted[hasValue] = pickNone, pickNone

	case variableToggle:
		wanted[exists] = pickEvery
		if e.values[exists] == pickEvery {
			wanted[exists] = pickNone
		}
	}
}

// isFalse reports whether a condition a widget is shown on is known not to
// hold at the values, which hides the widget.
func (e *evaluator) isFalse(widget int32) bool {
	holds, known := e.rules[widget].shown.evaluate(e.values)

	return known && !holds
}

// expand returns a call as read from the context: its macros written out,
// then written from the window down.
func (c context) expand(schema *lang.Schema, call jomini.Call) jomini.Call {
	return c.reach(schema.ResolveMacro(call, c.latest))
}

// reach returns a call written from the window down: a first step that is the
// data type of one of the context's objects becomes the call that leads to
// that object.
func (c context) reach(call jomini.Call) jomini.Call {
	if len(call.Steps) == 0 {
		return call
	}

	path, ok := c.paths[call.Steps[0].Text]
	if !ok {
		return call
	}

	return jomini.Call{Steps: slices.Concat(path.Steps, call.Steps[1:])}
}

// listContexts returns where each widget's data calls start from: where its
// holder's start, then each data context the widget writes.
func listContexts(schema *lang.Schema, window Window) []context {
	contexts := make([]context, len(window.Widgets))
	for _, widget := range window.Widgets {
		current := context{paths: map[string]jomini.Call{}}
		if widget.ID > 0 {
			holder := contexts[widget.Parent]
			current = context{paths: maps.Clone(holder.paths), latest: holder.latest}
		}

		for _, call := range listCalls(&window, widget, lang.GUIContextKey) {
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

// listCalls returns the data calls a widget writes under the given keys, in
// the order written.
func listCalls(window *Window, widget Widget, keys ...string) []jomini.Call {
	var calls []jomini.Call
	for _, position := range widget.Properties {
		call, written := window.calls[position], window.Properties[position].Key
		if call != nil && slices.ContainsFunc(keys, func(key string) bool { return strings.EqualFold(key, written) }) {
			calls = append(calls, *call)
		}
	}

	return calls
}

// comparisons are the data functions that compare two numbers, by their name
// before the underscore, each with the results of cmp.Compare it holds for.
var comparisons = map[string][]int{
	"LessThan": {-1}, "LessThanOrEqualTo": {-1, 0}, "GreaterThan": {1}, "GreaterThanOrEqualTo": {0, 1},
	"EqualTo": {0}, "NotEqualTo": {-1, 1},
}

// computeFact decides a fact about the window's data models as it opens, each
// list drawing the given number of rows: whether a model is empty or has
// items, or how two numbers compare. It returns false where it cannot decide.
func computeFact(schema *lang.Schema, context context, call jomini.Call, count int) (holds, computed bool) {
	if len(call.Steps) != 1 {
		return false, false
	}

	step := call.Steps[0]
	switch step.Text {
	case modelEmpty:
		return count == 0, true
	case modelHas:
		return count > 0, true
	}

	name, _, _ := strings.Cut(step.Text, "_")
	results, ok := comparisons[name]
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

// toNumbers returns the numbers an argument of a comparison may be. A data
// model's size is the given number of rows. A number an object holds is drawn
// as a sample of at least one, so it may be one or anything above. Any other
// argument gives none.
func toNumbers(schema *lang.Schema, context context, argument jomini.Argument, count int) []float64 {
	if argument.Call != nil {
		steps := argument.Call.Steps
		path := context.expand(schema, *argument.Call)
		function, _ := schema.FindFunction(path, "", len(path.Steps)-1)
		switch {
		case len(steps) == 1 && steps[0].Text == modelSize:
			return []float64{float64(count)}
		case len(steps) > 1 && callOutputs[function.Returns] == kindNumber:
			return []float64{minSample, math.Inf(1)}
		}

		return nil
	}

	number, err := strconv.ParseFloat(strings.TrimSpace(toWrittenValue(argument.Text)), 64)
	if err != nil {
		return nil
	}

	return []float64{number}
}

// expandVariableQuery rewrites NotExistOrHasValue('x', 'v') as
// Or(Not(Exists('x')), HasValue('x', 'v')) on the same variable. Any other
// call is returned as it is.
func expandVariableQuery(call jomini.Call) jomini.Call {
	last := len(call.Steps) - 1
	if last < 1 || call.Steps[last].Text != variableUnsetOrValue || len(listQuoted(call.Steps[last])) != 2 {
		return call
	}

	quoted := listQuoted(call.Steps[last])
	exists, hasValue := replaceLastStep(call, variableExists, quoted[:1]), replaceLastStep(call, variableHasValue, quoted)
	unset := jomini.Call{Steps: []jomini.Step{{Text: booleanNot, Arguments: []jomini.Argument{{Call: &exists}}}}}

	return jomini.Call{Steps: []jomini.Step{{
		Text: booleanOr, Arguments: []jomini.Argument{{Call: &unset}, {Call: &hasValue}},
	}}}
}

// splitBoolean returns what a boolean function combines: whether it needs
// all its operands or one, whether it flips the result, and its operands. It
// returns false for a call that is no boolean function.
func splitBoolean(call jomini.Call) (kind conditionKind, negated bool, operands []jomini.Call, ok bool) {
	if len(call.Steps) != 1 {
		return conditionUnknown, false, nil, false
	}

	step := call.Steps[0]
	switch strings.TrimRight(step.Text, "0123456789") {
	case booleanAnd:
		kind = conditionAll
	case booleanNot:
		kind, negated = conditionAll, true
	case booleanNor:
		kind, negated = conditionAny, true
	case booleanOr:
		kind = conditionAny
	default:
		return conditionUnknown, false, nil, false
	}

	for _, argument := range step.Arguments {
		if argument.Call != nil {
			operands = append(operands, *argument.Call)
		}
	}

	return kind, negated, operands, true
}

// splitFact splits a call into the group it asks about and the value it asks
// of it: the last quoted argument of the last step. The group's call is the
// call with that argument emptied. A call with no such argument is its own
// group and asks no value.
func splitFact(schema *lang.Schema, call jomini.Call) (blank jomini.Call, group, value string) {
	last := len(call.Steps) - 1
	if last < 0 {
		return call, "", ""
	}

	// a GUI variable's name is part of its group
	leading := 0
	if last > 0 && isVariables(schema, call.Steps[:last]) {
		leading = 1
	}

	asked := -1
	for position, argument := range call.Steps[last].Arguments {
		if argument.Quoted && position >= leading {
			asked, value = position, argument.Text
		}
	}

	if asked < 0 {
		return call, toCallText(call), ""
	}

	steps := slices.Clone(call.Steps)
	steps[last].Arguments = slices.Clone(steps[last].Arguments)
	steps[last].Arguments[asked].Text = ""
	blank = jomini.Call{Steps: steps}

	return blank, toCallText(blank), value
}

// isVariables reports whether the given steps lead to the GUI's variables.
func isVariables(schema *lang.Schema, steps []jomini.Step) bool {
	types := schema.ResolveCallTypes(jomini.Call{Steps: steps}, "")

	return len(types) == len(steps) && types[len(types)-1] == variableType
}

// openingFacts are the data functions with no arguments that hold whenever a
// window opens: an object exists, a list has items.
var openingFacts = []string{"IsValid", "HasItems"}

// sampleFacts are the data functions with no arguments that hold of the
// sample the view draws, a living, landed player ruler with legitimacy and a
// council, each with the games that declare it.
var sampleFacts = map[string][]string{
	"IsAlive":                        {game.CK3, game.EU5},
	"HasLandedTitles":                {game.CK3},
	"ShouldShowLegitimacy":           {game.CK3},
	"CanPotentiallySelectCouncillor": {game.CK3},
	"HasMainSkill":                   {game.CK3},
	"HasCouncillor":                  {game.CK3},
	"IsVisible":                      {game.CK3, game.Vic3, game.EU5},
	"CanSelect":                      {game.CK3, game.EU5},
}

// sampleAbsent are the data functions that lead to an object the sample
// lacks, each with the games that declare it. Any test through one fails.
var sampleAbsent = map[string][]string{
	"GetNextForcedCouncillor": {game.CK3},
}

// holdsOnOpening reports whether a group's call asks what holds as the window
// opens, by openingFacts or by the game's sampleFacts. It does not hold where
// the call leads through an object of sampleAbsent.
func holdsOnOpening(gameID string, call jomini.Call) bool {
	if len(call.Steps) == 0 {
		return false
	}

	last := call.Steps[len(call.Steps)-1]
	isAbsent := slices.ContainsFunc(call.Steps, func(step jomini.Step) bool {
		return slices.Contains(sampleAbsent[step.Text], gameID)
	})
	isFact := slices.Contains(openingFacts, last.Text) || slices.Contains(sampleFacts[last.Text], gameID)

	return isFact && !isAbsent && len(last.Arguments) == 0
}

// listQuoted returns the quoted arguments of a step, in order.
func listQuoted(step jomini.Step) []string {
	var quoted []string
	for _, argument := range step.Arguments {
		if argument.Quoted {
			quoted = append(quoted, argument.Text)
		}
	}

	return quoted
}

// replaceLastStep returns a call with its last step replaced by a function of
// the given quoted arguments.
func replaceLastStep(call jomini.Call, function string, quoted []string) jomini.Call {
	arguments := make([]jomini.Argument, len(quoted))
	for position, text := range quoted {
		arguments[position] = jomini.Argument{Text: text, Quoted: true}
	}

	steps := slices.Clone(call.Steps[:len(call.Steps)-1])

	return jomini.Call{Steps: append(steps, jomini.Step{Text: function, Arguments: arguments})}
}

// toCallText returns the text of a call.
func toCallText(call jomini.Call) string {
	steps := make([]string, len(call.Steps))
	for position, step := range call.Steps {
		steps[position] = step.Text
		if len(step.Arguments) == 0 {
			continue
		}

		arguments := make([]string, len(step.Arguments))
		for place, argument := range step.Arguments {
			switch {
			case argument.Call != nil:
				arguments[place] = toCallText(*argument.Call)
			case argument.Quoted:
				arguments[place] = "'" + argument.Text + "'"
			default:
				arguments[place] = argument.Text
			}
		}

		steps[position] += "(" + strings.Join(arguments, ", ") + ")"
	}

	return strings.Join(steps, ".")
}

// evaluate works out whether the condition holds at the groups' values. It is
// not known for a condition nothing can be said of, for a fact of a group the
// window lacks, or for a boolean function its known operands do not decide.
func (c *condition) evaluate(values map[string]string) (value, known bool) {
	switch c.kind {
	case conditionUnknown:
		return false, false

	case conditionDecided:
		return c.holds, true

	case conditionFact:
		picked, has := values[c.group]

		return picked == pickEvery || (picked != pickNone && picked == c.asked), has
	}

	conjunction := c.kind == conditionAll
	var anyTrue, anyFalse, anyUnknown bool
	for position := range c.operands {
		holds, operandKnown := c.operands[position].evaluate(values)
		switch {
		case !operandKnown:
			anyUnknown = true
		case holds:
			anyTrue = true
		default:
			anyFalse = true
		}
	}

	switch {
	// a false operand decides And, and a true one decides Or
	case conjunction && anyFalse, !conjunction && anyTrue:
		value = !conjunction

	case anyUnknown:
		return false, false

	default:
		value = conjunction
	}

	return value != c.negated, true
}

// reveal adds to the wanted values the ones that make the condition hold, or
// fail, as asked. It returns false if no values can: a group is already
// wanted at another value, or the condition is decided the other way as the
// window opens. A condition nothing can be said of asks for nothing.
func (c *condition) reveal(wanted map[string]string, holds bool) bool {
	switch c.kind {
	case conditionUnknown:
		return true

	case conditionDecided:
		return c.holds == holds

	case conditionFact:
		pick := pickNone
		if holds {
			pick = cmp.Or(c.asked, pickEvery)
		}

		if existing, has := wanted[c.group]; has && existing != pick {
			return false
		}

		wanted[c.group] = pick

		return true
	}

	// And holds when every operand holds, and fails when the first fails; Or
	// is the other way round
	operandHolds := holds != c.negated
	every := (c.kind == conditionAll) == operandHolds
	for position := range c.operands {
		if (position == 0 || every) && !c.operands[position].reveal(wanted, operandHolds) {
			return false
		}
	}

	return true
}
