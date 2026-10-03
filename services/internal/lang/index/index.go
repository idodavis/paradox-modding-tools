// Package index holds, per file, the definitions, references and localization entries a walk
// produced. Names resolve at query time across indexes, never when a file is added.
package index

import (
	"iter"
	"maps"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/schema"
	"paradox-modding-tools/services/internal/lang/source"
)

// Localization and File are the databases a loc key and a file path name; no definition carries
// them, so Resolve answers them from the loc entries and the inventories. Parameter holds a
// scripted entry's $NAME$ parameters, each named <entry>:<NAME> where defined and where passed.
const (
	Localization = "localization"
	File         = "file"
	Parameter    = "parameter"
)

// Def is one definition of a name; Mode is the entry word written before it, "" for none.
type Def struct {
	Database, Name, Mode, Path string
	Range                      jomini.Range
	Line, Col                  int32 // where Range starts, 0-based
}

// Ref is a name the walk reads as naming something: an entry of Database (Localization and File
// included), or a call, Called set and Database "".
type Ref struct {
	Database, Name, Path string
	Called               string // the block kind a call is written in (trigger, effect); "*" undeclared (D-29)
	Range                jomini.Range
	Line, Col            int32 // where Range starts, 0-based
	From                 int32 // the enclosing definition's index in its file's Records.Defs, -1 none
	InTrigger            bool
}

// Loc is one localization entry.
type Loc struct {
	Key, Language, Text, Path string
	Line                      int
}

// Records is one file's definitions, references and localization entries, never changed once added.
type Records struct {
	Defs   []Def
	Refs   []Ref
	Loc    []Loc
	scoped map[[2]string]bool // the walk's Pairs, moved into Index.Scoped by Add
}

// Index is one inventory with the records of its files.
type Index struct {
	Files    []source.File        // the inventory, as source.Inventory lists it
	Roots    []string             // the folders Files lists, in load order; a path under one belongs to it
	Replaced []string             // mods: folders whose earlier files the game does not load
	Records  map[string]*Records  // by path
	Repeated map[string]bool      // databases where two of its files define one name; Ready resets, Add marks
	Scoped   map[[2]string]bool   // each Pair its files write; kept in the cache, never reset (D-34)
	order    map[string]int       // a lower-case path → position in Files
	rels     map[string]int       // a lower-case Rel → the last position in Files holding it
	defs     map[[2]string][]*Def // by database and name
	refs     map[[2]string][]*Ref // by database and name
	locs     map[string][]*Loc    // by key
}

// Add stores a file's records and lists its definitions, references and loc entries by name. A
// path already added is dropped first by the caller.
func (idx *Index) Add(records *Records, path string) {
	if idx.defs == nil {
		idx.Ready()
	}
	idx.Records[path] = records
	if records.scoped != nil {
		maps.Copy(idx.Scoped, records.scoped)
		records.scoped = nil
	}
	for i := range records.Defs {
		def := &records.Defs[i]
		key := [2]string{def.Database, def.Name}
		if list := idx.defs[key]; def.Name != "" && len(list) > 0 && list[0].Path != def.Path {
			idx.Repeated[def.Database] = true
		}
		idx.defs[key] = append(idx.defs[key], def)
	}
	for i := range records.Refs {
		ref := &records.Refs[i]
		key := [2]string{ref.Database, ref.Name}
		idx.refs[key] = append(idx.refs[key], ref)
	}
	for i := range records.Loc {
		entry := &records.Loc[i]
		idx.locs[entry.Key] = append(idx.locs[entry.Key], entry)
	}
}

// Drop removes a file's records and their entries in the per-name lists.
func (idx *Index) Drop(path string) {
	records := idx.Records[path]
	if records == nil {
		return
	}
	for i := range records.Defs {
		def := &records.Defs[i]
		key := [2]string{def.Database, def.Name}
		if list := slices.DeleteFunc(idx.defs[key], func(other *Def) bool { return other == def }); len(list) > 0 {
			idx.defs[key] = list
		} else {
			delete(idx.defs, key)
		}
	}
	for i := range records.Refs {
		ref := &records.Refs[i]
		key := [2]string{ref.Database, ref.Name}
		if list := slices.DeleteFunc(idx.refs[key], func(other *Ref) bool { return other == ref }); len(list) > 0 {
			idx.refs[key] = list
		} else {
			delete(idx.refs, key)
		}
	}
	for i := range records.Loc {
		entry := &records.Loc[i]
		if list := slices.DeleteFunc(idx.locs[entry.Key], func(other *Loc) bool { return other == entry }); len(list) > 0 {
			idx.locs[entry.Key] = list
		} else {
			delete(idx.locs, entry.Key)
		}
	}
	delete(idx.Records, path)
}

// Ready builds the lookup maps from Files and Records, after a cache load; Add keeps them current.
// A record of a path no longer in Files is dropped.
func (idx *Index) Ready() {
	cached := idx.Records
	idx.Records = make(map[string]*Records, len(cached))
	idx.order, idx.rels = make(map[string]int, len(idx.Files)), make(map[string]int, len(idx.Files))
	idx.defs, idx.refs, idx.locs = map[[2]string][]*Def{}, map[[2]string][]*Ref{}, map[string][]*Loc{}
	idx.Repeated = map[string]bool{}
	if idx.Scoped == nil {
		idx.Scoped = map[[2]string]bool{}
	}
	for i, file := range idx.Files {
		idx.order[strings.ToLower(file.Path)] = i
		idx.rels[strings.ToLower(file.Rel)] = i
		if records := cached[file.Path]; records != nil {
			idx.Add(records, file.Path)
		}
	}
}

