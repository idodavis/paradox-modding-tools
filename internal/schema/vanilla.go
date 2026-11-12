// vanilla.go counts what the game's own files write and adds it to the
// schema.

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

// counter counts the files one worker walks. It counts into fields of its
// own, apart from the schema.
type counter struct {
	schema *Schema

	// contexts is the list the worker's walks fill, one file after another.
	contexts []Context

	// holders holds, for each statement of the file being counted, where the
	// keys of its block are counted.
	holders []holder

	// path is the file being counted, and colorSuffix the ending of the keys
	// that hold an untagged color in it.
	path        string
	colorSuffix string

	// fields holds, by the schema's field, what this worker counted at it.
	fields map[*Field]*Field

	// arguments holds, by command, what this worker counted in the command's
	// keys.
	arguments map[*Definition]*Field

	// definitions holds, by database, what this worker counted in its
	// definitions, and defined the names each database defines.
	definitions map[string]*Field
	defined     map[string]map[string]bool

	// messages holds every message key the localization files write, in any
	// language.
	messages map[string]bool

	// unlisted holds the steps of data calls that no dump declares and a
	// text writes, as Type.step.
	unlisted map[string]bool

	// gui is whether the file being counted is a GUI file.
	gui bool

	// prefixed holds, by link prefix, the count of each name written after
	// it.
	prefixed map[string]map[string]int

	// examples holds, by a data function's name as Type.Function, the first
	// places it is called.
	examples map[string][]Example
}

// holder is where the keys of one statement are counted while the counter is
// inside the statement.
type holder struct {
	// field is the field the keys count into. It is nil if the keys are no
	// fields.
	field *Field

	// shaped is whether the keys of the statement tell the shape of its
	// field's blocks.
	shaped bool

	// keys holds the keys a plain definition has written. It is nil if the
	// statement is no plain definition.
	keys map[string]bool
}

// shapeAmbiguous is the shape of a key that fits more than one shape. It is
// no real shape.
const shapeAmbiguous game.Shape = "ambiguous"

// CountVanilla counts what the game's own files write and adds it to the
// schema: fields, their claims, the prefixes' databases and the colors. It
// parses with the given parser in parallel, so the parser must be safe to
// call at once. It returns an error only if no file of the listing can be
// parsed.
func (s *Schema) CountVanilla(listing []files.File, parse func(path string) (*jomini.Tree, error)) error {
	localizing := slices.DeleteFunc(slices.Clone(listing), func(file files.File) bool {
		return file.Format != files.FormatLocalization
	})
	listing = slices.DeleteFunc(slices.Clone(listing), func(file files.File) bool { return !file.Format.IsScript() })

	// the files that declare callables are read first, so a key under a call
	// counts as a call
	declaring := slices.DeleteFunc(slices.Clone(listing), func(file files.File) bool { return !s.declares(file) })
	s.resolveRelays(s.declareFiles(declaring, parse), nil)

	counters := make([]*counter, files.Workers)
	for worker := range counters {
		counters[worker] = &counter{
			schema: s, fields: map[*Field]*Field{}, arguments: map[*Definition]*Field{},
			definitions: map[string]*Field{}, defined: map[string]map[string]bool{}, messages: map[string]bool{},
			unlisted: map[string]bool{}, prefixed: map[string]map[string]int{}, examples: map[string][]Example{},
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

			counters[worker].countUnlisted(message.Segments, "")
		}
	})

	failures := eachParsed(listing, parse, func(worker, index int, tree *jomini.Tree) {
		counter, file := counters[worker], listing[index]
		counter.path, counter.gui = file.Path, file.Format == files.FormatGUI
		counter.colorSuffix = game.FileTypes[strings.ToLower(path.Ext(file.Rel))].ColorSuffix
		counter.count(tree, file)
	})
	for _, other := range counters[1:] {
		counters[0].add(other)
	}

	counters[0].resolveCounts()

	// read again, so a parameter's typings see the fields the count made and
	// the calls the files declare
	winners := s.declareFiles(declaring, parse)
	s.typeParameters(winners)
	s.resolveRelays(winners, nil)
	if len(failures) == len(listing) && len(failures) > 0 {
		return errors.Join(failures...)
	}

	return nil
}

// count walks a file and counts each of its statements, in the tree's order.
// The file's own context counts only where the file is one definition.
func (c *counter) count(tree *jomini.Tree, file files.File) {
	c.contexts = c.schema.Walk(tree, file, c.contexts)
	c.holders = slices.Grow(c.holders[:0], len(c.contexts))[:len(c.contexts)]
	c.holders[0] = holder{}

	for position := range c.contexts {
		statement, context := int32(position), &c.contexts[position]
		if statement > 0 || context.Definition != nil {
			c.countStatement(tree, statement, context)
		}
	}
}

