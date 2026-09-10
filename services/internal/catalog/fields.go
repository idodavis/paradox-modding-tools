// fields.go types a field from the values it takes: `field = value` where every
// value names a def of one kind makes the field that kind, and a small closed
// set of scalars makes it an enum. Both are install-derived — no dump states
// which field holds which type — and both merge mods over vanilla.

package catalog

import (
	"maps"
	"paradox-modding-tools/services/internal/parser/loc"
	"slices"
	"strings"
)

// fieldKindCoverage is the share of a field's values that must resolve to the
// kind before the field is typed as it.
//
// Unanimity alone is not evidence: values that resolve to nothing were skipped,
// so one coincidental hit typed the field and every other use of it then became
// a reference of that kind which could never resolve. Measured on vanilla,
// `has_cultural_parameter` was typed `ep_3` on 1 of 432 values,
// `remove_variable` was typed `achievements` on 2 of 392, and `has_game_rule`
// was typed `modifiers` on 4 of 212 — between them the largest source of false
// "dangling reference" rows in Workspace Health.
//
// The distribution is strongly bimodal, so the exact threshold is not delicate:
// CK3 types 861 fields of which 632 sit at ≥90% coverage and 18 below 5%; Vic3
// types 357 with 194 at ≥90%. Half is the honest reading of "most of what this
// field holds is a thing of that kind".
const fieldKindCoverage = 50

// minFieldHits is how many of a field's values must resolve before the field is
// typed at all. Coverage is a ratio, and a ratio with no floor lets a single
// coincidence decide: `body_part` takes exactly two values across all of CK3,
// `head` and `torso`, and `head` also happens to name an artifact visual — so
// 1 of 2 cleared 50% exactly and every `body_part = torso` in the corpus became
// a dangling `visuals` reference, 859 of them on one mod.
//
// Measured on the three installs, fields typed on a single hit are junk without
// exception: `SIEGE_MESSAGE_ALIGNMENT` and `@zoom_step_far` as EU5
// locators_override, `clothes` as a CK3 animation, `0.25` as a Vic3
// trigger_localization. There are 240 such fields on CK3, 54 on Vic3 and 2,717
// on EU5. Two hits is where real typings start — `has_active_building` →
// buildings, `has_court_language` → pillars — so that is the floor.
//
// Same rule as minOptionRefs in options.go, which exists because a long tail of
// one-hit owners let a GUI property become a database.
const minFieldHits = 2

