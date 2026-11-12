// values.go reads what a statement's value refers to.

package schema

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/jomini"
)

// lookupValue records what the value of a scalar statement refers to, and its
// category. A chain of links that refers to nothing is a link, never yes or no.
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
			context.References = append(context.References, Reference{Database: game.Constant, Name: text, Span: span})
		}

		// an expression such as @[…] refers to the constants it uses
		for _, token := range jomini.ListConstants(text, span.Start) {
			context.References = append(context.References,
				Reference{Database: game.Constant, Name: "@" + w.tree.Text(token), Span: token})
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

	switch {
	case len(context.References) > before:
		context.ValueGroup = CategoryReference
	case w.isChoice(context, text):
		context.ValueGroup = CategoryChoice
	case !slices.Contains(game.Bools, text) && w.schema.IsLink(text) && w.schema.isChain(text):
		context.ValueGroup = CategoryLink
		context.References = append(context.References, w.schema.listLinkUses(text, span.Start)...)
	}
}

func (w *walker) isChoice(context *Context, text string) bool {
	field := context.Field

	return field != nil && field.Value.Value.Type == ValueTypeChoice && w.schema.MatchValue(field.Value.Value, text)
}

// isSavedName reports whether the value in the span is a name its statement
// saves or reads back, such as a flag. It names nothing else, even if it looks
// like an event id.
func isSavedName(context *Context, span jomini.Span) bool {
	if context.Defines && context.Definition.Span == span && slices.Contains(game.Links, context.Definition.Database) {
		return true
	}

	return slices.ContainsFunc(context.References, func(reference Reference) bool {
		return reference.Span == span && slices.Contains(game.Links, reference.Database)
	})
}

// listNames returns the references a plain value makes to the databases its
// field, keyword, place and block give it, to an event or script value, and
// after link prefixes.
func (w *walker) listNames(context *Context, statement int32, level int, span jomini.Span) []Reference {
	current := w.tree.Statements[statement]
	text, class := w.tree.Text(span), current.ValueClass

	// a math keyword's value such as opinion(scope:ruler) stands for the
	// command's number, so only its argument makes references
	if step, command, ok := w.schema.findKeyCall(text, span.Start); ok && context.isMath() {
		return w.listArgumentReferences(command, command.Field, step.Arguments[0])
	}

	claim, databases := w.listClaimed(context, text)

	// a switch's trigger may be a trigger such as has_culture, or a link
	if keyword := context.Keyword; keyword != nil && keyword.Names != "" && w.schema.LookupDefinition(keyword.Names, text) != nil {
		databases = append(databases, keyword.Names)
	}

	placed, folder, isPlaced := w.findPlace(context, statement, level, text)
	if isPlaced {
		databases = append(databases, placed)
	}

	// a value with a space is literal text, since a message key has none
	if database := shapeRows[findShape(context)].values; database != "" && !isPlaced && !strings.Contains(text, " ") {
		databases = append(databases, database)
	}

	// an event id names an event if its field claims events, or nothing and
	// an events file declares its namespace
	if w.schema.Events != "" && w.file.Format == files.FormatScript && class == jomini.ClassEventID &&
		(claim == w.schema.Events || claim == "" && w.hasNamespace(text)) {
		databases = append(databases, w.schema.Events)
	}

	// the claim is inferred if no rule but the game's files makes it
	inferred := claim != "" && !context.Field.Value.Source.CanAccuse() && !slices.Contains(databases[1:], claim)
	var references []Reference
	for position, database := range databases {
		if slices.Contains(databases[:position], database) {
			continue
		}

		name := text
		switch {
		case database == placed && folder != "" && !strings.Contains(text, "/"):
			name = folder + "/" + text
		case database == claim && context.Field.Value.Value.Stem != "":
			name = fmt.Sprintf(context.Field.Value.Value.Stem, text)
		}

		references = append(references, Reference{
			Database: database, Name: name, Span: span, Inferred: position == 0 && inferred || w.allowsUndefined(database),
		})
	}

	if value, ok := w.findScriptValue(context, span, class, current.Value != span); ok {
		references = append(references, value)
	}

	return append(references, w.listPrefixed(text, span.Start)...)
}

// isMath reports whether the statement's key is a math keyword, such as add.
func (c *Context) isMath() bool {
	return c.Keyword != nil && c.Keyword.Class == game.KeywordClassMath
}

// listArgumentReferences returns the references the argument in parentheses
// after a command's name makes. It fills the command's ArgumentKey, so it
// names an entry of that key's database unless it is a link.
func (w *walker) listArgumentReferences(command *Definition, arguments *Field, argument jomini.Argument) []Reference {
	field := arguments.findChild(command.ArgumentKey)
	if field != nil && field.Value.Value.Type == ValueTypeReference && field.Value.Value.Database != "" &&
		!w.schema.IsLink(argument.Text) {
		return []Reference{{
			Database: field.Value.Value.Database, Name: argument.Text, Span: argument.Span,
			Inferred: !field.Value.Source.CanAccuse(),
		}}
	}

	return w.listPrefixed(argument.Text, argument.Span.Start)
}

