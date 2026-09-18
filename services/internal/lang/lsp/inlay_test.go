// inlay_test.go covers the era hints, and above all the case where there are none.
package lsp

import (
	"strings"
	"testing"

	"paradox-modding-tools/services/internal/lang/index"
)

const eraSrc = "namespace = agot\n\nagot.1 = {\n\ttrigger = {\n\t\tgame_start_date >= 8082.1.1\n\t}\n" +
	"\timmediate = {\n\t\tset_variable = { name = x value = 5 }\n\t}\n}\n"

func TestInlay(t *testing.T) {
	t.Run("silent without a calendar", func(t *testing.T) {
		s, root := buildSession(t, "ck3", map[string]string{"events/agot_events.txt": eraSrc})
		if got := InlayHints(s, root+"/events/agot_events.txt"); len(got) != 0 {
			t.Fatalf("hints without a declared calendar: %+v", got)
		}
	})
	withCalendar(t, index.ModCalendar{Era: "AC", EpochOffset: 7782})
	s, root := buildSession(t, "ck3", map[string]string{
		"events/agot_events.txt": eraSrc,
		// A history file keys its block BY a date, which is the one a reader most needs converted.
		"history/characters/agot.txt": "1000 = {\n\t8082.1.1 = {\n\t\tbirth = yes\n\t}\n}\n",
	})
	for _, rel := range []string{"events/agot_events.txt", "history/characters/agot.txt"} {
		got := InlayHints(s, root+"/"+rel)
		found := false
		for _, h := range got {
			found = found || strings.TrimSpace(h.Label) == "300.1.1 AC"
			if strings.Contains(h.Label, "-") {
				t.Errorf("negative era year rendered: %q", h.Label)
			}
		}
		if !found {
			t.Fatalf("%s: no hint labelled 300.1.1 AC in %+v", rel, got)
		}
	}
	// A bare number and a quoted string are not dates: only the one date in eraSrc earns a hint.
	if got := InlayHints(s, root+"/events/agot_events.txt"); len(got) != 1 {
		t.Fatalf("got %d hints, want exactly the one date: %+v", len(got), got)
	}
}
