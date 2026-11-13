// vanilla.go counts what the game's own files write: which keys each block
// has, what values each key is given, and what kind of script each block
// holds. From the counts it fills in what no dump and no doc states.

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
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/internal/loc"
)

// counter is what one worker has counted in the files it walked. Each worker
// counts into fields of its own, and the counts are added to the schema once
// every file is walked.
type counter struct {
	builder *builder

	// contexts is the walk's result for the file being counted, and holders
	// where the keys of each of its statements' blocks are counted. Both are
	// reused from file to file.
	contexts []lang.Context
	holders  []holder

	// path is the path of the file being counted. colorSuffix is how a key of
	// that file ends when its block of plain numbers is a color, as color
	// does. It is "" for a file with no such keys.
	path        string
	colorSuffix string

	// fields holds, for a field of the schema, the worker's own copy that it
	// counts into.
	fields map[*lang.Field]*lang.Field

	// tallies holds, for a field, the counts that are needed only to decide
	// what the field takes and that the schema does not keep. A field with
	// no entry has none.
	tallies map[*lang.Field]*tally

	// arguments holds, for a command, the field the worker counts the keys of
	// the command's block into.
	arguments map[*lang.Definition]*lang.Field

	// definitions holds, for a database, the field the worker counts the keys
	// of its definitions into. defined holds the names each database's files
	// define.
	definitions map[string]*lang.Field
	defined     map[string]map[string]bool

	// messages holds the key of every localization message, in any language.
	messages map[string]bool

	// unlisted holds the steps of data calls, each as Type.step, that a text
	// writes though no dump declares the step for that type.
	unlisted map[string]bool

	// gui is whether the file being counted is a GUI file.
	gui bool

	// prefixed holds, for a link written with a name after a colon, how many
	// times each name is written after it.
	prefixed map[string]map[string]int

	// examples holds the first places each data function is called, by the
	// function's name written as Type.Function.
	examples map[string][]lang.Example
}

// holder says where the keys written inside one statement are counted.
type holder struct {
	// field is the field the keys are counted into. It is nil where the keys
	// are not fields.
	field *lang.Field

	// shaped is whether the keys in the statement's block are evidence of
	// what the block holds: an effect among them says it holds effects.
	shaped bool

	// keys holds the keys a definition has written so far, so that a key
	// written twice in it counts once. It is nil for a statement that is no
	// plain definition.
	keys map[string]bool
}

// tally holds the counts for one field that are needed only to decide what
// the field takes. The schema does not keep them.
type tally struct {
	// colors is how many of the field's blocks are a color. definitions is
	// how many definitions write the field.
	colors      int
	definitions int

	// shapes holds, for each kind of script, how many keys in the field's
	// blocks are of that kind: an effect counts for effects.
	shapes map[lang.Shape]int

	// proofs holds how many calls in the field's blocks are of scripted
	// definitions that may be called from the same kinds of block. Each key
	// is those kinds, joined by spaces.
	proofs map[string]int
}

// shapeAmbiguous marks a key that is no evidence of what its block holds,
// because it fits more than one kind of block. No block has this shape.
const shapeAmbiguous lang.Shape = "ambiguous"

