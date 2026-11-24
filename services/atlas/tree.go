// tree.go draws a technology's tree: the game's trees and eras, the
// technologies of the subject's tree with what each requires, and whom an EU5
// advance is for.

package atlas

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/services/workspace"
)

// Tree is one of a game's technology trees.
type Tree struct {
	// ID is what its technologies share: the id of the group they are
	// listed under. Title is its name in the game's words.
	ID    string `json:"id"`
	Title string `json:"title"`

	// Painting is the file of the painting behind its head, "" for none.
	Painting string `json:"painting,omitempty"`

	// Count is how many technologies it holds. First is the id of the one
	// that opens it: the first every country can have, else the first.
	Count int    `json:"count"`
	First string `json:"first"`
}

// Audience is one row of the list of whom an age's advances are for: a group,
// or one country, religion or culture in it.
type Audience struct {
	// ID is the filter's Potential that draws the row's advances: the id of
	// a definition. A group's own row has its Group as its ID.
	ID string `json:"id"`

	// Group is the id of the group the row is in.
	Group string `json:"group"`

	Title string `json:"title"`

	// Count is how many advances the row draws.
	Count int `json:"count"`
}

// rootedTrees are the games that draw a tree as several, each from a
// technology that requires none, and whose technologies can be for some
// countries only: EU5's in_game/common/advances/0_age_of_discovery.txt:12
// writes depth = 0 on each such advance of an age. joinedTrees are the games
// whose trees are drawn together as one grid of era by group, since no
// technology requires another: no CK3 innovation writes a requirement
// (common/culture/innovations/_culture_innovations.info).
var (
	rootedTrees = []string{game.EU5}
	joinedTrees = []string{game.CK3}
)

// eraYears holds, for each game whose eras write the year they start in, the
// key of that year: CK3's common/culture/eras/00_culture_eras.txt:7 writes
// year = 843. eraTitles holds how a game whose eras have no text words one by
// its number: Vic3's interfaces_l_english.yml:4240 writes Era [Era.GetNumber].
var (
	eraYears  = map[string]string{game.CK3: "year"}
	eraTitles = map[string]string{game.Vic3: "Era %d"}
)

// audienceGroup is a group of whom an advance can be for: the definitions of
// its databases that a potential names.
type audienceGroup struct {
	ID, Title string
	Databases []string
}

// audienceGroups are the groups of EU5's advances, narrowest first: an
// advance is in the narrowest group its potential names, so one for a
// Catholic country is in the countries. otherAudience is the group of the
// advances whose potential names nobody, as one that asks for a region.
var audienceGroups = []audienceGroup{
	{ID: "countries", Title: "Countries", Databases: []string{lang.SetupCountries}},
	{ID: "cultures", Title: "Cultures", Databases: []string{"cultures", "culture_groups", "languages"}},
	{ID: "religions", Title: "Religions", Databases: []string{"religions", "religion_groups"}},
}

var otherAudience = audienceGroup{ID: "other", Title: "Other conditions"}

// potentialKey is the block of an advance that says whom it is for. It names
// them as the triggers of in_game/common/advances/0_age_of_traditions.txt:188
// do: has_or_had_tag = BYZ. scriptedTriggers is the database of the triggers
// a potential can call, as is_protestant = yes.
const (
	potentialKey     = "potential"
	scriptedTriggers = "scripted_triggers"
)

// potentialParents holds what a definition a potential can name belongs to,
// as the keys that write it and the database each names: a country's culture
// and religion (in_game/setup/countries/balkans.txt:62), a culture's language
// and groups (in_game/common/cultures/greek.txt), and a religion's group
// (in_game/common/religions/christian.txt:194).
var potentialParents = map[string]map[string]string{
	lang.SetupCountries: {"culture_definition": "cultures", "religion_definition": "religions"},
	"cultures":          {"language": "languages", "culture_groups": "culture_groups"},
	"religions":         {"group": "religion_groups"},
}

// advanceLimits are the keys that limit an EU5 advance beside its potential,
// with the words of each value the game does not name
// (in_game/common/advances/readme.txt).
var advanceLimits = []struct {
	Key   string
	Words map[string]string

	// Heading is what the advances the key limits stand under where no
	// requirement ties them to a tree. It is "" where they stand under the
	// value's own words.
	Heading string
}{
	{Key: "for", Words: map[string]string{
		"adm": "Administrative specialization", "dip": "Diplomatic specialization", "mil": "Military specialization",
	}},
	{Key: "government", Heading: "For one government"},
	{Key: "country_type", Heading: "For one country type"},
}

