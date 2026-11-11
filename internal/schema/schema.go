// Package schema models the language of one game version: which databases
// exist, what each key takes and which commands run in which scope. Build
// makes it from the game's dumps, the docs the install ships and the game's
// own files. The package also owns the one walk over a file, and every reader
// of script is a visitor on it.
//
// # Sources
//
//   - Every fact comes with a game.Source, and a lower source outranks a
//     higher one: the rows, the dumps, the game's executable, its logs, the
//     docs, the wiki, the game's own files, and inferred last. A claim keeps
//     the fact of its best source.
//   - Only the dumps, the docs, the wiki and what a definition in the files
//     says its name or a parameter takes can accuse a mod.
//   - A fact learned from the game's own files completes a hover card, marked
//     as inferred. It never fails a verdict, and a reference that only such a
//     fact makes is inferred.
//
// # Databases
//
//   - A database is built for each folder a script file belongs to, each
//     folder or file a row names, each nesting, each start setup and each GUI
//     declaration. A nested database is named by its parent's name, a dot and
//     its own.
//   - A nesting that writes a separator names its definitions by their
//     holder's name, the separator and their own key: define X of the NAI
//     group is NAI|X.
//   - The files under the DLC folder are left out, since the game's own root
//     lists each DLC's files too.
//
// # The dumps
//
//   - A section the install's dumps lack comes from the shipped backup, one
//     section at a time. The effects, the triggers and the links are three
//     sections, and the links bring the targets saved from code.
//   - A dump correction fixes a dump line that contradicts its own text. It
//     wins over the dump.
//   - A name that several dumps list is one command, which takes the
//     arguments each dump gives it where that dump declares it. Its scopes
//     are the union of its dumps', and none if any dump names none. Its
//     target is the one scope a dump says it leads to, and none where the
//     dump names several.
//   - A command's arguments are the keys of the usage blocks written under
//     its name, and of the lines of its prose that open with key = value. A
//     statement in [ ] or < > is optional, [a|b] gives choices, and a
//     placeholder such as <triggers> gives the block that shape. A trigger's
//     Traits line says what its value takes, and outranks a value the dump
//     writes after the command's name, as add_trait = <trait_key> does.
//   - A command declared as both an effect and a trigger keeps each one's
//     keys, and a union of the two for a block that allows either. The union
//     keeps a shape only where both state it, or where the shape allows
//     effects and triggers alike.
//   - The commands of one list family, such as any_x, every_x, random_x and
//     ordered_x, share the keys of their arguments.
//   - A keyword row written under a command, such as random_list's numbered
//     branches, gives the command's block that key in the row's shape.
//   - A command with no target whose blurb opens with "Iterate through"
//     walks a list, and the scope inside its block is not known.
//   - A link that requires data is a prefix, written with a name after a
//     colon, as scope:target is.
//   - A data function written for several arities keeps the first. Self and
//     AccessSelf return the type they are called on.
//
// # The docs
//
//   - A doc is an .info, .md or readme file, or a text file named as one, in
//     a database's folder or below one. Prose is no valid script, so a doc is
//     read line by line, by pattern.
//   - A doc describes the database of its own folder, else of a folder above
//     it, else the one its file's name gives. A doc in a folder below its
//     database's describes part of it, and gives it no blurb. A doc of a
//     database whose definitions are named by their file is read as inside
//     that one definition.
//   - A doc's example gives fields, and the comments beside it state a blurb,
//     a type, a requirement, a default and choices. It states no scope. A
//     later doc fills only what an earlier one left out.
//   - A requirement is conditional where the text states a condition, else
//     optional or always as the terms optional, required and mandatory say.
//     The first one stated is kept.
//   - Choices that are only yes and no make a bool, and the choice marked
//     (default) is the default.
//   - A database of triggers, effects or a value takes no fields from a doc,
//     and a field whose block holds script takes none below it.
//   - A field with no blurb takes the one the docs give the same key
//     elsewhere in its database.
//
// # The count of the game's own files
//
//   - The files that declare callables are read first, so that a key under a
//     call counts as a call.
//   - The count states what no other source does, field by field. A field's
//     shape is the one the keys in its blocks tell, value over triggers, and
//     unknown where they disagree.
//   - Where vanilla writes a reference, the field names the database that
//     defines every value written there. A field written only as scalars
//     takes the first type all its values fit; one always written as a block
//     is a color or a block. A target that names a database is firm only
//     where the database defines every name the game's files write there.
//   - The game concepts name the values only where no other database
//     defines them, and the localization only where neither does. A field
//     that another source states is a block or a text never takes a game
//     concept's names, and no field takes a GUI database's, since their
//     names are common words such as value.
//   - Where every key that only vanilla writes in a block is a definition of
//     one database, the block's keys name that database, as a culture's
//     opinions name cultures. Keys never name the game concepts, the
//     localization, a GUI database or a database with nested ones. A
//     command's own block keeps its keys apart, except where a command key
//     row says they name a database.
//   - A number takes a script value too where the game's files write one
//     there.
//   - A prefix's database is the one that defines every name the game's
//     files write after it. A prefix that leads to a scope from any scope
//     tries that scope's database first.
//   - A database gives a command pattern where each of its plain
//     definitions, two at least, fills the pattern's star in a command the
//     dumps declare, one command each.
//
// # Scripted definitions
//
//   - Each parameter is a field the definition declares, so a key in a call
//     reads as its argument, never as a link or a command of that name. A
//     parameter is one written in a key, a tag or a value of the body, never
//     in a comment, and one whose every use writes a default takes the first
//     as its default.
//   - A parameter written whole after a link prefix takes the prefix's
//     database. One written whole as the value of a key takes what that key's
//     field claims, inferred unless it names a database. One passed on whole
//     to a call of another scripted definition takes what that one's argument
//     takes. A parameter keeps the first database it gets, else the first
//     claim.
//   - A modifier type's name takes yes or no where its definition writes
//     boolean = yes, else a number or a script value.
//   - A call finds the game's definition first, then a mod's, and a mod's
//     never takes the place of a command the dumps declare. A name in
//     another case finds a definition only where its database ignores case.
//     A later definition of a name replaces an earlier one.
//
// # The walk
//
//   - A statement either defines a name, or its key fits the first rule of
//     its block's shape that it fits. A keyword of the language adjusts what
//     the rule gave. A key with a parameter is not known until the call is
//     made, and neither are its block's shape and scope.
//   - A command fits only where the block's shape allows what it is declared
//     as, so an effect never fits in a trigger block. A key written with an
//     argument in parentheses, as aptitude(court_jester_court_position), is
//     read as the command, and its argument as the command's ArgumentKey. A
//     math keyword's value written so, as opinion(scope:ruler) is, makes the
//     references of its argument.
//   - A link or a command that leads to a scope moves the scope there. An
//     argument of a command is read in a scope the command makes, which is
//     not known, but a conditional keyword such as limit is read in the
//     scope the command is written in. Where a database gives its blocks a
//     run order, they are read in that order first, so each block starts
//     with the scopes the earlier ones saved. A walk to an offset still
//     reads the statements before it, to learn what they saved.
//   - An event's name opens with a namespace its file declares above it.
//   - A value shaped like an event id names an event where its field claims
//     events, or claims nothing and an events file declares its namespace. A
//     value with a space is text, since a message key has none.
//   - A GUI block's datacontext key sets the data context of the calls in
//     it, unknown where the dumps do not resolve the call.
//
// # Robustness
//
// Build returns an error if a dump file's time or a doc cannot be read, or if
// no file of the listing can be parsed. A dump file that cannot be read as
// text, such as one over files.TextLimit or in UTF-16, counts as missing. A
// file that cannot be parsed is left out of the count. A doc with no prose and
// no comment, or a .txt doc with more script lines than prose, is data and is
// skipped. LoadShipped returns false for a game with no backup and for one
// that does not decode. The walk never fails: a key no rule explains has no
// field. SetScripted reads a file that cannot be parsed, or is gone, as
// declaring nothing.
package schema

