// databases.go holds each game's database tables. They say where definitions are written, how they are named and how
// they combine.

package game

import (
	"path"
	"slices"
	"strings"
)

// A Database is the row for one database's folder. A folder with no row is a database whose top-level keys name its
// definitions.
type Database struct {
	// Houses is what it holds beyond definitions: events, on_actions, decisions, messages, lists, concepts, history or
	// modifiers.
	Houses        string
	NamedBy       string   // how a definition is named: "" means its own key, else field, call, file, column or nothing
	NameKey       string   // the key whose value names a definition, or holds the call that does
	WrapperKeys   []string // keys at the top that only wrap definitions
	DeclaringKeys []string // keys that declare a definition inline: scripted_effect x = { }
	AliasKeys     []string // the keys whose value names a definition a second time
	// RunOrder is a definition's blocks in the order they run. Unlisted keys run after them, as written.
	RunOrder  []string
	Shape     string   // what a definition's block holds, a Shapes key; "" means its own fields
	Target    string   // the database whose definitions its own set up or are named for; "" means none
	FoldsCase bool     // its references match without regard to case
	Prefixes  []string // the link prefixes script writes before a definition's name, as in dynasty:<key>
	// Separator is the character between a definition's name and a key nested in it, as | in define:NAI|X. A prefixed
	// reference names the definition up to it, and a nested definition is named across it. An empty one means neither.
	Separator string
}

// A Nesting is a database whose definitions are keys inside another database's definitions. Its name is the
// parent's name, a dot and Database.
type Nesting struct {
	Parent   string // the folder or file whose definitions hold its keys
	KeyPath  string // the key path from the holding definition to its keys; "" means its block, * any key, ** any depth
	Database string // its own name after the parent's; "" means the parent's own definitions (a title inside a title)
	// Keys is the pattern a key must fit to define one; any other key is a field. With a Database of its own, "" means
	// every key.
	Keys    string
	NameKey string // the key whose value names a definition; "" means the key that defines it
	Target  string // the database whose definitions its own set up; "" means none
}

// A Callable is a database whose definitions script or a GUI file calls by name.
type Callable struct {
	Callers    []string // the words that call a definition by name
	Shape      string   // what a definition's block holds, a Shapes key; "" means the caller's shape
	Parameters bool     // a call passes parameters
	Local      bool     // its definitions answer only in the file that declares them
	FoldsCase  bool     // its references match without regard to case
	Proves     []string // the shapes a block that calls one of its definitions can have
	// Command is the kind of command a definition acts as where it is called, effect or trigger, or "" for neither.
	Command string
}

// A FileType is what a file's extension says about a file, beyond what the databases of its folder say.
type FileType struct {
	NameKey     string // the key whose value names a top block's definition; "" means the block's own key
	ColorSuffix string // the ending of keys whose untagged number block is a color; "" means none
	Shape       string // what a top block holds, a Shapes key; "" means the database's
}

// A Precedence is how definitions of one name combine.
type Precedence struct {
	Keeps       string   // which definition the game keeps: first, last, highest or merged
	ByPlayset   bool     // the order is the playset's, not the load order's
	Modes       string   // where an entry mode is written: entry or wrapped; "" means none
	PriorityKey string   // the key carrying a definition's override priority; "" means none
	Overrides   []string // the keys a higher priority replaces; nil means the whole definition
	// Injects is what a mode that adds does: "" adds, replaces makes INJECT act as REPLACE, and unsettled means the
	// sources do not say for any mode that adds.
	Injects string
	// Contested is the blocks that merged definitions cannot both write. Which copy the game keeps is not documented.
	Contested []string
}

// A Reference is a key path in a database's definitions whose words name definitions.
type Reference struct {
	Database string // the database whose definitions hold the key path
	KeyPath  string // the key path from the definition; <number> matches a number key
	Target   string // the database the words name; "" means the row's own
	Folder   string // the folder a file named with no folder of its own is in; "" means none
}

// An EntryMode is a word written before a definition's name, and what it does to a definition already loaded.
type EntryMode struct {
	Prefix   string // INJECT, REPLACE, TRY_INJECT…
	Replaces bool   // takes the earlier definition's place; else it adds to it
	Optional bool   // does nothing where there is no earlier definition (TRY_)
	Creates  bool   // defines it where there is none (_OR_CREATE)
}

// A PictureStep is one hop from an event toward its picture.
type PictureStep struct {
	// Key is the key path read in the block the hop starts from. A key written key[field=value] reads, of the blocks
	// of that key, the one that writes the value at the field.
	Key      string
	Database string // the database whose definition the key's value names
	Match    string // the key path whose words match the value in each definition; "" means the definition's name
}

// A PictureChain is the hops from an event to a picture file.
type PictureChain struct {
	Steps    []PictureStep
	FileKey  string // the key path, in the last definition, that holds the picture file
	Fallback string // the key that names another definition to use where the file is absent; "" means none
}

// A Picture is how an event's picture is found.
type Picture struct {
	FileKey string // the key that writes a picture file outright; "" means none
	Chains  []PictureChain
}

// A DelayUnit is one key a firing's delay is written with, and how many days one of it is.
type DelayUnit struct {
	Key  string
	Days int
}

// Localization and File are the databases that a loc key and a file path name. No definition carries them.
const (
	Localization = "localization"
	File         = "file"
)

// Constant is the database of the constants a file writes for itself as @name = value, local to the file.
const Constant = "constant"

