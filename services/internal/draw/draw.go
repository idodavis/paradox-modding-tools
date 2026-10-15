// draw.go is what the visual editors draw: the request, the list an entry resolves into, and the two calls that
// answer it from one resolve kept between them.

package draw

import (
	"fmt"
	"strings"
	"sync"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lsp"
	"paradox-modding-tools/services/internal/workspace"
)

// FileRoute is where the page reads a file: ?path= its absolute path and &v= when it changed.
const FileRoute = "/pmt/file"

// Request is what a render is asked for: the file, and the entry's id and database ("" for the first of the id). Its
// texts are in the workspace's language.
type Request struct {
	Path     string `json:"path"`
	ID       string `json:"id"`
	Database string `json:"database"`
}

// List is one render of an entry: the widgets the game lays it out in, its elements in pre-order, the textures they
// draw and the fonts they use, each listed once, the calls they are shown on, and what nothing found.
type List struct {
	IsAlone  bool      `json:"isAlone"` // a type, template, text icon or format drawn by itself, not a window
	Holders  []Element `json:"holders"` // outermost first, each of its type row and properties alone
	Elements []Element `json:"elements"`
	Textures []Texture `json:"textures"`
	Groups   []Group   `json:"groups"`
	Fonts    []Font    `json:"fonts"`
	Missing  []Absent  `json:"missing"`
}

// Element is one widget of the entry: where it is written and how it is named, how its type draws, and what it is
// drawn by: its properties as written, its textures, its text and what it is shown on.
type Element struct {
	Parent    int          `json:"parent"` // -1 at the root
	Key       string       `json:"key"`
	Type      string       `json:"type"` // the built-in type the key resolves to
	Name      string       `json:"name"`
	Label     string       `json:"label"` // the key, then the name where it has one of its own
	Path      string       `json:"path"`  // the labels from the window down
	Ancestors []int        `json:"ancestors"`
	Location  lsp.Location `json:"location"`

	Role   game.WidgetRole `json:"role"` // "" a child among its siblings
	IsItem bool            `json:"isItem"`

	Holds       game.Holding `json:"holds"`
	Clips       bool         `json:"clips"`
	Fills       bool         `json:"fills"`
	CentersText bool         `json:"centersText"`
	DropsHidden bool         `json:"dropsHidden"` // a hidden child of it takes no room

	IsHidden       bool              `json:"isHidden"` // visible = no, or visible_at_creation = no
	VisibleBinding string            `json:"visibleBinding"`
	Condition      *Condition        `json:"condition,omitempty"`
	Properties     map[string]string `json:"properties"` // by lower-cased key, the last written, as written
	Textures       []int             `json:"textures"`   // its own, then its overlays
	TextureBinding string            `json:"textureBinding"`
	Text           *Text             `json:"text,omitempty"`
}

// Texture is what an element paints from a file: its size in the image's pixels over its density, its border in
// the screen's, its frame, tiling, mirror, fit, tint and blend, and the texture that masks it.
type Texture struct {
	URL        string  `json:"url"`
	Width      int     `json:"width"`
	Height     int     `json:"height"`
	Density    float64 `json:"density"`
	Border     [4]int  `json:"border"` // top, right, bottom, left
	IsTiled    bool    `json:"isTiled"`
	IsMirrored bool    `json:"isMirrored"`
	Fit        string  `json:"fit"`
	Tint       string  `json:"tint"`      // the color the image is multiplied by, opaque; "" none
	TintAlpha  float64 `json:"tintAlpha"` // what the tint multiplies the image's alpha by; 1 none
	Blend      string  `json:"blend"`     // a CSS blend mode
	Alpha      float64 `json:"alpha"`
	Mask       int     `json:"mask"` // the list's texture whose brightness masks this one; -1 none
}

// Text is an element's text as runs in a font at a size.
type Text struct {
	Font  string `json:"font"`
	Size  int    `json:"size"`
	Color string `json:"color"`
	Runs  []Run  `json:"runs"`
}

// Run is one stretch of a text under one style: an icon drawn in place of text, or a data call drawn as a value.
type Run struct {
	Text     string `json:"text"`
	IsBold   bool   `json:"isBold"`
	IsItalic bool   `json:"isItalic"`
	Color    string `json:"color"`
	Icon     string `json:"icon"`
	Call     string `json:"call"`
}

// Font is one face the texts use: the game's font name, its style and its file.
type Font struct {
	Name  string `json:"name"`
	Style string `json:"style"`
	URL   string `json:"url"`
}

// Condition is what a widget is shown on: a fact, one of the list's groups at a value, or a boolean over conditions.
type Condition struct {
	Operator string      `json:"operator"` // "" a fact
	Operands []Condition `json:"operands,omitempty"`
	Group    int         `json:"group"`
	Value    string      `json:"value,omitempty"`
}

// Group is a call widgets are shown on, its last quoted argument blanked, and the values its facts are written with.
type Group struct {
	Call   string   `json:"call"`
	Values []string `json:"values"`
}

// Property is one property of an element as written, with the type or template that wrote it ("" the element) and
// where.
type Property struct {
	Key      string       `json:"key"`
	Value    string       `json:"value"`
	Writer   string       `json:"writer"`
	Location lsp.Location `json:"location"`
}

// Absent is a thing the game's files did not give: the status line's sentence and the element that asked (-1 none).
type Absent struct {
	Text    string `json:"text"`
	Element int    `json:"element"`
}

// Render is the last resolve of an entry, kept while the session's records and the request are the same.
type Render struct {
	mu      sync.Mutex
	changes int64
	request Request
	last    *resolver
}

// List is the request's entry resolved into a list. The caller holds the workspace's read lock.
func (render *Render) List(ws *workspace.Session, request Request) (*List, error) {
	render.mu.Lock()
	defer render.mu.Unlock()
	resolver, err := render.resolver(ws, request)
	if err != nil {
		return nil, err
	}
	return resolver.list, nil
}

// Properties is an element's properties: every key that applies to it but a state, its last value, its writer and
// where it is written, in the order the keys are first written.
func (render *Render) Properties(ws *workspace.Session, request Request, element int) ([]Property, error) {
	render.mu.Lock()
	defer render.mu.Unlock()
	resolver, err := render.resolver(ws, request)
	if err != nil {
		return nil, err
	}
	if element < 0 || element >= len(resolver.widgets) {
		return nil, fmt.Errorf("the entry has no element %d", element)
	}
	widget := resolver.widgets[element]
	properties := []Property{}
	keys := map[string]bool{}
	for _, statement := range widget.statements {
		key := strings.ToLower(statement.assignment.Key.Text)
		if key == stateKey || keys[key] {
			continue
		}
		keys[key] = true
		last, _ := widget.last(key)
		properties = append(properties, Property{
			Key: last.assignment.Key.Text, Value: last.text(), Writer: last.writer,
			Location: lsp.Locate(ws, last.path, last.span()),
		})
	}
	return properties, nil
}

// resolver is the request's resolve: the last where the request and the session's records are the same, else anew.
func (render *Render) resolver(ws *workspace.Session, request Request) (*resolver, error) {
	if render.last != nil && render.changes == ws.Changes && render.request == request {
		return render.last, nil
	}
	resolver, err := newResolver(ws, request)
	if err != nil {
		return nil, err
	}
	render.changes, render.request, render.last = ws.Changes, request, resolver
	return resolver, nil
}
