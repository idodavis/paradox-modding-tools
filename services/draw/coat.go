// coat.go resolves a coat of arms into the layers the Visual view draws, and
// makes the coat panel's edits.

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
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/services/lsp"
	"paradox-modding-tools/services/workspace"
)

// An Instance is one placement of a layer on the field. Values the file does
// not set take the game's defaults.
type Instance struct {
	Position [2]float64   `json:"position"`
	Scale    [2]float64   `json:"scale"`
	Rotation float64      `json:"rotation"` // in degrees, clockwise
	Depth    float64      `json:"depth"`
	Rank     int          `json:"rank"` // its place among every instance of its coat in the order they draw, from 0
	Range    jomini.Range `json:"range"`
}

// CoatStrength is a texture channel taken at a strength.
type CoatStrength struct {
	// Channel is the channel: r, g or b.
	Channel string `json:"channel"`

	// Strength is how much of the channel is taken. 0 takes none.
	Strength float64 `json:"strength"`
}

// A Blend is a texture channel that weights a blend toward a color.
type Blend struct {
	Channel string `json:"channel"` // the channel that weights the blend
	Color   string `json:"color"`   // the color blended in, as CSS
}

// A Recolor is how the game colors the texture of a layer, with the layer's
// colors as CSS.
type Recolor struct {
	Start   string       `json:"start"`   // the color it starts from
	Blends  []Blend      `json:"blends"`  // the blends toward a color, in order
	Overlay CoatStrength `json:"overlay"` // the channel overlaid on the result
	Alpha   string       `json:"alpha"`   // the channel that is its alpha; "" means the blends' own
}

// A Shading is the texture that shades the cloth of a coat.
type Shading struct {
	Path    string       `json:"path"`    // the image file of the texture
	Overlay CoatStrength `json:"overlay"` // the channel overlaid on each coat and sub
	Grain   CoatStrength `json:"grain"`   // the channel times a factor over each emblem's alpha
}

// A Layer is one layer of a coat: the pattern, an emblem or a sub-coat.
type Layer struct {
	Role         string            `json:"role"`         // the key as written: "pattern", "colored_emblem", "textured_emblem" or "sub"
	Texture      string            `json:"texture"`      // as written; "missing:x" when `list "x"` names a list that does not exist
	Path         string            `json:"path"`         // the image file for the texture
	Emblem       bool              `json:"emblem"`       // the layer is an emblem, colored or textured
	Writer       string            `json:"writer"`       // the parent coat that writes the layer; "" for the coat's own
	Mask         string            `json:"mask"`         // the image file of the pattern whose regions an emblem shows in
	MaskRange    jomini.Range      `json:"maskRange"`    // where the emblem's mask is written; zero where it writes none
	Regions      []int             `json:"regions"`      // the pattern's color regions an emblem shows in; none means all of them
	MaskChannels []string          `json:"maskChannels"` // the pattern channel of each of those regions; none without a mask
	Colors       []string          `json:"colors"`       // CSS colors, the fallback first and then one for each slot of the game
	ColorNames   []string          `json:"colorNames"`   // the named color of each of those, "" for one written as numbers
	Recolor      *Recolor          `json:"recolor"`      // how the game colors its texture; nil where it keeps its pixels
	Lists        []Group           `json:"lists"`        // the template lists its values are drawn from, each with its options
	Inputs       map[string]string `json:"inputs"`       // how the panel edits each key the layer's role may write, by key
	Instances    []Instance        `json:"instances"`    // in the order written; each one's Rank says when the coat draws it
	Sub          *Coat             `json:"sub,omitempty"`
	Range        jomini.Range      `json:"range"`
	Place        workspace.Place   `json:"place"` // the file the layer is written in
}

