// Package schema holds what the sources state about one game version's script: each database's positions
// with their claims and vanilla's evidence, each command the dumps declare, and the one walker every reader
// of script uses. It decides whether a claim may accuse a user's file (MayAccuse); it never looks at a mod.
package schema

import (
	"cmp"
	"slices"
	"strings"
	"time"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/jomini"
)

// Source says who stated a claim; a lower value outranks a higher one, and a claim keeps its highest-ranked
// source (Set).
type Source uint8

// The sources, by rank: each outranks the ones after it.
const (
	SourceNone     Source = iota
	SourceRegistry        // the game's rows and the language rules: they classify and never accuse
	SourceDumps           // the console dumps: what the game declares about its own language
	SourceVanilla         // the install's own files: evidence, never a claim that accuses
	SourceDocs            // the prose the install ships beside its script
	SourceWiki            // the community wiki's examples, last in trust
)

// Declared reports whether a source states claims that may accuse: the dumps, the docs or the wiki.
func (source Source) Declared() bool {
	return source == SourceDumps || source == SourceDocs || source == SourceWiki
}

// Claim is a value and the source that stated it; Source is SourceNone while the claim is empty.
type Claim[T any] struct {
	Value  T
	Source Source
}

// ValueType is the kind of word a position holds; ExpectsBlock is a doc's block, whose kind none names.
type ValueType uint8

// The kinds of word a position holds.
const (
	ValueUnknown ValueType = iota
	ValueNumber
	ValueBool
	ValueDate
	ValueString
	ValueReference
	ValueLoc
	ValueFile
	ValueScope
	ValueBlock
)

// Expected is what a source says a position holds.
type Expected struct {
	Type     ValueType
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
	Level     Level
	Condition string
}

// Writes is the set of forms vanilla writes at a position, as bits.
type Writes uint16

// The forms a written value or block takes.
const (
	WritesNumber Writes = 1 << iota
	WritesBool
	WritesDate
	WritesIdent
	WritesString
	WritesBlock
	WritesCommand // a block that holds a trigger or an effect
	WritesTrigger // a trigger written in the block
	WritesEffect  // an effect written in the block
	WritesColor   // a tagged color block: rgb { }, hsv { }
)

// writesOf returns the form a scalar is written in.
func writesOf(scalar *jomini.Scalar) Writes {
	if scalar.Quoted {
		return WritesString
	}
	switch jomini.Literal(scalar.Text) {
	case jomini.LitNumber:
		return WritesNumber
	case jomini.LitDate:
		return WritesDate
	}
	if slices.Contains(game.Bools, scalar.Text) {
		return WritesBool
	}
	return WritesIdent
}

// Vanilla is what vanilla writes at a position; it is never a claim.
type Vanilla struct {
	Count     int            // how many of the parent's written blocks hold this key
	Blocks    int            // how many times this position was written as a block
	NotColors int            // how many values written here are no color: neither a block of a color's numbers nor a word
	Writes    Writes         // the forms written here
	Values    map[string]int // words written, with counts
}

// WritesColors reports whether vanilla writes nothing but colors at the position, one of them tagged, so that an
// untagged block of numbers there is a color too: a named color's entry, a coat of arms' color1, written as numbers
// or as words (countColor).
func (vanilla Vanilla) WritesColors() bool {
	return vanilla.Writes&WritesColor != 0 && vanilla.NotColors == 0
}

// Claims is what the sources state about a position, each claim with its source.
type Claims struct {
	Value    Claim[Expected]
	Holds    Claim[game.Shape]
	Scope    Claim[string]
	Required Claim[Requirement]
	Default  Claim[string]
	Blurb    Claim[string]
}

// Position is one place in a database's entry tree or a command's arguments: its claims, each with its
// source, and vanilla's evidence.
type Position struct {
	Children map[string]*Position // below it, by written name or form
	Claims
	DeclaredBy Source // the highest-ranked declared source that writes the position: the dumps anywhere, the docs and wiki only where vanilla writes it too
	Vanilla    Vanilla
}

