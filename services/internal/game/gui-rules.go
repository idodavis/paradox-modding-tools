// gui-rules.go is the registry of what a game does with a GUI file beyond its syntax: the roles of a widget's blocks,
// how each built-in type draws, where a window is laid out, the state it opens in, the calls the preview answers and
// how an entry names its picture. A row from the wiki's Interface page or the game's shaders says nothing of it; one
// from vanilla observed in the app says observed.

package game

import (
	"maps"
	"slices"
)

// WidgetRole is what a widget-valued block is to the widget that writes it: a child ("") or one of the four below.
type WidgetRole string

// The roles a block has besides being a child.
const (
	RoleSkin       WidgetRole = "skin"       // fills the widget, drawn behind its children: a background
	RoleChrome     WidgetRole = "chrome"     // sits at the widget's edge: a scroll bar
	RoleAttachment WidgetRole = "attachment" // shown on an event, never in the window: a tooltip
	RoleLabel      WidgetRole = "label"      // holds the widget that draws its widget's text: buttonText
)

// WidgetSlot is a key whose block is a widget no type names: its role, and the built-in type the block is drawn as.
type WidgetSlot struct {
	Role WidgetRole
	Type string
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

// WidgetType is how a built-in type draws: how it holds its children, whether it clips them, whether it fills its
// holder, whether its own text is centered, and the games in which a hidden child of it takes no room.
type WidgetType struct {
	Holds       Holding
	Clips       bool
	Fills       bool
	CentersText bool
	DropsHidden []string
}

// The words the rows share: the plain widget type, and the games a row holds in alone.
const (
	plainWidget = "widget"
	ck3         = "ck3"
	vic3        = "vic3"
	eu5         = "eu5"
)

// WidgetSlots are the keys whose block is a widget no type names, lower-cased (the wiki's background, tooltipwidget,
// the scroll bars, buttonText, scrollwidget and zoomwidget).
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

// GUITypes is how each built-in type draws; any other type holds its children by their anchors and keeps a hidden
// child's room. The wiki: a box ignores invisible children, a grid cannot, a flowcontainer keeps their room (Vic3's
// drops it, observed); a slice over its chart and a button's centered label are observed.
var GUITypes = map[string]WidgetType{
	"hbox":           {Holds: HoldsRow, DropsHidden: allGames},
	"uberhbox":       {Holds: HoldsRow, DropsHidden: allGames},
	"vbox":           {Holds: HoldsColumn, DropsHidden: allGames},
	"ubervbox":       {Holds: HoldsColumn, DropsHidden: allGames},
	"scrollvbox":     {Holds: HoldsColumn, Clips: true, DropsHidden: allGames},
	"flowcontainer":  {Holds: HoldsFlow, DropsHidden: []string{vic3}},
	"fixedgridbox":   {Holds: HoldsGrid},
	"dynamicgridbox": {Holds: HoldsGrid},
	"scrollarea":     {Holds: HoldsScroll, Clips: true},
	"uberscrollarea": {Holds: HoldsScroll, Clips: true},
	"pieslice":       {Fills: true},
	"uberpieslice":   {Fills: true},
	"barslice":       {Fills: true},
	"treemapslice":   {Fills: true},
	"button":         {CentersText: true},
	"game_button":    {CentersText: true},
}

// UndeclaredTypes are the built-in types a game draws though no file of its install declares them.
var UndeclaredTypes = []string{
	"zoomarea", "text_occluder", // CK3
	"overlappingitembox", "treemapchart", "portrait_button", // Vic3
	"axis",                                         // Vic3 and EU5
	"plotline", "screen_grab", "newsfeed_carousel", // EU5
}

// GUIHolders is the window the engine lays a window made from a type out in, by the type (EU5's lateral views).
var GUIHolders = map[string]string{"lateralview": "lateralview_left_layer"}

// GUIOpeningStates is which states run as a window opens, by name or by a key's value, and the keys of a state that
// are its own rather than end values.
var GUIOpeningStates = struct {
	Names    []string
	Triggers map[string]string
	OwnKeys  []string
}{
	Names:    []string{"_show"},
	Triggers: map[string]string{"trigger_on_create": "yes"},
	OwnKeys: []string{
		FileTypes[".gui"].WidgetKey, "duration", "next", "on_start", "on_finish", "trigger_on_create", "delay", "bezier", "loop", "animation",
		"restart_on_show", "skip_initial_animation", "trigger_when", "start_sound", "end_sound", "sstart_sound",
		"animation_speed", "loopinterval",
	},
}

// GUIHiddenKeys are the keys whose `no` hides a widget: `visible`, and `visible_at_creation`, which every game writes
// only as `no` for a widget a script shows later (a search box's autocomplete list, a tooltip's expansion).
var GUIHiddenKeys = []string{"visible", "visible_at_creation"}

// DefinesDatabase is the database of the game's defines, which GetDefine reads and a coat's frames name.
const DefinesDatabase = "defines.define"

// GUIAnswered are the data calls the preview answers from the index: the database whose entry the call's last
// argument names.
var GUIAnswered = map[string]string{"GetDefine": DefinesDatabase}

// GUIBooleans are the data functions a binding combines conditions with, by name less the count of their arguments.
var GUIBooleans = map[string]string{"And": "and", "Or": "or", "Nor": "nor", "Not": "not"}

// GUIPictureKeys are the keys an entry names its picture by, the first written taken.
var GUIPictureKeys = []string{"icon", "texture"}

// GUIIconPathSuffix ends the defines that name a folder of icons.
const GUIIconPathSuffix = "_ICON_PATH"
