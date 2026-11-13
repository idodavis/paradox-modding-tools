// values.go reads a statement's value during the walk: what it refers to,
// and whether it fits what its key takes.

package lang

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/jomini"
)

// lookupValue records what a statement's value is and what it refers to. A
// value that names nothing but is a chain of links is a link; yes and no
// never are.
func (w *walker) lookupValue(context *Context, statement int32, level int) {
	current := w.tree.Statements[statement]
	if current.IsBlock() || current.Value.IsEmpty() {
		return
	}

	span := w.tree.UnquotedSpan(current.Value)
	text := w.tree.Text(span)
	before := len(context.References)

	switch current.ValueClass {
	case jomini.ClassParameter:
		context.ValueGroup = CategoryParameter

		return

	case jomini.ClassConstant, jomini.ClassExpression:
		context.ValueGroup = CategoryConstant
		if current.ValueClass == jomini.ClassConstant {
			context.References = append(context.References, Reference{Database: Constant, Name: text, Span: span})
		}

		// an expression, as @[base * 2], refers to each constant it uses
		for _, token := range jomini.ListConstants(text, span.Start) {
			context.References = append(context.References,
				Reference{Database: Constant, Name: "@" + w.tree.Text(token), Span: token})
		}

		return
	}

	if call, ok := w.tree.ParseBracketed(current.Value); ok {
		context.ValueGroup, context.Call = CategoryCall, new(call)
		context.References = append(context.References, w.schema.ListCallReferences(call, context.Datacontext)...)

		return
	}

	if text != "" && !strings.ContainsAny(text, "[$") && !isSavedName(context, span) {
		context.References = append(context.References, w.listNames(context, statement, level, span)...)
	}

	field := context.Field
	switch {
	case len(context.References) > before:
		context.ValueGroup = CategoryReference

	case field != nil && field.Value.Value.Type == ValueTypeChoice && w.schema.MatchValue(field.Value.Value, text):
		context.ValueGroup = CategoryChoice

	case !slices.Contains(Bools, text) && w.schema.isChain(text):
		context.ValueGroup = CategoryLink
		context.References = append(context.References, w.schema.listLinkUses(text, span.Start)...)
	}
}

// isSavedName reports whether the value is a name its own statement saves or
// reads back, as in has_character_flag = x. Such a value names nothing else,
// even where it looks like an event's id.
func isSavedName(context *Context, span jomini.Span) bool {
	if context.Defines && context.Definition.Span == span && slices.Contains(Links, context.Definition.Database) {
		return true
	}

	return slices.ContainsFunc(context.References, func(reference Reference) bool {
		return reference.Span == span && slices.Contains(Links, reference.Database)
	})
}

// listNames returns the references a plain value makes: one in each database
// that its field, its keyword, its place or its block's shape says it may
// name, then a script value and the names after a link's colon.
func (w *walker) listNames(context *Context, statement int32, level int, span jomini.Span) []Reference {
	current := w.tree.Statements[statement]
	text, class := w.tree.Text(span), current.ValueClass

	// a math keyword's value such as opinion(scope:ruler) stands for the
	// command's number, so only its argument refers to anything
	step, command, isCall := w.schema.FindKeyCall(text, span.Start)
	if isCall && context.isMath() {
		return w.listArgumentReferences(command, command.Field, step.Arguments[0])
	}

	claim, databases := w.listClaimed(context, text, command)

	// a switch's trigger may be a trigger such as has_culture, or a link
	if keyword := context.Keyword; keyword != nil && keyword.Names != "" && w.schema.LookupDefinition(keyword.Names, text) != nil {
		databases = append(databases, keyword.Names)
	}

	place, folder, hasPlace := w.findPlace(context, statement, level, text)
	if hasPlace {
		databases = append(databases, place)
	}

	// a value with a space is literal text, since a message key has none
	if database := ShapeRows[findShape(context)].Values; database != "" && !hasPlace && !strings.Contains(text, " ") {
		databases = append(databases, database)
	}

	// an event id names an event where its field takes events, or takes
	// nothing and an events file declares its namespace
	namespace, _, _ := strings.Cut(text, ".")
	if w.schema.Events != "" && w.file.Format == files.FormatScript && class == jomini.ClassEventID &&
		(claim == w.schema.Events ||
			claim == "" && w.schema.LookupScripted(w.winners, EventNamespaces, namespace) != nil) {
		databases = append(databases, w.schema.Events)
	}

	// what only the game's files say a field takes accuses no mod
	isInferred := claim != "" && !context.Field.Value.Source.CanAccuse() && !slices.Contains(databases[1:], claim)

	var references []Reference
	for position, database := range databases {
		if slices.Contains(databases[:position], database) {
			continue
		}

		name := text

		switch {
		case database == place && folder != "" && !strings.Contains(text, "/"):
			name = folder + "/" + text
		case database == claim && context.Field.Value.Value.Stem != "":
			name = fmt.Sprintf(context.Field.Value.Value.Stem, text)
		}

		// a name of some databases needs no definition there, as a name of
		// an effect's localization does not
		owner := w.schema.Databases[database]
		references = append(references, Reference{
			Database: database, Name: name, Span: span,
			Inferred: position == 0 && isInferred || owner != nil && owner.Declaration.AllowsUndefined,
		})
	}

	links := jomini.Links(text, span.Start)
	if value, ok := w.findScriptValue(context, links, class, current.Value != span); ok {
		references = append(references, value)
	}

	return append(references, w.listPrefixed(links)...)
}

