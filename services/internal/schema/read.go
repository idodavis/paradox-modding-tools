// read.go is the walker. Every reader of script (vanilla's evidence, the docs and wiki, the dumps'
// usages, a file's index records) goes through Read, which reads each statement by the tables in
// game/lang-rules.go.

package schema

import (
	"cmp"
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
	Rule         game.KeyRule  // the step its key was read by
	Position     *Position     // its position; nil when none
	Command      *Command      // the declared command or link its key is
	Calls        string        // set when its key is a call: the CalledIn word it resolves by
	Listed       string        // set when its key iterates a scripted list: the iterator prefix
	Leaf         bool          // the leaf of a dynamic description
	BlockContext               // where it is written
	Inner        BlockContext  // where its own block is read
}

// Visitor is one use of the walker. Create makes the positions it reads (vanilla, usages); Entry
// says which database's entry a statement starts, if any; Names lists the names a key is read as
// (a docs key's a/b|c alternatives); Visit sees each step, may change where its block is read, and
// returns whether to read that block.
type Visitor struct {
	Create bool
	Entry  func(jomini.Statement) *Database
	Names  func(key string) []string
	Visit  func(step *Reading) bool
}

// WalkBlock reads a block's statements (holder nil: a file's top) at a place.
func (schema *Schema) WalkBlock(reader Visitor, holder *jomini.Block, statements []jomini.Statement, at BlockContext) {
	for index, statement := range statements {
		assign, isAssign := statement.(*jomini.Assignment)

		// an entry starts at its database; a key inside an entry is read once per name; a bare value,
		// or anything outside an entry, stays at the place's position
		var steps []Reading
		var database *Database
		if reader.Entry != nil {
			database = reader.Entry(statement)
		}
		switch {
		case database != nil:
			inner := schema.EntryContext(database)
			if isAssign && database.Row.OnActions && schema.OnActions[assign.Key.Text].Scope != "" {
				inner.Scope = schema.OnActions[assign.Key.Text].Scope // the scope the dumps declare for it
			}
			steps = []Reading{{Statement: statement, Position: database.Entry, BlockContext: at, Inner: inner}}
		case isAssign && at.InEntry:
			names, here := []string{assign.Key.Text}, at
			if reader.Names != nil {
				names = reader.Names(assign.Key.Text)
			}
			if index > 0 { // a calling word written before a name calls it by that word (blockoverride "x" { })
				if before, isWord := statements[index-1].(*jomini.ValueStmt); isWord {
					if word, isScalar := before.Value.(*jomini.Scalar); isScalar && len(schema.Callees[word.Text]) > 0 {
						here.Calls = word.Text
					}
				}
			}
			for _, name := range names {
				steps = append(steps, schema.ReadKey(here, statement, name, reader.Create))
			}
		default:
			steps = []Reading{{Statement: statement, Position: at.Position, BlockContext: at, Inner: at}}
		}

		block := jomini.ChildBlock(statement)
		for i := range steps {
			steps[i].Holder = holder
			if reader.Visit(&steps[i]) && block != nil {
				schema.WalkBlock(reader, block, block.Statements, steps[i].Inner)
			}
		}
	}
}

// EntryContext is where a database's entry is read: at its entry position, in the scope it declares.
func (schema *Schema) EntryContext(database *Database) BlockContext {
	kind, scope := database.Entry.Block.Value, database.Entry.Scope.Value
	if declared := schema.Scopes[scope]; !declared.Triggers && !declared.Effects {
		scope = ""
	}
	return BlockContext{Position: database.Entry, Kind: kind, Calls: game.Blocks[kind].Calls, Scope: scope,
		InTrigger: kind == game.BlockTriggers, InEntry: true, Dated: database.Row.Dated, Fields: database.Fields}
}

