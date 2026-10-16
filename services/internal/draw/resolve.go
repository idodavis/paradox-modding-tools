// resolve.go is an entry resolved into a tree of widgets, each an element: the type chain to its built-in type, its
// templates and blocks applied in order under the overrides in force, the state it opens in, its children, and
// the widgets the game lays the entry out in.

package draw

import (
	"cmp"
	"fmt"
	"iter"
	"maps"
	"regexp"
	"slices"
	"strings"
	"sync"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/index"
	"paradox-modding-tools/services/internal/jomini"
	"paradox-modding-tools/services/internal/lsp"
	"paradox-modding-tools/services/internal/schema"
	"paradox-modding-tools/services/internal/workspace"
)

// resolver is one request's resolve: the workspace, the parses made for it, the list being built, the widget of each
// element, and what is read once a render.
type resolver struct {
	ws      *workspace.Session
	parses  workspace.Parses
	request Request
	list    *List
	widgets []*widget // by element
	element int       // the element being resolved, where a thing not found is listed; -1 for none
	textbox func() *widget
	folders func() map[string]string // the icon folders by define
	formats func() map[string]string // each format's id by its lower-cased id

	textureIndexes map[string]int // by the texture in Go's syntax
	groupIndexes   map[string]int
	definitions    map[string]statement // by "caller|database|id"; no assignment for none
	fonts          map[string]*Font     // by "id/style"; nil for none
	pictures       map[string]string    // the file a texture binding draws, by "type|key"; "" for none
	pathTextures   map[string]Texture   // a path's texture with the defaults; URL "" for none
	urls           map[string]string    // a file's URL by path
}

// widget is one resolved widget: its element, its built-in type, the statement that writes it, every statement that
// applies to it in order, the children its blocks hold, the overrides in force and the data context it reads.
type widget struct {
	block
	element     int
	builtIn     string
	at          statement
	children    []child
	overrides   map[string]statement
	dataContext string
}

// child is a statement a widget's block holds as a widget: its role, and whether it is a data model's item.
type child struct {
	statement
	role   game.WidgetRole
	isItem bool
}

// entryRow is how a database's entries are drawn: alone, as what script with the id put in ("" as written), and by
// what resolve of their own (nil a widget's).
type entryRow struct {
	isAlone bool
	script  string
	resolve func(*resolver, child)
}

// The words the resolver reads, and the deepest types, templates and blocks nest before a cycle is assumed.
const (
	yes            = "yes"
	no             = "no"
	usingKey       = "using"
	stateKey       = "state"
	sizeKey        = "size"
	windowDatabase = "gui.widget"
	maxDepth       = 40
)

// templateDatabases hold templates; widgetDatabases hold what is laid out where the install writes it.
var (
	templateDatabases = []string{"gui.template", "gui.local_template"}
	widgetDatabases   = append([]string{"gui.type", "gui.local_type"}, templateDatabases...)
)

// entryRows is how the entries of each database are drawn.
var entryRows = map[string]entryRow{
	windowDatabase:           {},
	"gui.type":               {isAlone: true, script: "%s = { }"},
	"gui.local_type":         {isAlone: true, script: "%s = { }"},
	"gui.template":           {isAlone: true, script: "widget = { using = %s }"},
	"gui.local_template":     {isAlone: true, script: "widget = { using = %s }"},
	"gui.format":             {isAlone: true, script: `textbox = { text = "#%s Sample text#!" }`},
	textIconDatabase:         {isAlone: true},
	game.CoatDatabases.Coats: {isAlone: true, resolve: resolveCoat},
}

// missingTexts is the status line's sentence for each thing not found; %s is what was asked for.
var missingTexts = map[string]string{
	"template": "Template not found: %s", "type": "Type not found: %s",
	"cycle": "Type cycle: %s", "templatecycle": "Template cycle: %s",
	"texture": "Texture not found: %s", fontKey: "Font not found: %s", "fontsize": "Font size missing: %s",
	"format": "Text format not found: %s", "texticon": "Text icon not found: %s", "loc": "Localization key not found: %s",
	"blend":   "Blend mode not drawn: %s",
	fieldType: "Field size not found in %s", "list": "Picked from a list, not drawn: %s", "parent": "Parent coat not found: %s",
	"color": "Named color not found: %s",
}

