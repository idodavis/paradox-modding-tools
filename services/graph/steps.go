// steps.go finds where a firing sits in the definition that writes it.

package graph

import (
	"slices"
	"strings"

	"github.com/samber/lo"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/schema"
	"paradox-modding-tools/services/lsp"
	"paradox-modding-tools/services/workspace"
)

// A Step is one block a firing is written inside, as the page shows it.
type Step struct {
	Key       string      `json:"key"`                 // as written, such as if, random_list or scope:child.dynasty
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

// makeEdge returns the edge of one firing. A firing written as a weighted entry of a list that picks one, such as an
// on_action's random_events, has that entry as its last step. If the file cannot be read, the edge has only its ends
// and its place.
func makeEdge(ws *workspace.Session, mods *index.Index, fire index.Fire) Edge {
	edge := Edge{
		From: toID(mods, fire.Source), To: toID(mods, fire.Target),
		Via:  lo.Map(fire.Via, func(site index.Site, _ int) string { return mods.ToReference(site).Name }),
		Site: lsp.LocateSite(ws, mods, fire.Use),
	}
	// a step is what the walk decided its block is, so the file is walked
	document, err := ws.WalkDocument(edge.Site.Place.Path)
	if err != nil || document.Contexts == nil {
		return edge
	}
	tree := document.Tree
	use := tree.FindStatement(mods.Occurrence(fire.Use).Span.Start)
	firing := findFiring(document, ws.Schema.Effects, use)
	edge.Effect = tree.Text(tree.Statements[firing].Key)
	edge.Delay = findDelay(tree, firing)
	edge.Title = findTitle(ws, mods, tree, firing, game.TitleKey)
	site, _ := mods.FindDefinition(fire.Use)
	definition := mods.Occurrence(site).Statement
	edge.Steps = listSteps(ws, mods, document, firing, definition)
	if entry := makeStep(ws, mods, document, use, definition); use != firing && entry.Kind == kindWeight {
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

// findFiring returns the statement that writes a firing, given the statement its reference sits in. That is the block
// around it where the reference is a list value, a numbered branch or a field of an effect, such as trigger_event's id.
func findFiring(document schema.Document, effects string, statement int32) int32 {
	for {
		current := document.Tree.Statements[statement]
		isField := isEffect(&document.Contexts[current.Parent], effects) && !isEffect(&document.Contexts[statement], effects)
		isHeld := current.IsListed() || current.KeyClass.IsNumberKey() || isField
		if current.Parent == 0 || !isHeld {
			return statement
		}

		statement = current.Parent
	}
}

// isEffect reports whether the walk decided a statement's key is an effect:
// one the dumps declare, or a call of a scripted effect, whose database is
// given. So a firing written as a scripted effect's argument, as in
// my_effect = { EVENT = x.1 }, is the call and not the argument.
func isEffect(context *schema.Context, effects string) bool {
	return context.Command != nil && context.Command.Database == game.DumpEffects ||
		context.Callee != nil && context.Callee.Database == effects
}

// findDelay returns the delay written for a firing, in words, or "" if none is written. It is the delay of the nearest
// block around the firing that writes one.
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

// describeDelay returns a delay in words, such as 7 days, 1 day or 10–30 days, from the statement that writes it and
// the key of its unit.
func describeDelay(tree *jomini.Tree, statement int32, unit string) string {
	amount := tree.Unquoted(tree.Statements[statement].Value)
	if tree.Statements[statement].IsBlock() {
		amount = strings.Join(lo.Map(slices.Collect(tree.Children(statement)), func(child int32, _ int) string {
			return tree.Unquoted(tree.Statements[child].Value)
		}), "–")
	}
	return amount + " " + lo.Ternary(amount == "1", strings.TrimSuffix(unit, "s"), unit)
}

// findTitle returns the text a field of a statement names, in words: a firing's title or an option's name. It returns
// "" if the statement writes no such field or no message has its key.
func findTitle(ws *workspace.Session, mods *index.Index, tree *jomini.Tree, statement int32, field string) string {
	position, ok := schema.FindTextKey(tree, statement, field)
	if !ok {
		return ""
	}
	text, _, _ := ws.LookupText(mods, tree.Unquoted(tree.Statements[position].Value))
	return toTitle(ws, mods, text)
}

// listSteps returns the blocks a firing's statement sits in, from the top of the definition down, leaving out the
// definition and the firing itself.
func listSteps(ws *workspace.Session, mods *index.Index, document schema.Document, firing, definition int32) []Step {
	var steps []Step
	statements := document.Tree.Statements
	for statement := statements[firing].Parent; statement > definition; statement = statements[statement].Parent {
		step := makeStep(ws, mods, document, statement, definition)
		if step.Kind == kindWeight {
			statement = statements[statement].Parent
		}
		steps = append(steps, step)
	}
	slices.Reverse(steps)
	return steps
}

// weightModifiers are the keys of the blocks that change the weight of the
// branch they are written in.
var weightModifiers = []string{"modifier", "opinion_modifier", "compare_modifier", "compatibility_modifier"}

// makeStep returns the step of one block a firing sits in, in the given
// definition. Whether the block is a branch, a weighted branch or a list is
// what the walk decided its key is.
func makeStep(ws *workspace.Session, mods *index.Index, document schema.Document, statement, definition int32) Step {
	tree, context := document.Tree, &document.Contexts[statement]
	current := tree.Statements[statement]
	key, holder := tree.Text(current.Key), tree.Text(tree.Statements[current.Parent].Key)
	keyword := game.Keyword{}
	if context.Keyword != nil {
		keyword = *context.Keyword
	}
	step := Step{
		Key: key, Kind: kindBlock, When: findCondition(ws, mods, tree, statement), Line: toSourceLine(tree, current.Key),
	}
	switch {
	case keyword.Class == game.KeywordClassConditional:
		step.Kind = kindBranch
	case keyword.Weighted || context.Field != nil && context.Field.Weighted:
		step.Key, step.Kind, step.Weight = holder, kindWeight, key
		step.Modifiers = lo.CountBy(slices.Collect(tree.Children(statement)), func(child int32) bool {
			return slices.Contains(weightModifiers, strings.ToLower(tree.Text(tree.Statements[child].Key)))
		})
	case context.Iterates:
		step.Kind = kindList
	case context.Category == schema.CategoryLink:
		step.Kind = kindScope
	case key == game.OptionKey && current.Parent == definition:
		step.Kind, step.Title = kindOption, findTitle(ws, mods, tree, statement, game.OptionTitleKey)
	}
	return step
}
