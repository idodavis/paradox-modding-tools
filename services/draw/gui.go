// Package draw builds what the Visual view shows. It resolves a GUI window into its widgets and a coat of arms
// into its layers, and it produces the text edits the view's panels make.
package draw

import (
	"cmp"
	"fmt"
	"hash/fnv"
	"iter"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/samber/lo"

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
	mu        sync.Mutex // over the opening
	opening   opening
}

// A Property is one property of a widget, with the place where it is written. That place can be a template or the
// widget's type instead of the widget itself.
type Property struct {
	Key string `json:"key"`
	// Value is what the game reads. A constant or an expression is worked out, and a GetDefine call is replaced by the
	// value of its define. The same holds for each value inside a block. Any other value is as written.
	Value  string    `json:"value"`
	Range  lsp.Range `json:"range"`
	Place  int32     `json:"place"`  // the position of the file it is written in among its window's places
	Origin string    `json:"origin"` // "widget", "template" or "type"; "missing" if no file defines the source
	Writer string    `json:"writer"` // the type or template whose block it is written in; "" for no such block
}

// A Widget is one element of a resolved window, with everything needed to draw it. The page lays it out.
type Widget struct {
	ID int32 `json:"id"`
	// Parent is the widget that holds it; the window's own widget is 0, and so is its parent.
	Parent int32           `json:"parent"`
	Name   string          `json:"name"`
	Type   string          `json:"type"`
	Row    game.WidgetType `json:"row"` // the game's row for its type
	// Builtin is the engine's type that its type comes down to through the chain of types it is made on, such as
	// widget, hbox or icon.
	Builtin string `json:"builtin"`
	// TypeDefined is true when a file defines its type, which can then be opened; a built-in type is not defined.
	TypeDefined bool   `json:"typeDefined"`
	Slot        string `json:"slot"` // the key of the slot it fills, such as scrollbar_vertical; "" for none
	Role        string `json:"role"` // the slot it fills in its holder, or "" for a plain child
	Item        bool   `json:"item"` // it is an item of a data model
	// Properties is the positions of its properties among its window's properties: the last written of each key, in
	// the order the keys are first written.
	Properties []int32 `json:"properties"`
	// Texts is, by property key, the plain text and the data calls of each text it shows. A data call's text is
	// sample text of the kind the call returns, as sampleCall makes it.
	Texts    map[string][]loc.Segment `json:"texts"`
	Textures map[string]string        `json:"textures"` // by property key; the image file for the property's path
	// Colors is, by property key, the CSS color that a binding of a definition's color leads to, as findColor finds it.
	Colors map[string]string `json:"colors"`
	// Placeholder is the shape among the page's placeholders that it draws where its picture has no file, by what the
	// picture is of, as game.FindPlaceholder says. It is "" for a widget with a picture, a background or no picture.
	Placeholder string `json:"placeholder"`
	// Pictures is where the pictures are listed that its picture binding may draw, where no file is found for the
	// binding as written. It draws the first of them; nil means no list.
	Pictures *PictureList `json:"pictures,omitempty"`
	Overlays []int32      `json:"overlays"` // the positions of the textures laid over its own among its window's overlays
	Range    lsp.Range    `json:"range"`
	Place    int32        `json:"place"` // the position of the file it is written in among its window's places
}

// An Overlay is one texture laid over a widget's own, as a modify_texture block writes it.
type Overlay struct {
	Path       string  `json:"path"`       // the image file for its texture; "" if there is none
	Properties []int32 `json:"properties"` // the positions of its properties among its window's properties
}

// A Window is one GUI window resolved for drawing. Its widgets are in document order, and its holders are the game
// windows that contain it, outermost first, each its own widget alone.
type Window struct {
	Name        string   `json:"name"`
	Widgets     []Widget `json:"widgets"`
	Holders     []Widget `json:"holders"`
	Datacontext string   `json:"datacontext"` // the data context its bindings read
	// Places is the places of the files that its widgets and holders are written in, each once.
	Places []workspace.Place `json:"places"`
	Fonts  []Font            `json:"fonts"` // every style of the fonts its widgets name
	// Rows is, by the item of a data model, the rows its list draws, one for each of the first definitions of the
	// database its widgets read. An item whose widgets read none is left out.
	Rows map[int32][]Row `json:"rows"`
	// Conditions is, by widget, the conditions it is shown on, as written. A widget shown on none is left out.
	Conditions map[int32][]string `json:"conditions"`
	// Groups is what the panel lets the user switch: the groups its widgets' conditions read, its lists of pictures,
	// then its tiers. The value each is at is the states' (States.Values).
	Groups []Group `json:"groups"`
	// TextFont and TextSize are the font and size of a text that sets neither, or "" if the game sets none.
	TextFont string `json:"textFont"`
	TextSize string `json:"textSize"`
	// Properties is the properties of its widgets, its holders and their overlays, each once. A widget and an overlay
	// hold the positions of their own.
	Properties []Property `json:"properties"`
	// Overlays is the textures laid over its widgets' own, each once. A widget holds the positions of its own.
	Overlays []Overlay `json:"overlays"`
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
	role string
	item bool // it is an item of a data model
}

// A gathering is what one widget collects from its own block and from the blocks of its types and templates.
type gathering struct {
	overrides map[string]clause // the blocks it replaces, by the block they replace
	children  []child
	missing   []Property // the types and templates it uses that no file defines
	states    []clause   // the states it has
	overlays  []int32    // the positions of the textures it overlays among its window's overlays
	// triggers is the states that its opening and its holders' run on the widgets under it, by their names, which
	// those widgets open with
	triggers map[string][]string
}

// A resolver makes the widgets of one window, one widget at a time, and holds them until the window is done.
type resolver struct {
	ws          *workspace.Session
	mods        *index.Index
	file        int32             // the file being drawn, by its position in the index
	isRead      bool              // the game reads the file being drawn, rather than another file at its path
	places      *placeList        // shared with the resolvers of the window's holders
	properties  *propertyList     // shared with the resolvers of the window's holders
	definitions map[string]clause // the definition of a type or template, by its caller, its label and its file
	ancestors   []clause
	library     *library // what the definitions show, shared with the resolvers of the window's holders
	gathered    gathering
	widgets     []Widget
}