// deriveFieldValueKinds types a field from the definitions its values name: the
// values that resolve must agree on one kind, and enough of them must resolve.
func deriveFieldValueKinds(rhs map[string]map[string]bool, idx *defIndex) map[string]string {
	kindsByKey := idx.kindsByKey
	out := map[string]string{}
	for field, vals := range rhs {
		// A bare number is a weight, not a field name. Typing `20` as `event`
		// made `20 = empty` inside a Vic3 gene block read as a reference to a
		// missing event.
		if isDigitKey(field) {
			continue
		}
		var kind string
		ok, conflict, hits := false, false, 0
		for v := range vals {
			kinds := kindsByKey[v]
			if len(kinds) == 0 {
				continue
			}
			if len(kinds) > 1 {
				conflict = true
				break
			}
			if !ok {
				kind, ok = kinds[0], true
			} else if kinds[0] != kind {
				conflict = true
				break
			}
			hits++
		}
		if ok && !conflict && hits >= minFieldHits &&
			hits*100 >= len(vals)*fieldKindCoverage {
			out[field] = kind
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// MergeFieldValueKinds overlays mods on vanilla; a type conflict drops the field.
func MergeFieldValueKinds(vanilla, mods map[string]string) map[string]string {
	if len(vanilla) == 0 && len(mods) == 0 {
		return nil
	}
	out := maps.Clone(vanilla)
	if out == nil {
		out = map[string]string{}
	}
	for k, v := range mods {
		if prev, ok := out[k]; ok && prev != v {
			delete(out, k)
			continue
		}
		out[k] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

const fieldEnumMin, fieldEnumMax = 2, 64

// deriveFieldEnums keeps unique scalar RHS per kind+field when the field did not
// resolve to a def type and the unique count is a small enum (2–64).
func deriveFieldEnums(
	byKind map[string]map[string]map[string]bool,
	typed map[string]string,
) map[string]map[string][]string {
	typedL := map[string]bool{}
	for k := range typed {
		typedL[strings.ToLower(k)] = true
	}
	var out map[string]map[string][]string
	for kind, fields := range byKind {
		for field, vals := range fields {
			if typedL[strings.ToLower(field)] {
				continue
			}
			if n := len(vals); n < fieldEnumMin || n > fieldEnumMax {
				continue
			}
			keys := make([]string, 0, len(vals))
			for v := range vals {
				keys = append(keys, v)
			}
			slices.Sort(keys)
			if out == nil {
				out = map[string]map[string][]string{}
			}
			m := out[kind]
			if m == nil {
				m = map[string][]string{}
				out[kind] = m
			}
			m[strings.ToLower(field)] = keys
		}
	}
	return out
}

// MergeFieldEnums overlays mod enums on vanilla per kind+field.
func MergeFieldEnums(
	vanilla, mods map[string]map[string][]string,
) map[string]map[string][]string {
	if len(vanilla) == 0 && len(mods) == 0 {
		return nil
	}
	out := cloneFieldEnums(vanilla)
	if out == nil {
		out = map[string]map[string][]string{}
	}
	for kind, fields := range mods {
		dst := out[kind]
		if dst == nil {
			dst = map[string][]string{}
			out[kind] = dst
		}
		for field, vals := range fields {
			dst[strings.ToLower(field)] = slices.Clone(vals)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func cloneFieldEnums(
	in map[string]map[string][]string,
) map[string]map[string][]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]map[string][]string, len(in))
	for kind, fields := range in {
		m := make(map[string][]string, len(fields))
		for field, vals := range fields {
			m[field] = slices.Clone(vals)
		}
		out[kind] = m
	}
	return out
}

// minLocFieldHits and locFieldCoverage are the evidence floors for deciding a
// field holds localization keys.
//
// Coverage is higher than fieldKindCoverage because the target namespace is
// enormous: an install defines hundreds of thousands of localization keys, so a
// value coinciding with one proves much less than a value coinciding with a
// definition of some specific kind. Four in five is where a field is holding
// keys rather than occasionally colliding with them.
const (
	minLocFieldHits  = 8
	locFieldCoverage = 80
)

// deriveLocFields works out which script properties hold a localization key,
// from the values they take.
//
// The engine property names — `desc`, `title`, `custom_tooltip` — are listed in
// parser/loc, and that list is right for what it does: those are the fields
// whose value MUST resolve, so an unresolved one is a diagnostic. But it is a
// list of about thirty names, and the games have more. Victoria 3 character
// templates write `last_name = Addams` against a `Addams:0 "Addams"` key, so
// every localized surname in a mod was defined, consumed by the game, and
// reported as an orphaned key.
//
// A field found this way is only ever treated as broad: it marks a key used, it
// never demands one exist. Deriving that a field usually holds keys is not
// evidence that every value of it must.
//
// Values that name a definition are skipped, and that exclusion is the whole
// difference between this working and not. Every object in these games has a
// localization key named after it, so "the value is a key" is true of `culture`,
// `has_trait` and `capital` as much as of `last_name` — measured on CK3 it
// elected 323 fields and grew the reference table from 309k rows to 607k. A
// value that names a definition is a reference, and the definition's key is
// already reached through its kind's naming convention. What is left is the
// values that name nothing: prose the game looks up by its own text.
func deriveLocFields(
	rhs map[string]map[string]bool, locKeys, defKeys map[string]bool, schema *Schema,
) map[string]bool {
	if len(locKeys) == 0 || len(rhs) == 0 {
		return nil
	}
	out := map[string]bool{}
	for field, vals := range rhs {
		if isDigitKey(field) || loc.Classify(field) != loc.PropNone {
			continue // already declared by the engine property list
		}
		// Declared beats derived. `has_realm_law`, `add_realm_law` and
		// `unlock_law` are triggers and effects the game states outright; that
		// their values are law ids with localization keys named after them is a
		// fact about laws, not about the token. Without this the corpus elected
		// them, and every check macro along with them.
		if schema.HasName(field) {
			continue
		}
		// A macro-parameterised key (`has_$TYPE$_law`) is a spelling of a token,
		// not a property in its own right.
		if strings.ContainsRune(field, '$') {
			continue
		}
		hits, of := 0, 0
		for v := range vals {
			if defKeys[v] {
				continue
			}
			of++
			if locKeys[v] {
				hits++
			}
		}
		if hits >= minLocFieldHits && hits*100 >= of*locFieldCoverage {
			out[field] = true
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
