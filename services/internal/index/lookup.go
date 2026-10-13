// lookup.go answers the editor's and the pages' questions from the store, in the game's load order.

package index

import (
	"cmp"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/appdata"
	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/inventory"
	"paradox-modding-tools/services/internal/schema"
)

// Lookup is every occurrence of a role of a name across the indexes, in the game's load order: the files
// the game loads, by Rel, each file's in source order. A call is looked up with Database "".
func Lookup(indexes []*Index, role schema.Role, database, id string) (matches []At) {
	for _, index := range indexes {
		for _, at := range index.listed(role, database, id) {
			if _, loaded := Loaded(indexes, at.File().Path); loaded {
				matches = append(matches, at)
			}
		}
	}
	slices.SortStableFunc(matches, func(first, second At) int {
		return cmp.Or(strings.Compare(strings.ToLower(first.File().Rel), strings.ToLower(second.File().Rel)),
			first.Occurrence().Range.Start-second.Occurrence().Range.Start)
	})
	return matches
}

// listed is the index's own occurrences of a role of a name, in file order; none for a name it never met.
func (index *Index) listed(role schema.Role, database, id string) []At {
	databaseOrdinal, inDatabases := index.Databases.ordinals[database]
	idOrdinal, inIDs := index.IDs.ordinals[id]
	if !inDatabases || !inIDs {
		return nil
	}
	return index.postings[posting{role, uint16(databaseOrdinal), uint32(idOrdinal)}]
}

// FileAt is the file at a Rel across the indexes, whatever its case: the last index's that lists
// it, at its last place there.
func FileAt(indexes []*Index, rel string) (inventory.File, bool) {
	for _, index := range slices.Backward(indexes) {
		if pos, listed := index.rels[strings.ToLower(rel)]; listed {
			return index.Files[pos], true
		}
	}
	return inventory.File{}, false
}

// Loaded is the file listed at a path of the indexes and whether the game loads it: it is hidden by
// a later file at its Rel, in its index or a later one, or by a later index's Replaced.
func Loaded(indexes []*Index, path string) (inventory.File, bool) {
	path = appdata.PathKey(path)
	for rank, index := range indexes {
		pos, listed := index.order[path]
		if !listed {
			continue
		}
		rel := strings.ToLower(index.Files[pos].Rel)
		return index.Files[pos], index.rels[rel] <= pos && !slices.ContainsFunc(indexes[rank+1:], func(later *Index) bool {
			_, shipped := later.rels[rel]
			return shipped || slices.ContainsFunc(later.Replaced, func(folder string) bool {
				return strings.HasPrefix(rel, strings.ToLower(folder)+"/")
			})
		})
	}
	return inventory.File{}, false
}

// RefsInside is the references written inside an entry's definition, in its file's order; none for a
// definition that is no entry (a name script makes inside one).
func RefsInside(def At) (refs []At) {
	if def.index == nil || def.Occurrence().Entry != def.occurrence {
		return nil
	}
	for _, at := range def.index.InFile(def.File().Path) {
		if occurrence := at.Occurrence(); occurrence.Role == schema.Refers && occurrence.Entry == def.occurrence {
			refs = append(refs, at)
		}
	}
	return refs
}

// Names is every name defined in a database across the indexes, each once, the install's first.
func Names(indexes []*Index, database string) (names []string) {
	seen := map[string]bool{}
	for _, index := range indexes {
		ordinal, known := index.Databases.ordinals[database]
		for _, id := range index.byDatabase[uint16(ordinal)] {
			if name := index.IDs.Words[id]; known && !seen[name] {
				seen[name], names = true, append(names, name)
			}
		}
	}
	return names
}

