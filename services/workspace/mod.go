package workspace

import (
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/services/app"
)

// A Descriptor is a mod's own description as its game writes it: CK3's `.mod` file, or Vic3's and EU5's
// `.metadata/metadata.json`.
type Descriptor struct {
	Path             string   `json:"path"`
	Name             string   `json:"name"`
	Version          string   `json:"version"`
	SupportedVersion string   `json:"supportedVersion"`
	Tags             []string `json:"tags"`
	ReplacePaths     []string `json:"replacePaths"` // folders whose earlier files the game does not load
	Thumbnail        string   `json:"thumbnail"`    // the picture file the descriptor names, from the mod's folder
	WorkshopID       string   `json:"workshopId"`
}

// A Mod is one mod of the open workspace: its record in the config, together with its descriptor as read from its
// folder.
type Mod struct {
	app.Mod
	Descriptor Descriptor `json:"descriptor"`
	Broken     bool       `json:"broken"` // its folder is missing
}

// ReadDescriptor reads a mod's descriptor from its folder, where the game's rule says it is.
func ReadDescriptor(game *game.Game, dir string) (Descriptor, error) { panic("stub") }

// WriteDescriptor writes a mod's descriptor back in the game's own form, keeping any fields it does not model.
func WriteDescriptor(game *game.Game, dir string, descriptor Descriptor) error { panic("stub") }

// createModFolder creates a new mod on disk: its folder, its descriptor and the folders the game expects.
func createModFolder(game *game.Game, dir, name string) (Descriptor, error) { panic("stub") }
