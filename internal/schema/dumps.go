// dumps.go reads the dump files a game writes of its own script language, and builds the schema's commands from them.

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
)

// A DumpCommand is one effect, trigger or link as its dump lists it.
type DumpCommand struct {
	Name        string
	Declared    string // game.DeclaredEffect, DeclaredTrigger, DeclaredLink
	Scopes      []string
	Target      string
	Description string
	Prefixed    bool   // the dump says it requires data: the link is written with a name after a colon
	Wild        bool   // the dump says it is a wild card
	Traits      string // what a trigger's Traits line writes after its colon; "" if none
}

// Dumps is everything that a game's script_docs and data types dumps declare.
type Dumps struct {
	Commands  []DumpCommand // a list, since several dumps may list one name
	Scopes    map[string]ScopeType
	Saved     map[string]bool
	Modifiers map[string]Modifier
	OnActions map[string]OnAction
	DataTypes map[string]DataType
	Customs   map[string]bool // the customizable localizations the game has, by name
	WrittenAt time.Time       // the newest dump file's time
	OldestAt  time.Time       // the oldest dump file's time
	Folders   []string        // the folders the dump files were read from, each once
}

// yes is the term the dumps write for true.
const yes = "yes"

// savedTitle is the title of the list of targets saved from code at the end of the event targets dump.
const savedTitle = "Event Targets Saved from Code:"

// dumpFiles lists the dump files read.
var dumpFiles = []string{
	"effects.log", "triggers.log", "event_targets.log", "modifiers.log", "on_actions.log", "event_scopes.log",
	"custom_localization.log", "data_types_common.txt", "data_types_gui.txt", "data_types_internalclausewitzgui.txt",
	"data_types_script.txt", "data_types_uncategorized.txt",
}

// The patterns of the lines a dump is read by.
var (
	dashesLine = regexp.MustCompile(`^-{10,}$`)
	headingRow = regexp.MustCompile(`^#{2,3} +(\S+)$`)
	scopeLine  = regexp.MustCompile(`^\*{0,2}(Supported Scopes|Supported Targets|Input Scopes|Output Scopes)\*{0,2}:\s*(.*)$`)
	usageLine  = regexp.MustCompile(`^\s*\S+\s*=\s*\{`)
	titleLine  = regexp.MustCompile(`^([A-Za-z0-9_$]+):$`)
	pairLine   = regexp.MustCompile(`^\s*([A-Za-z ]+?):\s*(.*)$`)
	tagLine    = regexp.MustCompile(`^Tag:\s*([^,\s]+)(?:,\s*Categories:\s*(.*))?$`)
	signature  = regexp.MustCompile(`^(\S+?)\s*(?:\(\s*(.*?)\s*\))?$`)
	// usageToken is one token of a usage: a brace, an operator of two characters, a group in angle or square brackets,
	// an operator of one, or a run of other characters but spaces.
	usageToken = regexp.MustCompile(`[{}]|[<>!=?]=|<[^<>\n]*>|\[[^\[\]\n]*\]|[=<>]|[^\s{}=<>\[\]]+`)
	// usageComment is a comment in a usage, as CK3's stress_impact writes "# multiple 'trait = value' lines".
	usageComment = regexp.MustCompile(`#[^\n]*`)
	// describedLine is a line that closes a block and then describes it after a dash, as CK3's
	// "random_traits_list = { count = { 1 5 } … } - A number of traits …" does.
	describedLine = regexp.MustCompile(`\}\s+-\s+\S`)
)

// The forms of a trigger's Traits line.
var (
	comparisonTraits = regexp.MustCompile(`^<, <=, =, (?:!=, )?>, >=( valid date)?$`)
	classTraits      = regexp.MustCompile(`^class (C\w+) key$`)
	targetTraits     = regexp.MustCompile(`^(\w+) (?:scope|target)(?: \w+ scope)?$`)
	tagTraits        = regexp.MustCompile(`^\w+ (?:tag|id)/(\w+) scope$`)
	choiceTraits     = regexp.MustCompile(`^(?:Valid [\w ]+: )?(.+, .+)$`)
)

// usageOperators are the operators a usage writes between a key and its value.
var usageOperators = []string{"=", "==", "!=", "<", ">", "<=", ">=", "?="}

