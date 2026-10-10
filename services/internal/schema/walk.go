// walk.go is the walker: WalkFile places every statement of a file at its position in the schema's tree,
// WalkBlock a snippet's; each key is placed by PlaceKey.

package schema

import (
	"cmp"
	"path"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/inventory"
	"paradox-modding-tools/services/internal/jomini"
)

// BlockContext is where statements are read.
type BlockContext struct {
	Position  *Position       // the position the block reads at; nil when none is known
	Shape     game.Shape      // what the block holds
	Calls     string          // the CalledIn word a call in the block resolves by
	Scope     string          // the scope the block runs in; "" unknown
	InTrigger bool            // inside a trigger
	InEntry   bool            // inside a database entry
	Command   string          // the command whose own block this is, lower-case
	Call      string          // the scripted entry this block calls, whose parameters its keys pass
	Bound     *Position       // the position a dynamic description in the block belongs to
	Dated     bool            // an entry's own block whose number keys are dates (Folder.Dated)
	Fields    map[string]bool // in an entry's own block: the key names its database's docs write
}

// Placement is one statement as the walker reads it.
type Placement struct {
	Statement    jomini.Statement
	Holder       *jomini.Block // the block holding it; nil at a file's top
	Name         string        // the key name read: the key, or one of a docs key's alternatives
	Rule         game.KeyRule  // the rule its key was read by
	Position     *Position     // its position; nil when none
	Command      *Command      // the declared command or link its key is
	Calls        string        // set when its key is a call: the CalledIn word it resolves by
	Listed       string        // set when its key iterates a scripted list: the iterator prefix
	Leaf         bool          // the leaf of a dynamic description
	Entry        int           // the index, in the entries WalkFile was given, of the entry it is in; -1 outside one
	Occurrences  []Occurrence  // what its key and value name, in the order the rules fit (occurrences.go)
	StoredIn     string        // the database a name command's value names here (set_variable: var); "" none
	BlockContext               // where it is written
	Inner        BlockContext  // where its own block is read
}

// Visitor is one use of the walker. Create makes the positions it reads (vanilla); Visit sees each reading, may
// change where its block is read, and returns whether to read that block.
type Visitor struct {
	Create bool
	Visit  func(*Placement) bool
}

// meaning is what a key is: the rule that read it, its position, its declared command or link, the calling
// word a call resolves by, and whether it is a description's leaf.
type meaning struct {
	rule     game.KeyRule
	position *Position
	command  *Command
	calls    string
	leaf     bool
}

// fileWalk is one walk of a file: the entries known, which block belongs to which entry, and what the name rules
// need of the file (its extension, the events database, the parameters defined so far).
type fileWalk struct {
	schema  *Schema
	visitor Visitor
	entries []Entry
	own     map[jomini.Statement]int // an entry's statement: its index in entries
	inside  map[*jomini.Block]int    // a block: the entry it is in, -1 none
	ext     string                   // the file's extension, lower-case, with its dot; "" for a snippet
	events  string                   // the database an event id names; "" none
	defined map[string]bool          // the parameters defined so far, as <entry>:<NAME>
}

// WalkFile reads a whole file with its entries known: every statement in source order, outer before inner,
// each reading carrying the entry it is in and what its words name. A whole-file entry (a history file) holds the
// root's statements as one block. A table or list (parsed nil) has no statements to read: its entries are its
// records, and the visitor sees nothing.
func (schema *Schema) WalkFile(file inventory.File, parsed *jomini.Result, entries []Entry, visitor Visitor) {
	if parsed == nil {
		return
	}
	walk := &fileWalk{
		schema: schema, visitor: visitor, entries: entries,
		own: map[jomini.Statement]int{}, inside: map[*jomini.Block]int{},
		ext:     strings.ToLower(path.Ext(file.Rel)),
		events:  cmp.Or(schema.DatabasesWhere(func(row game.Folder) bool { return row.Events })...),
		defined: map[string]bool{},
	}
	top := parsed.Root.Statements
	for i, entry := range entries {
		walk.own[entry.Statement] = i
		if entry.Statement.NodeRange() == parsed.Root.Range {
			top = []jomini.Statement{entry.Statement}
		}
	}
	walk.block(nil, top, BlockContext{})
}

// WalkBlock reads a snippet's statements at a context, with no entries and no names: the docs and the dumps'
// usages read script this way.
func (schema *Schema) WalkBlock(visitor Visitor, holder *jomini.Block, statements []jomini.Statement, in BlockContext) {
	walk := &fileWalk{schema: schema, visitor: visitor, own: map[jomini.Statement]int{}, inside: map[*jomini.Block]int{}}
	walk.block(holder, statements, in)
}

