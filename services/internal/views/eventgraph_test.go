// eventgraph_test.go covers the graph around a root: hops out, the one hop in, macros as hops, the stubs and their opening, the caps the page brings, and an empty root.
package views

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

func ids(g EventGraph) []string {
	var out []string
	for _, n := range g.Nodes {
		out = append(out, n.ID)
	}
	return out
}

func edge(g EventGraph, from, to string) (Edge, bool) {
	at := slices.IndexFunc(g.Edges, func(e Edge) bool { return e.From == from && e.To == to })
	if at < 0 {
		return Edge{}, false
	}
	return g.Edges[at], true
}

func TestGraph(t *testing.T) {
	chain := "namespace = g\n"
	for i := range 6 {
		chain += fmt.Sprintf("g.%d = {\n\ttype = character_event\n\timmediate = { trigger_event = g.%d }\n}\n", i, i+1)
	}
	chain += "g.6 = { type = character_event }\ng.9 = { type = character_event }\n"
	wide := "namespace = w\nw.0 = {\n\ttype = character_event\n\timmediate = {\n"
	for i := range 15 {
		wide += fmt.Sprintf("\t\ttrigger_event = w.%d\n", i+1)
	}
	wide += "\t}\n}\n"
	for i := range 15 {
		wide += fmt.Sprintf("w.%d = { type = character_event }\n", i+1)
	}
	s := workspace(t, map[string]string{
		"events/chain.txt":              chain,
		"events/wide.txt":               wide,
		"events/macro.txt":              "namespace = m\nm.1 = {\n\ttype = character_event\n\timmediate = { m_fire = yes }\n}\nm.2 = { type = character_event }\n",
		"common/scripted_effects/m.txt": "m_fire = {\n\ttrigger_event = m.2\n}\n",
		"common/on_action/m.txt":        "on_birth = {\n\tevents = { g.3 }\n}\n",
	})
	p := Params{Depth: 3, Fanout: 12, MaxNodes: 40}

	g := Graph(s, Params{})
	if len(g.Nodes) != 0 || g.Reason == "" {
		t.Errorf("an empty root is answered with a reason: %+v", g)
	}
	if g := Graph(s, Params{Root: "nothing.1"}); len(g.Nodes) != 0 || g.Reason == "" {
		t.Errorf("a root nothing defines: %+v", g)
	}

	// three hops out, the caller as a stub, the fourth hop not drawn
	p.Root = "g.1"
	g = Graph(s, p)
	for _, want := range []string{"g.1", "g.2", "g.3", "g.4"} {
		if !slices.Contains(ids(g), want) {
			t.Errorf("three hops out: no %s in %v", want, ids(g))
		}
	}
	if slices.Contains(ids(g), "g.5") || slices.Contains(ids(g), "g.9") || slices.Contains(ids(g), "g.0") || g.Truncated {
		t.Errorf("a fourth hop, an unconnected event and the caller are not drawn: %v", ids(g))
	}
	if g.Nodes[0].ID != "g.1" || !g.Nodes[0].Root || g.Nodes[0].Origin != "moda" || g.Nodes[0].Namespace != "g" || g.Nodes[0].Hidden != 1 {
		t.Errorf("the root, its origin, its namespace, and its hidden caller: %+v", g.Nodes[0])
	}
	if at := slices.IndexFunc(g.Nodes, func(n Node) bool { return n.ID == "more-in:g.1" }); at < 0 || g.Nodes[at].Stub != "fired-by" || g.Nodes[at].Title != "+1 fired by" {
		t.Errorf("the fired-by stub: %+v", g.Nodes)
	}
	if e, ok := edge(g, "g.1", "g.2"); !ok || e.Via != "trigger_event" {
		t.Errorf("a fire is an edge via its token: %+v", g.Edges)
	}

	// opening the stub draws the caller, and the on_action that fires g.3 is one hop in from g.3 only
	p.Expand = []string{"more-in:g.1"}
	g = Graph(s, p)
	if !slices.Contains(ids(g), "g.0") || slices.ContainsFunc(g.Nodes, func(n Node) bool { return n.Stub == "fired-by" }) {
		t.Errorf("opening the stub: %v", ids(g))
	}
	if g := Graph(s, Params{Root: "g.3", Depth: 1, Expand: []string{"more-in:g.3"}}); !slices.Contains(ids(g), "on_birth") || slices.Contains(ids(g), "g.1") {
		t.Errorf("one hop in stops at the caller: %v", ids(g))
	}

	// a macro is a hop: the edge goes through it, labelled
	g = Graph(s, Params{Root: "m.1", Depth: 2})
	if e, ok := edge(g, "m.1", "m.2"); !ok || !strings.Contains(e.Via, "m_fire") || slices.Contains(ids(g), "m_fire") {
		t.Errorf("a fire through a scripted effect: %+v %v", g.Edges, ids(g))
	}

	// a wide fan-out: the page's number of targets, then a stub; opened, all of them; a node cap truncates
	g = Graph(s, Params{Root: "w.0", Depth: 1, Fanout: 12, MaxNodes: 40})
	if at := slices.IndexFunc(g.Nodes, func(n Node) bool { return n.ID == "more:w.0" }); at < 0 || g.Nodes[at].Title != "+3 more" || len(g.Nodes) != 14 {
		t.Errorf("the more stub: %d nodes, %+v", len(g.Nodes), g.Nodes)
	}
	if g := Graph(s, Params{Root: "w.0", Depth: 1, Fanout: 12, MaxNodes: 40, Expand: []string{"more:w.0"}}); len(g.Nodes) != 16 {
		t.Errorf("opening the fan-out: %d nodes", len(g.Nodes))
	}
	if g := Graph(s, Params{Root: "w.0", Depth: 1, MaxNodes: 5}); !g.Truncated || len(g.Nodes) != 5 {
		t.Errorf("the node cap: %d nodes, truncated %v", len(g.Nodes), g.Truncated)
	}

	// the install's own fires are drawn with or without a mod
	g = Graph(s, Params{Root: "v.1", Depth: 1})
	if _, ok := edge(g, "v.1", "v.2"); !ok {
		t.Errorf("the install's fire: %+v", g.Edges)
	}
	if g := Graph(workspace(t), Params{Root: "v.1", Depth: 1}); len(g.Nodes) == 0 {
		t.Error("with no mod at all")
	}
}

