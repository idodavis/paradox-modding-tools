package schema

import (
	"cmp"
	"errors"
	"io/fs"
	"iter"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/jomini"
)

// A DumpCommand is one effect, trigger or link as its dump lists it. The usage lines in its description become
// the command's arguments.
type DumpCommand struct {
	Name        string
	Declared    string // game.DeclaredEffect, DeclaredTrigger, DeclaredLink
	Scopes      []string
	Target      string
	Description string
	Prefixed    bool   // the dump says it requires data: the link is written with a name after a colon
	Wild        bool   // the dump says it is a wild card
	Global      bool   // the dump says it is a global link
	Class       string // the database class a trigger's traits line names; "" if none
}

// Dumps is everything a game's script_docs and data types dumps declare, each by its name but the commands, which
// several dumps may list under one name.
type Dumps struct {
	Commands  []DumpCommand
	Scopes    map[string]ScopeType
	Saved     map[string]bool
	Modifiers map[string]Modifier
	OnActions map[string]OnAction
	CustomLoc map[string]CustomLoc
	DataTypes map[string]DataType
	WrittenAt time.Time // the oldest dump file's time
}

// yes is the term the dumps write for true.
const yes = "yes"

// savedTitle is the title of the list that ends the event targets dump: the targets the game saves from code.
const savedTitle = "Event Targets Saved from Code:"

// dumpFiles lists the dump files a game writes. The data types come in five files.
var dumpFiles = []string{
	"effects.log", "triggers.log", "event_targets.log", "modifiers.log", "on_actions.log", "custom_localization.log",
	"event_scopes.log", "data_types_common.txt", "data_types_gui.txt", "data_types_internalclausewitzgui.txt",
	"data_types_script.txt", "data_types_uncategorized.txt",
}

// The shapes of the lines a dump is read by: the line of dashes or the markdown heading that starts an entry, a
// scope line plain or in bold, a usage line, a `Name:` title, any `Key: value` pair, a modifier's tag line, a
// trigger's traits line and a data type's `Name( Arg0, Arg1 )`.
var (
	dashesLine = regexp.MustCompile(`^-{10,}$`)
	headingRow = regexp.MustCompile(`^#{2,3} +(\S+)$`)
	scopeLine  = regexp.MustCompile(`^\*{0,2}(Supported Scopes|Supported Targets|Input Scopes|Output Scopes)\*{0,2}:\s*(.*)$`)
	usageLine  = regexp.MustCompile(`^\s*\S+\s*=\s*\{`)
	titleLine  = regexp.MustCompile(`^([A-Za-z0-9_$]+):$`)
	pairLine   = regexp.MustCompile(`^\s*([A-Za-z ]+?):\s*(.*)$`)
	tagLine    = regexp.MustCompile(`^Tag:\s*([^,\s]+)(?:,\s*Categories:\s*(.*))?$`)
	traitsLine = regexp.MustCompile(`^Traits:\s*class (C\w+) key$`)
	signature  = regexp.MustCompile(`^(\S+?)\s*(?:\(\s*(.*?)\s*\))?$`)
)

// ReadDumps reads every dump file under the given folders. A missing file is simply left out, so the caller can
// fill it from the shipped backup. It returns an error only for a file that exists and cannot be read.
func ReadDumps(dirs []string) (*Dumps, error) {
	dumps := &Dumps{
		Scopes: map[string]ScopeType{}, Saved: map[string]bool{}, Modifiers: map[string]Modifier{},
		OnActions: map[string]OnAction{}, CustomLoc: map[string]CustomLoc{}, DataTypes: map[string]DataType{},
	}
	for _, file := range dumpFiles {
		text, writtenAt, err := loadDump(dirs, file)
		if err != nil {
			return nil, err
		}
		if writtenAt.IsZero() {
			continue
		}
		dumps.parseFile(file, text)
		if dumps.WrittenAt.IsZero() || writtenAt.Before(dumps.WrittenAt) {
			dumps.WrittenAt = writtenAt
		}
	}
	return dumps, nil
}