// ReadKey reads one key written at a place: what it is by the block's steps (or the dynamic
// description shape), then where its own block is read. With create, the position is made.
func (schema *Schema) ReadKey(at BlockContext, statement jomini.Statement, name string, create bool) Reading {
	step := Reading{Statement: statement, Name: name, BlockContext: at}
	create = create && at.Position != nil
	lower, position := strings.ToLower(name), at.Position.Child(name)
	made := func() *Position {
		if create {
			return at.Position.EnsureChild(name)
		}
		return position
	}
	var value jomini.Value
	if assign, isAssign := statement.(*jomini.Assignment); isAssign {
		value = assign.Value
	}

	// a date key re-opens the block it stands in: the same keys again, at that date (history)
	if literal := jomini.Literal(name); literal == jomini.LitDate || at.Dated && literal == jomini.LitNumber {
		step.Inner = at
		return step
	}

	// a key holding a parameter ($ACTOR$, $TARGET$.liege) or written as a data call (name(…)) is a
	// scope written in place, no position and no call: its block reads where it stands, scope unknown
	command := schema.Commands[lower]
	if command == nil && (len(jomini.Interpolations(name)) > 0 || strings.Contains(name, "(") &&
		slices.ContainsFunc(jomini.ParseCall(name, 0), func(part jomini.Step) bool { return part.Range.End-part.Range.Start > len(part.Name) })) {
		step.Inner, step.Inner.Scope, step.Inner.Command, step.Inner.Call = at, "", "", ""
		return step
	}
	macro, called := schema.ScriptedEntries[at.Calls+":"+name] // a scripted entry the block's calling word calls
	known := called && command == nil
	if command != nil && command.Kind&game.KindLink != 0 {
		command = nil
	}
	link := schema.Link(lower)
	number, operator := game.ValueOperators[lower]

	// what the key is: in a description by its shape, else the first of the block's steps that fits
	shape := game.DynamicDescription
	if at.Kind == game.BlockDescription {
		step.Rule = game.KeyRulePosition
		switch {
		case slices.Contains(shape.Markers, name):
			step.Position = made()
		case name == shape.Trigger:
			if step.Position = made(); create {
				SetFirst(&step.Position.Block, game.BlockTriggers, SourceRegistry)
			}
		case name == shape.Leaf:
			step.Position, step.Leaf = at.Bound, true
		}
	} else {
	steps:
		for _, rule := range game.Blocks[at.Kind].Keys {
			step.Rule = rule
			switch {
			case rule == game.KeyRuleKnownCall && known:
				// its block holds the entry's parameters, or reads at its database's entry
				step.Position, step.Calls = cmp.Or(macro.Parameters, schema.Databases[macro.Database].Entry), at.Calls
			case rule == game.KeyRuleDeclared && position != nil && position.DeclaredBy != SourceNone:
				step.Position = position
			case rule == game.KeyRuleDocumented && at.Fields[collapse(name)]:
				step.Position = made()
			case rule == game.KeyRuleCommand && command != nil:
				step.Position, step.Command = command.Args, command
			case rule == game.KeyRuleOperator && operator:
				step.Position = made()
			case rule == game.KeyRuleLink && link != nil:
				step.Command = link
			case rule == game.KeyRuleObserved && (create || position != nil && position.Evidence.In > 0):
				step.Position = made()
			case rule == game.KeyRulePosition:
				step.Position = made()
			case rule == game.KeyRulePositionOrCall:
				step.Position = made()
				if at.Position != nil && at.Position.Evidence.Forms&FormCommand != 0 {
					step.Calls = at.Calls
				}
			case rule == game.KeyRuleCall:
				step.Position, step.Calls = made(), at.Calls
			default:
				continue
			}
			break steps
		}
	}
	isLink := step.Command != nil && step.Command.Kind&game.KindLink != 0
	isCommand := step.Command != nil && !isLink

	// a call in a triggers, effects or value block that starts with an iterator prefix, takes a block
	// and names no known scripted entry also iterates the scripted list named by the rest
	listing := step.Calls != "" && step.Rule != game.KeyRuleKnownCall && jomini.BlockOf(value) != nil
	if word := at.Calls; listing && schema.Lists != "" && word != "" && word != game.AnyCall {
		for prefix := range game.ListIterators {
			if strings.HasPrefix(name, prefix) {
				step.Listed = prefix
			}
		}
	}

	// what its block holds: a link's reads where the link stands, a command's by the contents rules,
	// an operator's by what it takes, a description part's by the shape, a position's by its claim
	inner := BlockContext{InEntry: at.InEntry, Bound: at.Bound}
	switch {
	case isLink:
		inner, inner.Command, inner.Call = at, "", ""
	case isCommand:
		inner.Position, inner.Kind, inner.Command = step.Command.Args, schema.contents(step.Command, at.Kind), lower
	case step.Rule == game.KeyRuleOperator && step.Position != nil:
		inner.Position, inner.Kind = step.Position, game.BlockArguments
		if number {
			inner.Kind = game.BlockValue
		}
	case at.Kind == game.BlockDescription && step.Position != nil && name != shape.Trigger:
		inner.Position, inner.Kind = step.Position, game.BlockDescription
	default:
		inner.Position, inner.Kind = step.Position, holds(step.Position)
	}
	for _, row := range game.Structurals {
		if row.Command == at.Command && (row.Key == "" || row.Key == collapse(name)) {
			inner.Kind, inner.Calls = row.Holds, row.Calls
			if row.Enclosing {
				inner.Kind = at.Kind
			}
		}
	}
	if at.Kind == game.BlockWidget { // a widget's blocks are widgets again: no script is read in a GUI file
		inner.Kind = game.BlockWidget
	}
	if step.Listed != "" {
		inner.Kind = game.ListIterators[step.Listed]
	}
	if inner.Calls == "" {
		inner.Calls = game.Blocks[inner.Kind].Calls
	}
	if inner.Kind != game.BlockDescription && step.Position != nil && binds(value) {
		inner.Kind, inner.Bound, inner.Calls = game.BlockDescription, step.Position, ""
	}
	if step.Calls != "" {
		inner.Call = name
	}
	step.InTrigger = at.InTrigger || isCommand && step.Command.Kind == game.KindTrigger
	inner.InTrigger = step.InTrigger || inner.Kind == game.BlockTriggers

	// the scope its block runs in: the first row of the scope table that fits; a scope that runs no
	// triggers and no effects is unknown
	out, iterates := "", false
	switch {
	case isLink:
		if last := schema.Link(strings.ToLower(name[strings.LastIndexByte(name, '.')+1:])); last != nil {
			out = last.Out
		}
	case isCommand:
		out = step.Command.Out
		iterates = step.Command.Args.Block.Value == game.BlockTriggers || step.Command.Args.Block.Value == game.BlockEffects
	}
	_, leads := schema.Scopes[out]
scopes:
	for _, row := range game.ScopeRules {
		switch {
		case row.When == game.WhenClaim && step.Position != nil && step.Position.Scope.Source != SourceNone,
			row.When == game.WhenLink && isLink,
			row.When == game.WhenIterator && isCommand && iterates && leads && !step.Command.Lists,
			row.When == game.WhenList && (step.Listed != "" || isCommand && step.Command.Lists),
			row.When == game.WhenOut && isCommand && step.Command.Out != "" && !iterates,
			row.When == game.WhenValue && (inner.Kind == game.BlockValue || inner.Kind == game.BlockDescription),
			row.When == game.WhenCommand && isCommand,
			row.When == game.WhenClaimed && step.Position != nil && (step.Position.Block.Source != SourceNone || inner.Kind != game.BlockUnknown),
			row.When == game.WhenAnything:
		default:
			continue
		}
		inner.Scope = ""
		switch row.To {
		case game.ToClaim:
			inner.Scope = step.Position.Scope.Value
		case game.ToTarget:
			if leads {
				inner.Scope = out
			}
		case game.ToSame:
			inner.Scope = at.Scope
		}
		break scopes
	}
	if declared := schema.Scopes[inner.Scope]; !declared.Triggers && !declared.Effects {
		inner.Scope = ""
	}
	step.Inner = inner
	return step
}

