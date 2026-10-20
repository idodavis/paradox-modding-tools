// coat.go is a coat of arms resolved into layers: the coat on its field, its pattern, each emblem's instances and each
// sub with its parent under it, each layer the file it paints, how its shader colors it and where it is placed; the
// lists the designer picks from; and the file a mod's new coat is written in.

package draw

import (
	"cmp"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/index"
	"paradox-modding-tools/services/internal/inventory"
	"paradox-modding-tools/services/internal/jomini"
	"paradox-modding-tools/services/internal/schema"
	"paradox-modding-tools/services/internal/texture"
	"paradox-modding-tools/services/internal/workspace"
)

// Coat is one render of a coat of arms.
type Coat struct {
	Width   int      `json:"width"` // the field it is drawn on: the first atlas' tile, else the pattern's image
	Height  int      `json:"height"`
	Shading string   `json:"shading"` // the cloth that shades it; "" none
	Layers  []Layer  `json:"layers"`  // in pre-order
	Missing []Absent `json:"missing"` // what nothing found
}

// Layer is one layer of a coat.
type Layer struct {
	Element
	URL        string            `json:"url"`        // the file it paints; "" none
	Recolor    string            `json:"recolor"`    // how its shader colors it: a key of coat.ts' recolor rows
	Colors     []string          `json:"colors"`     // its slots as CSS; "" the shader's fallback
	Mask       string            `json:"mask"`       // an emblem's mask: its coat's pattern file; "" none
	Regions    []int             `json:"regions"`    // the mask's regions it keeps
	Properties map[string]string `json:"properties"` // every property as written, by lower-cased key
}

// Frame is a shape the game's windows cut a coat to.
type Frame struct {
	Label  string `json:"label"`
	URL    string `json:"url"`    // its mask's route
	Origin string `json:"origin"` // the origin of the mask that wins
}

// CoatTarget is the file a mod's new coat of arms is written in.
type CoatTarget struct {
	Path        string `json:"path"`
	Rel         string `json:"rel"`         // its path in the mod
	IsNewFile   bool   `json:"isNewFile"`   // writing it makes the file first
	IsNewFolder bool   `json:"isNewFolder"` // writing it makes its folder first
}

// coatResolver is one coat's resolve.
type coatResolver struct {
	*resolver
	coat            *Coat         // being built
	layerProperties []*properties // by layer
}

// instance is one instance of a coat's emblems in paint order.
type instance struct {
	emblem    int        // its emblem
	statement *statement // nil for the one an emblem that writes none has
	number    int        // among its emblem's
	depth     float64
}

// The words a coat is written with, and the kinds of its layers.
const (
	patternKey  = "pattern"
	instanceKey = "instance"
	subKey      = "sub"
	parentKey   = "parent"
	maskKey     = "mask"
	depthKey    = "depth"
	textureKey  = "texture"
	coatType    = "coat"
	emblemType  = "emblem"
	listPick    = "list" // the tag of a value the game picks from a template list
)

// emblemKeys are the keys an emblem is written with, each a key of game.CoatTextureFolders.
var emblemKeys = []string{"colored_emblem", "textured_emblem"}

// slotKeys are the color slots a coat and an emblem write, slot n at n-1.
var slotKeys = []string{"color1", "color2", "color3", "color4", "color5"}

// notAlphanumeric is a run of the characters a file name made from a mod's folder leaves out.
var notAlphanumeric = regexp.MustCompile(`[^a-z0-9]+`)

