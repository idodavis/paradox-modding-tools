// walk.go is the walker: WalkFile reads every statement of a file in its context and decides the entries as it meets
// them; Enter is the step from the context a statement is written in to the one its block reads in.

package schema

import (
	"path"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/inventory"
	"paradox-modding-tools/services/internal/jomini"
)

// Scope is where a block's script runs; "" is unknown.
type Scope struct {
	This   string            `json:"this,omitempty"`   // the scope type it is in
	Root   string            `json:"root,omitempty"`   // the one its entry runs in
	Prev   []string          `json:"prev,omitempty"`   // the ones entered on the way here, the nearest last
	Named  map[string]string `json:"named,omitempty"`  // the scopes its entry has saved so far, shared by its blocks
	Reason string            `json:"reason,omitempty"` // why This is what it is, in plain words
}

// enter returns the record one scope deeper: the scope entered, the one left pushed on Prev, and how it was entered.
func (scope Scope) enter(this, reason string) Scope {
	return Scope{
		This: this, Root: scope.Root, Prev: append(slices.Clip(scope.Prev), scope.This), Named: scope.Named, Reason: reason,
	}
}

// forget returns the record with the scope and the ones before it unknown, for a reason; the root and the saved scopes
// are kept.
func (scope Scope) forget(reason string) Scope {
	return Scope{Root: scope.Root, Named: scope.Named, Reason: reason}
}

// save records the scope saved under a name: this one, or unknown where the command makes another (create_character
// saves the new character) or the name is saved as two scopes.
func (scope Scope) save(name string, this string) {
	if saved, isSaved := scope.Named[name]; isSaved && saved != this {
		this = ""
	}
	scope.Named[name] = this
}

// Context is where statements are read: the state a block inherits and how the statement that opened it was read.
type Context struct {
	BlockPosition *Position     // the position the block reads at; nil when none is known
	Shape         game.Shape    // what the block holds
	Scope         Scope         // where the block's script runs
	Caller        string        // the word a call in the block resolves by
	Call          string        // the scripted entry this block calls, whose parameters its keys pass; "" none
	Command       string        // the command whose own block this is, lower-case; "" none
	List          string        // the scripted list this block iterates, named by the rest of its key; "" none
	KeyRule       game.KeyRule  // the rule the key that opened this block was read by
	Database      *Database     // the database of the entry the block is in; nil outside an entry
	Entry         int32         // the entry the block is in, as its ordinal in the file's entries; -1 none
	LeafPosition  *Position     // the position a description's leaf reads at; nil outside a description
	Block         *jomini.Block // the block whose statements read here; nil at a file's top
	InTrigger     bool          // inside a trigger
	Leaf          bool          // the statement that opened this context is a description's leaf
	Keyword       game.Keyword  // the keyword that fitted the key that opened this context; zero for none
	Defines       bool          // the statement that opened this context defines an entry
	top           bool          // the statements here are a file's top ones: the root's, or a wrapper's
	keyPath       string        // the keys from the entry's own block down to this one, joined by /
}

// Position returns the own position of the key that opened the context: nil for a link, for a statement that reopened
// its holder and for one with no key.
func (context Context) Position() *Position {
	if context.KeyRule == game.KeyRuleLink || context.KeyRule == game.KeyRuleNone {
		return nil
	}
	return context.BlockPosition
}

// ValuePosition returns the position whose claims type a statement's value: the key's own for an assignment, the
// block's for a word listed in it.
func ValuePosition(statement jomini.Statement, context, inner Context) *Position {
	if _, isAssign := statement.(*jomini.Assignment); isAssign {
		return inner.Position()
	}
	return context.Position()
}

