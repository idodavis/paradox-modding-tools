// gui_layout.go is pass 2: the resolved tree placed at the request's size. A data model's rows are drawn once each,
// every container places its children by its layout class (rows and columns by the three branches of a box layout,
// a grid, a flow, a scroll area, or each child by its anchors), and the elements follow in paint order.

package draw

import (
	"math"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/game"
)

// unbounded is a maximum nothing declares: no widget grows to it.
const unbounded = 1e9

// item is one child of a row or column as the three branches see it.
type item struct {
	minimum, preferred, maximum float64
	stretch                     int
	expanding                   bool
}

// across and down are each anchor word's fraction of a box across it (left, hcenter, right) and down it (top,
// vcenter, bottom); center is both.
var (
	across = map[string]float64{"left": 0, "hcenter": 0.5, center: 0.5, "right": 1}
	down   = map[string]float64{"top": 0, "vcenter": 0.5, center: 0.5, "bottom": 1}
)

// place runs pass 2: the rows of every data model drawn, the root at its size, each widget's children by its layout
// class, pre-order, writing each widget's box; then the elements in paint order.
func place(root *widget, request Request) []Element {
	tree := instance(root, -1, request.HideUnknown)
	resolution := size{width: float64(request.Width), height: float64(request.Height)}
	tree.box = rootBox(tree, resolution)
	arrange(tree)
	return elementsOf(tree, 1, nil, []Element{})
}

// instance is a copy of a widget with the rows of its data model drawn: its template children once per row, each
// copy with its row's visibility, and with hide, a widget that is unknown laid out as hidden.
func instance(node *widget, row int, hide bool) *widget {
	copied := *node
	if row >= 0 && row < len(node.byRow) {
		copied.visibility = node.byRow[row]
	}
	if hide && copied.visibility == Unknown {
		copied.visibility = Hidden
	}
	copied.children = nil
	for _, child := range node.children {
		if !child.template {
			copied.children = append(copied.children, instance(child, row, hide))
		}
	}
	for index := range node.rows {
		for _, child := range node.children {
			if child.template {
				copied.children = append(copied.children, instance(child, index, hide))
			}
		}
	}
	return &copied
}

// rootBox is the root's box: its size (a percent of the resolution), else the resolution, anchored in it.
func rootBox(root *widget, resolution size) Box {
	own := preferredSize(root, resolution)
	if own.width == 0 {
		own.width = resolution.width
	}
	if own.height == 0 {
		own.height = resolution.height
	}
	return anchored(root, Box{Width: resolution.width, Height: resolution.height}, own)
}

// arrange places every widget's children inside its box by its layout class, then each child's own.
func arrange(node *widget) {
	switch node.class {
	case game.LayoutRow:
		placeBox(node, false)
	case game.LayoutColumn:
		placeBox(node, true)
	case game.LayoutGrid:
		placeGrid(node)
	case game.LayoutFlow:
		placeFlow(node)
	case game.LayoutScroll:
		placeScroll(node)
	case game.LayoutText, game.LayoutImage:
	default:
		placePlain(node)
	}
	for _, child := range node.children {
		arrange(child)
	}
}

// pair is a key's two numbers in pixels at the UI scale, a percent kept as a negative fraction; a single number is
// both, none is zero.
func (node *widget) pair(key string) size {
	numbers, has := node.numbers(key)
	if !has {
		return size{}
	}
	scaled := make([]float64, 2)
	for i := range scaled {
		scaled[i] = numbers[min(i, len(numbers)-1)]
		if scaled[i] > 0 {
			scaled[i] *= node.scale
		}
	}
	return size{width: scaled[0], height: scaled[1]}
}

// single is a key's one number in pixels at the UI scale; false where it is not written.
func (node *widget) single(key string) (float64, bool) {
	numbers, has := node.numbers(key)
	if !has {
		return 0, false
	}
	return numbers[0] * node.scale, true
}