// GlobalType is the data type a data call's first step is looked up on.
const GlobalType = "Global"

// SelfFunctions are the data functions that return the object they are called on, such as EU5's Goods.Self. The dumps
// say they return [unregistered].
var SelfFunctions = []string{"Self", "AccessSelf"}

// EventNamespaceKey is the key an events file declares a namespace with. A file may declare several. Its events are
// written as <namespace>.N.
const EventNamespaceKey = "namespace"

// TitleKey is the key that names a definition's title localization, as in an event's title = <key>.
const TitleKey = "title"

// DescriptionKey is the key that writes an event's description: a localization key, or a block that chooses one.
const DescriptionKey = "desc"

// LabelKey is the key that holds a text's localization key in a block that picks the text, as in an option's
// name = { trigger = { … } text = <key> } or a custom_tooltip's text.
const LabelKey = "text"

// HiddenKey is the key whose yes makes an event show no window (CK3 events/_events.info:38). The three games write it
// alike.
const HiddenKey = "hidden"

// EventDelay is how a firing's delay is written. A month is counted as 30 days and a year as 365.
var EventDelay = struct {
	Units []DelayUnit // the keys that write how long the firing waits, in the order they are looked for
	Block string      // the key of a block that holds the delay of every firing written after it
}{[]DelayUnit{{"days", 1}, {"weeks", 7}, {"months", 30}, {"years", 365}}, "delay"} // Vic3 effects.log:38529

// GameRules is how the game rules are written and named. The three games write them alike: CK3
// common/game_rules/_game_rules.info:1-20, Vic3 common/game_rules/game_rules.md:1-12 and EU5
// main_menu/common/game_rules/_game_rules.info:1-19.
var GameRules = struct {
	Database      string // the database of the rules, each a block holding a block per setting
	Trigger       string // the trigger that asks whether a rule is set to the setting it names
	DefaultKey    string // the key that names a rule's default setting; with none, the first setting is the default
	CategoriesKey string // the key of the one block in a rule that is no setting
	Setting       string // the localization key of a setting's name, with the setting at %s
	Description   string // the localization key of a setting's description, with the setting at %s
}{"game_rules", "has_game_rule", "default", "categories", "setting_%s", "setting_%s_desc"}

// CustomTexts is how a customizable localization is written: a block for each text it can show, which names the
// text's localization key. The three games write it alike: CK3 common/customizable_localization/_custom_loc.info:18-38,
// EU5 in_game/common/customizable_localization/customizable_localization.info:1-10, and Vic3's own files there.
var CustomTexts = struct {
	Database string // the database of the customizable localizations
	Block    string // the key of a block that holds one of the texts
	Key      string // the key in that block that names the text's localization key
}{"customizable_localization", "text", "localization_key"}

// MisspelledFolders maps folder names the game never reads to the name it reads instead.
var MisspelledFolders = map[string]string{"localisation": "localization"}

// ClassDatabases maps the dumps' class names that no naming rule resolves to their database's path.
var ClassDatabases = map[string]string{
	"CCultureInnovationTypeDatabase":     "culture/innovations",
	"CCultureTraditionDatabase":          "culture/traditions",
	"CCourtPositionTypesDatabase":        "court_positions/types",
	"CTravelPointOfInterestTypeDatabase": "travel/point_of_interest_types",
}

// definitionWords are the singular and plural words for a definition of some databases, by database.
var definitionWords = map[string]struct{ One, Many string }{
	"messages":      {"player message", "player messages"},
	SavedScopes:     {"saved scope", "saved scopes"},
	Variables:       {"variable", "variables"},
	LocalVariables:  {"local variable", "local variables"},
	GlobalVariables: {"global variable", "global variables"},
	Lists:           {"list", "lists"},
	Flags:           {"flag", "flags"},
}

// plurals are the plural endings of a word, each with its singular ending. The first ending that fits wins.
var plurals = []struct{ Plural, One string }{
	// the same in the singular and the plural
	{"arms", "arms"}, {"goods", "goods"}, {"belli", "belli"}, {"criteria", "criteria"}, {"data", "data"},
	// a y after a vowel only takes an s
	{"ays", "ay"}, {"eys", "ey"}, {"oys", "oy"}, {"uys", "uy"},
	{"ouses", "ouse"}, {"eases", "ease"}, {"ies", "y"}, {"ses", "s"}, {"xes", "x"}, {"ches", "ch"}, {"shes", "sh"},
	{"s", ""},
}

// EntryModes are the modes a definition's name may start with. CK3 has none.
var EntryModes = []EntryMode{
	{Prefix: "INJECT_OR_CREATE", Creates: true}, {Prefix: "REPLACE_OR_CREATE", Replaces: true, Creates: true},
	{Prefix: "TRY_INJECT", Optional: true}, {Prefix: "TRY_REPLACE", Replaces: true, Optional: true},
	{Prefix: "INJECT"}, {Prefix: "REPLACE", Replaces: true},
}

// FireHouses are the Houses of the databases that fire and are fired. A message is fired when an effect sends it.
var FireHouses = []string{"events", "on_actions", "decisions", "messages"}

// TableSyntax is how a table file such as CK3's map_data/definition.csv writes its rows. The first column names a row.
var TableSyntax = struct{ Separator, Comment string }{";", "#"}

// The words that call a scripted database's definitions.
const (
	callTrigger = "trigger"
	callEffect  = "effect"
	callValue   = "value"
)