// countVanilla walks every file the game ships, counts what they write, and
// gives the schema what the counts show. Several workers parse and count at
// once, so the parser must be safe to call from several at a time. It
// returns an error only if no script file could be parsed.
func (b *builder) countVanilla(listing []files.File, parse func(path string) (*jomini.Tree, error)) error {
	localizing := slices.DeleteFunc(slices.Clone(listing), func(file files.File) bool {
		return file.Format != files.FormatLocalization
	})
	listing = slices.DeleteFunc(slices.Clone(listing), func(file files.File) bool { return !file.Format.IsScript() })

	// the files that declare scripted definitions are read first, so that a
	// key that calls one is counted as a call
	declaring := slices.DeleteFunc(slices.Clone(listing), func(file files.File) bool { return !b.Declares(file) })
	b.ResolveRelays(b.DeclareFiles(declaring, parse), nil)

	counters := make([]*counter, files.Workers)
	for worker := range counters {
		counters[worker] = &counter{
			builder: b, fields: map[*lang.Field]*lang.Field{}, tallies: map[*lang.Field]*tally{},
			arguments: map[*lang.Definition]*lang.Field{}, definitions: map[string]*lang.Field{},
			defined: map[string]map[string]bool{}, messages: map[string]bool{}, unlisted: map[string]bool{},
			prefixed: map[string]map[string]int{}, examples: map[string][]lang.Example{},
		}
	}

	lang.EachParsed(localizing, func(path string) (*loc.File, error) {
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

	failures := lang.EachParsed(listing, parse, func(worker, index int, tree *jomini.Tree) {
		counter, file := counters[worker], listing[index]
		counter.path, counter.gui = file.Path, file.Format == files.FormatGUI
		counter.colorSuffix = lang.FileTypes[strings.ToLower(path.Ext(file.Rel))].ColorSuffix
		counter.count(tree, file)
	})

	for _, other := range counters[1:] {
		counters[0].add(other)
	}

	counters[0].resolveCounts()

	// the same files are read again, now that the fields are counted, so
	// that what each parameter takes can be worked out from them
	winners := b.DeclareFiles(declaring, parse)
	b.TypeParameters(winners)
	b.ResolveRelays(winners, nil)

	if len(failures) == len(listing) && len(failures) > 0 {
		return errors.Join(failures...)
	}

	return nil
}

// count walks one file and counts each of its statements. The file as a
// whole is counted only where the file is itself one definition.
func (c *counter) count(tree *jomini.Tree, file files.File) {
	c.contexts, _ = c.builder.Walk(tree, file, c.contexts)
	c.holders = slices.Grow(c.holders[:0], len(c.contexts))[:len(c.contexts)]
	c.holders[0] = holder{}

	for position := range c.contexts {
		statement, context := int32(position), &c.contexts[position]
		if statement > 0 || context.Definition != nil {
			c.countStatement(tree, statement, context)
		}
	}
}

// countStatement counts one statement: the name it defines, the names it
// writes after a link's colon, the data functions it calls, its value, and
// its key as a field of the block around it. It also settles where the keys
// inside the statement will be counted.
func (c *counter) countStatement(tree *jomini.Tree, statement int32, context *lang.Context) {
	// nothing is around the file's own statement
	var parent holder
	if statement > 0 {
		parent = c.holders[tree.Statements[statement].Parent]
	}

	c.countDefinition(context)
	current := tree.Statements[statement]
	c.countPrefixes(tree, current)
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
	// the numbers in a color's block are its parts and are not counted as
	// values
	case field != nil && (!block || !c.isColor(tree, statement, tree.Text(current.Key))):
		next.field = field

	// the keys inside a keyword such as if are counted where the keys
	// around it are
	case context.Keyword != nil && context.Keyword.Shape == lang.ShapeEnclosing:
		c.holders[statement] = parent

		return

	case context.Defines && context.Category == lang.CategoryDefinition && context.Definition.Mode == "":
		next = holder{field: mirror(c.definitions, context.Database), keys: map[string]bool{}}
		mirror(c.tallies, next.field).definitions++

	case context.Defines && context.Category == lang.CategoryDefinition, c.isFrame(context):
		next.field = mirror(c.definitions, context.Database)

	// the keys in a command's block are its arguments, and are no evidence
	// of what the block holds
	case context.Category == lang.CategoryCommand && block:
		next.field = mirror(c.arguments, context.Command)
		block = false

	// the keys in the block of a keyword that has a field, such as a
	// random_list's branch, are counted into that field
	case context.Category == lang.CategoryKeyword && block && context.Field != nil:
		next.field = mirror(c.fields, context.Field)
		block = false
	}

	next.shaped = next.field != nil && block
	c.holders[statement] = next
}

// countDefinition records the name a statement defines in its database. A
// second name a key gives a definition, as a trait's group does, is recorded
// too.
func (c *counter) countDefinition(context *lang.Context) {
	if !context.Defines || context.Category != lang.CategoryDefinition && !context.Definition.Alias {
		return
	}

	mirrorSet(c.defined, context.Definition.Database)[context.Definition.Name] = true
}

// countPrefixes counts the names a statement's key and value write after a
// link's colon, as norse in culture:norse. It counts only for a link whose
// database is not yet known.
func (c *counter) countPrefixes(tree *jomini.Tree, current jomini.Statement) {
	for _, span := range []jomini.Span{tree.UnquotedSpan(current.Key), tree.UnquotedSpan(current.Value)} {
		// most keys and values have no colon, and need no splitting
		if !strings.Contains(tree.Text(span), ":") {
			continue
		}

		for _, link := range jomini.Links(tree.Text(span), span.Start) {
			prefix := tree.Text(link.Prefix)
			if !link.Prefix.IsEmpty() && c.builder.isVoted(prefix) {
				c.prefixed[prefix] = addCounts(c.prefixed[prefix], map[string]int{tree.Text(link.Name): 1})
			}
		}
	}
}

// dumpLiteral is what the dumps give as the scope of a link whose name is
// just a word and names nothing, as the_boss in flag:the_boss.
const dumpLiteral = "flag"

// isVoted reports whether the names written after a link's colon should be
// counted to find the database they belong to. They should if the link takes
// a name, its database is not known yet, and its name is not just a word.
func (b *builder) isVoted(prefix string) bool {
	database, exists := b.LookupPrefix(prefix)

	return exists && database == "" && b.LookupLink(prefix, true).Target != dumpLiteral
}

// addCounts adds each count of the second map to the first map and returns
// the first. A nil first map is made if there is anything to add.
func addCounts[K comparable](total, added map[K]int) map[K]int {
	if len(added) > 0 && total == nil {
		total = map[K]int{}
	}

	for key, count := range added {
		total[key] += count
	}

	return total
}

// countCalls records where a statement's value calls data functions, as
// [Character.GetName] calls GetName of Character. Each place is kept as an
// example of the function.
func (c *counter) countCalls(tree *jomini.Tree, statement int32, context *lang.Context) {
	call := context.Call
	if call == nil {
		return
	}

	value := tree.Statements[statement].Value
	example := lang.Example{Path: c.path, Line: tree.Lines.ToPosition(value.Start).Line}
	owners, _ := c.builder.ListCallOwners(*call, context.Datacontext)
	for i, owner := range owners {
		if owner != "" {
			name := owner + "." + call.Steps[i].Text
			c.examples[name] = keepExample(c.examples[name], example)
		}
	}
}

// ExampleCount is how many places are kept as the examples of one data
// function.
const ExampleCount = 5

// keepExample adds a place to the places kept for one data function, and
// returns them. They are sorted by path and then by line, and only the
// first five are kept. A place already kept is not added again.
func keepExample(examples []lang.Example, example lang.Example) []lang.Example {
	// the list is sorted, so the result is the same whichever worker read
	// which file
	position, exists := slices.BinarySearchFunc(examples, example, func(kept, added lang.Example) int {
		return cmp.Or(strings.Compare(kept.Path, added.Path), cmp.Compare(kept.Line, added.Line))
	})
	if exists || position == ExampleCount {
		return examples
	}

	examples = slices.Insert(examples, position, example)

	return examples[:min(len(examples), ExampleCount)]
}

// countUnlisted records the steps of a text's data calls that no dump
// declares.
func (c *counter) countUnlisted(segments []loc.Segment, datacontext string) {
	for _, segment := range segments {
		if segment.Call == nil {
			continue
		}

		if step, before, ok := c.builder.FindUnlisted(*segment.Call, datacontext); ok {
			c.unlisted[before+"."+segment.Call.Steps[step].Text] = true
		}
	}
}

// countShape counts one key as evidence of what the block around it holds.
// A key that is an effect is evidence of effects, and a trigger of triggers.
// A key that calls a scripted definition is evidence of the kinds of block
// that definition may be called from. A key that is evidence of nothing
// counts nothing.
func (c *counter) countShape(field *lang.Field, tree *jomini.Tree, statement int32, context *lang.Context) {
	if tree.Statements[statement].Key.IsEmpty() {
		return
	}

	key := c.builder.FindCallee(tree, statement)
	if context.Category == lang.CategoryCall {
		if proofs, isDefined := c.resolveProofs(key); isDefined {
			if proofs != "" {
				counts := mirror(c.tallies, field)
				counts.proofs = addCounts(counts.proofs, map[string]int{proofs: 1})
			}

			return
		}
	}

	if shape := c.resolveKeyShape(key, context); shape != "" {
		counts := mirror(c.tallies, field)
		counts.shapes = addCounts(counts.shapes, map[lang.Shape]int{shape: 1})
	}
}

// resolveProofs returns the kinds of block a scripted definition of the
// given name may be called from, sorted and joined by spaces. Where several
// databases define the name, they are the kinds every one of them allows,
// and "" if they share none. It returns false if no database defines the
// name.
func (c *counter) resolveProofs(key string) (string, bool) {
	var shapes []lang.Shape
	isDefined := false
	for _, database := range lang.Called {
		if c.builder.LookupScripted(nil, database, key) == nil {
			continue
		}

		proves := c.builder.Databases[database].Declaration.Proves()
		if isDefined {
			shapes = slices.DeleteFunc(shapes, func(shape lang.Shape) bool { return !slices.Contains(proves, shape) })
		} else {
			shapes = slices.Clone(proves)
		}

		isDefined = true
	}

	slices.Sort(shapes)
	proofs := make([]string, len(shapes))
	for i, shape := range shapes {
		proofs[i] = string(shape)
	}

	return strings.Join(proofs, " "), isDefined
}

// resolveKeyShape returns what a key says about the block it is written in.
// A keyword says what its row says. An effect says effects and a trigger
// says triggers. A key nothing else explains says fields, or modifiers. A
// link, a key such as every_vassal, a parameter and a call say nothing, and
// give "".
func (c *counter) resolveKeyShape(key string, context *lang.Context) lang.Shape {
	if shape, ok := c.resolveKeywordShape(strings.ToLower(key)); ok {
		return shape
	}

	switch {
	case context.Category == lang.CategoryKeyword, context.Category == lang.CategoryParameter:
		return ""
	case context.Category == lang.CategoryCommand && context.Command != nil:
		return c.resolveCommandShape(key, context.Command)
	case c.isUnexplained(key, context):
		return c.resolveFieldShape(key)
	}

	return ""
}

// resolveKeywordShape returns what a keyword, given in lower case, says
// about the block it is written in. A keyword written in blocks of one kind
// only says that kind. One the dumps also list as a command says what the
// command says. A keyword with no class says nothing, since its key is a
// common field too, as modifier and trigger are. It returns false if the
// key is no keyword that may be written in any block.
func (c *counter) resolveKeywordShape(key string) (lang.Shape, bool) {
	dialect := lang.LookupDialect(c.builder.Game)
	for _, keyword := range lang.Keywords {
		if keyword.Key != key || keyword.Parent != "" || !dialect.Matches(keyword.Games) {
			continue
		}

		command := c.builder.FindCommand(key)
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

// resolveCommandShape returns what a command says about the block it is
// written in: triggers for a trigger, effects for an effect. A command whose
// name is also a modifier's, or that is written in a letter case the dumps
// do not use, is no evidence either way. A key such as every_vassal, a
// command that is both an effect and a trigger, and one that is neither say
// nothing, since a block of either kind may hold them.
func (c *counter) resolveCommandShape(key string, command *lang.Definition) lang.Shape {
	if _, isIterator := lang.MatchListIterator(key); isIterator {
		return ""
	}

	effects, triggers := c.builder.Databases[lang.DumpEffects].Definitions, c.builder.Databases[lang.DumpTriggers].Definitions
	switch {
	case command.Database == lang.DumpLinks, effects[command.Key] != nil && triggers[command.Key] != nil:
		return ""
	case c.builder.DeclaresModifier(key), effects[key] == nil && triggers[key] == nil:
		return shapeAmbiguous
	case command.Database == lang.DumpTriggers:
		return lang.ShapeTriggers
	}

	return lang.ShapeEffects
}

// isUnexplained reports whether a key can only be a field of the block
// around it: the walk found nothing else it could be, or took it for a field
// already, or took it for a call of a name nothing defines.
func (c *counter) isUnexplained(key string, context *lang.Context) bool {
	switch context.Category {
	case lang.CategoryNone, lang.CategoryField, lang.CategoryReference:
		return true

	case lang.CategoryCall:
		_, isIterator := lang.MatchListIterator(key)

		return context.Field == nil && (!isIterator || c.builder.Lists == "")
	}

	return false
}

// resolveFieldShape returns what a key that can only be a field says about
// the block it is written in: modifiers if the dumps list a modifier of its
// name, else fields. A modifier whose name is also a command's is no
// evidence either way.
func (c *counter) resolveFieldShape(key string) lang.Shape {
	isModifier := c.builder.DeclaresModifier(key)
	isCommand := c.builder.FindCommand(key) != nil
	switch {
	case isModifier && isCommand:
		return shapeAmbiguous
	case isModifier:
		return lang.ShapeModifiers
	}

	return lang.ShapeFields
}

// countField counts a statement's key as a field of the block around it,
// and returns that field. It returns nil if the key is no field there: the
// block around it has no fields, or the statement is a definition, a
// command, a link, a call of a definition, or a value written alone.
func (c *counter) countField(parent holder, tree *jomini.Tree, statement int32, context *lang.Context) *lang.Field {
	current := tree.Statements[statement]
	key := tree.Unquoted(current.Key)
	if parent.field == nil || current.Key.IsEmpty() || isOwnDefinition(context) || !c.isUnexplained(key, context) {
		return nil
	}

	rawKey := key
	if current.KeyClass.IsNumberKey() {
		key = jomini.NumberKey
	}

	field := parent.field.BuildChild(key, lang.SourceNone)
	field.Vanilla.Count++
	if parent.keys != nil && !parent.keys[key] {
		parent.keys[key] = true
		mirror(c.tallies, field).definitions++
	}

	if current.IsBlock() {
		field.Vanilla.Blocks++
		if c.isColor(tree, statement, rawKey) {
			mirror(c.tallies, field).colors++
		}
	}

	return field
}

// isOwnDefinition reports whether a statement makes a definition. A key that
// only gives a definition a second name, as a trait's group does, does not.
func isOwnDefinition(context *lang.Context) bool {
	return context.Defines && !context.Definition.Alias
}

// isColor reports whether a block is a color. A block written with a color
// tag, as rgb { 255 0 0 }, is one. A block of three or four plain numbers is
// one only under a key that ends as the file's kind says a color's key does.
func (c *counter) isColor(tree *jomini.Tree, statement int32, key string) bool {
	_, ok := jomini.ToColor(tree, statement)

	return ok && (!tree.Statements[statement].Tag.IsEmpty() || c.colorSuffix != "" && strings.HasSuffix(key, c.colorSuffix))
}

// findValueField returns the field a statement's value is counted at: the
// field its key is, else the arguments of the command its key is, else, for
// a value written alone in a block, the field of that block. It returns nil
// for a statement with no value, and for a definition.
func (c *counter) findValueField(parent holder, field *lang.Field, current jomini.Statement, context *lang.Context) *lang.Field {
	switch {
	case current.IsBlock() || current.Value.IsEmpty() || isOwnDefinition(context):
		return nil
	case field != nil:
		return field
	case context.Category == lang.CategoryCommand && context.Command != nil:
		return mirror(c.arguments, context.Command)
	case current.IsListed():
		return parent.field
	}

	return nil
}

// mirror returns what a map holds under a key, a field or a tally, putting
// an empty one there first if the map has none.
func mirror[K comparable, V any](values map[K]*V, key K) *V {
	if values[key] == nil {
		values[key] = new(V)
	}

	return values[key]
}

// mirrorSet returns the set a map holds under a key, putting an empty set
// there first if the map has none.
func mirrorSet(sets map[string]map[string]bool, key string) map[string]bool {
	if sets[key] == nil {
		sets[key] = map[string]bool{}
	}

	return sets[key]
}

// lookupTally returns what the count kept for a field, the empty tally if it
// kept nothing.
func (c *counter) lookupTally(field *lang.Field) tally {
	if counts := c.tallies[field]; counts != nil {
		return *counts
	}

	return tally{}
}

// isFrame reports whether a statement is the block of a definition that has
// no name, in a database whose definitions have none. Its keys are fields of
// the database's definition.
func (c *counter) isFrame(context *lang.Context) bool {
	database := c.builder.Databases[context.Database]

	return database != nil && context.Field == database.Field && context.Definition == nil
}

// add adds everything another worker counted to what this one counted.
func (c *counter) add(other *counter) {
	for field, counted := range other.fields {
		c.addField(mirror(c.fields, field), counted, other)
	}

	for command, counted := range other.arguments {
		c.addField(mirror(c.arguments, command), counted, other)
	}

	for database, counted := range other.definitions {
		c.addField(mirror(c.definitions, database), counted, other)
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

// addField adds what was counted at one field to another field, and the
// same for every field inside it. The counts the schema does not keep are
// read from the given counter and added to this one's. A field inside that
// no source declared is marked as known from the game's files alone.
func (c *counter) addField(field, counted *lang.Field, other *counter) {
	addVanilla(&field.Vanilla, counted.Vanilla)
	mirror(c.tallies, field).add(mirror(other.tallies, counted))

	for key, child := range counted.Children {
		c.addField(field.BuildChild(key, lang.SourceVanilla), child, other)
	}
}

// addVanilla adds the counts of one field to those of another.
func addVanilla(vanilla *lang.Vanilla, counted lang.Vanilla) {
	vanilla.Count += counted.Count
	vanilla.Blocks += counted.Blocks
	vanilla.Values = addCounts(vanilla.Values, counted.Values)
}

// add adds another tally's counts to this one's.
func (t *tally) add(counted *tally) {
	t.colors += counted.colors
	t.definitions += counted.definitions
	t.shapes = addCounts(t.shapes, counted.shapes)
	t.proofs = addCounts(t.proofs, counted.proofs)
}

// resolveCounts gives the schema what the count found. It adds the counted
// fields to the schema's, makes a database for each name the files define in
// a database the schema lacked, and then decides from the counts what each
// field takes, which star keys exist, and which database each link's names
// belong to.
func (c *counter) resolveCounts() {
	dialect := lang.LookupDialect(c.builder.Game)

	for field, counted := range c.fields {
		c.addField(field, counted, c)
	}

	for typeName, dataType := range c.builder.DataTypes {
		for name, function := range dataType.Functions {
			function.Examples = c.examples[typeName+"."+name]
			dataType.Functions[name] = function
		}
	}

	for command, counted := range c.arguments {
		c.addField(makeField(command, lang.SourceVanilla), counted, c)
	}

	for _, database := range slices.Sorted(maps.Keys(c.defined)) {
		c.builder.buildDatabase(dialect, database, lang.Declaration{})
	}

	for database, counted := range c.definitions {
		c.builder.buildDatabase(dialect, database, lang.Declaration{})
		c.addField(c.builder.Databases[database].Field, counted, c)
	}

	c.resolvePatterns()
	c.builder.walkFields(c.resolveClaims)

	for prefix, labels := range c.prefixed {
		c.resolvePrefix(prefix, labels)
	}

	for _, database := range c.builder.Databases {
		c.demoteRequired(database.Field)
	}

	c.builder.UnlistedCalls = c.unlisted
}

// resolvePatterns finds the star keys, as has_relation_*. A database makes
// one where the dumps list a command for each of its definitions, at least
// two, whose name is the same but for the definition's own name: the dumps
// list has_relation_rival and has_relation_friend, and rival and friend are
// both scripted relations.
func (c *counter) resolvePatterns() {
	owners := map[string][]string{}
	for database, labels := range c.defined {
		for label := range labels {
			owners[label] = append(owners[label], database)
		}
	}

	type patternKey struct{ dump, pattern, database string }

	// the commands of one dump that fit each star key, by the database
	// whose names fill the star
	members := map[patternKey][]*lang.Definition{}
	for _, dump := range lang.CommandDatabases {
		owner := c.builder.Databases[dump]
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
		// there must be one command for each definition of the database. A
		// name defined twice, or with an entry mode, is not counted as one.
		definitions := c.lookupTally(c.builder.Databases[key.database].Field).definitions
		if len(commands) < 2 || len(commands) != definitions {
			continue
		}

		// the star key takes its scopes and its keys from the first command
		pattern := *commands[0]
		pattern.Key, pattern.Star, pattern.Patterns = key.pattern, []string{key.database}, nil
		owner := c.builder.Databases[key.dump]
		owner.Patterns = append(owner.Patterns, &pattern)
		for _, command := range commands {
			command.Patterns = append(command.Patterns, &pattern)
		}
	}

	order := func(first, second *lang.Definition) int {
		return cmp.Or(strings.Compare(first.Key, second.Key), strings.Compare(first.Star[0], second.Star[0]))
	}
	for _, dump := range lang.CommandDatabases {
		owner := c.builder.Databases[dump]
		slices.SortFunc(owner.Patterns, order)
		for _, command := range owner.Definitions {
			slices.SortFunc(command.Patterns, order)
		}
	}
}

// splitPattern yields every way to put a star in a command's name in place
// of one or more of its parts between underscores, each with the parts the
// star stands for: has_relation_rival gives has_relation_* with rival,
// has_*_rival with relation, and so on. The whole name is never the star.
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

// walkFields calls a function once for every field of the schema: the fields
// of each database's definitions, then the arguments of each command. The
// function is told whether the field belongs to a command.
func (b *builder) walkFields(visit func(field *lang.Field, isArgument bool)) {
	each := func(root *lang.Field, isArgument bool) {
		visit(root, isArgument)
		lang.WalkField(root, func(_ string, field *lang.Field) { visit(field, isArgument) })
	}
	for _, database := range b.Databases {
		each(database.Field, false)
	}

	// an effect and a trigger of one name share one field
	visited := map[*lang.Field]bool{}
	for _, dump := range lang.CommandDatabases {
		for _, command := range b.Databases[dump].Definitions {
			if command.Field != nil && !visited[command.Field] {
				visited[command.Field] = true
				each(command.Field, true)
			}
		}
	}
}

// valueTests are the kinds of value the counts can show a field takes, in
// the order they are tried.
var valueTests = []lang.ValueType{
	lang.ValueTypeBool, lang.ValueTypeNumber, lang.ValueTypeDate, lang.ValueTypeCall, lang.ValueTypeTarget,
	lang.ValueTypeText,
}

// resolveClaims decides from the counts what one field takes: what its block
// holds, which database the keys of its block name, and what its value is.
// The flag says whether the field belongs to a command. What another source
// already states is kept, with one exception: where that source says only
// that the value is some name, a block or a text, the database the game's
// files show it names is taken.
func (c *counter) resolveClaims(field *lang.Field, isArgument bool) {
	vanilla, counts := field.Vanilla, c.lookupTally(field)
	if shape := resolveShapeClaim(counts); shape != "" {
		field.Shape.Resolve(shape, lang.SourceVanilla)
	}

	// a command's own block holds its arguments and the effects or triggers
	// it runs, so its keys name no database unless a source says so
	if !isArgument || field.Keys.IsStated() && field.Shape.IsStated() {
		c.resolveKeys(field)
	}

	claim := field.Value.Value
	isOpen := claim.Type == lang.ValueTypeReference && claim.Database == "" || claim.Type == lang.ValueTypeBlock ||
		claim.Type == lang.ValueTypeText

	// a source that says the value may name a database is trusted only if
	// that database defines every name the game's files write there
	if claim.Type == lang.ValueTypeTarget && claim.Database != "" &&
		!hasAll(c.listLabels(vanilla.Values), c.defined[claim.Database]) {
		field.Value.Source = lang.SourceVanilla
	}

	// a field given numbers and script values takes a script value. So does
	// a field a source says takes a number, if the game's files give it a
	// script value, as EU5's add_cooldown does in years =
	// INTERNATIONAL_ORGANIZATION_SAME_AGENDA_COOLDOWN_YEARS.
	takesValue := (vanilla.Blocks == 0 || field.Shape.Value == lang.ShapeValue) && c.takesValues(vanilla.Values)
	if field.Value.IsStated() && claim.Type == lang.ValueTypeNumber && takesValue {
		field.Value.Value.Values = true
	}

	if field.Value.IsStated() && !isOpen {
		return
	}

	// a number is the name of a definition where the same field is also
	// given names that are no numbers, as CK3's character ids are
	expected, ok := c.resolveReference(vanilla.Values)
	labels := slices.Collect(maps.Keys(c.listLabels(vanilla.Values)))
	namesNumbers := ok && slices.ContainsFunc(labels, func(label string) bool { return jomini.Classify(label) != jomini.ClassNumber })
	hasLiteral := slices.ContainsFunc(slices.Collect(maps.Keys(vanilla.Values)), func(value string) bool {
		isName := namesNumbers && jomini.Classify(value) == jomini.ClassNumber && c.defined[expected.Database][value]

		return c.isLiteral(value) && !isName
	})

	// the names of game concepts and of GUI definitions are common words, so
	// a field a source says takes a block or a text is not taken to name one
	isConcept := expected.Database == c.builder.Concepts
	isGUI := strings.HasPrefix(expected.Database, lang.GUIDatabasePrefix)
	if ok && !hasLiteral && !isGUI && (!field.Value.IsStated() || claim.Type == lang.ValueTypeReference || !isConcept) {
		field.Value = lang.Claim[lang.Expected]{Value: expected, Source: lang.SourceVanilla}

		return
	}

	if takesValue {
		field.Value.Resolve(lang.Expected{Type: lang.ValueTypeNumber, Values: true}, lang.SourceVanilla)

		return
	}

	switch {
	// the arguments of a command have their values counted, but not how
	// often they are written
	case vanilla.Count == 0:

	case vanilla.Blocks == vanilla.Count && counts.colors == vanilla.Count:
		field.Value.Resolve(lang.Expected{Type: lang.ValueTypeColor}, lang.SourceVanilla)

	case vanilla.Blocks == vanilla.Count:
		field.Value.Resolve(lang.Expected{Type: lang.ValueTypeBlock}, lang.SourceVanilla)

	case vanilla.Blocks == 0:
		for _, valueType := range valueTests {
			fits := len(vanilla.Values) > 0
			for value := range vanilla.Values {
				fits = fits && c.builder.MatchValue(lang.Expected{Type: valueType}, value)
			}

			if fits {
				field.Value.Resolve(lang.Expected{Type: valueType}, lang.SourceVanilla)

				return
			}
		}
	}
}

// resolveShapeClaim returns what the blocks of a field hold, as the keys
// counted in them show. Where some keys are math and others are effects or
// triggers, the block computes a number. It returns the unknown shape where
// the keys disagree, and "" where they are evidence of nothing.
func resolveShapeClaim(counts tally) lang.Shape {
	_, hasValue := counts.shapes[lang.ShapeValue]
	var shapes []lang.Shape
	switch {
	case len(counts.proofs) > 0:
		for proofs := range counts.proofs {
			var allowed []lang.Shape
			for shape := range strings.FieldsSeq(proofs) {
				allowed = append(allowed, lang.Shape(shape))
			}

			if shapes == nil {
				shapes = allowed
			}

			shapes = slices.DeleteFunc(shapes, func(shape lang.Shape) bool { return !slices.Contains(allowed, shape) })
		}

		if hasValue {
			shapes = slices.DeleteFunc(shapes, func(shape lang.Shape) bool { return shape != lang.ShapeValue })
		} else if len(shapes) > 1 {
			shapes = slices.DeleteFunc(shapes, func(shape lang.Shape) bool { return shape == lang.ShapeValue })
		}

	case len(counts.shapes) > 0:
		shapes = slices.Sorted(maps.Keys(counts.shapes))
		if hasValue {
			// a block that computes a number may hold triggers and effects
			// too
			shapes = slices.DeleteFunc(shapes, func(shape lang.Shape) bool {
				return shape == lang.ShapeTriggers || shape == lang.ShapeEffects
			})
		}

		if slices.Contains(shapes, shapeAmbiguous) {
			return lang.ShapeUnknown
		}

	default:
		return ""
	}

	if len(shapes) != 1 {
		return lang.ShapeUnknown
	}

	return shapes[0]
}

// resolveKeys finds the database that the keys of a field's block name, as
// the keys of a culture's opinions block name cultures. It does so only
// where every key that the game's files alone write there is a definition of
// one database. Those keys are then counted together as one field that
// stands for any key. A database another source already states is kept.
func (c *counter) resolveKeys(field *lang.Field) {
	takes := lang.ShapeRows[field.Shape.Value].Takes
	if slices.Contains(takes, lang.DumpEffects) || slices.Contains(takes, lang.DumpTriggers) {
		return
	}

	keys := map[string]int{}
	for key, child := range field.Children {
		if child.Source == lang.SourceVanilla && key != lang.AnyKey && key != jomini.NumberKey {
			keys[key] = child.Vanilla.Count
		}
	}

	expected, ok := lang.Expected{Database: field.Keys.Value}, len(keys) > 0
	if !field.Keys.IsStated() {
		expected, ok = c.resolveReference(keys)
	}

	// keys are never taken to name game concepts, localization keys, GUI
	// definitions (whose names are common words such as value) or a database
	// with others nested in it
	if !ok || expected.Nested || expected.Database == c.builder.Concepts || expected.Database == lang.Localization ||
		strings.HasPrefix(expected.Database, lang.GUIDatabasePrefix) {
		return
	}

	merged := field.BuildChild(lang.AnyKey, lang.SourceVanilla)
	for key := range keys {
		c.addField(merged, field.Children[key], c)
		delete(field.Children, key)
	}

	field.Keys.Resolve(expected.Database, lang.SourceVanilla)
}

// resolveReference returns what a set of written values all name: the one
// database that defines every one of them, or a database that defines them
// all together with the databases nested in it. The game concepts count only
// if no other database does, and the localization only if not even they do.
// It returns false if no database defines them all, or several do.
func (c *counter) resolveReference(values map[string]int) (lang.Expected, bool) {
	labels := c.listLabels(values)
	if len(labels) == 0 {
		return lang.Expected{}, false
	}

	var candidates []string
	for database, names := range c.defined {
		if hasAll(labels, names) {
			candidates = append(candidates, database)
		}
	}

	databases := slices.DeleteFunc(slices.Clone(candidates), func(database string) bool {
		return database == c.builder.Concepts || c.isNamedFor(database, candidates)
	})
	isNested := false
	if len(databases) == 0 {
		for label, database := range c.builder.Databases {
			members := []map[string]bool{c.defined[label]}
			for _, member := range database.Nested {
				members = append(members, c.defined[member])
			}

			if len(database.Nested) > 0 && hasAll(labels, members...) {
				databases = append(databases, label)
			}
		}

		isNested = len(databases) > 0
	}

	if len(databases) == 0 && slices.Contains(candidates, c.builder.Concepts) {
		databases = append(databases, c.builder.Concepts)
	}

	if len(databases) == 0 && hasAll(labels, c.messages) {
		databases = append(databases, lang.Localization)
	}

	if len(databases) != 1 {
		return lang.Expected{}, false
	}

	return lang.Expected{Type: lang.ValueTypeReference, Database: databases[0], Nested: isNested}, true
}

// listLabels returns the names among a set of written values, each with how
// often it is written. A quoted value counts as the name inside its quotes.
// A value with a parameter in it is left out, and so is a link, which leads
// to a scope and names no definition.
func (c *counter) listLabels(values map[string]int) map[string]int {
	labels := map[string]int{}
	for value, count := range values {
		label := strings.Trim(value, `"`)
		if jomini.Classify(label) != jomini.ClassParameter && !c.builder.IsLink(label) {
			labels[label] += count
		}
	}

	return labels
}

// hasAll reports whether each value is in one of the sets.
func hasAll(values map[string]int, sets ...map[string]bool) bool {
	for value := range values {
		if !slices.ContainsFunc(sets, func(set map[string]bool) bool { return set[value] }) {
			return false
		}
	}

	return true
}

// isNamedFor reports whether a database's definitions only repeat the names
// of another of the given databases, so that the other is the one a value
// names: its row says it sets up one of them, as history/titles sets up
// landed titles, or says it is named after several others and it is not the
// only one given.
func (c *counter) isNamedFor(label string, others []string) bool {
	database := c.builder.Databases[label]

	return database != nil && (database.Declaration.Target != "" && slices.Contains(others, database.Declaration.Target) ||
		database.Declaration.Mirrors && len(others) > 1)
}

// takesValues reports whether the values the game's files give a field show
// that it takes a number a script value may stand for. At least one value
// must be the name of a script value, as in add_stability =
// stability_mild_bonus, and every other must be a number, a parameter, a
// chain of links or a command.
func (c *counter) takesValues(values map[string]int) bool {
	names := c.defined[c.builder.Values]
	hasValue := false
	for value := range values {
		text := strings.Trim(value, `"`)
		class := jomini.Classify(text)
		isCommand := c.builder.FindCommand(text) != nil
		_, _, isCall := c.builder.FindKeyCall(text, 0)
		switch {
		case names[text]:
			hasValue = true
		case class == jomini.ClassNumber || class == jomini.ClassParameter || c.builder.IsLink(text) || isCommand || isCall:
			// allowed
		default:
			return false
		}
	}

	return hasValue
}

// isLiteral reports whether a value is yes or no, a number or a date.
func (c *counter) isLiteral(value string) bool {
	return slices.ContainsFunc([]lang.ValueType{lang.ValueTypeBool, lang.ValueTypeNumber, lang.ValueTypeDate}, func(valueType lang.ValueType) bool {
		return c.builder.MatchValue(lang.Expected{Type: valueType}, value)
	})
}

// resolvePrefix finds the database of the names written after one link's
// colon: the one that defines every such name the game's files write. A link
// that leads to a scope tries the database of that scope's type first. The
// names and their counts are kept on the link, whether or not a database is
// found.
func (c *counter) resolvePrefix(prefix string, labels map[string]int) {
	database := c.builder.findScopeDatabase(prefix)
	expected, ok := lang.Expected{Type: lang.ValueTypeReference, Database: database}, database != ""
	if !ok || !hasAll(c.listLabels(labels), c.defined[database]) {
		expected, ok = c.resolveReference(labels)
	}

	arguments := makeField(c.builder.LookupLink(prefix, true), lang.SourceVanilla)
	arguments.Vanilla = lang.Vanilla{Values: labels}
	for _, count := range labels {
		arguments.Vanilla.Count += count
	}

	if ok {
		arguments.Value.Resolve(expected, lang.SourceVanilla)
	}
}

// findScopeDatabase returns the database of the names written after a link
// that may be written in any scope and leads to a scope, as title:x leads to
// a landed title. It is the database of the data type that scope has, else
// the database named as the scope's type is. It returns "" for any other
// link, and where neither gives a database.
func (b *builder) findScopeDatabase(prefix string) string {
	command := b.LookupLink(prefix, true)
	if command == nil || len(command.Scopes) > 0 || !b.IsScope(command.Target) {
		return ""
	}

	getters := b.DataTypes[lang.AnyScopeType].Functions
	for _, name := range slices.Sorted(maps.Keys(getters)) {
		database, ok := b.ResolveTypeDatabase(getters[name].Returns)
		if ok && lang.ToPhrase(lang.DropGetter(name)) == lang.ToPhrase(command.Target) {
			return database
		}
	}

	database, _ := b.ResolveTypeDatabase(command.Target)

	return database
}

// demoteRequired stops a key from being required of a mod where the game's
// own files leave it out. A key a firm source calls required stays required
// only if every definition the game ships in the database writes it.
func (c *counter) demoteRequired(root *lang.Field) {
	definitions := c.lookupTally(root).definitions
	for _, field := range root.Children {
		if field.Required.Value == lang.RequirementAlways && c.lookupTally(field).definitions != definitions {
			field.Required.Source = lang.SourceVanilla
		}
	}
}