// FindDumpsAt returns when the dumps under the given folders were last written, which is the time of the newest dump
// file. It reads no file. The time is zero if no folder holds a dump. It returns an error if a dump file's time cannot
// be read.
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

// ReadDumps reads the newest copy of each dump file under the given folders, and records the folders it read from. A
// missing file is left out, and so is one that cannot be read as text, such as one over files.TextLimit or in UTF-16,
// so the caller can fill it from the shipped backup. It returns an error only where a file's time cannot be read.
func ReadDumps(dirs []string) (*Dumps, error) {
	dumpsAt, err := FindDumpsAt(dirs)
	if err != nil {
		return nil, err
	}
	dumps := &Dumps{
		Scopes: map[string]ScopeType{}, Saved: map[string]bool{}, Modifiers: map[string]Modifier{},
		OnActions: map[string]OnAction{}, DataTypes: map[string]DataType{}, Customs: map[string]bool{}, WrittenAt: dumpsAt,
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
		dumps.parseFile(file, text.Text)
		if dir := filepath.Dir(path); !slices.Contains(dumps.Folders, dir) {
			dumps.Folders = append(dumps.Folders, dir)
		}
	}
	return dumps, nil
}

// findDump returns the path and time of the newest file with the given base name in the folders, "" and zero if there
// is none.
func findDump(dirs []string, file string) (string, time.Time, error) {
	newest, newestAt := "", time.Time{}
	for _, dir := range dirs {
		path := filepath.Join(dir, file)
		stat, err := os.Stat(path)
		switch {
		case errors.Is(err, fs.ErrNotExist): // the folder lacks this file
		case err != nil:
			return "", time.Time{}, err
		case stat.ModTime().After(newestAt):
			newest, newestAt = path, stat.ModTime()
		}
	}
	return newest, newestAt, nil
}

// parseFile parses the text of the dump file with the given base name into the dumps.
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
		maps.Copy(dumps.OnActions, parseOnActions(text))
	case "event_scopes.log":
		dumps.parseScopes(text)
	case "custom_localization.log":
		for head := range parseSections(text, true) {
			if name, ok := strings.CutSuffix(head, ":"); ok {
				dumps.Customs[name] = true
			}
		}
	default:
		dumps.parseDataTypes(text)
	}
}

// parseSections yields the entries of a dump, each as its head and the lines after it. A line of dashes or a markdown
// heading starts an entry. The head is the heading's text, or the first line after the dashes. If the flag is set, the
// dump opens with a title, and the lines before the first entry are skipped.
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
			case !open: // the dump's title
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

// parseCommands parses the effects, triggers or links of one script_docs dump in the order it lists them, and the
// targets saved from code that the event targets dump ends with.
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
				// several scopes give no single target
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

// splitScopes splits a comma list of scope names, leaving out the names the dumps use for no scope.
func splitScopes(text string) []string {
	return slices.DeleteFunc(splitList(text), func(scope string) bool {
		return slices.Contains(game.DumpPhrases.NoScope, scope)
	})
}

// parsePairs returns the "Key: value" lines of an entry's body by key. A later line with the same key wins.
func parsePairs(body []string) map[string]string {
	pairs := map[string]string{}
	for _, line := range body {
		if pair := pairLine.FindStringSubmatch(line); pair != nil {
			pairs[pair[1]] = strings.TrimSpace(pair[2])
		}
	}
	return pairs
}

// parseOnActions parses the on_actions dump. Each entry gives an on_action's name, the scope it runs in and whether the
// game calls it from its own code. It returns the on_actions by name.
func parseOnActions(text string) map[string]OnAction {
	actions := map[string]OnAction{}
	for head, body := range parseSections(text, true) {
		if name, ok := strings.CutSuffix(head, ":"); ok {
			pairs := parsePairs(body)
			actions[name] = OnAction{
				Name:      name,
				Scope:     strings.Join(splitScopes(pairs["Expected Scope"]), ""),
				Hardcoded: pairs["From Code"] == "Yes",
			}
		}
	}
	return actions
}

