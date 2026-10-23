package lsp

import (
	"cmp"
	"maps"
	"slices"
	"strings"

	"github.com/samber/lo"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/loc"
	"paradox-modding-tools/internal/schema"
	"paradox-modding-tools/services/workspace"
)

// A Code is one kind of problem: its id, its severity, the Health section it is listed under, and its message,
// which is a template filled from a diagnostic's facts.
type Code struct {
	ID       string `json:"id"`       // kebab: unresolved-reference, duplicate-definition, wrong-scope …
	Severity int    `json:"severity"` // 1 error, 2 warning, 3 information, 4 hint
	// Section is the Health section: contests, overrides, dependencies, dangling, localization, script, unused or
	// errorlog.
	Section string `json:"section"`
	Label   string `json:"label"`   // the code's title on Health
	Message string `json:"message"` // with {name}, {database}, {scope}, {expected} filled from the facts
}

// Codes is the one table of every code, the parser's and localization's included.
var Codes = []Code{
	{ID: "unclosed-block", Severity: 1, Section: "script", Label: "Unclosed block",
		Message: "This block is opened and never closed."},
	{ID: "unexpected-brace", Severity: 1, Section: "script", Label: "Unexpected brace",
		Message: "This closing brace has no block to close."},
	{ID: "unclosed-string", Severity: 1, Section: "script", Label: "Unclosed string",
		Message: "This string is opened and never closed."},
	{ID: "malformed-line", Severity: 1, Section: "localization", Label: "Malformed line",
		Message: "The game cannot read this line as a message."},
	{ID: "unresolved-reference", Severity: 1, Section: "dangling", Label: "Unresolved reference",
		Message: "Nothing defines the {database} {name}."},
	{ID: "unknown-key", Severity: 2, Section: "script", Label: "Unknown key",
		Message: "{name} is not a key a {database} takes here."},
	{ID: "wrong-namespace", Severity: 1, Section: "script", Label: "Wrong namespace",
		Message: "The {database} {name} is not in a namespace this file declares ({namespace})."},
	{ID: "misplaced-mode", Severity: 1, Section: "script", Label: "Misplaced entry mode",
		Message: "The {database} {name} cannot be written with {mode} here."},
	{ID: "duplicate-definition", Severity: 2, Section: "script", Label: "Duplicate definition",
		Message: "Another file defines the {database} {name} too, and the game keeps only one."},
	{ID: "conflicting-priority", Severity: 2, Section: "script", Label: "Conflicting priority",
		Message: "Another definition of the {database} {name} has the same priority, so the game cannot tell which to keep."},
	{ID: "wrong-scope", Severity: 1, Section: "script", Label: "Wrong scope",
		Message: "{name} runs in {allowed}, not in {scope}."},
	{ID: "wrong-value", Severity: 1, Section: "script", Label: "Wrong value",
		Message: "{name} takes {expected}."},
	{ID: "missing-key", Severity: 1, Section: "script", Label: "Missing key",
		Message: "The {database} {name} does not write {key}, which it must."},
	{ID: "duplicate-message", Severity: 2, Section: "localization", Label: "Duplicate message",
		Message: "The message {name} is defined more than once in {language}."},
	{ID: "orphaned-message", Severity: 4, Section: "localization", Label: "Orphaned message",
		Message: "Nothing refers to the message {name}."},
	{ID: "unused-definition", Severity: 4, Section: "unused", Label: "Unused definition",
		Message: "Nothing refers to the {database} {name}."},
	{ID: "contest", Severity: 3, Section: "contests", Label: "Contested definition",
		Message: "More than one mod defines the {database} {name}, and the game keeps one ({rule})."},
	{ID: "override", Severity: 3, Section: "overrides", Label: "Override of the game",
		Message: "A mod defines the {database} {name}, which the game defines too, and the game keeps {kept}."},
	{ID: "depends", Severity: 3, Section: "dependencies", Label: "Depends on a mod",
		Message: "This mod changes {count} definitions of {other}, so it needs that mod."},
	{ID: "affects", Severity: 3, Section: "dependencies", Label: "Affects a mod",
		Message: "{other} changes {count} definitions of this mod."},
	{ID: "missing-message", Severity: 2, Section: "localization", Label: "Missing message",
		Message: "The {database} {name} has no title message in the base language."},
	{ID: "coverage", Severity: 3, Section: "localization", Label: "Language coverage",
		Message: "{mod} has {have} of its {of} messages in {name}."},
	{ID: "game-error", Severity: 1, Section: "errorlog", Label: "Game error",
		Message: "The game logged: {message}"},
}

