// gui.go holds the rules for GUI files that every game shares. What differs by
// game is on the game's row.

package game

import (
	"slices"
	"strings"
)

// WidgetType is how a built-in widget type lays its children out and draws its
// own text.
type WidgetType struct {
	// Holding is how it holds its children: row, column, flow, grid or
	// scroll. It is "" for by their anchors.
	Holding string `json:"holding,omitempty"`

	// Clips is whether it clips its children, and Fills whether it fills its
	// holder.
	Clips bool `json:"clips,omitempty"`
	Fills bool `json:"fills,omitempty"`

	// CentersText is whether its own text is centered.
	CentersText bool `json:"centersText,omitempty"`

	// DropsHidden is whether a hidden child of it takes no room, and
	// KeepsHidden whether a hidden child keeps its room whatever the widget
	// writes.
	DropsHidden bool `json:"dropsHidden,omitempty"`
	KeepsHidden bool `json:"keepsHidden,omitempty"`
}

// WidgetSlot is a key whose block is a widget that no type names.
type WidgetSlot struct {
	// Role is what the block is to its widget: skin (a background), chrome (a
	// scroll bar), attachment (a tooltip) or label (its text).
	Role string

	// Type is the built-in type the block is drawn as. It is "" for the
	// block's own.
	Type string

	// Floats is whether the block floats apart from its widget, as a tooltip
	// does, so that it is drawn as no child of it.
	Floats bool

	// Dresses is whether the block only dresses its widget, so that a picture
	// it lacks draws no placeholder.
	Dresses bool

	// Labels is whether the block's widgets show their widget's text. They
	// come before its other children, and the next label replaces them.
	Labels bool
}

// Animation is how one state of a widget leads to others.
type Animation struct {
	// NextKey, FinishKey and AlphaKey are the keys of the next state, of the
	// calls run as the state ends and of its alpha.
	NextKey   string
	FinishKey string
	AlphaKey  string

	// Trigger and FindChild are the data functions that run a state by its
	// name, on the widget or on a child.
	Trigger   string
	FindChild string
}

// VariableSystem is the GUI's own variables. Each of its functions takes the
// variable's name first.
type VariableSystem struct {
	// Type is the data type the variables are reached through.
	Type string

	// Exists, HasValue and UnsetOrValue are the functions that hold while a
	// variable is set, is set to the given value, or is unset or set to the
	// given value.
	Exists       string
	HasValue     string
	UnsetOrValue string

	// Set is the function that sets a variable, to the given value or to
	// none, Clear the one that unsets it, and Toggle the one that sets a
	// variable that is unset and unsets one that is set.
	Set    string
	Clear  string
	Toggle string
}

// FontRules is how a font file names a font's files: a block for each style
// names a definition of files, which has groups of languages, each with the
// files they read, the font's own file first.
type FontRules struct {
	// Key is the key of a widget that names its font, and SizeKey the key that
	// sets the size of its text.
	Key     string
	SizeKey string

	// TextType is the built-in type of a text, whose own type in the game's
	// files gives a text with no font its font and size.
	TextType string

	// Database is the database of fonts.
	Database string

	// StyleKey is the key of a font's block for one of its styles, and
	// StyleValueKey the key in that block whose value is the style, such as
	// regular or bold|italic.
	StyleKey      string
	StyleValueKey string

	// FilesKey is the key in that block that names the definition of the
	// style's files, and FilesDatabase the database of those definitions.
	FilesKey      string
	FilesDatabase string

	// GroupKey is the key of a definition's block for a group of languages,
	// and LanguagesKey and PathsKey the keys in a group of its languages and
	// of its files.
	GroupKey     string
	LanguagesKey string
	PathsKey     string
}

// DateCalls is how a data call in a text that writes a date is known.
type DateCalls struct {
	// Returns is the type its last function returns, and Endings the endings
	// of that function's name.
	Returns string
	Endings []string
}

// DateForm is how the games write a date.
type DateForm struct {
	// Key is the localization key of the form, and Day, Month and Year the
	// names the form fills in.
	Key              string
	Day, Month, Year string

	// Months are the localization keys of the months' names, January first.
	Months []string
}

