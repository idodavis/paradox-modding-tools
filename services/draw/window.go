// Package draw builds what the Visual view shows. It resolves a GUI window
// into its widgets and a coat of arms into its layers, and it makes the text
// edits the view's panels ask for.
package draw

import (
	"cmp"
	"errors"
	"iter"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/internal/loc"
	"paradox-modding-tools/services/lsp"
	"paradox-modding-tools/services/workspace"
)

// Service is the Wails service behind the Visual view. A change to a file the
// editor may have open is returned as edits for the editor to apply.
type Service struct {
	Workspace *workspace.Service

	// mu guards opening.
	mu      sync.Mutex
	opening opening
}

// Property is one property of a widget, with the place it is written, which
// may be a template or the widget's type rather than the widget itself.
type Property struct {
	Key string `json:"key"`

	// Value is what the game reads: a constant, an expression or a GetDefine
	// call is replaced by its value.
	Value string `json:"value"`

	Range jomini.Range `json:"range"`

	// Place is the position of the property's file among the window's places.
	Place int32 `json:"place"`

	// Origin is "widget", "template" or "type", or "missing" for a type or a
	// template that no file defines.
	Origin string `json:"origin"`

	// Writer is the type or template whose block holds the property, "" for
	// the widget's own. Source says the same in words.
	Writer string `json:"writer"`
	Source string `json:"source"`
}

// Widget is one element of a resolved window, with everything the page needs
// to draw it.
type Widget struct {
	ID int32 `json:"id"`

	// Parent is the widget that holds it. The window's own widget is 0, and
	// is its own parent.
	Parent int32 `json:"parent"`

	Name string `json:"name"`
	Type string `json:"type"`

	// Row is the game's row for its built-in type.
	Row lang.WidgetType `json:"row"`

	// Builtin is the built-in type its type chain ends in, such as hbox.
	Builtin string `json:"builtin"`

	// TypeDefined is whether a file defines its type, so it can be opened.
	TypeDefined bool `json:"typeDefined"`

	// Slot is the slot key it fills, such as scrollbar_vertical, and Role is
	// what that slot is to its holder. Both are "" for a plain child.
	Slot string `json:"slot"`
	Role string `json:"role"`

	// Item is whether it is drawn once per item of a data model.
	Item bool `json:"item"`

	// Properties are positions in the window's Properties, one per key.
	Properties []int32 `json:"properties"`

	// Texts, Textures and Colors hold, by property key, what a property
	// shows: its text with its data calls, its image file, and the color of
	// the definition its binding names.
	Texts    map[string][]loc.Segment `json:"texts"`
	Textures map[string]string        `json:"textures"`
	Colors   map[string]string        `json:"colors"`

	// Placeholder is the page's shape drawn where its picture has no file, ""
	// where none is drawn.
	Placeholder string `json:"placeholder"`

	// Pictures lists the pictures its binding may draw where the binding as
	// written names none; the widget draws the first. It is nil otherwise.
	Pictures *PictureList `json:"pictures,omitempty"`

	// Overlays are positions in the window's Overlays.
	Overlays []int32 `json:"overlays"`

	Range jomini.Range `json:"range"`

	// Place is the position of its file among the window's places.
	Place int32 `json:"place"`
}

// Overlay is a texture laid over a widget's own by a modify_texture block.
type Overlay struct {
	// Path is the texture's image file, "" if none is found.
	Path string `json:"path"`

	// Properties are positions in the window's Properties.
	Properties []int32 `json:"properties"`
}

// Missing is a type or a template that a widget uses and no file defines.
type Missing struct {
	Widget int32 `json:"widget"`

	// Text is the key and the name, such as "using button_sort". Detail is
	// the widget's name, else its type.
	Text   string `json:"text"`
	Detail string `json:"detail"`
}

