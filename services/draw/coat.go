// coat.go resolves a coat of arms into the layers the Visual view draws, and
// makes the coat panel's edits.

package draw

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/services/lsp"
	"paradox-modding-tools/services/workspace"
)

// Instance is one placement of a layer on the field. A value the file does
// not set takes the game's default.
type Instance struct {
	Position [2]float64 `json:"position"`
	Scale    [2]float64 `json:"scale"`

	// Rotation is in degrees, clockwise.
	Rotation float64 `json:"rotation"`

	Depth float64 `json:"depth"`

	// Rank is the instance's place, from 0, among all of its coat's instances
	// in the order they draw.
	Rank int `json:"rank"`

	Range jomini.Range `json:"range"`
}

// CoatStrength is a texture channel taken at a strength.
type CoatStrength struct {
	// Channel is r, g or b.
	Channel string `json:"channel"`

	// Strength is how much of the channel is taken; 0 takes none.
	Strength float64 `json:"strength"`
}

// Blend is a texture channel that weights a blend toward a CSS color.
type Blend struct {
	Channel string `json:"channel"`
	Color   string `json:"color"`
}

// Recolor is how the game colors a layer's texture, with the layer's colors
// as CSS.
type Recolor struct {
	// Start is the color it starts from, and Blends the blends toward a
	// color, in order.
	Start  string  `json:"start"`
	Blends []Blend `json:"blends"`

	// Overlay is the channel overlaid on the result.
	Overlay CoatStrength `json:"overlay"`

	// Alpha is the channel that is its alpha, "" for the blends' own.
	Alpha string `json:"alpha"`
}

// Shading is the texture that shades the cloth of a coat.
type Shading struct {
	Path string `json:"path"`

	// Overlay is the channel overlaid on each coat and sub, and Grain the
	// channel laid times a factor over each emblem's alpha.
	Overlay CoatStrength `json:"overlay"`
	Grain   CoatStrength `json:"grain"`
}

// Layer is one layer of a coat: the pattern, an emblem or a sub-coat.
type Layer struct {
	// Role is the layer's key in lower case: "pattern", "colored_emblem",
	// "textured_emblem" or "sub".
	Role string `json:"role"`

	// Texture is as written, or "missing:x" where `list "x"` names a list
	// that does not exist. Path is its image file.
	Texture string `json:"texture"`
	Path    string `json:"path"`

	// Emblem is set for a colored or textured emblem.
	Emblem bool `json:"emblem"`

	// Writer is the parent coat that writes the layer, "" for the coat's own.
	Writer string `json:"writer"`

	// Mask is the image file of the pattern whose regions an emblem shows
	// in. MaskRange is where the emblem writes its mask, zero for nowhere.
	Mask      string       `json:"mask"`
	MaskRange jomini.Range `json:"maskRange"`

	// Regions are the pattern's color regions an emblem shows in, none
	// meaning all. MaskChannels holds the pattern channel of each, none
	// without a mask.
	Regions      []int    `json:"regions"`
	MaskChannels []string `json:"maskChannels"`

	// Colors holds CSS colors, the fallback first, then one per slot.
	// ColorNames holds the named color of each, "" for one written as
	// numbers.
	Colors     []string `json:"colors"`
	ColorNames []string `json:"colorNames"`

	// Recolor is nil where the texture keeps its pixels.
	Recolor *Recolor `json:"recolor"`

	// Lists are the template lists the layer's values are drawn from.
	Lists []Group `json:"lists"`

	// Inputs says, by key, how the panel edits each key the layer's role may
	// write.
	Inputs map[string]string `json:"inputs"`

	// Instances are in the order written; each one's Rank says when it
	// draws.
	Instances []Instance `json:"instances"`

	Sub   *Coat           `json:"sub,omitempty"`
	Range jomini.Range    `json:"range"`
	Place workspace.Place `json:"place"`
}

// Coat is a coat of arms resolved for drawing, as its layers.
type Coat struct {
	Name   string          `json:"name"`
	Layers []Layer         `json:"layers"`
	Range  jomini.Range    `json:"range"`
	Place  workspace.Place `json:"place"`

	// Body is the range inside the coat's braces, zero for a sub-coat and for
	// a coat written as another's name.
	Body jomini.Range `json:"body"`

	// Width and Height are its field's size in pixels, 0 for a sub-coat or
	// where the game gives none.
	Width  int `json:"width"`
	Height int `json:"height"`

	// Shading is nil where the game shades no coat.
	Shading *Shading `json:"shading"`

	// Regions are the pattern's color regions an emblem can show in.
	Regions []int `json:"regions"`

	// Slots are the keys of the color slots, slot 1 first.
	Slots []string `json:"slots"`
}

// Image is one image file the panel offers, with its label.
type Image struct {
	Label string          `json:"label"`
	Place workspace.Place `json:"place"`
}

// NamedColor is one of the game's named colors, for the color picker.
type NamedColor struct {
	Name string `json:"name"`
	CSS  string `json:"css"`
}

// coatResolver makes the layers of one coat, with the option picked for each
// of its lists.
type coatResolver struct {
	ws   *workspace.Session
	mods *index.Index

	// picks holds the option picked for a list, by its group's ID.
	picks map[string]string

	// definition is the coat asked for.
	definition clause

	// colors caches the CSS of the named colors looked up, by name.
	colors map[string]string
}

