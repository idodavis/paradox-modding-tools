// databases.go holds the rules of every folder and file type of each game: how entries are named and used, how their
// definitions combine across files and mods, what an event's blocks mean, and the databases no definition carries.

package game

import (
	"cmp"
	"slices"
	"strings"
)

// Precedence is how one database's definitions combine across files and mods.
type Precedence struct {
	Keeps       string   // which definition the game keeps: first, last, highest, merged
	ByPlayset   bool     // the order is the playset's, not the load order's
	Modes       string   // where an entry mode is written: entry, wrapped; "" none
	ReplaceOnly bool     // INJECT acts as REPLACE
	Adds        []string // keys whose lists append across definitions
	Once        []string // keys a second definition may not write again
	PriorityKey string   // the key carrying an entry's override priority; "" none
	Overrides   []string // what a higher priority replaces: nil the whole entry, else these keys alone
	Source      Source
}

// ModeFolders says which databases take an entry mode, by the folder their rule declares.
type ModeFolders struct {
	Roots      []string   // takes modes: its folder's first folder is one of these (used where Paths is empty)
	Paths      []string   // takes modes: a database whose folder is one of these or below one
	Except     []string   // takes none: a database named here, or whose folder is
	Precedence Precedence // how the databases that take modes combine
}

// EntryMode is a word written before a definition's name, and what it does to one already loaded.
type EntryMode struct {
	Prefix   string // INJECT, REPLACE, TRY_INJECT...
	Replaces bool   // takes the earlier definition's place; else it adds to it
	Optional bool   // does nothing where there is no earlier definition (TRY_)
	Creates  bool   // defines it where there is none (_OR_CREATE)
}

// PictureStep is one hop from an event toward its picture.
type PictureStep struct {
	Key      string // the key path read in the block the hop starts from
	Database string // the database whose entry the key's value names
	Match    string // the key path whose words in each entry the value is matched with; "" the entry's own name
}

// PictureChain is the hops from an event to a picture file.
type PictureChain struct {
	Steps    []PictureStep
	FileKey  string // the key path, in the last entry, that holds the picture file
	Fallback string // the key naming another entry of the last database where that file is absent; "" none
}

// EventPicture is how an event's picture is found: a key that writes the file outright, else the first chain that
// ends in one.
type EventPicture struct {
	FileKey string // the key that writes a picture file outright; "" none
	Chains  []PictureChain
	Source  Source
}

// Reference is the entries or files the words at a key name.
type Reference struct {
	KeyPath  string // a file type's: a key at any depth; a database's: the key path from the entry
	Database string // the database whose entries the words name; "" the rule's own (or a call by the key)
	Folder   string // the folder the words name files under; "" none
}

