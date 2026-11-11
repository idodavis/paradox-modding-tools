// complete.go lists what can be written at a position in a file: keys,
// keywords, commands and values.

package lsp

import (
	"cmp"
	"maps"
	"slices"
	"strings"

	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/schema"
	"paradox-modding-tools/services/workspace"
)

// A Completion is one candidate in the completion list.
type Completion struct {
	Label    string          `json:"label"`
	Insert   string          `json:"insert"`           // the text inserted; the label where equal
	Category schema.Category `json:"category"`         // what it is, for the icon and the color
	Detail   string          `json:"detail,omitempty"` // the database's singular, the command's class
	Blurb    string          `json:"blurb,omitempty"`  // the one line shown beside it
	Rank     int             `json:"rank"`             // how often the game writes it there, for the order
	Snippet  bool            `json:"snippet"`          // Insert is a snippet with placeholders
	Image    string          `json:"image,omitempty"`  // the full path of the image file it names; "" if it names none
}

// A CompletionList is what can be written at a position in a file.
type CompletionList struct {
	Candidates []Completion `json:"candidates"`

	// Cut is whether a limit left candidates out. The editor asks again as
	// typing narrows a list that is cut, and filters any other itself.
	Cut bool `json:"cut"`
}

// keyLimit is the most localization keys a completion lists.
const keyLimit = 200

// commandDatabases are the databases of the dumps' commands that a block may
// take as keys, in the order a name is looked up in them.
var commandDatabases = []string{game.DumpEffects, game.DumpTriggers}

// Complete returns the completion candidates at a position in a file: at a
// key the language's keywords, commands and fields, at a value what may be
// written there. A word written alone in a block may be a value or a key
// typed before its equals sign, so it gets both. It returns an empty list for
// a file that is not script.
func (lsp *Service) Complete(path string, line, column int) (CompletionList, error) {
	ws, document, err := lsp.loadDocument(path)
	if err != nil {
		return CompletionList{}, err
	}

	tree, mods := document.Tree, ws.Mods.Load()
	place := ws.Locate(mods, path)
	list := CompletionList{Candidates: []Completion{}}
	if document.Contexts == nil {
		return list, nil
	}

	offset := ToOffset(tree.Src, tree.Lines, line, column)
	statement, listed, ok := findValue(tree, offset)
	if ok {
		current := tree.Statements[statement]
		typed := ""
		if !current.Value.IsEmpty() && offset >= current.Value.Start {
			typed = tree.Src[current.Value.Start:offset]
		}

		context := &document.Contexts[statement]
		list.Candidates, list.Cut = listValues(ws, mods, place.Position, tree, context, typed, offset)
	}

	if ok && !listed {
		return list, nil
	}

	// the keys are those of the innermost block that holds the offset
	holder := tree.FindStatement(offset)
	for holder > 0 && (offset <= tree.Statements[holder].Block.Start || offset >= tree.Statements[holder].Block.End) {
		holder = tree.Statements[holder].Parent
	}

	if holder == 0 || document.Contexts[holder].Shape == "" {
		return list, nil
	}

	keys := listKeys(mods, place.Position, ws.Schema, ws.Game, &document.Contexts[holder])
	list.Candidates = dropRepeats(slices.Concat(keys, list.Candidates))

	return list, nil
}

// findValue returns the statement whose value the given offset is writing,
// and whether that value is written alone in a block, as a listed name or a
// key typed before its equals sign is. It returns false if the offset is not
// writing a value.
func findValue(tree *jomini.Tree, offset int) (int32, bool, bool) {
	prefix := strings.TrimRight(tree.Src[:offset], " \t")
	if prefix == "" {
		return 0, false, false
	}

	statement := tree.FindStatement(len(prefix) - 1)
	current := tree.Statements[statement]
	if current.IsListed() && current.Parent != 0 {
		return statement, true, offset >= current.Value.Start && offset <= current.Value.End
	}

	if current.IsBlock() || current.Op.IsEmpty() || offset < current.Op.End {
		return 0, false, false
	}

	pastLine := strings.Contains(tree.Src[current.Op.End:offset], "\n")
	pastValue := !current.Value.IsEmpty() && offset > current.Value.End

	return statement, false, !pastLine && !pastValue
}

