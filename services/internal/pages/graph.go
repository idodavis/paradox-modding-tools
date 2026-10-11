// graph.go is the event graph page: the definitions around a root over what they fire, hop by hop, with stubs for
// what the page's numbers hide, cut to the origins the page shows; and the roots to pick from.

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

// Roots is a root search's hits, grouped by database, and how many of each database the query holds.
type Roots struct {
	Hits      []Node          `json:"hits"`
	Databases []DatabaseCount `json:"databases"`
}

// DatabaseCount is one database and how many roots of it a search holds.
type DatabaseCount struct {
	Database string `json:"database"`
	Count    int    `json:"count"`
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

// stubMore is the database every stub is drawn in, and the Stub of one standing for fires out of a node.
const stubMore = "more"

// maxRootsPerDatabase is how many roots of one database a root search draws.
const maxRootsPerDatabase = 8

// Graph draws the definitions around a root: hop by hop over what they fire, each node's fan-out cut by the page's
// number, the callers into the root as a stub until the page opens it, every fire between two drawn definitions,
// the scripted effects drawn where the page shows them, and only the page's origins. It takes the session's read
// lock.
func Graph(ws *workspace.Session, params Params) *EventGraph {
	ws.RLock()
	defer ws.RUnlock()
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

// followed is the fires drawn, through a scripted effect only when the page follows them: every fire with Follow,
// else the ones with no scripted effect between.
func followed(params Params, fires []workspace.Fire) []workspace.Fire {
	if params.Follow {
		return fires
	}
	return slices.DeleteFunc(slices.Clone(fires), func(fire workspace.Fire) bool { return fire.Via != "" })
}

// moreStub draws what a node's fan-out hides as one stub, joined to the node.
func (grapher *grapher) moreStub(id string, hidden []workspace.Fire) {
	grapher.out.Nodes = append(grapher.out.Nodes, Node{ID: "more:" + id, Database: stubMore, Stub: stubMore, Title: stubTitle(hidden)})
	grapher.out.Edges = append(grapher.out.Edges, Edge{From: id, To: "more:" + id})
}

// stubTitle is how many fires a stub hides, and of what kind where they are all of one database.
func stubTitle(hidden []workspace.Fire) string {
	title := fmt.Sprintf("+%d more", len(hidden))
	database := hidden[0].Database
	if slices.ContainsFunc(hidden, func(fire workspace.Fire) bool { return fire.Database != database }) {
		return title
	}
	title += " " + game.KindName(database)
	if len(hidden) > 1 {
		title += "s"
	}
	return title
}

// countReach counts the definitions within depth hops of the root, each once, by kind and by origin; with
// Scripted, the scripted effects a chain passes through too.
func (grapher *grapher) countReach() {
	grapher.out.Kinds, grapher.out.Origins = map[string]int{}, map[string]int{}
	seen := map[string]bool{}
	grapher.countOnce(grapher.params.Root, seen)
	frontier := []string{grapher.params.Root}
	for hop := 0; hop < grapher.params.Depth && len(frontier) > 0; hop++ {
		var next []string
		for _, id := range frontier {
			next = append(next, grapher.reachFrom(id, seen)...)
		}
		frontier = next
	}
}

// reachFrom counts what one node fires that is not counted yet, and says which are new, for the next hop.
func (grapher *grapher) reachFrom(id string, seen map[string]bool) (next []string) {
	for _, fire := range followed(grapher.params, grapher.ws.Fires(grapher.def(id))) {
		if grapher.params.Scripted && fire.Via != "" {
			for _, scripted := range strings.Split(fire.Via, " → ") {
				grapher.countOnce(scripted, seen)
			}
		}
		if !seen[fire.Name] {
			grapher.countOnce(fire.Name, seen)
			next = append(next, fire.Name)
		}
	}
	return next
}

// countOnce counts one definition, by its kind and by the origin that defines it, unless it is counted.
func (grapher *grapher) countOnce(id string, seen map[string]bool) {
	if seen[id] {
		return
	}
	seen[id] = true
	def := grapher.def(id)
	grapher.out.Reach++
	grapher.out.Kinds[game.KindName(def.Occurrence().Database)]++
	grapher.out.Origins[def.File().Origin]++
}

// callersStub draws what fires the root, one hop in, as a stub until the page opens it; a root the game never
// fires has none.
func (grapher *grapher) callersStub(root index.At) {
	params := grapher.params
	var callers []workspace.Fire
	if index.Fires(grapher.ws.Install.Schema, schema.Occurrence{Database: root.Occurrence().Database}) {
		callers = followed(params, grapher.ws.FiredBy(root, true))
	}
	if len(callers) == 0 {
		return
	}
	if !slices.Contains(params.Expand, "more-in:"+params.Root) {
		stub := "more-in:" + params.Root
		grapher.out.Nodes = append(grapher.out.Nodes, Node{
			ID: stub, Database: stubMore, Stub: "fired-by", Title: fmt.Sprintf("+%d fired by", len(callers)),
		})
		grapher.out.Edges = append(grapher.out.Edges, Edge{From: stub, To: params.Root})
		return
	}
	for _, caller := range callers {
		if !grapher.drawn[caller.Name] {
			grapher.node(caller.Name, false)
		}
		grapher.out.Edges = append(grapher.out.Edges, Edge{From: caller.Name, To: params.Root, Via: caller.Via, Block: caller.Block})
	}
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
		edges = append(edges, grapher.chain(edge)...)
	}
	slices.SortStableFunc(edges, func(first, second Edge) int {
		return cmp.Compare(first.From+first.To, second.From+second.To)
	})
	grapher.out.Edges = slices.CompactFunc(edges, func(first, second Edge) bool {
		return first.From == second.From && first.To == second.To
	})
}

// chain is an edge as the edges between the scripted effects it passes through, the first with the edge's block,
// drawing each effect not drawn yet; an edge through none is itself.
func (grapher *grapher) chain(edge Edge) []Edge {
	var chain []Edge
	from, block := edge.From, edge.Block
	if edge.Via != "" {
		for _, scripted := range strings.Split(edge.Via, " → ") {
			if !grapher.drawn[scripted] {
				grapher.node(scripted, false)
			}
			chain = append(chain, Edge{From: from, To: scripted, Block: block})
			from, block = scripted, ""
		}
	}
	return append(chain, Edge{From: from, To: edge.To, Block: block})
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

// definedBy is whether one of the origins defines the id, in the database the game's definition of it is in, in any
// copy.
func (grapher *grapher) definedBy(id string, origins []string) bool {
	database := grapher.def(id).Occurrence().Database
	return slices.ContainsFunc(index.Lookup(grapher.ws.Indexes(), schema.Defines, database, id), func(def index.At) bool {
		return slices.Contains(origins, def.File().Origin)
	})
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

// SearchRoots is the fired events and on_actions, and what fires one but a scripted entry, whose id holds the
// query: ids that start with it first, 8 of each database. It takes the session's read lock.
func SearchRoots(ws *workspace.Session, query string) *Roots {
	ws.RLock()
	defer ws.RUnlock()
	query = strings.ToLower(query)
	fired, firing := firedNames(ws)
	databaseOf := map[string]string{} // by name
	for name, database := range fired {
		if strings.Contains(strings.ToLower(name), query) && len(index.Lookup(ws.Indexes(), schema.Defines, database, name)) > 0 {
			databaseOf[name] = database
		}
	}
	// a name of several databases takes the first in sorted order, so no map order decides
	for _, roots := range firing {
		for _, root := range roots {
			if strings.Contains(strings.ToLower(root.Name), query) && (databaseOf[root.Name] == "" || root.Database < databaseOf[root.Name]) {
				databaseOf[root.Name] = root.Database
			}
		}
	}
	byDatabase := map[string][]string{}
	for name, database := range databaseOf {
		byDatabase[database] = append(byDatabase[database], name)
	}
	out := &Roots{}
	for database, names := range byDatabase {
		out.Databases = append(out.Databases, DatabaseCount{Database: database, Count: len(names)})
	}
	slices.SortFunc(out.Databases, func(first, second DatabaseCount) int {
		return cmp.Or(cmp.Compare(second.Count, first.Count), strings.Compare(first.Database, second.Database))
	})
	drawn := newGrapher(ws, Params{})
	for _, count := range out.Databases {
		names := byDatabase[count.Database]
		slices.SortFunc(names, func(first, second string) int {
			return cmp.Or(
				cmp.Compare(min(strings.Index(strings.ToLower(first), query), 1), min(strings.Index(strings.ToLower(second), query), 1)),
				strings.Compare(first, second),
			)
		})
		for _, id := range names[:min(maxRootsPerDatabase, len(names))] {
			drawn.node(id, false)
		}
	}
	position := func(node Node) int {
		return slices.IndexFunc(out.Databases, func(count DatabaseCount) bool { return count.Database == node.Database })
	}
	slices.SortStableFunc(drawn.out.Nodes, func(first, second Node) int { return cmp.Compare(position(first), position(second)) })
	out.Hits = drawn.out.Nodes
	return out
}

// firedNames is, in the files the game loads, what is fired (by database) and what fires it: a scripted entry by
// what fires through it (FiredBy), another definition by itself.
func firedNames(ws *workspace.Session) (fired map[string]string, firing map[index.At][]workspace.Fire) {
	fired, firing = map[string]string{}, map[index.At][]workspace.Fire{}
	s, indexes := ws.Install.Schema, ws.Indexes()
	for i := range indexes {
		for _, file := range indexes[i].Files {
			if _, loaded := index.Loaded(indexes, file.Path); !loaded {
				continue
			}
			for _, ref := range indexes[i].InFile(file.Path) {
				occurrence := ref.Occurrence()
				if occurrence.Role != schema.Refers || !index.Fires(s, occurrence) {
					continue
				}
				fired[occurrence.ID] = occurrence.Database
				caller, inEntry := ref.Entry()
				if _, known := firing[caller]; !inEntry || known {
					continue
				}
				calling := caller.Occurrence()
				if index.Scripted(s, calling) {
					firing[caller] = ws.FiredBy(caller, false)
				} else {
					firing[caller] = []workspace.Fire{{Name: calling.ID, Database: calling.Database}}
				}
			}
		}
	}
	return fired, firing
}
