// databases.go holds the database tables of each game, one per kind of fact: where definitions are written and how
// they are named, how they nest and combine, what script calls by name, and what a file's extension says.

package game

import (
	"path"
	"slices"
	"strings"
)

// A Database is the row for one database: where its definitions are written and how each is named. Its name is its
// path without common/ and without the file extension (see ToDatabase). A folder with no row is a database whose
// top-level keys name its definitions.
type Database struct {
	// Houses is what it holds beyond definitions: events, on_actions, decisions, lists, concepts, history or modifiers.
	Houses        string
	NamedBy       string   // how a definition is named: "" means its own key; or field, call, file, column or nothing
	NameKey       string   // NamedBy field: the key whose value names the definition; call: the key whose value is a call
	WrapperKeys   []string // keys at the top that only wrap definitions
	DeclaringKeys []string // keys that declare a definition inline: scripted_effect x = { }
	AliasKeys     []string // the keys whose value names a definition a second time
	// RunOrder is a definition's blocks in the order they run; the keys it does not list run after them, in the order
	// written.
	RunOrder  []string
	Shape     string   // what a definition's block holds, a Shapes key; "" means its own fields
	Target    string   // the database whose definitions its own set up or are named for; "" means none
	FoldsCase bool     // its references match without regard to case
	Prefixes  []string // the link prefixes script writes before a definition's name, as in dynasty:<key>
	// Separator is the character that ends a definition's part of a prefixed reference, as | in define:NAI|X; "" means
	// the whole label.
	Separator string
}

// A Nesting is a database whose definitions are keys inside another database's definitions. Its name is the
// parent's name, a dot and Database.
type Nesting struct {
	Parent   string // the folder or file whose definitions hold its keys
	KeyPath  string // the key path from the holding definition to its keys; "" means its block; * any key; ** any depth
	Database string // its own name after the parent's; "" means the parent's own definitions (a title inside a title)
	Keys     string // with Database "": the pattern a key fits to define one (path.Match); a key that does not is a field
	Target   string // setup: the database whose definitions its own set up; "" means none
}

// A Callable is a database whose definitions script or a GUI file calls by name, with the words that call them.
type Callable struct {
	Callers    []string // the words a call by a definition's name resolves by, a block's Caller
	Shape      string   // what a definition's block holds, a Shapes key; "" means the caller's shape
	Parameters bool     // a call passes parameters
	Local      bool     // its definitions answer only in the file that declares them
	FoldsCase  bool     // its references match without regard to case
	Proves     []string // the shapes a block that calls one of its definitions can have
}

// A FileType is what a file's extension says about a file, beyond what the databases of its folder say.
type FileType struct {
	NameKey     string // the key whose value names a top block's definition; "" means the block's own key
	ColorSuffix string // the ending of keys whose untagged number block is a color; "" means none
	Shape       string // what a top block holds, a Shapes key; "" means the database's
}

// A Precedence is how definitions of one name combine: which one the game keeps, and what an entry mode or a
// priority does to the others.
type Precedence struct {
	Keeps       string   // which definition the game keeps: first, last, highest or merged
	ByPlayset   bool     // the order is the playset's, not the load order's
	Modes       string   // where an entry mode is written: entry or wrapped; "" means none
	ReplaceOnly bool     // INJECT acts as REPLACE
	PriorityKey string   // the key carrying a definition's override priority; "" means none
	Overrides   []string // what a higher priority replaces: nil means the whole definition, else these keys alone
}

// A Reference says which definitions the words at a key path inside a database's definitions name.
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
	Key      string // the key path read in the block the hop starts from
	Database string // the database whose definition the key's value names
	Match    string // the key path whose words match the value in each definition; "" means the definition's name
}

// A PictureChain is the hops from an event to a picture file.
type PictureChain struct {
	Steps    []PictureStep
	FileKey  string // the key path, in the last definition, that holds the picture file
	Fallback string // the key naming another definition of the last database where that file is absent; "" means none
}

// A Picture is how an event's picture is found: a key that writes the file outright, or else the first chain that
// ends in one.
type Picture struct {
	FileKey string // the key that writes a picture file outright; "" means none
	Chains  []PictureChain
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

// EventNamespaceKey is the key an events file declares a namespace with; a file may declare several. Its
// definitions are its top-level keys, written as <namespace>.N with a namespace the file declares.
const EventNamespaceKey = "namespace"

// TitleKey is the key a definition names its title's localization key with, as every game's events do
// (title = <key>), and some of CK3's decisions.
const TitleKey = "title"

// EventPortraits is how an event's portraits are found: the endings of the keys that write one, and the key inside
// each that names the character.
var EventPortraits = struct {
	Suffixes  []string // the last word of a portrait's key (left_portrait, left_icon)
	Character string   // the key in a portrait's block that names its character
}{[]string{"_portrait", "_icon"}, "character"}

// LocReplaceFolders are the two forms of the folder whose localization overrides every other, with the language
// at %s. The game's executables match the path part /replace/.
var LocReplaceFolders = []string{"localization/%s/replace", "localization/replace/%s"}

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
	{"arms", "arms"}, {"goods", "goods"}, // the same in the singular
	{"ouses", "ouse"}, {"eases", "ease"}, {"ies", "y"}, {"ses", "s"}, {"xes", "x"}, {"ches", "ch"}, {"shes", "sh"},
	{"s", ""},
}

