// language.go holds the script language's own rules: how a block of each shape reads its keys, the operators and
// fixed-shape keys with the sentence a hover card shows, and the words that name a scope, a variable or a yes-or-no.

package game

import (
	"slices"
	"strings"
)

// Shape is the kind of content a block holds.
type Shape uint8

// KeyRule is one way a key can be read; a block tries its rules in order and the first that fits wins.
type KeyRule uint8

// BlockAt is where a key's block reads.
type BlockAt uint8

// ShapeFrom is what decides the shape a key's block holds.
type ShapeFrom uint8

// ScopeFrom is how a key's block gets its scope.
type ScopeFrom uint8

// KeyRuleRow is what reading a key by one rule means for its block; whether the rule fits is the walker's test.
type KeyRuleRow struct {
	BlockAt   BlockAt   // where the block reads
	ShapeFrom ShapeFrom // what decides the shape it holds
	ScopeFrom ScopeFrom // how its scope follows
}

// ShapeRule is how a block of one shape reads its keys.
type ShapeRule struct {
	KeyRules []KeyRule // the rules a key is tried by, in order
	Caller   string    // the word a call here resolves by; AnyCall for every scripted folder
	Allows   []string  // the declared commands it holds (Declared*); inside a trigger, never an effect
}

// Keyword is a key of the language itself, one it gives a fixed shape to or one of its operators; the first row that
// fits a key wins.
type Keyword struct {
	Command   string    // fits only inside this command's block; "" any
	Holder    Shape     // fits only in a block of this shape; ShapeUnknown any
	Key       string    // the key as written in lower case (<number> and <any> match by form), or its prefix
	Prefix    bool      // Key is a prefix: a call that iterates the scripted list named after it
	Shape     Shape     // the shape its block holds; ShapeEnclosing the holder's; ShapeUnknown what the key's rule gives
	ScopeFrom ScopeFrom // how its block's scope follows
	Caller    string    // the word its calls resolve by; "" the shape's own
	Leaf      bool      // a description's text, read where the description belongs and counted nowhere
	Class     string    // the operator class it belongs to (logical, conditional, flow, display, saving, math, list)
	Shows     string    // which of a description's texts a marker shows (first, random, leaf)
	Blurb     string    // the one line a hover card shows
	Source    Source    // SourceDumps where every game declares the key, SourceRegistry where none does
}

// ScopeVarCommand is a command whose value makes or reads a name script keeps; a link of the same name reads it back.
type ScopeVarCommand struct {
	Database string // the database of the names (SavedScopes, Variables...)
	Makes    bool   // it makes the name; else it reads one
}

// The shapes a block can hold.
const (
	ShapeUnknown     Shape = iota // no source says what it holds
	ShapeFields                   // an entry's own keys, or a field's
	ShapeTriggers                 // conditions
	ShapeEffects                  // actions
	ShapeArguments                // a command's arguments only
	ShapeValue                    // a script value's formula
	ShapeDescription              // a dynamic description
	ShapeSetup                    // a history entry's start state: the docs' fields, else effects
	ShapeWidget                   // a GUI widget: its properties, and blocks that are widgets again
	ShapeEnclosing                // in a keyword or a claim: the holder's shape
)

// The rules a block can read a key by.
const (
	KeyRuleKnownCall      KeyRule = iota // the name of a scripted entry vanilla defines: a call
	KeyRuleDeclared                      // a key the dumps, docs or wiki declare at this place
	KeyRuleDocumented                    // a key the docs of the entry's database write (Database.Fields)
	KeyRuleCommand                       // a trigger or effect the dumps declare
	KeyRuleOperator                      // a value operator (ValueOperators)
	KeyRuleLink                          // a link the dumps declare, read by name or prefix
	KeyRuleObserved                      // a key vanilla writes at this place
	KeyRulePosition                      // the place's own key, known or not
	KeyRulePositionOrCall                // the place's own key, and a call where vanilla writes commands here
	KeyRuleCall                          // any other key: a call to a scripted entry
	KeyRuleNone                          // no rule: a statement with no key, outside an entry, or reopening its holder
)

