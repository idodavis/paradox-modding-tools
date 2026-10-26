// language.go holds the script language's own rules: the shapes a block can hold and how each reads its keys, the
// keywords with the sentence a hover card shows for each, and the words that name a scope, a variable or a yes-or-no.

package game

import (
	"path"
	"slices"
	"strings"

	"paradox-modding-tools/internal/jomini"
)

// A KeyRule is what follows when a key fits one rule of the language. Whether the rule fits is the walk's test.
type KeyRule struct {
	Fields string // where the block's fields come from: child, callee, arguments or holder
	Shape  string // what decides the shape it holds: claim, command, same or arguments
	Scope  string // how its scope follows: stays, follows or unknown
}

// A ShapeRule is how a block of one shape reads its keys.
type ShapeRule struct {
	Rules  []string // the KeyRules tried in order; the first that fits wins
	Caller string   // the word a call here resolves by; AnyCall every callable
	Allows []string // the declared commands it holds; inside a trigger, never an effect
	// Values is the database every value written in a block of this shape names; "" means none.
	Values string
	// Blocks is the shape of a block written in a block of this shape where its key gives none; "" means the key's rule
	// decides.
	Blocks string
}

// A Keyword is a key of the language itself: one it gives a fixed shape to, or one of its operators. The first row
// that fits a key wins.
type Keyword struct {
	// Key is a key path in lower case: the commands that must hold the key, then the key.
	Key    string
	Holder string // fits only in a block of this shape; "" means any
	Shape  string // what its block holds, a Shapes key; enclosing the holder's; "" means what the key's rule gives
	Caller string // the word its calls resolve by; "" means the shape's own
	Shows  string // a description marker: which of its texts it shows, first, random or leaf
	Class  string // the operator class: logical, conditional, flow, display, saving, math, list; "" means none
	Blurb  string // the one line a hover card shows
	// Weighted marks a keyword whose branches are weights: a branch's key is a number, and its chance is its share.
	Weighted bool
	// Preview marks a keyword whose block is shown in the tooltip and never run.
	Preview bool
}

// AnyCall is the Caller of a block that resolves a call in every callable.
const AnyCall = "*"

// AnyKey is the key that matches any key written in a block, such as switch's cases.
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

// customBlurb is the sentence shown for a list's custom text.
const customBlurb = "The text shown for the list in the tooltip: a localization key."

// ScopeLinks are the language's links to the scopes a walk holds at any place in a definition: the definition's
// root, the scope before the last step, and the current one. The dumps declare them as links with no target.
var ScopeLinks = struct{ Root, Previous, This string }{"root", "prev", "this"}

// ConceptFormat is the format a data call in a localization text carries after its bar when its one step is the name
// of a game concept, as in [faith|E]. It is written in either case.
const ConceptFormat = "E"

// A DocType is what a type word in the games' own docs stands for.
type DocType struct {
	Value    string // bool, number, date, text, reference, color, block, or unknown for a key or a block
	Database string // the database a reference names; "" means unknown
	Shape    string // a Shapes key for a block; "" for none
	Values   bool   // the term takes a script value
}