// widgetCaller is the word a widget key naming a type resolves by.
var widgetCaller = game.Shapes[game.ShapeWidget].Caller

// quotedArgument is a quoted argument of a call.
var quotedArgument = regexp.MustCompile(`'[^']*'`)

// newResolver resolves the request's entry into its tree, its name the request's id and drawn whatever its visible
// says, in the holders the game lays it out in.
func newResolver(ws *workspace.Session, request Request) (*resolver, error) {
	resolver := &resolver{
		ws: ws, parses: workspace.Parses{}, request: request, element: -1,
		list: &List{
			Holders: []Element{}, Elements: []Element{}, Textures: []Texture{}, Groups: []Group{}, Fonts: []Font{},
			Missing: []Absent{},
		},
		textureIndexes: map[string]int{}, groupIndexes: map[string]int{}, definitions: map[string]statement{},
		fonts: map[string]*Font{}, pictures: map[string]string{}, pathTextures: map[string]Texture{},
		urls: map[string]string{},
	}
	resolver.textbox = sync.OnceValue(func() *widget {
		_, chain := resolver.typeChain("textbox")
		return resolver.expanded(nil, chain...)
	})
	resolver.folders = sync.OnceValue(resolver.iconFolders)
	resolver.formats = sync.OnceValue(func() map[string]string {
		ids := map[string]string{}
		for _, id := range index.Names(ws.Indexes(), formatDatabase) {
			ids[strings.ToLower(id)] = id
		}
		return ids
	})
	entry, err := resolver.entry(request)
	if err != nil {
		return nil, err
	}
	if resolve := entryRows[resolver.request.Database].resolve; resolve != nil {
		resolve(resolver, entry)
		resolver.element = -1
		return resolver, nil
	}
	resolver.resolveWidget(nil, entry)
	resolver.list.Elements[0].IsHidden = false
	resolver.element = -1
	_, chain := resolver.typeChain(entry.assignment.Key.Text)
	for _, link := range slices.Backward(chain) {
		holder, hasHolder := game.GUIHolders[link.writer]
		definition, isDefined := resolver.definition(schema.Occurrence{Database: windowDatabase, ID: holder})
		if hasHolder && isDefined {
			resolver.list.Holders = []Element{resolver.holderElement(definition)}
			return resolver, nil
		}
	}
	if resolver.list.IsAlone {
		resolver.list.Holders = append(resolver.list.Holders, resolver.placeChain(request.Database, request.ID, 0)...)
	}
	return resolver, nil
}

// entry is the statement the request names, as its database's row draws it: a window as written, a type or template
// as the script that names it, a text icon as its iconsize block drawn as an icon.
func (resolver *resolver) entry(request Request) (child, error) {
	if _, _, isInWorkspace := resolver.ws.Locate(request.Path); !isInWorkspace {
		return child{}, fmt.Errorf("%s is not in the open workspace: it is under neither its %s install nor its mods",
			request.Path, resolver.ws.Game.Name)
	}
	parsed, _, err := resolver.ws.Parsed(resolver.parses, request.Path)
	if err != nil {
		return child{}, err
	}
	for _, scanned := range resolver.ws.Indexes() {
		for _, def := range scanned.InFile(request.Path) {
			occurrence := def.Occurrence()
			row, hasRow := entryRows[occurrence.Database]
			isEntry := occurrence.Role == schema.Defines && occurrence.ID == request.ID &&
				(request.Database == "" || occurrence.Database == request.Database)
			definition, isAssignment := parsed.AssignmentAt(int(occurrence.Line), int(occurrence.Col)).(*jomini.Assignment)
			if !hasRow || !isEntry || !isAssignment {
				continue
			}
			resolver.list.IsAlone, resolver.request.Database = row.isAlone, occurrence.Database
			assignment := definition
			switch {
			case !row.isAlone, row.resolve != nil:
			case row.script != "":
				script := jomini.Parse(fmt.Sprintf(row.script, request.ID))
				assignment = jomini.Fields(&jomini.Block{Statements: script.Root.Statements})[0]
				assignment.Range = definition.Range
			default:
				assignment = &jomini.Assignment{Key: jomini.Scalar{Text: "icon"}, Value: &jomini.Block{}, Range: definition.Range}
				for _, field := range jomini.Fields(jomini.ChildBlock(definition)) {
					if strings.EqualFold(field.Key.Text, "iconsize") && jomini.ScalarOf(jomini.BlockOf(field.Value), "texture") != nil {
						assignment.Value = field.Value
					}
				}
				if jomini.ScalarOf(jomini.BlockOf(assignment.Value), "texture") == nil {
					resolver.addMissing("texticon", request.ID)
				}
			}
			return child{statement: statement{assignment: assignment, parsed: parsed, path: request.Path}}, nil
		}
	}
	return child{}, fmt.Errorf("%s holds no window, type, template, text icon, format or coat of arms %q", request.Path,
		request.ID)
}

