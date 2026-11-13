// Package lang is the script language of one game version. It holds three
// things. The first is the language's fixed rules: its keywords, the shapes of
// its blocks, where each database's definitions are written and which copy of
// a name the game keeps. The second is the model of what is learned about one
// game version: which databases exist, what each key takes and which commands
// run in which scope. The third is the one walk that reads a file against
// both. Every feature that reads script uses the walk's result.
//
// The fixed rules are tables of rows. A fact that is one value for each game
// is a field of the game's Dialect. A fact the games share is a row of a table
// that names the games it holds for.
//
// # Rows
//
//   - A row holds for the games its Games lists, and for every game when it
//     lists none.
//   - A game's own rows come before the rows of every game, and a lookup
//     takes the first row that fits.
//   - A row's comment cites where its fact comes from: a file of the install
//     and its line, a dump line or a wiki page. Nothing reads it; it lets a
//     reader check the row against the game.
//   - A row classifies and never accuses. Only a claim from the dumps, the
//     docs, the wiki or a definition in the files is firm enough to fail a
//     verdict (Source.CanAccuse).
//
// # Sources
//
//   - Every fact comes with a Source, and a lower source outranks a
//     higher one: the rows, the dumps, the docs, the wiki, the game's own
//     files, and inferred last. A claim keeps the fact of its best source.
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
//   - A folder names its database: common/ and the extension are dropped, so
//     common/traits is traits. A folder with no Declarations row is still a
//     database, whose top keys name its definitions.
//   - Which copy of a name the game keeps is said by the first Precedences
//     row that holds for the database, by its name or its folder. Where none
//     holds, the last copy by file name wins.
//   - In a game whose definitions take entry modes, a name may start with
//     one, as REPLACE:x does. A prefix that is no entry mode is part of the
//     name.
//   - A database with no DefinitionTexts row titles its definitions by their
//     names, and gives them no description.
//   - Of a game's localization replace folders, the one listed first wins.
//     For Vic3 no source says which wins, and the row's order is taken.
//
// # Keywords
//
//   - A block's shape lists the rules its keys are read by, in the order they
//     are tried. The first rule a key fits wins.
//   - A keyword fits a key in any letter case. Where several rows fit, the
//     first in Keywords wins, and a row written command/key fits only in that
//     command's block.
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
//   - A statement either defines a name, or its key is a keyword of the
//     language where it is written, or it is the first thing its block's
//     shape takes that has the key. A key with a parameter is not known
//     until the call is made, and neither are its block's shape and scope.
//   - A command fits only where the block's shape takes its database, so an
//     effect never fits in a trigger block. Each use of an effect, a trigger
//     or a link is a reference to it, and each link of a chain is its own.
//     A key written with an argument in parentheses, as
//     aptitude(court_jester_court_position), is read as the command, and its
//     argument as the command's ArgumentKey. A math keyword's value written
//     so, as opinion(scope:ruler) is, makes the references of its argument.
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
// The walk never fails: a key no rule explains has no field. SetScripted
// reads a file that cannot be parsed, or is gone, as declaring nothing.
package lang

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
	"paradox-modding-tools/internal/jomini"
)

// Source is where a fact about the language came from. A lower value outranks
// a higher one, and a claim keeps the fact of its best source.
type Source uint8

// The sources, best first.
const (
	SourceNone Source = iota

	// SourceRegistry is a row of this package's tables. A row says what a
	// thing is and never accuses a mod of a mistake.
	SourceRegistry

	// SourceDumps is the game's console dumps: what the game declares about
	// its own language.
	SourceDumps

	// SourceDocs is the prose an install ships beside its script, in .info,
	// .md and readme.txt files.
	SourceDocs

	// SourceWiki is the community wiki. It is taken where no source of the
	// game contradicts it.
	SourceWiki

	// SourceVanilla is what the game's own files write. It is evidence of
	// what the game accepts, and never accuses a mod.
	SourceVanilla

	// SourceInferred is a guess no source states. It never accuses a mod.
	SourceInferred

	// SourceDefinitions is what a definition in the files says about its own
	// name or parameter, as a modifier type says its modifier takes yes or no.
	// No other source states these facts, so its rank never decides anything.
	SourceDefinitions
)

