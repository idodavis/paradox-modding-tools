// conditions.go says script in words: a block of triggers as conditions, and
// a firing with its delay, its title and the blocks it is written inside.

package atlas

import (
	"fmt"
	"slices"
	"strings"

	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/services/lsp"
	"paradox-modding-tools/services/workspace"
)

// Condition is one statement of a block of triggers in words, with the
// statements of its own block under it.
type Condition struct {
	// Subject is what the condition is about, said first: a key that leads
	// to a scope, or the value of a key such as exists. It is "" otherwise.
	Subject string `json:"subject,omitempty"`

	// Text is the key and its operator in words, such as "has trait".
	Text string `json:"text,omitempty"`

	// Value is the value as written. It is "" for a block, a yes or a no,
	// and a value that became the subject.
	Value string `json:"value,omitempty"`

	// Title is the in-game name of what the value names, "" for none.
	Title string `json:"title,omitempty"`

	// Negated is set where the condition must be false.
	Negated bool `json:"negated,omitempty"`

	// Line counts from one. It is 0 for a heading the answer adds.
	Line int `json:"line"`

	// Conditions are the statements of its block, in the order written.
	Conditions []Condition `json:"conditions,omitempty"`
}

// Step is one block a firing is written inside.
type Step struct {
	// Key is the block's key as written, such as if or scope:child.dynasty.
	// A weighted branch has the key of the list it is in.
	Key string `json:"key"`

	// Block is what sort of block the firing sits in: branch, weight, list,
	// scope, option or block.
	Block string `json:"block"`

	// Weight is a weighted branch's number, as written, with ± where a
	// block inside the branch changes it.
	Weight string `json:"weight,omitempty"`

	// Line counts from one.
	Line int `json:"line"`

	// Conditions is the step in words: the sort of block, then the
	// conditions it runs on. A plain block has none.
	Conditions []Condition `json:"conditions,omitempty"`
}

// The sorts of block a firing can be written in.
const (
	blockBranch = "branch"
	blockWeight = "weight"
	blockList   = "list"
	blockScope  = "scope"
	blockOption = "option"
	blockOther  = "block"
)

// operatorWords says each comparison in a condition's words. An equals sign
// says nothing after a key that is no scope.
var operatorWords = map[string]string{
	"=": "is", "==": "is", "!=": "is not", ">": "more than", ">=": "at least", "<": "less than", "<=": "at most",
	"?=": "is, if it exists",
}

// tooltipKeys are the triggers that show a text of their own in place of the
// conditions inside them.
var tooltipKeys = []string{"custom_tooltip", "custom_description"}

// existsKey is the trigger whose value is what must exist.
const existsKey = "exists"

// optionKey is the key of an event's option, and optionName the key of the
// option's text.
const (
	optionKey  = "option"
	optionName = "name"
)

// delayUnits are the keys a block writes a delay with, each with its word.
var delayUnits = map[string]string{"days": "day", "weeks": "week", "months": "month", "years": "year"}

// delayKey is the block an effect may write its delay inside.
const delayKey = "delay"

// weightChangers are the blocks inside a weighted branch that change its
// weight while the game runs.
var weightChangers = []string{"modifier", "opinion_modifier", "compare_modifier", "compatibility_modifier"}

// walkStatement returns the walked document of a site's file and the
// statement that writes the site. It returns false where the file changed
// since the scan or cannot be walked.
func walkStatement(ws *workspace.Session, mods *index.Index, site index.Site) (lang.Document, int32, bool) {
	_, statement, err := ws.FindStatement(mods, site)
	if err != nil {
		return lang.Document{}, 0, false
	}

	document, err := ws.WalkDocument(mods.File(site).Path)

	return document, statement, err == nil && int(statement) < len(document.Contexts)
}

// describeTrigger returns a definition's own condition in words: its limit
// block, else its trigger block. Where any condition is negated, they are
// listed under "must be true" and "must be false". It returns nil for a
// definition with neither block.
func describeTrigger(ws *workspace.Session, mods *index.Index, definition index.Site) []Condition {
	document, statement, ok := walkStatement(ws, mods, definition)
	if !ok {
		return nil
	}

	conditions := describeLimit(ws, mods, document, statement)
	isNegated := func(condition Condition) bool { return condition.Negated }
	if !slices.ContainsFunc(conditions, isNegated) {
		return conditions
	}

	required := slices.DeleteFunc(slices.Clone(conditions), isNegated)
	refused := slices.DeleteFunc(conditions, func(condition Condition) bool { return !condition.Negated })
	for position := range refused {
		refused[position].Negated = false
	}

	return []Condition{{Text: "must be true", Conditions: required}, {Text: "must be false", Conditions: refused}}
}

