// vocab.go reads what a game declares about its own language from the files two console commands
// write: script_docs's seven dumps (dumpFiles) and dump_data_types's data_types_*.txt. Their three
// layouts (markdown headings, blocks between dashed rules, bare headers) are told apart by the
// file, never by the game. Any part may be absent: a user who has not run the commands gets an
// empty Vocab, which every reader must handle.
package schema

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Vocab is what the game declares about its own language. It is never nil; any part may be empty.
type Vocab struct {
	Effects     map[string]EngineToken
	Triggers    map[string]EngineToken
	Modifiers   map[string]Modifier            // by name; a CK3 name with $TYPE$ stands for one per definition of TYPE
	Links       map[string]Link                // the links written alone, each a hop from its input scopes: dynasty = { }
	Prefixes    map[string]Link                // the links written name:key, which require the key: dynasty:x
	ScopeTypes  map[string]ScopeType           // every scope type event_scopes.log lists, none included
	OnActions   map[string]OnAction            // every on_action the game declares
	CustomLoc   map[string]CustomLoc           // every custom localization, by the name localization calls it by
	DataTypes   map[string]map[string]DataFunc // each type of a data call, and its functions and promotes by name
	Globals     map[string]DataFunc            // what a data call may start with: GetPlayer, GetVariableSystem
	SavedScopes []string                       // the scope names the engine saves itself: actor, recipient
	Unknown     []Tally                        // labels and forms the reader did not know, for the drift report
}

// IsScopeType reports a scope type a block may run in: one where triggers are evaluated or effects
// run. A link or token whose output is anything else (value, boolean, flag) leads to no scope.
func (vocab *Vocab) IsScopeType(name string) bool {
	scope := vocab.ScopeTypes[name]
	return scope.Triggers || scope.Effects
}

// EngineToken is one effect or trigger the game declares.
type EngineToken struct {
	Name   string
	Doc    string   // the dump's description
	Usage  string   // the dump's signature or example, line breaks kept
	In     []string // Supported Scopes: the scope types it may be written in
	Out    string   // Supported Targets: the scope an iterator's block runs in, or the type of value it takes
	Traits Traits   // a trigger's Traits: what the value written after it may be
	Block  Member   // what its block holds and takes, as its usage writes it
}

// Member is one member of a token's block, as the token's usage writes it.
type Member struct {
	Doc     string             // the dump's words for it: add this trait; male/female/character scope
	Body    BodyClass          // what its block holds: limit = { <triggers> }
	Members map[string]*Member // the members of its own block: random_traits_list = { count = { 1 5 } }
}

// Traits is what a trigger's Traits line says the value written after it may be. A form the
// reader does not know is drift, and the trigger keeps no traits.
type Traits struct {
	Compare []string // the operators it is compared with: <, <=, =, !=, >, >=
	Date    bool     // what it compares is a date (valid date)
	Bool    bool     // yes/no
	Scopes  []string // the scope types its value may be: character target, landed_title scope
	Keys    []string // the definitions it may name, as the dump says: CTraitDatabase, country tag, province id
	Words   []string // the closed set of words it takes: minor, major, apocalyptic; a DLC's name
}

// Link is one reading of a word of the link dump.
type Link struct {
	Name   string
	Doc    string
	In     []string // Input Scopes: the scope types it may be written in; none for a global link
	Out    string   // Output Scopes: the scope type it leads to; one listing several (value, boolean) is no hop
	Global bool     // Global Link: it may be written in any scope
	Wild   bool     // Wild Card: a pattern (value: any numeric literal), which is no hop between scopes
}

// ScopeType is one scope type of event_scopes.log and what script may do in it.
type ScopeType struct {
	Triggers  bool   // Evaluate Triggers
	Effects   bool   // Execute Effects
	Hops      bool   // Change Scopes: links lead on from it
	SaveToken string // Save Token: the word it is saved as; none where it cannot be saved
	Variables bool   // Stores Variables
}

// OnAction is one on_action the game declares.
type OnAction struct {
	Scope    string // Expected Scope: the scope type it fires in; none where it has no root
	FromCode bool   // From Code: the game fires it itself, and not only script
}

// CustomLoc is one custom localization: text chosen by triggers, called from localization.
type CustomLoc struct {
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
	Categories []string // where the dump says it applies, lower-cased: character, county, country, all
	Section    string   // Vic3: static, dynamic or potential dynamic, from the heading it is listed under
}