import (
	"cmp"
	"maps"
	"path"
	"slices"
	"strings"
	"sync/atomic"
	"time"
	"unicode"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/jomini"
)

// Claim is one fact about a field, as its best source states it.
type Claim[T any] struct {
	Value T

	// Source is the source that states the value. It is game.SourceNone when
	// no source states it.
	Source game.Source
}

// ValueType is the type of value a field takes.
type ValueType string

// The types a field's value can have.
const (
	ValueTypeUnknown ValueType = "unknown"
	ValueTypeBool    ValueType = "bool"
	ValueTypeNumber  ValueType = "number"
	ValueTypeDate    ValueType = "date"
	ValueTypeText    ValueType = "text"

	// ValueTypeReference is a name of Expected.Database.
	ValueTypeReference ValueType = "reference"

	// ValueTypeTarget is a link to Expected.Scope, or a name of
	// Expected.Database where it is set.
	ValueTypeTarget ValueType = "target"

	// ValueTypeChoice is one of Expected.Choices.
	ValueTypeChoice ValueType = "choice"

	ValueTypeColor ValueType = "color"
	ValueTypeBlock ValueType = "block"

	// ValueTypeCall is a data call.
	ValueTypeCall ValueType = "call"
)

// Expected is what a field's value must be.
type Expected struct {
	Type ValueType `json:"type"`

	// Database is the database a reference or a target names.
	Database string `json:"database,omitempty"`

	// Scope is the scope a target's link leads to.
	Scope string `json:"scope,omitempty"`

	Choices []string `json:"choices,omitempty"`

	// Values is whether a number may be a script value too.
	Values bool `json:"values,omitempty"`

	// Nested is whether a name of a database nested in Database fits too.
	Nested bool `json:"nested,omitempty"`

	// Stem is the name a reference's value stands for, with %s for the value,
	// as CK3's activity log names the key <value>_title. It is "" for the
	// value itself.
	Stem string `json:"stem,omitempty"`
}

