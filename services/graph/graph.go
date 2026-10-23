// Package graph is the Event Graph page's service. It builds the graph of what fires what from one root, using the
// index's fires, and the detail of one node. The graph has no coordinates; the page lays it out.
package graph

import (
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

// Service is the Event Graph service.
type Service struct {
	Workspace *workspace.Service
}

// A Node is one node of the event graph: the kept definition of an event, an on_action, a decision or a scripted
// effect, with where it is written.
type Node struct {
	ID       string          `json:"id"` // database:name
	Database string          `json:"database"`
	Name     string          `json:"name"`
	Title    string          `json:"title"` // its localized title; "" if it has none
	Site     index.Site      `json:"site"`
	Place    workspace.Place `json:"place"`
	Line     int             `json:"line"` // counted from one
	Step     int             `json:"step"` // its distance from the root, negative for a firer
}

// An Edge is one firing the graph draws, from a firer to what it fires, with the delay as written and the scripted
// effects that were spliced out of the line.
type Edge struct {
	From string   `json:"from"`
	To   string   `json:"to"`
	Days string   `json:"days,omitempty"` // the delay as written: days, months or years
	Via  []string `json:"via,omitempty"`  // the scripted effects passed through, when they are not drawn as nodes
}

// A Filter says what the graph shows: which databases and origins are drawn, how deep and how many nodes, and
// whether a firing through a scripted effect is followed.
type Filter struct {
	Databases []string `json:"databases"`
	Origins   []string `json:"origins"`
	Depth     int      `json:"depth"`
	Limit     int      `json:"limit"`
	Follow    bool     `json:"follow"`
}

// A Graph is the event graph from one root. It has no coordinates; the page lays it out.
type Graph struct {
	Root      string `json:"root"`
	Nodes     []Node `json:"nodes"`
	Edges     []Edge `json:"edges"`
	Truncated bool   `json:"truncated"` // the limit cut it
}

// A reach holds one graph while it is built from its root.
type reach struct {
	ws        *workspace.Session
	mods      *index.Index
	filter    Filter
	nodes     []Node
	sites     map[index.Site]bool // the sites of nodes
	edges     []Edge
	count     int // how many of the nodes the filter draws
	truncated bool
}

// delayKeys are the keys that write a delay.
var delayKeys = []string{"days", "months", "years"}

// delayBlock is the key of a block that holds a delay for every firing listed after it.
const delayBlock = "delay"

// BuildGraph builds the graph from a root node under a filter. It goes breadth-first forward over what the root
// fires and backward over what fires it, to the filter's depth and through scripted effects when they are
// followed, and stops at the node limit. A node the filter leaves out is spliced out: its incoming and outgoing
// edges become one edge with the node's name added to Via.
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
	reach := &reach{ws: ws, mods: mods, filter: filter, sites: map[index.Site]bool{site: true}, count: 1}
	reach.nodes = []Node{makeNode(ws, mods, site, 0)}
	reach.expand(site, true)
	reach.expand(site, false)
	reach.splice()
	return Graph{Root: root, Nodes: reach.nodes, Edges: lo.UniqBy(reach.edges, toKey), Truncated: reach.truncated}, nil
}

// resolveNode returns the definition the game keeps of the node an id names, `database:name`. It returns an error if
// the game keeps none.
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
		Title:    ws.LookupTitle(mods, site),
		Site:     site,
		Place:    ws.Locate(mods.File(site).Path),
		Line:     toLine(mods, site),
		Step:     step,
	}
}

// toLine returns the line of the occurrence at a site, counted from one.
func toLine(mods *index.Index, site index.Site) int {
	return int(mods.Occurrence(site).Line) + 1
}

// toID returns the id of the node of a definition: its database and its name.
func toID(mods *index.Index, site index.Site) string {
	reference := mods.ToReference(site)
	return reference.Database + ":" + reference.Name
}

