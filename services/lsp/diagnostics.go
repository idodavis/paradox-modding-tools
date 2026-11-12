// diagnostics.go reports the problems of a file. The index decides them,
// and this file gives each its level and its words, and holds the table of
// codes.

package lsp

import (
	"cmp"
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

// The questions a check answers, and the levels a check's findings have, by
// how much the game loses.
const (
	QuestionCompatibility = "compatibility" // how the workspace's mods touch the same things
	QuestionHealth        = "health"        // each mod's own problems
	QuestionLog           = "log"           // what the game's last run logged
	LevelCritical         = "critical"      // the game loses a whole file, folder, definition or mod
	LevelError            = "error"         // the game skips one statement, line or value and keeps running
	LevelWarning          = "warning"       // it may be fine, or only shows a player the wrong text
	LevelInfo             = "info"          // only how things are
)

// A Level is one level of a finding: its wording, its icon and color on the
// page, whether it counts as a problem and whether the game loses something.
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

// keyFacts are the codes of a localization key with no text, each with the
// fact that names the key.
var keyFacts = map[string]string{index.ProblemTitle: "key", index.ProblemLocKey: "name"}

// elsewhereMessage is the message for a name that the file's mod does not
// define but a mod it does not depend on does.
const elsewhereMessage = "{mod} uses this {database}, but the game doesn't define it: {other} does. {mod} doesn't " +
	"list {other} as a dependency, so in a game without {other} the {database} doesn't exist, and every place that " +
	"uses it finds nothing."

// A Code is one kind of problem.
type Code struct {
	// ID is the code's name in kebab case, such as wrong-scope.
	ID string `json:"id"`

	// Question is compatibility, health or log. Level is critical, error,
	// warning or info, and Topic one such as syntax or localization; both
	// are "" for the game's log.
	Question string `json:"question"`
	Level    string `json:"level"`
	Topic    string `json:"topic"`

	// Label is the code's title, and Description one plain sentence on what
	// the check looks for.
	Label       string `json:"label"`
	Description string `json:"description"`

	// Message is a template the facts fill, such as {name}. Advice says what
	// to do about it in plain words, and repeats no fact of the message.
	Message string `json:"message"`
	Advice  string `json:"advice"`

	// Elsewhere is the message where a mod the file's mod does not depend on
	// defines the name. It is "" if the code has none.
	Elsewhere string `json:"elsewhere,omitempty"`

	// HealthOnly is set for a code the editor never gives and only Health's
	// report finds.
	HealthOnly bool `json:"healthOnly"`
}

// Codes holds every problem code.
var Codes = []Code{
	{ID: "unclosed-block", Question: QuestionHealth, Level: LevelWarning, Topic: "syntax",
		Label: "Unclosed block", Description: "A block that is opened and never closed.",
		Message: "This block is opened and never closed.",
		Advice:  "Add the closing brace where the block ends."},
	{ID: "too-deep", Question: QuestionHealth, Level: LevelWarning, Topic: "syntax",
		Label: "Block nested too deep", Description: "A block nested more than 1,000 levels deep.",
		Message: "This block is nested more than 1,000 levels deep, so nothing inside it is checked.",
		Advice:  "Close the blocks above it that are left open, or nest it less deeply."},
	{ID: "unexpected-brace", Question: QuestionHealth, Level: LevelCritical, Topic: "syntax",
		Label: "Unexpected brace", Description: "A closing brace with no block to close.",
		Message: "This closing brace has no block to close.",
		Advice:  "Delete this brace, or add the opening brace it belongs to."},
	{ID: "unclosed-string", Question: QuestionHealth, Level: LevelCritical, Topic: "syntax",
		Label: "Unclosed string", Description: "A string that is opened and never closed.",
		Message: "This string is opened and never closed.",
		Advice:  "Add the closing quote."},
	{ID: "missing-value", Question: QuestionHealth, Level: LevelError, Topic: "syntax",
		Label: "Missing value", Description: "A key with an operator and no value after it.",
		Message: "This key has an operator and no value.",
		Advice:  "Write the value after the operator, or delete the key."},
	{ID: index.ProblemFollows, Question: QuestionHealth, Level: LevelError, Topic: "syntax",
		Label: "An else with no if before it", Description: "An else that does not come right after an if.",
		Message: "{name} must come right after {follows}.",
		Advice:  "Move it right after the block it belongs to, or delete it."},
	{ID: "malformed-line", Question: QuestionHealth, Level: LevelError, Topic: "localization",
		Label: "Malformed line", Description: "A localization line the game cannot read as a key and its text.",
		Message: "The game cannot read this line as a localization key and its text.",
		Advice:  "Write the line as the key, a colon and the text in quotes."},
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
	{ID: index.ProblemBOM, Question: QuestionHealth, Level: LevelCritical, Topic: "localization",
		Label:       "Missing byte order mark",
		Description: "A localization file saved without a UTF-8 byte order mark.",
		Message:     "The game reads a localization file only with a UTF-8 byte order mark.",
		Advice:      "Save the file again as UTF-8 with a byte order mark."},
	{ID: "wrong-localization-folder", Question: QuestionHealth, Level: LevelCritical, Topic: "localization",
		Label: "Wrong localization folder", Description: "A localization file in a folder the game does not read.",
		Message: "The game reads the folder {right}, not {wrong}.",
		Advice:  "Move the file into the folder the game reads."},
	{ID: "loc-header-mismatch", Question: QuestionHealth, Level: LevelWarning, Topic: "localization",
		Label:       "Header of another language",
		Description: "A localization file whose name and header name different languages.",
		Message:     "The file is named for {language}, but its header says {header}.",
		Advice:      "Make the file name and the header use the same language."},
	{ID: index.ProblemDuplicate, Question: QuestionHealth, Level: LevelWarning, Topic: "localization",
		Label:       "Duplicate localization key",
		Description: "A localization key defined more than once in one language.",
		Message:     "The localization key {name} is defined more than once in {language}.",
		Advice:      "Delete the extra lines, or give each its own key."},
	{ID: index.ProblemTitle, Question: QuestionHealth, Level: LevelWarning, Topic: "localization",
		Label:       "Missing title localization",
		Description: "A definition whose title key has no text the game reads, in the workspace's language or in English.",
		Message:     "The {database} {name} has no text for its title, in the workspace's language or in English.",
		Advice:      "Add the key and its text to your mod's localization file."},
	{ID: index.ProblemLocKey, Question: QuestionHealth, Level: LevelWarning, Topic: "localization",
		Label: "Unknown localization key", Description: "A localization key that nothing defines.",
		Message: "Nothing defines the localization key {name}.",
		Advice: "Add the key and its text to your mod's localization file, or fix the spelling here. " +
			"If another mod defines it, list that mod as a dependency.",
		Elsewhere: elsewhereMessage},
	{ID: index.ProblemMarkup, Question: QuestionHealth, Level: LevelWarning, Topic: "localization",
		Label:       "Unknown text markup",
		Description: "Text markup in a localization text that the game does not read.",
		Message:     "This is not text markup the game reads: {why}.",
		Advice:      "Fix the spelling of the markup, or take it out."},
	{ID: "coverage", Question: QuestionHealth, Level: LevelInfo, Topic: "localization", HealthOnly: true,
		Label: "Language coverage", Description: "How many of a mod's localization keys each language has.",
		Message: "{mod} has {have} of its {of} localization keys in {name}.",
		Advice:  "Translate the keys that are missing, or leave them if the mod does not need that language."},
	{ID: index.ProblemUnresolved, Question: QuestionHealth, Level: LevelError, Topic: "references",
		Label: "Unresolved reference", Description: "A name that nothing defines.",
		Message: "Nothing defines the {database} {name}.",
		Advice: "Fix the spelling, or add the missing one to your mod. " +
			"If another mod defines it, list that mod as a dependency. " +
			"If the game used to define it, a patch may have removed it: see Patch Notes.",
		Elsewhere: elsewhereMessage},
	{ID: index.ProblemDataCall, Question: QuestionHealth, Level: LevelWarning, Topic: "references",
		Label: "Unknown data function", Description: "A data function that its type does not have.",
		Message: "{type} has no data function {name}.",
		Advice:  "Fix the spelling, or use a function the type has."},
	{ID: index.ProblemScope, Question: QuestionHealth, Level: LevelError, Topic: "values",
		Label: "Wrong scope", Description: "A key used in a scope where it does not work.",
		Message: "{name} runs in {allowed}, not in {scope}.",
		Advice:  "Move the line to a scope where it works, or switch to a scope that allows it first."},
	{ID: index.ProblemValue, Question: QuestionHealth, Level: LevelError, Topic: "values",
		Label: "Wrong value", Description: "A value of another kind than its key takes.",
		Message: "{name} takes {expected}.",
		Advice:  "Write a value of the kind the key takes."},
	{ID: index.ProblemDate, Question: QuestionHealth, Level: LevelError, Topic: "values",
		Label: "Impossible date", Description: "A date with a month or a day that does not exist.",
		Message: "{name} is no date: a month runs from 1 to 12, and a day from 1 to 31.",
		Advice:  "Write a real date as year.month.day."},
	{ID: index.ProblemRequired, Question: QuestionHealth, Level: LevelError, Topic: "values",
		Label: "Missing key", Description: "A definition that leaves out a key it must write.",
		Message: "The {database} {name} does not write {key}, which it must.",
		Advice:  "Add the key to the entry."},
	{ID: index.ProblemUnknownKey, Question: QuestionHealth, Level: LevelWarning, Topic: "values",
		Label: "Unknown key", Description: "A key that the block it is written in does not take.",
		Message: "{name} is not a key a {database} takes here.",
		Advice:  "Fix the spelling, or delete the key."},
	{ID: index.ProblemArgument, Question: QuestionHealth, Level: LevelWarning, Topic: "values",
		Label:       "Argument not read or not given",
		Description: "A call and the definition it calls that disagree about an argument.",
		Message:     "The argument {name} {why} {callee}.",
		Advice:      "Fix the argument's spelling, or add or delete it so the call and the definition match."},
	{ID: index.ProblemNamespace, Question: QuestionHealth, Level: LevelCritical, Topic: "definitions",
		Label:       "Wrong namespace",
		Description: "An event whose name is not in a namespace its file declares.",
		Message:     "The {database} {name} is not in a namespace this file declares ({namespace}).",
		Advice:      "Declare the namespace at the top of the file, or rename the event to start with one the file declares."},
	{ID: index.ProblemMode, Question: QuestionHealth, Level: LevelCritical, Topic: "definitions",
		Label:       "Misplaced entry mode",
		Description: "An entry mode, such as REPLACE, where the game does not take one.",
		Message:     "The {database} {name} cannot be written with {mode} here.",
		Advice:      "Delete the mode."},
	{ID: index.ProblemOrphan, Question: QuestionHealth, Level: LevelCritical, Topic: "definitions",
		Label:       "Entry mode with nothing to change",
		Description: "An entry mode written for a name that no earlier entry defines.",
		Message:     "The {database} {name} is written with {mode}, and no {database} {name} is loaded for it to change.",
		Advice:      "Fix the name, or load the mod that defines it before this one."},
	{ID: index.ProblemPriority, Question: QuestionHealth, Level: LevelCritical, Topic: "definitions",
		Label:       "Conflicting priority",
		Description: "Two copies of one name with the same override priority.",
		Message: "{name} is defined twice with the same override priority. The game logs an error and keeps one copy; " +
			"which one is not documented. Give one a higher {key}.",
		Advice: "Decide which copy should win and raise its number."},
	{ID: "unmounted-folder", Question: QuestionHealth, Level: LevelWarning, Topic: "definitions", HealthOnly: true,
		Label:       "Folder the game never reads",
		Description: "Files in a folder the game never reads.",
		Message: "This folder is outside the in_game, main_menu and loading_screen folders, " +
			"so the game never reads the files in it: {count}.",
		Advice: "Move the files into one of those folders."},
	{ID: "supported-version", Question: QuestionHealth, Level: LevelWarning, Topic: "descriptor",
		Label:       "Made for another game version",
		Description: "A mod made for another game version than the installed one.",
		Message:     "{mod} is made for game {supported}; the game is {version}.",
		Advice:      "Test the mod in this version, then set the version it supports to match."},
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
	{ID: "game-error", Question: QuestionLog, HealthOnly: true,
		Label: "Game error", Description: "An error the game wrote to its own log.",
		Message: "The game logged: {message}",
		Advice:  "Open the file the error names and fix what it says."},
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

// A Part is one piece of a sentence: plain text, or a fact drawn in its
// style, such as a mod's name, a code, a count or a path.
type Part struct {
	Text string `json:"text"`

	// Style is "", mod, code, count or path.
	Style string `json:"style,omitempty"`
}

// factStyles holds the style of each fact a message draws apart, by the
// fact's key. Any other fact is plain text.
var factStyles = map[string]string{
	"mod": "mod", "other": "mod", "winner": "mod", "beaten": "mod",
	"name": "code", "key": "code", "callee": "code", "namespace": "code",
	"file": "path", "folder": "path", "beatenFile": "path",
	"count": "count", "have": "count", "of": "count", "uses": "count", "removed": "count", "total": "count",
}

// languageFacts holds the facts of a message that hold a language's folder,
// shown as the language's name.
var languageFacts = []string{"language", "header"}

// factPlaceholder matches a fact in a message template, such as {name}.
var factPlaceholder = regexp.MustCompile(`\{\w+\}`)

// A Diagnostic is one problem found in a file.
type Diagnostic struct {
	Code string `json:"code"`

	// Level is its code's, and Message its code's template filled with its
	// facts, as text.
	Level   string            `json:"level"`
	Message string            `json:"message"`
	Range   jomini.Range      `json:"range"`
	Facts   map[string]string `json:"facts"`

	// Defined is where the workspace defines a name the file's mod does not,
	// in a mod it does not depend on.
	Defined []Site `json:"defined,omitempty"`
}

// Diagnose returns the problems of a file, judged against the open
// workspace. It leaves out the checks the user turned off.
func (lsp *Service) Diagnose(path string) ([]Diagnostic, error) {
	ws, err := lsp.Workspace.Session()
	if err != nil {
		return nil, err
	}
	diagnostics, err := DiagnoseFile(ws, ws.Mods.Load(), path)
	return lsp.listEnabled(ws.ID, diagnostics), err
}

// DiagnoseText returns the problems a file would have with the given text,
// judged against the open workspace, without touching the editor's text. It
// leaves out the checks the user turned off, and returns an error if no
// workspace is open.
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
	return lsp.listEnabled(ws.ID, diagnoseDocument(ws, mods, place, ws.WalkText(place, text))), nil
}

// DiagnoseFiles returns the problems of each of the given files in a
// workspace, as DiagnoseFile judges them, in the order the paths are given.
// It judges the files in parallel. A file that cannot be read has none.
func DiagnoseFiles(ws *workspace.Session, mods *index.Index, paths []string) [][]Diagnostic {
	diagnostics := make([][]Diagnostic, len(paths))
	var group errgroup.Group
	group.SetLimit(files.Workers)
	for position, path := range paths {
		group.Go(func() error {
			diagnostics[position], _ = DiagnoseFile(ws, mods, path)
			return nil
		})
	}
	_ = group.Wait()
	return diagnostics
}

// judged is the problems of one file as last judged.
type judged struct {
	diagnostics []Diagnostic

	// edits is how many times the editor's buffers had changed when the
	// file was judged on the editor's text. It is -1 for a file judged on
	// its text on disk.
	edits int64
}

// problems holds the problems of the files judged against one index of the
// mods, by file key. A scan puts a new index in place, and the problems
// judged against the old one go.
var problems struct {
	sync.Mutex
	mods  *index.Index
	files map[string]judged
}

// DiagnoseFile returns the problems of a file in a workspace, judged on its
// document: the editor's text where the file is open, else the text on disk.
// It returns an error if the file cannot be read.
//
// A file's problems are judged once and kept: until a scan puts a new index
// of the mods in place, and for a file open in the editor also until a
// buffer of the editor changes, since its text changes with no scan.
func DiagnoseFile(ws *workspace.Session, mods *index.Index, path string) ([]Diagnostic, error) {
	place := ws.Locate(mods, path)
	if !isDiagnosed(place) {
		return []Diagnostic{}, nil
	}

	edits := int64(-1)
	if _, open := ws.LookupBuffer(path); open {
		edits = ws.Edits.Load()
	}

	problems.Lock()
	last, kept := problems.files[place.Key]
	kept = kept && problems.mods == mods && last.edits == edits
	problems.Unlock()
	if kept {
		return slices.Clone(last.diagnostics), nil
	}

	document, err := ws.WalkDocument(path)
	if err != nil {
		return nil, err
	}

	diagnostics := diagnoseDocument(ws, mods, place, document)
	problems.Lock()
	if problems.mods != mods {
		problems.mods, problems.files = mods, map[string]judged{}
	}

	problems.files[place.Key] = judged{diagnostics: diagnostics, edits: edits}
	problems.Unlock()

	return slices.Clone(diagnostics), nil
}

// isDiagnosed reports whether a file can have problems: it is script,
// localization or a mod's descriptor, or a mod's .yml file, which may sit in
// a misspelled localization folder. A file of the game's own GUI editor has
// none.
func isDiagnosed(place workspace.Place) bool {
	rel := strings.ToLower(place.Rel)
	if lo.SomeBy(game.GUI.EditorFolders, func(folder string) bool { return strings.HasPrefix(rel, folder+"/") }) {
		return false
	}
	return place.Format == files.FormatLocalization || place.Format.IsScript() || place.Format == files.FormatDescriptor ||
		place.Format == files.FormatText && place.Origin != files.OriginVanilla && strings.HasSuffix(rel, ".yml")
}

// diagnoseDocument returns the problems of a file judged on the given
// document of its text, each with its level and its message. A mod's file is
// judged as the mod on its own: against the index narrowed to its mod and
// the mods it depends on. A file of the game gets its parse errors only, and
// a file that is neither script nor localization only its folder's.
func diagnoseDocument(ws *workspace.Session, mods *index.Index, place workspace.Place, document schema.Document) []Diagnostic {
	tree := document.Tree
	var diagnostics []Diagnostic
	switch {
	case place.Format == files.FormatDescriptor:
		return describeDiagnostics(diagnoseDescriptor(ws, place, tree))
	case place.Format == files.FormatLocalization:
		diagnostics = toDiagnostics(tree, document.Messages.Problems)
	case !place.Format.IsScript():
		// the game reads nothing under a misspelled folder
		return describeDiagnostics(diagnoseFolders(tree, place))
	default:
		diagnostics = toDiagnostics(tree, tree.Problems)
	}
	if place.Origin == files.OriginVanilla {
		return describeDiagnostics(diagnostics)
	}

	if document.Messages != nil {
		diagnostics = append(diagnostics, diagnoseHeader(tree, document.Messages, place)...)
		diagnostics = append(diagnostics, diagnoseFolders(tree, place)...)
		for _, message := range document.Messages.Messages {
			diagnostics = append(diagnostics, toDiagnostics(tree, loc.ListMarkupFaults(message.Segments))...)
		}
	}

	narrowed := mods
	if place.Origin != "" {
		narrowed = mods.Narrow(append([]string{place.Origin}, ws.ListDependencies(place.Origin)...))
	}

	for _, problem := range narrowed.ListProblems(ws.Schema, mods, document, ws.Languages(), ws.LoadTree) {
		// what a wrong value must be is put in words here
		if problem.Code == index.ProblemValue {
			problem.Facts["expected"] = describeExpected(problem.Expected)
		}

		diagnostic := makeDiagnostic(tree, problem.Span, problem.Code, problem.Facts)
		if site := problem.Elsewhere; site != nil {
			file := mods.File(*site)
			diagnostic.Facts["mod"], diagnostic.Facts["other"] = ws.DescribeMod(place.Origin), ws.DescribeMod(file.Origin)
			diagnostic.Facts["file"] = file.Rel
			diagnostic.Defined = []Site{LocateSite(ws, mods, *site)}
		}

		diagnostics = append(diagnostics, diagnostic)
	}

	return describeDiagnostics(diagnostics)
}

// listEnabled drops the diagnostics of the checks the user turned off in a
// workspace.
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
	return Diagnostic{Code: code, Range: jomini.ToRange(tree.Src, tree.Lines, span), Facts: facts}
}

