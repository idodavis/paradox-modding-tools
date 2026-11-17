// merge.go puts a window together from its files: each widget from its own
// block, its type chain, the templates it uses and the blocks it overrides.

package draw

import (
	"cmp"
	"fmt"
	"iter"
	"maps"
	"slices"
	"strings"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/services/workspace"
)

// clause is one statement of a file. Where it is part of a widget, it carries
// the type or template that wrote it.
type clause struct {
	tree *jomini.Tree
	path string

	// statement is absentStatement where there is no statement.
	statement int32

	// writer is the type or template that wrote it, "" for the widget's own
	// block. origin is widget, template or type.
	writer string
	origin string

	// dropped is whether files define it but the game keeps none of them.
	dropped bool
}

// widgetSlot is a key whose block is a widget that no type names.
type widgetSlot struct {
	// Role is what the block is to its widget: skin (a background), chrome (a
	// scroll bar), attachment (a tooltip) or label (its text).
	Role string

	// Type is the built-in type the block is drawn as, "" for the key's own.
	Type string

	// Floats is whether the block floats apart, as a tooltip does, so it is
	// not drawn as a child.
	Floats bool

	// Dresses is whether the block only dresses its widget, so a picture it
	// lacks draws no placeholder.
	Dresses bool

	// Labels is whether the block's widgets show their widget's text. They
	// come before its other children, and a later label replaces them.
	Labels bool
}

// child is a clause that a widget holds as a widget of its own.
type child struct {
	clause

	// slot is the zero slot for a plain child. item is whether the child is
	// an item of a data model.
	slot widgetSlot
	item bool
}

// gathering is what one widget collects from its own block and from the
// blocks of its types and templates.
type gathering struct {
	// overrides holds the blocks the widget replaces, by the name of the block
	// each replaces.
	overrides map[string]clause

	children []child

	// missing are the types and templates it uses that no file defines.
	missing []Property

	states []clause

	// overlays are positions in the window's overlays.
	overlays []int32

	// triggers holds, by the name of a widget below, the states that an
	// opening call above starts on it through FindChild and TriggerAnimation.
	triggers map[string][]string
}

// resolver makes the widgets of one window. The resolvers of a window's
// holders share its places, properties, overlays and library.
type resolver struct {
	ws   *workspace.Session
	mods *index.Index

	// file is the file being drawn. isRead is whether the game loads it
	// rather than another file at its path.
	file   int32
	isRead bool

	// places holds the places of the files, by path.
	places     *table[string, workspace.Place]
	properties *ledger
	overlays   *table[string, Overlay]

	// definitions caches the definition found for each type and template, by
	// caller, name and file.
	definitions map[string]clause

	ancestors []clause
	library   *library
	gathered  gathering
	widgets   []Widget

	// contexts holds where each widget's data calls start from, by widget.
	contexts []context

	// colorLists holds the lists of each widget's color bindings, by widget.
	colorLists map[int32][]PictureList
}

// table is a list that keeps each value once, by its key, in the order first
// added.
type table[K comparable, V any] struct {
	values    []V
	positions map[K]int32
}

const (
	// maxDepth is how deep templates, types and texts may nest.
	maxDepth = 8

	absentStatement int32 = 0
)

// widgetSlots are the keys whose block is a widget that no type names.
var widgetSlots = map[string]widgetSlot{
	"background":           {Role: "skin", Type: "background", Dresses: true},
	"tooltipwidget":        {Role: "attachment", Floats: true},
	"scrollbar_vertical":   {Role: "chrome", Type: "scrollbar"},
	"scrollbar_horizontal": {Role: "chrome", Type: "scrollbar"},
	"buttontext":           {Role: "label", Labels: true},
	"scrollwidget":         {Type: "widget"},
	"zoomwidget":           {Type: "widget"},
}

// makeResolver returns a resolver of the windows of one file, drawn from the
// given index.
func makeResolver(ws *workspace.Session, mods *index.Index, path string, library *library) *resolver {
	file, _ := mods.FindFile(path)

	return &resolver{
		ws: ws, mods: mods, file: file, isRead: mods.IsLoaded(file),
		places: &table[string, workspace.Place]{}, properties: &ledger{}, overlays: &table[string, Overlay]{},
		definitions: map[string]clause{}, library: library,
	}
}

