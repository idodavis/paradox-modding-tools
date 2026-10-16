// gui.go is a GUI entry resolved into a list of widgets: the type chain to each one's built-in type, its templates
// and blocks applied in order under the overrides in force, the state it opens in, its children, and the widgets
// the game lays the entry out in.

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
	"paradox-modding-tools/services/internal/schema"
)

// List is one render of a GUI entry: the widgets the game lays it out in, its widgets in pre-order, the textures they
// draw and the fonts they use, each listed once, the calls they are shown on, and what nothing found.
type List struct {
	IsAlone  bool      `json:"isAlone"` // a type, template, text icon or format drawn by itself, not a window
	Holders  []Widget  `json:"holders"` // outermost first, each of its type row and properties alone
	Elements []Widget  `json:"elements"`
	Textures []Texture `json:"textures"`
	Groups   []Group   `json:"groups"`
	Fonts    []Font    `json:"fonts"`
	Missing  []Absent  `json:"missing"`
}

// Widget is one widget of the entry: the element it is, how its type draws, and what it is drawn by: its properties
// as written, its textures, its text and what it is shown on.
type Widget struct {
	Element

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

// Texture is what a widget paints from a file: its size in the image's pixels over its density, its border in the
// screen's, its frame, tiling, mirror, fit, tint and blend, and the texture that masks it.
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

// Text is a widget's text as runs in a font at a size.
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

// guiResolver is one GUI entry's resolve: the shared resolve, the list being built, the widget of each element, and
// what is read once a render.
type guiResolver struct {
	*resolver
	list    *List
	widgets []*widget // by element
	textbox func() *widget
	folders func() map[string]string // the icon folders by define
	formats func() map[string]string // each format's id by its lower-cased id

	textureIndexes map[Texture]int
	groupIndexes   map[string]int
	fonts          map[string]*Font   // by "id/style"; nil for none
	pictures       map[string]string  // the file a texture binding draws, by "type|key"; "" for none
	pathTextures   map[string]Texture // a path's texture with the defaults; URL "" for none
}

// widget is one resolved widget: its element, its built-in type, the statement that writes it, every statement that
// applies to it in order, the children its blocks hold, the overrides in force and the data context it reads.
type widget struct {
	properties
	element     int
	builtIn     string
	block       statement
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

// entryRow is how a database's entries are drawn: alone, and as what script with the id put in ("" as written).
type entryRow struct {
	isAlone bool
	script  string
}

// The words the resolver reads.
const (
	yes            = "yes"
	no             = "no"
	usingKey       = "using"
	stateKey       = "state"
	sizeKey        = "size"
	windowDatabase = "gui.widget"
)

// templateDatabases hold templates; widgetDatabases hold what is laid out where the install writes it.
var (
	templateDatabases = []string{"gui.template", "gui.local_template"}
	widgetDatabases   = append([]string{"gui.type", "gui.local_type"}, templateDatabases...)
)

// entryRows is how the entries of each database are drawn.
var entryRows = map[string]entryRow{
	windowDatabase:       {},
	"gui.type":           {isAlone: true, script: "%s = { }"},
	"gui.local_type":     {isAlone: true, script: "%s = { }"},
	"gui.template":       {isAlone: true, script: "widget = { using = %s }"},
	"gui.local_template": {isAlone: true, script: "widget = { using = %s }"},
	"gui.format":         {isAlone: true, script: `textbox = { text = "#%s Sample text#!" }`},
	textIconDatabase:     {isAlone: true},
}

// widgetCaller is the word a widget key naming a type resolves by.
var widgetCaller = game.Shapes[game.ShapeWidget].Caller

// quotedArgument is a quoted argument of a call.
var quotedArgument = regexp.MustCompile(`'[^']*'`)

// newGUIResolver resolves the request's GUI entry on a resolve's core into its tree, its name the request's id and
// drawn whatever its visible says, in the holders the game lays it out in.
func newGUIResolver(core *resolver) (*guiResolver, error) {
	resolver := &guiResolver{
		resolver: core,
		list: &List{
			Holders: []Widget{}, Elements: []Widget{}, Textures: []Texture{}, Groups: []Group{}, Fonts: []Font{},
			Missing: []Absent{},
		},
		textureIndexes: map[Texture]int{}, groupIndexes: map[string]int{}, fonts: map[string]*Font{},
		pictures: map[string]string{}, pathTextures: map[string]Texture{},
	}
	resolver.textbox = sync.OnceValue(func() *widget {
		_, chain := resolver.typeChain("textbox")
		return resolver.expanded(nil, chain...)
	})
	resolver.folders = sync.OnceValue(resolver.iconFolders)
	resolver.formats = sync.OnceValue(func() map[string]string {
		ids := map[string]string{}
		for _, id := range index.Names(resolver.ws.Indexes(), formatDatabase) {
			ids[strings.ToLower(id)] = id
		}
		return ids
	})
	definition, err := resolver.entry("window, type, template, text icon or format", slices.Collect(maps.Keys(entryRows)))
	if err != nil {
		return nil, err
	}
	row := entryRows[resolver.request.Database]
	resolver.list.IsAlone = row.isAlone
	entry := resolver.entryWidget(definition, row)
	resolver.resolveWidget(nil, entry)
	resolver.list.Elements[0].IsHidden = false
	resolver.element = -1
	resolver.list.Holders = append(resolver.list.Holders, resolver.holders(entry)...)
	resolver.list.Missing = resolver.missing
	return resolver, nil
}

// holders are the widgets the game lays an entry out in, outermost first: the window GUIHolders names for a type of
// its chain, else, for an entry drawn alone, the widgets around the one place the install writes it.
func (resolver *guiResolver) holders(entry child) []Widget {
	_, chain := resolver.typeChain(entry.assignment.Key.Text)
	for _, link := range slices.Backward(chain) {
		holder, hasHolder := game.GUIHolders[link.writer]
		definition, isDefined := resolver.definition(schema.Occurrence{Database: windowDatabase, ID: holder})
		if hasHolder && isDefined {
			return []Widget{resolver.holderWidget(definition)}
		}
	}
	if !resolver.list.IsAlone {
		return nil
	}
	return resolver.placeChain(resolver.request.Database, resolver.request.ID, 0)
}

// entryWidget is the widget an entry is drawn as, by its database's row: a window as written, a type or template as
// the script that names it, a text icon as its iconsize block drawn as an icon.
func (resolver *guiResolver) entryWidget(definition statement, row entryRow) child {
	assignment := definition.assignment
	switch {
	case !row.isAlone:
	case row.script != "":
		script := jomini.Parse(fmt.Sprintf(row.script, resolver.request.ID))
		assignment = jomini.Fields(&jomini.Block{Statements: script.Root.Statements})[0]
		assignment.Range = definition.assignment.Range
	default:
		assignment = &jomini.Assignment{Key: jomini.Scalar{Text: "icon"}, Value: &jomini.Block{}, Range: definition.assignment.Range}
		for _, field := range jomini.Fields(jomini.ChildBlock(definition.assignment)) {
			if strings.EqualFold(field.Key.Text, "iconsize") && jomini.ScalarOf(jomini.BlockOf(field.Value), "texture") != nil {
				assignment.Value = field.Value
			}
		}
		if jomini.ScalarOf(jomini.BlockOf(assignment.Value), "texture") == nil {
			resolver.addMissing("texticon", resolver.request.ID)
		}
	}
	definition.assignment = assignment
	return child{statement: definition}
}

// resolveWidget resolves a child under a parent (nil for the entry): its element listed before its descendants, its
// type and chain, the overrides scanned, templates and blocks applied, the opening state, its data context, its
// children, then its element described.
func (resolver *guiResolver) resolveWidget(parent *widget, child child) *widget {
	widget := &widget{element: len(resolver.list.Elements), block: child.statement, overrides: map[string]statement{}}
	element := Widget{
		Element: Element{Parent: -1, Key: child.assignment.Key.Text}, Role: child.role, IsItem: child.isItem,
	}
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
	var holder *Element
	if parent != nil {
		holder = &resolver.list.Elements[parent.element].Element
	}
	resolver.identify(&resolver.list.Elements[widget.element].Element, holder, widget.block, id)
	resolver.resolveChildren(widget)
	resolver.describe(widget)
	return widget
}

// describe fills a widget's element once its children are resolved: what it is drawn by, whether it is hidden or
// shown on a condition, its text (its label's where it has one) and its textures.
func (resolver *guiResolver) describe(widget *widget) {
	resolver.element = widget.element
	element := &resolver.list.Elements[widget.element]
	resolver.drawnBy(widget, element)
	element.IsHidden = slices.ContainsFunc(game.GUIHiddenKeys, func(key string) bool {
		value, _ := widget.scalar(key)
		return isNo(value)
	})
	if element.VisibleBinding = binding(&widget.properties, "visible"); element.VisibleBinding != "" {
		condition := resolver.condition(element.VisibleBinding)
		element.Condition = &condition
	}
	if !slices.ContainsFunc(widget.children, func(child child) bool { return child.role == game.RoleLabel }) {
		element.Text = resolver.text(widget)
	}
	element.Textures = resolver.layers(widget)
	if len(element.Textures) == 0 {
		element.TextureBinding = cmp.Or(
			binding(&widget.properties, "texture"), binding(&widget.properties, "portrait_texture"),
		)
	}
}

// drawnBy fills what a widget is drawn by: its type's row for the workspace's game, and every property as written, a
// call the preview answers replaced by the entry's value.
func (resolver *guiResolver) drawnBy(widget *widget, element *Widget) {
	row := game.GUITypes[widget.builtIn]
	scissor, _ := widget.scalar("scissor")
	ignore, _ := widget.scalar("ignoreinvisible")
	element.Type, element.Holds = widget.builtIn, cmp.Or(row.Holds, game.HoldsCell)
	element.Clips, element.Fills, element.CentersText = row.Clips || isYes(scissor), row.Fills, row.CentersText
	element.DropsHidden = !isNo(ignore) &&
		(slices.Contains(row.DropsHidden, resolver.ws.Game.ID) || isYes(ignore) && row.Holds != game.HoldsGrid)
	element.Properties = resolver.propertyTexts(widget.statements)
}

// condition reads a visible binding into what the widget is shown on: a boolean over its unquoted arguments, or a
// fact, its call's group (listed the first time) at its last quoted argument, else at yes.
func (resolver *guiResolver) condition(binding string) Condition {
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
	groupIndex, isKnown := resolver.groupIndexes[call]
	if !isKnown {
		groupIndex = len(resolver.list.Groups)
		resolver.groupIndexes[call] = groupIndex
		resolver.list.Groups = append(resolver.list.Groups, Group{Call: call})
	}
	if group := &resolver.list.Groups[groupIndex]; !slices.Contains(group.Values, value) {
		group.Values = append(group.Values, value)
	}
	return Condition{Group: groupIndex, Value: value}
}

// resolveChildren resolves a widget's children, the label first and a data model's item last; one written by the
// widget or an ancestor is left out, since it would nest without end; a label draws its holder's text in its own box.
func (resolver *guiResolver) resolveChildren(holder *widget) {
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
			ancestor := resolver.widgets[element].block
			isAbove = isAbove || ancestor.path == child.path && ancestor.assignment.Range == child.assignment.Range
		}
		if isAbove {
			continue
		}
		childWidget := resolver.resolveWidget(holder, child)
		if child.role == game.RoleLabel {
			resolver.element = childWidget.element
			label := &widget{element: childWidget.element, builtIn: childWidget.builtIn}
			label.add(childWidget.statements...)
			label.add(textStatements(holder)...)
			resolver.list.Elements[childWidget.element].Text = resolver.text(label)
		}
	}
}

