// vanilla.go counts what the game's own files write and adds it to the schema.

package schema

import (
	"cmp"
	"errors"
	"iter"
	"maps"
	"path"
	"slices"
	"strings"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/loc"
)

// A counter is the visitor of one worker's counting walk. It counts into fields of its own, apart from the schema.
type counter struct {
	schema      *Schema
	holders     []holder
	path        string                     // the file being counted
	colorSuffix string                     // the ending of the keys that hold an untagged color in the file
	fields      map[*Field]*Field          // by the schema's field: what this worker counted at it
	arguments   map[argumentsKey]*Field    // by command and declaration: what this worker counted in its arguments
	definitions map[string]*Field          // by database
	defined     map[string]map[string]bool // the names each database defines, by database
	messages    map[string]bool            // every message key the localization files write, in any language
	unlisted    map[string]bool            // the steps of data calls no dump declares that the text writes, as Type.step
	gui         bool                       // the file is a GUI file
	prefixed    map[string]map[string]int  // by link prefix: the count of each name written after it
	examples    map[string][]Example       // by the name written: a command's, or a data function's as Type.Function
	commands    map[string]int             // by a command's name: how often the files write it
}

// An argumentsKey is one command under one declaration, whose arguments a worker counts apart.
type argumentsKey struct {
	command     *Command
	declaration string
}

// A holder is where the keys of one statement are counted while the counter is inside the statement.
type holder struct {
	field  *Field          // nil if the keys are no fields
	shaped bool            // the keys of the statement tell the shape of its field's blocks
	keys   map[string]bool // the keys a plain definition has written; nil if the statement is no plain definition
}

// valueTests are the types a count can give a field, in the order they are tried. A field gets the first type that
// every value written at it fits.
var valueTests = []ValueType{ValueBool, ValueNumber, ValueDate, ValueCall, ValueTarget, ValueText}

// ExampleCount is how many places of a name are kept as its examples.
const ExampleCount = 5

// shapeAmbiguous is the shape of a key that fits more than one shape. It is no real shape.
const shapeAmbiguous = "ambiguous"

// CountVanilla counts what the game's own files write and adds it to the schema: fields, their claims, the prefixes'
// databases and the colors. It parses with the given parser in parallel, so the parser must be safe to call at once.
// It returns an error only if no file of the listing can be parsed.
func (schema *Schema) CountVanilla(listing []files.File, parse func(path string) (*jomini.Tree, error)) error {
	localizing := slices.DeleteFunc(slices.Clone(listing), func(file files.File) bool {
		return file.Format != files.FormatLocalization
	})
	listing = slices.DeleteFunc(slices.Clone(listing), func(file files.File) bool { return !file.Format.IsScript() })
	// the files that declare callables are read first, so a key under a call counts as a call
	declaring := slices.DeleteFunc(slices.Clone(listing), func(file files.File) bool { return !schema.declares(file) })
	schema.declareFiles(declaring, parse)
	schema.resolveRelays(schema.ScriptedDefinitions, nil)
	counters := make([]*counter, files.Workers)
	for worker := range counters {
		counters[worker] = &counter{
			schema: schema, fields: map[*Field]*Field{}, arguments: map[argumentsKey]*Field{},
			definitions: map[string]*Field{}, defined: map[string]map[string]bool{}, messages: map[string]bool{},
			unlisted: map[string]bool{}, prefixed: map[string]map[string]int{}, examples: map[string][]Example{},
			commands: map[string]int{},
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
			// the copy keeps the key from holding the file's text in memory
			if !counters[worker].messages[message.Key] {
				counters[worker].messages[strings.Clone(message.Key)] = true
			}
			counters[worker].countUnlisted(message.Text, "")
		}
	})
	failures := eachParsed(listing, parse, func(worker, index int, tree *jomini.Tree) {
		counter, file := counters[worker], listing[index]
		counter.holders = counter.holders[:0]
		counter.path, counter.gui = file.Path, file.Format == files.FormatGUI
		counter.colorSuffix = game.FileTypes[strings.ToLower(path.Ext(file.Rel))].ColorSuffix
		schema.Walk(tree, file, counter)
	})
	for _, other := range counters[1:] {
		counters[0].add(other)
	}
	counters[0].resolveCounts()

	// read again, so a parameter's typings see the fields the count made and the calls the files declare
	schema.declareFiles(declaring, parse)
	schema.typeParameters(schema.ScriptedDefinitions)
	schema.resolveRelays(schema.ScriptedDefinitions, nil)
	if len(failures) == len(listing) && len(failures) > 0 {
		return errors.Join(failures...)
	}
	return nil
}