// loadDump loads the first file of the given base name that one of the folders holds, with the time it was
// written. The time is zero when no folder holds the file.
func loadDump(dirs []string, file string) (string, time.Time, error) {
	for _, dir := range dirs {
		path := filepath.Join(dir, file)
		stat, err := os.Stat(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", time.Time{}, err
		}
		text, err := files.ReadText(path)
		return text.Text, stat.ModTime(), err
	}
	return "", time.Time{}, nil
}

// parseFile parses the text of one dump file into the dumps, by the file's base name.
func (dumps *Dumps) parseFile(file, text string) {
	switch file {
	case "effects.log":
		dumps.parseCommands(text, game.DeclaredEffect)
	case "triggers.log":
		dumps.parseCommands(text, game.DeclaredTrigger)
	case "event_targets.log":
		dumps.parseCommands(text, game.DeclaredLink)
	case "modifiers.log":
		dumps.parseModifiers(text)
	case "on_actions.log":
		for title, scope := range parseScoped(text, "Expected Scope") {
			dumps.OnActions[title] = OnAction{Name: title, Scope: scope}
		}
	case "custom_localization.log":
		for title, scope := range parseScoped(text, "Scope") {
			dumps.CustomLoc[title] = CustomLoc{Name: title, Scope: scope}
		}
	case "event_scopes.log":
		dumps.parseScopes(text)
	default:
		dumps.parseDataTypes(text)
	}
}

