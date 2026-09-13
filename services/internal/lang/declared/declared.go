// declared.go turns the type words a game writes beside a field in its own
// shipped documentation into something PMT can report and complete against.
//
// These are DECLARED types. They outrank FieldValueKinds and FieldEnumsByKind,
// which are inferred from how vanilla happens to use a field behind coverage
// floors -- so a wrong answer here is expensive in the way GAME-SYNTAX 13
// describes: it reaches hover, completion and typing at once. Refusing to
// classify is always the safe outcome; guessing is not.
package declared

import (
	"strings"

	"paradox-modding-tools/services/internal/lang/jomini"
)

// DeclaredType is what a game's documentation says a field holds.
//
// Zero value means "the text did not name a type PMT understands", which is the
// correct outcome for the prose that shares the same syntax -- EU5 writes
// `<name of global variable list with the list of candidates in>` and Vic3
// writes `<locator_name_1>` in the very same brackets that hold `<yes/no>`.
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

// declaredPrim maps one normalised type word to the name PMT reports for it, or
// "" when the words are not a type this check recognises.
//
// Scope is deliberately narrow: primitives and localization only. Those
// constrain what a modder may actually type and PMT has no other source for
// them. Everything else declines, and each refusal is a decision:
//
//   - Script constructs -- `trigger` (45), `effect` (22), `script value` (90
//     across three spellings), `modifier` (12), `mtth` (7) -- are already
//     declared in script_docs, which ranks above this and names the database
//     rather than a word.
//   - `database key` (14), `key` (16), `enum` (15), `identity key` (3) name no
//     PARTICULAR database, so "accepts a database key" only restates that a
//     value goes here.
//   - `dynamic desc` / `dynamic description` (7) hold a localization key, but
//     the games also accept a `first_valid` / `triggered_desc` block in that
//     slot. Calling it a localization key understates what the slot takes, and
//     understating it is how a modder learns a wrong rule.
//   - Prose wears the same brackets: EU5's `name of global variable list with
//     the list of candidates in` (7) and Vic3's `locator_name_1` (4). An
//     allow-list rejects those for free, which is why there is no shape rule --
//     the cost is a type lost each time a game adds a word, which a rescan
//     cannot fix but a line here can.
//
// Whole and decimal numbers stay apart rather than collapsing to "number".
// `annullment_favours_required = 2.5` is a real mistake and only a distinction
// can ever say so; one phrase would be simpler and never wrong, which is also
// never useful.
//
// The wording is a noun phrase because fieldAccepts renders it as "accepts a X"
// and "accepts several X values", and it explains rather than names the C type:
// this is the hover a modder reads to find out what goes here.
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
