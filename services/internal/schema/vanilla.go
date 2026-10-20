// vanilla.go is steps 5 and 6: what vanilla writes at each position, counted; then the kind a position's
// scalars share where nothing declares one, and each prefix's database.

package schema

import (
	"context"
	"maps"
	"path"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/inventory"
	"paradox-modding-tools/services/internal/jomini"
)

// typeCollector finds the built-in widget types: a tag that is a type's own name (type hbox = hbox { }) is one, and so
// is a base no type is named, once every file is read.
type typeCollector struct {
	schema *Schema
	bases  map[string]bool // every tag a type is written with that is not its own name
}

// counter is one file's count of what vanilla writes.
type counter struct {
	schema  *Schema
	counted map[*jomini.Block]map[*Position]bool // the positions each block counted a key at: once per block
}

// typing holds what giving positions a type from vanilla's words needs.
type typing struct {
	schema   *Schema
	names    map[string]map[string]bool // database → the names its entries have
	holding  map[string][]string        // name → the databases holding it
	locKeys  map[string]bool            // the install's localization keys
	prefixes map[string]map[string]bool // link prefix → the names vanilla writes after it
}

// countVanilla counts what the install's files write, after registerDefinitions so calls read as calls, and returns
// the entry names vanilla defines by database; a database a new name needs is declared on the way.
func (schema *Schema) countVanilla(ctx context.Context, install []inventory.File, info *game.Game) (map[string]map[string]bool, error) {
	parses := schema.registerDefinitions(install, info)
	files := slices.DeleteFunc(slices.Clone(install), func(file inventory.File) bool {
		return !slices.Contains([]inventory.Format{inventory.Script, inventory.Table, inventory.List}, file.Format)
	})
	names := map[string]map[string]bool{}
	work := func(file inventory.File) (parsed jomini.Result) {
		if reused, isReused := parses[file.Path]; isReused {
			return reused
		}
		if file.Format == inventory.Script {
			return parseScript(file)
		}
		return parsed
	}
	keep := func(file inventory.File, result jomini.Result) {
		var parsed *jomini.Result
		if file.Format == inventory.Script {
			parsed = &result
		}
		for _, entry := range schema.countFile(file, parsed) {
			schema.noteEntry(entry, names)
		}
	}
	return names, inventory.Ordered(ctx, files, work, keep)
}

// registerDefinitions walks the scripted folders and GUI files for the scripted entries and widget types they define
// and returns their parses; a scripted entry is read only to its wrapper's children, a GUI file down its whole tree.
func (schema *Schema) registerDefinitions(install []inventory.File, info *game.Game) map[string]jomini.Result {
	schema.ScriptedEntries, schema.WidgetTypes = map[string]ScriptedEntry{}, map[string]bool{}
	types := &typeCollector{schema: schema, bases: map[string]bool{}}
	parses := map[string]jomini.Result{}
	for _, file := range install {
		if file.Format != inventory.Script {
			continue
		}
		folder, _ := schema.folder(file.Rel)
		rule, _ := info.FindRule(folder)
		gui := game.FindRuleByExtension(strings.ToLower(path.Ext(file.Rel))).DeclaringKeys != nil
		if len(rule.CallerWords) == 0 && !gui {
			continue
		}
		visit := Visit(topOnly)
		if gui {
			visit = types.note
		}
		parsed := parseScript(file)
		for _, entry := range schema.WalkFile(file, &parsed, false, visit) {
			schema.registerScriptedEntries(entry)
		}
		parses[file.Path] = parsed
	}
	types.settle()
	return parses
}

// topOnly visits so that the walk reads only into a wrapper's block, never below an entry's statement.
func topOnly(_ jomini.Statement, _ Context, inner *Context, _ []Occurrence) bool {
	return inner.top
}

