// inlay_test.go covers the era hints, and above all the case where there are
// none: a mod that declares no calendar must produce no hint anywhere.

package lsp

import (
	"strings"
	"testing"

	"paradox-modding-tools/services/internal/catalog"
)

const eraSrc = "namespace = agot\n\nagot.1 = {\n" +
	"\ttrigger = {\n\t\tgame_start_date >= 8082.1.1\n\t}\n" +
	"\timmediate = {\n\t\tset_variable = { name = x value = 5 }\n\t}\n}\n"

// TestInlayHintsSilentWithoutCalendar is the guardrail, and it is the whole
// reason the feature is opt-in: with nothing declared, nothing anywhere changes.
func TestInlayHintsSilentWithoutCalendar(t *testing.T) {
	s, root := buildSession(t, "ck3", map[string]string{
		"events/agot_events.txt": eraSrc,
	}, nil, nil)
	path := root + "/events/agot_events.txt"
	s.DidOpen(path, s.FileText(path))
	if got := InlayHints(s, path); len(got) != 0 {
		t.Fatalf("hints without a declared calendar: %+v", got)
	}
}

func TestInlayHintsRenderEra(t *testing.T) {
	withCalendar(t, catalog.ModCalendar{Era: "AC", EpochOffset: 7782})
	s, root := buildSession(t, "ck3", map[string]string{
		"events/agot_events.txt": eraSrc,
		// A history file keys its block BY a date, which is the one a reader
		// most needs converted.
		"history/characters/agot.txt": "1000 = {\n\t8082.1.1 = {\n\t\tbirth = yes\n\t}\n}\n",
	}, nil, nil)

	for _, tt := range []struct{ rel, want string }{
		{"events/agot_events.txt", "300.1.1 AC"},
		{"history/characters/agot.txt", "300.1.1 AC"},
	} {
		t.Run(tt.rel, func(t *testing.T) {
			path := root + "/" + tt.rel
			s.DidOpen(path, s.FileText(path))
			got := InlayHints(s, path)
			if len(got) == 0 {
				t.Fatalf("no hints")
			}
			var labels []string
			for _, h := range got {
				labels = append(labels, strings.TrimSpace(h.Label))
			}
			found := false
			for _, l := range labels {
				if l == tt.want {
					found = true
				}
			}
			if !found {
				t.Fatalf("labels = %v, want one %q", labels, tt.want)
			}
			// A year that would land on or before zero is not a date in this
			// era, and EraDate refuses it rather than printing a negative year.
			for _, h := range got {
				if strings.Contains(h.Label, "-") {
					t.Errorf("negative era year rendered: %q", h.Label)
				}
			}
		})
	}
}

// TestInlayHintsSkipNonDates: a bare number and a quoted string are not dates,
// and 5 in `value = 5` must not be hinted.
func TestInlayHintsSkipNonDates(t *testing.T) {
	withCalendar(t, catalog.ModCalendar{Era: "AC", EpochOffset: 7782})
	s, root := buildSession(t, "ck3", map[string]string{
		"events/agot_events.txt": eraSrc,
	}, nil, nil)
	path := root + "/events/agot_events.txt"
	s.DidOpen(path, s.FileText(path))
	got := InlayHints(s, path)
	if len(got) != 1 {
		t.Fatalf("got %d hints, want exactly the one date: %+v", len(got), got)
	}
}
