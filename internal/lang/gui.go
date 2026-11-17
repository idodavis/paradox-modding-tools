// gui.go holds the fixed keys and names of GUI files and of coats of arms.

package lang

// WidgetType is how one of the game's built-in widget types lays out the
// widgets inside it and its own text.
type WidgetType struct {
	// Holding is how it places the widgets inside it: row, column, flow, grid
	// or scroll. It is "" where each is placed by its own anchors.
	Holding string `json:"holding,omitempty"`

	// Clips is whether the widget hides what reaches outside it. Fills is
	// whether it takes all the room of the widget it is in.
	Clips bool `json:"clips,omitempty"`
	Fills bool `json:"fills,omitempty"`

	// KeepsSize is whether the widget is as large as it writes, whatever
	// the widgets inside it take.
	KeepsSize bool `json:"keepsSize,omitempty"`

	CentersText bool `json:"centersText,omitempty"`

	// DropsHidden is whether a hidden widget inside it takes no room.
	// KeepsHidden is whether a hidden widget inside it keeps its room
	// whatever the file writes.
	DropsHidden bool `json:"dropsHidden,omitempty"`
	KeepsHidden bool `json:"keepsHidden,omitempty"`
}

// GUITypeKey is the key that declares a widget type, as
// type x = button { } does.
const GUITypeKey = "type"

// GUIBlockKey is the key that declares a named block, as block "x" { }. A
// widget of the type or template the block is in may replace it with
// blockoverride and the same name.
const GUIBlockKey = "block"

// GUIDeclares are the keys that declare a definition before a block at the
// top of a GUI file. Its database is GUIDatabasePrefix and the key, as
// gui.template. The same keys open a group of definitions, as types x { }.
var GUIDeclares = []string{GUITypeKey, "local_type", "template", "local_template", GUIBlockKey, "layer"}

// GUINameKeyOf holds the keys whose block is a definition wherever it is
// written, each with the key inside the block that names it.
var GUINameKeyOf = map[string]string{"texticon": "icon", "format": "name"}

// GUIWidgetKey is the key that names any other block at the top of a file.
// A file's only such block, if it writes no name, is named after the file.
const GUIWidgetKey = "name"

// GUIDatabasePrefix is what the name of a GUI database starts with. The key
// that declares the database's definitions follows it.
const GUIDatabasePrefix = "gui."

// GUIWidgetDatabase is the database of the top blocks of a file that no key
// of GUIDeclares declares. GUIWidgetTypes is the database of the widget
// types no file declares: the game's built-in ones, and those its files
// build their types on.
const (
	GUIWidgetDatabase = "gui.widget"
	GUIWidgetTypes    = "gui.widget_type"
)

// GUITagCalls is the kind of caller a block's key is when it names a widget
// type.
const GUITagCalls = "widget"

// GUITemplateCalls is the key that applies a template, as using = x does.
// GUIBlockCalls is the key that replaces a named block that a template or a
// type declares.
const (
	GUITemplateCalls = "using"
	GUIBlockCalls    = "blockoverride"
)

// GUIContextKey is the key that sets the data context of its block: the
// object that the data calls written in the block start from.
const GUIContextKey = "datacontext"

// GUITextFormats is the database of the formats a text applies with #, as
// #bold does. GUITextIcons is the database of the icons a text writes as
// @icon!.
const (
	// gui/preload/textformatting.gui:6-7
	GUITextFormats = "gui.format"
	// gui/debug/placeholder_types_templates.gui:346-347
	GUITextIcons = "gui.texticon"
)

// GUIEditorFolders are the folders of the game's own GUI editor. Its files
// call functions no dump lists, so nothing in them is reported.
var GUIEditorFolders = []string{
	// clausewitz/gui/gui_editor, and EU5's loading_screen stage
	"gui/gui_editor", "loading_screen/gui/gui_editor",
}

// BuiltinWidgets holds the built-in widget types that place things in a way
// of their own. Any other type places the widgets inside it by their
// anchors, and keeps the room of a hidden one.
var BuiltinWidgets = map[string]WidgetType{
	"widget":         {KeepsSize: true},
	"uberwidget":     {KeepsSize: true},
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
}

// GUIFontKey is the key of a widget whose value names its font.
// GUIFontDatabase is the database of fonts.
const (
	// CK3 and Vic3 fonts/fonts.font, EU5 in_game/fonts/in_game_fonts.font
	GUIFontKey      = "font"
	GUIFontDatabase = "font.font"
)

// GUIGetters are the words a data function's name starts with when it only
// leads to an object, as Get in GetPlayer.
var GUIGetters = []string{"Get", "Access"}

// GUINoTooltip is how a data function's name ends when it does the same as
// the function without it but turns off the tooltip.
const GUINoTooltip = "NoTooltip"

// GUIStepDatabases are the databases whose definitions a data call writes
// by name as one of its steps, though no dump lists them as functions.
var GUIStepDatabases = []string{
	// a data binding is called by its own name, as a customizable
	// localization is
	"data_binding", CustomLocalization,
}

// GUIDefineCall is the data function that reads a define. Its row in
// GUICallTargets gives the databases of the defines.
const GUIDefineCall = "GetDefine"

// GUICallTargets holds the data functions whose quoted arguments are names,
// each with the database of each argument in order, "" for one that is no
// name.
var GUICallTargets = map[string][]string{
	GUIDefineCall: {"defines", "defines.define"},
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
	// CK3's Character.Custom2(Arg0, Arg1) names one by its first argument
	"Custom2": {CustomLocalization},
}

// CoatDatabase is the database of coats of arms. CoatColorsDatabase is the
// database of the colors a coat names. CoatListsDatabase is the database of
// the lists a coat names with list "x".
const (
	CoatDatabase       = "coat_of_arms/coat_of_arms"
	CoatColorsDatabase = "named_colors"
	CoatListsDatabase  = "coat_of_arms/template_lists"
)

// CoatTextureFolders holds, for each key of a coat that names a texture,
// the folder of the texture's files.
var CoatTextureFolders = map[string]string{
	"pattern":         "gfx/coat_of_arms/patterns",
	"colored_emblem":  "gfx/coat_of_arms/colored_emblems",
	"textured_emblem": "gfx/coat_of_arms/textured_emblems",
}

// CoatSlots are the keys of the colors a coat or one of its layers writes,
// in order. The game draws a coat with the first three.
var CoatSlots = []string{
	// an emblem names a coat's color4 (EU5 00_random_countries.txt:30-31,69,
	// Vic3 01_subs.txt:235)
	"color1", "color2", "color3", "color4", "color5",
}

// CoatPatternKey is the key of a coat's pattern, which is drawn under every
// emblem and has no instances.
const CoatPatternKey = "pattern"

// CoatTextureKey is the key of an emblem's texture.
const CoatTextureKey = "texture"
