// index.go harvests a whole workspace: every mod in load order, merged into one
// in-memory Harvest. Nothing here is persisted; the session rebuilds it on open.

package catalog

import (
	"cmp"
	"context"
	"slices"
)

// Harvest is the in-memory result of indexing a workspace's mods.
type Harvest struct {
	Defs             []Def
	Refs             []Ref
	Edges            []Edge
	Loc              map[string]map[string]LocEntry
	Order            []string
	FieldValueKinds  map[string]string
	FieldEnumsByKind map[string]map[string][]string
	FireKeys         map[string]string
	// Structures are the block-member key names the mods declare, per kind. The
	// walk already collected these and threw them away; the orphan check needs
	// them, because a mod's new game-rule option is a member name that exists in
	// no install and in no definition index.
	Structures map[string][]string
}

// BuildIndex walks every mod in load order and harvests its files.
func BuildIndex(ctx context.Context, gameID string, mods []ModInput, cache *VanillaCache) (Harvest, error) {
	ordered := append([]ModInput{}, mods...)
	slices.SortStableFunc(ordered, func(a, b ModInput) int { return cmp.Compare(a.Order, b.Order) })
	var files []fileRef
	var order []string
	for _, m := range ordered {
		order = append(order, m.Origin)
		for _, f := range modFiles(m.Root) {
			f.origin = m.Origin
			files = append(files, f)
		}
	}
	var schema *Schema
	if cache != nil {
		schema = cache.Schema
	}
	acc, derived, _, err := collectExtracts(ctx, gameID, files, false, cache, schema, nil)
	if acc == nil {
		return Harvest{}, err
	}
	defs := acc.defs
	if cache != nil && len(cache.Defs) > 0 {
		defs = append(append([]Def{}, cache.Defs...), acc.defs...)
	}
	kinds := deriveFieldValueKinds(acc.fieldRHS, defs)
	var fireKeys map[string]string
	if derived != nil {
		fireKeys = derived.FireKeys
	}
	return Harvest{
		Defs: acc.defs, Refs: acc.refs, Edges: acc.edges, Loc: acc.loc,
		Order: uniqKeep(order), FieldValueKinds: kinds,
		FieldEnumsByKind: deriveFieldEnums(acc.fieldRHSByKind, kinds),
		FireKeys:         fireKeys,
		Structures:       keysByCount(acc.structures),
	}, err
}

func uniqKeep(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// MergeLoc copies locd into dst, allocating maps as needed.
func MergeLoc(dst map[string]map[string]LocEntry, locd LocDelta) map[string]map[string]LocEntry {
	if locd.Lang == "" || len(locd.Vals) == 0 {
		return dst
	}
	if dst == nil {
		dst = map[string]map[string]LocEntry{}
	}
	m := dst[locd.Lang]
	if m == nil {
		m = map[string]LocEntry{}
		dst[locd.Lang] = m
	}
	for k, v := range locd.Vals {
		m[k] = v
	}
	return dst
}

func modFiles(root string) []fileRef {
	var out []fileRef
	walkClassified(root, func(kind string, f fileRef) {
		switch kind {
		case "mod", "gui", "loc", "script":
			out = append(out, f)
		}
	})
	return out
}