// listValues returns the values that may be written at a statement's value in
// one file of the index, given the text typed so far, with the quote it may
// begin with, and the offset it ends at. A call takes what its database's row
// says, a field yes and no or its choices, and a value the walk found a
// reference in takes the names of that reference's database. Where none of
// these apply, it offers the values the game's own files write there. A
// target also takes the links and the link prefixes that lead to the scope it
// needs. It returns an empty list if nothing is known of the value. It also
// reports whether a limit cut the names of a database short.
func listValues(
	ws *workspace.Session, mods *index.Index, file int32, tree *jomini.Tree, context *schema.Context, typed string,
	offset int,
) ([]Completion, bool) {
	expected, vanilla := findExpected(context)
	quoted := strings.HasPrefix(typed, `"`)
	typed = strings.TrimPrefix(typed, `"`)
	start := offset - len(typed)
	rank := func(value string) int { return max(vanilla[value], vanilla[`"`+value+`"`]) }
	makeConstant := func(value string) Completion {
		insert := value
		if !quoted && strings.Contains(value, " ") {
			insert = `"` + value + `"`
		}

		return Completion{Label: value, Insert: insert, Category: schema.CategoryConstant, Rank: rank(value)}
	}

	var constants []string
	switch {
	case context.Callee != nil:
		constants = game.Callables[context.Callee.Database].Values
	case expected.Type == schema.ValueTypeBool:
		constants = game.Bools
	case expected.Type == schema.ValueTypeChoice:
		constants = expected.Choices
	}

	values := []Completion{}
	for _, constant := range constants {
		values = append(values, makeConstant(constant))
	}

	cut := false
	for _, reference := range listTyped(ws.Schema, context, expected, start, offset) {
		// the text before the reference, such as a link's prefix, is written
		// before each of its names
		head := typed[:reference.Span.Start-start]
		names, namesCut := listNames(ws, mods, file, reference, tree.Text(reference.Span))
		cut = cut || namesCut
		for _, candidate := range names {
			candidate.Label = head + candidate.Label
			candidate.Insert, candidate.Rank = candidate.Label, rank(candidate.Label)
			values = append(values, candidate)
		}
	}

	if len(values) == 0 {
		values = listWritten(vanilla, makeConstant)
	}

	if expected.Type == schema.ValueTypeTarget && expected.Scope != "" {
		isScope := func(scope string) bool { return scope == expected.Scope }
		values = append(values, listPrefixes(ws, expected.Scope)...)
		values = append(values, listScopeLinks(context, isScope, rank)...)
	}

	return dropRepeats(values), cut
}

// dropRepeats returns the candidates with each label once, the first of each
// kept.
func dropRepeats(candidates []Completion) []Completion {
	listed := map[string]bool{}
	kept := []Completion{}
	for _, candidate := range candidates {
		if !listed[candidate.Label] {
			listed[candidate.Label] = true
			kept = append(kept, candidate)
		}
	}

	return kept
}

// findExpected returns what a statement's value must be, and how often the
// game's own files write each value there. The value of a link is a target of
// the scope the link leads to, and the field a link has is its block's, so it
// gives no counts. Both are empty for a statement no source knows.
func findExpected(context *schema.Context) (schema.Expected, map[string]int) {
	switch {
	case context.Category == schema.CategoryLink:
		return schema.Expected{Type: schema.ValueTypeTarget, Scope: context.Scope}, nil
	case context.Field == nil:
		return schema.Expected{}, nil
	}

	return context.Field.Value.Value, context.Field.Vanilla.Values
}

// listTyped returns the references the walk found in a value that start
// between the given offsets, where the typed text starts and where it ends. A
// saved scope is left out, since only the scopes saved earlier in the
// definition may be named there, and so is a use of a name the dumps
// declare, which the links and the commands are listed for. Before anything
// is typed the walk has read no value, so the database the field claims
// stands in for it. It returns none if the field claims no database either.
func listTyped(language *schema.Schema, context *schema.Context, expected schema.Expected, start, offset int) []schema.Reference {
	references := slices.DeleteFunc(slices.Clone(context.References), func(reference schema.Reference) bool {
		return reference.Span.Start < start || reference.Span.Start > offset ||
			reference.Database == game.SavedScopes || language.ListKeys(reference.Database) != nil
	})
	claims := expected.Type == schema.ValueTypeReference || expected.Type == schema.ValueTypeTarget
	if start == offset && len(references) == 0 && claims && expected.Database != "" {
		references = []schema.Reference{{Database: expected.Database, Span: jomini.Span{Start: offset, End: offset}}}
	}

	return references
}