// Vanilla is what the game's own files write at a field, counted.
type Vanilla struct {
	// Count is how many times the field is written, Blocks how many of those
	// writes are a block, and Colors how many of those blocks are a color.
	Count  int
	Blocks int
	Colors int

	// Definitions is how many plain definitions of its database write it.
	Definitions int

	// Values holds the count of each value written.
	Values map[string]int

	// Shapes holds the count of each shape that a key written directly in its
	// blocks tells.
	Shapes map[game.Shape]int

	// Proofs holds the count of each set of shapes that a call in its blocks
	// allows, the shapes joined by spaces.
	Proofs map[string]int
}

// Requirement is whether a field must be written.
type Requirement string

// The requirements a field can have.
const (
	// RequirementOptional is a field that may be left out.
	RequirementOptional Requirement = "optional"

	// RequirementAlways is a field that a definition must write.
	RequirementAlways Requirement = "always"

	// RequirementConditional is a field that a definition must write only
	// under a condition the docs state.
	RequirementConditional Requirement = "conditional"
)

// Claims holds every fact the sources state about one field.
type Claims struct {
	Value Claim[Expected]
	Shape Claim[game.Shape]

	// Required is whether the field must be written.
	Required Claim[Requirement]

	Default Claim[string]
	Blurb   Claim[string]

	// Keys is the database that the keys of the field's block name.
	Keys Claim[string]
}

// Field is one key that may be written in a definition, with what it takes and
// the fields that may be written inside it. A database's root field is the
// whole definition block.
type Field struct {
	// Children are the fields inside it, by written key, or by a pattern such
	// as <number> or <any>.
	Children map[string]*Field

	Claims

	// Source is the highest-ranked declared source that writes the field. It
	// is game.SourceNone if none does.
	Source game.Source

	// Weighted is whether the field's key is a number whose share of its
	// siblings' is its chance, as an on_action's random_events entry is.
	Weighted bool

	Vanilla Vanilla
}

// Command is one effect, trigger or link the dumps declare.
type Command struct {
	// Declared is what the dumps declare it as, in dump order.
	Declared []game.Declaration

	// Scopes are the scopes the dumps say it runs in. They are none if the
	// dumps name none.
	Scopes []string

	// Target is the scope a link leads to. It is "" for none.
	Target string

	// Arguments are the keys it takes, as fields, by declaration: an effect
	// and a trigger of one name each take their own.
	Arguments map[game.Declaration]*Field

	// Union is the keys of its effect and its trigger together, which it
	// takes in a block that allows either. It is nil unless the dumps declare
	// it as both.
	Union *Field

	// ArgumentKey is the key that the argument in parentheses after its name
	// stands for. It is "" if none.
	ArgumentKey string

	Blurb string

	// Usage is the dump's usage lines. It is "" if none.
	Usage string

	// Examples are the first places the game's own files write it, by path
	// and then line, and Count is how often they write it.
	Examples []Example
	Count    int

	// Wild is whether it is a link that is a pattern, so that a written name
	// never matches it.
	Wild bool

	// Bare is whether it is a link that script may write as its name alone,
	// not only as name:data.
	Bare bool

	// Lists is whether it iterates a list and has no target, so the scope
	// inside its block is unknown.
	Lists bool

	// Patterned is whether some pattern lists it.
	Patterned bool
}

// Example is one place where the game's own files write a command or call a
// data function. Its line counts from zero.
type Example struct {
	Path string
	Line int
}

// CommandPattern is a family of commands that the dumps declare once for each
// definition of a database, as has_relation_* is for the scripted relations.
type CommandPattern struct {
	// Key is the commands' name with a star in the place of a definition's
	// name, and Database the database whose definitions' names fill the star.
	Key      string
	Database string

	// Commands are the commands the dumps declare by it, sorted.
	Commands []string
}

// Database is one database of the game: the field tree of its definitions and
// the game's rows that govern it.
type Database struct {
	Name string

	// Nested are the databases whose definitions are keys inside its own.
	Nested []string

	Definition *Field

	// Row is where its definitions are written and how each is named.
	Row game.Database

	// Callable is what calls its definitions. It is zero if nothing does.
	Callable game.Callable

	// Precedence is how several definitions of one name combine.
	Precedence game.Precedence

	// Documented holds every key the docs mention for its definitions.
	Documented map[string]bool

	// Readme is the game doc its blurb was read from, else the first that
	// describes it all. It is "" if none.
	Readme string
}

