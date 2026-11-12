// conditions.go puts a block of triggers into words.

package graph

import (
	"slices"
	"strings"

	"github.com/samber/lo"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/schema"
	"paradox-modding-tools/services/workspace"
)

// A Condition is one statement of a block of triggers in words, with the
// statements of its block under it.
type Condition struct {
	Subject    string      `json:"subject,omitempty"`    // what it is about, as written, read first: a key that names a scope, or the value of a keyword that reads its value first; "" for any other
	Text       string      `json:"text,omitempty"`       // the key and its operator in words, such as "has trait"
	Value      string      `json:"value,omitempty"`      // the value as written; "" for a block, a yes, a no or a subject
	Title      string      `json:"title,omitempty"`      // the in-game name of what its value names, or its subject where that is its value; "" for none
	Negated    bool        `json:"negated,omitempty"`    // it must be false
	Line       int         `json:"line"`                 // counted from one
	Conditions []Condition `json:"conditions,omitempty"` // the statements of its block, in the order written
}

// A phrase is how a condition reads one key in English.
type phrase struct {
	// Says is the key in words, with a * for the rest of a prefix key. It is
	// "" for the key as written.
	Says string

	// ValueFirst is whether its value is read first, as in "X exists".
	// Negates is whether it is true when what is inside it is false. Covers
	// is whether its own text is shown in place of the conditions inside it.
	ValueFirst, Negates, Covers bool
}

// notAll is how a condition reads a keyword that is true unless everything
// inside it is true.
const notAll = "not all of"

// phrases holds how a condition reads each key that does not read as its own
// words, by the key in lower case.
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

// anyPrefix is how the key of a trigger over a list starts, and anyPhrase how
// a condition reads such a key.
const anyPrefix = "any_"

var anyPhrase = phrase{Says: "any * where"}

// operatorSays is how a condition reads each operator between a key and a
// value.
var operatorSays = map[string]string{
	"=": "is", "==": "is", "!=": "is not", ">": "more than", ">=": "at least", "<": "less than", "<=": "at most",
	"?=": "is, if it exists",
}

// findPhrase returns how a condition reads a key, and the rest of the key
// where it opens with the prefix of a trigger over a list.
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

// findCondition returns the limit of a block in words, else its trigger. It
// returns nil if it holds neither.
func findCondition(ws *workspace.Session, mods *index.Index, tree *jomini.Tree, statement int32) []Condition {
	for _, key := range []string{game.LimitKey, game.TriggerKey} {
		if child, ok := tree.FindChild(statement, key); ok && tree.Statements[child].IsBlock() {
			return listConditions(ws, mods, tree, child)
		}
	}
	return nil
}

// listConditions returns the statements of a block of triggers in words, in the
// order written.
func listConditions(ws *workspace.Session, mods *index.Index, tree *jomini.Tree, block int32) []Condition {
	return lo.Map(slices.Collect(tree.Children(block)), func(child int32, _ int) Condition {
		return makeCondition(ws, mods, tree, child)
	})
}

// makeCondition returns one statement of a block of triggers in words. A
// value of one prefixed link, as title:k_france, also has the in-game name of
// the definition it names, if the game keeps one. A block holds its
// statements, and one that only wraps one of them folds into it.
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
		if winner, ok := mods.FindKept(ws.Schema, schema.Reference{Database: database, Name: label}); ok {
			condition.Title = toTitle(ws, mods, ws.LookupDefinitionText(mods, winner.Site, game.TitleKey))
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

// foldCondition folds a block that only wraps one condition into it: a negation
// makes it negated, and a scope becomes its subject.
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

// describeCover returns the text a statement shows in place of the conditions
// it holds. If no message has the key it names, it returns the statement's own
// key in words and that key.
func describeCover(ws *workspace.Session, mods *index.Index, tree *jomini.Tree, statement int32) (string, string) {
	key := tree.Unquoted(tree.Statements[statement].Value)
	if label, ok := tree.FindChildValue(statement, game.LabelKey); ok {
		key = label
	}
	text, _, ok := ws.LookupText(mods, key)
	if !ok {
		return toWords(tree.Text(tree.Statements[statement].Key)), key
	}
	return toTitle(ws, mods, text), ""
}

// toWords returns a key as words, each underscore a space.
func toWords(key string) string {
	return strings.ReplaceAll(key, "_", " ")
}

// describeOperator returns the words an operator reads after a key. An = reads
// nothing where the key's words already say what it checks.
func describeOperator(operator string, isBlock, isScope bool) string {
	switch {
	case operator == "=" && (isBlock || !isScope):
		return ""
	case operator == "?=" && isBlock:
		return "if it exists"
	}
	return operatorSays[operator]
}
