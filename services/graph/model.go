// model.go reads how a game's goods, buildings and technologies link: which
// production method takes or makes which good, which technology unlocks which
// building, which level leads to the next.

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

// model holds every link between the definitions of one game's index: goods,
// buildings, production methods, technologies and the places a good is found. A
// question walks it from a definition to the links that start or end at it.
type model struct {
	edges []Edge

	// outgoing and incoming hold, for the id of a definition, the positions
	// in edges of the links that start and end at it.
	outgoing, incoming map[string][]int
}

// The ways a link says its two ends relate. A link runs from the end that
// does it, as the technology does the unlocking.
const (
	// saysLevel is a level that leads to the next, from the one built first.
	saysLevel = "level"

	// saysGroup is a definition that is a part of another: a building in its
	// group, a production method in its production method group, a group in
	// its building.
	saysGroup = "group"

	// saysInput is a definition that uses up a good, and saysOutput one that
	// produces it.
	saysInput  = "input"
	saysOutput = "output"

	// saysBoost is a building that raises what is produced of a good by a
	// share, as CK3's trade_good_wood_production_mult does.
	saysBoost = "boost"

	// saysGives is what a building produces or boosts when a need is met, as
	// CK3's trade_good_need writes it.
	saysGives = "gives"

	// saysEmployment is a production method that hires a pop type.
	saysEmployment = "employment"

	// saysUnlock is a technology that opens a definition, and saysRequires a
	// technology that must be researched first.
	saysUnlock   = "unlock"
	saysRequires = "requires"

	// saysEra is a technology in its era or age.
	saysEra = "era"

	// saysResource is a good in the places it is found.
	saysResource = "resource"

	// saysTrigger is a definition that tests whether a technology is
	// researched. No row reads it: a technology's detail lists it.
	saysTrigger = "trigger"
)

// writing is how a link is written in a definition.
type writing uint8

// The ways a link is written. The zero value is the usual one.
const (
	// writtenReference is a reference the index holds inside the definition.
	writtenReference writing = iota

	// writtenKey is a key the definition writes that the index holds no
	// reference for, as CK3's trade_good_wood_production_add.
	writtenKey

	// writtenNesting is a definition inside the target's, as an EU5 unique
	// production method inside its building.
	writtenNesting

	// writtenSelf is a definition whose own name names the target, as CK3's
	// county potentials are named after their good.
	writtenSelf
)

// linkRow is one way a game writes a link between two definitions: in which
// definitions, how, and what the link says.
type linkRow struct {
	Games []string

	Says string

	// Database holds the definitions the link is written in. Target holds
	// the database it names; "" takes the database the reference names.
	Database string
	Target   string

	Written writing

	// KeyPath is the way from the definition to the link's statement, keys
	// joined by slashes, as jomini.MatchKeyPath reads them. It is "" for
	// anywhere in the definition. For a link written as a key, it is the way
	// to the block that holds the key.
	KeyPath string

	// Pattern is the name the link is written as, %s standing for the
	// target's name, as Vic3's goods_input_%s_add. It is "" where the name
	// is the target's own.
	Pattern string

	// Backward is whether the link runs from the target to the definition it
	// is written in, as EU5's obsolete names the building a new one replaces.
	Backward bool

	// Amount is the key beside the link whose value is its amount, as amount
	// in CK3's trade_good_need. It is "" where the link's own value is its
	// amount, or it has none.
	Amount string

	// Share is whether the amount is a share of what is produced, as 0.25 for
	// a quarter more, and not a number of the good.
	Share bool

	// Counted is whether the links between two definitions are counted into one,
	// as the locations that hold a raw material. The definitions' end of the
	// link is then their database, written with no name, and its amount is
	// how many it stands for.
	Counted bool
}

var (
	ck3  = []string{game.CK3}
	vic3 = []string{game.Vic3}
	eu5  = []string{game.EU5}
)