// margins are the insets of a widget's content: margin = { x y } for left and right and top and bottom, then each
// side's own key.
func (node *widget) margins() (leading, top, trailing, bottom float64) {
	both := node.pair("margin")
	leading, trailing, top, bottom = both.width, both.width, both.height, both.height
	for key, side := range map[string]*float64{"margin_left": &leading, "margin_right": &trailing, "margin_top": &top, "margin_bottom": &bottom} {
		if value, has := node.single(key); has {
			*side = value
		}
	}
	return max(leading, 0), max(top, 0), max(trailing, 0), max(bottom, 0)
}

// inner is a widget's box less its margins: the room its children are placed in.
func (node *widget) inner() Box {
	leading, top, trailing, bottom := node.margins()
	return Box{
		X: node.box.X + leading, Y: node.box.Y + top,
		Width: max(node.box.Width-leading-trailing, 0), Height: max(node.box.Height-top-bottom, 0),
	}
}

// preferredSize is the widget's own size given the room its parent gives: the declared size, a fraction of the room,
// or what the widget holds; held by its minimum and maximum.
func preferredSize(node *widget, room size) size {
	declared := node.pair("size")
	preferred := size{width: sizeOf(declared.width, room.width), height: sizeOf(declared.height, room.height)}
	if auto, _ := node.scalar("autoresize"); strings.EqualFold(auto, yes) && node.text != nil {
		preferred = node.content
	}
	if preferred.width == 0 {
		preferred.width = contentOf(node, room, false)
	}
	if preferred.height == 0 {
		preferred.height = contentOf(node, room, true)
	}
	if node.class == game.LayoutImage && node.content.width > 0 {
		preferred = byRatio(preferred, node.content)
	}
	return clamped(node, preferred)
}

// sizeOf is one axis of a declared size: pixels, a fraction of the room, or zero where it is unset.
func sizeOf(declared, room float64) float64 {
	if declared < 0 {
		return -declared * room
	}
	return declared
}

// byRatio is an image's size where only one axis is declared: the other follows the texture's ratio.
func byRatio(preferred, texture size) size {
	switch {
	case preferred.width > 0 && preferred.height == 0:
		preferred.height = preferred.width * texture.height / texture.width
	case preferred.height > 0 && preferred.width == 0:
		preferred.width = preferred.height * texture.width / texture.height
	}
	return preferred
}

// clamped is a size held to the widget's minimum and maximum (zero is unset).
func clamped(node *widget, preferred size) size {
	minimum, maximum := node.pair("minimumsize"), node.pair("maximumsize")
	if value, has := node.single("min_width"); has {
		minimum.width = value
	}
	if value, has := node.single("max_width"); has {
		maximum.width = value
	}
	if maximum.width > 0 {
		preferred.width = min(preferred.width, maximum.width)
	}
	if maximum.height > 0 {
		preferred.height = min(preferred.height, maximum.height)
	}
	preferred.width, preferred.height = max(preferred.width, minimum.width), max(preferred.height, minimum.height)
	return preferred
}

// contentOf is what a widget holds on one axis where it declares no size: its text or texture, or its children by
// its class (a row sums widths and takes the tallest, a column the mirror, with spacing and margins); else zero.
func contentOf(node *widget, room size, vertical bool) float64 {
	axis := func(measured size) float64 {
		if vertical {
			return measured.height
		}
		return measured.width
	}
	switch node.class {
	case game.LayoutText, game.LayoutImage:
		return axis(node.content)
	case game.LayoutRow, game.LayoutColumn:
		stacked := (node.class == game.LayoutColumn) == vertical
		total, widest, count := 0.0, 0.0, 0
		for _, child := range node.children {
			if child.visibility == Hidden && ignoresHidden(node) {
				continue
			}
			length := axis(preferredSize(child, room))
			total, widest, count = total+length, max(widest, length), count+1
		}
		leading, top, trailing, bottom := node.margins()
		around := leading + trailing
		if vertical {
			around = top + bottom
		}
		if stacked {
			spacing, _ := node.single("spacing")
			return total + spacing*float64(max(count-1, 0)) + around
		}
		return widest + around
	}
	return 0
}

// ignoresHidden reports whether a box gives a hidden child no room (ignoreinvisible = yes).
func ignoresHidden(node *widget) bool {
	value, _ := node.scalar("ignoreinvisible")
	return strings.EqualFold(value, yes)
}

