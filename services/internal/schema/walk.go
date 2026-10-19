// walk.go is the walker: WalkFile reads every statement of a file in the context it is written in and the one
// its block reads in, deciding the entries as it meets them, WalkBlock a snippet's; Enter is the one step from
// the first context to the second.

package schema

import (
	"path"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/inventory"
	"paradox-modding-tools/services/internal/jomini"
)

// Scope is where a block's script runs: the scope type it is in (This), the one its entry runs in (Root), the ones
// entered on the way here (Prev, the last the nearest), the scopes its entry has saved by name so far (Named, one
// map shared by the entry's blocks), and why This is what it is, in plain words. "" is unknown.
type Scope struct {
	This   string            `json:"this,omitempty"`
	Root   string            `json:"root,omitempty"`
	Prev   []string          `json:"prev,omitempty"`
	Named  map[string]string `json:"named,omitempty"`
	Reason string            `json:"reason,omitempty"`
}

// enter is the record one scope deeper: the scope entered, the one left pushed on Prev, and how it was entered.
func (scope Scope) enter(this, reason string) Scope {
	return Scope{
		This: this, Root: scope.Root, Prev: append(slices.Clip(scope.Prev), scope.This), Named: scope.Named, Reason: reason,
	}
}

// forget is the record with the scope and the ones before it unknown, for a reason; the root and the saved scopes
// are kept.
func (scope Scope) forget(reason string) Scope {
	return Scope{Root: scope.Root, Named: scope.Named, Reason: reason}
}

// save records a scope saved under a name: this one where the block runs effects, else one the command makes
// (create_character = { save_scope_as = x } saves the new character), unknown; unknown too once the name is
// saved as two scopes.
func (scope Scope) save(name string, this string) {
	if saved, isSaved := scope.Named[name]; isSaved && saved != this {
		this = ""
	}
	scope.Named[name] = this
}

// Context is where statements are read: the state a walk inherits at a block, and for the block a statement opens,
// how that statement was read.
type Context struct {
	At        *Position     // the position the block reads at; nil when none is known
	Shape     game.Shape    // what the block holds
	Scope     Scope         // where the block's script runs
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
	Defines   bool          // the statement that opened this context defines an entry
	top       bool          // the statements here are a file's top ones: the root's, or a wrapper's
	within    string        // the keys from the entry's own block down to this one, joined by /, as Descriptor.Within
}

// Position is the own position of the key that opened the context: nil for a link, which reads where it stands,
// for a statement that reopened its holder, and for one with no key.
func (context Context) Position() *Position {
	if context.Rule == game.KeyRuleLink || context.Rule == game.KeyRuleNone {
		return nil
	}
	return context.At
}

