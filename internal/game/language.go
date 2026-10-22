// language.go holds the script language's own rules: the shapes a block can hold and how a block of each shape
// reads its keys, the keys the language gives a fixed shape to, its operators with the sentence a hover card
// shows, and the words that name a scope, a variable or a yes-or-no.

package game

import (
	"slices"
	"strings"

	"paradox-modding-tools/internal/jomini"
)

// A KeyRule says what it means for a key to fit one rule: where its block's fields come from, what decides the
// block's shape, and how the scope changes inside it. Whether the rule fits is the walk's test.
type KeyRule struct {
	Fields string // where the block's fields come from: child, callee, arguments, holder. Read by the walk alone.
	Shape  string // what decides the shape it holds: claim, command, same, arguments. Read by the walk alone.
	Scope  string // how its scope follows: stays, follows, unknown. Read by the walk alone.
}

// A ShapeRule says how a block of one shape reads its keys.
type ShapeRule struct {
	Rules  []string // the KeyRules tried in order; the first that fits wins
	Caller string   // the word a call here resolves by; AnyCall every callable
	Allows []string // the declared commands it holds; inside a trigger, never an effect
}

// A Keyword is a key of the language itself: one it gives a fixed shape to, or one of its operators. Key is a key
// path whose last step is the key as written in lower case and whose earlier steps are the commands that must
// hold it; <number> and <any> match by form, and a trailing * makes it a prefix. The first row that fits a key
// wins.
type Keyword struct {
	Key    string
	Holder string // fits only in a block of this shape; "" any
	Shape  string // what its block holds, a Shapes key; enclosing the holder's; "" what the key's rule gives
	Caller string // the word its calls resolve by; "" the shape's own
	Shows  string // a description marker: which of its texts it shows, first, random, leaf. Read by the walk.
	Class  string // the operator class: logical, conditional, flow, display, saving, math, list; "" none
	Blurb  string // the one line a hover card shows
}

// AnyCall is the Caller of a block that resolves a call in every callable.
const AnyCall = "*"

// AnyKey is the key that matches any key written in a block, such as switch's cases or a widget's blocks.
const AnyKey = "<any>"

// NumberKey is the key that matches a key written as a number or a date, such as random_list's branches.
const NumberKey = "<number>"

// The dumps' words for what declares a command, by the file that declares it. Command.Declared lists them in dump
// order.
const (
	DeclaredEffect  = "effect"       // effects.log
	DeclaredTrigger = "trigger"      // triggers.log
	DeclaredLink    = "event target" // event_targets.log
)

// The databases of the names script saves and a link reads back: saved scopes (scope:x), the variables of a scope
// (var:x), of the effect being run (local_var:x) and of the game (global_var:x).
const (
	SavedScopes     = "scope"
	Variables       = "var"
	LocalVariables  = "local_var"
	GlobalVariables = "global_var"
)

// descBlurb is the sentence shown for a description, which a value block's desc shares.
const descBlurb = "The text shown: a localization key, or a block that chooses one."

// ScopeLinks are the language's links to the scopes a walk holds at any place in a definition: the definition's
// root, the scope before the last step, and the current one. The dumps declare them as links with no target.
var ScopeLinks = struct{ Root, Previous, This string }{"root", "prev", "this"}

// ConceptCalls are the two ways a data call in a localization text names a game concept. A Call step names the
// concept its first quoted argument holds, as in [Concept('faith','Faiths')]. A call of one step with no arguments
// and Format after its bar, in either case, names the concept its step is, as in [faith|E].
var ConceptCalls = struct{ Call, Format string }{"Concept", "E"}

// A DocType is what a term the docs write a field's type with means: the value type, the database a reference
// names, and the shape of the block the field holds.
type DocType struct {
	Value    string // bool, number, date, text, reference, color, block, or unknown for a key or a block
	Database string // the database a reference names; "" unknown
	Shape    string // a Shapes key for a block; "" for none
}

