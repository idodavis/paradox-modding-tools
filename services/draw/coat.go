package draw

import (
	"cmp"
	"fmt"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/samber/lo"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/schema"
	"paradox-modding-tools/services/lsp"
	"paradox-modding-tools/services/workspace"
)

// An Instance is one placement of a layer on the field. Values the file does not set take the game's defaults.
type Instance struct {
	Position [2]float64 `json:"position"`
	Scale    [2]float64 `json:"scale"`
	Rotation float64    `json:"rotation"`
	Depth    float64    `json:"depth"`
	Range    lsp.Range  `json:"range"`
}

// A Layer is one layer of a coat: the pattern, an emblem or a sub-coat. It carries the image file found for its
// texture, its mask, and its colors as CSS.
type Layer struct {
	Role      string     `json:"role"`    // the key as written: "pattern", "colored_emblem", "textured_emblem" or "sub"
	Texture   string     `json:"texture"` // as written; "missing:x" when `list "x"` names a list that does not exist
	Path      string     `json:"path"`    // the image file for the texture
	Mask      string     `json:"mask"`
	Regions   []int      `json:"regions"` // the pattern's color regions an emblem shows in; none means all of them
	Colors    []string   `json:"colors"`
	Lists     []Group    `json:"lists"` // the template lists its values are drawn from, each with its options
	Instances []Instance `json:"instances"`
	Sub       *Coat      `json:"sub,omitempty"`
	Range     lsp.Range  `json:"range"`
}

// A Coat is a coat of arms resolved for drawing, as its layers.
type Coat struct {
	Name   string          `json:"name"`
	Layers []Layer         `json:"layers"`
	Range  lsp.Range       `json:"range"`
	Place  workspace.Place `json:"place"`
}

// A NamedColor is one of the game's named colors, with its CSS value for the color picker.
type NamedColor struct {
	Name string `json:"name"`
	CSS  string `json:"css"`
}

// A coatResolver makes the layers of one coat and finds the clause of each.
type coatResolver struct {
	ws         *workspace.Session
	mods       *index.Index
	places     map[string]workspace.Place // by path
	picks      map[string]string
	definition clause            // the coat asked for
	sources    []clause          // the clause of each of its layers, in the order of its layers
	colors     map[string]string // the CSS of the named colors looked up, by name
}

// The words a coat of arms is written with, the parts of a placement, and the start of what a list that is not there
// is shown as.
const (
	patternKey  = "pattern"
	instanceKey = "instance"
	subKey      = "sub"
	parentKey   = "parent"
	textureKey  = "texture"
	maskKey     = "mask"
	depthKey    = "depth"
	listTag     = "list"
	noParent    = "none"
	missingList = "missing:"
	partCenter  = "center"
	partCorner  = "corner"
	partSize    = "size"
	partTurn    = "turn"
	partDepth   = "depth"
)

// ResolveCoat resolves one coat of arms in a file into its layers. When a layer's value is a `list "x"`, the
// option drawn is the one picked for that list, by the ID of its group in the layer's lists, or the heaviest
// option in the list if nothing was picked. It returns an error if the file is not in the workspace or does not
// define the coat.
func (drw *Service) ResolveCoat(path, name string, picks map[string]string) (Coat, error) {
	resolver, err := drw.findCoat(path, name, picks)
	if err != nil {
		return Coat{}, err
	}
	return resolver.resolveCoat(), nil
}

// findCoat returns the resolver for the coat that a file defines under the given name, with the given picks. It
// returns an error if the file is not in the workspace or does not define the coat.
func (drw *Service) findCoat(path, name string, picks map[string]string) (*coatResolver, error) {
	ws, err := drw.Workspace.Session()
	if err != nil {
		return nil, err
	}
	mods := ws.Mods.Load()
	file, ok := mods.FindFile(path)
	if !ok {
		return nil, fmt.Errorf("the file %q is not in the workspace", path)
	}
	var definition clause
	site, ok := findDrawn(mods, file, name, func(database string) bool { return database == game.Coat.Database })
	if ok {
		definition, ok = makeClause(ws, mods, index.Winner{Site: site})
	}
	if !ok {
		return nil, fmt.Errorf("the file %q does not define the coat %q", path, name)
	}
	return &coatResolver{
		ws: ws, mods: mods, places: map[string]workspace.Place{}, picks: picks, colors: map[string]string{},
		definition: definition,
	}, nil
}

