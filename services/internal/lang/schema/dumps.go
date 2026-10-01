// dumps.go reads what a game declares about its own language from the files two console commands
// write: script_docs's seven dumps (dumpFiles) and dump_data_types's data_types_*.txt. Their three
// layouts (markdown headings, blocks between dashed rules, bare headers) are told apart by the
// file, never by the game. Any part may be absent: a user who has not run the commands gets an
// empty Dumps, which every reader must handle.
package schema

import (
	"cmp"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"paradox-modding-tools/services/internal/lang/jomini"
)

// Dumps is what the game declares about its own language. It is never nil; any part may be empty.
type Dumps struct {
	Effects            map[string]Command
	Triggers           map[string]Command
	Modifiers          map[string]Modifier           // by name; a CK3 name with $TYPE$ stands for one per definition of TYPE
	EventTargets       map[string]EventTarget        // the event targets written alone, each a hop from its input scopes: dynasty = { }
	DataTargets        map[string]EventTarget        // the event targets written name:key, key required: dynasty:x
	Scopes             map[string]Scope              // every scope type event_scopes.log lists, none included
	OnActions          map[string]OnAction           // every on_action the game declares
	CustomLocalization map[string]CustomLocalization // every custom localization, by the name localization calls it by
	DataTypes          map[string]DataType           // each type of a data call, by name
	Globals            map[string]DataFunction       // what a data call may start with: GetPlayer, GetVariableSystem
	SavedScopes        []string                      // the scope names the engine saves itself: actor, recipient
	Unknown            []Tally                       // labels and forms the reader did not know, for the unread report
	ScriptDocsWritten  time.Time                     // when script_docs last wrote effects.log; zero with none
	DataTypesWritten   time.Time                     // when dump_data_types last wrote its newest file; zero with none

	families     []string // the modifiers named with $TYPE$, gathered once
	familiesOnce sync.Once
}

// IsScope reports a scope type a block may run in: one where triggers are evaluated or effects
// run. An event target or command whose output is anything else (value, boolean, flag) leads to no
// scope.
func (dumps *Dumps) IsScope(name string) bool {
	scope := dumps.Scopes[name]
	return scope.Triggers || scope.Effects
}

// Modifier is the modifier of that name: declared as written, else by a family whose $TYPE$ stands
// for a name-shaped run (CK3 $CULTURE$_opinion answers norse_opinion).
func (dumps *Dumps) Modifier(name string) (Modifier, bool) {
	if modifier, ok := dumps.Modifiers[name]; ok {
		return modifier, true
	}
	dumps.familiesOnce.Do(func() {
		for family := range dumps.Modifiers {
			if open, close := strings.IndexByte(family, '$'), strings.LastIndexByte(family, '$'); open >= 0 && open != close {
				dumps.families = append(dumps.families, family)
			}
		}
		slices.Sort(dumps.families)
	})
	for _, family := range dumps.families {
		modifier := dumps.Modifiers[family]
		open, close := strings.IndexByte(family, '$'), strings.LastIndexByte(family, '$')
		prefix, suffix := family[:open], family[close+1:]
		if len(name) <= len(prefix)+len(suffix) || !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, suffix) {
			continue
		}
		if run := name[len(prefix) : len(name)-len(suffix)]; !strings.ContainsFunc(run, func(char rune) bool {
			return !(char == '_' || unicode.IsLower(char) || unicode.IsDigit(char))
		}) {
			return modifier, true
		}
	}
	return Modifier{}, false
}

// Command is one effect or trigger the game declares.
type Command struct {
	Name  string
	Doc   string   // the dump's description
	Usage string   // the dump's signature or example, line breaks kept
	In    []string // Supported Scopes: the scope types it may be written in
	Out   string   // Supported Targets: the scope an iterator's block runs in, or the type of value it takes
	Call  string   // the call form its usage offers besides the statement: add_estate_satisfaction_utility(<estate>|<amount>)
	Block *Key     // what is written after it, from its usage and Traits; nil where they declare nothing
}

// EventTarget is one reading of a word of the event target dump.
type EventTarget struct {
	Name   string
	Doc    string
	In     []string // Input Scopes: the scope types it may be written in; none for a global event target
	Out    []string // Output Scopes: what it leads to; a hop only when that is one scope the dumps declare
	Global bool     // Global Link: it may be written in any scope
	Wild   bool     // Wild Card: a pattern (value: any numeric literal), which is no hop between scopes
}

// Scope is one scope type of event_scopes.log and what script may do in it.
type Scope struct {
	Triggers  bool   // Evaluate Triggers
	Effects   bool   // Execute Effects
	Hops      bool   // Change Scopes: event targets lead on from it
	SaveToken string // Save Token: the word it is saved as; none where it cannot be saved
	Variables bool   // Stores Variables
}

// OnAction is one on_action the game declares.
type OnAction struct {
	Scope    string // Expected Scope: the scope type it fires in; none where it has no root
	FromCode bool   // From Code: the game fires it itself, and not only script
}

// CustomLocalization is one custom localization: text chosen by triggers, called from localization.
type CustomLocalization struct {
	Scope   string // the scope type it is called on; all where any
	Random  bool   // Random Valid: of several texts whose triggers hold, one is picked at random
	Parent  string // the custom localization whose texts this one shows, each with Suffix added
	Suffix  string
	Entries []string // the localization keys it may show
}

