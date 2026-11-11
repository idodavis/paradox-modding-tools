// definitions.go finds the definition that a statement makes during a walk.

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

// findDefinition returns the definition that a statement makes: a constant,
// a name that a nesting defines, or what the file's format and database say
// is defined there. It returns false if the statement defines nothing.
func (w *walker) findDefinition(statement int32, level int) (Definition, bool) {
	if label := w.frames[level].label; strings.HasPrefix(label, "@") {
		return Definition{Database: game.Constant, Name: label, Span: w.frames[level].span, Statement: statement, Local: true}, true
	}

	if definition, ok := w.findNested(statement, level); ok {
		return definition, true
	}

	switch w.file.Format {
	case files.FormatGUI:
		return w.findGUIDefinition(statement, level)
	case files.FormatAsset, files.FormatFont:
		return w.findAssetDefinition(statement, level)
	}

	return w.findScriptDefinition(statement, level)
}

// findNested returns the definition a statement makes in a nested database:
// its key, or the value it writes alone, or the value of the nesting's name
// key, joined to its holder's name where it has a holder. It returns false
// if it makes none.
func (w *walker) findNested(statement int32, level int) (Definition, bool) {
	current := w.tree.Statements[statement]
	label, span := w.frames[level].label, w.frames[level].span
	if label == "" && current.IsListed() {
		span = w.tree.UnquotedSpan(current.Value)
		label = w.tree.Text(span)
	}

	if label == "" {
		return Definition{}, false
	}

	for _, nesting := range w.nestings {
		if !jomini.MatchKeyPath(nesting.KeyPath, w.frames[level-1].path) {
			continue
		}

		database, same := w.findNestedDatabase(nesting)
		holding := w.frames[level-1].context.Definition != nil || w.database != nil && w.database.Row.DefinedBy == game.DefinedByNothing
		if database == "" || !holding && (same || path.Ext(nesting.Parent) == "") {
			continue
		}

		if nesting.KeyPath == "" && !w.isNested(label, nesting, same) {
			continue
		}

		definition := Definition{Database: database, Name: label, Span: span, Statement: statement}
		if nesting.NameKey != "" {
			name, ok := w.findNameSpan(statement, nesting.NameKey)
			if !ok {
				continue
			}

			definition.Name, definition.Span = w.tree.Text(name), name
		}

		if holder := w.frames[level-1].context.Definition; holder != nil {
			definition.Name = w.schema.JoinNested(database, holder.Name, definition.Name)
		}

		return definition, true
	}

	return Definition{}, false
}

// findNestedDatabase returns the database that a nesting defines in, and
// whether it is the holder's own database. It returns "" and true where the
// nesting names no database and the file has none.
func (w *walker) findNestedDatabase(nesting game.Nesting) (string, bool) {
	if nesting.Database != "" {
		return nesting.Label(), false
	}

	if w.database == nil {
		return "", true
	}

	return w.database.Name, true
}

// isNested reports whether a key in a definition's block defines a nested
// definition rather than a field. A key the docs write for the database is
// a field.
func (w *walker) isNested(key string, nesting game.Nesting, same bool) bool {
	if w.database != nil && w.database.Documented[key] {
		return false
	}

	fits, _ := path.Match(nesting.Keys, key)

	return fits || !same && nesting.Keys == ""
}

// findNameSpan returns where the value of a key in a statement's block is
// written, without quotes. It returns false if the block lacks the key.
func (w *walker) findNameSpan(statement int32, nameKey string) (jomini.Span, bool) {
	child, ok := w.tree.FindChild(statement, nameKey)

	return w.tree.UnquotedSpan(w.tree.Statements[child].Value), ok
}

// findGUIDefinition returns the definition a statement of a GUI file makes:
// a name a declaring lead or key gives, the value of a GUI name key, or a
// window at the top of the file. It returns false if it makes none.
func (w *walker) findGUIDefinition(statement int32, level int) (Definition, bool) {
	current := w.tree.Statements[statement]
	label, span := w.frames[level].label, w.frames[level].span
	lead := w.findLead(statement)
	switch nameKey := game.GUI.NameKeyOf[label]; {
	case slices.Contains(game.GUI.Declares, lead) && label != "":
		return w.makeGUIDefinition(game.GUI.DatabasePrefix+lead, label, span, statement)

	case slices.Contains(game.GUI.Declares, label) && !current.Tag.IsEmpty():
		return w.makeGUIDefinition(game.GUI.DatabasePrefix+label, w.tree.Text(current.Tag), current.Tag, statement)

	case nameKey != "":
		name, _ := w.findNameSpan(statement, nameKey)

		return w.makeGUIDefinition(game.GUI.DatabasePrefix+label, w.tree.Text(name), name, statement)

	case level == 1 && !current.Key.IsEmpty() && current.IsBlock():
		return w.makeGUIDefinition(game.GUI.WidgetDatabase, w.findWindowLabel(statement), span, statement)
	}

	return Definition{}, false
}