// The blocks of an on_action and of an event in the order they run (the games' own documents).
var (
	onActionOrder = []string{"trigger", "effect", "events"}
	ck3EventOrder = []string{"trigger", "immediate", "title", "desc", "option", "after"}
	eventOrder    = []string{"trigger", "immediate", "option", "after"}
)

// onActionPrecedence is how on_actions combine: their lists merge, and two trigger or effect blocks conflict.
var onActionPrecedence = Precedence{Keeps: "merged", Contested: []string{"trigger", "effect"}}

// mergedPrecedence is how a database combines whose copies all stay.
var mergedPrecedence = Precedence{Keeps: "merged"}

// replacingInjectPrecedence is how a database that takes entry modes combines where INJECT acts as REPLACE.
var replacingInjectPrecedence = Precedence{Keeps: "first", Modes: "entry", Injects: "replaces"}

// replaceOnlyPrecedence is how a database combines that the Vic3 wiki lists as taking REPLACE only. It does not say
// whether a mode that adds, such as INJECT, is refused or acts as REPLACE.
var replaceOnlyPrecedence = Precedence{Keeps: "first", Modes: "entry", Injects: "unsettled"}

// modePrecedence is how the databases that take entry modes combine: the first plain copy stays.
var modePrecedence = Precedence{Keeps: "first", Modes: "entry"}

// defaultPrecedence is how definitions combine where no row says: the last by file name wins.
var defaultPrecedence = Precedence{Keeps: "last"}

// Databases holds the shared rows for the folders that need one. A game's own rows come first. A row for a folder a
// game lacks matches nothing.
var Databases = map[string]Database{
	"common/scripted_effects":  {DeclaringKeys: []string{"scripted_effect"}},
	"common/scripted_triggers": {DeclaringKeys: []string{"scripted_trigger"}},
	"common/scripted_lists":    {Houses: "lists"},
	// its subfolders write the same define groups. define:Namespace|Name reads one (CK3 event_targets.log:109, the
	// Vic3 wiki's Event target page)
	"common/defines":                     {Prefixes: []string{"define", "array_define"}, Separator: "|"},
	"common/genes":                       {WrapperKeys: []string{"color_genes", "special_genes", "morph_genes", "accessory_genes", "decal_atlases", "age_presets"}},
	"common/named_colors":                {WrapperKeys: []string{"colors"}},
	"data_binding":                       {NamedBy: "call", NameKey: "definition"},
	"common/game_concepts":               {Houses: "concepts"},
	"common/achievement_groups.txt":      {NamedBy: "field", NameKey: "name"},
	"common/coat_of_arms/options":        {NamedBy: "nothing"},
	"common/coat_of_arms/coat_of_arms":   {WrapperKeys: []string{"template"}},
	"common/coat_of_arms/template_lists": {WrapperKeys: []string{"coat_of_arms_template_lists", "pattern_texture_lists", "colored_emblem_texture_lists", "textured_emblem_texture_lists", "color_lists"}},
	"gui/scripted_widgets":               {NamedBy: "nothing"},
	"music/music_player_categories":      {NamedBy: "field", NameKey: "id"},
	// its blocks are pattern_textures, pattern_layout and variation, each a database of its own
	"gfx/portraits/accessory_variations": {NamedBy: "nothing"},
}

// Nestings holds the shared nestings. A game's own come before them.
var Nestings = []Nesting{
	{Parent: "common/defines", Database: "define"},
	// each kind of block is named by its name key, as CK3 afr.txt:11-12 writes, and two kinds may share a name
	{Parent: "gfx/portraits/accessory_variations", Database: "pattern_textures", Keys: "pattern_textures", NameKey: "name"},
	{Parent: "gfx/portraits/accessory_variations", Database: "pattern_layout", Keys: "pattern_layout", NameKey: "name"},
	{Parent: "gfx/portraits/accessory_variations", Database: "variation", Keys: "variation", NameKey: "name"},
}

// Callables lists the callable databases by name.
var Callables = map[string]Callable{
	"scripted_effects": {
		Callers: []string{callEffect}, Shape: "effects", Parameters: true, Proves: []string{ShapeEffects}, Command: callEffect,
	},
	// A value block calls them too.
	"scripted_triggers": {
		Callers: []string{callTrigger, callValue}, Shape: "triggers", Parameters: true, Proves: []string{ShapeTriggers, ShapeValue},
		Command: callTrigger,
	},
	"scripted_modifiers": {Callers: []string{callValue}, Parameters: true, Proves: []string{ShapeValue}},
	"script_values":      {Callers: []string{callTrigger, callValue}, Shape: "value", Proves: []string{ShapeTriggers, ShapeValue}},
	"gui.type":           {Callers: []string{guiTagCalls}, FoldsCase: true}, // a widget key naming a type
	"gui.local_type":     {Callers: []string{guiTagCalls}, Local: true},
	"gui.template":       {Callers: []string{guiTemplateCalls}},
	"gui.local_template": {Callers: []string{guiTemplateCalls}, Local: true},
	"gui.block":          {Callers: []string{guiBlockCalls}, FoldsCase: true},
}

// FileTypes says what each file extension means. A key ending in color holds a color even without a tag, except a
// portrait gene's.
var FileTypes = map[string]FileType{
	".txt":   {ColorSuffix: "color"},
	".asset": {NameKey: "name"},
	".font":  {NameKey: "name"},
	".gui":   {Shape: ShapeWidget, ColorSuffix: "color"},
}