// listClaimed returns the database a value's field claims it names, with the
// ones nested in it if the field says so. It returns "" and nil if the field
// claims none, or the key or the value is a link. A text key may open like a
// link, as CK3's great_holy_war.0051.generic.start.tt does, so there only a
// chain of links is one.
func (w *walker) listClaimed(context *Context, text string) (string, []string) {
	field := context.Field
	if !field.claimsDatabase() || context.Category == CategoryLink {
		return "", nil
	}

	claim := field.Value.Value.Database
	_, command, isCall := w.schema.findKeyCall(text, 0)
	linked := w.schema.IsLink(text) || isCall && command.Database == game.DumpLinks
	if linked && (claim != game.Localization || w.schema.isChain(text)) {
		return "", nil
	}

	databases := []string{claim}
	if database := w.schema.Databases[claim]; database != nil && field.Value.Value.Nested {
		databases = append(databases, database.Nested...)
	}

	return claim, databases
}

// findScriptValue returns the reference a value makes to a script value: the
// last step of a chain of links, a math keyword's value, or the value of a
// field that takes one. It returns false for a number, a command, a link, yes,
// no and a plain name in quotes, which is text. The reference is inferred if
// only the field makes it from the game's files.
func (w *walker) findScriptValue(context *Context, span jomini.Span, class jomini.Class, quoted bool) (Reference, bool) {
	links := jomini.Links(w.tree.Text(span), span.Start)
	tail := links[len(links)-1].Span
	last := w.tree.Text(tail)
	chained := len(links) > 1 && class != jomini.ClassEventID &&
		!slices.ContainsFunc(links[:len(links)-1], func(link jomini.Link) bool { return !w.schema.IsLink(w.tree.Text(link.Span)) })
	operated := context.isMath()
	valued := context.Category != CategoryLink && context.Field != nil && context.Field.Value.Value.Values
	declared := w.schema.FindCommand(last) != nil
	switch {
	case w.schema.Values == "" || !chained && !operated && !valued:
		return Reference{}, false
	case !jomini.Classify(last).IsIdentifier() || declared || w.schema.IsLink(last) || slices.Contains(game.Bools, last):
		return Reference{}, false
	case quoted && !chained, operated && !w.takesNumber(findFormula(context)):
		return Reference{}, false
	}

	inferred := !chained && !operated && !context.Field.Value.Source.CanAccuse()

	return Reference{Database: w.schema.Values, Name: last, Span: tail, Inferred: inferred}, true
}

// findFormula returns the context of the key that opens the formula a
// statement is in: the nearest block around it that is no math keyword's,
// enclosing keyword's, link's or list's.
func findFormula(context *Context) *Context {
	opener := context.Parent
	for opener.Parent != nil {
		keyword := opener.Keyword
		inside := opener.Category == CategoryLink || opener.Iterates ||
			keyword != nil && (keyword.Class == game.KeywordClassMath || keyword.Shape == game.ShapeEnclosing)
		if !inside {
			break
		}

		opener = opener.Parent
	}

	return opener
}

// takesNumber reports whether the key that opens a formula takes a number,
// unless a firm source says otherwise, as EU5's docs do for a situation's
// secondary_map_color.
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

// findPlace returns the database a statement's value names by where it is
// written, and the folder of a file it names with no folder of its own. Rows
// of references, event picture steps, aliases and coat of arms keys are tried
// in that order. It returns false if the place names none.
func (w *walker) findPlace(context *Context, statement int32, level int, text string) (string, string, bool) {
	key := w.frames[level].label
	keys := w.frames[level-1].path
	if key != "" {
		keys = slices.Concat(keys, []string{key})
	}

	// a GUI file's widgets name definitions by one key at any depth
	if widgets := w.schema.Databases[game.GUI.WidgetDatabase]; w.file.Format == files.FormatGUI && widgets != nil {
		for _, row := range widgets.Declaration.References {
			if key == row.KeyPath {
				return row.Target, row.Folder, true
			}
		}
	}

	if database := w.schema.Databases[context.Database]; database != nil {
		for _, row := range database.Declaration.References {
			if jomini.MatchKeyPath(row.KeyPath, keys) {
				return cmp.Or(row.Target, database.Name), row.Folder, true
			}
		}
	}

	// a value with a slash is a file's path, which Vic3's video step also
	// takes (events/brazil/amazonas.txt:15)
	if database, ok := w.findPictureDatabase(context.Database, keys); ok && !strings.Contains(text, "/") {
		return database, "", true
	}

	if database := w.schema.Databases[context.Database]; database != nil && (!context.Defines || !context.Definition.Alias) {
		if target, isAlias := database.Declaration.Aliases[key]; isAlias {
			return cmp.Or(target, database.Name), "", true
		}
	}

	return w.findCoatDatabase(statement, level, text)
}

