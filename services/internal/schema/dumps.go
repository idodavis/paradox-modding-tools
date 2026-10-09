// dumps.go is step 2: the console dumps, or the shipped backup, parsed into commands, scopes, saved scopes,
// modifiers, on_actions, data types and custom loc.

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
func (s *Schema) dumpsOrShipped(input Input) {
	if shipped := input.Shipped; shipped != nil {
		s.Commands, s.Scopes, s.Saved = shipped.Commands, shipped.Scopes, shipped.Saved
		s.Modifiers, s.OnActions = shipped.Modifiers, shipped.OnActions
		s.DataTypes, s.CustomLoc = shipped.DataTypes, shipped.CustomLoc
		s.Sources[SourceDumps] = shipped.Sources[SourceDumps]
		return
	}
	written := s.parseDumps(input.Dumps)
	s.Sources[SourceDumps] = SourceState{From: "none"}
	if !written.IsZero() {
		s.Sources[SourceDumps] = SourceState{From: "yours", Written: written, Stale: written.Before(input.Updated)}
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
func (s *Schema) parseDumps(files []inventory.File) (written time.Time) {
	s.Commands, s.Scopes, s.Saved = map[string]*Command{}, map[string]Scope{}, map[string]bool{}
	s.Modifiers, s.OnActions = map[string]Modifier{}, map[string]OnAction{}
	s.DataTypes, s.CustomLoc = map[string]DataType{}, map[string]CustomLoc{}

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
			if info, err := os.Stat(file.Path); err == nil && (written.IsZero() || info.ModTime().Before(written)) {
				written = info.ModTime()
			}
			for _, e := range splitEntries(kind.entry, kind.labels, content) {
				switch kind.record {
				case recordScope:
					s.parseScope(e)
				case recordCommand:
					s.parseCommand(e, kinds[fileName], ranked, usages)
				case "Modifier":
					s.parseModifier(e)
				case "OnAction":
					s.parseOnAction(e)
				case "CustomLoc":
					s.parseCustomLoc(e)
				case "DataType":
					s.parseDataType(e)
				}
			}
		}
	}
	s.applyUsages(usages)
	return written
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
			if scriptShapes.code.MatchString(strings.TrimLeft(line, " \t")) {
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
func (s *Schema) parseScope(e dumpEntry) {
	s.Scopes[e.name] = Scope{
		Name:      e.name,
		Variables: e.fields["Scope.Variables"] != "no",
		Triggers:  e.fields["Scope.Triggers"] != "no",
		Effects:   e.fields["Scope.Effects"] != "no",
	}
}

// parseCommand reads an effect, trigger or link entry into the command of its name. A name written twice is
// one command that runs where either reading runs.
func (s *Schema) parseCommand(e dumpEntry, kind game.Kinds, ranked map[string]int, usages map[string][]string) {
	if e.group2 != "" { // the block closing the link dump, "Event Targets Saved from Code"
		for _, line := range e.lines {
			if line != "" {
				s.Saved[line] = true
			}
		}
		return
	}
	name := strings.ToLower(e.name) // script writes AND for and
	named := scopeList(e.fields["Command.Scopes"])
	command := s.Commands[name]
	switch {
	case command == nil:
		command = &Command{Scopes: named, Args: &Position{}}
		s.Commands[name] = command
	case len(named) == 0:
		command.Scopes = nil // a reading with no scope runs anywhere
	case len(command.Scopes) > 0:
		command.Scopes = slices.Compact(slices.Sorted(slices.Values(append(command.Scopes, named...))))
	}

	// a trigger or effect reading's blurb beats a plain link's, which beats a global link's
	reading := 2
	if kind != game.KindLink {
		reading = 0
	} else if e.fields["Command.Global"] != yes {
		reading = 1
	}
	if _, isRanked := ranked[name]; e.blurb != "" && (!isRanked || reading < ranked[name]) {
		command.Blurb, ranked[name] = e.blurb, reading
	}
	command.Kind |= kind
	command.Out = cmp.Or(command.Out, e.fields["Command.Out"])
	command.Global = command.Global || e.fields["Command.Global"] == yes
	command.Wild = command.Wild || e.fields["Command.Wild"] == yes

	if claim := s.valueFromTraits(command, name, e.fields["Command.Args.Value"]); claim.Kind != ExpectsUnknown || claim.Choices != nil {
		SetFirst(&command.Args.Value, claim, SourceDumps)
	}
	if strings.TrimSpace(e.usage) != "" {
		usages[name] = append(usages[name], e.usage)
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
// read as the usage name = <class>, which claims the reference itself.
func (s *Schema) valueFromTraits(command *Command, name, traits string) (claim Expected) {
	switch {
	case traits == "yes/no":
		claim.Kind = ExpectsBool
	case strings.HasSuffix(traits, " valid date"):
		claim.Kind = ExpectsDate
	case strings.HasPrefix(traits, "<"): // a comparison operator says no type
	case strings.HasSuffix(traits, " scope"), strings.HasSuffix(traits, " target"):
		claim.Kind = ExpectsScope
	case strings.HasPrefix(traits, "class C") && strings.HasSuffix(traits, "Database key"):
		class := strings.TrimSuffix(strings.TrimPrefix(traits, "class C"), "Database key")
		parsed := jomini.Parse(normalize(usageLines(name + " = <" + snakeCase(strings.TrimSpace(class)) + ">")))
		s.applyUsage(&parsed, command, name)
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
func (s *Schema) parseModifier(e dumpEntry) {
	if e.group2 != "" { // a --- section --- heading
		return
	}
	pattern := ""
	if spans := jomini.Interpolations(e.name); len(spans) > 0 {
		pattern = e.name
		for _, span := range slices.Backward(spans) {
			pattern = pattern[:span.Start] + "*" + pattern[span.End:]
		}
	}
	s.Modifiers[e.name] = Modifier{Pattern: pattern, Blurb: e.blurb}
}

// parseOnAction reads an on_action's entry.
func (s *Schema) parseOnAction(e dumpEntry) {
	s.OnActions[e.name] = OnAction{Scope: cmp.Or(scopeList(e.fields["OnAction.Scope"])...), Blurb: e.blurb}
}

// parseCustomLoc reads a custom localization's entry.
func (s *Schema) parseCustomLoc(e dumpEntry) {
	var entries []string
	for _, line := range e.lines {
		if line != "" {
			entries = append(entries, line)
		}
	}
	s.CustomLoc[e.name] = CustomLoc{
		Scope: cmp.Or(scopeList(e.fields["CustomLoc.Scope"])...), Parent: e.fields["CustomLoc.Parent"],
		Suffix: e.fields["CustomLoc.Suffix"], Entries: entries,
	}
}

// parseDataType reads a data type's entry, or one function of one; a function with no type is a global.
func (s *Schema) parseDataType(e dumpEntry) {
	owner, member := e.name, e.group2
	if member == "" && e.fields["DataType.Functions"] != "Type" {
		owner, member = game.GlobalType, owner
	}
	dataType := s.DataTypes[owner]
	if dataType.Functions == nil {
		dataType.Functions = map[string]DataFunction{}
	}
	if _, present := dataType.Functions[member]; member != "" && !present {
		dataType.Functions[member] = DataFunction{Returns: e.fields["DataFunction.Returns"]}
	}
	s.DataTypes[owner] = dataType
}

// placeholder matches a block placeholder in a usage.
var placeholder = regexp.MustCompile(game.UsagePlaceholder)

// applyUsages reads each command's usages into its arguments, once every command is declared. A command
// whose usage blocks write no block placeholder takes arguments only.
func (s *Schema) applyUsages(usages map[string][]string) {
	for _, name := range slices.Sorted(maps.Keys(usages)) {
		command := s.Commands[name]
		blocks, plain := 0, 0
		for _, usage := range usages[name] {
			parsed := jomini.Parse(normalize(usageLines(usage)))
			written := usageName(&parsed, name)
			s.applyUsage(&parsed, command, written)
			if written == "" && len(parsed.Root.Statements) > 0 {
				blocks++
				if !placeholder.MatchString(usage) {
					plain++
				}
				continue
			}
			found, plainBlocks := countBlocks(usage, written)
			blocks, plain = blocks+found, plain+plainBlocks
		}
		command.Lists = command.Out == "" && strings.Contains(command.Blurb, game.DumpWords.Iterates)
		if blocks > 0 && plain == blocks {
			SetFirst(&command.Args.Block, game.BlockArguments, SourceDumps)
		}
	}
}

// usageName returns the key of the first assignment, at any depth, named as the command; failing that the
// key of the one top statement when it is an assignment (a stale or sibling name); else "".
func usageName(parsed *jomini.Result, command string) (found string) {
	jomini.Walk(parsed.Root, func(statement jomini.Statement, _ int, _ *jomini.Block) bool {
		if assign, isAssign := statement.(*jomini.Assignment); isAssign && strings.EqualFold(assign.Key.Text, command) {
			found = assign.Key.Text
			return false
		}
		return true
	})
	if found == "" && len(parsed.Root.Statements) == 1 {
		if assign, isAssign := parsed.Root.Statements[0].(*jomini.Assignment); isAssign {
			found = assign.Key.Text
		}
	}
	return found
}

// countBlocks counts the blocks of a usage written under the command's name and how many of them write only
// arguments: no line is a block placeholder, and each is a statement or a brace.
func countBlocks(usage, written string) (blocks, plain int) {
	raw := jomini.Parse(usage)
	for _, statement := range raw.Root.Statements {
		assign, isAssign := statement.(*jomini.Assignment)
		block := jomini.ChildBlock(statement)
		if !isAssign || block == nil || !strings.EqualFold(assign.Key.Text, written) {
			continue
		}
		end := block.CloseBrace
		if end < 0 {
			end = len(usage)
		}
		onlyArguments := true
		for line := range strings.SplitSeq(usage[block.OpenBrace+1:end], "\n") {
			line, _, _ = strings.Cut(line, "#")
			onlyArguments = onlyArguments && !placeholder.MatchString(line) &&
				(strings.Trim(line, "{} \t") == "" || scriptShapes.statement.MatchString(strings.TrimSpace(line)))
		}
		blocks++
		if onlyArguments {
			plain++
		}
	}
	return blocks, plain
}
