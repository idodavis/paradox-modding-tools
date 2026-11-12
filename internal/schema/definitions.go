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

// findDefinition returns the definition a statement makes. It returns false if
// the statement defines nothing.
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

// findNested returns the nested definition a statement makes, named after its
// holder if it has one. It returns false if the statement makes none.
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

// isNested reports whether a key defines a nested definition rather than a
// field. A key the database declares is a field.
func (w *walker) isNested(key string, nesting game.Nesting, same bool) bool {
	if w.database.declares(key) {
		return false
	}

	fits, _ := path.Match(nesting.Keys, key)

	return fits || !same && nesting.Keys == ""
}

func (w *walker) findNameSpan(statement int32, nameKey string) (jomini.Span, bool) {
	child, ok := w.tree.FindChild(statement, nameKey)

	return w.tree.UnquotedSpan(w.tree.Statements[child].Value), ok
}

// findGUIDefinition returns the definition a statement of a GUI file makes.
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

// makeGUIDefinition returns a definition in a GUI database. If the database is
// local to its file, the file can call it. It returns false if the label is
// empty.
func (w *walker) makeGUIDefinition(database, label string, span jomini.Span, statement int32) (Copy, bool) {
	owner := w.schema.Databases[database]
	local := owner != nil && owner.Declaration.Local
	if local && label != "" {
		w.inline[label], _ = makeScripted(owner, label, w.tree, statement)
	}

	return Copy{Database: database, Name: label, Span: span, Statement: statement, Local: local}, label != ""
}

// findWindowLabel returns the name of a top window of a GUI file, or the
// file's name if it holds only that window. It returns "" otherwise.
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

// findAssetDefinition returns the definition a top block of an asset or font
// file makes, in a database named for the file's extension and the block's
// key, as in "asset.entity".
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
// makes. An event's name must open with a namespace declared above it.
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

		// a macro's name comes before its parameters
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

// judgeMode records an entry mode written where its database reads none.
func (w *walker) judgeMode(statement int32, definition Copy) {
	database := w.schema.Databases[definition.Database]
	if database == nil || definition.Mode == "" || !definition.IsOwn() {
		return
	}

	top := w.tree.Statements[statement].Parent == 0
	if precedence := database.Precedence; precedence.Modes == "" || precedence.ReadsMode(top) {
		return
	}

	facts := DescribeReference(Reference{Database: definition.Database, Name: definition.Name})
	facts["mode"] = definition.Mode
	mode := jomini.Span{Start: definition.Span.Start - len(definition.Mode) - 1, End: definition.Span.Start}
	w.record(ProblemMode, mode, facts)
}

// FindTextKey returns the statement that writes the localization key a
// definition names under the given key, such as an event's title. A key
// written as a block, as a triggered description is, gives the first text in
// it. A quoted sentence is no key. It returns false if there is none.
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

// isEvent reports whether a label is a declared namespace, a dot and an id.
func (w *walker) isEvent(label string, statement int32) bool {
	return slices.ContainsFunc(w.frames[0].context.Namespaces, func(declaring int32) bool {
		namespace := w.tree.Unquoted(w.tree.Statements[declaring].Value)

		return declaring < statement && strings.HasPrefix(label, namespace+".") && len(label) > len(namespace)+1
	})
}
