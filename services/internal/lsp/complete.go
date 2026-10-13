// complete.go is what may be written at the cursor: at a key, the block's keys, commands, links and
// names; at a value, what its position declares or vanilla writes, or a database's names; in a data
// call, its next step. Resolve reads an item's place again for the card of what its label names.

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

// completer gathers the items that start with the typed prefix, each once, and what each stands
// for.
type completer struct {
	file   *openFile
	prefix string
	at     Range
	items  []Item
	about  map[string]subject
}

// partial is the text being typed at the cursor: where it starts and ends, and in a data call the
// call's text from its [ up to it ("" outside one).
type partial struct {
	start, end int
	call       string
}

// Complete is what may be written at a position of a script or localization file, the best first,
// at most maxComplete of it.
func Complete(ws *workspace.Session, path string, line, col int) (*Completion, error) {
	ws.RLock()
	defer ws.RUnlock()
	file, err := open(ws, path)
	if err != nil {
		return nil, err
	}
	return file.complete(file.parsed.Lines().OffsetAt(line, col), "").list(), nil
}

// partialAt finds the text being typed at an offset: the step of a data call, else the name the
// cursor is in, walked back over a prefixed name (trait:br). It is false in a file that is no
// script under a root, outside a data call.
func (file *openFile) partialAt(offset int) (partial, bool) {
	lines := file.parsed.Lines()
	lineStart := lines.OffsetAt(lines.PositionAt(offset).Line, 0)
	lineText := file.text[lineStart:offset]
	if bracket := strings.LastIndex(lineText, "["); bracket >= 0 && !strings.Contains(lineText[bracket:], "]") {
		start := lineStart + bracket + strings.LastIndexAny(lineText[bracket:], "[.") + 1
		return partial{start: start, end: offset, call: file.text[lineStart+bracket : start]}, true
	}
	if file.place.Format != inventory.Script || file.place.Rel == "" {
		return partial{}, false
	}
	_, start, end := file.parsed.TokenAt(offset)
	start, end = min(start, offset), max(end, offset)
	for start > 0 && (isNameByte(file.text[start-1]) || file.text[start-1] == ':') {
		start--
	}
	return partial{start: start, end: end}, true
}

// complete gathers the items for the text typed at an offset, or for a label an item offered
// (Resolve): at a value what the position takes, in a data call its next step, after a prefix its
// names, else the block's keys.
func (file *openFile) complete(offset int, label string) *completer {
	completer := &completer{file: file, about: map[string]subject{}}
	partial, isPartial := file.partialAt(offset)
	if !isPartial {
		return completer
	}
	completer.prefix = strings.ToLower(cmp.Or(label, file.text[partial.start:offset]))
	completer.at = file.span(jomini.Range{Start: partial.start, End: partial.end})
	textBefore := strings.TrimRight(file.text[:partial.start], " \t\r\n")
	atValue := strings.ContainsAny(textBefore[max(len(textBefore)-1, 0):], "=<>!?")
	switch {
	case partial.call != "":
		completer.dataCalls(partial.call)
	case strings.Contains(completer.prefix, ":"):
		completer.prefixedNames()
	case atValue:
		if cursor := file.walkTo(partial.start); len(cursor.chain) > 0 {
			completer.values(cursor.chain[len(cursor.chain)-1])
		}
	default:
		completer.keys(file.walkTo(partial.start))
	}
	return completer
}

// list is the items best first (by rank, in the order they were added within one), cut at
// maxComplete.
func (completer *completer) list() *Completion {
	slices.SortStableFunc(completer.items, func(first, second Item) int { return cmp.Compare(first.Rank, second.Rank) })
	cut := len(completer.items) > maxComplete
	if cut {
		completer.items = completer.items[:maxComplete]
	}
	return &Completion{Items: completer.items, Incomplete: cut}
}

// add keeps an item whose lower-cased label starts with the typed prefix, once, and remembers what
// it stands for.
func (completer *completer) add(item Item, about subject) {
	if _, kept := completer.about[item.Label]; kept || !strings.HasPrefix(strings.ToLower(item.Label), completer.prefix) {
		return
	}
	item.Range = completer.at
	completer.items = append(completer.items, item)
	completer.about[item.Label] = about
}

// names offers a database's names and more, each written after spelled, as references to them.
func (completer *completer) names(database, spelled string, rank int, more ...string) {
	if database == "" {
		return
	}
	for _, name := range append(index.Names(completer.file.ws.Indexes(), database), more...) {
		reference := subject{ref: &schema.Occurrence{Database: database, ID: name}}
		completer.add(Item{Label: spelled + name, Head: game.KindName(database), Rank: rank}, reference)
	}
}

