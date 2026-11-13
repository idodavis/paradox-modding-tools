// dumps.go reads the files a game writes when asked to dump its script
// language (effects.log, triggers.log, event_targets.log, the data types
// files and the rest), and builds the schema's effects, triggers, links,
// scope types and data types from them.

package schema

import (
	"cmp"
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
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/lang"
)

// DumpCommand is one effect, trigger or link as its dump file lists it,
// before anything is worked out from it.
type DumpCommand struct {
	Name string

	// Declared is what its dump declares: an effect, a trigger or an event
	// target.
	Declared string

	// Scopes are the scope types the dump says it may be written in. Target
	// is the one scope type it leads to, "" where the dump names none or
	// several.
	Scopes []string
	Target string

	// Description is the rest of its entry: the sentence about it and the
	// example of how it is written.
	Description string

	// Path and Line say where its entry is in its dump file, lines counting
	// from 1. One from the shipped backup has neither.
	Path string
	Line int

	// Prefixed is whether a link is written with a name after a colon, as
	// the dump's "Requires Data: yes" says.
	Prefixed bool

	// Wild is whether the dump marks a link "Wild Card: yes".
	Wild bool

	// Traits is what a trigger's Traits line says its value is, as "yes/no"
	// or "<, <=, =, !=, >, >=". It is "" for an entry with no such line.
	Traits string
}

// Dumps is everything the dump files of one game declare, as read from the
// files.
type Dumps struct {
	// Commands are the effects, triggers and links, in the order the files
	// list them. Two files may list the same name.
	Commands []DumpCommand

	// Scopes holds the scope types, by name.
	Scopes map[string]lang.ScopeType

	// Modifiers holds the name of every modifier.
	Modifiers map[string]bool

	OnActions map[string]lang.OnAction
	DataTypes map[string]lang.DataType

	// Customs holds the name of every customizable localization.
	Customs map[string]bool

	// WrittenAt and OldestAt are when the newest and the oldest of the dump
	// files read were written. Each is the zero time if none was read.
	WrittenAt time.Time
	OldestAt  time.Time

	// Folders are the folders the dump files were read from.
	Folders []string
}

// dumpCorrection is one command whose scopes in the dump contradict the
// dump's own sentence about it. The row's scopes and target are used in
// place of the dump's.
type dumpCorrection struct {
	// Games lists the games the row holds for, all if none.
	Games []string

	Command string
	Scopes  []string
	Target  string
}

// commandKey is one command, or one key of a command's block, that takes a
// name of some database though the dump's example of it does not say so.
type commandKey struct {
	// Games lists the games the row holds for, all if none.
	Games []string

	// Command is the command. Written command/key it is a key inside the
	// command's block.
	Command string

	// Database is the database the keys of its block name, and Value the
	// database its value names. Each is "" for none.
	Database, Value string

	// Stem is the name the value stands for in its database, with %s for
	// the value, as %s_title. It is "" where the value is the name itself.
	Stem string
}

// What a dump file declares. The dumps call a link an event target.
const (
	declaredEffect  = "effect"
	declaredTrigger = "trigger"
	declaredLink    = "event target"
)

// dumpDatabases holds the database for each thing a dump file declares, in
// the order the files are read.
var dumpDatabases = []struct{ declared, database string }{
	{declaredEffect, lang.DumpEffects}, {declaredTrigger, lang.DumpTriggers}, {declaredLink, lang.DumpLinks},
}

