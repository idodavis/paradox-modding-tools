// scripted.go reads the scripted definitions, modifier types and event
// namespaces that files declare, and keeps the copy the game uses.

package schema

import (
	"cmp"
	"maps"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/jomini"
)

// declarer is what one file declares: the definitions script and GUI files
// call by name, modifier types and event namespaces.
type declarer struct {
	schema *Schema

	copies []ScriptedCopy

	// widgets are the widget types the file defines or builds on.
	widgets []string

	// declared are the modifier types and event namespaces the file declares.
	declared []*Definition

	// body is the definition the walk is in. It is nil outside one.
	body *body
}

type body struct {
	statement int32
	scripted  ScriptedCopy
}

// modSequence is the place of the first mod file, after every game file.
const modSequence = 1 << 30

// SetScripted reads what the given files of the mods declare, in place of what
// they declared before. A file that cannot be parsed or is gone declares
// nothing, nor does one in unread, the keys of files the game does not read.
// The winners are picked again only if a changed file declares or declared a
// scripted definition, or unread differs.
func (s *Schema) SetScripted(changed []files.File, unread map[string]bool, parse func(path string) (*jomini.Tree, error)) {
	declaring := slices.DeleteFunc(slices.Clone(changed), func(file files.File) bool {
		return file.Format != files.FormatScript || !s.declares(file)
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

// resolveModded returns the winner of each scripted name the open workspace
// changes, by database and name: one a mod declares, or the game declares in
// a file it does not read. A name left with no copy has nil. A mod's copy
// never replaces a command the dumps declare.
func (s *Schema) resolveModded(mods *modScripted) map[string]*Definition {
	groups := map[string][]ScriptedCopy{}
	for key, copies := range s.ScriptedCopies {
		if slices.ContainsFunc(copies, func(scripted ScriptedCopy) bool { return mods.unread[scripted.File] }) {
			groups[key] = nil
		}
	}

	for _, key := range slices.Sorted(maps.Keys(mods.byFile)) {
		if mods.unread[key] {
			continue
		}

		for _, scripted := range mods.byFile[key].copies {
			if s.FindCommand(scripted.Copy.Name) != nil {
				continue
			}

			// each copy has its own saves and parameters, which are added to
			definition := *scripted.Definition
			definition.Saves, definition.Field = maps.Clone(definition.Saves), cloneParameters(definition.Field)
			scripted.Definition = &definition
			groups[s.toScriptedKey(scripted.Copy)] = append(groups[s.toScriptedKey(scripted.Copy)], scripted)
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

	s.typeParameters(modded)
	s.resolveRelays(modded, winners)

	return winners
}

// findScriptedWinner returns the copy the game keeps among the copies of one
// name. It returns false if it keeps none.
func (s *Schema) findScriptedWinner(copies []ScriptedCopy) (ScriptedCopy, bool) {
	if len(copies) == 0 {
		return ScriptedCopy{}, false
	}

	precedence := game.Precedence{}
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

// compareCopies orders two copies the way the game loads them: by file path,
// or by listed order if the playset decides, then as written.
func compareCopies(first, second ScriptedCopy, precedence game.Precedence) int {
	if !precedence.ByPlayset {
		if order := files.CompareLoadOrder(first.Rel, second.Rel); order != 0 {
			return order
		}
	}

	return cmp.Or(cmp.Compare(first.Sequence, second.Sequence), cmp.Compare(first.Copy.Statement, second.Copy.Statement))
}

// toScriptedKey returns a copy's key, database:name, in lower case if the
// database ignores case.
func (s *Schema) toScriptedKey(definition Copy) string {
	label := definition.Name
	if database := s.Databases[definition.Database]; database != nil && database.Declaration.FoldsCase {
		label = strings.ToLower(label)
	}

	return definition.Database + ":" + label
}

// lookupScripted returns the definition of a name: the winner if winners
// holds one, else the game's copy. Another case finds it only if the database
// ignores case. It returns nil for none.
func (s *Schema) lookupScripted(winners map[string]*Definition, database, key string) *Definition {
	owner := s.Databases[database]
	if owner == nil {
		return nil
	}

	if owner.Declaration.FoldsCase {
		key = strings.ToLower(key)
	}

	if winner, changed := winners[database+":"+key]; changed {
		return winner
	}

	return owner.Definitions[key]
}

func (s *Schema) lookupCall(winners map[string]*Definition, caller, key string) *Definition {
	for _, database := range s.Callees[caller] {
		if callee := s.lookupScripted(winners, database, key); callee != nil {
			return callee
		}
	}

	return nil
}

// findScripted returns the definition a name calls and the caller, for a file
// of the given format. A GUI file's keys call GUI types, templates and blocks,
// and script calls the rest, so script never finds a GUI type. It returns
// false if no caller finds one.
func (s *Schema) findScripted(winners map[string]*Definition, key string, format files.Format) (*Definition, string, bool) {
	gui := []string{game.GUI.TagCalls, game.GUI.TemplateCalls, game.GUI.BlockCalls}
	for _, caller := range slices.Sorted(maps.Keys(s.Callees)) {
		if slices.Contains(gui, caller) != (format == files.FormatGUI) {
			continue
		}

		if callee := s.lookupCall(winners, caller, key); callee != nil {
			return callee, caller, true
		}
	}

	return nil, "", false
}

func (d *declarer) place(file files.File, sequence int) {
	for position := range d.copies {
		d.copies[position].File, d.copies[position].Rel, d.copies[position].Sequence = file.Key, file.StageRel(), sequence
	}
}

// declares reports whether a file declares definitions called by name or event
// namespaces: GUI, modifier type and event files, and files of databases with
// callers.
func (s *Schema) declares(file files.File) bool {
	if file.Format == files.FormatGUI {
		return true
	}

	database, ok := s.FindDatabase(file)

	return ok && (len(database.Declaration.Callers) > 0 || database.Declaration.Contents == game.ContentsModifiers ||
		database.Declaration.Contents == game.ContentsEvents)
}

// listDeclarers walks the files in parallel. A file that cannot be parsed has
// a nil declarer.
func (s *Schema) listDeclarers(declaring []files.File, parse func(path string) (*jomini.Tree, error)) []*declarer {
	declarers := make([]*declarer, len(declaring))
	contexts := make([][]Context, files.Workers)
	eachParsed(declaring, parse, func(worker, index int, tree *jomini.Tree) {
		declarers[index] = &declarer{schema: s}
		contexts[worker] = s.WalkDeclarations(tree, declaring[index], contexts[worker])
		declarers[index].record(tree, contexts[worker])
	})

	return declarers
}

// eachParsed parses the files in parallel and calls visit on each result with
// the worker's number and the file's position. It skips files that cannot be
// parsed and returns their errors.
func eachParsed[T any](listing []files.File, parse func(path string) (T, error), visit func(worker, index int, parsed T)) []error {
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

func (s *Schema) typeParameters(copies []ScriptedCopy) {
	for _, scripted := range copies {
		s.typeDefinition(scripted.Definition, scripted.typings)
	}
}

// typeDefinition gives each parameter the claim its typings make, as
// isBetterClaim picks. A typing of an absent parameter gives nothing.
func (s *Schema) typeDefinition(scripted *Definition, typings []typing) {
	for _, typing := range typings {
		argument, claim := scripted.Field.findChild(typing.Parameter), s.findTypingClaim(typing)
		if argument != nil && isBetterClaim(claim, argument.Value) {
			argument.Value = claim
		}
	}
}

// findTypingClaim returns what a typing says its parameter takes: a name in
// the prefix's database, or what the key's field claims, inferred unless it
// names a database, as a number or choice fits only where it is written.
func (s *Schema) findTypingClaim(typing typing) Claim[Expected] {
	if typing.Field == nil {
		database, _ := s.LookupPrefix(typing.Prefix)
		if database == "" {
			return Claim[Expected]{}
		}

		return Claim[Expected]{Value: Expected{Type: ValueTypeReference, Database: database}, Source: game.SourceDefinitions}
	}

	claim := typing.Field.Value
	if claim.IsStated() && !claim.Value.namesDatabase() {
		claim.Source = game.SourceInferred
	}

	return claim
}

// isBetterClaim reports whether a claim replaces what a parameter claims: it
// states one where there is none, or names a database where that names none.
func isBetterClaim(claim, current Claim[Expected]) bool {
	if !claim.IsStated() {
		return false
	}

	return !current.IsStated() || claim.Value.namesDatabase() && !current.Value.namesDatabase()
}

// resolveRelays gives the parameters of the copies what the calls they pass
// them on to give. It repeats until nothing is added.
func (s *Schema) resolveRelays(copies []ScriptedCopy, winners map[string]*Definition) {
	for added := true; added; {
		added = false
		for _, scripted := range copies {
			for _, relay := range scripted.relays {
				if callee, _, ok := s.findScripted(winners, relay.Callee, files.FormatScript); ok {
					added = passRelay(scripted.Definition, callee, relay) || added
				}
			}
		}
	}
}

// passRelay gives the relay's parameter the callee's save and claim, where it
// lacks the save or isBetterClaim picks the claim. It reports if it did.
func passRelay(scripted, callee *Definition, relay relay) bool {
	added := false
	_, saved := scripted.Saves[relay.Parameter]
	if link, saves := callee.Saves[relay.Argument]; saves && !saved {
		scripted.Saves[relay.Parameter] = link
		added = true
	}

	parameter, argument := scripted.Field.findChild(relay.Parameter), callee.Field.findChild(relay.Argument)
	if parameter != nil && argument != nil && isBetterClaim(argument.Value, parameter.Value) {
		parameter.Value = argument.Value
		added = true
	}

	return added
}

// declareFiles replaces what the schema holds with what the game's files
// declare: every copy of each scripted definition, and as the database's
// definitions the game's copy, modifier types and event namespaces. Widget
// types are only added to. It returns the winners.
func (s *Schema) declareFiles(declaring []files.File, parse func(path string) (*jomini.Tree, error)) []ScriptedCopy {
	s.ScriptedCopies = map[string][]ScriptedCopy{}
	for label, database := range s.Databases {
		if len(database.Declaration.Callers) > 0 || label == s.ModifierTypes || label == game.EventNamespaces {
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

		s.declareWidgets(declarer.widgets)
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

// declareWidgets adds widget types as definitions with no database, so a
// widget's block takes no type's keys.
func (s *Schema) declareWidgets(widgets []string) {
	types := s.Databases[game.GUI.WidgetTypes]
	if types.Definitions == nil {
		types.Definitions = map[string]*Definition{}
	}

	for _, widget := range widgets {
		types.Definitions[widget] = &Definition{Key: widget}
	}
}

// record reads what a file declares from the contexts WalkDeclarations gave:
// definitions, event namespaces and the typings in the body of a definition
// that takes parameters, which is recorded when its body ends.
func (d *declarer) record(tree *jomini.Tree, contexts []Context) {
	for position := range contexts {
		statement, context := int32(position), &contexts[position]
		if d.body != nil && tree.Statements[d.body.statement].Last <= statement {
			d.copies = append(d.copies, d.body.scripted)
			d.body = nil
		}

		if d.body != nil {
			d.body.scripted.typings = append(d.body.scripted.typings, listTypings(tree, statement, context)...)

			continue
		}

		if context.Defines && context.Category == CategoryDefinition {
			d.declare(*context.Definition, tree, statement)
		}

		if context.Parent != nil && slices.Contains(context.Parent.Namespaces, statement) {
			namespace := tree.Unquoted(tree.Statements[statement].Value)
			d.declared = append(d.declared, &Definition{Database: game.EventNamespaces, Key: namespace})
		}
	}

	if d.body != nil {
		d.copies = append(d.copies, d.body.scripted)
		d.body = nil
	}
}

// listTypings returns the typings one statement of a definition's body writes:
// a link with a prefix and a lone parameter as its name, as in scope:$TARGET$
// and $LIEGE$.court_position:$POS$, or a lone parameter value at a key with a
// field. A link key and a call's argument are no typing.
func listTypings(tree *jomini.Tree, statement int32, context *Context) []typing {
	current := tree.Statements[statement]
	var typings []typing
	for _, span := range []jomini.Span{tree.UnquotedSpan(current.Key), tree.UnquotedSpan(current.Value)} {
		for _, link := range jomini.Links(tree.Text(span), span.Start) {
			if link.Prefix.IsEmpty() {
				continue
			}

			if parameter, whole := findParameter(tree.Text(link.Name)); whole {
				typings = append(typings, typing{Parameter: parameter, Prefix: tree.Text(link.Prefix)})
			}
		}
	}

	parameter, whole := findParameter(tree.Unquoted(current.Value))
	if whole && context.Field != nil && context.Category != CategoryLink && context.Parent.Callee == nil {
		typings = append(typings, typing{Parameter: parameter, Field: context.Field})
	}

	return typings
}

// findParameter returns the parameter that is the whole text, or false.
func findParameter(text string) (string, bool) {
	parameters := jomini.Parameters(text, 0)
	if len(parameters) == 0 || parameters[0].Span != (jomini.Span{End: len(text)}) {
		return "", false
	}

	label := parameters[0].Name

	return text[label.Start:label.End], true
}

// declare records a definition called by name: a copy with the parameters its
// body writes, kept when the walk leaves the body. A widget also records its
// type and the type it is built on. A modifier type is recorded with what its
// name takes.
func (d *declarer) declare(definition Copy, tree *jomini.Tree, statement int32) {
	database := d.schema.Databases[definition.Database]
	if database == nil {
		return
	}

	if database.Declaration.Contents == game.ContentsModifiers {
		d.declared = append(d.declared, &Definition{
			Database: database.Name, Key: definition.Name, Field: makeModifierField(tree, statement),
		})
	}

	base := tree.Unquoted(tree.Statements[statement].Tag)
	if base != "" && slices.Contains(database.Declaration.Callers, game.GUI.TagCalls) {
		d.widgets = append(d.widgets, base)
	}

	if definition.Local || len(database.Declaration.Callers) == 0 {
		return
	}

	if slices.Contains(database.Declaration.Callers, game.GUI.TagCalls) {
		d.widgets = append(d.widgets, definition.Name)
	}

	pending := &body{statement: statement, scripted: ScriptedCopy{Copy: definition}}
	pending.scripted.Definition, pending.scripted.relays = makeScripted(database, definition.Name, tree, statement)
	if database.Declaration.Parameters {
		d.body = pending

		return
	}

	d.copies = append(d.copies, pending.scripted)
}

// modifierBoolKey is the key whose yes makes a modifier type take yes or no.
const modifierBoolKey = "boolean"

// makeModifierField returns what a modifier type's name takes: yes or no, or
// else a number or script value.
func makeModifierField(tree *jomini.Tree, statement int32) *Field {
	expected := Expected{Type: ValueTypeNumber, Values: true}
	if value, _ := tree.FindChildValue(statement, modifierBoolKey); value == yes {
		expected = Expected{Type: ValueTypeBool}
	}

	return &Field{Value: Claim[Expected]{Value: expected, Source: game.SourceDefinitions}}
}

// makeScripted returns the scripted definition a statement makes, and the
// relays its body passes on. Where the database's definitions take parameters,
// its field holds them.
func makeScripted(database *Database, key string, tree *jomini.Tree, statement int32) (*Definition, []relay) {
	scripted := &Definition{Database: database.Name, Key: key}
	if !database.Declaration.Parameters {
		return scripted, nil
	}

	scripted.Field = ListParameters(tree, statement)
	saves, relays := listSaves(tree, statement)
	scripted.Saves = saves

	return scripted, relays
}

// ListParameters returns the parameters a definition's body writes, as the
// fields a call takes. One written only in a comment is none. One whose every
// use writes a default has the first as its default.
func ListParameters(tree *jomini.Tree, statement int32) *Field {
	parameters := &Field{}
	undefaulted := map[string]bool{}
	for position := statement + 1; position < tree.Statements[statement].Last; position++ {
		current := tree.Statements[position]
		for _, span := range []jomini.Span{current.Key, current.Tag, current.Value} {
			for _, parameter := range jomini.Parameters(tree.Text(span), span.Start) {
				label := tree.Text(parameter.Name)
				argument := parameters.buildChild(label, game.SourceDefinitions)
				argument.Vanilla.Count++

				// a closing $ right after the name writes no default; a bar
				// writes one, even an empty one
				switch {
				case parameter.Span.End == parameter.Name.End+1:
					undefaulted[label] = true
					argument.Default = Claim[string]{}

				case !undefaulted[label] && !argument.Default.IsStated():
					argument.Default = Claim[string]{Value: tree.Text(parameter.Default), Source: game.SourceVanilla}
				}
			}
		}
	}

	return parameters
}

// judgeCalls records, for each call in the walked file, the arguments its
// callee does not read and the parameters with no default the call does not
// give. A call inside another definition counts that one's parameters as
// given.
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

		// a definition the file writes is read as written now
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
			if _, known := field.Children[w.tree.Unquoted(key)]; !known && !key.IsEmpty() {
				w.recordArgument(key, w.tree.Unquoted(key), "is never read by", callee)
			}
		}

		for _, parameter := range slices.Sorted(maps.Keys(field.Children)) {
			_, inherited := enclosing.Children[parameter]
			if !given[parameter] && !inherited && !field.Children[parameter].Default.IsStated() {
				w.recordArgument(current.Key, parameter, "is not given to", callee)
			}
		}
	}
}

// listOwnDefinitions returns the statement of the first definition the
// walked file writes of each name, by the name's id.
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

func (w *walker) recordArgument(span jomini.Span, parameter, why, callee string) {
	facts := DescribeReference(Reference{Name: parameter})
	facts["why"], facts["callee"] = why, callee
	w.record(ProblemArgument, span, facts)
}

// listSaves returns the parameters a body saves a name under, with the link
// that reads each, and the parameters it passes on to other calls.
func listSaves(tree *jomini.Tree, statement int32) (map[string]string, []relay) {
	saves := map[string]string{}
	var relays []relay
	for position := statement + 1; position < tree.Statements[statement].Last; position++ {
		current := tree.Statements[position]
		parameter, whole := findParameter(tree.Unquoted(current.Value))
		if !whole || current.Key.IsEmpty() {
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