// parseScopes parses the scope types of the event scopes dump. Each is a title, then lines saying whether triggers and
// effects run in the scope. The dump's other lines, such as its save token, are not read.
func (dumps *Dumps) parseScopes(text string) {
	var lines []string
	flush := func() {
		if len(lines) == 0 {
			return
		}
		title, _ := strings.CutSuffix(lines[0], ":")
		pairs := parsePairs(lines[1:])
		dumps.Scopes[title] = ScopeType{
			Name: title, Triggers: pairs["Evaluate Triggers"] == yes, Effects: pairs["Execute Effects"] == yes,
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

// parseModifiers parses the modifiers dump, which the games write in three layouts: a "Tag:" line with a "Use areas:"
// line below it, a "Tag:" line with its categories, or a title with an indented description.
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
		case last == "": // a line before the first modifier
		case strings.HasPrefix(trimmed, "Use areas:") || strings.HasPrefix(trimmed, "Description:"):
			_, blurb, _ := strings.Cut(trimmed, ":")
			dumps.Modifiers[last] = Modifier{Name: last, Blurb: strings.TrimSpace(blurb)}
		}
	}
}

// parseDataTypes parses the types and functions of one data types dump. A function written on a type belongs to that
// type, and a global one belongs to game.GlobalType. A function in game.SelfFunctions returns its own type. A macro
// keeps its text. A function written for several arities keeps the first.
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
		returns := pairs["Return type"]
		if slices.Contains(game.SelfFunctions, label) && owner != game.GlobalType {
			returns = owner
		}
		if _, exists := dataType.Functions[label]; definition != "Type" && label != "" && !exists {
			dataType.Functions[label] = DataFunction{
				Name: label, Returns: returns, Arguments: splitList(parts[2]), Blurb: pairs["Description"],
				Expansion: pairs["Macro replacement"], Promote: strings.HasSuffix(strings.ToLower(definition), "promote"),
			}
		}
		dumps.DataTypes[owner] = dataType
	}
}

// buildDumps reads the dumps of the install and builds what they declare. What they lack comes from the shipped
// backup. It records where the dumps came from and whether they are stale.
func (schema *Schema) buildDumps(row *game.Game, installPath string) error {
	dumps, err := ReadDumps(row.FindDumpDirs(installPath))
	if err != nil {
		return err
	}
	backup, hasBackup := LoadShipped(row.ID)
	filled := hasBackup && dumps.resolveMissing(backup)
	dumps.applyCorrections(row)
	state := SourceState{Origin: originNone, WrittenAt: dumps.WrittenAt, Folders: dumps.Folders}
	switch {
	case !dumps.WrittenAt.IsZero():
		state.Origin = originInstall
		state.Stale = dumps.OldestAt.Before(game.ReadInstallUpdatedAt(installPath))
	case filled:
		state.Origin = originShipped
	}
	schema.Dumps = state
	schema.Scopes, schema.Saved, schema.Modifiers, schema.Customs = dumps.Scopes, dumps.Saved, dumps.Modifiers, dumps.Customs
	schema.OnActions, schema.DataTypes = dumps.OnActions, dumps.DataTypes
	schema.buildCommands(dumps.Commands)
	schema.buildCommandKeys(row)
	schema.buildKeywordShapes(row)
	schema.buildFamilies()
	return nil
}

// resolveMissing takes each section the dumps lack from the backup, and reports whether it took any. The effects,
// triggers and links are three sections, and the links bring the targets saved from code.
func (dumps *Dumps) resolveMissing(backup *Dumps) bool {
	missing := false
	for _, declaration := range []string{game.DeclaredEffect, game.DeclaredTrigger, game.DeclaredLink} {
		declares := func(command DumpCommand) bool { return command.Declared == declaration }
		if slices.ContainsFunc(dumps.Commands, declares) {
			continue
		}
		for _, command := range backup.Commands {
			if declares(command) {
				dumps.Commands = append(dumps.Commands, command)
			}
		}
		if declaration == game.DeclaredLink {
			dumps.Saved = backup.Saved
		}
		missing = true
	}
	tables := []bool{
		takeMissing(&dumps.Scopes, backup.Scopes), takeMissing(&dumps.Modifiers, backup.Modifiers),
		takeMissing(&dumps.OnActions, backup.OnActions), takeMissing(&dumps.DataTypes, backup.DataTypes),
		takeMissing(&dumps.Customs, backup.Customs),
	}
	return missing || slices.Contains(tables, true)
}

