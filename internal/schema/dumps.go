// dumps.go reads the files a game writes when asked to dump its script
// language, such as effects.log and the data types files.

package schema

import (
	"errors"
	"io/fs"
	"iter"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/lang"
)

// DumpCommand is one effect, trigger or link as its dump file lists it.
type DumpCommand struct {
	Name string

	// Declared is what its dump declares: an effect, a trigger or an event
	// target.
	Declared string

	// Scopes are the scope types it may be written in. Target is the one it
	// leads to, "" where the dump names none or several.
	Scopes []string
	Target string

	// Description is the rest of its entry: its sentence and its example.
	Description string

	// Path and Line locate its entry, lines counting from 1. An entry from
	// the shipped backup has neither.
	Path string
	Line int

	// Prefixed is whether a link takes a name after a colon, which the dump
	// writes as "Requires Data: yes".
	Prefixed bool

	// Wild is whether the dump marks a link "Wild Card: yes".
	Wild bool

	// Traits is what a trigger's Traits line says its value is, as "yes/no".
	// It is "" for an entry with no such line.
	Traits string
}

// Dumps is everything the dump files of one game declare.
type Dumps struct {
	// Commands are the effects, triggers and links in the order the files
	// list them. Two files may list the same name.
	Commands []DumpCommand

	Scopes    map[string]lang.ScopeType
	Modifiers map[string]bool
	OnActions map[string]lang.OnAction
	DataTypes map[string]lang.DataType

	// Customs holds the name of every customizable localization.
	Customs map[string]bool

	// WrittenAt and OldestAt are when the newest and the oldest of the dump
	// files were written. Each is the zero time if none was read.
	WrittenAt time.Time
	OldestAt  time.Time

	// Folders are the folders the dump files were read from.
	Folders []string
}

// What a dump file declares. The dumps call a link an event target.
const (
	declaredEffect  = "effect"
	declaredTrigger = "trigger"
	declaredLink    = "event target"
)

// dumpSection pairs what a dump file declares with the database it fills.
type dumpSection struct{ declared, database string }

// dumpDatabases are the sections, in the order the files are read.
var dumpDatabases = []dumpSection{
	{declaredEffect, lang.DumpEffects}, {declaredTrigger, lang.DumpTriggers}, {declaredLink, lang.DumpLinks},
}

// dumpFiles are the dump files read, by name.
var dumpFiles = []string{
	"effects.log", "triggers.log", "event_targets.log", "modifiers.log", "on_actions.log", "event_scopes.log",
	"custom_localization.log", "data_types_common.txt", "data_types_gui.txt", "data_types_internalclausewitzgui.txt",
	"data_types_script.txt", "data_types_uncategorized.txt",
}

// titleLine is a line that is only a name and a colon, which opens an entry
// in several dump files.
var titleLine = regexp.MustCompile(`^([A-Za-z0-9_$]+):$`)

// FindDumpsAt returns when the newest dump file in the folders was written,
// or the zero time if there is none.
func FindDumpsAt(dirs []string) (time.Time, error) {
	var newest time.Time
	for _, file := range dumpFiles {
		_, fileAt, err := findDump(dirs, file)
		if err != nil {
			return time.Time{}, err
		}

		if fileAt.After(newest) {
			newest = fileAt
		}
	}

	return newest, nil
}

// ReadDumps reads the newest copy of each dump file in the folders. A file
// that is missing or cannot be read as text is left out, for the shipped
// backup to fill. It fails only if a file's time cannot be read.
func ReadDumps(dirs []string) (*Dumps, error) {
	dumps := &Dumps{
		Scopes: map[string]lang.ScopeType{}, Modifiers: map[string]bool{}, OnActions: map[string]lang.OnAction{},
		DataTypes: map[string]lang.DataType{}, Customs: map[string]bool{},
	}

	for _, file := range dumpFiles {
		path, fileAt, err := findDump(dirs, file)
		if err != nil {
			return nil, err
		}

		if fileAt.After(dumps.WrittenAt) {
			dumps.WrittenAt = fileAt
		}

		if path == "" {
			continue
		}

		text, err := files.ReadText(path)
		if err != nil {
			continue
		}

		if dumps.OldestAt.IsZero() || fileAt.Before(dumps.OldestAt) {
			dumps.OldestAt = fileAt
		}

		dumps.parseFile(path, text.Text)
		if dir := filepath.Dir(path); !slices.Contains(dumps.Folders, dir) {
			dumps.Folders = append(dumps.Folders, dir)
		}
	}

	return dumps, nil
}