// Modifier is one modifier the game declares.
type Modifier struct {
	Name       string
	Label      string   // Vic3's Name: the modifier as the player reads it, markup kept
	Doc        string   // the dump's description, where it gives one
	Categories []string // where the dump says it applies, lower-cased: character, county, country
	Section    string   // Vic3: static, dynamic or potential dynamic, from the heading it is listed under
}

// DataType is one type of the data-type dump: its own description and its functions and promotes.
type DataType struct {
	Doc       string
	Functions map[string]DataFunction
}

// DataFunction is one function, promote or macro of the data-type dump.
type DataFunction struct {
	Name    string
	Args    []string // the arguments its header names: Arg0, Arg1
	Doc     string   // Description
	Promote bool     // it returns an object a call walks on from; a function returns a value
	Returns string   // Return type: a type, a value's type, or [unregistered] for a concept or alias
	Macro   string   // Macro replacement: the call a global macro stands for
}

// dumpBlock is one entry of a dump: its header and the lines below it.
type dumpBlock struct {
	name    string
	tail    string   // what follows the name on the header line: a description, or a modifier's categories
	section string   // the heading the entry is listed under, in a bare dump that has headings
	body    []string // the lines below the header
}

// dumpFiles is the seven files script_docs writes, under the same names in every game.
var dumpFiles = []string{"effects.log", "triggers.log", "event_targets.log", "event_scopes.log", "modifiers.log", "on_actions.log", "custom_localization.log"}

// dumpLabels is each dump file's labels as the games spell them, ** removed; any other line is prose.
var dumpLabels = map[string][]string{
	"effects.log":             {"Supported Scopes", "Supported Targets", "Traits", "Usage", "usage", "Usages"},
	"triggers.log":            {"Supported Scopes", "Supported Targets", "Traits", "Usage", "usage", "Usages"},
	"event_targets.log":       {"Input Scopes", "Output Scopes", "Requires Data", "Global Link", "Wild Card"},
	"event_scopes.log":        {"Evaluate Triggers", "Execute Effects", "Change Scopes", "Save Token", "Stores Variables"},
	"modifiers.log":           {"Tag", "Use areas", "Categories", "Mask", "Name", "Description", "Extra info"},
	"on_actions.log":          {"Expected Scope", "From Code"},
	"custom_localization.log": {"Name", "Scope", "Random Valid", "Parent", "Suffix", "Entries"},
	"data_types":              {"Macro replacement", "Description", "Definition type", "Return type"},
}

// nonLabels are the lines the dumps write under every entry that say nothing of it.
var nonLabels = []string{"Reads gamestate for all scopes.", "Reads nothing for all scopes."}

// iteratorPrefixes are the four spellings of one iterator, which document each other unevenly.
var iteratorPrefixes = []string{"any_", "every_", "random_", "ordered_"}

// The forms the dumps write around usage: a statement's start, an optional annotation, a closing
// brace glued to prose (}simplified: x), a data-type header, and icon markup (\x16name!).
var (
	statementRe = regexp.MustCompile(`(?:^|[\s,])((?:[A-Za-z_][\w.:/|$@]*|"[^"]*"|<[^<>]+>)\s*(?:\?=|<=|>=|!=|=|<|>))`)
	optionalRe  = regexp.MustCompile(`\(optional(?:,\s*default\s*=?\s*([^)\s]*))?\)`)
	gluedProse  = regexp.MustCompile(`\}([A-Za-z]\w*:)\s`)
	headerRe    = regexp.MustCompile(`^[A-Za-z_]\w*(?:\.[A-Za-z_]\w*)?(?:\(\s*[\w,\s]*\))?$`)
	iconRe      = regexp.MustCompile("\x16([\\w.]+)!")
	numberKey   = regexp.MustCompile(`^(?:\d+|[A-Z]\d+)$`)
)

