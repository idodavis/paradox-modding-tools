// gui_resolve.go is pass 1: a GUI file's entry resolved into a tree of widgets. Each widget's type chain is followed
// to its built-in kind, its templates and blocks expanded in the order they apply, the state a window opens in
// applied, its visibility answered, and its text and textures found; every widget is listed as a part.

package draw

import (
	"fmt"
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

// defaultRows is how many rows a data model draws where no fact gives its count.
const defaultRows = 3

// entryHow is how each database a file's entries are drawn from is drawn: a window is its own statement, a type is
// drawn by naming it and a template by using it.
var entryHow = map[string]string{
	"gui.widget": entryWidget, "gui.type": entryType, "gui.local_type": entryType,
	"gui.template": entryTemplate, "gui.local_template": entryTemplate,
}

// How an entry is drawn: a window is its own statement, a type is named and a template used.
const (
	entryWidget   = "widget"
	entryType     = "type"
	entryTemplate = "template"
)

// resolver is pass 1 over one request: the workspace, the parses made for it, the evaluator with the facts, and the
// list being built with what was not found.
type resolver struct {
	ws        *workspace.Session
	parses    workspace.Parses
	evaluator *evaluator
	list      *List
	request   Request
	fonts     map[string]*face // by name, style and size, the faces opened for this render
	missing   map[string]bool
	places    map[string]inventory.File // by path, where each file sits
}

// resolve runs pass 1: the entry found by name, its widget tree resolved, every part listed.
func resolve(ws *workspace.Session, request Request, facts map[string]string) (*resolver, *widget, error) {
	evaluator := newEvaluator(ws, facts, request.Language)
	resolver := &resolver{
		ws: ws, parses: workspace.Parses{}, evaluator: evaluator, request: request, fonts: map[string]*face{},
		missing: evaluator.missing, places: map[string]inventory.File{},
		list: &List{Elements: []Element{}, Parts: []Part{}, Fonts: []Font{}, Facts: []Fact{}, Missing: []string{}},
	}
	entry, key, err := resolver.entry(request.Path, request.Name)
	if err != nil {
		return nil, nil, err
	}
	root := resolver.widget(nil, key, entry, 0)
	resolver.list.Parts[0].Name = request.Name
	for _, asked := range evaluator.order {
		resolver.list.Facts = append(resolver.list.Facts, evaluator.asked[asked])
	}
	resolver.list.Missing = slices.Sorted(maps.Keys(resolver.missing))
	return resolver, root, nil
}

// entry is the statement the request's name resolves to in the file, and the key it is drawn by: a window is its
// own statement; a type is drawn by its name with nothing of its own, and a template by a widget that uses it.
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
			own, key := statement{assign: assign, parsed: parsed, path: path}, assign.Key.Text
			switch how {
			case entryType:
				own.assign, own.made, key = &jomini.Assignment{Key: assign.Key, Value: &jomini.Block{}, Range: assign.Range}, true, name
			case entryTemplate:
				using := &jomini.Assignment{Key: jomini.Scalar{Text: usingKey}, Value: &jomini.Scalar{Text: name}}
				block := &jomini.Block{Statements: []jomini.Statement{using}}
				own.assign, own.made, key = &jomini.Assignment{Key: jomini.Scalar{Text: entryWidget}, Value: block, Range: assign.Range}, true, entryWidget
			}
			return own, key, nil
		}
	}
	return statement{}, "", fmt.Errorf("%s holds no window, type or template named %q", path, name)
}

