// occurrences.go decides which entry each scalar of a placed statement stands for, and whether the
// statement creates it: the occurrence rules.

package schema

import (
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/jomini"
)

// Role is what an occurrence does: defines a name, refers to one, or localizes one.
type Role uint8

// The roles.
const (
	Defines Role = iota
	Refers
	Localizes
)

// Occurrence is one occurrence of a name in a file, as the walk produces it: the name's database and ID
// (a call has Caller set and Database ""), where it is written (the line and column are filled by the
// index's Collect), the entry it is in, and for a definition its mode word, whether it is declared inline
// (Local) and whether an entry's Names key gives it (Named) rather than an entry of its own. Entry is the
// position, in the file's occurrences, of the definition of the entry it is in; -1 none; an entry's definition
// points at itself. The index stores it with its strings interned.
type Occurrence struct {
	Role      Role
	Database  string
	ID        string
	Caller    string
	Range     jomini.Range
	Line, Col int32
	Entry     int32
	Mode      string
	InTrigger bool
	Local     bool
	Named     bool
}

// Localization and File are the databases a loc key and a file path name; no definition carries
// them, so the index resolves them from the loc entries and the inventories. Parameter holds a
// scripted entry's $NAME$ parameters, each named <entry>:<NAME> where defined and where passed.
const (
	Localization = "localization"
	File         = "file"
	Parameter    = "parameter"
)

// scalarFacts is what every name rule asks of a word: the statement it is in, whether that statement
// is an entry's own, the entry's database, and whether the word is read as a name at all.
type scalarFacts struct {
	key      *jomini.Scalar // the statement's key; nil for a value statement
	isEntry  bool           // the statement is an entry's own
	isKey    bool           // the word is the key, not the value
	named    bool           // the word is a name: not "", not a constant, no $X$ in it
	reads    bool           // it may refer: a key in an entry, not the entry's own; a value in or at one, a name
	database *Database      // the entry's database; nil outside an entry
	claim    Expected       // the position's expected value; zero without a position
	literal  jomini.LiteralKind
}

// occurrencesOf fills a reading's StoredIn and Found: the key's occurrences, then the value's, each in the
// reading's entry and trigger state.
func (walk *fileWalk) occurrencesOf(placement *Placement) {
	assign, isAssign := placement.Statement.(*jomini.Assignment)
	command := nameCommand(placement, assign)
	placement.StoredIn = command.Database
	if isAssign {
		walk.scalarOccurrences(placement, &assign.Key, walk.facts(placement, assign, &assign.Key, true), command)
	}
	if scalar, isScalar := jomini.ValueOf(placement.Statement).(*jomini.Scalar); isScalar {
		walk.scalarOccurrences(placement, scalar, walk.facts(placement, assign, scalar, false), command)
	}
	for i := range placement.Found {
		placement.Found[i].Entry, placement.Found[i].InTrigger = int32(placement.Entry), placement.InTrigger
	}
}

// nameCommand returns the name command a statement's value belongs to: the key's own (set_variable
// = x), else the key inside the command whose block it is in (save_scope_value_as = { name = x });
// zero for none.
func nameCommand(placement *Placement, assign *jomini.Assignment) game.NameCommand {
	if assign == nil {
		return game.NameCommand{}
	}
	if own, found := game.NameCommands[strings.ToLower(assign.Key.Text)]; found {
		return own
	}
	return game.NameCommands[placement.BlockContext.Command+"/"+assign.Key.Text]
}

// facts gathers what the rules ask of one word of a reading.
func (walk *fileWalk) facts(placement *Placement, assign *jomini.Assignment, scalar *jomini.Scalar, isKey bool) scalarFacts {
	f := scalarFacts{isKey: isKey, literal: jomini.Literal(scalar.Text)}
	if assign != nil {
		f.key = &assign.Key
	}
	if placement.Entry >= 0 {
		f.isEntry = walk.entries[placement.Entry].Statement == placement.Statement
		f.database = walk.schema.Databases[walk.entries[placement.Entry].Database]
	}
	_, constant := jomini.Constant(scalar.Text)
	f.named = scalar.Text != "" && !constant && len(jomini.Interpolations(scalar.Text)) == 0
	f.reads = isKey && !f.isEntry && placement.InEntry || !isKey && (f.isEntry || placement.InEntry) && f.named
	if placement.Position != nil {
		f.claim = placement.Position.Value.Value
	}
	return f
}

// scalarOccurrences adds what one word names: its link parts, then the key rules or the value rule, then
// its parameters.
func (walk *fileWalk) scalarOccurrences(placement *Placement, scalar *jomini.Scalar, f scalarFacts, command game.NameCommand) {
	linked := walk.prefixReferences(placement, scalar, f)
	switch {
	case f.isKey:
		walk.keyOccurrences(placement, scalar, f)
	case f.reads && !linked && (!f.isEntry || f.key != nil):
		// a listed entry's word is its name, not a value
		walk.valueOccurrence(placement, scalar, f, command)
	}
	walk.parameterDefinitions(placement, scalar, f)
}

