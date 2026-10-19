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

// countVanilla reads the install's files for the entries they define and what they write. Scripted entries
// come first, from the scripted folders and the GUI files alone, so the walk reads their names as calls
// rather than as arguments. It returns the entry names vanilla defines, by database; a database made for a
// new name is declared on the way.
func (schema *Schema) countVanilla(ctx context.Context, install []inventory.File, info *game.GameInfo) (map[string]map[string]bool, error) {
	schema.ScriptedEntries, schema.WidgetTypes = map[string]ScriptedEntry{}, map[string]bool{}
	types := &typeCollector{schema: schema, bases: map[string]bool{}}
	names := map[string]map[string]bool{}
	parsedFirst := map[string]jomini.Result{}
	var files []inventory.File
	for _, file := range install {
		at, _ := schema.folder(file.Rel)
		row, _ := info.At(at)
		gui := game.FileTypes[strings.ToLower(path.Ext(file.Rel))].Keywords != nil
		var visit Visit
		if gui {
			visit = types.note
		}
		if file.Format == inventory.Script && (len(row.Callers) > 0 || gui) {
			parsed := parseScript(file)
			for _, entry := range schema.WalkFile(file, &parsed, false, visit) {
				schema.registerScriptedEntries(entry)
			}
			parsedFirst[file.Path] = parsed
		}
		if slices.Contains([]inventory.Format{inventory.Script, inventory.Table, inventory.List}, file.Format) {
			files = append(files, file)
		}
	}
	types.settle()
	work := func(file inventory.File) (parsed jomini.Result) {
		if first, found := parsedFirst[file.Path]; found {
			return first
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

// typeCollector finds the built-in widget types from the tag each GUI type's block is written with: a tag that is
// the type's own name (type hbox = hbox { }) is one, and so is a base no type is named, once every file is read;
// the types no file declares are game.UndeclaredTypes.
type typeCollector struct {
	schema *Schema
	bases  map[string]bool // every tag a type is written with that is not its own name
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

// settle adds each tag no type of the install is named, once every GUI file's entries are registered, and the types
// no file declares.
func (collector *typeCollector) settle() {
	for tag := range collector.bases {
		if _, defined := collector.schema.ScriptedEntries[game.Shapes[game.ShapeWidget].Caller+":"+tag]; !defined {
			collector.schema.WidgetTypes[tag] = true
		}
	}
	for _, widgetType := range game.UndeclaredTypes {
		collector.schema.WidgetTypes[widgetType] = true
	}
}

// noteEntry makes the database of an entry that has none (a table's, a file type's with its Body: gui.x) and
// records the name a named entry defines.
func (schema *Schema) noteEntry(entry Occurrence, names map[string]map[string]bool) {
	if schema.Databases[entry.Database] == nil {
		extension, _, _ := strings.Cut(entry.Database, ".")
		schema.declareDatabase(entry.Database, game.Descriptor{Database: entry.Database, Body: game.FileTypes["."+extension].Body})
	}
	if entry.ID != "" {
		if names[entry.Database] == nil {
			names[entry.Database] = map[string]bool{}
		}
		names[entry.Database][strings.Clone(entry.ID)] = true
	}
}

// registerScriptedEntries records a scripted entry vanilla defines, under each word its database is called
// by. A database that takes parameters gives the entry its own parameters position; one that takes none
// reads at its database's entry, so no entry tree is held twice.
func (schema *Schema) registerScriptedEntries(entry Occurrence) {
	database := schema.Databases[entry.Database]
	if database == nil || len(database.Descriptor.Callers) == 0 {
		return
	}
	called := ScriptedEntry{Database: database.Name}
	if database.Descriptor.Parameters {
		called.Parameters = &Position{}
	}
	words := database.Descriptor.Callers
	if !strings.Contains(database.Name, ".") {
		words = append(slices.Clone(words), game.AnyCall)
	}
	for _, word := range words {
		schema.ScriptedEntries[word+":"+strings.Clone(entry.ID)] = called
	}
}

// parseScript parses a script file; an unreadable one parses as empty.
func parseScript(file inventory.File) jomini.Result {
	text, err := file.Text()
	if err != nil {
		return jomini.Parse("")
	}
	return jomini.Parse(text.Content)
}

// countFile walks one vanilla file and records at each position how often and in what forms vanilla writes
// it, evidence only, never a claim; it returns the entries the file defines.
func (schema *Schema) countFile(file inventory.File, parsed *jomini.Result) []Occurrence {
	counter := &counter{schema: schema, counted: map[*jomini.Block]map[*Position]bool{}}
	return schema.WalkFile(file, parsed, true, counter.count)
}

// counter is one file's count: the positions each block has counted a key at, so a key counts once per block.
type counter struct {
	schema  *Schema
	counted map[*jomini.Block]map[*Position]bool
}

// count records one statement of the walk and returns whether to read its block.
func (counter *counter) count(statement jomini.Statement, context Context, inner *Context, _ []Occurrence) bool {
	// an entry's own statement records its value at the entry's position and counts nothing; a word listed as an
	// entry is written at its holder's position, so it is recorded there
	if inner.Defines {
		switch written := statement.(type) {
		case *jomini.Assignment:
			countValue(written.Value, inner.At)
			countColor(written.Value, inner.At)
		case *jomini.ValueStmt:
			if block := jomini.ChildBlock(statement); block != nil {
				countValue(block, inner.At)
				countColor(block, inner.At)
			} else {
				countValue(written.Value, context.At)
			}
		}
		return true
	}
	if context.Entry < 0 {
		return true // outside every entry there is no position: read on for the entries inside
	}
	if list, isList := statement.(*jomini.ValueStmt); isList {
		countValue(list.Value, context.At)
		return true
	}
	assign := statement.(*jomini.Assignment)
	if inner.Rule == game.KeyRuleCommand && context.At != nil {
		command := counter.schema.Commands[strings.ToLower(assign.Key.Text)]
		writes := &context.At.Vanilla.Writes
		*writes |= WritesCommand
		if command.Kinds == game.KindTrigger {
			*writes |= WritesTrigger
		}
		if command.Kinds == game.KindEffect {
			*writes |= WritesEffect
		}
	}
	position := inner.Position()
	if position == nil {
		// a key whose block reads in place, a link or a date, records nothing itself
		return inner.Rule == game.KeyRuleLink || inner.At != nil && inner.At == context.At
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
		form := writesOf(written)
		position.Vanilla.Writes |= form
		if form&(WritesNumber|WritesDate) == 0 {
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

// countColor records whether the value of a position, an assignment's or an entry's, is a color, as the editor's
// swatches read it (Vanilla.WritesColors): a tagged color block marks its form; a word may name one (a named color,
// a list of them, another slot: a coat of arms writes color1 = "red", list "x" and color2); a number, a yes, a date or
// any other block counts against the position (a map's lighting writes sun_direction beside its colors, and numbers).
// The words listed in a block are its contents, so a color's own numbers count for nothing here. A tag written with a
// text (list "x") names a color as its tag does.
func countColor(value jomini.Value, position *Position) {
	if position == nil {
		return
	}
	tag, parts, isNumbers := jomini.ColorParts(value)
	if _, isColor := game.ColorOf(tag, parts); isNumbers && isColor {
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

// inferTypesAndPrefixes gives positions the type vanilla's words share and each link prefix the database of
// the names written after it. A prefix names the database of the names script makes (the name commands'
// first), then the one vanilla's words show, then the one its own name names.
func (schema *Schema) inferTypesAndPrefixes(names map[string]map[string]bool, locKeys map[string]bool) {
	schema.Prefixes = map[string]string{}
	for _, row := range game.NameCommands {
		schema.Prefixes[row.Database] = row.Database
	}
	schema.inferValueTypes(names, locKeys)
	for name := range schema.Commands {
		if schema.Link(name) != nil && schema.Prefixes[name] == "" {
			schema.Prefixes[name] = schema.DatabaseNamed(name)
		}
	}
}

// typing holds what giving positions a type from vanilla's words needs.
type typing struct {
	s        *Schema
	names    map[string]map[string]bool // database → the names its entries have
	holding  map[string][]string        // name → the databases holding it
	locKeys  map[string]bool
	prefixes map[string]map[string]bool // link prefix → the names vanilla writes after it
}

// inferValueTypes gives each untyped position the type vanilla's words there share, then each link prefix
// the database vanilla writes names of after it. names are the entry names vanilla defines, by database.
func (schema *Schema) inferValueTypes(names map[string]map[string]bool, locKeys map[string]bool) {
	t := &typing{s: schema, names: names, holding: map[string][]string{}, locKeys: locKeys, prefixes: map[string]map[string]bool{}}

	// the words a database's entries write at its row's Names keys name entries too (trait groups)
	for name, database := range schema.Databases {
		for _, key := range database.Descriptor.Names {
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
			t.holding[name] = append(t.holding[name], database)
		}
	}

	visited := map[*Position]bool{}
	for name, database := range schema.Databases {
		t.visit(database.Entry, isFileTypeDatabase(name))
	}
	for _, command := range schema.Commands {
		t.visit(command.Args, false)
	}
	for _, entry := range schema.ScriptedEntries {
		if entry.Parameters != nil && !visited[entry.Parameters] { // an entry is under each word that calls it
			visited[entry.Parameters] = true
			t.visit(entry.Parameters, false)
		}
	}
	for prefix, written := range t.prefixes {
		if database := t.one(slices.Collect(maps.Keys(written)), false); database != "" && schema.Prefixes[prefix] == "" {
			schema.Prefixes[prefix] = database
		}
	}
}

// isFileTypeDatabase reports whether a database name is a file type's own (gui.type).
func isFileTypeDatabase(name string) bool {
	extension, _, cut := strings.Cut(name, ".")
	_, typed := game.FileTypes["."+extension]
	return cut && typed
}

// one returns the only database that holds every one of the words, among the databases of the same kind
// (a file type's own, or not) and not a history database; "" for none or several.
func (typing *typing) one(words []string, inGUI bool) (found string) {
	count := 0
	for _, database := range typing.holding[words[0]] {
		if isFileTypeDatabase(database) != inGUI || typing.s.Databases[database].Descriptor.Purpose == game.PurposeHistory {
			continue
		}
		if slices.ContainsFunc(words, func(word string) bool { return !typing.names[database][word] }) {
			continue
		}
		found = database
		count++
	}
	if count != 1 {
		return ""
	}
	return found
}

// visit types a position and the ones below it from the words vanilla writes at each: a database the words
// all name, else a loc key when every word is one. The words after a link prefix are kept for the prefix.
func (typing *typing) visit(position *Position, inGUI bool) {
	var words []string
	for word := range position.Vanilla.Values {
		switch {
		case len(jomini.Parameters(word)) > 0:
		case typing.s.Link(strings.ToLower(word)) == nil:
			words = append(words, word)
		default:
			typing.keepAfterPrefixes(word)
		}
	}
	if len(words) > 0 {
		if database := typing.one(words, inGUI); database != "" {
			position.Value.Set(Expected{Type: ValueReference, Database: database}, SourceVanilla)
		} else if !slices.ContainsFunc(words, func(word string) bool { return !typing.locKeys[word] }) {
			position.Value.Set(Expected{Type: ValueLoc}, SourceVanilla)
		}
	}
	for _, child := range position.Children {
		typing.visit(child, inGUI)
	}
}

// keepAfterPrefixes keeps, for each link prefix in a chain of links (c:FRA.title), the name written after
// it, unless that name is a link itself (ai_state_value:root).
func (typing *typing) keepAfterPrefixes(chain string) {
	for part := range strings.SplitSeq(chain, ".") {
		prefixed, found := jomini.SplitPrefixed(part)
		if !found || typing.s.Link(strings.ToLower(part)) == nil || typing.s.Link(strings.ToLower(prefixed.Name)) != nil {
			continue
		}
		prefix := strings.ToLower(prefixed.Prefix)
		if typing.prefixes[prefix] == nil {
			typing.prefixes[prefix] = map[string]bool{}
		}
		typing.prefixes[prefix][prefixed.Name] = true
	}
}
