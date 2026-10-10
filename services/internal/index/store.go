// store.go is what a scan keeps: every file's occurrences, the tables their ordinals point into, and the
// maps that list them by name, rebuilt at load.

// Package index holds, per file, the occurrences a walk produced: the definitions, references and
// localization entries. Names resolve at query time across indexes, never when a file is added.
package index

import (
	"iter"
	"path/filepath"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/appdata"
	"paradox-modding-tools/services/internal/inventory"
	"paradox-modding-tools/services/internal/jomini"
	"paradox-modding-tools/services/internal/schema"
)

// Flags are the bits on an occurrence.
type Flags uint8

// The flags: written inside a trigger; a definition declared inline by a Keyword, seen only in its file; a
// definition an entry's Names key gives its database rather than an entry of its own.
const (
	InTrigger Flags = 1 << iota
	Local
	Named
)

// Occurrence is the stored form of a schema.Occurrence, pointer-free: its strings are ordinals into the
// index's tables ("" is 0).
type Occurrence struct {
	Range     jomini.Range
	Line, Col int32
	Database  uint16
	ID        uint32
	Caller    uint8
	Mode      uint8
	Language  uint8 // Localizes: the entry's language
	Role      schema.Role
	Flags     Flags
	Entry     int32 // as schema.Occurrence.Entry
	Text      int32 // Localizes: into the file's Texts; -1 none
}

// At is a handle to one occurrence: its index, its file's position in Files and its position in the file. The
// zero At is no occurrence: its accessors give zero values.
type At struct {
	index            *Index
	file, occurrence int32
}

// table is one symbol table: the words by ordinal, and each word's ordinal while adding.
type table struct {
	Words    []string
	ordinals map[string]int
}

// posting keys the occurrences of one role of one name.
type posting struct {
	role     schema.Role
	database uint16
	id       uint32
}

// Index is one inventory with its files' occurrences; the maps and flags are rebuilt at load, never cached.
type Index struct {
	Files       []inventory.File   // the inventory, as inventory.Files lists it
	Roots       []string           // the folders Files lists, in load order; a path under one belongs to it
	Replaced    []string           // mods: folders whose earlier files the game does not load
	Occurrences [][]Occurrence     // per file, as Files lists them; a file's entries come first, in source order
	Texts       [][]string         // per file, the localization texts its Localizes occurrences point to
	Databases   table              // the tables the occurrences' ordinals point into
	IDs         table              //
	Callers     table              //
	Modes       table              //
	Languages   table              //
	Scoped      map[[2]string]bool // each scope pair its files write (WritesInScope); kept in the cache, never reset
	order       map[string]int     // a lower-case path → position in Files
	rels        map[string]int     // a lower-case Rel → the last position in Files holding it
	postings    map[posting][]At   // a role of a name → its occurrences, in file order
	byDatabase  map[uint16][]uint32
	repeated    map[uint16]bool // databases where two files define one name
}

// collected is one file's occurrences as a worker collected them, before the index interns their strings.
type collected struct {
	found    []schema.Occurrence
	texts    []string // the localization texts, in the Localizes occurrences' order
	language string   // a localization file's language
	scoped   map[[2]string]bool
}

// Rebuild builds the tables' maps, the postings, the by-database lists and the repeated flags again from Files
// and Occurrences, after a cache load or a Forget; add keeps them current. A file without occurrences keeps an
// empty slot, so Occurrences stays parallel to Files.
func (index *Index) Rebuild() {
	index.PlaceFiles()
	for _, table := range []*table{&index.Databases, &index.IDs, &index.Callers, &index.Modes, &index.Languages} {
		if len(table.Words) == 0 {
			table.Words = []string{""} // "" is always ordinal 0
		}
		table.ordinals = make(map[string]int, len(table.Words))
		for ordinal, word := range table.Words {
			table.ordinals[word] = ordinal
		}
	}
	index.postings, index.byDatabase, index.repeated = map[posting][]At{}, map[uint16][]uint32{}, map[uint16]bool{}
	if index.Scoped == nil {
		index.Scoped = map[[2]string]bool{}
	}
	index.Occurrences = slices.Grow(index.Occurrences[:min(len(index.Occurrences), len(index.Files))], len(index.Files))[:len(index.Files)]
	index.Texts = slices.Grow(index.Texts[:min(len(index.Texts), len(index.Files))], len(index.Files))[:len(index.Files)]
	for file := range index.Files {
		index.list(int32(file))
	}
}