// TierFrames is how a game draws the tier of a title from a sheet of cells, a
// cell for each tier.
type TierFrames struct {
	// Function is the data function that gives a title's cell, counting from
	// one, and Tiers the tiers in the order of the cells.
	Function string
	Tiers    []string
}

// Placeholder is a shape PMT draws where a picture has no file, for the things
// the picture can be of.
type Placeholder struct {
	// Games are the games the row holds for, every game if none.
	Games []string

	Shape string

	// Terms are what the picture can be of, as keys, data types and data
	// functions write it, in lower case with spaces.
	Terms []string

	// Runtime is whether only the running game draws these pictures, from
	// what it holds, so that no file is looked for.
	Runtime bool
}

// Illustrations is where the pictures are that a definition names by a path
// below a folder.
type Illustrations struct {
	// FolderKey is the key that names the folder, and Folder the folder where
	// the definition names none.
	FolderKey string
	Folder    string
}

// GameGUI is what one game does with GUI files beyond what every game does.
type GameGUI struct {
	// Holders are the windows that a window made from a type is laid out in,
	// by the type.
	Holders map[string]string

	// Undeclared are the widget types the game draws though no file of its
	// install declares them.
	Undeclared []string

	// DropsHidden are the types beyond GUI.Types in which a hidden child takes
	// no room.
	DropsHidden []string

	// SuffixStems are the stems of the defines for a picture field, each with
	// the stem its suffix define is written under, where the two differ.
	SuffixStems map[string]string

	// DefineGroup is the define group whose defines say where the game's
	// icons and pictures are.
	DefineGroup string

	// Tiers is how the game draws a title's tier from a sheet of cells. It is
	// the zero TierFrames for none.
	Tiers TierFrames

	Illustrations Illustrations
}

// Levels is how a level, such as prestige_level, finds its pictures.
type Levels struct {
	// Ending marks a name as a level, and Prefix starts the names of its
	// files.
	Ending string
	Prefix string
}