// findCoatDatabase returns the database a statement's value names in a coat of
// arms, and the folder of its image. It returns false anywhere else.
func (w *walker) findCoatDatabase(statement int32, level int, text string) (string, string, bool) {
	if w.database == nil || w.database.Name != game.Coat.Database {
		return "", "", false
	}

	isSlot := func(key string) bool {
		return slices.ContainsFunc(game.Coat.Slots, func(slot game.CoatSlot) bool { return strings.EqualFold(slot.Key, key) })
	}
	key, holder := w.frames[level].label, w.frames[level-1].label
	switch {
	case !w.tree.Statements[statement].Tag.IsEmpty():
		return game.Coat.ListsDatabase, "", true
	case isSlot(key) && !isSlot(text):
		return game.Coat.ColorsDatabase, "", true
	case key == game.Coat.PatternKey:
		return game.File, game.Coat.TextureFolders[key], true
	case key == game.Coat.TextureKey && game.Coat.TextureFolders[holder] != "":
		return game.File, game.Coat.TextureFolders[holder], true
	}

	return "", "", false
}

// findPictureDatabase returns the database a value names as one step of the
// game's event pictures, given the database it is written in and the keys
// that lead to it. Steps that pick a block by a field or match the value
// elsewhere than in a name are skipped. It returns false for none.
func (w *walker) findPictureDatabase(database string, keys []string) (string, bool) {
	for _, chain := range w.row.Events.Picture.Chains {
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

// hasNamespace reports whether an events file declares the namespace of an
// event id.
func (w *walker) hasNamespace(id string) bool {
	namespace, _, _ := strings.Cut(id, ".")

	return w.schema.lookupScripted(w.winners, game.EventNamespaces, namespace) != nil
}

func (w *walker) allowsUndefined(label string) bool {
	database := w.schema.Databases[label]

	return database != nil && database.Declaration.AllowsUndefined
}

// listPrefixed returns the references a chain of links makes after prefixes
// that have a database. A name that is itself a chain, as scope:overlord is in
// relations:scope:overlord, makes its own.
func (w *walker) listPrefixed(chain string, base int) []Reference {
	var references []Reference
	for _, link := range jomini.Links(chain, base) {
		if link.Prefix.IsEmpty() {
			continue
		}

		prefix, label := chain[link.Prefix.Start-base:link.Prefix.End-base], chain[link.Name.Start-base:link.Name.End-base]
		database, _ := w.schema.LookupPrefix(prefix)
		switch {
		case !slices.Contains(game.Links, database) && w.schema.IsLink(label):
			references = append(references, w.listPrefixed(label, link.Name.Start)...)
		case database != "":
			references = append(references, w.listLabelReferences(database, label, link.Name.Start)...)
		}
	}

	return references
}

// listLabelReferences returns the references a prefixed link's label makes: for
// define:NAI|X, the group NAI and the define NAI|X.
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

// judgeValue records a value that is not the type its key takes, if a firm
// source states the type and every value the game's files write fits it.
func (w *walker) judgeValue(context *Context, statement int32) {
	field, current := context.Field, w.tree.Statements[statement]
	eligible := context.Category == CategoryField || context.Category == CategoryReference ||
		context.Category == CategoryCommand
	if !eligible || field == nil || !field.Value.IsStated() || !field.Value.Source.CanAccuse() || current.IsBlock() ||
		current.Value.IsEmpty() || context.ValueGroup == CategoryReference {
		return
	}

	expected, text := field.Value.Value, w.tree.Text(current.Value)
	if expected.Database == game.File || expected.Type == ValueTypeText || expected.Type == ValueTypeBlock {
		return
	}

	// the game reads a script value, or a trigger that returns a number,
	// where a number is expected
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

// judgeDate records a date the game cannot have, in a key or in the value of a
// field a firm source says takes a date.
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

// judge sets Inferred on each reference that is never an error, such as one
// to names script saves, or a name with a prefix or a parameter in it.
func judge(references []Reference) {
	for position := range references {
		reference := &references[position]
		reference.Inferred = reference.Inferred || reference.Database == "" || slices.Contains(game.Links, reference.Database) ||
			reference.Caller == game.AnyCall || reference.Caller == game.GUI.TagCalls || hasPrefix(reference.Name) ||
			strings.Contains(reference.Name, "$")
	}
}

// hasPrefix reports whether a chain writes a link with a name after a colon,
// also inside a link's parentheses.
func hasPrefix(chain string) bool {
	return slices.ContainsFunc(jomini.Links(chain, 0), func(link jomini.Link) bool {
		return !link.Prefix.IsEmpty() || !link.Argument.IsEmpty() && hasPrefix(chain[link.Argument.Start:link.Argument.End])
	})
}
