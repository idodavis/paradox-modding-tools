// coat.go holds the rules for coats of arms that every game shares. What differs by game is on the game's row.

package game

// A CoatStrength is a texture channel taken at a strength.
type CoatStrength struct {
	Channel  string  `json:"channel"`  // the channel (r, g, b)
	Strength float64 `json:"strength"` // 0 takes none
}

// A CoatLerp is a texture channel that blends a color slot in.
type CoatLerp struct {
	Channel string // the channel that weights the blend
	Slot    int    // the color slot blended in
}

// A CoatRecolor is how one kind of texture is colored.
type CoatRecolor struct {
	Start   int          // the slot it starts from; 0 means the fallback color
	Lerps   []CoatLerp   // the blends toward a slot, in order
	Overlay CoatStrength // the channel overlaid on the result
	Shape   string       // the channel that is its alpha; "" means the blends' own
}

// A CoatShade is the shading applied over every coat of a game that shades its coats. The zero value shades
// nothing.
type CoatShade struct {
	Texture string       // the texture sampled over the field
	Overlay CoatStrength // the channel of it overlaid on each coat and sub
	Grain   CoatStrength // the channel times a factor over each emblem's alpha
}

// A CoatSlot is a color slot that a layer's shaders read.
type CoatSlot struct {
	Key      string // the key that writes it (color1)
	Required bool   // the layer must write it
}

// A CoatPlace is what one placement key does to a layer.
type CoatPlace struct {
	Part     string    // what it moves, sizes or turns: center, size, turn, corner or depth
	Defaults []float64 // the values it takes where the layer writes none
}

// A CoatFrame is a shape that a game's coat designer previews a coat in.
type CoatFrame struct {
	Label    string       // the designer's name for it
	Mask     string       // the mask file; "" where a define names the frame
	Define   string       // the define naming the frame whose mask is used (a culture's, drawn as the default)
	Override CoatOverride // the defines that win over the frame and the default one
}

// A CoatOverride names the defines that replace a frame, or "" where the frame has none.
type CoatOverride struct {
	Frame   string // the frame's name (a dynasty's or house's)
	Enabled string // whether the realm's mask is forced
	Offset  string // the offset both give
	Scale   string // the scale both give
}

// A CoatTemplate is what the coat dialog writes for a new coat.
type CoatTemplate struct {
	Pattern   string              // a plain coat's pattern: the first starting with it, else the first
	Colors    []string            // its first colors
	Instances map[string]string   // a new instance of each role that is placed, on one line
	Layers    map[string][]string // a new layer of each role, a line each, with %s for its texture or its parent
}

// CoatRules is the rules for a game's coats of arms beyond their syntax. Coat is its one row.
type CoatRules struct {
	Database       string                 // the database of coats
	ColorsDatabase string                 // the named colors its slots name
	ListsDatabase  string                 // the template lists that a list "x" value names
	FieldFile      string                 // the file whose first atlas' tile_size sizes the field
	TextureFolders map[string]string      // the folder each texture key's files are under
	Recolors       map[string]CoatRecolor // how each kind of texture is colored; none keeps its pixels
	Regions        []string               // the pattern channel of each mask region, region n at n-1
	Fallback       string                 // the color a pattern starts from, as CSS
	// Slots are the color slots a coat or a layer writes, in order; the shaders read the first three.
	Slots     []CoatSlot
	Placement map[string]map[string]CoatPlace // what a placed layer's keys do to it, by the layer's type
	Inputs    map[string]string               // how each coat key's value is edited
	Template  CoatTemplate
	FrameMask string   // where a named frame's mask is, with the name at %s
	Holders   []string // the databases whose definitions take the coat of their own name where they name none
}

// coatInstance is the instance a new emblem is written with: at the middle of the field, at half size, on one
// line.
const coatInstance = "instance = { position = { 0.5 0.5 } scale = { 0.5 0.5 } }"

// coatSubInstance is the instance a new sub is written with: a quarter of the field in from its corner, at half size,
// on one line.
const coatSubInstance = "instance = { offset = { 0.25 0.25 } scale = { 0.5 0.5 } }"

