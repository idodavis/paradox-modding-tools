// complete.go lists what can be written at a position in a file: keys, operators, commands and values.

package lsp

import (
	"cmp"
	"maps"
	"slices"
	"strings"

	"github.com/samber/lo"

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

// keyLimit is the most localization keys a completion lists. The editor asks again as typing narrows them.
const keyLimit = 200

// Complete returns the completion candidates at a position in a file: at a key the language's operators, commands and
// fields, at a value what may be written there. A word written alone in a block may be a value or a key typed before
// its equals sign, so it gets both. It returns an empty list for a file that is not script.
func (lsp *Service) Complete(path string, line, column int) ([]Completion, error) {
	ws, tree, err := lsp.loadTree(path)
	if err != nil {
		return nil, err
	}
	mods := ws.Mods.Load()
	place := ws.Locate(mods, path)
	if !place.Format.IsScript() {
		return []Completion{}, nil
	}
	offset := ToOffset(tree.Src, tree.Lines, line, column)
	values := []Completion{}
	statement, listed, ok := findValue(tree, offset)
	if ok {
		current := tree.Statements[statement]
		visitor := &hoverVisitor{tree: tree, offset: current.Span.Start}
		ws.Schema.WalkTo(tree, place.File, visitor.offset, visitor)
		typed := ""
		if !current.Value.IsEmpty() && offset >= current.Value.Start {
			typed = tree.Src[current.Value.Start:offset]
		}
		values = listValues(ws, mods, place.Position, tree, &visitor.context, typed, offset)
	}
	if ok && !listed {
		return values, nil
	}
	visitor := &completeVisitor{offset: offset}
	ws.Schema.WalkTo(tree, place.File, offset, visitor)
	if visitor.context.Shape == "" {
		return values, nil
	}
	keys := listKeys(mods, place.Position, ws.Schema, ws.Game, &visitor.context)
	return lo.UniqBy(slices.Concat(keys, values), func(candidate Completion) string { return candidate.Label }), nil
}

// findValue returns the statement whose value the given offset is writing, and whether that value is written alone
// in a block, as a listed name or a key typed before its equals sign is. It returns false if the offset is not writing
// a value.
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

// listValues returns the values that may be written at a statement's value in one file of the index, given the text
// typed so far, with the quote it may begin with, and the offset it ends at. A call takes what its database's row
// says, a field yes and no or its choices, and a value the walk found a reference in takes the names of that
// reference's database. Where none of these apply, it offers the values the game's own files write there. A target
// also takes the links and the link prefixes that lead to the scope it needs. It returns an empty list if nothing is
// known of the value.
func listValues(
	ws *workspace.Session, mods *index.Index, file int32, tree *jomini.Tree, context *schema.Context, typed string,
	offset int,
) []Completion {
	expected, vanilla := findExpected(context)
	quoted := strings.HasPrefix(typed, `"`)
	typed = strings.TrimPrefix(typed, `"`)
	start := offset - len(typed)
	rank := func(value string) int { return max(vanilla[value], vanilla[`"`+value+`"`]) }
	makeConstant := func(value string, _ int) Completion {
		insert := value
		if !quoted && strings.Contains(value, " ") {
			insert = `"` + value + `"`
		}
		return Completion{Label: value, Insert: insert, Category: schema.CategoryConstant, Rank: rank(value)}
	}
	values := []Completion{}
	switch {
	case context.Callee != nil:
		values = lo.Map(game.Callables[context.Callee.Database].Values, makeConstant)
	case expected.Type == schema.ValueTypeBool:
		values = lo.Map(game.Bools, makeConstant)
	case expected.Type == schema.ValueTypeChoice:
		values = lo.Map(expected.Choices, makeConstant)
	}
	for _, reference := range listTyped(context, expected, start, offset) {
		// the text before the reference, such as a link's prefix, is written before each of its names
		head := typed[:reference.Span.Start-start]
		for _, candidate := range listNames(ws, mods, file, reference, tree.Text(reference.Span)) {
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
	return lo.UniqBy(values, func(candidate Completion) string { return candidate.Label })
}

// findExpected returns what a statement's value must be, and how often the game's own files write each value there.
// The value of a link is a target of the scope the link leads to, and the field a link has is its block's, so it
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

// listTyped returns the references the walk found in a value that start between the given offsets, where the typed
// text starts and where it ends. A saved scope is left out, since only the scopes saved earlier in the definition may
// be named there. Before anything is typed the walk has read no value, so the database the field claims stands in
// for it. It returns none if the field claims no database either.
func listTyped(context *schema.Context, expected schema.Expected, start, offset int) []schema.Reference {
	references := lo.Filter(context.References, func(reference schema.Reference, _ int) bool {
		return reference.Span.Start >= start && reference.Span.Start <= offset && reference.Database != game.SavedScopes
	})
	claims := expected.Type == schema.ValueTypeReference || expected.Type == schema.ValueTypeTarget
	if start == offset && len(references) == 0 && claims && expected.Database != "" {
		references = []schema.Reference{{Database: expected.Database, Span: jomini.Span{Start: offset, End: offset}}}
	}
	return references
}

// listNames returns the names of a reference's database that one file of the index sees, each written the way the
// given text writes the reference: what the walk put around that text to make the name, such as an image's folder, is
// left off. Localization keys are those that contain the reference's name, up to keyLimit, and images those under its
// folder. A definition local to another file is left out.
func listNames(
	ws *workspace.Session, mods *index.Index, file int32, reference schema.Reference, text string,
) []Completion {
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
	switch reference.Database {
	case game.File:
		for _, image := range mods.ListImages(reference.Name[:strings.LastIndex(reference.Name, "/")+1]) {
			// script writes a path without the stage folder it sits in
			offer(image.StageRel(), image.Path)
		}
	case game.Localization:
		for _, key := range mods.ListKeys(cmp.Or(ws.Language, game.BaseLanguage), reference.Name, keyLimit) {
			offer(key, "")
		}
	default:
		for _, definition := range listDefinitions(mods, file, reference.Database) {
			offer(definition.Label, "")
		}
	}
	return names
}

// listWritten returns the values the game's own files write at a field, each ranked by the given function. Numbers,
// dates, parameters and constants are left out, since none of them names one value of the game.
func listWritten(vanilla map[string]int, makeConstant func(string, int) Completion) []Completion {
	var values []Completion
	for _, value := range slices.Sorted(maps.Keys(vanilla)) {
		text := strings.Trim(value, `"`)
		class := jomini.Classify(text)
		if !class.IsNumberKey() && class != jomini.ClassParameter && class != jomini.ClassConstant &&
			class != jomini.ClassExpression {
			values = append(values, makeConstant(text, 0))
		}
	}
	return values
}

// listPrefixes returns the link prefixes whose link leads to the given scope type, each written with its colon.
func listPrefixes(ws *workspace.Session, scope string) []Completion {
	var prefixes []Completion
	for _, prefix := range slices.Sorted(maps.Keys(ws.Schema.Prefixes)) {
		if command := ws.Schema.Commands[prefix]; command != nil && command.Target == scope {
			prefixes = append(prefixes, Completion{
				Label: prefix + ":", Insert: prefix + ":", Category: schema.CategoryLink, Blurb: command.Blurb,
			})
		}
	}
	return prefixes
}

// listDefinitions returns a candidate for each definition of a database that one file of the index sees: those in a
// file the game reads, without a definition local to another file.
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

// listScopeLinks returns the links from where a statement is written to the scopes that the first function accepts,
// the latest saved scope first. The second function ranks a link.
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
	offer(game.ScopeLinks.Previous, lo.FirstOrEmpty(holder.Previous), "the scope before this one")
	latest := slices.Clone(context.Saved)
	slices.Reverse(latest)
	for _, saved := range lo.UniqBy(latest, func(saved schema.Saved) string { return saved.Name }) {
		offer(game.SavedScopes+":"+saved.Name, saved.Type, "saved earlier in the definition")
	}
	return links
}

// A completeVisitor finds the context of the innermost block that holds an offset.
type completeVisitor struct {
	offset  int
	context schema.Context
}

// Enter keeps the context of the block if it holds the offset.
func (visitor *completeVisitor) Enter(tree *jomini.Tree, statement int32, context *schema.Context) bool {
	if block := tree.Statements[statement].Block; visitor.offset > block.Start && visitor.offset < block.End {
		visitor.context = *context
	}
	return true
}

// Leave does nothing.
func (visitor *completeVisitor) Leave(tree *jomini.Tree, statement int32, context *schema.Context) {}

// listKeys returns the keys a block takes: the language's operators, then its commands, calls and links, then its
// fields, each kind ranked above the next. A key listed twice is kept once, as the first kind.
func listKeys(
	mods *index.Index, file int32, language *schema.Schema, row *game.Game, holder *schema.Context,
) []Completion {
	fields := listFields(mods, file, holder)
	floor := findTopRank(fields) + 1
	rank := func(key string) int { return floor + countKey(language.Commands, holder, key) }
	commands := slices.Concat(listCommands(language.Commands, holder, rank), listCalls(mods, file, language, holder, rank))
	if isLinkShape(holder.Shape) {
		commands = append(commands, listKeyLinks(language, holder, rank)...)
	}
	operators := listOperators(row, language.Commands, holder, findTopRank(commands)+1)
	candidates := slices.Concat(operators, commands, fields)
	return lo.UniqBy(candidates, func(candidate Completion) string { return candidate.Label })
}

// countKey returns how often the game's own files write a key: in blocks of the given block's field, or as a command
// anywhere, whichever is more. It returns 0 if they never write it.
func countKey(commands map[string]*schema.Command, holder *schema.Context, key string) int {
	count := 0
	if holder.Field != nil && holder.Field.Children[key] != nil {
		count = holder.Field.Children[key].Vanilla.Count
	}
	if command := commands[key]; command != nil {
		count = max(count, command.Count)
	}
	return count
}

// isLinkShape reports whether a block of the given shape reads a key as a scope link.
func isLinkShape(shape game.Shape) bool {
	return slices.ContainsFunc(game.Shapes[shape].KeyRules, func(rule game.KeyRule) bool {
		return rule == game.KeyRuleLink || rule == game.KeyRuleScopeLink
	})
}

// listCalls returns the scripted definitions that the caller of a block calls. A block whose caller is any call takes
// those of each callable that acts as a command the block allows. The given function ranks a call.
func listCalls(
	mods *index.Index, file int32, language *schema.Schema, holder *schema.Context, rank func(string) int,
) []Completion {
	caller := holder.FindCaller()
	databases := language.Callees[caller]
	if caller == game.AnyCall {
		databases = lo.Filter(slices.Sorted(maps.Keys(game.Callables)), func(database string, _ int) bool {
			return holder.Allows(game.Callables[database].Command)
		})
	}
	var calls []Completion
	for _, database := range databases {
		for _, call := range listDefinitions(mods, file, database) {
			call.Category, call.Rank = schema.CategoryCall, rank(call.Label)
			calls = append(calls, makeKey(call, false))
		}
	}
	return calls
}

// listKeyLinks returns the links a key in a block may be: the links the dumps declare that are written alone and run
// in the block's scope, then this, root, prev and the scopes saved earlier, each with a block. The given function
// ranks a link.
func listKeyLinks(language *schema.Schema, holder *schema.Context, rank func(string) int) []Completion {
	var links []Completion
	for _, key := range slices.Sorted(maps.Keys(language.Commands)) {
		command := language.Commands[key]
		if _, ok := language.ResolveLink(key, holder.Scope); ok && command.Bare {
			links = append(links, Completion{
				Label: key, Category: schema.CategoryLink, Detail: findClass(game.DeclarationLink).ID, Blurb: command.Blurb,
				Rank: rank(key),
			})
		}
	}
	links = append(links, listScopeLinks(holder, func(string) bool { return true }, rank)...)
	for position := range links {
		links[position] = makeKey(links[position], true)
	}
	return links
}

// findTopRank returns the highest rank among some candidates, or 0 if there are none.
func findTopRank(candidates []Completion) int {
	return lo.MaxBy(candidates, func(first, second Completion) bool { return first.Rank > second.Rank }).Rank
}

// listFields returns the keys the block's field declares, in alphabetical order, except those that begin with "<",
// then the names of the database its keys name. It returns nil if the block has no field.
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

// listOperators returns the given game's keywords that fit a block, grouped by class and ranked above the given floor.
// A key written with a block is inserted with one.
func listOperators(row *game.Game, commands map[string]*schema.Command, holder *schema.Context, floor int) []Completion {
	fitting := lo.Filter(game.Keywords, func(keyword game.Keyword, _ int) bool {
		return !strings.ContainsAny(keyword.Key, "/*<") && row.Matches(keyword.Games) &&
			isOperatorFit(keyword, commands[keyword.Key], holder)
	})
	classes := lo.Uniq(lo.Map(fitting, func(keyword game.Keyword, _ int) game.KeywordClass { return keyword.Class }))
	slices.SortStableFunc(fitting, func(first, second game.Keyword) int {
		return cmp.Compare(slices.Index(classes, first.Class), slices.Index(classes, second.Class))
	})
	return lo.Map(fitting, func(keyword game.Keyword, position int) Completion {
		command := commands[keyword.Key]
		var claim *schema.Field
		if holder.Field != nil {
			claim = holder.Field.Children[keyword.Key]
		}
		block := isBlockShape(keyword.Shape) || isBlockField(claim) || command != nil && isBlockCommand(command)
		return makeKey(Completion{
			Label: keyword.Key, Category: schema.CategoryKeyword, Detail: string(keyword.Class), Blurb: keyword.Blurb,
			Rank: floor + len(fitting) - 1 - position,
		}, block)
	})
}

// isBlockShape reports whether the given shape can only be written as a block. A value or unknown shape may be plain.
func isBlockShape(shape game.Shape) bool {
	return shape != "" && shape != game.ShapeValue && shape != game.ShapeUnknown
}

// isBlockField reports whether a field is written as a block. It is false for a nil field.
func isBlockField(field *schema.Field) bool {
	return field != nil && (field.Value.Value.Type == schema.ValueTypeBlock || len(field.Children) > 0 ||
		field.Shape.IsStated() && isBlockShape(field.Shape.Value) ||
		field.Vanilla.Blocks > 0 && field.Vanilla.Blocks == field.Vanilla.Count)
}

// isOperatorFit reports whether a language keyword, with the command of its key, fits the block of the context.
func isOperatorFit(keyword game.Keyword, command *schema.Command, holder *schema.Context) bool {
	switch {
	case keyword.Holder != "":
		return keyword.Holder == holder.Shape
	case command != nil:
		_, ok := holder.FindDeclaration(command)
		return ok
	}
	return len(game.Shapes[holder.Shape].Allows) > 0
}

// listCommands returns the commands a block takes, in alphabetical order. The given function ranks a command.
func listCommands(commands map[string]*schema.Command, holder *schema.Context, rank func(string) int) []Completion {
	var candidates []Completion
	for _, key := range slices.Sorted(maps.Keys(commands)) {
		command := commands[key]
		declaration, ok := findAllowed(command, holder)
		if !ok {
			continue
		}
		candidates = append(candidates, makeKey(Completion{
			Label: key, Category: schema.CategoryCommand, Detail: findClass(declaration).ID, Blurb: command.Blurb,
			Rank: rank(key),
		}, isBlockCommand(command)))
	}
	return candidates
}

// findAllowed returns how the dumps declare a command, among the ways the given block takes it. It returns false if
// the block does not take the command, or the command is a pattern that no written name matches.
func findAllowed(command *schema.Command, holder *schema.Context) (game.Declaration, bool) {
	declaration, ok := holder.FindDeclaration(command)
	return declaration, ok && !command.Wild
}

// isBlockCommand reports whether a command is only ever written with a block: every form of it takes block arguments,
// or it leads to a scope or walks a list, and the game's own files never write it as a plain value.
func isBlockCommand(command *schema.Command) bool {
	arguments := slices.Collect(maps.Values(command.Arguments))
	if slices.ContainsFunc(arguments, func(field *schema.Field) bool { return field != nil && len(field.Vanilla.Values) > 0 }) {
		return false
	}
	return command.Target != "" || command.Lists || len(arguments) > 0 && lo.EveryBy(arguments, isBlockField)
}

// makeKey returns the given candidate with its insert text set: the key and an equals sign, and an empty block if the
// flag is set.
func makeKey(candidate Completion, block bool) Completion {
	candidate.Insert = candidate.Label + " = "
	if block {
		candidate.Insert, candidate.Snippet = candidate.Label+" = {\n\t$0\n}", true
	}
	return candidate
}