// DocTypes are the words the games' own docs write a type with, as the installs' .info, .md and readme files
// write them, each with what it means.
var DocTypes = map[string]DocType{
	"bool": {Value: "bool"}, "boolean": {Value: "bool"}, "yes/no": {Value: "bool"},
	"int": {Value: "number"}, "integer": {Value: "number"}, "int32": {Value: "number"}, "number": {Value: "number"},
	"float": {Value: "number"}, "decimal": {Value: "number"}, "fixed point": {Value: "number"}, "value": {Value: "number", Values: true},
	"script value": {Value: "number", Values: true}, "scripted value": {Value: "number", Values: true},
	"scripted value int32": {Value: "number"},
	"date":                 {Value: "date"},
	"string":               {Value: "text"}, "text": {Value: "text"}, "name": {Value: "text"}, "identifier": {Value: "text"},
	"localization key": {Value: "reference", Database: Localization}, "loc key": {Value: "reference", Database: Localization},
	"localisation key": {Value: "reference", Database: Localization},
	"database key":     {Value: "reference"}, "key": {Value: "reference"}, "array of database keys": {Value: "reference"},
	"string path": {Value: "reference", Database: File}, "path": {Value: "reference", Database: File},
	"texture": {Value: "reference", Database: File}, "file": {Value: "reference", Database: File},
	"color":   {Value: "color"},
	"trigger": {Value: "block", Shape: "triggers"}, "triggers": {Value: "block", Shape: "triggers"},
	"effect": {Value: "block", Shape: "effects"}, "effects": {Value: "block", Shape: "effects"},
	"dynamic description": {Value: "unknown", Shape: "description"},
	"modifier":            {Value: "block", Shape: ShapeModifiers}, "modifiers": {Value: "block", Shape: ShapeModifiers},
	"modifier(s)": {Value: "block", Shape: ShapeModifiers}, "block": {Value: "block"}, "mtth": {Value: "block"},
	"scripted cost": {Value: "block"}, "cost": {Value: "block"}, "vector 2": {Value: "block"}, "vector2": {Value: "block"},
}

// KeyRules is the table of key rules by name.
var KeyRules = map[string]KeyRule{
	"known-call":    {Fields: "callee", Shape: "claim", Scope: "stays"},        // a scripted definition vanilla defines
	"declared":      {Fields: "child", Shape: "claim", Scope: "stays"},         // a key the dumps, docs or wiki declare
	"documented":    {Fields: "child", Shape: "claim", Scope: "stays"},         // a key the database's docs write
	"command":       {Fields: "arguments", Shape: "command", Scope: "follows"}, // a trigger or effect the dumps declare
	"operator":      {Fields: "child", Shape: "arguments", Scope: "stays"},     // a value block's operator (IsOperator)
	"link":          {Fields: "holder", Shape: "same", Scope: "follows"},       // a link the dumps declare
	"modifier":      {Fields: "child", Shape: "claim", Scope: "stays"},         // a modifier the dumps declare
	"modifier-type": {Fields: "child", Shape: "claim", Scope: "stays"},         // any other name: a modifier type
	"observed":      {Fields: "child", Shape: "claim", Scope: "unknown"},       // a key vanilla writes: scope unknown
	"field":         {Fields: "child", Shape: "claim", Scope: "stays"},         // the place's own field, known or not
	"field-or-call": {Fields: "child", Shape: "claim", Scope: "stays"},         // its own field, or a call vanilla writes
	"call":          {Fields: "child", Shape: "claim", Scope: "stays"},         // any other key: a scripted call
}

// The shapes that Go code names. Each is the key of its row in Shapes.
const (
	ShapeUnknown     = "unknown"     // a block whose keys can be of any kind
	ShapeFields      = "fields"      // a block of a definition's own fields
	ShapeArguments   = "arguments"   // a block of what a command takes
	ShapeTriggers    = "triggers"    // a block of triggers
	ShapeEffects     = "effects"     // a block of effects
	ShapeValue       = "value"       // a block that computes a number
	ShapeWidget      = "widget"      // a block of a GUI widget's properties and children
	ShapeModifiers   = "modifiers"   // a block of modifiers, each a name and a number
	ShapeDescription = "description" // a block that writes a text with its trigger
)

