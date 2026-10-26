package lsp

import (
	"cmp"
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

// The questions a check answers, and the levels a check's findings have.
const (
	QuestionCompatibility = "compatibility" // how the workspace's mods touch the same things
	QuestionHealth        = "health"        // each mod's own problems
	QuestionLog           = "log"           // what the game's last run logged
	LevelSerious          = "serious"
	LevelWarning          = "warning"
	LevelInfo             = "info"
)

// A Code is one kind of problem.
type Code struct {
	ID       string `json:"id"`       // the code's name in kebab case, such as wrong-scope
	Question string `json:"question"` // compatibility, health or log
	Level    string `json:"level"`    // serious, warning or info, or "" for the game log
	Topic    string `json:"topic"`    // such as syntax or localization, or "" for the log
	Label    string `json:"label"`    // the code's title
	Message  string `json:"message"`  // a template the facts fill, such as {name}
}

// Codes holds every problem code.
var Codes = []Code{
	{ID: "unclosed-block", Question: QuestionHealth, Level: LevelWarning, Topic: "syntax",
		Label: "Unclosed block", Message: "This block is opened and never closed."},
	{ID: "unexpected-brace", Question: QuestionHealth, Level: LevelWarning, Topic: "syntax",
		Label: "Unexpected brace", Message: "This closing brace has no block to close."},
	{ID: "unclosed-string", Question: QuestionHealth, Level: LevelSerious, Topic: "syntax",
		Label: "Unclosed string", Message: "This string is opened and never closed."},
	{ID: "missing-value", Question: QuestionHealth, Level: LevelSerious, Topic: "syntax",
		Label: "Missing value", Message: "This key has an operator and no value."},
	{ID: "malformed-line", Question: QuestionHealth, Level: LevelSerious, Topic: "localization",
		Label: "Malformed line", Message: "The game cannot read this line as a localization key and its text."},
	{ID: "loc-content-before-header", Question: QuestionHealth, Level: LevelSerious, Topic: "localization",
		Label:   "Line above the header",
		Message: "This line is above the localization header, so the game does not read it."},
	{ID: "loc-unterminated-value", Question: QuestionHealth, Level: LevelSerious, Topic: "localization",
		Label:   "Text without a closing quote",
		Message: "This localization key's text has no closing quote, so it runs to the end of the line."},
	{ID: "missing-bom", Question: QuestionHealth, Level: LevelSerious, Topic: "localization",
		Label:   "Missing byte order mark",
		Message: "The game reads a localization file only with a UTF-8 byte order mark."},
	{ID: "wrong-localization-folder", Question: QuestionHealth, Level: LevelSerious, Topic: "localization",
		Label: "Wrong localization folder", Message: "The game reads the folder {right}, not {wrong}."},
	{ID: "loc-header-mismatch", Question: QuestionHealth, Level: LevelWarning, Topic: "localization",
		Label: "Header of another language", Message: "The file is named for {language}, but its header says {header}."},
	{ID: "duplicate-message", Question: QuestionHealth, Level: LevelWarning, Topic: "localization",
		Label:   "Duplicate localization key",
		Message: "The localization key {name} is defined more than once in {language}."},
	{ID: "missing-message", Question: QuestionHealth, Level: LevelWarning, Topic: "localization",
		Label:   "Missing title localization",
		Message: "The {database} {name} has no localization key for its title in the base language."},
	{ID: "coverage", Question: QuestionHealth, Level: LevelInfo, Topic: "localization",
		Label: "Language coverage", Message: "{mod} has {have} of its {of} localization keys in {name}."},
	{ID: "unresolved-reference", Question: QuestionHealth, Level: LevelSerious, Topic: "references",
		Label: "Unresolved reference", Message: "Nothing defines the {database} {name}."},
	{ID: "wrong-scope", Question: QuestionHealth, Level: LevelSerious, Topic: "values",
		Label: "Wrong scope", Message: "{name} runs in {allowed}, not in {scope}."},
	{ID: "wrong-value", Question: QuestionHealth, Level: LevelSerious, Topic: "values",
		Label: "Wrong value", Message: "{name} takes {expected}."},
	{ID: "missing-key", Question: QuestionHealth, Level: LevelSerious, Topic: "values",
		Label: "Missing key", Message: "The {database} {name} does not write {key}, which it must."},
	{ID: "unknown-key", Question: QuestionHealth, Level: LevelWarning, Topic: "values",
		Label: "Unknown key", Message: "{name} is not a key a {database} takes here."},
	{ID: "wrong-namespace", Question: QuestionHealth, Level: LevelSerious, Topic: "definitions",
		Label:   "Wrong namespace",
		Message: "The {database} {name} is not in a namespace this file declares ({namespace})."},
	{ID: "misplaced-mode", Question: QuestionHealth, Level: LevelSerious, Topic: "definitions",
		Label: "Misplaced entry mode", Message: "The {database} {name} cannot be written with {mode} here."},
	{ID: "inject-without-target", Question: QuestionHealth, Level: LevelWarning, Topic: "definitions",
		Label:   "Entry mode with nothing to change",
		Message: "The {database} {name} is written with {mode}, and no {database} {name} is loaded for it to change."},
	{ID: "duplicate-definition", Question: QuestionHealth, Level: LevelWarning, Topic: "definitions",
		Label:   "Duplicate definition",
		Message: "The {database} {name} is defined more than once, and the game keeps only one."},
	{ID: "conflicting-priority", Question: QuestionHealth, Level: LevelWarning, Topic: "definitions",
		Label: "Conflicting priority",
		Message: "{name} has the same override priority in two files. The game logs an error and keeps one copy; " +
			"which one is not documented. Give one a higher {key}."},
	{ID: "mod-for-older-version", Question: QuestionHealth, Level: LevelWarning, Topic: "descriptor",
		Label:   "Made for an older game version",
		Message: "{name} is made for game {supported}; the game is {version}."},
	{ID: "unmounted-folder", Question: QuestionHealth, Level: LevelWarning, Topic: "definitions",
		Label: "Folder the game never reads",
		Message: "{name} is outside the in_game, main_menu and loading_screen folders, " +
			"so the game never reads the files in it: {count}."},
	{ID: "contest", Question: QuestionCompatibility, Level: LevelSerious, Topic: "definitions",
		Label:   "Contested definition",
		Message: "{beaten}'s copy of {name} is never used: the game keeps {winner}'s."},
	{ID: "file-contest", Question: QuestionCompatibility, Level: LevelSerious, Topic: "definitions",
		Label: "Contested file",
		Message: "{winner} and {beaten} both ship {name}. The game reads {winner}'s, which is lower in the load order, " +
			"so {beaten}'s is never used."},
	{ID: "undecided-contest", Question: QuestionCompatibility, Level: LevelSerious, Topic: "definitions",
		Label:   "Undecided contest",
		Message: "{mods} each write {name}. Which copy the game uses, or whether they combine, is not documented: {why}."},
	{ID: "shared-message", Question: QuestionCompatibility, Level: LevelWarning, Topic: "localization",
		Label: "Localization key in several mods",
		Message: "{mods} define the localization key {name} in {language}. The game shows one copy's text, " +
			"and a copy in a replace folder wins."},
	{ID: "missing-dependency", Question: QuestionCompatibility, Level: LevelSerious, Topic: "descriptor",
		Label:   "Missing dependency",
		Message: "{mod} depends on {name}, which the workspace does not have."},
	{ID: "undeclared-dependency", Question: QuestionCompatibility, Level: LevelWarning, Topic: "references",
		Label: "Use of a mod it does not depend on",
		Message: "{name} uses {count} names that only {other} defines, and does not declare it as a dependency. " +
			"Without {other} they are undefined."},
	{ID: "override", Question: QuestionCompatibility, Level: LevelInfo, Topic: "definitions",
		Label:   "Override of the game",
		Message: "{name}: a mod and the game both define this {database}, and the game keeps {kept}."},
	{ID: "unused-copy", Question: QuestionCompatibility, Level: LevelWarning, Topic: "definitions",
		Label:   "Copy never used",
		Message: "{mod}'s copy of {name} is never used: the game keeps its own, {reason}."},
	{ID: "ignored-pair", Question: QuestionCompatibility, Level: LevelInfo, Topic: "definitions",
		Label:   "Ignored conflicts",
		Message: "Conflicts between {name} and {other} are ignored in this workspace: {count}."},
	{ID: "game-error", Question: QuestionLog, Label: "Game error", Message: "The game logged: {message}"},
}

// A Diagnostic is one problem found in a file.
type Diagnostic struct {
	Code  string            `json:"code"`
	Range Range             `json:"range"`
	Facts map[string]string `json:"facts"`
}

// valueDescriptions holds the words that describe each value type.
var valueDescriptions = map[schema.ValueType]string{
	schema.ValueBool: "yes or no", schema.ValueNumber: "a number", schema.ValueDate: "a date",
	schema.ValueColor: "a color", schema.ValueCall: "a data call",
}

// Diagnose returns the problems of a file, judged against the open workspace. It leaves out the checks the user turned
// off.
func (lsp *Service) Diagnose(path string) ([]Diagnostic, error) {
	ws, err := lsp.Workspace.Session()
	if err != nil {
		return nil, err
	}
	diagnostics, err := DiagnoseFile(ws, ws.Mods.Load(), path)
	return lsp.listEnabled(ws.ID, diagnostics), err
}

// DescribeReference returns the facts that say which name a problem is about.
func DescribeReference(reference schema.Reference) map[string]string {
	return map[string]string{"name": reference.Name, "database": game.DescribeDefinition(reference.Database, false)}
}

// DiagnoseFile returns the problems of a file in a workspace. A file of the game has its parse errors only. A file that
// is neither script nor localization has none, unless it sits in a misspelled localization folder.
func DiagnoseFile(ws *workspace.Session, mods *index.Index, path string) ([]Diagnostic, error) {
	place := ws.Locate(mods, path)
	if place.Format == files.FormatText && !place.ReadOnly && strings.HasSuffix(strings.ToLower(place.Rel), ".yml") {
		// the game reads nothing under a misspelled folder, so only the folder is judged
		tree, err := ws.LoadTree(path)
		if err != nil {
			return nil, err
		}
		return diagnoseFolders(tree, place), nil
	}
	if place.Format != files.FormatLocalization && !place.Format.IsScript() {
		return []Diagnostic{}, nil
	}
	tree, err := ws.LoadTree(path)
	if err != nil {
		return nil, err
	}
	if place.Format == files.FormatLocalization {
		return diagnoseMessages(ws, mods, tree, place), nil
	}
	diagnostics := toDiagnostics(tree, tree.Errors)
	if !place.ReadOnly {
		position, listed := mods.FindFile(path)
		visitor := &diagnoseVisitor{ws: ws, index: mods, file: place.File, position: position, listed: listed}
		ws.Schema.Walk(tree, place.File, visitor)
		diagnostics = append(diagnostics, visitor.diagnostics...)
	}
	return diagnostics, nil
}

// listEnabled drops the diagnostics of the checks the user turned off in a workspace.
func (lsp *Service) listEnabled(workspaceID string, diagnostics []Diagnostic) []Diagnostic {
	setting, _ := lsp.Workspace.Config.LookupSetting(workspaceID, "ide.checks")
	disabled := lo.Map(strings.Split(setting, ","), func(id string, _ int) string { return strings.TrimSpace(id) })
	return slices.DeleteFunc(diagnostics, func(diagnostic Diagnostic) bool {
		return slices.Contains(disabled, diagnostic.Code)
	})
}

// toDiagnostics returns a tree's parse errors as diagnostics.
func toDiagnostics(tree *jomini.Tree, errors []jomini.Error) []Diagnostic {
	return lo.Map(errors, func(parseError jomini.Error, _ int) Diagnostic {
		return makeDiagnostic(tree, parseError.Span, parseError.Code, map[string]string{})
	})
}

// makeDiagnostic returns a diagnostic over a span of a tree's text.
func makeDiagnostic(tree *jomini.Tree, span jomini.Span, code string, facts map[string]string) Diagnostic {
	return Diagnostic{Code: code, Range: ToRange(tree.Src, tree.Lines, span), Facts: facts}
}

// diagnoseMessages returns the problems of a localization file. A file of the game has its parse errors only.
func diagnoseMessages(ws *workspace.Session, mods *index.Index, tree *jomini.Tree, place workspace.Place) []Diagnostic {
	file := loc.Parse(tree.Src, place.Path)
	diagnostics := toDiagnostics(tree, file.Errors)
	if place.ReadOnly {
		return diagnostics
	}
	diagnostics = append(diagnostics, diagnoseLocalizationFile(tree, file, place)...)
	if _, replaces := schema.FindLocReplacement(ws.Game, place.Rel, file.Language); replaces {
		return diagnostics
	}
	position, known := mods.FindFile(place.Path)
	counts := lo.CountValuesBy(file.Messages, func(message loc.Message) string { return message.Key })
	for _, message := range file.Messages {
		others := lo.CountBy(mods.ListMessages(message.Key), func(site index.Site) bool {
			other := mods.File(site)
			_, replaces := schema.FindLocReplacement(ws.Game, other.Rel, file.Language)
			repeats := other.Origin == place.Origin || other.Origin == game.OriginVanilla && ws.Game.LogsRepeatedKey
			return (!known || site.File != position) && repeats && mods.Language(site) == file.Language &&
				mods.IsLoaded(site.File) && !replaces
		})
		if counts[message.Key]+others > 1 {
			diagnostics = append(diagnostics, makeDiagnostic(tree, message.KeySpan, "duplicate-message",
				map[string]string{"name": message.Key, "language": file.Language}))
		}
	}
	return diagnostics
}

// diagnoseLocalizationFile returns the problems of a mod's localization file as a whole.
func diagnoseLocalizationFile(tree *jomini.Tree, file *loc.File, place workspace.Place) []Diagnostic {
	diagnostics := []Diagnostic{}
	add := func(code string, facts map[string]string) {
		diagnostics = append(diagnostics, makeDiagnostic(tree, jomini.Span{}, code, facts))
	}
	if suffix := loc.FindSuffixLanguage(place.Path); suffix != "" && file.Language != "" &&
		!strings.EqualFold(suffix, file.Language) {
		add("loc-header-mismatch", map[string]string{"language": suffix, "header": file.Language})
	}
	diagnostics = append(diagnostics, diagnoseFolders(tree, place)...)
	if disk, err := files.ReadText(place.Path); err != nil || disk.Encoding != files.EncodingUTF8BOM {
		add("missing-bom", map[string]string{})
	}
	return diagnostics
}

// diagnoseFolders returns a problem for each misspelled localization folder in a file's path.
func diagnoseFolders(tree *jomini.Tree, place workspace.Place) []Diagnostic {
	diagnostics := []Diagnostic{}
	folders := strings.Split(strings.ToLower(place.Rel), "/")
	for _, folder := range folders[:len(folders)-1] {
		if right, misspelled := game.MisspelledFolders[folder]; misspelled {
			diagnostics = append(diagnostics, makeDiagnostic(tree, jomini.Span{}, "wrong-localization-folder",
				map[string]string{"wrong": folder, "right": right}))
		}
	}
	return diagnostics
}

// A diagnoseVisitor collects the problems of a script file.
type diagnoseVisitor struct {
	ws          *workspace.Session
	index       *index.Index // the mods' index
	file        files.File
	position    int32 // where the index lists the file
	listed      bool  // whether the index lists the file
	diagnostics []Diagnostic
}

// Enter records the problems of a statement.
func (visitor *diagnoseVisitor) Enter(tree *jomini.Tree, statement int32, context *schema.Context) bool {
	visitor.diagnoseReferences(tree, context)
	visitor.diagnoseKey(tree, statement, context)
	visitor.diagnoseScope(tree, statement, context)
	visitor.diagnoseValue(tree, statement, context)
	return true
}

// Leave records each key a definition must write and does not, in alphabetical order.
func (visitor *diagnoseVisitor) Leave(tree *jomini.Tree, statement int32, context *schema.Context) {
	definition := context.Definition
	if !context.Defines || definition.Mode != "" {
		return
	}
	database := visitor.ws.Schema.Databases[definition.Database]
	if database == nil || context.Field != database.Definition {
		return
	}
	var missing []string
	for key, field := range context.Field.Children {
		if field.Required.Value == "always" && field.Required.Source.CanAccuse() && !strings.HasPrefix(key, "<") &&
			!hasChild(tree, statement, key) && !visitor.isWritten(definition, key) && isRequired(context.Field, field) {
			missing = append(missing, key)
		}
	}
	slices.Sort(missing)
	for _, key := range missing {
		facts := DescribeReference(schema.Reference{Database: definition.Database, Name: definition.Name})
		facts["key"] = key
		visitor.diagnostics = append(visitor.diagnostics, makeDiagnostic(tree, definition.Span, "missing-key", facts))
	}
}

// diagnoseReferences records each name the statement refers to that nothing defines. A name that could be in several
// databases is recorded only if none has it.
func (visitor *diagnoseVisitor) diagnoseReferences(tree *jomini.Tree, context *schema.Context) {
	for position, reference := range context.References {
		if slices.ContainsFunc(context.References[:position], func(other schema.Reference) bool { return other.Span == reference.Span }) {
			continue
		}
		alternatives := lo.Filter(context.References, func(other schema.Reference, _ int) bool { return other.Span == reference.Span })
		if lo.EveryBy(alternatives, canJudge) &&
			!lo.SomeBy(alternatives, visitor.isKnown) {
			databases := lo.Map(alternatives, func(other schema.Reference, _ int) string {
				return game.DescribeDefinition(other.Database, false)
			})
			visitor.diagnostics = append(visitor.diagnostics, makeDiagnostic(tree, reference.Span, "unresolved-reference",
				map[string]string{"name": reference.Name, "database": strings.Join(lo.Uniq(databases), " or ")}))
		}
	}
}

// canJudge reports whether a reference can be judged dangling.
func canJudge(reference schema.Reference) bool {
	_, runtime := game.LinkMakers[reference.Database]
	_, _, prefixed := jomini.SplitPrefixed(reference.Name)
	return reference.Database != "" && !runtime && reference.Caller != game.AnyCall && reference.Caller != game.GUI.TagCalls &&
		!reference.Inferred && !prefixed && !strings.Contains(reference.Name, "$")
}

// isKnown reports whether the game knows a reference's name.
func (visitor *diagnoseVisitor) isKnown(reference schema.Reference) bool {
	mods := visitor.index
	if reference.Database == game.File {
		if _, ok := mods.ResolveTexture(reference.Name); ok {
			return true
		}
	} else if sites := mods.ResolveLocal(reference, visitor.file.Path); len(sites) > 0 {
		return slices.ContainsFunc(sites, func(site index.Site) bool {
			file := mods.File(site)
			hiding, _, ok := mods.FindLoaded(file.Rel)
			return file.Key == visitor.file.Key || mods.IsLoaded(site.File) || ok && hiding.Origin != visitor.file.Origin
		})
	}
	return lo.SomeBy(mods.ListUses(reference.Database, reference.Name), func(site index.Site) bool {
		return mods.File(site).Origin == game.OriginVanilla
	})
}

// diagnoseKey records what is wrong with the key of a statement.
func (visitor *diagnoseVisitor) diagnoseKey(tree *jomini.Tree, statement int32, context *schema.Context) {
	current := tree.Statements[statement]
	switch {
	case context.Defines:
		visitor.diagnoseDefinition(tree, statement, context.Definition)
	case current.Key.IsEmpty() || context.Category != schema.CategoryNone || context.Field != nil:
		// a key with a meaning of its own is not an unknown key
	case current.Parent == 0 && jomini.IsEventID(tree.Unquoted(current.Key)):
		visitor.diagnoseNamespace(tree, current)
	case context.Definition != nil:
		visitor.diagnoseUnknown(tree, statement, context)
	}
}

// diagnoseDefinition records the problems of a definition's entry mode and of a name it shares.
func (visitor *diagnoseVisitor) diagnoseDefinition(tree *jomini.Tree, statement int32, definition *schema.Definition) {
	database := visitor.ws.Schema.Databases[definition.Database]
	if database == nil || definition.Alias {
		return
	}
	facts := DescribeReference(schema.Reference{Database: definition.Database, Name: definition.Name})
	modes, top := database.Precedence.Modes, tree.Statements[statement].Parent == 0
	fits := modes == "entry" && top || modes == "wrapped" && !top
	switch {
	case definition.Mode != "" && modes != "" && !fits:
		facts["mode"] = definition.Mode
		mode := jomini.Span{Start: definition.Span.Start - len(definition.Mode) - 1, End: definition.Span.Start}
		visitor.diagnostics = append(visitor.diagnostics, makeDiagnostic(tree, mode, "misplaced-mode", facts))
	case definition.Mode != "" && visitor.isOrphan(definition, database.Precedence):
		facts["mode"] = definition.Mode
		visitor.diagnostics = append(visitor.diagnostics, makeDiagnostic(tree, definition.Span, "inject-without-target", facts))
	case definition.Mode == "" && !definition.Local:
		if code := visitor.classifyDuplicate(definition, database.Precedence); code != "" {
			if database.Precedence.PriorityKey != "" {
				facts["key"] = database.Precedence.PriorityKey
			}
			visitor.diagnostics = append(visitor.diagnostics, makeDiagnostic(tree, definition.Span, code, facts))
		}
	}
}

// isOrphan reports whether a definition written with an entry mode has nothing to change. An optional mode, or one
// that creates, never is.
func (visitor *diagnoseVisitor) isOrphan(definition *schema.Definition, precedence game.Precedence) bool {
	mode, _ := game.LookupEntryMode(definition.Mode)
	if mode.Optional || mode.Creates {
		return false
	}
	mods := visitor.index
	reference := schema.Reference{Database: definition.Database, Name: definition.Name}
	_, acts := mods.FindWinner(mods.ResolveLocal(reference, visitor.file.Path), precedence)
	return !acts
}

// isOwn reports whether a site is a definition as written in the file being walked.
func (visitor *diagnoseVisitor) isOwn(site index.Site, definition *schema.Definition) bool {
	return visitor.listed && site.File == visitor.position && visitor.index.Occurrence(site).Span == definition.Span
}

// classifyDuplicate returns the code of the problem a definition has because others share its name, or "" for none.
func (visitor *diagnoseVisitor) classifyDuplicate(definition *schema.Definition, precedence game.Precedence) string {
	mods := visitor.index
	reference := schema.Reference{Database: definition.Database, Name: definition.Name}
	defined := lo.Filter(mods.ResolveLocal(reference, visitor.file.Path), func(site index.Site, _ int) bool {
		return mods.Occurrence(site).Mode == 0 && !mods.Occurrence(site).Alias
	})
	sites := lo.Filter(defined, func(site index.Site, _ int) bool { return mods.IsLoaded(site.File) })
	own := slices.IndexFunc(sites, func(site index.Site) bool { return visitor.isOwn(site, definition) })
	if len(sites) < 2 || own < 0 || precedence.KeepsEvery() {
		return ""
	}
	if precedence.Keeps == "highest" {
		priority := mods.Occurrence(sites[own]).Priority
		if lo.CountBy(sites, func(site index.Site) bool { return mods.Occurrence(site).Priority == priority }) > 1 {
			return "conflicting-priority"
		}
		return ""
	}
	byGame := lo.CountBy(defined, func(site index.Site) bool { return mods.File(site).Origin == game.OriginVanilla })
	winner, kept := findKept(visitor.ws, mods, reference, visitor.file.Path)
	if kept && winner.Reason != index.Unsettled && byGame < 2 && winner.Site != sites[own] &&
		mods.Occurrence(winner.Site).Mode == 0 && mods.File(winner.Site).Origin == visitor.file.Origin {
		return "duplicate-definition"
	}
	return ""
}

// diagnoseNamespace records an event at the top of an events file that is outside the file's namespaces.
func (visitor *diagnoseVisitor) diagnoseNamespace(tree *jomini.Tree, current jomini.Statement) {
	database, ok := visitor.ws.Schema.FindDatabase(visitor.file.Rel)
	if !ok || database.Row.Houses != "events" {
		return
	}
	facts := DescribeReference(schema.Reference{Database: database.Name, Name: tree.Unquoted(current.Key)})
	facts["namespace"] = cmp.Or(strings.Join(visitor.ws.Schema.ListNamespaces(tree, visitor.file), ", "), "none")
	visitor.diagnostics = append(visitor.diagnostics, makeDiagnostic(tree, current.Key, "wrong-namespace", facts))
}

// diagnoseUnknown records a key its block does not take. A key that is not a plain name is not judged.
func (visitor *diagnoseVisitor) diagnoseUnknown(tree *jomini.Tree, statement int32, context *schema.Context) {
	key := tree.Statements[statement].Key
	if visitor.ws.Schema.Databases[context.Database] == nil || !jomini.IsIdentifier(tree.Unquoted(key)) {
		return
	}
	holder := context.Parent
	if holder.Field == nil || holder.Shape == game.ShapeUnknown || !holder.Field.Source.CanAccuse() || holder.Field.Vanilla.Count > 0 {
		return
	}
	facts := DescribeReference(schema.Reference{Database: context.Database, Name: tree.Unquoted(key)})
	visitor.diagnostics = append(visitor.diagnostics, makeDiagnostic(tree, key, "unknown-key", facts))
}

// diagnoseScope records a command or a link written in a scope the game does not allow for it. It records none at the
// top of a definition or outside effects and triggers.
func (visitor *diagnoseVisitor) diagnoseScope(tree *jomini.Tree, statement int32, context *schema.Context) {
	if context.Category != schema.CategoryLink && context.Category != schema.CategoryCommand {
		return
	}
	span := tree.Statements[statement].Key
	key, command := tree.Unquoted(span), context.Command
	if context.Category == schema.CategoryLink {
		key = jomini.SplitChain(key)[0]
	}
	if command == nil || len(command.Scopes) == 0 {
		return
	}
	// a definition's starting scope is only its database's claim, so only a followed scope counts
	scope, followed := context.Parent.Scope, len(context.Parent.Previous)
	if scope == "" || followed == 0 || slices.Contains(command.Scopes, scope) || slices.Contains(command.VanillaScopes, scope) ||
		!isScripted(context) {
		return
	}
	allowed := lo.Uniq(slices.Concat(command.Scopes, command.VanillaScopes))
	visitor.diagnostics = append(visitor.diagnostics, makeDiagnostic(tree, span, "wrong-scope", map[string]string{
		"name": key, "scope": scope, "allowed": strings.Join(allowed, ", "),
	}))
}

// isScripted reports whether the block that holds a statement holds effects or triggers. The arguments of a command are
// not effects or triggers.
func isScripted(context *schema.Context) bool {
	holder := context.Parent
	if holder.Shape != game.ShapeEffects && holder.Shape != game.ShapeTriggers {
		return false
	}
	if holder.Category != schema.CategoryCommand {
		return true
	}
	return holder.Command != nil && (holder.Command.Target != "" || holder.Command.Lists)
}

// diagnoseValue records a value that is not the type its key takes. References, and names where a number is expected,
// are not judged.
func (visitor *diagnoseVisitor) diagnoseValue(tree *jomini.Tree, statement int32, context *schema.Context) {
	field, current := context.Field, tree.Statements[statement]
	if context.Category != schema.CategoryField || field == nil || !field.Value.IsStated() ||
		!field.Value.Source.CanAccuse() || current.IsBlock() || current.Value.IsEmpty() ||
		context.ValueGroup == schema.CategoryReference {
		return
	}
	// MatchValue cannot judge a path, free text or a block's short form
	expected, text := field.Value.Value, tree.Text(current.Value)
	if expected.Database == game.File || expected.Type == schema.ValueText || expected.Type == schema.ValueBlock {
		return
	}
	// the game reads a script value, or a trigger that returns a number, where a number is expected
	readsScriptValue := expected.Type == schema.ValueNumber && jomini.IsIdentifier(tree.Unquoted(current.Value))
	match := visitor.ws.Schema.MatchValue
	if readsScriptValue || strings.ContainsAny(text, "[$@") || match(expected, text) ||
		!lo.EveryBy(lo.Keys(field.Vanilla.Values), func(value string) bool { return match(expected, value) }) {
		return
	}
	visitor.diagnostics = append(visitor.diagnostics, makeDiagnostic(tree, current.Value, "wrong-value", map[string]string{
		"name": tree.Unquoted(current.Key), "expected": describeExpected(expected),
	}))
}

// describeExpected returns the words for what a value must be.
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

// isWritten reports whether another definition the game merges with this one writes a key.
func (visitor *diagnoseVisitor) isWritten(definition *schema.Definition, key string) bool {
	mods := visitor.index
	winner, ok := findKept(visitor.ws, mods, schema.Reference{Database: definition.Database, Name: definition.Name}, visitor.file.Path)
	return ok && slices.ContainsFunc(winner.Merged, func(site index.Site) bool {
		if visitor.isOwn(site, definition) {
			return false
		}
		tree, statement, err := visitor.ws.FindStatement(mods, site)
		return err == nil && hasChild(tree, statement, key)
	})
}

// isRequired reports whether every definition the game ships writes a field.
func isRequired(root, field *schema.Field) bool {
	return field.Vanilla.Definitions == root.Vanilla.Definitions
}

// hasChild reports whether a statement has a child with a key.
func hasChild(tree *jomini.Tree, statement int32, key string) bool {
	_, ok := tree.FindChild(statement, key)
	return ok
}
