// names.go says what a written word names: a key by every key rule that fits, a value by the first
// value rule that fits, each `prefix:name` part a link reads, and each `$NAME$` an entry of a
// folder with Parameters writes. A constant (@x) and a word holding a parameter ($X$) name nothing.

package schema

import (
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/jomini"
)

// Localization and File are the databases a loc key and a file path name; no definition carries
// them, so the index resolves them from the loc entries and the inventories. Parameter holds a
// scripted entry's $NAME$ parameters, each named <entry>:<NAME> where defined and where passed.
const (
	Localization = "localization"
	File         = "file"
	Parameter    = "parameter"
)

// wordFacts is what every name rule asks of a word: the statement it is in, whether that statement
// is an entry's own, the entry's database, and whether the word is read as a name at all.
type wordFacts struct {
	key      *jomini.Scalar // the statement's key; nil for a value statement
	isEntry  bool           // the statement is an entry's own
	isKey    bool           // the word is the key, not the value
	named    bool           // the word is a name: not "", not a constant, no $X$ in it
	reads    bool           // it may refer: a key in an entry, not the entry's own; a value in or at one, a name
	database *Database      // the entry's database; nil outside an entry
	claim    Expected       // the position's expected value; zero without a position
	literal  jomini.LiteralKind
}

// names fills a reading's NameDatabase and Names: the key's names, then the value's.
func (w *fileWalk) names(r *Reading) {
	assign, isAssign := r.Statement.(*jomini.Assignment)
	command := w.nameCommand(r, assign)
	r.NameDatabase = command.Database
	if isAssign {
		w.wordNames(r, &assign.Key, w.facts(r, assign, &assign.Key, true), command)
	}
	if scalar, isScalar := jomini.ValueOf(r.Statement).(*jomini.Scalar); isScalar {
		w.wordNames(r, scalar, w.facts(r, assign, scalar, false), command)
	}
}

// nameCommand returns the name command a statement's value belongs to: the key's own (set_variable
// = x), else the key inside the command whose block it is in (save_scope_value_as = { name = x });
// zero for none.
func (w *fileWalk) nameCommand(r *Reading, assign *jomini.Assignment) game.NameCommand {
	if assign == nil {
		return game.NameCommand{}
	}
	if own, found := game.NameCommands[strings.ToLower(assign.Key.Text)]; found {
		return own
	}
	return game.NameCommands[r.BlockContext.Command+"/"+assign.Key.Text]
}

// facts gathers what the rules ask of one word of a reading.
func (w *fileWalk) facts(r *Reading, assign *jomini.Assignment, word *jomini.Scalar, isKey bool) wordFacts {
	f := wordFacts{isKey: isKey, literal: jomini.Literal(word.Text)}
	if assign != nil {
		f.key = &assign.Key
	}
	if r.Entry >= 0 {
		f.isEntry = w.entries[r.Entry].Statement == r.Statement
		f.database = w.s.Databases[w.entries[r.Entry].Database]
	}
	_, constant := jomini.Constant(word.Text)
	f.named = word.Text != "" && !constant && len(jomini.Interpolations(word.Text)) == 0
	f.reads = isKey && !f.isEntry && r.InEntry || !isKey && (f.isEntry || r.InEntry) && f.named
	if r.Position != nil {
		f.claim = r.Position.Value.Value
	}
	return f
}

// wordNames adds what one word names: its link parts, then the key rules or the value rule, then
// its parameters.
func (w *fileWalk) wordNames(r *Reading, word *jomini.Scalar, f wordFacts, command game.NameCommand) {
	linked := w.prefixReferences(r, word, f)
	switch {
	case f.isKey:
		w.keyNames(r, word, f)
	case f.reads && !linked && (!f.isEntry || f.key != nil):
		// a listed entry's word is its name, not a value
		w.valueName(r, word, f, command)
	}
	w.parameterDefinitions(r, word, f)
}

