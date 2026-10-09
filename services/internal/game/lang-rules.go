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

// Block is the kind of content a block holds.
type Block uint8

const (
	BlockUnknown     Block = iota // no source says what it holds
	BlockFields                   // an entry's own keys, or a field's
	BlockTriggers                 // conditions
	BlockEffects                  // actions
	BlockArguments                // a command's arguments only
	BlockValue                    // a script value's formula
	BlockDescription              // a dynamic description
	BlockSetup                    // a history entry's start state: the docs' fields, else effects
	BlockWidget                   // a GUI widget: its properties, and blocks that are widgets again
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
)

// BlockRule is how one kind of block reads.
type BlockRule struct {
	Keys   []KeyRule // the steps a key is tried as, in order
	Calls  string    // the CalledIn word a call here is resolved by; AnyCall for every scripted folder
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

// Blocks is the rule of each kind of block: keys, calls, allows. A description block reads by
// DynamicDescription.
var Blocks = map[Block]BlockRule{
	BlockFields:    {[]KeyRule{KeyRulePosition}, "", 0},
	BlockTriggers:  {[]KeyRule{KeyRuleKnownCall, KeyRuleDeclared, KeyRuleCommand, KeyRuleLink, KeyRuleObserved, KeyRuleCall}, callTrigger, KindTrigger},
	BlockEffects:   {[]KeyRule{KeyRuleKnownCall, KeyRuleDeclared, KeyRuleCommand, KeyRuleLink, KeyRuleObserved, KeyRuleCall}, callEffect, KindEffect},
	BlockValue:     {[]KeyRule{KeyRuleKnownCall, KeyRuleDeclared, KeyRuleCommand, KeyRuleOperator, KeyRuleLink, KeyRuleObserved, KeyRuleCall}, callValue, KindTrigger | KindEffect},
	BlockArguments: {[]KeyRule{KeyRuleKnownCall, KeyRuleDeclared, KeyRuleCommand, KeyRuleObserved, KeyRuleCall}, AnyCall, 0},
	BlockUnknown:   {[]KeyRule{KeyRuleKnownCall, KeyRuleDeclared, KeyRuleCommand, KeyRuleLink, KeyRulePositionOrCall}, AnyCall, KindTrigger | KindEffect},
	BlockSetup:     {[]KeyRule{KeyRuleDeclared, KeyRuleDocumented, KeyRuleKnownCall, KeyRuleCommand, KeyRuleObserved, KeyRuleLink, KeyRuleCall}, callEffect, KindEffect},
	BlockWidget:    {[]KeyRule{KeyRuleKnownCall, KeyRulePosition}, "widget", 0},
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

// Structural is a key inside a command's block whose own block has a fixed kind.
type Structural struct {
	Command   string
	Key       string // the key, by written form (<number>); "" any key
	Holds     Block  // the kind its block holds
	Enclosing bool   // it holds the kind of block the command is written in instead
	Calls     string // the CalledIn word for calls in its block, where it differs from Holds'
}

// Structurals are random_list's weighted branches and switch's cases.
var Structurals = []Structural{
	{Command: "random_list", Key: "<number>", Holds: BlockEffects, Calls: AnyCall},
	{Command: "switch", Enclosing: true},
}

// ---- Names: what a written word names.

// Name is one rule for what a word names.
type Name uint8

const (
	NameHistory   Name = iota // a history entry's top key, and its value (key = value), name what it sets up
	NameList                  // a call starting with a ListIterators prefix iterates that scripted list
	NameCall                  // a call names a scripted entry
	NameParameter             // a key in a call's block names a parameter of the called entry
	NameMade                  // the value of a command that makes a name defines it
	NameGroup                 // a value at a folder's Names key names an entry of that folder
	NameRead                  // the value of a command that reads a name refers to it
	NameClaim                 // a value at a position declared to name a database, a loc key or a file
	NameValue                 // a word where a number is declared or vanilla writes one names a script value
	NameEvent                 // a value written as an event id names an event
	NameFileRef               // a value at a key its file type lists (FileType.Refs) names that entry
)

// KeyNames are the rules for a key, every one that fits; ValueNames for a value, the first that
// fits. On every word, prefix:name names an entry of the prefix's database where the prefix is a
// link, and $NAME$ in an entry of a folder with Parameters defines a parameter.
var (
	KeyNames   = []Name{NameHistory, NameList, NameCall, NameParameter}
	ValueNames = []Name{NameHistory, NameFileRef, NameMade, NameGroup, NameRead, NameClaim, NameValue, NameEvent}
)

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

// ListIterators are the prefixes a scripted list is iterated by (every_<list>), and the kind of
// block each holds.
var ListIterators = map[string]Block{"every_": BlockEffects, "random_": BlockEffects, "ordered_": BlockEffects, "any_": BlockTriggers}

// ---- Values and descriptions.

// ValueOperators are a value block's operators: true where the operator takes a number, so its own
// block is a value block; false where its block holds arguments.
var ValueOperators = map[string]bool{
	"value": true, "add": true, "subtract": true, "multiply": true, "divide": true, "modulo": true, "min": true,
	"max": true, "round": true, "round_to": true, "ceiling": true, "floor": true, "fixed_range": true,
	"integer_range": true, "abs": true, "desc": false, "format": false, "save_temporary_value_as": false,
}

// DynamicDescription is the shape of a dynamic description. A block is one when it holds a marker
// whose block holds the leaf or another marker. Inside, a marker holds the shape again, Trigger
// holds triggers, and the leaf is read as the position the description belongs to. Triggered shows
// its leaf only while its trigger holds; Random shows one of its own at random, any other marker the
// first whose trigger holds.
var DynamicDescription = struct {
	Markers                          []string
	Trigger, Leaf, Triggered, Random string
}{[]string{"first_valid", "random_valid", "triggered_desc"}, "trigger", "desc", "triggered_desc", "random_valid"}

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

// Placeholders are the last words of a block placeholder a block lists ({ <display_triggers> },
// { <interface effects> }) and the kind of block each names; the first listed wins.
var Placeholders = map[string]Block{
	"effect": BlockEffects, "effects": BlockEffects, "trigger": BlockTriggers, "triggers": BlockTriggers,
}

// UsagePlaceholder matches a block placeholder in a dump usage line; a usage with none writes
// arguments only.
const UsagePlaceholder = `<[^<>\n]*(?i:trigger|effect)[^<>\n]*>|\.\.\.\s*(?i:triggers?|effects?)\s*\.\.\.`

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
	Body      Block             // the kind its entries' blocks hold
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
		Body:      BlockWidget,
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
	Body       Block    // scripted: the kind an entry's block holds; unset means its own fields
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
			"history/characters":       {Body: BlockSetup, Dated: true},
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
			"common/history":          {History: true, Unnamed: true, Body: BlockEffects}, // COUNTRIES = { … }: effects
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
	"common/scripted_effects":   {CalledIn: []string{callEffect}, Body: BlockEffects, Keyword: "scripted_effect", Parameters: true},
	"common/scripted_triggers":  {CalledIn: []string{callTrigger}, Body: BlockTriggers, Keyword: "scripted_trigger", Parameters: true},
	"common/scripted_modifiers": {CalledIn: []string{callValue}, Parameters: true},
	"common/script_values":      {CalledIn: []string{callTrigger, callValue}, Body: BlockValue},
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
