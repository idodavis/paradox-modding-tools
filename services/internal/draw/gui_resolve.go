// gui_resolve.go is pass 1: a GUI file's entry resolved into a tree of widgets. Each widget's type chain is followed
// to its built-in kind, its templates and blocks expanded in the order they apply, the state a window opens in
// applied, and its text and textures found; every widget is listed as a part.

package draw

import (
	"fmt"
	"iter"
	"maps"
	"slices"
	"strconv"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/index"
	"paradox-modding-tools/services/internal/inventory"
	"paradox-modding-tools/services/internal/jomini"
	"paradox-modding-tools/services/internal/lsp"
	"paradox-modding-tools/services/internal/schema"
	"paradox-modding-tools/services/internal/texture"
	"paradox-modding-tools/services/internal/workspace"
)

// maxDepth is how deep types, templates and widgets nest before a cycle is assumed.
const maxDepth = 40

// defaultRows is how many rows a data model draws: its binding's count is the game's to know.
const defaultRows = 3

// widgetCaller is the word a widget key naming a type is resolved by.
var widgetCaller = game.Shapes[game.ShapeWidget].Caller

// entryHow is how each database a file's entries are drawn from is drawn: a window is its own statement, a type is
// drawn by naming it, a template by using it, a text icon as the icon it is and a format as a text in it.
var entryHow = map[string]string{
	"gui.widget": entryWidget, "gui.type": entryType, "gui.local_type": entryType,
	"gui.template": entryTemplate, "gui.local_template": entryTemplate,
	"gui.texticon": entryTextIcon, "gui.format": entryFormat,
}

// How an entry is drawn: a window is its own statement, a type is named, a template used, a text icon is an icon
// of its texture and size, and a format is a sample text written in it.
const (
	entryWidget   = "widget"
	entryType     = "type"
	entryTemplate = "template"
	entryTextIcon = "texticon"
	entryFormat   = "format"
)

// resolver is pass 1 over one request: the workspace, the parses made for it, and the list being built with what was
// not found.
type resolver struct {
	ws       *workspace.Session
	parses   workspace.Parses
	list     *List
	request  Request
	alone    bool             // the entry is a type, template, text icon or format drawn by itself, not a window
	fonts    map[string]*face // by name, style and size, the faces opened for this render
	fallback *widget          // the install's textbox type: the style a text with none of its own is drawn in
	missing  misses
	places   map[string]inventory.File // by path, where each file sits
}

// resolve runs pass 1: the entry found by name, its widget tree resolved, every part listed.
func resolve(ws *workspace.Session, request Request) (*resolver, *widget, error) {
	resolver := &resolver{
		ws: ws, parses: workspace.Parses{}, request: request, fonts: map[string]*face{},
		missing: misses{}, places: map[string]inventory.File{},
		list: &List{Elements: []Element{}, Parts: []Part{}, Fonts: []Font{}, Missing: []string{}},
	}
	resolver.fallback = resolver.textDefault()
	entry, key, err := resolver.entry(request.Path, request.Name)
	if err != nil {
		return nil, nil, err
	}
	root := resolver.widget(nil, key, entry, 0)
	resolver.list.Parts[0].Name = request.Name
	return resolver, root, nil
}

// entry is the statement the request's name resolves to in the file, and the key it is drawn by: a window is its
// own statement; a type is drawn by its name with nothing of its own, a template by a widget that uses it, a text icon
// by an icon of its texture and size, and a format by a text written in it.
func (resolver *resolver) entry(path, name string) (statement, string, error) {
	parsed, _, err := resolver.ws.Parsed(resolver.parses, path)
	if err != nil {
		return statement{}, "", err
	}
	for _, scanned := range resolver.ws.Indexes() {
		for _, def := range scanned.InFile(path) {
			occurrence := def.Occurrence()
			how, drawable := entryHow[occurrence.Database]
			assign, isAssign := parsed.AssignmentAt(int(occurrence.Line), int(occurrence.Col)).(*jomini.Assignment)
			if occurrence.Role != schema.Defines || occurrence.ID != name || !drawable || !isAssign {
				continue
			}
			if how == entryWidget {
				return statement{assign: assign, parsed: parsed, path: path}, assign.Key.Text, nil
			}
			resolver.alone = true
			drawn, key := resolver.entryAssign(how, assign, name)
			return statement{assign: drawn, parsed: parsed, path: path}, key, nil
		}
	}
	return statement{}, "", fmt.Errorf("%s holds no window, type, template, text icon or format named %q", path, name)
}

