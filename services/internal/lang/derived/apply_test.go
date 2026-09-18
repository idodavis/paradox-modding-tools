package derived

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/jomini"
)

// applyOf is Phase A over one testdata file, with the rel path deciding its mode.
func applyOf(t *testing.T, name, rel string) *FileHarvest {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "apply", name))
	if err != nil {
		t.Fatal(err)
	}
	text, _ := jomini.Decode(raw)
	text = jomini.Normalize(text)
	id := fileID{GameID: "ck3", Path: name, Rule: game.MatchExtract("ck3", rel)}
	if id.Rule.Mode == game.ModeLocKey {
		return ApplyLoc(id, text)
	}
	return Apply(FactsOf("ck3", name, rel, "", jomini.Parse(text)), nil)
}

// TestApplyRows is the golden for the fixed grammar rows, one fixture per mode.
func TestApplyRows(t *testing.T) {
	for _, tt := range []struct {
		name, rel   string
		defs, refs  []string
		cells       []string
		notCells    []string
		wantWrapper bool
	}{
		{
			name: "traits.txt", rel: `common/traits/traits.txt`,
			// Row 1 and row 10: a top-level block is a definition and the comment above it is its doc.
			defs: []string{"traits/brave", "traits/craven"},
			// Row 8's context: a member sits at the empty path, a member of a block one level down.
			cells:    []string{"traits||category", "traits|portrait|trigger"},
			notCells: []string{"traits|<top>|brave", "traits||brave"},
		},
		{
			name: "events.txt", rel: `events/events.txt`,
			// Rows 2 to 5: the namespace, the event ids, the inline declaration keyword, the script-name table and `$PARAM$`.
			defs: []string{
				"namespace/test", "event/test.1", "event/test.2",
				"saved_scope/duel_target", "scripted_effect/my_inline",
				"script_param/AMOUNT",
			},
			refs:  []string{"namespace/test", "script_param/AMOUNT"},
			cells: []string{"event||type", "event|immediate|trigger_event", "event|immediate.trigger_event|id"},
		},
		{
			name: "window.gui", rel: `gui/window.gui`,
			// Row 3 in a .gui file, which carries no prose.
			defs:  []string{"gui_type/my_window", "gui_type/my_tpl"},
			cells: []string{"gui_type||size", "gui_type|widget|name"},
		},
		{
			name: "strings_l_english.yml", rel: `localization/english/strings_l_english.yml`,
			// The localization rows: one entry is a value, a `$key$` interpolation a reference.
			refs: []string{"loc/trait_brave_desc"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := applyOf(t, tt.name, tt.rel)
			var defs, refs []string
			for _, d := range h.Defs {
				defs = append(defs, jomini.CanonicalKind(d.Kind)+"/"+d.Key)
			}
			for _, r := range h.Refs {
				refs = append(refs, r.Kind+"/"+r.Key)
			}
			for _, want := range tt.defs {
				if !slices.Contains(defs, want) {
					t.Errorf("missing def %s in %v", want, defs)
				}
			}
			for _, want := range tt.refs {
				if !slices.Contains(refs, want) {
					t.Errorf("missing ref %s in %v", want, refs)
				}
			}
			if h.recs == nil {
				if len(tt.cells) > 0 {
					t.Fatal("no records")
				}
				if h.Loc.Vals["test.1.t"].Value == "" {
					t.Errorf("localization entry missing: %v", h.Loc)
				}
				return
			}
			h.recs.boot(nil, func(string) bool { return false })
			h.recs.settle(nil, nil)
			got := map[string]bool{}
			for i := range h.recs.occs {
				o := &h.recs.occs[i]
				got[h.recs.kind+"|"+cellPathAt(h.recs.paths, o.node, o.flags&oTop != 0)+"|"+o.key] = true
			}
			for i := range h.recs.tallies {
				ta := &h.recs.tallies[i]
				got[h.recs.kind+"|"+cellPathAt(h.recs.paths, ta.node, ta.top)+"|"+ta.key] = true
			}
			for _, want := range tt.cells {
				if !got[want] {
					t.Errorf("missing cell %s", want)
				}
			}
			for _, bad := range tt.notCells {
				if got[bad] {
					t.Errorf("cell %s should not exist", bad)
				}
			}
		})
	}
}

// TestApplyDocAndValue covers row 10 and the script-name body a hover reads.
func TestApplyDocAndValue(t *testing.T) {
	h := applyOf(t, "traits.txt", `common/traits/traits.txt`)
	for _, d := range h.Defs {
		if d.Key == "brave" && !strings.Contains(d.Doc, "brave character") {
			t.Errorf("brave doc = %q", d.Doc)
		}
	}
	e := applyOf(t, "events.txt", `events/events.txt`)
	for _, d := range e.Defs {
		if d.Kind == "saved_scope" && d.Key != "duel_target" {
			t.Errorf("saved scope = %q", d.Key)
		}
	}
}
