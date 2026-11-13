// scripted.go reads what files declare for script to call by name: scripted
// definitions with their parameters, modifier types and event namespaces. Of
// several copies of one name it keeps the one the game keeps.

package lang

import (
	"cmp"
	"maps"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/jomini"
)

// declarer is what one file declares for script and GUI files to call by
// name.
type declarer struct {
	schema *Schema

	copies []ScriptedCopy

	// widgets are the widget types the file defines, and the types those are
	// built on.
	widgets []string

	// declared are the modifier types and event namespaces the file declares.
	declared []*Definition

	// bodyLast is the statement after the block of the scripted definition
	// being read, whose copy is the last of copies. It is 0 outside one.
	bodyLast int32
}

// modSequence is the place of a mod's first declaring file among the files
// that declare scripted definitions, after every file of the game.
const modSequence = 1 << 30

// SetScripted reads again what the given mod files declare, and forgets what
// they declared before. A file that cannot be parsed, is gone, or is one the
// game does not read (unread) declares nothing. The copy the game keeps of
// each name is picked again only where a declaring file changed or the
// unread files are not the same.
func (s *Schema) SetScripted(changed []files.File, unread map[string]bool, parse func(path string) (*jomini.Tree, error)) {
	declaring := slices.DeleteFunc(slices.Clone(changed), func(file files.File) bool {
		return file.Format != files.FormatScript || !s.Declares(file)
	})
	declarers := s.listDeclarers(declaring, parse)
	next := &modScripted{byFile: map[string]*declarer{}, unread: unread}

	previous := s.mods.Load()
	stale := previous == nil || !maps.Equal(unread, previous.unread)
	if previous != nil {
		next.byFile, next.sequence, next.callees = maps.Clone(previous.byFile), previous.sequence, previous.callees
	}

	for _, file := range changed {
		stale = stale || next.byFile[file.Key] != nil && len(next.byFile[file.Key].copies) > 0
		delete(next.byFile, file.Key)
	}

	for position, declarer := range declarers {
		if declarer != nil {
			next.sequence++
			declarer.place(declaring[position], modSequence+next.sequence)
			next.byFile[declaring[position].Key] = declarer
			stale = stale || len(declarer.copies) > 0
		}
	}

	if stale {
		next.callees = s.resolveModded(next)
	}

	next.winners = maps.Clone(next.callees)
	for key, declarer := range next.byFile {
		if unread[key] {
			continue
		}

		for _, declared := range declarer.declared {
			next.winners[declared.Database+":"+declared.Key] = declared
		}
	}

	s.mods.Store(next)
}

// resolveModded returns the copy the game keeps of each scripted name the
// workspace changes, by database and name: a name a mod declares, or one the
// game declares in a file it does not read. A name left with no copy has
// nil. A mod's copy never replaces an effect, a trigger or a link the dumps
// declare.
func (s *Schema) resolveModded(mods *modScripted) map[string]*Definition {
	groups := map[string][]ScriptedCopy{}
	for key, copies := range s.ScriptedCopies {
		if slices.ContainsFunc(copies, func(scripted ScriptedCopy) bool { return mods.unread[scripted.File] }) {
			groups[key] = nil
		}
	}

	for _, file := range slices.Sorted(maps.Keys(mods.byFile)) {
		if mods.unread[file] {
			continue
		}

		for _, scripted := range mods.byFile[file].copies {
			if s.FindCommand(scripted.Copy.Name) != nil {
				continue
			}

			// the copy gets saves and parameters of its own, since what its
			// calls pass on is added to them
			definition := *scripted.Definition
			definition.Saves, definition.Field = maps.Clone(definition.Saves), cloneParameters(definition.Field)
			scripted.Definition = &definition
			key := s.toScriptedKey(scripted.Copy)
			groups[key] = append(groups[key], scripted)
		}
	}

	winners := make(map[string]*Definition, len(groups))
	var modded []ScriptedCopy
	for key, group := range groups {
		for _, scripted := range s.ScriptedCopies[key] {
			if !mods.unread[scripted.File] {
				group = append(group, scripted)
			}
		}

		winner, ok := s.findScriptedWinner(group)
		winners[key] = winner.Definition
		if ok && winner.Sequence >= modSequence {
			modded = append(modded, winner)
		}
	}

	s.TypeParameters(modded)
	s.ResolveRelays(modded, winners)

	return winners
}