// Shapes is the table of shapes a block can hold, each with how it reads its keys. A description block reads its
// keys by the Keywords alone, and "enclosing" in a keyword or a claim means the holder's shape.
var Shapes = map[string]ShapeRule{
	ShapeFields: {Rules: []string{"field"}},
	ShapeTriggers: {
		Rules:  []string{"known-call", "declared", "command", "observed", "link", "call"},
		Caller: callTrigger, Allows: []string{DeclaredTrigger},
	},
	ShapeEffects: {
		Rules:  []string{"known-call", "declared", "command", "observed", "link", "call"},
		Caller: callEffect, Allows: []string{DeclaredEffect},
	},
	ShapeArguments: {
		Rules:  []string{"known-call", "declared", "command", "observed", "link", "call"},
		Caller: AnyCall, Allows: []string{DeclaredEffect, DeclaredTrigger},
	},
	ShapeValue: {
		Rules:  []string{"known-call", "declared", "command", "operator", "observed", "link", "call"},
		Caller: callValue, Allows: []string{DeclaredEffect, DeclaredTrigger},
	},
	ShapeUnknown: {
		Rules:  []string{"known-call", "declared", "command", "link", "field-or-call"},
		Caller: AnyCall, Allows: []string{DeclaredEffect, DeclaredTrigger},
	},
	"setup": {
		Rules:  []string{"declared", "documented", "known-call", "command", "observed", "link", "call"},
		Caller: callEffect, Allows: []string{DeclaredEffect},
	},
	ShapeWidget:    {Rules: []string{"known-call", "field", "call"}, Caller: guiTagCalls, Blocks: ShapeWidget},
	ShapeModifiers: {Rules: []string{"modifier", "declared", "observed", "modifier-type"}},
	// _events.info:313-318 writes a description's texts as localization keys
	"description": {Values: Localization},
}

