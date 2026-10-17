// lang-rules.go is Paradox script's language rules as data, the same for every game and version,
// and each game's file Layout. The schema's walker (schema/walk.go) reads statements by these tables and
// the walker's own rules.

package game

import (
	"slices"
	"strings"
)

// ---- Blocks: what a block holds, and how a key written in it is read.

// Shape is the kind of content a block holds.
type Shape uint8

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
	ShapeEnclosing                // a construct's or a claim's value for "the holder's shape"
)

// KeyRule is one way a key can be read; a block tries its rules in order and the first that fits wins.
type KeyRule uint8

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

// Where is where a key's block reads, by the rule that read the key.
type Where uint8

// The places a key's block can read.
const (
	AtChild  Where = iota // the position under the holder by the key's written form, made when the walk creates
	AtCallee              // the scripted entry's parameters, else its database's entry
	AtArgs                // the declared command's arguments
	AtHolder              // where the key stands: a link
	AtNone                // nowhere: a key no rule places
)

// HoldsFrom is what decides what a key's block holds.
type HoldsFrom uint8

// The deciders of a block's shape.
const (
	HoldsClaim     HoldsFrom = iota // the position's claim, else its evidence
	HoldsCommand                    // the command's kinds and the holder's shape
	HoldsSame                       // the holder's shape
	HoldsArguments                  // the command's arguments
)

// ScopeVerb is how a key's block gets its scope.
type ScopeVerb uint8

// The verbs a block's scope follows.
const (
	ScopeStays   ScopeVerb = iota // the holder's scope when the block's shape is known, else unknown
	ScopeFollows                  // the command's or link's target when it is a scope that runs script, else unknown
	ScopeUnknown
)

// KeyRuleRow is the three consequences of reading a key by a rule: where its block reads, what decides what it
// holds, and how its scope follows. Whether the rule fits is the walker's test of the rule's name (schema/walk.go).
type KeyRuleRow struct {
	At    Where
	Holds HoldsFrom
	Scope ScopeVerb
}

// KeyRules is one row per rule.
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

// ShapeRule is how a block of one shape reads.
type ShapeRule struct {
	Keys   []KeyRule // the rules a key is tried by, in order
	Caller string    // the word a call here is resolved by; AnyCall for every scripted folder
	Allows Kinds     // the commands it holds; inside a trigger, never an effect
}

// AnyCall resolves a call in every scripted folder that has a CalledIn.
const AnyCall = "*"

// The CalledIn words: a scripted folder is called from the blocks that hold its kind.
const (
	callTrigger = "trigger"
	callEffect  = "effect"
	callValue   = "value"
)

// Shapes is the rule of each shape of block: the key rules it tries, the word its calls resolve by, the commands
// it allows. A description block reads its keys by the construct rows alone.
var Shapes = map[Shape]ShapeRule{
	ShapeFields:      {[]KeyRule{KeyRulePosition}, "", 0},
	ShapeTriggers:    {[]KeyRule{KeyRuleKnownCall, KeyRuleDeclared, KeyRuleCommand, KeyRuleLink, KeyRuleObserved, KeyRuleCall}, callTrigger, KindTrigger},
	ShapeEffects:     {[]KeyRule{KeyRuleKnownCall, KeyRuleDeclared, KeyRuleCommand, KeyRuleLink, KeyRuleObserved, KeyRuleCall}, callEffect, KindEffect},
	ShapeValue:       {[]KeyRule{KeyRuleKnownCall, KeyRuleDeclared, KeyRuleCommand, KeyRuleOperator, KeyRuleLink, KeyRuleObserved, KeyRuleCall}, callValue, KindTrigger | KindEffect},
	ShapeArguments:   {[]KeyRule{KeyRuleKnownCall, KeyRuleDeclared, KeyRuleCommand, KeyRuleObserved, KeyRuleCall}, AnyCall, 0},
	ShapeUnknown:     {[]KeyRule{KeyRuleKnownCall, KeyRuleDeclared, KeyRuleCommand, KeyRuleLink, KeyRulePositionOrCall}, AnyCall, KindTrigger | KindEffect},
	ShapeSetup:       {[]KeyRule{KeyRuleDeclared, KeyRuleDocumented, KeyRuleKnownCall, KeyRuleCommand, KeyRuleObserved, KeyRuleLink, KeyRuleCall}, callEffect, KindEffect},
	ShapeWidget:      {[]KeyRule{KeyRuleKnownCall, KeyRulePosition}, "widget", 0},
	ShapeDescription: {nil, "", 0},
}