// ValuePosition is the position whose claims type a statement's value: the key's own for an assignment (the
// context its block reads in), the block's for a word listed in it (the context it is written in).
func ValuePosition(statement jomini.Statement, context, inner Context) *Position {
	if _, isAssign := statement.(*jomini.Assignment); isAssign {
		return inner.Position()
	}
	return context.Position()
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

// fileWalk is one walk of a file: whether it makes positions, the file and its own database, how its entries
// are told (its file type, the nested descriptors that hold for it, the keywords that declare an entry and the
// databases they declare), the entries met so far, the statement a whole-file entry is, and the parameters
// defined so far.
type fileWalk struct {
	schema   *Schema
	visit    Visit
	create   bool
	rel      string            // the file's path from its root
	own      string            // the database the file's folder makes; "" at a root
	database *Database         // that database; nil for none
	ext      string            // the file's extension, lower-case, with its dot; "" for a snippet
	fileType game.FileType     // how the extension names entries
	nested   []game.Descriptor // the nested descriptors that hold for the file
	declares map[string]string // a keyword → the database an entry it declares is in
	inline   map[string]bool   // the databases a keyword declares inline in other folders' files
	entries  []Occurrence      // the definitions met, in source order
	whole    jomini.Statement  // the statement a whole-file entry is; nil for none
	defined  map[string]bool   // the parameters defined so far, as <entry>:<NAME>
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

// WalkFile reads a whole file: every statement in source order, outer before inner, each in the context it is
// written in and the one its block reads in, with what its words name; it returns the entries the file defines, in
// source order. With create, the walk makes the positions it reads (the count) and the databases its entries need.
// A table or list (parsed nil) has no statements: its entries are its rows. An extension no file type names
// defines nothing.
func (schema *Schema) WalkFile(file inventory.File, parsed *jomini.Result, create bool, visit Visit) []Occurrence {
	_, own := schema.folder(file.Rel)
	layout := game.Get(schema.Game).Layout
	walk := &fileWalk{
		schema: schema, visit: visit, create: create, rel: file.Rel, own: own, database: schema.Databases[own],
		ext: strings.ToLower(path.Ext(file.Rel)), nested: layout.Nested(file.Rel),
		declares: map[string]string{}, inline: map[string]bool{}, defined: map[string]bool{},
	}
	if parsed == nil {
		return walk.tableEntries(file)
	}
	fileType, typed := game.FileTypes[walk.ext]
	if !typed {
		return nil
	}
	walk.fileType = fileType
	for _, row := range layout.Descriptors {
		if row.Keyword != "" {
			walk.declares[row.Keyword], walk.inline[row.Database] = row.Database, true
		}
	}
	for _, word := range fileType.Keywords {
		walk.declares[word] = walk.ext[1:] + "." + word // a file type's keywords declare an entry in <ext>.<word>
	}
	top := parsed.Root.Statements
	if statement, whole := walk.wholeFileEntry(parsed); whole {
		top, walk.whole = []jomini.Statement{statement}, statement
	}
	for i := range top { // a scripted entry declared inline (at the top, in every install) is called anywhere in its file
		if entry, _, isEntry := walk.entryAt(Context{Entry: -1, top: true}, top, i); create && isEntry && entry.Local {
			schema.registerScriptedEntries(entry)
		}
	}
	walk.walk(top, Context{Entry: -1, top: true})
	return walk.entries
}

// WalkBlock reads a snippet's statements at a context, with no entries and no names: the tests read script this
// way.
func (schema *Schema) WalkBlock(context Context, statements []jomini.Statement, create bool, visit Visit) {
	walk := &fileWalk{schema: schema, visit: visit, create: create}
	walk.walk(statements, context)
}

// walk reads a block's statements at a context: for each, whether it defines an entry (entryAt) and the context
// its block reads in (an entry's statement opens its database's; any other steps by Enter), what its words name,
// then the visitor, then its block when the visitor agrees. An entry's own statement is read in its holder's
// context with the entry as its own; one whose database the schema lacks is listed, and read as any statement.
func (walk *fileWalk) walk(statements []jomini.Statement, context Context) {
	for i, statement := range statements {
		at, inner, opened := context, Context{}, false
		if entry, nested, isEntry := walk.entryAt(context, statements, i); isEntry {
			walk.entries = append(walk.entries, entry)
			if inner, opened = walk.entryOpens(context, entry, statement, nested); opened {
				at.Entry = entry.Entry
			}
		}
		if !opened {
			var previous jomini.Statement
			if i > 0 {
				previous = statements[i-1]
			}
			inner = walk.schema.Enter(at, statement, previous, walk.create)
			inner.top, inner.within = walk.below(context, statement)
		}
		var occurrences []Occurrence
		if walk.ext != "" {
			occurrences = walk.occurrencesOf(at, inner, statement)
		}
		block := jomini.ChildBlock(statement)
		inner.Block = block
		if (walk.visit == nil || walk.visit(statement, at, &inner, occurrences)) && block != nil {
			walk.walk(block.Statements, inner)
		}
	}
}

// below is where a statement that defines no entry reads its block: a wrapper's block holds top statements; any
// other block inside an entry reads one key further from the entry's own block, never at the top.
func (walk *fileWalk) below(context Context, statement jomini.Statement) (top bool, within string) {
	assign, isAssign := statement.(*jomini.Assignment)
	if !isAssign {
		return false, context.within
	}
	if context.top && jomini.BlockOf(assign.Value) != nil && walk.database != nil &&
		slices.Contains(walk.database.Descriptor.Wrappers, assign.Key.Text) {
		return true, ""
	}
	if context.Entry < 0 {
		return false, ""
	}
	return false, path.Join(context.within, assign.Key.Text)
}

// entryOpens is the context an entry's statement opens: its database's (made when the walk creates and none has
// it; false when none has it and the walk does not), with the entry as its own. A nested entry's block continues
// the key path of the entry holding it; any other's starts one.
func (walk *fileWalk) entryOpens(context Context, entry Occurrence, statement jomini.Statement, nested bool) (Context, bool) {
	database := walk.schema.Databases[entry.Database]
	if database == nil && walk.create {
		extension, _, _ := strings.Cut(entry.Database, ".")
		walk.schema.declareDatabase(entry.Database, game.Descriptor{Database: entry.Database, Body: game.FileTypes["."+extension].Body})
		database = walk.schema.Databases[entry.Database]
	}
	if database == nil {
		return Context{}, false
	}
	inner := walk.schema.EntryContext(database, entry.ID)
	inner.Entry, inner.Defines = entry.Entry, true
	if assign, isAssign := statement.(*jomini.Assignment); nested && isAssign {
		inner.within = path.Join(context.within, assign.Key.Text)
	}
	return inner, true
}

// EntryContext is where a database's entry reads: at its entry position, in the shape its entry holds, where its
// entries run (EntryScope, by the entry's name).
func (schema *Schema) EntryContext(database *Database, id string) Context {
	shape := database.Entry.Holds.Value
	return Context{
		At: database.Entry, Shape: shape, Scope: schema.EntryScope(database, id), Caller: game.Shapes[shape].Caller,
		Rule: game.KeyRulePosition, Database: database, Entry: -1, InTrigger: shape == game.ShapeTriggers,
	}
}

// EntryScope is where a database's entries run: in the scope the entry declares, when the dumps say it runs script;
// an on_action in the scope the dumps declare for it by name. That scope is the root; nothing is saved yet.
func (schema *Schema) EntryScope(database *Database, id string) Scope {
	scope := Scope{Named: map[string]string{}}
	if this := database.Entry.Scope.Value; schema.RunsScript(this) {
		scope.This, scope.Reason = this, "every "+game.KindName(database.Name)+" runs in it"
	}
	if action := schema.OnActions[id]; database.Descriptor.Purpose == game.PurposeOnActions && action.Scope != "" {
		scope.This, scope.Reason = action.Scope, id+" runs in it"
	}
	scope.Root = scope.This
	return scope
}

// RunsScript reports whether a scope type the dumps declare evaluates triggers or executes effects.
func (schema *Schema) RunsScript(scopeType string) bool {
	state := schema.Scopes[scopeType]
	return state.Triggers || state.Effects
}

// Enter is the one step of the walk: from the context a statement is written in to the one its block reads in.
// Outside an entry, or for a statement with no key, the block reads where the statement stands. A date key (a
// number in a dated entry) reopens the holder; so does a key holding a parameter or written as a data call, with
// its scope, command and call unknown. A calling word written before a key calls it by that word (blockoverride
// "x" { }). A command that saves the scope under a name (save_scope_as = x) records it for the entry. Any other key
// is read by its rule (keyEnter). With create, the key's position is made.
func (schema *Schema) Enter(context Context, statement, previous jomini.Statement, create bool) Context {
	assign, isAssign := statement.(*jomini.Assignment)
	reopened := context
	reopened.Rule, reopened.Defines = game.KeyRuleNone, false
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
	if literal == jomini.LitDate || context.Database.Descriptor.Dated && literal == jomini.LitNumber {
		return reopened
	}
	if schema.Commands[strings.ToLower(key)] == nil && writtenInPlace(key) {
		reopened.Scope, reopened.Command, reopened.Call = context.Scope.forget(key+" is filled in as the game runs"), "", ""
		return reopened
	}
	if saves := game.NameCommands[strings.ToLower(key)]; saves.Makes && saves.Database == game.SavedScopes && context.Scope.Named != nil {
		if scalar, isScalar := assign.Value.(*jomini.Scalar); isScalar {
			this := ""
			if game.Shapes[context.Shape].Allows&game.KindEffect != 0 {
				this = context.Scope.This
			}
			context.Scope.save(scalar.Text, this)
		}
	}
	return schema.keyEnter(context, assign, create)
}

// writtenInPlace reports whether a key holds a parameter ($ACTOR$, $TARGET$.liege) or is a data call written
// with arguments (name(…)).
func writtenInPlace(key string) bool {
	if len(jomini.Parameters(key)) > 0 {
		return true
	}
	return strings.Contains(key, "(") && slices.ContainsFunc(jomini.ParseCall(key, 0), func(part jomini.CallStep) bool {
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
	construct, isConstruct := game.ConstructOf(context.Shape, context.Command, positionName(key))
	// a construct that names no shape (an operator the dumps declare) leaves the block to the key's rule
	isConstruct = isConstruct && (construct.Leaf || construct.Holds != game.ShapeUnknown)
	iterates := inner.Call == key && inner.Rule != game.KeyRuleKnownCall && facts.Block != nil &&
		schema.Lists != "" && context.Caller != "" && context.Caller != game.AnyCall
	if list, isList := game.ListIterator(key); iterates && isList {
		construct, isConstruct, inner.List = list, true, strings.TrimPrefix(key, list.Key)
	}
	switch {
	case isConstruct && construct.Leaf:
		inner.At, inner.Leaf = context.Bound, true
	case isConstruct && row.At == game.AtNone:
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
	case isConstruct && construct.Leaf:
		inner.Shape = game.ShapeDescription // a leaf written as a block holds the description again
	case isConstruct && construct.Holds == game.ShapeEnclosing:
		inner.Shape, inner.Caller, verb = context.Shape, construct.Caller, construct.Scope
	case isConstruct:
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

// scopeOf is where a key's block runs. A scope claim on the key's position beats the verb and starts a chain of its
// own. ScopeFollows: a link chain leads link by link (Follow), a command's block as commandScope says. ScopeStays
// keeps the holder's record where the block's shape is known or the position claims one; else the scope is unknown.
func (schema *Schema) scopeOf(context, inner Context, facts KeyFacts, verb game.ScopeVerb) Scope {
	from, key, position := context.Scope, facts.Key, inner.At
	if inner.Rule == game.KeyRuleLink {
		position = nil // a link reads where it stands; the holder's claim is not its own
	}
	switch {
	case position != nil && position.Scope.Source != SourceNone:
		if this := position.Scope.Value; schema.RunsScript(this) {
			return Scope{This: this, Root: this, Named: from.Named, Reason: key + " is declared to run in it"}
		}
		return from.forget("where " + key + " runs is not declared")
	case verb == game.ScopeUnknown:
		return from.enter("", key+" iterates a list")
	case verb == game.ScopeFollows && inner.Rule == game.KeyRuleLink:
		return schema.Follow(from, key)
	case verb == game.ScopeFollows:
		return schema.commandScope(from, facts.Command, key)
	case verb == game.ScopeStays && (inner.Shape != game.ShapeUnknown || position != nil && position.Holds.Source != SourceNone):
		return from
	}
	return from.forget("what " + key + " holds is not declared")
}

// Follow is where a link chain leads from a scope, link by link: a declared link to its target; root to the scope
// the entry runs in; prev to the scope before this one; this to the same; scope:x to the scope the entry saved as
// x. A link to a scope type that runs no script, or to a name not saved above as one scope, leads to an unknown
// one; the links after it still lead where they lead. The block entered has the scope left before it.
func (schema *Schema) Follow(from Scope, chain string) Scope {
	at := from
	for part := range strings.SplitSeq(chain, ".") {
		prefix, name, _ := strings.Cut(part, ":")
		why, whyNot := part+" leads to it", "where "+part+" leads is not declared"
		switch link := schema.Link(strings.ToLower(part)); game.ScopeLinks[strings.ToLower(prefix)] {
		case game.LinkRoot:
			at.This, why, whyNot = from.Root, "the root scope", "the root scope is not declared"
		case game.LinkPrev:
			at.This, why, whyNot = "", "the previous scope", "the scope before this one is not known"
			if last := len(at.Prev) - 1; last >= 0 {
				at.This, at.Prev = at.Prev[last], at.Prev[:last]
			}
		case game.LinkThis:
			continue
		case game.LinkSaved:
			saved, isSaved := at.Named[name]
			at.This, why, whyNot = saved, "saved as "+part, part+" is saved nowhere above"
			if isSaved {
				whyNot = "what " + part + " holds is not known"
			}
		default:
			at.This = ""
			if link != nil && schema.RunsScript(link.Target) {
				at.This = link.Target
			}
		}
		at.Reason = why
		if at.This == "" {
			at.Reason = whyNot
		}
	}
	return from.enter(at.This, at.Reason)
}

// commandScope is where a command's block runs: in its target when its arguments hold script and it walks no list;
// unknown when it walks a list, or leads somewhere while its arguments hold no known script; else where its holder
// runs.
func (schema *Schema) commandScope(from Scope, command *Command, key string) Scope {
	holds := command.Args.Holds.Value
	iterates := holds == game.ShapeTriggers || holds == game.ShapeEffects
	_, leads := schema.Scopes[command.Target]
	switch {
	case iterates && leads && !command.Lists && !schema.RunsScript(command.Target):
		return from.enter("", key+" leads to "+command.Target+", which runs no script")
	case iterates && leads && !command.Lists:
		return from.enter(command.Target, key+" leads to it")
	case command.Lists:
		return from.enter("", key+" iterates a list")
	case command.Target != "" && !iterates:
		return from.forget("what " + key + " holds is not declared")
	}
	return from
}
