// gui.go holds what every game does with a GUI file beyond its syntax, as one literal: which words declare its
// databases, which keys name its widgets, how each built-in type draws, the states a window opens in, the calls
// the Visual view answers, and how a definition names its picture. What differs by game is on the game's row.

package game

// A WidgetType says how a built-in widget type lays its children out and draws its own text.
type WidgetType struct {
	Holding     string // how it holds its children: row, column, flow, grid, scroll; "" by their anchors. Read by picture.ts's HOLDS.
	Clips       bool   // it clips them
	Fills       bool   // it fills its holder
	CentersText bool   // its own text is centered
	DropsHidden bool   // a hidden child of it takes no room
}

// A WidgetSlot is a key whose block is a widget that no type names.
type WidgetSlot struct {
	Role string // what the block is to its widget: skin (a background), chrome (a scroll bar), attachment (a tooltip), label (its text). Read by picture.ts.
	Type string // the built-in type the block is drawn as; "" the block's own
}

// GUIRules is everything the walk, the scan and the Visual view need to know about a GUI file beyond its syntax.
// GUI is its one row.
type GUIRules struct {
	Declares        []string          // the words before a top block that declare a definition of gui.<word>, or group them (types x { })
	NameKeyOf       map[string]string // a block key at any depth, and the key whose value names its definition
	WidgetKey       string            // every other top block is a definition of WidgetDatabase named by this key
	DatabasePrefix  string            // what a GUI database's name starts with, before the word that declares it
	WidgetDatabase  string            // the database of every top block that no declaring word declares
	TagCalls        string            // the word that calls the tag a block is written with
	ContextKey      string            // the key whose value is the data call its block's bindings read in
	References      []Reference       // what a widget's keys name, Database "" for any
	Slots           map[string]WidgetSlot
	Types           map[string]WidgetType // any other type holds its children by their anchors and keeps a hidden child's room
	OpeningStates   []string              // the states run by name as a window opens
	OpeningTriggers map[string]string     // the states run as a window opens by a key's value
	StateKeys       []string              // the keys of a state that are its own rather than end values
	HiddenKeys      []string              // the keys whose no hides a widget
	Answered        map[string]string     // the data calls the Visual view answers from the index, each with the database its last argument names
	Booleans        map[string]string     // the data functions a binding combines conditions with, by name without the argument count
	IconPathFile    string                // the file in an icon folder that names the paths its icons are read from
	PictureKeys     []string              // the keys a definition names its picture by; the first written wins
	IconPathSuffix  string                // the ending of the defines that name a folder of icons
}

const guiWidgetKey = "name"

// GUI is what every game does with a GUI file. The types come from the wiki's Interface page: a box ignores
// invisible children, a grid cannot, and a flowcontainer keeps their room; a slice over its chart and a button's
// centered label are observed. visible_at_creation is only ever written as no, for a widget a script shows later.
// CK3's install writes a path_overrides.txt, and the CK3 and EU5 executables hold the name.
var GUI = GUIRules{
	Declares:       []string{"type", "local_type", "template", "local_template", "block", "layer"},
	NameKeyOf:      map[string]string{"texticon": "icon", "format": "name"},
	WidgetKey:      guiWidgetKey,
	DatabasePrefix: "gui.",
	WidgetDatabase: "gui.widget",
	TagCalls:       "widget",
	ContextKey:     "datacontext",
	References: []Reference{
		{KeyPath: "using", Target: "gui.template"}, {KeyPath: "layer", Target: "gui.layer"},
		{KeyPath: "texture", Target: File}, {KeyPath: "font", Target: "font.font"},
		{KeyPath: "text", Target: Localization}, {KeyPath: "title", Target: Localization},
	},
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
		"fixedgridbox":   {Holding: "grid"},
		"dynamicgridbox": {Holding: "grid"},
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
	HiddenKeys:     []string{"visible", "visible_at_creation"},
	Answered:       map[string]string{"GetDefine": "defines.define"},
	Booleans:       map[string]string{"And": "and", "Or": "or", "Nor": "nor", "Not": "not"},
	IconPathFile:   "path_overrides.txt",
	PictureKeys:    []string{"icon", "texture"},
	IconPathSuffix: "_ICON_PATH",
}