// A Diagnostic is one problem found in a file: its code, where it is, and the facts its message is written from.
type Diagnostic struct {
	Code  string            `json:"code"`
	Range Range             `json:"range"`
	Facts map[string]string `json:"facts"`
}

// valueDescriptions holds the words a value type is described with where a value is wrong.
var valueDescriptions = map[schema.ValueType]string{
	schema.ValueBool: "yes or no", schema.ValueNumber: "a number", schema.ValueDate: "a date",
	schema.ValueColor: "a color", schema.ValueCall: "a data call",
}

// Diagnose returns a file's problems: its parse errors and every verdict that fails on it, judged against the
// index for what other files say. It reads the open text if the editor has the file, else the file on disk.
func (lsp *Service) Diagnose(path string) ([]Diagnostic, error) {
	ws, err := lsp.Workspace.Session()
	if err != nil {
		return nil, err
	}
	diagnostics, err := DiagnoseFile(ws, ws.Mods.Load(), path)
	return lsp.listEnabled(diagnostics), err
}

// DiagnoseFile returns the problems of a file in the given session, judged against the given index: its parse
// errors and every verdict that fails on it. A file of the game's own gets its parse errors only.
func DiagnoseFile(ws *workspace.Session, mods *index.Index, path string) ([]Diagnostic, error) {
	tree, err := ws.LoadTree(path)
	if err != nil {
		return nil, err
	}
	place := ws.Locate(path)
	diagnostics := []Diagnostic{}
	switch {
	case place.Format == files.FormatLocalization:
		diagnostics = diagnoseMessages(ws, mods, tree, place)
	case place.Format.IsScript():
		diagnostics = toDiagnostics(tree, tree.Errors)
		if !place.ReadOnly {
			visitor := &diagnoseVisitor{ws: ws, index: mods, file: place.File}
			ws.Schema.Walk(tree, place.File, visitor)
			diagnostics = append(diagnostics, visitor.diagnostics...)
		}
	}
	return diagnostics, nil
}

// listEnabled returns the diagnostics less those of the checks the user turned off in the editor's settings.
func (lsp *Service) listEnabled(diagnostics []Diagnostic) []Diagnostic {
	setting, _ := lsp.Workspace.Config.LookupSetting("ide.checks")
	disabled := lo.Map(strings.Split(setting, ","), func(id string, _ int) string { return strings.TrimSpace(id) })
	return slices.DeleteFunc(diagnostics, func(diagnostic Diagnostic) bool {
		return slices.Contains(disabled, diagnostic.Code)
	})
}

// toDiagnostics converts the parse errors of a text to diagnostics with the parser's codes.
func toDiagnostics(tree *jomini.Tree, errors []jomini.Error) []Diagnostic {
	return lo.Map(errors, func(parseError jomini.Error, _ int) Diagnostic {
		return makeDiagnostic(tree, parseError.Span, parseError.Code, map[string]string{})
	})
}

// makeDiagnostic returns a diagnostic of the given code over a span of a tree's text.
func makeDiagnostic(tree *jomini.Tree, span jomini.Span, code string, facts map[string]string) Diagnostic {
	return Diagnostic{Code: code, Range: ToRange(tree.Src, tree.Lines, span), Facts: facts}
}

