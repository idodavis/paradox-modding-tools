// eventgraph.go builds an event/on_action/decision graph from a root:
// outgoing depth 3 plus inbound 1-hop, fan-out 12, via-effect hops ≤3, no coordinates.

package views

import (
	"cmp"
	"maps"
	"slices"
	"strconv"
	"strings"

	"paradox-modding-tools/services/internal/lang/derived"
	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/session"
)

const (
	defaultMaxNodes = 40
	outDepth        = 3
	maxFanout       = 12
	morePrefix      = "more:"
	moreInPrefix    = "more-in:"
)

// Graph returns the event graph for the live session. Layout is not computed.
func Graph(s *session.Session, params EventGraphParams) EventGraph {
	kinds, macroKeys := s.GraphCatalog()
	if params.Root == "" {
		return EventGraph{EmptyReason: "Pick a root event."}
	}

	maxNodes := params.MaxNodes
	if maxNodes <= 0 {
		maxNodes = defaultMaxNodes
	}

	edges := collect(s, params.Root, kinds, macroKeys)
	selected, truncated, hidden, hiddenIn := neighborhood(
		edges, params.Root, kinds, maxNodes, params.Expand,
	)

	// Every stored edge between two selected nodes is drawn, cross-links included.
	outEdges := make([]EventGraphEdge, 0)
	seen := map[string]bool{}
	for _, id := range slices.Sorted(maps.Keys(selected)) {
		for _, e := range s.EdgesFrom(id) {
			if !selected[e.To] || !keepEdge(e, macroKeys) {
				continue
			}
			ge := labeledEdge(s, e)
			key := ge.From + "→" + ge.To + ":" + ge.Via + ":" + ge.Label + ":" + ge.Kind
			if seen[key] {
				continue
			}
			seen[key] = true
			outEdges = append(outEdges, ge)
		}
	}

	for parent, n := range hidden {
		if n <= 0 || !selected[parent] {
			continue
		}
		id := morePrefix + parent
		selected[id] = true
		outEdges = append(outEdges, EventGraphEdge{
			From: parent, To: id, Via: "more", Kind: "events",
		})
	}
	for parent, n := range hiddenIn {
		if n <= 0 || !selected[parent] {
			continue
		}
		id := moreInPrefix + parent
		selected[id] = true
		outEdges = append(outEdges, EventGraphEdge{
			From: id, To: parent, Via: "more-in", Kind: "events",
		})
	}

	firesCount := map[string]int{}
	for _, e := range outEdges {
		firesCount[e.From]++
	}

	nodes := make([]EventGraphNode, 0, len(selected))
	for id := range selected {
		if after, ok := strings.CutPrefix(id, morePrefix); ok {
			parent := after
			nodes = append(nodes, EventGraphNode{
				ID: id, Kind: "more",
				Title: "+" + strconv.Itoa(hidden[parent]) + " more",
			})
			continue
		}
		if after, ok := strings.CutPrefix(id, moreInPrefix); ok {
			parent := after
			nodes = append(nodes, EventGraphNode{
				ID: id, Kind: "more",
				Title: "+" + strconv.Itoa(hiddenIn[parent]) + " fired by",
			})
			continue
		}
		d := s.Resolve(id)
		n := EventGraphNode{ID: id, Kind: "unknown"}
		if d != nil {
			n.Kind = jomini.CanonicalKind(d.Kind)
			n.Origin = originID(d.Origin)
			n.OriginName = s.OriginName(n.Origin)
		}
		n.Title = titleOf(s, id)
		if n.Kind == "event" {
			n.Namespace = eventNamespace(s, d, id)
		}
		if c := firesCount[id]; c > 0 {
			n.Fires = c
		}
		if id == params.Root {
			n.Role = "root"
		}
		nodes = append(nodes, n)
	}
	slices.SortFunc(nodes, func(a, b EventGraphNode) int { return cmp.Compare(a.ID, b.ID) })

	return EventGraph{Nodes: nodes, Edges: outEdges, Truncated: truncated}
}

// keepEdge drops call edges and a macro's own fires: a macro is a hop, not a node.
func keepEdge(e derived.Edge, macroKeys map[string]bool) bool {
	return e.Kind != derived.EdgeKindCall &&
		(e.Kind == derived.EdgeKindVia || !macroKeys[e.From])
}

// collect gathers root's outgoing neighbourhood to outDepth, then what fires it.
func collect(
	s *session.Session, root string, kinds map[string]string, macroKeys map[string]bool,
) []EventGraphEdge {
	var out []EventGraphEdge
	type item struct {
		id    string
		depth int
	}
	seen := map[string]bool{root: true}
	for q := []item{{root, 0}}; len(q) > 0; q = q[1:] {
		cur := q[0]
		for _, e := range s.EdgesFrom(cur.id) {
			if !keepEdge(e, macroKeys) {
				continue
			}
			out = append(out, labeledEdge(s, e))
			// An on_action hub is drawn but not walked through, as in neighborhood.
			if seen[e.To] || cur.depth+1 > outDepth || (kinds[e.To] != "event" && e.To != root) {
				continue
			}
			seen[e.To] = true
			q = append(q, item{e.To, cur.depth + 1})
		}
	}
	for _, e := range s.EdgesTo(root) {
		if keepEdge(e, macroKeys) {
			out = append(out, labeledEdge(s, e))
		}
	}
	return out
}

func neighborhood(
	edges []EventGraphEdge, root string, kinds map[string]string, maxNodes int, expand []string,
) (selected map[string]bool, truncated bool, hidden, hiddenIn map[string]int) {
	selected = map[string]bool{root: true}
	hidden = map[string]int{}
	hiddenIn = map[string]int{}
	lift := map[string]bool{}
	for _, id := range expand {
		lift[id] = true
	}
	take := func(id string) bool {
		if selected[id] {
			return false
		}
		if maxNodes > 0 && len(selected) >= maxNodes {
			truncated = true
			return false
		}
		selected[id] = true
		return true
	}
	out := map[string][]string{}
	seenChild := map[string]bool{}
	for _, e := range edges {
		k := e.From + "\x00" + e.To
		if seenChild[k] {
			continue
		}
		seenChild[k] = true
		out[e.From] = append(out[e.From], e.To)
	}
	for from := range out {
		slices.Sort(out[from])
	}
	type item struct {
		id    string
		depth int
	}
	q := []item{{root, 0}}
	for len(q) > 0 {
		cur := q[0]
		q = q[1:]
		if cur.depth >= outDepth {
			continue
		}
		children := out[cur.id]
		if !lift[cur.id] && len(children) > maxFanout {
			hidden[cur.id] = len(children) - maxFanout
			truncated = true
			children = children[:maxFanout]
		}
		for _, to := range children {
			if !take(to) {
				if truncated {
					return selected, true, hidden, hiddenIn
				}
				continue
			}
			if kinds[to] != "event" && to != root {
				continue
			}
			q = append(q, item{to, cur.depth + 1})
		}
	}
	var inbound []string
	seenIn := map[string]bool{}
	for _, e := range edges {
		if e.To != root || selected[e.From] || seenIn[e.From] {
			continue
		}
		seenIn[e.From] = true
		inbound = append(inbound, e.From)
	}
	if lift[root] {
		for _, from := range inbound {
			if !take(from) {
				break
			}
		}
	} else if len(inbound) > 0 {
		hiddenIn[root] = len(inbound)
	}
	return selected, truncated, hidden, hiddenIn
}