// linkRows are the links each game writes. The first row that holds for a
// link reads it, so a row with a narrower key path comes before a wider one
// of the same database. A row cites the file and line of the game that
// writes what it reads, in the install's game folder.
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

	// What a technology unlocks is whatever writes it in unlocking_technologies:
	// common/buildings/01_industry.txt:179, then each folder that holds one
	// (production_methods, laws, decrees, parties, diplomatic_actions,
	// mobilization_options, combat_unit_types, ship_types, ship_modifications)
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

	// A building's production is a modifier named after the good, as
	// common/buildings/resource_district_buildings/wood_district_buildings.txt:19
	// writes it: _add and _flat a number of the good, _mult a share of what is
	// produced. What a met need gives is the same modifier inside the need
	// (entrepot_district_buildings/00_entrepot_district_buildings.txt:256),
	// so its rows come first.
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

// unlockedBy returns the row of a Vic3 database whose definitions write the
// technologies that unlock them in unlocking_technologies.
func unlockedBy(database string) linkRow {
	return linkRow{
		Games: vic3, Says: saysUnlock, Database: database, Target: "technology/technologies",
		KeyPath: "unlocking_technologies", Backward: true,
	}
}

// holdsReference reports whether the row reads a reference to a name in the
// given database, written at the given way down from the definition.
func (l linkRow) holdsReference(database string, keys []string) bool {
	return l.Written == writtenReference && (l.Target == "" || l.Target == database) && l.fits(keys)
}

// fits reports whether the given way down from the definition to a statement
// fits the row's KeyPath.
func (l linkRow) fits(keys []string) bool {
	return l.KeyPath == "" || jomini.MatchKeyPath(l.KeyPath, keys)
}

// cutTarget returns the name a key spells in the row's Pattern. It returns
// false if the key is not written that way.
func (l linkRow) cutTarget(key string) (string, bool) {
	prefix, suffix, _ := strings.Cut(l.Pattern, "%s")
	name, isPrefixed := strings.CutPrefix(key, prefix)
	name, isSuffixed := strings.CutSuffix(name, suffix)

	return name, isPrefixed && isSuffixed && name != ""
}

// listFrom returns the links of the given relations that start at a
// definition, or every link that does if none is given.
func (m *model) listFrom(id string, says ...string) []Edge {
	return m.pick(m.outgoing[id], says)
}

// listTo returns the links of the given relations that end at a definition,
// or every link that does if none is given.
func (m *model) listTo(id string, says ...string) []Edge {
	return m.pick(m.incoming[id], says)
}

// pick returns the links at the given positions of the given relations, in
// order.
func (m *model) pick(positions []int, says []string) []Edge {
	var edges []Edge
	for _, position := range positions {
		if edge := m.edges[position]; len(says) == 0 || slices.Contains(says, edge.Relation) {
			edges = append(edges, edge)
		}
	}

	return edges
}

// listLine returns the levels of the line a building is in, first to last. A
// building no level leads to or from is a line of its own. A level that leads
// to more than one is followed to the first.
func (m *model) listLine(id string) []string {
	line := []string{id}
	for {
		before := m.listTo(line[0], saysLevel)
		if len(before) == 0 || slices.Contains(line, before[0].From) {
			break
		}

		line = slices.Insert(line, 0, before[0].From)
	}

	for {
		after := m.listFrom(line[len(line)-1], saysLevel)
		if len(after) == 0 || slices.Contains(line, after[0].To) {
			break
		}

		line = append(line, after[0].To)
	}

	return line
}

// listMembers returns the given definitions and everything that is a part of
// them, as a building's production method groups and their production
// methods, in the order found.
func (m *model) listMembers(ids []string) []string {
	members := slices.Clone(ids)
	for position := 0; position < len(members); position++ {
		for _, edge := range m.listTo(members[position], saysGroup) {
			members = appendNew(members, edge.From)
		}
	}

	return members
}

// listTargets returns a good and the categories it is in: the ends a link to
// the good may name, as a need for a category is a need for each good in it.
func (m *model) listTargets(good string) []string {
	targets := []string{good}
	for _, edge := range m.listFrom(good, saysGroup) {
		targets = append(targets, edge.To)
	}

	return targets
}

// models holds the model of the latest index of the mods.
var models latest[struct{}, *model]

// loadModel returns the model of an index, reading it the first time it is
// asked for.
func loadModel(ws *workspace.Session, mods *index.Index) *model {
	return models.load(mods, struct{}{}, func() *model { return buildModel(ws, mods) })
}

