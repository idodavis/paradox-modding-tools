// language.go holds the script language's own rules: shapes and their key
// rules, the keywords, and the names script saves and reads back.

package game

import (
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

// KeyRule is one way a block reads a key that is no keyword: as a call, a
// field, a command, a link and so on.
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

// Keyword is a fixed word of the script language, such as if, limit, and or
// add. It means the same in every game. The walk recognizes it by the block
// it is written in, before it looks the key up as a name. Where the dumps
// also list it, as they list if among the effects, their entry gives its
// text, usage and scopes.
type Keyword struct {
	// Key is the keyword in lower case. One that ends in * is a prefix, as
	// every_* is of every_vassal. AnyKey stands for any key and
	// jomini.NumberKey for a number.
	Key string

	// Games are the games that have it, every game if none.
	Games []string

	Class KeywordClass

	// Parent is the key whose block it is written in, as random_list is for
	// a numbered branch. One that ends in * is a prefix. It is "" for a
	// keyword written in any block its Holders name.
	Parent string

	// Holders are the shapes of the blocks it is written in. For a keyword
	// with no Parent, none means any block that holds script. For one with a
	// Parent, none means any block.
	Holders []Shape

	// Shape is what its own block holds, or ShapeEnclosing for what the
	// block around it holds. It is "" for a keyword whose block has no shape
	// of its own, and for one written with a value.
	Shape Shape

	// Follows are the keys one of which is written just before it, as if
	// or else_if before else. None means any.
	Follows []string

	// Recursive is whether its block may hold the same keyword again, as
	// Vic3's else chain in a progress bar does.
	Recursive bool

	// Caller is the caller a call in its block goes through, "" for the
	// block's own.
	Caller string

	// Weighted is whether its key is a number whose share of its siblings'
	// is its chance. Preview is whether its block is shown in the tooltip
	// and never run.
	Weighted, Preview bool

	// Blurb is the hover text where no dump describes it.
	Blurb string
}

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

// Shapes is the shapes a block can hold, each with how it reads a key that
// is no keyword. A description block's keys are all keywords.
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
		KeyRules: []KeyRule{KeyRuleKnownCall, KeyRuleDeclared, KeyRuleCommand, KeyRuleObserved, KeyRuleLink, KeyRuleCall},
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

// scriptShapes are the shapes of the blocks that hold script, where a
// keyword with no Parent and no Holders is written.
var scriptShapes = []Shape{
	ShapeTriggers, ShapeEffects, ShapeValue, ShapeSetup, ShapeUnknown,
	ShapeArguments, ShapeEffectArguments, ShapeTriggerArguments,
}

// descBlurb is the sentence shown for a description, which a value block's
// desc shares.
const descBlurb = "The text shown: a localization key, or a block that chooses one."

// customBlurb is the sentence shown for a list's custom text.
const customBlurb = "The text shown for the list in the tooltip: a localization key."

// Keywords are the fixed words of the script language. Where several rows
// fit a key, one written under the key's Parent comes before one written in
// any block, and then the first row wins.
var Keywords = []Keyword{
	{Key: "and", Class: KeywordClassLogical, Shape: ShapeTriggers, Blurb: "True only when everything inside it is true."},
	{Key: "or", Class: KeywordClassLogical, Shape: ShapeTriggers, Blurb: "True when at least one thing inside it is true."},
	{Key: "not", Class: KeywordClassLogical, Shape: ShapeTriggers, Blurb: "True when what is inside it is false."},
	{Key: "nor", Class: KeywordClassLogical, Shape: ShapeTriggers, Blurb: "True only when nothing inside it is true."},
	{Key: "nand", Class: KeywordClassLogical, Shape: ShapeTriggers, Blurb: "True unless everything inside it is true."},
	{Key: "all_false", Class: KeywordClassLogical, Shape: ShapeTriggers, Blurb: "True only when nothing inside it is true (the same as NOR)."},
	{Key: "any_false", Class: KeywordClassLogical, Shape: ShapeTriggers, Blurb: "True when at least one thing inside it is false (the same as NAND)."},
	{Key: "always", Class: KeywordClassLogical, Blurb: "True or false as written: always = yes is always true."},

	{Key: "if", Class: KeywordClassConditional, Shape: ShapeEnclosing, Blurb: "Runs what is inside it when its limit holds."},
	{Key: "else_if", Class: KeywordClassConditional, Shape: ShapeEnclosing, Follows: []string{"if", "else_if"},
		Blurb: "Runs when the if or else_if before it did not, and its own limit holds."},
	{Key: "else", Class: KeywordClassConditional, Shape: ShapeEnclosing, Follows: []string{"if", "else_if"},
		Blurb: "Runs when no if or else_if before it ran."},
	{Key: "trigger_if", Class: KeywordClassConditional, Shape: ShapeEnclosing, Blurb: "Checks what is inside it only when its limit holds."},
	{Key: "trigger_else_if", Class: KeywordClassConditional, Shape: ShapeEnclosing, Follows: []string{"trigger_if", "trigger_else_if"},
		Blurb: "Checks what is inside it when the trigger_if before it was skipped and its own limit holds."},
	{Key: "trigger_else", Class: KeywordClassConditional, Shape: ShapeEnclosing, Follows: []string{"trigger_if", "trigger_else_if"},
		Blurb: "Checks what is inside it when every trigger_if before it was skipped."},
	// Vic3 common/scripted_progress_bars/scripted_progress_bars.md: an else
	// inside a progress_condition takes over when its limit fails, "chained
	// as deep as you need", so it is written inside the block, after its
	// body, and the else of such an else inside it in the same way
	{Games: []string{Vic3}, Key: "else", Parent: "progress_condition", Class: KeywordClassConditional, Shape: ShapeEnclosing,
		Recursive: true, Blurb: "Takes over when the limit of the progress condition or the else around it fails."},
	{Key: "limit", Class: KeywordClassConditional, Shape: ShapeTriggers, Blurb: "The conditions an if, a list, a while or a trigger_if checks first."},
	{Key: "alternative_limit", Class: KeywordClassConditional, Shape: ShapeTriggers, Blurb: "A second limit a list falls back to when its limit matches nothing."},
	{Key: "calc_true_if", Class: KeywordClassConditional, Shape: ShapeTriggers, Blurb: "True when at least amount of the triggers inside it are true."},

	// effects.log and triggers.log of the three games write a switch as
	// trigger = <a trigger>, a block for each value, and fallback = { }
	{Key: "switch", Class: KeywordClassConditional, Shape: ShapeEnclosing,
		Blurb: "Reads one trigger and runs the case whose value matches, else the fallback."},
	{Key: "trigger", Parent: "switch", Class: KeywordClassConditional, Blurb: "The trigger whose value picks the case."},
	{Key: "fallback", Parent: "switch", Class: KeywordClassConditional, Shape: ShapeEnclosing, Blurb: "Runs when no case matches."},
	{Key: AnyKey, Parent: "switch", Shape: ShapeEnclosing, Blurb: "One case of a switch: runs when the trigger's value is this."},

	{Key: "while", Class: KeywordClassFlow, Shape: ShapeEffects, Blurb: "Repeats what is inside it while its limit holds, or count times."},
	{Key: "random", Class: KeywordClassFlow, Shape: ShapeEffects, Blurb: "Runs what is inside it with the chance given, else nothing."},
	{Key: "random_list", Class: KeywordClassFlow, Blurb: "Picks one branch at random; each branch's number is its weight."},
	{Key: jomini.NumberKey, Parent: "random_list", Shape: ShapeEffects, Caller: AnyCall, Weighted: true,
		Blurb: "One branch of a random list; its number is how likely it is."},
	{Key: "hidden_effect", Class: KeywordClassDisplay, Shape: ShapeEffects, Blurb: "Runs its effects without showing them in the tooltip."},
	{Key: "show_as_tooltip", Class: KeywordClassDisplay, Shape: ShapeEffects, Preview: true, Blurb: "Shows its effects in the tooltip without running them."},

	{Key: "value", Holders: []Shape{ShapeValue}, Class: KeywordClassMath, Shape: ShapeValue, Blurb: "The number a script value starts from, or is set to."},
	{Key: "value", Holders: []Shape{ShapeArguments}, Class: KeywordClassMath, Shape: ShapeValue,
		Blurb: "The number the command takes, or a script value that works it out."},
	{Key: "add", Holders: []Shape{ShapeValue}, Class: KeywordClassMath, Shape: ShapeValue, Blurb: "Adds the number or script value given."},
	{Key: "subtract", Holders: []Shape{ShapeValue}, Class: KeywordClassMath, Shape: ShapeValue, Blurb: "Subtracts the number or script value given."},
	{Key: "multiply", Holders: []Shape{ShapeValue}, Class: KeywordClassMath, Shape: ShapeValue, Blurb: "Multiplies by the number or script value given."},
	{Key: "divide", Holders: []Shape{ShapeValue}, Class: KeywordClassMath, Shape: ShapeValue, Blurb: "Divides by the number or script value given."},
	{Key: "modulo", Holders: []Shape{ShapeValue}, Class: KeywordClassMath, Shape: ShapeValue, Blurb: "Keeps the remainder of dividing by the number given."},
	{Key: "min", Holders: []Shape{ShapeValue}, Class: KeywordClassMath, Shape: ShapeValue, Blurb: "Keeps the result at least the number given."},
	{Key: "max", Holders: []Shape{ShapeValue}, Class: KeywordClassMath, Shape: ShapeValue, Blurb: "Keeps the result at most the number given."},
	{Key: "round", Holders: []Shape{ShapeValue}, Class: KeywordClassMath, Shape: ShapeValue, Blurb: "Rounds the result to the nearest whole number."},
	{Key: "round_to", Holders: []Shape{ShapeValue}, Class: KeywordClassMath, Shape: ShapeValue, Blurb: "Rounds the result to the nearest multiple of the number given."},
	// _script_values.info:38
	{Key: "pow", Holders: []Shape{ShapeValue}, Class: KeywordClassMath, Shape: ShapeValue, Blurb: "Raises the result to the power of the number given."},
	{Key: "ceiling", Holders: []Shape{ShapeValue}, Class: KeywordClassMath, Shape: ShapeValue, Blurb: "Rounds the result up to a whole number."},
	{Key: "floor", Holders: []Shape{ShapeValue}, Class: KeywordClassMath, Shape: ShapeValue, Blurb: "Rounds the result down to a whole number."},
	{Key: "fixed_range", Holders: []Shape{ShapeValue}, Class: KeywordClassMath, Shape: ShapeValue, Blurb: "Keeps the result between the min and max inside it."},
	{Key: "integer_range", Holders: []Shape{ShapeValue}, Class: KeywordClassMath, Shape: ShapeValue,
		Blurb: "Keeps the result a whole number between the min and max inside it."},
	{Key: "abs", Holders: []Shape{ShapeValue}, Class: KeywordClassMath, Shape: ShapeValue, Blurb: "Drops the result's sign."},
	// _script_values.info:48-49
	{Key: "desc", Holders: []Shape{ShapeValue}, Shape: ShapeDescription, Blurb: descBlurb},
	{Key: "format", Holders: []Shape{ShapeValue}, Shape: ShapeDescription, Blurb: "The text that lays out this block's line when the value is broken down."},
	// EU5 in_game/common/international_organizations/hre.txt:897,
	// "desc_sort_order = -99999 # Make sure we are last"
	{Key: "desc_sort_order", Holders: []Shape{ShapeValue}, Class: KeywordClassDisplay,
		Blurb: "Where this block's line sorts when the value is broken down: the lowest comes last."},
	{Key: "save_temporary_value_as", Holders: []Shape{ShapeValue}, Class: KeywordClassSaving, Shape: ShapeArguments,
		Blurb: "Remembers a value under a name until this script value ends."},
	// a weight block: CK3 _scripted_modifiers.info:13-22, EU5
	// scripted_modifiers.info:8-22, Vic3 _on_actions.md:50-55
	{Key: "base", Holders: []Shape{ShapeValue}, Class: KeywordClassMath, Shape: ShapeValue, Blurb: "The number a weight starts from."},
	// the CK3 wiki's Weight modifier page
	{Key: "factor", Holders: []Shape{ShapeValue}, Class: KeywordClassMath, Shape: ShapeValue, Blurb: "Multiplies the weight by the number given."},
	{Key: "modifier", Holders: []Shape{ShapeValue}, Shape: ShapeValue, Blurb: "Changes the weight by its add or factor when the triggers inside it hold."},
	// common/character_interactions/00_alliance.txt:648 writes a weight
	// modifier's conditions in it
	{Key: "trigger", Holders: []Shape{ShapeValue}, Shape: ShapeTriggers, Blurb: "The conditions that must hold for the modifier around it to apply."},
	{Key: "opinion_modifier", Holders: []Shape{ShapeValue}, Shape: ShapeArguments, Blurb: "Changes the weight by how much one character likes another."},
	{Key: "compare_modifier", Holders: []Shape{ShapeValue}, Shape: ShapeArguments,
		Blurb: "Changes the weight by how a value of one character compares with another's."},
	// CK3 character_interactions/00_scheme_interactions.txt:2099 and
	// schemes/scheme_types/befriend_scheme.txt:315
	{Games: []string{CK3}, Key: "compatibility_modifier", Holders: []Shape{ShapeValue}, Shape: ShapeArguments,
		Blurb: "Changes the weight by how well two characters' traits get along."},

	{Key: "first_valid", Holders: []Shape{ShapeDescription}, Class: KeywordClassDisplay, Shape: ShapeDescription,
		Blurb: "Shows the first text inside it whose trigger holds."},
	{Key: "random_valid", Holders: []Shape{ShapeDescription}, Class: KeywordClassDisplay, Shape: ShapeDescription,
		Blurb: "Shows one of the texts inside it whose trigger holds, at random."},
	{Key: "triggered_desc", Holders: []Shape{ShapeDescription}, Class: KeywordClassDisplay, Shape: ShapeDescription,
		Blurb: "A text shown only when its trigger holds."},
	{Key: "trigger", Holders: []Shape{ShapeDescription}, Shape: ShapeTriggers, Blurb: "The conditions that must hold for the block around it to count."},
	// a scalar desc is the text itself
	{Key: "desc", Holders: []Shape{ShapeDescription}, Shape: ShapeDescription, Blurb: descBlurb},

	{Key: "every_*", Class: KeywordClassList, Shape: ShapeEffects, Blurb: "Runs the effects inside it on every item of the list named after every_."},
	{Key: "random_*", Class: KeywordClassList, Shape: ShapeEffects, Blurb: "Runs the effects inside it on one random item of the list."},
	{Key: "ordered_*", Class: KeywordClassList, Shape: ShapeEffects, Blurb: "Runs the effects inside it on the items of the list in the order given."},
	{Key: "any_*", Class: KeywordClassList, Shape: ShapeTriggers, Blurb: "True when at least one item of the list passes the triggers inside it."},
	// no dump declares a list's custom text; the games write it inside every_
	// and ordered_ lists only
	{Key: "custom", Parent: "every_*", Class: KeywordClassDisplay, Shape: ShapeDescription, Blurb: customBlurb},
	{Key: "custom", Parent: "ordered_*", Class: KeywordClassDisplay, Shape: ShapeDescription, Blurb: customBlurb},
}

// LimitKey is the key of the conditions an if, a list or a while checks first.
const LimitKey = "limit"

// TriggerKey is the key of the conditions a block holds for itself, such as an
// event's or an option's trigger.
const TriggerKey = "trigger"

// Bools are the values a yes-or-no is written with.
var Bools = []string{"yes", "no"}

// keywordRows holds the rows of Keywords by their Key, in the table's order.
var keywordRows = func() map[string][]Keyword {
	rows := map[string][]Keyword{}
	for _, row := range Keywords {
		rows[row.Key] = append(rows[row.Key], row)
	}

	return rows
}()

// LookupKeyword returns the keyword a key is in a block of the given shape,
// written under the given key, matching both keys in any letter case. A
// keyword whose block holds triggers, effects or what the block around it
// holds is one only where it is written with a block, as the flag says. A
// prefix keyword is never returned; MatchListIterator finds those. It returns
// false if the key is no keyword there.
func (g *Game) LookupKeyword(shape Shape, parent, key string, block bool) (Keyword, bool) {
	parent, key = strings.ToLower(parent), strings.ToLower(key)
	candidates := []string{key, AnyKey}
	if jomini.Classify(key).IsNumberKey() {
		candidates = []string{key, jomini.NumberKey, AnyKey}
	}

	// A row written under the key above is closer than one written in any
	// block.
	for _, held := range []bool{true, false} {
		for _, candidate := range candidates {
			for _, row := range keywordRows[candidate] {
				if (row.Parent != "") == held && g.fitsKeyword(row, shape, parent, block) {
					return row, true
				}
			}
		}
	}

	return Keyword{}, false
}

// ListKeywords returns the keywords that may be written in a block of the
// given shape under the given key, in the table's order, without the prefixes
// and the rows for any key or a number.
func (g *Game) ListKeywords(shape Shape, parent string) []Keyword {
	parent = strings.ToLower(parent)

	var rows []Keyword
	for _, row := range Keywords {
		named := !strings.HasSuffix(row.Key, "*") && row.Key != AnyKey && row.Key != jomini.NumberKey
		if named && g.fitsKeyword(row, shape, parent, true) {
			rows = append(rows, row)
		}
	}

	return rows
}

// fitsKeyword reports whether a row of Keywords holds for the game and may be
// written in a block of the given shape under the given key, in lower case,
// with a block or with a value as the flag says.
func (g *Game) fitsKeyword(row Keyword, shape Shape, parent string, block bool) bool {
	if !g.Matches(row.Games) {
		return false
	}

	prefix, isPrefix := strings.CutSuffix(row.Parent, "*")
	switch {
	case row.Parent == "":
	case isPrefix && strings.HasPrefix(parent, prefix):
	case row.Parent == parent:
	default:
		return false
	}

	holders := row.Holders
	if row.Parent == "" && len(holders) == 0 {
		holders = scriptShapes
	}

	if len(holders) > 0 && !slices.Contains(holders, shape) {
		return false
	}

	// A keyword that holds script is never written with a value, so a field
	// of its key, as EU5's limit = janissary_unit_limit, is no keyword.
	holdsScript := row.Shape == ShapeTriggers || row.Shape == ShapeEffects || row.Shape == ShapeEnclosing

	return block || !holdsScript
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
