// detail.go builds what the page shows about one node of the Graph.

package graph

import (
	"fmt"
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
	// Node is the node, with its trigger and its firings.
	Node Node `json:"node"`

	// Language is the workspace's language, else the base language.
	Language string `json:"language"`

	// Source is the definition as written.
	Source string `json:"source"`

	// About are the facts under the node's name: the groups above its own,
	// its levels, where it is found and what it costs. An event has none.
	About []Fact `json:"about"`

	// Gives is what the node's modifiers give. An event has none.
	Gives []Fact `json:"gives"`

	// Sections are the keys at the top of the definition that show a text,
	// and Options its option blocks. A good, a building and a technology keep
	// only their title and description.
	Sections []Section `json:"sections"`
	Options  []Section `json:"options"`

	FiredBy []Edge `json:"firedBy"`

	// Links are the links from and to the node among the game's goods,
	// buildings and technologies, with what tests a technology and what an
	// event's script names. A good's are joined by building line.
	Links []Edge `json:"links"`

	// Linked holds the node at the other end of each firing and link, by id,
	// and the node's own group and era.
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

	// Text and Description are the setting's localized name and description,
	// "" where it has none.
	Text        string `json:"text"`
	Description string `json:"description"`

	Default bool `json:"default,omitempty"`
}

// detailRow is the keys one game's database writes what a definition gives
// and costs in.
type detailRow struct {
	Game     string
	Database string

	// Bonuses are the blocks that hold what a definition gives, and Cost the
	// keys that hold what it costs.
	Bonuses []modifierBlock
	Cost    []string

	// Loose is set where the modifiers stand among the other keys, in no
	// block.
	Loose bool

	// Custom is the keys at the top whose value is the localization key of a
	// line the definition gives, written in the game's own words.
	Custom []string
}

// ck3Modifiers are the blocks of a CK3 building that hold what it gives, as
// regular_district_buildings/00_guild_district_buildings.txt:36 writes one.
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

// modifierKey is the block of a Vic3 technology and an EU5 building that
// holds what it gives.
const modifierKey = "modifier"

var detailRows = []detailRow{
	// regular_district_buildings/00_guild_district_buildings.txt:41-47 writes
	// cost_gold and the time; special_buildings/00_legendary_buildings.txt:45
	// and common/domiciles/buildings a cost block
	{
		Game: game.CK3, Database: "buildings", Bonuses: ck3Modifiers,
		Cost: []string{"cost_gold", "cost_prestige", "cost", "construction_time"},
	},
	{Game: game.CK3, Database: "domiciles/buildings", Bonuses: ck3Modifiers, Cost: []string{"cost", "construction_time"}},

	// common/culture/innovations/00_tribal_innovations.txt writes a modifier
	// block for each of these scopes, and 00_high_medieval_innovations.txt:366
	// custom = fabricate_claim_speed, a line of the innovation's effects
	// (_culture_innovations.info)
	{
		Game: game.CK3, Database: "culture/innovations",
		Bonuses: []modifierBlock{
			{Key: "character_modifier", Where: "For the culture's characters"},
			{Key: "county_modifier", Where: "In the culture's counties"},
			{Key: "province_modifier", Where: "In the culture's provinces"},
			{Key: "culture_modifier", Where: "For the culture"},
		},
		Custom: []string{"custom"},
	},
	{Game: game.CK3, Database: "trade/trade_good_types", Cost: []string{"base_price"}},

	// common/buildings/01_industry.txt:197, production_methods/
	// 01_industry.txt:1922, technology/technologies/10_production.txt:11 and
	// goods/00_goods.txt:11
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

	// in_game/common/building_types/production_books.txt:20 and
	// goods/02_produced_goods.txt:5; building_types/readme.txt:30-36 says
	// what each modifier block applies to
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

// costLabels holds what the page calls each cost key that writes a number.
// A key with no label is not shown, as a CK3 building's cost block.
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

// buildTimeLabel is the label of a cost the games write in days.
const buildTimeLabel = "Takes to build"

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
		FiredBy:  makeEdges(ws, mods, firedBy),
		Links:    draft.listLinks(node, site),
		Rules:    listRules(ws, mods, tree, statement),
	}
	detail.Sections, detail.Options = listSections(ws, mods, site, tree, statement)
	detail.Linked = draft.listLinked(node.ID, detail.Links)
	for _, fire := range fires {
		detail.Linked[toID(mods, fire.Target)] = makeNode(ws, mods, fire.Target, 0)
	}

	for _, fire := range firedBy {
		detail.Linked[toID(mods, fire.Source)] = makeNode(ws, mods, fire.Source, 0)
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

// findDetailRow returns the row of a game's database, empty if it has none.
func findDetailRow(game, database string) detailRow {
	position := slices.IndexFunc(detailRows, func(row detailRow) bool {
		return row.Game == game && row.Database == database
	})
	if position < 0 {
		return detailRow{}
	}

	return detailRows[position]
}

// listAbout returns the facts under a node's name: each group above its own,
// nearest first, the levels of a line of several, where it or a group of its
// is found, and what it costs. A building's costs are rows of its line.
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
			places = describeAmount(float64(source.Count), d.formatAmount(false)) + " " + places
		}

		about = append(about, Fact{Label: foundLabel, Text: places, When: source.Gathering})
	}

	if node.Type == lang.TypeBuilding {
		return about
	}

	return append(about, d.listCosts(node.ID)...)
}