func TestDetail(t *testing.T) {
	s := workspace(t, map[string]string{
		"events/m.txt":                         "namespace = m\nm.1 = {\n\ttype = character_event\n\ttitle = v.1.t\n\timmediate = { trigger_event = v.2 }\n\toption = {\n\t\tname = v.1.a\n\t}\n}\n",
		"localization/english/m_l_english.yml": "\xEF\xBB\xBFl_english:\n v.1.a:0 \"Go\"\n",
	})
	d := EventDetail(s, "m.1")
	if d.Reason != "" || d.Kind != "events" || d.Namespace != "m" || d.At.Origin != "moda" || len(d.Sites) != 1 {
		t.Fatalf("the header: %+v", d)
	}
	if at := slices.IndexFunc(d.Sections, func(x Section) bool { return x.Key == "type" }); at < 0 || d.Sections[at].Value != "character_event" || len(d.Sections) != 4 {
		t.Errorf("the sections: %+v", d.Sections)
	}
	if len(d.Options) != 1 || d.Options[0].Name != "v.1.a" || d.Options[0].Text != "Go" {
		t.Errorf("the options: %+v", d.Options)
	}
	if len(d.Fires) != 1 || d.Fires[0].To != "v.2" {
		t.Errorf("what it fires: %+v", d.Fires)
	}
	if v := EventDetail(s, "v.2"); len(v.FiredBy) == 0 {
		t.Errorf("what fires it: %+v", v.FiredBy)
	}
	if on := EventDetail(s, "on_birth"); on.Kind != "on_action" || on.Namespace != "" || len(on.Sections) == 0 {
		t.Errorf("an on_action's detail: %+v", on)
	}
	if none := EventDetail(s, "nothing.1"); none.Reason == "" {
		t.Error("a name nothing defines")
	}
}
