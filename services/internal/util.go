package internal

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// canonicalScriptValue strips Paradox-style comments and whitespace for comparison.
func canonicalScriptValue(s string) string {
	var parts []string
	for _, line := range strings.Split(s, "\n") {
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		parts = append(parts, strings.Fields(line)...)
	}
	return strings.Join(parts, "")
}

// ScriptValueHash returns a hash of the canonical form of Paradox script value text
// (comments and whitespace stripped). Use for semantic equality in merges.
func ScriptValueHash(text string) string {
	canonical := canonicalScriptValue(text)
	h := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(h[:])
}

// ScriptValuesEqual returns true if two script values are semantically equal
// (identical after stripping comments and whitespace).
func ScriptValuesEqual(a, b string) bool {
	if a == b {
		return true
	}
	return ScriptValueHash(a) == ScriptValueHash(b)
}

func InstallPathHash(path string) string {
	h := sha256.Sum256([]byte(path))
	return hex.EncodeToString(h[:])
}

func IntersectStrings(a, b []string) []string {
	set := make(map[string]bool)
	for _, s := range b {
		set[s] = true
	}
	var out []string
	for _, s := range a {
		if set[s] {
			out = append(out, s)
		}
	}
	return out
}