// widget resolves one widget statement under a parent: its kind and chain, its statements expanded, its opening
// state, its children, visibility, text and textures; a Part is appended for it first, so a parent's index is lower
// than its descendants'.
func (resolver *resolver) widget(parent *widget, key string, own statement, depth int) *widget {
	node := &widget{part: len(resolver.list.Parts), source: own, row: -1, overrides: map[string]statement{}, visibility: Shown, scale: resolver.scale(), alpha: 1}
	parentPart := -1
	if parent != nil {
		parentPart = parent.part
		node.dataType, node.row, node.inRows = parent.dataType, parent.row, parent.inRows
		maps.Copy(node.overrides, parent.overrides)
	}
	resolver.list.Parts = append(resolver.list.Parts, Part{Parent: parentPart, Key: key, Location: resolver.location(own), Visibility: Shown})
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
	resolver.dataContext(node)
	resolver.rows(node)
	resolver.children(node, depth)
	resolver.visibility(node)
	resolver.list.Parts[node.part].Visibility, resolver.list.Parts[node.part].Fact = node.visibility, node.fact
	if alpha, has := resolver.number(node, "alpha"); has {
		node.alpha = min(max(alpha, 0), 1)
	}
	node.text = resolver.text(node)
	resolver.textures(node)
	return node
}

