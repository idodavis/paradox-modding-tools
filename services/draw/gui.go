// Package draw builds what the Visual view shows. It resolves a GUI window into its widgets and a coat of arms
// into its layers, and it produces the text edits the view's panels make.
package draw

import (
	"cmp"
	"errors"
	"fmt"
	"iter"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/samber/lo"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/loc"
	"paradox-modding-tools/internal/schema"
	"paradox-modding-tools/services/lsp"
	"paradox-modding-tools/services/workspace"
)

// Service is the Wails service behind the Visual view. It returns edits instead of writing files, and keeps the last
// window it resolved.
type Service struct {
	Workspace *workspace.Service
	mu        sync.Mutex // held by every call that reads or replaces the opening
	opening   opening
}

// A Property is one property of a widget, with the place where it is written. That place can be a template or the
// widget's type instead of the widget itself.
type Property struct {
	Key    string       `json:"key"`
	Value  string       `json:"value"` // what the game reads: a constant, an expression or a GetDefine call is worked out, also inside a block
	Range  jomini.Range `json:"range"`
	Place  int32        `json:"place"`  // the position of the file it is written in among its window's places
	Origin string       `json:"origin"` // "widget", "template" or "type"; "missing" if no file defines the source
	Writer string       `json:"writer"` // the type or template whose block it is written in; "" for no such block
	Source string       `json:"source"` // where it comes from, in words
}

// A Widget is one element of a resolved window, with everything needed to draw it. The page lays it out.
type Widget struct {
	ID          int32                    `json:"id"`
	Parent      int32                    `json:"parent"` // the widget that holds it; the window's own widget is 0, and so is its parent
	Name        string                   `json:"name"`
	Type        string                   `json:"type"`
	Row         game.WidgetType          `json:"row"`                // the game's row for its type
	Builtin     string                   `json:"builtin"`            // the engine's type its type comes down to, such as widget, hbox or icon
	TypeDefined bool                     `json:"typeDefined"`        // a file defines its type, which can then be opened; a built-in type is not defined
	Slot        string                   `json:"slot"`               // the key of the slot it fills, such as scrollbar_vertical; "" for none
	Role        string                   `json:"role"`               // the slot it fills in its holder, or "" for a plain child
	Item        bool                     `json:"item"`               // it is an item of a data model
	Properties  []int32                  `json:"properties"`         // the positions of its properties among its window's properties, the last written of each key
	Texts       map[string][]loc.Segment `json:"texts"`              // by property key, the plain text and the data calls of each text it shows
	Textures    map[string]string        `json:"textures"`           // by property key; the image file for the property's path
	Colors      map[string]string        `json:"colors"`             // by property key, the CSS color that a binding of a definition's color leads to
	Placeholder string                   `json:"placeholder"`        // the shape among the page's placeholders that it draws where its picture has no file; "" for a widget with a picture, a background or no picture
	Pictures    *PictureList             `json:"pictures,omitempty"` // where the pictures are listed that its picture binding may draw, where no file is found for the binding as written; it draws the first of them; nil means no list
	Overlays    []int32                  `json:"overlays"`           // the positions of the textures laid over its own among its window's overlays
	Range       jomini.Range             `json:"range"`
	Place       int32                    `json:"place"` // the position of the file it is written in among its window's places
}

// An Overlay is one texture laid over a widget's own, as a modify_texture block writes it.
type Overlay struct {
	Path       string  `json:"path"`       // the image file for its texture; "" if there is none
	Properties []int32 `json:"properties"` // the positions of its properties among its window's properties
}

// A Missing is one type or template that a widget of a window uses and no file defines.
type Missing struct {
	Widget int32  `json:"widget"` // the widget that uses it
	Text   string `json:"text"`   // the key that uses it and its name, such as "using button_sort"
	Detail string `json:"detail"` // the widget's name, else its type
}

// A Window is one GUI window resolved for drawing. Its widgets are in document order, and its holders are the game
// windows that contain it, outermost first, each its own widget alone.
type Window struct {
	Name       string             `json:"name"`
	Widgets    []Widget           `json:"widgets"`
	Holders    []Widget           `json:"holders"`
	Places     []workspace.Place  `json:"places"`     // the places of the files that its widgets and holders are written in, each once
	Fonts      []Font             `json:"fonts"`      // every style of the fonts its widgets name
	Rows       map[int32][]Row    `json:"rows"`       // by the item of a data model, the rows its list draws; an item whose widgets read no database is left out
	Conditions map[int32][]string `json:"conditions"` // by widget, the conditions it is shown on, as written; a widget shown on none is left out
	Missing    []Missing          `json:"missing"`    // the types and templates its widgets use that no file defines
	Groups     []Group            `json:"groups"`     // what the panel lets the user switch: the groups its widgets' conditions read, its lists of pictures, then its tiers; the value each is at is in States.Values
	TextFont   string             `json:"textFont"`   // the font of a text that sets none; "" if the game sets none
	TextSize   string             `json:"textSize"`   // the size of a text that sets none; "" if the game sets none
	Properties []Property         `json:"properties"` // the properties of its widgets, its holders and their overlays, each once
	Overlays   []Overlay          `json:"overlays"`   // the textures laid over its widgets' own, each once
}

// A Setting is one property to set in a block: the key and the value to write for it.
type Setting struct {
	Block jomini.Range `json:"block"` // starts where the block does
	Key   string       `json:"key"`
	Value string       `json:"value"`
}

// A clause is one statement of a file, with the type or template that wrote it where it is part of a widget.
type clause struct {
	tree      *jomini.Tree
	path      string
	statement int32  // its place in the tree; absentStatement where no statement is
	writer    string // the type or template that wrote it; "" for the widget's own block
	origin    string // what its properties come from: widget, template or type
	dropped   bool   // files define it, but the game keeps none of those definitions
}

// A child is a clause that a widget holds as a widget of its own, with the slot it fills.
type child struct {
	clause
	slot game.WidgetSlot // the slot it fills in its holder; the zero slot for a plain child
	item bool            // it is an item of a data model
}

