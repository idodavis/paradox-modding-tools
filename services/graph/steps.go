// steps.go finds where a firing sits in the definition that writes it: the blocks around it, its delay, and the effect
// that writes it.

package graph

import (
	"slices"
	"strings"

	"github.com/samber/lo"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/services/lsp"
	"paradox-modding-tools/services/workspace"
)

// A Step is one block a firing is written inside, as the page shows it.
type Step struct {
	Key       string      `json:"key"`                 // as written, such as if, random_list, option or scope:child.dynasty
	Kind      string      `json:"kind"`                // branch, weight, list, scope, option or block
	Weight    string      `json:"weight,omitempty"`    // a weighted branch's number as written
	Modifiers int         `json:"modifiers,omitempty"` // the blocks in a weighted branch that change its weight
	When      []Condition `json:"when,omitempty"`      // its limit, else its trigger, in words
	Title     string      `json:"title,omitempty"`     // an option's name in words; "" for the other kinds
	Line      int         `json:"line"`                // counted from one
}

// The kinds of block a firing can sit in.
const (
	kindBranch = "branch" // one block of a chain of ifs
	kindWeight = "weight" // one weighted branch, such as a random list's
	kindList   = "list"   // the items of a list, each in turn or one of them
	kindScope  = "scope"  // another scope, reached by a link
	kindOption = "option" // one of an event's options
	kindBlock  = "block"  // any other block
)

// makeEdge returns the edge of one firing: where it is written, with the range of the name it fires and the text of
// its line, the blocks it sits in, its delay, and the effect that writes it with the title that effect gives it. A
// firing written as one weighted entry of a list that picks one, such as an on_action's random_events, has that entry
// as its last step. An edge whose file cannot be read has only its ends, its place and its line.
func makeEdge(ws *workspace.Session, mods *index.Index, fire index.Fire) Edge {
	edge := Edge{
		From: toID(mods, fire.Source), To: toID(mods, fire.Target),
		Via:  lo.Map(fire.Via, func(site index.Site, _ int) string { return mods.ToReference(site).Name }),
		Site: lsp.LocateSite(ws, mods, fire.Use),
	}
	tree, err := ws.LoadTree(edge.Site.Place.Path)
	if err != nil {
		return edge
	}
	use := tree.FindStatement(mods.Occurrence(fire.Use).Span.Start)
	firing := findFiring(ws, tree, use)
	edge.Effect = tree.Text(tree.Statements[firing].Key)
	edge.Delay = findDelay(tree, firing)
	edge.Title = findTitle(ws, mods, tree, firing, game.TitleKey)
	site, _ := mods.FindDefinition(fire.Use)
	definition := mods.Occurrence(site).Statement
	edge.Steps = listSteps(ws, mods, tree, firing, definition)
	if entry := makeStep(ws, mods, tree, use, definition); use != firing && entry.Kind == kindWeight {
		edge.Steps = append(edge.Steps, entry)
	}
	return edge
}

// makeEdges returns the edges of some firings, one for each place a firing is written.
func makeEdges(ws *workspace.Session, mods *index.Index, fires []index.Fire) []Edge {
	type firing struct {
		source, target, path string
		line                 int
	}
	edges := lo.Map(fires, func(fire index.Fire, _ int) Edge { return makeEdge(ws, mods, fire) })
	return lo.UniqBy(edges, func(edge Edge) firing { return firing{edge.From, edge.To, edge.Site.Place.Path, edge.Site.Line} })
}

// findFiring returns the statement that writes a firing, from the statement its reference sits in. It is that
// statement, or the block that holds it where the reference is a value in a list, a numbered branch or a field of an
// effect, such as trigger_event's id or send_interface_message's type.
func findFiring(ws *workspace.Session, tree *jomini.Tree, statement int32) int32 {
	for {
		current := tree.Statements[statement]
		key := tree.Text(current.Key)
		isField := isEffect(ws, tree.Text(tree.Statements[current.Parent].Key)) && !isEffect(ws, key)
		isHeld := current.IsListed() || jomini.IsNumberKey(key) || isField
		if current.Parent == 0 || !isHeld {
			return statement
		}
		statement = current.Parent
	}
}

// isEffect reports whether a key is an effect the dumps declare.
func isEffect(ws *workspace.Session, key string) bool {
	command, ok := ws.Schema.FindCommand(key)
	return ok && slices.Contains(command.Declared, game.DeclaredEffect)
}

