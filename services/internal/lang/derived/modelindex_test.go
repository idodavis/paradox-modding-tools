// modelindex_test.go covers the answers the model computes per query.

package derived

import "testing"

func TestEdgesToFollowsMacroCalls(t *testing.T) {
	m := NewModel("ck3", "english", &VanillaCache{}, nil, nil)
	m.addEdges([]Edge{
		{From: "ev.1", To: "fx", Kind: EdgeKindCall, Path: "a"},
		{From: "fx", To: "ev.2", Via: "trigger_event", Path: "b", Rescoped: true},
	})
	m.macroKeys["fx"] = true
	got := m.EdgesTo("ev.2")
	if len(got) != 2 || got[1].From != "ev.1" || got[1].Via != "fx" {
		t.Fatalf("EdgesTo = %+v", got)
	}
	// A hop inherits the fire site's scope truth: EventRootScope must still decline.
	if !got[1].Rescoped {
		t.Fatalf("via edge dropped Rescoped: %+v", got[1])
	}
}
