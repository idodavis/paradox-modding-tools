// problems.go decides the problems of a walked file: what its contexts and
// the schema say is wrong with it, and what the index's definitions, winners
// and messages say.

package index

import (
	"cmp"
	"maps"
	"slices"
	"strings"
	"unicode"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/loc"
	"paradox-modding-tools/internal/schema"
)

// Problem is one thing wrong in a file, as the engine decides it. Whoever
// shows it gives it its level and its words.
type Problem struct {
	Code string
	Span jomini.Span

	// Facts holds what the problem is about, by fact: the name, its database
	// in words, and whatever else the code's sentence names.
	Facts map[string]string

	// Expected is what the value of a ProblemValue must be.
	Expected schema.Expected

	// Elsewhere is the definition the whole workspace supplies for a name
	// that the file's mod, and the mods it depends on, do not define. It is
	// nil for none.
	Elsewhere *Site
}

// The codes of the problems. The first eight need the file alone, and the
// rest need every file.
const (
	ProblemUnknownKey = "unknown-key"
	ProblemScope      = "wrong-scope"
	ProblemValue      = "wrong-value"
	ProblemDate       = "invalid-date"
	ProblemFollows    = "else-without-if"
	ProblemNamespace  = "wrong-namespace"
	ProblemMode       = "misplaced-mode"
	ProblemArgument   = "unused-argument"

	ProblemUnresolved = "unresolved-reference"
	ProblemLocKey     = "unknown-loc-key"
	ProblemMarkup     = "loc-markup"
	ProblemDuplicate  = "duplicate-message"
	ProblemRequired   = "missing-key"
	ProblemOrphan     = "inject-without-target"
	ProblemPriority   = "conflicting-priority"
	ProblemTitle      = "missing-message"
	ProblemDataCall   = "unknown-data-call"
	ProblemBOM        = "missing-bom"
)

// judge collects the problems of one document.
type judge struct {
	// index is the index the document is judged against, and whole the same
	// index with every mod of the workspace.
	index, whole *Index

	schema    *schema.Schema
	document  schema.Document
	languages []string
	parse     func(path string) (*jomini.Tree, error)

	// position is the document's file in the index. It is -1 for a file the
	// index does not list.
	position int32

	// copies holds the definitions in the document, by database and name.
	// The index may hold an older, saved copy of the file.
	copies map[string][]*schema.Copy

	// previous holds, by block, the statement last judged in it. It is 0 for
	// a block nothing was judged in yet.
	previous []int32

	// parameters holds the parameters of each definition of the document
	// read so far, by the definition's statement.
	parameters map[int32]*schema.Field

	problems []Problem
}

// ListProblems returns the problems of a walked document, in the tree's
// order.
//
// The file alone decides a key its block does not take, a command or a link
// in a scope the dumps do not list for it, a value its field does not take,
// a date the game cannot have, a keyword that does not follow what it must,
// an event outside its file's namespaces, an entry mode written where its
// database reads none, and a call whose arguments and its callee's
// parameters differ.
//
// Every file decides a name nothing defines, a localization key written
// twice in one language where the game logs it, a key every definition must
// write that no merged copy writes, an entry mode with nothing to act on, a
// tie for the highest override priority, a title with no text in the given
// languages, a data call no dump and no file knows, and a localization file
// read with no byte order mark.
//
// The index is the one the document's file sees: narrowed to its mod and
// the mods that one depends on. The whole index holds every mod, and says
// where a name the file's mod lacks is defined after all. The parser reads
// the other files a merged definition is written in. The file's own context
// is judged only where the file is one definition.
func (i *Index) ListProblems(
	schema *schema.Schema, whole *Index, document schema.Document, languages []string,
	parse func(path string) (*jomini.Tree, error),
) []Problem {
	judge := &judge{
		index: i, whole: whole, schema: schema, document: document, languages: languages, parse: parse, position: -1,
		previous: make([]int32, len(document.Contexts)),
	}
	if position, ok := i.FindFile(document.File.Path); ok {
		judge.position = position
	}

	if document.Messages != nil {
		judge.judgeMessages()

		return judge.problems
	}

	judge.buildCopies()
	tree := document.Tree
	for position := range document.Contexts {
		statement, context := int32(position), &document.Contexts[position]
		if statement > 0 || context.Definition != nil {
			judge.judgeRequired(statement, context)
			judge.judgeReferences(context)
			judge.judgeKey(statement, context)
			judge.judgeScope(statement, context)
			judge.judgeValue(statement, context)
			judge.judgeDate(statement, context)
			judge.judgeCall(statement, context)

			// a GUI file's text is read as localization is, in the data
			// context of its block
			value := tree.Statements[statement].Value
			if document.File.Format == files.FormatGUI && strings.Contains(tree.Text(value), "[") {
				judge.judgeCalls(loc.Segments(tree.Unquoted(value), tree.UnquotedSpan(value).Start), context.Datacontext)
			}
		}

		if statement > 0 {
			judge.previous[tree.Statements[statement].Parent] = statement
		}
	}

	return judge.problems
}