// findScriptedWinner returns the copy the game keeps among the copies of one
// scripted name. It returns false if the game keeps none.
func (s *Schema) findScriptedWinner(copies []ScriptedCopy) (ScriptedCopy, bool) {
	if len(copies) == 0 {
		return ScriptedCopy{}, false
	}

	precedence := Precedence{}
	if database := s.Databases[copies[0].Copy.Database]; database != nil {
		precedence = database.Precedence
	}

	ordered := slices.SortedStableFunc(slices.Values(copies), func(first, second ScriptedCopy) int {
		return compareCopies(first, second, precedence)
	})
	modes, priorities := make([]string, len(ordered)), make([]int, len(ordered))
	for position, scripted := range ordered {
		modes[position], priorities[position] = scripted.Copy.Mode, scripted.Copy.Priority
	}

	winner, ok := precedence.FindWinner(modes, priorities)
	if !ok {
		return ScriptedCopy{}, false
	}

	return ordered[winner.Copy], true
}

// compareCopies orders two copies the way the game reads them: by the paths
// of their files, then the order of the files, then their place in the file.
// Where the playset decides, the paths are left out.
func compareCopies(first, second ScriptedCopy, precedence Precedence) int {
	if !precedence.ByPlayset {
		if order := files.CompareLoadOrder(first.Rel, second.Rel); order != 0 {
			return order
		}
	}

	return cmp.Or(cmp.Compare(first.Sequence, second.Sequence), cmp.Compare(first.Copy.Statement, second.Copy.Statement))
}

// toScriptedKey returns the key a copy is kept under, database:name, with
// the name in lower case where the database ignores case.
func (s *Schema) toScriptedKey(definition Copy) string {
	label := definition.Name
	if database := s.Databases[definition.Database]; database != nil && database.IsCaseless() {
		label = strings.ToLower(label)
	}

	return definition.Database + ":" + label
}

// LookupScripted returns the definition a name has in a database: the mods'
// winner where the given table has the name, else the game's own copy.
func (s *Schema) LookupScripted(winners map[string]*Definition, database, key string) *Definition {
	owner := s.Databases[database]
	if owner == nil {
		return nil
	}

	if owner.IsCaseless() {
		key = strings.ToLower(key)
	}

	if winner, isModded := winners[database+":"+key]; isModded {
		return winner
	}

	return owner.Definitions[key]
}

// lookupCall returns the definition a name calls from one kind of place, as
// a key in an effect block does: the first database that place calls into
// that defines the name.
func (s *Schema) lookupCall(winners map[string]*Definition, caller, key string) *Definition {
	for _, database := range s.Callees[caller] {
		if callee := s.LookupScripted(winners, database, key); callee != nil {
			return callee
		}
	}

	return nil
}

// scriptCallers and guiCallers are the kinds of caller a key in a script file
// and in a GUI file calls from, in the order they are tried.
var (
	scriptCallers = []string{CallEffect, CallTrigger, CallValue}
	guiCallers    = []string{GUIBlockCalls, GUITemplateCalls, GUITagCalls}
)

// findScripted returns the definition a name calls in a file of the given
// format, and the kind of caller that finds it. Script never finds a GUI
// type, nor a GUI file a scripted effect.
func (s *Schema) findScripted(winners map[string]*Definition, key string, format files.Format) (*Definition, string, bool) {
	callers := scriptCallers
	if format == files.FormatGUI {
		callers = guiCallers
	}

	for _, caller := range callers {
		if callee := s.lookupCall(winners, caller, key); callee != nil {
			return callee, caller, true
		}
	}

	return nil, "", false
}

// place gives each copy its file and the file's place among the declaring
// files.
func (d *declarer) place(file files.File, sequence int) {
	for position := range d.copies {
		d.copies[position].File, d.copies[position].Rel, d.copies[position].Sequence = file.Key, file.StageRel(), sequence
	}
}

// Declares reports whether a file may declare something script calls by
// name: a GUI file, or a file of a called database, of the modifier types or
// of the events, which declare namespaces.
func (s *Schema) Declares(file files.File) bool {
	if file.Format == files.FormatGUI {
		return true
	}

	database, ok := s.FindDatabase(file)

	return ok && (len(database.Declaration.Callers) > 0 || database.Declaration.Contents == ContentsModifiers ||
		database.Declaration.Contents == ContentsEvents)
}