// Precedences holds the shared precedences, by database. A game's own come first.
var Precedences = map[string]Precedence{
	// a group is opened in several files: CK3 common/defines/00_defines.txt:1793 and graphic/00_graphics.txt:1124, Vic3
	// common/defines/00_defines.txt:1382,1749, the EU5 wiki's Defines, l.86
	"defines": mergedPrecedence,
	// one define, named NGroup|KEY, keeps the last copy by file name: the CK3 wiki's Modding page, the EU5 wiki's
	// Defines, l.94, and the Vic3 wiki's Defines (1.13)
	"defines.define": defaultPrecedence,
	// a block belongs to its type, so many types write a block of the same name
	"gui.block": mergedPrecedence,
}

// References holds the shared references. A game's own come after them.
var References = []Reference{
	{Database: "on_action", KeyPath: "on_actions"}, {Database: "on_action", KeyPath: "random_on_action/<number>"},
	{Database: "on_action", KeyPath: "first_valid_on_action"}, {Database: "on_action", KeyPath: "fallback"},
	{Database: "on_actions", KeyPath: "on_actions"}, {Database: "on_actions", KeyPath: "random_on_actions/<number>"},
	{Database: "on_actions", KeyPath: "first_valid_on_action"}, {Database: "on_actions", KeyPath: "fallback"},
}

var ck3Picture = Picture{ // _events.info, _event_themes.info
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
}

var vic3Picture = Picture{
	Chains: []PictureChain{{
		Steps:   []PictureStep{{Key: "event_image/video", Database: "gfx/media_aliases"}},
		FileKey: "video", Fallback: "fallback",
	}},
}

var eu5Picture = Picture{
	FileKey: "image",
	Chains: []PictureChain{{
		Steps: []PictureStep{
			{Key: "illustration_tags", Database: "gfx/illustrations/database", Match: "tags"},
			{Key: "scene", Database: "gfx/scenes"},
			// a scene's layers are written front to back, and its backdrop is the one drawn on background2_mesh, as
			// in_game/gfx/scenes/00_scenes_events.txt writes each of its characters_ scenes
			{Key: "ecs_illustration_image[pdxmesh=background2_mesh]/list", Database: "gfx/images", Match: "tag"},
		},
		FileKey: "illustration_image/texture_file",
	}},
}

var ck3Databases = map[string]Database{ // its .info files unless noted
	"common/on_action": {Houses: "on_actions", RunOrder: onActionOrder},
	"events":           {Houses: "events", RunOrder: ck3EventOrder},
	// _messages.info:1, sent by send_interface_message, send_interface_toast and send_interface_popup (effects.log:9999)
	"common/messages": {Houses: "messages"},
	// _decisions.info:194,200,208
	"common/decisions": {
		Houses: "decisions", RunOrder: []string{"is_shown", "is_valid", "cost", "cooldown", "effect"},
	},
	// a group is also a name, which has_trait holds for any trait in it (_traits.info:3,181,183)
	"common/traits":            {AliasKeys: []string{"group", "group_equivalence"}},
	"common/opinion_modifiers": {}, // the DLC subfolder writes the same blocks
	"gfx/map/map_object_data":  {NamedBy: "nothing"},
	"history/titles":           {Houses: "history", Target: "landed_titles"},
	"history/provinces":        {Houses: "history", Target: "map_data/definition"},
	"history/province_mapping": {Houses: "history", Target: "map_data/definition"},
	"history/cultures":         {Houses: "history", Target: "culture/cultures", NamedBy: "file"},
	"history/faiths":           {Houses: "history", Target: "religion/faith_types"},
	"history/artifacts":        {Houses: "history"},
	"history/situations":       {Houses: "history"},
	"history/struggles":        {Houses: "history"},
	"history/wars":             {Houses: "history", NamedBy: "nothing"},
	// history/_history.info:3 "All history files have the same format"
	"history/characters":      {Houses: "history", Shape: "setup"},
	"common/reader_export":    {}, // no folder: _reader_export.info says what the reader loads before all other files
	"map_data/definition.csv": {NamedBy: "column"},
	// common/province_terrain/_province_terrain.info:3 "This is not a regular keyed database"
	"common/province_terrain": {NamedBy: "nothing"},
	// common/coat_of_arms/dynamic_definitions/_dynamic_definitions.info:1 "Name must match a landed title definition"
	"common/coat_of_arms/dynamic_definitions": {Target: "landed_titles"},
	// scene_settings_byzantine_1_g1.txt:1-6 writes one setting's fields
	"gfx/court_scene/scene_settings": {NamedBy: "file"},
	"gfx/map/environment":            {NamedBy: "file"},                 // gfx/map/environment/environment.info:1
	"common/dynasties":               {Prefixes: []string{"dynasty"}},   // _dynasties.info:3
	"common/dynasty_houses":          {Prefixes: []string{"house"}},     // _dynasty_houses.info:3
	"common/situation/situations":    {Prefixes: []string{"situation"}}, // _situations.info:206
}

var ck3Nestings = []Nesting{
	{Parent: "common/landed_titles", Keys: "[ekdcb]_*"}, // a title inside a title is a title; its key opens with its tier
	{Parent: "common/activities/activity_types", KeyPath: "options", Database: "option_category"},
	{Parent: "common/activities/activity_types", KeyPath: "phases", Database: "phase"},
	{Parent: "common", KeyPath: "**/parameters", Database: "parameter"},
	{Parent: "common", KeyPath: "**/*_parameters", Database: "parameter"},
	{Parent: "common/game_concepts", KeyPath: "alias"}, // an alias is a concept
}

