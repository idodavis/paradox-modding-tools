package draw

import "paradox-modding-tools/internal/schema"

// A Group is one set of conditions the toolbar lets the user switch: the data call the conditions read, the
// values it can take, and the value currently picked.
type Group struct {
	ID      string   `json:"id"`
	Label   string   `json:"label"`
	Options []string `json:"options"`
	Picked  string   `json:"picked"`
}

// States describes one way of drawing a window: the groups the toolbar offers with their current picks, and the
// widgets those picks hide.
type States struct {
	Groups []Group `json:"groups"`
	Hidden []int32 `json:"hidden"`
}

// EvaluateStates works out which widgets of a window are hidden under the given picks, where each pick maps a
// group's ID to the value chosen for it. A group with no pick takes its first option. Nothing about the game is
// simulated: a condition that reads a group with no value is unknown and hides nothing.
func (drw *Service) EvaluateStates(path, name string, picks map[string]string) (States, error) {
	panic("stub")
}

// Reveal returns the picks that make a hidden widget visible, or nil if its conditions contradict each other.
func (drw *Service) Reveal(path, name string, widget int32) (map[string]string, error) { panic("stub") }

// evaluate works out the states of an already resolved window under the given picks.
func evaluate(schema *schema.Schema, window Window, picks map[string]string) States { panic("stub") }