// Claim is one fact about a field, as its best source states it.
type Claim[T any] struct {
	Value T

	// Source is the source that states the value. It is SourceNone when no
	// source states one.
	Source Source
}

// ValueType is the kind of value a field takes.
type ValueType string

// The kinds of value.
const (
	ValueTypeUnknown ValueType = "unknown"
	ValueTypeBool    ValueType = "bool"
	ValueTypeNumber  ValueType = "number"
	ValueTypeDate    ValueType = "date"
	ValueTypeText    ValueType = "text"

	// ValueTypeReference is the name of a definition in the database the
	// field names, as brave is in has_trait = brave.
	ValueTypeReference ValueType = "reference"

	// ValueTypeTarget is a link that leads to a scope of the type the field
	// expects, as root.father leads to a character. Where the field also
	// names a database, the name of one of its definitions fits too.
	ValueTypeTarget ValueType = "target"

	// ValueTypeChoice is one of a fixed list of values.
	ValueTypeChoice ValueType = "choice"

	ValueTypeColor ValueType = "color"
	ValueTypeBlock ValueType = "block"

	// ValueTypeCall is a data call, as [ROOT.GetName] is.
	ValueTypeCall ValueType = "call"
)

// Expected is what a field's value must be.
type Expected struct {
	Type ValueType `json:"type"`

	// Database is the database whose definitions a reference names, and that
	// a target may name. It is "" for none.
	Database string `json:"database,omitempty"`

	// Scope is the type of scope a target's link must lead to, such as
	// character. It is "" for any.
	Scope string `json:"scope,omitempty"`

	// Choices are the values a choice allows.
	Choices []string `json:"choices,omitempty"`

	// Values is whether a script value may be written where a number is
	// expected.
	Values bool `json:"values,omitempty"`

	// Nested is whether the name of a definition nested inside one of the
	// database's own fits too, as a policy inside an EU5 law does.
	Nested bool `json:"nested,omitempty"`

	// Stem is the name the value stands for in the database, with %s for the
	// value. CK3's activity log looks up <value>_title. It is "" where the
	// value is the name itself.
	Stem string `json:"stem,omitempty"`
}

// Vanilla is what the game's own files write at a field, counted.
type Vanilla struct {
	// Count is how many times the field is written, and Blocks how many of
	// those times it is written as a block.
	Count  int
	Blocks int

	// Values holds how many times each value is written.
	Values map[string]int
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
	Value    Claim[Expected]
	Shape    Claim[Shape]
	Required Claim[Requirement]
	Default  Claim[string]
	Blurb    Claim[string]

	// Keys is the database whose definitions the keys of the field's block
	// name, as the keys of a culture's opinions block name cultures.
	Keys Claim[string]
}

// Field is one key a definition may write, with what it takes and the fields
// inside it. A database's root field is the whole block of a definition.
type Field struct {
	// Children are the fields inside it, by key. A key written as <number>
	// stands for any number, and <any> for any key.
	Children map[string]*Field

	Claims

	// Source is the best source that declares the field. It is SourceNone
	// when no source does.
	Source Source

	// Weighted is whether the field's key is a number that gives its chance
	// among its siblings, as 10 = some_event does in an on_action's
	// random_events.
	Weighted bool

	Vanilla Vanilla
}