// applyCorrections sets the scopes and target of each command that one of the game's dump corrections names. The
// correction outranks the dump.
func (dumps *Dumps) applyCorrections(row *game.Game) {
	for position, command := range dumps.Commands {
		for _, correction := range game.DumpCorrections {
			if correction.Command == command.Name && row.Matches(correction.Games) {
				dumps.Commands[position].Scopes, dumps.Commands[position].Target = correction.Scopes, correction.Target
			}
		}
	}
}

// takeMissing replaces an empty table with the backup's, and reports whether it did.
func takeMissing[T any](table *map[string]T, backup map[string]T) bool {
	if len(*table) > 0 {
		return false
	}
	*table = backup
	return true
}

// buildCommands builds the commands the dumps declare. A name that several dumps list is one command, which takes the
// arguments each dump gives it where that dump declares it. A link that requires data is a prefix. A trigger whose
// Traits line says what its value is takes that as its value.
func (schema *Schema) buildCommands(dumps []DumpCommand) {
	for _, dump := range dumps {
		command := schema.Commands[dump.Name]
		if command == nil {
			command = &Command{Arguments: map[string]*Field{}}
			schema.Commands[dump.Name] = command
		}
		command.build(dump)
		if command.Arguments[dump.Declared] == nil {
			prose, usage := splitUsage(dump.Description)
			arguments, key := schema.parseArguments(dump.Name, prose, usage)
			if arguments != nil {
				command.Arguments[dump.Declared] = arguments
			}
			command.ArgumentKey = cmp.Or(command.ArgumentKey, key)
		}
		if expected, ok := schema.resolveTraits(dump.Traits); ok {
			command.makeArguments(dump.Declared, game.SourceDumps).Value.resolve(expected, game.SourceDumps)
		}
		schema.resolveTitleValue(command, dump)
		if _, reserved := schema.Prefixes[dump.Name]; dump.Declared == game.DeclaredLink && !reserved && dump.Prefixed {
			schema.Prefixes[dump.Name] = ""
		}
	}
}

// buildCommandKeys gives each command, or key of a command's block, that a game.CommandKeys row names the database
// its block's keys name, for the rows that hold for the given game. A row whose command the dumps do not declare is
// skipped.
func (schema *Schema) buildCommandKeys(row *game.Game) {
	for _, commandKey := range game.CommandKeys {
		title, key, nested := strings.Cut(commandKey.Command, "/")
		command := schema.Commands[title]
		if command == nil || !row.Matches(commandKey.Games) {
			continue
		}
		field := command.makeArguments("", game.SourceRegistry)
		if nested {
			field = field.buildChild(key, game.SourceRegistry)
		}
		field.Keys.resolve(commandKey.Database, game.SourceRegistry)
	}
}

// resolveTitleValue gives a command the value that a line of its dump writes after its name, as add_trait =
// <trait_key> does; a Traits line's value outranks it. Where the dump also writes the command's keys, a value that
// names a shape gives a name of that shape's values' database instead, or nothing.
func (schema *Schema) resolveTitleValue(command *Command, dump DumpCommand) {
	arguments := command.Arguments[dump.Declared]
	keyed := arguments != nil && len(arguments.Children) > 0
	for line := range strings.SplitSeq(usageComment.ReplaceAllString(dump.Description, ""), "\n") {
		tokens := usageToken.FindAllString(line, -1)
		if len(tokens) < 3 || tokens[0] != dump.Name || !slices.Contains(usageOperators, tokens[1]) || tokens[2] == "{" {
			continue
		}
		value := &Field{}
		schema.parseUsageValue(tokens, 2, value)
		// a block with keys, as change_first_name = { template_character = … }, holds the command's arguments
		if keyed && value.Shape.IsStated() {
			database := game.Shapes[value.Shape.Value].Values
			value.Shape, value.Value = Claim[string]{}, Claim[Expected]{}
			if database != "" {
				value.Value.resolve(Expected{Type: ValueReference, Database: database}, game.SourceDumps)
			}
		}
		if !value.Value.IsStated() {
			continue
		}
		command.makeArguments(dump.Declared, game.SourceDumps).take(value.Claims)
		return
	}
}

