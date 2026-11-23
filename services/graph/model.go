// model.go reads the links between a game's goods, buildings and technologies.

package graph

import (
	"slices"
	"strconv"
	"strings"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/services/lsp"
	"paradox-modding-tools/services/workspace"
)

// model is every link between the definitions of one index, with the plain
// statements of their modifier blocks and costs.
type model struct {
	edges []Edge

	// outgoing and incoming hold the positions in edges of the links that
	// start and end at each id.
	outgoing, incoming map[string][]int

	// clauses holds each definition's clauses by its id, in the order
	// written.
	clauses map[string][]clause
}

// clause is one plain statement a definition writes in a modifier block,
// among its keys where its database writes modifiers there, as a cost, or as
// a line in the game's own words: a modifier, a condition of its block, what
// it costs, or that line.
type clause struct {
	Key string

	// Amount is the number the value stands for, else the value as written,
	// as yes or mountains.
	Amount string

	// Where is what its modifier block applies to, "" where the block says
	// nothing of it and at the top of the definition.
	Where string

	// Need is the id of the good or category of the need it is inside, ""
	// for none.
	Need string

	// Blocks are the blocks around it, innermost first, up to its modifier
	// block. They are none at the top of the definition.
	Blocks []int32
}

// What a link says of its two ends. It runs from the end that does it: a
// technology unlocks, a building takes.
const (
	// saysLevel is a level that leads to the next.
	saysLevel = "level"

	// saysGroup is a part of another: a building in its group, a production
	// method in its group, a group in its building.
	saysGroup = "group"

	// saysInput uses up a good, and saysOutput produces it.
	saysInput  = "input"
	saysOutput = "output"

	// saysBoost raises what is produced of a good by a share, as CK3's
	// trade_good_wood_production_mult.
	saysBoost = "boost"

	// saysGives is what a building produces or boosts once a need is met, as
	// in CK3's trade_good_need.
	saysGives = "gives"

	// saysEmployment is a production method that hires a pop type.
	saysEmployment = "employment"

	// saysUnlock is a technology that opens a definition, and saysRequires
	// one that must be researched first.
	saysUnlock   = "unlock"
	saysRequires = "requires"

	// saysEra is a technology in its era or age.
	saysEra = "era"

	// saysResource is a good in the places it is found.
	saysResource = "resource"

	// saysTrigger is a definition that tests a technology. No row reads it.
	saysTrigger = "trigger"
)

// writing is how a definition writes a link.
type writing uint8

const (
	// writtenReference is a reference the index holds. It is the zero value.
	writtenReference writing = iota

	// writtenKey is a key the index holds no reference for, as CK3's
	// trade_good_wood_production_add.
	writtenKey

	// writtenNesting is a definition inside its target's, as an EU5 unique
	// production method inside its building.
	writtenNesting

	// writtenSelf is a definition named after its target, as a CK3 county
	// potential is after its good.
	writtenSelf
)

// linkRow is one way a game writes a link between two definitions.
type linkRow struct {
	Games []string

	Says string

	// Database holds the definitions that write the link, and Target the
	// database it names: "" for the one the reference names.
	Database string
	Target   string

	Written writing

	// KeyPath is the keys from the definition down to the link's statement,
	// joined by slashes, with * for any text and ** for any depth. It is ""
	// for anywhere. A link written as a key gives the block that holds it.
	KeyPath string

	// Pattern is the key a link is written as, with %s for the target's
	// name, as Vic3's goods_input_%s_add.
	Pattern string

	// Backward makes the link run from the target, as EU5's obsolete names
	// the building a new one replaces.
	Backward bool

	// Amount is the key beside the link that holds its amount, as amount in
	// CK3's trade_good_need. It is "" where the link's own value does.
	Amount string

	// Share marks an amount that is a share of what is produced: 0.25 is a
	// quarter more.
	Share bool

	// Counted makes one link of all those between two ends, as the locations
	// that hold a raw material. Its end is then the database with no name,
	// and its amount how many it stands for.
	Counted bool
}

var (
	ck3  = []string{game.CK3}
	vic3 = []string{game.Vic3}
	eu5  = []string{game.EU5}
)

