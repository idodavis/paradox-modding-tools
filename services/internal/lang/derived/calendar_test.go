// calendar_test.go covers the mod era conversion.

package derived

import "testing"

// TestEraDate uses A Game of Thrones' real offset: it writes start_date =
// 8082.1.1 for what the game shows as 300 AC.
func TestEraDate(t *testing.T) {
	agot := ModCalendar{Era: "AC", EpochOffset: 7782}
	cases := []struct{ in, want string }{
		{"8082.1.1", "300.1.1 AC"},
		{"8209.2.1", "427.2.1 AC"},
		{"8209.2.1.12", "427.2.1.12 AC"},
		// Below the epoch: "year -12" is worse than no conversion.
		{"7000.1.1", ""},
		{"7782.1.1", ""},
		// Not a date.
		{"brave", ""},
		{"ns.1", ""},
		{"100", ""},
	}
	for _, c := range cases {
		if got := EraDate(c.in, agot); got != c.want {
			t.Errorf("EraDate(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestEraDateUndeclaredIsSilent is the guardrail for the whole feature: with no
// calendar declared, nothing anywhere changes. Vanilla CK3's 867 needs no help,
// and guessing an offset from bookmark dates would be a guess dressed as a fact.
func TestEraDateUndeclaredIsSilent(t *testing.T) {
	if got := EraDate("867.1.1", ModCalendar{}); got != "" {
		t.Errorf("EraDate with no calendar = %q, want empty", got)
	}
	if (ModCalendar{}).Declared() {
		t.Error("an empty calendar must not read as declared")
	}
	// An era with no offset still counts: a mod may simply want its years
	// labelled.
	if got := EraDate("300.1.1", ModCalendar{Era: "AC"}); got != "300.1.1 AC" {
		t.Errorf("EraDate = %q, want the year labelled", got)
	}
}