// listDeclarers reads what each file declares, several files at a time. A
// file that cannot be parsed gets nil.
func (s *Schema) listDeclarers(declaring []files.File, parse func(path string) (*jomini.Tree, error)) []*declarer {
	declarers := make([]*declarer, len(declaring))
	contexts := make([][]Context, files.Workers)
	EachParsed(declaring, parse, func(worker, index int, tree *jomini.Tree) {
		declarers[index] = &declarer{schema: s}
		contexts[worker] = s.WalkDeclarations(tree, declaring[index], contexts[worker])
		declarers[index].record(tree, contexts[worker])
	})

	return declarers
}

// EachParsed parses the files on several workers and calls a function for
// each parsed file, on the worker that parsed it, with the worker and the
// file's place in the list. It skips a file that cannot be parsed and
// returns those errors.
func EachParsed[T any](listing []files.File, parse func(path string) (T, error), visit func(worker, index int, parsed T)) []error {
	failures := make([]error, len(listing))
	var next atomic.Int64
	var group sync.WaitGroup
	for worker := range files.Workers {
		group.Go(func() {
			for index := int(next.Add(1) - 1); index < len(listing); index = int(next.Add(1) - 1) {
				parsed, err := parse(listing[index].Path)
				if err != nil {
					failures[index] = err
					continue
				}

				visit(worker, index, parsed)
			}
		})
	}

	group.Wait()

	return slices.DeleteFunc(failures, func(err error) bool { return err == nil })
}

// cloneParameters returns a copy of a scripted definition's parameters that
// can change without changing the original.
func cloneParameters(parameters *Field) *Field {
	if parameters == nil {
		return nil
	}

	clone := &Field{Children: make(map[string]*Field, len(parameters.Children))}
	for key, argument := range parameters.Children {
		copied := *argument
		clone.Children[key] = &copied
	}

	return clone
}

// TypeParameters works out what each parameter of the given scripted
// definitions takes from where their bodies write it: one written as
// title:$TITLE$ takes the name of a title.
func (s *Schema) TypeParameters(copies []ScriptedCopy) {
	for _, scripted := range copies {
		s.typeDefinition(scripted.Definition, scripted.typings)
	}
}

// typeDefinition gives each parameter of one scripted definition what the
// places it is written say it takes. A place that names a database wins over
// one that does not, and otherwise the first place wins.
func (s *Schema) typeDefinition(scripted *Definition, typings []typing) {
	for _, typing := range typings {
		argument, claim := scripted.Field.findChild(typing.Parameter), s.findTypingClaim(typing)
		if argument != nil && isBetterClaim(claim, argument.Value) {
			argument.Value = claim
		}
	}
}

// findTypingClaim returns what one place a parameter is written says it
// takes. After a link's colon it is a name of the link's database. As a
// key's value it is what the key takes, firm only where that is a database's
// name: a number or a choice fits the key but need not fit every call.
func (s *Schema) findTypingClaim(typing typing) Claim[Expected] {
	if typing.Field == nil {
		database, _ := s.LookupPrefix(typing.Prefix)
		if database == "" {
			return Claim[Expected]{}
		}

		return Claim[Expected]{Value: Expected{Type: ValueTypeReference, Database: database}, Source: SourceDefinitions}
	}

	claim := typing.Field.Value
	if claim.IsStated() && !claim.Value.namesDatabase() {
		claim.Source = SourceInferred
	}

	return claim
}

// isBetterClaim reports whether a new claim should replace what a parameter
// is known to take: it has no claim yet, or only the new one names a
// database.
func isBetterClaim(claim, current Claim[Expected]) bool {
	if !claim.IsStated() {
		return false
	}

	return !current.IsStated() || claim.Value.namesDatabase() && !current.Value.namesDatabase()
}

// ResolveRelays carries what is known about a parameter back to the callers
// that pass their own parameter on to it: where a scripted effect writes
// other_effect = { TARGET = $WHO$ }, its WHO takes what TARGET takes. It
// repeats until a pass learns nothing new.
func (s *Schema) ResolveRelays(copies []ScriptedCopy, winners map[string]*Definition) {
	for hasNew := true; hasNew; {
		hasNew = false
		for _, scripted := range copies {
			for _, relay := range scripted.relays {
				if callee, _, ok := s.findScripted(winners, relay.Callee, files.FormatScript); ok {
					hasNew = passRelay(scripted.Definition, callee, relay) || hasNew
				}
			}
		}
	}
}

