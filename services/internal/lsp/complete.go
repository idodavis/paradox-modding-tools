// complete.go is what may be written at the cursor, read from the walk that stops there: at a key,
// the keys the block's position holds and the commands, links and names its kind and scope allow;
// at a value, the words its position declares or vanilla writes, or a database's names. Resolve
// reads an item's place again for what its label names.

package lsp

import (
	"cmp"
	"maps"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/index"
	"paradox-modding-tools/services/internal/inventory"
	"paradox-modding-tools/services/internal/jomini"
	"paradox-modding-tools/services/internal/schema"
	"paradox-modding-tools/services/internal/workspace"
)

// maxComplete caps the list; a cut list is marked incomplete, so the editor asks again.
const maxComplete = 80

// Item is one completion: its text, what it is, what may follow, and its rank (0 stated or in
// scope, 1 vanilla writes it, 2 a name, a link or a command for another scope, 3 a loc key; in a
// data call 1 a function, 2 a type).
type Item struct {
	Label   string `json:"label"`
	Head    string `json:"head"`
	Snippet string `json:"snippet,omitempty"` // the editor snippet a key writes: its block, or key = and a value
	Rank    int    `json:"rank"`
	Range   Range  `json:"range"`
}

// Completion is the list, and whether the cap cut it.
type Completion struct {
	Items      []Item `json:"items"`
	Incomplete bool   `json:"incomplete"`
}

// completer gathers the items that start with the typed prefix, each once, and what each names.
type completer struct {
	file   *file
	prefix string
	at     Range
	items  []Item
	about  map[string]reading
}

// Complete is what may be written at a position.
func Complete(sess *workspace.Session, path string, line, col int) (*Completion, error) {
	sess.RLock()
	defer sess.RUnlock()
	file, err := open(sess, path)
	if err != nil {
		return nil, err
	}
	return file.complete(file.parsed.Lines().OffsetAt(line, col), "").list(), nil
}

// complete gathers what may be written at an offset for the word so far, or a label (Resolve): in a
// data call the part after its [ or dot, else a script word, a prefixed one with its colon.
func (file *file) complete(offset int, label string) *completer {
	completer := &completer{file: file, about: map[string]reading{}}
	lineStart := strings.LastIndexByte(file.text[:offset], '\n') + 1
	bracket := strings.LastIndexByte(file.text[lineStart:offset], '[')
	data := bracket >= 0 && !strings.Contains(file.text[lineStart+bracket:offset], "]")
	start, end := offset, offset
	switch {
	case data:
		written := file.text[lineStart+bracket : offset]
		start = offset - len(written) + strings.LastIndexAny(written, "[.") + 1
	case file.place.Format != inventory.Script || file.place.Rel == "":
		return completer
	default:
		_, start, end = file.parsed.TokenAt(offset)
		if end < offset || start > offset {
			start, end = offset, offset
		}
		for start > 0 && (isWord(file.text[start-1]) || file.text[start-1] == ':') {
			start--
		}
	}
	completer.prefix = strings.ToLower(cmp.Or(label, file.text[start:offset]))
	completer.at = file.span(jomini.Range{Start: start, End: end})
	if data {
		completer.calls(file.text[lineStart+bracket : start])
		return completer
	}

	// a prefixed word: the names of the database its link prefix reads (Schema.Prefixes), spelled
	// whole, and the scopes the game saves that Resolve knows there
	sch, prefix := file.sess.Install.Schema, completer.prefix
	if colon := strings.LastIndexByte(prefix, ':'); colon >= 0 {
		link := prefix[strings.LastIndexByte(prefix[:colon], '.')+1 : colon]
		database := sch.Prefixes[link]
		var saved []string
		for _, name := range slices.Sorted(maps.Keys(sch.Saved)) {
			if _, known := index.Resolve(sch, file.sess.Indexes(), index.Ref{Database: database, Name: name}); known {
				saved = append(saved, name)
			}
		}
		if sch.Link(link) != nil {
			completer.names(database, prefix[:colon+1], 2, saved...)
		}
		return completer
	}

	// at a value, what the statement being written takes; else a key
	cursor, before := file.walk(start), strings.TrimRight(file.text[:start], " \t")
	if before != "" && strings.ContainsRune("=<>!?", rune(before[len(before)-1])) {
		completer.values(cursor)
	} else {
		completer.keys(cursor)
	}
	return completer
}

