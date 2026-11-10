// scripted.go reads the scripted definitions, the modifier types and the
// event namespaces that the workspace's files declare.

package schema

import (
	"maps"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/jomini"
)

// declarer is the visitor of a walk over one file. It records the
// definitions that script and GUI files call by name, and the event
// namespaces that event ids are written with. It reads the top statements,
// and the bodies of the definitions that take parameters.
type declarer struct {
	schema *Schema

	// scripted holds the scripted definitions the file declares, by caller
	// and name.
	scripted map[string]ScriptedDefinition

	// widgets are the widget types the file defines or builds on.
	widgets []string

	// modifiers holds, by modifier type, what its name takes.
	modifiers map[string]*Field

	// namespaces are the event namespaces the file declares.
	namespaces []string

	// body is the definition whose body the walk is in. It is nil outside
	// one.
	body *body
}

// body is a scripted definition that a declarer is reading, with the keys it
// is recorded under once it is read.
type body struct {
	statement int32

	// keys are the keys it is recorded under, each a caller and a name.
	keys []string

	scripted ScriptedDefinition
}

// SetScripted reads, with the given parser, the scripted definitions,
// modifier types and event namespaces that the given files of the
// workspace's mods declare, in place of what those files declared before, so
// calls to them are known calls. A file that cannot be parsed, or is gone,
// declares nothing.
func (s *Schema) SetScripted(changed []files.File, parse func(path string) (*jomini.Tree, error)) {
	declaring := slices.DeleteFunc(slices.Clone(changed), func(file files.File) bool {
		return file.Format != files.FormatScript || !s.declares(file)
	})
	declarers := s.listDeclarers(declaring, parse)
	next := &modScripted{
		byFile: map[string]*declarer{}, all: map[string]ScriptedDefinition{}, modifiers: map[string]*Field{},
		namespaces: map[string]bool{},
	}
	if previous := s.mods.Load(); previous != nil {
		next.byFile = maps.Clone(previous.byFile)
	}

	for _, file := range changed {
		delete(next.byFile, file.Key)
	}

	for position, declarer := range declarers {
		if declarer != nil {
			next.byFile[declaring[position].Key] = declarer
		}
	}

	// each definition gets its own copy of its saves and parameters, which
	// only this table's typings and relays add to
	for _, key := range slices.Sorted(maps.Keys(next.byFile)) {
		for name, scripted := range next.byFile[key].scripted {
			scripted.Saves, scripted.Parameters = maps.Clone(scripted.Saves), cloneParameters(scripted.Parameters)
			next.all[name] = scripted
		}

		maps.Copy(next.modifiers, next.byFile[key].modifiers)
		for _, namespace := range next.byFile[key].namespaces {
			next.namespaces[namespace] = true
		}
	}

	s.typeParameters(next.all)
	s.resolveRelays(next.all, next.all)
	s.mods.Store(next)
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

	return ok && (len(database.Callable.Callers) > 0 || database.Row.Contents == game.ContentsModifiers ||
		database.Row.Contents == game.ContentsEvents)
}