// resolveWidget resolves a child under a parent (nil for the entry): its element listed before its descendants, its
// type and chain, the overrides scanned, templates and blocks applied, the opening state, its data context, its
// children, then its element described.
func (resolver *resolver) resolveWidget(parent *widget, child child) *widget {
	widget := &widget{element: len(resolver.list.Elements), at: child.statement, overrides: map[string]statement{}}
	element := Element{Parent: -1, Key: child.assignment.Key.Text, Role: child.role, IsItem: child.isItem}
	if parent != nil {
		element.Parent = parent.element
		maps.Copy(widget.overrides, parent.overrides)
		widget.dataContext = parent.dataContext
	}
	resolver.list.Elements = append(resolver.list.Elements, element)
	resolver.widgets = append(resolver.widgets, widget)
	resolver.element = widget.element
	slot := game.WidgetSlots[strings.ToLower(element.Key)]
	builtIn, chain := resolver.typeChain(cmp.Or(slot.Type, element.Key))
	widget.builtIn = builtIn
	chain = append(chain, child.statement)
	for _, link := range chain {
		resolver.scanOverrides(widget, link)
	}
	for _, link := range chain {
		resolver.expand(widget, link, 0)
	}
	resolver.openingState(widget)
	if text, has := widget.scalar(game.FileTypes[".gui"].ContextKey); has && isBinding(text) {
		widget.dataContext = text
	}
	id, _ := widget.scalar("name")
	resolver.identify(widget, id)
	resolver.resolveChildren(widget)
	resolver.describe(widget)
	return widget
}

// identify fills how a widget's element is named and where it is written, before its children are, which name it in
// their paths; the root is named for the request.
func (resolver *resolver) identify(widget *widget, id string) {
	element := &resolver.list.Elements[widget.element]
	element.Name = id
	element.Label = element.Key
	if element.Parent < 0 {
		element.Name = resolver.request.ID
	}
	if element.Name != "" && element.Name != element.Key {
		element.Label += " " + element.Name
	}
	element.Path, element.Ancestors = element.Label, []int{}
	if element.Parent >= 0 {
		parent := resolver.list.Elements[element.Parent]
		element.Path = parent.Path + " › " + element.Label
		element.Ancestors = append(slices.Clone(parent.Ancestors), element.Parent)
	}
	element.Location = lsp.Locate(resolver.ws, widget.at.path, widget.at.span())
}

// describe fills a widget's element once its children are resolved: what it is drawn by, whether it is hidden or
// shown on a condition, its text (its label's where it has one) and its textures.
func (resolver *resolver) describe(widget *widget) {
	resolver.element = widget.element
	element := &resolver.list.Elements[widget.element]
	resolver.drawnBy(widget, element)
	element.IsHidden = slices.ContainsFunc(game.GUIHiddenKeys, func(key string) bool {
		value, _ := widget.scalar(key)
		return isNo(value)
	})
	if element.VisibleBinding = binding(&widget.block, "visible"); element.VisibleBinding != "" {
		condition := resolver.condition(element.VisibleBinding)
		element.Condition = &condition
	}
	if !slices.ContainsFunc(widget.children, func(child child) bool { return child.role == game.RoleLabel }) {
		element.Text = resolver.text(widget)
	}
	element.Textures = resolver.layers(widget)
	if len(element.Textures) == 0 {
		element.TextureBinding = cmp.Or(binding(&widget.block, "texture"), binding(&widget.block, "portrait_texture"))
	}
}