// linkRows are the links each game writes. The first row that fits a link
// reads it, so a narrower key path comes first. Each cites the file and line
// in the game's folder that writes it.
var linkRows = []linkRow{
	// common/buildings/01_industry.txt:174 and 186;
	// common/production_method_groups/01_industry.txt:96
	{Games: vic3, Says: saysGroup, Database: "buildings", KeyPath: "building_group"},
	{Games: vic3, Says: saysGroup, Database: "buildings", KeyPath: "production_method_groups", Backward: true},
	{Games: vic3, Says: saysGroup, Database: "production_method_groups", KeyPath: "production_methods", Backward: true},

	// common/building_groups/00_building_groups.txt:48
	{Games: vic3, Says: saysGroup, Database: "building_groups", KeyPath: "parent_group"},

	// common/technology/technologies/10_production.txt:5 and 48
	{Games: vic3, Says: saysEra, Database: "technology/technologies", KeyPath: "era"},
	{Games: vic3, Says: saysRequires, Database: "technology/technologies", KeyPath: "unlocking_technologies"},

	// A technology unlocks whatever names it in unlocking_technologies:
	// common/buildings/01_industry.txt:179
	unlockedBy("buildings"),
	unlockedBy("production_methods"),
	unlockedBy("laws"),
	unlockedBy("decrees"),
	unlockedBy("parties"),
	unlockedBy("diplomatic_actions"),
	unlockedBy("mobilization_options"),
	unlockedBy("combat_unit_types"),
	unlockedBy("ship_types"),
	unlockedBy("ship_modifications"),

	// A method's goods and jobs are modifiers named after them:
	// common/production_methods/01_industry.txt:979, 983 and 987
	{Games: vic3, Says: saysInput, Database: "production_methods", Written: writtenKey, Target: "goods", Pattern: "goods_input_%s_add"},
	{Games: vic3, Says: saysOutput, Database: "production_methods", Written: writtenKey, Target: "goods", Pattern: "goods_output_%s_add"},
	{Games: vic3, Says: saysEmployment, Database: "production_methods", Written: writtenKey, Target: "pop_types", Pattern: "building_employment_%s_add"},

	// common/pop_needs/00_pop_needs.txt:9
	{Games: vic3, Says: saysInput, Database: "pop_needs", KeyPath: "entry/goods"},

	// A state region names the resource buildings it holds:
	// map_data/state_regions/00_west_europe.txt:12, 14 and 324
	{Games: vic3, Says: saysResource, Database: "map_data/state_regions", KeyPath: "arable_resources", Backward: true, Counted: true},
	{Games: vic3, Says: saysResource, Database: "map_data/state_regions", KeyPath: "capped_resources/*", Backward: true, Counted: true},
	{Games: vic3, Says: saysResource, Database: "map_data/state_regions", KeyPath: "resource/type", Backward: true, Counted: true},

	// common/buildings/regular_district_buildings/
	// 00_guild_district_buildings.txt:111
	{Games: ck3, Says: saysLevel, Database: "buildings", KeyPath: "next_building"},

	// common/domiciles/buildings/00_camp_buildings.txt:117
	{Games: ck3, Says: saysLevel, Database: "domiciles/buildings", KeyPath: "previous_building", Backward: true},

	// common/districts/duchy_capital_districts.txt:5 and 35;
	// common/holdings/00_holdings.txt:6 and 8
	{Games: ck3, Says: saysGroup, Database: "districts", KeyPath: "primary_building", Backward: true},
	{Games: ck3, Says: saysGroup, Database: "districts", KeyPath: "buildings", Backward: true},
	{Games: ck3, Says: saysGroup, Database: "holdings", KeyPath: "primary_district", Backward: true},
	{Games: ck3, Says: saysGroup, Database: "holdings", KeyPath: "districts", Backward: true},

	// common/trade/trade_good_categories/00_trade_good_categories.txt:5
	{Games: ck3, Says: saysGroup, Database: "trade/trade_good_categories", Target: "trade/trade_good_types", KeyPath: "trade_goods", Backward: true},

	// common/buildings/regular_district_buildings/
	// 00_guild_district_buildings.txt:73-75
	{Games: ck3, Says: saysInput, Database: "buildings", KeyPath: "trade_good_need/trade_good", Amount: "amount"},
	{Games: ck3, Says: saysInput, Database: "buildings", KeyPath: "trade_good_need/trade_good_category", Amount: "amount"},

	// A building's production is a modifier named after the good: _add and
	// _flat a number, _mult a share
	// (resource_district_buildings/wood_district_buildings.txt:19). Inside a
	// need it is what meeting the need gives
	// (entrepot_district_buildings/00_entrepot_district_buildings.txt:256).
	{Games: ck3, Says: saysGives, Database: "buildings", Written: writtenKey, Target: "trade/trade_good_types", KeyPath: "trade_good_need/modifiers/**", Pattern: "%s_production_add"},
	{Games: ck3, Says: saysGives, Database: "buildings", Written: writtenKey, Target: "trade/trade_good_types", KeyPath: "trade_good_need/modifiers/**", Pattern: "%s_production_mult", Share: true},
	{Games: ck3, Says: saysGives, Database: "buildings", Written: writtenKey, Target: "trade/trade_good_types", KeyPath: "trade_good_need/modifiers/**", Pattern: "%s_production_flat"},
	{Games: ck3, Says: saysOutput, Database: "buildings", Written: writtenKey, Target: "trade/trade_good_types", Pattern: "%s_production_add"},
	{Games: ck3, Says: saysBoost, Database: "buildings", Written: writtenKey, Target: "trade/trade_good_types", Pattern: "%s_production_mult", Share: true},
	{Games: ck3, Says: saysOutput, Database: "buildings", Written: writtenKey, Target: "trade/trade_good_types", Pattern: "%s_production_flat"},

	// common/culture/innovations/00_tribal_innovations.txt:465 and
	// 00_cultural_maa_innovations.txt:39
	{Games: ck3, Says: saysUnlock, Database: "culture/innovations", KeyPath: "unlock_*"},
	{Games: ck3, Says: saysEra, Database: "culture/innovations", KeyPath: "culture_era"},

	// A building tests the innovation it needs in can_construct and
	// is_enabled: common/buildings/00_duchy_capital_buildings.txt:2178
	{Games: ck3, Says: saysUnlock, Database: "buildings", Target: "culture/innovations", KeyPath: "can_construct*/**", Backward: true},
	{Games: ck3, Says: saysUnlock, Database: "buildings", Target: "culture/innovations", KeyPath: "is_enabled/**", Backward: true},

	// common/trade/county_potential_trade_goods/
	// county_potential_trade_goods.txt:1
	{Games: ck3, Says: saysResource, Database: "trade/county_potential_trade_goods", Target: "trade/trade_good_types", Written: writtenSelf, Backward: true},

	// in_game/common/building_types/production_books.txt:72, 9 and 22; a
	// building's possible methods are at
	// 00_unique_buildings_to_make_obsolete.txt:66
	{Games: eu5, Says: saysLevel, Database: "building_types", KeyPath: "obsolete", Backward: true},
	{Games: eu5, Says: saysGroup, Database: "building_types", KeyPath: "category"},
	{Games: eu5, Says: saysGroup, Database: "building_types", KeyPath: "possible_production_methods", Backward: true},
	{Games: eu5, Says: saysGroup, Database: "building_types.unique_production_method", Target: "building_types", Written: writtenNesting},

	// A method writes the good it makes and its output, then each good it
	// takes as a key and its amount: building_types/production_books.txt:27-30
	{Games: eu5, Says: saysOutput, Database: "building_types.unique_production_method", Target: "goods", KeyPath: "produced", Amount: "output"},
	{Games: eu5, Says: saysOutput, Database: "production_methods", Target: "goods", KeyPath: "produced", Amount: "output"},
	{Games: eu5, Says: saysInput, Database: "building_types.unique_production_method", Target: "goods", KeyPath: "*"},
	{Games: eu5, Says: saysInput, Database: "production_methods", Target: "goods", KeyPath: "*"},

	// A building writes the pop type that works in it and how many:
	// in_game/common/building_types/production_books.txt:57 and 63
	{Games: eu5, Says: saysEmployment, Database: "building_types", KeyPath: "pop_type", Amount: "employment_size"},

	// in_game/common/goods_demand/pop_demands.txt:15
	{Games: eu5, Says: saysInput, Database: "goods_demand", Target: "goods", KeyPath: "*"},

	// in_game/common/advances/0_age_of_absolutism.txt:35 and
	// 0_age_of_renaissance.txt:5 and 19
	{Games: eu5, Says: saysUnlock, Database: "advances", KeyPath: "unlock_*"},
	{Games: eu5, Says: saysEra, Database: "advances", KeyPath: "age"},
	{Games: eu5, Says: saysRequires, Database: "advances", KeyPath: "requires"},

	// in_game/map_data/location_templates.txt:1
	{Games: eu5, Says: saysResource, Database: "map_data/location_templates", Target: "goods", KeyPath: "raw_material", Backward: true, Counted: true},
}

