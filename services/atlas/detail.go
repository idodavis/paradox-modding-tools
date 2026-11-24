// detail.go answers what the page shows about one node: the facts under its
// name, what it gives, its texts and options, its firings and links, the
// game rules it reads and the copy it hides.

package atlas

import (
	"cmp"
	"slices"
	"strings"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/internal/loc"
	"paradox-modding-tools/services/lsp"
	"paradox-modding-tools/services/workspace"
)

// Detail is what the page shows about one node.
type Detail struct {
	// Node is the node, with its trigger and its firings.
	Node Node `json:"node"`

	// Language is the workspace's language, else the base language.
	Language string `json:"language"`

	// Source is the definition as written, and SourceLine the line it
	// starts on, counted from one.
	Source     string `json:"source"`
	SourceLine int    `json:"sourceLine"`

	// About are the facts under the node's name: the groups above its own,
	// its levels, where it is found, what it costs and the plain facts its
	// database writes. An event has none.
	About []Fact `json:"about"`

	// Gives is what the node's modifiers give. An event has none.
	Gives []Fact `json:"gives"`

	// Sections are the keys at the top of the definition that show a text,
	// and Options its option blocks. A good, a building and a technology
	// keep only their title and description.
	Sections []Section `json:"sections"`
	Options  []Section `json:"options"`

	FiredBy []Edge `json:"firedBy"`

	// Links are the links from and to the node among the game's goods,
	// buildings and technologies, with what tests a technology and what an
	// event's script names. A good's are joined by building line.
	Links []Edge `json:"links"`

	// Linked holds the node at the other end of each firing and link, by id.
	Linked map[string]Node `json:"linked"`

	// Rules are the game rules the definition reads.
	Rules []Rule `json:"rules"`

	// Copy is the copy of its name the definition hides, nil for none.
	Copy *Node `json:"copy,omitempty"`
}

// Section is one key at the top of a definition that shows a text, or one of
// its options.
type Section struct {
	Key string `json:"key"`

	// Line counts from one.
	Line int `json:"line"`

	// When is a block's own limit, else its trigger, in words.
	When []Condition `json:"when,omitempty"`

	Texts []Version `json:"texts,omitempty"`
}