// looseHeading is what the advances the game does not limit stand under where
// no requirement ties them to a tree.
const looseHeading = "For every country"

// listEras returns the game's eras or ages in the order its files define
// them, each with its year where the game writes one.
func listEras(ws *workspace.Session, mods *index.Index) []Node {
	var eras []Node
	for name, database := range ws.Schema.Databases {
		if database.Declaration.Type != lang.TypeEra {
			continue
		}

		for position, winner := range mods.ListKept(ws.Schema, name) {
			era := makeNode(ws, mods, winner.Site)
			if tree, statement, err := ws.FindStatement(mods, winner.Site); err == nil {
				era.Year, _ = tree.FindChildValue(statement, eraYears[ws.Game.ID])
			}

			if pattern := eraTitles[ws.Game.ID]; pattern != "" {
				era.Title = fmt.Sprintf(pattern, position+1)
			}

			era.Painting = ws.FindPainting(mods, winner.Site)
			eras = append(eras, era)
		}
	}

	return eras
}

// findEra returns the id of a technology's era or age, "" for none.
func findEra(ws *workspace.Session, mods *index.Index, technology index.Site) string {
	if eras := pickLinks(ws, mods, technology, index.RelationEra, true); len(eras) > 0 {
		return mods.ToReference(eras[0].Target).ToID()
	}

	return ""
}

// buildTree returns the canvas of a technology: the technologies of its tree
// with an edge to each from what it requires, the game's trees and eras, and
// how many technologies the subject needs and leads to. A game whose trees
// are joined sends every tree, with what its technologies unlock. An EU5 age
// draws the advances every country can have, those of the audience picked,
// those of the subject's own audience, and whatever a drawn one requires.
func buildTree(ws *workspace.Session, mods *index.Index, subject index.Site, filter Filter) Graph {
	database := mods.ToReference(subject).Database
	isJoined, isRooted := slices.Contains(joinedTrees, ws.Game.ID), slices.Contains(rootedTrees, ws.Game.ID)
	r := &reach{
		ws: ws, mods: mods, filter: filter, sites: map[string]index.Site{}, positions: map[string]int{},
		walked: map[string]bool{},
	}
	r.graph.Root, r.graph.Eras = mods.ToReference(subject).ToID(), listEras(ws, mods)

	own := findTree(ws, mods, subject)
	drawn := listAudienceIDs(ws, mods, filter.Potential)
	audiences, limited := map[string]int{}, map[index.Site]bool{}
	var subjectAudience string
	if isRooted {
		subjectAudience, _ = findAudience(ws, mods, subject)
	}

	var members []Node
	for _, winner := range mods.ListKept(ws.Schema, database) {
		var audience, limit string
		if isRooted {
			audience, limit = findAudience(ws, mods, winner.Site)
		}

		group := findTree(ws, mods, winner.Site)
		limited[winner.Site] = audience != ""
		r.countTree(group, winner.Site, audience == "")
		if group.ID != own.ID && !isJoined {
			continue
		}

		node := r.describeTechnology(winner.Site, group.ID)
		if isRooted {
			node.Audience, node.Limit, node.Root = audience, limit, findRoot(ws, mods, winner.Site)
		}

		audiences[audience]++
		if audience == "" || audience == subjectAudience || slices.Contains(drawn, audience) {
			r.put(node, winner.Site)
		}

		members = append(members, node)
	}

	r.addRequired(members)
	r.linkRequired()
	if isRooted {
		r.placeLoose()
	}

	r.graph.Audiences = listAudiences(ws, mods, audiences)
	r.graph.Needs, _ = r.countChain(subject, true, limited)
	r.graph.Leads, r.graph.SharedLeads = r.countChain(subject, false, limited)
	r.graph.Total = len(members)
	if isJoined {
		r.graph.Unlocks = countUnlocks(r.graph.Nodes)
	}

	r.settleEras()

	return r.graph
}