// EntryModes are the modes a definition's name may start with, in the order the game runs them. CK3 has none.
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

// onActionPrecedence is how on_actions combine in every game: the definitions of one name merge (_on_actions.info; the
// wiki for EU5).
var onActionPrecedence = Precedence{Keeps: "merged"}

// replaceOnlyPrecedence is how a database that takes entry modes combines when INJECT acts as REPLACE.
var replaceOnlyPrecedence = Precedence{Keeps: "first", Modes: "entry", ReplaceOnly: true}

// modePrecedence is how the databases that take entry modes combine (the Vic3 wiki's Mod compatibility list).
var modePrecedence = Precedence{Keeps: "first", Modes: "entry"}

// defaultPrecedence is how definitions combine where no row says: the last loaded wins (the wikis).
var defaultPrecedence = Precedence{Keeps: "last"}

// Databases holds the shared rows for the folders whose definitions are not simply their top-level keys, or that
// hold more than plain definitions. A folder with no row needs none. A game's own rows are tried first, and a row for
// a folder a game lacks matches nothing.
var Databases = map[string]Database{
	"common/scripted_effects":  {DeclaringKeys: []string{"scripted_effect"}},
	"common/scripted_triggers": {DeclaringKeys: []string{"scripted_trigger"}},
	"common/scripted_lists":    {Houses: "lists"},
	// its subfolders (ai, graphic, jomini) write the same define groups; define:Namespace|Name reads one (CK3
	// event_targets.log:109, the Vic3 wiki's Event target page, array_define:Namespace|Name|Index)
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
}

// Nestings holds the shared nestings. A game's own come before them.
var Nestings = []Nesting{
	{Parent: "common/defines", Database: "define"},
}

// Callables says which database answers a call: the word a block calls with leads to the database whose
// definition answers.
var Callables = map[string]Callable{
	"scripted_effects": {Callers: []string{callEffect}, Shape: "effects", Parameters: true, Proves: []string{ShapeEffects}},
	// A value block calls them too.
	"scripted_triggers": {
		Callers: []string{callTrigger, callValue}, Shape: "triggers", Parameters: true, Proves: []string{ShapeTriggers, ShapeValue},
	},
	"scripted_modifiers": {Callers: []string{callValue}, Parameters: true, Proves: []string{ShapeValue}},
	"script_values":      {Callers: []string{callTrigger, callValue}, Shape: "value", Proves: []string{ShapeTriggers, ShapeValue}},
	"gui.type":           {Callers: []string{guiTagCalls}, FoldsCase: true}, // a widget key naming a type
	"gui.local_type":     {Callers: []string{guiTagCalls}, Local: true},
	"gui.template":       {Callers: []string{guiTemplateCalls}},
	"gui.local_template": {Callers: []string{guiTemplateCalls}, Local: true},
	"gui.block":          {Callers: []string{guiBlockCalls}, FoldsCase: true},
}

// FileTypes says what each file extension means. A key ending in color holds a color even without a tag (a
// culture's color, fontcolor), except a portrait gene's.
var FileTypes = map[string]FileType{
	".txt":   {ColorSuffix: "color"},
	".asset": {NameKey: "name"},
	".font":  {NameKey: "name"},
	".gui":   {Shape: ShapeWidget, ColorSuffix: "color"},
}

// Precedences holds the shared precedences no game overrides, by database. LookupPrecedence tries the game's own
// first.
var Precedences = map[string]Precedence{
	"defines":        {Keeps: "merged"},
	"defines.define": {Keeps: "merged"},
	"gui.block":      {Keeps: "merged"}, // one label is written in many types
}

// References holds the shared references, by the database whose definitions hold the key path: an on_action's key
// paths name other on_actions. A game's own come after them, and a row for a database a game lacks matches nothing.
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
			{Key: "ecs_illustration_image/list", Database: "gfx/images", Match: "tag"},
		},
		FileKey: "illustration_image/texture_file",
	}},
}

