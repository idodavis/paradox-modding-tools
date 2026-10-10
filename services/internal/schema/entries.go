// entries.go is the entry detector: whether a statement defines an entry, and in which database, by the game's
// layout, the file's type and the file's own database. The walk asks it at every statement.

package schema

import (
	"path"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/inventory"
	"paradox-modding-tools/services/internal/jomini"
)

// folder is the folder or file whose database a file belongs to, and that database: the longest layout row at or
// above the path; else the file's folder, raised to the highest common/ ancestor that is a database (the game
// reads a common/ folder whole, so decisions/dlc_decisions is decisions; common/culture holds no file of its own,
// so culture/traditions stays).
func (schema *Schema) folder(rel string) (at, name string) {
	layout := game.Get(schema.Game).Layout
	for up := rel; up != "."; up = path.Dir(up) {
		if row, found := layout.At(up); found {
			return up, row.Database
		}
	}
	at = path.Dir(rel)
	for up := path.Dir(at); strings.HasPrefix(up, "common/"); up = path.Dir(up) {
		if schema.Databases[game.NameOf(up)] != nil {
			at = up
		}
	}
	return at, game.NameOf(at)
}

// DatabaseAt is the database of the file at rel; nil when the schema has none.
func (schema *Schema) DatabaseAt(rel string) *Database {
	_, name := schema.folder(rel)
	return schema.Databases[name]
}

// tableEntries are the entries of a table or list file, each named in the file's database: a table's rows by
// their first column where the layout names them so, a list's words. Any other table defines nothing.
func (walk *fileWalk) tableEntries(file inventory.File) []Occurrence {
	cut := func(line string) string { // the entry's name in a line
		if strings.HasPrefix(line, "#") {
			return ""
		}
		name, _, _ := strings.Cut(line, ";")
		return name
	}
	if file.Format == inventory.List {
		cut = func(line string) string {
			_, name, _ := strings.Cut(line, " ")
			return name
		}
	} else if row, found := game.Get(walk.schema.Game).At(file.Rel); !found || row.Identity != game.IdentityByColumn {
		return nil
	}
	text, err := file.Text()
	if err != nil {
		return nil
	}
	offset := 0
	for line := range strings.SplitAfterSeq(text, "\n") {
		if name := strings.TrimSpace(cut(strings.TrimSpace(line))); name != "" {
			start := offset + strings.Index(line, name)
			walk.entries = append(walk.entries, walk.entry(walk.own, name, jomini.Range{Start: start, End: start + len(name)}))
		}
		offset += len(line)
	}
	return walk.entries
}

// wholeFileEntry is the statement a file is whole as, holding the root's statements as one block: where the
// file's database names each file one entry, or holds the start state of its own files, and the file type names
// no entries of its own. False for a file of many entries.
func (walk *fileWalk) wholeFileEntry(parsed *jomini.Result) (jomini.Statement, bool) {
	if walk.database == nil || walk.fileType.NameKey != "" {
		return nil, false
	}
	row := walk.database.Descriptor
	ownFiles := row.Purpose == game.PurposeHistory && row.Of == "" && row.Identity != game.IdentityNone
	if row.Identity != game.IdentityByFile && !ownFiles {
		return nil, false
	}
	root := &jomini.Block{Statements: parsed.Root.Statements, Range: parsed.Root.Range, OpenBrace: -1, CloseBrace: -1}
	return &jomini.ValueStmt{Value: root, Range: parsed.Root.Range}, true
}

// entryAt is whether the statement at i defines an entry, the entry, and whether it nests in the entry holding it
// (its block then continues the key path); the first rule that fits decides. A word that declares the next
// statement (type x = { }) or groups widgets (types x { }) is no entry; what it declares is one of its database.
// At the top (the root's or a wrapper's statements) the file type's name keys name an entry of the type's own
// database, else the file's database names the key by its identity; inside an entry, a nested descriptor whose
// key path matches names a key that is no field, or a listed word shaped as a name.
func (walk *fileWalk) entryAt(context Context, statements []jomini.Statement, i int) (entry Occurrence, nested, isEntry bool) {
	statement := statements[i]
	assign, isAssign := statement.(*jomini.Assignment)
	block, key, span := jomini.ChildBlock(statement), "", statement.NodeRange()
	if isAssign {
		key, span = assign.Key.Text, assign.Key.Range
	}
	if statement == walk.whole {
		name := ""
		if walk.database.Descriptor.Identity == game.IdentityByFile {
			name = strings.TrimSuffix(path.Base(walk.rel), path.Ext(walk.rel))
		}
		return walk.entry(walk.own, name, span), false, true
	}
	if !isAssign && i+1 < len(statements) {
		if _, declares := walk.declaring(context, statement, statements[i+1]); declares {
			return entry, false, false
		}
	}
	if isAssign && i > 0 {
		if word, declares := walk.declaring(context, statements[i-1], statement); declares {
			return walk.entry(walk.declares[word], key, span), false, walk.declares[word] != ""
		}
	}
	if _, constant := jomini.Constant(key); constant {
		return entry, false, false
	}
	fileType, ext := walk.fileType, strings.TrimPrefix(walk.ext, ".")
	switch {
	case context.top && block != nil && walk.database != nil && slices.Contains(walk.database.Descriptor.Wrappers, key):
		return entry, false, false
	case block != nil && fileType.NameKeyOf[key] != "":
		name := nameIn(block, fileType.NameKeyOf[key])
		return walk.entry(ext+"."+key, name, span), false, name != ""
	case context.top && fileType.NameKey != "":
		name := nameIn(block, fileType.NameKey)
		return walk.entry(ext+"."+key, name, span), false, name != ""
	case context.top && block != nil && fileType.WidgetKey != "":
		return walk.entry(ext+".widget", nameIn(block, fileType.WidgetKey), span), false, true
	case context.top && walk.database != nil:
		return walk.identified(key, block, span)
	case context.Entry >= 0 && context.Database != nil:
		return walk.nestedEntry(context, statement, span)
	}
	return entry, false, false
}