// listNames returns the names of a reference's database that one file of the
// index sees, each written the way the given text writes the reference: what
// the walk put around that text to make the name, such as an image's folder,
// is left off. Localization keys are those that contain the reference's name,
// up to keyLimit with a key that is the name first, and images those under
// its folder. A definition local to another file is left out. It also
// reports whether keyLimit cut the keys short.
func listNames(
	ws *workspace.Session, mods *index.Index, file int32, reference schema.Reference, text string,
) ([]Completion, bool) {
	before, after := "", ""
	if position := strings.LastIndex(reference.Name, text); text != "" && position >= 0 {
		before, after = reference.Name[:position], reference.Name[position+len(text):]
	}

	var names []Completion
	offer := func(name, image string) {
		if len(name) >= len(before)+len(after) && strings.EqualFold(name[:len(before)], before) &&
			strings.EqualFold(name[len(name)-len(after):], after) {
			names = append(names, Completion{
				Label: name[len(before) : len(name)-len(after)], Category: schema.CategoryReference,
				Detail: game.DescribeDefinition(reference.Database, false), Image: image,
			})
		}
	}
	cut := false
	switch reference.Database {
	case game.File:
		for _, image := range mods.ListImages(reference.Name[:strings.LastIndex(reference.Name, "/")+1]) {
			// script writes a path without the stage folder it sits in
			offer(image.StageRel(), image.Path)
		}

	case game.Localization:
		var keys []string
		keys, cut = mods.ListKeys(cmp.Or(ws.Language, game.BaseLanguage), reference.Name, keyLimit)
		for _, key := range keys {
			offer(key, "")
		}

	default:
		for _, definition := range listDefinitions(mods, file, reference.Database) {
			offer(definition.Label, "")
		}
	}

	return names, cut
}

// listWritten returns the values the game's own files write at a field, each
// ranked by the given function. Numbers, dates, parameters and constants are
// left out, since none of them names one value of the game.
func listWritten(vanilla map[string]int, makeConstant func(string) Completion) []Completion {
	var values []Completion
	for _, value := range slices.Sorted(maps.Keys(vanilla)) {
		text := strings.Trim(value, `"`)
		class := jomini.Classify(text)
		if !class.IsNumberKey() && class != jomini.ClassParameter && class != jomini.ClassConstant &&
			class != jomini.ClassExpression {
			values = append(values, makeConstant(text))
		}
	}

	return values
}

// listPrefixes returns the link prefixes whose link leads to the given scope
// type, each written with its colon, as its key is.
func listPrefixes(ws *workspace.Session, scope string) []Completion {
	var prefixes []Completion
	for _, key := range ws.Schema.ListKeys(game.DumpLinks) {
		if link := ws.Schema.LookupDefinition(game.DumpLinks, key); link.Prefixed && link.Target == scope {
			prefixes = append(prefixes, Completion{Label: key, Insert: key, Category: schema.CategoryLink, Blurb: link.Blurb})
		}
	}

	return prefixes
}

// listDefinitions returns a candidate for each definition of a database that
// one file of the index sees: those in a file the game reads, without a
// definition local to another file.
func listDefinitions(mods *index.Index, file int32, database string) []Completion {
	var candidates []Completion
	for _, site := range mods.ListDefinitions(database) {
		sees := site.File == file || !mods.Occurrence(site).Local
		if key := mods.ToReference(site).Name; mods.IsLoaded(site.File) && sees {
			candidates = append(candidates, Completion{
				Label: key, Insert: key, Category: schema.CategoryReference,
				Detail: game.DescribeDefinition(database, false),
			})
		}
	}

	return candidates
}

// listScopeLinks returns the links from where a statement is written to the
// scopes that the first function accepts, the latest saved scope first. The
// second function ranks a link.
func listScopeLinks(context *schema.Context, fits func(scope string) bool, rank func(string) int) []Completion {
	holder := context.Parent
	if context.Defines || holder == nil {
		holder = context
	}

	var links []Completion
	offer := func(link, target, blurb string) {
		if fits(target) {
			links = append(links, Completion{
				Label: link, Insert: link, Category: schema.CategoryLink, Blurb: blurb, Rank: rank(link),
			})
		}
	}
	offer(game.ScopeLinks.This, holder.Scope, "the scope this line runs in")
	offer(game.ScopeLinks.Root, holder.Root, "the scope the definition starts in")
	previous := ""
	if len(holder.Previous) > 0 {
		previous = holder.Previous[0]
	}

	offer(game.ScopeLinks.Previous, previous, "the scope before this one")

	// a name saved twice is offered once, with the type it was saved with
	// last
	offered := map[string]bool{}
	for _, saved := range slices.Backward(context.Saved) {
		if !offered[saved.Name] {
			offered[saved.Name] = true
			offer(game.SavedScopes+":"+saved.Name, saved.Type, "saved earlier in the definition")
		}
	}

	return links
}