// build adds what one dump says about a command, but its arguments. The command's scopes are the union of its dumps',
// and none if any dump names none. The first target, blurb and usage are kept.
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
	prose, usage := splitUsage(dump.Description)
	blurb, _, _ := strings.Cut(strings.TrimSpace(prose), "\n")
	blurb = strings.TrimSpace(blurb)
	command.Blurb = cmp.Or(command.Blurb, blurb)
	command.Usage = cmp.Or(command.Usage, usage)
	if dump.Declared == game.DeclaredLink {
		command.Wild = dump.Wild
		command.Bare = command.Bare || !dump.Prefixed
	}
	command.Lists = command.Lists || (strings.HasPrefix(blurb, game.DumpPhrases.Iterates) && command.Target == "")
}

// splitUsage returns a dump's description as its prose and its usage, which starts at the first line that opens a
// block, such as "key = {". A line that describes its block after a dash is prose, as "key = value - a description"
// is. The usage is "" if no line opens a block.
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

// parseArguments returns the keys a command takes as fields, from its usage blocks and the prose lines that open with
// key = value, and its argument key, "" if none. It returns nil if the description gives the command no key and no
// shape.
func (schema *Schema) parseArguments(title, prose, usage string) (*Field, string) {
	arguments, first := &Field{Source: game.SourceDumps}, ""
	prose, usage = usageComment.ReplaceAllString(prose, ""), usageComment.ReplaceAllString(usage, "")
	lines := strings.Split(prose, "\n")
	for position, line := range lines {
		tokens := usageToken.FindAllString(line, -1)
		// a key with nothing after it whose block opens on the next line is the scope the usage runs in, as Vic3's
		// "power_bloc =" is, and no argument
		opens := len(tokens) == 2 && position+1 < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[position+1]), "{")
		if len(tokens) > 1 && tokens[0] != title && jomini.IsIdentifier(tokens[0]) && tokens[1] == "=" && !opens {
			schema.parseUsageStatement(tokens, 0, arguments, false)
		}
	}
	tokens := usageToken.FindAllString(prose+"\n"+usage, -1)
	for position := 0; position+2 < len(tokens); position++ {
		if tokens[position] == title && tokens[position+1] == "=" && tokens[position+2] == "{" {
			start := position + 3
			if first == "" && start+1 < len(tokens) && jomini.IsIdentifier(tokens[start]) &&
				slices.Contains(usageOperators, tokens[start+1]) {
				first = tokens[start]
			}
			position = schema.parseUsageBlock(tokens, start, arguments) - 1
		}
	}
	if len(arguments.Children) == 0 && !arguments.Shape.IsStated() {
		return nil, ""
	}
	return arguments, first
}

// parseUsageBlock reads the statements of the usage block that starts at a token into a field's children, up to the
// brace that closes it. A placeholder such as <triggers> gives the field the shape it names, and a statement written in
// angle or square brackets is optional. It returns the position after the closing brace.
func (schema *Schema) parseUsageBlock(tokens []string, cursor int, field *Field) int {
	for cursor < len(tokens) && tokens[cursor] != "}" {
		token := tokens[cursor]
		switch {
		case token == "{":
			cursor = schema.parseUsageBlock(tokens, cursor+1, &Field{})
		case (token[0] == '<' || token[0] == '[') && strings.Contains(token, "="):
			schema.parseUsageStatement(usageToken.FindAllString(token[1:len(token)-1], -1), 0, field, true)
			cursor++
		case token[0] == '<':
			if shape, ok := findPlaceholderShape(token[1:]); ok {
				field.Shape.resolve(shape, game.SourceDumps)
			}
			cursor++
		default:
			cursor = schema.parseUsageStatement(tokens, cursor, field, false)
		}
	}
	return cursor + 1
}

// parseUsageStatement reads the key = value statement of a usage that starts at a token into a field's children. It
// adds a child for each key that slashes join, and marks it optional if the given flag says so. It returns the
// position after the statement, or the next position if no statement starts there.
func (schema *Schema) parseUsageStatement(tokens []string, cursor int, field *Field, optional bool) int {
	if cursor+1 >= len(tokens) || !slices.Contains(usageOperators, tokens[cursor+1]) {
		return cursor + 1
	}
	// a key with no value, as create_character's "age =", is declared all the same
	valued := cursor+2 < len(tokens) && tokens[cursor+2] != "}"
	next := cursor + 2
	for part := range strings.SplitSeq(tokens[cursor], "/") {
		argument := &Field{} // a part that is no key still reads its value, into no field
		if jomini.IsIdentifier(part) {
			argument = field.buildChild(part, game.SourceDumps)
		}
		if optional {
			argument.Required.resolve("optional", game.SourceDumps)
		}
		if valued {
			next = schema.parseUsageValue(tokens, cursor+2, argument)
		}
	}
	return next
}