// KeyFacts is what the key rules' fit tests are tried against: one key at a context.
type KeyFacts struct {
	Context Context       // where the key is written
	Key     string        // the key as written
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

// fileWalk is one walk of a file.
type fileWalk struct {
	schema      *Schema
	visit       Visit               // called at each statement
	create      bool                // the walk makes the positions it reads
	rel         string              // the file's path from its root
	own         string              // the database the file's folder makes; "" at a root
	database    *Database           // that database; nil for none
	ext         string              // the file's extension, lower-case, with its dot
	fileRule    game.DatabaseRule   // how the extension names entries
	nestedRules []game.DatabaseRule // the nested rules that hold for the file
	declares    map[string]string   // a keyword → the database an entry it declares is in
	inline      map[string]bool     // the databases a keyword declares inline in other folders' files
	entries     []Occurrence        // the definitions met, in source order
	whole       jomini.Statement    // the statement a whole-file entry is; nil for none
	defined     map[string]bool     // the parameters defined so far, as <entry>:<NAME>
}

// fits is the test of each key rule: whether the rule reads a key, by the facts about it.
var fits = [...]func(KeyFacts) bool{
	game.KeyRuleKnownCall:      func(facts KeyFacts) bool { return facts.Known },
	game.KeyRuleDeclared:       func(facts KeyFacts) bool { return facts.Child != nil && facts.Child.Source != game.SourceNone },
	game.KeyRuleDocumented:     func(facts KeyFacts) bool { return facts.Context.Database.Fields[positionName(facts.Key)] },
	game.KeyRuleCommand:        func(facts KeyFacts) bool { return facts.Command != nil },
	game.KeyRuleOperator:       func(facts KeyFacts) bool { return game.IsOperator(strings.ToLower(facts.Key)) },
	game.KeyRuleLink:           func(facts KeyFacts) bool { return facts.Link != nil },
	game.KeyRuleObserved:       func(facts KeyFacts) bool { return facts.Create || facts.Child != nil && facts.Child.Vanilla.Count > 0 },
	game.KeyRulePosition:       func(KeyFacts) bool { return true },
	game.KeyRulePositionOrCall: func(KeyFacts) bool { return true },
	game.KeyRuleCall:           func(KeyFacts) bool { return true },
}

// WalkFile reads every statement of a file in source order, each in its context, and returns the entries it defines;
// with create it makes the positions and databases it needs. A table or list (parsed nil) has its rows as entries.
func (schema *Schema) WalkFile(file inventory.File, parsed *jomini.Result, create bool, visit Visit) []Occurrence {
	_, own := schema.folder(file.Rel)
	info := game.Lookup(schema.Game)
	walk := &fileWalk{
		schema: schema, visit: visit, create: create, rel: file.Rel, own: own, database: schema.Databases[own],
		ext: strings.ToLower(path.Ext(file.Rel)), nestedRules: info.FindNestedRules(file.Rel),
		declares: map[string]string{}, inline: map[string]bool{}, defined: map[string]bool{},
	}
	if parsed == nil {
		return walk.tableEntries(file)
	}
	walk.fileRule = game.FindRuleByExtension(walk.ext)
	if walk.fileRule.Extension == "" {
		return nil
	}
	for _, rule := range info.Rules {
		if rule.Extension != "" {
			continue
		}
		for _, key := range rule.DeclaringKeys {
			walk.declares[key], walk.inline[rule.Name] = rule.Name, true
		}
	}
	for _, word := range walk.fileRule.DeclaringKeys {
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

// walk reads a block's statements: for each, whether it defines an entry and the context its block reads in (the
// entry's database's, else by Enter), what its words name, then the visitor, then its block when the visitor agrees.
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
			inner.top, inner.keyPath = walk.below(context, statement)
		}
		occurrences := walk.occurrencesOf(at, inner, statement)
		block := jomini.ChildBlock(statement)
		inner.Block = block
		if (walk.visit == nil || walk.visit(statement, at, &inner, occurrences)) && block != nil {
			walk.walk(block.Statements, inner)
		}
	}
}

