// scripted.go reads the scripted definitions, the modifier types and the
// event namespaces that the workspace's files declare, and keeps of each
// scripted definition the copy the game keeps.

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

// declarer records what one file declares: the definitions that script and
// GUI files call by name, and the event namespaces that event ids are
// written with.
type declarer struct {
	schema *Schema

	// copies are the scripted definitions the file declares, as written.
	copies []ScriptedCopy

	// widgets are the widget types the file defines or builds on.
	widgets []string

	// declared are the modifier types the file defines, each with what its
	// name takes as its field, and the event namespaces the file declares.
	declared []*Definition

	// body is the definition whose body the walk is in. It is nil outside
	// one.
	body *body
}

// body is a scripted definition that a declarer is reading, kept once it is
// read.
type body struct {
	statement int32
	scripted  ScriptedCopy
}

// modSequence is the place of the first file of a mod among the files that
// declare: after every file of the game.
const modSequence = 1 << 30

// SetScripted reads, with the given parser, the scripted definitions,
// modifier types and event namespaces that the given files of the
// workspace's mods declare, in place of what those files declared before, so
// calls to them are known calls. A file that cannot be parsed, or is gone,
// declares nothing, and neither does a file the game does not read: unread
// holds the keys of those, the game's own among them. The winners are picked
// again only when a changed file declares a scripted definition, or declared
// one, or the unread files differ: any other save leaves every copy as it
// was.
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

// resolveModded returns the scripted definitions that the open workspace
// changes, by database and name: the winner of each name a mod declares,
// among the mods' copies and the game's, and of each name the game declares
// in a file it does not read, among the copies left. A name with no copy
// left has nil. A mod's copy never takes the place of a command the dumps
// declare by its name. A mod's winners get their parameters typed and their
// relays passed on.
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

			// each copy gets its own saves and parameters, which only this
			// table's typings and relays add to
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
// scripted definition's name, by its database's precedence. It returns false
// if the game keeps none.
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

// compareCopies orders two copies of a name the way the game loads them: by
// their files' paths, or by the order their files were listed in where the
// precedence says the playset decides, and within one file as written.
func compareCopies(first, second ScriptedCopy, precedence game.Precedence) int {
	if !precedence.ByPlayset {
		if order := files.CompareLoadOrder(first.Rel, second.Rel); order != 0 {
			return order
		}
	}

	return cmp.Or(cmp.Compare(first.Sequence, second.Sequence), cmp.Compare(first.Copy.Statement, second.Copy.Statement))
}

// toScriptedKey returns the key a copy of a scripted definition is kept
// under: its database, a colon and its name, in lower case where the
// database ignores case.
func (s *Schema) toScriptedKey(definition Copy) string {
	label := definition.Name
	if database := s.Databases[definition.Database]; database != nil && database.Declaration.FoldsCase {
		label = strings.ToLower(label)
	}

	return definition.Database + ":" + label
}

// lookupScripted returns the scripted definition of a name in a database:
// the open workspace's winner where the given winners hold one for the name,
// else the copy the game keeps. A name in another case finds it only if the
// database ignores case. It returns nil if there is none.
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

// lookupCall returns the scripted definition that a call of a name through
// the given caller finds, in the first of the caller's databases that has
// one. It returns nil if none has.
func (s *Schema) lookupCall(winners map[string]*Definition, caller, key string) *Definition {
	for _, database := range s.Callees[caller] {
		if callee := s.lookupScripted(winners, database, key); callee != nil {
			return callee
		}
	}

	return nil
}

// findScripted returns the scripted definition of a name and the caller it
// is called through, for a name written in a file of the given format. It
// tries the callers that format has, in alphabetical order: a GUI file's
// keys call the GUI's types, templates and blocks, and script calls the
// rest, so a name in script never finds a GUI type. It returns false if no
// caller finds one.
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

// place gives the copies a file declares the facts of the file: its key, its
// path from its stage, and its place among the files listed.
func (d *declarer) place(file files.File, sequence int) {
	for position := range d.copies {
		d.copies[position].File, d.copies[position].Rel, d.copies[position].Sequence = file.Key, file.StageRel(), sequence
	}
}

// declares reports whether a file declares definitions that script or GUI
// files call by name, or event namespaces. A GUI file does, and so does a
// file of a database that has callers, a modifier types file and an events
// file.
func (s *Schema) declares(file files.File) bool {
	if file.Format == files.FormatGUI {
		return true
	}

	database, ok := s.FindDatabase(file)

	return ok && (len(database.Declaration.Callers) > 0 || database.Declaration.Contents == game.ContentsModifiers ||
		database.Declaration.Contents == game.ContentsEvents)
}

// listDeclarers walks the given files that declare definitions, in
// parallel, and returns the declarer of each. A file that cannot be parsed
// has none.
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

// eachParsed parses the listed files in parallel and calls visit on each
// result with the worker's number and the file's position. It skips a file
// that cannot be parsed and returns the errors of those files.
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

// cloneParameters returns a copy of a scripted definition's parameters whose
// fields change apart from the originals. It returns nil for nil.
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