// block reads a block's statements at a context; holder is nil at a file's top. A statement that starts an entry
// is read at the entry's position and its block at the entry's context; inside an entry each key is read by
// PlaceKey; anything else stays at the context's position.
func (walk *fileWalk) block(holder *jomini.Block, statements []jomini.Statement, in BlockContext) {
	for index, statement := range statements {
		entry, inEntry := walk.inside[holder]
		if !inEntry {
			entry = -1
		}
		var database *Database
		if i, isEntry := walk.own[statement]; isEntry {
			if database = walk.schema.Databases[walk.entries[i].Database]; database != nil {
				entry = i
			}
		}
		_, isAssign := statement.(*jomini.Assignment)
		var placements []Placement
		switch {
		case database != nil:
			placements = []Placement{walk.schema.entryPlacement(statement, database, in)}
		case isAssign && in.InEntry:
			placements = []Placement{walk.schema.keyPlacement(walk.visitor, statements, index, in)}
		default:
			placements = []Placement{{Statement: statement, Position: in.Position, BlockContext: in, Inner: in}}
		}
		block := jomini.ChildBlock(statement)
		for _, placement := range placements {
			placement.Holder, placement.Entry = holder, entry
			if walk.ext != "" {
				walk.occurrencesOf(&placement)
			}
			if walk.visitor.Visit(&placement) && block != nil {
				walk.inside[block] = entry
				walk.block(block, block.Statements, placement.Inner)
			}
		}
	}
}

// entryPlacement reads a statement that starts an entry of a database. The dumps declare the scope of an
// on_action's block.
func (schema *Schema) entryPlacement(statement jomini.Statement, database *Database, in BlockContext) Placement {
	inner := schema.EntryContext(database)
	if assign, isAssign := statement.(*jomini.Assignment); isAssign && database.Row.OnActions {
		if scope := schema.OnActions[assign.Key.Text].Scope; scope != "" {
			inner.Scope = scope
		}
	}
	return Placement{Statement: statement, Position: database.Entry, BlockContext: in, Inner: inner}
}

// keyPlacement reads the key of the assignment at statements[index]. A calling word written before a name calls
// it by that word: blockoverride "x" { }.
func (schema *Schema) keyPlacement(visitor Visitor, statements []jomini.Statement, index int, in BlockContext) Placement {
	assign := statements[index].(*jomini.Assignment)
	if index > 0 {
		if listed, isList := statements[index-1].(*jomini.ValueStmt); isList {
			if scalar, isScalar := listed.Value.(*jomini.Scalar); isScalar && len(schema.Callees[scalar.Text]) > 0 {
				in.Calls = scalar.Text
			}
		}
	}
	return schema.PlaceKey(in, assign, assign.Key.Text, visitor.Create)
}

// EntryContext returns where a database's entry is read: at its entry position, in the kind of block its
// entry holds, in the scope its entry declares when the dumps say that scope runs triggers or effects.
func (schema *Schema) EntryContext(database *Database) BlockContext {
	kind := database.Entry.Holds.Value
	scope := database.Entry.Scope.Value
	if state := schema.Scopes[scope]; !state.Triggers && !state.Effects {
		scope = ""
	}
	return BlockContext{
		Position: database.Entry, Shape: kind, Calls: game.Shapes[kind].Calls, Scope: scope,
		InTrigger: kind == game.ShapeTriggers, InEntry: true, Dated: database.Row.Dated, Fields: database.Fields,
	}
}

// PlaceKey reads one key written at a context: what it is, where its block is read, and the scope that block
// runs in. With create, the key's position is made.
func (schema *Schema) PlaceKey(in BlockContext, statement jomini.Statement, name string, create bool) Placement {
	placement := Placement{Statement: statement, Name: name, BlockContext: in}
	create = create && in.Position != nil
	var value jomini.Value
	if assign, isAssign := statement.(*jomini.Assignment); isAssign {
		value = assign.Value
	}

	// a date key re-opens the block it stands in (history)
	literal := jomini.Literal(name)
	if literal == jomini.LitDate || in.Dated && literal == jomini.LitNumber {
		placement.Inner = in
		return placement
	}
	// a key holding a parameter or written as a data call is a scope written in place: no position, no call,
	// scope unknown
	if schema.Commands[strings.ToLower(name)] == nil && writtenInPlace(name) {
		placement.Inner = in
		placement.Inner.Scope, placement.Inner.Command, placement.Inner.Call = "", "", ""
		return placement
	}

	m := schema.keyMeaning(in, name, in.Position.Child(name), create)
	placement.Rule, placement.Position, placement.Command, placement.Calls, placement.Leaf = m.rule, m.position, m.command, m.calls, m.leaf
	placement.Listed = schema.listIterated(in, placement, name, value)
	placement.Inner = innerContext(in, placement, name, value)

	// a trigger is exactly a trigger, not one that is also an effect
	placement.InTrigger = in.InTrigger || isCommand(placement) && placement.Command.Kinds == game.KindTrigger
	placement.Inner.InTrigger = placement.InTrigger || placement.Inner.Shape == game.ShapeTriggers
	placement.Inner.Scope = schema.blockScope(in, placement, name)
	return placement
}

