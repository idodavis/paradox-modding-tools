// dumps.go is step 2: the console dumps, or the shipped backup, parsed into commands, scopes, saved scopes,
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

// Modifier is one modifier the dumps declare; Pattern is a name written with $TYPE$ parts as a glob, ""
// for a plain name.
type Modifier struct {
	Pattern, Blurb string
}

// OnAction is one on_action the dumps declare, with the scope it expects ("" when none is named).
type OnAction struct {
	Scope, Blurb string
}

// DataType is one type of the data system with its functions by name; "Global" holds the global ones.
type DataType struct {
	Functions map[string]DataFunction
}

// DataFunction is one function, promote or macro of a data type.
type DataFunction struct {
	Returns string
}

// CustomLoc is one custom localization: the scope it runs in and the keys it picks between.
type CustomLoc struct {
	Scope, Parent, Suffix string
	Entries               []string
}

// The kinds of dump record whose names the table and its reader share, and the dumps' word for true.
const (
	recordScope   = "Scope"
	recordCommand = "Command"
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
			"Usage": usageField, "usage": usageField, "Usages": usageField,
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

// dumpsOrShipped reads the dump files into the schema, or takes a shipped dump part whole. The dumps
// are stale when older than the install's last update.
func (schema *Schema) dumpsOrShipped(input Input) {
	if shipped := input.Shipped; shipped != nil {
		schema.Commands, schema.Scopes, schema.Saved = shipped.Commands, shipped.Scopes, shipped.Saved
		schema.Modifiers, schema.OnActions = shipped.Modifiers, shipped.OnActions
		schema.DataTypes, schema.CustomLoc = shipped.DataTypes, shipped.CustomLoc
		schema.Sources[SourceDumps] = shipped.Sources[SourceDumps]
		return
	}
	writtenAt := schema.parseDumps(input.Dumps)
	schema.Sources[SourceDumps] = SourceState{Origin: "none"}
	if !writtenAt.IsZero() {
		schema.Sources[SourceDumps] = SourceState{Origin: "yours", WrittenAt: writtenAt, Stale: writtenAt.Before(input.Updated)}
	}
}

// DumpFiles lists every file name the dumps are read from.
var DumpFiles = func() (names []string) {
	for _, kind := range dumpLabels {
		names = append(names, kind.files...)
	}
	return names
}()

// MissingDumps lists the dump files no file of files has, by base name.
func MissingDumps(files []inventory.File) []string {
	return slices.DeleteFunc(slices.Clone(DumpFiles), func(name string) bool {
		return slices.ContainsFunc(files, func(file inventory.File) bool { return filepath.Base(file.Path) == name })
	})
}

// a label line is "Label: value", the label maybe bold; a dashes line ends an entry
var labelLine, dashes = regexp.MustCompile(`^\s*\*{0,2}([^:*]+?)\*{0,2}: *(.*)$`), regexp.MustCompile(`(?m)^-{20,}$`)

// dumpEntry is one entry of a dump file: its name and second group, the labels it fills by field, its lines
// that fill none, its usage and its blurb.
type dumpEntry struct {
	name, group2 string
	fields       map[string]string
	lines        []string
	usage, blurb string
}

// parseDumps reads the dump files into the schema and returns when they were written: the dumps are as fresh
// as their oldest file. Usages are applied last, once every command is declared.
func (schema *Schema) parseDumps(files []inventory.File) (writtenAt time.Time) {
	schema.Commands, schema.Scopes, schema.Saved = map[string]*Command{}, map[string]ScopeType{}, map[string]bool{}
	schema.Modifiers, schema.OnActions = map[string]Modifier{}, map[string]OnAction{}
	schema.DataTypes, schema.CustomLoc = map[string]DataType{}, map[string]CustomLoc{}

	byName := map[string]inventory.File{}
	for _, file := range files {
		if _, seen := byName[filepath.Base(file.Path)]; !seen {
			byName[filepath.Base(file.Path)] = file
		}
	}
	kinds := map[string]game.Kinds{"effects.log": game.KindEffect, "triggers.log": game.KindTrigger, "event_targets.log": game.KindLink}
	ranked, usages := map[string]int{}, map[string][]string{} // the blurb's reading rank per command; usage texts per command
	for _, kind := range dumpLabels {
		for _, fileName := range kind.files {
			file, found := byName[fileName]
			content, err := file.Text()
			if !found || err != nil {
				continue
			}
			if info, err := os.Stat(file.Path); err == nil && (writtenAt.IsZero() || info.ModTime().Before(writtenAt)) {
				writtenAt = info.ModTime()
			}
			for _, e := range splitEntries(kind.entry, kind.labels, content) {
				switch kind.record {
				case recordScope:
					schema.parseScope(e)
				case recordCommand:
					schema.parseCommand(e, kinds[fileName], ranked, usages)
				case "Modifier":
					schema.parseModifier(e)
				case "OnAction":
					schema.parseOnAction(e)
				case "CustomLoc":
					schema.parseCustomLoc(e)
				case "DataType":
					schema.parseDataType(e)
				}
			}
		}
	}
	schema.mergeUsages(usages)
	return writtenAt
}

// splitEntries splits a dump file into its entries, each from its header to the next header or a dashes
// line, and sorts the lines of each into the labels' fields and the lines that fill none.
func splitEntries(header *regexp.Regexp, labels map[string]string, content string) (entries []dumpEntry) {
	headers := header.FindAllStringSubmatchIndex(content, -1)
	for index, at := range headers {
		end := len(content)
		if index+1 < len(headers) {
			end = headers[index+1][0]
		}
		body := content[at[1]:end]
		if cut := dashes.FindStringIndex(body); cut != nil {
			body = body[:cut[0]]
		}
		group := func(i int) string {
			if 2*i+1 >= len(at) || at[2*i] < 0 {
				return ""
			}
			return content[at[2*i]:at[2*i+1]]
		}
		entries = append(entries, parseEntryBody(body, group(1), group(2), labels))
	}
	return entries
}

// usageField is the field the labels that open a command's usage fill.
const usageField = "Command.Usage"

// knownLabel splits a "Label: value" line whose label the table lists into the field it fills and the value.
func knownLabel(line string, labels map[string]string) (field, value string, known bool) {
	label := labelLine.FindStringSubmatch(line)
	if label == nil {
		return "", "", false
	}
	field, known = labels[label[1]]
	return field, label[2], known
}

// parseEntryBody sorts an entry's lines into fields and description lines and cuts off its usage. The usage
// starts at its label or, when no label opens one, at the first line shaped like script.
func parseEntryBody(body, name, group2 string, labels map[string]string) dumpEntry {
	fields, lines, usageAt := map[string]string{}, []string{}, -1
	for line := range strings.SplitSeq(body, "\n") {
		line = strings.TrimRight(line, " \t\r")
		if field, value, known := knownLabel(line, labels); known {
			switch {
			case field != usageField:
				fields[field] = value
			case usageAt < 0:
				usageAt = len(lines)
				fallthrough
			default:
				if value != "" {
					lines = append(lines, value)
				}
			}
			continue
		}
		if usageAt < 0 {
			// the usage may start inside a description line, where it writes the command's own name as a statement
			// (add_opinion - Adds …, add_opinion = { … })
			if at := strings.Index(line, name+" ="); at > 0 && strings.ContainsAny(line[at-1:at], " ,.;:") {
				lines = append(lines, line[:at])
				line = line[at:]
			}
			if isDocCode(tokenizeDocLine(line), false) {
				usageAt = len(lines)
			}
		}
		lines = append(lines, line)
	}
	description, usage := lines, ""
	if usageAt >= 0 {
		description, usage = lines[:usageAt], strings.TrimRight(strings.Join(lines[usageAt:], "\n"), " \t\n")
	}
	blurb := strings.TrimSpace(strings.Join(append([]string{fields["Blurb"]}, description...), "\n"))
	return dumpEntry{name: name, group2: group2, fields: fields, lines: lines, usage: usage, blurb: blurb}
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
func (schema *Schema) parseCommand(entry dumpEntry, kind game.Kinds, ranked map[string]int, usages map[string][]string) {
	if entry.group2 != "" { // the block closing the link dump, "Event Targets Saved from Code"
		for _, line := range entry.lines {
			if line != "" {
				schema.Saved[line] = true
			}
		}
		return
	}
	name := strings.ToLower(entry.name) // script writes AND for and
	named := scopeList(entry.fields["Command.Scopes"])
	command := schema.Commands[name]
	switch {
	case command == nil:
		command = &Command{Scopes: named, Args: &Position{}}
		schema.Commands[name] = command
	case len(named) == 0:
		command.Scopes = nil // a reading with no scope runs anywhere
	case len(command.Scopes) > 0:
		command.Scopes = slices.Compact(slices.Sorted(slices.Values(append(command.Scopes, named...))))
	}

	// a trigger or effect reading's blurb beats a plain link's, which beats a global link's
	reading := 2
	if kind != game.KindLink {
		reading = 0
	} else if entry.fields["Command.Global"] != yes {
		reading = 1
	}
	if _, isRanked := ranked[name]; entry.blurb != "" && (!isRanked || reading < ranked[name]) {
		command.Blurb, ranked[name] = entry.blurb, reading
	}
	command.Kinds |= kind
	command.Target = cmp.Or(command.Target, entry.fields["Command.Out"])
	command.Global = command.Global || entry.fields["Command.Global"] == yes
	command.Wild = command.Wild || entry.fields["Command.Wild"] == yes

	if claim := schema.valueFromTraits(command, name, entry.fields["Command.Args.Value"]); claim.Type != ValueUnknown || claim.Choices != nil {
		command.Args.Value.Set(claim, SourceDumps)
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

// valueFromTraits reads what a command's own value is from the dump's Traits line. A database key trait is
// merged as the usage name = <class>, which claims the reference itself.
func (schema *Schema) valueFromTraits(command *Command, name, traits string) (claim Expected) {
	switch {
	case traits == "yes/no":
		claim.Type = ValueBool
	case strings.HasSuffix(traits, " valid date"):
		claim.Type = ValueDate
	case strings.HasPrefix(traits, "<"): // a comparison operator says no type
	case strings.HasSuffix(traits, " scope"), strings.HasSuffix(traits, " target"):
		claim.Type = ValueScope
	case strings.HasPrefix(traits, "class C") && strings.HasSuffix(traits, "Database key"):
		class := strings.TrimSuffix(strings.TrimPrefix(traits, "class C"), "Database key")
		schema.mergeUsage(parseDocScript([]string{name + " = <" + snakeCase(strings.TrimSpace(class)) + ">"}).Children[0], command)
	case strings.Contains(traits, ", "):
		list := traits
		if _, after, found := strings.Cut(traits, ": "); found {
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

// snakeCase turns a class name into snake_case: TenetType is tenet_type.
func snakeCase(class string) string {
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
	if spans := jomini.Interpolations(entry.name); len(spans) > 0 {
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

// mergeUsages merges each command's usages into its arguments, once every command is declared, then decides
// from them whether the command iterates a list and whether its block takes arguments only: every block the
// usages write under the command's name holds arguments only.
func (schema *Schema) mergeUsages(usages map[string][]string) {
	for _, name := range slices.Sorted(maps.Keys(usages)) {
		command := schema.Commands[name]
		blocks, plain := 0, 0
		for _, usage := range usages[name] {
			tree := parseDocScript(strings.Split(usage, "\n"))
			for _, root := range usageRoots(schema, tree, name) {
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
			command.Args.Holds.Set(game.ShapeArguments, SourceDumps)
		}
	}
}

// usageRoots are the nodes of a usage that stand for the command: every node named as the command, at any depth
// (add_character_flag = X and add_character_flag = { flag = X }); else the one top statement, as an alias family
// writes it under a sibling's name (add_loyalists' usage writes add_radicals), unless it is named as no declared
// command and holds commands, which makes it a script body and no usage (Vic3's generated iterators carry an
// unrelated scripted trigger); else the tree itself, whose top statements are the arguments.
func usageRoots(schema *Schema, tree *DocNode, name string) (roots []*DocNode) {
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

// blockPlaceholder matches a placeholder standing for a block of triggers or effects in a usage.
var blockPlaceholder = regexp.MustCompile(`<[^<>]*(?i:trigger|effect)[^<>]*>|(?i:triggers?|effects?)\.\.\.`)

// mergeUsage merges a node standing for a command into the command's arguments: a named node's own claims are
// the arguments position's, and each statement in its block is an argument.
func (schema *Schema) mergeUsage(root *DocNode, command *Command) {
	if root.Key != "" {
		command.Args.declare(schema.usageClaims(root), SourceDumps)
	}
	for _, child := range root.Children {
		// a usage is written in no database: an empty one stands for the entry its arguments are read in
		schema.argument(child, Context{At: command.Args, Shape: game.ShapeFields, Database: &Database{}, Entry: -1})
	}
}

// argument merges one statement of a usage's block as an argument at a context: its position is made on write
// where the walker reads its key, and gets the statement's claims unless the key is a declared command; its own
// block's statements are arguments below it. A bare value's claims are the holder's (<optional> after an argument).
func (schema *Schema) argument(node *DocNode, context Context) {
	if node.Key == "" {
		if context.At != nil {
			context.At.declare(schema.usageClaims(node), SourceDumps)
		}
		for _, child := range node.Children { // a block with no key reads in place
			schema.argument(child, context)
		}
		return
	}
	for _, name := range keyNames(node.Key) {
		inner := schema.Enter(context, &jomini.Assignment{Key: jomini.Scalar{Text: name}}, nil, true)
		if inner.Position() != nil && inner.Rule != game.KeyRuleCommand && inner.Rule != game.KeyRuleLink {
			inner.Position().declare(schema.usageClaims(node), SourceDumps)
		}
		for _, child := range node.Children {
			schema.argument(child, inner)
		}
	}
}

// usageClaims is a usage node's claims: its claims by the tables, and no known scope where its value names the
// kind of block it holds (limit = <triggers>).
func (schema *Schema) usageClaims(node *DocNode) Claims {
	claims := schema.claimsOf(node, SourceDumps)
	if node.Key != "" && node.Value != "" && claims.Holds.Source != SourceNone {
		claims.Scope.Set("", SourceDumps)
	}
	return claims
}
