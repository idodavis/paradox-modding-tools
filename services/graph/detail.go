// detail.go builds what the page shows about one node of the Graph: what it
// is listed under and costs, its texts, its firings, its links and the game
// rules it reads.

package graph

import (
	"fmt"
	"math"
	"slices"
	"strconv"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/services/lsp"
	"paradox-modding-tools/services/workspace"
)

// Detail is what the page shows about one node.
type Detail struct {
	// Node is the node, with its trigger and every firing it makes as its
	// Fires.
	Node Node `json:"node"`

	// Language is the workspace's language, else the base language.
	Language string `json:"language"`

	// Source is the definition as written.
	Source string `json:"source"`

	// About are the facts listed under the node's name: each group above its
	// own, how many levels its line has, where it is found and what it
	// costs. An event has none.
	About []Fact `json:"about"`

	// Gives is what the node's modifiers give, as a technology's. An event
	// has none.
	Gives []Fact `json:"gives"`

	// Sections are the keys at the top of the definition but its option
	// blocks, which are Options. A good, a building and a technology keep
	// only their title and description.
	Sections []Section `json:"sections"`
	Options  []Section `json:"options"`

	FiredBy []Edge `json:"firedBy"`

	// Links are every link from and to the node among the game's goods,
	// buildings and technologies. A good's are joined by building line. A
	// technology also has a link from each definition that checks it, and an
	// event one to each good, building and technology its script names, the
	// relation the type of what it names.
	Links []Edge `json:"links"`

	// Linked holds the node at the other end of each firing and each link, by
	// id, and the node's own group and era.
	Linked map[string]Node `json:"linked"`

	// Rules are the game rules the definition reads.
	Rules []Rule `json:"rules"`

	// Copy is the copy of its name the definition hides, nil for none.
	Copy *Node `json:"copy,omitempty"`
}

// Rule is one game rule a definition reads, with its settings.
type Rule struct {
	// ID is the rule's name.
	ID      string       `json:"id"`
	Options []RuleOption `json:"options"`
}

// RuleOption is one setting of a game rule.
type RuleOption struct {
	Key string `json:"key"`

	// Text and Description are the setting's localized name and
	// description, "" where it has none.
	Text        string `json:"text"`
	Description string `json:"description"`

	Default bool `json:"default,omitempty"`
}

// detailRow says, for the definitions of one database in one game, the keys
// that hold what a definition gives and what it costs.
type detailRow struct {
	Game     string
	Database string

	// Bonuses are the blocks that hold what a definition gives. Cost are the
	// keys that hold what it costs, a block or a plain value.
	Bonuses []modifierBlock
	Cost    []string

	// Loose is set where a definition writes its modifiers among its other
	// keys, in no block.
	Loose bool
}

// ck3Modifiers are the blocks of a CK3 building that hold what it gives, one
// for each scope the game applies them in. Line 36 of common/buildings/
// regular_district_buildings/00_guild_district_buildings.txt writes one.
var ck3Modifiers = []modifierBlock{
	{Key: "province_modifier", Where: inProvince},
	{Key: "province_government_modifier", Where: inProvince},
	{Key: "province_culture_modifier", Where: inProvince},
	{Key: "province_terrain_modifier", Where: inProvince},
	{Key: "province_rite_modifier", Where: inProvince},
	{Key: "province_potential_trade_good_modifier", Where: inProvince},
	{Key: "county_modifier", Where: inCounty},
	{Key: "county_culture_modifier", Where: inCounty},
	{Key: "county_potential_trade_good_modifier", Where: inCounty},
	{Key: "duchy_capital_county_modifier", Where: "In the duchy's capital county"},
	{Key: "market_modifier", Where: inMarket},
	{Key: "market_culture_modifier", Where: inMarket},
	{Key: "market_rite_modifier", Where: inMarket},
	{Key: "character_modifier", Where: forHolder},
	{Key: "character_culture_modifier", Where: forHolder},
	{Key: "character_government_modifier", Where: forHolder},
	{Key: "county_holder_character_modifier", Where: "For the county's holder"},
}

// What the modifier blocks of a building apply to, in words.
const (
	inProvince = "In the province"
	inCounty   = "In the county"
	inMarket   = "In the market"
	forHolder  = "For the holder"
	inLocation = "In the location"
)