// A Coat is a coat of arms resolved for drawing, as its layers.
type Coat struct {
	Name    string          `json:"name"`
	Layers  []Layer         `json:"layers"`
	Range   jomini.Range    `json:"range"`
	Place   workspace.Place `json:"place"`
	Width   int             `json:"width"`   // the width of its field in pixels; 0 for a sub-coat or where the game gives none
	Height  int             `json:"height"`  // the height of its field in pixels; 0 for a sub-coat or where the game gives none
	Shading *Shading        `json:"shading"` // nil where the game shades no coat
	Regions []int           `json:"regions"` // the pattern's color regions, each one an emblem can show in
	Slots   []string        `json:"slots"`   // the keys that write the color slots, slot 1 first
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

// A coatResolver makes the layers of one coat, with the option picked for each
// of its lists.
type coatResolver struct {
	ws         *workspace.Session
	mods       *index.Index
	picks      map[string]string // the option picked for a list, by the ID of its group
	definition clause            // the coat asked for
	colors     map[string]string // the CSS of the named colors looked up, by name
}

// coatLerp is a texture channel that blends a color slot in.
type coatLerp struct {
	// Channel is the channel that weights the blend, and Slot the color slot
	// blended in.
	Channel string
	Slot    int
}

// coatRecolor is how the textures of one texture key are colored, such as a
// pattern's.
type coatRecolor struct {
	// Start is the slot it starts from. It is 0 for the fallback color.
	Start int

	// Lerps are the blends toward a slot, in order.
	Lerps []coatLerp

	// Overlay is the channel overlaid on the result.
	Overlay CoatStrength

	// Shape is the channel that is its alpha. It is "" for the blends' own.
	Shape string
}

// coatShade is the shading laid over every coat of a game that shades its
// coats. The zero coatShade shades nothing.
type coatShade struct {
	// Texture is the texture sampled over the field.
	Texture string

	// Overlay is the channel of it overlaid on each coat and sub, and Grain
	// the channel laid times a factor over each emblem's alpha.
	Overlay CoatStrength
	Grain   CoatStrength
}

// coatPlace is what one placement key does to a layer.
type coatPlace struct {
	// Part is what it moves, sizes or turns.
	Part coatPart

	// Defaults are the values it takes where the layer writes none.
	Defaults []float64
}

// coatPart is what one placement key sets on a layer's instance. The zero
// coatPart sets nothing.
type coatPart uint8

// The parts a placement key sets: the center an emblem is placed by, the
// corner a sub is placed by, the size, the turn and the depth.
const (
	coatPartCenter coatPart = iota + 1
	coatPartCorner
	coatPartSize
	coatPartTurn
	coatPartDepth
)

// coatFrame is a shape that a game's coat designer previews a coat in.
type coatFrame struct {
	// Label is the designer's name for it.
	Label string

	// Mask is the mask file. It is "" where a define names the frame.
	Mask string

	// Define is the define that names the frame whose mask is used: a
	// culture's, drawn as the default.
	Define string
}

// missingList is the start of what a template list that is not there is shown
// as.
const missingList = "missing:"

// recolors are how each texture key's files are colored. A key with none
// keeps their pixels.
var recolors = map[string]coatRecolor{
	"pattern":        {Start: 0, Lerps: []coatLerp{{"r", 1}, {"g", 2}, {"b", 3}}, Overlay: CoatStrength{Channel: "b"}},
	"colored_emblem": {Start: 1, Lerps: []coatLerp{{"g", 2}, {"r", 3}}, Overlay: CoatStrength{Channel: "b", Strength: 1}, Shape: "a"},
}

// regions are the pattern channel of each mask region, region n at n-1.
var regions = []string{"r", "g", "b"}

// placement is what a placed layer's keys do to it, by the layer's type.
var placement = map[string]map[string]coatPlace{
	instanceKey: {
		"position": {Part: coatPartCenter, Defaults: []float64{0.5, 0.5}},
		"scale":    {Part: coatPartSize, Defaults: []float64{1, 1}},
		"rotation": {Part: coatPartTurn, Defaults: []float64{0}},
		// CK3 01_holy_order_coas.txt:139 writes one, and CK3
		// _emblem_layouts.info:67 gives the default as 0.01. Vic3 and EU5
		// ship no doc of it and are taken to share it.
		"depth": {Part: coatPartDepth, Defaults: []float64{0.01}},
	},
	subKey: {
		"offset": {Part: coatPartCorner, Defaults: []float64{0, 0}},
		"scale":  {Part: coatPartSize, Defaults: []float64{1, 1}},
	},
}

// coatInstance is the instance a new emblem is written with: at the middle of
// the field, at half size, on one line.
const coatInstance = "instance = { position = { 0.5 0.5 } scale = { 0.5 0.5 } }"

// coatSubInstance is the instance a new sub is written with: a quarter of the
// field in from its corner, at half size, on one line.
const coatSubInstance = "instance = { offset = { 0.25 0.25 } scale = { 0.5 0.5 } }"

// templateInstances are the new instance the coat dialog writes for each role
// that is placed, on one line.
var templateInstances = map[string]string{
	"colored_emblem": coatInstance, "textured_emblem": coatInstance, subKey: coatSubInstance,
}

// The keys of a coat's statements.
const (
	// instanceKey is the key of one placement of an emblem or a sub, and
	// placement's row for an emblem.
	instanceKey = "instance"

	// subKey is the key of a sub-coat, and placement's row for a sub.
	subKey = "sub"

	// parentKey is the key that names the coat a coat or a sub is laid over.
	parentKey = "parent"

	// maskKey is the key of the pattern's regions an emblem shows in.
	maskKey = "mask"
)

// shades are how each game shades every coat, by the game's id. A game with
// no row shades none.
var shades = map[string]coatShade{
	// how the game shades its own coats
	game.CK3: {
		Texture: "gfx/coat_of_arms/coa_mask_texture.dds",
		Overlay: CoatStrength{Channel: "b", Strength: 0.2},
		Grain:   CoatStrength{Channel: "g", Strength: 2},
	},
}

// ResolveCoat resolves one coat of arms in a file into its layers. A coat
// written as the name of another is that coat, and one whose name no coat
// answers has no layers. The given picks are the option chosen for each list,
// by the ID of its group. It returns an error if the file does not define the
// coat.
func (drw *Service) ResolveCoat(path, name string, picks map[string]string) (Coat, error) {
	resolver, err := drw.findCoat(path, name, picks)
	if err != nil {
		return Coat{}, err
	}
	definition := resolver.definition
	coat := Coat{
		Name: definition.tree.Unquoted(definition.tree.Statements[definition.statement].Key), Layers: []Layer{},
		Range: toRange(definition), Place: resolver.ws.Locate(resolver.mods, definition.path),
		Regions: lo.RangeFrom(1, len(regions)),
		Slots:   slices.Clone(lang.CoatSlots),
	}
	shade := shades[resolver.ws.Game.ID]
	if texture, ok := resolver.mods.ResolveTexture(shade.Texture); ok {
		coat.Shading = &Shading{Path: texture, Overlay: shade.Overlay, Grain: shade.Grain}
	}
	coat.Width, coat.Height = findFieldSize(resolver.ws, resolver.mods)
	root := definition
	for range maxDepth {
		current := root.tree.Statements[root.statement]
		if current.IsBlock() {
			coat.Layers = resolver.resolveLayers(resolver.listCoatFields(root, 0), 0)
			return coat, nil
		}
		target, ok := findKeptClause(resolver.ws, resolver.mods, lang.Reference{Database: lang.CoatDatabase, Name: root.tree.Unquoted(current.Value)})
		if !ok {
			return coat, nil
		}
		root = target
	}
	return coat, nil
}

// findCoat returns the resolver for the coat a file defines under the given
// name, with the given picks. It returns an error if the file does not define
// the coat.
func (drw *Service) findCoat(path, name string, picks map[string]string) (*coatResolver, error) {
	ws, err := drw.Workspace.Session()
	if err != nil {
		return nil, err
	}
	mods := resolveIndex(ws, path)
	isCoat := func(database string) bool { return database == lang.CoatDatabase }
	definition, _, err := makeLibrary(ws, mods).findDrawn(path, name, isCoat)
	if err != nil {
		return nil, err
	}
	return &coatResolver{
		ws: ws, mods: mods, picks: picks, colors: map[string]string{},
		definition: definition,
	}, nil
}

// fieldFile is the file whose first atlas' tile_size sizes the field.
const fieldFile = "common/coat_of_arms/options/atlases.txt"

// tileSizeKey is the key of an atlas' tile size in fieldFile.
const tileSizeKey = "tile_size"

// findFieldSize returns the width and height in pixels of the field a coat is
// drawn on, or 0 and 0 if the game lists none.
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
// emblems where the coat writes emblems of its own. The given depth is how many
// parents lie between the coat and the one asked for.
func (resolver *coatResolver) listCoatFields(coat clause, depth int) []clause {
	own := listChildren(coat)
	parentField, ok := findField(own, parentKey)
	if !ok || depth >= maxDepth {
		return own
	}
	_, label, _ := splitField(parentField)
	parent, ok := findKeptClause(resolver.ws, resolver.mods, lang.Reference{Database: lang.CoatDatabase, Name: label})
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

// splitField returns the key in lower case, the value without its quotes and
// the tag of a statement.
func splitField(field clause) (key, value, tag string) {
	current := field.tree.Statements[field.statement]
	return strings.ToLower(field.tree.Unquoted(current.Key)), field.tree.Unquoted(current.Value), field.tree.Text(current.Tag)
}

// findField returns the last of the given statements that has the given key, or
// false if none has.
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

// listLayerFields returns the statements that write the layers of a coat in the
// order they draw. A sub is a layer only of the coat asked for, which has the
// given depth 0.
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

// resolveLayers returns the layers of a coat from its statements, each instance
// ranked in the order the coat draws them. The given depth is how many coats
// lie between the coat and the one asked for.
func (resolver *coatResolver) resolveLayers(fields []clause, depth int) []Layer {
	colors, names, lists := resolver.resolveColors(fields, nil, nil)
	layers := []Layer{}
	patternFile := ""
	for _, field := range listLayerFields(fields, depth) {
		var layer Layer
		key, _, _ := splitField(field)
		switch key {
		case lang.CoatPatternKey:
			texture, own := resolver.resolveValue(field)
			layer = Layer{
				Role: field.tree.Unquoted(field.tree.Statements[field.statement].Key), Texture: texture,
				Path: resolver.findCoatFile(key, texture), Colors: colors, ColorNames: names, Lists: slices.Concat(own, lists),
				Regions: []int{}, Instances: []Instance{}, Range: toRange(field),
			}
			patternFile = layer.Path
		case subKey:
			layer = resolver.resolveSub(field)
		default:
			layer = resolver.resolveEmblem(field, colors, names, patternFile)
		}
		if row, ok := recolors[key]; ok {
			layer.Recolor = &Recolor{
				Start: lo.NthOr(layer.Colors, row.Start, ""),
				Blends: lo.Map(row.Lerps, func(lerp coatLerp, _ int) Blend {
					return Blend{Channel: lerp.Channel, Color: lo.NthOr(layer.Colors, lerp.Slot, "")}
				}),
				Overlay: row.Overlay, Alpha: row.Shape,
			}
		}
		layer.Inputs, layer.MaskChannels = listInputs(key), listMaskChannels(layer)
		layer.Place, layer.Writer = resolver.ws.Locate(resolver.mods, field.path), field.writer
		layers = append(layers, layer)
	}
	rankInstances(layers)
	return layers
}

// rankInstances sets each instance's rank in the order the game draws them: by
// depth, a larger depth further back, and as written where depths are equal. A
// pattern has no instance and draws under them all.
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

// listMaskChannels returns the pattern channel of each region a layer shows in,
// all of them if it names none, or none for a layer with no mask.
func listMaskChannels(layer Layer) []string {
	if layer.Mask == "" {
		return []string{}
	}
	if len(layer.Regions) == 0 {
		return slices.Clone(regions)
	}
	return lo.FilterMap(layer.Regions, func(region, _ int) (string, bool) {
		return lo.NthOr(regions, region-1, ""), region >= 1 && region <= len(regions)
	})
}

// coatInputs are how each coat key's value is edited.
var coatInputs = map[string]string{
	"pattern": "texture", "texture": "texture",
	"color1": "color", "color2": "color", "color3": "color", "color4": "color", "color5": "color",
	"position": "pair", "scale": "pair", "offset": "pair",
	"rotation": "number", "depth": "number",
	"mask": "regions",
}

// listInputs returns how the panel edits each key a layer with the given role
// may write, including the keys inside its instances.
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

// resolveEmblem returns the layer of an emblem. Its mask is the coat's pattern
// file, with the regions the emblem shows in.
func (resolver *coatResolver) resolveEmblem(field clause, colors, names []string, patternFile string) Layer {
	children := listChildren(field)
	key, _, _ := splitField(field)
	emblemColors, emblemNames, lists := resolver.resolveColors(children, colors, names)
	layer := Layer{
		Role: field.tree.Unquoted(field.tree.Statements[field.statement].Key), Emblem: true, Colors: emblemColors,
		ColorNames: emblemNames, Lists: lists, Regions: []int{},
		Instances: resolver.resolveInstances(children, instanceKey), Range: toRange(field),
	}
	if texture, ok := findField(children, lang.CoatTextureKey); ok {
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

// resolveSub returns the layer of a sub: the coat its parent names, laid under
// the sub's own statements.
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

// listTag is the tag of a value drawn from a template list, as list "x"
// writes it.
const listTag = "list"

// resolveValue returns the text a statement of a coat writes as its value, and
// the list it draws from. A value written as a list is the option picked for
// it, else its heaviest option, or "missing:x" if no list is named x.
func (resolver *coatResolver) resolveValue(field clause) (string, []Group) {
	key, value, tag := splitField(field)
	if tag != listTag {
		return value, nil
	}
	definition, ok := findKeptClause(resolver.ws, resolver.mods, lang.Reference{Database: lang.CoatListsDatabase, Name: value})
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

// findCoatFile returns the image file of a coat's texture: the file under
// the folder of the key that writes it, else the file at the texture's own
// path where it is written with one. It returns "" if there is none.
func (resolver *coatResolver) findCoatFile(key, texture string) string {
	if texture == "" || strings.HasPrefix(texture, missingList) {
		return ""
	}

	file, ok := resolver.mods.ResolveTexture(lang.CoatTextureFolders[key] + "/" + texture)
	if !ok && strings.Contains(texture, "/") {
		file, _ = resolver.mods.ResolveTexture(texture)
	}

	return file
}

// fallbackColor is the color a pattern starts from, as CSS.
const fallbackColor = "rgba(0, 0, 0, 0)"

// resolveColors returns the colors a coat's or an emblem's statements give its
// slots as CSS, with the fallback at 0, the named color of each ("" for one
// written as numbers), and the lists they are drawn from. A slot not written
// takes the base's color and name.
func (resolver *coatResolver) resolveColors(fields []clause, base, baseNames []string) ([]string, []string, []Group) {
	colors := make([]string, len(lang.CoatSlots)+1)
	names := make([]string, len(colors))
	colors[0] = fallbackColor
	var lists []Group
	for position, slot := range lang.CoatSlots {
		colors[position+1], names[position+1] = lo.NthOr(base, position+1, ""), lo.NthOr(baseNames, position+1, "")
		if field, ok := findField(fields, slot); ok {
			var own []Group
			colors[position+1], names[position+1], own = resolver.resolveColor(field, base, baseNames)
			lists = append(lists, own...)
		}
	}
	return colors, names, lists
}

// resolveColor returns the CSS of the color a statement writes, the color's
// name and the list it is drawn from. A statement that names a slot takes that
// slot's base color and name.
func (resolver *coatResolver) resolveColor(field clause, base, baseNames []string) (string, string, []Group) {
	if css := makeColor(field); css != "" {
		return css, "", nil
	}
	label, lists := resolver.resolveValue(field)
	position := slices.IndexFunc(lang.CoatSlots, func(slot string) bool { return strings.EqualFold(slot, label) })
	if position >= 0 {
		return lo.NthOr(base, position+1, ""), lo.NthOr(baseNames, position+1, ""), lists
	}
	css, ok := resolver.colors[label]
	if !ok {
		if definition, ok := findKeptClause(resolver.ws, resolver.mods, lang.Reference{Database: lang.CoatColorsDatabase, Name: label}); ok {
			css = makeColor(definition)
		}
		resolver.colors[label] = css
	}
	return css, label, lists
}

// makeColor returns the CSS of the color a clause writes as a color block, or
// "" if it writes none.
func makeColor(definition clause) string {
	color, ok := jomini.ToColor(definition.tree, definition.statement)
	if !ok {
		return ""
	}
	return color.ToCSS()
}

// resolveInstances returns the instances the given statements write, in the
// given row of the game's placements. Where they write none, it returns the one
// default instance the game draws, which has no range.
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

// resolveInstance returns the instance an instance block writes, over the
// game's defaults.
func (resolver *coatResolver) resolveInstance(field clause, row string) Instance {
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

// setPlacement sets the part of an instance that the given part of a placement
// moves, sizes or turns, to the given numbers. It sets nothing for an unknown
// part or no numbers.
func setPlacement(instance *Instance, part coatPart, numbers []float64) {
	if len(numbers) == 0 {
		return
	}
	switch part {
	case coatPartCenter, coatPartCorner:
		instance.Position = toPair(numbers)
	case coatPartSize:
		instance.Scale = toPair(numbers)
	case coatPartTurn:
		instance.Rotation = numbers[0]
	case coatPartDepth:
		instance.Depth = numbers[0]
	}
}

// toPair returns the first two of the given numbers, the first twice where
// there is only one. It needs at least one.
func toPair(numbers []float64) [2]float64 {
	return [2]float64{numbers[0], numbers[min(1, len(numbers)-1)]}
}

// listNumbers returns the numbers a statement writes, its value and the bare
// values inside its block. It returns none if a value is no number.
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

// ListColors returns the game's named colors, for the color picker. It returns
// an empty list if no workspace is open.
func (drw *Service) ListColors() []NamedColor {
	ws, err := drw.Workspace.Session()
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

// ListTextures returns the texture files a layer with the given role can use,
// each labeled with the name a coat writes for it. It returns an empty list if
// no workspace is open or the game has no folder for the role.
func (drw *Service) ListTextures(role string) []Image {
	ws, err := drw.Workspace.Session()
	if err != nil {
		return []Image{}
	}
	return listImages(ws, ws.Mods.Load(), lang.CoatTextureFolders[role])
}

// coatFrames are the frames each game's coat designer previews, in order, by
// the game's id.
var coatFrames = map[string][]coatFrame{
	// the frames of gui/shared/coa_designer.gui, without the unit's 3D
	// banner. Each define is named in full, as
	// common/defines/graphic/00_graphics.txt:309,319 write them in
	// NCoatOfArms.
	game.CK3: {
		{Label: "Dynasty", Define: "NCoatOfArms|DEFAULT_DYNASTY_COA_FRAME_NAME"},
		{Label: "House", Define: "NCoatOfArms|DEFAULT_HOUSE_COA_FRAME_NAME"},
		{Label: "Title", Mask: "gfx/interface/coat_of_arms/title_mask.dds"},
		{Label: "Realm", Mask: "gfx/interface/icons/realm_frames/_default_mask.dds"},
	},
}

// frameMask is where a named frame's mask is, with the name at %s.
const frameMask = "gfx/interface/coat_of_arms/frames/%s_mask.dds"

// ListFrames returns the shapes the game's coat designer previews a coat in,
// each with where its mask is. It is empty if no workspace is open or the game
// has no designer.
func (drw *Service) ListFrames() []Image {
	frames := []Image{}
	ws, err := drw.Workspace.Session()
	if err != nil {
		return frames
	}
	mods := ws.Mods.Load()
	for _, row := range coatFrames[ws.Game.ID] {
		mask := row.Mask
		if row.Define != "" {
			define, ok := findKeptClause(ws, mods, lang.Reference{Database: lookupDefineDatabase(), Name: row.Define})
			if !ok {
				continue
			}
			mask = fmt.Sprintf(frameMask, define.tree.Unquoted(define.tree.Statements[define.statement].Value))
		}
		if file, ok := mods.ResolveTexture(mask); ok {
			frames = append(frames, Image{Label: row.Label, Place: ws.Locate(mods, file)})
		}
	}
	return frames
}

// listImages returns the image files the game reads directly in a folder,
// letter case aside, each labeled with its file name and sorted by label. Files
// with the same label are listed once.
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

// AddLayer returns the edits that insert a new layer from the game's template
// for the given role into a coat at the given position among its layers, 0
// being first. It returns an error if the file does not define the coat, the
// coat is written as the name of another, a pattern is added to a coat that has
// one, the game has no template for the role, or the layer before the position
// does not exist or comes from a parent coat.
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
	if _, hasPattern := findField(layers, lang.CoatPatternKey); hasPattern && role == lang.CoatPatternKey {
		return nil, fmt.Errorf("the coat %q has a pattern already", coat)
	}
	lines, err := makeLayerLines(resolver.ws, resolver.mods, role, coat)
	if err != nil {
		return nil, err
	}
	own := lo.FilterMap(layers, func(layer clause, _ int) (int32, bool) {
		return layer.statement, resolver.isOwn(layer)
	})
	slices.Sort(own)
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
	case len(own) > 0:
		start := toLineSpan(tree, tree.Statements[own[0]].Span).Start
		edit = jomini.Edit{Span: jomini.Span{Start: start, End: start}, Text: toIndented(tree, start, lines) + jomini.LineEnding(tree.Src)}
	default:
		edit = tree.BuildInsertion(definition.statement, lines)
	}
	return lsp.ToEdits(tree.Src, tree.Lines, []jomini.Edit{edit}), nil
}

// templateLayers are the new layer the coat dialog writes for each role, a line
// each, with %s for its texture or its parent.
var templateLayers = map[string][]string{
	"pattern":         {`pattern = "%s"`},
	"colored_emblem":  {"colored_emblem = {", `texture = "%s"`, `color1 = "white"`, coatInstance, "}"},
	"textured_emblem": {"textured_emblem = {", `texture = "%s"`, coatInstance, "}"},
	subKey:            {"sub = {", `parent = "%s"`, coatSubInstance, "}"},
}

// makeLayerLines returns the lines of the game's template for a layer with the
// given role, to add to the given coat. It returns an error for a role the
// template has no layer for, or a game with nothing to start one from.
func makeLayerLines(ws *workspace.Session, mods *index.Index, role, coat string) ([]string, error) {
	lines, ok := templateLayers[role]
	if !ok {
		return nil, fmt.Errorf("the game has no template for a layer with the role %q", role)
	}
	fill, err := findTemplateFill(ws, mods, role, coat)
	if err != nil {
		return nil, err
	}
	return lo.Map(lines, func(line string, _ int) string { return strings.ReplaceAll(line, "%s", fill) }), nil
}

// templatePattern is how the coat dialog picks a new layer's texture: the
// first whose name starts with it, else the first.
const templatePattern = "pattern_solid"

// findTemplateFill returns what a new layer with the given role starts from:
// for a sub, a coat other than the given one, else a texture file. It returns
// an error if the game has none.
func findTemplateFill(ws *workspace.Session, mods *index.Index, role, coat string) (string, error) {
	if role == subKey {
		names := lo.FilterMap(mods.ListKept(ws.Schema, lang.CoatDatabase), func(winner index.Winner, _ int) (string, bool) {
			name := mods.ToReference(winner.Site).Name
			return name, !strings.EqualFold(name, coat)
		})
		if len(names) == 0 {
			return "", fmt.Errorf("the game has no other coat to start a sub with")
		}
		return slices.Min(names), nil
	}
	textures := listImages(ws, mods, lang.CoatTextureFolders[role])
	if len(textures) == 0 {
		return "", fmt.Errorf("the game has no %s texture to start a layer with", role)
	}
	return lo.FindOrElse(textures, textures[0], func(image Image) bool {
		return strings.HasPrefix(image.Label, templatePattern)
	}).Label, nil
}

// listLayers returns the statements that write the layers of the coat asked
// for, those from its parents included.
func (resolver *coatResolver) listLayers() []clause {
	return listLayerFields(resolver.listCoatFields(resolver.definition, 0), 0)
}

// findLayer returns the statement of one of the given layers of the coat asked
// for. It returns an error for a layer that does not exist or that the coat has
// from a parent.
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

// MoveLayer returns the edits that move one layer of a coat to another position
// among its layers, none if both positions are the same. It returns an error if
// either position has no layer or has one from a parent coat.
func (drw *Service) MoveLayer(path, coat string, source, target int) ([]lsp.Edit, error) {
	resolver, err := drw.findCoat(path, coat, nil)
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
		text += lo.Ternary(strings.HasSuffix(tree.Src[destination.Start:destination.End], "\n"), jomini.LineEnding(tree.Src), " ")
	}
	insertion := lo.Ternary(target < source, destination.Start, destination.End)
	edits := []jomini.Edit{{Span: cut}, {Span: jomini.Span{Start: insertion, End: insertion}, Text: text}}
	return lsp.ToEdits(tree.Src, tree.Lines, edits), nil
}

// RemoveBlock returns the edits that delete one layer or instance of a coat,
// including its line. It returns an error if no statement starts where the
// given range does.
func (drw *Service) RemoveBlock(path string, block jomini.Range) ([]lsp.Edit, error) {
	tree, statement, err := drw.findStatement(path, block)
	if err != nil {
		return nil, err
	}
	edit := jomini.Edit{Span: toLineSpan(tree, tree.Statements[statement].Span)}
	return lsp.ToEdits(tree.Src, tree.Lines, []jomini.Edit{edit}), nil
}

// AddInstance returns the edits that add a placement to a layer of a coat. It
// returns an error if no block starts where the given range does or the layer
// takes no placement.
func (drw *Service) AddInstance(path string, layer jomini.Range) ([]lsp.Edit, error) {
	tree, statement, err := drw.findBlock(path, layer)
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

// findBlock returns the tree of a file and the block that starts where the
// given range does. It returns an error if no block starts there.
func (drw *Service) findBlock(path string, block jomini.Range) (*jomini.Tree, int32, error) {
	tree, statement, err := drw.findStatement(path, block)
	if err == nil && !tree.Statements[statement].IsBlock() {
		err = fmt.Errorf("the statement at line %d, column %d of %q is no block", block.StartLine, block.StartColumn, path)
	}
	return tree, statement, err
}

// findStatement returns the tree of a file and the statement that starts where
// the given range does. It returns an error if no statement starts there.
func (drw *Service) findStatement(path string, block jomini.Range) (*jomini.Tree, int32, error) {
	ws, err := drw.Workspace.Session()
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

// toIndented returns lines indented as the line holding an offset is, joined as
// the file ends its lines.
func toIndented(tree *jomini.Tree, offset int, lines []string) string {
	row := tree.Lines.LineSpan(tree.Lines.ToPosition(offset).Line)
	text := tree.Src[row.Start:row.End]
	return tree.IndentLines(lines, text[:len(text)-len(strings.TrimLeft(text, " \t"))])
}

// toLineSpan returns the span of the whole lines a statement takes, or the span
// itself if it shares a line with another statement.
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

// templateColors are the first colors the coat dialog writes for a new coat.
var templateColors = []string{"white", "black"}

// CreateCoatFile writes a new coat of arms file into the given mod and returns
// its place. The coat is named after the given text as a slug and holds the
// given script, or the game's coat template if the script is "". It returns an
// error if the mod does not exist, the game keeps no coats of arms, the name
// has no slug or the file exists already.
func (drw *Service) CreateCoatFile(modID, name, script string) (workspace.Place, error) {
	ws, err := drw.Workspace.Session()
	if err != nil {
		return workspace.Place{}, err
	}
	slug := files.ToSlug(name)
	if script = strings.TrimSpace(script); script != "" {
		return drw.writeCoatFile(ws, modID, slug, fmt.Sprintf("%s = {\n%s\n}", slug, script))
	}
	pattern, err := makeLayerLines(ws, ws.Mods.Load(), lang.CoatPatternKey, slug)
	if err != nil {
		return workspace.Place{}, err
	}
	lines := slices.Concat([]string{slug + " = {"}, lo.Map(pattern, func(line string, _ int) string { return "\t" + line }))
	for position, color := range templateColors {
		lines = append(lines, fmt.Sprintf("\t%s = %q", lang.CoatSlots[position], color))
	}
	return drw.writeCoatFile(ws, modID, slug, strings.Join(append(lines, "}"), "\n"))
}

// CopyCoat copies a coat a file defines into the given mod as a new coat of
// arms file and returns its place. It returns an error if the file does not
// define the coat, or as CreateCoatFile does.
func (drw *Service) CopyCoat(path, name, modID string) (workspace.Place, error) {
	resolver, err := drw.findCoat(path, name, nil)
	if err != nil {
		return workspace.Place{}, err
	}
	definition := resolver.definition
	text := definition.tree.Text(definition.tree.Statements[definition.statement].Span)
	return drw.writeCoatFile(resolver.ws, modID, files.ToSlug(name), text)
}

// coatFolders are where each game keeps its coats, from the folder that holds
// its stages, by the game's id.
var coatFolders = map[string]string{
	game.CK3:  "common/coat_of_arms/coat_of_arms",
	game.Vic3: "common/coat_of_arms/coat_of_arms",
	game.EU5:  "main_menu/common/coat_of_arms/coat_of_arms",
}

// writeCoatFile writes the given coat text as a file named after the given slug
// in the given mod's coat folder and returns its place. It returns an error as
// CreateCoatFile does.
func (drw *Service) writeCoatFile(ws *workspace.Session, modID, slug, coat string) (workspace.Place, error) {
	mod, ok := drw.Workspace.LookupMod(modID)
	folder := coatFolders[ws.Game.ID]
	if !ok || folder == "" || slug == "" {
		return workspace.Place{}, fmt.Errorf("the open workspace has no mod %q that can hold a coat of arms named %q", modID, slug)
	}
	target := files.Join(mod.Dir, folder+"/"+slug+".txt")
	if _, err := os.Stat(target); err == nil {
		return workspace.Place{}, fmt.Errorf("the file %q exists already", target)
	}
	if err := drw.Workspace.Write(target, files.Text{Text: coat + "\n", Encoding: files.EncodingUTF8BOM}); err != nil {
		return workspace.Place{}, err
	}
	return ws.Locate(ws.Mods.Load(), target), nil
}
