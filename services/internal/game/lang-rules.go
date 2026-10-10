// lang-rules.go is Paradox script's language rules as data, the same for every game and version,
// and each game's file Layout. The schema's walker (schema/walk.go) reads statements by these tables and
// the walker's own rules.

package game

import (
	"maps"
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

// Construct is a key the language itself gives a fixed shape to, which no dump declares: inside one command's
// block (Command; "" any), within a holder of one shape (Within; ShapeUnknown any), a key by its written form (Key:
// <number>, <any>, a name) or by its prefix (Prefix), read as a call that iterates the scripted list the rest of
// the key names. Holds is the shape its block holds, ShapeEnclosing the holder's; Scope how its block's scope
// follows; Caller the word its calls resolve by, "" the shape's own; Leaf marks a description's leaf, read at
// the position the description belongs to and counted nowhere. The first row that fits wins.
type Construct struct {
	Command string
	Within  Shape
	Key     string
	Prefix  bool
	Holds   Shape
	Scope   ScopeVerb
	Caller  string
	Leaf    bool
}

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
// markers, trigger and leaf, a value block's operators, and a widget's blocks.
var Constructs = []Construct{
	{Command: "random_list", Key: "<number>", Holds: ShapeEffects, Caller: AnyCall},
	{Command: "switch", Key: AnyKey, Holds: ShapeEnclosing},
	{Prefix: true, Key: "every_", Holds: ShapeEffects, Scope: ScopeUnknown},
	{Prefix: true, Key: "random_", Holds: ShapeEffects, Scope: ScopeUnknown},
	{Prefix: true, Key: "ordered_", Holds: ShapeEffects, Scope: ScopeUnknown},
	{Prefix: true, Key: "any_", Holds: ShapeTriggers, Scope: ScopeUnknown},
	{Within: ShapeDescription, Key: descFirstValid, Holds: ShapeDescription},
	{Within: ShapeDescription, Key: descRandomValid, Holds: ShapeDescription},
	{Within: ShapeDescription, Key: descTriggered, Holds: ShapeDescription},
	{Within: ShapeDescription, Key: descTrigger, Holds: ShapeTriggers},
	{Within: ShapeDescription, Key: descLeaf, Leaf: true},
	{Within: ShapeValue, Key: "value", Holds: ShapeValue},
	{Within: ShapeValue, Key: "add", Holds: ShapeValue},
	{Within: ShapeValue, Key: "subtract", Holds: ShapeValue},
	{Within: ShapeValue, Key: "multiply", Holds: ShapeValue},
	{Within: ShapeValue, Key: "divide", Holds: ShapeValue},
	{Within: ShapeValue, Key: "modulo", Holds: ShapeValue},
	{Within: ShapeValue, Key: "min", Holds: ShapeValue},
	{Within: ShapeValue, Key: "max", Holds: ShapeValue},
	{Within: ShapeValue, Key: "round", Holds: ShapeValue},
	{Within: ShapeValue, Key: "round_to", Holds: ShapeValue},
	{Within: ShapeValue, Key: "ceiling", Holds: ShapeValue},
	{Within: ShapeValue, Key: "floor", Holds: ShapeValue},
	{Within: ShapeValue, Key: "fixed_range", Holds: ShapeValue},
	{Within: ShapeValue, Key: "integer_range", Holds: ShapeValue},
	{Within: ShapeValue, Key: "abs", Holds: ShapeValue},
	{Within: ShapeValue, Key: descLeaf, Holds: ShapeArguments},
	{Within: ShapeValue, Key: "format", Holds: ShapeArguments},
	{Within: ShapeValue, Key: "save_temporary_value_as", Holds: ShapeArguments},
	{Within: ShapeWidget, Key: AnyKey, Holds: ShapeWidget},
}

// ConstructOf returns the first construct row that fits a key written in a block of a shape inside a command's
// block, by the key's position name; a prefix row never fits here (the walker reads it for a call).
func ConstructOf(within Shape, command, key string) (Construct, bool) {
	for _, row := range Constructs {
		fitsHolder := (row.Command == "" || row.Command == command) && (row.Within == ShapeUnknown || row.Within == within)
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

// ---- Names: what a written word names: the name commands, the saved-scope database and the list iterators; the
// rules themselves are the schema's (schema/occurrences.go).

// SavedScopes is the database of saved scope names (save_scope_as).
const SavedScopes = "scope"

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
	"set_variable":                         {"var", true},
	"set_variable/name":                    {"var", true},
	"add_to_variable_list/name":            {"var", true},
	"has_variable":                         {"var", false},
	"set_local_variable":                   {"local_var", true},
	"set_local_variable/name":              {"local_var", true},
	"add_to_local_variable_list/name":      {"local_var", true},
	"has_local_variable":                   {"local_var", false},
	"set_global_variable":                  {"global_var", true},
	"set_global_variable/name":             {"global_var", true},
	"add_to_global_variable_list/name":     {"global_var", true},
	"has_global_variable":                  {"global_var", false},

	// the commands that read a variable or variable list, and the keys a list iterator takes one by
	"variable": {"var", false}, "change_variable/name": {"var", false}, "clamp_variable/name": {"var", false},
	"round_variable/name": {"var", false}, "remove_variable": {"var", false}, "has_variable_list": {"var", false},
	"clear_variable_list": {"var", false}, "is_target_in_variable_list/name": {"var", false},
	"variable_list_size/name": {"var", false}, "remove_list_variable/name": {"var", false},
	"local_variable": {"local_var", false}, "change_local_variable/name": {"local_var", false},
	"clamp_local_variable/name": {"local_var", false}, "round_local_variable/name": {"local_var", false},
	"remove_local_variable": {"local_var", false}, "has_local_variable_list": {"local_var", false},
	"clear_local_variable_list": {"local_var", false}, "is_target_in_local_variable_list/name": {"local_var", false},
	"local_variable_list_size/name": {"local_var", false}, "remove_list_local_variable/name": {"local_var", false},
	"global_variable": {"global_var", false}, "change_global_variable/name": {"global_var", false},
	"clamp_global_variable/name": {"global_var", false}, "round_global_variable/name": {"global_var", false},
	"remove_global_variable": {"global_var", false}, "has_global_variable_list": {"global_var", false},
	"clear_global_variable_list": {"global_var", false}, "is_target_in_global_variable_list/name": {"global_var", false},
	"global_variable_list_size/name": {"global_var", false}, "remove_list_global_variable/name": {"global_var", false},
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

// Color is a tagged block a color is written with (rgb { 255 0 0 }).
type Color struct {
	Tag        string
	HSV        bool    // hue, saturation, value; else red, green, blue
	Hex        bool    // one hex word
	Hue, Parts float64 // what the hue and the other parts are out of; 0 means 1
}

// Colors are the color blocks script writes; rgb's parts are out of 255, or of 1 where every part
// is at most 1.
var Colors = []Color{
	{Tag: "rgb"}, {Tag: "hsv", HSV: true}, {Tag: "hsv360", HSV: true, Hue: 360, Parts: 100}, {Tag: "hex", Hex: true},
}

// DumpWords are the dumps' words the engine decides by: a blurb opening "Iterate through" with no
// target walks a list or variable map, so its block's scope is unknown; none and all name no scope.
var DumpWords = struct {
	Iterates string
	NoScope  []string
}{"Iterate through", []string{"none", "all"}}

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
func (info *GameInfo) Mode(written string) (mode, name string) {
	word, rest, cut := strings.Cut(written, ":")
	if cut && slices.ContainsFunc(info.EntryModes, func(entryMode EntryMode) bool { return entryMode.Word == word }) {
		return word, rest
	}
	return "", written
}

// entryModes are the words Victoria 3 and EU5 share, in the order the game processes them.
var entryModes = []EntryMode{
	{Word: "INJECT_OR_CREATE", Creates: true}, {Word: "REPLACE_OR_CREATE", Replaces: true, Creates: true},
	{Word: "TRY_INJECT", Optional: true}, {Word: "TRY_REPLACE", Replaces: true, Optional: true},
	{Word: "INJECT"}, {Word: "REPLACE", Replaces: true},
}

// FileType is how files of one extension name entries; an extension with no row defines nothing.
type FileType struct {
	NameKey   string            // the key whose value names a top block's entry: .asset name = x
	Keywords  []string          // the words that declare an entry: type x = button { }
	NameKeyOf map[string]string // a block key at any depth, and the key whose value names its entry
	WidgetKey string            // every other top block is an entry of <ext>.widget, named by this key's value
	Body      Shape             // the kind its entries' blocks hold
	Refs      map[string]string // keys whose value names an entry of <ext>.<word>, at any depth
}

// FileTypes are how .txt, .asset, .font and .gui files name entries, in every game. In a .gui file
// a word before a top block declares it (template x { }) or groups declarations (types x { }).
var FileTypes = map[string]FileType{
	".txt":   {},
	".asset": {NameKey: "name"},
	".font":  {NameKey: "name"},
	".gui": {
		Keywords:  []string{"type", "local_type", "template", "local_template", "block", "layer"},
		NameKeyOf: map[string]string{"texticon": "icon", "format": "name"},
		WidgetKey: "name",
		Body:      ShapeWidget,
		Refs:      map[string]string{"using": "template", "layer": "layer"},
	},
}

// Folder is one folder's row, by its path from a root (without EU5's stage folder); a path takes
// the longest row at or above it.
type Folder struct {
	History    bool     // the start state of entries defined elsewhere; defines none of its own
	Of         string   // History: the database its entries are named after
	ByFile     bool     // History: each file is one entry, named by its stem
	Dated      bool     // a number key in an entry's own block is a date written short (999, 1077.1)
	Unnamed    bool     // its entries name nothing: generated objects, locators, path-keyed lists
	NameKey    string   // every top block is one entry named by this key's value, up to a (
	Wrappers   []string // keys that only wrap entries: their blocks hold top statements
	CalledIn   []string // scripted: the CalledIn words (Blocks' Calls) a call by an entry's name is in
	Body       Shape    // scripted: the kind an entry's block holds; unset means its own fields
	Keyword    string   // scripted: the word that declares an entry inline in any file
	Parameters bool     // scripted: a call passes parameters
	Events     bool     // the whole tree is one database, named by event id
	Iterated   bool     // its entries are iterated as every_/any_/random_/ordered_<name> (scripted lists)
	OnActions  bool     // its entries are on_actions
	Refs       []string // positions in an entry (key/<number>) whose words name this database's entries
	Names      []string // keys whose values in an entry also name this database's entries (trait groups)
	Functions  bool     // its entries are data functions callable in data calls
	Concepts   bool     // its entries are game concepts named in loc text
}

// Nested is a place inside an entry where keys define entries of their own.
type Nested struct {
	Parent   string // a folder path; the row holds for every database at or below it
	Path     string // "" the entry's own block, * one key, a leading **/ any depth
	Database string // the database the keys define; self for the parent's own
	Of       string // the keys set up entries of this database, defined elsewhere (start state)
}

// MapTable is a table whose rows are named by their first column.
type MapTable struct {
	Database string
	File     string
}

// Layout is how one game's script files lay out their entries.
type Layout struct {
	Folders            map[string]Folder
	Nested             []Nested
	MapTable           MapTable
	EntryModes         []EntryMode // in the order the game processes them; file order only within one
	FirstWinsDatabases []string    // the databases whose first definition wins; in every other, the last
}

// layouts are each game's layout: the shared rows, with its own over them.
var layouts = map[string]Layout{
	"ck3": {
		Folders: withShared(map[string]Folder{
			"common/on_action": {OnActions: true,
				Refs: []string{"on_actions", "random_on_action/<number>", "first_valid_on_action", "fallback"}},
			"common/traits":            {Names: []string{"group", "group_equivalence"}},
			"gfx/map/map_object_data":  {Unnamed: true},
			"history/titles":           {History: true, Of: "landed_titles"},
			"history/provinces":        {History: true, Of: "map_data.definition"},
			"history/province_mapping": {History: true, Of: "map_data.definition"},
			"history/cultures":         {History: true, Of: "culture/cultures", ByFile: true},
			"history/faiths":           {History: true, Of: "religion/faith_types"},
			"history/artifacts":        {History: true},
			"history/situations":       {History: true},
			"history/struggles":        {History: true},
			"history/wars":             {History: true, Unnamed: true},
			"history/characters":       {Body: ShapeSetup, Dated: true},
		}),
		Nested: slices.Concat(sharedNested, []Nested{
			{Parent: "common/landed_titles", Path: "", Database: "self"},
			{Parent: "common/activities/activity_types", Path: "options", Database: "option_category"},
			{Parent: "common/activities/activity_types", Path: "phases", Database: "phase"},
			{Parent: "common", Path: "**/parameters", Database: "parameter"},
			{Parent: "common", Path: "**/*_parameters", Database: "parameter"},
			{Parent: "common/game_concepts", Path: "alias", Database: "self"},
		}),
		MapTable:           MapTable{Database: "map_data.definition", File: "map_data/definition.csv"},
		FirstWinsDatabases: []string{"gui.type", "gui.template"},
	},
	"vic3": {
		Folders: withShared(map[string]Folder{
			"common/on_actions": {OnActions: true,
				Refs: []string{"on_actions", "random_on_actions/<number>", "first_valid_on_action", "fallback"}},
			"gfx/map/map_object_data": {Unnamed: true},
			"gfx/map/post_effects":    {NameKey: "name", Wrappers: []string{"posteffect_volumes"}},
			"common/history":          {History: true, Unnamed: true, Body: ShapeEffects}, // COUNTRIES = { … }: effects
		}),
		Nested:             sharedNested,
		EntryModes:         entryModes,
		FirstWinsDatabases: []string{"gui.type", "gui.template", "events"},
	},
	"eu5": {
		Folders: withShared(map[string]Folder{
			"common/on_action": {OnActions: true,
				Refs: []string{"on_actions", "random_on_action/<number>", "first_valid_on_action", "fallback"}},
			"gfx/map/map_objects":                  {Unnamed: true},
			"gfx/map/city_data":                    {NameKey: "name"},
			"setup/start":                          {History: true},
			"setup/templates":                      {ByFile: true},
			"map_data/location_templates.txt":      {History: true, Of: "map_data/named_locations"},
			"map_data/generated_locators_port.txt": {Unnamed: true},
		}),
		Nested: slices.Concat(sharedNested, []Nested{
			{Parent: "gfx/defines", Path: "", Database: "define"},
			{Parent: "common/laws", Path: "", Database: "policy"},
			{Parent: "common/languages", Path: "dialects", Database: "dialect"},
			{Parent: "common/gods", Path: "omens", Database: "omen"},
			{Parent: "common/building_types", Path: "unique_production_methods", Database: "unique_production_method"},
			{Parent: "common/game_concepts", Path: "alias", Database: "self"},
			{Parent: "map_data/definitions.txt", Path: "", Database: "sub_continent"},
			{Parent: "map_data/definitions.txt", Path: "*", Database: "region"},
			{Parent: "map_data/definitions.txt", Path: "*/*", Database: "area"},
			{Parent: "map_data/definitions.txt", Path: "*/*/*", Database: "province"},
			{Parent: "setup/start", Path: "character_db", Database: "character"},
			{Parent: "setup/start", Path: "dynasty_manager", Database: "dynasty"},
			{Parent: "setup/start", Path: "countries/countries", Database: "country", Of: "setup/countries"},
			{Parent: "setup/start", Path: "locations", Database: "location", Of: "map_data/named_locations"},
			{Parent: "setup/start", Path: "road_network", Database: "road", Of: "map_data/named_locations"},
			{Parent: "setup/start", Path: "townrights_manager", Database: "town_rights", Of: "map_data/named_locations"},
			{Parent: "setup/start", Path: "building_manager", Database: "building", Of: "building_types"},
			{Parent: "setup/start", Path: "work_of_art_manager", Database: "work_of_art", Of: "artist_work"},
			{Parent: "setup/start", Path: "colony_manager", Database: "colony", Of: "map_data/definitions.txt.province"},
			{Parent: "setup/start", Path: "institution_manager/institutions", Database: "institution", Of: "institution"},
			{Parent: "setup/start", Path: "situation_manager", Database: "situation", Of: "situations"},
		}),
		EntryModes:         entryModes,
		FirstWinsDatabases: []string{"events"},
	},
}

// sharedNested are the nested rows every game has.
var sharedNested = []Nested{{Parent: "common/defines", Path: "", Database: "define"}}

// sharedFolders are every game's folder rows; a row whose folder a game lacks matches nothing.
var sharedFolders = map[string]Folder{
	"common/scripted_effects":   {CalledIn: []string{callEffect}, Body: ShapeEffects, Keyword: "scripted_effect", Parameters: true},
	"common/scripted_triggers":  {CalledIn: []string{callTrigger}, Body: ShapeTriggers, Keyword: "scripted_trigger", Parameters: true},
	"common/scripted_modifiers": {CalledIn: []string{callValue}, Parameters: true},
	"common/script_values":      {CalledIn: []string{callTrigger, callValue}, Body: ShapeValue},
	"common/scripted_lists":     {Iterated: true},
	"events":                    {Events: true},
	"common/genes": {Wrappers: []string{"color_genes", "special_genes", "morph_genes", "accessory_genes",
		"decal_atlases", "age_presets"}},
	"common/named_colors":              {Wrappers: []string{"colors"}},
	"data_binding":                     {NameKey: "definition", Functions: true},
	"common/game_concepts":             {Concepts: true},
	"common/achievement_groups.txt":    {NameKey: "name"},
	"common/coat_of_arms/options":      {Unnamed: true},
	"common/coat_of_arms/coat_of_arms": {Wrappers: []string{"template"}},
	"common/coat_of_arms/template_lists": {Wrappers: []string{"coat_of_arms_template_lists",
		"pattern_texture_lists", "colored_emblem_texture_lists", "textured_emblem_texture_lists", "color_lists"}},
	"gui/scripted_widgets":          {Unnamed: true},
	"gui.type":                      {CalledIn: []string{"widget"}},        // a widget key naming a type
	"gui.block":                     {CalledIn: []string{"blockoverride"}}, // blockoverride "x" { }
	"music/music_player_categories": {NameKey: "id"},
}

// withShared is the shared rows with one game's own rows over them.
func withShared(own map[string]Folder) map[string]Folder {
	rows := maps.Clone(sharedFolders)
	maps.Copy(rows, own)
	return rows
}
