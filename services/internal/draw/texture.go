// texture.go is a widget's textures: its own and each modify_texture laid on it, read as the game's sprite shader
// reads them, and the file a texture binding draws found from what the scan holds.

package draw

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/index"
	"paradox-modding-tools/services/internal/jomini"
	"paradox-modding-tools/services/internal/schema"
	"paradox-modding-tools/services/internal/texture"
)

// The blend a texture draws in unless written, and the two that draw nothing of their own.
const (
	blendNormal        = "normal"
	blendMask          = "mask"
	blendAlphaMultiply = "alphamultiply"
)

// blendModes is each blend_mode word as the page draws it: a CSS blend mode, or one of the two that draw nothing of
// their own: alphamultiply masks the widget's texture, mask scales every overlay after it.
var blendModes = map[string]string{
	blendNormal: blendNormal, "overlay": "overlay", "colordodge": "color-dodge", "multiply": "multiply",
	"add": "plus-lighter", "lighten": "lighten", "darken": "darken", blendAlphaMultiply: blendAlphaMultiply,
	blendMask: blendMask,
}

// layers returns a widget's textures by index in the list: its own, then each modify_texture in the order the shader
// applies them (a mask scales every overlay after it); one written visible = no is not laid on.
func (resolver *guiResolver) layers(widget *widget) []int {
	own, isTextured := resolver.textureOf(widget)
	if !isTextured {
		return []int{}
	}
	var overlays []int
	mask := -1
	for _, statement := range widget.statements {
		if !strings.EqualFold(statement.assignment.Key.Text, "modify_texture") {
			continue
		}
		modifier := resolver.expanded(widget, statement)
		visible, _ := modifier.scalar("visible")
		layer, isLayer := resolver.textureOf(modifier)
		switch {
		case !isLayer || isNo(visible):
		case layer.Blend == blendAlphaMultiply:
			layer.Mask = own.Mask
			own.Mask = resolver.textureIndex(layer)
		case layer.Blend == blendMask:
			mask = resolver.textureIndex(layer)
		default:
			if alpha, has := modifier.numbers("alpha"); has {
				layer.Alpha = min(max(alpha[0], 0), 1)
			}
			layer.Mask = mask
			overlays = append(overlays, resolver.textureIndex(layer))
		}
	}
	return append([]int{resolver.textureIndex(own)}, overlays...)
}

// textureIndex returns a texture's index in the list, listing it the first time it is drawn.
func (resolver *guiResolver) textureIndex(texture Texture) int {
	ordinal, isKnown := resolver.textureIndexes[texture]
	if !isKnown {
		ordinal = len(resolver.list.Textures)
		resolver.textureIndexes[texture] = ordinal
		resolver.list.Textures = append(resolver.list.Textures, texture)
	}
	return ordinal
}

// textureOf returns the texture a widget (or a modify_texture) draws: its file's route and size with the defaults, then
// its sprite words; a binding draws the file pictureOf finds. False for none; a file nothing holds is listed.
func (resolver *guiResolver) textureOf(holder *widget) (Texture, bool) {
	path, has := holder.scalar("texture")
	if !has {
		return Texture{}, false
	}
	if isBinding(path) {
		if path = resolver.pictureOf(holder, path); path == "" {
			return Texture{}, false
		}
	}
	base, isKnown := resolver.pathTextures[path]
	if !isKnown {
		if file, hasFile := resolver.fileOf(path); hasFile {
			if facts, err := texture.Info(file); err == nil {
				base = Texture{
					URL: resolver.urlOf(file), Width: facts.Width, Height: facts.Height, Density: 1, TintAlpha: 1, Blend: blendNormal,
					Alpha: 1, Mask: -1,
				}
			}
		}
		resolver.pathTextures[path] = base
	}
	if base.URL == "" {
		resolver.addMissing("texture", path)
		return Texture{}, false
	}
	resolver.sprite(&holder.properties, &base)
	return base, true
}

// sprite applies the sprite keys a widget writes to a texture: density, the frame of a sheet, border, tiling, mirror,
// fit, tint and blend; a blend the page cannot draw is listed and drawn normal.
func (resolver *guiResolver) sprite(properties *properties, texture *Texture) {
	if density, has := properties.numbers("texture_density"); has && density[0] > 0 {
		texture.Density = density[0]
	}
	if width, height, isCut := cellOf(properties, *texture); isCut {
		frame := 0
		for _, key := range []string{"frame", "upframe"} {
			if numbers, has := properties.numbers(key); has {
				frame = max(int(numbers[0])-1, 0)
				break
			}
		}
		columns := texture.Width / width
		texture.URL += fmt.Sprintf("&frame=%d,%d,%d,%d", frame%columns*width, frame/columns*height, width, height)
		texture.Width, texture.Height = width, height
	}
	texture.Border = borderOf(properties)
	if choices, has := properties.choices("spritetype"); has {
		texture.IsTiled = slices.Contains(choices, "corneredtiled")
	}
	if choices, has := properties.choices("mirror"); has {
		texture.IsMirrored = slices.Contains(choices, "horizontal") || slices.Contains(choices, yes)
	}
	if choices, has := properties.choices("fittype"); has {
		texture.Fit = choices[0]
	}
	for _, key := range []string{colorKey, "tintcolor"} {
		if tint, has := resolver.tintOf(properties, key); has {
			texture.Tint, texture.TintAlpha = tint.ToHex(), tint.A
		}
	}
	if choices, has := properties.choices("blend_mode"); has {
		if blend, isKnown := blendModes[choices[0]]; isKnown {
			texture.Blend = blend
		} else {
			resolver.addMissing("blend", choices[0])
		}
	}
}