// prefixedNames offers the names a link's prefix is followed by (trait:, title:, scope:): its
// database's names and the scopes the game saves, written after the prefix typed so far.
func (completer *completer) prefixedNames() {
	ws := completer.file.ws
	s := ws.Install.Schema
	colon := strings.LastIndex(completer.prefix, ":")
	link := completer.prefix[:colon]
	link = link[strings.LastIndex(link, ".")+1:]
	database := s.Prefixes[link]
	var saved []string
	for _, name := range slices.Sorted(maps.Keys(s.Saved)) {
		if _, known := index.Resolve(s, ws.Indexes(), schema.Occurrence{Database: database, ID: name}, completer.file.place.Path); known {
			saved = append(saved, name)
		}
	}
	if s.Link(link) != nil {
		completer.names(database, completer.prefix[:colon+1], 2, saved...)
	}
}

// keys offers what may be written as a key where the cursor is: at the file's top the file's
// database's names; in a block its holder's arguments and words, then what its context reads
// (operators, keys, commands and links) and the names its calls resolve in.
func (completer *completer) keys(cursor cursor) {
	s := completer.file.ws.Install.Schema
	holder := cursor.blockAt()
	if holder < 0 {
		if database := completer.file.place.Database; database != nil {
			completer.names(database.Name, "", 2)
		}
		return
	}
	at := cursor.chain[holder]
	completer.parameters(cursor, at)
	completer.values(at)
	inner := at.inner
	if inner.At == nil && len(completer.items) == 0 && holder > 0 {
		inner = cursor.chain[holder-1].inner
	}
	if inner.At == nil {
		return
	}
	completer.operators(inner)
	completer.blockKeys(inner)
	completer.commands(inner)
	for _, name := range s.Callees[inner.Caller] {
		completer.names(name, "", 2)
	}
	if inner.Caller == game.Shapes[game.ShapeWidget].Caller {
		for _, kind := range slices.Sorted(maps.Keys(s.WidgetKinds)) {
			kindCall := subject{ref: &schema.Occurrence{Role: schema.Refers, Caller: inner.Caller, ID: kind}}
			completer.add(Item{Label: kind, Head: builtInHeading(inner.Caller), Rank: 2}, kindCall)
		}
	}
}

// parameters offers the parameters of the scripted entry a block's key calls, as the keys the call
// passes.
func (completer *completer) parameters(cursor cursor, at step) {
	assign, isAssign := at.statement.(*jomini.Assignment)
	if !isAssign {
		return
	}
	call := slices.IndexFunc(cursor.occurrences, func(occurrence schema.Occurrence) bool {
		return occurrence.Role == schema.Refers && occurrence.Caller != "" && occurrence.Range == assign.Key.Range
	})
	if call < 0 {
		return
	}
	spelled := cursor.occurrences[call].ID + ":"
	for _, name := range index.Names(completer.file.ws.Indexes(), schema.Parameter) {
		if parameter, isParameter := strings.CutPrefix(name, spelled); isParameter {
			completer.add(Item{Label: parameter, Head: heading(schema.Parameter)}, subject{ref: &schema.Occurrence{Database: schema.Parameter, ID: name}})
		}
	}
}

// operators offers the operators a value block writes (add, multiply, min, max ...).
func (completer *completer) operators(inner schema.Context) {
	s := completer.file.ws.Install.Schema
	var names []string
	for _, construct := range game.Constructs {
		if construct.Within == game.ShapeValue {
			names = append(names, construct.Key)
		}
	}
	slices.Sort(names)
	for _, name := range slices.Compact(names) {
		entered := s.Enter(inner, &jomini.Assignment{Key: jomini.Scalar{Text: name}}, nil, false)
		if entered.Rule == game.KeyRuleOperator {
			position := entered.Position()
			completer.add(Item{Label: name, Head: requirementText(position)}, subject{key: position, scope: inner.Scope})
		}
	}
}

// blockKeys offers the keys the block's position holds, those a declared source states first, then
// those vanilla writes; a key with no position of its own is a call to a scripted entry.
func (completer *completer) blockKeys(inner schema.Context) {
	s := completer.file.ws.Install.Schema
	names, stated := positionKeys(inner.At)
	for i, name := range names {
		position := s.Enter(inner, &jomini.Assignment{Key: jomini.Scalar{Text: name}}, nil, false).Position()
		rank := 1
		if i < stated {
			rank = 0
		}
		about := subject{key: position, scope: inner.Scope}
		if position == nil {
			about.ref = &schema.Occurrence{Caller: inner.Caller, ID: name}
		}
		completer.add(Item{Label: name, Head: requirementText(position), Snippet: snippet(name, position), Rank: rank}, about)
	}
}