// drawnBy fills what an element is drawn by: its type's row for the workspace's game, and every property as
// written, a call the preview answers replaced by the entry's value.
func (resolver *resolver) drawnBy(widget *widget, element *Element) {
	row := game.GUITypes[widget.builtIn]
	scissor, _ := widget.scalar("scissor")
	ignore, _ := widget.scalar("ignoreinvisible")
	element.Type, element.Holds = widget.builtIn, cmp.Or(row.Holds, game.HoldsCell)
	element.Clips, element.Fills, element.CentersText = row.Clips || isYes(scissor), row.Fills, row.CentersText
	element.DropsHidden = !isNo(ignore) &&
		(slices.Contains(row.DropsHidden, resolver.ws.Game.ID) || isYes(ignore) && row.Holds != game.HoldsGrid)
	element.Properties = resolver.propertiesOf(widget.statements)
}

// propertiesOf is every property statements write, by lower-cased key, the last written, its words as written, a
// call the preview answers replaced by the entry's value.
func (resolver *resolver) propertiesOf(statements []statement) map[string]string {
	properties := map[string]string{}
	for _, statement := range statements {
		words := statement.words()
		if len(words) == 0 {
			continue
		}
		text := strings.Join(words, " ")
		if define, isAnswered := resolver.answered(text); isAnswered {
			text = strings.TrimSuffix(strings.Join(define.words(), " "), ";")
		}
		properties[strings.ToLower(statement.assignment.Key.Text)] = text
	}
	return properties
}

// condition reads a visible binding into what the widget is shown on: a boolean over its unquoted arguments, or a
// fact, its call's group (listed the first time) at its last quoted argument, else at yes.
func (resolver *resolver) condition(binding string) Condition {
	if steps := jomini.ParseCall(binding, 0); len(steps) == 1 {
		if operator, isBoolean := game.GUIBooleans[strings.TrimRight(steps[0].Name, "0123456789")]; isBoolean {
			condition := Condition{Operator: operator}
			for _, arg := range steps[0].Args {
				if !arg.Quoted {
					condition.Operands = append(condition.Operands, resolver.condition(arg.Text))
				}
			}
			return condition
		}
	}
	call, value := strings.Join(strings.Fields(strings.Trim(binding, "[]")), ""), yes
	if spans := quotedArgument.FindAllStringIndex(call, -1); len(spans) > 0 {
		last := spans[len(spans)-1]
		value, call = call[last[0]:last[1]], call[:last[0]]+"'…'"+call[last[1]:]
	}
	i, isKnown := resolver.groupIndexes[call]
	if !isKnown {
		i = len(resolver.list.Groups)
		resolver.groupIndexes[call] = i
		resolver.list.Groups = append(resolver.list.Groups, Group{Call: call})
	}
	if group := &resolver.list.Groups[i]; !slices.Contains(group.Values, value) {
		group.Values = append(group.Values, value)
	}
	return Condition{Group: i, Value: value}
}

// resolveChildren resolves a widget's children, the label first and a data model's item last; one written by the
// widget or an ancestor is left out, since it would nest without end; a label draws its holder's text in its own box.
func (resolver *resolver) resolveChildren(holder *widget) {
	rank := func(child child) int {
		switch {
		case child.role == game.RoleLabel:
			return 0
		case child.isItem:
			return 2
		}
		return 1
	}
	slices.SortStableFunc(holder.children, func(first, second child) int { return cmp.Compare(rank(first), rank(second)) })
	for _, child := range holder.children {
		isAbove := false
		for element := holder.element; element >= 0; element = resolver.list.Elements[element].Parent {
			ancestor := resolver.widgets[element].at
			isAbove = isAbove || ancestor.path == child.path && ancestor.assignment.Range == child.assignment.Range
		}
		if isAbove {
			continue
		}
		held := resolver.resolveWidget(holder, child)
		if child.role == game.RoleLabel {
			resolver.element = held.element
			label := &widget{element: held.element, builtIn: held.builtIn}
			label.add(held.statements...)
			label.add(textStatements(holder)...)
			resolver.list.Elements[held.element].Text = resolver.text(label)
		}
	}
}