// resolveCoat resolves the coat asked for: its layers, from its own statements and its parent's. A coat written as
// the name of another is that coat. An alias that no coat answers, or that passes through too many, has no layers.
func (resolver *coatResolver) resolveCoat() Coat {
	definition := resolver.definition
	coat := Coat{
		Name: definition.tree.Unquoted(definition.tree.Statements[definition.statement].Key), Layers: []Layer{},
		Range: toRange(definition), Place: locate(resolver.ws, resolver.places, definition.path),
	}
	root := definition
	for range maxDepth {
		current := root.tree.Statements[root.statement]
		if current.IsBlock() {
			coat.Layers = resolver.resolveLayers(resolver.listCoatFields(root, 0), 0)
			return coat
		}
		target, ok := findKeptClause(resolver.ws, resolver.mods, schema.Reference{Database: game.Coat.Database, Name: root.tree.Unquoted(current.Value)})
		if !ok {
			return coat
		}
		root = target
	}
	return coat
}

// listCoatFields returns the statements that apply to a coat, in the order they apply: its parent's first, leaving out
// the parent's instances and, where the coat writes emblems of its own, the parent's emblems, then the coat's own. The
// depth is how many parents lie between the coat and the one asked for.
func (resolver *coatResolver) listCoatFields(coat clause, depth int) []clause {
	own := listChildren(coat)
	parentField, ok := findField(own, parentKey)
	if !ok || depth >= maxDepth {
		return own
	}
	_, label, _ := splitField(parentField)
	parent, ok := findKeptClause(resolver.ws, resolver.mods, schema.Reference{Database: game.Coat.Database, Name: label})
	if !ok || strings.EqualFold(label, noParent) {
		return own
	}
	emblematic := slices.ContainsFunc(own, isEmblem)
	inherited := lo.Filter(resolver.listCoatFields(parent, depth+1), func(field clause, _ int) bool {
		key, _, _ := splitField(field)
		return key != instanceKey && (!emblematic || !isEmblem(field))
	})
	return append(inherited, own...)
}

// listChildren returns the statements written directly inside a clause.
func listChildren(holder clause) []clause {
	return lo.Map(slices.Collect(holder.tree.Children(holder.statement)), func(statement int32, _ int) clause {
		return clause{tree: holder.tree, path: holder.path, statement: statement}
	})
}

// splitField returns the key in lower case, the value without its quotes and the tag of a statement.
func splitField(field clause) (key, value, tag string) {
	current := field.tree.Statements[field.statement]
	return strings.ToLower(field.tree.Unquoted(current.Key)), field.tree.Unquoted(current.Value), field.tree.Text(current.Tag)
}

// findField returns the last of the given statements that has the given key. It returns false if none has.
func findField(fields []clause, key string) (clause, bool) {
	for _, field := range slices.Backward(fields) {
		if candidate, _, _ := splitField(field); candidate == key {
			return field, true
		}
	}
	return clause{}, false
}

// isEmblem reports whether a statement writes an emblem: a key the game gives a texture folder, other than the pattern.
func isEmblem(field clause) bool {
	key, _, _ := splitField(field)
	_, hasFolder := game.Coat.TextureFolders[key]
	return hasFolder && key != patternKey
}