// DatabaseRule is how the entries of one database, or the files of one extension, are written, named and used. A
// folder with no rule is a database whose top statements' keys name its entries; a path takes the longest rule at or
// above it, and an extension with no rule defines nothing.
type DatabaseRule struct {
	Name       string // the database: a folder from a root (decisions), a file, a file type's own (gui.type)
	Path       string // the folder or file its entries are written in; "" for a nested or a file type's database
	Extension  string // a file type's rule: .txt, .asset, .font, .gui; "" a database's
	ParentPath string // nested: the folder or file whose entries hold its keys
	KeyPath    string // nested: the key path from the holding entry to its keys; "" the entry's own block

	NamedBy         string   // how an entry is named: field, file, column, nothing; "" by its own key
	NameKey         string   // NamedBy field: the key whose value names the entry; a file type's: that of a top block
	NamespaceKey    string   // the key a file declares a namespace by: its entries are its top keys <namespace>.N
	WrapperKeys     []string // keys at the top that only wrap entries
	DeclaringKeys   []string // keys that declare an entry inline: scripted_effect x = { }, type x = button { }
	Houses          string   // beyond plain entries: events, on_actions, lists, functions, concepts, history
	Target          string   // history: the database whose entries its own set up; "" its own files'
	Dated           bool     // a number key in an entry's own block is a date written short (999, 1077.1)
	Shape           Shape    // what an entry's block holds; ShapeUnknown its own fields
	CallerWords     []string // scripted: the words a call by an entry's name resolves by
	TakesParameters bool     // scripted: a call passes parameters
	Local           bool     // its entries answer only in the file that declares them
	References      []Reference
	AliasKeys       []string    // keys whose values in an entry also name this database's entries
	LocPrefix       string      // what the loc key of an entry's name puts before it; "" the name is the key
	FoldsCase       bool        // its references match without regard to case
	Precedence      *Precedence // how its definitions combine; nil the game's default
	EventPicture    EventPicture

	ScopeKey     string   // the key whose value is the entry's root scope
	TypeKey      string   // the key whose value, less TypeSuffix, names the entry's root scope
	TypeSuffix   string   // dropped from TypeKey's value
	DefaultScope string   // the root scope where neither key is written
	RunOrder     []string // an entry's blocks in the order they run; a key not listed runs where it is written, after

	NameKeyOf   map[string]string // a file type's: a block key at any depth, and the key whose value names its entry
	WidgetKey   string            // a file type's: every other top block is an entry of <ext>.widget named by this key
	TagCalls    string            // a file type's: the word that calls the tag a block is written with
	ContextKey  string            // a file type's: the key whose value is the data call its block's bindings read in
	ColorSuffix string            // a file type's: the ending of keys whose untagged number block is a color

	Source Source // the standing of every fact on the rule that carries none of its own
}

// LocReplaceFolder is a folder whose localization replaces the game's own.
type LocReplaceFolder struct {
	Path   string // with the language at %s
	Source Source
}

// LocFillIn is a value the game fills in while it shows a localization text, written as $NAME$.
type LocFillIn struct {
	Placeholder string
	Source      Source
}

// Localization, File and DataFunction are the databases a loc key, a file path and a data function (Type.Function)
// name; no definition carries them.
const (
	Localization = "localization"
	File         = "file"
	DataFunction = "datafunction"
)

// GlobalType is the data type a data call's first step is read on.
const GlobalType = "Global"

// conceptPrefix is what CK3 and EU5 put before a game concept's name for its loc key; Vic3's are their own keys.
const conceptPrefix = "game_concept_"

// entryModes are the prefixes Victoria 3 and EU5 share, in the order the game runs them.
var entryModes = []EntryMode{
	{Prefix: "INJECT_OR_CREATE", Creates: true}, {Prefix: "REPLACE_OR_CREATE", Replaces: true, Creates: true},
	{Prefix: "TRY_INJECT", Optional: true}, {Prefix: "TRY_REPLACE", Replaces: true, Optional: true},
	{Prefix: "INJECT"}, {Prefix: "REPLACE", Replaces: true},
}

// EventPortraits is how an event names its portraits.
var EventPortraits = struct {
	Suffixes  []string // the last word of a portrait's key (left_portrait, left_icon)
	Character string   // the key in a portrait's block that names its character
}{[]string{"_portrait", "_icon"}, "character"}

// OptionFlags are the yes-or-no keys an option is marked with: the default and the historical choice.
var OptionFlags = []string{"default_option", "historical_option"}

// The blocks of an on_action and of an event in the order they run (the games' own documents).
var (
	onActionOrder = []string{"trigger", "effect", "events"}
	ck3EventOrder = []string{"trigger", "immediate", "title", "desc", "option", "after"}
	eventOrder    = []string{"trigger", "immediate", "option", "after"}
)

// ck3Picture is CK3's event picture (_events.info, _event_themes.info).
var ck3Picture = EventPicture{
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
	Source: SourceDocs,
}

// vic3Picture is Vic3's event picture (observed in vanilla; the wiki's Event images page says Bink videos).
var vic3Picture = EventPicture{
	Chains: []PictureChain{{
		Steps:   []PictureStep{{Key: "event_image/video", Database: "gfx/media_aliases"}},
		FileKey: "video", Fallback: "fallback",
	}},
	Source: SourceVanilla,
}

