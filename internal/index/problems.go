// problems.go decides the problems of a walked file that need every file.

package index

import (
	"cmp"
	"slices"
	"strings"
	"unicode"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/loc"
	"paradox-modding-tools/internal/schema"
)

// Problem is one thing wrong in a file: one the walk recorded, or one the
// index decides.
type Problem struct {
	schema.Problem

	// Elsewhere is where the whole workspace defines a name that the file's
	// mod and its dependencies do not. It is nil for none.
	Elsewhere *Site
}

// The codes of the problems that need every file.
const (
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
	// index is what the document is judged against, and whole is the same
	// with every mod.
	index, whole *Index

	schema    *schema.Schema
	document  schema.Document
	languages []string
	parse     func(path string) (*jomini.Tree, error)

	// position is the document's file in the index, or -1 if it is not listed.
	position int32

	// copies holds the document's definitions by database and name. The index
	// may hold an older, saved copy of the file.
	copies map[string][]*schema.Copy

	problems []Problem
}

// ListProblems returns the problems of a walked document in the order they
// are written: those its walk recorded and those that need every file. The
// parser reads the other files of a merged definition.
func (i *Index) ListProblems(
	schema *schema.Schema, whole *Index, document schema.Document, languages []string,
	parse func(path string) (*jomini.Tree, error),
) []Problem {
	judge := &judge{
		index: i, whole: whole, schema: schema, document: document, languages: languages, parse: parse, position: -1,
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
		if statement == 0 && context.Definition == nil {
			continue
		}

		judge.judgeRequired(statement, context)
		judge.judgeReferences(context)
		if context.Defines {
			judge.judgeDefinition(statement, context.Definition)
		}

		// a GUI file's text is read as localization is
		value := tree.Statements[statement].Value
		if document.File.Format == files.FormatGUI && strings.Contains(tree.Text(value), "[") {
			judge.judgeCalls(loc.Segments(tree.Unquoted(value), tree.UnquotedSpan(value).Start), context.Datacontext)
		}
	}

	for _, problem := range document.Problems {
		judge.problems = append(judge.problems, Problem{Problem: problem})
	}

	slices.SortStableFunc(judge.problems, func(first, second Problem) int {
		return cmp.Compare(first.Span.Start, second.Span.Start)
	})

	return judge.problems
}

func (j *judge) buildCopies() {
	j.copies = map[string][]*schema.Copy{}
	for position := range j.document.Contexts {
		if context := &j.document.Contexts[position]; context.Defines {
			id := toID(context.Definition)
			j.copies[id] = append(j.copies[id], context.Definition)
		}
	}
}

func toID(definition *schema.Copy) string {
	return schema.Reference{Database: definition.Database, Name: definition.Name}.ToID()
}

func (j *judge) add(code string, span jomini.Span, facts map[string]string) {
	j.problems = append(j.problems, Problem{Code: code, Span: span, Facts: facts})
}

// findKept returns the winner the given index keeps for a reference: its
// message for a localization key, else its definition.
func (j *judge) findKept(index *Index, reference schema.Reference) (Winner, bool) {
	if reference.Database == game.Localization {
		return index.FindText(reference.Name, j.languages)
	}

	return index.FindKeptLocal(j.schema, reference, j.position)
}

// judgeRequired records each key a definition must write and does not. Only a
// source firm enough to accuse makes a key required, and a key a merged copy
// writes is not missing.
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

		facts := schema.DescribeReference(reference)
		facts["key"] = key
		j.add(ProblemRequired, definition.Span, facts)
	}
}

// isWritten reports whether another copy the winner merges writes a key.
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
// defines, except a definition's title key.
func (j *judge) judgeReferences(context *schema.Context) {
	if context.Label == game.TitleKey && context.Parent != nil && context.Parent.Defines {
		return
	}

	j.judgeMissing(context.References)
}

// judgeMissing records each reference whose name nothing defines. References
// at one span are alternatives, recorded only if none is known or inferred.
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