// Window is one GUI window resolved for drawing. Its widgets are in the order
// written. Holders are the game windows it is laid out in, outermost first,
// each as its root widget alone.
type Window struct {
	Name    string   `json:"name"`
	Widgets []Widget `json:"widgets"`
	Holders []Widget `json:"holders"`

	// Places are the files its widgets and holders are written in, each once.
	Places []workspace.Place `json:"places"`

	// Fonts are every style of the fonts its widgets name.
	Fonts []Font `json:"fonts"`

	// Rows holds the rows each data model's list draws, by the model's item.
	// An item that reads no database is left out.
	Rows map[int32][]Row `json:"rows"`

	// Conditions holds the condition each widget is shown on, as written. A
	// widget always shown is left out.
	Conditions map[int32][]string `json:"conditions"`

	Missing []Missing `json:"missing"`

	// Groups are what the States panel can switch: the groups the conditions
	// ask about, then the picture lists, then the tiers.
	Groups []Group `json:"groups"`

	// TextFont and TextSize are what a text that sets neither is drawn in, ""
	// where the game sets none.
	TextFont string `json:"textFont"`
	TextSize string `json:"textSize"`

	// Properties and Overlays are shared by the widgets, the holders and the
	// overlays, which point into them, so each is sent once.
	Properties []Property `json:"properties"`
	Overlays   []Overlay  `json:"overlays"`

	// calls holds the data call each of Properties writes, nil for a value
	// that is none.
	calls []*jomini.Call
}

// Setting is one property to set in a block.
type Setting struct {
	// Block is where the block starts.
	Block jomini.Range `json:"block"`

	Key   string `json:"key"`
	Value string `json:"value"`
}

// opening is the window the service keeps: what it was resolved from, its
// evaluator at the values it opens with, and the lookups later calls reuse.
type opening struct {
	mods *index.Index

	// edits is the session's count of editor changes when the window was
	// resolved; a later change makes the window stale.
	edits int64

	path      string
	name      string
	evaluator *evaluator
	library   *library

	// colorLists holds the lists of each widget's color bindings, by widget.
	colorLists map[int32][]PictureList

	pictures []Group
	tiers    []Group
}

// ListDrawables lists the windows, types, templates and coats a file defines,
// with the one the view opens on first. A file that is not text has none.
func (s *Service) ListDrawables(path string) ([]lsp.Symbol, error) {
	ws, err := s.Workspace.Session()
	if err != nil {
		return nil, err
	}

	symbols, err := lsp.ListFileSymbols(ws, resolveIndex(ws, path), path)
	if errors.Is(err, files.ErrBinary) {
		return []lsp.Symbol{}, nil
	}

	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	drawn := listDrawn(ws.Schema, symbols)
	if first := s.findFirstDrawn(ws, path, drawn); first > 0 {
		drawn = slices.Concat(drawn[first:first+1], drawn[:first], drawn[first+1:])
	}

	return drawn, nil
}

// findFirstDrawn returns the position of the drawable a GUI file opens on:
// the one named as the file is, else its first window, else the first that
// draws something. A window it opens to test that is kept, so the view's next
// ask for it is free. It runs under the service's lock.
func (s *Service) findFirstDrawn(ws *workspace.Session, path string, drawn []lsp.Symbol) int {
	stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if named := slices.IndexFunc(drawn, func(symbol lsp.Symbol) bool { return strings.EqualFold(symbol.Name, stem) }); named >= 0 {
		return named
	}

	if window := slices.IndexFunc(drawn, func(symbol lsp.Symbol) bool { return symbol.Database == lang.GUIWidgetDatabase }); window >= 0 {
		return window
	}

	for position, symbol := range drawn {
		if symbol.Database == lang.CoatDatabase {
			return 0
		}

		if opened, err := s.openWindow(ws, path, symbol.Name); err == nil && drawsSomething(&opened.evaluator.window) {
			return position
		}
	}

	return 0
}

// sizeKey is the key that sets a widget's size.
const sizeKey = "size"

// drawsSomething reports whether a resolved window draws anything.
func drawsSomething(window *Window) bool {
	root := window.Widgets[0]

	return len(window.Widgets) > 1 || isDrawing(window, root) || findProperty(window.listProperties(root), sizeKey).Value != ""
}

