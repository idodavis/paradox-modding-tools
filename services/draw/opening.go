// opening.go works out which states of a widget run as its window opens, and
// what they set.

package draw

import (
	"iter"
	"slices"
	"strconv"
	"strings"

	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/lang"
)

// openingCalls is the key of a state whose calls run as the state starts.
const openingCalls = "on_start"

// The keys and data functions that lead one state of a widget to others, as
// [PdxGuiWidget.FindChild('icon').TriggerAnimation('1')] does.
const (
	// animationNextKey names the state that follows, animationFinishKey the
	// calls run as the state ends, and animationAlphaKey its alpha.
	animationNextKey   = "next"
	animationFinishKey = "on_finish"
	animationAlphaKey  = "alpha"

	// animationTrigger runs a state by name on the widget, and
	// animationFindChild leads to a child to run it on.
	animationTrigger   = "TriggerAnimation"
	animationFindChild = "FindChild"
)

// openingStates are the state names run as a window opens. openingTriggers
// are the keys whose given value runs a state as it opens.
var (
	openingStates   = []string{"_show"}
	openingTriggers = map[string]string{"trigger_on_create": "yes"}
)

// classifyState returns a state's name, "" for none, whether its widget is
// in it as the window opens, and whether it takes the widget off the screen.
// A state that fades to an alpha of 0 and runs calls as it ends does that, as
// a popup that removes itself once faded.
func (r *resolver) classifyState(state clause) (label string, opens, leaves bool) {
	fades, ends := false, false
	for field := range r.listStateFields(state) {
		key, value, _ := splitField(field)
		if key == lang.GUIWidgetKey && label == "" {
			label = value
		}

		runsByName := key == lang.GUIWidgetKey && slices.Contains(openingStates, value)
		runsByValue := value != "" && strings.EqualFold(openingTriggers[key], value)
		opens = opens || runsByName || runsByValue
		alpha, err := strconv.ParseFloat(value, 64)
		fades = fades || key == animationAlphaKey && err == nil && alpha == 0
		ends = ends || key == animationFinishKey && value != ""
	}

	return label, opens, fades && ends
}

// listStateFields yields each field of a state that writes a value.
func (r *resolver) listStateFields(state clause) iter.Seq[clause] {
	return func(yield func(clause) bool) {
		for lead, field := range r.listFields(state, 0) {
			if lead == "" && !field.tree.Statements[field.statement].IsBlock() && !yield(field) {
				return
			}
		}
	}
}

// resolveOpening returns the properties a widget has once its opening
// animation has run: those of the states it opens in or that an opening call
// starts on it, and of the states they lead to. Each state runs once, and one
// that takes the widget off the screen does not run.
func (r *resolver) resolveOpening(name string) []Property {
	states, queue, leaving := map[string]clause{}, []clause{}, map[clause]bool{}
	for _, state := range r.gathered.states {
		label, opens, leaves := r.classifyState(state)
		states[label], leaving[state] = state, leaves
		if opens {
			queue = append(queue, state)
		}
	}

	for _, label := range r.gathered.triggers[name] {
		if state, ok := states[label]; ok {
			queue = append(queue, state)
		}
	}

	var properties []Property
	run := map[clause]bool{}
	for len(queue) > 0 {
		state := queue[0]
		queue = queue[1:]
		if run[state] || leaving[state] {
			continue
		}

		run[state] = true
		values, after := r.runState(state, states)
		properties = append(properties, values...)
		queue = append(queue, after...)
	}

	return properties
}

// stateKeys are the keys of a state block that describe the state itself and
// are no end values.
var stateKeys = []string{
	lang.GUIWidgetKey, "duration", animationNextKey, openingCalls, animationFinishKey, "trigger_on_create", "delay", "bezier",
	"loop", "animation", "restart_on_show", "skip_initial_animation", "trigger_when", "start_sound", "end_sound",
	"sstart_sound", "animation_speed", "loopinterval",
}

// runState returns the end values and the opening calls a state gives its
// widget, and the states it leads to.
func (r *resolver) runState(state clause, states map[string]clause) (properties []Property, after []clause) {
	for field := range r.listStateFields(state) {
		key, value, _ := splitField(field)
		switch {
		case key == openingCalls:
			properties = append(properties, r.makeProperty(field))
			after = append(after, r.triggerStates(value, states)...)

		case key == animationNextKey:
			if next, ok := states[value]; ok {
				after = append(after, next)
			}

		case !slices.Contains(stateKeys, key):
			properties = append(properties, r.makeProperty(field))
		}
	}

	return properties, after
}

// triggerStates returns the states a call starts by name on its own widget.
// A call on a child found by FindChild is recorded for that child instead.
func (r *resolver) triggerStates(source string, states map[string]clause) []clause {
	call, ok := jomini.ParseBracketed(source, 0)
	if !ok {
		return nil
	}

	last := len(call.Steps) - 1
	labels := listQuoted(call.Steps[last])
	if call.Steps[last].Text != animationTrigger || len(labels) != 1 {
		return nil
	}

	if last > 0 && call.Steps[last-1].Text == animationFindChild {
		if children := listQuoted(call.Steps[last-1]); len(children) == 1 {
			r.gathered.triggers[children[0]] = slices.Concat(r.gathered.triggers[children[0]], labels)

			return nil
		}
	}

	if state, ok := states[labels[0]]; ok {
		return []clause{state}
	}

	return nil
}
