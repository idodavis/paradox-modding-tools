// databases.go holds the database tables: where definitions are written, how
// they are named and how copies of one name combine, each row for the games
// it names.

package game

import (
	"path"
	"slices"
	"strings"
)

// Database is the row of one database's folder or file. A definition's name is
// the word script calls it by: brave in brave = { … }, which has_trait = brave
// refers to. A definition is named by its top key unless its row says
// otherwise.
type Database struct {
	// Folder is the folder or file the definitions are written in.
	Folder string

	// Games are the games the row holds for, every game if none.
	Games []string

	// Contents is what the database holds beyond definitions.
	Contents Contents

	// NameKey is the key whose value names a definition in place of its top
	// key, as id = x does. It is "" for none.
	NameKey string

	// DefinedBy is what defines a definition in place of a top key. It is ""
	// for the top key itself.
	DefinedBy DefinedBy

	// RootKey is the key whose value names the scope a definition's script
	// starts in. It is "" for none.
	RootKey string

	// WrapperKeys are keys at the top that only wrap definitions.
	WrapperKeys []string

	// DeclaringKeys are keys that declare a definition inline, as
	// scripted_effect x = { } does.
	DeclaringKeys []string

	// Aliases are the keys whose value is a second name for a definition, as
	// group = x, each with the database that name is in: "" for its own.
	Aliases map[string]string

	// RunOrder is a definition's blocks in the order they run. Unlisted keys
	// run after them, as written.
	RunOrder []string

	// Shape is what a definition's block holds. It is "" for
	// the definition's own fields.
	Shape Shape

	// Target is the database whose definitions this one's set up or are named
	// after. It is "" for none.
	Target string

	// Mirrors is whether its definitions are named after several other
	// databases', and FoldsCase whether its references match in any case.
	Mirrors   bool
	FoldsCase bool

	// Prefixes are the link prefixes written before a definition's name, as
	// in dynasty:<key>.
	Prefixes []string

	// Separator is the character that joins a definition's name to a nested
	// key, as | does in NAI|X. It is "" for none.
	Separator string

	// AllowsUndefined is whether a reference needs no definition here: the
	// game falls back to localization keys such as <effect>_first.
	AllowsUndefined bool
}

// DefinedBy is what defines the definitions of a database in place of a top
// key. The zero DefinedBy is the top key itself.
type DefinedBy string

// What can define a database's definitions.
const (
	// DefinedByFile is a file: each file is one definition.
	DefinedByFile DefinedBy = "file"

	// DefinedByColumn is a table's row, named by its first column.
	DefinedByColumn DefinedBy = "column"

	// DefinedByNothing is nothing: no top key defines a definition.
	DefinedByNothing DefinedBy = "nothing"
)

// Contents is what a database holds beyond definitions. The zero Contents is
// definitions alone.
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

// Nesting is a database whose definitions are keys inside another database's
// definitions. Its name is the parent's, a dot and its own Database.
type Nesting struct {
	// Games are the games the row holds for, every game if none.
	Games []string

	// Parent is the folder or file whose definitions hold its keys.
	Parent string

	// KeyPath is the key path from the holding definition to its keys. It is
	// "" for the definition's block; * is any key and ** any depth.
	KeyPath string

	// Database is its name after the parent's and a dot. It is "" for the
	// parent's own database.
	Database string

	// Keys is the pattern a key fits to define one, other keys being fields.
	// It is "" for every key.
	Keys string

	// NameKey is the key whose value names a definition. It is "" for the key
	// that defines it.
	NameKey string

	// Target is the database whose definitions its own set up. It is "" for
	// none.
	Target string

	// Sets is whether its keys only switch a name on or give a definition a
	// second name, defining nothing.
	Sets bool
}

// Callable is a database whose definitions script or a GUI file calls by
// name.
type Callable struct {
	// Callers are where a definition is called from: a trigger, an effect or
	// a value block, or a GUI key.
	Callers []string

	// Shape is what a definition's block holds. It is "" for
	// the caller's shape.
	Shape Shape

	// Parameters is whether a call passes parameters, and Local whether the
	// definitions answer only in the file that declares them.
	Parameters bool
	Local      bool

	// FoldsCase is whether its references match in any case.
	FoldsCase bool

	// Proves are the shapes a block that calls one of its definitions can
	// have.
	Proves []Shape

	// Command is the database of the commands a definition acts as where it
	// is called: DumpEffects or DumpTriggers. It is "" for neither.
	Command string

	// Values are what a call writes as its value where it passes no
	// parameters. None means a call writes none.
	Values []string
}

// FileType is what a file's extension says about a file, beyond what the
// databases of its folder say.
type FileType struct {
	// NameKey is the key whose value names a top block's definition. It is ""
	// for the block's own key.
	NameKey string

	// ColorSuffix is the ending of keys whose untagged number block is a
	// color. It is "" for none.
	ColorSuffix string

	// Shape is what a top block holds. It is "" for the
	// database's.
	Shape Shape
}

// Precedence is how definitions of one name combine in the databases it holds
// for.
type Precedence struct {
	// Games are the games the row holds for, every game if none.
	Games []string

	// Databases are the databases it holds for, by name or path.Match
	// pattern, and Folders the folders, each with everything below it.
	Databases []string
	Folders   []string

	// Keeps is which copy the game keeps, and ByPlayset whether the order is
	// the playset's rather than the load order's.
	Keeps     Keep
	ByPlayset bool

	// Modes is where an entry mode is written. It is "" where the databases
	// take none.
	Modes ModePlace

	// PriorityKey is the key that carries a definition's override priority.
	// It is "" for none.
	PriorityKey string

	// Overrides are the keys a higher priority replaces. Nil means the whole
	// definition.
	Overrides []string

	// Injection is what INJECT: does where the databases take modes.
	Injection Injection

	// Contested are the blocks that two merged copies cannot both write.
	Contested []string
}

// Keep is which copy of a name the game keeps where several definitions share
// it.
type Keep string

// The copies the game can keep: the first one read, the last one read, the
// one with the highest override priority, or every copy, merged into one.
const (
	KeepFirst   Keep = "first"
	KeepLast    Keep = "last"
	KeepHighest Keep = "highest"
	KeepMerged  Keep = "merged"
)

// ModePlace is where a database's definitions write their entry modes. The
// zero ModePlace is nowhere: the database takes no modes.
type ModePlace string

// The places an entry mode is written in.
const (
	// ModePlaceEntry is before a definition's name at the top of its file, as
	// in REPLACE:x = { … }.
	ModePlaceEntry ModePlace = "entry"

	// ModePlaceWrapped is before a definition's name inside the block that
	// wraps it, as in accessory_genes = { INJECT_OR_CREATE:x = { … } }.
	ModePlaceWrapped ModePlace = "wrapped"
)

// Injection is what INJECT: does to a definition already loaded. The zero
// Injection adds to it.
type Injection string

// The injections that differ from adding.
const (
	// InjectionReplaces takes the loaded definition's place, as REPLACE: does.
	InjectionReplaces Injection = "replaces"

	// InjectionUnsettled is that no source says what INJECT: does.
	InjectionUnsettled Injection = "unsettled"
)