// entryAssign is the statement an entry that is no window is drawn as, and the key it is drawn by: a type is its
// name with an empty block, a template a widget using it, a text icon an icon of its iconsize's texture and size, and
// a format a textbox whose text is a sample in the format. A text icon with no texture is listed and drawn as an
// icon with none.
func (resolver *resolver) entryAssign(how string, assign *jomini.Assignment, name string) (*jomini.Assignment, string) {
	switch how {
	case entryType:
		return &jomini.Assignment{Key: assign.Key, Value: &jomini.Block{}, Range: assign.Range}, name
	case entryTemplate:
		using := &jomini.Assignment{Key: jomini.Scalar{Text: usingKey}, Value: &jomini.Scalar{Text: name}}
		return blockOf(entryWidget, assign.Range, using), entryWidget
	case entryFormat:
		sample := &jomini.Scalar{Text: "#" + name + " Sample text#!", Quoted: true}
		text := &jomini.Assignment{Key: jomini.Scalar{Text: "text"}, Value: sample}
		return blockOf("textbox", assign.Range, text), "textbox"
	}
	texture := iconFields(assign)
	if texture == nil {
		resolver.missing.add("texticon", name)
	}
	return blockOf("icon", assign.Range, texture...), "icon"
}

// blockOf is an assignment of a key to a block of statements, written at a span.
func blockOf(key string, span jomini.Range, statements ...jomini.Statement) *jomini.Assignment {
	return &jomini.Assignment{Key: jomini.Scalar{Text: key}, Value: &jomini.Block{Statements: statements}, Range: span}
}

// iconFields are the texture and size a text icon's iconsize block writes; nil where it writes no texture.
func iconFields(assign *jomini.Assignment) []jomini.Statement {
	for _, field := range jomini.Fields(jomini.ChildBlock(assign)) {
		iconsize := jomini.BlockOf(field.Value)
		if !strings.EqualFold(field.Key.Text, "iconsize") || jomini.ScalarOf(iconsize, "texture") == nil {
			continue
		}
		var statements []jomini.Statement
		for _, inner := range jomini.Fields(iconsize) {
			if strings.EqualFold(inner.Key.Text, "texture") || strings.EqualFold(inner.Key.Text, "size") {
				statements = append(statements, inner)
			}
		}
		return statements
	}
	return nil
}

// widget resolves one widget statement under a parent: its kind and chain, its statements expanded, its opening
// state, its children, text and textures; a Part is appended for it first, so a parent's index is lower than its
// descendants'.
func (resolver *resolver) widget(parent *widget, key string, own statement, depth int) *widget {
	node := &widget{part: len(resolver.list.Parts), overrides: map[string]statement{}, scale: resolver.scale(), alpha: 1}
	parentPart := -1
	if parent != nil {
		parentPart = parent.part
		maps.Copy(node.overrides, parent.overrides)
	}
	resolver.list.Parts = append(resolver.list.Parts, Part{Parent: parentPart, Key: key, Location: resolver.location(own)})
	kind, chain := resolver.chain(key)
	node.kind, node.class = kind, game.LayoutClasses[kind]
	resolver.list.Parts[node.part].Kind = kind
	chain = append(chain, own)
	for _, source := range chain {
		resolver.scanOverrides(node, source, 0)
	}
	for _, source := range chain {
		resolver.expand(node, source, depth)
	}
	resolver.opening(node)
	if name, isNamed := node.scalar("name"); isNamed {
		resolver.list.Parts[node.part].Name = name
	}
	if _, has := node.scalar("datamodel"); has {
		node.rows = defaultRows
	}
	resolver.children(node, depth)
	node.hidden = hidden(node)
	if alpha, has := resolver.number(node, "alpha"); has {
		node.alpha = min(max(alpha, 0), 1)
	}
	node.text = resolver.text(node)
	resolver.textures(node)
	return node
}

// children resolves the child statements an expansion found, each under the widget; the data model's item is the
// template of its rows, whose widgets are resolved once and drawn once a row.
func (resolver *resolver) children(node *widget, depth int) {
	for _, child := range node.pending {
		node.children = append(node.children, resolver.widget(node, child.assign.Key.Text, child, depth+1))
	}
	for _, child := range node.items {
		template := resolver.widget(node, child.assign.Key.Text, child, depth+1)
		template.template = true
		node.children = append(node.children, template)
	}
}

// location is the part's location: the statement's file placed in the workspace and its whole range.
func (resolver *resolver) location(assignment statement) lsp.Location {
	file, placed := resolver.places[assignment.path]
	if !placed {
		file, _, _ = resolver.ws.Locate(assignment.path)
		resolver.places[assignment.path] = file
	}
	lines := assignment.parsed.Lines()
	return lsp.Location{
		Path: assignment.path, Origin: file.Origin, Rel: file.Rel,
		Range: lsp.Range{Start: lines.PositionAt(assignment.assign.Range.Start), End: lines.PositionAt(assignment.assign.Range.End)},
	}
}

