// vocab.go reads what a game declares about its own language from the six files its script_docs console command writes: effects.log, triggers.log, event_targets.log, event_scopes.log, modifiers.log and on_actions.log, the same names in all three games. They come in three layouts, told apart by looking at the file and never by asking which game wrote it: markdown headings (Vic3, EU5), blocks between dashed rules (CK3, and every game's on_actions), and bare headers with nothing between them (scope types and modifiers). Below its header every block is the same: a description, an optional signature written as script, and labelled lines. The vocabulary is declared, never learned, and any part of it may be absent: a user who has not run script_docs gets an empty Vocab, and everything that reads one must answer for that.
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
	Modifiers   map[string]Modifier
	Links       map[string]Link
	ScopeTypes  []string          // in the dump's order; none is one of them
	OnActions   map[string]string // on_action name to the scope type it fires in, none included
	SavedScopes []string          // the scope names the engine saves itself, cited like any saved scope: actor, recipient
	Unknown     []Tally           // labels the reader did not know, for the drift report
}

// IsScopeType reports a name the dump lists as a scope type; a link or a token whose output is anything else (value, boolean, a name of the game's own) leads to no scope.
func (v *Vocab) IsScopeType(name string) bool {
	return slices.Contains(v.ScopeTypes, name)
}

// EngineToken is one effect or trigger the game declares.
type EngineToken struct {
	Name  string
	Doc   string   // the dump's description
	Usage string   // the dump's signature or example, line breaks kept
	In    []string // the scope types it may be written in
	Out   string   // the dump's supported target: the scope type an iterator's block runs in, or the type of the value it takes
}

// Link is one declared hop between scope types.
type Link struct {
	Name   string
	Doc    string
	In     []string // the scope types it may be written in; none for a global link
	Out    string   // the scope type it leads to, as the dump writes it; a few links list several (modifier: value, boolean), and none of those is a hop
	Prefix bool     // written as name:key (the dump says it requires data)
	Wild   bool     // a pattern the dump says is a wild card (value: any numeric literal), which is no hop between scopes
}

// Modifier is one modifier the game declares.
type Modifier struct {
	Name       string
	Doc        string   // the dump's description, where it gives one
	Categories []string // where the dump says it applies, lower-cased: character, county, country, all
}

// dumpBlock is one entry of a dump: its header and the lines below it.
type dumpBlock struct {
	name string   // the token, link, scope type, modifier or on_action
	tail string   // what follows the name on the header line: a description, or a modifier's categories
	body []string // the lines below the header
}

// dumpFiles is the six files script_docs writes, under the same names in every game.
var dumpFiles = []string{"effects.log", "triggers.log", "event_targets.log", "event_scopes.log", "modifiers.log", "on_actions.log"}

// dumpLabels is the labels the three games write. A label-shaped line outside it is prose.
var dumpLabels = []string{
	"supported scopes", "supported targets", "usage", "input scopes", "output scopes", "global link", "requires data", "wild card", "expected scope", "from code",
	"evaluate triggers", "execute effects", "change scopes", "save token", "stores variables", "mask", "use areas", "categories", "name", "description", "scope", "random valid", "traits",
}

// iteratorPrefixes are the four spellings of one iterator, which document each other unevenly.
var iteratorPrefixes = []string{"any_", "every_", "random_", "ordered_"}

