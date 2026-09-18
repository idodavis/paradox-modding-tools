// eventgraph_test.go covers neighborhood BFS and graph/detail payloads.

package views

import (
	"fmt"
	"strings"
	"testing"

	"paradox-modding-tools/services/internal/lang/session"
)

func nodeIDs(g EventGraph) map[string]EventGraphNode {
	out := make(map[string]EventGraphNode, len(g.Nodes))
	for _, n := range g.Nodes {
		out[n.ID] = n
	}
	return out
}

func TestNeighborhood(t *testing.T) {
	assert := func(name string, edges []EventGraphEdge, kinds map[string]string, root string, want, forbid []string) {
		t.Run(name, func(t *testing.T) {
			got, trunc, _, _ := neighborhood(edges, root, kinds, 80, nil)
			if trunc {
				t.Fatal("truncated")
			}
			for _, id := range want {
				if !got[id] {
					t.Fatalf("missing %s in %v", id, got)
				}
			}
			for _, id := range forbid {
				if got[id] {
					t.Fatalf("unexpected %s in %v", id, got)
				}
			}
		})
	}
	assert("one hop stops at on_action hub", []EventGraphEdge{
		{From: "a", To: "pulse"}, {From: "pulse", To: "c"}, {From: "pulse", To: "d"}, {From: "e", To: "a"},
	}, map[string]string{"a": "event", "c": "event", "d": "event", "e": "event", "pulse": "on_action"},
		"a", []string{"a", "pulse"}, []string{"c", "d", "e"})
	got, trunc, _, hiddenIn := neighborhood([]EventGraphEdge{{From: "a", To: "pulse"}, {From: "e", To: "a"}},
		"a", map[string]string{"a": "event", "e": "event", "pulse": "on_action"}, 80, nil)
	if trunc || hiddenIn["a"] != 1 || got["e"] {
		t.Fatalf("inbound stub: got=%v hiddenIn=%v trunc=%v", got, hiddenIn, trunc)
	}
	got, _, _, hiddenIn = neighborhood([]EventGraphEdge{{From: "a", To: "pulse"}, {From: "e", To: "a"}},
		"a", map[string]string{"a": "event", "e": "event", "pulse": "on_action"}, 80, []string{"a"})
	if !got["e"] || hiddenIn["a"] != 0 {
		t.Fatalf("expand inbound: got=%v hiddenIn=%v", got, hiddenIn)
	}
	assert("three event hops", []EventGraphEdge{
		{From: "a", To: "b"}, {From: "b", To: "c"}, {From: "c", To: "d"}, {From: "d", To: "e"},
	}, map[string]string{"a": "event", "b": "event", "c": "event", "d": "event", "e": "event"},
		"a", []string{"a", "b", "c", "d"}, []string{"e"})
}