// writtenInPlace reports whether a key holds a parameter ($ACTOR$, $TARGET$.liege) or is a data call
// written with arguments (name(…)).
func writtenInPlace(name string) bool {
	if len(jomini.Interpolations(name)) > 0 {
		return true
	}
	return strings.Contains(name, "(") && slices.ContainsFunc(jomini.ParseCall(name, 0), func(part jomini.Step) bool {
		return part.Range.End-part.Range.Start > len(part.Name)
	})
}

// isLink reports whether a reading's key is a declared link.
func isLink(placement Placement) bool {
	return placement.Command != nil && placement.Command.Kinds&game.KindLink != 0
}

// isCommand reports whether a reading's key is a declared effect or trigger.
func isCommand(placement Placement) bool { return placement.Command != nil && !isLink(placement) }

// keyMeaning decides what a key is at a context by the block's rules in order; the first whose test holds
// wins. In a description block the shape of a dynamic description decides instead.
func (schema *Schema) keyMeaning(in BlockContext, name string, position *Position, create bool) meaning {
	made := func() *Position {
		if create {
			return in.Position.EnsureChild(name)
		}
		return position
	}
	if in.Shape == game.ShapeDescription {
		return descriptionMeaning(in, name, made, create)
	}

	lower := strings.ToLower(name)
	command := schema.Commands[lower]
	entry, called := schema.ScriptedEntries[in.Calls+":"+name]
	k := keyFacts{
		in: in, name: name, position: position, made: made, create: create,
		entry: entry, known: called && command == nil, command: command, link: schema.Link(lower),
	}
	if command != nil && command.Kinds&game.KindLink != 0 {
		k.command = nil // a link is read by Link
	}
	_, k.isOperator = game.ValueOperators[lower]

	var found meaning
	for _, rule := range game.Shapes[in.Shape].Keys {
		var fits bool
		if found, fits = schema.fits(rule, k); fits {
			return found
		}
	}
	return found
}

// keyFacts is what the rules of a block are tried against for one key.
type keyFacts struct {
	in         BlockContext
	name       string
	position   *Position // the position under the written name; nil when there is none
	made       func() *Position
	create     bool
	entry      ScriptedEntry // the scripted entry the block's calling word calls by this name
	known      bool          // there is one, and no declared command has the name
	command    *Command      // a declared effect or trigger, else nil
	link       *Command      // the declared link the key starts with
	isOperator bool
}

// fits tries one rule against a key and returns what the key is by it; fits is false when the rule does not
// hold. The meaning keeps the rule either way, so a block with no fitting rule reads by its last.
func (schema *Schema) fits(rule game.KeyRule, facts keyFacts) (found meaning, fits bool) {
	found = meaning{rule: rule}
	switch {
	case rule == game.KeyRuleKnownCall && facts.known:
		found.position, found.calls = facts.entry.Parameters, facts.in.Calls
		if found.position == nil {
			found.position = schema.Databases[facts.entry.Database].Entry
		}
	case rule == game.KeyRuleDeclared && facts.position != nil && facts.position.DeclaredBy != SourceNone:
		found.position = facts.position
	case rule == game.KeyRuleDocumented && facts.in.Fields[positionName(facts.name)]:
		found.position = facts.made()
	case rule == game.KeyRuleCommand && facts.command != nil:
		found.position, found.command = facts.command.Args, facts.command
	case rule == game.KeyRuleOperator && facts.isOperator:
		found.position = facts.made()
	case rule == game.KeyRuleLink && facts.link != nil:
		found.command = facts.link
	case rule == game.KeyRuleObserved && (facts.create || facts.position != nil && facts.position.Vanilla.Count > 0):
		found.position = facts.made()
	case rule == game.KeyRulePosition:
		found.position = facts.made()
	case rule == game.KeyRulePositionOrCall:
		found.position = facts.made()
		if facts.in.Position != nil && facts.in.Position.Vanilla.Writes&WritesCommand != 0 {
			found.calls = facts.in.Calls
		}
	case rule == game.KeyRuleCall:
		found.position, found.calls = facts.made(), facts.in.Calls
	default:
		return found, false
	}
	return found, true
}