// Coat is the rules for coats of arms that every game shares.
var Coat = CoatRules{
	Database:       "coat_of_arms/coat_of_arms",
	ColorsDatabase: "named_colors",
	ListsDatabase:  "coat_of_arms/template_lists",
	FieldFile:      "common/coat_of_arms/options/atlases.txt",
	TextureFolders: map[string]string{
		"pattern":         "gfx/coat_of_arms/patterns",
		"colored_emblem":  "gfx/coat_of_arms/colored_emblems",
		"textured_emblem": "gfx/coat_of_arms/textured_emblems",
	},
	Recolors: map[string]CoatRecolor{
		"pattern":        {Start: 0, Lerps: []CoatLerp{{"r", 1}, {"g", 2}, {"b", 3}}, Overlay: CoatStrength{Channel: "b"}},
		"colored_emblem": {Start: 1, Lerps: []CoatLerp{{"g", 2}, {"r", 3}}, Overlay: CoatStrength{Channel: "b", Strength: 1}, Shape: "a"},
	},
	Regions:  []string{"r", "g", "b"},
	Fallback: "rgba(0, 0, 0, 0)",
	// The shaders read three (coat_of_arms_pattern.fxh:7-9); an emblem names a coat's color4 (EU5
	// 00_random_countries.txt:30-31,69, Vic3 01_subs.txt:235).
	Slots: []CoatSlot{{Key: "color1", Required: true}, {Key: "color2"}, {Key: "color3"}, {Key: "color4"}, {Key: "color5"}},
	Placement: map[string]map[string]CoatPlace{
		"instance": {
			"position": {Part: "center", Defaults: []float64{0.5, 0.5}},
			"scale":    {Part: "size", Defaults: []float64{1, 1}},
			"rotation": {Part: "turn", Defaults: []float64{0}},
			"depth":    {Part: "depth", Defaults: []float64{0}}, // CK3 01_holy_order_coas.txt:139
		},
		"sub": {
			"offset": {Part: "corner", Defaults: []float64{0, 0}},
			"scale":  {Part: "size", Defaults: []float64{1, 1}},
		},
	},
	Inputs: map[string]string{
		"pattern": "texture", "texture": "texture",
		"color1": "color", "color2": "color", "color3": "color", "color4": "color", "color5": "color",
		"position": "pair", "scale": "pair", "offset": "pair",
		"rotation": "number", "depth": "number",
		"mask": "regions",
	},
	Template: CoatTemplate{
		Pattern: "pattern_solid",
		Colors:  []string{"white", "black"},
		Instances: map[string]string{
			"colored_emblem": coatInstance, "textured_emblem": coatInstance, "sub": coatSubInstance,
		},
		Layers: map[string][]string{
			"colored_emblem":  {"colored_emblem = {", `texture = "%s"`, `color1 = "white"`, coatInstance, "}"},
			"textured_emblem": {"textured_emblem = {", `texture = "%s"`, coatInstance, "}"},
			"sub":             {"sub = {", `parent = "%s"`, coatSubInstance, "}"},
		},
	},
	FrameMask: "gfx/interface/coat_of_arms/frames/%s_mask.dds",
	// _dynamic_definitions.info:11, common/dynasties/_dynasties.info:7, common/dynasty_houses/_dynasty_houses.info:6,
	// Vic3 common/flag_definitions/00_flag_definitions.txt:1 ("its tag is used a COA_KEY").
	Holders: []string{"landed_titles", "dynasties", "dynasty_houses", "country_definitions"},
}

var ck3CoatShade = CoatShade{ // game/gfx/FX/coat_of_arms
	Texture: "gfx/coat_of_arms/coa_mask_texture.dds",
	Overlay: CoatStrength{Channel: "b", Strength: 0.2},
	Grain:   CoatStrength{Channel: "g", Strength: 2},
}

// ck3CoatFrames are the frames of gui/shared/coa_designer.gui, without the unit's 3D banner. The override defines are
// in common/defines/graphic/00_graphics.txt.
var ck3CoatFrames = []CoatFrame{
	{
		Label: "Dynasty", Define: "DEFAULT_DYNASTY_COA_FRAME_NAME",
		Override: CoatOverride{
			Frame: "OVERRIDE_DYNASTY_COA_FRAME_NAME", Offset: "OVERRIDE_DYNASTY_COA_FRAME_OFFSET",
			Scale: "OVERRIDE_DYNASTY_COA_FRAME_SCALE",
		},
	},
	{
		Label: "House", Define: "DEFAULT_HOUSE_COA_FRAME_NAME",
		Override: CoatOverride{
			Frame: "OVERRIDE_HOUSE_COA_FRAME_NAME", Offset: "OVERRIDE_HOUSE_COA_FRAME_OFFSET",
			Scale: "OVERRIDE_HOUSE_COA_FRAME_SCALE",
		},
	},
	{Label: "Title", Mask: "gfx/interface/coat_of_arms/title_mask.dds"},
	{
		Label: "Realm", Mask: "gfx/interface/icons/realm_frames/_default_mask.dds",
		Override: CoatOverride{
			Enabled: "OVERRIDE_REALM_MASK_ENABLED", Offset: "OVERRIDE_REALM_MASK_OFFSET", Scale: "OVERRIDE_REALM_MASK_SCALE",
		},
	},
}
