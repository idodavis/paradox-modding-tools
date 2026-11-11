// diagnostics.go finds the problems in a file. It walks a script with the schema and the index and reports each problem
// as a diagnostic: a code, a level and the facts its message is made from. It also holds the table of codes.

package lsp

import (
	"cmp"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/samber/lo"
	"golang.org/x/sync/errgroup"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/loc"
	"paradox-modding-tools/internal/schema"
	"paradox-modding-tools/services/workspace"
)

// The questions a check answers, and the levels a check's findings have, by how much the game loses.
const (
	QuestionCompatibility = "compatibility" // how the workspace's mods touch the same things
	QuestionHealth        = "health"        // each mod's own problems
	QuestionLog           = "log"           // what the game's last run logged
	LevelCritical         = "critical"      // the game loses a whole file, folder, definition or mod
	LevelError            = "error"         // the game skips one statement, line or value and keeps running
	LevelWarning          = "warning"       // it may be fine, or only shows a player the wrong text
	LevelInfo             = "info"          // only how things are
)

// A Level is one level of a finding: its wording, its icon and color on the page, whether it counts as a problem and
// whether the game loses something.
type Level struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Icon    string `json:"icon"`  // the page's icon
	Color   string `json:"color"` // the page's color
	Noun    string `json:"noun"`  // such as critical problem
	Nouns   string `json:"nouns"`
	Problem bool   `json:"problem"` // it counts as a problem; info does not
	Fix     bool   `json:"fix"`     // the game loses something, so a mod needs a fix
	Cost    string `json:"cost"`    // what the player loses, in a sentence
}

// Levels holds the levels of a finding, the worst first.
var Levels = []Level{
	{ID: LevelCritical, Label: "Critical", Icon: "i-lucide-octagon-x", Color: "error", Noun: "critical problem",
		Nouns: "critical problems", Problem: true, Fix: true, Cost: "The game may fail to read the rest of the file."},
	{ID: LevelError, Label: "Error", Icon: "i-lucide-circle-x", Color: "warning", Noun: "error", Nouns: "errors",
		Problem: true, Fix: true, Cost: "The game skips this line and keeps running, so the mod plays without it."},
	{ID: LevelWarning, Label: "Warning", Icon: "i-lucide-triangle-alert", Color: "yellow", Noun: "warning",
		Nouns: "warnings", Problem: true, Cost: "The game reads it, but it may not do what you meant."},
	{ID: LevelInfo, Label: "Info", Icon: "i-lucide-info", Color: "info", Noun: "info", Nouns: "info",
		Cost: "Nothing breaks; it is a note."},
}

// unresolvedCode is the code of a name nothing defines that is of no narrower kind.
const unresolvedCode = "unresolved-reference"

// keyFacts are the codes of a localization key with no base-language text, each with the fact that names the key.
var keyFacts = map[string]string{"missing-message": "key", "unknown-loc-key": "name"}

// elsewhereMessage is the message for a name that the file's mod does not define but a mod it does not depend on does.
const elsewhereMessage = "{mod} uses this {database}, but the game doesn't define it: {other} does. {mod} doesn't " +
	"list {other} as a dependency, so in a game without {other} the {database} doesn't exist, and every place that " +
	"uses it finds nothing."

// A Code is one kind of problem.
type Code struct {
	ID          string `json:"id"`                  // the code's name in kebab case, such as wrong-scope
	Question    string `json:"question"`            // compatibility, health or log
	Level       string `json:"level"`               // critical, error, warning or info, or "" for the game log
	Topic       string `json:"topic"`               // such as syntax or localization, or "" for the log
	Label       string `json:"label"`               // the code's title
	Description string `json:"description"`         // one plain sentence on what the check looks for
	Message     string `json:"message"`             // a template the facts fill, such as {name}
	Advice      string `json:"advice"`              // what to do about it, in plain words; it repeats no fact of the message
	Elsewhere   string `json:"elsewhere,omitempty"` // the message where a mod the file's mod does not depend on defines the name; "" if the code has none
	HealthOnly  bool   `json:"healthOnly"`          // the editor never gives the code and only Health's report finds it
}