// describeDiagnostics fills in each diagnostic's level and message from its
// code and its facts.
func describeDiagnostics(diagnostics []Diagnostic) []Diagnostic {
	for position := range diagnostics {
		diagnostic := &diagnostics[position]
		code := LookupCode(diagnostic.Code)
		diagnostic.Level = code.Level
		diagnostic.Message = JoinParts(SplitMessage(code.Template(diagnostic.Facts), diagnostic.Facts))
	}
	return diagnostics
}

// LookupCode returns the problem code of the given id. It returns the zero
// code if it is not listed.
func LookupCode(id string) Code {
	code, _ := lo.Find(Codes, func(code Code) bool { return code.ID == id })
	return code
}

// Template returns the template of a code's message for the given facts:
// its elsewhere message where it has one and the facts name another mod that
// defines the name, else its message.
func (code Code) Template(facts map[string]string) string {
	return lo.Ternary(code.Elsewhere != "" && facts["other"] != "", code.Elsewhere, code.Message)
}

// SplitMessage fills a template with the given facts, as parts, each fact in
// its style. A placeholder with no fact stays as written.
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

// DescribeCount returns a count as the page shows it, its thousands
// separated by commas, such as 1,350.
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

// Plural returns a count with its noun, such as 1 file or 1,350 files: the
// singular noun where the count is one, else the plural.
func Plural(count int, noun, nouns string) string {
	return DescribeCount(count) + " " + lo.Ternary(count == 1, noun, nouns)
}