// unlockedBy returns the row of a Vic3 database whose definitions name the
// technologies that unlock them.
func unlockedBy(database string) linkRow {
	return linkRow{
		Games: vic3, Says: saysUnlock, Database: database, Target: "technology/technologies",
		KeyPath: "unlocking_technologies", Backward: true,
	}
}

// holdsReference reports whether the row reads a reference to a database,
// written under the given keys.
func (l linkRow) holdsReference(database string, keys []string) bool {
	return l.Written == writtenReference && (l.Target == "" || l.Target == database) && l.fits(keys)
}

// fits reports whether the keys down to a statement match the row's path.
func (l linkRow) fits(keys []string) bool {
	return l.KeyPath == "" || jomini.MatchKeyPath(l.KeyPath, keys)
}

// cutTarget returns the name a key spells in the row's pattern, and false
// for a key not written that way.
func (l linkRow) cutTarget(key string) (string, bool) {
	prefix, suffix, _ := strings.Cut(l.Pattern, "%s")
	name, isPrefixed := strings.CutPrefix(key, prefix)
	name, isSuffixed := strings.CutSuffix(name, suffix)

	return name, isPrefixed && isSuffixed && name != ""
}

// listFrom returns the links that start at a definition: those of the given
// relations, or all of them.
func (m *model) listFrom(id string, says ...string) []Edge {
	return m.pick(m.outgoing[id], says)
}