// coatLerp is a texture channel that blends a color slot in.
type coatLerp struct {
	Channel string
	Slot    int
}

// coatRecolor is how the textures of one texture key are colored.
type coatRecolor struct {
	// Start is the slot it starts from, 0 for the fallback color.
	Start int

	// Lerps are the blends toward a slot, in order.
	Lerps []coatLerp

	// Overlay is the channel overlaid on the result.
	Overlay CoatStrength

	// Shape is the channel that is its alpha, "" for the blends' own.
	Shape string
}

// coatPlace is what one placement key does to a layer.
type coatPlace struct {
	Part coatPart

	// Defaults are the values taken where the layer writes none.
	Defaults []float64
}

// coatPart is what one placement key sets on a layer's instance. The zero
// coatPart sets nothing.
type coatPart uint8

// The parts a placement key sets: the place, which is an emblem's center and
// a sub's corner, the size, the turn and the depth.
const (
	coatPartPlace coatPart = iota + 1
	coatPartSize
	coatPartTurn
	coatPartDepth
)

// coatFrame is a shape a game's coat designer previews a coat in.
type coatFrame struct {
	Label string

	// Mask is the mask file, "" where a define names the frame.
	Mask string

	// Define is the define in coatDefineGroup that names the frame whose
	// mask is drawn.
	Define string
}

// The keys of a coat's statements.
const (
	// instanceKey is the key of one placement of an emblem or a sub, and
	// placement's row for an emblem.
	instanceKey = "instance"

	// subKey is the key of a sub-coat, and placement's row for a sub.
	subKey = "sub"

	// parentKey names the coat a coat or a sub is laid over.
	parentKey = "parent"

	// maskKey is the key of the pattern's regions an emblem shows in.
	maskKey = "mask"
)

// missingList starts what a template list that does not exist is shown as.
const missingList = "missing:"

// recolors holds how each texture key's files are colored. A key with no row
// keeps their pixels.
var recolors = map[string]coatRecolor{
	"pattern":        {Start: 0, Lerps: []coatLerp{{"r", 1}, {"g", 2}, {"b", 3}}, Overlay: CoatStrength{Channel: "b"}},
	"colored_emblem": {Start: 1, Lerps: []coatLerp{{"g", 2}, {"r", 3}}, Overlay: CoatStrength{Channel: "b", Strength: 1}, Shape: "a"},
}

// regions are the pattern channel of each mask region, region n at n-1.
var regions = []string{"r", "g", "b"}

// placement holds what a placed layer's keys do to it, by the layer's type.
var placement = map[string]map[string]coatPlace{
	instanceKey: {
		"position": {Part: coatPartPlace, Defaults: []float64{0.5, 0.5}},
		"scale":    {Part: coatPartSize, Defaults: []float64{1, 1}},
		"rotation": {Part: coatPartTurn, Defaults: []float64{0}},
		// only CK3 documents the default depth; Vic3 and EU5 are taken to
		// share it
		"depth": {Part: coatPartDepth, Defaults: []float64{0.01}},
	},
	subKey: {
		"offset": {Part: coatPartPlace, Defaults: []float64{0, 0}},
		"scale":  {Part: coatPartSize, Defaults: []float64{1, 1}},
	},
}

// coatInstance is the instance a new emblem is written with: centered, at
// half size, on one line.
const coatInstance = "instance = { position = { 0.5 0.5 } scale = { 0.5 0.5 } }"

// coatSubInstance is the instance a new sub is written with: a quarter of
// the field in from its corner, at half size, on one line.
const coatSubInstance = "instance = { offset = { 0.25 0.25 } scale = { 0.5 0.5 } }"

// templateInstances are the new instance the coat panel writes for each
// placed role.
var templateInstances = map[string]string{
	"colored_emblem": coatInstance, "textured_emblem": coatInstance, subKey: coatSubInstance,
}

// shades holds how each game shades every coat, its Path the texture as the
// game names it. A game with no row shades none.
var shades = map[string]Shading{
	game.CK3: {
		Path:    "gfx/coat_of_arms/coa_mask_texture.dds",
		Overlay: CoatStrength{Channel: "b", Strength: 0.2},
		Grain:   CoatStrength{Channel: "g", Strength: 2},
	},
}

// ResolveCoat resolves one coat of arms of a file into its layers. A coat
// written as another's name is that coat, and one whose name no coat answers
// has no layers. The picks are the option chosen for each list, by its
// group's ID. It returns an error if the file does not define the coat.
func (s *Service) ResolveCoat(path, name string, picks map[string]string) (Coat, error) {
	resolver, err := s.findCoat(path, name, picks)
	if err != nil {
		return Coat{}, err
	}

	definition := resolver.definition
	own := definition.tree.Statements[definition.statement]
	coat := Coat{
		Name: definition.tree.Unquoted(own.Key), Layers: []Layer{},
		Range: toRange(definition), Place: resolver.ws.Locate(resolver.mods, definition.path),
		Regions: make([]int, len(regions)), Slots: slices.Clone(lang.CoatSlots),
	}
	for position := range coat.Regions {
		coat.Regions[position] = position + 1
	}

	if own.IsBlock() {
		body := jomini.Span{Start: own.Block.Start + 1, End: own.Block.End - 1}
		coat.Body = jomini.ToRange(definition.tree.Src, definition.tree.Lines, body)
	}

	shade := shades[resolver.ws.Game.ID]
	if texture, ok := resolver.mods.ResolveTexture(shade.Path); ok {
		shade.Path, coat.Shading = texture, &shade
	}

	coat.Width, coat.Height = findFieldSize(resolver.ws, resolver.mods)
	root := definition
	for range maxDepth {
		current := root.tree.Statements[root.statement]
		if current.IsBlock() {
			coat.Layers = resolver.resolveLayers(resolver.listCoatFields(root, 0), 0)

			return coat, nil
		}

		reference := lang.Reference{Database: lang.CoatDatabase, Name: root.tree.Unquoted(current.Value)}
		target, ok := findKeptClause(resolver.ws, resolver.mods, reference)
		if !ok {
			return coat, nil
		}

		root = target
	}

	return coat, nil
}