// newCoatResolver resolves the request's coat on a resolve's core: the coat named for the request (the one it names,
// for an entry that is another's id), its layers, the cloth that shades it and the field it is drawn on.
func newCoatResolver(core *resolver) (*coatResolver, error) {
	entry, err := core.entry("coat of arms", []string{game.CoatDatabases.Coats})
	if err != nil {
		return nil, err
	}
	resolver := &coatResolver{resolver: core, coat: &Coat{Layers: []Layer{}}}
	if shading, isShaded := game.CoatShading[resolver.ws.Game.ID]; isShaded {
		resolver.coat.Shading = resolver.textureURL(shading.Texture)
	}
	coat := entry
	if alias, isAlias := entry.assignment.Value.(*jomini.Scalar); isAlias {
		definition, isDefined := resolver.definition(schema.Occurrence{Database: game.CoatDatabases.Coats, ID: alias.Text})
		if !isDefined {
			resolver.addMissing("parent", alias.Text)
			definition = statement{
				assignment: &jomini.Assignment{Key: entry.assignment.Key, Range: entry.assignment.Range},
				parsed:     entry.parsed, path: entry.path,
			}
		}
		coat = definition
	}
	resolver.resolveCoat(coat, resolver.coatProperties(coat, 0), -1, nil)
	for layer := range resolver.coat.Layers {
		resolver.coat.Layers[layer].Properties = resolver.propertyTexts(resolver.layerProperties[layer].statements)
	}
	resolver.coat.Width, resolver.coat.Height = resolver.fieldSize()
	resolver.coat.Missing = resolver.missing
	return resolver, nil
}

// coatProperties returns what a coat writes as it applies: its parent's statements first (instances left out, and
// emblems where it writes its own), then its own, the last of each key winning; a bad parent is listed.
func (resolver *coatResolver) coatProperties(coat statement, depth int) *properties {
	own := propertiesOf(coat)
	parentID, hasParent := own.scalar(parentKey)
	if !hasParent || strings.EqualFold(parentID, "none") {
		return own
	}
	parent, isDefined := resolver.definition(schema.Occurrence{Database: game.CoatDatabases.Coats, ID: parentID})
	if !isDefined || depth > maxDepth {
		resolver.addMissing("parent", parentID)
		return own
	}
	writesEmblems := slices.ContainsFunc(own.statements, isEmblem)
	whole := &properties{}
	for _, statement := range resolver.coatProperties(parent, depth+1).statements {
		isInstance := strings.EqualFold(statement.assignment.Key.Text, instanceKey)
		if !isInstance && (!writesEmblems || !isEmblem(statement)) {
			whole.add(statement)
		}
	}
	whole.add(own.statements...)
	return whole
}

// fieldSize returns the field a coat is drawn on: the first atlas' tile_size, else (listed) the root's pattern's image
// where one draws.
func (resolver *coatResolver) fieldSize() (int, int) {
	resolver.element = -1
	if file, isListed := index.FileAt(resolver.ws.Indexes(), game.CoatFieldFile); isListed {
		if parsed, _, err := resolver.ws.Parsed(resolver.parses, file.Path); err == nil {
			for _, atlas := range jomini.Fields(&jomini.Block{Statements: parsed.Root.Statements}) {
				size, has := propertiesOf(statement{assignment: atlas, parsed: parsed}).numbers("tile_size")
				if has && len(size) == 2 {
					return int(size[0]), int(size[1])
				}
			}
		}
	}
	resolver.addMissing("field", game.CoatFieldFile)
	pattern := slices.IndexFunc(resolver.coat.Layers, func(layer Layer) bool {
		return layer.Type == patternKey && layer.URL != ""
	})
	if pattern < 0 {
		return 0, 0
	}
	file, _ := index.FileAt(resolver.ws.Indexes(), coatPath(patternKey, resolver.coat.Layers[pattern].Name))
	facts, err := texture.Info(file.Path)
	if err != nil {
		return 0, 0
	}
	return facts.Width, facts.Height
}

// resolveCoat resolves one coat, the root or a sub at one of its instances: its layer, its pattern, its emblems'
// instances in paint order, and for the root its subs.
func (resolver *coatResolver) resolveCoat(statement statement, coat *properties, parent int, instance *statement) {
	layerType, id := coatType, ""
	if parent >= 0 {
		layerType = subKey
		id, _ = coat.scalar(parentKey)
	}
	layer := resolver.addLayer(parent, statement, layerType, id)
	resolver.layerProperties[layer].add(slices.DeleteFunc(slices.Clone(coat.statements), isLayer)...)
	if instance != nil {
		resolver.layerProperties[layer].add(propertiesOf(*instance).statements...)
	}
	colors := resolver.coatColors(coat, nil)
	resolver.coat.Layers[layer].Colors = colors
	resolver.resolvePattern(layer, coat, colors)
	instances, emblems := instancesOf(coat)
	for start := 0; start < len(instances); {
		end := start + 1
		for end < len(instances) && instances[end].emblem == instances[start].emblem {
			end++
		}
		resolver.resolveEmblem(layer, emblems[instances[start].emblem], instances[start:end], colors)
		start = end
	}
	if parent < 0 {
		for _, sub := range coat.statements {
			if strings.EqualFold(sub.assignment.Key.Text, subKey) {
				resolver.resolveSub(layer, sub)
			}
		}
	}
}