// eu5Picture is EU5's event picture (observed in vanilla: no document names illustration_tags).
var eu5Picture = EventPicture{
	FileKey: "image",
	Chains: []PictureChain{{
		Steps: []PictureStep{
			{Key: "illustration_tags", Database: "gfx/illustrations/database", Match: "tags"},
			{Key: "scene", Database: "gfx/scenes"},
			{Key: "ecs_illustration_image/list", Database: "gfx/images", Match: "tag"},
		},
		FileKey: "illustration_image/texture_file",
	}},
	Source: SourceVanilla,
}

// LocReplaceFolders are the two forms of the folder whose localization overrides every other (the executables match
// the path part /replace/; vanilla ships none). The reader is the session's localization.
var LocReplaceFolders = []LocReplaceFolder{
	{Path: "localization/%s/replace", Source: SourceExe}, {Path: "localization/replace/%s", Source: SourceExe},
}

// LocFillIns are the values a localization text may name that no file defines. The reader is the localization verdict.
var LocFillIns = []LocFillIn{
	{"MODEL_SIZE", SourceDocs}, {"ITEM_INDEX", SourceDocs}, {"ITEM_NUM", SourceDocs}, {"IS_LAST_INDEX", SourceDocs},
	{"IS_LAST_ITEM", SourceVanilla}, {"ITEM", SourceVanilla},
}

// MisspelledFolders are folder names the game never reads, by the name it reads instead.
var MisspelledFolders = map[string]string{"localisation": "localization"}

// ClassDatabases maps the dumps' class names that no naming rule resolves (CCourtPositionTypesDatabase) to the path of
// their database; each stands on SourceDumps for the class and SourceVanilla for the database.
var ClassDatabases = map[string]string{
	"CCultureInnovationTypeDatabase":     "culture/innovations",
	"CCultureTraditionDatabase":          "culture/traditions",
	"CCourtPositionTypesDatabase":        "court_positions/types",
	"CTravelPointOfInterestTypeDatabase": "travel/point_of_interest_types",
}

// plurals turn a folder's last word into one thing's; the first ending that fits wins.
var plurals = []struct{ Plural, One string }{
	{"ouses", "ouse"}, {"eases", "ease"}, {"ies", "y"}, {"ses", "s"}, {"xes", "x"}, {"ches", "ch"}, {"shes", "sh"},
	{"s", ""},
}

