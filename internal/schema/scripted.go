// scripted.go reads the scripted definitions, the modifier types and the event namespaces that the workspace's files
// declare.

package schema

import (
	"maps"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/jomini"
)

// A declarer is the visitor of a walk over one file. It records the definitions that script and GUI files call by name,
// and the event namespaces that event ids are written with. It reads the top statements, and the bodies of the
// definitions that take parameters.
type declarer struct {
	schema     *Schema
	scripted   map[string]ScriptedDefinition // by caller and name
	widgets    []string                      // the widget types the file defines or builds on
	modifiers  map[string]*Field             // by modifier type: what its name takes
	namespaces []string                      // the event namespaces the file declares
	body       *body                         // the definition whose body the walk is in; nil outside one
}

// A body is a scripted definition that a declarer is reading, with the keys it is recorded under once it is read.
type body struct {
	statement int32
	keys      []string // by caller and name
	scripted  ScriptedDefinition
}

// parameterRe matches a parameter in a scripted definition's body, with its default if it has one.
var parameterRe = regexp.MustCompile(`\$([A-Za-z0-9_]+)(?:\|([^$]*))?\$`)

// SetScripted reads, with the given parser, the scripted definitions, modifier types and event namespaces that the
// given files of the workspace's mods declare, in place of what those files declared before, so calls to them are
// known calls. A file that cannot be parsed, or is gone, declares nothing.
func (schema *Schema) SetScripted(changed []files.File, parse func(path string) (*jomini.Tree, error)) {
	declaring := slices.DeleteFunc(slices.Clone(changed), func(file files.File) bool {
		return file.Format != files.FormatScript || !schema.declares(file)
	})
	declarers := schema.listDeclarers(declaring, parse)
	next := &modScripted{
		byFile: map[string]*declarer{}, all: map[string]ScriptedDefinition{}, modifiers: map[string]*Field{},
		namespaces: map[string]bool{},
	}
	if previous := schema.mods.Load(); previous != nil {
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
	// each definition gets its own copy of its saves and parameters, which only this table's typings and relays add to
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
	schema.typeParameters(next.all)
	schema.resolveRelays(next.all, next.all)
	schema.mods.Store(next)
}

// cloneParameters returns a copy of a scripted definition's parameters whose fields change apart from the originals.
// It returns nil for nil.
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

// listDeclarers walks the given files that declare definitions, in parallel, and returns the declarer of each. A file
// that cannot be parsed has none.
func (schema *Schema) listDeclarers(declaring []files.File, parse func(path string) (*jomini.Tree, error)) []*declarer {
	declarers := make([]*declarer, len(declaring))
	eachParsed(declaring, parse, func(_, index int, tree *jomini.Tree) {
		declarers[index] = &declarer{schema: schema, scripted: map[string]ScriptedDefinition{}, modifiers: map[string]*Field{}}
		schema.Walk(tree, declaring[index], declarers[index])
	})
	return declarers
}

// typeParameters gives each parameter of the scripted definitions in a table the claim of the first of its typings
// that names a database. A parameter that has a claim keeps it.
func (schema *Schema) typeParameters(table map[string]ScriptedDefinition) {
	for _, scripted := range table {
		for _, typing := range scripted.typings {
			argument := scripted.Parameters.findChild(typing.Parameter)
			if claim, ok := schema.findTypingClaim(typing); ok && argument != nil && !argument.Value.IsStated() {
				argument.Value = claim
			}
		}
	}
}

// findTypingClaim returns what a typing says its parameter takes. After a prefix it is a name in the prefix's
// database, as the definition states it. At a key it is what the key's field claims. It returns false if neither names
// a database.
func (schema *Schema) findTypingClaim(typing typing) (Claim[Expected], bool) {
	if typing.Field != nil {
		return typing.Field.Value, typing.Field.claimsDatabase()
	}
	database := schema.Prefixes[typing.Prefix]
	expected := Expected{Type: ValueReference, Database: database}
	return Claim[Expected]{Value: expected, Source: game.SourceDefinitions}, database != ""
}

// resolveRelays fills in what the parameters of each scripted definition in a table take from the calls of other
// definitions it passes them on to: the link a name is saved under, and the claim on what it names. It repeats until
// nothing is added. A call finds the game's definitions, then the given mods'. A parameter keeps the first of each it
// gets.
func (schema *Schema) resolveRelays(table, mods map[string]ScriptedDefinition) {
	for added := true; added; {
		added = false
		for _, scripted := range table {
			for _, relay := range scripted.relays {
				if callee, _, ok := schema.findScripted(mods, relay.Callee); ok {
					added = passRelay(scripted, callee, relay) || added
				}
			}
		}
	}
}

// passRelay gives the parameter that a relay passes on the save and the claim of the callee's argument that the
// parameter lacks. It reports whether it gave either.
func passRelay(scripted, callee ScriptedDefinition, relay relay) bool {
	added := false
	_, saved := scripted.Saves[relay.Parameter]
	if link, saves := callee.Saves[relay.Argument]; saves && !saved {
		scripted.Saves[relay.Parameter] = link
		added = true
	}
	parameter, argument := scripted.Parameters.findChild(relay.Parameter), callee.Parameters.findChild(relay.Argument)
	if parameter != nil && argument != nil && argument.Value.IsStated() && !parameter.Value.IsStated() {
		parameter.Value = argument.Value
		added = true
	}
	return added
}

// findScripted returns the scripted definition of a name and the caller it is called through, trying each caller in
// alphabetical order. It returns false if no caller finds one.
func (schema *Schema) findScripted(mods map[string]ScriptedDefinition, key string) (ScriptedDefinition, string, bool) {
	for _, caller := range slices.Sorted(maps.Keys(schema.Callees)) {
		if callee, ok := schema.lookupScripted(mods, caller, key); ok {
			return callee, caller, true
		}
	}
	return ScriptedDefinition{}, "", false
}

// lookupScripted returns the scripted definition of a name called by the given caller, from the game or else from the
// given mods. A mod's never takes the place of a command the dumps declare by that name. Case matters as in
// lookupDefinition.
func (schema *Schema) lookupScripted(mods map[string]ScriptedDefinition, caller, key string) (ScriptedDefinition, bool) {
	if callee, ok := schema.lookupDefinition(schema.ScriptedDefinitions, caller, key); ok {
		return callee, true
	}
	if _, command := schema.FindCommand(key); command {
		return ScriptedDefinition{}, false
	}
	return schema.lookupDefinition(mods, caller, key)
}

// lookupDefinition returns the scripted definition of a name called by the given caller in one table. A name in another
// case finds it only if its database ignores case.
func (schema *Schema) lookupDefinition(table map[string]ScriptedDefinition, caller, key string) (ScriptedDefinition, bool) {
	if callee, ok := table[caller+":"+key]; ok {
		return callee, true
	}
	callee, ok := table[caller+":"+strings.ToLower(key)]
	database := schema.Databases[callee.Database]
	return callee, ok && database != nil && database.Callable.FoldsCase
}

// declares reports whether a file declares definitions that script or GUI files call by name, or event namespaces. A
// GUI file does, and so does a file of a database that has callers, a file of a database whose definitions say what
// their names take, and an events file.
func (schema *Schema) declares(file files.File) bool {
	if file.Format == files.FormatGUI {
		return true
	}
	database, ok := schema.FindDatabase(file.Rel)
	return ok && (len(database.Callable.Callers) > 0 || database.Row.BoolKey != "" ||
		database.Row.Contents == game.ContentsEvents)
}

// eachParsed parses the listed files in parallel and calls visit on each result with the worker's number and the file's
// position. It skips a file that cannot be parsed and returns the errors of those files.
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

// Enter records the definition that a statement makes, if its database is called by name, and the event namespace it
// declares. It goes into the top blocks that only group definitions, into a file that is one definition, and into the
// body of a definition that takes parameters, whose typings it records.
func (declarer *declarer) Enter(tree *jomini.Tree, statement int32, context *Context) bool {
	if declarer.body != nil {
		declarer.body.scripted.typings = append(declarer.body.scripted.typings, listTypings(tree, statement, context)...)
		return true
	}
	if context.Defines && context.Category == CategoryDefinition {
		declarer.declare(*context.Definition, tree, statement)
	}
	if context.Parent != nil && slices.Contains(context.Parent.Namespaces, statement) {
		declarer.namespaces = append(declarer.namespaces, tree.Unquoted(tree.Statements[statement].Value))
	}
	return statement == 0 || declarer.body != nil || !context.Defines && tree.Statements[statement].Parent == 0
}

// declare records a definition that is called by name. For each caller of its database it records the scripted
// definition with the parameters its body writes; one that takes parameters is recorded when the walk leaves its body.
// If a widget calls it, it also records the widget type, and the type that widget is built on. It also records what
// the name of a modifier type takes.
func (declarer *declarer) declare(definition Definition, tree *jomini.Tree, statement int32) {
	database := declarer.schema.Databases[definition.Database]
	if database == nil {
		return
	}
	if database.Row.BoolKey != "" {
		declarer.modifiers[definition.Name] = makeModifierField(tree, statement, database.Row.BoolKey)
	}
	base := tree.Unquoted(tree.Statements[statement].Tag)
	if base != "" && slices.Contains(database.Callable.Callers, game.GUI.TagCalls) {
		declarer.widgets = append(declarer.widgets, base)
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
			declarer.widgets = append(declarer.widgets, definition.Name)
		}
	}
	if database.Callable.Parameters {
		declarer.body = pending
		return
	}
	declarer.keep(pending)
}

