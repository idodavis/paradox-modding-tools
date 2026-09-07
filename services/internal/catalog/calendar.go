// calendar.go converts a script date literal into the era a mod's players see.
//
// A reading aid and nothing more. Nothing here is ever written back to a file:
// the engine needs the offset year to do its arithmetic, so the script is
// correct as it stands and only the human reading it needs help.

package catalog

import (
	"strconv"
	"strings"
)

// EraDate renders a Clausewitz date in the mod's era, or empty when there is
// nothing to say.
//
// Empty for an undeclared calendar, a non-date, or a year the offset would push
// to zero or below — a conversion that produces "year -12" is worse than no
// conversion at all.
func EraDate(date string, cal ModCalendar) string {
	if !cal.Declared() {
		return ""
	}
	parts := strings.Split(date, ".")
	if len(parts) < 3 || len(parts) > 4 {
		return ""
	}
	year, err := strconv.Atoi(parts[0])
	if err != nil {
		return ""
	}
	era := year - cal.EpochOffset
	if era <= 0 {
		return ""
	}
	out := strconv.Itoa(era) + "." + strings.Join(parts[1:], ".")
	if cal.Era != "" {
		out += " " + cal.Era
	}
	return out
}
