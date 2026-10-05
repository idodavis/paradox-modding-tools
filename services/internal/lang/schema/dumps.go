// dumps.go reads the games' console dumps (script_docs and dump_data_types): the label table says
// how each file's entries start and which field each label fills, and readDumps fills the schema.

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
	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/source"
)

// Modifier is one modifier the dumps declare; Pattern is a name written with $TYPE$ parts as a
// glob, "" for a plain name.
type Modifier struct {
	Pattern, Blurb string
}

// OnAction is one on_action the dumps declare, with the scope it expects ("" when none is named).
type OnAction struct {
	Scope, Blurb string
}

// DataType is one type of the data system with its functions by name; "Global" holds the global
// ones.
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

// dumpLabels is the label table, one row per kind of dump file in reading order: the record, files,
// entry header (group 1 names it) and each label with the field it fills ("" kept nowhere).
var dumpLabels = []struct {
	record string
	files  []string
	entry  *regexp.Regexp
	labels map[string]string
}{
	{
		record: "Scope",
		files:  []string{"event_scopes.log"},
		entry:  regexp.MustCompile(`(?m)^(\S+):$`),
		labels: map[string]string{
			"Save Token": "", "Evaluate Triggers": "Scope.Triggers", "Execute Effects": "Scope.Effects",
			"Change Scopes": "", "Stores Variables": "Scope.Variables",
		},
	},
	{
		record: "Command",
		files:  []string{"event_targets.log"},
		entry:  regexp.MustCompile(`(?m)^(?:(?:-{20}\n\n|### )(\S+)(?: - |$)|(Event Targets Saved from Code):$)`),
		labels: map[string]string{
			"Input Scopes": "Command.Scopes", "Output Scopes": "Command.Out", "Requires Data": "",
			"Global Link": "Command.Global", "Wild Card": "Command.Wild",
		},
	},
	{
		record: "Command",
		files:  []string{"effects.log", "triggers.log"},
		entry:  regexp.MustCompile(`(?m)^(?:-{20}\n\n|## )(\S+)(?: - |$)`),
		labels: map[string]string{
			"Supported Scopes": "Command.Scopes", "Supported Targets": "Command.Out", "Traits": "Command.Args.Value",
			"Usage": "Command.Usage", "usage": "Command.Usage", "Usages": "Command.Usage",
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
		// groups: the type, or a global's name; the function; its arguments. "Definition type" files
		// the entry among its type's functions, or declares the type when it says Type.
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

// DumpFiles is every file name the dumps are read from.
var DumpFiles = func() (names []string) {
	for _, kind := range dumpLabels {
		names = append(names, kind.files...)
	}
	return names
}()

// MissingDumps is the names of DumpFiles no file of files has.
func MissingDumps(files []source.File) []string {
	return slices.DeleteFunc(slices.Clone(DumpFiles), func(name string) bool {
		return slices.ContainsFunc(files, func(file source.File) bool { return filepath.Base(file.Path) == name })
	})
}

// a label line is "Label: value", the label maybe bold; a dashes line ends an entry
var labelLine, dashes = regexp.MustCompile(`^\s*\*{0,2}([^:*]+?)\*{0,2}: *(.*)$`), regexp.MustCompile(`(?m)^-{20,}$`)

// readDumps reads the dump files into the schema: commands (with usages through template, stated by
// SourceDumps) and the flat tables. It returns the oldest file's time, zero when none was read.
func (schema *Schema) readDumps(files []source.File) (written time.Time) {
	schema.Commands, schema.Scopes, schema.Saved = map[string]*Command{}, map[string]Scope{}, map[string]bool{}
	schema.Modifiers, schema.OnActions = map[string]Modifier{}, map[string]OnAction{}
	schema.DataTypes, schema.CustomLoc = map[string]DataType{}, map[string]CustomLoc{}

	byName := map[string]source.File{}
	for _, file := range files {
		if _, seen := byName[filepath.Base(file.Path)]; !seen {
			byName[filepath.Base(file.Path)] = file
		}
	}

	kinds := map[string]game.Kinds{"effects.log": game.KindEffect, "triggers.log": game.KindTrigger, "event_targets.log": game.KindLink}
	ranked := map[string]int{}
	usages := map[string][]string{}

	// a scope list names scopes, its no-scope words aside
	scopes := func(list string) (named []string) {
		for name := range strings.SplitSeq(list, ",") {
			if name = strings.TrimSpace(name); name != "" && !slices.Contains(game.DumpWords.NoScope, name) {
				named = append(named, name)
			}
		}
		return named
	}

	for _, kind := range dumpLabels {
		for _, fileName := range kind.files {
			content, err := byName[fileName].Text()
			if err != nil {
				continue
			}
			// the dumps are as fresh as their oldest file: dump_data_types runs apart from script_docs
			if info, err := os.Stat(byName[fileName].Path); err == nil && (written.IsZero() || info.ModTime().Before(written)) {
				written = info.ModTime()
			}

			// an entry runs from its header to the next header or a dashes line
			headers := kind.entry.FindAllStringSubmatchIndex(content, -1)
			for i, header := range headers {
				end := len(content)
				if i+1 < len(headers) {
					end = headers[i+1][0]
				}
				body := content[header[1]:end]
				if cut := dashes.FindStringIndex(body); cut != nil {
					body = body[:cut[0]]
				}
				group := func(index int) string {
					if 2*index >= len(header) || header[2*index] < 0 {
						return ""
					}
					return content[header[2*index]:header[2*index+1]]
				}

				// a line is a label or text; the text is description until the usage starts, at a Usage
				// label or the first line shaped like script
				fields, lines, usageAt := map[string]string{}, []string(nil), -1
				for _, line := range strings.Split(body, "\n") {
					line = strings.TrimRight(line, " \t\r")
					label := labelLine.FindStringSubmatch(line)
					field, known := "", false
					if label != nil {
						field, known = kind.labels[label[1]]
					}
					switch {
					case known && field == "Command.Usage":
						if usageAt < 0 {
							usageAt = len(lines)
						}
						if label[2] != "" {
							lines = append(lines, label[2])
						}
					case known:
						fields[field] = label[2]
					default:
						// the usage may start inside a description line, where it writes the command's own name
						// as a statement (add_opinion - Adds …, add_opinion = { … })
						if at := strings.Index(line, group(1)+" ="); usageAt < 0 && at > 0 && strings.ContainsRune(" ,.;:", rune(line[at-1])) {
							lines, line = append(lines, line[:at]), line[at:]
						}
						if usageAt < 0 && islands.code.MatchString(strings.TrimLeft(line, " \t")) {
							usageAt = len(lines)
						}
						lines = append(lines, line)
					}
				}
				description, usage := lines, ""
				if usageAt >= 0 {
					description, usage = lines[:usageAt], strings.TrimRight(strings.Join(lines[usageAt:], "\n"), " \t\n")
				}
				blurb := strings.TrimSpace(strings.Join(append([]string{fields["Blurb"]}, description...), "\n"))

				// the record is filled from its fields
				entryName := group(1)
				switch kind.record {
				case "Scope":
					schema.Scopes[entryName] = Scope{Name: entryName, Variables: fields["Scope.Variables"] != "no",
						Triggers: fields["Scope.Triggers"] != "no", Effects: fields["Scope.Effects"] != "no"}

				case "Command":
					// the block closing the link dump lists the scopes the game saves from code
					if group(2) != "" {
						for _, name := range lines {
							if name != "" {
								schema.Saved[name] = true
							}
						}
						continue
					}

					// a name written twice is one command: it runs where either reading runs, and a reading
					// with no scope runs anywhere. Names are lower-case: script writes AND for and
					entryName = strings.ToLower(entryName)
					command, named := schema.Commands[entryName], scopes(fields["Command.Scopes"])
					switch {
					case command == nil:
						command = &Command{Scopes: named, Args: &Key{}}
						schema.Commands[entryName] = command
					case len(named) == 0:
						command.Scopes = nil
					case len(command.Scopes) > 0:
						command.Scopes = slices.Compact(slices.Sorted(slices.Values(append(command.Scopes, named...))))
					}
					// its blurb is its trigger or effect reading's, else a plain link's, else a global link's
					reading := 2
					switch {
					case kinds[fileName] != game.KindLink:
						reading = 0
					case fields["Command.Global"] != "yes":
						reading = 1
					}
					if best, seen := ranked[entryName]; blurb != "" && (!seen || reading < best) {
						command.Blurb, ranked[entryName] = blurb, reading
					}
					command.Kind |= kinds[fileName]
					command.Out = cmp.Or(command.Out, fields["Command.Out"])
					command.Global = command.Global || fields["Command.Global"] == "yes"
					command.Wild = command.Wild || fields["Command.Wild"] == "yes"

					// a Traits value says what the command's own value is; comparison operators say no type
					claim, traits := Value{}, fields["Command.Args.Value"]
					switch {
					case traits == "yes/no":
						claim.Type = WordBool
					case strings.HasSuffix(traits, " valid date"):
						claim.Type = WordDate
					case strings.HasPrefix(traits, "<"):
					case strings.HasSuffix(traits, " scope"), strings.HasSuffix(traits, " target"):
						claim.Type = WordScope
					case strings.HasPrefix(traits, "class C") && strings.HasSuffix(traits, "Database key"):
						// class CXDatabase key is read as the usage `command = <x>`, X in snake_case (TenetType
						// → tenet_type), x naming a database (Named)
						var word []rune
						for i, char := range strings.TrimSuffix(strings.TrimPrefix(traits, "class C"), "Database key") {
							if i > 0 && unicode.IsUpper(char) {
								word = append(word, '_')
							}
							word = append(word, unicode.ToLower(char))
						}
						parsed := jomini.Parse(normalize(usageLines(entryName + " = <" + string(word) + ">")))
						schema.usage(&parsed, command, entryName)
					case strings.Contains(traits, ", "):
						list := traits
						if _, after, prefixed := strings.Cut(traits, ": "); prefixed {
							list = after
						}
						for _, word := range strings.Split(list, ", ") {
							claim.Words = append(claim.Words, strings.TrimPrefix(word, "and "))
						}
						slices.Sort(claim.Words)
						claim.Words = slices.Compact(claim.Words)
					}
					if claim.Type != WordUnknown || claim.Words != nil {
						Set(&command.Args.Value, claim, SourceDumps)
					}
					if strings.TrimSpace(usage) != "" {
						usages[entryName] = append(usages[entryName], usage)
					}

				case "Modifier":
					// a section heading (--- name ---) is no modifier
					if group(2) != "" {
						continue
					}
					// a name with $TYPE$ parts names a family, matched as a glob
					pattern, last := "", 0
					for _, part := range jomini.Interpolations(entryName) {
						pattern, last = pattern+entryName[last:part.Start]+"*", part.End
					}
					if last > 0 {
						pattern += entryName[last:]
					}
					schema.Modifiers[entryName] = Modifier{Pattern: pattern, Blurb: blurb}

				case "OnAction":
					schema.OnActions[entryName] = OnAction{Scope: cmp.Or(scopes(fields["OnAction.Scope"])...), Blurb: blurb}

				case "CustomLoc":
					schema.CustomLoc[entryName] = CustomLoc{
						Scope: cmp.Or(scopes(fields["CustomLoc.Scope"])...), Parent: fields["CustomLoc.Parent"],
						Suffix:  fields["CustomLoc.Suffix"],
						Entries: slices.DeleteFunc(lines, func(line string) bool { return line == "" }),
					}

				case "DataType":
					owner, member := group(1), group(2)
					if member == "" && fields["DataType.Functions"] != "Type" {
						owner, member = game.GlobalType, owner
					}
					dataType := schema.DataTypes[owner]
					if dataType.Functions == nil {
						dataType.Functions = map[string]DataFunction{}
					}
					if _, seen := dataType.Functions[member]; member != "" && !seen {
						dataType.Functions[member] = DataFunction{Returns: fields["DataFunction.Returns"]}
					}
					schema.DataTypes[owner] = dataType
				}
			}
		}
	}

	// usages are read once every command is declared: the statement named after the command at any
	// depth, else the one statement (a stale or sibling name); several, none so named, list arguments
	placeholder := regexp.MustCompile(game.UsagePlaceholder)
	for _, name := range slices.Sorted(maps.Keys(schema.Commands)) {
		command, blocks, plain := schema.Commands[name], 0, 0
		for _, usage := range usages[name] {
			parsed, usageName := jomini.Parse(normalize(usageLines(usage))), ""
			jomini.Walk(parsed.Root, func(statement jomini.Statement, _ int, _ *jomini.Block) bool {
				if assign, isAssign := statement.(*jomini.Assignment); isAssign && strings.EqualFold(assign.Key.Text, name) {
					usageName = assign.Key.Text
				}
				return usageName == ""
			})
			if usageName == "" && len(parsed.Root.Statements) == 1 {
				if first, isAssign := parsed.Root.Statements[0].(*jomini.Assignment); isAssign {
					usageName = first.Key.Text
				}
			}
			schema.usage(&parsed, command, usageName)

			// a usage block is plain when every line, comments aside, is key = value with no
			// placeholder; a usage that lists the arguments is one block
			if usageName == "" && len(parsed.Root.Statements) > 0 {
				blocks++
				if !placeholder.MatchString(usage) {
					plain++
				}
			}
			raw := jomini.Parse(usage)
			for _, statement := range raw.Root.Statements {
				assign, isAssign := statement.(*jomini.Assignment)
				block := jomini.ChildBlock(statement)
				if !isAssign || block == nil || !strings.EqualFold(assign.Key.Text, usageName) {
					continue
				}
				end, lines := block.CloseBrace, true
				if end < 0 {
					end = len(usage)
				}
				for _, line := range strings.Split(usage[block.OpenBrace+1:end], "\n") {
					line, _, _ = strings.Cut(line, "#")
					lines = lines && !placeholder.MatchString(line) &&
						(strings.Trim(line, "{} \t") == "" || islands.statement.MatchString(strings.TrimSpace(line)))
				}
				blocks++
				if lines {
					plain++
				}
			}
		}

		command.Lists = command.Out == "" && strings.Contains(command.Blurb, game.DumpWords.Iterates)

		// a usage whose blocks write no block placeholder takes arguments only
		if blocks > 0 && plain == blocks {
			Set(&command.Args.Block, game.BlockArguments, SourceDumps)
		}
	}
	return written
}