// ScopeType is one type of scope the game runs script in, and what script can
// do there.
type ScopeType struct {
	Triggers bool
	Effects  bool
}

// DataFunction is one function of a data type, as the data types dump
// declares it.
type DataFunction struct {
	Name      string
	Returns   string
	Arguments []string
	Blurb     string

	// Expansion is the text of a data binding macro. It is "" for a function
	// the game has itself.
	Expansion string

	// Promote is whether the dumps define it as a promote, which leads to an
	// object of its type rather than a value.
	Promote bool

	// Examples are the first places the game's own files call it, by path
	// and then line.
	Examples []Example
}

// DataType is one type of the data binding language, with its functions.
type DataType struct {
	Name      string
	Functions map[string]DataFunction
}

// OnAction is one on_action the dumps declare, with the scope it runs in.
type OnAction struct {
	Scope string

	// Hardcoded is whether the game calls it from its own code, as the dumps'
	// "From Code: Yes" says.
	Hardcoded bool
}

// ScriptedDefinition is a scripted effect, trigger, value or list the game or
// a mod defines, with the parameters it takes.
type ScriptedDefinition struct {
	Database string

	// Parameters are the parameters it takes, as fields. It is nil if the
	// database's definitions take none.
	Parameters *Field

	// Saves holds, by parameter, the link its body saves the argument's name
	// under, such as scope.
	Saves map[string]string

	// relays are the parameters its body passes on to calls of other scripted
	// definitions, and typings the places its body writes a parameter where a
	// database's name goes, in order.
	relays  []relay
	typings []typing
}

// relay is one parameter that a scripted definition's body passes on as an
// argument of a call to another one.
type relay struct {
	// Callee is the called definition's name as written, Argument the
	// argument's key in the call, and Parameter the parameter of the body that
	// the argument writes.
	Callee    string
	Argument  string
	Parameter string
}

// typing is one place where a scripted definition's body writes a parameter
// whole where a database's name may go: after a link prefix, or as the value
// of a key that has a field.
type typing struct {
	Parameter string

	// Prefix is the link prefix written before it. It is "" for a key's
	// value.
	Prefix string

	// Field is the field of the key it is the value of. It is nil after a
	// prefix.
	Field *Field
}

// modScripted is what the files of the workspace's mods declare, by file and
// all together.
type modScripted struct {
	// byFile holds the declarers by a file's key.
	byFile map[string]*declarer

	// all, modifiers and namespaces hold what the files declare, as
	// ScriptedDefinitions, ModifierFields and Namespaces hold the game's.
	all        map[string]ScriptedDefinition
	modifiers  map[string]*Field
	namespaces map[string]bool
}

// The origins of a source's data: the install, the shipped backup, or none.
const (
	originNone    = "none"
	originInstall = "install"
	originShipped = "shipped"
)

// SourceState says where one source's data came from and how fresh it is.
type SourceState struct {
	// Origin is install, shipped or none.
	Origin string `json:"origin"`

	WrittenAt time.Time `json:"writtenAt"`

	// Stale is whether a dump file read is older than the install.
	Stale bool `json:"stale"`

	// Folders are the install's folders its files were read from. They are
	// none for the shipped backup.
	Folders []string `json:"folders"`
}

// Schema is the language of one game version, as built from the dumps, the
// docs and the game's own files.
type Schema struct {
	Game string

	// Version is the install's version. Build leaves it "".
	Version string

	Dumps     SourceState
	Databases map[string]*Database

	// Setups are the folders that the game's bookmarks name for their start
	// setups, without the stage folder. They are none for a game with a
	// single start.
	Setups []string

	// Callees holds, by caller, the databases a call through it resolves in.
	Callees map[string][]string

	Commands map[string]*Command
	Scopes   map[string]ScopeType

	// Customs holds the customizable localizations the dumps name, though the
	// game's files may define none.
	Customs map[string]bool

	// Modifiers holds the modifiers the dumps declare, which a modifier block
	// may write as keys.
	Modifiers map[string]bool

	OnActions map[string]OnAction
	DataTypes map[string]DataType

	// Prefixes holds, by link prefix, the database its names are in. It holds
	// "" for none.
	Prefixes map[string]string

	// TypeDatabases holds, by the phrase of a data type, the database of its
	// names. It holds "" where several databases have the phrase.
	TypeDatabases map[string]string

	// InlineDatabases holds, by a key that declares a definition inline in an
	// events file, the definition's database.
	InlineDatabases map[string]string

	// ModifierFields holds, by modifier type the game defines, what its name
	// takes in a modifiers block.
	ModifierFields map[string]*Field

	ScriptedDefinitions map[string]ScriptedDefinition
	WidgetTypes         map[string]bool

	// Namespaces holds the event namespaces the game's events files declare.
	Namespaces map[string]bool

	// UnlistedCalls holds the data call steps, as Type.step, that no dump
	// declares for the type before them, though the game's own localization
	// and GUI text write them.
	UnlistedCalls map[string]bool

	// CommandPatterns are the patterns the dumps name commands by, sorted by
	// key and database.
	CommandPatterns []CommandPattern

	// TopFolders holds the first folder of each of the game's files in lower
	// case, without the stage folder: the folders under which the game reads
	// a mod's files.
	TopFolders map[string]bool

	// Lists, Events, Messages, Concepts, ModifierTypes, Effects and Values
	// are the databases of scripted lists, of events, of the messages an
	// effect sends the player, of game concepts, of modifier types, of
	// scripted effects and of script values. Each is "" if none.
	Lists         string
	Events        string
	Messages      string
	Concepts      string
	ModifierTypes string
	Effects       string
	Values        string

	// FireDatabases are the databases that are fired: those whose row's
	// contents are one of fireContents.
	FireDatabases []string

	// mods holds the mods' scripted definitions, which a call finds after the
	// game's. A rescan puts a new table in place.
	mods atomic.Pointer[modScripted]
}

