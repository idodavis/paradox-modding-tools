// codes.go is the registry of diagnostic codes: what each one is called, what
// it says, and whether it is on by default.
//
// A registry rather than a scattering of string literals, because three things
// need the same list — the settings surface that toggles a code, the
// `# pmt:ignore` documentation, and the sweep that counts findings per code. A
// code that exists in only one of those is a code someone cannot turn off.

package lsp

import (
	"slices"
	"strings"
)

// Severity levels a code may be set to.
const (
	SevOff     = "off"
	SevWarning = "warning"
	SevError   = "error"
)

// DiagnosticCode describes one check for the settings surface.
type DiagnosticCode struct {
	Code    string `json:"code"`
	Summary string `json:"summary"`
	// Default is the severity when the user has not chosen one.
	Default string `json:"default"`
	// Experimental marks a check whose ship condition — zero findings across
	// all three vanilla installs — has not been demonstrated yet. Off by
	// default: a check that cries wolf teaches a modder to ignore the panel,
	// which costs more than the check is worth.
	Experimental bool `json:"experimental,omitempty"`
}

// diagnosticCodes is the full list, ordered as the settings surface shows them.
var diagnosticCodes = []DiagnosticCode{
	// Experimental: fires 1,614 times across 398 vanilla CK3 files. Several
	// kinds harvest nested block keys as definitions -- every activity type
	// carries `special_guests` -- so the check cannot yet tell the name of a
	// thing from the name of a field. Off until the vanilla gate is clean.
	{Code: "duplicate-definition", Default: SevWarning, Experimental: true,
		Summary: "One key defined twice inside a single mod; load order decides which survives."},
	{Code: "loc-header-mismatch", Default: SevWarning,
		Summary: "A localization file's l_<lang>: header disagrees with its folder."},
	{Code: "missing-bom", Default: SevWarning,
		Summary: "A localization file without the UTF-8 BOM the games require."},
	{Code: "missing-descriptor", Default: SevWarning,
		Summary: "A mod with no descriptor.mod."},
	{Code: "missing-required-loc", Default: SevWarning,
		Summary: "A key the game will look for that nothing defines."},
	{Code: "required-loc", Default: SevWarning,
		Summary: "A definition missing localization its kind conventionally has."},
	{Code: "unknown-event", Default: SevWarning,
		Summary: "An event fired or called that no file defines."},
	{Code: "unresolved-ref", Default: SevWarning,
		Summary: "A citation naming a database PMT holds, and a key nothing defines."},
	{Code: "wrong-localization-folder", Default: SevWarning,
		Summary: "A localization file under the wrong language folder."},
	{Code: "wrong-scope", Default: SevWarning,
		Summary: "An effect or trigger used in a scope the game does not allow."},
	{Code: "wrong-value-type", Default: SevWarning,
		Summary: "A value contradicting the argument type the game declares for that key."},
}

// DiagnosticCodes returns the registry, for the settings surface.
func DiagnosticCodes() []DiagnosticCode {
	return slices.Clone(diagnosticCodes)
}

// severityOf maps a configured severity name to an LSP level, or 0 for off.
func severityOf(name string) int {
	switch strings.ToLower(name) {
	case SevOff:
		return 0
	case SevError:
		return sevError
	case SevWarning:
		return sevWarning
	}
	return -1 // not configured
}

// ApplySeverities drops diagnostics whose code is turned off and re-levels the
// rest, using settings keyed "<code>" -> "off" | "warning" | "error".
//
// Applied after Diagnose rather than inside it so a toggle takes effect on the
// next keystroke with no rescan, and so the checks themselves stay ignorant of
// configuration — a check that reads settings is a check that can be wrong in
// two ways.
func ApplySeverities(ds []Diagnostic, settings map[string]string) []Diagnostic {
	defaults := map[string]string{}
	for _, c := range diagnosticCodes {
		if c.Experimental {
			defaults[c.Code] = SevOff
		}
	}
	out := ds[:0]
	for _, d := range ds {
		want, ok := settings[d.Code]
		if !ok {
			want = defaults[d.Code]
		}
		if want == "" {
			out = append(out, d)
			continue
		}
		lvl := severityOf(want)
		if lvl == 0 {
			continue
		}
		if lvl > 0 {
			d.Severity = lvl
		}
		out = append(out, d)
	}
	return out
}