// Keywords lists the keys the language gives a fixed shape to, then the operators the dumps declare as commands.
var Keywords = []Keyword{
	{Key: "random_list/<number>", Shape: "effects", Caller: AnyCall, Weighted: true, Blurb: "One branch of a random list; its number is how likely it is."},
	{Key: "switch/<any>", Shape: "enclosing", Blurb: "One case of a switch: runs when the trigger's value is this."},
	{Key: "every_*", Shape: "effects", Class: "list", Blurb: "Runs the effects inside it on every item of the list named after every_."},
	{Key: "random_*", Shape: "effects", Class: "list", Blurb: "Runs the effects inside it on one random item of the list."},
	{Key: "ordered_*", Shape: "effects", Class: "list", Blurb: "Runs the effects inside it on the items of the list in the order given."},
	{Key: "any_*", Shape: "triggers", Class: "list", Blurb: "True when at least one item of the list passes the triggers inside it."},
	// no dump declares a list's custom text; the games write it inside every_ and ordered_ lists only
	{Key: "every_*/custom", Shape: "description", Class: "display", Blurb: customBlurb},
	{Key: "ordered_*/custom", Shape: "description", Class: "display", Blurb: customBlurb},
	{Holder: "description", Key: "first_valid", Shape: "description", Class: "display", Shows: "first", Blurb: "Shows the first text inside it whose trigger holds."},
	{Holder: "description", Key: "random_valid", Shape: "description", Class: "display", Shows: "random", Blurb: "Shows one of the texts inside it whose trigger holds, at random."},
	{Holder: "description", Key: "triggered_desc", Shape: "description", Class: "display", Shows: "leaf", Blurb: "A text shown only when its trigger holds."},
	{Holder: "description", Key: "trigger", Shape: "triggers", Blurb: "The conditions that must hold for the block around it to count."},
	{Holder: "description", Key: "desc", Shape: "description", Blurb: descBlurb}, // a scalar desc is the text itself
	{Holder: "value", Key: "value", Shape: "value", Class: "math", Blurb: "The number a script value starts from, or is set to."},
	{Holder: "arguments", Key: "value", Shape: "value", Class: "math", Blurb: "The number the command takes, or a script value that works it out."},
	{Holder: "value", Key: "add", Shape: "value", Class: "math", Blurb: "Adds the number or script value given."},
	{Holder: "value", Key: "subtract", Shape: "value", Class: "math", Blurb: "Subtracts the number or script value given."},
	{Holder: "value", Key: "multiply", Shape: "value", Class: "math", Blurb: "Multiplies by the number or script value given."},
	{Holder: "value", Key: "divide", Shape: "value", Class: "math", Blurb: "Divides by the number or script value given."},
	{Holder: "value", Key: "modulo", Shape: "value", Class: "math", Blurb: "Keeps the remainder of dividing by the number given."},
	{Holder: "value", Key: "min", Shape: "value", Class: "math", Blurb: "Keeps the result at least the number given."},
	{Holder: "value", Key: "max", Shape: "value", Class: "math", Blurb: "Keeps the result at most the number given."},
	{Holder: "value", Key: "round", Shape: "value", Class: "math", Blurb: "Rounds the result to the nearest whole number."},
	{Holder: "value", Key: "round_to", Shape: "value", Class: "math", Blurb: "Rounds the result to the nearest multiple of the number given."},
	// _script_values.info:38
	{Holder: "value", Key: "pow", Shape: "value", Class: "math", Blurb: "Raises the result to the power of the number given."},
	{Holder: "value", Key: "ceiling", Shape: "value", Class: "math", Blurb: "Rounds the result up to a whole number."},
	{Holder: "value", Key: "floor", Shape: "value", Class: "math", Blurb: "Rounds the result down to a whole number."},
	{Holder: "value", Key: "fixed_range", Shape: "value", Class: "math", Blurb: "Keeps the result between the min and max inside it."},
	{Holder: "value", Key: "integer_range", Shape: "value", Class: "math", Blurb: "Keeps the result a whole number between the min and max inside it."},
	{Holder: "value", Key: "abs", Shape: "value", Class: "math", Blurb: "Drops the result's sign."},
	// _script_values.info:48-49
	{Holder: "value", Key: "desc", Shape: "description", Blurb: descBlurb},
	{Holder: "value", Key: "format", Shape: "description", Blurb: "The text that lays out this block's line when the value is broken down."},
	{Holder: "value", Key: "save_temporary_value_as", Shape: "arguments", Class: "saving", Blurb: "Remembers a value under a name until this script value ends."},
	// a weight block: CK3 _scripted_modifiers.info:13-22, EU5 scripted_modifiers.info:8-22, Vic3 _on_actions.md:50-55
	{Holder: "value", Key: "base", Shape: "value", Class: "math", Blurb: "The number a weight starts from."},
	// the CK3 wiki's Weight modifier page
	{Holder: "value", Key: "factor", Shape: "value", Class: "math", Blurb: "Multiplies the weight by the number given."},
	{Holder: "value", Key: "modifier", Shape: "value", Blurb: "Changes the weight by its add or factor when the triggers inside it hold."},
	// common/character_interactions/00_alliance.txt:648 writes a weight modifier's conditions in it
	{Holder: "value", Key: "trigger", Shape: ShapeTriggers, Blurb: "The conditions that must hold for the modifier around it to apply."},
	{Holder: "value", Key: "opinion_modifier", Shape: "arguments", Blurb: "Changes the weight by how much one character likes another."},
	{Holder: "value", Key: "compare_modifier", Shape: "arguments", Blurb: "Changes the weight by how a value of one character compares with another's."},
	{Key: "and", Shape: "triggers", Class: "logical", Blurb: "True only when everything inside it is true."},
	{Key: "or", Shape: "triggers", Class: "logical", Blurb: "True when at least one thing inside it is true."},
	{Key: "not", Shape: "triggers", Class: "logical", Blurb: "True when what is inside it is false."},
	{Key: "nor", Shape: "triggers", Class: "logical", Blurb: "True only when nothing inside it is true."},
	{Key: "nand", Shape: "triggers", Class: "logical", Blurb: "True unless everything inside it is true."},
	{Key: "all_false", Shape: "triggers", Class: "logical", Blurb: "True only when nothing inside it is true (the same as NOR)."},
	{Key: "any_false", Shape: "triggers", Class: "logical", Blurb: "True when at least one thing inside it is false (the same as NAND)."},
	{Key: "always", Class: "logical", Blurb: "True or false as written: always = yes is always true."},
	{Key: "if", Shape: "enclosing", Class: "conditional", Blurb: "Runs what is inside it when its limit holds."},
	{Key: "else_if", Shape: "enclosing", Class: "conditional", Blurb: "Runs when the if or else_if before it did not, and its own limit holds."},
	{Key: "else", Shape: "enclosing", Class: "conditional", Blurb: "Runs when no if or else_if before it ran."},
	{Key: "trigger_if", Shape: "enclosing", Class: "conditional", Blurb: "Checks what is inside it only when its limit holds."},
	{Key: "trigger_else_if", Shape: "enclosing", Class: "conditional", Blurb: "Checks what is inside it when the trigger_if before it was skipped and its own limit holds."},
	{Key: "trigger_else", Shape: "enclosing", Class: "conditional", Blurb: "Checks what is inside it when every trigger_if before it was skipped."},
	{Key: "limit", Shape: "triggers", Class: "conditional", Blurb: "The conditions an if, a list, a while or a trigger_if checks first."},
	{Key: "alternative_limit", Shape: "triggers", Class: "conditional", Blurb: "A second limit a list falls back to when its limit matches nothing."},
	{Key: "switch", Class: "conditional", Blurb: "Reads one trigger and runs the case whose value matches, else the fallback."},
	{Key: "calc_true_if", Shape: "triggers", Class: "conditional", Blurb: "True when at least amount of the triggers inside it are true."},
	{Key: "while", Shape: "effects", Class: "flow", Blurb: "Repeats what is inside it while its limit holds, or count times."},
	{Key: "random", Shape: "effects", Class: "flow", Blurb: "Runs what is inside it with the chance given, else nothing."},
	{Key: "random_list", Class: "flow", Blurb: "Picks one branch at random; each branch's number is its weight."},
	{Key: "hidden_effect", Shape: "effects", Class: "display", Blurb: "Runs its effects without showing them in the tooltip."},
	{Key: "show_as_tooltip", Shape: "effects", Class: "display", Blurb: "Shows its effects in the tooltip without running them.", Preview: true},
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

// LinkMakers are the commands that make the names a link such as scope:x or var:x reads, by link.
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

// keywordPositions holds where each keyword sits in Keywords, by the key it fits. A keyword written under a command
// is held by its key without the command. A prefix keyword is not held.
var keywordPositions = func() map[string][]int {
	positions := map[string][]int{}
	for position, row := range Keywords {
		if !strings.HasSuffix(row.Key, "*") {
			_, last, held := strings.Cut(row.Key, "/")
			if !held {
				last = row.Key
			}
			positions[last] = append(positions[last], position)
		}
	}
	return positions
}()

// iteratorKeywords holds the keywords whose key is a prefix, such as any_*, in the order Keywords gives them.
var iteratorKeywords = slices.DeleteFunc(slices.Clone(Keywords), func(row Keyword) bool { return !strings.HasSuffix(row.Key, "*") })

// makerLinks holds, for each command that LinkMakers names, the link that reads the names the command makes.
var makerLinks = func() map[string]string {
	links := map[string]string{}
	for link, makers := range LinkMakers {
		for _, maker := range makers {
			links[maker] = link
		}
	}
	return links
}()

// LookupKeyword returns the first keyword that fits a key in a block's shape and command. It returns false if none
// fits.
func LookupKeyword(shape, command, key string) (Keyword, bool) {
	candidates := append(make([]string, 0, 3), key, AnyKey)
	if IsNumberKey(key) {
		candidates = append(candidates, NumberKey)
	}
	best, ok := 0, false
	for _, candidate := range candidates {
		for _, position := range keywordPositions[candidate] {
			if ok && position > best {
				break
			}
			row := Keywords[position]
			holder, _, held := strings.Cut(row.Key, "/")
			fits, _ := path.Match(holder, command)
			if (row.Holder == "" || row.Holder == shape) && (!held || fits) {
				best, ok = position, true
				break
			}
		}
	}
	if !ok {
		return Keyword{}, false
	}
	return Keywords[best], true
}

// IsNumberKey reports whether a key is written as a number or a date, the keys that a <number> form stands for.
func IsNumberKey(key string) bool {
	_, date := jomini.ParseDate(key)
	return date || jomini.IsNumber(key)
}

// LookupLinkMaker returns the link whose names a command makes, given the command or command/key for a key in its
// block. It returns false if the command makes no name a link reads.
func LookupLinkMaker(maker string) (string, bool) {
	link, ok := makerLinks[maker]
	return link, ok
}

// ClassifyKey returns the operator class of a key, ignoring case, or "" if it is not an operator.
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
	for _, row := range iteratorKeywords {
		if strings.HasPrefix(key, strings.TrimSuffix(row.Key, "*")) {
			return row, true
		}
	}
	return Keyword{}, false
}
