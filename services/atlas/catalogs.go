// catalogs.go lists what the page offers: the catalogs of each game, a
// catalog's rows under their groups, what the mods define, and what an agent
// finds by name. It also finds the group a definition is listed under.

package atlas

import (
	"cmp"
	"maps"
	"slices"
	"strings"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/services/workspace"
)

// Catalog is the definitions of one type that the page lists together.
type Catalog struct {
	// Types are the types of its definitions.
	Types []lang.Type `json:"types"`

	// Label is what the game calls its definitions.
	Label string `json:"label"`

	// Count is how many it lists. A building's line counts once.
	Count int `json:"count"`
}

// CatalogSection is a group of a catalog with the rows listed under it and
// the sections inside it. The rows in no group are the last section, whose
// group has no id.
type CatalogSection struct {
	// Group is the group, with how many rows it and its sections list as
	// its Count.
	Group    Node             `json:"group"`
	Rows     []Node           `json:"rows"`
	Sections []CatalogSection `json:"sections"`

	// Shown and Found are how many rows a search or a limit left of how
	// many, set on the first section of an answer.
	Shown int `json:"shown"`
	Found int `json:"found"`
}

// catalogTypes are the types of the catalogs the page offers, in the order
// it offers them. A Vic3 production method has no catalog: it is reached
// through its building.
var catalogTypes = []lang.Type{
	lang.TypeGood, lang.TypeGoodCategory, lang.TypeBuilding, lang.TypeTechnology, lang.TypeEvent,
}

// groupTypes holds the type of group each type of definition is in: a
// building in a district, a building group or a category, a Vic3 production
// method in its group, a Vic3 building group in its parent, a CK3 good in a
// category.
var groupTypes = map[lang.Type]lang.Type{
	lang.TypeBuilding:         lang.TypeBuildingGroup,
	lang.TypeProductionMethod: lang.TypeProductionMethodGroup,
	lang.TypeBuildingGroup:    lang.TypeBuildingGroup,
	lang.TypeGood:             lang.TypeGoodCategory,
}

// listedMark is the statement that marks, among the CK3 categories a trade
// good is in, the one it is listed under, and the only categories a catalog
// lists: _trade_good_categories.info:14-17 says a trade good is in one
// category that writes need = yes, and may be in several that do not.
var listedMark = struct{ Database, Key string }{Database: "trade/trade_good_categories", Key: "need"}

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
	// listed under its value. One that does not is listed under Unmarked.
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
	{Game: game.EU5, Database: "goods", Key: "category"},
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

// The parts of the id of a group written as a plain value, as
// group:goods=staple, and the start of the id of an audience's heading
// inside an age, as audience:countries=age:age_1_traditions.
const (
	writtenGroupPrefix = "group:"
	audiencePrefix     = "audience:"
	groupSeparator     = "="
)

// listGroups returns the group a definition is listed under and each wider
// group above it, nearest first. A group that leads back ends the list.
func listGroups(ws *workspace.Session, mods *index.Index, definition index.Site) []Node {
	var groups []Node
	for seen := map[index.Site]bool{}; !seen[definition]; {
		seen[definition] = true
		group, site, ok := findGroup(ws, mods, definition)
		if !ok {
			break
		}

		groups = append(groups, group)
		if site == noSite {
			break
		}

		definition = site
	}

	return groups
}

// findGroup returns the group a definition is listed under, with the group's
// own definition, or noSite for a group written as a plain value. It is the
// value its database writes as its group, else the first group of its type's
// group type that its links give it, a CK3 good's the category that needs
// can ask for. A technology in no such group is listed under its era, and a
// later level with none under its line's first level's. It returns false for
// a definition in no group.
func findGroup(ws *workspace.Session, mods *index.Index, definition index.Site) (Node, index.Site, bool) {
	reference := mods.ToReference(definition)
	if group, ok := findWrittenGroup(ws, mods, definition); ok {
		return group, noSite, true
	}

	nodeType := ws.Schema.LookupType(reference.Database)
	var found []index.Site
	for _, link := range mods.ListLinks(ws.Schema, definition, index.RelationPart, index.RelationEra) {
		target := ws.Schema.LookupType(mods.ToReference(link.Target).Database)
		isGroup := link.Relation == index.RelationPart && target == groupTypes[nodeType] && target != ""
		isEra := link.Relation == index.RelationEra && nodeType == lang.TypeTechnology
		if link.Source == definition && (isGroup || isEra) {
			found = append(found, link.Target)
		}
	}

	if len(found) > 0 {
		listed := max(slices.IndexFunc(found, func(group index.Site) bool { return isListed(ws, mods, group) }), 0)
		group := makeNode(ws, mods, found[listed])
		group.Painting = ws.FindPainting(mods, found[listed])

		return group, found[listed], true
	}

	if levels := listLevels(ws, mods, definition); levels[0] != definition {
		return findGroup(ws, mods, levels[0])
	}

	return Node{}, noSite, false
}

