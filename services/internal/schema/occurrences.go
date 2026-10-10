// occurrences.go is the occurrence rules: which entry each word of a statement names at its context, and whether
// the statement defines it.

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
// (Local) and whether an entry's Names key gives it (IsName) rather than an entry of its own. Entry is the
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
	IsName    bool
}

// Localization and File are the databases a loc key and a file path name; no definition carries
// them, so the index resolves them from the loc entries and the inventories. Parameter holds a
// scripted entry's $NAME$ parameters, each named <entry>:<NAME> where defined and where passed.
const (
	Localization = "localization"
	File         = "file"
	Parameter    = "parameter"
)

// scalarFacts is what every occurrence rule asks of a word: the statement's key, whether the statement is an
// entry's own and the entry's database and ID, whether the word is read as a name at all, the key's own position
// and its expected value, and how the statement was read: as a call by the holder's caller, iterating a list,
// in a block that calls an entry, at the entry's own block, naming a name command's database.
type scalarFacts struct {
	key        *jomini.Scalar // the statement's key; nil for a value statement
	isEntry    bool           // the statement is an entry's own
	isKey      bool           // the word is the key, not the value
	named      bool           // the word is a name: not "", not a constant, no $X$ in it
	reads      bool           // it may refer: a key in an entry, not the entry's own; a value in or at one, a name
	database   *Database      // the entry's database; nil outside an entry
	entryID    string         // the entry's ID
	position   *Position      // the key's own position; nil where its block reads where the key stands
	claim      Expected       // that position's expected value; zero without one
	literal    jomini.LiteralKind
	call       bool   // the key is read as a call
	caller     string // the word the holder's calls resolve by
	holderCall string // the scripted entry the holder's block calls; "" none
	list       string // the scripted list the key iterates; "" none
	atEntry    bool   // the statement is written in the entry's own block
	stored     string // the database a name command's value names here; "" none
	makes      bool   // that name command makes the name (set_variable), rather than reading one
}

// occurrencesOf is what a statement's words name at its context: the key's occurrences, then the value's, each in
// the statement's entry and trigger state. A trigger is exactly a trigger, not one that is also an effect.
func (walk *fileWalk) occurrencesOf(context, inner Context, statement jomini.Statement) (occurrences []Occurrence) {
	assign, isAssign := statement.(*jomini.Assignment)
	inTrigger := context.InTrigger
	if isAssign {
		occurrences = walk.scalarOccurrences(occurrences, &assign.Key, walk.facts(context, inner, statement, &assign.Key, true))
		command := walk.schema.Commands[strings.ToLower(assign.Key.Text)]
		inTrigger = inTrigger || inner.Rule == game.KeyRuleCommand && command != nil && command.Kinds == game.KindTrigger
	}
	if scalar, isScalar := jomini.ValueOf(statement).(*jomini.Scalar); isScalar {
		occurrences = walk.scalarOccurrences(occurrences, scalar, walk.facts(context, inner, statement, scalar, false))
	}
	for i := range occurrences {
		occurrences[i].Entry, occurrences[i].InTrigger = context.Entry, inTrigger
	}
	return occurrences
}

// StoredIn is the database a name command's value names at a statement (set_variable = x: var); "" none.
func StoredIn(context Context, statement jomini.Statement) string {
	assign, isAssign := statement.(*jomini.Assignment)
	if !isAssign {
		return ""
	}
	return nameCommand(context, assign).Database
}

// nameCommand returns the name command a statement's value belongs to: the key's own (set_variable
// = x), else the key inside the command whose block it is in (save_scope_value_as = { name = x });
// zero for none.
func nameCommand(context Context, assign *jomini.Assignment) game.NameCommand {
	if own, named := game.NameCommands[strings.ToLower(assign.Key.Text)]; named {
		return own
	}
	return game.NameCommands[context.Command+"/"+assign.Key.Text]
}

// facts gathers what the rules ask of one word of a statement.
func (walk *fileWalk) facts(context, inner Context, statement jomini.Statement, scalar *jomini.Scalar, isKey bool) scalarFacts {
	facts := scalarFacts{
		isKey: isKey, literal: jomini.Literal(scalar.Text), caller: context.Caller, holderCall: context.Call, list: inner.List,
	}
	if assign, isAssign := statement.(*jomini.Assignment); isAssign {
		facts.key = &assign.Key
		facts.call = inner.Call == assign.Key.Text && inner.Rule != game.KeyRuleNone
		command := nameCommand(context, assign)
		facts.stored, facts.makes = command.Database, command.Makes
	}
	if context.Entry >= 0 {
		entry := walk.entries[context.Entry]
		facts.isEntry, facts.entryID = entry.Statement == statement, entry.ID
		facts.database = walk.schema.Databases[entry.Database]
		facts.atEntry = facts.database != nil && context.At == facts.database.Entry
	}
	_, constant := jomini.Constant(scalar.Text)
	facts.named = scalar.Text != "" && !constant && len(jomini.Interpolations(scalar.Text)) == 0
	inEntry := context.Database != nil
	facts.reads = isKey && !facts.isEntry && inEntry || !isKey && (facts.isEntry || inEntry) && facts.named
	if facts.position = inner.Position(); facts.position != nil {
		facts.claim = facts.position.Value.Value
	}
	return facts
}