// settleEras counts each era's drawn technologies, and puts the trees that
// are eras, as EU5's ages are, in the eras' order. Any other tree keeps the
// order the files reach it in.
func (r *reach) settleEras() {
	for position, era := range r.graph.Eras {
		r.graph.Eras[position].Count = len(slices.DeleteFunc(slices.Clone(r.graph.Nodes), func(node Node) bool {
			return node.Era != era.ID
		}))
	}

	place := func(tree Tree) int {
		position := slices.IndexFunc(r.graph.Eras, func(era Node) bool { return era.ID == tree.ID })

		return cmp.Or(position+1, len(r.graph.Eras)+1)
	}
	slices.SortStableFunc(r.graph.Trees, func(first, second Tree) int { return place(first) - place(second) })
}

// findTree returns the group a technology's tree is: the group it is listed
// under, as a Vic3 category or an EU5 age. A technology in no group is in a
// tree with no id.
func findTree(ws *workspace.Session, mods *index.Index, technology index.Site) Node {
	group, _, _ := findGroup(ws, mods, technology)

	return group
}

// countTree counts one technology into its tree, adding the tree on its
// first. A tree opens on the first technology every country can have, else
// on its first. Its painting is its group's, else that technology's.
func (r *reach) countTree(group Node, technology index.Site, isShared bool) {
	position := slices.IndexFunc(r.graph.Trees, func(tree Tree) bool { return tree.ID == group.ID })
	isFirst := position < 0
	if isFirst {
		position, r.graph.Trees = len(r.graph.Trees), append(r.graph.Trees, Tree{ID: group.ID, Title: group.Title})
	}

	tree := &r.graph.Trees[position]
	tree.Count++
	if isFirst || isShared && !r.walked[tree.ID] {
		tree.First, r.walked[tree.ID] = r.mods.ToReference(technology).ToID(), isShared
		tree.Painting = cmp.Or(group.Painting, r.ws.FindPainting(r.mods, technology))
	}
}

// describeTechnology returns the node of one technology of a tree: its era,
// its tree, and what it gives and unlocks as its line.
func (r *reach) describeTechnology(technology index.Site, group string) Node {
	node := makeNode(r.ws, r.mods, technology)
	node.Group, node.Era = group, findEra(r.ws, r.mods, technology)

	var parts []string
	for _, given := range listGives(r.ws, r.mods, technology) {
		parts = append(parts, strings.TrimSpace(given.Label+" "+given.Text))
	}

	counts, order := map[string]int{}, []string{}
	for _, link := range pickLinks(r.ws, r.mods, technology, index.RelationUnlock, true) {
		unlocked := makeNode(r.ws, r.mods, link.Target)
		node.Unlocks = append(node.Unlocks, unlocked)
		if counts[unlocked.Database]++; counts[unlocked.Database] == 1 {
			order = append(order, unlocked.Database)
		}
	}

	for position, database := range order {
		order[position] = describeNumber(float64(counts[database])) + " " + lang.DescribeDefinition(database, counts[database] > 1)
	}

	if len(order) > 0 {
		parts = append(parts, "Unlocks "+strings.Join(order, ", "))
	}

	node.Summary = strings.Join(parts, " · ")

	return node
}

// findRoot returns the id of the technology a rooted tree starts from: the
// one a technology's first requirements lead back to. It returns "" for a
// technology that requires none.
func findRoot(ws *workspace.Session, mods *index.Index, technology index.Site) string {
	root := technology
	for seen := map[index.Site]bool{}; !seen[root]; {
		seen[root] = true
		if required := pickLinks(ws, mods, root, index.RelationRequires, true); len(required) > 0 {
			root = required[0].Target
		}
	}

	if root == technology {
		return ""
	}

	return mods.ToReference(root).ToID()
}

// addRequired draws whatever a drawn technology requires that the audience
// left out, until none is missing. The members are every technology of the
// tree.
func (r *reach) addRequired(members []Node) {
	for position := 0; position < len(r.graph.Nodes); position++ {
		for _, link := range pickLinks(r.ws, r.mods, r.sites[r.graph.Nodes[position].ID], index.RelationRequires, true) {
			id := r.mods.ToReference(link.Target).ToID()
			member := slices.IndexFunc(members, func(node Node) bool { return node.ID == id })
			if _, drawn := r.positions[id]; !drawn && member >= 0 {
				r.put(members[member], link.Target)
			}
		}
	}
}