// Definition is a name the schema itself knows, with what the name takes. From
// the dumps it is an effect, a trigger, a link, a scope type, a modifier or an
// on_action. From the game's files it is a scripted definition, a modifier
// type or an event namespace. Every other definition is known to the index
// alone.
type Definition struct {
	// Database is the database it belongs to. It is "" for a widget type the
	// game draws itself, which no file declares.
	Database string

	Key string

	// Blurb is its hover text, as the dumps or the docs give it.
	Blurb string

	// Usage is the block the dumps write to show how it is written, as
	// create_character = { name = … }. It is "" where the dumps write none.
	Usage string

	// Field is what it takes: the keys of a command's block and its value, or
	// the name written after a link's colon. It is nil where no source says.
	Field *Field

	// Union is the keys of an effect and of a trigger of one name, together.
	// A block that may hold either reads it. It is nil unless the dumps
	// declare both and each has keys of its own.
	Union *Field

	// Scopes are the scope types it may be written in, none for any. Target
	// is the scope type it leads to, and for an on_action the scope it runs
	// in. Target is "" for none.
	Scopes []string
	Target string

	// Hardcoded is whether the game fires an on_action from its own code, as
	// "From Code: Yes" says in the dumps.
	Hardcoded bool

	// Saves holds, for each parameter of a scripted definition whose body
	// saves the argument under a name, the kind of name: scope for
	// save_scope_as = $TARGET$.
	Saves map[string]string

	// Path and Line say where the dumps declare it, lines counting from 1.
	// They are "" and 0 for one from the shipped backup or from a row.
	Path string
	Line int

	// Star lists, for a key with a * in it, the databases whose names fill
	// the star, as the scripted relations fill has_relation_*. Patterns
	// lists, for a plain key, the star keys it fits.
	Star     []string
	Patterns []*Definition

	// ArgumentKey is the key that an argument written in parentheses stands
	// for, as in aptitude(court_jester_court_position). It is "" for none.
	ArgumentKey string

	// Prefixed is whether a link is written with a name after a colon, as
	// scope:x is. Wild is whether the dumps call the link a wild card.
	Prefixed, Wild bool

	// Iterates is whether a command runs its block on each item of a list
	// whose scope type the dumps do not name.
	Iterates bool

	// Runs is whether script runs in a scope type: the dumps say triggers or
	// effects do.
	Runs bool
}

// Example is one place where the game's own files call a data function.
type Example struct {
	Path string

	// Line counts from 0.
	Line int
}

// Database is one database: the fields its definitions may write, and the
// rules that hold for it.
type Database struct {
	Name string

	// Definitions are the definitions the schema itself knows, by key. For a
	// dump's database they are the dump's entries. For a database of files
	// they are the copy the game keeps of each definition that is called by
	// name (in lower case where names match in any case), each modifier type,
	// each event namespace and each widget type the game draws itself. Any
	// other database has none.
	Definitions map[string]*Definition

	// Keys are the keys of Definitions, sorted. Patterns are the definitions
	// whose key has a star, sorted by key.
	Keys     []string
	Patterns []*Definition

	// Nested are the databases whose definitions are written inside this
	// one's, as a law's policies are.
	Nested []string

	// Field is the block of one definition: the keys it may write.
	Field *Field

	// Declaration is what the sources state about the database for the game.
	// It is the zero Declaration where no row names the database.
	Declaration Declaration

	// Precedence is which copy of a name the game keeps.
	Precedence Precedence

	// Readme is the doc file its blurb came from, else the first doc that
	// describes the whole database. It is "" if there is none.
	Readme string
}

// ScopeType is one type of scope, with what script may do in it.
type ScopeType struct {
	// Triggers is whether triggers are evaluated in it, and Effects whether
	// effects run in it.
	Triggers bool
	Effects  bool
}

// DataFunction is one function of a data type, as the data types dump
// declares it. GetName of Character is one.
type DataFunction struct {
	Name string

	// Returns is the data type it returns.
	Returns string

	Arguments []string
	Blurb     string

	// Expansion is the text a data binding macro stands for. It is "" for a
	// function of the game's own.
	Expansion string

	// Promote is whether the dumps define it as a promote: it leads to an
	// object, not to a value.
	Promote bool

	// Examples are the first places the game's files call it, sorted by path
	// and then line.
	Examples []Example
}

// DataType is one type of the data calls written in localization and GUI
// text, with its functions.
type DataType struct {
	Name      string
	Functions map[string]DataFunction
}

// OnAction is one on_action as the dumps declare it.
type OnAction struct {
	// Scope is the scope type it runs in, "" where the dumps name none.
	Scope string

	// Hardcoded is whether the game fires it from its own code, as "From
	// Code: Yes" says in the dumps.
	Hardcoded bool
}