// prefixReferences refers, for each prefix:name part of a word that a link reads, to the name in
// the prefix's database, and reports whether the word has any such part: a linked word takes no
// value rule.
func (w *fileWalk) prefixReferences(r *Reading, word *jomini.Scalar, f wordFacts) (linked bool) {
	if !f.reads {
		return false
	}
	for part := range strings.SplitSeq(word.Text, ".") {
		prefixed, isPrefixed := jomini.SplitPrefixed(part)
		if !isPrefixed || w.s.Link(strings.ToLower(part)) == nil {
			continue
		}
		linked = true
		if database := w.s.Prefixes[strings.ToLower(prefixed.Prefix)]; database != "" {
			r.Names = append(r.Names, Naming{Database: database, Name: prefixed.Name, Range: word.Range})
		}
	}
	return linked
}

// keyNames adds what a key names by every key rule that fits, in this order: a history entry's top
// key refers to the object it sets up; a call every_x iterates the scripted list x; a key read as a
// call names a scripted entry by its calling word; a key in a call's block names the called entry's
// parameter.
func (w *fileWalk) keyNames(r *Reading, word *jomini.Scalar, f wordFacts) {
	if f.isEntry && f.database != nil && f.database.Row.Of != "" {
		r.Names = append(r.Names, Naming{Database: f.database.Row.Of, Name: w.entries[r.Entry].Name, Range: word.Range})
	}
	if f.reads && r.Listed != "" {
		r.Names = append(r.Names, Naming{
			Database: w.s.Lists, Name: strings.TrimPrefix(word.Text, r.Listed), Range: word.Range,
		})
	}
	if f.reads && r.Calls != "" && f.named {
		r.Names = append(r.Names, Naming{Called: r.Calls, Name: word.Text, Range: word.Range})
	}
	if f.reads && r.Call != "" {
		r.Names = append(r.Names, Naming{Database: Parameter, Name: r.Call + ":" + word.Text, Range: word.Range})
	}
}

// valueName adds what a value names by the first value rule that fits, in this order: history, file
// reference, made, group, read, claim, value, event. A history key names its object before anything
// else; a loc key is referred to only where the position's claim says so; vanilla types a position
// by its words, never its numbers; a word where a number is expected names a script value.
func (w *fileWalk) valueName(r *Reading, word *jomini.Scalar, f wordFacts, command game.NameCommand) {
	n := Naming{Name: word.Text, Range: word.Range}
	fileRef := ""
	if f.key != nil {
		fileRef = game.FileTypes[w.ext].Refs[f.key.Text]
	}
	switch {
	case f.isEntry && f.database != nil && f.database.Row.Of != "":
		n.Database = f.database.Row.Of
	case fileRef != "":
		n.Database = w.ext[1:] + "." + fileRef
	case command.Makes && r.NameDatabase != "":
		n.Defines, n.Database = true, r.NameDatabase
	case f.key != nil && f.database != nil && r.BlockContext.Position == f.database.Entry &&
		slices.Contains(f.database.Row.Names, f.key.Text):
		n.Defines, n.Named, n.Database = true, true, f.database.Name
	case r.NameDatabase != "":
		n.Database = r.NameDatabase
	case f.claim.Type == WordReference && (r.Position.Value.Source != SourceVanilla || f.literal != jomini.LitNumber):
		n.Database = f.claim.Database
	case f.claim.Type == WordLoc:
		n.Database = Localization
	case f.claim.Type == WordFile:
		n.Database = File
	case f.literal != jomini.LitNumber && f.literal != jomini.LitDate && r.Position != nil &&
		(f.claim.Type == WordNumber || r.Position.Evidence.Forms&FormNumber != 0):
		n.Called = game.Blocks[game.BlockValue].Calls
	case jomini.IsEventID(word.Text):
		n.Database = w.events
	default:
		return
	}
	if n.Database != "" || n.Called != "" {
		r.Names = append(r.Names, n)
	}
}

// parameterDefinitions defines each $NAME$ an entry of a folder with Parameters writes, at its
// first use in the file.
func (w *fileWalk) parameterDefinitions(r *Reading, word *jomini.Scalar, f wordFacts) {
	if f.database == nil || !f.database.Row.Parameters {
		return
	}
	start := word.Range.Start
	if word.Quoted {
		start++
	}
	for _, span := range jomini.Interpolations(word.Text) {
		name := w.entries[r.Entry].Name + ":" + span.Name
		if !w.defined[name] {
			w.defined[name] = true
			r.Names = append(r.Names, Naming{
				Defines: true, Database: Parameter, Name: name,
				Range: jomini.Range{Start: start + span.Start, End: start + span.End},
			})
		}
	}
}
