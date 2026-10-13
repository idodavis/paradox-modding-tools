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
// class, pre-order, writing each widget's box; then the elements in paint order, and the scene they are drawn on: a
// window's resolution, or the size of an entry drawn alone.
func place(root *widget, request Request, alone bool) ([]Element, size) {
	tree := instance(root)
	tree.hidden = false // the entry drawn by name is drawn whatever its own visible says
	resolution := size{width: float64(request.Width), height: float64(request.Height)}
	tree.box = rootBox(tree, resolution, alone)
	arrange(tree)
	scene := resolution
	if alone {
		scene = size{width: tree.box.Width, height: tree.box.Height}
	}
	return elementsOf(tree, 1, nil, []Element{}), scene
}

// instance is a copy of a widget with the rows of its data model drawn: its template children once per row.
func instance(node *widget) *widget {
	copied := *node
	copied.children = twins(node, false)
	for range node.rows {
		copied.children = append(copied.children, twins(node, true)...)
	}
	return &copied
}

// twins are copies of a widget's children, the rows' template ones or the others, each one's shared places pointing
// at the copies of its siblings.
func twins(node *widget, template bool) []*widget {
	copies := map[*widget]*widget{}
	var out []*widget
	for _, child := range node.children {
		if child.template == template {
			copies[child] = instance(child)
			out = append(out, copies[child])
		}
	}
	for child, twin := range copies {
		twin.either = nil
		for _, other := range child.either {
			if twinOf, isTwin := copies[other]; isTwin {
				twin.either = append(twin.either, twinOf)
			}
		}
	}
	return out
}

// rootBox is the root's box: a window's size (a percent of the resolution), else the resolution, anchored in it; an
// entry drawn alone is its own size at the origin, the resolution being the room its percents take.
func rootBox(root *widget, resolution size, alone bool) Box {
	own := preferredSize(root, resolution)
	if alone {
		return Box{Width: own.width, Height: own.height}
	}
	if own.width == 0 {
		own.width = resolution.width
	}
	if own.height == 0 {
		own.height = resolution.height
	}
	return anchored(root, Box{Width: resolution.width, Height: resolution.height}, own)
}

