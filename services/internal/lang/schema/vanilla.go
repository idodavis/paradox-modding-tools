// vanilla.go is the one entry detector, for the schema and the index stage alike, and vanilla's
// evidence: what the install writes under each database's entries, recorded and never claimed.

package schema

import (
	"path"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/source"
)

// Entry is one definition a file makes. Mode is the declared mode word stripped from the written
// name, or ""; Name is "" for an entry that names nothing (an Unnamed row's, a history file).
type Entry struct {
	Database, Name, Mode string
	Statement            jomini.Statement // the statement that defines it
}

// folder is where a path's database sits: the longest registry row at or above it (else its
// folder), and the database's name: that path without common/.
func (schema *Schema) folder(rel string) (at, name string) {
	info := game.Get(schema.Game)
	at = path.Dir(rel)
	for up := rel; up != "."; up = path.Dir(up) {
		if _, listed := info.Folders[up]; listed {
			at = up
			break
		}
	}
	return at, strings.TrimPrefix(at, "common/")
}

// DatabaseAt is the database of the file at rel (folder), nil when the schema has none.
func (schema *Schema) DatabaseAt(rel string) *Database {
	_, name := schema.folder(rel)
	return schema.Databases[name]
}

// Entries finds every entry a file defines, in source order, by the registry's rows: one per line
// of a table or sound list (parsed nil); in script by keyword, file type, or the folder row.
func (schema *Schema) Entries(file source.File, parsed *jomini.Result, found func(Entry)) {
	info, rel := game.Get(schema.Game), file.Rel
	_, own := schema.folder(rel)
	ext := strings.ToLower(path.Ext(rel))

	// the Nested rows that create entries inside the entries of a file their Parent holds
	var rows []game.Nested
	for _, row := range info.Nested {
		if row.Database != "" && (rel == row.Parent || strings.HasPrefix(rel, row.Parent+"/")) {
			rows = append(rows, row)
		}
	}

	// strip splits a declared mode word off a written name: REPLACE:x is x with mode REPLACE
	strip := func(written string) (mode, name string) {
		word, rest, cut := strings.Cut(written, ":")
		declared := func(entryMode game.EntryMode) bool { return entryMode.Word == word }
		if cut && slices.ContainsFunc(info.EntryModes, declared) {
			return word, rest
		}
		return "", written
	}

	// entry hands on one entry, then the keys each inside row puts at its path below it ("" the
	// entry's own block, * one key, ** any depth); a self row's keys are the parent's and nest again
	var entry func(database, written string, statement jomini.Statement, inside []game.Nested)
	entry = func(database, written string, statement jomini.Statement, inside []game.Nested) {
		mode, name := strip(written)
		found(Entry{Database: database, Name: name, Mode: mode, Statement: statement})
		for _, row := range inside {
			nested, again := strings.TrimPrefix(row.Parent, "common/"), []game.Nested{row}
			if row.Database != "self" {
				nested, again = nested+"."+row.Database, nil
			}

			// in the entry's own block a key in the database's Fields is a field, and so is a declared
			// command or link: nested entries are new names (D-21)
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
					switch {
					case constant, declared[collapse(assign.Key.Text)],
						row.Path == "" && (schema.Commands[assign.Key.Text] != nil || schema.Link(assign.Key.Text) != nil):
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

	// the map table names a row by its first column (a # line is a comment), a sound list a sound by
	// what follows its GUID; each name is a bare word at its place in the text
	if parsed == nil {
		database := info.MapTable.Database
		cut := func(line string) string {
			name, _, _ := strings.Cut(line, ";")
			if strings.HasPrefix(name, "#") {
				return ""
			}
			return name
		}
		if file.Format == source.List {
			database = own
			cut = func(line string) string { _, name, _ := strings.Cut(line, " "); return name }
		}
		if file.Format != source.List && rel != info.MapTable.File {
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
				entry(database, written, &jomini.ValueStmt{Value: &jomini.Scalar{Text: written, Range: span}, Range: span}, nil)
			}
			offset += len(line)
		}
		return
	}

	fileType, typed := info.FileTypes[ext]
	if !typed {
		return
	}
	database, row := schema.Databases[own], game.Folder{}
	if database != nil {
		row = database.Naming
	}

	// keywords declare an entry inline: a file type's in <ext>.<word>, a folder row's in its database
	declares := map[string]string{}
	for _, word := range fileType.Keywords {
		declares[word] = ext[1:] + "." + word
	}
	for at, declaring := range info.Folders {
		if declaring.Keyword != "" {
			_, declares[declaring.Keyword] = schema.folder(at)
		}
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

	// top statements are named by the file type's NameKey, else by the folder row; below them the
	// file type's NameKeyOf and WidgetKey name blocks, and a keyword declares an entry at any depth
	var walk func(statements []jomini.Statement, top bool)
	walk = func(statements []jomini.Statement, top bool) {
		for i := 0; i < len(statements); i++ {
			if word, isWord := statements[i].(*jomini.ValueStmt); isWord && i+1 < len(statements) {
				scalar, isScalar := word.Value.(*jomini.Scalar)
				declared, isDeclared := statements[i+1].(*jomini.Assignment)
				if isScalar && isDeclared && declares[scalar.Text] != "" {
					i++
					entry(declares[scalar.Text], declared.Key.Text, declared, rows)
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
			_, name := strip(key)
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
			case !top && block != nil && fileType.WidgetKey != "":
				if written := nameIn(block, fileType.WidgetKey); written != "" {
					entry(ext[1:]+".widget", written, statement, nil)
				}
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
	whole := database != nil && fileType.NameKey == "" && (row.ByFile || row.History && len(row.Of) == 0)
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

// observe records vanilla's evidence for a file's entries under their databases' Entry positions,
// each key where Slot reads it; a statement that is itself an entry is observed only on its own,
// and a listed word that is one only as a value of its position.
func (schema *Schema) observe(entries []Entry) {
	own := map[jomini.Statement]bool{}
	for _, entry := range entries {
		own[entry.Statement] = true
	}

	// held is the positions each open block holds, innermost last, so In counts a key once per block
	var held []*Key
	var read func(statements []jomini.Statement, at *Key, from int)
	record := func(written jomini.Value, at *Key) {
		switch value := written.(type) {
		case *jomini.Scalar:
			form := FormWord
			switch literal := jomini.Literal(value.Text); {
			case value.Quoted:
				form = FormText
			case literal == jomini.LitNumber:
				form = FormNumber
			case literal == jomini.LitDate:
				form = FormDate
			case value.Text == "yes" || value.Text == "no":
				form = FormBool
			}
			at.Seen.Forms |= form
			// a word a declared value names a database's entry with is kept too: unresolved-ref checks it
			if form&(FormNumber|FormDate) == 0 {
				if at.Seen.Values == nil {
					at.Seen.Values = map[string]int{}
				}
				at.Seen.Values[value.Text]++
			}
		case *jomini.Block, *jomini.TaggedBlock:
			at.Seen.Blocks++
			at.Seen.Forms |= FormBlock
			from := len(held)
			read(jomini.BlockOf(value).Statements, at, from)
			held = held[:from]
		}
	}
	read = func(statements []jomini.Statement, at *Key, from int) {
		for _, statement := range statements {
			// an entry is observed on its own, but a listed word that is one (an alias) stays a value of
			// the position holding it
			list, isList := statement.(*jomini.ValueStmt)
			if own[statement] && (!isList || jomini.BlockOf(list.Value) != nil) {
				continue
			}
			switch written := statement.(type) {
			case *jomini.ValueStmt:
				record(written.Value, at)
			case *jomini.Assignment:
				// a declared command (not a link) marks the block it is written in (D-29); a link's block
				// reads at its parent
				position, command := schema.Slot(at, written.Key.Text, true)
				if command != nil && command.Kind&KindLink == 0 {
					at.Seen.Forms |= FormCommand
				}
				if position == nil {
					if block := jomini.BlockOf(written.Value); block != nil {
						read(block.Statements, at, from)
					}
					continue
				}
				if !slices.Contains(held[from:], position) {
					held = append(held, position)
					position.Seen.In++
				}
				record(written.Value, position)
			}
		}
	}

	// an entry whose database is not made records nothing; one written as a bare word (a table row, a
	// sound) is its name, with nothing to record
	for _, entry := range entries {
		database := schema.Databases[entry.Database]
		if database == nil {
			continue
		}
		switch statement := entry.Statement.(type) {
		case *jomini.Assignment:
			record(statement.Value, database.Entry)
		case *jomini.ValueStmt:
			if block := jomini.ChildBlock(statement); block != nil {
				record(block, database.Entry)
			}
		}
	}
}