// below returns where a statement that defines no entry reads its block: a wrapper's block holds top statements; any
// other block inside an entry reads one key further from the entry's own block.
func (walk *fileWalk) below(context Context, statement jomini.Statement) (top bool, keyPath string) {
	assign, isAssign := statement.(*jomini.Assignment)
	if !isAssign {
		return false, context.keyPath
	}
	if context.top && jomini.BlockOf(assign.Value) != nil && walk.database != nil &&
		slices.Contains(walk.database.Rule.WrapperKeys, assign.Key.Text) {
		return true, ""
	}
	if context.Entry < 0 {
		return false, ""
	}
	return false, path.Join(context.keyPath, assign.Key.Text)
}

// entryOpens returns the context an entry's statement opens: its database's, made when the walk creates; false where
// there is none. A nested entry's block continues the key path of the entry holding it.
func (walk *fileWalk) entryOpens(context Context, entry Occurrence, statement jomini.Statement, nested bool) (Context, bool) {
	database := walk.schema.Databases[entry.Database]
	if database == nil && walk.create {
		extension, _, _ := strings.Cut(entry.Database, ".")
		walk.schema.declareDatabase(entry.Database, game.DatabaseRule{Name: entry.Database, Shape: game.FindRuleByExtension("." + extension).Shape})
		database = walk.schema.Databases[entry.Database]
	}
	if database == nil {
		return Context{}, false
	}
	inner := walk.schema.EntryContext(database, entry.ID, jomini.ChildBlock(statement))
	inner.Entry, inner.Defines = entry.Entry, true
	if assign, isAssign := statement.(*jomini.Assignment); nested && isAssign {
		inner.keyPath = path.Join(context.keyPath, assign.Key.Text)
	}
	return inner, true
}

// EntryContext returns where a database's entry reads: at its entry position, in the shape its entry holds, where its
// entries run (ResolveEntryScope; the block may be nil).
func (schema *Schema) EntryContext(database *Database, id string, block *jomini.Block) Context {
	shape := database.Entry.Shape.Value
	return Context{
		BlockPosition: database.Entry, Shape: shape, Scope: schema.ResolveEntryScope(database, id, block), Caller: game.Shapes[shape].Caller,
		KeyRule: game.KeyRulePosition, Database: database, Entry: -1, InTrigger: shape == game.ShapeTriggers,
	}
}

// ResolveEntryScope returns where a database's entries run: the scope its rule gives the entry when the dumps say it
// runs script, an on_action's in the scope the dumps declare for it. That scope is the root; nothing is saved yet.
func (schema *Schema) ResolveEntryScope(database *Database, id string, block *jomini.Block) Scope {
	scope := Scope{Named: map[string]string{}}
	if this, reason := schema.resolveEntryRoot(database, block); schema.RunsScript(this) {
		scope.This, scope.Reason = this, reason
	}
	if action := schema.OnActions[id]; database.Rule.Houses == "on_actions" && action.Scope != "" {
		scope.This, scope.Reason = action.Scope, id+" runs in it"
	}
	scope.Root = scope.This
	return scope
}

// resolveEntryRoot returns the scope an entry's root runs in, and why: the scope key's value in the entry's block
// (none leaves it unknown), else the type key's value less its suffix where that names a scope type, else the default.
func (schema *Schema) resolveEntryRoot(database *Database, block *jomini.Block) (this, reason string) {
	rule, term := database.Rule, game.DescribeEntry(database.Name, false)
	if rule.ScopeKey == "" && rule.TypeKey == "" {
		return database.Entry.Scope.Value, "every " + term + " runs in it"
	}
	if scalar := jomini.KeyValue(block, rule.ScopeKey, jomini.Match{}); rule.ScopeKey != "" && scalar != nil {
		if slices.Contains(game.DumpWords.NoScope, scalar.Text) {
			return "", "its " + rule.ScopeKey + " names none"
		}
		return scalar.Text, "its " + rule.ScopeKey + " says so"
	}
	if scalar := jomini.KeyValue(block, rule.TypeKey, jomini.Match{}); rule.TypeKey != "" && scalar != nil {
		if word, cut := strings.CutSuffix(scalar.Text, rule.TypeSuffix); cut {
			if _, declared := schema.Scopes[word]; declared {
				return word, "its " + rule.TypeKey + " says so"
			}
		}
	}
	return rule.DefaultScope, "every " + term + " runs in it unless it says otherwise"
}

