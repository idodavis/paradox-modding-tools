// vanilla.go is the entry detector, for the schema and the index stage alike, and vanilla's
// evidence: what the install writes under each database's entries, and the value types it gives.

package schema

import (
	"maps"
	"path"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/inventory"
	"paradox-modding-tools/services/internal/jomini"
)

// Entry is one definition a file makes. Mode is the declared mode word stripped from the written
// name, or ""; Name is "" for an entry that names nothing (an Unnamed row's, a history file).
type Entry struct {
	Database, Name, Mode string
	Statement            jomini.Statement // the statement that defines it
	Local                bool             // declared inline by a folder's Keyword in another folder's file
}

// NameOf is a registry row's database name: the row's path without common/.
func NameOf(at string) string {
	return strings.TrimPrefix(at, "common/")
}

// folder is where a path's database sits: the longest registry row at or above it (else its
// folder), and the database's name (NameOf).
func (schema *Schema) folder(rel string) (at, name string) {
	info := game.Get(schema.Game)
	at = path.Dir(rel)
	for up := rel; up != "."; up = path.Dir(up) {
		if _, listed := info.Folders[up]; listed {
			return up, NameOf(up)
		}
	}

	// the game reads a common/ folder whole: a subfolder is its highest ancestor's database
	// (decisions/dlc_decisions is decisions; common/culture holds no file, so traditions stays)
	for up := path.Dir(at); strings.HasPrefix(up, "common/"); up = path.Dir(up) {
		if schema.Databases[NameOf(up)] != nil {
			at = up
		}
	}
	return at, NameOf(at)
}

// DatabaseAt is the database of the file at rel (folder), nil when the schema has none.
func (schema *Schema) DatabaseAt(rel string) *Database {
	_, name := schema.folder(rel)
	return schema.Databases[name]
}