// Enter counts a statement: the name it defines, the names it writes after prefixes, its command, its value and its
// field.
func (counter *counter) Enter(tree *jomini.Tree, statement int32, context *Context) bool {
	var parent holder
	if last := len(counter.holders) - 1; last >= 0 {
		parent = counter.holders[last]
	}
	counter.countDefinition(context)
	current := tree.Statements[statement]
	for _, text := range []string{tree.Unquoted(current.Key), tree.Unquoted(current.Value)} {
		for _, link := range jomini.SplitChain(text) {
			prefix, label, ok := jomini.SplitPrefixed(link)
			if ok && counter.schema.isVoted(prefix) {
				counter.prefixed[prefix] = addCounts(counter.prefixed[prefix], map[string]int{label: 1})
			}
		}
	}
	counter.countCommand(tree, statement, context)
	counter.countCalls(tree, statement, context)
	if value := tree.Unquoted(tree.Statements[statement].Value); counter.gui && strings.Contains(value, "[") {
		counter.countUnlisted(value, context.Datacontext)
	}
	if parent.shaped {
		counter.countShape(parent.field, tree, statement, context)
	}
	var next holder
	block := current.IsBlock()
	field := counter.countField(parent, tree, statement, context)
	if valueField := counter.findValueField(parent, field, current, context); valueField != nil {
		valueField.Vanilla.Values = addCounts(valueField.Vanilla.Values, map[string]int{tree.Text(current.Value): 1})
	}
	switch {
	// a color block's numbers are its parts, not values, so a field written as a named color or a color block can name
	// the named colors
	case field != nil && (!block || !counter.isColor(tree, statement, tree.Text(current.Key))):
		next.field = field
	case context.Defines && context.Category == CategoryDefinition && context.Definition.Mode == "":
		next = holder{field: mirror(counter.definitions, context.Database), keys: map[string]bool{}}
		next.field.Vanilla.Definitions++
	case context.Defines && context.Category == CategoryDefinition, counter.isFrame(context):
		next.field = mirror(counter.definitions, context.Database)
	case context.Category == CategoryCommand && block:
		// a command's block holds its arguments and has no shape of its own
		next.field = mirror(counter.arguments, toArgumentsKey(context))
		block = false
	case context.Category == CategoryKeyword && block && context.Field != nil:
		// a keyword's block, such as a random_list branch, holds fields of the shape its holder gives
		next.field = mirror(counter.fields, context.Field)
		block = false
	}
	next.shaped = next.field != nil && block
	counter.holders = append(counter.holders, next)
	return true
}

// countDefinition records the name a statement defines, if any, and the second name that an alias key gives it, as a
// trait's group does.
func (counter *counter) countDefinition(context *Context) {
	if !context.Defines || context.Category != CategoryDefinition && !context.Definition.Alias {
		return
	}
	mirrorSet(counter.defined, context.Definition.Database)[context.Definition.Name] = true
}

// isVoted reports whether the names written after a link prefix count toward its database: only if it has none yet and
// its link leads to no literal, as flag:the_boss's does.
func (schema *Schema) isVoted(prefix string) bool {
	database, exists := schema.Prefixes[prefix]
	command := schema.Commands[prefix]
	return exists && database == "" && (command == nil || command.Target != game.DumpPhrases.Literal)
}

// addCounts adds each count of the second map to the first and returns the first, making it if it is nil.
func addCounts(total, added map[string]int) map[string]int {
	if len(added) > 0 && total == nil {
		total = map[string]int{}
	}
	for key, count := range added {
		total[key] += count
	}
	return total
}

// countCommand counts a statement whose key is a command or a link the dumps declare, and keeps its place. A chain of
// links counts as its first link, a prefixed link as its prefix, each in lower case, and a command's name with an
// argument in parentheses as the name.
func (counter *counter) countCommand(tree *jomini.Tree, statement int32, context *Context) {
	if context.Command == nil || context.Category != CategoryCommand && context.Category != CategoryLink {
		return
	}
	key := tree.Statements[statement].Key
	name, _ := splitLinkHead(counter.schema.findCallee(tree, statement))
	example := Example{Path: counter.path, Line: tree.Lines.ToPosition(key.Start).Line}
	counter.examples[name] = keepExample(counter.examples[name], example)
	counter.commands[name]++
}