// DocTypes are the words the games' own docs write a type with, as the installs' .info, .md and readme files
// write them, each with what it means.
var DocTypes = map[string]DocType{
	"bool": {Value: "bool"}, "boolean": {Value: "bool"}, "yes/no": {Value: "bool"},
	"int": {Value: "number"}, "integer": {Value: "number"}, "int32": {Value: "number"}, "number": {Value: "number"},
	"float": {Value: "number"}, "decimal": {Value: "number"}, "fixed point": {Value: "number"}, "value": {Value: "number"},
	"script value": {Value: "number"}, "scripted value": {Value: "number"}, "scripted value int32": {Value: "number"},
	"date":   {Value: "date"},
	"string": {Value: "text"}, "text": {Value: "text"}, "name": {Value: "text"}, "identifier": {Value: "text"},
	"localization key": {Value: "reference", Database: Localization}, "loc key": {Value: "reference", Database: Localization},
	"localisation key": {Value: "reference", Database: Localization},
	"database key":     {Value: "reference"}, "key": {Value: "reference"}, "array of database keys": {Value: "reference"},
	"string path": {Value: "reference", Database: File}, "path": {Value: "reference", Database: File},
	"texture": {Value: "reference", Database: File}, "file": {Value: "reference", Database: File},
	"color":   {Value: "color"},
	"trigger": {Value: "block", Shape: "triggers"}, "triggers": {Value: "block", Shape: "triggers"},
	"effect": {Value: "block", Shape: "effects"}, "effects": {Value: "block", Shape: "effects"},
	"dynamic description": {Value: "unknown", Shape: "description"},
	"modifier":            {Value: "block"}, "modifiers": {Value: "block"}, "block": {Value: "block"}, "mtth": {Value: "block"},
	"scripted cost": {Value: "block"}, "cost": {Value: "block"}, "vector 2": {Value: "block"}, "vector2": {Value: "block"},
}

// KeyRules is the table of key rules by name. When the walk finds that a key fits a rule, the row says where the
// block's fields come from, what decides its shape and how the scope follows.
var KeyRules = map[string]KeyRule{
	"known-call":    {Fields: "callee", Shape: "claim", Scope: "stays"},        // the name of a scripted definition vanilla defines
	"declared":      {Fields: "child", Shape: "claim", Scope: "stays"},         // a key the dumps, docs or wiki declare at this place
	"documented":    {Fields: "child", Shape: "claim", Scope: "stays"},         // a key the docs of the definition's database write
	"command":       {Fields: "arguments", Shape: "command", Scope: "follows"}, // a trigger or effect the dumps declare
	"operator":      {Fields: "child", Shape: "arguments", Scope: "stays"},     // a value block's operator (IsOperator)
	"link":          {Fields: "holder", Shape: "same", Scope: "follows"},       // a link the dumps declare, by name or prefix
	"observed":      {Fields: "child", Shape: "claim", Scope: "stays"},         // a key vanilla writes at this place
	"field":         {Fields: "child", Shape: "claim", Scope: "stays"},         // the place's own field, known or not
	"field-or-call": {Fields: "child", Shape: "claim", Scope: "stays"},         // the place's own field, and a call where vanilla writes commands here
	"call":          {Fields: "child", Shape: "claim", Scope: "stays"},         // any other key: a call to a scripted definition
}

// Shapes is the table of shapes a block can hold, each with how it reads its keys. A description block reads its
// keys by the Keywords alone, and "enclosing" in a keyword or a claim means the holder's shape.
var Shapes = map[string]ShapeRule{
	"fields":    {Rules: []string{"field"}},
	"triggers":  {Rules: []string{"known-call", "declared", "command", "observed", "link", "call"}, Caller: callTrigger, Allows: []string{DeclaredTrigger}},
	"effects":   {Rules: []string{"known-call", "declared", "command", "observed", "link", "call"}, Caller: callEffect, Allows: []string{DeclaredEffect}},
	"arguments": {Rules: []string{"known-call", "declared", "command", "observed", "link", "call"}, Caller: AnyCall, Allows: []string{DeclaredEffect, DeclaredTrigger}},
	"value": {
		Rules:  []string{"known-call", "declared", "command", "operator", "observed", "link", "call"},
		Caller: callValue, Allows: []string{DeclaredEffect, DeclaredTrigger},
	},
	"unknown": {
		Rules:  []string{"known-call", "declared", "command", "link", "field-or-call"},
		Caller: AnyCall, Allows: []string{DeclaredEffect, DeclaredTrigger},
	},
	"setup": {
		Rules:  []string{"declared", "documented", "known-call", "command", "observed", "link", "call"},
		Caller: callEffect, Allows: []string{DeclaredEffect},
	},
	"widget":      {Rules: []string{"known-call", "field"}, Caller: "widget"},
	"description": {},
}