// chain follows a key through the types to its built-in kind: each type's block, base first; a type based on its
// own name ends the chain with the kind's defaults; a key no type defines is a kind the install implies or is
// listed under Missing and read as a plain widget.
func (resolver *resolver) chain(key string) (kind string, blocks []statement) {
	kind = key
	for range maxDepth {
		block, isType := resolver.lookup(widgetCaller, kind)
		if !isType {
			if !resolver.ws.Install.Schema.WidgetKinds[kind] {
				resolver.missing.add("type", kind)
			}
			slices.Reverse(blocks)
			return kind, blocks
		}
		blocks = append(blocks, block)
		base := "widget"
		if tagged, isTagged := block.assign.Value.(*jomini.TaggedBlock); isTagged {
			base = tagged.Tag.Text
		}
		if base == kind {
			slices.Reverse(blocks)
			return kind, blocks
		}
		kind = base
	}
	resolver.missing.add("cycle", key)
	return key, nil
}

// lookup is the statement of the definition a caller's word resolves a name to: one declared in this file first, else
// the load order's winner; false for none, which its caller lists where it wants a miss.
func (resolver *resolver) lookup(caller, name string) (statement, bool) {
	ref := schema.Occurrence{Role: schema.Refers, Caller: caller, ID: name}
	defs, _ := index.Resolve(resolver.ws.Install.Schema, resolver.ws.Indexes(), ref, resolver.request.Path)
	if len(defs) == 0 {
		return statement{}, false
	}
	winner, _, _, _ := resolver.ws.Winner(defs)
	if local := slices.IndexFunc(defs, func(def index.At) bool { return def.Occurrence().Local }); local >= 0 {
		winner = defs[local]
	}
	return resolver.definition(winner, name)
}

// definition is the statement a definition is written as, written by the name it defines; false where its file
// does not parse or none starts there.
func (resolver *resolver) definition(def index.At, writtenBy string) (statement, bool) {
	parsed, _, err := resolver.ws.Parsed(resolver.parses, def.File().Path)
	if err != nil {
		return statement{}, false
	}
	assign, isAssign := parsed.AssignmentAt(int(def.Occurrence().Line), int(def.Occurrence().Col)).(*jomini.Assignment)
	if !isAssign {
		return statement{}, false
	}
	return statement{assign: assign, parsed: parsed, path: def.File().Path, writtenBy: writtenBy}, true
}

// fields yields each assignment of a source's block as a statement of the same file, with the word written just
// before it (block or blockoverride; "" for none).
func fields(source statement) iter.Seq2[string, statement] {
	return func(yield func(string, statement) bool) {
		block := jomini.ChildBlock(source.assign)
		if block == nil {
			return
		}
		previous := ""
		for _, item := range block.Statements {
			assign, isAssign := item.(*jomini.Assignment)
			if isAssign && !yield(previous, source.child(assign)) {
				return
			}
			previous = ""
			if listed, isListed := item.(*jomini.ValueStmt); isListed {
				if scalar, isScalar := listed.Value.(*jomini.Scalar); isScalar {
					previous = strings.ToLower(scalar.Text)
				}
			}
		}
	}
}

// child is an assignment of the source's block as a statement of the same file, written by the same type or template.
func (source statement) child(assign *jomini.Assignment) statement {
	return statement{assign: assign, parsed: source.parsed, path: source.path, writtenBy: source.writtenBy}
}

// scanOverrides records the blockoverrides a source writes, and those of the templates it uses, before any block is
// expanded: a later one replaces an earlier, so the widget's own win.
func (resolver *resolver) scanOverrides(node *widget, source statement, depth int) {
	if depth > maxDepth {
		return
	}
	for previous, assignment := range fields(source) {
		switch {
		case previous == "blockoverride":
			node.overrides[assignment.assign.Key.Text] = assignment
		case strings.EqualFold(assignment.assign.Key.Text, usingKey):
			scalar, isScalar := assignment.assign.Value.(*jomini.Scalar)
			if !isScalar {
				continue
			}
			if used, isTemplate := resolver.lookup(usingKey, scalar.Text); isTemplate {
				resolver.scanOverrides(node, used, depth+1)
			}
		}
	}
}