// describeLimit returns the conditions a block runs on, in words: those of
// its limit, else of its trigger. It returns nil for a block with neither.
func describeLimit(ws *workspace.Session, mods *index.Index, document lang.Document, block int32) []Condition {
	limit, found := document.Tree.FindChild(block, lang.LimitKey)
	if !found {
		limit, found = document.Tree.FindChild(block, lang.TriggerKey)
	}

	if !found {
		return nil
	}

	return describeConditions(ws, mods, document, limit)
}

// describeConditions returns the statements of a block of triggers in words.
func describeConditions(ws *workspace.Session, mods *index.Index, document lang.Document, block int32) []Condition {
	var conditions []Condition
	for child := range document.Tree.Children(block) {
		conditions = append(conditions, describeCondition(ws, mods, document, child))
	}

	return conditions
}

// describeCondition returns one statement of a block of triggers in words. A
// keyword is said by its phrase, a key that leads to a scope is the subject,
// and any other key is said in words with its operator. A block that wraps
// one condition folds into it: a negating key negates it, and a scope
// becomes its subject.
func describeCondition(ws *workspace.Session, mods *index.Index, document lang.Document, statement int32) Condition {
	tree, context := document.Tree, &document.Contexts[statement]
	current := tree.Statements[statement]
	key, value := tree.Unquoted(current.Key), tree.Unquoted(current.Value)
	condition := Condition{Line: tree.Lines.ToPosition(current.Span.Start).Line + 1}
	if slices.Contains(tooltipKeys, key) {
		return describeTooltip(ws, mods, tree, statement, condition)
	}

	if current.IsBlock() {
		condition.Conditions = describeConditions(ws, mods, document, statement)
	}

	operator := operatorWords[tree.Text(current.Op)]
	isLink := context.Category == lang.CategoryLink || context.Category == lang.CategoryParameter
	switch keyword := context.Keyword; {
	case keyword != nil && keyword.Phrase != "":
		condition.Text = strings.Replace(keyword.Phrase, "%s", toWords(strings.TrimPrefix(key, "any_")), 1)
		if keyword.Negates && len(condition.Conditions) == 1 {
			condition = condition.Conditions[0]
			condition.Negated = !condition.Negated
		}

	case isLink && len(condition.Conditions) == 1 && condition.Conditions[0].Subject == "":
		condition = condition.Conditions[0]
		condition.Subject = key

	case isLink:
		condition.Subject, condition.Value = key, value
		if !current.IsBlock() {
			condition.Text = operator
		}

	case key == existsKey:
		condition.Subject, condition.Text = value, "exists"

	default:
		condition.Text = toWords(key)
		if operator != operatorWords["="] {
			condition.Text += " " + operator
		}

		condition.Negated = value == lang.No
		if !slices.Contains(lang.Bools, value) {
			condition.Value = value
		}
	}

	if condition.Value == value && value != "" {
		condition.Title = findValueTitle(ws, mods, context, current.Value)
	}

	return condition
}

// describeTooltip returns a custom tooltip or description as the text the
// game shows for it. Where no message has its key, it is the trigger in
// words with the key it names.
func describeTooltip(ws *workspace.Session, mods *index.Index, tree *jomini.Tree, statement int32, condition Condition) Condition {
	current := tree.Statements[statement]
	textKey := tree.Unquoted(current.Value)
	if current.IsBlock() {
		textKey, _ = tree.FindChildValue(statement, lang.LabelKey)
	}

	if text, _, ok := ws.LookupText(mods, textKey); ok {
		condition.Text = ws.FlattenText(mods, text)

		return condition
	}

	condition.Text, condition.Value = toWords(tree.Unquoted(current.Key)), textKey

	return condition
}

// findValueTitle returns the in-game name of the definition a statement's
// value names, as France for title:k_france. It returns "" where the value
// names none the game keeps, or one with no title.
func findValueTitle(ws *workspace.Session, mods *index.Index, context *lang.Context, value jomini.Span) string {
	for _, reference := range context.References {
		winner, ok := mods.FindKept(ws.Schema, reference)
		if ok && value.Contains(reference.Span.Start) && reference.Database != lang.Localization {
			return ws.LookupTitle(mods, winner.Site)
		}
	}

	return ""
}

// toWords returns a key in words: its underscores as spaces.
func toWords(key string) string {
	return strings.ReplaceAll(key, "_", " ")
}

