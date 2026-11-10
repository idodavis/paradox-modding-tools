// language.go holds the script language's own rules: shapes and their key
// rules, keywords, how a condition reads in English, and the names script
// saves and reads back.

package game

import (
	"path"
	"slices"
	"strings"

	"paradox-modding-tools/internal/jomini"
)

// ShapeSpec is how a block of one shape reads its keys.
type ShapeSpec struct {
	// KeyRules are the key rules tried in order. The first that fits wins.
	KeyRules []KeyRule

	// Caller is the caller a call here is looked up by, among
	// Callable.Callers. AnyCall means every callable.
	Caller string

	// Allows are the declarations a command in the block may have, so that a
	// trigger block holds no effect.
	Allows []Declaration

	// Values is the database every value in the block names. It is "" for
	// none.
	Values string

	// Blocks is the shape of a block written in it where its key gives none.
	// It is "" where the key's rule decides.
	Blocks Shape

	// Encloses is the shape of the block that a link or an enclosing keyword
	// in it holds. It is "" for the shape itself.
	Encloses Shape
}

// KeyRule is one way a block reads a key: as a call, a field, a command, a
// link and so on.
type KeyRule string

// The key rules, each with the key it fits.
const (
	// KeyRuleKnownCall fits a key that names a scripted definition the block
	// calls, the file's own first.
	KeyRuleKnownCall KeyRule = "known-call"

	// KeyRuleDeclared fits a key that the block's field holds from any source
	// but the game's own files.
	KeyRuleDeclared KeyRule = "declared"

	// KeyRuleDocumented fits a key that the docs write for the block's database.
	KeyRuleDocumented KeyRule = "documented"

	// KeyRuleCommand fits a command that the dumps declare, or a pattern makes,
	// and the block takes.
	KeyRuleCommand KeyRule = "command"

	// KeyRuleOperator fits an operator of a value block, such as add.
	KeyRuleOperator KeyRule = "operator"

	// KeyRuleLink fits a key that starts with a scope link, as scope:target
	// does.
	KeyRuleLink KeyRule = "link"

	// KeyRuleScopeLink fits what KeyRuleLink fits, but not a plain name with a
	// plain value, which is an argument or a call.
	KeyRuleScopeLink KeyRule = "scope-link"

	// KeyRuleModifier fits a modifier the dumps declare.
	KeyRuleModifier KeyRule = "modifier"

	// KeyRuleModifierType fits any identifier, as a modifier type's name, in a
	// game whose modifier types have a database.
	KeyRuleModifierType KeyRule = "modifier-type"

	// KeyRuleObserved fits a key the game's own files write in the block's
	// field.
	KeyRuleObserved KeyRule = "observed"

	// KeyRuleField fits a key that the block's field has a child for.
	KeyRuleField KeyRule = "field"

	// KeyRuleFieldOrCall fits what KeyRuleField fits, else what KeyRuleCall fits.
	KeyRuleFieldOrCall KeyRule = "field-or-call"

	// KeyRuleCall fits any identifier that is no event id, as a call of the
	// block's caller.
	KeyRuleCall KeyRule = "call"
)

// Keyword is a key of the language itself: one it gives a fixed shape to, or
// one of its operators. The first row that fits a key wins.
type Keyword struct {
	// Key is a key path in lower case. Written command/key, it fits only in
	// that command's block.
	Key string

	// Games are the games the row holds for, every game if none.
	Games []string

	// Holder is the shape of the only block it fits in. It is "" for any.
	Holder Shape

	// Shape is what its block holds, a Shapes key or ShapeEnclosing. It is ""
	// for what the key's rule gives.
	Shape Shape

	// Caller is the caller its calls are looked up by. It is "" for the
	// shape's own.
	Caller string

	// Shows is which of the texts inside it the keyword shows.
	Shows Show

	Class KeywordClass

	// Blurb is the line a hover card shows.
	Blurb string

	// Says is how a condition in English reads it, with a * for the rest of
	// a prefix key. It is "" for the key as words.
	Says string

	// Covers is whether its own text is shown in place of the conditions
	// inside it.
	Covers bool

	// ValueFirst is whether a condition in English reads its value first, as
	// in "X exists".
	ValueFirst bool

	// Negates is whether it is true when what is inside it is false.
	Negates bool

	// Follows are the keys, one of which must be written just before it in
	// its block. None means any.
	Follows []string

	// Continues are the Keys of the rows, one of which must be the keyword of
	// the block that holds it. None means any.
	Continues []string

	// Weighted is whether its branches are weights: a branch's key is a
	// number, and its chance is its share.
	Weighted bool

	// Modifies is whether its block changes the weight of the block it is
	// written in.
	Modifies bool

	// Preview is whether its block is shown in the tooltip and never run.
	Preview bool

	// Values is the database of the text it shows, as its value or its text
	// key. It is "" for none.
	Values string
}

