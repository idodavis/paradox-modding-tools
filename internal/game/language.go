// language.go holds the script language's own rules: the shapes a block can hold and how each reads its keys, the
// keywords with the sentence a hover card shows for each and the words a condition reads, and the words that name a
// scope, a variable or a yes-or-no.

package game

import (
	"path"
	"slices"
	"strings"

	"github.com/samber/lo"

	"paradox-modding-tools/internal/jomini"
)

// A ShapeRule is how a block of one shape reads its keys.
type ShapeRule struct {
	Rules  []string // the key rules tried in order; the first that fits wins
	Caller string   // the word a call here resolves by; AnyCall every callable
	Allows []string // the declared commands it holds; inside a trigger, never an effect
	// Values is the database every value written in a block of this shape names; "" means none.
	Values string
	// Blocks is the shape of a block written in a block of this shape where its key gives none; "" means the key's rule
	// decides.
	Blocks string
	// Encloses is the shape of the block that a link or an "enclosing" keyword written in a block of this shape holds;
	// "" means the shape itself.
	Encloses string
}

// A Keyword is a key of the language itself: one it gives a fixed shape to, or one of its operators. The first row
// that fits a key wins.
type Keyword struct {
	// Key is a key path in lower case. Written command/key, it fits the key only in that command's block.
	Key    string
	Games  []string // the games it holds for; none means every game
	Holder string   // fits only in a block of this shape; "" means any
	Shape  string   // what its block holds, a Shapes key or "enclosing"; "" means what the key's rule gives
	Caller string   // the word its calls resolve by; "" means the shape's own
	Shows  string   // a description marker: which of its texts it shows, first, random or leaf
	Class  string   // the operator class: logical, conditional, flow, display, saving, math, list; "" means none
	Blurb  string   // the one line a hover card shows
	// Says is what a condition in words reads for it, a * in it the rest of a prefix key; "" means its key in words.
	Says string
	// Covers marks a keyword whose own text is shown in place of the conditions written inside it.
	Covers bool
	// ValueFirst marks a keyword whose value a condition in words reads first, as what it is about: X exists.
	ValueFirst bool
	// Negates marks a keyword that is true when what it holds is false; around one condition, it reads as that
	// condition, negated.
	Negates bool
	// Follows are the keys, in lower case, one of which must be written just before it in its block; empty means any.
	Follows []string
	// Weighted marks a keyword whose branches are weights: a branch's key is a number, and its chance is its share.
	Weighted bool
	// Modifies marks a keyword whose block changes the weight of the block it is written in.
	Modifies bool
	// Preview marks a keyword whose block is shown in the tooltip and never run.
	Preview bool
	// Values is the database of the text it shows, written as its value or as the text key in its block; "" means none.
	Values string
}

// AnyCall is the Caller of a block that resolves a call in every callable.
const AnyCall = "*"

// AnyKey is the key that matches any key written in a block, such as switch's cases.
const AnyKey = "<any>"

// DocDateKey is the key the games' own docs write for a date, as `date = { … }` stands for each dated block of a
// history file.
const DocDateKey = "date"

// The dumps' words for what declares a command, by the file that declares it. Command.Declared lists them in dump
// order.
const (
	DeclaredEffect  = "effect"       // effects.log
	DeclaredTrigger = "trigger"      // triggers.log
	DeclaredLink    = "event target" // event_targets.log
)

// The databases of the names script saves and reads back: saved scopes (scope:x), the variables of a scope (var:x), of
// the effect being run (local_var:x) and of the game (global_var:x), the lists add_to_list makes, and the flags CK3
// gives a character.
const (
	SavedScopes     = "scope"
	Variables       = "var"
	LocalVariables  = "local_var"
	GlobalVariables = "global_var"
	Lists           = "list"
	Flags           = "flag"
)

// Links are the databases of the names script saves and reads back, which no file defines.
var Links = []string{SavedScopes, Variables, LocalVariables, GlobalVariables, Lists, Flags}

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

	// EU5 in_game/common/estate_privileges/readme.txt:17 and 22 more lines of its readmes
	"scaled and triggered modifier": {Value: "block", Shape: ShapeModifiers},
}