// buildCopies lists the definitions the document writes, by database and
// name.
func (j *judge) buildCopies() {
	j.copies, j.parameters = map[string][]*schema.Copy{}, map[int32]*schema.Field{}
	for position := range j.document.Contexts {
		if context := &j.document.Contexts[position]; context.Defines {
			id := toID(context.Definition)
			j.copies[id] = append(j.copies[id], context.Definition)
		}
	}
}

// toID returns the id of the name a definition defines: its database and
// its name.
func toID(definition *schema.Copy) string {
	return schema.Reference{Database: definition.Database, Name: definition.Name}.ToID()
}

// DescribeReference returns the facts that say which name a problem or a
// finding is about: the name, and its database in words.
func DescribeReference(reference schema.Reference) map[string]string {
	return map[string]string{"name": reference.Name, "database": game.DescribeDefinition(reference.Database, false)}
}

// add records a problem at a span.
func (j *judge) add(code string, span jomini.Span, facts map[string]string) {
	j.problems = append(j.problems, Problem{Code: code, Span: span, Facts: facts})
}

// findKept returns the winner the given index keeps for a reference, as the
// document's file sees it: the message shown for a localization key, else
// the kept definition.
func (j *judge) findKept(index *Index, reference schema.Reference) (Winner, bool) {
	if reference.Database == game.Localization {
		return index.FindText(reference.Name, j.languages)
	}

	return index.FindKeptLocal(j.schema, reference, j.position)
}

// judgeRequired records each key a definition must write and does not, in
// alphabetical order. A key is required where a firm source says so; the
// count has taken that claim back where a definition the game ships leaves
// the key out. A key another copy merged into the kept one writes is not
// missing.
func (j *judge) judgeRequired(statement int32, context *schema.Context) {
	definition := context.Definition
	if !context.Defines || definition.Mode != "" {
		return
	}

	database := j.schema.Databases[definition.Database]
	if database == nil || context.Field != database.Field {
		return
	}

	var missing []string
	for key, field := range context.Field.Children {
		required := field.Required.Value == schema.RequirementAlways && field.Required.Source.CanAccuse()
		_, written := j.document.Tree.FindChild(statement, key)
		if required && !written && !strings.HasPrefix(key, "<") {
			missing = append(missing, key)
		}
	}

	if len(missing) == 0 {
		return
	}

	reference := schema.Reference{Database: definition.Database, Name: definition.Name}
	winner, _ := j.findKept(j.index, reference)
	slices.Sort(missing)
	for _, key := range missing {
		if j.isWritten(winner, definition, key) {
			continue
		}

		facts := DescribeReference(reference)
		facts["key"] = key
		j.add(ProblemRequired, definition.Span, facts)
	}
}