// relator reads the links of the definitions one after another while a model
// is built: the definition it is at and the links it has collected so far.
type relator struct {
	ws    *workspace.Session
	mods  *index.Index
	edges []Edge

	// definition is the one being read: its site, name, tree and statement,
	// and where its file is.
	definition index.Site
	reference  lang.Reference
	tree       *jomini.Tree
	statement  int32
	file       workspace.Place

	// counted holds the position in edges of each counted link by its ends,
	// and counts how many links each stands for.
	counted map[[2]string]int
	counts  map[int]int

	// places and values hold what was worked out for a file and for a script
	// value, so each is worked out once.
	places map[int32]workspace.Place
	values map[string]string
}

// buildModel collects the links of every kept definition of the databases
// the game's link rows write them in. A definition whose file has changed
// since the scan is passed over.
func buildModel(ws *workspace.Session, mods *index.Index) *model {
	links := &relator{
		ws: ws, mods: mods, counted: map[[2]string]int{}, counts: map[int]int{},
		places: map[int32]workspace.Place{}, values: map[string]string{},
	}

	dialect := lang.LookupDialect(ws.Game.ID)
	rows := slices.DeleteFunc(slices.Clone(linkRows), func(row linkRow) bool { return !dialect.Matches(row.Games) })
	for _, database := range listDatabases(rows) {
		held := slices.DeleteFunc(slices.Clone(rows), func(row linkRow) bool { return row.Database != database })
		for _, winner := range mods.ListKept(ws.Schema, database) {
			links.scanDefinition(winner.Site, held)
		}
	}

	return links.finish()
}

// listDatabases returns the databases the rows are written in, in the order
// of their first row.
func listDatabases(rows []linkRow) []string {
	var databases []string
	for _, row := range rows {
		databases = appendNew(databases, row.Database)
	}

	return databases
}

// scanDefinition adds the links the rows find in one definition.
func (r *relator) scanDefinition(definition index.Site, rows []linkRow) {
	tree, statement, err := r.ws.FindStatement(r.mods, definition)
	if err != nil {
		return
	}

	r.definition, r.tree, r.statement = definition, tree, statement
	r.reference = r.mods.ToReference(definition)
	r.file = r.locateFile()

	for _, row := range rows {
		switch row.Written {
		case writtenNesting:
			r.scanNesting(row)

		case writtenSelf:
			r.scanSelf(row)
		}
	}

	r.scanReferences(rows)
	r.scanKeys(rows)
}

// locateFile returns where the definition's file is, working it out once for
// each file.
func (r *relator) locateFile() workspace.Place {
	if _, isLocated := r.places[r.definition.File]; !isLocated {
		r.places[r.definition.File] = r.ws.Locate(r.mods, r.mods.File(r.definition).Path)
	}

	return r.places[r.definition.File]
}

// scanNesting adds the link to the definition the current one is written
// inside, where that is of the row's target database.
func (r *relator) scanNesting(row linkRow) {
	holder, isNested := r.mods.FindDefinition(r.definition)
	if !isNested || r.mods.ToReference(holder).Database != row.Target {
		return
	}

	r.add(row, r.mods.ToReference(holder).ToID(), r.mods.Occurrence(r.definition).Span, "")
}

// scanSelf adds the link to the definition of the target database that has
// the current one's name.
func (r *relator) scanSelf(row linkRow) {
	target, isDefined := r.resolve(lang.Reference{Database: row.Target, Name: r.reference.Name})
	if isDefined {
		r.add(row, target, r.mods.Occurrence(r.definition).Span, "")
	}
}

// scanReferences adds the link of each reference in the definition that a
// row reads.
func (r *relator) scanReferences(rows []linkRow) {
	for _, site := range r.mods.ListReferences(r.definition) {
		reference, occurrence := r.mods.ToReference(site), r.mods.Occurrence(site)
		keys := r.listKeys(occurrence.Statement)
		position := slices.IndexFunc(rows, func(row linkRow) bool { return row.holdsReference(reference.Database, keys) })
		if position < 0 {
			continue
		}

		if target, isDefined := r.resolve(reference); isDefined {
			isKey := occurrence.Span.Start == r.tree.Statements[occurrence.Statement].Key.Start
			r.add(rows[position], target, occurrence.Span, r.findAmount(rows[position], occurrence.Statement, isKey))
		}
	}
}

