// bind.go joins the type system the game DECLARES to the databases the install
// walk harvested. Split out of schema.go (plan R1/FH.3): everything else in
// that file reads script_docs and knows nothing about defs, while this half is
// a derivation over the corpus and is the only reason schema.go referenced
// defIndex at all.

package derived

import (
	"strings"

	"paradox-modding-tools/services/internal/lang/declared"
	"paradox-modding-tools/services/internal/lang/jomini"
)

// bindKinds binds a declared scope to a harvested kind by exact name only.
func bindKinds(s *declared.Schema, idx *defIndex) (prefixKind, kindScope map[string]string) {
	prefixKind, kindScope = map[string]string{}, map[string]string{}
	if s == nil {
		return prefixKind, kindScope
	}
	for prefix, scope := range s.Prefixes() {
		kind := nameMatchKind(scope, idx.keysByKind)
		if kind == "" {
			continue
		}
		prefixKind[prefix] = kind
		kindScope[kind] = scope
	}
	return prefixKind, kindScope
}

// nameMatchKind matches a scope type to a folder kind by name, tolerating the
// singular/plural split (scope "culture" vs folder "cultures").
func nameMatchKind(scope string, keysByKind map[string]map[string]bool) string {
	if scope == "" {
		return ""
	}
	for _, cand := range []string{scope, scope + "s", strings.TrimSuffix(scope, "s")} {
		if k := jomini.CanonicalKind(cand); k != "" && len(keysByKind[k]) > 0 {
			return k
		}
	}
	// A `_type` scope is routinely named after its stem's own folder rather than
	// its own plural: `building_type` -> `buildings`, not `building_types`.
	if stem := strings.TrimSuffix(scope, "_type"); stem != scope {
		for _, cand := range []string{stem, stem + "s", strings.TrimSuffix(stem, "s")} {
			if k := jomini.CanonicalKind(cand); k != "" && len(keysByKind[k]) > 0 {
				return k
			}
		}
	}
	return ""
}

// typedCite is one `prefix:id` citation found in script.
type typedCite struct {
	prefix, id string
}

// collectTypedCites gathers every `prefix:id` citation in one parsed file.
// Language prefixes (scope:, var:, …) are skipped — they name locals, not
// database rows.
func collectTypedCites(root *jomini.Root) []typedCite {
	if root == nil {
		return nil
	}
	var out []typedCite
	add := func(text string) {
		for _, sp := range jomini.TypedSpans(text) {
			if jomini.PrefixKind(sp.Prefix) != "" {
				continue
			}
			out = append(out, typedCite{prefix: sp.Prefix, id: sp.ID})
		}
	}
	jomini.Walk(root, func(st jomini.Statement, _ int, _ *jomini.Block) bool {
		switch n := st.(type) {
		case *jomini.Assignment:
			if !n.Key.Quoted {
				add(n.Key.Text)
			}
			if sc, ok := n.Value.(*jomini.Scalar); ok && !sc.Quoted {
				add(sc.Text)
			}
		case *jomini.ValueStmt:
			if sc, ok := n.Value.(*jomini.Scalar); ok && !sc.Quoted {
				add(sc.Text)
			}
		}
		return true
	})
	return out
}
