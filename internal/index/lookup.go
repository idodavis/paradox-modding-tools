package index

import (
	"cmp"
	"slices"

	"github.com/samber/lo"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/schema"
)

// Resolve returns the definitions that a reference names, in load order. A local definition is found only from
// its own file, which is why the referring file is passed. The result is empty if the name is undefined.
func (index *Index) Resolve(reference schema.Reference, file int32) []Site {
	if reference.Database != "" {
		return index.listDefining(reference.Database, reference.Name, file)
	}
	// Of the databases that define the name, the smallest is the one meant, and the first by name where several are
	// as small, so that the order of the table, which differs between scans, never decides.
	var smallest []Site
	smallestDatabase, smallestCount := "", 0
	for ordinal, database := range index.Databases.Texts {
		sites := index.listDefining(database, reference.Name, file)
		if len(sites) == 0 {
			continue
		}
		count := index.countDatabase(uint16(ordinal))
		if smallest == nil || count < smallestCount || count == smallestCount && database < smallestDatabase {
			smallest, smallestDatabase, smallestCount = sites, database, count
		}
	}
	return smallest
}

// countDatabase returns how many definitions a database has, the install's included. It is 0 for a database that
// no file defines in.
func (index *Index) countDatabase(ordinal uint16) int {
	count := 0
	if int(ordinal) < len(index.counts) {
		count = index.counts[ordinal]
	}
	if index.install != nil {
		count += index.install.countDatabase(ordinal)
	}
	return count
}

// ListUses returns every reference to a name in a database, in load order.
func (index *Index) ListUses(database, name string) []Site {
	return index.listPosting(RoleRefers, database, name)
}

// ListDefinitions returns every definition in a database, in load order.
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
	for key, postings := range index.postings {
		if key.Role == RoleDefines && uint32(key.Database) == ordinal {
			own = append(own, postings...)
		}
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
	parent := index.Occurrence(site).Parent
	return Site{File: site.File, Occurrence: parent}, parent >= 0
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

// listDefining returns the sites that define a name in a database, less the local ones in a file other than the
// given one. A key of the localization database is defined by its messages.
func (index *Index) listDefining(database, name string, file int32) []Site {
	role := RoleDefines
	if database == game.Localization {
		role = RoleLocalizes
	}
	return lo.Filter(index.listPosting(role, database, name), func(site Site, _ int) bool {
		return !index.Occurrence(site).Local || site.File == file
	})
}

// listPosting returns the sites of the occurrences with the given role, database and name, in load order. It is
// empty where the index has none.
func (index *Index) listPosting(role Role, database, name string) []Site {
	databaseOrdinal, ok := index.Databases.Lookup(database)
	nameOrdinal, known := index.Names.Lookup(index.toCaseless(database, name))
	if !ok || !known {
		return nil
	}
	return index.listKey(Posting{Role: role, Database: uint16(databaseOrdinal), Name: nameOrdinal})
}

// listKey returns the sites of a posting, the install's first and then this index's own.
func (index *Index) listKey(key Posting) []Site {
	own := index.postings[key]
	if index.install == nil {
		return own
	}
	fromInstall := index.install.listKey(key)
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
	index.postings = map[Posting][]Site{}
	index.counts = make([]int, len(index.Databases.Texts))
	for file := int(index.countInstallFiles()); file < len(index.Occurrences); file++ {
		for position, occurrence := range index.Occurrences[file] {
			key := Posting{Role: occurrence.Role, Database: occurrence.Database, Name: occurrence.Name}
			site := Site{File: int32(file), Occurrence: int32(position)}
			index.postings[key] = append(index.postings[key], site)
			if occurrence.Role == RoleDefines {
				index.counts[occurrence.Database]++
			}
		}
	}
}
