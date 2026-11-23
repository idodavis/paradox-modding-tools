// steps.go finds where a firing is written in its definition, and puts the
// blocks around it into words.

package graph

import (
	"slices"
	"strconv"
	"strings"

	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/services/lsp"
	"paradox-modding-tools/services/workspace"
)

// Step is one block a firing is written inside.
type Step struct {
	// Key is the block's key as written, such as if or scope:child.dynasty.
	// A weighted branch has the key of the list it is in.
	Key string `json:"key"`

	// Block is what sort of block the firing sits in: branch, weight, list,
	// scope, option or block.
	Block string `json:"block"`

	// Weight is a weighted branch's number, as written.
	Weight string `json:"weight,omitempty"`

	// Line counts from one.
	Line int `json:"line"`

	// Conditions is the step in words as the page lists it: the sort of
	// block, then the conditions it runs on. A plain block has none.
	Conditions []Condition `json:"conditions,omitempty"`
}

// The sorts of block a firing can be written in.
const (
	// blockBranch is one block of a chain of ifs.
	blockBranch = "branch"

	// blockWeight is one weighted branch, such as a random_list's.
	blockWeight = "weight"

	// blockList is a block run for each member of a list, or for one of them.
	blockList = "list"

	// blockScope is a block in another scope, reached through a link.
	blockScope = "scope"

	blockOption = "option"

	// blockOther is any other block.
	blockOther = "block"
)

// makeEdge returns the edge of one firing, with the blocks around it. A
// firing written under a weight, as in an on_action's random_events, has that
// weight as its last step. If the file cannot be walked, the edge has only its
// ends and its place.
func makeEdge(ws *workspace.Session, mods *index.Index, fire index.Fire) Edge {
	edge := Edge{From: toID(mods, fire.Source), To: toID(mods, fire.Target), Site: lsp.LocateSite(ws, mods, fire.Use)}
	for _, site := range fire.Via {
		edge.Via = append(edge.Via, mods.ToReference(site).Name)
	}

	document, err := ws.WalkDocument(edge.Site.Place.Path)
	if err != nil || document.Contexts == nil {
		return edge
	}

	tree := document.Tree
	use := tree.FindStatement(mods.Occurrence(fire.Use).Span.Start)
	firing := findFiring(tree, use, mods.Occurrence(fire.Use).Firing)
	edge.Effect = tree.Text(tree.Statements[firing].Key)
	edge.Delay = findDelay(tree, firing)
	edge.Title = findTitle(ws, mods, tree, firing, lang.TitleKey)
	site, _ := mods.FindDefinition(fire.Use)
	definition := mods.Occurrence(site).Statement
	edge.Steps = listSteps(ws, mods, document, firing, definition)
	if entry := makeStep(ws, mods, document, use, definition); use != firing && entry.Block == blockWeight {
		edge.Steps = append(edge.Steps, entry)
	}

	return edge
}

// findFiring returns the statement that fires. The scan recorded it as a
// depth above the reference's own statement, which is depth 1.
func findFiring(tree *jomini.Tree, use int32, depth uint8) int32 {
	for ; depth > 1; depth-- {
		use = tree.Statements[use].Parent
	}

	return use
}

// makeEdges returns one edge for each place the firings are written.
func makeEdges(ws *workspace.Session, mods *index.Index, fires []index.Fire) []Edge {
	edges := make([]Edge, len(fires))
	for position, fire := range fires {
		edges[position] = makeEdge(ws, mods, fire)
	}

	return dropRepeats(edges, toPlaceKey)
}

// toPlaceKey returns a key that two edges share when they join the same
// nodes at the same line of a file.
func toPlaceKey(edge Edge) [4]string {
	return [4]string{edge.From, edge.To, edge.Site.Place.Path, strconv.Itoa(edge.Site.Line)}
}

// delayKey is a block that sets the delay of the firings beside it.
// delayUnits are the keys that give a delay, in the order looked for.
const delayKey = "delay"

var delayUnits = []string{"days", "weeks", "months", "years"}

// findDelay returns a firing's delay in words: that of the nearest block
// around it that writes one. It returns "" if none does.
func findDelay(tree *jomini.Tree, statement int32) string {
	for ; statement > 0; statement = tree.Statements[statement].Parent {
		holders := []int32{statement}
		if block, ok := tree.FindChild(statement, delayKey); ok {
			holders = append(holders, block)
		}

		for _, holder := range holders {
			for _, unit := range delayUnits {
				if child, ok := tree.FindChild(holder, unit); ok {
					return describeDelay(tree, child, unit)
				}
			}
		}
	}

	return ""
}

