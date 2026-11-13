// lookup.go answers questions about names: where a reference is defined and
// used, what a database holds, and which file a texture's path names.

package index

import (
	"cmp"
	"slices"
	"strings"
	"sync"

	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/internal/texture"
)

// Resolve returns the definitions that a reference names, in load order. A
// local definition never counts. A reference with no database names the
// definitions in every database that defines its name. The result is empty if
// the name is undefined.
func (i *Index) Resolve(reference lang.Reference) []Site {
	return i.ResolveLocal(reference, -1)
}

// ResolveLocal is Resolve for a reference written in a file, given by its
// position in Files: that file's local definitions count too. A file the
// index does not list has the position -1, which counts none.
func (i *Index) ResolveLocal(reference lang.Reference, file int32) []Site {
	var sites []Site

	if reference.Database != "" {
		for _, callee := range ListCallees(reference) {
			sites = append(sites, i.listDefining(callee.Database, callee.Name, file)...)
		}

		return sites
	}

	databases := i.ListDatabases(reference.Name)
	slices.Sort(databases)

	for _, database := range databases {
		sites = append(sites, i.listDefining(database, reference.Name, file)...)
	}

	return sites
}

// ListCallees returns the references that a reference stands for. A
// reference in lang.AnyCall stands for the name in each callable database, in
// database name order. Any other reference stands for itself.
func ListCallees(reference lang.Reference) []lang.Reference {
	if reference.Database != lang.AnyCall {
		return []lang.Reference{reference}
	}

	callees := make([]lang.Reference, len(lang.Called))
	for position, database := range lang.Called {
		callees[position] = lang.Reference{Database: database, Name: reference.Name, Span: reference.Span}
	}

	return callees
}

// ListDatabases returns the databases with a definition of a name, local ones
// included, in the order of the index's table. It is empty if nothing
// defines the name.
func (i *Index) ListDatabases(name string) []string {
	var databases []string

	for _, database := range i.Databases.Texts {
		if len(i.listPosting(toRole(database), database, name)) > 0 {
			databases = append(databases, database)
		}
	}

	return databases
}

// ListUses returns every reference to a name in a database, in load order.
// For a database whose definitions script calls, the calls that may be to
// any callable database count too.
func (i *Index) ListUses(database, name string) []Site {
	uses := i.listPosting(RoleRefers, database, name)
	if !slices.Contains(lang.Called, database) {
		return uses
	}

	calls := i.listPosting(RoleRefers, lang.AnyCall, name)

	return slices.SortedFunc(slices.Values(slices.Concat(uses, calls)), compareSites)
}

// CountUses returns how many references the index holds to a name in a
// database.
func (i *Index) CountUses(database, name string) int {
	return len(i.listPosting(RoleRefers, database, name))
}

// isUsedByGame reports whether the game's own files, which are the install's,
// refer to a name in a database.
func (i *Index) isUsedByGame(database, name string) bool {
	return len(cmp.Or(i.install, i).ListUses(database, name)) > 0
}

// ListDefinitions returns every definition in a database, in load order. The
// definitions of localization keys are their messages.
func (i *Index) ListDefinitions(database string) []Site {
	var sites []Site
	if i.install != nil {
		sites = i.install.ListDefinitions(database)
	}

	ordinal, ok := i.Databases.Lookup(database)
	if !ok {
		return sites
	}

	var own []Site

	for _, postings := range i.postings[Posting{Role: toRole(database), Database: uint16(ordinal)}] {
		own = append(own, i.narrowSites(postings)...)
	}

	slices.SortFunc(own, compareSites)

	return append(sites, own...)
}

// ListMessages returns every message with the given key, one per language and
// file, in load order.
func (i *Index) ListMessages(key string) []Site {
	return i.listPosting(RoleLocalizes, lang.Localization, key)
}

