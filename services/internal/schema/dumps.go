// dumps.go is the console dumps, or the shipped backup, parsed into commands, scopes, saved scopes,
// modifiers, on_actions, data types and custom loc; the usages merged into each command's arguments.

package schema

import (
	"cmp"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/inventory"
	"paradox-modding-tools/services/internal/jomini"
)

// Modifier is one modifier the dumps declare.
type Modifier struct {
	Pattern string // a name written with $TYPE$ parts, as a glob; "" for a plain name
	Blurb   string
}

// OnAction is one on_action the dumps declare.
type OnAction struct {
	Scope string // the scope it expects; "" none named
	Blurb string
}

// DataType is one type of the data system; "Global" holds the global functions.
type DataType struct {
	Functions map[string]DataFunction // by name
}

// DataFunction is one function, promote or macro of a data type.
type DataFunction struct {
	Returns string // the type it returns
}

// CustomLoc is one custom localization.
type CustomLoc struct {
	Scope   string   // the scope it runs in
	Parent  string   // the parent the dump names
	Suffix  string   // the suffix the dump names
	Entries []string // the keys it picks between
}

// dumpEntry is one entry of a dump file.
type dumpEntry struct {
	name   string
	group2 string            // its second group
	fields map[string]string // the labels it fills, by field
	lines  []string          // the lines that fill none
	usage  string            // its usage, as script
	blurb  string            // its description
}

// The kinds of dump record whose names the table and its reader share, the field the labels that open a
// command's usage fill, and the dumps' word for true.
const (
	recordScope   = "Scope"
	recordCommand = "Command"
	usageField    = "Command.Usage"
	yes           = "yes"
)

// dumpLabels is the label table, one row per kind of dump file in reading order: the record, its files, the
// entry header (group 1 names the entry) and each label with the field it fills ("" kept nowhere).
var dumpLabels = []struct {
	record string
	files  []string
	entry  *regexp.Regexp
	labels map[string]string
}{
	{
		record: recordScope,
		files:  []string{"event_scopes.log"},
		entry:  regexp.MustCompile(`(?m)^(\S+):$`),
		labels: map[string]string{
			"Save Token": "", "Evaluate Triggers": "Scope.Triggers", "Execute Effects": "Scope.Effects",
			"Change Scopes": "", "Stores Variables": "Scope.Variables",
		},
	},
	{
		record: recordCommand,
		files:  []string{"event_targets.log"},
		entry:  regexp.MustCompile(`(?m)^(?:(?:-{20}\n\n|### )(\S+)(?: - |$)|(Event Targets Saved from Code):$)`),
		labels: map[string]string{
			"Input Scopes": "Command.Scopes", "Output Scopes": "Command.Out", "Requires Data": "",
			"Global Link": "Command.Global", "Wild Card": "Command.Wild",
		},
	},
	{
		record: recordCommand,
		files:  []string{"effects.log", "triggers.log"},
		entry:  regexp.MustCompile(`(?m)^(?:-{20}\n\n|## )(\S+)(?: - |$)`),
		labels: map[string]string{
			"Supported Scopes": "Command.Scopes", "Supported Targets": "Command.Out", "Traits": "Command.Args.Value",
			"Usage": usageField, "usage": usageField, "Usages": usageField, "Example": usageField, "example": usageField,
		},
	},
	{
		record: "Modifier",
		files:  []string{"modifiers.log"},
		entry:  regexp.MustCompile(`(?m)^(?:(?:Tag: )?([^\s,:]+)(?::|,.*)?|--- (.+) ---)$`),
		labels: map[string]string{
			"Description": "Blurb", "Extra info": "Blurb", "Use areas": "", "Mask": "", "Name": "",
		},
	},
	{
		record: "OnAction",
		files:  []string{"on_actions.log"},
		entry:  regexp.MustCompile(`(?m)^-{20}\n\n(\S+):$`),
		labels: map[string]string{"Expected Scope": "OnAction.Scope", "From Code": ""},
	},
	{
		record: "CustomLoc",
		files:  []string{"custom_localization.log"},
		entry:  regexp.MustCompile(`(?m)^-{20}\n\n(\S+):$`),
		labels: map[string]string{
			"Scope": "CustomLoc.Scope", "Random Valid": "", "Parent": "CustomLoc.Parent",
			"Suffix": "CustomLoc.Suffix", "Entries": "",
		},
	},
	{
		// groups: the type, or a global's name; the function; its arguments. "Definition type" files the
		// entry among its type's functions, or declares the type when it says Type.
		record: "DataType",
		files: []string{
			"data_types_common.txt", "data_types_gui.txt", "data_types_internalclausewitzgui.txt",
			"data_types_script.txt", "data_types_uncategorized.txt",
		},
		entry: regexp.MustCompile(`(?m)(?:\A|^-{20,}\n\n)(\w+)(?:\.(\w+))?(?: *\( *([^)\n]*?) *\))?[^\n]*$`),
		labels: map[string]string{
			"Definition type": "DataType.Functions", "Return type": "DataFunction.Returns",
			"Macro replacement": "", "Description": "",
		},
	},
}