// The places a key's block can read.
const (
	AtChild  BlockAt = iota // the position under the holder by the key's written form, made when the walk creates
	AtCallee                // the scripted entry's parameters, else its database's entry
	AtArgs                  // the declared command's arguments
	AtHolder                // where the key stands: a link
	AtNone                  // nowhere: a key no rule places
)

// The deciders of a block's shape.
const (
	HoldsClaim     ShapeFrom = iota // the position's claim, else its evidence
	HoldsCommand                    // the command's declared kinds and the holder's shape
	HoldsSame                       // the holder's shape
	HoldsArguments                  // the command's arguments
)

// The ways a block's scope follows.
const (
	ScopeStays   ScopeFrom = iota // the holder's scope when the block's shape is known, else unknown
	ScopeFollows                  // the command's or link's target when it is a scope that runs script, else unknown
	ScopeUnknown                  // the scope is unknown whatever the holder's
)

// AnyCall is the Caller of a block that resolves a call in every scripted folder.
const AnyCall = "*"

// The words a scripted folder is called by, from the blocks that hold its kind.
const (
	callTrigger = "trigger"
	callEffect  = "effect"
	callValue   = "value"
)

// The dumps' words for what declares a command, by the file that declares it; Command.Declared lists them in dump
// order.
const (
	DeclaredEffect  = "effect"       // effects.log
	DeclaredTrigger = "trigger"      // triggers.log
	DeclaredLink    = "event target" // event_targets.log
)

// AnyKey is the key that matches any key written in a block (switch's cases).
const AnyKey = "<any>"

// descBlurb is the sentence of a description's leaf, which a value block's desc shares.
const descBlurb = "The text shown: a localization key, or a block that chooses one."

// The databases of the names script saves and a link reads back: saved scopes (scope:x) and variables of a scope
// (var:x), of the effect being run (local_var:x) and of the game (global_var:x).
const (
	SavedScopes     = "scope"
	Variables       = "var"
	LocalVariables  = "local_var"
	GlobalVariables = "global_var"
)

// KeyRules is the row of each key rule.
var KeyRules = [...]KeyRuleRow{
	KeyRuleKnownCall:      {AtCallee, HoldsClaim, ScopeStays},
	KeyRuleDeclared:       {AtChild, HoldsClaim, ScopeStays},
	KeyRuleDocumented:     {AtChild, HoldsClaim, ScopeStays},
	KeyRuleCommand:        {AtArgs, HoldsCommand, ScopeFollows},
	KeyRuleOperator:       {AtChild, HoldsArguments, ScopeStays},
	KeyRuleLink:           {AtHolder, HoldsSame, ScopeFollows},
	KeyRuleObserved:       {AtChild, HoldsClaim, ScopeStays},
	KeyRulePosition:       {AtChild, HoldsClaim, ScopeStays},
	KeyRulePositionOrCall: {AtChild, HoldsClaim, ScopeStays},
	KeyRuleCall:           {AtChild, HoldsClaim, ScopeStays},
}

// Shapes is the rule of each shape of block; a description block reads its keys by the Keywords alone.
var Shapes = map[Shape]ShapeRule{
	ShapeFields:   {KeyRules: []KeyRule{KeyRulePosition}},
	ShapeTriggers: {KeyRules: []KeyRule{KeyRuleKnownCall, KeyRuleDeclared, KeyRuleCommand, KeyRuleLink, KeyRuleObserved, KeyRuleCall}, Caller: callTrigger, Allows: []string{DeclaredTrigger}},
	ShapeEffects:  {KeyRules: []KeyRule{KeyRuleKnownCall, KeyRuleDeclared, KeyRuleCommand, KeyRuleLink, KeyRuleObserved, KeyRuleCall}, Caller: callEffect, Allows: []string{DeclaredEffect}},
	ShapeValue: {
		KeyRules: []KeyRule{KeyRuleKnownCall, KeyRuleDeclared, KeyRuleCommand, KeyRuleOperator, KeyRuleLink, KeyRuleObserved, KeyRuleCall},
		Caller:   callValue, Allows: []string{DeclaredEffect, DeclaredTrigger},
	},
	ShapeArguments: {KeyRules: []KeyRule{KeyRuleKnownCall, KeyRuleDeclared, KeyRuleCommand, KeyRuleObserved, KeyRuleCall}, Caller: AnyCall},
	ShapeUnknown: {
		KeyRules: []KeyRule{KeyRuleKnownCall, KeyRuleDeclared, KeyRuleCommand, KeyRuleLink, KeyRulePositionOrCall},
		Caller:   AnyCall, Allows: []string{DeclaredEffect, DeclaredTrigger},
	},
	ShapeSetup: {
		KeyRules: []KeyRule{KeyRuleDeclared, KeyRuleDocumented, KeyRuleKnownCall, KeyRuleCommand, KeyRuleObserved, KeyRuleLink, KeyRuleCall},
		Caller:   callEffect, Allows: []string{DeclaredEffect},
	},
	ShapeWidget:      {KeyRules: []KeyRule{KeyRuleKnownCall, KeyRulePosition}, Caller: "widget"},
	ShapeDescription: {},
}

