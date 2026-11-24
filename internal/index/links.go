// links.go lists which definitions are linked to which: a building that takes
// a good, a technology that unlocks a law.

package index

import (
	"slices"
	"strings"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/lang"
)

// Relation is what a link says of its two ends. A link runs from the end
// that does it: a technology unlocks, a building takes.
type Relation string

// The relations of a link.
const (
	// RelationLevel is a level of a building that leads to the next.
	RelationLevel Relation = "level"

	// RelationPart is a part of another: a building in its group, a
	// production method in its group.
	RelationPart Relation = "part"

	// RelationInput uses up a good, and RelationOutput produces it.
	RelationInput  Relation = "input"
	RelationOutput Relation = "output"

	// RelationBoost raises what is produced of a good by a share, as CK3's
	// trade_good_wood_production_mult does.
	RelationBoost Relation = "boost"

	// RelationGives is what a building produces or boosts once a need is
	// met, as inside CK3's trade_good_need.
	RelationGives Relation = "gives"

	// RelationEmployment hires a pop type.
	RelationEmployment Relation = "employment"

	// RelationUnlock opens a definition, and RelationRequires must be
	// researched first.
	RelationUnlock   Relation = "unlock"
	RelationRequires Relation = "requires"

	// RelationEra is a technology in its era or age.
	RelationEra Relation = "era"

	// RelationResource is a good in the places it is found.
	RelationResource Relation = "resource"

	// RelationAccess is a good the building of a Vic3 production method must
	// reach before the method can run.
	RelationAccess Relation = "access"
)

// Link is one link between two definitions, in the direction its relation
// reads.
type Link struct {
	Source Site `json:"source"`
	Target Site `json:"target"`

	Relation Relation `json:"relation"`

	// Use is where the link is written: the reference that names the other
	// end, or the definition nested in its target or named after it.
	Use Site `json:"use"`

	// AmountKey is the key beside the reference that holds the link's
	// amount. It is "" where the reference's own statement does.
	AmountKey string `json:"amountKey,omitempty"`

	// Share is set for an amount that is a share of what is produced: 0.25
	// is a quarter more.
	Share bool `json:"share,omitempty"`

	// Count is how many definitions of the target's database the link stands
	// for, 0 for a link to one.
	Count int `json:"count,omitempty"`
}

// linkRow is one way a game writes a link between two definitions.
type linkRow struct {
	Games []string

	Relation Relation

	// Database holds the definitions that write the link. Target is the
	// database it names, "" for the one the reference names.
	Database, Target string

	// KeyPath is the keys from the definition down to the reference, as
	// jomini.MatchKeyPath reads them. For a link written as a key it leads
	// to the block that holds the key.
	KeyPath string

	// Pattern is the key a link is written as, with %s for the target's
	// name, as Vic3's goods_input_%s_add. It is "" for any other link.
	Pattern string

	// Nested is set where the definition is written inside its target's, as
	// an EU5 unique production method is inside its building. Named is set
	// where it is named after its target, as a CK3 county potential is
	// after its good.
	Nested, Named bool

	// Backward is set where the link runs from the target, as EU5's obsolete
	// names the building a new one replaces.
	Backward bool

	// AmountKey and Share are the link's.
	AmountKey string
	Share     bool

	// Counted is set where the definitions of one database that name a
	// definition are one link with their number, as the locations that hold
	// a raw material are.
	Counted bool
}

var (
	ck3  = []string{game.CK3}
	vic3 = []string{game.Vic3}
	eu5  = []string{game.EU5}
)

// The databases several rows name.
const (
	ck3Buildings   = "buildings"
	ck3Goods       = "trade/trade_good_types"
	ck3Categories  = "trade/trade_good_categories"
	ck3Innovations = "culture/innovations"
	ck3NeedGives   = "trade_good_need/modifiers/**"
	vic3Methods    = "production_methods"
	vic3Regions    = "map_data/state_regions"
	eu5Buildings   = "building_types"
	eu5Methods     = "production_methods"
	eu5OwnMethods  = "building_types.unique_production_method"
	eu5Goods       = "goods"
	anyBlock       = "**"
)