// GUISpec is the rules for GUI files beyond their syntax. GUI is its one row.
type GUISpec struct {
	// Declares are the keys before a top block that declare a definition of
	// gui.<key>, or that group definitions, as types x { } does.
	Declares []string

	// TypeKey is the key that declares a type. A type no file defines is
	// marked missing under it.
	TypeKey string

	// BlockKey is the key that declares a labeled block, which a widget's
	// BlockCalls block replaces.
	BlockKey string

	// NameKeyOf maps a block key at any depth to the key whose value names
	// its definition.
	NameKeyOf map[string]string

	// WidgetKey is the key that names any other top block. A file's only
	// block that writes none is named after the file.
	WidgetKey string

	// DatabasePrefix is what a GUI database's name starts with, before the
	// key that declares it.
	DatabasePrefix string

	// WidgetDatabase is the database of every top block that no key of
	// Declares declares. WidgetTypes is the database of the widget types a
	// block's key names where no type of a file has the name: those the game
	// draws itself, and those the files define or build on.
	WidgetDatabase string
	WidgetTypes    string

	// TagCalls is the caller of the type a block's key names, as
	// Callable.Callers lists it.
	TagCalls string

	// TemplateCalls is the key that calls a template, and BlockCalls the key
	// that calls the block a template or a type leaves for a widget to
	// replace.
	TemplateCalls string
	BlockCalls    string

	// ContextKey is the key whose value is the data call its block's bindings
	// read in.
	ContextKey string

	// TextFormats is the database a text's #format names, and TextIcons the
	// database an icon in a text, @icon!, names.
	TextFormats string
	TextIcons   string

	// EditorFolders are the folders of the game's own GUI editor files, which
	// call editor functions no dump declares. They are not judged.
	EditorFolders []string

	Slots map[string]WidgetSlot

	// ItemKey is the key of a data model's block for the widgets of each of
	// its items.
	ItemKey string

	// Types is the row of each built-in widget type. Any other type holds its
	// children by their anchors and keeps a hidden child's room.
	Types map[string]WidgetType

	// OpeningStates are the states run by name as a window opens, and
	// OpeningTriggers the states run as it opens by a key's value.
	OpeningStates   []string
	OpeningTriggers map[string]string

	// StateKey is the key of a widget's block for one of its states, and
	// StateKeys the keys inside one that are the state's own rather than end
	// values.
	StateKey  string
	StateKeys []string

	// OpeningCalls is the key of a state whose calls run as the state starts.
	OpeningCalls string

	Animation Animation

	// RepeatedKeys are the keys a widget keeps every value of, in the order
	// written, each adding to the ones before.
	RepeatedKeys []string

	// TextKeys are the keys that write a widget's one text, the localized one
	// first and then the one shown as written. The last one written wins.
	TextKeys []string

	// MultilineKey is the key whose yes lets a widget's text run over several
	// lines, and ResizeKey the key whose yes resizes a widget to fit what it
	// holds.
	MultilineKey string
	ResizeKey    string

	// SizeKey is the key that sets a widget's size.
	SizeKey string

	// OverlayKey is the key of a block that lays a texture over its widget's
	// own, and OverlayTextureKey the key of the texture in that block.
	OverlayKey        string
	OverlayTextureKey string

	// CallOutputs is what a data call in a text writes, number or text, by
	// its last function's return type. A type with no row writes text.
	CallOutputs map[string]string

	DateCalls DateCalls
	DateForm  DateForm

	// PercentCode is the format code a data call in a text writes after | for
	// a percent. A digit among the codes is the number's decimals.
	PercentCode string

	// SignCode is the format code a data call in a text writes after | for a
	// plus sign before a number above zero.
	SignCode string

	// ColorType is the data type that a binding of a color returns.
	ColorType string

	// SelectPrefix is the start of the data functions that give their second
	// argument if their first holds, else their third, as Select_float.
	SelectPrefix string

	// ClickKeys are the keys whose calls run as a widget is clicked.
	ClickKeys []string

	// DownKey is the key whose condition holds while a button is drawn down.
	// Side-by-side buttons of one type that write it are tabs.
	DownKey string

	// CheckedKey is the key whose condition holds while a box is drawn
	// checked.
	CheckedKey string

	// ValidFunction is the data function that asks whether an object exists.
	ValidFunction string

	// OpeningFacts are the data functions with no arguments that hold
	// whenever a window opens, as an object existing or a list having items.
	OpeningFacts []string

	// SampleFacts are the data functions with no arguments that hold of the
	// Visual view's sample: a living, landed player ruler with legitimacy and
	// a council.
	SampleFacts []string

	// SampleAbsent are the data functions that reach an object the sample
	// lacks, so that a test of its existence, or of anything past it, fails.
	SampleAbsent []string

	Variables VariableSystem

	// PortraitKey is the key whose binding draws a character's portrait, and
	// CoatKey the key whose binding draws a coat of arms.
	PortraitKey string
	CoatKey     string

	// MaskKeys are the keys whose image cuts a widget's picture to its shape:
	// a coat of arms' mask, and a widget's own.
	MaskKeys []string

	Fonts FontRules

	// Getters are how the name of a data function that only reaches an
	// object starts, such as Get.
	Getters []string

	// NoTooltip is how the name of a data function ends when it shows the
	// same as the function without it but turns off the tooltip.
	NoTooltip string

	// HiddenKeys are the keys whose no hides a widget. The first one written
	// decides.
	HiddenKeys []string

	// DropsHiddenKey is the key whose yes makes a hidden child of a widget
	// take no room.
	DropsHiddenKey string

	// CallTargets are the data functions whose quoted arguments are names,
	// each with its arguments' databases in order: "" for no name.
	CallTargets map[string][]string

	// DefineCall is the data function that reads a define. Its CallTargets
	// row names the defines' databases.
	DefineCall string

	// StepDatabases are the databases whose definitions a data call names as
	// a step of its own, which no dump declares.
	StepDatabases []string

	// Booleans are the data functions a binding combines conditions with, by
	// name without the argument count.
	Booleans map[string]string

	// Literals are a condition's plain values, in lower case, each with
	// whether it holds.
	Literals map[string]bool

	// Comparisons are the data functions that compare two numbers, by name
	// before the underscore, each with the cmp.Compare results it holds for.
	Comparisons map[string][]int

	// ModelSize is the data function that gives how many items a data model
	// has, and ModelEmpty and ModelHas the ones that ask whether it has none
	// and whether it has some.
	ModelSize  string
	ModelEmpty string
	ModelHas   string

	// IconPathFile is the file in an icon folder that names the paths its
	// icons are read from.
	IconPathFile string

	// PictureKeys are the keys a definition names its picture by, tried in
	// this order.
	PictureKeys []string

	// IconPathSuffix is the ending of the defines that name a folder of
	// icons.
	IconPathSuffix string

	// FolderDefineSuffix, PrefixDefineSuffix and NameDefineSuffix are the
	// endings, after a database and a field, of the defines that say where a
	// data function finds its pictures, how each picture file's name starts
	// and how it ends.
	FolderDefineSuffix string
	PrefixDefineSuffix string
	NameDefineSuffix   string

	Levels Levels

	// DefaultPicture is the name, in the folder of a definition's pictures,
	// of the picture drawn for a definition with none of its own.
	DefaultPicture string

	// ShownFirst is the database whose first definition in a GUI file is the
	// one the file shows first, wherever the file writes it: its main window.
	ShownFirst string
}

