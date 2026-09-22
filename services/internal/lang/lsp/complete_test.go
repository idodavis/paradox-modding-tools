// complete_test.go covers what is offered at a key position and at a value position, how the items rank, the cap, and the lazy documentation.
package lsp

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"paradox-modding-tools/services/internal/lang/schema"
)

func TestComplete(t *testing.T) {
	s, dir := fixture(t, "ck3")
	opened(s, dir, "common/scripted_effects/m.txt", "m_effect = {\n\tadd_gold = 1\n}\n")
	labels := func(items []Item) []string {
		var out []string
		for _, i := range items {
			out = append(out, i.Label)
		}
		return out
	}
	rank := func(items []Item, label string) int {
		if at := slices.IndexFunc(items, func(i Item) bool { return i.Label == label }); at >= 0 {
			return items[at].Rank
		}
		return -1
	}

	for _, tt := range []struct {
		what, rel, text string
		has, hasNot     []string
		check           func(items []Item) bool
	}{
		{"a trigger body offers triggers, not effects", "events/x.txt", "namespace = m\nm.1 = {\n\ttrigger = {\n\t\t", []string{"is_adult", "has_trait"}, []string{"add_gold", "type"}, nil},
		{"an effect body offers effects, those in its scope first", "common/on_action/x.txt", "on_birth = {\n\teffect = {\n\t\t", []string{"add_gold", "add_title_law"}, nil,
			func(items []Item) bool { return rank(items, "add_gold") == 0 && rank(items, "add_title_law") == 2 }},
		{"a structure offers its members, from every position the key is written at", "events/x.txt", "namespace = m\nm.1 = {\n\tcooldown = {\n\t\t", []string{"days", "months"}, nil, nil},
		{"the top of a file offers the kind's own names", "events/x.txt", "namespace = m\nm.1 = { }\nm.2 = { }\nm.", []string{"m.1", "m.2"}, nil, nil},
		{"a value position offers definitions of the kind the field accepts", "events/x.txt", "namespace = m\nm.1 = {\n\ttheme = s", []string{"seduction"}, []string{"brave"}, nil},
		{"a loc field offers loc keys, not definitions", "events/x.txt", "namespace = m\nm.1 = {\n\ttitle = v.1", []string{"v.1.t"}, []string{"v.1"}, nil},
		{"a body offers the macros of its class beside its tokens", "events/x.txt", "namespace = m\nm.1 = {\n\timmediate = {\n\t\t", []string{"add_gold", "v_effect", "m_effect"}, []string{"v_trigger"}, nil},
		{"a name token offers the names the workspace makes", "events/x.txt", "namespace = m\nm.1 = {\n\timmediate = {\n\t\tadd_to_variable_list = { name = mine target = root }\n\t\tevery_in_list = { variable = ", []string{"mine", "seen_list"}, nil, nil},
		{"a block nothing classifies offers what the body around it offers", "common/on_action/x.txt", "on_birth = {\n\teffect = {\n\t\tweird = {\n\t\t\t", []string{"add_gold"}, nil, nil},
		{"a cite offers the kind's names spelled whole, over the whole cite", "events/x.txt", "namespace = m\nm.1 = {\n\ttrigger = {\n\t\thas_trait = trait:br", []string{"trait:brave"}, nil,
			func(items []Item) bool {
				at := slices.IndexFunc(items, func(i Item) bool { return i.Label == "trait:brave" })
				return at >= 0 && items[at].Range.Start.Character == 14 && items[at].Range.End.Character == 22
			}},
		{"a list offers what its field accepts", "common/traits/m.txt", "bold = {\n\topposites = {\n\t\tbr", nil, nil, nil},
		{"a bool", "events/x.txt", "namespace = m\nm.1 = {\n\ttrigger = {\n\t\tis_adult = ", []string{"yes", "no"}, nil, nil},
	} {
		path := opened(s, dir, tt.rel, tt.text)
		line := strings.Count(tt.text, "\n")
		col := len(tt.text) - strings.LastIndex(tt.text, "\n") - 1
		got, err := Complete(s, path, line, col)
		if err != nil {
			t.Fatal(err)
		}
		names := labels(got.Items)
		for _, want := range tt.has {
			if !slices.Contains(names, want) {
				t.Errorf("%s: no %q in %v", tt.what, want, names)
			}
		}
		for _, not := range tt.hasNot {
			if slices.Contains(names, not) {
				t.Errorf("%s: %q offered", tt.what, not)
			}
		}
		if tt.check != nil && !tt.check(got.Items) {
			t.Errorf("%s: %+v", tt.what, got.Items)
		}
	}

	// the cap cuts and marks the list
	var many strings.Builder
	for i := range 100 {
		fmt.Fprintf(&many, "eff_%03d = { add_gold = 1 }\n", i)
	}
	opened(s, dir, "common/scripted_effects/many.txt", many.String())
	text := "namespace = m\nm.1 = {\n\timmediate = {\n\t\t"
	path := opened(s, dir, "events/x.txt", text)
	got, _ := Complete(s, path, 3, 2)
	if len(got.Items) != maxComplete || !got.Incomplete {
		t.Errorf("the cap: %d items, incomplete %v", len(got.Items), got.Incomplete)
	}

	// documentation comes when an item is shown
	for _, tt := range []struct {
		item Item
		want string
	}{
		{Item{Label: "add_gold", Value: schema.Value{Type: schema.TypeToken, Name: "add_gold"}}, "Adds gold to the scoped character"},
		{Item{Label: "v.1.t", Value: schema.Value{Type: schema.TypeLoc, Name: "v.1.t"}}, "Hello"},
		{Item{Label: "brave", Value: schema.Value{Type: schema.TypeRef, Kind: "traits", Name: "brave"}}, "Brave"},
	} {
		if docs, err := Resolve(s, path, tt.item); err != nil || !strings.Contains(docs, tt.want) {
			t.Errorf("%s: %q (%v)", tt.item.Label, docs, err)
		}
	}
}
