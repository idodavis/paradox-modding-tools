// Package index records what a scan found in the files of an install or a workspace's mods: every definition,
// reference and localization message, with the postings that answer lookups by name. It also decides which file
// and which definition the game keeps when several compete, lists what fires what, and caches an install's index
// on disk. It interprets no text itself; the walk and the loc parser did that.
package index

import (
	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/jomini"
)

// A Role says what an occurrence does with its name: defines it, refers to it or localizes it.
type Role uint8

const (
	RoleDefines   Role = iota // it defines the name in its database
	RoleRefers                // it refers to the name
	RoleLocalizes             // it is a localization message with the name as its key
)

// An Occurrence is the stored form of one definition, reference or message, compact enough to keep for every file
// of the game. Each string is stored as an ordinal into one of the index's tables.
type Occurrence struct {
	Span     jomini.Span
	Line     int32
	Database uint16 // into Index.Databases
	Name     uint32 // into Index.Names
	Caller   uint8  // into Index.Callers
	Mode     uint8  // into Index.Modes; an entry mode's prefix
	Priority int16  // CK3's override priority; 0 none
	Language uint8  // into Index.Languages; a message's
	Scope    uint8  // into Index.Scopes; a reference's scope, a definition's root; 0 unknown
	Role     Role
	Local    bool
	Parent   int32 // the defining occurrence this one sits inside; -1 at the top of a file
}

// A Table interns texts, so that an occurrence stores a small ordinal instead of a string.
type Table struct {
	Texts    []string
	ordinals map[string]uint32
}

// Intern returns the ordinal of a text, adding the text to the table if it is new.
func (table *Table) Intern(text string) uint32 { panic("stub") }

// Lookup returns the ordinal of a text, or false if the text was never interned.
func (table *Table) Lookup(text string) (uint32, bool) { panic("stub") }

// A Site points at one occurrence: the position of its file in Files and its position among that file's
// occurrences.
type Site struct {
	File       int32 `json:"file"`
	Occurrence int32 `json:"occurrence"`
}

// A Posting is one key of the postings: a role, a database and a name.
type Posting struct {
	Role     Role
	Database uint16
	Name     uint32
}

// An Index is what a scan found in one list of files, with the postings that answer lookups by name.
type Index struct {
	Files       []files.File
	Roots       []files.Root   // in load order; a file under one belongs to it
	Replaced    []string       // the folders the mods replace with replace_path, as relative paths
	Occurrences [][]Occurrence // per file, as Files lists them; a file's definitions first, in source order
	Texts       [][]string     // per file: each message's text
	Databases   Table
	Names       Table
	Callers     Table
	Modes       Table
	Languages   Table
	Scopes      Table
	postings    map[Posting][]Site
	byKey       map[string]int32 // by a file's key: its position in Files
	byRel       map[string]int32 // by relative path: the last file in load order with that path
}

// Occurrence returns the occurrence a site points at.
func (index *Index) Occurrence(site Site) Occurrence { panic("stub") }

// File returns the file a site is in.
func (index *Index) File(site Site) files.File { panic("stub") }

// Text returns the message text stored at a site.
func (index *Index) Text(site Site) string { panic("stub") }

// FindFile returns the position in Files of the file with the given path, or false if the index has no such file.
func (index *Index) FindFile(path string) (int32, bool) { panic("stub") }
