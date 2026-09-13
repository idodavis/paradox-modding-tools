// bind.go joins the type system the game DECLARES to the databases the install
// walk harvested. Split out of schema.go (plan R1/FH.3): everything else in
// that file reads script_docs and knows nothing about defs, while this half is
// a derivation over the corpus and is the only reason schema.go referenced
// defIndex at all.

package derived

import (
	"maps"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/lang/declared"
	"paradox-modding-tools/services/internal/lang/jomini"
)

// minBindCites is the evidence floor for the CITATION join: below this a
// coincidental key match is plausible, so the join declines.
//
// The type does not then stay unbound. bindOne falls through to nameMatchKind,
// which binds a scope type to a folder whose name matches it singular or plural.
// That is deliberate and it is not a guess: measured across all three vanilla
// installs, every one of the 17 name-matched bindings is a real folder —
// struggle/struggles (2 defs), road_type/road_types (4),
// parliament_agenda/parliament_agendas (128), province/provinces (10,863). The
// last is why the fallback exists at all: CK3 cites provinces as `p:xB0D0E0`
// map colours, which are not script rows, so the citation join can never make a
// binding the folder name states outright.
//
// An earlier version of this comment said the type "stays unbound", and I.2g
// proposed flooring nameMatchKind on the strength of it. A floor would be
// wrong: this pass has no ratio to floor. It matches a name the game DECLARES
// against a folder of exactly that name, and the smallest matches are the
// genuine ones — house_relation_type/house_relation_types holds 1 def, Vic3
// provinces 3, religious_figures 2. Any count floor unbinds a correct declared
// type. TestBindByNameEvidence reports the distribution.
//
// b.ByName marks these, and checkBindings uses it: a name binding has coverage
// 0 by construction, so without the flag Vic3's province — 78 citations, none
// of them script rows — trips the weak-join assertion.
const minBindCites = 3

// bindFloor is the share of a prefix's citations its folders must explain for
// the type to bind at all. Measured on live installs the real bindings sit near
// 1.00, so a simple majority is a wide margin.
const bindFloor = 0.5

// bindSaturated stops the search once the database is essentially accounted
// for, leaving the tail of folders that each explain a citation or two.
const bindSaturated = 0.98

// bindShare is the fraction of a type's citations a folder must explain to be
// treated as one of its homes rather than an incidental co-keyed folder.
const bindShare = 0.1

// KindShare is one folder's contribution to a binding.
type KindShare struct {
	Kind string `json:"kind"`
	Hits int    `json:"hits"`
}

// KindBinding records one resolved type-to-database link, with the evidence.
// Kinds may hold more than one entry: a scope type is sometimes split across
// folders, as CK3 decisions are across their DLC directories. Kind is the
// largest contributor.
type KindBinding struct {
	Prefix   string      `json:"prefix"`
	Scope    string      `json:"scope"`
	Kind     string      `json:"kind"`
	Kinds    []KindShare `json:"kinds,omitempty"`
	Cites    int         `json:"cites"`
	Coverage float64     `json:"coverage"`
	ByName   bool        `json:"byName,omitempty"`
}

// Owners returns the folders that hold enough of the type to be treated as its
// home. Folders below the share threshold are incidental: CK3 coats of arms are
// keyed by title id, so they match title citations without being titles.
func (b KindBinding) Owners() []string {
	var out []string
	for _, k := range b.Kinds {
		if b.Cites == 0 || k.Hits*int(1/bindShare) >= b.Cites {
			out = append(out, k.Kind)
		}
	}
	return out
}