// listDrawn returns the symbols the view can draw, nested ones included, in
// the order written.
func listDrawn(schema *lang.Schema, symbols []lsp.Symbol) []lsp.Symbol {
	drawn := []lsp.Symbol{}
	for _, symbol := range symbols {
		nested := symbol.Children
		symbol.Children = nil
		if isDrawn(schema, symbol.Database) || symbol.Database == lang.CoatDatabase {
			drawn = append(drawn, symbol)
		}

		drawn = append(drawn, listDrawn(schema, nested)...)
	}

	return drawn
}

// isDrawn reports whether a database holds what the view draws as a window:
// a window, a type or a template.
func isDrawn(schema *lang.Schema, database string) bool {
	return database == lang.GUIWidgetDatabase || slices.Contains(schema.Callees[lang.GUITagCalls], database) ||
		slices.Contains(schema.Callees[lang.GUITemplateCalls], database)
}

// ResolveWindow resolves one window of a GUI file for drawing. A window asked
// for again is the kept one until a file, the editor's text or the rows
// setting changes. It returns an error if the file is not in the workspace or
// does not define the window.
func (s *Service) ResolveWindow(path, name string) (Window, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	opened, err := s.findWindow(path, name)
	if err != nil {
		return Window{}, err
	}

	window := opened.evaluator.window
	window.Rows = listRows(opened)

	return window, nil
}

// rowsSetting is the setting of how many rows a data model's list draws.
const rowsSetting = "draw.rows"

// lookupRows returns how many rows a data model's list draws.
func (s *Service) lookupRows(ws *workspace.Session) int {
	setting, _ := s.Workspace.Config.LookupSetting(ws.ID, rowsSetting)
	count, _ := strconv.Atoi(setting)

	return max(count, 0)
}

// resolveIndex returns the index a file is drawn from: the game's own for a
// file of the game, so it draws as the game does with no mod loaded, else the
// mods'.
func resolveIndex(ws *workspace.Session, path string) *index.Index {
	mods := ws.Mods.Load()
	if root, ok := mods.FindRoot(path); ok && root.Origin == files.OriginVanilla {
		return ws.Install
	}

	return mods
}

// findWindow returns a window of a GUI file as it opens, the kept one if
// nothing has changed. It runs under the service's lock.
func (s *Service) findWindow(path, name string) (opening, error) {
	ws, err := s.Workspace.Session()
	if err != nil {
		return opening{}, err
	}

	kept := s.opening
	if kept.isCurrent(ws, resolveIndex(ws, path)) && kept.path == path && kept.name == name &&
		kept.evaluator.rows == s.lookupRows(ws) {
		return kept, nil
	}

	return s.openWindow(ws, path, name)
}

// isCurrent reports whether the opening was made from the given index and
// from the editor's text as it is now, so what it looked up still holds.
func (o opening) isCurrent(ws *workspace.Session, mods *index.Index) bool {
	return o.mods == mods && o.edits == ws.Edits.Load()
}

// findLibrary returns the kept window's library while it is current for the
// index, else a new one. It runs under the service's lock.
func (s *Service) findLibrary(ws *workspace.Session, mods *index.Index) *library {
	if s.opening.isCurrent(ws, mods) {
		return s.opening.library
	}

	return makeLibrary(ws, mods)
}

// hiddenKeys are the keys whose no hides a widget. The first one written
// with a value decides.
var hiddenKeys = []string{"visible", "visible_at_creation"}