// ordinal returns a word's ordinal in a table, adding the word when it is new; "" is always 0.
func (table *table) ordinal(word string) int {
	ordinal, known := table.ordinals[word]
	if !known {
		word, ordinal = strings.Clone(word), len(table.Words) // cloned, so no table pins a file's text
		table.Words, table.ordinals[word] = append(table.Words, word), ordinal
	}
	return ordinal
}

// PlaceFiles records where each file stands in Files. A copy given another load order calls it
// alone, so the occurrences it shares with the live index are never written.
func (index *Index) PlaceFiles() {
	index.order, index.rels = make(map[string]int, len(index.Files)), make(map[string]int, len(index.Files))
	for pos, file := range index.Files {
		index.order[appdata.PathKey(file.Path)] = pos
		index.rels[strings.ToLower(file.Rel)] = pos
	}
}

// list adds one file's occurrences to the postings and the by-database lists, and marks a database repeated
// when a second file defines one of its names.
func (index *Index) list(file int32) {
	for n, occurrence := range index.Occurrences[file] {
		key := posting{occurrence.Role, occurrence.Database, occurrence.ID}
		listed := index.postings[key]
		if occurrence.Role == schema.Defines && occurrence.ID != 0 {
			if len(listed) > 0 && listed[len(listed)-1].file != file {
				index.repeated[occurrence.Database] = true
			}
			if len(listed) == 0 {
				index.byDatabase[occurrence.Database] = append(index.byDatabase[occurrence.Database], occurrence.ID)
			}
		}
		index.postings[key] = append(listed, At{index, file, int32(n)})
	}
}

// add stores a file's occurrences (the caller drops old ones first): their strings interned into the tables,
// then listed; the file's scope pairs join Scoped.
func (index *Index) add(path string, file collected) {
	if index.postings == nil {
		index.Rebuild()
	}
	pos := index.order[appdata.PathKey(path)]
	stored, texts := make([]Occurrence, 0, len(file.found)), 0
	language := uint8(index.Languages.ordinal(file.language))
	for _, found := range file.found {
		occurrence := Occurrence{
			Range: found.Range, Line: found.Line, Col: found.Col, Role: found.Role, Entry: found.Entry, Text: -1,
			Database: uint16(index.Databases.ordinal(found.Database)), ID: uint32(index.IDs.ordinal(found.ID)),
			Caller: uint8(index.Callers.ordinal(found.Caller)), Mode: uint8(index.Modes.ordinal(found.Mode)),
		}
		if found.Role == schema.Localizes {
			occurrence.Text, occurrence.Language, texts = int32(texts), language, texts+1
		}
		for flag, set := range map[Flags]bool{InTrigger: found.InTrigger, Local: found.Local, Named: found.Named} {
			if set {
				occurrence.Flags |= flag
			}
		}
		stored = append(stored, occurrence)
	}
	index.Occurrences[pos], index.Texts[pos] = stored, file.texts
	for pair := range file.scoped {
		index.Scoped[[2]string{strings.Clone(pair[0]), pair[1]}] = true
	}
	index.list(int32(pos))
}