// isMath reports whether the statement's key is a math keyword, such as add.
func (c *Context) isMath() bool {
	return c.Keyword != nil && c.Keyword.Class == KeywordClassMath
}

// listArgumentReferences returns the references of an argument in
// parentheses after a command's name, as in
// aptitude(court_jester_court_position). The argument stands for one key of
// the command, so it names a definition of that key's database, unless it
// is a link.
func (w *walker) listArgumentReferences(command *Definition, arguments *Field, argument jomini.Argument) []Reference {
	field := arguments.findChild(command.ArgumentKey)
	if field != nil && field.Value.Value.Type == ValueTypeReference && field.Value.Value.Database != "" &&
		!w.schema.IsLink(argument.Text) {
		return []Reference{{
			Database: field.Value.Value.Database, Name: argument.Text, Span: argument.Span,
			Inferred: !field.Value.Source.CanAccuse(),
		}}
	}

	return w.listPrefixed(jomini.Links(argument.Text, argument.Span.Start))
}

// listPrefixed returns the references of the names a chain writes after a
// colon, as norse in culture:norse. A name that is itself a chain, as
// scope:overlord in relations:scope:overlord, gives that chain's references.
func (w *walker) listPrefixed(links []jomini.Link) []Reference {
	var references []Reference
	for _, link := range links {
		if link.Prefix.IsEmpty() {
			continue
		}

		label := w.tree.Text(link.Name)
		database, _ := w.schema.LookupPrefix(w.tree.Text(link.Prefix))

		switch {
		case !slices.Contains(Links, database) && w.schema.IsLink(label):
			references = append(references, w.listPrefixed(jomini.Links(label, link.Name.Start))...)
		case database != "":
			references = append(references, w.listLabelReferences(database, label, link.Name.Start)...)
		}
	}

	return references
}

// listLabelReferences returns the references of one name after a link's
// colon. Where the database joins names with a separator there are two:
// define:NAI|X names the group NAI and the define NAI|X.
func (w *walker) listLabelReferences(database, label string, start int) []Reference {
	owner := w.schema.Databases[database]
	if owner == nil || owner.Declaration.Separator == "" {
		return []Reference{{Database: database, Name: label, Span: jomini.Span{Start: start, End: start + len(label)}}}
	}

	holder, rest, _ := strings.Cut(label, owner.Declaration.Separator)
	references := []Reference{{Database: database, Name: holder, Span: jomini.Span{Start: start, End: start + len(holder)}}}
	key, _, _ := strings.Cut(rest, owner.Declaration.Separator)
	if key == "" {
		return references
	}

	keyStart := start + len(holder) + len(owner.Declaration.Separator)

	for _, nested := range owner.Nested {
		references = append(references, Reference{
			Database: nested, Name: w.schema.JoinNested(nested, holder, key),
			Span: jomini.Span{Start: keyStart, End: keyStart + len(key)},
		})
	}

	return references
}

// listClaimed returns the database the key's field says the value names,
// with its nested databases where the field allows them. command is what the
// value calls, if it is written as a call. It returns none where the key or
// the value is a link. A localization key may start like a link, as
// great_holy_war.0051.generic.start.tt does, so there only a whole chain of
// links counts as one.
func (w *walker) listClaimed(context *Context, text string, command *Definition) (string, []string) {
	field := context.Field
	if !field.claimsDatabase() || context.Category == CategoryLink {
		return "", nil
	}

	claim := field.Value.Value.Database
	isLink := w.schema.IsLink(text) || command != nil && command.Database == DumpLinks
	if isLink && (claim != Localization || w.schema.isChain(text)) {
		return "", nil
	}

	databases := []string{claim}
	if database := w.schema.Databases[claim]; database != nil && field.Value.Value.Nested {
		databases = append(databases, database.Nested...)
	}

	return claim, databases
}

