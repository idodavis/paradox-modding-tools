// complete.go lists what can be written at a position in a file: keys,
// keywords, commands and values.

package lsp

import (
	"cmp"
	"maps"
	"slices"
	"strings"

	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/services/workspace"
)

// Completion is one candidate in the completion list.
type Completion struct {
	Label string `json:"label"`

	// Insert is the text written when the candidate is chosen.
	Insert string `json:"insert"`

	// Category is what the candidate is, for its icon and color.
	Category lang.Category `json:"category"`

	// Detail is a few words on what the candidate is. It is "" if there are
	// none.
	Detail string `json:"detail,omitempty"`

	Blurb string `json:"blurb,omitempty"`

	// Rank is how often the game writes the candidate there.
	Rank int `json:"rank"`

	// Snippet is whether Insert has placeholders.
	Snippet bool `json:"snippet"`

	// Image is the full path of the image file the candidate names, or "".
	Image string `json:"image,omitempty"`
}

// CompletionList is what can be written at a position in a file.
type CompletionList struct {
	Candidates []Completion `json:"candidates"`

	// Cut is whether a limit left candidates out. The editor asks again as
	// typing narrows a list that is cut, and filters any other itself.
	Cut bool `json:"cut"`
}

// commandDatabases are the databases of the dumps' commands that a block may
// take as keys, in lookup order.
var commandDatabases = []string{lang.DumpEffects, lang.DumpTriggers}

// Complete returns the candidates at a position: keywords, commands and fields
// at a key, what may be written at a value. A word alone in a block gets both.
// It returns an empty list for a file that is not script.
func (s *Service) Complete(path string, line, column int) (CompletionList, error) {
	ws, document, err := s.loadDocument(path)
	if err != nil {
		return CompletionList{}, err
	}

	tree, mods := document.Tree, ws.Mods.Load()
	place := ws.Locate(mods, path)
	list := CompletionList{Candidates: []Completion{}}
	if document.Contexts == nil {
		return list, nil
	}

	offset := jomini.ToOffset(tree.Src, tree.Lines, line, column)
	statement, listed, ok := findValue(tree, offset)
	if ok {
		current := tree.Statements[statement]
		typed := ""
		if !current.Value.IsEmpty() && offset >= current.Value.Start {
			typed = tree.Src[current.Value.Start:offset]
		}

		context := &document.Contexts[statement]
		list.Candidates, list.Cut = listValues(ws, mods, place.Position, tree, context, typed, offset)
		links := listChainLinks(ws.Schema, context, strings.TrimPrefix(typed, `"`))
		list.Candidates = dropRepeats(append(list.Candidates, links...))
	}

	if ok && !listed {
		return list, nil
	}

	// The innermost block holding the offset gives the keys.
	holder := tree.FindStatement(offset)
	for holder > 0 && (offset <= tree.Statements[holder].Block.Start || offset >= tree.Statements[holder].Block.End) {
		holder = tree.Statements[holder].Parent
	}

	if holder == 0 || document.Contexts[holder].Shape == "" {
		return list, nil
	}

	keys := listKeys(mods, place.Position, ws.Schema, lang.LookupDialect(ws.Game.ID), &document.Contexts[holder])
	list.Candidates = dropRepeats(slices.Concat(keys, list.Candidates))

	return list, nil
}

// findValue returns the statement whose value the offset is writing, and
// whether the value stands alone in a block, so that it may also be a key. It
// returns false if the offset writes no value.
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

// listValues returns what may be written as a statement's value: constants, the
// names of the databases its references name, and for a target the links and
// prefixes of its scope. With none of these it offers the values the game
// writes there. It reports whether a limit cut the names short.
func listValues(
	ws *workspace.Session, mods *index.Index, file int32, tree *jomini.Tree, context *lang.Context, typed string,
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

		return Completion{Label: value, Insert: insert, Category: lang.CategoryConstant, Rank: rank(value)}
	}

	var constants []string
	switch {
	case context.Callee != nil:
		constants = ws.Schema.Databases[context.Callee.Database].Declaration.Values
	case expected.Type == lang.ValueTypeBool:
		constants = lang.Bools
	case expected.Type == lang.ValueTypeChoice:
		constants = expected.Choices
	}

	values := []Completion{}
	for _, constant := range constants {
		values = append(values, makeConstant(constant))
	}

	cut := false
	for _, reference := range listTyped(context, expected, start, offset) {
		// A link's prefix is written before each name.
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

	if expected.Type == lang.ValueTypeTarget && expected.Scope != "" {
		isScope := func(scope string) bool { return scope == expected.Scope }
		values = append(values, listPrefixes(ws, expected.Scope)...)
		values = append(values, listScopeLinks(context, isScope, rank)...)
	}

	return dropRepeats(values), cut
}