// listTo returns the links that end at a definition, as listFrom does.
func (m *model) listTo(id string, says ...string) []Edge {
	return m.pick(m.incoming[id], says)
}

func (m *model) pick(positions []int, says []string) []Edge {
	var edges []Edge
	for _, position := range positions {
		if edge := m.edges[position]; len(says) == 0 || slices.Contains(says, edge.Relation) {
			edges = append(edges, edge)
		}
	}

	return edges
}

// listLine returns the levels of a building's line, first to last. A building
// with no other level is a line of its own, and a level that leads to several
// is followed to the first.
func (m *model) listLine(id string) []string {
	line := []string{id}
	for {
		previous := m.listTo(line[0], saysLevel)
		if len(previous) == 0 || slices.Contains(line, previous[0].From) {
			break
		}

		line = slices.Insert(line, 0, previous[0].From)
	}

	for {
		next := m.listFrom(line[len(line)-1], saysLevel)
		if len(next) == 0 || slices.Contains(line, next[0].To) {
			break
		}

		line = append(line, next[0].To)
	}

	return line
}

// listMembers returns the given definitions and every part of them, as a
// building's production method groups and their methods.
func (m *model) listMembers(ids []string) []string {
	members := slices.Clone(ids)
	for position := 0; position < len(members); position++ {
		for _, edge := range m.listTo(members[position], saysGroup) {
			members = appendNew(members, edge.From)
		}
	}

	return members
}

// listTargets returns a good and its categories: a need for a category is a
// need for each good in it.
func (m *model) listTargets(good string) []string {
	targets := []string{good}
	for _, edge := range m.listFrom(good, saysGroup) {
		targets = append(targets, edge.To)
	}

	return targets
}

// models holds the model of the latest index.
var models latest[struct{}, *model]

// loadModel returns the model of an index, built the first time.
func loadModel(ws *workspace.Session, mods *index.Index) *model {
	return models.load(mods, struct{}{}, func() *model { return buildModel(ws, mods) })
}

// relator collects the links and clauses of one definition after another
// while a model is built.
type relator struct {
	ws      *workspace.Session
	mods    *index.Index
	edges   []Edge
	clauses map[string][]clause

	// definition is the one being read, with its name, its tree, its
	// statement and where its file is.
	definition index.Site
	reference  lang.Reference
	tree       *jomini.Tree
	statement  int32
	file       workspace.Place

	// block and need are the block of the statement being read, 0 at the top
	// of the definition, and the id of the good or category of the need it
	// is inside, "" for none.
	block int32
	need  string

	// counted holds the position in edges of each counted link by its ends,
	// and counts how many links each stands for.
	counted map[[2]string]int
	counts  map[int]int

	// places and values keep each file's place and each script value's
	// number once worked out.
	places map[int32]workspace.Place
	values map[string]string
}