// findPlace returns the database a value names because of where it is
// written, and the folder of a file it names without one. It returns false
// where the place names no database.
func (w *walker) findPlace(context *Context, statement int32, level int, text string) (string, string, bool) {
	key, keys := w.frames[level].label, w.frames[level].path

	// a GUI file's widgets name definitions by one key at any depth
	if widgets := w.schema.Databases[GUIWidgetDatabase]; w.file.Format == files.FormatGUI && widgets != nil {
		for _, row := range widgets.Declaration.References {
			if key == row.KeyPath {
				return row.Target, row.Folder, true
			}
		}
	}

	database := w.schema.Databases[context.Database]
	if database != nil {
		for _, row := range database.Declaration.References {
			if jomini.MatchKeyPath(row.KeyPath, keys) {
				return cmp.Or(row.Target, database.Name), row.Folder, true
			}
		}
	}

	// a value with a slash is a file's path, which Vic3's video step also
	// takes (events/brazil/amazonas.txt:15)
	if picture, ok := w.findPictureDatabase(context.Database, keys); ok && !strings.Contains(text, "/") {
		return picture, "", true
	}

	if database != nil && (!context.Defines || !context.Definition.Alias) {
		if target, isAlias := database.Declaration.Aliases[key]; isAlias {
			return cmp.Or(target, database.Name), "", true
		}
	}

	return w.findCoatDatabase(statement, level, text)
}

// findPictureDatabase returns the database a value names as one step from
// an event to its picture, as an event's theme names an event theme. A step
// that picks a block by a field, or matches something other than a name, is
// skipped.
func (w *walker) findPictureDatabase(database string, keys []string) (string, bool) {
	for _, chain := range w.dialect.Events.Picture.Chains {
		start := w.schema.Events
		for _, step := range chain.Steps {
			if start == database && step.Match == "" && !strings.Contains(step.Key, "[") &&
				jomini.MatchKeyPath(step.Key, keys) {
				return step.Database, true
			}

			start = step.Database
		}
	}

	return "", false
}

// findCoatDatabase returns the database a value names inside a coat of arms,
// and the folder of a picture it names.
func (w *walker) findCoatDatabase(statement int32, level int, text string) (string, string, bool) {
	if w.database == nil || w.database.Name != CoatDatabase {
		return "", "", false
	}

	isSlot := func(key string) bool {
		return slices.ContainsFunc(CoatSlots, func(slot string) bool { return strings.EqualFold(slot, key) })
	}
	key, holder := w.frames[level].label, w.frames[level-1].label

	switch {
	case !w.tree.Statements[statement].Tag.IsEmpty():
		return CoatListsDatabase, "", true

	case isSlot(key) && !isSlot(text):
		return CoatColorsDatabase, "", true

	case key == CoatPatternKey:
		return File, CoatTextureFolders[key], true

	case key == CoatTextureKey && CoatTextureFolders[holder] != "":
		return File, CoatTextureFolders[holder], true
	}

	return "", "", false
}

// findScriptValue returns the reference a value makes to a script value: as
// the last link of a chain (root.my_value), as a math keyword's value
// (add = my_value), or as the value of a field that takes one. Where only
// the game's files say the field takes one, it accuses no mod.
func (w *walker) findScriptValue(
	context *Context, links []jomini.Link, class jomini.Class, isQuoted bool,
) (Reference, bool) {
	tail := links[len(links)-1].Span
	last := w.tree.Text(tail)
	isChain := len(links) > 1 && class != jomini.ClassEventID &&
		!slices.ContainsFunc(links[:len(links)-1], func(link jomini.Link) bool { return !w.schema.IsLink(w.tree.Text(link.Span)) })
	isMath := context.isMath()
	takesValue := context.Category != CategoryLink && context.Field != nil && context.Field.Value.Value.Values
	isCommand := w.schema.FindCommand(last) != nil

	switch {
	case w.schema.Values == "" || !isChain && !isMath && !takesValue:
		return Reference{}, false
	case !jomini.Classify(last).IsIdentifier() || isCommand || w.schema.IsLink(last) || slices.Contains(Bools, last):
		return Reference{}, false
	case isQuoted && !isChain, isMath && !w.takesNumber(findFormula(context)):
		return Reference{}, false
	}

	isInferred := !isChain && !isMath && !context.Field.Value.Source.CanAccuse()

	return Reference{Database: w.schema.Values, Name: last, Span: tail, Inferred: isInferred}, true
}