// parseUsageValue gives an argument what the value at a token says: its keys, its choices, its type or what its
// placeholder takes. A sample value says nothing. It returns the position after the value.
func (schema *Schema) parseUsageValue(tokens []string, cursor int, argument *Field) int {
	value := tokens[cursor]
	term := strings.TrimSuffix(strings.TrimPrefix(value, "<"), ">")
	_, known := game.DocTypes[ToPhrase(term)]
	switch {
	case value == "{":
		return schema.parseUsageBlock(tokens, cursor+1, argument)
	case strings.ContainsAny(value[:1], "<[") && strings.Contains(value, "|"):
		resolveChoices(argument, strings.Split(strings.Trim(value, "<>[]"), "|"), game.SourceDumps)
	case known && len(term) > 1:
		resolveDocType(argument, term, game.SourceDumps)
	case value[0] == '<':
		if expected, ok := schema.resolvePlaceholder(term); ok {
			argument.Value.resolve(expected, game.SourceDumps)
		}
	}
	return cursor + 1
}

// resolvePlaceholder returns what a placeholder in the dumps takes: a name of the database it names before a closing
// key or name, as <effect_localization_key> does, else a target of the scope type it names, or a name of that scope's
// database. It returns false if it names neither.
func (schema *Schema) resolvePlaceholder(term string) (Expected, bool) {
	words := strings.Fields(ToPhrase(term))
	if len(words) == 0 {
		return Expected{}, false
	}
	last, stem := words[len(words)-1], strings.Join(words[:len(words)-1], " ")
	if last == "key" || last == "name" {
		database, ok := schema.ResolveTypeDatabase(stem)
		return Expected{Type: ValueReference, Database: database}, ok
	}
	if last != "scope" && last != "target" {
		stem = strings.Join(words, " ")
	}
	scope := strings.ReplaceAll(stem, " ", "_")
	if !schema.isScope(scope) {
		return Expected{}, false
	}
	database, _ := schema.ResolveTypeDatabase(scope)
	return Expected{Type: ValueTarget, Scope: scope, Database: database}, true
}

// findPlaceholderShape returns the shape that a placeholder names, such as <triggers> or <display_triggers>. It returns
// false if it names none.
func findPlaceholderShape(placeholder string) (string, bool) {
	shape := strings.TrimSuffix(placeholder, ">")
	shape = shape[strings.LastIndex(shape, "_")+1:]
	if !strings.HasSuffix(shape, "s") {
		shape += "s"
	}
	_, ok := game.Shapes[shape]
	return shape, ok
}

// resolveTraits returns what a trigger's value must be, as its Traits line says: a number or a date, yes or no, a
// name, a link to a scope, or one of the line's values. It returns false for an empty line, a line in no known form
// and a class whose database the game lacks.
func (schema *Schema) resolveTraits(traits string) (Expected, bool) {
	comparison, class := comparisonTraits.FindStringSubmatch(traits), classTraits.FindStringSubmatch(traits)
	target, tag := targetTraits.FindStringSubmatch(traits), tagTraits.FindStringSubmatch(traits)
	list := choiceTraits.FindStringSubmatch(traits)
	switch {
	case comparison != nil && comparison[1] != "":
		return Expected{Type: ValueDate}, true
	case comparison != nil:
		return Expected{Type: ValueNumber, Values: true}, true
	case traits == "yes/no":
		return Expected{Type: ValueBool}, true
	case class != nil:
		database, ok := schema.FindDatabaseByClass(class[1])
		if !ok {
			return Expected{}, false
		}
		return Expected{Type: ValueReference, Database: database.Name}, true
	case target != nil:
		return Expected{Type: ValueTarget, Scope: target[1]}, true
	case tag != nil:
		database, _ := schema.ResolveTypeDatabase(tag[1])
		return Expected{Type: ValueTarget, Scope: tag[1], Database: database}, true
	case list != nil:
		choices := splitList(list[1])
		choices[len(choices)-1] = strings.TrimPrefix(choices[len(choices)-1], "and ")
		return Expected{Type: ValueChoice, Choices: choices}, true
	}
	return Expected{}, false
}