// Codes holds every problem code.
var Codes = []Code{
	{ID: "unclosed-block", Question: QuestionHealth, Level: LevelWarning, Topic: "syntax",
		Label: "Unclosed block", Description: "A block that is opened and never closed.", Message: "This block is opened and never closed.",
		Advice: "Add the closing brace where the block ends."},
	{ID: "too-deep", Question: QuestionHealth, Level: LevelWarning, Topic: "syntax",
		Label: "Block nested too deep", Description: "A block nested more than 1,000 levels deep.", Message: "This block is nested more than 1,000 levels deep, so nothing inside it is checked.",
		Advice: "Close the blocks above it that are left open, or nest it less deeply."},
	{ID: "unexpected-brace", Question: QuestionHealth, Level: LevelCritical, Topic: "syntax",
		Label: "Unexpected brace", Description: "A closing brace with no block to close.", Message: "This closing brace has no block to close.",
		Advice: "Delete this brace, or add the opening brace it belongs to."},
	{ID: "unclosed-string", Question: QuestionHealth, Level: LevelCritical, Topic: "syntax",
		Label: "Unclosed string", Description: "A string that is opened and never closed.", Message: "This string is opened and never closed.",
		Advice: "Add the closing quote."},
	{ID: "missing-value", Question: QuestionHealth, Level: LevelError, Topic: "syntax",
		Label: "Missing value", Description: "A key with an operator and no value after it.", Message: "This key has an operator and no value.",
		Advice: "Write the value after the operator, or delete the key."},
	{ID: "else-without-if", Question: QuestionHealth, Level: LevelError, Topic: "syntax",
		Label: "An else with no if before it", Description: "An else that does not come right after an if.", Message: "{name} must come right after {follows}.",
		Advice: "Move it right after the block it belongs to, or delete it."},
	{ID: "malformed-line", Question: QuestionHealth, Level: LevelError, Topic: "localization",
		Label: "Malformed line", Description: "A localization line the game cannot read as a key and its text.", Message: "The game cannot read this line as a localization key and its text.",
		Advice: "Write the line as the key, a colon and the text in quotes."},
	{ID: "loc-content-before-header", Question: QuestionHealth, Level: LevelError, Topic: "localization",
		Label:       "Line above the header",
		Description: "A localization line written above the language header.",
		Message:     "This line is above the localization header, so the game does not read it.",
		Advice:      "Move the line below the header, or delete it."},
	{ID: "loc-unterminated-value", Question: QuestionHealth, Level: LevelError, Topic: "localization",
		Label:       "Text without a closing quote",
		Description: "A localization text that has no closing quote.",
		Message:     "This localization key's text has no closing quote, so it runs to the end of the line.",
		Advice:      "Add the closing quote at the end of the text."},
	{ID: "missing-bom", Question: QuestionHealth, Level: LevelCritical, Topic: "localization",
		Label:       "Missing byte order mark",
		Description: "A localization file saved without a UTF-8 byte order mark.",
		Message:     "The game reads a localization file only with a UTF-8 byte order mark.",
		Advice:      "Save the file again as UTF-8 with a byte order mark."},
	{ID: "wrong-localization-folder", Question: QuestionHealth, Level: LevelCritical, Topic: "localization",
		Label: "Wrong localization folder", Description: "A localization file in a folder the game does not read.", Message: "The game reads the folder {right}, not {wrong}.",
		Advice: "Move the file into the folder the game reads."},
	{ID: "loc-header-mismatch", Question: QuestionHealth, Level: LevelWarning, Topic: "localization",
		Label: "Header of another language", Description: "A localization file whose name and header name different languages.", Message: "The file is named for {language}, but its header says {header}.",
		Advice: "Make the file name and the header use the same language."},
	{ID: "duplicate-message", Question: QuestionHealth, Level: LevelWarning, Topic: "localization",
		Label:       "Duplicate localization key",
		Description: "A localization key defined more than once in one language.",
		Message:     "The localization key {name} is defined more than once in {language}.",
		Advice:      "Delete the extra lines, or give each its own key."},
	{ID: "missing-message", Question: QuestionHealth, Level: LevelWarning, Topic: "localization",
		Label:       "Missing title localization",
		Description: "A definition with no localization key for its title in the base language.",
		Message:     "The {database} {name} has no localization key for its title in the base language.",
		Advice:      "Add the key and its text to your mod's localization file."},
	{ID: "unknown-loc-key", Question: QuestionHealth, Level: LevelWarning, Topic: "localization",
		Label: "Unknown localization key", Description: "A localization key that nothing defines.", Message: "Nothing defines the localization key {name}.",
		Advice: "Add the key and its text to your mod's localization file, or fix the spelling here. " +
			"If another mod defines it, list that mod as a dependency.",
		Elsewhere: elsewhereMessage},
	{ID: "loc-markup", Question: QuestionHealth, Level: LevelWarning, Topic: "localization",
		Label: "Unknown text markup", Description: "Text markup in a localization text that the game does not read.", Message: "This is not text markup the game reads: {why}.",
		Advice: "Fix the spelling of the markup, or take it out."},
	{ID: "coverage", Question: QuestionHealth, Level: LevelInfo, Topic: "localization", HealthOnly: true,
		Label: "Language coverage", Description: "How many of a mod's localization keys each language has.", Message: "{mod} has {have} of its {of} localization keys in {name}.",
		Advice: "Translate the keys that are missing, or leave them if the mod does not need that language."},
	{ID: unresolvedCode, Question: QuestionHealth, Level: LevelError, Topic: "references",
		Label: "Unresolved reference", Description: "A name that nothing defines.", Message: "Nothing defines the {database} {name}.",
		Advice: "Fix the spelling, or add the missing one to your mod. " +
			"If another mod defines it, list that mod as a dependency. " +
			"If the game used to define it, a patch may have removed it: see Patch Notes.",
		Elsewhere: elsewhereMessage},
	{ID: "unknown-data-call", Question: QuestionHealth, Level: LevelWarning, Topic: "references",
		Label: "Unknown data function", Description: "A data function that its type does not have.", Message: "{type} has no data function {name}.",
		Advice: "Fix the spelling, or use a function the type has."},
	{ID: "wrong-scope", Question: QuestionHealth, Level: LevelError, Topic: "values",
		Label: "Wrong scope", Description: "A key used in a scope where it does not work.", Message: "{name} runs in {allowed}, not in {scope}.",
		Advice: "Move the line to a scope where it works, or switch to a scope that allows it first."},
	{ID: "wrong-value", Question: QuestionHealth, Level: LevelError, Topic: "values",
		Label: "Wrong value", Description: "A value of another kind than its key takes.", Message: "{name} takes {expected}.",
		Advice: "Write a value of the kind the key takes."},
	{ID: "invalid-date", Question: QuestionHealth, Level: LevelError, Topic: "values",
		Label: "Impossible date", Description: "A date with a month or a day that does not exist.", Message: "{name} is no date: a month runs from 1 to 12, and a day from 1 to 31.",
		Advice: "Write a real date as year.month.day."},
	{ID: "missing-key", Question: QuestionHealth, Level: LevelError, Topic: "values",
		Label: "Missing key", Description: "A definition that leaves out a key it must write.", Message: "The {database} {name} does not write {key}, which it must.",
		Advice: "Add the key to the entry."},
	{ID: "unknown-key", Question: QuestionHealth, Level: LevelWarning, Topic: "values",
		Label: "Unknown key", Description: "A key that the block it is written in does not take.", Message: "{name} is not a key a {database} takes here.",
		Advice: "Fix the spelling, or delete the key."},
	{ID: "unused-argument", Question: QuestionHealth, Level: LevelWarning, Topic: "values",
		Label: "Argument not read or not given", Description: "A call and the definition it calls that disagree about an argument.", Message: "The argument {name} {why} {callee}.",
		Advice: "Fix the argument's spelling, or add or delete it so the call and the definition match."},
	{ID: "wrong-namespace", Question: QuestionHealth, Level: LevelCritical, Topic: "definitions",
		Label:       "Wrong namespace",
		Description: "An event whose name is not in a namespace its file declares.",
		Message:     "The {database} {name} is not in a namespace this file declares ({namespace}).",
		Advice:      "Declare the namespace at the top of the file, or rename the event to start with one the file declares."},
	{ID: "misplaced-mode", Question: QuestionHealth, Level: LevelCritical, Topic: "definitions",
		Label: "Misplaced entry mode", Description: "An entry mode, such as REPLACE, where the game does not take one.", Message: "The {database} {name} cannot be written with {mode} here.",
		Advice: "Delete the mode."},
	{ID: "inject-without-target", Question: QuestionHealth, Level: LevelCritical, Topic: "definitions",
		Label:       "Entry mode with nothing to change",
		Description: "An entry mode written for a name that no earlier entry defines.",
		Message:     "The {database} {name} is written with {mode}, and no {database} {name} is loaded for it to change.",
		Advice:      "Fix the name, or load the mod that defines it before this one."},
	{ID: "conflicting-priority", Question: QuestionHealth, Level: LevelCritical, Topic: "definitions",
		Label:       "Conflicting priority",
		Description: "Two files that give one name the same override priority.",
		Message: "{name} has the same override priority in two files. The game logs an error and keeps one copy; " +
			"which one is not documented. Give one a higher {key}.",
		Advice: "Decide which copy should win and raise its number."},
	{ID: "unmounted-folder", Question: QuestionHealth, Level: LevelWarning, Topic: "definitions", HealthOnly: true,
		Label:       "Folder the game never reads",
		Description: "Files in a folder the game never reads.",
		Message: "This folder is outside the in_game, main_menu and loading_screen folders, " +
			"so the game never reads the files in it: {count}.",
		Advice: "Move the files into one of those folders."},
	{ID: "supported-version", Question: QuestionHealth, Level: LevelWarning, Topic: "descriptor",
		Label: "Made for another game version", Description: "A mod made for another game version than the installed one.", Message: "{mod} is made for game {supported}; the game is {version}.",
		Advice: "Test the mod in this version, then set the version it supports to match."},
	{ID: "contest", Question: QuestionCompatibility, Level: LevelCritical, Topic: "definitions", HealthOnly: true,
		Label:       "Contested definition",
		Description: "A name that two or more mods define, of which the game keeps one copy.",
		Message: "{winner} and {beaten} each define this {database}. The game uses only {winner}'s copy, so " +
			"{beaten}'s never takes effect.",
		Advice: "Rename one of the two copies, or delete the one you do not want."},
	{ID: "file-contest", Question: QuestionCompatibility, Level: LevelCritical, Topic: "definitions", HealthOnly: true,
		Label:       "Contested file",
		Description: "A file that two or more mods both ship.",
		Message: "{winner} and {beaten} both ship this file. The game reads {winner}'s, which is lower in the load order, " +
			"so {beaten}'s is never used.",
		Advice: "Rename your file, or delete the copy you do not want."},
	{ID: "undecided-contest", Question: QuestionCompatibility, Level: LevelWarning, Topic: "definitions", HealthOnly: true,
		Label:       "Copy the game may not keep",
		Description: "A name written by several mods where no source says which copy the game keeps.",
		Message:     "{mods} each write this {database}. Which copy the game uses, or whether they combine, is not documented: {why}.",
		Advice: "PMT can't tell which copy the game keeps here: no source says how this folder combines copies. " +
			"Check it in the game."},
	{ID: "shared-message", Question: QuestionCompatibility, Level: LevelWarning, Topic: "localization", HealthOnly: true,
		Label:       "Localization key in several mods",
		Description: "A localization key that two or more mods define in one language.",
		Message: "{mods} define this localization key in {language}. The game shows one copy's text, " +
			"and a copy in a replace folder wins.",
		Advice: "Give each mod its own key."},
	{ID: "missing-dependency", Question: QuestionCompatibility, Level: LevelCritical, Topic: "descriptor", HealthOnly: true,
		Label:       "Missing dependency",
		Description: "A mod that another mod depends on and the workspace does not have.",
		Message:     "{mod} depends on {name}, which the workspace does not have.",
		Advice:      "Add that mod to the workspace, or delete it from the descriptor."},
	{ID: "override", Question: QuestionCompatibility, Level: LevelInfo, Topic: "definitions", HealthOnly: true,
		Label:       "Override of the game",
		Description: "A name or file that a mod writes and the game also has.",
		Message:     "A mod and the game both define this {database}, and {kept}.",
		Advice:      "Check that the copy the game keeps is the one you want."},
	{ID: "removed-names", Question: QuestionHealth, Level: LevelCritical, Topic: "definitions", HealthOnly: true,
		Label:       "Replaced file leaves out entries in use",
		Description: "A mod's copy of a game file that leaves out entries the mods still use.",
		Message: "{mod} replaces the game's copy of this file with its own, which leaves out {removed} of the " +
			"{total} {definitions} the game's copy defines. Still used and defined nowhere else: {count}. Each of " +
			"their uses, {uses} in all, now finds nothing.",
		Advice: "Put the missing entries back into your copy, or stop using them."},
	{ID: "unused-copy", Question: QuestionHealth, Level: LevelCritical, Topic: "definitions", HealthOnly: true,
		Label:       "Copy never used",
		Description: "A mod's copy of a name that the game never uses, because it keeps its own.",
		Message:     "{beaten}'s copy is never used: the game keeps its own.",
		Advice:      "Delete the copy, or rename it if it should be a new entry."},
	{ID: "ignored-pair", Question: QuestionCompatibility, Level: LevelInfo, Topic: "definitions", HealthOnly: true,
		Label:       "Ignored conflicts",
		Description: "Conflicts between two mods that you chose to ignore.",
		Message:     "Conflicts between {name} and {other} are ignored in this workspace: {count}.",
		Advice:      "Nothing to fix. Stop ignoring the pair to see them again."},
	{ID: "game-error", Question: QuestionLog, HealthOnly: true, Label: "Game error", Description: "An error the game wrote to its own log.", Message: "The game logged: {message}",
		Advice: "Open the file the error names and fix what it says."},
}

