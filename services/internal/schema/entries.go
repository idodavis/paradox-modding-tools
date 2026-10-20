// entries.go is the entry detector: whether a statement defines an entry, and in which database, by the game's
// database rules, the file's type and the file's own database. The walk asks it at every statement.

package schema

import (
	"path"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/inventory"
	"paradox-modding-tools/services/internal/jomini"
)

// folder returns the folder or file whose database a file belongs to, and that database: the longest rule at or above
// the path, else the file's folder raised to its highest common/ ancestor that is a database.
func (schema *Schema) folder(rel string) (at, name string) {
	info := game.Lookup(schema.Game)
	for up := rel; up != "."; up = path.Dir(up) {
		if rule, found := info.FindRule(up); found {
			return up, rule.Name
		}
	}
	at = path.Dir(rel)
	for up := path.Dir(at); strings.HasPrefix(up, "common/"); up = path.Dir(up) {
		if schema.Databases[game.DeriveDatabaseName(up)] != nil {
			at = up
		}
	}
	return at, game.DeriveDatabaseName(at)
}

// DatabaseAt returns the database of the file at rel; nil when the schema has none.
func (schema *Schema) DatabaseAt(rel string) *Database {
	_, name := schema.folder(rel)
	return schema.Databases[name]
}

// tableEntries returns the entries of a table or list file, each named in the file's database: a table's rows by their
// first column where the rule says so, a list's words. Any other table defines nothing.
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
	} else if rule, found := game.Lookup(walk.schema.Game).FindRule(file.Rel); !found || rule.NamedBy != "column" {
		return nil
	}
	text, err := file.Text()
	if err != nil {
		return nil
	}
	offset := 0
	for line := range strings.SplitAfterSeq(text.Content, "\n") {
		if name := strings.TrimSpace(cut(strings.TrimSpace(line))); name != "" {
			start := offset + strings.Index(line, name)
			walk.entries = append(walk.entries, walk.entry(walk.own, name, jomini.Range{Start: start, End: start + len(name)}))
		}
		offset += len(line)
	}
	return walk.entries
}

// wholeFileEntry returns the root's statements as one block where the file is one entry: its database names each
// file one entry, or holds the start state of its own files. False for a file of many entries.
func (walk *fileWalk) wholeFileEntry(parsed *jomini.Result) (jomini.Statement, bool) {
	if walk.database == nil || walk.fileRule.NameKey != "" {
		return nil, false
	}
	rule := walk.database.Rule
	ownFiles := rule.Houses == "history" && rule.Target == "" && rule.NamedBy != "nothing"
	if rule.NamedBy != "file" && !ownFiles {
		return nil, false
	}
	root := &jomini.Block{Statements: parsed.Root.Statements, Range: parsed.Root.Range, OpenBrace: -1, CloseBrace: -1}
	return &jomini.ValueStmt{Value: root, Range: parsed.Root.Range}, true
}

