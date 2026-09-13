package lsp

import (
	"path/filepath"
	"strings"
	"testing"

	"paradox-modding-tools/services/internal/lang/declared"
	"paradox-modding-tools/services/internal/lang/derived"
	"paradox-modding-tools/services/internal/lang/session"
)

func TestModifierAreas(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"location, all", "location"},
		{"character and province", "character or province"},
		{"Country, , All,", "country"},
		{"all", ""},
		{"", ""},
		{"state/state_region", "state or state region"},
		{"country, country", "country"},
	}
	for _, c := range cases {
		if got := strings.Join(modifierAreas(c.in), " or "); got != c.want {
			t.Errorf("modifierAreas(%q) = %q want %q", c.in, got, c.want)
		}
	}
}

// CanonPath is for comparison, never for a value anything downstream opens.
// Reindex used to overwrite the path with it, so every definition
// harvested from an open file carried a lower-cased path on Windows. That
// spelling reached the editor, which keys its tabs by exact URI: go-to-
// definition on a symbol in the file already on screen opened a second tab, and
// reveal-in-explorer went quiet because the workbench first asks whether the
// resource sits inside a workspace folder — and the folders are registered with
// their real casing.
func TestResolutionOrder(t *testing.T) {
	want := []string{
		"language-prefix",
		"typed-cite",
		"slot-declared",
		"slot-derived",
		"definition",
		"vocabulary",
		"localization",
		"data-function",
		"macro-kind",
		"field-doc",
		"unresolved",
	}
	if len(wordResolvers) != len(want) {
		t.Fatalf("resolver count = %d, want %d — add it to the list here too",
			len(wordResolvers), len(want))
	}
	for i, w := range want {
		if got := wordResolvers[i].name; got != w {
			t.Errorf("resolver %d = %q, want %q", i, got, w)
		}
	}
}

// TestDeclaredOutranksDerived is the invariant the order exists to hold: what
// the game declares about a slot is consulted before what PMT inferred about
// it. The derived pass carries coverage floors, so it is silent exactly where
// the declared answer was available all along.
func TestDeclaredOutranksDerived(t *testing.T) {
	declared, derived := -1, -1
	for i, r := range wordResolvers {
		switch r.name {
		case "slot-declared":
			declared = i
		case "slot-derived":
			derived = i
		}
	}
	if declared < 0 || derived < 0 {
		t.Fatal("slot resolvers missing")
	}
	if declared > derived {
		t.Errorf("slot-declared at %d ranks below slot-derived at %d", declared, derived)
	}
}

// TestUnresolvedIsLast pins the floor. Every other resolver may decline; this
// one answers for anything left, so a source added below it would never run.
func TestUnresolvedIsLast(t *testing.T) {
	last := wordResolvers[len(wordResolvers)-1]
	if last.name != "unresolved" {
		t.Fatalf("last resolver = %q, want %q", last.name, "unresolved")
	}
	for i, r := range wordResolvers[:len(wordResolvers)-1] {
		if r.name == "unresolved" {
			t.Errorf("unresolved also at %d", i)
		}
	}
}

// TestVocabularyOutranksLocalization pins the fix for the most visible symptom
// of the ordering fault: a declared effect or trigger hovering as the quoted
// player-facing string, because Paradox localizes a great many ordinary words.
func TestVocabularyOutranksLocalization(t *testing.T) {
	vocab, locz := -1, -1
	for i, r := range wordResolvers {
		switch r.name {
		case "vocabulary":
			vocab = i
		case "localization":
			locz = i
		}
	}
	if vocab < 0 || locz < 0 {
		t.Fatal("resolvers missing")
	}
	if vocab > locz {
		t.Errorf("vocabulary at %d ranks below localization at %d", vocab, locz)
	}
}

// covers CanonPath identity on Windows vs Unix.
func TestDottedSegmentAt(t *testing.T) {
	const chain = "holder.culture"
	cases := []struct {
		off  int
		want string
		ok   bool
	}{
		{0, "holder", true},
		{3, "holder", true},
		{6, "holder", true},  // on the dot: the segment that ends there
		{7, "culture", true}, // first char after it
		{11, "culture", true},
		{len(chain), "culture", true},
	}
	for _, c := range cases {
		got, ok := dottedSegmentAt(chain, c.off)
		if ok != c.ok || got != c.want {
			t.Errorf("dottedSegmentAt(%q, %d) = %q,%v want %q,%v",
				chain, c.off, got, ok, c.want, c.ok)
		}
	}
	// Not a chain: leave it to the resolvers that handle whole names.
	if _, ok := dottedSegmentAt("holder", 2); ok {
		t.Error("a plain name is not a chain")
	}
	if _, ok := dottedSegmentAt("holder.culture", 99); ok {
		t.Error("an offset past the end is not a segment")
	}
}

func TestResolutionOrderViaEndToEnd(t *testing.T) {
	body := "namespace = t\n\nt.1 = {\n\ttitle = k.t\n\tadd_gold = 5\n}\n"
	root := writeFiles(t, map[string]string{
		"events/x.txt":                         body,
		"localization/english/a_l_english.yml": "l_english:\n k.t:0 \"Hi\"\n",
	})
	ev := filepath.Join(root, "events", "x.txt")
	cache := &derived.VanillaCache{
		InstallPath: "/game",
		Vocabulary:  []string{"add_gold"},
		Schema: &declared.Schema{
			Effects: map[string]declared.EngineToken{"add_gold": {}},
		},
	}
	s := session.NewWithLoc("ws", "ck3", "english", cache, nil, []derived.ModInput{
		{Origin: "mod", Root: root, Name: "M", Order: 0},
	})

	for _, tt := range []struct {
		name, word, wantVia string
	}{
		// A declared engine token must be claimed by `vocabulary`, the declared
		// tier — not by macro-kind, and not by a hand-written branch that
		// stamps no via at all.
		{"declared effect", "add_gold", "vocabulary"},
		// A harvested object is claimed by `definition`.
		{"harvested event", "t.1", "definition"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			line, col := lineColOf(body, tt.word)
			ins := InspectAt(s, ev, line, col)
			if ins == nil {
				t.Fatalf("no card for %q", tt.word)
			}
			if ins.Via == "" {
				t.Fatalf("%q resolved with no via: it did not go through "+
					"resolveWord, so some other ladder claimed it", tt.word)
			}
			if ins.Via != tt.wantVia {
				t.Errorf("%q claimed by %q, want %q", tt.word, ins.Via, tt.wantVia)
			}
		})
	}
}

// lineColOf is the 0-based line and UTF-8 column of the first occurrence.
