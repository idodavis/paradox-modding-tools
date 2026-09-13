// defindex.go is the one pass over a definition slice that five derivations used
// to make for themselves.

package derived

import "paradox-modding-tools/services/internal/lang/jomini"

// defIndex groups a def slice the ways the derivations ask for it.
//
// Grouped under IsCitableDef because that is what most of them want:
// defKeySet, bindKinds and deriveFieldValueKinds apply exactly this filter, and
// IsObjectKind — deriveLocAffixes' rule — is this filter minus `namespace`, so
// it narrows from here rather than needing its own walk. deriveSnippets is the
// one that does not: it counts definitions per RAW kind, so defsByKind is kept
// beside the rest and counts everything.
//
// One index per slice, never shared across two. rebind runs after nested defs
// exist and the mod path prepends the cache's defs, so those hold slices this
// one does not describe and build their own.
type defIndex struct {
	// kindsByKey is key -> the distinct citable kinds defined under it.
	kindsByKey map[string][]string
	// keysByKind is citable kind -> its distinct keys.
	keysByKind map[string]map[string]bool
	// defsByKind counts DEFINITIONS per raw kind — not distinct keys, not
	// canonicalised, not filtered. deriveSnippets weighs a member against how
	// often its kind occurs, so two definitions of one key are two occurrences.
	defsByKind map[string]int
}

func newDefIndex(defs []Def) *defIndex {
	x := &defIndex{
		kindsByKey: make(map[string][]string, len(defs)/2),
		keysByKind: make(map[string]map[string]bool, 64),
		defsByKind: make(map[string]int, 64),
	}
	for _, d := range defs {
		x.defsByKind[d.Kind]++
		if !IsCitableDef(d) {
			continue
		}
		k := jomini.CanonicalKind(d.Kind)
		set := x.keysByKind[k]
		if set == nil {
			set = map[string]bool{}
			x.keysByKind[k] = set
		}
		// One membership test dedupes both maps: the key is new for this kind
		// exactly when the kind is new for this key.
		if !set[d.Key] {
			set[d.Key] = true
			x.kindsByKey[d.Key] = append(x.kindsByKey[d.Key], k)
		}
	}
	return x
}

// citableKeys is every key a citable definition claims (was defKeySet).
func (x *defIndex) citableKeys() map[string]bool {
	out := make(map[string]bool, len(x.kindsByKey))
	for key := range x.kindsByKey {
		out[key] = true
	}
	return out
}

// keysOfKind is the distinct keys defined under one canonical kind. Nil when
// the install has none; callers only read it.
func (x *defIndex) keysOfKind(kind string) map[string]bool { return x.keysByKind[kind] }

// objectNames narrows to IsObjectKind — this index minus `namespace`, which is
// the only kind citable and not an object. Returns the kinds each name has and
// how many distinct names each kind holds, which is what the affix derivation
// weighs a shape against.
func (x *defIndex) objectNames() (kindsOf map[string][]string, total map[string]int) {
	kindsOf = make(map[string][]string, len(x.kindsByKey))
	total = make(map[string]int, len(x.keysByKind))
	for k, keys := range x.keysByKind {
		if !jomini.IsObjectKind(k) {
			continue
		}
		total[k] = len(keys)
	}
	for key, kinds := range x.kindsByKey {
		var keep []string
		for _, k := range kinds {
			if jomini.IsObjectKind(k) {
				keep = append(keep, k)
			}
		}
		if len(keep) > 0 {
			kindsOf[key] = keep
		}
	}
	return kindsOf, total
}
