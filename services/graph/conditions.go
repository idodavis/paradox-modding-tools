// conditions.go puts a block of triggers into words.

package graph

import (
	"strings"

	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/lang"
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

	// Line counts from one. It is 0 for a heading the page adds.
	Line int `json:"line"`

	// Conditions are the statements of its block, in the order written.
	Conditions []Condition `json:"conditions,omitempty"`
}

// phrase is how a condition says one key in English.
type phrase struct {
	// Says is the key in words, with a * for the rest of a key that starts
	// with a list's word. It is "" for a key said in its own words.
	Says string

	// ValueFirst says the value first, as in "X exists". Negates is set for
	// a key that holds when what is inside it does not. Covers shows the
	// key's own text in place of the conditions inside it.
	ValueFirst, Negates, Covers bool
}

// notAll says a key that fails only when everything inside it holds.
const notAll = "not all of"

// phrases holds the words of each key that has its own, by the key in lower
// case.
var phrases = map[string]phrase{
	"and": {Says: "all of"}, "or": {Says: "any of"},
	"not": {Says: notAll, Negates: true}, "nor": {Says: "none of", Negates: true},
	"nand": {Says: notAll, Negates: true}, "all_false": {Says: "none of", Negates: true},
	"any_false":  {Says: notAll, Negates: true},
	"trigger_if": {Says: "if"}, "trigger_else_if": {Says: "else if"}, "trigger_else": {Says: "else"},
	"limit": {Says: "when"}, "calc_true_if": {Says: "at least the amount of"},
	"custom_tooltip": {Covers: true}, "custom_description": {Covers: true},
	"exists": {ValueFirst: true},
}

// anyPrefix starts the key of a trigger over a list, such as any_child, and
// anyPhrase says such a key.
const anyPrefix = "any_"

var anyPhrase = phrase{Says: "any * where"}

// operatorSays holds the words of each operator between a key and a value.
var operatorSays = map[string]string{
	"=": "is", "==": "is", "!=": "is not", ">": "more than", ">=": "at least", "<": "less than", "<=": "at most",
	"?=": "is, if it exists",
}

// findPhrase returns how a condition says a key. For a trigger over a list
// it also returns the key without its any_.
func findPhrase(key string) (phrase, string) {
	lower := strings.ToLower(key)
	if entry, ok := phrases[lower]; ok {
		return entry, ""
	}

	if rest, ok := strings.CutPrefix(lower, anyPrefix); ok {
		return anyPhrase, rest
	}

	return phrase{}, ""
}

// findCondition returns a block's limit in words, else its trigger, as the
// page lists it. It returns nil if the block holds neither.
func findCondition(ws *workspace.Session, mods *index.Index, tree *jomini.Tree, statement int32) []Condition {
	for _, key := range []string{lang.LimitKey, lang.TriggerKey} {
		if child, ok := tree.FindChild(statement, key); ok && tree.Statements[child].IsBlock() {
			return groupConditions(listConditions(ws, mods, tree, child))
		}
	}

	return nil
}

// groupConditions puts a trigger's negated conditions under "must be
// false", no longer marked negated, and the rest under "must be true". With
// none negated, the conditions are returned as they are.
func groupConditions(conditions []Condition) []Condition {
	var holding, failing []Condition
	for _, condition := range conditions {
		if condition.Negated {
			condition.Negated = false
			failing = append(failing, condition)
		} else {
			holding = append(holding, condition)
		}
	}

	if len(failing) == 0 {
		return conditions
	}

	grouped := []Condition{}
	if len(holding) > 0 {
		grouped = append(grouped, Condition{Text: "must be true", Conditions: holding})
	}

	return append(grouped, Condition{Text: "must be false", Conditions: failing})
}

func listConditions(ws *workspace.Session, mods *index.Index, tree *jomini.Tree, block int32) []Condition {
	conditions := []Condition{}
	for child := range tree.Children(block) {
		conditions = append(conditions, makeCondition(ws, mods, tree, child))
	}

	return conditions
}