// A Topic is one topic of the checks, in words.
type Topic struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// Topics holds the topics of the checks, in the order they are listed.
var Topics = []Topic{
	{ID: "syntax", Label: "Syntax"}, {ID: "references", Label: "References"},
	{ID: "values", Label: "Scopes and values"}, {ID: "localization", Label: "Localization"},
	{ID: "definitions", Label: "Definitions"}, {ID: "descriptor", Label: "Descriptor"},
}

// A Part is one piece of a sentence: plain text, or a fact drawn in its style, such as a mod's name, a code, a count
// or a path.
type Part struct {
	Text  string `json:"text"`
	Style string `json:"style,omitempty"` // "", mod, code, count or path
}

// factStyles holds the style of each fact a message draws apart, by the fact's key. Any other fact is plain text.
var factStyles = map[string]string{
	"mod": "mod", "other": "mod", "winner": "mod", "beaten": "mod",
	"name": "code", "key": "code", "callee": "code", "namespace": "code",
	"file": "path", "folder": "path", "beatenFile": "path",
	"count": "count", "have": "count", "of": "count", "uses": "count", "removed": "count", "total": "count",
}

// languageFacts holds the facts of a message that hold a language's folder, shown as the language's name.
var languageFacts = []string{"language", "header"}

// factPlaceholder matches a fact in a message template, such as {name}.
var factPlaceholder = regexp.MustCompile(`\{\w+\}`)

// A Diagnostic is one problem found in a file.
type Diagnostic struct {
	Code    string            `json:"code"`
	Level   string            `json:"level"`   // its code's
	Message string            `json:"message"` // its code's template filled with its facts, as text
	Range   Range             `json:"range"`
	Facts   map[string]string `json:"facts"`
	Defined []Site            `json:"defined,omitempty"` // where the workspace defines a name the file's mod does not, in a mod it does not depend on
}