var ck3Precedences = map[string]Precedence{
	"on_action": onActionPrecedence,
	"events":    {Keeps: "highest", PriorityKey: "id_override_priority"}, // events/_events.info:77-83
	"history/characters": {
		Keeps: "highest", PriorityKey: "history_override_priority", // history/_characters.info:25-31
		Overrides: []string{
			"name", "dna", "female", "martial", "prowess", "diplomacy", "intrigue", "stewardship", "learning", "trait",
			"father", "mother", "disallow_random_traits", "obscured", "birth",
		},
	},
	// the wiki's Modding page: types in UI files use FIOS; ck3.exe "Type '%s' already registered at '%s'"
	"gui.type": {Keeps: "first"},
	// the wiki's Modding page; ck3.exe "Template '%s' already registered at '%s'"
	"gui.template": {Keeps: "first"},
	// common/religion/doctrine_types/20_doctrines.txt:2066,2079,2102 set coronation_artifact in three doctrines
	"common.parameter": mergedPrecedence,
	// each activity holds its own categories, which need unique names only within it (_activity_type.info:138-141)
	"activities/activity_types.option_category": mergedPrecedence,
}

var vic3Databases = map[string]Database{
	"common/on_actions":       {Houses: "on_actions", RunOrder: onActionOrder}, // _on_actions.md
	"events":                  {Houses: "events", RunOrder: eventOrder},        // the wiki
	"common/decisions":        {Houses: "decisions"},
	"common/messages":         {Houses: "messages"}, // post_notification sends one (effects.log:27072)
	"gfx/map/map_object_data": {NamedBy: "nothing"},
	"gfx/map/post_effects":    {NamedBy: "field", NameKey: "name", WrapperKeys: []string{"posteffect_volumes"}},
	"common/history":          {Houses: "history", NamedBy: "nothing", Shape: "effects"}, // COUNTRIES = { … }: effects
	"tools/scripted_tests":    {NamedBy: "file"},                                         // scripted_tests.md:2 "Every file is a test suite"
	// modifier_types.md:1-3
	"common/modifier_type_definitions": {Houses: "modifiers"},
	// map_data/province_terrains.txt:21698 defines x7F25CD, common/history/states/00_states.txt:137 writes x7f25cd
	"map_data/province_terrains.txt": {FoldsCase: true},
	"common/strategic_regions":       {Prefixes: []string{"sr"}}, // common/geographic_regions/geographic_regions.md:8
	"common/static_modifiers":        {Shape: ShapeModifiers},    // modifier_types.md:2
	// common/geographic_regions/geographic_regions.md:4 names its lists by the short_key: every_country_in_<short_key>
	"common/geographic_regions": {AliasKeys: []string{"short_key"}},
}

// vic3Precedences cite the wiki's Mod compatibility page, verified for 1.13, by its section, unless noted.
var vic3Precedences = map[string]Precedence{
	"on_actions":              onActionPrecedence,
	"events":                  {Keeps: "first"},          // Events: the first one loaded wins
	"scripted_rules":          replaceOnlyPrecedence,     // the list of folders: REPLACE only
	"technology/technologies": modePrecedence,            // the list of folders: common/technology
	"technology/eras":         modePrecedence,            // the list of folders: common/technology
	"strategic_regions":       modePrecedence,            // the list of folders: common/strategic_regions
	"gui.type":                {Keeps: "first"},          // GUI types: the one loaded first wins
	"gui.template":            {Keeps: "first"},          // GUI types
	"script_values":           replaceOnlyPrecedence,     // the list of folders: REPLACE only
	"scripted_effects":        replacingInjectPrecedence, // Behavior: INJECT acts as REPLACE
	"scripted_triggers":       replacingInjectPrecedence, // Behavior
	// Genes: modes at the second level; victoria3.exe "If you're replacing or injecting this error is shown"
	"genes": {Keeps: "first", Modes: "wrapped"},
	// DNA data: defined character DNAs do not overwrite each other; the list of folders gives it entry modes
	"dna_data": {Keeps: "merged", Modes: "entry"},
}

// ck3References are the CK3 key paths whose values name files or another database's definitions, such as a trait's
// icon (_traits.info:4,23-35) and an event portrait's animation.
var ck3References = []Reference{
	{Database: "traits", KeyPath: "icon", Target: File, Folder: "gfx/interface/icons/traits"},
	{Database: "traits", KeyPath: "icon/**/desc", Target: File, Folder: "gfx/interface/icons/traits"},
	// events/_events.info:101,107: a portrait's animation is an "animation name … See animations.txt"
	{Database: "events", KeyPath: "*_portrait/animation", Target: "gfx/portraits/portrait_animations"},
	{Database: "events", KeyPath: "*_portrait/triggered_animation/animation", Target: "gfx/portraits/portrait_animations"},
	{Database: "events", KeyPath: "sender/animation", Target: "gfx/portraits/portrait_animations"},
	{Database: "events", KeyPath: "sender/triggered_animation/animation", Target: "gfx/portraits/portrait_animations"},
}