func TestGraphViaOptionRoles(t *testing.T) {
	s := buildSession(t, "ck3", oneMod(t, "ck3", map[string]string{
		"events/ns.txt": "namespace = ns\nns.0 = { type = character_event\n\timmediate = { trigger_event = ns.1 }\n}\n" +
			"ns.1 = {\n\ttype = character_event\n\ttitle = ns.1.t\n\ttrigger = { always = yes }\n" +
			"\timmediate = {\n\t\thop_effect = yes\n\t\ttrigger_event = { id = ns.4 days = 30 }\n\t}\n\tdesc = used_key\n" +
			"\toption = { name = ns.1.a\n\t\ttrigger_event = ns.3\n\t\tadd_gold = 2\n\t}\n\tafter = { add_gold = 3 }\n}\n" +
			"ns.5 = { type = character_event\n\ttitle = missing_key\n\tdesc = used_key\n}\n" +
			"ns.2 = { type = character_event }\nns.3 = { type = character_event }\nns.4 = { type = character_event }\nns.9 = { type = character_event }\n" +
			"mod.1 = { type = character_event\n\ttitle = vanilla_key\n\timmediate = { trigger_event = v.1 }\n}\n",
		"events/fan.txt":                       fanoutBody(),
		"common/scripted_effects/e.txt":        "hop_effect = {\n\ttrigger_event = ns.2\n}\n",
		"localization/english/a_l_english.yml": "l_english:\n ns.1.t:0 \"Hello\"\n ns.1.a:0 \"Go\"\n used_key:0 \"Hi\"\n orphan_key:0 \"Bye\"\n",
		"localization/french/a_l_french.yml":   "l_french:\n used_key:0 \"Hi\"\n",
	}))
	g := Graph(s, EventGraphParams{Root: "ns.1"})
	ids := nodeIDs(g)
	for _, id := range []string{"ns.1", "ns.2", "ns.3", "ns.4"} {
		if _, ok := ids[id]; !ok {
			t.Fatalf("missing %s in %v", id, ids)
		}
	}
	if _, ok := ids["ns.0"]; ok {
		t.Fatal("ns.0 should be collapsed into the fired-by stub")
	}
	if ids["ns.1"].Role != "root" || ids["ns.2"].Role != "" {
		t.Fatalf("roles: %+v", ids)
	}
	if stub, ok := ids["more-in:ns.1"]; !ok || stub.Title != "+1 fired by" {
		t.Fatalf("fired-by stub = %+v ok=%v", stub, ok)
	}
	if expIDs := nodeIDs(Graph(s, EventGraphParams{Root: "ns.1", Expand: []string{"ns.1"}})); expIDs["ns.0"].Role == "caller" || expIDs["ns.0"].ID == "" {
		t.Fatalf("expand: %v", expIDs)
	}
	if _, ok := ids["ns.9"]; ok {
		t.Fatal("orphan ns.9 should not appear")
	}
	var via, opt, hops int
	for _, e := range g.Edges {
		if e.From == "ns.1" && e.To == "ns.2" && e.Kind == "via" {
			via++
			if strings.Count(e.Label, "→") > 2 {
				t.Errorf("via hop chain too long: %q", e.Label)
			}
		}
		if e.From == "ns.1" && e.To == "ns.3" && e.Kind == "option" {
			opt++
		}
		if e.To == "ns.4" {
			hops++
		}
	}
	if via == 0 || opt == 0 || hops == 0 {
		t.Fatalf("via=%d option=%d delay=%d edges=%v", via, opt, hops, g.Edges)
	}
	d := Detail(s, "ns.1")
	if d == nil || d.Type != "character_event" || d.Title == nil || d.Title.Text != "Hello" {
		t.Fatalf("header = %+v", d)
	}
	if len(d.Options) != 1 || d.Options[0].Name == nil || d.Options[0].Name.Text != "Go" {
		t.Fatalf("options = %+v", d.Options)
	}
	fired := false
	for _, sec := range d.Sections {
		for _, tgt := range sec.Targets {
			fired = fired || tgt.Name == "ns.3" || tgt.Name == "ns.4"
		}
	}
	if !fired {
		t.Fatalf("detail should target ns.3/ns.4; sections=%+v", d.Sections)
	}
	if empty := Graph(s, EventGraphParams{}); len(empty.Nodes) != 0 || empty.EmptyReason == "" {
		t.Fatalf("empty root nodes=%d reason=%q", len(empty.Nodes), empty.EmptyReason)
	}
	if rooted := nodeIDs(Graph(s, EventGraphParams{Root: "mod.1"})); rooted["mod.1"].Origin != "mod" || rooted["v.1"].Origin != "vanilla" {
		t.Fatalf("rooted = %v", rooted)
	}
	fg := Graph(s, EventGraphParams{Root: "r.1"})
	if !fg.Truncated {
		t.Fatal("expected truncated")
	}
	var more *EventGraphNode
	kids := 0
	for i := range fg.Nodes {
		n := &fg.Nodes[i]
		if n.Kind == "more" {
			more = n
		}
		if strings.HasPrefix(n.ID, "r.") && n.ID != "r.1" && n.Kind == "event" {
			kids++
		}
	}
	if more == nil || more.ID != "more:r.1" || more.Title != "+1 more" || kids != 12 {
		t.Fatalf("more stub = %+v kids=%d", more, kids)
	}
	exp := Graph(s, EventGraphParams{Root: "r.1", Expand: []string{"r.1"}})
	if len(exp.Nodes) != 14 {
		t.Fatalf("expand nodes = %d, want 14", len(exp.Nodes))
	}
	for _, gameID := range []string{"vic3", "eu5"} {
		rel := "events/x.txt"
		if gameID == "eu5" {
			rel = "in_game/events/x.txt"
		}
		gs := buildSession(t, gameID, oneMod(t, gameID, map[string]string{rel: "test.1 = { type = character_event }\n"}))
		if gg := Graph(gs, EventGraphParams{Root: "test.1"}); len(gg.Nodes) != 1 || gg.Nodes[0].ID != "test.1" || Detail(gs, "test.1") == nil {
			t.Errorf("%s graph nodes = %v", gameID, gg.Nodes)
		}
	}
}

