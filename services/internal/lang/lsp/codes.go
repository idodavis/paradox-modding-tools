// codes.go is the registry of diagnostic codes: what each one is called, what
// it says, and whether it is on by default.
//
// A registry rather than a scattering of string literals, because three things
// need the same list — the settings surface that toggles a code, the
// `# pmt:ignore` documentation, and the sweep that counts findings per code. A
// code that exists in only one of those is a code someone cannot turn off.

package lsp

import "slices"

// Groups a code belongs to, for the settings surface.
const (
	// GroupCheck judges script against what the game declares.
	GroupCheck = "check"
	// GroupParse reports that a file does not parse. Distinct because the
	// finding is about syntax rather than meaning, and silencing one hides a
	// file the game itself cannot read.
	GroupParse = "parse"
)

// SettingOn and SettingOff are the two stored values a code's setting may hold.
const (
	SettingOn  = "on"
	SettingOff = "off"
)

// DiagnosticCode describes one check for the settings surface.
type DiagnosticCode struct {
	Code    string `json:"code"`
	Summary string `json:"summary"`
	// Group separates the semantic checks from the parse errors, which the
	// settings surface shows apart because silencing a parse error hides a file
	// the game cannot read at all.
	Group string `json:"group"`
	// Default is whether the check runs when the user has not chosen.
	Default bool `json:"default"`
}

// diagnosticCodes is the full list, ordered as the settings surface shows them.
var diagnosticCodes = []DiagnosticCode{
	{Code: "duplicate-definition", Default: true, Group: GroupCheck,
		Summary: "One key defined twice inside a single mod; load order decides which survives."},
	{Code: "loc-header-mismatch", Default: true, Group: GroupCheck,
		Summary: "A localization file's l_<lang>: header disagrees with its folder."},
	{Code: "missing-bom", Default: true, Group: GroupCheck,
		Summary: "A localization file without the UTF-8 BOM the games require."},
	{Code: "missing-descriptor", Default: true, Group: GroupCheck,
		Summary: "A mod with no descriptor.mod."},
	{Code: "missing-required-loc", Default: true, Group: GroupCheck,
		Summary: "A key the game will look for that nothing defines."},
	{Code: "required-loc", Default: true, Group: GroupCheck,
		Summary: "A definition missing localization its kind conventionally has."},
	{Code: "unknown-event", Default: true, Group: GroupCheck,
		Summary: "An event fired or called that no file defines."},
	{Code: "unknown-key", Default: true, Group: GroupCheck,
		Summary: "A key that is not a member of the block it sits in."},
	{Code: "unresolved-ref", Default: true, Group: GroupCheck,
		Summary: "A citation naming a database PMT holds, and a key nothing defines."},
	{Code: "wrong-localization-folder", Default: true, Group: GroupCheck,
		Summary: "A localization file under the wrong language folder."},
	{Code: "wrong-scope", Default: true, Group: GroupCheck,
		Summary: "An effect or trigger used in a scope the game does not allow."},
	{Code: "wrong-value-type", Default: true, Group: GroupCheck,
		Summary: "A value contradicting the argument type the game declares for that key."},
	{Code: "missing-value", Default: true, Group: GroupParse,
		Summary: "A key with `=` and nothing after it."},
	{Code: "stray-close", Default: true, Group: GroupParse,
		Summary: "A closing brace with no block open."},
	{Code: "unclosed-brace", Default: true, Group: GroupParse,
		Summary: "A block opened and never closed."},
	{Code: "unterminated-string", Default: true, Group: GroupParse,
		Summary: "A quoted value with no closing quote, recovered at end of line."},
	{Code: "loc-bad-entry", Default: true, Group: GroupParse,
		Summary: "A localization line that is not `key:0 \"value\"`."},
	{Code: "loc-content-before-header", Default: true, Group: GroupParse,
		Summary: "Localization entries above the l_<lang>: header, which the game ignores."},
	{Code: "loc-no-header", Default: true, Group: GroupParse,
		Summary: "A localization file with no l_<lang>: header."},
	{Code: "loc-tab-indent", Default: true, Group: GroupParse,
		Summary: "A localization line indented with a tab; the games want spaces."},
	{Code: "loc-unterminated-value", Default: true, Group: GroupParse,
		Summary: "A localization value with no closing quote."},
}

// DiagnosticCodes returns the registry, for the settings surface.
func DiagnosticCodes() []DiagnosticCode {
	return slices.Clone(diagnosticCodes)
}

// EnabledCodes resolves each code's on/off state: the stored setting, or the
// code's own default when the user has not chosen one.
func EnabledCodes(settings map[string]string) map[string]bool {
	out := make(map[string]bool, len(diagnosticCodes))
	for _, c := range diagnosticCodes {
		on := c.Default
		if v, ok := settings[c.Code]; ok && v != "" {
			on = v == SettingOn
		}
		out[c.Code] = on
	}
	return out
}

// ApplyEnabled drops diagnostics whose code the user has switched off.
func ApplyEnabled(ds []Diagnostic, enabled map[string]bool) []Diagnostic {
	out := ds[:0]
	for _, d := range ds {
		if on, ok := enabled[d.Code]; ok && !on {
			continue
		}
		out = append(out, d)
	}
	return out
}