// openWindow resolves a window of a GUI file as it opens, without its rows,
// and keeps it. While the kept window is current, its library and its text
// font are reused. It runs under the service's lock.
func (s *Service) openWindow(ws *workspace.Session, path, name string) (opening, error) {
	mods, edits := resolveIndex(ws, path), ws.Edits.Load()
	last, library := s.opening, s.findLibrary(ws, mods)
	isCurrent := last.isCurrent(ws, mods)
	resolver := makeResolver(ws, mods, path, library)
	window, err := resolver.resolveWindow(path, name)
	if err != nil {
		return opening{}, err
	}

	if isCurrent {
		window.TextFont, window.TextSize = last.evaluator.window.TextFont, last.evaluator.window.TextSize
	} else {
		window.TextFont, window.TextSize = resolver.findTextFont()
	}

	window.Places, window.Fonts = resolver.places.values, findFonts(ws, mods, &window)
	for _, widget := range window.Widgets {
		for property := range window.listProperties(widget) {
			if property.Origin == originMissing {
				window.Missing = append(window.Missing, Missing{
					Widget: widget.ID, Text: property.Key + " " + property.Value, Detail: cmp.Or(widget.Name, widget.Type),
				})
			}
		}
	}

	evaluator := openEvaluator(ws.Schema, window, resolver.contexts, s.lookupRows(ws))
	s.opening = opening{
		mods: mods, edits: edits, path: path, name: name, evaluator: evaluator, library: library,
		colorLists: resolver.colorLists,
		pictures:   listPictureGroups(window), tiers: evaluator.listTierGroups(titleTiers[ws.Game.ID]),
	}
	evaluator.window.Groups = slices.Concat(evaluator.groups, s.opening.pictures, s.opening.tiers)
	for position := range evaluator.window.Groups {
		group := &evaluator.window.Groups[position]
		group.Captions, group.Box = listCaptions(*group), chooseBox(*group, window.Widgets)
	}

	return s.opening, nil
}

// findCondition returns the property that says whether a widget is shown,
// and its data call: the first of hiddenKeys the widget writes with a value.
// The window's own widget is always drawn, so it has none.
func (w *Window) findCondition(widget Widget) (Property, *jomini.Call) {
	if widget.ID == 0 {
		return Property{}, nil
	}

	for _, key := range hiddenKeys {
		if property, call := w.findCall(widget, key); property.Value != "" {
			return property, call
		}
	}

	return Property{}, nil
}

// findCall returns the property a widget writes under a key, letter case
// aside, and its data call, which is nil for a value that is none. It returns
// the zero Property if the widget writes no such key.
func (w *Window) findCall(widget Widget, key string) (Property, *jomini.Call) {
	for _, position := range widget.Properties {
		if property := w.Properties[position]; strings.EqualFold(property.Key, key) {
			return property, w.calls[position]
		}
	}

	return Property{}, nil
}

// findTextFont returns the font and the size the game's text type sets, ""
// for either it does not.
func (r *resolver) findTextFont() (font, size string) {
	definition, ok := r.findCalled(clause{statement: absentStatement}, lang.GUITagCalls, fontTextType)
	if !ok {
		return "", ""
	}

	window, err := makeResolver(r.ws, r.mods, definition.path, r.library).resolveWindow(definition.path, fontTextType)
	if err != nil {
		return "", ""
	}

	properties := window.listProperties(window.Widgets[0])

	return findProperty(properties, lang.GUIFontKey).Value, findProperty(properties, fontSizeKey).Value
}

// listProperties yields the properties of one of the window's widgets.
func (w *Window) listProperties(widget Widget) iter.Seq[Property] {
	return func(yield func(Property) bool) {
		for _, position := range widget.Properties {
			if !yield(w.Properties[position]) {
				return
			}
		}
	}
}

// EditProperties returns the edits that make the given settings in a file. A
// key the block writes has its value replaced; the others are added together
// at the block's end. It returns an error if the file is not in the workspace
// or no block starts where a setting says.
func (s *Service) EditProperties(path string, settings []Setting) ([]lsp.Edit, error) {
	var tree *jomini.Tree
	var edits []jomini.Edit
	var holders []int32
	lines := map[int32][]string{}
	for _, setting := range settings {
		var holder int32
		var err error
		if tree, holder, err = s.findBlock(path, setting.Block); err != nil {
			return nil, err
		}

		if field, ok := findField(listChildren(clause{tree: tree, statement: holder}), strings.ToLower(setting.Key)); ok {
			edits = append(edits, jomini.Edit{Span: tree.Statements[field.statement].ResolveValueSpan(), Text: setting.Value})

			continue
		}

		if _, ok := lines[holder]; !ok {
			holders = append(holders, holder)
		}

		lines[holder] = append(lines[holder], setting.Key+" = "+setting.Value)
	}

	if tree == nil {
		return []lsp.Edit{}, nil
	}

	for _, holder := range holders {
		edits = append(edits, tree.BuildInsertion(holder, lines[holder]))
	}

	return lsp.ToEdits(tree.Src, tree.Lines, edits), nil
}