// A gathering is what one widget collects from its own block and from the blocks of its types and templates.
type gathering struct {
	overrides map[string]clause // the blocks it replaces, by the block they replace
	children  []child
	missing   []Property          // the types and templates it uses that no file defines
	states    []clause            // the states it has
	overlays  []int32             // the positions of the textures it overlays among its window's overlays
	triggers  map[string][]string // by the name of a widget under it, the labels of the states that an opening call of this widget or one above it starts on that widget with FindChild and TriggerAnimation
}

// A resolver makes the widgets of one window and holds them until the window is done.
type resolver struct {
	ws          *workspace.Session
	mods        *index.Index
	file        int32                           // the file being drawn, by its position in the index
	isRead      bool                            // the game reads the drawn file, not another file at its path
	places      *table[string, workspace.Place] // by path; shared with the resolvers of the window's holders
	properties  *table[Property, Property]      // shared with the resolvers of the window's holders
	overlays    *table[string, Overlay]         // shared with the resolvers of the window's holders
	definitions map[string]clause               // a type's or template's definition, by caller, label and file
	ancestors   []clause
	library     *library // what the definitions show, shared with the resolvers of the window's holders
	gathered    gathering
	widgets     []Widget
	contexts    []context               // what each widget of the window reads its bindings in, by widget
	colorLists  map[int32][]PictureList // by widget, the lists of its bindings of a definition's color
}

// A table is a list of values that keeps each one once, by its key, in the order it is first added.
type table[K comparable, V any] struct {
	values    []V
	positions map[K]int32
}

// The property origins, the data call kinds and the deepest nesting.
const (
	originWidget   = "widget"
	originTemplate = "template"
	originType     = "type"
	originMissing  = "missing"
	kindNumber     = "number"
	kindText       = "text"
	kindDate       = "date"
	maxDepth       = 8

	minSample       = 1.0         // the least a sample number shows
	openingBrackets = "([{"       // the characters after which a data call's sample needs no space
	rowsSetting     = "draw.rows" // the setting of how many rows the Visual view draws of any data model's list

	absentStatement int32 = 0
)

// localizedKeys holds the keys whose value is a localization key.
var localizedKeys = sync.OnceValue(func() map[string]bool {
	keys := map[string]bool{}
	for _, declaration := range game.Declarations {
		for _, reference := range declaration.References {
			if declaration.Name == game.GUI.WidgetDatabase && reference.Target == game.Localization {
				keys[reference.KeyPath] = true
			}
		}
	}
	return keys
})

// ListDrawables lists the windows, types, templates and coats a file defines, the one the view opens on first. It
// lists none for a file that is not text.
func (drw *Service) ListDrawables(path string) ([]lsp.Symbol, error) {
	ws, err := drw.Workspace.Session()
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
	drawn := listDrawn(ws.Schema, symbols)
	if first := findFirstDrawn(ws, path, drawn); first > 0 {
		drawn = slices.Concat(drawn[first:first+1], drawn[:first], drawn[first+1:])
	}
	return drawn, nil
}

// findFirstDrawn returns the position of the drawable a GUI file opens on, or 0 if none draws anything.
func findFirstDrawn(ws *workspace.Session, path string, drawn []lsp.Symbol) int {
	stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if named := slices.IndexFunc(drawn, func(symbol lsp.Symbol) bool { return strings.EqualFold(symbol.Name, stem) }); named >= 0 {
		return named
	}
	if window := slices.IndexFunc(drawn, func(symbol lsp.Symbol) bool { return symbol.Database == game.GUI.ShownFirst }); window >= 0 {
		return window
	}
	mods := resolveIndex(ws, path)
	for position, symbol := range drawn {
		if symbol.Database == game.Coat.Database {
			return 0
		}
		if window, err := makeResolver(ws, mods, path).resolveWindow(path, symbol.Name); err == nil && drawsSomething(&window) {
			return position
		}
	}
	return 0
}

// drawsSomething reports whether a resolved window draws anything.
func drawsSomething(window *Window) bool {
	root := window.Widgets[0]
	return len(window.Widgets) > 1 || isDrawing(window, root) || findProperty(window.listProperties(root), game.GUI.SizeKey).Value != ""
}

// listDrawn returns the symbols the view can draw among the given ones and those nested in them, in document order.
func listDrawn(schema *schema.Schema, symbols []lsp.Symbol) []lsp.Symbol {
	drawn := []lsp.Symbol{}
	for _, symbol := range symbols {
		nested := symbol.Children
		symbol.Children = nil
		if isDrawn(schema, symbol.Database) || symbol.Database == game.Coat.Database {
			drawn = append(drawn, symbol)
		}
		drawn = append(drawn, listDrawn(schema, nested)...)
	}
	return drawn
}

// isDrawn reports whether a database holds what the view draws as a window: a window, a type or a template.
func isDrawn(schema *schema.Schema, database string) bool {
	return database == game.GUI.WidgetDatabase || slices.Contains(schema.Callees[game.GUI.TagCalls], database) ||
		slices.Contains(schema.Callees[game.GUI.TemplateCalls], database)
}

// ResolveWindow resolves one window in a GUI file. It applies the window's type and templates, looks up its texts and
// finds its textures. It returns an error if the file is not in the workspace or does not define the window.
func (drw *Service) ResolveWindow(path, name string) (Window, error) {
	ws, err := drw.Workspace.Session()
	if err != nil {
		return Window{}, err
	}
	drw.mu.Lock()
	defer drw.mu.Unlock()
	opened, err := drw.openWindow(ws, path, name)
	if err != nil {
		return Window{}, err
	}
	window := opened.evaluator.window
	window.Rows = listRows(opened)
	return window, nil
}

// lookupRows returns how many rows the Visual view draws of a data model's list.
func (drw *Service) lookupRows(ws *workspace.Session) int {
	setting, _ := drw.Workspace.Config.LookupSetting(ws.ID, rowsSetting)
	count, _ := strconv.Atoi(setting)
	return count
}

// resolveIndex returns the index a file is drawn from: the game's own for a game file, else the mods'.
func resolveIndex(ws *workspace.Session, path string) *index.Index {
	mods := ws.Mods.Load()
	if root, ok := mods.FindRoot(path); ok && root.Origin == files.OriginVanilla {
		return ws.Install
	}
	return mods
}