// expand adds what is further from the root, a step at a time to the filter's depth: what each node fires when
// going forward, what fires each node when not.
func (reach *reach) expand(root index.Site, forward bool) {
	frontier := []index.Site{root}
	for distance := 1; distance <= reach.filter.Depth && len(frontier) > 0; distance++ {
		var next []index.Site
		for _, site := range frontier {
			var fires []index.Fire
			if forward {
				fires = listFires(reach.ws, reach.mods, site, reach.filter.Follow)
			} else {
				fires = reach.mods.ListFiredBy(reach.ws.Schema, site, reach.filter.Follow)
			}
			for _, fire := range fires {
				if end, isNew := reach.addFire(fire, forward, lo.Ternary(forward, distance, -distance)); isNew {
					next = append(next, end)
				}
			}
		}
		frontier = next
	}
}

// listFires returns what a node fires. With the flag set it goes through the scripted effects the node calls. A
// database that merges definitions fires what all of them do.
func listFires(ws *workspace.Session, mods *index.Index, site index.Site, follow bool) []index.Fire {
	return lo.FlatMap(listMerged(ws, mods, site), func(part index.Site, _ int) []index.Fire {
		return mods.ListFires(ws.Schema, part, follow)
	})
}

// listMerged returns the definitions that make up a node: all of those the game merges into its name, else the one
// given.
func listMerged(ws *workspace.Session, mods *index.Index, site index.Site) []index.Site {
	if winner, ok := mods.FindKeptLocal(ws.Schema, mods.ToReference(site), site.File); ok && len(winner.Merged) > 0 {
		return winner.Merged
	}
	return []index.Site{site}
}

// addFire puts a firing in the graph, with the nodes it needs, at the given step from the root. It returns the node
// at the end of the firing that is farther from the root, and whether the graph did not have that node before.
// Nothing past the node limit is added.
func (reach *reach) addFire(fire index.Fire, forward bool, step int) (index.Site, bool) {
	path := reach.listPath(fire)
	if !forward {
		slices.Reverse(path)
	}
	end := path[len(path)-1]
	isNew := !reach.sites[end]
	for position := 1; position < len(path); position++ {
		if !reach.add(path[position], step) {
			return end, false
		}
		from, to := path[position-1], path[position]
		if !forward {
			from, to = to, from
		}
		edge := Edge{From: toID(reach.mods, from), To: toID(reach.mods, to)}
		if to == fire.Target {
			edge.Days = findDelay(reach.ws, reach.mods, fire.Use)
		}
		if len(path) == 2 {
			edge.Via = fire.Via
		}
		reach.edges = append(reach.edges, edge)
	}
	return end, isNew
}

// listPath returns the definitions a firing goes through, from the firer to what it fires. The scripted effects it
// passes are between them when the filter draws the effects, else they are left to the edge.
func (reach *reach) listPath(fire index.Fire) []index.Site {
	path := []index.Site{fire.Source}
	effects := reach.ws.Schema.Effects
	if !slices.Contains(reach.filter.Databases, effects) {
		return append(path, fire.Target)
	}
	for _, name := range fire.Via {
		winner, ok := reach.mods.FindKeptLocal(reach.ws.Schema, schema.Reference{Database: effects, Name: name}, fire.Source.File)
		if !ok {
			return []index.Site{fire.Source, fire.Target}
		}
		path = append(path, winner.Site)
	}
	return append(path, fire.Target)
}

// add puts the node of a definition among the nodes, at the given step from the root. It returns false when the node
// limit keeps it out, and true when the node is in.
func (reach *reach) add(site index.Site, step int) bool {
	if reach.sites[site] {
		return true
	}
	if !reach.isHidden(site) {
		if reach.count >= reach.filter.Limit {
			reach.truncated = true
			return false
		}
		reach.count++
	}
	reach.sites[site] = true
	reach.nodes = append(reach.nodes, makeNode(reach.ws, reach.mods, site, step))
	return true
}