// resolveLayers returns the layers of a coat from its statements: the pattern, then each emblem and, for the coat
// asked for and not one inside it, each sub. The depth is how many coats lie between the coat and the one asked for.
func (resolver *coatResolver) resolveLayers(fields []clause, depth int) []Layer {
	colors, lists := resolver.resolveColors(fields, nil)
	layers := []Layer{}
	sources := []clause{}
	patternFile := ""
	if pattern, ok := findField(fields, patternKey); ok {
		layer := resolver.resolvePattern(pattern, colors, lists)
		patternFile = layer.Path
		layers = append(layers, layer)
		sources = append(sources, pattern)
	}
	for _, field := range fields {
		if isEmblem(field) {
			layers = append(layers, resolver.resolveEmblem(field, colors, patternFile))
			sources = append(sources, field)
		}
	}
	for _, field := range fields {
		if key, _, _ := splitField(field); key == subKey && depth == 0 {
			layers = append(layers, resolver.resolveSub(field))
			sources = append(sources, field)
		}
	}
	if depth == 0 {
		resolver.sources = sources
	}
	return layers
}

// resolvePattern returns the layer of a coat's pattern, painted in the coat's colors. The lists of the coat's colors
// are its own.
func (resolver *coatResolver) resolvePattern(field clause, colors []string, lists []Group) Layer {
	texture, own := resolver.resolveValue(field)
	return Layer{
		Role: field.tree.Unquoted(field.tree.Statements[field.statement].Key), Texture: texture,
		Path: resolver.findCoatFile(patternKey, texture), Colors: colors, Lists: slices.Concat(own, lists),
		Regions: []int{}, Instances: []Instance{}, Range: toRange(field),
	}
}

// resolveEmblem returns the layer of an emblem: its texture, its colors over the coat's, its mask where it writes
// one, which is the file of the coat's pattern with the regions it shows in, and its instances.
func (resolver *coatResolver) resolveEmblem(field clause, colors []string, patternFile string) Layer {
	children := listChildren(field)
	key, _, _ := splitField(field)
	emblemColors, lists := resolver.resolveColors(children, colors)
	layer := Layer{
		Role: field.tree.Unquoted(field.tree.Statements[field.statement].Key), Colors: emblemColors, Lists: lists,
		Regions: []int{}, Instances: resolver.resolveInstances(children, instanceKey), Range: toRange(field),
	}
	if texture, ok := findField(children, textureKey); ok {
		var own []Group
		layer.Texture, own = resolver.resolveValue(texture)
		layer.Path, layer.Lists = resolver.findCoatFile(key, layer.Texture), slices.Concat(own, lists)
	}
	if mask, ok := findField(children, maskKey); ok {
		layer.Mask = patternFile
		layer.Regions = lo.Map(listNumbers(mask), func(region float64, _ int) int { return int(region) })
	}
	return layer
}

// resolveSub returns the layer of a sub: the coat its parent names, laid under the sub's own statements, and the
// places it fills.
func (resolver *coatResolver) resolveSub(field clause) Layer {
	children := listChildren(field)
	parentField, _ := findField(children, parentKey)
	_, parent, _ := splitField(parentField)
	sub := Coat{
		Name: parent, Layers: resolver.resolveLayers(resolver.listCoatFields(field, 0), 1), Range: toRange(field),
		Place: locate(resolver.ws, resolver.places, field.path),
	}
	return Layer{
		Role: field.tree.Unquoted(field.tree.Statements[field.statement].Key), Texture: parent, Colors: []string{}, Lists: []Group{},
		Regions: []int{}, Sub: &sub, Range: sub.Range, Instances: resolver.resolveInstances(children, subKey),
	}
}