// isWritten reports whether another copy that the winner merges writes a
// key. Copies in other files are read with the parser, and copies in the
// document from its own tree.
func (j *judge) isWritten(winner Winner, definition *schema.Copy, key string) bool {
	mergesOwn := false
	for _, site := range winner.Merged {
		if site.File == j.position {
			mergesOwn = true

			continue
		}

		occurrence := j.index.Occurrence(site)
		tree, err := j.parse(j.index.File(site).Path)
		if err != nil || int(occurrence.Statement) >= len(tree.Statements) {
			continue
		}

		if _, written := tree.FindChild(occurrence.Statement, key); written {
			return true
		}
	}

	return mergesOwn && slices.ContainsFunc(j.copies[toID(definition)], func(other *schema.Copy) bool {
		_, written := j.document.Tree.FindChild(other.Statement, key)

		return other != definition && written
	})
}

// judgeReferences records each name a statement refers to that nothing
// defines. A definition's title key is ProblemTitle's.
func (j *judge) judgeReferences(context *schema.Context) {
	if context.Label == game.TitleKey && context.Parent != nil && context.Parent.Defines {
		return
	}

	j.judgeMissing(context.References)
}

// judgeMissing records each of the given references whose name nothing
// defines. References at one span are alternatives, recorded only if none is
// defined and none is inferred. Where the whole workspace defines the name
// after all, the problem says where.
func (j *judge) judgeMissing(references []schema.Reference) {
	for position, reference := range references {
		atSpan := func(other schema.Reference) bool { return other.Span == reference.Span }
		if slices.ContainsFunc(references[:position], atSpan) {
			continue
		}

		var alternatives []schema.Reference
		var databases []string
		settled := false
		for _, other := range references {
			if !atSpan(other) {
				continue
			}

			alternatives = append(alternatives, other)
			settled = settled || other.Inferred || j.isKnown(other)
			if phrase := game.DescribeDefinition(other.Database, false); !slices.Contains(databases, phrase) {
				databases = append(databases, phrase)
			}
		}

		if settled {
			continue
		}

		code := classifyMissing(alternatives)
		facts := map[string]string{"name": reference.Name, "database": game.JoinList(databases, "or")}
		if code == ProblemMarkup {
			facts["why"] = "no file declares the " + facts["database"] + " " + reference.Name
		}

		problem := Problem{Code: code, Span: reference.Span, Facts: facts}
		if site, ok := j.findElsewhere(alternatives); ok {
			problem.Elsewhere = &site
		}

		j.problems = append(j.problems, problem)
	}
}

// classifyMissing returns the code for a name nothing defines, from the
// databases of its alternative references: an unknown localization key or
// unknown text markup if every alternative is one, else an unresolved
// reference.
func classifyMissing(alternatives []schema.Reference) string {
	code := ""
	for position, reference := range alternatives {
		kind := ProblemUnresolved
		switch reference.Database {
		case game.Localization:
			kind = ProblemLocKey
		case game.GUI.TextIcons, game.GUI.TextFormats:
			kind = ProblemMarkup
		}

		if position > 0 && kind != code {
			return ProblemUnresolved
		}

		code = kind
	}

	return code
}

// isKnown reports whether the game knows a reference's name. A name the
// game's own files use is known, and so is a customizable localization the
// dumps name. A definition counts in the document's own file, in a file the
// game reads, and in a file that only another mod's file hides.
func (j *judge) isKnown(reference schema.Reference) bool {
	if reference.Database == game.CustomLocalization && j.schema.Customs[reference.Name] {
		return true
	}

	if reference.Database == game.File {
		if _, ok := j.index.ResolveTexture(reference.Name); ok {
			return true
		}

		return j.index.IsUsedByGame(reference.Database, reference.Name)
	}

	sites := j.index.ResolveLocal(reference, j.position)
	if len(sites) == 0 {
		return j.index.IsUsedByGame(reference.Database, reference.Name)
	}

	return slices.ContainsFunc(sites, func(site Site) bool {
		file := j.index.File(site)
		hiding, _, ok := j.index.FindLoaded(file.Rel)

		return file.Key == j.document.File.Key || j.index.IsLoaded(site.File) || ok && hiding.Origin != j.document.File.Origin
	})
}