// DataFunc is one function, promote or macro of the data-type dump.
type DataFunc struct {
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

// dumpLabels is the labels the dumps write. A label-shaped line outside it is prose.
var dumpLabels = []string{
	"supported scopes", "supported targets", "usage", "traits", "input scopes", "output scopes", "global link", "requires data", "wild card", "expected scope", "from code",
	"evaluate triggers", "execute effects", "change scopes", "save token", "stores variables", "mask", "use areas", "categories", "name", "description",
	"scope", "random valid", "parent", "suffix", "entries", "definition type", "return type", "macro replacement",
}

// iteratorPrefixes are the four spellings of one iterator, which document each other unevenly.
var iteratorPrefixes = []string{"any_", "every_", "random_", "ordered_"}

// ReadVocab reads the dumps in dirs, the folders the registry lists for the game; a missing dump
// stays empty.
func ReadVocab(dirs []string) *Vocab {
	vocab := &Vocab{
		Effects: map[string]EngineToken{}, Triggers: map[string]EngineToken{}, Modifiers: map[string]Modifier{}, Links: map[string]Link{}, Prefixes: map[string]Link{},
		ScopeTypes: map[string]ScopeType{}, OnActions: map[string]OnAction{}, CustomLoc: map[string]CustomLoc{}, DataTypes: map[string]map[string]DataFunc{}, Globals: map[string]DataFunc{},
	}
	unknown := map[string]int{}
	for _, dir := range dirs {
		entries, _ := os.ReadDir(dir)
		for _, entry := range entries {
			file := strings.ToLower(entry.Name())
			dataTypes := strings.HasPrefix(file, "data_types_") && strings.HasSuffix(file, ".txt")
			if !dataTypes && !slices.Contains(dumpFiles, file) {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
			if err != nil {
				continue
			}

			blocks, saved := dumpBlocks(string(raw))
			vocab.SavedScopes = append(vocab.SavedScopes, saved...)
			for _, block := range blocks {
				doc, usage, labels := dumpFields(block, unknown)
				token := EngineToken{Name: block.name, Doc: doc, Usage: usage, In: commaList(labels["supported scopes"]), Out: labels["supported targets"]}
				switch {
				case dataTypes:
					vocab.dataCall(block.name, labels, unknown)
				case file == "effects.log":
					vocab.Effects[block.name] = token
				case file == "triggers.log":
					token.Traits = readTraits(labels["traits"], unknown)
					vocab.Triggers[block.name] = token
				case file == "event_targets.log":
					// a word may be declared twice, once written name:key and once alone: two readings
					links := vocab.Links
					if labels["requires data"] == "yes" {
						links = vocab.Prefixes
					}
					links[block.name] = Link{Name: block.name, Doc: doc, In: commaList(labels["input scopes"]), Out: labels["output scopes"], Global: labels["global link"] == "yes", Wild: labels["wild card"] == "yes"}
				case file == "event_scopes.log":
					vocab.ScopeTypes[block.name] = ScopeType{
						Triggers: labels["evaluate triggers"] == "yes", Effects: labels["execute effects"] == "yes", Hops: labels["change scopes"] == "yes",
						SaveToken: labels["save token"], Variables: labels["stores variables"] == "yes",
					}
				case file == "modifiers.log":
					areas := labels["mask"] + "," + labels["use areas"] + "," + labels["categories"]
					vocab.Modifiers[block.name] = Modifier{Name: block.name, Label: labels["name"], Doc: labels["description"], Categories: commaList(strings.ToLower(areas)), Section: block.section}
				case file == "on_actions.log":
					vocab.OnActions[block.name] = OnAction{Scope: labels["expected scope"], FromCode: strings.EqualFold(labels["from code"], "yes")}
				case file == "custom_localization.log":
					// the entries are the bare lines below Entries:, which read as prose
					vocab.CustomLoc[block.name] = CustomLoc{Scope: labels["scope"], Random: labels["random valid"] == "yes", Parent: labels["parent"], Suffix: labels["suffix"], Entries: strings.Fields(doc)}
				}
			}
		}
	}

	// members are read once every token is known, since a declared token after <effects> is an
	// example statement; a trigger's block holds no effects (any_ruler shares every_ruler's example)
	for name, token := range vocab.Effects {
		token.Block = vocab.members(name, token.Usage)
		vocab.Effects[name] = token
	}
	for name, token := range vocab.Triggers {
		token.Block = vocab.members(name, token.Usage)
		token.Block.Body &= BodyTrigger
		vocab.Triggers[name] = token
	}

	for label, count := range unknown {
		vocab.Unknown = append(vocab.Unknown, Tally{Name: label, Count: count})
	}
	slices.SortFunc(vocab.Unknown, func(first, second Tally) int { return strings.Compare(first.Name, second.Name) })
	return vocab
}

// dumpBlocks splits a dump into its entries, in the layout the file first shows. The link dump ends
// with the scope names the engine saves itself, below "Saved from Code"; those come back as saved.
func dumpBlocks(text string) (blocks []dumpBlock, saved []string) {
	text = strings.ToValidUTF8(strings.ReplaceAll(strings.TrimPrefix(text, "\xef\xbb\xbf"), "\r", ""), string(utf8.RuneError))
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

// dumpFields reads one entry: its description, its signature, and its labels, lower-cased. A label
// ends a signature (CK3 never closes switch = {); an unknown one written like the games' is drift.
func dumpFields(block dumpBlock, unknown map[string]int) (doc, usage string, labels map[string]string) {
	labels = map[string]string{}
	var prose, signature []string
	depth := 0

	// a header's tail reads like the lines below it: a description, a modifier's first label, or
	// a signature opening on the header line
	for _, line := range append([]string{block.tail}, block.body...) {
		line = strings.TrimRight(strings.ReplaceAll(line, "**", ""), " \t")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		if label, value, ok := dumpLabel(line); ok {
			if slices.Contains(dumpLabels, label) {
				// scope lists may be stated twice and join; every other label keeps its first value
				if had, seen := labels[label]; !seen {
					labels[label] = value
				} else if label == "supported scopes" || label == "input scopes" {
					labels[label] = had + ", " + value
				}

				// a usage label's own value opens the signature
				depth = 0
				if label == "usage" && value != "" {
					signature = append(signature, value)
					depth = max(0, strings.Count(value, "{")-strings.Count(value, "}"))
				}
				continue
			}
			if words := strings.Fields(trimmed[:strings.IndexByte(trimmed, ':')]); depth == 0 && len(words) == 2 && unicode.IsUpper(rune(words[0][0])) && unicode.IsUpper(rune(words[1][0])) {
				unknown[label]++
				continue
			}
		}

		// a signature is key = value lines, the token among them; it runs on while its braces are open
		key := trimmed[:strings.IndexFunc(trimmed+" ", func(char rune) bool {
			return !(char == '_' || char == '.' || char == '/' || unicode.IsLetter(char) || unicode.IsDigit(char))
		})]
		assigns := key != "" && !unicode.IsDigit(rune(key[0])) && strings.HasPrefix(strings.TrimSpace(trimmed[len(key):]), "=")
		if depth > 0 || assigns {
			signature = append(signature, line)
			depth = max(0, depth+strings.Count(line, "{")-strings.Count(line, "}"))
			continue
		}

		// prose after a signature is a second paragraph
		if len(signature) > 0 && len(prose) > 0 && !slices.Contains(prose, "") {
			prose = append(prose, "")
		}
		prose = append(prose, trimmed)
	}

	doc = strings.TrimSpace(strings.Join(prose, "\n"))
	if strings.EqualFold(doc, "unknown, add something in code registration") {
		doc = ""
	}
	return doc, strings.Join(signature, "\n"), labels
}

// dumpLabel splits Label: value; a label is at most three words and 24 characters, lower-cased.
func dumpLabel(line string) (label, value string, ok bool) {
	label, value, ok = strings.Cut(strings.TrimSpace(line), ":")
	if !ok || label == "" || len(label) > 24 || len(strings.Fields(label)) > 3 || strings.ContainsAny(label, "={}<>") {
		return "", "", false
	}
	return strings.ToLower(strings.TrimSpace(label)), strings.TrimSpace(value), true
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

// readTraits reads a Traits line: yes/no, operators (then valid date), a list of words, or forms
// joined by / (character target, country tag/country scope, class CTraitDatabase key).
func readTraits(text string, unknown map[string]int) Traits {
	var traits Traits
	switch {
	case text == "":
	case text == "yes/no":
		traits.Bool = true
	case strings.HasPrefix(text, "<"):
		operators, date := strings.CutSuffix(text, " valid date")
		traits.Compare, traits.Date = commaList(operators), date
	case strings.Contains(text, ","):
		// CK3 heads a few lists with a label (Valid DLCs:) and writes "and" before the last word
		if label, list, ok := strings.Cut(text, ": "); ok && !strings.Contains(label, ",") {
			text = list
		}
		for _, word := range commaList(text) {
			traits.Words = append(traits.Words, strings.TrimPrefix(word, "and "))
		}
	default:
		for part := range strings.SplitSeq(text, "/") {
			switch words := strings.Fields(part); {
			case len(words) == 2 && (words[1] == "target" || words[1] == "scope"):
				traits.Scopes = append(traits.Scopes, words[0])
			case len(words) == 4 && words[1] == "target" && words[3] == "scope":
				traits.Scopes = append(traits.Scopes, words[0], words[2])
			case len(words) == 3 && words[0] == "class" && words[2] == "key":
				traits.Keys = append(traits.Keys, words[1])
			case len(words) == 2 && (words[1] == "tag" || words[1] == "id"):
				traits.Keys = append(traits.Keys, strings.Join(words, " "))
			default:
				unknown["traits: "+text]++
				return Traits{}
			}
		}
		slices.Sort(traits.Scopes)
		traits.Scopes = slices.Compact(traits.Scopes)
	}
	return traits
}

// dataCall reads one entry of the data-type dump: Name, Name( Arg0 ) or Type.Name( Arg0 ). Of a
// name listed twice the promote is kept: a concept is also a function returning [unregistered].
func (vocab *Vocab) dataCall(header string, labels map[string]string, unknown map[string]int) {
	head, args, _ := strings.Cut(strings.TrimSuffix(header, ","), "(")
	typeName, name, member := strings.Cut(strings.TrimSpace(head), ".")
	call := DataFunc{Name: typeName, Args: commaList(strings.TrimSuffix(strings.TrimSpace(args), ")")), Doc: labels["description"], Returns: labels["return type"], Macro: labels["macro replacement"]}
	calls := vocab.Globals
	switch kind := labels["definition type"]; kind {
	case "Type", "Function", "Promote":
		if vocab.DataTypes[typeName] == nil {
			vocab.DataTypes[typeName] = map[string]DataFunc{}
		}
		if !member {
			return
		}
		call.Name, call.Promote, calls = name, kind == "Promote", vocab.DataTypes[typeName]
	case "Global function", "Global macro":
	case "Global promote":
		call.Promote = true
	default:
		unknown["definition type: "+kind]++
		return
	}
	if had, ok := calls[call.Name]; !ok || call.Promote && !had.Promote {
		calls[call.Name] = call
	}
}

// usageNode is one key a usage writes: the words after it, and the keys of its block.
type usageNode struct {
	key       string
	words     []string // its value and any words after its block, spaced as written
	block     bool
	body      BodyClass // its block holds <triggers> or <effects>
	statement bool      // written after <effects> or the like: an example statement
	kids      []*usageNode
}

// members is what a token's usage declares its block takes: its own block or an iterator sibling's,
// its family's plain spelling, or a bare list of members; any other text declares nothing.
func (vocab *Vocab) members(token, usage string) Member {
	root := parseUsage(usage)

	// CK3 writes an iterator's filter under any_ in all four, EU5 shared members under the bare stem
	names := []string{token}
	for _, stem := range stems(token) {
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
			written = written || strings.EqualFold(kid.key, token)
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
		// the token written only inside a wider example
		return Member{}
	case vocab.declares(strings.ToLower(root.kids[0].key)):
		// the family's plain spelling (add_to_variable_list): its block, or nothing for a value
		if !root.kids[0].block {
			return Member{}
		}
		own = root.kids[:1]
	case !root.kids[0].block:
		// a bare list of two keys or more; one key is a stale name (start_diarchy = regency), and a
		// hop with nothing after it a scope whose brace the dump lost (power_bloc =)
		first := root.kids[0]
		other := slices.ContainsFunc(root.kids, func(kid *usageNode) bool { return !strings.EqualFold(kid.key, first.key) })
		if link, hop := vocab.Links[strings.ToLower(first.key)]; !other || hop && !link.Wild && len(first.words) == 0 {
			return Member{}
		}
		own = []*usageNode{root}
	default:
		// not this token's text: Vic3's scripted-list iterators carry some scripted trigger's
		return Member{}
	}

	// of a member two blocks write, the first block's is kept
	var block Member
	for _, node := range own {
		read := vocab.member(token, node)
		block.Body |= read.Body
		for name, member := range read.Members {
			if block.Members == nil {
				block.Members = map[string]*Member{}
			}
			if block.Members[name] == nil {
				block.Members[name] = member
			}
		}
	}
	return block
}

// member is what a usage node's block takes, one member per name a slash joins (days/weeks); the
// token, numbers, cites, capitals (traitID, OR) and declared words after <effects> are none.
func (vocab *Vocab) member(token string, node *usageNode) *Member {
	member := &Member{Doc: strings.Join(node.words, " "), Body: node.body}
	for _, kid := range node.kids {
		for name := range strings.SplitSeq(kid.key, "/") {
			name = strings.TrimRight(name, ".")
			_, link := vocab.Links[name]
			if !usageKey(name) || strings.Contains(name, ":") || name != strings.ToLower(name) || name == token || member.Members[name] != nil || kid.statement && (vocab.declares(name) || link) {
				continue
			}
			if member.Members == nil {
				member.Members = map[string]*Member{}
			}
			member.Members[name] = vocab.member(token, kid)
		}
	}
	return member
}

// declares reports an effect or trigger the game declares.
func (vocab *Vocab) declares(name string) bool {
	_, effect := vocab.Effects[name]
	_, trigger := vocab.Triggers[name]
	return effect || trigger
}

// parseUsage reads a usage as loose script. A block outlives its line only when the line ends on
// its brace, as prose leaves braces open; a key starts only where a statement may, never in prose.
func parseUsage(usage string) *usageNode {
	root := &usageNode{block: true}
	stack := []*usageNode{root}
	for _, line := range strings.Split(usage, "\n") {
		opened := len(stack)
		tokens, spaced := usageTokens(line)
		var target *usageNode        // the key the words on this line go to
		expect, value := true, false // a key may start here; the target's value is being read
		for i, token := range tokens {
			block := stack[len(stack)-1]
			operator := strings.Trim(token, "=<>!?") == ""
			placeholder := strings.HasPrefix(token, "<") && strings.HasSuffix(token, ">")
			switch {
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
				// <count=num/all>: a member the dump writes as a placeholder
				name, word, _ := strings.Cut(strings.Trim(token, "<>"), "=")
				block.kids = append(block.kids, &usageNode{key: strings.TrimSpace(name), words: []string{strings.TrimSpace(word)}})
			case expect && !operator && i+1 < len(tokens) && strings.Trim(tokens[i+1], "=<>!?") == "" && usageKey(token):
				target, value, expect = &usageNode{key: token, statement: block.body != 0}, false, false
				block.kids = append(block.kids, target)
			case target != nil && (value || !placeholder || !spaced[i]):
				// the words of a value, a comment, or prose after a block, spaced as written
				if last := len(target.words) - 1; last >= 0 && !spaced[i] {
					target.words[last] += token
				} else {
					target.words = append(target.words, token)
				}
				expect = value && len(target.words) == 1
			default:
				// a word of no key or a comment line: <triggers>, <effects>, CK3's { some effects } and
				// # optional effects... say what the block holds
				words := strings.FieldsFunc(strings.ToLower(strings.Trim(token, "<>#")), func(char rune) bool { return strings.ContainsRune("_ .,:;()", char) })
				if len(words) == 0 {
					break
				}
				switch words[len(words)-1] {
				case "trigger", "triggers", "condition", "conditions":
					block.body |= BodyTrigger
				case "effect", "effects":
					block.body |= BodyEffect
				}
			}
		}

		// a line that ends inside blocks it opened carries them only when it ends on the brace
		last := len(tokens) - 1
		if last >= 0 && strings.HasPrefix(tokens[last], "#") {
			last--
		}
		if len(stack) > opened && (last < 0 || tokens[last] != "{") {
			stack = stack[:opened]
		}
	}
	return root
}

// usageTokens splits a usage line into braces, operators, "quoted text", <placeholders>, a
// # comment, and words, each with whether space comes before it.
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
		case char == '{' || char == '}':
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
			for end < len(line) && strings.IndexByte(" \t{}#\"=<>", line[end]) < 0 && !((line[end] == '!' || line[end] == '?') && end+1 < len(line) && line[end+1] == '=') {
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
