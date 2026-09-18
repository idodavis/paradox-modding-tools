// complete_test.go covers completion: which items each block type offers.

package lsp

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"paradox-modding-tools/services/internal/lang/declared"
	"paradox-modding-tools/services/internal/lang/derived"
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

// cursorAt returns the line/UTF-8 column right after needle's last occurrence
// in body, so a needle can end exactly where the cursor belongs.
func cursorAt(body, needle string) (int, int) {
	i := strings.Index(body, needle)
	if i < 0 {
		return -1, -1
	}
	end := i + len(needle)
	line := strings.Count(body[:end], "\n")
	col := end - (strings.LastIndex(body[:end], "\n") + 1)
	return line, col
}

func TestComplete(t *testing.T) {
	cases := []struct {
		name   string
		files  map[string]string
		cache  *vanillaCache
		vloc   *vanillaLoc
		path   string
		needle string
		want   []string
		avoid  []string
		bucket map[string]string // label -> expected leading SortText digit
	}{
		{
			// A body's own declared tokens, not another kind's structure keys.
			name: "body trigger",
			files: map[string]string{
				"events/x.txt": "test.1 = {\n\ttrigger = {\n\t\t\n\t}\n}\n" +
					"test.2 = {\n\ttrigger = {\n\t\tis_adult = yes\n\t}\n}\n",
			},
			cache: &vanillaCache{Schema: &declared.Schema{
				Triggers: map[string]declared.EngineToken{"is_adult": {}},
			}},
			path:   "events/x.txt",
			needle: "test.1 = {\n\ttrigger = {\n\t\t",
			want:   []string{"is_adult"},
			avoid:  []string{"add_gold"},
		},
		{
			// A token in scope sorts bucket 0; the rest still offer, bucket 2.
			name: "body effect with scope",
			files: map[string]string{
				"common/on_action/x.txt": "my_on_action = {\n\teffect = {\n\t\t\n\t}\n}\n" +
					"my_on_action2 = {\n\teffect = { add_gold = 5 }\n}\n",
			},
			cache: &vanillaCache{
				Schema: &declared.Schema{
					Effects: map[string]declared.EngineToken{
						"add_gold":      {In: []string{"character"}},
						"add_stability": {In: []string{"country"}},
					},
					OnActions: map[string]string{"my_on_action": "character", "my_on_action2": "character"},
				},
				// The install's own evidence that "effect" is a body: the scope
				// walk trusts only what the install left, never a mod's own script.
				NestedMembers: map[string][]string{"effect": {"add_gold"}},
			},
			path:   "common/on_action/x.txt",
			needle: "my_on_action = {\n\teffect = {\n\t\t",
			want:   []string{"add_gold", "add_stability"},
			bucket: map[string]string{"add_gold": "0", "add_stability": "2"},
		},
		{
			// select_trigger's case: a structure unifies across every position
			// its key is written at, so a member written elsewhere still shows.
			name: "structure members incl. a member written at another position",
			files: map[string]string{
				"events/x.txt": "test.1 = {\n\tcooldown = {\n\t\tdays = 10\n\t}\n}\n" +
					"test.2 = {\n\textra = {\n\t\tcooldown = {\n\t\t\tmonths = 2\n\t\t}\n\t}\n}\n" +
					"test.3 = {\n\tcooldown = {\n\t\t\n\t}\n}\n",
			},
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
			want:   []string{"trait_a", "trait_b"},
		},
		{
			// A scalar value position offers definitions of the cell's resolved kind.
			name: "value position by cell",
			files: map[string]string{
				"common/event_themes/00.txt": "seduction = { }\n",
				"events/x.txt": "test.1 = {\n\ttheme = seduction\n}\n" +
					"test.2 = {\n\ttheme = s\n}\n",
			},
			path:   "events/x.txt",
			needle: "test.2 = {\n\ttheme = s",
			want:   []string{"seduction"},
		},
		{
			// A cell classified loc offers loc keys, not definitions.
			name:  "loc key",
			files: map[string]string{"events/x.txt": "test.1 = {\n\ttitle = te\n}\n"},
			cache: &vanillaCache{LocFields: []string{"title"}},
			vloc: &vanillaLoc{Sites: map[string]derived.LocEntry{
				"test.1.t": {Value: "Hi"}, "other": {},
			}},
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
				"events/x.txt": "test.1 = {\n\timmediate = {\n\t\t\n\t}\n}\n" +
					"test.2 = {\n\timmediate = { add_gold = 5 }\n}\n",
			},
			cache: &vanillaCache{Schema: &declared.Schema{
				Effects: map[string]declared.EngineToken{"add_gold": {}},
			}},
			path:   "events/x.txt",
			needle: "test.1 = {\n\timmediate = {\n\t\t",
			want:   []string{"add_gold", "my_effect"},
		},
		{
			// Grammar row 4 (jomini.ScriptName): has_variable names an ephemeral
			// value, and offers the variable names the workspace defines for it.
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
			// A missing type system may narrow what PMT knows, never hide what it
			// does know: with no declared schema at all, the corpus vocabulary
			// still completes, unfiltered by scope.
			name: "no-schema fallback",
			files: map[string]string{
				"common/on_action/x.txt": "on_birth = {\n\teffect = {\n\t\t\n\t}\n}\n",
			},
			cache:  &vanillaCache{Vocabulary: []string{"add_gold", "add_title_law"}},
			path:   "common/on_action/x.txt",
			needle: "on_birth = {\n\teffect = {\n\t\t",
			want:   []string{"add_gold", "add_title_law"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, root := buildSession(t, "ck3", c.files, c.cache, c.vloc)
			f := filepath.Join(root, filepath.FromSlash(c.path))
			body := c.files[c.path]
			line, col := cursorAt(body, c.needle)
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

	// The single cap: a body offering more than maxComplete declared tokens is
	// truncated and marked IsIncomplete, so the editor asks again as the reader
	// keeps typing.
	t.Run("cap flags IsIncomplete", func(t *testing.T) {
		effects := map[string]declared.EngineToken{}
		for i := range maxComplete + 20 {
			effects[fmt.Sprintf("eff_%03d", i)] = declared.EngineToken{}
		}
		body := "test.1 = {\n\timmediate = {\n\t\t\n\t}\n}\n" +
			"test.2 = {\n\timmediate = { eff_000 = yes }\n}\n"
		s, root := buildSession(t, "ck3", map[string]string{"events/x.txt": body},
			&vanillaCache{Schema: &declared.Schema{Effects: effects}}, nil)
		f := filepath.Join(root, "events", "x.txt")
		line, col := cursorAt(body, "test.1 = {\n\timmediate = {\n\t\t")
		list := Complete(s, f, line, col)
		if len(list.Items) != maxComplete {
			t.Fatalf("items = %d, want %d", len(list.Items), maxComplete)
		}
		if !list.IsIncomplete {
			t.Fatal("want IsIncomplete")
		}
	})

	// Accepting a citation replaces the whole `prefix:id` expression, not just
	// the part after the colon: the word scan stops at `:` while the lexer does
	// not, so a naive range left `trait:trait:brave` behind.
	t.Run("citation replaces the whole cite", func(t *testing.T) {
		cache := &vanillaCache{
			PrefixKinds:     map[string]string{"trait": "traits"},
			FieldValueKinds: map[string]string{"this": "traits"},
			Defs:            []derived.Def{{Kind: "traits", Key: "brave", Path: "t.txt"}},
		}
		const typed = "\tthis = trait:br"
		body := "a = {\n" + typed + "\n}\n"
		s, root := buildSession(t, "ck3", map[string]string{
			"common/scripted_effects/e.txt": body,
		}, cache, nil)
		f := filepath.Join(root, "common", "scripted_effects", "e.txt")
		line, col := cursorAt(body, typed)
		items := Complete(s, f, line, col).Items
		var cite *CompletionItem
		for i, it := range items {
			if it.Label == "trait:brave" {
				cite = &items[i]
			}
		}
		if cite == nil {
			t.Fatalf("no citation item: %v", labels(items))
		}
		startCol := strings.Index(typed, "trait:br")
		if cite.Range.Start.Character != startCol || cite.Range.End.Character != len(typed) {
			t.Fatalf("range = %d..%d, want %d..%d", cite.Range.Start.Character, cite.Range.End.Character,
				startCol, len(typed))
		}
	})
}