// dumpDeclared is what each dump file declares its commands to be, which Command.Declared records.
var dumpDeclared = map[string]string{
	"effects.log": game.DeclaredEffect, "triggers.log": game.DeclaredTrigger, "event_targets.log": game.DeclaredLink,
}

// DumpFiles lists every file name the dumps are read from.
var DumpFiles = func() (names []string) {
	for _, dump := range dumpLabels {
		names = append(names, dump.files...)
	}
	return names
}()

// MissingDumps lists the dump files no file of files has, by base name.
func MissingDumps(files []inventory.File) []string {
	return slices.DeleteFunc(slices.Clone(DumpFiles), func(name string) bool {
		return slices.ContainsFunc(files, func(file inventory.File) bool { return filepath.Base(file.Path) == name })
	})
}

// labelLine matches a "Label: value" line, the label maybe bold; dashes matches the line that ends an entry.
var labelLine, dashes = regexp.MustCompile(`^\s*\*{0,2}([^:*]+?)\*{0,2}: *(.*)$`), regexp.MustCompile(`(?m)^-{20,}$`)

// usageLabel matches a usage label of the table, with its colon or dash and the parenthesis that may open it,
// wherever a description line writes it ((example: x > 3), Usage: x = y).
var usageLabel = func() *regexp.Regexp {
	var labels []string
	for _, dump := range dumpLabels {
		for label, field := range dump.labels {
			if field == usageField {
				labels = append(labels, regexp.QuoteMeta(label))
			}
		}
	}
	slices.Sort(labels)
	return regexp.MustCompile(`\(?\b(?:` + strings.Join(labels, "|") + `)\s*(?::|-\s)`)
}()

// The parts of scriptStart: what follows a statement's key (an equals sign, which a comma may precede; a comparison,
// <, >, <=, >=, != or ?=, where < and > need a blank after them; a brace after a blank), a <placeholder> in a
// statement's place (an equals sign, a brace or the line's end follows), a statement's key (scope:x and c:GBR are keys;
// group 1 leaves out a word and a dot before it, the end of a sentence the dump ran into the usage: the same
// time.kill_population = {), and a quoted key.
const (
	afterKey             = `(?:,?\s*=|\s*(?:[<>!?]=|[<>]\s)|\s+\{)`
	statementPlaceholder = `<[^<>\s=][^<>]*>\s*(?:[={}]|$)`
	statementKey         = `(?:[\w*/]+\.)?([\w:./*]+)` + afterKey
	quotedKey            = `^\s*"[^"]*"` + afterKey
)

// scriptStart matches what opens script in a description line: a statement's key or a <placeholder> (a bullet
// before it goes with it), a quoted key at the line's start, a brace opening a block, a brace closing at the line's
// start.
var scriptStart = regexp.MustCompile(`(?:^\s*[*-]\s+)?(?:` + statementPlaceholder + `|` + statementKey + `)|` +
	quotedKey + `|\{|^\s*\}`)

// blockPlaceholder matches a placeholder standing for a block of triggers or effects in a usage.
var blockPlaceholder = regexp.MustCompile(`<[^<>]*(?i:trigger|effect)[^<>]*>|(?i:triggers?|effects?)\.\.\.`)

// dumpsOrShipped reads the dump files into the schema, or takes a shipped dump part whole. The dumps
// are stale when older than the install's last update.
func (schema *Schema) dumpsOrShipped(input Input) {
	if shipped := input.Shipped; shipped != nil {
		schema.Commands, schema.Scopes, schema.Saved = shipped.Commands, shipped.Scopes, shipped.Saved
		schema.Modifiers, schema.OnActions = shipped.Modifiers, shipped.OnActions
		schema.DataTypes, schema.CustomLoc = shipped.DataTypes, shipped.CustomLoc
		schema.Sources[game.SourceDumps] = shipped.Sources[game.SourceDumps]
		return
	}
	oldest := schema.parseDumps(input.Dumps)
	schema.Sources[game.SourceDumps] = SourceState{Origin: "none"}
	if !oldest.IsZero() {
		schema.Sources[game.SourceDumps] = SourceState{Origin: "yours", WrittenAt: oldest, Stale: oldest.Before(input.Updated)}
	}
}