// list is the items by rank, cut at maxComplete.
func (completer *completer) list() *Completion {
	slices.SortStableFunc(completer.items, func(first, second Item) int { return first.Rank - second.Rank })
	out := &Completion{Items: completer.items}
	if len(out.Items) > maxComplete {
		out.Items, out.Incomplete = out.Items[:maxComplete], true
	}
	return out
}

// add keeps an item whose label starts with what was typed, once, with what its label names.
func (completer *completer) add(item Item, read reading) {
	if _, seen := completer.about[item.Label]; seen || !strings.HasPrefix(strings.ToLower(item.Label), completer.prefix) {
		return
	}
	item.Range = completer.at
	completer.items, completer.about[item.Label] = append(completer.items, item), read
}

// names offers a database's names and more, each prefixed with spelled (scope:).
func (completer *completer) names(database, spelled string, rank int, more ...string) {
	if database == "" {
		return
	}
	for _, name := range append(index.Names(completer.file.sess.Indexes(), database), more...) {
		completer.add(Item{Label: spelled + name, Head: game.KindName(database), Rank: rank},
			reading{ref: &index.Ref{Database: database, Name: name}})
	}
}

// keys offers the keys of the cursor's block: a call's parameters, its position's keys, the
// commands its kind allows (in scope first), links, the names it calls; at the top, its database's.
func (completer *completer) keys(cursor spot) {
	sess, sch := completer.file.sess, completer.file.sess.Install.Schema
	holder := cursor.block()
	if holder < 0 {
		if database := completer.file.place.Database; database != nil {
			completer.names(database.Name, "", 2)
		}
		return
	}
	at := cursor.chain[holder]

	// a call's block passes the parameters the called entry defines, <entry>:<NAME>
	assign, isAssign := at.Statement.(*jomini.Assignment)
	if found := slices.IndexFunc(cursor.records.Refs, func(ref index.Ref) bool {
		return isAssign && ref.Called != "" && ref.Range == assign.Key.Range
	}); found >= 0 {
		passed := cursor.records.Refs[found].Name + ":"
		for _, name := range index.Names(sess.Indexes(), index.Parameter) {
			if parameter, isPassed := strings.CutPrefix(name, passed); isPassed {
				completer.add(Item{Label: parameter, Head: heading(index.Parameter)},
					reading{ref: &index.Ref{Database: index.Parameter, Name: name}})
			}
		}
	}
	// a list's words are what its key takes (events = { v.1 }); a block nothing classifies offers
	// what the body around it offers
	completer.values(spot{chain: cursor.chain[:holder+1]})
	inner := at.Inner
	if inner.Key == nil && len(completer.items) == 0 && holder > 0 {
		inner = cursor.chain[holder-1].Inner
	}
	if inner.Key == nil {
		return
	}

	// a value block's operators; allowed is for the commands below
	scope, called, allowed := inner.Scope, inner.Calls, game.Blocks[inner.Kind].Allows
	if inner.InTrigger {
		allowed &^= game.KindEffect
	}
	for _, name := range slices.Sorted(maps.Keys(game.ValueOperators)) {
		if step := sch.Step(inner, nil, name, false); step.Rule == game.StepOperator {
			completer.add(Item{Label: name, Head: need(step.Key)}, reading{key: step.Key, scope: scope})
		}
	}

	// the position's keys, stated first, read as the walker reads them; one left to calls is a call
	names, stated := keys(inner.Key)
	for i, name := range names {
		position := sch.Step(inner, nil, name, false).Key
		read := reading{key: position, scope: scope}
		if position == nil {
			read = reading{ref: &index.Ref{Called: called, Name: name}}
		}
		item := Item{Label: name, Head: need(position), Snippet: snippet(name, position), Rank: 1}
		if i < stated {
			item.Rank = 0
		}
		completer.add(item, read)
	}
	// the commands the block's kind allows, in scope first; a Wild Card link has no name to write
	for _, name := range slices.Sorted(maps.Keys(sch.Commands)) {
		command := sch.Commands[name]
		if allowed == 0 || command.Kind&(allowed|game.KindLink) == 0 || command.Wild {
			continue
		}
		item := Item{Label: name, Head: kinds(command), Rank: 2,
			Snippet: snippet(name, command.Args)}
		inScope := scope == "" || len(command.Scopes) == 0 || slices.Contains(command.Scopes, scope)
		if command.Kind&allowed != 0 && inScope {
			item.Rank = 0
		}
		completer.add(item, reading{command: command, scope: scope})
	}

	// the names called here: the entries of the databases a call by the block's kind resolves in
	for _, name := range sch.Callees[called] {
		completer.names(name, "", 2)
	}
}