// makeGUIDefinition returns a definition in a GUI database. Where that
// database's definitions are local to their file, the rest of the file can
// call the definition. It returns false if the label is empty.
func (w *walker) makeGUIDefinition(database, label string, span jomini.Span, statement int32) (Definition, bool) {
	owner := w.schema.Databases[database]
	local := owner != nil && owner.Callable.Local
	if local && label != "" {
		w.inline[label] = makeScripted(owner, w.tree, statement)
	}

	return Definition{Database: database, Name: label, Span: span, Statement: statement, Local: local}, label != ""
}

// findWindowLabel returns the name of a window at the top of a GUI file, or
// the file's name if it is the only window. It returns "" otherwise.
func (w *walker) findWindowLabel(statement int32) string {
	if value, _ := w.tree.FindChildValue(statement, game.GUI.WidgetKey); value != "" {
		return value
	}

	for child := range w.tree.Children(0) {
		current := w.tree.Statements[child]
		if child != statement && !current.Key.IsEmpty() && current.IsBlock() {
			return ""
		}
	}

	return w.stem
}

// findAssetDefinition returns the definition that a top block of an asset or
// font file makes. Its database is named for the file's extension and the
// block's key, and its name is the value of the key that the file type
// names. It returns false if the statement is no keyed top block or its name
// is empty.
func (w *walker) findAssetDefinition(statement int32, level int) (Definition, bool) {
	current := w.tree.Statements[statement]
	key, span := w.frames[level].label, w.frames[level].span
	if level != 1 || key == "" || !current.IsBlock() {
		return Definition{}, false
	}

	label := key
	if w.fileType.NameKey != "" {
		label, _ = w.tree.FindChildValue(statement, w.fileType.NameKey)
	}

	database := strings.TrimPrefix(strings.ToLower(path.Ext(w.rel)), ".") + "." + key

	return Definition{Database: database, Name: label, Span: span, Statement: statement}, label != ""
}

// findScriptDefinition returns the definition a statement of a script file
// makes: one a lead declares inline, or one of the file's database, named
// by its key or by the value of its name key, after any entry mode. An
// event's name opens with a namespace the file declares above it. It
// returns false if the statement makes none.
func (w *walker) findScriptDefinition(statement int32, level int) (Definition, bool) {
	label, span := w.frames[level].label, w.frames[level].span
	if owner := w.schema.Databases[w.declaring[w.findLead(statement)]]; owner != nil && label != "" {
		return Definition{Database: owner.Name, Name: label, Span: span, Statement: statement, Local: true}, true
	}

	database := w.database
	if database == nil || !w.frames[level-1].wraps {
		return Definition{}, false
	}

	switch {
	case database.Row.DefinedBy != "":
		return Definition{}, false

	case database.Row.NameKey != "":
		name, ok := w.findNameSpan(statement, database.Row.NameKey)
		if !ok {
			return Definition{}, false
		}

		// a macro's name is written before its parameters
		label, _, _ = strings.Cut(w.tree.Text(name), "(")
		span = jomini.Span{Start: name.Start, End: name.Start + len(label)}
	}

	if label == "" || slices.Contains(database.Row.WrapperKeys, label) {
		return Definition{}, false
	}

	mode, label := w.row.SplitMode(label)
	if database.Row.Contents == game.ContentsEvents && !w.isEvent(label, statement) {
		return Definition{}, false
	}

	// the name follows the mode and its colon
	span.Start = span.End - len(label)
	value, _ := w.tree.FindChildValue(statement, database.Precedence.PriorityKey)
	priority, _ := strconv.Atoi(value)

	return Definition{
		Database: database.Name, Name: label, Span: span, Statement: statement, Mode: mode,
		Priority: priority, Local: database.Callable.Local,
	}, true
}

// isEvent reports whether a label names an event: a namespace that the file
// declares above the statement, a dot and an id.
func (w *walker) isEvent(label string, statement int32) bool {
	return slices.ContainsFunc(w.frames[0].context.Namespaces, func(declaring int32) bool {
		namespace := w.tree.Unquoted(w.tree.Statements[declaring].Value)

		return declaring < statement && strings.HasPrefix(label, namespace+".") && len(label) > len(namespace)+1
	})
}
