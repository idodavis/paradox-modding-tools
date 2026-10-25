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
	Class       string // the database class a trigger's traits line names; "" if none
}

// Dumps is everything that a game's script_docs and data types dumps declare.
type Dumps struct {
	Commands  []DumpCommand // a list, since several dumps may list one name
	Scopes    map[string]ScopeType
	Saved     map[string]bool
	Modifiers map[string]Modifier
	OnActions map[string]OnAction
	DataTypes map[string]DataType
	WrittenAt time.Time // the oldest dump file's time
}

// yes is the term the dumps write for true.
const yes = "yes"

// savedTitle is the title of the list at the end of the event targets dump. It holds the targets the game saves from
// code.
const savedTitle = "Event Targets Saved from Code:"

// dumpFiles lists the dump files that are read.
var dumpFiles = []string{
	"effects.log", "triggers.log", "event_targets.log", "modifiers.log", "on_actions.log", "event_scopes.log",
	"data_types_common.txt", "data_types_gui.txt", "data_types_internalclausewitzgui.txt", "data_types_script.txt",
	"data_types_uncategorized.txt",
}

// The patterns of the lines a dump is read by.
var (
	dashesLine   = regexp.MustCompile(`^-{10,}$`)
	headingRow   = regexp.MustCompile(`^#{2,3} +(\S+)$`)
	scopeLine    = regexp.MustCompile(`^\*{0,2}(Supported Scopes|Supported Targets|Input Scopes|Output Scopes)\*{0,2}:\s*(.*)$`)
	usageLine    = regexp.MustCompile(`^\s*\S+\s*=\s*\{`)
	argumentLine = regexp.MustCompile(`^\s*(\S+?)\s*=`)
	titleLine    = regexp.MustCompile(`^([A-Za-z0-9_$]+):$`)
	pairLine     = regexp.MustCompile(`^\s*([A-Za-z ]+?):\s*(.*)$`)
	tagLine      = regexp.MustCompile(`^Tag:\s*([^,\s]+)(?:,\s*Categories:\s*(.*))?$`)
	traitsLine   = regexp.MustCompile(`^Traits:\s*class (C\w+) key$`)
	signature    = regexp.MustCompile(`^(\S+?)\s*(?:\(\s*(.*?)\s*\))?$`)
)

// FindDumpsAt returns when the dumps under the given folders were written: the time of the oldest dump file. It reads
// no file. The time is zero when no folder holds a dump, and the error is that of a dump file whose time cannot be
// read.
func FindDumpsAt(dirs []string) (time.Time, error) {
	var oldest time.Time
	for _, file := range dumpFiles {
		_, fileAt, err := findDump(dirs, file)
		if err != nil {
			return time.Time{}, err
		}
		if !fileAt.IsZero() && (oldest.IsZero() || fileAt.Before(oldest)) {
			oldest = fileAt
		}
	}
	return oldest, nil
}

// ReadDumps reads every dump file under the given folders. A missing file is left out, so the caller can fill it from
// the shipped backup. It returns an error only for a file that exists and cannot be read.
func ReadDumps(dirs []string) (*Dumps, error) {
	dumpsAt, err := FindDumpsAt(dirs)
	if err != nil {
		return nil, err
	}
	dumps := &Dumps{
		Scopes: map[string]ScopeType{}, Saved: map[string]bool{}, Modifiers: map[string]Modifier{},
		OnActions: map[string]OnAction{}, DataTypes: map[string]DataType{}, WrittenAt: dumpsAt,
	}
	for _, file := range dumpFiles {
		text, err := loadDump(dirs, file)
		if err != nil {
			return nil, err
		}
		dumps.parseFile(file, text)
	}
	return dumps, nil
}

// findDump returns the path and the time of the first file of the given base name that one of the folders holds. The
// path is "" and the time zero when no folder holds the file.
func findDump(dirs []string, file string) (string, time.Time, error) {
	for _, dir := range dirs {
		path := filepath.Join(dir, file)
		stat, err := os.Stat(path)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			continue
		case err != nil:
			return "", time.Time{}, err
		}
		return path, stat.ModTime(), nil
	}
	return "", time.Time{}, nil
}

// loadDump loads the text of the first file of the given base name that one of the folders holds. The text is ""
// when no folder holds the file.
func loadDump(dirs []string, file string) (string, error) {
	path, _, err := findDump(dirs, file)
	if path == "" || err != nil {
		return "", err
	}
	text, err := files.ReadText(path)
	return text.Text, err
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
		for title, scope := range parseScoped(text) {
			dumps.OnActions[title] = OnAction{Name: title, Scope: scope}
		}
	case "event_scopes.log":
		dumps.parseScopes(text)
	default:
		dumps.parseDataTypes(text)
	}
}