// linkRequired adds an edge to each drawn technology from each drawn one it
// requires, and counts into a tree's root how many advances its tree holds,
// the root among them.
func (r *reach) linkRequired() {
	for _, node := range r.graph.Nodes {
		for _, link := range pickLinks(r.ws, r.mods, r.sites[node.ID], index.RelationRequires, true) {
			required := r.mods.ToReference(link.Target).ToID()
			if _, drawn := r.positions[required]; drawn {
				r.graph.Edges = append(r.graph.Edges, Edge{From: required, To: node.ID, Relation: index.RelationRequires})
			}
		}

		if position, drawn := r.positions[node.Root]; drawn {
			root := &r.graph.Nodes[position]
			root.Count = max(root.Count, 1) + 1
		}
	}
}

// placeLoose puts each drawn advance that no requirement ties to another
// under a block: a node for its heading, with how many advances it holds. The
// blocks stand in the order of the game's limits, every country last.
func (r *reach) placeLoose() {
	tied := map[string]bool{}
	for _, edge := range r.graph.Edges {
		tied[edge.From], tied[edge.To] = true, true
	}

	counts, ranks := map[string]int{}, map[string]int{}
	var headings []string
	for position := range r.graph.Nodes {
		node := &r.graph.Nodes[position]
		if tied[node.ID] {
			continue
		}

		heading, rank := findBlock(r.ws, r.mods, r.sites[node.ID])
		if counts[heading]++; counts[heading] == 1 {
			headings, ranks[heading] = append(headings, heading), rank
		}

		node.Parent = blockPrefix + heading
	}

	slices.SortStableFunc(headings, func(first, second string) int { return ranks[first] - ranks[second] })
	for _, heading := range headings {
		r.put(Node{ID: blockPrefix + heading, Title: heading, Count: counts[heading]}, noSite)
	}
}

// findBlock returns the heading an advance stands under where no requirement
// ties it to a tree, and the heading's place among them: its specialization,
// then one government, then one country type, then every country.
func findBlock(ws *workspace.Session, mods *index.Index, advance index.Site) (string, int) {
	tree, statement, err := ws.FindStatement(mods, advance)
	if err != nil {
		return looseHeading, len(advanceLimits)
	}

	for position, row := range advanceLimits {
		if value, written := tree.FindChildValue(statement, row.Key); written {
			return cmp.Or(row.Heading, row.Words[value], toWords(value)), position
		}
	}

	return looseHeading, len(advanceLimits)
}

// countChain returns how many technologies the subject requires at any
// distance, or how many require it, and of those how many are not among the
// limited ones, which only some countries can have.
func (r *reach) countChain(subject index.Site, isRequired bool, limited map[index.Site]bool) (count, shared int) {
	seen, frontier := map[index.Site]bool{subject: true}, []index.Site{subject}
	for len(frontier) > 0 {
		current := frontier[0]
		frontier = frontier[1:]
		for _, link := range pickLinks(r.ws, r.mods, current, index.RelationRequires, isRequired) {
			next := link.Source
			if isRequired {
				next = link.Target
			}

			if seen[next] {
				continue
			}

			seen[next] = true
			frontier = append(frontier, next)
			count++
			if !limited[next] {
				shared++
			}
		}
	}

	return count, shared
}

// countUnlocks returns what a joined tree's technologies unlock, as a node
// for each database with how many technologies unlock one of it.
func countUnlocks(technologies []Node) []Node {
	var unlocks []Node
	for _, technology := range technologies {
		var counted []string
		for _, unlocked := range technology.Unlocks {
			position := slices.IndexFunc(unlocks, func(node Node) bool { return node.ID == unlocked.Database })
			if position < 0 {
				words := lang.DescribeDefinition(unlocked.Database, true)
				position = len(unlocks)
				unlocks = append(unlocks, Node{ID: unlocked.Database, Title: strings.ToUpper(words[:1]) + words[1:]})
			}

			if !slices.Contains(counted, unlocked.Database) {
				counted = append(counted, unlocked.Database)
				unlocks[position].Count++
			}
		}
	}

	return unlocks
}