// ReadVocab reads the dump files found in dirs, the game's logs and docs folders. A file that is missing leaves its part empty.
func ReadVocab(dirs []string) *Vocab {
	v := &Vocab{Effects: map[string]EngineToken{}, Triggers: map[string]EngineToken{}, Modifiers: map[string]Modifier{}, Links: map[string]Link{}, OnActions: map[string]string{}}
	unknown := map[string]int{}
	for _, dir := range dirs {
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			file := strings.ToLower(e.Name())
			if !slices.Contains(dumpFiles, file) {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				continue
			}

			blocks, saved := dumpBlocks(string(raw))
			v.SavedScopes = append(v.SavedScopes, saved...)
			for _, b := range blocks {
				doc, usage, labels := dumpFields(b, unknown)
				switch file {
				case "effects.log":
					v.Effects[b.name] = EngineToken{Name: b.name, Doc: doc, Usage: usage, In: commaList(labels["supported scopes"]), Out: labels["supported targets"]}
				case "triggers.log":
					v.Triggers[b.name] = EngineToken{Name: b.name, Doc: doc, Usage: usage, In: commaList(labels["supported scopes"]), Out: labels["supported targets"]}
				case "event_targets.log":
					// a link may be declared twice, once global and once for a scope: the two are one link
					l := Link{Name: b.name, Doc: doc, In: commaList(labels["input scopes"]), Out: labels["output scopes"], Prefix: labels["requires data"] == "yes", Wild: labels["wild card"] == "yes"}
					if first, ok := v.Links[b.name]; ok {
						l.In, l.Prefix, l.Wild = append(first.In, l.In...), l.Prefix || first.Prefix, l.Wild || first.Wild
						if l.Doc == "" {
							l.Doc = first.Doc
						}
						if l.Out == "" {
							l.Out = first.Out
						}
					}
					v.Links[b.name] = l
				case "event_scopes.log":
					// a scope in which nothing is evaluated or run (value, boolean) is no scope a block runs in
					if labels["evaluate triggers"] == "yes" || labels["execute effects"] == "yes" {
						v.ScopeTypes = append(v.ScopeTypes, b.name)
					}
				case "modifiers.log":
					areas := labels["mask"] + "," + labels["use areas"] + "," + labels["categories"]
					v.Modifiers[b.name] = Modifier{Name: b.name, Doc: labels["description"], Categories: commaList(strings.ToLower(areas))}
				case "on_actions.log":
					v.OnActions[b.name] = labels["expected scope"]
				}
			}
		}
	}

	for label, n := range unknown {
		v.Unknown = append(v.Unknown, Tally{Name: label, Count: n})
	}
	slices.SortFunc(v.Unknown, func(a, b Tally) int { return strings.Compare(a.Name, b.Name) })
	return v
}