// listChainLinks returns what continues the links typed so far. After scope:
// it is the saved scopes, whatever the field takes. After a dot it is the
// links that run in the scope the chain leads to, each written after the
// chain, as root.liege after root. An unknown link before the dot gives none.
func listChainLinks(language *lang.Schema, context *lang.Context, typed string) []Completion {
	var links []Completion
	if strings.HasPrefix(typed, lang.SavedScopes+":") {
		links = listScopeLinks(context, func(string) bool { return true }, func(string) int { return 0 })
	}

	position := strings.LastIndex(typed, ".")
	if position < 0 || context.Parent == nil {
		return links
	}

	chain := language.ResolveChain(typed[:position], context.Parent, context.Saved)
	if slices.ContainsFunc(chain, func(link lang.ChainLink) bool { return link.Definition == nil }) {
		return links
	}

	scope := chain[len(chain)-1].Scope
	for _, key := range language.ListKeys(lang.DumpLinks) {
		link := language.LookupDefinition(lang.DumpLinks, key)
		if label := typed[:position+1] + key; !link.Prefixed && link.ResolveTarget(scope) != "" {
			links = append(links, Completion{
				Label: label, Insert: label, Category: lang.CategoryLink, Detail: findClass(lang.DumpLinks).ID,
				Blurb: link.Blurb,
			})
		}
	}

	return links
}

// findExpected returns what a statement's value must be, and how often the
// game writes each value there. A link's value is a target of its scope, with
// no counts. Both are empty for an unknown statement.
func findExpected(context *lang.Context) (lang.Expected, map[string]int) {
	switch {
	case context.Category == lang.CategoryLink:
		return lang.Expected{Type: lang.ValueTypeTarget, Scope: context.Scope}, nil
	case context.Field == nil:
		return lang.Expected{}, nil
	}

	return context.Field.Value.Value, context.Field.Vanilla.Values
}

// listTyped returns the walk's references in the typed text, between the two
// offsets, except saved scopes and names the dumps declare, which links and
// commands cover. With nothing typed, the database the field claims stands in.
func listTyped(context *lang.Context, expected lang.Expected, start, offset int) []lang.Reference {
	references := slices.DeleteFunc(slices.Clone(context.References), func(reference lang.Reference) bool {
		return reference.Span.Start < start || reference.Span.Start > offset ||
			reference.Database == lang.SavedScopes || slices.Contains(lang.Dumps, reference.Database)
	})
	claims := expected.Type == lang.ValueTypeReference || expected.Type == lang.ValueTypeTarget
	if start == offset && len(references) == 0 && claims && expected.Database != "" {
		references = []lang.Reference{{Database: expected.Database, Span: jomini.Span{Start: offset, End: offset}}}
	}

	return references
}

// keyLimit is the most localization keys a completion lists.
const keyLimit = 200

