// entries.go is the entry detector: it says which definitions a file makes, for the schema's own build and
// for the index.

package schema

import (
	"path"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/inventory"
	"paradox-modding-tools/services/internal/jomini"
)

// Entry is one definition a file makes. Mode is the declared mode word stripped from the written name, or
// ""; ID is "" for an entry that names nothing (an Unnamed row's, a history file).
type Entry struct {
	Database, ID, Mode string
	Statement          jomini.Statement // the statement that defines it
	Local              bool             // declared inline by a folder's Keyword in another folder's file
}

// NameOf returns a registry row's database name: the row's path without common/.
func NameOf(at string) string { return strings.TrimPrefix(at, "common/") }

// folder returns the registry row a file's database is read by and the database's name: the longest row at
// or above the path. The game reads a common/ folder whole, so a subfolder with no row is its highest
// ancestor's database (decisions/dlc_decisions is decisions); common/culture holds no file of its own, so
// culture/traditions stays.
func (s *Schema) folder(rel string) (at, name string) {
	info := game.Get(s.Game)
	for up := rel; up != "."; up = path.Dir(up) {
		if _, found := info.Folders[up]; found {
			return up, NameOf(up)
		}
	}
	at = path.Dir(rel)
	for up := path.Dir(at); strings.HasPrefix(up, "common/"); up = path.Dir(up) {
		if s.Databases[NameOf(up)] != nil {
			at = up
		}
	}
	return at, NameOf(at)
}

// DatabaseAt returns the database of the file at rel; nil when the schema has none.
func (s *Schema) DatabaseAt(rel string) *Database {
	_, name := s.folder(rel)
	return s.Databases[name]
}

// FindEntries returns every entry a file defines, in source order. A table or list file has no parse; a script
// file is read by its folder's row, its nested rows and its file type's keywords.
func (s *Schema) FindEntries(file inventory.File, parsed *jomini.Result) (entries []Entry) {
	_, own := s.folder(file.Rel)
	found := func(entry Entry) { entries = append(entries, entry) }
	if parsed == nil {
		s.tableEntries(file, own, found)
	} else {
		s.scriptEntries(file, own, parsed, found)
	}
	return entries
}

// tableEntries finds the entries of a table or list file: the map table's first column, or a list's names.
func (s *Schema) tableEntries(file inventory.File, own string, found func(Entry)) {
	info := game.Get(s.Game)
	database := info.MapTable.Database
	cut := func(line string) string { // the entry's name in a line
		if strings.HasPrefix(line, "#") {
			return ""
		}
		name, _, _ := strings.Cut(line, ";")
		return name
	}
	switch {
	case file.Format == inventory.List:
		database = own
		cut = func(line string) string {
			_, name, _ := strings.Cut(line, " ")
			return name
		}
	case file.Rel != info.MapTable.File:
		return
	}
	text, err := file.Text()
	if err != nil {
		return
	}
	offset := 0
	for line := range strings.SplitAfterSeq(text, "\n") {
		written := strings.TrimSpace(cut(strings.TrimSpace(line)))
		if written != "" {
			start := offset + strings.Index(line, written)
			span := jomini.Range{Start: start, End: start + len(written)}
			found(Entry{
				Database: database, ID: written,
				Statement: &jomini.ValueStmt{Value: &jomini.Scalar{Text: written, Range: span}, Range: span},
			})
		}
		offset += len(line)
	}
}

// entryFinder holds what reading one script file for its entries needs.
type entryFinder struct {
	s        *Schema
	info     *game.GameInfo
	found    func(Entry)
	own      string            // the file's folder's database name
	ext      string            // the file's extension, lower-case, with its dot
	database *Database         // the file's folder's database; nil for none
	row      game.Folder       // its registry row
	fileType game.FileType     // how the file's extension names entries
	rows     []game.Nested     // the nested rows that hold for the file's path
	declares map[string]string // a keyword → the database an entry it declares is in
	inline   map[string]bool   // the databases a folder's Keyword declares inline in other folders' files
}