// makeArguments returns the field of the keys a command takes where it is declared as the given declaration. If the
// command has none there, it adds an empty one from the given source.
func (command *Command) makeArguments(declaration string, source game.Source) *Field {
	declaration = command.resolveDeclaration(declaration)
	if command.Arguments == nil {
		command.Arguments = map[string]*Field{}
	}
	if command.Arguments[declaration] == nil {
		command.Arguments[declaration] = &Field{Source: source}
	}
	return command.Arguments[declaration]
}

// buildKeywordShapes gives a command an argument with the row's shape for each keyword row written under it, such as
// the numbered branches of random_list, for the rows that hold for the given game.
func (schema *Schema) buildKeywordShapes(row *game.Game) {
	for _, keyword := range game.Keywords {
		title, key, nested := strings.Cut(keyword.Key, "/")
		command := schema.Commands[title]
		if !nested || command == nil || keyword.Shape == "" || keyword.Shape == "enclosing" || !row.Matches(keyword.Games) {
			continue
		}
		command.makeArguments("", game.SourceDumps).buildChild(key, game.SourceNone).Shape.resolve(keyword.Shape, game.SourceRegistry)
	}
}

// buildFamilies makes the commands of one list family, such as any_x, every_x, random_x and ordered_x, share the keys
// of their arguments. Each command keeps its own arguments field and shape. A command with no arguments gets none.
func (schema *Schema) buildFamilies() {
	families := map[string][]*Field{}
	for _, name := range slices.Sorted(maps.Keys(schema.Commands)) {
		iterator, ok := game.MatchListIterator(name)
		for _, declaration := range schema.Commands[name].Declared {
			if arguments := schema.Commands[name].Arguments[declaration]; ok && arguments != nil {
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

// buildUnions gives each command the dumps declare as both an effect and a trigger the keys of both together. The
// effect's claims win where the two state the same rank, but a shape stays only where both state it, or where it
// allows effects and triggers alike. A command where neither takes keys gets an empty union.
func (schema *Schema) buildUnions() {
	for _, command := range schema.Commands {
		if slices.Contains(command.Declared, game.DeclaredEffect) && slices.Contains(command.Declared, game.DeclaredTrigger) {
			fields := []*Field{command.Arguments[game.DeclaredEffect], command.Arguments[game.DeclaredTrigger]}
			fields = slices.DeleteFunc(fields, func(field *Field) bool { return field == nil })
			command.Union = unite(fields)
			keepSharedShapes(command.Union, fields)
		}
	}
}

// keepSharedShapes clears the shape of a united field, and of each key below it, where the given fields it merges do
// not all state it, unless the shape allows effects and triggers alike, as value does.
func keepSharedShapes(united *Field, fields []*Field) {
	allowed := game.Shapes[united.Shape.Value].Allows
	mixed := slices.Contains(allowed, game.DeclaredEffect) && slices.Contains(allowed, game.DeclaredTrigger)
	if slices.ContainsFunc(fields, func(field *Field) bool {
		return field.Shape.Value != united.Shape.Value && (field.Shape.IsStated() || !mixed)
	}) {
		united.Shape = Claim[string]{}
	}
	for key, child := range united.Children {
		var children []*Field
		for _, field := range fields {
			if field.Children[key] != nil {
				children = append(children, field.Children[key])
			}
		}
		keepSharedShapes(child, children)
	}
}

// unite returns one field that merges the given fields. It has one child for each key they write, merged the same way.
// It takes all their claims and counts, and the highest-ranked source that writes any of them.
func unite(fields []*Field) *Field {
	united := &Field{}
	keyed := map[string][]*Field{}
	for _, field := range fields {
		if field.Source != game.SourceNone && (united.Source == game.SourceNone || field.Source < united.Source) {
			united.Source = field.Source
		}
		united.take(field.Claims)
		united.Keys.take(field.Keys)
		united.Vanilla.add(field.Vanilla)
		for key, child := range field.Children {
			keyed[key] = append(keyed[key], child)
		}
	}
	for key, children := range keyed {
		if united.Children == nil {
			united.Children = map[string]*Field{}
		}
		united.Children[key] = unite(children)
	}
	return united
}