// countStatement counts a statement: the name it defines, the names it
// writes after prefixes, its command, its value and its field.
func (c *counter) countStatement(tree *jomini.Tree, statement int32, context *Context) {
	// the file's own statement is held by nothing
	var parent holder
	if statement > 0 {
		parent = c.holders[tree.Statements[statement].Parent]
	}

	c.countDefinition(context)
	current := tree.Statements[statement]
	for _, span := range []jomini.Span{tree.UnquotedSpan(current.Key), tree.UnquotedSpan(current.Value)} {
		for _, link := range jomini.Links(tree.Text(span), span.Start) {
			prefix := tree.Text(link.Prefix)
			if !link.Prefix.IsEmpty() && c.schema.isVoted(prefix) {
				c.prefixed[prefix] = addCounts(c.prefixed[prefix], map[string]int{tree.Text(link.Name): 1})
			}
		}
	}

	c.countCalls(tree, statement, context)
	if value := tree.Unquoted(tree.Statements[statement].Value); c.gui && strings.Contains(value, "[") {
		c.countUnlisted(loc.Segments(value, 0), context.Datacontext)
	}

	if parent.shaped {
		c.countShape(parent.field, tree, statement, context)
	}

	var next holder
	block := current.IsBlock()
	field := c.countField(parent, tree, statement, context)
	if valueField := c.findValueField(parent, field, current, context); valueField != nil {
		valueField.Vanilla.Values = addCounts(valueField.Vanilla.Values, map[string]int{tree.Text(current.Value): 1})
	}

	switch {
	// a color block's numbers are its parts, not values, so a field written
	// as a named color or a color block can name the named colors
	case field != nil && (!block || !c.isColor(tree, statement, tree.Text(current.Key))):
		next.field = field

	case context.Keyword != nil && context.Keyword.Shape == game.ShapeEnclosing:
		// an enclosing keyword's block, such as an if's, is the block around
		// it again, so its keys count where that block's do
		c.holders[statement] = parent

		return

	case context.Defines && context.Category == CategoryDefinition && context.Definition.Mode == "":
		next = holder{field: mirror(c.definitions, context.Database), keys: map[string]bool{}}
		next.field.Vanilla.definitions++

	case context.Defines && context.Category == CategoryDefinition, c.isFrame(context):
		next.field = mirror(c.definitions, context.Database)

	case context.Category == CategoryCommand && block:
		// a command's block holds its arguments and has no shape of its own
		next.field = mirror(c.arguments, context.Command)
		block = false

	case context.Category == CategoryKeyword && block && context.Field != nil:
		// a keyword's block, such as a random_list branch, holds fields of
		// the shape its holder gives
		next.field = mirror(c.fields, context.Field)
		block = false
	}

	next.shaped = next.field != nil && block
	c.holders[statement] = next
}

// countDefinition records the name a statement defines, if any, and the
// second name that an alias key gives it, as a trait's group does.
func (c *counter) countDefinition(context *Context) {
	if !context.Defines || context.Category != CategoryDefinition && !context.Definition.Alias {
		return
	}

	mirrorSet(c.defined, context.Definition.Database)[context.Definition.Name] = true
}

// isVoted reports whether the names written after a link prefix count toward
// its database: only if it has none yet and its link leads to no literal, as
// flag:the_boss's does.
func (s *Schema) isVoted(prefix string) bool {
	database, exists := s.LookupPrefix(prefix)

	return exists && database == "" && s.lookupLink(prefix, true).Target != dumpPhrases.Literal
}

// addCounts adds each count of the second map to the first and returns the
// first, making it if it is nil.
func addCounts[K comparable](total, added map[K]int) map[K]int {
	if len(added) > 0 && total == nil {
		total = map[K]int{}
	}

	for key, count := range added {
		total[key] += count
	}

	return total
}

// ExampleCount is how many places of a name are kept as its examples.
const ExampleCount = 5

// keepExample adds a place to a list of places. It returns the first places
// by path and then line, up to ExampleCount. A place already in the list is
// not added twice.
func keepExample(examples []Example, example Example) []Example {
	// sorted by path, so the result is the same whichever worker read which
	// file
	position, exists := slices.BinarySearchFunc(examples, example, func(kept, added Example) int {
		return cmp.Or(strings.Compare(kept.Path, added.Path), cmp.Compare(kept.Line, added.Line))
	})
	if exists || position == ExampleCount {
		return examples
	}

	examples = slices.Insert(examples, position, example)

	return examples[:min(len(examples), ExampleCount)]
}