// typeParameters gives each parameter of the given copies of scripted
// definitions the claim its typings make.
func (s *Schema) typeParameters(copies []ScriptedCopy) {
	for _, scripted := range copies {
		s.typeDefinition(scripted.Definition, scripted.typings)
	}
}

// typeDefinition gives each parameter of a scripted definition the claim
// that the given typings of its body make, as isBetterClaim picks among
// them. A typing of a parameter the definition lacks gives nothing.
func (s *Schema) typeDefinition(scripted *Definition, typings []typing) {
	for _, typing := range typings {
		argument, claim := scripted.Field.findChild(typing.Parameter), s.findTypingClaim(typing)
		if argument != nil && isBetterClaim(claim, argument.Value) {
			argument.Value = claim
		}
	}
}

// findTypingClaim returns what a typing says its parameter takes. After a
// prefix it is a name in the prefix's database, as the definition states it.
// At a key it is what the key's field claims, inferred unless it names a
// database, since whether a number or a choice fits depends on where the
// value is written. It is unstated where neither says.
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

// isBetterClaim reports whether a claim takes the place of what a parameter
// claims so far: it states one where the parameter has none, or names a
// database where the parameter's names none.
func isBetterClaim(claim, current Claim[Expected]) bool {
	if !claim.IsStated() {
		return false
	}

	return !current.IsStated() || claim.Value.namesDatabase() && !current.Value.namesDatabase()
}

// resolveRelays fills in what the parameters of the given copies of scripted
// definitions take from the calls of other definitions they pass them on to:
// the link a name is saved under, and the claim on what it names. It repeats
// until nothing is added. A call finds its definition as a walk does, with
// the given winners of the open workspace.
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

// passRelay gives the parameter that a relay passes on the save that it
// lacks, and the claim of the callee's argument where isBetterClaim picks
// it. It reports whether it gave either.
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

// declareFiles gives the schema what the given files of the game declare, in
// place of what it held: every copy of each scripted definition, and as
// definitions of their databases the copy of each name that the game keeps,
// each modifier type and each event namespace. A widget type the files
// define or build on joins the widget types, which are never emptied. It
// returns the winners, for their parameters to be typed and their relays
// passed on.
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

// declareWidgets adds the given widget types to the database of them, each
// as a definition with no database, so its block takes no type's keys.
func (s *Schema) declareWidgets(widgets []string) {
	types := s.Databases[game.GUI.WidgetTypes]
	if types.Definitions == nil {
		types.Definitions = map[string]*Definition{}
	}

	for _, widget := range widgets {
		types.Definitions[widget] = &Definition{Key: widget}
	}
}

// record records what a file declares, from the contexts WalkDeclarations
// gave its statements: each definition whose database is called by name,
// each event namespace, and the typings in the body of a definition that
// takes parameters, which is recorded when its body ends.
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

// listTypings returns the typings that one statement of a scripted
// definition's body writes. Each link of a key or a value that is written
// with a prefix and one parameter as its name is one, as scope:$TARGET$ is,
// and court_position:$POS$ in $LIEGE$.court_position:$POS$. So is a value
// that is one parameter at a key with a field of its own. A link's key has
// its holder's field, and an argument of a call is the call's relay, so
// neither is one.
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

// findParameter returns the name of the parameter that a text is, written
// whole, and true. It returns false for a text that is not one parameter.
func findParameter(text string) (string, bool) {
	parameters := jomini.Parameters(text, 0)
	if len(parameters) == 0 || parameters[0].Span != (jomini.Span{End: len(text)}) {
		return "", false
	}

	label := parameters[0].Name

	return text[label.Start:label.End], true
}

// declare records a definition that is called by name: a copy of a scripted
// definition, with the parameters its body writes. One that takes
// parameters is recorded when the walk leaves its body. If a widget calls
// it, it also records the widget type, and the type that widget is built
// on. A modifier type is recorded with what its name takes.
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

// modifierBoolKey is the key whose yes makes a modifier type take yes or no
// rather than a number.
const modifierBoolKey = "boolean"

// makeModifierField returns what the name of a modifier type takes: yes or
// no if its definition writes yes at modifierBoolKey, else a number or a
// script value.
func makeModifierField(tree *jomini.Tree, statement int32) *Field {
	expected := Expected{Type: ValueTypeNumber, Values: true}
	if value, _ := tree.FindChildValue(statement, modifierBoolKey); value == yes {
		expected = Expected{Type: ValueTypeBool}
	}

	return &Field{Value: Claim[Expected]{Value: expected, Source: game.SourceDefinitions}}
}

// makeScripted returns the scripted definition that a statement makes under
// the given key, and the parameters its body passes on to other calls. Its
// field holds its parameters, and it has its saves, where the database's
// definitions take parameters.
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

// ListParameters returns the parameters a scripted definition's body writes
// in its keys, tags and values, as the fields a call to it takes, which the
// definition declares. A parameter written only in a comment is none. A
// parameter whose every use writes a default has the first one as its
// default.
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

// listSaves returns the parameters a scripted definition's body saves a name
// under, with the link that reads each name, and the parameters it passes on
// to calls of other scripted definitions. Only a value that is one whole
// parameter counts.
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