// findElsewhere returns the definition the whole workspace supplies for a
// name the document's mod does not define. It returns false if the document
// is judged with every mod, or if no alternative has a kept copy in a file
// the game reads.
func (j *judge) findElsewhere(alternatives []schema.Reference) (Site, bool) {
	if j.whole == j.index {
		return Site{}, false
	}

	for _, reference := range alternatives {
		winner, ok := j.findKept(j.whole, reference)
		if ok && j.whole.IsLoaded(winner.Site.File) {
			return winner.Site, true
		}
	}

	return Site{}, false
}

// judgeKey records what is wrong with the key of a statement. The first
// rule that fits the key decides.
func (j *judge) judgeKey(statement int32, context *schema.Context) {
	current := j.document.Tree.Statements[statement]
	switch {
	case context.Defines:
		j.judgeDefinition(statement, context.Definition)

	case context.Category == schema.CategoryKeyword:
		j.judgeFollows(statement, context)

	// a key with a meaning of its own is not an unknown key
	case current.Key.IsEmpty() || context.Category != schema.CategoryNone || context.Field != nil:

	case current.Parent == 0 && current.KeyClass == jomini.ClassEventID:
		j.judgeNamespace(current, context)

	case context.Definition != nil:
		j.judgeUnknown(statement, context)
	}
}

// judgeDefinition records what is wrong with a definition of its own: an
// entry mode written where its database reads none, else one with nothing
// to act on, else a tie for the highest override priority; and a title key
// with no message the game reads in the given languages. A database reads an
// entry mode at the top of a file or inside a wrapper, by its precedence.
func (j *judge) judgeDefinition(statement int32, definition *schema.Copy) {
	database := j.schema.Databases[definition.Database]
	if database == nil || !definition.IsOwn() {
		return
	}

	tree, precedence := j.document.Tree, database.Precedence
	reference := schema.Reference{Database: definition.Database, Name: definition.Name}
	facts := DescribeReference(reference)
	misplaced := precedence.Modes != "" && !precedence.ReadsMode(tree.Statements[statement].Parent == 0)
	switch {
	case definition.Mode != "" && misplaced:
		facts["mode"] = definition.Mode
		mode := jomini.Span{Start: definition.Span.Start - len(definition.Mode) - 1, End: definition.Span.Start}
		j.add(ProblemMode, mode, facts)

	case definition.Mode != "" && j.isOrphan(reference, definition.Mode):
		facts["mode"] = definition.Mode
		j.add(ProblemOrphan, definition.Span, facts)

	case definition.Mode == "" && !definition.Local && j.isPriorityTied(reference, definition.Priority):
		facts["key"] = precedence.PriorityKey
		j.add(ProblemPriority, definition.Span, facts)
	}

	title, ok := schema.FindTextKey(tree, statement, game.TitleKey)
	if !ok {
		return
	}

	// a title's message counts only in a file the game reads, in one of the
	// languages every text is looked up in
	key := tree.Unquoted(tree.Statements[title].Value)
	if _, shown := j.index.FindText(key, j.languages); !shown {
		missing := DescribeReference(reference)
		missing["key"] = key
		j.add(ProblemTitle, definition.Span, missing)
	}
}

// isOrphan reports whether a definition written with the given entry mode
// has nothing to change. An optional mode, or one that creates, never has.
func (j *judge) isOrphan(reference schema.Reference, prefix string) bool {
	mode, _ := game.LookupEntryMode(prefix)
	if mode.Optional || mode.Creates {
		return false
	}

	_, acts := j.findKept(j.index, reference)

	return !acts
}

// isPriorityTied reports whether a plain definition with the given override
// priority is one of several copies that share the highest, so that no
// source says which the game keeps. The index decides the tie when it picks
// the winner, from the saved files. It is false for a file the game does
// not read.
func (j *judge) isPriorityTied(reference schema.Reference, priority int) bool {
	if j.position < 0 || !j.index.IsLoaded(j.position) {
		return false
	}

	winner, ok := j.findKept(j.index, reference)

	return ok && winner.Cause == game.CausePriority && int(j.index.Occurrence(winner.Site).Priority) == priority
}