// scriptEntries finds the entries of a parsed script file.
func (s *Schema) scriptEntries(file inventory.File, own string, parsed *jomini.Result, found func(Entry)) {
	info := game.Get(s.Game)
	ext := strings.ToLower(path.Ext(file.Rel))
	fileType, typed := game.FileTypes[ext]
	if !typed {
		return
	}
	f := &entryFinder{
		s: s, info: info, found: found, own: own, ext: ext, database: s.Databases[own], fileType: fileType,
		declares: map[string]string{}, inline: map[string]bool{},
	}
	if f.database != nil {
		f.row = f.database.Row
	}
	for _, nested := range info.Nested {
		if nested.Database != "" && (file.Rel == nested.Parent || strings.HasPrefix(file.Rel, nested.Parent+"/")) {
			f.rows = append(f.rows, nested)
		}
	}
	for at, row := range info.Folders {
		if row.Keyword != "" {
			f.declares[row.Keyword], f.inline[NameOf(at)] = NameOf(at), true
		}
	}
	for _, word := range fileType.Keywords {
		f.declares[word] = ext[1:] + "." + word // a file type's keywords declare an entry in <ext>.<word>
	}

	whole := f.database != nil && fileType.NameKey == "" &&
		(f.row.ByFile || f.row.History && f.row.Of == "" && !f.row.Unnamed)
	if whole {
		f.wholeFile(file.Rel, parsed)
	}
	f.walkScript(parsed.Root.Statements, !whole)
}

// wholeFile finds the one entry a file is whole: a history file of one entry, named by its stem when the
// row says each file is one.
func (f *entryFinder) wholeFile(rel string, parsed *jomini.Result) {
	name := ""
	if f.row.ByFile {
		name = strings.TrimSuffix(path.Base(rel), path.Ext(rel))
	}
	root := &jomini.Block{
		Statements: parsed.Root.Statements, Range: parsed.Root.Range, OpenBrace: -1, CloseBrace: -1,
	}
	f.emit(f.database.Name, name, &jomini.ValueStmt{Value: root, Range: parsed.Root.Range}, f.rows)
}

// emit reports an entry, then the entries nested inside it by the rows that hold for its statement.
func (f *entryFinder) emit(database, written string, statement jomini.Statement, inside []game.Nested) {
	mode, name := f.info.Mode(written)
	f.found(Entry{
		Database: database, ID: name, Mode: mode, Statement: statement,
		Local: f.inline[database] && database != f.own,
	})
	f.nestedEntries(database, statement, inside)
}

// nesting is a nested row as one entry reads it: the database its keys define, and the rows its entries
// nest by in turn (a self row's keys are the parent's, so they nest again).
type nesting struct {
	row      game.Nested
	database string
	again    []game.Nested
	declared map[string]bool // in the entry's own block: the key names that are fields, not entries
}

// nestedEntries finds the entries a statement holds by each of the rows.
func (f *entryFinder) nestedEntries(database string, statement jomini.Statement, rows []game.Nested) {
	for _, row := range rows {
		n := nesting{row: row, database: NameOf(row.Parent), again: []game.Nested{row}}
		if row.Database != "self" {
			n.database += "." + row.Database
			n.again = nil
		}
		if holder := f.s.Databases[database]; row.Path == "" && holder != nil {
			n.declared = holder.Fields
		}
		if block := jomini.ChildBlock(statement); block != nil {
			steps := strings.FieldsFunc(row.Path, func(char rune) bool { return char == '/' })
			f.nestedAt(n, block.Statements, steps)
		}
	}
}

// nestedAt finds the entries at the end of a row's path of steps: a step is a key pattern, and ** matches
// any depth.
func (f *entryFinder) nestedAt(n nesting, statements []jomini.Statement, steps []string) {
	for _, statement := range statements {
		assign, isAssign := statement.(*jomini.Assignment)
		if !isAssign {
			// a word listed at the row's path and shaped as a name is an entry; a constant or a stray ; is none
			if listed, isList := statement.(*jomini.ValueStmt); isList && len(steps) == 0 {
				if scalar, isScalar := listed.Value.(*jomini.Scalar); isScalar && jomini.IsDefName(scalar.Text) {
					f.emit(n.database, scalar.Text, statement, n.again)
				}
			}
			continue
		}
		key := assign.Key.Text
		if f.isField(n, key) {
			continue
		}
		var below []jomini.Statement
		if block := jomini.ChildBlock(assign); block != nil {
			below = block.Statements
		}
		switch {
		case len(steps) == 0:
			f.emit(n.database, key, assign, n.again)
		case steps[0] == "**":
			f.nestedAt(n, []jomini.Statement{assign}, steps[1:])
			f.nestedAt(n, below, steps)
		default:
			if matched, _ := path.Match(steps[0], key); matched {
				f.nestedAt(n, below, steps[1:])
			}
		}
	}
}

