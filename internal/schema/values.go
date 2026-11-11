// values.go reads the value of a statement: what it refers to, by its class,
// by what its field claims, by where it is written, and as a formula's
// number. It also gives each reference its verdict.

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

// lookupValue resolves what the value of a scalar statement refers to, and
// gives the value its color category. A parameter refers to nothing, a
// constant to itself and the constants it uses, and a data call to what its
// arguments name. Any other value refers to the names listNames finds, unless
// it holds a parameter or a bracket, or is a name the statement saves or
// reads back. A value that refers to nothing and is a chain of links is a
// link, but never a yes or a no.
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
	}
}

// isChoice reports whether a value is one of the choices that the field of a
// context takes.
func (w *walker) isChoice(context *Context, text string) bool {
	field := context.Field

	return field != nil && field.Value.Value.Type == ValueTypeChoice && w.schema.MatchValue(field.Value.Value, text)
}

// isSavedName reports whether the value written at a span is a name of one
// of game.Links that its statement saves or reads back, such as a flag. Such
// a name names nothing else, even where it is shaped like an event id.
func isSavedName(context *Context, span jomini.Span) bool {
	if context.Defines && context.Definition.Span == span && slices.Contains(game.Links, context.Definition.Database) {
		return true
	}

	return slices.ContainsFunc(context.References, func(reference Reference) bool {
		return reference.Span == span && slices.Contains(game.Links, reference.Database)
	})
}

