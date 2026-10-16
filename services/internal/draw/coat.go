// coat.go is a coat of arms resolved into elements: the coat on its field, its pattern, each emblem's instances and
// each sub with its parent under it, every texture carrying how its shader colors it; and the file a mod's new coat
// is written in.

package draw

import (
	"cmp"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/index"
	"paradox-modding-tools/services/internal/jomini"
	"paradox-modding-tools/services/internal/schema"
	"paradox-modding-tools/services/internal/workspace"
)

// placement is one instance of a coat's emblems in paint order: its emblem, its statement (none for the one an
// emblem that writes none has), its number among its emblem's, and its depth.
type placement struct {
	emblem   int
	instance *statement
	number   int
	depth    float64
}

// The words a coat is written with.
const (
	patternKey  = "pattern"
	instanceKey = "instance"
	subKey      = "sub"
	parentKey   = "parent"
	maskKey     = "mask"
	depthKey    = "depth"
	coatType    = "coat"
	emblemType  = "emblem"
	fieldType   = "field"
	listPick    = "list" // a value the game picks from a template list
)

// The recolors the page applies beyond a texture kind's own: a mask of the pattern's regions, the shading drawn over a
// coat, and the shading's grain over an emblem's alpha.
const (
	recolorRegions = "regions"
	recolorShading = "shading"
	recolorGrain   = "grain"
)

// emblemKeys are the keys an emblem is written with, each a key of game.CoatTextureFolders.
var emblemKeys = []string{"colored_emblem", "textured_emblem"}

// slotKeys are the color slots a coat and an emblem write, slot n at n-1.
var slotKeys = []string{"color1", "color2", "color3", "color4", "color5"}

// notAlphanumeric is a run of the characters a file name made from a mod's folder leaves out.
var notAlphanumeric = regexp.MustCompile(`[^a-z0-9]+`)

// resolveCoat resolves a coat entry: the coat as the root element (the one it names, for an entry that is another's
// id), every element's properties as written, and the field it is drawn on as the holder.
func resolveCoat(resolver *resolver, entry child) {
	coat := entry.statement
	if alias, isAlias := coat.assignment.Value.(*jomini.Scalar); isAlias {
		named, isNamed := resolver.definition(schema.Occurrence{Database: game.CoatDatabases.Coats, ID: alias.Text})
		if !isNamed {
			resolver.addMissing(parentKey, alias.Text)
			named = statement{assignment: &jomini.Assignment{Key: coat.assignment.Key, Range: coat.assignment.Range},
				parsed: coat.parsed, path: coat.path}
		}
		coat = named
	}
	resolver.list.DepthInFront = game.CoatDepthInFront
	resolver.resolveCoatBlock(coat, resolver.coatBlock(coat, 0), -1, nil)
	for i, widget := range resolver.widgets {
		resolver.list.Elements[i].Properties = resolver.propertiesOf(widget.statements)
	}
	resolver.list.Holders = []Element{resolver.resolveField()}
}

// resolveField is the holder a coat is drawn on: the field, as large as the first atlas' tile, else as its pattern's
// texture, listed.
func (resolver *resolver) resolveField() Element {
	resolver.element = -1
	field := Element{
		Parent: -1, Key: fieldType, Type: fieldType, Label: fieldType, Path: fieldType, Holds: game.HoldsCell,
		Ancestors: []int{}, Properties: map[string]string{}, Textures: []int{},
	}
	if file, isListed := index.FileAt(resolver.ws.Indexes(), game.CoatFieldFile); isListed {
		if parsed, _, err := resolver.ws.Parsed(resolver.parses, file.Path); err == nil {
			for _, atlas := range jomini.Fields(&jomini.Block{Statements: parsed.Root.Statements}) {
				size, has := blockOf(statement{assignment: atlas, parsed: parsed}).numbers("tile_size")
				if has && len(size) == 2 {
					field.Properties[sizeKey] = fmt.Sprintf("%g %g", size[0], size[1])
					return field
				}
			}
		}
	}
	resolver.addMissing(fieldType, game.CoatFieldFile)
	isDrawnPattern := func(element Element) bool { return element.Type == patternKey && len(element.Textures) > 0 }
	if pattern := slices.IndexFunc(resolver.list.Elements, isDrawnPattern); pattern >= 0 {
		texture := resolver.list.Textures[resolver.list.Elements[pattern].Textures[0]]
		field.Properties[sizeKey] = fmt.Sprintf("%d %d", texture.Width, texture.Height)
	}
	return field
}