// findFormula returns the statement that opens the formula a statement is
// part of: the nearest block around it that is not a math keyword's, an
// if's, a link's or a list's.
func findFormula(context *Context) *Context {
	opener := context.Parent
	for opener.Parent != nil {
		keyword := opener.Keyword
		isInner := opener.Category == CategoryLink || opener.Iterates ||
			keyword != nil && (keyword.Class == KeywordClassMath || keyword.Shape == ShapeEnclosing)
		if !isInner {
			break
		}

		opener = opener.Parent
	}

	return opener
}

// takesNumber reports whether the key that opens a formula takes a number.
// It does unless a firm source says otherwise, as EU5's docs give a
// situation's secondary_map_color a color.
func (w *walker) takesNumber(opener *Context) bool {
	if opener.Field == nil || !opener.Field.Value.Source.CanAccuse() {
		return true
	}

	switch expected := opener.Field.Value.Value; expected.Type {
	case ValueTypeNumber, ValueTypeBlock, ValueTypeUnknown:
		return true
	case ValueTypeReference:
		return expected.Database == w.schema.Values
	}

	return false
}

// judgeValue records a problem for a value of a kind its key does not take,
// as a word where a number goes. It does so only where a firm source states
// the kind and every value the game's own files write there fits it.
func (w *walker) judgeValue(context *Context, statement int32) {
	field, current := context.Field, w.tree.Statements[statement]
	eligible := context.Category == CategoryField || context.Category == CategoryReference ||
		context.Category == CategoryCommand
	if !eligible || field == nil || !field.Value.IsStated() || !field.Value.Source.CanAccuse() || current.IsBlock() ||
		current.Value.IsEmpty() || context.ValueGroup == CategoryReference {
		return
	}

	expected, text := field.Value.Value, w.tree.Text(current.Value)
	if expected.Database == File || expected.Type == ValueTypeText || expected.Type == ValueTypeBlock {
		return
	}

	// where a number is expected the game also reads a script value, or a
	// trigger that returns a number
	if expected.Type == ValueTypeNumber && current.ValueClass.IsIdentifier() || strings.ContainsAny(text, "[$@") ||
		w.schema.MatchValue(expected, text) {
		return
	}

	for value := range field.Vanilla.Values {
		if !w.schema.MatchValue(expected, value) {
			return
		}
	}

	w.problems = append(w.problems, Problem{
		Code: ProblemValue, Span: current.Value, Expected: expected,
		Facts: DescribeReference(Reference{Name: w.tree.Unquoted(current.Key)}),
	})
}

// judgeDate records a problem for a date the game cannot have, as 1066.13.1,
// written as a key or as the value of a field a firm source says takes a
// date.
func (w *walker) judgeDate(context *Context, statement int32) {
	current := w.tree.Statements[statement]
	spans := make([]jomini.Span, 0, 2)
	if current.KeyClass == jomini.ClassDate {
		spans = append(spans, current.Key)
	}

	field := context.Field
	if field != nil && !current.IsBlock() && field.Value.Value.Type == ValueTypeDate && field.Value.Source.CanAccuse() {
		spans = append(spans, current.Value)
	}

	for _, span := range spans {
		if date, ok := jomini.ParseDate(w.tree.Unquoted(span)); ok && !date.IsValid() {
			w.record(ProblemDate, span, DescribeReference(Reference{Name: w.tree.Unquoted(span)}))
		}
	}
}

// judge marks as inferred each reference that is no mistake when nothing
// defines its name: a name script saves, a call anything may answer, a
// widget's type, and a name with a link's colon or a parameter in it.
func judge(references []Reference) {
	for position := range references {
		reference := &references[position]
		reference.Inferred = reference.Inferred || reference.Database == "" || slices.Contains(Links, reference.Database) ||
			reference.Caller == AnyCall || reference.Caller == GUITagCalls || hasPrefix(reference.Name) ||
			strings.Contains(reference.Name, "$")
	}
}

// hasPrefix reports whether a chain writes a name after a link's colon, as
// scope:x, inside a link's parentheses too.
func hasPrefix(chain string) bool {
	return slices.ContainsFunc(jomini.Links(chain, 0), func(link jomini.Link) bool {
		return !link.Prefix.IsEmpty() || !link.Argument.IsEmpty() && hasPrefix(chain[link.Argument.Start:link.Argument.End])
	})
}
