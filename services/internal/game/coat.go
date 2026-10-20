// coat.go holds what a game does with a coat of arms beyond its syntax: where its coats and textures are, how layers
// order, turn, color and shade, how a key is edited, what a new coat writes and the frames its designer previews. A row
// the game's shaders state is SourceVanilla; one observed in the app is SourceInferred.

package game

// CoatStrength is a texture channel taken at a strength.
type CoatStrength struct {
	Channel  string  // the channel (r, g, b)
	Strength float64 // 0 takes none
}

// CoatLerp is a texture channel that blends a color slot in.
type CoatLerp struct {
	Channel string // the channel that weights the blend
	Slot    int    // the color slot blended in
}

// CoatRecolor is how one texture kind is colored.
type CoatRecolor struct {
	Start   int          // the slot it starts from; 0 the fallback color
	Lerps   []CoatLerp   // the blends toward a slot, in order
	Overlay CoatStrength // the channel overlaid on the result
	Shape   string       // the channel that is its alpha; "" the blends' own
	Source  Source
}

// CoatShade is how a game shades every coat.
type CoatShade struct {
	Texture string       // the texture sampled over the field
	Overlay CoatStrength // the channel of it overlaid on each coat and sub
	Grain   CoatStrength // the channel times a factor over each emblem's alpha
	Source  Source
}

// CoatSlot is a color slot a layer's shaders read.
type CoatSlot struct {
	Key      string // the key that writes it (color1)
	Required bool   // the layer must write it
	Source   Source
}

// CoatPlace is what one placement key does to a layer.
type CoatPlace struct {
	Part     string    // what it moves, sizes or turns (center, size, turn, corner)
	Defaults []float64 // the values it takes where the layer writes none
	Source   Source
}

// CoatFrame is a shape the game's coat designer previews a coat in.
type CoatFrame struct {
	Label    string       // the designer's name for it
	Mask     string       // the mask file; "" where a define names the frame
	Define   string       // the define naming the frame whose mask is used (a culture's, drawn as the default)
	Override CoatOverride // the defines that win over the frame and the default one (CK3 1.20)
}

// CoatOverride is the defines that replace a frame; "" where the frame has none.
type CoatOverride struct {
	Frame   string // the frame's name (a dynasty's or house's)
	Enabled string // whether the realm's mask is forced
	Offset  string // the offset both give
	Scale   string // the scale both give
}

// CoatDatabases are the databases a coat reads: its own, and the named colors its slots name.
var CoatDatabases = struct{ Coats, Colors string }{Coats: "coat_of_arms/coat_of_arms", Colors: "named_colors"}

// CoatFolders are where each game keeps its coats, from the folder that holds its stages: vanilla's, and where a mod's
// new ones go (observed).
var CoatFolders = map[string]string{
	ck3:  "common/coat_of_arms/coat_of_arms",
	vic3: "common/coat_of_arms/coat_of_arms",
	eu5:  "main_menu/common/coat_of_arms/coat_of_arms",
}

// CoatFieldFile is the file whose first atlas' tile_size is the field a coat is drawn on.
const CoatFieldFile = "common/coat_of_arms/options/atlases.txt"

// CoatTextureFolders are the folders each texture key's files are under (CK3's gfx/coat_of_arms/*/_*.info).
var CoatTextureFolders = map[string]string{
	"pattern":         "gfx/coat_of_arms/patterns",
	"colored_emblem":  "gfx/coat_of_arms/colored_emblems",
	"textured_emblem": "gfx/coat_of_arms/textured_emblems",
}

// CoatDepthInFront reports whether a layer of a larger depth draws in front (SourceInferred: observed, it draws
// behind).
const CoatDepthInFront = false

// CoatRotationClockwise reports whether a positive rotation turns clockwise (SourceInferred: observed in
// k_papal_state's keys).
const CoatRotationClockwise = true

// CoatRecolors are how each texture kind is colored, as its shader header says (coat_of_arms_pattern.fxh and
// coat_of_arms_textured_emblem.fxh under COLORED_EMBLEM); a textured emblem has no row and draws as its pixels are.
var CoatRecolors = map[string]CoatRecolor{
	"pattern": {
		Start: 0, Lerps: []CoatLerp{{"r", 1}, {"g", 2}, {"b", 3}}, Overlay: CoatStrength{Channel: "b"}, Source: SourceVanilla,
	},
	"colored_emblem": {
		Start: 1, Lerps: []CoatLerp{{"g", 2}, {"r", 3}}, Overlay: CoatStrength{Channel: "b", Strength: 1}, Shape: "a",
		Source: SourceVanilla,
	},
}

