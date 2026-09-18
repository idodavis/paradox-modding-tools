// complete_test.go covers completion: which items each block type offers.

package lsp

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func hasComplete(items []CompletionItem, label string) bool {
	for _, it := range items {
		if it.Label == label {
			return true
		}
	}
	return false
}

func labels(items []CompletionItem) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Label
	}
	return out
}

// cursorAt returns the line and UTF-8 column right after needle's first occurrence in body.
func cursorAt(body, needle string) (int, int) {
	i := strings.Index(body, needle)
	if i < 0 {
		return -1, -1
	}
	end := i + len(needle)
	return strings.Count(body[:end], "\n"), end - (strings.LastIndex(body[:end], "\n") + 1)
}

func TestComplete(t *testing.T) {
	cases := []struct {
		name   string
		files  map[string]string
		path   string
		needle string
		want   []string
		avoid  []string
		bucket map[string]string // label -> expected leading SortText digit
	}{
		{
			// A body's own declared tokens, not another kind's structure keys.
			name:   "body trigger",
			files:  map[string]string{"events/x.txt": "test.1 = {\n\ttrigger = {\n\t\t\n\t}\n}\n"},
			path:   "events/x.txt",
			needle: "test.1 = {\n\ttrigger = {\n\t\t",
			want:   []string{"is_adult"},
			avoid:  []string{"add_gold"},
		},
		{
			// A token in scope sorts bucket 0; the rest still offer, bucket 2.
			name:   "body effect with scope",
			files:  map[string]string{"common/on_action/x.txt": "on_birth = {\n\teffect = {\n\t\t\n\t}\n}\n"},
			path:   "common/on_action/x.txt",
			needle: "on_birth = {\n\teffect = {\n\t\t",
			want:   []string{"add_gold", "add_stability"},
			bucket: map[string]string{"add_gold": "0", "add_stability": "2"},
		},
		{
			// A structure unifies across every position its key is written at.
			name:   "structure members incl. a member written at another position",
			files:  map[string]string{"events/x.txt": "test.3 = {\n\tcooldown = {\n\t\t\n\t}\n}\n"},
			path:   "events/x.txt",
			needle: "test.3 = {\n\tcooldown = {\n\t\t",
			want:   []string{"days", "months"},
		},
		{
			// A database position offers the kind's own definition names.
			name:   "database names",
			files:  map[string]string{"events/x.txt": "test.1 = { }\ntest.2 = { }\nte"},
			path:   "events/x.txt",
			needle: "test.1 = { }\ntest.2 = { }\nte",
			want:   []string{"test.1", "test.2"},
		},
		{
			// A bare list member's cell is the field one level up.
			name: "list values",
			files: map[string]string{
				"common/traits/t.txt": "trait_a = { }\ntrait_b = { }\n",
				"events/x.txt": "test.1 = {\n\tsome_list = {\n\t\ttrait_a trait_b\n\t}\n}\n" +
					"test.2 = {\n\tsome_list = {\n\t\ttr\n\t}\n}\n",
			},
			path:   "events/x.txt",
			needle: "test.2 = {\n\tsome_list = {\n\t\ttr",
			want:   []string{"trait:trait_a", "trait:trait_b"},
		},
		{
			// A scalar value position offers definitions of the cell's resolved kind.
			name:   "value position by cell",
			files:  map[string]string{"events/x.txt": "test.2 = {\n\ttheme = s\n}\n"},
			path:   "events/x.txt",
			needle: "test.2 = {\n\ttheme = s",
			want:   []string{"seduction"},
		},
		{
			// A cell classified loc offers loc keys, not definitions.
			name: "loc key",
			files: map[string]string{
				"events/x.txt":                         "test.1 = {\n\ttitle = te\n}\n",
				"localization/english/m_l_english.yml": "l_english:\n test.1.t:0 \"Hi\"\n other:0 \"O\"\n",
			},
			path:   "events/x.txt",
			needle: "title = te",
			want:   []string{"test.1.t"},
			avoid:  []string{"other"},
		},
		{
			// A scripted macro of the body's own class completes alongside its tokens.
			name: "macro call",
			files: map[string]string{
				"common/scripted_effects/e.txt": "my_effect = {\n\tadd_gold = 5\n}\n",
				"events/x.txt":                  "test.1 = {\n\timmediate = {\n\t\t\n\t}\n}\n",
			},
			path:   "events/x.txt",
			needle: "test.1 = {\n\timmediate = {\n\t\t",
			want:   []string{"add_gold", "my_effect"},
		},
		{
			// Grammar row 4: has_variable names an ephemeral value, and offers the names the workspace defines.
			name: "script-name value",
			files: map[string]string{
				"events/a.txt": "test.1 = {\n\timmediate = {\n\t\tset_variable = { name = foo value = 3 }\n\t}\n}\n",
				"events/x.txt": "test.2 = {\n\timmediate = {\n\t\thas_variable = \n\t}\n}\n",
			},
			path:   "events/x.txt",
			needle: "has_variable = ",
			want:   []string{"foo"},
		},
		{
			// A block nothing classifies must not offer nothing.
			name:   "unknown block offers what PMT knows",
			files:  map[string]string{"common/on_action/x.txt": "on_birth = {\n\teffect = {\n\t\tweird = {\n\t\t\t\n\t\t}\n\t}\n}\n"},
			path:   "common/on_action/x.txt",
			needle: "weird = {\n\t\t\t",
			want:   []string{"add_gold"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, root := buildSession(t, "ck3", c.files)
			f := filepath.Join(root, filepath.FromSlash(c.path))
			line, col := cursorAt(c.files[c.path], c.needle)
			if line < 0 {
				t.Fatalf("needle %q not found in %q", c.needle, c.path)
			}
			items := Complete(s, f, line, col).Items
			got := labels(items)
			for _, w := range c.want {
				if !hasComplete(items, w) {
					t.Errorf("missing %q: %v", w, got)
				}
			}
			for _, a := range c.avoid {
				if hasComplete(items, a) {
					t.Errorf("unwanted %q: %v", a, got)
				}
			}
			for label, bucket := range c.bucket {
				for _, it := range items {
					if it.Label == label && !strings.HasPrefix(it.SortText, bucket) {
						t.Errorf("%s sortText=%q, want prefix %q", label, it.SortText, bucket)
					}
				}
			}
			for _, it := range items {
				if it.Documentation != "" {
					t.Errorf("completion item carries a doc, want lazy ResolveDoc: %#v", it)
				}
			}
		})
	}

	// A body offering more than maxComplete items is truncated and marked IsIncomplete.
	t.Run("cap flags IsIncomplete", func(t *testing.T) {
		var macros strings.Builder
		for i := range maxComplete + 20 {
			fmt.Fprintf(&macros, "eff_%03d = { add_gold = 1 }\n", i)
		}
		body := "test.1 = {\n\timmediate = {\n\t\t\n\t}\n}\n"
		s, root := buildSession(t, "ck3", map[string]string{"events/x.txt": body, "common/scripted_effects/m.txt": macros.String()})
		line, col := cursorAt(body, "test.1 = {\n\timmediate = {\n\t\t")
		list := Complete(s, filepath.Join(root, "events", "x.txt"), line, col)
		if len(list.Items) != maxComplete || !list.IsIncomplete {
			t.Fatalf("items = %d incomplete=%v, want %d and true", len(list.Items), list.IsIncomplete, maxComplete)
		}
	})

	// Accepting a citation replaces the whole `prefix:id` expression, not just the part after the colon.
	t.Run("citation replaces the whole cite", func(t *testing.T) {
		const typed = "\t\thas_trait = trait:br"
		body := "ns.1 = {\n\ttrigger = {\n" + typed + "\n\t}\n}\n"
		s, root := buildSession(t, "ck3", map[string]string{"events/x.txt": body})
		line, col := cursorAt(body, typed)
		items := Complete(s, filepath.Join(root, "events", "x.txt"), line, col).Items
		var cite *CompletionItem
		for i, it := range items {
			if it.Label == "trait:brave" {
				cite = &items[i]
			}
		}
		if cite == nil {
			t.Fatalf("no citation item: %v", labels(items))
		}
		if startCol := strings.Index(typed, "trait:br"); cite.Range.Start.Character != startCol || cite.Range.End.Character != len(typed) {
			t.Fatalf("range = %d..%d, want %d..%d", cite.Range.Start.Character, cite.Range.End.Character, startCol, len(typed))
		}
	})
}