// typeChain follows a key through the types to its built-in type: each type's statement, base first; a key no type
// defines is a built-in, else listed and read as a plain widget; one nested past maxDepth is a cycle.
func (resolver *resolver) typeChain(key string) (builtIn string, chain []statement) {
	builtIn = key
	for range maxDepth {
		definition, isType := resolver.definition(schema.Occurrence{Caller: widgetCaller, ID: builtIn})
		if !isType {
			if !resolver.ws.Install.Schema.WidgetTypes[builtIn] {
				resolver.addMissing("type", builtIn)
			}
			slices.Reverse(chain)
			return builtIn, chain
		}
		chain = append(chain, definition)
		base := "widget"
		if taggedBlock, isTaggedBlock := definition.assignment.Value.(*jomini.TaggedBlock); isTaggedBlock {
			base = taggedBlock.Tag.Text
		}
		if base == builtIn {
			slices.Reverse(chain)
			return builtIn, chain
		}
		builtIn = base
	}
	resolver.addMissing("cycle", key)
	return key, nil
}

// definition is the statement a reference resolves to, written by its id, read once a render; false for none.
func (resolver *resolver) definition(ref schema.Occurrence) (statement, bool) {
	key := ref.Caller + "|" + ref.Database + "|" + ref.ID
	definition, isKnown := resolver.definitions[key]
	if !isKnown {
		if winner, isDefined := resolver.winner(ref); isDefined {
			parsed, assignment := resolver.ws.Statement(resolver.parses, winner)
			if assignment, isAssignment := assignment.(*jomini.Assignment); isAssignment {
				definition = statement{assignment: assignment, parsed: parsed, path: winner.File().Path, writer: ref.ID}
			}
		}
		resolver.definitions[key] = definition
	}
	return definition, definition.assignment != nil
}

// winner is the definition a reference resolves to: one of this file first (a local type or template), else the
// load order's winner; false for none.
func (resolver *resolver) winner(ref schema.Occurrence) (index.At, bool) {
	ref.Role = schema.Refers
	defs, _ := index.Resolve(resolver.ws.Install.Schema, resolver.ws.Indexes(), ref, resolver.request.Path)
	if local := slices.IndexFunc(defs, func(def index.At) bool { return def.Occurrence().Local }); local >= 0 {
		return defs[local], true
	}
	winner, _, _, _ := resolver.ws.Winner(defs)
	return winner, winner != index.At{}
}

// templated yields a statement's fields with the word before each, a using followed by its template's fields in
// place; a template nothing defines is listed, one nested past maxDepth is a cycle.
func (resolver *resolver) templated(holder statement, depth int) iter.Seq2[string, statement] {
	return func(yield func(string, statement) bool) {
		if depth > maxDepth {
			resolver.addMissing("templatecycle", holder.writer)
			return
		}
		for previous, field := range fields(holder) {
			if !strings.EqualFold(field.assignment.Key.Text, usingKey) {
				if !yield(previous, field) {
					return
				}
				continue
			}
			scalar, isScalar := field.assignment.Value.(*jomini.Scalar)
			if !isScalar {
				continue
			}
			template, isTemplate := resolver.definition(schema.Occurrence{Caller: usingKey, ID: scalar.Text})
			if !isTemplate {
				resolver.addMissing("template", scalar.Text)
				continue
			}
			if !yield(previous, field) {
				return
			}
			for previous, field := range resolver.templated(template, depth+1) {
				if !yield(previous, field) {
					return
				}
			}
		}
	}
}

// scanOverrides records the blockoverrides a statement and its templates write, a later one over an earlier.
func (resolver *resolver) scanOverrides(widget *widget, statement statement) {
	for previous, field := range resolver.templated(statement, 0) {
		if previous == "blockoverride" {
			widget.overrides[field.assignment.Key.Text] = field
		}
	}
}