// Reference is a key path whose values name definitions.
type Reference struct {
	// Games are the games the row holds for, every game if none.
	Games []string

	// Database is the database whose definitions hold the key path. It is ""
	// for a GUI file's widgets, with a one-key path.
	Database string

	// KeyPath is the key path from the definition; <number> matches a number
	// key.
	KeyPath string

	// Target is the database the values name. It is "" for the row's own.
	Target string

	// Folder is the folder of a file that a value names with no folder of
	// its own. It is "" for none.
	Folder string
}

// DatabaseField is one field of a database's definitions with what no dump
// states about it.
type DatabaseField struct {
	// Games are the games the row holds for, every game if none.
	Games []string

	// Database is the database whose definitions hold the field, and KeyPath
	// the field's key path from the definition; <number> matches a number
	// key.
	Database, KeyPath string

	// Shape is what the field's block holds. It is "" where the row states
	// none.
	Shape Shape

	// Weighted is whether the field's key is a number whose share of its
	// siblings' is its chance.
	Weighted bool

	// Blurb is the field's hover text.
	Blurb string
}

// EntryMode is a prefix written before a definition's name, and what it does
// to a definition already loaded.
type EntryMode struct {
	// Prefix is the mode as written, such as INJECT or TRY_REPLACE.
	Prefix string

	// Replaces is whether it takes the earlier definition's place rather than
	// adding to it.
	Replaces bool

	// Optional is whether it does nothing where there is no earlier
	// definition, as TRY_ says.
	Optional bool

	// Creates is whether it defines one where there is none, as _OR_CREATE
	// says.
	Creates bool
}

// PictureStep is one hop from an event toward its picture.
type PictureStep struct {
	// Key is the key path read in the hop's block. key[field=value] picks the
	// block of that key whose field is the value.
	Key string

	// Database is the database whose definition the key's value names.
	Database string

	// Match is the key path in each definition whose values are matched with
	// the value. It is "" for the definition's name.
	Match string
}

// PictureChain is the hops from an event to a picture file.
type PictureChain struct {
	Steps []PictureStep

	// FileKey is the key path, in the last definition, that holds the picture
	// file.
	FileKey string

	// Fallback is the key that names another definition to use where the
	// file is absent. It is "" for none.
	Fallback string
}

// Picture is how an event's picture is found.
type Picture struct {
	// FileKey is the key that writes a picture file outright. It is "" for
	// none.
	FileKey string

	Chains []PictureChain
}

// GameEvents is one game's rules for events: where an event writes its root
// scope and how its picture is found.
type GameEvents struct {
	// TypeKey is the key whose value, less TypeSuffix, names the event's root
	// scope. It is "" for none.
	TypeKey    string
	TypeSuffix string

	// DefaultScope is the root scope where neither key is written.
	DefaultScope string

	Picture Picture
}

// TextKeys says how the localization keys of some databases' definitions are
// named where a definition does not write them, with the definition's name at
// %s.
type TextKeys struct {
	// Games are the games the row holds for, every game if none.
	Games []string

	// Title is the key of a definition's title. It is "" for the definition's
	// name.
	Title string

	// Description is the key of a definition's description. It is "" for
	// none.
	Description string

	// Databases are the databases it holds for.
	Databases []string
}

// Localization and File are the databases that a localization key and a file
// path name. No definition carries them.
const (
	Localization = "localization"
	File         = "file"
)

// CustomLocalization is the database of the customizable localizations, each
// a block for each text it can show.
const CustomLocalization = "customizable_localization"

// Constant is the database of the constants a file writes for itself as
// @name = value, local to the file.
const Constant = "constant"

// EventNamespaceKey is the key an events file declares a namespace with. A
// file may declare several. Its events are written as <namespace>.N.
const EventNamespaceKey = "namespace"

// TitleKey is the key that names a definition's title localization, as in an
// event's title = <key>.
const TitleKey = "title"

// DescriptionKey is the key that writes an event's description: a
// localization key, or a block that chooses one.
const DescriptionKey = "desc"

// OptionKey is the key of one of an event's options.
const OptionKey = "option"

// OptionTitleKey is the key that writes an option's title: a localization
// key, or a block that picks one.
const OptionTitleKey = "name"

// LabelKey is the key that holds a text's localization key in a block that
// picks the text, as in an option's name = { trigger = { … } text = <key> }
// or a custom_tooltip's text.
const LabelKey = "text"

// HiddenKey is the key whose yes makes an event show no window (CK3
// events/_events.info:38). The three games write it alike.
const HiddenKey = "hidden"

// EventDelay is how a firing's delay is written (Vic3 effects.log:38529).
var EventDelay = struct {
	// Units are the keys that write how long the firing waits, in the order
	// they are looked for.
	Units []string

	// Block is the key of a block that holds the delay of every firing
	// written after it.
	Block string
}{[]string{"days", "weeks", "months", "years"}, "delay"}

// GameRules is how the game rules are written and named, alike in the three
// games: CK3 _game_rules.info:1-20, Vic3 game_rules.md:1-12 and EU5
// main_menu/common/game_rules/_game_rules.info:1-19.
var GameRules = struct {
	// Database is the database of the rules, each a block that holds a block
	// for each setting, and Settings the database of those settings.
	Database, Settings string

	// Trigger is the trigger that asks whether a rule is set to the setting
	// it names.
	Trigger string

	// DefaultKey is the key that names a rule's default setting. With none,
	// the first setting is the default.
	DefaultKey string

	// CategoriesKey is the key of the one block in a rule that is no setting.
	CategoriesKey string

	// Setting and Description are the localization keys of a setting's name
	// and description, with the setting at %s.
	Setting     string
	Description string
}{"game_rules", "game_rules.setting", "has_game_rule", "default", "categories", "setting_%s", "setting_%s_desc"}

// CustomTexts is how a customizable localization is written, alike in the
// three games: a block for each text it can show, which names the text's key
// (CK3 _custom_loc.info:18-38, EU5 customizable_localization.info:1-10,
// Vic3's files).
var CustomTexts = struct {
	// Block is the key of a block that holds one of the texts, and Key the
	// key in that block that names the text's localization key.
	Block string
	Key   string
}{"text", "localization_key"}

// SetupFolder is the folder the start setup's rows are written for. A
// bookmark's start setup folder is read as if its files were here.
const SetupFolder = "setup/start"

// The callers of the scripted databases' definitions: a trigger block, an
// effect block and a value block.
const (
	CallTrigger = "trigger"
	CallEffect  = "effect"
	CallValue   = "value"
)

// The blocks of an on_action and of an event in the order they run, as the
// games' own documents write them.
var (
	onActionOrder = []string{"trigger", "effect", "events"}
	ck3EventOrder = []string{"trigger", "immediate", "title", "desc", "option", "after"}
	eventOrder    = []string{"trigger", "immediate", "option", "after"}
)