// ck3FiringFields are the CK3 fields that fire the events, on_actions and decisions they name.
var ck3FiringFields = []Reference{
	// common/council_tasks/00_chancellor_tasks.txt:150
	{Database: "council_tasks", KeyPath: "monthly_on_action"},
	// common/schemes/scheme_types/coerce_contribution_scheme.txt:143
	{Database: "schemes/scheme_types", KeyPath: "on_monthly/**"},
	// common/defines/00_defines.txt:1813-1816: "Default event to run when a player province is infected"
	{Database: "defines.define", KeyPath: "DEFAULT_HOLDER_INFECTION_EVENT"},
	{Database: "defines.define", KeyPath: "DEFAULT_LIEGE_INFECTION_EVENT"},
	{Database: "defines.define", KeyPath: "DEFAULT_REALM_INFECTION_EVENT"},
	// common/puppets/actions/puppet_actions.txt:26
	{Database: "puppets/actions", KeyPath: "decision"},
}

var vic3References = []Reference{
	{Database: "events", KeyPath: "title", Target: Localization},
	{Database: "events", KeyPath: "desc", Target: Localization},
	{Database: "events", KeyPath: "flavor", Target: Localization},
	{Database: "events", KeyPath: "option/name", Target: Localization},
	// common/flag_definitions/00_flag_definitions.txt:5,7,10
	{Database: "flag_definitions", KeyPath: "flag_definition/coa", Target: Coat.Database},
	{Database: "flag_definitions", KeyPath: "flag_definition/coa_with_overlord_canton", Target: Coat.Database},
	{Database: "flag_definitions", KeyPath: "flag_definition/subject_canton", Target: Coat.Database},
}

// vic3FiringFields are the Victoria 3 fields that fire the events they name (journal_entries.md:354-375).
var vic3FiringFields = []Reference{
	// common/journal_entries/journal_entries.md:357
	{Database: "journal_entries", KeyPath: "on_weekly_pulse/random_events/<number>"},
	// common/journal_entries/00_belle_epoque.txt:215
	{Database: "journal_entries", KeyPath: "on_weekly_pulse/events"},
	// common/journal_entries/00_acw_entries.txt:61
	{Database: "journal_entries", KeyPath: "on_monthly_pulse/random_events/<number>"},
	// common/journal_entries/00_acw_entries.txt:120
	{Database: "journal_entries", KeyPath: "on_monthly_pulse/events"},
	// common/journal_entries/00_acw_entries.txt:68
	{Database: "journal_entries", KeyPath: "on_yearly_pulse/random_events/<number>"},
	// common/journal_entries/journal_entries.md:373-375
	{Database: "journal_entries", KeyPath: "on_yearly_pulse/events"},
}

// vic3ModeFolders are the wiki's Mod compatibility page's list of folders, verified for 1.13.
var vic3ModeFolders = ModeFolders{
	Paths: []string{
		"common/acceptance_statuses", "common/achievements", "common/ai_strategies", "common/alert_groups",
		"common/alert_types", "common/amendments", "common/battle_conditions", "common/building_groups",
		"common/buildings", "common/buy_packages", "common/character_interactions", "common/character_templates",
		"common/character_traits", "common/cohesion_levels", "common/combat_unit_experience_levels",
		"common/combat_unit_groups", "common/combat_unit_types", "common/commander_orders", "common/commander_ranks",
		"common/company_charter_types", "common/company_types", "common/country_creation",
		"common/country_definitions", "common/country_formation", "common/country_ranks", "common/country_types",
		"common/culture_graphics", "common/cultures", "common/decisions", "common/decrees",
		"common/diplomatic_actions", "common/diplomatic_catalyst_categories", "common/diplomatic_catalysts",
		"common/diplomatic_plays", "common/discrimination_trait_groups", "common/discrimination_traits",
		"common/dna_data", "common/dynamic_company_names", "common/dynamic_country_map_colors",
		"common/dynamic_country_names", "common/dynamic_treaty_names", "common/ethnicities",
		"common/flag_definitions", "common/game_concepts", "common/genes", "common/geographic_regions",
		"common/goods", "common/government_types", "common/harvest_condition_types", "common/ideologies",
		"common/institutions", "common/interest_group_traits", "common/interest_groups", "common/journal_entries",
		"common/journal_entry_groups", "common/labels", "common/law_groups", "common/laws",
		"common/legitimacy_levels", "common/liberty_desire_levels", "common/military_formation_flags",
		"common/mobilization_option_groups", "common/mobilization_options", "common/modifier_type_definitions",
		"common/objective_subgoal_categories", "common/objective_subgoals", "common/objectives", "common/parties",
		"common/political_lobbies", "common/political_lobby_appeasement", "common/political_movement_categories",
		"common/political_movement_pop_support", "common/political_movements", "common/pop_needs",
		"common/pop_types", "common/power_bloc_coa_pieces", "common/power_bloc_identities",
		"common/power_bloc_map_textures", "common/power_bloc_names", "common/power_bloc_principle_groups",
		"common/power_bloc_principles", "common/prestige_goods", "common/production_method_groups",
		"common/production_methods", "common/proposal_types", "common/religions", "common/script_values",
		"common/scripted_buttons", "common/scripted_effects", "common/scripted_guis", "common/scripted_lists",
		"common/scripted_modifiers", "common/scripted_progress_bars", "common/scripted_rules",
		"common/scripted_triggers", "common/social_classes", "common/social_hierarchies", "common/state_traits",
		"common/static_modifiers", "common/strategic_regions", "common/subject_types", "common/technology",
		"common/terrain", "common/terrain_manipulators", "common/themes", "common/treaty_articles",
		"common/trigger_localization", "common/tutorial_lesson_chains", "common/tutorial_lessons",
		"common/war_goal_types", "gfx/map/army_dioramas", "gfx/map/city_data/city_building_vfx",
		"gfx/map/city_data/city_types", "gfx/map/fleet_dioramas", "gfx/map/fleet_entities", "gfx/map/front_entities",
		"gfx/portraits/accessories", "gfx/portraits/portrait_modifiers", "gui_animations", "modifier_icons", "music",
		"notifications", "sound/persistent_objects",
	},
}