// makeCondition returns one statement of a block of triggers in words. A
// value such as title:k_france also gets the in-game name of what it names,
// and a block that only wraps one condition is folded into it.
func makeCondition(ws *workspace.Session, mods *index.Index, tree *jomini.Tree, statement int32) Condition {
	current := tree.Statements[statement]
	key := tree.Unquoted(current.Key)
	condition := Condition{Value: tree.Unquoted(current.Value), Line: toSourceLine(tree, current.Span)}
	keyword, rest := findPhrase(key)
	switch {
	case keyword.Covers:
		condition.Text, condition.Value = describeCover(ws, mods, tree, statement)

		return condition

	case keyword.Says != "":
		condition.Text = strings.Replace(keyword.Says, "*", toWords(rest), 1)

	case ws.Schema.IsLink(key) || current.KeyClass == jomini.ClassParameter:
		condition.Subject = key

	default:
		condition.Text = toWords(key)
	}

	operator := describeOperator(tree.Text(current.Op), current.IsBlock(), condition.Subject != "")
	condition.Text = strings.TrimSpace(condition.Text + " " + operator)
	if condition.Subject == "" && operator == "" {
		switch condition.Value {
		case "yes":
			condition.Value = ""
		case "no":
			condition.Negated, condition.Value = true, ""
		}
	}

	links := jomini.Links(condition.Value, 0)
	link := links[0]
	prefix, label := condition.Value[link.Prefix.Start:link.Prefix.End], condition.Value[link.Name.Start:link.Name.End]
	if database, _ := ws.Schema.LookupPrefix(prefix); len(links) == 1 && !link.Prefix.IsEmpty() && database != "" {
		if winner, ok := mods.FindKept(ws.Schema, lang.Reference{Database: database, Name: label}); ok {
			condition.Title = toTitle(ws, mods, ws.LookupDefinitionText(mods, winner.Site, lang.TitleKey))
		}
	}

	if keyword.ValueFirst {
		condition.Subject, condition.Value = condition.Value, ""
	}

	if current.IsBlock() {
		condition.Conditions = listConditions(ws, mods, tree, statement)
	}

	return foldCondition(condition, keyword)
}

// foldCondition folds a block that wraps one condition into it: a negating
// key negates the condition, and a key that leads to a scope becomes its
// subject.
func foldCondition(condition Condition, keyword phrase) Condition {
	if len(condition.Conditions) != 1 {
		return condition
	}

	inner := condition.Conditions[0]
	switch {
	case keyword.Negates:
		inner.Negated = !inner.Negated

		return inner

	case condition.Subject != "" && condition.Text == "" && inner.Subject == "" && inner.Conditions == nil:
		inner.Subject = condition.Subject

		return inner
	}

	return condition
}

// describeCover returns the text a statement such as custom_tooltip shows in
// place of its conditions. If no message has its key, it returns the
// statement's key in words, with the key it names as the value.
func describeCover(ws *workspace.Session, mods *index.Index, tree *jomini.Tree, statement int32) (string, string) {
	key := tree.Unquoted(tree.Statements[statement].Value)
	if label, ok := tree.FindChildValue(statement, lang.LabelKey); ok {
		key = label
	}

	text, _, ok := ws.LookupText(mods, key)
	if !ok {
		return toWords(tree.Text(tree.Statements[statement].Key)), key
	}

	return toTitle(ws, mods, text), ""
}

func toWords(key string) string {
	return strings.ReplaceAll(key, "_", " ")
}

// describeOperator returns the words an operator is said with after a key.
// An = says nothing where the key's words already say what is checked.
func describeOperator(operator string, isBlock, isScope bool) string {
	switch {
	case operator == "=" && (isBlock || !isScope):
		return ""
	case operator == "?=" && isBlock:
		return "if it exists"
	}

	return operatorSays[operator]
}