// resolveWindow resolves the window a file defines under the given name.
// Unlike ResolveWindow, it leaves the places, fonts, conditions, missing
// types and groups to its caller.
func (r *resolver) resolveWindow(path, name string) (Window, error) {
	ws, mods := r.ws, r.mods
	definition, site, err := findDrawn(ws, mods, path, name,
		func(database string) bool { return database == lang.GUIWidgetDatabase },
		func(database string) bool { return isDrawn(ws.Schema, database) })
	if err != nil {
		return Window{}, err
	}

	root, key := r.findRoot(definition, mods.ToReference(site).Database, name)
	r.buildWidget(child{clause: root}, 0, key)
	widgets := r.widgets
	widgets[0].Name = name
	if root.statement == absentStatement {
		widgets[0].Range, widgets[0].Place = toRange(definition), r.locate(path)
	}

	// the holders come first: resolving them adds to the properties the
	// window takes below
	window := Window{Name: name, Widgets: widgets, Holders: r.findHolders(key, root)}
	window.Properties, window.Overlays = r.properties.values, r.overlays.values
	window.calls = r.properties.calls
	r.contexts, r.colorLists = listContexts(ws.Schema, window), map[int32][]PictureList{}
	for id, context := range r.contexts {
		r.resolveBindings(&window, &window.Widgets[id], context)
	}

	return window, nil
}

// add returns the position of a key's value. The value is made by the given
// function the first time the key is given.
func (t *table[K, V]) add(key K, build func() V) int32 {
	if t.positions == nil {
		t.positions = map[K]int32{}
	}

	position, ok := t.positions[key]
	if !ok {
		position = int32(len(t.values))
		t.values = append(t.values, build())
		t.positions[key] = position
	}

	return position
}

// addGroup returns the group with an ID, made by the given function the
// first time, with the widget among its readers.
func addGroup(groups *table[string, Group], id string, reader int32, build func() Group) *Group {
	group := &groups.values[groups.add(id, build)]
	group.Readers = appendNew(group.Readers, reader)

	return group
}

// appendNew appends a value to a list that does not hold it yet.
func appendNew[T comparable](list []T, value T) []T {
	if slices.Contains(list, value) {
		return list
	}

	return append(list, value)
}

// findDrawn returns the first definition a file writes under a name, letter
// case aside, and its site. Each test is a filter on the database, tried in
// turn until one finds a definition. It returns an error if the file is not
// in the index, does not define the name, or changed since it was scanned.
func findDrawn(
	ws *workspace.Session, mods *index.Index, path, label string, tests ...func(database string) bool,
) (clause, index.Site, error) {
	file, ok := mods.FindFile(path)
	if !ok {
		return clause{}, index.Site{}, fmt.Errorf("the file %q is not in the workspace", path)
	}

	for _, accepts := range tests {
		for position, occurrence := range mods.Occurrences[file] {
			site := index.Site{File: file, Occurrence: int32(position)}
			reference := mods.ToReference(site)
			if occurrence.Role != index.RoleDefines || !accepts(reference.Database) || !strings.EqualFold(reference.Name, label) {
				continue
			}

			if definition, ok := makeClause(ws, mods, index.Winner{Site: site}); ok {
				return definition, site, nil
			}

			return clause{}, index.Site{}, fmt.Errorf("the file %q has changed since it was scanned", path)
		}
	}

	return clause{}, index.Site{}, fmt.Errorf("the file %q does not define %q", path, label)
}

// findRoot returns the clause a drawn definition is made of, and the type of
// its widget. A type is drawn as a widget of that type, and a template as a
// plain widget that uses it.
func (r *resolver) findRoot(definition clause, database, label string) (root clause, key string) {
	callees := r.ws.Schema.Callees
	key = definition.tree.Unquoted(definition.tree.Statements[definition.statement].Key)
	root = clause{tree: definition.tree, path: definition.path, statement: absentStatement}
	switch {
	case slices.Contains(callees[lang.GUITagCalls], database):
		return root, key

	case slices.Contains(callees[lang.GUITemplateCalls], database):
		root.writer = label

		return root, lang.GUITagCalls
	}

	return definition, key
}

