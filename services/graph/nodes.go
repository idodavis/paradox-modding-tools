// nodes.go makes the node of a good, a building, a production method, a
// technology or a group, with the group and era the game's links give it.

package graph

import (
	"fmt"
	"slices"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/lang"
)

// lookupNode returns the node of the definition with an id, with its picture,
// its painting, the group it is listed under and, for a technology, its era.
// It is made once. A name the game keeps no definition of is a node with only
// its name.
func (d *draft) lookupNode(id string) Node {
	if node, isKnown := d.known[id]; isKnown {
		return node
	}

	node := Node{ID: id, Database: toDatabase(id), Name: toReference(id).Name}
	if site, err := resolveNode(d.ws, d.mods, id); err == nil {
		node = makeNode(d.ws, d.mods, site, 0)
		node.Picture, node.Silhouette = findPicture(d.ws, d.mods, site)
		node.Painting = findPainting(d.ws, d.mods, site)
		node.Group = d.findGroupID(id)
		node.Title = d.describeEra(node)
		switch node.Type {
		case lang.TypeTechnology:
			node.Era = d.findEra(id)

		case lang.TypeEra:
			if tree, statement, isRead := d.findStatement(id); isRead {
				node.Year, _ = tree.FindChildValue(statement, eraYears[d.ws.Game.ID])
			}
		}
	}

	d.known[id] = node

	return node
}

// build returns the node of the definition with an id, with its picture and
// its group. It returns false for an id no definition has.
func (d *draft) build(id string) (Node, bool) {
	_, err := resolveNode(d.ws, d.mods, id)

	return d.lookupNode(id), err == nil
}

// eraYears holds, for each game whose eras write the year they start in, the
// key of that year. Line 7 of CK3's common/culture/eras/00_culture_eras.txt
// writes year = 843.
var eraYears = map[string]string{game.CK3: "year"}

// eraTitles holds how a game whose eras have no text of their own words one by
// its number: Vic3's interfaces_l_english.yml:4240 writes Era [Era.GetNumber].
var eraTitles = map[string]string{game.Vic3: "Era %d"}

// describeEra returns the title of a node: its own, or for an era of a game
// in eraTitles, the game's words for its number.
func (d *draft) describeEra(node Node) string {
	title, isNumbered := eraTitles[d.ws.Game.ID]
	if !isNumbered || node.Type != lang.TypeEra {
		return node.Title
	}

	return fmt.Sprintf(title, slices.Index(d.listEras(), node.ID)+1)
}

// findEra returns the id of the era or age a technology is in, "" if it is in
// none.
func (d *draft) findEra(id string) string {
	if edges := d.model.listFrom(id, saysEra); len(edges) > 0 {
		return edges[0].To
	}

	return ""
}

// typeOf returns the type of node the definition with an id is, "" if the game
// gives it none.
func (d *draft) typeOf(id string) lang.Type {
	return findNodeType(d.ws.Schema, toDatabase(id))
}

// listEras returns the ids of the game's eras in the order its files define
// them.
func (d *draft) listEras() []string {
	if d.eras == nil {
		d.eras = d.listDefinitions(lang.TypeEra)
	}

	return d.eras
}

// listDefinitions returns the ids of every definition the game keeps of a
// type of node, database by database.
func (d *draft) listDefinitions(definitionType lang.Type) []string {
	databases := listTypedDatabases(d.ws, func(drawn lang.Type) bool { return drawn == definitionType })
	ids := []string{}
	for _, database := range databases {
		for _, winner := range d.mods.ListKept(d.ws.Schema, database) {
			ids = append(ids, toID(d.mods, winner.Site))
		}
	}

	return ids
}
