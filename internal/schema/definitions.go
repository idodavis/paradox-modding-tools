// definitions.go finds the definition that a statement makes during a walk. It is a constant, a name that a nesting
// defines, or what the file's format and database say is defined there.

package schema

import (
	"path"
	"slices"
	"strconv"
	"strings"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/jomini"
)

// findDefinition returns the definition that a statement makes. It is a constant, a name that a nesting defines, or
// what the file's format or database says is defined here. It returns false if the statement defines nothing.
func (walker *walker) findDefinition(statement int32, level int, pending string) (Definition, bool) {
	if label := walker.frames[level].label; strings.HasPrefix(label, "@") {
		return Definition{Database: game.Constant, Name: label, Span: walker.frames[level].span, Statement: statement, Local: true}, true
	}
	if definition, ok := walker.findNested(statement, level); ok {
		return definition, true
	}
	switch walker.file.Format {
	case files.FormatGUI:
		return walker.findGUIDefinition(statement, level, pending)
	case files.FormatAsset, files.FormatFont:
		return walker.findAssetDefinition(statement, level)
	}
	return walker.findScriptDefinition(statement, level, pending)
}

// findScriptDefinition returns the definition a statement of a script file makes. It returns false if it makes none.
func (walker *walker) findScriptDefinition(statement int32, level int, pending string) (Definition, bool) {
	label, span := walker.frames[level].label, walker.frames[level].span
	if owner := walker.schema.Databases[walker.declaring[pending]]; owner != nil && label != "" {
		walker.inline[label] = makeScripted(owner, walker.tree, statement)
		return Definition{Database: owner.Name, Name: label, Span: span, Statement: statement, Local: true}, true
	}
	database := walker.database
	if database == nil || !walker.frames[level-1].wraps {
		return Definition{}, false
	}
	switch database.Row.NamedBy {
	case "nothing", "file", "column":
		return Definition{}, false
	case "field":
		name, ok := walker.findNameSpan(statement, database.Row.NameKey)
		if !ok {
			return Definition{}, false
		}
		span, label = name, walker.tree.Text(name)
	case "call":
		name, ok := walker.findNameSpan(statement, database.Row.NameKey)
		call, parsed := jomini.ParseCall(walker.tree.Text(name), name.Start)
		if !ok || !parsed {
			return Definition{}, false
		}
		span, label = call.Steps[0].Span, call.Steps[0].Text
	}
	if label == "" || slices.Contains(database.Row.WrapperKeys, label) {
		return Definition{}, false
	}
	mode, label := walker.row.SplitMode(label)
	if database.Row.Contents == game.ContentsEvents && !walker.isEvent(label, statement) {
		return Definition{}, false
	}
	span.Start = span.End - len(label) // the name follows the mode and colon
	value, _ := walker.tree.FindChildValue(statement, database.Precedence.PriorityKey)
	priority, _ := strconv.Atoi(value)
	return Definition{
		Database: database.Name, Name: label, Span: span, Statement: statement, Mode: mode,
		Priority: priority, Local: database.Callable.Local,
	}, true
}

// findNameSpan returns the span of the value of the given key in a statement's block, without quotes. It returns false
// if the block writes no such key.
func (walker *walker) findNameSpan(statement int32, nameKey string) (jomini.Span, bool) {
	child, ok := walker.tree.FindChild(statement, nameKey)
	return walker.tree.UnquotedSpan(walker.tree.Statements[child].Value), ok
}

// isEvent reports whether the label is the name of an event in the file. It is a namespace that the file declares above
// the statement, a dot and an id.
func (walker *walker) isEvent(label string, statement int32) bool {
	return slices.ContainsFunc(walker.frames[0].context.Namespaces, func(declaring int32) bool {
		namespace := walker.tree.Unquoted(walker.tree.Statements[declaring].Value)
		return declaring < statement && strings.HasPrefix(label, namespace+".") && len(label) > len(namespace)+1
	})
}

// findGUIDefinition returns the definition a statement of a GUI file makes. It returns false if it makes none.
func (walker *walker) findGUIDefinition(statement int32, level int, pending string) (Definition, bool) {
	current := walker.tree.Statements[statement]
	label, span := walker.frames[level].label, walker.frames[level].span
	switch nameKey := game.GUI.NameKeyOf[label]; {
	case slices.Contains(game.GUI.Declares, pending) && label != "":
		return walker.makeGUIDefinition(game.GUI.DatabasePrefix+pending, label, span, statement)
	case slices.Contains(game.GUI.Declares, label) && !current.Tag.IsEmpty():
		return walker.makeGUIDefinition(game.GUI.DatabasePrefix+label, walker.tree.Text(current.Tag), current.Tag, statement)
	case nameKey != "":
		value, _ := walker.tree.FindChildValue(statement, nameKey)
		return walker.makeGUIDefinition(game.GUI.DatabasePrefix+label, value, span, statement)
	case level == 1 && !current.Key.IsEmpty() && current.IsBlock():
		return walker.makeGUIDefinition(game.GUI.WidgetDatabase, walker.findWindowLabel(statement), span, statement)
	}
	return Definition{}, false
}

