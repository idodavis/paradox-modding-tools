// tree.go answers for a technology: its tree, the game's trees, and for an
// EU5 age whom its advances are for.

package graph

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/lang"
)

// TreeGraph is the technologies of one tree, around the one asked about.
type TreeGraph struct {
	// Subject is the id of the technology asked about, and Tree the id of the
	// tree it is in.
	Subject string `json:"subject"`
	Tree    string `json:"tree"`

	// Trees are the game's trees, and Eras the eras of the technologies
	// drawn, each in the order the game's files define them.
	Trees []Tree `json:"trees"`
	Eras  []Node `json:"eras"`

	// Technologies are the tree's technologies, or every tree's where the
	// game draws its trees together. Requires links each to what it requires.
	Technologies []Technology `json:"technologies"`
	Requires     []Edge       `json:"requires"`

	// Unlocks are the databases of what the technologies unlock: each a node
	// titled with the game's word for its definitions, its Count how many
	// technologies unlock one.
	Unlocks []Node `json:"unlocks"`

	// Needs are the ids of what the subject requires, at any distance, and
	// Leads the ids of what requires it. SharedLeads is how many of those
	// every country can have.
	Needs       []string `json:"needs"`
	Leads       []string `json:"leads"`
	SharedLeads int      `json:"sharedLeads"`

	// Audiences are the rows of whom the tree's advances are for, beside
	// every country, none for a game whose technologies are for everyone.
	Audiences []Audience `json:"audiences"`
}

// Tree is one of a game's technology trees.
type Tree struct {
	// ID is what its technologies share: a CK3 group, a Vic3 category or the
	// id of an EU5 age. Title is its name in the game's words.
	ID    string `json:"id"`
	Title string `json:"title"`

	// Painting is the path of the painting behind its head, "" for none.
	Painting string `json:"painting,omitempty"`

	// Count is how many technologies it holds. First is the id of the one
	// that opens it: the first every country can have, else the first.
	Count int    `json:"count"`
	First string `json:"first"`
}

// Technology is one technology of a tree.
type Technology struct {
	Node Node `json:"node"`

	// Gives is what it gives, and Unlocks what it unlocks.
	Gives   []Fact `json:"gives"`
	Unlocks []Node `json:"unlocks"`

	// Summary is the one line its card shows: what it gives, then what it
	// unlocks counted by what each is, as "Control Growth +10% · Unlocks 1
	// law, 3 buildings". It is "" for one that gives and unlocks nothing.
	Summary string `json:"summary"`

	// Root is the id of the technology its tree inside an age starts from,
	// "" for one that stands alone and for a game that draws a tree whole.
	Root string `json:"root,omitempty"`

	// Audience is the id of the audience it is drawn for, "" for one every
	// country can have.
	Audience string `json:"audience,omitempty"`
}

// rootedTrees holds the games that draw a tree as several, each from a
// technology that requires none. EU5's in_game/common/advances/
// 0_age_of_discovery.txt:12 writes depth = 0 on each such advance of an age.
var rootedTrees = []string{game.EU5}

// joinedTrees holds the games whose trees are drawn together, as one grid of
// era by group. CK3's innovations require nothing of each other: none of
// common/culture/innovations writes a prerequisite.
var joinedTrees = []string{game.CK3}