// findWindow returns a window of a GUI file as it opens, the kept one if nothing changed. It returns an error if no
// workspace is open or the file does not define the window. It runs under the service's lock.
func (drw *Service) findWindow(path, name string) (opening, error) {
	ws, err := drw.Workspace.Session()
	if err != nil {
		return opening{}, err
	}
	kept := drw.opening
	if kept.mods == resolveIndex(ws, path) && kept.path == path && kept.name == name &&
		kept.evaluator.rows == drw.lookupRows(ws) {
		return kept, nil
	}
	return drw.openWindow(ws, path, name)
}

// openWindow resolves a window of a GUI file as it opens, without its rows, and keeps it. It returns an error if the
// file does not define the window. It runs under the service's lock.
func (drw *Service) openWindow(ws *workspace.Session, path, name string) (opening, error) {
	mods := resolveIndex(ws, path)
	resolver := makeResolver(ws, mods, path)
	window, err := resolver.resolveWindow(path, name)
	if err != nil {
		return opening{}, err
	}
	window.TextFont, window.TextSize = resolver.findTextFont()
	window.Places, window.Fonts = resolver.places.values, findFonts(ws, mods, &window)
	window.Conditions = map[int32][]string{}
	for _, widget := range window.Widgets[1:] {
		for _, key := range game.GUI.HiddenKeys {
			if property := findProperty(window.listProperties(widget), key); property.Value != "" {
				window.Conditions[widget.ID] = []string{property.Value}
				break
			}
		}
	}
	for _, widget := range window.Widgets {
		for property := range window.listProperties(widget) {
			if property.Origin == originMissing {
				window.Missing = append(window.Missing, Missing{Widget: widget.ID, Text: property.Key + " " + property.Value,
					Detail: cmp.Or(widget.Name, widget.Type)})
			}
		}
	}
	evaluator := openEvaluator(ws.Schema, window, resolver.contexts, drw.lookupRows(ws))
	drw.opening = opening{
		mods: mods, path: path, name: name, evaluator: evaluator, library: resolver.library, colorLists: resolver.colorLists,
		pictures: listPictureGroups(window), tiers: evaluator.listTierGroups(ws.Game.GUI.Tiers),
	}
	evaluator.window.Groups = slices.Concat(evaluator.groups, drw.opening.pictures, drw.opening.tiers)
	for position := range evaluator.window.Groups {
		group := &evaluator.window.Groups[position]
		group.Captions, group.Box = listCaptions(*group), chooseBox(*group, window.Widgets)
	}
	return drw.opening, nil
}

// findTextFont returns the font and size of the game's text type. Each is "" if the game sets none.
func (resolver *resolver) findTextFont() (font, size string) {
	rules := game.GUI.Fonts
	definition, ok := resolver.findCalled(clause{statement: absentStatement}, game.GUI.TagCalls, rules.TextType)
	if !ok {
		return "", ""
	}
	window, err := makeResolver(resolver.ws, resolver.mods, definition.path).resolveWindow(definition.path, rules.TextType)
	if err != nil {
		return "", ""
	}
	properties := window.listProperties(window.Widgets[0])
	return findProperty(properties, rules.Key).Value, findProperty(properties, rules.SizeKey).Value
}

// makeResolver returns a resolver of the windows of one file, drawn from the given index.
func makeResolver(ws *workspace.Session, mods *index.Index, path string) *resolver {
	file, _ := mods.FindFile(path)
	return &resolver{ws: ws, mods: mods, file: file, isRead: mods.IsLoaded(file),
		places: &table[string, workspace.Place]{}, properties: &table[Property, Property]{}, overlays: &table[string, Overlay]{},
		definitions: map[string]clause{}, library: makeLibrary(ws, mods)}
}

// resolveWindow resolves the window a file defines under the given name. Unlike ResolveWindow, it leaves the window's
// places, fonts, conditions, missing types and groups to the caller.
func (resolver *resolver) resolveWindow(path, name string) (Window, error) {
	ws, mods := resolver.ws, resolver.mods
	definition, site, err := resolver.library.findDrawn(path, name,
		func(database string) bool { return database == game.GUI.WidgetDatabase },
		func(database string) bool { return isDrawn(ws.Schema, database) })
	if err != nil {
		return Window{}, err
	}
	root, key := resolver.findRoot(definition, mods.ToReference(site).Database, name)
	resolver.buildWidget(child{clause: root}, 0, key)
	widgets := resolver.widgets
	widgets[0].Name = name
	if root.statement == absentStatement {
		widgets[0].Range, widgets[0].Place = toRange(definition), resolver.locate(path)
	}
	// The holders come first, as resolving them adds to the properties the window takes below.
	window := Window{Name: name, Widgets: widgets, Holders: resolver.findHolders(key, root)}
	window.Properties, window.Overlays = resolver.properties.values, resolver.overlays.values
	resolver.contexts, resolver.colorLists = listContexts(ws.Schema, window), map[int32][]PictureList{}
	for id, context := range resolver.contexts {
		resolver.resolveBindings(&window, &window.Widgets[id], context)
	}
	return window, nil
}

// listProperties returns the properties of one of the window's widgets or holders.
func (window *Window) listProperties(widget Widget) iter.Seq[Property] {
	return func(yield func(Property) bool) {
		for _, position := range widget.Properties {
			if !yield(window.Properties[position]) {
				return
			}
		}
	}
}

// add returns the position of the value of a key, building the value with the given function the first time.
func (table *table[K, V]) add(key K, build func() V) int32 {
	if table.positions == nil {
		table.positions = map[K]int32{}
	}
	position, ok := table.positions[key]
	if !ok {
		position = int32(len(table.values))
		table.values = append(table.values, build())
		table.positions[key] = position
	}
	return position
}

// intern returns the positions among the window's properties of the given properties, adding each the first time.
func (resolver *resolver) intern(properties []Property) []int32 {
	return lo.Map(properties, func(property Property, _ int) int32 {
		return resolver.properties.add(property, func() Property { return property })
	})
}

