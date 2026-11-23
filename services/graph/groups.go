// groups.go finds the group a definition is listed under: the group the
// game's links give it, or the group its database writes as a plain value.

package graph

import (
	"slices"
	"strings"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/lang"
)

// groupTypes holds the type of group each type of node is in: a building in
// a district, a building group or a category, a Vic3 production method in
// its group, a Vic3 building group in its parent, a CK3 good in a category.
var groupTypes = map[lang.Type]lang.Type{
	lang.TypeBuilding:         lang.TypeBuildingGroup,
	lang.TypeProductionMethod: lang.TypeProductionMethodGroup,
	lang.TypeBuildingGroup:    lang.TypeBuildingGroup,
	lang.TypeGood:             lang.TypeGoodCategory,
}

// groupMark is the statement that marks, among the groups a definition is
// in, the one it is listed under.
type groupMark struct{ Database, Key, Value string }

// groupMarks holds the mark of each game that has one. CK3's
// _trade_good_categories.info:14-17 says a trade good is in one category
// that writes need = yes, and may be in several that do not.
var groupMarks = map[string]groupMark{
	game.CK3: {Database: "trade/trade_good_categories", Key: "need", Value: lang.Yes},
}

// writtenGroup is where one game's database writes a definition's group as a
// plain value, which no definition has.
type writtenGroup struct {
	Game     string
	Database string

	// Key holds the value, as category = staple. Default is the value the
	// game takes where the key is left out, "" where it takes none.
	Key     string
	Default string

	// Titles holds the localization key of each value's name. It is nil
	// where the game names a value by the value itself.
	Titles map[string]string

	// MarkKey and MarkValue are a statement a definition must write to be
	// listed under its value; one that does not is listed under Unmarked.
	// They are "" where every definition is listed under its value.
	MarkKey   string
	MarkValue string
	Unmarked  string
}

// writtenGroups are the groups each game writes as a plain value.
var writtenGroups = []writtenGroup{
	// common/goods/00_goods.txt:12 writes category = military, and the
	// market's filter names it at localization/english/
	// interfaces_l_english.yml:10363
	{Game: game.Vic3, Database: "goods", Key: "category", Titles: map[string]string{
		"staple": "STAPLE_FILTER_NAME", "industrial": "INDUSTRIAL_FILTER_NAME",
		"luxury": "LUXURY_FILTER_NAME", "military": "MILITARY_FILTER_NAME",
	}},

	// in_game/common/goods/02_produced_goods.txt:2 writes category =
	// produced, named by the same key at main_menu/localization/english/
	// goods_l_english.yml:4
	{Game: game.EU5, Database: "goods", Key: "category", Titles: map[string]string{
		"raw_material": "raw_material", "produced": "produced",
	}},

	// common/districts/_districts.info:27-31 says type is the slot a district
	// takes, regular where it is left out; common/districts/
	// resource_districts.txt:36 writes type = resource, which that file's
	// notes leave out. The game's concepts name each.
	{Game: game.CK3, Database: "districts", Key: "type", Default: "regular", Titles: map[string]string{
		"regular": "game_concept_standard_districts", "special": "game_concept_special_districts",
		"resource": "game_concept_resource_districts", "holding": "game_concept_holding_districts",
		"duchy_capital": "game_concept_duchy_districts", "great_district": "game_concept_great_districts",
		"trade_post": "game_concept_trade_post_districts", "entrepot": "game_concept_entrepot_districts",
	}},

	// A technology's tree. 00_early_medieval_innovations.txt:52 writes group
	// = culture_group_military, and 00_high_medieval_innovations.txt:218 flag
	// = global_regular on an innovation every culture can have; the game
	// lists the rest as Cultural and Regional. Vic3's
	// technology/technologies/10_production.txt:7 writes category = production.
	{
		Game: game.CK3, Database: "culture/innovations", Key: "group",
		MarkKey: "flag", MarkValue: "global_regular", Unmarked: "culture_group_regional",
	},
	{Game: game.Vic3, Database: "technology/technologies", Key: "category"},
}

// writtenGroupPrefix starts the id of a group written as a plain value, and
// writtenGroupSeparator parts the database its members are in from the
// value, as group:goods=staple.
const (
	writtenGroupPrefix    = "group:"
	writtenGroupSeparator = "="
)

// listGroupChain returns the group a definition is listed under and each
// wider group above it, nearest first. A group that leads back ends the chain.
func (d *draft) listGroupChain(id string) []string {
	var chain []string
	for group := d.findGroupID(id); group != "" && !slices.Contains(chain, group); group = d.findGroupID(group) {
		chain = append(chain, group)
	}

	return chain
}