// modifierKey is the block of a Vic3 technology and an EU5 building that holds
// what it gives.
const modifierKey = "modifier"

// detailRows are the blocks each game's databases write what a definition
// gives and costs in.
var detailRows = []detailRow{
	// common/buildings/regular_district_buildings/
	// 00_guild_district_buildings.txt:41-47 writes cost_gold and the time;
	// common/buildings/special_buildings/00_legendary_buildings.txt:45 a cost
	// block; common/domiciles/buildings writes the same, the cost a block
	{
		Game: game.CK3, Database: "buildings", Bonuses: ck3Modifiers,
		Cost: []string{"cost_gold", "cost_prestige", "cost", "construction_time"},
	},
	{Game: game.CK3, Database: "domiciles/buildings", Bonuses: ck3Modifiers, Cost: []string{"cost", "construction_time"}},

	// common/culture/innovations/00_tribal_innovations.txt writes a modifier
	// block for each of these scopes, and the goods write their base price
	{
		Game: game.CK3, Database: "culture/innovations",
		Bonuses: []modifierBlock{
			{Key: "character_modifier", Where: "For the culture's characters"},
			{Key: "county_modifier", Where: "In the culture's counties"},
			{Key: "province_modifier", Where: "In the culture's provinces"},
			{Key: "culture_modifier", Where: "For the culture"},
		},
	},
	{Game: game.CK3, Database: "trade/trade_good_types", Cost: []string{"base_price"}},

	// common/buildings/01_industry.txt:197 writes required_construction;
	// common/production_methods/01_industry.txt:1922 writes the blocks of what
	// a method gives; common/technology/technologies/10_production.txt:11 a
	// technology's modifier; common/goods/00_goods.txt:11 a good's cost
	{Game: game.Vic3, Database: "buildings", Cost: []string{"required_construction"}},
	{
		Game: game.Vic3, Database: "production_methods",
		Bonuses: []modifierBlock{
			{Key: "country_modifiers", Where: "For the country"},
			{Key: "state_modifiers", Where: "In the state"},
		},
	},
	{Game: game.Vic3, Database: "technology/technologies", Bonuses: []modifierBlock{{Key: modifierKey}}},
	{Game: game.Vic3, Database: "goods", Cost: []string{"cost"}},

	// in_game/common/building_types/production_books.txt:20 writes build_time;
	// in_game/common/goods/02_produced_goods.txt:5 a good's price; lines 30 to
	// 36 of in_game/common/building_types/readme.txt say what each modifier
	// block applies to
	{
		Game: game.EU5, Database: "building_types",
		Bonuses: []modifierBlock{
			{Key: modifierKey, Where: inLocation},
			{Key: "raw_modifier", Where: inLocation},
			{Key: "capital_modifier", Where: "In the location, if it is the capital"},
			{Key: "market_center_modifier", Where: "In the location, if it is a market center"},
			{Key: "capital_country_modifier", Where: "For the country, if built in its capital"},
		},
		Cost: []string{"build_time", "price"},
	},
	{Game: game.EU5, Database: "goods", Cost: []string{"default_market_price"}},

	// in_game/common/advances/0_age_of_discovery.txt:307 writes
	// global_institution_growth_modifier = 0.20 beside the advance's age
	{Game: game.EU5, Database: "advances", Loose: true},
}

// costLabels holds what the page calls each key of detailRows that writes a
// cost, a price or a time as a number. A key with no label is not shown. A
// Vic3 good's cost is its base price; a CK3 building's cost is a block, which
// is no number.
var costLabels = map[string]string{
	"cost_gold":             "Gold",
	"cost_prestige":         "Prestige",
	"construction_time":     buildTimeLabel,
	"required_construction": "Construction",
	"build_time":            buildTimeLabel,
	"base_price":            "Base price",
	"default_market_price":  "Base price",
	"cost":                  "Base price",
}

// buildTimeLabel is the label of a cost the games write in days, which is
// shown as years or months where the days come to whole ones.
const buildTimeLabel = "Takes to build"