// descriptionMeaning reads a key in a description block: a marker or the trigger is a position, the leaf is
// the position the description belongs to, and any other key is nothing.
func descriptionMeaning(in BlockContext, name string, made func() *Position, create bool) meaning {
	shape := game.DynamicDescription
	found := meaning{rule: game.KeyRulePosition}
	switch {
	case slices.Contains(shape.Markers, name):
		found.position = made()
	case name == shape.Trigger:
		found.position = made()
		if create {
			found.position.Holds.Set(game.ShapeTriggers, SourceRegistry)
		}
	case name == shape.Leaf:
		found.position, found.leaf = in.Bound, true
	}
	return found
}

// listIterated returns the iterator prefix when a key iterates a scripted list: a call in a triggers,
// effects or value block that starts with an iterator prefix, takes a block and names no known scripted
// entry iterates the scripted list named by the rest (every_adult_child).
func (schema *Schema) listIterated(in BlockContext, placement Placement, name string, value jomini.Value) string {
	listing := placement.Calls != "" && placement.Rule != game.KeyRuleKnownCall && jomini.BlockOf(value) != nil
	if !listing || schema.Lists == "" || in.Calls == "" || in.Calls == game.AnyCall {
		return ""
	}
	for prefix := range game.ListIterators {
		if strings.HasPrefix(name, prefix) {
			return prefix
		}
	}
	return ""
}

// blockKindOf returns the kind of block a position holds: its declared kind, else a value block where the
// position takes a number or vanilla writes only value operators in it, else the kind of command vanilla
// writes there, else fields where vanilla writes keys and no command.
func blockKindOf(position *Position) game.Shape {
	switch {
	case position == nil:
		return game.ShapeUnknown
	case position.Holds.Source != SourceNone:
		return position.Holds.Value
	case position.Value.Value.Type == ValueNumber && position.Value.Source.Declared(), writesOnlyOperators(position):
		return game.ShapeValue
	}
	switch position.Vanilla.Writes & (WritesTrigger | WritesEffect) {
	case WritesTrigger:
		return game.ShapeTriggers // every command vanilla writes there is a trigger
	case WritesEffect:
		return game.ShapeEffects
	}
	if position.Vanilla.Writes&WritesCommand == 0 && len(position.Children) > 0 {
		return game.ShapeFields // vanilla writes keys and no command
	}
	return game.ShapeUnknown
}

// writesOnlyOperators reports whether a position has children and every one is a value operator.
func writesOnlyOperators(position *Position) bool {
	for name := range position.Children {
		if _, operator := game.ValueOperators[name]; !operator {
			return false
		}
	}
	return len(position.Children) > 0
}

// commandBlockKind returns the kind of block a command's block is, given the kind of block it is written in:
// a value block when it takes a number or is written in one, an arguments block when its usage writes
// arguments only, the enclosing kind when it is both a trigger and an effect, else its own kind.
func commandBlockKind(command *Command, in game.Shape) game.Shape {
	switch {
	case blockKindOf(command.Args) == game.ShapeValue:
		return game.ShapeValue
	case command.Args.Holds.Value == game.ShapeArguments:
		return game.ShapeArguments
	case in == game.ShapeValue:
		return game.ShapeValue
	case command.Kinds&(game.KindTrigger|game.KindEffect) == game.KindTrigger|game.KindEffect:
		return in
	case command.Kinds&game.KindTrigger != 0:
		return game.ShapeTriggers
	case command.Kinds&game.KindEffect != 0:
		return game.ShapeEffects
	}
	return game.ShapeUnknown
}