// Show is which of the texts inside it a description keyword shows. The zero
// Show is none of them.
type Show string

// The texts a description keyword shows: the first whose trigger holds, one
// of those at random, or the keyword's own, when its trigger holds.
const (
	ShowFirst  Show = "first"
	ShowRandom Show = "random"
	ShowLeaf   Show = "leaf"
)

// KeywordClass is what kind of keyword one is, such as logical for and, or
// math for add. The zero KeywordClass is none.
type KeywordClass string

// The classes of the keywords.
const (
	KeywordClassLogical     KeywordClass = "logical"
	KeywordClassConditional KeywordClass = "conditional"
	KeywordClassFlow        KeywordClass = "flow"
	KeywordClassDisplay     KeywordClass = "display"
	KeywordClassSaving      KeywordClass = "saving"
	KeywordClassMath        KeywordClass = "math"
	KeywordClassList        KeywordClass = "list"
)

// AnyCall is the Caller of a block that resolves a call in every callable.
const AnyCall = "*"

// AnyKey is the key that matches any key written in a block, such as
// switch's cases.
const AnyKey = "<any>"

// Declaration is what the dumps declare a command as: an effect, a trigger or
// a link.
type Declaration string

// The declarations a command can have, as the dumps call them. Each is
// declared in its own log: effects.log, triggers.log and event_targets.log.
const (
	DeclarationEffect  Declaration = "effect"
	DeclarationTrigger Declaration = "trigger"
	DeclarationLink    Declaration = "event target"
)

// The databases of the names script saves and reads back: saved scopes
// (scope:x), variables of a scope (var:x), of the running effect
// (local_var:x) and of the game (global_var:x), lists, and a CK3 character's
// flags.
const (
	SavedScopes     = "scope"
	Variables       = "var"
	LocalVariables  = "local_var"
	GlobalVariables = "global_var"
	Lists           = "list"
	Flags           = "flag"
)

// Links are the databases of the names script saves and reads back, which no
// file defines.
var Links = []string{SavedScopes, Variables, LocalVariables, GlobalVariables, Lists, Flags}

// ScopeLinks are the language's links to the scopes a walk holds at any place
// in a definition: the definition's root, the scope before the last step, and
// the current one. The dumps declare them as links with no target.
var ScopeLinks = struct{ Root, Previous, This string }{"root", "prev", "this"}

// Shape is what a block holds, which decides how its keys are read. It is a
// key of Shapes, or ShapeEnclosing for a keyword's block.
type Shape string

// The shapes a block can hold. Each but ShapeEnclosing is the key of its row
// in Shapes.
const (
	// ShapeUnknown is a block whose keys may be fields, commands, links or
	// calls.
	ShapeUnknown Shape = "unknown"

	// ShapeFields is a block of a definition's own fields, and
	// ShapeArguments a block of what a command takes.
	ShapeFields    Shape = "fields"
	ShapeArguments Shape = "arguments"

	// ShapeEffectArguments and ShapeTriggerArguments are the block of an
	// effect and of a trigger: what the command takes, and the effects or the
	// triggers it runs.
	ShapeEffectArguments  Shape = "effect arguments"
	ShapeTriggerArguments Shape = "trigger arguments"

	ShapeTriggers Shape = "triggers"
	ShapeEffects  Shape = "effects"

	// ShapeValue is a block that computes a number.
	ShapeValue Shape = "value"

	// ShapeWidget is a block of a GUI widget's properties and children.
	ShapeWidget Shape = "widget"

	// ShapeModifiers is a block of modifiers, each a name and a number.
	ShapeModifiers Shape = "modifiers"

	// ShapeDescription is a block that writes a text with its trigger.
	ShapeDescription Shape = "description"

	// ShapeSetup is a block of history: the fields its docs write, and
	// effects.
	ShapeSetup Shape = "setup"

	// ShapeEnclosing is the shape of a keyword's block that holds what its
	// holder encloses: the Encloses of the holder's shape, else that shape.
	ShapeEnclosing Shape = "enclosing"
)

