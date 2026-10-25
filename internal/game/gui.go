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
	Holding     string // how it holds its children: row, column, flow, grid or scroll; "" means by their anchors
	Clips       bool   // it clips them
	Fills       bool   // it fills its holder
	CentersText bool   // its own text is centered
	DropsHidden bool   // a hidden child of it takes no room
	KeepsHidden bool   // a hidden child of it keeps its room whatever the widget writes
}

// A WidgetSlot is a key whose block is a widget that no type names.
type WidgetSlot struct {
	// Role is what the block is to its widget: skin (a background), chrome (a scroll bar), attachment (a tooltip) or
	// label (its text).
	Role string
	Type string // the built-in type the block is drawn as; "" means the block's own
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
	Key           string // the key of a widget that names its font
	Database      string // the database of fonts
	StyleKey      string // the key of a font's block for one of its styles
	StyleWords    string // the key in that block of the style's words, such as bold|italic
	FilesKey      string // the key in that block that names the definition of the style's files
	FilesDatabase string // the database of those definitions
	GroupKey      string // the key of a definition's block for a group of languages
	LanguagesKey  string // the key in a group of its languages
	PathsKey      string // the key in a group of its files
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
	// RepeatedKeys is the keys a widget keeps every value of, in the order written, each adding to the ones before.
	RepeatedKeys []string
	// TextKeys is the keys that write a widget's one text, localized or as written. The last one written wins.
	TextKeys []string
	// SelectPrefix is the start of the name of the data functions that give their second argument where their first
	// holds, else their third: Select_float, Select_CString and the rest.
	SelectPrefix string
	ClickKeys    []string // the keys whose calls run as a widget is clicked
	// DownKey is the key whose condition holds while a button is drawn down. Two or more buttons of one type side by
	// side, each written with it, are a strip of tabs.
	DownKey       string
	CheckedKey    string // the key whose condition holds while a box is drawn checked
	ValidFunction string // the data function that asks whether an object exists
	Variables     VariableSystem
	PortraitKey   string // the key whose binding draws a character's portrait
	CoatKey       string // the key whose binding draws a coat of arms
	Fonts         FontRules
	Getters       []string // the words a data function that only reaches an object starts with
	// HiddenKeys is the keys whose no hides a widget. The first one a widget writes decides: visible_at_creation only
	// says how a widget starts until its visible condition is read.
	HiddenKeys     []string
	DropsHiddenKey string // the key whose yes makes a hidden child of a widget take no room
	// CallTargets is the data functions whose quoted arguments are names, by function: the database each argument's
	// names are in, in order, and "" for one that is no name.
	CallTargets map[string][]string
	// Booleans is the data functions a binding combines conditions with, by name without the argument count.
	Booleans       map[string]string
	Literals       map[string]bool // a condition's plain values, in lower case, and whether each holds
	IconPathFile   string          // the file in an icon folder that names the paths its icons are read from
	PictureKeys    []string        // the keys a definition names its picture by, tried in this order
	IconPathSuffix string          // the ending of the defines that name a folder of icons
	// FolderDefineSuffix, PrefixDefineSuffix and NameDefineSuffix end the defines that say where a data function finds
	// its pictures, after the kind of object and the field it reads: the folder, the start of each file's name and its
	// ending.
	FolderDefineSuffix string
	PrefixDefineSuffix string
	NameDefineSuffix   string
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
	OpeningCalls:  "on_start",
	RepeatedKeys:  []string{"datacontext", "onclick", "on_start"},
	TextKeys:      []string{"text", "raw_text"}, // the wiki: raw_text is a text that is not localized
	SelectPrefix:  "Select_",                    // the dumps' global functions Select_float( Arg0, Arg1, Arg2 ) and the rest
	ClickKeys:     []string{"onclick"},
	DownKey:       "down",
	CheckedKey:    "checked",
	ValidFunction: "IsValid",
	Variables: VariableSystem{ // EU5's data_types_uncategorized.txt declares NotExistOrHasValue( Arg0, Arg1 )
		Type: "VariableSystem", Exists: "Exists", HasValue: "HasValue", UnsetOrValue: "NotExistOrHasValue", Set: "Set",
		Clear: "Clear", Toggle: "Toggle",
	},
	PortraitKey: "portrait_texture",
	CoatKey:     "coat_of_arms", // CK3 gui/shared/coat_of_arms.gui
	Fonts: FontRules{ // CK3 and Vic3 fonts/fonts.font, EU5 in_game/fonts/in_game_fonts.font
		Key: guiFontKey, Database: guiFontDatabase, StyleKey: "fontstyle", StyleWords: "style", FilesKey: "fontfiles",
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
	Booleans:       map[string]string{"And": "and", "Or": "or", "Nor": "nor", "Not": "not"},
	Literals:       map[string]bool{"yes": true, "true": true, "no": false, "false": false},
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
