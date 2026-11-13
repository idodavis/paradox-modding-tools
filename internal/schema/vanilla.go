// vanilla.go counts what the game's own files write and, from the counts,
// fills in what no dump and no doc states.

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

// counter is what one worker has counted. Each worker counts into fields of
// its own, which are added together once every file is walked.
type counter struct {
	builder *builder

	// contexts is the walk of the file being counted, and holders where the
	// keys of each statement's block are counted. Both are reused from file
	// to file.
	contexts []lang.Context
	holders  []holder

	// path is the file being counted. colorSuffix is how a key of that file
	// ends when its block of plain numbers is a color, "" if it has no such
	// keys.
	path        string
	colorSuffix string

	// isGUI is whether the file being counted is a GUI file.
	isGUI bool

	// fields maps a field of the schema to the worker's own copy, which it
	// counts into.
	fields map[*lang.Field]*lang.Field

	// tallies holds the counts of a field that only decide what it takes and
	// that the schema does not keep.
	tallies map[*lang.Field]*tally

	// arguments maps a command to the field the keys of its block are
	// counted into.
	arguments map[*lang.Definition]*lang.Field

	// definitions maps a database to the field its definitions' keys are
	// counted into. defined holds the names each database's files define.
	definitions map[string]*lang.Field
	defined     map[string]map[string]bool

	// messages holds the key of every localization message, in any language.
	messages map[string]bool

	// unlisted holds the steps of data calls a text writes that no dump
	// declares for their type, each as Type.step.
	unlisted map[string]bool

	// prefixed counts, for each link that takes a name after a colon, each
	// name written after it.
	prefixed map[string]map[string]int

	// examples holds the first places each data function is called, by
	// Type.Function.
	examples map[string][]lang.Example
}

// holder says where the keys written inside one statement are counted.
type holder struct {
	// field is where the keys are counted, nil where they are no fields.
	field *lang.Field

	// isShaped is whether the keys are evidence of what the block holds, as
	// an effect among them says it holds effects.
	isShaped bool

	// keys holds the keys a definition has written so far, so a key written
	// twice counts once. It is nil for a statement that is no plain
	// definition.
	keys map[string]bool
}

// tally holds the counts that only decide what a field takes. The schema
// does not keep them.
type tally struct {
	// colors is how many of the field's blocks are colors, and definitions
	// how many definitions write the field.
	colors      int
	definitions int

	// shapes counts the keys of each kind of script in the field's blocks.
	shapes map[lang.Shape]int

	// proof is the kinds of block every scripted definition called in the
	// field's blocks may be called from.
	proof proof
}

// proof is the kinds of block a set of scripted definitions may all be
// called from. isSet is false until the first is added; shapes is then empty
// when they share no kind.
type proof struct {
	shapes []lang.Shape
	isSet  bool
}

// shapeAmbiguous marks a key that is no evidence of what its block holds,
// because it fits more than one kind of block. No block has this shape.
const shapeAmbiguous lang.Shape = "ambiguous"

// countVanilla walks every file the game ships and gives the schema what the
// counts show. Several workers parse at once, so parse must be safe to call
// concurrently. It fails only if no script file could be parsed.
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
		counter.path, counter.isGUI = file.Path, file.Format == files.FormatGUI
		counter.colorSuffix = lang.FileTypes[strings.ToLower(path.Ext(file.Rel))].ColorSuffix
		counter.count(tree, file)
	})

	for _, other := range counters[1:] {
		counters[0].add(other)
	}

	counters[0].resolveCounts()

	// the declaring files are read again, now that the fields are counted,
	// to work out what each parameter takes
	winners := b.DeclareFiles(declaring, parse)
	b.TypeParameters(winners)
	b.ResolveRelays(winners, nil)

	if len(failures) == len(listing) && len(failures) > 0 {
		return errors.Join(failures...)
	}

	return nil
}

