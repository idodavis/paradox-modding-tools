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

// Occurrence is one occurrence of a name in a file, as the walk produces it.
type Occurrence struct {
	Role      Role         // defines it, refers to it, or localizes it
	Database  string       // the database the name is in; "" for a call
	ID        string       // the name
	Caller    string       // a call's calling word; "" otherwise
	Range     jomini.Range // where it is written
	Line      int32        // filled by the index's Collect
	Col       int32        // filled by the index's Collect
	Entry     int32        // the file's occurrence that defines the entry it is in; -1 none
	Mode      string       // a definition's entry mode (INJECT, REPLACE); "" none
	InTrigger bool         // written inside a trigger
	Local     bool         // a definition declared inline, answering only in its file
	IsName    bool         // given by a key that lists names of the entry's database, not an entry of its own
}

// Parameter is the database of a scripted entry's $NAME$ parameters, each named <entry>:<NAME> where defined
// and where passed.
const Parameter = "parameter"

// scalarFacts is what every occurrence rule asks of a word.
type scalarFacts struct {
	key        *jomini.Scalar     // the statement's key; nil for a value statement
	isEntry    bool               // the statement is an entry's own
	isKey      bool               // the word is the key, not the value
	named      bool               // the word is a name: not "", not a constant, no $X$ in it
	reads      bool               // it may refer: a key in an entry, not the entry's own; a value in or at one, a name
	database   *Database          // the entry's database; nil outside an entry
	entryID    string             // the entry's ID
	position   *Position          // the position that types the word: the key's own, a listed word's block's
	expected   Expected           // that position's expected value; zero without one
	literal    jomini.LiteralKind // how the word is written: number, date, text...
	call       bool               // the key is read as a call
	caller     string             // the word the holder's calls resolve by
	holderCall string             // the scripted entry the holder's block calls; "" none
	list       string             // the scripted list the key iterates; "" none
	atEntry    bool               // the statement is written in the entry's own block
	stored     string             // the database a scope-or-variable command's value names here; "" none
	makes      bool               // that scope-or-variable command makes the name (set_variable), rather than reading one
}