// sharedWords are the names the dumps list as both an effect and a trigger
// whose two entries differ only in what their block holds. Each pair shares
// one set of keys.
var sharedWords = []string{
	"switch", "assert_if", "assert_read", "debug_log", "save_temporary_scope_value_as",
	"save_temporary_opinion_value_as",
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

// usageToken is one piece of the example a dump writes for a command: a
// brace, an operator, a word, or a group in <> or [].
var usageToken = regexp.MustCompile(`[{}]|[<>!=?]=|<[^<>\n]*>|\[[^\[\]\n]*\]|[=<>]|[^\s{}=<>\[\]]+`)

// usageComment is a comment inside such an example, as CK3's stress_impact
// writes "# multiple 'trait = value' lines".
var usageComment = regexp.MustCompile(`#[^\n]*`)

// usageOperators are the operators a key in such an example may be written
// with.
var usageOperators = []string{"=", "==", "!=", "<", ">", "<=", ">=", "?="}

// FindDumpsAt returns when the newest dump file in the given folders was
// written. It returns the zero time if the folders hold no dump file, and an
// error if a file's time cannot be read.
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

// ReadDumps reads the dump files in the given folders, the newest copy of
// each. A file that is missing, or that cannot be read as text, is left out,
// and the shipped backup fills its place later. It returns an error only if
// a file's time cannot be read.
func ReadDumps(dirs []string) (*Dumps, error) {
	dumpsAt, err := FindDumpsAt(dirs)
	if err != nil {
		return nil, err
	}

	dumps := &Dumps{
		Scopes: map[string]lang.ScopeType{}, Modifiers: map[string]bool{}, OnActions: map[string]lang.OnAction{},
		DataTypes: map[string]lang.DataType{}, Customs: map[string]bool{}, WrittenAt: dumpsAt,
	}

	for _, file := range dumpFiles {
		path, fileAt, err := findDump(dirs, file)
		if err != nil {
			return nil, err
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

// findDump returns the path of the newest copy of one dump file in the given
// folders, and when it was written. The path is "" if no folder has the
// file. It returns an error if a copy's time cannot be read.
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

// parseFile reads the text of one dump file, by the file's name.
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
// event_targets.log: each one's name, the scopes it is written in, the scope
// it leads to, and the rest as its description.
func (d *Dumps) parseCommands(path, text, declared string) {
	offset, line := 0, 1
	for head, body := range parseSections(text, true) {
		// the targets saved from code
		if head == "Event Targets Saved from Code:" {
			continue
		}

		if start := strings.Index(text[offset:], head); start >= 0 {
			line += strings.Count(text[offset:offset+start], "\n")
			offset += start
		}

		title, first, _ := strings.Cut(head, " - ")
		command := DumpCommand{Name: strings.TrimSpace(title), Declared: declared, Path: path, Line: line}
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
				// the target is the one scope the entry leads to, none if several
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

// splitList returns the items a text lists between commas, without the
// spaces around them and without empty items.
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

// parseModifiers reads modifiers.log: the name of each modifier, written
// after "Tag:" or alone before a colon.
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

// parsePairs returns the "Key: value" lines of an entry, by key. Where a key
// is written twice, the last one is kept.
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
	var lines []string
	flush := func() {
		if len(lines) == 0 {
			return
		}

		title, _ := strings.CutSuffix(lines[0], ":")
		pairs := parsePairs(lines[1:])
		d.Scopes[title] = lang.ScopeType{
			Triggers: pairs["Evaluate Triggers"] == lang.Yes, Effects: pairs["Execute Effects"] == lang.Yes,
		}
		lines = nil
	}

	for line := range strings.Lines(text) {
		line = strings.TrimSpace(line)
		if titleLine.MatchString(line) {
			flush()
		}

		if line != "" && (len(lines) > 0 || titleLine.MatchString(line)) {
			lines = append(lines, line)
		}
	}

	flush()
}

// signature is the first line of a data function's entry: its name and, in
// parentheses, its arguments, as "Character.GetName( Arg0 )".
var signature = regexp.MustCompile(`^(\S+?)\s*(?:\(\s*(.*?)\s*\))?$`)

// selfFunctions are the functions that return the very object they are
// called on, as EU5's Goods.Self does. The dumps give their return type as
// [unregistered].
var selfFunctions = []string{"Self", "AccessSelf"}

// parseDataTypes reads one of the data types files. A function belongs to
// the data type written before its dot, and a global function to the global
// type. Where a function is listed more than once, the first entry is kept.
// An entry for a data type itself adds the type and no function.
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

// The origins of the dumps' data: none, the install, or the shipped backup.
const (
	originNone    = "none"
	originInstall = "install"
	originShipped = "shipped"
)

// buildDumps reads the dump files and builds from them the schema's effects,
// triggers, links, scope types, modifiers, on_actions and data types. What
// the install's dumps lack comes from the shipped backup. It also records
// where the data came from and whether it is older than the install. It
// returns an error if a dump file's time cannot be read.
func (b *builder) buildDumps(dialect *lang.Dialect, dumpDirs []string, installPath string) error {
	dumps, err := ReadDumps(dumpDirs)
	if err != nil {
		return err
	}

	backup, hasBackup := LoadShipped(dialect.ID)
	isFilled := hasBackup && dumps.resolveMissing(backup)
	dumps.applyCorrections(dialect)

	state := lang.SourceState{Origin: originNone, WrittenAt: dumps.WrittenAt, Folders: dumps.Folders}
	switch {
	case !dumps.WrittenAt.IsZero():
		state.Origin = originInstall
		state.Stale = dumps.OldestAt.Before(game.ReadInstallUpdatedAt(installPath))

	case isFilled:
		state.Origin = originShipped
	}

	b.Dumps = state
	b.Customs, b.DataTypes = dumps.Customs, dumps.DataTypes
	b.buildTypes(dumps)
	b.buildCommands(dumps.Commands)
	b.buildPrefixes()
	b.buildCommandKeys(dialect)
	b.buildKeywordShapes(dialect)
	b.buildFamilies()

	for _, database := range b.Databases {
		database.Keys = slices.Sorted(maps.Keys(database.Definitions))
	}

	return nil
}

// resolveMissing takes from the shipped backup each whole part the install's
// dumps lack: the effects, the triggers, the links, the scope types, the
// modifiers, the on_actions, the data types and the customizable
// localizations, each on its own. It reports whether it took any.
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

// takeMissing puts the backup's table in place of an empty one. It reports
// whether it did.
func takeMissing[T any](table *map[string]T, backup map[string]T) bool {
	if len(*table) > 0 {
		return false
	}

	*table = backup

	return true
}

// dumpCorrections are the commands whose scopes in the dump contradict the
// dump's own sentence about them. Each row cites the lines.
var dumpCorrections = []dumpCorrection{
	// Vic3 docs/effects.log:3521 says "Iterate through all Provinces in the
	// scoped State"; :3523-3524 swap the two
	{Games: []string{game.Vic3}, Command: "every_province", Scopes: []string{"state"}, Target: "province"},
	// Vic3 docs/effects.log:14240 says the same; :14249-14250 swap them
	{Games: []string{game.Vic3}, Command: "ordered_province", Scopes: []string{"state"}, Target: "province"},
	// Vic3 docs/effects.log:29483 says the same; :29485-29486 swap them
	{Games: []string{game.Vic3}, Command: "random_province", Scopes: []string{"state"}, Target: "province"},
	// Vic3 docs/triggers.log:2360 says the same; :2363-2364 swap them
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

// buildTypes makes the databases the dumps fill, and in them a definition
// for each scope type, each modifier and each on_action. Script runs in a
// scope type if its entry says triggers or effects do.
func (b *builder) buildTypes(dumps *Dumps) {
	for _, database := range lang.Dumps {
		b.Databases[database] = &lang.Database{Name: database, Field: &lang.Field{}, Definitions: map[string]*lang.Definition{}}
	}

	types := b.Databases[lang.DumpTypes]
	for key, scope := range dumps.Scopes {
		types.Definitions[key] = &lang.Definition{Database: types.Name, Key: key, Runs: scope.Triggers || scope.Effects}
	}

	for key := range dumps.Modifiers {
		b.Databases[lang.DumpModifiers].Definitions[key] = &lang.Definition{Database: lang.DumpModifiers, Key: key}
	}

	// a game may have no on_actions database
	actions := b.Databases[b.OnActions]
	if actions == nil {
		return
	}

	actions.Definitions = map[string]*lang.Definition{}
	for key, action := range dumps.OnActions {
		actions.Definitions[key] = &lang.Definition{
			Database: actions.Name, Key: key, Target: action.Scope, Hardcoded: action.Hardcoded,
		}
	}
}

// buildCommands makes a definition for each effect, trigger and link the
// dumps list, with the keys its example writes. A name listed more than once
// in one database is one definition. The few names whose effect and trigger
// differ only in what their block holds share one set of keys.
func (b *builder) buildCommands(dumps []DumpCommand) {
	for _, dump := range dumps {
		position := slices.IndexFunc(dumpDatabases, func(section struct{ declared, database string }) bool {
			return section.declared == dump.Declared
		})
		if position < 0 {
			continue
		}

		// a link written with a name after a colon is kept apart from the
		// same word written alone, under its name and a colon
		key := dump.Name
		if dump.Declared == declaredLink && dump.Prefixed {
			key += ":"
		}

		owner := b.Databases[dumpDatabases[position].database]
		definition := owner.Definitions[key]
		isListed := definition != nil
		if !isListed {
			definition = &lang.Definition{Database: owner.Name, Key: key, Path: dump.Path, Line: dump.Line}
			owner.Definitions[key] = definition

			prose, usage := splitUsage(dump.Description)
			definition.Field, definition.ArgumentKey = b.parseArguments(dump.Name, prose, usage)
		}

		buildDefinition(definition, dump, isListed)
		if expected, ok := b.resolveTraits(dump.Traits); ok {
			makeField(definition, lang.SourceDumps).Value.Resolve(expected, lang.SourceDumps)
		}

		b.resolveTitleValue(definition, dump)
	}

	for _, key := range sharedWords {
		effect, trigger := b.LookupDefinition(lang.DumpEffects, key), b.LookupDefinition(lang.DumpTriggers, key)
		if effect == nil || trigger == nil {
			continue
		}

		shared := uniteCommands(effect, trigger)
		effect.Field, trigger.Field = shared, shared
	}
}

// usageLine is a line that opens a block, as key = { does. describedLine is
// such a block closed and described on the same line, after a dash, which is
// prose and not an example.
var (
	usageLine     = regexp.MustCompile(`^\s*\S+\s*=\s*\{`)
	describedLine = regexp.MustCompile(`\}\s+-\s+\S`)
)

// splitUsage splits a command's description into the sentences about it and
// the example of how it is written, which starts at the first line that
// opens a block. The example is "" if no line opens one.
func splitUsage(description string) (prose, usage string) {
	lines := strings.Split(description, "\n")
	cut := slices.IndexFunc(lines, func(line string) bool {
		return usageLine.MatchString(line) && !describedLine.MatchString(line)
	})
	if cut < 0 {
		return description, ""
	}

	return strings.Join(lines[:cut], "\n"), strings.Join(lines[cut:], "\n")
}

// parseArguments reads the keys a command's block takes from its
// description: the keys of the example block written under the command's
// name, and the lines of its sentences written as key = value. It returns
// them as a field, nil if there are none. It also returns the first key of
// the example, which is the key an argument in parentheses stands for, and
// "" if there is none.
func (b *builder) parseArguments(title, prose, usage string) (*lang.Field, string) {
	arguments, first := &lang.Field{Source: lang.SourceDumps}, ""
	prose, usage = usageComment.ReplaceAllString(prose, ""), usageComment.ReplaceAllString(usage, "")
	lines := strings.Split(prose, "\n")

	for position, line := range lines {
		tokens := usageToken.FindAllString(line, -1)

		// a key alone whose block opens on the next line is a scope, as
		// Vic3's "power_bloc =" is
		opens := len(tokens) == 2 && position+1 < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[position+1]), "{")
		if len(tokens) > 1 && tokens[0] != title && jomini.Classify(tokens[0]).IsIdentifier() && tokens[1] == "=" && !opens {
			b.parseUsageStatement(tokens, 0, arguments, false)
		}
	}

	tokens := usageToken.FindAllString(prose+"\n"+usage, -1)
	for position := 0; position+2 < len(tokens); position++ {
		if tokens[position] == title && tokens[position+1] == "=" && tokens[position+2] == "{" {
			start := position + 3
			if first == "" && start+1 < len(tokens) && jomini.Classify(tokens[start]).IsIdentifier() &&
				slices.Contains(usageOperators, tokens[start+1]) {
				first = tokens[start]
			}

			position = b.parseUsageBlock(tokens, start, arguments) - 1
		}
	}

	if len(arguments.Children) == 0 && !arguments.Shape.IsStated() {
		return nil, ""
	}

	return arguments, first
}

// parseUsageStatement reads one key = value of an example, starting at the
// given piece, and adds the key to the given field. The flag says the key
// may be left out. Keys joined by slashes each get the value. It returns the
// place of the piece after the statement.
func (b *builder) parseUsageStatement(tokens []string, cursor int, field *lang.Field, optional bool) int {
	if cursor+1 >= len(tokens) || !slices.Contains(usageOperators, tokens[cursor+1]) {
		return cursor + 1
	}

	// a key with no value, as create_character's "age =", is still declared
	hasValue := cursor+2 < len(tokens) && tokens[cursor+2] != "}"
	next := cursor + 2
	for part := range strings.SplitSeq(tokens[cursor], "/") {
		// a part between slashes that is no key still has its value read,
		// and the value is thrown away
		argument := &lang.Field{}
		if jomini.Classify(part).IsIdentifier() {
			argument = field.BuildChild(part, lang.SourceDumps)
		}

		if optional {
			argument.Required.Resolve(lang.RequirementOptional, lang.SourceDumps)
		}

		if hasValue {
			next = b.parseUsageValue(tokens, cursor+2, argument)
		}
	}

	return next
}

// parseUsageValue reads the value an example writes for a key and gives the
// key what the value says it takes: a block, one of a list written a|b, a
// type such as <bool>, or what a placeholder such as <character> names. A
// plain sample value, as 10, says nothing. It returns the place of the piece
// after the value.
func (b *builder) parseUsageValue(tokens []string, cursor int, argument *lang.Field) int {
	value := tokens[cursor]
	term := strings.TrimSuffix(strings.TrimPrefix(value, "<"), ">")
	_, isType := docTypes[lang.ToPhrase(term)]
	switch {
	case value == "{":
		return b.parseUsageBlock(tokens, cursor+1, argument)
	case strings.ContainsAny(value[:1], "<[") && strings.Contains(value, "|"):
		resolveChoices(argument, strings.Split(strings.Trim(value, "<>[]"), "|"), lang.SourceDumps)
	case isType && len(term) > 1:
		resolveDocType(argument, term, lang.SourceDumps)
	case value[0] == '<':
		if expected, ok := b.resolvePlaceholder(term); ok {
			argument.Value.Resolve(expected, lang.SourceDumps)
		}
	}

	return cursor + 1
}

// parseUsageBlock reads one block of an example and adds its keys to the
// given field. A placeholder alone, as <triggers>, says what the block
// holds. A statement written in [ ] or < > may be left out. It returns the
// place of the piece after the block's closing brace.
func (b *builder) parseUsageBlock(tokens []string, cursor int, field *lang.Field) int {
	for cursor < len(tokens) && tokens[cursor] != "}" {
		token := tokens[cursor]
		switch {
		case token == "{":
			cursor = b.parseUsageBlock(tokens, cursor+1, &lang.Field{})

		case (token[0] == '<' || token[0] == '[') && strings.Contains(token, "="):
			b.parseUsageStatement(usageToken.FindAllString(token[1:len(token)-1], -1), 0, field, true)
			cursor++

		case token[0] == '<':
			if shape, ok := findPlaceholderShape(token[1:]); ok {
				field.Shape.Resolve(shape, lang.SourceDumps)
			}

			cursor++

		default:
			cursor = b.parseUsageStatement(tokens, cursor, field, false)
		}
	}

	return cursor + 1
}

// findPlaceholderShape returns what a block holds when an example writes a
// placeholder alone in it: <triggers> and <character_trigger> both say
// triggers. It returns false for a placeholder that names no kind of block.
func findPlaceholderShape(placeholder string) (lang.Shape, bool) {
	shape := lang.Shape(strings.TrimSuffix(placeholder, ">"))
	shape = shape[strings.LastIndex(string(shape), "_")+1:]
	if !strings.HasSuffix(string(shape), "s") {
		shape += "s"
	}

	_, ok := lang.ShapeRows[shape]

	return shape, ok
}

// resolvePlaceholder returns what a placeholder written as a value stands
// for. One that ends in key or name, as <effect_localization_key>, stands
// for a name of the database its other words lead to. Any other stands for a
// link to a scope of the type it names, as <character> does. It returns
// false for a placeholder that is neither.
func (b *builder) resolvePlaceholder(term string) (lang.Expected, bool) {
	words := strings.Fields(lang.ToPhrase(term))
	if len(words) == 0 {
		return lang.Expected{}, false
	}

	last, stem := words[len(words)-1], strings.Join(words[:len(words)-1], " ")
	if last == "key" || last == "name" {
		database, ok := b.ResolveTypeDatabase(stem)

		return lang.Expected{Type: lang.ValueTypeReference, Database: database}, ok
	}

	if last != "scope" && last != "target" {
		stem = strings.Join(words, " ")
	}

	scope := strings.ReplaceAll(stem, " ", "_")
	if !b.IsScope(scope) {
		return lang.Expected{}, false
	}

	database, _ := b.ResolveTypeDatabase(scope)

	return lang.Expected{Type: lang.ValueTypeTarget, Scope: scope, Database: database}, true
}

// dumpIterates is how the dumps begin the sentence about a command that
// runs its block on each item of a list.
const dumpIterates = "Iterate through"

// buildDefinition gives a definition what one dump entry says about it: its
// scopes, the scope it leads to, its sentence and its example. Its keys are
// read elsewhere. A name listed before gets the scopes of both entries, and
// no scopes at all if either entry lists none. A command whose sentence says
// it goes through a list, and that leads to no scope, is marked as running
// its block on each item.
func buildDefinition(definition *lang.Definition, dump DumpCommand, isListed bool) {
	switch {
	case !isListed:
		definition.Scopes = slices.Clone(dump.Scopes)
	case len(dump.Scopes) == 0 || len(definition.Scopes) == 0:
		definition.Scopes = nil
	default:
		definition.Scopes = slices.Compact(slices.Sorted(slices.Values(slices.Concat(definition.Scopes, dump.Scopes))))
	}

	definition.Target = cmp.Or(definition.Target, dump.Target)
	prose, usage := splitUsage(dump.Description)
	blurb, _, _ := strings.Cut(strings.TrimSpace(prose), "\n")
	blurb = strings.TrimSpace(blurb)
	definition.Blurb = cmp.Or(definition.Blurb, blurb)
	definition.Usage = cmp.Or(definition.Usage, usage)
	if dump.Declared == declaredLink {
		definition.Wild, definition.Prefixed = dump.Wild, dump.Prefixed
	}

	definition.Iterates = definition.Iterates || (strings.HasPrefix(blurb, dumpIterates) && definition.Target == "")
}

// The forms of a trigger's Traits line: a comparison (with "valid date" for
// a date), a class of keys, a scope, a tag or id with a scope, and a list of
// values.
var (
	comparisonTraits = regexp.MustCompile(`^<, <=, =, (?:!=, )?>, >=( valid date)?$`)
	classTraits      = regexp.MustCompile(`^class (C\w+) key$`)
	targetTraits     = regexp.MustCompile(`^(\w+) (?:scope|target)(?: \w+ scope)?$`)
	tagTraits        = regexp.MustCompile(`^\w+ (?:tag|id)/(\w+) scope$`)
	choiceTraits     = regexp.MustCompile(`^(?:Valid [\w ]+: )?(.+, .+)$`)
)

// resolveTraits returns what a trigger's value must be, as the Traits line
// of its dump entry says: a number, a date, yes or no, a name of a database,
// a link to a scope, or one of a list of values. It returns false for a line
// it does not know, and for a class of keys no database of the game fits.
func (b *builder) resolveTraits(traits string) (lang.Expected, bool) {
	comparison, class := comparisonTraits.FindStringSubmatch(traits), classTraits.FindStringSubmatch(traits)
	target, tag := targetTraits.FindStringSubmatch(traits), tagTraits.FindStringSubmatch(traits)
	list := choiceTraits.FindStringSubmatch(traits)
	switch {
	case comparison != nil && comparison[1] != "":
		return lang.Expected{Type: lang.ValueTypeDate}, true

	case comparison != nil:
		return lang.Expected{Type: lang.ValueTypeNumber, Values: true}, true

	case traits == "yes/no":
		return lang.Expected{Type: lang.ValueTypeBool}, true

	case class != nil:
		database, ok := b.FindDatabaseByClass(class[1])
		if !ok {
			return lang.Expected{}, false
		}

		return lang.Expected{Type: lang.ValueTypeReference, Database: database.Name}, true

	case target != nil:
		return lang.Expected{Type: lang.ValueTypeTarget, Scope: target[1]}, true

	case tag != nil:
		database, _ := b.ResolveTypeDatabase(tag[1])

		return lang.Expected{Type: lang.ValueTypeTarget, Scope: tag[1], Database: database}, true

	case list != nil:
		choices := splitList(list[1])
		choices[len(choices)-1] = strings.TrimPrefix(choices[len(choices)-1], "and ")

		return lang.Expected{Type: lang.ValueTypeChoice, Choices: choices}, true
	}

	return lang.Expected{}, false
}

// makeField returns a definition's field, made with the given source if it
// has none.
func makeField(definition *lang.Definition, source lang.Source) *lang.Field {
	if definition.Field == nil {
		definition.Field = &lang.Field{Source: source}
	}

	return definition.Field
}

// resolveTitleValue gives a command what the value written right after its
// name in the dump says it takes, as add_trait = <trait_key> does. What a
// Traits line says comes first.
func (b *builder) resolveTitleValue(command *lang.Definition, dump DumpCommand) {
	hasKeys := command.Field != nil && len(command.Field.Children) > 0
	for line := range strings.SplitSeq(usageComment.ReplaceAllString(dump.Description, ""), "\n") {
		tokens := usageToken.FindAllString(line, -1)
		if len(tokens) < 3 || tokens[0] != dump.Name || !slices.Contains(usageOperators, tokens[1]) || tokens[2] == "{" {
			continue
		}

		value := &lang.Field{}
		b.parseUsageValue(tokens, 2, value)

		// a command with keys of its own, as change_first_name =
		// { template_character = … }, holds its arguments in its block. What
		// the placeholder says the block holds then gives only the database
		// a plain value names.
		if hasKeys && value.Shape.IsStated() {
			database := lang.ShapeRows[value.Shape.Value].Values
			value.Shape, value.Value = lang.Claim[lang.Shape]{}, lang.Claim[lang.Expected]{}
			if database != "" {
				value.Value.Resolve(lang.Expected{Type: lang.ValueTypeReference, Database: database}, lang.SourceDumps)
			}
		}

		if !value.Value.IsStated() {
			continue
		}

		makeField(command, lang.SourceDumps).Take(value.Claims)

		return
	}
}

// buildPrefixes gives each link that takes a name after a colon the database
// of those names, as scope: takes a saved scope and var: a variable. A
// database's declaration lists the links that take its names. A link the
// dumps do not list is added.
func (b *builder) buildPrefixes() {
	links := b.Databases[lang.DumpLinks]
	claim := func(prefix, database string) {
		link := b.LookupLink(prefix, true)
		if link == nil {
			link = &lang.Definition{Database: links.Name, Key: prefix + ":", Prefixed: true}
			links.Definitions[link.Key] = link
		}

		expected := lang.Expected{Type: lang.ValueTypeReference, Database: database}
		makeField(link, lang.SourceRegistry).Value.Resolve(expected, lang.SourceRegistry)
	}

	for _, saved := range []string{lang.SavedScopes, lang.Variables, lang.LocalVariables, lang.GlobalVariables} {
		claim(saved, saved)
	}

	for _, label := range slices.Sorted(maps.Keys(b.Databases)) {
		for _, prefix := range b.Databases[label].Declaration.Prefixes {
			claim(prefix, label)
		}
	}
}

// commandKeys are the commands, and the keys of commands' blocks, that take
// a name of a database the dump's example of them does not give. Each row
// quotes the dump text that says so.
var commandKeys = []commandKey{
	// CK3 effects.log, stress_impact: "trait = value # add this stress if the
	// character has this trait"
	{Command: "stress_impact", Database: "traits"}, {Command: "stress_and_fulfillment_impact", Database: "traits"},
	// CK3 effects.log:349: "random_traits_list = { count = { 1 5 }
	// traitID = { some triggers } … }"
	{Games: []string{game.CK3}, Command: "create_character/random_traits_list", Database: "traits"},
	// CK3 effects.log, custom_tooltip and custom_label: "just a tooltip …
	// Can also be written as custom_tooltip = { text = key … }"
	{Command: "custom_tooltip", Value: lang.Localization}, {Command: "custom_tooltip/text", Value: lang.Localization},
	{Command: "custom_label", Value: lang.Localization}, {Command: "custom_label/text", Value: lang.Localization},
	// CK3: ActivityLogEntry.GetTitle shows <key>_title, which every key the
	// game's files log defines; GetDescription is <key> itself where written
	{Command: "add_activity_log_entry/key", Value: lang.Localization, Stem: "%s_title"},
	// CK3 triggers.log:4766, "Does the character have this trait or a trait
	// of this trait group?": the dump writes no usage line
	{Games: []string{game.CK3}, Command: "has_trait", Value: "traits"},
	// triggers.log, "has_game_rule = faster_conversion": the usage line
	// writes one setting's name
	{Command: lang.GameRuleTrigger, Value: lang.GameRuleSettings},
}

// buildCommandKeys applies the rows of commandKeys that hold for the game.
// A row says that the keys of a command's block, or its value, are names of
// a database. A row for a command the dumps do not list does nothing.
func (b *builder) buildCommandKeys(dialect *lang.Dialect) {
	for _, entry := range commandKeys {
		if !dialect.Matches(entry.Games) {
			continue
		}

		title, key, hasKey := strings.Cut(entry.Command, "/")
		for _, database := range lang.CommandDatabases {
			command := b.Databases[database].Definitions[title]
			if command == nil {
				continue
			}

			field := makeField(command, lang.SourceRegistry)
			if hasKey {
				field = field.BuildChild(key, lang.SourceRegistry)
			}

			if entry.Database != "" {
				field.Keys.Resolve(entry.Database, lang.SourceRegistry)
			}

			if entry.Value != "" {
				expected := lang.Expected{Type: lang.ValueTypeReference, Database: entry.Value, Stem: entry.Stem}
				field.Value = lang.Claim[lang.Expected]{Value: expected, Source: lang.SourceDumps}
			}
		}
	}
}

// buildKeywordShapes gives a command the keys that keywords written inside
// its block add to it, with what each one's block holds. A random_list gets
// its numbered branches this way.
func (b *builder) buildKeywordShapes(dialect *lang.Dialect) {
	for _, keyword := range lang.Keywords {
		command := b.FindCommand(keyword.Parent)
		if command == nil || keyword.Shape == "" || keyword.Shape == lang.ShapeEnclosing || !dialect.Matches(keyword.Games) {
			continue
		}

		argument := makeField(command, lang.SourceDumps).BuildChild(keyword.Key, lang.SourceNone)
		argument.Shape.Resolve(keyword.Shape, lang.SourceRegistry)
	}
}

// buildFamilies gives the commands that go through one list, as any_vassal,
// every_vassal, random_vassal and ordered_vassal, the keys of all of them.
func (b *builder) buildFamilies() {
	families := map[string][]*lang.Field{}
	for _, database := range lang.CommandDatabases {
		for _, name := range slices.Sorted(maps.Keys(b.Databases[database].Definitions)) {
			iterator, ok := lang.MatchListIterator(name)
			if arguments := b.Databases[database].Definitions[name].Field; ok && arguments != nil {
				base := strings.TrimPrefix(name, strings.TrimSuffix(iterator.Key, "*"))
				families[base] = append(families[base], arguments)
			}
		}
	}

	for _, members := range families {
		if len(members) < 2 {
			continue
		}

		united := unite(members)
		for _, member := range members {
			member.Children = united.Children
		}
	}
}

// buildUnions gives each name that is both an effect and a trigger one more
// set of keys, those of both together, for a block that may hold either.
// Where the two say different things about a key, the effect's is kept. The
// names that already share one set of keys are left out.
func (b *builder) buildUnions() {
	for key, effect := range b.Databases[lang.DumpEffects].Definitions {
		trigger := b.Databases[lang.DumpTriggers].Definitions[key]
		if trigger == nil || slices.Contains(sharedWords, key) {
			continue
		}

		union := uniteCommands(effect, trigger)
		effect.Union, trigger.Union = union, union
	}
}

// uniteCommands returns the keys of an effect and of a trigger of one name
// as one field. A key keeps what its block is said to hold only where the
// two agree, or where the block may hold effects and triggers alike.
func uniteCommands(effect, trigger *lang.Definition) *lang.Field {
	fields := slices.DeleteFunc([]*lang.Field{effect.Field, trigger.Field}, func(field *lang.Field) bool {
		return field == nil
	})
	united := unite(fields)
	keepSharedShapes(united, fields)

	return united
}

// unite returns one field that holds everything the given fields hold: what
// each says it takes, the first to say it winning, and every key of each,
// keys of one name being united in the same way.
func unite(fields []*lang.Field) *lang.Field {
	united := &lang.Field{}
	members := map[string][]*lang.Field{}
	for _, field := range fields {
		if field.Source != lang.SourceNone && (united.Source == lang.SourceNone || field.Source < united.Source) {
			united.Source = field.Source
		}

		united.Take(field.Claims)
		united.Keys.Take(field.Keys)
		addVanilla(&united.Vanilla, field.Vanilla)

		for key, child := range field.Children {
			members[key] = append(members[key], child)
		}
	}

	for key, children := range members {
		if united.Children == nil {
			united.Children = map[string]*lang.Field{}
		}

		united.Children[key] = unite(children)
	}

	return united
}

// keepSharedShapes removes what a united field's block is said to hold,
// unless every field it was made from says the same, or the block may hold
// effects and triggers alike. It does the same for each key inside.
func keepSharedShapes(united *lang.Field, fields []*lang.Field) {
	takes := lang.ShapeRows[united.Shape.Value].Takes
	takesBoth := slices.Contains(takes, lang.DumpEffects) && slices.Contains(takes, lang.DumpTriggers)
	if slices.ContainsFunc(fields, func(field *lang.Field) bool {
		return field.Shape.Value != united.Shape.Value && (field.Shape.IsStated() || !takesBoth)
	}) {
		united.Shape = lang.Claim[lang.Shape]{}
	}

	for key, child := range united.Children {
		var children []*lang.Field
		for _, field := range fields {
			if field.Children[key] != nil {
				children = append(children, field.Children[key])
			}
		}

		keepSharedShapes(child, children)
	}
}
