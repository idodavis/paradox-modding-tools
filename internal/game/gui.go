// gui.go holds the rules for GUI files that every game shares: the words that declare databases, the keys that name
// widgets, how each built-in type draws, how a definition names its picture, and the shapes drawn where a picture has
// no file. What differs by game is on the game's row.

package game

import (
	"slices"
	"strings"
)

// A WidgetType is how a built-in widget type lays its children out and draws its own text.
type WidgetType struct {
	// Holding is how it holds its children: row, column, flow, grid or scroll; "" means by their anchors.
	Holding     string `json:"holding,omitempty"`
	Clips       bool   `json:"clips,omitempty"`       // it clips them
	Fills       bool   `json:"fills,omitempty"`       // it fills its holder
	CentersText bool   `json:"centersText,omitempty"` // its own text is centered
	DropsHidden bool   `json:"dropsHidden,omitempty"` // a hidden child of it takes no room
	KeepsHidden bool   `json:"keepsHidden,omitempty"` // a hidden child of it keeps its room whatever the widget writes
}

// A WidgetSlot is a key whose block is a widget that no type names.
type WidgetSlot struct {
	// Role is what the block is to its widget: skin (a background), chrome (a scroll bar), attachment (a tooltip) or
	// label (its text).
	Role string
	Type string // the built-in type the block is drawn as; "" means the block's own
}

// An Animation is how one state of a widget leads to others: the key that names the state run after it, the key of
// the calls it runs as it ends, the key of the widget's alpha, and the data functions that run a state by its name, on
// the widget itself or on a child found by its name.
type Animation struct {
	NextKey   string
	FinishKey string
	AlphaKey  string
	Trigger   string
	FindChild string
}

// A VariableSystem is the GUI's own variables: the data type that holds them, the functions that ask them and the
// functions a click runs on them. Each function takes the variable's name first.
type VariableSystem struct {
	Type         string // the data type the variables are reached through
	Exists       string // holds while a variable is set
	HasValue     string // holds while a variable is set to the given value
	UnsetOrValue string // holds while a variable is unset or set to the given value
	Set          string // sets a variable, to the given value or to none
	Clear        string // unsets a variable
	Toggle       string // sets a variable that is unset and unsets one that is set
}

// FontRules is how a font file lays out the files of a font. A font has a block for each of its styles, which names a
// definition of files. That definition has groups of languages, each with the files those languages read, the font's
// own file first.
type FontRules struct {
	Key     string // the key of a widget that names its font
	SizeKey string // the key of a widget that sets the size of its text
	// TextType is the built-in type of a text, whose own type in the game's files gives a text with no font of its
	// own its font and size.
	TextType      string
	Database      string // the database of fonts
	StyleKey      string // the key of a font's block for one of its styles
	StyleWords    string // the key in that block of the style's words, such as bold|italic
	FilesKey      string // the key in that block that names the definition of the style's files
	FilesDatabase string // the database of those definitions
	GroupKey      string // the key of a definition's block for a group of languages
	LanguagesKey  string // the key in a group of its languages
	PathsKey      string // the key in a group of its files
}

// DateCalls is how a data call in a text that writes a date is known: the type its last function returns, and the
// endings of that function's name.
type DateCalls struct {
	Returns string
	Endings []string
}

// A DateForm is how the games write a date: the localization key of the form, the names the form fills in for the
// day, the month and the year, and the localization keys of the months' names, January first.
type DateForm struct {
	Key              string
	Day, Month, Year string
	Months           []string
}

// TierFrames is how a game draws the tier of a title from a sheet of cells, one cell a tier: the data function that
// gives a title's cell, counted from one, and the tiers in the order of the cells.
type TierFrames struct {
	Function string
	Tiers    []string
}

// A Placeholder is a shape PMT draws where a picture has no file, for the kinds of object it stands for. The shape is
// a file among the page's placeholders, and the page tints each in a color of its own.
type Placeholder struct {
	Shape   string
	Kinds   []string // the kinds as keys, data types and data functions write them, in lower-case words ("coat of arms")
	Runtime bool     // only the running game draws its pictures, from what it holds, so no file is looked for
}