// keep records a scripted definition under each of its keys.
func (declarer *declarer) keep(pending *body) {
	for _, key := range pending.keys {
		declarer.scripted[key] = pending.scripted
	}
}

// makeModifierField returns what the name of a modifier type takes: yes or no if its definition writes yes at the given
// key, else a number or a script value.
func makeModifierField(tree *jomini.Tree, statement int32, key string) *Field {
	expected := Expected{Type: ValueNumber, Values: true}
	if value, _ := tree.FindChildValue(statement, key); value == yes {
		expected = Expected{Type: ValueBool}
	}
	return &Field{Claims: Claims{Value: Claim[Expected]{Value: expected, Source: game.SourceDefinitions}}}
}

// makeScripted returns the scripted definition that a statement makes, with its parameters and saves if the database's
// definitions take parameters.
func makeScripted(database *Database, tree *jomini.Tree, statement int32) ScriptedDefinition {
	scripted := ScriptedDefinition{Database: database.Name}
	if database.Callable.Parameters {
		scripted.Parameters = ListParameters(tree, statement)
		scripted.Saves, scripted.relays = listSaves(tree, statement)
	}
	return scripted
}

// ListParameters returns the parameters a scripted definition's body writes in its keys, tags and values, as the
// fields a call to it takes, which the definition declares. A parameter written only in a comment is none. A
// parameter whose every use writes a default has the first one as its default.
func ListParameters(tree *jomini.Tree, statement int32) *Field {
	parameters := &Field{}
	undefaulted := map[string]bool{}
	for position := statement + 1; position < tree.Statements[statement].Last; position++ {
		current := tree.Statements[position]
		for _, span := range []jomini.Span{current.Key, current.Tag, current.Value} {
			for _, parameter := range parameterRe.FindAllStringSubmatch(tree.Text(span), -1) {
				argument := parameters.buildChild(parameter[1], game.SourceDefinitions)
				argument.Vanilla.Count++
				switch {
				case !strings.Contains(parameter[0], "|"):
					undefaulted[parameter[1]] = true
					argument.Default = Claim[string]{}
				case !undefaulted[parameter[1]] && !argument.Default.IsStated():
					argument.Default = Claim[string]{Value: parameter[2], Source: game.SourceVanilla}
				}
			}
		}
	}
	return parameters
}