// findDump returns the newest copy of one dump file in the folders and when
// it was written. The path is "" if no folder has the file.
func findDump(dirs []string, file string) (string, time.Time, error) {
	newest, newestAt := "", time.Time{}
	for _, dir := range dirs {
		path := filepath.Join(dir, file)
		stat, err := os.Stat(path)
		switch {
		case errors.Is(err, fs.ErrNotExist):
		case err != nil:
			return "", time.Time{}, err
		case stat.ModTime().After(newestAt):
			newest, newestAt = path, stat.ModTime()
		}
	}

	return newest, newestAt, nil
}

func (d *Dumps) parseFile(path, text string) {
	switch filepath.Base(path) {
	case "effects.log":
		d.parseCommands(path, text, declaredEffect)
	case "triggers.log":
		d.parseCommands(path, text, declaredTrigger)
	case "event_targets.log":
		d.parseCommands(path, text, declaredLink)
	case "modifiers.log":
		d.parseModifiers(text)
	case "on_actions.log":
		maps.Copy(d.OnActions, parseOnActions(text))
	case "event_scopes.log":
		d.parseScopes(text)
	case "custom_localization.log":
		for head := range parseSections(text, true) {
			if name, ok := strings.CutSuffix(head, ":"); ok {
				d.Customs[name] = true
			}
		}
	default:
		d.parseDataTypes(text)
	}
}

// scopeLine is a line of an entry that lists scope types, as
// "Supported Scopes: character, landed_title" does.
var scopeLine = regexp.MustCompile(`^\*{0,2}(Supported Scopes|Supported Targets|Input Scopes|Output Scopes)\*{0,2}:\s*(.*)$`)

// parseCommands reads the entries of effects.log, triggers.log or
// event_targets.log.
func (d *Dumps) parseCommands(path, text, declared string) {
	offset, number := 0, 1
	for head, body := range parseSections(text, true) {
		if head == "Event Targets Saved from Code:" {
			continue
		}

		if start := strings.Index(text[offset:], head); start >= 0 {
			number += strings.Count(text[offset:offset+start], "\n")
			offset += start
		}

		title, first, _ := strings.Cut(head, " - ")
		command := DumpCommand{Name: strings.TrimSpace(title), Declared: declared, Path: path, Line: number}
		lines := []string{first}

		for _, line := range body {
			scope := scopeLine.FindStringSubmatch(line)
			switch trimmed := strings.TrimSpace(line); {
			case trimmed == "Requires Data: yes":
				command.Prefixed = true
			case trimmed == "Global Link: yes": // dropped from the description
			case trimmed == "Wild Card: yes":
				command.Wild = true
			case strings.HasPrefix(trimmed, "Traits:"):
				command.Traits = strings.TrimSpace(strings.TrimPrefix(trimmed, "Traits:"))
			case scope == nil:
				lines = append(lines, line)
			case scope[1] == "Supported Scopes" || scope[1] == "Input Scopes":
				command.Scopes = splitScopes(scope[2])
			default:
				// a command leads to a scope only where it names exactly one
				if targets := splitScopes(scope[2]); len(targets) == 1 {
					command.Target = targets[0]
				}
			}
		}

		command.Description = strings.TrimSpace(strings.Join(lines, "\n"))
		d.Commands = append(d.Commands, command)
	}
}

// An entry of a dump file starts after a line of ten or more dashes, or at a
// markdown heading that names it.
var (
	dashesLine = regexp.MustCompile(`^-{10,}$`)
	headingRow = regexp.MustCompile(`^#{2,3} +(\S+)$`)
)