// Databases holds the rows of the folders that need one, a game's own before
// every game's. A row for a folder a game lacks matches nothing. A game's rows
// cite its .info files unless noted.
var Databases = []Database{
	// CK3 and EU5 _on_action.info
	{Folder: "common/on_action", Games: ck3AndEU5, Contents: ContentsOnActions, RunOrder: onActionOrder},
	// CK3 events/_events.info:12: scope = scope_type overrides the root scope
	// of an event.
	{Folder: "events", Games: []string{CK3}, Contents: ContentsEvents, RunOrder: ck3EventOrder, RootKey: "scope"},
	// CK3 _messages.info:1, sent by send_interface_message,
	// send_interface_toast and send_interface_popup (effects.log:9999); Vic3's
	// post_notification sends one (effects.log:27072)
	{Folder: "common/messages", Games: ck3AndVic3, Contents: ContentsMessages},
	// _decisions.info:194,200,208
	{
		Folder: "common/decisions", Games: []string{CK3}, Contents: ContentsDecisions,
		RunOrder: []string{"is_shown", "is_valid", "cost", "cooldown", "effect"},
	},
	// a group is also a name, which has_trait holds for any trait in it
	// (_traits.info:3,181,183)
	{Folder: "common/traits", Games: []string{CK3}, Aliases: map[string]string{"group": "", "group_equivalence": ""}},
	// the DLC subfolder writes the same blocks
	{Folder: "common/opinion_modifiers", Games: []string{CK3}},
	{Folder: "gfx/map/map_object_data", Games: ck3AndVic3, DefinedBy: DefinedByNothing},
	{Folder: "history/titles", Games: []string{CK3}, Target: "landed_titles"},
	{Folder: "history/provinces", Games: []string{CK3}, Target: "map_data/definition"},
	{Folder: "history/province_mapping", Games: []string{CK3}, Target: "map_data/definition"},
	{Folder: "history/cultures", Games: []string{CK3}, Target: "culture/cultures", DefinedBy: DefinedByFile},
	{Folder: "history/faiths", Games: []string{CK3}, Target: "religion/faith_types"},
	{Folder: "history/artifacts", Games: []string{CK3}},
	{Folder: "history/situations", Games: []string{CK3}},
	{Folder: "history/struggles", Games: []string{CK3}},
	{Folder: "history/wars", Games: []string{CK3}, DefinedBy: DefinedByNothing},
	// history/_history.info:3 "All history files have the same format"
	{Folder: "history/characters", Games: []string{CK3}, Shape: ShapeSetup},
	// _reader_export.info: what the reader loads before all other files
	{Folder: "reader_export", Games: []string{CK3}},
	{Folder: "map_data/definition.csv", Games: []string{CK3}, DefinedBy: DefinedByColumn},
	// common/province_terrain/_province_terrain.info:3 "This is not a regular
	// keyed database"
	{Folder: "common/province_terrain", Games: []string{CK3}, DefinedBy: DefinedByNothing},
	// common/coat_of_arms/dynamic_definitions/_dynamic_definitions.info:1
	// "Name must match a landed title definition"
	{Folder: "common/coat_of_arms/dynamic_definitions", Games: []string{CK3}, Target: "landed_titles"},
	// scene_settings_byzantine_1_g1.txt:1-6 writes one setting's fields
	{Folder: "gfx/court_scene/scene_settings", Games: []string{CK3}, DefinedBy: DefinedByFile},
	// court_scene_environment.txt:1-10 writes one environment's fields
	{Folder: "gfx/court_scene/scene_environment", Games: []string{CK3}, DefinedBy: DefinedByFile},
	// gfx/map/environment/environment.info:1
	{Folder: "gfx/map/environment", Games: []string{CK3}, DefinedBy: DefinedByFile},
	// _dynasties.info:3
	{Folder: "common/dynasties", Games: []string{CK3}, Prefixes: []string{"dynasty"}},
	// _situations.info:206
	{Folder: "common/situation/situations", Games: []string{CK3}, Prefixes: []string{"situation"}},
	// Vic3 _on_actions.md
	{Folder: "common/on_actions", Games: []string{Vic3}, Contents: ContentsOnActions, RunOrder: onActionOrder},
	// the Vic3 wiki and EU5 _events.info
	{Folder: "events", Games: vic3AndEU5, Contents: ContentsEvents, RunOrder: eventOrder},
	{Folder: "common/decisions", Games: []string{Vic3}, Contents: ContentsDecisions},
	{
		Folder: "gfx/map/post_effects", Games: []string{Vic3}, NameKey: "name", WrapperKeys: []string{"posteffect_volumes"},
	},
	// COUNTRIES = { … }: effects
	{Folder: "common/history", Games: []string{Vic3}, DefinedBy: DefinedByNothing, Shape: ShapeEffects},
	// scripted_tests.md:2 "Every file is a test suite"
	{Folder: "tools/scripted_tests", Games: []string{Vic3}, DefinedBy: DefinedByFile},
	// Vic3 modifier_types.md:1-3,16-17 and EU5 00_modifier_types.txt:1,3
	{Folder: "common/modifier_type_definitions", Games: vic3AndEU5, Contents: ContentsModifiers},
	// map_data/province_terrains.txt:21698 defines x7F25CD, and
	// common/history/states/00_states.txt:137 writes x7f25cd
	{Folder: "map_data/province_terrains.txt", Games: []string{Vic3}, FoldsCase: true},
	// modifier_types.md:2
	{Folder: "common/static_modifiers", Games: []string{Vic3}, Shape: ShapeModifiers},
	// common/geographic_regions/geographic_regions.md:4 names its lists by the
	// short_key: every_country_in_<short_key>
	{Folder: "common/geographic_regions", Games: []string{Vic3}, Aliases: map[string]string{"short_key": ""}},
	// in_game/common/formable_countries/00_formable_countries.txt:18,
	// tag = SCA: a formable names the country it forms
	{Folder: "common/formable_countries", Games: []string{EU5}, Aliases: map[string]string{"tag": "setup/countries"}},
	{Folder: "gfx/map/map_objects", Games: []string{EU5}, DefinedBy: DefinedByNothing},
	{Folder: "gfx/map/city_data", Games: []string{EU5}, NameKey: "name"},
	// a reference to a concept matches in either case
	{Folder: "common/game_concepts", Games: []string{EU5}, Contents: ContentsConcepts, FoldsCase: true},
	// its top keys are the managers that hold the nestings
	{Folder: SetupFolder, Games: []string{EU5}, DefinedBy: DefinedByNothing},
	// its subfolders write the same attachment lists
	{Folder: "gfx/unit_graphics/attachments", Games: []string{EU5}},
	{Folder: "setup/templates", Games: []string{EU5}, DefinedBy: DefinedByFile},
	{Folder: "map_data/location_templates.txt", Games: []string{EU5}, Target: "map_data/named_locations"},
	{Folder: "map_data/generated_locators_port.txt", Games: []string{EU5}, DefinedBy: DefinedByNothing},
	// main_menu/common/modifier_icons: 2,771 definitions, 99.2% named after
	// modifier types
	{Folder: "common/modifier_icons", Games: []string{EU5}, Mirrors: true},
	// The type of a customizable localization is the scope its command is
	// used in: CK3 _custom_loc.info:1-2 and EU5
	// customizable_localization.info:3.
	{Folder: "common/customizable_localization", Games: ck3AndEU5, RootKey: "type"},
	// The scope of a scripted GUI is the scope type it is available to: Vic3
	// scripted_guis.md:3 and EU5 scripted_guis.info:2.
	{Folder: "common/scripted_guis", Games: vic3AndEU5, RootKey: "scope"},
	// The type of a parliament agenda or type is the scope its root is: EU5
	// parliament_agendas/readme.txt:2-5 and parliament_types/readme.txt:2-3.
	{Folder: "common/parliament_agendas", Games: []string{EU5}, RootKey: "type"},
	{Folder: "common/parliament_types", Games: []string{EU5}, RootKey: "type"},
	// The scope of a filter is the type of object it operates on: EU5
	// gui/filters/readme.txt:12.
	{Folder: "gui/filters", Games: []string{EU5}, RootKey: "scope"},
	{Folder: "common/scripted_effects", DeclaringKeys: []string{"scripted_effect"}},
	{Folder: "common/scripted_triggers", DeclaringKeys: []string{"scripted_trigger"}},
	{Folder: "common/scripted_lists", Contents: ContentsLists},
	// CK3 _effect_localization.info:9-17 and _trigger_localization.info:15;
	// EU5 international_organization_effects.txt:169 refers to one no entry
	// defines
	{Folder: "common/effect_localization", AllowsUndefined: true},
	{Folder: "common/trigger_localization", AllowsUndefined: true},
	// its subfolders write the same define groups. define:Namespace|Name
	// reads one (CK3 event_targets.log:109, the Vic3 wiki's Event target
	// page)
	{Folder: "common/defines", Prefixes: []string{"define", "array_define"}, Separator: "|"},
	{
		Folder:      "common/genes",
		WrapperKeys: []string{"color_genes", "special_genes", "morph_genes", "accessory_genes", "decal_atlases", "age_presets"},
	},
	{Folder: "common/named_colors", WrapperKeys: []string{"colors"}},
	{Folder: "data_binding", NameKey: "definition"},
	{Folder: "common/game_concepts", Contents: ContentsConcepts},
	{Folder: "common/achievement_groups.txt", NameKey: "name"},
	{Folder: "common/coat_of_arms/options", DefinedBy: DefinedByNothing},
	{Folder: "common/coat_of_arms/coat_of_arms", WrapperKeys: []string{"template"}, Mirrors: true},
	{
		Folder: "common/coat_of_arms/template_lists",
		WrapperKeys: []string{
			"coat_of_arms_template_lists", "pattern_texture_lists", "colored_emblem_texture_lists",
			"textured_emblem_texture_lists", "color_lists",
		},
	},
	{Folder: "gui/scripted_widgets", DefinedBy: DefinedByNothing},
	{Folder: "music/music_player_categories", NameKey: "id"},
	// its blocks are pattern_textures, pattern_layout and variation, each a
	// database of its own
	{Folder: "gfx/portraits/accessory_variations", DefinedBy: DefinedByNothing},
}

