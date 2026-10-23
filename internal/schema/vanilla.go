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
)

// parameterRe matches a parameter that a scripted definition's body writes, with its default if it has one.
var parameterRe = regexp.MustCompile(`\$([A-Za-z0-9_]+)(?:\|[^$]*)?\$`)

// valueTests are the types a count can give a field, in the order they are tried. A field takes the first that
// every value written at it fits.
var valueTests = []ValueType{ValueBool, ValueNumber, ValueDate, ValueCall, ValueText}

// exampleCount is how many places the count keeps for each command and data function.
const exampleCount = 5

// A declarer is the visitor of the walk over one file that declares what script and GUI files call by name. It
// reads the definitions from the top statements alone and keeps what the file declares.
type declarer struct {
	schema   *Schema
	scripted map[string]ScriptedDefinition // by the word that calls it and its name
	widgets  []string                      // the widget types the file defines or builds on
}

// A counter is the visitor of one worker's counting walk. It counts what the files write at the fields of the
// definitions, the names they define, and the names they write after prefixes. It counts into fields of its own,
// one for each field, command or database the keys are written under, so the walk never sees the count. It also
// keeps the first places each command and data function is written.
type counter struct {
	schema      *Schema
	holders     []holder
	path        string // the file being counted
	colorSuffix string // the ending of the keys that hold an untagged color in the file being counted
	fields      map[*Field]*Field
	arguments   map[*Command]*Field
	definitions map[string]*Field // by database
	defined     map[string]map[string]bool
	prefixed    map[string]map[string]int
	examples    map[string][]Example       // by the name written: a command's, or a data function's as Type.Function
	scopes      map[string]map[string]bool // by command: the scopes the game's own files run it in
}

// A holder is the field a statement's keys are counted into while the counter is inside the statement. It counts the
// kinds of keys the statement writes, which decide the shape of its block.
type holder struct {
	field *Field // nil where the keys are no fields
	votes map[string]int
}

// CountVanilla learns from the game's own files by three plain rules and no tuned number. A key written under a
// database's definitions becomes a field with game.SourceVanilla once it has been seen once. Its shape claim is
// the shape most of its writes have. Its value claim is a type only when every written value matches that type.
// The names written after each prefix and the colors are counted the same way. Before counting, the top-level
// statements of the files that declare callables and widget types are read without going into their blocks, so
// that a key under a call is counted as a call. The files are parsed and walked on every core but two, each with the
// given function, which must be safe to call at once. A vanilla claim helps completion and marks a hover card as
// inferred; it never fails a verdict.
func (schema *Schema) CountVanilla(paths []string, parse func(path string) (*jomini.Tree, error)) error {
	listing := schema.listVanilla(paths)
	declaring := slices.DeleteFunc(slices.Clone(listing), func(file files.File) bool { return !schema.declares(file) })
	declarers := make([]*declarer, len(declaring))
	eachTree(declaring, parse, func(_, index int, tree *jomini.Tree) {
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
			definitions: map[string]*Field{}, defined: map[string]map[string]bool{}, prefixed: map[string]map[string]int{},
			examples: map[string][]Example{}, scopes: map[string]map[string]bool{},
		}
	}
	failures := eachTree(listing, parse, func(worker, index int, tree *jomini.Tree) {
		counters[worker].resolveFile(listing[index])
		schema.Walk(tree, listing[index], counters[worker])
	})
	for _, other := range counters[1:] {
		counters[0].add(other)
	}
	counters[0].resolveCounts()
	state := SourceState{Origin: originNone}
	if len(listing) > 0 {
		state.Origin = originInstall
	}
	schema.Sources[game.SourceVanilla] = state
	// a file that cannot be read is left out of the count; only a count of nothing is an error
	if len(failures) == len(listing) && len(failures) > 0 {
		return errors.Join(failures...)
	}
	return nil
}