// anchored is the box a widget of a size takes inside a cell: the point of it its widgetanchor names on the point of
// the cell its parentanchor names, then moved by its position.
func anchored(node *widget, cell Box, own size) Box {
	parentAcross, parentDown := anchorOf(node, "parentanchor")
	selfAcross, selfDown := anchorOf(node, "widgetanchor")
	offset := node.pair("position")
	if value, has := node.single("position_x"); has {
		offset.width = value
	}
	if value, has := node.single("position_y"); has {
		offset.height = value
	}
	return Box{
		X:      cell.X + parentAcross*cell.Width - selfAcross*own.width + offset.width,
		Y:      cell.Y + parentDown*cell.Height - selfDown*own.height + offset.height,
		Width:  own.width,
		Height: own.height,
	}
}

// anchorOf is the fractions a key's anchor words name across and down; unnamed is zero.
func anchorOf(node *widget, key string) (acrossFraction, downFraction float64) {
	choices, _ := node.choices(key)
	for _, choice := range choices {
		if fraction, isNamed := across[choice]; isNamed {
			acrossFraction = fraction
		}
		if fraction, isNamed := down[choice]; isNamed {
			downFraction = fraction
		}
	}
	return acrossFraction, downFraction
}

// placePlain places each child by its anchors and position in the box of its parent.
func placePlain(node *widget) {
	cell := node.inner()
	for _, child := range node.children {
		child.box = anchored(child, cell, preferredSize(child, size{width: cell.Width, height: cell.Height}))
	}
}

// policyOf is the layout policy a widget writes for an axis (layoutpolicy_horizontal or _vertical), none where it
// names none.
func policyOf(node *widget, vertical bool) game.LayoutPolicy {
	key := "layoutpolicy_horizontal"
	if vertical {
		key = "layoutpolicy_vertical"
	}
	value, _ := node.scalar(key)
	return game.LayoutPolicies[strings.ToLower(value)]
}

// itemOf is a child as a row or column sees it along its axis: held to its preferred size unless its policy says it
// shrinks or grows.
func itemOf(child *widget, room size, vertical bool) item {
	preferred := preferredSize(child, room)
	length, least, most := preferred.width, child.pair("minimumsize").width, child.pair("maximumsize").width
	stretchKey := "layoutstretchfactor_horizontal"
	if vertical {
		length, least, most = preferred.height, child.pair("minimumsize").height, child.pair("maximumsize").height
		stretchKey = "layoutstretchfactor_vertical"
	}
	policy := policyOf(child, vertical)
	result := item{minimum: length, preferred: length, maximum: length, expanding: policy.Grows}
	if policy.Shrinks && !policy.Fixed {
		result.minimum = least
	}
	if policy.Grows && !policy.Fixed {
		result.maximum = unbounded
		if most > 0 {
			result.maximum = most
		}
	}
	if stretch, has := child.numbers(stretchKey); has {
		result.stretch = int(stretch[0])
	}
	return result
}

// placeBox lays a row's or column's children out along its direction by the branch the room falls in, and across it
// by each child's alignment and policy.
func placeBox(node *widget, vertical bool) {
	cell := node.inner()
	room := size{width: cell.Width, height: cell.Height}
	var visible []*widget
	for _, child := range node.children {
		if child.visibility != Hidden || !ignoresHidden(node) {
			visible = append(visible, child)
		}
	}
	items := make([]item, len(visible))
	hints, minimums := 0.0, 0.0
	for i, child := range visible {
		items[i] = itemOf(child, room, vertical)
		hints, minimums = hints+items[i].preferred, minimums+items[i].minimum
	}
	spacing, _ := node.single("spacing")
	along, cross := cell.Width, cell.Height
	if vertical {
		along, cross = cell.Height, cell.Width
	}
	available := along - spacing*float64(max(len(visible)-1, 0))
	var lengths []float64
	switch {
	case available < minimums:
		lengths = atMinimums(items)
	case available < hints:
		lengths = betweenMinimumAndHint(items, available)
	default:
		lengths = abovePreferred(items, available)
	}
	offset := 0.0
	for i, child := range visible {
		child.box = crossed(child, cell, lengths[i], offset, cross, vertical)
		offset += lengths[i] + spacing
	}
}