// countCalls keeps the place of each data function that the data call of a
// statement names, under the type that declares it.
func (c *counter) countCalls(tree *jomini.Tree, statement int32, context *Context) {
	call := context.Call
	if call == nil {
		return
	}

	value := tree.Statements[statement].Value
	example := Example{Path: c.path, Line: tree.Lines.ToPosition(value.Start).Line}
	owners, _ := c.schema.listCallOwners(*call, context.Datacontext)
	for i, owner := range owners {
		if owner != "" {
			name := owner + "." + call.Steps[i].Text
			c.examples[name] = keepExample(c.examples[name], example)
		}
	}
}

// countUnlisted records the step of each data call in the segments of a text
// that no dump declares, read in the given data context.
func (c *counter) countUnlisted(segments []loc.Segment, datacontext string) {
	for _, segment := range segments {
		if segment.Call == nil {
			continue
		}

		if step, before, ok := c.schema.FindUnlisted(*segment.Call, datacontext); ok {
			c.unlisted[before+"."+segment.Call.Steps[step].Text] = true
		}
	}
}

// countShape counts the shape that a key in a block of a field tells. A call
// to a scripted definition counts the shapes it allows instead. A key that
// tells nothing counts nothing.
func (c *counter) countShape(field *Field, tree *jomini.Tree, statement int32, context *Context) {
	if tree.Statements[statement].Key.IsEmpty() {
		return
	}

	key := c.schema.findCallee(tree, statement)
	if context.Category == CategoryCall {
		if proofs, defined := c.resolveProofs(key); defined {
			if proofs != "" {
				field.Vanilla.proofs = addCounts(field.Vanilla.proofs, map[string]int{proofs: 1})
			}

			return
		}
	}

	if shape := c.resolveKeyShape(key, context); shape != "" {
		field.Vanilla.shapes = addCounts(field.Vanilla.shapes, map[game.Shape]int{shape: 1})
	}
}

// resolveProofs returns the shapes that a call to a scripted definition
// allows: those that every database defining the name allows, sorted and
// joined by spaces. It is "" if the databases share none. It returns false
// if no database defines the name.
func (c *counter) resolveProofs(key string) (string, bool) {
	var shapes []game.Shape
	defined := false
	for _, database := range game.Called {
		if c.schema.lookupScripted(nil, database, key) == nil {
			continue
		}

		proves := c.schema.Databases[database].Declaration.Proves()
		if defined {
			shapes = slices.DeleteFunc(shapes, func(shape game.Shape) bool { return !slices.Contains(proves, shape) })
		} else {
			shapes = slices.Clone(proves)
		}

		defined = true
	}

	slices.Sort(shapes)
	proofs := make([]string, len(shapes))
	for i, shape := range shapes {
		proofs[i] = string(shape)
	}

	return strings.Join(proofs, " "), defined
}

// resolveKeyShape returns the shape of the block that holds the given key. A
// keyword's key is read as written, whatever the walk made of it: one written
// in blocks of one shape gives that shape, and one the dumps declare gives
// what its command gives. A command declared as one of triggers or effects
// gives that. A key nothing else explains gives modifiers or fields. A link,
// a list iterator, a parameter and a call give "".
func (c *counter) resolveKeyShape(key string, context *Context) game.Shape {
	if shape, ok := c.resolveKeywordShape(strings.ToLower(key)); ok {
		return shape
	}

	switch {
	case context.Category == CategoryKeyword, context.Category == CategoryParameter:
		return ""
	case context.Category == CategoryCommand && context.Command != nil:
		return c.resolveCommandShape(key, context.Command)
	case c.isUnexplained(key, context):
		return c.resolveFieldShape(key)
	}

	return ""
}

// resolveKeywordShape returns the shape of the block that holds a key, in
// lower case, that is a keyword written in any block its holders name. The
// key's first such row decides, as it does in a lookup: it gives its one
// holder, else what its command in the dumps gives, else "". A keyword with
// no class gives "", since its key is a common field's too, as modifier and
// trigger are. It returns false if the key is no such keyword.
func (c *counter) resolveKeywordShape(key string) (game.Shape, bool) {
	row := game.LookupGame(c.schema.Game)
	for _, keyword := range game.Keywords {
		if keyword.Key != key || keyword.Parent != "" || !row.Matches(keyword.Games) {
			continue
		}

		command := c.schema.FindCommand(key)
		switch {
		case keyword.Class == "":
		case len(keyword.Holders) == 1:
			return keyword.Holders[0], true
		case len(keyword.Holders) == 0 && command != nil:
			return c.resolveCommandShape(key, command), true
		}

		return "", true
	}

	return "", false
}