// parseSections yields each entry of a dump file: its first line and the
// lines after it. Where the file starts with a title of its own, the lines
// before the first entry are skipped.
func parseSections(text string, hasTitle bool) iter.Seq2[string, []string] {
	return func(yield func(string, []string) bool) {
		head, body, isOpen := "", []string(nil), !hasTitle
		for line := range strings.Lines(text) {
			line = strings.TrimRight(line, " \t\r\n")
			heading := headingRow.FindStringSubmatch(line)
			switch {
			case dashesLine.MatchString(line) || heading != nil:
				if head != "" && !yield(head, body) {
					return
				}

				head, body, isOpen = "", nil, true
				if heading != nil {
					head = heading[1]
				}

			case !isOpen:

			case head == "":
				head = strings.TrimSpace(line)

			default:
				body = append(body, line)
			}
		}

		if head != "" {
			yield(head, body)
		}
	}
}

// splitScopes returns the scope types a line lists between commas, without
// the words the dumps write for no scope, such as none.
func splitScopes(text string) []string {
	return slices.DeleteFunc(splitList(text), func(scope string) bool {
		return slices.Contains(lang.NoScope, scope)
	})
}

// splitList returns the items a text lists between commas, trimmed, without
// empty ones.
func splitList(text string) []string {
	var parts []string
	for part := range strings.SplitSeq(text, ",") {
		if part = strings.TrimSpace(part); part != "" {
			parts = append(parts, part)
		}
	}

	return parts
}

// tagLine is a line of modifiers.log that lists one modifier with its
// categories, as "Tag: stress_gain_mult, Categories: character" does.
var tagLine = regexp.MustCompile(`^Tag:\s*([^,\s]+)(?:,\s*Categories:\s*(.*))?$`)

// parseModifiers reads modifiers.log, which writes a modifier's name after
// "Tag:" or alone before a colon.
func (d *Dumps) parseModifiers(text string) {
	for line := range strings.Lines(text) {
		line = strings.TrimRight(line, " \t\r\n")
		if tag := tagLine.FindStringSubmatch(line); tag != nil {
			d.Modifiers[tag[1]] = true
		} else if titleLine.MatchString(line) {
			d.Modifiers[strings.TrimSuffix(line, ":")] = true
		}
	}
}

// parseOnActions reads on_actions.log: for each on_action, the scope it runs
// in and whether the game fires it from its own code.
func parseOnActions(text string) map[string]lang.OnAction {
	actions := map[string]lang.OnAction{}
	for head, body := range parseSections(text, true) {
		if name, ok := strings.CutSuffix(head, ":"); ok {
			pairs := parsePairs(body)
			actions[name] = lang.OnAction{
				Scope:     strings.Join(splitScopes(pairs["Expected Scope"]), ""),
				Hardcoded: pairs["From Code"] == "Yes",
			}
		}
	}

	return actions
}

// pairLine is a line of an entry written "Key: value".
var pairLine = regexp.MustCompile(`^\s*([A-Za-z ]+?):\s*(.*)$`)

// parsePairs returns the "Key: value" lines of an entry, by key.
func parsePairs(body []string) map[string]string {
	pairs := map[string]string{}
	for _, line := range body {
		if pair := pairLine.FindStringSubmatch(line); pair != nil {
			pairs[pair[1]] = strings.TrimSpace(pair[2])
		}
	}

	return pairs
}

// parseScopes reads event_scopes.log: each scope type, with whether triggers
// are evaluated and effects run in it.
func (d *Dumps) parseScopes(text string) {
	title := ""
	for line := range strings.Lines(text) {
		line = strings.TrimSpace(line)
		key, value, _ := strings.Cut(line, ":")
		scope, isYes := d.Scopes[title], strings.TrimSpace(value) == lang.Yes
		switch {
		case titleLine.MatchString(line):
			title, scope = key, lang.ScopeType{}
		case title == "":
			continue
		case key == "Evaluate Triggers":
			scope.Triggers = isYes
		case key == "Execute Effects":
			scope.Effects = isYes
		}

		d.Scopes[title] = scope
	}
}

// signature is the first line of a data function's entry: its name and, in
// parentheses, its arguments, as "Character.GetName( Arg0 )".
var signature = regexp.MustCompile(`^(\S+?)\s*(?:\(\s*(.*?)\s*\))?$`)

