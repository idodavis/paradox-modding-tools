// Package draw builds what the Visual view shows. It resolves a GUI window into its widgets and a coat of arms
// into its layers, and it produces the text edits the view's panels make.
package draw

import (
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/services/lsp"
	"paradox-modding-tools/services/workspace"
)

// Service is the Wails service behind the Visual view. Methods that change a file do not write it. They return
// edits, which the editor applies to its own model, so there is only ever one copy of the text.
type Service struct {
	Workspace *workspace.Service
}

// A Property is one property of a widget. It records the value as written and where it was written, because a
// template or the widget's type can set a property instead of the widget itself.
type Property struct {
	Key    string          `json:"key"`
	Value  string          `json:"value"`
	Span   jomini.Span     `json:"span"`
	Place  workspace.Place `json:"place"`
	Origin string          `json:"origin"` // "widget", "template" or "type"; "missing" if the source was not found
}

// A Widget is one element of a resolved window with everything needed to draw it: the game's row for its type,
// its properties after its type and templates are applied, and its texts and textures looked up. Go does not
// measure or lay out anything; the page does.
type Widget struct {
	ID         int32             `json:"id"`
	Parent     int32             `json:"parent"` // -1 means the window itself
	Name       string            `json:"name"`
	Type       string            `json:"type"`
	Row        game.WidgetType   `json:"row"`
	Role       string            `json:"role"` // the slot it fills in its holder, or "" for a plain child
	Properties []Property        `json:"properties"`
	Texts      map[string]string `json:"texts"`    // by property key; a data binding shows as […]
	Textures   map[string]string `json:"textures"` // by property key; the image file found for the written path
	Span       jomini.Span       `json:"span"`
	Place      workspace.Place   `json:"place"`
}

// A Window is a GUI window resolved for drawing: its widgets in document order, the game windows that contain it,
// and the data context its bindings read.
type Window struct {
	Name        string   `json:"name"`
	Widgets     []Widget `json:"widgets"`
	Holders     []string `json:"holders"`
	Datacontext string   `json:"datacontext"`
}

// A Drawable is something in a GUI or coat file that the view can draw: a window, a type, a template or a coat,
// and where it starts in the file.
type Drawable struct {
	Name     string      `json:"name"`
	Database string      `json:"database"`
	Span     jomini.Span `json:"span"`
	Line     int         `json:"line"`
}

// ListDrawables lists what a file defines that the view can draw, in document order. A file that is not a GUI
// file or a coat file has nothing to draw.
func (drw *Service) ListDrawables(path string) ([]Drawable, error) { panic("stub") }

// ResolveWindow resolves one window in a GUI file. It applies the window's type and templates, looks up its
// texts and finds its textures. It returns an error if the file is not in the workspace or does not define the
// window. A type or template that cannot be found becomes a Property with Origin "missing", so the page can
// flag it.
func (drw *Service) ResolveWindow(path, name string) (Window, error) { panic("stub") }

// EditProperty changes one property of a widget and returns the edits that make the change. If the widget
// already sets the property, the edit replaces its value. Otherwise the edit adds the property at the end of the
// widget's block.
func (drw *Service) EditProperty(path string, widget int32, key, value string) ([]lsp.Edit, error) {
	panic("stub")
}

// resolveText returns the text to show for a property value. A localization key becomes its text, a macro is
// expanded, and a data binding shows as […] because bindings are never evaluated.
func resolveText(ws *workspace.Session, value string) string { panic("stub") }
