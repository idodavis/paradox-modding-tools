// declarations.go holds what is known about each database before any file is
// read: where its definitions are written, how they are named and where they
// are called from.

package lang

import (
	"path"
	"slices"
	"strings"

	"paradox-modding-tools/internal/game"
)

// Declaration is what the games' documents and files say about one
// database. A definition is named by the key of its block at the top of a
// file, as brave in brave = { … }, unless the declaration says otherwise.
type Declaration struct {
	// Folder is the folder or file the definitions are written in. Name is
	// the database's name where it has no folder, as gui.type.
	Folder string
	Name   string

	// Games are the games the row holds for, all if none.
	Games []string

	Type Type

	// NameKey is the key inside a definition whose value is the
	// definition's name, as id = x. It is "" where the key of the block is
	// the name.
	NameKey string

	DefinedBy DefinedBy

	// RootKey is the key inside a definition whose value is the type of
	// scope its script starts in. It is "" for a database with no such key.
	RootKey string

	// WrapperKeys are the keys at the top of a file whose blocks only hold
	// definitions, and are no definition themselves.
	WrapperKeys []string

	// DeclaringKeys are the keys that declare a definition of this database
	// inside a file of another, as scripted_effect x = { } does.
	DeclaringKeys []string

	// Aliases are the keys inside a definition whose value is a second name
	// for it, as group = x. Each has the database the second name is in,
	// "" for this one.
	Aliases map[string]string

	Nestings []Nesting

	// RunOrder lists the blocks of a definition in the order the game runs
	// them. Blocks that are not listed run after them.
	RunOrder []string

	// Shape is what the block of a definition holds. It is "" for a block of
	// the definition's own fields.
	Shape Shape

	// Target is the database whose definitions these set up or are named
	// after, as a title's history is named after its title. It is "" for
	// none.
	Target string

	// Mirrors is whether the definitions are named after those of several
	// other databases. FoldsCase is whether names match in any letter case.
	Mirrors   bool
	FoldsCase bool

	// Prefixes are the links that take a name of this database after a
	// colon, as dynasty does in dynasty:<key>.
	Prefixes []string

	// Separator is what joins a definition's name to the key of a nested
	// definition, as | does in NAI|X. It is "" for a database with none.
	Separator string

	// AllowsUndefined is whether a name written for this database need not
	// be defined in it. The game then uses localization keys built on the
	// name, such as <effect>_first.
	AllowsUndefined bool

	// Callers are the kinds of place a definition is called from by its
	// name: a trigger block, an effect block, a value block or a GUI key.
	// They are none for a database whose definitions are not called.
	Callers []string

	// Parameters is whether a call may pass parameters. Local is whether a
	// definition can be called only in the file that declares it.
	Parameters bool
	Local      bool

	// Values are the values a call may write where it passes no parameters,
	// as yes in my_trigger = yes. They are none where a call writes none.
	Values []string

	// Precedences say which copy of a name the game keeps. The first row
	// that holds for the game is used. With none, the game's rule for the
	// folder holds.
	Precedences []Precedence

	// References are the keys inside a definition whose values are names of
	// definitions or of files. Fields are the keys of a definition that no
	// dump describes.
	References []KeyReference
	Fields     []DatabaseField
}

// DefinedBy is what makes a definition of a database, where the key of a
// block at the top of a file does not. It is "" where that key does.
type DefinedBy string

const (
	// DefinedByFile says each file is one definition.
	DefinedByFile DefinedBy = "file"

	// DefinedByColumn says each row of a table is a definition, named by
	// the row's first column.
	DefinedByColumn DefinedBy = "column"

	// DefinedByNothing says the database's files define nothing.
	DefinedByNothing DefinedBy = "nothing"
)

// Type is what the definitions of a database are, where the app treats them
// in a way of its own. It is "" for any other database.
type Type string

// The types the app reads in a way of its own.
const (
	TypeEvent    Type = "event"
	TypeOnAction Type = "on_action"
	TypeDecision Type = "decision"
	TypeMessage  Type = "message"
	TypeList     Type = "list"
	TypeConcept  Type = "concept"
	TypeModifier Type = "modifier"
)

// The types Atlas draws.
const (
	// TypeGood is a good, as wood is. TypeGoodCategory is a group of goods,
	// as CK3's trade_category_mandala_arms is.
	TypeGood         Type = "good"
	TypeGoodCategory Type = "good_category"

	// TypeBuilding is a building, a level of one in CK3. TypeBuildingGroup is
	// what holds buildings: a district, a holding, a category.
	TypeBuilding      Type = "building"
	TypeBuildingGroup Type = "building_group"

	// TypeProductionMethodGroup is a slot of production methods, and
	// TypeProductionMethod a way a building produces.
	TypeProductionMethodGroup Type = "production_method_group"
	TypeProductionMethod      Type = "production_method"

	// TypeTechnology is a technology, an innovation or an advance, and TypeEra
	// the era or age it belongs to.
	TypeTechnology Type = "technology"
	TypeEra        Type = "era"

	// TypeDemand is a demand for goods that is no building, as a pop need or
	// an army's upkeep.
	TypeDemand Type = "demand"
)

// Nesting is a database whose definitions are written as keys inside the
// definitions of another database, its parent. Its name is the parent's
// name, a dot and its own.
type Nesting struct {
	// Games are the games the row holds for, all if none.
	Games []string

	// Parent is the folder or file of the parent database. A nesting listed
	// in a declaration leaves it empty, and ListNestings fills it in.
	Parent string

	// Type is what the nested definitions are, as a declaration's Type is. It
	// is "" for any other nesting.
	Type Type

	// Sets is whether the keys define nothing: they only switch a name on,
	// or give a definition a second name.
	Sets bool

	// KeyPath is the way from a definition of the parent to the block the
	// keys are written in, as keys joined by slashes. * stands for any key
	// and ** for any number of keys. It is "" for the definition's own
	// block.
	KeyPath string

	// Database is the nesting's own name, which follows the parent's name
	// and a dot. It is "" where the definitions belong to the parent's
	// database.
	Database string

	// Keys is the pattern a key must fit to be a definition. Any other key
	// is a field. It is "" where every key is a definition.
	Keys string

	// NameKey is the key inside a definition whose value is the
	// definition's name. It is "" where the key of its block is the name.
	NameKey string

	// Target is another database, of which each definition here sets one
	// up. It is "" for a nesting with no such tie.
	Target string

	// Precedence says which copy of a name the game keeps. The zero value
	// leaves it to the game's rule for the folder.
	Precedence Precedence

	// Fields are the keys of a nested definition that no dump describes.
	Fields []DatabaseField
}

// FileType is what the extension of a file says about the file, beside what
// the database of its folder says.
type FileType struct {
	// NameKey is the key inside a block at the top of the file whose value
	// is the definition's name. It is "" where the key of the block is the
	// name.
	NameKey string

	// ColorSuffix is how a key ends when a block of plain numbers written
	// for it is a color. It is "" where no key is read so.
	ColorSuffix string

	// Shape is what a block at the top of the file holds. It is "" where
	// the database says.
	Shape Shape
}

// KeyReference is one key inside the definitions of a database whose values
// are names of definitions.
type KeyReference struct {
	// Games are the games the row holds for, all if none.
	Games []string

	// KeyPath is the way from the definition to the key, as keys joined by
	// slashes. <number> stands for a key that is a number. For the widgets
	// of a GUI file it is one key, wherever in a widget it is written.
	KeyPath string

	// Target is the database the values are names of. It is "" for the
	// database the row is in.
	Target string

	// Also is a second database the values may name, as an EU5 building's
	// possible methods may name another building's unique method. It is ""
	// for none.
	Also string

	// Folder is the folder of a file that a value names with no folder of
	// its own. It is "" where the values are no such files.
	Folder string

	// Literal is whether the game shows a value as written where nothing
	// defines its name, as a GUI text = "Hello" shows Hello.
	Literal bool
}