// FindEntries finds every entry a file defines, in source order, by the registry's rows: one per line
// of a table or sound list (parsed nil); in script by keyword, file type, or the folder row.
func (schema *Schema) FindEntries(file inventory.File, parsed *jomini.Result, found func(Entry)) {
	info, rel := game.Get(schema.Game), file.Rel
	_, own := schema.folder(rel)
	ext := strings.ToLower(path.Ext(rel))
	if parsed == nil {
		schema.tableEntries(file, own, found)
		return
	}

	// the Nested rows that create entries inside the entries of a file their Parent holds
	var rows []game.Nested
	for _, row := range info.Nested {
		if row.Database != "" && (rel == row.Parent || strings.HasPrefix(rel, row.Parent+"/")) {
			rows = append(rows, row)
		}
	}

	// a folder row's Keyword declares an entry inline in its database; one written in a file of
	// another folder is local to that file
	declares, inline := map[string]string{}, map[string]bool{}
	for at, declaring := range info.Folders {
		if declaring.Keyword != "" {
			declares[declaring.Keyword], inline[NameOf(at)] = NameOf(at), true
		}
	}

	// entry hands on one entry, then the keys each inside row puts at its path below it ("" the
	// entry's own block, * one key, ** any depth); a self row's keys are the parent's and nest again
	var entry func(database, written string, statement jomini.Statement, inside []game.Nested)
	entry = func(database, written string, statement jomini.Statement, inside []game.Nested) {
		mode, name := info.Mode(written)
		found(Entry{Database: database, Name: name, Mode: mode, Statement: statement, Local: inline[database] && database != own})
		for _, row := range inside {
			nested, again := strings.TrimPrefix(row.Parent, "common/"), []game.Nested{row}
			if row.Database != "self" {
				nested, again = nested+"."+row.Database, nil
			}

			// in the entry's own block a key in the database's Fields is a field, and so is a declared
			// command or link: nested entries are the other names
			var declared map[string]bool
			if parent := schema.Databases[database]; row.Path == "" && parent != nil {
				declared = parent.Fields
			}
			var at func(statements []jomini.Statement, steps []string)
			at = func(statements []jomini.Statement, steps []string) {
				for _, inner := range statements {
					// a word listed at the row's path and shaped as a name is an entry, as a key there is (a
					// concept's alias); a constant or a stray ; is none
					if list, isList := inner.(*jomini.ValueStmt); isList && len(steps) == 0 {
						if word, isWord := list.Value.(*jomini.Scalar); isWord && jomini.IsDefName(word.Text) {
							entry(nested, word.Text, list, again)
						}
						continue
					}
					assign, isAssign := inner.(*jomini.Assignment)
					if !isAssign {
						continue
					}
					var below []jomini.Statement
					if block := jomini.BlockOf(assign.Value); block != nil {
						below = block.Statements
					}
					_, constant := jomini.Constant(assign.Key.Text)
					command := schema.Commands[assign.Key.Text]
					switch {
					case constant, declared[collapse(assign.Key.Text)],
						row.Path == "" && (command != nil && !command.Wild || schema.Link(assign.Key.Text) != nil):
					case len(steps) == 0:
						entry(nested, assign.Key.Text, assign, again)
					case steps[0] == "**":
						at([]jomini.Statement{assign}, steps[1:])
						at(below, steps)
					default:
						if matched, _ := path.Match(steps[0], assign.Key.Text); matched {
							at(below, steps[1:])
						}
					}
				}
			}
			if block := jomini.ChildBlock(statement); block != nil {
				at(block.Statements, strings.FieldsFunc(row.Path, func(char rune) bool { return char == '/' }))
			}
		}
	}

	fileType, typed := game.FileTypes[ext]
	if !typed {
		return
	}
	database, row := schema.Databases[own], game.Folder{}
	if database != nil {
		row = database.Row
	}

	// a file type's keywords declare an entry inline in <ext>.<word>, global wherever it is written
	for _, word := range fileType.Keywords {
		declares[word] = ext[1:] + "." + word
	}

	// nameIn is the word a block writes for key, "" when it writes none
	nameIn := func(block *jomini.Block, key string) string {
		for i := 0; block != nil && i < len(block.Statements); i++ {
			if assign, isAssign := block.Statements[i].(*jomini.Assignment); isAssign && assign.Key.Text == key {
				if word, isWord := assign.Value.(*jomini.Scalar); isWord {
					return word.Text
				}
			}
		}
		return ""
	}

	// top statements are named by the file type's NameKey or are its widgets, else named by the
	// folder row; below them NameKeyOf names blocks, and a keyword declares an entry at any depth
	var walk func(statements []jomini.Statement, top bool)
	walk = func(statements []jomini.Statement, top bool) {
		for i := 0; i < len(statements); i++ {
			// a keyword before a name declares it (template x { }); where top blocks are widgets, any
			// other word before a top block only groups declarations (types x { })
			if word, isWord := statements[i].(*jomini.ValueStmt); isWord && i+1 < len(statements) {
				scalar, isScalar := word.Value.(*jomini.Scalar)
				declared, isDeclared := statements[i+1].(*jomini.Assignment)
				if isScalar && isDeclared && (declares[scalar.Text] != "" || top && fileType.WidgetKey != "") {
					i++
					if declares[scalar.Text] != "" {
						entry(declares[scalar.Text], declared.Key.Text, declared, rows)
					}
					if block := jomini.ChildBlock(declared); block != nil {
						walk(block.Statements, false)
					}
					continue
				}
			}

			statement, block := statements[i], jomini.ChildBlock(statements[i])
			key := ""
			if assign, isAssign := statement.(*jomini.Assignment); isAssign {
				key = assign.Key.Text
			}
			_, constant := jomini.Constant(key)
			_, name := info.Mode(key)
			switch {
			case constant:
			case top && block != nil && slices.Contains(row.Wrappers, key):
				walk(block.Statements, true)
				continue
			case block != nil && fileType.NameKeyOf[key] != "":
				if written := nameIn(block, fileType.NameKeyOf[key]); written != "" {
					entry(ext[1:]+"."+key, written, statement, nil)
				}
			case top && fileType.NameKey != "":
				if written := nameIn(block, fileType.NameKey); written != "" {
					entry(ext[1:]+"."+key, written, statement, rows)
				}
			case top && block != nil && fileType.WidgetKey != "":
				entry(ext[1:]+".widget", nameIn(block, fileType.WidgetKey), statement, nil)
			case !top || database == nil:
			case row.Unnamed:
				entry(database.Name, "", statement, rows)
			case row.NameKey != "":
				if written, _, _ := strings.Cut(nameIn(block, row.NameKey), "("); strings.TrimSpace(written) != "" {
					entry(database.Name, strings.TrimSpace(written), statement, rows)
				}
			case key == "", row.Events && !jomini.IsEventID(name):
			default:
				entry(database.Name, key, statement, rows)
			}
			if block != nil {
				walk(block.Statements, false)
			}
		}
	}

	// a ByFile row's file is one entry named by its stem; a history row naming no database's entries
	// (no Of) makes its file one entry that names nothing
	whole := database != nil && fileType.NameKey == "" && (row.ByFile || row.History && row.Of == "" && !row.Unnamed)
	if whole {
		name := ""
		if row.ByFile {
			name = strings.TrimSuffix(path.Base(rel), path.Ext(rel))
		}
		root := &jomini.Block{Statements: parsed.Root.Statements, Range: parsed.Root.Range, OpenBrace: -1, CloseBrace: -1}
		entry(database.Name, name, &jomini.ValueStmt{Value: root, Range: parsed.Root.Range}, rows)
	}
	walk(parsed.Root.Statements, !whole)
}