// diagnoseDescriptor returns a problem, at the text that writes it, if a
// mod's descriptor names a supported version the game's version does not
// match. It returns none if the descriptor writes no version, writes one no
// source shows the game matching, or cannot be read.
func diagnoseDescriptor(ws *workspace.Session, place workspace.Place, tree *jomini.Tree) []Diagnostic {
	descriptor, err := workspace.ParseDescriptor(place.Path, tree.Src)
	version := ws.Schema.Version
	if matches, judged := workspace.MatchVersion(descriptor.SupportedVersion, version); err != nil || matches || !judged {
		return []Diagnostic{}
	}

	// the version is the first copy of its text after the word supported,
	// which both descriptor forms write it under
	span := jomini.Span{}
	if key := strings.Index(tree.Src, "supported"); key >= 0 {
		if value := strings.Index(tree.Src[key:], descriptor.SupportedVersion); value >= 0 {
			span = jomini.Span{Start: key + value, End: key + value + len(descriptor.SupportedVersion)}
		}
	}
	facts := index.DescribeReference(schema.Reference{Name: cmp.Or(descriptor.Name, "This mod")})
	facts["mod"], facts["supported"], facts["version"] = facts["name"], descriptor.SupportedVersion, version
	return []Diagnostic{makeDiagnostic(tree, span, "supported-version", facts)}
}