// resolveCommandShape returns the shape of the block that holds the given
// command. A trigger gives triggers and an effect gives effects. One that is
// also a modifier and one written in a case the dumps do not use give
// shapeAmbiguous. A list iterator, a command declared as neither and one
// declared as both give "": a block of either may hold it.
func (c *counter) resolveCommandShape(key string, command *Definition) game.Shape {
	if _, iterator := game.MatchListIterator(key); iterator {
		return ""
	}

	effects, triggers := c.schema.Databases[game.DumpEffects].Definitions, c.schema.Databases[game.DumpTriggers].Definitions
	switch {
	case command.Database == game.DumpLinks, effects[command.Key] != nil && triggers[command.Key] != nil:
		return ""
	case c.schema.declaresModifier(key), effects[key] == nil && triggers[key] == nil:
		return shapeAmbiguous
	case command.Database == game.DumpTriggers:
		return game.ShapeTriggers
	}

	return game.ShapeEffects
}

// isUnexplained reports whether only a field can explain a key. No rule fits
// it, or its rule says it is a field, or it is a call to a name that no
// definition has.
func (c *counter) isUnexplained(key string, context *Context) bool {
	switch context.Category {
	case CategoryNone, CategoryField, CategoryReference:
		return true

	case CategoryCall:
		_, iterator := game.MatchListIterator(key)

		return context.Field == nil && (!iterator || c.schema.Lists == "")
	}

	return false
}

// resolveFieldShape returns the shape of the block that holds a key nothing
// else explains: modifiers, fields, or shapeAmbiguous for a modifier that is
// a command too.
func (c *counter) resolveFieldShape(key string) game.Shape {
	modifier := c.schema.declaresModifier(key)
	command := c.schema.FindCommand(key) != nil
	switch {
	case modifier && command:
		return shapeAmbiguous
	case modifier:
		return game.ShapeModifiers
	}

	return game.ShapeFields
}

// countField counts a statement as a write of the field its key names under
// the holder's field. It returns that field, or nil if the statement is no
// field: one written under no field, a definition, a command, a link, a call
// to a definition or a value written alone.
func (c *counter) countField(parent holder, tree *jomini.Tree, statement int32, context *Context) *Field {
	current := tree.Statements[statement]
	key := tree.Unquoted(current.Key)
	if parent.field == nil || current.Key.IsEmpty() || isOwnDefinition(context) || !c.isUnexplained(key, context) {
		return nil
	}

	rawKey := key
	if current.KeyClass.IsNumberKey() {
		key = jomini.NumberKey
	}

	field := parent.field.buildChild(key, game.SourceNone)
	field.Vanilla.Count++
	if parent.keys != nil && !parent.keys[key] {
		parent.keys[key] = true
		field.Vanilla.definitions++
	}

	if current.IsBlock() {
		field.Vanilla.Blocks++
		if c.isColor(tree, statement, rawKey) {
			field.Vanilla.colors++
		}
	}

	return field
}

// isOwnDefinition reports whether a statement defines a name of its own,
// which an alias does not.
func isOwnDefinition(context *Context) bool {
	return context.Defines && !context.Definition.Alias
}

// isColor reports whether a block is a color. A block with a color tag is
// one. A bare block of three or four numbers is one only under a key that
// ends as the file type says.
func (c *counter) isColor(tree *jomini.Tree, statement int32, key string) bool {
	_, ok := jomini.ToColor(tree, statement)

	return ok && (!tree.Statements[statement].Tag.IsEmpty() || c.colorSuffix != "" && strings.HasSuffix(key, c.colorSuffix))
}

// findValueField returns the field that counts the value a statement writes:
// the field its key names, the arguments of the command its key names, or
// the field of the block that holds a value written alone. It returns nil if
// the statement writes no value or the value is a definition.
func (c *counter) findValueField(parent holder, field *Field, current jomini.Statement, context *Context) *Field {
	switch {
	case current.IsBlock() || current.Value.IsEmpty() || isOwnDefinition(context):
		return nil
	case field != nil:
		return field
	case context.Category == CategoryCommand && context.Command != nil:
		return mirror(c.arguments, context.Command)
	case current.IsListed():
		return parent.field
	}

	return nil
}

// mirror returns the field that a worker counts into for the given key,
// making it if there is none.
func mirror[K comparable](fields map[K]*Field, key K) *Field {
	if fields[key] == nil {
		fields[key] = &Field{}
	}

	return fields[key]
}

