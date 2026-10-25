package draw

import (
	"cmp"
	"fmt"
	"maps"
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
	Rotation float64    `json:"rotation"` // in degrees, clockwise
	Depth    float64    `json:"depth"`
	Rank     int        `json:"rank"` // its place among every instance of its coat in the order they draw, from 0
	Range    lsp.Range  `json:"range"`
}

// A Blend is a texture channel that weights a blend toward a color.
type Blend struct {
	Channel string `json:"channel"` // the channel that weights the blend
	Color   string `json:"color"`   // the color blended in, as CSS
}

// A Recolor is how the game colors the texture of a layer, with the layer's colors as CSS.
type Recolor struct {
	Start   string            `json:"start"`   // the color it starts from
	Blends  []Blend           `json:"blends"`  // the blends toward a color, in order
	Overlay game.CoatStrength `json:"overlay"` // the channel overlaid on the result
	Alpha   string            `json:"alpha"`   // the channel that is its alpha; "" means the blends' own
}

// A Shading is the texture that shades the cloth of a coat, with how it lies over the coat and its emblems.
type Shading struct {
	Path    string            `json:"path"`    // the image file of the texture
	Overlay game.CoatStrength `json:"overlay"` // the channel overlaid on each coat and sub
	Grain   game.CoatStrength `json:"grain"`   // the channel times a factor over each emblem's alpha
}

// A Layer is one layer of a coat: the pattern, an emblem or a sub-coat.
type Layer struct {
	// Role is the key as written: "pattern", "colored_emblem", "textured_emblem" or "sub".
	Role string `json:"role"`
	// Texture is as written; "missing:x" when `list "x"` names a list that does not exist.
	Texture      string    `json:"texture"`
	Path         string    `json:"path"`         // the image file for the texture
	Emblem       bool      `json:"emblem"`       // the layer is an emblem, colored or textured
	Writer       string    `json:"writer"`       // the parent coat that writes the layer; "" for the coat's own
	Mask         string    `json:"mask"`         // the image file of the pattern whose regions an emblem shows in
	MaskRange    lsp.Range `json:"maskRange"`    // where the emblem's mask is written; zero where it writes none
	Regions      []int     `json:"regions"`      // the pattern's color regions an emblem shows in; none means all of them
	MaskChannels []string  `json:"maskChannels"` // the pattern channel of each of those regions; none without a mask
	Colors       []string  `json:"colors"`       // CSS colors, the fallback first and then one for each slot of the game
	ColorNames   []string  `json:"colorNames"`   // the named color of each of those, "" for one written as numbers
	Recolor      *Recolor  `json:"recolor"`      // how the game colors its texture; nil where it keeps its pixels
	Lists        []Group   `json:"lists"`        // the template lists its values are drawn from, each with its options
	// Inputs is how the panel edits each key the layer's role may write, by key.
	Inputs map[string]string `json:"inputs"`
	// Instances are in the order written. Each one's Rank says when the coat draws it.
	Instances []Instance      `json:"instances"`
	Sub       *Coat           `json:"sub,omitempty"`
	Range     lsp.Range       `json:"range"`
	Place     workspace.Place `json:"place"` // the file the layer is written in
}

// A Coat is a coat of arms resolved for drawing, as its layers.
type Coat struct {
	Name   string          `json:"name"`
	Layers []Layer         `json:"layers"`
	Range  lsp.Range       `json:"range"`
	Place  workspace.Place `json:"place"`
	// Width and Height are the size of its field in pixels; 0 for a sub-coat or where the game gives none.
	Width  int `json:"width"`
	Height int `json:"height"`
	// Shading is nil where the game shades no coat.
	Shading *Shading `json:"shading"`
	Regions []int    `json:"regions"` // the pattern's color regions, each one an emblem can show in
	Slots   []string `json:"slots"`   // the keys that write the color slots, slot 1 first
}

// An Image is one image file the panel offers, with its label.
type Image struct {
	Label string          `json:"label"`
	Place workspace.Place `json:"place"` // where the image file is
}

// A NamedColor is one of the game's named colors, for the color picker.
type NamedColor struct {
	Name string `json:"name"`
	CSS  string `json:"css"`
}