// parseSections yields the entries of a dump, each as its first line and the lines after it. A line of dashes or a
// markdown heading starts an entry. A heading is the whole first line of its entry. If the flag says the dump opens
// with a title, the title is skipped.
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

// parseCommands parses the effects, triggers or links of one script_docs dump, in the order it lists them. The event
// targets dump ends with a list of the targets that the game saves from code. Those are names only.
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
			case trimmed == "Global Link: yes": // dropped from the description
			case trimmed == "Wild Card: yes":
				command.Wild = true
			case traits != nil:
				command.Class = traits[1]
			case scope == nil:
				lines = append(lines, line)
			case scope[1] == "Supported Scopes" || scope[1] == "Input Scopes":
				command.Scopes = splitScopes(scope[2])
			default:
				// A line that names several scopes gives no single target.
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

// parseScoped parses the on_actions dump, whose entries are a title and the scope it is used in. It returns the scope
// by title.
func parseScoped(text string) map[string]string {
	scopes := map[string]string{}
	for head, body := range parseSections(text, true) {
		if title, ok := strings.CutSuffix(head, ":"); ok {
			scopes[title] = strings.Join(splitScopes(parsePairs(body)["Expected Scope"]), "")
		}
	}
	return scopes
}

// parseScopes parses the scope types of the event scopes dump. Each is a title, then what script can do in the scope.
// The save token the dump also writes is not read.
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

// parseModifiers parses the modifiers dump. The games write it in three layouts: a `Tag:` line with a `Use areas:` line
// below it, a `Tag:` line with its categories, or a title with an indented description.
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

// parseDataTypes parses the types and functions of one data types dump. A function written on a type belongs to that
// type, and a global one belongs to game.GlobalType. A function that returns its own object (game.SelfFunctions)
// returns its type. A macro keeps its text. A function written for several arities keeps the first.
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
				Expansion: pairs["Macro replacement"],
			}
		}
		dumps.DataTypes[owner] = dataType
	}
}

// buildDumps reads the dumps of the install and builds what they declare. What they lack comes from the shipped
// backup. It records where the dumps came from and whether they are stale.
func (schema *Schema) buildDumps(row *game.Game, installPath string) error {
	dumps, err := ReadDumps(game.FindDumpDirs(row.ID, installPath))
	if err != nil {
		return err
	}
	backup, hasBackup := LoadShipped(row.ID)
	filled := hasBackup && dumps.resolveMissing(backup)
	state := SourceState{Origin: originNone, WrittenAt: dumps.WrittenAt}
	switch {
	case !dumps.WrittenAt.IsZero():
		state.Origin = originInstall
		state.Stale = dumps.WrittenAt.Before(game.ReadInstallUpdatedAt(installPath))
	case filled:
		state.Origin = originShipped
	}
	schema.Dumps = state
	schema.buildCommands(dumps.Commands)
	schema.buildKeywordShapes()
	schema.buildFamilies()
	schema.Scopes, schema.Saved, schema.Modifiers = dumps.Scopes, dumps.Saved, dumps.Modifiers
	schema.OnActions, schema.DataTypes = dumps.OnActions, dumps.DataTypes
	return nil
}

// resolveMissing takes each section the dumps lack from the backup. It reports whether it took any. The effects,
// triggers and links are three sections. The links bring the targets saved from code.
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
// takes data after a colon is a prefix. A trigger that takes the key of a database names that database.
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

// build adds what one dump says about a command. Its scopes are the union of the scopes of its dumps, and a dump that
// names none makes it run in any scope. The first target, blurb and arguments are kept.
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
	prose, usage := cutUsage(dump.Description)
	blurb, _, _ := strings.Cut(strings.TrimSpace(prose), "\n")
	blurb = strings.TrimSpace(blurb)
	command.Blurb = cmp.Or(command.Blurb, blurb)
	command.Usage = cmp.Or(command.Usage, usage)
	if command.Arguments == nil {
		command.Arguments = parseArguments(dump.Name, prose, usage)
	}
	if dump.Declared == game.DeclaredLink {
		command.Wild = dump.Wild
	}
	command.Lists = command.Lists || (strings.HasPrefix(blurb, game.DumpPhrases.Iterates) && command.Target == "")
}