// ck3Rules are CK3's rules: the shared rules, with its own over them.
var ck3Rules = makeGameRules([]DatabaseRule{
	makeFolderRule("common/on_action", DatabaseRule{Houses: "on_actions", RunOrder: onActionOrder, Source: SourceDocs,
		Precedence: makeOnActionsPrecedence(SourceDocs),
		References: []Reference{{KeyPath: "on_actions"}, {KeyPath: "random_on_action/<number>"}, {KeyPath: "first_valid_on_action"}, {KeyPath: "fallback"}}}),
	makeEventRule(DatabaseRule{ScopeKey: "scope", DefaultScope: "character", RunOrder: ck3EventOrder, Source: SourceDocs,
		Precedence: &Precedence{Keeps: "highest", PriorityKey: "id_override_priority", Source: SourceDocs}, EventPicture: ck3Picture}),
	makeFolderRule("common/decisions", DatabaseRule{RunOrder: []string{"is_shown", "is_valid", "cost", "cooldown", "effect"}, Source: SourceDocs}),
	makeFolderRule("common/game_concepts", DatabaseRule{Houses: "concepts", LocPrefix: conceptPrefix}),
	makeFolderRule("common/traits", DatabaseRule{AliasKeys: []string{"group", "group_equivalence"}}),
	makeFolderRule("gfx/map/map_object_data", DatabaseRule{NamedBy: "nothing"}),
	makeFolderRule("history/titles", DatabaseRule{Houses: "history", Target: "landed_titles"}),
	makeFolderRule("history/provinces", DatabaseRule{Houses: "history", Target: "map_data.definition"}),
	makeFolderRule("history/province_mapping", DatabaseRule{Houses: "history", Target: "map_data.definition"}),
	makeFolderRule("history/cultures", DatabaseRule{Houses: "history", Target: "culture/cultures", NamedBy: "file"}),
	makeFolderRule("history/faiths", DatabaseRule{Houses: "history", Target: "religion/faith_types"}),
	makeFolderRule("history/artifacts", DatabaseRule{Houses: "history"}),
	makeFolderRule("history/situations", DatabaseRule{Houses: "history"}),
	makeFolderRule("history/struggles", DatabaseRule{Houses: "history"}),
	makeFolderRule("history/wars", DatabaseRule{Houses: "history", NamedBy: "nothing"}),
	makeFolderRule("history/characters", DatabaseRule{Shape: ShapeSetup, Dated: true, Precedence: &Precedence{
		Keeps: "highest", PriorityKey: "history_override_priority", Source: SourceDocs,
		Overrides: []string{"name", "dna", "female", "martial", "prowess", "diplomacy", "intrigue", "stewardship",
			"learning", "trait", "father", "mother", "disallow_random_traits", "obscured", "birth"},
	}}),
	{Name: "reader_export", Source: SourceDocs}, // _reader_export.info: loaded before any other file
	{Name: "map_data.definition", Path: "map_data/definition.csv", NamedBy: "column"},
	makeNestedRule("common/landed_titles", "", ""),
	makeNestedRule("common/activities/activity_types", "options", "option_category"),
	makeNestedRule("common/activities/activity_types", "phases", "phase"),
	makeNestedRule("common", "**/parameters", "parameter"),
	makeNestedRule("common", "**/*_parameters", "parameter"),
	makeNestedRule("common/game_concepts", "alias", ""),
	makePrecedenceRule("gui.type", Precedence{Keeps: "first", Source: SourceExe}),
	makePrecedenceRule("gui.template", Precedence{Keeps: "first", Source: SourceExe}),
})

// vic3Rules are Victoria 3's rules: the shared rules, with its own over them.
var vic3Rules = makeGameRules([]DatabaseRule{
	makeFolderRule("common/on_actions", DatabaseRule{Houses: "on_actions", RunOrder: onActionOrder, Source: SourceDocs,
		Precedence: makeOnActionsPrecedence(SourceDocs),
		References: []Reference{{KeyPath: "on_actions"}, {KeyPath: "random_on_actions/<number>"}, {KeyPath: "first_valid_on_action"}, {KeyPath: "fallback"}}}),
	makeEventRule(DatabaseRule{TypeKey: "type", TypeSuffix: "_event", DefaultScope: "country", RunOrder: eventOrder, Source: SourceWiki,
		Precedence: &Precedence{Keeps: "first", Source: SourceWiki}, EventPicture: vic3Picture}),
	makeFolderRule("gfx/map/map_object_data", DatabaseRule{NamedBy: "nothing"}),
	makeFolderRule("gfx/map/post_effects", DatabaseRule{NamedBy: "field", NameKey: "name", WrapperKeys: []string{"posteffect_volumes"}}),
	// COUNTRIES = { … }: effects
	makeFolderRule("common/history", DatabaseRule{Houses: "history", NamedBy: "nothing", Shape: ShapeEffects}),
	makeFolderRule("common/scripted_rules", DatabaseRule{Precedence: makeReplaceOnlyPrecedence(SourceWiki)}),
	// the wiki lists these in the folders that take modes yet tells modders to override them by a later file
	makeFolderRule("common/technology/technologies", DatabaseRule{Precedence: &Precedence{Keeps: "first", Modes: "entry", Source: SourceInferred}}),
	makeFolderRule("common/technology/eras", DatabaseRule{Precedence: &Precedence{Keeps: "first", Modes: "entry", Source: SourceInferred}}),
	makeFolderRule("common/strategic_regions", DatabaseRule{Precedence: &Precedence{Keeps: "first", Modes: "entry", Source: SourceInferred}}),
	makePrecedenceRule("gui.type", Precedence{Keeps: "first", Source: SourceWiki}),
	makePrecedenceRule("gui.template", Precedence{Keeps: "first", Source: SourceWiki}),
	makePrecedenceRule("script_values", *makeReplaceOnlyPrecedence(SourceWiki)),
	makePrecedenceRule("scripted_effects", *makeReplaceOnlyPrecedence(SourceWiki)),
	makePrecedenceRule("scripted_triggers", *makeReplaceOnlyPrecedence(SourceWiki)),
	makePrecedenceRule("genes", Precedence{Keeps: "first", Modes: "wrapped", Source: SourceWiki}),
})

