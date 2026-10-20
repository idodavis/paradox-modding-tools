// Package schema holds what the sources state about one game version's script: each database's positions
// with their claims and vanilla's evidence, each command the dumps declare, and the one walker every reader
// of script uses. It decides whether a claim may accuse a user's file (MayAccuse); it never looks at a mod.
package schema

import (
	"cmp"
	"maps"
	"slices"
	"strings"
	"time"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/jomini"
)

// Claim is a value and the source that stated it.
type Claim[T any] struct {
	Value  T           // the value; zero while the claim is empty
	Source game.Source // game.SourceNone while the claim is empty
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
	Type     ValueType // the kind of value
	Database string    // ValueReference: the database the word names
	Choices  []string  // a declared word list, sorted; empty when none
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

// Requirement is whether a position is required.
type Requirement struct {
	Level     Level  // unknown, optional, always or conditional
	Condition string // the condition's text for LevelConditional
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
	Count     int            // how many of the parent's blocks hold this key
	Blocks    int            // how many times it was written as a block
	NotColors int            // how many values written here are no color
	Writes    Writes         // the forms written here
	Values    map[string]int // the words written, with counts
}

// WritesColors reports whether vanilla writes nothing but colors at the position, one of them tagged, so an untagged
// block of numbers there is a color too (a named color's entry, a coat of arms' color1).
func (vanilla Vanilla) WritesColors() bool {
	return vanilla.Writes&WritesColor != 0 && vanilla.NotColors == 0
}

// Claims is what the sources state about a position.
type Claims struct {
	Value    Claim[Expected]    // what its value is
	Shape    Claim[game.Shape]  // the shape its block holds
	Scope    Claim[string]      // the scope its block runs in
	Required Claim[Requirement] // whether it must be written
	Default  Claim[string]      // the value it takes when unwritten
	Blurb    Claim[string]      // what it is, in a sentence
}

// Position is one place in a database's entry tree or a command's arguments.
type Position struct {
	Children map[string]*Position // below it, by written name or form
	Claims                        // what the sources state about it
	Source   game.Source          // the highest-ranked declared source that writes it; SourceNone for none
	Vanilla  Vanilla              // what vanilla writes there
}

// declare writes what a source states about the position, claim by claim where it outranks the source there; a source
// that lists the position and says nothing of it is still recorded as declaring it.
func (position *Position) declare(claims Claims, source game.Source) {
	position.Value.Set(claims.Value.Value, claims.Value.Source)
	position.Shape.Set(claims.Shape.Value, claims.Shape.Source)
	position.Scope.Set(claims.Scope.Value, claims.Scope.Source)
	position.Required.Set(claims.Required.Value, claims.Required.Source)
	position.Default.Set(claims.Default.Value, claims.Default.Source)
	position.Blurb.Set(claims.Blurb.Value, claims.Blurb.Source)
	if position.Source == game.SourceNone || source < position.Source {
		position.Source = source
	}
}

// Command is an effect, trigger or link the dumps declare.
type Command struct {
	Declared []string  // what declares it (game.Declared*), in dump order
	Scopes   []string  // the scopes it runs in; empty any
	Target   string    // the scope it leads to; "" none
	Args     *Position // its usage as positions
	Blurb    string    // what it does, in a sentence
	Global   bool      // a link that reads the same from any scope
	Wild     bool      // a link that is a pattern, never read from a written name
	Lists    bool      // it iterates a list and has no target: its block's scope is unknown
}

// isDeclaredOnly reports whether the dumps declare a command as one thing alone: a trigger that is never an effect.
func isDeclaredOnly(command *Command, word string) bool {
	return len(command.Declared) == 1 && command.Declared[0] == word
}

// Database is one script folder's entries.
type Database struct {
	Name   string
	Entry  *Position         // the tree of what an entry holds
	Rule   game.DatabaseRule // how its entries are written, named and used
	Fields map[string]bool   // every key the docs and wiki write in its entries
}

// SourceState says where a source's data came from and whether it is out of date.
type SourceState struct {
	Origin    string    // "yours", "install", "shipped" or "none"
	WrittenAt time.Time // when its oldest file was written; zero when unknown
	Stale     bool      // older than the install
}

// ScopeType is one scope type the dumps declare; each flag is false only where the dump says no.
type ScopeType struct {
	Name      string
	Variables bool // it stores variables
	Triggers  bool // it evaluates triggers
	Effects   bool // it executes effects
}

// ScriptedEntry is a scripted entry as a call reads it.
type ScriptedEntry struct {
	Database   string
	Parameters *Position // its own parameters; nil where the database takes none, and the call reads the database's entry
}

// Schema is everything the sources state about one game version.
type Schema struct {
	Game            string                      // the game's id
	Version         string                      // the game version it describes
	Sources         map[game.Source]SourceState // where each source's data came from
	Databases       map[string]*Database        // by name
	Callees         map[string][]string         // a caller word → the databases a call by it resolves in, by name
	Commands        map[string]*Command         // effects, triggers and links, by name
	Scopes          map[string]ScopeType        // by scope name
	Saved           map[string]bool             // scope names the game saves from code
	Modifiers       map[string]Modifier         // by name
	OnActions       map[string]OnAction         // by name
	DataTypes       map[string]DataType         // by name
	CustomLoc       map[string]CustomLoc        // by name
	Prefixes        map[string]string           // a link prefix → the database its names are in; "" none
	ScriptedEntries map[string]ScriptedEntry    // vanilla's, by calling word and name (word:name)
	WidgetTypes     map[string]bool             // the built-in widget types
	Lists           string                      // the database of scripted lists; "" none
	Functions       string                      // the database of data functions' macros; "" none
	Events          string                      // the database of events; "" none
	Concepts        string                      // the database of game concepts; "" none
}

// Link returns the declared link a lower-case key starts with: the first link of a chain (root.liege), by
// name or by the prefix before ':' (c:FRA, scope:x), never a Wild Card link; nil for none.
func (schema *Schema) Link(key string) *Command {
	first, _, _ := strings.Cut(key, ".")
	prefix, _, _ := strings.Cut(first, ":")
	for _, name := range []string{first, prefix} {
		if command := schema.Commands[name]; command != nil && slices.Contains(command.Declared, game.DeclaredLink) && !command.Wild {
			return command
		}
	}
	return nil
}

// CallLeadsTo returns the data type a data call's steps lead to, through each function's return type, starting on the
// context type's functions, then the global ones; "" where a step is unknown.
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

// CallStep returns the type step i of a data call is called on and the function it names there; false where the steps
// before it lead nowhere known or it names no function of that type.
func (schema *Schema) CallStep(steps []jomini.CallStep, i int, context string) (owner string, function DataFunction, ok bool) {
	owner = schema.CallLeadsTo(steps[:i], context)
	if owner == "" {
		return "", DataFunction{}, false
	}
	return schema.functionOf(owner, i == 0, steps[i].Name)
}

// functionOf returns the function a name is on a type and the type it is on: the type's own, else (as a call's first
// step) the global functions'.
func (schema *Schema) functionOf(owner string, first bool, name string) (string, DataFunction, bool) {
	if function, ok := schema.DataTypes[owner].Functions[name]; ok {
		return owner, function, true
	}
	if function, ok := schema.DataTypes[game.GlobalType].Functions[name]; ok && first {
		return game.GlobalType, function, true
	}
	return owner, DataFunction{}, false
}

// ResolveNamedDatabase returns the database a word names: the one called by the word or its plural, else the one
// whose last path word is either; "" for none or several.
func (schema *Schema) ResolveNamedDatabase(word string) string {
	var named []string
	forms := []string{word, word + "s"}
	if stem, endsInY := strings.CutSuffix(word, "y"); endsInY {
		forms = append(forms, stem+"ies")
	}
	for _, name := range forms {
		if database := schema.Databases[name]; database != nil && database.Rule.Houses != "history" {
			named = append(named, name)
		}
	}
	exact := len(named) > 0
	for name, database := range schema.Databases {
		last := name[strings.LastIndexByte(name, '/')+1:]
		if !exact && database.Rule.Houses != "history" && slices.Contains(forms, last) {
			named = append(named, name)
		}
	}
	if len(named) == 1 {
		return named[0]
	}
	return ""
}

// ResolveTypeDatabase returns the database a data type's entries are in: the concepts for the global type, else that
// of the first link prefix whose link leads to the type's scope; "" for none.
func (schema *Schema) ResolveTypeDatabase(dataType string) string {
	switch dataType {
	case "":
		return ""
	case game.GlobalType:
		return schema.Concepts
	}
	scope := SnakeCase(dataType)
	for _, prefix := range slices.Sorted(maps.Keys(schema.Prefixes)) {
		if link := schema.Commands[prefix]; link != nil && link.Target == scope {
			return schema.Prefixes[prefix]
		}
	}
	return ""
}

// DatabasesWhere returns the names of the databases whose rule passes a test, in name order.
func (schema *Schema) DatabasesWhere(test func(game.DatabaseRule) bool) (names []string) {
	for name, database := range schema.Databases {
		if test(database.Rule) {
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
func (claim *Claim[T]) Set(value T, source game.Source) {
	if source != game.SourceNone && (claim.Source == game.SourceNone || source < claim.Source) {
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

// MayAccuse reports whether a claim may accuse a user's code: a fresh declared source states it and vanilla's evidence
// does not contradict it; docs, wiki and word-list claims accuse only where vanilla writes the position.
func (schema *Schema) MayAccuse(check Check, position, parent *Position) bool {
	source := game.SourceDumps
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
	if (source == game.SourceDocs || source == game.SourceWiki || listed) && position.Vanilla.Count == 0 {
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