// contents is the kind of block a command's block is, written in a block of the given kind: the
// first contents rule that fits.
func (schema *Schema) contents(command *Command, kind game.Block) game.Block {
	args := command.Args
	for _, rule := range game.ContentsOrder {
		switch {
		case rule == game.ContentsNumber && holds(args) == game.BlockValue:
			return game.BlockValue
		case rule == game.ContentsUsage && args.Block.Value == game.BlockArguments:
			return game.BlockArguments
		case rule == game.ContentsInValue && kind == game.BlockValue:
			return game.BlockValue
		case rule == game.ContentsBoth && command.Kind&game.KindTrigger != 0 && command.Kind&game.KindEffect != 0:
			return kind
		case rule == game.ContentsTrigger && command.Kind&game.KindTrigger != 0:
			return game.BlockTriggers
		case rule == game.ContentsEffect && command.Kind&game.KindEffect != 0:
			return game.BlockEffects
		}
	}
	return game.BlockUnknown
}

// holds is the kind of block a position holds: its claim, else a value block where a declared
// source says it takes a number, else what vanilla writes there makes it, else unknown.
func holds(position *Position) game.Block {
	// every key vanilla writes there is a value operator (add_prestige = { value = x if = { … } })
	operators := func() bool {
		for name := range position.Children {
			if _, operator := game.ValueOperators[name]; !operator {
				return false
			}
		}
		return len(position.Children) > 0
	}
	switch {
	case position == nil:
	case position.Block.Source != SourceNone:
		return position.Block.Value
	case position.Value.Value.Type == WordNumber && position.Value.Source.Declared(), operators():
		return game.BlockValue
	case position.Evidence.Forms&(FormTrigger|FormEffect) == FormTrigger: // every command vanilla writes there
		return game.BlockTriggers
	case position.Evidence.Forms&(FormTrigger|FormEffect) == FormEffect:
		return game.BlockEffects
	case position.Evidence.Forms&FormCommand == 0 && len(position.Children) > 0: // vanilla writes keys, no command
		return game.BlockFields
	}
	return game.BlockUnknown
}

// binds is whether a block is a dynamic description: it holds a marker whose own block holds the
// leaf or another marker.
func binds(value jomini.Value) bool {
	shape := game.DynamicDescription
	writes := func(value jomini.Value, names []string) []jomini.Value {
		var found []jomini.Value
		if block := jomini.BlockOf(value); block != nil {
			for _, statement := range block.Statements {
				if assign, isAssign := statement.(*jomini.Assignment); isAssign && slices.Contains(names, assign.Key.Text) {
					found = append(found, assign.Value)
				}
			}
		}
		return found
	}
	return slices.ContainsFunc(writes(value, shape.Markers), func(marker jomini.Value) bool {
		return len(writes(marker, append([]string{shape.Leaf}, shape.Markers...))) > 0
	})
}
