// keywords.go holds the games' dialects and the script language's keywords.

package lang

import (
	"fmt"
	"slices"
	"strings"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/jomini"
)

// Dialect holds, for one game, the facts of the script language that differ
// from game to game.
type Dialect struct {
	ID string

	Events GameEvents

	// TakesModes is whether a mod may write a mode before a definition's
	// name, as REPLACE:x does.
	TakesModes bool

	// LocReplaceFolders are the folders whose localization files replace
	// keys written elsewhere. %s stands for the language. Where two of the
	// folders write the same key, the folder listed first wins.
	LocReplaceFolders []string

	// LogsRepeatedKey is whether the game logs a localization key that a mod
	// and the game both write outside a replace folder.
	LogsRepeatedKey bool

	// DecisionEffects are the effects that run the decision they name. They
	// are none for a game with no such effect.
	DecisionEffects []string

	// BookmarkFolder is the folder of the game's bookmark files. SetupKey is
	// the key of a bookmark whose value is the folder of its start setup.
	// Both are "" for a game with a single start.
	BookmarkFolder string
	SetupKey       string

	// UndeclaredWidgets are the widget types the game draws without
	// declaring them in any file.
	UndeclaredWidgets []string

	// CoatColor is how the game's coats of arms write a color, as
	// rgb { 255 255 255 }.
	CoatColor jomini.ColorForm
}

// Keyword is a fixed word of the script language, such as if, limit or add.
// The walk tries a key as a keyword of its block before it looks it up as a
// name.
type Keyword struct {
	// Key is the keyword in lower case. One that ends in * stands for every
	// key that starts with it, as every_* does. AnyKey stands for any key
	// and jomini.NumberKey for a number.
	Key string

	// Games are the games that have the keyword, all if none.
	Games []string

	Class KeywordClass

	// Parent is the key of the block the keyword is written in. One that
	// ends in * stands for every key that starts with it. It is "" for a
	// keyword written in any block that Holders allows.
	Parent string

	// Holders are the kinds of block the keyword may be written in. With
	// none, a keyword may be written in any block that holds script, and a
	// keyword with a Parent in any block of that key.
	Holders []Shape

	// Shape is what the keyword's own block holds. ShapeEnclosing means it
	// holds what the block around it holds. It is "" for a keyword that
	// takes a value, and for one whose block holds nothing known.
	Shape Shape

	// Follows are the keys the keyword must come right after, as else comes
	// after if. With none, it may come after any key.
	Follows []string

	// Recursive is whether the keyword's block may hold the same keyword
	// again, as Vic3's else in a progress bar does.
	Recursive bool

	// Caller is the kind of place a call written in the keyword's block is
	// made from, which decides the scripted definitions it may call. It is
	// "" where what the block holds decides.
	Caller string

	// Names is the database the keyword's value is a name of, as the value
	// of a switch's trigger key is a trigger's name. It is "" where the
	// value is no name.
	Names string

	// Weighted is whether the keyword's key is a number that is a chance:
	// its share of the numbers written beside it. Preview is whether the
	// keyword's block is only shown in the tooltip.
	Weighted, Preview bool

	// Blurb is the keyword's hover text, shown where no dump gives one.
	Blurb string
}

// KeywordClass is what kind of keyword one is. It is "" for a keyword of no
// kind.
type KeywordClass string

// The classes of keywords.
const (
	KeywordClassLogical     KeywordClass = "logical"
	KeywordClassConditional KeywordClass = "conditional"
	KeywordClassFlow        KeywordClass = "flow"
	KeywordClassDisplay     KeywordClass = "display"
	KeywordClassSaving      KeywordClass = "saving"
	KeywordClassMath        KeywordClass = "math"
	KeywordClassList        KeywordClass = "list"
)

// Shape is what a block holds, which decides how its keys are read.
type Shape string

