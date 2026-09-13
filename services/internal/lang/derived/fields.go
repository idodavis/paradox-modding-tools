// fields.go types a field from the values it takes: `field = value` where every
// value names a def of one kind makes the field that kind. Install-derived — no
// dump states which field holds which type — and merges mods over vanilla.

package derived

import (
	"paradox-modding-tools/services/internal/lang/declared"
	"paradox-modding-tools/services/internal/lang/loc"
	"strings"
)

const fieldKindCoverage = 50

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

func MergeFieldValueKinds(vanilla, mods map[string]string) map[string]string {
	return mergeStringMap(vanilla, mods)
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

func deriveLocFields(
	rhs map[string]map[string]bool, locKeys, defKeys map[string]bool, schema *declared.Schema,
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