// Keywords lists the keys the language gives a fixed shape to (random_list's branches, switch's cases, the list
// iterators, a description's markers, a value block's operators, a widget's blocks), followed by the operators the
// dumps declare as commands, which name no shape and leave it to the key's rule.
var Keywords = []Keyword{
	{Key: "random_list/<number>", Shape: "effects", Caller: AnyCall, Blurb: "One branch of a random list; its number is how likely it is."},
	{Key: "switch/<any>", Shape: "enclosing", Blurb: "One case of a switch: runs when the trigger's value is this."},
	{Key: "every_*", Shape: "effects", Class: "list", Blurb: "Runs the effects inside it on every item of the list named after every_."},
	{Key: "random_*", Shape: "effects", Class: "list", Blurb: "Runs the effects inside it on one random item of the list."},
	{Key: "ordered_*", Shape: "effects", Class: "list", Blurb: "Runs the effects inside it on the items of the list in the order given."},
	{Key: "any_*", Shape: "triggers", Class: "list", Blurb: "True when at least one item of the list passes the triggers inside it."},
	{Holder: "description", Key: "first_valid", Shape: "description", Class: "display", Shows: "first", Blurb: "Shows the first text inside it whose trigger holds."},
	{Holder: "description", Key: "random_valid", Shape: "description", Class: "display", Shows: "random", Blurb: "Shows one of the texts inside it whose trigger holds, at random."},
	{Holder: "description", Key: "triggered_desc", Shape: "description", Class: "display", Shows: "leaf", Blurb: "A text shown only when its trigger holds."},
	{Holder: "description", Key: "trigger", Shape: "triggers", Blurb: "The conditions that must hold for the block around it to count."},
	{Holder: "description", Key: "desc", Shape: "description", Blurb: descBlurb}, // a scalar desc is the text itself
	{Holder: "value", Key: "value", Shape: "value", Class: "math", Blurb: "The number a script value starts from, or is set to."},
	{Holder: "value", Key: "add", Shape: "value", Class: "math", Blurb: "Adds the number or script value given."},
	{Holder: "value", Key: "subtract", Shape: "value", Class: "math", Blurb: "Subtracts the number or script value given."},
	{Holder: "value", Key: "multiply", Shape: "value", Class: "math", Blurb: "Multiplies by the number or script value given."},
	{Holder: "value", Key: "divide", Shape: "value", Class: "math", Blurb: "Divides by the number or script value given."},
	{Holder: "value", Key: "modulo", Shape: "value", Class: "math", Blurb: "Keeps the remainder of dividing by the number given."},
	{Holder: "value", Key: "min", Shape: "value", Class: "math", Blurb: "Keeps the result at least the number given."},
	{Holder: "value", Key: "max", Shape: "value", Class: "math", Blurb: "Keeps the result at most the number given."},
	{Holder: "value", Key: "round", Shape: "value", Class: "math", Blurb: "Rounds the result to the nearest whole number."},
	{Holder: "value", Key: "round_to", Shape: "value", Class: "math", Blurb: "Rounds the result to the nearest multiple of the number given."},
	{Holder: "value", Key: "ceiling", Shape: "value", Class: "math", Blurb: "Rounds the result up to a whole number."},
	{Holder: "value", Key: "floor", Shape: "value", Class: "math", Blurb: "Rounds the result down to a whole number."},
	{Holder: "value", Key: "fixed_range", Shape: "value", Class: "math", Blurb: "Keeps the result between the min and max inside it."},
	{Holder: "value", Key: "integer_range", Shape: "value", Class: "math", Blurb: "Keeps the result a whole number between the min and max inside it."},
	{Holder: "value", Key: "abs", Shape: "value", Class: "math", Blurb: "Drops the result's sign."},
	{Holder: "value", Key: "desc", Shape: "arguments", Blurb: descBlurb},
	{Holder: "value", Key: "format", Shape: "arguments", Blurb: "How a value is shown: its digits, a percent sign, a plus sign."},
	{Holder: "value", Key: "save_temporary_value_as", Shape: "arguments", Class: "saving", Blurb: "Remembers a value under a name until this script value ends."},
	{Holder: "widget", Key: AnyKey, Shape: "widget"},
	{Key: "and", Class: "logical", Blurb: "True only when everything inside it is true."},
	{Key: "or", Class: "logical", Blurb: "True when at least one thing inside it is true."},
	{Key: "not", Class: "logical", Blurb: "True when what is inside it is false."},
	{Key: "nor", Class: "logical", Blurb: "True only when nothing inside it is true."},
	{Key: "nand", Class: "logical", Blurb: "True unless everything inside it is true."},
	{Key: "all_false", Class: "logical", Blurb: "True only when nothing inside it is true (the same as NOR)."},
	{Key: "any_false", Class: "logical", Blurb: "True when at least one thing inside it is false (the same as NAND)."},
	{Key: "always", Class: "logical", Blurb: "True or false as written: always = yes is always true."},
	{Key: "if", Shape: "enclosing", Class: "conditional", Blurb: "Runs what is inside it when its limit holds."},
	{Key: "else_if", Shape: "enclosing", Class: "conditional", Blurb: "Runs when the if or else_if before it did not, and its own limit holds."},
	{Key: "else", Shape: "enclosing", Class: "conditional", Blurb: "Runs when no if or else_if before it ran."},
	{Key: "trigger_if", Shape: "enclosing", Class: "conditional", Blurb: "Checks what is inside it only when its limit holds."},
	{Key: "trigger_else_if", Shape: "enclosing", Class: "conditional", Blurb: "Checks what is inside it when the trigger_if before it was skipped and its own limit holds."},
	{Key: "trigger_else", Shape: "enclosing", Class: "conditional", Blurb: "Checks what is inside it when every trigger_if before it was skipped."},
	{Key: "limit", Class: "conditional", Blurb: "The conditions an if, a list, a while or a trigger_if checks first."},
	{Key: "alternative_limit", Class: "conditional", Blurb: "A second limit a list falls back to when its limit matches nothing."},
	{Key: "switch", Class: "conditional", Blurb: "Reads one trigger and runs the case whose value matches, else the fallback."},
	{Key: "calc_true_if", Class: "conditional", Blurb: "True when at least amount of the triggers inside it are true."},
	{Key: "while", Class: "flow", Blurb: "Repeats what is inside it while its limit holds, or count times."},
	{Key: "random", Class: "flow", Blurb: "Runs what is inside it with the chance given, else nothing."},
	{Key: "random_list", Class: "flow", Blurb: "Picks one branch at random; each branch's number is its weight."},
	{Key: "hidden_effect", Class: "display", Blurb: "Runs its effects without showing them in the tooltip."},
	{Key: "show_as_tooltip", Class: "display", Blurb: "Shows its effects in the tooltip without running them."},
	{Key: "custom_tooltip", Class: "display", Blurb: "Shows a text of your own in place of the tooltip of what is inside."},
	{Key: "custom_description", Class: "display", Blurb: "Shows a text of your own in place of the generated description."},
	{Key: "custom_label", Class: "display", Blurb: "Shows a text of your own in the tooltip, with this scope as its subject."},
	{Key: "save_scope_as", Class: "saving", Blurb: "Remembers the current scope under a name for the rest of the event chain."},
	{Key: "save_temporary_scope_as", Class: "saving", Blurb: "Remembers the current scope under a name until this effect ends."},
	{Key: "save_scope_value_as", Class: "saving", Blurb: "Remembers a number or a yes/no under a name for the rest of the event chain."},
	{Key: "save_temporary_scope_value_as", Class: "saving", Blurb: "Remembers a number or a yes/no under a name until this effect ends."},
	{Key: "add_to_list", Class: "saving", Blurb: "Adds the current scope to a named list for the rest of the event chain."},
	{Key: "add_to_temporary_list", Class: "saving", Blurb: "Adds the current scope to a named list until this effect ends."},
	{Key: "exists", Class: "saving", Blurb: "True when the scope named is there and is not nothing."},
}