// scalarOccurrences adds what one word names: its link parts, then the key rules or the value rule, then
// its parameters.
func (walk *fileWalk) scalarOccurrences(occurrences []Occurrence, scalar *jomini.Scalar, facts scalarFacts) []Occurrence {
	occurrences, linked := walk.prefixReferences(occurrences, scalar, facts)
	switch {
	case facts.isKey:
		occurrences = walk.keyOccurrences(occurrences, scalar, facts)
	case facts.reads && !linked && (!facts.isEntry || facts.key != nil):
		// a listed entry's word is its name, not a value
		occurrences = walk.valueOccurrence(occurrences, scalar, facts)
	}
	return walk.parameterDefinitions(occurrences, scalar, facts)
}

// prefixReferences refers, for each prefix:name part of a word that a link reads, to the name in
// the prefix's database, and reports whether the word has any such part: a linked word takes no
// value rule.
func (walk *fileWalk) prefixReferences(occurrences []Occurrence, scalar *jomini.Scalar, facts scalarFacts) ([]Occurrence, bool) {
	if !facts.reads {
		return occurrences, false
	}
	linked := false
	for part := range strings.SplitSeq(scalar.Text, ".") {
		prefixed, isPrefixed := jomini.SplitPrefixed(part)
		if !isPrefixed || walk.schema.Link(strings.ToLower(part)) == nil {
			continue
		}
		linked = true
		if database := walk.schema.Prefixes[strings.ToLower(prefixed.Prefix)]; database != "" {
			occurrences = append(occurrences, Occurrence{Role: Refers, Database: database, ID: prefixed.Name, Range: scalar.Range})
		}
	}
	return occurrences, linked
}

// keyOccurrences adds what a key names by every key rule that fits, in this order: a history entry's top
// key refers to the object it sets up; a call every_x iterates the scripted list x; a key read as a
// call names a scripted entry by its calling word; a key in a call's block names the called entry's
// parameter.
func (walk *fileWalk) keyOccurrences(occurrences []Occurrence, scalar *jomini.Scalar, facts scalarFacts) []Occurrence {
	if facts.isEntry && facts.database != nil && facts.database.Row.Of != "" {
		occurrences = append(occurrences, Occurrence{Role: Refers, Database: facts.database.Row.Of, ID: facts.entryID, Range: scalar.Range})
	}
	if facts.reads && facts.list != "" {
		occurrences = append(occurrences, Occurrence{Role: Refers, Database: walk.schema.Lists, ID: facts.list, Range: scalar.Range})
	}
	if facts.reads && facts.call && facts.named {
		occurrences = append(occurrences, Occurrence{Role: Refers, Caller: facts.caller, ID: scalar.Text, Range: scalar.Range})
	}
	if facts.reads && facts.holderCall != "" {
		occurrences = append(occurrences, Occurrence{Role: Refers, Database: Parameter, ID: facts.holderCall + ":" + scalar.Text, Range: scalar.Range})
	}
	return occurrences
}

// valueOccurrence adds what a value names by the first value rule that fits, in this order: history, file
// reference, made, group, read, claim, value, event. A history key names its object before anything
// else; a loc key is referred to only where the position's claim says so; vanilla types a position
// by its words, never its numbers; a word where a number is expected names a script value.
func (walk *fileWalk) valueOccurrence(occurrences []Occurrence, scalar *jomini.Scalar, facts scalarFacts) []Occurrence {
	occurrence := Occurrence{Role: Refers, ID: scalar.Text, Range: scalar.Range}
	fileRef := ""
	if facts.key != nil {
		fileRef = game.FileTypes[walk.ext].Refs[facts.key.Text]
	}
	switch {
	case facts.isEntry && facts.database != nil && facts.database.Row.Of != "":
		occurrence.Database = facts.database.Row.Of
	case fileRef != "":
		occurrence.Database = walk.ext[1:] + "." + fileRef
	case facts.makes && facts.stored != "":
		occurrence.Role, occurrence.Database = Defines, facts.stored
	case facts.key != nil && facts.database != nil && facts.atEntry && slices.Contains(facts.database.Row.Names, facts.key.Text):
		occurrence.Role, occurrence.IsName, occurrence.Database = Defines, true, facts.database.Name
	case facts.stored != "":
		occurrence.Database = facts.stored
	case facts.claim.Type == ValueReference && (facts.position.Value.Source != SourceVanilla || facts.literal != jomini.LitNumber):
		occurrence.Database = facts.claim.Database
	case facts.claim.Type == ValueLoc:
		occurrence.Database = Localization
	case facts.claim.Type == ValueFile:
		occurrence.Database = File
	case facts.literal != jomini.LitNumber && facts.literal != jomini.LitDate && facts.position != nil &&
		(facts.claim.Type == ValueNumber || facts.position.Vanilla.Writes&WritesNumber != 0):
		occurrence.Caller = game.Shapes[game.ShapeValue].Caller
	case jomini.IsEventID(scalar.Text):
		occurrence.Database = walk.events
	default:
		return occurrences
	}
	if occurrence.Database != "" || occurrence.Caller != "" {
		occurrences = append(occurrences, occurrence)
	}
	return occurrences
}

// parameterDefinitions defines each $NAME$ an entry of a folder with Parameters writes, at its
// first use in the file.
func (walk *fileWalk) parameterDefinitions(occurrences []Occurrence, scalar *jomini.Scalar, facts scalarFacts) []Occurrence {
	if facts.database == nil || !facts.database.Row.Parameters {
		return occurrences
	}
	start := scalar.Range.Start
	if scalar.Quoted {
		start++
	}
	for _, span := range jomini.Interpolations(scalar.Text) {
		name := facts.entryID + ":" + span.Name
		if !walk.defined[name] {
			walk.defined[name] = true
			occurrences = append(occurrences, Occurrence{
				Role: Defines, Database: Parameter, ID: name,
				Range: jomini.Range{Start: start + span.Start, End: start + span.End},
			})
		}
	}
	return occurrences
}