// The keys, callers and values that GUI shares with another table or with
// another of its own fields, each written once.
const (
	guiType          = "type"
	guiBlock         = "block"
	guiDefineCall    = "GetDefine"
	guiFontKey       = "font"
	guiFontDatabase  = "font.font"
	guiWidgetKey     = "name"
	guiTagCalls      = "widget"
	guiTemplateCalls = "using"
	guiBlockCalls    = "blockoverride"
)

// GUI is what every game does with a GUI file.
var GUI = GUISpec{
	Declares:       []string{guiType, "local_type", "template", "local_template", guiBlock, "layer"},
	TypeKey:        guiType,
	BlockKey:       guiBlock,
	NameKeyOf:      map[string]string{"texticon": "icon", "format": "name"},
	WidgetKey:      guiWidgetKey,
	DatabasePrefix: "gui.",
	WidgetDatabase: "gui.widget",
	WidgetTypes:    "gui.widget_type",
	TagCalls:       guiTagCalls,
	TemplateCalls:  guiTemplateCalls,
	BlockCalls:     guiBlockCalls,
	ContextKey:     "datacontext",
	// gui/preload/textformatting.gui:6-7
	TextFormats: "gui.format",
	// gui/debug/placeholder_types_templates.gui:346-347
	TextIcons: "gui.texticon",
	// clausewitz/gui/gui_editor, and EU5's loading_screen stage
	EditorFolders: []string{"gui/gui_editor", "loading_screen/gui/gui_editor"},
	Slots: map[string]WidgetSlot{
		"background":           {Role: "skin", Type: "background", Dresses: true},
		"tooltipwidget":        {Role: "attachment", Floats: true},
		"scrollbar_vertical":   {Role: "chrome", Type: "scrollbar"},
		"scrollbar_horizontal": {Role: "chrome", Type: "scrollbar"},
		"buttontext":           {Role: "label", Labels: true},
		"scrollwidget":         {Type: "widget"},
		"zoomwidget":           {Type: "widget"},
	},
	ItemKey: "item",
	Types: map[string]WidgetType{
		"hbox":           {Holding: "row", DropsHidden: true},
		"uberhbox":       {Holding: "row", DropsHidden: true},
		"vbox":           {Holding: "column", DropsHidden: true},
		"ubervbox":       {Holding: "column", DropsHidden: true},
		"scrollvbox":     {Holding: "column", Clips: true, DropsHidden: true},
		"flowcontainer":  {Holding: "flow"},
		"fixedgridbox":   {Holding: "grid", KeepsHidden: true},
		"dynamicgridbox": {Holding: "grid", KeepsHidden: true},
		"scrollarea":     {Holding: "scroll", Clips: true},
		"uberscrollarea": {Holding: "scroll", Clips: true},
		"pieslice":       {Fills: true},
		"uberpieslice":   {Fills: true},
		"barslice":       {Fills: true},
		"treemapslice":   {Fills: true},
		"button":         {CentersText: true},
		"game_button":    {CentersText: true},
	},
	OpeningStates:   []string{"_show"},
	OpeningTriggers: map[string]string{"trigger_on_create": "yes"},
	StateKey:        "state",
	StateKeys: []string{
		guiWidgetKey, "duration", "next", "on_start", "on_finish", "trigger_on_create", "delay", "bezier", "loop", "animation",
		"restart_on_show", "skip_initial_animation", "trigger_when", "start_sound", "end_sound", "sstart_sound",
		"animation_speed", "loopinterval",
	},
	OpeningCalls: "on_start",
	// Vic3 gui/achievements/popup.gui:
	// [PdxGuiWidget.FindChild('achievements_popup_icon').TriggerAnimation('1')]
	Animation: Animation{
		NextKey: "next", FinishKey: "on_finish", AlphaKey: "alpha", Trigger: "TriggerAnimation", FindChild: "FindChild",
	},
	RepeatedKeys: []string{"datacontext", "onclick", "on_start"},
	// the wiki: raw_text is a text that is not localized
	TextKeys: []string{"text", "raw_text"},
	// the wiki's Interface page
	MultilineKey: "multiline",
	// the wiki's Interface page
	ResizeKey:  "autoresize",
	SizeKey:    "size",
	OverlayKey: "modify_texture", OverlayTextureKey: "texture",
	// the dumps' Return type of the last function of every data call in the
	// three installs' GUI texts; CPdxStringView is EU5's alone and int64
	// Vic3's. The four short integer types are the dumps' too, which a mod's
	// text may end a call in.
	CallOutputs: map[string]string{
		"CString": "text", "CUTF8String": "text", "CPdxStringView": "text",
		"int8": "number", "int16": "number", "int32": "number", "int64": "number",
		"uint8": "number", "uint16": "number", "uint32": "number", "uint64": "number",
		"CFixedPoint": "number", "float": "number", "double": "number",
	},
	// no data call in the three installs' GUI texts returns the dumps' Date
	// type; the calls that write a date return CString, such as EU5's
	// GetEndDate and GetDateString, so a name with one of these endings is
	// taken to write one
	DateCalls: DateCalls{Returns: "CString", Endings: []string{"Date", "DateString"}},
	// jomini/localization/jomini/date_l_english.yml:4 and
	// clausewitz/localization/cw_date_l_english.yml, the same in the three
	// installs (EU5's under loading_screen/)
	DateForm: DateForm{
		Key: "DATE_FORMATTING_LONG", Day: "DAY", Month: "MONTH", Year: "YEAR",
		Months: []string{
			"CW_DATE_January", "CW_DATE_February", "CW_DATE_March", "CW_DATE_April", "CW_DATE_May", "CW_DATE_June",
			"CW_DATE_July", "CW_DATE_August", "CW_DATE_September", "CW_DATE_October", "CW_DATE_November",
			"CW_DATE_December",
		},
	},
	// each game's English localization: CK3 core_l_english.yml:493
	// $VALUE|V0%$, :830 $VALUE|V2$, and :831 its own plus before $VALUE|2$;
	// Vic3 interfaces_l_english.yml:20 $VALUE|%0$ and :2040 $VALUE|v1$; EU5
	// diplomacy_l_english.yml:380-383 $VALUE|+=2$, $VALUE|+2%$ and $VALUE|=$
	PercentCode: "%",
	SignCode:    "=",
	// the dumps' Return type of GetColor in the three installs
	ColorType: "CVector4f",
	// the dumps' global functions Select_float( Arg0, Arg1, Arg2 ) and the
	// rest
	SelectPrefix:  "Select_",
	ClickKeys:     []string{"onclick"},
	DownKey:       "down",
	CheckedKey:    "checked",
	ValidFunction: "IsValid",
	// CK3 gui/window_council.gui:465 and :484 (IsValid),
	// gui/window_character_finder.gui:51 (HasItems)
	OpeningFacts: []string{"IsValid", "HasItems"},
	// CK3's character window shows a living, landed ruler with legitimacy
	// (window_character.gui:5440, :5722, :5633), EU5's event portraits are
	// living (cards.gui:1038, :1054), and CK3's council window shows each
	// seat's replace button and main skill (window_council.gui:609, :662,
	// :671)
	SampleFacts: []string{
		"IsAlive", "HasLandedTitles", "ShouldShowLegitimacy", "CanPotentiallySelectCouncillor", "HasMainSkill",
		"HasCouncillor", "IsVisible", "CanSelect",
	},
	// CK3's council window shows no forced next councilor, which
	// window_council.gui:925 marks with a debug square
	SampleAbsent: []string{"GetNextForcedCouncillor"},
	// EU5's data_types_uncategorized.txt declares
	// NotExistOrHasValue( Arg0, Arg1 )
	Variables: VariableSystem{
		Type: "VariableSystem", Exists: "Exists", HasValue: "HasValue", UnsetOrValue: "NotExistOrHasValue", Set: "Set",
		Clear: "Clear", Toggle: "Toggle",
	},
	PortraitKey: "portrait_texture",
	// CK3 gui/shared/coat_of_arms.gui
	CoatKey: "coat_of_arms",
	// CK3 gui/shared/coat_of_arms.gui:128 [GovernmentType.GetRealmMask]; Vic3
	// gui/company_panel.gui's portrait
	MaskKeys: []string{"coat_of_arms_mask", "mask"},
	// CK3 and Vic3 fonts/fonts.font, EU5 in_game/fonts/in_game_fonts.font
	Fonts: FontRules{
		Key: guiFontKey, SizeKey: "fontsize", TextType: "textbox", Database: guiFontDatabase, StyleKey: "fontstyle",
		StyleValueKey: "style", FilesKey: "fontfiles",
		FilesDatabase: "font.fontfiles", GroupKey: "group", LanguagesKey: "languages", PathsKey: "files",
	},
	Getters:        []string{"Get", "Access"},
	NoTooltip:      "NoTooltip",
	HiddenKeys:     []string{"visible", "visible_at_creation"},
	DropsHiddenKey: "ignoreinvisible",
	// a data binding is called by its own name, as a customizable
	// localization is
	StepDatabases: []string{"data_binding", CustomLocalization},
	CallTargets: map[string][]string{
		guiDefineCall: {"defines", "defines.define"},
		// the dumps' AddLocalizationIf expansion calls it
		"Localize": {Localization},
		// the dumps' expansion is AddLocalizationIf(Condition, LocKey)
		"AddLocalizationIf": {"", Localization},
		"Concept":           {"game_concepts"},
		// CK3 gui/shared/mapmodes.gui:26, [SetMapMode('baronies')]
		"SetMapMode": {"gfx/map/map_modes"},
		// EU5 customizable_localization.info:25
		"Custom":    {CustomLocalization},
		"GetCustom": {CustomLocalization},
		// CK3's data_types declare Character.Custom2( Arg0, Arg1 ); each of
		// its 5,455 calls names one by the first
		"Custom2": {CustomLocalization},
	},
	DefineCall: guiDefineCall,
	Booleans:   map[string]string{"And": "and", "Or": "or", "Nor": "nor", "Not": "not"},
	// no GUI file of the three installs writes a property as true or false
	Literals: map[string]bool{"yes": true, "no": false},
	// CK3 gui/shared/lists.gui:2671, LessThanOrEqualTo_int32(
	// GetDataModelSize(TraitArrays.GetPersonalityTraits), …)
	Comparisons: map[string][]int{
		"LessThan": {-1}, "LessThanOrEqualTo": {-1, 0}, "GreaterThan": {1}, "GreaterThanOrEqualTo": {0, 1},
		"EqualTo": {0}, "NotEqualTo": {-1, 1},
	},
	ModelSize:  "GetDataModelSize",
	ModelEmpty: "IsDataModelEmpty",
	// CK3 gui/window_activity.gui, EU5 and Vic3 the same
	ModelHas:       "DataModelHasItems",
	IconPathFile:   "path_overrides.txt",
	PictureKeys:    []string{"icon", "texture"},
	IconPathSuffix: "_ICON_PATH",
	// CK3 common/defines/graphic/00_graphics.txt:814-817,
	// GOVERNMENT_TYPE_REALM_FRAME_PATH and _SUFFIX; EU5
	// loading_screen/common/defines/graphic/00_graphics.txt:836,
	// GOODS_ICON_PREFIX
	FolderDefineSuffix: "_PATH",
	PrefixDefineSuffix: "_PREFIX",
	NameDefineSuffix:   "_SUFFIX",
	// CK3 gfx/interface/icons/realm_frames/_default_62_frame.dds, drawn for
	// frameless feudal_government
	DefaultPicture: "_default",
	// CK3 common/defines/graphic/00_graphics.txt:846-851, PRESTIGE_LEVEL_PATH
	// and the piety, influence and merit ones, name
	// gfx/interface/icons/modifiers, where the files are icon_prestige_00 to
	// _05 and icon_piety_christian_00
	Levels: Levels{Ending: "level", Prefix: "icon_"},
	// a file's first entry is a window in 88% of CK3's GUI files with one,
	// 57% of Vic3's and 52% of EU5's
	ShownFirst: "gui.widget",
}