// BuildTree returns the tree a technology is in. An EU5 age draws the
// advances of every country, of the filter's potential and of the subject's
// own audience. It returns an error if no workspace is open, the game keeps
// no definition of the id, or it is no technology.
func (s *Service) BuildTree(id string, filter Filter) (TreeGraph, error) {
	ws, err := s.Workspace.Session()
	if err != nil {
		return TreeGraph{}, err
	}

	mods := ws.Mods.Load()
	site, err := resolveNode(ws, mods, id)
	if err != nil {
		return TreeGraph{}, err
	}

	draft := newDraft(ws, mods)
	subject := toID(mods, site)
	if draft.typeOf(subject) != lang.TypeTechnology {
		return TreeGraph{}, fmt.Errorf("%s is no technology", id)
	}

	trees, members := draft.listTrees()
	graph := TreeGraph{Subject: subject, Tree: draft.findGroupID(subject), Trees: trees, Eras: []Node{}}

	technologies := members[graph.Tree]
	if slices.Contains(joinedTrees, ws.Game.ID) {
		technologies = nil
		for _, tree := range trees {
			technologies = append(technologies, members[tree.ID]...)
		}
	}

	var audiences map[string]string

	graph.Audiences, audiences = draft.listDrawn(subject, technologies, filter.Potential)
	graph.Technologies, graph.Requires = draft.makeTechnologies(technologies, audiences)
	graph.Unlocks = listUnlocked(graph.Technologies)
	graph.Needs = draft.listChain(subject, func(id string) []Edge { return draft.model.listFrom(id, saysRequires) })
	graph.Leads = draft.listChain(subject, func(id string) []Edge { return draft.model.listTo(id, saysRequires) })
	graph.SharedLeads = draft.countShared(graph.Leads)
	for _, era := range draft.listEras() {
		isDrawn := slices.ContainsFunc(graph.Technologies, func(technology Technology) bool { return technology.Node.Era == era })
		if node, isDefined := draft.build(era); isDrawn && isDefined {
			graph.Eras = append(graph.Eras, node)
		}
	}

	return graph, nil
}

// listUnlocked returns the databases of what the given technologies unlock,
// each counted once for a technology.
func listUnlocked(technologies []Technology) []Node {
	unlocked := []Node{}
	for _, technology := range technologies {
		var databases []string
		for _, definition := range technology.Unlocks {
			databases = appendNew(databases, definition.Database)
		}

		for _, database := range databases {
			position := slices.IndexFunc(unlocked, func(node Node) bool { return node.ID == database })
			if position < 0 {
				position = len(unlocked)
				unlocked = append(unlocked, Node{ID: database, Database: database, Title: lang.DescribeDefinition(database, true)})
			}

			unlocked[position].Count++
		}
	}

	return unlocked
}

// listTrees returns the game's trees in the order its files reach them, ages
// in the order of the eras, and the ids of each tree's technologies.
func (d *draft) listTrees() ([]Tree, map[string][]string) {
	var ids []string

	members := map[string][]string{}
	for _, id := range d.listDefinitions(lang.TypeTechnology) {
		if tree := d.findGroupID(id); tree != "" {
			ids = appendNew(ids, tree)
			members[tree] = append(members[tree], id)
		}
	}

	eras := d.listEras()
	slices.SortStableFunc(ids, func(first, second string) int {
		return cmp.Compare(slices.Index(eras, first), slices.Index(eras, second))
	})

	trees := make([]Tree, len(ids))
	for position, id := range ids {
		group, _ := d.makeGroup(id, len(members[id]))
		first := d.findFirst(members[id])
		trees[position] = Tree{
			ID: id, Title: labelNode(group), Count: group.Count, First: first,
			Painting: cmp.Or(group.Painting, d.findOwnPainting(first)),
		}
	}

	return trees, members
}

// findFirst returns the first of a tree's technologies that every country can
// have, else its first.
func (d *draft) findFirst(tree []string) string {
	position := slices.IndexFunc(tree, func(id string) bool {
		group, _ := d.findAudience(id)

		return group == sharedAdvances
	})

	return tree[max(position, 0)]
}

// findOwnPainting returns the path of a definition's painting, "" for none.
func (d *draft) findOwnPainting(id string) string {
	site, err := resolveNode(d.ws, d.mods, id)
	if err != nil {
		return ""
	}

	return findPainting(d.ws, d.mods, site)
}