// judgeFollows records a keyword such as else that does not come right
// after one of the keys it must follow.
func (j *judge) judgeFollows(statement int32, context *schema.Context) {
	tree := j.document.Tree
	current, follows := tree.Statements[statement], context.Keyword.Follows
	previous := j.previous[current.Parent]
	if len(follows) == 0 ||
		previous > 0 && slices.Contains(follows, strings.ToLower(tree.Unquoted(tree.Statements[previous].Key))) {
		return
	}

	facts := DescribeReference(schema.Reference{Name: tree.Unquoted(current.Key)})
	facts["follows"] = game.JoinList(follows, "or")
	j.add(ProblemFollows, current.Key, facts)
}

// judgeNamespace records an event at the top of an events file that is
// outside the file's namespaces.
func (j *judge) judgeNamespace(current jomini.Statement, context *schema.Context) {
	tree := j.document.Tree
	database, ok := j.schema.FindDatabase(j.document.File)
	if !ok || database.Declaration.Contents != game.ContentsEvents {
		return
	}

	namespaces := make([]string, len(context.Parent.Namespaces))
	for position, statement := range context.Parent.Namespaces {
		namespaces[position] = tree.Unquoted(tree.Statements[statement].Value)
	}

	facts := DescribeReference(schema.Reference{Database: database.Name, Name: tree.Unquoted(current.Key)})
	facts["namespace"] = cmp.Or(strings.Join(namespaces, ", "), "none")
	j.add(ProblemNamespace, current.Key, facts)
}

// judgeUnknown records a key its block does not take. Only a plain name is
// judged, in a block whose keys a firm source lists and the game's own files
// never write.
func (j *judge) judgeUnknown(statement int32, context *schema.Context) {
	tree := j.document.Tree
	current, holder := tree.Statements[statement], context.Parent
	if j.schema.Databases[context.Database] == nil || !current.KeyClass.IsIdentifier() {
		return
	}

	if holder.Field == nil || holder.Shape == game.ShapeUnknown || !holder.Field.Source.CanAccuse() ||
		holder.Field.Vanilla.Count > 0 {
		return
	}

	facts := DescribeReference(schema.Reference{Database: context.Database, Name: tree.Unquoted(current.Key)})
	j.add(ProblemUnknownKey, current.Key, facts)
}

// judgeScope records a command written in a scope the dumps do not list for
// it, and each link of a chain written in one: the scope the chain is
// written in for its first link, and the scope the link before it leads to
// for every other. A scope the definition starts in is stated, not followed,
// so only a scope that script moved to counts. The block must hold effects
// or triggers and no arguments.
func (j *judge) judgeScope(statement int32, context *schema.Context) {
	holder := context.Parent
	if context.Category != schema.CategoryLink && context.Category != schema.CategoryCommand || holder == nil {
		return
	}

	if holder.Shape != game.ShapeEffects && holder.Shape != game.ShapeTriggers || holder.HoldsArguments() {
		return
	}

	tree := j.document.Tree
	span := tree.UnquotedSpan(tree.Statements[statement].Key)
	scope, followed := holder.Scope, len(holder.Previous) > 0
	if context.Category == schema.CategoryCommand {
		j.judgeCommandScope(span, context.Command, scope, followed)

		return
	}

	key := tree.Text(span)
	links := jomini.Links(key, span.Start)
	for position, link := range j.schema.ResolveChain(key, holder, context.Saved) {
		j.judgeCommandScope(links[position].Span, link.Definition, scope, followed)
		scope, followed = link.Scope, true
	}
}

// judgeCommandScope records a command or a link, written at the given span,
// that the dumps do not list for the scope it is written in. It records none
// for a name the dumps do not declare or list no scope for, for a scope that
// is not known, and for one that script did not move to.
func (j *judge) judgeCommandScope(span jomini.Span, command *schema.Definition, scope string, followed bool) {
	if command == nil || len(command.Scopes) == 0 || scope == "" || !followed || slices.Contains(command.Scopes, scope) {
		return
	}

	facts := DescribeReference(schema.Reference{Name: j.document.Tree.Text(span)})
	facts["scope"], facts["allowed"] = scope, strings.Join(command.Scopes, ", ")
	j.add(ProblemScope, span, facts)
}

