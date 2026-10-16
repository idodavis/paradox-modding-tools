// coa-rules.go is the registry of what a game does with a coat of arms beyond its syntax: the databases it reads,
// where each game keeps its coats and their textures, the file that sizes its field, which way depth orders its
// layers, the texture that shades it and the frames its coat designer previews it in. A row from the wiki says nothing
// of it; one from vanilla observed in the app says observed.

package game

// CoatFrame is a shape the game's coat designer previews a coat in: its label, and its mask, a file or the mask of the
// frame a define names (a culture's frame, drawn as the default).
type CoatFrame struct{ Label, Mask, Define string }

// CoatDatabases are the databases a coat reads: its own, and the named colors its color slots name.
var CoatDatabases = struct{ Coats, Colors string }{Coats: "coat_of_arms/coat_of_arms", Colors: "named_colors"}

// CoatFolders are the folder each game keeps its coats in, from the folder that holds its stages (the install's script
// root, a mod's root), as ModFolders are: where vanilla's are and a mod's new ones go (observed).
var CoatFolders = map[string]string{
	ck3:  "common/coat_of_arms/coat_of_arms",
	vic3: "common/coat_of_arms/coat_of_arms",
	eu5:  "main_menu/common/coat_of_arms/coat_of_arms",
}

// CoatFieldFile is the file whose first atlas' tile_size is the field a coat is drawn on.
const CoatFieldFile = "common/coat_of_arms/options/atlases.txt"

// CoatTextureFolders are the folders each texture key's file is under, by the key that writes it (CK3's
// gfx/coat_of_arms/*/_*.info).
var CoatTextureFolders = map[string]string{
	"pattern":         "gfx/coat_of_arms/patterns",
	"colored_emblem":  "gfx/coat_of_arms/colored_emblems",
	"textured_emblem": "gfx/coat_of_arms/textured_emblems",
}

// CoatDepthInFront reports whether a layer of a larger depth draws in front of one of a smaller (observed: it draws
// behind).
const CoatDepthInFront = false

// CoatShading is the texture a game shades every coat with, by game, sampled over the field: CK3's coat shaders
// (game/gfx/FX/coat_of_arms) overlay its blue on the pattern and each emblem and take its green into an emblem's
// alpha; a game not listed shades none.
var CoatShading = map[string]string{ck3: "gfx/coat_of_arms/coa_mask_texture.dds"}

// CoatFrameMask is where a named frame's mask is (common/culture/cultures/_cultures.info).
const CoatFrameMask = "gfx/interface/coat_of_arms/frames/%s_mask.dds"

// CoatFrames are the shapes each game's coat designer previews a coat in, in its order (CK3's
// gui/shared/coa_designer.gui and its types in gui/shared/coat_of_arms.gui; the unit's banner is 3D and left out); a
// game with no designer has none.
var CoatFrames = map[string][]CoatFrame{
	ck3: {
		{Label: "Dynasty", Define: "DEFAULT_DYNASTY_COA_FRAME_NAME"},
		{Label: "House", Define: "DEFAULT_HOUSE_COA_FRAME_NAME"},
		{Label: "Title", Mask: "gfx/interface/coat_of_arms/title_mask.dds"},
		{Label: "Realm", Mask: "gfx/interface/icons/realm_frames/_default_mask.dds"},
	},
}