// diagnoseMessages returns the problems of a localization file: its parse errors, and each message that another
// message of the same language also defines, unless a replace folder holds either. A file of the game's own gets its
// parse errors only.
func diagnoseMessages(ws *workspace.Session, mods *index.Index, tree *jomini.Tree, place workspace.Place) []Diagnostic {
	file := loc.Parse(tree.Src, place.Path)
	diagnostics := toDiagnostics(tree, file.Errors)
	if place.ReadOnly || schema.IsLocReplacement(ws.Game, place.Rel, file.Language) {
		return diagnostics
	}
	position, known := mods.FindFile(place.Path)
	counts := lo.CountValuesBy(file.Messages, func(message loc.Message) string { return message.Key })
	for _, message := range file.Messages {
		others := lo.CountBy(mods.ListMessages(message.Key), func(site index.Site) bool {
			return (!known || site.File != position) && mods.Language(site) == file.Language &&
				isSameStage(ws.Game, place.Rel, mods.File(site).Rel) && mods.IsLoaded(site.File) &&
				!schema.IsLocReplacement(ws.Game, mods.File(site).Rel, file.Language)
		})
		if counts[message.Key]+others > 1 {
			diagnostics = append(diagnostics, makeDiagnostic(tree, message.KeySpan, "duplicate-message",
				map[string]string{"name": message.Key, "language": file.Language}))
		}
	}
	return diagnostics
}

// isSameStage reports whether two files, given by their paths from their roots, are loaded together: a game that keeps
// its files under stages loads one stage's at a time.
func isSameStage(game *game.Game, first, second string) bool {
	firstStage, _ := schema.SplitStage(game, first)
	secondStage, _ := schema.SplitStage(game, second)
	return firstStage == secondStage
}

// A diagnoseVisitor is the visitor behind Diagnose. At each statement it judges the five verdicts the context
// allows and records each one that fails.
type diagnoseVisitor struct {
	ws          *workspace.Session
	index       *index.Index // the mods' index, loaded once for the whole file
	file        files.File
	diagnostics []Diagnostic
}

// Enter judges the statement by the five verdicts the context allows and records each failure.
func (visitor *diagnoseVisitor) Enter(tree *jomini.Tree, statement int32, context *schema.Context) bool {
	visitor.diagnoseReferences(tree, context)
	visitor.diagnoseKey(tree, statement, context)
	visitor.diagnoseScope(tree, statement, context)
	visitor.diagnoseValue(tree, statement, context)
	return true
}

// Leave judges what the block as a whole had to contain: the keys its definition must write.
func (visitor *diagnoseVisitor) Leave(tree *jomini.Tree, statement int32, context *schema.Context) {
	definition := context.Definition
	if !context.Defines || definition.Mode != "" {
		return
	}
	database := visitor.ws.Schema.Databases[definition.Database]
	if database == nil || context.Field != database.Definition {
		return
	}
	for _, key := range slices.Sorted(maps.Keys(context.Field.Children)) {
		field := context.Field.Children[key]
		if field.Required.Value.Level == "always" && field.Required.Source.CanAccuse() && !strings.HasPrefix(key, "<") &&
			!hasChild(tree, statement, key) && !visitor.isWritten(definition, key) && visitor.isRequired(definition, key) {
			visitor.diagnostics = append(visitor.diagnostics, makeDiagnostic(tree, definition.Span, "missing-key", map[string]string{
				"name": definition.Name, "database": game.DescribeDefinition(definition.Database, false), "key": key,
			}))
		}
	}
}

// diagnoseReferences records each name the statement refers to that no definition answers. Where a name could be
// in more than one database, it is dangling only if none has it.
func (visitor *diagnoseVisitor) diagnoseReferences(tree *jomini.Tree, context *schema.Context) {
	for position, reference := range context.References {
		if slices.ContainsFunc(context.References[:position], func(other schema.Reference) bool { return other.Span == reference.Span }) {
			continue
		}
		alternatives := lo.Filter(context.References, func(other schema.Reference, _ int) bool { return other.Span == reference.Span })
		if lo.EveryBy(alternatives, func(other schema.Reference) bool { return canJudge(context.Field, other) }) &&
			!lo.SomeBy(alternatives, visitor.isKnown) {
			databases := lo.Map(alternatives, func(other schema.Reference, _ int) string {
				return game.DescribeDefinition(other.Database, false)
			})
			visitor.diagnostics = append(visitor.diagnostics, makeDiagnostic(tree, reference.Span, "unresolved-reference",
				map[string]string{"name": reference.Name, "database": strings.Join(lo.Uniq(databases), " or ")}))
		}
	}
}

