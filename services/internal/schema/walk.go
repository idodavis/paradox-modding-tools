// walk.go is the walker: WalkFile reads every statement of a file in the context it is written in and the one
// its block reads in, WalkBlock a snippet's; Enter is the one step from the first to the second.

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

// Context is where statements are read: the state a walk inherits at a block, and for the block a statement opens,
// how that statement was read.
type Context struct {
	At        *Position     // the position the block reads at; nil when none is known
	Shape     game.Shape    // what the block holds
	Scope     string        // the scope the block runs in; "" unknown
	Caller    string        // the word a call in the block resolves by
	Call      string        // the scripted entry this block calls, whose parameters its keys pass; "" none
	Command   string        // the command whose own block this is, lower-case; "" none
	List      string        // the scripted list this block iterates, named by the rest of its key; "" none
	Rule      game.KeyRule  // the rule the key that opened this block was read by
	Database  *Database     // the database of the entry the block is in; nil outside an entry
	Entry     int32         // the entry the block is in, as its ordinal in the file's entries; -1 none
	Bound     *Position     // the position a description's leaf reads at; nil outside a description
	Block     *jomini.Block // the block whose statements read here; nil at a file's top and for a snippet
	InTrigger bool          // inside a trigger
	Leaf      bool          // the statement that opened this context is a description's leaf
}

// Position is the own position of the key that opened the context: nil for a link, which reads where it stands,
// for a statement that reopened its holder, and for one with no key.
func (context Context) Position() *Position {
	if context.Rule == game.KeyRuleLink || context.Rule == game.KeyRuleNone {
		return nil
	}
	return context.At
}

// KeyFacts is what the ten fit tests are tried against: one key at a context.
type KeyFacts struct {
	Context Context
	Key     string
	Block   *jomini.Block // the key's own block; nil for a scalar value
	Child   *Position     // the position under the holder by the key's written form; nil when there is none
	Command *Command      // the declared effect or trigger of that name; nil for none or for a link
	Link    *Command      // the declared link the key starts with; nil for none
	Callee  ScriptedEntry // the scripted entry the holder's caller calls by the key
	Known   bool          // there is one, and no declared command has the name
	Create  bool          // the walk makes the positions it reads
}

// Visit is what a walk hands each statement: the statement, the context it is written in, the one its block
// reads in (which the visitor may change) and what its words name. False stops that block.
type Visit func(statement jomini.Statement, context Context, inner *Context, occurrences []Occurrence) bool

// fileWalk is one walk of a file: whether it makes positions, the entries known and which statement is an
// entry's own, and what the occurrence rules need of the file (its extension, the events database, the
// parameters defined so far).
type fileWalk struct {
	schema  *Schema
	visit   Visit
	create  bool
	entries []Entry
	own     map[jomini.Statement]int // an entry's statement: its index in entries
	ext     string                   // the file's extension, lower-case, with its dot; "" for a snippet
	events  string                   // the database an event id names; "" none
	defined map[string]bool          // the parameters defined so far, as <entry>:<NAME>
}

// fits is the test of each key rule: whether the rule reads a key, by the facts about it.
var fits = [...]func(KeyFacts) bool{
	game.KeyRuleKnownCall:      func(facts KeyFacts) bool { return facts.Known },
	game.KeyRuleDeclared:       func(facts KeyFacts) bool { return facts.Child != nil && facts.Child.DeclaredBy != SourceNone },
	game.KeyRuleDocumented:     func(facts KeyFacts) bool { return facts.Context.Database.Fields[positionName(facts.Key)] },
	game.KeyRuleCommand:        func(facts KeyFacts) bool { return facts.Command != nil },
	game.KeyRuleOperator:       func(facts KeyFacts) bool { return game.IsOperator(strings.ToLower(facts.Key)) },
	game.KeyRuleLink:           func(facts KeyFacts) bool { return facts.Link != nil },
	game.KeyRuleObserved:       func(facts KeyFacts) bool { return facts.Create || facts.Child != nil && facts.Child.Vanilla.Count > 0 },
	game.KeyRulePosition:       func(KeyFacts) bool { return true },
	game.KeyRulePositionOrCall: func(KeyFacts) bool { return true },
	game.KeyRuleCall:           func(KeyFacts) bool { return true },
}