// eu5Rules are EU5's rules: the shared rules, with its own over them.
var eu5Rules = makeGameRules([]DatabaseRule{
	makeFolderRule("common/on_action", DatabaseRule{Houses: "on_actions", RunOrder: onActionOrder, Source: SourceDocs,
		Precedence: makeOnActionsPrecedence(SourceWiki),
		References: []Reference{{KeyPath: "on_actions"}, {KeyPath: "random_on_action/<number>"}, {KeyPath: "first_valid_on_action"}, {KeyPath: "fallback"}}}),
	makeEventRule(DatabaseRule{TypeKey: "type", TypeSuffix: "_event", DefaultScope: "country", RunOrder: eventOrder, Source: SourceDocs,
		Precedence: &Precedence{Keeps: "first", Source: SourceWiki}, EventPicture: eu5Picture}),
	makeFolderRule("gfx/map/map_objects", DatabaseRule{NamedBy: "nothing"}),
	makeFolderRule("gfx/map/city_data", DatabaseRule{NamedBy: "field", NameKey: "name"}),
	makeFolderRule("common/game_concepts", DatabaseRule{Houses: "concepts", LocPrefix: conceptPrefix, FoldsCase: true, Source: SourceVanilla}),
	makeFolderRule("setup/start", DatabaseRule{Houses: "history"}),
	makeFolderRule("setup/templates", DatabaseRule{NamedBy: "file"}),
	makeFolderRule("map_data/location_templates.txt", DatabaseRule{Houses: "history", Target: "map_data/named_locations"}),
	makeFolderRule("map_data/generated_locators_port.txt", DatabaseRule{NamedBy: "nothing"}),
	makeNestedRule("gfx/defines", "", "define"),
	makeNestedRule("common/laws", "", "policy"),
	makeNestedRule("common/languages", "dialects", "dialect"),
	makeNestedRule("common/gods", "omens", "omen"),
	makeNestedRule("common/building_types", "unique_production_methods", "unique_production_method"),
	makeNestedRule("common/game_concepts", "alias", ""),
	makeNestedRule("map_data/definitions.txt", "", "sub_continent"),
	makeNestedRule("map_data/definitions.txt", "*", "region"),
	makeNestedRule("map_data/definitions.txt", "*/*", "area"),
	makeNestedRule("map_data/definitions.txt", "*/*/*", "province"),
	makeNestedRule("setup/start", "character_db", "character"),
	makeNestedRule("setup/start", "dynasty_manager", "dynasty"),
	makeSetupRule("setup/start", "countries/countries", "country", "setup/countries"),
	makeSetupRule("setup/start", "locations", "location", "map_data/named_locations"),
	makeSetupRule("setup/start", "road_network", "road", "map_data/named_locations"),
	makeSetupRule("setup/start", "townrights_manager", "town_rights", "map_data/named_locations"),
	makeSetupRule("setup/start", "building_manager", "building", "building_types"),
	makeSetupRule("setup/start", "work_of_art_manager", "work_of_art", "artist_work"),
	makeSetupRule("setup/start", "colony_manager", "colony", "map_data/definitions.txt.province"),
	makeSetupRule("setup/start", "institution_manager/institutions", "institution", "institution"),
	makeSetupRule("setup/start", "situation_manager", "situation", "situations"),
	makePrecedenceRule("gui.type", Precedence{Keeps: "last", ByPlayset: true, Source: SourceInferred}),
	makePrecedenceRule("gui.template", Precedence{Keeps: "last", ByPlayset: true, Source: SourceInferred}),
	makePrecedenceRule("coat_of_arms/coat_of_arms", Precedence{Keeps: "last", Source: SourceWiki}),
	makePrecedenceRule("scripted_effects", *makeReplaceOnlyPrecedence(SourceInferred)),
	makePrecedenceRule("scripted_triggers", *makeReplaceOnlyPrecedence(SourceInferred)),
	// the DLC writes INJECT_OR_CREATE: inside accessory_genes
	makePrecedenceRule("genes", Precedence{Keeps: "first", Modes: "wrapped", Source: SourceVanilla}),
})