// keepExample adds a place to a list of places. It returns the first places by path and then line, up to ExampleCount.
// A place already in the list is not added twice.
func keepExample(examples []Example, example Example) []Example {
	// sorted by path, so the result is the same whichever worker read which file
	position, exists := slices.BinarySearchFunc(examples, example, func(kept, added Example) int {
		return cmp.Or(strings.Compare(kept.Path, added.Path), cmp.Compare(kept.Line, added.Line))
	})
	if exists || position == ExampleCount {
		return examples
	}
	examples = slices.Insert(examples, position, example)
	return examples[:min(len(examples), ExampleCount)]
}

// countCalls keeps the place of each data function that the data call of a statement names, under the type that
// declares it.
func (counter *counter) countCalls(tree *jomini.Tree, statement int32, context *Context) {
	call := context.Call
	if call == nil {
		return
	}
	value := tree.Statements[statement].Value
	example := Example{Path: counter.path, Line: tree.Lines.ToPosition(value.Start).Line}
	owners, _ := counter.schema.listCallOwners(*call, context.Datacontext)
	for i, owner := range owners {
		if owner != "" {
			name := owner + "." + call.Steps[i].Text
			counter.examples[name] = keepExample(counter.examples[name], example)
		}
	}
}

// countUnlisted records the step of each data call in a text that no dump declares, read in the given data context.
func (counter *counter) countUnlisted(text, datacontext string) {
	for _, call := range loc.ListCalls(loc.Segments(text, 0)) {
		if step, before, ok := counter.schema.FindUnlisted(call, datacontext); ok {
			counter.unlisted[before+"."+call.Steps[step].Text] = true
		}
	}
}

