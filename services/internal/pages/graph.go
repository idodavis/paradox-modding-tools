// graph.go is the event graph page: the definitions around a root over what they fire, hop by hop, with stubs for
// what the page's numbers hide, cut to the origins the page shows.

package pages

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/index"
	"paradox-modding-tools/services/internal/schema"
	"paradox-modding-tools/services/internal/workspace"
)

// Params is what the page asks for. Every number is the page's.
type Params struct {
	Root     string   `json:"root"`     // the definition to draw around
	Depth    int      `json:"depth"`    // hops out of the root
	Fanout   int      `json:"fanout"`   // edges drawn out of one node before the rest are a stub
	MaxNodes int      `json:"maxNodes"` // nodes drawn before the graph is marked truncated
	Expand   []string `json:"expand"`   // stubs the page opened: more:<id>, more-in:<id>
	Origins  []string `json:"origins"`  // the origins drawn, with the root and its stubs; empty for all
	Scripted bool     `json:"scripted"` // each scripted effect on a chain drawn as a node between the two it joins
	Follow   bool     `json:"follow"`   // fires reached through a scripted effect are drawn; else only direct ones
}

// Node is one definition drawn, or a stub standing for what is hidden.
type Node struct {
	ID       string `json:"id"`
	Database string `json:"database"`
	Kind     string `json:"kind"` // what one of its database is called (game.KindName)
	Title    string `json:"title,omitempty"`
	Origin   string `json:"origin,omitempty"`
	Root     bool   `json:"root,omitempty"`
	Stub     string `json:"stub,omitempty"` // "more": fires hidden out of the node in its ID; "fired-by": callers hidden
}

// Edge is one fire drawn; Via names the scripted effects between, in the order they are entered, and Block the
// block of its definition the fire sits in (option a, immediate).
type Edge struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Via   string `json:"via"`
	Block string `json:"block,omitempty"`
}

// EventGraph is the page's graph; Reason says why it is empty. Reach counts what the depth holds with no fan-out
// or node cap (the definitions, and the scripted effects between when they are drawn), Kinds and Origins split it
// by kind (game.KindName) and by the origin that defines each.
type EventGraph struct {
	Nodes     []Node         `json:"nodes"`
	Edges     []Edge         `json:"edges"`
	Truncated bool           `json:"truncated"`
	Reached   int            `json:"reached,omitempty"` // truncated: the hop the node cap cut
	Reach     int            `json:"reach"`
	Kinds     map[string]int `json:"kinds"`
	Origins   map[string]int `json:"origins"`
	Reason    string         `json:"reason,omitempty"`
}

// grapher holds one graph as it is drawn: the session, the page's numbers, each definition found once, and the ids
// drawn.
type grapher struct {
	ws     *workspace.Session
	params Params
	out    *EventGraph
	drawn  map[string]bool
	defs   map[string]index.At
}

// Graph draws the definitions around a root: hop by hop over what they fire, each node's fan-out cut by the page's
// number, the callers into the root as a stub until the page opens it, every fire between two drawn definitions,
// the scripted effects drawn where the page shows them, and only the page's origins.
func Graph(ws *workspace.Session, params Params) *EventGraph {
	if params.Root == "" {
		return &EventGraph{Reason: "Pick a root event."}
	}
	graph := newGrapher(ws, params)
	root := graph.def(params.Root)
	if root == (index.At{}) {
		return &EventGraph{Reason: fmt.Sprintf("Nothing defines %s.", params.Root)}
	}
	graph.node(params.Root, true)
	graph.hops()
	graph.countReach()
	graph.callersStub(root)
	graph.between()
	if params.Scripted {
		graph.drawScripted()
	}
	if len(params.Origins) > 0 {
		graph.keepOrigins()
	}
	return graph.out
}

// newGrapher is a graph with nothing drawn.
func newGrapher(ws *workspace.Session, params Params) *grapher {
	return &grapher{ws: ws, params: params, out: &EventGraph{}, drawn: map[string]bool{}, defs: map[string]index.At{}}
}

// def is the definition the game keeps of an id, in whichever database defines it: one a fire names (an event, an
// on_action) before the others; the zero At where nothing does.
func (grapher *grapher) def(id string) index.At {
	if def, cached := grapher.defs[id]; cached {
		return def
	}
	s := grapher.ws.Install.Schema
	var def index.At
	for _, database := range slices.Sorted(maps.Keys(s.Databases)) {
		defs := index.Lookup(grapher.ws.Indexes(), schema.Defines, database, id)
		if len(defs) > 0 && (def == (index.At{}) || index.Fires(s, schema.Occurrence{Database: database})) {
			def, _, _, _ = grapher.ws.Winner(defs)
		}
	}
	grapher.defs[id] = def
	return def
}

// node draws one definition, with its title and the origin that defines it.
func (grapher *grapher) node(id string, root bool) {
	def := grapher.def(id)
	database := def.Occurrence().Database
	grapher.drawn[id] = true
	grapher.out.Nodes = append(grapher.out.Nodes, Node{
		ID: id, Database: database, Kind: game.KindName(database), Title: grapher.ws.Title(id, def),
		Origin: def.File().Origin, Root: root,
	})
}

// definedBy is whether one of the origins defines the id, in the database the game's definition of it is in, in any
// copy.
func (grapher *grapher) definedBy(id string, origins []string) bool {
	database := grapher.def(id).Occurrence().Database
	return slices.ContainsFunc(index.Lookup(grapher.ws.Indexes(), schema.Defines, database, id), func(def index.At) bool {
		return slices.Contains(origins, def.File().Origin)
	})
}