// resolveValue returns the text a statement of a coat writes as its value, and the list it draws from. A value written
// as `list "x"` is the option of the list that is picked for it, or else the heaviest one; it is "missing:x" if no
// list has the name.
func (resolver *coatResolver) resolveValue(field clause) (string, []Group) {
	key, value, tag := splitField(field)
	if tag != listTag {
		return value, nil
	}
	definition, ok := findKeptClause(resolver.ws, resolver.mods, schema.Reference{Database: game.Coat.ListsDatabase, Name: value})
	if !ok {
		return missingList + value, nil
	}
	list := Group{ID: fmt.Sprintf("%s:%d:%s", field.path, field.statement, key), Label: value}
	heaviest, drawn, weighed := 0.0, "", false
	for _, option := range listChildren(definition) {
		weight, text, _ := splitField(option)
		if number, err := strconv.ParseFloat(weight, 64); err == nil && text != "" {
			list.Options = append(list.Options, text)
			if !weighed || number > heaviest {
				heaviest, drawn, weighed = number, text, true
			}
		}
	}
	list.Options = lo.Uniq(list.Options)
	list.Picked = cmp.Or(resolver.picks[list.ID], drawn)
	return list.Picked, []Group{list}
}

// findCoatFile returns the image file of a coat's texture, under the folder of the key that writes it. It returns ""
// if there is no texture, a list gave none, or no file has the name.
func (resolver *coatResolver) findCoatFile(key, texture string) string {
	if texture == "" || strings.HasPrefix(texture, missingList) {
		return ""
	}
	return resolveTexture(resolver.ws, resolver.mods, nil, game.Coat.TextureFolders[key]+"/"+texture)
}

// resolveColors returns the colors a coat's or an emblem's statements give its slots as CSS, with the fallback at 0,
// and the lists they are drawn from. A slot that is not written is the base's.
func (resolver *coatResolver) resolveColors(fields []clause, base []string) ([]string, []Group) {
	colors := make([]string, len(game.Coat.Slots)+1)
	colors[0] = game.Coat.Fallback
	var lists []Group
	for position, slot := range game.Coat.Slots {
		colors[position+1] = lo.NthOr(base, position+1, "")
		if field, ok := findField(fields, slot.Key); ok {
			var own []Group
			colors[position+1], own = resolver.resolveColor(field, base)
			lists = append(lists, own...)
		}
	}
	return colors, lists
}

// resolveColor returns the CSS of the color a statement writes: a color block, the name of a named color, or the name
// of a slot, which is the base's color in it. It also returns the list the color is drawn from.
func (resolver *coatResolver) resolveColor(field clause, base []string) (string, []Group) {
	if color, ok := game.ToColor(field.tree, field.statement); ok {
		return color.ToCSS(), nil
	}
	label, lists := resolver.resolveValue(field)
	if position := slices.IndexFunc(game.Coat.Slots, func(slot game.CoatSlot) bool {
		return strings.EqualFold(slot.Key, label)
	}); position >= 0 {
		return lo.NthOr(base, position+1, ""), lists
	}
	css, ok := resolver.colors[label]
	if !ok {
		if definition, found := findKeptClause(resolver.ws, resolver.mods, schema.Reference{Database: game.Coat.ColorsDatabase, Name: label}); found {
			css = makeColor(definition)
		}
		resolver.colors[label] = css
	}
	return css, lists
}

// makeColor returns the CSS of the color that the given definition of a named color writes, or "" if it is not a
// color.
func makeColor(definition clause) string {
	color, ok := game.ToColor(definition.tree, definition.statement)
	if !ok {
		return ""
	}
	return color.ToCSS()
}

// resolveInstances returns the instances the given statements write, in the given row of the game's placements. Where
// they write none, it returns the one default instance the game draws, which has no range.
func (resolver *coatResolver) resolveInstances(children []clause, row string) []Instance {
	instances := []Instance{}
	for _, field := range children {
		if key, _, _ := splitField(field); key == instanceKey {
			instances = append(instances, resolver.resolveInstance(field, row))
		}
	}
	if len(instances) == 0 {
		instances = append(instances, resolver.resolveInstance(clause{statement: absentStatement}, row))
	}
	return instances
}

