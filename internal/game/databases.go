// databases.go holds the database tables of each game, one per kind of fact: where a database's definitions are
// written and how each is named, which databases nest inside another's definitions, which ones script calls by
// name, what a file's extension says, how definitions combine across files and mods, what a definition's key paths
// name, the entry modes, how an event's picture is found, and the databases no definition carries.

package game

import (
	"path"
	"slices"
	"strings"
)

// A Database says where one database's definitions are written and how each is named. Its name is its path
// without common/ and without the file extension (see ToDatabase). A folder with no row is a database whose
// top-level keys name its definitions.
type Database struct {
	Houses        string   // beyond definitions: events, on_actions, decisions, lists, functions, concepts, history
	NamedBy       string   // how a definition is named: "" its own key, field, file, column, nothing
	NameKey       string   // NamedBy field: the key whose value names the definition
	WrapperKeys   []string // keys at the top that only wrap definitions
	DeclaringKeys []string // keys that declare a definition inline: scripted_effect x = { }
	AliasKeys     []string // keys whose values in a definition also name this database's definitions
	RunOrder      []string // a definition's blocks in the order they run; a key not listed runs where it is written, after
	Shape         string   // what a definition's block holds, a Shapes key; "" its own fields
	Dated         bool     // a number key in a definition's own block is a date written short (999, 1077.1)
	Target        string   // history: the database whose definitions its own set up; "" its own files'
	FoldsCase     bool     // its references match without regard to case
}

// A Nesting is a database whose definitions are keys inside another database's definitions. Its name is the
// parent's name, a dot and Database.
type Nesting struct {
	Parent   string // the folder or file whose definitions hold its keys
	KeyPath  string // the key path from the holding definition to its keys; "" its own block; * any key, ** any depth
	Database string // its own name after the parent's; "" the parent's own definitions (a title inside a title)
	Keys     string // with Database "": the pattern a key fits to define one (path.Match); a key that does not is a field
	Target   string // setup: the database whose definitions its own set up; "" none
}

// A Callable is a database whose definitions script or a GUI file calls by name, with the words that call them.
type Callable struct {
	Callers    []string // the words a call by a definition's name resolves by, a block's Caller. Read by the walk.
	Shape      string   // what a definition's block holds, a Shapes key; "" the caller's shape
	Parameters bool     // a call passes parameters
	Local      bool     // its definitions answer only in the file that declares them
	FoldsCase  bool     // its references match without regard to case
}

// A FileType is what a file's extension says about a file, beyond what the databases of its folder say.
type FileType struct {
	NameKey     string // the key whose value names a top block's definition; "" the block's own key
	ColorSuffix string // the ending of keys whose untagged number block is a color; "" none
	Shape       string // what a top block holds, a Shapes key; "" the database's
}

// A Precedence says how FindWinner settles a contest between definitions of one name: which one the game keeps,
// and what an entry mode or a priority does to the others.
type Precedence struct {
	Keeps       string   // which definition the game keeps: first, last, highest, merged. Read by index.FindWinner alone.
	ByPlayset   bool     // the order is the playset's, not the load order's
	Modes       string   // where an entry mode is written: entry, wrapped; "" none. Read by index.FindWinner alone.
	ReplaceOnly bool     // INJECT acts as REPLACE
	Adds        []string // keys whose lists append across definitions
	Once        []string // keys a second definition may not write again
	PriorityKey string   // the key carrying a definition's override priority; "" none
	Overrides   []string // what a higher priority replaces: nil the whole definition, else these keys alone
}

// A Reference says which definitions the words at a key path inside a database's definitions name.
type Reference struct {
	Database string // the database whose definitions hold the key path
	KeyPath  string // the key path from the definition; <number> matches a number key
	Target   string // the database the words name; "" the row's own
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
	Key      string // the key path read in the block the hop starts from
	Database string // the database whose definition the key's value names
	Match    string // the key path whose words in each definition the value is matched with; "" the definition's name
}

// A PictureChain is the hops from an event to a picture file.
type PictureChain struct {
	Steps    []PictureStep
	FileKey  string // the key path, in the last definition, that holds the picture file
	Fallback string // the key naming another definition of the last database where that file is absent; "" none
}

// Picture says how an event's picture is found: a key that writes the file outright, or else the first chain that
// ends in one.
type Picture struct {
	FileKey string // the key that writes a picture file outright; "" none
	Chains  []PictureChain
}