// cellOf returns the size in the image's pixels of the frames a sheet is cut into: its framesize times the density,
// else the image over its frame_grid; false for neither, or a frame larger than the image.
func cellOf(properties *properties, texture Texture) (int, int, bool) {
	var width, height int
	cell, isSized := properties.numbers("framesize")
	grid, isGrid := properties.numbers("frame_grid")
	switch {
	case isSized && len(cell) == 2:
		width, height = int(cell[0]*texture.Density), int(cell[1]*texture.Density)
	case isGrid && len(grid) == 2 && grid[0] >= 1 && grid[1] >= 1:
		width, height = texture.Width/int(grid[0]), texture.Height/int(grid[1])
	}
	return width, height, width >= 1 && height >= 1 && width <= texture.Width && height <= texture.Height
}

// borderOf returns a sprite's insets top, right, bottom and left: spriteborder as one value for every side or a
// horizontal and a vertical, a side's own key over it.
func borderOf(properties *properties) [4]int {
	var border [4]int
	if sides, has := properties.numbers("spriteborder"); has {
		switch len(sides) {
		case 1:
			border = [4]int{int(sides[0]), int(sides[0]), int(sides[0]), int(sides[0])}
		case 2:
			border = [4]int{int(sides[1]), int(sides[0]), int(sides[1]), int(sides[0])}
		}
	}
	for i, key := range []string{"spriteborder_top", "spriteborder_right", "spriteborder_bottom", "spriteborder_left"} {
		if side, has := properties.numbers(key); has {
			border[i] = int(side[0])
		}
	}
	return border
}

// tintOf returns a key's last color: a color block, or an answered call naming an entry that is one; false for none.
func (resolver *guiResolver) tintOf(properties *properties, key string) (game.RGBA, bool) {
	last, has := properties.last(key)
	if !has {
		return game.RGBA{}, false
	}
	value := last.assignment.Value
	if text, _ := properties.scalar(key); text != "" {
		if define, isAnswered := resolver.answered(text); isAnswered {
			value = define.assignment.Value
		}
	}
	tag, parts, isColor := jomini.ColorParts(value)
	color, isKnown := game.ParseColor(tag, parts)
	return color, isColor && isKnown
}

// pictureOf returns the file a texture binding draws: the entry a quoted key names, in the call or the widget's data
// context, pictured by its GUIPictureKeys field as written or under an icon folder; "" where none is found.
func (resolver *guiResolver) pictureOf(widget *widget, binding string) string {
	install := resolver.ws.Install
	steps := jomini.ParseCall(binding, 0)
	if len(steps) == 0 {
		return ""
	}
	dataType, key := install.Schema.CallLeadsTo(steps[:len(steps)-1], ""), literalKey(steps)
	context := jomini.ParseCall(widget.dataContext, 0)
	if key == "" && len(context) > 0 && install.Schema.CallLeadsTo(context, "") == dataType {
		key = literalKey(context)
	}
	if key == "" {
		return ""
	}
	memoKey := dataType + "|" + key
	if picture, isKnown := resolver.pictures[memoKey]; isKnown {
		return picture
	}
	path := key
	if database := resolver.ws.Install.Schema.ResolveTypeDatabase(dataType); database != "" {
		if entry, isDefined := resolver.entryProperties(database, key); isDefined {
			for _, pictureKey := range game.GUIPictureKeys {
				if file, has := entry.scalar(pictureKey); has {
					path = file
					break
				}
			}
		}
	}
	folders := resolver.folders()
	own := strings.ToUpper(schema.SnakeCase(dataType)) + game.GUIIconPathSuffix
	candidates := []string{path}
	if folder, has := folders[own]; has {
		candidates = append(candidates, folder+"/"+path)
	}
	for _, define := range slices.Sorted(maps.Keys(folders)) {
		if define != own {
			candidates = append(candidates, folders[define]+"/"+path)
		}
	}
	picture := ""
	for _, candidate := range candidates {
		if _, hasFile := resolver.fileOf(candidate); hasFile {
			picture = candidate
			break
		}
	}
	resolver.pictures[memoKey] = picture
	return picture
}

// iconFolders returns the folders the icon path defines name, by define.
func (resolver *guiResolver) iconFolders() map[string]string {
	folders := map[string]string{}
	for _, id := range index.Names(resolver.ws.Indexes(), game.DefinesDatabase) {
		if !strings.HasSuffix(id, game.GUIIconPathSuffix) {
			continue
		}
		define, isDefined := resolver.definition(schema.Occurrence{Database: game.DefinesDatabase, ID: id})
		if !isDefined {
			continue
		}
		if scalar, isScalar := define.assignment.Value.(*jomini.Scalar); isScalar {
			folders[id] = strings.TrimSuffix(scalar.Text, "/")
		}
	}
	return folders
}

// literalKey returns the first quoted argument of a call; "" where it writes none.
func literalKey(steps []jomini.CallStep) string {
	for _, step := range steps {
		for _, arg := range step.Args {
			if arg.Quoted {
				return arg.Text
			}
		}
	}
	return ""
}