// ReadDumps reads the dumps in dirs, the folders the registry lists for the game; a missing dump
// stays empty, and of one file in two folders the newer is read.
func ReadDumps(dirs []string) *Dumps {
	dumps := &Dumps{
		Effects: map[string]Command{}, Triggers: map[string]Command{}, Modifiers: map[string]Modifier{}, EventTargets: map[string]EventTarget{}, DataTargets: map[string]EventTarget{},
		Scopes: map[string]Scope{}, OnActions: map[string]OnAction{}, CustomLocalization: map[string]CustomLocalization{}, DataTypes: map[string]DataType{}, Globals: map[string]DataFunction{},
	}

	// the newest copy of each dump file
	type found struct {
		path    string
		written time.Time
	}
	files := map[string]found{}
	for _, dir := range dirs {
		entries, _ := os.ReadDir(dir)
		for _, entry := range entries {
			file := strings.ToLower(entry.Name())
			info, err := entry.Info()
			isDataTypes := strings.HasPrefix(file, "data_types_") && strings.HasSuffix(file, ".txt")
			if err != nil || !isDataTypes && !slices.Contains(dumpFiles, file) {
				continue
			}
			if had, ok := files[file]; !ok || info.ModTime().After(had.written) {
				files[file] = found{path: filepath.Join(dir, entry.Name()), written: info.ModTime()}
			}
		}
	}

	unknown := map[string]int{}
	filters := map[string][]string{}
	for _, file := range slices.Sorted(maps.Keys(files)) {
		raw, err := os.ReadFile(files[file].path)
		if err != nil {
			continue
		}
		labelSet := file
		if strings.HasPrefix(file, "data_types_") {
			labelSet = "data_types"
			if files[file].written.After(dumps.DataTypesWritten) {
				dumps.DataTypesWritten = files[file].written
			}
		} else if file == "effects.log" {
			dumps.ScriptDocsWritten = files[file].written
		}

		blocks, saved := dumpBlocks(string(raw))
		if len(blocks) == 0 && strings.TrimSpace(string(raw)) != "" {
			unknown["layout: "+file]++
		}
		dumps.SavedScopes = append(dumps.SavedScopes, saved...)
		for _, block := range blocks {
			fields := dumpFields(block, labelSet, unknown)
			labels := fields.labels
			command := Command{Name: block.name, Doc: fields.doc, Usage: fields.usage, In: commaList(labels["supported scopes"]), Out: labels["supported targets"], Call: fields.call}
			switch {
			case labelSet == "data_types":
				dumps.dataCall(block.name, labels, unknown)
			case file == "effects.log":
				dumps.Effects[block.name], filters[block.name] = command, fields.filters
			case file == "triggers.log":
				if takes := traitTakes(labels["traits"], unknown); len(takes) > 0 {
					command.Block = &Key{Name: block.name, Source: SourceDumps, Written: WrittenWord, Takes: takes}
				}
				dumps.Triggers[block.name], filters[block.name] = command, fields.filters
			case file == "event_targets.log":
				// a word may be declared twice, once written name:key and once alone: two readings
				targets := dumps.EventTargets
				if labels["requires data"] == "yes" {
					targets = dumps.DataTargets
				}
				targets[block.name] = EventTarget{Name: block.name, Doc: fields.doc, In: commaList(labels["input scopes"]), Out: commaList(labels["output scopes"]), Global: labels["global link"] == "yes", Wild: labels["wild card"] == "yes"}
			case file == "event_scopes.log":
				dumps.Scopes[block.name] = Scope{
					Triggers: labels["evaluate triggers"] == "yes", Effects: labels["execute effects"] == "yes", Hops: labels["change scopes"] == "yes",
					SaveToken: labels["save token"], Variables: labels["stores variables"] == "yes",
				}
			case file == "modifiers.log":
				dumps.Modifiers[block.name] = Modifier{
					Name: block.name, Label: labels["name"], Doc: strings.TrimSpace(labels["description"] + "\n" + labels["extra info"]), Section: block.section,
					Categories: areas(labels["mask"] + "," + labels["use areas"] + "," + labels["categories"]),
				}
			case file == "on_actions.log":
				dumps.OnActions[block.name] = OnAction{Scope: labels["expected scope"], FromCode: strings.EqualFold(labels["from code"], "yes")}
			case file == "custom_localization.log":
				// the entries are the bare lines below Entries:, which read as prose
				dumps.CustomLocalization[block.name] = CustomLocalization{Scope: labels["scope"], Random: labels["random valid"] == "yes", Parent: labels["parent"], Suffix: labels["suffix"], Entries: strings.Fields(fields.doc)}
			}
		}
	}

	// usage is read once every command is known, since a declared command after <effects> is an
	// example statement; a trigger's block holds no effects (any_ruler shares every_ruler's example)
	for _, commands := range []map[string]Command{dumps.Effects, dumps.Triggers} {
		for name, command := range commands {
			block, foreign := dumps.commandKey(name, command.Usage, filters[name])
			if foreign && command.Doc == "" {
				// another definition's text under no words of its own (Vic3's geographic iterators):
				// the targets alone stay
				command.Usage = ""
			}
			_, isTrigger := dumps.Triggers[name]
			if block != nil && isTrigger && block.Block != BlockTriggers {
				block.Block = BlockNone
			}
			switch {
			case block == nil:
				block = command.Block
			case command.Block != nil:
				block.Takes = command.Block.Takes
			}
			command.Block = block
			commands[name] = command
		}
	}

	dumps.Unknown = tallies(unknown)
	return dumps
}

