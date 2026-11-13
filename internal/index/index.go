// Package index records what a scan found in the files of an install or of
// a workspace's mods: every definition, reference and localization message,
// with lookups by name. It decides which definition the game keeps when
// several compete and why the game does not read a copy, lists what fires
// what, and loads an install's index from its cache. Which file the game
// reads at a path is decided by the mounts of package files, which an index
// embeds. It keeps no source, and of a statement only what it defines and
// what it refers to: anything else is read by walking the file again.
//
// An install's index is scanned once and cached. A mods' index stands on it:
// its mounts are the mods' roots mounted on the install's, and it starts with
// the install's occurrences and copies of its tables, so a site and an
// ordinal mean the same in both. It scans and posts only the mods' own files.
// A lookup reads the install's postings first and the mods' after them, so a
// result is in load order.
//
// # The scan
//
//   - A script, GUI, asset, font or GFX file is walked once by schema.Walk,
//     and each statement's definition and references become occurrences.
//   - A localization file gives an occurrence for each message, followed by
//     the names its text refers to. A localization key is defined by its
//     messages.
//   - A table file whose database names its definitions by their first
//     column gives a definition for each row.
//   - The game's own localization is scanned in the base language only. A
//     workspace in another language adds that one as a Translation, scanned
//     once and cached. A mod's files are scanned in every language. A mod's
//     file in a third language is so not checked against the game's keys in
//     that language.
//   - The message the game shows for a key in a language is the one in a
//     file it reads, and of several, the one in the replace folder that
//     ranks first.
//   - A database whose names match in any letter case stores its names in
//     lower case.
//
// # Which copy the game keeps
//
// Only the copies of a name in files the game reads take part, and the
// database's lang.Precedence decides between them. The copies are put in load
// order first: by their paths from their stages, or by their roots where the
// playset decides, and within one file as written.
//
//   - Where the database takes entry modes, the first plain copy stands, and
//     where it keeps every copy, each later plain copy joins it. The modes act
//     after every plain copy, kind by kind in the order of lang.EntryModes and
//     then in load order (the Vic3 and EU5 wikis' Mod compatibility,
//     Processing order). A mode that replaces takes the standing copy's
//     place, one that adds joins it, and one with nothing to act on does
//     nothing unless it creates.
//   - Where no source says what INJECT: does in a database, a copy that adds
//     leaves the choice open. A mode that replaces after it settles the
//     choice again where it creates a copy, or where the copies hold a plain
//     one or a REPLACE_OR_CREATE: one, which stand whatever INJECT: does.
//     Otherwise the game may refuse every copy INJECT: made, and then
//     TRY_REPLACE: and REPLACE: have nothing to act on, so the choice stays
//     open.
//   - A copy written with an entry mode in a database that takes none leaves
//     the choice open: no source says what the game does with it.
//   - Otherwise the first or the last copy by file name wins, the lower
//     root's where the playset decides, the one with the highest override
//     priority, or every copy merges into the first, as the precedence's
//     Keeps says. Copies that share the highest priority leave the choice
//     open: the game logs an error and keeps one, and no source says which.
//   - Where some copies sit in files the game does not read, the winner wins
//     by replace if the copies a replace_path drops would change it, and by
//     path if the copies a file at the same path hides would.
//   - No source says whether a later mod's replace_path drops an earlier
//     mod's files in its folder, so they are read, and where dropping them
//     would change the winner, the choice is open.
//
// A lone copy wins for no reason. Where the choice is open, the winner is one
// of the copies in question, and none of them counts as kept. A copy in a
// file the game does not read is never kept: it is shadowed where a file at
// the same path later in the load order replaces its file, and dropped where
// a mod's replace_path names its folder.
//
// # What fires what
//
// A reference fires the event, on_action or decision it names only where the
// scan marks it as firing:
//
//   - In effect script: an effect or its arguments, as
//     trigger_event = { id = x } writes, or a scripted effect's parameters.
//     An effect is a command the dumps declare as one, or a call of a
//     scripted effect. A decision fires only through an effect the game's
//     DecisionEffects names, such as CK3's execute_decision.
//   - In an on_action's own lists, such as events and random_events.
//   - In the fields the game runs although the schema reads them as a
//     definition's own, such as CK3's council tasks' monthly_on_action.
//   - Never as a scope link, as decision:x = { } writes, inside a trigger
//     block, or in a preview, which the tooltip shows and the game never
//     runs; nor in a GUI or localization file, or in a definition's other
//     fields.
//
// Only the copy the game keeps is called by name. Where the choice is open,
// every copy in a file the game reads fires and is fired, since no source
// says which one runs. Asked to follow, a walk goes through the scripted
// effects on the way, and effects that call each other end it.
//
// # Robustness
//
// A file that cannot be read, or whose format holds no names, gets no
// occurrences, and the scan goes on. ScanMods returns an error only for a
// root it cannot walk. ScanFiles refuses a file of the install, and then
// changes nothing. A file deleted since the scan keeps its position, which a
// new file at the same path takes again. LoadIndex returns false for a cache
// that is missing, does not decode or holds no mounts. A lookup in a database
// the schema does not have finds nothing, and an entry mode no game uses does
// nothing.
package index

