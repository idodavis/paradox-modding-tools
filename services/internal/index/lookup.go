// lookup.go answers the editor's and the pages' questions from the store, in the game's load order.

package index

import (
	"cmp"
	"path"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/inventory"
	"paradox-modding-tools/services/internal/schema"
)

// Lookup returns every occurrence of a role of a name across the indexes in the game's load order (the files the game
// loads, by Rel, each in source order); a call is looked up with Database "".
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

// listed returns the index's own occurrences of a role of a name, in file order; none for a name it never met.
func (index *Index) listed(role schema.Role, database, id string) []At {
	databaseOrdinal, inDatabases := index.Databases.ordinals[database]
	idOrdinal, inIDs := index.IDs.ordinals[id]
	if !inDatabases || !inIDs {
		return nil
	}
	return index.postings[posting{role, uint16(databaseOrdinal), uint32(idOrdinal)}]
}

// FileAt returns the file at a Rel across the indexes, read as the game reads a path (any case, doubled slashes, a
// leading slash); the last index's that lists it, at its last place there.
func FileAt(indexes []*Index, rel string) (inventory.File, bool) {
	key := strings.TrimPrefix(path.Clean("/"+strings.ToLower(rel)), "/")
	for _, index := range slices.Backward(indexes) {
		if pos, listed := index.rels[key]; listed {
			return index.Files[pos], true
		}
	}
	return inventory.File{}, false
}

// Loaded returns the file listed at a path of the indexes and whether the game loads it: a later file at its Rel, or
// a later index's Replaced, hides it.
func Loaded(indexes []*Index, path string) (inventory.File, bool) {
	path = inventory.PathKey(path)
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

// RefsInside returns the references written inside an entry's definition, in its file's order; none for a definition
// that is no entry (a name script makes inside one).
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

// Names returns every name defined in a database across the indexes, each once, the install's first.
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

// Resolve returns a reference's definitions (from is its file's path): a call's in the databases its caller names, else
// Lookup's; a saved scope, loc key, file or widget kind is known with none. A local one answers its own file only.
func Resolve(s *schema.Schema, indexes []*Index, ref schema.Occurrence, from string) (defs []At, known bool) {
	switch {
	case ref.Caller != "":
		for _, database := range s.Callees[ref.Caller] {
			defs = append(defs, Lookup(indexes, schema.Defines, database, ref.ID)...)
		}
		if len(defs) == 0 && ref.Caller == game.Shapes[game.ShapeWidget].Caller && s.WidgetTypes[ref.ID] {
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

// Winner applies the entry modes to a name's definitions in Lookup's order and returns the winner, its adds, orphans
// with no target and whether two mods contest it; the caller gives the database's precedence.
func Winner(info *game.Game, defs []At, origin func(path string) string, precedence func(database string) game.Precedence,
) (winner At, adds, orphans []At, contested bool) {
	combines := make([]game.Precedence, len(defs))
	for i, at := range defs {
		combines[i] = precedence(at.Occurrence().Database)
	}
	found := false
	for _, mode := range slices.Concat([]game.EntryMode{{}}, info.EntryModes) {
		plain := mode.Prefix == ""
		for i, at := range defs {
			def := at.Occurrence()
			if def.Mode != mode.Prefix {
				continue
			}
			merged := len(combines[i].Adds) > 0

			// a plain on_action, a name an entry gives (Named) and a local declaration take no place
			contends := (plain && !merged || mode.Replaces) && !def.IsName && !def.Local

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
			case plain && combines[i].Keeps == "first":
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

// Fires reports whether a reference fires its target: an event or on_action outside a trigger.
func Fires(s *schema.Schema, ref schema.Occurrence) bool {
	database := s.Databases[ref.Database]
	return !ref.InTrigger && database != nil && (database.Rule.Houses == "events" || database.Rule.Houses == "on_actions")
}

// Runs reports whether a reference outside a trigger runs a scripted entry (a call, or a name of a database called in
// effects), so what it fires is fired through it.
func Runs(s *schema.Schema, ref schema.Occurrence) bool {
	database := s.Databases[ref.Database]
	return !ref.InTrigger && (ref.Caller != "" || database != nil && slices.Contains(database.Rule.CallerWords, game.Shapes[game.ShapeEffects].Caller))
}

// Scripted reports whether a definition is a scripted entry a reference may run (Runs): one of a database some block
// calls.
func Scripted(s *schema.Schema, def schema.Occurrence) bool {
	database := s.Databases[def.Database]
	return database != nil && len(database.Rule.CallerWords) > 0
}