// selfFunctions return the object they are called on. The dumps give their
// return type as [unregistered], so the type they belong to is used.
var selfFunctions = []string{"Self", "AccessSelf"}

// parseDataTypes reads one of the data types files. A function belongs to
// the type written before its dot, or to the global type. Of a function
// listed twice, the first entry is kept.
func (d *Dumps) parseDataTypes(text string) {
	for head, body := range parseSections(text, false) {
		parts := signature.FindStringSubmatch(head)
		if parts == nil {
			continue
		}

		pairs := parsePairs(body)
		definition := pairs["Definition type"]
		owner, label := lang.GlobalType, parts[1]
		if !strings.HasPrefix(definition, "Global ") {
			owner, label, _ = strings.Cut(parts[1], ".")
		}

		dataType := d.DataTypes[owner]
		if dataType.Functions == nil {
			dataType = lang.DataType{Name: owner, Functions: map[string]lang.DataFunction{}}
		}

		returns := pairs["Return type"]
		if slices.Contains(selfFunctions, label) && owner != lang.GlobalType {
			returns = owner
		}

		if _, exists := dataType.Functions[label]; definition != "Type" && label != "" && !exists {
			dataType.Functions[label] = lang.DataFunction{
				Name: label, Returns: returns, Arguments: splitList(parts[2]), Blurb: pairs["Description"],
				Expansion: pairs["Macro replacement"], Promote: strings.HasSuffix(strings.ToLower(definition), "promote"),
			}
		}

		d.DataTypes[owner] = dataType
	}
}

// resolveMissing takes from the shipped backup each part the install's dumps
// lack, each section and table on its own. It reports whether it took any.
func (d *Dumps) resolveMissing(backup *Dumps) bool {
	missing := false
	for _, section := range dumpDatabases {
		declares := func(command DumpCommand) bool { return command.Declared == section.declared }
		if slices.ContainsFunc(d.Commands, declares) {
			continue
		}

		for _, command := range backup.Commands {
			if declares(command) {
				d.Commands = append(d.Commands, command)
			}
		}

		missing = true
	}

	tables := []bool{
		takeMissing(&d.Scopes, backup.Scopes), takeMissing(&d.Modifiers, backup.Modifiers),
		takeMissing(&d.OnActions, backup.OnActions), takeMissing(&d.DataTypes, backup.DataTypes),
		takeMissing(&d.Customs, backup.Customs),
	}

	return missing || slices.Contains(tables, true)
}

// takeMissing puts the backup's table in place of an empty one, and reports
// whether it did.
func takeMissing[T any](table *map[string]T, backup map[string]T) bool {
	if len(*table) > 0 {
		return false
	}

	*table = backup

	return true
}

// dumpCorrection is a command whose scopes in the dump contradict the dump's
// own sentence about it, with the scopes and target to use instead.
type dumpCorrection struct {
	// Games lists the games the row holds for, all if none.
	Games []string

	Command string
	Scopes  []string
	Target  string
}

// dumpCorrections are the commands whose dump entries say they go through
// the provinces of a state, but list the two scopes swapped.
var dumpCorrections = []dumpCorrection{
	{Games: []string{game.Vic3}, Command: "every_province", Scopes: []string{"state"}, Target: "province"},
	{Games: []string{game.Vic3}, Command: "ordered_province", Scopes: []string{"state"}, Target: "province"},
	{Games: []string{game.Vic3}, Command: "random_province", Scopes: []string{"state"}, Target: "province"},
	{Games: []string{game.Vic3}, Command: "any_province", Scopes: []string{"state"}, Target: "province"},
}

// applyCorrections gives each command that has a correction for the game the
// correction's scopes and target.
func (d *Dumps) applyCorrections(dialect *lang.Dialect) {
	for position, command := range d.Commands {
		for _, correction := range dumpCorrections {
			if correction.Command == command.Name && dialect.Matches(correction.Games) {
				d.Commands[position].Scopes, d.Commands[position].Target = correction.Scopes, correction.Target
			}
		}
	}
}