// Shapes is the shapes a block can hold, each with how it reads its keys. A
// description block reads them by the Keywords alone.
var Shapes = map[Shape]ShapeSpec{
	ShapeFields: {KeyRules: []KeyRule{KeyRuleField}},
	ShapeTriggers: {
		KeyRules: []KeyRule{KeyRuleKnownCall, KeyRuleDeclared, KeyRuleCommand, KeyRuleObserved, KeyRuleLink, KeyRuleCall},
		Caller:   callTrigger, Allows: []Declaration{DeclarationTrigger},
	},
	ShapeEffects: {
		KeyRules: []KeyRule{KeyRuleKnownCall, KeyRuleDeclared, KeyRuleCommand, KeyRuleObserved, KeyRuleScopeLink, KeyRuleCall},
		Caller:   callEffect, Allows: []Declaration{DeclarationEffect},
	},
	ShapeArguments: {
		KeyRules: []KeyRule{KeyRuleKnownCall, KeyRuleDeclared, KeyRuleCommand, KeyRuleObserved, KeyRuleLink, KeyRuleCall},
		Caller:   AnyCall, Allows: []Declaration{DeclarationEffect, DeclarationTrigger},
	},
	// A key no rule explains in a command's block is a call of any callable,
	// never judged: it may be an argument no dump or file writes.
	ShapeEffectArguments: {
		KeyRules: []KeyRule{KeyRuleKnownCall, KeyRuleDeclared, KeyRuleCommand, KeyRuleObserved, KeyRuleScopeLink, KeyRuleCall},
		Caller:   AnyCall, Allows: []Declaration{DeclarationEffect}, Encloses: ShapeEffects,
	},
	ShapeTriggerArguments: {
		KeyRules: []KeyRule{KeyRuleKnownCall, KeyRuleDeclared, KeyRuleCommand, KeyRuleObserved, KeyRuleScopeLink, KeyRuleCall},
		Caller:   AnyCall, Allows: []Declaration{DeclarationTrigger}, Encloses: ShapeTriggers,
	},
	ShapeValue: {
		KeyRules: []KeyRule{KeyRuleKnownCall, KeyRuleDeclared, KeyRuleCommand, KeyRuleOperator, KeyRuleObserved, KeyRuleLink, KeyRuleCall},
		Caller:   callValue, Allows: []Declaration{DeclarationEffect, DeclarationTrigger},
	},
	ShapeUnknown: {
		KeyRules: []KeyRule{KeyRuleKnownCall, KeyRuleDeclared, KeyRuleCommand, KeyRuleLink, KeyRuleFieldOrCall},
		Caller:   AnyCall, Allows: []Declaration{DeclarationEffect, DeclarationTrigger},
	},
	ShapeSetup: {
		KeyRules: []KeyRule{KeyRuleDeclared, KeyRuleDocumented, KeyRuleKnownCall, KeyRuleCommand, KeyRuleObserved, KeyRuleLink, KeyRuleCall},
		Caller:   callEffect, Allows: []Declaration{DeclarationEffect},
	},
	ShapeWidget:    {KeyRules: []KeyRule{KeyRuleKnownCall, KeyRuleField, KeyRuleCall}, Caller: guiTagCalls, Blocks: ShapeWidget},
	ShapeModifiers: {KeyRules: []KeyRule{KeyRuleModifier, KeyRuleDeclared, KeyRuleObserved, KeyRuleModifierType}},
	// _events.info:313-318 writes a description's texts as localization keys
	ShapeDescription: {Values: Localization},
}

// descBlurb is the sentence shown for a description, which a value block's
// desc shares.
const descBlurb = "The text shown: a localization key, or a block that chooses one."

// customBlurb is the sentence shown for a list's custom text.
const customBlurb = "The text shown for the list in the tooltip: a localization key."