// DatabaseField is one key inside the definitions of a database, with what
// no dump says about it.
type DatabaseField struct {
	// Games are the games the row holds for, all if none.
	Games []string

	// KeyPath is the way from the definition to the key, as keys joined by
	// slashes. <number> stands for a key that is a number. It is "" for the
	// definition's own block.
	KeyPath string

	// Keys is the database whose definitions the keys of the field's block
	// name, as the goods an EU5 production method takes. It is "" where its
	// keys name nothing.
	Keys string

	// Shape is what the key's block holds. It is "" where the row does not
	// say.
	Shape Shape

	// Weighted is whether the key is a number that is a chance: its share
	// of the numbers written beside it.
	Weighted bool

	// Blurb is the key's hover text.
	Blurb string
}

// PictureStep is one step of the way from an event to its picture: a key
// whose value names a definition, in which the next step is looked up.
type PictureStep struct {
	// Key is the way to the key, as keys joined by slashes, starting in the
	// event or in the definition the step before found. key[field=value]
	// takes the block of that key in which the given field has the given
	// value.
	Key string

	// Database is the database of the definition the key's value names.
	Database string

	// Match is the key inside each definition of that database whose values
	// are compared with the value. It is "" where the value is the
	// definition's name.
	Match string
}

// PictureChain is one way from an event to a picture file, step by step.
type PictureChain struct {
	Steps []PictureStep

	// FileKey is the key, in the definition the last step finds, whose
	// value is the picture file.
	FileKey string

	// Fallback is the key that names another definition to take the file
	// from where the file is missing. It is "" for a chain with none.
	Fallback string
}

// Picture is how the picture of an event is found.
type Picture struct {
	// FileKey is the key of an event whose value is the picture file
	// itself. It is "" for a game whose events have no such key.
	FileKey string

	Chains []PictureChain
}

// GameEvents is what differs between the games about events: where an event
// says which scope it starts in, and how its picture is found.
type GameEvents struct {
	// TypeKey is the key of an event whose value says the type of scope the
	// event starts in. The value ends in TypeSuffix, which is not part of
	// the type: Vic3's type = country_event starts in a country. TypeKey is
	// "" for a game whose events have no such key.
	TypeKey    string
	TypeSuffix string

	// DefaultScope is the type of scope an event starts in where it does
	// not say.
	DefaultScope string

	Picture Picture
}

// TextKeys says, for some databases, which localization keys hold the title
// and the description of a definition that names none itself. %s stands for
// the definition's name.
type TextKeys struct {
	// Games are the games the row holds for, all if none.
	Games []string

	// Title is the localization key of a definition's title. It is "" where
	// the title's key is the definition's name. Description is the key of
	// its description. It is "" where a definition has none.
	Title       string
	Description string

	// Capitals is whether the keys write the name in capitals, as CK3's
	// MOD_STEWARDSHIP does for the modifier stewardship.
	Capitals bool

	Databases []string
}

// PictureRow is how the definitions of one database show a picture: the file
// a definition names, or the file named after it. Of a database's rows the
// first that leads to a file shows.
type PictureRow struct {
	// Games are the games the row holds for, all if none.
	Games []string

	Database string

	// Painting is set for the painting behind a definition's head, where
	// any other row is its picture.
	Painting bool

	// Key is the way from the definition to the key that writes the file,
	// as keys joined by slashes. A constant, as @icon, stands for its text,
	// and a value that does not start with gfx/ is a path under Folder. Of
	// several blocks of one key, the first with no trigger is read. Key is
	// "" for a file named after the definition.
	Key    string
	Folder string

	// Pattern is the path of a file named after the definition, with %s for
	// its name, or for the value of the key Field where that is set.
	Pattern string
	Field   string

	// Silhouette is set where the game tints the picture in the color of
	// the text beside it.
	Silhouette bool
}

// ModifierFormats is CK3's database of how each modifier's value is written.
// Vic3 and EU5 write the same keys in the database Schema.ModifierTypes names.
const ModifierFormats = "modifier_definition_formats"

// ModifierPrefix is the key of a CK3 modifier's format that names the text
// shown before its value, and ModifierPrefixEnding how that text's key ends.
// Without the ending the key is the modifier's name: fort_level writes
// prefix = MOD_HOLDING_FORT_LEVEL_PREFIX
// (common/modifier_definition_formats/00_definitions.txt:2642-2645), and
// MOD_HOLDING_FORT_LEVEL names it
// (localization/english/modifiers/modifiers_l_english.yml:974).
const (
	ModifierPrefix       = "prefix"
	ModifierPrefixEnding = "_PREFIX"
)

// Localization is the database of localization keys, and File the database
// of file paths. Neither has definitions written in script.
const (
	Localization = "localization"
	File         = "file"
)

// CustomLocalization is the database of the customizable localizations.
const CustomLocalization = "customizable_localization"

// Constant is the database of the constants a file writes for itself as
// @name = value. A constant holds only in its own file.
const Constant = "constant"

// EventNamespaceKey is the key an events file declares a namespace with. A
// file may declare several. Its events are named <namespace>.N.
const EventNamespaceKey = "namespace"

// TitleKey is the key whose value is the localization key of a definition's
// title, as in an event's title = <key>.
const TitleKey = "title"

// DescriptionKey is the key that gives an event's description: a
// localization key, or a block that chooses one.
const DescriptionKey = "desc"

// LabelKey is the key whose value is a text's localization key, inside a
// block that chooses the text, as in an option's
// name = { trigger = { … } text = <key> }.
const LabelKey = "text"

// SetupFolder is the folder the rows for a start setup are written for. The
// folder a bookmark names for its start setup is read as if its files were
// in this one.
const SetupFolder = "setup/start"

// The kinds of place a scripted definition is called from: a trigger block,
// an effect block and a value block.
const (
	CallTrigger = "trigger"
	CallEffect  = "effect"
	CallValue   = "value"
)

// GameRuleSettings is the database of the settings of the game rules.
// GameRuleTrigger is the trigger that asks whether a rule is set to the
// setting it names. The three games write them alike: CK3
// _game_rules.info:1-20, Vic3 game_rules.md:1-12 and EU5
// main_menu/common/game_rules/_game_rules.info:1-19.
const (
	GameRuleSettings = "game_rules.setting"
	GameRuleTrigger  = "has_game_rule"
)

// The blocks of an on_action and of an event in the order they run, as the
// games' own documents write them.
var (
	onActionOrder = []string{"trigger", "effect", "events"}
	ck3EventOrder = []string{"trigger", "immediate", "title", "desc", "option", "after"}
	eventOrder    = []string{"trigger", "immediate", "option", "after"}
)

// The hover texts of the events and on_actions an on_action picks one of by
// chance.
const (
	randomEventBlurb    = "One event the on_action can pick; its number is how likely it is. An event of 0 is a chance that none fires."
	randomOnActionBlurb = "One on_action the on_action can pick; its number is how likely it is."
)

// portraitAnimations is the database of the animations a portrait plays.
const portraitAnimations = "gfx/portraits/portrait_animations"

// traitIcons is the folder of CK3's trait icons.
const traitIcons = "gfx/interface/icons/traits"

// SetupCountries is the database of the countries EU5's start setup sets up,
// whose names are the countries' tags.
const SetupCountries = "setup/countries"

// namedLocations is the database of the locations EU5's start setup names.
const namedLocations = "map_data/named_locations"

// onActionPrecedence says what the game keeps of several copies of one
// on_action: it joins their lists, and their trigger or effect blocks
// conflict.
var onActionPrecedence = Precedence{Keeps: KeepMerged, Contested: []string{"trigger", "effect"}}