// findCoat returns the resolver of the coat a file defines under a name. It
// returns an error if the file does not define the coat.
func (s *Service) findCoat(path, name string, picks map[string]string) (*coatResolver, error) {
	ws, err := s.Workspace.Session()
	if err != nil {
		return nil, err
	}

	mods := resolveIndex(ws, path)
	isCoat := func(database string) bool { return database == lang.CoatDatabase }
	definition, _, err := findDrawn(ws, mods, path, name, isCoat)
	if err != nil {
		return nil, err
	}

	return &coatResolver{ws: ws, mods: mods, picks: picks, colors: map[string]string{}, definition: definition}, nil
}

// fieldFile is the file whose first atlas' tile_size sizes the field.
const fieldFile = "common/coat_of_arms/options/atlases.txt"

// tileSizeKey is the key of an atlas' tile size in fieldFile.
const tileSizeKey = "tile_size"

// findFieldSize returns the size in pixels of the field a coat is drawn on,
// or 0 and 0 if the game lists none.
func findFieldSize(ws *workspace.Session, mods *index.Index) (width, height int) {
	loaded, _, ok := mods.FindLoaded(fieldFile)
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
			pair := toPair(numbers)

			return int(pair[0]), int(pair[1])
		}
	}

	return 0, 0
}

// noParent is the parent that names no coat.
const noParent = "none"

// listCoatFields returns the statements that apply to a coat: its parent's
// first, then its own. The parent's instances are left out, and so are its
// emblems where the coat writes its own. depth counts the parents between
// the coat and the one asked for.
func (r *coatResolver) listCoatFields(coat clause, depth int) []clause {
	own := listChildren(coat)
	parentField, ok := findField(own, parentKey)
	if !ok || depth >= maxDepth {
		return own
	}

	_, label, _ := splitField(parentField)
	if strings.EqualFold(label, noParent) {
		return own
	}

	parent, ok := findKeptClause(r.ws, r.mods, lang.Reference{Database: lang.CoatDatabase, Name: label})
	if !ok {
		return own
	}

	var inherited []clause
	hasEmblems := slices.ContainsFunc(own, isEmblem)
	for _, field := range r.listCoatFields(parent, depth+1) {
		if key, _, _ := splitField(field); key == instanceKey || hasEmblems && isEmblem(field) {
			continue
		}

		field.writer = cmp.Or(field.writer, label)
		inherited = append(inherited, field)
	}

	return append(inherited, own...)
}

// listChildren returns the statements written directly inside a clause.
func listChildren(holder clause) []clause {
	var children []clause
	for statement := range holder.tree.Children(holder.statement) {
		children = append(children, clause{tree: holder.tree, path: holder.path, statement: statement})
	}

	return children
}

// splitField returns a statement's key in lower case, its value without
// quotes, and its tag.
func splitField(field clause) (key, value, tag string) {
	current := field.tree.Statements[field.statement]

	return strings.ToLower(field.tree.Unquoted(current.Key)), field.tree.Unquoted(current.Value), field.tree.Text(current.Tag)
}

// findField returns the last of the statements with a key, or false if none
// has it.
func findField(fields []clause, key string) (clause, bool) {
	for _, field := range slices.Backward(fields) {
		if candidate, _, _ := splitField(field); candidate == key {
			return field, true
		}
	}

	return clause{}, false
}

// isEmblem reports whether a statement writes an emblem.
func isEmblem(field clause) bool {
	key, _, _ := splitField(field)
	_, hasFolder := lang.CoatTextureFolders[key]

	return hasFolder && key != lang.CoatPatternKey
}

// resolveLayers returns the layers a coat's statements write, each instance
// ranked in the order the coat draws them. depth counts the coats between
// this one and the one asked for.
func (r *coatResolver) resolveLayers(fields []clause, depth int) []Layer {
	colors, names, lists := r.resolveColors(fields, nil, nil)
	layers := []Layer{}
	patternFile := ""
	for _, field := range listLayerFields(fields, depth) {
		var layer Layer
		key, _, _ := splitField(field)
		switch key {
		case lang.CoatPatternKey:
			texture, own := r.resolveValue(field)
			layer = Layer{
				Role: key, Texture: texture, Path: r.findCoatFile(key, texture), Colors: colors, ColorNames: names,
				Lists: slices.Concat(own, lists), Regions: []int{}, Instances: []Instance{}, Range: toRange(field),
			}
			patternFile = layer.Path

		case subKey:
			layer = r.resolveSub(field)

		default:
			layer = r.resolveEmblem(field, colors, names, patternFile)
		}

		// a recolored layer has a color for each slot its row names
		if row, ok := recolors[key]; ok {
			recolor := Recolor{Start: layer.Colors[row.Start], Blends: make([]Blend, len(row.Lerps)), Overlay: row.Overlay, Alpha: row.Shape}
			for position, lerp := range row.Lerps {
				recolor.Blends[position] = Blend{Channel: lerp.Channel, Color: layer.Colors[lerp.Slot]}
			}

			layer.Recolor = &recolor
		}

		layer.Inputs, layer.MaskChannels = listInputs(key), listMaskChannels(layer)
		layer.Place, layer.Writer = r.ws.Locate(r.mods, field.path), field.writer
		layers = append(layers, layer)
	}

	rankInstances(layers)

	return layers
}