// declare writes what a source states about the position: each claim by Claim.Set, where its source outranks
// the one here, and the position as declared by the source where it outranks the one that has, claims or none
// (name = lists an argument and says nothing of it).
func (position *Position) declare(claims Claims, source Source) {
	position.Value.Set(claims.Value.Value, claims.Value.Source)
	position.Holds.Set(claims.Holds.Value, claims.Holds.Source)
	position.Scope.Set(claims.Scope.Value, claims.Scope.Source)
	position.Required.Set(claims.Required.Value, claims.Required.Source)
	position.Default.Set(claims.Default.Value, claims.Default.Source)
	position.Blurb.Set(claims.Blurb.Value, claims.Blurb.Source)
	if position.DeclaredBy == SourceNone || source < position.DeclaredBy {
		position.DeclaredBy = source
	}
}

// Command is an effect, trigger or link the dumps declare.
type Command struct {
	Kinds  game.Kinds
	Scopes []string  // where it runs; empty: any
	Target string    // where it leads; "" none
	Args   *Position // its usage as positions
	Blurb  string
	Global bool // a link the dumps call global: it reads the same from any scope
	Wild   bool // a link the dumps call Wild Card: a pattern, never read from a written name
	Lists  bool // it iterates a list and has no target: its block's scope is unknown
}

// Database is one script folder's entries: the entry's position tree and the folder's registry row.
type Database struct {
	Name       string
	Entry      *Position
	Descriptor game.Descriptor // how its entries are written, named and used
	Fields     map[string]bool // every key name the docs and wiki write in its entries
}

// SourceState says where a source's data came from and whether it is out of date.
type SourceState struct {
	Origin    string    // "yours", "install", "shipped" or "none"
	WrittenAt time.Time // when the source was written, by its oldest file; zero when unknown
	Stale     bool
}

// ScopeType is one scope type the dumps declare; each flag is false only where the dump says no.
type ScopeType struct {
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
	Scopes          map[string]ScopeType
	Saved           map[string]bool // scope names the game saves from code
	Modifiers       map[string]Modifier
	OnActions       map[string]OnAction
	DataTypes       map[string]DataType
	CustomLoc       map[string]CustomLoc
	Prefixes        map[string]string        // a link prefix → the database the names written after it are in; "" none
	ScriptedEntries map[string]ScriptedEntry // vanilla's scripted entries by calling word and name (word:name)
	WidgetTypes     map[string]bool          // the built-in widget types: untyped bases, self-based types, game.UndeclaredTypes
	Lists           string                   // the database of scripted lists (a row of PurposeLists); "" none
	Functions       string                   // the database of data functions' macros (a row of PurposeFunctions); "" none
	Events          string                   // the database of events (a row of PurposeEvents); "" none
}

// Link returns the declared link a lower-case key starts with: the first link of a chain (root.liege), by
// name or by the prefix before ':' (c:FRA, scope:x), never a Wild Card link; nil for none.
func (schema *Schema) Link(key string) *Command {
	first, _, _ := strings.Cut(key, ".")
	prefix, _, _ := strings.Cut(first, ":")
	for _, name := range []string{first, prefix} {
		if command := schema.Commands[name]; command != nil && command.Kinds&game.KindLink != 0 && !command.Wild {
			return command
		}
	}
	return nil
}

// CallLeadsTo is the data type a data call's steps lead to: from the functions of the context type, then the global
// ones, or a data type written first, through each function's return type; "" where a step is unknown.
func (schema *Schema) CallLeadsTo(steps []jomini.CallStep, context string) string {
	typeName := cmp.Or(context, game.GlobalType)
	for i, step := range steps {
		_, function, isFunction := schema.functionOf(typeName, i == 0, step.Name)
		_, isType := schema.DataTypes[step.Name]
		switch {
		case isFunction:
			typeName = function.Returns
		case i == 0 && isType:
			typeName = step.Name
		default:
			return ""
		}
	}
	return typeName
}

// CallStep is the type the step at index i of a data call is called on and the function it names there; false where
// the steps before it lead nowhere known or it names no function of that type (a data type written first is such a
// step, and the type the next one is called on).
func (schema *Schema) CallStep(steps []jomini.CallStep, i int, context string) (owner string, function DataFunction, ok bool) {
	owner = schema.CallLeadsTo(steps[:i], context)
	if owner == "" {
		return "", DataFunction{}, false
	}
	return schema.functionOf(owner, i == 0, steps[i].Name)
}