// values offers what the statement takes: its declared words, vanilla's (most written first), a
// referenced database's names or loc keys, and the names its command makes or reads (Names).
func (completer *completer) values(cursor spot) {
	if len(cursor.chain) == 0 {
		return
	}
	at := cursor.chain[len(cursor.chain)-1]
	if _, isAssign := at.Statement.(*jomini.Assignment); !isAssign {
		return
	}
	if position := at.Key; position != nil {
		claim := position.Value.Value
		words := claim.Words
		if claim.Type == schema.WordBool {
			words = game.Bools
		}
		for _, word := range words {
			completer.add(Item{Label: word}, reading{})
		}
		written := position.Seen.Values
		for _, word := range slices.SortedFunc(maps.Keys(written), func(first, second string) int {
			return cmp.Or(written[second]-written[first], strings.Compare(first, second))
		}) {
			completer.add(Item{Label: word, Rank: 1}, reading{})
		}
		switch claim.Type {
		case schema.WordReference:
			completer.names(claim.Database, "", 2)
		case schema.WordLoc:
			for _, key := range completer.file.sess.LocKeys(completer.prefix, maxComplete+1) {
				completer.add(Item{Label: key, Head: heading(index.Localization), Rank: 3},
					reading{ref: &index.Ref{Database: index.Localization, Name: key}})
			}
		}
	}

	// the names of the database its command makes or reads them in (set_variable, has_variable)
	completer.names(at.Names, "", 2)
}

// calls offers what may follow in a data call: after [ the global functions, data types and data
// function or concept entries; after a dot the functions of the type the steps lead to.
func (completer *completer) calls(written string) {
	sch := completer.file.sess.Install.Schema
	types := sch.DataTypes
	if dot := strings.LastIndexByte(written, '.'); dot >= 0 {
		for _, name := range slices.Sorted(maps.Keys(types[leads(types, jomini.ParseCall(written[:dot], 0))].Functions)) {
			completer.add(Item{Label: name, Head: "data function", Rank: 1}, reading{})
		}
		return
	}
	for _, name := range slices.Sorted(maps.Keys(types[game.GlobalType].Functions)) {
		completer.add(Item{Label: name, Head: "data function", Rank: 1}, reading{})
	}
	for _, name := range slices.Sorted(maps.Keys(types)) {
		completer.add(Item{Label: name, Head: "data type", Rank: 2}, reading{})
	}
	for _, name := range sch.Flagged(func(row game.Folder) bool { return row.Functions || row.Concepts }) {
		completer.names(name, "", 2)
	}
}

// snippet is the editor snippet a key writes: a block where a source declares one or vanilla writes
// only blocks there, else key = and a value where either says it takes one; "" where neither does.
func snippet(name string, position *schema.Key) string {
	switch {
	case position == nil:
		return ""
	case position.Block.From != schema.SourceNone || position.Seen.Forms&^schema.FormCommand == schema.FormBlock:
		return name + " = {\n\t$0\n}"
	case position.Value.From != schema.SourceNone || position.Seen.Forms&^(schema.FormBlock|schema.FormCommand) != 0:
		return name + " = $0"
	}
	return ""
}

// isWord reports a byte a name is spelled with.
func isWord(char byte) bool {
	return char == '_' || char == '.' || char == '@' || char == '$' || ('a' <= char && char <= 'z') || ('A' <= char && char <= 'Z') || ('0' <= char && char <= '9')
}

// Resolve is an item's card, fetched when the editor shows it: what its label names where it was
// offered, read again there; nil where nothing is known.
func Resolve(sess *workspace.Session, path string, item Item) (*Inspect, error) {
	sess.RLock()
	defer sess.RUnlock()
	file, err := open(sess, path)
	if err != nil {
		return nil, err
	}
	start := file.parsed.Lines().OffsetAt(item.Range.Start.Line, item.Range.Start.Character)
	read, offered := file.complete(start, item.Label).about[item.Label]
	card := &Inspect{Word: item.Label, Winner: -1}
	if !offered || !file.describe(card, read) && len(card.Writes) == 0 {
		return nil, nil
	}
	return card, nil
}