// hops draws the fires out of the root, hop by hop, each node's fan-out cut by the page's number, until the depth,
// a hop with no new node, or the node cap.
func (grapher *grapher) hops() {
	frontier := []string{grapher.params.Root}
	for hop := 0; hop < grapher.params.Depth && len(frontier) > 0 && !grapher.out.Truncated; hop++ {
		var next []string
		for _, id := range frontier {
			next = append(next, grapher.hop(id, hop)...)
		}
		frontier = next
	}
}

// hop draws what one node fires, and says which nodes it drew new, for the next hop: the fires past the fan-out
// are a stub, a new node past the node cap marks the graph truncated at the next hop, and an edge is drawn to a
// node drawn before as well.
func (grapher *grapher) hop(id string, hop int) (next []string) {
	params := grapher.params
	fires := followed(params, grapher.ws.Fires(grapher.def(id)))
	opened := slices.Contains(params.Expand, "more:"+id)
	for i, fire := range fires {
		if !opened && params.Fanout > 0 && i >= params.Fanout {
			grapher.moreStub(id, fires[i:])
			break
		}
		if !grapher.drawn[fire.Name] {
			if params.MaxNodes > 0 && len(grapher.out.Nodes) >= params.MaxNodes {
				grapher.out.Truncated, grapher.out.Reached = true, hop+1
				break
			}
			grapher.node(fire.Name, false)
			next = append(next, fire.Name)
		}
		grapher.out.Edges = append(grapher.out.Edges, Edge{From: id, To: fire.Name, Via: fire.Via, Block: fire.Block})
	}
	return next
}

// between draws every fire between two drawn definitions, whatever hop found them.
func (grapher *grapher) between() {
	for _, id := range slices.Sorted(maps.Keys(grapher.drawn)) {
		for _, fired := range followed(grapher.params, grapher.ws.Fires(grapher.def(id))) {
			known := slices.ContainsFunc(grapher.out.Edges, func(edge Edge) bool { return edge.From == id && edge.To == fired.Name })
			if grapher.drawn[fired.Name] && !known {
				grapher.out.Edges = append(grapher.out.Edges, Edge{From: id, To: fired.Name, Via: fired.Via, Block: fired.Block})
			}
		}
	}
}

// drawScripted draws each chain's scripted effects in turn between the two it joins; a hop two chains share is
// drawn once.
func (grapher *grapher) drawScripted() {
	var edges []Edge
	for _, edge := range grapher.out.Edges {
		from, block := edge.From, edge.Block
		if edge.Via != "" {
			for _, scripted := range strings.Split(edge.Via, " → ") {
				if !grapher.drawn[scripted] {
					grapher.node(scripted, false)
				}
				edges = append(edges, Edge{From: from, To: scripted, Block: block})
				from, block = scripted, ""
			}
		}
		edges = append(edges, Edge{From: from, To: edge.To, Block: block})
	}
	slices.SortStableFunc(edges, func(first, second Edge) int {
		return cmp.Compare(first.From+first.To, second.From+second.To)
	})
	grapher.out.Edges = slices.CompactFunc(edges, func(first, second Edge) bool {
		return first.From == second.From && first.To == second.To
	})
}

// keepOrigins keeps the nodes one of the page's origins defines (the root, and a stub whose node stays, always)
// and, for each node that goes, joins its edges in to its edges out, one edge per pair, so a chain through it stays
// joined.
func (grapher *grapher) keepOrigins() {
	kept := map[string]bool{}
	var gone []string
	var nodes []Node
	for _, node := range grapher.out.Nodes {
		kept[node.ID] = grapher.keeps(node, kept)
		if kept[node.ID] {
			nodes = append(nodes, node)
		} else {
			gone = append(gone, node.ID)
		}
	}
	grapher.out.Nodes = nodes
	for _, id := range gone {
		grapher.splice(id)
	}
}

// keeps is whether a node stays: the root, a stub whose node stays (the nodes before it say), or a definition one
// of the page's origins has.
func (grapher *grapher) keeps(node Node, kept map[string]bool) bool {
	if node.Root {
		return true
	}
	if node.Stub != "" {
		_, stubbed, _ := strings.Cut(node.ID, ":")
		return kept[stubbed]
	}
	return grapher.definedBy(node.ID, grapher.params.Origins)
}

// splice takes a node's edges away and joins each edge into it to each edge out of it, unless the pair is joined
// already or would loop to where it began.
func (grapher *grapher) splice(id string) {
	var entering, leaving, rest []Edge
	joined := map[[2]string]bool{}
	for _, edge := range grapher.out.Edges {
		switch {
		case edge.To == id:
			entering = append(entering, edge)
		case edge.From == id:
			leaving = append(leaving, edge)
		default:
			rest = append(rest, edge)
			joined[[2]string{edge.From, edge.To}] = true
		}
	}
	for _, edgeIn := range entering {
		for _, edgeOut := range leaving {
			pair := [2]string{edgeIn.From, edgeOut.To}
			if edgeIn.From != edgeOut.To && !joined[pair] {
				joined[pair] = true
				rest = append(rest, Edge{From: edgeIn.From, To: edgeOut.To, Block: edgeIn.Block})
			}
		}
	}
	grapher.out.Edges = rest
}
