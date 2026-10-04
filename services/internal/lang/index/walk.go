// walk.go turns a file into records: the walker reads its tree, and the name rules (game.KeyNames,
// game.ValueNames) say what each word defines or refers to. Each step is handed on as a Context.

package index

import (
	"cmp"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/schema"
	"paradox-modding-tools/services/internal/lang/source"
)

// Context is a step as the index reads it.
type Context struct {
	schema.Step
	Entry int32  // the entry it is in, as an index into the Records' Defs; -1 outside one
	Names string // the database its value names by a name command (set_variable); "" none
}

// Walk reads a file with the schema: its records, and each statement's context handed to visit
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

	// define records a definition in this file (Index.Add gives it shared copies of its strings)
	define := func(def Def) {
		def.Path = file.Path
		def.Line, def.Col = position(def.Range.Start)
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
		define(Def{Database: entry.Database, Name: entry.Name, Mode: entry.Mode, Range: span, Local: entry.Local})
	})

	// a word can name something unless it is a constant (@x) or holds a parameter ($X$)
	named := func(word string) bool {
		_, constant := jomini.Constant(word)
		return word != "" && !constant && len(jomini.Interpolations(word)) == 0
	}
	refer := func(ref Ref, at Context) {
		ref.Path, ref.From, ref.InTrigger = file.Path, at.Entry, at.InTrigger
		ref.Line, ref.Col = position(ref.Range.Start)
		records.Refs = append(records.Refs, ref)
	}
	events := cmp.Or(sch.Flagged(func(row game.Folder) bool { return row.Events })...)

	// each written block's entry, so a step knows the entry it is in
	stopped, entries := false, map[*jomini.Block]int32{}
	reader := schema.Reader{
		Entry: func(statement jomini.Statement) *schema.Database {
			if index, isEntry := own[statement]; isEntry {
				return sch.Databases[records.Defs[index].Database]
			}
			return nil
		},
		Visit: func(step *schema.Step) bool {
			if stopped {
				return false
			}
			here, isEntry := Context{Step: *step, Entry: -1}, false
			if entry, known := entries[step.Holder]; known {
				here.Entry = entry
			}
			if entry, known := own[step.Statement]; known {
				here.Entry, isEntry = entry, true
			}
			var database *schema.Database
			if here.Entry >= 0 {
				database = sch.Databases[records.Defs[here.Entry].Database]
			}

			// the database its value names, by a name command: the key's own (set_variable), or the key's
			// inside the command whose block it is in (save_scope_value_as = { name = x })
			assign, isAssign := step.Statement.(*jomini.Assignment)
			value, naming := jomini.ValueOf(step.Statement), game.NameCommand{}
			if isAssign {
				naming = game.NameCommands[step.Place.Command+"/"+assign.Key.Text]
				if own, found := game.NameCommands[strings.ToLower(assign.Key.Text)]; found {
					naming = own
				}
			}
			made := ""
			if here.Names = naming.Database; naming.Makes {
				made = here.Names
			}

			// what each word names: the key by every key rule that fits, the value by the first value
			// rule; a link prefix (scope:x) and a $NAME$ parameter always
			scalar, isScalar := value.(*jomini.Scalar)
			var words []*jomini.Scalar
			if isAssign {
				words = append(words, &assign.Key)
			}
			if isScalar {
				words = append(words, scalar)
			}
			for i, word := range words {
				isKey := isAssign && i == 0
				reads := isKey && !isEntry && step.InEntry || !isKey && (isEntry || step.InEntry) && named(word.Text)

				linked := false
				for part := range strings.SplitSeq(word.Text, ".") {
					prefixed, isPrefixed := jomini.SplitPrefixed(part)
					if !reads || !isPrefixed || sch.Link(strings.ToLower(part)) == nil {
						continue
					}
					linked = true
					if database := sch.Prefixes[strings.ToLower(prefixed.Prefix)]; database != "" {
						refer(Ref{Database: database, Name: prefixed.Name, Range: word.Range}, here)
					}
				}

				claim := schema.Value{}
				if step.Key != nil {
					claim = step.Key.Value.Value
				}
				if isKey {
					for _, rule := range game.KeyNames {
						switch {
						case rule == game.NameHistory && isEntry && database != nil && database.Naming.History &&
							len(database.Naming.Of) == 1:
							refer(Ref{Database: database.Naming.Of[0], Name: records.Defs[here.Entry].Name, Range: word.Range}, here)
						case rule == game.NameList && reads && step.Listed != "":
							refer(Ref{Database: sch.Lists, Name: strings.TrimPrefix(word.Text, step.Listed), Range: word.Range}, here)
						case rule == game.NameCall && reads && step.Calls != "" && named(word.Text):
							refer(Ref{Called: step.Calls, Name: word.Text, Range: word.Range}, here)
						case rule == game.NameParameter && reads && step.Place.Call != "":
							refer(Ref{Database: Parameter, Name: step.Place.Call + ":" + word.Text, Range: word.Range}, here)
						}
					}
				}
			values:
				for _, rule := range game.ValueNames {
					if isKey || !reads || linked || isEntry && !isAssign { // a listed entry's word is its name
						break
					}
					ref := Ref{Name: word.Text, Range: word.Range}
					switch {
					case rule == game.NameMade && made != "":
						define(Def{Database: made, Name: word.Text, Range: word.Range})
					case rule == game.NameGroup && isAssign && database != nil && step.Place.Key == database.Entry &&
						slices.Contains(database.Naming.Names, assign.Key.Text):
						define(Def{Database: database.Name, Name: word.Text, Range: word.Range, Named: true})
					case rule == game.NameRead && here.Names != "":
						ref.Database = here.Names
					case rule == game.NameClaim && claim.Type == schema.WordReference &&
						(step.Key.Value.From != schema.SourceVanilla || jomini.Literal(word.Text) != jomini.LitNumber):
						ref.Database = claim.Database // vanilla types a position by its words, never its numbers
					case rule == game.NameClaim && claim.Type == schema.WordLoc:
						ref.Database = Localization
					case rule == game.NameClaim && claim.Type == schema.WordFile:
						ref.Database = File
					case rule == game.NameValue && jomini.Literal(word.Text) != jomini.LitNumber && step.Key != nil &&
						(claim.Type == schema.WordNumber || step.Key.Seen.Forms&schema.FormNumber != 0):
						ref.Called = game.Blocks[game.BlockValue].Calls
					case rule == game.NameEvent && jomini.IsEventID(word.Text) && jomini.Literal(word.Text) != jomini.LitNumber:
						ref.Database = events
					default:
						continue
					}
					if ref.Database != "" || ref.Called != "" {
						refer(ref, here)
					}
					break values
				}

				// each $NAME$ in an entry of a folder with Parameters defines that entry's parameter
				if database == nil || !database.Naming.Parameters {
					continue
				}
				start := word.Range.Start
				if word.Quoted {
					start++
				}
				for _, span := range jomini.Interpolations(word.Text) {
					written := jomini.Range{Start: start + span.Start, End: start + span.End}
					define(Def{Database: Parameter, Name: records.Defs[here.Entry].Name + ":" + span.Name, Range: written})
				}
			}

			// the scope each command, link or link-read word is written in, where it is known
			if here.Scope != "" && isAssign && here.Command != nil {
				records.scoped[Pair(assign.Key.Text, here.Scope)] = true
			}
			if here.Scope != "" && isScalar && sch.Link(strings.ToLower(scalar.Text)) != nil {
				records.scoped[Pair(scalar.Text, here.Scope)] = true
			}

			if visit != nil && !visit(here) {
				stopped = true
				return false
			}
			if block := jomini.BlockOf(value); block != nil {
				entries[block] = here.Entry
			}
			return true
		},
	}
	sch.Read(reader, nil, top, schema.Place{})
	return records
}

// Pair is a written command, link or link-read word and the scope it is read in, as Index.Scoped
// keeps them: by its first part's name before any colon, lower-case (scope:x.liege is scope).
func Pair(written, scope string) [2]string {
	first, _, _ := strings.Cut(strings.ToLower(written), ".")
	name, _, _ := strings.Cut(first, ":")
	return [2]string{name, scope}
}