// A coatResolver makes the layers of one coat, with the option picked for each of its lists.
type coatResolver struct {
	ws         *workspace.Session
	mods       *index.Index
	picks      map[string]string // the option picked for a list, by the ID of its group
	definition clause            // the coat asked for
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
	tileSizeKey = "tile_size"
	listTag     = "list"
	noParent    = "none"
	missingList = "missing:"
	partCenter  = "center"
	partCorner  = "corner"
	partSize    = "size"
	partTurn    = "turn"
	partDepth   = "depth"
)

// ResolveCoat resolves one coat of arms in a file into its layers. A layer whose value is a template list draws the
// option the given picks hold for that list, else the list's heaviest option. A pick is the option chosen for the ID
// of a group. It returns an error if the file is not in the workspace or does not define the coat.
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
	mods := resolveIndex(ws, path)
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
		ws: ws, mods: mods, picks: picks, colors: map[string]string{},
		definition: definition,
	}, nil
}

// resolveCoat resolves the coat asked for into its layers, from its own statements and its parent's. A coat written as
// the name of another is that coat. A name that no coat answers has no layers.
func (resolver *coatResolver) resolveCoat() Coat {
	definition := resolver.definition
	coat := Coat{
		Name: definition.tree.Unquoted(definition.tree.Statements[definition.statement].Key), Layers: []Layer{},
		Range: toRange(definition), Place: resolver.ws.Locate(resolver.mods, definition.path),
		Shading: makeShading(resolver.ws, resolver.mods), Regions: lo.RangeFrom(1, len(game.Coat.Regions)),
		Slots: lo.Map(game.Coat.Slots, func(slot game.CoatSlot, _ int) string { return slot.Key }),
	}
	coat.Width, coat.Height = findFieldSize(resolver.ws, resolver.mods)
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

// makeShading returns the shading the game lays over its coats. It returns nil if the game shades none or no file has
// the shading's texture.
func makeShading(ws *workspace.Session, mods *index.Index) *Shading {
	shade := ws.Game.CoatShade
	texture, ok := mods.ResolveTexture(shade.Texture)
	if !ok {
		return nil
	}
	return &Shading{Path: texture, Overlay: shade.Overlay, Grain: shade.Grain}
}

// findFieldSize returns the width and the height in pixels of the field a coat is drawn on: the tile size of the first
// atlas the game lists. It returns 0 and 0 if the game lists none.
func findFieldSize(ws *workspace.Session, mods *index.Index) (width, height int) {
	loaded, _, ok := mods.FindLoaded(game.Coat.FieldFile)
	if !ok {
		return 0, 0
	}
	tree, err := ws.LoadTree(loaded.Path)
	if err != nil {
		return 0, 0
	}
	for statement := range tree.Children(0) {
		size, ok := findField(listChildren(clause{tree: tree, path: loaded.Path, statement: statement}), tileSizeKey)
		if !ok {
			continue
		}
		if numbers := listNumbers(size); len(numbers) > 0 {
			return int(numbers[0]), int(numbers[min(1, len(numbers)-1)])
		}
	}
	return 0, 0
}

// listCoatFields returns the statements that apply to a coat, in the order they apply: its parent's first, then its
// own. The parent's instances are left out, and so are its emblems where the coat writes emblems of its own. The given
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
	inherited := lo.FilterMap(resolver.listCoatFields(parent, depth+1), func(field clause, _ int) (clause, bool) {
		key, _, _ := splitField(field)
		field.writer = cmp.Or(field.writer, label)
		return field, key != instanceKey && (!emblematic || !isEmblem(field))
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

// listLayerFields returns the statements that write the layers of a coat, in the order the layers draw: the pattern,
// each emblem and, for the coat asked for only, each sub. The given depth is how many coats lie between the coat and
// the one asked for.
func listLayerFields(fields []clause, depth int) []clause {
	var layers []clause
	if pattern, ok := findField(fields, patternKey); ok {
		layers = append(layers, pattern)
	}
	for _, field := range fields {
		if isEmblem(field) {
			layers = append(layers, field)
		}
	}
	for _, field := range fields {
		if key, _, _ := splitField(field); key == subKey && depth == 0 {
			layers = append(layers, field)
		}
	}
	return layers
}

// resolveLayers returns the layers of a coat from its statements, in the order written, each instance ranked in the
// order the coat draws them. The given depth is how many coats lie between the coat and the one asked for.
func (resolver *coatResolver) resolveLayers(fields []clause, depth int) []Layer {
	colors, names, lists := resolver.resolveColors(fields, nil, nil)
	layers := []Layer{}
	patternFile := ""
	for _, field := range listLayerFields(fields, depth) {
		var layer Layer
		key, _, _ := splitField(field)
		switch key {
		case patternKey:
			layer = resolver.resolvePattern(field, colors, names, lists)
			patternFile = layer.Path
		case subKey:
			layer = resolver.resolveSub(field)
		default:
			layer = resolver.resolveEmblem(field, colors, names, patternFile)
		}
		layer.Recolor, layer.Inputs = makeRecolor(key, layer.Colors), listInputs(key)
		layer.MaskChannels = listMaskChannels(layer)
		layer.Place, layer.Writer = resolver.ws.Locate(resolver.mods, field.path), field.writer
		layers = append(layers, layer)
	}
	rankInstances(layers)
	return layers
}

// rankInstances sets each instance's rank in the order the game draws a coat's layers: every instance of every layer
// by its depth, a larger depth further back, and in the order written where depths are equal. A pattern has no
// instance and draws under them all.
func rankInstances(layers []Layer) {
	type spot struct{ layer, instance int }
	var spots []spot
	for layer := range layers {
		for instance := range layers[layer].Instances {
			spots = append(spots, spot{layer, instance})
		}
	}
	depth := func(one spot) float64 { return layers[one.layer].Instances[one.instance].Depth }
	slices.SortStableFunc(spots, func(first, second spot) int { return cmp.Compare(depth(second), depth(first)) })
	for rank, one := range spots {
		layers[one.layer].Instances[one.instance].Rank = rank
	}
}

// makeRecolor returns how the game colors the texture of a layer with the given key, from the layer's colors. It
// returns nil if the game keeps the pixels of such a texture.
func makeRecolor(key string, colors []string) *Recolor {
	row, ok := game.Coat.Recolors[key]
	if !ok {
		return nil
	}
	return &Recolor{
		Start: lo.NthOr(colors, row.Start, ""),
		Blends: lo.Map(row.Lerps, func(lerp game.CoatLerp, _ int) Blend {
			return Blend{Channel: lerp.Channel, Color: lo.NthOr(colors, lerp.Slot, "")}
		}),
		Overlay: row.Overlay, Alpha: row.Shape,
	}
}

// listMaskChannels returns the pattern channel of each region a layer shows in, all of them if it names none. It
// returns none for a layer with no mask.
func listMaskChannels(layer Layer) []string {
	if layer.Mask == "" {
		return []string{}
	}
	if len(layer.Regions) == 0 {
		return slices.Clone(game.Coat.Regions)
	}
	return lo.FilterMap(layer.Regions, func(region, _ int) (string, bool) {
		return lo.NthOr(game.Coat.Regions, region-1, ""), region >= 1 && region <= len(game.Coat.Regions)
	})
}

// listInputs returns how the panel edits each key a layer with the given role may write, including the keys inside its
// instances. A key the game gives no input is left out.
func listInputs(role string) map[string]string {
	keys := []string{role}
	if _, colored := game.Coat.Recolors[role]; colored {
		keys = append(keys, lo.Map(game.Coat.Slots, func(slot game.CoatSlot, _ int) string { return slot.Key })...)
	}
	if _, placed := game.Coat.Template.Instances[role]; placed {
		row := subKey
		if role != subKey {
			row = instanceKey
			keys = append(keys, textureKey, maskKey)
		}
		keys = append(keys, slices.Collect(maps.Keys(game.Coat.Placement[row]))...)
	}
	inputs := map[string]string{}
	for _, key := range keys {
		if input, ok := game.Coat.Inputs[key]; ok {
			inputs[key] = input
		}
	}
	return inputs
}

// resolvePattern returns the layer of a coat's pattern, painted in the coat's colors, with their names. Its lists are
// the ones its texture and the coat's colors are drawn from.
func (resolver *coatResolver) resolvePattern(field clause, colors, names []string, lists []Group) Layer {
	texture, own := resolver.resolveValue(field)
	return Layer{
		Role: field.tree.Unquoted(field.tree.Statements[field.statement].Key), Texture: texture,
		Path: resolver.findCoatFile(patternKey, texture), Colors: colors, ColorNames: names, Lists: slices.Concat(own, lists),
		Regions: []int{}, Instances: []Instance{}, Range: toRange(field),
	}
}

// resolveEmblem returns the layer of an emblem: its texture, its colors over the coat's colors and their names, its
// mask and its instances. The mask is the coat's pattern file, with the regions the emblem shows in.
func (resolver *coatResolver) resolveEmblem(field clause, colors, names []string, patternFile string) Layer {
	children := listChildren(field)
	key, _, _ := splitField(field)
	emblemColors, emblemNames, lists := resolver.resolveColors(children, colors, names)
	layer := Layer{
		Role: field.tree.Unquoted(field.tree.Statements[field.statement].Key), Emblem: true, Colors: emblemColors,
		ColorNames: emblemNames, Lists: lists, Regions: []int{},
		Instances: resolver.resolveInstances(children, instanceKey), Range: toRange(field),
	}
	if texture, ok := findField(children, textureKey); ok {
		var own []Group
		layer.Texture, own = resolver.resolveValue(texture)
		layer.Path, layer.Lists = resolver.findCoatFile(key, layer.Texture), slices.Concat(own, lists)
	}
	if mask, ok := findField(children, maskKey); ok {
		layer.Mask, layer.MaskRange = patternFile, toRange(mask)
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
		Place: resolver.ws.Locate(resolver.mods, field.path),
	}
	return Layer{
		Role: field.tree.Unquoted(field.tree.Statements[field.statement].Key), Texture: parent, Colors: []string{},
		ColorNames: []string{}, Lists: []Group{},
		Regions: []int{}, Sub: &sub, Range: sub.Range, Instances: resolver.resolveInstances(children, subKey),
	}
}

// resolveValue returns the text a statement of a coat writes as its value, and the list it draws from. A value written
// as a list is the option picked for that list, else its heaviest option. It is "missing:x" if no list is named x.
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
	file, _ := resolver.mods.ResolveTexture(game.Coat.TextureFolders[key] + "/" + texture)
	return file
}

// resolveColors returns the colors a coat's or an emblem's statements give its slots as CSS, with the fallback at 0,
// the named color of each ("" for one written as numbers), and the lists they are drawn from. A slot that is not
// written has the base's color and name.
func (resolver *coatResolver) resolveColors(fields []clause, base, baseNames []string) ([]string, []string, []Group) {
	colors := make([]string, len(game.Coat.Slots)+1)
	names := make([]string, len(colors))
	colors[0] = game.Coat.Fallback
	var lists []Group
	for position, slot := range game.Coat.Slots {
		colors[position+1], names[position+1] = lo.NthOr(base, position+1, ""), lo.NthOr(baseNames, position+1, "")
		if field, ok := findField(fields, slot.Key); ok {
			var own []Group
			colors[position+1], names[position+1], own = resolver.resolveColor(field, base, baseNames)
			lists = append(lists, own...)
		}
	}
	return colors, names, lists
}

// resolveColor returns the CSS of the color a statement writes and the named color's name: a color block, which has
// no name, the name of a named color, or the name of a slot, which is the base's color and name in it. It also
// returns the list the color is drawn from.
func (resolver *coatResolver) resolveColor(field clause, base, baseNames []string) (string, string, []Group) {
	if css := makeColor(field); css != "" {
		return css, "", nil
	}
	label, lists := resolver.resolveValue(field)
	if position := slices.IndexFunc(game.Coat.Slots, func(slot game.CoatSlot) bool {
		return strings.EqualFold(slot.Key, label)
	}); position >= 0 {
		return lo.NthOr(base, position+1, ""), lo.NthOr(baseNames, position+1, ""), lists
	}
	css, ok := resolver.colors[label]
	if !ok {
		if definition, ok := findKeptClause(resolver.ws, resolver.mods, schema.Reference{Database: game.Coat.ColorsDatabase, Name: label}); ok {
			css = makeColor(definition)
		}
		resolver.colors[label] = css
	}
	return css, label, lists
}

// makeColor returns the CSS of the color that a clause writes as a color block, such as a named color's definition. It
// returns "" if the clause writes no color.
func makeColor(definition clause) string {
	color, ok := game.ToColor(definition.tree, definition.statement)
	if !ok {
		return ""
	}
	return color.ToCSS()
}

// resolveInstances returns the instances the given statements write, in the given row of the game's placements, in the
// order written. Where they write none, it returns the one default instance the game draws, which has no range.
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
		setPlacement(&instance, game.Coat.Placement[row][key].Part, listNumbers(child))
	}
	return instance
}