// count walks one file and counts each of its statements. The file's own
// statement is counted only where the file is one definition.
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

// countStatement counts one statement and settles where the keys inside it
// are counted.
func (c *counter) countStatement(tree *jomini.Tree, statement int32, context *lang.Context) {
	current := tree.Statements[statement]

	// nothing is around the file's own statement
	var parent holder
	if statement > 0 {
		parent = c.holders[current.Parent]
	}

	c.countDefinition(context)
	c.countPrefixes(tree, current)
	c.countCalls(tree, statement, context)
	if value := tree.Unquoted(current.Value); c.isGUI && strings.Contains(value, "[") {
		c.countUnlisted(loc.Segments(value, 0), context.Datacontext)
	}

	if parent.isShaped {
		c.countShape(parent.field, tree, statement, context)
	}

	var next holder
	block := current.IsBlock()
	field := c.countField(parent, tree, statement, context)
	if valueField := c.findValueField(parent, field, current, context); valueField != nil {
		valueField.Vanilla.Values = addCounts(valueField.Vanilla.Values, map[string]int{tree.Text(current.Value): 1})
	}

	switch {
	// the numbers in a color's block are its parts, not values
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

	// the keys in a command's block are its arguments, and no evidence of
	// what the block holds
	case context.Category == lang.CategoryCommand && block:
		next.field = mirror(c.arguments, context.Command)
		block = false

	// the keys in the block of a keyword that has a field, such as a
	// random_list's branch, are counted into that field
	case context.Category == lang.CategoryKeyword && block && context.Field != nil:
		next.field = mirror(c.fields, context.Field)
		block = false
	}

	next.isShaped = next.field != nil && block
	c.holders[statement] = next
}

// countDefinition records the name a statement defines in its database, and
// also a second name a key gives a definition, as a trait's group does.
func (c *counter) countDefinition(context *lang.Context) {
	if !context.Defines || context.Category != lang.CategoryDefinition && !context.Definition.Alias {
		return
	}

	mirrorSet(c.defined, context.Definition.Database)[context.Definition.Name] = true
}

// countPrefixes counts the names a statement writes after a link's colon,
// as norse in culture:norse, for links whose database is not known yet.
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

// isVoted reports whether the names written after a link's colon are
// counted to find their database: the link takes a name, its database is
// not known yet, and its name is not just a word.
func (b *builder) isVoted(prefix string) bool {
	database, exists := b.LookupPrefix(prefix)

	return exists && database == "" && b.LookupLink(prefix, true).Target != dumpLiteral
}

// addCounts adds each count of the second map to the first, made if it is
// nil, and returns the first.
func addCounts[K comparable](total, added map[K]int) map[K]int {
	if len(added) > 0 && total == nil {
		total = map[K]int{}
	}

	for key, count := range added {
		total[key] += count
	}

	return total
}

// countCalls keeps the places a statement's value calls data functions, as
// [Character.GetName] does, as examples of each function.
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

// keepExample adds a place to the examples kept for one data function. Only
// the first ExampleCount by path and line are kept.
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

// countShape counts one key as evidence of what the block around it holds:
// an effect of effects, a trigger of triggers, and a call of a scripted
// definition of the blocks it may be called from.
func (c *counter) countShape(field *lang.Field, tree *jomini.Tree, statement int32, context *lang.Context) {
	if tree.Statements[statement].Key.IsEmpty() {
		return
	}

	key := c.builder.FindCallee(tree, statement)
	if context.Category == lang.CategoryCall {
		if called := c.resolveProof(key); called.isSet {
			if len(called.shapes) > 0 {
				mirror(c.tallies, field).proof.narrow(called.shapes)
			}

			return
		}
	}

	if shape := c.resolveKeyShape(key, context); shape != "" {
		counts := mirror(c.tallies, field)
		counts.shapes = addCounts(counts.shapes, map[lang.Shape]int{shape: 1})
	}
}