// tableEntries finds the entries of a table or sound list, each a bare word at its place in the
// text: the map table names a row by its first column (a # line is a comment), a sound list a sound
// by what follows its GUID.
func (schema *Schema) tableEntries(file inventory.File, own string, found func(Entry)) {
	info := game.Get(schema.Game)
	database := info.MapTable.Database
	cut := func(line string) string {
		name, _, _ := strings.Cut(line, ";")
		if strings.HasPrefix(name, "#") {
			return ""
		}
		return name
	}
	if file.Format == inventory.List {
		database = own
		cut = func(line string) string { _, name, _ := strings.Cut(line, " "); return name }
	}
	if file.Format != inventory.List && file.Rel != info.MapTable.File {
		return
	}
	text, err := file.Text()
	if err != nil {
		return
	}
	offset := 0
	for _, line := range strings.SplitAfter(text, "\n") {
		if written := strings.TrimSpace(cut(strings.TrimSpace(line))); written != "" {
			start := offset + strings.Index(line, written)
			span := jomini.Range{Start: start, End: start + len(written)}
			found(Entry{Database: database, Name: written, Statement: &jomini.ValueStmt{Value: &jomini.Scalar{Text: written, Range: span}, Range: span}})
		}
		offset += len(line)
	}
}

// observe records vanilla's evidence for a file's entries at their databases' positions, each key
// where Read places it (creating positions); a nested entry is observed under its own database
// only, and a bare-word entry also as a value where it stands.
func (schema *Schema) observe(entries []Entry) {
	own := map[jomini.Statement]*Database{}
	for _, entry := range entries {
		own[entry.Statement] = schema.Databases[entry.Database]
	}

	// a written word's form, and the word, kept at a position; a block is counted there
	record := func(value jomini.Value, at *Position) {
		switch written := value.(type) {
		case *jomini.Scalar:
			form := formOf(written)
			at.Evidence.Forms |= form
			// a word is cloned on every write, since a map write stores its key again: the schema never
			// pins vanilla's text
			if form&(FormNumber|FormDate) == 0 {
				if at.Evidence.Values == nil {
					at.Evidence.Values = map[string]int{}
				}
				at.Evidence.Values[strings.Clone(written.Text)]++
			}
		case *jomini.Block, *jomini.TaggedBlock:
			at.Evidence.Blocks++
			at.Evidence.Forms |= FormBlock
		}
	}

	// a declared command marks the block it is written in; In counts a key once per block; a key the
	// walker reads as nothing records nothing, and neither does its block
	counted := map[*jomini.Block]map[*Position]bool{}
	reader := Visitor{Create: true, Entry: func(statement jomini.Statement) *Database {
		if _, isAssign := statement.(*jomini.Assignment); isAssign {
			return own[statement] // an entry inside another makes no position there
		}
		return nil
	}, Visit: func(step *Reading) bool {
		list, isList := step.Statement.(*jomini.ValueStmt)
		switch {
		case own[step.Statement] != nil && (!isList || jomini.BlockOf(list.Value) != nil):
			return false
		case isList:
			record(list.Value, step.Position)
			return true
		}
		if step.Command != nil && step.Command.Kind&game.KindLink == 0 && step.BlockContext.Position != nil {
			step.BlockContext.Position.Evidence.Forms |= FormCommand
			switch step.Command.Kind {
			case game.KindTrigger:
				step.BlockContext.Position.Evidence.Forms |= FormTrigger
			case game.KindEffect:
				step.BlockContext.Position.Evidence.Forms |= FormEffect
			}
		}
		if step.Position == nil { // a key whose block reads in place (a link, a date) records nothing itself
			return step.Command != nil || step.Inner.Position != nil && step.Inner.Position == step.BlockContext.Position
		}
		if !step.Leaf {
			if counted[step.Holder] == nil {
				counted[step.Holder] = map[*Position]bool{}
			}
			if !counted[step.Holder][step.Position] {
				counted[step.Holder][step.Position] = true
				step.Position.Evidence.In++
			}
		}
		record(step.Statement.(*jomini.Assignment).Value, step.Position)
		return true
	}}

	// Build has made every entry's database; an entry written as a bare word (a table row, a sound)
	// is its name, with nothing to record
	for _, entry := range entries {
		database := schema.Databases[entry.Database]
		var block *jomini.Block
		switch statement := entry.Statement.(type) {
		case *jomini.Assignment:
			record(statement.Value, database.Entry)
			block = jomini.BlockOf(statement.Value)
		case *jomini.ValueStmt:
			if block = jomini.ChildBlock(statement); block != nil {
				record(block, database.Entry)
			}
		}
		if block != nil {
			schema.WalkBlock(reader, block, block.Statements, schema.EntryContext(database))
		}
	}
}