// crossed is a child's box in a row or column: its length along, at its offset, and across the room its alignment
// and policy leave it.
func crossed(child *widget, cell Box, length, offset, cross float64, vertical bool) Box {
	preferred := preferredSize(child, size{width: cell.Width, height: cell.Height})
	thick := preferred.height
	if vertical {
		thick = preferred.width
	}
	if policyOf(child, !vertical).Grows {
		thick = cross
	}
	acrossFraction, downFraction := anchorOf(child, "parentanchor")
	if vertical {
		return Box{X: cell.X + acrossFraction*(cross-thick), Y: cell.Y + offset, Width: thick, Height: length}
	}
	return Box{X: cell.X + offset, Y: cell.Y + downFraction*(cross-thick), Width: length, Height: thick}
}

// atMinimums gives every child its minimum; the box overflows.
func atMinimums(items []item) []float64 {
	out := make([]float64, len(items))
	for i, one := range items {
		out[i] = one.minimum
	}
	return out
}

// betweenMinimumAndHint shrinks each child from its preferred size in proportion to what it can give up.
func betweenMinimumAndHint(items []item, room float64) []float64 {
	out := make([]float64, len(items))
	hints, minimums := 0.0, 0.0
	for _, one := range items {
		hints, minimums = hints+one.preferred, minimums+one.minimum
	}
	short := hints - room
	for i, one := range items {
		out[i] = one.preferred
		if give := hints - minimums; give > 0 {
			out[i] -= (one.preferred - one.minimum) * short / give
		}
	}
	return out
}

// abovePreferred hands the extra room to the stretch factors, else to the expanding children, else to nobody, each
// capped at its maximum, repeated until no cap is hit.
func abovePreferred(items []item, room float64) []float64 {
	out, capped := make([]float64, len(items)), make([]bool, len(items))
	for i, one := range items {
		out[i] = one.preferred
	}
	for {
		extra, weights := room, 0.0
		for i, one := range items {
			extra -= out[i]
			if !capped[i] && one.stretch > 0 {
				weights += float64(one.stretch)
			}
		}
		byStretch := weights > 0
		if !byStretch {
			for i, one := range items {
				if !capped[i] && one.expanding {
					weights++
				}
			}
		}
		if weights == 0 || extra <= 0 {
			return out
		}
		hit := false
		for i, one := range items {
			if capped[i] || byStretch && one.stretch == 0 || !byStretch && !one.expanding {
				continue
			}
			weight := 1.0
			if byStretch {
				weight = float64(one.stretch)
			}
			if out[i] += extra * weight / weights; out[i] > one.maximum {
				out[i], capped[i], hit = one.maximum, true, true
			}
		}
		if !hit {
			return out
		}
	}
}

// placeGrid lays a grid's children out in its columns and rows at its cell size: a fixed grid by addcolumn and
// addrow, a dynamic one by its largest child, wrapping after datamodel_wrap columns.
func placeGrid(node *widget) {
	cell := node.inner()
	room := size{width: cell.Width, height: cell.Height}
	columnWidth, _ := node.single("addcolumn")
	rowHeight, _ := node.single("addrow")
	for _, child := range node.children {
		preferred := preferredSize(child, room)
		columnWidth, rowHeight = max(columnWidth, preferred.width), max(rowHeight, preferred.height)
	}
	if columnWidth == 0 {
		return
	}
	columns := int(cell.Width / columnWidth)
	if wrap, has := node.numbers("datamodel_wrap"); has && wrap[0] > 0 {
		columns = int(wrap[0])
	}
	columns = max(columns, 1)
	flipped, _ := node.scalar("flipdirection")
	rows := (len(node.children) + columns - 1) / columns
	for i, child := range node.children {
		column, row := i%columns, i/columns
		if strings.EqualFold(flipped, yes) {
			column, row = i/rows, i%rows
		}
		own := preferredSize(child, room)
		origin := Box{X: cell.X + float64(column)*columnWidth, Y: cell.Y + float64(row)*rowHeight}
		child.box = Box{X: origin.X, Y: origin.Y, Width: max(own.width, 0), Height: max(own.height, 0)}
	}
}