// linkRows are the links each game writes. The first row that fits a
// reference reads it, so a narrower key path comes first. Each cites the
// file and line in the game's folder that writes it.
var linkRows = []linkRow{
	// common/buildings/01_industry.txt:174 and 186;
	// common/production_method_groups/01_industry.txt:96
	{Games: vic3, Relation: RelationPart, Database: "buildings", KeyPath: "building_group"},
	{Games: vic3, Relation: RelationPart, Database: "buildings", KeyPath: "production_method_groups", Backward: true},
	{Games: vic3, Relation: RelationPart, Database: "production_method_groups", KeyPath: "production_methods", Backward: true},

	// common/building_groups/00_building_groups.txt:48
	{Games: vic3, Relation: RelationPart, Database: "building_groups", KeyPath: "parent_group"},

	// common/technology/technologies/10_production.txt:5 and 48
	{Games: vic3, Relation: RelationEra, Database: "technology/technologies", KeyPath: "era"},
	{Games: vic3, Relation: RelationRequires, Database: "technology/technologies", KeyPath: "unlocking_technologies"},

	// A technology unlocks whatever names it in unlocking_technologies:
	// common/buildings/01_industry.txt:179
	unlockedBy("buildings", "unlocking_technologies"),
	unlockedBy(vic3Methods, "unlocking_technologies"),
	unlockedBy("laws", "unlocking_technologies"),
	unlockedBy("decrees", "unlocking_technologies"),
	unlockedBy("parties", "unlocking_technologies"),
	unlockedBy("diplomatic_actions", "unlocking_technologies"),
	unlockedBy("mobilization_options", "unlocking_technologies"),
	unlockedBy("combat_unit_types", "unlocking_technologies"),
	unlockedBy("ship_types", "unlocking_technologies"),
	unlockedBy("ship_modifications", "unlocking_technologies"),

	// A production method names what else unlocks it:
	// common/production_methods/05_military.txt:153, 01_industry.txt:161 and
	// 414, 08_monuments.txt:313 and 237, 02_agro.txt:21. No file defines the
	// company categories that unlocking_company_categories names
	// (14_companies.txt:5), so no row reads them.
	unlockedBy(vic3Methods, "unlocking_laws"),
	unlockedBy(vic3Methods, "unlocking_principles"),
	unlockedBy(vic3Methods, "unlocking_production_methods"),
	unlockedBy(vic3Methods, "unlocking_identity"),
	unlockedBy(vic3Methods, "unlocking_religions"),
	unlockedBy(vic3Methods, "unlocking_geographic_regions"),

	// "those goods need to be accessible to the building to be able to
	// activate this pm" (common/production_methods/production_methods.md:29);
	// 01_industry.txt:379
	{Games: vic3, Relation: RelationAccess, Database: vic3Methods, KeyPath: "required_input_goods"},

	// A method's goods and jobs are modifiers named after them:
	// common/production_methods/01_industry.txt:979, 983 and 987
	{Games: vic3, Relation: RelationInput, Database: vic3Methods, Target: "goods", KeyPath: anyBlock, Pattern: "goods_input_%s_add"},
	{Games: vic3, Relation: RelationOutput, Database: vic3Methods, Target: "goods", KeyPath: anyBlock, Pattern: "goods_output_%s_add"},
	{Games: vic3, Relation: RelationEmployment, Database: vic3Methods, Target: "pop_types", KeyPath: anyBlock, Pattern: "building_employment_%s_add"},

	// common/pop_needs/00_pop_needs.txt:9
	{Games: vic3, Relation: RelationInput, Database: "pop_needs", KeyPath: "entry/goods"},

	// A state region names the resource buildings it holds:
	// map_data/state_regions/00_west_europe.txt:12, 14 and 324
	{Games: vic3, Relation: RelationResource, Database: vic3Regions, KeyPath: "arable_resources", Backward: true, Counted: true},
	{Games: vic3, Relation: RelationResource, Database: vic3Regions, KeyPath: "capped_resources/*", Backward: true, Counted: true},
	{Games: vic3, Relation: RelationResource, Database: vic3Regions, KeyPath: "resource/type", Backward: true, Counted: true},

	// common/buildings/regular_district_buildings/
	// 00_guild_district_buildings.txt:111
	{Games: ck3, Relation: RelationLevel, Database: ck3Buildings, KeyPath: "next_building"},

	// common/domiciles/buildings/00_camp_buildings.txt:117
	{Games: ck3, Relation: RelationLevel, Database: "domiciles/buildings", KeyPath: "previous_building", Backward: true},

	// common/districts/duchy_capital_districts.txt:5 and 35;
	// common/holdings/00_holdings.txt:6 and 8
	{Games: ck3, Relation: RelationPart, Database: "districts", KeyPath: "primary_building", Backward: true},
	{Games: ck3, Relation: RelationPart, Database: "districts", KeyPath: "buildings", Backward: true},
	{Games: ck3, Relation: RelationPart, Database: "holdings", KeyPath: "primary_district", Backward: true},
	{Games: ck3, Relation: RelationPart, Database: "holdings", KeyPath: "districts", Backward: true},

	// common/trade/trade_good_categories/00_trade_good_categories.txt:5
	{Games: ck3, Relation: RelationPart, Database: ck3Categories, Target: ck3Goods, KeyPath: "trade_goods", Backward: true},

	// common/buildings/regular_district_buildings/
	// 00_guild_district_buildings.txt:73-75
	{Games: ck3, Relation: RelationInput, Database: ck3Buildings, KeyPath: "trade_good_need/trade_good", AmountKey: "amount"},
	{Games: ck3, Relation: RelationInput, Database: ck3Buildings, KeyPath: "trade_good_need/trade_good_category", AmountKey: "amount"},

	// A building's production is a modifier named after the good or the
	// category of goods: _add a number, _mult a share
	// (resource_district_buildings/wood_district_buildings.txt:19,
	// 01_duchy_office_buildings.txt:990). Inside a need it is what meeting
	// the need gives, and only there do the game's files write _flat
	// (entrepot_district_buildings/00_entrepot_district_buildings.txt:256).
	{Games: ck3, Relation: RelationGives, Database: ck3Buildings, Target: ck3Goods, KeyPath: ck3NeedGives, Pattern: "%s_production_add"},
	{Games: ck3, Relation: RelationGives, Database: ck3Buildings, Target: ck3Goods, KeyPath: ck3NeedGives, Pattern: "%s_production_mult", Share: true},
	{Games: ck3, Relation: RelationGives, Database: ck3Buildings, Target: ck3Goods, KeyPath: ck3NeedGives, Pattern: "%s_production_flat"},
	{Games: ck3, Relation: RelationGives, Database: ck3Buildings, Target: ck3Categories, KeyPath: ck3NeedGives, Pattern: "%s_production_mult", Share: true},
	{Games: ck3, Relation: RelationOutput, Database: ck3Buildings, Target: ck3Goods, KeyPath: anyBlock, Pattern: "%s_production_add"},
	{Games: ck3, Relation: RelationBoost, Database: ck3Buildings, Target: ck3Goods, KeyPath: anyBlock, Pattern: "%s_production_mult", Share: true},
	{Games: ck3, Relation: RelationBoost, Database: ck3Buildings, Target: ck3Categories, KeyPath: anyBlock, Pattern: "%s_production_mult", Share: true},

	// common/culture/innovations/00_tribal_innovations.txt:465 and
	// 00_cultural_maa_innovations.txt:39
	{Games: ck3, Relation: RelationUnlock, Database: ck3Innovations, KeyPath: "unlock_*"},
	{Games: ck3, Relation: RelationEra, Database: ck3Innovations, KeyPath: "culture_era"},

	// A building tests the innovation it needs in can_construct:
	// common/buildings/00_duchy_capital_buildings.txt:2178
	{Games: ck3, Relation: RelationUnlock, Database: ck3Buildings, Target: ck3Innovations, KeyPath: "can_construct*/**", Backward: true},

	// common/trade/county_potential_trade_goods/
	// county_potential_trade_goods.txt:1
	{Games: ck3, Relation: RelationResource, Database: "trade/county_potential_trade_goods", Target: ck3Goods, Named: true, Backward: true},

	// in_game/common/building_types/production_books.txt:72, 9 and 22; a
	// building's possible methods are at
	// 00_unique_buildings_to_make_obsolete.txt:66
	{Games: eu5, Relation: RelationLevel, Database: eu5Buildings, KeyPath: "obsolete", Backward: true},
	{Games: eu5, Relation: RelationPart, Database: eu5Buildings, KeyPath: "category"},
	{Games: eu5, Relation: RelationPart, Database: eu5Buildings, KeyPath: "possible_production_methods", Backward: true},
	{Games: eu5, Relation: RelationPart, Database: eu5OwnMethods, Target: eu5Buildings, Nested: true},

	// A method writes the good it makes and its output, then each good it
	// takes as a key and its amount: building_types/production_books.txt:27-30.
	// A unique method's keys are counted from its building.
	{Games: eu5, Relation: RelationOutput, Database: eu5OwnMethods, Target: eu5Goods, KeyPath: "unique_production_methods/*/produced", AmountKey: "output"},
	{Games: eu5, Relation: RelationOutput, Database: eu5Methods, Target: eu5Goods, KeyPath: "produced", AmountKey: "output"},
	{Games: eu5, Relation: RelationInput, Database: eu5OwnMethods, Target: eu5Goods, KeyPath: "unique_production_methods/*/*"},
	{Games: eu5, Relation: RelationInput, Database: eu5Methods, Target: eu5Goods, KeyPath: "*"},

	// A building writes the pop type that works in it and how many:
	// in_game/common/building_types/production_books.txt:57 and 63
	{Games: eu5, Relation: RelationEmployment, Database: eu5Buildings, KeyPath: "pop_type", AmountKey: "employment_size"},

	// in_game/common/goods_demand/pop_demands.txt:15
	{Games: eu5, Relation: RelationInput, Database: "goods_demand", Target: eu5Goods, KeyPath: "*"},

	// in_game/common/advances/0_age_of_absolutism.txt:35 and
	// 0_age_of_renaissance.txt:5 and 19
	{Games: eu5, Relation: RelationUnlock, Database: "advances", KeyPath: "unlock_*"},
	{Games: eu5, Relation: RelationEra, Database: "advances", KeyPath: "age"},
	{Games: eu5, Relation: RelationRequires, Database: "advances", KeyPath: "requires"},

	// in_game/map_data/location_templates.txt:1
	{Games: eu5, Relation: RelationResource, Database: "map_data/location_templates", Target: eu5Goods, KeyPath: "raw_material", Backward: true, Counted: true},
}