// isField reports whether a key is no entry: a constant, a field of the entry's own block, or a declared
// command or link there.
func (f *entryFinder) isField(n nesting, key string) bool {
	if _, constant := jomini.Constant(key); constant || n.declared[positionName(key)] {
		return true
	}
	command := f.s.Commands[key]
	return n.row.Path == "" && (command != nil && !command.Wild || f.s.Link(key) != nil)
}

// nameIn returns the scalar value of the first assignment in a block whose key is key; "" for none.
func nameIn(block *jomini.Block, key string) string {
	if block == nil {
		return ""
	}
	for _, statement := range block.Statements {
		if assign, isAssign := statement.(*jomini.Assignment); isAssign && assign.Key.Text == key {
			if value, isScalar := assign.Value.(*jomini.Scalar); isScalar {
				return value.Text
			}
		}
	}
	return ""
}

// walkScript reads statements for the entries they declare or are; top says they are a file's top
// statements (or a wrapper's), where each block is an entry.
func (f *entryFinder) walkScript(statements []jomini.Statement, top bool) {
	for i := 0; i < len(statements); i++ {
		// a keyword before a name declares it: template x { }
		if word, declared := f.keywordDeclaration(statements, i, top); declared != nil {
			i++
			if database := f.declares[word]; database != "" {
				f.emit(database, declared.Key.Text, declared, f.rows)
			}
			if block := jomini.ChildBlock(declared); block != nil {
				f.walkScript(block.Statements, false)
			}
			continue
		}
		statement := statements[i]
		block, key := jomini.ChildBlock(statement), ""
		if assign, isAssign := statement.(*jomini.Assignment); isAssign {
			key = assign.Key.Text
		}
		if top && block != nil && slices.Contains(f.row.Wrappers, key) {
			f.walkScript(block.Statements, true) // a wrapper's block holds top statements
			continue
		}
		f.entryOf(statement, block, key, top)
		if block != nil {
			f.walkScript(block.Statements, false)
		}
	}
}

// keywordDeclaration returns the word and the assignment at statements[i:i+2] when a keyword declares the
// assignment: a file type's keyword anywhere, or, where top blocks are widgets, any word before a top block
// (types x { }, which only groups declarations).
func (f *entryFinder) keywordDeclaration(statements []jomini.Statement, i int, top bool) (string, *jomini.Assignment) {
	if i+1 >= len(statements) {
		return "", nil
	}
	listed, isList := statements[i].(*jomini.ValueStmt)
	declared, isAssign := statements[i+1].(*jomini.Assignment)
	if !isList || !isAssign {
		return "", nil
	}
	scalar, isScalar := listed.Value.(*jomini.Scalar)
	if !isScalar || f.declares[scalar.Text] == "" && (!top || f.fileType.WidgetKey == "") {
		return "", nil
	}
	return scalar.Text, declared
}

// entryOf reports the entry a statement is, if any, by the first rule that fits: a file type's name keys,
// then the folder's row.
func (f *entryFinder) entryOf(statement jomini.Statement, block *jomini.Block, key string, top bool) {
	if _, constant := jomini.Constant(key); constant {
		return
	}
	fileType, ext := f.fileType, f.ext[1:]
	switch {
	case block != nil && fileType.NameKeyOf[key] != "":
		if written := nameIn(block, fileType.NameKeyOf[key]); written != "" {
			f.emit(ext+"."+key, written, statement, nil)
		}
	case top && fileType.NameKey != "":
		if written := nameIn(block, fileType.NameKey); written != "" {
			f.emit(ext+"."+key, written, statement, f.rows)
		}
	case top && block != nil && fileType.WidgetKey != "":
		f.emit(ext+".widget", nameIn(block, fileType.WidgetKey), statement, nil)
	case top && f.database != nil:
		f.rowEntry(statement, block, key)
	}
}

// rowEntry reports the entry a top statement is by its folder's row: unnamed, named by a key's value, or
// named by its own key (an event id where the row says so).
func (f *entryFinder) rowEntry(statement jomini.Statement, block *jomini.Block, key string) {
	row, database := f.row, f.database.Name
	switch {
	case row.Unnamed:
		f.emit(database, "", statement, f.rows)
	case row.NameKey != "":
		written, _, _ := strings.Cut(nameIn(block, row.NameKey), "(")
		if written = strings.TrimSpace(written); written != "" {
			f.emit(database, written, statement, f.rows)
		}
	default:
		if _, name := f.info.Mode(key); key != "" && (!row.Events || jomini.IsEventID(name)) {
			f.emit(database, key, statement, f.rows)
		}
	}
}