// resolveInstance returns the instance an instance block writes: each key it writes over the game's default for it.
func (resolver *coatResolver) resolveInstance(field clause, row string) Instance {
	var instance Instance
	for _, place := range game.Coat.Placement[row] {
		setPlacement(&instance, place.Part, place.Defaults)
	}
	if field.statement == absentStatement {
		return instance
	}
	instance.Range = toRange(field)
	for _, child := range listChildren(field) {
		key, _, _ := splitField(child)
		part := lo.Ternary(key == depthKey, partDepth, game.Coat.Placement[row][key].Part)
		setPlacement(&instance, part, listNumbers(child))
	}
	return instance
}

// setPlacement sets the part of an instance that the given part of a placement moves, sizes or turns, to the numbers
// written. A part of no kind, or no numbers, set nothing.
func setPlacement(instance *Instance, part string, numbers []float64) {
	if len(numbers) == 0 {
		return
	}
	pair := [2]float64{numbers[0], numbers[min(1, len(numbers)-1)]}
	switch part {
	case partCenter, partCorner:
		instance.Position = pair
	case partSize:
		instance.Scale = pair
	case partTurn:
		instance.Rotation = numbers[0]
	case partDepth:
		instance.Depth = numbers[0]
	}
}

// listNumbers returns the numbers a statement writes: its value, the values inside its block, or the x and y inside
// it. It returns none if a value is no number.
func listNumbers(field clause) []float64 {
	var texts []string
	if _, value, _ := splitField(field); value != "" {
		texts = append(texts, value)
	}
	for _, child := range listChildren(field) {
		if key, value, _ := splitField(child); key == "" || key == "x" || key == "y" {
			texts = append(texts, value)
		}
	}
	var numbers []float64
	for _, text := range texts {
		number, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return nil
		}
		numbers = append(numbers, number)
	}
	return numbers
}

// ListColors returns the game's named colors for the color picker.
func (drw *Service) ListColors() []NamedColor {
	ws, err := drw.Workspace.Session()
	if err != nil {
		return []NamedColor{}
	}
	mods := ws.Mods.Load()
	colors := []NamedColor{}
	for _, winner := range mods.ListKept(ws.Schema, game.Coat.ColorsDatabase) {
		if definition, ok := makeClause(ws, mods, winner); ok {
			if css := makeColor(definition); css != "" {
				colors = append(colors, NamedColor{Name: mods.ToReference(winner.Site).Name, CSS: css})
			}
		}
	}
	return colors
}

// ListTextures returns the texture files that a layer with the given role can use, from the game's coat folders.
func (drw *Service) ListTextures(role string) []string {
	ws, err := drw.Workspace.Session()
	if err != nil {
		return []string{}
	}
	return listImages(ws, ws.Mods.Load(), game.Coat.TextureFolders[role], "")
}

// ListFrames returns the frame files the game ships, for the Frame menu.
func (drw *Service) ListFrames() []string {
	ws, err := drw.Workspace.Session()
	if err != nil {
		return []string{}
	}
	return listImages(ws, ws.Mods.Load(), path.Dir(game.Coat.FrameMask), strings.TrimPrefix(path.Base(game.Coat.FrameMask), "%s"))
}

// listImages returns the paths from their roots of the image files the game reads in a folder whose names end as given,
// sorted. No folder has none.
func listImages(ws *workspace.Session, mods *index.Index, folder, ending string) []string {
	paths := []string{}
	for _, file := range mods.ListImages(folder + "/") {
		if _, rest := schema.SplitStage(ws.Game, file.Rel); path.Dir(rest) == folder && strings.HasSuffix(rest, ending) {
			paths = append(paths, file.Rel)
		}
	}
	slices.Sort(paths)
	return slices.Compact(paths)
}

