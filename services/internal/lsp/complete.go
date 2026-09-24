// complete.go is what may be written at the cursor: at a key, what the enclosing field holds (a
// body's declared tokens and its class's macros, a structure's members, a database's names); at a
// value, what the field accepts. Items carry the reader's value of what they write; their docs are
// fetched when one is shown.
package lsp

import (
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

// Item is one completion: the text it writes, what that text is, and its rank (0 a token in scope,
// 1 a member, 2 a definition or a token out of scope, 3 a loc key).
type Item struct {
	Label string       `json:"label"`
	Value schema.Value `json:"value"`
	Rank  int          `json:"rank"`
	Range Range        `json:"range"`
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
	if file.place.Format != source.Script || len(file.place.Rel) == 0 {
		return &Completion{}, nil
	}
	offset := file.offset(line, col)
	_, start, end := file.parsed.TokenAt(offset)
	if end < offset || start > offset {
		start, end = offset, offset
	}
	// a cite is one word: the prefix and the colon before the typed part belong to it
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

	slices.SortStableFunc(char.items, func(first, second Item) int { return first.Rank - second.Rank })
	out := &Completion{Items: char.items}
	if len(out.Items) > maxComplete {
		out.Items, out.Incomplete = out.Items[:maxComplete], true
	}
	return out, nil
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
func (completer *completer) add(label string, value schema.Value, rank int) {
	if !strings.HasPrefix(strings.ToLower(label), completer.prefix) || completer.seen[label] {
		return
	}
	completer.seen[label], completer.items = true, append(completer.items, Item{Label: label, Value: value, Rank: rank, Range: completer.at})
}

// enclosing is the statement whose block holds the cursor, that block's field, and the scope it
// runs in; nil at the top of the file.
func (completer *completer) enclosing(descent *index.Descent) (*index.Level, *schema.Field, string) {
	sess := completer.file.sess
	for _, level := range slices.Backward(descent.Chain) {

		assignment, ok := level.Statement.(*jomini.Assignment)
		if !ok || jomini.BlockOf(assignment.Value) == nil {
			continue
		}
		scope, vocab, moved := level.Scope, sess.Install.Vocab, false
		switch level.Key.Type {
		case schema.TypeToken:
			moved = true
			if _, _, _, out := tokenDocs(vocab, level.Key.Name); out != "" {
				scope = out
			} else if slices.ContainsFunc(schema.NameTokensOf(level.Key.Name), func(row schema.NameToken) bool { return row.Member != "" && !row.Defines }) {
				scope = ""
			}
		case schema.TypeScopePath:
			last := assignment.Key.Text[strings.LastIndexByte(assignment.Key.Text, '.')+1:]
			if cite, isCite := jomini.Cite(last); isCite {
				last = cite.Prefix
			}
			scope, moved = vocab.Links[strings.ToLower(last)].Out, true
		case schema.TypeRef:
			if cite, isCite := jomini.Cite(assignment.Key.Text); isCite {
				scope, moved = vocab.Links[cite.Prefix].Out, true
			} else if kind := sess.Schema.Kinds[level.Key.Kind]; kind != nil {
				scope, moved = kind.ScopeType, true
			}
		case schema.TypeMember, schema.TypeUnresolved:
			if link, isLink := vocab.Links[strings.ToLower(assignment.Key.Text)]; isLink && !link.Wild {
				scope, moved = link.Out, true
			} else if _, _, _, out := tokenDocs(vocab, strings.ToLower(assignment.Key.Text)); out != "" || level.Key.Type == schema.TypeUnresolved {
				scope, moved = out, true
			}
		}
		if moved && !vocab.IsScopeType(scope) {
			scope = ""
		}
		if !moved && level.Below != nil && vocab.IsScopeType(level.Below.BodyScope) {
			scope = level.Below.BodyScope
		}
		return &level, level.Below, scope
	}
	return nil, nil, ""
}

// keys offers what may be written as a key here.
func (completer *completer) keys(descent *index.Descent) {
	sess := completer.file.sess
	level, field, scope := completer.enclosing(descent)
	if level == nil {
		for _, kind := range completer.file.place.Kinds {
			completer.cited(kind.Name, "", 2)
		}
		return
	}

	// a block nothing classifies offers what the body around it offers
	body := field
	for i := len(descent.Chain) - 1; body == nil && i >= 0; i-- {
		if descent.Chain[i].Below != nil && descent.Chain[i].Below.Class == schema.ClassBody {
			body = descent.Chain[i].Below
		}
	}
	if field != nil && field.Class != schema.ClassBody {
		for name := range field.Members {
			if !strings.HasPrefix(name, "<") {
				completer.add(name, schema.Value{Type: schema.TypeMember, Name: name}, 1)
			}
		}
		if field.Defines != "" {
			completer.cited(field.Defines, "", 2)
		}
		if field.Class == schema.ClassDatabase && level.Kind != nil {
			completer.cited(level.Kind.Name, "", 2)
		}
		if field.Class == schema.ClassList {
			completer.accepted(field)
		}
		return
	}
	if body == nil {
		return
	}
	if body.Body&schema.BodyEffect != 0 {
		completer.tokens(sess.Install.Vocab.Effects, scope)
	}
	if body.Body&schema.BodyTrigger != 0 {
		completer.tokens(sess.Install.Vocab.Triggers, scope)
	}
	for name, link := range sess.Install.Vocab.Links {
		if !link.Prefix && (len(link.In) == 0 || scope == "" || slices.Contains(link.In, scope)) {
			completer.add(name, schema.Value{Type: schema.TypeScopePath, Declared: true}, 2)
		}
	}
	for _, kind := range sess.Schema.Kinds {
		if kind.Macro&body.Body != 0 {
			completer.cited(kind.Name, "", 2)
		}
	}
}

// isWord reports a byte a name is spelled with.
func isWord(char byte) bool {
	return char == '_' || char == '.' || char == '@' || char == '$' || ('a' <= char && char <= 'z') || ('A' <= char && char <= 'Z') || ('0' <= char && char <= '9')
}

// tokens offers a vocabulary's tokens, those usable in the scope first.
func (completer *completer) tokens(tokens map[string]schema.EngineToken, scope string) {
	for name, token := range tokens {
		rank := 2
		if scope == "" || schema.UsableIn(token.In, scope) {
			rank = 0
		}
		completer.add(name, schema.Value{Type: schema.TypeToken, Name: name, Declared: true}, rank)
	}
}

// cited offers a kind's definitions, spelled with the prefix (trait:brave) where one was typed.
func (completer *completer) cited(kind, prefix string, rank int) {
	for _, idx := range completer.file.sess.All() {
		for _, def := range idx.Defs {
			if def.File >= 0 && def.Kind == kind {
				completer.add(prefix+def.Name, schema.Value{Type: schema.TypeRef, Kind: kind, Name: def.Name, Parent: def.Parent}, rank)
			}
		}
	}
}

// values offers what the field of the statement being written accepts. The statement may not have
// parsed yet (hidden = ), so its key is read from the text before the operator and its field from
// the block around the cursor.
func (completer *completer) values(descent *index.Descent, before string) {
	sess := completer.file.sess
	if len(descent.Chain) == 0 {
		return
	}
	level := descent.Chain[len(descent.Chain)-1]
	field := level.Below
	if _, bare := level.Statement.(*jomini.ValueStmt); bare {
		field = level.Field
	}
	key, _, _ := completer.file.parsed.TokenAt(len(strings.TrimRight(before, " \t")) - 1)
	if assignment, ok := level.Statement.(*jomini.Assignment); !ok || assignment.Key.Text != key {
		// the block's statement is a name token whose member this key is: the names the member reads
		if level.Key.Type == schema.TypeToken {
			for _, row := range schema.NameTokensOf(level.Key.Name) {
				if !row.Defines && row.Member == key {
					completer.cited(row.Kind, "", 2)
				}
			}
		}
		reader := sess.Reader()
		value := reader.Key(level.Below, key)
		level, field = index.Level{Kind: level.Kind, Field: level.Below, Key: value, Scope: level.Scope}, reader.Below(level.Kind, level.Below, key, value)
	}

	// a cite: the kind the prefix belongs to, spelled whole
	if prefix, _, ok := strings.Cut(completer.prefix, ":"); ok {
		for _, kind := range sess.Schema.ByPrefix(prefix) {
			completer.cited(kind.Name, prefix+":", 2)
		}
		return
	}
	if level.Key.Type == schema.TypeToken {
		for _, row := range schema.NameTokensOf(level.Key.Name) {
			if !row.Defines && row.Member == "" {
				completer.cited(row.Kind, "", 2)
			}
		}
	}
	if field != nil {
		completer.accepted(field)
	}
}

// accepted offers the words a field accepts: definitions, enum words, yes and no, loc keys.
func (completer *completer) accepted(field *schema.Field) {
	for _, accept := range field.Accepts {
		switch accept.Type {
		case schema.TypeRef:
			completer.cited(accept.Kind, "", 2)
		case schema.TypeEnum:
			for _, enum := range accept.Enum {
				completer.add(enum, schema.Value{Type: schema.TypeEnum, Name: enum}, 1)
			}
		case schema.TypeBool:
			completer.add("yes", schema.Value{Type: schema.TypeBool}, 1)
			completer.add("no", schema.Value{Type: schema.TypeBool}, 1)
		case schema.TypeLoc:
			for _, key := range completer.file.sess.LocKeys(completer.prefix, maxComplete+1) {
				completer.add(key, schema.Value{Type: schema.TypeLoc, Name: key}, 3)
			}
		}
	}
}

// Resolve is an item's documentation, fetched when the editor shows it: a token's prose, a
// definition's label or comment, a loc key's text.
func Resolve(sess *session.Session, path string, item Item) (string, error) {
	sess.RLock()
	defer sess.RUnlock()
	value := item.Value
	switch value.Type {
	case schema.TypeToken:
		docs, usage, _, _ := tokenDocs(sess.Install.Vocab, value.Name)
		return strings.TrimSpace(docs + "\n" + usage), nil
	case schema.TypeScopePath:
		return sess.Install.Vocab.Links[item.Label].Doc, nil
	case schema.TypeLoc:
		if said, ok := sess.Loc(value.Name, ""); ok {
			return said.Value, nil
		}
	case schema.TypeRef:
		file, err := open(sess, path)
		if err != nil {
			return "", err
		}
		card := &Inspect{}
		file.defined(card, value)
		return strings.TrimSpace(card.Label + "\n" + card.Docs), nil
	}
	return "", nil
}