// coatBlock is a coat's statements as they apply: its parent's first, their placements left out and their emblems
// too where it writes any of its own (the wiki: emblems are inherited all or nothing), then its own, the last of each
// key winning; a parent nothing defines, or one nested past maxDepth, is listed.
func (resolver *resolver) coatBlock(coat statement, depth int) *block {
	own := blockOf(coat)
	parentID, hasParent := own.scalar(parentKey)
	if !hasParent || strings.EqualFold(parentID, "none") {
		return own
	}
	parent, isDefined := resolver.definition(schema.Occurrence{Database: game.CoatDatabases.Coats, ID: parentID})
	if !isDefined || depth > maxDepth {
		resolver.addMissing(parentKey, parentID)
		return own
	}
	writesEmblems := slices.ContainsFunc(own.statements, isEmblem)
	merged := &block{}
	for _, inherited := range resolver.coatBlock(parent, depth+1).statements {
		isPlacement := strings.EqualFold(inherited.assignment.Key.Text, instanceKey)
		if !isPlacement && (!writesEmblems || !isEmblem(inherited)) {
			merged.add(inherited)
		}
	}
	merged.add(own.statements...)
	return merged
}

// resolveCoatBlock resolves one coat, the root or a sub at a placement: its element, then its pattern, its emblems'
// instances in paint order, and, for the root, its subs. It returns the coat's element.
func (resolver *resolver) resolveCoatBlock(at statement, coat *block, parent int, place *statement) int {
	parentID, layerType := "", coatType
	if parent >= 0 {
		parentID, _ = coat.scalar(parentKey)
		layerType = subKey
	}
	element := resolver.coatWidget(parent, at, layerType, parentID)
	// the game draws a coat into an atlas tile, which clips what lies outside it
	resolver.list.Elements[element.element].Clips = parent < 0
	element.add(slices.DeleteFunc(slices.Clone(coat.statements), isPart)...)
	if place != nil {
		element.add(blockOf(*place).statements...)
	}
	if shading, isShaded := game.CoatShading[resolver.ws.Game.ID]; isShaded {
		resolver.addTexture(element.element, shading, Texture{Recolor: recolorShading})
	}
	colors := resolver.coatColors(coat, nil)
	resolver.resolvePattern(element.element, coat, colors)
	placements, emblems := resolver.placements(coat)
	for start := 0; start < len(placements); {
		end := start + 1
		for end < len(placements) && placements[end].emblem == placements[start].emblem {
			end++
		}
		resolver.resolveEmblem(element.element, emblems[placements[start].emblem], placements[start:end], colors)
		start = end
	}
	if parent < 0 {
		for _, sub := range coat.statements {
			if strings.EqualFold(sub.assignment.Key.Text, subKey) {
				resolver.resolveSub(element.element, sub)
			}
		}
	}
	return element.element
}

// coatWidget lists an element of a coat under a parent: its widget written at a statement, of a coat type, named
// for its layer ("" for the root, which is named for the request). Every one fills its holder, the field's size, in
// which the page moves, sizes and turns an instance or a sub.
func (resolver *resolver) coatWidget(parent int, at statement, layerType, id string) *widget {
	widget := &widget{element: len(resolver.list.Elements), at: at, overrides: map[string]statement{}}
	resolver.list.Elements = append(resolver.list.Elements, Element{
		Parent: parent, Key: at.assignment.Key.Text, Type: layerType, Holds: game.HoldsCell, Fills: true,
		Textures: []int{},
	})
	resolver.widgets = append(resolver.widgets, widget)
	resolver.element = widget.element
	resolver.identify(widget, id)
	return widget
}