// buildModel collects the links and clauses of every definition the game
// keeps in the databases its rows name. A definition whose file changed
// since the scan is passed over.
func buildModel(ws *workspace.Session, mods *index.Index) *model {
	links := &relator{
		ws: ws, mods: mods, clauses: map[string][]clause{}, counted: map[[2]string]int{}, counts: map[int]int{},
		places: map[int32]workspace.Place{}, values: map[string]string{},
	}

	dialect := lang.LookupDialect(ws.Game.ID)
	rows := slices.DeleteFunc(slices.Clone(linkRows), func(row linkRow) bool { return !dialect.Matches(row.Games) })
	for _, database := range listDatabases(ws.Game.ID, rows) {
		own := slices.DeleteFunc(slices.Clone(rows), func(row linkRow) bool { return row.Database != database })
		detail := findDetailRow(ws.Game.ID, database)
		for _, winner := range mods.ListKept(ws.Schema, database) {
			links.scanDefinition(winner.Site, own, detail)
		}
	}

	return links.finish()
}

// listDatabases returns the databases the rows name, in the rows' order, and
// then those whose definitions write modifiers or costs in the given game.
func listDatabases(game string, rows []linkRow) []string {
	var databases []string
	for _, row := range rows {
		databases = appendNew(databases, row.Database)
	}

	for _, row := range detailRows {
		if row.Game == game {
			databases = appendNew(databases, row.Database)
		}
	}

	return databases
}

// scanDefinition reads one definition statement by statement: each is a
// link where the first row that fits reads it, else a clause where its place
// holds modifiers or costs.
func (r *relator) scanDefinition(definition index.Site, rows []linkRow, detail detailRow) {
	tree, statement, err := r.ws.FindStatement(r.mods, definition)
	if err != nil {
		return
	}

	r.definition, r.tree, r.statement = definition, tree, statement
	r.reference = r.mods.ToReference(definition)
	r.file = r.locateFile()
	r.block, r.need = 0, ""

	for _, row := range rows {
		switch row.Written {
		case writtenNesting:
			r.scanNesting(row)

		case writtenSelf:
			r.scanSelf(row)
		}
	}

	references := map[int32][]index.Site{}
	for _, site := range r.mods.ListReferences(definition) {
		child := r.mods.Occurrence(site).Statement
		references[child] = append(references[child], site)
	}

	for child := statement + 1; child < tree.Statements[statement].Last; child++ {
		r.block = tree.Statements[child].Parent
		if r.block == statement {
			r.block, r.need = 0, ""
		}

		if r.isNeed(child) {
			r.need = r.readNeed(rows, references, child)
		}

		if !r.scanReferences(rows, references[child]) && !r.scanKey(rows, child) {
			r.keep(detail, child)
		}
	}
}

func (r *relator) locateFile() workspace.Place {
	if _, isLocated := r.places[r.definition.File]; !isLocated {
		r.places[r.definition.File] = r.ws.Locate(r.mods, r.mods.File(r.definition).Path)
	}

	return r.places[r.definition.File]
}

// scanNesting adds the link to the definition this one is written inside,
// where that is of the row's target.
func (r *relator) scanNesting(row linkRow) {
	holder, isNested := r.mods.FindDefinition(r.definition)
	if !isNested || r.mods.ToReference(holder).Database != row.Target {
		return
	}

	r.add(row, r.mods.ToReference(holder).ToID(), r.mods.Occurrence(r.definition).Span, "")
}

// scanSelf adds the link to the target's definition of this one's name.
func (r *relator) scanSelf(row linkRow) {
	target, isDefined := r.resolve(lang.Reference{Database: row.Target, Name: r.reference.Name})
	if isDefined {
		r.add(row, target, r.mods.Occurrence(r.definition).Span, "")
	}
}

// scanReferences adds the link of each of a statement's references that a
// row reads. It reports whether it added any.
func (r *relator) scanReferences(rows []linkRow, references []index.Site) bool {
	isLink := false
	for _, site := range references {
		row, target, isRead := r.readReference(rows, site)
		if !isRead {
			continue
		}

		occurrence := r.mods.Occurrence(site)
		isKey := occurrence.Span.Start == r.tree.Statements[occurrence.Statement].Key.Start
		r.add(row, target, occurrence.Span, r.findAmount(row, occurrence.Statement, isKey))
		isLink = true
	}

	return isLink
}