// occurrencesOf returns what a statement's words name at its context: the key's occurrences, then the value's, each in
// the statement's entry and trigger state (a trigger is exactly a trigger, not also an effect).
func (walk *fileWalk) occurrencesOf(context, inner Context, statement jomini.Statement) (occurrences []Occurrence) {
	assign, isAssign := statement.(*jomini.Assignment)
	inTrigger := context.InTrigger
	if isAssign {
		occurrences = walk.scalarOccurrences(occurrences, &assign.Key, walk.facts(context, inner, statement, &assign.Key, true))
		command := walk.schema.Commands[strings.ToLower(assign.Key.Text)]
		inTrigger = inTrigger || inner.KeyRule == game.KeyRuleCommand && command != nil && isDeclaredOnly(command, game.DeclaredTrigger)
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

// callOccurrences adds a reference to each function the steps of a data call name ([GetPlayer.GetName], as
// Type.Function) and its arguments' calls name; an undeclared first step with arguments may be a macro.
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

// stepOccurrences adds what each step of a data call names, read on a data type ("" the global functions); the first
// unknown step ends the call.
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

// dataContext returns the data type the datacontext a block writes leads to; "" where it writes none, or it is the
// value held (a datacontext reads outside its own).
func (walk *fileWalk) dataContext(block *jomini.Block, held *jomini.Scalar) string {
	key := walk.fileRule.ContextKey
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
	if !isAssign || !inner.Defines || walk.fileRule.TagCalls == "" {
		return occurrences
	}
	if tagged, isTagged := assign.Value.(*jomini.TaggedBlock); isTagged {
		occurrences = append(occurrences, Occurrence{Role: Refers, Caller: walk.fileRule.TagCalls, ID: tagged.Tag.Text, Range: tagged.Tag.Range})
	}
	return occurrences
}

// StoredIn returns the database a scope-or-variable command's value names at a statement (set_variable = x: var); ""
// none.
func StoredIn(context Context, statement jomini.Statement) string {
	assign, isAssign := statement.(*jomini.Assignment)
	if !isAssign {
		return ""
	}
	return scopeVarCommand(context, assign).Database
}

// scopeVarCommand returns the scope-or-variable command a statement's value belongs to: the key's own (set_variable
// = x), else the key inside the command's block (save_scope_value_as = { name = x }); zero for none.
func scopeVarCommand(context Context, assign *jomini.Assignment) game.ScopeVarCommand {
	if own, named := game.ScopeVarCommands[strings.ToLower(assign.Key.Text)]; named {
		return own
	}
	return game.ScopeVarCommands[context.Command+"/"+assign.Key.Text]
}

// facts gathers what the rules ask of one word of a statement.
func (walk *fileWalk) facts(context, inner Context, statement jomini.Statement, scalar *jomini.Scalar, isKey bool) scalarFacts {
	facts := scalarFacts{
		isKey: isKey, literal: jomini.Literal(scalar.Text), caller: context.Caller, holderCall: context.Call, list: inner.List,
	}
	if assign, isAssign := statement.(*jomini.Assignment); isAssign {
		facts.key = &assign.Key
		facts.call = inner.Call == assign.Key.Text && inner.KeyRule != game.KeyRuleNone
		command := scopeVarCommand(context, assign)
		facts.stored, facts.makes = command.Database, command.Makes
	}
	if context.Entry >= 0 {
		entry := walk.entries[context.Entry]
		facts.isEntry, facts.entryID = inner.Defines, entry.ID
		facts.database = walk.schema.Databases[entry.Database]
		facts.atEntry = facts.database != nil && context.BlockPosition == facts.database.Entry
	}
	_, constant := jomini.Constant(scalar.Text)
	facts.named = scalar.Text != "" && !constant && len(jomini.Parameters(scalar.Text)) == 0
	inEntry := context.Database != nil
	facts.reads = isKey && !facts.isEntry && inEntry || !isKey && (facts.isEntry || inEntry) && facts.named
	if facts.position = ValuePosition(statement, context, inner); facts.position != nil {
		facts.expected = facts.position.Value.Value
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

// prefixReferences refers to the name in each prefix:name part of a word that a link reads, and reports whether there
// was one: a linked word takes no value rule.
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

// keyOccurrences adds what a key names: the object a history entry's top key sets up, the scripted list a call
// every_x iterates, the scripted entry a call names, and the parameter a key in a call's block passes.
func (walk *fileWalk) keyOccurrences(occurrences []Occurrence, scalar *jomini.Scalar, facts scalarFacts) []Occurrence {
	if facts.isEntry && facts.database != nil && facts.database.Rule.Target != "" {
		occurrences = append(occurrences, Occurrence{Role: Refers, Database: facts.database.Rule.Target, ID: facts.entryID, Range: scalar.Range})
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

// valueOccurrence adds what a value names, by the first rule that fits: a history key's object, a file reference, a
// name script makes or reads, a group, the claim, a script value, an event; a loc key only where the claim says so.
func (walk *fileWalk) valueOccurrence(occurrences []Occurrence, scalar *jomini.Scalar, facts scalarFacts) []Occurrence {
	occurrence := Occurrence{Role: Refers, ID: scalar.Text, Range: scalar.Range}
	ref, isRef := walk.fileRef(facts.key, scalar.Text)
	switch {
	case facts.isEntry && facts.database != nil && facts.database.Rule.Target != "":
		occurrence.Database = facts.database.Rule.Target
	case isRef:
		occurrence.Database, occurrence.Caller, occurrence.ID = ref.Database, ref.Caller, ref.ID
	case facts.makes && facts.stored != "":
		occurrence.Role, occurrence.Database = Defines, facts.stored
	case facts.key != nil && facts.database != nil && facts.atEntry && slices.Contains(facts.database.Rule.AliasKeys, facts.key.Text):
		occurrence.Role, occurrence.IsName, occurrence.Database = Defines, true, facts.database.Name
	case facts.stored != "":
		occurrence.Database = facts.stored
	case facts.expected.Type == ValueReference && (facts.position.Value.Source != game.SourceVanilla || facts.literal != jomini.LitNumber):
		occurrence.Database = facts.expected.Database
	case facts.expected.Type == ValueLoc:
		occurrence.Database = game.Localization
	case facts.expected.Type == ValueFile:
		occurrence.Database = game.File
	case facts.literal != jomini.LitNumber && facts.literal != jomini.LitDate && facts.position != nil &&
		(facts.expected.Type == ValueNumber || facts.position.Vanilla.Writes&WritesNumber != 0):
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

// fileRef returns what a value names where the file type has a Reference for the statement's key: an entry, a file
// under the reference's folder, a localization key, or a call by the key's word; false where it names nothing.
func (walk *fileWalk) fileRef(key *jomini.Scalar, value string) (ref Occurrence, isRef bool) {
	if key == nil {
		return ref, false
	}
	for _, reference := range walk.fileRule.References {
		if reference.KeyPath != key.Text {
			continue
		}
		switch {
		case reference.Folder != "":
			return Occurrence{Database: game.File, ID: path.Join(reference.Folder, value)}, true
		case reference.Database == "":
			return Occurrence{Caller: key.Text, ID: value}, true
		case reference.Database == game.Localization && (!loc.LooksLikeKey(value) || len(jomini.Calls(value, 0)) > 0):
			return ref, false
		}
		return Occurrence{Database: reference.Database, ID: value}, true
	}
	return ref, false
}

// parameterDefinitions defines each $NAME$ an entry of a folder with TakesParameters writes, at its
// first use in the file.
func (walk *fileWalk) parameterDefinitions(occurrences []Occurrence, scalar *jomini.Scalar, facts scalarFacts) []Occurrence {
	if facts.database == nil || !facts.database.Rule.TakesParameters {
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