// CoatRegions are the pattern channel of each mask region, region n at n-1: a region keeps its channel less every
// later one, and an emblem's alpha is the sum of the regions it keeps (the shader's USE_PATTERN_MASK, SourceVanilla).
var CoatRegions = []string{"r", "g", "b"}

// CoatFallback is the color a pattern starts from, the shader's FallbackColor: transparent (SourceInferred: observed
// in Vic3's heater-shield subs).
const CoatFallback = "rgba(0, 0, 0, 0)"

// CoatShading is how each game shades its coats, as CK3's shaders (game/gfx/FX/coat_of_arms) do; a game not listed
// shades none.
var CoatShading = map[string]CoatShade{
	ck3: {
		Texture: "gfx/coat_of_arms/coa_mask_texture.dds",
		Overlay: CoatStrength{Channel: "b", Strength: 0.2}, Grain: CoatStrength{Channel: "g", Strength: 2}, Source: SourceVanilla,
	},
}

// CoatSlots are the color slots a layer's shaders read, in order; the first is required (SourceInferred: no document
// says, and every game's coats write it).
var CoatSlots = []CoatSlot{
	{Key: "color1", Required: true, Source: SourceInferred},
	{Key: "color2", Source: SourceVanilla},
	{Key: "color3", Source: SourceVanilla},
}

// CoatPlacement is what a placed layer's keys do to it, by the layer's type, with the values they take where none is
// written (the wiki's Coat of arms modding page; observed: an unwritten position is the field's middle, not the
// wiki's corner).
var CoatPlacement = map[string]map[string]CoatPlace{
	"instance": {
		"position": {Part: "center", Defaults: []float64{0.5, 0.5}, Source: SourceInferred},
		"scale":    {Part: "size", Defaults: []float64{1, 1}, Source: SourceWiki},
		"rotation": {Part: "turn", Defaults: []float64{0}, Source: SourceWiki},
	},
	"sub": {
		"offset": {Part: "corner", Defaults: []float64{0, 0}, Source: SourceWiki},
		"scale":  {Part: "size", Defaults: []float64{1, 1}, Source: SourceWiki},
	},
}

// CoatInputs are how each coat key's value is edited (the wiki's Coat of arms modding page, SourceWiki).
var CoatInputs = map[string]string{
	"pattern": "texture", "texture": "texture",
	"color1": "color", "color2": "color", "color3": "color", "color4": "color", "color5": "color",
	"position": "pair", "scale": "pair", "offset": "pair",
	"rotation": "number", "depth": "number",
	"mask": "regions",
}

// coatInstance is the instance a new emblem is written with: at the field's middle at half size, on one line.
const coatInstance = "instance = { position = { 0.5 0.5 } scale = { 0.5 0.5 } }"

// CoatNew is what the coat dialog writes for a new coat (SourceInferred).
var CoatNew = struct {
	Pattern     string   // a plain coat's pattern: the first starting with it, else the first
	Colors      []string // its first colors
	Instance    string   // a new instance
	EmblemLines []string // a new colored emblem, a line each, with %s for its texture
}{
	Pattern:     "pattern_solid",
	Colors:      []string{"white", "black"},
	Instance:    coatInstance,
	EmblemLines: []string{"colored_emblem = {", `texture = "%s"`, `color1 = "white"`, coatInstance, "}"},
}

// CoatFrameMask is where a named frame's mask is (common/culture/cultures/_cultures.info).
const CoatFrameMask = "gfx/interface/coat_of_arms/frames/%s_mask.dds"

// CoatFrames are the shapes each game's coat designer previews a coat in, in its order (CK3's
// gui/shared/coa_designer.gui; the unit's banner is 3D and left out); a game with no designer has none. The override
// defines are CK3 1.20's (common/defines/graphic/00_graphics.txt).
var CoatFrames = map[string][]CoatFrame{
	ck3: {
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
	},
}