// innerContext returns where a key's own block is read, apart from its scope: what the key is decides the
// position and kind, then the structural keys (random_list's branches, switch's cases), widgets, list
// iterators and dynamic descriptions adjust them.
func innerContext(in BlockContext, placement Placement, name string, value jomini.Value) BlockContext {
	inner := BlockContext{InEntry: in.InEntry, Bound: in.Bound}
	lower := strings.ToLower(name)
	switch {
	case isLink(placement):
		inner = in // a link's block reads where the link stands
		inner.Command, inner.Call = "", ""
	case isCommand(placement):
		inner.Position, inner.Command = placement.Command.Args, lower
		inner.Shape = commandBlockKind(placement.Command, in.Shape)
	case placement.Rule == game.KeyRuleOperator && placement.Position != nil:
		inner.Position, inner.Shape = placement.Position, game.ShapeArguments
		if game.ValueOperators[lower] {
			inner.Shape = game.ShapeValue // the operator takes a number
		}
	case in.Shape == game.ShapeDescription && placement.Position != nil && name != game.DynamicDescription.Trigger:
		inner.Position, inner.Shape = placement.Position, game.ShapeDescription
	default:
		inner.Position, inner.Shape = placement.Position, blockKindOf(placement.Position)
	}

	for _, row := range game.Structurals {
		if row.Command == in.Command && (row.Key == "" || row.Key == positionName(name)) {
			inner.Shape, inner.Calls = row.Holds, row.Calls
			if row.Enclosing {
				inner.Shape = in.Shape
			}
		}
	}
	if in.Shape == game.ShapeWidget {
		inner.Shape = game.ShapeWidget // a widget's blocks are widgets again
	}
	if placement.Listed != "" {
		inner.Shape = game.ListIterators[placement.Listed]
	}
	if inner.Calls == "" {
		inner.Calls = game.Shapes[inner.Shape].Calls
	}
	if inner.Shape != game.ShapeDescription && placement.Position != nil && isDynamicDescription(value) {
		inner.Shape, inner.Bound, inner.Calls = game.ShapeDescription, placement.Position, ""
	}
	if placement.Calls != "" {
		inner.Call = name // the scripted entry this block calls, whose parameters its keys pass
	}
	return inner
}

// blockScope returns the scope a key's block runs in. Order matters: a scope the position declares beats
// everything; a link or an iterating command leads to its target; a list or a bare target gives no scope; a
// value or description block, a command's block and a block a source gives a kind stay where they are
// written; anything else is unknown. A scope the dumps say runs no triggers and no effects is unknown too.
func (schema *Schema) blockScope(in BlockContext, placement Placement, name string) string {
	target, iterates := "", false
	switch {
	case isLink(placement):
		last := strings.ToLower(name[strings.LastIndexByte(name, '.')+1:]) // a chain leads where its last link does
		if link := schema.Link(last); link != nil {
			target = link.Target
		}
	case isCommand(placement):
		target = placement.Command.Target
		iterates = placement.Command.Args.Holds.Value == game.ShapeTriggers || placement.Command.Args.Holds.Value == game.ShapeEffects
	}
	_, leads := schema.Scopes[target]
	kind := placement.Inner.Shape

	declared := placement.Position != nil && placement.Position.Scope.Source != SourceNone
	followsTarget := isLink(placement) || isCommand(placement) && iterates && leads && !placement.Command.Lists
	walksList := placement.Listed != "" || isCommand(placement) && placement.Command.Lists
	bareTarget := isCommand(placement) && placement.Command.Target != "" && !iterates
	staysPut := kind == game.ShapeValue || kind == game.ShapeDescription || isCommand(placement) ||
		placement.Position != nil && (placement.Position.Holds.Source != SourceNone || kind != game.ShapeUnknown)

	scope := ""
	switch {
	case declared:
		scope = placement.Position.Scope.Value
	case followsTarget && leads:
		scope = target
	case followsTarget, walksList, bareTarget:
		scope = ""
	case staysPut:
		scope = in.Scope
	}
	if state := schema.Scopes[scope]; !state.Triggers && !state.Effects {
		scope = ""
	}
	return scope
}

// isDynamicDescription reports whether a value is a dynamic description: a block that holds a marker whose
// own block holds the leaf or another marker (title = { first_valid = { triggered_desc = { desc = … } } }).
func isDynamicDescription(value jomini.Value) bool {
	shape := game.DynamicDescription
	inside := append([]string{shape.Leaf}, shape.Markers...)
	return slices.ContainsFunc(valuesWritten(value, shape.Markers), func(marker jomini.Value) bool {
		return len(valuesWritten(marker, inside)) > 0
	})
}

// valuesWritten returns the values of a block's assignments whose key is one of names.
func valuesWritten(value jomini.Value, names []string) (values []jomini.Value) {
	block := jomini.BlockOf(value)
	if block == nil {
		return nil
	}
	for _, statement := range block.Statements {
		if assign, isAssign := statement.(*jomini.Assignment); isAssign && slices.Contains(names, assign.Key.Text) {
			values = append(values, assign.Value)
		}
	}
	return values
}
