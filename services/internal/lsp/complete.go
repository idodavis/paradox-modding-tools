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
	"paradox-modding-tools/services/internal/loc"
	"paradox-modding-tools/services/internal/schema"
	"paradox-modding-tools/services/internal/workspace"
)

// maxComplete caps the list; a cut list is marked incomplete, so the editor asks again.
const maxComplete = 80

// Item is one completion.
type Item struct {
	Label   string `json:"label"`             // the text
	Head    string `json:"head"`              // what it is
	Snippet string `json:"snippet,omitempty"` // the editor snippet a key writes: its block, or key = and a value
	// 0 stated or in scope, 1 vanilla's or a data function, 2 other names, links and types, 3 a loc key
	Rank  int   `json:"rank"`
	Range Range `json:"range"` // the text it replaces
}

// Completion is the list of items.
type Completion struct {
	Items      []Item `json:"items"`
	Incomplete bool   `json:"incomplete"` // the cap cut it
}

// completer gathers the items that start with the typed prefix, each once.
type completer struct {
	file   *openFile
	prefix string // the text typed
	at     Range  // where it is
	items  []Item
	about  map[string]subject // what each item stands for
}

// partial is the text being typed at the cursor.
type partial struct {
	start int    // where it starts
	end   int    // where it ends
	call  string // in a data call, its text from the [ up to it; "" outside one
}

// Complete returns what may be written at a position of a script or localization file, the best first, at most
// maxComplete of it.
func Complete(ws *workspace.Session, path string, line, col int) (*Completion, error) {
	ws.RLock()
	defer ws.RUnlock()
	file, err := open(ws, path)
	if err != nil {
		return nil, err
	}
	return file.complete(file.parsed.Lines().OffsetAt(line, col), "").list(), nil
}

// partialAt finds the text being typed at an offset: the step of a data call, else the word the cursor touches, a
// prefixed name (trait:br) whole. It is false in a comment, and outside a data call in a file that is no script.
func (file *openFile) partialAt(offset int) (partial, bool) {
	isComment := slices.ContainsFunc(file.parsed.Comments, func(comment jomini.Comment) bool {
		return comment.Range.Start < offset && offset <= comment.Range.End
	})
	if isComment {
		return partial{}, false
	}
	lines := file.parsed.Lines()
	lineStart := lines.OffsetAt(lines.PositionAt(offset).Line, 0)
	for _, segment := range slices.Backward(loc.Segments(file.text[lineStart:offset])) {
		if segment.IsOpen {
			bracket := lineStart + segment.Range.Start
			start := bracket + strings.LastIndexAny(file.text[bracket:offset], "[.") + 1
			return partial{start: start, end: offset, call: file.text[bracket:start]}, true
		}
	}
	if file.place.Format != inventory.Script || file.place.Rel == "" {
		return partial{}, false
	}
	start, end := file.parsed.TokenAt(offset)
	return partial{start: min(start, offset), end: max(end, offset)}, true
}

// complete gathers the items for the text typed at an offset, or for a label an item offered: what the position
// takes at a value, the next step in a data call, the names after a prefix, else the block's keys.
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

// list returns the items best first (by rank, then the order added), cut at maxComplete.
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
		reference := subject{occurrence: &schema.Occurrence{Database: database, ID: name}}
		completer.add(Item{Label: spelled + name, Head: game.DescribeEntry(database, false), Rank: rank}, reference)
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

// keys offers what may be written as a key: at the file's top its database's names; in a block its holder's arguments
// and words, what its context reads (operators, keys, commands, links) and the names its calls resolve in.
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
	if inner.BlockPosition == nil && len(completer.items) == 0 && holder > 0 {
		inner = cursor.chain[holder-1].inner
	}
	if inner.BlockPosition == nil {
		return
	}
	completer.operators(inner)
	completer.blockKeys(inner)
	completer.commands(inner)
	for _, name := range s.Callees[inner.Caller] {
		completer.names(name, "", 2)
	}
	if inner.Caller == game.Shapes[game.ShapeWidget].Caller {
		for _, widgetType := range slices.Sorted(maps.Keys(s.WidgetTypes)) {
			typeCall := subject{occurrence: &schema.Occurrence{Role: schema.Refers, Caller: inner.Caller, ID: widgetType}}
			completer.add(Item{Label: widgetType, Head: builtInHeading(inner.Caller), Rank: 2}, typeCall)
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
			completer.add(Item{Label: parameter, Head: game.DescribeEntry(schema.Parameter, true)}, subject{occurrence: &schema.Occurrence{Database: schema.Parameter, ID: name}})
		}
	}
}