// describeDelay returns a delay in words, such as "1 day" or "10–30 days".
func describeDelay(tree *jomini.Tree, statement int32, unit string) string {
	amount := tree.Unquoted(tree.Statements[statement].Value)
	if tree.Statements[statement].IsBlock() {
		var bounds []string
		for child := range tree.Children(statement) {
			bounds = append(bounds, tree.Unquoted(tree.Statements[child].Value))
		}

		amount = strings.Join(bounds, "–")
	}

	if amount == "1" {
		unit = strings.TrimSuffix(unit, "s")
	}

	return amount + " " + unit
}

// findTitle returns the text a field of a statement names, in words, such
// as a firing's title or an option's name. It returns "" if there is none.
func findTitle(ws *workspace.Session, mods *index.Index, tree *jomini.Tree, statement int32, field string) string {
	position, ok := lang.FindTextKey(tree, statement, field)
	if !ok {
		return ""
	}

	text, _, _ := ws.LookupText(mods, tree.Unquoted(tree.Statements[position].Value))

	return toTitle(ws, mods, text)
}

// listSteps returns the blocks around a firing, from the top of its
// definition down, the definition itself left out. A weighted branch's step
// names its list, so the list's own is skipped.
func listSteps(ws *workspace.Session, mods *index.Index, document lang.Document, firing, definition int32) []Step {
	var steps []Step
	statements := document.Tree.Statements
	for statement := statements[firing].Parent; statement > definition; statement = statements[statement].Parent {
		step := makeStep(ws, mods, document, statement, definition)
		if step.Block == blockWeight {
			statement = statements[statement].Parent
		}

		steps = append(steps, step)
	}

	slices.Reverse(steps)

	return steps
}

// weightModifiers are the blocks that change the weight of the branch they
// are in.
var weightModifiers = []string{"modifier", "opinion_modifier", "compare_modifier", "compatibility_modifier"}

// makeStep returns the step of one block around a firing. Its block is what
// the walk decided the block's key is, and a plain block has no words. A
// weighted branch's step names its list, as "10± in random_list", where the ±
// marks a weight that blocks inside the branch change.
func makeStep(ws *workspace.Session, mods *index.Index, document lang.Document, statement, definition int32) Step {
	tree, context := document.Tree, &document.Contexts[statement]
	current := tree.Statements[statement]
	key := tree.Text(current.Key)
	keyword := lang.Keyword{}
	if context.Keyword != nil {
		keyword = *context.Keyword
	}

	step := Step{Key: key, Block: blockOther, Line: toSourceLine(tree, current.Key)}
	detail := key
	switch {
	case keyword.Class == lang.KeywordClassConditional:
		step.Block = blockBranch

	case keyword.Weighted || context.Field != nil && context.Field.Weighted:
		step.Key, step.Block, step.Weight = tree.Text(tree.Statements[current.Parent].Key), blockWeight, key
		detail = key + " in " + step.Key
		if hasWeightModifier(tree, statement) {
			detail = key + "± in " + step.Key
		}

	case context.Iterates:
		step.Block = blockList

	case context.Category == lang.CategoryLink:
		step.Block = blockScope

	case key == optionKey && current.Parent == definition:
		step.Block, detail = blockOption, findTitle(ws, mods, tree, statement, optionTitleKey)

	default:
		return step
	}

	step.Conditions = describeStep(step, detail, findCondition(ws, mods, tree, statement))

	return step
}

// hasWeightModifier reports whether a branch holds a block that changes its
// weight.
func hasWeightModifier(tree *jomini.Tree, statement int32) bool {
	for child := range tree.Children(statement) {
		if slices.Contains(weightModifiers, strings.ToLower(tree.Text(tree.Statements[child].Key))) {
			return true
		}
	}

	return false
}

// stepPhrases is how the page starts a weight, list or scope step.
var stepPhrases = map[string]string{blockWeight: "weight", blockList: "for", blockScope: "in"}

// elseKey is the block that ends a chain of ifs. The page names it as
// written, and any other branch "if".
const elseKey = "else"

// describeStep returns a step in words as the page lists it: a heading with
// the given detail as its value, then the conditions under "when", or
// "where" for a list. A branch is "if" or "else" with its conditions under
// it.
func describeStep(step Step, detail string, when []Condition) []Condition {
	if step.Block == blockBranch {
		text := "if"
		if step.Key == elseKey {
			text = elseKey
		}

		return []Condition{{Text: text, Conditions: when, Line: step.Line}}
	}

	head := Condition{Text: stepPhrases[step.Block], Value: detail, Line: step.Line}
	if step.Block == blockOption {
		head = Condition{Text: "option chosen", Line: step.Line}
		if detail != "" {
			head.Text = "option “" + detail + "” chosen"
		}
	}

	if len(when) == 0 {
		return []Condition{head}
	}

	text := "when"
	if step.Block == blockList {
		text = "where"
	}

	return []Condition{head, {Text: text, Conditions: when, Line: step.Line}}
}