// The shapes of blocks.
const (
	// ShapeUnknown is a block that may hold fields, commands, links or
	// calls.
	ShapeUnknown Shape = "unknown"

	// ShapeFields is a block of a definition's own fields. ShapeArguments is
	// a block of the keys a command takes.
	ShapeFields    Shape = "fields"
	ShapeArguments Shape = "arguments"

	// ShapeParameters is the block of a call of a scripted definition, which
	// holds only the call's arguments.
	ShapeParameters Shape = "parameters"

	// ShapeEffectArguments is the block of an effect, which holds the keys
	// the effect takes and the effects it runs. ShapeTriggerArguments is the
	// same for a trigger.
	ShapeEffectArguments  Shape = "effect arguments"
	ShapeTriggerArguments Shape = "trigger arguments"

	ShapeTriggers Shape = "triggers"
	ShapeEffects  Shape = "effects"

	// ShapeValue is a block that computes a number.
	ShapeValue Shape = "value"

	// ShapeWidget is the block of a GUI widget: its properties and the
	// widgets inside it.
	ShapeWidget Shape = "widget"

	// ShapeModifiers is a block of modifiers, each a name and a number.
	ShapeModifiers Shape = "modifiers"

	// ShapeDescription is a block that gives a text and the trigger it is
	// shown under.
	ShapeDescription Shape = "description"

	// ShapeSetup is a block of history: the fields its docs write, and
	// effects.
	ShapeSetup Shape = "setup"

	// ShapeEnclosing stands for whatever the block around a keyword holds,
	// as the block of an if holds effects inside effects and triggers inside
	// triggers.
	ShapeEnclosing Shape = "enclosing"
)

var (
	ck3AndEU5  = []string{game.CK3, game.EU5}
	ck3AndVic3 = []string{game.CK3, game.Vic3}
	vic3AndEU5 = []string{game.Vic3, game.EU5}
)

// AnyCall is the caller of a block in which a key may call a scripted
// definition of any kind.
const AnyCall = "*"

// AnyKey matches any key written in a block, such as switch's cases.
const AnyKey = "<any>"

// LimitKey is the key of an if's, a list's or a while's conditions.
const LimitKey = "limit"

// TriggerKey is the key of a block's own conditions, as in an event.
const TriggerKey = "trigger"

// Yes is the language's word for true.
const Yes = "yes"

// Bools are the two values of a yes or no.
var Bools = []string{Yes, "no"}

// The databases the dumps fill, from effects.log, triggers.log,
// event_targets.log, event_scopes.log and modifiers.log. DumpModifiers is
// not named modifiers, since CK3 has a database of that name.
const (
	DumpEffects   = "effects"
	DumpTriggers  = "triggers"
	DumpLinks     = "links"
	DumpTypes     = "types"
	DumpModifiers = "dumped_modifiers"
)

// Dumps lists the databases the dumps fill.
var Dumps = []string{DumpEffects, DumpTriggers, DumpLinks, DumpTypes, DumpModifiers}

// EventNamespaces is the database of the namespaces that events files
// declare.
const EventNamespaces = "namespace"

// The databases of the names script saves, such as scope:x and var:x.
const (
	SavedScopes     = "scope"
	Variables       = "var"
	LocalVariables  = "local_var"
	GlobalVariables = "global_var"
	Lists           = "list"
	Flags           = "flag"
)

// Links are the databases of the names script saves. No file defines those
// names.
var Links = []string{SavedScopes, Variables, LocalVariables, GlobalVariables, Lists, Flags}

// The links to the scopes the walk always has: the scope a definition
// starts in, the scope before the last step, and the current scope. The
// dumps list them as leading to no scope.
const (
	LinkRoot     = "root"
	LinkPrevious = "prev"
	LinkThis     = "this"
)