// operators offers the operators a value block writes (add, multiply, min, max ...).
func (completer *completer) operators(inner schema.Context) {
	s := completer.file.ws.Install.Schema
	var names []string
	for _, keyword := range game.Keywords {
		if keyword.Holder == game.ShapeValue {
			names = append(names, keyword.Key)
		}
	}
	slices.Sort(names)
	for _, name := range slices.Compact(names) {
		entered := s.Enter(inner, &jomini.Assignment{Key: jomini.Scalar{Text: name}}, nil, false)
		if entered.KeyRule == game.KeyRuleOperator {
			position := entered.Position()
			completer.add(Item{Label: name, Head: requirementText(position)}, subject{position: position, scope: inner.Scope})
		}
	}
}

// blockKeys offers the keys the block's position holds, those a declared source states first, then
// those vanilla writes; a key with no position of its own is a call to a scripted entry.
func (completer *completer) blockKeys(inner schema.Context) {
	s := completer.file.ws.Install.Schema
	names, stated := positionKeys(inner.BlockPosition)
	for i, name := range names {
		position := s.Enter(inner, &jomini.Assignment{Key: jomini.Scalar{Text: name}}, nil, false).Position()
		rank := 1
		if i < stated {
			rank = 0
		}
		about := subject{position: position, scope: inner.Scope}
		if position == nil {
			about.occurrence = &schema.Occurrence{Caller: inner.Caller, ID: name}
		}
		completer.add(Item{Label: name, Head: requirementText(position), Snippet: snippet(name, position), Rank: rank}, about)
	}
}

// isDeclaredAny reports whether the dumps declare a command as any of the words.
func isDeclaredAny(command *schema.Command, words []string) bool {
	return slices.ContainsFunc(command.Declared, func(word string) bool { return slices.Contains(words, word) })
}

// commands offers the effects, triggers and links the block allows (no effect inside a trigger):
// those that run in the block's scope first, the others after.
func (completer *completer) commands(inner schema.Context) {
	s := completer.file.ws.Install.Schema
	allowed := game.Shapes[inner.Shape].Allows
	if inner.InTrigger {
		allowed = slices.DeleteFunc(slices.Clone(allowed), func(word string) bool { return word == game.DeclaredEffect })
	}
	if len(allowed) == 0 {
		return
	}
	for _, name := range slices.Sorted(maps.Keys(s.Commands)) {
		command := s.Commands[name]
		if !isDeclaredAny(command, allowed) && !slices.Contains(command.Declared, game.DeclaredLink) || command.Wild {
			continue
		}
		runsHere := inner.Scope.This == "" || len(command.Scopes) == 0 || slices.Contains(command.Scopes, inner.Scope.This)
		rank := 2
		if isDeclaredAny(command, allowed) && runsHere {
			rank = 0
		}
		completer.add(Item{Label: name, Head: kindText(command), Snippet: snippet(name, command.Args), Rank: rank},
			subject{command: command, scope: inner.Scope})
	}
}

// values offers what a statement's key takes: the words its position declares or vanilla writes there, the names of
// the database it refers to or the localization keys it names, and the names its command makes or reads.
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
				reference := subject{occurrence: &schema.Occurrence{Database: game.Localization, ID: key}}
				completer.add(Item{Label: key, Head: game.DescribeEntry(game.Localization, true), Rank: 3}, reference)
			}
		}
	}
	completer.names(schema.StoredIn(at.context, at.statement), "", 2)
}

// dataCalls offers the next step of a data call: after a dot the functions of the type reached, else the global
// functions, the data types and the functions and concepts databases' names.
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

// snippet returns the editor snippet a key writes: its block where it holds one, key = and a value where it takes
// one; "" for neither.
func snippet(name string, position *schema.Position) string {
	switch {
	case position == nil:
		return ""
	case position.Shape.Source != game.SourceNone || position.Vanilla.Writes&^schema.WritesCommand == schema.WritesBlock:
		return name + " = {\n\t$0\n}"
	case position.Value.Source != game.SourceNone || position.Vanilla.Writes&^(schema.WritesBlock|schema.WritesCommand) != 0:
		return name + " = $0"
	}
	return ""
}

// Resolve returns the card of what an item's label names, read from where the item was offered; nil where the label is
// not offered there or the card says nothing.
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