// relay is one place where the body of a scripted definition passes one of its
// own parameters on to another scripted definition, as
// other_effect = { TARGET = $WHO$ } does.
type relay struct {
	// Callee is the name of the definition called, as written. Argument is
	// the key written in the call, and Parameter the body's own parameter
	// written as its value.
	Callee    string
	Argument  string
	Parameter string
}

// typing is one place where the body of a scripted definition writes a
// parameter as a whole value in a spot that takes a name of some database:
// after a link's colon, as in title:$TITLE$, or as the value of a key.
type typing struct {
	Parameter string

	// Prefix is the link written before the colon. It is "" where the
	// parameter is a key's value.
	Prefix string

	// Field is the field of the key the parameter is the value of. It is nil
	// where the parameter follows a link's colon.
	Field *Field
}

// ScriptedCopy is one copy of a scripted definition, as one file declares it.
type ScriptedCopy struct {
	Copy Copy

	// File is the key of its file and Rel the file's path from its stage
	// folder. Sequence is the file's place among the files that declare
	// scripted definitions, the game's before the mods'.
	File     string
	Rel      string
	Sequence int

	// Definition is what a call of it takes, its field holding the
	// parameters. It is nil in a database whose definitions take none.
	Definition *Definition

	// relays and typings are what was found in its body, in the order
	// written: the parameters it passes on to other calls, and the
	// parameters it writes where a database's name goes. No cache keeps them.
	relays  []relay
	typings []typing
}

// modScripted is the scripted definitions the mods' files declare.
type modScripted struct {
	// byFile holds what each file declares, by the file's key. sequence
	// counts the files placed so far. unread holds the keys of the files the
	// game does not read, as they were when the winners were last picked.
	byFile   map[string]*declarer
	sequence int
	unread   map[string]bool

	// callees holds, by database and name, the definition a call finds
	// wherever the mods change what the game's files give: the copy the game
	// keeps among the mods' and its own, or nil if no copy is left. They are
	// picked again only when a file that declares one changes.
	callees map[string]*Definition

	// winners holds the callees, and with them each modifier type and each
	// event namespace the mods declare. A name it lacks is the game's own.
	winners map[string]*Definition
}

// SourceState says where the data of one source came from and how fresh it
// is.
type SourceState struct {
	// Origin is install, shipped or none.
	Origin string `json:"origin"`

	WrittenAt time.Time `json:"writtenAt"`

	// Stale is whether a dump file is older than the install's last update.
	Stale bool `json:"stale"`

	// Folders are the folders the files were read from. They are none for
	// the shipped backup.
	Folders []string `json:"folders"`
}

// Schema is the language of one game version, as built from the dumps, the
// docs and the game's own files.
type Schema struct {
	// Game is the game's id.
	Game string

	// Version is the install's version. A new schema has "" until the
	// workspace sets it.
	Version string

	// Dumps says where the dumps' data came from.
	Dumps SourceState

	// Databases holds every database, by name.
	Databases map[string]*Database

	// Setups are the folders that the game's bookmarks name for their start
	// setups, without the stage folder. They are none for a game with a
	// single start.
	Setups []string

	// Callees holds, for each kind of caller (a trigger block, an effect
	// block, a value block or a GUI key), the databases whose definitions it
	// may call.
	Callees map[string][]string

	// Customs holds the names of the customizable localizations the dumps
	// list, whether or not a file defines them.
	Customs map[string]bool

	// DataTypes holds the data types, by name.
	DataTypes map[string]DataType

	// TypeDatabases holds, for the words of a data type's name, the database
	// of the names the type stands for: "government type" gives governments.
	// It holds "" where several databases share the words.
	TypeDatabases map[string]string

	// InlineDatabases holds, for each key that declares a definition inside
	// an events file, as scripted_effect x = { } does, the definition's
	// database.
	InlineDatabases map[string]string

	// ScriptedCopies holds every copy of each scripted definition the game's
	// files declare, by database and name.
	ScriptedCopies map[string][]ScriptedCopy

	// UnlistedCalls holds the steps of data calls, each as Type.step, that
	// the game's own localization and GUI text write though no dump declares
	// the step for that type.
	UnlistedCalls map[string]bool

	// Lists, Events, OnActions, Messages, Concepts, ModifierTypes, Effects
	// and Values are the names of the databases of scripted lists, events,
	// on_actions, the messages an effect sends the player, game concepts,
	// modifier types, scripted effects and script values. Each is "" for a
	// game with no such database.
	Lists         string
	Events        string
	OnActions     string
	Messages      string
	Concepts      string
	ModifierTypes string
	Effects       string
	Values        string

	// FireDatabases are the databases whose definitions fire one another:
	// events, on_actions, decisions and messages.
	FireDatabases []string

	// mods holds the mods' scripted definitions, which a call looks in after
	// the game's. Each rescan puts a new table in its place.
	mods atomic.Pointer[modScripted]
}