// Keywords are the keys the language gives a fixed shape to, then the
// operators the dumps declare as commands.
var Keywords = []Keyword{
	{Key: "random_list/<number>", Shape: ShapeEffects, Caller: AnyCall, Weighted: true, Blurb: "One branch of a random list; its number is how likely it is."},
	// _on_actions.info:55-90 (CK3; EU5's on_actions.info and Vic3's
	// _on_actions.md alike): one entry is picked to fire
	{Key: "random_events/<number>", Weighted: true, Blurb: "One event the on_action can pick; its number is how likely it is. An event of 0 is a chance that none fires."},
	{Key: "random_on_action/<number>", Weighted: true, Blurb: "One on_action the on_action can pick; its number is how likely it is."},
	{Key: "random_on_actions/<number>", Weighted: true, Blurb: "One on_action the on_action can pick; its number is how likely it is."},
	// CK3 history/_characters.info:65-66,78: a date entry changes the
	// character's own keys, and reads any other key as an effect
	{Games: []string{CK3}, Holder: ShapeSetup, Key: "<number>", Shape: ShapeSetup, Blurb: "What changes for the character on this date."},
	{Key: "switch/<any>", Shape: ShapeEnclosing, Blurb: "One case of a switch: runs when the trigger's value is this."},
	{Key: "every_*", Shape: ShapeEffects, Class: KeywordClassList, Blurb: "Runs the effects inside it on every item of the list named after every_."},
	{Key: "random_*", Shape: ShapeEffects, Class: KeywordClassList, Blurb: "Runs the effects inside it on one random item of the list."},
	{Key: "ordered_*", Shape: ShapeEffects, Class: KeywordClassList, Blurb: "Runs the effects inside it on the items of the list in the order given."},
	{Key: "any_*", Shape: ShapeTriggers, Class: KeywordClassList, Says: "any * where", Blurb: "True when at least one item of the list passes the triggers inside it."},
	// no dump declares a list's custom text; the games write it inside every_
	// and ordered_ lists only
	{Key: "every_*/custom", Shape: ShapeDescription, Class: KeywordClassDisplay, Blurb: customBlurb},
	{Key: "ordered_*/custom", Shape: ShapeDescription, Class: KeywordClassDisplay, Blurb: customBlurb},
	{Holder: ShapeDescription, Key: "first_valid", Shape: ShapeDescription, Class: KeywordClassDisplay, Shows: ShowFirst, Blurb: "Shows the first text inside it whose trigger holds."},
	{Holder: ShapeDescription, Key: "random_valid", Shape: ShapeDescription, Class: KeywordClassDisplay, Shows: ShowRandom, Blurb: "Shows one of the texts inside it whose trigger holds, at random."},
	{Holder: ShapeDescription, Key: "triggered_desc", Shape: ShapeDescription, Class: KeywordClassDisplay, Shows: ShowLeaf, Blurb: "A text shown only when its trigger holds."},
	{Holder: ShapeDescription, Key: "trigger", Shape: ShapeTriggers, Blurb: "The conditions that must hold for the block around it to count."},
	// a scalar desc is the text itself
	{Holder: ShapeDescription, Key: "desc", Shape: ShapeDescription, Blurb: descBlurb},
	{Holder: ShapeValue, Key: "value", Shape: ShapeValue, Class: KeywordClassMath, Blurb: "The number a script value starts from, or is set to."},
	{Holder: ShapeArguments, Key: "value", Shape: ShapeValue, Class: KeywordClassMath, Blurb: "The number the command takes, or a script value that works it out."},
	{Holder: ShapeValue, Key: "add", Shape: ShapeValue, Class: KeywordClassMath, Blurb: "Adds the number or script value given."},
	{Holder: ShapeValue, Key: "subtract", Shape: ShapeValue, Class: KeywordClassMath, Blurb: "Subtracts the number or script value given."},
	{Holder: ShapeValue, Key: "multiply", Shape: ShapeValue, Class: KeywordClassMath, Blurb: "Multiplies by the number or script value given."},
	{Holder: ShapeValue, Key: "divide", Shape: ShapeValue, Class: KeywordClassMath, Blurb: "Divides by the number or script value given."},
	{Holder: ShapeValue, Key: "modulo", Shape: ShapeValue, Class: KeywordClassMath, Blurb: "Keeps the remainder of dividing by the number given."},
	{Holder: ShapeValue, Key: "min", Shape: ShapeValue, Class: KeywordClassMath, Blurb: "Keeps the result at least the number given."},
	{Holder: ShapeValue, Key: "max", Shape: ShapeValue, Class: KeywordClassMath, Blurb: "Keeps the result at most the number given."},
	{Holder: ShapeValue, Key: "round", Shape: ShapeValue, Class: KeywordClassMath, Blurb: "Rounds the result to the nearest whole number."},
	{Holder: ShapeValue, Key: "round_to", Shape: ShapeValue, Class: KeywordClassMath, Blurb: "Rounds the result to the nearest multiple of the number given."},
	// _script_values.info:38
	{Holder: ShapeValue, Key: "pow", Shape: ShapeValue, Class: KeywordClassMath, Blurb: "Raises the result to the power of the number given."},
	{Holder: ShapeValue, Key: "ceiling", Shape: ShapeValue, Class: KeywordClassMath, Blurb: "Rounds the result up to a whole number."},
	{Holder: ShapeValue, Key: "floor", Shape: ShapeValue, Class: KeywordClassMath, Blurb: "Rounds the result down to a whole number."},
	{Holder: ShapeValue, Key: "fixed_range", Shape: ShapeValue, Class: KeywordClassMath, Blurb: "Keeps the result between the min and max inside it."},
	{Holder: ShapeValue, Key: "integer_range", Shape: ShapeValue, Class: KeywordClassMath, Blurb: "Keeps the result a whole number between the min and max inside it."},
	{Holder: ShapeValue, Key: "abs", Shape: ShapeValue, Class: KeywordClassMath, Blurb: "Drops the result's sign."},
	// _script_values.info:48-49
	{Holder: ShapeValue, Key: "desc", Shape: ShapeDescription, Blurb: descBlurb},
	{Holder: ShapeValue, Key: "format", Shape: ShapeDescription, Blurb: "The text that lays out this block's line when the value is broken down."},
	// EU5 in_game/common/international_organizations/hre.txt:897,
	// "desc_sort_order = -99999 # Make sure we are last"
	{Holder: ShapeValue, Key: "desc_sort_order", Class: KeywordClassDisplay, Blurb: "Where this block's line sorts when the value is broken down: the lowest comes last."},
	{Holder: ShapeValue, Key: "save_temporary_value_as", Shape: ShapeArguments, Class: KeywordClassSaving, Blurb: "Remembers a value under a name until this script value ends."},
	// a weight block: CK3 _scripted_modifiers.info:13-22, EU5
	// scripted_modifiers.info:8-22, Vic3 _on_actions.md:50-55
	{Holder: ShapeValue, Key: "base", Shape: ShapeValue, Class: KeywordClassMath, Blurb: "The number a weight starts from."},
	// the CK3 wiki's Weight modifier page
	{Holder: ShapeValue, Key: "factor", Shape: ShapeValue, Class: KeywordClassMath, Blurb: "Multiplies the weight by the number given."},
	{Holder: ShapeValue, Key: "modifier", Shape: ShapeValue, Modifies: true, Blurb: "Changes the weight by its add or factor when the triggers inside it hold."},
	// common/character_interactions/00_alliance.txt:648 writes a weight
	// modifier's conditions in it
	{Holder: ShapeValue, Key: "trigger", Shape: ShapeTriggers, Blurb: "The conditions that must hold for the modifier around it to apply."},
	{Holder: ShapeValue, Key: "opinion_modifier", Shape: ShapeArguments, Modifies: true, Blurb: "Changes the weight by how much one character likes another."},
	{Holder: ShapeValue, Key: "compare_modifier", Shape: ShapeArguments, Modifies: true, Blurb: "Changes the weight by how a value of one character compares with another's."},
	// CK3 character_interactions/00_scheme_interactions.txt:2099 and
	// schemes/scheme_types/befriend_scheme.txt:315
	{Games: []string{CK3}, Holder: ShapeValue, Key: "compatibility_modifier", Shape: ShapeArguments, Modifies: true, Blurb: "Changes the weight by how well two characters' traits get along."},
	{Key: "and", Shape: ShapeTriggers, Class: KeywordClassLogical, Says: "all of", Blurb: "True only when everything inside it is true."},
	{Key: "or", Shape: ShapeTriggers, Class: KeywordClassLogical, Says: "any of", Blurb: "True when at least one thing inside it is true."},
	{Key: "not", Shape: ShapeTriggers, Class: KeywordClassLogical, Says: "not all of", Negates: true, Blurb: "True when what is inside it is false."},
	{Key: "nor", Shape: ShapeTriggers, Class: KeywordClassLogical, Says: "none of", Negates: true, Blurb: "True only when nothing inside it is true."},
	{Key: "nand", Shape: ShapeTriggers, Class: KeywordClassLogical, Says: "not all of", Negates: true, Blurb: "True unless everything inside it is true."},
	{Key: "all_false", Shape: ShapeTriggers, Class: KeywordClassLogical, Says: "none of", Negates: true, Blurb: "True only when nothing inside it is true (the same as NOR)."},
	{Key: "any_false", Shape: ShapeTriggers, Class: KeywordClassLogical, Says: "not all of", Negates: true, Blurb: "True when at least one thing inside it is false (the same as NAND)."},
	{Key: "always", Class: KeywordClassLogical, Blurb: "True or false as written: always = yes is always true."},
	// Vic3 common/scripted_progress_bars/scripted_progress_bars.md: an else
	// inside a progress_condition takes over when its limit fails, "chained
	// as deep as you need", so it is written inside the block, after its
	// body; the else of such an else is written inside it in the same way
	{Games: []string{Vic3}, Key: "progress_condition/else", Shape: ShapeEnclosing, Class: KeywordClassConditional,
		Blurb: "Takes over when the limit of the progress condition around it fails."},
	{Games: []string{Vic3}, Key: "else/else", Continues: []string{"progress_condition/else", "else/else"},
		Shape: ShapeEnclosing, Class: KeywordClassConditional, Blurb: "Takes over when the limit of the else around it fails."},
	{Key: "if", Shape: ShapeEnclosing, Class: KeywordClassConditional, Blurb: "Runs what is inside it when its limit holds."},
	{Key: "else_if", Shape: ShapeEnclosing, Class: KeywordClassConditional, Blurb: "Runs when the if or else_if before it did not, and its own limit holds.", Follows: []string{"if", "else_if"}},
	{Key: "else", Shape: ShapeEnclosing, Class: KeywordClassConditional, Blurb: "Runs when no if or else_if before it ran.", Follows: []string{"if", "else_if"}},
	{Key: "trigger_if", Shape: ShapeEnclosing, Class: KeywordClassConditional, Says: "if", Blurb: "Checks what is inside it only when its limit holds."},
	{Key: "trigger_else_if", Shape: ShapeEnclosing, Class: KeywordClassConditional, Says: "else if", Blurb: "Checks what is inside it when the trigger_if before it was skipped and its own limit holds.", Follows: []string{"trigger_if", "trigger_else_if"}},
	{Key: "trigger_else", Shape: ShapeEnclosing, Class: KeywordClassConditional, Says: "else", Blurb: "Checks what is inside it when every trigger_if before it was skipped.", Follows: []string{"trigger_if", "trigger_else_if"}},
	{Key: "limit", Shape: ShapeTriggers, Class: KeywordClassConditional, Says: "when", Blurb: "The conditions an if, a list, a while or a trigger_if checks first."},
	{Key: "alternative_limit", Shape: ShapeTriggers, Class: KeywordClassConditional, Blurb: "A second limit a list falls back to when its limit matches nothing."},
	{Key: "switch", Class: KeywordClassConditional, Blurb: "Reads one trigger and runs the case whose value matches, else the fallback."},
	{Key: "calc_true_if", Shape: ShapeTriggers, Class: KeywordClassConditional, Says: "at least the amount of", Blurb: "True when at least amount of the triggers inside it are true."},
	{Key: "while", Shape: ShapeEffects, Class: KeywordClassFlow, Blurb: "Repeats what is inside it while its limit holds, or count times."},
	{Key: "random", Shape: ShapeEffects, Class: KeywordClassFlow, Blurb: "Runs what is inside it with the chance given, else nothing."},
	{Key: "random_list", Class: KeywordClassFlow, Blurb: "Picks one branch at random; each branch's number is its weight."},
	{Key: "hidden_effect", Shape: ShapeEffects, Class: KeywordClassDisplay, Blurb: "Runs its effects without showing them in the tooltip."},
	{Key: "show_as_tooltip", Shape: ShapeEffects, Class: KeywordClassDisplay, Blurb: "Shows its effects in the tooltip without running them.", Preview: true},
	{Key: "custom_tooltip", Class: KeywordClassDisplay, Covers: true, Values: Localization, Blurb: "Shows a text of your own in place of the tooltip of what is inside."},
	{Key: "custom_description", Class: KeywordClassDisplay, Covers: true, Blurb: "Shows a text of your own in place of the generated description."},
	{Key: "custom_label", Class: KeywordClassDisplay, Values: Localization, Blurb: "Shows a text of your own in the tooltip, with this scope as its subject."},
	{Key: "save_scope_as", Class: KeywordClassSaving, Blurb: "Remembers the current scope under a name for the rest of the event chain."},
	{Key: "save_temporary_scope_as", Class: KeywordClassSaving, Blurb: "Remembers the current scope under a name until this effect ends."},
	{Key: "save_scope_value_as", Class: KeywordClassSaving, Blurb: "Remembers a number or a yes/no under a name for the rest of the event chain."},
	{Key: "save_temporary_scope_value_as", Class: KeywordClassSaving, Blurb: "Remembers a number or a yes/no under a name until this effect ends."},
	{Key: "add_to_list", Class: KeywordClassSaving, Blurb: "Adds the current scope to a named list for the rest of the event chain."},
	{Key: "add_to_temporary_list", Class: KeywordClassSaving, Blurb: "Adds the current scope to a named list until this effect ends."},
	{Key: "exists", Class: KeywordClassSaving, ValueFirst: true, Blurb: "True when the scope named is there and is not nothing."},
	// EU5 in_game/common/subject_types/readme.txt:80 writes it as goal keys,
	// each holding a type and a bias
	{Games: []string{EU5}, Holder: ShapeFields, Key: "ai_biases", Shape: ShapeFields, Blurb: "The weight the AI adds to certain goals, one block for each goal with its type and its bias."},
}