// findDrawn returns the first definition in a file under the given label, with its site, letter case aside. Its
// database must pass the first of the given tests, and the next test is tried only if none does. It returns an error if
// the file is not in the index, defines the label in no database that passes, or has changed since it was scanned.
func (library *library) findDrawn(path, label string, tests ...func(database string) bool) (clause, index.Site, error) {
	file, ok := library.mods.FindFile(path)
	if !ok {
		return clause{}, index.Site{}, fmt.Errorf("the file %q is not in the workspace", path)
	}
	for _, accepts := range tests {
		for position, occurrence := range library.mods.Occurrences[file] {
			site := index.Site{File: file, Occurrence: int32(position)}
			reference := library.mods.ToReference(site)
			if occurrence.Role != index.RoleDefines || !accepts(reference.Database) || !strings.EqualFold(reference.Name, label) {
				continue
			}
			if definition, ok := makeClause(library.ws, library.mods, index.Winner{Site: site}); ok {
				return definition, site, nil
			}
			return clause{}, index.Site{}, fmt.Errorf("the file %q has changed since it was scanned", path)
		}
	}
	return clause{}, index.Site{}, fmt.Errorf("the file %q does not define %q", path, label)
}

// findRoot returns the clause a drawn definition is made of, and the type its widget has. A type is drawn as a widget
// of that type, and a template as a plain widget that uses it.
func (resolver *resolver) findRoot(definition clause, database, label string) (root clause, key string) {
	callees := resolver.ws.Schema.Callees
	key = definition.tree.Unquoted(definition.tree.Statements[definition.statement].Key)
	root = clause{tree: definition.tree, path: definition.path, statement: absentStatement}
	switch {
	case slices.Contains(callees[game.GUI.TagCalls], database):
		return root, key
	case slices.Contains(callees[game.GUI.TemplateCalls], database):
		root.writer = label
		return root, game.GUI.TagCalls
	}
	return definition, key
}

// locate returns the position of a file's place among the window's places.
func (resolver *resolver) locate(path string) int32 {
	return resolver.places.add(path, func() workspace.Place { return resolver.ws.Locate(resolver.mods, path) })
}

// buildWidget makes the widget that a clause writes, then the widgets inside it. An empty key takes the clause's own.
func (resolver *resolver) buildWidget(own child, parent int32, key string) {
	own.origin = originWidget
	if own.statement != absentStatement {
		key = cmp.Or(key, own.tree.Unquoted(own.tree.Statements[own.statement].Key))
	}
	slotKey := strings.ToLower(key)
	slot, slotted := game.GUI.Slots[slotKey]
	widgetType := cmp.Or(slot.Type, key)
	id := int32(len(resolver.widgets))
	resolver.widgets = append(resolver.widgets, Widget{
		ID: id, Parent: parent, Type: widgetType, Slot: lo.Ternary(slotted, slotKey, ""), Role: own.slot.Role,
		Item: own.item, Place: resolver.locate(own.path), Range: toRange(own.clause),
	})
	outer, outerAncestors := resolver.gathered, resolver.ancestors
	resolver.gathered = gathering{overrides: map[string]clause{}, triggers: map[string][]string{}}
	maps.Copy(resolver.gathered.overrides, outer.overrides)
	maps.Copy(resolver.gathered.triggers, outer.triggers)
	if own.statement != absentStatement {
		resolver.ancestors = append(resolver.ancestors, own.clause)
	}
	defer func() { resolver.gathered, resolver.ancestors = outer, outerAncestors }()
	builtin, chain := resolver.listTypes(widgetType, own.clause)
	resolver.widgets[id].TypeDefined = len(chain) > 0
	links := slices.Concat(chain, []clause{own.clause})
	resolver.listOverrides(links)
	var properties []Property
	for _, link := range links {
		if link.statement != absentStatement {
			properties = append(properties, resolver.resolveBlock(link, 0)...)
		}
	}
	properties = append(properties, resolver.resolveAlone(own.clause)...)
	name := findProperty(slices.Values(resolveProperties(properties)), game.GUI.WidgetKey).Value
	properties = append(properties, resolver.resolveOpening(name)...)
	resolver.resolveWidget(id, builtin, append(properties, resolver.gathered.missing...))
	resolver.resolveChildren(id)
}

// listTypes returns the built-in type that a type is made on, and the definitions in between, base first. An unknown
// type or a cycle is marked missing.
func (resolver *resolver) listTypes(label string, owner clause) (string, []clause) {
	builtin := label
	var chain []clause
	for range maxDepth {
		definition, ok := resolver.findCalled(owner, game.GUI.TagCalls, builtin)
		if !ok {
			if builtin != "" && !resolver.isBuiltin(owner, builtin) {
				resolver.gathered.missing = append(resolver.gathered.missing, resolver.makeMissing(game.GUI.TypeKey, builtin, owner))
			}
			return builtin, chain
		}
		chain = slices.Insert(chain, 0, definition)
		base := cmp.Or(definition.tree.Unquoted(definition.tree.Statements[definition.statement].Tag), game.GUI.TagCalls)
		if strings.EqualFold(base, builtin) {
			return builtin, chain
		}
		builtin = base
	}
	resolver.gathered.missing = append(resolver.gathered.missing, resolver.makeMissing(game.GUI.TypeKey, label, owner))
	return label, nil
}

// findCalled returns the definition the game keeps of a type or a template, or false if there is none. The caller is
// the word that names it: the widget's own word for a type, using for a template.
func (resolver *resolver) findCalled(owner clause, caller, label string) (clause, bool) {
	cacheKey := strings.Join([]string{caller, label, owner.path}, "\x00")
	definition, ok := resolver.definitions[cacheKey]
	if !ok {
		definition = resolver.findDeclared(owner, caller, label)
		resolver.definitions[cacheKey] = definition
	}
	definition.writer, definition.origin = label, lo.Ternary(caller == game.GUI.TagCalls, originType, originTemplate)
	return definition, definition.statement != absentStatement
}

