// Package index records what a scan found in the files of an install or a workspace's mods: every definition,
// reference and localization message, with lookups by name. It decides which file and which definition the game
// keeps when several compete, lists what fires what, and loads an install's index from its cache.
package index

import (
	"maps"
	"slices"

	"github.com/samber/lo"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/jomini"
)

// A Role is what an occurrence does with its name: defines it, refers to it or localizes it.
type Role uint8

// The roles an occurrence can have.
const (
	RoleDefines   Role = iota // it defines the name in its database
	RoleRefers                // it refers to the name
	RoleLocalizes             // it is a localization message with the name as its key
)

// An Occurrence is the stored form of one definition, reference or message, compact enough to keep for every file
// of the game. Each string is stored as an ordinal into one of the index's tables.
type Occurrence struct {
	Span      jomini.Span
	Line      int32
	Database  uint16 // into Index.Databases
	Name      uint32 // into Index.Names
	Mode      uint8  // into Index.Modes; an entry mode's prefix
	Priority  int16  // CK3's override priority; 0 means none
	Language  uint8  // into Index.Languages; a localization file's
	Role      Role
	Local     bool
	Alias     bool  // whether it is a second name of the definition it sits in, not a definition of its own
	Nested    bool  // whether it sits inside a definition, the one Parent names
	Firing    bool  // whether a reference fires the event, on_action or decision it names
	Parent    int32 // the defining occurrence this one sits inside
	Statement int32 // the statement that writes it; 0, the file, for a definition named by its file, a message or a row
}

// A Table interns texts, so that an occurrence stores a small ordinal instead of a string.
type Table struct {
	Texts    []string
	ordinals map[string]uint32
}

// Intern returns the ordinal of a text, adding the text to the table if it is new.
func (table *Table) Intern(text string) uint32 {
	if ordinal, ok := table.ordinals[text]; ok {
		return ordinal
	}
	if table.ordinals == nil {
		table.ordinals = map[string]uint32{}
	}
	ordinal := uint32(len(table.Texts))
	table.Texts = append(table.Texts, text)
	table.ordinals[text] = ordinal
	return ordinal
}

// Lookup returns the ordinal of a text, or false if the text was never interned.
func (table *Table) Lookup(text string) (uint32, bool) {
	ordinal, ok := table.ordinals[text]
	return ordinal, ok
}

// clone returns a copy of a table that can be interned into without changing this one.
func (table *Table) clone() Table {
	return Table{Texts: slices.Clone(table.Texts), ordinals: maps.Clone(table.ordinals)}
}

// A Site is a handle to one occurrence. A mods' index lists the install's files first, so a site means the same file
// in both indexes.
type Site struct {
	File       int32 `json:"file"`
	Occurrence int32 `json:"occurrence"`
}

// A Posting names one group of occurrences: those of one role in one database.
type Posting struct {
	Role     Role
	Database uint16
}

// An Index is what a scan found in one list of files. A mods' index stands on the install's.
type Index struct {
	Game        string // the game's id
	Files       []files.File
	Roots       []files.Root   // in load order; a file under two belongs to the inner one
	Replaced    [][]string     // per root: its mod's replace_path folders as relative paths; empty for the game's
	Occurrences [][]Occurrence // per file, as Files lists them; in source order, a definition before what it holds
	Texts       [][]string     // per file: each message's text
	Caseless    []string       // the databases whose names match without regard to case, stored in lower case
	Databases   Table
	Names       Table
	Modes       Table
	Languages   Table
	// install is the install's index that a mods' index stands on, whose postings it reads. It is nil for the
	// install's own.
	install  *Index
	postings map[Posting]map[uint32][]Site // by a posting, then by a name's ordinal
	byKey    map[string]int32              // by a file's key: its position in Files
	// byRel holds the positions of the files that share a path from a root, in load order. The path is in lower case
	// and has no stage folder.
	byRel    map[string][]int32
	rootKeys []string // by root, as Roots lists them: its folder's key, ending in a slash
	// origins are the ids of the mods a narrowed index answers for; nil means every mod.
	origins map[string]struct{}
}

// Narrow returns a copy of a mods' index that answers for the install and the given mods only: the other mods' files,
// definitions, references and messages are left out of its lookups. It shares everything else with the original.
func (index *Index) Narrow(origins []string) *Index {
	narrowed := *index
	narrowed.origins = lo.Keyify(origins)
	return &narrowed
}

// hasFile reports whether an index answers for a file: one of the install's, or one of a mod it answers for.
func (index *Index) hasFile(position int32) bool {
	return position < index.countInstallFiles() || index.hasMod(index.Files[position].Origin)
}

// hasMod reports whether an index answers for a mod. An index that is not narrowed answers for every mod.
func (index *Index) hasMod(origin string) bool {
	_, ok := index.origins[origin]
	return index.origins == nil || ok
}

// Occurrence returns the occurrence a site points at.
func (index *Index) Occurrence(site Site) Occurrence {
	return index.Occurrences[site.File][site.Occurrence]
}

// File returns the file a site is in.
func (index *Index) File(site Site) files.File { return index.Files[site.File] }

// Language returns the language of the localization file a site is in, for a message and for a use in its text.
func (index *Index) Language(site Site) string {
	return index.Languages.Texts[index.Occurrence(site).Language]
}

// Text returns the message text stored at a site. It returns "" if the index keeps no text there.
func (index *Index) Text(site Site) string {
	texts := index.Texts[site.File]
	if int(site.Occurrence) >= len(texts) {
		return ""
	}
	return texts[site.Occurrence]
}

// FindMode returns the entry mode a definition is written with. It returns false for a plain definition.
func (index *Index) FindMode(site Site) (game.EntryMode, bool) {
	return game.LookupEntryMode(index.Modes.Texts[index.Occurrence(site).Mode])
}

// FindFile returns the position in Files of the file with the given path, or false if the index has no such file.
func (index *Index) FindFile(path string) (int32, bool) {
	position, ok := index.byKey[files.ToKey(path)]
	return position, ok
}

// IsListed reports whether a position in Files is still a listed file. It is not when the file was deleted after the
// scan.
func (index *Index) IsListed(position int32) bool {
	listed, ok := index.byKey[index.Files[position].Key]
	return ok && listed == position
}

// ListOwnFiles returns the positions in Files of the files this index scanned itself, in order: for a mods' index,
// the mods' files, and for the install's, all of them. A file deleted since the scan is left out, and so is a file of
// a mod a narrowed index does not answer for.
func (index *Index) ListOwnFiles() []int32 {
	var positions []int32
	for position := index.countInstallFiles(); int(position) < len(index.Files); position++ {
		if index.IsListed(position) && index.hasFile(position) {
			positions = append(positions, position)
		}
	}
	return positions
}

// FindRoot returns the root that holds a path: the innermost one, since a DLC's folder is a root inside the game's.
// A root's own folder is held by that root. It returns false if no root holds the path.
func (index *Index) FindRoot(path string) (files.Root, bool) {
	position, ok := findRoot(index.rootKeys, files.ToKey(path)+"/")
	if !ok {
		return files.Root{}, false
	}
	return index.Roots[position], true
}

// countInstallFiles returns how many of the files are the install's, which is where this index's own files begin.
// It is 0 for the install's own index.
func (index *Index) countInstallFiles() int32 {
	if index.install == nil {
		return 0
	}
	return int32(len(index.install.Files))
}
