// Package schema holds what the sources state about one game version's script: each database's positions
// with their claims and vanilla's evidence, each command the dumps declare, and the one walker every reader
// of script uses. It decides whether a claim may accuse a user's file (MayAccuse); it never looks at a mod.
package schema

import (
	"slices"
	"strings"
	"time"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/jomini"
)

// Source says who stated a claim. The order is the trust order: a source writes a claim only where every
// earlier one left it empty (SetFirst).
type Source uint8

// The sources, in trust order.
const (
	SourceNone     Source = iota
	SourceRegistry        // the game's rows and the language rules: they classify and never accuse
	SourceDumps           // the console dumps: what the game declares about its own language
	SourceVanilla         // the install's own files: evidence, never a claim that accuses
	SourceDocs            // the prose the install ships beside its script
	SourceWiki            // the community wiki's examples, last in trust
)

// Declared reports whether a source states claims that may accuse: the dumps, the docs or the wiki.
func (s Source) Declared() bool { return s == SourceDumps || s == SourceDocs || s == SourceWiki }

// Claim is a value and the source that stated it; Source is SourceNone while the claim is empty.
type Claim[T any] struct {
	Value  T
	Source Source
}

// ExpectedKind is the kind of word a position holds; ExpectsBlock is a doc's block, whose kind none names.
type ExpectedKind uint8

// The kinds of word a position holds.
const (
	ExpectsUnknown ExpectedKind = iota
	ExpectsNumber
	ExpectsBool
	ExpectsDate
	ExpectsString
	ExpectsReference
	ExpectsLoc
	ExpectsFile
	ExpectsScope
	ExpectsBlock
)

// Expected is what a source says a position holds.
type Expected struct {
	Kind     ExpectedKind
	Database string   // ExpectsReference: the database the word names
	Choices  []string // a declared word list, sorted; empty when none
}

// Level is how strongly a position is required inside its parent.
type Level uint8

// The levels a position's requirement can have.
const (
	LevelUnknown Level = iota
	LevelOptional
	LevelAlways
	LevelConditional
)

// Requirement is a position's level, with the condition text for LevelConditional.
type Requirement struct {
	Level Level
	When  string
}

// Shapes is the set of forms vanilla writes at a position, as bits.
type Shapes uint16

// The forms a written value or block takes.
const (
	ShapeNumber Shapes = 1 << iota
	ShapeBool
	ShapeDate
	ShapeIdent
	ShapeString
	ShapeBlock
	ShapeCommand // a block that holds a trigger or an effect
	ShapeTrigger // a trigger written in the block
	ShapeEffect  // an effect written in the block
)

// shapeOf returns the form a scalar is written in.
func shapeOf(written *jomini.Scalar) Shapes {
	if written.Quoted {
		return ShapeString
	}
	switch jomini.Literal(written.Text) {
	case jomini.LitNumber:
		return ShapeNumber
	case jomini.LitDate:
		return ShapeDate
	}
	if slices.Contains(game.Bools, written.Text) {
		return ShapeBool
	}
	return ShapeIdent
}

// Vanilla is what vanilla writes at a position; it is never a claim.
type Vanilla struct {
	In     int            // how many of the parent's written blocks hold this key
	Blocks int            // how many times this position was written as a block
	Shapes Shapes         // the forms written here
	Values map[string]int // words written, with counts
}

// Position is one place in a database's entry tree or a command's arguments: its claims, each with its
// source, and vanilla's evidence.
type Position struct {
	Children   map[string]*Position // below it, by written name or form
	Value      Claim[Expected]
	Block      Claim[game.Block]
	Scope      Claim[string]
	Required   Claim[Requirement]
	Default    Claim[string]
	Blurb      Claim[string]
	DeclaredBy Source // the first declared source that writes the position: the dumps anywhere, the docs and wiki only where vanilla writes it too
	Vanilla    Vanilla
}

// Command is an effect, trigger or link the dumps declare.
type Command struct {
	Kind   game.Kinds
	Scopes []string  // where it runs; empty: any
	Out    string    // where it leads; "" none
	Args   *Position // its usage as positions
	Blurb  string
	Global bool // a link the dumps call global: it reads the same from any scope
	Wild   bool // a link the dumps call Wild Card: a pattern, never read from a written name
	Lists  bool // it iterates a list and has no target: its block's scope is unknown
}

// Database is one script folder's entries: the entry's position tree and the folder's registry row.
type Database struct {
	Name   string
	Entry  *Position
	Row    game.Folder
	Fields map[string]bool // every key name the docs and wiki write in its entries
}

// SourceState says where a source's data came from and whether it is out of date.
type SourceState struct {
	From    string    // "yours", "install", "shipped" or "none"
	Written time.Time // when the source was written, by its oldest file; zero when unknown
	Stale   bool
}

// Scope is one scope type the dumps declare; each flag is false only where the dump says no.
type Scope struct {
	Name                         string
	Variables, Triggers, Effects bool // stores variables, evaluates triggers, executes effects
}

// ScriptedEntry is a scripted entry as a call reads it: at its own parameters, or at its database's entry
// where the database takes none. It names its database, so no entry tree is held twice.
type ScriptedEntry struct {
	Database   string
	Parameters *Position // nil where the database takes no parameters
}

