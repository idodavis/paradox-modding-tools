// coat.go holds the rules for coats of arms that every game shares. What
// differs by game is on the game's row.

package game

import "paradox-modding-tools/internal/jomini"

// CoatStrength is a texture channel taken at a strength.
type CoatStrength struct {
	// Channel is the channel: r, g or b.
	Channel string `json:"channel"`

	// Strength is how much of the channel is taken. 0 takes none.
	Strength float64 `json:"strength"`
}

// CoatLerp is a texture channel that blends a color slot in.
type CoatLerp struct {
	// Channel is the channel that weights the blend, and Slot the color slot
	// blended in.
	Channel string
	Slot    int
}

// CoatRecolor is how the textures of one texture key are colored, such as a
// pattern's.
type CoatRecolor struct {
	// Start is the slot it starts from. It is 0 for the fallback color.
	Start int

	// Lerps are the blends toward a slot, in order.
	Lerps []CoatLerp

	// Overlay is the channel overlaid on the result.
	Overlay CoatStrength

	// Shape is the channel that is its alpha. It is "" for the blends' own.
	Shape string
}

// CoatShade is the shading laid over every coat of a game that shades its
// coats. The zero CoatShade shades nothing.
type CoatShade struct {
	// Texture is the texture sampled over the field.
	Texture string

	// Overlay is the channel of it overlaid on each coat and sub, and Grain
	// the channel laid times a factor over each emblem's alpha.
	Overlay CoatStrength
	Grain   CoatStrength
}

// CoatSlot is a color slot of a coat or of one of its layers.
type CoatSlot struct {
	// Key is the key that writes it, as color1 does.
	Key string
}

// CoatPlace is what one placement key does to a layer.
type CoatPlace struct {
	// Part is what it moves, sizes or turns.
	Part CoatPart

	// Defaults are the values it takes where the layer writes none.
	Defaults []float64
}

// CoatPart is what one placement key sets on a layer's instance. The zero
// CoatPart sets nothing.
type CoatPart uint8

// The parts a placement key sets: the center an emblem is placed by, the
// corner a sub is placed by, the size, the turn and the depth.
const (
	CoatPartCenter CoatPart = iota + 1
	CoatPartCorner
	CoatPartSize
	CoatPartTurn
	CoatPartDepth
)

// CoatFrame is a shape that a game's coat designer previews a coat in.
type CoatFrame struct {
	// Label is the designer's name for it.
	Label string

	// Mask is the mask file. It is "" where a define names the frame.
	Mask string

	// Define is the define that names the frame whose mask is used: a
	// culture's, drawn as the default.
	Define string
}

// CoatTemplate is what the coat dialog writes for a new coat.
type CoatTemplate struct {
	// Pattern is how a new layer's texture is picked: the first whose name
	// starts with it, else the first.
	Pattern string

	// Colors are its first colors.
	Colors []string

	// Instances are a new instance of each role that is placed, on one line.
	Instances map[string]string

	// Layers are a new layer of each role, a line each, with %s for its
	// texture or its parent.
	Layers map[string][]string
}

// CoatSpec is the rules for a game's coats of arms beyond their syntax. Coat
// is its one row.
type CoatSpec struct {
	// Database is the database of coats, ColorsDatabase the named colors its
	// slots name, and ListsDatabase the template lists that a list "x" value
	// names.
	Database       string
	ColorsDatabase string
	ListsDatabase  string

	// FieldFile is the file whose first atlas' tile_size sizes the field.
	FieldFile string

	// TextureFolders are the folder of each texture key's files.
	TextureFolders map[string]string

	// Recolors are how each texture key's files are colored. A key with none
	// keeps their pixels.
	Recolors map[string]CoatRecolor

	// Regions are the pattern channel of each mask region, region n at n-1.
	Regions []string

	// Fallback is the color a pattern starts from, as CSS.
	Fallback string

	// Slots are the color slots a coat or a layer writes, in order. The
	// game draws with the first three.
	Slots []CoatSlot

	// Placement is what a placed layer's keys do to it, by the layer's type.
	Placement map[string]map[string]CoatPlace

	// Inputs are how each coat key's value is edited.
	Inputs map[string]string

	Template CoatTemplate

	// FrameMask is where a named frame's mask is, with the name at %s.
	FrameMask string

	// PatternKey is the key of a coat's pattern, which draws under every
	// emblem and has no instance.
	PatternKey string

	// InstanceKey is the key of one placement of an emblem or a sub, and
	// Placement's row for an emblem.
	InstanceKey string

	// SubKey is the key of a sub-coat, and Placement's row for a sub.
	SubKey string

	// ParentKey is the key that names the coat a coat or a sub is laid over,
	// and NoParent the parent that names no coat.
	ParentKey string
	NoParent  string

	// TextureKey is the key of an emblem's texture, and MaskKey the key of the
	// pattern's regions it shows in.
	TextureKey string
	MaskKey    string

	// TileSizeKey is the key of an atlas' tile size in FieldFile.
	TileSizeKey string

	// ListTag is the tag of a value drawn from a template list, as list "x"
	// writes it.
	ListTag string
}

// GameCoat is what one game does with coats of arms beyond what every game
// does.
type GameCoat struct {
	// Folder is where the game keeps its coats, from the folder that holds its
	// stages.
	Folder string

	// Shade is how the game shades every coat. The zero CoatShade shades none.
	Shade CoatShade

	// Frames are the frames its coat designer previews, in order.
	Frames []CoatFrame

	// ColorForm is the form PMT writes a new layer's color in: the one the
	// game's own coats use.
	ColorForm jomini.ColorForm
}

// coatInstance is the instance a new emblem is written with: at the middle of
// the field, at half size, on one line.
const coatInstance = "instance = { position = { 0.5 0.5 } scale = { 0.5 0.5 } }"

// coatSubInstance is the instance a new sub is written with: a quarter of the
// field in from its corner, at half size, on one line.
const coatSubInstance = "instance = { offset = { 0.25 0.25 } scale = { 0.5 0.5 } }"

// Coat is the rules for coats of arms that every game shares.
var Coat = CoatSpec{
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
	// The game draws a coat with its first three colors. An emblem names a
	// coat's color4 (EU5 00_random_countries.txt:30-31,69, Vic3
	// 01_subs.txt:235).
	Slots: []CoatSlot{{Key: "color1"}, {Key: "color2"}, {Key: "color3"}, {Key: "color4"}, {Key: "color5"}},
	Placement: map[string]map[string]CoatPlace{
		"instance": {
			"position": {Part: CoatPartCenter, Defaults: []float64{0.5, 0.5}},
			"scale":    {Part: CoatPartSize, Defaults: []float64{1, 1}},
			"rotation": {Part: CoatPartTurn, Defaults: []float64{0}},
			// CK3 01_holy_order_coas.txt:139 writes one, and CK3
			// _emblem_layouts.info:67 gives the default as 0.01. Vic3 and EU5
			// ship no doc of it and are taken to share it.
			"depth": {Part: CoatPartDepth, Defaults: []float64{0.01}},
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