// listDrawn returns a tree's audiences and the technologies drawn, each with
// the audience it is drawn for: "" for one every country can have and for
// what a drawn one requires. A game with no audiences draws the whole tree.
func (d *draft) listDrawn(subject string, tree []string, potential string) ([]Audience, map[string]string) {
	audiences := map[string]string{}
	rows, members := d.listAudiences(tree)
	if len(rows) == 0 {
		for _, id := range tree {
			audiences[id] = ""
		}

		return rows, audiences
	}

	own, names := d.findAudience(subject)
	if len(names) > 0 {
		own = names[0]
	}

	for _, row := range []string{sharedAdvances, potential, own} {
		for _, id := range members[row] {
			if _, isDrawn := audiences[id]; !isDrawn && row != "" {
				audiences[id] = row
			}
		}
	}

	for id, row := range audiences {
		if row == sharedAdvances {
			audiences[id] = ""
		}

		for _, required := range d.listChain(id, func(id string) []Edge { return d.model.listFrom(id, saysRequires) }) {
			if _, isDrawn := audiences[required]; !isDrawn {
				audiences[required] = ""
			}
		}
	}

	return rows, audiences
}

// listChain returns the ids the given links lead to from a definition, at any
// distance, each once and never the definition itself.
func (d *draft) listChain(id string, list func(string) []Edge) []string {
	chain := []string{}
	reached := []string{id}
	for position := 0; position < len(reached); position++ {
		for _, edge := range list(reached[position]) {
			far := edge.To
			if far == reached[position] {
				far = edge.From
			}

			if !slices.Contains(reached, far) {
				reached = append(reached, far)
				chain = append(chain, far)
			}
		}
	}

	return chain
}

// makeTechnologies returns the drawn technologies in the order the game's
// files define them, and the links from each to what it requires among them.
func (d *draft) makeTechnologies(tree []string, audiences map[string]string) ([]Technology, []Edge) {
	technologies, requires := []Technology{}, []Edge{}
	isRooted := slices.Contains(rootedTrees, d.ws.Game.ID)
	for _, id := range tree {
		row, isDrawn := audiences[id]
		if !isDrawn {
			continue
		}

		technology := d.makeTechnology(id)
		technology.Audience = row
		if isRooted {
			technology.Root = d.findRoot(id)
		}

		technologies = append(technologies, technology)
		for _, edge := range d.model.listFrom(id, saysRequires) {
			if _, isDrawn := audiences[edge.To]; isDrawn {
				requires = append(requires, edge)
			}
		}
	}

	return technologies, requires
}

// makeTechnology returns a technology with its era, what it gives and what
// it unlocks, and the line its card shows.
func (d *draft) makeTechnology(id string) Technology {
	technology := Technology{Node: d.lookupNode(id), Gives: d.listGives(id), Unlocks: []Node{}}
	for _, edge := range d.model.listFrom(id, saysUnlock) {
		technology.Unlocks = append(technology.Unlocks, d.lookupNode(edge.To))
	}

	technology.Summary = summarize(technology)

	return technology
}

// summarize returns the line a technology's card shows: each thing it gives,
// then how many of each database it unlocks, in the game's word for them.
func summarize(technology Technology) string {
	parts := make([]string, 0, len(technology.Gives)+1)
	for _, fact := range technology.Gives {
		parts = append(parts, strings.TrimSpace(fact.Label+" "+fact.Text))
	}

	var databases []string

	counts := map[string]int{}
	for _, unlocked := range dropRepeats(technology.Unlocks, func(node Node) string { return node.ID }) {
		databases = appendNew(databases, unlocked.Database)
		counts[unlocked.Database]++
	}

	unlocks := make([]string, len(databases))
	for position, database := range databases {
		unlocks[position] = strconv.Itoa(counts[database]) + " " + lang.DescribeDefinition(database, counts[database] != 1)
	}

	if len(unlocks) > 0 {
		parts = append(parts, "Unlocks "+strings.Join(unlocks, ", "))
	}

	return strings.Join(parts, " · ")
}

// findRoot returns the id of the technology a technology's own tree starts
// from: the one its first requirements lead back to. It returns "" for a
// technology that requires none and that none requires.
func (d *draft) findRoot(id string) string {
	if len(d.model.listFrom(id, saysRequires)) == 0 && len(d.model.listTo(id, saysRequires)) == 0 {
		return ""
	}

	visited := []string{}
	for root := id; !slices.Contains(visited, root); {
		visited = append(visited, root)
		required := d.model.listFrom(root, saysRequires)
		if len(required) == 0 {
			return root
		}

		root = required[0].To
	}

	return id
}
