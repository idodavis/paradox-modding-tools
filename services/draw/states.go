package draw

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/samber/lo"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/schema"
	"paradox-modding-tools/services/workspace"
)

// A Group is one choice the page lets the user switch, with the values it can take and the one picked: the data
// call that a window's conditions read, or the template list a coat's value is drawn from.
type Group struct {
	ID       string   `json:"id"`
	Label    string   `json:"label"`
	Options  []string `json:"options"`
	Picked   string   `json:"picked"`
	Database string   `json:"database,omitempty"` // the database whose definitions it offers; "" for any other group
}

// States describes one way of drawing a window: the groups the toolbar offers with their current picks, and the
// widgets those picks hide.
type States struct {
	Groups []Group `json:"groups"`
	Hidden []int32 `json:"hidden"`
}

// An evaluator works out whether conditions hold under the values that groups are at, or the values they have to be at.
type evaluator struct {
	schema      *schema.Schema
	datacontext string
	values      map[string]string          // the value each group is at
	decided     map[string]map[string]bool // for a group a chosen definition decides: whether each of its values holds
	required    map[string]string          // for a reveal: the value each group has to be at
}

// A scanner is a visitor that gathers the texts that some definitions write outside the script they run.
type scanner struct {
	spans map[jomini.Span]string     // the definitions to scan, by the span of their name
	texts map[string]map[string]bool // by definition: the texts it writes
}

// The picks that stand for every value of a group and for none.
const (
	pickEvery = "*every"
	pickNone  = "none"
)

// skippedShapes are the shapes of the blocks of a definition that hold script the game runs, which write nothing
// the definition itself has.
var skippedShapes = []string{"effects", "triggers"}

// EvaluateStates works out which widgets of a window are hidden under the given picks, where each pick maps a
// group's ID to the value chosen for it. A group with no pick takes its first option. Nothing about the game is
// simulated: a condition that reads a group with no value is unknown and hides nothing.
func (drw *Service) EvaluateStates(path, name string, picks map[string]string) (States, error) {
	ws, err := drw.Workspace.Session()
	if err != nil {
		return States{}, err
	}
	mods := ws.Mods.Load()
	window, err := resolveWindow(ws, mods, path, name)
	if err != nil {
		return States{}, err
	}
	return evaluate(ws, mods, window, picks), nil
}

// Reveal returns the picks that make a hidden widget visible, or nil if its conditions contradict each other.
func (drw *Service) Reveal(path, name string, widget int32) (map[string]string, error) {
	ws, err := drw.Workspace.Session()
	if err != nil {
		return nil, err
	}
	mods := ws.Mods.Load()
	window, err := resolveWindow(ws, mods, path, name)
	if err != nil {
		return nil, err
	}
	if widget < 0 || int(widget) >= len(window.Widgets) {
		return nil, fmt.Errorf("the window %q has no widget %d", name, widget)
	}
	datacontexts := listDatacontexts(ws.Schema, window)
	revealer := &evaluator{schema: ws.Schema, required: map[string]string{}}
	for id := widget; id > 0; id = window.Widgets[id].Parent {
		revealer.datacontext = datacontexts[id]
		for _, condition := range listConditions(window.Widgets[id]) {
			if !revealer.revealCondition(condition, true) {
				return nil, nil
			}
		}
	}
	states := evaluate(ws, mods, window, revealer.required)
	for id := widget; id > 0; id = window.Widgets[id].Parent {
		if slices.Contains(states.Hidden, id) {
			return nil, nil
		}
	}
	picks := lo.SliceToMap(states.Groups, func(group Group) (string, string) { return group.ID, lo.FirstOr(group.Options, "") })
	maps.Copy(picks, revealer.required)
	return picks, nil
}