// cutUsage splits a dump description into its prose and its usage. The usage starts at the first line written as
// `<scope> = { … }`.
func cutUsage(description string) (prose string, usage string) {
	lines := strings.Split(description, "\n")
	cut := slices.IndexFunc(lines, usageLine.MatchString)
	if cut < 0 {
		return description, ""
	}
	return strings.Join(lines[:cut], "\n"), strings.Join(lines[cut:], "\n")
}

// parseArguments returns the keys a command takes, as fields. They are the keys inside every block of the usage that is
// keyed by the command's name, and the keys that the prose writes as `key = value`. It returns nil if there are none.
func parseArguments(title, prose, usage string) *Field {
	field, found := &Field{Source: game.SourceDumps}, false
	for line := range strings.Lines(prose) {
		if match := argumentLine.FindStringSubmatch(line); match != nil && match[1] != title && jomini.IsIdentifier(match[1]) {
			field.buildChild(match[1]).Source, found = game.SourceDumps, true
		}
	}
	tree := jomini.Parse(usage)
	for statement := 1; statement < len(tree.Statements); statement++ {
		if tree.Text(tree.Statements[statement].Key) == title && tree.Statements[statement].IsBlock() {
			buildArguments(tree, int32(statement), field)
			found = true
		}
	}
	if !found {
		return nil
	}
	return field
}

// buildArguments adds the keys of a usage block to a field as children. A key written with slashes adds a child for
// each part. A child gets the keys of its own block, and the type its value names if the value is a type term. A
// placeholder such as `<triggers>` inside a block gives the block its shape.
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
		case !inner.IsListed():
			for part := range strings.SplitSeq(key, "/") {
				if !jomini.IsIdentifier(part) {
					continue
				}
				argument := field.buildChild(part)
				argument.Source = game.SourceDumps
				term := strings.TrimSuffix(strings.TrimPrefix(tree.Text(inner.Value), "<"), ">")
				_, known := game.DocTypes[ToPhrase(term)]
				switch {
				case inner.IsBlock():
					buildArguments(tree, child, argument)
				case known && len(term) > 1:
					resolveDocType(argument, term, game.SourceDumps)
				}
			}
		}
	}
}

// findPlaceholderShape returns the shape that a placeholder names, such as `<triggers>`, `<effect>` or
// `<display_triggers>`. It returns false if the placeholder names none.
func findPlaceholderShape(placeholder string) (string, bool) {
	shape := strings.TrimSuffix(placeholder, ">")
	shape = shape[strings.LastIndex(shape, "_")+1:]
	if !strings.HasSuffix(shape, "s") {
		shape += "s"
	}
	_, ok := game.Shapes[shape]
	return shape, ok
}

// buildClass gives a command a key of a database as its argument, if its dump names the class that holds the key.
func (schema *Schema) buildClass(command *Command, class string) {
	if class == "" {
		return
	}
	database, ok := schema.FindDatabaseByClass(class)
	if !ok {
		return
	}
	command.makeArguments(game.SourceDumps).Value.resolve(Expected{Type: ValueReference, Database: database.Name}, game.SourceDumps)
}

// makeArguments returns the field of the keys a command takes. It makes the field, with the given source, if the
// command has none.
func (command *Command) makeArguments(source game.Source) *Field {
	if command.Arguments == nil {
		command.Arguments = &Field{Source: source}
	}
	return command.Arguments
}

// buildKeywordShapes gives a command an argument for each keyword row written under it, such as the numbered branches
// of random_list. The argument gets the shape that the row gives its block.
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

// buildFamilies makes the commands of one list family, any_x, every_x, random_x and ordered_x, share the keys of their
// arguments. Each command keeps its own arguments field and shape. A command with no arguments gets none.
func (schema *Schema) buildFamilies() {
	families := map[string][]*Field{}
	for _, name := range slices.Sorted(maps.Keys(schema.Commands)) {
		iterator, ok := game.MatchListIterator(name)
		if arguments := schema.Commands[name].Arguments; ok && arguments != nil {
			base := strings.TrimPrefix(name, strings.TrimSuffix(iterator.Key, "*"))
			families[base] = append(families[base], arguments)
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

// unite returns one field that merges the given fields. It has one child for each key they write, merged the same way,
// and it states all their claims.
func unite(fields []*Field) *Field {
	united := &Field{}
	keyed := map[string][]*Field{}
	for _, field := range fields {
		united.Source = cmp.Or(united.Source, field.Source)
		united.take(field.Claims)
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