// expand appends a block's statements to the widget in the order they apply: a using puts the template's statements
// in at that point; a block "n" puts its statements in, or those of the override in force for it; a key that
// resolves as a widget is a child; an item under a datamodel is the row's template; every other statement applies
// to the widget.
func (resolver *resolver) expand(node *widget, source statement, depth int) {
	if depth > maxDepth {
		resolver.missing.add("templatecycle", source.writtenBy)
		return
	}
	for previous, assignment := range fields(source) {
		resolver.apply(node, assignment, previous, depth)
	}
}

// apply does what one statement of a block does to the widget; previous is the word written before it (block or
// blockoverride).
func (resolver *resolver) apply(node *widget, assignment statement, previous string, depth int) {
	key := strings.ToLower(assignment.assign.Key.Text)
	switch {
	case previous == "block":
		source := assignment
		if override, overridden := node.overrides[assignment.assign.Key.Text]; overridden && override.writtenBy != assignment.writtenBy {
			source = override
		}
		resolver.expand(node, source, depth+1)
	case previous == "blockoverride":
	case key == usingKey:
		resolver.use(node, assignment, depth)
	case key == "item" && jomini.ChildBlock(assignment.assign) != nil:
		resolver.collectItem(node, assignment)
	case jomini.ChildBlock(assignment.assign) != nil && resolver.isWidget(assignment.assign.Key.Text):
		node.pending = append(node.pending, assignment)
	default:
		node.statements = append(node.statements, assignment)
	}
}

// use expands the template a using statement names at its place; a template nothing defines is listed.
func (resolver *resolver) use(node *widget, assignment statement, depth int) {
	scalar, isScalar := assignment.assign.Value.(*jomini.Scalar)
	if !isScalar {
		return
	}
	used, isTemplate := resolver.lookup(usingKey, scalar.Text)
	if !isTemplate {
		resolver.missing.add("template", scalar.Text)
		return
	}
	resolver.expand(node, used, depth+1)
}

// collectItem takes the widgets an item block holds as the template of a data model's rows.
func (resolver *resolver) collectItem(node *widget, item statement) {
	for _, inner := range jomini.Fields(jomini.ChildBlock(item.assign)) {
		if jomini.ChildBlock(inner) != nil && resolver.isWidget(inner.Key.Text) {
			node.items = append(node.items, item.child(inner))
		}
	}
}

// isWidget reports whether a block-valued key names a widget: a type, or a kind the install implies.
func (resolver *resolver) isWidget(key string) bool {
	if _, isType := resolver.lookup(widgetCaller, key); isType {
		return true
	}
	return resolver.ws.Install.Schema.WidgetKinds[key]
}

// opening applies the state the game runs as a window opens: the state named _show, and every state with
// trigger_on_create = yes, each end value written as the widget's own.
func (resolver *resolver) opening(node *widget) {
	var opened []statement
	for _, assignment := range node.statements {
		if !strings.EqualFold(assignment.assign.Key.Text, "state") {
			continue
		}
		block := jomini.ChildBlock(assignment.assign)
		name, trigger := jomini.ScalarOf(block, "name"), jomini.ScalarOf(block, "trigger_on_create")
		if name != nil && name.Text == game.OpeningState || trigger != nil && strings.EqualFold(trigger.Text, yes) {
			opened = append(opened, assignment)
		}
	}
	for _, state := range opened {
		for _, field := range jomini.Fields(jomini.ChildBlock(state.assign)) {
			if _, isScalar := field.Value.(*jomini.Scalar); isScalar && !slices.ContainsFunc(game.StateControls, func(control string) bool {
				return strings.EqualFold(control, field.Key.Text)
			}) {
				node.statements = append(node.statements, state.child(field))
			}
		}
	}
}

// hidden reports whether the widget writes visible = no; a visible that is a binding is the game's to answer, so the
// widget is drawn.
func hidden(node *widget) bool {
	text, _ := node.scalar("visible")
	return strings.EqualFold(text, no) || strings.EqualFold(text, "false")
}

// isBinding reports whether the whole of a value is one binding ([GetPlayer.GetName]), which no file answers.
func isBinding(text string) bool {
	text = strings.TrimSpace(text)
	calls := jomini.Calls(text, 0)
	return len(calls) == 1 && calls[0].Start == 0 && calls[0].End == len(text) && strings.HasSuffix(text, "]")
}

