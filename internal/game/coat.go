// coat.go holds the rules for coats of arms that every game shares. What differs by game is on the game's row.

package game

import "paradox-modding-tools/internal/jomini"

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
	Part     CoatPart  // what it moves, sizes or turns
	Defaults []float64 // the values it takes where the layer writes none
}

// A CoatPart is what one placement key sets on a layer's instance. The zero CoatPart sets nothing.
type CoatPart uint8

// The parts a placement key sets: the center an emblem is placed by, the corner a sub is placed by, the size, the
// turn and the depth.
const (
	CoatPartCenter CoatPart = iota + 1
	CoatPartCorner
	CoatPartSize
	CoatPartTurn
	CoatPartDepth
)

// A CoatFrame is a shape that a game's coat designer previews a coat in.
type CoatFrame struct {
	Label  string // the designer's name for it
	Mask   string // the mask file; "" where a define names the frame
	Define string // the define naming the frame whose mask is used (a culture's, drawn as the default)
}

// A CoatTemplate is what the coat dialog writes for a new coat.
type CoatTemplate struct {
	Pattern   string              // a new layer's texture: the first whose name starts with it, else the first
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
	FrameMask string // where a named frame's mask is, with the name at %s
	// PatternKey is the key of a coat's pattern, which draws under every emblem and has no instance.
	PatternKey  string
	InstanceKey string // the key of one placement of an emblem or a sub, and Placement's row for an emblem
	SubKey      string // the key of a sub-coat, and Placement's row for a sub
	ParentKey   string // the key that names the coat a coat or a sub is laid over
	NoParent    string // the parent that names no coat
	TextureKey  string // the key of an emblem's texture
	MaskKey     string // the key of the pattern's regions an emblem shows in
	TileSizeKey string // the key of an atlas' tile size in FieldFile
	ListTag     string // the tag of a value drawn from a template list, as list "x" writes it
}

// A GameCoat is what one game does with coats of arms beyond what every game does.
type GameCoat struct {
	Folder    string           // where it keeps its coats, from the folder holding its stages
	Shade     CoatShade        // how it shades every coat; the zero value shades none
	Frames    []CoatFrame      // the frames its coat designer previews, in order
	ColorForm jomini.ColorForm // the form PMT writes a new layer color in, the one the game's own coats use
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
			"position": {Part: CoatPartCenter, Defaults: []float64{0.5, 0.5}},
			"scale":    {Part: CoatPartSize, Defaults: []float64{1, 1}},
			"rotation": {Part: CoatPartTurn, Defaults: []float64{0}},
			"depth":    {Part: CoatPartDepth, Defaults: []float64{0}}, // CK3 01_holy_order_coas.txt:139
		},
		"sub": {
			"offset": {Part: CoatPartCorner, Defaults: []float64{0, 0}},
			"scale":  {Part: CoatPartSize, Defaults: []float64{1, 1}},
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
			"pattern":         {`pattern = "%s"`},
			"colored_emblem":  {"colored_emblem = {", `texture = "%s"`, `color1 = "white"`, coatInstance, "}"},
			"textured_emblem": {"textured_emblem = {", `texture = "%s"`, coatInstance, "}"},
			"sub":             {"sub = {", `parent = "%s"`, coatSubInstance, "}"},
		},
	},
	FrameMask:  "gfx/interface/coat_of_arms/frames/%s_mask.dds",
	PatternKey: "pattern", InstanceKey: "instance", SubKey: "sub", ParentKey: "parent", NoParent: "none",
	TextureKey: "texture", MaskKey: "mask", TileSizeKey: "tile_size", ListTag: "list",
}