// readReference returns the first row that reads a reference as a link, and
// the id of its target. It returns false where no row reads it or the game
// keeps no definition of its name.
func (r *relator) readReference(rows []linkRow, site index.Site) (linkRow, string, bool) {
	reference := r.mods.ToReference(site)
	keys := r.listKeys(r.mods.Occurrence(site).Statement)
	position := slices.IndexFunc(rows, func(row linkRow) bool { return row.holdsReference(reference.Database, keys) })
	if position < 0 {
		return linkRow{}, "", false
	}

	target, isDefined := r.resolve(reference)

	return rows[position], target, isDefined
}

// isNeed reports whether a statement is a need block at the top of the
// definition.
func (r *relator) isNeed(statement int32) bool {
	current := r.tree.Statements[statement]

	return current.Parent == r.statement && current.IsBlock() && r.tree.Text(current.Key) == needKeys[r.ws.Game.ID]
}

// readNeed returns the id of the good or category a need block asks for:
// the target of the input link one of its own statements writes. It is ""
// where none names one the game keeps.
func (r *relator) readNeed(rows []linkRow, references map[int32][]index.Site, block int32) string {
	for child := range r.tree.Children(block) {
		for _, site := range references[child] {
			if row, target, isRead := r.readReference(rows, site); isRead && row.Says == saysInput {
				return target
			}
		}
	}

	return ""
}

// scanKey adds the link of a statement's key, by the first row whose pattern
// spells a target in it. It reports whether it added one.
func (r *relator) scanKey(rows []linkRow, statement int32) bool {
	current := r.tree.Statements[statement]
	key := r.tree.Text(current.Key)
	for _, row := range rows {
		if row.Written != writtenKey || key == "" {
			continue
		}

		name, isSpelled := row.cutTarget(key)
		if !isSpelled || !row.fits(r.listKeys(current.Parent)) {
			continue
		}

		target, isDefined := r.resolve(lang.Reference{Database: row.Target, Name: name})
		if isDefined {
			r.add(row, target, current.Key, r.findAmount(row, statement, true))
		}

		return isDefined
	}

	return false
}

// keep records a plain statement as a clause where the definition's
// database writes modifiers or costs: in a modifier block at the top or
// inside a need, and at the top where its modifiers stand loose or the key
// is a cost or a custom line. A block inside a modifier block is part of it.
func (r *relator) keep(detail detailRow, statement int32) {
	current := r.tree.Statements[statement]
	key := r.tree.Text(current.Key)
	if current.IsBlock() || key == "" || (len(detail.Bonuses) == 0 && current.Parent != r.statement) {
		return
	}

	parents := r.listParents(statement)
	entry, where := -1, ""
	for position, parent := range parents {
		parentKey := r.tree.Text(r.tree.Statements[parent].Key)
		if bonus := slices.IndexFunc(detail.Bonuses, func(row modifierBlock) bool { return row.Key == parentKey }); bonus >= 0 {
			entry, where = position, detail.Bonuses[bonus].Where
		}
	}

	isTop := len(parents) == 0 && (detail.Loose || slices.Contains(detail.Cost, key) || slices.Contains(detail.Custom, key))
	isEntered := entry >= 0 && (entry == len(parents)-1 || r.isNeed(parents[len(parents)-1]))
	if !isTop && !isEntered {
		return
	}

	id := r.reference.ToID()
	r.clauses[id] = append(r.clauses[id], clause{
		Key: strings.Clone(key), Amount: strings.Clone(r.describeNumber(current.Value)),
		Where: where, Need: r.need, Blocks: parents[:entry+1],
	})
}

// listParents returns the blocks around a statement inside the definition,
// innermost first.
func (r *relator) listParents(statement int32) []int32 {
	var parents []int32
	for parent := r.tree.Statements[statement].Parent; parent != r.statement; parent = r.tree.Statements[parent].Parent {
		parents = append(parents, parent)
	}

	return parents
}

