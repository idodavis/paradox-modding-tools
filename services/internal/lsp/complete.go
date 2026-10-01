// complete.go is what may be written at the cursor: at a key, what the enclosing key holds (a
// body's declared commands and its class's scripted effects, a structure's keys, a database's
// names); at a value, what the key takes. Items carry the reader's value of what they write; their
// docs are fetched when one is shown.
package lsp

import (
	"cmp"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/lang/index"
	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/schema"
	"paradox-modding-tools/services/internal/lang/source"
	"paradox-modding-tools/services/internal/session"
)

// maxComplete caps the list; a cut list is marked incomplete, so the editor asks again.
const maxComplete = 80

// Item is one completion: the text it writes, what that text is, its head and takes, and its rank
// (0 a command in scope or a key every entry writes, 1 a key, 2 a definition or a command out of
// scope, 3 a loc key).
type Item struct {
	Label string      `json:"label"`
	Word  schema.Word `json:"word"`
	Head  string      `json:"head"`
	Takes []string    `json:"takes,omitempty"`
	Rank  int         `json:"rank"`
	Range Range       `json:"range"`
}

// Completion is the list, and whether the cap cut it.
type Completion struct {
	Items      []Item `json:"items"`
	Incomplete bool   `json:"incomplete"`
}

// Complete is what may be written at a position.
func Complete(sess *session.Session, path string, line, col int) (*Completion, error) {
	sess.RLock()
	defer sess.RUnlock()
	file, err := open(sess, path)
	if err != nil {
		return nil, err
	}
	offset := file.offset(line, col)

	// in a data call's [ … ], in script and localization alike, what may follow the typed part
	lineStart := strings.LastIndexByte(file.text[:offset], '\n') + 1
	if open := strings.LastIndexByte(file.text[lineStart:offset], '['); open >= 0 && !strings.Contains(file.text[lineStart+open:offset], "]") {
		written := file.text[lineStart+open : offset]
		typed := written[strings.LastIndexAny(written, "[.")+1:]
		char := &completer{file: file, prefix: strings.ToLower(typed), at: file.span(jomini.Range{Start: offset - len(typed), End: offset}), seen: map[string]bool{}}
		char.calls(written)
		return char.list(), nil
	}
	if file.place.Format != source.Script || len(file.place.Rel) == 0 {
		return &Completion{}, nil
	}
	_, start, end := file.parsed.TokenAt(offset)
	if end < offset || start > offset {
		start, end = offset, offset
	}
	// a prefixed word is one word: the prefix and the colon before the typed part belong to it
	for start > 0 && (isWord(file.text[start-1]) || file.text[start-1] == ':') {
		start--
	}
	char := &completer{file: file, prefix: strings.ToLower(file.text[start:offset]), at: file.span(jomini.Range{Start: start, End: end}), seen: map[string]bool{}}

	found, _, _ := sess.Located(path)
	reader := index.Probe(found, file.parsed, sess.Reader(), start)
	before := strings.TrimRight(file.text[:start], " \t")
	if len(before) > 0 && strings.ContainsRune("=<>!?", rune(before[len(before)-1])) {
		char.values(reader, strings.TrimRight(before, "=<>!?"))
	} else {
		char.keys(reader)
	}
	return char.list(), nil
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

// calls offers what may follow in a data call: after [ the globals, the data types and the loaded
// data_binding macros; after a step's dot the functions its return type declares.
func (completer *completer) calls(written string) {
	dumps := completer.file.sess.Schema.Dumps
	dot := strings.LastIndexByte(written, '.')
	if dot < 0 {
		for name := range dumps.Globals {
			completer.add(name, schema.Word{Type: schema.WordDataCall, Name: name}, 1)
		}
		for name := range dumps.DataTypes {
			completer.add(name, schema.Word{Type: schema.WordDataCall, Name: name}, 2)
		}
		completer.entries("data_binding", "", 2)
		return
	}
	function, dataType := completer.file.function(jomini.ParseCall(written[:dot], 0))
	for name := range dumps.DataTypes[cmp.Or(dataType, function.Returns)].Functions {
		completer.add(name, schema.Word{Type: schema.WordDataCall, Name: name}, 1)
	}
}

// completer gathers items that start with the typed prefix.
type completer struct {
	file   *file
	prefix string
	at     Range
	items  []Item
	seen   map[string]bool
}

// add keeps an item whose label starts with what was typed, once.
func (completer *completer) add(label string, word schema.Word, rank int) {
	if !strings.HasPrefix(strings.ToLower(label), completer.prefix) || completer.seen[label] {
		return
	}
	item := Item{Label: label, Word: word, Head: completer.file.head(word, label), Rank: rank, Range: completer.at}
	completer.seen[label], completer.items = true, append(completer.items, item)
}

// key offers a key: one every entry writes first, and one written under a condition or optional
// says so in its head.
func (completer *completer) key(name string, key *schema.Key) {
	rank, from := 1, len(completer.items)
	if key.Required == schema.RequiredAlways {
		rank = 0
	}
	completer.add(name, schema.Word{Type: schema.WordKey, Name: name}, rank)
	for i := from; i < len(completer.items); i++ {
		item := &completer.items[i]
		item.Takes = completer.file.takes(key)
		switch key.Required {
		case schema.RequiredWhen:
			item.Head += " · " + key.When
		case schema.RequiredOptional:
			item.Head += " · optional"
		}
	}
}

// enclosing is the statement whose block holds the cursor, that block's key, and the scope it
// runs in, as the descent found it; nil at the top of the file.
func (completer *completer) enclosing(descent *index.Descent) (*index.Level, *schema.Key, string) {
	for _, level := range slices.Backward(descent.Chain) {
		if assignment, ok := level.Statement.(*jomini.Assignment); ok && jomini.BlockOf(assignment.Value) != nil {
			return &level, level.Below, descent.Scopes.This
		}
	}
	return nil, nil, ""
}

// keys offers what may be written as a key here.
func (completer *completer) keys(descent *index.Descent) {
	sess := completer.file.sess
	level, key, scope := completer.enclosing(descent)
	if level == nil {
		for _, database := range completer.file.place.Databases {
			completer.entries(database.Name, "", 2)
		}
		return
	}

	// a block nothing describes offers what the command block around it offers
	body := key
	for i := len(descent.Chain) - 1; body == nil && i >= 0; i-- {
		if descent.Chain[i].Below != nil && descent.Chain[i].Below.Block != schema.BlockNone {
			body = descent.Chain[i].Below
		}
	}
	if key != nil && key.Block == schema.BlockNone {
		for name := range key.Keys {
			if !strings.HasPrefix(name, "<") {
				completer.key(name, key.Keys[name])
			}
		}
		for _, database := range []string{key.Defines, key.Names} {
			if database != "" {
				completer.entries(database, "", 2)
			}
		}
		if key.Written == schema.WrittenList {
			completer.takes(key)
		}
		return
	}
	if body == nil {
		return
	}
	dumps := sess.Schema.Dumps
	switch body.Block {
	case schema.BlockEffects:
		completer.commands(dumps.Effects, scope)
	case schema.BlockTriggers:
		completer.commands(dumps.Triggers, scope)
	}
	for name, target := range dumps.EventTargets {
		if !target.Wild && (len(target.In) == 0 || scope == "" || slices.Contains(target.In, scope)) {
			completer.add(name, schema.Word{Type: schema.WordScopePath, Declared: true}, 2)
		}
	}
	for _, database := range sess.Schema.Databases {
		if slices.Contains(database.CalledIn, body.Block) {
			completer.entries(database.Name, "", 2)
		}
	}
}

// isWord reports a byte a name is spelled with.
func isWord(char byte) bool {
	return char == '_' || char == '.' || char == '@' || char == '$' || ('a' <= char && char <= 'z') || ('A' <= char && char <= 'Z') || ('0' <= char && char <= '9')
}

// commands offers a vocabulary's commands, those usable in the scope first.
func (completer *completer) commands(commands map[string]schema.Command, scope string) {
	for name, command := range commands {
		rank := 2
		if scope == "" || schema.UsableIn(command.In, scope) {
			rank = 0
		}
		completer.add(name, schema.Word{Type: schema.WordCommand, Name: name, Declared: true}, rank)
	}
}

// entries offers a database's definitions, spelled with the prefix (trait:brave) where one was
// typed.
func (completer *completer) entries(database, prefix string, rank int) {
	all := completer.file.sess.All()
	for _, idx := range all {
		for _, def := range idx.Defs {
			if def.Database == database && all.Loaded(idx, def.File) {
				completer.add(prefix+def.Name, schema.Word{Type: schema.WordReference, Database: database, Name: def.Name, Parent: def.Parent}, rank)
			}
		}
	}
}

// values offers what the key of the statement being written takes. The statement may not have
// parsed yet (hidden = ), so its key is read from the text before the operator and its schema key
// from the block around the cursor.
func (completer *completer) values(descent *index.Descent, before string) {
	sess := completer.file.sess
	if len(descent.Chain) == 0 {
		return
	}
	level := descent.Chain[len(descent.Chain)-1]
	at := level.Below
	if _, bare := level.Statement.(*jomini.ValueStmt); bare {
		at = level.At
	}
	key, _, _ := completer.file.parsed.TokenAt(len(strings.TrimRight(before, " \t")) - 1)
	if assignment, ok := level.Statement.(*jomini.Assignment); !ok || assignment.Key.Text != key {
		// the block's statement is a name command whose key this key is: the names the key reads
		if level.Key.Type == schema.WordCommand {
			for _, row := range schema.NameCommandsOf(level.Key.Name) {
				if !row.Defines && row.NameKey == key {
					completer.entries(row.Database, "", 2)
				}
			}
		}
		reader := sess.Reader()
		word := reader.Key(level.Below, key)
		database := sess.Schema.Databases[level.Database]
		level, at = index.Level{Database: level.Database, At: level.Below, Key: word, Scopes: level.Scopes}, reader.Below(database, level.Below, key, word)
	}

	// a prefixed word: the database the prefix belongs to, spelled whole
	if prefix, _, ok := strings.Cut(completer.prefix, ":"); ok {
		for _, database := range sess.Schema.ByPrefix(prefix) {
			completer.entries(database.Name, prefix+":", 2)
		}
		return
	}
	if level.Key.Type == schema.WordCommand {
		for _, row := range schema.NameCommandsOf(level.Key.Name) {
			if !row.Defines && row.NameKey == "" {
				completer.entries(row.Database, "", 2)
			}
		}
	}
	if at != nil {
		completer.takes(at)
	}
}

// takes offers the words a key takes: definitions, enum words, yes and no, loc keys; the words of a
// guessed take are marked guessed.
func (completer *completer) takes(key *schema.Key) {
	for _, take := range key.Takes {
		from := len(completer.items)
		switch take.Type {
		case schema.WordReference:
			completer.entries(take.Database, "", 2)
		case schema.WordEnum:
			for _, enum := range take.Enum {
				completer.add(enum, schema.Word{Type: schema.WordEnum, Name: enum}, 1)
			}
		case schema.WordBool:
			completer.add("yes", schema.Word{Type: schema.WordBool}, 1)
			completer.add("no", schema.Word{Type: schema.WordBool}, 1)
		case schema.WordLoc:
			for _, key := range completer.file.sess.LocKeys(completer.prefix, maxComplete+1) {
				completer.add(key, schema.Word{Type: schema.WordLoc, Name: key}, 3)
			}
		}
		for i := from; take.Guessed && i < len(completer.items); i++ {
			completer.items[i].Head += " · guessed"
		}
	}
}

// Resolve is an item's documentation, fetched when the editor shows it: a command's prose, a
// definition's label or comment, a loc key's text.
func Resolve(sess *session.Session, path string, item Item) (string, error) {
	sess.RLock()
	defer sess.RUnlock()
	word := item.Word
	switch word.Type {
	case schema.WordCommand:
		docs, usage, _, _ := commandDocs(sess.Schema.Dumps, word.Name)
		return strings.TrimSpace(docs + "\n" + usage), nil
	case schema.WordScopePath:
		return sess.Schema.Dumps.EventTargets[item.Label].Doc, nil
	case schema.WordLoc:
		if said, ok := sess.Loc(word.Name, ""); ok {
			return said.Value, nil
		}
	case schema.WordReference:
		file, err := open(sess, path)
		if err != nil {
			return "", err
		}
		card := &Inspect{}
		file.defined(card, word, "")
		return strings.TrimSpace(card.Name + "\n" + card.Prose), nil
	}
	return "", nil
}