// guiTypePrecedences say which copy of a GUI type or template each game
// keeps.
var guiTypePrecedences = []Precedence{
	// the CK3 wiki's Modding page: types in UI files use FIOS; Vic3 GUI
	// types: the one loaded first wins
	{Games: ck3AndVic3, Keeps: KeepFirst},
	// EU5 l.155: the last by playlist position
	{Games: []string{game.EU5}, Keeps: KeepLast, ByPlayset: true},
}

// scriptedPrecedences say which copy of a scripted effect or trigger Vic3
// and EU5 keep. There INJECT acts as REPLACE does (the Vic3 wiki's
// Behavior, and the EU5 wiki, l.92).
var scriptedPrecedences = []Precedence{
	{Games: vic3AndEU5, Keeps: KeepFirst, Modes: ModePlaceEntry, Injection: InjectionReplaces},
}

// unsettledPrecedences say which copy of a scripted rule or a script value
// Vic3 keeps. The Vic3 wiki's list of folders gives them REPLACE only. It
// does not say whether INJECT is refused or acts as REPLACE does.
var unsettledPrecedences = []Precedence{
	{Games: []string{game.Vic3}, Keeps: KeepFirst, Modes: ModePlaceEntry, Injection: InjectionUnsettled},
}

// customTextReferences says that each text of a customizable localization
// names its localization key: CK3
// common/customizable_localization/_custom_loc.info:18-38, EU5
// in_game/common/customizable_localization/customizable_localization.info
// :1-10, and Vic3's own files there.
var customTextReferences = []KeyReference{{KeyPath: "text/localization_key", Target: Localization}}

// listOnActionReferences returns the keys of an on_action whose values name
// on_actions and events. randomKey is the key of the on_actions it picks one
// of by chance, which the games name differently (CK3
// _on_actions.info:47-80, Vic3 _on_actions.md:58-90).
func listOnActionReferences(randomKey string) []KeyReference {
	return []KeyReference{
		{KeyPath: "on_actions"},
		{KeyPath: randomKey + "/<number>"},
		{KeyPath: "first_valid_on_action"},
		{KeyPath: "fallback"},
		{KeyPath: "events", Target: "events"},
		{KeyPath: "random_events/<number>", Target: "events"},
		{KeyPath: "first_valid", Target: "events"},
	}
}

// listOnActionFields returns the two lists of an on_action whose keys are
// chances: one event, or one on_action, is picked to fire. randomKey is the
// key of the list of on_actions (_on_actions.info:55-90).
func listOnActionFields(randomKey string) []DatabaseField {
	return []DatabaseField{
		{KeyPath: "random_events/<number>", Weighted: true, Blurb: randomEventBlurb},
		{KeyPath: randomKey + "/<number>", Weighted: true, Blurb: randomOnActionBlurb},
	}
}

// goodsKeys says that the keys of a definition's own block name goods.
var goodsKeys = []DatabaseField{{Keys: "goods"}}

