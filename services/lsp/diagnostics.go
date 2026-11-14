// diagnostics.go judges a file and returns its problems. It holds the table of
// problem codes.

package lsp

import (
	"cmp"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/sync/errgroup"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/internal/loc"
	"paradox-modding-tools/services/workspace"
)

// Level is one level of a finding.
type Level struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Icon  string `json:"icon"`
	Color string `json:"color"`
	Noun  string `json:"noun"`
	Nouns string `json:"nouns"`

	// Problem is set for the levels that count as problems, and Fix for those
	// where the game loses something, so a mod needs a fix. Cost says what the
	// game loses.
	Problem bool   `json:"problem"`
	Fix     bool   `json:"fix"`
	Cost    string `json:"cost"`
}

// Code is one kind of problem.
type Code struct {
	// ID is the code's name in kebab case, such as wrong-scope.
	ID string `json:"id"`

	// Question is the Health question the code is asked under. Level and Topic
	// are "" for the game's log.
	Question string `json:"question"`
	Level    string `json:"level"`
	Topic    string `json:"topic"`

	Label       string `json:"label"`
	Description string `json:"description"`

	// Message is a template the facts fill, such as {name}.
	Message string `json:"message"`
	Advice  string `json:"advice"`

	// Elsewhere is the message for a name that a mod the file's mod does not
	// depend on defines. It is "" for none.
	Elsewhere string `json:"elsewhere,omitempty"`

	// HealthOnly is set for a code that only Health's report finds.
	HealthOnly bool `json:"healthOnly"`
}

// Topic is one topic of the checks, in words.
type Topic struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// Part is one piece of a sentence: plain text, or a fact drawn in its style.
type Part struct {
	Text string `json:"text"`

	// Style is mod, code, count or path. It is "" for plain text.
	Style string `json:"style,omitempty"`
}

// Diagnostic is one problem found in a file.
type Diagnostic struct {
	Code string `json:"code"`

	// Label and Level are the code's, Label as Settings names the check.
	// Message is the code's template filled with the facts, as plain text.
	Label   string            `json:"label"`
	Level   string            `json:"level"`
	Message string            `json:"message"`
	Range   jomini.Range      `json:"range"`
	Facts   map[string]string `json:"facts"`

	// Defined is where a mod the file's mod does not depend on defines the
	// name.
	Defined []Site `json:"defined,omitempty"`
}

// judged is the problems of one file as last judged.
type judged struct {
	diagnostics []Diagnostic

	// edits is the editor's change count when the file was judged on its text
	// there. It is -1 for a file judged on its text on disk.
	edits int64
}

// The questions a check answers, and the levels of a finding by how much the
// game loses.
const (
	QuestionCompatibility = "compatibility"
	QuestionHealth        = "health"
	QuestionLog           = "log"

	LevelCritical = "critical"
	LevelError    = "error"
	LevelWarning  = "warning"
	LevelInfo     = "info"
)

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