// Build returns the schema of a game version, from an install's dumps, docs
// and own files. It returns an error if a dump or a doc cannot be read, or no
// file can be parsed.
func Build(row *game.Game, installPath string, listing []files.File, parse func(path string) (*jomini.Tree, error)) (*Schema, error) {
	listing = slices.DeleteFunc(slices.Clone(listing), func(file files.File) bool {
		return strings.HasPrefix(file.Rel, game.DLCFolder+"/")
	})

	schema := &Schema{
		Game:      row.ID,
		Databases: map[string]*Database{},
		Callees:   map[string][]string{},
		Commands:  map[string]*Command{},
		Prefixes: map[string]string{
			game.SavedScopes: game.SavedScopes, game.Variables: game.Variables,
			game.LocalVariables: game.LocalVariables, game.GlobalVariables: game.GlobalVariables,
		},
		TypeDatabases:       map[string]string{},
		InlineDatabases:     map[string]string{},
		ModifierFields:      map[string]*Field{},
		ScriptedDefinitions: map[string]ScriptedDefinition{},
		WidgetTypes:         map[string]bool{},
		Namespaces:          map[string]bool{},
		UnlistedCalls:       map[string]bool{},
		TopFolders:          map[string]bool{},
	}
	for _, file := range listing {
		if folder, _, nested := strings.Cut(strings.ToLower(file.StageRel()), "/"); nested {
			schema.TopFolders[folder] = true
		}
	}

	schema.Setups = listSetups(row, listing, parse)
	folders := listFolders(row, listing)

	// the count reads the unions as every later walk does, so they are built
	// before it and again after
	schema.buildDatabases(row, folders)
	schema.buildFields(row)
	if err := schema.buildDumps(row, installPath); err != nil {
		return nil, err
	}

	if err := schema.buildDocs(installPath, listing, folders); err != nil {
		return nil, err
	}

	schema.buildUnions()
	if err := schema.CountVanilla(listing, parse); err != nil {
		return nil, err
	}

	schema.buildUnions()
	schema.buildDocBlurbs()

	return schema, nil
}

// listSetups returns the folders that the game's bookmarks name as the folders
// of their start setups, sorted and each once. It returns none for a game with
// no bookmark folder. A bookmark file that cannot be parsed names none.
func listSetups(row *game.Game, listing []files.File, parse func(path string) (*jomini.Tree, error)) []string {
	var setups []string
	for _, file := range listing {
		if row.BookmarkFolder == "" || !strings.HasPrefix(file.StageRel(), row.BookmarkFolder+"/") {
			continue
		}

		tree, err := parse(file.Path)
		if err != nil {
			continue
		}

		for bookmark := range tree.Children(0) {
			if setup, ok := tree.FindChildValue(bookmark, row.SetupKey); ok && setup != "" {
				setups = append(setups, setup)
			}
		}
	}

	slices.Sort(setups)

	return slices.Compact(setups)
}

// listFolders returns the folder of each database the game's files make: the
// folder each script file belongs to, and every folder or file a row names
// that the listing has.
func listFolders(row *game.Game, listing []files.File) []string {
	folders := map[string]bool{}
	present := map[string]bool{}
	for _, file := range listing {
		rel := file.StageRel()
		for dir := rel; dir != "."; dir = path.Dir(dir) {
			present[dir] = true
		}

		if files.Classify(rel) == files.FormatScript {
			folders[findFolder(row, rel)] = true
		}
	}

	for _, folder := range row.ListDatabaseFolders() {
		if present[folder] {
			folders[folder] = true
		}
	}

	delete(folders, ".")

	return slices.Sorted(maps.Keys(folders))
}