// listDeclarers walks the given files that declare definitions, in
// parallel, and returns the declarer of each. A file that cannot be parsed
// has none.
func (s *Schema) listDeclarers(declaring []files.File, parse func(path string) (*jomini.Tree, error)) []*declarer {
	declarers := make([]*declarer, len(declaring))
	eachParsed(declaring, parse, func(_, index int, tree *jomini.Tree) {
		declarers[index] = &declarer{schema: s, scripted: map[string]ScriptedDefinition{}, modifiers: map[string]*Field{}}
		s.Walk(tree, declaring[index], declarers[index])
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

// typeParameters gives each parameter of the scripted definitions in a
// table the claim its typings make, as isBetterClaim picks among them.
func (s *Schema) typeParameters(table map[string]ScriptedDefinition) {
	for _, scripted := range table {
		for _, typing := range scripted.typings {
			argument, claim := scripted.Parameters.findChild(typing.Parameter), s.findTypingClaim(typing)
			if argument != nil && isBetterClaim(claim, argument.Value) {
				argument.Value = claim
			}
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
		database := s.Prefixes[typing.Prefix]
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

// resolveRelays fills in what the parameters of each scripted definition in
// a table take from the calls of other definitions it passes them on to: the
// link a name is saved under, and the claim on what it names. It repeats
// until nothing is added. A call finds the game's definitions, then the
// given mods'.
func (s *Schema) resolveRelays(table, mods map[string]ScriptedDefinition) {
	for added := true; added; {
		added = false
		for _, scripted := range table {
			for _, relay := range scripted.relays {
				if callee, _, ok := s.findScripted(mods, relay.Callee); ok {
					added = passRelay(scripted, callee, relay) || added
				}
			}
		}
	}
}

// passRelay gives the parameter that a relay passes on the save that it
// lacks, and the claim of the callee's argument where isBetterClaim picks
// it. It reports whether it gave either.
func passRelay(scripted, callee ScriptedDefinition, relay relay) bool {
	added := false
	_, saved := scripted.Saves[relay.Parameter]
	if link, saves := callee.Saves[relay.Argument]; saves && !saved {
		scripted.Saves[relay.Parameter] = link
		added = true
	}

	parameter, argument := scripted.Parameters.findChild(relay.Parameter), callee.Parameters.findChild(relay.Argument)
	if parameter != nil && argument != nil && isBetterClaim(argument.Value, parameter.Value) {
		parameter.Value = argument.Value
		added = true
	}

	return added
}

// findScripted returns the scripted definition of a name and the caller it
// is called through, trying each caller in alphabetical order. It returns
// false if no caller finds one.
func (s *Schema) findScripted(mods map[string]ScriptedDefinition, key string) (ScriptedDefinition, string, bool) {
	for _, caller := range slices.Sorted(maps.Keys(s.Callees)) {
		if callee, ok := s.lookupScripted(mods, caller, key); ok {
			return callee, caller, true
		}
	}

	return ScriptedDefinition{}, "", false
}

// lookupScripted returns the scripted definition of a name called by the
// given caller, from the game or else from the given mods. A mod's never
// takes the place of a command the dumps declare by that name. Case matters
// as in lookupDefinition.
func (s *Schema) lookupScripted(mods map[string]ScriptedDefinition, caller, key string) (ScriptedDefinition, bool) {
	if callee, ok := s.lookupDefinition(s.ScriptedDefinitions, caller, key); ok {
		return callee, true
	}

	if _, command := s.FindCommand(key); command {
		return ScriptedDefinition{}, false
	}

	return s.lookupDefinition(mods, caller, key)
}

// lookupDefinition returns the scripted definition of a name called by the
// given caller in one table. A name in another case finds it only if its
// database ignores case.
func (s *Schema) lookupDefinition(table map[string]ScriptedDefinition, caller, key string) (ScriptedDefinition, bool) {
	if callee, ok := table[caller+":"+key]; ok {
		return callee, true
	}

	callee, ok := table[caller+":"+strings.ToLower(key)]
	database := s.Databases[callee.Database]

	return callee, ok && database != nil && database.Callable.FoldsCase
}

// declareFiles gives the schema the definitions that the given files
// declare, a later one in place of an earlier.
func (s *Schema) declareFiles(declaring []files.File, parse func(path string) (*jomini.Tree, error)) {
	for _, declarer := range s.listDeclarers(declaring, parse) {
		if declarer != nil {
			declarer.build()
		}
	}
}

// build gives the schema what the walked file declared. A later definition
// of a name replaces an earlier one.
func (d *declarer) build() {
	maps.Copy(d.schema.ScriptedDefinitions, d.scripted)
	maps.Copy(d.schema.ModifierFields, d.modifiers)
	for _, widget := range d.widgets {
		d.schema.WidgetTypes[widget] = true
	}

	for _, namespace := range d.namespaces {
		d.schema.Namespaces[namespace] = true
	}
}

// Enter records the definition that a statement makes, if its database is
// called by name, and the event namespace it declares. It goes into the top
// blocks that only group definitions, into a file that is one definition,
// and into the body of a definition that takes parameters, whose typings it
// records.
func (d *declarer) Enter(tree *jomini.Tree, statement int32, context *Context) bool {
	if d.body != nil {
		d.body.scripted.typings = append(d.body.scripted.typings, listTypings(tree, statement, context)...)

		return true
	}

	if context.Defines && context.Category == CategoryDefinition {
		d.declare(*context.Definition, tree, statement)
	}

	if context.Parent != nil && slices.Contains(context.Parent.Namespaces, statement) {
		d.namespaces = append(d.namespaces, tree.Unquoted(tree.Statements[statement].Value))
	}

	return statement == 0 || d.body != nil || !context.Defines && tree.Statements[statement].Parent == 0
}

// listTypings returns the typings that one statement of a scripted
// definition's body writes. A key or a value that is one prefixed link whose
// name is one parameter, as scope:$TARGET$, is one. So is a value that is one
// parameter at a key with a field of its own. A link's key has its holder's
// field, and an argument of a call is the call's relay, so neither is one.
func listTypings(tree *jomini.Tree, statement int32, context *Context) []typing {
	current := tree.Statements[statement]
	var typings []typing
	for _, span := range []jomini.Span{tree.UnquotedSpan(current.Key), tree.UnquotedSpan(current.Value)} {
		links := jomini.Links(tree.Text(span), span.Start)
		if len(links) != 1 || links[0].Prefix.IsEmpty() {
			continue
		}

		if parameter, whole := findParameter(tree.Text(links[0].Name)); whole {
			typings = append(typings, typing{Parameter: parameter, Prefix: tree.Text(links[0].Prefix)})
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

	name := parameters[0].Name

	return text[name.Start:name.End], true
}

// declare records a definition that is called by name. For each caller of
// its database it records the scripted definition with the parameters its
// body writes; one that takes parameters is recorded when the walk leaves
// its body. If a widget calls it, it also records the widget type, and the
// type that widget is built on. It also records what the name of a modifier
// type takes.
func (d *declarer) declare(definition Definition, tree *jomini.Tree, statement int32) {
	database := d.schema.Databases[definition.Database]
	if database == nil {
		return
	}

	if database.Row.Contents == game.ContentsModifiers {
		d.modifiers[definition.Name] = makeModifierField(tree, statement)
	}

	base := tree.Unquoted(tree.Statements[statement].Tag)
	if base != "" && slices.Contains(database.Callable.Callers, game.GUI.TagCalls) {
		d.widgets = append(d.widgets, base)
	}

	if definition.Local {
		return
	}

	pending := &body{statement: statement, scripted: makeScripted(database, tree, statement)}
	for _, caller := range database.Callable.Callers {
		pending.keys = append(pending.keys, caller+":"+definition.Name)
		if database.Callable.FoldsCase {
			pending.keys = append(pending.keys, caller+":"+strings.ToLower(definition.Name))
		}

		if caller == game.GUI.TagCalls {
			d.widgets = append(d.widgets, definition.Name)
		}
	}

	if database.Callable.Parameters {
		d.body = pending

		return
	}

	d.keep(pending)
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

// makeScripted returns the scripted definition that a statement makes, with
// its parameters and saves where the database's definitions take
// parameters.
func makeScripted(database *Database, tree *jomini.Tree, statement int32) ScriptedDefinition {
	scripted := ScriptedDefinition{Database: database.Name}
	if database.Callable.Parameters {
		scripted.Parameters = ListParameters(tree, statement)
		scripted.Saves, scripted.relays = listSaves(tree, statement)
	}

	return scripted
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
				name := tree.Text(parameter.Name)
				argument := parameters.buildChild(name, game.SourceDefinitions)
				argument.Vanilla.Count++

				// a closing $ right after the name writes no default; a bar
				// writes one, even an empty one
				switch {
				case parameter.Span.End == parameter.Name.End+1:
					undefaulted[name] = true
					argument.Default = Claim[string]{}

				case !undefaulted[name] && !argument.Default.IsStated():
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

// keep records a scripted definition under each of its keys.
func (d *declarer) keep(pending *body) {
	for _, key := range pending.keys {
		d.scripted[key] = pending.scripted
	}
}

// Leave records the scripted definition whose body the walk leaves, with the
// typings its body writes.
func (d *declarer) Leave(tree *jomini.Tree, statement int32, context *Context) {
	if d.body != nil && d.body.statement == statement {
		d.keep(d.body)
		d.body = nil
	}
}