// dumpBlocks splits a dump into its entries, in the layout the file first shows. The event target
// dump ends with the scope names the engine saves itself, below "Saved from Code"; those come back
// as saved. Control bytes go: icon markup becomes the icon's name, NBSP and BEL a space.
func dumpBlocks(text string) (blocks []dumpBlock, saved []string) {
	text = strings.ToValidUTF8(strings.ReplaceAll(strings.TrimPrefix(text, "\xef\xbb\xbf"), "\r", ""), string(utf8.RuneError))
	text = strings.NewReplacer(" ", " ", "\a", " ").Replace(iconRe.ReplaceAllString(text, "$1"))
	text = strings.Map(func(char rune) rune {
		if char < ' ' && char != '\n' && char != '\t' {
			return -1
		}
		return char
	}, text)
	if at := strings.Index(text, "Saved from Code:"); at >= 0 {
		saved = strings.Fields(text[at+len("Saved from Code:"):])
		text = text[:strings.LastIndexByte(text[:at], '\n')+1]
	}
	lines := strings.Split(text, "\n")
	layout := "bare"
	for _, line := range lines {
		if strings.HasPrefix(line, "## ") || strings.HasPrefix(line, "### ") {
			layout = "markdown"
			break
		}
		if strings.HasPrefix(line, "----") {
			layout = "classic"
			break
		}
	}

	// a data-type dump opens on its first entry, with no rule before it
	ruled, section := true, ""
	for _, line := range lines {
		name, tail, isHeader := "", "", false
		if strings.HasPrefix(line, "----") {
			ruled = true
			continue
		}

		switch layout {
		case "markdown":
			// ## name or ### name, never indented: a quoted body below a heading may hold # comments
			if rest := strings.TrimLeft(line, "#"); strings.HasPrefix(line, "##") && strings.HasPrefix(rest, " ") {
				name, isHeader = rest, true
			}
		case "classic":
			// the first line after a rule: name - description, or name:, but not the file's banner
			if ruled && strings.TrimSpace(line) != "" {
				ruled, isHeader = false, !strings.HasSuffix(strings.TrimSpace(line), "Documentation:")
				name, tail, _ = strings.Cut(line, " - ")
				name = strings.TrimSuffix(strings.TrimSpace(name), ":")
			}
		case "bare":
			// Tag: name, Categories: a, b, or an unindented name: alone, so a body's label starts
			// nothing; --- Static modifier types --- heads the entries below it
			if heading, ok := strings.CutPrefix(line, "--- "); ok {
				section = strings.TrimSuffix(strings.ToLower(strings.TrimSuffix(heading, " ---")), " modifier types")
			} else if rest, ok := strings.CutPrefix(line, "Tag: "); ok {
				name, tail, _ = strings.Cut(rest, ",")
				isHeader = true
			} else if word, ok := strings.CutSuffix(line, ":"); ok && word != "" && !strings.ContainsAny(word, " \t:") {
				name, isHeader = word, true
			}
		}

		if isHeader {
			blocks = append(blocks, dumpBlock{name: strings.TrimSpace(name), tail: strings.TrimSpace(tail), section: section})
		} else if len(blocks) > 0 {
			blocks[len(blocks)-1].body = append(blocks[len(blocks)-1].body, line)
		}
	}
	return blocks, saved
}

// fields is what one entry's lines say: its description, its usage and call form, the filter
// statements it lists, and its labels, lower-cased.
type fields struct {
	doc, usage, call string
	filters          []string
	labels           map[string]string
}

// dumpFields reads one entry. A label of its file ends a signature (CK3 never closes switch = {); a
// line shaped like one that is not in the file's set is counted unread.
func dumpFields(block dumpBlock, labelSet string, unknown map[string]int) fields {
	read := fields{labels: map[string]string{}}
	var prose, signature []string
	depth, last, filtering := 0, "", false

	// usage opens the signature, after the words before its first statement and its call form
	addUsage := func(text string) {
		text = read.takeCall(block.name, text)
		if at := statementRe.FindStringSubmatchIndex(text); at != nil && at[2] > 0 {
			if before := strings.TrimRight(strings.TrimSpace(text[:at[2]]), ","); before != "" {
				prose = append(prose, before)
			}
			text = text[at[2]:]
		}
		if text = strings.TrimSpace(text); text != "" {
			signature = append(signature, text)
			depth = max(0, strings.Count(text, "{")-strings.Count(text, "}"))
		}
	}

	// a header's tail reads like the lines below it: a description, a modifier's first label, or
	// a signature opening on the header line
	for _, line := range append([]string{block.tail}, block.body...) {
		line = strings.TrimRight(strings.ReplaceAll(line, "**", ""), " \t")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || slices.Contains(nonLabels, trimmed) {
			continue
		}

		if label, value, ok := strings.Cut(trimmed, ":"); ok && slices.Contains(dumpLabels[labelSet], strings.TrimSpace(label)) {
			// scope lists may be stated twice and join; every other label keeps its first value
			label, value = strings.ToLower(strings.TrimSpace(label)), strings.TrimSpace(value)
			if had, seen := read.labels[label]; !seen {
				read.labels[label] = value
			} else if label == "supported scopes" || label == "input scopes" {
				read.labels[label] = had + ", " + value
			}
			depth, last = 0, label
			if label == "usage" || label == "usages" {
				addUsage(value)
			}
			continue
		}

		// a Vic3 modifier's description runs on over unindented lines until the next entry
		if last == "description" && labelSet == "modifiers.log" {
			read.labels["description"] += "\n" + trimmed
			continue
		}

		// a line shaped like a label the file does not have is counted, and read as prose
		if depth == 0 && labelShaped(trimmed) {
			label, _, _ := strings.Cut(trimmed, ":")
			unknown[strings.ToLower(label)]++
		}

		// the statements listed under Can optionally filter by: are optional keys of its block
		if trimmed == "Can optionally filter by:" {
			filtering = true
			continue
		}
		if _, statement, ok := strings.Cut(trimmed, ":"); filtering && ok && strings.HasPrefix(trimmed, "*") && isStatement(strings.TrimSpace(statement)) {
			read.filters = append(read.filters, strings.TrimSpace(statement))
			continue
		}

		// a signature is statements, the command among them; it runs on while its braces are open,
		// and prose glued after a closing brace (}simplified: x) leaves it
		if depth > 0 || isStatement(trimmed) {
			if at := gluedProse.FindStringSubmatchIndex(line); at != nil {
				prose = append(prose, strings.TrimSpace(line[at[2]:]))
				line = line[:at[2]]
			}
			signature = append(signature, line)
			depth = max(0, depth+strings.Count(line, "{")-strings.Count(line, "}"))
			continue
		}

		// usage written inside the description: after usage:, or the entry's own statement after a
		// comma or a dash
		lower := strings.ToLower(trimmed)
		if at := strings.Index(lower, "usage:"); at > 0 {
			prose = append(prose, strings.TrimRight(trimmed[:at], " ,-"))
			addUsage(trimmed[at+len("usage:"):])
			continue
		}
		if at := ownStatement(trimmed, block.name); at > 0 {
			prose = append(prose, strings.TrimRight(strings.TrimSpace(trimmed[:at]), ",-"))
			addUsage(trimmed[at:])
			continue
		}

		// prose after a signature is a second paragraph
		if len(signature) > 0 && len(prose) > 0 && !slices.Contains(prose, "") {
			prose = append(prose, "")
		}
		prose = append(prose, trimmed)
	}

	read.doc = strings.TrimSpace(strings.Join(prose, "\n"))
	if strings.EqualFold(read.doc, "unknown, add something in code registration") || strings.Trim(read.doc, "{} \n\t") == "" ||
		!strings.ContainsFunc(read.doc, func(char rune) bool { return !(unicode.IsUpper(char) || unicode.IsDigit(char) || char == '_') }) {
		read.doc = ""
	}
	read.doc = strayQuote(read.doc)
	read.usage = strayQuote(strings.Join(signature, "\n"))
	return read
}

