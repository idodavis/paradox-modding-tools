// gui.go holds what a game does with a GUI file beyond its syntax: the roles of a widget's blocks, how each built-in
// type draws, where a window is laid out, the state it opens in, the calls the preview answers and how an entry names
// its picture. Each row says whether the wiki's Interface page, the game's shaders or vanilla observed in the app
// states it.

package game

import (
	"maps"
	"slices"
)

// WidgetRole is what a widget-valued block is to the widget that writes it; "" a child.
type WidgetRole string

// The roles a block has besides being a child.
const (
	RoleSkin       WidgetRole = "skin"       // fills the widget, drawn behind its children: a background
	RoleChrome     WidgetRole = "chrome"     // sits at the widget's edge: a scroll bar
	RoleAttachment WidgetRole = "attachment" // shown on an event, never in the window: a tooltip
	RoleLabel      WidgetRole = "label"      // holds the widget that draws its widget's text: buttonText
)

// WidgetSlot is a key whose block is a widget no type names.
type WidgetSlot struct {
	Role WidgetRole // what the block is to its widget
	Type string     // the built-in type the block is drawn as
}

// Holding is how a built-in type holds its children.
type Holding string

// The holdings; the zero value holds children by their anchors.
const (
	HoldsRow    Holding = "row"
	HoldsColumn Holding = "column"
	HoldsFlow   Holding = "flow"
	HoldsGrid   Holding = "grid"
	HoldsScroll Holding = "scroll"
	HoldsCell   Holding = "cell"
)

// WidgetType is how a built-in type draws.
type WidgetType struct {
	Holding     Holding  // how it holds its children
	Clips       bool     // it clips them
	Fills       bool     // it fills its holder
	CentersText bool     // its own text is centered
	DropsHidden []string // the games in which a hidden child of it takes no room
}

// plainWidget is the type a slot with none of its own is drawn as.
const plainWidget = "widget"

// WidgetSlots are the keys whose block is a widget no type names, lower-cased.
var WidgetSlots = map[string]WidgetSlot{
	"background":           {Role: RoleSkin, Type: "background"},
	"tooltipwidget":        {Role: RoleAttachment},
	"scrollbar_vertical":   {Role: RoleChrome, Type: "scrollbar"},
	"scrollbar_horizontal": {Role: RoleChrome, Type: "scrollbar"},
	"buttontext":           {Role: RoleLabel},
	"scrollwidget":         {Type: plainWidget},
	"zoomwidget":           {Type: plainWidget},
}

// allGames is every game, for a row that holds in each.
var allGames = slices.Sorted(maps.Keys(registry))

// GUITypes is how each built-in type draws; any other holds its children by their anchors and keeps a hidden child's
// room. The wiki: a box ignores invisible children, a grid cannot, a flowcontainer keeps their room (Vic3's drops it,
// observed); a slice over its chart and a button's centered label are observed.
var GUITypes = map[string]WidgetType{
	"hbox":           {Holding: HoldsRow, DropsHidden: allGames},
	"uberhbox":       {Holding: HoldsRow, DropsHidden: allGames},
	"vbox":           {Holding: HoldsColumn, DropsHidden: allGames},
	"ubervbox":       {Holding: HoldsColumn, DropsHidden: allGames},
	"scrollvbox":     {Holding: HoldsColumn, Clips: true, DropsHidden: allGames},
	"flowcontainer":  {Holding: HoldsFlow, DropsHidden: []string{vic3}},
	"fixedgridbox":   {Holding: HoldsGrid},
	"dynamicgridbox": {Holding: HoldsGrid},
	"scrollarea":     {Holding: HoldsScroll, Clips: true},
	"uberscrollarea": {Holding: HoldsScroll, Clips: true},
	"pieslice":       {Fills: true},
	"uberpieslice":   {Fills: true},
	"barslice":       {Fills: true},
	"treemapslice":   {Fills: true},
	"button":         {CentersText: true},
	"game_button":    {CentersText: true},
}

// UndeclaredType is a built-in type that no file of the install declares.
type UndeclaredType struct {
	Type  string
	Games []string // the games that draw it
}

// UndeclaredTypes are the built-in types a game draws though no file of its install declares them.
var UndeclaredTypes = []UndeclaredType{
	{"zoomarea", []string{ck3}}, {"text_occluder", []string{ck3}},
	{"overlappingitembox", []string{vic3}}, {"treemapchart", []string{vic3}}, {"portrait_button", []string{vic3}},
	{"axis", []string{vic3, eu5}},
	{"plotline", []string{eu5}}, {"screen_grab", []string{eu5}}, {"newsfeed_carousel", []string{eu5}},
}

// GUIHolder is the window the engine lays out a window made from a type in.
type GUIHolder struct {
	Type   string   // the type a window is made from
	Window string   // the window it is laid out in
	Games  []string // the games that lay it out so
}

// GUIHolders are the windows the engine lays those windows out in (EU5's lateral views).
var GUIHolders = []GUIHolder{{Type: "lateralview", Window: "lateralview_left_layer", Games: []string{eu5}}}

// GUIOpeningStates is which states run as a window opens.
var GUIOpeningStates = struct {
	Names    []string          // states run by name
	Triggers map[string]string // states run by a key's value
	OwnKeys  []string          // keys of a state that are its own rather than end values
}{
	Names:    []string{"_show"},
	Triggers: map[string]string{"trigger_on_create": "yes"},
	OwnKeys: []string{
		FindRuleByExtension(".gui").WidgetKey, "duration", "next", "on_start", "on_finish", "trigger_on_create", "delay", "bezier", "loop", "animation",
		"restart_on_show", "skip_initial_animation", "trigger_when", "start_sound", "end_sound", "sstart_sound",
		"animation_speed", "loopinterval",
	},
}

// GUIHiddenKeys are the keys whose `no` hides a widget; visible_at_creation is written only as `no`, for a widget a
// script shows later (a search box's autocomplete list).
var GUIHiddenKeys = []string{"visible", "visible_at_creation"}

// DefinesDatabase is the database of the game's defines, which GetDefine reads.
const DefinesDatabase = "defines.define"

// GUIAnswered are the data calls the preview answers from the index, each with the database its last argument names.
var GUIAnswered = map[string]string{"GetDefine": DefinesDatabase}

// GUIBooleans are the data functions a binding combines conditions with, by name without the argument count.
var GUIBooleans = map[string]string{"And": "and", "Or": "or", "Nor": "nor", "Not": "not"}

// IconPathOverrides is the file in an icon folder that names the paths its icons are read from, read by every game
// where it exists (SourceVanilla: CK3's install writes one; the CK3 and EU5 executables hold the name). The reader is
// the draw payload's icon lookup.
const IconPathOverrides = "path_overrides.txt"

// GUIPictureKeys are the keys an entry names its picture by; the first written wins.
var GUIPictureKeys = []string{"icon", "texture"}

// GUIIconPathSuffix ends the defines that name a folder of icons.
const GUIIconPathSuffix = "_ICON_PATH"
