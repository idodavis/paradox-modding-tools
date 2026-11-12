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
func (w *walker) findDefinition(statement int32, level int) (Copy, bool) {
	if label := w.frames[level].label; strings.HasPrefix(label, "@") {
		return Copy{Database: game.Constant, Name: label, Span: w.frames[level].span, Statement: statement, Local: true}, true
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
func (w *walker) findNested(statement int32, level int) (Copy, bool) {
	current := w.tree.Statements[statement]
	label, span := w.frames[level].label, w.frames[level].span
	if label == "" && current.IsListed() {
		span = w.tree.UnquotedSpan(current.Value)
		label = w.tree.Text(span)
	}

	if label == "" || w.database == nil {
		return Copy{}, false
	}

	for _, nesting := range w.database.Declaration.Nestings {
		if !jomini.MatchKeyPath(nesting.KeyPath, w.frames[level-1].path) {
			continue
		}

		database, same := nesting.Label(), nesting.Database == ""
		holding := w.frames[level-1].context.Definition != nil || w.database.Declaration.DefinedBy == game.DefinedByNothing
		if !holding && (same || path.Ext(nesting.Parent) == "") {
			continue
		}

		if nesting.KeyPath == "" && !w.isNested(label, nesting, same) {
			continue
		}

		definition := Copy{Database: database, Name: label, Span: span, Statement: statement, Sets: nesting.Sets}
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

	return Copy{}, false
}

// isNested reports whether a key in a definition's block defines a nested
// definition rather than a field. A key a row or the docs declare for the
// database is a field.
func (w *walker) isNested(key string, nesting game.Nesting, same bool) bool {
	if w.database.declares(key) {
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
func (w *walker) findGUIDefinition(statement int32, level int) (Copy, bool) {
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

	return Copy{}, false
}

// makeGUIDefinition returns a definition in a GUI database. Where that
// database's definitions are local to their file, the rest of the file can
// call the definition. It returns false if the label is empty.
func (w *walker) makeGUIDefinition(database, label string, span jomini.Span, statement int32) (Copy, bool) {
	owner := w.schema.Databases[database]
	local := owner != nil && owner.Declaration.Local
	if local && label != "" {
		w.inline[label], _ = makeScripted(owner, label, w.tree, statement)
	}

	return Copy{Database: database, Name: label, Span: span, Statement: statement, Local: local}, label != ""
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
func (w *walker) findAssetDefinition(statement int32, level int) (Copy, bool) {
	current := w.tree.Statements[statement]
	key, span := w.frames[level].label, w.frames[level].span
	if level != 1 || key == "" || !current.IsBlock() {
		return Copy{}, false
	}

	label := key
	if w.fileType.NameKey != "" {
		label, _ = w.tree.FindChildValue(statement, w.fileType.NameKey)
	}

	database := strings.TrimPrefix(strings.ToLower(path.Ext(w.rel)), ".") + "." + key

	return Copy{Database: database, Name: label, Span: span, Statement: statement}, label != ""
}

// findScriptDefinition returns the definition a statement of a script file
// makes: one a lead declares inline, or one of the file's database, named
// by its key or by the value of its name key, after any entry mode. An
// event's name opens with a namespace the file declares above it. It
// returns false if the statement makes none.
func (w *walker) findScriptDefinition(statement int32, level int) (Copy, bool) {
	label, span := w.frames[level].label, w.frames[level].span
	if owner := w.schema.Databases[w.declaring[w.findLead(statement)]]; owner != nil && label != "" {
		return Copy{Database: owner.Name, Name: label, Span: span, Statement: statement, Local: true}, true
	}

	database := w.database
	if database == nil || !w.frames[level-1].wraps {
		return Copy{}, false
	}

	switch {
	case database.Declaration.DefinedBy != "":
		return Copy{}, false

	case database.Declaration.NameKey != "":
		name, ok := w.findNameSpan(statement, database.Declaration.NameKey)
		if !ok {
			return Copy{}, false
		}

		// a macro's name is written before its parameters
		label, _, _ = strings.Cut(w.tree.Text(name), "(")
		span = jomini.Span{Start: name.Start, End: name.Start + len(label)}
	}

	if label == "" || slices.Contains(database.Declaration.WrapperKeys, label) {
		return Copy{}, false
	}

	mode, label := w.row.SplitMode(label)
	if database.Declaration.Contents == game.ContentsEvents && !w.isEvent(label, statement) {
		return Copy{}, false
	}

	// the name follows the mode and its colon
	span.Start = span.End - len(label)
	value, _ := w.tree.FindChildValue(statement, database.Precedence.PriorityKey)
	priority, _ := strconv.Atoi(value)

	return Copy{
		Database: database.Name, Name: label, Span: span, Statement: statement, Mode: mode,
		Priority: priority, Local: database.Declaration.Local,
	}, true
}

// FindTextKey returns the statement that writes the localization key a
// definition's statement names under the given key, as an event's title is
// under game.TitleKey. A key written as a block, as a triggered description
// is, gives the first text in the block. Only a value that is an identifier
// is a key, so a quoted sentence is none. It returns false if the statement
// writes no such key.
func FindTextKey(tree *jomini.Tree, statement int32, key string) (int32, bool) {
	child, ok := tree.FindChild(statement, key)
	if !ok {
		return 0, false
	}

	for position := child; position < tree.Statements[child].Last; position++ {
		current := tree.Statements[position]
		field := tree.Text(current.Key)
		isText := field == game.DescriptionKey || field == game.LabelKey && current.Parent == child
		if (position == child || isText) && current.ValueClass.IsIdentifier() {
			return position, true
		}
	}

	return 0, false
}

// isEvent reports whether a label names an event: a namespace that the file
// declares above the statement, a dot and an id.
func (w *walker) isEvent(label string, statement int32) bool {
	return slices.ContainsFunc(w.frames[0].context.Namespaces, func(declaring int32) bool {
		namespace := w.tree.Unquoted(w.tree.Statements[declaring].Value)

		return declaring < statement && strings.HasPrefix(label, namespace+".") && len(label) > len(namespace)+1
	})
}