// Resolve is a reference's definitions, the reference given as the walk produced it (from is its file's path,
// "" for none): a call's in the databases its calling word names; a scope saved from code, a loc key, a file
// path or a built-in widget kind known with no definitions; else Lookup. A local definition answers only a
// reference in its own file.
func Resolve(s *schema.Schema, indexes []*Index, ref schema.Occurrence, from string) (defs []At, known bool) {
	switch {
	case ref.Caller != "":
		for _, database := range s.Callees[ref.Caller] {
			defs = append(defs, Lookup(indexes, schema.Defines, database, ref.ID)...)
		}
		if len(defs) == 0 && ref.Caller == game.Shapes[game.ShapeWidget].Caller && s.WidgetKinds[ref.ID] {
			return nil, true
		}
	case ref.Database == game.SavedScopes && s.Saved[ref.ID]:
		return nil, true
	case ref.Database == game.Localization:
		return nil, slices.ContainsFunc(indexes, func(index *Index) bool {
			return len(index.listed(schema.Localizes, game.Localization, ref.ID)) > 0
		})
	case ref.Database == game.File:
		_, listed := FileAt(indexes, ref.ID)
		return nil, listed
	default:
		defs = Lookup(indexes, schema.Defines, ref.Database, ref.ID)
	}
	defs = slices.DeleteFunc(defs, func(def At) bool { return def.Occurrence().Local && def.File().Path != from })
	return defs, len(defs) > 0
}

// RefsTo turns Resolve around: it returns every reference to a name of a database, the calls whose
// calling word resolves in that database included.
func RefsTo(s *schema.Schema, indexes []*Index, database, id string) []At {
	refs := Lookup(indexes, schema.Refers, database, id)
	for _, call := range Lookup(indexes, schema.Refers, "", id) {
		if slices.Contains(s.Callees[call.Occurrence().Caller], database) {
			refs = append(refs, call)
		}
	}
	return refs
}

// Winner applies the entry modes to a name's definitions in Lookup's order, plain then each mode's:
// the winner, its adds, INJECT/REPLACE orphans with no target, and whether two mods contest it.
func Winner(info *game.GameInfo, defs []At, origin func(path string) string) (winner At, adds, orphans []At, contested bool) {
	// in a database of on_actions every plain definition after the first adds, as an inject does; a moded one
	// follows its mode
	merged := map[string]bool{}
	for _, row := range info.Descriptors {
		merged[row.Database] = row.Purpose == game.PurposeOnActions
	}

	found := false
	for _, mode := range slices.Concat([]game.EntryMode{{}}, info.EntryModes) {
		plain := mode.Word == ""
		for _, at := range defs {
			def := at.Occurrence()
			if def.Mode != mode.Word {
				continue
			}

			// a plain on_action, a name an entry gives (Named) and a local declaration take no place
			contends := (plain && !merged[def.Database] || mode.Replaces) && !def.IsName && !def.Local

			// two mods contest when one takes, or tries to take, the other's place
			if found && contends {
				incoming, current := origin(at.File().Path), origin(winner.File().Path)
				if incoming != current && incoming != game.OriginVanilla && current != game.OriginVanilla {
					contested = true
				}
			}

			switch {
			case !found:
				// nothing to act on: the definition creates its own, or, unless a TRY_ form, is an orphan
				if plain || mode.Creates {
					winner, found = at, true
				} else if !mode.Optional {
					orphans = append(orphans, at)
				}
			case plain && slices.Contains(info.Layout.FirstWins, def.Database):
				// the earlier plain definition holds
			case contends:
				winner, adds = at, nil
			default:
				adds = append(adds, at)
			}
		}
	}
	return winner, adds, orphans, contested
}

// Fires is whether a reference fires its target: an event or on_action outside a trigger.
func Fires(s *schema.Schema, ref schema.Occurrence) bool {
	database := s.Databases[ref.Database]
	return !ref.InTrigger && database != nil && (database.Descriptor.Purpose == game.PurposeEvents || database.Descriptor.Purpose == game.PurposeOnActions)
}

// Runs is whether a reference outside a trigger runs a scripted entry, so what it fires is fired
// through it: a call, or a name of a database called in effects.
func Runs(s *schema.Schema, ref schema.Occurrence) bool {
	database := s.Databases[ref.Database]
	return !ref.InTrigger && (ref.Caller != "" || database != nil && slices.Contains(database.Descriptor.Callers, game.Shapes[game.ShapeEffects].Caller))
}

// Scripted is whether a definition is a scripted entry, one a reference may run (Runs): of a
// database some block calls.
func Scripted(s *schema.Schema, def schema.Occurrence) bool {
	database := s.Databases[def.Database]
	return database != nil && len(database.Descriptor.Callers) > 0
}