// canJudge reports whether a reference can be judged dangling. It cannot where its database is unknown or holds names
// that script makes as it runs, where the name has a parameter or a prefix in it, where it is only a call that a
// block of unknown shape might make, or where only a claim learned from the game's files makes it.
func canJudge(field *schema.Field, reference schema.Reference) bool {
	inferred := field != nil && field.Value.Value.Type == schema.ValueReference &&
		field.Value.Value.Database == reference.Database && !field.Value.Source.CanAccuse()
	_, runtime := game.LinkMakers[reference.Database]
	_, _, prefixed := jomini.SplitPrefixed(reference.Name)
	return reference.Database != "" && !runtime && reference.Caller != game.AnyCall && !inferred && !prefixed &&
		!strings.Contains(reference.Name, "$")
}

// isKnown reports whether a reference names something: a definition in a file the game loads, a file the game loads
// for a written path, or a name that the game's own files use too, which the game then takes without a definition.
func (visitor *diagnoseVisitor) isKnown(reference schema.Reference) bool {
	mods := visitor.index
	if reference.Database == game.File {
		if _, ok := mods.ResolveTexture(reference.Name); ok {
			return true
		}
	} else if slices.ContainsFunc(resolve(visitor.ws, mods, reference, visitor.file.Path), func(site index.Site) bool {
		return mods.IsLoaded(site.File)
	}) {
		return true
	}
	return lo.SomeBy(mods.ListUses(reference.Database, reference.Name), func(site index.Site) bool {
		return mods.File(site).Origin == game.OriginVanilla
	})
}

// diagnoseKey records what is wrong with the key a statement is written at: a definition that is out of place or
// shared, an event outside the namespaces its file declares, or a key no rule of its block takes.
func (visitor *diagnoseVisitor) diagnoseKey(tree *jomini.Tree, statement int32, context *schema.Context) {
	current := tree.Statements[statement]
	switch {
	case context.Defines:
		visitor.diagnoseDefinition(tree, statement, context.Definition)
	case current.Key.IsEmpty() || context.Category != schema.CategoryNone || context.Field != nil:
	case current.Parent == 0 && jomini.IsEventID(tree.Unquoted(current.Key)):
		visitor.diagnoseNamespace(tree, current)
	case context.Definition != nil:
		visitor.diagnoseUnknown(tree, statement, context)
	}
}

// diagnoseDefinition records an entry mode on a definition where its database does not take one, and a plain
// definition whose name another shares in a way the database's precedence does not allow.
func (visitor *diagnoseVisitor) diagnoseDefinition(tree *jomini.Tree, statement int32, definition *schema.Definition) {
	database := visitor.ws.Schema.Databases[definition.Database]
	if database == nil {
		return
	}
	facts := map[string]string{"name": definition.Name, "database": game.DescribeDefinition(definition.Database, false)}
	modes, top := database.Precedence.Modes, tree.Statements[statement].Parent == 0
	fits := modes == "entry" && top || modes == "wrapped" && !top
	switch {
	case definition.Mode != "" && !fits:
		facts["mode"] = definition.Mode
		visitor.diagnostics = append(visitor.diagnostics, makeDiagnostic(tree, definition.Span, "misplaced-mode", facts))
	case definition.Mode == "" && !definition.Local:
		if code := visitor.classifyDuplicate(definition, database.Precedence); code != "" {
			visitor.diagnostics = append(visitor.diagnostics, makeDiagnostic(tree, definition.Span, code, facts))
		}
	}
}

