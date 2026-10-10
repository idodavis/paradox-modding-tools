// vanilla.go is steps 4 and 5: what vanilla writes at each position, counted; then the kind a position's
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
	schema.ScriptedEntries = map[string]ScriptedEntry{}
	names := map[string]map[string]bool{}
	parsedFirst := map[string]jomini.Result{}
	var files []inventory.File
	for _, file := range install {
		at, _ := schema.folder(file.Rel)
		gui := game.FileTypes[strings.ToLower(path.Ext(file.Rel))].Keywords != nil
		if file.Format == inventory.Script && (len(info.Folders[at].CalledIn) > 0 || gui) {
			parsed := parseScript(file)
			for _, entry := range schema.FindEntries(file, &parsed) {
				schema.registerScriptedEntries(entry)
			}
			parsedFirst[file.Path] = parsed
		}
		if slices.Contains([]inventory.Format{inventory.Script, inventory.Table, inventory.List}, file.Format) {
			files = append(files, file)
		}
	}
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
		entries := schema.FindEntries(file, parsed)
		for _, entry := range entries {
			schema.noteEntry(entry, names)
		}
		schema.countFile(file, parsed, entries)
	}
	return names, inventory.Ordered(ctx, files, work, keep)
}

// noteEntry makes the database of an entry that has none (a file type's database holds its Body: gui.x),
// records the name a named entry defines, and registers a scripted entry declared inline.
func (schema *Schema) noteEntry(entry Entry, names map[string]map[string]bool) {
	if schema.Databases[entry.Database] == nil {
		extension, _, _ := strings.Cut(entry.Database, ".")
		schema.declareDatabase(entry.Database, game.Folder{Body: game.FileTypes["."+extension].Body})
	}
	if entry.ID != "" {
		if names[entry.Database] == nil {
			names[entry.Database] = map[string]bool{}
		}
		names[entry.Database][strings.Clone(entry.ID)] = true
	}
	if entry.Local {
		schema.registerScriptedEntries(entry)
	}
}

// registerScriptedEntries records a scripted entry vanilla defines, under each word its database is called
// by. A database that takes parameters gives the entry its own parameters position; one that takes none
// reads at its database's entry, so no entry tree is held twice.
func (schema *Schema) registerScriptedEntries(entry Entry) {
	database := schema.Databases[entry.Database]
	if database == nil || len(database.Row.CalledIn) == 0 {
		return
	}
	called := ScriptedEntry{Database: database.Name}
	if database.Row.Parameters {
		called.Parameters = &Position{}
	}
	words := database.Row.CalledIn
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
	return jomini.Parse(text)
}

// countFile walks one vanilla file with its entries and records at each position how often and in what
// forms vanilla writes it. It records evidence only, never a claim.
func (schema *Schema) countFile(file inventory.File, parsed *jomini.Result, entries []Entry) {
	counted := map[*jomini.Block]map[*Position]bool{} // In counts a key once per block
	schema.WalkFile(file, parsed, entries, Visitor{
		Create: true,
		Visit:  func(placement *Placement) bool { return countPlacement(placement, entries, counted) },
	})
}

// countPlacement records one reading of the walk and returns whether to read its block.
func countPlacement(placement *Placement, entries []Entry, counted map[*jomini.Block]map[*Position]bool) bool {
	// an entry's own statement records its value at the entry's position and counts nothing; a word listed as an
	// entry is written at its holder's position, so it is recorded there
	if placement.Entry >= 0 && entries[placement.Entry].Statement == placement.Statement {
		switch statement := placement.Statement.(type) {
		case *jomini.Assignment:
			countValue(statement.Value, placement.Position)
		case *jomini.ValueStmt:
			if block := jomini.ChildBlock(statement); block != nil {
				countValue(block, placement.Position)
			} else {
				countValue(statement.Value, placement.BlockContext.Position)
			}
		}
		return true
	}
	if placement.Entry < 0 {
		return true // outside every entry there is no position: read on for the entries inside
	}
	if list, isList := placement.Statement.(*jomini.ValueStmt); isList {
		countValue(list.Value, placement.Position)
		return true
	}
	if command := placement.Command; command != nil && command.Kind&game.KindLink == 0 && placement.BlockContext.Position != nil {
		forms := &placement.BlockContext.Position.Vanilla.Shapes
		*forms |= ShapeCommand
		if command.Kind == game.KindTrigger {
			*forms |= ShapeTrigger
		}
		if command.Kind == game.KindEffect {
			*forms |= ShapeEffect
		}
	}
	if placement.Position == nil {
		// a key whose block reads in place, a link or a date, records nothing itself
		return placement.Command != nil || placement.Inner.Position != nil && placement.Inner.Position == placement.BlockContext.Position
	}
	if !placement.Leaf {
		if counted[placement.Holder] == nil {
			counted[placement.Holder] = map[*Position]bool{}
		}
		if !counted[placement.Holder][placement.Position] {
			counted[placement.Holder][placement.Position] = true
			placement.Position.Vanilla.In++
		}
	}
	countValue(placement.Statement.(*jomini.Assignment).Value, placement.Position)
	return true
}

// countValue records a written value at a position: a word's form and, unless it is a number or a date,
// the word itself, cloned so the schema never pins vanilla's text; a block is counted.
func countValue(value jomini.Value, at *Position) {
	if at == nil {
		return
	}
	switch written := value.(type) {
	case *jomini.Scalar:
		form := shapeOf(written)
		at.Vanilla.Shapes |= form
		if form&(ShapeNumber|ShapeDate) == 0 {
			if at.Vanilla.Values == nil {
				at.Vanilla.Values = map[string]int{}
			}
			at.Vanilla.Values[strings.Clone(written.Text)]++
		}
	case *jomini.Block, *jomini.TaggedBlock:
		at.Vanilla.Blocks++
		at.Vanilla.Shapes |= ShapeBlock
	}
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
		for _, key := range database.Row.Names {
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
		if isFileTypeDatabase(database) != inGUI || typing.s.Databases[database].Row.History {
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
		case len(jomini.Interpolations(word)) > 0:
		case typing.s.Link(strings.ToLower(word)) == nil:
			words = append(words, word)
		default:
			typing.keepAfterPrefixes(word)
		}
	}
	if len(words) > 0 {
		if database := typing.one(words, inGUI); database != "" {
			position.Value.Set(Expected{Kind: ExpectsReference, Database: database}, SourceVanilla)
		} else if !slices.ContainsFunc(words, func(word string) bool { return !typing.locKeys[word] }) {
			position.Value.Set(Expected{Kind: ExpectsLoc}, SourceVanilla)
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
