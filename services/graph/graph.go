// Package graph is the Event Graph page's service. It builds the graph of what fires what from one root, and the
// detail of one node.
package graph

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/samber/lo"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/schema"
	"paradox-modding-tools/services/workspace"
)

// Service is the Wails service behind the Event Graph. It finds the roots a search names and builds a graph from one.
type Service struct {
	Workspace *workspace.Service
}

// A Node is one node of the event graph: the kept definition of an event, an on_action, a decision, a scripted
// effect or any other entry that fires one, with where it is written, or a fold that stands for the firings of one
// node past the fan-out.
type Node struct {
	ID       string          `json:"id"` // database:name; more:<id> or more-in:<id> for a fold
	Database string          `json:"database"`
	Name     string          `json:"name"`
	What     string          `json:"what"`  // what a definition of its database is called, such as "scripted effect"
	Title    string          `json:"title"` // its localized title; "" if it has none
	Site     index.Site      `json:"site"`
	Place    workspace.Place `json:"place"`
	Line     int             `json:"line"`   // counted from one
	Step     int             `json:"step"`   // its distance from the root, negative for a firer
	Parent   string          `json:"parent"` // the drawn node one step nearer the root; "" for the root
	Count    int             `json:"count"`  // the firings a fold stands for; 0 for a definition
	Deep     bool            `json:"deep"`   // the depth stopped at it, and it links further to a next step
}

// An Edge is one firing the graph draws, from a firer to what it fires.
type Edge struct {
	From  string          `json:"from"`
	To    string          `json:"to"`
	Days  string          `json:"days,omitempty"` // the delay as written: days, months or years
	Via   []string        `json:"via,omitempty"`  // the scripted effects passed through, when they are not drawn as nodes
	Place workspace.Place `json:"place"`          // where the firing is written
	Line  int             `json:"line"`           // counted from one
}

// A Filter says what the graph shows and how far it reaches from its root.
type Filter struct {
	Databases []string          `json:"databases"`
	Origins   []string          `json:"origins"`
	Depth     int               `json:"depth"`
	Limit     int               `json:"limit"`
	Fanout    int               `json:"fanout"` // the firings of one node drawn before the rest fold; 0 folds none
	Expand    []string          `json:"expand"` // the folds opened, and the nodes the depth stopped at that are opened
	Follow    bool              `json:"follow"`
	Labels    map[string]string `json:"labels,omitempty"` // what each database's definitions are called
}

// A Graph is the event graph from one root. It has no coordinates.
type Graph struct {
	Root      string `json:"root"`
	Nodes     []Node `json:"nodes"`
	Edges     []Edge `json:"edges"`
	Truncated bool   `json:"truncated"` // the limit cut it
	Total     int    `json:"total"`     // how many nodes the reach found before the limit cut it
}

// A reach holds one graph while it is built from its root.
type reach struct {
	ws     *workspace.Session
	mods   *index.Index
	filter Filter
	nodes  []Node
	sites  map[index.Site]bool // the sites of nodes
	edges  []Edge
	count  int                 // how many of the nodes the filter draws
	cut    map[index.Site]bool // the nodes the limit kept out
}

// delayKeys are the keys that write a delay.
var delayKeys = []string{"days", "months", "years"}

// delayBlock is the key of a block that holds a delay for every firing listed after it.
const delayBlock = "delay"

// BuildGraph builds the graph that reaches out from a root node under a filter. It follows what the root fires and what
// fires it, up to the filter's depth and node limit. A node the filter hides is replaced by one edge, with the node's
// name added to Via. It returns an error if the game keeps no definition of the root.
func (gph *Service) BuildGraph(root string, filter Filter) (Graph, error) {
	ws, err := gph.Workspace.Session()
	if err != nil {
		return Graph{}, err
	}
	mods := ws.Mods.Load()
	site, err := resolveNode(ws, mods, root)
	if err != nil {
		return Graph{}, err
	}
	reach := &reach{ws: ws, mods: mods, filter: filter, sites: map[index.Site]bool{site: true}, count: 1, cut: map[index.Site]bool{}}
	reach.nodes = []Node{makeNode(ws, mods, site, 0)}
	reach.expand(site, true)
	reach.expand(site, false)
	reach.splice()
	return Graph{
		Root: root, Nodes: reach.nodes, Edges: lo.UniqBy(reach.edges, toKey),
		Truncated: len(reach.cut) > 0, Total: reach.count + len(reach.cut),
	}, nil
}