// Localization, File and DataFunction are the databases that a loc key, a file path and a data function
// (Type.Function) name. No definition carries them.
const (
	Localization = "localization"
	File         = "file"
	DataFunction = "datafunction"
)

// Constant is the database of the constants a file writes for itself as @name = value, local to the file.
const Constant = "constant"

// GlobalType is the data type a data call's first step is looked up on.
const GlobalType = "Global"

// EventNamespaceKey is the key an events file declares a namespace with; a file may declare several. Its
// definitions are its top-level keys, written as <namespace>.N with a namespace the file declares.
const EventNamespaceKey = "namespace"

// EventPortraits says how an event's portraits are found: the endings of the keys that write one, and the key
// inside each that names the character.
var EventPortraits = struct {
	Suffixes  []string // the last word of a portrait's key (left_portrait, left_icon)
	Character string   // the key in a portrait's block that names its character
}{[]string{"_portrait", "_icon"}, "character"}

// OptionFlags are the yes-or-no keys an option can be marked with: the default and the historical choice.
var OptionFlags = []string{"default_option", "historical_option"}

// LocReplaceFolders are the two forms of the folder whose localization overrides every other, with the language
// at %s. The executables match the path part /replace/, and vanilla ships none.
var LocReplaceFolders = []string{"localization/%s/replace", "localization/replace/%s"}

// LocFillIns are the values a localization text may name that no file defines, written as $NAME$. The docs name
// the first four; vanilla writes the last two.
var LocFillIns = []string{"MODEL_SIZE", "ITEM_INDEX", "ITEM_NUM", "IS_LAST_INDEX", "IS_LAST_ITEM", "ITEM"}

// MisspelledFolders maps folder names the game never reads to the name it reads instead.
var MisspelledFolders = map[string]string{"localisation": "localization"}

// ClassDatabases maps the dumps' class names that no naming rule resolves, such as CCourtPositionTypesDatabase, to
// the path of their database as the install writes it.
var ClassDatabases = map[string]string{
	"CCultureInnovationTypeDatabase":     "culture/innovations",
	"CCultureTraditionDatabase":          "culture/traditions",
	"CCourtPositionTypesDatabase":        "court_positions/types",
	"CTravelPointOfInterestTypeDatabase": "travel/point_of_interest_types",
}

// plurals turns a folder's last word into the singular. The first ending that fits wins.
var plurals = []struct{ Plural, One string }{
	{"ouses", "ouse"}, {"eases", "ease"}, {"ies", "y"}, {"ses", "s"}, {"xes", "x"}, {"ches", "ch"}, {"shes", "sh"},
	{"s", ""},
}

// EntryModes are the modes SplitMode strips off a definition's name and FindWinner applies, in the order the game
// runs them. CK3's row has none.
var EntryModes = []EntryMode{
	{Prefix: "INJECT_OR_CREATE", Creates: true}, {Prefix: "REPLACE_OR_CREATE", Replaces: true, Creates: true},
	{Prefix: "TRY_INJECT", Optional: true}, {Prefix: "TRY_REPLACE", Replaces: true, Optional: true},
	{Prefix: "INJECT"}, {Prefix: "REPLACE", Replaces: true},
}

// FireHouses are the Houses of the databases that fire and are fired: events, on_actions and decisions.
var FireHouses = []string{"events", "on_actions", "decisions"}

// TableSyntax is how a table file writes its rows, as CK3's map_data/definition.csv does: each row's columns split
// by Separator, the first naming the row, and a line that opens with Comment a comment.
var TableSyntax = struct{ Separator, Comment string }{";", "#"}

// The words a scripted database is called by, from the blocks that hold its kind of definition.
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

// onActionPrecedence is how on_actions combine in every game: lists append, and a second trigger or effect is an
// error (_on_actions.info; the wiki for EU5).
var onActionPrecedence = Precedence{
	Keeps: "merged", Adds: []string{"events", "on_actions", "random_events"}, Once: []string{"trigger", "effect"},
}

// replaceOnlyPrecedence is how a database that takes entry modes combines when INJECT acts as REPLACE.
var replaceOnlyPrecedence = Precedence{Keeps: "first", Modes: "entry", ReplaceOnly: true}

// modePrecedence is how the databases that take entry modes combine (the Vic3 wiki's Mod compatibility list).
var modePrecedence = Precedence{Keeps: "first", Modes: "entry"}