// ---- Commands: what a declared command is, and what its block holds.

// Kinds is what a declared command name is, as bits; a name can be more than one.
type Kinds uint8

// The kinds a declared command can be.
const (
	KindEffect Kinds = 1 << iota
	KindTrigger
	KindLink
)

// KindWords are the dumps' words for each kind, in the order a hover card lists them.
var KindWords = []struct {
	Kind Kinds
	Word string
}{{KindEffect, "effect"}, {KindTrigger, "trigger"}, {KindLink, "event target"}}

// Construct is a key of the language itself: one it gives a fixed shape to, or one of its operators, which the
// dumps declare as commands. A row fits inside one command's block (Command; "" any), within a holder of one shape
// (Within; ShapeUnknown any), a key by its written form (Key: <number>, <any>, a name, lower-case) or by its prefix
// (Prefix), read as a call that iterates the scripted list the rest of the key names. Holds is the shape its block
// holds, ShapeEnclosing the holder's, ShapeUnknown the shape the key's rule gives; Scope how its block's scope
// follows; Caller the word its calls resolve by, "" the shape's own; Leaf marks a description's leaf, read at the
// position the description belongs to and counted nowhere; Class is the operator it is, ClassNone for none. The
// first row that fits wins.
type Construct struct {
	Command string
	Within  Shape
	Key     string
	Prefix  bool
	Holds   Shape
	Scope   ScopeVerb
	Caller  string
	Leaf    bool
	Class   Class
}

// Class is what one of the language's operators does.
type Class uint8

// The classes.
const (
	ClassNone        Class = iota
	ClassLogical           // combines conditions, or is a fixed one: AND, OR, NOT, always
	ClassConditional       // runs or holds its block on a condition: if, limit, switch
	ClassFlow              // repeats or picks at random: while, random, random_list
	ClassList              // runs its block for the scopes of a list: every_, any_, random_, ordered_
	ClassMath              // computes a script value: add, multiply, min
	ClassDisplay           // shapes what a tooltip or description shows: hidden_effect, custom_tooltip, first_valid
	ClassSaving            // saves a scope, value or list by name, or tests one is there: save_scope_as, exists
)

// AnyKey is the written form every key of a block reads at (switch's cases).
const AnyKey = "<any>"

// The words of a dynamic description: its markers, its trigger and its leaf.
const (
	descFirstValid  = "first_valid"
	descRandomValid = "random_valid"
	descTriggered   = "triggered_desc"
	descTrigger     = "trigger"
	descLeaf        = "desc"
)