// The shapes that Go code names. Each is the key of its row in Shapes.
const (
	ShapeUnknown   = "unknown"   // a block whose keys can be of any kind
	ShapeFields    = "fields"    // a block of a definition's own fields
	ShapeArguments = "arguments" // a block of what a command takes
	// ShapeEffectArguments and ShapeTriggerArguments are the block of an effect and of a trigger: what it takes, and
	// the commands of its own kind it runs.
	ShapeEffectArguments  = "effect arguments"
	ShapeTriggerArguments = "trigger arguments"
	ShapeTriggers         = "triggers"    // a block of triggers
	ShapeEffects          = "effects"     // a block of effects
	ShapeValue            = "value"       // a block that computes a number
	ShapeWidget           = "widget"      // a block of a GUI widget's properties and children
	ShapeModifiers        = "modifiers"   // a block of modifiers, each a name and a number
	ShapeDescription      = "description" // a block that writes a text with its trigger
)

// Shapes is the table of shapes a block can hold, each with how it reads its keys. A description block reads its
// keys by the Keywords alone, and "enclosing" in a keyword or a claim means the holder's Encloses, else its shape. The
// "scope-link" rule is "link" except for a plain name written with a plain value, which is an argument or a call.
var Shapes = map[string]ShapeRule{
	ShapeFields: {Rules: []string{"field"}},
	ShapeTriggers: {
		Rules:  []string{"known-call", "declared", "command", "observed", "link", "call"},
		Caller: callTrigger, Allows: []string{DeclaredTrigger},
	},
	ShapeEffects: {
		Rules:  []string{"known-call", "declared", "command", "observed", "scope-link", "call"},
		Caller: callEffect, Allows: []string{DeclaredEffect},
	},
	ShapeArguments: {
		Rules:  []string{"known-call", "declared", "command", "observed", "link", "call"},
		Caller: AnyCall, Allows: []string{DeclaredEffect, DeclaredTrigger},
	},
	// A key no rule explains in a command's block is a call of any kind, which is never judged: it may be an argument
	// that neither the dumps nor the game's files write.
	ShapeEffectArguments: {
		Rules:  []string{"known-call", "declared", "command", "observed", "scope-link", "call"},
		Caller: AnyCall, Allows: []string{DeclaredEffect}, Encloses: ShapeEffects,
	},
	ShapeTriggerArguments: {
		Rules:  []string{"known-call", "declared", "command", "observed", "scope-link", "call"},
		Caller: AnyCall, Allows: []string{DeclaredTrigger}, Encloses: ShapeTriggers,
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
	// _on_actions.info:55-90 (CK3; EU5's on_actions.info and Vic3's _on_actions.md alike): one entry is picked to fire
	{Key: "random_events/<number>", Weighted: true, Blurb: "One event the on_action can pick; its number is how likely it is. An event of 0 is a chance that none fires."},
	{Key: "random_on_action/<number>", Weighted: true, Blurb: "One on_action the on_action can pick; its number is how likely it is."},
	{Key: "random_on_actions/<number>", Weighted: true, Blurb: "One on_action the on_action can pick; its number is how likely it is."},
	{Key: "switch/<any>", Shape: "enclosing", Blurb: "One case of a switch: runs when the trigger's value is this."},
	{Key: "every_*", Shape: "effects", Class: "list", Blurb: "Runs the effects inside it on every item of the list named after every_."},
	{Key: "random_*", Shape: "effects", Class: "list", Blurb: "Runs the effects inside it on one random item of the list."},
	{Key: "ordered_*", Shape: "effects", Class: "list", Blurb: "Runs the effects inside it on the items of the list in the order given."},
	{Key: "any_*", Shape: "triggers", Class: "list", Says: "any * where", Blurb: "True when at least one item of the list passes the triggers inside it."},
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
	// EU5 in_game/common/international_organizations/hre.txt:897, "desc_sort_order = -99999 # Make sure we are last"
	{Holder: "value", Key: "desc_sort_order", Class: "display", Blurb: "Where this block's line sorts when the value is broken down: the lowest comes last."},
	{Holder: "value", Key: "save_temporary_value_as", Shape: "arguments", Class: "saving", Blurb: "Remembers a value under a name until this script value ends."},
	// a weight block: CK3 _scripted_modifiers.info:13-22, EU5 scripted_modifiers.info:8-22, Vic3 _on_actions.md:50-55
	{Holder: "value", Key: "base", Shape: "value", Class: "math", Blurb: "The number a weight starts from."},
	// the CK3 wiki's Weight modifier page
	{Holder: "value", Key: "factor", Shape: "value", Class: "math", Blurb: "Multiplies the weight by the number given."},
	{Holder: "value", Key: "modifier", Shape: "value", Modifies: true, Blurb: "Changes the weight by its add or factor when the triggers inside it hold."},
	// common/character_interactions/00_alliance.txt:648 writes a weight modifier's conditions in it
	{Holder: "value", Key: "trigger", Shape: ShapeTriggers, Blurb: "The conditions that must hold for the modifier around it to apply."},
	{Holder: "value", Key: "opinion_modifier", Shape: "arguments", Modifies: true, Blurb: "Changes the weight by how much one character likes another."},
	{Holder: "value", Key: "compare_modifier", Shape: "arguments", Modifies: true, Blurb: "Changes the weight by how a value of one character compares with another's."},
	{Key: "and", Shape: "triggers", Class: "logical", Says: "all of", Blurb: "True only when everything inside it is true."},
	{Key: "or", Shape: "triggers", Class: "logical", Says: "any of", Blurb: "True when at least one thing inside it is true."},
	{Key: "not", Shape: "triggers", Class: "logical", Says: "not all of", Negates: true, Blurb: "True when what is inside it is false."},
	{Key: "nor", Shape: "triggers", Class: "logical", Says: "none of", Negates: true, Blurb: "True only when nothing inside it is true."},
	{Key: "nand", Shape: "triggers", Class: "logical", Says: "not all of", Negates: true, Blurb: "True unless everything inside it is true."},
	{Key: "all_false", Shape: "triggers", Class: "logical", Says: "none of", Negates: true, Blurb: "True only when nothing inside it is true (the same as NOR)."},
	{Key: "any_false", Shape: "triggers", Class: "logical", Says: "not all of", Negates: true, Blurb: "True when at least one thing inside it is false (the same as NAND)."},
	{Key: "always", Class: "logical", Blurb: "True or false as written: always = yes is always true."},
	{Key: "if", Shape: "enclosing", Class: "conditional", Blurb: "Runs what is inside it when its limit holds."},
	{Key: "else_if", Shape: "enclosing", Class: "conditional", Blurb: "Runs when the if or else_if before it did not, and its own limit holds.", Follows: []string{"if", "else_if"}},
	{Key: "else", Shape: "enclosing", Class: "conditional", Blurb: "Runs when no if or else_if before it ran.", Follows: []string{"if", "else_if"}},
	{Key: "trigger_if", Shape: "enclosing", Class: "conditional", Says: "if", Blurb: "Checks what is inside it only when its limit holds."},
	{Key: "trigger_else_if", Shape: "enclosing", Class: "conditional", Says: "else if", Blurb: "Checks what is inside it when the trigger_if before it was skipped and its own limit holds.", Follows: []string{"trigger_if", "trigger_else_if"}},
	{Key: "trigger_else", Shape: "enclosing", Class: "conditional", Says: "else", Blurb: "Checks what is inside it when every trigger_if before it was skipped.", Follows: []string{"trigger_if", "trigger_else_if"}},
	{Key: "limit", Shape: "triggers", Class: "conditional", Says: "when", Blurb: "The conditions an if, a list, a while or a trigger_if checks first."},
	{Key: "alternative_limit", Shape: "triggers", Class: "conditional", Blurb: "A second limit a list falls back to when its limit matches nothing."},
	{Key: "switch", Class: "conditional", Blurb: "Reads one trigger and runs the case whose value matches, else the fallback."},
	{Key: "calc_true_if", Shape: "triggers", Class: "conditional", Says: "at least the amount of", Blurb: "True when at least amount of the triggers inside it are true."},
	{Key: "while", Shape: "effects", Class: "flow", Blurb: "Repeats what is inside it while its limit holds, or count times."},
	{Key: "random", Shape: "effects", Class: "flow", Blurb: "Runs what is inside it with the chance given, else nothing."},
	{Key: "random_list", Class: "flow", Blurb: "Picks one branch at random; each branch's number is its weight."},
	{Key: "hidden_effect", Shape: "effects", Class: "display", Blurb: "Runs its effects without showing them in the tooltip."},
	{Key: "show_as_tooltip", Shape: "effects", Class: "display", Blurb: "Shows its effects in the tooltip without running them.", Preview: true},
	{Key: "custom_tooltip", Class: "display", Covers: true, Values: Localization, Blurb: "Shows a text of your own in place of the tooltip of what is inside."},
	{Key: "custom_description", Class: "display", Covers: true, Blurb: "Shows a text of your own in place of the generated description."},
	{Key: "custom_label", Class: "display", Values: Localization, Blurb: "Shows a text of your own in the tooltip, with this scope as its subject."},
	{Key: "save_scope_as", Class: "saving", Blurb: "Remembers the current scope under a name for the rest of the event chain."},
	{Key: "save_temporary_scope_as", Class: "saving", Blurb: "Remembers the current scope under a name until this effect ends."},
	{Key: "save_scope_value_as", Class: "saving", Blurb: "Remembers a number or a yes/no under a name for the rest of the event chain."},
	{Key: "save_temporary_scope_value_as", Class: "saving", Blurb: "Remembers a number or a yes/no under a name until this effect ends."},
	{Key: "add_to_list", Class: "saving", Blurb: "Adds the current scope to a named list for the rest of the event chain."},
	{Key: "add_to_temporary_list", Class: "saving", Blurb: "Adds the current scope to a named list until this effect ends."},
	{Key: "exists", Class: "saving", ValueFirst: true, Blurb: "True when the scope named is there and is not nothing."},
	// EU5 in_game/common/subject_types/readme.txt:80 writes it as goal keys, each holding a type and a bias
	{Games: []string{eu5}, Holder: "fields", Key: "ai_biases", Shape: "fields", Blurb: "The weight the AI adds to certain goals, one block for each goal with its type and its bias."},
}

// LimitKey is the key of the conditions an if, a list or a while checks first.
const LimitKey = "limit"

// TriggerKey is the key of the conditions a block holds for itself, such as an event's or an option's trigger.
const TriggerKey = "trigger"

// OperatorSays is what a condition in words reads for each operator between a key and a value.
var OperatorSays = map[string]string{
	"=": "is", "==": "is", "!=": "is not", ">": "more than", ">=": "at least", "<": "less than", "<=": "at most",
	"?=": "is, if it exists",
}

// DumpPhrases are the dumps' own words that the engine decides by.
var DumpPhrases = struct {
	Iterates string   // a blurb opening with it and naming no target walks a list or variable map: scope unknown
	NoScope  []string // scope names that name no scope
	Literal  string   // the scope of a link whose name is a literal, as flag:the_boss's
	Unnamed  string   // the return type of a data function whose type the dumps do not name
}{"Iterate through", []string{"none", "all", "none/any"}, "flag", "[unregistered]"}

// A DumpCorrection is a command whose dump row contradicts the text of its own entry. It carries the scopes and the
// target that the text says. The schema applies it to the dumps after reading them, so it outranks the dump.
type DumpCorrection struct {
	Games   []string // the games it holds for; none means every game
	Command string   // the command's name
	Scopes  []string // the scopes it runs in
	Target  string   // the scope it leads to
}

// DumpCorrections are the dump rows that contradict their own text. A row's comment cites the lines of the entry's
// text and of its Supported Scopes and Supported Targets.
var DumpCorrections = []DumpCorrection{
	// Vic3 docs/effects.log:3521 says "Iterate through all Provinces in the scoped State"; :3523-3524 swap the two
	{Games: []string{vic3}, Command: "every_province", Scopes: []string{"state"}, Target: "province"},
	// Vic3 docs/effects.log:14240 says the same; :14249-14250 swap the two too
	{Games: []string{vic3}, Command: "ordered_province", Scopes: []string{"state"}, Target: "province"},
	// Vic3 docs/effects.log:29483 says the same; :29485-29486 swap the two too
	{Games: []string{vic3}, Command: "random_province", Scopes: []string{"state"}, Target: "province"},
	// Vic3 docs/triggers.log:2360 says the same; :2363-2364 swap the two too
	{Games: []string{vic3}, Command: "any_province", Scopes: []string{"state"}, Target: "province"},
}

// LinkMakers are the commands that save a name for one of the Links. A row is a command, or command/key for a key
// inside the command's block, and its value is the link that reads the name back.
var LinkMakers = map[string]string{
	"save_scope_as": SavedScopes, "save_temporary_scope_as": SavedScopes, "save_scope_value_as/name": SavedScopes,
	"save_temporary_scope_value_as/name": SavedScopes, "save_opinion_value_as/name": SavedScopes,
	"save_temporary_opinion_value_as/name": SavedScopes,
	// a script value's (CK3 common/script_values; EU5 generic_actions/western_schism.txt:361 writes the block)
	"save_temporary_value_as": SavedScopes, "save_temporary_value_as/name": SavedScopes,
	// CK3 _character_interactions.info:349 and _decisions.info:315, 395: the option picked sets scope:<name>
	"send_option/flag": SavedScopes, "item/value": SavedScopes,
	// EU5 cabinet_actions/assimilate_area.txt:16: the target picked is scope:<name>
	"select_trigger/target_flag": SavedScopes,
	"set_variable":               Variables, "set_variable/name": Variables, "set_dead_character_variable/name": Variables,
	"add_to_variable_list/name": Variables, "add_to_variable_map/name": Variables,
	"set_local_variable": LocalVariables, "set_local_variable/name": LocalVariables,
	"add_to_local_variable_list/name": LocalVariables, "add_to_local_variable_map/name": LocalVariables,
	"set_global_variable": GlobalVariables, "set_global_variable/name": GlobalVariables,
	"add_to_global_variable_list/name": GlobalVariables, "add_to_global_variable_map/name": GlobalVariables,
	"add_to_list": Lists, "add_to_temporary_list": Lists,
	// CK3 effects.log:2984 and 3014: "where X is the name of the flag", "flag = <name>"
	"add_character_flag": Flags, "add_character_flag/flag": Flags, "add_dead_character_flag/flag": Flags,
}

// LinkUsers are the triggers and lists that read a name saved for one of the Links. A row is shaped like LinkMakers',
// and its value is the link whose names it reads. Each is in its game's triggers.log or effects.log. The variable
// maps are Vic3's and EU5's, and a character's flags are CK3's.
var LinkUsers = map[string]string{
	"has_variable": Variables, "has_variable/name": Variables, "has_all_variables/name": Variables,
	"has_none_of_variables/name": Variables, "has_dead_character_variable": Variables, "has_variable_list": Variables,
	"variable_list_size/name": Variables, "is_target_in_variable_list/name": Variables, "has_variable_map": Variables,
	"variable_map_size/name": Variables, "is_key_in_variable_map/name": Variables,
	"is_value_in_variable_map/name": Variables, "any_key_in_variable_map/variable": Variables,
	"every_key_in_variable_map/variable": Variables, "ordered_key_in_variable_map/variable": Variables,
	"random_key_in_variable_map/variable": Variables, "any_in_list/variable": Variables,
	"every_in_list/variable": Variables, "ordered_in_list/variable": Variables, "random_in_list/variable": Variables,
	"has_local_variable": LocalVariables, "has_local_variable_list": LocalVariables,
	"local_variable_list_size/name": LocalVariables, "is_target_in_local_variable_list/name": LocalVariables,
	"has_local_variable_map": LocalVariables, "local_variable_map_size/name": LocalVariables,
	"is_key_in_local_variable_map/name": LocalVariables, "is_value_in_local_variable_map/name": LocalVariables,
	"any_key_in_local_variable_map/variable": LocalVariables, "every_key_in_local_variable_map/variable": LocalVariables,
	"ordered_key_in_local_variable_map/variable": LocalVariables,
	"random_key_in_local_variable_map/variable":  LocalVariables, "any_in_local_list/variable": LocalVariables,
	"every_in_local_list/variable": LocalVariables, "ordered_in_local_list/variable": LocalVariables,
	"random_in_local_list/variable": LocalVariables,
	"has_global_variable":           GlobalVariables, "has_global_variable_list": GlobalVariables,
	"global_variable_list_size/name": GlobalVariables, "is_target_in_global_variable_list/name": GlobalVariables,
	"has_global_variable_map": GlobalVariables, "global_variable_map_size/name": GlobalVariables,
	"is_key_in_global_variable_map/name": GlobalVariables, "is_value_in_global_variable_map/name": GlobalVariables,
	"any_key_in_global_variable_map/variable":     GlobalVariables,
	"every_key_in_global_variable_map/variable":   GlobalVariables,
	"ordered_key_in_global_variable_map/variable": GlobalVariables,
	"random_key_in_global_variable_map/variable":  GlobalVariables, "any_in_global_list/variable": GlobalVariables,
	"every_in_global_list/variable": GlobalVariables, "ordered_in_global_list/variable": GlobalVariables,
	"random_in_global_list/variable": GlobalVariables,
	"is_in_list":                     Lists, "any_in_list/list": Lists, "every_in_list/list": Lists,
	"ordered_in_list/list": Lists, "random_in_list/list": Lists,
	"has_character_flag": Flags, "has_dead_character_flag": Flags, "remove_character_flag": Flags,
}

// LocStems are the arguments whose value is the stem of a localization key. A row is shaped like LinkMakers', and its
// value is that key with the stem at %s.
var LocStems = map[string]string{
	// CK3: ActivityLogEntry.GetTitle shows <key>_title, which every key the game's files log defines; the description,
	// GetDescription, is <key> itself where one is written
	"add_activity_log_entry/key": "%s_title",
}

// CommandKeys are the commands whose block's keys, beside the arguments the dumps declare, are names of a database:
// each with that database.
var CommandKeys = map[string]string{
	// CK3 effects.log, stress_impact: "trait = value # add this stress if the character has this trait"
	"stress_impact": "traits", "stress_and_fulfillment_impact": "traits",
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

// LookupKeyword returns the keyword of the game that fits a key in a block of the given shape and command, the key and
// the command in any case. Where several fit, it is the first row of Keywords. It returns false if none fits.
func (game *Game) LookupKeyword(shape, command, key string) (Keyword, bool) {
	command, key = strings.ToLower(command), strings.ToLower(key)
	candidates := []string{key, AnyKey}
	if jomini.IsNumberKey(key) {
		candidates = append(candidates, jomini.NumberKey)
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
			if (row.Holder == "" || row.Holder == shape) && (!held || fits) && game.Matches(row.Games) {
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

// IsOperator reports whether a key is an operator of a value block.
func IsOperator(key string) bool {
	return slices.ContainsFunc(Keywords, func(row Keyword) bool { return row.Holder == "value" && row.Key == key })
}

// MatchListIterator returns the prefix keyword, such as every_*, that a key starts with. Such a key iterates over a
// scripted list, and its block runs in the list's item scope. It returns false if the key starts with none.
func MatchListIterator(key string) (Keyword, bool) {
	return lo.Find(Keywords, func(row Keyword) bool {
		prefix, ok := strings.CutSuffix(row.Key, "*")
		return ok && strings.HasPrefix(key, prefix)
	})
}
