// walk.go is the classify walk: one pass over a file's tree with the schema, keeping each
// statement's context. It is the only place that decides what a written word is (spec §7.2).

package index

import (
	"cmp"
	"maps"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/schema"
	"paradox-modding-tools/services/internal/lang/source"
)

// Context is what the walk knows at one statement: Block, Scope and InTrigger say where it is
// written; Key and Command are what its key reads as; the Inner fields are how its own block reads.
type Context struct {
	Statement    jomini.Statement
	Entry        int32           // index into the Records' Defs, -1 outside an entry
	Key          *schema.Key     // the statement's position
	Command      *schema.Command // when the key is a declared command
	Block        schema.Block
	Scope        string // "" unknown
	InTrigger    bool
	InnerKey     *schema.Key // the position its block reads at: a link's is its parent's; nil unknown
	InnerBlock   schema.Block
	InnerScope   string // the scope its block runs in; "" unknown
	InnerTrigger bool   // its block reads inside a trigger
}

// Walk classifies a file with the schema: its records, and each statement's context handed to visit
// (nil for none); visit returning false stops the walk.
func Walk(sch *schema.Schema, file source.File, parsed *jomini.Result, visit func(Context) bool) *Records {
	records := &Records{scoped: map[[2]string]bool{}}

	// where an offset is, from the parse's lines; a table or sound list (parsed nil) is read for them
	var lines *jomini.LineIndex
	if parsed != nil {
		lines = parsed.Lines()
	}
	position := func(offset int) (int32, int32) {
		if lines == nil {
			text, _ := file.Text()
			lines = jomini.NewLineIndex(text)
		}
		at := lines.PositionAt(offset)
		return int32(at.Line), int32(at.Character)
	}

	// define records a definition; a name is cloned, so a record never pins the file's text
	define := func(database, name, mode string, span jomini.Range) {
		def := Def{Database: database, Name: strings.Clone(name), Mode: strings.Clone(mode), Path: file.Path, Range: span}
		def.Line, def.Col = position(span.Start)
		records.Defs = append(records.Defs, def)
	}

	// the entry detector's definitions, so an entry's index is its place in Defs; a whole file's
	// entry holds the root's statements as one block
	own, top := map[jomini.Statement]int32{}, []jomini.Statement(nil)
	if parsed != nil {
		top = parsed.Root.Statements
	}
	sch.Entries(file, parsed, func(entry schema.Entry) {
		span := entry.Statement.NodeRange()
		if assign, isAssign := entry.Statement.(*jomini.Assignment); isAssign {
			span = assign.Key.Range
		}
		if parsed != nil && entry.Statement.NodeRange() == parsed.Root.Range {
			top = []jomini.Statement{entry.Statement}
		}
		own[entry.Statement] = int32(len(records.Defs))
		define(entry.Database, entry.Name, entry.Mode, span)
	})

	// a link prefix's database, kept per prefix (Prefixed); an event id names an entry of the
	// database whose registry row is Events (D-25)
	links, events := map[string]string{}, cmp.Or(sch.Flagged(func(row game.Folder) bool { return row.Events })...)

	// a word can name something unless it is a constant (@x) or holds a parameter ($X$)
	named := func(word string) bool {
		_, constant := jomini.Constant(word)
		return word != "" && !constant && len(jomini.Interpolations(word)) == 0
	}
	refer := func(ref Ref, at Context) {
		ref.Name, ref.Path, ref.From, ref.InTrigger = strings.Clone(ref.Name), file.Path, at.Entry, at.InTrigger
		ref.Line, ref.Col = position(ref.Range.Start)
		records.Refs = append(records.Refs, ref)
	}

	// linked refers each part of a word read through a declared link (scope:x, root.var:y),
	// saying whether one was
	linked := func(word string, span jomini.Range, at Context) (found bool) {
		if !strings.Contains(word, ":") {
			return false
		}
		for _, part := range strings.Split(word, ".") {
			prefixed, isPrefixed := jomini.SplitPrefixed(part)
			if !isPrefixed || sch.Link(strings.ToLower(part)) == nil {
				continue
			}
			found = true
			database, known := links[prefixed.Prefix]
			if !known {
				database = Prefixed(sch, prefixed.Prefix)
				links[prefixed.Prefix] = database
			}
			if database != "" {
				refer(Ref{Database: database, Name: prefixed.Name, Range: span}, at)
			}
		}
		return found
	}

	// parameters defines each $NAME$ in a word of an entry whose registry row passes parameters, as
	// <entry>:<NAME> (D-31)
	parameters := func(word *jomini.Scalar, at Context) {
		if at.Entry < 0 || !strings.Contains(word.Text, "$") {
			return
		}
		entry := records.Defs[at.Entry]
		if database := sch.Databases[entry.Database]; database == nil || !database.Naming.Parameters {
			return
		}
		start := word.Range.Start
		if word.Quoted {
			start++
		}
		for _, span := range jomini.Interpolations(word.Text) {
			define(Parameter, entry.Name+":"+span.Name, "", jomini.Range{Start: start + span.Start, End: start + span.End})
		}
	}

	// walk visits a block's statements read at under (nil: outside any entry, nothing classified) in
	// the context at, inside the lower-case command whose Args it reads (NameCommands' command/key),
	// as the arguments of call when the block is a call's
	nothing, stopped := &schema.Key{}, false
	var walk func(statements []jomini.Statement, under *schema.Key, at Context, enclosing, call string)
	walk = func(statements []jomini.Statement, under *schema.Key, at Context, enclosing, call string) {
		for _, statement := range statements {
			if stopped {
				return
			}
			here := at
			here.Statement = statement
			assign, isAssign := statement.(*jomini.Assignment)
			var value jomini.Value
			if isAssign {
				value = assign.Value
			} else if list, isList := statement.(*jomini.ValueStmt); isList {
				value = list.Value
			}

			// below is where the statement's block reads, inner the context it gives, inside its command,
			// calls the name it calls
			below, inner, inside, calls := under, at, enclosing, ""
			entry, isEntry := own[statement]
			switch {
			case isEntry:
				// an entry starts at its database's position, in the root scope it claims
				here.Entry, below, inner, inside = entry, nil, Context{Entry: entry}, ""
				if database := sch.Databases[records.Defs[entry].Database]; database != nil {
					here.Key, below = database.Entry, database.Entry
					inner.Block, inner.Scope = below.Block.Value, below.Scope.Value
					inner.InTrigger = inner.Block == schema.BlockTriggers
				}
			case under == nil, !isAssign:
				// outside an entry nothing is classified; a list element is at the position holding it
				if under != nothing {
					here.Key = under
				}
			default:
				// the slot decides the kind (D-17): a command reads on in its Args, a link's block at
				// under, a field at its position; an unknown key's block is one nothing declares
				key := assign.Key.Text
				here.Key, here.Command = sch.Slot(under, key, false)
				command := here.Command
				here.InTrigger = here.InTrigger || command != nil && command.Kind == schema.KindTrigger

				// a key in a call's block passes the called entry's parameter of its name (D-31)
				if call != "" {
					refer(Ref{Database: Parameter, Name: call + ":" + key, Range: assign.Key.Range}, here)
				}

				// a data call (name(…), jomini's call form) is no field and no call
				data := command == nil && strings.Contains(key, "(") && slices.ContainsFunc(jomini.ParseCall(key, 0),
					func(step jomini.Step) bool { return step.Range.End-step.Range.Start > len(step.Name) })
				if data {
					here.Key = nil
				}

				// out is where the key leads: a link chain's last link's Out (root.liege), a command's Out
				below, inside = here.Key, ""
				out := ""
				switch {
				case command != nil && command.Kind&schema.KindLink != 0:
					below, inside = under, enclosing
					if last := sch.Link(strings.ToLower(key[strings.LastIndexByte(key, '.')+1:])); last != nil {
						out = last.Out
					}
				case command != nil:
					inside, out = strings.ToLower(key), command.Out
				}
				if below == nil {
					below = nothing
				}

				// the scope inside comes from the position's claim, then where the key leads when that is
				// a declared scope; a command leading nowhere and a declared block keep it; else unknown
				inner = Context{Entry: here.Entry, Block: below.Block.Value}
				inner.InTrigger = here.InTrigger || inner.Block == schema.BlockTriggers
				_, leads := sch.Scopes[out]
				switch {
				case here.Key != nil && here.Key.Scope.From != schema.SourceNone:
					inner.Scope = here.Key.Scope.Value
				case leads:
					inner.Scope = out
				case command != nil && command.Kind&schema.KindLink == 0,
					here.Key != nil && here.Key.Block.From != schema.SourceNone:
					inner.Scope = here.Scope
				}

				// the key's words: a link form, else a call resolved by CalledIn: in a triggers or effects
				// block unless a stated field takes it (D-24), in an undeclared one with FormCommand (D-29)
				word := Called(here.Block, under)
				if !linked(key, assign.Key.Range, here) && command == nil && !data && named(key) && word != "" &&
					(here.Block == schema.BlockUnknown || here.Key == nil) {
					refer(Ref{Called: word, Name: key, Range: assign.Key.Range}, here)
					calls = key
				}
			}

			// the value's word: a link form, a name a command creates (its own word, or the key of its
			// block NameCommands lists), a word the position's declared value names, or an event id
			scalar, isScalar := value.(*jomini.Scalar)
			if (isEntry || under != nil) && isScalar && named(scalar.Text) && !linked(scalar.Text, scalar.Range, here) {
				claim, made := schema.Value{}, ""
				if here.Key != nil {
					claim = here.Key.Value.Value
				}
				if isAssign {
					made = Made(enclosing, assign.Key.Text, here.Command != nil)
				}
				word := Ref{Name: scalar.Text, Range: scalar.Range}
				switch {
				case made != "":
					define(made, scalar.Text, "", scalar.Range)
				case claim.Type == schema.WordReference:
					word.Database = claim.Database
				case claim.Type == schema.WordLoc:
					word.Database = Localization
				case claim.Type == schema.WordFile:
					word.Database = File
				case jomini.IsEventID(scalar.Text) && jomini.Literal(scalar.Text) != jomini.LitNumber:
					word.Database = events
				}
				if word.Database != "" {
					refer(word, here)
				}
			}

			// a $NAME$ in its key or value defines a parameter of its entry (D-31)
			if isAssign {
				parameters(&assign.Key, here)
			}
			if isScalar {
				parameters(scalar, here)
			}

			// a command or link key, or a value word read through a link, in a known scope (D-34)
			if here.Scope != "" && isAssign && here.Command != nil {
				records.scoped[Pair(assign.Key.Text, here.Scope)] = true
			}
			if here.Scope != "" && isScalar && sch.Link(strings.ToLower(scalar.Text)) != nil {
				records.scoped[Pair(scalar.Text, here.Scope)] = true
			}

			// a scope that evaluates no triggers and executes no effects leaves its block unknown (D-34)
			if scope := sch.Scopes[inner.Scope]; !scope.Triggers && !scope.Effects {
				inner.Scope = ""
			}

			// how its own block reads, for the visit; a block nothing declares reads at no position
			here.InnerKey, here.InnerBlock, here.InnerScope, here.InnerTrigger = below, inner.Block, inner.Scope, inner.InTrigger
			if below == nothing {
				here.InnerKey = nil
			}
			if visit != nil && !visit(here) {
				stopped = true
				return
			}
			if block := jomini.BlockOf(value); block != nil {
				walk(block.Statements, below, inner, inside, calls)
			}
		}
	}
	walk(top, nil, Context{Entry: -1}, "", "")
	return records
}

