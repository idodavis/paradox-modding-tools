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

// findCondition returns the limit of a block in words, else its trigger. It returns nil if it holds neither.
func findCondition(ws *workspace.Session, mods *index.Index, tree *jomini.Tree, statement int32) []Condition {
	for _, key := range []string{game.LimitKey, game.TriggerKey} {
		if child, ok := tree.FindChild(statement, key); ok && tree.Statements[child].IsBlock() {
			return listConditions(ws, mods, tree, child)
		}
	}
	return nil
}

// listConditions returns the statements of a block of triggers in words, in the order written.
func listConditions(ws *workspace.Session, mods *index.Index, tree *jomini.Tree, block int32) []Condition {
	return lo.Map(slices.Collect(tree.Children(block)), func(child int32, _ int) Condition {
		return makeCondition(ws, mods, tree, child)
	})
}

// makeCondition returns one statement of a block of triggers in words. A value written prefix:name also has the in-game
// name of the definition it names, if the game keeps one. A block holds its statements, and one that only wraps one of
// them folds into it.
func makeCondition(ws *workspace.Session, mods *index.Index, tree *jomini.Tree, statement int32) Condition {
	current := tree.Statements[statement]
	key := tree.Unquoted(current.Key)
	condition := Condition{Value: tree.Unquoted(current.Value), Line: toSourceLine(tree, current.Span)}
	keyword, ok := ws.Game.LookupKeyword(game.ShapeTriggers, "", key)
	if !ok {
		keyword, _ = game.MatchListIterator(strings.ToLower(key))
	}
	switch {
	case keyword.Covers:
		condition.Text, condition.Value = describeCover(ws, mods, tree, statement)
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
	prefix, label, isPrefixed := jomini.SplitPrefixed(condition.Value)
	if database := ws.Schema.Prefixes[prefix]; isPrefixed && database != "" {
		if winner, ok := mods.FindKept(ws.Schema, schema.Reference{Database: database, Name: label}); ok {
			condition.Title = toTitle(ws, mods, ws.LookupTitle(mods, winner.Site))
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

// foldCondition folds a block that only wraps one condition into it: a negation makes it negated, and a scope becomes
// its subject.
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

// describeCover returns the text a statement shows in place of the conditions it holds. If no message has the key it
// names, it returns the statement's own key in words and that key.
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

// describeOperator returns the words an operator reads after a key. An = reads nothing where the key's words already
// say what it checks.
func describeOperator(operator string, isBlock, isScope bool) string {
	switch {
	case operator == "=" && (isBlock || !isScope):
		return ""
	case operator == "?=" && isBlock:
		return "if it exists"
	}
	return game.OperatorSays[operator]
}