// isHidden reports whether the filter leaves out a definition's node: its database or its origin is not in the
// filter.
func (reach *reach) isHidden(site index.Site) bool {
	return !slices.Contains(reach.filter.Databases, reach.mods.ToReference(site).Database) ||
		!slices.Contains(reach.filter.Origins, reach.mods.File(site).Origin)
}

// findDelay returns the delay written for a firing, as written: the days, months or years statement in the first
// block, going up from the reference that fires, that holds one directly or in a delay block. It returns "" if none
// is written.
func findDelay(ws *workspace.Session, mods *index.Index, use index.Site) string {
	tree, err := ws.LoadTree(mods.File(use).Path)
	if err != nil {
		return ""
	}
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

// findDuration returns the days, months or years statement a block holds, as written. It returns "" if the block holds
// none.
func findDuration(tree *jomini.Tree, block int32) string {
	for _, key := range delayKeys {
		if child, ok := tree.FindChild(block, key); ok {
			return tree.Text(tree.Statements[child].Span)
		}
	}
	return ""
}

// splice takes the nodes the filter leaves out from the graph, each one's incoming and outgoing edges joined into one
// edge that passes through the node. The root stays whatever the filter says.
func (reach *reach) splice() {
	hidden := lo.Filter(reach.nodes, func(node Node, _ int) bool {
		return node.Step != 0 && reach.isHidden(node.Site)
	})
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
				rest = append(rest, Edge{From: first.From, To: second.To, Days: first.Days, Via: via})
			}
		}
		reach.edges = rest
	}
	reach.nodes = lo.Without(reach.nodes, hidden...)
}

// toKey returns a text that is the same for two edges exactly when they are the same edge: they go between the same
// nodes with the same delay and through the same scripted effects.
func toKey(edge Edge) string {
	return strings.Join(slices.Concat([]string{edge.From, edge.To, edge.Days}, edge.Via), "\x00")
}

// FindRoots returns the nodes a query names, up to the given limit: an exact id, a namespace as a prefix, or a
// number within every namespace.
func (gph *Service) FindRoots(query string, limit int) []Node {
	ws, err := gph.Workspace.Session()
	if err != nil {
		return nil
	}
	mods := ws.Mods.Load()
	var nodes []Node
	for _, database := range ws.Schema.FireDatabases {
		for _, site := range mods.ListDefinitions(database) {
			if len(nodes) >= limit {
				return nodes
			}
			if matchRoot(query, mods, site) && isKept(ws, mods, site) {
				nodes = append(nodes, makeNode(ws, mods, site, 0))
			}
		}
	}
	return nodes
}

// matchRoot reports whether a definition is a root a query names: its id or its name is the query, its name is in
// the namespace the query names, or its name ends with the number the query is.
func matchRoot(query string, mods *index.Index, site index.Site) bool {
	reference := mods.ToReference(site)
	return query == toID(mods, site) || query == reference.Name ||
		strings.HasPrefix(reference.Name, query+".") ||
		jomini.IsNumber(query) && strings.HasSuffix(reference.Name, "."+query)
}

// isKept reports whether a definition is the one the game keeps of its name. A name that several files define has
// one such definition.
func isKept(ws *workspace.Session, mods *index.Index, site index.Site) bool {
	winner, ok := mods.FindKeptLocal(ws.Schema, mods.ToReference(site), site.File)
	return ok && winner.Site == site
}

// Defaults returns the filter the page opens with: events, on_actions, decisions and scripted effects, the game and
// every mod, three steps deep, two hundred nodes, scripted effects followed.
func (gph *Service) Defaults() Filter {
	ws, err := gph.Workspace.Session()
	if err != nil {
		return Filter{}
	}
	databases := slices.Clone(ws.Schema.FireDatabases)
	if ws.Schema.Effects != "" {
		databases = append(databases, ws.Schema.Effects)
	}
	origins := []string{game.OriginVanilla}
	for _, mod := range ws.Order {
		origins = append(origins, mod.ID)
	}
	return Filter{Databases: databases, Origins: origins, Depth: 3, Limit: 200, Follow: true}
}