// defaultPrecedence is how definitions combine where no row says: the last loaded wins (the wikis).
var defaultPrecedence = Precedence{Keeps: "last"}

// Databases holds the shared rows for the folders whose definitions are not simply their top-level keys, or that
// hold more than plain definitions. A folder with no row needs none. Readers go through FindDatabase, which tries
// the game's own rows first; a row for a folder a game lacks matches nothing.
var Databases = map[string]Database{
	"common/scripted_effects":            {DeclaringKeys: []string{"scripted_effect"}},
	"common/scripted_triggers":           {DeclaringKeys: []string{"scripted_trigger"}},
	"common/scripted_lists":              {Houses: "lists"},
	"common/defines":                     {}, // its subfolders (ai, graphic, jomini) write the same define groups
	"common/genes":                       {WrapperKeys: []string{"color_genes", "special_genes", "morph_genes", "accessory_genes", "decal_atlases", "age_presets"}},
	"common/named_colors":                {WrapperKeys: []string{"colors"}},
	"data_binding":                       {NamedBy: "field", NameKey: "definition", Houses: "functions"},
	"common/game_concepts":               {Houses: "concepts"},
	"common/achievement_groups.txt":      {NamedBy: "field", NameKey: "name"},
	"common/coat_of_arms/options":        {NamedBy: "nothing"},
	"common/coat_of_arms/coat_of_arms":   {WrapperKeys: []string{"template"}},
	"common/coat_of_arms/template_lists": {WrapperKeys: []string{"coat_of_arms_template_lists", "pattern_texture_lists", "colored_emblem_texture_lists", "textured_emblem_texture_lists", "color_lists"}},
	"gui/scripted_widgets":               {NamedBy: "nothing"},
	"music/music_player_categories":      {NamedBy: "field", NameKey: "id"},
	"localization":                       {FoldsCase: true}, // a $KEY$ may differ in case (vanilla)
}

// Nestings holds the shared nestings. FindNestings puts the game's own before them.
var Nestings = []Nesting{
	{Parent: "common/defines", Database: "define"},
}

// Callables says which database answers a call: the word a block calls with leads to the database whose
// definition answers. Readers index it by database.
var Callables = map[string]Callable{
	"scripted_effects":   {Callers: []string{callEffect}, Shape: "effects", Parameters: true},
	"scripted_triggers":  {Callers: []string{callTrigger}, Shape: "triggers", Parameters: true},
	"scripted_modifiers": {Callers: []string{callValue}, Parameters: true},
	"script_values":      {Callers: []string{callTrigger, callValue}, Shape: "value"},
	"gui.type":           {Callers: []string{"widget"}, FoldsCase: true}, // a widget key naming a type (vanilla)
	"gui.local_type":     {Callers: []string{"widget"}, Local: true},
	"gui.template":       {Callers: []string{"using"}},
	"gui.local_template": {Callers: []string{"using"}, Local: true},
	"gui.block":          {Callers: []string{"blockoverride"}, FoldsCase: true}, // vanilla
}

// FileTypes says what each file extension means. A key ending in color holds a color even without a tag (a
// culture's color, fontcolor), except a portrait gene's.
var FileTypes = map[string]FileType{
	".txt":   {ColorSuffix: "color"},
	".asset": {NameKey: "name"},
	".font":  {NameKey: "name"},
	".gui":   {Shape: "widget", ColorSuffix: "color"},
}