// Constructs are random_list's weighted branches, switch's cases, the list iterators, a dynamic description's
// markers, trigger and leaf, a value block's operators, a widget's blocks, then the language's operators the dumps
// declare as commands, which name no shape: the walker reads those as the dumps declare them.
var Constructs = []Construct{
	{Command: "random_list", Key: "<number>", Holds: ShapeEffects, Caller: AnyCall},
	{Command: "switch", Key: AnyKey, Holds: ShapeEnclosing},
	{Prefix: true, Key: "every_", Holds: ShapeEffects, Scope: ScopeUnknown, Class: ClassList},
	{Prefix: true, Key: "random_", Holds: ShapeEffects, Scope: ScopeUnknown, Class: ClassList},
	{Prefix: true, Key: "ordered_", Holds: ShapeEffects, Scope: ScopeUnknown, Class: ClassList},
	{Prefix: true, Key: "any_", Holds: ShapeTriggers, Scope: ScopeUnknown, Class: ClassList},
	{Within: ShapeDescription, Key: descFirstValid, Holds: ShapeDescription, Class: ClassDisplay},
	{Within: ShapeDescription, Key: descRandomValid, Holds: ShapeDescription, Class: ClassDisplay},
	{Within: ShapeDescription, Key: descTriggered, Holds: ShapeDescription, Class: ClassDisplay},
	{Within: ShapeDescription, Key: descTrigger, Holds: ShapeTriggers},
	{Within: ShapeDescription, Key: descLeaf, Leaf: true},
	{Within: ShapeValue, Key: "value", Holds: ShapeValue, Class: ClassMath},
	{Within: ShapeValue, Key: "add", Holds: ShapeValue, Class: ClassMath},
	{Within: ShapeValue, Key: "subtract", Holds: ShapeValue, Class: ClassMath},
	{Within: ShapeValue, Key: "multiply", Holds: ShapeValue, Class: ClassMath},
	{Within: ShapeValue, Key: "divide", Holds: ShapeValue, Class: ClassMath},
	{Within: ShapeValue, Key: "modulo", Holds: ShapeValue, Class: ClassMath},
	{Within: ShapeValue, Key: "min", Holds: ShapeValue, Class: ClassMath},
	{Within: ShapeValue, Key: "max", Holds: ShapeValue, Class: ClassMath},
	{Within: ShapeValue, Key: "round", Holds: ShapeValue, Class: ClassMath},
	{Within: ShapeValue, Key: "round_to", Holds: ShapeValue, Class: ClassMath},
	{Within: ShapeValue, Key: "ceiling", Holds: ShapeValue, Class: ClassMath},
	{Within: ShapeValue, Key: "floor", Holds: ShapeValue, Class: ClassMath},
	{Within: ShapeValue, Key: "fixed_range", Holds: ShapeValue, Class: ClassMath},
	{Within: ShapeValue, Key: "integer_range", Holds: ShapeValue, Class: ClassMath},
	{Within: ShapeValue, Key: "abs", Holds: ShapeValue, Class: ClassMath},
	{Within: ShapeValue, Key: descLeaf, Holds: ShapeArguments},
	{Within: ShapeValue, Key: "format", Holds: ShapeArguments},
	{Within: ShapeValue, Key: "save_temporary_value_as", Holds: ShapeArguments, Class: ClassSaving},
	{Within: ShapeWidget, Key: AnyKey, Holds: ShapeWidget},
	{Key: "and", Class: ClassLogical},
	{Key: "or", Class: ClassLogical},
	{Key: "not", Class: ClassLogical},
	{Key: "nor", Class: ClassLogical},
	{Key: "nand", Class: ClassLogical},
	{Key: "all_false", Class: ClassLogical},
	{Key: "any_false", Class: ClassLogical},
	{Key: "always", Class: ClassLogical},
	{Key: "if", Class: ClassConditional},
	{Key: "else_if", Class: ClassConditional},
	{Key: "else", Class: ClassConditional},
	{Key: "trigger_if", Class: ClassConditional},
	{Key: "trigger_else_if", Class: ClassConditional},
	{Key: "trigger_else", Class: ClassConditional},
	{Key: "limit", Class: ClassConditional},
	{Key: "alternative_limit", Class: ClassConditional},
	{Key: "switch", Class: ClassConditional},
	{Key: "calc_true_if", Class: ClassConditional},
	{Key: "while", Class: ClassFlow},
	{Key: "random", Class: ClassFlow},
	{Key: "random_list", Class: ClassFlow},
	{Key: "hidden_effect", Class: ClassDisplay},
	{Key: "show_as_tooltip", Class: ClassDisplay},
	{Key: "custom_tooltip", Class: ClassDisplay},
	{Key: "custom_description", Class: ClassDisplay},
	{Key: "custom_label", Class: ClassDisplay},
	{Key: "save_scope_as", Class: ClassSaving},
	{Key: "save_temporary_scope_as", Class: ClassSaving},
	{Key: "save_scope_value_as", Class: ClassSaving},
	{Key: "save_temporary_scope_value_as", Class: ClassSaving},
	{Key: "add_to_list", Class: ClassSaving},
	{Key: "add_to_temporary_list", Class: ClassSaving},
	{Key: "exists", Class: ClassSaving},
}

// ConstructOf returns the first construct row that fits a key written in a block of a shape inside a command's
// block, by the key's position name; a prefix row never fits here (the walker reads it for a call).
func ConstructOf(shape Shape, command, key string) (Construct, bool) {
	for _, row := range Constructs {
		fitsHolder := (row.Command == "" || row.Command == command) && (row.Within == ShapeUnknown || row.Within == shape)
		if fitsHolder && !row.Prefix && (row.Key == key || row.Key == AnyKey) {
			return row, true
		}
	}
	return Construct{}, false
}

// IsOperator reports whether a key, as written, is a value block's operator: a construct within a value.
func IsOperator(key string) bool {
	return slices.ContainsFunc(Constructs, func(row Construct) bool { return row.Within == ShapeValue && row.Key == key })
}

// ListIterator returns the construct row whose prefix a key starts with: a call that iterates a scripted list.
func ListIterator(key string) (Construct, bool) {
	for _, row := range Constructs {
		if row.Prefix && strings.HasPrefix(key, row.Key) {
			return row, true
		}
	}
	return Construct{}, false
}