// mirrorSet returns the set that a worker counts into for the given key,
// making it if there is none.
func mirrorSet(sets map[string]map[string]bool, key string) map[string]bool {
	if sets[key] == nil {
		sets[key] = map[string]bool{}
	}

	return sets[key]
}

// isFrame reports whether a statement is the block of a definition that has
// no name. Its keys are fields of its database's definition.
func (c *counter) isFrame(context *Context) bool {
	database := c.schema.Databases[context.Database]

	return database != nil && context.Field == database.Field && context.Definition == nil
}

// add adds the counts of another worker to this one's.
func (c *counter) add(other *counter) {
	for field, counted := range other.fields {
		mirror(c.fields, field).add(counted)
	}

	for command, counted := range other.arguments {
		mirror(c.arguments, command).add(counted)
	}

	for database, counted := range other.definitions {
		mirror(c.definitions, database).add(counted)
	}

	for database, labels := range other.defined {
		maps.Copy(mirrorSet(c.defined, database), labels)
	}

	maps.Copy(c.messages, other.messages)
	maps.Copy(c.unlisted, other.unlisted)
	for prefix, labels := range other.prefixed {
		c.prefixed[prefix] = addCounts(c.prefixed[prefix], labels)
	}

	for name, examples := range other.examples {
		for _, example := range examples {
			c.examples[name] = keepExample(c.examples[name], example)
		}
	}
}

// add adds the counts of another field and its children to this field's. A
// child that no source declared gets game.SourceVanilla.
func (f *Field) add(counted *Field) {
	f.Vanilla.add(counted.Vanilla)
	for key, child := range counted.Children {
		f.buildChild(key, game.SourceVanilla).add(child)
	}
}

// add adds the given counts to these.
func (v *Vanilla) add(counted Vanilla) {
	v.Count += counted.Count
	v.Blocks += counted.Blocks
	v.colors += counted.colors
	v.definitions += counted.definitions
	v.Values = addCounts(v.Values, counted.Values)
	v.shapes = addCounts(v.shapes, counted.shapes)
	v.proofs = addCounts(v.proofs, counted.proofs)
}

// resolveCounts gives the schema what the count found: fields with their
// claims, the databases only the walk names, the prefixes, the examples and
// the command patterns.
func (c *counter) resolveCounts() {
	row := game.LookupGame(c.schema.Game)
	for field, counted := range c.fields {
		field.add(counted)
	}

	for typeName, dataType := range c.schema.DataTypes {
		for name, function := range dataType.Functions {
			function.Examples = c.examples[typeName+"."+name]
			dataType.Functions[name] = function
		}
	}

	for command, counted := range c.arguments {
		command.makeField(game.SourceVanilla).add(counted)
	}

	for _, database := range slices.Sorted(maps.Keys(c.defined)) {
		c.schema.buildDatabase(row, database, game.Declaration{})
	}

	for database, counted := range c.definitions {
		c.schema.buildDatabase(row, database, game.Declaration{})
		c.schema.Databases[database].Field.add(counted)
	}

	c.resolvePatterns()
	c.schema.walkFields(c.resolveClaims)
	for prefix, labels := range c.prefixed {
		c.resolvePrefix(prefix, labels)
	}

	for _, database := range c.schema.Databases {
		demoteRequired(database.Field)
	}

	c.schema.UnlistedCalls = c.unlisted
}

// demoteRequired settles which keys the definitions of a database must
// write, given the field of its definitions. A key that a firm source calls
// required is not required of a mod where a definition the game ships
// leaves it out: its claim then stands as the game's own files show it,
// which accuses nothing.
func demoteRequired(root *Field) {
	for _, field := range root.Children {
		if field.Required.Value == RequirementAlways && field.Vanilla.definitions != root.Vanilla.definitions {
			field.Required.Source = game.SourceVanilla
		}
	}
}