// dialects holds the dialect of each supported game, by id.
var dialects = map[string]*Dialect{
	game.CK3: {
		ID: game.CK3,
		// _events.info, _event_themes.info
		Events: GameEvents{DefaultScope: "character", Picture: Picture{
			Chains: []PictureChain{
				{
					Steps:   []PictureStep{{Key: "override_background/reference", Database: "event_backgrounds"}},
					FileKey: "background/reference",
				},
				{
					Steps: []PictureStep{
						{Key: "theme", Database: "event_themes"}, {Key: "background/reference", Database: "event_backgrounds"},
					},
					FileKey: "background/reference",
				},
			},
		}},
		// the wiki's Localization page: replace/<language> wins over
		// <language>/replace
		LocReplaceFolders: []string{"localization/replace/%s", "localization/%s/replace"},
		// the wiki's Modding page
		LogsRepeatedKey: true,
		// effects.log: execute_decision
		DecisionEffects:   []string{"execute_decision"},
		UndeclaredWidgets: []string{"zoomarea", "text_occluder"},
		CoatColor:         jomini.ColorForm{Tag: "rgb", Example: []string{"255", "255", "255"}},
	},
	game.Vic3: {
		ID: game.Vic3,
		Events: GameEvents{TypeKey: "type", TypeSuffix: "_event", DefaultScope: "country", Picture: Picture{
			Chains: []PictureChain{{
				Steps:   []PictureStep{{Key: "event_image/video", Database: "gfx/media_aliases"}},
				FileKey: "video", Fallback: "fallback",
			}},
		}},
		TakesModes: true,
		// no source says which of the two folders wins
		LocReplaceFolders: []string{"localization/%s/replace", "localization/replace/%s"},
		UndeclaredWidgets: []string{"overlappingitembox", "treemapchart", "portrait_button", "axis"},
		CoatColor:         jomini.ColorForm{Tag: jomini.TagHSV360, Example: []string{"360", "100", "100"}},
	},
	game.EU5: {
		ID: game.EU5,
		Events: GameEvents{TypeKey: "type", TypeSuffix: "_event", DefaultScope: "country", Picture: Picture{
			FileKey: "image",
			Chains: []PictureChain{{
				Steps: []PictureStep{
					{Key: "illustration_tags", Database: "gfx/illustrations/database", Match: "tags"},
					{Key: "scene", Database: "gfx/scenes"},
					// a scene's backdrop is the layer drawn on background2_mesh
					// (in_game/gfx/scenes/00_scenes_events.txt)
					{Key: "ecs_illustration_image[pdxmesh=background2_mesh]/list", Database: "gfx/images", Match: "tag"},
				},
				FileKey: "illustration_image/texture_file",
			}},
		}},
		TakesModes: true,
		// the wiki's Mod compatibility and Localization pages name only this
		// form
		LocReplaceFolders: []string{"localization/%s/replace"},
		// main_menu/common/bookmarks/00_bookmarks.txt:4
		BookmarkFolder: "common/bookmarks", SetupKey: "setup_folder",
		UndeclaredWidgets: []string{"axis", "plotline", "screen_grab", "newsfeed_carousel"},
		CoatColor:         jomini.ColorForm{Tag: jomini.TagHSV360, Example: []string{"360", "100", "100"}},
	},
}

// LookupDialect returns the dialect of the game with the given id. It
// returns nil for a game the app does not support.
func LookupDialect(id string) *Dialect {
	return dialects[id]
}

// Matches reports whether the dialect's game is among the given games. An
// empty list names every game, even for a nil dialect.
func (d *Dialect) Matches(games []string) bool {
	return len(games) == 0 || d != nil && slices.Contains(games, d.ID)
}

// FindLocReplacement reports whether a localization file of the language,
// by its path from its stage folder, is in one of the game's replace
// folders. It also returns the folder's rank, 0 for the one that wins.
func (d *Dialect) FindLocReplacement(rel, language string) (int, bool) {
	rank := slices.IndexFunc(d.LocReplaceFolders, func(folder string) bool {
		return strings.HasPrefix(rel, fmt.Sprintf(folder, language)+"/")
	})

	return rank, rank >= 0
}