// WalkFile reads a whole file with its entries known: every statement in source order, outer before inner, each
// in the context it is written in and the one its block reads in, with what its words name. With create, the
// walk makes the positions it reads (the count). A whole-file entry (a history file) holds the root's statements
// as one block. A table or list (parsed nil) has no statements: its entries are its records.
func (schema *Schema) WalkFile(file inventory.File, parsed *jomini.Result, entries []Entry, create bool, visit Visit) {
	if parsed == nil {
		return
	}
	walk := &fileWalk{
		schema: schema, visit: visit, create: create, entries: entries, own: map[jomini.Statement]int{},
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
	walk.walk(top, Context{Entry: -1})
}

// WalkBlock reads a snippet's statements at a context, with no entries and no names: the tests read script this
// way.
func (schema *Schema) WalkBlock(context Context, statements []jomini.Statement, create bool, visit Visit) {
	walk := &fileWalk{schema: schema, visit: visit, create: create, own: map[jomini.Statement]int{}}
	walk.walk(statements, context)
}

// walk reads a block's statements at a context: for each, the context its block reads in (an entry's statement
// opens its database's; any other steps by Enter), what its words name, then the visitor, then its block when the
// visitor agrees. An entry's own statement is read in its holder's context with the entry as its own.
func (walk *fileWalk) walk(statements []jomini.Statement, context Context) {
	var previous jomini.Statement
	for _, statement := range statements {
		at, inner := context, Context{}
		if i, isEntry := walk.own[statement]; isEntry && walk.schema.Databases[walk.entries[i].Database] != nil {
			at.Entry, inner = int32(i), walk.entryOpens(i, statement)
		} else {
			inner = walk.schema.Enter(at, statement, previous, walk.create)
		}
		var occurrences []Occurrence
		if walk.ext != "" {
			occurrences = walk.occurrencesOf(at, inner, statement)
		}
		block := jomini.ChildBlock(statement)
		inner.Block = block
		if walk.visit(statement, at, &inner, occurrences) && block != nil {
			walk.walk(block.Statements, inner)
		}
		previous = statement
	}
}

// entryOpens is the context an entry's statement opens: its database's, in the scope the dumps declare for an
// on_action.
func (walk *fileWalk) entryOpens(i int, statement jomini.Statement) Context {
	database := walk.schema.Databases[walk.entries[i].Database]
	inner := walk.schema.EntryContext(database)
	inner.Entry = int32(i)
	if assign, isAssign := statement.(*jomini.Assignment); isAssign && database.Row.OnActions {
		if scope := walk.schema.OnActions[assign.Key.Text].Scope; scope != "" {
			inner.Scope = scope
		}
	}
	return inner
}

// EntryContext is where a database's entry reads: at its entry position, in the shape its entry holds, in the
// scope its entry declares when the dumps say that scope runs triggers or effects.
func (schema *Schema) EntryContext(database *Database) Context {
	shape, scope := database.Entry.Holds.Value, database.Entry.Scope.Value
	if state := schema.Scopes[scope]; !state.Triggers && !state.Effects {
		scope = ""
	}
	return Context{
		At: database.Entry, Shape: shape, Scope: scope, Caller: game.Shapes[shape].Caller, Rule: game.KeyRulePosition,
		Database: database, Entry: -1, InTrigger: shape == game.ShapeTriggers,
	}
}

// Enter is the one step of the walk: from the context a statement is written in to the one its block reads in.
// Outside an entry, or for a statement with no key, the block reads where the statement stands. A date key (a
// number in a dated entry) reopens the holder; so does a key holding a parameter or written as a data call, with
// its scope, command and call unknown. A calling word written before a key calls it by that word (blockoverride
// "x" { }). Any other key is read by its rule (keyEnter). With create, the key's position is made.
func (schema *Schema) Enter(context Context, statement, previous jomini.Statement, create bool) Context {
	assign, isAssign := statement.(*jomini.Assignment)
	reopened := context
	reopened.Rule = game.KeyRuleNone
	if !isAssign || context.Database == nil {
		return reopened
	}
	if caller, isCaller := previous.(*jomini.ValueStmt); isCaller {
		if scalar, isScalar := caller.Value.(*jomini.Scalar); isScalar && len(schema.Callees[scalar.Text]) > 0 {
			context.Caller, reopened.Caller = scalar.Text, scalar.Text
		}
	}
	key := assign.Key.Text
	literal := jomini.Literal(key)
	if literal == jomini.LitDate || context.Database.Row.Dated && literal == jomini.LitNumber {
		return reopened
	}
	if schema.Commands[strings.ToLower(key)] == nil && writtenInPlace(key) {
		reopened.Scope, reopened.Command, reopened.Call = "", "", ""
		return reopened
	}
	return schema.keyEnter(context, assign, create)
}

// writtenInPlace reports whether a key holds a parameter ($ACTOR$, $TARGET$.liege) or is a data call written
// with arguments (name(…)).
func writtenInPlace(key string) bool {
	if len(jomini.Interpolations(key)) > 0 {
		return true
	}
	return strings.Contains(key, "(") && slices.ContainsFunc(jomini.ParseCall(key, 0), func(part jomini.Step) bool {
		return part.Range.End-part.Range.Start > len(part.Name)
	})
}

// keyEnter reads a key by the first rule the holder's shape tries whose test holds, else the last tried, and
// places its block by the rule's row: under the holder by the key's written form (made when the walk creates),
// at the scripted entry it calls, at the command's arguments, or where the key stands. A call names the entry it
// calls; a key in a block that writes commands is a call too. Then the block's shape and scope (blockOf).
func (schema *Schema) keyEnter(context Context, assign *jomini.Assignment, create bool) Context {
	facts := schema.keyFacts(context, assign, create)
	row := game.KeyRuleRow{At: game.AtNone}
	inner := Context{Database: context.Database, Entry: context.Entry, Bound: context.Bound, Rule: game.KeyRulePosition}
	for _, rule := range game.Shapes[context.Shape].Keys {
		inner.Rule = rule
		if fits[rule](facts) {
			row = game.KeyRules[rule]
			break
		}
	}
	key := assign.Key.Text
	switch row.At {
	case game.AtChild:
		inner.At = facts.Child
		if create && context.At != nil {
			inner.At = context.At.EnsureChild(key)
		}
	case game.AtCallee:
		inner.At, inner.Call = facts.Callee.Parameters, key
		if inner.At == nil {
			inner.At = schema.Databases[facts.Callee.Database].Entry
		}
	case game.AtArgs:
		inner.At, inner.Command = facts.Command.Args, strings.ToLower(key)
	case game.AtHolder:
		inner.At = context.At
	}
	writesCommands := context.At != nil && context.At.Vanilla.Writes&WritesCommand != 0
	if inner.Rule == game.KeyRuleCall || inner.Rule == game.KeyRulePositionOrCall && writesCommands {
		inner.Call = key
	}
	return schema.blockOf(context, inner, facts, row)
}

// keyFacts gathers what the fit tests ask of one key: the position under the holder by its written form, the
// declared command or link it names, the scripted entry the holder's caller calls by it.
func (schema *Schema) keyFacts(context Context, assign *jomini.Assignment, create bool) KeyFacts {
	key := assign.Key.Text
	lower := strings.ToLower(key)
	command := schema.Commands[lower]
	callee, called := schema.ScriptedEntries[context.Caller+":"+key]
	facts := KeyFacts{
		Context: context, Key: key, Block: jomini.BlockOf(assign.Value), Command: command, Link: schema.Link(lower),
		Callee: callee, Known: called && command == nil, Create: create,
	}
	if context.At != nil {
		facts.Child = context.At.Child(key)
	}
	if command != nil && command.Kinds&game.KindLink != 0 {
		facts.Command = nil // a link is read by Link
	}
	return facts
}

// blockOf is what the block a key opens holds and the scope it runs in. The first construct that fits the key
// where it stands, or the list iterator a call starts with when the block iterates a scripted list, places a key
// the shape's rules do not place (a description's markers and trigger read under the holder, their shape claimed
// for the registry when the walk creates; the leaf reads at the bound position). The shape is the rule's row's
// (holdsOf, commandShape, the holder's, arguments), then the construct's; a block written as a dynamic description
// is one whatever the rows say, and entering a description binds its leaf to the position entered. The block's
// trigger state is the holder's, a trigger's (exactly a trigger, not one that is also an effect), or a triggers
// block's.
func (schema *Schema) blockOf(context, inner Context, facts KeyFacts, row game.KeyRuleRow) Context {
	key, verb := facts.Key, row.Scope
	construct, found := game.ConstructOf(context.Shape, context.Command, positionName(key))
	iterates := inner.Call == key && inner.Rule != game.KeyRuleKnownCall && facts.Block != nil &&
		schema.Lists != "" && context.Caller != "" && context.Caller != game.AnyCall
	if list, isList := game.ListIterator(key); iterates && isList {
		construct, found, inner.List = list, true, strings.TrimPrefix(key, list.Key)
	}
	switch {
	case found && construct.Leaf:
		inner.At, inner.Leaf = context.Bound, true
	case found && row.At == game.AtNone:
		inner.At = facts.Child
		if facts.Create && context.At != nil {
			inner.At = context.At.EnsureChild(key)
			inner.At.Holds.Set(construct.Holds, SourceRegistry)
		}
	}
	switch row.Holds {
	case game.HoldsClaim:
		inner.Shape = schema.holdsOf(inner.At)
	case game.HoldsCommand:
		inner.Shape = schema.commandShape(facts.Command, context.Shape)
	case game.HoldsSame:
		inner.Shape = context.Shape
	case game.HoldsArguments:
		inner.Shape = game.ShapeArguments
	}
	switch {
	case found && construct.Leaf:
		inner.Shape = game.ShapeDescription // a leaf written as a block holds the description again
	case found && construct.Holds == game.ShapeEnclosing:
		inner.Shape, inner.Caller, verb = context.Shape, construct.Caller, construct.Scope
	case found:
		inner.Shape, inner.Caller, verb = construct.Holds, construct.Caller, construct.Scope
	}
	if facts.Block != nil && inner.Position() != nil && isDescription(facts.Block) {
		inner.Shape, inner.Caller = game.ShapeDescription, ""
	}
	if inner.Caller == "" {
		inner.Caller = game.Shapes[inner.Shape].Caller
	}
	if inner.Shape == game.ShapeDescription && context.Shape != game.ShapeDescription {
		inner.Bound = inner.At
	}
	inner.Scope = schema.scopeOf(context, inner, facts, verb)
	exactTrigger := inner.Rule == game.KeyRuleCommand && facts.Command.Kinds == game.KindTrigger
	inner.InTrigger = context.InTrigger || exactTrigger || inner.Shape == game.ShapeTriggers
	return inner
}

// holdsOf is what a position's block holds: its claim; else a value's formula where the position takes a number
// by a declared source or every key written under it is an operator; else triggers or effects where vanilla
// writes only one of them there; else fields where vanilla writes keys and no command; else unknown.
func (schema *Schema) holdsOf(position *Position) game.Shape {
	switch {
	case position == nil:
		return game.ShapeUnknown
	case position.Holds.Source != SourceNone:
		return position.Holds.Value
	case position.Value.Value.Type == ValueNumber && position.Value.Source.Declared(), onlyOperators(position):
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

// onlyOperators reports whether a position has children and every one is a value operator.
func onlyOperators(position *Position) bool {
	for key := range position.Children {
		if !game.IsOperator(key) {
			return false
		}
	}
	return len(position.Children) > 0
}

// isDescription reports whether a block is a dynamic description: it holds a marker whose own block holds the
// leaf or another marker (title = { first_valid = { triggered_desc = { desc = … } } }).
func isDescription(block *jomini.Block) bool {
	words := game.DynamicDescription
	inside := append([]string{words.Leaf}, words.Markers...)
	return block != nil && slices.ContainsFunc(block.Statements, func(statement jomini.Statement) bool {
		assign, isAssign := statement.(*jomini.Assignment)
		return isAssign && slices.Contains(words.Markers, assign.Key.Text) && writesAny(jomini.BlockOf(assign.Value), inside)
	})
}

// writesAny reports whether a block has an assignment to one of the keys.
func writesAny(block *jomini.Block, keys []string) bool {
	return block != nil && slices.ContainsFunc(block.Statements, func(statement jomini.Statement) bool {
		assign, isAssign := statement.(*jomini.Assignment)
		return isAssign && slices.Contains(keys, assign.Key.Text)
	})
}

// commandShape is what a command's block holds, given the shape of the block it is written in: a value's
// formula when it takes a number or is written in one, arguments when its usages write arguments only, the
// holder's shape when it is both a trigger and an effect, else its own kind's.
func (schema *Schema) commandShape(command *Command, holder game.Shape) game.Shape {
	switch {
	case schema.holdsOf(command.Args) == game.ShapeValue:
		return game.ShapeValue
	case command.Args.Holds.Value == game.ShapeArguments:
		return game.ShapeArguments
	case holder == game.ShapeValue:
		return game.ShapeValue
	case command.Kinds&(game.KindTrigger|game.KindEffect) == game.KindTrigger|game.KindEffect:
		return holder
	case command.Kinds&game.KindTrigger != 0:
		return game.ShapeTriggers
	case command.Kinds&game.KindEffect != 0:
		return game.ShapeEffects
	}
	return game.ShapeUnknown
}

// scopeOf is the scope a key's block runs in. A scope claim on the key's position beats the verb. ScopeFollows: a
// link leads where its last link does; a command leads by commandScope. ScopeStays keeps the holder's scope where
// the block's shape is known or the position claims one. A scope the dumps say runs no triggers and no effects is
// unknown.
func (schema *Schema) scopeOf(context, inner Context, facts KeyFacts, verb game.ScopeVerb) string {
	position := inner.At
	if inner.Rule == game.KeyRuleLink {
		position = nil // a link reads where it stands; the holder's claim is not its own
	}
	scope := ""
	switch {
	case position != nil && position.Scope.Source != SourceNone:
		scope = position.Scope.Value
	case verb == game.ScopeUnknown:
	case verb == game.ScopeFollows && inner.Rule == game.KeyRuleLink:
		last := strings.ToLower(facts.Key[strings.LastIndexByte(facts.Key, '.')+1:])
		if link := schema.Link(last); link != nil {
			if _, leads := schema.Scopes[link.Target]; leads {
				scope = link.Target
			}
		}
	case verb == game.ScopeFollows:
		scope = schema.commandScope(facts.Command, context.Scope)
	case verb == game.ScopeStays && (inner.Shape != game.ShapeUnknown || position != nil && position.Holds.Source != SourceNone):
		scope = context.Scope
	}
	if state := schema.Scopes[scope]; !state.Triggers && !state.Effects {
		scope = ""
	}
	return scope
}

// commandScope is the scope a command's block runs in: its target when its arguments hold triggers or effects
// and the target is a scope and it walks no list; unknown when it walks a list or its target is bare; else the
// holder's.
func (schema *Schema) commandScope(command *Command, holder string) string {
	holds := command.Args.Holds.Value
	iterates := holds == game.ShapeTriggers || holds == game.ShapeEffects
	_, leads := schema.Scopes[command.Target]
	switch {
	case iterates && leads && !command.Lists:
		return command.Target
	case command.Lists, command.Target != "" && !iterates:
		return ""
	}
	return holder
}
