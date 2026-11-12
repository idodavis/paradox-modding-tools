// lookup.go answers the index's questions about names: where a reference is
// defined and used, what a database holds, which definition a site sits in
// and which file a texture's path names.

package index

import (
	"cmp"
	"slices"
	"strings"
	"sync"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/schema"
	"paradox-modding-tools/internal/texture"
)

// Resolve returns the definitions that a reference names, in load order. A
// local definition never counts. A reference with no database names the
// definitions in every database that defines its name. The result is empty if
// the name is undefined.
func (i *Index) Resolve(reference schema.Reference) []Site {
	return i.listResolved(reference, func(Site) bool { return false })
}

// ResolveLocal is Resolve for a reference written in the file at a position
// in Files: that file's local definitions count too. A file the index does
// not list has the position -1, which counts none.
func (i *Index) ResolveLocal(reference schema.Reference, file int32) []Site {
	return i.listResolved(reference, func(site Site) bool { return site.File == file })
}

// listResolved returns the definitions that a reference names, as Resolve
// orders them. A local definition counts only if the given test accepts its
// site.
func (i *Index) listResolved(reference schema.Reference, isVisible func(Site) bool) []Site {
	var sites []Site

	if reference.Database != "" {
		for _, callee := range ListCallees(reference) {
			sites = append(sites, i.listDefining(callee.Database, callee.Name, isVisible)...)
		}

		return sites
	}

	databases := i.ListDatabases(reference.Name)
	slices.Sort(databases)

	for _, database := range databases {
		sites = append(sites, i.listDefining(database, reference.Name, isVisible)...)
	}

	return sites
}

// ListCallees returns the references that a reference stands for. A
// reference in game.AnyCall stands for the name in each callable database, in
// database name order. Any other reference stands for itself.
func ListCallees(reference schema.Reference) []schema.Reference {
	if reference.Database != game.AnyCall {
		return []schema.Reference{reference}
	}

	callees := make([]schema.Reference, len(game.Called))
	for position, database := range game.Called {
		callees[position] = schema.Reference{Database: database, Name: reference.Name, Span: reference.Span}
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
	if !slices.Contains(game.Called, database) {
		return uses
	}

	calls := i.listPosting(RoleRefers, game.AnyCall, name)

	return slices.SortedFunc(slices.Values(slices.Concat(uses, calls)), compareSites)
}

// CountUses returns how many references the index holds to a name in a
// database.
func (i *Index) CountUses(database, name string) int {
	return len(i.listPosting(RoleRefers, database, name))
}

// IsUsedByGame reports whether the game's own files refer to a name in a
// database. For a database whose definitions script calls, the calls that
// may be to any callable database count too.
func (i *Index) IsUsedByGame(database, name string) bool {
	// The game's own files are the install's, whose postings answer without
	// a list of every use being built.
	install := cmp.Or(i.install, i)

	return len(install.listPosting(RoleRefers, database, name)) > 0 ||
		slices.Contains(game.Called, database) && len(install.listPosting(RoleRefers, game.AnyCall, name)) > 0
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
	return i.listPosting(RoleLocalizes, game.Localization, key)
}

// ListKeys returns up to the given limit of the localization keys that
// contain the given text, in any case, and have a loaded message in one of
// the given languages. A key that is the text itself comes first, then the
// keys that
// start with the text, then the ones that only contain it. Each key comes
// once, and within each of the two the keys come in the order the index
// first met them. It also reports whether the list is cut: it holds as many
// keys as the limit allows, so more may match.
func (i *Index) ListKeys(languages []string, text string, limit int) ([]string, bool) {
	databaseOrdinal, ok := i.Databases.Lookup(game.Localization)
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
// rank this index's own files come before the install's, each in load order.
// It reads the postings, so it looks at each name once and at no reference.
func (i *Index) ListNamed(role Role, rank func(name string) int) []Site {
	// order is 0 for a site of this index's own files and 1 for the install's
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
	occurrence := i.Occurrence(site)

	return Site{File: site.File, Occurrence: occurrence.Parent}, occurrence.Nested
}

// ToReference returns the database and the name that one occurrence is
// written with.
func (i *Index) ToReference(site Site) schema.Reference {
	occurrence := i.Occurrence(site)

	return schema.Reference{Database: i.Databases.Texts[occurrence.Database], Name: i.Names.Texts[occurrence.Name]}
}

// FindTextKey returns the localization key a definition writes for its title,
// given game.TitleKey, or for its description, given game.DescriptionKey. It
// returns false if the definition writes none.
func (i *Index) FindTextKey(definition Site, key string) (string, bool) {
	for _, site := range i.listReferences(definition) {
		occurrence := i.Occurrence(site)
		if key == game.TitleKey && occurrence.Titles || key == game.DescriptionKey && occurrence.Describes {
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

// listDefining returns the sites that define a name in a database. A local
// definition counts only if the given test accepts its site. A key of the
// localization database is defined by its messages.
func (i *Index) listDefining(database, name string, isVisible func(Site) bool) []Site {
	return slices.DeleteFunc(slices.Clone(i.listPosting(toRole(database), database, name)), func(site Site) bool {
		return i.Occurrence(site).Local && !isVisible(site)
	})
}

// toRole returns the role of the occurrences that define the names of a
// database. A localization key is defined by its messages, and every other
// name by its definitions.
func toRole(database string) Role {
	if database == game.Localization {
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
// for. A list it answers for whole is returned as it is.
func (i *Index) narrowSites(sites []Site) []Site {
	outside := func(site Site) bool { return !i.Covers(site.File) }
	if !slices.ContainsFunc(sites, outside) {
		return sites
	}

	return slices.DeleteFunc(slices.Clone(sites), outside)
}

// compareSites orders two sites by their files' positions, then by their
// places in a file.
func compareSites(first, second Site) int {
	return cmp.Or(cmp.Compare(first.File, second.File), cmp.Compare(first.Occurrence, second.Occurrence))
}

// buildPostings fills the postings from the occurrences after a scan or a
// load, and drops the winners worked out before it.
func (i *Index) buildPostings() {
	// Only the index's own files are posted, since a lookup reads the
	// install's postings as well.
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