// expand applies a statement's block to the widget, its templates in place.
func (resolver *resolver) expand(widget *widget, statement statement, depth int) {
	for previous, field := range resolver.templated(statement, depth) {
		resolver.apply(widget, field, previous, depth)
	}
}

// apply does what one statement does to a widget: a block puts its statements in, or its override's where another
// writer wrote one; a blockoverride and a using are their own; an item's or a label's widgets are held children;
// a slot's block is a child of its role, an attachment left out; a widget key is a child; anything else applies.
func (resolver *resolver) apply(widget *widget, statement statement, previous string, depth int) {
	key := strings.ToLower(statement.assignment.Key.Text)
	slot, isSlot := game.WidgetSlots[key]
	hasBlock := jomini.ChildBlock(statement.assignment) != nil
	switch {
	case previous == "block":
		if override, has := widget.overrides[statement.assignment.Key.Text]; has && override.writer != statement.writer {
			statement = override
		}
		resolver.expand(widget, statement, depth+1)
	case previous == "blockoverride", key == usingKey:
	case hasBlock && (key == "item" || slot.Role == game.RoleLabel):
		if slot.Role == game.RoleLabel {
			widget.children = slices.DeleteFunc(widget.children, func(child child) bool { return child.role == game.RoleLabel })
		}
		for _, field := range fields(statement) {
			if jomini.ChildBlock(field.assignment) != nil && resolver.isWidget(field.assignment.Key.Text) {
				widget.children = append(widget.children, child{statement: field, role: slot.Role, isItem: slot.Role == ""})
			}
		}
	case hasBlock && isSlot:
		if slot.Role != game.RoleAttachment {
			widget.children = append(widget.children, child{statement: statement, role: slot.Role})
		}
	case hasBlock && resolver.isWidget(statement.assignment.Key.Text):
		widget.children = append(widget.children, child{statement: statement})
	default:
		widget.add(statement)
	}
}

// isWidget reports whether a block-valued key names a widget: a type, or a built-in of the install's.
func (resolver *resolver) isWidget(key string) bool {
	if _, isType := resolver.definition(schema.Occurrence{Caller: widgetCaller, ID: key}); isType {
		return true
	}
	return resolver.ws.Install.Schema.WidgetTypes[key]
}

// expanded is a widget that is no element, its statements applied under a holder's overrides and data context.
func (resolver *resolver) expanded(holder *widget, statements ...statement) *widget {
	widget := &widget{element: -1, overrides: map[string]statement{}}
	if holder != nil {
		widget.element, widget.overrides, widget.dataContext = holder.element, holder.overrides, holder.dataContext
	}
	for _, statement := range statements {
		resolver.expand(widget, statement, 0)
	}
	return widget
}

// openingState applies the end values of the states GUIOpeningStates names as the widget's own.
func (resolver *resolver) openingState(widget *widget) {
	rules := game.GUIOpeningStates
	var states []statement
	for _, statement := range widget.statements {
		if !strings.EqualFold(statement.assignment.Key.Text, stateKey) {
			continue
		}
		state := blockOf(statement)
		name, _ := state.scalar("name")
		isOpening := slices.Contains(rules.Names, name)
		for key, value := range rules.Triggers {
			written, _ := state.scalar(key)
			isOpening = isOpening || strings.EqualFold(written, value)
		}
		if isOpening {
			states = append(states, statement)
		}
	}
	for _, field := range resolver.expanded(widget, states...).statements {
		_, isScalar := field.assignment.Value.(*jomini.Scalar)
		isOwn := slices.ContainsFunc(rules.OwnKeys, func(key string) bool { return strings.EqualFold(key, field.assignment.Key.Text) })
		if isScalar && !isOwn {
			widget.add(field)
		}
	}
}