// bindKinds joins the type system the game declares to the databases the
// install walk harvested. For each citable `prefix:key` link, the folder whose
// top-level keys account for those citations owns that scope type.
//
// This replaces inference: the prefix list and its output types are read from
// event_targets.log, and the only thing computed here is which folder holds the
// instances — a set intersection over thousands of citations, not a vote.
func bindKinds(s *declared.Schema, idx *defIndex, cites []typedCite) (prefixKind, kindScope map[string]string, bound []KindBinding) {
	prefixKind, kindScope = map[string]string{}, map[string]string{}
	if s == nil {
		return prefixKind, kindScope, nil
	}

	keysByKind := idx.keysByKind

	idsByPrefix := map[string]map[string]bool{}
	for _, c := range cites {
		if c.prefix == "" || c.id == "" {
			continue
		}
		set := idsByPrefix[c.prefix]
		if set == nil {
			set = map[string]bool{}
			idsByPrefix[c.prefix] = set
		}
		set[c.id] = true
	}

	// Resolve the strongest evidence first so a contested kind goes to the type
	// that explains it best.
	for prefix, scope := range s.Prefixes() {
		b := bindOne(prefix, scope, idsByPrefix[prefix], keysByKind)
		if b.Kind != "" {
			bound = append(bound, b)
		}
	}
	slices.SortFunc(bound, func(a, b KindBinding) int {
		if a.Coverage != b.Coverage {
			if a.Coverage > b.Coverage {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Prefix, b.Prefix)
	})
	for _, b := range bound {
		prefixKind[b.Prefix] = b.Kind
		// Every folder that genuinely holds this type carries the scope, so a
		// split database still types all of its rows.
		for _, k := range b.Owners() {
			if _, taken := kindScope[k]; !taken {
				kindScope[k] = b.Scope
			}
		}
	}
	return prefixKind, kindScope, bound
}

// bindOne finds the folders that hold one prefix's citations. Kinds are taken
// in descending order of unexplained citations until the floor is cleared, so a
// database split across folders binds to all of them. Falls back to a
// singular/plural name match when vanilla never cites the prefix.
func bindOne(prefix, scope string, ids map[string]bool, keysByKind map[string]map[string]bool) KindBinding {
	b := KindBinding{Prefix: prefix, Scope: scope, Cites: len(ids)}
	if len(ids) >= minBindCites {
		remaining := make(map[string]bool, len(ids))
		maps.Copy(remaining, ids)
		explained := 0
		// Take folders in descending order of what they still explain, and keep
		// going while they contribute. Stopping at the floor would bind only the
		// largest folder of a split database.
		for len(remaining) > 0 {
			bestKind, bestHits := "", 0
			for kind, keys := range keysByKind {
				hits := 0
				for id := range remaining {
					if keys[id] {
						hits++
					}
				}
				// Ties go to the shorter kind name so a broad folder beats one
				// that incidentally reuses the same keys.
				if hits > bestHits || (hits == bestHits && hits > 0 && kind < bestKind) {
					bestKind, bestHits = kind, hits
				}
			}
			if bestHits == 0 {
				break
			}
			b.Kinds = append(b.Kinds, KindShare{Kind: bestKind, Hits: bestHits})
			explained += bestHits
			for id := range remaining {
				if keysByKind[bestKind][id] {
					delete(remaining, id)
				}
			}
			// Leave the long tail of one-hit folders alone once the database is
			// essentially accounted for.
			if float64(explained)/float64(len(ids)) >= bindSaturated {
				break
			}
		}
		if cov := float64(explained) / float64(len(ids)); cov >= bindFloor {
			b.Kind, b.Coverage = b.Kinds[0].Kind, cov
			return b
		}
		b.Kinds = nil
	}
	if kind := nameMatchKind(scope, keysByKind); kind != "" {
		b.Kind, b.ByName = kind, true
		b.Kinds = []KindShare{{Kind: kind}}
	}
	return b
}

// nameMatchKind matches a scope type to a folder kind by name, tolerating the
// singular/plural split (scope "culture" vs folder "cultures").
func nameMatchKind(scope string, keysByKind map[string]map[string]bool) string {
	if scope == "" {
		return ""
	}
	for _, cand := range []string{scope, scope + "s", strings.TrimSuffix(scope, "s")} {
		if cand != "" && len(keysByKind[cand]) > 0 {
			return cand
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