// unlockedBy returns the row of a Vic3 database whose definitions name, under
// the given key, what unlocks them.
func unlockedBy(database, keyPath string) linkRow {
	return linkRow{Games: vic3, Relation: RelationUnlock, Database: database, KeyPath: keyPath, Backward: true}
}

// ListLinks returns every link with a definition at either end: the links
// the definition writes, in the order written, the links that name it, then
// those read from where a definition is written or what it is named after.
// Given relations, it returns the links of those only. Only the copies the
// game keeps write links. A link whose other end the game keeps no definition
// of is left out.
func (i *Index) ListLinks(schema *lang.Schema, definition Site, relations ...Relation) []Link {
	copies := i.ListActing(schema, i.ToReference(definition), definition.File)

	var links []Link
	for _, written := range copies {
		for _, use := range i.listHeld(written, RoleRefers) {
			row, ok := i.findLinkRow(use)
			if !ok || !row.matches(relations) {
				continue
			}

			if winner, kept := i.FindKeptLocal(schema, i.resolveLinked(row, use), use.File); kept {
				links = append(links, row.link(definition, winner.Site, use))
			}
		}
	}

	naming, placed := i.listNaming(schema, definition, relations), i.listPlaced(schema, definition, copies, relations)

	return slices.Concat(links, naming, placed)
}

