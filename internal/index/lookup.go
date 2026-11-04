// lookup.go answers the index's questions about names: where a reference is defined and used, what a database holds
// and which definition a site sits in.

package index

import (
	"cmp"
	"maps"
	"slices"
	"strings"

	"github.com/samber/lo"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/schema"
)

// Resolve returns the definitions that a reference names, in load order. A local definition never counts. A reference
// with no database names the definitions in every database that defines its name, ordered by database name and then in
// load order. The result is empty if the name is undefined.
func (index *Index) Resolve(reference schema.Reference) []Site {
	return index.listResolved(reference, func(Site) bool { return false })
}

// ResolveLocal is Resolve for a reference written in the file at a position in Files: that file's local definitions
// count too. A file the index does not list has the position -1, which counts none.
func (index *Index) ResolveLocal(reference schema.Reference, file int32) []Site {
	return index.listResolved(reference, func(site Site) bool { return site.File == file })
}

// listResolved returns the definitions that a reference names, as Resolve orders them. A local definition counts only
// if isVisible accepts its site.
func (index *Index) listResolved(reference schema.Reference, isVisible func(Site) bool) []Site {
	if reference.Database != "" {
		return lo.FlatMap(ListCallees(reference), func(callee schema.Reference, _ int) []Site {
			return index.listDefining(callee.Database, callee.Name, isVisible)
		})
	}
	databases := index.ListDatabases(reference.Name)
	slices.Sort(databases)
	return lo.FlatMap(databases, func(database string, _ int) []Site {
		return index.listDefining(database, reference.Name, isVisible)
	})
}

// ListDatabases returns the databases with a definition of a name, local ones included, in the order of the index's
// table. A key of the localization database is defined by its messages. It is empty if nothing defines the name.
func (index *Index) ListDatabases(name string) []string {
	return lo.Filter(index.Databases.Texts, func(database string, _ int) bool {
		return len(index.listPosting(toRole(database), database, name)) > 0
	})
}

// ListUses returns every reference to a name in a database, in load order. For a database whose definitions script
// calls, the calls that may be to any callable database count too.
func (index *Index) ListUses(database, name string) []Site {
	uses := index.listPosting(RoleRefers, database, name)
	if len(game.Callables[database].Callers) == 0 {
		return uses
	}
	return slices.SortedFunc(slices.Values(slices.Concat(uses, index.listPosting(RoleRefers, game.AnyCall, name))), compareSites)
}

// ListCallees returns the references that a reference stands for. A reference in game.AnyCall may name any callable
// database, so it stands for the name in each database whose definitions script calls by name, in database name order.
// Any other reference stands for itself.
func ListCallees(reference schema.Reference) []schema.Reference {
	if reference.Database != game.AnyCall {
		return []schema.Reference{reference}
	}
	var callees []schema.Reference
	for _, database := range slices.Sorted(maps.Keys(game.Callables)) {
		if len(game.Callables[database].Callers) > 0 {
			callees = append(callees, schema.Reference{Database: database, Name: reference.Name, Span: reference.Span})
		}
	}
	return callees
}

// ListDefinitions returns every definition in a database, in load order. The definitions of localization keys are
// their messages.
func (index *Index) ListDefinitions(database string) []Site {
	var sites []Site
	if index.install != nil {
		sites = index.install.ListDefinitions(database)
	}
	ordinal, ok := index.Databases.Lookup(database)
	if !ok {
		return sites
	}
	var own []Site
	for _, postings := range index.postings[Posting{Role: toRole(database), Database: uint16(ordinal)}] {
		own = append(own, index.narrowSites(postings)...)
	}
	slices.SortFunc(own, compareSites)
	return append(sites, own...)
}

// ListMessages returns every message with the given key, one per language and file, in load order.
func (index *Index) ListMessages(key string) []Site {
	return index.listPosting(RoleLocalizes, game.Localization, key)
}