var eu5Databases = map[string]Database{
	"common/on_action":    {Houses: "on_actions", RunOrder: onActionOrder}, // _on_actions.info
	"events":              {Houses: "events", RunOrder: eventOrder},        // _events.info
	"gfx/map/map_objects": {NamedBy: "nothing"},
	"gfx/map/city_data":   {NamedBy: "field", NameKey: "name"},
	// A reference to a concept matches in either case.
	"common/game_concepts": {Houses: "concepts", FoldsCase: true},
	// Its top keys are the managers that hold the nestings.
	"setup/start": {Houses: "history", NamedBy: "nothing"},
	// Its subfolders write the same attachment lists.
	"gfx/unit_graphics/attachments":        {},
	"setup/templates":                      {NamedBy: "file"},
	"map_data/location_templates.txt":      {Houses: "history", Target: "map_data/named_locations"},
	"map_data/generated_locators_port.txt": {NamedBy: "nothing"},
	// 00_modifier_types.txt:1
	"common/modifier_type_definitions": {
		Houses: "modifiers",
	},
	"common/prices": {Prefixes: []string{"price"}}, // in_game/common/bureaucracies/readme.txt:16
	// in_game/common/international_organizations/readme.txt:182
	"common/international_organizations": {Prefixes: []string{"international_organization"}},
}

var eu5Nestings = []Nesting{ // the start setup's managers each set up the database their Target names
	{Parent: "gfx/defines", Database: "define"},
	{Parent: "common/laws", Database: "policy"},
	{Parent: "common/languages", KeyPath: "dialects", Database: "dialect"},
	{Parent: "common/gods", KeyPath: "omens", Database: "omen"},
	{Parent: "common/building_types", KeyPath: "unique_production_methods", Database: "unique_production_method"},
	{Parent: "common/game_concepts", KeyPath: "alias"}, // an alias is a concept
	// map_data/definitions.txt:1-5, europe > western_europe > scandinavian_region > svealand_area > uppland_province
	{Parent: "map_data/definitions.txt", Database: "continent"},
	{Parent: "map_data/definitions.txt", KeyPath: "*", Database: "sub_continent"},
	{Parent: "map_data/definitions.txt", KeyPath: "*/*", Database: "region"},
	{Parent: "map_data/definitions.txt", KeyPath: "*/*/*", Database: "area"},
	{Parent: "map_data/definitions.txt", KeyPath: "*/*/*/*", Database: "province"},
	{Parent: "setup/start", KeyPath: "character_db", Database: "character"},
	{Parent: "setup/start", KeyPath: "dynasty_manager", Database: "dynasty"},
	{Parent: "setup/start", KeyPath: "countries/countries", Database: "country", Target: "setup/countries"},
	{Parent: "setup/start", KeyPath: "locations", Database: "location", Target: "map_data/named_locations"},
	{Parent: "setup/start", KeyPath: "road_network", Database: "road", Target: "map_data/named_locations"},
	{Parent: "setup/start", KeyPath: "townrights_manager", Database: "town_rights", Target: "map_data/named_locations"},
	{Parent: "setup/start", KeyPath: "building_manager", Database: "building", Target: "building_types"},
	{Parent: "setup/start", KeyPath: "work_of_art_manager", Database: "work_of_art", Target: "artist_work"},
	{Parent: "setup/start", KeyPath: "colony_manager", Database: "colony", Target: "map_data/definitions.province"},
	{Parent: "setup/start", KeyPath: "institution_manager/institutions", Database: "institution", Target: "institution"},
	{Parent: "setup/start", KeyPath: "situation_manager", Database: "situation", Target: "situations"},
}

// eu5Precedences cite the wiki's Mod compatibility page by line, unless noted.
var eu5Precedences = map[string]Precedence{
	"on_action":                 onActionPrecedence,
	"events":                    {Keeps: "first"},                 // l.169: the first one loaded wins
	"gui.type":                  {Keeps: "last", ByPlayset: true}, // l.155: the last by playlist position
	"gui.template":              {Keeps: "last", ByPlayset: true}, // l.155
	"coat_of_arms/coat_of_arms": {Keeps: "last"},                  // the wiki's Flag modding, l.126
	"scripted_effects":          replacingInjectPrecedence,        // l.92: INJECT acts as REPLACE
	"scripted_triggers":         replacingInjectPrecedence,        // l.92
	// dlc/D008_fate_of_the_phoenix/in_game/common/genes/D008_genes_accessories.txt:1-3 writes INJECT_OR_CREATE inside
	// accessory_genes
	"genes": {Keeps: "first", Modes: "wrapped"},
}