// describeDays returns a number of days in words: whole years of 365 days,
// else whole months of 30, else the days, as "3 years", "6 months" or "45
// days".
func describeDays(amount float64) string {
	units := []struct {
		Size       float64
		One, Other string
	}{{365, "year", "years"}, {30, "month", "months"}}
	for _, unit := range units {
		count := amount / unit.Size
		if count != math.Trunc(count) {
			continue
		}

		if count == 1 {
			return "1 " + unit.One
		}

		return formatNumber(count) + " " + unit.Other
	}

	return groupThousands(formatNumber(amount)) + " days"
}

// The labels of the facts about a node that no key of the game names.
const (
	levelsLabel = "levels"
	foundLabel  = "found in"
)

// optionKey is an event's option, and optionTitleKey the key that names
// it: a localization key, or a block that chooses one.
const (
	optionKey      = "option"
	optionTitleKey = "name"
)

// BuildDetail returns the detail of a node. It returns an error if no
// workspace is open, the game keeps no definition of the node, or its file
// cannot be read.
func (s *Service) BuildDetail(id string) (Detail, error) {
	ws, err := s.Workspace.Session()
	if err != nil {
		return Detail{}, err
	}

	mods := ws.Mods.Load()
	site, err := resolveNode(ws, mods, id)
	if err != nil {
		return Detail{}, err
	}

	tree, statement, err := ws.FindStatement(mods, site)
	if err != nil {
		return Detail{}, err
	}

	draft := newDraft(ws, mods)
	node := draft.lookupNode(toID(mods, site))
	fires, firedBy := listFires(ws, mods, site, true), mods.ListFiredBy(ws.Schema, site, true)
	node.Trigger = findCondition(ws, mods, tree, statement)
	node.Fires = makeEdges(ws, mods, fires)
	detail := Detail{
		Node:     node,
		Language: resolveLanguage(ws),
		Source:   tree.Text(tree.Statements[statement].Span),
		About:    []Fact{},
		Gives:    []Fact{},
		Sections: []Section{},
		Options:  []Section{},
		FiredBy:  makeEdges(ws, mods, firedBy),
		Links:    draft.listLinks(node, site),
		Rules:    listRules(ws, mods, tree, statement),
	}
	detail.Linked = draft.listLinked(node.ID, detail.Links)
	for _, fire := range fires {
		detail.Linked[toID(mods, fire.Target)] = makeNode(ws, mods, fire.Target, 0)
	}

	for _, fire := range firedBy {
		detail.Linked[toID(mods, fire.Source)] = makeNode(ws, mods, fire.Source, 0)
	}

	for child := range tree.Children(statement) {
		if section := makeSection(ws, mods, tree, child); section.Key == optionKey {
			detail.Options = append(detail.Options, section)
		} else {
			detail.Sections = append(detail.Sections, section)
		}
	}

	if node.Era != "" {
		detail.Linked[node.Era] = draft.lookupNode(node.Era)
	}

	if group, isGroup := draft.makeGroup(node.Group, 0); isGroup {
		detail.Linked[node.Group] = group
	}

	if node.Type != "" {
		detail.About = draft.listAbout(node)
		detail.Gives = draft.listGives(node.ID)
		detail.Sections = keepOwnTexts(ws, mods, []string{node.ID, draft.model.listLine(node.ID)[0]}, detail.Sections)
	}

	if hidden, ok := findCopy(ws, mods, site); ok {
		other := makeNode(ws, mods, hidden, 0)
		detail.Copy = &other
	}

	return detail, nil
}

// findDetailRow returns the row of a definition's database in the game. It
// returns the empty row if the game has none.
func (d *draft) findDetailRow(id string) detailRow {
	position := slices.IndexFunc(detailRows, func(row detailRow) bool {
		return row.Game == d.ws.Game.ID && row.Database == toDatabase(id)
	})
	if position < 0 {
		return detailRow{}
	}

	return detailRows[position]
}