// placeFlow lays a flow's children out along its direction (horizontal unless it says vertical), each at its
// preferred size, wrapping at the box's size on that axis, spacing between them.
func placeFlow(node *widget) {
	cell := node.inner()
	room := size{width: cell.Width, height: cell.Height}
	choices, _ := node.choices("direction")
	vertical := slices.Contains(choices, "vertical")
	spacing, _ := node.single("spacing")
	along, line, thickest := 0.0, 0.0, 0.0
	limit := cell.Width
	if vertical {
		limit = cell.Height
	}
	for _, child := range node.children {
		if child.visibility == Hidden && ignoresHidden(node) {
			continue
		}
		own := preferredSize(child, room)
		length, thick := own.width, own.height
		if vertical {
			length, thick = own.height, own.width
		}
		if along > 0 && along+length > limit {
			along, line, thickest = 0, line+thickest+spacing, 0
		}
		if vertical {
			child.box = Box{X: cell.X + line, Y: cell.Y + along, Width: own.width, Height: own.height}
		} else {
			child.box = Box{X: cell.X + along, Y: cell.Y + line, Width: own.width, Height: own.height}
		}
		along, thickest = along+length+spacing, max(thickest, thick)
	}
}

// placeScroll lays a scroll area's content out at its own size, starting at the area's top left, clipped to the
// area: nothing is scrolled.
func placeScroll(node *widget) {
	cell := node.inner()
	room := size{width: cell.Width, height: cell.Height}
	for _, child := range node.children {
		own := preferredSize(child, room)
		if own.width == 0 && child.pair("size").width == 0 {
			own.width = cell.Width
		}
		child.box = Box{X: cell.X, Y: cell.Y, Width: own.width, Height: own.height}
	}
	area := node.box
	node.clip = &area
}

// elementsOf adds the widget's elements and its children's, in paint order, parents before children: one element a
// texture, or one bare where it has none, then one for the text. A hidden widget and what it holds have none.
func elementsOf(node *widget, alpha float64, clip *Box, out []Element) []Element {
	if node.visibility == Hidden {
		return out
	}
	alpha *= node.alpha
	box := node.box
	base := Element{Part: node.part, Box: box, Clip: clip, Alpha: alpha}
	if len(node.textures) == 0 {
		out = append(out, base)
	}
	for i := range node.textures {
		textured := base
		textured.Texture = &node.textures[i]
		out = append(out, textured)
	}
	if node.text != nil {
		texted := base
		texted.Text = alignedText(node, box)
		out = append(out, texted)
	}
	if node.clip != nil {
		clip = node.clip
	}
	for _, child := range node.children {
		out = elementsOf(child, alpha, clip, out)
	}
	return out
}

// alignedText is a text's lines placed in the box its align words name: left, hcenter or right across, top, vcenter
// or bottom down.
func alignedText(node *widget, box Box) *Text {
	choices, _ := node.choices("align")
	var acrossFraction, downFraction float64
	for _, choice := range choices {
		if fraction, isNamed := across[choice]; isNamed {
			acrossFraction = fraction
		}
		if fraction, isNamed := down[choice]; isNamed {
			downFraction = fraction
		}
	}
	aligned := *node.text
	aligned.Lines = make([]Line, len(node.text.Lines))
	block := 0.0
	if len(node.lineWidths) > 0 && len(node.text.Lines) > 0 {
		block = node.text.Lines[len(node.text.Lines)-1].Y + node.lineHeight
	}
	shiftDown := math.Max(box.Height-block, 0) * downFraction
	for i, line := range node.text.Lines {
		shifted := Line{Y: line.Y + shiftDown, Runs: slices.Clone(line.Runs)}
		if i < len(node.lineWidths) {
			shiftAcross := math.Max(box.Width-node.lineWidths[i], 0) * acrossFraction
			for index := range shifted.Runs {
				shifted.Runs[index].X += shiftAcross
			}
		}
		aligned.Lines[i] = shifted
	}
	return &aligned
}