// note records the tag a statement that defines an entry writes its block with; the walk calls it for every
// statement of a GUI file, and always reads on.
func (collector *typeCollector) note(statement jomini.Statement, _ Context, inner *Context, occurrences []Occurrence) bool {
	assign, isAssign := statement.(*jomini.Assignment)
	if !isAssign || !inner.Defines {
		return true
	}
	for _, occurrence := range occurrences {
		switch {
		case occurrence.Caller == "":
		case occurrence.ID == assign.Key.Text:
			collector.schema.WidgetTypes[strings.Clone(occurrence.ID)] = true
		default:
			collector.bases[strings.Clone(occurrence.ID)] = true
		}
	}
	return true
}

// parseScript parses a script file; an unreadable one parses as empty.
func parseScript(file inventory.File) jomini.Result {
	text, err := file.Text()
	if err != nil {
		return jomini.Parse("")
	}
	return jomini.Parse(text.Content)
}

// registerScriptedEntries records a scripted entry vanilla defines under each word its database is called by; one
// whose database takes no parameters reads at the database's entry, so no entry tree is held twice.
func (schema *Schema) registerScriptedEntries(entry Occurrence) {
	database := schema.Databases[entry.Database]
	if database == nil || len(database.Rule.CallerWords) == 0 {
		return
	}
	called := ScriptedEntry{Database: database.Name}
	if database.Rule.TakesParameters {
		called.Parameters = &Position{}
	}
	words := database.Rule.CallerWords
	if !strings.Contains(database.Name, ".") {
		words = append(slices.Clone(words), game.AnyCall)
	}
	for _, word := range words {
		schema.ScriptedEntries[word+":"+strings.Clone(entry.ID)] = called
	}
}

// settle adds each tag no type of the install is named, once every GUI file's entries are registered, and the types
// no file declares.
func (collector *typeCollector) settle() {
	for tag := range collector.bases {
		if _, defined := collector.schema.ScriptedEntries[game.Shapes[game.ShapeWidget].Caller+":"+tag]; !defined {
			collector.schema.WidgetTypes[tag] = true
		}
	}
	for _, row := range game.UndeclaredTypes {
		if slices.Contains(row.Games, collector.schema.Game) {
			collector.schema.WidgetTypes[row.Type] = true
		}
	}
}

// countFile walks one vanilla file and records at each position how often and in what forms vanilla writes
// it, evidence only, never a claim; it returns the entries the file defines.
func (schema *Schema) countFile(file inventory.File, parsed *jomini.Result) []Occurrence {
	counter := &counter{schema: schema, counted: map[*jomini.Block]map[*Position]bool{}}
	return schema.WalkFile(file, parsed, true, counter.count)
}

// count records one statement of the walk and returns whether to read its block.
func (counter *counter) count(statement jomini.Statement, context Context, inner *Context, _ []Occurrence) bool {
	// an entry's own statement records its value at the entry's position and counts nothing; a word listed as an
	// entry is written at its holder's position, so it is recorded there
	if inner.Defines {
		switch written := statement.(type) {
		case *jomini.Assignment:
			countValue(written.Value, inner.BlockPosition)
			countColor(written.Value, inner.BlockPosition)
		case *jomini.ValueStmt:
			if block := jomini.ChildBlock(statement); block != nil {
				countValue(block, inner.BlockPosition)
				countColor(block, inner.BlockPosition)
			} else {
				countValue(written.Value, context.BlockPosition)
			}
		}
		return true
	}
	if context.Entry < 0 {
		return true // outside every entry there is no position: read on for the entries inside
	}
	if list, isList := statement.(*jomini.ValueStmt); isList {
		countValue(list.Value, context.BlockPosition)
		return true
	}
	assign := statement.(*jomini.Assignment)
	if inner.KeyRule == game.KeyRuleCommand && context.BlockPosition != nil {
		command := counter.schema.Commands[strings.ToLower(assign.Key.Text)]
		writes := &context.BlockPosition.Vanilla.Writes
		*writes |= WritesCommand
		if isDeclaredOnly(command, game.DeclaredTrigger) {
			*writes |= WritesTrigger
		}
		if isDeclaredOnly(command, game.DeclaredEffect) {
			*writes |= WritesEffect
		}
	}
	position := inner.Position()
	if position == nil {
		// a key whose block reads in place, a link or a date, records nothing itself
		return inner.KeyRule == game.KeyRuleLink || inner.BlockPosition != nil && inner.BlockPosition == context.BlockPosition
	}
	if !inner.Leaf {
		if counter.counted[context.Block] == nil {
			counter.counted[context.Block] = map[*Position]bool{}
		}
		if !counter.counted[context.Block][position] {
			counter.counted[context.Block][position] = true
			position.Vanilla.Count++
		}
	}
	countValue(assign.Value, position)
	countColor(assign.Value, position)
	return true
}