// parseDumps reads the dump files into the schema and returns when the oldest of them was written: the dumps
// are as fresh as their oldest file. Usages are applied last, once every command is declared.
func (schema *Schema) parseDumps(files []inventory.File) (oldest time.Time) {
	schema.Commands, schema.Scopes, schema.Saved = map[string]*Command{}, map[string]ScopeType{}, map[string]bool{}
	schema.Modifiers, schema.OnActions = map[string]Modifier{}, map[string]OnAction{}
	schema.DataTypes, schema.CustomLoc = map[string]DataType{}, map[string]CustomLoc{}

	firstFiles := map[string]inventory.File{}
	for _, file := range files {
		if _, present := firstFiles[filepath.Base(file.Path)]; !present {
			firstFiles[filepath.Base(file.Path)] = file
		}
	}
	ranked, usages := map[string]int{}, map[string][]string{} // per command: the blurb's reading rank; usage texts
	for _, dump := range dumpLabels {
		for _, base := range dump.files {
			file, present := firstFiles[base]
			text, err := file.Text()
			if !present || err != nil {
				continue
			}
			if info, err := os.Stat(file.Path); err == nil && (oldest.IsZero() || info.ModTime().Before(oldest)) {
				oldest = info.ModTime()
			}
			for _, entry := range splitEntries(dump.entry, dump.labels, text.Content) {
				switch dump.record {
				case recordScope:
					schema.parseScope(entry)
				case recordCommand:
					schema.parseCommand(entry, dumpDeclared[base], ranked, usages)
				case "Modifier":
					schema.parseModifier(entry)
				case "OnAction":
					schema.parseOnAction(entry)
				case "CustomLoc":
					schema.parseCustomLoc(entry)
				case "DataType":
					schema.parseDataType(entry)
				}
			}
		}
	}
	schema.mergeUsages(usages)
	return oldest
}

// splitEntries splits a dump file into its entries, each from its header to the next header or a dashes
// line, and sorts the lines of each into the labels' fields and the lines that fill none.
func splitEntries(header *regexp.Regexp, labels map[string]string, content string) (entries []dumpEntry) {
	headers := header.FindAllStringSubmatchIndex(content, -1)
	for n, span := range headers {
		end := len(content)
		if n+1 < len(headers) {
			end = headers[n+1][0]
		}
		body := content[span[1]:end]
		if cut := dashes.FindStringIndex(body); cut != nil {
			body = body[:cut[0]]
		}
		group := func(number int) string {
			if 2*number+1 >= len(span) || span[2*number] < 0 {
				return ""
			}
			return content[span[2*number]:span[2*number+1]]
		}
		entries = append(entries, parseEntryBody(body, group(1), group(2), labels))
	}
	return entries
}

// parseEntryBody sorts an entry's lines into fields and description and cuts off its usage, which starts at its label
// or, where none opens one, where a description line first writes script (cutScript).
func parseEntryBody(body, name, group2 string, labels map[string]string) dumpEntry {
	fields, lines, usageStart := map[string]string{}, []string{}, -1
	for line := range strings.SplitSeq(body, "\n") {
		line = strings.TrimRight(line, " \t\r")
		field, value, known := knownLabel(line, labels)
		switch {
		case known && field != usageField:
			fields[field] = value
			continue
		case known:
			if usageStart < 0 {
				usageStart = len(lines)
			}
			if line = value; line == "" {
				continue
			}
		case usageStart < 0:
			if prose, usage, cut := cutScript(line); cut {
				if prose != "" {
					lines = append(lines, prose)
				}
				usageStart, line = len(lines), usage
			}
		}
		lines = append(lines, line)
	}
	description, usage := lines, ""
	if usageStart >= 0 {
		description, usage = lines[:usageStart], strings.TrimRight(strings.Join(lines[usageStart:], "\n"), " \t\n")
	}
	blurb := strings.TrimSpace(strings.Join(append([]string{fields["Blurb"]}, description...), "\n"))
	return dumpEntry{name: name, group2: group2, fields: fields, lines: lines, usage: usage, blurb: blurb}
}

