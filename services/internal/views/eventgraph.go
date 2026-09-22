// Package views is the three reports the app draws from a session: the event graph and one event's detail, and the workspace's health. Every report is the index's records arranged for a page; the page brings every number it wants (depth, fan-out, caps), and the package decides none.
//
// eventgraph.go is the graph around one definition: what it fires, hop by hop, and what fires it, one hop. A macro is a hop, not a node: a fire through a scripted effect is drawn as one edge whose label names the macros between. What is hidden is a stub the page can open, never silently dropped.
package views

import (
	"fmt"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/lang/index"
	"paradox-modding-tools/services/internal/lang/loc"
	"paradox-modding-tools/services/internal/lang/session"
)

// Params is what the page asks for. Every number is the page's.
type Params struct {
	Root     string   `json:"root"`     // the definition to draw around
	Depth    int      `json:"depth"`    // hops out of the root
	Fanout   int      `json:"fanout"`   // edges drawn out of one node before the rest are a stub
	MaxNodes int      `json:"maxNodes"` // nodes drawn before the graph is marked truncated
	Expand   []string `json:"expand"`   // stubs the page opened: more:<id>, more-in:<id>
}

// Node is one definition drawn, or a stub standing for what is hidden.
type Node struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Namespace string `json:"namespace,omitempty"`
	Title     string `json:"title,omitempty"`
	Origin    string `json:"origin,omitempty"`
	Rel       string `json:"rel,omitempty"`
	Line      int    `json:"line"`
	Root      bool   `json:"root,omitempty"`
	Stub      string `json:"stub,omitempty"` // "more": hidden fires out of Hidden's node; "fired-by": hidden callers
	Hidden    int    `json:"hidden,omitempty"`
}

// Edge is one fire drawn; Via names the tokens and macros between.
type Edge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Via  string `json:"via"`
}

// EventGraph is the page's graph; Reason says why it is empty.
type EventGraph struct {
	Nodes     []Node `json:"nodes"`
	Edges     []Edge `json:"edges"`
	Truncated bool   `json:"truncated"`
	Reason    string `json:"reason,omitempty"`
}

// fire is one edge out of a definition, with the macros it went through.
type fire struct {
	to, via string
}

// Graph draws the definitions around a root.
func Graph(s *session.Session, p Params) EventGraph {
	s.RLock()
	defer s.RUnlock()
	if p.Root == "" {
		return EventGraph{Reason: "Pick a root event."}
	}
	found := s.Defs(p.Root)
	if len(found) == 0 {
		return EventGraph{Reason: fmt.Sprintf("Nothing defines %s.", p.Root)}
	}
	g := &grapher{s: s, p: p, out: &EventGraph{}, drawn: map[string]bool{}}
	g.node(p.Root, true)

	// out of the root, hop by hop, each node's fan-out cut by the page's number
	frontier := []string{p.Root}
	for hop := 0; hop < p.Depth && len(frontier) > 0 && !g.out.Truncated; hop++ {
		var next []string
		for _, from := range frontier {
			fires := g.fires(from, map[string]bool{})
			opened := slices.Contains(p.Expand, "more:"+from)
			for i, f := range fires {
				if !opened && p.Fanout > 0 && i >= p.Fanout {
					g.out.Nodes = append(g.out.Nodes, Node{ID: "more:" + from, Kind: "more", Stub: "more", Hidden: len(fires) - i, Title: fmt.Sprintf("+%d more", len(fires)-i)})
					g.out.Edges = append(g.out.Edges, Edge{From: from, To: "more:" + from})
					break
				}
				if !g.drawn[f.to] {
					if p.MaxNodes > 0 && len(g.out.Nodes) >= p.MaxNodes {
						g.out.Truncated = true
						break
					}
					g.node(f.to, false)
					next = append(next, f.to)
				}
				g.out.Edges = append(g.out.Edges, Edge{From: from, To: f.to, Via: f.via})
			}
		}
		frontier = next
	}

	// into the root, one hop: a stub until the page opens it
	callers := g.firedBy(p.Root)
	if len(callers) > 0 {
		if slices.Contains(p.Expand, "more-in:"+p.Root) {
			for _, c := range callers {
				if !g.drawn[c.to] {
					g.node(c.to, false)
				}
				g.out.Edges = append(g.out.Edges, Edge{From: c.to, To: p.Root, Via: c.via})
			}
		} else {
			g.out.Nodes = append(g.out.Nodes, Node{ID: "more-in:" + p.Root, Kind: "more", Stub: "fired-by", Hidden: len(callers), Title: fmt.Sprintf("+%d fired by", len(callers))})
			g.out.Edges = append(g.out.Edges, Edge{From: "more-in:" + p.Root, To: p.Root})
			g.out.Nodes[0].Hidden = len(callers)
		}
	}

	// every fire between two drawn definitions is drawn, whatever hop found them
	for id := range g.drawn {
		for _, f := range g.fires(id, map[string]bool{}) {
			if g.drawn[f.to] && !slices.ContainsFunc(g.out.Edges, func(e Edge) bool { return e.From == id && e.To == f.to }) {
				g.out.Edges = append(g.out.Edges, Edge{From: id, To: f.to, Via: f.via})
			}
		}
	}
	return *g.out
}