// listKeys returns the keys from the definition's block down to a statement,
// its own key last.
func (r *relator) listKeys(statement int32) []string {
	var keys []string
	for ; statement != r.statement && statement > 0; statement = r.tree.Statements[statement].Parent {
		if key := r.tree.Text(r.tree.Statements[statement].Key); key != "" {
			keys = append(keys, key)
		}
	}

	slices.Reverse(keys)

	return keys
}

// resolve returns the id of the definition the game keeps of a name, and
// false if it keeps none.
func (r *relator) resolve(reference lang.Reference) (string, bool) {
	winner, isKept := r.mods.FindKept(r.ws.Schema, reference)
	if !isKept {
		return "", false
	}

	return toID(r.mods, winner.Site), true
}

// findAmount returns the amount of the link a statement writes: the value of
// the row's amount key beside it, else its own where the link is its key. It
// is "" if there is none.
func (r *relator) findAmount(row linkRow, statement int32, isKey bool) string {
	switch {
	case row.Amount != "":
		if beside, isWritten := r.tree.FindChild(r.tree.Statements[statement].Parent, row.Amount); isWritten {
			return r.describeNumber(r.tree.Statements[beside].Value)
		}

	case isKey:
		return r.describeNumber(r.tree.Statements[statement].Value)
	}

	return ""
}

// describeNumber returns the number a value stands for. A script value gives
// the one number it is defined as, worked out once. Any other text is
// returned as written, and an empty span is "".
func (r *relator) describeNumber(span jomini.Span) string {
	name := r.tree.Unquoted(span)
	if number, isKnown := r.values[name]; isKnown || span.IsEmpty() {
		return number
	}

	if number, isNumber := r.tree.ResolveNumber(span); isNumber {
		return formatNumber(number)
	}

	number := findScriptValue(r.ws, r.mods, name)
	r.values[name] = number

	return number
}

// findScriptValue returns the number of a script value, through values that
// name another, as wood_tier_1_main_resource = tier_1_main_resource. It
// returns the name where the game keeps no such value, the value is more
// than one number, or values name each other.
func findScriptValue(ws *workspace.Session, mods *index.Index, name string) string {
	visited := map[string]bool{}
	for current := name; !visited[current]; {
		visited[current] = true

		winner, isKept := mods.FindKept(ws.Schema, lang.Reference{Database: ws.Schema.Values, Name: current})
		if !isKept {
			return name
		}

		tree, statement, err := ws.FindStatement(mods, winner.Site)
		if err != nil || tree.Statements[statement].IsBlock() {
			return name
		}

		value := tree.Statements[statement].Value
		if number, isNumber := tree.ResolveNumber(value); isNumber {
			return formatNumber(number)
		}

		current = tree.Unquoted(value)
	}

	return name
}

// formatNumber returns a number as the game writes it, as 0.25 and 3.
func formatNumber(number float64) string {
	return strconv.FormatFloat(number, 'f', -1, 64)
}

// add puts a link between the definition being read and a target. A counted
// link is put once, and each later one adds to its count.
func (r *relator) add(row linkRow, target string, span jomini.Span, amount string) {
	holder := r.reference.ToID()
	if row.Counted {
		holder = lang.Reference{Database: r.reference.Database}.ToID()
	}

	ends := [2]string{holder, target}
	if row.Backward {
		ends = [2]string{target, holder}
	}

	if row.Counted {
		position, isCounted := r.counted[ends]
		if isCounted {
			r.counts[position]++

			return
		}

		r.counted[ends] = len(r.edges)
		r.counts[len(r.edges)] = 1
	}

	site := lsp.LocateSpan(r.file, r.tree.Src, r.tree.Lines, span)
	r.edges = append(r.edges, Edge{
		From: ends[0], To: ends[1], Relation: row.Says, Amount: amount, Share: row.Share, Site: site,
		block: r.block, need: r.need,
	})
}

// finish returns the model of the links and clauses read, each counted
// link's amount its count.
func (r *relator) finish() *model {
	for position, count := range r.counts {
		r.edges[position].Amount = strconv.Itoa(count)
	}

	linked := &model{edges: r.edges, outgoing: map[string][]int{}, incoming: map[string][]int{}, clauses: r.clauses}
	for position, edge := range r.edges {
		linked.outgoing[edge.From] = append(linked.outgoing[edge.From], position)
		linked.incoming[edge.To] = append(linked.incoming[edge.To], position)
	}

	return linked
}
