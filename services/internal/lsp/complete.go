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
	"paradox-modding-tools/services/internal/lang/index"
	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/schema"
	"paradox-modding-tools/services/internal/lang/source"
	"paradox-modding-tools/services/internal/session"
)

// maxComplete caps the list; a cut list is marked incomplete, so the editor asks again.
const maxComplete = 80

// Item is one completion: its text, what it is, what may follow, and its rank (0 stated or in
// scope, 1 vanilla writes it, 2 a name, a link or a command for another scope, 3 a loc key).
type Item struct {
	Label   string   `json:"label"`
	Head    string   `json:"head"`
	Takes   []string `json:"takes,omitempty"`
	Snippet string   `json:"snippet,omitempty"` // the editor snippet a key writes: its block, or key = and a value
	Rank    int      `json:"rank"`
	Range   Range    `json:"range"`
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
func Complete(sess *session.Session, path string, line, col int) (*Completion, error) {
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
	case file.place.Format != source.Script || file.place.Rel == "":
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

	// a prefixed word: the names of the database its link prefix reads (index.Prefixed), spelled
	// whole, and the scopes the game saves that Resolve knows there
	sch, prefix := file.sess.Schema(), completer.prefix
	if colon := strings.LastIndexByte(prefix, ':'); colon >= 0 {
		link := prefix[strings.LastIndexByte(prefix[:colon], '.')+1 : colon]
		database := index.Prefixed(sch, link)
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

// names offers a database's names and more, spelled after spelled (scope:), each a reference to it.
func (completer *completer) names(database, spelled string, rank int, more ...string) {
	if database == "" {
		return
	}
	for _, name := range append(index.Names(completer.file.sess.Indexes(), database), more...) {
		completer.add(Item{Label: spelled + name, Head: heading(database), Rank: rank},
			reading{ref: &index.Ref{Database: database, Name: name}})
	}
}

// keys offers the keys of the cursor's block: a call's parameters, its position's keys, the
// commands its kind allows (in scope first), links, the names it calls; at the top, its database's.
func (completer *completer) keys(cursor spot) {
	sess, sch := completer.file.sess, completer.file.sess.Schema()
	holder := cursor.block()
	if holder < 0 {
		if database := completer.file.place.Database; database != nil {
			completer.names(database.Name, "", 2)
		}
		return
	}
	at := cursor.chain[holder]

	// a call's block passes the parameters the called entry defines, <entry>:<NAME> (D-31)
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
	under := at.InnerKey
	if under == nil {
		return
	}

	// how the block reads, as the walk read it, and the word it calls names by (D-24, D-29)
	block, scope, inTrigger := at.InnerBlock, at.InnerScope, at.InnerTrigger
	called := index.Called(block, under)

	// the position's keys, each read where the slot says; one the slot leaves to calls is a call
	names, stated := keys(under)
	for i, name := range names {
		position, _ := sch.Slot(under, name, false)
		read := reading{key: position, scope: scope}
		if position == nil {
			read = reading{ref: &index.Ref{Called: called, Name: name}}
		}
		item := Item{Label: name, Head: need(position), Takes: takes(position), Snippet: snippet(name, position), Rank: 1}
		if i < stated {
			item.Rank = 0
		}
		completer.add(item, read)
	}

	// the commands the kind allows, those running in the scope first, and the links
	allowed := schema.Kinds(0)
	switch {
	case block == schema.BlockFields:
	case inTrigger:
		allowed = schema.KindTrigger
	case block == schema.BlockEffects:
		allowed = schema.KindEffect
	case called == "*":
		allowed = schema.KindEffect | schema.KindTrigger
	}
	for _, name := range slices.Sorted(maps.Keys(sch.Commands)) {
		command := sch.Commands[name]
		if allowed == 0 || command.Kind&(allowed|schema.KindLink) == 0 {
			continue
		}
		item := Item{Label: name, Head: kinds(command), Takes: takes(command.Args), Rank: 2,
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
// referenced database's names or loc keys, and the names made in the database its command makes.
func (completer *completer) values(cursor spot) {
	if len(cursor.chain) == 0 {
		return
	}
	at := cursor.chain[len(cursor.chain)-1]
	assign, isAssign := at.Statement.(*jomini.Assignment)
	if !isAssign {
		return
	}
	if position := at.Key; position != nil {
		claim := position.Value.Value
		words := claim.Words
		if claim.Type == schema.WordBool {
			words = []string{"yes", "no"}
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

	// a name a command makes (index.Made), inside the command whose Args the block reads at
	enclosing := ""
	if holder := cursor.block(); holder >= 0 && cursor.chain[holder].InnerKey != nil {
		for name, command := range completer.file.sess.Schema().Commands {
			if command.Args == cursor.chain[holder].InnerKey {
				enclosing = name
			}
		}
	}
	completer.names(index.Made(enclosing, assign.Key.Text, at.Command != nil), "", 2)
}

// calls offers what may follow in a data call: after [ the global functions, data types and data
// function or concept entries; after a dot the functions of the type the steps lead to.
func (completer *completer) calls(written string) {
	sch := completer.file.sess.Schema()
	types := sch.DataTypes
	if dot := strings.LastIndexByte(written, '.'); dot >= 0 {
		for _, name := range slices.Sorted(maps.Keys(types[leads(types, jomini.ParseCall(written[:dot], 0))].Functions)) {
			completer.add(Item{Label: name, Head: "data function", Rank: 1}, reading{})
		}
		return
	}
	for _, name := range slices.Sorted(maps.Keys(types["Global"].Functions)) {
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
func Resolve(sess *session.Session, path string, item Item) (*Inspect, error) {
	sess.RLock()
	defer sess.RUnlock()
	file, err := open(sess, path)
	if err != nil {
		return nil, err
	}
	start := file.parsed.Lines().OffsetAt(item.Range.Start.Line, item.Range.Start.Character)
	read, offered := file.complete(start, item.Label).about[item.Label]
	card := &Inspect{Word: item.Label, Winner: -1}
	if !offered || !file.describe(card, read) {
		return nil, nil
	}
	return card, nil
}