// resolvePatterns finds the command patterns. A database makes one where two
// or more of its definitions, every plain one it has, each fill the
// pattern's star in a command the dumps declare.
func (c *counter) resolvePatterns() {
	owners := map[string][]string{}
	for database, labels := range c.defined {
		for label := range labels {
			owners[label] = append(owners[label], database)
		}
	}

	type patternKey struct{ dump, pattern, database string }

	// members holds the commands of one dump that fit each pattern with a
	// name of the database in its star
	members := map[patternKey][]*Definition{}
	for _, dump := range commandDatabases {
		owner := c.schema.Databases[dump]
		for _, command := range owner.Keys {
			for pattern, label := range splitPattern(command) {
				for _, database := range owners[label] {
					key := patternKey{dump, pattern, database}
					members[key] = append(members[key], owner.Definitions[command])
				}
			}
		}
	}

	for key, commands := range members {
		// one command for each definition: a name written twice, or in an
		// entry mode, makes no pattern
		if len(commands) < 2 || len(commands) != c.schema.Databases[key.database].Field.Vanilla.definitions {
			continue
		}

		// the pattern has the facts and the keys of its first command
		pattern := *commands[0]
		pattern.Key, pattern.Star, pattern.Patterns = key.pattern, []string{key.database}, nil
		owner := c.schema.Databases[key.dump]
		owner.Patterns = append(owner.Patterns, &pattern)
		for _, command := range commands {
			command.Patterns = append(command.Patterns, &pattern)
		}
	}

	order := func(first, second *Definition) int {
		return cmp.Or(strings.Compare(first.Key, second.Key), strings.Compare(first.Star[0], second.Star[0]))
	}
	for _, dump := range commandDatabases {
		owner := c.schema.Databases[dump]
		slices.SortFunc(owner.Patterns, order)
		for _, command := range owner.Definitions {
			slices.SortFunc(command.Patterns, order)
		}
	}
}

// splitPattern yields each way a command's name splits into a pattern, with
// a star in the place of a run of its underscore parts, and the label that
// run is. The whole name is never a label.
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

// walkFields calls the given function on every field of the databases'
// definitions and the commands' arguments, with whether a command holds the
// field.
func (s *Schema) walkFields(visit func(field *Field, held bool)) {
	each := func(root *Field, held bool) {
		visit(root, held)
		walkField(root, func(_ string, field *Field) { visit(field, held) })
	}
	for _, database := range s.Databases {
		each(database.Field, false)
	}

	// the effect and the trigger of a shared word hold one field
	visited := map[*Field]bool{}
	for _, dump := range commandDatabases {
		for _, command := range s.Databases[dump].Definitions {
			if command.Field != nil && !visited[command.Field] {
				visited[command.Field] = true
				each(command.Field, true)
			}
		}
	}
}

// valueTests are the types a count can give a field, in the order they are
// tried. A field gets the first type that every value written at it fits.
var valueTests = []ValueType{ValueTypeBool, ValueTypeNumber, ValueTypeDate, ValueTypeCall, ValueTypeTarget, ValueTypeText}

// resolveClaims gives a field the claims its counts support: its shape, the
// database its block's keys name and its value. The flag says whether a
// command holds the field. A stated claim is kept, but a reference with no
// database, a block or a text takes the reference the game's files show.
func (c *counter) resolveClaims(field *Field, held bool) {
	vanilla := field.Vanilla
	if shape := resolveShapeClaim(vanilla); shape != "" {
		field.Shape.resolve(shape, game.SourceVanilla)
	}

	// a command's own block, whose shape no source states, takes its
	// arguments and the effects or triggers it runs
	if !held || field.Keys.IsStated() && field.Shape.IsStated() {
		c.resolveKeys(field)
	}

	stated := field.Value.Value
	open := stated.Type == ValueTypeReference && stated.Database == "" || stated.Type == ValueTypeBlock || stated.Type == ValueTypeText

	// a target that names a database is firm only where the database defines
	// every name the game's own files write
	if stated.Type == ValueTypeTarget && stated.Database != "" &&
		!hasAll(c.listLabels(vanilla.Values), c.defined[stated.Database]) {
		field.Value.Source = game.SourceVanilla
	}

	// a field written as numbers, script values and script value blocks
	// takes a script value; a stated number takes one too if the game's
	// files write one, as EU5's add_cooldown does in
	// years = INTERNATIONAL_ORGANIZATION_SAME_AGENDA_COOLDOWN_YEARS
	valued := (vanilla.Blocks == 0 || field.Shape.Value == game.ShapeValue) && c.takesValues(vanilla.Values)
	if field.Value.IsStated() && stated.Type == ValueTypeNumber && valued {
		field.Value.Value.Values = true
	}

	if field.Value.IsStated() && !open {
		return
	}

	// a number names a definition where the database every label names also
	// names a label that is no number, as CK3's character ids are written
	// beside character keys
	expected, ok := c.resolveReference(vanilla.Values)
	labels := slices.Collect(maps.Keys(c.listLabels(vanilla.Values)))
	named := ok && slices.ContainsFunc(labels, func(label string) bool { return jomini.Classify(label) != jomini.ClassNumber })
	literal := slices.ContainsFunc(slices.Collect(maps.Keys(vanilla.Values)), func(value string) bool {
		isName := named && jomini.Classify(value) == jomini.ClassNumber && c.defined[expected.Database][value]

		return c.isLiteral(value) && !isName
	})

	// a game concept's name is a common word, so a block or a text never
	// refers to one; a GUI database's names are common words too, such as
	// value, as they are for the keys (resolveKeys)
	concept := expected.Database == c.schema.Concepts
	gui := strings.HasPrefix(expected.Database, game.GUI.DatabasePrefix)
	if ok && !literal && !gui && (!field.Value.IsStated() || stated.Type == ValueTypeReference || !concept) {
		field.Value = Claim[Expected]{Value: expected, Source: game.SourceVanilla}

		return
	}

	if valued {
		field.Value.resolve(Expected{Type: ValueTypeNumber, Values: true}, game.SourceVanilla)

		return
	}

	switch {
	case vanilla.Count == 0: // a command's argument counts its values but not its writes
	case vanilla.Blocks == vanilla.Count && vanilla.colors == vanilla.Count:
		field.Value.resolve(Expected{Type: ValueTypeColor}, game.SourceVanilla)
	case vanilla.Blocks == vanilla.Count:
		field.Value.resolve(Expected{Type: ValueTypeBlock}, game.SourceVanilla)
	case vanilla.Blocks == 0:
		for _, valueType := range valueTests {
			fits := len(vanilla.Values) > 0
			for value := range vanilla.Values {
				fits = fits && c.schema.MatchValue(Expected{Type: valueType}, value)
			}

			if fits {
				field.Value.resolve(Expected{Type: valueType}, game.SourceVanilla)

				return
			}
		}
	}
}