// Placeholders are the shapes drawn where a picture has no file, a game's own
// first. The last is the plain icon, for a picture no term fits.
var Placeholders = []Placeholder{
	{Games: []string{CK3}, Shape: "crowned-ruler", Terms: []string{"portrait texture"}, Runtime: true},
	{Games: []string{CK3}, Shape: "shield", Terms: []string{"coat of arms"}, Runtime: true},
	{Games: []string{Vic3}, Shape: "top-hat", Terms: []string{"portrait texture"}, Runtime: true},
	{Games: []string{EU5}, Shape: "plumed-hat", Terms: []string{"portrait texture"}, Runtime: true},
	{Games: vic3AndEU5, Shape: "flag", Terms: []string{"coat of arms"}, Runtime: true},
	{Shape: "illustration", Terms: []string{"illustration", "image", "picture"}},
	{Shape: "flag", Terms: []string{"flag"}},
	{Shape: "faith", Terms: []string{"faith", "religion", "religious", "rite", "tenet", "doctrine"}},
	{Shape: "goods", Terms: []string{"goods", "resource"}},
	{Shape: "action", Terms: []string{"action", "actions", "interaction", "task"}},
	{Shape: "military", Terms: []string{"unit", "formation", "battle", "war", "military"}},
	{Shape: "building", Terms: []string{"building", "holding", "domicile", "construction", "great project"}},
	{Shape: "trait", Terms: []string{"trait", "perk", "role", "tradition", "pillar"}},
	{Shape: "modifier", Terms: []string{"modifier"}},
	{Shape: "concept", Terms: []string{"concept", "encyclopedia", "article"}},
	{Shape: "rank", Terms: []string{"rank", "level", "status"}},
	{Shape: "map", Terms: []string{"map", "terrain", "strait", "location"}},
	{Shape: "message", Terms: []string{"message", "alert", "notification"}},
	{Shape: "icon"},
}

// FindPlaceholder returns the first placeholder of the game with a term that a
// phrase holds whole, such as "coat of arms". The phrase says what the picture
// is of. It returns the plain icon if no term fits.
func (g *Game) FindPlaceholder(phrase string) Placeholder {
	padded := " " + phrase + " "
	for _, placeholder := range Placeholders {
		isNamed := g.Matches(placeholder.Games) && slices.ContainsFunc(placeholder.Terms, func(term string) bool {
			return strings.Contains(padded, " "+term+" ")
		})
		if isNamed {
			return placeholder
		}
	}

	return Placeholders[len(Placeholders)-1]
}