// Illustrations is where the pictures are that a definition names by a path below a folder: the key that names the
// folder, and the folder where the definition names none. A picture written in a block with the trigger key is drawn
// only where its condition holds; the others are the fallback.
type Illustrations struct {
	FolderKey  string
	Folder     string
	TriggerKey string
}

// Levels is where the pictures are of a kind of level, one for each level: the last word of the kind, which says it is
// a level, and the start of each file's name before the kind's other words. The folder is the kind's define, with the
// folder define's ending.
type Levels struct {
	Word   string
	Prefix string
}

// GUIRules is the rules for GUI files beyond their syntax. GUI is its one row.
type GUIRules struct {
	// Declares is the words before a top block that declare a definition of gui.<word>, or group definitions
	// (types x { }).
	Declares  []string
	NameKeyOf map[string]string // a block key at any depth, and the key whose value names its definition
	// WidgetKey is the key that names every other top block, a definition of WidgetDatabase. The file's only block
	// written under a key that writes none is named after the file.
	WidgetKey      string
	DatabasePrefix string      // what a GUI database's name starts with, before the word that declares it
	WidgetDatabase string      // the database of every top block that no declaring word declares
	TagCalls       string      // the word that calls the tag a block is written with
	TemplateCalls  string      // the word that calls a template
	BlockCalls     string      // the word that calls the block a template or type leaves for a widget to replace
	ContextKey     string      // the key whose value is the data call its block's bindings read in
	References     []Reference // what a widget's keys name, Database "" for any
	TextFormats    string      // the database a text's #format names
	TextIcons      string      // the database an icon in a text, @icon!, names
	Slots          map[string]WidgetSlot
	// Types is the row of each built-in widget type. Any other type holds its children by their anchors and keeps a
	// hidden child's room.
	Types           map[string]WidgetType
	OpeningStates   []string          // the states run by name as a window opens
	OpeningTriggers map[string]string // the states run as a window opens by a key's value
	StateKeys       []string          // the keys of a state that are its own rather than end values
	OpeningCalls    string            // the key of a state whose calls run as the state starts
	Animation       Animation
	// RepeatedKeys is the keys a widget keeps every value of, in the order written, each adding to the ones before.
	RepeatedKeys []string
	// TextKeys is the keys that write a widget's one text, localized or as written. The last one written wins.
	TextKeys     []string
	MultilineKey string // the key whose yes lets a widget's text run over several lines
	ResizeKey    string // the key whose yes resizes a widget to fit what it holds
	// CallKinds is what a data call in a text shows, by the type its last function returns: number or text. A type
	// with no row shows two words of text. A call that DateCalls knows shows a date. draw's sampleCall reads the
	// words, number, text and date, and writes each sample's word in its loc.Segment.Kind, by which placeholders.ts
	// colors it.
	CallKinds map[string]string
	DateCalls DateCalls
	DateForm  DateForm
	// PercentCode and SignCode are format codes a data call in a text writes after |. The first shows its number as a
	// percent, and the second shows a plus sign before a number above zero. A digit among the codes is how many
	// decimals the number has.
	PercentCode string
	SignCode    string
	ColorType   string // the data type that a binding of a color returns
	// SelectPrefix is the start of the name of the data functions that give their second argument where their first
	// holds, else their third: Select_float, Select_CString and the rest.
	SelectPrefix string
	ClickKeys    []string // the keys whose calls run as a widget is clicked
	// DownKey is the key whose condition holds while a button is drawn down. Two or more buttons of one type side by
	// side, each written with it, are a strip of tabs.
	DownKey       string
	CheckedKey    string // the key whose condition holds while a box is drawn checked
	ValidFunction string // the data function that asks whether an object exists
	// OpeningFacts is the data functions, called with no arguments, that hold as a window opens whatever it is about:
	// an object exists, or a list has the items the window draws.
	OpeningFacts []string
	// SampleFacts is the data functions, called with no arguments, that hold of the sample the Visual view draws: the
	// player's own living, landed ruler, who shows their legitimacy and sits on a council of councilors they may
	// replace.
	SampleFacts []string
	// SampleAbsent is the data functions that reach an object the sample lacks, so that an existence test of it, or of
	// anything past it, fails.
	SampleAbsent []string
	Variables    VariableSystem
	PortraitKey  string // the key whose binding draws a character's portrait
	CoatKey      string // the key whose binding draws a coat of arms
	// MaskKeys is the keys whose image cuts a widget's picture to its shape: a coat of arms' mask, and a widget's own.
	MaskKeys []string
	Fonts    FontRules
	Getters  []string // the words a data function that only reaches an object starts with
	// HiddenKeys is the keys whose no hides a widget. The first one a widget writes decides: visible_at_creation only
	// says how a widget starts until its visible condition is read.
	HiddenKeys     []string
	DropsHiddenKey string // the key whose yes makes a hidden child of a widget take no room
	// CallTargets is the data functions whose quoted arguments are names, by function: the database each argument's
	// names are in, in order, and "" for one that is no name.
	CallTargets map[string][]string
	// Booleans is the data functions a binding combines conditions with, by name without the argument count.
	Booleans map[string]string
	Literals map[string]bool // a condition's plain values, in lower case, and whether each holds
	// Comparisons is the data functions that compare two numbers, by name without the type after the underscore:
	// the results of comparing the first with the second, as cmp.Compare gives them, for which each holds.
	Comparisons    map[string][]int
	ModelSize      string   // the data function that gives how many items a data model has
	ModelEmpty     string   // the data function that asks whether a data model has no items
	ModelHas       string   // the data function that asks whether a data model has items
	IconPathFile   string   // the file in an icon folder that names the paths its icons are read from
	PictureKeys    []string // the keys a definition names its picture by, tried in this order
	IconPathSuffix string   // the ending of the defines that name a folder of icons
	// FolderDefineSuffix, PrefixDefineSuffix and NameDefineSuffix end the defines that say where a data function finds
	// its pictures, after the kind of object and the field it reads: the folder, the start of each file's name and its
	// ending.
	FolderDefineSuffix string
	PrefixDefineSuffix string
	NameDefineSuffix   string
	Levels             Levels
	// DefaultPicture is the name in such a folder of the picture drawn for a definition with none of its own.
	DefaultPicture string
	// ShownFirst is the database whose first definition in a GUI file is the one the file shows first, wherever the
	// file writes it: its main window, which CK3 writes at the top and EU5 after its types.
	ShownFirst string
}

