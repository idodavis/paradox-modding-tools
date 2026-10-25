package schema

import (
	"cmp"
	"errors"
	"maps"
	"path"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/loc"
)

// A declarer is the visitor of a walk over one file. It records the definitions that script and GUI files call by name.
// It reads only the top statements.
type declarer struct {
	schema   *Schema
	scripted map[string]ScriptedDefinition // by the word that calls it and its name
	widgets  []string                      // the widget types the file defines or builds on
}

// A counter is the visitor of one worker's counting walk. It counts what the files write into fields of its own, apart
// from the schema.
type counter struct {
	schema      *Schema
	holders     []holder
	path        string // the file being counted
	colorSuffix string // the ending of the keys that hold an untagged color in the file being counted
	fields      map[*Field]*Field
	arguments   map[*Command]*Field
	definitions map[string]*Field          // by database
	defined     map[string]map[string]bool // the names each database defines, by database
	messages    map[string]bool            // every message key the localization files write, in any language
	prefixed    map[string]map[string]int
	examples    map[string][]Example       // by the name written: a command's, or a data function's as Type.Function
	scopes      map[string]map[string]bool // by command: the scopes the game's own files run it in
}

// A holder is where the keys of one statement are counted while the counter is inside the statement.
type holder struct {
	field  *Field          // nil where the keys are no fields
	shaped bool            // the keys of the statement tell the shape of its field's blocks
	keys   map[string]bool // the keys a plain definition has written; nil where the statement is no plain definition
}

// parameterRe matches a parameter that a scripted definition's body writes, with its default if it has one.
var parameterRe = regexp.MustCompile(`\$([A-Za-z0-9_]+)(?:\|[^$]*)?\$`)

// valueTests are the types a count can give a field, in the order they are tried. A field gets the first type that
// every value written at it fits.
var valueTests = []ValueType{ValueBool, ValueNumber, ValueDate, ValueCall, ValueText}

// exampleCount is how many places the count keeps for each command and data function.
const exampleCount = 5

// shapeAmbiguous is the shape of a key that fits more than one kind of block. It is no real shape.
const shapeAmbiguous = "ambiguous"

// CountVanilla counts what the game's own files write and adds it to the schema. A key written under a database's
// definitions becomes a field with game.SourceVanilla. The count also gives each field its shape, its value and the
// database its block's keys name, resolves the names written after each prefix, and finds the colors. It parses the
// script files of the given listing in parallel with the given function, which must be safe to call at once. It reads
// the localization files for their message keys. It returns an error only if no file of the listing can be parsed.
func (schema *Schema) CountVanilla(listing []files.File, parse func(path string) (*jomini.Tree, error)) error {
	localizing := slices.DeleteFunc(slices.Clone(listing), func(file files.File) bool {
		return file.Format != files.FormatLocalization
	})
	listing = listVanilla(listing)
	// Files that declare callables and widget types are read first, so that a key under a call counts as a call.
	declaring := slices.DeleteFunc(slices.Clone(listing), func(file files.File) bool { return !schema.declares(file) })
	declarers := make([]*declarer, len(declaring))
	eachParsed(declaring, parse, func(_, index int, tree *jomini.Tree) {
		declarers[index] = &declarer{schema: schema, scripted: map[string]ScriptedDefinition{}}
		schema.Walk(tree, declaring[index], declarers[index])
	})
	for _, declarer := range declarers {
		if declarer != nil {
			declarer.build()
		}
	}
	counters := make([]*counter, files.Workers)
	for worker := range counters {
		counters[worker] = &counter{
			schema: schema, fields: map[*Field]*Field{}, arguments: map[*Command]*Field{},
			definitions: map[string]*Field{}, defined: map[string]map[string]bool{}, messages: map[string]bool{},
			prefixed: map[string]map[string]int{}, examples: map[string][]Example{}, scopes: map[string]map[string]bool{},
		}
	}
	eachParsed(localizing, func(path string) (*loc.File, error) {
		text, err := files.ReadText(path)
		if err != nil {
			return nil, err
		}
		return loc.Parse(text.Text, path), nil
	}, func(worker, _ int, file *loc.File) {
		for _, message := range file.Messages {
			// The copy keeps the key from holding the file's text in memory.
			if !counters[worker].messages[message.Key] {
				counters[worker].messages[strings.Clone(message.Key)] = true
			}
		}
	})
	failures := eachParsed(listing, parse, func(worker, index int, tree *jomini.Tree) {
		counters[worker].resolveFile(listing[index])
		schema.Walk(tree, listing[index], counters[worker])
	})
	for _, other := range counters[1:] {
		counters[0].add(other)
	}
	counters[0].resolveCounts()
	// A file that cannot be parsed is left out. It is an error only if none can be parsed.
	if len(failures) == len(listing) && len(failures) > 0 {
		return errors.Join(failures...)
	}
	return nil
}