// listKeys returns the keys a block takes: the language's keywords, then its
// commands, calls and links, then its fields, each kind ranked above the
// next. A key listed twice is kept once, as the first kind. A key of one of
// the given database's names is ranked by how often the game's own files
// write it: in blocks of the block's field, or anywhere, whichever is more.
func listKeys(
	mods *index.Index, file int32, language *schema.Schema, row *game.Game, holder *schema.Context,
) []Completion {
	fields := listFields(mods, file, holder)
	floor := findTopRank(fields) + 1
	rank := func(database, key string) int {
		count := mods.CountUses(database, key)
		if holder.Field != nil && holder.Field.Children[key] != nil {
			count = max(count, holder.Field.Children[key].Vanilla.Count)
		}

		return floor + count
	}
	commands := slices.Concat(listCommands(language, holder, rank), listCalls(mods, file, language, holder, rank))
	if holder.TakesLinks() {
		commands = append(commands, listKeyLinks(language, holder, rank)...)
	}

	keywords := listKeywords(row, language, holder, findTopRank(commands)+1)

	return dropRepeats(slices.Concat(keywords, commands, fields))
}

// listCalls returns the scripted definitions that the caller of a block
// calls. A block whose caller is any call takes those of each callable that
// acts as a command the block takes. The given function ranks a call.
func listCalls(
	mods *index.Index, file int32, language *schema.Schema, holder *schema.Context, rank func(database, key string) int,
) []Completion {
	caller := holder.FindCaller()
	databases := language.Callees[caller]
	if caller == game.AnyCall {
		databases = slices.DeleteFunc(slices.Sorted(maps.Keys(game.Callables)), func(database string) bool {
			return !holder.Takes(game.Callables[database].Command)
		})
	}

	var calls []Completion
	for _, database := range databases {
		for _, call := range listDefinitions(mods, file, database) {
			call.Category, call.Rank = schema.CategoryCall, rank(database, call.Label)
			calls = append(calls, makeKey(call, false))
		}
	}

	return calls
}

// listKeyLinks returns the links a key in a block may be: the links the dumps
// declare that are written alone and run in the block's scope, then this,
// root, prev and the scopes saved earlier, each with a block. The given
// function ranks a link.
func listKeyLinks(language *schema.Schema, holder *schema.Context, rank func(database, key string) int) []Completion {
	var links []Completion
	for _, key := range language.ListKeys(game.DumpLinks) {
		link := language.LookupDefinition(game.DumpLinks, key)
		if !link.Prefixed && link.ResolveTarget(holder.Scope) != "" {
			links = append(links, Completion{
				Label: key, Category: schema.CategoryLink, Detail: findClass(game.DumpLinks).ID, Blurb: link.Blurb,
				Rank: rank(game.DumpLinks, key),
			})
		}
	}

	rankLink := func(key string) int { return rank(game.DumpLinks, key) }
	links = append(links, listScopeLinks(holder, func(string) bool { return true }, rankLink)...)
	for position := range links {
		links[position] = makeKey(links[position], true)
	}

	return links
}

// findTopRank returns the highest rank among some candidates, or 0 if there
// are none.
func findTopRank(candidates []Completion) int {
	top := 0
	for _, candidate := range candidates {
		top = max(top, candidate.Rank)
	}

	return top
}

// listFields returns the keys the block's field declares, in alphabetical
// order, except those that begin with "<", then the names of the database its
// keys name. It returns nil if the block has no field.
func listFields(mods *index.Index, file int32, holder *schema.Context) []Completion {
	if holder.Field == nil {
		return nil
	}

	var fields []Completion
	for _, key := range slices.Sorted(maps.Keys(holder.Field.Children)) {
		field := holder.Field.Children[key]
		if strings.HasPrefix(key, "<") {
			continue
		}

		fields = append(fields, makeKey(Completion{
			Label: key, Category: schema.CategoryField, Detail: string(field.Value.Value.Type), Blurb: field.Blurb.Value,
			Rank: field.Vanilla.Count,
		}, isBlockField(field)))
	}

	if database := holder.Field.Keys.Value; database != "" {
		block := isBlockField(holder.Field.Children[game.AnyKey])
		for _, name := range listDefinitions(mods, file, database) {
			fields = append(fields, makeKey(name, block))
		}
	}

	return fields
}