// ListKeys returns up to a limit of the localization keys that contain a
// text, in any case, and have a message the game reads in one of the given
// languages. The text itself comes first, then the keys that start with it,
// then those that only contain it. It also reports whether the list is full,
// so more keys may match.
func (i *Index) ListKeys(languages []string, text string, limit int) ([]string, bool) {
	databaseOrdinal, ok := i.Databases.Lookup(lang.Localization)
	if !ok {
		return nil, false
	}

	posting, lower := Posting{Role: RoleLocalizes, Database: uint16(databaseOrdinal)}, strings.ToLower(text)
	isWritten := func(nameOrdinal uint32) bool {
		return slices.ContainsFunc(i.listKey(posting, nameOrdinal), func(site Site) bool {
			return i.IsLoaded(site.File) && slices.Contains(languages, i.Language(site))
		})
	}

	var starting, containing []string
	if nameOrdinal, ok := i.Names.Lookup(text); ok && isWritten(nameOrdinal) {
		starting = append(starting, text)
	}

	for nameOrdinal, key := range i.Names.Texts {
		if len(starting) == limit {
			break
		}

		folded := strings.ToLower(key)
		starts := strings.HasPrefix(folded, lower)

		// The key that is the text is listed already. A key that only
		// contains the text is kept only while the keys that start with it
		// leave room.
		if key == text || !starts && (len(starting)+len(containing) >= limit || !strings.Contains(folded, lower)) {
			continue
		}

		if !isWritten(uint32(nameOrdinal)) {
			continue
		}

		if starts {
			starting = append(starting, key)
		} else {
			containing = append(containing, key)
		}
	}

	keys := slices.Concat(starting, containing)

	return keys[:min(len(keys), limit)], len(keys) >= limit
}

// ListNamed returns the sites of the occurrences with the given role whose
// name the given function ranks 0 or higher, the lowest rank first. Within a
// rank this index's own files come before the install's. It ranks each name
// once, from the postings.
func (i *Index) ListNamed(role Role, rank func(name string) int) []Site {
	// order is 0 for a site in this index's own files and 1 for the install's.
	type match struct {
		site        Site
		rank, order int
	}

	var matches []match
	ranks := map[uint32]int{}
	for order, index := range []*Index{i, i.install} {
		if index == nil {
			continue
		}

		for posting, names := range index.postings {
			if posting.Role != role {
				continue
			}

			for name, sites := range names {
				if _, ranked := ranks[name]; !ranked {
					ranks[name] = rank(i.Names.Texts[name])
				}

				for _, site := range i.narrowSites(sites) {
					if ranks[name] >= 0 {
						matches = append(matches, match{site, ranks[name], order})
					}
				}
			}
		}
	}

	slices.SortFunc(matches, func(first, second match) int {
		return cmp.Or(cmp.Compare(first.rank, second.rank), cmp.Compare(first.order, second.order),
			compareSites(first.site, second.site))
	})

	sites := make([]Site, len(matches))
	for position, found := range matches {
		sites[position] = found.site
	}

	return sites
}

// FindDefinition returns the definition that a site sits inside. It returns
// false if the site is at the top of its file.
func (i *Index) FindDefinition(site Site) (Site, bool) {
	parent := i.Occurrence(site).Parent

	return Site{File: site.File, Occurrence: parent}, parent >= 0
}

// ToReference returns the database and the name that one occurrence is
// written with.
func (i *Index) ToReference(site Site) lang.Reference {
	occurrence := i.Occurrence(site)

	return lang.Reference{Database: i.Databases.Texts[occurrence.Database], Name: i.Names.Texts[occurrence.Name]}
}

// FindTextKey returns the localization key a definition writes for its title,
// given lang.TitleKey, or for its description, given lang.DescriptionKey. It
// returns false if the definition writes none.
func (i *Index) FindTextKey(definition Site, key string) (string, bool) {
	for _, site := range i.listReferences(definition) {
		occurrence := i.Occurrence(site)
		if key == lang.TitleKey && occurrence.Titles || key == lang.DescriptionKey && occurrence.Describes {
			return i.Names.Texts[occurrence.Name], true
		}
	}

	return "", false
}