// classifyDuplicate returns the code of the problem a plain definition has because other plain definitions share its
// name, or "" if it has none. Where the game keeps the highest priority, the definitions that tie at the highest one
// conflict, the default priority included. Where it keeps the first or the last, a definition the game does not keep
// is a duplicate, in the same mod or in another. Definitions that merge, a name that the game's own files define more
// than once, a definition that only the game keeps, and files that a replace folder drops are no problem.
func (visitor *diagnoseVisitor) classifyDuplicate(definition *schema.Definition, precedence game.Precedence) string {
	mods := visitor.index
	position, known := mods.FindFile(visitor.file.Path)
	reference := schema.Reference{Database: definition.Database, Name: definition.Name}
	defined := lo.Filter(resolve(visitor.ws, mods, reference, visitor.file.Path), func(site index.Site, _ int) bool {
		return mods.Occurrence(site).Mode == 0
	})
	sites := lo.Filter(defined, func(site index.Site, _ int) bool {
		return mods.IsLoaded(site.File) && isSameStage(visitor.ws.Game, visitor.file.Rel, mods.File(site).Rel)
	})
	own := slices.IndexFunc(sites, func(site index.Site) bool {
		return known && site.File == position && mods.Occurrence(site).Span == definition.Span
	})
	if len(sites) < 2 || own < 0 || precedence.Keeps == "merged" {
		return ""
	}
	if precedence.Keeps == "highest" {
		priorities := lo.Map(sites, func(site index.Site, _ int) int16 { return mods.Occurrence(site).Priority })
		top := slices.Max(priorities)
		if priorities[own] == top && lo.Count(priorities, top) > 1 {
			return "conflicting-priority"
		}
		return ""
	}
	byGame := lo.CountBy(defined, func(site index.Site) bool { return mods.File(site).Origin == game.OriginVanilla })
	winner, kept := findKept(visitor.ws, mods, reference, visitor.file.Path)
	if kept && byGame < 2 && winner.Site != sites[own] && mods.Occurrence(winner.Site).Mode == 0 &&
		mods.File(winner.Site).Origin != game.OriginVanilla {
		return "duplicate-definition"
	}
	return ""
}

// diagnoseNamespace records an event id written at the top of a file of events that the walk made no definition of,
// because the file declares no namespace the id is in. The fact lists the namespaces the file does declare.
func (visitor *diagnoseVisitor) diagnoseNamespace(tree *jomini.Tree, current jomini.Statement) {
	database, ok := visitor.ws.Schema.FindDatabase(visitor.file.Rel)
	if !ok || database.Row.Houses != "events" {
		return
	}
	namespaces := visitor.ws.Schema.ListNamespaces(tree, visitor.file)
	visitor.diagnostics = append(visitor.diagnostics, makeDiagnostic(tree, current.Key, "wrong-namespace", map[string]string{
		"name": tree.Unquoted(current.Key), "database": game.DescribeDefinition(database.Name, false),
		"namespace": cmp.Or(strings.Join(namespaces, ", "), "none"),
	}))
}

// diagnoseUnknown records a key that no rule of the block it is in takes, where a firm source describes the block and
// the game's own files never write it. Where they write it, they show it holds more keys than the sources list. A
// key that is no plain name, such as a number or an expression, is not judged.
func (visitor *diagnoseVisitor) diagnoseUnknown(tree *jomini.Tree, statement int32, context *schema.Context) {
	key := tree.Statements[statement].Key
	if visitor.ws.Schema.Databases[context.Database] == nil || !jomini.IsIdentifier(tree.Unquoted(key)) {
		return
	}
	holder := context.Parent
	if holder.Field == nil || holder.Shape == unknownShape || !holder.Field.Source.CanAccuse() || holder.Field.Vanilla.Count > 0 {
		return
	}
	visitor.diagnostics = append(visitor.diagnostics, makeDiagnostic(tree, key, "unknown-key", map[string]string{
		"name": tree.Unquoted(key), "database": game.DescribeDefinition(context.Database, false),
	}))
}

// diagnoseScope records a command or a link written in a scope that neither the dumps nor the game's own files list
// for it. The scope does not count at the top of a definition, where it is the database's claim, nor in a block that
// holds no effects or triggers.
func (visitor *diagnoseVisitor) diagnoseScope(tree *jomini.Tree, statement int32, context *schema.Context) {
	if context.Category != schema.CategoryLink && context.Category != schema.CategoryCommand {
		return
	}
	span := tree.Statements[statement].Key
	key, command := tree.Unquoted(span), context.Command
	if context.Category == schema.CategoryLink {
		key = jomini.SplitChain(key)[0]
		command = findCommand(visitor.ws.Schema, key)
	}
	if command == nil || len(command.Scopes) == 0 {
		return
	}
	// the scope counts only where the walk reached it by following links: the one a definition starts in is a claim of
	// its database
	followed := len(context.Previous)
	if context.Category == schema.CategoryLink || command.Target != "" || command.Lists {
		followed--
	}
	scope := context.ResolveCommandScope(command)
	if scope == "" || followed <= 0 || slices.Contains(command.Scopes, scope) || slices.Contains(command.VanillaScopes, scope) ||
		!isScripted(context) {
		return
	}
	allowed := lo.Uniq(slices.Concat(command.Scopes, command.VanillaScopes))
	visitor.diagnostics = append(visitor.diagnostics, makeDiagnostic(tree, span, "wrong-scope", map[string]string{
		"name": key, "scope": scope, "allowed": strings.Join(allowed, ", "),
	}))
}

