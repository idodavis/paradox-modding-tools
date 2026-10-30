// conditions.go puts a block of triggers into words: each statement as one line, read by the language's keywords and
// its operators, with the statements of its block under it.

package graph

import (
	"slices"
	"strings"

	"github.com/samber/lo"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/schema"
	"paradox-modding-tools/services/workspace"
)

// A Condition is one statement of a block of triggers in words, with the statements of its block under it.
type Condition struct {
	// Subject is what it is about, as written, read first: a key that names a scope, or the value of a keyword that
	// reads its value first, as in X exists; "" for any other.
	Subject string `json:"subject,omitempty"`
	Text    string `json:"text,omitempty"`  // the key and its operator in words, such as "has trait"
	Value   string `json:"value,omitempty"` // the value as written; "" for a block, a yes, a no or a subject
	// Title is the in-game name of what its value names, or its subject where that is its value; "" for none.
	Title      string      `json:"title,omitempty"`
	Negated    bool        `json:"negated,omitempty"`    // it must be false
	Line       int         `json:"line"`                 // counted from one
	Conditions []Condition `json:"conditions,omitempty"` // the statements of its block, in the order written
}

// comparisonWords are the words each operator reads between a key and a value.
var comparisonWords = map[string]string{
	"=": "is", "==": "is", "!=": "is not", ">": "more than", ">=": "at least", "<": "less than", "<=": "at most",
	"?=": "is, if it exists",
}

// findCondition returns the condition of a block in words: its limit, else its trigger. It returns nil if it holds
// neither as a block.
func findCondition(ws *workspace.Session, tree *jomini.Tree, statement int32) []Condition {
	for _, key := range []string{limitKey, triggerKey} {
		if child, ok := tree.FindChild(statement, key); ok && tree.Statements[child].IsBlock() {
			return listConditions(ws, tree, child)
		}
	}
	return nil
}

// listConditions returns the statements of a block of triggers in words, in the order written.
func listConditions(ws *workspace.Session, tree *jomini.Tree, block int32) []Condition {
	return lo.Map(slices.Collect(tree.Children(block)), func(child int32, _ int) Condition {
		return makeCondition(ws, tree, child)
	})
}

// makeCondition returns one statement of a block of triggers in words. A keyword of the language reads its own words,
// and one whose text is shown in place of what it holds reads that text alone. A key that names a scope, or a parameter
// that stands for one, stays as written. Any other key is its words, underscores as spaces: a yes after it is dropped
// and a no makes it negated.
// A value stays as written, with the in-game name of what it names, read first where its keyword says so. A block
// holds its statements, and one that only wraps one of them folds into it.
func makeCondition(ws *workspace.Session, tree *jomini.Tree, statement int32) Condition {
	current := tree.Statements[statement]
	key := tree.Unquoted(current.Key)
	condition := Condition{Value: tree.Unquoted(current.Value), Line: toSourceLine(tree, current.Span)}
	keyword, ok := game.LookupKeyword(game.ShapeTriggers, "", strings.ToLower(key))
	if !ok {
		keyword, _ = game.MatchListIterator(strings.ToLower(key))
	}
	switch {
	case keyword.Covers:
		condition.Text, condition.Value = describeCover(ws, tree, statement)
		return condition
	case keyword.Says != "":
		rest := strings.TrimPrefix(strings.ToLower(key), strings.TrimSuffix(keyword.Key, "*"))
		condition.Text = strings.Replace(keyword.Says, "*", toWords(rest), 1)
	case ws.Schema.IsLink(key) || jomini.IsParameter(key):
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
	condition.Title = findName(ws, condition.Value)
	if keyword.ValueFirst {
		condition.Subject, condition.Value = condition.Value, ""
	}
	if current.IsBlock() {
		condition.Conditions = listConditions(ws, tree, statement)
	}
	return foldCondition(condition, keyword)
}

// findName returns the in-game name of the definition a value written prefix:name names, as its node shows it: the
// title of the copy the game keeps. It returns "" for a value with no prefix that names a database, and for one that
// names nothing kept or nothing with a title.
func findName(ws *workspace.Session, value string) string {
	prefix, label, ok := jomini.SplitPrefixed(value)
	database := ws.Schema.Prefixes[prefix]
	if !ok || database == "" {
		return ""
	}
	mods := ws.Mods.Load()
	winner, ok := mods.FindKept(ws.Schema, schema.Reference{Database: database, Name: label})
	if !ok {
		return ""
	}
	return toTitle(ws, mods, ws.LookupTitle(mods, winner.Site))
}

// foldCondition folds a block that only wraps one condition into it. A negation around one condition is that
// condition negated, and a scope around one plain condition is that condition with the scope as its subject. Any
// other condition is returned as it is.
func foldCondition(condition Condition, keyword game.Keyword) Condition {
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

// describeCover returns the text a statement shows in place of the conditions it holds, as a player reads it: the
// message its value or its text field names, from the workspace's index of the moment. Where no message has that key,
// it returns the statement's own key in words and the key it names.
func describeCover(ws *workspace.Session, tree *jomini.Tree, statement int32) (string, string) {
	key := tree.Unquoted(tree.Statements[statement].Value)
	if label, ok := tree.FindChildValue(statement, game.LabelKey); ok {
		key = label
	}
	mods := ws.Mods.Load()
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

// describeOperator returns the words an operator reads after a key. An = reads nothing before a block or after a key
// that names no scope, where the key's words say what it checks; a ?= before a block reads "if it exists". Any other
// reads its comparison's words.
func describeOperator(operator string, isBlock, isScope bool) string {
	switch {
	case operator == "=" && (isBlock || !isScope):
		return ""
	case operator == "?=" && isBlock:
		return "if it exists"
	}
	return comparisonWords[operator]
}