// setPlacement sets the part of an instance that the given part of a placement moves, sizes or turns, to the given
// numbers. A turn is set in degrees clockwise. An unknown part, or no numbers, sets nothing.
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

// listNumbers returns the numbers a statement writes: its value and the bare values inside its block, each as the
// file stands for it. It returns none if a value is no number.
func listNumbers(field clause) []float64 {
	current := field.tree.Statements[field.statement]
	spans := []jomini.Span{current.Value}
	for statement := range field.tree.Children(field.statement) {
		if inner := field.tree.Statements[statement]; inner.IsListed() {
			spans = append(spans, inner.Value)
		}
	}
	var numbers []float64
	for _, span := range spans {
		if span.IsEmpty() {
			continue
		}
		number, ok := field.tree.ResolveNumber(span)
		if !ok {
			return nil
		}
		numbers = append(numbers, number)
	}
	return numbers
}

// ListColors returns the game's named colors, for the color picker. It returns an empty list if no workspace is open.
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

// ListTextures returns the texture files that a layer with the given role can use, from the game's coat folders. Each
// is labeled with the name a coat writes for it. It returns an empty list if no workspace is open or the game's coats
// have no folder for the role.
func (drw *Service) ListTextures(role string) []Image {
	ws, err := drw.Workspace.Session()
	if err != nil {
		return []Image{}
	}
	return listImages(ws, ws.Mods.Load(), game.Coat.TextureFolders[role])
}