// vic3ModeFolders is where Victoria 3 takes entry modes: the wiki's Mod compatibility list.
var vic3ModeFolders = ModeFolders{
	Precedence: Precedence{Keeps: "first", Modes: "entry", Source: SourceWiki},
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

// eu5ModeFolders is where EU5 takes entry modes; no source lists the folders.
var eu5ModeFolders = ModeFolders{
	Precedence: Precedence{Keeps: "first", Modes: "entry", Source: SourceInferred},
	Roots:      []string{"common", "setup"},
	Except:     []string{"on_action", "defines"},
}

// sharedRules are every game's rules: the file types', then the databases'; a rule whose folder a game lacks matches
// nothing. In a .gui file a word before a top block declares it (template x { }) or groups declarations (types x { }).
// A key ending in color holds a color even untagged (a culture's color, fontcolor), except a portrait gene's.
var sharedRules = []DatabaseRule{
	{Extension: ".txt", ColorSuffix: "color"},
	{Extension: ".asset", NameKey: "name"},
	{Extension: ".font", NameKey: "name"},
	{
		Extension:     ".gui",
		DeclaringKeys: []string{"type", "local_type", "template", "local_template", "block", "layer"},
		NameKeyOf:     map[string]string{"texticon": "icon", "format": "name"},
		WidgetKey:     "name",
		Shape:         ShapeWidget,
		TagCalls:      "widget",
		ContextKey:    "datacontext",
		ColorSuffix:   "color",
		References: []Reference{
			{KeyPath: "using"}, {KeyPath: "layer", Database: "gui.layer"}, {KeyPath: "texture", Database: File},
			{KeyPath: "font", Database: "font.font"}, {KeyPath: "text", Database: Localization},
			{KeyPath: "title", Database: Localization},
		},
	},
	makeFolderRule("common/scripted_effects", DatabaseRule{CallerWords: []string{callEffect}, Shape: ShapeEffects, DeclaringKeys: []string{"scripted_effect"}, TakesParameters: true}),
	makeFolderRule("common/scripted_triggers", DatabaseRule{CallerWords: []string{callTrigger}, Shape: ShapeTriggers, DeclaringKeys: []string{"scripted_trigger"}, TakesParameters: true}),
	makeFolderRule("common/scripted_modifiers", DatabaseRule{CallerWords: []string{callValue}, TakesParameters: true}),
	makeFolderRule("common/script_values", DatabaseRule{CallerWords: []string{callTrigger, callValue}, Shape: ShapeValue}),
	makeFolderRule("common/scripted_lists", DatabaseRule{Houses: "lists"}),
	makeFolderRule("common/genes", DatabaseRule{WrapperKeys: []string{"color_genes", "special_genes", "morph_genes", "accessory_genes",
		"decal_atlases", "age_presets"}}),
	makeFolderRule("common/named_colors", DatabaseRule{WrapperKeys: []string{"colors"}}),
	makeFolderRule("data_binding", DatabaseRule{NamedBy: "field", NameKey: "definition", Houses: "functions"}),
	makeFolderRule("common/game_concepts", DatabaseRule{Houses: "concepts"}),
	makeFolderRule("common/achievement_groups.txt", DatabaseRule{NamedBy: "field", NameKey: "name"}),
	makeFolderRule("common/coat_of_arms/options", DatabaseRule{NamedBy: "nothing"}),
	makeFolderRule("common/coat_of_arms/coat_of_arms", DatabaseRule{WrapperKeys: []string{"template"}}),
	makeFolderRule("common/coat_of_arms/template_lists", DatabaseRule{WrapperKeys: []string{"coat_of_arms_template_lists",
		"pattern_texture_lists", "colored_emblem_texture_lists", "textured_emblem_texture_lists", "color_lists"}}),
	makeFolderRule("gui/scripted_widgets", DatabaseRule{NamedBy: "nothing"}),
	// a widget key naming a type, its own file's form, a template used by name and a block overridden by name
	{Name: "gui.type", CallerWords: []string{"widget"}, FoldsCase: true, Source: SourceVanilla},
	{Name: "gui.local_type", CallerWords: []string{"widget"}, Local: true},
	{Name: "gui.template", CallerWords: []string{"using"}},
	{Name: "gui.local_template", CallerWords: []string{"using"}, Local: true},
	{Name: "gui.block", CallerWords: []string{"blockoverride"}, FoldsCase: true, Source: SourceVanilla},
	makeFolderRule("music/music_player_categories", DatabaseRule{NamedBy: "field", NameKey: "id"}),
	makeFolderRule("common/defines", DatabaseRule{Precedence: makeMergedPrecedence(SourceWiki)}),
	makeFolderRule("localization", DatabaseRule{FoldsCase: true, Source: SourceVanilla}), // a $KEY$ may differ in case
	{Name: "defines.define", ParentPath: "common/defines", Precedence: makeMergedPrecedence(SourceWiki)},
}

// LookupPrecedence returns how a database's definitions combine: its rule's, else that of the databases that take
// entry modes where its folder takes them, else the game's default. Path is the folder its rule declares.
func (game *Game) LookupPrecedence(database, path string) Precedence {
	if index := slices.IndexFunc(game.Rules, func(rule DatabaseRule) bool {
		return rule.Name == database && rule.Precedence != nil
	}); index >= 0 {
		return *game.Rules[index].Precedence
	}
	if game.ModeFolders.takesModes(database, path) {
		return game.ModeFolders.Precedence
	}
	return game.DefaultPrecedence
}

// takesModes reports whether a database's folder is one the game lists or under a root it lists, and not excepted.
func (folders ModeFolders) takesModes(database, path string) bool {
	switch {
	case path == "", slices.Contains(folders.Except, database), slices.Contains(folders.Except, DeriveDatabaseName(path)):
		return false
	case len(folders.Paths) > 0:
		return slices.ContainsFunc(folders.Paths, func(listed string) bool {
			return path == listed || strings.HasPrefix(path, listed+"/")
		})
	}
	root, _, _ := strings.Cut(path, "/")
	return slices.Contains(folders.Roots, root)
}

// FindRule returns the rule written for a folder or file; false for none.
func (game *Game) FindRule(path string) (DatabaseRule, bool) {
	for _, rule := range game.Rules {
		if rule.Path != "" && rule.Path == path {
			return rule, true
		}
	}
	return DatabaseRule{}, false
}

// FindNestedRules returns the rules for keys written inside the entries of a file.
func (game *Game) FindNestedRules(rel string) (rules []DatabaseRule) {
	for _, rule := range game.Rules {
		if rule.ParentPath != "" && (rel == rule.ParentPath || strings.HasPrefix(rel, rule.ParentPath+"/")) {
			rules = append(rules, rule)
		}
	}
	return rules
}

// FindRuleByExtension returns the file type's rule of an extension (".gui"), the same in every game; the zero rule
// for an extension no rule names.
func FindRuleByExtension(extension string) DatabaseRule {
	if extension == "" {
		return DatabaseRule{}
	}
	for i := range sharedRules {
		if sharedRules[i].Extension == extension {
			return sharedRules[i]
		}
	}
	return DatabaseRule{}
}

// SplitMode splits a mode prefix off a written key: REPLACE:x is x with mode REPLACE.
func (game *Game) SplitMode(key string) (mode, name string) {
	prefix, rest, cut := strings.Cut(key, ":")
	if cut && slices.ContainsFunc(game.EntryModes, func(entryMode EntryMode) bool { return entryMode.Prefix == prefix }) {
		return prefix, rest
	}
	return "", key
}

// ResolveFolder returns the folder a rule declares its database in: its path, or a nested rule's parent; "" none.
func (rule DatabaseRule) ResolveFolder() string { return cmp.Or(rule.Path, rule.ParentPath) }

// DeriveDatabaseName returns the database a folder or file makes: its path without common/.
func DeriveDatabaseName(path string) string { return strings.TrimPrefix(path, "common/") }

// DescribeEntry returns what an entry of a database, or its entries, are called: "scripted effect" or "scripted
// effects" for scripted_effects.
func DescribeEntry(database string, plural bool) string {
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

// makeFolderRule makes the rule of the database a folder or file holds.
func makeFolderRule(path string, rule DatabaseRule) DatabaseRule {
	rule.Name, rule.Path = DeriveDatabaseName(path), path
	return rule
}

// makeEventRule makes the rule of the events folder, whose entries a file's namespace names.
func makeEventRule(rule DatabaseRule) DatabaseRule {
	rule.Houses, rule.NamespaceKey = "events", "namespace"
	return makeFolderRule("events", rule)
}

// makeNestedRule makes the rule of keys written inside the entries of a folder or file, at a key path from each
// entry; database "" makes them entries of the holding database itself (a title inside a title).
func makeNestedRule(parent, keyPath, database string) DatabaseRule {
	rule := DatabaseRule{Name: DeriveDatabaseName(parent), ParentPath: parent, KeyPath: keyPath}
	if database != "" {
		rule.Name += "." + database
	}
	return rule
}

// makeSetupRule makes a nested rule whose entries set up those of a database defined elsewhere.
func makeSetupRule(parent, keyPath, database, target string) DatabaseRule {
	rule := makeNestedRule(parent, keyPath, database)
	rule.Houses, rule.Target = "history", target
	return rule
}

// makePrecedenceRule makes the shared rule of a database with a game's precedence.
func makePrecedenceRule(database string, precedence Precedence) DatabaseRule {
	rule := sharedRules[slices.IndexFunc(sharedRules, func(rule DatabaseRule) bool {
		return rule.Name == database && rule.KeyPath == ""
	})]
	rule.Precedence = &precedence
	return rule
}

// makeOnActionsPrecedence makes how on_actions combine: lists append, and a second trigger or effect is an error.
func makeOnActionsPrecedence(source Source) *Precedence {
	return &Precedence{
		Keeps: "merged", Adds: []string{"events", "on_actions", "random_events"}, Once: []string{"trigger", "effect"}, Source: source,
	}
}

// makeMergedPrecedence makes how a database combines whose definitions all contribute (defines, per define).
func makeMergedPrecedence(source Source) *Precedence {
	return &Precedence{Keeps: "merged", Source: source}
}

// makeReplaceOnlyPrecedence makes how a database that takes entry modes combines where INJECT acts as REPLACE.
func makeReplaceOnlyPrecedence(source Source) *Precedence {
	return &Precedence{Keeps: "first", Modes: "entry", ReplaceOnly: true, Source: source}
}

// makeGameRules makes a game's rules: the shared rules with its own over them, by database.
func makeGameRules(own []DatabaseRule) []DatabaseRule {
	rules := slices.Clone(sharedRules)
	for _, rule := range own {
		if index := slices.IndexFunc(rules, func(existing DatabaseRule) bool {
			return existing.Name == rule.Name && existing.KeyPath == rule.KeyPath && existing.Extension == rule.Extension
		}); index >= 0 {
			rules[index] = rule
		} else {
			rules = append(rules, rule)
		}
	}
	return rules
}
