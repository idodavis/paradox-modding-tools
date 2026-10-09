// store.go is what a scan keeps: each file's records, and the store that lists them by database and
// name.

// Package index holds, per file, the definitions, references and localization entries a walk
// produced. Names resolve at query time across indexes, never when a file is added.
package index

import (
	"iter"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/appdata"
	"paradox-modding-tools/services/internal/inventory"
	"paradox-modding-tools/services/internal/jomini"
)

// Def is one definition of a name; Mode is the entry word written before it, "" for none.
type Def struct {
	Database, Name, Mode, Path string
	Range                      jomini.Range
	Line, Col                  int32 // where Range starts, 0-based
	Local                      bool  // declared inline by a folder row's Keyword: seen only in its file
	Named                      bool  // a name an entry's Names key gives its database, not an entry
}

// Ref is a name the walk reads as naming something: an entry of Database (schema.Localization and
// schema.File included), or a call, Called set and Database "".
type Ref struct {
	Database, Name, Path string
	Called               string // a call's CalledIn word (trigger, effect, value); game.AnyCall any
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
	scoped map[[2]string]bool // the scope pairs Collect collects (scopePair), moved into Index.Scoped by Add
}

// Index is one inventory with the records of its files.
type Index struct {
	Files    []inventory.File     // the inventory, as inventory.Files lists it
	Roots    []string             // the folders Files lists, in load order; a path under one belongs to it
	Replaced []string             // mods: folders whose earlier files the game does not load
	Records  map[string]*Records  // by path
	Repeated map[string]bool      // databases where two of its files define one name; Ready resets, Add marks
	Scoped   map[[2]string]bool   // each scope pair its files write (WritesInScope); kept in the cache, never reset
	order    map[string]int       // a lower-case path → position in Files
	rels     map[string]int       // a lower-case Rel → the last position in Files holding it
	defs     map[[2]string][]*Def // by database and name
	refs     map[[2]string][]*Ref // by database and name
	locs     map[string][]*Loc    // by key
	shared   map[string]string    // the one kept copy of each record string
}

// Add stores a file's records (the caller drops old ones first) and lists them by name; record
// strings share one copy per index, so no record pins a file's text.
func (index *Index) Add(records *Records, path string) {
	if index.defs == nil {
		index.Rebuild()
	}
	share := func(text string) string {
		if kept, found := index.shared[text]; found {
			return kept
		}
		text = strings.Clone(text)
		index.shared[text] = text
		return text
	}
	index.Records[path] = records
	for pair := range records.scoped {
		index.Scoped[[2]string{share(pair[0]), pair[1]}] = true
	}
	records.scoped = nil
	for n := range records.Defs {
		def := &records.Defs[n]
		def.Database, def.Name, def.Mode, def.Path = share(def.Database), share(def.Name), share(def.Mode), path
		key := [2]string{def.Database, def.Name}
		if list := index.defs[key]; def.Name != "" && len(list) > 0 && list[0].Path != def.Path {
			index.Repeated[def.Database] = true
		}
		index.defs[key] = append(index.defs[key], def)
	}
	for n := range records.Refs {
		ref := &records.Refs[n]
		ref.Database, ref.Name, ref.Called, ref.Path = share(ref.Database), share(ref.Name), share(ref.Called), path
		key := [2]string{ref.Database, ref.Name}
		index.refs[key] = append(index.refs[key], ref)
	}
	for n := range records.Loc {
		entry := &records.Loc[n]
		entry.Key, entry.Language, entry.Path = share(entry.Key), share(entry.Language), path
		index.locs[entry.Key] = append(index.locs[entry.Key], entry)
	}
}

// Drop removes a file's records and their entries in the per-name lists.
func (index *Index) Drop(path string) {
	records := index.Records[path]
	if records == nil {
		return
	}
	for n := range records.Defs {
		def := &records.Defs[n]
		key := [2]string{def.Database, def.Name}
		if list := slices.DeleteFunc(index.defs[key], func(other *Def) bool { return other == def }); len(list) > 0 {
			index.defs[key] = list
		} else {
			delete(index.defs, key)
		}
	}
	for n := range records.Refs {
		ref := &records.Refs[n]
		key := [2]string{ref.Database, ref.Name}
		if list := slices.DeleteFunc(index.refs[key], func(other *Ref) bool { return other == ref }); len(list) > 0 {
			index.refs[key] = list
		} else {
			delete(index.refs, key)
		}
	}
	for n := range records.Loc {
		entry := &records.Loc[n]
		if list := slices.DeleteFunc(index.locs[entry.Key], func(other *Loc) bool { return other == entry }); len(list) > 0 {
			index.locs[entry.Key] = list
		} else {
			delete(index.locs, entry.Key)
		}
	}
	delete(index.Records, path)
}

// Rebuild builds the lookup maps again from Files and Records, after a cache load; Add keeps them
// current. A record of a path no longer in Files is dropped.
func (index *Index) Rebuild() {
	cached := index.Records
	index.Records = make(map[string]*Records, len(cached))
	index.defs, index.refs, index.locs, index.shared = map[[2]string][]*Def{}, map[[2]string][]*Ref{}, map[string][]*Loc{}, map[string]string{}
	index.Repeated = map[string]bool{}
	if index.Scoped == nil {
		index.Scoped = map[[2]string]bool{}
	}
	index.PlaceFiles()
	for _, file := range index.Files {
		if records := cached[file.Path]; records != nil {
			index.Add(records, file.Path)
		}
	}
}

// PlaceFiles records where each file stands in Files. A copy given another load order calls it
// alone, so the records it shares with the live index are never written.
func (index *Index) PlaceFiles() {
	index.order, index.rels = make(map[string]int, len(index.Files)), make(map[string]int, len(index.Files))
	for pos, file := range index.Files {
		index.order[appdata.PathKey(file.Path)] = pos
		index.rels[strings.ToLower(file.Rel)] = pos
	}
}

// At is the file listed at a path, by its appdata.PathKey (on Windows, whatever the case it is
// spelled in).
func (index *Index) At(path string) (inventory.File, bool) {
	pos, listed := index.order[appdata.PathKey(path)]
	if !listed {
		return inventory.File{}, false
	}
	return index.Files[pos], true
}

// Forget removes a deleted file, or every file under a deleted folder, from the index: their
// records and their places in Files, so they no longer hide files at their Rels.
func (index *Index) Forget(path string) {
	path = appdata.PathKey(path)
	listed := len(index.Files)
	index.Files = slices.DeleteFunc(index.Files, func(file inventory.File) bool {
		at := appdata.PathKey(file.Path)
		return at == path || strings.HasPrefix(at, path+string(filepath.Separator))
	})
	if len(index.Files) != listed {
		index.Rebuild()
	}
}

// WritesInScope reports whether a file of the index writes a command, link or link-read word in a
// scope: the install's pairs are vanilla's evidence, and wrong-scope accuses only a pair vanilla
// never writes.
func (index *Index) WritesInScope(written, scope string) bool {
	return index.Scoped[scopePair(written, scope)]
}

// LocKeys is every key a loc entry has, in no order.
func (index *Index) LocKeys() iter.Seq[string] {
	return maps.Keys(index.locs)
}