// commands offers the effects, triggers and links the block allows (no effect inside a trigger):
// those that run in the block's scope first, the others after.
func (completer *completer) commands(inner schema.Context) {
	s := completer.file.ws.Install.Schema
	allowed := game.Shapes[inner.Shape].Allows
	if inner.InTrigger {
		allowed &^= game.KindEffect
	}
	if allowed == 0 {
		return
	}
	for _, name := range slices.Sorted(maps.Keys(s.Commands)) {
		command := s.Commands[name]
		if command.Kinds&(allowed|game.KindLink) == 0 || command.Wild {
			continue
		}
		runsHere := inner.Scope.This == "" || len(command.Scopes) == 0 || slices.Contains(command.Scopes, inner.Scope.This)
		rank := 2
		if command.Kinds&allowed != 0 && runsHere {
			rank = 0
		}
		completer.add(Item{Label: name, Head: kindText(command), Snippet: snippet(name, command.Args), Rank: rank},
			subject{command: command, scope: inner.Scope})
	}
}

// values offers what a statement's key takes: the words its position declares, those vanilla writes
// there, the names of the database it refers to or the localization keys it names, and the names
// its command makes or reads.
func (completer *completer) values(at step) {
	if _, isAssign := at.statement.(*jomini.Assignment); !isAssign {
		return
	}
	ws := completer.file.ws
	if position := at.inner.Position(); position != nil {
		claim := position.Value.Value
		words := claim.Choices
		if claim.Type == schema.ValueBool {
			words = game.Bools
		}
		for _, word := range words {
			completer.add(Item{Label: word}, subject{})
		}
		vanillaWords := slices.SortedFunc(maps.Keys(position.Vanilla.Values), func(first, second string) int {
			return cmp.Or(position.Vanilla.Values[second]-position.Vanilla.Values[first], strings.Compare(first, second))
		})
		for _, word := range vanillaWords {
			completer.add(Item{Label: word, Rank: 1}, subject{})
		}
		if claim.Type == schema.ValueReference {
			completer.names(claim.Database, "", 2)
		}
		if claim.Type == schema.ValueLoc {
			for _, key := range ws.LocKeys(completer.prefix, maxComplete+1) {
				reference := subject{ref: &schema.Occurrence{Database: game.Localization, ID: key}}
				completer.add(Item{Label: key, Head: heading(game.Localization), Rank: 3}, reference)
			}
		}
	}
	completer.names(schema.StoredIn(at.context, at.statement), "", 2)
}

// dataCalls offers the next step of a data call: after a dot the functions of the type the steps
// before lead to, else the global functions, the data types and the names of the functions and
// concepts databases.
func (completer *completer) dataCalls(call string) {
	s := completer.file.ws.Install.Schema
	if strings.HasSuffix(call, ".") {
		typeName := s.CallLeadsTo(jomini.ParseCall(call[:len(call)-1], 0), "")
		for _, name := range slices.Sorted(maps.Keys(s.DataTypes[typeName].Functions)) {
			completer.add(Item{Label: name, Head: "data function", Rank: 1}, subject{})
		}
		return
	}
	for _, name := range slices.Sorted(maps.Keys(s.DataTypes[game.GlobalType].Functions)) {
		completer.add(Item{Label: name, Head: "data function", Rank: 1}, subject{})
	}
	for _, name := range slices.Sorted(maps.Keys(s.DataTypes)) {
		completer.add(Item{Label: name, Head: "data type", Rank: 2}, subject{})
	}
	for _, name := range s.DatabasesWhere(readInDataCalls) {
		completer.names(name, "", 2)
	}
}

// snippet is the editor snippet a key writes: its block where a source declares it holds one or
// vanilla writes only blocks there, key = and a value where a source declares its value or vanilla
// writes one; "" for neither.
func snippet(name string, position *schema.Position) string {
	switch {
	case position == nil:
		return ""
	case position.Holds.Source != schema.SourceNone || position.Vanilla.Writes&^schema.WritesCommand == schema.WritesBlock:
		return name + " = {\n\t$0\n}"
	case position.Value.Source != schema.SourceNone || position.Vanilla.Writes&^(schema.WritesBlock|schema.WritesCommand) != 0:
		return name + " = $0"
	}
	return ""
}

// isNameByte reports a byte a name is written with: a letter, a digit, or one of _ . @ $.
func isNameByte(char byte) bool {
	return char == '_' || char == '.' || char == '@' || char == '$' ||
		char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9'
}

// Resolve is the card of what an item's label names, read again from the place the item was offered
// at; nil where the label is not offered there or the card says nothing.
func Resolve(ws *workspace.Session, path string, item Item) (*Inspect, error) {
	ws.RLock()
	defer ws.RUnlock()
	file, err := open(ws, path)
	if err != nil {
		return nil, err
	}
	start := file.parsed.Lines().OffsetAt(item.Range.Start.Line, item.Range.Start.Character)
	about, offered := file.complete(start, item.Label).about[item.Label]
	if !offered {
		return nil, nil
	}
	card := &Inspect{Word: item.Label, Winner: -1}
	if !file.describe(card, about) && len(card.Writes) == 0 {
		return nil, nil
	}
	return card, nil
}