// declaring reports whether a word declares the statement after it, and the word: a keyword of the file type or
// of a database (template x { }, scripted_effect x = { }), or, where the top blocks are widgets, any word before
// a block (types x { } groups declarations).
func (walk *fileWalk) declaring(context Context, word, next jomini.Statement) (string, bool) {
	listed, isList := word.(*jomini.ValueStmt)
	_, isAssign := next.(*jomini.Assignment)
	if !isList || !isAssign {
		return "", false
	}
	scalar, isScalar := listed.Value.(*jomini.Scalar)
	if !isScalar {
		return "", false
	}
	return scalar.Text, walk.declares[scalar.Text] != "" || context.top && walk.fileType.WidgetKey != ""
}

// identified is the entry a top statement is by the file's database's identity: named nothing, by the value of
// its name key up to a (, or by its own key (an event id where the database is the events').
func (walk *fileWalk) identified(key string, block *jomini.Block, span jomini.Range) (entry Occurrence, nested, isEntry bool) {
	row := walk.database.Descriptor
	switch row.Identity {
	case game.IdentityNone:
		return walk.entry(walk.own, "", span), false, true
	case game.IdentityByField:
		name, _, _ := strings.Cut(nameIn(block, row.NameKey), "(")
		name = strings.TrimSpace(name)
		return walk.entry(walk.own, name, span), false, name != ""
	}
	_, name := game.Get(walk.schema.Game).Mode(key)
	isEvent := row.Purpose != game.PurposeEvents || jomini.IsEventID(name)
	return walk.entry(walk.own, key, span), false, key != "" && isEvent
}

// nestedEntry is the entry a statement inside an entry is, by the first nested descriptor whose key path from
// the holding entry matches: a key that is no field, or a listed word shaped as a name. It nests unless its
// database is the holding entry's own, where the key path starts again (a title inside a title).
func (walk *fileWalk) nestedEntry(context Context, statement jomini.Statement, span jomini.Range) (entry Occurrence, nested, isEntry bool) {
	for _, row := range walk.nested {
		if !matchesWithin(row.Within, context.within) {
			continue
		}
		nested = row.Database != context.Database.Name
		switch written := statement.(type) {
		case *jomini.Assignment:
			if !walk.isField(written.Key.Text, row, context.Database) {
				return walk.entry(row.Database, written.Key.Text, span), nested, true
			}
		case *jomini.ValueStmt:
			if scalar, isScalar := written.Value.(*jomini.Scalar); isScalar && jomini.IsDefName(scalar.Text) {
				return walk.entry(row.Database, scalar.Text, span), nested, true
			}
		}
	}
	return entry, false, false
}

// matchesWithin reports whether a key path (keys joined by /) matches a descriptor's Within: a pattern per key,
// and a leading ** any number of keys.
func matchesWithin(within, keys string) bool {
	if rest, deep := strings.CutPrefix(within, "**/"); deep {
		for at := 0; at < len(keys); at++ {
			if (at == 0 || keys[at-1] == '/') && matchesWithin(rest, keys[at:]) {
				return true
			}
		}
		return false
	}
	matched, _ := path.Match(within, keys)
	return matched
}

// isField reports whether a key inside an entry is a field of it, no entry: a constant; in the entry's own
// block, a key the docs give the database, or a declared command (not a wild one) or link.
func (walk *fileWalk) isField(key string, row game.Descriptor, database *Database) bool {
	if _, constant := jomini.Constant(key); constant || row.Within == "" && database.Fields[positionName(key)] {
		return true
	}
	command := walk.schema.Commands[key]
	return row.Within == "" && (command != nil && !command.Wild || walk.schema.Link(key) != nil)
}

// entry is a definition in a database by its written name: the game's mode word split off, at a span, after the
// file's entries so far, and declared inline where a keyword declares its database in another database's file.
func (walk *fileWalk) entry(database, written string, span jomini.Range) Occurrence {
	mode, name := game.Get(walk.schema.Game).Mode(written)
	return Occurrence{
		Role: Defines, Database: database, ID: name, Mode: mode, Range: span, Entry: int32(len(walk.entries)),
		Local: walk.inline[database] && database != walk.own,
	}
}

// nameIn is the scalar value of the first assignment in a block whose key is key; "" for none.
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