// Keywords are the keys the language gives a fixed shape to (random_list's branches, switch's cases, the list
// iterators, a description's markers, a value block's operators, a widget's blocks), then the operators the dumps
// declare as commands, which name no shape.
var Keywords = []Keyword{
	{Command: "random_list", Key: "<number>", Shape: ShapeEffects, Caller: AnyCall, Source: SourceRegistry,
		Blurb: "One branch of a random list; its number is how likely it is."},
	{Command: "switch", Key: AnyKey, Shape: ShapeEnclosing, Source: SourceRegistry,
		Blurb: "One case of a switch: runs when the trigger's value is this."},
	{Prefix: true, Key: "every_", Shape: ShapeEffects, ScopeFrom: ScopeUnknown, Class: "list", Source: SourceDumps,
		Blurb: "Runs the effects inside it on every item of the list named after every_."},
	{Prefix: true, Key: "random_", Shape: ShapeEffects, ScopeFrom: ScopeUnknown, Class: "list", Source: SourceDumps,
		Blurb: "Runs the effects inside it on one random item of the list."},
	{Prefix: true, Key: "ordered_", Shape: ShapeEffects, ScopeFrom: ScopeUnknown, Class: "list", Source: SourceDumps,
		Blurb: "Runs the effects inside it on the items of the list in the order given."},
	{Prefix: true, Key: "any_", Shape: ShapeTriggers, ScopeFrom: ScopeUnknown, Class: "list", Source: SourceDumps,
		Blurb: "True when at least one item of the list passes the triggers inside it."},
	{Holder: ShapeDescription, Key: "first_valid", Shape: ShapeDescription, Class: "display", Shows: "first",
		Source: SourceRegistry, Blurb: "Shows the first text inside it whose trigger holds."},
	{Holder: ShapeDescription, Key: "random_valid", Shape: ShapeDescription, Class: "display", Shows: "random",
		Source: SourceRegistry, Blurb: "Shows one of the texts inside it whose trigger holds, at random."},
	{Holder: ShapeDescription, Key: "triggered_desc", Shape: ShapeDescription, Class: "display", Shows: "leaf",
		Source: SourceRegistry, Blurb: "A text shown only when its trigger holds."},
	{Holder: ShapeDescription, Key: "trigger", Shape: ShapeTriggers, Source: SourceRegistry,
		Blurb: "The conditions that must hold for the block around it to count."},
	{Holder: ShapeDescription, Key: "desc", Leaf: true, Source: SourceRegistry, Blurb: descBlurb},
	{Holder: ShapeValue, Key: "value", Shape: ShapeValue, Class: "math", Source: SourceDumps,
		Blurb: "The number a script value starts from, or is set to."},
	{Holder: ShapeValue, Key: "add", Shape: ShapeValue, Class: "math", Source: SourceRegistry,
		Blurb: "Adds the number or script value given."},
	{Holder: ShapeValue, Key: "subtract", Shape: ShapeValue, Class: "math", Source: SourceRegistry,
		Blurb: "Subtracts the number or script value given."},
	{Holder: ShapeValue, Key: "multiply", Shape: ShapeValue, Class: "math", Source: SourceRegistry,
		Blurb: "Multiplies by the number or script value given."},
	{Holder: ShapeValue, Key: "divide", Shape: ShapeValue, Class: "math", Source: SourceRegistry,
		Blurb: "Divides by the number or script value given."},
	{Holder: ShapeValue, Key: "modulo", Shape: ShapeValue, Class: "math", Source: SourceRegistry,
		Blurb: "Keeps the remainder of dividing by the number given."},
	{Holder: ShapeValue, Key: "min", Shape: ShapeValue, Class: "math", Source: SourceRegistry,
		Blurb: "Keeps the result at least the number given."},
	{Holder: ShapeValue, Key: "max", Shape: ShapeValue, Class: "math", Source: SourceRegistry,
		Blurb: "Keeps the result at most the number given."},
	{Holder: ShapeValue, Key: "round", Shape: ShapeValue, Class: "math", Source: SourceRegistry,
		Blurb: "Rounds the result to the nearest whole number."},
	{Holder: ShapeValue, Key: "round_to", Shape: ShapeValue, Class: "math", Source: SourceRegistry,
		Blurb: "Rounds the result to the nearest multiple of the number given."},
	{Holder: ShapeValue, Key: "ceiling", Shape: ShapeValue, Class: "math", Source: SourceRegistry,
		Blurb: "Rounds the result up to a whole number."},
	{Holder: ShapeValue, Key: "floor", Shape: ShapeValue, Class: "math", Source: SourceRegistry,
		Blurb: "Rounds the result down to a whole number."},
	{Holder: ShapeValue, Key: "fixed_range", Shape: ShapeValue, Class: "math", Source: SourceRegistry,
		Blurb: "Keeps the result between the min and max inside it."},
	{Holder: ShapeValue, Key: "integer_range", Shape: ShapeValue, Class: "math", Source: SourceRegistry,
		Blurb: "Keeps the result a whole number between the min and max inside it."},
	{Holder: ShapeValue, Key: "abs", Shape: ShapeValue, Class: "math", Source: SourceRegistry,
		Blurb: "Drops the result's sign."},
	{Holder: ShapeValue, Key: "desc", Shape: ShapeArguments, Source: SourceRegistry, Blurb: descBlurb},
	{Holder: ShapeValue, Key: "format", Shape: ShapeArguments, Source: SourceRegistry,
		Blurb: "How a value is shown: its digits, a percent sign, a plus sign."},
	{Holder: ShapeValue, Key: "save_temporary_value_as", Shape: ShapeArguments, Class: "saving", Source: SourceRegistry,
		Blurb: "Remembers a value under a name until this script value ends."},
	{Holder: ShapeWidget, Key: AnyKey, Shape: ShapeWidget, Source: SourceRegistry},
	{Key: "and", Class: "logical", Source: SourceDumps, Blurb: "True only when everything inside it is true."},
	{Key: "or", Class: "logical", Source: SourceDumps, Blurb: "True when at least one thing inside it is true."},
	{Key: "not", Class: "logical", Source: SourceDumps, Blurb: "True when what is inside it is false."},
	{Key: "nor", Class: "logical", Source: SourceDumps, Blurb: "True only when nothing inside it is true."},
	{Key: "nand", Class: "logical", Source: SourceDumps, Blurb: "True unless everything inside it is true."},
	{Key: "all_false", Class: "logical", Source: SourceDumps,
		Blurb: "True only when nothing inside it is true (the same as NOR)."},
	{Key: "any_false", Class: "logical", Source: SourceDumps,
		Blurb: "True when at least one thing inside it is false (the same as NAND)."},
	{Key: "always", Class: "logical", Source: SourceDumps,
		Blurb: "True or false as written: always = yes is always true."},
	{Key: "if", Class: "conditional", Source: SourceDumps, Blurb: "Runs what is inside it when its limit holds."},
	{Key: "else_if", Class: "conditional", Source: SourceDumps,
		Blurb: "Runs when the if or else_if before it did not, and its own limit holds."},
	{Key: "else", Class: "conditional", Source: SourceDumps, Blurb: "Runs when no if or else_if before it ran."},
	{Key: "trigger_if", Class: "conditional", Source: SourceDumps,
		Blurb: "Checks what is inside it only when its limit holds."},
	{Key: "trigger_else_if", Class: "conditional", Source: SourceDumps,
		Blurb: "Checks what is inside it when the trigger_if before it was skipped and its own limit holds."},
	{Key: "trigger_else", Class: "conditional", Source: SourceDumps,
		Blurb: "Checks what is inside it when every trigger_if before it was skipped."},
	{Key: "limit", Class: "conditional", Source: SourceRegistry,
		Blurb: "The conditions an if, a list, a while or a trigger_if checks first."},
	{Key: "alternative_limit", Class: "conditional", Source: SourceRegistry,
		Blurb: "A second limit a list falls back to when its limit matches nothing."},
	{Key: "switch", Class: "conditional", Source: SourceDumps,
		Blurb: "Reads one trigger and runs the case whose value matches, else the fallback."},
	{Key: "calc_true_if", Class: "conditional", Source: SourceDumps,
		Blurb: "True when at least amount of the triggers inside it are true."},
	{Key: "while", Class: "flow", Source: SourceDumps,
		Blurb: "Repeats what is inside it while its limit holds, or count times."},
	{Key: "random", Class: "flow", Source: SourceDumps,
		Blurb: "Runs what is inside it with the chance given, else nothing."},
	{Key: "random_list", Class: "flow", Source: SourceDumps,
		Blurb: "Picks one branch at random; each branch's number is its weight."},
	{Key: "hidden_effect", Class: "display", Source: SourceDumps,
		Blurb: "Runs its effects without showing them in the tooltip."},
	{Key: "show_as_tooltip", Class: "display", Source: SourceDumps,
		Blurb: "Shows its effects in the tooltip without running them."},
	{Key: "custom_tooltip", Class: "display", Source: SourceDumps,
		Blurb: "Shows a text of your own in place of the tooltip of what is inside."},
	{Key: "custom_description", Class: "display", Source: SourceDumps,
		Blurb: "Shows a text of your own in place of the generated description."},
	{Key: "custom_label", Class: "display", Source: SourceDumps,
		Blurb: "Shows a text of your own in the tooltip, with this scope as its subject."},
	{Key: "save_scope_as", Class: "saving", Source: SourceDumps,
		Blurb: "Remembers the current scope under a name for the rest of the event chain."},
	{Key: "save_temporary_scope_as", Class: "saving", Source: SourceDumps,
		Blurb: "Remembers the current scope under a name until this effect ends."},
	{Key: "save_scope_value_as", Class: "saving", Source: SourceDumps,
		Blurb: "Remembers a number or a yes/no under a name for the rest of the event chain."},
	{Key: "save_temporary_scope_value_as", Class: "saving", Source: SourceDumps,
		Blurb: "Remembers a number or a yes/no under a name until this effect ends."},
	{Key: "add_to_list", Class: "saving", Source: SourceDumps,
		Blurb: "Adds the current scope to a named list for the rest of the event chain."},
	{Key: "add_to_temporary_list", Class: "saving", Source: SourceDumps,
		Blurb: "Adds the current scope to a named list until this effect ends."},
	{Key: "exists", Class: "saving", Source: SourceDumps,
		Blurb: "True when the scope named is there and is not nothing."},
}