// ListFrames returns the shapes the game's coat designer previews a coat in, in the designer's order, each with where
// its mask is. A frame is left out if its define is not defined or the game loads no file at its mask's path. It is
// empty if no workspace is open or the game has no designer.
func (drw *Service) ListFrames() []Image {
	frames := []Image{}
	ws, err := drw.Workspace.Session()
	if err != nil {
		return frames
	}
	mods := ws.Mods.Load()
	for _, row := range ws.Game.CoatFrames {
		mask := row.Mask
		if row.Define != "" {
			define, ok := findKeptClause(ws, mods, schema.Reference{Database: lookupDefineDatabase(), Name: row.Define})
			if !ok {
				continue
			}
			mask = fmt.Sprintf(game.Coat.FrameMask, define.tree.Unquoted(define.tree.Statements[define.statement].Value))
		}
		if file, ok := mods.ResolveTexture(mask); ok {
			frames = append(frames, Image{Label: row.Label, Place: ws.Locate(mods, file)})
		}
	}
	return frames
}

// listImages returns the image files the game reads directly in a folder, each labeled with its file name, sorted by
// label. It returns an empty list if the folder has none.
func listImages(ws *workspace.Session, mods *index.Index, folder string) []Image {
	images := []Image{}
	for _, file := range mods.ListImages(folder + "/") {
		if _, rest := schema.SplitStage(ws.Game, file.Rel); path.Dir(rest) == folder {
			images = append(images, Image{Label: path.Base(rest), Place: ws.Locate(mods, file.Path)})
		}
	}
	slices.SortFunc(images, func(first, second Image) int { return cmp.Compare(first.Label, second.Label) })
	return slices.CompactFunc(images, func(first, second Image) bool { return first.Label == second.Label })
}