// LimitKey is the key of the conditions an if, a list or a while checks first.
const LimitKey = "limit"

// TriggerKey is the key of the conditions a block holds for itself, such as an
// event's or an option's trigger.
const TriggerKey = "trigger"

// OperatorSays is how a condition in English reads each operator between a key
// and a value.
var OperatorSays = map[string]string{
	"=": "is", "==": "is", "!=": "is not", ">": "more than", ">=": "at least", "<": "less than", "<=": "at most",
	"?=": "is, if it exists",
}

// Bools are the values a yes-or-no is written with.
var Bools = []string{"yes", "no"}

// keywordPositions holds where each keyword sits in Keywords, by the key it
// fits. A keyword written under a command is held by its key without the
// command. A prefix keyword is not held.
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

// LookupKeyword returns the game's keyword for a key in a block of the given
// shape and command, matching the key and the command in any letter case. The
// outer is the Key of the keyword row of the block that holds the key, or ""
// if it has none. Where several fit, it is the first row of Keywords. It
// returns false if none fits.
func (g *Game) LookupKeyword(shape Shape, command, key, outer string) (Keyword, bool) {
	command, key = strings.ToLower(command), strings.ToLower(key)
	candidates := []string{key, AnyKey}
	if jomini.Classify(key).IsNumberKey() {
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
			continues := len(row.Continues) == 0 || slices.Contains(row.Continues, outer)
			if (row.Holder == "" || row.Holder == shape) && (!held || fits) && continues && g.Matches(row.Games) {
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
	return slices.ContainsFunc(Keywords, func(row Keyword) bool { return row.Holder == ShapeValue && row.Key == key })
}

// MatchListIterator returns the prefix keyword, such as every_*, that a key
// starts with. Such a key iterates over a scripted list, and its block runs in
// the list's item scope. It returns false if the key starts with none.
func MatchListIterator(key string) (Keyword, bool) {
	for _, row := range Keywords {
		if prefix, ok := strings.CutSuffix(row.Key, "*"); ok && strings.HasPrefix(key, prefix) {
			return row, true
		}
	}

	return Keyword{}, false
}