// findWindowLabel returns the name of a window at the top of a GUI file, else its file's name if it is the file's
// only window. It returns "" otherwise.
func (walker *walker) findWindowLabel(statement int32) string {
	if value, _ := walker.tree.FindChildValue(statement, game.GUI.WidgetKey); value != "" {
		return value
	}
	for child := range walker.tree.Children(0) {
		current := walker.tree.Statements[child]
		if child != statement && !current.Key.IsEmpty() && current.IsBlock() {
			return ""
		}
	}
	return walker.stem
}

// makeGUIDefinition returns a definition in a GUI database. If that database's definitions are local to their file,
// the rest of the file can call the definition. It returns false if the label is empty.
func (walker *walker) makeGUIDefinition(database, label string, span jomini.Span, statement int32) (Definition, bool) {
	owner := walker.schema.Databases[database]
	local := owner != nil && owner.Callable.Local
	if local && label != "" {
		walker.inline[label] = makeScripted(owner, walker.tree, statement)
	}
	return Definition{Database: database, Name: label, Span: span, Statement: statement, Local: local}, label != ""
}

// findAssetDefinition returns the definition that a top block of an asset or font file makes. Its database is named
// for the file's extension and the block's key, and its name is the value of the key that the file type names.
func (walker *walker) findAssetDefinition(statement int32, level int) (Definition, bool) {
	current := walker.tree.Statements[statement]
	key, span := walker.frames[level].label, walker.frames[level].span
	if level != 1 || key == "" || !current.IsBlock() {
		return Definition{}, false
	}
	label := key
	if walker.fileType.NameKey != "" {
		label, _ = walker.tree.FindChildValue(statement, walker.fileType.NameKey)
	}
	database := strings.TrimPrefix(strings.ToLower(path.Ext(walker.rel)), ".") + "." + key
	return Definition{Database: database, Name: label, Span: span, Statement: statement}, label != ""
}

// findNested returns the definition a statement makes in a nested database. It returns false if it makes none.
func (walker *walker) findNested(statement int32, level int) (Definition, bool) {
	current := walker.tree.Statements[statement]
	label, span := walker.frames[level].label, walker.frames[level].span
	if label == "" && current.IsListed() {
		span = walker.tree.UnquotedSpan(current.Value)
		label = walker.tree.Text(span)
	}
	if label == "" {
		return Definition{}, false
	}
	for _, nesting := range walker.nestings {
		if !jomini.MatchKeyPath(nesting.KeyPath, walker.frames[level-1].path) {
			continue
		}
		database, same := walker.findNestedDatabase(nesting)
		holding := walker.frames[level-1].context.Definition != nil || walker.database != nil && walker.database.Row.NamedBy == "nothing"
		if database == "" || !holding && (same || path.Ext(nesting.Parent) == "") {
			continue
		}
		if nesting.KeyPath == "" && !walker.isNested(label, nesting, same) {
			continue
		}
		definition := Definition{Database: database, Name: label, Span: span, Statement: statement}
		if nesting.NameKey != "" {
			name, ok := walker.findNameSpan(statement, nesting.NameKey)
			if !ok {
				continue
			}
			definition.Name, definition.Span = walker.tree.Text(name), name
		}
		if holder := walker.frames[level-1].context.Definition; holder != nil {
			definition.Name = walker.schema.JoinNested(database, holder.Name, definition.Name)
		}
		return definition, true
	}
	return Definition{}, false
}

// findNestedDatabase returns the database that a nesting defines in. It also reports whether that is the holder's own.
func (walker *walker) findNestedDatabase(nesting game.Nesting) (string, bool) {
	if nesting.Database != "" {
		return nesting.Label(), false
	}
	if walker.database == nil {
		return "", true
	}
	return walker.database.Name, true
}

// isNested reports whether a key in a definition's block defines a nested definition rather than a field.
func (walker *walker) isNested(key string, nesting game.Nesting, same bool) bool {
	if walker.database != nil && walker.database.Documented[key] {
		return false
	}
	fits, _ := path.Match(nesting.Keys, key)
	return fits || !same && nesting.Keys == ""
}
