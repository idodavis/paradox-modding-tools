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

// Groups a code belongs to, for the settings surface.
const (
	// GroupCheck judges script against what the game declares.
	GroupCheck = "check"
	// GroupParse reports that a file does not parse. Distinct because the
	// finding is about syntax rather than meaning, and silencing one hides a
	// file the game itself cannot read.
	GroupParse = "parse"
)

// DiagnosticCode describes one check for the settings surface.
type DiagnosticCode struct {
	Code    string `json:"code"`
	Summary string `json:"summary"`
	// Group separates the semantic checks from the parse errors, which the
	// settings surface shows apart because silencing a parse error hides a file
	// the game cannot read at all.
	Group string `json:"group"`
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
	{Code: "duplicate-definition", Default: SevWarning, Group: GroupCheck,
		Summary: "One key defined twice inside a single mod; load order decides which survives."},
	{Code: "loc-header-mismatch", Default: SevWarning, Group: GroupCheck,
		Summary: "A localization file's l_<lang>: header disagrees with its folder."},
	{Code: "missing-bom", Default: SevWarning, Group: GroupCheck,
		Summary: "A localization file without the UTF-8 BOM the games require."},
	{Code: "missing-descriptor", Default: SevWarning, Group: GroupCheck,
		Summary: "A mod with no descriptor.mod."},
	{Code: "missing-required-loc", Default: SevWarning, Group: GroupCheck,
		Summary: "A key the game will look for that nothing defines."},
	{Code: "required-loc", Default: SevWarning, Group: GroupCheck,
		Summary: "A definition missing localization its kind conventionally has."},
	{Code: "unknown-event", Default: SevWarning, Group: GroupCheck,
		Summary: "An event fired or called that no file defines."},
	{Code: "unknown-key", Default: SevWarning, Group: GroupCheck,
		Summary: "A key that is not a member of the block it sits in."},
	{Code: "unknown-token", Default: SevOff, Experimental: true, Group: GroupCheck,
		Summary: "An effect or trigger neither declared by the game nor defined as a scripted macro."},
	{Code: "unresolved-ref", Default: SevWarning, Group: GroupCheck,
		Summary: "A citation naming a database PMT holds, and a key nothing defines."},
	{Code: "wrong-declared-value", Default: SevOff, Experimental: true, Group: GroupCheck,
		Summary: "A value outside the closed set the game documents for that field."},
	{Code: "wrong-localization-folder", Default: SevWarning, Group: GroupCheck,
		Summary: "A localization file under the wrong language folder."},
	{Code: "wrong-scope", Default: SevWarning, Group: GroupCheck,
		Summary: "An effect or trigger used in a scope the game does not allow."},
	{Code: "wrong-value-type", Default: SevWarning, Group: GroupCheck,
		Summary: "A value contradicting the argument type the game declares for that key."},
	// Parse errors. The parser owns their codes -- lsp/diagnostics.go emits
	// `string(e.Code)` for script and `"loc-"+string(e.Code)` for localization
	// -- but they reach ApplySeverities like any other, so leaving them out of
	// the registry made them settable and not un-settable: a severity written
	// for one had nothing in the UI that could list or clear it.
	//
	// Default records what the parser emits, which ApplySeverities preserves by
	// leaving an unconfigured code alone. Listing them is what makes the
	// override visible, not what creates it.
	{Code: "missing-value", Default: SevError, Group: GroupParse,
		Summary: "A key with `=` and nothing after it."},
	{Code: "stray-close", Default: SevError, Group: GroupParse,
		Summary: "A closing brace with no block open."},
	{Code: "unclosed-brace", Default: SevError, Group: GroupParse,
		Summary: "A block opened and never closed."},
	{Code: "unterminated-string", Default: SevError, Group: GroupParse,
		Summary: "A quoted value with no closing quote, recovered at end of line."},
	{Code: "loc-bad-entry", Default: SevWarning, Group: GroupParse,
		Summary: "A localization line that is not `key:0 \"value\"`."},
	{Code: "loc-content-before-header", Default: SevError, Group: GroupParse,
		Summary: "Localization entries above the l_<lang>: header, which the game ignores."},
	{Code: "loc-no-header", Default: SevError, Group: GroupParse,
		Summary: "A localization file with no l_<lang>: header."},
	{Code: "loc-tab-indent", Default: SevWarning, Group: GroupParse,
		Summary: "A localization line indented with a tab; the games want spaces."},
	{Code: "loc-unterminated-value", Default: SevError, Group: GroupParse,
		Summary: "A localization value with no closing quote."},
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
		// An empty stored value means the user cleared their choice, and it has
		// to resolve exactly as an absent one. SaveSettings can only write keys,
		// never remove them, so "" is the only way the settings surface can say
		// "back to the default" -- and letting it mean "keep whatever severity
		// the check emitted" would have switched every experimental code ON the
		// moment somebody pressed Reset to defaults.
		if !ok || want == "" {
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