func TestGraphDrawsEdgesBetweenSelectedNodes(t *testing.T) {
	s := buildSession(t, "ck3", oneMod(t, "ck3", map[string]string{
		"events/x.txt": "namespace = x\nx.0 = { type = character_event\n\timmediate = { trigger_event = x.1 trigger_event = x.2 }\n}\n" +
			"x.1 = { type = character_event immediate = { trigger_event = x.2 } }\nx.2 = { type = character_event immediate = { trigger_event = x.3 } }\n" +
			"x.3 = { type = character_event immediate = { trigger_event = x.4 trigger_event = x.5 } }\nx.4 = { type = character_event immediate = { trigger_event = x.5 } }\nx.5 = { type = character_event }\n",
	}))
	has := func(g EventGraph, from, to string) bool {
		for _, e := range g.Edges {
			if e.From == from && e.To == to {
				return true
			}
		}
		return false
	}
	if g := Graph(s, EventGraphParams{Root: "x.1"}); !has(g, "x.4", "x.5") {
		t.Fatalf("max-depth cross edge missing: %+v", g.Edges)
	}
	if exp := Graph(s, EventGraphParams{Root: "x.1", Expand: []string{"x.1"}}); !has(exp, "x.0", "x.2") {
		t.Fatalf("inbound cross edge missing: %+v", exp.Edges)
	}
}

func TestEventNamespacePayload(t *testing.T) {
	s := buildSession(t, "ck3", oneMod(t, "ck3", map[string]string{
		"events/x.txt":            "namespace = court\ncourt.1 = { type = character_event\n\timmediate = { trigger_event = court.2 }\n}\ncourt.2 = { type = character_event }\n",
		"common/on_actions/o.txt": "yearly_pulse = { events = { court.1 } }\n",
	}))
	g := Graph(s, EventGraphParams{Root: "court.1"})
	if n := nodeIDs(g)["court.1"]; n.Kind != "event" || n.Namespace != "court" {
		t.Fatalf("event node = %+v", n)
	}
	if d := Detail(s, "court.1"); d == nil || d.Namespace != "court" {
		t.Fatalf("event detail = %+v", d)
	}
	if oa := Detail(s, "yearly_pulse"); oa == nil || oa.Kind != "on_action" || oa.Namespace != "" || len(oa.Sections) == 0 {
		t.Fatalf("on_action detail = %+v", oa)
	}
}

func fanoutBody() string {
	var b strings.Builder
	b.WriteString("r.1 = {\n\ttype = character_event\n\tevents = {\n")
	for i := 2; i <= 14; i++ {
		fmt.Fprintf(&b, "\t\tr.%d\n", i)
	}
	b.WriteString("\t}\n}\n")
	for i := 2; i <= 14; i++ {
		fmt.Fprintf(&b, "r.%d = { type = character_event }\n", i)
	}
	return b.String()
}

// A fire edge the install holds is drawn with and without a mod.
func TestGraphVanillaFireEdge(t *testing.T) {
	for _, s := range []*session.Session{
		buildSession(t, "ck3"),
		buildSession(t, "ck3", oneMod(t, "ck3", map[string]string{"events/m.txt": "mod.1 = { type = character_event }\n"})),
	} {
		found := false
		for _, e := range Graph(s, EventGraphParams{Root: "v.1"}).Edges {
			found = found || e.From == "v.1" && e.To == "v.2"
		}
		if !found {
			t.Fatal("missing vanilla fire v.1 -> v.2")
		}
	}
}
