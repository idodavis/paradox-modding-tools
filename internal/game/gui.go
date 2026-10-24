// gui.go holds the rules for GUI files that every game shares: the words that declare databases, the keys that name
// widgets, how each built-in type draws, and how a definition names its picture. What differs by game is on the
// game's row.

package game

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

// GUIRules is the rules for GUI files beyond their syntax. GUI is its one row.
type GUIRules struct {
	// Declares is the words before a top block that declare a definition of gui.<word>, or group definitions
	// (types x { }).
	Declares       []string
	NameKeyOf      map[string]string // a block key at any depth, and the key whose value names its definition
	WidgetKey      string            // every other top block is a definition of WidgetDatabase named by this key
	DatabasePrefix string            // what a GUI database's name starts with, before the word that declares it
	WidgetDatabase string            // the database of every top block that no declaring word declares
	TagCalls       string            // the word that calls the tag a block is written with
	TemplateCalls  string            // the word that calls a template
	BlockCalls     string            // the word that calls the block a template or type leaves for a widget to replace
	ContextKey     string            // the key whose value is the data call its block's bindings read in
	References     []Reference       // what a widget's keys name, Database "" for any
	TextFormats    string            // the database a text's #format names
	TextIcons      string            // the database an icon in a text, @icon!, names
	Slots          map[string]WidgetSlot
	// Types is the row of each built-in widget type. Any other type holds its children by their anchors and keeps a
	// hidden child's room.
	Types           map[string]WidgetType
	OpeningStates   []string          // the states run by name as a window opens
	OpeningTriggers map[string]string // the states run as a window opens by a key's value
	StateKeys       []string          // the keys of a state that are its own rather than end values
	HiddenKeys      []string          // the keys whose no hides a widget
	DropsHiddenKey  string            // the key whose yes makes a hidden child of a widget take no room
	// CallTargets is the data functions whose quoted arguments are names, by function: the database each argument's
	// names are in, in order, and "" for one that is no name.
	CallTargets map[string][]string
	// Booleans is the data functions a binding combines conditions with, by name without the argument count.
	Booleans       map[string]string
	Literals       map[string]bool // a condition's plain values, in lower case, and whether each holds
	IconPathFile   string          // the file in an icon folder that names the paths its icons are read from
	PictureKeys    []string        // the keys a definition names its picture by, tried in this order
	IconPathSuffix string          // the ending of the defines that name a folder of icons
}

// The words that call a type, a template and a block override, which GUI and the callable databases both name,
// and the key that names a widget.
const (
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
		{KeyPath: "texture", Target: File}, {KeyPath: "font", Target: "font.font"},
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
}