// coatColors are a coat's or an emblem's color slots as CSS, the shader's fallback "" at 0: a scalar names a named
// color, or the slot of the coat under it (colorN); a color block is read as one; a list's pick is listed; a slot the
// block does not write is the coat's under it ("" for none).
func (resolver *resolver) coatColors(coat *block, under []string) []string {
	colors := make([]string, len(slotKeys)+1)
	for i, key := range slotKeys {
		if i+1 < len(under) {
			colors[i+1] = under[i+1]
		}
		scalar, isScalar := coat.scalar(key)
		color, isColor := coat.color(key)
		switch {
		case isColor:
			colors[i+1] = color
		case !isScalar:
		case resolver.isPicked(key, scalar):
			colors[i+1] = ""
		case slices.Contains(slotKeys, strings.ToLower(scalar)):
			slot := slices.Index(slotKeys, strings.ToLower(scalar)) + 1
			colors[i+1] = ""
			if slot < len(under) {
				colors[i+1] = under[slot]
			}
		default:
			colors[i+1] = resolver.entryColor(scalar)
		}
	}
	return colors
}

// entryColor is the CSS of the named colors' entry an id names; "" for an id no entry defines as a color, listed.
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

// resolvePattern lists a coat's pattern under it, its texture colored by the coat's slots; a coat with no pattern
// draws none, and one a list picks is listed.
func (resolver *resolver) resolvePattern(coat int, block *block, colors []string) {
	last, has := block.last(patternKey)
	file, _ := block.scalar(patternKey)
	hasPattern := has && file != ""
	if !hasPattern || resolver.isPicked(patternKey, file) {
		return
	}
	pattern := resolver.coatWidget(coat, last, patternKey, file)
	pattern.add(last)
	for _, key := range slotKeys {
		if slot, hasSlot := block.last(key); hasSlot {
			pattern.add(slot)
		}
	}
	resolver.addTexture(pattern.element, coatPath(patternKey, file), Texture{Recolor: patternKey, Colors: colors})
}

// placements are a coat's emblems' instances in paint order, back to front: by depth, the larger behind unless
// game.CoatDepthInFront, the written order among equals; an emblem that writes no instance has one. It returns the
// emblems too.
func (resolver *resolver) placements(coat *block) ([]placement, []statement) {
	var placements []placement
	emblems := slices.DeleteFunc(slices.Clone(coat.statements), func(one statement) bool { return !isEmblem(one) })
	for i, emblem := range emblems {
		number := 0
		for _, field := range fields(emblem) {
			if strings.EqualFold(field.assignment.Key.Text, instanceKey) {
				depth, _ := blockOf(field).numbers(depthKey)
				number++
				placements = append(placements,
					placement{emblem: i, instance: &field, number: number, depth: append(depth, 0)[0]})
			}
		}
		if number == 0 {
			placements = append(placements, placement{emblem: i, number: 1})
		}
	}
	slices.SortStableFunc(placements, func(a, b placement) int {
		if game.CoatDepthInFront {
			return cmp.Compare(a.depth, b.depth)
		}
		return cmp.Compare(b.depth, a.depth)
	})
	return placements, emblems
}

// resolveEmblem lists a run of an emblem's instances under a coat: the emblem as an element filling the field, masked
// by its coat's pattern's regions where it writes a mask over a pattern of a file and by the shading's grain, each
// instance under it drawing the emblem's texture colored by its slots (the coat's where it writes none).
func (resolver *resolver) resolveEmblem(coat int, emblem statement, run []placement, colors []string) {
	own := blockOf(emblem)
	file, _ := own.scalar("texture")
	element := resolver.coatWidget(coat, emblem, emblemType, file)
	element.add(slices.DeleteFunc(slices.Clone(own.statements), isPart)...)
	pattern, _ := resolver.widgets[coat].scalar(patternKey)
	isPatternFile := pattern != "" && !strings.EqualFold(pattern, listPick)
	if regions, has := own.numbers(maskKey); has && isPatternFile {
		kept := make([]int, len(regions))
		for i, region := range regions {
			kept[i] = int(region)
		}
		resolver.addTexture(element.element, coatPath(patternKey, pattern), Texture{Recolor: recolorRegions, Regions: kept})
	}
	if shading, isShaded := game.CoatShading[resolver.ws.Game.ID]; isShaded {
		resolver.addTexture(element.element, shading, Texture{Recolor: recolorGrain})
	}
	key := strings.ToLower(emblem.assignment.Key.Text)
	texture := Texture{Recolor: key, Colors: resolver.coatColors(own, colors)}
	isPicked := resolver.isPicked("texture", file)
	for _, placed := range run {
		at := emblem
		if placed.instance != nil {
			at = *placed.instance
		}
		instance := resolver.coatWidget(element.element, at, instanceKey, fmt.Sprint(placed.number))
		if placed.instance != nil {
			instance.add(blockOf(*placed.instance).statements...)
		}
		if !isPicked {
			resolver.addTexture(instance.element, coatPath(key, file), texture)
		}
	}
}