// parseSections yields the entries of a dump, each its first line and the lines after it; a markdown dump writes
// only the title of the entry on its first line. A line of dashes or a markdown heading starts an entry. A dump that
// opens with a title has it left out; one that has none starts with its first entry.
func parseSections(text string, titled bool) iter.Seq2[string, []string] {
	return func(yield func(string, []string) bool) {
		head, body, open := "", []string(nil), !titled
		for line := range strings.Lines(text) {
			line = strings.TrimRight(line, " \t\r\n")
			heading := headingRow.FindStringSubmatch(line)
			switch {
			case dashesLine.MatchString(line) || heading != nil:
				if head != "" && !yield(head, body) {
					return
				}
				head, body, open = "", nil, true
				if heading != nil {
					head = heading[1]
				}
			case !open:
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

// parseCommands parses the effects, triggers or links of one script_docs dump, in the order the dump lists them.
// The event targets dump also ends with the list of the targets the game saves from code, which are names.
func (dumps *Dumps) parseCommands(text, declaration string) {
	for head, body := range parseSections(text, true) {
		if head == savedTitle {
			for _, line := range body {
				if saved := strings.TrimSpace(line); saved != "" {
					dumps.Saved[saved] = true
				}
			}
			continue
		}
		title, first, _ := strings.Cut(head, " - ")
		command := DumpCommand{Name: strings.TrimSpace(title), Declared: declaration}
		lines := []string{first}
		for _, line := range body {
			scope := scopeLine.FindStringSubmatch(line)
			traits := traitsLine.FindStringSubmatch(line)
			switch trimmed := strings.TrimSpace(line); {
			case trimmed == "Requires Data: yes":
				command.Prefixed = true
			case trimmed == "Global Link: yes":
				command.Global = true
			case trimmed == "Wild Card: yes":
				command.Wild = true
			case traits != nil:
				command.Class = traits[1]
			case scope == nil:
				lines = append(lines, line)
			case scope[1] == "Supported Scopes" || scope[1] == "Input Scopes":
				command.Scopes = splitScopes(scope[2])
			default:
				// a command leads to one scope; a line naming several leads to none in particular
				if targets := splitScopes(scope[2]); len(targets) == 1 {
					command.Target = targets[0]
				}
			}
		}
		command.Description = strings.TrimSpace(strings.Join(lines, "\n"))
		dumps.Commands = append(dumps.Commands, command)
	}
}

// splitList splits a comma list into its trimmed, non-empty parts.
func splitList(text string) []string {
	var parts []string
	for part := range strings.SplitSeq(text, ",") {
		if part = strings.TrimSpace(part); part != "" {
			parts = append(parts, part)
		}
	}
	return parts
}

// splitScopes splits a comma list of scope names. A name the dumps use for no scope is left out.
func splitScopes(text string) []string {
	return slices.DeleteFunc(splitList(text), func(scope string) bool {
		return slices.Contains(game.DumpPhrases.NoScope, scope)
	})
}

// parsePairs returns the `Key: value` lines of an entry's body by key. A later line of the same key wins.
func parsePairs(body []string) map[string]string {
	pairs := map[string]string{}
	for _, line := range body {
		if pair := pairLine.FindStringSubmatch(line); pair != nil {
			pairs[pair[1]] = strings.TrimSpace(pair[2])
		}
	}
	return pairs
}

// parseScoped parses a dump whose entries are a title and the scope it is used in, written under the given key: the
// on_actions and the custom localization. It returns the scope by title.
func parseScoped(text, key string) map[string]string {
	scopes := map[string]string{}
	for head, body := range parseSections(text, true) {
		if title, ok := strings.CutSuffix(head, ":"); ok {
			scopes[title] = strings.Join(splitScopes(parsePairs(body)[key]), "")
		}
	}
	return scopes
}

// parseScopes parses the scope types of the event scopes dump: a title, then what script can do in the scope. The
// save token the dump also writes is the tag a save game writes an object reference with, which no script writes,
// so it is not read.
func (dumps *Dumps) parseScopes(text string) {
	var lines []string
	flush := func() {
		if len(lines) == 0 {
			return
		}
		title, _ := strings.CutSuffix(lines[0], ":")
		pairs := parsePairs(lines[1:])
		dumps.Scopes[title] = ScopeType{
			Name: title, Variables: pairs["Stores Variables"] == yes,
			Triggers: pairs["Evaluate Triggers"] == yes, Effects: pairs["Execute Effects"] == yes,
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

// parseModifiers parses the modifiers dump in the three forms the games write it: a `Tag:` line with a `Use areas:`
// line below it, a `Tag:` line with its categories, or a title with its indented description.
func (dumps *Dumps) parseModifiers(text string) {
	last := ""
	for line := range strings.Lines(text) {
		line = strings.TrimRight(line, " \t\r\n")
		trimmed := strings.TrimSpace(line)
		tag := tagLine.FindStringSubmatch(line)
		switch {
		case tag != nil:
			last = tag[1]
			dumps.Modifiers[last] = Modifier{Name: last, Blurb: strings.Join(splitList(tag[2]), ", ")}
		case titleLine.MatchString(line):
			last = strings.TrimSuffix(line, ":")
			dumps.Modifiers[last] = Modifier{Name: last}
		case last == "":
		case strings.HasPrefix(trimmed, "Use areas:"):
			dumps.Modifiers[last] = Modifier{Name: last, Blurb: strings.TrimSpace(strings.TrimPrefix(trimmed, "Use areas:"))}
		case strings.HasPrefix(trimmed, "Description:"):
			dumps.Modifiers[last] = Modifier{Name: last, Blurb: strings.TrimSpace(strings.TrimPrefix(trimmed, "Description:"))}
		}
	}
}

// parseDataTypes parses the types and functions of one data types dump. A function written on a type belongs to
// it, and a global one belongs to game.GlobalType. A macro keeps the text it stands for. A function written for
// several arities keeps its first. The dump opens with its first entry, not a title.
func (dumps *Dumps) parseDataTypes(text string) {
	for head, body := range parseSections(text, false) {
		parts := signature.FindStringSubmatch(head)
		pairs := parsePairs(body)
		if parts == nil {
			continue
		}
		definition := pairs["Definition type"]
		owner, label := game.GlobalType, parts[1]
		if !strings.HasPrefix(definition, "Global ") {
			owner, label, _ = strings.Cut(parts[1], ".")
		}
		dataType := dumps.DataTypes[owner]
		if dataType.Functions == nil {
			dataType = DataType{Name: owner, Functions: map[string]DataFunction{}}
		}
		if _, exists := dataType.Functions[label]; definition != "Type" && label != "" && !exists {
			dataType.Functions[label] = DataFunction{
				Name: label, Returns: pairs["Return type"], Arguments: splitList(parts[2]), Blurb: pairs["Description"],
				Expansion: pairs["Macro replacement"],
			}
		}
		dumps.DataTypes[owner] = dataType
	}
}

// buildDumps reads the dumps of the install, takes what they lack from the shipped backup, and builds what they
// declare. The state of the dumps is recorded as the state of their source.
func (schema *Schema) buildDumps(row *game.Game, installPath string) error {
	dumps, err := ReadDumps(game.FindDumpDirs(row.ID, installPath))
	if err != nil {
		return err
	}
	state := SourceState{Origin: originNone, WrittenAt: dumps.WrittenAt}
	backup, hasBackup := LoadShipped(row.ID)
	switch {
	case !dumps.WrittenAt.IsZero():
		state.Origin = originInstall
		state.Stale = dumps.WrittenAt.Before(game.ReadInstallUpdatedAt(installPath))
	case hasBackup && dumps.resolveMissing(backup):
		state.Origin = originShipped
	}
	if hasBackup && state.Origin == originInstall {
		dumps.resolveMissing(backup)
	}
	schema.Sources[game.SourceDumps] = state
	schema.buildCommands(dumps.Commands)
	schema.buildKeywordShapes()
	schema.Scopes, schema.Saved, schema.Modifiers = dumps.Scopes, dumps.Saved, dumps.Modifiers
	schema.OnActions, schema.CustomLoc, schema.DataTypes = dumps.OnActions, dumps.CustomLoc, dumps.DataTypes
	return nil
}

// resolveMissing takes each section the dumps lack from the backup, and reports whether it took any. The commands
// are three sections, one for each file they come from, and the links bring the targets saved from code.
func (dumps *Dumps) resolveMissing(backup *Dumps) bool {
	missing := false
	for _, declaration := range []string{game.DeclaredEffect, game.DeclaredTrigger, game.DeclaredLink} {
		declares := func(command DumpCommand) bool { return command.Declared == declaration }
		if slices.ContainsFunc(dumps.Commands, declares) {
			continue
		}
		dumps.Commands = slices.AppendSeq(dumps.Commands, func(yield func(DumpCommand) bool) {
			for _, command := range backup.Commands {
				if declares(command) && !yield(command) {
					return
				}
			}
		})
		if declaration == game.DeclaredLink {
			dumps.Saved = backup.Saved
		}
		missing = true
	}
	tables := []bool{
		takeMissing(&dumps.Scopes, backup.Scopes), takeMissing(&dumps.Modifiers, backup.Modifiers),
		takeMissing(&dumps.OnActions, backup.OnActions), takeMissing(&dumps.CustomLoc, backup.CustomLoc),
		takeMissing(&dumps.DataTypes, backup.DataTypes),
	}
	return missing || slices.Contains(tables, true)
}

// takeMissing gives the dumps the backup's table where they have none of it, and reports whether it did.
func takeMissing[T any](table *map[string]T, backup map[string]T) bool {
	if len(*table) > 0 {
		return false
	}
	*table = backup
	return true
}

// buildCommands builds the commands the dumps declare. A name that several dumps list is one command. A link that
// takes data after a colon is a prefix, and a trigger that takes the key of a database says which.
func (schema *Schema) buildCommands(dumps []DumpCommand) {
	for _, dump := range dumps {
		command := schema.Commands[dump.Name]
		if command == nil {
			command = &Command{}
			schema.Commands[dump.Name] = command
		}
		command.build(dump)
		if dump.Declared != game.DeclaredLink {
			schema.buildClass(command, dump.Class)
		} else if _, reserved := schema.Prefixes[dump.Name]; !reserved && dump.Prefixed {
			schema.Prefixes[dump.Name] = ""
		}
	}
}

// build adds what one dump says about a command. The scopes of a command are the union of its dumps' scopes, and a
// dump that names none makes it run in any. The first target, blurb and arguments written are kept, and a link is
// global if any of its dumps says so.
func (command *Command) build(dump DumpCommand) {
	switch {
	case len(command.Declared) == 0:
		command.Scopes = slices.Clone(dump.Scopes)
	case len(dump.Scopes) == 0 || len(command.Scopes) == 0:
		command.Scopes = nil
	default:
		command.Scopes = slices.Compact(slices.Sorted(slices.Values(slices.Concat(command.Scopes, dump.Scopes))))
	}
	command.Declared = append(command.Declared, dump.Declared)
	command.Target = cmp.Or(command.Target, dump.Target)
	blurb, usage := cutUsage(dump.Description)
	command.Blurb = cmp.Or(command.Blurb, blurb)
	command.Usage = cmp.Or(command.Usage, usage)
	if command.Arguments == nil {
		command.Arguments = parseArguments(dump.Name, usage)
	}
	if dump.Declared == game.DeclaredLink {
		command.Global = command.Global || dump.Global
		command.Wild = dump.Wild
	}
	command.Lists = command.Lists || (strings.HasPrefix(blurb, game.DumpPhrases.Iterates) && command.Target == "")
}

// cutUsage splits a dump description into its prose and its usage, the lines written as `<scope> = { … }`. The
// usage becomes a command's arguments and never appears in a blurb.
func cutUsage(description string) (blurb string, usage string) {
	lines := strings.Split(description, "\n")
	cut := slices.IndexFunc(lines, usageLine.MatchString)
	if cut < 0 {
		cut = len(lines)
	} else {
		usage = strings.Join(lines[cut:], "\n")
	}
	for _, line := range lines[:cut] {
		if blurb = strings.TrimSpace(line); blurb != "" {
			break
		}
	}
	return blurb, usage
}

// parseArguments returns the keys a command takes, as fields, from the usage its dump gives: the keys written
// inside every block keyed by the command's name. It returns nil if the usage has none.
func parseArguments(title, usage string) *Field {
	tree := jomini.Parse(usage)
	var field *Field
	for statement := range tree.Children(0) {
		if tree.Text(tree.Statements[statement].Key) == title && tree.Statements[statement].IsBlock() {
			if field == nil {
				field = &Field{Source: game.SourceDumps}
			}
			buildArguments(tree, statement, field)
		}
	}
	return field
}

// buildArguments builds into a field the keys a usage block writes: a child for each key that is an identifier,
// with the keys of its own block, and the shape that a placeholder such as `<triggers>` inside a block names.
func buildArguments(tree *jomini.Tree, statement int32, field *Field) {
	for child := range tree.Children(statement) {
		inner := tree.Statements[child]
		token := inner.Key
		if inner.IsListed() {
			token = inner.Value
		}
		key := tree.Text(token)
		switch {
		case token.Start > 0 && tree.Src[token.Start-1] == '<':
			if shape, ok := findPlaceholderShape(key); ok {
				field.Shape.resolve(shape, game.SourceDumps)
			}
		case jomini.IsIdentifier(key) && !inner.IsListed():
			argument := field.buildChild(key)
			argument.Source = game.SourceDumps
			if inner.IsBlock() {
				buildArguments(tree, child, argument)
			}
		}
	}
}

// findPlaceholderShape returns the shape a placeholder names, such as `<triggers>`, `<effect>` or
// `<display_triggers>`. It returns false for a placeholder that names none.
func findPlaceholderShape(placeholder string) (string, bool) {
	shape := strings.TrimSuffix(placeholder, ">")
	shape = shape[strings.LastIndex(shape, "_")+1:]
	if !strings.HasSuffix(shape, "s") {
		shape += "s"
	}
	_, ok := game.Shapes[shape]
	return shape, ok
}

// buildClass gives a command the arguments of a database's key where its dump names the class that holds the key.
func (schema *Schema) buildClass(command *Command, class string) {
	database, ok := schema.FindDatabaseByClass(class)
	if class == "" || !ok {
		return
	}
	command.makeArguments(game.SourceDumps).Value.resolve(Expected{Type: ValueReference, Database: database.Name}, game.SourceDumps)
}

// makeArguments returns the field of the keys a command takes, making it with the given source where the command has
// none yet.
func (command *Command) makeArguments(source game.Source) *Field {
	if command.Arguments == nil {
		command.Arguments = &Field{Source: source}
	}
	return command.Arguments
}

// buildKeywordShapes gives a command's arguments the child that each keyword row written under the command names,
// such as random_list's numbered branches, with the shape the row gives its block, so the count learns the keys
// written in it.
func (schema *Schema) buildKeywordShapes() {
	for _, row := range game.Keywords {
		title, key, held := strings.Cut(row.Key, "/")
		command := schema.Commands[title]
		if !held || command == nil || row.Shape == "" || row.Shape == "enclosing" {
			continue
		}
		command.makeArguments(game.SourceDumps).buildChild(key).Shape.resolve(row.Shape, game.SourceRegistry)
	}
}