var ck3Databases = map[string]Database{ // its .info files unless noted
	"common/on_action": {Houses: "on_actions", RunOrder: onActionOrder},
	"events":           {Houses: "events", RunOrder: ck3EventOrder},
	// _decisions.info:194,200,208
	"common/decisions": {
		Houses: "decisions", RunOrder: []string{"is_shown", "is_valid", "cost", "cooldown", "effect"},
	},
	// _traits.info:3, and :181,183: a group is also a name, which has_trait holds for any trait of the group
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
	"gfx/map/environment":            {NamedBy: "file"}, // gfx/map/environment/environment.info:1
	// afr.txt:10-11
	"gfx/portraits/accessory_variations": {NamedBy: "field", NameKey: "name"},
	"common/dynasties":                   {Prefixes: []string{"dynasty"}},   // _dynasties.info:3
	"common/dynasty_houses":              {Prefixes: []string{"house"}},     // _dynasty_houses.info:3
	"common/situation/situations":        {Prefixes: []string{"situation"}}, // _situations.info:206
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
	"gui.type":          {Keeps: "first"},  // the executable
	"gui.template":      {Keeps: "first"},  // the executable
	"common.parameter":  {Keeps: "merged"}, // several holders set one
	"history/provinces": {Keeps: "merged"}, // a province's history can be written in two files
	// c_nf_wang_738b_7 is written twice: history/titles/01_admin_titles_tgp.txt:6339,6435
	"history/titles": {Keeps: "merged"},
}

var vic3Databases = map[string]Database{
	"common/on_actions":       {Houses: "on_actions", RunOrder: onActionOrder}, // _on_actions.info
	"events":                  {Houses: "events", RunOrder: eventOrder},        // the wiki
	"common/decisions":        {Houses: "decisions"},
	"gfx/map/map_object_data": {NamedBy: "nothing"},
	"gfx/map/post_effects":    {NamedBy: "field", NameKey: "name", WrapperKeys: []string{"posteffect_volumes"}},
	"common/history":          {Houses: "history", NamedBy: "nothing", Shape: "effects"}, // COUNTRIES = { … }: effects
	// modifier_types.md:1-3
	"common/modifier_type_definitions": {Houses: "modifiers"},
	// map_data/province_terrains.txt:21698 defines x7F25CD, common/history/states/00_states.txt:137 writes x7f25cd
	"map_data/province_terrains.txt": {FoldsCase: true},
	"common/strategic_regions":       {Prefixes: []string{"sr"}}, // common/geographic_regions/geographic_regions.md:8
	"common/static_modifiers":        {Shape: ShapeModifiers},    // modifier_types.md:2
	// common/geographic_regions/geographic_regions.md:4 names its lists by the short_key: every_country_in_<short_key>
	"common/geographic_regions": {AliasKeys: []string{"short_key"}},
}

var vic3Precedences = map[string]Precedence{ // the wiki unless noted
	"on_actions":              onActionPrecedence,
	"events":                  {Keeps: "first"},
	"scripted_rules":          replaceOnlyPrecedence,
	"technology/technologies": modePrecedence,
	"technology/eras":         modePrecedence, // inferred, as technologies
	"strategic_regions":       modePrecedence, // inferred, as technologies
	"gui.type":                {Keeps: "first"},
	"gui.template":            {Keeps: "first"},
	"script_values":           replaceOnlyPrecedence,
	"scripted_effects":        replaceOnlyPrecedence,
	"scripted_triggers":       replaceOnlyPrecedence,
	"genes":                   {Keeps: "first", Modes: "wrapped"},
}

// _traits.info:4,23-35: a trait's icon is an image file, written alone or by the descriptions inside the icon, and a
// file named with no folder is in the traits' icon folder.
var ck3References = []Reference{
	{Database: "traits", KeyPath: "icon", Target: File, Folder: "gfx/interface/icons/traits"},
	{Database: "traits", KeyPath: "icon/**/desc", Target: File, Folder: "gfx/interface/icons/traits"},
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
	"on_action":                 onActionPrecedence,               // the wiki
	"events":                    {Keeps: "first"},                 // the wiki
	"gui.type":                  {Keeps: "last", ByPlayset: true}, // inferred
	"gui.template":              {Keeps: "last", ByPlayset: true}, // inferred
	"coat_of_arms/coat_of_arms": {Keeps: "last"},                  // the wiki
	"scripted_effects":          replaceOnlyPrecedence,            // inferred
	"scripted_triggers":         replaceOnlyPrecedence,            // inferred
	// The DLC writes INJECT_OR_CREATE inside accessory_genes.
	"genes": {Keeps: "first", Modes: "wrapped"},
}

var eu5References = []Reference{
	{Database: "events", KeyPath: "title", Target: Localization},
	{Database: "events", KeyPath: "desc", Target: Localization},
	{Database: "events", KeyPath: "historical_info", Target: Localization},
	{Database: "events", KeyPath: "option/name", Target: Localization},
}

// eu5ModeFolders takes modes under every folder of common and setup but on_action and defines.
var eu5ModeFolders = ModeFolders{Roots: []string{"common", "setup"}, Except: []string{"on_action", "defines"}}

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

// FindNestings returns the nestings whose parent database holds a file, the game's own before the shared ones.
func (game *Game) FindNestings(rel string) (rows []Nesting) {
	for _, row := range slices.Concat(game.Nestings, Nestings) {
		if rel == row.Parent || strings.HasPrefix(rel, row.Parent+"/") {
			rows = append(rows, row)
		}
	}
	return rows
}

// MatchKeyPath reports whether the keys that lead to a statement fit a key path: segments separated by slashes, each
// a key, a glob such as *_parameters, <number>, * for any one key, or ** for any number of them.
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