// A placeList is the places of the files that one window is written in, each located once, in the order first met.
type placeList struct {
	places    []workspace.Place
	positions map[string]int32 // by path
}

// A propertyList is the properties and the overlays of one window's widgets, each kept once, in the order first met.
type propertyList struct {
	properties []Property
	overlays   []Overlay
	positions  map[Property]int32
	// overlayPositions is the positions of the overlays, by the file and the property positions of each
	overlayPositions map[string]int32
}

// The words of a GUI file, the slot roles, the property origins, the data call kinds and the deepest nesting.
const (
	typeKey        = "type"
	stateKey       = "state"
	itemKey        = "item"
	overlayKey     = "modify_texture"
	rawTextKey     = "raw_text"
	sizeKey        = "size"
	blockLead      = "block"
	roleLabel      = "label"
	roleAttachment = "attachment"
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

// loremSentences is the sample text of a data call that shows text, one sentence each.
var loremSentences = []string{
	"Lorem ipsum dolor sit amet, consectetur adipiscing elit.",
	"Sed do eiusmod tempor incididunt ut labore et dolore magna aliqua.",
	"Ut enim ad minim veniam, quis nostrud exercitation ullamco laboris.",
	"Duis aute irure dolor in reprehenderit in voluptate velit esse.",
	"Excepteur sint occaecat cupidatat non proident, sunt in culpa qui officia.",
}

// ListDrawables lists the windows, types, templates and coats a file defines, the one the view opens on first. It
// lists none for a file that is not a GUI or coat file.
func (drw *Service) ListDrawables(path string) ([]lsp.Symbol, error) {
	ws, err := drw.Workspace.Session()
	if err != nil {
		return nil, err
	}
	symbols, err := lsp.ListFileSymbols(ws, resolveIndex(ws, path), path)
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

// drawsSomething reports whether a resolved window draws anything: its own widget writes a size, shows a picture or a
// text, or holds widgets of its own.
func drawsSomething(window *Window) bool {
	root := window.Widgets[0]
	return len(window.Widgets) > 1 || isDrawing(window, root) || findProperty(window.listProperties(root), sizeKey).Value != ""
}

// listDrawn returns the symbols the view can draw among the given ones and the symbols nested in them, each without
// its nested symbols, in document order.
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

// isDrawn reports whether a database holds what the view draws as a window: a window, or a type or a template, which
// are the definitions that a widget or a using calls by name.
func isDrawn(schema *schema.Schema, database string) bool {
	callers := listCallers(schema, database)
	return database == game.GUI.WidgetDatabase || slices.Contains(callers, game.GUI.TagCalls) || slices.Contains(callers, game.GUI.TemplateCalls)
}

// listCallers returns the words that call the definitions of a database by name, or none if the schema has no such
// database.
func listCallers(schema *schema.Schema, database string) []string {
	row, ok := schema.Databases[database]
	if !ok {
		return nil
	}
	return row.Callable.Callers
}

// ResolveWindow resolves one window in a GUI file. It applies the window's type and templates, looks up its texts and
// finds its textures. It returns an error if the file is not in the workspace or does not define the window.
func (drw *Service) ResolveWindow(path, name string) (Window, error) {
	ws, err := drw.Workspace.Session()
	if err != nil {
		return Window{}, err
	}
	opened, err := drw.resolveOpening(ws, path, name)
	if err != nil {
		return Window{}, err
	}
	window := opened.evaluator.window
	window.Rows = listRows(&library{ws: ws, mods: opened.mods}, window, opened.evaluator.rows)
	return window, nil
}

// lookupRows returns how many rows the Visual view draws of a data model's list, as the workspace's rows setting says.
func (drw *Service) lookupRows(ws *workspace.Session) int {
	setting, _ := drw.Workspace.Config.LookupSetting(ws.ID, rowsSetting)
	count, _ := strconv.Atoi(setting)
	return count
}

// resolveIndex returns the index a file is drawn from: the game's own for a game file, else the mods'.
func resolveIndex(ws *workspace.Session, path string) *index.Index {
	mods := ws.Mods.Load()
	if root, ok := mods.FindRoot(path); ok && root.Origin == game.OriginVanilla {
		return ws.Install
	}
	return mods
}

// findWindow returns the open workspace and a window of a GUI file, the kept one if nothing changed. It returns an
// error if no workspace is open, or the file is not in it or does not define the window.
func (drw *Service) findWindow(path, name string) (*workspace.Session, opening, error) {
	ws, err := drw.Workspace.Session()
	if err != nil {
		return nil, opening{}, err
	}
	drw.mu.Lock()
	kept := drw.opening
	drw.mu.Unlock()
	if kept.mods == resolveIndex(ws, path) && kept.path == path && kept.name == name &&
		kept.evaluator.rows == drw.lookupRows(ws) {
		return ws, kept, nil
	}
	opened, err := drw.resolveOpening(ws, path, name)
	return ws, opened, err
}

// resolveOpening resolves a window of a GUI file without its rows and keeps it. It returns an error if the file is not
// in the workspace or does not define the window.
func (drw *Service) resolveOpening(ws *workspace.Session, path, name string) (opening, error) {
	mods := resolveIndex(ws, path)
	resolver := makeResolver(ws, mods, path)
	window, err := resolver.resolveWindow(path, name)
	if err != nil {
		return opening{}, err
	}
	window.TextFont, window.TextSize = resolver.findTextFont()
	window.Places, window.Fonts = resolver.places.places, findFonts(ws, mods, &window)
	window.Conditions = map[int32][]string{}
	for _, widget := range window.Widgets[1:] {
		if written := listConditions(&window, widget); len(written) > 0 {
			window.Conditions[widget.ID] = written
		}
	}
	evaluator := openEvaluator(ws.Schema, window, drw.lookupRows(ws))
	opened := opening{mods: mods, path: path, name: name, evaluator: evaluator, pictures: listPictureGroups(window),
		tiers: evaluator.listTierGroups(ws.Game.GUITiers)}
	evaluator.window.Groups = slices.Concat(evaluator.groups, opened.pictures, opened.tiers)
	drw.mu.Lock()
	drw.opening = opened
	drw.mu.Unlock()
	return opened, nil
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

// makeResolver returns a resolver of the windows of one file, drawn from the given index, with lists of places,
// properties and definitions of its own.
func makeResolver(ws *workspace.Session, mods *index.Index, path string) *resolver {
	file, _ := mods.FindFile(path)
	return &resolver{ws: ws, mods: mods, file: file, isRead: mods.IsLoaded(file),
		places:      &placeList{positions: map[string]int32{}},
		properties:  &propertyList{positions: map[Property]int32{}, overlayPositions: map[string]int32{}},
		definitions: map[string]clause{}, library: &library{ws: ws, mods: mods}}
}

// resolveWindow resolves a window that a file defines, as ResolveWindow does, but leaves its places to the caller.
func (resolver *resolver) resolveWindow(path, name string) (Window, error) {
	ws, mods := resolver.ws, resolver.mods
	file, ok := mods.FindFile(path)
	if !ok {
		return Window{}, fmt.Errorf("the file %q is not in the workspace", path)
	}
	site, ok := findDrawn(mods, file, name, func(database string) bool { return database == game.GUI.WidgetDatabase })
	if !ok {
		site, ok = findDrawn(mods, file, name, func(database string) bool { return isDrawn(ws.Schema, database) })
	}
	var definition clause
	if ok {
		definition, ok = makeClause(ws, mods, index.Winner{Site: site})
	}
	if !ok {
		return Window{}, fmt.Errorf("the file %q does not define the window %q", path, name)
	}
	root, key := resolver.findRoot(definition, mods.ToReference(site).Database, name)
	resolver.buildWidget(child{clause: root}, 0, key)
	widgets := resolver.widgets
	widgets[0].Name = name
	if root.statement == absentStatement {
		widgets[0].Range, widgets[0].Place = toRange(definition), resolver.places.locate(ws, mods, path)
	}
	// the holders are resolved first, since their properties join the window's
	window := Window{Name: name, Widgets: widgets, Holders: resolver.findHolders(key, root)}
	window.Properties, window.Overlays = resolver.properties.properties, resolver.properties.overlays
	window.Datacontext = findProperty(window.listProperties(widgets[0]), game.GUI.ContextKey).Value
	for id, context := range listContexts(ws.Schema, window) {
		resolver.resolveBindings(&window, &window.Widgets[id], context)
	}
	return window, nil
}

// listProperties returns the properties of one of the window's widgets or holders, in the order the widget holds
// them.
func (window *Window) listProperties(widget Widget) iter.Seq[Property] {
	return func(yield func(Property) bool) {
		for _, position := range widget.Properties {
			if !yield(window.Properties[position]) {
				return
			}
		}
	}
}

// intern returns the positions in the list of the given properties, each one added the first time it is met.
func (list *propertyList) intern(properties []Property) []int32 {
	return lo.Map(properties, func(property Property, _ int) int32 {
		position, ok := list.positions[property]
		if !ok {
			position = int32(len(list.properties))
			list.properties = append(list.properties, property)
			list.positions[property] = position
		}
		return position
	})
}

// internOverlay returns the position in the list of an overlay of the given image file and properties, added the
// first time it is met.
func (list *propertyList) internOverlay(path string, properties []Property) int32 {
	overlay := Overlay{Path: path, Properties: list.intern(properties)}
	key := fmt.Sprint(path, overlay.Properties)
	position, ok := list.overlayPositions[key]
	if !ok {
		position = int32(len(list.overlays))
		list.overlays = append(list.overlays, overlay)
		list.overlayPositions[key] = position
	}
	return position
}

// findDrawn returns the first definition of the given label in a file, among the databases the given function
// accepts. The label matches without regard to case.
func findDrawn(mods *index.Index, file int32, label string, accepts func(database string) bool) (index.Site, bool) {
	for position, occurrence := range mods.Occurrences[file] {
		site := index.Site{File: file, Occurrence: int32(position)}
		reference := mods.ToReference(site)
		if occurrence.Role == index.RoleDefines && accepts(reference.Database) && strings.EqualFold(reference.Name, label) {
			return site, true
		}
	}
	return index.Site{}, false
}

// findRoot returns the clause a drawn definition is made of, and the type its widget has. A window is drawn as
// written. A type is drawn as a widget of that type, and a template as a plain widget that uses it.
func (resolver *resolver) findRoot(definition clause, database, label string) (root clause, key string) {
	callers := listCallers(resolver.ws.Schema, database)
	key = definition.tree.Unquoted(definition.tree.Statements[definition.statement].Key)
	root = clause{tree: definition.tree, path: definition.path, statement: absentStatement}
	switch {
	case slices.Contains(callers, game.GUI.TagCalls):
		return root, key
	case slices.Contains(callers, game.GUI.TemplateCalls):
		root.writer = label
		return root, game.GUI.TagCalls
	}
	return definition, key
}

// locate returns the position in the list of the place of a file, located in the given index of the mods the first
// time the file is met.
func (list *placeList) locate(ws *workspace.Session, mods *index.Index, path string) int32 {
	position, ok := list.positions[path]
	if !ok {
		position = int32(len(list.places))
		list.places = append(list.places, ws.Locate(mods, path))
		list.positions[path] = position
	}
	return position
}

// buildWidget makes the widget that a clause writes, then the widgets inside it. An empty type takes the clause's key.
func (resolver *resolver) buildWidget(own child, parent int32, key string) {
	own.origin = originWidget
	if own.statement != absentStatement {
		key = cmp.Or(key, own.tree.Unquoted(own.tree.Statements[own.statement].Key))
	}
	slotKey := strings.ToLower(key)
	slot, slotted := game.GUI.Slots[slotKey]
	applied := cmp.Or(slot.Type, key)
	id := int32(len(resolver.widgets))
	resolver.widgets = append(resolver.widgets, Widget{
		ID: id, Parent: parent, Type: applied, Slot: lo.Ternary(slotted, slotKey, ""), Role: own.role,
		Item: own.item, Place: resolver.places.locate(resolver.ws, resolver.mods, own.path), Range: toRange(own.clause),
	})
	outer, outerAncestors := resolver.gathered, resolver.ancestors
	resolver.gathered = gathering{overrides: map[string]clause{}, triggers: maps.Clone(outer.triggers)}
	maps.Copy(resolver.gathered.overrides, outer.overrides)
	if own.statement != absentStatement {
		resolver.ancestors = append(resolver.ancestors, own.clause)
	}
	defer func() { resolver.gathered, resolver.ancestors = outer, outerAncestors }()
	builtin, chain := resolver.listTypes(applied, own.clause)
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
				resolver.gathered.missing = append(resolver.gathered.missing, resolver.makeMissing(typeKey, builtin, owner))
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
	resolver.gathered.missing = append(resolver.gathered.missing, resolver.makeMissing(typeKey, label, owner))
	return label, nil
}

// findCalled returns the definition the game keeps of a type or a template that the given caller names: the widget's
// own word for a type, using for a template. A definition local to the file of the given clause is tried first.
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
// if the game keeps none.
func (resolver *resolver) findDeclared(owner clause, caller, label string) clause {
	exists := false
	for _, declared := range slices.Backward(game.GUI.Declares) {
		database := game.GUI.DatabasePrefix + declared
		if !slices.Contains(listCallers(resolver.ws.Schema, database), caller) {
			continue
		}
		sites := resolver.mods.ResolveLocal(schema.Reference{Database: database, Name: label}, owner.path)
		if kept, ok := resolver.findWinner(sites, resolver.ws.Schema.Databases[database].Precedence); ok {
			if definition, loaded := makeClause(resolver.ws, resolver.mods, kept); loaded {
				return definition
			}
		}
		exists = exists || len(sites) > 0
	}
	return clause{statement: absentStatement, dropped: exists}
}

// findWinner returns the definition the game keeps, as if it read the file being drawn. It returns false if it keeps
// none.
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

// isBuiltin reports whether a type is one the engine draws without a file defining it. A type that files define but the
// game keeps none of is not one.
func (resolver *resolver) isBuiltin(owner clause, label string) bool {
	definition, _ := resolver.findCalled(owner, game.GUI.TagCalls, label)
	return resolver.ws.Schema.WidgetTypes[label] && !definition.dropped
}

// makeMissing returns the property that marks a type or template the game does not define, used by a clause.
func (resolver *resolver) makeMissing(key, value string, source clause) Property {
	return Property{Key: key, Value: value, Origin: originMissing, Place: resolver.places.locate(resolver.ws, resolver.mods, source.path), Range: toRange(source)}
}

// listOverrides notes the blocks that the given clauses replace, a later clause over an earlier one.
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

// listFields yields each field of a clause and the bare value before it, in the order the game applies them. A using
// yields its template's fields.
func (resolver *resolver) listFields(holder clause, depth int) iter.Seq2[string, clause] {
	return func(yield func(string, clause) bool) {
		if depth > maxDepth {
			resolver.gathered.missing = append(resolver.gathered.missing, resolver.makeMissing(game.GUI.TemplateCalls, holder.writer, holder))
			return
		}
		siblings := slices.Collect(holder.tree.Children(holder.statement))
		for position, statement := range siblings {
			current := holder.tree.Statements[statement]
			field := clause{tree: holder.tree, path: holder.path, statement: statement, writer: holder.writer, origin: holder.origin}
			switch {
			case current.IsListed():
			case holder.tree.Unquoted(current.Key) != game.GUI.TemplateCalls:
				if !yield(findLead(holder.tree, siblings, position), field) {
					return
				}
			case !current.IsBlock() && !resolver.listTemplate(field, depth, yield):
				return
			}
		}
	}
}

// listTemplate yields the fields of the template that a using statement names, in place. It returns false if the loop
// over them stops early. A template that no file defines is marked missing.
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

// findLead returns the lower-case word that opens a labeled block, block or blockoverride. It returns "" for any other
// statement.
func findLead(tree *jomini.Tree, siblings []int32, position int) string {
	current := tree.Statements[siblings[position]]
	if key := strings.ToLower(tree.Unquoted(current.Key)); tree.Unquoted(current.Tag) != "" &&
		(key == blockLead || key == game.GUI.BlockCalls) {
		return key
	}
	if position == 0 {
		return ""
	}
	previous := tree.Statements[siblings[position-1]]
	if previous.IsListed() {
		return strings.ToLower(tree.Text(previous.Value))
	}
	return ""
}

// resolveBlock returns the properties a clause gives its widget, and gathers its children, states and overlays.
func (resolver *resolver) resolveBlock(holder clause, depth int) []Property {
	var properties []Property
	for lead, field := range resolver.listFields(holder, depth) {
		current := field.tree.Statements[field.statement]
		key := cmp.Or(field.tree.Unquoted(current.Key), field.tree.Unquoted(current.Tag))
		slot, slotted := game.GUI.Slots[strings.ToLower(key)]
		switch {
		case lead == blockLead:
			properties = append(properties, resolver.resolveBlock(resolver.findReplacement(field), depth+1)...)
		case lead == game.GUI.BlockCalls:
		case current.IsBlock() && (key == itemKey || slot.Role == roleLabel):
			resolver.gatherInner(field, slot, depth)
		case current.IsBlock() && slotted:
			if slot.Role != roleAttachment {
				resolver.gathered.children = append(resolver.gathered.children, child{clause: field, role: slot.Role})
			}
		case current.IsBlock() && resolver.isWidget(field, key):
			resolver.gathered.children = append(resolver.gathered.children, child{clause: field})
		case current.IsBlock() && key == stateKey:
			resolver.gathered.states = append(resolver.gathered.states, field)
		default:
			if current.IsBlock() && key == overlayKey {
				resolver.gathered.overlays = append(resolver.gathered.overlays, resolver.makeOverlay(field, depth))
			}
			properties = append(properties, resolver.makeProperty(field))
		}
	}
	return properties
}

// findReplacement returns the block that the widget replaces a labeled block with, unless the same type or template
// wrote both. It returns the labeled block as written otherwise.
func (resolver *resolver) findReplacement(field clause) clause {
	override, ok := resolver.gathered.overrides[field.tree.Unquoted(field.tree.Statements[field.statement].Tag)]
	if ok && override.writer != field.writer {
		return override
	}
	return field
}

// gatherInner gathers the widgets inside a label or an item as children. A label replaces the one before it.
func (resolver *resolver) gatherInner(field clause, slot game.WidgetSlot, depth int) {
	if slot.Role == roleLabel {
		resolver.gathered.children = slices.DeleteFunc(resolver.gathered.children, func(held child) bool {
			return held.role == roleLabel
		})
	}
	resolver.gatherWidgets(field, slot.Role, depth)
}

// gatherWidgets gathers the widgets in a block and its templates as children in a role. A child with no role is an
// item of a data model.
func (resolver *resolver) gatherWidgets(holder clause, role string, depth int) {
	for lead, inner := range resolver.listFields(holder, depth) {
		current := inner.tree.Statements[inner.statement]
		switch {
		case lead == blockLead:
			resolver.gatherWidgets(resolver.findReplacement(inner), role, depth+1)
		case current.IsBlock() && resolver.isWidget(inner, inner.tree.Unquoted(current.Key)):
			resolver.gathered.children = append(resolver.gathered.children, child{clause: inner, role: role, item: role == ""})
		}
	}
}

// isWidget reports whether a key written in a widget's block starts another widget: a built-in type, or a type a file
// defines.
func (resolver *resolver) isWidget(owner clause, key string) bool {
	_, defined := resolver.findCalled(owner, game.GUI.TagCalls, key)
	return defined || resolver.isBuiltin(owner, key)
}

// isOpening reports whether a state is one that a widget is in as it opens: one the game runs by name, or by the value
// of a key.
func (resolver *resolver) isOpening(state clause) bool {
	for field := range resolver.listStateFields(state) {
		key, value, _ := splitField(field)
		if key == game.GUI.WidgetKey && slices.Contains(game.GUI.OpeningStates, value) ||
			value != "" && strings.EqualFold(game.GUI.OpeningTriggers[key], value) {
			return true
		}
	}
	return false
}

// isLeaving reports whether a state takes its widget off the screen: it fades the widget out, to an alpha of 0, and
// runs calls as it ends (game.GUI.Animation), as a popup does that removes itself once faded.
func (resolver *resolver) isLeaving(state clause) bool {
	rules := game.GUI.Animation
	fades, ends := false, false
	for field := range resolver.listStateFields(state) {
		key, value, _ := splitField(field)
		alpha, err := strconv.ParseFloat(value, 64)
		fades = fades || key == rules.AlphaKey && err == nil && alpha == 0
		ends = ends || key == rules.FinishKey && value != ""
	}
	return fades && ends
}

// findStateName returns the name of a state, or "" for a state that has none.
func (resolver *resolver) findStateName(state clause) string {
	for field := range resolver.listStateFields(state) {
		if key, value, _ := splitField(field); key == game.GUI.WidgetKey {
			return value
		}
	}
	return ""
}

// listStateFields yields each field of a state that writes a value, in the order the game applies them.
func (resolver *resolver) listStateFields(state clause) iter.Seq[clause] {
	return func(yield func(clause) bool) {
		for lead, field := range resolver.listFields(state, 0) {
			switch {
			case lead == blockLead:
				for inner := range resolver.listStateFields(resolver.findReplacement(field)) {
					if !yield(inner) {
						return
					}
				}
			case lead == "" && !field.tree.Statements[field.statement].IsBlock():
				if !yield(field) {
					return
				}
			}
		}
	}
}

// makeOverlay returns the position among the window's overlays of the overlay that a modify_texture block writes: its
// properties, and the image file its texture leads to.
func (resolver *resolver) makeOverlay(field clause, depth int) int32 {
	var properties []Property
	for lead, inner := range resolver.listFields(field, depth+1) {
		if lead == "" {
			properties = append(properties, resolver.makeProperty(inner))
		}
	}
	properties = resolveProperties(properties)
	path := resolver.library.resolveTexture(findProperty(slices.Values(properties), textureKey).Value)
	return resolver.properties.internOverlay(path, properties)
}

// makeProperty returns the property that a field writes. A block's value is its text, with its tag.
func (resolver *resolver) makeProperty(field clause) Property {
	current := field.tree.Statements[field.statement]
	value := field.tree.Unquoted(current.Value)
	if current.IsBlock() {
		value = resolver.resolveContents(field.tree, field.statement)
	} else if resolved, ok := resolver.resolveSpan(field.tree, current.Value); ok {
		value = resolved
	}
	return Property{
		Key: cmp.Or(field.tree.Unquoted(current.Key), field.tree.Unquoted(current.Tag)), Value: value, Origin: field.origin,
		Place: resolver.places.locate(resolver.ws, resolver.mods, field.path), Range: toRange(field), Writer: field.writer,
	}
}

// resolveContents returns the text a block statement writes after its operator, with each value that resolveSpan
// answers replaced by its answer.
func (resolver *resolver) resolveContents(tree *jomini.Tree, statement int32) string {
	current := tree.Statements[statement]
	span := current.ResolveValueSpan()
	var edits []jomini.Edit
	for _, inner := range tree.Statements[statement+1 : current.Last] {
		if resolved, ok := resolver.resolveSpan(tree, inner.Value); ok {
			shifted := jomini.Span{Start: inner.Value.Start - span.Start, End: inner.Value.End - span.Start}
			edits = append(edits, jomini.Edit{Span: shifted, Text: resolved})
		}
	}
	return jomini.ApplyEdits(tree.Text(span), edits)
}

// resolveSpan returns what the game reads for a value written in a span: the number of a constant or an expression, or
// the value of the define that a GetDefine call names. It returns false for any other value, which is read as written.
func (resolver *resolver) resolveSpan(tree *jomini.Tree, span jomini.Span) (string, bool) {
	if number, ok := tree.ResolveNumber(span); ok && !jomini.IsNumber(tree.Text(span)) {
		return strconv.FormatFloat(number, 'f', -1, 64), true
	}
	call, ok := jomini.ParseBracketed(tree.Unquoted(span), 0)
	if !ok || len(call.Steps) != 1 || call.Steps[0].Text != defineCall || len(call.Steps[0].Arguments) == 0 {
		return "", false
	}
	// GetDefine('NAI', 'X') names the define X of the group NAI
	arguments := call.Steps[0].Arguments
	group := lo.Ternary(len(arguments) > 1, arguments[0].Text, "")
	value := resolver.library.lookupDefine(group, arguments[len(arguments)-1].Text)
	return value, value != ""
}

// resolveAlone returns the properties that a template drawn on its own gives its widget. A clause that is not the
// root of a template gives none.
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

// resolveOpening returns the properties a widget has once its opening animation has run. A state that takes it off
// the screen is left out, with what follows it.
func (resolver *resolver) resolveOpening(name string) []Property {
	states, queue := map[string]clause{}, []clause{}
	for _, state := range resolver.gathered.states {
		states[resolver.findStateName(state)] = state
		if resolver.isOpening(state) {
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
		if run[state] || resolver.isLeaving(state) {
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

// triggerStates returns the states that a call in a state triggers by name. It returns none for any other call.
func (resolver *resolver) triggerStates(source string, states map[string]clause) []clause {
	rules := game.GUI.Animation
	call, ok := jomini.ParseBracketed(source, 0)
	if !ok || call.Steps[len(call.Steps)-1].Text != rules.Trigger {
		return nil
	}
	last := len(call.Steps) - 1
	labels := listQuoted(call.Steps[last])
	if len(labels) != 1 {
		return nil
	}
	if children := listQuoted(call.Steps[max(last-1, 0)]); last > 0 && call.Steps[last-1].Text == rules.FindChild &&
		len(children) == 1 {
		if resolver.gathered.triggers == nil {
			resolver.gathered.triggers = map[string][]string{}
		}
		resolver.gathered.triggers[children[0]] = slices.Concat(resolver.gathered.triggers[children[0]], labels)
		return nil
	}
	state, ok := states[labels[0]]
	return lo.Ternary(ok, []clause{state}, nil)
}

// resolveWidget gives a widget its properties, the last written of each key, and what they lead to: its name, its
// built-in type and that type's row, its texts, its textures and its overlays.
func (resolver *resolver) resolveWidget(id int32, builtin string, properties []Property) {
	properties = resolveProperties(properties)
	texts, textures := resolver.resolvePictures(properties)
	widget := &resolver.widgets[id]
	widget.Name = findProperty(slices.Values(properties), game.GUI.WidgetKey).Value
	widget.Row, widget.Builtin = game.GUI.Types[builtin], builtin
	dropsHidden := findProperty(slices.Values(properties), game.GUI.DropsHiddenKey).Value
	widget.Row.DropsHidden = resolver.isDropping(widget.Row, builtin, dropsHidden)
	widget.Properties, widget.Texts, widget.Textures = resolver.properties.intern(properties), texts, textures
	widget.Colors, widget.Overlays = map[string]string{}, slices.Concat([]int32{}, resolver.gathered.overlays)
}

// isDropping reports whether the hidden children of a widget take no room. The type's row or the game's list of types
// can say so, and so can the widget by writing the game's key as yes. Writing the key as no turns it off.
func (resolver *resolver) isDropping(row game.WidgetType, builtin, ignore string) bool {
	literal, isLiteral := game.GUI.Literals[strings.ToLower(ignore)]
	if isLiteral && !literal {
		return false
	}
	return row.DropsHidden || slices.Contains(resolver.ws.Game.GUIDropsHidden, builtin) ||
		isLiteral && !row.KeepsHidden
}

// resolvePictures returns the texts and the textures that a widget's properties lead to, by property key. A property
// that leads nowhere has no entry. The raw text of a widget is its text as written.
func (resolver *resolver) resolvePictures(properties []Property) (map[string][]loc.Segment, map[string]string) {
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
	return texts, textures
}

// resolveValue returns the text a key's value shows or the image file it names. Both are empty if it leads to
// neither.
func (library *library) resolveValue(key, value string) (text []loc.Segment, texture string) {
	switch {
	case value == "":
	case slices.Contains(game.GUI.PictureKeys, key) || slices.Contains(game.GUI.MaskKeys, key):
		texture = library.resolveTexture(value)
	case isLocalized(key):
		text = resolveText(library.ws, library.mods, value)
	case key == rawTextKey:
		text = resolveSegments(library.ws, library.mods, loc.Segments(value, 0), 0)
	}
	return text, texture
}

// isLocalized reports whether the value of a key is a localization key.
func isLocalized(key string) bool {
	return slices.ContainsFunc(game.GUI.References, func(reference game.Reference) bool {
		return reference.KeyPath == key && reference.Target == game.Localization
	})
}

// resolveChildren makes the widgets of the blocks that a widget gathered, the label first and an item of a data model
// last. A block that a widget above it is written in is left out. A label shows the text of its holder.
func (resolver *resolver) resolveChildren(holder int32) {
	children := resolver.gathered.children
	slices.SortStableFunc(children, compareChildren)
	for _, held := range children {
		if !slices.ContainsFunc(resolver.ancestors, func(ancestor clause) bool {
			return ancestor.path == held.path && ancestor.statement == held.statement
		}) {
			resolver.buildWidget(held, holder, "")
		}
	}
	for position := int(holder) + 1; position < len(resolver.widgets); position++ {
		if label := resolver.widgets[position]; label.Parent == holder && label.Role == roleLabel {
			resolver.applyTexts(holder, label.ID)
		}
	}
}

// compareChildren orders two children among their siblings: a label before the others, and an item of a data model
// after them.
func compareChildren(first, second child) int {
	rank := func(held child) int {
		switch {
		case held.role == roleLabel:
			return 0
		case held.item:
			return 2
		}
		return 1
	}
	return cmp.Compare(rank(first), rank(second))
}

// applyTexts gives a label the texts of its holder, which then has none of them.
func (resolver *resolver) applyTexts(holder, label int32) {
	for key, text := range resolver.widgets[holder].Texts {
		resolver.widgets[label].Texts[key] = text
		delete(resolver.widgets[holder].Texts, key)
	}
}

// findHolders returns the game window that a window of the given type is laid out in, as its own widget: the one the
// game names for the last type of its chain that has one, if the install defines it. It returns none otherwise.
func (resolver *resolver) findHolders(key string, root clause) []Widget {
	_, chain := resolver.listTypes(key, root)
	for _, link := range slices.Backward(chain) {
		name, ok := resolver.ws.Game.GUIHolders[link.writer]
		reference := schema.Reference{Database: game.GUI.WidgetDatabase, Name: name}
		winner, defined := resolver.mods.FindKept(resolver.ws.Schema, reference)
		if !ok || !defined {
			continue
		}
		// the holder's own resolver shares the places, the definitions and the library
		other := *resolver
		other.ancestors, other.gathered, other.widgets = nil, gathering{}, nil
		if holder, err := other.resolveWindow(resolver.mods.Files[winner.Site.File].Path, name); err == nil {
			return []Widget{holder.Widgets[0]}
		}
	}
	return []Widget{}
}

// EditProperty returns the edits that set one property of a block. It returns an error if the file is not in the
// workspace or no block starts at the given range.
func (drw *Service) EditProperty(path string, block lsp.Range, key, value string) ([]lsp.Edit, error) {
	tree, statement, err := drw.findBlock(path, block)
	if err != nil {
		return nil, err
	}
	return lsp.ToEdits(tree.Src, tree.Lines, []jomini.Edit{makeSetting(tree, statement, key, value)}), nil
}

// AddInstance returns the edits that add a placement to a layer of a coat. It returns an error if the file is not in
// the workspace, no block starts at the given range, or the layer takes no placement.
func (drw *Service) AddInstance(path string, layer lsp.Range) ([]lsp.Edit, error) {
	tree, statement, err := drw.findBlock(path, layer)
	if err != nil {
		return nil, err
	}
	role := strings.ToLower(tree.Unquoted(tree.Statements[statement].Key))
	instance, ok := game.Coat.Template.Instances[role]
	if !ok {
		return nil, fmt.Errorf("the game places no layer with the role %q", role)
	}
	edit := tree.BuildInsertion(statement, []string{instance})
	return lsp.ToEdits(tree.Src, tree.Lines, []jomini.Edit{edit}), nil
}

// findBlock returns the tree of a file and the block that starts where the given range does. It returns an error if
// the file is not in the workspace or no block starts there.
func (drw *Service) findBlock(path string, block lsp.Range) (*jomini.Tree, int32, error) {
	tree, statement, err := drw.findStatement(path, block)
	if err == nil && !tree.Statements[statement].IsBlock() {
		err = fmt.Errorf("the statement at line %d, column %d of %q is no block", block.StartLine, block.StartColumn, path)
	}
	return tree, statement, err
}

// findStatement returns the tree of a file and the statement that starts where the given range does. It returns an
// error if the file is not in the workspace or no statement starts there.
func (drw *Service) findStatement(path string, block lsp.Range) (*jomini.Tree, int32, error) {
	ws, err := drw.Workspace.Session()
	if err != nil {
		return nil, 0, err
	}
	tree, err := ws.LoadTree(path)
	if err != nil {
		return nil, 0, err
	}
	offset := lsp.ToOffset(tree.Src, tree.Lines, block.StartLine, block.StartColumn)
	statement, ok := tree.LookupStatement(offset)
	if !ok {
		return nil, 0, fmt.Errorf("no statement starts at line %d, column %d of %q", block.StartLine, block.StartColumn, path)
	}
	return tree, statement, nil
}

// resolveText returns the text to show for a property value, its messages found in the given index of the mods. A
// localization key becomes the segments of its text, as resolveSegments makes them; any other value is read as a text.
func resolveText(ws *workspace.Session, mods *index.Index, value string) []loc.Segment {
	text, _, ok := ws.LookupText(mods, value)
	return resolveSegments(ws, mods, loc.Segments(lo.Ternary(ok, text, value), 0), 0)
}

// resolveSegments returns what a player reads in a text: each $key$ as its message and each break as a newline.
func resolveSegments(ws *workspace.Session, mods *index.Index, segments []loc.Segment, depth int) []loc.Segment {
	return lo.FlatMap(segments, func(segment loc.Segment, _ int) []loc.Segment {
		switch segment.Markup {
		case loc.MarkupCall:
			return []loc.Segment{segment}
		case loc.MarkupKey:
			if text, _, ok := ws.LookupText(mods, segment.Text); ok && depth < maxDepth {
				return resolveSegments(ws, mods, loc.Segments(text, 0), depth+1)
			}
		}
		segment.Markup, segment.Text = loc.MarkupText, loc.ToPlain([]loc.Segment{segment})
		return lo.Ternary(segment.Text == "", nil, []loc.Segment{segment})
	})
}

// sampleText returns a text's segments with each data call replaced by its sample.
func (library *library) sampleText(segments []loc.Segment, context context, properties iter.Seq[Property]) []loc.Segment {
	segments = slices.Clone(segments)
	for position, segment := range segments {
		if segment.Markup != loc.MarkupCall {
			continue
		}
		sample, kind := library.sampleCall(segment.Text, context, properties)
		if position > 0 {
			last, _ := utf8.DecodeLastRuneInString(segments[position-1].Text)
			sample = lo.Ternary(unicode.IsSpace(last) || strings.ContainsRune(openingBrackets, last), "", " ") + sample
		}
		segments[position].Text, segments[position].Kind = sample, kind
	}
	return segments
}

// sampleCall returns the sample text of a data call and its kind. The same call always shows the same sample.
func (library *library) sampleCall(source string, context context, properties iter.Seq[Property]) (sample, kind string) {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(source))
	seed := hash.Sum32()
	call, _ := jomini.ParseCall(source, 0)
	path := context.expand(library.ws.Schema, call)
	function, _ := library.ws.Schema.FindFunction(path, "", len(path.Steps)-1)
	kind, known := game.GUI.CallKinds[function.Returns]
	switch {
	case kind == kindNumber:
		return sampleNumber(seed, call.Suffix), kind
	case isDate(function):
		return library.sampleDate(seed), kindDate
	case !known:
		return sampleWords(seed, 2), kindText
	case !isOn(properties, game.GUI.MultilineKey):
		return sampleWords(seed, 2+int(seed%2)), kind
	case isOn(properties, game.GUI.ResizeKey):
		return sampleSentences(seed, 1+int(seed%2)), kind
	}
	return sampleSentences(seed, len(loremSentences)), kind
}

// sampleNumber returns a sample number, picked by a seed and shaped by format codes.
func sampleNumber(seed uint32, codes string) string {
	decimals := 0
	if position := strings.IndexFunc(codes, unicode.IsDigit); position >= 0 {
		decimals = int(codes[position] - '0')
	}
	number := strconv.FormatFloat(minSample+float64(seed%9900)/100, 'f', decimals, 64)
	if strings.Contains(codes, game.GUI.PercentCode) {
		number += "%"
	}
	if strings.Contains(codes, game.GUI.SignCode) {
		number = "+" + number
	}
	return number
}

// isDate reports whether a data function writes a date, as game.GUI.DateCalls knows one: by the type it returns and the
// ending of its name.
func isDate(function schema.DataFunction) bool {
	calls := game.GUI.DateCalls
	return function.Returns == calls.Returns && slices.ContainsFunc(calls.Endings, func(ending string) bool {
		return strings.HasSuffix(function.Name, ending)
	})
}

// sampleDate returns a sample date, picked by a seed, in the game's form and the workspace's language.
func (library *library) sampleDate(seed uint32) string {
	form := game.GUI.DateForm
	month := form.Months[seed%uint32(len(form.Months))]
	values := map[string]string{
		form.Day: strconv.Itoa(1 + int(seed%28)), form.Month: library.lookupText(month),
		form.Year: strconv.Itoa(1000 + int(seed%900)),
	}
	var date strings.Builder
	for _, segment := range loc.Segments(library.lookupText(form.Key), 0) {
		if value, ok := values[segment.Text]; ok && segment.Markup == loc.MarkupKey {
			date.WriteString(value)
		} else {
			date.WriteString(loc.ToPlain([]loc.Segment{segment}))
		}
	}
	return date.String()
}

// lookupText returns the text of the message with the given key in the workspace's language, or the key where there is
// no such message.
func (library *library) lookupText(key string) string {
	text, _, ok := library.ws.LookupText(library.mods, key)
	return lo.Ternary(ok, text, key)
}

// sampleWords returns the given count of the first words of one lorem sentence, which the given seed picks.
func sampleWords(seed uint32, count int) string {
	return strings.Join(strings.Fields(sampleSentences(seed, 1))[:count], " ")
}

// sampleSentences returns the given count of lorem sentences in order, the first of which the given seed picks.
func sampleSentences(seed uint32, count int) string {
	first := int(seed % uint32(len(loremSentences)))
	sentences := make([]string, count)
	for position := range sentences {
		sentences[position] = loremSentences[(first+position)%len(loremSentences)]
	}
	return strings.Join(sentences, " ")
}

// isOn reports whether the given properties of a widget write the given key as yes.
func isOn(properties iter.Seq[Property], key string) bool {
	return game.GUI.Literals[strings.ToLower(findProperty(properties, key).Value)]
}

// findKeptClause returns the definition the game keeps of a name in a database, local ones aside. It returns false if
// there is none.
func findKeptClause(ws *workspace.Session, mods *index.Index, reference schema.Reference) (clause, bool) {
	kept, ok := mods.FindKept(ws.Schema, resolveReference(mods, reference))
	if !ok {
		return clause{}, false
	}
	return makeClause(ws, mods, kept)
}

// resolveReference returns a reference with its database: one given with none is in the one database that defines its
// name. Any other reference is returned as it is.
func resolveReference(mods *index.Index, reference schema.Reference) schema.Reference {
	if reference.Database != "" {
		return reference
	}
	if databases := mods.ListDatabases(reference.Name); len(databases) == 1 {
		reference.Database = databases[0]
	}
	return reference
}

// makeClause returns the clause of a definition the game keeps. It returns false if its file cannot be read or no
// longer has the definition's statement.
func makeClause(ws *workspace.Session, mods *index.Index, kept index.Winner) (clause, bool) {
	tree, statement, err := ws.FindStatement(mods, kept.Site)
	if err != nil {
		return clause{}, false
	}
	return clause{tree: tree, path: mods.File(kept.Site).Path, statement: statement}, true
}

// findProperty returns the property with the given key among the given ones, or the zero Property if there is none.
// The key matches without regard to case.
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
func toRange(field clause) lsp.Range {
	var span jomini.Span
	if field.statement != absentStatement {
		span = field.tree.Statements[field.statement].Span
	}
	return lsp.ToRange(field.tree.Src, field.tree.Lines, span)
}

// makeSetting returns the edit that sets a key of a block, replacing its value or adding it as the last statement.
func makeSetting(tree *jomini.Tree, holder int32, key, value string) jomini.Edit {
	setting, ok := int32(0), false
	for child := range tree.Children(holder) {
		if strings.EqualFold(tree.Unquoted(tree.Statements[child].Key), key) {
			setting, ok = child, true
		}
	}
	if !ok {
		return tree.BuildInsertion(holder, []string{key + " = " + value})
	}
	return jomini.Edit{Span: tree.Statements[setting].ResolveValueSpan(), Text: value}
}

// toIndented returns lines indented as the line holding an offset is, joined as the file ends its lines.
func toIndented(tree *jomini.Tree, offset int, lines []string) string {
	row := tree.Lines.LineSpan(tree.Lines.ToPosition(offset).Line)
	text := tree.Src[row.Start:row.End]
	return tree.IndentLines(lines, text[:len(text)-len(strings.TrimLeft(text, " \t"))])
}

// toLineSpan returns the span of the whole lines a statement takes, with the line ending of the last. A statement
// that shares a line with another is the span itself.
func toLineSpan(tree *jomini.Tree, span jomini.Span) jomini.Span {
	first, last := tree.Lines.ToPosition(span.Start).Line, tree.Lines.ToPosition(span.End).Line
	start, end := tree.Lines.LineSpan(first).Start, tree.Lines.LineSpan(last).End
	if strings.TrimSpace(tree.Src[start:span.Start]) != "" || strings.TrimSpace(tree.Src[span.End:end]) != "" {
		return span
	}
	if last+1 < tree.Lines.CountLines() {
		end = tree.Lines.LineSpan(last + 1).Start
	}
	return jomini.Span{Start: start, End: end}
}