// passRelay gives one parameter what the argument it is passed on as takes,
// and the name the callee saves that argument under, where these are new or
// better. It reports whether it gave anything.
func passRelay(scripted, callee *Definition, relay relay) bool {
	hasNew := false
	_, hasSave := scripted.Saves[relay.Parameter]
	if link, ok := callee.Saves[relay.Argument]; ok && !hasSave {
		scripted.Saves[relay.Parameter] = link
		hasNew = true
	}

	parameter, argument := scripted.Field.findChild(relay.Parameter), callee.Field.findChild(relay.Argument)
	if parameter != nil && argument != nil && isBetterClaim(argument.Value, parameter.Value) {
		parameter.Value = argument.Value
		hasNew = true
	}

	return hasNew
}

// DeclareFiles reads what the game's files declare into the schema, in place
// of what was there. It keeps every copy of each scripted definition, and
// each database's definitions become the copies the game keeps, its modifier
// types and its event namespaces. Widget types are added to those already
// known. It returns the copies the game keeps.
func (s *Schema) DeclareFiles(declaring []files.File, parse func(path string) (*jomini.Tree, error)) []ScriptedCopy {
	s.ScriptedCopies = map[string][]ScriptedCopy{}
	for label, database := range s.Databases {
		if len(database.Declaration.Callers) > 0 || label == s.ModifierTypes || label == EventNamespaces {
			database.Definitions = map[string]*Definition{}
		}
	}

	for position, declarer := range s.listDeclarers(declaring, parse) {
		if declarer == nil {
			continue
		}

		declarer.place(declaring[position], position)
		for _, scripted := range declarer.copies {
			key := s.toScriptedKey(scripted.Copy)
			s.ScriptedCopies[key] = append(s.ScriptedCopies[key], scripted)
		}

		for _, declared := range declarer.declared {
			s.Databases[declared.Database].Definitions[declared.Key] = declared
		}

		s.DeclareWidgets(declarer.widgets)
	}

	var winners []ScriptedCopy
	for key, copies := range s.ScriptedCopies {
		winner, ok := s.findScriptedWinner(copies)
		if !ok {
			continue
		}

		database := s.Databases[winner.Copy.Database]
		database.Definitions[strings.TrimPrefix(key, database.Name+":")] = winner.Definition
		winners = append(winners, winner)
	}

	return winners
}

// DeclareWidgets adds widget types to the ones the schema knows, each a
// definition with no database and no keys.
func (s *Schema) DeclareWidgets(widgets []string) {
	types := s.Databases[GUIWidgetTypes]
	if types.Definitions == nil {
		types.Definitions = map[string]*Definition{}
	}

	for _, widget := range widgets {
		types.Definitions[widget] = &Definition{Key: widget}
	}
}

// record reads what a file declares from its walked contexts: the
// definitions it makes, the event namespaces it declares, and, inside a
// definition that takes parameters, where each parameter is written.
func (d *declarer) record(tree *jomini.Tree, contexts []Context) {
	for position := range contexts {
		statement, context := int32(position), &contexts[position]
		if d.bodyLast <= statement {
			d.bodyLast = 0
		}

		if d.bodyLast > 0 {
			body := &d.copies[len(d.copies)-1]
			body.typings = append(body.typings, listTypings(tree, statement, context)...)

			continue
		}

		if context.Defines && context.Category == CategoryDefinition {
			d.declare(*context.Definition, tree, statement)
		}

		if context.Parent != nil && slices.Contains(context.Parent.Namespaces, statement) {
			namespace := tree.Unquoted(tree.Statements[statement].Value)
			d.declared = append(d.declared, &Definition{Database: EventNamespaces, Key: namespace})
		}
	}
}