// knownLabel splits a "Label: value" line whose label the table lists into the field it fills and the value.
func knownLabel(line string, labels map[string]string) (field, value string, known bool) {
	label := labelLine.FindStringSubmatch(line)
	if label == nil {
		return "", "", false
	}
	field, known = labels[label[1]]
	return field, label[2], known
}

// cutScript cuts a description line into prose and the usage from the first place it writes script (a usage label, or a
// key before an equals sign, a comparison, a brace or a <placeholder>); script quoted in prose stays prose.
func cutScript(line string) (prose, usage string, cut bool) {
	start, usageStart := -1, -1
	for _, match := range scriptStart.FindAllStringSubmatchIndex(line, -1) {
		first := match[0]
		if first > 0 && match[2] >= 0 {
			first = match[2]
		}
		if first == 0 || !strings.ContainsRune("'\"`(", rune(line[first-1])) {
			start, usageStart = first, first
			break
		}
	}
	if label := usageLabel.FindStringIndex(line); label != nil && (start < 0 || label[0] < start) {
		start, usageStart = label[0], label[1]
	}
	if start < 0 {
		return line, "", false
	}
	return strings.TrimRight(line[:start], " \t"), strings.TrimLeft(line[usageStart:], " \t"), true
}

// parseScope reads a scope type's entry.
func (schema *Schema) parseScope(entry dumpEntry) {
	schema.Scopes[entry.name] = ScopeType{
		Name:      entry.name,
		Variables: entry.fields["Scope.Variables"] != "no",
		Triggers:  entry.fields["Scope.Triggers"] != "no",
		Effects:   entry.fields["Scope.Effects"] != "no",
	}
}

// parseCommand reads an effect, trigger or link entry into the command of its name. A name written twice is
// one command that runs where either reading runs.
func (schema *Schema) parseCommand(entry dumpEntry, declared string, ranked map[string]int, usages map[string][]string) {
	if entry.group2 != "" { // the block closing the link dump, "Event Targets Saved from Code"
		for _, line := range entry.lines {
			if line != "" {
				schema.Saved[line] = true
			}
		}
		return
	}
	name := strings.ToLower(entry.name) // script writes AND for and
	scopes := scopeList(entry.fields["Command.Scopes"])
	command := schema.Commands[name]
	switch {
	case command == nil:
		command = &Command{Scopes: scopes, Args: &Position{}}
		schema.Commands[name] = command
	case len(scopes) == 0:
		command.Scopes = nil // a reading with no scope runs anywhere
	case len(command.Scopes) > 0:
		command.Scopes = slices.Compact(slices.Sorted(slices.Values(append(command.Scopes, scopes...))))
	}

	// a trigger or effect reading's blurb beats a plain link's, which beats a global link's
	reading := 2
	if declared != game.DeclaredLink {
		reading = 0
	} else if entry.fields["Command.Global"] != yes {
		reading = 1
	}
	if _, isRanked := ranked[name]; entry.blurb != "" && (!isRanked || reading < ranked[name]) {
		command.Blurb, ranked[name] = entry.blurb, reading
	}
	if !slices.Contains(command.Declared, declared) {
		command.Declared = append(command.Declared, declared)
	}
	command.Target = cmp.Or(command.Target, entry.fields["Command.Out"])
	command.Global = command.Global || entry.fields["Command.Global"] == yes
	command.Wild = command.Wild || entry.fields["Command.Wild"] == yes

	if claim := schema.valueFromTraits(entry.fields["Command.Args.Value"]); claim.Type != ValueUnknown || claim.Choices != nil {
		command.Args.Value.Set(claim, game.SourceDumps)
	}
	if strings.TrimSpace(entry.usage) != "" {
		usages[name] = append(usages[name], entry.usage)
	}
}

// scopeList returns the scope names of a comma-separated list, without the words that name no scope.
func scopeList(list string) (scopes []string) {
	for name := range strings.SplitSeq(list, ",") {
		if name = strings.TrimSpace(name); name != "" && !slices.Contains(game.DumpWords.NoScope, name) {
			scopes = append(scopes, name)
		}
	}
	return scopes
}