// classifyMissing returns the code for a name nothing defines: the one kind
// all its alternatives share, else an unresolved reference.
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

// isKnown reports whether the game knows a reference's name: one its own
// files use, a customizable localization the dumps name, or a definition in
// the document's file, a file the game reads, or one only another mod's file
// hides.
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

// findElsewhere returns where the whole workspace defines a name the
// document's mod does not. It returns false if the document is judged with
// every mod, or if no alternative is kept in a file the game reads.
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

// judgeDefinition records an entry mode with nothing to act on, a tie for the
// highest override priority, and a title key with no message. A misplaced
// mode, which the walk recorded, is not judged again.
func (j *judge) judgeDefinition(statement int32, definition *schema.Copy) {
	database := j.schema.Databases[definition.Database]
	if database == nil || !definition.IsOwn() {
		return
	}

	tree := j.document.Tree
	reference := schema.Reference{Database: definition.Database, Name: definition.Name}
	facts := schema.DescribeReference(reference)
	misplaced := slices.ContainsFunc(j.document.Problems, func(problem schema.Problem) bool {
		return problem.Code == schema.ProblemMode && problem.Span.End == definition.Span.Start
	})
	switch {
	case definition.Mode != "" && !misplaced && j.isOrphan(reference, definition.Mode):
		facts["mode"] = definition.Mode
		j.add(ProblemOrphan, definition.Span, facts)

	case definition.Mode == "" && !definition.Local && j.isPriorityTied(reference, definition.Priority):
		facts["key"] = database.Precedence.PriorityKey
		j.add(ProblemPriority, definition.Span, facts)
	}

	title, ok := schema.FindTextKey(tree, statement, game.TitleKey)
	if !ok {
		return
	}

	// a title's message counts only in a file the game reads
	key := tree.Unquoted(tree.Statements[title].Value)
	if _, shown := j.index.FindText(key, j.languages); !shown {
		missing := schema.DescribeReference(reference)
		missing["key"] = key
		j.add(ProblemTitle, definition.Span, missing)
	}
}

// isOrphan reports whether a definition written with the given entry mode has
// nothing to change. An optional or creating mode never has.
func (j *judge) isOrphan(reference schema.Reference, prefix string) bool {
	mode, _ := game.LookupEntryMode(prefix)
	if mode.Optional || mode.Creates {
		return false
	}

	_, acts := j.findKept(j.index, reference)

	return !acts
}

// isPriorityTied reports whether a plain definition shares the highest
// override priority with other copies. It is false for a file the game does
// not read.
func (j *judge) isPriorityTied(reference schema.Reference, priority int) bool {
	if j.position < 0 || !j.index.IsLoaded(j.position) {
		return false
	}

	winner, ok := j.findKept(j.index, reference)

	return ok && winner.Cause == game.CausePriority && int(j.index.Occurrence(winner.Site).Priority) == priority
}

// judgeMessages records the problems of a localization document: no byte
// order mark, unknown names and data calls, and a key written twice in the
// file's language outside a replace folder.
func (j *judge) judgeMessages() {
	messages, file := j.document.Messages, j.document.File
	if j.index.Unmarked[j.position] {
		j.add(ProblemBOM, jomini.Span{}, map[string]string{})
	}

	// the index holds keys of the given languages only, so a key named in
	// another language is not judged
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
			facts := schema.DescribeReference(schema.Reference{Database: game.Localization, Name: message.Key})
			facts["language"] = messages.Language
			j.add(ProblemDuplicate, message.KeySpan, facts)
		}
	}
}

// judgeCalls records each data call with a step unknown after the type
// before it. A first step in lower case, or one a file saves a scope by, is a
// saved scope. A step the game's own texts write, or a database a step may
// name, is no problem.
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

		facts := schema.DescribeReference(schema.Reference{Name: label})
		facts["type"] = before
		j.add(ProblemDataCall, call.Steps[step].Span, facts)
	}
}