// prefixReferences refers, for each prefix:name part of a word that a link reads, to the name in
// the prefix's database, and reports whether the word has any such part: a linked word takes no
// value rule.
func (walk *fileWalk) prefixReferences(placement *Placement, scalar *jomini.Scalar, f scalarFacts) (linked bool) {
	if !f.reads {
		return false
	}
	for part := range strings.SplitSeq(scalar.Text, ".") {
		prefixed, isPrefixed := jomini.SplitPrefixed(part)
		if !isPrefixed || walk.schema.Link(strings.ToLower(part)) == nil {
			continue
		}
		linked = true
		if database := walk.schema.Prefixes[strings.ToLower(prefixed.Prefix)]; database != "" {
			placement.Found = append(placement.Found, Occurrence{Role: Refers, Database: database, ID: prefixed.Name, Range: scalar.Range})
		}
	}
	return linked
}

// keyOccurrences adds what a key names by every key rule that fits, in this order: a history entry's top
// key refers to the object it sets up; a call every_x iterates the scripted list x; a key read as a
// call names a scripted entry by its calling word; a key in a call's block names the called entry's
// parameter.
func (walk *fileWalk) keyOccurrences(placement *Placement, scalar *jomini.Scalar, f scalarFacts) {
	if f.isEntry && f.database != nil && f.database.Row.Of != "" {
		placement.Found = append(placement.Found, Occurrence{Role: Refers, Database: f.database.Row.Of, ID: walk.entries[placement.Entry].ID, Range: scalar.Range})
	}
	if f.reads && placement.Listed != "" {
		placement.Found = append(placement.Found, Occurrence{
			Role: Refers, Database: walk.schema.Lists, ID: strings.TrimPrefix(scalar.Text, placement.Listed), Range: scalar.Range,
		})
	}
	if f.reads && placement.Calls != "" && f.named {
		placement.Found = append(placement.Found, Occurrence{Role: Refers, Caller: placement.Calls, ID: scalar.Text, Range: scalar.Range})
	}
	if f.reads && placement.Call != "" {
		placement.Found = append(placement.Found, Occurrence{Role: Refers, Database: Parameter, ID: placement.Call + ":" + scalar.Text, Range: scalar.Range})
	}
}

// valueOccurrence adds what a value names by the first value rule that fits, in this order: history, file
// reference, made, group, read, claim, value, event. A history key names its object before anything
// else; a loc key is referred to only where the position's claim says so; vanilla types a position
// by its words, never its numbers; a word where a number is expected names a script value.
func (walk *fileWalk) valueOccurrence(placement *Placement, scalar *jomini.Scalar, f scalarFacts, command game.NameCommand) {
	n := Occurrence{Role: Refers, ID: scalar.Text, Range: scalar.Range}
	fileRef := ""
	if f.key != nil {
		fileRef = game.FileTypes[walk.ext].Refs[f.key.Text]
	}
	switch {
	case f.isEntry && f.database != nil && f.database.Row.Of != "":
		n.Database = f.database.Row.Of
	case fileRef != "":
		n.Database = walk.ext[1:] + "." + fileRef
	case command.Makes && placement.StoredIn != "":
		n.Role, n.Database = Defines, placement.StoredIn
	case f.key != nil && f.database != nil && placement.BlockContext.Position == f.database.Entry &&
		slices.Contains(f.database.Row.Names, f.key.Text):
		n.Role, n.Named, n.Database = Defines, true, f.database.Name
	case placement.StoredIn != "":
		n.Database = placement.StoredIn
	case f.claim.Kind == ExpectsReference && (placement.Position.Value.Source != SourceVanilla || f.literal != jomini.LitNumber):
		n.Database = f.claim.Database
	case f.claim.Kind == ExpectsLoc:
		n.Database = Localization
	case f.claim.Kind == ExpectsFile:
		n.Database = File
	case f.literal != jomini.LitNumber && f.literal != jomini.LitDate && placement.Position != nil &&
		(f.claim.Kind == ExpectsNumber || placement.Position.Vanilla.Shapes&ShapeNumber != 0):
		n.Caller = game.Blocks[game.BlockValue].Calls
	case jomini.IsEventID(scalar.Text):
		n.Database = walk.events
	default:
		return
	}
	if n.Database != "" || n.Caller != "" {
		placement.Found = append(placement.Found, n)
	}
}

// parameterDefinitions defines each $NAME$ an entry of a folder with Parameters writes, at its
// first use in the file.
func (walk *fileWalk) parameterDefinitions(placement *Placement, scalar *jomini.Scalar, f scalarFacts) {
	if f.database == nil || !f.database.Row.Parameters {
		return
	}
	start := scalar.Range.Start
	if scalar.Quoted {
		start++
	}
	for _, span := range jomini.Interpolations(scalar.Text) {
		name := walk.entries[placement.Entry].ID + ":" + span.Name
		if !walk.defined[name] {
			walk.defined[name] = true
			placement.Found = append(placement.Found, Occurrence{
				Role: Defines, Database: Parameter, ID: name,
				Range: jomini.Range{Start: start + span.Start, End: start + span.End},
			})
		}
	}
}