// DumpPhrases are the dumps' own words that the engine decides by.
var DumpPhrases = struct {
	Iterates string   // a blurb opening with it and naming no target walks a list or variable map: scope unknown
	NoScope  []string // scope names that name no scope
}{"Iterate through", []string{"none", "all"}}

// LinkMakers are, for each link of the dumps that reads a name back (scope:x, var:x, local_var:x, global_var:x),
// the commands that make its names: a command whose value is the name, or command/key for the key in its block
// that holds it. The dumps declare the links and the commands, but only their prose says which command makes the
// name a link reads.
var LinkMakers = map[string][]string{
	SavedScopes: {
		"save_scope_as", "save_temporary_scope_as", "save_scope_value_as/name", "save_temporary_scope_value_as/name",
		"save_opinion_value_as/name", "save_temporary_opinion_value_as/name",
	},
	Variables: {
		"set_variable", "set_variable/name", "set_dead_character_variable/name", "add_to_variable_list/name",
		"add_to_variable_map/name",
	},
	LocalVariables: {
		"set_local_variable", "set_local_variable/name", "add_to_local_variable_list/name", "add_to_local_variable_map/name",
	},
	GlobalVariables: {
		"set_global_variable", "set_global_variable/name", "add_to_global_variable_list/name",
		"add_to_global_variable_map/name",
	},
}