// valueDescriptions holds the words that describe each value type.
var valueDescriptions = map[schema.ValueType]string{
	schema.ValueTypeBool: "yes or no", schema.ValueTypeNumber: "a number", schema.ValueTypeDate: "a date",
	schema.ValueTypeColor: "a color", schema.ValueTypeCall: "a data call", schema.ValueTypeText: "text", schema.ValueTypeBlock: "a block",
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

// DiagnoseText returns the problems a file would have with the given text, judged against the open workspace, without
// touching the editor's text. It leaves out the checks the user turned off, and returns an error if no workspace is
// open.
//
//wails:ignore
func (lsp *Service) DiagnoseText(path, text string) ([]Diagnostic, error) {
	ws, err := lsp.Workspace.Session()
	if err != nil {
		return nil, err
	}
	mods := ws.Mods.Load()
	place := ws.Locate(mods, path)
	if !isDiagnosed(place) {
		return []Diagnostic{}, nil
	}
	document := ws.WalkText(place, text)
	return lsp.listEnabled(ws.ID, describeDiagnostics(newVisitor(ws, mods, place, &sync.Map{}).diagnoseTree(document))), nil
}

// DescribeReference returns the facts that say which name a problem is about.
func DescribeReference(reference schema.Reference) map[string]string {
	return map[string]string{"name": reference.Name, "database": game.DescribeDefinition(reference.Database, false)}
}

// DiagnoseFile returns the problems of a file in a workspace, judged on its document: the editor's text where the file
// is open, else the text on disk. It returns an error if the file cannot be read.
func DiagnoseFile(ws *workspace.Session, mods *index.Index, path string) ([]Diagnostic, error) {
	return diagnoseFile(ws, mods, path, &sync.Map{})
}

// DiagnoseFiles returns the problems of each of the given files in a workspace, as DiagnoseFile judges them, in the
// order the paths are given. It judges the files in parallel. A file that cannot be read has no problems.
func DiagnoseFiles(ws *workspace.Session, mods *index.Index, paths []string) [][]Diagnostic {
	// one map of the callees' parameters serves every file, so each callee's copy is read once in this run
	parameters := &sync.Map{}
	diagnostics := make([][]Diagnostic, len(paths))
	var group errgroup.Group
	group.SetLimit(files.Workers)
	for position, path := range paths {
		group.Go(func() error {
			diagnostics[position], _ = diagnoseFile(ws, mods, path, parameters)
			return nil
		})
	}
	_ = group.Wait()
	return diagnostics
}

// diagnoseFile returns the problems of a file in a workspace, as DiagnoseFile does. The given map holds the
// parameters of the callees read so far, by the site of the copy the game keeps, and gains the ones this file reads.
func diagnoseFile(ws *workspace.Session, mods *index.Index, path string, parameters *sync.Map) ([]Diagnostic, error) {
	place := ws.Locate(mods, path)
	if !isDiagnosed(place) {
		return []Diagnostic{}, nil
	}
	document, err := ws.WalkDocument(path)
	if err != nil {
		return nil, err
	}
	return describeDiagnostics(newVisitor(ws, mods, place, parameters).diagnoseTree(document)), nil
}

// isDiagnosed reports whether a file can have problems: it is script, localization or a mod's descriptor, or a mod's
// .yml file, which may sit in a misspelled localization folder. A file of the game's own GUI editor has none.
func isDiagnosed(place workspace.Place) bool {
	rel := strings.ToLower(place.Rel)
	if lo.SomeBy(game.GUI.EditorFolders, func(folder string) bool { return strings.HasPrefix(rel, folder+"/") }) {
		return false
	}
	return place.Format == files.FormatLocalization || place.Format.IsScript() || place.Format == files.FormatDescriptor ||
		place.Format == files.FormatText && place.Origin != files.OriginVanilla && strings.HasSuffix(rel, ".yml")
}

// listEnabled drops the diagnostics of the checks the user turned off in a workspace.
func (lsp *Service) listEnabled(workspaceID string, diagnostics []Diagnostic) []Diagnostic {
	setting, _ := lsp.Workspace.Config.LookupSetting(workspaceID, "ide.checks")
	disabled := lo.Compact(lo.Map(strings.Split(setting, ","), func(id string, _ int) string { return strings.TrimSpace(id) }))
	return slices.DeleteFunc(diagnostics, func(diagnostic Diagnostic) bool {
		return slices.Contains(disabled, diagnostic.Code)
	})
}

// toDiagnostics returns the problems a parse found in a tree's text as
// diagnostics. Each problem's words are its fact why.
func toDiagnostics(tree *jomini.Tree, problems []jomini.Problem) []Diagnostic {
	return lo.Map(problems, func(problem jomini.Problem, _ int) Diagnostic {
		return makeDiagnostic(tree, problem.Span, problem.Code, map[string]string{"why": problem.Message})
	})
}

// makeDiagnostic returns a diagnostic over a span of a tree's text.
func makeDiagnostic(tree *jomini.Tree, span jomini.Span, code string, facts map[string]string) Diagnostic {
	return Diagnostic{Code: code, Range: ToRange(tree.Src, tree.Lines, span), Facts: facts}
}

// describeDiagnostics fills in each diagnostic's level and message from its code and its facts.
func describeDiagnostics(diagnostics []Diagnostic) []Diagnostic {
	for position := range diagnostics {
		diagnostic := &diagnostics[position]
		code := LookupCode(diagnostic.Code)
		diagnostic.Level = code.Level
		diagnostic.Message = JoinParts(SplitMessage(code.Template(diagnostic.Facts), diagnostic.Facts))
	}
	return diagnostics
}

// LookupCode returns the problem code of the given id. It returns the zero code if it is not listed.
func LookupCode(id string) Code {
	code, _ := lo.Find(Codes, func(code Code) bool { return code.ID == id })
	return code
}

// Template returns the template of a code's message for the given facts: its elsewhere message where it has one and
// the facts name another mod that defines the name, else its message.
func (code Code) Template(facts map[string]string) string {
	return lo.Ternary(code.Elsewhere != "" && facts["other"] != "", code.Elsewhere, code.Message)
}

// SplitMessage fills a template with the given facts, as parts, each fact in its style. A placeholder with no fact
// stays as written.
func SplitMessage(template string, facts map[string]string) []Part {
	var pieces []string
	last := 0
	for _, match := range factPlaceholder.FindAllStringIndex(template, -1) {
		pieces = append(pieces, template[last:match[0]], template[match[0]:match[1]])
		last = match[1]
	}
	var parts []Part
	for _, piece := range lo.Compact(append(pieces, template[last:])) {
		key := strings.Trim(piece, "{}")
		fact, ok := facts[key]
		switch {
		case !strings.HasPrefix(piece, "{") || !ok:
			parts = append(parts, Part{Text: piece})
		case slices.Contains(languageFacts, key):
			parts = append(parts, Part{Text: loc.DescribeLanguage(fact)})
		default:
			style := factStyles[key]
			if key == "name" && facts["database"] == game.File {
				style = "path"
			}
			if number, err := strconv.Atoi(fact); err == nil && style == "count" {
				fact = DescribeCount(number)
			}
			parts = append(parts, Part{Text: fact, Style: style})
		}
	}
	return parts
}

// DescribeCount returns a count as the page shows it, its thousands separated by commas, such as 1,350.
func DescribeCount(count int) string {
	text := strconv.Itoa(count)
	for start := len(text) - 3; start > 0 && text[start-1] != '-'; start -= 3 {
		text = text[:start] + "," + text[start:]
	}
	return text
}

// JoinParts returns parts as one plain sentence.
func JoinParts(parts []Part) string {
	return strings.Join(lo.Map(parts, func(part Part, _ int) string { return part.Text }), "")
}

// Plural returns a count with its noun, such as 1 file or 1,350 files: the singular noun where the count is one, else
// the plural.
func Plural(count int, noun, nouns string) string {
	return DescribeCount(count) + " " + lo.Ternary(count == 1, noun, nouns)
}

// A diagnoseVisitor collects the problems of one file.
type diagnoseVisitor struct {
	ws          *workspace.Session
	index       *index.Index // the mods' index, narrowed to the file's mod and the mods it depends on
	whole       *index.Index // the mods' index, every mod of the workspace
	place       workspace.Place
	parseOnly   bool                    // the file is the game's, so only its parse errors are judged
	parameters  *sync.Map               // the parameters of each callee read so far, by the site of the copy the game keeps; shared by every file of one run
	enclosing   map[int32]*schema.Field // the parameters of each definition of the file read so far, by the definition's statement
	diagnostics []Diagnostic
}

// newVisitor returns the visitor that judges a file of a workspace. A mod's file is judged as the mod on its own, with
// the index narrowed to its mod and the mods it depends on. A file of the game is judged for its parse errors only.
// The given map holds the parameters of the callees the run has read.
func newVisitor(ws *workspace.Session, mods *index.Index, place workspace.Place, parameters *sync.Map) *diagnoseVisitor {
	visitor := &diagnoseVisitor{
		ws: ws, index: mods, whole: mods, place: place, parseOnly: place.Origin == files.OriginVanilla,
		parameters: parameters, enclosing: map[int32]*schema.Field{},
	}
	if place.Origin != "" && place.Origin != files.OriginVanilla {
		visitor.index = mods.Narrow(append([]string{place.Origin}, ws.ListDependencies(place.Origin)...))
	}
	return visitor
}

// diagnoseTree returns the problems of the visitor's file judged on the given document of its text.
func (visitor *diagnoseVisitor) diagnoseTree(document workspace.Document) []Diagnostic {
	tree, place := document.Tree, visitor.place
	switch {
	case place.Format == files.FormatDescriptor:
		return visitor.diagnoseDescriptor(tree)
	case place.Format == files.FormatLocalization:
		return visitor.diagnoseMessages(document)
	case !place.Format.IsScript():
		// the game reads nothing under a misspelled folder, so only the folder is judged
		return diagnoseFolders(tree, place)
	}
	diagnostics := toDiagnostics(tree, tree.Problems)
	if visitor.parseOnly {
		return diagnostics
	}
	// the file's own context is judged only where the file is one definition
	for position := range document.Contexts {
		statement, context := int32(position), &document.Contexts[position]
		if statement > 0 || context.Definition != nil {
			visitor.diagnoseStatement(tree, statement, context)
		}
	}
	return append(diagnostics, visitor.diagnostics...)
}

// diagnoseDescriptor returns a problem, at the text that writes it, if a mod's descriptor names a supported version the
// game's version does not match. It returns none if the descriptor writes no version, writes one no source shows the
// game matching, or cannot be read.
func (visitor *diagnoseVisitor) diagnoseDescriptor(tree *jomini.Tree) []Diagnostic {
	descriptor, err := workspace.ParseDescriptor(visitor.place.Path, tree.Src)
	version := visitor.ws.Schema.Version
	if matches, judged := workspace.MatchVersion(descriptor.SupportedVersion, version); err != nil || matches || !judged {
		return []Diagnostic{}
	}
	// the version is the first copy of its text after the word supported, which both descriptor forms write it under
	span := jomini.Span{}
	if key := strings.Index(tree.Src, "supported"); key >= 0 {
		if value := strings.Index(tree.Src[key:], descriptor.SupportedVersion); value >= 0 {
			span = jomini.Span{Start: key + value, End: key + value + len(descriptor.SupportedVersion)}
		}
	}
	facts := DescribeReference(schema.Reference{Name: cmp.Or(descriptor.Name, "This mod")})
	facts["mod"], facts["supported"], facts["version"] = facts["name"], descriptor.SupportedVersion, version
	return []Diagnostic{makeDiagnostic(tree, span, "supported-version", facts)}
}

// diagnoseStatement records the problems of a statement, and for a
// definition each key it must write and does not.
func (visitor *diagnoseVisitor) diagnoseStatement(tree *jomini.Tree, statement int32, context *schema.Context) {
	visitor.diagnoseRequired(tree, statement, context)
	visitor.diagnoseReferences(tree, context)
	visitor.diagnoseKey(tree, statement, context)
	visitor.diagnoseScope(tree, statement, context)
	visitor.diagnoseValue(tree, statement, context)
	visitor.diagnoseDate(tree, statement, context)
	visitor.diagnoseCall(tree, statement, context)
	// a GUI file's text is read as localization is, in the data context of its block
	if value := tree.Statements[statement].Value; visitor.place.Format == files.FormatGUI && strings.Contains(tree.Text(value), "[") {
		visitor.diagnoseCalls(tree, loc.Segments(tree.Unquoted(value), tree.UnquotedSpan(value).Start), context.Datacontext)
	}
}

// diagnoseRequired records each key a definition must write and does not, in
// alphabetical order.
func (visitor *diagnoseVisitor) diagnoseRequired(tree *jomini.Tree, statement int32, context *schema.Context) {
	definition := context.Definition
	if !context.Defines || definition.Mode != "" {
		return
	}
	database := visitor.ws.Schema.Databases[definition.Database]
	if database == nil || context.Field != database.Field {
		return
	}
	var missing []string
	for key, field := range context.Field.Children {
		if field.Required.Value == schema.RequirementAlways && field.Required.Source.CanAccuse() && !strings.HasPrefix(key, "<") &&
			!hasChild(tree, statement, key) && isRequired(context.Field, field) {
			missing = append(missing, key)
		}
	}
	reference := schema.Reference{Database: definition.Database, Name: definition.Name}
	if len(missing) > 0 {
		winner, _ := findKept(visitor.ws, visitor.index, reference, visitor.place.Position)
		missing = slices.DeleteFunc(missing, func(key string) bool { return visitor.isWritten(winner, definition, key) })
	}
	slices.Sort(missing)
	for _, key := range missing {
		facts := DescribeReference(reference)
		facts["key"] = key
		visitor.diagnostics = append(visitor.diagnostics, makeDiagnostic(tree, definition.Span, "missing-key", facts))
	}
}

// diagnoseReferences records each name the statement refers to that nothing defines. A definition's title key is
// missing-message's.
func (visitor *diagnoseVisitor) diagnoseReferences(tree *jomini.Tree, context *schema.Context) {
	if context.Label == game.TitleKey && context.Parent.Defines {
		return
	}
	visitor.diagnoseMissing(tree, context.References)
}

// diagnoseMissing records each of the given references whose name nothing defines. References at one span are
// alternatives, recorded only if none is defined. Where a mod the file's mod does not depend on defines the name, the
// problem says where.
func (visitor *diagnoseVisitor) diagnoseMissing(tree *jomini.Tree, references []schema.Reference) {
	for position, reference := range references {
		atSpan := func(other schema.Reference) bool { return other.Span == reference.Span }
		if slices.ContainsFunc(references[:position], atSpan) {
			continue
		}
		alternatives := lo.Filter(references, func(other schema.Reference, _ int) bool { return atSpan(other) })
		isInferred := func(other schema.Reference) bool { return other.Inferred }
		if lo.SomeBy(alternatives, isInferred) || lo.SomeBy(alternatives, visitor.isKnown) {
			continue
		}
		code := classifyMissing(alternatives)
		databases := lo.Uniq(lo.Map(alternatives, func(other schema.Reference, _ int) string {
			return game.DescribeDefinition(other.Database, false)
		}))
		facts := map[string]string{"name": reference.Name, "database": JoinList(databases, "or")}
		if code == "loc-markup" {
			facts["why"] = "no file declares the " + facts["database"] + " " + reference.Name
		}
		diagnostic := makeDiagnostic(tree, reference.Span, code, facts)
		if site, ok := visitor.findElsewhere(alternatives); ok {
			file := visitor.whole.File(site)
			facts["mod"], facts["other"], facts["file"] = visitor.ws.DescribeMod(visitor.place.Origin),
				visitor.ws.DescribeMod(file.Origin), file.Rel
			diagnostic.Defined = []Site{LocateSite(visitor.ws, visitor.whole, site)}
		}
		visitor.diagnostics = append(visitor.diagnostics, diagnostic)
	}
}

// classifyMissing returns the code for a name nothing defines, from the databases of its alternative references: an
// unknown localization key or unknown text markup if every alternative is one, else an unresolved reference.
func classifyMissing(alternatives []schema.Reference) string {
	codes := lo.Uniq(lo.Map(alternatives, func(reference schema.Reference, _ int) string {
		switch reference.Database {
		case game.Localization:
			return "unknown-loc-key"
		case game.GUI.TextIcons, game.GUI.TextFormats:
			return "loc-markup"
		}
		return ""
	}))
	if len(codes) == 1 && codes[0] != "" {
		return codes[0]
	}
	return unresolvedCode
}

// JoinList returns the given texts joined as in a sentence, the last two by the given conjunction: "A", "A and B"
// or "A, B and C". It returns "" for none.
func JoinList(texts []string, conjunction string) string {
	if len(texts) < 2 {
		return strings.Join(texts, "")
	}
	return strings.Join(texts[:len(texts)-1], ", ") + " " + conjunction + " " + texts[len(texts)-1]
}

// isKnown reports whether the game knows a reference's name. Any name the game's own files use is known, and so is a
// customizable localization the dumps name.
func (visitor *diagnoseVisitor) isKnown(reference schema.Reference) bool {
	if reference.Database == game.CustomLocalization && visitor.ws.Schema.Customs[reference.Name] {
		return true
	}
	mods := visitor.index
	if reference.Database == game.File {
		if _, ok := mods.ResolveTexture(reference.Name); ok {
			return true
		}
	} else if sites := mods.ResolveLocal(reference, visitor.place.Position); len(sites) > 0 {
		return slices.ContainsFunc(sites, func(site index.Site) bool {
			file := mods.File(site)
			hiding, _, ok := mods.FindLoaded(file.Rel)
			return file.Key == visitor.place.Key || mods.IsLoaded(site.File) || ok && hiding.Origin != visitor.place.Origin
		})
	}
	return mods.IsUsedByGame(reference.Database, reference.Name)
}

// findElsewhere returns the definition the whole workspace supplies for a name the file's mod does not define. It
// returns false if the file is judged with every mod, or if no alternative has a kept copy in a file the game reads.
func (visitor *diagnoseVisitor) findElsewhere(alternatives []schema.Reference) (index.Site, bool) {
	if visitor.whole == visitor.index {
		return index.Site{}, false
	}
	for _, reference := range alternatives {
		winner, ok := findKept(visitor.ws, visitor.whole, reference, visitor.place.Position)
		if ok && visitor.whole.IsLoaded(winner.Site.File) {
			return winner.Site, true
		}
	}
	return index.Site{}, false
}

// diagnoseKey records what is wrong with the key of a statement.
func (visitor *diagnoseVisitor) diagnoseKey(tree *jomini.Tree, statement int32, context *schema.Context) {
	current := tree.Statements[statement]
	switch {
	case context.Defines:
		visitor.diagnoseDefinition(tree, statement, context.Definition)
	case context.Category == schema.CategoryKeyword:
		visitor.diagnoseFollows(tree, statement, context)
	case current.Key.IsEmpty() || context.Category != schema.CategoryNone || context.Field != nil:
		// a key with a meaning of its own is not an unknown key
	case current.Parent == 0 && current.KeyClass == jomini.ClassEventID:
		visitor.diagnoseNamespace(tree, current, context)
	case context.Definition != nil:
		visitor.diagnoseUnknown(tree, statement, context)
	}
}

// diagnoseDefinition records the problems of a definition's entry mode, of a name it shares, and of a title key with
// no message in the base language.
func (visitor *diagnoseVisitor) diagnoseDefinition(tree *jomini.Tree, statement int32, definition *schema.Copy) {
	database := visitor.ws.Schema.Databases[definition.Database]
	if database == nil || definition.Alias {
		return
	}
	facts := DescribeReference(schema.Reference{Database: definition.Database, Name: definition.Name})
	modes, top := database.Precedence.Modes, tree.Statements[statement].Parent == 0
	fits := modes == game.ModePlaceEntry && top || modes == game.ModePlaceWrapped && !top
	switch {
	case definition.Mode != "" && modes != "" && !fits:
		facts["mode"] = definition.Mode
		mode := jomini.Span{Start: definition.Span.Start - len(definition.Mode) - 1, End: definition.Span.Start}
		visitor.diagnostics = append(visitor.diagnostics, makeDiagnostic(tree, mode, "misplaced-mode", facts))
	case definition.Mode != "" && visitor.isOrphan(definition):
		facts["mode"] = definition.Mode
		visitor.diagnostics = append(visitor.diagnostics, makeDiagnostic(tree, definition.Span, "inject-without-target", facts))
	case definition.Mode == "" && !definition.Local && visitor.isPriorityTied(definition, database.Precedence):
		facts["key"] = database.Precedence.PriorityKey
		visitor.diagnostics = append(visitor.diagnostics, makeDiagnostic(tree, definition.Span, "conflicting-priority", facts))
	}
	title, ok := tree.FindChild(statement, game.TitleKey)
	key := tree.Unquoted(tree.Statements[title].Value)
	if ok && tree.Statements[title].ValueClass.IsIdentifier() && !visitor.hasMessage(key) {
		missing := DescribeReference(schema.Reference{Database: definition.Database, Name: definition.Name})
		missing["key"] = key
		visitor.diagnostics = append(visitor.diagnostics, makeDiagnostic(tree, definition.Span, "missing-message", missing))
	}
}

// hasMessage reports whether the judged index has a message of a localization key in the base language.
func (visitor *diagnoseVisitor) hasMessage(key string) bool {
	mods := visitor.index
	return lo.ContainsBy(mods.ListMessages(key), func(site index.Site) bool { return mods.Language(site) == game.BaseLanguage })
}

// isOrphan reports whether a definition written with an entry mode has nothing to change. An optional mode, or one
// that creates, never is.
func (visitor *diagnoseVisitor) isOrphan(definition *schema.Copy) bool {
	mode, _ := game.LookupEntryMode(definition.Mode)
	if mode.Optional || mode.Creates {
		return false
	}
	reference := schema.Reference{Database: definition.Database, Name: definition.Name}
	_, acts := findKept(visitor.ws, visitor.index, reference, visitor.place.Position)
	return !acts
}

// isOwn reports whether a site is a definition as written in the file being walked.
func (visitor *diagnoseVisitor) isOwn(site index.Site, definition *schema.Copy) bool {
	return site.File == visitor.place.Position && visitor.index.Occurrence(site).Span == definition.Span
}

// isPriorityTied reports whether a definition has the same override priority as another loaded plain copy of its
// name, in a database where the game keeps the copy with the highest priority. It is false in any other database.
func (visitor *diagnoseVisitor) isPriorityTied(definition *schema.Copy, precedence game.Precedence) bool {
	if precedence.Keeps != game.KeepHighest {
		return false
	}
	mods := visitor.index
	reference := schema.Reference{Database: definition.Database, Name: definition.Name}
	sites := lo.Filter(mods.ResolveLocal(reference, visitor.place.Position), func(site index.Site, _ int) bool {
		occurrence := mods.Occurrence(site)
		return occurrence.Mode == 0 && !occurrence.Alias && mods.IsLoaded(site.File)
	})
	own := slices.IndexFunc(sites, func(site index.Site) bool { return visitor.isOwn(site, definition) })
	if len(sites) < 2 || own < 0 {
		return false
	}
	priority := mods.Occurrence(sites[own]).Priority
	return lo.CountBy(sites, func(site index.Site) bool { return mods.Occurrence(site).Priority == priority }) > 1
}

// diagnoseNamespace records an event at the top of an events file that is outside the file's namespaces.
func (visitor *diagnoseVisitor) diagnoseNamespace(tree *jomini.Tree, current jomini.Statement, context *schema.Context) {
	database, ok := visitor.ws.Schema.FindDatabase(visitor.place.File)
	if !ok || database.Row.Contents != game.ContentsEvents {
		return
	}
	namespaces := lo.Map(context.Parent.Namespaces, func(statement int32, _ int) string {
		return tree.Unquoted(tree.Statements[statement].Value)
	})
	facts := DescribeReference(schema.Reference{Database: database.Name, Name: tree.Unquoted(current.Key)})
	facts["namespace"] = cmp.Or(strings.Join(namespaces, ", "), "none")
	visitor.diagnostics = append(visitor.diagnostics, makeDiagnostic(tree, current.Key, "wrong-namespace", facts))
}

// diagnoseFollows records a keyword such as else that does not come right after one of the keys it must follow.
func (visitor *diagnoseVisitor) diagnoseFollows(tree *jomini.Tree, statement int32, context *schema.Context) {
	current := tree.Statements[statement]
	keyword := context.Keyword
	if len(keyword.Follows) == 0 {
		return
	}
	previous, ok := tree.FindPrevious(statement)
	if ok && slices.Contains(keyword.Follows, strings.ToLower(tree.Unquoted(tree.Statements[previous].Key))) {
		return
	}
	facts := DescribeReference(schema.Reference{Name: tree.Unquoted(current.Key)})
	facts["follows"] = JoinList(keyword.Follows, "or")
	visitor.diagnostics = append(visitor.diagnostics, makeDiagnostic(tree, current.Key, "else-without-if", facts))
}

// diagnoseUnknown records a key its block does not take. A key that is not a plain name is not judged.
func (visitor *diagnoseVisitor) diagnoseUnknown(tree *jomini.Tree, statement int32, context *schema.Context) {
	key := tree.Statements[statement].Key
	if visitor.ws.Schema.Databases[context.Database] == nil || !tree.Statements[statement].KeyClass.IsIdentifier() {
		return
	}
	holder := context.Parent
	if holder.Field == nil || holder.Shape == game.ShapeUnknown || !holder.Field.Source.CanAccuse() || holder.Field.Vanilla.Count > 0 {
		return
	}
	facts := DescribeReference(schema.Reference{Database: context.Database, Name: tree.Unquoted(key)})
	visitor.diagnostics = append(visitor.diagnostics, makeDiagnostic(tree, key, "unknown-key", facts))
}

// diagnoseScope records a command written in a scope the dumps do not list
// for it, and each link of a chain written in one: the scope the chain is
// written in for its first link, and the scope the link before it leads to
// for every other. A scope the definition starts in is stated, not followed,
// so only a scope that script moved to counts.
func (visitor *diagnoseVisitor) diagnoseScope(tree *jomini.Tree, statement int32, context *schema.Context) {
	if context.Category != schema.CategoryLink && context.Category != schema.CategoryCommand || !isScripted(context) {
		return
	}

	span := tree.UnquotedSpan(tree.Statements[statement].Key)
	scope, followed := context.Parent.Scope, len(context.Parent.Previous) > 0
	if context.Category == schema.CategoryCommand {
		visitor.checkScope(tree, span, context.Command, scope, followed)

		return
	}

	key := tree.Text(span)
	links := jomini.Links(key, span.Start)
	for position, link := range visitor.ws.Schema.ResolveChain(key, context.Parent, context.Saved) {
		visitor.checkScope(tree, links[position].Span, link.Definition, scope, followed)
		scope, followed = link.Scope, true
	}
}

// checkScope records a command or a link, written at the given span, that
// the dumps do not list for the scope it is written in. It records none for
// a name the dumps do not declare or list no scope for, for a scope that is
// not known, and for one that script did not move to.
func (visitor *diagnoseVisitor) checkScope(tree *jomini.Tree, span jomini.Span, command *schema.Definition, scope string, followed bool) {
	if command == nil || len(command.Scopes) == 0 || scope == "" || !followed || slices.Contains(command.Scopes, scope) {
		return
	}

	visitor.diagnostics = append(visitor.diagnostics, makeDiagnostic(tree, span, "wrong-scope", map[string]string{
		"name": tree.Text(span), "scope": scope, "allowed": strings.Join(command.Scopes, ", "),
	}))
}

// isScripted reports whether the block that holds a statement holds effects or triggers. A command's own block does
// only if the command leads to a scope or walks a list, else it holds arguments.
func isScripted(context *schema.Context) bool {
	holder := context.Parent
	return (holder.Shape == game.ShapeEffects || holder.Shape == game.ShapeTriggers) && !holder.HoldsArguments()
}

// diagnoseValue records a value that is not the type its key takes. References, and names where a number is expected,
// are not judged.
func (visitor *diagnoseVisitor) diagnoseValue(tree *jomini.Tree, statement int32, context *schema.Context) {
	field, current := context.Field, tree.Statements[statement]
	categories := []schema.Category{schema.CategoryField, schema.CategoryReference, schema.CategoryCommand}
	if !slices.Contains(categories, context.Category) || field == nil || !field.Value.IsStated() ||
		!field.Value.Source.CanAccuse() || current.IsBlock() || current.Value.IsEmpty() ||
		context.ValueGroup == schema.CategoryReference {
		return
	}
	// MatchValue cannot judge a path, free text or a block's short form
	expected, text := field.Value.Value, tree.Text(current.Value)
	if expected.Database == game.File || expected.Type == schema.ValueTypeText || expected.Type == schema.ValueTypeBlock {
		return
	}
	// the game reads a script value, or a trigger that returns a number, where a number is expected
	readsScriptValue := expected.Type == schema.ValueTypeNumber && current.ValueClass.IsIdentifier()
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
	case expected.Type == schema.ValueTypeChoice:
		return "one of " + strings.Join(expected.Choices, ", ")
	case expected.Type == schema.ValueTypeReference && expected.Database != "":
		return "the name of a " + game.DescribeDefinition(expected.Database, false)
	case expected.Type == schema.ValueTypeReference:
		return "a name"
	case expected.Type == schema.ValueTypeTarget:
		description := "a link"
		if expected.Scope != "" {
			description += " to " + describeScope(expected.Scope)
		}
		if expected.Database != "" {
			description += ", or the name of a " + game.DescribeDefinition(expected.Database, false)
		}
		return description
	}
	return valueDescriptions[expected.Type]
}