// resolveSub lists a sub under the root coat, once for each of its instances (once at the whole field where it writes
// none): its parent's statements under its own.
func (resolver *resolver) resolveSub(coat int, sub statement) {
	block := resolver.coatBlock(sub, 0)
	placed := false
	for _, field := range fields(sub) {
		if strings.EqualFold(field.assignment.Key.Text, instanceKey) {
			resolver.resolveCoatBlock(sub, block, coat, &field)
			placed = true
		}
	}
	if !placed {
		resolver.resolveCoatBlock(sub, block, coat, nil)
	}
}

// addTexture adds a coat texture to an element: the file at a path colored as the texture asks; a file nothing holds
// is listed.
func (resolver *resolver) addTexture(element int, path string, coloring Texture) {
	base, has := resolver.fileTexture(path)
	if !has {
		return
	}
	base.Recolor, base.Colors, base.Regions = coloring.Recolor, coloring.Colors, coloring.Regions
	textures := &resolver.list.Elements[element].Textures
	*textures = append(*textures, resolver.textureIndex(base))
}

// isPicked reports whether a key's value is what a list picks, which the game decides and the preview cannot, listed.
func (resolver *resolver) isPicked(key, value string) bool {
	if !strings.EqualFold(value, listPick) {
		return false
	}
	resolver.addMissing(listPick, key)
	return true
}

// CoatFile is the file a mod's new coat of arms is written in: the last of the mod's files that defines coats, else
// one named for the mod's folder in the folder its game keeps coats in (game.CoatFolders). The caller holds the
// workspace's read lock.
func CoatFile(ws *workspace.Session, modID string) (string, error) {
	modIndex := slices.IndexFunc(ws.Mods, func(mod workspace.Mod) bool { return mod.ID == modID })
	folder, hasFolder := game.CoatFolders[ws.Game.ID]
	if modIndex < 0 || !hasFolder {
		return "", fmt.Errorf("the open workspace has no mod %q that can hold a coat of arms", modID)
	}
	own := ""
	for _, file := range ws.Workspace.Files {
		definesCoats := slices.ContainsFunc(ws.Workspace.InFile(file.Path), func(def index.At) bool {
			return def.Occurrence().Database == game.CoatDatabases.Coats && def.Occurrence().Role == schema.Defines
		})
		if definesCoats && file.Origin == modID {
			own = file.Path
		}
	}
	if own != "" {
		return own, nil
	}
	mod := ws.Mods[modIndex]
	base := strings.Trim(notAlphanumeric.ReplaceAllString(strings.ToLower(filepath.Base(mod.Dir)), "_"), "_") + "_coats.txt"
	return filepath.Join(mod.Dir, filepath.FromSlash(folder), base), nil
}

// coatPath is the path of a coat texture's file, by the key that writes it.
func coatPath(key, file string) string {
	return game.CoatTextureFolders[key] + "/" + file
}

// isEmblem reports whether a statement writes an emblem.
func isEmblem(one statement) bool {
	return slices.ContainsFunc(emblemKeys, func(key string) bool { return strings.EqualFold(key, one.assignment.Key.Text) })
}

// isPart reports whether a statement writes an element of its own (an emblem, an instance or a sub), which its
// holder's properties leave out.
func isPart(one statement) bool {
	key := strings.ToLower(one.assignment.Key.Text)
	return isEmblem(one) || key == instanceKey || key == subKey
}
