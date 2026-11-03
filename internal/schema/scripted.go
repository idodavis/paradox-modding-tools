// scripted.go reads the scripted definitions and the modifier types that the workspace's files declare. It also finds
// the parameters a definition takes and the scopes it saves.

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

// A declarer is the visitor of a walk over one file. It records the definitions that script and GUI files call by name.
// It reads only the top statements.
type declarer struct {
	schema    *Schema
	scripted  map[string]ScriptedDefinition // by the word that calls it and its name
	widgets   []string                      // the widget types the file defines or builds on
	modifiers map[string]*Field             // by modifier type the file defines: what its name takes
}

// parameterRe matches a parameter that a scripted definition's body writes, with its default if it has one.
var parameterRe = regexp.MustCompile(`\$([A-Za-z0-9_]+)(?:\|([^$]*))?\$`)

// SetScripted reads the scripted definitions and the modifier types that the given files of the workspace's mods
// declare, in place of what they declared before, so that a call to one is a known call, as a call to one of the
// game's is, and a modifier type's name takes what its definition says. It reads the script files of the databases
// that script calls by name or whose definitions say what their names take, parsed with the given function. A file
// that cannot be parsed, or is gone, declares nothing.
func (schema *Schema) SetScripted(changed []files.File, parse func(path string) (*jomini.Tree, error)) {
	declaring := slices.DeleteFunc(slices.Clone(changed), func(file files.File) bool {
		return file.Format != files.FormatScript || !schema.declares(file)
	})
	declarers := schema.listDeclarers(declaring, parse)
	next := &modScripted{
		byFile: map[string]*declarer{}, all: map[string]ScriptedDefinition{}, modifiers: map[string]*Field{},
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
	// each definition gets its own copy of its saves, which only this table's search for saves adds to
	for _, key := range slices.Sorted(maps.Keys(next.byFile)) {
		for name, scripted := range next.byFile[key].scripted {
			scripted.Saves = maps.Clone(scripted.Saves)
			next.all[name] = scripted
		}
		maps.Copy(next.modifiers, next.byFile[key].modifiers)
	}
	schema.resolveSaves(next.all, next.all)
	schema.mods.Store(next)
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

// resolveSaves gives each scripted definition of a table the parameters it saves by passing them on to a call of
// another one that saves them, until no more are found. A call finds the game's definitions, then the given mods'. A
// parameter keeps the first link found.
func (schema *Schema) resolveSaves(table, mods map[string]ScriptedDefinition) {
	for found := true; found; {
		found = false
		for _, scripted := range table {
			for _, relay := range scripted.relays {
				_, saved := scripted.Saves[relay.Parameter]
				callee, _, ok := schema.findScripted(mods, relay.Callee)
				if link, saves := callee.Saves[relay.Argument]; ok && saves && !saved {
					scripted.Saves[relay.Parameter] = link
					found = true
				}
			}
		}
	}
}

// findScripted returns the scripted definition of a name and the word it is called through, trying each word that
// calls definitions in alphabetical order. It returns false if no word finds one.
func (schema *Schema) findScripted(mods map[string]ScriptedDefinition, key string) (ScriptedDefinition, string, bool) {
	for _, caller := range slices.Sorted(maps.Keys(schema.Callees)) {
		if callee, ok := schema.lookupScripted(mods, caller, key); ok {
			return callee, caller, true
		}
	}
	return ScriptedDefinition{}, "", false
}

// lookupScripted returns the scripted definition of a name called through the given word: the game's, else one of the
// given mods'. A mod's never takes the place of a command the dumps declare by that name. A name in another case finds
// the definition only in a database that ignores case.
func (schema *Schema) lookupScripted(mods map[string]ScriptedDefinition, caller, key string) (ScriptedDefinition, bool) {
	if callee, ok := schema.lookupDefinition(schema.ScriptedDefinitions, caller, key); ok {
		return callee, true
	}
	if _, command := schema.FindCommand(key); command {
		return ScriptedDefinition{}, false
	}
	return schema.lookupDefinition(mods, caller, key)
}

// lookupDefinition returns the scripted definition of a name called through the given word in one table. A name in
// another case finds the definition only in a database that ignores case.
func (schema *Schema) lookupDefinition(table map[string]ScriptedDefinition, caller, key string) (ScriptedDefinition, bool) {
	if callee, ok := table[caller+":"+key]; ok {
		return callee, true
	}
	callee, ok := table[caller+":"+strings.ToLower(key)]
	database := schema.Databases[callee.Database]
	return callee, ok && database != nil && database.Callable.FoldsCase
}

// declares reports whether a file declares definitions that script or GUI files call by name. A GUI file does, and so
// does a file of a database that has callers, or a file of a database whose definitions say what their names take.
func (schema *Schema) declares(file files.File) bool {
	if file.Format == files.FormatGUI {
		return true
	}
	database, ok := schema.FindDatabase(file.Rel)
	return ok && (len(database.Callable.Callers) > 0 || database.Row.BoolKey != "")
}

// eachParsed parses each file of a listing in parallel with the first given function, and calls the second on each
// parse with the worker's number and the file's position. A file that cannot be parsed is skipped. It returns the
// errors of the files that could not be parsed.
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

// Enter records the definition that a statement makes, if its database is called by name. It goes into the top blocks
// that only group definitions, and into a file that is one definition.
func (declarer *declarer) Enter(tree *jomini.Tree, statement int32, context *Context) bool {
	if context.Defines && context.Category == CategoryDefinition {
		declarer.declare(*context.Definition, tree, statement)
	}
	return statement == 0 || !context.Defines && tree.Statements[statement].Parent == 0
}

// declare records a definition that is called by name. For each word that calls its database it records the scripted
// definition with the parameters its body writes. If a widget calls it, it also records the widget type, and the type
// that widget is built on. It also records what the name of a modifier type takes.
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
	scripted := makeScripted(database, tree, statement)
	for _, caller := range database.Callable.Callers {
		declarer.scripted[caller+":"+definition.Name] = scripted
		if database.Callable.FoldsCase {
			declarer.scripted[caller+":"+strings.ToLower(definition.Name)] = scripted
		}
		if caller == game.GUI.TagCalls {
			declarer.widgets = append(declarer.widgets, definition.Name)
		}
	}
}

// makeModifierField returns what the name of a modifier type takes in a modifiers block, as the type's definition
// says: yes or no where it writes yes at the given key, else a number or a script value.
func makeModifierField(tree *jomini.Tree, statement int32, key string) *Field {
	expected := Expected{Type: ValueNumber, Values: true}
	if value, _ := tree.FindChildValue(statement, key); value == yes {
		expected = Expected{Type: ValueBool}
	}
	return &Field{Claims: Claims{Value: Claim[Expected]{Value: expected, Source: game.SourceDefinitions}}}
}

// makeScripted returns the scripted definition that a statement makes. If the database's definitions take parameters,
// it has the parameters the statement's body writes and those the body saves names under.
func makeScripted(database *Database, tree *jomini.Tree, statement int32) ScriptedDefinition {
	scripted := ScriptedDefinition{Database: database.Name}
	if database.Callable.Parameters {
		scripted.Parameters = ListParameters(tree, statement)
		scripted.Saves, scripted.relays = listSaves(tree, statement)
	}
	return scripted
}

// ListParameters returns the parameters a scripted definition's body writes in its keys, tags and values, as the
// fields a call to it takes. A parameter written only in a comment is none. A parameter whose every use writes a
// default has the first one as its default.
func ListParameters(tree *jomini.Tree, statement int32) *Field {
	parameters := &Field{}
	undefaulted := map[string]bool{}
	for position := statement + 1; position < tree.Statements[statement].Last; position++ {
		current := tree.Statements[position]
		for _, span := range []jomini.Span{current.Key, current.Tag, current.Value} {
			for _, parameter := range parameterRe.FindAllStringSubmatch(tree.Text(span), -1) {
				argument := parameters.buildChild(parameter[1], game.SourceVanilla)
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

// listSaves returns the parameters a scripted definition's body saves a name under, with the link that reads each
// name, and the parameters it passes on to calls of other scripted definitions. Only a value that is one whole
// parameter counts.
func listSaves(tree *jomini.Tree, statement int32) (map[string]string, []relay) {
	saves := map[string]string{}
	var relays []relay
	for position := statement + 1; position < tree.Statements[statement].Last; position++ {
		current := tree.Statements[position]
		value := tree.Unquoted(current.Value)
		parameter := parameterRe.FindStringSubmatch(value)
		if parameter == nil || parameter[0] != value || current.Key.IsEmpty() {
			continue
		}
		key, holder := tree.Unquoted(current.Key), tree.Unquoted(tree.Statements[current.Parent].Key)
		if link, _, ok := findLinked(game.LinkMakers, holder, key); ok {
			saves[parameter[1]] = link
		} else if current.Parent != statement {
			relays = append(relays, relay{Callee: holder, Argument: key, Parameter: parameter[1]})
		}
	}
	return saves, relays
}

// Leave does nothing.
func (declarer *declarer) Leave(tree *jomini.Tree, statement int32, context *Context) {}

// build gives the schema what the walked files declared. A later definition of a name replaces an earlier one.
func (declarer *declarer) build() {
	maps.Copy(declarer.schema.ScriptedDefinitions, declarer.scripted)
	maps.Copy(declarer.schema.ModifierFields, declarer.modifiers)
	for _, widget := range declarer.widgets {
		declarer.schema.WidgetTypes[widget] = true
	}
}