// listNames returns the names one file sees in a reference's database, written
// as the given text writes the reference, without what the walk added around
// it, such as an image's folder. It reports whether keyLimit cut the keys.
func listNames(
	ws *workspace.Session, mods *index.Index, file int32, reference lang.Reference, text string,
) ([]Completion, bool) {
	prefix, suffix := "", ""
	if position := strings.LastIndex(reference.Name, text); text != "" && position >= 0 {
		prefix, suffix = reference.Name[:position], reference.Name[position+len(text):]
	}

	var names []Completion
	offer := func(name, image string) {
		if len(name) >= len(prefix)+len(suffix) && strings.EqualFold(name[:len(prefix)], prefix) &&
			strings.EqualFold(name[len(name)-len(suffix):], suffix) {
			names = append(names, Completion{
				Label: name[len(prefix) : len(name)-len(suffix)], Category: lang.CategoryReference,
				Detail: lang.DescribeDefinition(reference.Database, false), Image: image,
			})
		}
	}

	cut := false
	switch reference.Database {
	case lang.File:
		for _, image := range mods.ListImages(reference.Name[:strings.LastIndex(reference.Name, "/")+1]) {
			// Script writes a path without its stage folder.
			offer(image.StageRel(), image.Path)
		}

	case lang.Localization:
		var keys []string
		keys, cut = mods.ListKeys(ws.Languages(), reference.Name, keyLimit)
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

// listDefinitions returns a candidate for each definition of a database that
// one file sees: in a file the game reads, and not local to another file.
func listDefinitions(mods *index.Index, file int32, database string) []Completion {
	var candidates []Completion
	for _, site := range mods.ListDefinitions(database) {
		sees := site.File == file || !mods.Occurrence(site).Local
		if key := mods.ToReference(site).Name; mods.IsLoaded(site.File) && sees {
			candidates = append(candidates, Completion{
				Label: key, Insert: key, Category: lang.CategoryReference,
				Detail: lang.DescribeDefinition(database, false),
			})
		}
	}

	return candidates
}

// listWritten returns the values the game writes at a field. Numbers, dates,
// parameters and constants are left out, since none names one value.
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

// listPrefixes returns the link prefixes that lead to a scope.
func listPrefixes(ws *workspace.Session, scope string) []Completion {
	var prefixes []Completion
	for _, key := range ws.Schema.ListKeys(lang.DumpLinks) {
		if link := ws.Schema.LookupDefinition(lang.DumpLinks, key); link.Prefixed && link.Target == scope {
			prefixes = append(prefixes, Completion{Label: key, Insert: key, Category: lang.CategoryLink, Blurb: link.Blurb})
		}
	}

	return prefixes
}

// listScopeLinks returns this, root, prev and the saved scopes, latest first,
// whose scope the first function accepts. The second function ranks a link.
func listScopeLinks(context *lang.Context, fits func(scope string) bool, rank func(string) int) []Completion {
	holder := context.Parent
	if context.Defines || holder == nil {
		holder = context
	}

	var links []Completion
	offer := func(link, target, blurb string) {
		if fits(target) {
			links = append(links, Completion{
				Label: link, Insert: link, Category: lang.CategoryLink, Blurb: blurb, Rank: rank(link),
			})
		}
	}
	offer(lang.LinkThis, holder.Scope, "the scope this line runs in")
	offer(lang.LinkRoot, holder.Root, "the scope the definition starts in")
	previous := ""
	if len(holder.Previous) > 0 {
		previous = holder.Previous[0]
	}

	offer(lang.LinkPrevious, previous, "the scope before this one")

	// A name saved twice is offered once, with its last type.
	offered := map[string]bool{}
	for _, saved := range slices.Backward(context.Saved) {
		if !offered[saved.Name] {
			offered[saved.Name] = true
			offer(lang.SavedScopes+":"+saved.Name, saved.Type, "saved earlier in the definition")
		}
	}

	return links
}

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

// listKeys returns the keys a block takes: keywords, then commands, calls and
// links, then fields, each kind ranked above the next. A repeated key is kept
// once. A command ranks by how often the game writes it, in the field's blocks
// or anywhere.
func listKeys(
	mods *index.Index, file int32, language *lang.Schema, dialect *lang.Dialect, holder *lang.Context,
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

	keywords := listKeywords(dialect, language, holder, findTopRank(commands)+1)

	return dropRepeats(slices.Concat(keywords, commands, fields))
}

// listFields returns the keys the block's field declares, alphabetically,
// except those beginning with "<", then the names of the database its keys
// name. It returns nil without a field.
func listFields(mods *index.Index, file int32, holder *lang.Context) []Completion {
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
			Label: key, Category: lang.CategoryField, Detail: string(field.Value.Value.Type), Blurb: field.Blurb.Value,
			Rank: field.Vanilla.Count,
		}, isBlockField(field)))
	}

	if database := holder.Field.Keys.Value; database != "" {
		block := isBlockField(holder.Field.Children[lang.AnyKey])
		for _, name := range listDefinitions(mods, file, database) {
			fields = append(fields, makeKey(name, block))
		}
	}

	return fields
}

func findTopRank(candidates []Completion) int {
	top := 0
	for _, candidate := range candidates {
		top = max(top, candidate.Rank)
	}

	return top
}

// listCommands returns the effects and triggers a block takes, each in
// alphabetical order.
func listCommands(language *lang.Schema, holder *lang.Context, rank func(database, key string) int) []Completion {
	var candidates []Completion
	for _, database := range commandDatabases {
		if !holder.Takes(database) {
			continue
		}

		for _, key := range language.ListKeys(database) {
			command := language.LookupDefinition(database, key)
			candidates = append(candidates, makeKey(Completion{
				Label: key, Category: lang.CategoryCommand, Detail: findClass(database).ID, Blurb: command.Blurb,
				Rank: rank(database, key),
			}, isBlockCommand(command)))
		}
	}

	return candidates
}