// functionOf is the function a name is on a type, and the type it is on: the type's own, else where it is a call's
// first step the global functions'.
func (schema *Schema) functionOf(owner string, first bool, name string) (string, DataFunction, bool) {
	if function, ok := schema.DataTypes[owner].Functions[name]; ok {
		return owner, function, true
	}
	if function, ok := schema.DataTypes[game.GlobalType].Functions[name]; ok && first {
		return game.GlobalType, function, true
	}
	return owner, DataFunction{}, false
}

// DatabaseNamed returns the database a word names: the one called by the word or its plural, else the one
// whose last path word is either; "" for none or several.
func (schema *Schema) DatabaseNamed(word string) string {
	var named []string
	for _, name := range []string{word, word + "s"} {
		if database := schema.Databases[name]; database != nil && database.Descriptor.Purpose != game.PurposeHistory {
			named = append(named, name)
		}
	}
	exact := len(named) > 0
	for name, database := range schema.Databases {
		last := name[strings.LastIndexByte(name, '/')+1:]
		if !exact && database.Descriptor.Purpose != game.PurposeHistory && (last == word || last == word+"s") {
			named = append(named, name)
		}
	}
	if len(named) == 1 {
		return named[0]
	}
	return ""
}

// DatabasesWhere returns the names of the databases whose descriptor passes a test, in name order.
func (schema *Schema) DatabasesWhere(test func(game.Descriptor) bool) (names []string) {
	for name, database := range schema.Databases {
		if test(database.Descriptor) {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}

// Child returns the position under a written name, or the one every name reads at where the position has
// <any>; nil when there is none.
func (position *Position) Child(name string) *Position {
	if position == nil {
		return nil
	}
	if child := position.Children[positionName(name)]; child != nil {
		return child
	}
	return position.Children[game.AnyKey]
}

// EnsureChild returns the position under a written name, making it when it is missing. The name is cloned
// so a position never pins a file's text.
func (position *Position) EnsureChild(name string) *Position {
	if child := position.Child(name); child != nil {
		return child
	}
	if position.Children == nil {
		position.Children = map[string]*Position{}
	}
	child := &Position{}
	position.Children[strings.Clone(positionName(name))] = child
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

// Set writes a claim when from outranks the source it has: any source outranks an empty claim's none, a lower
// rank a higher one, and of two equal the first stands; so the order the sources are applied in is irrelevant.
func (claim *Claim[T]) Set(value T, source Source) {
	if source != SourceNone && (claim.Source == SourceNone || source < claim.Source) {
		claim.Value, claim.Source = value, source
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
func (schema *Schema) MayAccuse(check Check, position, parent *Position) bool {
	source := SourceDumps
	switch check {
	case CheckValue:
		source = position.Value.Source
	case CheckRequired:
		source = position.Required.Source
	}
	state, found := schema.Sources[source]
	if !found || state.Stale {
		return false
	}
	listed := check == CheckValue && len(position.Value.Value.Choices) > 0
	if (source == SourceDocs || source == SourceWiki || listed) && position.Vanilla.Count == 0 {
		return false
	}
	switch check {
	case CheckValue:
		return valueAgrees(position)
	case CheckRequired:
		return parent != nil && position.Required.Value.Level == LevelAlways &&
			position.Vanilla.Count >= parent.Vanilla.Blocks
	}
	return true
}

// valueAgrees reports whether vanilla's evidence at a position fits the value claimed there: every form it
// writes is one the type allows, and every word it writes is in the declared list.
func valueAgrees(position *Position) bool {
	expected := position.Value.Value
	var allowed Writes
	switch expected.Type {
	case ValueNumber:
		allowed = WritesNumber
	case ValueBool:
		allowed = WritesBool
	case ValueDate:
		allowed = WritesDate
	case ValueString, ValueReference, ValueLoc, ValueFile, ValueScope:
		allowed = WritesIdent | WritesString
	default:
		if len(expected.Choices) == 0 {
			return false
		}
		allowed = WritesIdent | WritesString
	}
	// the command forms mark the block, not a value
	if position.Vanilla.Writes&^(allowed|WritesCommand|WritesTrigger|WritesEffect) != 0 {
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
