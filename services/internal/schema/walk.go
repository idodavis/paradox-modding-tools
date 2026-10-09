// walk.go is the one walker: every reader of script (vanilla's evidence, the docs, the dumps' usages, the
// index's records) walks a block with WalkBlock, which reads each key by ReadKey.

package schema

import (
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/jomini"
)

// BlockContext is where statements are read.
type BlockContext struct {
	Position  *Position       // the position the block reads at; nil when none is known
	Kind      game.Block      // what the block holds
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

// Reading is one statement as the walker reads it.
type Reading struct {
	Statement    jomini.Statement
	Holder       *jomini.Block // the block holding it; nil at a file's top
	Name         string        // the key name read: the key, or one of a docs key's alternatives
	Rule         game.KeyRule  // the rule its key was read by
	Position     *Position     // its position; nil when none
	Command      *Command      // the declared command or link its key is
	Calls        string        // set when its key is a call: the CalledIn word it resolves by
	Listed       string        // set when its key iterates a scripted list: the iterator prefix
	Leaf         bool          // the leaf of a dynamic description
	BlockContext               // where it is written
	Inner        BlockContext  // where its own block is read
}

// Visitor is one use of the walker. Create makes the positions it reads (vanilla, usages); Entry says which
// database's entry a statement starts, nil for none; Alternatives lists the names a key is read as (a docs key's
// a/b|c alternatives); Visit sees each reading, may change where its block is read, and returns whether to
// read that block.
type Visitor struct {
	Create       bool
	Entry        func(jomini.Statement) *Database
	Alternatives func(key string) []string
	Visit        func(*Reading) bool
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

// WalkBlock reads a block's statements at a context; holder is nil at a file's top. A statement that starts
// an entry is read at the entry's position and its block at the entry's context; inside an entry each key
// is read by ReadKey; anything else stays at the context's position.
func (s *Schema) WalkBlock(v Visitor, holder *jomini.Block, statements []jomini.Statement, in BlockContext) {
	for index, statement := range statements {
		var database *Database
		if v.Entry != nil {
			database = v.Entry(statement)
		}
		_, isAssign := statement.(*jomini.Assignment)
		var readings []Reading
		switch {
		case database != nil:
			readings = []Reading{s.entryReading(statement, database, in)}
		case isAssign && in.InEntry:
			readings = s.keyReadings(v, statements, index, in)
		default:
			readings = []Reading{{Statement: statement, Position: in.Position, BlockContext: in, Inner: in}}
		}
		block := jomini.ChildBlock(statement)
		for _, reading := range readings {
			reading.Holder = holder
			if v.Visit(&reading) && block != nil {
				s.WalkBlock(v, block, block.Statements, reading.Inner)
			}
		}
	}
}

// entryReading reads a statement that starts an entry of a database. The dumps declare the scope of an
// on_action's block.
func (s *Schema) entryReading(statement jomini.Statement, database *Database, in BlockContext) Reading {
	inner := s.EntryContext(database)
	if assign, isAssign := statement.(*jomini.Assignment); isAssign && database.Row.OnActions {
		if scope := s.OnActions[assign.Key.Text].Scope; scope != "" {
			inner.Scope = scope
		}
	}
	return Reading{Statement: statement, Position: database.Entry, BlockContext: in, Inner: inner}
}

// keyReadings reads the key of the assignment at statements[index], once for each name the visitor reads
// it as. A calling word written before a name calls it by that word: blockoverride "x" { }.
func (s *Schema) keyReadings(v Visitor, statements []jomini.Statement, index int, in BlockContext) []Reading {
	assign := statements[index].(*jomini.Assignment)
	names := []string{assign.Key.Text}
	if v.Alternatives != nil {
		names = v.Alternatives(assign.Key.Text)
	}
	if index > 0 {
		if listed, isList := statements[index-1].(*jomini.ValueStmt); isList {
			if word, isWord := listed.Value.(*jomini.Scalar); isWord && len(s.Callees[word.Text]) > 0 {
				in.Calls = word.Text
			}
		}
	}
	readings := make([]Reading, 0, len(names))
	for _, name := range names {
		readings = append(readings, s.ReadKey(in, assign, name, v.Create))
	}
	return readings
}

// EntryContext returns where a database's entry is read: at its entry position, in the kind of block its
// entry holds, in the scope its entry declares when the dumps say that scope runs triggers or effects.
func (s *Schema) EntryContext(database *Database) BlockContext {
	kind := database.Entry.Block.Value
	scope := database.Entry.Scope.Value
	if state := s.Scopes[scope]; !state.Triggers && !state.Effects {
		scope = ""
	}
	return BlockContext{
		Position: database.Entry, Kind: kind, Calls: game.Blocks[kind].Calls, Scope: scope,
		InTrigger: kind == game.BlockTriggers, InEntry: true, Dated: database.Row.Dated, Fields: database.Fields,
	}
}

// ReadKey reads one key written at a context: what it is, where its block is read, and the scope that block
// runs in. With create, the key's position is made.
func (s *Schema) ReadKey(in BlockContext, statement jomini.Statement, name string, create bool) Reading {
	r := Reading{Statement: statement, Name: name, BlockContext: in}
	create = create && in.Position != nil
	var value jomini.Value
	if assign, isAssign := statement.(*jomini.Assignment); isAssign {
		value = assign.Value
	}

	// a date key re-opens the block it stands in (history)
	literal := jomini.Literal(name)
	if literal == jomini.LitDate || in.Dated && literal == jomini.LitNumber {
		r.Inner = in
		return r
	}
	// a key holding a parameter or written as a data call is a scope written in place: no position, no call,
	// scope unknown
	if s.Commands[strings.ToLower(name)] == nil && writtenInPlace(name) {
		r.Inner = in
		r.Inner.Scope, r.Inner.Command, r.Inner.Call = "", "", ""
		return r
	}

	m := s.keyMeaning(in, name, in.Position.Child(name), create)
	r.Rule, r.Position, r.Command, r.Calls, r.Leaf = m.rule, m.position, m.command, m.calls, m.leaf
	r.Listed = s.listIterated(in, r, name, value)
	r.Inner = innerContext(in, r, name, value)

	// a trigger is exactly a trigger, not one that is also an effect
	r.InTrigger = in.InTrigger || isCommand(r) && r.Command.Kind == game.KindTrigger
	r.Inner.InTrigger = r.InTrigger || r.Inner.Kind == game.BlockTriggers
	r.Inner.Scope = s.blockScope(in, r, name)
	return r
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
func isLink(r Reading) bool { return r.Command != nil && r.Command.Kind&game.KindLink != 0 }

// isCommand reports whether a reading's key is a declared effect or trigger.
func isCommand(r Reading) bool { return r.Command != nil && !isLink(r) }

// keyMeaning decides what a key is at a context by the block's rules in order; the first whose test holds
// wins. In a description block the shape of a dynamic description decides instead.
func (s *Schema) keyMeaning(in BlockContext, name string, position *Position, create bool) meaning {
	made := func() *Position {
		if create {
			return in.Position.EnsureChild(name)
		}
		return position
	}
	if in.Kind == game.BlockDescription {
		return descriptionMeaning(in, name, made, create)
	}

	lower := strings.ToLower(name)
	declared := s.Commands[lower]
	entry, called := s.ScriptedEntries[in.Calls+":"+name]
	k := keyFacts{
		in: in, name: name, position: position, made: made, create: create,
		entry: entry, known: called && declared == nil, declared: declared, link: s.Link(lower),
	}
	if declared != nil && declared.Kind&game.KindLink != 0 {
		k.declared = nil // a link is read by Link
	}
	_, k.isOperator = game.ValueOperators[lower]

	var found meaning
	for _, rule := range game.Blocks[in.Kind].Keys {
		var fits bool
		if found, fits = s.fits(rule, k); fits {
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
	declared   *Command      // a declared effect or trigger, else nil
	link       *Command      // the declared link the key starts with
	isOperator bool
}

// fits tries one rule against a key and returns what the key is by it; fits is false when the rule does not
// hold. The meaning keeps the rule either way, so a block with no fitting rule reads by its last.
func (s *Schema) fits(rule game.KeyRule, k keyFacts) (found meaning, fits bool) {
	found = meaning{rule: rule}
	switch {
	case rule == game.KeyRuleKnownCall && k.known:
		found.position, found.calls = k.entry.Parameters, k.in.Calls
		if found.position == nil {
			found.position = s.Databases[k.entry.Database].Entry
		}
	case rule == game.KeyRuleDeclared && k.position != nil && k.position.DeclaredBy != SourceNone:
		found.position = k.position
	case rule == game.KeyRuleDocumented && k.in.Fields[positionName(k.name)]:
		found.position = k.made()
	case rule == game.KeyRuleCommand && k.declared != nil:
		found.position, found.command = k.declared.Args, k.declared
	case rule == game.KeyRuleOperator && k.isOperator:
		found.position = k.made()
	case rule == game.KeyRuleLink && k.link != nil:
		found.command = k.link
	case rule == game.KeyRuleObserved && (k.create || k.position != nil && k.position.Evidence.In > 0):
		found.position = k.made()
	case rule == game.KeyRulePosition:
		found.position = k.made()
	case rule == game.KeyRulePositionOrCall:
		found.position = k.made()
		if k.in.Position != nil && k.in.Position.Evidence.Forms&FormCommand != 0 {
			found.calls = k.in.Calls
		}
	case rule == game.KeyRuleCall:
		found.position, found.calls = k.made(), k.in.Calls
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
			SetFirst(&found.position.Block, game.BlockTriggers, SourceRegistry)
		}
	case name == shape.Leaf:
		found.position, found.leaf = in.Bound, true
	}
	return found
}

// listIterated returns the iterator prefix when a key iterates a scripted list: a call in a triggers,
// effects or value block that starts with an iterator prefix, takes a block and names no known scripted
// entry iterates the scripted list named by the rest (every_adult_child).
func (s *Schema) listIterated(in BlockContext, r Reading, name string, value jomini.Value) string {
	listing := r.Calls != "" && r.Rule != game.KeyRuleKnownCall && jomini.BlockOf(value) != nil
	if !listing || s.Lists == "" || in.Calls == "" || in.Calls == game.AnyCall {
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
func blockKindOf(position *Position) game.Block {
	switch {
	case position == nil:
		return game.BlockUnknown
	case position.Block.Source != SourceNone:
		return position.Block.Value
	case position.Value.Value.Type == WordNumber && position.Value.Source.Declared(), writesOnlyOperators(position):
		return game.BlockValue
	}
	switch position.Evidence.Forms & (FormTrigger | FormEffect) {
	case FormTrigger:
		return game.BlockTriggers // every command vanilla writes there is a trigger
	case FormEffect:
		return game.BlockEffects
	}
	if position.Evidence.Forms&FormCommand == 0 && len(position.Children) > 0 {
		return game.BlockFields // vanilla writes keys and no command
	}
	return game.BlockUnknown
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
func commandBlockKind(command *Command, in game.Block) game.Block {
	switch {
	case blockKindOf(command.Args) == game.BlockValue:
		return game.BlockValue
	case command.Args.Block.Value == game.BlockArguments:
		return game.BlockArguments
	case in == game.BlockValue:
		return game.BlockValue
	case command.Kind&(game.KindTrigger|game.KindEffect) == game.KindTrigger|game.KindEffect:
		return in
	case command.Kind&game.KindTrigger != 0:
		return game.BlockTriggers
	case command.Kind&game.KindEffect != 0:
		return game.BlockEffects
	}
	return game.BlockUnknown
}

// innerContext returns where a key's own block is read, apart from its scope: what the key is decides the
// position and kind, then the structural keys (random_list's branches, switch's cases), widgets, list
// iterators and dynamic descriptions adjust them.
func innerContext(in BlockContext, r Reading, name string, value jomini.Value) BlockContext {
	inner := BlockContext{InEntry: in.InEntry, Bound: in.Bound}
	lower := strings.ToLower(name)
	switch {
	case isLink(r):
		inner = in // a link's block reads where the link stands
		inner.Command, inner.Call = "", ""
	case isCommand(r):
		inner.Position, inner.Command = r.Command.Args, lower
		inner.Kind = commandBlockKind(r.Command, in.Kind)
	case r.Rule == game.KeyRuleOperator && r.Position != nil:
		inner.Position, inner.Kind = r.Position, game.BlockArguments
		if game.ValueOperators[lower] {
			inner.Kind = game.BlockValue // the operator takes a number
		}
	case in.Kind == game.BlockDescription && r.Position != nil && name != game.DynamicDescription.Trigger:
		inner.Position, inner.Kind = r.Position, game.BlockDescription
	default:
		inner.Position, inner.Kind = r.Position, blockKindOf(r.Position)
	}

	for _, row := range game.Structurals {
		if row.Command == in.Command && (row.Key == "" || row.Key == positionName(name)) {
			inner.Kind, inner.Calls = row.Holds, row.Calls
			if row.Enclosing {
				inner.Kind = in.Kind
			}
		}
	}
	if in.Kind == game.BlockWidget {
		inner.Kind = game.BlockWidget // a widget's blocks are widgets again
	}
	if r.Listed != "" {
		inner.Kind = game.ListIterators[r.Listed]
	}
	if inner.Calls == "" {
		inner.Calls = game.Blocks[inner.Kind].Calls
	}
	if inner.Kind != game.BlockDescription && r.Position != nil && isDynamicDescription(value) {
		inner.Kind, inner.Bound, inner.Calls = game.BlockDescription, r.Position, ""
	}
	if r.Calls != "" {
		inner.Call = name // the scripted entry this block calls, whose parameters its keys pass
	}
	return inner
}

// blockScope returns the scope a key's block runs in. Order matters: a scope the position declares beats
// everything; a link or an iterating command leads to its target; a list or a bare target gives no scope; a
// value or description block, a command's block and a block a source gives a kind stay where they are
// written; anything else is unknown. A scope the dumps say runs no triggers and no effects is unknown too.
func (s *Schema) blockScope(in BlockContext, r Reading, name string) string {
	target, iterates := "", false
	switch {
	case isLink(r):
		last := strings.ToLower(name[strings.LastIndexByte(name, '.')+1:]) // a chain leads where its last link does
		if link := s.Link(last); link != nil {
			target = link.Out
		}
	case isCommand(r):
		target = r.Command.Out
		iterates = r.Command.Args.Block.Value == game.BlockTriggers || r.Command.Args.Block.Value == game.BlockEffects
	}
	_, leads := s.Scopes[target]
	kind := r.Inner.Kind

	declared := r.Position != nil && r.Position.Scope.Source != SourceNone
	followsTarget := isLink(r) || isCommand(r) && iterates && leads && !r.Command.Lists
	walksList := r.Listed != "" || isCommand(r) && r.Command.Lists
	bareTarget := isCommand(r) && r.Command.Out != "" && !iterates
	staysPut := kind == game.BlockValue || kind == game.BlockDescription || isCommand(r) ||
		r.Position != nil && (r.Position.Block.Source != SourceNone || kind != game.BlockUnknown)

	scope := ""
	switch {
	case declared:
		scope = r.Position.Scope.Value
	case followsTarget && leads:
		scope = target
	case followsTarget, walksList, bareTarget:
		scope = ""
	case staysPut:
		scope = in.Scope
	}
	if state := s.Scopes[scope]; !state.Triggers && !state.Effects {
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