// listAbout returns the facts listed under a node's name: each group above
// the one it is listed under, the nearest first, each labeled with what the
// game calls such a group; how many levels its line has, where it has more
// than one; where it or a group of its is found; and what it costs. Its own
// group and its era are the node's Group and Era, and a building's costs are
// rows of its line, so none of those is a fact here.
func (d *draft) listAbout(node Node) []Fact {
	about := []Fact{}
	sources := d.makeSources(node.ID)
	for position, id := range d.listGroupChain(node.ID) {
		group, isGroup := d.makeGroup(id, 0)
		if !isGroup {
			continue
		}

		sources = append(sources, d.makeSources(id)...)
		if position > 0 {
			about = append(about, Fact{Label: group.What, Text: labelNode(group)})
		}
	}

	if levels := len(d.model.listLine(node.ID)); levels > 1 {
		about = append(about, Fact{Label: levelsLabel, Text: strconv.Itoa(levels)})
	}

	for _, source := range sources {
		places := source.Places
		if source.Count > 0 {
			places = groupThousands(strconv.Itoa(source.Count)) + " " + places
		}

		about = append(about, Fact{Label: foundLabel, Text: places, When: source.Gathering})
	}

	if node.Type == lang.TypeBuilding {
		return about
	}

	return append(about, d.listCosts(node.ID)...)
}

// listCosts returns what a definition costs and how long it takes to build:
// each key its database writes a cost in that costLabels names and that
// holds a number, written or as the script value it names. It returns none
// for a definition that cannot be read.
func (d *draft) listCosts(id string) []Fact {
	tree, statement, isRead := d.findStatement(id)
	if !isRead {
		return nil
	}

	var costs []Fact
	for _, key := range d.findDetailRow(id).Cost {
		child, isWritten := tree.FindChild(statement, key)
		label, isShown := costLabels[key]
		if !isWritten || !isShown {
			continue
		}

		cost := describeNumber(d.ws, d.mods, d.values, tree, tree.Statements[child].Value)
		amount, err := strconv.ParseFloat(cost, 64)
		switch {
		case err != nil:
			continue

		case label == buildTimeLabel:
			costs = append(costs, Fact{Label: label, Text: describeDays(amount)})

		default:
			costs = append(costs, Fact{Label: label, Text: groupThousands(cost)})
		}
	}

	return costs
}

// listLinks returns every link from and to a node among the game's goods,
// buildings and technologies. A good's and a category's are joined by
// building line. For a technology it adds a link from each definition that
// tests it, and for a node of the event graph a link to each good, building
// and technology its script names.
func (d *draft) listLinks(node Node, site index.Site) []Edge {
	links := slices.Concat([]Edge{}, d.model.listFrom(node.ID), d.model.listTo(node.ID))
	switch node.Type {
	case lang.TypeGood, lang.TypeGoodCategory:
		return d.joinLines(node.ID, links)

	case lang.TypeTechnology:
		return append(links, d.listTriggers(node.ID, links)...)

	case "":
		return append(links, d.listTouched(node.ID, site)...)
	}

	return links
}

// joinLines returns the links of a good with those that end at it joined by
// building line: one link of each relation from the line's first level. Its
// Amount is the range of what the line's levels and production methods
// write, and its Via names each of them but the first level itself. A link
// that starts at the good is kept as it is, and so is the order.
func (d *draft) joinLines(good string, links []Edge) []Edge {
	var joined []Edge
	parts := map[[2]string][]Edge{}
	for _, edge := range links {
		if edge.To != good {
			joined = append(joined, edge)
			continue
		}

		for _, line := range d.listHolders(edge.From) {
			key := [2]string{line, edge.Relation}
			if len(parts[key]) == 0 {
				joined = append(joined, Edge{From: line, To: good, Relation: edge.Relation, Site: edge.Site})
			}

			parts[key] = append(parts[key], edge)
		}
	}

	for position, edge := range joined {
		lineParts := parts[[2]string{edge.From, edge.Relation}]
		if edge.To != good {
			continue
		}

		joined[position].Amount = d.describeAmounts(lineParts)
		for _, part := range lineParts {
			if part.From != edge.From {
				joined[position].Via = appendNew(joined[position].Via, labelNode(d.lookupNode(part.From)))
			}
		}
	}

	return joined
}