// findDelay returns the delay written for a firing, in words. The delay is in the first block, going up from the
// firing's statement, that writes one or holds a delay block that does. It returns "" where none is written.
func findDelay(tree *jomini.Tree, statement int32) string {
	for ; statement > 0; statement = tree.Statements[statement].Parent {
		holders := []int32{statement}
		if block, ok := tree.FindChild(statement, game.EventDelay.Block); ok {
			holders = append(holders, block)
		}
		for _, holder := range holders {
			for _, unit := range game.EventDelay.Units {
				if child, ok := tree.FindChild(holder, unit); ok {
					return describeDelay(tree, child, unit)
				}
			}
		}
	}
	return ""
}

// describeDelay returns a delay in words, from the statement that writes it and the key of its unit: its number, or
// the bounds of its range joined by a dash, then its unit, as 7 days or 10–30 days; one of a unit is 1 day.
func describeDelay(tree *jomini.Tree, statement int32, unit string) string {
	amount := tree.Unquoted(tree.Statements[statement].Value)
	if tree.Statements[statement].IsBlock() {
		amount = strings.Join(lo.Map(slices.Collect(tree.Children(statement)), func(child int32, _ int) string {
			return tree.Unquoted(tree.Statements[child].Value)
		}), "–")
	}
	return amount + " " + lo.Ternary(amount == "1", strings.TrimSuffix(unit, "s"), unit)
}

// findTitle returns the text that a field of a statement names, in words, from the given index: a firing's title or
// an option's name. It returns "" where the statement writes no such field or no message has the key it names.
func findTitle(ws *workspace.Session, mods *index.Index, tree *jomini.Tree, statement int32, field string) string {
	key, ok := workspace.FindTextKey(tree, statement, field)
	if !ok {
		return ""
	}
	text, _, _ := ws.LookupText(mods, key)
	return toTitle(ws, mods, text)
}

// listSteps returns the blocks a firing's statement sits in, from the top of the definition down. The definition and
// the firing's own statement are left out, and a weighted keyword is no step of its own: its branch stands for it.
func listSteps(ws *workspace.Session, mods *index.Index, tree *jomini.Tree, firing, definition int32) []Step {
	var steps []Step
	statements := tree.Statements
	for statement := statements[firing].Parent; statement > definition; statement = statements[statement].Parent {
		step := makeStep(ws, mods, tree, statement, definition)
		if step.Kind == kindWeight {
			statement = statements[statement].Parent
		}
		steps = append(steps, step)
	}
	slices.Reverse(steps)
	return steps
}

// makeStep returns the step of one block a firing sits in, in the given definition. Its kind is the first that fits,
// in this order: a branch of a chain of ifs, a weighted branch, a list the dumps declare, a link to another scope, one
// of the definition's options, and any other block. Its condition is its limit, else its trigger, in words. A weighted
// branch counts the blocks in it that change its weight.
func makeStep(ws *workspace.Session, mods *index.Index, tree *jomini.Tree, statement, definition int32) Step {
	current := tree.Statements[statement]
	key, holder := tree.Text(current.Key), tree.Text(tree.Statements[current.Parent].Key)
	keyword, isKeyword := ws.Game.LookupKeyword("", holder, key)
	_, iterates := game.MatchListIterator(key)
	_, isCommand := ws.Schema.FindCommand(key)
	step := Step{
		Key: key, Kind: kindBlock, When: findCondition(ws, mods, tree, statement), Line: toSourceLine(tree, current.Key),
	}
	switch {
	case keyword.Class == "conditional":
		step.Kind = kindBranch
	case keyword.Weighted:
		step.Key, step.Kind, step.Weight = holder, kindWeight, key
		step.Modifiers = lo.CountBy(slices.Collect(tree.Children(statement)), func(child int32) bool {
			row, _ := ws.Game.LookupKeyword(game.ShapeValue, "", tree.Text(tree.Statements[child].Key))
			return row.Modifies
		})
	case iterates && !isKeyword && isCommand:
		step.Kind = kindList
	case ws.Schema.IsLink(key):
		step.Kind = kindScope
	case key == game.OptionKey && current.Parent == definition:
		step.Kind, step.Title = kindOption, findTitle(ws, mods, tree, statement, game.OptionTitleKey)
	}
	return step
}