// countShape counts the shape that a key in a block of a field tells. A call to a scripted definition counts the
// shapes it allows instead. A key that tells nothing counts nothing.
func (counter *counter) countShape(field *Field, tree *jomini.Tree, statement int32, context *Context) {
	if tree.Statements[statement].Key.IsEmpty() {
		return
	}
	key := counter.schema.findCallee(tree, statement)
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
		if !ok {
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

// resolveKeyShape returns the shape of the block that holds the given key. A keyword gives its holder, and a command
// declared as one of triggers or effects gives that. A key nothing else explains gives modifiers or fields. A command
// that fits more than one shape gives shapeAmbiguous. A link, a list iterator, a parameter and a call give "".
func (counter *counter) resolveKeyShape(key string, context *Context) string {
	lower := strings.ToLower(key)
	row := game.LookupGame(counter.schema.Game)
	for _, keyword := range game.Keywords {
		if keyword.Holder != "" && keyword.Key == lower && row.Matches(keyword.Games) {
			if keyword.Class == "" {
				return ""
			}
			return keyword.Holder
		}
	}
	switch {
	case context.Category == CategoryKeyword, context.Category == CategoryParameter:
		return ""
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

// resolveFieldShape returns the shape of the block that holds a key nothing else explains: modifiers, fields, or
// shapeAmbiguous for a modifier that is a command too.
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
	if jomini.IsNumberKey(key) {
		key = jomini.NumberKey
	}
	field := parent.field.buildChild(key, game.SourceNone)
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

// isOwnDefinition reports whether a statement defines a name of its own, which an alias does not.
func isOwnDefinition(context *Context) bool {
	return context.Defines && !context.Definition.Alias
}

// isColor reports whether a block is a color. A block with a color tag is one. A bare block of three or four numbers is
// one only under a key that ends as the file type says.
func (counter *counter) isColor(tree *jomini.Tree, statement int32, key string) bool {
	_, ok := jomini.ToColor(tree, statement)
	return ok && (!tree.Statements[statement].Tag.IsEmpty() ||
		counter.colorSuffix != "" && strings.HasSuffix(key, counter.colorSuffix))
}

// findValueField returns the field that counts the value a statement writes. That is the field its key names, the
// arguments of the command its key names, or the field of the block that holds a value written alone. It returns nil
// if the statement writes no value or the value is a definition.
func (counter *counter) findValueField(parent holder, field *Field, current jomini.Statement, context *Context) *Field {
	switch {
	case current.IsBlock() || current.Value.IsEmpty() || isOwnDefinition(context):
		return nil
	case field != nil:
		return field
	case context.Category == CategoryCommand && context.Command != nil:
		return mirror(counter.arguments, toArgumentsKey(context))
	case current.IsListed():
		return parent.field
	}
	return nil
}

// toArgumentsKey returns the key of the arguments that a statement's command takes.
func toArgumentsKey(context *Context) argumentsKey {
	return argumentsKey{command: context.Command, declaration: context.Command.resolveDeclaration(context.Declaration)}
}

// mirror returns the field that a worker counts into for the given key, making it if there is none.
func mirror[K comparable](fields map[K]*Field, key K) *Field {
	if fields[key] == nil {
		fields[key] = &Field{}
	}
	return fields[key]
}

// mirrorSet returns the set that a worker counts into for the given key, making it if there is none.
func mirrorSet(sets map[string]map[string]bool, key string) map[string]bool {
	if sets[key] == nil {
		sets[key] = map[string]bool{}
	}
	return sets[key]
}

// isFrame reports whether a statement is the block of a definition that has no name. Its keys are fields of its
// database's definition.
func (counter *counter) isFrame(context *Context) bool {
	database := counter.schema.Databases[context.Database]
	return database != nil && context.Field == database.Definition && context.Definition == nil
}

// Leave drops the statement's holder.
func (counter *counter) Leave(tree *jomini.Tree, statement int32, context *Context) {
	counter.holders = counter.holders[:len(counter.holders)-1]
}

// add adds the counts of another worker to this one's.
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
		maps.Copy(mirrorSet(counter.defined, database), labels)
	}
	maps.Copy(counter.messages, other.messages)
	maps.Copy(counter.unlisted, other.unlisted)
	for prefix, labels := range other.prefixed {
		counter.prefixed[prefix] = addCounts(counter.prefixed[prefix], labels)
	}
	for name, examples := range other.examples {
		for _, example := range examples {
			counter.examples[name] = keepExample(counter.examples[name], example)
		}
	}
	counter.commands = addCounts(counter.commands, other.commands)
}

// add adds the counts of another field and its children to this field's. A child that no source declared gets
// game.SourceVanilla.
func (field *Field) add(counted *Field) {
	field.Vanilla.add(counted.Vanilla)
	for key, child := range counted.Children {
		field.buildChild(key, game.SourceVanilla).add(child)
	}
}

// add adds the given counts to these.
func (vanilla *Vanilla) add(counted Vanilla) {
	vanilla.Count += counted.Count
	vanilla.Blocks += counted.Blocks
	vanilla.Colors += counted.Colors
	vanilla.Definitions += counted.Definitions
	vanilla.Values = addCounts(vanilla.Values, counted.Values)
	vanilla.Shapes = addCounts(vanilla.Shapes, counted.Shapes)
	vanilla.Proofs = addCounts(vanilla.Proofs, counted.Proofs)
}

// resolveCounts gives the schema what the count found: fields with their claims, the databases only the walk names,
// the prefixes, the examples and the command patterns.
func (counter *counter) resolveCounts() {
	row := game.LookupGame(counter.schema.Game)
	for field, counted := range counter.fields {
		field.add(counted)
	}
	for name, command := range counter.schema.Commands {
		command.Examples, command.Count = counter.examples[name], counter.commands[name]
	}
	for typeName, dataType := range counter.schema.DataTypes {
		for name, function := range dataType.Functions {
			function.Examples = counter.examples[typeName+"."+name]
			dataType.Functions[name] = function
		}
	}
	for key, counted := range counter.arguments {
		key.command.makeArguments(key.declaration, game.SourceVanilla).add(counted)
	}
	for _, database := range slices.Sorted(maps.Keys(counter.defined)) {
		counter.schema.buildDatabase(row, database, game.Database{})
	}
	for database, counted := range counter.definitions {
		counter.schema.buildDatabase(row, database, game.Database{})
		counter.schema.Databases[database].Definition.add(counted)
	}
	counter.resolvePatterns()
	counter.schema.walkFields(counter.resolveClaims)
	for prefix, labels := range counter.prefixed {
		counter.resolvePrefix(prefix, labels)
	}
	counter.schema.UnlistedCalls = counter.unlisted
}

// resolvePatterns finds the command patterns. A database makes one if two or more definitions, all it has, each fill
// the pattern's star in a command the dumps declare.
func (counter *counter) resolvePatterns() {
	owners := map[string][]string{}
	for database, labels := range counter.defined {
		for label := range labels {
			owners[label] = append(owners[label], database)
		}
	}
	type patternKey struct{ pattern, database string }
	members := map[patternKey][]string{} // the commands that fit the pattern with a name of the database in the star
	for _, command := range slices.Sorted(maps.Keys(counter.schema.Commands)) {
		for pattern, label := range splitPattern(command) {
			for _, database := range owners[label] {
				key := patternKey{pattern, database}
				members[key] = append(members[key], command)
			}
		}
	}
	for key, commands := range members {
		// one command for each definition: a name written twice, or in an entry mode, makes no pattern
		if len(commands) >= 2 && len(commands) == counter.schema.Databases[key.database].Definition.Vanilla.Definitions {
			counter.schema.CommandPatterns = append(counter.schema.CommandPatterns,
				CommandPattern{Key: key.pattern, Database: key.database, Commands: commands})
			for _, command := range commands {
				counter.schema.Commands[command].Patterned = true
			}
		}
	}
	slices.SortFunc(counter.schema.CommandPatterns, func(first, second CommandPattern) int {
		return cmp.Or(strings.Compare(first.Key, second.Key), strings.Compare(first.Database, second.Database))
	})
}

// splitPattern yields each way a command's name splits into a pattern, with a star in the place of a run of its
// underscore parts, and the label that run is. The whole name is never a label.
func splitPattern(command string) iter.Seq2[string, string] {
	return func(yield func(string, string) bool) {
		parts := strings.Split(command, "_")
		for first := range parts {
			for last := first + 1; last <= len(parts); last++ {
				pattern := slices.Concat(parts[:first], []string{"*"}, parts[last:])
				if len(pattern) > 1 && !yield(strings.Join(pattern, "_"), strings.Join(parts[first:last], "_")) {
					return
				}
			}
		}
	}
}

// walkFields calls the function on every field of the databases' definitions and the commands' arguments, with whether
// a command holds the field.
func (schema *Schema) walkFields(visit func(field *Field, held bool)) {
	each := func(root *Field, held bool) {
		visit(root, held)
		walkField(root, func(_ string, field *Field) { visit(field, held) })
	}
	for _, database := range schema.Databases {
		each(database.Definition, false)
	}
	for _, command := range schema.Commands {
		for _, arguments := range command.Arguments {
			each(arguments, true)
		}
	}
}

// resolveClaims gives a field the claims its counts support: its shape, the database its block's keys name and its
// value. The flag says whether a command holds the field. A stated claim is kept, but a reference with no database, a
// block or a text takes the reference vanilla shows.
func (counter *counter) resolveClaims(field *Field, held bool) {
	vanilla := field.Vanilla
	if shape := resolveShapeClaim(vanilla); shape != "" {
		field.Shape.resolve(shape, game.SourceVanilla)
	}
	// a command's own block, whose shape no source states, takes its arguments and the effects or triggers it runs
	if !held || field.Keys.IsStated() && field.Shape.IsStated() {
		counter.resolveKeys(field)
	}
	stated := field.Value.Value
	open := stated.Type == ValueReference && stated.Database == "" || stated.Type == ValueBlock || stated.Type == ValueText
	// a target that names a database is firm only where the database defines every name the game's own files write
	if stated.Type == ValueTarget && stated.Database != "" &&
		!hasAll(counter.listLabels(vanilla.Values), counter.defined[stated.Database]) {
		field.Value.Source = game.SourceVanilla
	}
	// a field written as numbers, script values and script value blocks takes a script value
	valued := (vanilla.Blocks == 0 || field.Shape.Value == game.ShapeValue) && counter.takesValues(vanilla.Values)
	// a stated number takes a script value too if the game's files write one, as EU5's add_cooldown does in
	// years = INTERNATIONAL_ORGANIZATION_SAME_AGENDA_COOLDOWN_YEARS
	if field.Value.IsStated() && stated.Type == ValueNumber && valued {
		field.Value.Value.Values = true
	}
	if field.Value.IsStated() && !open {
		return
	}
	expected, ok := counter.resolveReference(vanilla.Values)
	// a number names a definition where the database every label names also names a label that is no number, as
	// CK3's character ids are written beside character keys
	labels := slices.Collect(maps.Keys(counter.listLabels(vanilla.Values)))
	named := ok && slices.ContainsFunc(labels, func(label string) bool { return !jomini.IsNumber(label) })
	literal := slices.ContainsFunc(slices.Collect(maps.Keys(vanilla.Values)), func(value string) bool {
		isName := named && jomini.IsNumber(value) && counter.defined[expected.Database][value]
		return counter.isLiteral(value) && !isName
	})
	// a game concept's name is a common word, so a block or a text never refers to one
	concept := expected.Database == counter.schema.Concepts
	// a GUI database's names are common words, such as value, as they are for the keys (resolveKeys)
	gui := strings.HasPrefix(expected.Database, game.GUI.DatabasePrefix)
	if ok && !literal && !gui && (!field.Value.IsStated() || stated.Type == ValueReference || !concept) {
		field.Value = Claim[Expected]{Value: expected, Source: game.SourceVanilla}
		return
	}
	if valued {
		field.Value.resolve(Expected{Type: ValueNumber, Values: true}, game.SourceVanilla)
		return
	}
	switch {
	case vanilla.Count == 0: // a command's argument counts its values but not its writes
	case vanilla.Blocks == vanilla.Count && vanilla.Colors == vanilla.Count:
		field.Value.resolve(Expected{Type: ValueColor}, game.SourceVanilla)
	case vanilla.Blocks == vanilla.Count:
		field.Value.resolve(Expected{Type: ValueBlock}, game.SourceVanilla)
	case vanilla.Blocks == 0:
		for _, valueType := range valueTests {
			fits := len(vanilla.Values) > 0
			for value := range vanilla.Values {
				fits = fits && counter.schema.MatchValue(Expected{Type: valueType}, value)
			}
			if fits {
				field.Value.resolve(Expected{Type: valueType}, game.SourceVanilla)
				return
			}
		}
	}
}

// resolveShapeClaim returns the one shape the blocks of a field were written in, value winning over triggers. It
// returns game.ShapeUnknown if the shapes disagree, and "" if the blocks tell nothing.
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

// resolveKeys sets the database that a field's block's keys name, as a culture's opinions name cultures, and folds the
// keys only vanilla writes there into the <any> field. A stated database is kept; else the keys name one only if every
// key is a definition of it.
func (counter *counter) resolveKeys(field *Field) {
	if len(game.Shapes[field.Shape.Value].Allows) > 0 {
		return
	}
	keys := map[string]int{}
	for key, child := range field.Children {
		if child.Source == game.SourceVanilla && key != game.AnyKey && key != jomini.NumberKey {
			keys[key] = child.Vanilla.Count
		}
	}
	expected, ok := Expected{Database: field.Keys.Value}, len(keys) > 0
	if !field.Keys.IsStated() {
		expected, ok = counter.resolveReference(keys)
	}
	// keys never name the game concepts, localization, a GUI database, whose names are common words such as value, or
	// a database with nested ones
	if !ok || expected.Nested || expected.Database == counter.schema.Concepts || expected.Database == game.Localization ||
		strings.HasPrefix(expected.Database, game.GUI.DatabasePrefix) {
		return
	}
	merged := field.buildChild(game.AnyKey, game.SourceVanilla)
	for key := range keys {
		merged.add(field.Children[key])
		delete(field.Children, key)
	}
	field.Keys.resolve(expected.Database, game.SourceVanilla)
}

// resolveReference returns the reference that every given value makes, to the one database, or a database with its
// nested ones, that defines them all. The game concepts count only if nothing else does, and the localization only if
// neither does. It returns false if no database or several do.
func (counter *counter) resolveReference(values map[string]int) (Expected, bool) {
	labels := counter.listLabels(values)
	if len(labels) == 0 {
		return Expected{}, false
	}
	var matched []string
	for database, names := range counter.defined {
		if hasAll(labels, names) {
			matched = append(matched, database)
		}
	}
	databases := slices.DeleteFunc(slices.Clone(matched), func(database string) bool {
		return database == counter.schema.Concepts || counter.isNamedFor(database, matched)
	})
	nested := false
	if len(databases) == 0 {
		for label, database := range counter.schema.Databases {
			members := []map[string]bool{counter.defined[label]}
			for _, member := range database.Nested {
				members = append(members, counter.defined[member])
			}
			if len(database.Nested) > 0 && hasAll(labels, members...) {
				databases = append(databases, label)
			}
		}
		nested = len(databases) > 0
	}
	if len(databases) == 0 && slices.Contains(matched, counter.schema.Concepts) {
		databases = append(databases, counter.schema.Concepts)
	}
	if len(databases) == 0 && hasAll(labels, counter.messages) {
		databases = append(databases, game.Localization)
	}
	if len(databases) != 1 {
		return Expected{}, false
	}
	return Expected{Type: ValueReference, Database: databases[0], Nested: nested}, true
}

// listLabels returns the labels that the given values write, each with its count. A quoted value counts as the label
// inside its quotes. A value that holds a parameter is left out, and so is a link, which leads to a scope rather than
// naming a definition.
func (counter *counter) listLabels(values map[string]int) map[string]int {
	labels := map[string]int{}
	for value, count := range values {
		label := strings.Trim(value, `"`)
		if !jomini.IsParameter(label) && !counter.schema.IsLink(label) {
			labels[label] += count
		}
	}
	return labels
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

// isNamedFor reports whether the definitions of a database are named for those of another of the given databases,
// which all define the values: its row names one of them as its target, or it mirrors others and is not alone.
func (counter *counter) isNamedFor(label string, others []string) bool {
	database := counter.schema.Databases[label]
	return database != nil && (database.Row.Target != "" && slices.Contains(others, database.Row.Target) ||
		database.Row.Mirrors && len(others) > 1)
}

// isLiteral reports whether a value is one of the language's own: a number, a date or a yes-or-no word.
func (counter *counter) isLiteral(value string) bool {
	return slices.ContainsFunc([]ValueType{ValueBool, ValueNumber, ValueDate}, func(valueType ValueType) bool {
		return counter.schema.MatchValue(Expected{Type: valueType}, value)
	})
}

// takesValues reports whether a field takes a number that a script value may give: of the values the game's own files
// write in it, one names a script value, as add_stability = stability_mild_bonus does, and every other is a number, a
// parameter, a chain of links or a command.
func (counter *counter) takesValues(values map[string]int) bool {
	names := counter.defined[counter.schema.Values]
	named := false
	for value := range values {
		text := strings.Trim(value, `"`)
		_, declared := counter.schema.FindCommand(text)
		switch {
		case names[text]:
			named = true
		case jomini.IsNumber(text) || jomini.IsParameter(text) || counter.schema.IsLink(text) || declared: // allowed
		default:
			return false
		}
	}
	return named
}

// resolvePrefix sets the database that defines every name the game's own files write after a prefix, none if no
// database or several do, and records the names counted in its link's arguments. A prefix that leads to a scope from
// any scope tries that scope's database first.
func (counter *counter) resolvePrefix(prefix string, labels map[string]int) {
	database := counter.schema.findScopeDatabase(prefix)
	expected, ok := Expected{Type: ValueReference, Database: database}, database != ""
	if !ok || !hasAll(counter.listLabels(labels), counter.defined[database]) {
		expected, ok = counter.resolveReference(labels)
	}
	counter.schema.Prefixes[prefix] = expected.Database
	command := counter.schema.Commands[prefix]
	if command == nil {
		return
	}
	arguments := command.makeArguments(game.DeclaredLink, game.SourceVanilla)
	arguments.Vanilla = Vanilla{Values: labels}
	for _, count := range labels {
		arguments.Vanilla.Count += count
	}
	if ok {
		arguments.Value.resolve(expected, game.SourceVanilla)
	}
}

// findScopeDatabase returns the database whose definitions the names after a link prefix are, where the prefix leads to
// a scope from any scope: the database of the type the scope's own getter gives, else of the scope's phrase. It returns
// "" for any other prefix, and where neither gives one.
func (schema *Schema) findScopeDatabase(prefix string) string {
	command := schema.Commands[prefix]
	if command == nil || len(command.Scopes) > 0 || !schema.isScope(command.Target) {
		return ""
	}
	getters := schema.DataTypes[game.AnyScopeType].Functions
	for _, name := range slices.Sorted(maps.Keys(getters)) {
		database, ok := schema.ResolveTypeDatabase(getters[name].Returns)
		if ok && ToPhrase(DropGetter(name)) == ToPhrase(command.Target) {
			return database
		}
	}
	database, _ := schema.ResolveTypeDatabase(command.Target)
	return database
}