// listTriggers returns a link from each definition that tests a technology, as
// an event that tests has_technology, where none of the given links is
// written at the same line. A definition that tests it more than once has
// one link, at its first test.
func (d *draft) listTriggers(id string, links []Edge) []Edge {
	var triggers []Edge
	reference := toReference(id)
	for _, use := range d.mods.ListUses(reference.Database, reference.Name) {
		holder, isInside := d.mods.FindDefinition(use)
		place := lsp.LocateSite(d.ws, d.mods, use)
		isHeld := slices.ContainsFunc(links, func(edge Edge) bool {
			return edge.Site.Place.Path == place.Place.Path && edge.Site.Line == place.Line
		})
		if isInside && !isHeld {
			triggers = append(triggers, Edge{From: toID(d.mods, holder), To: id, Relation: saysTrigger, Site: place})
		}
	}

	return dropRepeats(triggers, func(edge Edge) string { return edge.From })
}

// listTouched returns a link from a definition to each good, building and
// technology its script names, the relation the type of what it names. A
// name the game keeps no definition of gives no link.
func (d *draft) listTouched(id string, site index.Site) []Edge {
	var touched []Edge
	graphTypes := lang.LookupDialect(d.ws.Game.ID).GraphTypes
	for _, use := range d.mods.ListReferences(site) {
		reference := d.mods.ToReference(use)
		referenceType := d.ws.Schema.LookupType(reference.Database)
		winner, isKept := d.mods.FindKept(d.ws.Schema, reference)
		if !slices.Contains(graphTypes, referenceType) || !isKept {
			continue
		}

		touched = append(touched, Edge{
			From: id, To: toID(d.mods, winner.Site), Relation: string(referenceType), Site: lsp.LocateSite(d.ws, d.mods, use),
		})
	}

	return touched
}

// listLinked returns the node at the other end of each link of a definition,
// by id. A link to a place that stands for many definitions has none.
func (d *draft) listLinked(id string, links []Edge) map[string]Node {
	linked := map[string]Node{}
	for _, edge := range links {
		far := edge.To
		if far == id {
			far = edge.From
		}

		if node, isDefined := d.build(far); isDefined {
			linked[far] = node
		}
	}

	return linked
}

// listRules returns the game rules a definition reads, each once.
func listRules(ws *workspace.Session, mods *index.Index, tree *jomini.Tree, statement int32) []Rule {
	rules := []Rule{}
	for child := statement + 1; child < tree.Statements[statement].Last; child++ {
		if tree.Text(tree.Statements[child].Key) != lang.GameRuleTrigger {
			continue
		}

		if rule, ok := findRule(ws, mods, tree.Unquoted(tree.Statements[child].Value)); ok {
			rules = append(rules, rule)
		}
	}

	return dropRepeats(rules, func(rule Rule) string { return rule.ID })
}

// How a game rule is written.
const (
	// ruleDefaultKey names a rule's default setting. A rule without it
	// defaults to its first.
	ruleDefaultKey = "default"

	// ruleCategoriesKey is the one block in a rule that is no setting.
	ruleCategoriesKey = "categories"

	// ruleSettingText and ruleSettingDescription are the localization keys
	// of a setting's name and description.
	ruleSettingText        = "setting_%s"
	ruleSettingDescription = "setting_%s_desc"
)

// findRule returns the game rule a setting belongs to, with its settings.
// It returns false if nothing defines the setting.
func findRule(ws *workspace.Session, mods *index.Index, setting string) (Rule, bool) {
	winner, ok := mods.FindKept(ws.Schema, lang.Reference{Database: lang.GameRuleSettings, Name: setting})
	if !ok {
		return Rule{}, false
	}

	site, ok := mods.FindDefinition(winner.Site)
	if !ok {
		return Rule{}, false
	}

	tree, statement, err := ws.FindStatement(mods, site)
	if err != nil {
		return Rule{}, false
	}

	preset, hasDefault := tree.FindChildValue(statement, ruleDefaultKey)
	options := []RuleOption{}
	for child := range tree.Children(statement) {
		key := tree.Text(tree.Statements[child].Key)
		if !tree.Statements[child].IsBlock() || key == ruleCategoriesKey {
			continue
		}

		text, _, _ := ws.LookupText(mods, fmt.Sprintf(ruleSettingText, key))
		description, _, _ := ws.LookupText(mods, fmt.Sprintf(ruleSettingDescription, key))
		options = append(options, RuleOption{Key: key, Text: text, Description: description, Default: key == preset})
	}

	if !hasDefault && len(options) > 0 {
		options[0].Default = true
	}

	return Rule{ID: tree.Text(tree.Statements[statement].Key), Options: options}, true
}