// ListKeys returns up to the given limit of the localization keys that contain the given text, in any case, and have
// a loaded message in the given language. Each key comes once, in the order the index first met it.
func (index *Index) ListKeys(language, text string, limit int) []string {
	ordinal, ok := index.Databases.Lookup(game.Localization)
	if !ok {
		return nil
	}
	posting, lower := Posting{Role: RoleLocalizes, Database: uint16(ordinal)}, strings.ToLower(text)
	var keys []string
	for name, key := range index.Names.Texts {
		if len(keys) == limit {
			break
		}
		if strings.Contains(strings.ToLower(key), lower) && slices.ContainsFunc(index.listKey(posting, uint32(name)),
			func(site Site) bool { return index.IsLoaded(site.File) && index.Language(site) == language }) {
			keys = append(keys, key)
		}
	}
	return keys
}

// FindDefinition returns the definition that a site sits inside, or false if the site is at the top of its file.
func (index *Index) FindDefinition(site Site) (Site, bool) {
	occurrence := index.Occurrence(site)
	return Site{File: site.File, Occurrence: occurrence.Parent}, occurrence.Nested
}

// ToReference returns the database and the name that one occurrence is written with.
func (index *Index) ToReference(site Site) schema.Reference {
	occurrence := index.Occurrence(site)
	return schema.Reference{Database: index.Databases.Texts[occurrence.Database], Name: index.Names.Texts[occurrence.Name]}
}

// CountDefinitions counts a file's definitions by database. An alias is no definition of its own.
func (index *Index) CountDefinitions(file int32) map[string]int {
	counts := map[string]int{}
	for _, occurrence := range index.Occurrences[file] {
		if occurrence.Role == RoleDefines && !occurrence.Alias {
			counts[index.Databases.Texts[occurrence.Database]]++
		}
	}
	return counts
}

// listDefining returns the sites that define a name in a database. A local definition counts only if isVisible accepts
// its site. A key of the localization database is defined by its messages.
func (index *Index) listDefining(database, name string, isVisible func(Site) bool) []Site {
	return lo.Filter(index.listPosting(toRole(database), database, name), func(site Site, _ int) bool {
		return !index.Occurrence(site).Local || isVisible(site)
	})
}

// toRole returns the role of the occurrences that define the names of a database. A localization key is defined by
// its messages; every other name by its definitions.
func toRole(database string) Role {
	if database == game.Localization {
		return RoleLocalizes
	}
	return RoleDefines
}

// listPosting returns the sites of the occurrences with the given role, database and name, in load order. It is
// empty where the index has none.
func (index *Index) listPosting(role Role, database, name string) []Site {
	databaseOrdinal, ok := index.Databases.Lookup(database)
	nameOrdinal, known := index.Names.Lookup(index.ToCaseless(database, name))
	if !ok || !known {
		return nil
	}
	return index.listKey(Posting{Role: role, Database: uint16(databaseOrdinal)}, nameOrdinal)
}

// listKey returns the sites of a name's ordinal in a posting, the install's first and then this index's own.
func (index *Index) listKey(posting Posting, name uint32) []Site {
	own := index.narrowSites(index.postings[posting][name])
	if index.install == nil {
		return own
	}
	installSites := index.install.listKey(posting, name)
	switch {
	case len(installSites) == 0:
		return own
	case len(own) == 0:
		return installSites
	}
	return slices.Concat(installSites, own)
}

// narrowSites returns the given sites that are in files the index answers for. An index that is not narrowed returns
// them all.
func (index *Index) narrowSites(sites []Site) []Site {
	if index.origins == nil {
		return sites
	}
	return lo.Filter(sites, func(site Site, _ int) bool { return index.hasFile(site.File) })
}

// compareSites orders two sites by their files' positions, then by their places in a file.
func compareSites(a, b Site) int {
	return cmp.Or(cmp.Compare(a.File, b.File), cmp.Compare(a.Occurrence, b.Occurrence))
}

// buildPostings fills the postings from the occurrences after a scan or a load.
func (index *Index) buildPostings() {
	// Only the index's own files are posted; a lookup reads the install's postings as well.
	index.postings = map[Posting]map[uint32][]Site{}
	for file := int(index.countInstallFiles()); file < len(index.Occurrences); file++ {
		for position, occurrence := range index.Occurrences[file] {
			key := Posting{Role: occurrence.Role, Database: occurrence.Database}
			if index.postings[key] == nil {
				index.postings[key] = map[uint32][]Site{}
			}
			site := Site{File: int32(file), Occurrence: int32(position)}
			index.postings[key][occurrence.Name] = append(index.postings[key][occurrence.Name], site)
		}
	}
}