// DumpWords are the dumps' words the engine decides by.
var DumpWords = struct {
	Iterates string   // a blurb opening with it and naming no target walks a list or variable map: scope unknown
	NoScope  []string // scope names that name no scope
}{"Iterate through", []string{"none", "all"}}

// ScopeVarCommands are the commands that make or read a saved scope or a variable, by name or command/key for a key
// inside the command's block.
var ScopeVarCommands = map[string]ScopeVarCommand{
	"save_scope_as":                        {SavedScopes, true},
	"save_temporary_scope_as":              {SavedScopes, true},
	"save_scope_value_as/name":             {SavedScopes, true},
	"save_temporary_scope_value_as/name":   {SavedScopes, true},
	"save_opinion_value_as/name":           {SavedScopes, true},
	"save_temporary_opinion_value_as/name": {SavedScopes, true},
	"set_variable":                         {Variables, true},
	"set_variable/name":                    {Variables, true},
	"add_to_variable_list/name":            {Variables, true},
	"has_variable":                         {Variables, false},
	"set_local_variable":                   {LocalVariables, true},
	"set_local_variable/name":              {LocalVariables, true},
	"add_to_local_variable_list/name":      {LocalVariables, true},
	"has_local_variable":                   {LocalVariables, false},
	"set_global_variable":                  {GlobalVariables, true},
	"set_global_variable/name":             {GlobalVariables, true},
	"add_to_global_variable_list/name":     {GlobalVariables, true},
	"has_global_variable":                  {GlobalVariables, false},

	// the commands that read a variable or variable list, and the keys a list iterator takes one by
	"variable": {Variables, false}, "change_variable/name": {Variables, false}, "clamp_variable/name": {Variables, false},
	"round_variable/name": {Variables, false}, "remove_variable": {Variables, false}, "has_variable_list": {Variables, false},
	"clear_variable_list": {Variables, false}, "is_target_in_variable_list/name": {Variables, false},
	"variable_list_size/name": {Variables, false}, "remove_list_variable/name": {Variables, false},
	"local_variable": {LocalVariables, false}, "change_local_variable/name": {LocalVariables, false},
	"clamp_local_variable/name": {LocalVariables, false}, "round_local_variable/name": {LocalVariables, false},
	"remove_local_variable": {LocalVariables, false}, "has_local_variable_list": {LocalVariables, false},
	"clear_local_variable_list": {LocalVariables, false}, "is_target_in_local_variable_list/name": {LocalVariables, false},
	"local_variable_list_size/name": {LocalVariables, false}, "remove_list_local_variable/name": {LocalVariables, false},
	"global_variable": {GlobalVariables, false}, "change_global_variable/name": {GlobalVariables, false},
	"clamp_global_variable/name": {GlobalVariables, false}, "round_global_variable/name": {GlobalVariables, false},
	"remove_global_variable": {GlobalVariables, false}, "has_global_variable_list": {GlobalVariables, false},
	"clear_global_variable_list": {GlobalVariables, false}, "is_target_in_global_variable_list/name": {GlobalVariables, false},
	"global_variable_list_size/name": {GlobalVariables, false}, "remove_list_global_variable/name": {GlobalVariables, false},
}