// AddLayer inserts a new layer into a coat at the given position among its layers, 0 being first, and returns the
// edits that do it. The new layer is the game's template for that role.
func (drw *Service) AddLayer(path, coat, role string, position int) ([]lsp.Edit, error) {
	resolver, err := drw.findCoat(path, coat, nil)
	if err != nil {
		return nil, err
	}
	resolver.resolveCoat()
	definition := resolver.definition
	tree := definition.tree
	if !tree.Statements[definition.statement].IsBlock() {
		return nil, fmt.Errorf("the coat %q is written as the name of another coat", coat)
	}
	if role == patternKey && slices.ContainsFunc(resolver.sources, isPattern) {
		return nil, fmt.Errorf("the coat %q has a pattern already", coat)
	}
	lines, err := makeLayerLines(resolver.ws, resolver.mods, role)
	if err != nil {
		return nil, err
	}
	var edit jomini.Edit
	switch own := resolver.listOwn(); {
	case position > 0:
		layer, err := resolver.findLayer(position - 1)
		if err != nil {
			return nil, err
		}
		span := tree.Statements[layer.statement].Span
		end := tree.Lines.LineSpan(tree.Lines.ToPosition(span.End).Line).End
		edit = jomini.Edit{Span: jomini.Span{Start: end, End: end}, Text: jomini.LineEnding(tree.Src) + toIndented(tree, span.Start, lines)}
	case len(own) > 0:
		start := toLineSpan(tree, tree.Statements[own[0]].Span).Start
		edit = jomini.Edit{Span: jomini.Span{Start: start, End: start}, Text: toIndented(tree, start, lines) + jomini.LineEnding(tree.Src)}
	default:
		edit = tree.BuildInsertion(definition.statement, lines)
	}
	return lsp.ToEdits(tree.Src, tree.Lines, []jomini.Edit{edit}), nil
}

// isPattern reports whether a statement writes a coat's pattern.
func isPattern(field clause) bool {
	key, _, _ := splitField(field)
	return key == patternKey
}

// makeLayerLines returns the lines of the game's template for a layer with the given role. It returns an error for a
// role the template has no layer for, or a game with no texture to start one with.
func makeLayerLines(ws *workspace.Session, mods *index.Index, role string) ([]string, error) {
	template := game.Coat.Template
	switch {
	case role == patternKey:
		pattern, err := findTemplatePattern(ws, mods)
		return []string{fmt.Sprintf("%s = %q", patternKey, pattern)}, err
	case role == lo.FirstOr(strings.Fields(lo.FirstOr(template.EmblemLines, "")), ""):
		textures := listImages(ws, mods, game.Coat.TextureFolders[role], "")
		if len(textures) == 0 {
			return nil, fmt.Errorf("the game has no %s texture to start a layer with", role)
		}
		return lo.Map(template.EmblemLines, func(line string, _ int) string {
			return strings.ReplaceAll(line, "%s", path.Base(textures[0]))
		}), nil
	}
	return nil, fmt.Errorf("the game has no template for a layer with the role %q", role)
}

// findTemplatePattern returns the file of the pattern a new coat starts with: the first pattern whose name starts as
// the game's template says, else the first. It returns an error if the game has no pattern.
func findTemplatePattern(ws *workspace.Session, mods *index.Index) (string, error) {
	names := lo.Map(listImages(ws, mods, game.Coat.TextureFolders[patternKey], ""), func(rel string, _ int) string {
		return path.Base(rel)
	})
	if len(names) == 0 {
		return "", fmt.Errorf("the game has no pattern to start a coat with")
	}
	return lo.FindOrElse(names, names[0], func(each string) bool { return strings.HasPrefix(each, game.Coat.Template.Pattern) }), nil
}

// findLayer returns the clause of a layer of the coat asked for. It returns an error for a layer that does not exist,
// or that the coat has from a parent, in its file or another.
func (resolver *coatResolver) findLayer(layer int) (clause, error) {
	if layer < 0 || layer >= len(resolver.sources) {
		return clause{}, fmt.Errorf("the coat has no layer %d", layer)
	}
	if !resolver.isOwn(resolver.sources[layer]) {
		return clause{}, fmt.Errorf("the layer %d comes from a parent coat, in %q", layer, resolver.sources[layer].path)
	}
	return resolver.sources[layer], nil
}

