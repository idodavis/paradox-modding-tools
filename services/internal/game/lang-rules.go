// lang-rules.go is Paradox script's language rules as data, the same for every game and version,
// and each game's file Layout. The schema's walker (schema/walk.go) reads statements by these tables and
// the walker's own rules.

package game

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
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

// Color is a block a color is written with, by the tag before it (rgb { 255 0 0 }), or none ({ 0.8 0.3 0.3 1 }).
type Color struct {
	Tag        string
	HSV        bool    // hue, saturation, value; else red, green, blue
	Hex        bool    // one hex word, given as its bytes
	Hue, Parts float64 // what the hue and the other parts are out of; 0 means 1
}

// RGBA is a color, each part from 0 to 1.
type RGBA struct{ R, G, B, A float64 }

// Colors are the color blocks script writes; a part of rgb or of no tag is out of 255, or of 1 where every part
// of red, green and blue is at most 1, and a fourth part is its alpha.
var Colors = []Color{
	{Tag: "rgb"}, {Tag: ""}, {Tag: "hsv", HSV: true}, {Tag: "hsv360", HSV: true, Hue: 360, Parts: 100}, {Tag: "hex", Hex: true},
}

// ColorOf is the color the numbers of a color block make by the Colors row of its tag, matched without regard to
// case: false for a tag no row has and for fewer than three parts.
func ColorOf(tag string, parts []float64) (RGBA, bool) {
	i := slices.IndexFunc(Colors, func(row Color) bool { return strings.EqualFold(row.Tag, tag) })
	if i < 0 || len(parts) < 3 {
		return RGBA{}, false
	}
	row, scale := Colors[i], 255.0
	var red, green, blue float64
	if row.HSV {
		scale = cmp.Or(row.Parts, 1)
		red, green, blue = hsvToRGB(parts[0]/cmp.Or(row.Hue, 1), parts[1]/scale, parts[2]/scale)
	} else {
		if !row.Hex && max(parts[0], parts[1], parts[2]) <= 1 {
			scale = 1
		}
		red, green, blue = parts[0]/scale, parts[1]/scale, parts[2]/scale
	}
	alpha := 1.0
	if len(parts) > 3 {
		alpha = parts[3] / scale
	}
	return RGBA{clamp(red), clamp(green), clamp(blue), clamp(alpha)}, true
}

// hsvToRGB is the red, green and blue of a hue, saturation and value, each from 0 to 1.
func hsvToRGB(hue, saturation, value float64) (red, green, blue float64) {
	sector, fraction := int(hue*6)%6, hue*6-float64(int(hue*6))
	bottom, falling, rising := value*(1-saturation), value*(1-fraction*saturation), value*(1-(1-fraction)*saturation)
	switch sector {
	case 0:
		return value, rising, bottom
	case 1:
		return falling, value, bottom
	case 2:
		return bottom, value, rising
	case 3:
		return bottom, falling, value
	case 4:
		return rising, bottom, value
	}
	return value, bottom, falling
}

// clamp is a part held to 0 through 1.
func clamp(part float64) float64 { return min(max(part, 0), 1) }

// Hex is the color as #rrggbb, alpha left out.
func (color RGBA) Hex() string {
	return fmt.Sprintf("#%02x%02x%02x", byteOf(color.R), byteOf(color.G), byteOf(color.B))
}

// CSS is the color as rgba(red, green, blue, alpha), the parts of red, green and blue out of 255 and alpha out of 1.
func (color RGBA) CSS() string {
	return fmt.Sprintf("rgba(%d, %d, %d, %s)", byteOf(color.R), byteOf(color.G), byteOf(color.B),
		strconv.FormatFloat(color.A, 'f', -1, 64))
}

// byteOf is a part from 0 to 1 as a byte, rounded.
func byteOf(part float64) int { return int(part*255 + 0.5) }

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

// What a value of the data system is, as a binding's answer and a fact's Returns say it.
const (
	DataBool   = "bool"
	DataNumber = "number"
	DataText   = "text"
	DataList   = "list"
	DataObject = "object"
)

// DataKinds is what each primitive type of the data system is, by its name; any other type is an object, and a list
// where a data model reads it. No dump states which return types are numbers or text.
var DataKinds = map[string]string{
	"bool": DataBool, "int32": DataNumber, "uint32": DataNumber, "int64": DataNumber, "uint64": DataNumber,
	"float": DataNumber, "double": DataNumber, "CFixedPoint": DataNumber, "CString": DataText, "CUTF8String": DataText,
}

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