// Bools are the words a yes-or-no value is written with.
var Bools = []string{"yes", "no"}

// LookupKeyword returns the first keyword that fits a key in a block of a shape inside a command's block; a prefix
// keyword never fits here.
func LookupKeyword(shape Shape, command, key string) (Keyword, bool) {
	for _, row := range Keywords {
		fitsHolder := (row.Command == "" || row.Command == command) && (row.Holder == ShapeUnknown || row.Holder == shape)
		if fitsHolder && !row.Prefix && (row.Key == key || row.Key == AnyKey) {
			return row, true
		}
	}
	return Keyword{}, false
}

// LookupDescriptionKeyword returns the keyword of a key in a dynamic description: its leaf, a marker or its trigger,
// else the keyword of the same key in any block (and, or), else the zero keyword.
func LookupDescriptionKeyword(key string) Keyword {
	row, _ := LookupKeyword(ShapeDescription, "", key)
	return row
}

// ClassifyKey returns the operator class of a key, matched in lower case since the games write AND and and alike; a
// list iterator's prefix is no class by itself, since a scripted trigger may be named any_x.
func ClassifyKey(shape Shape, command, key string) string {
	keyword, _ := LookupKeyword(shape, command, strings.ToLower(key))
	return keyword.Class
}

// IsOperator reports whether a key is an operator of a value block.
func IsOperator(key string) bool {
	return slices.ContainsFunc(Keywords, func(row Keyword) bool { return row.Holder == ShapeValue && row.Key == key })
}

// MatchListIterator returns the keyword whose prefix a key starts with: a call that iterates a scripted list.
func MatchListIterator(key string) (Keyword, bool) {
	for _, row := range Keywords {
		if row.Prefix && strings.HasPrefix(key, row.Key) {
			return row, true
		}
	}
	return Keyword{}, false
}