// isOwn reports whether a layer is written in the block of the coat asked for.
func (resolver *coatResolver) isOwn(layer clause) bool {
	return layer.path == resolver.definition.path && layer.tree.Statements[layer.statement].Parent == resolver.definition.statement
}

// listOwn returns the statements of the layers that the coat writes in its own block, in the order they are written.
func (resolver *coatResolver) listOwn() []int32 {
	own := lo.FilterMap(resolver.sources, func(source clause, _ int) (int32, bool) {
		return source.statement, resolver.isOwn(source)
	})
	slices.Sort(own)
	return own
}

// MoveLayer moves one layer of a coat to a new position and returns the edits that do it.
func (drw *Service) MoveLayer(path, coat string, from, to int) ([]lsp.Edit, error) {
	resolver, err := drw.findCoat(path, coat, nil)
	if err != nil {
		return nil, err
	}
	resolver.resolveCoat()
	moved, err := resolver.findLayer(from)
	if err != nil {
		return nil, err
	}
	target, err := resolver.findLayer(to)
	if err != nil || from == to {
		return []lsp.Edit{}, err
	}
	tree := moved.tree
	cut := toLineSpan(tree, tree.Statements[moved.statement].Span)
	destination := toLineSpan(tree, tree.Statements[target.statement].Span)
	text := tree.Src[cut.Start:cut.End]
	if !strings.HasSuffix(text, "\n") {
		text += lo.Ternary(strings.HasSuffix(tree.Src[destination.Start:destination.End], "\n"), jomini.LineEnding(tree.Src), " ")
	}
	insertion := lo.Ternary(to < from, destination.Start, destination.End)
	edits := []jomini.Edit{{Span: cut}, {Span: jomini.Span{Start: insertion, End: insertion}, Text: text}}
	return lsp.ToEdits(tree.Src, tree.Lines, edits), nil
}

// RemoveLayer deletes one layer of a coat, including its line, and returns the edits that do it.
func (drw *Service) RemoveLayer(path, coat string, layer int) ([]lsp.Edit, error) {
	resolver, err := drw.findCoat(path, coat, nil)
	if err != nil {
		return nil, err
	}
	resolver.resolveCoat()
	removed, err := resolver.findLayer(layer)
	if err != nil {
		return nil, err
	}
	tree := removed.tree
	edit := jomini.Edit{Span: toLineSpan(tree, tree.Statements[removed.statement].Span)}
	return lsp.ToEdits(tree.Src, tree.Lines, []jomini.Edit{edit}), nil
}

// CreateCoatFile writes a new coat of arms file into a mod, from the game's coat template, and returns its path.
func (drw *Service) CreateCoatFile(modID, name string) (string, error) {
	ws, err := drw.Workspace.Session()
	if err != nil {
		return "", err
	}
	mod, ok := drw.Workspace.Config.LookupMod(ws.ID, modID)
	slug := files.ToSlug(name)
	if !ok || ws.Game.CoatFolder == "" || slug == "" {
		return "", fmt.Errorf("the open workspace has no mod %q that can hold a coat of arms named %q", modID, name)
	}
	target := files.Join(mod.Dir, ws.Game.CoatFolder+"/"+slug+".txt")
	if _, err := os.Stat(target); err == nil {
		return "", fmt.Errorf("the file %q exists already", target)
	}
	pattern, err := findTemplatePattern(ws, ws.Mods.Load())
	if err != nil {
		return "", err
	}
	lines := []string{slug + " = {", fmt.Sprintf("\t%s = %q", patternKey, pattern)}
	for position, color := range game.Coat.Template.Colors {
		lines = append(lines, fmt.Sprintf("\t%s = %q", game.Coat.Slots[position].Key, color))
	}
	text := files.Text{Text: strings.Join(append(lines, "}"), "\n") + "\n", Encoding: files.EncodingUTF8BOM}
	return target, drw.Workspace.Write(target, text)
}