// Declarations holds the declaration of each database that needs one. The
// rows of one game come before the rows of every game. A row for a folder a
// game does not have does nothing. A game's rows cite its .info files
// unless they say otherwise.
var Declarations = []Declaration{
	// CK3 and EU5 _on_action.info
	{
		Folder: "common/on_action", Games: ck3AndEU5, Type: TypeOnAction, RunOrder: onActionOrder,
		Precedences: []Precedence{onActionPrecedence},
		References:  listOnActionReferences("random_on_action"), Fields: listOnActionFields("random_on_action"),
	},
	// scope = <type> sets an event's root scope (CK3 _events.info:12); its
	// copies: _events.info:77-83
	{
		Folder: "events", Games: []string{game.CK3}, Type: TypeEvent, RunOrder: ck3EventOrder, RootKey: "scope",
		Precedences: []Precedence{{Keeps: KeepHighest, PriorityKey: "id_override_priority"}},
		// a portrait's animation names a portrait animation (CK3
		// _events.info:101,107)
		References: []KeyReference{
			{KeyPath: "*_portrait/animation", Target: portraitAnimations},
			{KeyPath: "*_portrait/triggered_animation/animation", Target: portraitAnimations},
			{KeyPath: "sender/animation", Target: portraitAnimations},
			{KeyPath: "sender/triggered_animation/animation", Target: portraitAnimations},
		},
	},
	// CK3 _messages.info:1; Vic3's post_notification sends one
	// (effects.log:27072)
	{Folder: "common/messages", Games: ck3AndVic3, Type: TypeMessage},
	// _decisions.info:194,200,208
	{
		Folder: "common/decisions", Games: []string{game.CK3}, Type: TypeDecision,
		RunOrder: []string{"is_shown", "is_valid", "cost", "cooldown", "effect"},
	},
	// a group is also a name, which has_trait holds for any trait in it
	// (_traits.info:3,181,183); CK3's trait icons (_traits.info:4,23-35)
	{
		Folder: "common/traits", Games: []string{game.CK3}, Aliases: map[string]string{"group": "", "group_equivalence": ""},
		References: []KeyReference{
			{KeyPath: "icon", Target: File, Folder: traitIcons}, {KeyPath: "icon/**/desc", Target: File, Folder: traitIcons},
		},
	},
	// the DLC subfolder writes the same blocks
	{Folder: "common/opinion_modifiers", Games: []string{game.CK3}},
	// its subfolders are one database (_districts.info:44,54); a building's
	// trade good need names a category of goods (buildings/
	// 00_duchy_capital_buildings.txt:1804)
	{
		Folder: "common/buildings", Games: []string{game.CK3}, Type: TypeBuilding,
		References: []KeyReference{{KeyPath: "trade_good_need/trade_good_category", Target: "trade/trade_good_categories"}},
	},
	// the types of CK3's goods, buildings and technologies that Atlas draws
	{Folder: "common/domiciles/buildings", Games: []string{game.CK3}, Type: TypeBuilding},
	{Folder: "common/districts", Games: []string{game.CK3}, Type: TypeBuildingGroup},
	{Folder: "common/holdings", Games: []string{game.CK3}, Type: TypeBuildingGroup},
	{Folder: "common/trade/trade_good_types", Games: []string{game.CK3}, Type: TypeGood},
	{Folder: "common/trade/trade_good_categories", Games: []string{game.CK3}, Type: TypeGoodCategory},
	{Folder: "common/culture/innovations", Games: []string{game.CK3}, Type: TypeTechnology},
	{Folder: "common/culture/eras", Games: []string{game.CK3}, Type: TypeEra},
	// an outbreak's notification fires events (CK3 _epidemics.info:124-128)
	{
		Folder: "common/epidemics", Games: []string{game.CK3},
		References: []KeyReference{
			{KeyPath: "outbreak_intensities/*/notification/holder_event", Target: "events"},
			{KeyPath: "outbreak_intensities/*/notification/liege_event", Target: "events"},
			{KeyPath: "outbreak_intensities/*/notification/realm_event", Target: "events"},
		},
	},
	// a title inside a title is a title; its key opens with its tier
	{Folder: "common/landed_titles", Games: []string{game.CK3}, Nestings: []Nesting{{Keys: "[ekdcb]_*"}}},
	// an activity's categories are unique only within it
	// (_activity_type.info:138-141)
	{
		Folder: "common/activities/activity_types", Games: []string{game.CK3},
		Nestings: []Nesting{
			{KeyPath: "options", Database: "option_category", Precedence: Precedence{Keeps: KeepMerged}},
			{KeyPath: "phases", Database: "phase"},
		},
	},
	{Folder: "gfx/map/map_object_data", Games: ck3AndVic3, DefinedBy: DefinedByNothing},
	// a date entry's liege names the title above it (CK3
	// history/titles/00_other_titles.txt:2525)
	{
		Folder: "history/titles", Games: []string{game.CK3}, Target: "landed_titles",
		References: []KeyReference{{KeyPath: "<number>/liege", Target: "landed_titles"}},
	},
	{Folder: "history/provinces", Games: []string{game.CK3}, Target: "map_data/definition"},
	{Folder: "history/province_mapping", Games: []string{game.CK3}, Target: "map_data/definition"},
	{Folder: "history/cultures", Games: []string{game.CK3}, Target: "culture/cultures", DefinedBy: DefinedByFile},
	{Folder: "history/faiths", Games: []string{game.CK3}, Target: "religion/faith_types"},
	{Folder: "history/artifacts", Games: []string{game.CK3}},
	{Folder: "history/situations", Games: []string{game.CK3}},
	{Folder: "history/struggles", Games: []string{game.CK3}},
	{Folder: "history/wars", Games: []string{game.CK3}, DefinedBy: DefinedByNothing},
	// a date entry changes the character's own keys and reads any other key
	// as an effect (_characters.info:65-66,78); its copies:
	// _characters.info:25-31
	{
		Folder: "history/characters", Games: []string{game.CK3}, Shape: ShapeSetup,
		Precedences: []Precedence{{
			Keeps: KeepHighest, PriorityKey: "history_override_priority",
			Overrides: []string{
				"name", "dna", "female", "martial", "prowess", "diplomacy", "intrigue", "stewardship", "learning", "trait",
				"father", "mother", "disallow_random_traits", "obscured", "birth",
			},
		}},
		Fields: []DatabaseField{{KeyPath: "<number>", Shape: ShapeSetup, Blurb: "What changes for the character on this date."}},
	},
	// _reader_export.info: what the reader loads before all other files
	{Folder: "reader_export", Games: []string{game.CK3}},
	{Folder: "map_data/definition.csv", Games: []string{game.CK3}, DefinedBy: DefinedByColumn},
	// no keyed database (_province_terrain.info:3)
	{Folder: "common/province_terrain", Games: []string{game.CK3}, DefinedBy: DefinedByNothing},
	// each is named after a landed title (_dynamic_definitions.info:1)
	{Folder: "common/coat_of_arms/dynamic_definitions", Games: []string{game.CK3}, Target: "landed_titles"},
	// scene_settings_byzantine_1_g1.txt:1-6 writes one setting's fields
	{Folder: "gfx/court_scene/scene_settings", Games: []string{game.CK3}, DefinedBy: DefinedByFile},
	// court_scene_environment.txt:1-10 writes one environment's fields
	{Folder: "gfx/court_scene/scene_environment", Games: []string{game.CK3}, DefinedBy: DefinedByFile},
	// gfx/map/environment/environment.info:1
	{Folder: "gfx/map/environment", Games: []string{game.CK3}, DefinedBy: DefinedByFile},
	// _dynasties.info:3
	{Folder: "common/dynasties", Games: []string{game.CK3}, Prefixes: []string{"dynasty"}},
	// _situations.info:206
	{Folder: "common/situation/situations", Games: []string{game.CK3}, Prefixes: []string{"situation"}},
	// Vic3 _on_actions.md
	{
		Folder: "common/on_actions", Games: []string{game.Vic3}, Type: TypeOnAction, RunOrder: onActionOrder,
		Precedences: []Precedence{onActionPrecedence},
		References:  listOnActionReferences("random_on_actions"), Fields: listOnActionFields("random_on_actions"),
	},
	// the Vic3 wiki and EU5 _events.info. Vic3 Events and EU5 l.169: the
	// first copy loaded wins.
	{
		Folder: "events", Games: vic3AndEU5, Type: TypeEvent, RunOrder: eventOrder,
		Precedences: []Precedence{{Keeps: KeepFirst}},
		References: []KeyReference{
			{KeyPath: "title", Target: Localization},
			{KeyPath: "desc", Target: Localization},
			{Games: []string{game.Vic3}, KeyPath: "flavor", Target: Localization},
			{Games: []string{game.EU5}, KeyPath: "historical_info", Target: Localization},
			{KeyPath: "option/name", Target: Localization},
		},
	},
	{Folder: "common/decisions", Games: []string{game.Vic3}, Type: TypeDecision},
	// the types of Vic3's goods, buildings and technologies that Atlas draws
	{Folder: "common/goods", Games: []string{game.Vic3}, Type: TypeGood},
	{Folder: "common/buildings", Games: []string{game.Vic3}, Type: TypeBuilding},
	{Folder: "common/building_groups", Games: []string{game.Vic3}, Type: TypeBuildingGroup},
	{Folder: "common/production_method_groups", Games: []string{game.Vic3}, Type: TypeProductionMethodGroup},
	// a method lists the goods its building must reach
	// (production_methods.md:29)
	{
		Folder: "common/production_methods", Games: []string{game.Vic3}, Type: TypeProductionMethod,
		References: []KeyReference{{KeyPath: "required_input_goods", Target: "goods"}},
	},
	{Folder: "common/technology/technologies", Games: []string{game.Vic3}, Type: TypeTechnology},
	{Folder: "common/technology/eras", Games: []string{game.Vic3}, Type: TypeEra},
	{
		Folder: "gfx/map/post_effects", Games: []string{game.Vic3}, NameKey: "name", WrapperKeys: []string{"posteffect_volumes"},
	},
	// COUNTRIES = { … }: effects
	{Folder: "common/history", Games: []string{game.Vic3}, DefinedBy: DefinedByNothing, Shape: ShapeEffects},
	// every file is a test suite (scripted_tests.md:2)
	{Folder: "tools/scripted_tests", Games: []string{game.Vic3}, DefinedBy: DefinedByFile},
	// Vic3 modifier_types.md:1-3,16-17 and EU5 00_modifier_types.txt:1,3
	{Folder: "common/modifier_type_definitions", Games: vic3AndEU5, Type: TypeModifier},
	// map_data/province_terrains.txt:21698 defines x7F25CD, and
	// common/history/states/00_states.txt:137 writes x7f25cd
	{Folder: "map_data/province_terrains.txt", Games: []string{game.Vic3}, FoldsCase: true},
	// modifier_types.md:2
	{Folder: "common/static_modifiers", Games: []string{game.Vic3}, Shape: ShapeModifiers},
	// common/geographic_regions/geographic_regions.md:4 names its lists by the
	// short_key: every_country_in_<short_key>
	{Folder: "common/geographic_regions", Games: []string{game.Vic3}, Aliases: map[string]string{"short_key": ""}},
	// a rule is a block of triggers, as CK3 00_rules.txt:4-6 and Vic3
	// 00_scripted_rules.txt:3-5 write it
	{Folder: "common/scripted_rules", Games: []string{game.CK3}, Shape: ShapeTriggers},
	{Folder: "common/scripted_rules", Games: []string{game.Vic3}, Shape: ShapeTriggers, Precedences: unsettledPrecedences},
	// Vic3 DNA data: defined character DNAs do not overwrite each other; the
	// wiki's list of folders gives it entry modes
	{
		Folder: "common/dna_data", Games: []string{game.Vic3},
		Precedences: []Precedence{{Keeps: KeepMerged, Modes: ModePlaceEntry}},
	},
	// Vic3 common/flag_definitions/00_flag_definitions.txt:5,7,10 and EU5
	// main_menu/common/flag_definitions/00_flag_definitions.txt:5,7,10
	{
		Folder: "common/flag_definitions", Games: vic3AndEU5,
		References: []KeyReference{
			{KeyPath: "flag_definition/coa", Target: CoatDatabase},
			{KeyPath: "flag_definition/coa_with_overlord_canton", Target: CoatDatabase},
			{KeyPath: "flag_definition/subject_canton", Target: CoatDatabase},
		},
	},
	// a formable's tag names the country it forms
	// (00_formable_countries.txt:18)
	{Folder: "common/formable_countries", Games: []string{game.EU5}, Aliases: map[string]string{"tag": SetupCountries}},
	{Folder: "gfx/map/map_objects", Games: []string{game.EU5}, DefinedBy: DefinedByNothing},
	{Folder: "gfx/map/city_data", Games: []string{game.EU5}, NameKey: "name"},
	// EU5 in_game/gfx/map/map_modes/readme.txt:6: a map mode's tooltip_key is
	// a script string, whose values are keys
	{
		Folder: "gfx/map/map_modes", Games: []string{game.EU5},
		References: []KeyReference{{KeyPath: "tooltip_key/**/value", Target: Localization}},
	},
	// a reference to a concept matches in either case;
	// common/game_concepts/00_game_concepts.txt:2 gives vassal the second
	// names of an alias
	{
		Folder: "common/game_concepts", Games: []string{game.EU5}, Type: TypeConcept, FoldsCase: true,
		Nestings: []Nesting{{KeyPath: "alias", Sets: true}},
	},
	// its top keys are the managers that hold the nestings, and each sets up
	// the database its Target names
	{
		Folder: SetupFolder, Games: []string{game.EU5}, DefinedBy: DefinedByNothing,
		Nestings: []Nesting{
			{KeyPath: "character_db", Database: "character"},
			{KeyPath: "dynasty_manager", Database: "dynasty"},
			{KeyPath: "countries/countries", Database: "country", Target: SetupCountries},
			{KeyPath: "locations", Database: "location", Target: namedLocations},
			{KeyPath: "road_network", Database: "road", Target: namedLocations},
			{KeyPath: "townrights_manager", Database: "town_rights", Target: namedLocations},
			{KeyPath: "building_manager", Database: "building", Target: "building_types"},
			{KeyPath: "work_of_art_manager", Database: "work_of_art", Target: "artist_work"},
			{KeyPath: "colony_manager", Database: "colony", Target: "map_data/definitions.province"},
			{KeyPath: "institution_manager/institutions", Database: "institution", Target: "institution"},
			{KeyPath: "situation_manager", Database: "situation", Target: "situations"},
		},
	},
	// its subfolders write the same attachment lists
	{Folder: "gfx/unit_graphics/attachments", Games: []string{game.EU5}},
	{Folder: "setup/templates", Games: []string{game.EU5}, DefinedBy: DefinedByFile},
	{Folder: "map_data/location_templates.txt", Games: []string{game.EU5}, Target: namedLocations},
	{Folder: "map_data/generated_locators_port.txt", Games: []string{game.EU5}, DefinedBy: DefinedByNothing},
	// map_data/definitions.txt:1-5, europe > western_europe >
	// scandinavian_region > svealand_area > uppland_province
	{
		Folder: "map_data/definitions.txt", Games: []string{game.EU5},
		Nestings: []Nesting{
			{Database: "continent"},
			{KeyPath: "*", Database: "sub_continent"},
			{KeyPath: "*/*", Database: "region"},
			{KeyPath: "*/*/*", Database: "area"},
			{KeyPath: "*/*/*/*", Database: "province"},
		},
	},
	{Folder: "gfx/defines", Games: []string{game.EU5}, Nestings: []Nesting{{Database: "define"}}},
	{Folder: "common/laws", Games: []string{game.EU5}, Nestings: []Nesting{{Database: "policy"}}},
	{Folder: "common/languages", Games: []string{game.EU5}, Nestings: []Nesting{{KeyPath: "dialects", Database: "dialect"}}},
	{Folder: "common/gods", Games: []string{game.EU5}, Nestings: []Nesting{{KeyPath: "omens", Database: "omen"}}},
	// a production method writes <goods_input> = <amount> (in_game/common/
	// production_methods/__readme.txt:2)
	{Folder: "common/production_methods", Games: []string{game.EU5}, Type: TypeProductionMethod, Fields: goodsKeys},
	// a building's unique methods are written as production methods are; its
	// possible methods name production methods, or another building's unique
	// method (building_types/00_unique_buildings_to_make_obsolete.txt:66)
	{
		Folder: "common/building_types", Games: []string{game.EU5}, Type: TypeBuilding,
		Nestings: []Nesting{{
			KeyPath: "unique_production_methods", Database: "unique_production_method", Type: TypeProductionMethod,
			Fields: goodsKeys,
		}},
		References: []KeyReference{{
			KeyPath: "possible_production_methods", Target: "production_methods",
			Also: "building_types.unique_production_method",
		}},
	},
	// a demand writes a good and its amount, as wine = 1 (in_game/common/
	// goods_demand/pop_demands.txt:15)
	{Folder: "common/goods_demand", Games: []string{game.EU5}, Type: TypeDemand, Fields: goodsKeys},
	// the types of EU5's goods, buildings and technologies that Atlas draws
	{Folder: "common/goods", Games: []string{game.EU5}, Type: TypeGood},
	{Folder: "common/building_categories", Games: []string{game.EU5}, Type: TypeBuildingGroup},
	{Folder: "common/advances", Games: []string{game.EU5}, Type: TypeTechnology},
	{Folder: "common/age", Games: []string{game.EU5}, Type: TypeEra},
	// a need's entries name goods, and so does its default
	// (common/pop_needs/00_pop_needs.txt:1,7)
	{
		Folder: "common/pop_needs", Games: []string{game.Vic3}, Type: TypeDemand,
		References: []KeyReference{{KeyPath: "entry/goods", Target: "goods"}, {KeyPath: "default", Target: "goods"}},
	},
	// EU5 in_game/common/subject_types/readme.txt:80 writes it as goal keys,
	// each holding a type and a bias
	{
		Folder: "common/subject_types", Games: []string{game.EU5},
		Fields: []DatabaseField{{
			KeyPath: "ai_biases", Shape: ShapeFields,
			Blurb: "The weight the AI adds to certain goals, one block for each goal with its type and its bias.",
		}},
	},
	// main_menu/common/modifier_icons: named after modifier types
	{Folder: "common/modifier_icons", Games: []string{game.EU5}, Mirrors: true},
	// The type of a customizable localization is the scope its command is
	// used in: CK3 _custom_loc.info:1-2 and EU5
	// customizable_localization.info:3.
	{Folder: "common/customizable_localization", Games: ck3AndEU5, RootKey: "type", References: customTextReferences},
	// The scope of a scripted GUI is the scope type it is available to: Vic3
	// scripted_guis.md:3 and EU5 scripted_guis.info:2.
	{Folder: "common/scripted_guis", Games: vic3AndEU5, RootKey: "scope"},
	// The type of a parliament agenda or type is the scope its root is: EU5
	// parliament_agendas/readme.txt:2-5 and parliament_types/readme.txt:2-3.
	{Folder: "common/parliament_agendas", Games: []string{game.EU5}, RootKey: "type"},
	{Folder: "common/parliament_types", Games: []string{game.EU5}, RootKey: "type"},
	// The scope of a filter is the type of object it operates on: EU5
	// gui/filters/readme.txt:12.
	{Folder: "gui/filters", Games: []string{game.EU5}, RootKey: "scope"},
	{Folder: "common/customizable_localization", References: customTextReferences},
	// a call writes yes and never no, as CK3
	// events/activities/activity_system_events.txt:151 does
	{
		Folder: "common/scripted_effects", DeclaringKeys: []string{"scripted_effect"},
		Callers: []string{CallEffect}, Shape: ShapeEffects, Parameters: true, Values: []string{"yes"},
		Precedences: scriptedPrecedences,
	},
	// a value block calls them too, and a call writes yes, or no for the
	// opposite (CK3 chariot_ongoing_events_jp.txt:189)
	{
		Folder: "common/scripted_triggers", DeclaringKeys: []string{"scripted_trigger"},
		Callers: []string{CallTrigger, CallValue}, Shape: ShapeTriggers, Parameters: true, Values: Bools,
		Precedences: scriptedPrecedences,
	},
	// CK3 common/activities/activity_types/camp_party.txt:638
	{
		Folder: "common/scripted_modifiers", Callers: []string{CallValue}, Shape: ShapeValue, Parameters: true,
		Values: []string{"yes"},
	},
	{
		Folder: "common/script_values", Callers: []string{CallTrigger, CallValue}, Shape: ShapeValue,
		Precedences: unsettledPrecedences,
	},
	{Folder: "common/scripted_lists", Type: TypeList},
	// CK3 _effect_localization.info:9-17 and _trigger_localization.info:15;
	// EU5 international_organization_effects.txt:169 refers to one no entry
	// defines
	{Folder: "common/effect_localization", AllowsUndefined: true},
	{Folder: "common/trigger_localization", AllowsUndefined: true},
	// a game rule holds a block for each of its settings (GameRuleSettings
	// cites the three games' docs); the keys the docs write for a rule stay
	// its fields
	{Folder: "common/game_rules", Nestings: []Nesting{{Database: "setting"}}},
	// define:Group|Name reads one define (CK3 event_targets.log:109). A
	// group is opened in several files, so groups merge, and one define
	// keeps the last copy by file name (the games' wikis, Defines).
	{
		Folder: "common/defines", Prefixes: []string{"define", "array_define"}, Separator: "|",
		Precedences: []Precedence{{Keeps: KeepMerged}},
		Nestings:    []Nesting{{Database: "define", Precedence: Precedence{Keeps: KeepLast}}},
	},
	// modes at the second level: the Vic3 wiki's Genes; EU5
	// D008_genes_accessories.txt:1-3 writes INJECT_OR_CREATE inside
	// accessory_genes
	{
		Folder:      "common/genes",
		WrapperKeys: []string{"color_genes", "special_genes", "morph_genes", "accessory_genes", "decal_atlases", "age_presets"},
		Precedences: []Precedence{{Games: vic3AndEU5, Keeps: KeepFirst, Modes: ModePlaceWrapped}},
	},
	{Folder: "common/named_colors", WrapperKeys: []string{"colors"}},
	{Folder: "data_binding", NameKey: "definition"},
	// common/game_concepts/00_game_concepts.txt:2 gives vassal the second
	// names of an alias
	{
		Folder: "common/game_concepts", Type: TypeConcept,
		Nestings: []Nesting{{Games: []string{game.CK3}, KeyPath: "alias", Sets: true}},
	},
	{Folder: "common/achievement_groups.txt", NameKey: "name"},
	{Folder: "common/coat_of_arms/options", DefinedBy: DefinedByNothing},
	// the EU5 wiki's Flag modding
	{
		Folder: "common/coat_of_arms/coat_of_arms", WrapperKeys: []string{"template"}, Mirrors: true,
		Precedences: []Precedence{{Games: []string{game.EU5}, Keeps: KeepLast}},
	},
	{
		Folder: "common/coat_of_arms/template_lists",
		WrapperKeys: []string{
			"coat_of_arms_template_lists", "pattern_texture_lists", "colored_emblem_texture_lists",
			"textured_emblem_texture_lists", "color_lists",
		},
	},
	{Folder: "gui/scripted_widgets", DefinedBy: DefinedByNothing},
	{Folder: "music/music_player_categories", NameKey: "id"},
	// pattern_textures, pattern_layout and variation are each a database
	// named by its name key (CK3 afr.txt:11-12). The game writes one name in
	// several files (catholic.txt:107, crusaders.txt:107), so every copy
	// stays.
	{
		Folder: "gfx/portraits/accessory_variations", DefinedBy: DefinedByNothing,
		Nestings: []Nesting{
			{Database: "pattern_textures", Keys: "pattern_textures", NameKey: "name", Precedence: Precedence{Keeps: KeepMerged}},
			{Database: "pattern_layout", Keys: "pattern_layout", NameKey: "name", Precedence: Precedence{Keeps: KeepMerged}},
			{Database: "variation", Keys: "variation", NameKey: "name", Precedence: Precedence{Keeps: KeepMerged}},
		},
	},
	// a widget key naming a type
	{Name: "gui.type", Callers: []string{GUITagCalls}, FoldsCase: true, Precedences: guiTypePrecedences},
	{Name: "gui.local_type", Callers: []string{GUITagCalls}, Local: true},
	{Name: "gui.template", Callers: []string{GUITemplateCalls}, Precedences: guiTypePrecedences},
	{Name: "gui.local_template", Callers: []string{GUITemplateCalls}, Local: true},
	// a GUI block belongs to its type, so many share a name
	{Name: "gui.block", Callers: []string{GUIBlockCalls}, FoldsCase: true, Precedences: []Precedence{{Keeps: KeepMerged}}},
	// the keys of any widget whose values name definitions or files
	{
		Name: "gui.widget",
		References: []KeyReference{
			{KeyPath: GUITemplateCalls, Target: "gui.template"},
			{KeyPath: "layer", Target: "gui.layer"},
			{KeyPath: "texture", Target: File},
			{KeyPath: "mask", Target: File},
			{KeyPath: GUIFontKey, Target: GUIFontDatabase},
			{KeyPath: "text", Target: Localization, Literal: true},
			{KeyPath: "title", Target: Localization, Literal: true},
		},
	},
	// a text names a format or an icon in any case
	{Name: "gui.format", FoldsCase: true},
	{Name: "gui.texticon", FoldsCase: true},
}