// isScripted reports whether the block that holds a statement holds effects or triggers. A key in the block of a
// command that takes arguments is one of them, even when a command of the same name exists.
func isScripted(context *schema.Context) bool {
	holder := context.Parent
	if holder.Shape != "effects" && holder.Shape != "triggers" {
		return false
	}
	if holder.Category != schema.CategoryCommand {
		return true
	}
	return holder.Command != nil && (holder.Command.Target != "" || holder.Command.Lists)
}

// diagnoseValue records a value that is not the type its key takes, where a firm source states the type and every
// value the game's own files write at the key fits it.
func (visitor *diagnoseVisitor) diagnoseValue(tree *jomini.Tree, statement int32, context *schema.Context) {
	field, current := context.Field, tree.Statements[statement]
	if context.Category != schema.CategoryField || field == nil || !field.Value.IsStated() ||
		!field.Value.Source.CanAccuse() || current.IsBlock() || current.Value.IsEmpty() {
		return
	}
	// MatchValue cannot judge a path, a text that is any written word, or a block's short form
	expected, text := field.Value.Value, tree.Text(current.Value)
	if expected.Database == game.File || expected.Type == schema.ValueText || expected.Type == schema.ValueBlock {
		return
	}
	// a number may be written as the name of a script value
	named := expected.Type == schema.ValueNumber && jomini.IsIdentifier(tree.Unquoted(current.Value))
	match := visitor.ws.Schema.MatchValue
	if named || strings.ContainsAny(text, "[$@") || match(expected, text) ||
		!lo.EveryBy(lo.Keys(field.Vanilla.Values), func(value string) bool { return match(expected, value) }) {
		return
	}
	visitor.diagnostics = append(visitor.diagnostics, makeDiagnostic(tree, current.Value, "wrong-value", map[string]string{
		"name": tree.Unquoted(current.Key), "expected": describeExpected(expected),
	}))
}

// describeExpected returns the words for what a field's value must be.
func describeExpected(expected schema.Expected) string {
	switch {
	case expected.Type == schema.ValueChoice:
		return "one of " + strings.Join(expected.Choices, ", ")
	case expected.Type == schema.ValueReference && expected.Database != "":
		return "the name of a " + game.DescribeDefinition(expected.Database, false)
	case expected.Type == schema.ValueReference:
		return "a name"
	}
	return valueDescriptions[expected.Type]
}

// isWritten reports whether a key is written by another of the definitions the game keeps together with a
// definition.
func (visitor *diagnoseVisitor) isWritten(definition *schema.Definition, key string) bool {
	mods := visitor.index
	position, known := mods.FindFile(visitor.file.Path)
	winner, ok := findKept(visitor.ws, mods, schema.Reference{Database: definition.Database, Name: definition.Name}, visitor.file.Path)
	return ok && slices.ContainsFunc(winner.Merged, func(site index.Site) bool {
		if known && site.File == position && mods.Occurrence(site).Span == definition.Span {
			return false
		}
		tree, statement, err := visitor.ws.FindStatement(mods, site)
		return err == nil && hasChild(tree, statement, key)
	})
}

// isRequired reports whether every definition the game ships in a database writes a key.
func (visitor *diagnoseVisitor) isRequired(definition *schema.Definition, key string) bool {
	mods := visitor.index
	return !slices.ContainsFunc(mods.ListDefinitions(definition.Database), func(site index.Site) bool {
		if mods.File(site).Origin != game.OriginVanilla || mods.Occurrence(site).Mode != 0 {
			return false
		}
		tree, statement, err := visitor.ws.FindStatement(mods, site)
		return err == nil && !hasChild(tree, statement, key)
	})
}

// hasChild reports whether a statement has a child written with the given key.
func hasChild(tree *jomini.Tree, statement int32, key string) bool {
	_, ok := tree.FindChild(statement, key)
	return ok
}