// makeEdge returns the edge of one firing between two drawn nodes: the key
// that writes it, its delay, its title and the blocks around it, outermost
// first. If the file cannot be walked, the edge has only its ends and its
// place.
func makeEdge(ws *workspace.Session, mods *index.Index, from, to string, use index.Site) Edge {
	edge := Edge{From: from, To: to, Site: lsp.MakeSite(ws, mods, use)}
	document, statement, ok := walkStatement(ws, mods, use)
	if !ok {
		return edge
	}

	tree := document.Tree
	firing := statement
	for range max(int(mods.Occurrence(use).Firing), 1) - 1 {
		firing = tree.Statements[firing].Parent
	}

	edge.Effect = tree.Unquoted(tree.Statements[firing].Key)
	if titleKey, found := tree.FindChildValue(firing, lang.TitleKey); found {
		text, _, _ := ws.LookupText(mods, titleKey)
		edge.Title = ws.FlattenText(mods, text)
	}

	// Below the statement that fires only a weight counts, as in an
	// on_action's own list. The block above a weighted branch is its list,
	// which says nothing the branch does not.
	isReached, isList := false, false
	for block := statement; block != 0 && !document.Contexts[block].Defines; block = tree.Statements[block].Parent {
		isReached = isReached || block == firing
		if isReached && edge.Delay == "" {
			edge.Delay = describeDelay(tree, block)
		}

		weighted := isWeighted(&document.Contexts[block])
		isStep := !isList && (weighted || isReached && block != firing)
		if isStep {
			edge.Steps = append(edge.Steps, describeStep(ws, mods, document, block))
		}

		isList = isStep && weighted
	}

	slices.Reverse(edge.Steps)

	return edge
}

// isWeighted reports whether a statement is a weighted branch: its key is a
// number that is its chance among its siblings.
func isWeighted(context *lang.Context) bool {
	return context.Keyword != nil && context.Keyword.Weighted || context.Field != nil && context.Field.Weighted
}

// describeStep returns one block around a firing in words: a branch with its
// conditions, a weight with its list, a list or a scope by its key, or an
// option by its name. Any other block says nothing.
func describeStep(ws *workspace.Session, mods *index.Index, document lang.Document, block int32) Step {
	tree, context := document.Tree, &document.Contexts[block]
	current := tree.Statements[block]
	key := tree.Unquoted(current.Key)
	limit := describeLimit(ws, mods, document, block)
	step := Step{Key: key, Block: blockOther, Line: tree.Lines.ToPosition(current.Span.Start).Line + 1}
	switch {
	case isWeighted(context):
		step.Block, step.Key, step.Weight = blockWeight, tree.Unquoted(tree.Statements[current.Parent].Key), key
		for child := range tree.Children(block) {
			if slices.Contains(weightChangers, tree.Text(tree.Statements[child].Key)) {
				step.Weight = key + "±"
			}
		}

		step.Conditions = []Condition{{Text: "weight", Value: step.Weight + " in " + step.Key}}
		if len(limit) > 0 {
			step.Conditions = append(step.Conditions, Condition{Text: "when", Conditions: limit})
		}

	case context.Keyword != nil && context.Keyword.Class == lang.KeywordClassConditional:
		step.Block, step.Conditions = blockBranch, []Condition{{Text: toWords(key), Conditions: limit}}

	case context.Iterates:
		step.Block, step.Conditions = blockList, []Condition{{Text: "for", Value: key}}
		if len(limit) > 0 {
			step.Conditions = append(step.Conditions, Condition{Text: "where", Conditions: limit})
		}

	case context.Category == lang.CategoryLink:
		step.Block, step.Conditions = blockScope, []Condition{{Text: "in", Value: key}}

	case key == optionKey && document.Contexts[current.Parent].Defines:
		step.Block, step.Conditions = blockOption, []Condition{{Text: "option chosen"}}
		if name, ok := lang.FindTextKey(tree, block, optionName); ok {
			text, _, _ := ws.LookupText(mods, tree.Unquoted(tree.Statements[name].Value))
			step.Conditions[0].Text = fmt.Sprintf("option “%s” chosen", ws.FlattenText(mods, text))
		}
	}

	return step
}

// describeDelay returns the delay a block writes, itself or in its delay
// block, as "1 day" or "10–30 days" for two bounds. It returns "" for a
// block that writes none.
func describeDelay(tree *jomini.Tree, block int32) string {
	holders := []int32{block}
	if delay, ok := tree.FindChild(block, delayKey); ok {
		holders = append(holders, delay)
	}

	for _, holder := range holders {
		for child := range tree.Children(holder) {
			current := tree.Statements[child]
			unit, ok := delayUnits[tree.Text(current.Key)]
			if !ok {
				continue
			}

			bounds := []string{tree.Unquoted(current.Value)}
			if current.IsBlock() {
				bounds = nil
				for bound := range tree.Children(child) {
					bounds = append(bounds, tree.Unquoted(tree.Statements[bound].Value))
				}
			}

			text := strings.Join(bounds, "–")
			if text != "1" {
				unit += "s"
			}

			return text + " " + unit
		}
	}

	return ""
}