// listCalls returns the scripted definitions the block's caller calls. For any
// call, they are those of each callable that acts as a command the block takes.
func listCalls(
	mods *index.Index, file int32, language *lang.Schema, holder *lang.Context, rank func(database, key string) int,
) []Completion {
	caller := holder.FindCaller()
	databases := language.Callees[caller]
	if caller == lang.AnyCall {
		databases = slices.DeleteFunc(slices.Clone(lang.Called), func(database string) bool {
			owner := language.Databases[database]

			return owner == nil || !holder.Takes(owner.Declaration.Command())
		})
	}

	var calls []Completion
	for _, database := range databases {
		for _, call := range listDefinitions(mods, file, database) {
			call.Category, call.Rank = lang.CategoryCall, rank(database, call.Label)
			calls = append(calls, makeKey(call, false))
		}
	}

	return calls
}

// listKeyLinks returns the links a key may be: those written alone that run in
// the block's scope, then this, root, prev and the saved scopes.
func listKeyLinks(language *lang.Schema, holder *lang.Context, rank func(database, key string) int) []Completion {
	var links []Completion
	for _, key := range language.ListKeys(lang.DumpLinks) {
		link := language.LookupDefinition(lang.DumpLinks, key)
		if !link.Prefixed && link.ResolveTarget(holder.Scope) != "" {
			links = append(links, Completion{
				Label: key, Category: lang.CategoryLink, Detail: findClass(lang.DumpLinks).ID, Blurb: link.Blurb,
				Rank: rank(lang.DumpLinks, key),
			})
		}
	}

	rankLink := func(key string) int { return rank(lang.DumpLinks, key) }
	links = append(links, listScopeLinks(holder, func(string) bool { return true }, rankLink)...)
	for position := range links {
		links[position] = makeKey(links[position], true)
	}

	return links
}

// listKeywords returns the game's keywords that fit a block, grouped by class
// and ranked above a given rank. A keyword the dumps declare is offered only
// where the block takes its command.
func listKeywords(dialect *lang.Dialect, language *lang.Schema, holder *lang.Context, floor int) []Completion {
	fitting := slices.DeleteFunc(dialect.ListKeywords(cmp.Or(holder.Shape, lang.ShapeUnknown), holder.Label), func(keyword lang.Keyword) bool {
		return language.FindCommand(keyword.Key) != nil && findTaken(language, holder, keyword.Key) == nil
	})
	// The classes come in the order their first keyword does.
	var classes []lang.KeywordClass
	for _, keyword := range fitting {
		if !slices.Contains(classes, keyword.Class) {
			classes = append(classes, keyword.Class)
		}
	}

	slices.SortStableFunc(fitting, func(first, second lang.Keyword) int {
		return cmp.Compare(slices.Index(classes, first.Class), slices.Index(classes, second.Class))
	})

	keywords := make([]Completion, len(fitting))
	for position, keyword := range fitting {
		command := findTaken(language, holder, keyword.Key)
		var claim *lang.Field
		if holder.Field != nil {
			claim = holder.Field.Children[keyword.Key]
		}

		block := isBlockShape(keyword.Shape) || isBlockField(claim) || command != nil && isBlockCommand(command)
		keywords[position] = makeKey(Completion{
			Label: keyword.Key, Category: lang.CategoryKeyword, Detail: string(keyword.Class), Blurb: keyword.Blurb,
			Rank: floor + len(fitting) - 1 - position,
		}, block)
	}

	return keywords
}

// findTaken returns the effect or trigger that a key names and a block takes,
// the effect first. It returns nil if there is none.
func findTaken(language *lang.Schema, holder *lang.Context, key string) *lang.Definition {
	for _, database := range commandDatabases {
		if command := language.LookupDefinition(database, key); command != nil && holder.Takes(database) {
			return command
		}
	}

	return nil
}

func isBlockShape(shape lang.Shape) bool {
	return shape != "" && shape != lang.ShapeValue && shape != lang.ShapeUnknown
}

func isBlockField(field *lang.Field) bool {
	return field != nil && (field.Value.Value.Type == lang.ValueTypeBlock || len(field.Children) > 0 ||
		field.Shape.IsStated() && isBlockShape(field.Shape.Value) ||
		field.Vanilla.Blocks > 0 && field.Vanilla.Blocks == field.Vanilla.Count)
}

// isBlockCommand reports whether the game only ever writes a command with a
// block, never as a plain value.
func isBlockCommand(command *lang.Definition) bool {
	if command.Field != nil && len(command.Field.Vanilla.Values) > 0 {
		return false
	}

	return command.Target != "" || command.Iterates || isBlockField(command.Field)
}

func makeKey(candidate Completion, block bool) Completion {
	candidate.Insert = candidate.Label + " = "
	if block {
		candidate.Insert, candidate.Snippet = candidate.Label+" = {\n\t$0\n}", true
	}

	return candidate
}