// eachTree parses each file on every core but two and calls the function with the worker's number, the file's
// position and its tree. A file that cannot be parsed is left out, and its error is among those it returns.
func eachTree(listing []files.File, parse func(path string) (*jomini.Tree, error), visit func(worker, index int, tree *jomini.Tree)) []error {
	failures := make([]error, len(listing))
	var next atomic.Int64
	var group sync.WaitGroup
	for worker := range files.Workers {
		group.Go(func() {
			for index := int(next.Add(1) - 1); index < len(listing); index = int(next.Add(1) - 1) {
				tree, err := parse(listing[index].Path)
				if err != nil {
					failures[index] = err
					continue
				}
				visit(worker, index, tree)
			}
		})
	}
	group.Wait()
	return slices.DeleteFunc(failures, func(err error) bool { return err == nil })
}

// listVanilla returns the files among the paths that script is read from: script, GUI, asset and font files under
// a game folder.
func (schema *Schema) listVanilla(paths []string) []files.File {
	var listing []files.File
	for _, file := range paths {
		rel, ok := trimScriptRoot(file)
		if !ok {
			continue
		}
		if format := files.Classify(rel); format.IsScript() {
			listing = append(listing, files.File{
				Path: file, Key: files.ToKey(file), Rel: rel, Origin: game.OriginVanilla, Format: format,
			})
		}
	}
	return listing
}