// Called is the CalledIn word a key written in a block of this kind calls a name by (D-24):
// trigger, effect, or "*" in a block nothing declares where vanilla writes commands (D-29).
func Called(block schema.Block, under *schema.Key) string {
	switch {
	case block == schema.BlockTriggers:
		return "trigger"
	case block == schema.BlockEffects:
		return "effect"
	case block == schema.BlockUnknown && under != nil && under.Seen.Forms&schema.FormCommand != 0:
		return "*"
	}
	return ""
}

// Pair is a written command, link or link-read word and the scope it is read in, as Index.Scoped
// keeps them: by its first part's name before any colon, lower-case (scope:x.liege is scope).
func Pair(written, scope string) [2]string {
	first, _, _ := strings.Cut(strings.ToLower(written), ".")
	name, _, _ := strings.Cut(first, ":")
	return [2]string{name, scope}
}

// ScriptMade reports a database of names script makes: one game.NameCommands names (scope, var).
func ScriptMade(database string) bool {
	return slices.Contains(slices.Collect(maps.Values(game.NameCommands)), database)
}

// Prefixed is the database the names a link prefix reads belong to: a script-made name's (read as
// scope:x and var:x, ScriptMade), else the one it names (D-15).
func Prefixed(sch *schema.Schema, prefix string) string {
	if ScriptMade(prefix) {
		return prefix
	}
	return sch.Named(prefix)
}

// Made is the database a word written as a key's value is a new name in (game.NameCommands): the
// key's own when it reads as a command, else its own under the command whose block it is in.
func Made(enclosing, key string, command bool) string {
	made := game.NameCommands[enclosing+"/"+key]
	if command {
		made = cmp.Or(game.NameCommands[strings.ToLower(key)], made)
	}
	return made
}