// isListed reports whether a catalog lists a definition by itself: a CK3
// category of goods only where a need can ask for it, and any other
// definition always.
func isListed(ws *workspace.Session, mods *index.Index, definition index.Site) bool {
	if ws.Game.ID != game.CK3 || mods.ToReference(definition).Database != listedMark.Database {
		return true
	}

	tree, statement, err := ws.FindStatement(mods, definition)
	if err != nil {
		return false
	}

	value, _ := tree.FindChildValue(statement, listedMark.Key)

	return value == lang.Yes
}

// findWrittenGroup returns the group a definition's database writes as a
// plain value, named by the game's text for the value. It returns false for
// a database that writes none, and for a definition that writes no value
// where the game takes no default.
func findWrittenGroup(ws *workspace.Session, mods *index.Index, definition index.Site) (Node, bool) {
	reference := mods.ToReference(definition)
	position := slices.IndexFunc(writtenGroups, func(row writtenGroup) bool {
		return row.Game == ws.Game.ID && row.Database == reference.Database
	})
	tree, statement, err := ws.FindStatement(mods, definition)
	if position < 0 || err != nil {
		return Node{}, false
	}

	row := writtenGroups[position]
	value, _ := tree.FindChildValue(statement, row.Key)
	value = cmp.Or(value, row.Default)
	isMarked := row.MarkKey == ""
	for child := range tree.Children(statement) {
		written := tree.Statements[child]
		isMarked = isMarked || tree.Text(written.Key) == row.MarkKey && tree.Unquoted(written.Value) == row.MarkValue
	}

	if !isMarked {
		value = row.Unmarked
	}

	if value == "" {
		return Node{}, false
	}

	text, _, _ := ws.LookupText(mods, cmp.Or(row.Titles[value], value))
	group := Node{
		ID: writtenGroupPrefix + reference.Database + groupSeparator + value, Database: reference.Database, Name: value,
		Title: cmp.Or(ws.FlattenText(mods, text), toWords(value)), What: "group",
	}
	group.Painting = ws.FindPainting(mods, definition)

	return group, true
}

// ListCatalogs returns the catalogs the open game has, in the order the page
// offers them, each with what the game calls its definitions and how many it
// lists. It returns none if no workspace is open.
func (s *Service) ListCatalogs() []Catalog {
	ws, err := s.Workspace.Session()
	if err != nil {
		return []Catalog{}
	}

	mods := ws.Mods.Load()
	catalogs := []Catalog{}
	for _, nodeType := range catalogTypes {
		rows := listRows(ws, mods, []lang.Type{nodeType})
		if len(rows) == 0 {
			continue
		}

		words := lang.DescribeDefinition(mods.ToReference(rows[0]).Database, true)
		catalogs = append(catalogs, Catalog{
			Types: []lang.Type{nodeType}, Label: strings.ToUpper(words[:1]) + words[1:], Count: len(rows),
		})
	}

	return catalogs
}

// listRows returns the definitions the catalogs of the given types list, in
// the order the files reach them: a building's line once, by its first
// level, and a CK3 category only where a need can ask for it. A Vic3
// production method is listed for no catalog.
func listRows(ws *workspace.Session, mods *index.Index, types []lang.Type) []index.Site {
	drawn := append(slices.Clone(lang.LookupDialect(ws.Game.ID).AtlasTypes), lang.TypeEvent)

	var rows []index.Site
	for _, name := range slices.Sorted(maps.Keys(ws.Schema.Databases)) {
		nodeType := ws.Schema.LookupType(name)
		if !slices.Contains(types, nodeType) || !slices.Contains(drawn, nodeType) {
			continue
		}

		for _, winner := range mods.ListKept(ws.Schema, name) {
			isFirst := nodeType != lang.TypeBuilding || len(pickLinks(ws, mods, winner.Site, index.RelationLevel, false)) == 0
			if isFirst && isListed(ws, mods, winner.Site) {
				rows = append(rows, winner.Site)
			}
		}
	}

	return rows
}

// ListCatalog returns the rows of the catalogs of the given types under
// their groups, widest first, with the rows in no group last. With a query
// it lists the rows whose name, else title, matches, the best match first,
// and at most the limit; with none, the first rows up to the limit. A group
// that holds one row is that row alone. It returns none if no workspace is
// open.
func (s *Service) ListCatalog(types []lang.Type, query string, limit int) []CatalogSection {
	ws, err := s.Workspace.Session()
	if err != nil {
		return []CatalogSection{}
	}

	mods := ws.Mods.Load()
	rows := listRows(ws, mods, types)
	if query != "" {
		rows = rankRows(ws, mods, rows, query)
	}

	root := CatalogSection{Found: len(rows)}
	rows = rows[:min(len(rows), max(limit, 0))]
	root.Shown = len(rows)
	for _, definition := range rows {
		groups := listGroups(ws, mods, definition)
		row := makeNode(ws, mods, definition)
		if len(groups) > 0 {
			row.Group = groups[0].ID
		}

		if audience := describeAudienceGroup(ws, mods, definition, groups); audience.ID != "" {
			groups = append([]Node{audience}, groups...)
		}

		slices.Reverse(groups)
		placeRow(&root, groups, row)
	}

	sections := foldSections(&root)
	if len(root.Rows) > 0 || len(sections) == 0 {
		sections = append(sections, CatalogSection{Rows: root.Rows, Sections: []CatalogSection{}})
	}

	sections[0].Shown, sections[0].Found = root.Shown, root.Found

	return sections
}