// addLayer lists a layer of a coat under a parent (-1 for the root), written by a statement, of a kind, named for
// what it is ("" for the root, which is named for the request), with no properties yet; it is the layer resolved now.
func (resolver *coatResolver) addLayer(parent int, statement statement, layerType, id string) int {
	layer := Layer{
		Element: Element{Parent: parent, Key: statement.assignment.Key.Text, Type: layerType}, Regions: []int{},
	}
	var holder *Element
	if parent >= 0 {
		holder = &resolver.coat.Layers[parent].Element
	}
	resolver.identify(&layer.Element, holder, statement, id)
	resolver.coat.Layers = append(resolver.coat.Layers, layer)
	resolver.layerProperties = append(resolver.layerProperties, &properties{})
	resolver.element = len(resolver.coat.Layers) - 1
	return resolver.element
}

// coatColors returns a coat's or an emblem's color slots as CSS (the shader's fallback "" at 0): a scalar names a named
// color or the slot of the coat under it, a color block is read as one, and an unwritten slot is the coat's under it.
func (resolver *coatResolver) coatColors(coat *properties, base []string) []string {
	colors := make([]string, len(slotKeys)+1)
	for ordinal, key := range slotKeys {
		slot := ordinal + 1
		if slot < len(base) {
			colors[slot] = base[slot]
		}
		scalar, isScalar := coat.scalar(key)
		color, isColor := coat.color(key)
		switch {
		case isColor:
			colors[slot] = color
		case resolver.isPicked(key, coat):
			colors[slot] = ""
		case !isScalar:
		case slices.Contains(slotKeys, strings.ToLower(scalar)):
			baseSlot := slices.Index(slotKeys, strings.ToLower(scalar)) + 1
			colors[slot] = ""
			if baseSlot < len(base) {
				colors[slot] = base[baseSlot]
			}
		default:
			colors[slot] = resolver.entryColor(scalar)
		}
	}
	return colors
}

// entryColor returns the CSS of the named colors' entry an id names; "" for an id no entry defines as a color, listed.
func (resolver *resolver) entryColor(id string) string {
	definition, isDefined := resolver.definition(schema.Occurrence{Database: game.CoatDatabases.Colors, ID: id})
	css := ""
	if isDefined {
		css = cssOf(definition.assignment.Value)
	}
	if css == "" {
		resolver.addMissing("color", id)
	}
	return css
}

// resolvePattern lists a coat's pattern under it, painting its file colored by the coat's slots and holding the
// pattern and the slots the coat writes; a coat with no pattern draws none, and one a list picks is listed.
func (resolver *coatResolver) resolvePattern(holder int, coat *properties, colors []string) {
	statement, has := coat.last(patternKey)
	file, _ := coat.scalar(patternKey)
	if !has || resolver.isPicked(patternKey, coat) || file == "" {
		return
	}
	layer := resolver.addLayer(holder, statement, patternKey, file)
	resolver.layerProperties[layer].add(statement)
	for _, key := range slotKeys {
		if slot, hasSlot := coat.last(key); hasSlot {
			resolver.layerProperties[layer].add(slot)
		}
	}
	url := resolver.textureURL(coatPath(patternKey, file))
	pattern := &resolver.coat.Layers[layer]
	pattern.URL, pattern.Recolor, pattern.Colors = url, patternKey, colors
}

