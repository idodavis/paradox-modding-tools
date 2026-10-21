package draw

import (
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/services/lsp"
	"paradox-modding-tools/services/workspace"
)

// An Instance is one placement of a layer on the field. Values the file does not set take the game's defaults.
type Instance struct {
	Position [2]float64  `json:"position"`
	Scale    [2]float64  `json:"scale"`
	Rotation float64     `json:"rotation"`
	Depth    float64     `json:"depth"`
	Span     jomini.Span `json:"span"`
}

// A Layer is one layer of a coat: the pattern, an emblem or a sub-coat. It carries the image file found for its
// texture, its mask, and its colors as CSS.
type Layer struct {
	Role      string      `json:"role"`    // "pattern", "emblem" or "sub"
	Texture   string      `json:"texture"` // as written; "missing:x" when `list "x"` names a list that does not exist
	Path      string      `json:"path"`    // the image file for the texture
	Mask      string      `json:"mask"`
	Colors    []string    `json:"colors"`
	Instances []Instance  `json:"instances"`
	Sub       *Coat       `json:"sub,omitempty"`
	Span      jomini.Span `json:"span"`
	Statement int32       `json:"statement"` // index of the layer's statement in the file's tree, used by the panel's edits
}

// A Coat is a coat of arms resolved for drawing: its layers, and its frame and mask after the game's override
// settings are applied.
type Coat struct {
	Name   string          `json:"name"`
	Layers []Layer         `json:"layers"`
	Frame  string          `json:"frame"`
	Mask   string          `json:"mask"`
	Span   jomini.Span     `json:"span"`
	Place  workspace.Place `json:"place"`
}

// A NamedColor is one of the game's named colors, with its CSS value for the color picker.
type NamedColor struct {
	Name string `json:"name"`
	CSS  string `json:"css"`
}

// ResolveCoat resolves one coat of arms in a file into its layers. When a layer's value is a `list "x"`, the
// option drawn is the one picked for that layer and key, or the heaviest option in the list if nothing was picked.
// It returns an error if the file is not in the workspace or does not define the coat.
func (drw *Service) ResolveCoat(path, name string, picks map[string]string) (Coat, error) {
	panic("stub")
}

// ListColors returns the game's named colors for the color picker.
func (drw *Service) ListColors() []NamedColor { panic("stub") }

// ListTextures returns the texture files that a layer with the given role can use, from the game's coat folders.
func (drw *Service) ListTextures(role string) []string { panic("stub") }

// ListFrames returns the frame files the game ships, for the Frame menu.
func (drw *Service) ListFrames() []string { panic("stub") }

// AddLayer inserts a new layer into a coat after the given layer, or first if the index is -1, and returns the
// edits that do it. The new layer is the game's template for that role.
func (drw *Service) AddLayer(path, coat, role string, after int) ([]lsp.Edit, error) {
	panic("stub")
}

// MoveLayer moves one layer of a coat to a new position and returns the edits that do it.
func (drw *Service) MoveLayer(path, coat string, from, to int) ([]lsp.Edit, error) {
	panic("stub")
}

// RemoveLayer deletes one layer of a coat, including its line, and returns the edits that do it.
func (drw *Service) RemoveLayer(path, coat string, layer int) ([]lsp.Edit, error) {
	panic("stub")
}

// SetLayerValue sets one key of a layer and returns the edits that do it. The key can be the texture, a color, or
// an instance's position, scale, rotation or depth. If the layer does not set the key yet, the edit adds it.
func (drw *Service) SetLayerValue(path, coat string, layer int, key, value string) ([]lsp.Edit, error) {
	panic("stub")
}

// CreateCoatFile writes a new coat of arms file into a mod, from the game's coat template, and returns its path.
func (drw *Service) CreateCoatFile(modID, name string) (string, error) {
	panic("stub")
}