// entryAt returns the entry the statement at i defines, and whether it nests in the entry holding it: at the top by the
// file type's name keys or the file's database rule, inside an entry by a nested rule whose key path matches.
func (walk *fileWalk) entryAt(context Context, statements []jomini.Statement, i int) (entry Occurrence, nested, isEntry bool) {
	statement := statements[i]
	assign, isAssign := statement.(*jomini.Assignment)
	block, key, span := jomini.ChildBlock(statement), "", statement.NodeRange()
	if isAssign {
		key, span = assign.Key.Text, assign.Key.Range
	}
	if statement == walk.whole {
		name := ""
		if walk.database.Rule.NamedBy == "file" {
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
	// a keyword written with an equals sign before the name it declares (Vic3's template = name { })
	if isAssign && walk.declares[key] != "" {
		if named, isNamed := assign.Value.(*jomini.TaggedBlock); isNamed {
			return walk.entry(walk.declares[key], named.Tag.Text, named.Tag.Range), false, true
		}
	}
	if _, constant := jomini.Constant(key); constant {
		return entry, false, false
	}
	fileRule, ext := walk.fileRule, strings.TrimPrefix(walk.ext, ".")
	switch {
	case context.top && block != nil && walk.database != nil && slices.Contains(walk.database.Rule.WrapperKeys, key):
		return entry, false, false
	case block != nil && fileRule.NameKeyOf[key] != "":
		name := nameIn(block, fileRule.NameKeyOf[key])
		return walk.entry(ext+"."+key, name, span), false, name != ""
	case context.top && fileRule.NameKey != "":
		name := nameIn(block, fileRule.NameKey)
		return walk.entry(ext+"."+key, name, span), false, name != ""
	case context.top && block != nil && fileRule.WidgetKey != "":
		return walk.entry(ext+".widget", nameIn(block, fileRule.WidgetKey), span), false, true
	case context.top && walk.database != nil:
		return walk.identified(key, block, span, statements[:i])
	case context.Entry >= 0 && context.Database != nil:
		return walk.nestedEntry(context, statement, span)
	}
	return entry, false, false
}

// declaring reports whether a word declares the statement after it, and the word: a declaring key of the file type or
// of a database (template x { }, scripted_effect x = { }), or any word before a block where the top blocks are widgets.
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
	return scalar.Text, walk.declares[scalar.Text] != "" || context.top && walk.fileRule.WidgetKey != ""
}

// identified returns the entry a top statement is, by its database's rule (NamedBy): none, the value of its name key up
// to a (, or its own key (an event id whose namespace a statement before it declares).
func (walk *fileWalk) identified(key string, block *jomini.Block, span jomini.Range, before []jomini.Statement) (entry Occurrence, nested, isEntry bool) {
	rule := walk.database.Rule
	switch rule.NamedBy {
	case "nothing":
		return walk.entry(walk.own, "", span), false, true
	case "field":
		name, _, _ := strings.Cut(nameIn(block, rule.NameKey), "(")
		name = strings.TrimSpace(name)
		return walk.entry(walk.own, name, span), false, name != ""
	}
	_, name := game.Lookup(walk.schema.Game).SplitMode(key)
	return walk.entry(walk.own, key, span), false,
		key != "" && (rule.NamespaceKey == "" || declaresNamespace(before, rule.NamespaceKey, name))
}

// declaresNamespace reports whether a statement before an id declares its namespace (namespace = x before x.N, N a
// number): Vic3's 1848.1 is an event of its file's namespace = 1848, though its shape alone reads as a number.
func declaresNamespace(before []jomini.Statement, namespaceKey, id string) bool {
	namespace, number, _ := strings.Cut(id, ".")
	return jomini.IsDigits(number) && slices.ContainsFunc(before, func(statement jomini.Statement) bool {
		assign, isAssign := statement.(*jomini.Assignment)
		scalar, isScalar := jomini.ValueOf(statement).(*jomini.Scalar)
		return isAssign && isScalar && assign.Key.Text == namespaceKey && scalar.Text == namespace
	})
}

// nestedEntry returns the entry a statement inside an entry is, by the first nested rule whose key path matches. It
// nests unless its database is the holding entry's own, where the key path starts again (a title inside a title).
func (walk *fileWalk) nestedEntry(context Context, statement jomini.Statement, span jomini.Range) (entry Occurrence, nested, isEntry bool) {
	for _, rule := range walk.nestedRules {
		if !matchesKeyPath(rule.KeyPath, context.keyPath) {
			continue
		}
		nested = rule.Name != context.Database.Name
		switch written := statement.(type) {
		case *jomini.Assignment:
			if !walk.isField(written.Key.Text, rule, context.Database) {
				return walk.entry(rule.Name, written.Key.Text, span), nested, true
			}
		case *jomini.ValueStmt:
			if scalar, isScalar := written.Value.(*jomini.Scalar); isScalar && jomini.IsIdentifier(scalar.Text) {
				return walk.entry(rule.Name, scalar.Text, span), nested, true
			}
		}
	}
	return entry, false, false
}

// matchesKeyPath reports whether a key path (keys joined by /) matches a rule's KeyPath: a pattern per key,
// and a leading ** any number of keys.
func matchesKeyPath(pattern, keys string) bool {
	if rest, deep := strings.CutPrefix(pattern, "**/"); deep {
		for at := 0; at < len(keys); at++ {
			if (at == 0 || keys[at-1] == '/') && matchesKeyPath(rest, keys[at:]) {
				return true
			}
		}
		return false
	}
	matched, _ := path.Match(pattern, keys)
	return matched
}

// isField reports whether a key inside an entry is a field of it, no entry: a constant; in the entry's own
// block, a key the docs give the database, or a declared command (not a wild one) or link.
func (walk *fileWalk) isField(key string, rule game.DatabaseRule, database *Database) bool {
	if _, constant := jomini.Constant(key); constant || rule.KeyPath == "" && database.Fields[positionName(key)] {
		return true
	}
	command := walk.schema.Commands[key]
	return rule.KeyPath == "" && (command != nil && !command.Wild || walk.schema.Link(key) != nil)
}

// entry returns a definition in a database by its written name, with the mode word split off; local where a key
// declares it in another database's file or its rule says its entries answer only in their file.
func (walk *fileWalk) entry(database, written string, span jomini.Range) Occurrence {
	mode, name := game.Lookup(walk.schema.Game).SplitMode(written)
	local := walk.inline[database] && database != walk.own
	if declared := walk.schema.Databases[database]; declared != nil {
		local = local || declared.Rule.Local
	}
	return Occurrence{
		Role: Defines, Database: database, ID: name, Mode: mode, Range: span, Entry: int32(len(walk.entries)),
		Local: local,
	}
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