import (
	"sync"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/lang"
)

// Role is what an occurrence does with its name.
type Role uint8

// The roles an occurrence can have.
const (
	// RoleDefines is a definition of the name in its database.
	RoleDefines Role = iota

	// RoleRefers is a reference to the name.
	RoleRefers

	// RoleLocalizes is a localization message with the name as its key.
	RoleLocalizes
)

// Occurrence is the stored form of one definition, reference or message. Its
// texts are ordinals into the index's tables, so that it is small enough to
// keep for every file of the game.
type Occurrence struct {
	Span jomini.Span
	Line int32

	// Database, Name and Mode are ordinals into the index's Databases, Names
	// and Modes. Mode is a definition's entry mode, 0 for none.
	Database uint16
	Name     uint32
	Mode     uint8

	// Priority is a definition's override priority, as CK3 writes it. It is
	// 0 for none.
	Priority int16

	// Language is an ordinal into the index's Languages: the language of the
	// localization file the occurrence is in.
	Language uint8

	Role  Role
	Local bool

	// Own is set for a definition of its own, as schema.Copy.IsOwn decides
	// it. A reference has it unset.
	Own bool

	// Firing is set for a reference that fires the event, on_action or
	// decision it names.
	Firing bool

	// Titles is set for a reference that is the localization key of its
	// definition's title, and Describes for the key of its description, as
	// schema.FindTextKey finds them.
	Titles, Describes bool

	// Nested is set for an occurrence inside a definition, and Parent is that
	// definition's position among the file's occurrences. Parent is 0 for an
	// occurrence at the top of its file.
	Nested bool
	Parent int32

	// Statement is the statement that writes the occurrence. It is 0, the
	// file, for a definition named by its file, a message or a row.
	Statement int32
}

// Table interns texts, so that an occurrence stores a small ordinal in place
// of a string. The zero Table is ready to use.
type Table struct {
	// Texts holds each text at its ordinal.
	Texts []string

	ordinals map[string]uint32
}

// Intern returns the ordinal of a text, adding the text to the table if it is
// new.
func (t *Table) Intern(text string) uint32 {
	if ordinal, ok := t.ordinals[text]; ok {
		return ordinal
	}

	if t.ordinals == nil {
		t.ordinals = map[string]uint32{}
	}

	ordinal := uint32(len(t.Texts))
	t.Texts = append(t.Texts, text)
	t.ordinals[text] = ordinal

	return ordinal
}

// Lookup returns the ordinal of a text. It returns false if the text was
// never interned.
func (t *Table) Lookup(text string) (uint32, bool) {
	ordinal, ok := t.ordinals[text]

	return ordinal, ok
}

// Site is a handle to one occurrence: its file's position in Files, and its
// position among the file's occurrences. A mods' index lists the install's
// files first, so a site means the same in both indexes.
type Site struct {
	File       int32 `json:"file"`
	Occurrence int32 `json:"occurrence"`
}