// listVanilla returns the files of the given listing that hold script: script, GUI, asset, font and GFX files.
func listVanilla(listing []files.File) []files.File {
	return slices.DeleteFunc(slices.Clone(listing), func(file files.File) bool { return !file.Format.IsScript() })
}

// declares reports whether a file declares definitions that script or GUI files call by name. A GUI file does, and so
// does a file of a database that has callers.
func (schema *Schema) declares(file files.File) bool {
	if file.Format == files.FormatGUI {
		return true
	}
	database, ok := schema.FindDatabase(file.Rel)
	return ok && len(database.Callable.Callers) > 0
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
// that widget is built on.
func (declarer *declarer) declare(definition Definition, tree *jomini.Tree, statement int32) {
	database := declarer.schema.Databases[definition.Database]
	if database == nil {
		return
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

// makeScripted returns the scripted definition that a statement makes. It has the parameters the statement's body
// writes if the database's definitions take parameters.
func makeScripted(database *Database, tree *jomini.Tree, statement int32) ScriptedDefinition {
	scripted := ScriptedDefinition{Database: database.Name}
	if database.Callable.Parameters {
		scripted.Parameters = &Field{}
		for _, parameter := range parameterRe.FindAllStringSubmatch(tree.Text(tree.Statements[statement].Block), -1) {
			argument := scripted.Parameters.buildChild(parameter[1])
			argument.Source = game.SourceVanilla
			argument.Vanilla.Count++
		}
	}
	return scripted
}

// Leave does nothing.
func (declarer *declarer) Leave(tree *jomini.Tree, statement int32, context *Context) {}

// build gives the schema what the walked files declared. A later definition of a name replaces an earlier one.
func (declarer *declarer) build() {
	maps.Copy(declarer.schema.ScriptedDefinitions, declarer.scripted)
	for _, widget := range declarer.widgets {
		declarer.schema.WidgetTypes[widget] = true
	}
}

// resolveFile starts the count of the given file. Its type says which keys hold an untagged color.
func (counter *counter) resolveFile(file files.File) {
	counter.holders = counter.holders[:0]
	counter.path = file.Path
	counter.colorSuffix = game.FileTypes[strings.ToLower(path.Ext(file.Rel))].ColorSuffix
}

// Enter counts a statement. It counts the name the statement defines, the names it writes after prefixes, the command
// it writes, its value and, under a definition's fields, the field it is.
func (counter *counter) Enter(tree *jomini.Tree, statement int32, context *Context) bool {
	var parent holder
	if last := len(counter.holders) - 1; last >= 0 {
		parent = counter.holders[last]
	}
	counter.countDefinition(context)
	counter.countPrefixes(tree, statement)
	counter.countCommand(tree, statement, context)
	counter.countCalls(tree, statement, context)
	if parent.shaped {
		counter.countShape(parent.field, tree, statement, context)
	}
	var next holder
	current := tree.Statements[statement]
	block := current.IsBlock()
	field := counter.countField(parent, tree, statement, context)
	if valueField := counter.findValueField(parent, field, current, context); valueField != nil {
		valueField.Vanilla.Values = addCounts(valueField.Vanilla.Values, map[string]int{tree.Text(current.Value): 1})
	}
	switch {
	// A color block's numbers are its parts, not values of its field. Leaving them out lets a field that is written as
	// a named color or as a color block name the named colors.
	case field != nil && (!block || !counter.isColor(tree, statement, tree.Text(current.Key))):
		next.field = field
	case context.Defines && context.Category == CategoryDefinition && context.Definition.Mode == "":
		next = counter.makeDefinitionHolder(context.Database)
	case context.Defines && context.Category == CategoryDefinition, counter.isFrame(context):
		next.field = mirror(counter.definitions, context.Database)
	case context.Category == CategoryCommand && block:
		// A command's block holds its arguments and has no shape of its own.
		next.field = mirror(counter.arguments, context.Command)
		block = false
	case context.Category == CategoryKeyword && block && context.Field != nil:
		// A keyword's block, such as a random_list branch, holds fields of the form its holder gives.
		next.field = mirror(counter.fields, context.Field)
		block = false
	}
	if next.field != nil && block {
		next.shaped = true
	}
	counter.holders = append(counter.holders, next)
	return true
}

// countDefinition records the name a statement defines, if it defines one.
func (counter *counter) countDefinition(context *Context) {
	if !context.Defines || context.Category != CategoryDefinition {
		return
	}
	if counter.defined[context.Database] == nil {
		counter.defined[context.Database] = map[string]bool{}
	}
	counter.defined[context.Database][context.Definition.Name] = true
}

// countPrefixes records the names that a statement writes after a prefix, in its key and in its value. Only a prefix
// with no database yet is counted.
func (counter *counter) countPrefixes(tree *jomini.Tree, statement int32) {
	current := tree.Statements[statement]
	for _, text := range []string{tree.Unquoted(current.Key), tree.Unquoted(current.Value)} {
		for _, link := range jomini.SplitChain(text) {
			prefix, label, ok := jomini.SplitPrefixed(link)
			if database, exists := counter.schema.Prefixes[prefix]; ok && exists && database == "" {
				counter.prefixed[prefix] = addCounts(counter.prefixed[prefix], map[string]int{label: 1})
			}
		}
	}
}

// addCounts adds each count of the second map to the same key of the first and returns the first. It makes the first
// map if it is nil and the second has counts.
func addCounts(total, added map[string]int) map[string]int {
	if len(added) > 0 && total == nil {
		total = map[string]int{}
	}
	for key, count := range added {
		total[key] += count
	}
	return total
}

// countCommand keeps the place of a statement whose key is a command or a link the dumps declare. It also keeps the
// scope the command runs in there, if known. A chain of links counts as its first link, and a prefixed link as its
// prefix.
func (counter *counter) countCommand(tree *jomini.Tree, statement int32, context *Context) {
	if context.Command == nil || context.Category != CategoryCommand && context.Category != CategoryLink {
		return
	}
	key := tree.Statements[statement].Key
	name, _, _ := strings.Cut(jomini.SplitChain(tree.Unquoted(key))[0], ":")
	example := Example{Path: counter.path, Line: tree.Lines.ToPosition(key.Start).Line}
	counter.examples[name] = keepExample(counter.examples[name], example)
	if scope := context.Parent.Scope; scope != "" {
		if counter.scopes[name] == nil {
			counter.scopes[name] = map[string]bool{}
		}
		counter.scopes[name][scope] = true
	}
}

// keepExample adds a place to a list of places. It returns the first places by path and then line, up to exampleCount.
// A place already in the list is not added twice.
func keepExample(examples []Example, example Example) []Example {
	// Sorting by path keeps the result the same whichever worker read which file.
	position, exists := slices.BinarySearchFunc(examples, example, func(kept, added Example) int {
		return cmp.Or(strings.Compare(kept.Path, added.Path), cmp.Compare(kept.Line, added.Line))
	})
	if exists || position == exampleCount {
		return examples
	}
	examples = slices.Insert(examples, position, example)
	return examples[:min(len(examples), exampleCount)]
}

// countCalls keeps the place of each data function that the data call of a statement names, under the type that
// declares it.
func (counter *counter) countCalls(tree *jomini.Tree, statement int32, context *Context) {
	if context.ValueGroup != CategoryCall {
		return
	}
	value := tree.Statements[statement].Value
	call, ok := ParseCall(tree, value)
	if !ok {
		return
	}
	example := Example{Path: counter.path, Line: tree.Lines.ToPosition(value.Start).Line}
	for i, owner := range counter.schema.listCallOwners(call, context.Datacontext) {
		if owner != "" {
			name := owner + "." + call.Steps[i].Text
			counter.examples[name] = keepExample(counter.examples[name], example)
		}
	}
}

// countShape counts the shape that a key in a block of a field tells. A call to a scripted definition counts the
// shapes it allows instead. A key that tells nothing counts nothing.
func (counter *counter) countShape(field *Field, tree *jomini.Tree, statement int32, context *Context) {
	if tree.Statements[statement].Key.IsEmpty() {
		return
	}
	key := tree.Unquoted(tree.Statements[statement].Key)
	if context.Category == CategoryCall {
		if proofs, defined := counter.resolveProofs(key); defined {
			if proofs != "" {
				field.Vanilla.Proofs = addCounts(field.Vanilla.Proofs, map[string]int{proofs: 1})
			}
			return
		}
	}
	if shape := counter.resolveKeyShape(key, context); shape != "" {
		field.Vanilla.Shapes = addCounts(field.Vanilla.Shapes, map[string]int{shape: 1})
	}
}

// resolveProofs returns the shapes that a call to a scripted definition allows. They are the shapes that every database
// defining the name allows, sorted and joined by spaces. It is "" if the databases share none. It returns false if no
// database defines the name.
func (counter *counter) resolveProofs(key string) (string, bool) {
	var shapes []string
	defined := false
	for caller := range counter.schema.Callees {
		scripted, ok := counter.schema.ScriptedDefinitions[caller+":"+key]
		if !ok || caller == game.AnyCall {
			continue
		}
		proves := game.Callables[scripted.Database].Proves
		if defined {
			shapes = slices.DeleteFunc(shapes, func(shape string) bool { return !slices.Contains(proves, shape) })
		} else {
			shapes = slices.Clone(proves)
		}
		defined = true
	}
	slices.Sort(shapes)
	return strings.Join(shapes, " "), defined
}

// resolveKeyShape returns the shape of the block that holds the given key. A keyword gives its holder, an operator
// gives value, and a command declared as one of triggers or effects gives that. A key nothing else explains gives
// modifiers or fields. A command that fits more than one kind gives shapeAmbiguous. A link, a list iterator, a
// parameter and a call give "".
func (counter *counter) resolveKeyShape(key string, context *Context) string {
	lower := strings.ToLower(key)
	for _, keyword := range game.Keywords {
		if keyword.Holder != "" && keyword.Key == lower {
			if keyword.Class == "" {
				return ""
			}
			return keyword.Holder
		}
	}
	switch {
	case context.Category == CategoryKeyword, context.Category == CategoryParameter:
		return ""
	case game.IsOperator(key):
		return game.ShapeValue
	case context.Category == CategoryCommand && context.Command != nil:
		return counter.resolveCommandShape(key, context.Command)
	case counter.isUnexplained(key, context):
		return counter.resolveFieldShape(key)
	}
	return ""
}

// resolveCommandShape returns the shape of the block that holds the given command. A trigger gives triggers and an
// effect gives effects. A command declared as both, one that is also a modifier and one written in a case the dumps
// do not use give shapeAmbiguous. A list iterator and a command declared as neither give "".
func (counter *counter) resolveCommandShape(key string, command *Command) string {
	if _, iterator := game.MatchListIterator(key); iterator {
		return ""
	}
	triggers := slices.Contains(command.Declared, game.DeclaredTrigger)
	effects := slices.Contains(command.Declared, game.DeclaredEffect)
	_, modifier := counter.schema.Modifiers[key]
	switch {
	case !triggers && !effects:
		return ""
	case triggers && effects, modifier, counter.schema.Commands[key] == nil:
		return shapeAmbiguous
	case triggers:
		return game.ShapeTriggers
	}
	return game.ShapeEffects
}

// isUnexplained reports whether only a field can explain a key. No rule fits it, or its rule says it is a field, or it
// is a call to a name that no definition has.
func (counter *counter) isUnexplained(key string, context *Context) bool {
	switch context.Category {
	case CategoryNone, CategoryField, CategoryReference:
		return true
	case CategoryCall:
		_, iterator := game.MatchListIterator(key)
		return context.Field == nil && (!iterator || counter.schema.Lists == "")
	}
	return false
}

// resolveFieldShape returns the shape of the block that holds a key nothing else explains. A modifier that is no
// command gives modifiers, one that is a command too gives shapeAmbiguous, and any other key gives fields.
func (counter *counter) resolveFieldShape(key string) string {
	_, modifier := counter.schema.Modifiers[key]
	_, command := counter.schema.FindCommand(key)
	switch {
	case modifier && command:
		return shapeAmbiguous
	case modifier:
		return game.ShapeModifiers
	}
	return game.ShapeFields
}

// countField counts a statement as a write of the field its key names under the holder's field. It returns that
// field, or nil if the statement is no field: one written under no field, a definition, a command, a link, a call to
// a definition or a value written alone.
func (counter *counter) countField(parent holder, tree *jomini.Tree, statement int32, context *Context) *Field {
	current := tree.Statements[statement]
	key := tree.Unquoted(current.Key)
	if parent.field == nil || current.Key.IsEmpty() || isOwnDefinition(context) || !counter.isUnexplained(key, context) {
		return nil
	}
	rawKey := key
	if game.IsNumberKey(key) {
		key = game.NumberKey
	}
	field := parent.field.buildChild(key)
	field.Vanilla.Count++
	if parent.keys != nil && !parent.keys[key] {
		parent.keys[key] = true
		field.Vanilla.Definitions++
	}
	if current.IsBlock() {
		field.Vanilla.Blocks++
		if counter.isColor(tree, statement, rawKey) {
			field.Vanilla.Colors++
		}
	}
	return field
}

// isOwnDefinition reports whether a statement defines a name of its own. An alias is only a second name of the
// definition it sits in, so it does not count.
func isOwnDefinition(context *Context) bool {
	return context.Defines && !context.Definition.Alias
}

// isColor reports whether a block is a color. A block with a color tag is one. A bare block of three or four numbers is
// one only under a key that ends as the file type says.
func (counter *counter) isColor(tree *jomini.Tree, statement int32, key string) bool {
	_, ok := game.ToColor(tree, statement)
	return ok && (!tree.Statements[statement].Tag.IsEmpty() ||
		counter.colorSuffix != "" && strings.HasSuffix(key, counter.colorSuffix))
}

// findValueField returns the field that counts the value a statement writes. That is the field its key names, the
// arguments of the command its key names, or the field of the block that holds a value written alone. It returns nil
// if the statement writes no value, the value is a definition, or the database names no definitions.
func (counter *counter) findValueField(parent holder, field *Field, current jomini.Statement, context *Context) *Field {
	switch {
	case current.IsBlock() || current.Value.IsEmpty() || isOwnDefinition(context) || counter.isInstanceList(context):
		return nil
	case field != nil:
		return field
	case context.Category == CategoryCommand && context.Command != nil:
		return mirror(counter.arguments, context.Command)
	case current.IsListed():
		return parent.field
	}
	return nil
}

// isInstanceList reports whether a statement is written in a database that names no definitions. Such files list
// instances, so their values are not kept.
func (counter *counter) isInstanceList(context *Context) bool {
	database := counter.schema.Databases[context.Database]
	return database != nil && database.Row.NamedBy == "nothing"
}

// mirror returns the field that a worker counts into for the given key. It makes the field if there is none.
func mirror[K comparable](fields map[K]*Field, key K) *Field {
	if fields[key] == nil {
		fields[key] = &Field{}
	}
	return fields[key]
}

// makeDefinitionHolder returns the holder of a plain definition of a database and counts the definition.
func (counter *counter) makeDefinitionHolder(database string) holder {
	root := mirror(counter.definitions, database)
	root.Vanilla.Definitions++
	return holder{field: root, keys: map[string]bool{}}
}

// isFrame reports whether a statement is the block of a definition that has no name. Its keys are fields of its
// database's definition.
func (counter *counter) isFrame(context *Context) bool {
	database := counter.schema.Databases[context.Database]
	return database != nil && context.Field == database.Definition && context.Definition == nil
}

// Leave ends a statement.
func (counter *counter) Leave(tree *jomini.Tree, statement int32, context *Context) {
	counter.holders = counter.holders[:len(counter.holders)-1]
}

// add adds the count of another worker to this one's.
func (counter *counter) add(other *counter) {
	for field, counted := range other.fields {
		mirror(counter.fields, field).add(counted)
	}
	for command, counted := range other.arguments {
		mirror(counter.arguments, command).add(counted)
	}
	for database, counted := range other.definitions {
		mirror(counter.definitions, database).add(counted)
	}
	for database, labels := range other.defined {
		if counter.defined[database] == nil {
			counter.defined[database] = map[string]bool{}
		}
		maps.Copy(counter.defined[database], labels)
	}
	maps.Copy(counter.messages, other.messages)
	for prefix, labels := range other.prefixed {
		counter.prefixed[prefix] = addCounts(counter.prefixed[prefix], labels)
	}
	for name, examples := range other.examples {
		for _, example := range examples {
			counter.examples[name] = keepExample(counter.examples[name], example)
		}
	}
	for name, scopes := range other.scopes {
		if counter.scopes[name] == nil {
			counter.scopes[name] = map[string]bool{}
		}
		maps.Copy(counter.scopes[name], scopes)
	}
}

// add adds the counts of another field and its children to this field's. A child that no source declared gets
// game.SourceVanilla.
func (field *Field) add(counted *Field) {
	field.Vanilla.Count += counted.Vanilla.Count
	field.Vanilla.Blocks += counted.Vanilla.Blocks
	field.Vanilla.Colors += counted.Vanilla.Colors
	field.Vanilla.Definitions += counted.Vanilla.Definitions
	field.Vanilla.Values = addCounts(field.Vanilla.Values, counted.Vanilla.Values)
	field.Vanilla.Shapes = addCounts(field.Vanilla.Shapes, counted.Vanilla.Shapes)
	field.Vanilla.Proofs = addCounts(field.Vanilla.Proofs, counted.Vanilla.Proofs)
	for key, child := range counted.Children {
		target := field.buildChild(key)
		if target.Source == game.SourceNone {
			target.Source = game.SourceVanilla
		}
		target.add(child)
	}
}

// resolveCounts gives the schema what the count found. Counted fields join the databases and commands, each with the
// claims its counts support, and the prefixes are resolved from the names defined. A database that only the walk
// names, such as an asset file's entity, is made here. Each command and data function gets its examples, and each
// command the scopes it runs in.
func (counter *counter) resolveCounts() {
	row := game.LookupGame(counter.schema.Game)
	for field, counted := range counter.fields {
		field.add(counted)
	}
	for name, command := range counter.schema.Commands {
		command.Examples = counter.examples[name]
		command.VanillaScopes = slices.Sorted(maps.Keys(counter.scopes[name]))
	}
	for typeName, dataType := range counter.schema.DataTypes {
		for name, function := range dataType.Functions {
			function.Examples = counter.examples[typeName+"."+name]
			dataType.Functions[name] = function
		}
	}
	for command, counted := range counter.arguments {
		command.makeArguments(game.SourceVanilla).add(counted)
	}
	for _, database := range slices.Sorted(maps.Keys(counter.defined)) {
		counter.schema.buildDatabase(row, database, "", game.Database{})
	}
	for database, counted := range counter.definitions {
		counter.schema.buildDatabase(row, database, "", game.Database{})
		counter.schema.Databases[database].Definition.add(counted)
	}
	counter.schema.walkFields(counter.resolveClaims)
	for prefix, labels := range counter.prefixed {
		counter.resolvePrefix(prefix, labels)
	}
}

// walkFields calls the function on every field of every database's definitions and of every command's arguments.
func (schema *Schema) walkFields(visit func(*Field)) {
	each := func(root *Field) {
		visit(root)
		walkField(root, func(_ string, field *Field) { visit(field) })
	}
	for _, database := range schema.Databases {
		each(database.Definition)
	}
	for _, command := range schema.Commands {
		if command.Arguments != nil {
			each(command.Arguments)
		}
	}
}

// resolveClaims gives a field the claims its counts support: its shape, the database its block's keys name and its
// value. A shape or a value that a source states is kept, except for three kinds of stated value. A reference with no
// database, as the docs' "key" is, takes the reference vanilla shows. A block takes it too, as a culture's traditions
// list their values, and so does a text, as a title's capital names one. Neither a block nor a text takes a reference
// to the game concepts, whose names are common words. The value is a reference if one database defines every value
// written and none is a number, a date or a yes-or-no word. Otherwise a field always written as a block is a block,
// or a color if every block was one. A field written only as scalars gets the first type in valueTests that all its
// values fit.
func (counter *counter) resolveClaims(field *Field) {
	vanilla := field.Vanilla
	if shape := resolveShapeClaim(vanilla); shape != "" {
		field.Shape.resolve(shape, game.SourceVanilla)
	}
	counter.resolveKeys(field)
	stated := field.Value.Value
	open := stated.Type == ValueReference && stated.Database == "" || stated.Type == ValueBlock || stated.Type == ValueText
	if field.Value.IsStated() && !open {
		return
	}
	literal := slices.ContainsFunc(slices.Collect(maps.Keys(vanilla.Values)), counter.isLiteral)
	expected, ok := counter.resolveReference(vanilla.Values)
	concept := expected.Database == counter.schema.Concepts
	if ok && !literal && (!field.Value.IsStated() || stated.Type == ValueReference || !concept) {
		field.Value = Claim[Expected]{Value: expected, Source: game.SourceVanilla}
		return
	}
	switch {
	case vanilla.Count == 0:
	case vanilla.Blocks == vanilla.Count && vanilla.Colors == vanilla.Count:
		field.Value.resolve(Expected{Type: ValueColor}, game.SourceVanilla)
	case vanilla.Blocks == vanilla.Count:
		field.Value.resolve(Expected{Type: ValueBlock}, game.SourceVanilla)
	case vanilla.Blocks == 0:
		for _, valueType := range valueTests {
			if counter.matchAll(valueType, vanilla.Values) {
				field.Value.resolve(Expected{Type: valueType}, game.SourceVanilla)
				return
			}
		}
	}
}

// resolveShapeClaim returns the shape that the blocks of a field were written in. If they call scripted definitions,
// it is the one shape every call allows. Otherwise it is the one shape the keys told. Where a key told value, value
// wins over triggers. It is unknown if the shapes disagree or a key told shapeAmbiguous, and "" if the blocks tell
// nothing.
func resolveShapeClaim(vanilla Vanilla) string {
	_, valued := vanilla.Shapes[game.ShapeValue]
	var shapes []string
	switch {
	case len(vanilla.Proofs) > 0:
		for proofs := range vanilla.Proofs {
			allowed := strings.Fields(proofs)
			if shapes == nil {
				shapes = allowed
			}
			shapes = slices.DeleteFunc(shapes, func(shape string) bool { return !slices.Contains(allowed, shape) })
		}
		if valued {
			shapes = slices.DeleteFunc(shapes, func(shape string) bool { return shape != game.ShapeValue })
		} else if len(shapes) > 1 {
			shapes = slices.DeleteFunc(shapes, func(shape string) bool { return shape == game.ShapeValue })
		}
	case len(vanilla.Shapes) > 0:
		shapes = slices.Sorted(maps.Keys(vanilla.Shapes))
		if valued {
			shapes = slices.DeleteFunc(shapes, func(shape string) bool { return shape == game.ShapeTriggers })
		}
		if slices.Contains(shapes, shapeAmbiguous) {
			return game.ShapeUnknown
		}
	default:
		return ""
	}
	if len(shapes) != 1 {
		return game.ShapeUnknown
	}
	return shapes[0]
}

// resolveKeys gives a field the database its block's keys name, where every key that only vanilla writes there is a
// definition of one database, as a culture's opinions name cultures. The fields of those keys are then folded into
// the field of any key. A key a source declares stays as it is. A block whose shape holds commands names nothing by
// its keys. Neither do keys that are game concepts or messages, the definitions of a GUI database (whose formats and
// blocks have common words for names, such as value), or the definitions of a database and those nested in it.
func (counter *counter) resolveKeys(field *Field) {
	if len(game.Shapes[field.Shape.Value].Allows) > 0 {
		return
	}
	keys := map[string]int{}
	for key, child := range field.Children {
		if child.Source == game.SourceVanilla && key != game.AnyKey && key != game.NumberKey {
			keys[key] = child.Vanilla.Count
		}
	}
	expected, ok := counter.resolveReference(keys)
	if !ok || expected.Nested || counter.isText(expected) || strings.HasPrefix(expected.Database, game.GUI.DatabasePrefix) {
		return
	}
	merged := field.buildChild(game.AnyKey)
	if merged.Source == game.SourceNone {
		merged.Source = game.SourceVanilla
	}
	for key := range keys {
		merged.add(field.Children[key])
		delete(field.Children, key)
	}
	field.Keys.resolve(expected.Database, game.SourceVanilla)
}

// resolveReference returns the reference that every given value makes: to the one database that defines them all. A
// value that holds a parameter is left out. A database named for another that defines them too does not count apart
// from it. Where no database defines them all, a database and those nested in it may, as a language and its dialects
// do, and the reference names the nested ones too. A game concept or a message key is text, not a definition: the
// concepts database counts only if nothing else defines every value, and the localization only if the concepts do not
// either. It returns false if there are no values, if nothing defines them all or if more than one does.
func (counter *counter) resolveReference(values map[string]int) (Expected, bool) {
	named := maps.Clone(values)
	maps.DeleteFunc(named, func(value string, _ int) bool { return jomini.IsParameter(value) })
	if len(named) == 0 {
		return Expected{}, false
	}
	var matched []string
	for database, names := range counter.defined {
		if hasAll(named, names) {
			matched = append(matched, database)
		}
	}
	databases := slices.DeleteFunc(slices.Clone(matched), func(database string) bool {
		return database == counter.schema.Concepts || counter.isNamedFor(database, matched)
	})
	nested := false
	if len(databases) == 0 {
		databases = counter.listFamilies(named)
		nested = len(databases) > 0
	}
	if len(databases) == 0 && slices.Contains(matched, counter.schema.Concepts) {
		databases = append(databases, counter.schema.Concepts)
	}
	if len(databases) == 0 && hasAll(named, counter.messages) {
		databases = append(databases, game.Localization)
	}
	if len(databases) != 1 {
		return Expected{}, false
	}
	return Expected{Type: ValueReference, Database: databases[0], Nested: nested}, true
}

// hasAll reports whether every given value is held by at least one of the given sets.
func hasAll(values map[string]int, sets ...map[string]bool) bool {
	for value := range values {
		if !slices.ContainsFunc(sets, func(set map[string]bool) bool { return set[value] }) {
			return false
		}
	}
	return true
}

// isNamedFor reports whether the definitions of a database are named for those of one of the given others. That is so
// if its row names one of them as its target, or, for coats of arms, if a database that holds coats is among them.
func (counter *counter) isNamedFor(database string, others []string) bool {
	if row := counter.schema.Databases[database]; row != nil && row.Row.Target != "" && slices.Contains(others, row.Row.Target) {
		return true
	}
	return database == game.Coat.Database &&
		slices.ContainsFunc(game.Coat.Holders, func(holder string) bool { return slices.Contains(others, holder) })
}

// listFamilies returns the databases that define every given value together with the databases nested in them, as a
// language and its dialects do.
func (counter *counter) listFamilies(values map[string]int) []string {
	var families []string
	for label, database := range counter.schema.Databases {
		members := []map[string]bool{counter.defined[label]}
		for _, nested := range database.Nested {
			members = append(members, counter.defined[nested])
		}
		if len(database.Nested) > 0 && hasAll(values, members...) {
			families = append(families, label)
		}
	}
	return families
}

// isText reports whether a reference is to text rather than to definitions: to the game concepts or the messages.
func (counter *counter) isText(expected Expected) bool {
	return expected.Database == counter.schema.Concepts || expected.Database == game.Localization
}

// isLiteral reports whether a value is one of the language's own: a number, a date or a yes-or-no word.
func (counter *counter) isLiteral(value string) bool {
	return slices.ContainsFunc([]ValueType{ValueBool, ValueNumber, ValueDate}, func(valueType ValueType) bool {
		return counter.schema.MatchValue(Expected{Type: valueType}, value)
	})
}

// matchAll reports whether every given value fits the type. It is false if there are no values.
func (counter *counter) matchAll(valueType ValueType, values map[string]int) bool {
	for value := range values {
		if !counter.schema.MatchValue(Expected{Type: valueType}, value) {
			return false
		}
	}
	return len(values) > 0
}

// resolvePrefix sets the database that defines every name written after the prefix. It sets none if no database does,
// or more than one does. The link's arguments record the names counted.
func (counter *counter) resolvePrefix(prefix string, labels map[string]int) {
	expected, ok := counter.resolveReference(labels)
	counter.schema.Prefixes[prefix] = expected.Database
	command := counter.schema.Commands[prefix]
	if command == nil {
		return
	}
	arguments := command.makeArguments(game.SourceVanilla)
	arguments.Vanilla = Vanilla{Values: labels}
	for _, count := range labels {
		arguments.Vanilla.Count += count
	}
	if ok {
		arguments.Value.resolve(expected, game.SourceVanilla)
	}
}