// listTypings returns the places in one statement of a scripted definition's
// body that say what a parameter takes: a parameter written whole after a
// link's colon, as in scope:$TARGET$, or as the value of a field. A key that
// is a link, and an argument of another scripted call, say nothing.
func listTypings(tree *jomini.Tree, statement int32, context *Context) []typing {
	current := tree.Statements[statement]
	var typings []typing
	for _, span := range []jomini.Span{tree.UnquotedSpan(current.Key), tree.UnquotedSpan(current.Value)} {
		for _, link := range jomini.Links(tree.Text(span), span.Start) {
			if link.Prefix.IsEmpty() {
				continue
			}

			if parameter, isWhole := findParameter(tree.Text(link.Name)); isWhole {
				typings = append(typings, typing{Parameter: parameter, Prefix: tree.Text(link.Prefix)})
			}
		}
	}

	parameter, isWhole := findParameter(tree.Unquoted(current.Value))
	if isWhole && context.Field != nil && context.Category != CategoryLink && context.Parent.Callee == nil {
		typings = append(typings, typing{Parameter: parameter, Field: context.Field})
	}

	return typings
}

// findParameter returns the name of the parameter a text is, as TARGET for
// $TARGET$. It returns false unless the whole text is one parameter.
func findParameter(text string) (string, bool) {
	parameters := jomini.Parameters(text, 0)
	if len(parameters) == 0 || parameters[0].Span != (jomini.Span{End: len(text)}) {
		return "", false
	}

	label := parameters[0].Name

	return text[label.Start:label.End], true
}

// declare records one definition a file makes, if script or a GUI file can
// call it by name. A widget type is recorded with the type it is built on,
// and a modifier type with what a modifier of its name takes.
func (d *declarer) declare(definition Copy, tree *jomini.Tree, statement int32) {
	database := d.schema.Databases[definition.Database]
	if database == nil {
		return
	}

	// a modifier takes yes or no where its type writes boolean = yes, else a
	// number or a script value
	if database.Declaration.Contents == ContentsModifiers {
		expected := Expected{Type: ValueTypeNumber, Values: true}
		if value, _ := tree.FindChildValue(statement, modifierBoolKey); value == Yes {
			expected = Expected{Type: ValueTypeBool}
		}

		d.declared = append(d.declared, &Definition{
			Database: database.Name, Key: definition.Name,
			Field: &Field{Value: Claim[Expected]{Value: expected, Source: SourceDefinitions}},
		})
	}

	base := tree.Unquoted(tree.Statements[statement].Tag)
	if base != "" && slices.Contains(database.Declaration.Callers, GUITagCalls) {
		d.widgets = append(d.widgets, base)
	}

	if definition.Local || len(database.Declaration.Callers) == 0 {
		return
	}

	if slices.Contains(database.Declaration.Callers, GUITagCalls) {
		d.widgets = append(d.widgets, definition.Name)
	}

	scripted := ScriptedCopy{Copy: definition}
	scripted.Definition, scripted.relays = makeScripted(database, definition.Name, tree, statement)
	d.copies = append(d.copies, scripted)
	if database.Declaration.Parameters {
		d.bodyLast = tree.Statements[statement].Last
	}
}

// modifierBoolKey is the key under which a modifier type writes yes when its
// modifier takes yes or no rather than a number.
const modifierBoolKey = "boolean"

// makeScripted returns the scripted definition a statement makes: a widget
// type with the datacontext its block writes, or, where the database takes
// parameters, a definition with its parameters and saves, and the
// parameters it passes on to other calls.
func makeScripted(database *Database, key string, tree *jomini.Tree, statement int32) (*Definition, []relay) {
	scripted := &Definition{Database: database.Name, Key: key}
	if child, ok := tree.FindChild(statement, GUIContextKey); ok && slices.Contains(database.Declaration.Callers, GUITagCalls) {
		if call, isCall := tree.ParseBracketed(tree.Statements[child].Value); isCall {
			scripted.Datacontext = &call
		}
	}

	if !database.Declaration.Parameters {
		return scripted, nil
	}

	scripted.Field = ListParameters(tree, statement)
	saves, relays := listSaves(tree, statement)
	scripted.Saves = saves

	return scripted, relays
}

// ListParameters returns the parameters a scripted definition's block
// writes, as $TARGET$, each as a key its calls may give. A parameter has a
// default only where every use of it writes one, as $SIZE|10$, and then it
// is the first.
func ListParameters(tree *jomini.Tree, statement int32) *Field {
	parameters := &Field{}
	undefaulted := map[string]bool{}
	for position := statement + 1; position < tree.Statements[statement].Last; position++ {
		current := tree.Statements[position]
		for _, span := range []jomini.Span{current.Key, current.Tag, current.Value} {
			for _, parameter := range jomini.Parameters(tree.Text(span), span.Start) {
				label := tree.Text(parameter.Name)
				argument := parameters.BuildChild(label, SourceDefinitions)
				argument.Vanilla.Count++

				// a closing $ right after the name writes no default; a bar
				// writes one, even an empty one
				switch {
				case parameter.Span.End == parameter.Name.End+1:
					undefaulted[label] = true
					argument.Default = Claim[string]{}

				case !undefaulted[label] && !argument.Default.IsStated():
					argument.Default = Claim[string]{Value: tree.Text(parameter.Default), Source: SourceVanilla}
				}
			}
		}
	}

	return parameters
}