// evaluate works out the states of an already resolved window under the given picks. A group that reads a kind of
// definition offers the definitions in the given index of the mods.
func evaluate(ws *workspace.Session, mods *index.Index, window Window, picks map[string]string) States {
	datacontexts := listDatacontexts(ws.Schema, window)
	groups, kept := listGroups(ws, mods, window, datacontexts)
	values := map[string]string{}
	for position := range groups {
		groups[position].Picked = cmp.Or(picks[groups[position].ID], lo.FirstOr(groups[position].Options, ""))
		values[groups[position].ID] = groups[position].Picked
	}
	decided := resolveDecided(ws, mods, groups, kept)
	for position, group := range groups {
		if holds, ok := decided[group.ID]; ok {
			groups[position].Picked = lo.FirstOr(lo.Filter(group.Options, func(option string, _ int) bool { return holds[option] }), pickNone)
		}
	}
	evaluator := &evaluator{schema: ws.Schema, values: values, decided: decided}
	hidden := []int32{}
	for _, widget := range window.Widgets[1:] {
		evaluator.datacontext = datacontexts[widget.ID]
		falsified := lo.SomeBy(listConditions(widget), func(condition string) bool {
			value, known := evaluator.evaluateCondition(condition)
			return known && !value
		})
		if falsified {
			hidden = append(hidden, widget.ID)
		}
	}
	return States{Groups: groups, Hidden: hidden}
}

// listDatacontexts returns the data context type of each widget of a window: the type its datacontext binding leads to
// from its holder's, else its holder's.
func listDatacontexts(schema *schema.Schema, window Window) []string {
	datacontexts := make([]string, len(window.Widgets))
	for _, widget := range window.Widgets {
		if widget.ID > 0 {
			datacontexts[widget.ID] = datacontexts[widget.Parent]
		}
		if call, ok := jomini.ParseBracketed(findProperty(widget.Properties, game.GUI.ContextKey).Value, 0); ok {
			datacontexts[widget.ID] = schema.ResolveDatacontext(call, datacontexts[widget.ID])
		}
	}
	return datacontexts
}

// listConditions returns the conditions that a widget writes: the values of its keys that hide it.
func listConditions(widget Widget) []string {
	return lo.FilterMap(widget.Properties, func(property Property, _ int) (string, bool) {
		return property.Value, slices.ContainsFunc(game.GUI.HiddenKeys, func(key string) bool {
			return strings.EqualFold(key, property.Key)
		})
	})
}

// listGroups returns the groups that the conditions of a window read, in the order they are first read, and the
// definitions the game keeps in each database a group leads to. A group offers the values the window asks of it. A
// group that leads to a database then offers its definitions, and any other every value and none.
func listGroups(ws *workspace.Session, mods *index.Index, window Window, datacontexts []string) ([]Group, map[string][]index.Winner) {
	var groups []Group
	kept := map[string][]index.Winner{}
	for _, widget := range window.Widgets {
		for _, condition := range listConditions(widget) {
			call, ok := jomini.ParseBracketed(condition, 0)
			if !ok {
				continue
			}
			for _, fact := range listFacts(ws.Schema, call, datacontexts[widget.ID]) {
				id, value, step, quoted := splitFact(fact)
				position := slices.IndexFunc(groups, func(group Group) bool { return group.ID == id })
				if position < 0 {
					groups = append(groups, Group{ID: id, Label: strings.Replace(id, "''", "…", 1)})
					position = len(groups) - 1
					if types := ws.Schema.ResolveCallTypes(fact, datacontexts[widget.ID]); quoted && step < len(types) {
						groups[position].Database, _ = ws.Schema.ResolveTypeDatabase(types[step])
					}
				}
				if value != "" && !slices.Contains(groups[position].Options, value) {
					groups[position].Options = append(groups[position].Options, value)
				}
			}
		}
	}
	for position, group := range groups {
		if group.Database == "" {
			groups[position].Options = slices.Concat(group.Options, []string{pickEvery, pickNone})
			continue
		}
		if _, listed := kept[group.Database]; !listed {
			kept[group.Database] = mods.ListKept(ws.Schema, group.Database)
		}
		for _, winner := range kept[group.Database] {
			groups[position].Options = append(groups[position].Options, mods.ToReference(winner.Site).Name)
		}
		groups[position].Options = lo.Uniq(groups[position].Options)
	}
	return groups, kept
}