// locate returns the position of a file's place among the window's places.
func (r *resolver) locate(path string) int32 {
	return r.places.add(path, func() workspace.Place { return r.ws.Locate(r.mods, path) })
}

// buildWidget makes the widget a clause writes, then the widgets inside it.
// An empty key takes the clause's own.
func (r *resolver) buildWidget(own child, parent int32, key string) {
	own.origin = originWidget
	if own.statement != absentStatement {
		key = cmp.Or(key, own.tree.Unquoted(own.tree.Statements[own.statement].Key))
	}

	slotKey := strings.ToLower(key)
	slot, isSlot := widgetSlots[slotKey]
	if !isSlot {
		slotKey = ""
	}

	widgetType := cmp.Or(slot.Type, key)
	id := int32(len(r.widgets))
	r.widgets = append(r.widgets, Widget{
		ID: id, Parent: parent, Type: widgetType, Slot: slotKey, Role: own.slot.Role,
		Item: own.item, Place: r.locate(own.path), Range: toRange(own.clause),
	})

	outer, outerAncestors := r.gathered, r.ancestors
	r.gathered = gathering{overrides: map[string]clause{}, triggers: map[string][]string{}}
	maps.Copy(r.gathered.overrides, outer.overrides)
	maps.Copy(r.gathered.triggers, outer.triggers)
	if own.statement != absentStatement {
		r.ancestors = append(r.ancestors, own.clause)
	}

	defer func() { r.gathered, r.ancestors = outer, outerAncestors }()

	builtin, chain := r.listTypes(widgetType, own.clause)
	r.widgets[id].TypeDefined = len(chain) > 0
	links := slices.Concat(chain, []clause{own.clause})
	r.listOverrides(links)

	var properties []Property
	for _, link := range links {
		if link.statement != absentStatement {
			properties = append(properties, r.resolveBlock(link, 0)...)
		}
	}

	properties = append(properties, r.resolveAlone(own.clause)...)
	name := findProperty(slices.Values(resolveProperties(properties)), lang.GUIWidgetKey).Value
	properties = append(properties, r.resolveOpening(name)...)
	r.resolveWidget(id, builtin, append(properties, r.gathered.missing...))
	r.resolveChildren(id)
}

// listTypes returns the built-in type a type is made on, and the definitions
// in between, the one made on the built-in type first. An unknown type and a
// type made on itself are marked missing.
func (r *resolver) listTypes(label string, owner clause) (string, []clause) {
	builtin := label
	var chain []clause
	for range maxDepth {
		definition, ok := r.findCalled(owner, lang.GUITagCalls, builtin)
		if !ok {
			if builtin != "" && !r.isBuiltin(owner, builtin) {
				r.gathered.missing = append(r.gathered.missing, r.makeMissing(lang.GUITypeKey, builtin, owner))
			}

			return builtin, chain
		}

		chain = slices.Insert(chain, 0, definition)
		base := cmp.Or(definition.tree.Unquoted(definition.tree.Statements[definition.statement].Tag), lang.GUITagCalls)
		if strings.EqualFold(base, builtin) {
			return builtin, chain
		}

		builtin = base
	}

	r.gathered.missing = append(r.gathered.missing, r.makeMissing(lang.GUITypeKey, label, owner))

	return label, nil
}

// findCalled returns the definition the game keeps of a type or a template.
// caller is the word that calls it: widget for a type, using for a template.
// It returns false if the game keeps none.
func (r *resolver) findCalled(owner clause, caller, label string) (clause, bool) {
	cacheKey := strings.Join([]string{caller, label, owner.path}, "\x00")
	definition, ok := r.definitions[cacheKey]
	if !ok {
		definition = r.findDeclared(owner, caller, label)
		r.definitions[cacheKey] = definition
	}

	definition.writer, definition.origin = label, originTemplate
	if caller == lang.GUITagCalls {
		definition.origin = originType
	}

	return definition, definition.statement != absentStatement
}