// listPlaced returns the links a definition has by where it is written or by
// its name: to the definition it is nested in or named after, and from the
// definitions nested in its copies or named after it. Given relations, it
// returns the links of those only.
func (i *Index) listPlaced(schema *lang.Schema, definition Site, copies []Site, relations []Relation) []Link {
	reference := i.ToReference(definition)
	parent, isNested := i.FindDefinition(definition)

	var links []Link
	for _, row := range linkRows {
		isWriter, isTarget := row.Database == reference.Database, row.Target == reference.Database
		if !i.dialect().Matches(row.Games) || !row.matches(relations) {
			continue
		}

		switch {
		case row.Named && isWriter:
			if winner, ok := i.FindKept(schema, lang.Reference{Database: row.Target, Name: reference.Name}); ok {
				links = append(links, row.link(definition, winner.Site, definition))
			}

		case row.Named && isTarget:
			if winner, ok := i.FindKept(schema, lang.Reference{Database: row.Database, Name: reference.Name}); ok {
				links = append(links, row.link(winner.Site, definition, winner.Site))
			}

		case row.Nested && isWriter && isNested && i.ToReference(parent).Database == row.Target:
			if winner, ok := i.FindKeptLocal(schema, i.ToReference(parent), parent.File); ok {
				links = append(links, row.link(definition, winner.Site, definition))
			}

		case row.Nested && isTarget:
			for _, written := range copies {
				for _, held := range i.listHeld(written, RoleDefines) {
					if i.ToReference(held).Database == row.Database {
						links = append(links, row.link(held, definition, held))
					}
				}
			}
		}
	}

	return links
}