// At is the file listed at a path, whatever the case it is spelled in (Windows spells one by any).
func (idx *Index) At(path string) (source.File, bool) {
	pos, listed := idx.order[strings.ToLower(path)]
	if !listed {
		return source.File{}, false
	}
	return idx.Files[pos], true
}

// FileAt is the file at a Rel across the indexes, whatever its case: the last index's that lists
// it, at its last place there.
func FileAt(indexes []*Index, rel string) (source.File, bool) {
	for _, idx := range slices.Backward(indexes) {
		if pos, listed := idx.rels[strings.ToLower(rel)]; listed {
			return idx.Files[pos], true
		}
	}
	return source.File{}, false
}

// Forget removes a deleted file from the index: its records and its place in Files, so it no longer
// hides a file at its Rel.
func (idx *Index) Forget(path string) {
	pos, listed := idx.order[strings.ToLower(path)]
	if !listed {
		return
	}
	idx.Files = slices.Delete(idx.Files, pos, pos+1)
	idx.Ready()
}

// LocKeys is every key a loc entry has, in no order.
func (idx *Index) LocKeys() iter.Seq[string] {
	return maps.Keys(idx.locs)
}

// Loaded is the file listed at a path of the indexes and whether the game loads it (D-30): it is
// hidden by a later file at its Rel, in its index or a later one, or by a later index's Replaced.
func Loaded(indexes []*Index, path string) (source.File, bool) {
	path = strings.ToLower(path)
	for rank, idx := range indexes {
		pos, listed := idx.order[path]
		if !listed {
			continue
		}
		rel := strings.ToLower(idx.Files[pos].Rel)
		return idx.Files[pos], idx.rels[rel] <= pos && !slices.ContainsFunc(indexes[rank+1:], func(later *Index) bool {
			_, shipped := later.rels[rel]
			return shipped || slices.ContainsFunc(later.Replaced, func(folder string) bool {
				return strings.HasPrefix(rel, strings.ToLower(folder)+"/")
			})
		})
	}
	return source.File{}, false
}

// gather is one name's items across the indexes in the game's load order (D-30), by Rel, those in
// files the game loads.
func gather[Item any](indexes []*Index, list func(idx *Index) []*Item, path func(item *Item) string) []Item {
	type placed struct {
		item *Item
		rel  string
	}
	var found []placed
	for _, idx := range indexes {
		for _, item := range list(idx) {
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
	return gather(indexes, func(idx *Index) []*Def { return idx.defs[[2]string{database, name}] },
		func(def *Def) string { return def.Path })
}

// LookupRefs is every reference to a name across the indexes, in load order and hidden alike.
func LookupRefs(indexes []*Index, database, name string) []Ref {
	return gather(indexes, func(idx *Index) []*Ref { return idx.refs[[2]string{database, name}] },
		func(ref *Ref) string { return ref.Path })
}

// LookupLocs is every localization entry of a key across the indexes, in load order.
func LookupLocs(indexes []*Index, key string) []Loc {
	return gather(indexes, func(idx *Index) []*Loc { return idx.locs[key] },
		func(entry *Loc) string { return entry.Path })
}

// Names is every name defined in a database across the indexes, in load order, each once.
func Names(indexes []*Index, database string) []string {
	defs := gather(indexes, func(idx *Index) (found []*Def) {
		for _, file := range idx.Files {
			records := idx.Records[file.Path]
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

// Resolve is the one resolver of a reference: a call by its databases' CalledIn (D-24, D-29); a
// scope saved from code, a loc key or a file path known with no definitions; else Lookup.
func Resolve(sch *schema.Schema, indexes []*Index, ref Ref) (defs []Def, known bool) {
	switch {
	case ref.Called != "":
		for _, database := range sch.Callees[ref.Called] {
			defs = append(defs, Lookup(indexes, database, ref.Name)...)
		}
	case ref.Database == game.NameCommands["save_scope_as"] && sch.Saved[ref.Name]:
		return nil, true
	case ref.Database == Localization:
		return nil, slices.ContainsFunc(indexes, func(idx *Index) bool { return len(idx.locs[ref.Name]) > 0 })
	case ref.Database == File:
		_, listed := FileAt(indexes, ref.Name)
		return nil, listed
	default:
		defs = Lookup(indexes, ref.Database, ref.Name)
	}
	return defs, len(defs) > 0
}

// Winner applies the entry modes to a name's definitions in Lookup's order, plain then each mode's:
// the winner, its adds, INJECT/REPLACE orphans with no target, and whether two mods contest it.
func Winner(info *game.GameInfo, defs []Def, origin func(path string) string) (winner Def, adds, orphans []Def, contested bool) {
	found := false
	for _, mode := range slices.Concat([]game.EntryMode{{}}, info.EntryModes) {
		plain := mode.Word == ""
		contends := plain || mode.Replaces
		for _, def := range defs {
			if def.Mode != mode.Word {
				continue
			}

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
			case plain && game.FirstWinsAt(info.ID, def.Database):
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
func (ref Ref) Fires(sch *schema.Schema) bool {
	database := sch.Databases[ref.Database]
	return !ref.InTrigger && database != nil && (database.Naming.Events || database.Naming.OnActions)
}

// Runs is whether a reference outside a trigger runs a scripted entry, so what it fires is fired
// through it: a call, or a name of a database called in effects.
func (ref Ref) Runs(sch *schema.Schema) bool {
	database := sch.Databases[ref.Database]
	return !ref.InTrigger && (ref.Called != "" || database != nil && slices.Contains(database.Naming.CalledIn, "effect"))
}