// Precedences holds the shared precedences no game overrides, by database. LookupPrecedence tries the game's own
// first.
var Precedences = map[string]Precedence{
	"defines":        {Keeps: "merged"},
	"defines.define": {Keeps: "merged"},
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

var vic3Picture = Picture{ // observed in vanilla; the wiki's Event images page says Bink videos
	Chains: []PictureChain{{
		Steps:   []PictureStep{{Key: "event_image/video", Database: "gfx/media_aliases"}},
		FileKey: "video", Fallback: "fallback",
	}},
}

var eu5Picture = Picture{ // observed in vanilla: no document names illustration_tags
	FileKey: "image",
	Chains: []PictureChain{{
		Steps: []PictureStep{
			{Key: "illustration_tags", Database: "gfx/illustrations/database", Match: "tags"},
			{Key: "scene", Database: "gfx/scenes"},
			{Key: "ecs_illustration_image/list", Database: "gfx/images", Match: "tag"},
		},
		FileKey: "illustration_image/texture_file",
	}},
}

var ck3Databases = map[string]Database{ // its .info files unless noted
	"common/on_action":         {Houses: "on_actions", RunOrder: onActionOrder},
	"events":                   {Houses: "events", RunOrder: ck3EventOrder},
	"common/decisions":         {Houses: "decisions", RunOrder: []string{"is_shown", "is_valid", "cost", "cooldown", "effect"}},
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
	"history/characters":       {Shape: "setup", Dated: true},
	"common/reader_export":     {}, // no folder: _reader_export.info documents what the reader loads before any other file
	"map_data/definition.csv":  {NamedBy: "column"},
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
	"events":    {Keeps: "highest", PriorityKey: "id_override_priority"}, // _events.info
	"history/characters": {
		Keeps: "highest", PriorityKey: "history_override_priority", // _characters.info
		Overrides: []string{
			"name", "dna", "female", "martial", "prowess", "diplomacy", "intrigue", "stewardship", "learning", "trait",
			"father", "mother", "disallow_random_traits", "obscured", "birth",
		},
	},
	"gui.type":     {Keeps: "first"}, // the executable
	"gui.template": {Keeps: "first"}, // the executable
}

var ck3References = []Reference{ // an on_action's key paths name other on_actions
	{Database: "on_action", KeyPath: "on_actions"}, {Database: "on_action", KeyPath: "random_on_action/<number>"},
	{Database: "on_action", KeyPath: "first_valid_on_action"}, {Database: "on_action", KeyPath: "fallback"},
}

var vic3Databases = map[string]Database{
	"common/on_actions":       {Houses: "on_actions", RunOrder: onActionOrder}, // _on_actions.info
	"events":                  {Houses: "events", RunOrder: eventOrder},        // the wiki
	"common/decisions":        {Houses: "decisions"},                           // observed in vanilla
	"gfx/map/map_object_data": {NamedBy: "nothing"},
	"gfx/map/post_effects":    {NamedBy: "field", NameKey: "name", WrapperKeys: []string{"posteffect_volumes"}},
	"common/history":          {Houses: "history", NamedBy: "nothing", Shape: "effects"}, // COUNTRIES = { … }: effects
}

var vic3Precedences = map[string]Precedence{ // the wiki unless noted
	"on_actions":              onActionPrecedence,
	"events":                  {Keeps: "first"},
	"scripted_rules":          replaceOnlyPrecedence,
	"technology/technologies": modePrecedence, // inferred: the wiki lists them in the folders that take modes yet says to override them by a later file
	"technology/eras":         modePrecedence, // inferred, as technologies
	"strategic_regions":       modePrecedence, // inferred, as technologies
	"gui.type":                {Keeps: "first"},
	"gui.template":            {Keeps: "first"},
	"script_values":           replaceOnlyPrecedence,
	"scripted_effects":        replaceOnlyPrecedence,
	"scripted_triggers":       replaceOnlyPrecedence,
	"genes":                   {Keeps: "first", Modes: "wrapped"},
}

var vic3References = []Reference{ // an on_action's key paths name other on_actions
	{Database: "on_actions", KeyPath: "on_actions"}, {Database: "on_actions", KeyPath: "random_on_actions/<number>"},
	{Database: "on_actions", KeyPath: "first_valid_on_action"}, {Database: "on_actions", KeyPath: "fallback"},
}

var vic3ModeFolders = ModeFolders{ // the wiki's Mod compatibility list
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

var eu5Databases = map[string]Database{ // observed in vanilla unless noted
	"common/on_action":                     {Houses: "on_actions", RunOrder: onActionOrder}, // _on_actions.info
	"events":                               {Houses: "events", RunOrder: eventOrder},        // _events.info
	"gfx/map/map_objects":                  {NamedBy: "nothing"},
	"gfx/map/city_data":                    {NamedBy: "field", NameKey: "name"},
	"common/game_concepts":                 {Houses: "concepts", FoldsCase: true},   // vanilla refers to a concept in either case
	"setup/start":                          {Houses: "history", NamedBy: "nothing"}, // its top keys are the managers that hold the nestings
	"gfx/unit_graphics/attachments":        {},                                      // its subfolders write the same attachment lists
	"setup/templates":                      {NamedBy: "file"},
	"map_data/location_templates.txt":      {Houses: "history", Target: "map_data/named_locations"},
	"map_data/generated_locators_port.txt": {NamedBy: "nothing"},
}

var eu5Nestings = []Nesting{ // the start setup's managers each set up the database their Target names
	{Parent: "gfx/defines", Database: "define"},
	{Parent: "common/laws", Database: "policy"},
	{Parent: "common/languages", KeyPath: "dialects", Database: "dialect"},
	{Parent: "common/gods", KeyPath: "omens", Database: "omen"},
	{Parent: "common/building_types", KeyPath: "unique_production_methods", Database: "unique_production_method"},
	{Parent: "common/game_concepts", KeyPath: "alias"}, // an alias is a concept
	{Parent: "map_data/definitions.txt", Database: "sub_continent"},
	{Parent: "map_data/definitions.txt", KeyPath: "*", Database: "region"},
	{Parent: "map_data/definitions.txt", KeyPath: "*/*", Database: "area"},
	{Parent: "map_data/definitions.txt", KeyPath: "*/*/*", Database: "province"},
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

var eu5Precedences = map[string]Precedence{
	"on_action":                 onActionPrecedence,                 // the wiki
	"events":                    {Keeps: "first"},                   // the wiki
	"gui.type":                  {Keeps: "last", ByPlayset: true},   // inferred
	"gui.template":              {Keeps: "last", ByPlayset: true},   // inferred
	"coat_of_arms/coat_of_arms": {Keeps: "last"},                    // the wiki
	"scripted_effects":          replaceOnlyPrecedence,              // inferred
	"scripted_triggers":         replaceOnlyPrecedence,              // inferred
	"genes":                     {Keeps: "first", Modes: "wrapped"}, // the DLC writes INJECT_OR_CREATE: inside accessory_genes
}

var eu5References = []Reference{ // an on_action's key paths name other on_actions
	{Database: "on_action", KeyPath: "on_actions"}, {Database: "on_action", KeyPath: "random_on_action/<number>"},
	{Database: "on_action", KeyPath: "first_valid_on_action"}, {Database: "on_action", KeyPath: "fallback"},
}

var eu5ModeFolders = ModeFolders{Roots: []string{"common", "setup"}, Except: []string{"on_action", "defines"}} // no source lists the folders

// ToDatabase returns the database name a folder or file makes: its path without common/ and without the file
// extension.
func ToDatabase(folder string) string {
	database := strings.TrimPrefix(folder, "common/")
	return strings.TrimSuffix(database, path.Ext(database))
}

// FindDatabase returns the row for a folder or file: the game's own, or else the shared one. It returns false if
// there is none.
func (game *Game) FindDatabase(folder string) (Database, bool) {
	if row, ok := game.Databases[folder]; ok {
		return row, true
	}
	row, ok := Databases[folder]
	return row, ok
}

// FindNestings returns the nestings whose parent database holds a file, the game's own before the shared ones, so
// that the walk can read their keys inside the holding definitions.
func (game *Game) FindNestings(rel string) (rows []Nesting) {
	for _, row := range slices.Concat(game.Nestings, Nestings) {
		if rel == row.Parent || strings.HasPrefix(rel, row.Parent+"/") {
			rows = append(rows, row)
		}
	}
	return rows
}

// MatchKeyPath reports whether the keys that lead to a statement fit a key path: segments apart by slashes, each a
// key, a glob such as *_parameters, <number>, * for any one key, or ** for any number of them.
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

// LookupPrecedence returns how a database's definitions combine: the game's own row, else the shared one, else the
// precedence for entry modes when the database's folder takes them, else the default. The folder is the one the
// database's row declares, or "" for a database no folder makes.
func (game *Game) LookupPrecedence(database, folder string) Precedence {
	if row, ok := game.Precedences[database]; ok {
		return row
	}
	if row, ok := Precedences[database]; ok {
		return row
	}
	if game.takesModes(database, folder) {
		return modePrecedence
	}
	return defaultPrecedence
}

// takesModes reports whether a database's folder takes entry modes: it is one the game lists or sits under a root
// it lists, and is not an exception.
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

// DescribeDefinition returns what a definition of a database is called, in the singular or the plural: "scripted
// effect" or "scripted effects" for scripted_effects.
func DescribeDefinition(database string, plural bool) string {
	last := database[strings.LastIndexByte(database, '/')+1:]
	if !plural {
		for _, ending := range plurals {
			if stem, cut := strings.CutSuffix(last, ending.Plural); cut {
				last = stem + ending.One
				break
			}
		}
	}
	return strings.NewReplacer("_", " ", ".", " ").Replace(last)
}