// describeAudienceGroup returns the heading an EU5 advance that is not for
// every country stands under inside its age: the group of its audience. It
// returns the zero node for any other definition.
func describeAudienceGroup(ws *workspace.Session, mods *index.Index, definition index.Site, groups []Node) Node {
	audience, _ := findAudience(ws, mods, definition)
	if audience == "" || len(groups) == 0 {
		return Node{}
	}

	database, _, _ := strings.Cut(audience, ":")
	for _, group := range append(slices.Clone(audienceGroups), otherAudience) {
		if slices.Contains(group.Databases, database) || group.ID == audience {
			return Node{ID: audiencePrefix + group.ID + groupSeparator + groups[0].ID, Title: group.Title, What: "group"}
		}
	}

	return Node{}
}

// placeRow puts a row under the sections of its groups, widest first, making
// each section on its first row.
func placeRow(section *CatalogSection, groups []Node, row Node) {
	section.Group.Count++
	if len(groups) == 0 {
		section.Rows = append(section.Rows, row)

		return
	}

	position := slices.IndexFunc(section.Sections, func(other CatalogSection) bool { return other.Group.ID == groups[0].ID })
	if position < 0 {
		position = len(section.Sections)
		group := groups[0]
		group.Count = 0
		section.Sections = append(section.Sections, CatalogSection{Group: group, Rows: []Node{}, Sections: []CatalogSection{}})
	}

	placeRow(&section.Sections[position], groups[1:], row)
}

// foldSections returns a section's own sections with each group of one row
// taken out: that row joins the rows of the section above.
func foldSections(section *CatalogSection) []CatalogSection {
	kept := []CatalogSection{}
	for position := range section.Sections {
		inner := &section.Sections[position]
		inner.Sections = foldSections(inner)
		if len(inner.Rows) == 1 && len(inner.Sections) == 0 {
			section.Rows = append(section.Rows, inner.Rows[0])

			continue
		}

		kept = append(kept, *inner)
	}

	return kept
}

// rankRows returns the rows whose name matches a query, else the plain words
// of whose title do, the best match first, then by title, then by name.
func rankRows(ws *workspace.Session, mods *index.Index, rows []index.Site, query string) []index.Site {
	type match struct {
		site        index.Site
		rank        int
		title, name string
	}

	var matches []match
	for _, definition := range rows {
		name := mods.ToReference(definition).Name
		title := ws.LookupTitle(mods, definition)
		rank := files.ClassifyMatch(name, query)
		if rank < 0 {
			rank = files.ClassifyMatch(title, query)
		}

		if rank >= 0 {
			matches = append(matches, match{definition, rank, title, name})
		}
	}

	slices.SortStableFunc(matches, func(first, second match) int {
		return cmp.Or(cmp.Compare(first.rank, second.rank), strings.Compare(first.title, second.title),
			strings.Compare(first.name, second.name))
	})

	ranked := make([]index.Site, len(matches))
	for position, found := range matches {
		ranked[position] = found.site
	}

	return ranked
}

// ListOwnRoots returns what the workspace's mods define that the game runs:
// every event and every definition of a type Atlas draws that a mod's file
// writes a kept copy of. It returns none if no workspace is open.
func (s *Service) ListOwnRoots() []Node {
	ws, err := s.Workspace.Session()
	if err != nil {
		return []Node{}
	}

	mods := ws.Mods.Load()
	roots := []Node{}
	for _, definition := range listRows(ws, mods, catalogTypes) {
		winner, _ := mods.FindKept(ws.Schema, mods.ToReference(definition))
		origins := listOrigins(mods, winner, definition)
		if len(origins) > 1 || origins[0] != files.OriginVanilla {
			roots = append(roots, makeNode(ws, mods, definition))
		}
	}

	return roots
}

// FindRoots returns the definitions Atlas draws whose name or title holds a
// query, the best match first, up to a limit: those of every database that
// fires, the scripted effects, and every type Atlas draws. It returns none
// if no workspace is open.
func (s *Service) FindRoots(query string, limit int) []Node {
	ws, err := s.Workspace.Session()
	if err != nil {
		return []Node{}
	}

	mods := ws.Mods.Load()
	rows := listRows(ws, mods, append(slices.Clone(catalogTypes), lang.TypeProductionMethod))
	for _, database := range slices.Concat(ws.Schema.FireDatabases, []string{ws.Schema.Effects}) {
		if ws.Schema.LookupType(database) == lang.TypeEvent {
			continue
		}

		for _, winner := range mods.ListKept(ws.Schema, database) {
			rows = append(rows, winner.Site)
		}
	}

	roots := []Node{}
	for _, definition := range rankRows(ws, mods, rows, query) {
		if len(roots) < limit {
			roots = append(roots, makeNode(ws, mods, definition))
		}
	}

	return roots
}