// declares reports whether a file declares definitions that script or GUI files call by name: a GUI file, or a
// file of a database that has callers.
func (schema *Schema) declares(file files.File) bool {
	if file.Format == files.FormatGUI {
		return true
	}
	database, ok := schema.FindDatabase(file.Rel)
	return ok && len(database.Callable.Callers) > 0
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

// Enter records the definition that a statement makes, if its database is called by name, and goes on into the top
// blocks that only group definitions.
func (declarer *declarer) Enter(tree *jomini.Tree, statement int32, context *Context) bool {
	if context.Defines && context.Category == CategoryDefinition {
		declarer.declare(*context.Definition, tree, statement)
	}
	return !context.Defines && tree.Statements[statement].Parent == 0
}

// Leave does nothing.
func (declarer *declarer) Leave(tree *jomini.Tree, statement int32, context *Context) {}

// declare records a definition that is called by name: for each word that calls its database, the scripted
// definition with the parameters its body writes, and the widget type it is, if a widget calls it. The type a
// widget type is built on is a widget type too, since it is either defined or built into the game.
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

// build gives the schema what one file declared. The files are built in order, so a later definition of a name
// replaces an earlier one.
func (declarer *declarer) build() {
	maps.Copy(declarer.schema.ScriptedDefinitions, declarer.scripted)
	for _, widget := range declarer.widgets {
		declarer.schema.WidgetTypes[widget] = true
	}
}

// makeScripted returns the scripted definition that a statement of a database called by name makes, with the
// parameters its body writes where the database's definitions take parameters.
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

// resolveFile starts the count of a file. The statements of a file that its database names as one definition are
// written under that definition's fields. The file type says which keys hold an untagged color.
func (counter *counter) resolveFile(file files.File) {
	counter.holders = counter.holders[:0]
	counter.path = file.Path
	counter.colorSuffix = game.FileTypes[strings.ToLower(path.Ext(file.Rel))].ColorSuffix
	if database, ok := counter.schema.FindDatabase(file.Rel); ok && database.Row.NamedBy == "file" {
		counter.holders = append(counter.holders, holder{field: mirror(counter.definitions, database.Name)})
	}
}

// Enter counts a statement: the name it defines, the names it writes after prefixes, the place of a command it
// writes, and, when it is written under a definition's fields, the field it is.
func (counter *counter) Enter(tree *jomini.Tree, statement int32, context *Context) bool {
	var parent holder
	if last := len(counter.holders) - 1; last >= 0 {
		parent = counter.holders[last]
	}
	counter.countDefinition(context)
	counter.countPrefixes(tree, statement)
	counter.countCommand(tree, statement, context)
	counter.countCalls(tree, statement, context)
	if parent.votes != nil {
		parent.votes[counter.findVote(tree, statement, context)]++
	}
	var next holder
	block := tree.Statements[statement].IsBlock()
	switch field := counter.countField(parent, tree, statement, context); {
	case field != nil:
		next.field = field
	case context.Defines && context.Category == CategoryDefinition, counter.isFrame(context):
		next.field = mirror(counter.definitions, context.Database)
	case context.Category == CategoryCommand && block:
		// a command's block holds what it takes, which is no shape of its own
		if command := counter.schema.Commands[tree.Unquoted(tree.Statements[statement].Key)]; command != nil {
			next.field = mirror(counter.arguments, command)
		}
		block = false
	case context.Category == CategoryKeyword && block && context.Field != nil:
		// a keyword's block, such as a random_list branch, holds the fields of the form its holder gives it
		next.field = mirror(counter.fields, context.Field)
		block = false
	}
	if next.field != nil && block {
		next.votes = map[string]int{}
	}
	counter.holders = append(counter.holders, next)
	return true
}

// Leave ends a statement. A block written as a field adds one write of the shape its keys have.
func (counter *counter) Leave(tree *jomini.Tree, statement int32, context *Context) {
	last := counter.holders[len(counter.holders)-1]
	counter.holders = counter.holders[:len(counter.holders)-1]
	if shape := resolveWriteShape(last.votes); last.field != nil && shape != "" {
		last.field.Vanilla.Shapes = addCounts(last.field.Vanilla.Shapes, map[string]int{shape: 1})
	}
}

// mirror returns the field a worker counts into for what the keys are written under, making it the first time.
func mirror[K comparable](fields map[K]*Field, key K) *Field {
	if fields[key] == nil {
		fields[key] = &Field{}
	}
	return fields[key]
}

// resolveWriteShape returns the shape one block has by the kinds its keys voted for. It is that kind if every key
// voted for the same; where they voted for several, it is the one whose shape holds the commands every other kind
// holds, as a value block holds triggers, else unknown, which holds the keys of any kind. It returns "" for a block
// whose keys voted for none.
func resolveWriteShape(votes map[string]int) string {
	var voted []string
	for vote, count := range votes {
		if vote != "" && count > 0 {
			voted = append(voted, vote)
		}
	}
	slices.Sort(voted)
	for _, shape := range voted {
		holds := game.Shapes[shape].Allows
		if !slices.ContainsFunc(voted, func(other string) bool {
			allows := game.Shapes[other].Allows
			return other != shape && (len(allows) == 0 || slices.ContainsFunc(allows, func(kind string) bool { return !slices.Contains(holds, kind) }))
		}) {
			return shape
		}
	}
	if len(voted) == 0 {
		return ""
	}
	return shapeUnknown
}

// findMost returns the key with the highest count, the first in order of name among equals, or "" for no counts.
func findMost(counts map[string]int) string {
	best := ""
	for key, count := range counts {
		if key != "" && (best == "" || count > counts[best] || count == counts[best] && key < best) {
			best = key
		}
	}
	return best
}

// isFrame reports whether a statement is written as the block of a definition that has no name: its keys are fields
// of its database's definition.
func (counter *counter) isFrame(context *Context) bool {
	database := counter.schema.Databases[context.Database]
	return database != nil && context.Field == database.Definition && context.Definition == nil
}

// countDefinition records that a statement defines a definition, if it does.
func (counter *counter) countDefinition(context *Context) {
	if !context.Defines || context.Category != CategoryDefinition {
		return
	}
	if counter.defined[context.Database] == nil {
		counter.defined[context.Database] = map[string]bool{}
	}
	counter.defined[context.Database][context.Definition.Name] = true
}

// countPrefixes records the names that a statement writes after a prefix with no database yet, in its key and in
// its value.
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

// countCommand keeps the place of a statement whose key is a command or a link the dumps declare, and the scope it
// runs in there when that is known. A chain of links counts as its first link.
func (counter *counter) countCommand(tree *jomini.Tree, statement int32, context *Context) {
	if context.Category != CategoryCommand && context.Category != CategoryLink {
		return
	}
	key := tree.Statements[statement].Key
	name := tree.Unquoted(key)
	if steps := jomini.SplitChain(name); context.Category == CategoryLink && len(steps) > 0 {
		name = steps[0]
	}
	command := counter.schema.Commands[name]
	if command == nil {
		return
	}
	example := Example{Path: counter.path, Line: tree.Lines.ToPosition(key.Start).Line}
	counter.examples[name] = keepExample(counter.examples[name], example)
	if scope := context.ResolveCommandScope(command); scope != "" {
		if counter.scopes[name] == nil {
			counter.scopes[name] = map[string]bool{}
		}
		counter.scopes[name][scope] = true
	}
}

// countCalls keeps the place of each data function that a statement's data call names, by the type that declares it.
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

// keepExample adds a place to a list of places and returns the first exampleCount of them by path and then line.
// A place already in the list is not added twice.
func keepExample(examples []Example, example Example) []Example {
	// The first by path rather than the first seen, so the places kept do not depend on which worker read which file.
	position, found := slices.BinarySearchFunc(examples, example, func(kept, added Example) int {
		return cmp.Or(strings.Compare(kept.Path, added.Path), cmp.Compare(kept.Line, added.Line))
	})
	if found || position == exampleCount {
		return examples
	}
	examples = slices.Insert(examples, position, example)
	return examples[:min(len(examples), exampleCount)]
}

// findVote returns what a key written in a block says the block holds: the kind of block that alone holds the word
// it is, triggers for a trigger, effects for an effect, and fields for a key counted as a field. A key that says
// nothing votes for "".
func (counter *counter) findVote(tree *jomini.Tree, statement int32, context *Context) string {
	if tree.Statements[statement].Key.IsEmpty() {
		return ""
	}
	key := tree.Unquoted(tree.Statements[statement].Key)
	for _, keyword := range game.Keywords {
		if keyword.Holder != "" && keyword.Key == strings.ToLower(key) {
			if keyword.Class == "" {
				return ""
			}
			return keyword.Holder
		}
	}
	command := counter.schema.Commands[key]
	switch {
	case command != nil && (context.Category == CategoryCommand || context.Category == CategoryKeyword):
		triggers := slices.Contains(command.Declared, game.DeclaredTrigger)
		effects := slices.Contains(command.Declared, game.DeclaredEffect)
		if triggers && !effects {
			return shapeTriggers
		}
		if effects && !triggers {
			return shapeEffects
		}
	case counter.isUnexplained(key, context):
		return "fields"
	}
	return ""
}

// countField counts a statement as a write of the field its key names under the holder's field, and returns that
// field. It returns nil for a statement that is no field: one written under no field, a definition, a command, a
// link, a call to a definition, and a value written alone.
func (counter *counter) countField(parent holder, tree *jomini.Tree, statement int32, context *Context) *Field {
	current := tree.Statements[statement]
	key := tree.Unquoted(current.Key)
	if parent.field == nil || current.Key.IsEmpty() || context.Defines || !counter.isUnexplained(key, context) {
		return nil
	}
	written := key
	if game.IsNumberKey(key) {
		key = game.NumberKey
	}
	field := parent.field.buildChild(key)
	field.Vanilla.Count++
	switch {
	case current.IsBlock():
		field.Vanilla.Blocks++
		if counter.isColor(tree, statement, written) {
			field.Vanilla.Colors++
		}
	case !current.Value.IsEmpty() && !counter.isInstanceList(context):
		field.Vanilla.Values = addCounts(field.Vanilla.Values, map[string]int{tree.Text(current.Value): 1})
	}
	return field
}

// isColor reports whether a block is a color. A block with a tag is one, and a bare block of three or four numbers
// is one only under a key that ends as the file type says.
func (counter *counter) isColor(tree *jomini.Tree, statement int32, key string) bool {
	tag, _, ok := jomini.ColorParts(tree, statement)
	return ok && (tag != "" || counter.colorSuffix != "" && strings.HasSuffix(key, counter.colorSuffix))
}

// isUnexplained reports whether nothing but a field explains a key: no rule fit it, or it fit a rule that says it
// is a field, or it is a call to a name that no definition has.
func (counter *counter) isUnexplained(key string, context *Context) bool {
	switch context.Category {
	case CategoryNone, CategoryField:
		return true
	case CategoryCall:
		_, iterator := game.MatchListIterator(key)
		return context.Field == nil && (!iterator || counter.schema.Lists == "")
	}
	return false
}

// isInstanceList reports whether a statement is written in a database that names no definitions. Such files list
// instances of things, thousands of values with no language in them, so their values are not kept.
func (counter *counter) isInstanceList(context *Context) bool {
	database := counter.schema.Databases[context.Database]
	return database != nil && database.Row.NamedBy == "nothing"
}

// add adds what another worker counted to this one's count.
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

// resolveCounts gives the schema's fields what the count found: the fields counted under each field, command and
// database, with the claims their counts support, and the prefixes resolved by the names defined. A database that
// only the walk names, such as an asset file's entity or a GUI file's texticon, is made here. Each command and data
// function gets the places kept for it, and each command the scopes the game's files run it in.
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
		counter.schema.buildDatabase(row, database, "")
		counter.schema.Databases[database].Count = len(counter.defined[database])
	}
	for database, counted := range counter.definitions {
		counter.schema.buildDatabase(row, database, "")
		counter.schema.Databases[database].Definition.add(counted)
	}
	counter.schema.walkFields(counter.resolveClaims)
	for prefix, labels := range counter.prefixed {
		counter.resolvePrefix(prefix, labels)
	}
}