// resolveShapeClaim returns the one shape the blocks of a field were written
// in, value winning over triggers and effects. It returns game.ShapeUnknown
// if the shapes disagree, and "" if the blocks tell nothing.
func resolveShapeClaim(vanilla Vanilla) game.Shape {
	_, valued := vanilla.shapes[game.ShapeValue]
	var shapes []game.Shape
	switch {
	case len(vanilla.proofs) > 0:
		for proofs := range vanilla.proofs {
			var allowed []game.Shape
			for shape := range strings.FieldsSeq(proofs) {
				allowed = append(allowed, game.Shape(shape))
			}

			if shapes == nil {
				shapes = allowed
			}

			shapes = slices.DeleteFunc(shapes, func(shape game.Shape) bool { return !slices.Contains(allowed, shape) })
		}

		if valued {
			shapes = slices.DeleteFunc(shapes, func(shape game.Shape) bool { return shape != game.ShapeValue })
		} else if len(shapes) > 1 {
			shapes = slices.DeleteFunc(shapes, func(shape game.Shape) bool { return shape == game.ShapeValue })
		}

	case len(vanilla.shapes) > 0:
		shapes = slices.Sorted(maps.Keys(vanilla.shapes))
		if valued {
			// a value block takes triggers and effects too
			shapes = slices.DeleteFunc(shapes, func(shape game.Shape) bool {
				return shape == game.ShapeTriggers || shape == game.ShapeEffects
			})
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

// resolveKeys sets the database that a field's block's keys name, as a
// culture's opinions name cultures, and folds the keys only the game's own
// files write there into the <any> field. A stated database is kept; else
// the keys name one only if every key is a definition of it.
func (c *counter) resolveKeys(field *Field) {
	takes := shapeRows[field.Shape.Value].takes
	if slices.Contains(takes, game.DumpEffects) || slices.Contains(takes, game.DumpTriggers) {
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
		expected, ok = c.resolveReference(keys)
	}

	// keys never name the game concepts, localization, a GUI database, whose
	// names are common words such as value, or a database with nested ones
	if !ok || expected.Nested || expected.Database == c.schema.Concepts || expected.Database == game.Localization ||
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

// resolveReference returns the reference that every given value makes, to
// the one database, or a database with its nested ones, that defines them
// all. The game concepts count only if nothing else does, and the
// localization only if neither does. It returns false if no database or
// several do.
func (c *counter) resolveReference(values map[string]int) (Expected, bool) {
	labels := c.listLabels(values)
	if len(labels) == 0 {
		return Expected{}, false
	}

	var matched []string
	for database, names := range c.defined {
		if hasAll(labels, names) {
			matched = append(matched, database)
		}
	}

	databases := slices.DeleteFunc(slices.Clone(matched), func(database string) bool {
		return database == c.schema.Concepts || c.isNamedFor(database, matched)
	})
	nested := false
	if len(databases) == 0 {
		for label, database := range c.schema.Databases {
			members := []map[string]bool{c.defined[label]}
			for _, member := range database.Nested {
				members = append(members, c.defined[member])
			}

			if len(database.Nested) > 0 && hasAll(labels, members...) {
				databases = append(databases, label)
			}
		}

		nested = len(databases) > 0
	}

	if len(databases) == 0 && slices.Contains(matched, c.schema.Concepts) {
		databases = append(databases, c.schema.Concepts)
	}

	if len(databases) == 0 && hasAll(labels, c.messages) {
		databases = append(databases, game.Localization)
	}

	if len(databases) != 1 {
		return Expected{}, false
	}

	return Expected{Type: ValueTypeReference, Database: databases[0], Nested: nested}, true
}

// listLabels returns the labels that the given values write, each with its
// count. A quoted value counts as the label inside its quotes. A value that
// holds a parameter is left out, and so is a link, which leads to a scope
// rather than naming a definition.
func (c *counter) listLabels(values map[string]int) map[string]int {
	labels := map[string]int{}
	for value, count := range values {
		label := strings.Trim(value, `"`)
		if jomini.Classify(label) != jomini.ClassParameter && !c.schema.IsLink(label) {
			labels[label] += count
		}
	}

	return labels
}

// hasAll reports whether every given value is held by at least one of the
// given sets.
func hasAll(values map[string]int, sets ...map[string]bool) bool {
	for value := range values {
		if !slices.ContainsFunc(sets, func(set map[string]bool) bool { return set[value] }) {
			return false
		}
	}

	return true
}

// isNamedFor reports whether the definitions of a database are named for
// those of another of the given databases, which all define the values: its
// row names one of them as its target, or it mirrors others and is not
// alone.
func (c *counter) isNamedFor(label string, others []string) bool {
	database := c.schema.Databases[label]

	return database != nil && (database.Declaration.Target != "" && slices.Contains(others, database.Declaration.Target) ||
		database.Declaration.Mirrors && len(others) > 1)
}

// isLiteral reports whether a value is one of the language's own: a number,
// a date or a yes-or-no word.
func (c *counter) isLiteral(value string) bool {
	return slices.ContainsFunc([]ValueType{ValueTypeBool, ValueTypeNumber, ValueTypeDate}, func(valueType ValueType) bool {
		return c.schema.MatchValue(Expected{Type: valueType}, value)
	})
}

// takesValues reports whether a field takes a number that a script value may
// give: of the values the game's own files write in it, one names a script
// value, as add_stability = stability_mild_bonus does, and every other is a
// number, a parameter, a chain of links or a command, alone or with its
// argument in parentheses.
func (c *counter) takesValues(values map[string]int) bool {
	names := c.defined[c.schema.Values]
	named := false
	for value := range values {
		text := strings.Trim(value, `"`)
		class := jomini.Classify(text)
		declared := c.schema.FindCommand(text) != nil
		_, _, called := c.schema.findKeyCall(text, 0)
		switch {
		case names[text]:
			named = true
		case class == jomini.ClassNumber || class == jomini.ClassParameter || c.schema.IsLink(text) || declared || called: // allowed
		default:
			return false
		}
	}

	return named
}

// resolvePrefix sets the database that defines every name the game's own
// files write after a prefix, none if no database or several do, and records
// the names counted in its link's arguments. A prefix that leads to a scope
// from any scope tries that scope's database first.
func (c *counter) resolvePrefix(prefix string, labels map[string]int) {
	database := c.schema.findScopeDatabase(prefix)
	expected, ok := Expected{Type: ValueTypeReference, Database: database}, database != ""
	if !ok || !hasAll(c.listLabels(labels), c.defined[database]) {
		expected, ok = c.resolveReference(labels)
	}

	arguments := c.schema.lookupLink(prefix, true).makeField(game.SourceVanilla)
	arguments.Vanilla = Vanilla{Values: labels}
	for _, count := range labels {
		arguments.Vanilla.Count += count
	}

	if ok {
		arguments.Value.resolve(expected, game.SourceVanilla)
	}
}

// findScopeDatabase returns the database whose definitions the names after a
// link prefix are, where the prefix leads to a scope from any scope: the
// database of the type the scope's own getter gives, else of the scope's
// phrase. It returns "" for any other prefix, and where neither gives one.
func (s *Schema) findScopeDatabase(prefix string) string {
	command := s.lookupLink(prefix, true)
	if command == nil || len(command.Scopes) > 0 || !s.isScope(command.Target) {
		return ""
	}

	getters := s.DataTypes[anyScopeType].Functions
	for _, name := range slices.Sorted(maps.Keys(getters)) {
		database, ok := s.ResolveTypeDatabase(getters[name].Returns)
		if ok && ToPhrase(DropGetter(name)) == ToPhrase(command.Target) {
			return database
		}
	}

	database, _ := s.ResolveTypeDatabase(command.Target)

	return database
}