// RunsScript reports whether a scope type the dumps declare evaluates triggers or executes effects.
func (schema *Schema) RunsScript(scopeType string) bool {
	state := schema.Scopes[scopeType]
	return state.Triggers || state.Effects
}

// Enter returns the context a statement's block reads in, from the one it is written in: a key-less block reads where
// it stands, a date key or data call reopens the holder, any other key is read by its rule (keyEnter).
func (schema *Schema) Enter(context Context, statement, previous jomini.Statement, create bool) Context {
	assign, isAssign := statement.(*jomini.Assignment)
	reopened := context
	reopened.KeyRule, reopened.Defines = game.KeyRuleNone, false
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
	if literal == jomini.LitDate || context.Database.Rule.Dated && literal == jomini.LitNumber {
		return reopened
	}
	if schema.Commands[strings.ToLower(key)] == nil && writtenInPlace(key) {
		reopened.Scope, reopened.Command, reopened.Call = context.Scope.forget(key+" is filled in as the game runs"), "", ""
		return reopened
	}
	if saves := game.ScopeVarCommands[strings.ToLower(key)]; saves.Makes && saves.Database == game.SavedScopes && context.Scope.Named != nil {
		if scalar, isScalar := assign.Value.(*jomini.Scalar); isScalar {
			this := ""
			if slices.Contains(game.Shapes[context.Shape].Allows, game.DeclaredEffect) {
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

// keyEnter reads a key by the first rule of the holder's shape that fits, else the last tried, and places its block
// by the rule's row: under the holder, at the entry it calls, at the command's arguments, or where the key stands.
func (schema *Schema) keyEnter(context Context, assign *jomini.Assignment, create bool) Context {
	facts := schema.keyFacts(context, assign, create)
	row := game.KeyRuleRow{BlockAt: game.AtNone}
	inner := Context{
		Database: context.Database, Entry: context.Entry, LeafPosition: context.LeafPosition, KeyRule: game.KeyRulePosition,
		Keyword: lookupKeywordRow(context, facts),
	}
	for _, rule := range game.Shapes[context.Shape].KeyRules {
		inner.KeyRule = rule
		if fits[rule](facts) {
			row = game.KeyRules[rule]
			break
		}
	}
	key := assign.Key.Text
	switch row.BlockAt {
	case game.AtChild:
		inner.BlockPosition = facts.Child
		if create && context.BlockPosition != nil {
			inner.BlockPosition = context.BlockPosition.EnsureChild(key)
		}
	case game.AtCallee:
		inner.BlockPosition, inner.Call = facts.Callee.Parameters, key
		if inner.BlockPosition == nil {
			inner.BlockPosition = schema.Databases[facts.Callee.Database].Entry
		}
	case game.AtArgs:
		inner.BlockPosition, inner.Command = facts.Command.Args, strings.ToLower(key)
	case game.AtHolder:
		inner.BlockPosition = context.BlockPosition
	}
	writesCommands := context.BlockPosition != nil && context.BlockPosition.Vanilla.Writes&WritesCommand != 0
	if inner.KeyRule == game.KeyRuleCall || inner.KeyRule == game.KeyRulePositionOrCall && writesCommands {
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
	if context.BlockPosition != nil {
		facts.Child = context.BlockPosition.Child(key)
	}
	if command != nil && slices.Contains(command.Declared, game.DeclaredLink) {
		facts.Command = nil // a link is read by Link
	}
	return facts
}

// blockOf returns what the block a key opens holds and the scope it runs in: the keyword that fits the key places a key
// the shape's rules do not, and a dynamic description is one whatever the rows say.
func (schema *Schema) blockOf(context, inner Context, facts KeyFacts, row game.KeyRuleRow) Context {
	key, verb := facts.Key, row.ScopeFrom
	keyword, isKeyword := game.LookupKeyword(context.Shape, context.Command, positionName(key))
	// a keyword that names no shape (an operator the dumps declare) leaves the block to the key's rule
	isKeyword = isKeyword && (keyword.Leaf || keyword.Shape != game.ShapeUnknown)
	iterates := inner.Call == key && inner.KeyRule != game.KeyRuleKnownCall && facts.Block != nil &&
		schema.Lists != "" && context.Caller != "" && context.Caller != game.AnyCall
	if list, isList := game.MatchListIterator(key); iterates && isList {
		keyword, isKeyword, inner.List, inner.Keyword = list, true, strings.TrimPrefix(key, list.Key), list
	}
	switch {
	case isKeyword && keyword.Leaf:
		inner.BlockPosition, inner.Leaf = context.LeafPosition, true
	case isKeyword && row.BlockAt == game.AtNone:
		inner.BlockPosition = facts.Child
		if facts.Create && context.BlockPosition != nil {
			inner.BlockPosition = context.BlockPosition.EnsureChild(key)
			inner.BlockPosition.Shape.Set(keyword.Shape, game.SourceRegistry)
		}
	}
	switch row.ShapeFrom {
	case game.HoldsClaim:
		inner.Shape = schema.holdsOf(inner.BlockPosition)
	case game.HoldsCommand:
		inner.Shape = schema.commandShape(facts.Command, context.Shape)
	case game.HoldsSame:
		inner.Shape = context.Shape
	case game.HoldsArguments:
		inner.Shape = game.ShapeArguments
	}
	switch {
	case isKeyword && keyword.Leaf:
		inner.Shape = game.ShapeDescription // a leaf written as a block holds the description again
	case isKeyword && keyword.Shape == game.ShapeEnclosing:
		inner.Shape, inner.Caller, verb = context.Shape, keyword.Caller, keyword.ScopeFrom
	case isKeyword:
		inner.Shape, inner.Caller, verb = keyword.Shape, keyword.Caller, keyword.ScopeFrom
	}
	if facts.Block != nil && inner.Position() != nil && isDescription(facts.Block) {
		inner.Shape, inner.Caller = game.ShapeDescription, ""
	}
	if inner.Caller == "" {
		inner.Caller = game.Shapes[inner.Shape].Caller
	}
	if inner.Shape == game.ShapeDescription && context.Shape != game.ShapeDescription {
		inner.LeafPosition = inner.BlockPosition
	}
	inner.Scope = schema.scopeOf(context, inner, facts, verb)
	exactTrigger := inner.KeyRule == game.KeyRuleCommand && isDeclaredOnly(facts.Command, game.DeclaredTrigger)
	inner.InTrigger = context.InTrigger || exactTrigger || inner.Shape == game.ShapeTriggers
	return inner
}

// lookupKeywordRow returns the keyword row that fits a key, by its position name in lower case (the games write AND and
// and alike); a list iterator's row is the walk's to give (blockOf), only where the key calls a list.
func lookupKeywordRow(context Context, facts KeyFacts) game.Keyword {
	row, _ := game.LookupKeyword(context.Shape, context.Command, strings.ToLower(positionName(facts.Key)))
	return row
}

// holdsOf returns what a position's block holds: its claim, else a value's formula where it takes a number or only
// operators are written under it, else triggers or effects where vanilla writes only one of them, else fields.
func (schema *Schema) holdsOf(position *Position) game.Shape {
	switch {
	case position == nil:
		return game.ShapeUnknown
	case position.Shape.Source != game.SourceNone:
		return position.Shape.Value
	case position.Value.Value.Type == ValueNumber && position.Value.Source.Accuses(), onlyOperators(position):
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
	return block != nil && slices.ContainsFunc(block.Statements, func(statement jomini.Statement) bool {
		assign, isAssign := statement.(*jomini.Assignment)
		return isAssign && game.LookupDescriptionKeyword(assign.Key.Text).Shows != "" &&
			writesDescription(jomini.BlockOf(assign.Value))
	})
}

// writesDescription reports whether a block has an assignment to the leaf or a marker.
func writesDescription(block *jomini.Block) bool {
	return block != nil && slices.ContainsFunc(block.Statements, func(statement jomini.Statement) bool {
		assign, isAssign := statement.(*jomini.Assignment)
		if !isAssign {
			return false
		}
		row := game.LookupDescriptionKeyword(assign.Key.Text)
		return row.Leaf || row.Shows != ""
	})
}

// commandShape returns what a command's block holds in a holder of a shape: a value's formula, arguments, the
// holder's shape where the command is both trigger and effect, else its own kind's.
func (schema *Schema) commandShape(command *Command, holder game.Shape) game.Shape {
	switch {
	case schema.holdsOf(command.Args) == game.ShapeValue:
		return game.ShapeValue
	case command.Args.Shape.Value == game.ShapeArguments:
		return game.ShapeArguments
	case holder == game.ShapeValue:
		return game.ShapeValue
	case slices.Contains(command.Declared, game.DeclaredTrigger) && slices.Contains(command.Declared, game.DeclaredEffect):
		return holder
	case slices.Contains(command.Declared, game.DeclaredTrigger):
		return game.ShapeTriggers
	case slices.Contains(command.Declared, game.DeclaredEffect):
		return game.ShapeEffects
	}
	return game.ShapeUnknown
}

// scopeOf returns where a key's block runs: a scope claim on its position, else by the key rule's ScopeFrom (a link
// chain or a command's block follows, otherwise the holder's scope stays where the shape is known).
func (schema *Schema) scopeOf(context, inner Context, facts KeyFacts, verb game.ScopeFrom) Scope {
	from, key, position := context.Scope, facts.Key, inner.BlockPosition
	if inner.KeyRule == game.KeyRuleLink {
		position = nil // a link reads where it stands; the holder's claim is not its own
	}
	switch {
	case position != nil && position.Scope.Source != game.SourceNone:
		if this := position.Scope.Value; schema.RunsScript(this) {
			return Scope{This: this, Root: this, Named: from.Named, Reason: key + " is declared to run in it"}
		}
		return from.forget("where " + key + " runs is not declared")
	case verb == game.ScopeUnknown:
		return from.enter("", key+" iterates a list")
	case verb == game.ScopeFollows && inner.KeyRule == game.KeyRuleLink:
		return schema.Follow(from, key)
	case verb == game.ScopeFollows:
		return schema.commandScope(from, facts.Command, key)
	case verb == game.ScopeStays && (inner.Shape != game.ShapeUnknown || position != nil && position.Shape.Source != game.SourceNone):
		return from
	}
	return from.forget("what " + key + " holds is not declared")
}

// Follow returns where a link chain leads from a scope, link by link (a declared link, root, prev, this, scope:x); a
// scope that runs no script, or a name not saved as one scope, leads to an unknown one.
func (schema *Schema) Follow(from Scope, chain string) Scope {
	at := from
	for part := range strings.SplitSeq(chain, ".") {
		prefix, name, _ := strings.Cut(part, ":")
		why, whyNot := part+" leads to it", "where "+part+" leads is not declared"
		switch link := schema.Link(strings.ToLower(part)); strings.ToLower(prefix) {
		case "root":
			at.This, why, whyNot = from.Root, "the root scope", "the root scope is not declared"
		case "prev":
			at.This, why, whyNot = "", "the previous scope", "the scope before this one is not known"
			if last := len(at.Prev) - 1; last >= 0 {
				at.This, at.Prev = at.Prev[last], at.Prev[:last]
			}
		case "this":
			continue
		case game.SavedScopes:
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

// commandScope returns where a command's block runs: its target when its arguments hold script and it walks no list,
// unknown when it walks a list or leads somewhere while its arguments hold no script, else where its holder runs.
func (schema *Schema) commandScope(from Scope, command *Command, key string) Scope {
	holds := command.Args.Shape.Value
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
