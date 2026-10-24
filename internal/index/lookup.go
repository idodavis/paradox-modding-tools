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
// with no database names the definitions of every database that defines its name: each database's in load order, the
// databases by name. The result is empty if the name is undefined.
func (index *Index) Resolve(reference schema.Reference) []Site {
	return index.listResolved(reference, func(Site) bool { return false })
}

// ResolveLocal is Resolve for a reference written in the given file: that file's local definitions count too. A file
// the index does not list counts none.
func (index *Index) ResolveLocal(reference schema.Reference, path string) []Site {
	file, listed := index.FindFile(path)
	return index.listResolved(reference, func(site Site) bool { return listed && site.File == file })
}

// listResolved returns the definitions that a reference names, as Resolve orders them. A local definition counts where
// the given function says it does.
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
	exact, hasExact := index.Names.Lookup(name)
	lower, hasLower := index.Names.Lookup(strings.ToLower(name))
	var databases []string
	for ordinal, database := range index.Databases.Texts {
		key, listed := exact, hasExact
		if slices.Contains(index.Caseless, database) {
			key, listed = lower, hasLower
		}
		posting := Posting{Role: toRole(database), Database: uint16(ordinal)}
		for source := index; listed && source != nil; source = source.install {
			if len(source.postings[posting][key]) > 0 {
				databases = append(databases, database)
				break
			}
		}
	}
	return databases
}

// ListUses returns every reference to a name in a database, in load order. For a database that script calls by name,
// the calls that may name any callable database count too.
func (index *Index) ListUses(database, name string) []Site {
	uses := index.listPosting(RoleRefers, database, name)
	if len(game.Callables[database].Callers) == 0 {
		return uses
	}
	return slices.SortedFunc(slices.Values(slices.Concat(uses, index.listPosting(RoleRefers, game.AnyCall, name))), compareSites)
}

// ListCallees returns the references that a reference stands for. A call that may name any callable database, one
// written in game.AnyCall, stands for the name in each database whose definitions script calls by name, in the order
// of their names. Any other reference stands for itself.
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
		own = append(own, postings...)
	}
	slices.SortFunc(own, compareSites)
	return append(sites, own...)
}

// ListMessages returns every message with the given key, one per language and file, in load order.
func (index *Index) ListMessages(key string) []Site {
	return index.listPosting(RoleLocalizes, game.Localization, key)
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

// listDefining returns the sites that define a name in a database, less the local ones that the given function does
// not accept. A key of the localization database is defined by its messages.
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

// listKey returns the sites of a name in a posting, the install's first and then this index's own.
func (index *Index) listKey(key Posting, name uint32) []Site {
	own := index.postings[key][name]
	if index.install == nil {
		return own
	}
	fromInstall := index.install.listKey(key, name)
	switch {
	case len(fromInstall) == 0:
		return own
	case len(own) == 0:
		return fromInstall
	}
	return slices.Concat(fromInstall, own)
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
