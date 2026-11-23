// tree.go answers for a technology: the technologies of its tree, what each
// requires, gives and unlocks, the game's trees, and for an EU5 age whom its
// advances are for.

package graph

import (
	"cmp"
	"fmt"
	"slices"

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

	// Technologies are the tree's technologies, or every tree's in a game of
	// joinedTrees, and Requires the links from each to what it requires.
	Technologies []Technology `json:"technologies"`
	Requires     []Edge       `json:"requires"`

	// Unlocks are the databases of what the technologies unlock, in the order
	// found: each a node whose Title is the game's word for its definitions
	// and whose Count is how many of the technologies unlock one.
	Unlocks []Node `json:"unlocks"`

	// Needs are the ids of what the subject requires, at any distance, and
	// Leads the ids of what requires it.
	Needs []string `json:"needs"`
	Leads []string `json:"leads"`

	// Audiences are the rows of whom the tree's advances are for, none for a
	// game whose technologies are for everyone.
	Audiences []Audience `json:"audiences"`
}

// Tree is one of a game's technology trees.
type Tree struct {
	// ID is what its technologies share: a CK3 group, a Vic3 category or the
	// id of an EU5 age. Title is its name in the game's words.
	ID    string `json:"id"`
	Title string `json:"title"`

	// Painting is the path of the painting behind its head, "" if it has
	// none.
	Painting string `json:"painting,omitempty"`

	// Count is how many technologies it holds. First is the id of the one
	// that opens it: the first the game's files define that every country
	// can have, else the first.
	Count int    `json:"count"`
	First string `json:"first"`
}

// Technology is one technology of a tree.
type Technology struct {
	Node Node `json:"node"`

	// Gives is what its modifiers give, and Unlocks what it unlocks.
	Gives   []Fact `json:"gives"`
	Unlocks []Node `json:"unlocks"`

	// Root is the id of the technology its own tree inside an age starts
	// from. It is "" for a technology that stands alone, and for a game that
	// draws a tree whole.
	Root string `json:"root,omitempty"`

	// Audience is the row of Audiences it is drawn for, "" for one every
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

// BuildTree returns the tree a technology is in. For an EU5 age it draws the
// advances of every country, those of the filter's Potential, and those of
// the subject's own row. It returns an error if no workspace is open, the
// game keeps no definition of the id, or it is no technology.
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
		for _, listed := range trees {
			technologies = append(technologies, members[listed.ID]...)
		}
	}

	var drawn map[string]string

	graph.Audiences, drawn = draft.listDrawn(subject, technologies, filter.Potential)
	graph.Technologies, graph.Requires = draft.makeTechnologies(technologies, drawn)
	graph.Unlocks = listUnlocked(graph.Technologies)
	graph.Needs = draft.listChain(subject, func(id string) []Edge { return draft.model.listFrom(id, saysRequires) })
	graph.Leads = draft.listChain(subject, func(id string) []Edge { return draft.model.listTo(id, saysRequires) })
	for _, era := range draft.listEras() {
		isDrawn := slices.ContainsFunc(graph.Technologies, func(drawn Technology) bool { return drawn.Node.Era == era })
		if node, isDefined := draft.build(era); isDrawn && isDefined {
			graph.Eras = append(graph.Eras, node)
		}
	}

	return graph, nil
}

// listUnlocked returns the databases of what the given technologies unlock,
// in the order found, each counted once for a technology however many of its
// definitions that technology unlocks.
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

// listTrees returns the game's technology trees in the order its files reach
// them, ages in the order of the eras, and the ids of each tree's
// technologies.
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

// findOwnPainting returns the path of the painting of the definition with an
// id, "" if it has none or the game keeps no such definition.
func (d *draft) findOwnPainting(id string) string {
	site, err := resolveNode(d.ws, d.mods, id)
	if err != nil {
		return ""
	}

	return findPainting(d.ws, d.mods, site)
}

// listDrawn returns the rows of whom a tree's advances are for, and the
// technologies drawn, each with the row it is drawn for: "" for one every
// country can have and for what a drawn one requires. A game with no rows
// draws the whole tree.
func (d *draft) listDrawn(subject string, tree []string, potential string) ([]Audience, map[string]string) {
	drawn := map[string]string{}
	rows, members := d.listAudiences(tree)
	if len(rows) == 0 {
		for _, id := range tree {
			drawn[id] = ""
		}

		return rows, drawn
	}

	own, named := d.findAudience(subject)
	if len(named) > 0 {
		own = named[0]
	}

	for _, row := range []string{sharedAdvances, potential, own} {
		for _, id := range members[row] {
			if _, isDrawn := drawn[id]; !isDrawn && row != "" {
				drawn[id] = row
			}
		}
	}

	for id, row := range drawn {
		if row == sharedAdvances {
			drawn[id] = ""
		}

		for _, required := range d.listChain(id, func(id string) []Edge { return d.model.listFrom(id, saysRequires) }) {
			if _, isDrawn := drawn[required]; !isDrawn {
				drawn[required] = ""
			}
		}
	}

	return rows, drawn
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

// makeTechnologies returns the drawn technologies of a tree in the order the
// game's files define them, and the links from each to what it requires
// among them.
func (d *draft) makeTechnologies(tree []string, drawn map[string]string) ([]Technology, []Edge) {
	technologies, requires := []Technology{}, []Edge{}
	isRooted := slices.Contains(rootedTrees, d.ws.Game.ID)
	for _, id := range tree {
		row, isDrawn := drawn[id]
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
			if _, isDrawn := drawn[edge.To]; isDrawn {
				requires = append(requires, edge)
			}
		}
	}

	return technologies, requires
}

// makeTechnology returns a technology with its era, what its modifiers give
// and what it unlocks.
func (d *draft) makeTechnology(id string) Technology {
	technology := Technology{Node: d.lookupNode(id), Gives: d.listGives(id), Unlocks: []Node{}}
	for _, edge := range d.model.listFrom(id, saysUnlock) {
		technology.Unlocks = append(technology.Unlocks, d.lookupNode(edge.To))
	}

	return technology
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