// dumpBlocks splits a dump into its entries. The layout is the first thing the file shows: a markdown heading, a dashed rule, or neither. The link dump of every game ends with a bare list of the scope names the engine saves itself, below a line that says Saved from Code; those come back as saved.
func dumpBlocks(text string) (blocks []dumpBlock, saved []string) {
	text = strings.ToValidUTF8(strings.ReplaceAll(strings.TrimPrefix(text, "\xef\xbb\xbf"), "\r", ""), string(utf8.RuneError))
	if list := strings.Index(text, "Saved from Code:"); list >= 0 {
		saved = strings.Fields(text[list+len("Saved from Code:"):])
		text = text[:strings.LastIndexByte(text[:list], '\n')+1]
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

	ruled := false
	for _, line := range lines {
		name, tail, isHeader := "", "", false
		if strings.HasPrefix(line, "----") {
			ruled = true
			continue
		}

		switch layout {
		case "markdown":
			// ## name or ### name, never indented: a scripted body quoted below a heading may hold its own comments
			if rest := strings.TrimLeft(line, "#"); strings.HasPrefix(line, "##") && strings.HasPrefix(rest, " ") {
				name, isHeader = rest, true
			}
		case "classic":
			// the first line after a rule: name - description, or name: alone
			if ruled && strings.TrimSpace(line) != "" {
				ruled, isHeader = false, true
				name, tail, _ = strings.Cut(line, " - ")
				name = strings.TrimSuffix(strings.TrimSpace(name), ":")
			}
		case "bare":
			// Tag: name, Categories: a, b  or  name: alone, never indented, so a body's label line starts nothing
			if rest, ok := strings.CutPrefix(line, "Tag: "); ok {
				name, tail, _ = strings.Cut(rest, ",")
				isHeader = true
			} else if word, ok := strings.CutSuffix(line, ":"); ok && word != "" && !strings.ContainsAny(word, " \t:") {
				name, isHeader = word, true
			}
		}

		if isHeader {
			blocks = append(blocks, dumpBlock{name: strings.TrimSpace(name), tail: strings.TrimSpace(tail)})
		} else if len(blocks) > 0 {
			blocks[len(blocks)-1].body = append(blocks[len(blocks)-1].body, line)
		}
	}
	return blocks, saved
}

// dumpFields reads one entry: its description, its signature, and its labelled lines by lower-case label. A label ends a signature, because CK3 writes switch = { and never closes it. A label-shaped line outside dumpLabels is prose, and is counted in unknown when it is written the way the games write theirs: two capitalised words.
func dumpFields(b dumpBlock, unknown map[string]int) (doc, usage string, labels map[string]string) {
	labels = map[string]string{}
	var prose, signature []string
	depth := 0

	// a header's tail is read like the lines below it: a description, a modifier's first label, or a signature that opens on the header line
	for _, line := range append([]string{b.tail}, b.body...) {
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

		// a signature is a key = value line, the token restated among them; it runs on while its braces are open
		key := trimmed[:strings.IndexFunc(trimmed+" ", func(r rune) bool {
			return !(r == '_' || r == '.' || r == '/' || unicode.IsLetter(r) || unicode.IsDigit(r))
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

// dumpLabel splits a line written Label: value. A label is at most three words and 24 characters, and comes back lower-cased.
func dumpLabel(line string) (label, value string, ok bool) {
	label, value, ok = strings.Cut(strings.TrimSpace(line), ":")
	if !ok || label == "" || len(label) > 24 || len(strings.Fields(label)) > 3 || strings.ContainsAny(label, "={}<>") {
		return "", "", false
	}
	return strings.ToLower(strings.TrimSpace(label)), strings.TrimSpace(value), true
}

// commaList splits a, b, c and drops the empty entries the games leave behind in "Location, , All, ".
func commaList(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// TokenParams is the keys a token's block takes, read from its signature: every key assigned directly inside the outer braces, <count=num/all> included. The four spellings of an iterator are read as one, because the games declare an argument on one and omit it on another.
func (v *Vocab) TokenParams(name string) []string {
	family := []string{name}
	for _, stem := range stems(name) {
		family = nil
		for _, prefix := range iteratorPrefixes {
			family = append(family, prefix+stem)
		}
	}

	var params []string
	for _, member := range family {
		usage := v.Effects[member].Usage + "\n" + v.Triggers[member].Usage
		depth := 0
		for i := 0; i < len(usage); i++ {
			switch c := usage[i]; {
			case c == '{':
				depth++
			case c == '}':
				depth--
			case depth == 1 && (c == '_' || unicode.IsLetter(rune(c))) && (i == 0 || !(usage[i-1] == '_' || unicode.IsLetter(rune(usage[i-1])) || unicode.IsDigit(rune(usage[i-1])))):
				// a whole word, followed by =
				end := i
				for end < len(usage) && (usage[end] == '_' || unicode.IsLetter(rune(usage[end])) || unicode.IsDigit(rune(usage[end]))) {
					end++
				}
				if rest := strings.TrimLeft(usage[end:], " \t"); strings.HasPrefix(rest, "=") && !slices.Contains(params, usage[i:end]) {
					params = append(params, usage[i:end])
				}
				i = end - 1
			}
		}
	}
	slices.Sort(params)
	return params
}

// UsableIn reports whether something declared for the scope types in may be written in scope. No list, a list that says none, and an unknown scope all answer yes: an unknown never accuses.
func UsableIn(in []string, scope string) bool {
	return len(in) == 0 || scope == "" || slices.Contains(in, "none") || slices.Contains(in, scope)
}