// countValue records a written value at a position: a word's form and, unless it is a number or a date,
// the word itself, cloned so the schema never pins vanilla's text; a block is counted.
func countValue(value jomini.Value, position *Position) {
	if position == nil {
		return
	}
	switch written := value.(type) {
	case *jomini.Scalar:
		writes := writesOf(written)
		position.Vanilla.Writes |= writes
		if writes&(WritesNumber|WritesDate) == 0 {
			if position.Vanilla.Values == nil {
				position.Vanilla.Values = map[string]int{}
			}
			position.Vanilla.Values[strings.Clone(written.Text)]++
		}
	default:
		if jomini.BlockOf(value) != nil {
			position.Vanilla.Blocks++
			position.Vanilla.Writes |= WritesBlock
		}
	}
}

// countColor records whether the value at a position is a color, as the editor's swatches read it: a tagged block, or a
// word naming one; a number, a yes, a date or any other block counts against it.
func countColor(value jomini.Value, position *Position) {
	if position == nil {
		return
	}
	tag, parts, isNumbers := jomini.ColorParts(value)
	if _, isColor := game.ParseColor(tag, parts); isNumbers && isColor {
		if tag != "" {
			position.Vanilla.Writes |= WritesColor
		}
		return
	}
	scalar, isScalar := value.(*jomini.Scalar)
	if tagged, isTagged := value.(*jomini.TaggedBlock); isTagged && jomini.BlockOf(value) == nil {
		scalar, isScalar = &tagged.Tag, true
	}
	if isScalar && writesOf(scalar)&(WritesIdent|WritesString) != 0 {
		return
	}
	position.Vanilla.NotColors++
}

// noteEntry makes the database of an entry that has none (a table's, a file type's, gui.x) and
// records the name a named entry defines.
func (schema *Schema) noteEntry(entry Occurrence, names map[string]map[string]bool) {
	if schema.Databases[entry.Database] == nil {
		extension, _, _ := strings.Cut(entry.Database, ".")
		schema.declareDatabase(entry.Database, game.DatabaseRule{Name: entry.Database, Shape: game.FindRuleByExtension("." + extension).Shape})
	}
	if entry.ID != "" {
		if names[entry.Database] == nil {
			names[entry.Database] = map[string]bool{}
		}
		names[entry.Database][strings.Clone(entry.ID)] = true
	}
}

// inferTypesAndPrefixes gives positions the type vanilla's words share, and each link prefix the database of its names:
// the one script makes names in, else the one vanilla's words show, else the one its own name names.
func (schema *Schema) inferTypesAndPrefixes(names map[string]map[string]bool, locKeys map[string]bool) {
	schema.Prefixes = map[string]string{}
	for _, row := range game.ScopeVarCommands {
		schema.Prefixes[row.Database] = row.Database
	}
	schema.inferValueTypes(names, locKeys)
	for name := range schema.Commands {
		if schema.Link(name) != nil && schema.Prefixes[name] == "" {
			schema.Prefixes[name] = schema.ResolveNamedDatabase(name)
		}
	}
}