// Version is one text a key can show, with the trigger that decides it.
type Version struct {
	// Key is the text's localization key.
	Key  string `json:"key"`
	Text string `json:"text"`

	// Segments are the pieces of the text, nested keys filled in.
	Segments []loc.Segment `json:"segments"`

	// Missing is set where the detail's language has no message for the key.
	Missing bool `json:"missing,omitempty"`

	// When is the text's trigger in words, nil for a text that always shows.
	When []Condition `json:"when,omitempty"`

	// Line counts from one.
	Line int `json:"line"`

	// Message is where the key's text is written. It is the zero Site for a
	// key with no message.
	Message lsp.Site `json:"message"`
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

// The relations of a detail's links that no link row reads: a definition that
// tests a technology, and what an event's script names.
const (
	relationChecks index.Relation = "checks"
	relationNames  index.Relation = "names"
)

// factRow is the plain facts one database's definitions write at their top,
// beside what they give: what a definition costs, and the keys of the facts
// the game's own notes document.
type factRow struct {
	Game     string
	Database string

	// Costs are the keys of what a definition costs. Facts are the other
	// keys, each said by its key in words. Head are the facts a building's
	// head says in its quiet line.
	Costs, Facts, Head []string
}

// factRows are the facts of each game's goods, buildings, eras and groups.
var factRows = []factRow{
	// regular_district_buildings/00_guild_district_buildings.txt:41-47 writes
	// cost_gold and the time
	{Game: game.CK3, Database: "buildings", Costs: []string{"cost_gold", "cost_prestige", "construction_time"}},
	{Game: game.CK3, Database: "domiciles/buildings", Costs: []string{"construction_time"}},
	// common/districts/_districts.info, common/trade/trade_good_types/
	// _trade_good_type.info and trade_good_categories/_trade_good_categories.info
	{Game: game.CK3, Database: "districts", Facts: []string{"can_be_replaced", "rebuild_cost", "governments", "provinces"}},
	{Game: game.CK3, Database: "trade/trade_good_types", Costs: []string{"base_price"}, Facts: []string{"origin", "is_basic"}},
	{Game: game.CK3, Database: "trade/trade_good_categories", Facts: []string{"milestone_interval", "trade_route_length_mult"}},
	// common/buildings/01_industry.txt:197 and buildings.md,
	// goods/00_goods.txt:11 and goods.md, technology/eras/eras.md
	{
		Game: game.Vic3, Database: "buildings", Costs: []string{"required_construction"},
		Facts: []string{"expandable", "city_type", "ownership_type", "buildable", "has_max_level"},
	},
	{Game: game.Vic3, Database: "goods", Costs: []string{"cost"}, Facts: []string{
		"prestige_factor", "traded_quantity", "convoy_cost_multiplier", "obsession_chance", "consumption_tax_cost",
		"local", "tradeable", "fixed_price",
	}},
	{Game: game.Vic3, Database: "technology/eras", Facts: []string{"technology_cost", "start_date", "end_date"}},
	// in_game/common/building_types/production_books.txt:20 and readme.txt,
	// goods/02_produced_goods.txt:5 and readme.txt
	{
		Game: game.EU5, Database: "building_types", Costs: []string{"build_time"},
		Facts: []string{"is_foreign", "construction_demand", "custom_tags", "need_good_relation", "always_add_demands"},
		Head:  []string{"city", "town", "rural_settlement", "max_levels"},
	},
	{Game: game.EU5, Database: "goods", Costs: []string{"default_market_price"}, Facts: []string{
		"demand_add", "transport_cost", "demand_multiply", "custom_tags", "food", "base_production",
	}},
}

// costLabels holds what the page calls each cost key. buildTimes are the cost
// keys the games write in days.
var (
	costLabels = map[string]string{
		"cost_gold": "Gold", "cost_prestige": "Prestige", "construction_time": "Takes to build",
		"required_construction": "Construction", "build_time": "Takes to build", "base_price": "Base price",
		"default_market_price": "Base price", "cost": "Base price",
	}
	buildTimes = []string{"construction_time", "build_time"}
)

// gatherings holds, for each game that writes how a raw good is gathered, the
// key that says it and the value taken where it is left out: EU5's goods
// write method = mining, and their readme says a good with no method farms.
var gatherings = map[string]struct{ Key, Default string }{game.EU5: {Key: "method", Default: "farming"}}

// foundPlaces holds what each game calls the places a good is found in, and
// the key under which a definition lists them: CK3's
// county_potential_trade_goods.txt:68 writes trade_good_wood = { counties =
// { c_onega_BJA … } }. A game with no such list counts the places.
var foundPlaces = map[string]struct{ Places, List string }{
	game.CK3:  {Places: "counties", List: "counties"},
	game.Vic3: {Places: "state regions"},
	game.EU5:  {Places: "locations"},
}

// The keys of a game rule and of the trigger that reads one
// (common/game_rules/00_game_rules.txt): a rule's settings are its blocks but
// categories, its default names one, and each setting's texts are
// setting_<key> and setting_<key>_desc.
const (
	ruleTrigger    = "has_game_rule"
	ruleCategories = "categories"
	ruleDefault    = "default"
	settingPrefix  = "setting_"
	settingEnding  = "_desc"
)

// textChoices are the blocks of a description that choose one text inside
// them, and triggeredText a description's text shown when its trigger holds.
var textChoices = []string{"first_valid", "random_valid", "triggered_desc"}

// BuildDetail returns the detail of a node. It returns an error if no
// workspace is open or the game keeps no definition of the node.
func (s *Service) BuildDetail(id string) (Detail, error) {
	ws, err := s.Workspace.Session()
	if err != nil {
		return Detail{}, err
	}

	mods := ws.Mods.Load()
	definition, err := findSubject(ws, mods, id)
	if err != nil {
		return Detail{}, err
	}

	detail := Detail{
		Node: makeNode(ws, mods, definition), Language: ws.Languages()[0], Linked: map[string]Node{},
		About: listAbout(ws, mods, definition), Gives: listGives(ws, mods, definition),
		Sections: []Section{}, Options: []Section{}, FiredBy: []Edge{}, Links: []Edge{}, Rules: []Rule{},
	}
	describeNode(ws, mods, &detail.Node, definition)
	detail.Node.Painting, detail.Node.Summary = ws.FindPainting(mods, definition), describeHead(ws, mods, definition)
	if group, _, ok := findGroup(ws, mods, definition); ok {
		detail.Node.Painting = cmp.Or(detail.Node.Painting, group.Painting)
	}

	detail.describeLevel(ws, mods, definition)
	if document, statement, ok := walkStatement(ws, mods, definition); ok {
		span := document.Tree.Statements[statement].Span
		detail.Source, detail.SourceLine = document.Tree.Text(span), document.Tree.Lines.ToPosition(span.Start).Line+1
		detail.Sections, detail.Options = listSections(ws, mods, document, statement, detail.Node.Type)
		detail.Rules = listRules(ws, mods, document, statement)
	}

	isDescribed := slices.ContainsFunc(detail.Sections, func(section Section) bool { return section.Key == lang.DescriptionKey })
	if built, ok := describeBuilt(ws, mods, definition); ok && !isDescribed {
		detail.Sections = append(detail.Sections, built)
	}

	detail.addFirings(ws, mods, definition)
	detail.addLinks(ws, mods, definition)
	winner, _ := mods.FindKeptLocal(ws.Schema, mods.ToReference(definition), definition.File)
	if len(winner.Beaten) > 0 {
		hidden := winner.Beaten[len(winner.Beaten)-1]
		detail.Copy = &Node{
			ID: detail.Node.ID, Database: detail.Node.Database, Name: detail.Node.Name, Title: detail.Node.Title,
			Origin: mods.File(hidden).Origin, Site: lsp.MakeSite(ws, mods, hidden),
		}
	}

	return detail, nil
}

// describeLevel says what a building's level makes, takes, employs, costs and
// gives with no condition as the node's line of that one level, and keeps
// among what it gives only what holds under a condition. A fact a met need
// gives says the need before its own condition.
func (d *Detail) describeLevel(ws *workspace.Session, mods *index.Index, definition index.Site) {
	for position, fact := range d.Gives {
		if need, err := findSubject(ws, mods, fact.Need); err == nil {
			met := "when its need for " + ws.LookupTitle(mods, need) + " is met"
			d.Gives[position].When = strings.TrimSuffix(met+", "+fact.When, ", ")
		}
	}

	if d.Node.Type != lang.TypeBuilding {
		return
	}

	d.Node.Line = buildLine(ws, mods, []index.Site{definition}, Filter{}, nil)
	d.Gives = slices.DeleteFunc(d.Gives, func(fact Fact) bool { return fact.When == "" })
}

// describeHead returns the line under a detail's title: what the node is, the
// group it is listed under and its era, or the era of the technology that
// unlocks it, as "Building in Forestry District · High Medieval". An era the
// group already names is left out.
func describeHead(ws *workspace.Session, mods *index.Index, definition index.Site) string {
	head := lang.DescribeDefinition(mods.ToReference(definition).Database, false)
	head = strings.ToUpper(head[:1]) + head[1:]
	groups := listGroups(ws, mods, definition)
	if len(groups) > 0 {
		head += " in " + groups[0].Title
	}

	era := findEra(ws, mods, definition)
	unlocks := pickLinks(ws, mods, listLevels(ws, mods, definition)[0], index.RelationUnlock, false)
	if era == "" && len(unlocks) > 0 {
		era = findEra(ws, mods, unlocks[0].Source)
	}

	eras := listEras(ws, mods)
	position := slices.IndexFunc(eras, func(node Node) bool { return node.ID == era })
	if position >= 0 && (len(groups) == 0 || groups[0].Title != eras[position].Title) {
		head += " · " + eras[position].Title
	}

	return head
}

// addFirings adds what the node fires, as its node's firings, and what fires
// it, each through the scripted effects between. Two firings at one line of
// one file are one.
func (d *Detail) addFirings(ws *workspace.Session, mods *index.Index, definition index.Site) {
	seen := map[index.Site]bool{}
	add := func(edges []Edge, fire index.Fire, other index.Site) []Edge {
		line := index.Site{File: fire.Use.File, Occurrence: mods.Occurrence(fire.Use).Line}
		if seen[line] {
			return edges
		}

		seen[line] = true
		node := makeNode(ws, mods, other)
		d.Linked[node.ID] = node
		edge := makeEdge(ws, mods, mods.ToReference(fire.Source).ToID(), mods.ToReference(fire.Target).ToID(), fire.Use)
		for _, effect := range fire.Via {
			edge.Via = append(edge.Via, mods.ToReference(effect).Name)
		}

		return append(edges, edge)
	}

	for _, written := range mods.ListActing(ws.Schema, mods.ToReference(definition), definition.File) {
		for _, fire := range mods.ListFires(ws.Schema, written, true) {
			d.Node.Fires = add(d.Node.Fires, fire, fire.Target)
		}
	}

	seen = map[index.Site]bool{}
	for _, fire := range mods.ListFiredBy(ws.Schema, definition, true) {
		d.FiredBy = add(d.FiredBy, fire, fire.Source)
	}
}

// addLinks adds the node's links with their amounts in words. A good's and a
// category's are joined by building line: one link of each relation from the
// line's first level, its amount the range over the line. A technology adds
// each definition that tests it, and an event each good, building and
// technology its script names.
func (d *Detail) addLinks(ws *workspace.Session, mods *index.Index, definition index.Site) {
	id := d.Node.ID
	isGood := d.Node.Type == lang.TypeGood || d.Node.Type == lang.TypeGoodCategory
	put := func(edge Edge, other index.Site) {
		node := makeNode(ws, mods, other)
		d.Linked[node.ID] = node
		position := slices.IndexFunc(d.Links, func(link Edge) bool {
			return link.From == edge.From && link.To == edge.To && link.Relation == edge.Relation
		})
		if position < 0 {
			d.Links = append(d.Links, edge)
		}
	}

	if isGood {
		r := &reach{ws: ws, mods: mods, sites: map[string]index.Site{}, positions: map[string]int{}}
		makers, users := r.listTies(definition)
		for _, member := range slices.Concat(makers, users) {
			node := r.describeTie(member, 1)
			edge := Edge{From: node.ID, To: id, Relation: member.relation, Via: strings.Fields(member.via), Site: lsp.MakeSite(ws, mods, member.use)}
			if len(node.Facts) > 0 {
				edge.Amount = node.Facts[0].Text
			}

			put(edge, member.line)
		}
	}

	for _, link := range mods.ListLinks(ws.Schema, definition) {
		isTie := link.Target == definition && (slices.Contains(makerRelations, link.Relation) || link.Relation == index.RelationInput)
		if isGood && isTie {
			continue
		}

		edge := Edge{
			From: mods.ToReference(link.Source).ToID(), To: mods.ToReference(link.Target).ToID(), Relation: link.Relation,
			Share: link.Share, Site: lsp.MakeSite(ws, mods, link.Use),
		}
		if number, ok := readAmount(ws, mods, link); ok {
			edge.Amount = describeRange([]float64{number}, link.Share)
		}

		if link.Count > 0 {
			edge.Amount = describeNumber(float64(link.Count))
		}

		other := link.Target
		if link.Target == definition {
			other = link.Source
		}

		put(edge, other)
	}

	d.addMentions(ws, mods, definition, put)
}

// addMentions adds the links no row reads: for a technology each definition
// that tests it, at its first test, and for a definition that fires each
// good, building and technology its script names.
func (d *Detail) addMentions(ws *workspace.Session, mods *index.Index, definition index.Site, put func(Edge, index.Site)) {
	reference := mods.ToReference(definition)
	drawn := lang.LookupDialect(ws.Game.ID).AtlasTypes
	if d.Node.Type == lang.TypeTechnology {
		for _, use := range mods.ListUses(reference.Database, reference.Name) {
			holder, nested := mods.FindDefinition(use)
			if nested && mods.Occurrence(use).Link == 0 && holder != definition {
				put(Edge{From: mods.ToReference(holder).ToID(), To: d.Node.ID, Relation: relationChecks, Site: lsp.MakeSite(ws, mods, use)}, holder)
			}
		}
	}

	if !slices.Contains(ws.Schema.FireDatabases, reference.Database) {
		return
	}

	for _, use := range mods.ListReferences(definition) {
		named := mods.ToReference(use)
		winner, kept := mods.FindKept(ws.Schema, named)
		if kept && slices.Contains(drawn, ws.Schema.LookupType(named.Database)) {
			put(Edge{From: d.Node.ID, To: named.ToID(), Relation: relationNames, Site: lsp.MakeSite(ws, mods, use)}, winner.Site)
		}
	}
}

// listAbout returns the facts under a node's name: each group above its own,
// nearest first, as what the group is and its name; its levels where it is a
// line of several; where it and each of its groups is found; what it costs,
// but for a building, whose table says it; and the plain facts its database
// writes.
func listAbout(ws *workspace.Session, mods *index.Index, definition index.Site) []Fact {
	about := []Fact{}
	sites := []index.Site{definition}
	for _, group := range listGroups(ws, mods, definition) {
		about = append(about, Fact{Label: group.What, Text: group.Title})
		if site, err := findSubject(ws, mods, group.ID); err == nil {
			sites = append(sites, site)
		}
	}

	if levels := listLevels(ws, mods, definition); len(levels) > 1 {
		about = append(about, Fact{Label: "levels", Text: describeNumber(float64(len(levels)))})
	}

	for _, site := range sites {
		if found := describeFound(ws, mods, site); found != "" {
			about = append(about, Fact{Label: "found in", Text: strings.TrimPrefix(found, "Found in ")})
		}
	}

	if ws.Schema.LookupType(mods.ToReference(definition).Database) != lang.TypeBuilding {
		about = append(about, listCosts(ws, mods, definition)...)
	}

	return append(about, listFacts(ws, mods, definition, false)...)
}

// listCosts returns what a definition costs: each labeled cost key at its top
// that holds a number, a time in days said in years, months or days.
func listCosts(ws *workspace.Session, mods *index.Index, definition index.Site) []Fact {
	row, tree, statement, ok := findFactRow(ws, mods, definition)
	if !ok {
		return nil
	}

	var costs []Fact
	for _, key := range row.Costs {
		child, written := tree.FindChild(statement, key)
		number, isNumber := resolveNumber(ws, mods, tree, tree.Statements[child].Value)
		if !written || !isNumber {
			continue
		}

		cost := Fact{Label: costLabels[key], Text: describeNumber(number)}
		if slices.Contains(buildTimes, key) {
			cost.Text = describeDays(number)
		}

		costs = append(costs, cost)
	}

	return costs
}

// listHeadFacts returns the facts a building's head says in its quiet line.
func listHeadFacts(ws *workspace.Session, mods *index.Index, definition index.Site) []Fact {
	return listFacts(ws, mods, definition, true)
}

// listFacts returns the plain facts a definition's database writes at its
// top, a building's head's or the rest: each key in words with its value,
// which is a number, Yes or No, the title of the definition it names, or the
// word as written. A key whose value is a block is no fact.
func listFacts(ws *workspace.Session, mods *index.Index, definition index.Site, isHead bool) []Fact {
	row, tree, statement, ok := findFactRow(ws, mods, definition)
	if !ok {
		return nil
	}

	keys := row.Facts
	if isHead {
		keys = row.Head
	}

	var facts []Fact
	for _, key := range keys {
		child, written := tree.FindChild(statement, key)
		if !written || tree.Statements[child].IsBlock() {
			continue
		}

		words := toWords(key)
		value := tree.Unquoted(tree.Statements[child].Value)
		fact := Fact{Label: strings.ToUpper(words[:1]) + words[1:], Text: value}
		number, isNumber := resolveNumber(ws, mods, tree, tree.Statements[child].Value)
		switch title, _, isText := ws.LookupText(mods, value); {
		case isNumber:
			fact.Text = describeNumber(number)
		case slices.Contains(lang.Bools, value):
			fact.Text = strings.ToUpper(value[:1]) + value[1:]
		case isText:
			fact.Text = ws.FlattenText(mods, title)
		}

		facts = append(facts, fact)
	}

	return facts
}

// findFactRow returns the row of a definition's database with the
// definition's statement. It returns false for a database with no row and
// for a file that changed since the scan.
func findFactRow(ws *workspace.Session, mods *index.Index, definition index.Site) (factRow, *jomini.Tree, int32, bool) {
	database := mods.ToReference(definition).Database
	position := slices.IndexFunc(factRows, func(row factRow) bool {
		return row.Game == ws.Game.ID && row.Database == database
	})
	tree, statement, err := ws.FindStatement(mods, definition)
	if position < 0 || err != nil {
		return factRow{}, nil, 0, false
	}

	return factRows[position], tree, statement, true
}

// describeFound returns where a good is found, as "Found in 2,523 locations ·
// Logging": how many places hold it, in the game's word for them, and how it
// is gathered where the game says. It returns "" for a definition found
// nowhere.
func describeFound(ws *workspace.Session, mods *index.Index, definition index.Site) string {
	places := foundPlaces[ws.Game.ID]
	count := 0
	for _, link := range pickLinks(ws, mods, definition, index.RelationResource, true) {
		count += link.Count
		tree, statement, err := ws.FindStatement(mods, link.Target)
		if list, ok := tree.FindChild(statement, places.List); err == nil && ok && link.Count == 0 {
			count += int(tree.Statements[list].Last - list - 1)
		}
	}

	if count == 0 {
		return ""
	}

	found := "Found in " + describeNumber(float64(count)) + " " + places.Places
	row := gatherings[ws.Game.ID]
	if tree, statement, err := ws.FindStatement(mods, definition); err == nil && row.Key != "" {
		method, _ := tree.FindChildValue(statement, row.Key)
		method = cmp.Or(method, row.Default)
		text, _, _ := ws.LookupText(mods, method)
		words := cmp.Or(ws.FlattenText(mods, text), toWords(method))
		found += " · " + strings.ToUpper(words[:1]) + words[1:]
	}

	return found
}

// listSections returns the texts a definition shows: each key at its top
// that names a localization key or chooses one, each text with its condition,
// and its option blocks apart. A good, a building and a technology keep only
// their title and description, the description the game builds from the name
// where the definition writes none.
func listSections(ws *workspace.Session, mods *index.Index, document lang.Document, statement int32, nodeType lang.Type) (sections, options []Section) {
	tree := document.Tree
	sections, options = []Section{}, []Section{}
	for child := range tree.Children(statement) {
		current := tree.Statements[child]
		key := tree.Text(current.Key)
		section := Section{Key: key, Line: tree.Lines.ToPosition(current.Span.Start).Line + 1}
		isText := slices.ContainsFunc(document.Contexts[child].References, func(reference lang.Reference) bool {
			return reference.Database == lang.Localization
		})
		switch {
		case key == optionKey:
			section.When = describeLimit(ws, mods, document, child)
			if name, ok := tree.FindChild(child, optionName); ok {
				section.Texts = listVersions(ws, mods, document, name)
			}

			options = append(options, section)

		case nodeType != "" && key != lang.TitleKey && key != lang.DescriptionKey:
		case isText || current.IsBlock() && (key == lang.DescriptionKey || key == lang.TitleKey || key == optionName):
			section.Texts = listVersions(ws, mods, document, child)
			if len(section.Texts) > 0 {
				sections = append(sections, section)
			}
		}
	}

	return sections, options
}

// listVersions returns the texts one key can show: the key it names, or each
// text of the block that chooses one, with its condition. A block's own text
// is its desc, else its text key, and either may be a block that chooses in
// its turn, as CK3 writes an option's
//
//	name = { text = { first_valid = { ... } } }
func listVersions(ws *workspace.Session, mods *index.Index, document lang.Document, statement int32) []Version {
	tree := document.Tree
	current := tree.Statements[statement]
	if !current.IsBlock() {
		return []Version{makeVersion(ws, mods, document, statement, nil)}
	}

	var versions []Version
	for child := range tree.Children(statement) {
		key := tree.Text(tree.Statements[child].Key)
		isOwn := key == lang.DescriptionKey || key == lang.LabelKey
		switch {
		case tree.Statements[child].IsBlock() && (isOwn || slices.Contains(textChoices, key)):
			for _, version := range listVersions(ws, mods, document, child) {
				if version.When == nil {
					version.When = describeLimit(ws, mods, document, child)
				}

				versions = append(versions, version)
			}

		case isOwn:
			versions = append(versions, makeVersion(ws, mods, document, child, describeLimit(ws, mods, document, statement)))
		}
	}

	return versions
}

// makeVersion returns the text a statement's value names, with the given
// condition. It is missing where the workspace's language has no message for
// the key.
func makeVersion(ws *workspace.Session, mods *index.Index, document lang.Document, statement int32, when []Condition) Version {
	tree := document.Tree
	current := tree.Statements[statement]
	version, ok := lookupVersion(ws, mods, tree.Unquoted(current.Value))
	version.When, version.Line = when, tree.Lines.ToPosition(current.Span.Start).Line+1
	if !ok {
		version.Missing = current.ValueClass.IsIdentifier()
	}

	return version
}

// lookupVersion returns the text of a localization key, as one line and in
// its pieces, with where its message is written. It returns false, with the
// key alone, for a key no message holds.
func lookupVersion(ws *workspace.Session, mods *index.Index, key string) (Version, bool) {
	version := Version{Key: key}
	text, winner, ok := ws.LookupText(mods, key)
	if !ok {
		return version, false
	}

	version.Text, version.Segments = ws.FlattenText(mods, text), ws.ExpandText(mods, text)
	version.Message = lsp.MakeSite(ws, mods, winner.Site)
	version.Missing = mods.Language(winner.Site) != ws.Languages()[0]

	return version, true
}

// describeBuilt returns the description the game builds from a definition's
// name, or from its line's first level's, as a section of its texts. It
// returns false where the game has no such text.
func describeBuilt(ws *workspace.Session, mods *index.Index, definition index.Site) (Section, bool) {
	for _, named := range []index.Site{definition, listLevels(ws, mods, definition)[0]} {
		key, ok := ws.FindBuiltKey(mods, mods.ToReference(named), lang.DescriptionKey)
		if !ok {
			continue
		}

		if version, found := lookupVersion(ws, mods, key); found {
			return Section{Key: lang.DescriptionKey, Texts: []Version{version}}, true
		}
	}

	return Section{}, false
}

// listRules returns the game rules a definition reads, each once with its
// settings: every block of the rule but its categories, the default the one
// the rule names, else the first.
func listRules(ws *workspace.Session, mods *index.Index, document lang.Document, statement int32) []Rule {
	rules := []Rule{}
	tree := document.Tree
	for position := statement + 1; position < tree.Statements[statement].Last; position++ {
		if tree.Text(tree.Statements[position].Key) != ruleTrigger {
			continue
		}

		for _, reference := range document.Contexts[position].References {
			winner, kept := mods.FindKept(ws.Schema, reference)
			if !kept {
				continue
			}

			holder, nested := mods.FindDefinition(winner.Site)
			if !nested || slices.ContainsFunc(rules, func(rule Rule) bool { return rule.ID == mods.ToReference(holder).Name }) {
				continue
			}

			rules = append(rules, makeRule(ws, mods, holder))
		}
	}

	return rules
}

// makeRule returns a game rule with its settings and their texts.
func makeRule(ws *workspace.Session, mods *index.Index, definition index.Site) Rule {
	rule := Rule{ID: mods.ToReference(definition).Name, Options: []RuleOption{}}
	tree, statement, err := ws.FindStatement(mods, definition)
	if err != nil {
		return rule
	}

	chosen, _ := tree.FindChildValue(statement, ruleDefault)
	for child := range tree.Children(statement) {
		key := tree.Text(tree.Statements[child].Key)
		if !tree.Statements[child].IsBlock() || key == ruleCategories {
			continue
		}

		text, _, _ := ws.LookupText(mods, settingPrefix+key)
		description, _, _ := ws.LookupText(mods, settingPrefix+key+settingEnding)
		rule.Options = append(rule.Options, RuleOption{
			Key: key, Text: ws.FlattenText(mods, text), Description: ws.FlattenText(mods, description),
			Default: key == chosen || chosen == "" && len(rule.Options) == 0,
		})
	}

	return rule
}