// children resolves the child statements an expansion found, each under the widget; the data model's item is the
// template of its rows, whose widgets are resolved once with the rows they are drawn for.
func (resolver *resolver) children(node *widget, depth int) {
	for _, child := range node.pending {
		node.children = append(node.children, resolver.widget(node, child.assign.Key.Text, child, depth+1))
	}
	for _, child := range node.items {
		template := resolver.widget(node, child.assign.Key.Text, child, depth+1)
		template.template, template.inRows = true, max(node.rows, 1)
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
		block, isType := resolver.typeOf(kind)
		if !isType {
			if !resolver.ws.Install.Schema.WidgetKinds[kind] {
				resolver.missing["type "+kind] = true
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
	resolver.missing["type "+key+": a cycle"] = true
	return key, nil
}

// typeOf is the statement of the type a name is called: one declared in this file first, else the winning one of
// the install and the mods; false for none.
func (resolver *resolver) typeOf(name string) (statement, bool) {
	ref := schema.Occurrence{Role: schema.Refers, Caller: game.Shapes[game.ShapeWidget].Caller, ID: name}
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

// template is the statement of the template a name is called: one declared in this file first, else the winning one;
// false for none, which is listed.
func (resolver *resolver) template(name string) (statement, bool) {
	ref := schema.Occurrence{Role: schema.Refers, Caller: usingKey, ID: name}
	defs, _ := index.Resolve(resolver.ws.Install.Schema, resolver.ws.Indexes(), ref, resolver.request.Path)
	if len(defs) == 0 {
		resolver.missing["template "+name] = true
		return statement{}, false
	}
	winner, _, _, _ := resolver.ws.Winner(defs)
	if local := slices.IndexFunc(defs, func(def index.At) bool { return def.Occurrence().Local }); local >= 0 {
		winner = defs[local]
	}
	return resolver.definition(winner, name)
}

// scanOverrides records the blockoverrides a source writes, and those of the templates it uses, before any block is
// expanded: a later one replaces an earlier, so the widget's own win.
func (resolver *resolver) scanOverrides(node *widget, source statement, depth int) {
	block := jomini.ChildBlock(source.assign)
	if block == nil || depth > maxDepth {
		return
	}
	previous := ""
	for _, item := range block.Statements {
		if listed, isListed := item.(*jomini.ValueStmt); isListed {
			previous = ""
			if scalar, isScalar := listed.Value.(*jomini.Scalar); isScalar {
				previous = strings.ToLower(scalar.Text)
			}
			continue
		}
		assign, isAssign := item.(*jomini.Assignment)
		if !isAssign {
			previous = ""
			continue
		}
		assignment := statement{assign: assign, parsed: source.parsed, path: source.path, writtenBy: source.writtenBy}
		switch {
		case previous == "blockoverride":
			node.overrides[assign.Key.Text] = assignment
		case strings.EqualFold(assign.Key.Text, usingKey):
			if used, isTemplate := resolver.usedTemplate(assign); isTemplate {
				resolver.scanOverrides(node, used, depth+1)
			}
		}
		previous = ""
	}
}

// usedTemplate is the template a using statement names, without listing a miss: the expansion lists it.
func (resolver *resolver) usedTemplate(assign *jomini.Assignment) (statement, bool) {
	scalar, isScalar := assign.Value.(*jomini.Scalar)
	if !isScalar {
		return statement{}, false
	}
	ref := schema.Occurrence{Role: schema.Refers, Caller: usingKey, ID: scalar.Text}
	defs, _ := index.Resolve(resolver.ws.Install.Schema, resolver.ws.Indexes(), ref, resolver.request.Path)
	if len(defs) == 0 {
		return statement{}, false
	}
	winner, _, _, _ := resolver.ws.Winner(defs)
	return resolver.definition(winner, scalar.Text)
}

// expand appends a block's statements to the widget in the order they apply: a using puts the template's statements
// in at that point; a block "n" puts its statements in, or those of the override in force for it; a key that
// resolves as a widget is a child; an item under a datamodel is the row's template; every other statement applies
// to the widget.
func (resolver *resolver) expand(node *widget, source statement, depth int) {
	block := jomini.ChildBlock(source.assign)
	if block == nil {
		return
	}
	if depth > maxDepth {
		resolver.missing["template "+source.writtenBy+": a cycle"] = true
		return
	}
	previous := ""
	for _, item := range block.Statements {
		if listed, isListed := item.(*jomini.ValueStmt); isListed {
			previous = ""
			if scalar, isScalar := listed.Value.(*jomini.Scalar); isScalar {
				previous = strings.ToLower(scalar.Text)
			}
			continue
		}
		if assign, isAssign := item.(*jomini.Assignment); isAssign {
			assignment := statement{assign: assign, parsed: source.parsed, path: source.path, writtenBy: source.writtenBy}
			resolver.apply(node, assignment, previous, depth)
		}
		previous = ""
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
		if scalar, isScalar := assignment.assign.Value.(*jomini.Scalar); isScalar {
			if used, isTemplate := resolver.template(scalar.Text); isTemplate {
				resolver.expand(node, used, depth+1)
			}
		}
	case key == "item" && jomini.ChildBlock(assignment.assign) != nil:
		resolver.collectItem(node, assignment)
	case jomini.ChildBlock(assignment.assign) != nil && resolver.isWidget(assignment.assign.Key.Text):
		node.pending = append(node.pending, assignment)
	default:
		node.statements = append(node.statements, assignment)
	}
}

// collectItem takes the widgets an item block holds as the template of a data model's rows.
func (resolver *resolver) collectItem(node *widget, item statement) {
	for _, inner := range jomini.Fields(jomini.ChildBlock(item.assign)) {
		if jomini.ChildBlock(inner) != nil && resolver.isWidget(inner.Key.Text) {
			node.items = append(node.items, statement{assign: inner, parsed: item.parsed, path: item.path, writtenBy: item.writtenBy})
		}
	}
}

// isWidget reports whether a block-valued key names a widget: a type, or a kind the install implies.
func (resolver *resolver) isWidget(key string) bool {
	if _, isType := resolver.typeOf(key); isType {
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
				node.statements = append(node.statements, statement{assign: field, parsed: state.parsed, path: state.path, writtenBy: state.writtenBy})
			}
		}
	}
}

// dataContext notes the data type the widget's datacontext leads to, which its children read their bindings in.
func (resolver *resolver) dataContext(node *widget) {
	text, has := node.scalar(game.FileTypes[".gui"].ContextKey)
	if !has {
		return
	}
	if call, isBinding := bindingOf(text); isBinding {
		node.dataType = resolver.ws.Install.Schema.CallLeadsTo(jomini.ParseCall(call, 0), node.dataType)
	}
}

// rows is how many rows a data model draws: its fact's count where one is set, else three.
func (resolver *resolver) rows(node *widget) {
	text, has := node.scalar("datamodel")
	if !has {
		return
	}
	node.rows = defaultRows
	if call, isBinding := bindingOf(text); isBinding {
		resolver.evaluator.context, resolver.evaluator.row = node.dataType, node.row
		if answer := resolver.evaluator.evaluate(call, game.DataList); answer.known {
			node.rows = answer.count
		}
	}
}

// visibility is the widget's visibility: visible = no is Hidden; a binding is the evaluator's answer, Hidden on no,
// Shown on yes, Unknown otherwise, with the fact that decides it; a row template's is the join of its rows'.
func (resolver *resolver) visibility(node *widget) {
	text, has := node.scalar("visible")
	if !has {
		return
	}
	call, isBinding := bindingOf(text)
	if !isBinding {
		if strings.EqualFold(text, no) || strings.EqualFold(text, "false") {
			node.visibility = Hidden
		}
		return
	}
	rows := max(node.inRows, 1)
	for row := range rows {
		resolver.evaluator.context, resolver.evaluator.row, resolver.evaluator.touched = node.dataType, row, nil
		if node.inRows == 0 {
			resolver.evaluator.row = node.row
		}
		answer := resolver.evaluator.evaluate(call, game.DataBool)
		state := Unknown
		if answer.known {
			state = map[bool]string{true: Shown, false: Hidden}[answer.yes]
		}
		node.byRow = append(node.byRow, state)
		if row == 0 && len(resolver.evaluator.touched) > 0 {
			node.fact = resolver.evaluator.touched[0]
		}
	}
	node.visibility = joinVisibility(node.byRow)
	if node.inRows == 0 {
		node.byRow = nil
	}
}

// joinVisibility is a row template's visibility over its rows: Shown where any row shows it, else Unknown where any
// is unknown, else Hidden.
func joinVisibility(rows []string) string {
	switch {
	case slices.Contains(rows, Shown):
		return Shown
	case slices.Contains(rows, Unknown):
		return Unknown
	}
	return Hidden
}

// bindingOf is the call a value holds when the whole value is one binding ([GetPlayer.GetName]); false for any
// other text.
func bindingOf(text string) (string, bool) {
	text = strings.TrimSpace(text)
	calls := jomini.Calls(text, 0)
	if len(calls) != 1 || calls[0].Start != 0 || calls[0].End != len(text) || !strings.HasSuffix(text, "]") {
		return "", false
	}
	return text[1 : len(text)-1], true
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
		out = append(out, statement{assign: field, parsed: assignment.parsed, path: assignment.path, writtenBy: assignment.writtenBy})
	}
	return out
}

// textureOf is the texture a holder (a widget, or a modify_texture block as one) writes: the file's route and size,
// the frame, the sprite's border, tiling, mirroring, fit, tint and blend.
func (resolver *resolver) textureOf(holder *widget) (Texture, bool) {
	path, has := holder.scalar("texture")
	if !has {
		return Texture{}, false
	}
	if call, isBinding := bindingOf(path); isBinding {
		resolver.evaluator.context = holder.dataType
		answer := resolver.evaluator.evaluate(call, game.DataText)
		if !answer.known {
			return Texture{}, false
		}
		path = answer.text
	}
	file, listed := index.FileAt(resolver.ws.Indexes(), path)
	if !listed {
		resolver.missing["texture "+path] = true
		return Texture{}, false
	}
	facts, err := texture.Info(file.Path)
	if err != nil {
		resolver.missing["texture "+path] = true
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
			resolver.missing["blend_mode "+choices[0]] = true
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

// number is the widget's value for a key as a number, a binding answered by the evaluator where it is known.
func (resolver *resolver) number(node *widget, key string) (float64, bool) {
	if numbers, has := node.numbers(key); has && len(numbers) > 0 {
		return numbers[0], true
	}
	text, has := node.scalar(key)
	if !has {
		return 0, false
	}
	call, isBinding := bindingOf(text)
	if !isBinding {
		number, err := strconv.ParseFloat(text, 64)
		return number, err == nil
	}
	resolver.evaluator.context, resolver.evaluator.row = node.dataType, node.row
	answer := resolver.evaluator.evaluate(call, game.DataNumber)
	return answer.number, answer.known && answer.kind == game.DataNumber
}