// findDeclared returns the definition the game keeps of a label, local ones first. Its statement is absentStatement
// if the game keeps none, and dropped is set if files define the label but the game keeps none of them.
func (resolver *resolver) findDeclared(owner clause, caller, label string) clause {
	exists := false
	position, ok := resolver.mods.FindFile(owner.path)
	file := lo.Ternary(ok, position, -1)
	for _, database := range resolver.ws.Schema.Callees[caller] {
		sites := resolver.mods.ResolveLocal(schema.Reference{Database: database, Name: label}, file)
		if kept, ok := resolver.findWinner(sites, resolver.ws.Schema.Databases[database].Precedence); ok {
			if definition, loaded := makeClause(resolver.ws, resolver.mods, kept); loaded {
				return definition
			}
		}
		exists = exists || len(sites) > 0
	}
	return clause{statement: absentStatement, dropped: exists}
}

// findWinner returns the definition the game keeps, as if it read the file being drawn, or false if it keeps none.
func (resolver *resolver) findWinner(sites []index.Site, precedence game.Precedence) (index.Winner, bool) {
	mods := resolver.mods
	if resolver.isRead {
		return mods.FindWinner(sites, precedence)
	}
	if site, ok := lo.Find(sites, func(site index.Site) bool { return site.File == resolver.file }); ok {
		return index.Winner{Site: site}, true
	}
	replacer, _, _ := mods.FindLoaded(mods.Files[resolver.file].Rel)
	return mods.FindWinner(lo.Reject(sites, func(site index.Site, _ int) bool {
		return mods.File(site).Key == replacer.Key
	}), precedence)
}

// isBuiltin reports whether a type is one the engine draws without a file defining it.
func (resolver *resolver) isBuiltin(owner clause, label string) bool {
	definition, _ := resolver.findCalled(owner, game.GUI.TagCalls, label)
	return resolver.ws.Schema.LookupDefinition(game.GUI.WidgetTypes, label) != nil && !definition.dropped
}

// makeMissing returns the property that marks a type or template, which no file defines, used by a clause.
func (resolver *resolver) makeMissing(key, value string, source clause) Property {
	return Property{
		Key: key, Value: value, Origin: originMissing, Source: describeSource(originMissing, ""),
		Place: resolver.locate(source.path), Range: toRange(source),
	}
}

// describeSource returns in words where a property comes from, such as "type button", "this widget" or "missing".
func describeSource(origin, writer string) string {
	switch {
	case origin == originMissing:
		return originMissing
	case origin == originType:
		return "type " + writer
	case origin == originTemplate:
		return writer
	case writer != "":
		return "in " + writer
	}
	return "this widget"
}

// listOverrides notes the labeled blocks the given clauses replace, a later clause over an earlier one.
func (resolver *resolver) listOverrides(links []clause) {
	for _, link := range links {
		if link.statement == absentStatement {
			continue
		}
		for lead, field := range resolver.listFields(link, 0) {
			if lead == game.GUI.BlockCalls {
				resolver.gathered.overrides[field.tree.Unquoted(field.tree.Statements[field.statement].Tag)] = field
			}
		}
	}
}

// listFields yields each field of a clause with its lead, in the order the game applies them. A using yields the
// fields of its template, and a labeled block the fields of the block that replaces it, or its own.
func (resolver *resolver) listFields(holder clause, depth int) iter.Seq2[string, clause] {
	return func(yield func(string, clause) bool) {
		if depth > maxDepth {
			resolver.gathered.missing = append(resolver.gathered.missing, resolver.makeMissing(game.GUI.TemplateCalls, holder.writer, holder))
			return
		}
		for statement := range holder.tree.Children(holder.statement) {
			current := holder.tree.Statements[statement]
			field := clause{tree: holder.tree, path: holder.path, statement: statement, writer: holder.writer, origin: holder.origin}
			lead := findLead(holder.tree, statement)
			switch {
			case current.IsListed(): // a bare value is only the lead of the field after it
			case holder.tree.Unquoted(current.Key) == game.GUI.TemplateCalls:
				if !current.IsBlock() && !resolver.listTemplate(field, depth, yield) {
					return
				}
			case lead == game.GUI.BlockKey:
				for inner, replacement := range resolver.listFields(resolver.findReplacement(field), depth+1) {
					if !yield(inner, replacement) {
						return
					}
				}
			case !yield(lead, field):
				return
			}
		}
	}
}

// listTemplate yields the fields of the template a using statement names. It returns false if the loop over them stops
// early. A template that no file defines is marked missing.
func (resolver *resolver) listTemplate(using clause, depth int, yield func(string, clause) bool) bool {
	label := using.tree.Unquoted(using.tree.Statements[using.statement].Value)
	template, ok := resolver.findCalled(using, game.GUI.TemplateCalls, label)
	if !ok {
		resolver.gathered.missing = append(resolver.gathered.missing, resolver.makeMissing(game.GUI.TemplateCalls, label, using))
		return true
	}
	for lead, field := range resolver.listFields(template, depth+1) {
		if !yield(lead, field) {
			return false
		}
	}
	return true
}

// findLead returns the lead of a statement in lower case: the key that opens
// a labeled block or blockoverride, else the value written alone just before
// it. It returns "" for any other statement.
func findLead(tree *jomini.Tree, statement int32) string {
	current := tree.Statements[statement]
	key := strings.ToLower(tree.Unquoted(current.Key))
	if tree.Unquoted(current.Tag) != "" && (key == game.GUI.BlockKey || key == game.GUI.BlockCalls) {
		return key
	}

	lead, ok := tree.FindLead(statement)
	if !ok {
		return ""
	}

	return strings.ToLower(tree.Text(tree.Statements[lead].Value))
}