// resolveProof returns the kinds of block a scripted name may be called
// from: those every database that defines it allows. It is unset if no
// database defines the name.
func (c *counter) resolveProof(key string) proof {
	var called proof
	for _, database := range lang.Called {
		if c.builder.LookupScripted(nil, database, key) != nil {
			called.narrow(c.builder.Databases[database].Declaration.Proves())
		}
	}

	return called
}

// narrow keeps of the proof's kinds of block only those also given.
func (p *proof) narrow(shapes []lang.Shape) {
	if !p.isSet {
		p.shapes, p.isSet = slices.Clone(shapes), true

		return
	}

	p.shapes = slices.DeleteFunc(p.shapes, func(shape lang.Shape) bool { return !slices.Contains(shapes, shape) })
}

// resolveKeyShape returns what a key says about the block it is written in,
// or "" where it is no evidence: a link, a list's iterator, a parameter or a
// call. A key that can only be a field says modifiers for a modifier's name,
// else fields; a name that is both a modifier's and a command's is
// ambiguous.
func (c *counter) resolveKeyShape(key string, context *lang.Context) lang.Shape {
	if shape, ok := c.resolveKeywordShape(strings.ToLower(key)); ok {
		return shape
	}

	switch {
	case context.Category == lang.CategoryKeyword, context.Category == lang.CategoryParameter:
		return ""
	case context.Category == lang.CategoryCommand && context.Command != nil:
		return c.resolveCommandShape(key, context.Command)
	case !c.isUnexplained(key, context):
		return ""
	case !c.builder.DeclaresModifier(key):
		return lang.ShapeFields
	case c.builder.FindCommand(key) != nil:
		return shapeAmbiguous
	}

	return lang.ShapeModifiers
}

// resolveKeywordShape returns what a keyword, given in lower case, says
// about its block: the one kind of block it is written in, or what its
// command says. A keyword with no class says nothing, since its key is a
// common field too. It returns false if the key is no keyword that may be
// written in any block.
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

// resolveCommandShape returns what a command says about its block: triggers
// for a trigger, effects for an effect. An iterator, a link or a name that
// is both says nothing. A name that is also a modifier's, or written in a
// letter case the dumps do not use, is ambiguous.
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
// around it: the walk found nothing else it could be, took it for a field,
// or took it for a call of a name nothing defines.
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

// countField counts a statement's key as a field of the block around it and
// returns that field, or nil where the key is no field there.
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

// isColor reports whether a block is a color. A block with a color tag, as
// rgb { 255 0 0 }, is one. A block of three or four plain numbers is one
// only under a key that ends as the file's kind says a color's key does.
func (c *counter) isColor(tree *jomini.Tree, statement int32, key string) bool {
	_, ok := jomini.ToColor(tree, statement)

	return ok && (!tree.Statements[statement].Tag.IsEmpty() || c.colorSuffix != "" && strings.HasSuffix(key, c.colorSuffix))
}

// findValueField returns the field a statement's value is counted at: its
// key's field, its command's arguments, or for a value written alone in a
// block, that block's field. It returns nil for a block, a definition or a
// statement with no value.
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

// mirror returns what a map holds under a key, putting a new value there
// first if it has none.
func mirror[K comparable, V any](values map[K]*V, key K) *V {
	if values[key] == nil {
		values[key] = new(V)
	}

	return values[key]
}

// mirrorSet returns the set a map holds under a key, putting an empty set
// there first if it has none.
func mirrorSet(sets map[string]map[string]bool, key string) map[string]bool {
	if sets[key] == nil {
		sets[key] = map[string]bool{}
	}

	return sets[key]
}

// lookupTally returns what the count kept for a field, the empty tally if
// nothing.
func (c *counter) lookupTally(field *lang.Field) tally {
	if counts := c.tallies[field]; counts != nil {
		return *counts
	}

	return tally{}
}