// resolveNode returns the definition the game keeps of the node with the given id, `database:name`. It returns an error
// if the game keeps none.
func resolveNode(ws *workspace.Session, mods *index.Index, id string) (index.Site, error) {
	database, name, _ := strings.Cut(id, ":")
	if winner, ok := mods.FindKept(ws.Schema, schema.Reference{Database: database, Name: name}); ok {
		return winner.Site, nil
	}
	return index.Site{}, fmt.Errorf("the game keeps no definition %q", id)
}

// makeNode returns the node of a definition, at the given step from the root.
func makeNode(ws *workspace.Session, mods *index.Index, site index.Site, step int) Node {
	reference := mods.ToReference(site)
	return Node{
		ID:       toID(mods, site),
		Database: reference.Database,
		Name:     reference.Name,
		What:     game.DescribeDefinition(reference.Database, false),
		Title:    ws.LookupTitle(mods, site),
		Site:     site,
		Place:    ws.Locate(mods, mods.File(site).Path),
		Line:     toLine(mods, site),
		Step:     step,
	}
}

// toLine returns the line of the occurrence at a site, counted from one.
func toLine(mods *index.Index, site index.Site) int {
	return int(mods.Occurrence(site).Line) + 1
}

// toID returns the id of the node of a definition: its database and its name.
func toID(mods *index.Index, site index.Site) string { return mods.ToReference(site).ToID() }

// expand adds the nodes further from the root: what each node fires going forward, what fires each node going
// backward. It stops at the filter's depth, but goes on from a node there that the filter opens: the node itself, or
// for a node the filter hides, the nearest drawn node before it. A node it stops at that links further is marked deep.
func (reach *reach) expand(root index.Site, forward bool) {
	frontier := []index.Site{root}
	for distance := 1; len(frontier) > 0; distance++ {
		if distance > reach.filter.Depth {
			parents := toParents(reach.listHidden())
			var rest []index.Site
			frontier, rest = lo.FilterReject(frontier, func(site index.Site, _ int) bool {
				return slices.Contains(reach.filter.Expand, findDrawn(parents, toID(reach.mods, site)))
			})
			reach.markDeep(rest, forward)
		}
		step := lo.Ternary(forward, distance, -distance)
		var next []index.Site
		for _, site := range frontier {
			for _, fire := range reach.fold(site, reach.listLinks(site, forward), forward, step) {
				if end, isNew := reach.addFire(fire, forward, step); isNew {
					next = append(next, end)
				}
			}
		}
		frontier = next
	}
}

// listHidden returns the nodes the filter hides. The root and the folds are never hidden.
func (reach *reach) listHidden() []Node {
	return lo.Filter(reach.nodes, func(node Node, _ int) bool {
		return node.Step != 0 && node.Count == 0 && reach.isHidden(node.Site)
	})
}

// toParents returns the parent of each of the given nodes, by the node's id.
func toParents(nodes []Node) map[string]string {
	return lo.SliceToMap(nodes, func(node Node) (string, string) { return node.ID, node.Parent })
}

// findDrawn returns the id of the nearest drawn node at or before the node with the given id. It goes up through the
// hidden nodes, whose parents are given by id.
func findDrawn(parents map[string]string, id string) string {
	for next, ok := parents[id]; ok; next, ok = parents[id] {
		id = next
	}
	return id
}