// descBlurb is the hover text of a desc key.
const descBlurb = "The text shown: a localization key, or a block that chooses one."

// customBlurb is the hover text of the custom key of a list.
const customBlurb = "The text shown for the list in the tooltip: a localization key."

// logical returns the row of a keyword that joins triggers, as and does.
func logical(key, blurb string) Keyword {
	return Keyword{Key: key, Class: KeywordClassLogical, Shape: ShapeTriggers, Blurb: blurb}
}

// math returns the row of a keyword that computes in a script value's
// block, as add does.
func math(key, blurb string) Keyword {
	return Keyword{Key: key, Holders: []Shape{ShapeValue}, Class: KeywordClassMath, Shape: ShapeValue, Blurb: blurb}
}

// iterator returns the row of the keys that go through a list, as every_*
// stands for every_vassal and every_courtier.
func iterator(key string, shape Shape, blurb string) Keyword {
	return Keyword{Key: key, Class: KeywordClassList, Shape: shape, Blurb: blurb}
}

// Keywords are the fixed words of the script language. Where several rows
// fit a key, a row with a Parent comes before one without, and then the
// first row wins.
var Keywords = []Keyword{
	logical("and", "True only when everything inside it is true."),
	logical("or", "True when at least one thing inside it is true."),
	logical("not", "True when what is inside it is false."),
	logical("nor", "True only when nothing inside it is true."),
	logical("nand", "True unless everything inside it is true."),
	logical("all_false", "True only when nothing inside it is true (the same as NOR)."),
	logical("any_false", "True when at least one thing inside it is false (the same as NAND)."),
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
	// an else inside a progress_condition takes over when its limit fails,
	// and may hold an else of its own (Vic3 scripted_progress_bars.md)
	{Games: []string{game.Vic3}, Key: "else", Parent: "progress_condition", Class: KeywordClassConditional, Shape: ShapeEnclosing,
		Recursive: true, Blurb: "Takes over when the limit of the progress condition or the else around it fails."},
	{Key: "limit", Class: KeywordClassConditional, Shape: ShapeTriggers, Blurb: "The conditions an if, a list, a while or a trigger_if checks first."},
	{Key: "alternative_limit", Class: KeywordClassConditional, Shape: ShapeTriggers, Blurb: "A second limit a list falls back to when its limit matches nothing."},
	{Key: "calc_true_if", Class: KeywordClassConditional, Shape: ShapeTriggers, Blurb: "True when at least amount of the triggers inside it are true."},

	// a switch is trigger = <a trigger>, a block for each value and
	// fallback = { } (effects.log and triggers.log)
	{Key: "switch", Class: KeywordClassConditional, Shape: ShapeEnclosing,
		Blurb: "Reads one trigger and runs the case whose value matches, else the fallback."},
	{Key: "trigger", Parent: "switch", Class: KeywordClassConditional, Names: DumpTriggers,
		Blurb: "The trigger whose value picks the case."},
	{Key: "fallback", Parent: "switch", Class: KeywordClassConditional, Shape: ShapeEnclosing, Blurb: "Runs when no case matches."},
	{Key: AnyKey, Parent: "switch", Shape: ShapeEnclosing, Blurb: "One case of a switch: runs when the trigger's value is this."},

	{Key: "while", Class: KeywordClassFlow, Shape: ShapeEffects, Blurb: "Repeats what is inside it while its limit holds, or count times."},
	{Key: "random", Class: KeywordClassFlow, Shape: ShapeEffects, Blurb: "Runs what is inside it with the chance given, else nothing."},
	{Key: "random_list", Class: KeywordClassFlow, Blurb: "Picks one branch at random; each branch's number is its weight."},
	{Key: jomini.NumberKey, Parent: "random_list", Shape: ShapeEffects, Caller: AnyCall, Weighted: true,
		Blurb: "One branch of a random list; its number is how likely it is."},
	{Key: "hidden_effect", Class: KeywordClassDisplay, Shape: ShapeEffects, Blurb: "Runs its effects without showing them in the tooltip."},
	{Key: "show_as_tooltip", Class: KeywordClassDisplay, Shape: ShapeEffects, Preview: true, Blurb: "Shows its effects in the tooltip without running them."},

	math("value", "The number a script value starts from, or is set to."),
	{Key: "value", Holders: []Shape{ShapeArguments}, Class: KeywordClassMath, Shape: ShapeValue,
		Blurb: "The number the command takes, or a script value that works it out."},
	math("add", "Adds the number or script value given."),
	math("subtract", "Subtracts the number or script value given."),
	math("multiply", "Multiplies by the number or script value given."),
	math("divide", "Divides by the number or script value given."),
	math("modulo", "Keeps the remainder of dividing by the number given."),
	math("min", "Keeps the result at least the number given."),
	math("max", "Keeps the result at most the number given."),
	math("round", "Rounds the result to the nearest whole number."),
	math("round_to", "Rounds the result to the nearest multiple of the number given."),
	// _script_values.info:38
	math("pow", "Raises the result to the power of the number given."),
	math("ceiling", "Rounds the result up to a whole number."),
	math("floor", "Rounds the result down to a whole number."),
	math("fixed_range", "Keeps the result between the min and max inside it."),
	math("integer_range", "Keeps the result a whole number between the min and max inside it."),
	math("abs", "Drops the result's sign."),
	// _script_values.info:48-49
	{Key: "desc", Holders: []Shape{ShapeValue}, Shape: ShapeDescription, Blurb: descBlurb},
	{Key: "format", Holders: []Shape{ShapeValue}, Shape: ShapeDescription, Blurb: "The text that lays out this block's line when the value is broken down."},
	// EU5 in_game/common/international_organizations/hre.txt:897
	{Key: "desc_sort_order", Holders: []Shape{ShapeValue}, Class: KeywordClassDisplay,
		Blurb: "Where this block's line sorts when the value is broken down: the lowest comes last."},
	{Key: "save_temporary_value_as", Holders: []Shape{ShapeValue}, Class: KeywordClassSaving, Shape: ShapeArguments,
		Blurb: "Remembers a value under a name until this script value ends."},
	// a weight block: CK3 _scripted_modifiers.info:13-22, EU5
	// scripted_modifiers.info:8-22, Vic3 _on_actions.md:50-55
	math("base", "The number a weight starts from."),
	// the CK3 wiki's Weight modifier page
	math("factor", "Multiplies the weight by the number given."),
	{Key: "modifier", Holders: []Shape{ShapeValue}, Shape: ShapeValue, Blurb: "Changes the weight by its add or factor when the triggers inside it hold."},
	// common/character_interactions/00_alliance.txt:648 writes a weight
	// modifier's conditions in it
	{Key: "trigger", Holders: []Shape{ShapeValue}, Shape: ShapeTriggers, Blurb: "The conditions that must hold for the modifier around it to apply."},
	{Key: "opinion_modifier", Holders: []Shape{ShapeValue}, Shape: ShapeArguments, Blurb: "Changes the weight by how much one character likes another."},
	{Key: "compare_modifier", Holders: []Shape{ShapeValue}, Shape: ShapeArguments,
		Blurb: "Changes the weight by how a value of one character compares with another's."},
	// CK3 character_interactions/00_scheme_interactions.txt:2099 and
	// schemes/scheme_types/befriend_scheme.txt:315
	{Games: []string{game.CK3}, Key: "compatibility_modifier", Holders: []Shape{ShapeValue}, Shape: ShapeArguments,
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

	iterator("every_*", ShapeEffects, "Runs the effects inside it on every item of the list named after every_."),
	iterator("random_*", ShapeEffects, "Runs the effects inside it on one random item of the list."),
	iterator("ordered_*", ShapeEffects, "Runs the effects inside it on the items of the list in the order given."),
	iterator("any_*", ShapeTriggers, "True when at least one item of the list passes the triggers inside it."),
	// no dump declares a list's custom text; the games write it inside every_
	// and ordered_ lists only
	{Key: "custom", Parent: "every_*", Class: KeywordClassDisplay, Shape: ShapeDescription, Blurb: customBlurb},
	{Key: "custom", Parent: "ordered_*", Class: KeywordClassDisplay, Shape: ShapeDescription, Blurb: customBlurb},
}

// keywordRows holds the rows of Keywords by their key.
var keywordRows = func() map[string][]Keyword {
	rows := map[string][]Keyword{}
	for _, row := range Keywords {
		rows[row.Key] = append(rows[row.Key], row)
	}

	return rows
}()

// LookupKeyword returns the keyword a key is, in any letter case, when
// written in a block of the given shape and parent key. A keyword whose
// block holds script fits only a key written with a block. Rows such as
// every_* are left to MatchListIterator.
func (d *Dialect) LookupKeyword(shape Shape, parent, key string, block bool) (Keyword, bool) {
	parent, key = strings.ToLower(parent), strings.ToLower(key)
	candidates := []string{key, AnyKey}
	if jomini.Classify(key).IsNumberKey() {
		candidates = []string{key, jomini.NumberKey, AnyKey}
	}

	for _, hasParent := range []bool{true, false} {
		for _, candidate := range candidates {
			for _, row := range keywordRows[candidate] {
				if (row.Parent != "") == hasParent && d.fitsKeyword(row, shape, parent, block) {
					return row, true
				}
			}
		}
	}

	return Keyword{}, false
}

// ListKeywords returns the keywords that may be written in a block of the
// given shape and parent key, in the order of Keywords. Rows that stand for
// many keys, as every_* does, are left out.
func (d *Dialect) ListKeywords(shape Shape, parent string) []Keyword {
	parent = strings.ToLower(parent)

	var rows []Keyword
	for _, row := range Keywords {
		isLiteral := !strings.HasSuffix(row.Key, "*") && row.Key != AnyKey && row.Key != jomini.NumberKey
		if isLiteral && d.fitsKeyword(row, shape, parent, true) {
			rows = append(rows, row)
		}
	}

	return rows
}

// scriptShapes are the kinds of block that hold script. A keyword that
// lists no holders may be written in any of them.
var scriptShapes = []Shape{
	ShapeTriggers, ShapeEffects, ShapeValue, ShapeSetup, ShapeUnknown,
	ShapeArguments, ShapeEffectArguments, ShapeTriggerArguments,
}

// fitsKeyword reports whether a row holds for the game and may be written
// in a block of the given shape and lower-case parent key, with a block or
// with a value as block says.
func (d *Dialect) fitsKeyword(row Keyword, shape Shape, parent string, block bool) bool {
	if !d.Matches(row.Games) {
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

	// a keyword whose block holds script is never written with a value, so
	// EU5's limit = janissary_unit_limit is a field and no keyword
	holdsScript := row.Shape == ShapeTriggers || row.Shape == ShapeEffects || row.Shape == ShapeEnclosing

	return block || !holdsScript
}

// listIterators are the rows of Keywords that stand for every key that
// starts with them, as every_* does.
var listIterators = func() []Keyword {
	var rows []Keyword
	for _, row := range Keywords {
		if strings.HasSuffix(row.Key, "*") {
			rows = append(rows, row)
		}
	}

	return rows
}()

// MatchListIterator returns the keyword, such as every_*, that a key starts
// with. The block of such a key runs once for each item of a list, in that
// item's scope. It returns false if the key starts with none.
func MatchListIterator(key string) (Keyword, bool) {
	for _, row := range listIterators {
		if strings.HasPrefix(key, strings.TrimSuffix(row.Key, "*")) {
			return row, true
		}
	}

	return Keyword{}, false
}