// isFrame reports whether a statement is the block of a definition with no
// name, in a database whose definitions have none. Its keys are fields of
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

// addField adds what was counted at one field, and at every field inside
// it, to another field. The tallies come from the given counter. A key no
// source declared is marked as known from the game's files alone.
func (c *counter) addField(field, counted *lang.Field, other *counter) {
	addVanilla(&field.Vanilla, counted.Vanilla)
	mirror(c.tallies, field).add(mirror(other.tallies, counted))

	for key, child := range counted.Children {
		c.addField(field.BuildChild(key, lang.SourceVanilla), child, other)
	}
}

func addVanilla(vanilla *lang.Vanilla, counted lang.Vanilla) {
	vanilla.Count += counted.Count
	vanilla.Blocks += counted.Blocks
	vanilla.Values = addCounts(vanilla.Values, counted.Values)
}

func (t *tally) add(counted *tally) {
	t.colors += counted.colors
	t.definitions += counted.definitions
	t.shapes = addCounts(t.shapes, counted.shapes)
	if counted.proof.isSet {
		t.proof.narrow(counted.proof.shapes)
	}
}

// resolveCounts adds the count to the schema, makes a database for each
// that the files define and the schema lacks, and then decides from the
// counts what each field, star key and link takes.
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

	// in a fixed order, since two spellings of one link may each claim it
	for _, prefix := range slices.Sorted(maps.Keys(c.prefixed)) {
		c.resolvePrefix(prefix, c.prefixed[prefix])
	}

	for _, database := range c.builder.Databases {
		c.demoteRequired(database.Field)
	}

	c.builder.UnlistedCalls = c.unlisted
}