// resolveBlock returns the properties a clause gives its widget, and gathers its children, states and overlays.
func (resolver *resolver) resolveBlock(holder clause, depth int) []Property {
	var properties []Property
	for lead, field := range resolver.listFields(holder, depth) {
		current := field.tree.Statements[field.statement]
		key := cmp.Or(field.tree.Unquoted(current.Key), field.tree.Unquoted(current.Tag))
		slot, slotted := game.GUI.Slots[strings.ToLower(key)]
		switch {
		case lead == game.GUI.BlockCalls: // a blockoverride gives no property; listOverrides took it
		case current.IsBlock() && (key == game.GUI.ItemKey || slot.Labels):
			resolver.gatherInner(field, slot, depth)
		case current.IsBlock() && slotted:
			if !slot.Floats {
				resolver.gathered.children = append(resolver.gathered.children, child{clause: field, slot: slot})
			}
		case current.IsBlock() && resolver.isWidget(field, key):
			resolver.gathered.children = append(resolver.gathered.children, child{clause: field})
		case current.IsBlock() && key == game.GUI.StateKey:
			resolver.gathered.states = append(resolver.gathered.states, field)
		default:
			if current.IsBlock() && key == game.GUI.OverlayKey {
				resolver.gathered.overlays = append(resolver.gathered.overlays, resolver.makeOverlay(field, depth))
			}
			properties = append(properties, resolver.makeProperty(field))
		}
	}
	return properties
}

// findReplacement returns the block the widget replaces a labeled block with, or the labeled block itself if the same
// type or template wrote both.
func (resolver *resolver) findReplacement(field clause) clause {
	override, ok := resolver.gathered.overrides[field.tree.Unquoted(field.tree.Statements[field.statement].Tag)]
	if ok && override.writer != field.writer {
		return override
	}
	return field
}

// gatherInner gathers the widgets inside a label or an item, templates included, as children that fill the given slot.
// A label replaces the one before it, and a widget inside an item is an item of a data model.
func (resolver *resolver) gatherInner(field clause, slot game.WidgetSlot, depth int) {
	if slot.Labels {
		resolver.gathered.children = slices.DeleteFunc(resolver.gathered.children, func(held child) bool {
			return held.slot.Labels
		})
	}
	for _, inner := range resolver.listFields(field, depth) {
		current := inner.tree.Statements[inner.statement]
		if current.IsBlock() && resolver.isWidget(inner, inner.tree.Unquoted(current.Key)) {
			resolver.gathered.children = append(resolver.gathered.children, child{clause: inner, slot: slot, item: !slot.Labels})
		}
	}
}

// isWidget reports whether a key written in a widget's block starts another widget.
func (resolver *resolver) isWidget(owner clause, key string) bool {
	_, defined := resolver.findCalled(owner, game.GUI.TagCalls, key)
	return defined || resolver.isBuiltin(owner, key)
}

// classifyState returns the label of a state, "" for one with none, whether a widget is in it as it opens, and whether
// it takes its widget off the screen. A state does that when it fades the widget out to an alpha of 0 and runs calls as
// it ends (game.GUI.Animation), as a popup does that removes itself once faded.
func (resolver *resolver) classifyState(state clause) (label string, opens, leaves bool) {
	rules := game.GUI.Animation
	fades, ends := false, false
	for field := range resolver.listStateFields(state) {
		key, value, _ := splitField(field)
		if key == game.GUI.WidgetKey && label == "" {
			label = value
		}
		runsByName := key == game.GUI.WidgetKey && slices.Contains(game.GUI.OpeningStates, value)
		runsByValue := value != "" && strings.EqualFold(game.GUI.OpeningTriggers[key], value)
		opens = opens || runsByName || runsByValue
		alpha, err := strconv.ParseFloat(value, 64)
		fades = fades || key == rules.AlphaKey && err == nil && alpha == 0
		ends = ends || key == rules.FinishKey && value != ""
	}
	return label, opens, fades && ends
}

// listStateFields yields each field of a state that writes a value.
func (resolver *resolver) listStateFields(state clause) iter.Seq[clause] {
	return func(yield func(clause) bool) {
		for lead, field := range resolver.listFields(state, 0) {
			if lead == "" && !field.tree.Statements[field.statement].IsBlock() && !yield(field) {
				return
			}
		}
	}
}

// makeOverlay returns the position among the window's overlays of the overlay a modify_texture block writes.
func (resolver *resolver) makeOverlay(field clause, depth int) int32 {
	var properties []Property
	for lead, inner := range resolver.listFields(field, depth+1) {
		if lead == "" {
			properties = append(properties, resolver.makeProperty(inner))
		}
	}
	properties = resolveProperties(properties)
	path := resolver.library.resolveTexture(findProperty(slices.Values(properties), game.GUI.OverlayTextureKey).Value)
	positions := resolver.intern(properties)
	return resolver.overlays.add(fmt.Sprint(path, positions), func() Overlay { return Overlay{Path: path, Properties: positions} })
}

// makeProperty returns the property that a field writes. A block's value is
// its text, with its tag. A color written with a tag, as hsv { 0.5 1 1 }
// is, is given as the plain color it makes, { red green blue alpha } from 0
// to 1, which is what the page draws.
func (resolver *resolver) makeProperty(field clause) Property {
	current := field.tree.Statements[field.statement]
	value := field.tree.Unquoted(current.Value)
	if color, ok := jomini.ToColor(field.tree, field.statement); ok && !current.Tag.IsEmpty() {
		value = fmt.Sprintf("{ %.4g %.4g %.4g %.4g }", color.R, color.G, color.B, color.A)
	} else if current.IsBlock() {
		span := current.ResolveValueSpan()
		var edits []jomini.Edit
		for _, inner := range field.tree.Statements[field.statement+1 : current.Last] {
			if resolved, ok := resolver.resolveSpan(field.tree, inner.Value, inner.ValueClass); ok {
				shifted := jomini.Span{Start: inner.Value.Start - span.Start, End: inner.Value.End - span.Start}
				edits = append(edits, jomini.Edit{Span: shifted, Text: resolved})
			}
		}
		value = jomini.ApplyEdits(field.tree.Text(span), edits)
	} else if resolved, ok := resolver.resolveSpan(field.tree, current.Value, current.ValueClass); ok {
		value = resolved
	}
	return Property{
		Key: cmp.Or(field.tree.Unquoted(current.Key), field.tree.Unquoted(current.Tag)), Value: value, Origin: field.origin,
		Place: resolver.locate(field.path), Range: toRange(field), Writer: field.writer,
		Source: describeSource(field.origin, field.writer),
	}
}