// FileType is how files of one extension name entries; an extension with no row defines nothing.
type FileType struct {
	NameKey    string            // the key whose value names a top block's entry: .asset name = x
	Keywords   []string          // the words that declare an entry: type x = button { }
	NameKeyOf  map[string]string // a block key at any depth, and the key whose value names its entry
	WidgetKey  string            // every other top block is an entry of <ext>.widget, named by this key's value
	Body       Shape             // the kind its entries' blocks hold
	TagCalls   string            // the word that calls the tag a block is written with (type x = button { }), "" none
	ContextKey string            // the key whose value is the data call the bindings of its block read in, "" none
	Refs       []Ref             // the keys, at any depth, whose value names an entry or a file
}

// FileTypes are how .txt, .asset, .font and .gui files name entries, in every game. In a .gui file
// a word before a top block declares it (template x { }) or groups declarations (types x { }).
var FileTypes = map[string]FileType{
	".txt":   {},
	".asset": {NameKey: "name"},
	".font":  {NameKey: "name"},
	".gui": {
		Keywords:   []string{"type", "local_type", "template", "local_template", "block", "layer"},
		NameKeyOf:  map[string]string{"texticon": "icon", "format": "name"},
		WidgetKey:  "name",
		Body:       ShapeWidget,
		TagCalls:   "widget",
		ContextKey: "datacontext",
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
	Within     string
	Identity   Identity
	NameKey    string   // IdentityByField: the key whose value names the entry
	Wrappers   []string // keys at the top that only wrap entries: their blocks hold top statements
	Keyword    string   // the word that declares an entry inline in any file (scripted_effect x = { })
	Purpose    Purpose
	Of         string   // PurposeHistory: the database whose entries its own set up; "" its own files'
	Dated      bool     // a number key in an entry's own block is a date written short (999, 1077.1)
	Body       Shape    // what an entry's block holds; ShapeUnknown: its own fields
	Callers    []string // scripted: the words a call by an entry's name resolves by
	Parameters bool     // scripted: a call passes parameters
	Local      bool     // its entries answer only in the file that declares them
	Refs       []Ref    // positions in an entry (key/<number>) whose words name an entry of a database
	Names      []string // keys whose values in an entry also name this database's entries (trait groups)
}

// Layout is how one game's script files lay out their entries.
type Layout struct {
	Descriptors []Descriptor
	EntryModes  []EntryMode // in the order the game processes them; file order only within one
	FirstWins   []string    // the databases whose first definition wins; in every other, the last
}

// NameOf is the database a folder or file from a root makes: its path without common/.
func NameOf(path string) string { return strings.TrimPrefix(path, "common/") }

// layouts are each game's layout: the shared rows, with its own over them.
var layouts = map[string]Layout{
	"ck3": {
		Descriptors: withShared([]Descriptor{
			folder("common/on_action", Descriptor{Purpose: PurposeOnActions,
				Refs: []Ref{{At: "on_actions"}, {At: "random_on_action/<number>"}, {At: "first_valid_on_action"}, {At: "fallback"}}}),
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
	folder("events", Descriptor{Purpose: PurposeEvents}),
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

// ---- Widgets: how a built-in widget kind arranges its children, and what a binding answers with no game running.

// LayoutClass is how a built-in widget kind arranges its children.
type LayoutClass uint8

// The layout classes.
const (
	LayoutPlain  LayoutClass = iota // children by their anchors and positions
	LayoutRow                       // children side by side
	LayoutColumn                    // children stacked
	LayoutGrid                      // children in cells
	LayoutFlow                      // children along a direction, wrapping
	LayoutScroll                    // one content, clipped and scrolled
	LayoutText                      // a text
	LayoutImage                     // a texture
)

// LayoutClasses is each built-in kind that arranges its children by other than its anchors, by the kind's name; a
// kind with no row is plain.
var LayoutClasses = map[string]LayoutClass{
	"hbox": LayoutRow, "uberhbox": LayoutRow,
	"vbox": LayoutColumn, "ubervbox": LayoutColumn,
	"fixedgridbox": LayoutGrid, "dynamicgridbox": LayoutGrid,
	"flowcontainer": LayoutFlow,
	"scrollarea":    LayoutScroll, "uberscrollarea": LayoutScroll, "scrollvbox": LayoutScroll,
	"textbox": LayoutText, "ubertextbox": LayoutText, "editbox": LayoutText, "debug_textbox": LayoutText,
	"icon": LayoutImage, "background": LayoutImage,
}

// LayoutPolicy is what a layoutpolicy word means for a widget's sizes in a row or column.
type LayoutPolicy struct {
	Grows   bool // takes extra room
	Shrinks bool // gives room below its preferred size, down to its minimum
	Fixed   bool // its preferred size is its minimum and maximum
}

// LayoutPolicies are the five policy words, read without regard to case.
var LayoutPolicies = map[string]LayoutPolicy{
	"expanding": {Grows: true, Shrinks: true}, "growing": {Grows: true}, "preferred": {Shrinks: true},
	"shrinking": {Shrinks: true}, "fixed": {Fixed: true},
}

// Computation is what the evaluator computes a global function as.
type Computation uint8

// The computations.
const (
	ComputeAnd         Computation = iota // no when a side is no, yes when every side is yes, else unknown
	ComputeOr                             // yes when a side is yes, no when every side is no, else unknown
	ComputeNot                            // the opposite; unknown stays unknown
	ComputeCompare                        // two known numbers or texts compared by the name's verb
	ComputeArithmetic                     // two known numbers combined by the name's verb; unknown otherwise
	ComputeParity                         // IsOdd and IsEven of a known number
	ComputeListCount                      // IsDataModelEmpty no, DataModelHasItems yes, GetDataModelSize the count
	ComputeTextEmpty                      // StringIsEmpty of the text: no for a stand-in
	ComputeValid                          // IsValid: yes, a stand-in exists
	ComputeSelect                         // Select_*(cond, a, b): a or b by a known cond, else a
	ComputeLocalize                       // Localize('KEY'): the key's text
	ComputeConcatenate                    // Concatenate: the texts joined
	ComputeConvert                        // FixedPointToFloat, IntToFixedPoint, FixedPointToInt: the number itself
	ComputeRowIndex                       // GetIndexInDataModel: the data model row this widget is drawn for
)

// ComputedFunction is one global function, or a family of them by prefix, and its computation.
type ComputedFunction struct {
	Name   string
	Prefix bool
	Computation
}

// ComputedFunctions are Jomini's global functions whose meaning is the engine's, not game data; the dumps declare
// their names and return types. An exact name is tried before a prefix.
var ComputedFunctions = []ComputedFunction{
	{Name: "And", Computation: ComputeAnd}, {Name: "Or", Computation: ComputeOr}, {Name: "Not", Computation: ComputeNot},
	{Name: "And", Prefix: true, Computation: ComputeAnd}, {Name: "Or", Prefix: true, Computation: ComputeOr},
	{Name: "ObjectsEqual", Computation: ComputeCompare},
	{Name: "EqualTo_", Prefix: true, Computation: ComputeCompare}, {Name: "NotEqualTo_", Prefix: true, Computation: ComputeCompare},
	{Name: "GreaterThan_", Prefix: true, Computation: ComputeCompare},
	{Name: "GreaterThanOrEqualTo_", Prefix: true, Computation: ComputeCompare},
	{Name: "LessThan_", Prefix: true, Computation: ComputeCompare},
	{Name: "LessThanOrEqualTo_", Prefix: true, Computation: ComputeCompare},
	{Name: "Add_", Prefix: true, Computation: ComputeArithmetic}, {Name: "Subtract_", Prefix: true, Computation: ComputeArithmetic},
	{Name: "Multiply_", Prefix: true, Computation: ComputeArithmetic}, {Name: "Divide_", Prefix: true, Computation: ComputeArithmetic},
	{Name: "Min_", Prefix: true, Computation: ComputeArithmetic}, {Name: "Max_", Prefix: true, Computation: ComputeArithmetic},
	{Name: "Abs_", Prefix: true, Computation: ComputeArithmetic},
	{Name: "IsOdd_", Prefix: true, Computation: ComputeParity}, {Name: "IsEven_", Prefix: true, Computation: ComputeParity},
	{Name: "IsDataModelEmpty", Computation: ComputeListCount}, {Name: "DataModelHasItems", Computation: ComputeListCount},
	{Name: "GetDataModelSize", Computation: ComputeListCount},
	{Name: "StringIsEmpty", Computation: ComputeTextEmpty}, {Name: "IsValid", Computation: ComputeValid},
	{Name: "Select_", Prefix: true, Computation: ComputeSelect}, {Name: "Localize", Computation: ComputeLocalize},
	{Name: "Concatenate", Computation: ComputeConcatenate},
	{Name: "FixedPointToFloat", Computation: ComputeConvert}, {Name: "IntToFixedPoint", Computation: ComputeConvert},
	{Name: "FixedPointToInt", Computation: ComputeConvert},
	{Name: "GetIndexInDataModel", Computation: ComputeRowIndex},
}

// OpeningState is the state a window runs as the game opens it; a state with trigger_on_create = yes runs too.
const OpeningState = "_show"

// StateControls are the keys of a state that run it rather than say how the widget ends up.
var StateControls = []string{"name", "using", "on_start", "on_finish", "duration", "delay", "next", "trigger_on_create"}

// UnsetAtOpen are the function names that read what is unset as the game opens a window: a UI variable or tab
// (Exists, HasValue, IsTabSelected, Get), and the pointer, which is nowhere yet. In the three games' dumps those
// names belong only to the variable systems, the tab holders and the widgets' pointer state.
var UnsetAtOpen = []string{
	"Exists", "HasValue", "IsTabSelected", "Get", "IsTooltipLocked", "IsHovered", "IsMouseOver", "IsHighlighted",
	"IsPressed",
}
