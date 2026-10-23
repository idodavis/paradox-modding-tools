package index

import (
	"cmp"
	"slices"

	"github.com/samber/lo"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/schema"
)

// Resolve returns the definitions that a reference names, in load order. A local definition never counts. The result
// is empty if the name is undefined.
func (index *Index) Resolve(schema *schema.Schema, reference schema.Reference) []Site {
	return index.listResolved(schema, reference, func(Site) bool { return false })
}

// ResolveLocal is Resolve for a reference written in the given file: the local definitions of that file count too.
func (index *Index) ResolveLocal(schema *schema.Schema, reference schema.Reference, file int32) []Site {
	return index.listResolved(schema, reference, func(site Site) bool { return site.File == file })
}

// listResolved returns the definitions that a reference names, in load order. A local definition counts where the
// given function says it does.
func (index *Index) listResolved(schema *schema.Schema, reference schema.Reference, isVisible func(Site) bool) []Site {
	if reference.Database != "" {
		return index.listDefining(reference.Database, reference.Name, isVisible)
	}
	// Of the databases that define the name, the one meant is the smallest in vanilla, as the schema ranks them, so
	// that neither the mods nor the order of the table decide.
	var smallest []Site
	smallestDatabase := ""
	for _, database := range index.ListDatabases(reference.Name) {
		sites := index.listDefining(database, reference.Name, isVisible)
		if len(sites) > 0 && (smallest == nil || schema.IsSmaller(database, smallestDatabase)) {
			smallest, smallestDatabase = sites, database
		}
	}
	return smallest
}

// ListDatabases returns the databases with a definition of a name, local ones included, in the order of the index's
// table. A key of the localization database is defined by its messages. It is empty if nothing defines the name.
func (index *Index) ListDatabases(name string) []string {
	return lo.Filter(index.Databases.Texts, func(database string, _ int) bool {
		return len(index.listPosting(toRole(database), database, name)) > 0
	})
}

// ListUses returns every reference to a name in a database, in load order.
func (index *Index) ListUses(database, name string) []Site {
	return index.listPosting(RoleRefers, database, name)
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

// FindNearest returns the innermost occurrence in a file that covers the given byte offset, or false if none
// does.
func (index *Index) FindNearest(file int32, offset int) (Site, bool) {
	nearest, length := Site{}, 0
	ok := false
	for position, occurrence := range index.Occurrences[file] {
		width := occurrence.Span.End - occurrence.Span.Start
		if occurrence.Span.Contains(offset) && (!ok || width <= length) {
			nearest, length, ok = Site{File: file, Occurrence: int32(position)}, width, true
		}
	}
	return nearest, ok
}

// CountDefinitions counts a file's definitions by database.
func (index *Index) CountDefinitions(file int32) map[string]int {
	counts := map[string]int{}
	for _, occurrence := range index.Occurrences[file] {
		if occurrence.Role == RoleDefines {
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
	nameOrdinal, known := index.Names.Lookup(index.toCaseless(database, name))
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
