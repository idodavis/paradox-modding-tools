// Package draw builds what the Visual view shows. It resolves a GUI window into its widgets and a coat of arms
// into its layers, and it produces the text edits the view's panels make.
package draw

import (
	"cmp"
	"fmt"
	"iter"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/samber/lo"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/loc"
	"paradox-modding-tools/internal/schema"
	"paradox-modding-tools/services/lsp"
	"paradox-modding-tools/services/workspace"
)

// Service is the Wails service behind the Visual view. A method that changes an existing file does not write it. It
// returns edits, which the editor applies to its own model, so there is only ever one copy of the text.
type Service struct {
	Workspace *workspace.Service
}

// A Property is one property of a widget, with the place where it is written. That place can be a template or the
// widget's type instead of the widget itself.
type Property struct {
	Key string `json:"key"`
	// Value is what the game reads. A constant or an expression is worked out, and a GetDefine call is replaced by the
	// value of its define. The same holds for each value inside a block. Any other value is as written.
	Value  string          `json:"value"`
	Range  lsp.Range       `json:"range"`
	Place  workspace.Place `json:"place"`
	Origin string          `json:"origin"` // "widget", "template" or "type"; "missing" if no file defines the source
}

// A Widget is one element of a resolved window, with everything needed to draw it. The page lays it out.
type Widget struct {
	ID int32 `json:"id"`
	// Parent is the widget that holds it; the window's own widget is 0, and so is its parent.
	Parent     int32             `json:"parent"`
	Name       string            `json:"name"`
	Type       string            `json:"type"`
	Row        game.WidgetType   `json:"row"`  // the game's row for its type
	Role       string            `json:"role"` // the slot it fills in its holder, or "" for a plain child
	Properties []Property        `json:"properties"`
	Texts      map[string]string `json:"texts"`    // by property key; a data binding shows as […]
	Textures   map[string]string `json:"textures"` // by property key; the image file for the property's path
	Overlays   []Overlay         `json:"overlays"`
	Range      lsp.Range         `json:"range"`
	Place      workspace.Place   `json:"place"`
}

// An Overlay is one texture laid over a widget's own, as a modify_texture block writes it.
type Overlay struct {
	Path       string     `json:"path"` // the image file for its texture; "" if there is none
	Properties []Property `json:"properties"`
}

// A Window is one GUI window resolved for drawing. Its widgets are in document order, and its holders are the game
// windows that contain it.
type Window struct {
	Name        string   `json:"name"`
	Widgets     []Widget `json:"widgets"`
	Holders     []string `json:"holders"`
	Datacontext string   `json:"datacontext"` // the data context its bindings read
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
	states    []clause   // the states it opens in
	overlays  []Overlay
}

// A resolver makes the widgets of one window, one widget at a time, and holds them until the window is done.
type resolver struct {
	ws          *workspace.Session
	mods        *index.Index
	places      map[string]workspace.Place // by path
	definitions map[string]clause          // the definition of a type or template, by its caller, its label and its file
	ancestors   []clause
	folders     map[string]string // the icon folders of the game, by the define that names each
	gathered    gathering
	widgets     []Widget
}

// The words a GUI file is written with, the roles of a slot, the origins of a property, and the deepest that types,
// templates and labeled blocks nest before they count as a cycle.
const (
	typeKey        = "type"
	stateKey       = "state"
	itemKey        = "item"
	overlayKey     = "modify_texture"
	rawTextKey     = "raw_text"
	blockLead      = "block"
	roleLabel      = "label"
	roleAttachment = "attachment"
	originWidget   = "widget"
	originTemplate = "template"
	originType     = "type"
	originMissing  = "missing"
	callText       = "[…]"
	maxDepth       = 8

	absentStatement int32 = 0
)