// takeCall keeps a call form written beside the statement, name(<a>|<b>) joined to it by or, and
// gives the text without it.
func (read *fields) takeCall(name, text string) string {
	at := strings.Index(text, name+"(")
	if at < 0 {
		return text
	}
	end := strings.IndexByte(text[at:], ')')
	if end < 0 {
		return text
	}
	read.call = text[at : at+end+1]
	before, after := strings.TrimSpace(text[:at]), strings.TrimSpace(text[at+end+1:])
	if trimmed, ok := strings.CutSuffix(before, " or"); ok {
		before = trimmed
	} else if before == "or" {
		before = ""
	}
	after = strings.TrimPrefix(after, "or ")
	return strings.TrimSpace(before + " " + after)
}

// ownStatement is where a description line writes the entry's own statement after a comma or a
// dash (break_betrothal - …, break_betrothal = target); 0 where it writes none.
func ownStatement(line, name string) int {
	for _, lead := range []string{", ", "- "} {
		if at := strings.Index(line, lead+name); at > 0 && isStatement(line[at+len(lead):]) {
			return at + len(lead)
		}
	}
	return 0
}

// isStatement reports a line of script: a key, then an operator. The key is a word, a dotted path,
// a prefixed name (scope:x, c:FRA), a quoted key or a placeholder.
func isStatement(line string) bool {
	at := statementRe.FindStringIndex(line)
	return at != nil && at[0] == 0
}

// labelShaped reports a line written like a label: one to three words, the first capitalised, then
// a colon and a value, with no = before the colon.
func labelShaped(line string) bool {
	label, value, ok := strings.Cut(line, ":")
	words := strings.Fields(label)
	return ok && strings.TrimSpace(value) != "" && len(words) >= 1 && len(words) <= 3 && label == strings.TrimSpace(label) &&
		unicode.IsUpper(rune(label[0])) && !strings.ContainsAny(label, "={}<>")
}

// strayQuote drops an unmatched quote at either end of a text.
func strayQuote(text string) string {
	if strings.Count(text, `"`)%2 == 1 {
		if trimmed, ok := strings.CutSuffix(text, `"`); ok {
			return strings.TrimSpace(trimmed)
		}
		return strings.TrimSpace(strings.TrimPrefix(text, `"`))
	}
	return text
}