// arrange places every widget's children inside its box: a child that fills its parent over the whole box, the rest
// by the widget's layout class, a child that takes a sibling's place over that sibling; then each child's own.
func arrange(node *widget) {
	for _, child := range node.children {
		if fills(child) {
			child.box = anchored(child, node.box, preferredSize(child, size{width: node.box.Width, height: node.box.Height}))
		}
	}
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
	default:
		placePlain(node)
	}
	for _, child := range node.children {
		for _, other := range child.either {
			other.box = child.box
		}
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

// extent is a size key's two numbers (size, minimumsize, maximumsize): pixels at the UI scale, a percent as a negative
// fraction, and a number written negative is unset on its axis, as vanilla writes size = { -1 32 } under an expanding
// policy and maximumsize = { 400 -1 } for no maximum; a single number is both, none is zero.
func (node *widget) extent(key string) size {
	latest, has := node.last(key)
	if !has {
		return size{}
	}
	words := valuesOf(latest.assign.Value)
	if len(words) == 0 {
		return size{}
	}
	scaled := make([]float64, 2)
	for i := range scaled {
		word := words[min(i, len(words)-1)]
		number, isNumber := numberOf(word)
		switch {
		case !isNumber || number < 0 && !strings.HasSuffix(word, "%"):
			scaled[i] = 0
		case number > 0:
			scaled[i] = number * node.scale
		default:
			scaled[i] = number
		}
	}
	return size{width: scaled[0], height: scaled[1]}
}

// fills reports whether a child fills its parent instead of taking a place among its siblings: a background.
func fills(child *widget) bool {
	return game.Sizings[child.kind] == game.SizeParent
}

// placed are the children a widget lays out: those that take a place of their own among their siblings and are
// given room; a child that takes an earlier sibling's place is laid over it once the sibling is placed.
func placed(node *widget) []*widget {
	var out []*widget
	for _, child := range node.children {
		if !fills(child) && !child.alternate && !givesNoRoom(node, child) {
			out = append(out, child)
		}
	}
	return out
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
// or what the widget holds; held by its minimum and maximum. A widget whose place a sibling takes is as big as the
// bigger of them, so the place holds whichever the game shows.
func preferredSize(node *widget, room size) size {
	declared := node.extent("size")
	preferred := size{width: sizeOf(declared.width, room.width), height: sizeOf(declared.height, room.height)}
	if auto, _ := node.scalar("autoresize"); strings.EqualFold(auto, yes) && node.text != nil {
		preferred = textExtent(node, room.width)
	}
	if preferred.width == 0 {
		preferred.width = contentOf(node, room, false)
	}
	if preferred.height == 0 {
		preferred.height = contentOf(node, room, true)
	}
	if game.Sizings[node.kind] == game.SizeTexture && node.content.width > 0 {
		preferred = byRatio(preferred, node.content)
	}
	preferred = clamped(node, preferred)
	for _, other := range node.either {
		theirs := preferredSize(other, room)
		preferred = size{width: max(preferred.width, theirs.width), height: max(preferred.height, theirs.height)}
	}
	return preferred
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
	minimum, maximum := node.extent("minimumsize"), node.extent("maximumsize")
	for key, side := range map[string]*float64{"min_width": &minimum.width, "min_height": &minimum.height, "max_width": &maximum.width, "max_height": &maximum.height} {
		if value, has := node.single(key); has && value > 0 {
			*side = value
		}
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

// contentOf is what a widget finds on one axis where it declares no size, by its kind's sizing: the room it is given,
// its text broken at the width it wraps at, its texture, or what its children make with no room to take a percent of;
// zero for a kind with no row.
func contentOf(node *widget, room size, vertical bool) float64 {
	switch game.Sizings[node.kind] {
	case game.SizeParent:
		return axisOf(room, vertical)
	case game.SizeText:
		if node.text == nil {
			return 0
		}
		return axisOf(textExtent(node, room.width), vertical)
	case game.SizeTexture:
		return axisOf(node.content, vertical)
	case game.SizeChildren:
		return node.stackedContent(vertical)
	case game.SizeBounds:
		return axisOf(node.reach(), vertical) + node.around(vertical)
	case game.SizeCells:
		return axisOf(node.cellsContent(room), vertical) + node.around(vertical)
	}
	return 0
}

// axisOf is a size on one axis.
func axisOf(measured size, vertical bool) float64 {
	if vertical {
		return measured.height
	}
	return measured.width
}

// around is the margins on both sides of an axis.
func (node *widget) around(vertical bool) float64 {
	leading, top, trailing, bottom := node.margins()
	if vertical {
		return top + bottom
	}
	return leading + trailing
}

// stackedContent is what a row's or column's children make on an axis: along the box their sizes summed with the
// spacing between and the margins around, across it the largest.
func (node *widget) stackedContent(vertical bool) float64 {
	stacked := (node.class == game.LayoutColumn) == vertical
	total, widest, count := 0.0, 0.0, 0
	for _, child := range placed(node) {
		length := axisOf(preferredSize(child, size{}), vertical)
		total, widest, count = total+length, max(widest, length), count+1
	}
	if stacked {
		spacing, _ := node.single("spacing")
		return total + spacing*float64(max(count-1, 0)) + node.around(vertical)
	}
	return widest + node.around(vertical)
}

// reach is how far a widget's children extend from its origin, each placed by its anchors in an empty cell: the
// origin and every box they take.
func (node *widget) reach() size {
	var low, high size
	for _, child := range placed(node) {
		box := anchored(child, Box{}, preferredSize(child, size{}))
		low = size{width: min(low.width, box.X), height: min(low.height, box.Y)}
		high = size{width: max(high.width, box.X+box.Width), height: max(high.height, box.Y+box.Height)}
	}
	return size{width: high.width - low.width, height: high.height - low.height}
}

// cellsContent is the size a grid's cells or a flow's children take where a room limits their rows: its largest cell
// by its columns and rows, or the flow's children laid along their direction. The limit is the room, held to the
// widget's own size and maximum, so what is measured here is what is placed inside the box it gets.
func (node *widget) cellsContent(room size) size {
	limit := room
	for _, own := range []size{node.extent("size"), node.extent("maximumsize")} {
		if own.width > 0 && (limit.width == 0 || own.width < limit.width) {
			limit.width = own.width
		}
		if own.height > 0 && (limit.height == 0 || own.height < limit.height) {
			limit.height = own.height
		}
	}
	children := placed(node)
	if node.class == game.LayoutFlow {
		var extent size
		for _, box := range flowBoxes(node, children, size{}, limit) {
			extent = size{width: max(extent.width, box.X+box.Width), height: max(extent.height, box.Y+box.Height)}
		}
		return extent
	}
	cell, columns := gridShape(node, children, size{}, limit.width)
	if columns == 0 {
		return size{}
	}
	count := len(children)
	return size{
		width:  float64(min(columns, count)) * cell.width,
		height: float64((count+columns-1)/columns) * cell.height,
	}
}

// ignoresHidden reports whether a box gives a hidden child no room (ignoreinvisible = yes).
func ignoresHidden(node *widget) bool {
	value, _ := node.scalar("ignoreinvisible")
	return strings.EqualFold(value, yes)
}

// givesNoRoom reports whether a box holds a child as though it were not there: it is hidden and the box ignores
// what is.
func givesNoRoom(node, child *widget) bool {
	return child.hidden && ignoresHidden(node)
}

// anchored is the box a widget of a size takes inside a cell: the point of it its widgetanchor names (its
// parentanchor where it names none) on the point of the cell its parentanchor names, then moved by its position.
func anchored(node *widget, cell Box, own size) Box {
	parentAcross, parentDown := anchorOf(node, "parentanchor")
	selfAcross, selfDown := parentAcross, parentDown
	if _, named := node.choices("widgetanchor"); named {
		selfAcross, selfDown = anchorOf(node, "widgetanchor")
	}
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

// placePlain places each child by its anchors and position in the box of its parent, a row or column whose children
// expand across it taking the parent's room.
func placePlain(node *widget) {
	cell := node.inner()
	room := size{width: cell.Width, height: cell.Height}
	for _, child := range placed(node) {
		child.box = anchored(child, cell, spanned(child, preferredSize(child, room), room))
	}
}

// spanned is a row's or column's size under a parent that does not lay it out: on an axis it declares no size for,
// where one of its children expands or takes a percent, it takes the room, as a window's main column does; on any
// other axis its own size. Vanilla CK3 writes 549 such boxes, 4 with a size, 366 with such a child, and anchors or
// positions 23, so a footer row of buttons keeps its own size and its anchor.
func spanned(child *widget, own, room size) size {
	if child.class != game.LayoutRow && child.class != game.LayoutColumn {
		return own
	}
	declared := child.extent("size")
	if declared.width == 0 && reaches(child, false) {
		own.width = room.width
	}
	if declared.height == 0 && reaches(child, true) {
		own.height = room.height
	}
	return clamped(child, own)
}

// reaches reports whether a child of a box expands or takes a percent on an axis.
func reaches(node *widget, vertical bool) bool {
	for _, child := range placed(node) {
		if policyOf(child, vertical).Grows || axisOf(child.extent("size"), vertical) < 0 {
			return true
		}
	}
	return false
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
	length, least, most := preferred.width, child.extent("minimumsize").width, child.extent("maximumsize").width
	stretchKey := "layoutstretchfactor_horizontal"
	if vertical {
		length, least, most = preferred.height, child.extent("minimumsize").height, child.extent("maximumsize").height
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
	visible := placed(node)
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

// gridShape is a grid's cell size and its columns across a width: the cell is addcolumn and addrow, or its largest
// child at a room's percents; the columns are datamodel_wrap's or as many cells as the width holds, one at least. A
// grid with no cell has no columns.
func gridShape(node *widget, children []*widget, room size, width float64) (cell size, columns int) {
	cell.width, _ = node.single("addcolumn")
	cell.height, _ = node.single("addrow")
	for _, child := range children {
		preferred := preferredSize(child, room)
		cell = size{width: max(cell.width, preferred.width), height: max(cell.height, preferred.height)}
	}
	if cell.width == 0 {
		return cell, 0
	}
	columns = int(width / cell.width)
	if wrap, has := node.numbers("datamodel_wrap"); has && wrap[0] > 0 {
		columns = int(wrap[0])
	}
	return cell, max(columns, 1)
}

// placeGrid lays a grid's children out in its columns and rows at its cell size: a fixed grid by addcolumn and
// addrow, a dynamic one by its largest child, wrapping after datamodel_wrap columns.
func placeGrid(node *widget) {
	inner := node.inner()
	room := size{width: inner.Width, height: inner.Height}
	children := placed(node)
	cell, columns := gridShape(node, children, room, inner.Width)
	if columns == 0 {
		return
	}
	flipped, _ := node.scalar("flipdirection")
	rows := (len(children) + columns - 1) / columns
	for i, child := range children {
		column, row := i%columns, i/columns
		if strings.EqualFold(flipped, yes) {
			column, row = i/rows, i%rows
		}
		own := preferredSize(child, room)
		child.box = Box{
			X: inner.X + float64(column)*cell.width, Y: inner.Y + float64(row)*cell.height,
			Width: max(own.width, 0), Height: max(own.height, 0),
		}
	}
}

// flowBoxes are the boxes of the children a flow places, from its origin and in their order: each at its preferred
// size in a room, along the flow's direction (horizontal unless it says vertical) with spacing between, wrapping
// where the next would pass a limit room's size on that axis; a limit of zero wraps never.
func flowBoxes(node *widget, children []*widget, room, limit size) []Box {
	choices, _ := node.choices("direction")
	vertical := slices.Contains(choices, "vertical")
	spacing, _ := node.single("spacing")
	along, line, thickest := 0.0, 0.0, 0.0
	edge := axisOf(limit, vertical)
	boxes := make([]Box, len(children))
	for i, child := range children {
		own := preferredSize(child, room)
		length, thick := own.width, own.height
		if vertical {
			length, thick = own.height, own.width
		}
		if along > 0 && edge > 0 && along+length > edge {
			along, line, thickest = 0, line+thickest+spacing, 0
		}
		boxes[i] = Box{X: along, Y: line, Width: own.width, Height: own.height}
		if vertical {
			boxes[i].X, boxes[i].Y = line, along
		}
		along, thickest = along+length+spacing, max(thickest, thick)
	}
	return boxes
}

// placeFlow lays a flow's children out along its direction, each at its preferred size, wrapping at the box's size
// on that axis.
func placeFlow(node *widget) {
	inner := node.inner()
	room := size{width: inner.Width, height: inner.Height}
	children := placed(node)
	for i, box := range flowBoxes(node, children, room, room) {
		children[i].box = Box{X: inner.X + box.X, Y: inner.Y + box.Y, Width: box.Width, Height: box.Height}
	}
}

// placeScroll lays a scroll area's content out at its own size, starting at the area's top left, clipped to the
// area: nothing is scrolled. A content with no width takes the area's.
func placeScroll(node *widget) {
	cell := node.inner()
	room := size{width: cell.Width, height: cell.Height}
	for _, child := range placed(node) {
		own := spanned(child, preferredSize(child, room), room)
		if own.width == 0 && child.extent("size").width == 0 {
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
	if node.hidden {
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

// alignedText is a text's lines, broken at the width its box gives it, placed in the box as its align words say:
// left, hcenter or right across, top, vcenter or bottom down.
func alignedText(node *widget, box Box) *Text {
	acrossFraction, downFraction := anchorOf(node, "align")
	lines, widths := linesAt(node, wrapAt(node, box.Width))
	aligned := *node.text
	aligned.Lines = lines
	block := lines[len(lines)-1].Y + node.lineHeight
	shiftDown := math.Max(box.Height-block, 0) * downFraction
	for i := range lines {
		lines[i].Y += shiftDown
		shiftAcross := math.Max(box.Width-widths[i], 0) * acrossFraction
		for index := range lines[i].Runs {
			lines[i].Runs[index].X += shiftAcross
		}
	}
	return &aligned
}

// wrapAt is the width a text's lines break at in a given width: its own limit (max_width, else its declared width),
// else the width it is given where it is multiline; zero breaks never.
func wrapAt(node *widget, width float64) float64 {
	if node.textLimit > 0 || !node.multiline {
		return node.textLimit
	}
	return width
}

// textExtent is what a text measures in a width: its lines broken where it wraps, the widest and their height. A
// multiline text with no width of its own, measured with no room (its parent is sized by its content), has no width
// yet: it measures its longest word, one line high, so a column it sits in is as wide as its other children.
func textExtent(node *widget, width float64) size {
	if node.multiline && node.textLimit == 0 && width == 0 {
		longest := 0.0
		for _, word := range node.pieces {
			longest = max(longest, word.advance)
		}
		return size{width: longest, height: node.lineHeight}
	}
	lines, widths := linesAt(node, wrapAt(node, width))
	return size{width: slices.Max(widths), height: float64(len(lines)) * node.lineHeight}
}