// judgeValue records a value that is not the type its key takes, where a
// firm source states the type and every value the game's own files write
// fits it. A reference, a name where a number is expected, a path, free
// text and a block are not judged.
func (j *judge) judgeValue(statement int32, context *schema.Context) {
	tree := j.document.Tree
	field, current := context.Field, tree.Statements[statement]
	categories := []schema.Category{schema.CategoryField, schema.CategoryReference, schema.CategoryCommand}
	if !slices.Contains(categories, context.Category) || field == nil || !field.Value.IsStated() ||
		!field.Value.Source.CanAccuse() || current.IsBlock() || current.Value.IsEmpty() ||
		context.ValueGroup == schema.CategoryReference {
		return
	}

	expected, text := field.Value.Value, tree.Text(current.Value)
	if expected.Database == game.File || expected.Type == schema.ValueTypeText || expected.Type == schema.ValueTypeBlock {
		return
	}

	// the game reads a script value, or a trigger that returns a number,
	// where a number is expected
	if expected.Type == schema.ValueTypeNumber && current.ValueClass.IsIdentifier() || strings.ContainsAny(text, "[$@") ||
		j.schema.MatchValue(expected, text) {
		return
	}

	for value := range field.Vanilla.Values {
		if !j.schema.MatchValue(expected, value) {
			return
		}
	}

	j.problems = append(j.problems, Problem{
		Code: ProblemValue, Span: current.Value, Expected: expected,
		Facts: DescribeReference(schema.Reference{Name: tree.Unquoted(current.Key)}),
	})
}

// judgeDate records a date the game cannot have: a key written as
// year.month.day, or the value of a field a firm source says takes a date. A
// key of two numbers is a number, not a date.
func (j *judge) judgeDate(statement int32, context *schema.Context) {
	tree := j.document.Tree
	current := tree.Statements[statement]
	var spans []jomini.Span
	if current.KeyClass != jomini.ClassNumber {
		spans = append(spans, current.Key)
	}

	field := context.Field
	dated := field != nil && field.Value.Value.Type == schema.ValueTypeDate && field.Value.Source.CanAccuse()
	if dated && !current.IsBlock() {
		spans = append(spans, current.Value)
	}

	for _, span := range spans {
		if date, ok := jomini.ParseDate(tree.Unquoted(span)); ok && !date.IsValid() {
			j.add(ProblemDate, span, DescribeReference(schema.Reference{Name: tree.Unquoted(span)}))
		}
	}
}

// judgeCall records the arguments a call of a scripted definition gives that
// its body does not read, and the parameters with no default that the call
// does not give. A call inside another scripted definition counts that
// definition's parameters as given, since they reach it.
func (j *judge) judgeCall(statement int32, context *schema.Context) {
	tree := j.document.Tree
	current := tree.Statements[statement]
	callee := tree.Unquoted(current.Key)
	parameters := j.listCalleeParameters(callee, context.Callee)
	if parameters == nil {
		return
	}

	enclosing := &schema.Field{}
	if context.Definition != nil {
		enclosing = j.listParameters(context.Definition.Statement)
	}

	add := func(span jomini.Span, parameter, why string) {
		facts := DescribeReference(schema.Reference{Name: parameter})
		facts["why"], facts["callee"] = why, callee
		j.add(ProblemArgument, span, facts)
	}
	given := map[string]bool{}
	if current.IsBlock() {
		for child := range tree.Children(statement) {
			key := tree.Statements[child].Key
			given[tree.Unquoted(key)] = true
			if _, takes := parameters.Children[tree.Unquoted(key)]; !takes && !key.IsEmpty() {
				add(key, tree.Unquoted(key), "is never read by")
			}
		}
	}

	for _, parameter := range slices.Sorted(maps.Keys(parameters.Children)) {
		_, inherited := enclosing.Children[parameter]
		if !given[parameter] && !inherited && !parameters.Children[parameter].Default.IsStated() {
			add(current.Key, parameter, "is not given to")
		}
	}
}