// valueFromTraits reads what a command's own value is from the dump's Traits line: a class names the database
// its key is in, a list of words its choices.
func (schema *Schema) valueFromTraits(traits string) (claim Expected) {
	switch {
	case traits == "yes/no":
		claim.Type = ValueBool
	case strings.HasSuffix(traits, " valid date"):
		claim.Type = ValueDate
	case strings.HasPrefix(traits, "<"): // a comparison operator says no type
	case strings.HasSuffix(traits, " scope"), strings.HasSuffix(traits, " target"):
		claim.Type = ValueScope
	case strings.HasPrefix(traits, "class ") && strings.HasSuffix(traits, " key"):
		class := strings.TrimSuffix(strings.TrimPrefix(traits, "class "), " key")
		if database := schema.resolveClassDatabase(class); database != "" {
			claim = Expected{Type: ValueReference, Database: database}
		}
	case strings.Contains(traits, ", "):
		list := traits
		if _, after, colon := strings.Cut(traits, ": "); colon {
			list = after
		}
		for word := range strings.SplitSeq(list, ", ") {
			claim.Choices = append(claim.Choices, strings.TrimPrefix(word, "and "))
		}
		slices.Sort(claim.Choices)
		claim.Choices = slices.Compact(claim.Choices)
	}
	return claim
}

// resolveClassDatabase returns the database a dumps' class name (CTenetTypeDatabase) keys: game.ClassDatabases'
// where it has one, else the one its snake_case stem names, else that without a trailing type or types word; "" none.
func (schema *Schema) resolveClassDatabase(class string) string {
	if database, row := game.ClassDatabases[class]; row {
		return database
	}
	const suffix = "database" // the dumps write DataBase as well as Database
	stem := strings.TrimPrefix(class, "C")
	if len(stem) < len(suffix) || !strings.EqualFold(stem[len(stem)-len(suffix):], suffix) {
		return ""
	}
	snake := SnakeCase(stem[:len(stem)-len(suffix)])
	if database := schema.ResolveNamedDatabase(snake); database != "" {
		return database
	}
	for _, ending := range []string{"_types", "_type"} {
		if trimmed, dropped := strings.CutSuffix(snake, ending); dropped {
			return schema.ResolveNamedDatabase(trimmed)
		}
	}
	return ""
}

// SnakeCase turns a class name into snake_case: TenetType is tenet_type.
func SnakeCase(class string) string {
	var out strings.Builder
	for i, char := range class {
		if unicode.IsUpper(char) && i > 0 {
			out.WriteByte('_')
		}
		out.WriteRune(unicode.ToLower(char))
	}
	return out.String()
}

// parseModifier reads a modifier's entry; a $TYPE$ part names a family matched as a glob.
func (schema *Schema) parseModifier(entry dumpEntry) {
	if entry.group2 != "" { // a --- section --- heading
		return
	}
	pattern := ""
	if spans := jomini.Parameters(entry.name); len(spans) > 0 {
		pattern = entry.name
		for _, span := range slices.Backward(spans) {
			pattern = pattern[:span.Start] + "*" + pattern[span.End:]
		}
	}
	schema.Modifiers[entry.name] = Modifier{Pattern: pattern, Blurb: entry.blurb}
}

// parseOnAction reads an on_action's entry.
func (schema *Schema) parseOnAction(entry dumpEntry) {
	schema.OnActions[entry.name] = OnAction{Scope: cmp.Or(scopeList(entry.fields["OnAction.Scope"])...), Blurb: entry.blurb}
}

// parseCustomLoc reads a custom localization's entry.
func (schema *Schema) parseCustomLoc(entry dumpEntry) {
	var entries []string
	for _, line := range entry.lines {
		if line != "" {
			entries = append(entries, line)
		}
	}
	schema.CustomLoc[entry.name] = CustomLoc{
		Scope: cmp.Or(scopeList(entry.fields["CustomLoc.Scope"])...), Parent: entry.fields["CustomLoc.Parent"],
		Suffix: entry.fields["CustomLoc.Suffix"], Entries: entries,
	}
}

// parseDataType reads a data type's entry, or one function of one; a function with no type is a global.
func (schema *Schema) parseDataType(entry dumpEntry) {
	owner, member := entry.name, entry.group2
	if member == "" && entry.fields["DataType.Functions"] != "Type" {
		owner, member = game.GlobalType, owner
	}
	dataType := schema.DataTypes[owner]
	if dataType.Functions == nil {
		dataType.Functions = map[string]DataFunction{}
	}
	if _, present := dataType.Functions[member]; member != "" && !present {
		dataType.Functions[member] = DataFunction{Returns: entry.fields["DataFunction.Returns"]}
	}
	schema.DataTypes[owner] = dataType
}