// Posting names one group of occurrences: those of one role in one database,
// given by its ordinal.
type Posting struct {
	Role     Role
	Database uint16
}

// Index is what a scan found in one list of files. A mods' index stands on
// the install's. Its mounts hold its roots and files, and which file the game
// reads at each path.
type Index struct {
	*files.Mounts

	// Game is the game's id.
	Game string

	// Stamp is when an install's index was scanned. A Translation holds only
	// for the scan it was made on.
	Stamp int64

	// Unmarked holds the localization files that were read with no byte
	// order mark, by their position in Files.
	Unmarked map[int32]bool

	// Occurrences holds each file's occurrences, as Files lists the files,
	// each in source order with a definition before what it holds. Texts
	// holds beside them the text of each message: "" for an occurrence that
	// is no message, and none for a file that is no localization file.
	Occurrences [][]Occurrence
	Texts       [][]string

	// Caseless holds the databases whose names match in any letter case,
	// sorted. Their names are stored in lower case.
	Caseless []string

	// Databases, Names, Modes and Languages intern the texts that occurrences
	// store as ordinals.
	Databases Table
	Names     Table
	Modes     Table
	Languages Table

	// install is the install's index that a mods' index stands on, whose
	// postings it reads. It is nil for the install's own index.
	install *Index

	// dialect is the game's dialect, looked up once when the index is made or
	// loaded.
	dialect *lang.Dialect

	// unread holds the keys of the files the game does not read, as they
	// were when a file was last listed or dropped. Only a mods' index has it.
	unread map[string]bool

	// postings holds the sites of each posting's occurrences, by the ordinal
	// of their name.
	postings map[Posting]map[uint32][]Site

	// winners holds the decisions FindKeptLocal has worked out, by
	// decisionKey. They hold for this index's files and mods only.
	winners *sync.Map
}

// Narrow returns a copy of a mods' index that answers for the install and the
// given mods only: the other mods' files, definitions, references and
// messages are left out of its lookups. It shares everything else with the
// original but the winners it has worked out, which other mods change.
func (i *Index) Narrow(origins []string) *Index {
	narrowed := *i
	narrowed.Mounts = i.Mounts.Narrow(origins)
	narrowed.winners = &sync.Map{}

	return &narrowed
}

// Occurrence returns the occurrence a site points at.
func (i *Index) Occurrence(site Site) Occurrence {
	return i.Occurrences[site.File][site.Occurrence]
}

// File returns the file a site is in.
func (i *Index) File(site Site) files.File { return i.Files[site.File] }

// Language returns the language of the localization file a site is in, for a
// message and for a use in its text.
func (i *Index) Language(site Site) string {
	return i.Languages.Texts[i.Occurrence(site).Language]
}

// Text returns the message text stored at a site. It returns "" if the index
// keeps no text there.
func (i *Index) Text(site Site) string {
	texts := i.Texts[site.File]
	if int(site.Occurrence) >= len(texts) {
		return ""
	}

	return texts[site.Occurrence]
}

// FindMode returns the entry mode a definition is written with. It returns
// false for a plain definition.
func (i *Index) FindMode(site Site) (lang.EntryMode, bool) {
	return lang.LookupEntryMode(i.Modes.Texts[i.Occurrence(site).Mode])
}

// ListOwnFiles returns the positions in Files of the files this index scanned
// itself, in order: the mods' files for a mods' index, all of them for the
// install's. It leaves out a file deleted since the scan, and a file of a mod
// that a narrowed index does not answer for.
func (i *Index) ListOwnFiles() []int32 {
	var positions []int32

	for position := i.countInstallFiles(); int(position) < len(i.Files); position++ {
		if i.IsListed(position) && i.Covers(position) {
			positions = append(positions, position)
		}
	}

	return positions
}

// countInstallFiles returns how many of the files are the install's, so this
// index's own files begin after them. It is 0 for the install's own index.
func (i *Index) countInstallFiles() int32 {
	if i.install == nil {
		return 0
	}

	return int32(len(i.install.Files))
}