// Callables are the callable databases, by name.
var Callables = map[string]Callable{
	// a call writes yes and never no, as CK3
	// events/activities/activity_system_events.txt:151 does
	"scripted_effects": {
		Callers: []string{CallEffect}, Shape: ShapeEffects, Parameters: true, Proves: []Shape{ShapeEffects}, Command: DumpEffects,
		Values: []string{"yes"},
	},
	// A value block calls them too. A call writes yes, or no to test the
	// opposite, as CK3
	// events/activities/chariot_race_activity/chariot_ongoing_events_jp.txt
	// :189 does.
	"scripted_triggers": {
		Callers: []string{CallTrigger, CallValue}, Shape: ShapeTriggers, Parameters: true, Proves: []Shape{ShapeTriggers, ShapeValue},
		Command: DumpTriggers, Values: Bools,
	},
	// CK3 common/activities/activity_types/camp_party.txt:638
	"scripted_modifiers": {
		Callers: []string{CallValue}, Shape: ShapeValue, Parameters: true, Proves: []Shape{ShapeValue},
		Values: []string{"yes"},
	},
	"script_values": {Callers: []string{CallTrigger, CallValue}, Shape: ShapeValue, Proves: []Shape{ShapeTriggers, ShapeValue}},
	// a widget key naming a type
	"gui.type":           {Callers: []string{guiTagCalls}, FoldsCase: true},
	"gui.local_type":     {Callers: []string{guiTagCalls}, Local: true},
	"gui.template":       {Callers: []string{guiTemplateCalls}},
	"gui.local_template": {Callers: []string{guiTemplateCalls}, Local: true},
	"gui.block":          {Callers: []string{guiBlockCalls}, FoldsCase: true},
}

// FileTypes says what each file extension means. A key ending in color holds a
// color even without a tag, except a portrait gene's.
var FileTypes = map[string]FileType{
	".txt":   {ColorSuffix: "color"},
	".asset": {NameKey: "name"},
	".font":  {NameKey: "name"},
	".gui":   {Shape: ShapeWidget, ColorSuffix: "color"},
}

// References holds the key paths whose values name definitions or files, in
// the order they are tried: those of GUI files, then every game's, then a
// game's own.
var References = []Reference{
	{KeyPath: guiTemplateCalls, Target: "gui.template"}, {KeyPath: "layer", Target: "gui.layer"},
	{KeyPath: "texture", Target: File}, {KeyPath: guiFontKey, Target: guiFontDatabase},
	{KeyPath: "text", Target: Localization}, {KeyPath: "title", Target: Localization},
	{Database: "on_action", KeyPath: "on_actions"}, {Database: "on_action", KeyPath: "random_on_action/<number>"},
	{Database: "on_action", KeyPath: "first_valid_on_action"}, {Database: "on_action", KeyPath: "fallback"},
	{Database: "on_actions", KeyPath: "on_actions"}, {Database: "on_actions", KeyPath: "random_on_actions/<number>"},
	{Database: "on_actions", KeyPath: "first_valid_on_action"}, {Database: "on_actions", KeyPath: "fallback"},
	// an on_action's event lists name events: CK3 _on_actions.info:47-80 and
	// Vic3 _on_actions.md:58-90 write events, random_events (a weight, then
	// the event) and first_valid; EU5's on_action files write the same lists
	{Database: "on_action", KeyPath: "events", Target: "events"},
	{Database: "on_action", KeyPath: "random_events/<number>", Target: "events"},
	{Database: "on_action", KeyPath: "first_valid", Target: "events"},
	{Database: "on_actions", KeyPath: "events", Target: "events"},
	{Database: "on_actions", KeyPath: "random_events/<number>", Target: "events"},
	{Database: "on_actions", KeyPath: "first_valid", Target: "events"},
	// a customizable localization's texts name their keys: CK3
	// common/customizable_localization/_custom_loc.info:18-38, EU5
	// in_game/common/customizable_localization/customizable_localization.info
	// :1-10, and Vic3's own files there
	{Database: CustomLocalization, KeyPath: "text/localization_key", Target: Localization},
	// CK3's trait icons (_traits.info:4,23-35)
	{Games: []string{CK3}, Database: "traits", KeyPath: "icon", Target: File, Folder: "gfx/interface/icons/traits"},
	{Games: []string{CK3}, Database: "traits", KeyPath: "icon/**/desc", Target: File, Folder: "gfx/interface/icons/traits"},
	// CK3 events/_events.info:101,107: a portrait's animation is an
	// "animation name … See animations.txt"
	{Games: []string{CK3}, Database: "events", KeyPath: "*_portrait/animation", Target: "gfx/portraits/portrait_animations"},
	{
		Games: []string{CK3}, Database: "events", KeyPath: "*_portrait/triggered_animation/animation",
		Target: "gfx/portraits/portrait_animations",
	},
	{Games: []string{CK3}, Database: "events", KeyPath: "sender/animation", Target: "gfx/portraits/portrait_animations"},
	{
		Games: []string{CK3}, Database: "events", KeyPath: "sender/triggered_animation/animation",
		Target: "gfx/portraits/portrait_animations",
	},
	// CK3 common/epidemics/_epidemics.info:124-128: "holder_event = <event>
	// # Fired for the player county holder of the province", and the same for
	// the liege and the realm
	{Games: []string{CK3}, Database: "epidemics", KeyPath: "outbreak_intensities/*/notification/holder_event", Target: "events"},
	{Games: []string{CK3}, Database: "epidemics", KeyPath: "outbreak_intensities/*/notification/liege_event", Target: "events"},
	{Games: []string{CK3}, Database: "epidemics", KeyPath: "outbreak_intensities/*/notification/realm_event", Target: "events"},
	// a date entry's liege names the title above it, as CK3
	// history/titles/00_other_titles.txt:2525 writes e_hre
	{Games: []string{CK3}, Database: "history/titles", KeyPath: "<number>/liege", Target: "landed_titles"},
	{Games: vic3AndEU5, Database: "events", KeyPath: "title", Target: Localization},
	{Games: vic3AndEU5, Database: "events", KeyPath: "desc", Target: Localization},
	{Games: []string{Vic3}, Database: "events", KeyPath: "flavor", Target: Localization},
	{Games: []string{EU5}, Database: "events", KeyPath: "historical_info", Target: Localization},
	{Games: vic3AndEU5, Database: "events", KeyPath: "option/name", Target: Localization},
	// EU5 in_game/gfx/map/map_modes/readme.txt:6: a map mode's tooltip_key is
	// a script string, whose values are keys
	{Games: []string{EU5}, Database: "gfx/map/map_modes", KeyPath: "tooltip_key/**/value", Target: Localization},
	// Vic3 common/flag_definitions/00_flag_definitions.txt:5,7,10 and EU5
	// main_menu/common/flag_definitions/00_flag_definitions.txt:5,7,10
	{Games: vic3AndEU5, Database: "flag_definitions", KeyPath: "flag_definition/coa", Target: Coat.Database},
	{Games: vic3AndEU5, Database: "flag_definitions", KeyPath: "flag_definition/coa_with_overlord_canton", Target: Coat.Database},
	{Games: vic3AndEU5, Database: "flag_definitions", KeyPath: "flag_definition/subject_canton", Target: Coat.Database},
}