// ClassOf returns the class of the operator a key is by its written form where it is written, the key in lower case
// since the games write AND and and alike: the class of the first construct that fits it. A list iterator's prefix
// is no class by itself (a scripted trigger may be named any_x): lsp's tokens read it where the walker reads a call.
func ClassOf(shape Shape, command, key string) Class {
	construct, _ := ConstructOf(shape, command, strings.ToLower(key))
	return construct.Class
}

// ---- Names: what a written word names: the name commands, the saved-scope and variable databases and the list
// iterators; the rules themselves are the schema's (schema/occurrences.go).

// The databases of the names script saves and reads back by a link: saved scopes (save_scope_as, scope:x) and
// variables, on a scope (set_variable, var:x), for the effect being run (set_local_variable, local_var:x) or for the
// game (set_global_variable, global_var:x).
const (
	SavedScopes     = "scope"
	Variables       = "var"
	LocalVariables  = "local_var"
	GlobalVariables = "global_var"
)

// ScopeLink is a link that leads by where script stands, not by a declared target: to the scope the entry runs in,
// the scope before this one, this one again, or one saved by name (scope:x).
type ScopeLink uint8

// The scope links.
const (
	LinkRoot ScopeLink = iota + 1
	LinkPrev
	LinkThis
	LinkSaved
)

// ScopeLinks are the scope links by how they are written.
var ScopeLinks = map[string]ScopeLink{"root": LinkRoot, "prev": LinkPrev, "this": LinkThis, SavedScopes: LinkSaved}

// NameCommand is a command whose value is a name script makes or reads, and the name's database;
// the link with the same name reads it back (scope:x, var:x).
type NameCommand struct {
	Database string
	Makes    bool // it makes the name; else it reads one
}