// AddLayer inserts a new layer into a coat at the given position among its layers, 0 being first, and returns the
// edits that do it. The new layer is the game's template for the given role. It returns an error if the file does not
// define the coat, the coat is written as the name of another, or a pattern is added to a coat that has one. It also
// returns an error if the game has no template for the role or nothing to start the layer from, or if the layer before
// the position does not exist or comes from a parent coat.
func (drw *Service) AddLayer(path, coat, role string, position int) ([]lsp.Edit, error) {
	resolver, err := drw.findCoat(path, coat, nil)
	if err != nil {
		return nil, err
	}
	definition := resolver.definition
	tree := definition.tree
	if !tree.Statements[definition.statement].IsBlock() {
		return nil, fmt.Errorf("the coat %q is written as the name of another coat", coat)
	}
	layers := resolver.listLayers()
	if role == patternKey && slices.ContainsFunc(layers, isPattern) {
		return nil, fmt.Errorf("the coat %q has a pattern already", coat)
	}
	lines, err := makeLayerLines(resolver.ws, resolver.mods, role, coat)
	if err != nil {
		return nil, err
	}
	var edit jomini.Edit
	switch own := resolver.listOwn(layers); {
	case position > 0:
		layer, err := resolver.findLayer(layers, position-1)
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

// makeLayerLines returns the lines of the game's template for a layer with the given role. The layer starts from the
// first texture of the role, or for a sub from the first coat other than the given one. It returns an error for a role
// the template has no layer for, or a game with nothing to start one from.
func makeLayerLines(ws *workspace.Session, mods *index.Index, role, coat string) ([]string, error) {
	if role == patternKey {
		pattern, err := findTemplatePattern(ws, mods)
		return []string{fmt.Sprintf("%s = %q", patternKey, pattern)}, err
	}
	lines, ok := game.Coat.Template.Layers[role]
	if !ok {
		return nil, fmt.Errorf("the game has no template for a layer with the role %q", role)
	}
	fill, err := findTemplateFill(ws, mods, role, coat)
	if err != nil {
		return nil, err
	}
	return lo.Map(lines, func(line string, _ int) string { return strings.ReplaceAll(line, "%s", fill) }), nil
}

// findTemplateFill returns what a new layer with the given role starts from: for a sub, the first coat by name other
// than the given one, else the first texture file of the role. It returns an error if the game has none.
func findTemplateFill(ws *workspace.Session, mods *index.Index, role, coat string) (string, error) {
	if role == subKey {
		names := lo.FilterMap(mods.ListKept(ws.Schema, game.Coat.Database), func(winner index.Winner, _ int) (string, bool) {
			name := mods.ToReference(winner.Site).Name
			return name, !strings.EqualFold(name, coat)
		})
		if len(names) == 0 {
			return "", fmt.Errorf("the game has no other coat to start a sub with")
		}
		return slices.Min(names), nil
	}
	textures := listImages(ws, mods, game.Coat.TextureFolders[role])
	if len(textures) == 0 {
		return "", fmt.Errorf("the game has no %s texture to start a layer with", role)
	}
	return textures[0].Label, nil
}

// findTemplatePattern returns the file of the pattern a new coat starts with: the first pattern whose name starts as
// the game's template says, else the first. It returns an error if the game has no pattern.
func findTemplatePattern(ws *workspace.Session, mods *index.Index) (string, error) {
	names := lo.Map(listImages(ws, mods, game.Coat.TextureFolders[patternKey]), func(image Image, _ int) string {
		return image.Label
	})
	if len(names) == 0 {
		return "", fmt.Errorf("the game has no pattern to start a coat with")
	}
	return lo.FindOrElse(names, names[0], func(each string) bool { return strings.HasPrefix(each, game.Coat.Template.Pattern) }), nil
}

// listLayers returns the statements that write the layers of the coat asked for, in the order the layers draw, the
// ones it has from its parents included.
func (resolver *coatResolver) listLayers() []clause {
	return listLayerFields(resolver.listCoatFields(resolver.definition, 0), 0)
}

// findLayer returns the statement of one of the given layers of the coat asked for. It returns an error for a layer
// that does not exist, or that the coat has from a parent, in its file or another.
func (resolver *coatResolver) findLayer(layers []clause, layer int) (clause, error) {
	if layer < 0 || layer >= len(layers) {
		return clause{}, fmt.Errorf("the coat has no layer %d", layer)
	}
	if !resolver.isOwn(layers[layer]) {
		return clause{}, fmt.Errorf("the layer %d comes from a parent coat, in %q", layer, layers[layer].path)
	}
	return layers[layer], nil
}

// isOwn reports whether a layer is written in the block of the coat asked for.
func (resolver *coatResolver) isOwn(layer clause) bool {
	return layer.path == resolver.definition.path && layer.tree.Statements[layer.statement].Parent == resolver.definition.statement
}

// listOwn returns the statements of the given layers that the coat writes in its own block, in the order they are
// written.
func (resolver *coatResolver) listOwn(layers []clause) []int32 {
	own := lo.FilterMap(layers, func(layer clause, _ int) (int32, bool) {
		return layer.statement, resolver.isOwn(layer)
	})
	slices.Sort(own)
	return own
}

// MoveLayer moves one layer of a coat to another position among its layers and returns the edits that do it. It
// returns no edits if both positions are the same. It returns an error if either position has no layer or has a layer
// that comes from a parent coat.
func (drw *Service) MoveLayer(path, coat string, from, to int) ([]lsp.Edit, error) {
	resolver, err := drw.findCoat(path, coat, nil)
	if err != nil {
		return nil, err
	}
	layers := resolver.listLayers()
	moved, err := resolver.findLayer(layers, from)
	if err != nil {
		return nil, err
	}
	target, err := resolver.findLayer(layers, to)
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

// RemoveBlock deletes one layer or instance of a coat, including its line, and returns the edits that do it. It
// returns an error if the file is not in the workspace or no statement starts where the range does.
func (drw *Service) RemoveBlock(path string, block lsp.Range) ([]lsp.Edit, error) {
	tree, statement, err := drw.findStatement(path, block)
	if err != nil {
		return nil, err
	}
	edit := jomini.Edit{Span: toLineSpan(tree, tree.Statements[statement].Span)}
	return lsp.ToEdits(tree.Src, tree.Lines, []jomini.Edit{edit}), nil
}

// CreateCoatFile writes a new coat of arms file into the given mod and returns its path. The coat is named after the
// given text as a slug, and holds the given script, as the game's coat designer copies it, or the game's coat template
// when the script is "". It returns an error if the workspace has no such mod, the game keeps coats of arms in no
// folder, the text has no slug, the file exists already or the game has no pattern.
func (drw *Service) CreateCoatFile(modID, name, script string) (string, error) {
	ws, err := drw.Workspace.Session()
	if err != nil {
		return "", err
	}
	slug := files.ToSlug(name)
	if script = strings.TrimSpace(script); script != "" {
		return drw.writeCoatFile(ws, modID, slug, fmt.Sprintf("%s = {\n%s\n}", slug, script))
	}
	pattern, err := findTemplatePattern(ws, ws.Mods.Load())
	if err != nil {
		return "", err
	}
	lines := []string{slug + " = {", fmt.Sprintf("\t%s = %q", patternKey, pattern)}
	for position, color := range game.Coat.Template.Colors {
		lines = append(lines, fmt.Sprintf("\t%s = %q", game.Coat.Slots[position].Key, color))
	}
	return drw.writeCoatFile(ws, modID, slug, strings.Join(append(lines, "}"), "\n"))
}

// CopyCoat copies a coat that a file defines into the given mod, as a new coat of arms file with the coat's own name,
// and returns its path. It returns an error if the file does not define the coat, or as CreateCoatFile does.
func (drw *Service) CopyCoat(path, name, modID string) (string, error) {
	resolver, err := drw.findCoat(path, name, nil)
	if err != nil {
		return "", err
	}
	definition := resolver.definition
	text := definition.tree.Text(definition.tree.Statements[definition.statement].Span)
	return drw.writeCoatFile(resolver.ws, modID, files.ToSlug(name), text)
}

// writeCoatFile writes the given coat text as a file named after the given slug in the coat folder of the given mod,
// and returns its path. It returns an error if the workspace has no such mod, the game keeps coats of arms in no
// folder, the slug is "" or the file exists already.
func (drw *Service) writeCoatFile(ws *workspace.Session, modID, slug, coat string) (string, error) {
	mod, ok := drw.Workspace.LookupMod(modID)
	if !ok || ws.Game.CoatFolder == "" || slug == "" {
		return "", fmt.Errorf("the open workspace has no mod %q that can hold a coat of arms named %q", modID, slug)
	}
	target := files.Join(mod.Dir, ws.Game.CoatFolder+"/"+slug+".txt")
	if _, err := os.Stat(target); err == nil {
		return "", fmt.Errorf("the file %q exists already", target)
	}
	return target, drw.Workspace.Write(target, files.Text{Text: coat + "\n", Encoding: files.EncodingUTF8BOM})
}
