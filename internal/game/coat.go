// coat.go holds what every game does with a coat of arms beyond its syntax, as one literal: the databases it
// reads, where its textures are, how layers are ordered, turned and colored, how a key is edited and what a new
// coat writes. Where a game keeps its coats, how it shades them and the frames its designer previews are on the
// game's row. A fact the shaders state says so; one observed in the app says observed.

package game

// A CoatStrength is a texture channel taken at a strength.
type CoatStrength struct {
	Channel  string  // the channel (r, g, b)
	Strength float64 // 0 takes none
}

// A CoatLerp is a texture channel that blends a color slot in.
type CoatLerp struct {
	Channel string // the channel that weights the blend
	Slot    int    // the color slot blended in
}

// A CoatRecolor says how one kind of texture is colored.
type CoatRecolor struct {
	Start   int          // the slot it starts from; 0 the fallback color
	Lerps   []CoatLerp   // the blends toward a slot, in order
	Overlay CoatStrength // the channel overlaid on the result
	Shape   string       // the channel that is its alpha; "" the blends' own
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

// A CoatPlace says what one placement key does to a layer.
type CoatPlace struct {
	Part     string    // what it moves, sizes or turns: center, size, turn, corner. Read by coat.ts.
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
	Pattern     string   // a plain coat's pattern: the first starting with it, else the first
	Colors      []string // its first colors
	Instance    string   // a new instance
	EmblemLines []string // a new colored emblem, a line each, with %s for its texture
}

// CoatRules is everything the Visual view needs to know about a game's coats of arms beyond their syntax. Coat is
// its one row.
type CoatRules struct {
	Database          string                          // the database of coats
	ColorsDatabase    string                          // the named colors its slots name
	FieldFile         string                          // the file whose first atlas' tile_size is the field a coat is drawn on
	TextureFolders    map[string]string               // the folder each texture key's files are under
	DepthFront        bool                            // a layer of a larger depth draws in front
	RotationClockwise bool                            // a positive rotation turns clockwise
	Recolors          map[string]CoatRecolor          // how each texture kind is colored; a kind with no row draws as its pixels are
	Regions           []string                        // the pattern channel of each mask region, region n at n-1
	Fallback          string                          // the color a pattern starts from, as CSS
	Slots             []CoatSlot                      // the color slots a layer's shaders read, in order
	Placement         map[string]map[string]CoatPlace // what a placed layer's keys do to it, by the layer's type
	Inputs            map[string]string               // how each coat key's value is edited: texture, color, pair, number, regions. Read by CoatLayers.vue.
	Template          CoatTemplate
	FrameMask         string // where a named frame's mask is, with the name at %s
}

// coatInstance is the instance a new emblem is written with: at the middle of the field, at half size, on one
// line.
const coatInstance = "instance = { position = { 0.5 0.5 } scale = { 0.5 0.5 } }"

// Coat is what every game does with a coat of arms. The recolors come from the shader headers
// (coat_of_arms_pattern.fxh and coat_of_arms_textured_emblem.fxh under COLORED_EMBLEM): a region keeps its channel
// less every later one, and an emblem's alpha is the sum of the regions it keeps (USE_PATTERN_MASK). The fallback
// is transparent, observed in Vic3's heater-shield subs. Depth draws behind and rotation turns clockwise, observed.
// color1 is required because every game's coats write it. An unwritten position is the middle of the field,
// observed, not the wiki's corner. The placement and inputs are from the wiki's Coat of arms modding page; the
// frame mask is from _cultures.info.
var Coat = CoatRules{
	Database:       "coat_of_arms/coat_of_arms",
	ColorsDatabase: "named_colors",
	FieldFile:      "common/coat_of_arms/options/atlases.txt",
	TextureFolders: map[string]string{
		"pattern":         "gfx/coat_of_arms/patterns",
		"colored_emblem":  "gfx/coat_of_arms/colored_emblems",
		"textured_emblem": "gfx/coat_of_arms/textured_emblems",
	},
	DepthFront:        false,
	RotationClockwise: true,
	Recolors: map[string]CoatRecolor{
		"pattern":        {Start: 0, Lerps: []CoatLerp{{"r", 1}, {"g", 2}, {"b", 3}}, Overlay: CoatStrength{Channel: "b"}},
		"colored_emblem": {Start: 1, Lerps: []CoatLerp{{"g", 2}, {"r", 3}}, Overlay: CoatStrength{Channel: "b", Strength: 1}, Shape: "a"},
	},
	Regions:  []string{"r", "g", "b"},
	Fallback: "rgba(0, 0, 0, 0)",
	Slots:    []CoatSlot{{Key: "color1", Required: true}, {Key: "color2"}, {Key: "color3"}},
	Placement: map[string]map[string]CoatPlace{
		"instance": {
			"position": {Part: "center", Defaults: []float64{0.5, 0.5}},
			"scale":    {Part: "size", Defaults: []float64{1, 1}},
			"rotation": {Part: "turn", Defaults: []float64{0}},
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
		Pattern:     "pattern_solid",
		Colors:      []string{"white", "black"},
		Instance:    coatInstance,
		EmblemLines: []string{"colored_emblem = {", `texture = "%s"`, `color1 = "white"`, coatInstance, "}"},
	},
	FrameMask: "gfx/interface/coat_of_arms/frames/%s_mask.dds",
}

var ck3CoatShade = CoatShade{ // game/gfx/FX/coat_of_arms
	Texture: "gfx/coat_of_arms/coa_mask_texture.dds",
	Overlay: CoatStrength{Channel: "b", Strength: 0.2},
	Grain:   CoatStrength{Channel: "g", Strength: 2},
}

var ck3CoatFrames = []CoatFrame{ // gui/shared/coa_designer.gui, the unit's 3D banner left out; the override defines are 1.20's (common/defines/graphic/00_graphics.txt)
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
