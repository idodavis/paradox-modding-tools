// databases.go holds the database tables. They say where definitions are written, how they are named and how they
// combine, each row for the games it names.

package game

import (
	"path"
	"slices"
	"strings"

	"github.com/samber/lo"
)

// A Database is the row for one database's folder. A folder with no row is a database whose top-level keys name its
// definitions.
type Database struct {
	Folder        string   // the folder or file its definitions are written in, from a stage
	Games         []string // the games it holds for; none means every game
	Contents      Contents // what it holds beyond definitions; "" means definitions alone
	NamedBy       string   // how a definition is named: "" means its own key, else field, call, file, column or nothing
	NameKey       string   // the key whose value names a definition, or holds the call that does
	RootKey       string   // the key in a definition whose value names the scope its script starts in; "" means none
	WrapperKeys   []string // keys at the top that only wrap definitions
	DeclaringKeys []string // keys that declare a definition inline: scripted_effect x = { }
	AliasKeys     []string // the keys whose value names a definition a second time
	AliasTarget   string   // the database whose name an alias key's value is; "" means its own
	// RunOrder is a definition's blocks in the order they run. Unlisted keys run after them, as written.
	RunOrder []string
	Shape    string // what a definition's block holds, a Shapes key; "" means its own fields
	Target   string // the database whose definitions its own set up or are named for; "" means none
	// Mirrors marks a database whose definitions are named after other databases', so it never wins a vote it shares.
	Mirrors   bool
	FoldsCase bool     // its references match without regard to case
	Prefixes  []string // the link prefixes script writes before a definition's name, as in dynasty:<key>
	// Separator is the character between a definition's name and a key nested in it, as | in define:NAI|X. "" means none.
	Separator string
	// BoolKey is the key a definition writes as yes when the name it defines takes yes or no, and a number otherwise.
	// "" means none.
	BoolKey string
	// Optional marks a database whose names need no definition: where one is missing, the game reads the localization
	// keys built on the name. A reference to such a name never accuses.
	Optional bool
}

// A Contents is what a database holds beyond definitions. The zero Contents is definitions alone.
type Contents string

// The contents a database can hold beyond definitions.
const (
	ContentsEvents    Contents = "events"
	ContentsOnActions Contents = "on_actions"
	ContentsDecisions Contents = "decisions"
	ContentsMessages  Contents = "messages"
	ContentsLists     Contents = "lists"
	ContentsConcepts  Contents = "concepts"
	ContentsModifiers Contents = "modifiers"
)

// A Nesting is a database whose definitions are keys inside another database's definitions. Its name is the
// parent's name, a dot and Database.
type Nesting struct {
	Games    []string // the games it holds for; none means every game
	Parent   string   // the folder or file whose definitions hold its keys
	KeyPath  string   // the key path from the holding definition to its keys; "" means its block, * any key, ** any depth
	Database string   // its own name after the parent's; "" means the parent's own definitions (a title inside a title)
	// Keys is the pattern a key must fit to define one; any other key is a field. With a Database of its own, "" means
	// every key.
	Keys    string
	NameKey string // the key whose value names a definition; "" means the key that defines it
	Target  string // the database whose definitions its own set up; "" means none
	Sets    bool   // its keys switch a name on or give a definition a second name; they define nothing
}

// Label returns the name of the database a nesting defines: the parent's name, then a dot and its own Database, if any.
func (nesting Nesting) Label() string {
	if nesting.Database == "" {
		return ToDatabase(nesting.Parent)
	}
	return ToDatabase(nesting.Parent) + "." + nesting.Database
}

// A Callable is a database whose definitions script or a GUI file calls by name.
type Callable struct {
	Callers    []string // where a definition is called from: a trigger, effect or value block, or a GUI key
	Shape      string   // what a definition's block holds, a Shapes key; "" means the caller's shape
	Parameters bool     // a call passes parameters
	Local      bool     // its definitions answer only in the file that declares them
	FoldsCase  bool     // its references match without regard to case
	Proves     []string // the shapes a block that calls one of its definitions can have
	// Command is the command a definition acts as where it is called, an effect or a trigger, or "" for neither.
	Command string
	Values  []string // what a call writes as its value where it passes no parameters; none means a call writes none
}

// A FileType is what a file's extension says about a file, beyond what the databases of its folder say.
type FileType struct {
	NameKey     string // the key whose value names a top block's definition; "" means the block's own key
	ColorSuffix string // the ending of keys whose untagged number block is a color; "" means none
	Shape       string // what a top block holds, a Shapes key; "" means the database's
}