// diagnoseDate records a date the game cannot have: a key written as year.month.day, or the value of a field a
// declared source says takes a date. A key of two numbers is a number, not a date.
func (visitor *diagnoseVisitor) diagnoseDate(tree *jomini.Tree, statement int32, context *schema.Context) {
	current := tree.Statements[statement]
	spans := []jomini.Span{}
	if current.KeyClass != jomini.ClassNumber {
		spans = append(spans, current.Key)
	}
	if field := context.Field; field != nil && field.Value.Value.Type == schema.ValueTypeDate && field.Value.Source.CanAccuse() &&
		!current.IsBlock() {
		spans = append(spans, current.Value)
	}
	for _, span := range spans {
		if date, ok := jomini.ParseDate(tree.Unquoted(span)); ok && !date.IsValid() {
			visitor.diagnostics = append(visitor.diagnostics, makeDiagnostic(tree, span, "invalid-date",
				DescribeReference(schema.Reference{Name: tree.Unquoted(span)})))
		}
	}
}

// diagnoseCall records the arguments a call to a scripted definition gives that its body does not read, and the
// parameters with no default that the call does not give. A call inside another scripted definition counts that
// definition's parameters as given, since they reach it.
func (visitor *diagnoseVisitor) diagnoseCall(tree *jomini.Tree, statement int32, context *schema.Context) {
	current := tree.Statements[statement]
	parameters := visitor.listCalleeParameters(tree.Unquoted(current.Key), context.Callee)
	if parameters == nil {
		return
	}
	enclosing := visitor.listEnclosingParameters(tree, context.Definition)
	add := func(span jomini.Span, name, why string) {
		facts := DescribeReference(schema.Reference{Name: name})
		facts["why"], facts["callee"] = why, tree.Unquoted(current.Key)
		visitor.diagnostics = append(visitor.diagnostics, makeDiagnostic(tree, span, "unused-argument", facts))
	}
	given := map[string]bool{}
	if current.IsBlock() {
		for child := range tree.Children(statement) {
			key := tree.Statements[child].Key
			given[tree.Unquoted(key)] = true
			if _, read := parameters.Children[tree.Unquoted(key)]; !read && !key.IsEmpty() {
				add(key, tree.Unquoted(key), "is never read by")
			}
		}
	}
	for _, name := range slices.Sorted(maps.Keys(parameters.Children)) {
		if _, inherited := enclosing.Children[name]; !given[name] && !inherited && !parameters.Children[name].Default.IsStated() {
			add(current.Key, name, "is not given to")
		}
	}
}