// resolvePatterns finds the star keys, as has_relation_*. A database makes
// one where the dumps list a command for each of its definitions, at least
// two, whose names differ only by the definition's name.
func (c *counter) resolvePatterns() {
	owners := map[string][]string{}
	for database, labels := range c.defined {
		for label := range labels {
			owners[label] = append(owners[label], database)
		}
	}

	type patternKey struct{ dump, pattern, database string }

	// the commands of one dump that fit each star key, by the database whose
	// names fill the star
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
		// a name defined twice, or with an entry mode, is not counted as a
		// definition
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
// star stands for. The whole name is never the star.
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

// walkFields calls visit once for every field of the schema: the fields of
// each database's definitions, then the arguments of each command.
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

// resolveClaims decides from the counts what one field takes. What another
// source states is kept, except where it says only that the value is some
// name, a block or a text: there the database the game's files show is
// taken.
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

	claim, labels := field.Value.Value, c.listLabels(vanilla.Values)
	isOpen := claim.Type == lang.ValueTypeReference && claim.Database == "" || claim.Type == lang.ValueTypeBlock ||
		claim.Type == lang.ValueTypeText

	// a source that says the value may name a database is trusted only if
	// that database defines every name the game's files write there
	if claim.Type == lang.ValueTypeTarget && claim.Database != "" && !hasAll(labels, c.defined[claim.Database]) {
		field.Value.Source = lang.SourceVanilla
	}

	// a field given numbers and script values takes a script value, and so
	// does a field a source says takes a number where the game's files give
	// it a script value
	takesValue := (vanilla.Blocks == 0 || field.Shape.Value == lang.ShapeValue) && c.takesValues(vanilla.Values)
	if field.Value.IsStated() && claim.Type == lang.ValueTypeNumber && takesValue {
		field.Value.Value.Values = true
	}

	if field.Value.IsStated() && !isOpen {
		return
	}

	// a number is the name of a definition where the same field is also
	// given names that are no numbers, as character ids are
	expected, ok := c.resolveReference(labels)
	namesNumbers := ok && slices.ContainsFunc(slices.Collect(maps.Keys(labels)), func(label string) bool {
		return jomini.Classify(label) != jomini.ClassNumber
	})
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
// counted in them show. Where some keys are math and others effects or
// triggers, the block computes a number. It returns the unknown shape where
// the keys disagree, and "" where they are evidence of nothing.
func resolveShapeClaim(counts tally) lang.Shape {
	_, hasValue := counts.shapes[lang.ShapeValue]
	var shapes []lang.Shape
	switch {
	case counts.proof.isSet:
		shapes = slices.Clone(counts.proof.shapes)
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

// resolveKeys finds the database the keys of a field's block name, as the
// keys of a culture's opinions block name cultures, where every key only
// the game's files write there is a definition of one database. Those keys
// are then counted together as one field that stands for any key.
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
		expected, ok = c.resolveReference(c.listLabels(keys))
	}

	// keys never name game concepts, localization keys, GUI definitions
	// (whose names are common words such as value) or a database with others
	// nested in it
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

// resolveReference returns what a set of names, as listLabels gives them,
// all name: the one database that defines every one of them, or a database
// that defines them with the databases nested in it. The game concepts count
// only if no other database does, and the localization only if not even
// they do. It returns false if no database or several define them all.
func (c *counter) resolveReference(labels map[string]int) (lang.Expected, bool) {
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
// A value with a parameter, and a link, which names no definition, are left
// out.
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
// of another of the candidates, so the other is the one a value names: it
// sets one of them up, as history/titles does landed titles, or it is named
// after several and is not the only candidate.
func (c *counter) isNamedFor(label string, others []string) bool {
	database := c.builder.Databases[label]

	return database != nil && (database.Declaration.Target != "" && slices.Contains(others, database.Declaration.Target) ||
		database.Declaration.Mirrors && len(others) > 1)
}

// takesValues reports whether the values the game's files give a field show
// it takes a number a script value may stand for: one value at least names
// a script value, and every other is a number, a parameter, a link or a
// command.
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

// literalTypes are the kinds of value that name nothing.
var literalTypes = []lang.ValueType{lang.ValueTypeBool, lang.ValueTypeNumber, lang.ValueTypeDate}

// isLiteral reports whether a value is yes or no, a number or a date.
func (c *counter) isLiteral(value string) bool {
	return slices.ContainsFunc(literalTypes, func(valueType lang.ValueType) bool {
		return c.builder.MatchValue(lang.Expected{Type: valueType}, value)
	})
}

// resolvePrefix finds the database of the names written after one spelling
// of a link's colon: the one that defines every such name the game's files
// write, trying first the database of the scope the link leads to. The names
// and their counts are kept on the link only for the dumps' own spelling;
// another, as a GUI text's "Age:", is mostly prose.
func (c *counter) resolvePrefix(prefix string, labels map[string]int) {
	names, database := c.listLabels(labels), c.builder.findScopeDatabase(prefix)
	expected, ok := lang.Expected{Type: lang.ValueTypeReference, Database: database}, database != ""
	if !ok || !hasAll(names, c.defined[database]) {
		expected, ok = c.resolveReference(names)
	}

	link := c.builder.LookupLink(prefix, true)
	arguments := makeField(link, lang.SourceVanilla)
	if link.Key == prefix+":" {
		arguments.Vanilla = lang.Vanilla{Values: labels}
		for _, count := range labels {
			arguments.Vanilla.Count += count
		}
	}

	if ok {
		arguments.Value.Resolve(expected, lang.SourceVanilla)
	}
}

// findScopeDatabase returns the database of the names written after a link
// that may be written in any scope and leads to one, as title:x leads to a
// landed title. It returns "" for any other link, and where no database
// fits.
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
// own files leave it out: a key a source calls required stays required only
// if every definition the game ships in the database writes it.
func (c *counter) demoteRequired(root *lang.Field) {
	definitions := c.lookupTally(root).definitions
	for _, field := range root.Children {
		if field.Required.Value == lang.RequirementAlways && c.lookupTally(field).definitions != definitions {
			field.Required.Source = lang.SourceVanilla
		}
	}
}