// NameCommands are the name commands, by name, or command/key for a key inside the command's block.
var NameCommands = map[string]NameCommand{
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

// ---- Descriptions.

// DynamicDescription is the words of a dynamic description, as the pages read one (the walker reads them as
// construct rows). Triggered shows its leaf only while its trigger holds; Random shows one of its own at random,
// any other marker the first whose trigger holds.
var DynamicDescription = struct {
	Markers                          []string
	Trigger, Leaf, Triggered, Random string
}{[]string{descFirstValid, descRandomValid, descTriggered}, descTrigger, descLeaf, descTriggered, descRandomValid}

// EventPortraits are the keys an event names its portraits with, by their last word (left_portrait,
// left_icon), and the key a portrait's block names its character with.
var EventPortraits = struct {
	Suffixes  []string
	Character string
}{[]string{"_portrait", "_icon"}, "character"}

// OptionFlags are the yes-or-no words an option is marked with: the default and the historical choice.
var OptionFlags = []string{"default_option", "historical_option"}

// ---- Written forms.

// Bools are the words a yes-or-no value is written with.
var Bools = []string{"yes", "no"}

// Color is a block a color is written with, by the tag before it (rgb { 255 0 0 }), or none ({ 0.8 0.3 0.3 1 }).
type Color struct {
	Tag        string
	HSV        bool    // hue, saturation, value; else red, green, blue
	Hex        bool    // one hex word, given as its bytes
	Hue, Parts float64 // what the hue and the other parts are out of; 0 means 1
}

// Colors are the color blocks script writes; a part of rgb or of no tag is out of 255, or of 1 where every part
// of red, green and blue is at most 1, and a fourth part is its alpha.
var Colors = []Color{
	{Tag: "rgb"}, {Tag: ""}, {Tag: "hsv", HSV: true}, {Tag: "hsv360", HSV: true, Hue: 360, Parts: 100}, {Tag: "hex", Hex: true},
}

// DumpWords are the dumps' words the engine decides by: a blurb opening "Iterate through" with no
// target walks a list or variable map, so its block's scope is unknown; none and all name no scope; a function's
// return type of the third word names no type the dumps declare.
var DumpWords = struct {
	Iterates     string
	NoScope      []string
	Unregistered string
}{"Iterate through", []string{"none", "all"}, "[unregistered]"}

// GlobalType is the data type a data call's first step is read on.
const GlobalType = "Global"

// ---- Files and folders: where entries are and how they are named.

// MisspelledFolders are folder names the game never reads, by the name it reads instead.
var MisspelledFolders = map[string]string{"localisation": "localization"}

// plurals make a folder's last word one thing's: the first row whose plural ending fits wins
// (houses, diseases, policies, statuses, boxes, patches, sashes, events).
var plurals = []struct{ Plural, One string }{
	{"ouses", "ouse"}, {"eases", "ease"}, {"ies", "y"}, {"ses", "s"}, {"xes", "x"}, {"ches", "ch"}, {"shes", "sh"},
	{"s", ""},
}

// KindName is what one entry of a database is called: its last path word, _ and . as spaces, made
// singular by plurals ("scripted_effects" is "scripted effect").
func KindName(database string) string {
	word := database[strings.LastIndexByte(database, '/')+1:]
	for _, row := range plurals {
		if stem, cut := strings.CutSuffix(word, row.Plural); cut {
			word = stem + row.One
			break
		}
	}
	return strings.NewReplacer("_", " ", ".", " ").Replace(word)
}

// EntryMode is a word written before a definition's name, and what it does to one already loaded.
type EntryMode struct {
	Word     string
	Replaces bool // takes the earlier definition's place; else it adds to it
	Optional bool // TRY_: does nothing where there is none
	Creates  bool // _OR_CREATE: defines it where there is none
}

// Mode splits a game's mode word off a written name: REPLACE:x is x with mode REPLACE.
func (info *GameInfo) Mode(key string) (mode, name string) {
	word, rest, cut := strings.Cut(key, ":")
	if cut && slices.ContainsFunc(info.EntryModes, func(entryMode EntryMode) bool { return entryMode.Word == word }) {
		return word, rest
	}
	return "", key
}

// entryModes are the words Victoria 3 and EU5 share, in the order the game processes them.
var entryModes = []EntryMode{
	{Word: "INJECT_OR_CREATE", Creates: true}, {Word: "REPLACE_OR_CREATE", Replaces: true, Creates: true},
	{Word: "TRY_INJECT", Optional: true}, {Word: "TRY_REPLACE", Replaces: true, Optional: true},
	{Word: "INJECT"}, {Word: "REPLACE", Replaces: true},
}

// Localization, File and DataFunction are the databases a loc key, a file path and a data function (Type.Function)
// name; no definition carries them, so the index resolves the first two from the loc entries and the inventories.
const (
	Localization = "localization"
	File         = "file"
	DataFunction = "datafunction"
)

// Ref is what the words at a position name: the entries of a database ("" the row's own database), or the files
// under a folder from a root. In a file type's row At is a key at any depth, and a row naming neither a database nor
// a folder makes the key the calling word of the entries its value names (using = x); in a database's row At is the
// key path from the entry (on_actions, random_on_action/<number>).
type Ref struct {
	At       string
	Database string
	Folder   string
}

// FileType is how files of one extension name entries, and which of their untagged blocks are colors; an extension
// with no row defines nothing.
type FileType struct {
	NameKey     string            // the key whose value names a top block's entry: .asset name = x
	Keywords    []string          // the words that declare an entry: type x = button { }
	NameKeyOf   map[string]string // a block key at any depth, and the key whose value names its entry
	WidgetKey   string            // every other top block is an entry of <ext>.widget, named by this key's value
	Body        Shape             // the kind its entries' blocks hold
	TagCalls    string            // the word that calls the tag a block is written with (type x = button { }), "" none
	ContextKey  string            // the key whose value is the data call the bindings of its block read in, "" none
	Refs        []Ref             // the keys, at any depth, whose value names an entry or a file
	ColorSuffix string            // the ending of the keys whose untagged number block is a color (fontcolor), "" none
}

// FileTypes are how .txt, .asset, .font and .gui files name entries, in every game. In a .gui file
// a word before a top block declares it (template x { }) or groups declarations (types x { }). A key ending in color
// holds a color even untagged, in script (a culture's color) as in a GUI file (fontcolor, tintcolor),
// but a key naming a portrait gene (hair_color), which lsp's blockColor leaves out.
var FileTypes = map[string]FileType{
	".txt":   {ColorSuffix: "color"},
	".asset": {NameKey: "name"},
	".font":  {NameKey: "name"},
	".gui": {
		Keywords:    []string{"type", "local_type", "template", "local_template", "block", "layer"},
		NameKeyOf:   map[string]string{"texticon": "icon", "format": "name"},
		WidgetKey:   "name",
		Body:        ShapeWidget,
		TagCalls:    "widget",
		ContextKey:  "datacontext",
		ColorSuffix: "color",
		Refs: []Ref{
			{At: "using"}, {At: "layer", Database: "gui.layer"}, {At: "texture", Database: File},
			{At: "font", Database: "font.font"}, {At: "text", Database: Localization}, {At: "title", Database: Localization},
		},
	},
}

// Identity is how an entry is identified.
type Identity uint8

// The identities.
const (
	IdentityByKey    Identity = iota // the top statement's key names it; an event id where the purpose is events
	IdentityByField                  // the value of NameKey in its block names it, up to a (
	IdentityByFile                   // each file is one entry, named by its stem
	IdentityByColumn                 // each row of a table by its first column, or each word of a list
	IdentityNone                     // its entries name nothing: generated objects, locators, a history file
)

// Purpose is what a database is for.
type Purpose uint8

// The purposes.
const (
	PurposeGeneral   Purpose = iota
	PurposeEvents            // the whole tree is one database, named by event id
	PurposeOnActions         // its entries are on_actions
	PurposeLists             // scripted lists, iterated as every_/any_/random_/ordered_<name>
	PurposeFunctions         // data functions callable in data calls
	PurposeConcepts          // game concepts named in loc text
	PurposeHistory           // the start state of entries defined elsewhere (Of), or of its own files
)

// Descriptor is how one database's entries are written, named and used: one row per database that needs one.
// A folder with no row is a database of general purpose whose top statements' keys name its entries; a path
// takes the longest row at or above it.
type Descriptor struct {
	// the database: a folder from a root (decisions), a file, a file type's own (gui.type), a nested one (defines.define)
	Database string
	Path     string // the folder or file its entries are written in; "" for a nested or a file type's database
	Parent   string // nested: the folder or file whose entries hold its keys
	// nested: the key path from the holding entry to its keys: "" the entry's own block, options, **/parameters, */*/*
	Within       string
	Identity     Identity
	NameKey      string   // IdentityByField: the key whose value names the entry
	NamespaceKey string   // the key a file declares a namespace x by: its entries are its top keys x.N, N a number
	Wrappers     []string // keys at the top that only wrap entries: their blocks hold top statements
	Keyword      string   // the word that declares an entry inline in any file (scripted_effect x = { })
	Purpose      Purpose
	Of           string   // PurposeHistory: the database whose entries its own set up; "" its own files'
	Dated        bool     // a number key in an entry's own block is a date written short (999, 1077.1)
	Body         Shape    // what an entry's block holds; ShapeUnknown: its own fields
	Callers      []string // scripted: the words a call by an entry's name resolves by
	Parameters   bool     // scripted: a call passes parameters
	Local        bool     // its entries answer only in the file that declares them
	Refs         []Ref    // positions in an entry (key/<number>) whose words name an entry of a database
	Names        []string // keys whose values in an entry also name this database's entries (trait groups)
	LocPrefix    string   // what the loc key of an entry's name puts before the entry's name; "" the name is the key
}

// Layout is how one game's script files lay out their entries.
type Layout struct {
	Descriptors []Descriptor
	EntryModes  []EntryMode // in the order the game processes them; file order only within one
	FirstWins   []string    // the databases whose first definition wins; in every other, the last
}

// NameOf is the database a folder or file from a root makes: its path without common/.
func NameOf(path string) string { return strings.TrimPrefix(path, "common/") }

// conceptPrefix is what CK3 and EU5 put before a game concept's name for its loc key; Vic3's names are their own keys.
const conceptPrefix = "game_concept_"

// layouts are each game's layout: the shared rows, with its own over them.
var layouts = map[string]Layout{
	"ck3": {
		Descriptors: withShared([]Descriptor{
			folder("common/on_action", Descriptor{Purpose: PurposeOnActions,
				Refs: []Ref{{At: "on_actions"}, {At: "random_on_action/<number>"}, {At: "first_valid_on_action"}, {At: "fallback"}}}),
			folder("common/game_concepts", Descriptor{Purpose: PurposeConcepts, LocPrefix: conceptPrefix}),
			folder("common/traits", Descriptor{Names: []string{"group", "group_equivalence"}}),
			folder("gfx/map/map_object_data", Descriptor{Identity: IdentityNone}),
			folder("history/titles", Descriptor{Purpose: PurposeHistory, Of: "landed_titles"}),
			folder("history/provinces", Descriptor{Purpose: PurposeHistory, Of: "map_data.definition"}),
			folder("history/province_mapping", Descriptor{Purpose: PurposeHistory, Of: "map_data.definition"}),
			folder("history/cultures", Descriptor{Purpose: PurposeHistory, Of: "culture/cultures", Identity: IdentityByFile}),
			folder("history/faiths", Descriptor{Purpose: PurposeHistory, Of: "religion/faith_types"}),
			folder("history/artifacts", Descriptor{Purpose: PurposeHistory}),
			folder("history/situations", Descriptor{Purpose: PurposeHistory}),
			folder("history/struggles", Descriptor{Purpose: PurposeHistory}),
			folder("history/wars", Descriptor{Purpose: PurposeHistory, Identity: IdentityNone}),
			folder("history/characters", Descriptor{Body: ShapeSetup, Dated: true}),
			{Database: "map_data.definition", Path: "map_data/definition.csv", Identity: IdentityByColumn},
			nested("common/landed_titles", "", ""),
			nested("common/activities/activity_types", "options", "option_category"),
			nested("common/activities/activity_types", "phases", "phase"),
			nested("common", "**/parameters", "parameter"),
			nested("common", "**/*_parameters", "parameter"),
			nested("common/game_concepts", "alias", ""),
		}),
		FirstWins: []string{"gui.type", "gui.template"},
	},
	"vic3": {
		Descriptors: withShared([]Descriptor{
			folder("common/on_actions", Descriptor{Purpose: PurposeOnActions,
				Refs: []Ref{{At: "on_actions"}, {At: "random_on_actions/<number>"}, {At: "first_valid_on_action"}, {At: "fallback"}}}),
			folder("gfx/map/map_object_data", Descriptor{Identity: IdentityNone}),
			folder("gfx/map/post_effects", Descriptor{Identity: IdentityByField, NameKey: "name", Wrappers: []string{"posteffect_volumes"}}),
			// COUNTRIES = { … }: effects
			folder("common/history", Descriptor{Purpose: PurposeHistory, Identity: IdentityNone, Body: ShapeEffects}),
		}),
		EntryModes: entryModes,
		FirstWins:  []string{"gui.type", "gui.template", "events"},
	},
	"eu5": {
		Descriptors: withShared([]Descriptor{
			folder("common/on_action", Descriptor{Purpose: PurposeOnActions,
				Refs: []Ref{{At: "on_actions"}, {At: "random_on_action/<number>"}, {At: "first_valid_on_action"}, {At: "fallback"}}}),
			folder("gfx/map/map_objects", Descriptor{Identity: IdentityNone}),
			folder("gfx/map/city_data", Descriptor{Identity: IdentityByField, NameKey: "name"}),
			folder("common/game_concepts", Descriptor{Purpose: PurposeConcepts, LocPrefix: conceptPrefix}),
			folder("setup/start", Descriptor{Purpose: PurposeHistory}),
			folder("setup/templates", Descriptor{Identity: IdentityByFile}),
			folder("map_data/location_templates.txt", Descriptor{Purpose: PurposeHistory, Of: "map_data/named_locations"}),
			folder("map_data/generated_locators_port.txt", Descriptor{Identity: IdentityNone}),
			nested("gfx/defines", "", "define"),
			nested("common/laws", "", "policy"),
			nested("common/languages", "dialects", "dialect"),
			nested("common/gods", "omens", "omen"),
			nested("common/building_types", "unique_production_methods", "unique_production_method"),
			nested("common/game_concepts", "alias", ""),
			nested("map_data/definitions.txt", "", "sub_continent"),
			nested("map_data/definitions.txt", "*", "region"),
			nested("map_data/definitions.txt", "*/*", "area"),
			nested("map_data/definitions.txt", "*/*/*", "province"),
			nested("setup/start", "character_db", "character"),
			nested("setup/start", "dynasty_manager", "dynasty"),
			setsUp("setup/start", "countries/countries", "country", "setup/countries"),
			setsUp("setup/start", "locations", "location", "map_data/named_locations"),
			setsUp("setup/start", "road_network", "road", "map_data/named_locations"),
			setsUp("setup/start", "townrights_manager", "town_rights", "map_data/named_locations"),
			setsUp("setup/start", "building_manager", "building", "building_types"),
			setsUp("setup/start", "work_of_art_manager", "work_of_art", "artist_work"),
			setsUp("setup/start", "colony_manager", "colony", "map_data/definitions.txt.province"),
			setsUp("setup/start", "institution_manager/institutions", "institution", "institution"),
			setsUp("setup/start", "situation_manager", "situation", "situations"),
		}),
		EntryModes: entryModes,
		FirstWins:  []string{"events"},
	},
}

// shared are every game's rows; a row whose folder a game lacks matches nothing.
var shared = []Descriptor{
	folder("common/scripted_effects", Descriptor{Callers: []string{callEffect}, Body: ShapeEffects, Keyword: "scripted_effect", Parameters: true}),
	folder("common/scripted_triggers", Descriptor{Callers: []string{callTrigger}, Body: ShapeTriggers, Keyword: "scripted_trigger", Parameters: true}),
	folder("common/scripted_modifiers", Descriptor{Callers: []string{callValue}, Parameters: true}),
	folder("common/script_values", Descriptor{Callers: []string{callTrigger, callValue}, Body: ShapeValue}),
	folder("common/scripted_lists", Descriptor{Purpose: PurposeLists}),
	folder("events", Descriptor{Purpose: PurposeEvents, NamespaceKey: "namespace"}),
	folder("common/genes", Descriptor{Wrappers: []string{"color_genes", "special_genes", "morph_genes", "accessory_genes",
		"decal_atlases", "age_presets"}}),
	folder("common/named_colors", Descriptor{Wrappers: []string{"colors"}}),
	folder("data_binding", Descriptor{Identity: IdentityByField, NameKey: "definition", Purpose: PurposeFunctions}),
	folder("common/game_concepts", Descriptor{Purpose: PurposeConcepts}),
	folder("common/achievement_groups.txt", Descriptor{Identity: IdentityByField, NameKey: "name"}),
	folder("common/coat_of_arms/options", Descriptor{Identity: IdentityNone}),
	folder("common/coat_of_arms/coat_of_arms", Descriptor{Wrappers: []string{"template"}}),
	folder("common/coat_of_arms/template_lists", Descriptor{Wrappers: []string{"coat_of_arms_template_lists",
		"pattern_texture_lists", "colored_emblem_texture_lists", "textured_emblem_texture_lists", "color_lists"}}),
	folder("gui/scripted_widgets", Descriptor{Identity: IdentityNone}),
	{Database: "gui.type", Callers: []string{"widget"}},                       // a widget key naming a type
	{Database: "gui.local_type", Callers: []string{"widget"}, Local: true},    // the same, written in its own file
	{Database: "gui.template", Callers: []string{"using"}},                    // using = x
	{Database: "gui.local_template", Callers: []string{"using"}, Local: true}, // the same, written in its own file
	{Database: "gui.block", Callers: []string{"blockoverride"}},               // blockoverride "x" { }
	folder("music/music_player_categories", Descriptor{Identity: IdentityByField, NameKey: "id"}),
	nested("common/defines", "", "define"),
}

// folder is a row for the database a folder or file makes.
func folder(path string, row Descriptor) Descriptor {
	row.Database, row.Path = NameOf(path), path
	return row
}

// nested is a row for a database whose entries are keys written inside the entries of a folder or file, at a key
// path from each entry; database "" makes those keys entries of the holding database itself (a title inside a
// title).
func nested(parent, within, database string) Descriptor {
	row := Descriptor{Database: NameOf(parent), Parent: parent, Within: within}
	if database != "" {
		row.Database += "." + database
	}
	return row
}

// setsUp is a nested row whose entries are the start state of a database defined elsewhere.
func setsUp(parent, within, database, of string) Descriptor {
	row := nested(parent, within, database)
	row.Purpose, row.Of = PurposeHistory, of
	return row
}

// withShared is the shared rows with one game's own rows over them, by database.
func withShared(own []Descriptor) []Descriptor {
	rows := slices.Clone(shared)
	for _, row := range own {
		if index := slices.IndexFunc(rows, func(existing Descriptor) bool {
			return existing.Database == row.Database && existing.Within == row.Within
		}); index >= 0 {
			rows[index] = row
		} else {
			rows = append(rows, row)
		}
	}
	return rows
}

// At is the row whose path is a folder or file; false for none.
func (layout Layout) At(path string) (Descriptor, bool) {
	for _, row := range layout.Descriptors {
		if row.Path == path {
			return row, true
		}
	}
	return Descriptor{}, false
}

// Nested are the rows whose keys are written inside the entries of a file: those whose parent is the file or
// a folder above it.
func (layout Layout) Nested(rel string) (rows []Descriptor) {
	for _, row := range layout.Descriptors {
		if row.Parent != "" && (rel == row.Parent || strings.HasPrefix(rel, row.Parent+"/")) {
			rows = append(rows, row)
		}
	}
	return rows
}