// Drop removes a file's occurrences and their listings.
func (index *Index) Drop(path string) {
	pos, listed := index.order[appdata.PathKey(path)]
	if !listed || len(index.Occurrences) <= pos {
		return
	}
	for _, occurrence := range index.Occurrences[pos] {
		key := posting{occurrence.Role, occurrence.Database, occurrence.ID}
		if kept := slices.DeleteFunc(index.postings[key], func(at At) bool { return int(at.file) == pos }); len(kept) > 0 {
			index.postings[key] = kept
		} else {
			delete(index.postings, key)
		}
	}
	index.Occurrences[pos], index.Texts[pos] = nil, nil
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

// InFile is every occurrence of the file at a path, in the file's order; none for a path not listed.
func (index *Index) InFile(path string) (found []At) {
	pos, listed := index.order[appdata.PathKey(path)]
	if !listed || len(index.Occurrences) <= pos {
		return nil
	}
	for n := range index.Occurrences[pos] {
		found = append(found, At{index, int32(pos), int32(n)})
	}
	return found
}

// Forget removes a deleted file, or every file under a deleted folder, from the index: their
// occurrences and their places in Files, so they no longer hide files at their Rels.
func (index *Index) Forget(path string) {
	path = appdata.PathKey(path)
	listed := len(index.Files)
	for pos := len(index.Files) - 1; pos >= 0; pos-- {
		at := appdata.PathKey(index.Files[pos].Path)
		if at == path || strings.HasPrefix(at, path+string(filepath.Separator)) {
			index.Files = slices.Delete(index.Files, pos, pos+1)
			index.Occurrences = slices.Delete(index.Occurrences, pos, pos+1)
			index.Texts = slices.Delete(index.Texts, pos, pos+1)
		}
	}
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

// LocKeys is every key a localization entry has, in no order.
func (index *Index) LocKeys() iter.Seq[string] {
	return func(yield func(string) bool) {
		for key, listed := range index.postings {
			if key.role == schema.Localizes && len(listed) > 0 && !yield(index.IDs.Words[key.id]) {
				return
			}
		}
	}
}

// Repeated reports whether two files of the index define one name of a database.
func (index *Index) Repeated(database string) bool {
	ordinal, known := index.Databases.ordinals[database]
	return known && index.repeated[uint16(ordinal)]
}

// Occurrence is the occurrence as the walk produced it, its strings read back from the tables.
func (at At) Occurrence() schema.Occurrence {
	index := at.index
	if index == nil {
		return schema.Occurrence{Entry: -1}
	}
	stored := &index.Occurrences[at.file][at.occurrence]
	return schema.Occurrence{
		Role: stored.Role, Database: index.Databases.Words[stored.Database], ID: index.IDs.Words[stored.ID],
		Caller: index.Callers.Words[stored.Caller], Range: stored.Range, Line: stored.Line, Col: stored.Col, Entry: stored.Entry,
		Mode: index.Modes.Words[stored.Mode], InTrigger: stored.Flags&InTrigger != 0, Local: stored.Flags&Local != 0, Named: stored.Flags&Named != 0,
	}
}

// File is the file the occurrence is in.
func (at At) File() inventory.File {
	if at.index == nil {
		return inventory.File{}
	}
	return at.index.Files[at.file]
}

// Entry is the definition of the entry the occurrence is in; false outside one. An entry's definition is its own.
func (at At) Entry() (At, bool) {
	if entry := at.Occurrence().Entry; entry >= 0 {
		return At{at.index, at.file, entry}, true
	}
	return At{}, false
}

// Text is a localization entry's text; "" for any other occurrence.
func (at At) Text() string {
	if at.index == nil {
		return ""
	}
	if text := at.index.Occurrences[at.file][at.occurrence].Text; text >= 0 {
		return at.index.Texts[at.file][text]
	}
	return ""
}

// Language is a localization entry's language; "" for any other occurrence.
func (at At) Language() string {
	if at.index == nil {
		return ""
	}
	return at.index.Languages.Words[at.index.Occurrences[at.file][at.occurrence].Language]
}
