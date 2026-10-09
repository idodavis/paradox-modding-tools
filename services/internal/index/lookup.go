// lookup.go answers the editor's and the pages' questions from the store, in the game's load order.

package index

import (
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/appdata"
	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/inventory"
	"paradox-modding-tools/services/internal/schema"
)

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

// inLoadOrder returns one name's items across the indexes in the game's load order, by Rel, those
// in files the game loads.
func inLoadOrder[Item any](indexes []*Index, list func(index *Index) []*Item, path func(item *Item) string) []Item {
	type placed struct {
		item *Item
		rel  string
	}
	var found []placed
	for _, index := range indexes {
		for _, item := range list(index) {
			if file, loaded := Loaded(indexes, path(item)); loaded {
				found = append(found, placed{item: item, rel: strings.ToLower(file.Rel)})
			}
		}
	}

	// equal Rels left are one file's items, kept in source order
	slices.SortStableFunc(found, func(first, second placed) int { return strings.Compare(first.rel, second.rel) })
	items := make([]Item, len(found))
	for i, entry := range found {
		items[i] = *entry.item
	}
	return items
}

// Lookup is every definition of a name across the indexes, in load order.
func Lookup(indexes []*Index, database, name string) []Def {
	return inLoadOrder(indexes, func(index *Index) []*Def { return index.defs[[2]string{database, name}] },
		func(def *Def) string { return def.Path })
}

// LookupRefs is every reference to a name in a file the game loads, in load order.
func LookupRefs(indexes []*Index, database, name string) []Ref {
	return inLoadOrder(indexes, func(index *Index) []*Ref { return index.refs[[2]string{database, name}] },
		func(ref *Ref) string { return ref.Path })
}

// RefsInside returns the references written inside a definition, in its file's order.
func RefsInside(indexes []*Index, def Def) (refs []Ref) {
	for _, index := range indexes {
		if records := index.Records[def.Path]; records != nil {
			at := slices.Index(records.Defs, def)
			for _, ref := range records.Refs {
				if at >= 0 && int(ref.From) == at {
					refs = append(refs, ref)
				}
			}
		}
	}
	return refs
}

// LookupLocs is every localization entry of a key across the indexes, in load order.
func LookupLocs(indexes []*Index, key string) []Loc {
	return inLoadOrder(indexes, func(index *Index) []*Loc { return index.locs[key] },
		func(entry *Loc) string { return entry.Path })
}

// Names is every name defined in a database across the indexes, in load order, each once.
func Names(indexes []*Index, database string) []string {
	defs := inLoadOrder(indexes, func(index *Index) (found []*Def) {
		for _, file := range index.Files {
			records := index.Records[file.Path]
			for i := 0; records != nil && i < len(records.Defs); i++ {
				if records.Defs[i].Database == database {
					found = append(found, &records.Defs[i])
				}
			}
		}
		return found
	}, func(def *Def) string { return def.Path })

	seen, names := map[string]bool{}, []string(nil)
	for _, def := range defs {
		if def.Name != "" && !seen[def.Name] {
			seen[def.Name], names = true, append(names, def.Name)
		}
	}
	return names
}

// Resolve is a reference's definitions: a call's in the databases its CalledIn word names; a scope
// saved from code, a loc key or a file path known with no definitions; else Lookup. A local
// definition answers only a reference in its own file.
func Resolve(s *schema.Schema, indexes []*Index, ref Ref) (defs []Def, known bool) {
	switch {
	case ref.Called != "":
		for _, database := range s.Callees[ref.Called] {
			defs = append(defs, Lookup(indexes, database, ref.Name)...)
		}
	case ref.Database == game.SavedScopes && s.Saved[ref.Name]:
		return nil, true
	case ref.Database == schema.Localization:
		return nil, slices.ContainsFunc(indexes, func(index *Index) bool { return len(index.locs[ref.Name]) > 0 })
	case ref.Database == schema.File:
		_, listed := FileAt(indexes, ref.Name)
		return nil, listed
	default:
		defs = Lookup(indexes, ref.Database, ref.Name)
	}
	defs = slices.DeleteFunc(defs, func(def Def) bool { return def.Local && def.Path != ref.Path })
	return defs, len(defs) > 0
}

// RefsTo turns Resolve around: it returns every reference to a name of a database, the calls whose
// CalledIn word resolves in that database included.
func RefsTo(s *schema.Schema, indexes []*Index, database, name string) []Ref {
	refs := LookupRefs(indexes, database, name)
	for _, call := range LookupRefs(indexes, "", name) {
		if slices.Contains(s.Callees[call.Called], database) {
			refs = append(refs, call)
		}
	}
	return refs
}

// Winner applies the entry modes to a name's definitions in Lookup's order, plain then each mode's:
// the winner, its adds, INJECT/REPLACE orphans with no target, and whether two mods contest it.
func Winner(info *game.GameInfo, defs []Def, origin func(path string) string) (winner Def, adds, orphans []Def, contested bool) {
	// in a database whose registry row has OnActions every plain definition after the first adds,
	// as an inject does; a moded one follows its mode
	merged := map[string]bool{}
	for at, row := range info.Folders {
		merged[schema.NameOf(at)] = row.OnActions
	}

	found := false
	for _, mode := range slices.Concat([]game.EntryMode{{}}, info.EntryModes) {
		plain := mode.Word == ""
		for _, def := range defs {
			if def.Mode != mode.Word {
				continue
			}

			// a plain on_action, a name an entry gives (Named) and a local declaration take no place
			contends := (plain && !merged[def.Database] || mode.Replaces) && !def.Named && !def.Local

			// two mods contest when one takes, or tries to take, the other's place
			if found && contends {
				incoming, current := origin(def.Path), origin(winner.Path)
				if incoming != current && incoming != game.OriginVanilla && current != game.OriginVanilla {
					contested = true
				}
			}

			switch {
			case !found:
				// nothing to act on: the definition creates its own, or, unless a TRY_ form, is an orphan
				if plain || mode.Creates {
					winner, found = def, true
				} else if !mode.Optional {
					orphans = append(orphans, def)
				}
			case plain && slices.Contains(info.FirstWinsDatabases, def.Database):
				// the earlier plain definition holds
			case contends:
				winner, adds = def, nil
			default:
				adds = append(adds, def)
			}
		}
	}
	return winner, adds, orphans, contested
}

// Fires is whether a reference fires its target: an event or on_action outside a trigger.
func (r Ref) Fires(s *schema.Schema) bool {
	database := s.Databases[r.Database]
	return !r.InTrigger && database != nil && (database.Row.Events || database.Row.OnActions)
}

// Runs is whether a reference outside a trigger runs a scripted entry, so what it fires is fired
// through it: a call, or a name of a database called in effects.
func (r Ref) Runs(s *schema.Schema) bool {
	database := s.Databases[r.Database]
	return !r.InTrigger && (r.Called != "" || database != nil && slices.Contains(database.Row.CalledIn, game.Blocks[game.BlockEffects].Calls))
}

// Scripted is whether a definition is a scripted entry, one a reference may run (Runs): of a
// database some block calls.
func (d Def) Scripted(s *schema.Schema) bool {
	database := s.Databases[d.Database]
	return database != nil && len(database.Row.CalledIn) > 0
}