// listKeywords returns the given game's keywords that fit a block, grouped by
// class and ranked above the given floor. A keyword the dumps declare is
// offered only where the block takes its command, so if is none of a trigger
// block's. A key written with a block is inserted with one.
func listKeywords(row *game.Game, language *schema.Schema, holder *schema.Context, floor int) []Completion {
	fitting := slices.DeleteFunc(row.ListKeywords(cmp.Or(holder.Shape, game.ShapeUnknown), holder.Label), func(keyword game.Keyword) bool {
		return language.FindCommand(keyword.Key) != nil && findTaken(language, holder, keyword.Key) == nil
	})
	// the classes come in the order their first keyword does
	var classes []game.KeywordClass
	for _, keyword := range fitting {
		if !slices.Contains(classes, keyword.Class) {
			classes = append(classes, keyword.Class)
		}
	}

	slices.SortStableFunc(fitting, func(first, second game.Keyword) int {
		return cmp.Compare(slices.Index(classes, first.Class), slices.Index(classes, second.Class))
	})

	keywords := make([]Completion, len(fitting))
	for position, keyword := range fitting {
		command := findTaken(language, holder, keyword.Key)
		var claim *schema.Field
		if holder.Field != nil {
			claim = holder.Field.Children[keyword.Key]
		}

		block := isBlockShape(keyword.Shape) || isBlockField(claim) || command != nil && isBlockCommand(command)
		keywords[position] = makeKey(Completion{
			Label: keyword.Key, Category: schema.CategoryKeyword, Detail: string(keyword.Class), Blurb: keyword.Blurb,
			Rank: floor + len(fitting) - 1 - position,
		}, block)
	}

	return keywords
}

// findTaken returns the effect or the trigger that a key names and a block
// takes, the effect first. It returns nil if the dumps declare none the
// block takes.
func findTaken(language *schema.Schema, holder *schema.Context, key string) *schema.Definition {
	for _, database := range commandDatabases {
		if command := language.LookupDefinition(database, key); command != nil && holder.Takes(database) {
			return command
		}
	}

	return nil
}

// isBlockShape reports whether the given shape can only be written as a
// block. A value or unknown shape may be plain.
func isBlockShape(shape game.Shape) bool {
	return shape != "" && shape != game.ShapeValue && shape != game.ShapeUnknown
}

// isBlockField reports whether a field is written as a block. It is false
// for a nil field.
func isBlockField(field *schema.Field) bool {
	return field != nil && (field.Value.Value.Type == schema.ValueTypeBlock || len(field.Children) > 0 ||
		field.Shape.IsStated() && isBlockShape(field.Shape.Value) ||
		field.Vanilla.Blocks > 0 && field.Vanilla.Blocks == field.Vanilla.Count)
}

// listCommands returns the effects and the triggers a block takes, each in
// alphabetical order. The given function ranks a command.
func listCommands(language *schema.Schema, holder *schema.Context, rank func(database, key string) int) []Completion {
	var candidates []Completion
	for _, database := range commandDatabases {
		if !holder.Takes(database) {
			continue
		}

		for _, key := range language.ListKeys(database) {
			command := language.LookupDefinition(database, key)
			candidates = append(candidates, makeKey(Completion{
				Label: key, Category: schema.CategoryCommand, Detail: findClass(database).ID, Blurb: command.Blurb,
				Rank: rank(database, key),
			}, isBlockCommand(command)))
		}
	}

	return candidates
}

// isBlockCommand reports whether a command is only ever written with a
// block: its keys are written as a block, or it leads to a scope or walks a
// list, and the game's own files never write it as a plain value.
func isBlockCommand(command *schema.Definition) bool {
	if command.Field != nil && len(command.Field.Vanilla.Values) > 0 {
		return false
	}

	return command.Target != "" || command.Iterates || isBlockField(command.Field)
}

// makeKey returns the given candidate with its insert text set: the key and
// an equals sign, and an empty block if the flag is set.
func makeKey(candidate Completion, block bool) Completion {
	candidate.Insert = candidate.Label + " = "
	if block {
		candidate.Insert, candidate.Snippet = candidate.Label+" = {\n\t$0\n}", true
	}

	return candidate
}