// judgeCalls records, for each call of a scripted definition in the file,
// every argument the definition never reads and every parameter without a
// default that the call does not give. A call inside another scripted
// definition may leave out a parameter that one has too, since its own
// caller gives it.
func (w *walker) judgeCalls() {
	var definitions map[string]int32
	parameters := map[int32]*Field{}
	list := func(statement int32) *Field {
		if parameters[statement] == nil {
			parameters[statement] = ListParameters(w.tree, statement)
		}

		return parameters[statement]
	}

	for position := range w.contexts {
		statement, context := int32(position), &w.contexts[position]
		if context.Callee == nil || context.Callee.Field == nil || statement == 0 && context.Definition == nil {
			continue
		}

		// a definition this file makes is read as the file has it now, not
		// as it was when the schema was last built
		if definitions == nil {
			definitions = w.listOwnDefinitions()
		}

		current := w.tree.Statements[statement]
		callee := w.tree.Unquoted(current.Key)
		field := context.Callee.Field
		if own, ok := definitions[Reference{Database: context.Callee.Database, Name: callee}.ToID()]; ok {
			field = list(own)
		}

		enclosing := &Field{}
		if context.Definition != nil {
			enclosing = list(context.Definition.Statement)
		}

		given := map[string]bool{}
		for child := range w.tree.Children(statement) {
			key := w.tree.Statements[child].Key
			given[w.tree.Unquoted(key)] = true
			if _, isParameter := field.Children[w.tree.Unquoted(key)]; !isParameter && !key.IsEmpty() {
				w.recordArgument(key, w.tree.Unquoted(key), "is never read by", callee)
			}
		}

		for _, parameter := range slices.Sorted(maps.Keys(field.Children)) {
			_, isOuter := enclosing.Children[parameter]
			if !given[parameter] && !isOuter && !field.Children[parameter].Default.IsStated() {
				w.recordArgument(current.Key, parameter, "is not given to", callee)
			}
		}
	}
}

// listOwnDefinitions returns the statement of the first definition of each
// name the file defines, keyed by database and name.
func (w *walker) listOwnDefinitions() map[string]int32 {
	definitions := map[string]int32{}
	for position := range w.contexts {
		if context := &w.contexts[position]; context.Defines {
			id := Reference{Database: context.Definition.Database, Name: context.Definition.Name}.ToID()
			if _, ok := definitions[id]; !ok {
				definitions[id] = context.Definition.Statement
			}
		}
	}

	return definitions
}

// recordArgument records a problem with one argument of a call, with the
// words that say what is wrong and the name of the definition called.
func (w *walker) recordArgument(span jomini.Span, parameter, why, callee string) {
	facts := DescribeReference(Reference{Name: parameter})
	facts["why"], facts["callee"] = why, callee
	w.record(ProblemArgument, span, facts)
}

// listSaves reads a scripted definition's block for each parameter it saves
// a name under, as save_scope_as = $NAME$ does, with the link that reads the
// name back, and for each parameter it passes on to another scripted call.
func listSaves(tree *jomini.Tree, statement int32) (map[string]string, []relay) {
	saves := map[string]string{}
	var relays []relay
	for position := statement + 1; position < tree.Statements[statement].Last; position++ {
		current := tree.Statements[position]
		parameter, isWhole := findParameter(tree.Unquoted(current.Value))
		if !isWhole || current.Key.IsEmpty() {
			continue
		}

		key, holder := tree.Unquoted(current.Key), tree.Unquoted(tree.Statements[current.Parent].Key)
		if link, _, ok := findLinked(linkMakers, holder, key); ok {
			saves[parameter] = link
		} else if current.Parent != statement {
			relays = append(relays, relay{Callee: holder, Argument: key, Parameter: parameter})
		}
	}

	return saves, relays
}
