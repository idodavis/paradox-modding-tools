// declared.go finds which mods declare which as dependencies. It also finds the mods that use another mod's names
// without declaring it.

package health

import (
	"slices"
	"strconv"

	"github.com/samber/lo"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/schema"
	"paradox-modding-tools/services/lsp"
	"paradox-modding-tools/services/workspace"
)

// listDeclared returns the pairs of mods where the first depends on the second, directly or down the chain. A
// dependency the workspace lacks is left out.
func listDeclared(ws *workspace.Session) map[[2]string]bool {
	direct := map[string][]string{}
	for _, mod := range ws.Order {
		for _, dependency := range mod.Descriptor.Dependencies {
			if other, ok := workspace.FindMod(ws.Order, dependency); ok {
				direct[mod.ID] = append(direct[mod.ID], other.ID)
			}
		}
	}
	declared := map[[2]string]bool{}
	for _, mod := range ws.Order {
		pending := slices.Clone(direct[mod.ID])
		for len(pending) > 0 {
			next := pending[len(pending)-1]
			pending = pending[:len(pending)-1]
			if next != mod.ID && !declared[[2]string{mod.ID, next}] {
				declared[[2]string{mod.ID, next}] = true
				pending = append(pending, direct[next]...)
			}
		}
	}
	return declared
}

// listUndeclared lists the mods that use another mod's names without depending on it, one finding per pair. A use is a
// reference, or a name written with an entry mode that needs one to act on, in a file the game reads.
func listUndeclared(ws *workspace.Session, mods *index.Index, declared map[[2]string]bool) []Finding {
	origins := map[schema.Reference][]string{} // the mods that define each name
	uses := listModSites(mods, func(site index.Site) bool {
		return isUse(mods, site) && mods.IsLoaded(site.File) && isUndeclaredUse(mods, origins, declared, site)
	})
	groups := map[[2]string][]index.Site{} // the first use of each name, by pair
	var pairs [][2]string
	keys := map[[3]string]bool{} // the names counted for each pair
	for _, site := range uses {
		reference := mods.ToReference(site)
		pair := [2]string{mods.File(site).Origin, findUsedMod(ws, mods, reference, origins[reference])}
		if key := [3]string{pair[0], pair[1], reference.Name}; !keys[key] {
			keys[key] = true
			if groups[pair] == nil {
				pairs = append(pairs, pair)
			}
			groups[pair] = append(groups[pair], site)
		}
	}
	return lo.Map(pairs, func(pair [2]string, _ int) Finding { return makeUndeclared(ws, mods, pair, groups[pair]) })
}

// isUse reports whether an occurrence uses a name: a reference, or a definition written with INJECT or REPLACE, which
// the game refuses where nothing defines the name (the EU5 wiki's Mod compatibility, l.57-59).
func isUse(mods *index.Index, site index.Site) bool {
	mode, known := mods.FindMode(site)
	return mods.Occurrence(site).Role == index.RoleRefers || known && !mode.Optional && !mode.Creates
}

// isUndeclaredUse reports whether a use names what only undeclared mods define. A name the game defines, or the file
// defines for itself, does not count.
func isUndeclaredUse(mods *index.Index, origins map[schema.Reference][]string, declared map[[2]string]bool, site index.Site) bool {
	reference, origin := mods.ToReference(site), mods.File(site).Origin
	definers := listOrigins(mods, origins, reference)
	if len(definers) == 0 || lo.SomeBy(definers, func(definer string) bool {
		return definer == game.OriginVanilla || definer == origin || declared[[2]string{origin, definer}]
	}) {
		return false
	}
	return !lo.SomeBy(mods.ResolveLocal(reference, mods.File(site).Path), func(definition index.Site) bool {
		return mods.Occurrence(definition).Local
	})
}

// listOrigins returns the mods that make a name, each once.
func listOrigins(mods *index.Index, origins map[schema.Reference][]string, reference schema.Reference) []string {
	if cached, ok := origins[reference]; ok {
		return cached
	}
	definitions := lo.Filter(mods.Resolve(reference), func(definition index.Site, _ int) bool {
		mode, known := mods.FindMode(definition)
		return (mods.IsLoaded(definition.File) || !isMod(mods, definition)) && (!known || mode.Creates)
	})
	origins[reference] = lo.Uniq(lo.Map(definitions, func(definition index.Site, _ int) string { return mods.File(definition).Origin }))
	return origins[reference]
}

// findUsedMod returns the one mod that defines a name, or the mod whose copy the game keeps. It returns the last mod
// that defines it if the game keeps none.
func findUsedMod(ws *workspace.Session, mods *index.Index, reference schema.Reference, origins []string) string {
	if len(origins) == 1 {
		return origins[0]
	}
	if winner, ok := mods.FindKept(ws.Schema, reference); ok {
		return mods.File(winner.Site).Origin
	}
	return origins[len(origins)-1]
}

// makeUndeclared returns the finding of a mod that uses names only another mod defines, without depending on it. It
// points at the first use of each name.
func makeUndeclared(ws *workspace.Session, mods *index.Index, pair [2]string, sites []index.Site) Finding {
	facts := lsp.DescribeReference(schema.Reference{Name: describeMod(ws, pair[0])})
	facts["other"], facts["count"] = describeMod(ws, pair[1]), strconv.Itoa(len(sites))
	finding := makeFinding(mods, "undeclared-dependency", facts, lo.Map(sites, func(site index.Site, _ int) FindingSite {
		return makeSite(ws, mods, site)
	}))
	finding.Origin, finding.Mods = pair[0], listMods(mods, pair[:])
	return finding
}