// CountDefinitions counts a file's own definitions by database.
func (i *Index) CountDefinitions(file int32) map[string]int {
	counts := map[string]int{}

	for _, occurrence := range i.Occurrences[file] {
		if occurrence.Own {
			counts[i.Databases.Texts[occurrence.Database]]++
		}
	}

	return counts
}

// ResolveTexture returns the file the game reads for a texture's path as
// script writes it. It returns false if the game reads no such file.
func (i *Index) ResolveTexture(rel string) (string, bool) {
	return texture.Resolve(rel, func(candidate string) (string, bool) {
		loaded, _, ok := i.FindLoaded(candidate)

		return loaded.Path, ok
	})
}

// listDefining returns the sites that define a name in a database, in load
// order. A local definition counts only in the given file, and none counts
// for the file -1. A localization key is defined by its messages.
func (i *Index) listDefining(database, name string, file int32) []Site {
	return without(i.listPosting(toRole(database), database, name), func(site Site) bool {
		return i.Occurrence(site).Local && site.File != file
	})
}

// toRole returns the role of the occurrences that define the names of a
// database. A localization key is defined by its messages, and every other
// name by its definitions.
func toRole(database string) Role {
	if database == lang.Localization {
		return RoleLocalizes
	}

	return RoleDefines
}

// listPosting returns the sites of the occurrences with the given role,
// database and name, in load order. It is empty where the index has none.
func (i *Index) listPosting(role Role, database, name string) []Site {
	databaseOrdinal, ok := i.Databases.Lookup(database)
	if !ok {
		return nil
	}

	nameOrdinal, ok := i.Names.Lookup(i.ToCaseless(database, name))
	if !ok {
		return nil
	}

	return i.listKey(Posting{Role: role, Database: uint16(databaseOrdinal)}, nameOrdinal)
}

// listKey returns the sites of a name's ordinal in a posting, the install's
// first and then this index's own.
func (i *Index) listKey(posting Posting, name uint32) []Site {
	own := i.narrowSites(i.postings[posting][name])
	if i.install == nil {
		return own
	}

	installSites := i.install.listKey(posting, name)
	switch {
	case len(installSites) == 0:
		return own
	case len(own) == 0:
		return installSites
	}

	return slices.Concat(installSites, own)
}

// narrowSites returns the given sites that are in files the index answers
// for.
func (i *Index) narrowSites(sites []Site) []Site {
	return without(sites, func(site Site) bool { return !i.Covers(site.File) })
}

// without returns the sites the test does not match. It returns the list
// itself where none match, so a posting is copied only when it changes.
func without(sites []Site, test func(Site) bool) []Site {
	if !slices.ContainsFunc(sites, test) {
		return sites
	}

	return slices.DeleteFunc(slices.Clone(sites), test)
}

func compareSites(first, second Site) int {
	return cmp.Or(cmp.Compare(first.File, second.File), cmp.Compare(first.Occurrence, second.Occurrence))
}

// buildPostings fills the postings from the occurrences after a scan or a
// load, and drops the winners worked out before it. Only the index's own
// files are posted, since a lookup reads the install's postings as well.
func (i *Index) buildPostings() {
	i.postings = map[Posting]map[uint32][]Site{}
	i.winners = &sync.Map{}

	for file := int(i.countInstallFiles()); file < len(i.Occurrences); file++ {
		for position, occurrence := range i.Occurrences[file] {
			posting := Posting{Role: occurrence.Role, Database: occurrence.Database}
			if i.postings[posting] == nil {
				i.postings[posting] = map[uint32][]Site{}
			}

			site := Site{File: int32(file), Occurrence: int32(position)}
			i.postings[posting][occurrence.Name] = append(i.postings[posting][occurrence.Name], site)
		}
	}
}