// Called lists the databases whose definitions are called by their names,
// sorted by name.
var Called = func() []string {
	var called []string
	for _, declaration := range Declarations {
		if len(declaration.Callers) > 0 {
			called = append(called, declaration.Database())
		}
	}

	slices.Sort(called)

	return slices.Compact(called)
}()

// FileTypes holds what each file extension says about a file. In a .txt or
// .gui file, a key that ends in color takes a color even where its block is
// plain numbers. A portrait gene's such key does not.
var FileTypes = map[string]FileType{
	".txt":   {ColorSuffix: "color"},
	".asset": {NameKey: "name"},
	".font":  {NameKey: "name"},
	".gui":   {Shape: ShapeWidget, ColorSuffix: "color"},
}

// ToDatabase returns the name of the database of a folder or file: its path
// without common/ at its start and without its extension.
func ToDatabase(folder string) string {
	database := strings.TrimPrefix(folder, "common/")

	return strings.TrimSuffix(database, path.Ext(database))
}

// Database returns the name of the declaration's database: the name the row
// gives, else the one its folder gives.
func (d Declaration) Database() string {
	if d.Name != "" {
		return d.Name
	}

	return ToDatabase(d.Folder)
}

// Command returns the database of commands a call of the definitions stands
// beside: effects where their block holds effects, triggers where it holds
// triggers, else "".
func (d Declaration) Command() string {
	switch {
	case len(d.Callers) == 0:
		return ""
	case d.Shape == ShapeEffects:
		return DumpEffects
	case d.Shape == ShapeTriggers:
		return DumpTriggers
	}

	return ""
}