// ListDrawables lists what a file defines that the view can draw, in document order: a window, a type, a template or a
// coat, nested ones included. A file that is not a GUI file or a coat file has nothing to draw.
func (drw *Service) ListDrawables(path string) ([]lsp.Symbol, error) {
	ws, err := drw.Workspace.Session()
	if err != nil {
		return nil, err
	}
	symbols, err := lsp.ListFileSymbols(ws, path)
	if err != nil {
		return nil, err
	}
	return listDrawn(ws.Schema, symbols), nil
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

// ResolveWindow resolves one window in a GUI file. It applies the window's type and templates, looks up its
// texts and finds its textures. A window wins over a type or a template of the same name. It returns an error if
// the file is not in the workspace or does not define the window. A type or template that cannot be found becomes a
// Property with Origin "missing", so the page can flag it.
func (drw *Service) ResolveWindow(path, name string) (Window, error) {
	ws, err := drw.Workspace.Session()
	if err != nil {
		return Window{}, err
	}
	mods := ws.Mods.Load()
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
	resolver := &resolver{ws: ws, mods: mods, places: map[string]workspace.Place{}, definitions: map[string]clause{},
		folders: findIconFolders(ws, mods)}
	root, key := resolver.findRoot(definition, mods.ToReference(site).Database, name)
	resolver.buildWidget(child{clause: root}, 0, key)
	widgets := resolver.widgets
	widgets[0].Name = name
	if root.statement == absentStatement {
		widgets[0].Range, widgets[0].Place = toRange(definition), locate(resolver.ws, resolver.mods, resolver.places, path)
	}
	return Window{
		Name:        name,
		Widgets:     widgets,
		Holders:     resolver.findHolders(key, root),
		Datacontext: findProperty(widgets[0].Properties, game.GUI.ContextKey).Value,
	}, nil
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

// locate returns where a file sits in the given index of the mods. The given map keeps each place, so a file is
// located once.
func locate(ws *workspace.Session, mods *index.Index, places map[string]workspace.Place, path string) workspace.Place {
	place, ok := places[path]
	if !ok {
		place = ws.Locate(mods, path)
		places[path] = place
	}
	return place
}

// buildWidget makes the widget that a clause writes, then the widgets inside it. The given key is the clause's type;
// "" takes the clause's own key. The widget's properties come from its type, its templates and its own block, the last
// written of each key.
func (resolver *resolver) buildWidget(own child, parent int32, key string) {
	own.origin = originWidget
	if own.statement != absentStatement {
		key = cmp.Or(key, own.tree.Unquoted(own.tree.Statements[own.statement].Key))
	}
	applied := cmp.Or(game.GUI.Slots[strings.ToLower(key)].Type, key)
	id := int32(len(resolver.widgets))
	resolver.widgets = append(resolver.widgets, Widget{
		ID: id, Parent: parent, Type: applied, Role: own.role, Place: locate(resolver.ws, resolver.mods, resolver.places, own.path), Range: toRange(own.clause),
	})
	outer, outerAncestors := resolver.gathered, resolver.ancestors
	resolver.gathered = gathering{overrides: map[string]clause{}}
	maps.Copy(resolver.gathered.overrides, outer.overrides)
	if own.statement != absentStatement {
		resolver.ancestors = append(resolver.ancestors, own.clause)
	}
	defer func() { resolver.gathered, resolver.ancestors = outer, outerAncestors }()
	builtin, chain := resolver.listTypes(applied, own.clause)
	links := slices.Concat(chain, []clause{own.clause})
	resolver.listOverrides(links)
	var properties []Property
	for _, link := range links {
		if link.statement != absentStatement {
			properties = append(properties, resolver.resolveBlock(link, 0)...)
		}
	}
	properties = append(properties, resolver.resolveAlone(own.clause)...)
	properties = append(properties, resolver.resolveOpening()...)
	resolver.resolveWidget(id, builtin, append(properties, resolver.gathered.missing...))
	resolver.resolveChildren(id)
}

// listTypes returns the built-in type that a type is made on, and the definitions it passes through, base first. A
// type that no file defines is a built-in, or missing if the schema has no such widget. A chain that is too deep is a
// cycle and marks the first type missing. The given clause says which file's local definitions count.
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

// findDeclared returns the clause of the definition that a caller's databases keep of a label, local ones first. Its
// statement is absentStatement if the game keeps no definition. It is dropped if files define the label but the game
// keeps none of those definitions.
func (resolver *resolver) findDeclared(owner clause, caller, label string) clause {
	exists := false
	for _, declared := range slices.Backward(game.GUI.Declares) {
		database := game.GUI.DatabasePrefix + declared
		if !slices.Contains(listCallers(resolver.ws.Schema, database), caller) {
			continue
		}
		sites := resolver.mods.ResolveLocal(schema.Reference{Database: database, Name: label}, owner.path)
		if kept, ok := resolver.mods.FindWinner(sites, resolver.ws.Schema.Databases[database].Precedence); ok {
			if definition, loaded := makeClause(resolver.ws, resolver.mods, kept); loaded {
				return definition
			}
		}
		exists = exists || len(sites) > 0
	}
	return clause{statement: absentStatement, dropped: exists}
}

// isBuiltin reports whether a type is one the engine draws without a file defining it. A type that files define but the
// game keeps none of is not one.
func (resolver *resolver) isBuiltin(owner clause, label string) bool {
	definition, _ := resolver.findCalled(owner, game.GUI.TagCalls, label)
	return resolver.ws.Schema.WidgetTypes[label] && !definition.dropped
}

// makeMissing returns the property that marks a type or template the game does not define, used by a clause.
func (resolver *resolver) makeMissing(key, value string, source clause) Property {
	return Property{Key: key, Value: value, Origin: originMissing, Place: locate(resolver.ws, resolver.mods, resolver.places, source.path), Range: toRange(source)}
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

// listFields yields each field of a clause with the bare value written just before it, in the order the game applies
// them. A using is replaced by the fields of its template. A template that no file defines, or one nested too deep,
// is marked missing.
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

// findLead returns the bare value written just before a statement among its siblings, in lower case, which says what
// a labeled block is: block or blockoverride. It returns "" if there is none.
func findLead(tree *jomini.Tree, siblings []int32, position int) string {
	if position == 0 {
		return ""
	}
	previous := tree.Statements[siblings[position-1]]
	if previous.IsListed() {
		return strings.ToLower(tree.Text(previous.Value))
	}
	return ""
}

// resolveBlock returns the properties that a clause gives its widget, and gathers the widgets it holds, the states it
// opens in and the textures it overlays. A labeled block is read as written, or as the widget replaces it. The given
// depth is how many types, templates and labeled blocks lie between the clause and its widget.
func (resolver *resolver) resolveBlock(holder clause, depth int) []Property {
	var properties []Property
	for lead, field := range resolver.listFields(holder, depth) {
		current := field.tree.Statements[field.statement]
		key := cmp.Or(field.tree.Unquoted(current.Key), field.tree.Unquoted(current.Tag))
		slot, slotted := game.GUI.Slots[strings.ToLower(key)]
		switch {
		case lead == blockLead:
			properties = append(properties, resolver.resolveLabeled(field, depth)...)
		case lead == game.GUI.BlockCalls:
		case current.IsBlock() && (key == itemKey || slot.Role == roleLabel):
			resolver.gatherInner(field, slot)
		case current.IsBlock() && slotted:
			if slot.Role != roleAttachment {
				resolver.gathered.children = append(resolver.gathered.children, child{clause: field, role: slot.Role})
			}
		case current.IsBlock() && resolver.isWidget(field, key):
			resolver.gathered.children = append(resolver.gathered.children, child{clause: field})
		case current.IsBlock() && key == stateKey:
			if isOpening(field) {
				resolver.gathered.states = append(resolver.gathered.states, field)
			}
		default:
			if current.IsBlock() && key == overlayKey {
				resolver.gathered.overlays = append(resolver.gathered.overlays, resolver.makeOverlay(field, depth))
			}
			properties = append(properties, resolver.makeProperty(field))
		}
	}
	return properties
}

// resolveLabeled returns the properties of a labeled block that a clause holds. They are those of the block the widget
// replaces it with, unless the same type or template wrote both, else of the block as written.
func (resolver *resolver) resolveLabeled(field clause, depth int) []Property {
	override, ok := resolver.gathered.overrides[field.tree.Unquoted(field.tree.Statements[field.statement].Tag)]
	if ok && override.writer != field.writer {
		field = override
	}
	return resolver.resolveBlock(field, depth+1)
}

// gatherInner gathers the widgets written inside a label or an item as children of the widget. A label replaces the
// one gathered before it.
func (resolver *resolver) gatherInner(field clause, slot game.WidgetSlot) {
	if slot.Role == roleLabel {
		resolver.gathered.children = slices.DeleteFunc(resolver.gathered.children, func(held child) bool {
			return held.role == roleLabel
		})
	}
	for statement := range field.tree.Children(field.statement) {
		current := field.tree.Statements[statement]
		inner := clause{tree: field.tree, path: field.path, statement: statement, writer: field.writer}
		if current.IsBlock() && resolver.isWidget(inner, field.tree.Unquoted(current.Key)) {
			resolver.gathered.children = append(resolver.gathered.children, child{clause: inner, role: slot.Role, item: slot.Role == ""})
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
func isOpening(state clause) bool {
	opening := false
	for statement := range state.tree.Children(state.statement) {
		current := state.tree.Statements[statement]
		key, value := state.tree.Unquoted(current.Key), state.tree.Unquoted(current.Value)
		opening = opening || key == game.GUI.WidgetKey && slices.Contains(game.GUI.OpeningStates, value) ||
			value != "" && strings.EqualFold(game.GUI.OpeningTriggers[key], value)
	}
	return opening
}

// makeOverlay returns the overlay that a modify_texture block writes: its properties, and the image file its texture
// leads to.
func (resolver *resolver) makeOverlay(field clause, depth int) Overlay {
	var properties []Property
	for lead, inner := range resolver.listFields(field, depth+1) {
		if lead == "" {
			properties = append(properties, resolver.makeProperty(inner))
		}
	}
	properties = resolveProperties(properties)
	return Overlay{Path: resolveTexture(resolver.ws, resolver.mods, resolver.folders, findProperty(properties, textureKey).Value), Properties: properties}
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
		Place: locate(resolver.ws, resolver.mods, resolver.places, field.path), Range: toRange(field),
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
	arguments := call.Steps[0].Arguments
	reference := schema.Reference{Database: lookupDefineDatabase(), Name: arguments[len(arguments)-1].Text}
	define, ok := findKeptClause(resolver.ws, resolver.mods, reference)
	if !ok {
		return "", false
	}
	return define.tree.Unquoted(define.tree.Statements[define.statement].ResolveValueSpan()), true
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

// resolveOpening returns the properties that the states a widget opens in give it: the end values of the states, which
// the widget has as its own. The keys that a state has for itself are left out.
func (resolver *resolver) resolveOpening() []Property {
	var properties []Property
	for _, state := range resolver.gathered.states {
		for lead, field := range resolver.listFields(state, 0) {
			current := field.tree.Statements[field.statement]
			own := slices.ContainsFunc(game.GUI.StateKeys, func(key string) bool {
				return strings.EqualFold(key, field.tree.Unquoted(current.Key))
			})
			if lead == "" && !own && !current.IsBlock() {
				properties = append(properties, resolver.makeProperty(field))
			}
		}
	}
	return properties
}

// resolveWidget gives a widget its properties, the last written of each key, and what they lead to: its name, the row
// of its built-in type, its texts, its textures and its overlays.
func (resolver *resolver) resolveWidget(id int32, builtin string, properties []Property) {
	properties = resolveProperties(properties)
	texts, textures := resolver.resolvePictures(properties)
	widget := &resolver.widgets[id]
	widget.Name = findProperty(properties, game.GUI.WidgetKey).Value
	widget.Row = game.GUI.Types[builtin]
	widget.Row.DropsHidden = resolver.isDropping(widget.Row, builtin, findProperty(properties, game.GUI.DropsHiddenKey).Value)
	widget.Properties, widget.Texts, widget.Textures = properties, texts, textures
	widget.Overlays = slices.Concat([]Overlay{}, resolver.gathered.overlays)
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
func (resolver *resolver) resolvePictures(properties []Property) (texts, textures map[string]string) {
	texts, textures = map[string]string{}, map[string]string{}
	for _, property := range properties {
		switch {
		case property.Origin == originMissing:
		case slices.Contains(game.GUI.PictureKeys, property.Key):
			if path := resolveTexture(resolver.ws, resolver.mods, resolver.folders, property.Value); path != "" {
				textures[property.Key] = path
			}
		case isLocalized(property.Key) && property.Value != "":
			texts[property.Key] = resolveText(resolver.ws, resolver.mods, property.Value)
		case property.Key == rawTextKey && property.Value != "":
			texts[property.Key] = toPlainText(resolver.ws, resolver.mods, property.Value)
		}
	}
	return texts, textures
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

// findHolders returns the game window that a window of the given type is laid out in: the one the game names for the
// last type of its chain that has one, if the install defines it. It returns none otherwise.
func (resolver *resolver) findHolders(key string, root clause) []string {
	_, chain := resolver.listTypes(key, root)
	for _, link := range slices.Backward(chain) {
		holder, ok := resolver.ws.Game.GUIHolders[link.writer]
		reference := schema.Reference{Database: game.GUI.WidgetDatabase, Name: holder}
		if _, defined := resolver.mods.FindKept(resolver.ws.Schema, reference); ok && defined {
			return []string{holder}
		}
	}
	return []string{}
}

// EditProperty sets one property of a block and returns the edits that do it. The block is a widget, a coat, a layer
// or an instance, located by the given file and range. If the block already sets the property, the edit replaces its
// value. Otherwise the edit adds the property at the end of the block. It returns an error if the file is not in the
// workspace or no block starts where the range does.
func (drw *Service) EditProperty(path string, block lsp.Range, key, value string) ([]lsp.Edit, error) {
	tree, statement, err := drw.findBlock(path, block)
	if err != nil {
		return nil, err
	}
	return lsp.ToEdits(tree.Src, tree.Lines, []jomini.Edit{makeSetting(tree, statement, key, value)}), nil
}

// AddInstance adds one placement to a layer of a coat and returns the edits that do it. The placement is the game's
// template for one in the layer's role, written as the layer's last statement. It returns an error if the file is not
// in the workspace, no block starts where the range does, or the game places no layer with the role of that block, as
// with a pattern.
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
	if !ok || !tree.Statements[statement].IsBlock() {
		return nil, 0, fmt.Errorf("no block starts at line %d, column %d of %q", block.StartLine, block.StartColumn, path)
	}
	return tree, statement, nil
}

// resolveText returns the text to show for a property value, its messages found in the given index of the mods. A
// localization key becomes its text, a macro is expanded, and a data binding shows as […] because bindings are never
// evaluated.
func resolveText(ws *workspace.Session, mods *index.Index, value string) string {
	text, _, ok := ws.LookupText(mods, value)
	return toPlainText(ws, mods, lo.Ternary(ok, text, value))
}

// toPlainText returns the text a player reads in a localization text: each $key$ replaced by the text of the message
// it names in the given index of the mods, each data call by […], and the markup dropped.
func toPlainText(ws *workspace.Session, mods *index.Index, text string) string {
	return loc.ToPlain(resolveSegments(ws, mods, loc.Segments(text, 0), 0))
}

// resolveSegments returns the segments of a text with each $key$ replaced by the segments of the message it names in
// the given index of the mods, and each data call replaced by […]. A key that names no message stays as it is.
func resolveSegments(ws *workspace.Session, mods *index.Index, segments []loc.Segment, depth int) []loc.Segment {
	return lo.FlatMap(segments, func(segment loc.Segment, _ int) []loc.Segment {
		switch segment.Markup {
		case loc.MarkupCall:
			return []loc.Segment{{Markup: loc.MarkupText, Text: callText}}
		case loc.MarkupKey:
			if text, _, ok := ws.LookupText(mods, segment.Text); ok && depth < maxDepth {
				return resolveSegments(ws, mods, loc.Segments(text, 0), depth+1)
			}
		}
		return []loc.Segment{segment}
	})
}

// findKeptClause returns the clause of the definition the game keeps of a name in a database. A local definition never
// counts. It returns false if the schema has no such database, nothing defines the name, or the game keeps none of its
// definitions.
func findKeptClause(ws *workspace.Session, mods *index.Index, reference schema.Reference) (clause, bool) {
	kept, ok := mods.FindKept(ws.Schema, reference)
	if !ok {
		return clause{}, false
	}
	return makeClause(ws, mods, kept)
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

// findProperty returns the property with the given key, or the zero Property if there is none.
func findProperty(properties []Property, key string) Property {
	return lo.FindOrElse(properties, Property{}, func(property Property) bool { return strings.EqualFold(property.Key, key) })
}

// resolveProperties returns the properties with the last written of each key, in the order the keys are first written.
// Keys match without regard to case. A missing type or template is kept with each name it was written for.
func resolveProperties(properties []Property) []Property {
	resolved := []Property{}
	positions := map[string]int{}
	for _, property := range properties {
		key := strings.ToLower(property.Key)
		if property.Origin == originMissing {
			key = property.Origin + key + property.Value
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

// makeSetting returns the edit that sets a key of a block to a value. It replaces the whole value where the block
// writes the key, else it adds the key as the block's last statement. The key matches without quotes or case, and the
// last statement that has it counts.
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