// grapher holds one graph as it is drawn.
type grapher struct {
	s     *session.Session
	p     Params
	out   *EventGraph
	drawn map[string]bool
}

// node draws a definition: the one the game keeps, with its title.
func (g *grapher) node(id string, root bool) {
	kept, _ := g.s.Winner(g.s.Defs(id))
	g.drawn[id] = true
	g.out.Nodes = append(g.out.Nodes, Node{ID: id, Kind: kept.Kind, Namespace: namespace(id), Title: title(g.s, kept), Origin: kept.At.Origin, Rel: kept.At.Rel, Line: int(kept.Line), Root: root})
}

// fires is what a definition fires, in name order, a fire through a macro followed to what the macro fires; a macro met once on the way is not entered again.
func (g *grapher) fires(from string, seen map[string]bool) []fire {
	var out []fire
	for _, x := range g.s.All() {
		for _, e := range x.Fires(from) {
			if e.File < 0 {
				continue
			}
			if k := g.s.Schema.Kinds[e.ToKind]; k != nil && k.Macro != 0 {
				if !seen[e.To] {
					seen[e.To] = true
					for _, through := range g.fires(e.To, seen) {
						out = append(out, fire{to: through.to, via: e.To + " → " + through.via})
					}
				}
				continue
			}
			out = append(out, fire{to: e.To, via: e.Via})
		}
	}
	slices.SortStableFunc(out, func(a, b fire) int { return strings.Compare(a.to, b.to) })
	return slices.CompactFunc(out, func(a, b fire) bool { return a.to == b.to })
}

// firedBy is what fires a definition, in name order; a fire from inside a macro is credited to the macro's callers.
func (g *grapher) firedBy(to string) []fire {
	var out []fire
	for _, x := range g.s.All() {
		for _, e := range x.FiredBy(to) {
			if e.File < 0 {
				continue
			}
			if k := g.s.Schema.Kinds[e.FromKind]; k != nil && k.Macro != 0 && e.From != to {
				for _, caller := range g.firedBy(e.From) {
					out = append(out, fire{to: caller.to, via: caller.via + " → " + e.From})
				}
				continue
			}
			out = append(out, fire{to: e.From, via: e.Via})
		}
	}
	slices.SortStableFunc(out, func(a, b fire) int { return strings.Compare(a.to, b.to) })
	return slices.CompactFunc(out, func(a, b fire) bool { return a.to == b.to })
}

// namespace is the part of an event id before its last dot; "" for any other name.
func namespace(id string) string {
	if at := strings.LastIndexByte(id, '.'); at > 0 {
		return id[:at]
	}
	return ""
}

// title is a definition's display name: its label, else the text of the first loc key the kind's templates give it.
func title(s *session.Session, d index.Found) string {
	if d.Label != "" {
		return d.Label
	}
	if k := s.Schema.Kinds[d.Kind]; k != nil {
		for _, key := range loc.Expand(d.Name, k.LocTemplates) {
			if said, ok := s.Loc(key, ""); ok {
				return said.Value
			}
		}
	}
	return ""
}