// listFacts returns the calls that a condition reads, which are the ones that are not a boolean function: it looks
// through the operands of And, Or and the rest. A call that is a macro past the deepest allowed reads nothing.
func listFacts(schema *schema.Schema, call jomini.Call, datacontext string) []jomini.Call {
	call = resolveCall(schema, call, datacontext)
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

// resolveCall returns the call that a data call stands for, where macros that open the result are expanded in turn.
// A macro that is still one after the deepest allowed is a cycle, which leaves an empty call.
func resolveCall(schema *schema.Schema, call jomini.Call, datacontext string) jomini.Call {
	for range maxDepth + 1 {
		next := schema.ResolveMacro(call, datacontext)
		if toCallText(next) == toCallText(call) {
			return call
		}
		call = next
	}
	return jomini.Call{}
}

// splitBoolean returns what a call that is a boolean function combines: whether it needs all its operands or just one
// of them, whether it flips the result, and its operands. The call is not a boolean function if the last result is
// false. A function counts for any number of operands: And3 is And.
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
// The group is the call's text with its last quoted argument blanked, and the value is that argument. A call with no
// quoted argument is its own group, asks for no value and has no step, and the last result is false.
func splitFact(call jomini.Call) (group, value string, step int, quoted bool) {
	blank := 0
	for position, each := range call.Steps {
		for argumentPosition, argument := range each.Arguments {
			if argument.Quoted {
				step, blank, value, quoted = position, argumentPosition, argument.Text, true
			}
		}
	}
	if !quoted {
		return toCallText(call), "", 0, false
	}
	return toBlankedText(call, step, blank), value, step, true
}

// toCallText returns the text of a call.
func toCallText(call jomini.Call) string {
	steps := lo.Map(call.Steps, func(step jomini.Step, _ int) string {
		if len(step.Arguments) == 0 {
			return step.Text
		}
		arguments := lo.Map(step.Arguments, func(argument jomini.Argument, _ int) string {
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

// resolveDecided returns, for each group that a chosen definition decides, whether each of its values holds. A group
// with a database at one of its definitions decides the groups on the same object whose values are all written in some
// definition of that database: a value holds where the chosen definition writes it, and fails where it does not. The
// given map holds the definitions the game keeps in each database, as listGroups lists them.
func resolveDecided(ws *workspace.Session, mods *index.Index, groups []Group, kept map[string][]index.Winner) map[string]map[string]bool {
	decided := map[string]map[string]bool{}
	scanned := map[string]map[string]map[string]bool{}
	for _, chosen := range groups {
		database, object := chosen.Database, toObject(chosen.ID)
		siblings := lo.Filter(groups, func(other Group, _ int) bool {
			return other.ID != chosen.ID && other.Database == "" && toObject(other.ID) == object && len(listValues(other)) > 0
		})
		if database == "" || object == "" || len(siblings) == 0 {
			continue
		}
		if _, ok := scanned[database]; !ok {
			scanned[database] = scanDefinitions(ws, mods, kept[database])
		}
		if _, defined := scanned[database][chosen.Picked]; !defined {
			continue
		}
		for _, sibling := range siblings {
			values := listValues(sibling)
			if lo.EveryBy(values, func(value string) bool {
				return lo.SomeBy(lo.Values(scanned[database]), func(texts map[string]bool) bool { return texts[value] })
			}) {
				decided[sibling.ID] = lo.SliceToMap(values, func(value string) (string, bool) {
					return value, scanned[database][chosen.Picked][value]
				})
			}
		}
	}
	return decided
}

// listValues returns the values a group offers that a window asks of it, which leaves out every value and none.
func listValues(group Group) []string {
	return lo.Without(group.Options, pickEvery, pickNone)
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

// scanDefinitions returns the texts that each of the given definitions writes outside the script it runs, by the name
// of the definition.
func scanDefinitions(ws *workspace.Session, mods *index.Index, winners []index.Winner) map[string]map[string]bool {
	spans := map[int32]map[jomini.Span]string{}
	for _, winner := range winners {
		site := winner.Site
		if spans[site.File] == nil {
			spans[site.File] = map[jomini.Span]string{}
		}
		spans[site.File][mods.Occurrence(site).Span] = mods.ToReference(site).Name
	}
	texts := map[string]map[string]bool{}
	for file, wanted := range spans {
		if tree, err := ws.LoadTree(mods.Files[file].Path); err == nil {
			ws.Schema.Walk(tree, mods.Files[file], &scanner{spans: wanted, texts: texts})
		}
	}
	return texts
}

// Enter gathers the texts of a statement inside one of the definitions to scan. It skips the script blocks of the
// definition, and leaves the rest of the file's statements alone.
func (scanner *scanner) Enter(tree *jomini.Tree, statement int32, context *schema.Context) bool {
	if context.Definition == nil {
		return true
	}
	definition, ok := scanner.spans[context.Definition.Span]
	if !ok {
		return true
	}
	if context.Conditional {
		return false
	}
	if scanner.texts[definition] == nil {
		scanner.texts[definition] = map[string]bool{}
	}
	current := tree.Statements[statement]
	for _, text := range []string{tree.Unquoted(current.Key), tree.Unquoted(current.Value)} {
		scanner.texts[definition][text] = true
	}
	return !slices.Contains(skippedShapes, context.Shape)
}

// Leave does nothing.
func (scanner *scanner) Leave(*jomini.Tree, int32, *schema.Context) {}

// evaluateCondition works out whether a condition holds under the values of the groups. It is unknown, which the second
// result says, for a condition that is no data call or that reads a group with no value.
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
// value or at every value, and fails when the group is at none. A group a chosen definition decides holds where the
// definition writes the value. A boolean function combines its operands, and is unknown when the known operands do
// not decide it.
func (evaluator *evaluator) evaluateCall(call jomini.Call) (value, known bool) {
	call = resolveCall(evaluator.schema, call, evaluator.datacontext)
	if len(call.Steps) == 0 {
		return false, false
	}
	conjunction, negated, operands, ok := splitBoolean(call)
	if !ok {
		group, asked, _, _ := splitFact(call)
		if holds, decided := evaluator.decided[group]; decided {
			return holds[asked], true
		}
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

// revealCondition adds to the required values the ones that make a condition hold, or fail when the flag is false. It
// returns false if a group is already required to be at another value, which means the conditions contradict. A
// condition that is no data call asks for nothing.
func (evaluator *evaluator) revealCondition(condition string, holds bool) bool {
	if literal, ok := game.GUI.Literals[strings.ToLower(condition)]; ok {
		return literal == holds
	}
	call, ok := jomini.ParseBracketed(condition, 0)
	return !ok || evaluator.revealCall(call, holds)
}

// revealCall adds the values that make a call hold, or fail when the flag is false. A fact is required at its value,
// or at none to fail. A boolean function needs all its operands, or just the first, by what it combines and the result
// wanted: And needs all to hold and one to fail, Or the reverse, and Not and Nor flip the result they ask of them.
func (evaluator *evaluator) revealCall(call jomini.Call, holds bool) bool {
	call = resolveCall(evaluator.schema, call, evaluator.datacontext)
	if len(call.Steps) == 0 {
		return true
	}
	conjunction, negated, operands, ok := splitBoolean(call)
	if !ok {
		group, asked, _, _ := splitFact(call)
		pick := lo.Ternary(holds, cmp.Or(asked, pickEvery), pickNone)
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