// listSaves returns the parameters a scripted definition's body saves a name under, with the link that reads each name,
// and the parameters it passes on to calls of other scripted definitions. Only a value that is one whole parameter
// counts.
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
		if link, _, ok := findLinked(game.LinkMakers, holder, key); ok {
			saves[parameter] = link
		} else if current.Parent != statement {
			relays = append(relays, relay{Callee: holder, Argument: key, Parameter: parameter})
		}
	}
	return saves, relays
}

// listTypings returns the typings that one statement of a scripted definition's body writes. A key or a value that is
// one parameter after a link prefix is one. So is a value that is one parameter at a key with a field of its own. A
// link's key has its holder's field, and an argument of a call is the call's relay, so neither is one.
func listTypings(tree *jomini.Tree, statement int32, context *Context) []typing {
	current := tree.Statements[statement]
	var typings []typing
	for _, span := range []jomini.Span{current.Key, current.Value} {
		prefix, label, prefixed := jomini.SplitPrefixed(tree.Unquoted(span))
		if parameter, whole := findParameter(label); prefixed && whole {
			typings = append(typings, typing{Parameter: parameter, Prefix: prefix})
		}
	}
	parameter, whole := findParameter(tree.Unquoted(current.Value))
	if whole && context.Field != nil && context.Category != CategoryLink && context.Parent.Callee == nil {
		typings = append(typings, typing{Parameter: parameter, Field: context.Field})
	}
	return typings
}

// findParameter returns the parameter that a text is, written whole, and true. It returns false for a text that is
// not one parameter.
func findParameter(text string) (string, bool) {
	parameter := parameterRe.FindStringSubmatch(text)
	if parameter == nil || parameter[0] != text {
		return "", false
	}
	return parameter[1], true
}

// Leave records the scripted definition whose body the walk leaves, with the typings its body writes.
func (declarer *declarer) Leave(tree *jomini.Tree, statement int32, context *Context) {
	if declarer.body != nil && declarer.body.statement == statement {
		declarer.keep(declarer.body)
		declarer.body = nil
	}
}

// build gives the schema what the walked files declared. A later definition of a name replaces an earlier one.
func (declarer *declarer) build() {
	maps.Copy(declarer.schema.ScriptedDefinitions, declarer.scripted)
	maps.Copy(declarer.schema.ModifierFields, declarer.modifiers)
	for _, widget := range declarer.widgets {
		declarer.schema.WidgetTypes[widget] = true
	}
	for _, namespace := range declarer.namespaces {
		declarer.schema.Namespaces[namespace] = true
	}
}