// A Precedence is how definitions of one name combine in the databases it holds for.
type Precedence struct {
	Games []string // the games it holds for; none means every game
	// Databases is the databases it holds for, by name or by a path.Match pattern, as setup/* for those under setup.
	Databases   []string
	Folders     []string // the folders it holds for, each with everything below it
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

// A Reference is a key path whose values name definitions.
type Reference struct {
	Games []string // the games it holds for; none means every game
	// Database is the database whose definitions hold the key path. "" means a GUI file's widgets, where the key path
	// is one key.
	Database string
	KeyPath  string // the key path from the definition; <number> matches a number key
	Target   string // the database the values name; "" means the row's own
	Folder   string // the folder a file named with no folder of its own is in; "" means none
}

// An EntryMode is a prefix written before a definition's name, and what it does to a definition already loaded.
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
	Match    string // the key path in each definition whose values are matched with the value; "" means its name
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

// GameEvents holds a game's rules for events: where an event writes its root scope and how its picture is found.
type GameEvents struct {
	TypeKey      string  // the key whose value, less TypeSuffix, names the event's root scope; "" means none
	TypeSuffix   string  // dropped from TypeKey's value
	DefaultScope string  // the root scope where neither key is written
	Picture      Picture // how the event's picture is found
}

// Localization and File are the databases that a loc key and a file path name. No definition carries them.
const (
	Localization = "localization"
	File         = "file"
)

// CustomLocalization is the database of the customizable localizations, each a block for each text it can show.
const CustomLocalization = "customizable_localization"

// Constant is the database of the constants a file writes for itself as @name = value, local to the file.
const Constant = "constant"

// GlobalType is the data type a data call's first step is looked up on.
const GlobalType = "Global"

// AnyScopeType is the data type of a scope of any type. Its getters give each type of scope its own data type, as
// Scope.GetCountry gives Country.
const AnyScopeType = "Scope"

// TopScopeType is the data type the SCOPE global gives. Its promotes read the scope saved under a name, as
// SCOPE.sC('heir') does: data_types_script.txt:797-800 in CK3's logs, :54-57 in Vic3's and EU5's.
const TopScopeType = "TopScope"

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

// OptionKey is the key of one of an event's options.
const OptionKey = "option"

// OptionTitleKey is the key that writes an option's title: a localization key, or a block that picks one.
const OptionTitleKey = "name"

// LabelKey is the key that holds a text's localization key in a block that picks the text, as in an option's
// name = { trigger = { … } text = <key> } or a custom_tooltip's text.
const LabelKey = "text"

// HiddenKey is the key whose yes makes an event show no window (CK3 events/_events.info:38). The three games write it
// alike.
const HiddenKey = "hidden"

// EventDelay is how a firing's delay is written.
var EventDelay = struct {
	Units []string // the keys that write how long the firing waits, in the order they are looked for
	Block string   // the key of a block that holds the delay of every firing written after it
}{[]string{"days", "weeks", "months", "years"}, "delay"} // Vic3 effects.log:38529

// GameRules is how the game rules are written and named, alike in the three games: CK3 _game_rules.info:1-20, Vic3
// game_rules.md:1-12 and EU5 main_menu/common/game_rules/_game_rules.info:1-19.
var GameRules = struct {
	Database      string // the database of the rules, each a block holding a block per setting
	Trigger       string // the trigger that asks whether a rule is set to the setting it names
	DefaultKey    string // the key that names a rule's default setting; with none, the first setting is the default
	CategoriesKey string // the key of the one block in a rule that is no setting
	Setting       string // the localization key of a setting's name, with the setting at %s
	Description   string // the localization key of a setting's description, with the setting at %s
}{"game_rules", "has_game_rule", "default", "categories", "setting_%s", "setting_%s_desc"}

// CustomTexts is how a customizable localization is written, alike in the three games: a block for each text it can
// show, naming the text's key (CK3 _custom_loc.info:18-38, EU5 customizable_localization.info:1-10, Vic3's files).
var CustomTexts = struct {
	Block string // the key of a block that holds one of the texts
	Key   string // the key in that block that names the text's localization key
}{"text", "localization_key"}

// MisspelledFolders maps folder names the game never reads to the name it reads instead.
var MisspelledFolders = map[string]string{"localisation": "localization"}

// ClassDatabases maps the dumps' class names that no naming rule resolves to their database's path.
var ClassDatabases = map[string]string{
	"CCultureInnovationTypeDatabase":     "culture/innovations",
	"CCultureTraditionDatabase":          "culture/traditions",
	"CCourtPositionTypesDatabase":        "court_positions/types",
	"CTravelPointOfInterestTypeDatabase": "travel/point_of_interest_types",
}

// definitionNouns are the singular and plural nouns for a definition of some databases, by database.
var definitionNouns = map[string]struct{ One, Many string }{
	"messages":      {"player message", "player messages"},
	SavedScopes:     {"saved scope", "saved scopes"},
	Variables:       {"variable", "variables"},
	LocalVariables:  {"local variable", "local variables"},
	GlobalVariables: {"global variable", "global variables"},
	Lists:           {"list", "lists"},
	Flags:           {"flag", "flags"},
}

// plurals are the plural endings of an English noun, each with its singular ending. The first ending that fits wins.
var plurals = []struct{ Plural, One string }{
	// the same in the singular and the plural
	{"arms", "arms"}, {"goods", "goods"}, {"belli", "belli"}, {"criteria", "criteria"}, {"data", "data"},
	// a y after a vowel only takes an s
	{"ays", "ay"}, {"eys", "ey"}, {"oys", "oy"}, {"uys", "uy"},
	{"ouses", "ouse"}, {"eases", "ease"}, {"ies", "y"}, {"ses", "s"}, {"xes", "x"}, {"ches", "ch"}, {"shes", "sh"},
	{"s", ""},
}

// EntryModes are the modes a definition's name may start with, in a game whose definitions take modes.
var EntryModes = []EntryMode{
	{Prefix: "INJECT_OR_CREATE", Creates: true}, {Prefix: "REPLACE_OR_CREATE", Replaces: true, Creates: true},
	{Prefix: "TRY_INJECT", Optional: true}, {Prefix: "TRY_REPLACE", Replaces: true, Optional: true},
	{Prefix: "INJECT"}, {Prefix: "REPLACE", Replaces: true},
}

// FireContents are the contents of the databases that fire and are fired. A message is fired when an effect sends it.
var FireContents = []Contents{ContentsEvents, ContentsOnActions, ContentsDecisions, ContentsMessages}

// TableSyntax is how a table file such as CK3's map_data/definition.csv writes its rows. The first column names a row.
var TableSyntax = struct{ Separator, Comment string }{";", "#"}

// SetupFolder is the folder the start setup's rows are written for. A bookmark's start setup folder is read as if its
// files were here.
const SetupFolder = "setup/start"

// The callers of the scripted databases' definitions: a trigger block, an effect block and a value block.
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

// Databases holds the rows for the folders that need one, a game's own before every game's. A row for a folder a game
// lacks matches nothing. A game's rows cite its .info files unless noted.
var Databases = []Database{
	// CK3 and EU5 _on_action.info
	{Folder: "common/on_action", Games: ck3AndEU5, Contents: ContentsOnActions, RunOrder: onActionOrder},
	// CK3 events/_events.info:12: scope = scope_type overrides the root scope of an event.
	{Folder: "events", Games: []string{ck3}, Contents: ContentsEvents, RunOrder: ck3EventOrder, RootKey: "scope"},
	// CK3 _messages.info:1, sent by send_interface_message, send_interface_toast and send_interface_popup
	// (effects.log:9999); Vic3's post_notification sends one (effects.log:27072)
	{Folder: "common/messages", Games: ck3AndVic3, Contents: ContentsMessages},
	// _decisions.info:194,200,208
	{
		Folder: "common/decisions", Games: []string{ck3}, Contents: ContentsDecisions,
		RunOrder: []string{"is_shown", "is_valid", "cost", "cooldown", "effect"},
	},
	// a group is also a name, which has_trait holds for any trait in it (_traits.info:3,181,183)
	{Folder: "common/traits", Games: []string{ck3}, AliasKeys: []string{"group", "group_equivalence"}},
	{Folder: "common/opinion_modifiers", Games: []string{ck3}}, // the DLC subfolder writes the same blocks
	{Folder: "gfx/map/map_object_data", Games: ck3AndVic3, NamedBy: "nothing"},
	{Folder: "history/titles", Games: []string{ck3}, Target: "landed_titles"},
	{Folder: "history/provinces", Games: []string{ck3}, Target: "map_data/definition"},
	{Folder: "history/province_mapping", Games: []string{ck3}, Target: "map_data/definition"},
	{Folder: "history/cultures", Games: []string{ck3}, Target: "culture/cultures", NamedBy: "file"},
	{Folder: "history/faiths", Games: []string{ck3}, Target: "religion/faith_types"},
	{Folder: "history/artifacts", Games: []string{ck3}},
	{Folder: "history/situations", Games: []string{ck3}},
	{Folder: "history/struggles", Games: []string{ck3}},
	{Folder: "history/wars", Games: []string{ck3}, NamedBy: "nothing"},
	// history/_history.info:3 "All history files have the same format"
	{Folder: "history/characters", Games: []string{ck3}, Shape: "setup"},
	{Folder: "reader_export", Games: []string{ck3}}, // _reader_export.info: what the reader loads before all other files
	{Folder: "map_data/definition.csv", Games: []string{ck3}, NamedBy: "column"},
	// common/province_terrain/_province_terrain.info:3 "This is not a regular keyed database"
	{Folder: "common/province_terrain", Games: []string{ck3}, NamedBy: "nothing"},
	// common/coat_of_arms/dynamic_definitions/_dynamic_definitions.info:1 "Name must match a landed title definition"
	{Folder: "common/coat_of_arms/dynamic_definitions", Games: []string{ck3}, Target: "landed_titles"},
	// scene_settings_byzantine_1_g1.txt:1-6 writes one setting's fields
	{Folder: "gfx/court_scene/scene_settings", Games: []string{ck3}, NamedBy: "file"},
	// court_scene_environment.txt:1-10 writes one environment's fields
	{Folder: "gfx/court_scene/scene_environment", Games: []string{ck3}, NamedBy: "file"},
	// gfx/map/environment/environment.info:1
	{Folder: "gfx/map/environment", Games: []string{ck3}, NamedBy: "file"},
	// _dynasties.info:3
	{Folder: "common/dynasties", Games: []string{ck3}, Prefixes: []string{"dynasty"}},
	// _situations.info:206
	{Folder: "common/situation/situations", Games: []string{ck3}, Prefixes: []string{"situation"}},
	// Vic3 _on_actions.md
	{Folder: "common/on_actions", Games: []string{vic3}, Contents: ContentsOnActions, RunOrder: onActionOrder},
	// the Vic3 wiki and EU5 _events.info
	{Folder: "events", Games: vic3AndEU5, Contents: ContentsEvents, RunOrder: eventOrder},
	{Folder: "common/decisions", Games: []string{vic3}, Contents: ContentsDecisions},
	{
		Folder: "gfx/map/post_effects", Games: []string{vic3}, NamedBy: "field", NameKey: "name",
		WrapperKeys: []string{"posteffect_volumes"},
	},
	// COUNTRIES = { … }: effects
	{Folder: "common/history", Games: []string{vic3}, NamedBy: "nothing", Shape: "effects"},
	// scripted_tests.md:2 "Every file is a test suite"
	{Folder: "tools/scripted_tests", Games: []string{vic3}, NamedBy: "file"},
	// Vic3 modifier_types.md:1-3,16-17 and EU5 00_modifier_types.txt:1,3
	{Folder: "common/modifier_type_definitions", Games: vic3AndEU5, Contents: ContentsModifiers, BoolKey: "boolean"},
	// map_data/province_terrains.txt:21698 defines x7F25CD, common/history/states/00_states.txt:137 writes x7f25cd
	{Folder: "map_data/province_terrains.txt", Games: []string{vic3}, FoldsCase: true},
	{Folder: "common/static_modifiers", Games: []string{vic3}, Shape: ShapeModifiers}, // modifier_types.md:2
	// common/geographic_regions/geographic_regions.md:4 names its lists by the short_key: every_country_in_<short_key>
	{Folder: "common/geographic_regions", Games: []string{vic3}, AliasKeys: []string{"short_key"}},
	// in_game/common/formable_countries/00_formable_countries.txt:18, tag = SCA: a formable names the country it forms
	{Folder: "common/formable_countries", Games: []string{eu5}, AliasKeys: []string{"tag"}, AliasTarget: "setup/countries"},
	{Folder: "gfx/map/map_objects", Games: []string{eu5}, NamedBy: "nothing"},
	{Folder: "gfx/map/city_data", Games: []string{eu5}, NamedBy: "field", NameKey: "name"},
	// A reference to a concept matches in either case.
	{Folder: "common/game_concepts", Games: []string{eu5}, Contents: ContentsConcepts, FoldsCase: true},
	// Its top keys are the managers that hold the nestings.
	{Folder: SetupFolder, Games: []string{eu5}, NamedBy: "nothing"},
	// Its subfolders write the same attachment lists.
	{Folder: "gfx/unit_graphics/attachments", Games: []string{eu5}},
	{Folder: "setup/templates", Games: []string{eu5}, NamedBy: "file"},
	{Folder: "map_data/location_templates.txt", Games: []string{eu5}, Target: "map_data/named_locations"},
	{Folder: "map_data/generated_locators_port.txt", Games: []string{eu5}, NamedBy: "nothing"},
	// main_menu/common/modifier_icons: 2,771 definitions, 99.2% named after modifier types
	{Folder: "common/modifier_icons", Games: []string{eu5}, Mirrors: true},
	// The type of a customizable localization is the scope its command is used in: CK3 _custom_loc.info:1-2 and EU5
	// customizable_localization.info:3.
	{Folder: "common/customizable_localization", Games: ck3AndEU5, RootKey: "type"},
	// The scope of a scripted GUI is the scope type it is available to: Vic3 scripted_guis.md:3 and EU5
	// scripted_guis.info:2.
	{Folder: "common/scripted_guis", Games: vic3AndEU5, RootKey: "scope"},
	// The type of a parliament agenda or type is the scope its root is: EU5 parliament_agendas/readme.txt:2-5 and
	// parliament_types/readme.txt:2-3.
	{Folder: "common/parliament_agendas", Games: []string{eu5}, RootKey: "type"},
	{Folder: "common/parliament_types", Games: []string{eu5}, RootKey: "type"},
	// The scope of a filter is the type of object it operates on: EU5 gui/filters/readme.txt:12.
	{Folder: "gui/filters", Games: []string{eu5}, RootKey: "scope"},
	{Folder: "common/scripted_effects", DeclaringKeys: []string{"scripted_effect"}},
	{Folder: "common/scripted_triggers", DeclaringKeys: []string{"scripted_trigger"}},
	{Folder: "common/scripted_lists", Contents: ContentsLists},
	// without an entry the game reads the keys built on the name (CK3 _effect_localization.info:9-17,
	// _trigger_localization.info:15); EU5 international_organization_effects.txt:169 names one no entry defines
	{Folder: "common/effect_localization", Optional: true},
	{Folder: "common/trigger_localization", Optional: true},
	// its subfolders write the same define groups. define:Namespace|Name reads one (CK3 event_targets.log:109, the
	// Vic3 wiki's Event target page)
	{Folder: "common/defines", Prefixes: []string{"define", "array_define"}, Separator: "|"},
	{
		Folder:      "common/genes",
		WrapperKeys: []string{"color_genes", "special_genes", "morph_genes", "accessory_genes", "decal_atlases", "age_presets"},
	},
	{Folder: "common/named_colors", WrapperKeys: []string{"colors"}},
	{Folder: "data_binding", NamedBy: "call", NameKey: "definition"},
	{Folder: "common/game_concepts", Contents: ContentsConcepts},
	{Folder: "common/achievement_groups.txt", NamedBy: "field", NameKey: "name"},
	{Folder: "common/coat_of_arms/options", NamedBy: "nothing"},
	{Folder: "common/coat_of_arms/coat_of_arms", WrapperKeys: []string{"template"}, Mirrors: true},
	{
		Folder: "common/coat_of_arms/template_lists",
		WrapperKeys: []string{
			"coat_of_arms_template_lists", "pattern_texture_lists", "colored_emblem_texture_lists",
			"textured_emblem_texture_lists", "color_lists",
		},
	},
	{Folder: "gui/scripted_widgets", NamedBy: "nothing"},
	{Folder: "music/music_player_categories", NamedBy: "field", NameKey: "id"},
	// its blocks are pattern_textures, pattern_layout and variation, each a database of its own
	{Folder: "gfx/portraits/accessory_variations", NamedBy: "nothing"},
}

// Nestings holds the nested databases, a game's own before every game's.
var Nestings = []Nesting{
	// a title inside a title is a title; its key opens with its tier
	{Games: []string{ck3}, Parent: "common/landed_titles", Keys: "[ekdcb]_*"},
	{Games: []string{ck3}, Parent: "common/activities/activity_types", KeyPath: "options", Database: "option_category"},
	{Games: []string{ck3}, Parent: "common/activities/activity_types", KeyPath: "phases", Database: "phase"},
	// common/confederation_types/10_house_bloc_types.txt:21-24 switches parameters on with `name = yes`
	{Games: []string{ck3}, Parent: "common", KeyPath: "**/parameters", Database: "parameter", Sets: true},
	// common/accolade_types/04_ep2_common_attributes.txt:416-418 lists the names it switches on
	{Games: []string{ck3}, Parent: "common", KeyPath: "**/*_parameters", Database: "parameter", Sets: true},
	// common/game_concepts/00_game_concepts.txt:2 gives vassal the second names of an alias
	{Games: ck3AndEU5, Parent: "common/game_concepts", KeyPath: "alias", Sets: true},
	// EU5's start setup's managers each set up the database their Target names
	{Games: []string{eu5}, Parent: "gfx/defines", Database: "define"},
	{Games: []string{eu5}, Parent: "common/laws", Database: "policy"},
	{Games: []string{eu5}, Parent: "common/languages", KeyPath: "dialects", Database: "dialect"},
	{Games: []string{eu5}, Parent: "common/gods", KeyPath: "omens", Database: "omen"},
	{Games: []string{eu5}, Parent: "common/building_types", KeyPath: "unique_production_methods", Database: "unique_production_method"},
	// map_data/definitions.txt:1-5, europe > western_europe > scandinavian_region > svealand_area > uppland_province
	{Games: []string{eu5}, Parent: "map_data/definitions.txt", Database: "continent"},
	{Games: []string{eu5}, Parent: "map_data/definitions.txt", KeyPath: "*", Database: "sub_continent"},
	{Games: []string{eu5}, Parent: "map_data/definitions.txt", KeyPath: "*/*", Database: "region"},
	{Games: []string{eu5}, Parent: "map_data/definitions.txt", KeyPath: "*/*/*", Database: "area"},
	{Games: []string{eu5}, Parent: "map_data/definitions.txt", KeyPath: "*/*/*/*", Database: "province"},
	{Games: []string{eu5}, Parent: SetupFolder, KeyPath: "character_db", Database: "character"},
	{Games: []string{eu5}, Parent: SetupFolder, KeyPath: "dynasty_manager", Database: "dynasty"},
	{Games: []string{eu5}, Parent: SetupFolder, KeyPath: "countries/countries", Database: "country", Target: "setup/countries"},
	{Games: []string{eu5}, Parent: SetupFolder, KeyPath: "locations", Database: "location", Target: "map_data/named_locations"},
	{Games: []string{eu5}, Parent: SetupFolder, KeyPath: "road_network", Database: "road", Target: "map_data/named_locations"},
	{
		Games: []string{eu5}, Parent: SetupFolder, KeyPath: "townrights_manager", Database: "town_rights",
		Target: "map_data/named_locations",
	},
	{Games: []string{eu5}, Parent: SetupFolder, KeyPath: "building_manager", Database: "building", Target: "building_types"},
	{Games: []string{eu5}, Parent: SetupFolder, KeyPath: "work_of_art_manager", Database: "work_of_art", Target: "artist_work"},
	{
		Games: []string{eu5}, Parent: SetupFolder, KeyPath: "colony_manager", Database: "colony",
		Target: "map_data/definitions.province",
	},
	{
		Games: []string{eu5}, Parent: SetupFolder, KeyPath: "institution_manager/institutions", Database: "institution",
		Target: "institution",
	},
	{Games: []string{eu5}, Parent: SetupFolder, KeyPath: "situation_manager", Database: "situation", Target: "situations"},
	{Parent: "common/defines", Database: "define"},
	// each of the three blocks is named by its name key, as CK3 afr.txt:11-12 writes, and two of them may share a name
	{Parent: "gfx/portraits/accessory_variations", Database: "pattern_textures", Keys: "pattern_textures", NameKey: "name"},
	{Parent: "gfx/portraits/accessory_variations", Database: "pattern_layout", Keys: "pattern_layout", NameKey: "name"},
	{Parent: "gfx/portraits/accessory_variations", Database: "variation", Keys: "variation", NameKey: "name"},
}

// Callables lists the callable databases by name.
var Callables = map[string]Callable{
	// a call writes yes and never no, as CK3 events/activities/activity_system_events.txt:151 does
	"scripted_effects": {
		Callers: []string{callEffect}, Shape: "effects", Parameters: true, Proves: []string{ShapeEffects}, Command: callEffect,
		Values: []string{"yes"},
	},
	// A value block calls them too. A call writes yes, or no to test the opposite, as CK3
	// events/activities/chariot_race_activity/chariot_ongoing_events_jp.txt:189 does.
	"scripted_triggers": {
		Callers: []string{callTrigger, callValue}, Shape: "triggers", Parameters: true, Proves: []string{ShapeTriggers, ShapeValue},
		Command: callTrigger, Values: Bools,
	},
	// CK3 common/activities/activity_types/camp_party.txt:638
	"scripted_modifiers": {
		Callers: []string{callValue}, Shape: ShapeValue, Parameters: true, Proves: []string{ShapeValue},
		Values: []string{"yes"},
	},
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

// Precedences holds how definitions combine; the first row that holds for a database wins. The Vic3 rows cite the
// wiki's Mod compatibility page (1.13) by section, and the EU5 rows the same page by line, unless noted.
var Precedences = []Precedence{
	// their lists merge, and two trigger or effect blocks conflict
	{Databases: []string{"on_action", "on_actions"}, Keeps: "merged", Contested: []string{"trigger", "effect"}},
	// events/_events.info:77-83
	{Games: []string{ck3}, Databases: []string{"events"}, Keeps: "highest", PriorityKey: "id_override_priority"},
	// Vic3 Events and EU5 l.169: the first one loaded wins
	{Games: vic3AndEU5, Databases: []string{"events"}, Keeps: "first"},
	{
		Games: []string{ck3}, Databases: []string{"history/characters"},
		Keeps: "highest", PriorityKey: "history_override_priority", // history/_characters.info:25-31
		Overrides: []string{
			"name", "dna", "female", "martial", "prowess", "diplomacy", "intrigue", "stewardship", "learning", "trait",
			"father", "mother", "disallow_random_traits", "obscured", "birth",
		},
	},
	// the CK3 wiki's Modding page: types in UI files use FIOS, and ck3.exe "Type '%s' already registered at '%s'" and
	// "Template '%s' already registered at '%s'"; Vic3 GUI types: the one loaded first wins
	{Games: ck3AndVic3, Databases: []string{"gui.type", "gui.template"}, Keeps: "first"},
	// EU5 l.155: the last by playlist position
	{Games: []string{eu5}, Databases: []string{"gui.type", "gui.template"}, Keeps: "last", ByPlayset: true},
	// a group of defines is opened in several files (CK3 00_defines.txt:1793 and 00_graphics.txt:1124, Vic3
	// 00_defines.txt:1382,1749, the EU5 wiki's Defines, l.86); a GUI block belongs to its type, so many share a name
	{Databases: []string{"defines", "gui.block"}, Keeps: "merged"},
	// the game's own files write one name in several files, as CK3 catholic.txt:107 and crusaders.txt:107 write the
	// pattern_textures catholic_silk_brocade_01, so every copy stays
	{
		Databases: []string{
			"gfx/portraits/accessory_variations.pattern_textures", "gfx/portraits/accessory_variations.pattern_layout",
			"gfx/portraits/accessory_variations.variation",
		},
		Keeps: "merged",
	},
	// CK3 20_doctrines.txt:2066,2079,2102 set coronation_artifact in three doctrines; an activity's categories are
	// unique only within it (_activity_type.info:138-141); dated history entries stack (_history.info:3-13), as
	// 01_admin_titles_tgp.txt:6339,6435 write c_nf_wang_738b_7 twice
	{
		Games: []string{ck3}, Databases: []string{"common.parameter", "activities/activity_types.option_category", "history/*"},
		Keeps: "merged",
	},
	// the EU5 wiki's Setup modding, l.10: its managers are additive; Mod structure, l.78
	{Games: []string{eu5}, Databases: []string{"setup/*"}, Keeps: "merged"},
	// one define, named NGroup|KEY, keeps the last copy by file name: the CK3 wiki's Modding page, the EU5 wiki's
	// Defines, l.94, and the Vic3 wiki's Defines (1.13)
	{Databases: []string{"defines.define"}, Keeps: "last"},
	// Vic3 Behavior and EU5 l.92: INJECT acts as REPLACE
	{Games: vic3AndEU5, Databases: []string{"scripted_effects", "scripted_triggers"}, Keeps: "first", Modes: "entry", Injects: "replaces"},
	// the Vic3 wiki's list of folders: REPLACE only; it does not say whether INJECT is refused or acts as REPLACE
	{Games: []string{vic3}, Databases: []string{"scripted_rules", "script_values"}, Keeps: "first", Modes: "entry", Injects: "unsettled"},
	// modes at the second level: the Vic3 wiki's Genes and victoria3.exe's "If you're replacing or injecting this error
	// is shown"; EU5 D008_genes_accessories.txt:1-3 writes INJECT_OR_CREATE inside accessory_genes
	{Games: vic3AndEU5, Databases: []string{"genes"}, Keeps: "first", Modes: "wrapped"},
	// Vic3 DNA data: defined character DNAs do not overwrite each other; the wiki's list of folders gives it entry modes
	{Games: []string{vic3}, Databases: []string{"dna_data"}, Keeps: "merged", Modes: "entry"},
	// the EU5 wiki's Flag modding, l.126
	{Games: []string{eu5}, Databases: []string{"coat_of_arms/coat_of_arms"}, Keeps: "last"},
	// the Vic3 wiki's list of folders: the first plain copy stays
	{
		Games: []string{vic3}, Keeps: "first", Modes: "entry",
		Folders: []string{
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
	},
	// the EU5 wiki's Mod structure, l.78: modes work in the vast majority of common's subfolders. Its exceptions,
	// on_action and defines, hold rows above.
	{Games: []string{eu5}, Folders: []string{"common"}, Keeps: "first", Modes: "entry"},
}

// References holds the key paths whose values name definitions or files, in the order they are tried: those of GUI
// files, then every game's, then a game's own.
var References = []Reference{
	{KeyPath: guiTemplateCalls, Target: "gui.template"}, {KeyPath: "layer", Target: "gui.layer"},
	{KeyPath: "texture", Target: File}, {KeyPath: guiFontKey, Target: guiFontDatabase},
	{KeyPath: "text", Target: Localization}, {KeyPath: "title", Target: Localization},
	{Database: "on_action", KeyPath: "on_actions"}, {Database: "on_action", KeyPath: "random_on_action/<number>"},
	{Database: "on_action", KeyPath: "first_valid_on_action"}, {Database: "on_action", KeyPath: "fallback"},
	{Database: "on_actions", KeyPath: "on_actions"}, {Database: "on_actions", KeyPath: "random_on_actions/<number>"},
	{Database: "on_actions", KeyPath: "first_valid_on_action"}, {Database: "on_actions", KeyPath: "fallback"},
	// an on_action's event lists name events: CK3 _on_actions.info:47-80 and Vic3 _on_actions.md:58-90 write events,
	// random_events (a weight, then the event) and first_valid; EU5's on_action files write the same lists
	{Database: "on_action", KeyPath: "events", Target: "events"},
	{Database: "on_action", KeyPath: "random_events/<number>", Target: "events"},
	{Database: "on_action", KeyPath: "first_valid", Target: "events"},
	{Database: "on_actions", KeyPath: "events", Target: "events"},
	{Database: "on_actions", KeyPath: "random_events/<number>", Target: "events"},
	{Database: "on_actions", KeyPath: "first_valid", Target: "events"},
	// a customizable localization's texts name their keys: CK3 common/customizable_localization/_custom_loc.info:18-38,
	// EU5 in_game/common/customizable_localization/customizable_localization.info:1-10, and Vic3's own files there
	{Database: CustomLocalization, KeyPath: "text/localization_key", Target: Localization},
	// CK3's trait icons (_traits.info:4,23-35)
	{Games: []string{ck3}, Database: "traits", KeyPath: "icon", Target: File, Folder: "gfx/interface/icons/traits"},
	{Games: []string{ck3}, Database: "traits", KeyPath: "icon/**/desc", Target: File, Folder: "gfx/interface/icons/traits"},
	// CK3 events/_events.info:101,107: a portrait's animation is an "animation name … See animations.txt"
	{Games: []string{ck3}, Database: "events", KeyPath: "*_portrait/animation", Target: "gfx/portraits/portrait_animations"},
	{
		Games: []string{ck3}, Database: "events", KeyPath: "*_portrait/triggered_animation/animation",
		Target: "gfx/portraits/portrait_animations",
	},
	{Games: []string{ck3}, Database: "events", KeyPath: "sender/animation", Target: "gfx/portraits/portrait_animations"},
	{
		Games: []string{ck3}, Database: "events", KeyPath: "sender/triggered_animation/animation",
		Target: "gfx/portraits/portrait_animations",
	},
	// a date entry's liege names the title above it, as CK3 history/titles/00_other_titles.txt:2525 writes e_hre
	{Games: []string{ck3}, Database: "history/titles", KeyPath: "<number>/liege", Target: "landed_titles"},
	{Games: vic3AndEU5, Database: "events", KeyPath: "title", Target: Localization},
	{Games: vic3AndEU5, Database: "events", KeyPath: "desc", Target: Localization},
	{Games: []string{vic3}, Database: "events", KeyPath: "flavor", Target: Localization},
	{Games: []string{eu5}, Database: "events", KeyPath: "historical_info", Target: Localization},
	{Games: vic3AndEU5, Database: "events", KeyPath: "option/name", Target: Localization},
	// EU5 in_game/gfx/map/map_modes/readme.txt:6: a map mode's tooltip_key is a script string, whose values are keys
	{Games: []string{eu5}, Database: "gfx/map/map_modes", KeyPath: "tooltip_key/**/value", Target: Localization},
	// Vic3 common/flag_definitions/00_flag_definitions.txt:5,7,10 and EU5
	// main_menu/common/flag_definitions/00_flag_definitions.txt:5,7,10
	{Games: vic3AndEU5, Database: "flag_definitions", KeyPath: "flag_definition/coa", Target: Coat.Database},
	{Games: vic3AndEU5, Database: "flag_definitions", KeyPath: "flag_definition/coa_with_overlord_canton", Target: Coat.Database},
	{Games: vic3AndEU5, Database: "flag_definitions", KeyPath: "flag_definition/subject_canton", Target: Coat.Database},
}

// FiringFields holds the key paths in the games' definitions where a name fires the event, on_action or decision it
// names, as it would in effect script.
var FiringFields = []Reference{
	// CK3 common/council_tasks/00_chancellor_tasks.txt:150
	{Games: []string{ck3}, Database: "council_tasks", KeyPath: "monthly_on_action"},
	// CK3 common/schemes/scheme_types/coerce_contribution_scheme.txt:143
	{Games: []string{ck3}, Database: "schemes/scheme_types", KeyPath: "on_monthly/**"},
	// CK3 common/defines/00_defines.txt:1813-1816: "Default event to run when a player province is infected"
	{Games: []string{ck3}, Database: "defines.define", KeyPath: "DEFAULT_HOLDER_INFECTION_EVENT"},
	{Games: []string{ck3}, Database: "defines.define", KeyPath: "DEFAULT_LIEGE_INFECTION_EVENT"},
	{Games: []string{ck3}, Database: "defines.define", KeyPath: "DEFAULT_REALM_INFECTION_EVENT"},
	// CK3 common/puppets/actions/puppet_actions.txt:26
	{Games: []string{ck3}, Database: "puppets/actions", KeyPath: "decision"},
	// Vic3 journal_entries.md:354-375: the fields that fire the events they name, as journal_entries.md:357,
	// 00_belle_epoque.txt:215, 00_acw_entries.txt:61,120,68 and journal_entries.md:373-375 write them
	{Games: []string{vic3}, Database: "journal_entries", KeyPath: "on_weekly_pulse/random_events/<number>"},
	{Games: []string{vic3}, Database: "journal_entries", KeyPath: "on_weekly_pulse/events"},
	{Games: []string{vic3}, Database: "journal_entries", KeyPath: "on_monthly_pulse/random_events/<number>"},
	{Games: []string{vic3}, Database: "journal_entries", KeyPath: "on_monthly_pulse/events"},
	{Games: []string{vic3}, Database: "journal_entries", KeyPath: "on_yearly_pulse/random_events/<number>"},
	{Games: []string{vic3}, Database: "journal_entries", KeyPath: "on_yearly_pulse/events"},
}

// ToDatabase returns the database name of a folder or file. It is the path without common/ and without the extension.
func ToDatabase(folder string) string {
	database := strings.TrimPrefix(folder, "common/")
	return strings.TrimSuffix(database, path.Ext(database))
}

// FindDatabase returns the row for a folder or file, the game's own before every game's. It returns false if there
// is none.
func (game *Game) FindDatabase(folder string) (Database, bool) {
	return lo.Find(Databases, func(row Database) bool { return row.Folder == folder && game.Matches(row.Games) })
}

// ListDatabaseFolders returns the folders and files that the game's database rows name.
func (game *Game) ListDatabaseFolders() []string {
	return lo.FilterMap(Databases, func(row Database, _ int) (string, bool) { return row.Folder, game.Matches(row.Games) })
}

// FindNestings returns the nestings whose parent database holds a file, the game's own before every game's.
func (game *Game) FindNestings(rel string) []Nesting {
	return lo.Filter(game.ListNestings(), func(row Nesting, _ int) bool {
		return rel == row.Parent || strings.HasPrefix(rel, row.Parent+"/")
	})
}

// ListNestings returns every nesting of the game, its own before every game's.
func (game *Game) ListNestings() []Nesting {
	return lo.Filter(Nestings, func(row Nesting, _ int) bool { return game.Matches(row.Games) })
}

// IsSet reports whether a nested definition of the given database only switches a name on or gives a definition a
// second name, as a parameter or a game concept's alias does.
func (game *Game) IsSet(database string) bool {
	return slices.ContainsFunc(game.ListNestings(), func(row Nesting) bool { return row.Sets && row.Label() == database })
}

// ListSetupNestings returns the start setup's nestings once for each given start setup, its folder as the parent.
func (game *Game) ListSetupNestings(setups ...string) []Nesting {
	var nestings []Nesting
	for _, setup := range setups {
		for _, nesting := range game.FindNestings(SetupFolder) {
			nesting.Parent = setup
			nestings = append(nestings, nesting)
		}
	}
	return nestings
}

// gameReferences holds the rows of References that hold for each game, by the game's id.
var gameReferences = func() map[string][]Reference {
	references := map[string][]Reference{}
	for id, row := range games {
		references[id] = lo.Filter(References, func(reference Reference, _ int) bool { return row.Matches(reference.Games) })
	}
	return references
}()

// ListReferences returns the key paths in the game's definitions whose values name definitions, GUI files' first,
// then every game's, then the game's own. It returns none for a game the package does not list.
func (game *Game) ListReferences() []Reference {
	return gameReferences[game.ID]
}

// ListFiringFields returns the key paths in the game's definitions where a name fires the event, on_action or
// decision it names, as it would in effect script.
func (game *Game) ListFiringFields() []Reference {
	return lo.Filter(FiringFields, func(row Reference, _ int) bool { return game.Matches(row.Games) })
}

// KeepsEvery reports whether the game keeps every copy of a name, merged into one.
func (precedence Precedence) KeepsEvery() bool { return precedence.Keeps == "merged" }

// Describe returns, in plain sentences, which copy the game keeps when several definitions share a name and what a mod
// can do about it.
func (precedence Precedence) Describe() string {
	var sentences []string
	switch {
	case precedence.KeepsEvery():
		sentences = append(sentences, "The game keeps every copy and combines them into one.")
		if len(precedence.Contested) > 0 {
			sentences = append(sentences, "Two copies cannot both write a "+strings.Join(precedence.Contested, " or ")+
				" block; the game does not say which one it keeps.")
		}
	case precedence.Keeps == "highest":
		sentences = append(sentences, "The game keeps the copy with the highest "+precedence.PriorityKey+precedence.describeOverrides()+
			". If two tie, it does not say which one it keeps.")
	case precedence.ByPlayset:
		sentences = append(sentences, "The game keeps the copy from the mod that loads last, so moving a mod in the load order "+
			"changes which one it is.")
	case precedence.Keeps == "first":
		sentences = append(sentences, "The game reads the files in a folder in order of file name and keeps the first copy it reads.")
		if precedence.Modes == "" {
			sentences = append(sentences, "A mod's copy wins when its file name sorts before the others.")
		}
	default:
		sentences = append(sentences, "The game reads the files in a folder in order of file name and keeps the last copy it reads.")
		if precedence.Modes == "" {
			sentences = append(sentences, "A mod's copy wins when its file name sorts after the others.")
		}
	}
	if precedence.Modes != "" {
		sentences = append(sentences, precedence.describeModes())
	}
	return strings.Join(sentences, " ")
}

// describeOverrides returns the clause saying a higher priority replaces only some keys. It returns "" if it replaces
// the whole definition.
func (precedence Precedence) describeOverrides() string {
	if len(precedence.Overrides) == 0 {
		return ""
	}
	return " for " + strings.Join(precedence.Overrides, ", ") + ", and combines the rest"
}

// describeModes returns the sentence saying how a mod changes a definition with an entry mode, and what the mode that
// adds does where the sources say.
func (precedence Precedence) describeModes() string {
	where := "in front of the name"
	if precedence.Modes == "wrapped" {
		where += " inside the block that holds it"
	}
	sentence := "A mod changes an entry by writing REPLACE: " + where + " to take its place"
	switch precedence.Injects {
	case "replaces":
		return sentence + "; INJECT: does the same here."
	case "unsettled":
		return sentence + "; the game does not say what INJECT: does here."
	}
	return sentence + ", or INJECT: to add to it."
}

// LookupPrecedence returns how a database's definitions combine, from the first row that holds for it by its name or
// folder. Where no row holds, the last copy by file name wins.
func (game *Game) LookupPrecedence(database, folder string) Precedence {
	row, ok := lo.Find(Precedences, func(row Precedence) bool { return game.Matches(row.Games) && row.holds(database, folder) })
	if !ok {
		return Precedence{Keeps: "last"}
	}
	return row
}

// holds reports whether a precedence row holds for a database, by its name or its folder. A database with no folder,
// such as a nested one, is held by its name only.
func (precedence Precedence) holds(database, folder string) bool {
	named := slices.ContainsFunc(precedence.Databases, func(pattern string) bool {
		matched, _ := path.Match(pattern, database)
		return matched
	})
	return named || (folder != "" && slices.ContainsFunc(precedence.Folders, func(listed string) bool {
		return folder == listed || strings.HasPrefix(folder, listed+"/")
	}))
}

// SplitMode splits a mode prefix off a written key, so REPLACE:x gives the name x with the mode REPLACE. A game whose
// definitions take no modes splits none.
func (game *Game) SplitMode(key string) (mode, name string) {
	prefix, rest, cut := strings.Cut(key, ":")
	if _, ok := LookupEntryMode(prefix); cut && game.TakesModes && ok {
		return prefix, rest
	}
	return "", key
}

// LookupEntryMode returns the entry mode a prefix names. It returns false for "" and for a prefix no game uses.
func LookupEntryMode(prefix string) (EntryMode, bool) {
	return lo.Find(EntryModes, func(mode EntryMode) bool { return mode.Prefix == prefix })
}

// DescribeDefinition returns what a definition of a database is called, such as "scripted effect", in the plural if
// asked. It returns "" for a database with no name.
func DescribeDefinition(database string, plural bool) string {
	if nouns, ok := definitionNouns[database]; ok && plural {
		return nouns.Many
	} else if ok {
		return nouns.One
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
	// a name already in the singular takes the plural of its ending
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