// findGroupID returns the id of the group a definition is listed under: the
// value its database writes, else the first group of its type the links give
// it. A technology of a game with no trees is listed under its era, as EU5's
// ages are its trees. A later level in no group is listed with its line's
// first: common/districts/resource_districts.txt:13 names only
// forestry_resource_building_1. It is "" for none, and worked out once.
func (d *draft) findGroupID(id string) string {
	group, isKnown := d.groups[id]
	if !isKnown {
		group = d.locateGroup(id)
		if head := d.model.listLine(id)[0]; group == "" && head != id {
			group = d.findGroupID(head)
		}

		d.groups[id] = group
	}

	return group
}

func (d *draft) locateGroup(id string) string {
	row, isWritten := d.findWrittenGroup(id)
	groupType, isLinked := groupTypes[d.typeOf(id)]
	if !isWritten && d.typeOf(id) == lang.TypeTechnology {
		return d.findEra(id)
	}

	if !isWritten && !isLinked {
		return ""
	}

	if !isWritten {
		edge, _ := d.findGroup(id, groupType)

		return edge.To
	}

	tree, statement, isRead := d.findStatement(id)
	if !isRead {
		return ""
	}

	value, hasValue := tree.FindChildValue(statement, row.Key)
	if !hasValue {
		value = row.Default
	}

	if row.MarkKey != "" && !slices.Contains(listValues(tree, statement, row.MarkKey), row.MarkValue) {
		value = row.Unmarked
	}

	if value == "" {
		return ""
	}

	return writtenGroupPrefix + row.Database + writtenGroupSeparator + value
}

// findWrittenGroup returns the row of a definition's database, and false if
// the game writes no group there as a plain value.
func (d *draft) findWrittenGroup(id string) (writtenGroup, bool) {
	database := toDatabase(id)
	position := slices.IndexFunc(writtenGroups, func(row writtenGroup) bool {
		return row.Game == d.ws.Game.ID && row.Database == database
	})
	if position < 0 {
		return writtenGroup{}, false
	}

	return writtenGroups[position], true
}

// findGroup returns the link from a definition to the first group of a type
// it is in, the marked one where the game marks one, and false for none.
func (d *draft) findGroup(id string, groupType lang.Type) (Edge, bool) {
	for _, edge := range d.model.listFrom(id, saysGroup) {
		if d.typeOf(edge.To) == groupType && d.isMarked(edge.To) {
			return edge, true
		}
	}

	return Edge{}, false
}

// isMarked reports whether a group is one its members are listed under: it
// writes its game's mark, or its database has none.
func (d *draft) isMarked(id string) bool {
	mark, hasMark := groupMarks[d.ws.Game.ID]
	if !hasMark || toDatabase(id) != mark.Database {
		return true
	}

	tree, statement, isRead := d.findStatement(id)
	if !isRead {
		return false
	}

	value, _ := tree.FindChildValue(statement, mark.Key)

	return value == mark.Value
}

// rankGroup returns where a catalog lists a group among those beside it: an
// era by the game's order, an audience's heading by its audience's, and -1
// for any other, which stay in the order the files reach them.
func (d *draft) rankGroup(id string) int {
	if era := slices.Index(d.listEras(), id); era >= 0 {
		return era
	}

	rest, _ := strings.CutPrefix(id, audiencePrefix)
	audience, _, _ := strings.Cut(rest, writtenGroupSeparator)

	return slices.IndexFunc(listAudienceGroups(d.ws.Game.ID), func(group audienceGroup) bool { return group.ID == audience })
}

// makeGroup returns the node of a group, with the group it is in and how
// many definitions it lists. A group written as a plain value takes the
// game's name for it, and an audience's heading inside an age the
// audience's. It returns false for an id that is none of them.
func (d *draft) makeGroup(id string, count int) (Node, bool) {
	if rest, isAudience := strings.CutPrefix(id, audiencePrefix); isAudience {
		audience, age, _ := strings.Cut(rest, writtenGroupSeparator)
		position := d.rankGroup(id)
		title := listAudienceGroups(d.ws.Game.ID)[max(position, 0)].Title

		return Node{ID: id, Name: audience, Title: title, Group: age, Count: count}, position >= 0
	}

	node, isDefined := d.build(id)
	rest, isWritten := strings.CutPrefix(id, writtenGroupPrefix)
	if !isDefined && !isWritten {
		return Node{}, false
	}

	if isWritten {
		database, value, _ := strings.Cut(rest, writtenGroupSeparator)
		row, _ := d.findWrittenGroup(lang.Reference{Database: database}.ToID())
		key := row.Titles[value]
		if row.Titles == nil {
			key = value
		}

		node = Node{ID: id, Name: value, What: row.Key}
		node.Title, _ = describeKey(d.ws, d.mods, key)
	}

	node.Count = count

	return node, true
}
