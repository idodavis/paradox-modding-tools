// walk.go turns a file into records: the walker reads its tree, and the name rules (game.KeyNames,
// game.ValueNames) say what each word defines or refers to. Each step is handed on as a Context.

package index

import (
	"cmp"
	"path"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/inventory"
	"paradox-modding-tools/services/internal/jomini"
	"paradox-modding-tools/services/internal/schema"
)

// Context is a step as the index reads it.
type Context struct {
	schema.Reading
	Entry int32  // the entry it is in, as an index into the Records' Defs; -1 outside one
	Names string // the database its value names by a name command (set_variable); "" none
}

// Walk reads a file with the schema: its records, and each statement's context handed to visit
// (nil for none); visit returning false stops the walk.
func Walk(s *schema.Schema, file inventory.File, parsed *jomini.Result, visit func(Context) bool) *Records {
	walker := &walker{
		schema: s, file: file, records: &Records{scoped: map[[2]string]bool{}}, visit: visit,
		own: map[jomini.Statement]int32{}, entries: map[*jomini.Block]int32{},
		events: cmp.Or(s.DatabasesWhere(func(row game.Folder) bool { return row.Events })...),
		ext:    strings.ToLower(path.Ext(file.Rel)),
	}

	// the entry detector's definitions, so an entry's index is its place in Defs; a whole file's
	// entry holds the root's statements as one block; a table or sound list (parsed nil) has no tree
	var top []jomini.Statement
	if parsed != nil {
		walker.lines, top = parsed.Lines(), parsed.Root.Statements
	}
	s.FindEntries(file, parsed, func(entry schema.Entry) {
		span := entry.Statement.NodeRange()
		if assign, isAssign := entry.Statement.(*jomini.Assignment); isAssign {
			span = assign.Key.Range
		}
		if parsed != nil && entry.Statement.NodeRange() == parsed.Root.Range {
			top = []jomini.Statement{entry.Statement}
		}
		walker.own[entry.Statement] = int32(len(walker.records.Defs))
		walker.define(Def{Database: entry.Database, Name: entry.Name, Mode: entry.Mode, Range: span, Local: entry.Local})
	})
	reader := schema.Visitor{Visit: walker.step, Entry: func(statement jomini.Statement) *schema.Database {
		if index, isEntry := walker.own[statement]; isEntry {
			return s.Databases[walker.records.Defs[index].Database]
		}
		return nil
	}}
	s.WalkBlock(reader, nil, top, schema.BlockContext{})
	return walker.records
}

// walker is one file's walk: its records, the entry each entry statement and written block is in,
// and the visitor each step is handed to.
type walker struct {
	schema  *schema.Schema
	file    inventory.File
	records *Records
	lines   *jomini.LineIndex
	own     map[jomini.Statement]int32 // an entry's statement: its index in Defs
	entries map[*jomini.Block]int32    // a written block: the entry it is in
	events  string                     // the database an event id names
	ext     string                     // the file's extension, lower-case
	visit   func(Context) bool
	stopped bool
}

// position is where an offset is, from the parse's lines; a table or sound list is read for them.
func (walker *walker) position(offset int) (int32, int32) {
	if walker.lines == nil {
		text, _ := walker.file.Text()
		walker.lines = jomini.NewLineIndex(text)
	}
	at := walker.lines.PositionAt(offset)
	return int32(at.Line), int32(at.Character)
}

// define records a definition in this file (Index.Add gives it shared copies of its strings).
func (walker *walker) define(def Def) {
	def.Path = walker.file.Path
	def.Line, def.Col = walker.position(def.Range.Start)
	walker.records.Defs = append(walker.records.Defs, def)
}

// refer records a reference written at a step.
func (walker *walker) refer(ref Ref, at Context) {
	ref.Path, ref.From, ref.InTrigger = walker.file.Path, at.Entry, at.InTrigger
	ref.Line, ref.Col = walker.position(ref.Range.Start)
	walker.records.Refs = append(walker.records.Refs, ref)
}

// step reads one statement: the entry it is in, what its key and value name, and the scope its
// command or link is written in; then it is handed to the visitor.
func (walker *walker) step(step *schema.Reading) bool {
	if walker.stopped {
		return false
	}
	here := Context{Reading: *step, Entry: -1}
	if entry, known := walker.entries[step.Holder]; known {
		here.Entry = entry
	}
	if entry, known := walker.own[step.Statement]; known {
		here.Entry = entry
	}

	// the database its value names, by a name command: the key's own (set_variable), or the key's
	// inside the command whose block it is in (save_scope_value_as = { name = x })
	assign, isAssign := step.Statement.(*jomini.Assignment)
	value, naming := jomini.ValueOf(step.Statement), game.NameCommand{}
	if isAssign {
		naming = game.NameCommands[step.BlockContext.Command+"/"+assign.Key.Text]
		if own, found := game.NameCommands[strings.ToLower(assign.Key.Text)]; found {
			naming = own
		}
	}
	here.Names = naming.Database
	scalar, isScalar := value.(*jomini.Scalar)
	if isAssign {
		walker.word(here, &assign.Key, true, naming.Makes)
	}
	if isScalar {
		walker.word(here, scalar, false, naming.Makes)
	}

	// the scope each command, link or link-read word is written in, where it is known
	if here.Scope != "" && isAssign && here.Command != nil {
		walker.records.scoped[Pair(assign.Key.Text, here.Scope)] = true
	}
	if here.Scope != "" && isScalar && walker.schema.Link(strings.ToLower(scalar.Text)) != nil {
		walker.records.scoped[Pair(scalar.Text, here.Scope)] = true
	}

	if walker.visit != nil && !walker.visit(here) {
		walker.stopped = true
		return false
	}
	if block := jomini.BlockOf(value); block != nil {
		walker.entries[block] = here.Entry
	}
	return true
}