// gameReferences holds the rows of References that hold for each game, by the
// game's id.
var gameReferences = func() map[string][]Reference {
	references := map[string][]Reference{}
	for id, row := range games {
		references[id] = slices.DeleteFunc(slices.Clone(References), func(reference Reference) bool {
			return !row.Matches(reference.Games)
		})
	}

	return references
}()

// The hover texts of an on_action's weighted entries.
const (
	randomEventBlurb    = "One event the on_action can pick; its number is how likely it is. An event of 0 is a chance that none fires."
	randomOnActionBlurb = "One on_action the on_action can pick; its number is how likely it is."
)

// DatabaseFields holds the fields whose shape or weights no dump states.
var DatabaseFields = []DatabaseField{
	// _on_actions.info:55-90 (CK3; EU5's on_actions.info and Vic3's
	// _on_actions.md alike): one entry is picked to fire
	{Database: "on_action", KeyPath: "random_events/<number>", Weighted: true, Blurb: randomEventBlurb},
	{Database: "on_action", KeyPath: "random_on_action/<number>", Weighted: true, Blurb: randomOnActionBlurb},
	{Database: "on_actions", KeyPath: "random_events/<number>", Weighted: true, Blurb: randomEventBlurb},
	{Database: "on_actions", KeyPath: "random_on_actions/<number>", Weighted: true, Blurb: randomOnActionBlurb},
	// CK3 history/_characters.info:65-66,78: a date entry changes the
	// character's own keys, and reads any other key as an effect
	{
		Games: []string{CK3}, Database: "history/characters", KeyPath: "<number>", Shape: ShapeSetup,
		Blurb: "What changes for the character on this date.",
	},
	// EU5 in_game/common/subject_types/readme.txt:80 writes it as goal keys,
	// each holding a type and a bias
	{
		Games: []string{EU5}, Database: "subject_types", KeyPath: "ai_biases", Shape: ShapeFields,
		Blurb: "The weight the AI adds to certain goals, one block for each goal with its type and its bias.",
	},
}

// ToDatabase returns the database name of a folder or file: the path without
// common/ and without its extension.
func ToDatabase(folder string) string {
	database := strings.TrimPrefix(folder, "common/")

	return strings.TrimSuffix(database, path.Ext(database))
}

// FindDatabase returns the row of a folder or file, the game's own before
// every game's. It returns false if there is none.
func (g *Game) FindDatabase(folder string) (Database, bool) {
	for _, row := range Databases {
		if row.Folder == folder && g.Matches(row.Games) {
			return row, true
		}
	}

	return Database{}, false
}

// ListDatabaseFolders returns the folders and files that the game's database
// rows name.
func (g *Game) ListDatabaseFolders() []string {
	var folders []string
	for _, row := range Databases {
		if g.Matches(row.Games) {
			folders = append(folders, row.Folder)
		}
	}

	return folders
}