// commaList splits a, b, c, dropping the empty entries the games leave: "Location, , All, ".
func commaList(text string) []string {
	var out []string
	for part := range strings.SplitSeq(text, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// areas is a modifier's lists as scope names: split on commas and and, All left out (it is on
// every entry), CamelCase turned to snake_case (InternationalOrganization).
func areas(text string) []string {
	var parts, out []string
	for _, part := range commaList(text) {
		parts = append(parts, commaList(strings.ReplaceAll(" "+part, " and ", ","))...)
	}
	for _, part := range parts {
		if part == "All" {
			continue
		}
		var name strings.Builder
		for i, char := range part {
			if unicode.IsUpper(char) && i > 0 && part[i-1] != ' ' {
				name.WriteByte('_')
			}
			name.WriteRune(unicode.ToLower(char))
		}
		out = append(out, name.String())
	}
	return out
}

// traitTakes reads a Traits line into what the word after the trigger may be: yes/no, operators
// (then valid date), a list of words, or forms joined by / (character target, country tag/country
// scope, class CTraitDatabase key). A class or tag keeps the dump's words until the build names the
// database; a scope path keeps its scope.
func traitTakes(text string, unknown map[string]int) []Take {
	take := func(wordType WordType, database string) Take {
		return Take{Type: wordType, Database: database, Source: SourceDumps}
	}
	switch {
	case text == "":
		return nil
	case text == "yes/no":
		return []Take{take(WordBool, "")}
	case strings.HasPrefix(text, "<"):
		if strings.HasSuffix(text, " valid date") {
			return []Take{take(WordDate, "")}
		}
		return []Take{take(WordNumber, "")}
	case strings.Contains(text, ","):
		// CK3 heads a few lists with a label (Valid DLCs:) and writes "and" before the last word
		if label, list, ok := strings.Cut(text, ": "); ok && !strings.Contains(label, ",") {
			text = list
		}
		enum := take(WordEnum, "")
		for _, word := range commaList(text) {
			enum.Enum = append(enum.Enum, strings.TrimPrefix(word, "and "))
		}
		return []Take{enum}
	}
	var out []Take
	for part := range strings.SplitSeq(text, "/") {
		switch words := strings.Fields(part); {
		case len(words) == 2 && (words[1] == "target" || words[1] == "scope"):
			out = append(out, take(WordScopePath, words[0]))
		case len(words) == 4 && words[1] == "target" && words[3] == "scope":
			out = append(out, take(WordScopePath, words[0]), take(WordScopePath, words[2]))
		case len(words) == 3 && words[0] == "class" && words[2] == "key":
			out = append(out, take(WordReference, words[1]))
		case len(words) == 2 && (words[1] == "tag" || words[1] == "id"):
			out = append(out, take(WordReference, strings.Join(words, " ")))
		default:
			unknown["traits: "+text]++
			return nil
		}
	}
	return slices.CompactFunc(out, func(first, second Take) bool { return first.Type == second.Type && first.Database == second.Database })
}

// dataCall reads one entry of the data-type dump: Name, Name( Arg0 ) or Type.Name( Arg0 ). A type
// keeps its own description; of a name listed twice the promote is kept, with the function's
// description where its own has none.
func (dumps *Dumps) dataCall(header string, labels map[string]string, unknown map[string]int) {
	header = strings.TrimSuffix(strings.TrimSpace(header), ",")
	if !headerRe.MatchString(header) {
		unknown["header: "+header]++
		return
	}
	head, args, _ := strings.Cut(header, "(")
	typeName, name, ofType := strings.Cut(strings.TrimSpace(head), ".")
	call := DataFunction{Name: typeName, Args: commaList(strings.TrimSuffix(strings.TrimSpace(args), ")")), Doc: labels["description"], Returns: labels["return type"], Macro: labels["macro replacement"]}
	calls := dumps.Globals
	switch definitionType := labels["definition type"]; definitionType {
	case "Type", "Function", "Promote":
		dataType, ok := dumps.DataTypes[typeName]
		if !ok {
			dataType = DataType{Functions: map[string]DataFunction{}}
		}
		if !ofType {
			dataType.Doc = cmp.Or(dataType.Doc, labels["description"])
			dumps.DataTypes[typeName] = dataType
			return
		}
		dumps.DataTypes[typeName] = dataType
		call.Name, call.Promote, calls = name, definitionType == "Promote", dataType.Functions
	case "Global function", "Global macro":
	case "Global promote":
		call.Promote = true
	default:
		unknown["definition type: "+definitionType]++
		return
	}
	had, ok := calls[call.Name]
	switch {
	case !ok:
		calls[call.Name] = call
	case call.Promote && !had.Promote:
		call.Doc = cmp.Or(call.Doc, had.Doc)
		calls[call.Name] = call
	case had.Promote && !call.Promote:
		had.Doc = cmp.Or(had.Doc, call.Doc)
		calls[call.Name] = had
	}
}

// usageNode is one key a usage writes: the words after it, and the keys of its block.
type usageNode struct {
	key       string
	words     []string // its value and any words after its block, spaced as written
	block     bool
	body      Block  // its block holds <triggers> or <effects>
	statement bool   // written after <effects> or the like: an example statement
	optional  bool   // (optional) or [key = x] says it may be left out
	value     string // the default an annotation gives: (optional, default = no)
	kids      []*usageNode
}

// commandKey is what a command's usage declares is written after it: its own block or an iterator
// sibling's, its family's plain spelling, or a bare list of keys, with the filters its entry lists as
// optional keys; nil where the text declares nothing, and foreign where the usage is another
// definition's text.
func (dumps *Dumps) commandKey(command, usage string, filters []string) (key *Key, foreign bool) {
	root := parseUsage(usage)

	// CK3 writes an iterator's filter under any_ in all four, EU5 shared keys under the bare stem
	names := []string{command}
	for _, stem := range stems(command) {
		names = append(names, stem)
		for _, prefix := range iteratorPrefixes {
			names = append(names, prefix+stem)
		}
	}
	var own []*usageNode
	written := false
	var walk func(node *usageNode)
	walk = func(node *usageNode) {
		for _, kid := range node.kids {
			written = written || strings.EqualFold(kid.key, command)
			if kid.block && slices.ContainsFunc(names, func(name string) bool { return strings.EqualFold(kid.key, name) }) {
				own = append(own, kid)
			}
			walk(kid)
		}
	}
	walk(root)

	switch {
	case len(own) > 0:
	case written || len(root.kids) == 0:
		// the command written only inside a wider example
	case dumps.declares(strings.ToLower(root.kids[0].key)):
		// the family's plain spelling (add_to_variable_list): its block, or nothing for a value
		if root.kids[0].block {
			own = root.kids[:1]
		}
	case !root.kids[0].block:
		// a bare list of two keys or more; one key is a stale name (start_diarchy = regency), and a
		// hop with nothing after it a scope whose brace the dump lost (power_bloc =)
		first := root.kids[0]
		other := slices.ContainsFunc(root.kids, func(kid *usageNode) bool { return !strings.EqualFold(kid.key, first.key) })
		if target, hop := dumps.EventTargets[strings.ToLower(first.key)]; other && !(hop && !target.Wild && len(first.words) == 0) {
			own = []*usageNode{root}
		}
	default:
		// not this command's text: Vic3's geographic iterators carry some scripted trigger's
		foreign = true
	}

	// of a key two blocks write, the first block's is kept
	for _, node := range own {
		read := dumps.nodeKey(command, command, node)
		if key == nil {
			key = read
			continue
		}
		key.Block = cmp.Or(key.Block, read.Block)
		for name, child := range read.Keys {
			if key.Keys == nil {
				key.Keys = map[string]*Key{}
			}
			if key.Keys[name] == nil {
				key.Keys[name] = child
			}
		}
	}
	for _, filter := range filters {
		for _, node := range parseUsage(filter).kids {
			if key == nil {
				key = &Key{Name: command, Source: SourceDumps, Written: WrittenBlock}
			}
			node.optional = true
			read := dumps.nodeKey(command, command, &usageNode{block: true, kids: []*usageNode{node}})
			for name, child := range read.Keys {
				if key.Keys == nil {
					key.Keys = map[string]*Key{}
				}
				if key.Keys[name] == nil {
					key.Keys[name] = child
				}
			}
		}
	}
	return key, foreign
}

// nodeKey is the key a usage node declares, one key below per name a slash joins (days/weeks); the
// command, prefixed words, capitals (traitID, OR) and declared words after <effects> are none, and a
// number or X1 is the <number> position.
func (dumps *Dumps) nodeKey(command, name string, node *usageNode) *Key {
	key := &Key{Name: name, Source: SourceDumps, Doc: strings.Join(node.words, " "), Block: node.body, Default: node.value}
	if node.optional {
		key.Required = RequiredOptional
	}
	if node.block {
		key.Written = WrittenBlock
	} else if len(node.words) > 0 {
		key.Written = WrittenWord
	}
	for _, kid := range node.kids {
		for child := range strings.SplitSeq(kid.key, "/") {
			child = strings.TrimRight(child, ".")
			if numberKey.MatchString(child) {
				child = "<number>"
			}
			_, isTarget := dumps.EventTargets[child]
			isKey := usageKey(child) || child == "<number>"
			if !isKey || strings.Contains(child, ":") || child != strings.ToLower(child) || child == command || key.Keys[child] != nil || kid.statement && (dumps.declares(child) || isTarget) {
				continue
			}
			if key.Keys == nil {
				key.Keys = map[string]*Key{}
			}
			key.Keys[child] = dumps.nodeKey(command, child, kid)
		}
	}
	return key
}

// command is the command a name is, or its iterator family's where the dumps declare only a sibling
// spelling (random_tenet beside any_tenet).
func (dumps *Dumps) command(commands map[string]Command, name string) (Command, bool) {
	if command, ok := commands[name]; ok {
		return command, true
	}
	for _, stem := range stems(name) {
		for _, prefix := range iteratorPrefixes {
			if command, ok := commands[prefix+stem]; ok {
				return command, true
			}
		}
	}
	return Command{}, false
}

// targets reports a name that is an event target or a path starting at one (root.primary_title),
// and one that is a prefixed name (scope:actor.faith) or vanilla's position of them.
func (dumps *Dumps) targets(name string) (target, prefixed bool) {
	first, _, _ := strings.Cut(strings.ToLower(name), ".")
	_, target = dumps.EventTargets[first]
	_, prefixed = jomini.SplitPrefixed(name)
	return target, prefixed || name == "<prefixed>"
}

// declares reports an effect or trigger the game declares.
func (dumps *Dumps) declares(name string) bool {
	_, effect := dumps.Effects[name]
	_, trigger := dumps.Triggers[name]
	return effect || trigger
}

// parseUsage reads a usage as loose script. A block outlives its line only when the line ends on
// its brace, prose in parentheses after it aside; a key starts only where a statement may, never in
// prose. (optional) right after a closing brace marks the next key, anywhere else the key whose
// value it follows; [key = x] marks its key; <operator> reads as an operator; a comma ends a value.
func parseUsage(usage string) *usageNode {
	root := &usageNode{block: true}
	stack := []*usageNode{root}
	for _, line := range strings.Split(usage, "\n") {
		opened := len(stack)
		line = optionalRe.ReplaceAllString(line, " \x01$1 ")
		tokens, spaced := usageTokens(line)
		var target *usageNode        // the key the words on this line go to
		expect, value := true, false // a key may start here; the target's value is being read
		pending, bracket := false, false
		previous := ""
		for i, token := range tokens {
			block := stack[len(stack)-1]
			isOperator := func(token string) bool {
				return strings.Trim(token, "=<>!?") == "" || token == "<operator>" || token == "<comparator>"
			}
			operator := isOperator(token)
			placeholder := strings.HasPrefix(token, "<") && strings.HasSuffix(token, ">")
			switch {
			case strings.HasPrefix(token, "\x01"):
				// (optional) or (optional, default = x)
				if previous != "}" && target != nil {
					target.optional, target.value = true, cmp.Or(target.value, token[1:])
				} else {
					pending = true
				}
			case token == "[" && expect:
				pending, bracket = true, true
			case token == "]" && bracket:
				bracket = false
			case token == "{":
				owner := &usageNode{block: true}
				if target != nil && value && len(target.words) == 0 {
					owner = target
					owner.block = true
				}
				stack, target, value, expect = append(stack, owner), nil, false, true
			case token == "}":
				if len(stack) > 1 {
					target, stack = stack[len(stack)-1], stack[:len(stack)-1]
				}
				value, expect = false, true
			case operator && target != nil && !value && len(target.words) == 0:
				value = true
			case placeholder && strings.Contains(token, "="):
				// <count=num/all>: a key the dump writes as a placeholder
				name, word, _ := strings.Cut(strings.Trim(token, "<>"), "=")
				block.kids = append(block.kids, &usageNode{key: strings.TrimSpace(name), words: []string{strings.TrimSpace(word)}})
			case expect && !operator && i+1 < len(tokens) && isOperator(tokens[i+1]) && (usageKey(token) || numberKey.MatchString(token)):
				target, value, expect = &usageNode{key: token, statement: block.body != BlockNone, optional: pending}, false, false
				pending = false
				block.kids = append(block.kids, target)
			case target != nil && (value || !placeholder || !spaced[i]):
				// the words of a value, a comment, or prose after a block, spaced as written; a
				// comma ends the value, and the next key may start
				word, comma := strings.CutSuffix(token, ",")
				if last := len(target.words) - 1; last >= 0 && !spaced[i] {
					target.words[last] += word
				} else if word != "" {
					target.words = append(target.words, word)
				}
				expect = comma || value && len(target.words) == 1
			default:
				// a word of no key or a comment line: <triggers>, <effects>, CK3's { some effects } and
				// # optional effects... say what the block holds
				words := strings.FieldsFunc(strings.ToLower(strings.Trim(token, "<>#")), func(char rune) bool { return strings.ContainsRune("_ .,:;()", char) })
				if len(words) == 0 {
					break
				}
				switch words[len(words)-1] {
				case "trigger", "triggers", "condition", "conditions":
					block.body = cmp.Or(block.body, BlockTriggers)
				case "effect", "effects":
					block.body = cmp.Or(block.body, BlockEffects)
				}
			}
			previous = token
		}

		// a line that ends inside blocks it opened carries them only when it ends on the brace, a
		// comment or prose in parentheses after it aside
		last := len(tokens) - 1
		if last >= 0 && strings.HasPrefix(tokens[last], "#") {
			last--
		}
		if last >= 0 && strings.HasSuffix(tokens[last], ")") {
			for open := last; open >= 0; open-- {
				if strings.HasPrefix(tokens[open], "(") {
					last = open - 1
					break
				}
			}
		}
		if len(stack) > opened && (last < 0 || tokens[last] != "{") {
			stack = stack[:opened]
		}
	}
	return root
}

// usageTokens splits a usage line into braces, brackets, operators, "quoted text", <placeholders>,
// a # comment, and words, each with whether space comes before it.
func usageTokens(line string) (tokens []string, spaced []bool) {
	for i := 0; i < len(line); {
		char, end := line[i], i+1
		closes := strings.IndexAny(line[i+1:], "<>{}") // where a placeholder opened here would close
		switch {
		case char == ' ' || char == '\t':
			i++
			continue
		case char == '#':
			end = len(line)
		case char == '{' || char == '}' || char == '[' || char == ']':
		case char == '"':
			if close := strings.IndexByte(line[i+1:], '"'); close >= 0 {
				end = i + close + 2
			} else {
				end = len(line)
			}
		case char == '<' && closes >= 0 && line[i+1+closes] == '>' && line[i+1] != '=':
			end = i + closes + 2
		case strings.IndexByte("=<>", char) >= 0 || (char == '!' || char == '?') && i+1 < len(line) && line[i+1] == '=':
			for end < len(line) && line[end] == '=' {
				end++
			}
		default:
			for end < len(line) && strings.IndexByte(" \t{}[]#\"=<>", line[end]) < 0 && !((line[end] == '!' || line[end] == '?') && end+1 < len(line) && line[end+1] == '=') {
				end++
			}
		}
		tokens, spaced = append(tokens, line[i:end]), append(spaced, i == 0 || line[i-1] == ' ' || line[i-1] == '\t')
		i = end
	}
	return tokens, spaced
}

// usageKey reports a word that may be a key in a usage: a letter or underscore, then letters,
// digits and _ . / : as the dumps spell them (martial/diplomacy/intrigue..., scope:x).
func usageKey(word string) bool {
	return word != "" && (word[0] == '_' || unicode.IsLetter(rune(word[0]))) && !strings.ContainsFunc(word, func(char rune) bool {
		return !(char == '_' || char == '.' || char == '/' || char == ':' || unicode.IsLetter(char) || unicode.IsDigit(char))
	})
}

// UsableIn reports whether something declared for the scope types in may be written in scope. No
// list, a list saying none, and an unknown scope all answer yes: an unknown never accuses.
func UsableIn(in []string, scope string) bool {
	return len(in) == 0 || scope == "" || slices.Contains(in, "none") || slices.Contains(in, scope)
}