// listNames returns the references a plain value makes, in this order: a
// name of each database its field claims, of the database its place names
// and of the one its block's shape gives a text; an event where it is shaped
// like an event id; a script value; and the names after its links' prefixes.
// A math keyword's value that is a command's name with an argument in
// parentheses makes the references of its argument alone.
func (w *walker) listNames(context *Context, statement int32, level int, span jomini.Span) []Reference {
	current := w.tree.Statements[statement]
	text, class := w.tree.Text(span), current.ValueClass

	// a math keyword's value such as opinion(scope:ruler) stands for the
	// command's number, so its argument makes the references
	if step, command, ok := w.schema.findKeyCall(text, span.Start); ok && context.isMath() {
		return w.listArgumentReferences(command, command.ArgumentsOf(""), step.Arguments[0])
	}

	claim, databases := w.listClaimed(context, text)
	placed, folder, isPlaced := w.findPlace(context, statement, level, text)
	if isPlaced {
		databases = append(databases, placed)
	}

	// a value with a space is literal text, since a message key has none
	if database := game.Shapes[findShape(context)].Values; database != "" && !isPlaced && !strings.Contains(text, " ") {
		databases = append(databases, database)
	}

	// a value shaped like an event id names an event where its field claims
	// events, and where its field claims nothing and an events file declares
	// its namespace
	if w.schema.Events != "" && w.file.Format == files.FormatScript && class == jomini.ClassEventID &&
		(claim == w.schema.Events || claim == "" && w.hasNamespace(text)) {
		databases = append(databases, w.schema.Events)
	}

	// the claim is inferred if it rests on the game's own files and no other
	// rule makes it
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

// isMath reports whether the statement's key is a math keyword, such as
// value or add, whose value is a number.
func (c *Context) isMath() bool {
	return c.Keyword != nil && c.Keyword.Class == game.KeywordClassMath
}

// listArgumentReferences returns the references that the argument in
// parentheses after a command's name makes. The argument fills the command's
// ArgumentKey, one of the given keys the command takes. Where that key takes
// a name from a database, the argument is such a name, unless it is a link.
// Any other argument is read as a chain of links.
func (w *walker) listArgumentReferences(command *Command, arguments *Field, argument jomini.Argument) []Reference {
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

// listClaimed returns the database that a value's field claims it names, and
// that database with the ones nested in it where the field says so, as a
// dialect is nested in its language. It returns "" and nil if the field
// claims none or the value is a link, even one written with an argument in
// parentheses. A link key's field is its holder's, so it claims nothing for
// the link's value. A text key may open as a link, as CK3's
// great_holy_war.0051.generic.start.tt does, so where a text key is claimed
// only a value whose every step is a link is one.
func (w *walker) listClaimed(context *Context, text string) (string, []string) {
	field := context.Field
	if !field.claimsDatabase() || context.Category == CategoryLink {
		return "", nil
	}

	claim := field.Value.Value.Database
	step, _, isCall := w.schema.findKeyCall(text, 0)
	linked := w.schema.IsLink(text) || isCall && w.schema.IsLink(step.Text)
	if linked && (claim != game.Localization || w.schema.isChain(text)) {
		return "", nil
	}

	databases := []string{claim}
	if database := w.schema.Databases[claim]; database != nil && field.Value.Value.Nested {
		databases = append(databases, database.Nested...)
	}

	return claim, databases
}

// findScriptValue returns the reference a value of the given class makes to
// a script value: the last step of a chain of links, a math keyword's value,
// or the value of a field that takes a script value. It returns false if the
// value makes none: a number, a command, a link, yes or no, a plain name
// written in quotes, which is text, and a math keyword's value in a formula
// that picks something other than a number. The reference is inferred where
// only the field makes it and the field learned so from the game's own
// files. A link key's field is its holder's, so it gives the link's value
// none.
func (w *walker) findScriptValue(context *Context, span jomini.Span, class jomini.Class, quoted bool) (Reference, bool) {
	links := jomini.Links(w.tree.Text(span), span.Start)
	tail := links[len(links)-1].Span
	last := w.tree.Text(tail)
	chained := len(links) > 1 && class != jomini.ClassEventID &&
		!slices.ContainsFunc(links[:len(links)-1], func(link jomini.Link) bool { return !w.schema.IsLink(w.tree.Text(link.Span)) })
	operated := context.isMath()
	valued := context.Category != CategoryLink && context.Field != nil && context.Field.Value.Value.Values
	_, declared := w.schema.FindCommand(last)
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
// statement is written in: the nearest block around it that is no math
// keyword's, no enclosing keyword's, no link's and no list's, each of which
// holds the formula's math again.
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

// takesNumber reports whether the key that opens a formula takes a number.
// It does unless a source that can accuse says its value is something else,
// as EU5's docs say a situation's secondary_map_color is a color: there the
// same if, else and value pick a color, and a value names no script value.
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

// findPlace returns the database that a statement's value names by the place
// it is written in, and the folder of a file it names with no folder of its
// own. The place is a row of the game's references, a step of its event
// pictures, an alias key of the statement's database, or a key of a coat of
// arms, tried in that order. A value with a slash names no picture step's
// database. It returns false if the place names no database.
func (w *walker) findPlace(context *Context, statement int32, level int, text string) (string, string, bool) {
	key := w.frames[level].label
	keys := w.frames[level-1].path
	if key != "" {
		keys = slices.Concat(keys, []string{key})
	}

	for _, row := range w.row.ListReferences() {
		if row.Database == "" && w.file.Format == files.FormatGUI && key == row.KeyPath ||
			row.Database != "" && row.Database == context.Database && jomini.MatchKeyPath(row.KeyPath, keys) {
			return cmp.Or(row.Target, row.Database), row.Folder, true
		}
	}

	// a value with a slash is a file's path, which Vic3's video step also
	// takes (events/brazil/amazonas.txt:15)
	if database, ok := w.findPictureDatabase(context.Database, keys); ok && !strings.Contains(text, "/") {
		return database, "", true
	}

	if database := w.schema.Databases[context.Database]; database != nil && (!context.Defines || !context.Definition.Alias) {
		if target, isAlias := database.Row.Aliases[key]; isAlias {
			return cmp.Or(target, database.Name), "", true
		}
	}

	return w.findCoatDatabase(statement, level, text)
}

// findCoatDatabase returns the database that a statement's value names in a
// coat of arms, and the folder of the image it names: a template list after
// a list tag, a named color at a color slot unless it names a slot, and an
// image at a pattern key or at the texture key of an emblem. It returns
// false anywhere else.
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

// findPictureDatabase returns the database whose definition a value names as
// one step of the game's event pictures, given the database the value is
// written in and the keys that lead to it. A step that picks its block by a
// field, or matches the value elsewhere than in a name, names none here. It
// returns false if no step reads the value.
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

// hasNamespace reports whether an events file of the game or of the
// workspace's mods declares the namespace of an event id.
func (w *walker) hasNamespace(id string) bool {
	namespace, _, _ := strings.Cut(id, ".")

	return w.schema.Namespaces[namespace] || w.namespaces[namespace]
}

// allowsUndefined reports whether a reference to the given database needs
// no entry there, as its row says.
func (w *walker) allowsUndefined(label string) bool {
	database := w.schema.Databases[label]

	return database != nil && database.Row.AllowsUndefined
}

// listPrefixed returns the references a chain of links makes after prefixes
// that have a database. A name that is itself a chain of links, as
// scope:overlord is in relations:scope:overlord, makes the references of its
// own prefixes. A name a script saves is the name itself, whatever a link is
// called.
func (w *walker) listPrefixed(chain string, base int) []Reference {
	var references []Reference
	for _, link := range jomini.Links(chain, base) {
		if link.Prefix.IsEmpty() {
			continue
		}

		prefix, label := chain[link.Prefix.Start-base:link.Prefix.End-base], chain[link.Name.Start-base:link.Name.End-base]
		database := w.schema.Prefixes[prefix]
		switch {
		case !slices.Contains(game.Links, database) && w.schema.IsLink(label):
			references = append(references, w.listPrefixed(label, link.Name.Start)...)
		case database != "":
			references = append(references, w.listLabelReferences(database, label, link.Name.Start)...)
		}
	}

	return references
}

// listLabelReferences returns the references a prefixed link's label makes,
// such as the group NAI and the define NAI|X for define:NAI|X.
func (w *walker) listLabelReferences(database, label string, start int) []Reference {
	owner := w.schema.Databases[database]
	if owner == nil || owner.Row.Separator == "" {
		return []Reference{{Database: database, Name: label, Span: jomini.Span{Start: start, End: start + len(label)}}}
	}

	holder, rest, _ := strings.Cut(label, owner.Row.Separator)
	references := []Reference{{Database: database, Name: holder, Span: jomini.Span{Start: start, End: start + len(holder)}}}
	key, _, _ := strings.Cut(rest, owner.Row.Separator)
	if key == "" {
		return references
	}

	keyStart := start + len(holder) + len(owner.Row.Separator)
	for _, nested := range owner.Nested {
		references = append(references, Reference{
			Database: nested, Name: w.schema.JoinNested(nested, holder, key),
			Span: jomini.Span{Start: keyStart, End: keyStart + len(key)},
		})
	}

	return references
}

// judge gives each of the given references its verdict: Inferred is set for
// one that is never an error, whatever the name. That is a reference to no
// database or to a database of names script saves, such as a scope or a
// variable; a call through any caller or as a GUI tag; and a name with a
// prefix or a parameter in it.
func judge(references []Reference) {
	for position := range references {
		reference := &references[position]
		prefixed := slices.ContainsFunc(jomini.Links(reference.Name, 0), func(link jomini.Link) bool {
			return !link.Prefix.IsEmpty()
		})
		reference.Inferred = reference.Inferred || reference.Database == "" || slices.Contains(game.Links, reference.Database) ||
			reference.Caller == game.AnyCall || reference.Caller == game.GUI.TagCalls || prefixed ||
			strings.Contains(reference.Name, "$")
	}
}