// typeChain follows a key through the types to its built-in type: each type's statement, base first; a key no type
// defines is a built-in, else listed and read as a plain widget; one nested past maxDepth is a cycle.
func (resolver *guiResolver) typeChain(key string) (builtIn string, chain []statement) {
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

// templated yields a statement's fields with the word before each, a using followed by its template's fields in
// place; a template nothing defines is listed, one nested past maxDepth is a cycle.
func (resolver *guiResolver) templated(holder statement, depth int) iter.Seq2[string, statement] {
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
func (resolver *guiResolver) scanOverrides(widget *widget, statement statement) {
	for previous, field := range resolver.templated(statement, 0) {
		if previous == "blockoverride" {
			widget.overrides[field.assignment.Key.Text] = field
		}
	}
}

// expand applies a statement's block to the widget, its templates in place.
func (resolver *guiResolver) expand(widget *widget, statement statement, depth int) {
	for previous, field := range resolver.templated(statement, depth) {
		resolver.apply(widget, field, previous, depth)
	}
}

// apply does what one statement does to a widget: a block puts its statements in, or its override's where another
// writer wrote one; a blockoverride and a using are their own; an item's or a label's widgets are held children;
// a slot's block is a child of its role, an attachment left out; a widget key is a child; anything else applies.
func (resolver *guiResolver) apply(widget *widget, statement statement, previous string, depth int) {
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
func (resolver *guiResolver) isWidget(key string) bool {
	if _, isType := resolver.definition(schema.Occurrence{Caller: widgetCaller, ID: key}); isType {
		return true
	}
	return resolver.ws.Install.Schema.WidgetTypes[key]
}

// expanded is a widget that is no element, its statements applied under a holder's overrides and data context.
func (resolver *guiResolver) expanded(holder *widget, statements ...statement) *widget {
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
func (resolver *guiResolver) openingState(widget *widget) {
	rules := game.GUIOpeningStates
	var states []statement
	for _, statement := range widget.statements {
		if !strings.EqualFold(statement.assignment.Key.Text, stateKey) {
			continue
		}
		state := propertiesOf(statement)
		id, _ := state.scalar("name")
		isOpening := slices.Contains(rules.Names, id)
		for key, value := range rules.Triggers {
			scalar, _ := state.scalar(key)
			isOpening = isOpening || strings.EqualFold(scalar, value)
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
func (resolver *guiResolver) placeChain(database, id string, depth int) []Widget {
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
		return append([]Widget{resolver.holderWidget(holder)}, inner...)
	case slices.Contains(templateDatabases, occurrence.Database):
		if outer := resolver.placeChain(occurrence.Database, occurrence.ID, depth+1); outer != nil {
			return append(outer, inner...)
		}
	}
	return nil
}

// offsetHolders is the widgets inside a holder's block whose blocks hold a byte offset, outermost first, the one at
// the offset left out; false where a block or blockoverride holds it.
func (resolver *guiResolver) offsetHolders(holder statement, offset int) ([]Widget, bool) {
	for previous, field := range fields(holder) {
		span := field.assignment.Range
		if span.Start >= offset || offset >= span.End || jomini.ChildBlock(field.assignment) == nil {
			continue
		}
		if previous != "" {
			return nil, false
		}
		inner, isPlain := resolver.offsetHolders(field, offset)
		return append([]Widget{resolver.holderWidget(field)}, inner...), isPlain
	}
	return nil, true
}

// holderWidget is a widget the entry is laid out in, of its type row and properties alone.
func (resolver *guiResolver) holderWidget(holder statement) Widget {
	key := holder.assignment.Key.Text
	builtIn, chain := resolver.typeChain(cmp.Or(game.WidgetSlots[strings.ToLower(key)].Type, key))
	widget := resolver.expanded(nil, append(chain, holder)...)
	widget.builtIn = builtIn
	element := Widget{Element: Element{Parent: -1, Key: key, Ancestors: []int{}}, Textures: []int{}}
	resolver.drawnBy(widget, &element)
	element.Name, _ = widget.scalar("name")
	element.Label = strings.TrimSpace(key + " " + element.Name)
	element.Path = element.Label
	return element
}

// binding is the binding properties write a key as; "" where they write none or a literal.
func binding(properties *properties, key string) string {
	if text, _ := properties.scalar(key); isBinding(text) {
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
