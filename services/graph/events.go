// events.go builds the graph of an event, an on_action, a decision, a message
// or a scripted effect: what it fires and what fires it.

package graph

import (
	"slices"

	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/services/workspace"
)

// reach is one graph of firings while it is built from its root.
type reach struct {
	ws     *workspace.Session
	mods   *index.Index
	filter Filter
	nodes  []Node

	// ids holds the id of every node added.
	ids map[string]bool

	edges []Edge

	// count is how many nodes count toward the limit; hidden ones do not.
	// cut holds the ids of the nodes the limit kept out.
	count int
	cut   map[string]bool
}

// buildFires builds the graph of what the definition fires and what fires it.
func buildFires(ws *workspace.Session, mods *index.Index, site index.Site, filter Filter) Graph {
	reach := &reach{ws: ws, mods: mods, filter: filter, ids: map[string]bool{toID(mods, site): true}, count: 1, cut: map[string]bool{}}
	reach.nodes = []Node{drawNode(ws, mods, site, 0)}
	reach.expand(site, true)
	reach.expand(site, false)
	reach.splice()

	return Graph{
		Root: toID(mods, site), Nodes: reach.nodes, Edges: dropRepeats(reach.edges, toKey),
		Truncated: len(reach.cut) > 0, Total: reach.count + len(reach.cut),
	}
}

// expand adds the nodes forward or backward from the root, link by link.
// Past the filter's depth it goes on only from the nodes the filter opens,
// and marks the rest as leading further.
func (r *reach) expand(root index.Site, forward bool) {
	frontier := []index.Site{root}
	for depth := 1; len(frontier) > 0; depth++ {
		if depth > r.filter.Depth {
			parents := toParents(r.listHidden())
			var opened, rest []index.Site
			for _, site := range frontier {
				if slices.Contains(r.filter.Expand, findDrawn(parents, toID(r.mods, site))) {
					opened = append(opened, site)
				} else {
					rest = append(rest, site)
				}
			}

			frontier = opened
			r.markDeep(rest, forward)
		}

		distance := depth
		if !forward {
			distance = -depth
		}

		var next []index.Site
		for _, site := range frontier {
			for _, fire := range r.fold(site, r.listLinks(site, forward), forward, distance) {
				if end, isNew := r.addFire(fire, forward, distance); isNew {
					next = append(next, end)
				}
			}
		}

		frontier = next
	}
}

// listHidden returns the nodes the filter hides. The root and the folds are
// never hidden.
func (r *reach) listHidden() []Node {
	var hidden []Node
	for _, node := range r.nodes {
		if node.Distance != 0 && node.Count == 0 && r.isHidden(node.Database, node.Site.Place.Origin) {
			hidden = append(hidden, node)
		}
	}

	return hidden
}

func toParents(nodes []Node) map[string]string {
	parents := map[string]string{}
	for _, node := range nodes {
		parents[node.ID] = node.Parent
	}

	return parents
}

// findDrawn returns the nearest drawn node at or above a node, climbing
// through the given parents of the hidden nodes.
func findDrawn(parents map[string]string, id string) string {
	for next, ok := parents[id]; ok; next, ok = parents[id] {
		id = next
	}

	return id
}

// markDeep marks each of the nodes that leads further in the given
// direction.
func (r *reach) markDeep(sites []index.Site, forward bool) {
	for _, site := range sites {
		id := toID(r.mods, site)
		position := slices.IndexFunc(r.nodes, func(node Node) bool { return node.Count == 0 && node.ID == id })
		if position >= 0 && len(r.listLinks(site, forward)) > 0 {
			r.nodes[position].Deep = true
		}
	}
}

// listLinks returns what a node fires going forward, or what fires it going
// backward.
func (r *reach) listLinks(site index.Site, forward bool) []index.Fire {
	if forward {
		return listFires(r.ws, r.mods, site, r.filter.Follow)
	}

	return r.mods.ListFiredBy(r.ws.Schema, site, r.filter.Follow)
}

// fold returns a node's firings up to the filter's fan-out, and adds one
// fold node for the rest.
func (r *reach) fold(site index.Site, fires []index.Fire, forward bool, distance int) []index.Fire {
	parent := toID(r.mods, site)
	id, edge := "more-in:"+parent, Edge{From: "more-in:" + parent, To: parent}
	if forward {
		id, edge = "more:"+parent, Edge{From: parent, To: "more:" + parent}
	}

	if !r.isFolded(len(fires), id) {
		return fires
	}

	rest := fires[r.filter.Fanout:]
	databases := make([]string, len(rest))
	for position, fire := range rest {
		end := fire.Source
		if forward {
			end = fire.Target
		}

		databases[position] = r.mods.ToReference(end).Database
	}

	r.nodes = append(r.nodes, makeFold(id, parent, distance, databases))
	r.edges = append(r.edges, edge)

	return fires[:r.filter.Fanout]
}

// isFolded reports whether a node's firings pass the fan-out while their
// fold is closed.
func (r *reach) isFolded(count int, id string) bool {
	return r.filter.Fanout > 0 && count > r.filter.Fanout && !slices.Contains(r.filter.Expand, id)
}

