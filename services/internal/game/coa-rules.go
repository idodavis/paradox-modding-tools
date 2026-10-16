// coa-rules.go is the registry of what a game does with a coat of arms beyond its syntax: the databases it reads,
// where each game keeps its coats and their textures, the file that sizes its field, which way depth orders its layers,
// the texture that shades it and the frames its windows cut it to. A row from the wiki says nothing of it; one from
// vanilla observed in the app says observed.

package game

// CoatFrame is a shape a game's windows draw a coat in: what the picker calls it, and the mask whose alpha cuts the
// field to it.
type CoatFrame struct{ Label, Mask string }

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

// CoatFrames are the frames each game's windows cut its coats to, the masks its GUI files name as a coat_of_arms_mask
// (observed: a title's shield, a dynasty's frame); a game not listed draws its coats as their field, as its flags are,
// and the masks a binding picks (a realm's, a culture's house frame) are the game's to choose.
var CoatFrames = map[string][]CoatFrame{
	ck3: {
		{Label: "Title", Mask: "gfx/interface/coat_of_arms/title_mask.dds"},
		{Label: "Dynasty", Mask: "gfx/interface/coat_of_arms/frames/dynasty_mask.dds"},
	},
}