// instancesOf returns a coat's emblems and their instances in paint order, back to front: by depth, the larger behind
// unless game.CoatDepthInFront, then the written order; an emblem that writes no instance has one.
func instancesOf(coat *properties) ([]instance, []statement) {
	emblems := slices.DeleteFunc(slices.Clone(coat.statements), func(one statement) bool { return !isEmblem(one) })
	var instances []instance
	for ordinal, emblem := range emblems {
		number := 0
		for _, field := range fields(emblem) {
			if strings.EqualFold(field.assignment.Key.Text, instanceKey) {
				number++
				depth, _ := propertiesOf(field).numbers(depthKey)
				instances = append(instances,
					instance{emblem: ordinal, statement: &field, number: number, depth: append(depth, 0)[0]})
			}
		}
		if number == 0 {
			instances = append(instances, instance{emblem: ordinal, number: 1})
		}
	}
	slices.SortStableFunc(instances, func(first, second instance) int {
		if game.CoatDepthInFront {
			return cmp.Compare(first.depth, second.depth)
		}
		return cmp.Compare(second.depth, first.depth)
	})
	return instances, emblems
}

// resolveEmblem lists a run of an emblem's instances under a coat: the emblem's layer, masked by its coat's pattern
// where it writes a mask, and each instance's layer painting its file in its slots; a list picking the file is listed.
func (resolver *coatResolver) resolveEmblem(holder int, emblem statement, run []instance, colors []string) {
	own := propertiesOf(emblem)
	file, _ := own.scalar(textureKey)
	layer := resolver.addLayer(holder, emblem, emblemType, file)
	resolver.layerProperties[layer].add(slices.DeleteFunc(slices.Clone(own.statements), isLayer)...)
	if regions, has := own.numbers(maskKey); has {
		pattern, _ := resolver.layerProperties[holder].scalar(patternKey)
		if pattern != "" {
			resolver.coat.Layers[layer].Mask = resolver.textureURL(coatPath(patternKey, pattern))
		}
		for _, region := range regions {
			resolver.coat.Layers[layer].Regions = append(resolver.coat.Layers[layer].Regions, int(region))
		}
	}
	key := strings.ToLower(emblem.assignment.Key.Text)
	emblemColors := resolver.coatColors(own, colors)
	resolver.coat.Layers[layer].Colors = emblemColors
	isPicked := resolver.isPicked(textureKey, own)
	for _, instance := range run {
		statement, instanceProperties := emblem, &properties{}
		if instance.statement != nil {
			statement, instanceProperties = *instance.statement, propertiesOf(*instance.statement)
		}
		child := resolver.addLayer(layer, statement, instanceKey, fmt.Sprint(instance.number))
		resolver.layerProperties[child] = instanceProperties
		url := ""
		if !isPicked {
			url = resolver.textureURL(coatPath(key, file))
		}
		instanceLayer := &resolver.coat.Layers[child]
		instanceLayer.URL, instanceLayer.Recolor, instanceLayer.Colors = url, key, emblemColors
	}
}

// resolveSub lists a sub under the root coat, once for each of its instances (once over the whole field where it
// writes none): its parent's statements under its own.
func (resolver *coatResolver) resolveSub(holder int, sub statement) {
	coat := resolver.coatProperties(sub, 0)
	isPlaced := false
	for _, field := range fields(sub) {
		if strings.EqualFold(field.assignment.Key.Text, instanceKey) {
			resolver.resolveCoat(sub, coat, holder, &field)
			isPlaced = true
		}
	}
	if !isPlaced {
		resolver.resolveCoat(sub, coat, holder, nil)
	}
}

// textureURL returns the route of a coat texture's file; "" for a path nothing holds, listed at the layer being
// resolved.
func (resolver *coatResolver) textureURL(path string) string {
	file, isListed := index.FileAt(resolver.ws.Indexes(), path)
	if !isListed {
		resolver.addMissing("texture", path)
		return ""
	}
	return resolver.urlOf(file.Path)
}

// isPicked reports whether a key's last value is what a list picks (`color1 = list "normal_colors"`), which the game
// decides and the preview cannot, listed.
func (resolver *coatResolver) isPicked(key string, properties *properties) bool {
	last, has := properties.last(key)
	if !has {
		return false
	}
	tagged, isTagged := last.assignment.Value.(*jomini.TaggedBlock)
	if !isTagged || !strings.EqualFold(tagged.Tag.Text, listPick) {
		return false
	}
	resolver.addMissing("list", key)
	return true
}