// typeValues gives each untyped position (in every entry tree, command's Args and scripted entry's
// parameters) the type vanilla's words there share; then each link prefix the database vanilla
// writes names of.
func (schema *Schema) typeValues(names map[string]map[string]bool, locKeys map[string]bool) {
	// the words a database's entries write at its row's Names keys name entries too (trait groups)
	for name, database := range schema.Databases {
		for _, key := range database.Row.Names {
			if position := database.Entry.Child(key); position != nil {
				if names[name] == nil {
					names[name] = map[string]bool{}
				}
				for word := range position.Evidence.Values {
					names[name][word] = true
				}
			}
		}
	}

	// the databases holding each name, so words are tested only against those holding the first
	holding := map[string][]string{}
	for database, entries := range names {
		for name := range entries {
			holding[name] = append(holding[name], database)
		}
	}

	// gui is whether a database is a file type's own (<ext>.<x>: gui.template, asset.x)
	gui := func(database string) bool {
		ext, _, cut := strings.Cut(database, ".")
		_, typed := game.FileTypes["."+ext]
		return cut && typed
	}

	// one is the single database holding every word, else ""; a GUI position takes GUI databases
	// only, others script ones, and a history database none (its names are other databases')
	one := func(words []string, inGUI bool) (found string) {
		for _, database := range holding[words[0]] {
			missing := func(word string) bool { return !names[database][word] }
			if gui(database) != inGUI || schema.Databases[database].Row.History || slices.ContainsFunc(words, missing) {
				continue
			}
			if found != "" {
				return ""
			}
			found = database
		}
		return found
	}

	// a position's plain words name one database's entries, else loc keys; a name written after a
	// link prefix is kept for that prefix, unless it is a link itself (ai_state_value:root)
	after := map[string]map[string]bool{}
	var visit func(key *Position, inGUI bool)
	visit = func(key *Position, inGUI bool) {
		var words []string
		for word := range key.Evidence.Values {
			switch {
			case len(jomini.Interpolations(word)) > 0:
			case schema.Link(strings.ToLower(word)) == nil:
				words = append(words, word)
			default:
				for _, part := range strings.Split(word, ".") {
					prefixed, isPrefixed := jomini.SplitPrefixed(part)
					if !isPrefixed || schema.Link(strings.ToLower(part)) == nil ||
						schema.Link(strings.ToLower(prefixed.Name)) != nil {
						continue
					}
					prefix := strings.ToLower(prefixed.Prefix)
					if after[prefix] == nil {
						after[prefix] = map[string]bool{}
					}
					after[prefix][prefixed.Name] = true
				}
			}
		}
		if len(words) > 0 && key.Value.Source == SourceNone {
			if database := one(words, inGUI); database != "" {
				SetFirst(&key.Value, Expected{Type: WordReference, Database: database}, SourceVanilla)
			} else if !slices.ContainsFunc(words, func(word string) bool { return !locKeys[word] }) {
				SetFirst(&key.Value, Expected{Type: WordLoc}, SourceVanilla)
			}
		}
		for _, child := range key.Children {
			visit(child, inGUI)
		}
	}
	for name, database := range schema.Databases {
		visit(database.Entry, gui(name))
	}
	for _, command := range schema.Commands {
		visit(command.Args, false)
	}
	visited := map[*Position]bool{} // an entry is under each word that calls it, and visited once
	for _, macro := range schema.ScriptedEntries {
		if parameters := macro.Parameters; parameters != nil && !visited[parameters] {
			visited[parameters] = true
			visit(parameters, false)
		}
	}

	// a link prefix names the one script database holding every name written after it, unless Build
	// has set it already
	for prefix, written := range after {
		if database := one(slices.Collect(maps.Keys(written)), false); database != "" && schema.Prefixes[prefix] == "" {
			schema.Prefixes[prefix] = database
		}
	}
}