// resolveClaims gives a field the shape most of its blocks were written in, and the value claim that every value
// written at it fits, unless a source has stated one. A field always written as a block is a block, or a color if
// every block was one. A field written as scalars is the first type in valueTests that all of them fit.
func (counter *counter) resolveClaims(field *Field) {
	vanilla := field.Vanilla
	if shape := findMost(vanilla.Shapes); shape != "" {
		field.Shape.resolve(shape, game.SourceVanilla)
	}
	if field.Value.IsStated() {
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

// matchAll reports whether every value fits a type. It is false for no values.
func (counter *counter) matchAll(valueType ValueType, values map[string]int) bool {
	for value := range values {
		if !counter.schema.MatchValue(Expected{Type: valueType}, value) {
			return false
		}
	}
	return len(values) > 0
}

// resolvePrefix names the database that the names written after a prefix are defined in: the one that defines more
// than half of them. Where several define as many, it is the smallest of them, and the first by name among those as
// small, as for a name written with no database. It names none where no database defines more than half. The link's
// arguments record the names counted.
func (counter *counter) resolvePrefix(prefix string, labels map[string]int) {
	best, most := "", 0
	for database, defined := range counter.defined {
		count := 0
		for label := range labels {
			if defined[label] {
				count++
			}
		}
		if count > most || count == most && count > 0 && counter.schema.IsSmaller(database, best) {
			best, most = database, count
		}
	}
	if most*2 <= len(labels) {
		best = ""
	}
	counter.schema.Prefixes[prefix] = best
	command := counter.schema.Commands[prefix]
	if command == nil {
		return
	}
	arguments := command.makeArguments(game.SourceVanilla)
	arguments.Vanilla = Vanilla{Values: labels}
	for _, count := range labels {
		arguments.Vanilla.Count += count
	}
	if best != "" {
		arguments.Value.resolve(Expected{Type: ValueReference, Database: best}, game.SourceVanilla)
	}
}

// add adds the writes another field's count found to a field's count, and its children's to its children's. A child
// no source declared takes vanilla as its source.
func (field *Field) add(counted *Field) {
	field.Vanilla.Count += counted.Vanilla.Count
	field.Vanilla.Blocks += counted.Vanilla.Blocks
	field.Vanilla.Colors += counted.Vanilla.Colors
	field.Vanilla.Values = addCounts(field.Vanilla.Values, counted.Vanilla.Values)
	field.Vanilla.Shapes = addCounts(field.Vanilla.Shapes, counted.Vanilla.Shapes)
	for key, child := range counted.Children {
		target := field.buildChild(key)
		if target.Source == game.SourceNone {
			target.Source = game.SourceVanilla
		}
		target.add(child)
	}
}

// addCounts adds each count of the second map to the first map's count of the same key and returns the first map,
// which it makes when it is empty and the second has any.
func addCounts(total, added map[string]int) map[string]int {
	if len(added) > 0 && total == nil {
		total = map[string]int{}
	}
	for key, count := range added {
		total[key] += count
	}
	return total
}