// listCosts returns what a definition costs and how long it takes to build:
// each labeled cost key at its top that holds a number, in its database's
// order.
func (d *draft) listCosts(id string) []Fact {
	var costs []Fact
	clauses := d.model.clauses[id]
	for _, key := range findDetailRow(d.ws.Game.ID, toDatabase(id)).Cost {
		position := slices.IndexFunc(clauses, func(clause clause) bool { return len(clause.Blocks) == 0 && clause.Key == key })
		label, isShown := costLabels[key]
		if position < 0 || !isShown {
			continue
		}

		amount, err := strconv.ParseFloat(clauses[position].Amount, 64)
		if err != nil {
			continue
		}

		format := d.formatAmount(false)
		if label == buildTimeLabel {
			format = timeFormat
		}

		costs = append(costs, Fact{Label: label, Text: describeAmount(amount, format)})
	}

	return costs
}

// listLinks returns the links from and to a node, each amount in words. A
// good's and a category's are joined by building line. A technology adds
// what tests it, and a node of the event graph the goods, buildings and
// technologies its script names.
func (d *draft) listLinks(node Node, site index.Site) []Edge {
	links := slices.Concat([]Edge{}, d.model.listFrom(node.ID), d.model.listTo(node.ID))
	if node.Type == lang.TypeGood || node.Type == lang.TypeGoodCategory {
		return d.joinLines(node.ID, links)
	}

	for position, edge := range links {
		links[position].Amount = d.describeAmounts([]Edge{edge})
	}

	switch node.Type {
	case lang.TypeTechnology:
		return append(links, d.listTriggers(node.ID, links)...)

	case "":
		return append(links, d.listTouched(node.ID, site)...)
	}

	return links
}

// joinLines returns a good's links with those that end at it joined by
// building line: one link of each relation from the line's first level, its
// amount the range over the line and its Via the levels and methods it goes
// through. A link that starts at the good keeps its own amount.
func (d *draft) joinLines(good string, links []Edge) []Edge {
	var edges []Edge
	parts := map[[2]string][]Edge{}
	for _, edge := range links {
		if edge.To != good {
			edge.Amount = d.describeAmounts([]Edge{edge})
			edges = append(edges, edge)

			continue
		}

		for _, line := range d.listHolders(edge.From) {
			key := [2]string{line, edge.Relation}
			if len(parts[key]) == 0 {
				edges = append(edges, Edge{From: line, To: good, Relation: edge.Relation, Site: edge.Site})
			}

			parts[key] = append(parts[key], edge)
		}
	}

	for position, edge := range edges {
		lineParts := parts[[2]string{edge.From, edge.Relation}]
		if edge.To != good {
			continue
		}

		edges[position].Amount = d.describeAmounts(lineParts)
		for _, part := range lineParts {
			if part.From != edge.From {
				edges[position].Via = appendNew(edges[position].Via, labelNode(d.lookupNode(part.From)))
			}
		}
	}

	return edges
}

// listTriggers returns a link from each definition that tests a technology,
// as an event's has_technology, at its first test. A test on the line of one
// of the given links is left out.
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
// technology its script names, its relation the type of what it names.
func (d *draft) listTouched(id string, site index.Site) []Edge {
	var edges []Edge
	graphTypes := lang.LookupDialect(d.ws.Game.ID).GraphTypes
	for _, use := range d.mods.ListReferences(site) {
		reference := d.mods.ToReference(use)
		referenceType := d.ws.Schema.LookupType(reference.Database)
		winner, isKept := d.mods.FindKept(d.ws.Schema, reference)
		if !slices.Contains(graphTypes, referenceType) || !isKept {
			continue
		}

		edges = append(edges, Edge{
			From: id, To: toID(d.mods, winner.Site), Relation: string(referenceType), Site: lsp.LocateSite(d.ws, d.mods, use),
		})
	}

	return edges
}

// listLinked returns the node at the other end of each link of a definition,
// by id. A link to a place that stands for many definitions has none.
func (d *draft) listLinked(id string, links []Edge) map[string]Node {
	nodes := map[string]Node{}
	for _, edge := range links {
		far := edge.To
		if far == id {
			far = edge.From
		}

		if node, isDefined := d.build(far); isDefined {
			nodes[far] = node
		}
	}

	return nodes
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

		text, _ := describeKey(ws, mods, fmt.Sprintf(ruleSettingText, key))
		description, _ := describeKey(ws, mods, fmt.Sprintf(ruleSettingDescription, key))
		options = append(options, RuleOption{Key: key, Text: text, Description: description, Default: key == preset})
	}

	if !hasDefault && len(options) > 0 {
		options[0].Default = true
	}

	return Rule{ID: tree.Text(tree.Statements[statement].Key), Options: options}, true
}