// Bools are the words a yes-or-no value is written with.
var Bools = []string{"yes", "no"}

// LookupKeyword returns the first keyword that fits a key, given the shape of the block it is in and the command
// that holds it: by the key alone, or by the command and the key. A key that is a number or a date also fits the
// <number> form. A prefix keyword never fits here. It returns false if none fits.
func LookupKeyword(shape, command, key string) (Keyword, bool) {
	number := IsNumberKey(key)
	for _, row := range Keywords {
		if row.Holder != "" && row.Holder != shape || strings.HasSuffix(row.Key, "*") {
			continue
		}
		holder, last, held := strings.Cut(row.Key, "/")
		if !held {
			last = row.Key
		} else if holder != command {
			continue
		}
		if last == key || last == AnyKey || number && last == NumberKey {
			return row, true
		}
	}
	return Keyword{}, false
}

// IsNumberKey reports whether a key is written as a number or a date, the keys that a <number> form stands for.
func IsNumberKey(key string) bool {
	_, date := jomini.ParseDate(key)
	return date || jomini.IsNumber(key)
}

// LookupLinkMaker returns the link whose names a command makes, given the command or command/key for a key in its
// block. It returns false if the command makes no name a link reads.
func LookupLinkMaker(maker string) (string, bool) {
	for link, makers := range LinkMakers {
		if slices.Contains(makers, maker) {
			return link, true
		}
	}
	return "", false
}

// LookupDescriptionKeyword returns the keyword for a key inside a dynamic description: its text, a marker or its
// trigger, else the keyword of the same key in any block (and, or), else the zero keyword.
func LookupDescriptionKeyword(key string) Keyword {
	row, _ := LookupKeyword("description", "", key)
	return row
}

// ClassifyKey returns the operator class of a key, or "" if it is not an operator. Keys are matched in lower case
// because the games write AND and and alike. A list iterator's prefix is no class by itself, since a scripted
// trigger may be named any_x.
func ClassifyKey(shape, command, key string) string {
	keyword, _ := LookupKeyword(shape, command, strings.ToLower(key))
	return keyword.Class
}

// IsOperator reports whether a key is an operator of a value block.
func IsOperator(key string) bool {
	return slices.ContainsFunc(Keywords, func(row Keyword) bool { return row.Holder == "value" && row.Key == key })
}

// MatchListIterator returns the keyword whose prefix a key starts with: a call that iterates a scripted list, whose
// block runs in the list's item scope. It returns false if the key starts with none.
func MatchListIterator(key string) (Keyword, bool) {
	for _, row := range Keywords {
		if prefix, cut := strings.CutSuffix(row.Key, "*"); cut && strings.HasPrefix(key, prefix) {
			return row, true
		}
	}
	return Keyword{}, false
}