// findFolder returns the folder of the database that a script file belongs
// to: the file itself if a row names it, else the nearest folder above it
// that a row names, else the file's own folder.
func findFolder(row *game.Game, rel string) string {
	if _, ok := row.FindDatabase(rel); ok {
		return rel
	}

	for dir := path.Dir(rel); dir != "."; dir = path.Dir(dir) {
		if _, ok := row.FindDatabase(dir); ok {
			return dir
		}
	}

	return path.Dir(rel)
}

// fireContents are the contents of the databases that fire and are fired. A
// message is fired when an effect sends it.
var fireContents = []game.Contents{
	game.ContentsEvents, game.ContentsOnActions, game.ContentsDecisions, game.ContentsMessages,
}

// buildDatabases builds the game's databases, one for each folder and
// nesting, and those the GUI files declare.
func (s *Schema) buildDatabases(row *game.Game, folders []string) {
	for _, folder := range folders {
		databaseRow, _ := row.FindDatabase(folder)
		if slices.Contains(s.Setups, folder) {
			databaseRow, _ = row.FindDatabase(game.SetupFolder)
		}

		databaseRow.Folder = folder
		s.buildDatabase(row, game.ToDatabase(folder), databaseRow)
	}

	for _, nesting := range slices.Concat(row.ListNestings(), row.ListSetupNestings(s.Setups...)) {
		if nesting.Database == "" || nesting.Parent == game.SetupFolder {
			continue
		}

		label := nesting.Label()
		s.buildDatabase(row, label, game.Database{Target: nesting.Target})
		if parent := s.Databases[game.ToDatabase(nesting.Parent)]; parent != nil && !slices.Contains(parent.Nested, label) {
			parent.Nested = append(parent.Nested, label)
		}
	}

	for _, declaring := range game.GUI.Declares {
		s.buildDatabase(row, game.GUI.DatabasePrefix+declaring, game.Database{})
	}

	s.buildDatabase(row, game.GUI.WidgetDatabase, game.Database{})

	// a text names a format or an icon in any case
	s.buildDatabase(row, game.GUI.TextFormats, game.Database{FoldsCase: true})
	s.buildDatabase(row, game.GUI.TextIcons, game.Database{FoldsCase: true})

	housed := map[game.Contents][]string{}
	for label, database := range s.Databases {
		housed[database.Row.Contents] = append(housed[database.Row.Contents], label)

		// the effects and value callables are the first label in order with
		// that shape
		switch {
		case database.Callable.Shape == game.ShapeEffects && (s.Effects == "" || label < s.Effects):
			s.Effects = label
		case database.Callable.Shape == game.ShapeValue && (s.Values == "" || label < s.Values):
			s.Values = label
		}

		for _, prefix := range database.Row.Prefixes {
			s.Prefixes[prefix] = label
		}
	}

	findOnly := func(contents game.Contents) string {
		if len(housed[contents]) == 1 {
			return housed[contents][0]
		}

		return ""
	}
	s.Events, s.Messages, s.Lists = findOnly(game.ContentsEvents), findOnly(game.ContentsMessages),
		findOnly(game.ContentsLists)
	s.Concepts, s.ModifierTypes = findOnly(game.ContentsConcepts), findOnly(game.ContentsModifiers)

	for _, house := range fireContents {
		s.FireDatabases = append(s.FireDatabases, housed[house]...)
	}

	slices.Sort(s.FireDatabases)
	for _, widget := range slices.Concat(slices.Collect(maps.Keys(game.GUI.Types)), row.GUI.Undeclared) {
		s.WidgetTypes[widget] = true
	}

	s.listCallees()
}

// buildDatabase adds one database built from its row. It does nothing if the
// schema has it already. A database with a target, such as the history of
// titles, names no data type, since its definitions are the target's.
func (s *Schema) buildDatabase(row *game.Game, label string, databaseRow game.Database) {
	if s.Databases[label] != nil {
		return
	}

	s.Databases[label] = &Database{
		Name: label, Definition: &Field{}, Row: databaseRow, Callable: game.Callables[label],
		Precedence: row.LookupPrecedence(label, databaseRow.Folder), Documented: map[string]bool{},
	}
	for _, key := range databaseRow.DeclaringKeys {
		s.InlineDatabases[key] = label
	}

	if databaseRow.Target != "" {
		return
	}

	// of two databases with the same phrase, the one named exactly by it keeps
	// it, else neither
	phrase := game.DescribeDefinition(label, false)
	owner := label
	if earlier, ok := s.TypeDatabases[phrase]; ok && !isNamedExactly(label, phrase) {
		owner = ""
		if isNamedExactly(earlier, phrase) {
			owner = earlier
		}
	}

	s.TypeDatabases[phrase] = owner
}