// listFires returns what a node fires: the firings of every copy of its
// name the game runs. With follow, it adds what the scripted effects it
// calls fire.
func listFires(ws *workspace.Session, mods *index.Index, site index.Site, follow bool) []index.Fire {
	var fires []index.Fire
	for _, part := range listActing(ws, mods, site) {
		fires = append(fires, mods.ListFires(ws.Schema, part, follow)...)
	}

	return fires
}

// listActing returns the copies of a node's name that the game runs, as
// seen from the given copy's file.
func listActing(ws *workspace.Session, mods *index.Index, site index.Site) []index.Site {
	return mods.ListActing(ws.Schema, mods.ToReference(site), site.File)
}

// addFire puts a firing and its nodes in the graph. Where the filter draws
// scripted effects, each effect the firing passes through is a node on the
// way. It returns the far end and whether that node is new; nothing past the
// node limit is added.
func (r *reach) addFire(fire index.Fire, forward bool, distance int) (index.Site, bool) {
	path := []index.Site{fire.Source, fire.Target}
	drawsEffects := slices.Contains(r.filter.Databases, r.ws.Schema.Effects)
	if drawsEffects {
		path = slices.Concat([]index.Site{fire.Source}, fire.Via, []index.Site{fire.Target})
	}

	if !forward {
		slices.Reverse(path)
	}

	end := path[len(path)-1]
	isNew := !r.ids[toID(r.mods, end)]
	whole := makeEdge(r.ws, r.mods, fire)
	for position := 1; position < len(path); position++ {
		if !r.add(path[position], distance, path[position-1]) {
			return end, false
		}

		source, target := path[position-1], path[position]
		if !forward {
			source, target = target, source
		}

		edge := Edge{Site: whole.Site}
		if target == fire.Target {
			edge = whole
		}

		edge.From, edge.To = toID(r.mods, source), toID(r.mods, target)
		if drawsEffects {
			edge.Via = nil
		}

		r.edges = append(r.edges, edge)
	}

	return end, isNew
}

// add puts a definition's node in the graph, reached from the given parent.
// A name has one node, whichever copy leads to it. It returns false if the
// node limit keeps it out.
func (r *reach) add(site index.Site, distance int, parent index.Site) bool {
	id := toID(r.mods, site)
	if r.ids[id] {
		return true
	}

	if !r.isHidden(r.mods.ToReference(site).Database, r.mods.File(site).Origin) {
		if r.count >= r.filter.Limit {
			r.cut[id] = true

			return false
		}

		r.count++
	}

	r.ids[id] = true
	node := drawNode(r.ws, r.mods, site, distance)
	node.Parent = toID(r.mods, parent)
	node.Looped = distance < 0 && r.isLooping(node.Parent)
	r.nodes = append(r.nodes, node)

	return true
}

// isLooping reports whether a firer found through another node comes round
// a loop: that node is one the root fires, or was found round a loop itself.
func (r *reach) isLooping(id string) bool {
	return slices.ContainsFunc(r.nodes, func(node Node) bool { return node.ID == id && (node.Distance > 0 || node.Looped) })
}

// isHidden reports whether the filter hides a node of a database and an
// origin. Only a database the filter picks among can be hidden by its
// database.
func (r *reach) isHidden(database, origin string) bool {
	isHiddenDatabase := !slices.Contains(r.filter.Databases, database) && isFiringDatabase(r.ws.Schema, database)

	return isHiddenDatabase || !slices.Contains(r.filter.Origins, origin)
}

// isFiringDatabase reports whether a database is one of the databases the
// filter picks among.
func isFiringDatabase(schema *lang.Schema, database string) bool {
	return slices.Contains(schema.FireDatabases, database) || schema.Effects != "" && database == schema.Effects
}

// listFiringDatabases returns the databases the filter picks among: those whose
// definitions fire one another, and the scripted effects.
func listFiringDatabases(schema *lang.Schema) []string {
	databases := slices.Clone(schema.FireDatabases)
	if schema.Effects != "" {
		databases = append(databases, schema.Effects)
	}

	return databases
}

// splice takes the hidden nodes out and joins the edges through each. A
// joined edge keeps the first firing's delay and steps.
func (r *reach) splice() {
	hidden := r.listHidden()
	for _, node := range hidden {
		var rest, incoming, outgoing []Edge
		for _, edge := range r.edges {
			isLoop := edge.From == node.ID && edge.To == node.ID
			switch {
			case isLoop:
				continue
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
				joined := first
				joined.To, joined.Via = second.To, slices.Concat(first.Via, []string{node.Name}, second.Via)
				rest = append(rest, joined)
			}
		}

		r.edges = rest
	}

	parents := toParents(hidden)
	r.nodes = slices.DeleteFunc(r.nodes, func(node Node) bool {
		_, isHidden := parents[node.ID]

		return isHidden
	})
	r.passDeep(hidden, parents)
	for position := range r.nodes {
		r.nodes[position].Parent = findDrawn(parents, r.nodes[position].Parent)
	}
}

// passDeep moves a hidden node's deep mark to the nearest drawn node above
// it, so that opening that node goes on past the hidden one.
func (r *reach) passDeep(hidden []Node, parents map[string]string) {
	for _, node := range hidden {
		if !node.Deep {
			continue
		}

		drawn := findDrawn(parents, node.Parent)
		if position := slices.IndexFunc(r.nodes, func(other Node) bool { return other.ID == drawn }); position >= 0 {
			r.nodes[position].Deep = true
		}
	}
}