// DefinitionTexts holds the rows of the databases whose definitions' texts are
// not under their plain names.
var DefinitionTexts = []TextKeys{
	{Games: []string{CK3, EU5}, Title: "game_concept_%s", Databases: []string{"game_concepts"}},
	// localization/english/content_304_l_english.yml:228 titles
	// arranged_accident_failed by this form
	{Games: []string{Vic3}, Title: "notification_%s_name", Databases: []string{"messages"}},
	{Games: []string{CK3}, Description: "%s_desc", Databases: []string{
		"activities/activity_locales", "activities/activity_types", "activities/activity_types.phase",
		"activities/intents", "ai_war_stances", "bookmarks/bookmarks", "bookmarks/challenge_characters",
		"character_memory_types", "council_positions", "court_positions/tasks", "court_positions/types",
		"court_types", "culture/eras", "culture/innovations", "decisions", "dlc_metadata", "factions", "governments",
		"history/faiths", "important_actions", "inspirations", "lease_contracts", "modifiers", "nicknames",
		"puppets/types", "raids/intents", "religion/faith_types", "religion/religion_types", "religion/rite_types",
		"schemes/agent_types", "secret_types", "situation/catalysts", "struggle/catalysts", "struggle/struggles",
		"subject_contracts/groups", "suggestions", "task_contracts", "travel/travel_options", "vassal_stances",
	}},
	{Games: []string{CK3}, Title: "%s_name", Databases: []string{
		"culture/aesthetics_bundles", "culture/creation_names", "dynasty_perks", "lifestyle_perks",
		"religion/doctrine_group_types",
	}},
	{Games: []string{CK3}, Title: "%s_name", Description: "%s_desc", Databases: []string{
		"culture/traditions", "dynasty_legacies", "lifestyles", "religion/doctrine_types", "religion/tenet_types",
	}},
	{Games: []string{CK3}, Title: "%s_title", Databases: []string{"activities/pulse_actions"}},
	{Games: []string{CK3}, Title: "activity_group_type_%s", Databases: []string{"activities/activity_group_types"}},
	{Games: []string{CK3}, Title: "building_%s", Description: "building_%s_desc", Databases: []string{"buildings"}},
	{Games: []string{CK3}, Title: "confederation_type_%s_name", Databases: []string{"confederation_types"}},
	{Games: []string{CK3}, Title: "decision_group_type_%s", Databases: []string{"decision_group_types"}},
	{Games: []string{CK3}, Title: "epidemic_%s", Databases: []string{"epidemics"}},
	{Games: []string{CK3}, Title: "feature_%s", Databases: []string{"artifacts/features"}},
	{Games: []string{CK3}, Title: "message_group_type_%s", Databases: []string{"message_group_types"}},
	{Games: []string{CK3}, Title: "situation_%s", Description: "situation_%s_desc", Databases: []string{
		"situation/situations",
	}},
	{Games: []string{CK3}, Title: "situation_group_type_%s", Databases: []string{"situation/situation_group_types"}},
	{Games: []string{EU5}, Description: "%s_desc", Databases: []string{
		"advances", "age", "ai_personalities", "artist_work", "avatars", "building_categories", "building_types",
		"bureaucracies", "cabinet_actions", "casus_belli", "character_interactions", "child_educations",
		"chivalric_orders", "climates", "colonial_charter_goals", "country_interactions", "country_ranks",
		"culture_groups", "disasters", "diseases", "employment_systems", "estate_privileges", "estates",
		"expedition_types", "formable_countries", "generic_actions", "gfx/map/flatmap",
		"gfx/map/spline_network/spline_styles", "gods", "gods.omen", "goods", "government_reforms",
		"government_types", "hegemons", "heir_selections", "historical_scores", "holy_site_types", "industry_types",
		"institution", "international_organization_payments", "international_organization_special_statuses",
		"international_organizations", "laws", "laws.policy", "location_ranks", "movements", "parliament_agendas",
		"parliament_issues", "parliament_types", "peace_treaties", "pop_types", "recruitment_method", "regencies",
		"religion_groups", "religions", "religious_aspects", "religious_factions", "religious_figures",
		"religious_focuses", "religious_order_callings", "religious_order_types", "religious_schools", "resolutions",
		"road_types", "scenarios", "setup/1337.building", "setup/1337.institution", "setup/1337.situation",
		"setup/1337.work_of_art", "situations", "societal_values", "subject_military_stances", "subject_types",
		"topography", "town_rights", "unit_abilities", "unit_categories", "unit_formation_preference", "unit_types",
		"vegetation",
	}},
	{Games: []string{EU5}, Title: "%s_name", Description: "%s_desc", Databases: []string{"bookmarks"}},
	{Games: []string{Vic3}, Description: "%s_desc", Databases: []string{
		"acceptance_statuses", "ai_regional_objectives", "ai_strategic_region_stance_types", "ai_strategies",
		"amendments", "battle_conditions", "character_interactions", "character_traits", "combat_unit_groups",
		"combat_unit_types", "commander_orders", "company_charter_types", "country_ranks", "decisions", "decrees",
		"diplomatic_actions", "diplomatic_catalysts", "dlc_metadata", "gfx/map/fleet_entities", "glossary",
		"government_types", "harvest_condition_types", "ideologies", "institutions", "interest_group_traits",
		"interest_groups", "interest_tier_types", "labels", "law_groups", "laws", "mobilization_option_groups",
		"mobilization_options", "modifier_type_definitions", "naval_battle_conditions", "naval_mission_types",
		"objectives", "political_lobby_appeasement", "pop_types", "power_bloc_identities",
		"power_bloc_principle_groups", "power_bloc_principles", "ship_groups", "ship_types", "social_classes",
		"social_hierarchies", "technology/technologies", "themes", "treaty_articles",
	}},
	{Games: []string{Vic3}, Title: "%s_name", Description: "%s_desc", Databases: []string{"proposal_types"}},
}

// LookupTextKey returns how the localization key that a database's
// definitions leave unwritten under the given key is named, with the
// definition's name at %s: TitleKey for the title, DescriptionKey for the
// description. A database with no row for the title is titled by the name
// itself. It returns false where the game has no such key.
func (g *Game) LookupTextKey(database, key string) (string, bool) {
	for _, row := range DefinitionTexts {
		if !g.Matches(row.Games) || !slices.Contains(row.Databases, database) {
			continue
		}

		pattern := row.Description
		if key == TitleKey {
			pattern = row.Title
		}

		if pattern != "" {
			return pattern, true
		}
	}

	return "%s", key == TitleKey
}

// Label returns the name of the database a nesting defines: the parent's
// name, then a dot and its own Database, if it has one.
func (n Nesting) Label() string {
	if n.Database == "" {
		return ToDatabase(n.Parent)
	}

	return ToDatabase(n.Parent) + "." + n.Database
}

// FindNestings returns the nestings whose parent database holds a file, the
// game's own before every game's.
func (g *Game) FindNestings(rel string) []Nesting {
	return slices.DeleteFunc(g.ListNestings(), func(row Nesting) bool {
		return rel != row.Parent && !strings.HasPrefix(rel, row.Parent+"/")
	})
}

// Nestings holds the nested databases, a game's own before every game's.
var Nestings = []Nesting{
	// a game rule holds a block for each of its settings (GameRules cites the
	// three games' docs); the keys the docs write for a rule stay its fields
	{Parent: "common/game_rules", Database: "setting"},
	// a title inside a title is a title; its key opens with its tier
	{Games: []string{CK3}, Parent: "common/landed_titles", Keys: "[ekdcb]_*"},
	{Games: []string{CK3}, Parent: "common/activities/activity_types", KeyPath: "options", Database: "option_category"},
	{Games: []string{CK3}, Parent: "common/activities/activity_types", KeyPath: "phases", Database: "phase"},
	// common/confederation_types/10_house_bloc_types.txt:21-24 switches
	// parameters on with `name = yes`
	{Games: []string{CK3}, Parent: "common", KeyPath: "**/parameters", Database: "parameter", Sets: true},
	// common/accolade_types/04_ep2_common_attributes.txt:416-418 lists the
	// names it switches on
	{Games: []string{CK3}, Parent: "common", KeyPath: "**/*_parameters", Database: "parameter", Sets: true},
	// common/game_concepts/00_game_concepts.txt:2 gives vassal the second
	// names of an alias
	{Games: ck3AndEU5, Parent: "common/game_concepts", KeyPath: "alias", Sets: true},
	// EU5's start setup's managers each set up the database their Target
	// names
	{Games: []string{EU5}, Parent: "gfx/defines", Database: "define"},
	{Games: []string{EU5}, Parent: "common/laws", Database: "policy"},
	{Games: []string{EU5}, Parent: "common/languages", KeyPath: "dialects", Database: "dialect"},
	{Games: []string{EU5}, Parent: "common/gods", KeyPath: "omens", Database: "omen"},
	{Games: []string{EU5}, Parent: "common/building_types", KeyPath: "unique_production_methods", Database: "unique_production_method"},
	// map_data/definitions.txt:1-5, europe > western_europe >
	// scandinavian_region > svealand_area > uppland_province
	{Games: []string{EU5}, Parent: "map_data/definitions.txt", Database: "continent"},
	{Games: []string{EU5}, Parent: "map_data/definitions.txt", KeyPath: "*", Database: "sub_continent"},
	{Games: []string{EU5}, Parent: "map_data/definitions.txt", KeyPath: "*/*", Database: "region"},
	{Games: []string{EU5}, Parent: "map_data/definitions.txt", KeyPath: "*/*/*", Database: "area"},
	{Games: []string{EU5}, Parent: "map_data/definitions.txt", KeyPath: "*/*/*/*", Database: "province"},
	{Games: []string{EU5}, Parent: SetupFolder, KeyPath: "character_db", Database: "character"},
	{Games: []string{EU5}, Parent: SetupFolder, KeyPath: "dynasty_manager", Database: "dynasty"},
	{Games: []string{EU5}, Parent: SetupFolder, KeyPath: "countries/countries", Database: "country", Target: "setup/countries"},
	{Games: []string{EU5}, Parent: SetupFolder, KeyPath: "locations", Database: "location", Target: "map_data/named_locations"},
	{Games: []string{EU5}, Parent: SetupFolder, KeyPath: "road_network", Database: "road", Target: "map_data/named_locations"},
	{
		Games: []string{EU5}, Parent: SetupFolder, KeyPath: "townrights_manager", Database: "town_rights",
		Target: "map_data/named_locations",
	},
	{Games: []string{EU5}, Parent: SetupFolder, KeyPath: "building_manager", Database: "building", Target: "building_types"},
	{Games: []string{EU5}, Parent: SetupFolder, KeyPath: "work_of_art_manager", Database: "work_of_art", Target: "artist_work"},
	{
		Games: []string{EU5}, Parent: SetupFolder, KeyPath: "colony_manager", Database: "colony",
		Target: "map_data/definitions.province",
	},
	{
		Games: []string{EU5}, Parent: SetupFolder, KeyPath: "institution_manager/institutions", Database: "institution",
		Target: "institution",
	},
	{Games: []string{EU5}, Parent: SetupFolder, KeyPath: "situation_manager", Database: "situation", Target: "situations"},
	{Parent: "common/defines", Database: "define"},
	// each of the three blocks is named by its name key, as CK3 afr.txt:11-12
	// writes, and two of them may share a name
	{Parent: "gfx/portraits/accessory_variations", Database: "pattern_textures", Keys: "pattern_textures", NameKey: "name"},
	{Parent: "gfx/portraits/accessory_variations", Database: "pattern_layout", Keys: "pattern_layout", NameKey: "name"},
	{Parent: "gfx/portraits/accessory_variations", Database: "variation", Keys: "variation", NameKey: "name"},
}