// buildFields gives each field that a game.DatabaseFields row names what the
// row states: its shape, its weights and its hover text. A row for another
// game, or for a database the game lacks, is skipped.
func (s *Schema) buildFields(row *game.Game) {
	for _, entry := range game.DatabaseFields {
		database := s.Databases[entry.Database]
		if database == nil || !row.Matches(entry.Games) {
			continue
		}

		field := database.Definition
		for key := range strings.SplitSeq(entry.KeyPath, "/") {
			field = field.buildChild(key, game.SourceRegistry)
		}

		field.Weighted = entry.Weighted
		field.Blurb.resolve(entry.Blurb, game.SourceRegistry)
		if entry.Shape != "" {
			field.Shape.resolve(entry.Shape, game.SourceRegistry)
		}
	}
}

// isNamedExactly reports whether a database's name is the given phrase with
// underscores. The database institution is named exactly by "institution",
// and setup/1337.institution is not.
func isNamedExactly(label, phrase string) bool {
	return label == strings.ReplaceAll(phrase, " ", "_")
}

// listCallees fills Callees: for each caller, the databases whose definitions
// it can call.
func (s *Schema) listCallees() {
	for label, callable := range game.Callables {
		if s.Databases[label] == nil {
			continue
		}

		for _, caller := range callable.Callers {
			s.Callees[caller] = append(s.Callees[caller], label)
		}
	}

	for _, labels := range s.Callees {
		slices.Sort(labels)
	}
}

// FindDatabase returns the database that a file belongs to, going by its path
// from its stage folder and the game's folder rules. It returns false if no
// database claims the file.
func (s *Schema) FindDatabase(file files.File) (*Database, bool) {
	for folder := file.StageRel(); folder != "."; folder = path.Dir(folder) {
		if database, ok := s.Databases[game.ToDatabase(folder)]; ok && database.Row.Folder == folder {
			return database, true
		}
	}

	return nil, false
}

// classDatabases maps the dumps' class names that no naming rule resolves to
// their database's path.
var classDatabases = map[string]string{
	"CCultureInnovationTypeDatabase":     "culture/innovations",
	"CCultureTraditionDatabase":          "culture/traditions",
	"CCourtPositionTypesDatabase":        "court_positions/types",
	"CTravelPointOfInterestTypeDatabase": "travel/point_of_interest_types",
}

// FindDatabaseByClass returns the database that a class in the dumps refers
// to, such as CTraitDatabase. It returns false if there is none.
func (s *Schema) FindDatabaseByClass(class string) (*Database, bool) {
	words := strings.TrimPrefix(class, "C")
	if lower := strings.ToLower(words); strings.HasSuffix(lower, "database") {
		words = words[:len(words)-len("database")]
	}

	label, ok := s.ResolveTypeDatabase(words)
	if folder, listed := classDatabases[class]; listed {
		label, ok = game.ToDatabase(folder), true
	}

	database := s.Databases[label]

	return database, ok && database != nil
}

// ResolveTypeDatabase returns the database that holds a data type's names,
// such as governments for GovernmentType. It returns false unless there is
// exactly one.
func (s *Schema) ResolveTypeDatabase(dataType string) (string, bool) {
	phrase := ToPhrase(dataType)
	label, ok := s.TypeDatabases[phrase]
	if stem, isTyped := strings.CutSuffix(phrase, " type"); !ok && isTyped {
		label = s.TypeDatabases[stem]
	}

	return label, label != ""
}

// ToPhrase returns a CamelCase or snake_case text as lower-case words
// separated by spaces.
func ToPhrase(text string) string {
	var words strings.Builder
	var previous rune
	for _, r := range text {
		if unicode.IsUpper(r) && (unicode.IsLower(previous) || unicode.IsDigit(previous)) {
			words.WriteByte(' ')
		}

		if r == '_' {
			r = ' '
		}

		words.WriteRune(unicode.ToLower(r))
		previous = r
	}

	return words.String()
}

// JoinNested returns the name of a nested definition from its holder's name
// and its key, such as NAI|X. It is the key if there is no separator or
// holder.
func (s *Schema) JoinNested(database, holder, key string) string {
	separator := s.findSeparator(database)
	if separator == "" || holder == "" {
		return key
	}

	return holder + separator + key
}

// SplitNested returns the key of a definition's name in a nested database, X
// for NAI|X. Any other name is its own key.
func (s *Schema) SplitNested(database, nested string) string {
	separator := s.findSeparator(database)
	if _, key, ok := strings.Cut(nested, separator); ok && separator != "" {
		return key
	}

	return nested
}

// findSeparator returns the separator that the holder of a nested database
// writes, such as the | of NAI|X. It returns "" if there is none.
func (s *Schema) findSeparator(database string) string {
	parent, _, nested := strings.CutLast(database, ".")
	if !nested {
		return ""
	}

	if holder := s.Databases[parent]; holder != nil {
		return holder.Row.Separator
	}

	return ""
}