// CanAccuse reports whether a claim from the source is firm enough to call a
// mod's script wrong. Only the dumps, the docs, the wiki and what a definition
// says about itself are.
func (s Source) CanAccuse() bool {
	return s == SourceDumps || s == SourceDocs || s == SourceWiki || s == SourceDefinitions
}

// IsStated reports whether any source states the claim.
func (c Claim[T]) IsStated() bool { return c.Source != SourceNone }

// Resolve gives the claim a value and its source. It does nothing if the claim
// already states a value from the same source or a better one.
func (c *Claim[T]) Resolve(value T, source Source) {
	if !c.IsStated() || source < c.Source {
		*c = Claim[T]{Value: value, Source: source}
	}
}

// Take gives the claim what another claim states, as Resolve does. It does
// nothing if the other claim states nothing.
func (c *Claim[T]) Take(other Claim[T]) {
	if other.IsStated() {
		c.Resolve(other.Value, other.Source)
	}
}

// Take gives the field each of the given claims, as Resolve does. It leaves
// out the database of the field's keys, which only the count of the game's
// files states.
func (f *Field) Take(other Claims) {
	f.Value.Take(other.Value)
	f.Shape.Take(other.Shape)
	f.Required.Take(other.Required)
	f.Default.Take(other.Default)
	f.Blurb.Take(other.Blurb)
}

// WalkField calls a function for every field below the given one, each with
// its key. A field comes before the fields inside it, and siblings come in
// order of their keys.
func WalkField(field *Field, visit func(key string, child *Field)) {
	for _, key := range slices.Sorted(maps.Keys(field.Children)) {
		child := field.Children[key]
		visit(key, child)
		WalkField(child, visit)
	}
}

