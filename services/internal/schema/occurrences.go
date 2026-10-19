// occurrences.go is the occurrence rules: which entry each word of a statement names at its context, and whether
// the statement defines it.

package schema

import (
	"path"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/jomini"
	"paradox-modding-tools/services/internal/loc"
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

// Parameter is the database of a scripted entry's $NAME$ parameters, each named <entry>:<NAME> where defined
// and where passed.
const Parameter = "parameter"

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
	position   *Position      // the position that types the word: the key's own, a listed word's block's
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
		occurrences = walk.callOccurrences(occurrences, scalar, context.Block)
	}
	occurrences = walk.tagCall(occurrences, inner, statement)
	for i := range occurrences {
		occurrences[i].Entry, occurrences[i].InTrigger = context.Entry, inTrigger
	}
	return occurrences
}

// callOccurrences adds, for each data call a value holds ([GetPlayer.GetName]), a reference to the function every
// declared step names, as Type.Function, and to those the calls in its arguments name; a first step no type declares
// that is written with arguments may be a macro, so it refers to the functions database's entry of its name. The
// steps read in the data type the block's datacontext leads to, where it has one.
func (walk *fileWalk) callOccurrences(occurrences []Occurrence, scalar *jomini.Scalar, block *jomini.Block) []Occurrence {
	if !strings.Contains(scalar.Text, "[") {
		return occurrences
	}
	start := scalar.TextStart()
	dataType := walk.dataContext(block, scalar)
	for _, call := range jomini.DataCalls(scalar.Text, start) {
		occurrences = walk.stepOccurrences(occurrences, call.Steps, dataType)
	}
	return occurrences
}

// stepOccurrences adds what each step of a data call names, read from the data type dataType (""'s global
// functions): a function of the type the steps before it lead to, as Type.Function; a data type written first names
// nothing itself; the first unknown step ends the call.
func (walk *fileWalk) stepOccurrences(occurrences []Occurrence, steps []jomini.CallStep, dataType string) []Occurrence {
	for i, step := range steps {
		owner, _, isFunction := walk.schema.CallStep(steps, i, dataType)
		_, isType := walk.schema.DataTypes[step.Name]
		switch {
		case owner == "":
			return occurrences
		case isFunction:
			occurrences = append(occurrences, Occurrence{Role: Refers, Database: game.DataFunction, ID: owner + "." + step.Name, Range: step.Range})
		case i == 0 && isType:
		case i == 0 && len(step.Args) > 0 && walk.schema.Functions != "":
			occurrences = append(occurrences, Occurrence{Role: Refers, Database: walk.schema.Functions, ID: step.Name, Range: step.Range})
		default:
			return occurrences
		}
		for _, arg := range step.Args {
			if !arg.Quoted {
				occurrences = walk.stepOccurrences(occurrences, jomini.ParseCall(arg.Text, arg.Range.Start), dataType)
			}
		}
	}
	return occurrences
}

// dataContext is the data type the datacontext a block writes leads to, read from the global functions; "" where the
// block writes none, or it is the value held (a datacontext reads outside its own).
func (walk *fileWalk) dataContext(block *jomini.Block, held *jomini.Scalar) string {
	key := walk.fileType.ContextKey
	if block == nil || key == "" {
		return ""
	}
	for _, field := range jomini.Fields(block) {
		scalar, isScalar := field.Value.(*jomini.Scalar)
		if !isScalar || scalar == held || field.Key.Text != key {
			continue
		}
		start := scalar.TextStart()
		if calls := jomini.DataCalls(scalar.Text, start); len(calls) > 0 {
			return walk.schema.CallLeadsTo(calls[0].Steps, "")
		}
	}
	return ""
}

