// draw.go is what the visual editors draw: the types of the one flat list Go resolves from a file.

// Package draw is what the visual editors draw: one flat List Go resolves from a file, which the page draws and
// computes nothing over. A GUI file resolves in two passes, resolve then place; the page reads each element's box,
// texture and text and nothing else.
package draw

import "paradox-modding-tools/services/internal/lsp"

// List is one render of a file's entry: what to draw, its structure, and what nothing found.
type List struct {
	Width    int       `json:"width"`  // the scene in pixels: a window's resolution, or the size of an entry drawn alone
	Height   int       `json:"height"` // the scene in pixels
	Elements []Element `json:"elements"`
	Parts    []Part    `json:"parts"`
	Fonts    []Font    `json:"fonts"`
	Missing  []string  `json:"missing"` // what was not found, each a sentence the status line shows
}

// Element is one painted rectangle, in paint order, at an absolute box.
type Element struct {
	Part    int      `json:"part"`
	Box     Box      `json:"box"`
	Clip    *Box     `json:"clip,omitempty"`
	Alpha   float64  `json:"alpha"`
	Texture *Texture `json:"texture,omitempty"`
	Text    *Text    `json:"text,omitempty"`
}

// Texture is what an element paints from a file: the route's URL, the part drawn, the slicing, the tint and blend.
type Texture struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`  // the image's pixels
	Height int    `json:"height"` // the image's pixels
	Frame  Box    `json:"frame"`  // the part drawn; the whole image when zero
	Border [4]int `json:"border"` // spriteborder insets top, right, bottom, left; zero is stretched
	Tiled  bool   `json:"tiled"`  // corneredtiled
	Mirror bool   `json:"mirror"`
	Fit    string `json:"fit"`            // fittype as written, lower-cased
	Tint   string `json:"tint"`           // a CSS color; "" none
	Blend  string `json:"blend"`          // blend_mode as a CSS blend mode
	Mask   string `json:"mask,omitempty"` // the URL of the texture that masks this one
}

// Text is lines of runs, already broken and measured; Font names a Fonts row's Name.
type Text struct {
	Font  string `json:"font"`
	Size  int    `json:"size"`
	Color string `json:"color"`
	Lines []Line `json:"lines"`
}

// Line is one line of a text, at its top's offset from the element's top.
type Line struct {
	Y    float64 `json:"y"`
	Runs []Run   `json:"runs"`
}

// Run is one stretch of a line under one style, at its offset from the element's left; Icon is a text icon's URL.
type Run struct {
	X      float64 `json:"x"`
	Text   string  `json:"text"`
	Bold   bool    `json:"bold"`
	Italic bool    `json:"italic"`
	Color  string  `json:"color"`
	Icon   string  `json:"icon"`
}

// Part is one widget of the entry, drawn or not, with where it is written.
type Part struct {
	Parent   int          `json:"parent"` // -1 at the root
	Key      string       `json:"key"`    // the statement's key: hbox, text_single
	Kind     string       `json:"kind"`   // the built-in kind it resolves to
	Name     string       `json:"name"`   // name = "…" where there is one
	Location lsp.Location `json:"location"`
}

// Font is one face the texts use: the game's font name, the style the file is for, and the file route's URL.
type Font struct {
	Name  string `json:"name"`
	Style string `json:"style"`
	URL   string `json:"url"`
}

// Box is a rectangle in scene pixels.
type Box struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// Property is one resolved property of a part, as written, with the type or template that wrote it ("" the part
// itself).
type Property struct {
	Key       string `json:"key"`
	Value     string `json:"value"`
	WrittenBy string `json:"writtenBy"`
}