// word records what a written word names: a link prefix (scope:x) always, a key by every key rule
// that fits, a value by the first value rule, and each $NAME$ an entry of a folder with Parameters
// writes. A constant (@x) or a word holding a parameter ($X$) names nothing.
func (walker *walker) word(here Context, word *jomini.Scalar, isKey, makes bool) {
	s, step := walker.schema, here.Reading
	_, isEntry := walker.own[step.Statement]
	assign, isAssign := step.Statement.(*jomini.Assignment)
	var database *schema.Database
	if here.Entry >= 0 {
		database = s.Databases[walker.records.Defs[here.Entry].Database]
	}
	_, constant := jomini.Constant(word.Text)
	named := word.Text != "" && !constant && len(jomini.Interpolations(word.Text)) == 0
	reads := isKey && !isEntry && step.InEntry || !isKey && (isEntry || step.InEntry) && named

	linked := false
	for part := range strings.SplitSeq(word.Text, ".") {
		prefixed, isPrefixed := jomini.SplitPrefixed(part)
		if !reads || !isPrefixed || s.Link(strings.ToLower(part)) == nil {
			continue
		}
		linked = true
		if database := s.Prefixes[strings.ToLower(prefixed.Prefix)]; database != "" {
			walker.refer(Ref{Database: database, Name: prefixed.Name, Range: word.Range}, here)
		}
	}

	claim, literal := schema.Expected{}, jomini.Literal(word.Text)
	if step.Position != nil {
		claim = step.Position.Value.Value
	}
	if isKey {
		for _, rule := range game.KeyNames {
			switch {
			case rule == game.NameHistory && isEntry && database != nil && database.Row.Of != "":
				walker.refer(Ref{Database: database.Row.Of, Name: walker.records.Defs[here.Entry].Name, Range: word.Range}, here)
			case rule == game.NameList && reads && step.Listed != "":
				walker.refer(Ref{Database: s.Lists, Name: strings.TrimPrefix(word.Text, step.Listed), Range: word.Range}, here)
			case rule == game.NameCall && reads && step.Calls != "" && named:
				walker.refer(Ref{Called: step.Calls, Name: word.Text, Range: word.Range}, here)
			case rule == game.NameParameter && reads && step.Call != "":
				walker.refer(Ref{Database: Parameter, Name: step.Call + ":" + word.Text, Range: word.Range}, here)
			}
		}
	}
values:
	for _, rule := range game.ValueNames {
		// a listed entry's word is its name, not a value
		if isKey || !reads || linked || isEntry && !isAssign {
			break
		}
		ref := Ref{Name: word.Text, Range: word.Range}
		switch {
		case rule == game.NameHistory && isEntry && database != nil && database.Row.Of != "":
			ref.Database = database.Row.Of // a history entry written key = value (province_mapping)
		case rule == game.NameFileRef && isAssign && game.FileTypes[walker.ext].Refs[assign.Key.Text] != "":
			ref.Database = walker.ext[1:] + "." + game.FileTypes[walker.ext].Refs[assign.Key.Text]
		case rule == game.NameMade && makes && here.Names != "":
			walker.define(Def{Database: here.Names, Name: word.Text, Range: word.Range})
		case rule == game.NameGroup && isAssign && database != nil && step.BlockContext.Position == database.Entry &&
			slices.Contains(database.Row.Names, assign.Key.Text):
			walker.define(Def{Database: database.Name, Name: word.Text, Range: word.Range, Named: true})
		case rule == game.NameRead && here.Names != "":
			ref.Database = here.Names
		case rule == game.NameClaim && claim.Type == schema.WordReference &&
			(step.Position.Value.Source != schema.SourceVanilla || literal != jomini.LitNumber):
			ref.Database = claim.Database // vanilla types a position by its words, never its numbers
		case rule == game.NameClaim && claim.Type == schema.WordLoc:
			ref.Database = Localization
		case rule == game.NameClaim && claim.Type == schema.WordFile:
			ref.Database = File
		case rule == game.NameValue && literal != jomini.LitNumber && literal != jomini.LitDate && step.Position != nil &&
			(claim.Type == schema.WordNumber || step.Position.Evidence.Forms&schema.FormNumber != 0):
			ref.Called = game.Blocks[game.BlockValue].Calls
		case rule == game.NameEvent && jomini.IsEventID(word.Text):
			ref.Database = walker.events
		default:
			continue
		}
		if ref.Database != "" || ref.Called != "" {
			walker.refer(ref, here)
		}
		break values
	}

	// an entry of a folder with Parameters defines each $NAME$ it writes, at its first use
	if database == nil || !database.Row.Parameters {
		return
	}
	start := word.Range.Start
	if word.Quoted {
		start++
	}
	for _, span := range jomini.Interpolations(word.Text) {
		name := walker.records.Defs[here.Entry].Name + ":" + span.Name
		if !slices.ContainsFunc(walker.records.Defs, func(def Def) bool { return def.Database == Parameter && def.Name == name }) {
			walker.define(Def{Database: Parameter, Name: name, Range: jomini.Range{Start: start + span.Start, End: start + span.End}})
		}
	}
}

// Pair is a written command, link or link-read word and the scope it is read in, as Index.Scoped
// keeps them: by its first part's name before any colon, lower-case (scope:x.liege is scope).
func Pair(written, scope string) [2]string {
	first, _, _ := strings.Cut(strings.ToLower(written), ".")
	name, _, _ := strings.Cut(first, ":")
	return [2]string{name, scope}
}