// textures is the widget's textures: texture through FileAt across the indexes, then each modify_texture in order; a
// file not found goes to Missing.
func (resolver *resolver) textures(node *widget) {
	first, textured := resolver.textureOf(node)
	if !textured {
		return
	}
	node.textures = []Texture{first}
	if node.text == nil {
		node.content = size{width: float64(first.Width), height: float64(first.Height)}
	}
	for _, assignment := range node.statements {
		if !strings.EqualFold(assignment.assign.Key.Text, "modify_texture") {
			continue
		}
		modifier := &widget{statements: fieldStatements(assignment)}
		more, isTexture := resolver.textureOf(modifier)
		switch {
		case !isTexture:
		case more.Blend == blendMask:
			node.textures[0].Mask = more.URL
		default:
			node.textures = append(node.textures, more)
		}
	}
}

// fieldStatements are the assignments a statement's block holds, as statements of the same file.
func fieldStatements(assignment statement) []statement {
	var out []statement
	for _, field := range jomini.Fields(jomini.ChildBlock(assignment.assign)) {
		out = append(out, assignment.child(field))
	}
	return out
}

// textureOf is the texture a holder (a widget, or a modify_texture block as one) writes: the file's route and size,
// the frame, the sprite's border, tiling, mirroring, fit, tint and blend; none where the texture is a binding.
func (resolver *resolver) textureOf(holder *widget) (Texture, bool) {
	path, has := holder.scalar("texture")
	if !has || isBinding(path) {
		return Texture{}, false
	}
	file, listed := index.FileAt(resolver.ws.Indexes(), path)
	if !listed {
		resolver.missing.add("texture", path)
		return Texture{}, false
	}
	facts, err := texture.Info(file.Path)
	if err != nil {
		resolver.missing.add("texture", path)
		return Texture{}, false
	}
	loaded := Texture{URL: fileURL(file), Width: facts.Width, Height: facts.Height, Blend: blendNormal}
	resolver.sprite(holder, &loaded)
	if density, dense := holder.numbers("texture_density"); dense && density[0] > 0 {
		loaded.Width, loaded.Height = int(float64(loaded.Width)/density[0]), int(float64(loaded.Height)/density[0])
	}
	return loaded, true
}

// sprite reads how a holder draws its texture: frame, border, tiling, mirroring, fit, tint and blend.
func (resolver *resolver) sprite(holder *widget, drawn *Texture) {
	if frame, has := holder.numbers("frame"); has && len(frame) > 0 {
		if cell, sized := holder.numbers("framesize"); sized && len(cell) == 2 && cell[0] > 0 && drawn.Width > 0 {
			columns := max(int(float64(drawn.Width)/cell[0]), 1)
			index := max(int(frame[0])-1, 0)
			drawn.Frame = Box{
				X: float64(index%columns) * cell[0], Y: float64(index/columns) * cell[1], Width: cell[0], Height: cell[1],
			}
		}
	}
	drawn.Border = borderOf(holder)
	if choices, has := holder.choices("spritetype"); has {
		drawn.Tiled = slices.Contains(choices, "corneredtiled")
	}
	if choices, has := holder.choices("mirror"); has {
		drawn.Mirror = slices.Contains(choices, "horizontal") || slices.Contains(choices, "yes")
	}
	if choices, has := holder.choices("fittype"); has && fitTypes[choices[0]] {
		drawn.Fit = choices[0]
	}
	for _, key := range []string{colorKey, "tintcolor"} {
		if tint, has := resolver.color(holder, key); has {
			drawn.Tint = tint
		}
	}
	if choices, has := holder.choices("blend_mode"); has {
		if blend, known := blendModes[choices[0]]; known {
			drawn.Blend = blend
		} else {
			resolver.missing.add("blend", choices[0])
		}
	}
}

// borderOf is a sprite's insets top, right, bottom and left: spriteborder as one value for every side or a
// horizontal and a vertical, a side's own key over it.
func borderOf(holder *widget) [4]int {
	var border [4]int
	if sides, has := holder.numbers("spriteborder"); has {
		switch len(sides) {
		case 1:
			border = [4]int{int(sides[0]), int(sides[0]), int(sides[0]), int(sides[0])}
		case 2:
			border = [4]int{int(sides[1]), int(sides[0]), int(sides[1]), int(sides[0])}
		}
	}
	for i, key := range []string{"spriteborder_top", "spriteborder_right", "spriteborder_bottom", "spriteborder_left"} {
		if side, has := holder.numbers(key); has && len(side) > 0 {
			border[i] = int(side[0])
		}
	}
	return border
}

// number is the widget's value for a key as a number; false where it is not written, or is a binding.
func (resolver *resolver) number(node *widget, key string) (float64, bool) {
	if numbers, has := node.numbers(key); has && len(numbers) > 0 {
		return numbers[0], true
	}
	text, has := node.scalar(key)
	if !has || isBinding(text) {
		return 0, false
	}
	number, err := strconv.ParseFloat(text, 64)
	return number, err == nil
}