// listNaming returns the links other definitions write to a definition. The
// definitions of one database that a counted row reads are one link, which
// names the first of them. Given relations, it returns the links of those
// only.
func (i *Index) listNaming(schema *lang.Schema, definition Site, relations []Relation) []Link {
	reference := i.ToReference(definition)
	uses := i.ListUses(reference.Database, reference.Name)
	for _, row := range linkRows {
		if row.Pattern != "" && row.Target == reference.Database && i.dialect().Matches(row.Games) {
			key := strings.Replace(row.Pattern, "%s", reference.Name, 1)
			uses = slices.Concat(uses, i.ListUses(schema.ModifierTypes, key))
		}
	}

	var links []Link
	counted, writers := map[string]int{}, map[Site]bool{}
	for _, use := range uses {
		row, ok := i.findLinkRow(use)
		if !ok || !row.matches(relations) {
			continue
		}

		parent, nested := i.FindDefinition(use)
		if !nested || i.resolveLinked(row, use).ToID() != reference.ToID() {
			continue
		}

		holder := i.ToReference(parent)
		winner, kept := i.FindKeptLocal(schema, holder, parent.File)
		if !kept || !i.isActing(winner, parent) || row.Counted && writers[winner.Site] {
			continue
		}

		link := row.link(winner.Site, definition, use)
		position, isCounted := counted[holder.Database]
		switch {
		case row.Counted && isCounted:
			links[position].Count++
		case row.Counted:
			link.Count, counted[holder.Database] = 1, len(links)
			links = append(links, link)
		default:
			links = append(links, link)
		}

		writers[winner.Site] = true
	}

	return links
}

// IsSpelled reports whether a reference is a name spelled inside a key, as
// trade_good_wood is in CK3's trade_good_wood_production_add. The walk makes
// no reference there, so a file read by the walk does not hold it.
func (i *Index) IsSpelled(use Site) bool {
	row, ok := i.findLinkRow(use)

	return ok && row.Pattern != "" && i.ToReference(use).Database == row.Target
}

// findLinkRow returns the row that reads a reference as a link. It returns
// false for a reference that is no link.
func (i *Index) findLinkRow(use Site) (linkRow, bool) {
	link := i.Occurrence(use).Link
	if link == 0 {
		return linkRow{}, false
	}

	return linkRows[link-1], true
}

// resolveLinked returns the definition a link's reference names. Where the
// reference is to the modifier a key is, as Vic3's goods_input_wood_add, it
// is the definition the key spells.
func (i *Index) resolveLinked(row linkRow, use Site) lang.Reference {
	reference := i.ToReference(use)
	if name, ok := row.cutName(reference.Name); ok && reference.Database != row.Target {
		return lang.Reference{Database: row.Target, Name: name}
	}

	return reference
}

// listHeld returns the occurrences with the given role that sit directly
// inside a definition, in source order. They follow it without a break, so
// the list ends at the first occurrence outside it.
func (i *Index) listHeld(definition Site, role Role) []Site {
	var sites []Site

	occurrences := i.Occurrences[definition.File]
	for position := definition.Occurrence + 1; int(position) < len(occurrences); position++ {
		occurrence := occurrences[position]
		if occurrence.Parent < definition.Occurrence {
			break
		}

		if occurrence.Role == role && occurrence.Parent == definition.Occurrence {
			sites = append(sites, Site{File: definition.File, Occurrence: position})
		}
	}

	return sites
}