// scanKeys adds the link of each key in the definition that a row names in
// its Pattern, for the target it spells.
func (r *relator) scanKeys(rows []linkRow) {
	rows = slices.DeleteFunc(slices.Clone(rows), func(row linkRow) bool { return row.Written != writtenKey })
	if len(rows) == 0 {
		return
	}

	for statement := r.statement + 1; statement < r.tree.Statements[r.statement].Last; statement++ {
		if !r.tree.Statements[statement].Key.IsEmpty() {
			r.scanKey(rows, statement)
		}
	}
}

// scanKey adds the link a statement's key makes, by the first row that
// reads it.
func (r *relator) scanKey(rows []linkRow, statement int32) {
	current := r.tree.Statements[statement]
	key := r.tree.Text(current.Key)
	keys := r.listKeys(current.Parent)
	for _, row := range rows {
		name, isSpelled := row.cutTarget(key)
		if !isSpelled || !row.fits(keys) {
			continue
		}

		if target, isDefined := r.resolve(lang.Reference{Database: row.Target, Name: name}); isDefined {
			r.add(row, target, current.Key, r.findAmount(row, statement, true))
		}

		return
	}
}

// listKeys returns the keys from the definition's own block down to a
// statement, its own key last. A value alone has none.
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

// resolve returns the id of the definition the game keeps of a name. It
// returns false if the game keeps none, so a link to it is dropped.
func (r *relator) resolve(reference lang.Reference) (string, bool) {
	winner, isKept := r.mods.FindKept(r.ws.Schema, reference)
	if !isKept {
		return "", false
	}

	return toID(r.mods, winner.Site), true
}

// findAmount returns the amount of the link a statement writes: the value
// of the row's Amount key beside it, else its own value where the link is
// its key. It returns "" if it has none.
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

// describeNumber returns the number a value span of the definition being read
// stands for, working a script value out once.
func (r *relator) describeNumber(span jomini.Span) string {
	return describeNumber(r.ws, r.mods, r.values, r.tree, span)
}

// describeNumber returns the number a value span of a file stands for. A
// script value written there gives its number where its definition is one
// number, kept in the given map once it is worked out. Any other text is
// returned as written, and an empty span is "".
func describeNumber(
	ws *workspace.Session, mods *index.Index, values map[string]string, tree *jomini.Tree, span jomini.Span,
) string {
	if span.IsEmpty() {
		return ""
	}

	if number, isNumber := tree.ResolveNumber(span); isNumber {
		return formatNumber(number)
	}

	name := tree.Unquoted(span)
	number, isKnown := values[name]
	if !isKnown {
		number = findScriptValue(ws, mods, name)
		values[name] = number
	}

	return number
}

// findScriptValue returns the number of the script value of a name, through
// any values that name another, as wood_tier_1_main_resource =
// tier_1_main_resource. It returns the name itself where the game keeps no
// such value, a definition is more than one number, or values name each other.
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

// formatNumber returns a number as the game writes it: no exponent, and no
// decimals it does not need, as 0.25 and 3.
func formatNumber(number float64) string {
	return strconv.FormatFloat(number, 'f', -1, 64)
}

// add puts a link from the definition being read to a target, or from the
// target to it where the row runs backward. A counted link is put once, and
// each later one adds to its count.
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
	})
}

// finish returns the model of the links found, with each counted link's
// count as its amount.
func (r *relator) finish() *model {
	for position, count := range r.counts {
		r.edges[position].Amount = strconv.Itoa(count)
	}

	linked := &model{edges: r.edges, outgoing: map[string][]int{}, incoming: map[string][]int{}}
	for position, edge := range r.edges {
		linked.outgoing[edge.From] = append(linked.outgoing[edge.From], position)
		linked.incoming[edge.To] = append(linked.incoming[edge.To], position)
	}

	return linked
}