// markDeep marks as deep each of the given nodes that links further: one that fires more going forward, or that more
// fires going backward.
func (reach *reach) markDeep(sites []index.Site, forward bool) {
	for _, site := range sites {
		position := slices.IndexFunc(reach.nodes, func(node Node) bool { return node.Count == 0 && node.Site == site })
		if position >= 0 && len(reach.listLinks(site, forward)) > 0 {
			reach.nodes[position].Deep = true
		}
	}
}

// listLinks returns what a node fires going forward, or what fires it going backward.
func (reach *reach) listLinks(site index.Site, forward bool) []index.Fire {
	if forward {
		return listFires(reach.ws, reach.mods, site, reach.filter.Follow)
	}
	return reach.mods.ListFiredBy(reach.ws.Schema, site, reach.filter.Follow)
}

// fold returns a node's firings up to the filter's fan-out and draws the rest as one fold joined to the node, at the
// given step from the root. A node the filter opens, or with no more firings than the fan-out, keeps them all.
func (reach *reach) fold(site index.Site, fires []index.Fire, forward bool, step int) []index.Fire {
	parent := toID(reach.mods, site)
	id := lo.Ternary(forward, "more:", "more-in:") + parent
	if reach.filter.Fanout <= 0 || len(fires) <= reach.filter.Fanout || slices.Contains(reach.filter.Expand, id) {
		return fires
	}
	rest := fires[reach.filter.Fanout:]
	databases := lo.Uniq(lo.Map(rest, func(fire index.Fire, _ int) string {
		return reach.mods.ToReference(lo.Ternary(forward, fire.Target, fire.Source)).Database
	}))
	node := Node{ID: id, Step: step, Parent: parent, Count: len(rest)}
	if len(databases) == 1 {
		node.What = game.DescribeDefinition(databases[0], len(rest) > 1)
	}
	reach.nodes = append(reach.nodes, node)
	reach.edges = append(reach.edges, lo.Ternary(forward, Edge{From: parent, To: id}, Edge{From: id, To: parent}))
	return fires[:reach.filter.Fanout]
}

// listFires returns what a node fires, including what the scripted effects it calls fire if the flag is set.
func listFires(ws *workspace.Session, mods *index.Index, site index.Site, follow bool) []index.Fire {
	return lo.FlatMap(listMerged(ws, mods, site), func(part index.Site, _ int) []index.Fire {
		return mods.ListFires(ws.Schema, part, follow)
	})
}

// listMerged returns the definitions that make up a node: all that the game merges under its name, else the given one.
func listMerged(ws *workspace.Session, mods *index.Index, site index.Site) []index.Site {
	if winner, ok := mods.FindKeptLocal(ws.Schema, mods.ToReference(site), site.File); ok && len(winner.Merged) > 0 {
		return winner.Merged
	}
	return []index.Site{site}
}

// addFire puts a firing in the graph, with its nodes, at the given step from the root. It returns the node of the
// firing that is farther from the root, and whether the graph is new to it. Nothing past the node limit is added.
func (reach *reach) addFire(fire index.Fire, forward bool, step int) (index.Site, bool) {
	path := reach.listPath(fire)
	if !forward {
		slices.Reverse(path)
	}
	end := path[len(path)-1]
	isNew := !reach.sites[end]
	whole := makeEdge(reach.ws, reach.mods, fire)
	for position := 1; position < len(path); position++ {
		if !reach.add(path[position], step, path[position-1]) {
			return end, false
		}
		from, to := path[position-1], path[position]
		if !forward {
			from, to = to, from
		}
		edge := Edge{From: toID(reach.mods, from), To: toID(reach.mods, to), Place: whole.Place, Line: whole.Line}
		if to == fire.Target {
			edge.Days = whole.Days
		}
		if len(path) == 2 {
			edge.Via = whole.Via
		}
		reach.edges = append(reach.edges, edge)
	}
	return end, isNew
}