// findDeclared returns the definition the game keeps of a type or a template,
// preferring one in the owner's own file. Its statement is absentStatement
// where the game keeps none.
func (r *resolver) findDeclared(owner clause, caller, label string) clause {
	exists := false
	file, ok := r.mods.FindFile(owner.path)
	if !ok {
		file = -1
	}

	for _, database := range r.ws.Schema.Callees[caller] {
		sites := r.mods.ResolveLocal(lang.Reference{Database: database, Name: label}, file)
		if kept, ok := r.findWinner(sites, r.ws.Schema.Databases[database].Precedence); ok {
			if definition, isLoaded := makeClause(r.ws, r.mods, kept); isLoaded {
				return definition
			}
		}

		exists = exists || len(sites) > 0
	}

	return clause{statement: absentStatement, dropped: exists}
}

// findWinner returns the definition the game keeps among the given ones, as
// if it loaded the file being drawn. A file the game does not load is drawn
// with its own definition, and without those of the file it replaces.
func (r *resolver) findWinner(sites []index.Site, precedence lang.Precedence) (index.Winner, bool) {
	mods := r.mods
	if r.isRead {
		return mods.FindWinner(sites, precedence)
	}

	if own := slices.IndexFunc(sites, func(site index.Site) bool { return site.File == r.file }); own >= 0 {
		return index.Winner{Site: sites[own]}, true
	}

	replacer, _, _ := mods.FindLoaded(mods.Files[r.file].Rel)
	others := slices.DeleteFunc(slices.Clone(sites), func(site index.Site) bool {
		return mods.File(site).Key == replacer.Key
	})

	return mods.FindWinner(others, precedence)
}

// isBuiltin reports whether the game draws a type with no file defining it.
func (r *resolver) isBuiltin(owner clause, label string) bool {
	definition, _ := r.findCalled(owner, lang.GUITagCalls, label)

	return r.ws.Schema.LookupDefinition(lang.GUIWidgetTypes, label) != nil && !definition.dropped
}

// makeMissing returns the property that marks a type or a template that a
// clause uses and no file defines.
func (r *resolver) makeMissing(key, value string, source clause) Property {
	return Property{
		Key: key, Value: value, Origin: originMissing, Source: describeSource(originMissing, ""),
		Place: r.locate(source.path), Range: toRange(source),
	}
}

// describeSource returns where a property comes from in words, such as "type
// button" or "this widget".
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

// listOverrides records the named blocks the given clauses replace, a later
// clause over an earlier one.
func (r *resolver) listOverrides(links []clause) {
	for _, link := range links {
		if link.statement == absentStatement {
			continue
		}

		for lead, field := range r.listFields(link, 0) {
			if lead == lang.GUIBlockCalls {
				r.gathered.overrides[field.tree.Unquoted(field.tree.Statements[field.statement].Tag)] = field
			}
		}
	}
}