// callerShapes holds what a block holds for each kind of place a scripted
// definition is called from.
var callerShapes = map[string]Shape{CallTrigger: ShapeTriggers, CallEffect: ShapeEffects, CallValue: ShapeValue}

// Proves returns what a block may hold if a key in it calls one of the
// definitions, as triggers where a trigger block calls them. A call from a
// GUI key proves nothing.
func (d Declaration) Proves() []Shape {
	var shapes []Shape
	for _, caller := range d.Callers {
		if shape, ok := callerShapes[caller]; ok {
			shapes = append(shapes, shape)
		}
	}

	return shapes
}

// FindDeclaration returns the declaration of a folder or file. Where
// several rows fit, the first is taken, and the rows of one game come before
// the rows of every game. It returns false if no row fits.
func (d *Dialect) FindDeclaration(folder string) (Declaration, bool) {
	for _, row := range Declarations {
		if row.Folder == folder && folder != "" && d.Matches(row.Games) {
			return row, true
		}
	}

	return Declaration{}, false
}

// LookupDeclaration returns the declaration of the database with the given
// name. Where several rows fit, the first is taken. It returns an empty
// Declaration if no row fits.
func (d *Dialect) LookupDeclaration(database string) Declaration {
	for _, row := range Declarations {
		if d.Matches(row.Games) && row.Database() == database {
			return row
		}
	}

	return Declaration{}
}