// listCalleeParameters returns the parameters of the scripted definition a
// call names: those of the document's own copy as its text now writes them,
// else those of the copy the game keeps, which the walk gave the call. It
// returns nil if the call names no scripted definition that takes
// parameters.
func (j *judge) listCalleeParameters(key string, callee *schema.Definition) *schema.Field {
	if callee == nil || callee.Field == nil {
		return nil
	}

	own := j.copies[schema.Reference{Database: callee.Database, Name: key}.ToID()]
	if len(own) > 0 {
		return j.listParameters(own[0].Statement)
	}

	return callee.Field
}

// listParameters returns the parameters that the definition written at a
// statement of the document reads. A definition is read once.
func (j *judge) listParameters(statement int32) *schema.Field {
	parameters, ok := j.parameters[statement]
	if !ok {
		parameters = schema.ListParameters(j.document.Tree, statement)
		j.parameters[statement] = parameters
	}

	return parameters
}

// judgeMessages records the problems of a localization document: a file
// read with no byte order mark, the names and the data calls each message's
// text writes, and a key written twice in the file's language outside a
// replace folder.
func (j *judge) judgeMessages() {
	messages, file := j.document.Messages, j.document.File
	if j.index.Unmarked[j.position] {
		j.add(ProblemBOM, jomini.Span{}, map[string]string{})
	}

	// the index holds the game's keys in the given languages only, so a key
	// that a file in any other language names is not judged
	held := slices.Contains(j.languages, messages.Language)
	for position, message := range messages.Messages {
		references := j.document.MessageContexts[position].References
		if !held {
			references = slices.DeleteFunc(slices.Clone(references), func(reference schema.Reference) bool {
				return reference.Database == game.Localization
			})
		}

		j.judgeMissing(references)
		j.judgeCalls(message.Segments, "")
	}

	row := j.index.row
	if _, replaces := row.FindLocReplacement(file.Rel, messages.Language); replaces {
		return
	}

	counts := map[string]int{}
	for _, message := range messages.Messages {
		counts[message.Key]++
	}

	for _, message := range messages.Messages {
		count := counts[message.Key]
		for _, site := range j.index.ListMessages(message.Key) {
			other := j.index.File(site)
			_, replaces := row.FindLocReplacement(other.Rel, messages.Language)
			repeats := other.Origin == file.Origin || other.Origin == files.OriginVanilla && row.LogsRepeatedKey
			if site.File != j.position && repeats && !replaces && j.index.Language(site) == messages.Language &&
				j.index.IsLoaded(site.File) {
				count++
			}
		}

		if count > 1 {
			facts := DescribeReference(schema.Reference{Database: game.Localization, Name: message.Key})
			facts["language"] = messages.Language
			j.add(ProblemDuplicate, message.KeySpan, facts)
		}
	}
}

// judgeCalls records each data call in the segments of a text that has a
// step unknown after the type before it, read in the given data context. A
// first step in lower case, or one a file saves a scope by, is a saved
// scope and no problem. Neither is a step the game's own texts write, or a
// name of one of the databases a step may name.
func (j *judge) judgeCalls(segments []loc.Segment, datacontext string) {
	for _, segment := range segments {
		call := segment.Call
		if call == nil {
			continue
		}

		step, before, ok := j.schema.FindUnlisted(*call, datacontext)
		if !ok {
			continue
		}

		label := call.Steps[step].Text
		isDefined := func(database string) bool {
			return len(j.index.Resolve(schema.Reference{Database: database, Name: label})) > 0
		}
		saved := step == 0 && label != "" && (unicode.IsLower(rune(label[0])) || isDefined(game.SavedScopes))
		if saved || j.schema.UnlistedCalls[before+"."+label] || slices.ContainsFunc(game.GUI.StepDatabases, isDefined) {
			continue
		}

		facts := DescribeReference(schema.Reference{Name: label})
		facts["type"] = before
		j.add(ProblemDataCall, call.Steps[step].Span, facts)
	}
}