// listCalleeParameters returns the parameters of the scripted definition a call names, read from the copy the game
// keeps, else those the walk knows. A copy read once in the run is not read again. It returns nil if the call names
// no scripted definition that takes parameters.
func (visitor *diagnoseVisitor) listCalleeParameters(key string, callee *schema.Definition) *schema.Field {
	if callee == nil || callee.Field == nil {
		return nil
	}
	reference := schema.Reference{Database: callee.Database, Name: key}
	winner, ok := findKept(visitor.ws, visitor.index, reference, visitor.place.Position)
	if !ok {
		return callee.Field
	}
	if parameters, ok := visitor.parameters.Load(winner.Site); ok {
		return parameters.(*schema.Field)
	}
	tree, statement, err := visitor.ws.FindStatement(visitor.index, winner.Site)
	if err != nil {
		return callee.Field
	}
	parameters := schema.ListParameters(tree, statement)
	visitor.parameters.Store(winner.Site, parameters)
	return parameters
}

// listEnclosingParameters returns the parameters of the definition a call is written in, read from the file being
// judged. A definition read once is not read again. It returns no parameters if the call is in no definition.
func (visitor *diagnoseVisitor) listEnclosingParameters(tree *jomini.Tree, definition *schema.Copy) *schema.Field {
	if definition == nil {
		return &schema.Field{}
	}
	parameters, ok := visitor.enclosing[definition.Statement]
	if !ok {
		parameters = schema.ListParameters(tree, definition.Statement)
		visitor.enclosing[definition.Statement] = parameters
	}
	return parameters
}

// isWritten reports whether a definition that a winner merges, other than the one walked, writes a key.
func (visitor *diagnoseVisitor) isWritten(winner index.Winner, definition *schema.Copy, key string) bool {
	return slices.ContainsFunc(winner.Merged, func(site index.Site) bool {
		if visitor.isOwn(site, definition) {
			return false
		}
		tree, statement, err := visitor.ws.FindStatement(visitor.index, site)
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