// The words that call a type, a template and a block override, which GUI and the callable databases both name,
// the key that names a widget, and the key that names a widget's font with the database of fonts.
const (
	guiFontKey       = "font"
	guiFontDatabase  = "font.font"
	guiWidgetKey     = "name"
	guiTagCalls      = "widget"
	guiTemplateCalls = "using"
	guiBlockCalls    = "blockoverride"
)

// GUI is what every game does with a GUI file.
var GUI = GUIRules{
	Declares:       []string{"type", "local_type", "template", "local_template", "block", "layer"},
	NameKeyOf:      map[string]string{"texticon": "icon", "format": "name"},
	WidgetKey:      guiWidgetKey,
	DatabasePrefix: "gui.",
	WidgetDatabase: "gui.widget",
	TagCalls:       guiTagCalls,
	TemplateCalls:  guiTemplateCalls,
	BlockCalls:     guiBlockCalls,
	ContextKey:     "datacontext",
	References: []Reference{
		{KeyPath: guiTemplateCalls, Target: "gui.template"}, {KeyPath: "layer", Target: "gui.layer"},
		{KeyPath: "texture", Target: File}, {KeyPath: guiFontKey, Target: guiFontDatabase},
		{KeyPath: "text", Target: Localization}, {KeyPath: "title", Target: Localization},
	},
	TextFormats: "gui.format",   // gui/preload/textformatting.gui:6-7
	TextIcons:   "gui.texticon", // gui/debug/placeholder_types_templates.gui:346-347
	Slots: map[string]WidgetSlot{
		"background":           {Role: "skin", Type: "background"},
		"tooltipwidget":        {Role: "attachment"},
		"scrollbar_vertical":   {Role: "chrome", Type: "scrollbar"},
		"scrollbar_horizontal": {Role: "chrome", Type: "scrollbar"},
		"buttontext":           {Role: "label"},
		"scrollwidget":         {Type: "widget"},
		"zoomwidget":           {Type: "widget"},
	},
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
	StateKeys: []string{
		guiWidgetKey, "duration", "next", "on_start", "on_finish", "trigger_on_create", "delay", "bezier", "loop", "animation",
		"restart_on_show", "skip_initial_animation", "trigger_when", "start_sound", "end_sound", "sstart_sound",
		"animation_speed", "loopinterval",
	},
	OpeningCalls: "on_start",
	// Vic3 gui/achievements/popup.gui: [PdxGuiWidget.FindChild('achievements_popup_icon').TriggerAnimation('1')]
	Animation: Animation{
		NextKey: "next", FinishKey: "on_finish", AlphaKey: "alpha", Trigger: "TriggerAnimation", FindChild: "FindChild",
	},
	RepeatedKeys: []string{"datacontext", "onclick", "on_start"},
	TextKeys:     []string{"text", "raw_text"}, // the wiki: raw_text is a text that is not localized
	MultilineKey: "multiline",                  // the wiki's Interface page
	ResizeKey:    "autoresize",                 // the wiki's Interface page
	// the dumps' Return type of the last function of every data call in the three installs' GUI texts;
	// CPdxStringView is EU5's alone and int64 Vic3's
	CallKinds: map[string]string{
		"CString": "text", "CUTF8String": "text", "CPdxStringView": "text",
		"int32": "number", "int64": "number", "uint32": "number", "uint64": "number", "CFixedPoint": "number",
		"float": "number", "double": "number",
	},
	// no data call in the three installs' GUI texts returns the dumps' Date type; the calls that write a date return
	// CString, such as EU5's GetEndDate and GetDateString, so a name with one of these endings is taken to write one
	DateCalls: DateCalls{Returns: "CString", Endings: []string{"Date", "DateString"}},
	// jomini/localization/jomini/date_l_english.yml:4 and clausewitz/localization/cw_date_l_english.yml, the same in
	// the three installs (EU5's under loading_screen/)
	DateForm: DateForm{
		Key: "DATE_FORMATTING_LONG", Day: "DAY", Month: "MONTH", Year: "YEAR",
		Months: []string{
			"CW_DATE_January", "CW_DATE_February", "CW_DATE_March", "CW_DATE_April", "CW_DATE_May", "CW_DATE_June",
			"CW_DATE_July", "CW_DATE_August", "CW_DATE_September", "CW_DATE_October", "CW_DATE_November",
			"CW_DATE_December",
		},
	},
	// each game's English localization, by keys named for the formats they write: CK3 core_l_english.yml:493
	// KEY_VALUE_PERCENT $VALUE|V0%$ and :830 BASE_VALUE_FORMAT_DECIMALS $VALUE|V2$; Vic3 interfaces_l_english.yml:20
	// VALUE_PERCENT_GOOD $VALUE|%0$ and :2040 FORMATION_COMMANDER_STAT_DECIMAL_BASE_ENTRY $VALUE|v1$; EU5
	// diplomacy_l_english.yml:380-383 TWO_DP_ENTRY_FORMAT $VALUE|+=2$, _PERCENT $VALUE|+2%$ and _MULTIPLIER_SIGNED
	// $VALUE|=$. CK3 core_l_english.yml:831 writes its own plus before $VALUE|2$.
	PercentCode:   "%",
	SignCode:      "=",
	ColorType:     "CVector4f", // the dumps' Return type of GetColor in the three installs
	SelectPrefix:  "Select_",   // the dumps' global functions Select_float( Arg0, Arg1, Arg2 ) and the rest
	ClickKeys:     []string{"onclick"},
	DownKey:       "down",
	CheckedKey:    "checked",
	ValidFunction: "IsValid",
	// CK3 gui/window_council.gui:465 and :484 (IsValid), gui/window_character_finder.gui:51 (HasItems)
	OpeningFacts: []string{"IsValid", "HasItems"},
	// the owner's CK3 character-window.png shows a living ruler with soldiers and legitimacy 2/5
	// (gui/window_character.gui:5440 IsAlive, :5722 HasLandedTitles, :5633 ShouldShowLegitimacy), EU5's event
	// portraits are living (in_game/gui/shared/cards.gui:1038 and :1054), and council-window.png shows each seat's
	// replace button and main skill (gui/window_council.gui:609 CanPotentiallySelectCouncillor, :662 HasMainSkill, :671
	// HasCouncillor)
	SampleFacts: []string{
		"IsAlive", "HasLandedTitles", "ShouldShowLegitimacy", "CanPotentiallySelectCouncillor", "HasMainSkill",
		"HasCouncillor", "IsVisible", "CanSelect",
	},
	// council-window.png shows no forced next councilor, which window_council.gui:925 marks with a debug square
	SampleAbsent: []string{"GetNextForcedCouncillor"},
	Variables: VariableSystem{ // EU5's data_types_uncategorized.txt declares NotExistOrHasValue( Arg0, Arg1 )
		Type: "VariableSystem", Exists: "Exists", HasValue: "HasValue", UnsetOrValue: "NotExistOrHasValue", Set: "Set",
		Clear: "Clear", Toggle: "Toggle",
	},
	PortraitKey: "portrait_texture",
	CoatKey:     "coat_of_arms", // CK3 gui/shared/coat_of_arms.gui
	// CK3 gui/shared/coat_of_arms.gui:128 [GovernmentType.GetRealmMask]; Vic3 gui/company_panel.gui's portrait
	MaskKeys: []string{"coat_of_arms_mask", "mask"},
	Fonts: FontRules{ // CK3 and Vic3 fonts/fonts.font, EU5 in_game/fonts/in_game_fonts.font
		Key: guiFontKey, SizeKey: "fontsize", TextType: "textbox", Database: guiFontDatabase, StyleKey: "fontstyle",
		StyleWords: "style", FilesKey: "fontfiles",
		FilesDatabase: "font.fontfiles", GroupKey: "group", LanguagesKey: "languages", PathsKey: "files",
	},
	Getters:        []string{"Get", "Access"},
	HiddenKeys:     []string{"visible", "visible_at_creation"},
	DropsHiddenKey: "ignoreinvisible",
	CallTargets: map[string][]string{
		"GetDefine":         {"defines", "defines.define"},
		"Localize":          {Localization},     // the dumps' AddLocalizationIf expansion calls it
		"AddLocalizationIf": {"", Localization}, // the dumps' expansion is AddLocalizationIf(Condition, LocKey)
		"Concept":           {"game_concepts"},
		"SetMapMode":        {"gfx/map/map_modes"},         // CK3 gui/shared/mapmodes.gui:26, [SetMapMode('baronies')]
		"Custom":            {"customizable_localization"}, // EU5 customizable_localization.info:25
		"GetCustom":         {"customizable_localization"}, // EU5 customizable_localization.info:25
	},
	Booleans: map[string]string{"And": "and", "Or": "or", "Nor": "nor", "Not": "not"},
	Literals: map[string]bool{"yes": true, "true": true, "no": false, "false": false},
	// CK3 gui/shared/lists.gui:2671, LessThanOrEqualTo_int32(GetDataModelSize(TraitArrays.GetPersonalityTraits), …)
	Comparisons: map[string][]int{
		"LessThan": {-1}, "LessThanOrEqualTo": {-1, 0}, "GreaterThan": {1}, "GreaterThanOrEqualTo": {0, 1},
		"EqualTo": {0}, "NotEqualTo": {-1, 1},
	},
	ModelSize:      "GetDataModelSize",
	ModelEmpty:     "IsDataModelEmpty",
	ModelHas:       "DataModelHasItems", // CK3 gui/window_activity.gui, EU5 and Vic3 the same
	IconPathFile:   "path_overrides.txt",
	PictureKeys:    []string{"icon", "texture"},
	IconPathSuffix: "_ICON_PATH",
	// CK3 common/defines/graphic/00_graphics.txt:814-817, GOVERNMENT_TYPE_REALM_FRAME_PATH and _SUFFIX; EU5
	// loading_screen/common/defines/graphic/00_graphics.txt:836, GOODS_ICON_PREFIX
	FolderDefineSuffix: "_PATH",
	PrefixDefineSuffix: "_PREFIX",
	NameDefineSuffix:   "_SUFFIX",
	// CK3 gfx/interface/icons/realm_frames/_default_62_frame.dds, which feudal_government, with no frames of its own,
	// draws in the game
	DefaultPicture: "_default",
	// CK3 common/defines/graphic/00_graphics.txt:846-851, PRESTIGE_LEVEL_PATH and the piety, influence and merit ones,
	// name gfx/interface/icons/modifiers, where the files are icon_prestige_00 to _05 and icon_piety_christian_00
	Levels: Levels{Word: "level", Prefix: "icon_"},
	// a file's first entry is a window in 88% of CK3's GUI files with one, 57% of Vic3's and 52% of EU5's
	ShownFirst: "gui.widget",
}