// IsCaseless reports whether the database's names match without regard to
// case.
func (d *Database) IsCaseless() bool {
	return d.Row.FoldsCase || d.Callable.FoldsCase
}

// MatchValue reports whether a written value fits what a field takes. A
// scalar never fits a color or a block. A choice fits in any case if its
// database ignores case. A target fits a link, or a name if the field names a
// database.
func (s *Schema) MatchValue(expected Expected, text string) bool {
	unquoted := strings.Trim(text, `"`)
	switch expected.Type {
	case ValueTypeBool:
		return slices.Contains(game.Bools, text)

	case ValueTypeNumber:
		return jomini.Classify(text) == jomini.ClassNumber

	case ValueTypeDate:
		_, ok := jomini.ParseDate(text)
		return ok

	case ValueTypeChoice:
		database := s.Databases[expected.Database]
		folds := database != nil && database.IsCaseless()
		return slices.ContainsFunc(expected.Choices, func(choice string) bool {
			return choice == unquoted || folds && strings.EqualFold(choice, unquoted)
		})

	case ValueTypeReference:
		return jomini.Classify(unquoted).IsIdentifier()

	case ValueTypeTarget:
		return s.IsLink(unquoted) || expected.Database != "" && jomini.Classify(unquoted).IsIdentifier()

	case ValueTypeColor, ValueTypeBlock:
		return false

	case ValueTypeText:
		return text != unquoted || jomini.Classify(text).IsIdentifier()

	case ValueTypeCall:
		_, ok := jomini.ParseBracketed(unquoted, 0)
		return ok
	}

	return true
}

// namesDatabase reports whether a value is a name of a database: a reference,
// or a target that a name can be.
func (e Expected) namesDatabase() bool {
	return e.Database != "" && (e.Type == ValueTypeReference || e.Type == ValueTypeTarget)
}

// IsStated reports whether any source states the claim.
func (c Claim[T]) IsStated() bool { return c.Source != game.SourceNone }

// resolve sets the claim to the given value and source if the claim states
// nothing yet or the source outranks the one it states. A lower source number
// is a higher rank.
func (c *Claim[T]) resolve(value T, source game.Source) {
	if !c.IsStated() || source < c.Source {
		*c = Claim[T]{Value: value, Source: source}
	}
}

// take states another claim's value where its source outranks this one's, or
// where this one states nothing.
func (c *Claim[T]) take(other Claim[T]) {
	if other.IsStated() {
		c.resolve(other.Value, other.Source)
	}
}

// take gives a field each claim of the given claims that it does not state,
// or that outranks its own. It leaves Keys alone, since only the game's own
// files state it.
func (f *Field) take(other Claims) {
	f.Value.take(other.Value)
	f.Shape.take(other.Shape)
	f.Required.take(other.Required)
	f.Default.take(other.Default)
	f.Blurb.take(other.Blurb)
}

// walkField calls the given function on each field below a field with its
// key, depth first in key order.
func walkField(field *Field, visit func(key string, child *Field)) {
	for _, key := range slices.Sorted(maps.Keys(field.Children)) {
		child := field.Children[key]
		visit(key, child)
		walkField(child, visit)
	}
}

// buildChild returns the field's child for a key, adding one if there is
// none. A child with no source takes the given source.
func (f *Field) buildChild(key string, source game.Source) *Field {
	if f.Children == nil {
		f.Children = map[string]*Field{}
	}

	if f.Children[key] == nil {
		f.Children[key] = &Field{}
	}

	child := f.Children[key]
	child.Source = cmp.Or(child.Source, source)

	return child
}

// findChild returns the field's child for a key, or nil if there is none. A
// nil field has none.
func (f *Field) findChild(key string) *Field {
	if f == nil {
		return nil
	}

	return f.Children[key]
}

// claimsDatabase reports whether a field's value claim says the value is a
// name in a database. A nil field claims none.
func (f *Field) claimsDatabase() bool {
	return f != nil && f.Value.Value.namesDatabase()
}

// findNumberChild returns the field's child for a key, else its <number>
// child if the key's class is a number or a date. It returns nil if there is
// none.
func (f *Field) findNumberChild(key string, class jomini.Class) *Field {
	if child := f.findChild(key); child != nil || !class.IsNumberKey() {
		return child
	}

	return f.findChild(jomini.NumberKey)
}

// findPatternChild returns the field's child for a key, else its <number>
// child if the key's class is a number or a date, else its <any> child. It
// returns nil if there is none.
func (f *Field) findPatternChild(key string, class jomini.Class) *Field {
	return cmp.Or(f.findNumberChild(key, class), f.findChild(game.AnyKey))
}