// diagnoseHeader returns a problem if a mod's localization file is named for
// one language and its header names another.
func diagnoseHeader(tree *jomini.Tree, file *loc.File, place workspace.Place) []Diagnostic {
	suffix := loc.FindSuffixLanguage(place.Path)
	if suffix == "" || file.Language == "" || strings.EqualFold(suffix, file.Language) {
		return nil
	}
	facts := map[string]string{"language": suffix, "header": file.Language}
	return []Diagnostic{makeDiagnostic(tree, jomini.Span{}, "loc-header-mismatch", facts)}
}

// diagnoseFolders returns a problem for each misspelled localization folder
// in a file's path.
func diagnoseFolders(tree *jomini.Tree, place workspace.Place) []Diagnostic {
	diagnostics := []Diagnostic{}
	folders := strings.Split(strings.ToLower(place.Rel), "/")
	for _, folder := range folders[:len(folders)-1] {
		if right, misspelled := loc.MisspelledFolders[folder]; misspelled {
			diagnostics = append(diagnostics, makeDiagnostic(tree, jomini.Span{}, "wrong-localization-folder",
				map[string]string{"wrong": folder, "right": right}))
		}
	}
	return diagnostics
}

// valueDescriptions holds the words that describe each value type.
var valueDescriptions = map[schema.ValueType]string{
	schema.ValueTypeBool: "yes or no", schema.ValueTypeNumber: "a number", schema.ValueTypeDate: "a date",
	schema.ValueTypeColor: "a color", schema.ValueTypeCall: "a data call", schema.ValueTypeText: "text",
	schema.ValueTypeBlock: "a block",
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
