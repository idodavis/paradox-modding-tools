// declared.go turns the type words a game writes beside a field in its own
// shipped documentation into something PMT can report and complete against.
// These are DECLARED types; they outrank FieldValueKinds/FieldEnumsByKind,
// which are inferred from usage. Refusing to classify beats guessing.
package declared

import (
	"strings"

	"paradox-modding-tools/services/internal/lang/jomini"
)

// DeclaredType is what a game's documentation says a field holds; zero value means the text named no type PMT understands.
type DeclaredType struct {
	// Values is a closed set the field accepts, in the order declared.
	Values []string
	// Prim is a normalised type word: see declaredPrim.
	Prim string
	// Array is set when the declaration says the field takes several.
	Array bool
}

// Known reports whether the declaration named anything usable.
func (d DeclaredType) Known() bool { return len(d.Values) > 0 || d.Prim != "" }

// declaredModifiers are words that qualify a type without changing it. They are
// removed before the type itself is read, so `bool, optional` and `bool` are one
// declaration -- optionality is a property of the field, not of its value.
var declaredModifiers = []string{"optional", "situational", "required", "opt"}

// ClassifyDeclaredType reads one raw declaration, as harvested.
func ClassifyDeclaredType(raw string) DeclaredType {
	s, array := normalizeDeclared(raw)
	if s == "" {
		return DeclaredType{}
	}
	if vs := declaredValueSet(s); len(vs) > 0 {
		return DeclaredType{Values: vs, Array: array}
	}
	return DeclaredType{Prim: declaredPrim(s), Array: array}
}

// normalizeDeclared lowercases, collapses whitespace, strips the qualifiers in
// declaredModifiers, and pulls an `array of` prefix out into its own flag.
func normalizeDeclared(raw string) (string, bool) {
	s := strings.ToLower(strings.Join(strings.Fields(raw), " "))
	s = strings.Trim(s, " ,.")
	array := false
	for _, m := range declaredModifiers {
		s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), m))
		s = strings.Trim(s, " ,")
		s = strings.TrimSpace(strings.TrimPrefix(s, m))
		s = strings.Trim(s, " ,")
	}
	if rest, ok := strings.CutPrefix(s, "array of "); ok {
		s, array = strings.TrimSpace(rest), true
	}
	if rest, ok := strings.CutPrefix(s, "list of "); ok {
		s, array = strings.TrimSpace(rest), true
	}
	return s, array
}

// declaredValueSet reads `yes/no`, `oneway/mutual`,
// `none/mutual/giving/receiving` -- a slash-separated set of bare words.
//
// Every member must be a bare word. A slash inside prose ("the giver/receiver
// of the relation") is not a value set, and admitting one would put sentence
// fragments in the completion list.
func declaredValueSet(s string) []string {
	if !strings.Contains(s, "/") {
		return nil
	}
	parts := strings.Split(s, "/")
	if len(parts) < 2 || len(parts) > 12 {
		return nil
	}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" || len(p) > 24 || !jomini.IsLowerName(p) {
			return nil
		}
		out = append(out, p)
	}
	return out
}

// declaredPrim maps one normalised type word to the noun phrase PMT reports, or "" when unrecognised (script constructs, database keys, and `<...>` prose all decline rather than guess).
func declaredPrim(s string) string {
	switch s {
	case "bool":
		return "yes/no value"
	case "int", "integer", "num":
		return "whole number"
	case "float":
		return "decimal number"
	case "string":
		return "text value"
	case "localization_key", "loc key":
		return "localization key"
	}
	return ""
}