// tagCall adds a call, by the file type's TagCalls word, of the tag a statement that defines an entry writes its
// block with (type x = button { }): the kind the entry is based on.
func (walk *fileWalk) tagCall(occurrences []Occurrence, inner Context, statement jomini.Statement) []Occurrence {
	assign, isAssign := statement.(*jomini.Assignment)
	if !isAssign || !inner.Defines || walk.fileType.TagCalls == "" {
		return occurrences
	}
	if tagged, isTagged := assign.Value.(*jomini.TaggedBlock); isTagged {
		occurrences = append(occurrences, Occurrence{Role: Refers, Caller: walk.fileType.TagCalls, ID: tagged.Tag.Text, Range: tagged.Tag.Range})
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
		facts.isEntry, facts.entryID = inner.Defines, entry.ID
		facts.database = walk.schema.Databases[entry.Database]
		facts.atEntry = facts.database != nil && context.At == facts.database.Entry
	}
	_, constant := jomini.Constant(scalar.Text)
	facts.named = scalar.Text != "" && !constant && len(jomini.Parameters(scalar.Text)) == 0
	inEntry := context.Database != nil
	facts.reads = isKey && !facts.isEntry && inEntry || !isKey && (facts.isEntry || inEntry) && facts.named
	if facts.position = ValuePosition(statement, context, inner); facts.position != nil {
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
	if facts.isEntry && facts.database != nil && facts.database.Descriptor.Of != "" {
		occurrences = append(occurrences, Occurrence{Role: Refers, Database: facts.database.Descriptor.Of, ID: facts.entryID, Range: scalar.Range})
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
	ref, isRef := walk.fileRef(facts.key, scalar.Text)
	switch {
	case facts.isEntry && facts.database != nil && facts.database.Descriptor.Of != "":
		occurrence.Database = facts.database.Descriptor.Of
	case isRef:
		occurrence.Database, occurrence.Caller, occurrence.ID = ref.Database, ref.Caller, ref.ID
	case facts.makes && facts.stored != "":
		occurrence.Role, occurrence.Database = Defines, facts.stored
	case facts.key != nil && facts.database != nil && facts.atEntry && slices.Contains(facts.database.Descriptor.Names, facts.key.Text):
		occurrence.Role, occurrence.IsName, occurrence.Database = Defines, true, facts.database.Name
	case facts.stored != "":
		occurrence.Database = facts.stored
	case facts.claim.Type == ValueReference && (facts.position.Value.Source != SourceVanilla || facts.literal != jomini.LitNumber):
		occurrence.Database = facts.claim.Database
	case facts.claim.Type == ValueLoc:
		occurrence.Database = game.Localization
	case facts.claim.Type == ValueFile:
		occurrence.Database = game.File
	case facts.literal != jomini.LitNumber && facts.literal != jomini.LitDate && facts.position != nil &&
		(facts.claim.Type == ValueNumber || facts.position.Vanilla.Writes&WritesNumber != 0):
		occurrence.Caller = game.Shapes[game.ShapeValue].Caller
	case jomini.IsEventID(scalar.Text):
		occurrence.Database = walk.schema.Events
	default:
		return occurrences
	}
	if occurrence.Database != "" || occurrence.Caller != "" {
		occurrences = append(occurrences, occurrence)
	}
	return occurrences
}

// fileRef is what a value names where the file type has a Ref row for the statement's key: an occurrence with
// the database and ID of an entry, a file under the row's folder, or a localization key, or with the ID and the
// calling word of a call the key makes; false where it names nothing, which a text is where it is no key or holds a
// data call.
func (walk *fileWalk) fileRef(key *jomini.Scalar, value string) (ref Occurrence, isRef bool) {
	if key == nil {
		return ref, false
	}
	for _, row := range game.FileTypes[walk.ext].Refs {
		if row.At != key.Text {
			continue
		}
		switch {
		case row.Folder != "":
			return Occurrence{Database: game.File, ID: path.Join(row.Folder, value)}, true
		case row.Database == "":
			return Occurrence{Caller: key.Text, ID: value}, true
		case row.Database == game.Localization && (!loc.LooksLikeKey(value) || len(jomini.Calls(value, 0)) > 0):
			return ref, false
		}
		return Occurrence{Database: row.Database, ID: value}, true
	}
	return ref, false
}

// parameterDefinitions defines each $NAME$ an entry of a folder with Parameters writes, at its
// first use in the file.
func (walk *fileWalk) parameterDefinitions(occurrences []Occurrence, scalar *jomini.Scalar, facts scalarFacts) []Occurrence {
	if facts.database == nil || !facts.database.Descriptor.Parameters {
		return occurrences
	}
	start := scalar.TextStart()
	for _, span := range jomini.Parameters(scalar.Text) {
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
