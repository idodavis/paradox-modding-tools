// health.go assembles the Workspace Health payload from Compatibility + Coverage.

package views

import "paradox-modding-tools/services/internal/lang/session"

// HealthLang is per-language loc KPI counts (every language, not just default).
type HealthLang struct {
	Language     string `json:"language"`
	Defined      int    `json:"defined"`
	Missing      int    `json:"missing"`
	Orphaned     int    `json:"orphaned"`
	Untranslated int    `json:"untranslated"`
}

// HealthReport is GetHealth: compatibility + localization rows and counts.
type HealthReport struct {
	Conflicts    int          `json:"conflicts"`
	Overrides    int          `json:"overrides"`
	Depends      int          `json:"depends"`
	Dangling     int          `json:"dangling"`
	Missing      int          `json:"missing"`
	Orphaned     int          `json:"orphaned"`
	Untranslated int          `json:"untranslated"`
	Languages    []HealthLang `json:"languages"`
	Rows         []HealthRow  `json:"rows"`
}

// Health returns grouped compatibility and loc rows. Order is contests only.
func Health(s *session.Session, order []string) HealthReport {
	out := Compatibility(s, order)
	langs, locRows := Coverage(s)
	out.Languages = langs
	for _, l := range langs {
		out.Missing += l.Missing
		out.Orphaned += l.Orphaned
		out.Untranslated += l.Untranslated
	}
	out.Rows = append(out.Rows, locRows...)
	return out
}