var eu5References = []Reference{
	{Database: "events", KeyPath: "title", Target: Localization},
	{Database: "events", KeyPath: "desc", Target: Localization},
	{Database: "events", KeyPath: "historical_info", Target: Localization},
	{Database: "events", KeyPath: "option/name", Target: Localization},
}

// eu5ModeFolders are the folders whose definitions take entry modes: common's, but on_action and defines. The wiki's
// Mod structure, l.78, says modes work in the vast majority of common's subfolders, with those two the exceptions.
var eu5ModeFolders = ModeFolders{Roots: []string{"common"}, Except: []string{"on_action", "defines"}}

// ToDatabase returns the database name of a folder or file. It is the path without common/ and without the extension.
func ToDatabase(folder string) string {
	database := strings.TrimPrefix(folder, "common/")
	return strings.TrimSuffix(database, path.Ext(database))
}

// FindDatabase returns the row for a folder or file, the game's own before the shared one. It returns false if there
// is none.
func (game *Game) FindDatabase(folder string) (Database, bool) {
	if row, ok := game.Databases[folder]; ok {
		return row, true
	}
	row, ok := Databases[folder]
	return row, ok
}

// FindNestings returns the nestings whose parent database holds a file, the game's own before the shared ones.
func (game *Game) FindNestings(rel string) (rows []Nesting) {
	for _, row := range slices.Concat(game.Nestings, Nestings) {
		if rel == row.Parent || strings.HasPrefix(rel, row.Parent+"/") {
			rows = append(rows, row)
		}
	}
	return rows
}

// MatchKeyPath reports whether the keys that lead to a statement fit a key path. A segment may be a glob, <number>,
// or ** for any depth.
func MatchKeyPath(pattern string, keys []string) bool {
	if pattern == "" {
		return len(keys) == 0
	}
	return matchSegments(strings.Split(pattern, "/"), keys)
}

// matchSegments reports whether keys fit the segments of a key path.
func matchSegments(segments, keys []string) bool {
	if len(segments) == 0 {
		return len(keys) == 0
	}
	if segments[0] == "**" {
		for skip := 0; skip <= len(keys); skip++ {
			if matchSegments(segments[1:], keys[skip:]) {
				return true
			}
		}
		return false
	}
	if len(keys) == 0 {
		return false
	}
	fits, _ := path.Match(segments[0], keys[0])
	if segments[0] == NumberKey {
		fits = IsNumberKey(keys[0])
	}
	return fits && matchSegments(segments[1:], keys[1:])
}

// KeepsEvery reports whether the game keeps every copy of a name, merged into one.
func (precedence Precedence) KeepsEvery() bool { return precedence.Keeps == "merged" }

// LookupPrecedence returns how a database's definitions combine, or the default if no row says.
func (game *Game) LookupPrecedence(database, folder string) Precedence {
	if row, ok := game.Precedences[database]; ok {
		return row
	}
	if row, ok := Precedences[database]; ok {
		return row
	}
	if root, _, nested := strings.Cut(database, "/"); nested && slices.Contains(game.MergedRoots, root) {
		return mergedPrecedence
	}
	if game.takesModes(database, folder) {
		return modePrecedence
	}
	return defaultPrecedence
}

// takesModes reports whether a database's folder takes entry modes.
func (game *Game) takesModes(database, folder string) bool {
	folders := game.ModeFolders
	switch {
	case folder == "", slices.Contains(folders.Except, database), slices.Contains(folders.Except, ToDatabase(folder)):
		return false
	case len(folders.Paths) > 0:
		return slices.ContainsFunc(folders.Paths, func(listed string) bool {
			return folder == listed || strings.HasPrefix(folder, listed+"/")
		})
	}
	root, _, _ := strings.Cut(folder, "/")
	return slices.Contains(folders.Roots, root)
}

// SplitMode splits a mode prefix off a written key, so REPLACE:x gives the name x with the mode REPLACE.
func (game *Game) SplitMode(key string) (mode, name string) {
	prefix, rest, cut := strings.Cut(key, ":")
	if cut && slices.ContainsFunc(game.EntryModes, func(entryMode EntryMode) bool { return entryMode.Prefix == prefix }) {
		return prefix, rest
	}
	return "", key
}

// LookupEntryMode returns the entry mode a prefix names. It returns false for "" and for a word no game uses.
func LookupEntryMode(prefix string) (EntryMode, bool) {
	position := slices.IndexFunc(EntryModes, func(mode EntryMode) bool { return mode.Prefix == prefix })
	if position < 0 {
		return EntryMode{}, false
	}
	return EntryModes[position], true
}

// DescribeDefinition returns what a definition of a database is called, such as "scripted effect", in the plural if
// asked. It returns "" for a database with no name.
func DescribeDefinition(database string, plural bool) string {
	if words, ok := definitionWords[database]; ok && plural {
		return words.Many
	} else if ok {
		return words.One
	}
	last := database[strings.LastIndexAny(database, "/.")+1:]
	one := last
	for _, ending := range plurals {
		if stem, cut := strings.CutSuffix(last, ending.Plural); cut {
			one = stem + ending.One
			break
		}
	}
	if !plural {
		return strings.ReplaceAll(one, "_", " ")
	}
	// a word already in the singular takes the plural of its ending
	if one == last && last != "" {
		for _, ending := range plurals {
			if stem, cut := strings.CutSuffix(last, ending.One); cut {
				last = stem + ending.Plural
				break
			}
		}
	}
	return strings.ReplaceAll(last, "_", " ")
}