// listPath returns the definitions a firing goes through, from the firer to what it fires. The scripted effects it
// passes are included if the filter draws them.
func (reach *reach) listPath(fire index.Fire) []index.Site {
	if !slices.Contains(reach.filter.Databases, reach.ws.Schema.Effects) {
		return []index.Site{fire.Source, fire.Target}
	}
	return slices.Concat([]index.Site{fire.Source}, fire.Via, []index.Site{fire.Target})
}

// listVia returns the names of the scripted effects a firing passes through, in order. It is empty if it passes
// through none.
func listVia(mods *index.Index, fire index.Fire) []string {
	return lo.Map(fire.Via, func(site index.Site, _ int) string { return mods.ToReference(site).Name })
}

// add puts the node of a definition in the graph, at the given step from the root, reached from the given parent. It
// returns false if the node limit keeps it out.
func (reach *reach) add(site index.Site, step int, parent index.Site) bool {
	if reach.sites[site] {
		return true
	}
	if !reach.isHidden(site) {
		if reach.count >= reach.filter.Limit {
			reach.cut[site] = true
			return false
		}
		reach.count++
	}
	reach.sites[site] = true
	node := makeNode(reach.ws, reach.mods, site, step)
	node.Parent = toID(reach.mods, parent)
	reach.nodes = append(reach.nodes, node)
	return true
}

// isHidden reports whether the filter hides a definition's node: its origin is not in the filter, or its database is
// one of the kinds the filter picks among and is not in it. A firer of any other database is drawn whatever the
// filter's databases.
func (reach *reach) isHidden(site index.Site) bool {
	database := reach.mods.ToReference(site).Database
	picked := slices.Contains(reach.filter.Databases, database) || !slices.Contains(listKinds(reach.ws.Schema), database)
	return !picked || !slices.Contains(reach.filter.Origins, reach.mods.File(site).Origin)
}

// listKinds returns the databases whose nodes the filter picks among: the events, on_actions and decisions, and the
// scripted effects.
func listKinds(schema *schema.Schema) []string {
	kinds := slices.Clone(schema.FireDatabases)
	if schema.Effects != "" {
		kinds = append(kinds, schema.Effects)
	}
	return kinds
}

// findDelay returns the delay written for a firing, such as "days = 5". It returns "" if there is none.
func findDelay(ws *workspace.Session, mods *index.Index, use index.Site) string {
	tree, err := ws.LoadTree(mods.File(use).Path)
	if err != nil {
		return ""
	}
	// the delay is in the first block, going up from the reference that fires, that holds one directly or in a delay
	// block
	for statement := tree.FindStatement(mods.Occurrence(use).Span.Start); statement > 0; statement = tree.Statements[statement].Parent {
		holders := []int32{statement}
		if block, ok := tree.FindChild(statement, delayBlock); ok {
			holders = append(holders, block)
		}
		for _, holder := range holders {
			if delay := findDuration(tree, holder); delay != "" {
				return delay
			}
		}
	}
	return ""
}

// findDuration returns the days, months or years statement a block holds. It returns "" if there is none.
func findDuration(tree *jomini.Tree, block int32) string {
	for _, key := range delayKeys {
		if child, ok := tree.FindChild(block, key); ok {
			return tree.Text(tree.Statements[child].Span)
		}
	}
	return ""
}

// splice removes the nodes the filter hides from the graph. The edges into and out of a node become one edge that
// passes through it, and a node whose parent is removed takes the nearest drawn one. The root and the folds are never
// removed.
func (reach *reach) splice() {
	hidden := reach.listHidden()
	for _, node := range hidden {
		var rest, incoming, outgoing []Edge
		for _, edge := range reach.edges {
			switch {
			case edge.From == node.ID && edge.To == node.ID:
			case edge.To == node.ID:
				incoming = append(incoming, edge)
			case edge.From == node.ID:
				outgoing = append(outgoing, edge)
			default:
				rest = append(rest, edge)
			}
		}
		for _, first := range incoming {
			for _, second := range outgoing {
				via := slices.Concat(first.Via, []string{node.Name}, second.Via)
				rest = append(rest, Edge{From: first.From, To: second.To, Days: first.Days, Via: via, Place: first.Place, Line: first.Line})
			}
		}
		reach.edges = rest
	}
	reach.nodes = lo.Without(reach.nodes, hidden...)
	parents := toParents(hidden)
	reach.passDeep(hidden, parents)
	for position := range reach.nodes {
		reach.nodes[position].Parent = findDrawn(parents, reach.nodes[position].Parent)
	}
}