// inferValueTypes gives each untyped position the type vanilla's words there share, then each link prefix
// the database vanilla writes names of after it. names are the entry names vanilla defines, by database.
func (schema *Schema) inferValueTypes(names map[string]map[string]bool, locKeys map[string]bool) {
	typing := &typing{
		schema: schema, names: names, holding: map[string][]string{}, locKeys: locKeys, prefixes: map[string]map[string]bool{},
	}

	// the words a database's entries write at its rule's AliasKeys name entries too (trait groups)
	for name, database := range schema.Databases {
		for _, key := range database.Rule.AliasKeys {
			if child := database.Entry.Child(key); child != nil {
				if names[name] == nil {
					names[name] = map[string]bool{}
				}
				for word := range child.Vanilla.Values {
					names[name][word] = true
				}
			}
		}
	}
	for database, set := range names {
		for name := range set {
			typing.holding[name] = append(typing.holding[name], database)
		}
	}

	visited := map[*Position]bool{}
	for name, database := range schema.Databases {
		typing.visit(database.Entry, isFileTypeDatabase(name))
	}
	for _, command := range schema.Commands {
		typing.visit(command.Args, false)
	}
	for _, entry := range schema.ScriptedEntries {
		if entry.Parameters != nil && !visited[entry.Parameters] { // an entry is under each word that calls it
			visited[entry.Parameters] = true
			typing.visit(entry.Parameters, false)
		}
	}
	for prefix, written := range typing.prefixes {
		if database := typing.one(slices.Collect(maps.Keys(written)), false); database != "" && schema.Prefixes[prefix] == "" {
			schema.Prefixes[prefix] = database
		}
	}
}

// isFileTypeDatabase reports whether a database name is a file type's own (gui.type).
func isFileTypeDatabase(name string) bool {
	extension, _, cut := strings.Cut(name, ".")
	return cut && game.FindRuleByExtension("."+extension).Extension != ""
}

// one returns the only database that holds every one of the words, among the databases of the same kind
// (a file type's own, or not) and not a history database; "" for none or several.
func (typing *typing) one(words []string, gui bool) string {
	var holders []string
	for _, database := range typing.holding[words[0]] {
		if isFileTypeDatabase(database) != gui || typing.schema.Databases[database].Rule.Houses == "history" {
			continue
		}
		if !slices.ContainsFunc(words, func(word string) bool { return !typing.names[database][word] }) {
			holders = append(holders, database)
		}
	}
	if len(holders) != 1 {
		return ""
	}
	return holders[0]
}

// visit types a position and the ones below it from the words vanilla writes at each: a database the words
// all name, else a loc key when every word is one. The words after a link prefix are kept for the prefix.
func (typing *typing) visit(position *Position, gui bool) {
	var words []string
	for word := range position.Vanilla.Values {
		switch {
		case len(jomini.Parameters(word)) > 0:
		case typing.schema.Link(strings.ToLower(word)) == nil:
			words = append(words, word)
		default:
			typing.keepAfterPrefixes(word)
		}
	}
	if len(words) > 0 {
		if database := typing.one(words, gui); database != "" {
			position.Value.Set(Expected{Type: ValueReference, Database: database}, game.SourceVanilla)
		} else if !slices.ContainsFunc(words, func(word string) bool { return !typing.locKeys[word] }) {
			position.Value.Set(Expected{Type: ValueLoc}, game.SourceVanilla)
		}
	}
	for _, child := range position.Children {
		typing.visit(child, gui)
	}
}

// keepAfterPrefixes keeps, for each link prefix in a chain of links (c:FRA.title), the name written after
// it, unless that name is a link itself (ai_state_value:root).
func (typing *typing) keepAfterPrefixes(chain string) {
	for part := range strings.SplitSeq(chain, ".") {
		prefixed, isPrefixed := jomini.SplitPrefixed(part)
		if !isPrefixed || typing.schema.Link(strings.ToLower(part)) == nil || typing.schema.Link(strings.ToLower(prefixed.Name)) != nil {
			continue
		}
		prefix := strings.ToLower(prefixed.Prefix)
		if typing.prefixes[prefix] == nil {
			typing.prefixes[prefix] = map[string]bool{}
		}
		typing.prefixes[prefix][prefixed.Name] = true
	}
}