// Schema is everything the sources state about one game version.
type Schema struct {
	Game, Version   string
	Sources         map[Source]SourceState
	Databases       map[string]*Database
	Callees         map[string][]string // CalledIn word → the databases a call by it resolves in, in name order
	Commands        map[string]*Command
	Scopes          map[string]Scope
	Saved           map[string]bool // scope names the game saves from code
	Modifiers       map[string]Modifier
	OnActions       map[string]OnAction
	DataTypes       map[string]DataType
	CustomLoc       map[string]CustomLoc
	Prefixes        map[string]string        // a link prefix → the database the names written after it are in; "" none
	ScriptedEntries map[string]ScriptedEntry // vanilla's scripted entries by calling word and name (word:name)
	Lists           string                   // the database of scripted lists (a folder row Iterated); "" none
}

// Link returns the declared link a lower-case key starts with: the first link of a chain (root.liege), by
// name or by the prefix before ':' (c:FRA, scope:x), never a Wild Card link; nil for none.
func (s *Schema) Link(key string) *Command {
	first, _, _ := strings.Cut(key, ".")
	prefix, _, _ := strings.Cut(first, ":")
	for _, name := range []string{first, prefix} {
		if command := s.Commands[name]; command != nil && command.Kind&game.KindLink != 0 && !command.Wild {
			return command
		}
	}
	return nil
}

// DatabaseNamed returns the database a word names: the one called by the word or its plural, else the one
// whose last path word is either; "" for none or several.
func (s *Schema) DatabaseNamed(word string) string {
	var named []string
	for _, name := range []string{word, word + "s"} {
		if database := s.Databases[name]; database != nil && !database.Row.History {
			named = append(named, name)
		}
	}
	exact := len(named) > 0
	for name, database := range s.Databases {
		last := name[strings.LastIndexByte(name, '/')+1:]
		if !exact && !database.Row.History && (last == word || last == word+"s") {
			named = append(named, name)
		}
	}
	if len(named) == 1 {
		return named[0]
	}
	return ""
}

// DatabasesWhere returns the names of the databases whose registry row passes a test, in name order.
func (s *Schema) DatabasesWhere(test func(game.Folder) bool) (names []string) {
	for name, database := range s.Databases {
		if test(database.Row) {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}

// Child returns the position under a written name; nil when there is none.
func (p *Position) Child(name string) *Position {
	if p == nil {
		return nil
	}
	return p.Children[positionName(name)]
}

// EnsureChild returns the position under a written name, making it when it is missing. The name is cloned
// so a position never pins a file's text.
func (p *Position) EnsureChild(name string) *Position {
	if child := p.Child(name); child != nil {
		return child
	}
	if p.Children == nil {
		p.Children = map[string]*Position{}
	}
	child := &Position{}
	p.Children[strings.Clone(positionName(name))] = child
	return child
}

// positionName returns the name a written key has as a position: a number is <number>, a date <date>, a
// prefix:x word <prefix:>, anything else itself.
func positionName(name string) string {
	switch jomini.Literal(name) {
	case jomini.LitNumber:
		return "<number>"
	case jomini.LitDate:
		return "<date>"
	}
	if prefixed, found := jomini.SplitPrefixed(name); found {
		return "<" + prefixed.Prefix + ":>"
	}
	return name
}

// SetFirst writes a claim only while it is empty, so a source writes only what an earlier one left unsaid.
func SetFirst[T any](claim *Claim[T], value T, from Source) {
	if claim.Source == SourceNone {
		claim.Value, claim.Source = value, from
	}
}

// Check names which claim of a position, or of the dumps, a diagnostic wants to accuse with.
type Check uint8

// The claims a diagnostic can accuse with.
const (
	CheckValue Check = iota
	CheckRequired
	CheckCommand
)

// MayAccuse reports whether a claim may accuse a user's code: a declared source states it, the source is
// fresh, and vanilla's evidence at the position does not contradict it. The registry and vanilla have no
// Sources entry, so they never accuse. A docs or wiki claim, and a declared word list from any source,
// accuse only where vanilla writes the position.
func (s *Schema) MayAccuse(check Check, position, parent *Position) bool {
	from := SourceDumps
	switch check {
	case CheckValue:
		from = position.Value.Source
	case CheckRequired:
		from = position.Required.Source
	}
	state, found := s.Sources[from]
	if !found || state.Stale {
		return false
	}
	listed := check == CheckValue && len(position.Value.Value.Choices) > 0
	if (from == SourceDocs || from == SourceWiki || listed) && position.Vanilla.In == 0 {
		return false
	}
	switch check {
	case CheckValue:
		return valueAgrees(position)
	case CheckRequired:
		return parent != nil && position.Required.Value.Level == LevelAlways &&
			position.Vanilla.In >= parent.Vanilla.Blocks
	}
	return true
}

// valueAgrees reports whether vanilla's evidence at a position fits the value claimed there: every form it
// writes is one the type allows, and every word it writes is in the declared list.
func valueAgrees(position *Position) bool {
	expected := position.Value.Value
	var allowed Shapes
	switch expected.Kind {
	case ExpectsNumber:
		allowed = ShapeNumber
	case ExpectsBool:
		allowed = ShapeBool
	case ExpectsDate:
		allowed = ShapeDate
	case ExpectsString, ExpectsReference, ExpectsLoc, ExpectsFile, ExpectsScope:
		allowed = ShapeIdent | ShapeString
	default:
		if len(expected.Choices) == 0 {
			return false
		}
		allowed = ShapeIdent | ShapeString
	}
	// the command forms mark the block, not a value
	if position.Vanilla.Shapes&^(allowed|ShapeCommand|ShapeTrigger|ShapeEffect) != 0 {
		return false
	}
	if len(expected.Choices) == 0 {
		return true
	}
	for word := range position.Vanilla.Values {
		if _, listed := slices.BinarySearch(expected.Choices, word); !listed {
			return false
		}
	}
	return true
}