// placeChain is the widgets around the one place the install writes an entry of a type or template, outermost
// first: its window and the widgets down to the place, or the chain of the one place a template is used; none where
// it is written nowhere, in several places, or in a block or blockoverride.
func (resolver *resolver) placeChain(database, id string, depth int) []Element {
	refs := index.RefsTo(resolver.ws.Install.Schema, resolver.ws.Indexes(), database, id)
	if len(refs) != 1 || depth > maxDepth || !slices.Contains(widgetDatabases, database) {
		return nil
	}
	entry, isInEntry := refs[0].Entry()
	if !isInEntry {
		return nil
	}
	parsed, assignment := resolver.ws.Statement(resolver.parses, entry)
	top, isAssignment := assignment.(*jomini.Assignment)
	if !isAssignment {
		return nil
	}
	holder := statement{assignment: top, parsed: parsed, path: entry.File().Path}
	inner, isPlain := resolver.offsetHolders(holder, refs[0].Occurrence().Range.Start)
	switch occurrence := entry.Occurrence(); {
	case !isPlain:
	case occurrence.Database == windowDatabase:
		return append([]Element{resolver.holderElement(holder)}, inner...)
	case slices.Contains(templateDatabases, occurrence.Database):
		if outer := resolver.placeChain(occurrence.Database, occurrence.ID, depth+1); outer != nil {
			return append(outer, inner...)
		}
	}
	return nil
}

// offsetHolders is the widgets inside a holder's block whose blocks hold a byte offset, outermost first, the one at
// the offset left out; false where a block or blockoverride holds it.
func (resolver *resolver) offsetHolders(holder statement, offset int) ([]Element, bool) {
	for previous, field := range fields(holder) {
		span := field.assignment.Range
		if span.Start >= offset || offset >= span.End || jomini.ChildBlock(field.assignment) == nil {
			continue
		}
		if previous != "" {
			return nil, false
		}
		inner, isPlain := resolver.offsetHolders(field, offset)
		return append([]Element{resolver.holderElement(field)}, inner...), isPlain
	}
	return nil, true
}

// holderElement is a widget the entry is laid out in, as an element of its type row and properties alone.
func (resolver *resolver) holderElement(holder statement) Element {
	key := holder.assignment.Key.Text
	builtIn, chain := resolver.typeChain(cmp.Or(game.WidgetSlots[strings.ToLower(key)].Type, key))
	widget := resolver.expanded(nil, append(chain, holder)...)
	widget.builtIn = builtIn
	element := Element{Parent: -1, Key: key, Ancestors: []int{}, Textures: []int{}}
	resolver.drawnBy(widget, &element)
	element.Name, _ = widget.scalar("name")
	element.Label = strings.TrimSpace(key + " " + element.Name)
	element.Path = element.Label
	return element
}

// addMissing lists a thing not found at the element being resolved, once.
func (resolver *resolver) addMissing(cause, text string) {
	absent := Absent{Text: fmt.Sprintf(missingTexts[cause], text), Element: resolver.element}
	if !slices.Contains(resolver.list.Missing, absent) {
		resolver.list.Missing = append(resolver.list.Missing, absent)
	}
}

// answered is the entry a value that is one call of GUIAnswered names, its last argument the id; false otherwise.
func (resolver *resolver) answered(text string) (statement, bool) {
	steps := jomini.ParseCall(text, 0)
	if !isBinding(text) || len(steps) != 1 || len(steps[0].Args) == 0 {
		return statement{}, false
	}
	database, isAnswered := game.GUIAnswered[steps[0].Name]
	if !isAnswered {
		return statement{}, false
	}
	args := steps[0].Args
	return resolver.definition(schema.Occurrence{Database: database, ID: args[len(args)-1].Text})
}

// binding is the binding a block writes a key as; "" where it writes none or a literal.
func binding(block *block, key string) string {
	if text, _ := block.scalar(key); isBinding(text) {
		return text
	}
	return ""
}

// isBinding reports whether the whole of a value is one [call].
func isBinding(text string) bool {
	text = strings.TrimSpace(text)
	calls := jomini.Calls(text, 0)
	return len(calls) == 1 && calls[0].Start == 0 && calls[0].End == len(text) && strings.HasSuffix(text, "]")
}

// isNo reports whether a word is the game's no.
func isNo(text string) bool {
	return strings.EqualFold(text, no) || strings.EqualFold(text, "false")
}

// isYes reports whether a word is the game's yes.
func isYes(text string) bool {
	return strings.EqualFold(text, yes) || strings.EqualFold(text, "true")
}