// matches reports whether the row's relation is among the given ones. With
// none given, every row's is.
func (l linkRow) matches(relations []Relation) bool {
	return len(relations) == 0 || slices.Contains(relations, l.Relation)
}

// link returns the row's link between the definition that writes it and the
// one it names.
func (l linkRow) link(writer, named, use Site) Link {
	link := Link{Source: writer, Target: named, Relation: l.Relation, Use: use, AmountKey: l.AmountKey, Share: l.Share}
	if l.Backward {
		link.Source, link.Target = named, writer
	}

	return link
}

// cutName returns the name a key spells by the row's pattern, as wood in
// goods_input_wood_add. It returns false for a row with no pattern and for a
// key the pattern does not fit.
func (l linkRow) cutName(key string) (string, bool) {
	prefix, suffix, _ := strings.Cut(l.Pattern, "%s")
	fits := l.Pattern != "" && len(key) > len(prefix)+len(suffix) &&
		strings.HasPrefix(key, prefix) && strings.HasSuffix(key, suffix)
	if !fits {
		return "", false
	}

	return key[len(prefix) : len(key)-len(suffix)], true
}

// listLinkRows returns the positions in linkRows of the rows that read the
// references and keys of a database's definitions in the index's game. It
// keeps the answer for the database asked last, since the statements of a
// file mostly share one.
func (c *collector) listLinkRows(database string) []uint8 {
	if database == c.rowDatabase {
		return c.rows
	}

	c.rowDatabase, c.rows = database, nil
	for position, row := range linkRows {
		if row.Database == database && c.index.dialect().Matches(row.Games) && !row.Nested && !row.Named {
			c.rows = append(c.rows, uint8(position))
		}
	}

	return c.rows
}

// findLink returns the Link an occurrence of a reference keeps: one more than
// the position of the first row that reads it, or 0 for none. A row with a
// pattern reads the reference at a key the pattern fits, and any other row a
// reference to its target under its key path.
func (c *collector) findLink(context *lang.Context, reference lang.Reference) uint8 {
	rows := c.listLinkRows(context.Database)
	if len(rows) == 0 {
		return 0
	}

	keys := listKeys(context)
	for _, position := range rows {
		row := linkRows[position]
		_, spells := row.cutName(context.Label)
		isKey := spells && reference.Name == context.Label && jomini.MatchKeyPath(row.KeyPath, context.Path)
		isReference := row.Pattern == "" && (row.Target == "" || row.Target == reference.Database) &&
			jomini.MatchKeyPath(row.KeyPath, keys)
		if isKey || isReference {
			return position + 1
		}
	}

	return 0
}

// spellLinks returns a reference for each definition a statement's key
// spells where the walk made none, as CK3's trade_good_wood_production_add
// spells the good. Of the rows that name one database, the first that fits
// makes it.
func (c *collector) spellLinks(tree *jomini.Tree, statement, holder int32, context *lang.Context) []Occurrence {
	rows := c.listLinkRows(context.Database)
	isNamed := slices.ContainsFunc(context.References, func(reference lang.Reference) bool {
		return reference.Name == context.Label
	})
	if len(rows) == 0 || isNamed {
		return nil
	}

	var spelled []Occurrence
	var targets []string
	key := tree.Statements[statement].Key
	for _, position := range rows {
		row := linkRows[position]
		name, ok := row.cutName(context.Label)
		if !ok || slices.Contains(targets, row.Target) || !jomini.MatchKeyPath(row.KeyPath, context.Path) {
			continue
		}

		start := key.Start + strings.Index(row.Pattern, "%s")
		occurrence := c.index.makeOccurrence(RoleRefers, row.Target, name, jomini.Span{Start: start, End: start + len(name)})
		occurrence.Line = int32(tree.Lines.ToPosition(key.Start).Line)
		occurrence.Parent, occurrence.Statement, occurrence.Link = holder, statement, position+1
		spelled, targets = append(spelled, occurrence), append(targets, row.Target)
	}

	return spelled
}

// listKeys returns the keys from a statement's definition down to the
// statement itself. A value written alone ends at the key of its block.
func listKeys(context *lang.Context) []string {
	if context.Label == "" {
		return context.Path
	}

	return slices.Concat(context.Path, []string{context.Label})
}