// ListDeclarations returns the declarations that hold for the game, in the
// order of Declarations.
func (d *Dialect) ListDeclarations() []Declaration {
	return slices.DeleteFunc(slices.Clone(Declarations), func(row Declaration) bool { return !d.Matches(row.Games) })
}

// DefinitionTexts holds the databases in which the title of a definition is
// not the localization key of its plain name, or in which a definition has
// a description, with the keys of both.
var DefinitionTexts = []TextKeys{
	{Games: []string{game.CK3, game.EU5}, Title: "game_concept_%s", Databases: []string{"game_concepts"}},
	// localization/english/content_304_l_english.yml:228 titles
	// arranged_accident_failed by this form
	{Games: []string{game.Vic3}, Title: "notification_%s_name", Databases: []string{"messages"}},
	{Games: []string{game.CK3}, Description: "%s_desc", Databases: []string{
		"activities/activity_locales", "activities/activity_types", "activities/activity_types.phase",
		"activities/intents", "ai_war_stances", "bookmarks/bookmarks", "bookmarks/challenge_characters", "buildings",
		"character_memory_types", "council_positions", "court_positions/tasks", "court_positions/types",
		"court_types", "culture/eras", "culture/innovations", "decisions", "districts", "dlc_metadata", "factions",
		"governments", "history/faiths", "important_actions", "inspirations", "lease_contracts", "modifiers",
		"nicknames", "puppets/types", "raids/intents", "religion/faith_types", "religion/religion_types",
		"religion/rite_types", "schemes/agent_types", "secret_types", "situation/catalysts", "struggle/catalysts",
		"struggle/struggles", "subject_contracts/groups", "suggestions", "task_contracts",
		"trade/trade_good_categories", "trade/trade_good_types", "trade_tasks", "travel/travel_options",
		"vassal_stances",
	}},
	{Games: []string{game.CK3}, Title: "%s_name", Databases: []string{
		"culture/aesthetics_bundles", "culture/creation_names", "dynasty_perks", "lifestyle_perks",
		"religion/doctrine_group_types",
	}},
	{Games: []string{game.CK3}, Title: "%s_name", Description: "%s_desc", Databases: []string{
		"culture/traditions", "dynasty_legacies", "lifestyles", "religion/doctrine_types", "religion/tenet_types",
	}},
	{Games: []string{game.CK3}, Title: "%s_title", Databases: []string{"activities/pulse_actions"}},
	{Games: []string{game.CK3}, Title: "activity_group_type_%s", Databases: []string{"activities/activity_group_types"}},
	{Games: []string{game.CK3}, Title: "confederation_type_%s_name", Databases: []string{"confederation_types"}},
	{Games: []string{game.CK3}, Title: "decision_group_type_%s", Databases: []string{"decision_group_types"}},
	{Games: []string{game.CK3}, Title: "epidemic_%s", Databases: []string{"epidemics"}},
	{Games: []string{game.CK3}, Title: "feature_%s", Databases: []string{"artifacts/features"}},
	{Games: []string{game.CK3}, Title: "message_group_type_%s", Databases: []string{"message_group_types"}},
	{Games: []string{game.CK3}, Title: "situation_%s", Description: "situation_%s_desc", Databases: []string{
		"situation/situations",
	}},
	{Games: []string{game.CK3}, Title: "situation_group_type_%s", Databases: []string{"situation/situation_group_types"}},
	// duchy_office_buildings_l_english.yml:20
	{Games: []string{game.CK3}, Description: "building_type_%s_desc", Databases: []string{"buildings"}},
	// common/domiciles/buildings/_domicile_buildings.info:2-3
	{
		Games: []string{game.CK3}, Title: "%s_domicile_building", Description: "%s_domicile_building_desc",
		Databases: []string{"domiciles/buildings"},
	},
	{Games: []string{game.EU5}, Description: "%s_desc", Databases: []string{
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
	{Games: []string{game.EU5}, Title: "%s_name", Description: "%s_desc", Databases: []string{"bookmarks"}},
	{Games: []string{game.Vic3}, Description: "%s_desc", Databases: []string{
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
	{Games: []string{game.Vic3}, Title: "%s_name", Description: "%s_desc", Databases: []string{"proposal_types"}},
	// A modifier's name is the first of these keys the game has a text for.
	// CK3: stewardship (localization/english/core_l_english.yml:577),
	// MOD_STEWARDSHIP and MOD_MONTHLY_INCOME
	// (modifiers/modifiers_l_english.yml:4 and 686),
	// MOD_TRADE_ROUTE_LENGTH_ADD (modifiers/gdp_modifiers_l_english.yml:13).
	// EU5: MODIFIER_TYPE_NAME_<key>
	// (main_menu/localization/english/modifier_types_l_english.yml:2).
	{Games: []string{game.CK3}, Title: "%s", Databases: []string{ModifierFormats}},
	{Games: []string{game.CK3}, Title: "MOD_%s", Capitals: true, Databases: []string{ModifierFormats}},
	{Games: []string{game.CK3}, Title: "MOD_%s_ADD", Capitals: true, Databases: []string{ModifierFormats}},
	{Games: []string{game.CK3}, Title: "MOD_MONTHLY_%s", Capitals: true, Databases: []string{ModifierFormats}},
	{Games: []string{game.EU5}, Title: "MODIFIER_TYPE_NAME_%s", Databases: []string{"modifier_type_definitions"}},
}

// ListTextKeys returns the localization keys that may hold the title or the
// description of a definition that names none itself, in the order the game
// tries them. key is TitleKey or DescriptionKey. Without a row the title's
// key is the definition's name, and there is no description.
func (d *Dialect) ListTextKeys(reference Reference, key string) []string {
	var keys []string
	for _, row := range DefinitionTexts {
		if !d.Matches(row.Games) || !slices.Contains(row.Databases, reference.Database) {
			continue
		}

		pattern, name := row.Description, reference.Name
		if key == TitleKey {
			pattern = row.Title
		}

		if row.Capitals {
			name = strings.ToUpper(name)
		}

		if pattern != "" {
			keys = append(keys, strings.Replace(pattern, "%s", name, 1))
		}
	}

	if len(keys) == 0 && key == TitleKey {
		return []string{reference.Name}
	}

	return keys
}

// DefinitionPictures are the pictures and paintings of each game's goods,
// buildings, technologies and what they are grouped under. Each row cites the
// game's file and line, or a file of the folder it names.
var DefinitionPictures = []PictureRow{
	// gfx/interface/icons/trade/trade_goods/trade_good_alum.dds and
	// trade_good_categories/trade_category_agricultural_goods.dds
	{Games: []string{game.CK3}, Database: "trade/trade_good_types", Pattern: "gfx/interface/icons/trade/trade_goods/%s"},
	{
		Games: []string{game.CK3}, Database: "trade/trade_good_categories",
		Pattern: "gfx/interface/icons/trade/trade_good_categories/%s",
	},
	// regular_district_buildings/00_guild_district_buildings.txt:852 writes
	// type_icon, a shape the game tints
	{
		Games: []string{game.CK3}, Database: "buildings", Key: "type_icon", Folder: "gfx/interface/icons/building_types",
		Silhouette: true,
	},
	// common/culture/innovations/00_cultural_maa_innovations.txt:2 writes
	// the constant that line 40 gives as the icon
	{Games: []string{game.CK3}, Database: "culture/innovations", Key: "icon"},
	// common/districts/duchy_capital_districts.txt:8 and 16 write a
	// background for some cases and one for the rest. An era's is
	// gfx/interface/window_culture/culture_era_illustrations/
	// culture_era_tribal.dds
	{Games: []string{game.CK3}, Database: "districts", Painting: true, Key: "background/reference"},
	{
		Games: []string{game.CK3}, Database: "culture/eras", Painting: true,
		Pattern: "gfx/interface/window_culture/culture_era_illustrations/%s",
	},
	// common/goods/00_goods.txt:10; buildings/01_industry.txt:175 and 211, an
	// icon and the background behind it;
	// technology/technologies/10_production.txt:6-7, whose category names
	// gfx/interface/illustrations/tech_tree/illustration_tech_production.dds
	{Games: []string{game.Vic3}, Database: "goods", Key: "texture"},
	{Games: []string{game.Vic3}, Database: "buildings", Key: "icon"},
	{Games: []string{game.Vic3}, Database: "buildings", Painting: true, Key: "background"},
	{Games: []string{game.Vic3}, Database: "technology/technologies", Key: "texture"},
	{
		Games: []string{game.Vic3}, Database: "technology/technologies", Painting: true, Field: "category",
		Pattern: "gfx/interface/illustrations/tech_tree/illustration_tech_%s",
	},
	// common/pop_types/laborers.txt:2 writes a pop type's icon, as
	// gfx/interface/icons/pops_icons/laborers.dds
	{Games: []string{game.Vic3}, Database: "pop_types", Key: "texture"},
	// common/production_methods/01_industry.txt:928 writes a production
	// method's icon, and common/production_method_groups/01_industry.txt:95
	// its group's
	{Games: []string{game.Vic3}, Database: "production_methods", Key: "texture"},
	{Games: []string{game.Vic3}, Database: "production_method_groups", Key: "texture"},
	// main_menu/gfx/interface/icons/trade_goods/icon_goods_alum.dds,
	// icons/buildings/ablaq_palace.dds, icons/building_categories/
	// consumer_goods_category.dds, icons/pops/clergy.dds
	{Games: []string{game.EU5}, Database: "goods", Pattern: "gfx/interface/icons/trade_goods/icon_goods_%s"},
	{Games: []string{game.EU5}, Database: "pop_types", Pattern: "gfx/interface/icons/pops/%s"},
	{Games: []string{game.EU5}, Database: "building_types", Pattern: "gfx/interface/icons/buildings/%s"},
	{Games: []string{game.EU5}, Database: "building_categories", Pattern: "gfx/interface/icons/building_categories/%s"},
	// in_game/common/advances/0_age_of_renaissance.txt:6 writes an advance's
	// icon. One that writes none shows the file named after it, as
	// advance/baptism_books.dds, else advance/_default.dds. An age's painting
	// is illustrations/advances/age_1_traditions.dds
	{Games: []string{game.EU5}, Database: "advances", Key: "icon", Folder: "gfx/interface/advance"},
	{Games: []string{game.EU5}, Database: "advances", Pattern: "gfx/interface/advance/%s"},
	{Games: []string{game.EU5}, Database: "advances", Pattern: "gfx/interface/advance/_default"},
	{Games: []string{game.EU5}, Database: "age", Painting: true, Pattern: "gfx/interface/illustrations/advances/%s"},
}

// Nestings holds the nestings that are written in the definitions of every
// database below a folder, and so belong to no one declaration.
var Nestings = []Nesting{
	// parameters are switched on with name = yes
	// (10_house_bloc_types.txt:21-24), and one name is set in many
	// doctrines (20_doctrines.txt:2066), so every copy stays
	{
		Games: []string{game.CK3}, Parent: "common", KeyPath: "**/parameters", Database: "parameter", Sets: true,
		Precedence: Precedence{Keeps: KeepMerged},
	},
	// common/accolade_types/04_ep2_common_attributes.txt:416-418 lists the
	// names it switches on
	{
		Games: []string{game.CK3}, Parent: "common", KeyPath: "**/*_parameters", Database: "parameter", Sets: true,
		Precedence: Precedence{Keeps: KeepMerged},
	},
}

// Label returns the name of the database the nesting's definitions belong
// to: the parent's name, a dot and the nesting's own. For a nesting with no
// name of its own it is the parent's.
func (n Nesting) Label() string {
	if n.Database == "" {
		return ToDatabase(n.Parent)
	}

	return ToDatabase(n.Parent) + "." + n.Database
}

// ListNestings returns the nestings written in the definitions of a
// declaration's folder, its own and then those of Nestings above it, each
// with its parent filled in.
func (d *Dialect) ListNestings(declaration Declaration) []Nesting {
	if declaration.Folder == "" {
		return nil
	}

	var nestings []Nesting
	for _, nesting := range declaration.Nestings {
		if d.Matches(nesting.Games) {
			nesting.Parent = declaration.Folder
			nestings = append(nestings, nesting)
		}
	}

	for _, nesting := range Nestings {
		isBelow := declaration.Folder == nesting.Parent || strings.HasPrefix(declaration.Folder, nesting.Parent+"/")
		if isBelow && d.Matches(nesting.Games) {
			nestings = append(nestings, nesting)
		}
	}

	return nestings
}

// JoinList returns the given texts joined as in a sentence, the last two by
// the given conjunction: "A", "A and B" or "A, B and C". It returns "" for
// none.
func JoinList(texts []string, conjunction string) string {
	if len(texts) < 2 {
		return strings.Join(texts, "")
	}

	return strings.Join(texts[:len(texts)-1], ", ") + " " + conjunction + " " + texts[len(texts)-1]
}

// definitionNouns holds what a definition is called, one and several, for
// the databases whose name does not say.
var definitionNouns = map[string]struct{ One, Many string }{
	"messages":      {"player message", "player messages"},
	SavedScopes:     {"saved scope", "saved scopes"},
	Variables:       {"variable", "variables"},
	LocalVariables:  {"local variable", "local variables"},
	GlobalVariables: {"global variable", "global variables"},
	Lists:           {"list", "lists"},
	Flags:           {"flag", "flags"},
}

// plurals holds how an English noun ends in the plural, each with how it
// ends in the singular. The first ending that fits a noun is used.
var plurals = []struct{ Plural, One string }{
	// the same in the singular and the plural
	{"arms", "arms"},
	{"goods", "goods"},
	{"belli", "belli"},
	{"criteria", "criteria"},
	{"data", "data"},
	// a y after a vowel only takes an s
	{"ays", "ay"},
	{"eys", "ey"},
	{"oys", "oy"},
	{"uys", "uy"},
	{"ouses", "ouse"},
	{"eases", "ease"},
	{"ies", "y"},
	{"ses", "s"},
	{"xes", "x"},
	{"ches", "ch"},
	{"shes", "sh"},
	{"s", ""},
}

// DescribeDefinition returns what a definition of a database is called, as
// "scripted effect" for scripted_effects, or "scripted effects" where
// plural is true. It returns "" for a database with no name.
func DescribeDefinition(database string, plural bool) string {
	if nouns, ok := definitionNouns[database]; ok {
		if plural {
			return nouns.Many
		}

		return nouns.One
	}

	last := database[strings.LastIndexAny(database, "/.")+1:]
	singular := last
	for _, ending := range plurals {
		if stem, cut := strings.CutSuffix(last, ending.Plural); cut {
			singular = stem + ending.One

			break
		}
	}

	if !plural {
		return strings.ReplaceAll(singular, "_", " ")
	}

	// a name already in the singular takes the plural of its ending
	if singular == last && last != "" {
		for _, ending := range plurals {
			if stem, cut := strings.CutSuffix(last, ending.One); cut {
				last = stem + ending.Plural

				break
			}
		}
	}

	return strings.ReplaceAll(last, "_", " ")
}