// Placeholders are the shapes every game draws where a picture has no file, tried in order after the game's own. The
// last is the plain icon, which stands for any picture of no listed kind.
var Placeholders = []Placeholder{
	{Shape: "illustration", Kinds: []string{"illustration", "image", "picture"}},
	{Shape: "flag", Kinds: []string{"flag"}},
	{Shape: "faith", Kinds: []string{"faith", "religion", "religious", "rite", "tenet", "doctrine"}},
	{Shape: "goods", Kinds: []string{"goods", "resource"}},
	{Shape: "action", Kinds: []string{"action", "actions", "interaction", "task"}},
	{Shape: "military", Kinds: []string{"unit", "formation", "battle", "war", "military"}},
	{Shape: "building", Kinds: []string{"building", "holding", "domicile", "construction", "great project"}},
	{Shape: "trait", Kinds: []string{"trait", "perk", "role", "tradition", "pillar"}},
	{Shape: "modifier", Kinds: []string{"modifier"}},
	{Shape: "concept", Kinds: []string{"concept", "encyclopedia", "article"}},
	{Shape: "rank", Kinds: []string{"rank", "level", "status"}},
	{Shape: "map", Kinds: []string{"map", "terrain", "strait", "location"}},
	{Shape: "message", Kinds: []string{"message", "alert", "notification"}},
	{Shape: "icon"},
}

// FindPlaceholder returns the placeholder of a picture, given a phrase that says what the picture is of: the first
// placeholder, the game's own first, one of whose kinds the phrase holds as whole words. It returns the plain icon
// where the phrase holds none.
func (game *Game) FindPlaceholder(phrase string) Placeholder {
	padded := " " + phrase + " "
	all := slices.Concat(game.Placeholders, Placeholders)
	for _, placeholder := range all {
		isNamed := slices.ContainsFunc(placeholder.Kinds, func(kind string) bool {
			return strings.Contains(padded, " "+kind+" ")
		})
		if isNamed {
			return placeholder
		}
	}
	return all[len(all)-1]
}