// BuildChild returns the field inside this one for a key, adding it if there
// is none. A field that has no source yet takes the given one.
func (f *Field) BuildChild(key string, source Source) *Field {
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

// findChild returns the field inside this one for a key. It returns nil if
// there is none, and for a nil field.
func (f *Field) findChild(key string) *Field {
	if f == nil {
		return nil
	}

	return f.Children[key]
}

// claimsDatabase reports whether the field's value is the name of a
// definition in some database. A nil field's is not.
func (f *Field) claimsDatabase() bool {
	return f != nil && f.Value.Value.namesDatabase()
}

// findNumberChild returns the field inside this one for a key. A key that is
// a number or a date, with no field of its own, gets the field that stands for
// any number. It returns nil if there is none.
func (f *Field) findNumberChild(key string, class jomini.Class) *Field {
	if child := f.findChild(key); child != nil || !class.IsNumberKey() {
		return child
	}

	return f.findChild(jomini.NumberKey)
}

// findPatternChild returns the field findNumberChild returns, and failing
// that the field that stands for any key. It returns nil if there is neither.
func (f *Field) findPatternChild(key string, class jomini.Class) *Field {
	return cmp.Or(f.findNumberChild(key, class), f.findChild(AnyKey))
}

// namesDatabase reports whether the value is the name of a definition in a
// database: a reference, or a target whose field also names a database.
func (e Expected) namesDatabase() bool {
	return e.Database != "" && (e.Type == ValueTypeReference || e.Type == ValueTypeTarget)
}

// declares reports whether a row, the dumps or the docs say a key is a field
// of the database's definitions. Such a key never opens a nested definition.
func (d *Database) declares(key string) bool {
	child := d.Field.findChild(key)

	return child != nil && child.Source != SourceNone && child.Source <= SourceDocs
}

// IsCaseless reports whether the database's names match in any letter case.
func (d *Database) IsCaseless() bool {
	return d.Declaration.FoldsCase
}

// FindDatabase returns the database a file's definitions belong to: the one
// whose folder is the file's own, or the nearest folder above it. It returns
// false if no database has such a folder.
func (s *Schema) FindDatabase(file files.File) (*Database, bool) {
	for folder := file.StageRel(); folder != "."; folder = path.Dir(folder) {
		if database, ok := s.Databases[ToDatabase(folder)]; ok && database.Declaration.Folder == folder {
			return database, true
		}
	}

	return nil, false
}

// classDatabases holds the folder of the database for each class name of the
// dumps that its words alone do not lead to.
var classDatabases = map[string]string{
	"CCultureInnovationTypeDatabase":     "culture/innovations",
	"CCultureTraditionDatabase":          "culture/traditions",
	"CCourtPositionTypesDatabase":        "court_positions/types",
	"CTravelPointOfInterestTypeDatabase": "travel/point_of_interest_types",
}

// FindDatabaseByClass returns the database that a class name of the dumps
// stands for, as CTraitDatabase stands for traits. It returns false if no
// database fits the name.
func (s *Schema) FindDatabaseByClass(class string) (*Database, bool) {
	words := strings.TrimPrefix(class, "C")
	if lower := strings.ToLower(words); strings.HasSuffix(lower, "database") {
		words = words[:len(words)-len("database")]
	}

	label, ok := s.ResolveTypeDatabase(words)
	if folder, listed := classDatabases[class]; listed {
		label, ok = ToDatabase(folder), true
	}

	database := s.Databases[label]

	return database, ok && database != nil
}

// ResolveTypeDatabase returns the database of the names a data type stands
// for, as GovernmentType stands for governments. It returns false if no
// database fits the type's words, or several do.
func (s *Schema) ResolveTypeDatabase(dataType string) (string, bool) {
	phrase := ToPhrase(dataType)
	label, ok := s.TypeDatabases[phrase]
	if stem, isTyped := strings.CutSuffix(phrase, " type"); !ok && isTyped {
		label = s.TypeDatabases[stem]
	}

	return label, label != ""
}

// ToPhrase returns a name written as GovernmentType or government_type as
// words in lower case: government type.
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

// JoinNested returns the name of a nested definition: the name of the
// definition that holds it, the database's separator and its own key, as
// NAI|X for the define X of the group NAI. It returns the key alone where the
// database writes no separator, and for a definition nothing holds.
func (s *Schema) JoinNested(database, holder, key string) string {
	separator := s.findSeparator(database)
	if separator == "" || holder == "" {
		return key
	}

	return holder + separator + key
}

// SplitNested returns the key of a nested definition from its name, as X from
// NAI|X. A name with no separator is returned as it is.
func (s *Schema) SplitNested(database, nested string) string {
	separator := s.findSeparator(database)
	if _, key, ok := strings.Cut(nested, separator); ok && separator != "" {
		return key
	}

	return nested
}

// findSeparator returns the character that joins a nested definition's name
// to the name of the definition that holds it, as | does in NAI|X. It returns
// "" for a database that is not nested, or whose parent writes none.
func (s *Schema) findSeparator(database string) string {
	parent, _, nested := strings.CutLast(database, ".")
	if !nested {
		return ""
	}

	if holder := s.Databases[parent]; holder != nil {
		return holder.Declaration.Separator
	}

	return ""
}

// MatchValue reports whether a value, as written, fits what a field expects.
// A value that is not a block never fits a color or a block. A choice fits in
// any letter case where its database's names do. A target fits a link, and a
// plain name too where the field names a database. A kind of value with no
// check of its own fits anything.
func (s *Schema) MatchValue(expected Expected, text string) bool {
	unquoted := strings.Trim(text, `"`)
	switch expected.Type {
	case ValueTypeBool:
		return slices.Contains(Bools, text)

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