// Colors returns the named colors a coat's slots can name, each id's CSS as the coat paints it ("" for one that is no
// color). The caller holds the workspace's read lock.
func Colors(ws *workspace.Session) map[string]string {
	resolver := newResolver(ws, Request{})
	colors := map[string]string{}
	for _, id := range index.Names(ws.Indexes(), game.CoatDatabases.Colors) {
		colors[id] = resolver.entryColor(id)
	}
	return colors
}

// Textures returns the images a coat texture key's folder holds across the load order, each route by its file name as
// written, a later origin's over an earlier's. The caller holds the workspace's read lock.
func Textures(ws *workspace.Session, key string) map[string]string {
	folder, isKnown := game.CoatTextureFolders[key]
	textures := map[string]string{}
	if !isKnown {
		return textures
	}
	for _, scanned := range ws.Indexes() {
		for _, file := range scanned.Files {
			dir, base := path.Split(file.Rel)
			if file.Format == inventory.Image && strings.EqualFold(strings.TrimSuffix(dir, "/"), folder) {
				textures[base] = routeOf(file.Path)
			}
		}
	}
	return textures
}

// Frames returns the shapes the game's coat designer previews a coat in, in its order, each with its mask's route and
// the winning origin; one whose mask the load order lacks is left out. The caller holds the workspace's read lock.
func Frames(ws *workspace.Session) []Frame {
	resolver := newResolver(ws, Request{})
	frames := []Frame{}
	for _, row := range game.CoatFrames[ws.Game.ID] {
		mask := row.Mask
		if row.Define != "" {
			define, isDefined := resolver.definition(schema.Occurrence{Database: game.DefinesDatabase, ID: row.Define})
			if !isDefined {
				continue
			}
			mask = fmt.Sprintf(game.CoatFrameMask, define.text())
		}
		if file, isListed := index.FileAt(ws.Indexes(), mask); isListed {
			frames = append(frames, Frame{Label: row.Label, URL: routeOf(file.Path), Origin: file.Origin})
		}
	}
	return frames
}

// CoatFile returns the file a mod's new coat of arms is written in (the mod's last coat file, else one named for it)
// and what writing it makes first. The caller holds the workspace's read lock.
func CoatFile(ws *workspace.Session, modID string) (CoatTarget, error) {
	modIndex := slices.IndexFunc(ws.Mods, func(mod workspace.Mod) bool { return mod.ID == modID })
	folder, hasFolder := game.CoatFolders[ws.Game.ID]
	if modIndex < 0 || !hasFolder {
		return CoatTarget{}, fmt.Errorf("the open workspace has no mod %q that can hold a coat of arms", modID)
	}
	mod := ws.Mods[modIndex]
	target := ""
	for _, file := range ws.Workspace.Files {
		definesCoats := slices.ContainsFunc(ws.Workspace.InFile(file.Path), func(def index.At) bool {
			return def.Occurrence().Database == game.CoatDatabases.Coats && def.Occurrence().Role == schema.Defines
		})
		if definesCoats && file.Origin == modID {
			target = file.Path
		}
	}
	if target == "" {
		stem := notAlphanumeric.ReplaceAllString(strings.ToLower(filepath.Base(mod.Dir)), "_")
		target = inventory.Join(mod.Dir, path.Join(folder, strings.Trim(stem, "_")+"_coats.txt"))
	}
	rel, err := filepath.Rel(mod.Dir, target)
	if err != nil {
		return CoatTarget{}, err
	}
	return CoatTarget{
		Path:        target,
		Rel:         filepath.ToSlash(rel),
		IsNewFile:   !exists(target),
		IsNewFolder: !exists(filepath.Dir(target)),
	}, nil
}

// exists reports whether a file or folder is on disk.
func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// coatPath returns the path of a coat texture's file, by the key that writes it.
func coatPath(key, file string) string {
	return game.CoatTextureFolders[key] + "/" + file
}

// isEmblem reports whether a statement writes an emblem.
func isEmblem(one statement) bool {
	return slices.ContainsFunc(emblemKeys, func(key string) bool {
		return strings.EqualFold(key, one.assignment.Key.Text)
	})
}

// isLayer reports whether a statement writes a layer of its own (an emblem, an instance or a sub), which its holder's
// properties leave out.
func isLayer(one statement) bool {
	key := strings.ToLower(one.assignment.Key.Text)
	return isEmblem(one) || key == instanceKey || key == subKey
}
