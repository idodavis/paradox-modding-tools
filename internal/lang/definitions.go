// definitions.go finds what a statement defines while a file is walked: a
// constant, a nested definition, or a definition of a GUI, asset or script
// file.

package lang

import (
	"path"
	"slices"
	"strconv"
	"strings"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/jomini"
)

// findDefinition returns the definition a statement makes, with the name it
// defines. It returns false if the statement defines nothing.
func (w *walker) findDefinition(statement int32, level int) (Copy, bool) {
	if label := w.frames[level].label; strings.HasPrefix(label, "@") {
		return Copy{Database: Constant, Name: label, Span: w.frames[level].span, Statement: statement, Local: true}, true
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

// findNested returns the definition a statement makes inside another
// definition, as a policy does inside an EU5 law. Where the database writes a
// separator, the name starts with the name of the definition that holds it,
// as NAI|X does. It returns false if the statement makes none.
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

		database, isOwn := nesting.Label(), nesting.Database == ""
		hasHolder := w.frames[level-1].context.Definition != nil || w.database.Declaration.DefinedBy == DefinedByNothing
		if !hasHolder && (isOwn || path.Ext(nesting.Parent) == "") {
			continue
		}

		if nesting.KeyPath == "" && !w.isNested(label, nesting, isOwn) {
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

// isNested reports whether a key written directly in a definition opens a
// nested definition and is no field. A key that a row, the dumps or the docs
// say is a field is a field. Any other key opens one if it fits the pattern
// the nesting gives, or if the nesting gives none and has a database of its
// own.
func (w *walker) isNested(key string, nesting Nesting, isOwn bool) bool {
	if w.database.declares(key) {
		return false
	}

	fits, _ := path.Match(nesting.Keys, key)

	return fits || !isOwn && nesting.Keys == ""
}

// findNameSpan returns where a statement's block writes its name: the value
// of the given key, without quotes. It returns false if the block does not
// write the key.
func (w *walker) findNameSpan(statement int32, nameKey string) (jomini.Span, bool) {
	child, ok := w.tree.FindChild(statement, nameKey)

	return w.tree.UnquotedSpan(w.tree.Statements[child].Value), ok
}

// findGUIDefinition returns the definition a statement of a GUI file makes: a
// type, a template or a block it declares, as type my_button = button { }
// does, a text format or icon named by a key of its block, or a top window.
// It returns false if the statement makes none.
func (w *walker) findGUIDefinition(statement int32, level int) (Copy, bool) {
	current := w.tree.Statements[statement]
	label, span := w.frames[level].label, w.frames[level].span
	lead := w.findLead(statement)

	switch nameKey := GUINameKeyOf[label]; {
	case slices.Contains(GUIDeclares, lead) && label != "":
		return w.makeGUIDefinition(GUIDatabasePrefix+lead, label, span, statement)

	case slices.Contains(GUIDeclares, label) && !current.Tag.IsEmpty():
		return w.makeGUIDefinition(GUIDatabasePrefix+label, w.tree.Text(current.Tag), current.Tag, statement)

	case nameKey != "":
		name, _ := w.findNameSpan(statement, nameKey)

		return w.makeGUIDefinition(GUIDatabasePrefix+label, w.tree.Text(name), name, statement)

	case level == 1 && !current.Key.IsEmpty() && current.IsBlock():
		return w.makeGUIDefinition(GUIWidgetDatabase, w.findWindowLabel(statement), span, statement)
	}

	return Copy{}, false
}

// makeGUIDefinition returns a definition of a GUI database. A definition of a
// database that answers only in its own file, as a local_type does, is also
// kept so the rest of the file can call it. It returns false for an empty
// name.
func (w *walker) makeGUIDefinition(database, label string, span jomini.Span, statement int32) (Copy, bool) {
	owner := w.schema.Databases[database]
	isLocal := owner != nil && owner.Declaration.Local
	if isLocal && label != "" {
		w.inline[label], _ = makeScripted(owner, label, w.tree, statement)
	}

	return Copy{Database: database, Name: label, Span: span, Statement: statement, Local: isLocal}, label != ""
}

// findWindowLabel returns the name of a top window of a GUI file: the name
// its block writes, else the file's own name if the file holds no other top
// block. It returns "" otherwise.
func (w *walker) findWindowLabel(statement int32) string {
	if value, _ := w.tree.FindChildValue(statement, GUIWidgetKey); value != "" {
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

// findAssetDefinition returns the definition a top block of an asset or a
// font file makes. Its database is named by the file's extension and the
// block's key, as asset.entity is, and its name is the value of the key the
// file type names. It returns false for any other statement, and for a block
// that writes no name.
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
// makes: one declared inside an events file, as scripted_effect x = { } is,
// or a top block of the file's database, named by its key or by the key the
// database names. An entry mode written before the name, as REPLACE:x, is
// split off. An event's name must start with a namespace its file declares
// above it. It returns false if the statement makes none.
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

	mode, label := w.dialect.SplitMode(label)
	if database.Declaration.Contents == ContentsEvents && !w.isEvent(label, statement) {
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

// isEvent reports whether a top key is an event's name: a namespace that the
// file declares above the statement, a dot and an id, as my_events.1.
func (w *walker) isEvent(label string, statement int32) bool {
	return slices.ContainsFunc(w.frames[0].context.Namespaces, func(declaring int32) bool {
		namespace := w.tree.Unquoted(w.tree.Statements[declaring].Value)

		return declaring < statement && strings.HasPrefix(label, namespace+".") && len(label) > len(namespace)+1
	})
}

// judgeMode records a problem for an entry mode written where the game does
// not read one: at the top of a file in a database that reads modes only
// inside a wrapping block, or the other way round. It records nothing for a
// database that takes no modes at all.
func (w *walker) judgeMode(statement int32, definition Copy) {
	database := w.schema.Databases[definition.Database]
	if database == nil || definition.Mode == "" || !definition.IsOwn() {
		return
	}

	isTop, place := w.tree.Statements[statement].Parent == 0, database.Precedence.Modes
	if place == "" || place == ModePlaceEntry && isTop || place == ModePlaceWrapped && !isTop {
		return
	}

	facts := DescribeReference(Reference{Database: definition.Database, Name: definition.Name})
	facts["mode"] = definition.Mode
	mode := jomini.Span{Start: definition.Span.Start - len(definition.Mode) - 1, End: definition.Span.Start}
	w.record(ProblemMode, mode, facts)
}

// FindTextKey returns the statement that writes the localization key of one
// of a definition's texts, as title = my_event.t does for an event's title.
// Where the text is written as a block that picks a key, as a triggered
// description is, it is the first key in the block. A quoted sentence is no
// key. It returns false if the definition writes none.
func FindTextKey(tree *jomini.Tree, statement int32, key string) (int32, bool) {
	child, ok := tree.FindChild(statement, key)
	if !ok {
		return 0, false
	}

	for position := child; position < tree.Statements[child].Last; position++ {
		current := tree.Statements[position]
		field := tree.Text(current.Key)
		isText := field == DescriptionKey || field == LabelKey && current.Parent == child
		if (position == child || isText) && current.ValueClass.IsIdentifier() {
			return position, true
		}
	}

	return 0, false
}