// passDeep gives the deep mark of each of the given hidden nodes to the nearest drawn node before it, so that opening
// that node goes on past the hidden one. The parents are the hidden nodes', by id.
func (reach *reach) passDeep(hidden []Node, parents map[string]string) {
	for _, node := range hidden {
		if !node.Deep {
			continue
		}
		drawn := findDrawn(parents, node.Parent)
		if position := slices.IndexFunc(reach.nodes, func(other Node) bool { return other.ID == drawn }); position >= 0 {
			reach.nodes[position].Deep = true
		}
	}
}

// toKey returns a text that is the same for two edges exactly when they go between the same nodes with the same delay
// and through the same scripted effects.
func toKey(edge Edge) string {
	return strings.Join(slices.Concat([]string{edge.From, edge.To, edge.Days}, edge.Via), "\x00")
}

// FindRoots returns up to the given limit of the nodes whose name holds the query, ignoring case. The name equal to
// the query comes first, then the names that start with it, then the rest, each in name order. It returns nil if no
// workspace is open.
func (gph *Service) FindRoots(query string, limit int) []Node {
	ws, err := gph.Workspace.Session()
	if err != nil {
		return nil
	}
	mods := ws.Mods.Load()
	query = strings.ToLower(query)
	type match struct {
		site index.Site
		name string
		rank int
	}
	var matches []match
	for _, database := range ws.Schema.FireDatabases {
		for _, site := range mods.ListDefinitions(database) {
			name := strings.ToLower(mods.ToReference(site).Name)
			if strings.Contains(name, query) && isKept(ws, mods, site) {
				matches = append(matches, match{site, name, rankRoot(name, query)})
			}
		}
	}
	slices.SortFunc(matches, func(first, second match) int {
		return cmp.Or(cmp.Compare(first.rank, second.rank), strings.Compare(first.name, second.name))
	})
	return lo.Map(matches[:min(limit, len(matches))], func(one match, _ int) Node {
		return makeNode(ws, mods, one.site, 0)
	})
}

// rankRoot returns where a name that holds a query sorts among a search's roots: 0 for the query itself, 1 for a name
// that starts with it, 2 for any other.
func rankRoot(name, query string) int {
	switch {
	case name == query:
		return 0
	case strings.HasPrefix(name, query):
		return 1
	default:
		return 2
	}
}

// isKept reports whether a definition is the one the game keeps of its name.
func isKept(ws *workspace.Session, mods *index.Index, site index.Site) bool {
	winner, ok := mods.FindKeptLocal(ws.Schema, mods.ToReference(site), site.File)
	return ok && winner.Site == site
}

// Defaults returns the filter for a new graph. It draws every kind the filter picks among, from the game and every
// mod, six firings of a node before the rest are folded, with every fold closed. It returns an empty filter if no
// workspace is open.
func (gph *Service) Defaults() Filter {
	ws, err := gph.Workspace.Session()
	if err != nil {
		return Filter{}
	}
	databases := listKinds(ws.Schema)
	origins := []string{game.OriginVanilla}
	for _, mod := range ws.Order {
		origins = append(origins, mod.ID)
	}
	labels := lo.SliceToMap(databases, func(database string) (string, string) {
		return database, game.DescribeDefinition(database, true)
	})
	return Filter{Databases: databases, Origins: origins, Depth: 3, Limit: 200, Fanout: 6, Follow: true, Labels: labels}
}