const elsewhereMessage = "{mod} uses this {database}, but the game doesn't define it: {other} does. {mod} doesn't " +
	"list {other} as a dependency, so in a game without {other} the {database} doesn't exist, and every place that " +
	"uses it finds nothing."

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
	{ID: lang.ProblemFollows, Question: QuestionHealth, Level: LevelError, Topic: "syntax",
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
	{ID: index.ProblemLiteral, Question: QuestionHealth, Level: LevelInfo, Topic: "localization",
		Label:       "Text with no localization key",
		Description: "A GUI text that no localization key has as its name, which the game shows as written.",
		Message:     "No localization key {name} exists, so the game shows this text as written.",
		Advice:      "If you meant a localization key, add it to your mod's localization file or fix the spelling here.",
		Elsewhere:   elsewhereMessage},
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
	{ID: lang.ProblemScope, Question: QuestionHealth, Level: LevelError, Topic: "values",
		Label: "Wrong scope", Description: "A key used in a scope where it does not work.",
		Message: "{name} runs in {allowed}, not in {scope}.",
		Advice:  "Move the line to a scope where it works, or switch to a scope that allows it first."},
	{ID: lang.ProblemValue, Question: QuestionHealth, Level: LevelError, Topic: "values",
		Label: "Wrong value", Description: "A value of another kind than its key takes.",
		Message: "{name} takes {expected}.",
		Advice:  "Write a value of the kind the key takes."},
	{ID: lang.ProblemDate, Question: QuestionHealth, Level: LevelError, Topic: "values",
		Label: "Impossible date", Description: "A date with a month or a day that does not exist.",
		Message: "{name} is no date: a month runs from 1 to 12, and a day from 1 to 31.",
		Advice:  "Write a real date as year.month.day."},
	{ID: index.ProblemRequired, Question: QuestionHealth, Level: LevelError, Topic: "values",
		Label: "Missing key", Description: "A definition that leaves out a key it must write.",
		Message: "The {database} {name} does not write {key}, which it must.",
		Advice:  "Add the key to the entry."},
	{ID: lang.ProblemUnknownKey, Question: QuestionHealth, Level: LevelWarning, Topic: "values",
		Label: "Unknown key", Description: "A key that the block it is written in does not take.",
		Message: "{name} is not a key a {database} takes here.",
		Advice:  "Fix the spelling, or delete the key."},
	{ID: lang.ProblemArgument, Question: QuestionHealth, Level: LevelWarning, Topic: "values",
		Label:       "Parameter not used or not set",
		Description: "A scripted trigger or effect and the place it is used disagree about a parameter.",
		Message:     "{why} parameter {name} for {callee}.",
		Advice:      "Fix the parameter's spelling, or add or remove it so both places match."},
	{ID: lang.ProblemNamespace, Question: QuestionHealth, Level: LevelCritical, Topic: "definitions",
		Label:       "Wrong namespace",
		Description: "An event whose name is not in a namespace its file declares.",
		Message:     "The {database} {name} is not in a namespace this file declares ({namespace}).",
		Advice:      "Declare the namespace at the top of the file, or rename the event to start with one the file declares."},
	{ID: lang.ProblemMode, Question: QuestionHealth, Level: LevelCritical, Topic: "definitions",
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

// Topics holds the topics of the checks, in listed order.
var Topics = []Topic{
	{ID: "syntax", Label: "Syntax"}, {ID: "references", Label: "References"},
	{ID: "values", Label: "Scopes and values"}, {ID: "localization", Label: "Localization"},
	{ID: "definitions", Label: "Definitions"}, {ID: "descriptor", Label: "Descriptor"},
}

// keyFacts holds the fact that names the key, for each code of a missing text.
var keyFacts = map[string]string{index.ProblemTitle: "key", index.ProblemLocKey: "name", index.ProblemLiteral: "name"}

// Diagnose returns the problems of a file, without the checks the user turned
// off.
func (s *Service) Diagnose(path string) ([]Diagnostic, error) {
	ws, err := s.Workspace.Session()
	if err != nil {
		return nil, err
	}

	diagnostics, err := DiagnoseFile(ws, ws.Mods.Load(), path)

	return s.listEnabled(ws.ID, diagnostics), err
}

// DiagnoseText returns the problems a file would have with the given text,
// without the checks the user turned off. It returns an error if no workspace
// is open.
//
//wails:ignore
func (s *Service) DiagnoseText(path, text string) ([]Diagnostic, error) {
	ws, err := s.Workspace.Session()
	if err != nil {
		return nil, err
	}

	mods := ws.Mods.Load()
	place := ws.Locate(mods, path)
	if !isDiagnosed(place) {
		return []Diagnostic{}, nil
	}

	return s.listEnabled(ws.ID, diagnoseDocument(ws, mods, place, ws.WalkText(place, text))), nil
}

// listEnabled drops the diagnostics of the checks the user turned off.
func (s *Service) listEnabled(workspaceID string, diagnostics []Diagnostic) []Diagnostic {
	setting, _ := s.Workspace.Config.LookupSetting(workspaceID, "ide.checks")

	var disabled []string
	for _, id := range strings.Split(setting, ",") {
		if id = strings.TrimSpace(id); id != "" {
			disabled = append(disabled, id)
		}
	}

	return slices.DeleteFunc(diagnostics, func(diagnostic Diagnostic) bool {
		return slices.Contains(disabled, diagnostic.Code)
	})
}

// DiagnoseFiles returns the problems of each file, in the order of the paths.
// A file that cannot be read has none.
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

// problems holds the problems of the files by key, judged against one index.
// A scan puts a new index in place, which drops them.
var problems struct {
	sync.Mutex
	mods  *index.Index
	files map[string]judged
}

// DiagnoseFile returns the problems of a file, judged on the editor's text
// where the file is open, else on disk. It returns an error if the file cannot
// be read. The problems are kept until a scan puts a new index in place, and
// for an open file until a buffer changes, since its text changes with no scan.
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
	last, ok := problems.files[place.Key]
	ok = ok && problems.mods == mods && last.edits == edits
	problems.Unlock()
	if ok {
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

// isDiagnosed reports whether a file can have problems. A mod's .yml file
// counts, since it may sit in a misspelled localization folder. A file of the
// game's GUI editor has none.
func isDiagnosed(place workspace.Place) bool {
	rel := strings.ToLower(place.Rel)
	if slices.ContainsFunc(lang.GUIEditorFolders, func(folder string) bool { return strings.HasPrefix(rel, folder+"/") }) {
		return false
	}

	return place.Format == files.FormatLocalization || place.Format.IsScript() || place.Format == files.FormatDescriptor ||
		place.Format == files.FormatText && place.Origin != files.OriginVanilla && strings.HasSuffix(rel, ".yml")
}

// diagnoseDocument returns the problems of a file. A mod's file is judged as
// the mod with its dependencies. A file of the game gets parse errors only, and
// one that is neither script nor localization only its folder's.
func diagnoseDocument(ws *workspace.Session, mods *index.Index, place workspace.Place, document lang.Document) []Diagnostic {
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
		if problem.Code == lang.ProblemValue {
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

// diagnoseDescriptor returns a problem if a mod's descriptor supports another
// game version than the game's. It returns none if no source shows a mismatch.
func diagnoseDescriptor(ws *workspace.Session, place workspace.Place, tree *jomini.Tree) []Diagnostic {
	descriptor, err := workspace.ParseDescriptor(place.Path, tree.Src)
	version := ws.Schema.Version
	if matches, known := workspace.MatchVersion(descriptor.SupportedVersion, version); err != nil || matches || !known {
		return []Diagnostic{}
	}

	// both descriptor forms write the version after the word supported
	span := jomini.Span{}
	if key := strings.Index(tree.Src, "supported"); key >= 0 {
		if value := strings.Index(tree.Src[key:], descriptor.SupportedVersion); value >= 0 {
			span = jomini.Span{Start: key + value, End: key + value + len(descriptor.SupportedVersion)}
		}
	}

	facts := lang.DescribeReference(lang.Reference{Name: cmp.Or(descriptor.Name, "This mod")})
	facts["mod"], facts["supported"], facts["version"] = facts["name"], descriptor.SupportedVersion, version

	return []Diagnostic{makeDiagnostic(tree, span, "supported-version", facts)}
}

func toDiagnostics(tree *jomini.Tree, problems []jomini.Problem) []Diagnostic {
	diagnostics := make([]Diagnostic, len(problems))
	for position, problem := range problems {
		diagnostics[position] = makeDiagnostic(tree, problem.Span, problem.Code, map[string]string{"why": problem.Message})
	}

	return diagnostics
}

// diagnoseHeader returns a problem if a file's name and header name different
// languages.
func diagnoseHeader(tree *jomini.Tree, file *loc.File, place workspace.Place) []Diagnostic {
	suffix := loc.FindSuffixLanguage(place.Path)
	if suffix == "" || file.Language == "" || strings.EqualFold(suffix, file.Language) {
		return nil
	}

	facts := map[string]string{"language": suffix, "header": file.Language}

	return []Diagnostic{makeDiagnostic(tree, jomini.Span{}, "loc-header-mismatch", facts)}
}

// diagnoseFolders returns a problem for each misspelled localization folder.
func diagnoseFolders(tree *jomini.Tree, place workspace.Place) []Diagnostic {
	diagnostics := []Diagnostic{}
	folders := strings.Split(strings.ToLower(place.Rel), "/")
	for _, folder := range folders[:len(folders)-1] {
		if right, ok := loc.MisspelledFolders[folder]; ok {
			diagnostics = append(diagnostics, makeDiagnostic(tree, jomini.Span{}, "wrong-localization-folder",
				map[string]string{"wrong": folder, "right": right}))
		}
	}

	return diagnostics
}

func makeDiagnostic(tree *jomini.Tree, span jomini.Span, code string, facts map[string]string) Diagnostic {
	return Diagnostic{Code: code, Range: jomini.ToRange(tree.Src, tree.Lines, span), Facts: facts}
}

var valueDescriptions = map[lang.ValueType]string{
	lang.ValueTypeBool: "yes or no", lang.ValueTypeNumber: "a number", lang.ValueTypeDate: "a date",
	lang.ValueTypeColor: "a color", lang.ValueTypeCall: "a data call", lang.ValueTypeText: "text",
	lang.ValueTypeBlock: "a block",
}

// describeExpected returns the words for what a value must be.
func describeExpected(expected lang.Expected) string {
	switch {
	case expected.Type == lang.ValueTypeChoice:
		return "one of " + strings.Join(expected.Choices, ", ")

	case expected.Type == lang.ValueTypeReference && expected.Database != "":
		return "the name of a " + lang.DescribeDefinition(expected.Database, false)

	case expected.Type == lang.ValueTypeReference:
		return "a name"

	case expected.Type == lang.ValueTypeTarget:
		description := "a link"
		if expected.Scope != "" {
			description += " to " + describeScope(expected.Scope)
		}

		if expected.Database != "" {
			description += ", or the name of a " + lang.DescribeDefinition(expected.Database, false)
		}

		return description
	}

	return valueDescriptions[expected.Type]
}

// describeDiagnostics fills in each label, level and message.
func describeDiagnostics(diagnostics []Diagnostic) []Diagnostic {
	for position := range diagnostics {
		diagnostic := &diagnostics[position]
		code := LookupCode(diagnostic.Code)
		diagnostic.Label, diagnostic.Level = code.Label, code.Level
		diagnostic.Message = JoinParts(SplitMessage(code.Template(diagnostic.Facts), diagnostic.Facts))
	}

	return diagnostics
}

// LookupCode returns the code with the given id, or the zero code.
func LookupCode(id string) Code {
	if position := slices.IndexFunc(Codes, func(code Code) bool { return code.ID == id }); position >= 0 {
		return Codes[position]
	}

	return Code{}
}

// Template returns the elsewhere message if the code has one and the facts
// name another mod, else the message.
func (c Code) Template(facts map[string]string) string {
	if c.Elsewhere != "" && facts["other"] != "" {
		return c.Elsewhere
	}

	return c.Message
}

// factStyles holds the style of each fact a message draws apart, by key.
var factStyles = map[string]string{
	"mod": "mod", "other": "mod", "winner": "mod", "beaten": "mod",
	"name": "code", "key": "code", "callee": "code", "namespace": "code",
	"file": "path", "folder": "path", "beatenFile": "path",
	"count": "count", "have": "count", "of": "count", "uses": "count", "removed": "count", "total": "count",
}

// languageFacts holds the facts that hold a language, shown as its name.
var languageFacts = []string{"language", "header"}

var factPlaceholder = regexp.MustCompile(`\{\w+\}`)

// SplitMessage fills a template with the given facts, as parts, each fact in
// its style. A placeholder with no fact stays as written.
func SplitMessage(template string, facts map[string]string) []Part {
	var pieces []string
	last := 0
	for _, match := range factPlaceholder.FindAllStringIndex(template, -1) {
		pieces = append(pieces, template[last:match[0]], template[match[0]:match[1]])
		last = match[1]
	}

	pieces = slices.DeleteFunc(append(pieces, template[last:]), func(piece string) bool { return piece == "" })

	var parts []Part
	for _, piece := range pieces {
		key := strings.Trim(piece, "{}")
		fact, ok := facts[key]
		switch {
		case !strings.HasPrefix(piece, "{") || !ok:
			parts = append(parts, Part{Text: piece})

		case slices.Contains(languageFacts, key):
			parts = append(parts, Part{Text: loc.DescribeLanguage(fact)})

		default:
			style := factStyles[key]
			if key == "name" && facts["database"] == lang.File {
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

// DescribeCount returns a count with commas between thousands, such as 1,350.
func DescribeCount(count int) string {
	text := strconv.Itoa(count)
	for start := len(text) - 3; start > 0 && text[start-1] != '-'; start -= 3 {
		text = text[:start] + "," + text[start:]
	}

	return text
}

// JoinParts returns the parts as one plain sentence.
func JoinParts(parts []Part) string {
	var text strings.Builder
	for _, part := range parts {
		text.WriteString(part.Text)
	}

	return text.String()
}

// Plural returns a count with its noun, such as 1 file or 1,350 files.
func Plural(count int, noun, nouns string) string {
	if count == 1 {
		return DescribeCount(count) + " " + noun
	}

	return DescribeCount(count) + " " + nouns
}
