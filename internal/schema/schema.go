// Package schema models the language of one game version: which databases exist, what each key takes and which
// commands run in which scope. It is built once per install from the dumps, the docs and a count of the game's own
// files, then cached. It also owns the one walk over a file; every reader of script is a visitor on it.
package schema

import (
	"time"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/jomini"
)

// A Claim is one fact about a field as its best source states it. Source is game.SourceNone when no source states
// it.
type Claim[T any] struct {
	Value  T
	Source game.Source
}

// IsStated reports whether any source states the claim.
func (claim Claim[T]) IsStated() bool { panic("stub") }

// A ValueType says what kind of value a field takes.
type ValueType string

const (
	ValueUnknown   ValueType = "unknown"
	ValueBool      ValueType = "bool"
	ValueNumber    ValueType = "number"
	ValueDate      ValueType = "date"
	ValueText      ValueType = "text"
	ValueReference ValueType = "reference" // a name of Expected.Database
	ValueChoice    ValueType = "choice"    // one of Expected.Choices
	ValueColor     ValueType = "color"
	ValueBlock     ValueType = "block"
	ValueCall      ValueType = "call" // a data call
)

// Expected says what a field's value must be: its type, the database a reference names, or the choices allowed.
type Expected struct {
	Type     ValueType `json:"type"`
	Database string    `json:"database,omitempty"`
	Choices  []string  `json:"choices,omitempty"`
}

// A Requirement says whether a field must be written. Level is optional, always or conditional, and Condition
// holds the docs' wording for a conditional one.
type Requirement struct {
	Level     string `json:"level"` // optional, always, conditional
	Condition string `json:"condition,omitempty"`
}

// Vanilla is what the game's own files write at a field: how many times, how many of those as a block or as a
// color, and each value with its count.
type Vanilla struct {
	Count  int
	Blocks int
	Colors int
	Values map[string]int
}

// Claims holds every fact the sources state about one field.
type Claims struct {
	Value    Claim[Expected]
	Shape    Claim[string] // a game.Shapes key
	Scope    Claim[string]
	Required Claim[Requirement]
	Default  Claim[string]
	Blurb    Claim[string]
}

// A Field is one key that may be written in a definition, with what it takes and the fields that may be written
// inside it. A database's root field is the whole definition block.
type Field struct {
	Children map[string]*Field // by written key, or by a form: <number>, <any>
	Claims
	Source  game.Source // the highest-ranked declared source that writes the field; SourceNone for none
	Vanilla Vanilla
}

// A Command is one effect, trigger or link the dumps declare.
type Command struct {
	Declared  []string // what declares it: game.DeclaredEffect, DeclaredTrigger, DeclaredLink, in dump order
	Scopes    []string // the scopes it runs in; empty for any
	Target    string   // the scope a link leads to; "" for none
	Arguments *Field   // the keys it takes, as fields
	Blurb     string
	Global    bool // a link that reads the same from any scope
	Wild      bool // a link that is a pattern, so it is never matched by a written name
	Lists     bool // it iterates a list and has no target, so the scope inside its block is unknown
}

// A Database is one database of the game: the field tree of its definitions and the game's rows that govern it.
type Database struct {
	Name       string
	Folder     string // the folder or file its definitions are written in; "" for a nested database
	Definition *Field
	Row        game.Database   // where its definitions are written and how each is named
	Callable   game.Callable   // the words that call its definitions; zero when nothing calls them
	Precedence game.Precedence // how several definitions of one name combine
	Documented map[string]bool // every key the docs mention for its definitions
}

// A ScopeType is one kind of scope the game runs script in, and what script can do there.
type ScopeType struct {
	Name      string
	Variables bool
	Triggers  bool
	Effects   bool
}

// A DataFunction is one function of a data type, as the data types dump declares it.
type DataFunction struct {
	Name      string
	Returns   string
	Arguments []string
	Blurb     string
}

// A DataType is one type of the data binding language, with its functions.
type DataType struct {
	Name      string
	Functions map[string]DataFunction
}