// ListNestings returns every nesting of the game, its own before every
// game's.
func (g *Game) ListNestings() []Nesting {
	return slices.DeleteFunc(slices.Clone(Nestings), func(row Nesting) bool { return !g.Matches(row.Games) })
}

// IsSet reports whether a nested definition of the given database only
// switches a name on or gives a definition a second name, as a parameter or a
// game concept's alias does.
func (g *Game) IsSet(database string) bool {
	return slices.ContainsFunc(g.ListNestings(), func(row Nesting) bool { return row.Sets && row.Label() == database })
}

// ListSetupNestings returns the start setup's nestings once for each given
// start setup, with its folder as the parent.
func (g *Game) ListSetupNestings(setups ...string) []Nesting {
	var nestings []Nesting
	for _, setup := range setups {
		for _, nesting := range g.FindNestings(SetupFolder) {
			nesting.Parent = setup
			nestings = append(nestings, nesting)
		}
	}

	return nestings
}

// ListReferences returns the key paths in the game's definitions whose values
// name definitions: GUI files' first, then every game's, then the game's own.
// It returns none for a game the package does not list.
func (g *Game) ListReferences() []Reference {
	return gameReferences[g.ID]
}

// Precedences holds how definitions combine. The first row that holds for a
// database wins. The Vic3 rows cite the wiki's Mod compatibility page (1.13)
// by section, and the EU5 rows the same page by line, unless noted.
var Precedences = []Precedence{
	// their lists merge, and two trigger or effect blocks conflict
	{Databases: []string{"on_action", "on_actions"}, Keeps: KeepMerged, Contested: []string{"trigger", "effect"}},
	// events/_events.info:77-83
	{Games: []string{CK3}, Databases: []string{"events"}, Keeps: KeepHighest, PriorityKey: "id_override_priority"},
	// Vic3 Events and EU5 l.169: the first one loaded wins
	{Games: vic3AndEU5, Databases: []string{"events"}, Keeps: KeepFirst},
	// history/_characters.info:25-31
	{
		Games: []string{CK3}, Databases: []string{"history/characters"},
		Keeps: KeepHighest, PriorityKey: "history_override_priority",
		Overrides: []string{
			"name", "dna", "female", "martial", "prowess", "diplomacy", "intrigue", "stewardship", "learning", "trait",
			"father", "mother", "disallow_random_traits", "obscured", "birth",
		},
	},
	// the CK3 wiki's Modding page: types in UI files use FIOS; Vic3 GUI
	// types: the one loaded first wins
	{Games: ck3AndVic3, Databases: []string{"gui.type", "gui.template"}, Keeps: KeepFirst},
	// EU5 l.155: the last by playlist position
	{Games: []string{EU5}, Databases: []string{"gui.type", "gui.template"}, Keeps: KeepLast, ByPlayset: true},
	// a group of defines is opened in several files (CK3 00_defines.txt:1793
	// and 00_graphics.txt:1124, Vic3 00_defines.txt:1382,1749, the EU5 wiki's
	// Defines, l.86); a GUI block belongs to its type, so many share a name
	{Databases: []string{"defines", "gui.block"}, Keeps: KeepMerged},
	// the game's own files write one name in several files, as CK3
	// catholic.txt:107 and crusaders.txt:107 write the pattern_textures
	// catholic_silk_brocade_01, so every copy stays
	{
		Databases: []string{
			"gfx/portraits/accessory_variations.pattern_textures", "gfx/portraits/accessory_variations.pattern_layout",
			"gfx/portraits/accessory_variations.variation",
		},
		Keeps: KeepMerged,
	},
	// CK3 20_doctrines.txt:2066,2079,2102 set coronation_artifact in three
	// doctrines; an activity's categories are unique only within it
	// (_activity_type.info:138-141); dated history entries stack
	// (_history.info:3-13), as 01_admin_titles_tgp.txt:6339,6435 write
	// c_nf_wang_738b_7 twice
	{
		Games: []string{CK3}, Databases: []string{"common.parameter", "activities/activity_types.option_category", "history/*"},
		Keeps: KeepMerged,
	},
	// the EU5 wiki's Setup modding, l.10: its managers are additive; Mod
	// structure, l.78
	{Games: []string{EU5}, Databases: []string{"setup/*"}, Keeps: KeepMerged},
	// one define, named NGroup|KEY, keeps the last copy by file name: the CK3
	// wiki's Modding page, the EU5 wiki's Defines, l.94, and the Vic3 wiki's
	// Defines (1.13)
	{Databases: []string{"defines.define"}, Keeps: KeepLast},
	// Vic3 Behavior and EU5 l.92: INJECT acts as REPLACE
	{Games: vic3AndEU5, Databases: []string{"scripted_effects", "scripted_triggers"}, Keeps: KeepFirst, Modes: ModePlaceEntry, Injection: InjectionReplaces},
	// the Vic3 wiki's list of folders: REPLACE only; it does not say whether
	// INJECT is refused or acts as REPLACE
	{Games: []string{Vic3}, Databases: []string{"scripted_rules", "script_values"}, Keeps: KeepFirst, Modes: ModePlaceEntry, Injection: InjectionUnsettled},
	// modes at the second level: the Vic3 wiki's Genes; EU5
	// D008_genes_accessories.txt:1-3 writes INJECT_OR_CREATE inside
	// accessory_genes
	{Games: vic3AndEU5, Databases: []string{"genes"}, Keeps: KeepFirst, Modes: ModePlaceWrapped},
	// Vic3 DNA data: defined character DNAs do not overwrite each other; the
	// wiki's list of folders gives it entry modes
	{Games: []string{Vic3}, Databases: []string{"dna_data"}, Keeps: KeepMerged, Modes: ModePlaceEntry},
	// the EU5 wiki's Flag modding, l.126
	{Games: []string{EU5}, Databases: []string{"coat_of_arms/coat_of_arms"}, Keeps: KeepLast},
	// the Vic3 wiki's list of folders: the first plain copy stays
	{
		Games: []string{Vic3}, Keeps: KeepFirst, Modes: ModePlaceEntry,
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
	// the EU5 wiki's Mod structure, l.78: modes work in the vast majority of
	// common's subfolders. Its exceptions, on_action and defines, hold rows
	// above.
	{Games: []string{EU5}, Folders: []string{"common"}, Keeps: KeepFirst, Modes: ModePlaceEntry},
}

// LookupPrecedence returns how a database's definitions combine, from the
// first row that holds for it by its name or folder. Where no row holds, the
// last copy by file name wins.
func (g *Game) LookupPrecedence(database, folder string) Precedence {
	for _, row := range Precedences {
		if g.Matches(row.Games) && row.holds(database, folder) {
			return row
		}
	}

	return Precedence{Keeps: KeepLast}
}

// holds reports whether a precedence row holds for a database, by its name or
// its folder. A database with no folder, such as a nested one, is held by its
// name only.
func (p Precedence) holds(database, folder string) bool {
	named := slices.ContainsFunc(p.Databases, func(pattern string) bool {
		matched, _ := path.Match(pattern, database)

		return matched
	})

	return named || (folder != "" && slices.ContainsFunc(p.Folders, func(listed string) bool {
		return folder == listed || strings.HasPrefix(folder, listed+"/")
	}))
}

// KeepsEvery reports whether the game keeps every copy of a name, merged into
// one.
func (p Precedence) KeepsEvery() bool { return p.Keeps == KeepMerged }

// Describe returns, in plain sentences, which copy the game keeps when several
// definitions share a name, and what a mod can do about it.
func (p Precedence) Describe() string {
	var sentences []string

	switch {
	case p.KeepsEvery():
		sentences = append(sentences, "The game keeps every copy and combines them into one.")
		if len(p.Contested) > 0 {
			sentences = append(sentences, "Two copies cannot both write a "+strings.Join(p.Contested, " or ")+
				" block; the game does not say which one it keeps.")
		}

	case p.Keeps == KeepHighest:
		sentences = append(sentences, "The game keeps the copy with the highest "+p.PriorityKey+p.describeOverrides()+
			". If two tie, it does not say which one it keeps.")

	case p.ByPlayset:
		sentences = append(sentences, "The game keeps the copy from the mod that loads last, so moving a mod in the load order "+
			"changes which one it is.")

	case p.Keeps == KeepFirst:
		sentences = append(sentences, "The game reads the files in a folder in order of file name and keeps the first copy it reads.")
		if p.Modes == "" {
			sentences = append(sentences, "A mod's copy wins when its file name sorts before the others.")
		}

	default:
		sentences = append(sentences, "The game reads the files in a folder in order of file name and keeps the last copy it reads.")
		if p.Modes == "" {
			sentences = append(sentences, "A mod's copy wins when its file name sorts after the others.")
		}
	}

	if p.Modes != "" {
		sentences = append(sentences, p.describeModes())
	}

	return strings.Join(sentences, " ")
}

// describeOverrides returns the clause saying that a higher priority replaces
// only some keys. It returns "" if it replaces the whole definition.
func (p Precedence) describeOverrides() string {
	if len(p.Overrides) == 0 {
		return ""
	}

	return " for " + strings.Join(p.Overrides, ", ") + ", and combines the rest"
}

// describeModes returns the sentence saying how a mod changes a definition
// with an entry mode, and what INJECT: does where the sources say.
func (p Precedence) describeModes() string {
	where := "in front of the name"
	if p.Modes == ModePlaceWrapped {
		where += " inside the block that holds it"
	}

	sentence := "A mod changes an entry by writing REPLACE: " + where + " to take its place"

	switch p.Injection {
	case InjectionReplaces:
		return sentence + "; INJECT: does the same here."
	case InjectionUnsettled:
		return sentence + "; the game does not say what INJECT: does here."
	}

	return sentence + ", or INJECT: to add to it."
}

// SplitMode splits a mode prefix off a written key, so REPLACE:x gives the
// name x with the mode REPLACE. A game whose definitions take no modes splits
// none.
func (g *Game) SplitMode(key string) (mode, name string) {
	prefix, rest, cut := strings.Cut(key, ":")
	if _, ok := LookupEntryMode(prefix); cut && g.TakesModes && ok {
		return prefix, rest
	}

	return "", key
}

// EntryModes are the modes a definition's name may start with, in a game
// whose definitions take modes. The modes act in this order (the Vic3 and EU5
// wikis' Mod compatibility, Processing order).
var EntryModes = []EntryMode{
	{Prefix: "INJECT_OR_CREATE", Creates: true}, {Prefix: "REPLACE_OR_CREATE", Replaces: true, Creates: true},
	{Prefix: "TRY_INJECT", Optional: true}, {Prefix: "TRY_REPLACE", Replaces: true, Optional: true},
	{Prefix: "INJECT"}, {Prefix: "REPLACE", Replaces: true},
}

// LookupEntryMode returns the entry mode a prefix names. It returns false for
// "" and for a prefix no game uses.
func LookupEntryMode(prefix string) (EntryMode, bool) {
	for _, mode := range EntryModes {
		if mode.Prefix == prefix {
			return mode, true
		}
	}

	return EntryMode{}, false
}

// Injects reports whether the mode adds to the definition already loaded
// rather than taking its place, as INJECT, TRY_INJECT and INJECT_OR_CREATE
// do.
func (e EntryMode) Injects() bool { return !e.Replaces }

// definitionNouns are the singular and plural nouns for a definition of some
// databases, by database.
var definitionNouns = map[string]struct{ One, Many string }{
	"messages":      {"player message", "player messages"},
	SavedScopes:     {"saved scope", "saved scopes"},
	Variables:       {"variable", "variables"},
	LocalVariables:  {"local variable", "local variables"},
	GlobalVariables: {"global variable", "global variables"},
	Lists:           {"list", "lists"},
	Flags:           {"flag", "flags"},
}

// plurals are the plural endings of an English noun, each with its singular
// ending. The first ending that fits wins.
var plurals = []struct{ Plural, One string }{
	// the same in the singular and the plural
	{"arms", "arms"}, {"goods", "goods"}, {"belli", "belli"}, {"criteria", "criteria"}, {"data", "data"},
	// a y after a vowel only takes an s
	{"ays", "ay"}, {"eys", "ey"}, {"oys", "oy"}, {"uys", "uy"},
	{"ouses", "ouse"}, {"eases", "ease"}, {"ies", "y"}, {"ses", "s"}, {"xes", "x"}, {"ches", "ch"}, {"shes", "sh"},
	{"s", ""},
}

// DescribeDefinition returns what a definition of a database is called, such
// as "scripted effect", in the plural if asked. It returns "" for a database
// with no name.
func DescribeDefinition(database string, plural bool) string {
	if nouns, ok := definitionNouns[database]; ok {
		if plural {
			return nouns.Many
		}

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

	// A name already in the singular takes the plural of its ending.
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