// mergeUsages merges each command's usages into its arguments once every command is declared, then decides whether
// it iterates a list and whether its block takes arguments only.
func (schema *Schema) mergeUsages(usages map[string][]string) {
	for _, name := range slices.Sorted(maps.Keys(usages)) {
		command := schema.Commands[name]
		blocks, plain := 0, 0
		for _, usage := range usages[name] {
			tree := parseDocScript(strings.Split(usage, "\n"))
			for _, root := range schema.usageRoots(tree, name) {
				schema.mergeUsage(root, command)
				// a block at the top, or the tree itself when the usage lists the arguments, is one of the command's
				if root.Block && slices.Contains(tree.Children, root) || root == tree && len(tree.Children) > 0 {
					blocks++
					if argumentsOnly(root) {
						plain++
					}
				}
			}
		}
		// a command with no target whose blurb says it iterates runs its block once per list item
		command.Lists = command.Target == "" && strings.Contains(command.Blurb, game.DumpWords.Iterates)
		if blocks > 0 && plain == blocks {
			command.Args.Shape.Set(game.ShapeArguments, game.SourceDumps)
		}
	}
}

// usageRoots returns the nodes of a usage that stand for the command: every node named as it, else the one top
// statement (an alias family writes it under a sibling's name) unless that is a script body, else the tree itself.
func (schema *Schema) usageRoots(tree *DocNode, name string) (roots []*DocNode) {
	roots = namedNodes(tree, name, nil)
	switch {
	case len(roots) > 0:
		return roots
	case len(tree.Children) == 1 && tree.Children[0].Key != "":
		root := tree.Children[0]
		declared := func(key string) bool {
			return schema.Commands[strings.ToLower(key)] != nil || schema.Link(strings.ToLower(key)) != nil
		}
		body := slices.ContainsFunc(root.Children, func(child *DocNode) bool { return child.Key != "" && declared(child.Key) })
		if !declared(root.Key) && body {
			return nil
		}
		return []*DocNode{root}
	}
	return []*DocNode{tree}
}

// namedNodes appends the nodes below one whose key is a name, at any depth, in source order.
func namedNodes(node *DocNode, name string, nodes []*DocNode) []*DocNode {
	for _, child := range node.Children {
		if strings.EqualFold(child.Key, name) {
			nodes = append(nodes, child)
		}
		nodes = namedNodes(child, name, nodes)
	}
	return nodes
}

// argumentsOnly reports whether a block holds arguments only: no statement in it, at any depth, writes a
// placeholder that names triggers or effects, or ... in their place.
func argumentsOnly(node *DocNode) bool {
	for _, child := range node.Children {
		if blockPlaceholder.MatchString(child.Key) || blockPlaceholder.MatchString(child.Value) || !argumentsOnly(child) {
			return false
		}
	}
	return true
}

// mergeUsage merges a node standing for a command into the command's arguments: a named node's own claims are
// the arguments position's, and each statement in its block is an argument.
func (schema *Schema) mergeUsage(root *DocNode, command *Command) {
	if root.Key != "" {
		command.Args.declare(schema.usageClaims(root), game.SourceDumps)
	}
	for _, child := range root.Children {
		// a usage is written in no database: an empty one stands for the entry its arguments are read in
		schema.argument(child, Context{BlockPosition: command.Args, Shape: game.ShapeFields, Database: &Database{}, Entry: -1})
	}
}

// argument merges one statement of a usage's block as an argument at a context: its position is made where the walker
// reads its key and gets the claims unless the key is a declared command. A bare value's claims are the holder's.
func (schema *Schema) argument(node *DocNode, context Context) {
	if node.Key == "" {
		if context.BlockPosition != nil {
			context.BlockPosition.declare(schema.usageClaims(node), game.SourceDumps)
		}
		for _, child := range node.Children { // a block with no key reads in place
			schema.argument(child, context)
		}
		return
	}
	for _, name := range keyNames(node.Key) {
		inner := schema.Enter(context, &jomini.Assignment{Key: jomini.Scalar{Text: name}}, nil, true)
		if inner.Position() != nil && inner.KeyRule != game.KeyRuleCommand && inner.KeyRule != game.KeyRuleLink {
			inner.Position().declare(schema.usageClaims(node), game.SourceDumps)
		}
		for _, child := range node.Children {
			schema.argument(child, inner)
		}
	}
}

// usageClaims returns a usage node's claims by the tables, with no known scope where its value names the kind of block
// it holds (limit = <triggers>).
func (schema *Schema) usageClaims(node *DocNode) Claims {
	claims := schema.claimsOf(node, game.SourceDumps)
	if node.Key != "" && node.Value != "" && claims.Shape.Source != game.SourceNone {
		claims.Scope.Set("", game.SourceDumps)
	}
	return claims
}
