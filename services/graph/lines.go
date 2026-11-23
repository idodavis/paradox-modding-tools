// lines.go reads a building line from the game's links: the lines that link
// to a good, the buildings a production method belongs to, what a level
// writes through its production methods, and where a good is found.

package graph

import (
	"cmp"
	"strconv"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/lang"
)

// Source is where a good is found.
type Source struct {
	// Gathering is how it is gathered, in the game's word, as Mining. It is
	// "" where the game says none.
	Gathering string `json:"gathering"`

	// Count is how many places hold it, 0 where the game lists none. Places
	// is what the game calls them, as counties.
	Count  int    `json:"count"`
	Places string `json:"places"`
}

// listLines returns the first levels of the building lines that have a link
// of the given relation to a good or to a category it is in, each once, in
// the order the links are found. What is part of no building, as a pop need,
// is a line of its own.
func (d *draft) listLines(good, says string) []string {
	var lines []string
	for _, id := range d.model.listTargets(good) {
		for _, edge := range d.model.listTo(id, says) {
			for _, line := range d.listHolders(edge.From) {
				lines = appendNew(lines, line)
			}
		}
	}

	return lines
}

// listHolders returns the first levels of the lines of the buildings a
// definition is a part of, or the definition itself where it is part of no
// building.
func (d *draft) listHolders(id string) []string {
	buildings := d.listBuildings(id)
	if len(buildings) == 0 {
		return []string{id}
	}

	holders := make([]string, len(buildings))
	for position, building := range buildings {
		holders[position] = d.model.listLine(building)[0]
	}

	return holders
}

// listBuildings returns the buildings a definition is, or is a part of: a
// production method's group's building, or the buildings that name the
// method. It returns none for a definition that is part of no building.
func (d *draft) listBuildings(id string) []string {
	if d.typeOf(id) == lang.TypeBuilding {
		return []string{id}
	}

	var buildings []string
	for _, edge := range d.model.listFrom(id, saysGroup) {
		for _, building := range d.listBuildings(edge.To) {
			buildings = appendNew(buildings, building)
		}
	}

	return buildings
}

// listLevelEdges returns the links of a relation that a level writes, itself
// or through what is a part of it, as its production methods.
func (d *draft) listLevelEdges(level, says string) []Edge {
	var edges []Edge
	for _, member := range d.model.listMembers([]string{level}) {
		edges = append(edges, d.model.listFrom(member, says)...)
	}

	return edges
}

// gatherings holds, for each game that writes how a raw good is gathered, the
// key of the good that says it and the value the game takes where it is left
// out. EU5's goods write method = mining, and the readme of its goods says a
// good with no method farms.
var gatherings = map[string]struct{ Key, Default string }{game.EU5: {Key: "method", Default: "farming"}}

// foundPlaces holds what each game calls the places a good is found in, and
// the key of the list where a definition lists them as values: CK3's common/
// trade/county_potential_trade_goods/county_potential_trade_goods.txt:68
// writes trade_good_wood = { counties = { c_onega_BJA … } }. A game with no
// such list counts the places that name the good.
var foundPlaces = map[string]struct{ Places, List string }{
	game.CK3:  {Places: "counties", List: "counties"},
	game.Vic3: {Places: "state regions"},
	game.EU5:  {Places: "locations"},
}

// makeSources returns where a good is found, one source for each of its
// links to a place. A link that counts its places gives how many; one to a
// definition that lists them gives the length of its list.
func (d *draft) makeSources(good string) []Source {
	row := foundPlaces[d.ws.Game.ID]
	sources := []Source{}
	for _, edge := range d.model.listFrom(good, saysResource) {
		source := Source{Gathering: d.findGathering(good), Places: row.Places}
		source.Count, _ = strconv.Atoi(edge.Amount)
		if row.List != "" {
			source.Count = d.countValues(edge.To, row.List)
		}

		sources = append(sources, source)
	}

	return sources
}

// findGathering returns how a raw good is gathered, as the game names it,
// such as Mining. It returns "" for a game that says nothing of it, and for a
// good that cannot be read.
func (d *draft) findGathering(good string) string {
	row, isWritten := gatherings[d.ws.Game.ID]
	tree, statement, isRead := d.findStatement(good)
	if !isWritten || !isRead {
		return ""
	}

	value, hasValue := tree.FindChildValue(statement, row.Key)
	if !hasValue {
		value = row.Default
	}

	text, _, _ := d.ws.LookupText(d.mods, value)

	return cmp.Or(text, value)
}

// countValues returns how many values the block under a key of a definition
// holds. It returns 0 for a definition that cannot be read or has no such
// block.
func (d *draft) countValues(id, key string) int {
	tree, statement, isRead := d.findStatement(id)
	if !isRead {
		return 0
	}

	block, hasBlock := tree.FindChild(statement, key)
	if !hasBlock {
		return 0
	}

	count := 0
	for range tree.Children(block) {
		count++
	}

	return count
}