// resolveSpan returns what the game reads for a value written in a span: the
// number of a constant or an expression, or the value of the define a
// GetDefine call names. It returns false for a plain number, which the given
// class says the value is, and for any other value.
func (resolver *resolver) resolveSpan(tree *jomini.Tree, span jomini.Span, class jomini.Class) (string, bool) {
	if number, ok := tree.ResolveNumber(span); ok && class != jomini.ClassNumber {
		return strconv.FormatFloat(number, 'f', -1, 64), true
	}
	call, ok := tree.ParseBracketed(span)
	if !ok || len(call.Steps) != 1 || call.Steps[0].Text != game.GUI.DefineCall || len(call.Steps[0].Arguments) == 0 {
		return "", false
	}
	// GetDefine('NAI', 'X') names the define X of the group NAI
	arguments := call.Steps[0].Arguments
	group := lo.Ternary(len(arguments) > 1, arguments[0].Text, "")
	value := resolver.library.lookupDefine(group, arguments[len(arguments)-1].Text)
	return value, value != ""
}

// resolveAlone returns the properties a template drawn on its own gives its widget.
func (resolver *resolver) resolveAlone(own clause) []Property {
	if own.statement != absentStatement || own.writer == "" {
		return nil
	}
	template, ok := resolver.findCalled(own, game.GUI.TemplateCalls, own.writer)
	if !ok {
		return []Property{resolver.makeMissing(game.GUI.TemplateCalls, own.writer, own)}
	}
	return resolver.resolveBlock(template, 1)
}

// resolveOpening returns the properties a widget has once its opening animation has run.
func (resolver *resolver) resolveOpening(name string) []Property {
	states, queue, leaving := map[string]clause{}, []clause{}, map[clause]bool{}
	for _, state := range resolver.gathered.states {
		label, opens, leaves := resolver.classifyState(state)
		states[label], leaving[state] = state, leaves
		if opens {
			queue = append(queue, state)
		}
	}
	for _, label := range resolver.gathered.triggers[name] {
		if state, ok := states[label]; ok {
			queue = append(queue, state)
		}
	}
	var properties []Property
	run := map[clause]bool{}
	for len(queue) > 0 {
		state := queue[0]
		queue = queue[1:]
		if run[state] || leaving[state] {
			continue
		}
		run[state] = true
		values, after := resolver.runState(state, states)
		properties = append(properties, values...)
		queue = append(queue, after...)
	}
	return properties
}

// runState returns the end values and opening calls a state gives its widget, and the states it leads to.
func (resolver *resolver) runState(state clause, states map[string]clause) (properties []Property, after []clause) {
	for field := range resolver.listStateFields(state) {
		key, value, _ := splitField(field)
		switch {
		case strings.EqualFold(key, game.GUI.OpeningCalls):
			properties = append(properties, resolver.makeProperty(field))
			after = append(after, resolver.triggerStates(value, states)...)
		case key == game.GUI.Animation.NextKey:
			if next, ok := states[value]; ok {
				after = append(after, next)
			}
		case !slices.ContainsFunc(game.GUI.StateKeys, func(stateKey string) bool { return strings.EqualFold(stateKey, key) }):
			properties = append(properties, resolver.makeProperty(field))
		}
	}
	return properties, after
}

// triggerStates returns the states a call in a state triggers by name. A call on a child found by FindChild is noted in
// the gathering for that child instead.
func (resolver *resolver) triggerStates(source string, states map[string]clause) []clause {
	rules := game.GUI.Animation
	call, ok := jomini.ParseBracketed(source, 0)
	if !ok {
		return nil
	}
	last := len(call.Steps) - 1
	labels := listQuoted(call.Steps[last])
	if call.Steps[last].Text != rules.Trigger || len(labels) != 1 {
		return nil
	}
	if last > 0 && call.Steps[last-1].Text == rules.FindChild {
		if children := listQuoted(call.Steps[last-1]); len(children) == 1 {
			resolver.gathered.triggers[children[0]] = slices.Concat(resolver.gathered.triggers[children[0]], labels)
			return nil
		}
	}
	state, ok := states[labels[0]]
	return lo.Ternary(ok, []clause{state}, nil)
}

// resolveWidget gives a widget its properties, the last written of each key, and what they lead to.
func (resolver *resolver) resolveWidget(id int32, builtin string, properties []Property) {
	properties = resolveProperties(properties)
	texts, textures := map[string][]loc.Segment{}, map[string]string{}
	for _, property := range properties {
		if property.Origin == originMissing {
			continue
		}
		text, texture := resolver.library.resolveValue(property.Key, property.Value)
		if len(text) > 0 {
			texts[property.Key] = text
		}
		if texture != "" {
			textures[property.Key] = texture
		}
	}
	widget := &resolver.widgets[id]
	widget.Name = findProperty(slices.Values(properties), game.GUI.WidgetKey).Value
	widget.Row, widget.Builtin = game.GUI.Types[builtin], builtin
	dropsHidden := findProperty(slices.Values(properties), game.GUI.DropsHiddenKey).Value
	widget.Row.DropsHidden = resolver.isDropping(widget.Row, builtin, dropsHidden)
	widget.Properties, widget.Texts, widget.Textures = resolver.intern(properties), texts, textures
	widget.Colors, widget.Overlays = map[string]string{}, slices.Concat([]int32{}, resolver.gathered.overlays)
}

// isDropping reports whether the hidden children of a widget take no room, as its type says or as the widget's own
// setting does.
func (resolver *resolver) isDropping(row game.WidgetType, builtin, setting string) bool {
	literal, isLiteral := game.GUI.Literals[strings.ToLower(setting)]
	if isLiteral && !literal {
		return false
	}
	return row.DropsHidden || slices.Contains(resolver.ws.Game.GUI.DropsHidden, builtin) ||
		(isLiteral && !row.KeepsHidden)
}

