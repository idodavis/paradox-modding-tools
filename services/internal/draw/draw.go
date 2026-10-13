// draw.go is what the visual editors draw: the types of the list of elements Go resolves from a file.

// Package draw is what the visual editors draw: a GUI file's entry resolved into one List of elements, each with the
// words it is laid out by as written, its textures and its text, which the page nests as elements for the browser to
// lay out. Nothing here measures or places: the browser's layout is the engine, and the page's table says what each
// word means to it. It decides nothing about script (the schema's walker did), and it writes nothing.
package draw

import (
	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lsp"
)

// List is one render of a file's entry: its elements, pre-order, the fonts their texts use, and what nothing found.
type List struct {
	Width    int       `json:"width"`  // the scene in pixels: a window's resolution; zero for an entry drawn alone, its own size
	Height   int       `json:"height"` // the scene in pixels
	Elements []Element `json:"elements"`
	Fonts    []Font    `json:"fonts"`
	Missing  []string  `json:"missing"` // what was not found, each a sentence the status line shows
}

// Element is one widget of the entry, with where it is written and what the page lays it out and draws by.
type Element struct {
	Parent   int               `json:"parent"` // -1 at the root
	Key      string            `json:"key"`    // the statement's key: hbox, text_single
	Kind     string            `json:"kind"`   // the built-in kind it resolves to
	Name     string            `json:"name"`   // name = "…" where there is one
	Location lsp.Location      `json:"location"`
	Role     game.WidgetRole   `json:"role"`     // "" a child among its siblings; skin or chrome
	Hidden   bool              `json:"hidden"`   // visible = no: the widget keeps its place and shows nothing
	Rows     int               `json:"rows"`     // a data model item's widget is drawn once a row; 0 for the rest
	Pair     int               `json:"pair"`     // the earlier sibling whose place this element shares, the game showing one of them; -1 none
	Layout   map[string]string `json:"layout"`   // the layout words as written, by lower-cased key
	Textures []Texture         `json:"textures"` // the widget's texture, then each overlay
	Text     *Text             `json:"text,omitempty"`
}

// Texture is what an element paints from a file: the route's URL, the part of the image drawn, the slicing, the tint
// and blend.
type Texture struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`  // the image's pixels
	Height int    `json:"height"` // the image's pixels
	Frame  Box    `json:"frame"`  // the part of the image drawn, in its pixels; the whole image when zero
	Border [4]int `json:"border"` // spriteborder insets top, right, bottom, left; zero is stretched
	Tiled  bool   `json:"tiled"`  // corneredtiled
	Mirror bool   `json:"mirror"`
	Fit    string `json:"fit"`   // fittype as written, lower-cased
	Tint   string `json:"tint"`  // a CSS color; "" none
	Blend  string `json:"blend"` // blend_mode as a CSS blend mode
	// the texture whose alpha masks this one, drawn as its own URL, frame and fit say: an overlay is masked by the
	// widget's texture, the widget's texture by a mask overlay; nil for none
	Mask *Texture `json:"mask,omitempty"`
}

// Text is an element's text as runs, in a font the Fonts name, at the size written; the browser breaks its lines.
type Text struct {
	Font  string `json:"font"`
	Size  int    `json:"size"`
	Color string `json:"color"`
	Runs  []Run  `json:"runs"`
}

// Run is one stretch of a text under one style; Icon is a text icon's URL, drawn in place of text.
type Run struct {
	Text   string `json:"text"`
	Bold   bool   `json:"bold"`
	Italic bool   `json:"italic"`
	Color  string `json:"color"`
	Icon   string `json:"icon"`
}

// Font is one face the texts use: the game's font name, the style the file is for, and the file route's URL.
type Font struct {
	Name  string `json:"name"`
	Style string `json:"style"`
	URL   string `json:"url"`
}

// Box is a rectangle in an image's pixels.
type Box struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// Property is one resolved property of an element, as written, with the type or template that wrote it ("" the element
// itself).
type Property struct {
	Key       string `json:"key"`
	Value     string `json:"value"`
	WrittenBy string `json:"writtenBy"`
}