// A Modifier is one modifier the dumps declare. It may be written as a key inside a modifier block.
type Modifier struct {
	Name  string
	Blurb string
}

// An OnAction is one on_action the dumps declare, with the scope it runs in.
type OnAction struct {
	Name  string
	Scope string
	Blurb string
}

// A CustomLoc is one custom localization the dumps declare, with its scope.
type CustomLoc struct {
	Name  string
	Scope string
}

// A ScriptedDefinition is a scripted effect, trigger, value or list the game defines, with the parameters it
// takes.
type ScriptedDefinition struct {
	Database   string
	Parameters *Field // nil when the database's definitions take no parameters
}

// A SourceState says where one source's data came from and how fresh it is.
type SourceState struct {
	Origin    string    `json:"origin"` // yours, install, shipped, none
	WrittenAt time.Time `json:"writtenAt"`
	Stale     bool      `json:"stale"` // older than the install
}

// A Schema is the language of one game version, as built from the dumps, the docs and the game's own files.
type Schema struct {
	Game                string
	Version             string
	Sources             map[game.Source]SourceState
	Databases           map[string]*Database
	Callees             map[string][]string // by caller: the databases a call through it resolves in
	Commands            map[string]*Command
	Scopes              map[string]ScopeType
	Saved               map[string]bool // scope names the game saves from code
	Modifiers           map[string]Modifier
	OnActions           map[string]OnAction
	DataTypes           map[string]DataType
	CustomLoc           map[string]CustomLoc
	Prefixes            map[string]string // by link prefix: the database its names are in; "" for none
	ScriptedDefinitions map[string]ScriptedDefinition
	WidgetTypes         map[string]bool
	Lists               string // the database of scripted lists, or "" if the game has none
	Functions           string // the database of data function macros, or "" if the game has none
	Events              string // the database of events, or "" if the game has none
	Concepts            string // the database of game concepts, or "" if the game has none
}

// Build makes the schema of a game version from an install. It reads the dumps, falls back to the shipped backup
// for any dump file that is missing, reads the docs, and then counts what the game's own files write, parsing each
// file with the given function.
func Build(game *game.Game, installPath string, vanilla []string, parse func(path string) (*jomini.Tree, error)) (*Schema, error) {
	panic("stub")
}

// LookupDatabase returns the database with the given name, or false if there is none.
func (schema *Schema) LookupDatabase(name string) (*Database, bool) { panic("stub") }

// FindDatabase returns the database that a file belongs to, going by its relative path and the game's folder
// rules. It returns false if no database claims the file.
func (schema *Schema) FindDatabase(rel string) (*Database, bool) { panic("stub") }

// FindDatabaseByClass returns the database that a class name in the dumps refers to, such as CTraitDatabase. It
// returns false if there is none.
func (schema *Schema) FindDatabaseByClass(class string) (*Database, bool) { panic("stub") }

// FindDatabaseByHouses returns the one database that serves a purpose, such as events, lists or concepts. It
// returns false if the game has none.
func (schema *Schema) FindDatabaseByHouses(houses string) (*Database, bool) { panic("stub") }

// LookupCommand returns the effect, trigger or link with the given name, or false if there is none.
func (schema *Schema) LookupCommand(name string) (*Command, bool) { panic("stub") }

// ResolveLink returns the scope that a link leads to when followed from a given scope. It returns false if the
// link is unknown or leads nowhere.
func (schema *Schema) ResolveLink(link, scope string) (string, bool) { panic("stub") }

// ResolveCallTypes returns the type that each step of a data call leads to, starting from the block's data
// context. A step no dump declares gets "", and the steps after it are left out.
func (schema *Schema) ResolveCallTypes(call jomini.Call, datacontext string) []string { panic("stub") }

// ResolveTypeDatabase returns the database that holds the names of a data type, or false if there is none.
func (schema *Schema) ResolveTypeDatabase(dataType string) (string, bool) { panic("stub") }

// MatchValue reports whether a written value fits what a field takes.
func (schema *Schema) MatchValue(expected Expected, text string) bool { panic("stub") }