// listFields yields each field of a clause with its lead, in the order the
// game applies them. A using yields its template's fields, and a named block
// those of the block that replaces it.
func (r *resolver) listFields(holder clause, depth int) iter.Seq2[string, clause] {
	return func(yield func(string, clause) bool) {
		if depth > maxDepth {
			r.gathered.missing = append(r.gathered.missing, r.makeMissing(lang.GUITemplateCalls, holder.writer, holder))

			return
		}

		for statement := range holder.tree.Children(holder.statement) {
			current := holder.tree.Statements[statement]
			field := clause{tree: holder.tree, path: holder.path, statement: statement, writer: holder.writer, origin: holder.origin}
			lead := findLead(holder.tree, statement)
			switch {
			case current.IsListed(): // a value written alone is only the lead of the field after it

			case holder.tree.Unquoted(current.Key) == lang.GUITemplateCalls:
				if !current.IsBlock() && !r.listTemplate(field, depth, yield) {
					return
				}

			case lead == lang.GUIBlockKey:
				for inner, replacement := range r.listFields(r.findReplacement(field), depth+1) {
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

// listTemplate yields the fields of the template a using statement names,
// and returns false if the loop stops early. A template that no file defines
// is marked missing.
func (r *resolver) listTemplate(using clause, depth int, yield func(string, clause) bool) bool {
	label := using.tree.Unquoted(using.tree.Statements[using.statement].Value)
	template, ok := r.findCalled(using, lang.GUITemplateCalls, label)
	if !ok {
		r.gathered.missing = append(r.gathered.missing, r.makeMissing(lang.GUITemplateCalls, label, using))

		return true
	}

	for lead, field := range r.listFields(template, depth+1) {
		if !yield(lead, field) {
			return false
		}
	}

	return true
}

// findLead returns the lead of a statement in lower case: block or
// blockoverride for a named block, else the value written alone just before
// it, else "".
func findLead(tree *jomini.Tree, statement int32) string {
	current := tree.Statements[statement]
	key := strings.ToLower(tree.Unquoted(current.Key))
	if tree.Unquoted(current.Tag) != "" && (key == lang.GUIBlockKey || key == lang.GUIBlockCalls) {
		return key
	}

	lead, ok := tree.FindLead(statement)
	if !ok {
		return ""
	}

	return strings.ToLower(tree.Text(tree.Statements[lead].Value))
}

// itemKey is the key of a data model's block for the widgets of each item.
const itemKey = "item"

// stateBlockKey is the key of a widget's block for one of its states.
const stateBlockKey = "state"

// overlayKey is the key of a block that lays a texture over its widget's.
const overlayKey = "modify_texture"

// resolveBlock returns the properties a clause gives its widget, and gathers
// its children, its states and its overlays.
func (r *resolver) resolveBlock(holder clause, depth int) []Property {
	var properties []Property
	for lead, field := range r.listFields(holder, depth) {
		current := field.tree.Statements[field.statement]
		key := cmp.Or(field.tree.Unquoted(current.Key), field.tree.Unquoted(current.Tag))
		slot, isSlot := widgetSlots[strings.ToLower(key)]
		switch {
		case lead == lang.GUIBlockCalls: // a blockoverride gives no property; listOverrides took it

		case current.IsBlock() && (key == itemKey || slot.Labels):
			r.gatherInner(field, slot, depth)

		case current.IsBlock() && isSlot:
			if !slot.Floats {
				r.gathered.children = append(r.gathered.children, child{clause: field, slot: slot})
			}

		case current.IsBlock() && r.isWidget(field, key):
			r.gathered.children = append(r.gathered.children, child{clause: field})

		case current.IsBlock() && key == stateBlockKey:
			r.gathered.states = append(r.gathered.states, field)

		default:
			if current.IsBlock() && key == overlayKey {
				r.gathered.overlays = append(r.gathered.overlays, r.makeOverlay(field, depth))
			}

			properties = append(properties, r.makeProperty(field))
		}
	}

	return properties
}

// findReplacement returns the block the widget replaces a named block with,
// or the named block itself where nothing replaces it or the same type or
// template wrote both.
func (r *resolver) findReplacement(field clause) clause {
	override, ok := r.gathered.overrides[field.tree.Unquoted(field.tree.Statements[field.statement].Tag)]
	if ok && override.writer != field.writer {
		return override
	}

	return field
}

// gatherInner gathers the widgets inside a label or an item as children that
// fill the given slot. A label replaces the one before it.
func (r *resolver) gatherInner(field clause, slot widgetSlot, depth int) {
	if slot.Labels {
		r.gathered.children = slices.DeleteFunc(r.gathered.children, func(held child) bool {
			return held.slot.Labels
		})
	}

	for _, inner := range r.listFields(field, depth) {
		current := inner.tree.Statements[inner.statement]
		if current.IsBlock() && r.isWidget(inner, inner.tree.Unquoted(current.Key)) {
			r.gathered.children = append(r.gathered.children, child{clause: inner, slot: slot, item: !slot.Labels})
		}
	}
}

// isWidget reports whether a key written in a widget's block starts another
// widget.
func (r *resolver) isWidget(owner clause, key string) bool {
	_, isDefined := r.findCalled(owner, lang.GUITagCalls, key)

	return isDefined || r.isBuiltin(owner, key)
}

// resolveAlone returns the properties a template drawn on its own gives its
// widget.
func (r *resolver) resolveAlone(own clause) []Property {
	if own.statement != absentStatement || own.writer == "" {
		return nil
	}

	template, ok := r.findCalled(own, lang.GUITemplateCalls, own.writer)
	if !ok {
		return []Property{r.makeMissing(lang.GUITemplateCalls, own.writer, own)}
	}

	return r.resolveBlock(template, 1)
}

// resolveChildren makes the widgets of the blocks a widget gathered. A label
// takes over the texts of the widget that holds it. A block that holds itself
// is skipped.
func (r *resolver) resolveChildren(holder int32) {
	children := r.gathered.children
	slices.SortStableFunc(children, compareChildren)
	for _, held := range children {
		if slices.ContainsFunc(r.ancestors, func(ancestor clause) bool {
			return ancestor.path == held.path && ancestor.statement == held.statement
		}) {
			continue
		}

		id := int32(len(r.widgets))
		r.buildWidget(held, holder, "")
		if held.slot.Labels {
			for key, text := range r.widgets[holder].Texts {
				r.widgets[id].Texts[key] = text
				delete(r.widgets[holder].Texts, key)
			}
		}
	}
}

// compareChildren orders a label before a widget's other children, and an
// item of a data model after them.
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

// holderWindows holds, by game, the window that a window of a type is laid
// out in, by the type.
var holderWindows = map[string]map[string]string{
	game.EU5: {"lateralview": "lateralview_left_layer"},
}

// findHolders returns the game window that a window of the given type is laid
// out in, as its root widget alone, or none.
func (r *resolver) findHolders(key string, root clause) []Widget {
	_, chain := r.listTypes(key, root)
	for _, link := range slices.Backward(chain) {
		name, ok := holderWindows[r.ws.Game.ID][link.writer]
		reference := lang.Reference{Database: lang.GUIWidgetDatabase, Name: name}
		winner, isDefined := r.mods.FindKept(r.ws.Schema, reference)
		if !ok || !isDefined {
			continue
		}

		// the copy starts with no widgets, and shares the tables and the
		// library
		other := *r
		other.ancestors, other.gathered, other.widgets = nil, gathering{}, nil
		if holder, err := other.resolveWindow(r.mods.Files[winner.Site.File].Path, name); err == nil {
			return []Widget{holder.Widgets[0]}
		}
	}

	return []Widget{}
}

// findKeptClause returns the definition the game keeps of a name, or false if
// it keeps none.
func findKeptClause(ws *workspace.Session, mods *index.Index, reference lang.Reference) (clause, bool) {
	kept, ok := mods.FindKept(ws.Schema, resolveReference(mods, reference))
	if !ok {
		return clause{}, false
	}

	return makeClause(ws, mods, kept)
}

// resolveReference gives a reference with no database the one database that
// defines its name, if exactly one does.
func resolveReference(mods *index.Index, reference lang.Reference) lang.Reference {
	if reference.Database != "" {
		return reference
	}

	if databases := mods.ListDatabases(reference.Name); len(databases) == 1 {
		reference.Database = databases[0]
	}

	return reference
}

// makeClause returns the clause of a kept definition, or false if its file
// cannot be loaded.
func makeClause(ws *workspace.Session, mods *index.Index, kept index.Winner) (clause, bool) {
	tree, statement, err := ws.FindStatement(mods, kept.Site)
	if err != nil {
		return clause{}, false
	}

	return clause{tree: tree, path: mods.File(kept.Site).Path, statement: statement}, true
}

// toRange returns where a clause is written, as the editor counts.
func toRange(field clause) jomini.Range {
	var span jomini.Span
	if field.statement != absentStatement {
		span = field.tree.Statements[field.statement].Span
	}

	return jomini.ToRange(field.tree.Src, field.tree.Lines, span)
}