// resolveValue returns the text a key's value shows or the image file it names. Both are empty if it leads to neither.
func (library *library) resolveValue(key, value string) (text []loc.Segment, texture string) {
	switch {
	case value == "": // an empty value shows nothing
	case slices.Contains(game.GUI.PictureKeys, key) || slices.Contains(game.GUI.MaskKeys, key):
		texture = library.resolveTexture(value)
	case isLocalized(key):
		message, _, ok := library.ws.LookupText(library.mods, value)
		text = resolveSegments(library.ws, library.mods, loc.Segments(lo.Ternary(ok, message, value), 0), 0)
	case key == game.GUI.TextKeys[1]: // the key that writes a text as shown
		text = resolveSegments(library.ws, library.mods, loc.Segments(value, 0), 0)
	}
	return text, texture
}

// isLocalized reports whether the value of a key is a localization key.
func isLocalized(key string) bool { return localizedKeys()[key] }

// resolveChildren makes the widgets of the blocks a widget gathered. A label takes the texts of its holder, which then
// shows none.
func (resolver *resolver) resolveChildren(holder int32) {
	children := resolver.gathered.children
	slices.SortStableFunc(children, compareChildren)
	for _, held := range children {
		if slices.ContainsFunc(resolver.ancestors, func(ancestor clause) bool {
			return ancestor.path == held.path && ancestor.statement == held.statement
		}) {
			continue
		}
		id := int32(len(resolver.widgets))
		resolver.buildWidget(held, holder, "")
		if held.slot.Labels {
			for key, text := range resolver.widgets[holder].Texts {
				resolver.widgets[id].Texts[key] = text
				delete(resolver.widgets[holder].Texts, key)
			}
		}
	}
}

// compareChildren orders two children among their siblings: a label before the others, and an item of a data model
// after them.
func compareChildren(first, second child) int {
	rank := func(held child) int {
		switch {
		case held.slot.Labels:
			return 0
		case held.item:
			return 2
		}
		return 1
	}
	return cmp.Compare(rank(first), rank(second))
}

// findHolders returns the game window that a window of the given type is laid out in, as its own widget alone, or none
// if there is no such window.
func (resolver *resolver) findHolders(key string, root clause) []Widget {
	_, chain := resolver.listTypes(key, root)
	for _, link := range slices.Backward(chain) {
		name, ok := resolver.ws.Game.GUI.Holders[link.writer]
		reference := schema.Reference{Database: game.GUI.WidgetDatabase, Name: name}
		winner, defined := resolver.mods.FindKept(resolver.ws.Schema, reference)
		if !ok || !defined {
			continue
		}
		// The copy starts with no widgets and shares the tables and the library.
		other := *resolver
		other.ancestors, other.gathered, other.widgets = nil, gathering{}, nil
		if holder, err := other.resolveWindow(resolver.mods.Files[winner.Site.File].Path, name); err == nil {
			return []Widget{holder.Widgets[0]}
		}
	}
	return []Widget{}
}

// EditProperties returns the edits that make the given settings in a file. A key a block writes has its value
// replaced, and the rest are added at the block's end. It returns an error if the file is not in the workspace or no
// block starts where a setting's range does.
func (drw *Service) EditProperties(path string, settings []Setting) ([]lsp.Edit, error) {
	var tree *jomini.Tree
	var edits []jomini.Edit
	var holders []int32
	lines := map[int32][]string{}
	for _, setting := range settings {
		var holder int32
		var err error
		if tree, holder, err = drw.findBlock(path, setting.Block); err != nil {
			return nil, err
		}
		statement, ok := int32(0), false
		for child := range tree.Children(holder) {
			if strings.EqualFold(tree.Unquoted(tree.Statements[child].Key), setting.Key) {
				statement, ok = child, true
			}
		}
		if ok {
			edits = append(edits, jomini.Edit{Span: tree.Statements[statement].ResolveValueSpan(), Text: setting.Value})
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

// findKeptClause returns the definition the game keeps of a name in a database, or false if there is none.
func findKeptClause(ws *workspace.Session, mods *index.Index, reference schema.Reference) (clause, bool) {
	kept, ok := mods.FindKept(ws.Schema, resolveReference(mods, reference))
	if !ok {
		return clause{}, false
	}
	return makeClause(ws, mods, kept)
}

// resolveReference returns a reference with its database. One given with none gets the one database that defines its
// name, if there is one.
func resolveReference(mods *index.Index, reference schema.Reference) schema.Reference {
	if reference.Database != "" {
		return reference
	}
	if databases := mods.ListDatabases(reference.Name); len(databases) == 1 {
		reference.Database = databases[0]
	}
	return reference
}

// makeClause returns the clause of a definition the game keeps, or false if its file cannot be read.
func makeClause(ws *workspace.Session, mods *index.Index, kept index.Winner) (clause, bool) {
	tree, statement, err := ws.FindStatement(mods, kept.Site)
	if err != nil {
		return clause{}, false
	}
	return clause{tree: tree, path: mods.File(kept.Site).Path, statement: statement}, true
}

// findProperty returns the property with the given key among the given ones, letter case aside, or the zero Property
// if there is none.
func findProperty(properties iter.Seq[Property], key string) Property {
	for property := range properties {
		if strings.EqualFold(property.Key, key) {
			return property
		}
	}
	return Property{}
}

// resolveProperties returns the last written property of each key, in the order the keys are first written.
func resolveProperties(properties []Property) []Property {
	resolved := []Property{}
	positions := map[string]int{}
	for _, property := range properties {
		key := strings.ToLower(property.Key)
		switch {
		case property.Origin == originMissing || slices.Contains(game.GUI.RepeatedKeys, key):
			key = property.Origin + key + property.Value
		case slices.Contains(game.GUI.TextKeys, key):
			key = game.GUI.TextKeys[0]
		}
		if position, ok := positions[key]; ok {
			resolved[position] = property
			continue
		}
		positions[key] = len(resolved)
		resolved = append(resolved, property)
	}
	return resolved
}

// toRange returns where a clause is written, as the editor counts. A clause with no statement has no place.
func toRange(field clause) jomini.Range {
	var span jomini.Span
	if field.statement != absentStatement {
		span = field.tree.Statements[field.statement].Span
	}
	return jomini.ToRange(field.tree.Src, field.tree.Lines, span)
}
