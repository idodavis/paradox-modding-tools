// inlay_test.go covers the era hints, and above all the case where there are
// none: a mod that declares no calendar must produce no hint anywhere.
package lsp

import (
	"strings"
	"testing"

	"paradox-modding-tools/services/internal/lang/derived"
)

const eraSrc = "namespace = agot\n\nagot.1 = {\n" +
	"\ttrigger = {\n\t\tgame_start_date >= 8082.1.1\n\t}\n" +
	"\timmediate = {\n\t\tset_variable = { name = x value = 5 }\n\t}\n}\n"

func TestInlay(t *testing.T) {
	// The guardrail, and the whole reason the feature is opt-in: with nothing
	// declared, nothing anywhere changes.
	t.Run("silent without a calendar", func(t *testing.T) {
		s, root := buildSession(t, "ck3", map[string]string{
			"events/agot_events.txt": eraSrc,
		}, nil, nil)
		path := root + "/events/agot_events.txt"
		if got := InlayHints(s, path); len(got) != 0 {
			t.Fatalf("hints without a declared calendar: %+v", got)
		}
	})

	withCalendar(t, derived.ModCalendar{Era: "AC", EpochOffset: 7782})
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
			got := InlayHints(s, path)
			if len(got) == 0 {
				t.Fatal("no hints")
			}
			found := false
			for _, h := range got {
				if strings.TrimSpace(h.Label) == tt.want {
					found = true
				}
				if strings.Contains(h.Label, "-") {
					t.Errorf("negative era year rendered: %q", h.Label)
				}
			}
			if !found {
				t.Fatalf("no hint labelled %q", tt.want)
			}
		})
	}

	// A bare number and a quoted string are not dates: only the one date in
	// eraSrc earns a hint, not value = 5.
	t.Run("skips non-dates", func(t *testing.T) {
		got := InlayHints(s, root+"/events/agot_events.txt")
		if len(got) != 1 {
			t.Fatalf("got %d hints, want exactly the one date: %+v", len(got), got)
		}
	})
}