// findAudience returns whom an EU5 advance is for, as the id of the narrowest
// country, culture or religion its potential names, itself or through the
// scripted triggers it calls, and what else the game limits it to, in words.
// The audience is "" for an advance every country can have, and the id of
// otherAudience for a potential that names nobody.
func findAudience(ws *workspace.Session, mods *index.Index, advance index.Site) (audience, limit string) {
	document, statement, ok := walkStatement(ws, mods, advance)
	if !ok {
		return "", ""
	}

	var limits []string
	for _, row := range advanceLimits {
		value, written := document.Tree.FindChildValue(statement, row.Key)
		title, _, _ := ws.LookupText(mods, value)
		if written {
			limits = append(limits, cmp.Or(row.Words[value], ws.FlattenText(mods, title), toWords(value)))
		}
	}

	potential, written := document.Tree.FindChild(statement, potentialKey)
	if !written {
		return "", strings.Join(limits, ", ")
	}

	named := listNamed(ws, mods, document, potential, 0)
	for _, group := range audienceGroups {
		for _, reference := range named {
			if slices.Contains(group.Databases, reference.Database) {
				return reference.ToID(), strings.Join(limits, ", ")
			}
		}
	}

	return otherAudience.ID, strings.Join(limits, ", ")
}

// maxTriggerDepth is how many scripted triggers a potential is followed
// through.
const maxTriggerDepth = 3

// listNamed returns the definitions a block of triggers names, itself or
// through the scripted triggers it calls. A name inside a negating block
// says whom the block is not for, and is left out.
func listNamed(ws *workspace.Session, mods *index.Index, document lang.Document, block int32, depth int) []lang.Reference {
	var named []lang.Reference
	tree := document.Tree
	for statement := block + 1; statement < tree.Statements[block].Last; statement++ {
		context := &document.Contexts[statement]
		isNegated := tree.Unquoted(tree.Statements[statement].Value) == lang.No
		for above := statement; above != block && !isNegated; above = tree.Statements[above].Parent {
			keyword := document.Contexts[above].Keyword
			isNegated = keyword != nil && keyword.Negates
		}

		for _, reference := range context.References {
			winner, kept := mods.FindKept(ws.Schema, reference)
			switch {
			case isNegated || !kept:
			case reference.Database != scriptedTriggers:
				named = append(named, reference)
			case depth < maxTriggerDepth:
				if called, trigger, ok := walkStatement(ws, mods, winner.Site); ok {
					named = append(named, listNamed(ws, mods, called, trigger, depth+1)...)
				}
			}
		}
	}

	return named
}

// listAudienceIDs returns the audiences whose advances a picked audience
// draws: itself and what it belongs to, as a country's culture and religion,
// a culture's language and groups, and a religion's group. It returns none
// for no pick.
func listAudienceIDs(ws *workspace.Session, mods *index.Index, picked string) []string {
	if picked == "" {
		return nil
	}

	ids := []string{picked}
	for position := 0; position < len(ids); position++ {
		definition, err := findSubject(ws, mods, ids[position])
		tree, statement, failure := ws.FindStatement(mods, definition)
		if err != nil || failure != nil {
			continue
		}

		for key, database := range potentialParents[mods.ToReference(definition).Database] {
			for child := range tree.Children(statement) {
				if tree.Text(tree.Statements[child].Key) != key {
					continue
				}

				values := []string{tree.Unquoted(tree.Statements[child].Value)}
				for held := range tree.Children(child) {
					values = append(values, tree.Unquoted(tree.Statements[held].Value))
				}

				for _, value := range values {
					if id := database + ":" + value; value != "" && !slices.Contains(ids, id) {
						ids = append(ids, id)
					}
				}
			}
		}
	}

	return ids
}

// listAudiences returns the rows of whom an age's advances are for: each
// group with how many advances are for one of it, then its names by title. A
// group with no advances has no row, and the advances every country can have
// have none.
func listAudiences(ws *workspace.Session, mods *index.Index, counts map[string]int) []Audience {
	var rows []Audience
	for _, group := range append(slices.Clone(audienceGroups), otherAudience) {
		var names []Audience
		total := counts[group.ID]
		for id, count := range counts {
			database, _, _ := strings.Cut(id, ":")
			definition, err := findSubject(ws, mods, id)
			if !slices.Contains(group.Databases, database) || err != nil {
				continue
			}

			total += count
			names = append(names, Audience{ID: id, Group: group.ID, Title: ws.LookupTitle(mods, definition), Count: count})
		}

		if total == 0 {
			continue
		}

		slices.SortFunc(names, func(first, second Audience) int { return strings.Compare(first.Title, second.Title) })
		rows = append(append(rows, Audience{ID: group.ID, Group: group.ID, Title: group.Title, Count: total}), names...)
	}

	return rows
}