// listLayerFields returns the statements that write a coat's layers in the
// order they draw: the pattern, the emblems, then the subs. A sub is drawn
// only in the coat asked for, whose depth is 0.
func listLayerFields(fields []clause, depth int) []clause {
	var layers []clause
	if pattern, ok := findField(fields, lang.CoatPatternKey); ok {
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

// coatInputs holds how the panel edits each coat key's value.
var coatInputs = map[string]string{
	"pattern": "texture", "texture": "texture",
	"color1": "color", "color2": "color", "color3": "color", "color4": "color", "color5": "color",
	"position": "pair", "scale": "pair", "offset": "pair",
	"rotation": "number", "depth": "number",
	"mask": "regions",
}

// listInputs returns how the panel edits each key a layer of a role may
// write, the keys inside its instances included.
func listInputs(role string) map[string]string {
	keys := []string{role}
	if _, colored := recolors[role]; colored {
		keys = append(keys, lang.CoatSlots...)
	}

	if _, placed := templateInstances[role]; placed {
		row := subKey
		if role != subKey {
			row = instanceKey
			keys = append(keys, lang.CoatTextureKey, maskKey)
		}

		keys = append(keys, slices.Collect(maps.Keys(placement[row]))...)
	}

	inputs := map[string]string{}
	for _, key := range keys {
		if input, ok := coatInputs[key]; ok {
			inputs[key] = input
		}
	}

	return inputs
}

// listMaskChannels returns the pattern channel of each region a layer shows
// in: all of them where it names none, and none where it has no mask.
func listMaskChannels(layer Layer) []string {
	if layer.Mask == "" {
		return []string{}
	}

	if len(layer.Regions) == 0 {
		return slices.Clone(regions)
	}

	channels := []string{}
	for _, region := range layer.Regions {
		if region >= 1 && region <= len(regions) {
			channels = append(channels, regions[region-1])
		}
	}

	return channels
}

// rankInstances ranks every instance in the order the game draws them: a
// larger depth further back, and equal depths as written. A pattern has no
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

// resolveEmblem returns the layer of an emblem. Its mask is the coat's
// pattern file, with the regions the emblem shows in.
func (r *coatResolver) resolveEmblem(field clause, colors, names []string, patternFile string) Layer {
	children := listChildren(field)
	key, _, _ := splitField(field)
	emblemColors, emblemNames, lists := r.resolveColors(children, colors, names)
	layer := Layer{
		Role: key, Emblem: true, Colors: emblemColors, ColorNames: emblemNames, Lists: lists, Regions: []int{},
		Instances: r.resolveInstances(children, instanceKey), Range: toRange(field),
	}
	if texture, ok := findField(children, lang.CoatTextureKey); ok {
		var own []Group
		layer.Texture, own = r.resolveValue(texture)
		layer.Path, layer.Lists = r.findCoatFile(key, layer.Texture), slices.Concat(own, lists)
	}

	if mask, ok := findField(children, maskKey); ok {
		layer.Mask, layer.MaskRange = patternFile, toRange(mask)
		for _, region := range listNumbers(mask) {
			layer.Regions = append(layer.Regions, int(region))
		}
	}

	return layer
}

// resolveSub returns the layer of a sub: the coat its parent names, laid
// under the sub's own statements.
func (r *coatResolver) resolveSub(field clause) Layer {
	children := listChildren(field)
	parentField, _ := findField(children, parentKey)
	_, parent, _ := splitField(parentField)
	sub := Coat{
		Name: parent, Layers: r.resolveLayers(r.listCoatFields(field, 0), 1), Range: toRange(field),
		Place: r.ws.Locate(r.mods, field.path),
	}

	return Layer{
		Role: subKey, Texture: parent, Colors: []string{}, ColorNames: []string{}, Lists: []Group{},
		Regions: []int{}, Sub: &sub, Range: sub.Range, Instances: r.resolveInstances(children, subKey),
	}
}

// listTag is the tag of a value drawn from a template list, as list "x"
// writes it.
const listTag = "list"

// resolveValue returns the value a coat's statement writes, and the list it
// draws from. A list value is the option picked for it, else its heaviest
// option, or "missing:x" where no list is named x.
func (r *coatResolver) resolveValue(field clause) (string, []Group) {
	key, value, tag := splitField(field)
	if tag != listTag {
		return value, nil
	}

	definition, ok := findKeptClause(r.ws, r.mods, lang.Reference{Database: lang.CoatListsDatabase, Name: value})
	if !ok {
		return missingList + value, nil
	}

	list := Group{ID: fmt.Sprintf("%s:%d:%s", field.path, field.statement, key), Label: value}
	heaviest, drawn, isWeighed := 0.0, "", false
	for _, option := range listChildren(definition) {
		weight, text, _ := splitField(option)
		number, err := strconv.ParseFloat(weight, 64)
		if err != nil || text == "" {
			continue
		}

		list.Options = appendNew(list.Options, text)
		if !isWeighed || number > heaviest {
			heaviest, drawn, isWeighed = number, text, true
		}
	}

	list.Picked = cmp.Or(r.picks[list.ID], drawn)

	return list.Picked, []Group{list}
}

// findCoatFile returns the image file of a coat's texture: the file in the
// folder of the key that writes it, else at the texture's own path where it
// has one, or "".
func (r *coatResolver) findCoatFile(key, texture string) string {
	if texture == "" || strings.HasPrefix(texture, missingList) {
		return ""
	}

	file, ok := r.mods.ResolveTexture(lang.CoatTextureFolders[key] + "/" + texture)
	if !ok && strings.Contains(texture, "/") {
		file, _ = r.mods.ResolveTexture(texture)
	}

	return file
}

// fallbackColor is the color a pattern starts from, as CSS.
const fallbackColor = "rgba(0, 0, 0, 0)"

// resolveColors returns the CSS colors a coat's or emblem's statements give
// its slots, the fallback first, with each one's color name and the lists
// they are drawn from. A slot not written takes the base's color and name; a
// coat's own colors have no base.
func (r *coatResolver) resolveColors(fields []clause, base, baseNames []string) ([]string, []string, []Group) {
	colors := make([]string, len(lang.CoatSlots)+1)
	names := make([]string, len(colors))
	copy(colors, base)
	copy(names, baseNames)
	colors[0] = fallbackColor

	var lists []Group
	for position, slot := range lang.CoatSlots {
		if field, ok := findField(fields, slot); ok {
			var own []Group
			colors[position+1], names[position+1], own = r.resolveColor(field, base, baseNames)
			lists = append(lists, own...)
		}
	}

	return colors, names, lists
}

// resolveColor returns the CSS of the color a statement writes, the named
// color it writes, and the list it is drawn from. A value that names a slot,
// such as color1, takes that slot's base color, "" without a base.
func (r *coatResolver) resolveColor(field clause, base, baseNames []string) (string, string, []Group) {
	if css := makeColor(field); css != "" {
		return css, "", nil
	}

	label, lists := r.resolveValue(field)
	slot := slices.IndexFunc(lang.CoatSlots, func(slot string) bool { return strings.EqualFold(slot, label) })
	switch {
	case slot >= 0 && slot+1 < len(base):
		return base[slot+1], baseNames[slot+1], lists
	case slot >= 0:
		return "", "", lists
	}

	css, ok := r.colors[label]
	if !ok {
		reference := lang.Reference{Database: lang.CoatColorsDatabase, Name: label}
		if definition, ok := findKeptClause(r.ws, r.mods, reference); ok {
			css = makeColor(definition)
		}

		r.colors[label] = css
	}

	return css, label, lists
}

// makeColor returns the CSS of the color block a clause writes, or "".
func makeColor(definition clause) string {
	color, ok := jomini.ToColor(definition.tree, definition.statement)
	if !ok {
		return ""
	}

	return color.ToCSS()
}

// resolveInstances returns the instances the statements write, by the given
// row of placement. Where they write none, the game draws one default
// instance, which has no range.
func (r *coatResolver) resolveInstances(children []clause, row string) []Instance {
	instances := []Instance{}
	for _, field := range children {
		if key, _, _ := splitField(field); key == instanceKey {
			instances = append(instances, resolveInstance(field, row))
		}
	}

	if len(instances) == 0 {
		instances = append(instances, resolveInstance(clause{statement: absentStatement}, row))
	}

	return instances
}

// resolveInstance returns the instance an instance block writes, over the
// game's defaults.
func resolveInstance(field clause, row string) Instance {
	var instance Instance
	for _, place := range placement[row] {
		setPlacement(&instance, place.Part, place.Defaults)
	}

	if field.statement == absentStatement {
		return instance
	}

	instance.Range = toRange(field)
	for _, child := range listChildren(field) {
		key, _, _ := splitField(child)
		setPlacement(&instance, placement[row][key].Part, listNumbers(child))
	}

	return instance
}

// setPlacement sets one part of an instance to the numbers. An unknown part
// or no numbers set nothing.
func setPlacement(instance *Instance, part coatPart, numbers []float64) {
	if len(numbers) == 0 {
		return
	}

	switch part {
	case coatPartPlace:
		instance.Position = toPair(numbers)
	case coatPartSize:
		instance.Scale = toPair(numbers)
	case coatPartTurn:
		instance.Rotation = numbers[0]
	case coatPartDepth:
		instance.Depth = numbers[0]
	}
}

// toPair returns the first two numbers, the first twice where there is only
// one. It needs at least one.
func toPair(numbers []float64) [2]float64 {
	return [2]float64{numbers[0], numbers[min(1, len(numbers)-1)]}
}

// listNumbers returns the numbers a statement writes: its value and the bare
// values in its block. It returns none if any of them is no number.
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

// ListColors returns the game's named colors for the color picker, none if
// no workspace is open.
func (s *Service) ListColors() []NamedColor {
	ws, err := s.Workspace.Session()
	if err != nil {
		return []NamedColor{}
	}

	mods := ws.Mods.Load()
	colors := []NamedColor{}
	for _, winner := range mods.ListKept(ws.Schema, lang.CoatColorsDatabase) {
		if definition, ok := makeClause(ws, mods, winner); ok {
			if css := makeColor(definition); css != "" {
				colors = append(colors, NamedColor{Name: mods.ToReference(winner.Site).Name, CSS: css})
			}
		}
	}

	return colors
}

// ListTextures returns the texture files a layer of a role can use, each
// labeled with the file name a coat writes. It returns none if no workspace
// is open or the game has no folder for the role.
func (s *Service) ListTextures(role string) []Image {
	ws, err := s.Workspace.Session()
	if err != nil {
		return []Image{}
	}

	return listImages(ws, ws.Mods.Load(), lang.CoatTextureFolders[role])
}

// listImages returns the image files the game reads directly in a folder,
// letter case aside, labeled by file name and sorted, each label once.
func listImages(ws *workspace.Session, mods *index.Index, folder string) []Image {
	images := []Image{}
	for _, file := range mods.ListImages(folder + "/") {
		if rest := file.StageRel(); strings.EqualFold(path.Dir(rest), folder) {
			images = append(images, Image{Label: path.Base(rest), Place: ws.Locate(mods, file.Path)})
		}
	}

	slices.SortFunc(images, func(first, second Image) int { return cmp.Compare(first.Label, second.Label) })

	return slices.CompactFunc(images, func(first, second Image) bool { return first.Label == second.Label })
}

// coatDefineGroup is the group of the defines that name a coat's frames.
const coatDefineGroup = "NCoatOfArms"

// coatFrames holds, by game, the frames its coat designer previews, in order:
// those of gui/shared/coa_designer.gui without the unit's 3D banner.
var coatFrames = map[string][]coatFrame{
	game.CK3: {
		{Label: "Dynasty", Define: "DEFAULT_DYNASTY_COA_FRAME_NAME"},
		{Label: "House", Define: "DEFAULT_HOUSE_COA_FRAME_NAME"},
		{Label: "Title", Mask: "gfx/interface/coat_of_arms/title_mask.dds"},
		{Label: "Realm", Mask: "gfx/interface/icons/realm_frames/_default_mask.dds"},
	},
}

// frameMask is the mask file of a frame a define names, with the frame's
// name at %s.
const frameMask = "gfx/interface/coat_of_arms/frames/%s_mask.dds"

// ListFrames returns the shapes the game's coat designer previews a coat in,
// each with its mask. It returns none if no workspace is open or the game
// has no designer.
func (s *Service) ListFrames() []Image {
	frames := []Image{}
	ws, err := s.Workspace.Session()
	if err != nil {
		return frames
	}

	mods := ws.Mods.Load()
	library := makeLibrary(ws, mods)
	for _, row := range coatFrames[ws.Game.ID] {
		mask := row.Mask
		if row.Define != "" {
			frame := library.lookupDefine(coatDefineGroup, row.Define)
			if frame == "" {
				continue
			}

			mask = fmt.Sprintf(frameMask, frame)
		}

		if file, ok := mods.ResolveTexture(mask); ok {
			frames = append(frames, Image{Label: row.Label, Place: ws.Locate(mods, file)})
		}
	}

	return frames
}

// AddLayer returns the edits that insert a new layer of a role into a coat,
// at a position among its layers, 0 being first. It returns an error if the
// file does not define the coat, the coat is written as another's name, a
// second pattern is added, the game has no template for the role, or the
// layer before the position does not exist or comes from a parent.
func (s *Service) AddLayer(path, coat, role string, position int) ([]lsp.Edit, error) {
	resolver, err := s.findCoat(path, coat, nil)
	if err != nil {
		return nil, err
	}

	definition := resolver.definition
	tree := definition.tree
	if !tree.Statements[definition.statement].IsBlock() {
		return nil, fmt.Errorf("the coat %q is written as the name of another coat", coat)
	}

	layers := resolver.listLayers()
	if _, hasPattern := findField(layers, lang.CoatPatternKey); hasPattern && role == lang.CoatPatternKey {
		return nil, fmt.Errorf("the coat %q has a pattern already", coat)
	}

	lines, err := makeLayerLines(resolver.ws, resolver.mods, role, coat)
	if err != nil {
		return nil, err
	}

	// the coat's own layer written first, 0 for none
	first := int32(0)
	for _, layer := range layers {
		if resolver.isOwn(layer) && (first == 0 || layer.statement < first) {
			first = layer.statement
		}
	}

	var edit jomini.Edit
	switch {
	case position > 0:
		layer, err := resolver.findLayer(layers, position-1)
		if err != nil {
			return nil, err
		}

		span := tree.Statements[layer.statement].Span
		end := tree.Lines.LineSpan(tree.Lines.ToPosition(span.End).Line).End
		edit = jomini.Edit{Span: jomini.Span{Start: end, End: end}, Text: jomini.LineEnding(tree.Src) + toIndented(tree, span.Start, lines)}

	case first > 0:
		start := toLineSpan(tree, tree.Statements[first].Span).Start
		edit = jomini.Edit{Span: jomini.Span{Start: start, End: start}, Text: toIndented(tree, start, lines) + jomini.LineEnding(tree.Src)}

	default:
		edit = tree.BuildInsertion(definition.statement, lines)
	}

	return lsp.ToEdits(tree.Src, tree.Lines, []jomini.Edit{edit}), nil
}

// listLayers returns the statements that write the layers of the coat asked
// for, its parents' included.
func (r *coatResolver) listLayers() []clause {
	return listLayerFields(r.listCoatFields(r.definition, 0), 0)
}

// findLayer returns one of the layers of the coat asked for by its position.
// It returns an error for a layer that does not exist or comes from a parent.
func (r *coatResolver) findLayer(layers []clause, position int) (clause, error) {
	if position < 0 || position >= len(layers) {
		return clause{}, fmt.Errorf("the coat has no layer %d", position)
	}

	if !r.isOwn(layers[position]) {
		return clause{}, fmt.Errorf("the layer %d comes from a parent coat, in %q", position, layers[position].path)
	}

	return layers[position], nil
}

// isOwn reports whether a layer is written in the block of the coat asked for.
func (r *coatResolver) isOwn(layer clause) bool {
	return layer.path == r.definition.path && layer.tree.Statements[layer.statement].Parent == r.definition.statement
}

// templateLayers are the new layer the coat panel writes for each role, a
// line each, with %s for its texture or its parent.
var templateLayers = map[string][]string{
	"pattern":         {`pattern = "%s"`},
	"colored_emblem":  {"colored_emblem = {", `texture = "%s"`, `color1 = "white"`, coatInstance, "}"},
	"textured_emblem": {"textured_emblem = {", `texture = "%s"`, coatInstance, "}"},
	subKey:            {"sub = {", `parent = "%s"`, coatSubInstance, "}"},
}

// makeLayerLines returns the lines of a new layer of a role for a coat. It
// returns an error for a role with no template, or a game with nothing to
// start one from.
func makeLayerLines(ws *workspace.Session, mods *index.Index, role, coat string) ([]string, error) {
	template, ok := templateLayers[role]
	if !ok {
		return nil, fmt.Errorf("the game has no template for a layer with the role %q", role)
	}

	fill, err := findTemplateFill(ws, mods, role, coat)
	if err != nil {
		return nil, err
	}

	lines := make([]string, len(template))
	for position, line := range template {
		lines[position] = strings.ReplaceAll(line, "%s", fill)
	}

	return lines, nil
}

// templatePattern is how the panel picks a new layer's texture: the first
// file that starts with it, else the first file.
const templatePattern = "pattern_solid"

// findTemplateFill returns what a new layer of a role starts from: for a sub,
// another coat than the given one, else a texture file. It returns an error
// if the game has none.
func findTemplateFill(ws *workspace.Session, mods *index.Index, role, coat string) (string, error) {
	if role == subKey {
		parent := ""
		for _, winner := range mods.ListKept(ws.Schema, lang.CoatDatabase) {
			other := mods.ToReference(winner.Site).Name
			if !strings.EqualFold(other, coat) && (parent == "" || other < parent) {
				parent = other
			}
		}

		if parent == "" {
			return "", errors.New("the game has no other coat to start a sub with")
		}

		return parent, nil
	}

	textures := listImages(ws, mods, lang.CoatTextureFolders[role])
	if len(textures) == 0 {
		return "", fmt.Errorf("the game has no %s texture to start a layer with", role)
	}

	for _, image := range textures {
		if strings.HasPrefix(image.Label, templatePattern) {
			return image.Label, nil
		}
	}

	return textures[0].Label, nil
}

// MoveLayer returns the edits that move one layer of a coat to another
// position among its layers, none if the positions are the same. It returns
// an error if either position has no layer or one from a parent coat.
func (s *Service) MoveLayer(path, coat string, source, target int) ([]lsp.Edit, error) {
	resolver, err := s.findCoat(path, coat, nil)
	if err != nil {
		return nil, err
	}

	layers := resolver.listLayers()
	moved, err := resolver.findLayer(layers, source)
	if err != nil {
		return nil, err
	}

	neighbor, err := resolver.findLayer(layers, target)
	if err != nil || source == target {
		return []lsp.Edit{}, err
	}

	tree := moved.tree
	cut := toLineSpan(tree, tree.Statements[moved.statement].Span)
	destination := toLineSpan(tree, tree.Statements[neighbor.statement].Span)
	text := tree.Src[cut.Start:cut.End]
	if !strings.HasSuffix(text, "\n") {
		ending := " "
		if strings.HasSuffix(tree.Src[destination.Start:destination.End], "\n") {
			ending = jomini.LineEnding(tree.Src)
		}

		text += ending
	}

	insertion := destination.End
	if target < source {
		insertion = destination.Start
	}

	edits := []jomini.Edit{{Span: cut}, {Span: jomini.Span{Start: insertion, End: insertion}, Text: text}}

	return lsp.ToEdits(tree.Src, tree.Lines, edits), nil
}

// RemoveBlock returns the edits that delete one layer or instance of a coat,
// with its line. It returns an error if no statement starts where the range
// does.
func (s *Service) RemoveBlock(path string, block jomini.Range) ([]lsp.Edit, error) {
	tree, statement, err := s.findStatement(path, block)
	if err != nil {
		return nil, err
	}

	edit := jomini.Edit{Span: toLineSpan(tree, tree.Statements[statement].Span)}

	return lsp.ToEdits(tree.Src, tree.Lines, []jomini.Edit{edit}), nil
}

// AddInstance returns the edits that add a placement to a coat's layer. It
// returns an error if no block starts where the range does or the layer
// takes no placement.
func (s *Service) AddInstance(path string, layer jomini.Range) ([]lsp.Edit, error) {
	tree, statement, err := s.findBlock(path, layer)
	if err != nil {
		return nil, err
	}

	role := strings.ToLower(tree.Unquoted(tree.Statements[statement].Key))
	instance, ok := templateInstances[role]
	if !ok {
		return nil, fmt.Errorf("the game places no layer with the role %q", role)
	}

	edit := tree.BuildInsertion(statement, []string{instance})

	return lsp.ToEdits(tree.Src, tree.Lines, []jomini.Edit{edit}), nil
}

// findBlock returns a file's tree and the block that starts where the range
// does. It returns an error if no block starts there.
func (s *Service) findBlock(path string, block jomini.Range) (*jomini.Tree, int32, error) {
	tree, statement, err := s.findStatement(path, block)
	if err == nil && !tree.Statements[statement].IsBlock() {
		err = fmt.Errorf("the statement at line %d, column %d of %q is no block", block.StartLine, block.StartColumn, path)
	}

	return tree, statement, err
}

// findStatement returns a file's tree and the statement that starts where
// the range does. It returns an error if no statement starts there.
func (s *Service) findStatement(path string, block jomini.Range) (*jomini.Tree, int32, error) {
	ws, err := s.Workspace.Session()
	if err != nil {
		return nil, 0, err
	}

	tree, err := ws.LoadTree(path)
	if err != nil {
		return nil, 0, err
	}

	offset := jomini.ToOffset(tree.Src, tree.Lines, block.StartLine, block.StartColumn)
	statement, ok := tree.LookupStatement(offset)
	if !ok {
		return nil, 0, fmt.Errorf("no statement starts at line %d, column %d of %q", block.StartLine, block.StartColumn, path)
	}

	return tree, statement, nil
}

// toIndented returns lines indented as the line holding an offset is, joined
// as the file ends its lines.
func toIndented(tree *jomini.Tree, offset int, lines []string) string {
	row := tree.Lines.LineSpan(tree.Lines.ToPosition(offset).Line)
	text := tree.Src[row.Start:row.End]

	return tree.IndentLines(lines, text[:len(text)-len(strings.TrimLeft(text, " \t"))])
}

// toLineSpan returns the span of the whole lines a statement takes, or the
// span itself where it shares a line with another statement.
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

// templateColors are the first colors the panel writes for a new coat.
var templateColors = []string{"white", "black"}

// CreateCoatFile writes a new coat of arms file into a mod and returns its
// place. The coat's key is the name as a slug, and it holds the script, or
// the game's template where the script is "". It returns an error if the mod
// does not exist, the game keeps no coats, the name has no slug, or the file
// exists already.
func (s *Service) CreateCoatFile(modID, name, script string) (workspace.Place, error) {
	ws, err := s.Workspace.Session()
	if err != nil {
		return workspace.Place{}, err
	}

	slug := files.ToSlug(name)
	if script = strings.TrimSpace(script); script != "" {
		return s.writeCoatFile(ws, modID, slug, fmt.Sprintf("%s = {\n%s\n}", slug, script))
	}

	pattern, err := makeLayerLines(ws, ws.Mods.Load(), lang.CoatPatternKey, slug)
	if err != nil {
		return workspace.Place{}, err
	}

	lines := []string{slug + " = {"}
	for _, line := range pattern {
		lines = append(lines, "\t"+line)
	}

	for position, color := range templateColors {
		lines = append(lines, fmt.Sprintf("\t%s = %q", lang.CoatSlots[position], color))
	}

	return s.writeCoatFile(ws, modID, slug, strings.Join(append(lines, "}"), "\n"))
}

// CopyCoat copies a coat a file defines into a mod as a new coat of arms
// file and returns its place. It returns an error if the file does not define
// the coat, or as CreateCoatFile does.
func (s *Service) CopyCoat(path, name, modID string) (workspace.Place, error) {
	resolver, err := s.findCoat(path, name, nil)
	if err != nil {
		return workspace.Place{}, err
	}

	definition := resolver.definition
	text := definition.tree.Text(definition.tree.Statements[definition.statement].Span)

	return s.writeCoatFile(resolver.ws, modID, files.ToSlug(name), text)
}

// coatFolders holds, by game, where it keeps its coats, from the folder that
// holds its stages.
var coatFolders = map[string]string{
	game.CK3:  "common/coat_of_arms/coat_of_arms",
	game.Vic3: "common/coat_of_arms/coat_of_arms",
	game.EU5:  "main_menu/common/coat_of_arms/coat_of_arms",
}

// writeCoatFile writes a coat's text as a new file named by the slug in a
// mod's coat folder, and returns its place. It returns an error as
// CreateCoatFile does.
func (s *Service) writeCoatFile(ws *workspace.Session, modID, slug, coat string) (workspace.Place, error) {
	mod, ok := s.Workspace.LookupMod(modID)
	folder := coatFolders[ws.Game.ID]
	if !ok || folder == "" || slug == "" {
		return workspace.Place{}, fmt.Errorf("the open workspace has no mod %q that can hold a coat of arms named %q", modID, slug)
	}

	target := files.Join(mod.Dir, folder+"/"+slug+".txt")
	if _, err := os.Stat(target); err == nil {
		return workspace.Place{}, fmt.Errorf("the file %q exists already", target)
	}

	if err := s.Workspace.Write(target, files.Text{Text: coat + "\n", Encoding: files.EncodingUTF8BOM}); err != nil {
		return workspace.Place{}, err
	}

	return ws.Locate(ws.Mods.Load(), target), nil
}
