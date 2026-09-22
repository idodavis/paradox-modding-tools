// complete.go is what may be written at the cursor: at a key position, what the enclosing field holds (a body's declared tokens and the macros of its class, a structure's members, a database's names); at a value position, what the field accepts. Items carry the reader's value of what they would write; their documentation is fetched when one is shown.
package lsp

import (
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/lang/index"
	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/schema"
	"paradox-modding-tools/services/internal/lang/session"
	"paradox-modding-tools/services/internal/source"
)

// maxComplete is the payload's cap: past it the list is cut and marked incomplete, so the editor asks again as the word grows.
const maxComplete = 80

// Item is one completion: the text it writes, what that text is, and its rank (0 a token in scope, 1 a member, 2 a definition or a token out of scope, 3 a loc key).
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
func Complete(s *session.Session, path string, line, col int) (*Completion, error) {
	s.RLock()
	defer s.RUnlock()
	f, err := open(s, path)
	if err != nil {
		return nil, err
	}
	if f.place.Format != source.Script || len(f.place.Rel) == 0 {
		return &Completion{}, nil
	}
	offset := f.offset(line, col)
	_, start, end := f.parsed.TokenAt(offset)
	if end < offset || start > offset {
		start, end = offset, offset
	}
	// a cite is one word: the prefix and the colon before the typed part belong to it
	for start > 0 && (isWord(f.text[start-1]) || f.text[start-1] == ':') {
		start--
	}
	c := &completer{f: f, prefix: strings.ToLower(f.text[start:offset]), at: f.span(jomini.Range{Start: start, End: end}), seen: map[string]bool{}}

	found, _, _ := s.Located(path)
	d := index.Probe(found, f.text, s.Reader(), start)
	before := strings.TrimRight(f.text[:start], " \t")
	if len(before) > 0 && strings.ContainsRune("=<>!?", rune(before[len(before)-1])) {
		c.values(d, strings.TrimRight(before, "=<>!?"))
	} else {
		c.keys(d)
	}

	slices.SortStableFunc(c.items, func(a, b Item) int { return a.Rank - b.Rank })
	out := &Completion{Items: c.items}
	if len(out.Items) > maxComplete {
		out.Items, out.Incomplete = out.Items[:maxComplete], true
	}
	return out, nil
}

// completer gathers items that start with the typed prefix.
type completer struct {
	f      *file
	prefix string
	at     Range
	items  []Item
	seen   map[string]bool
}

// add keeps an item whose label starts with what was typed, once.
func (c *completer) add(label string, v schema.Value, rank int) {
	if !strings.HasPrefix(strings.ToLower(label), c.prefix) || c.seen[label] {
		return
	}
	c.seen[label], c.items = true, append(c.items, Item{Label: label, Value: v, Rank: rank, Range: c.at})
}

// enclosing is the statement whose block the cursor is in, the field that block is an instance of, and the scope it runs in; nil at the top of the file.
func (c *completer) enclosing(d *index.Descent) (*index.Level, *schema.Field, string) {
	s := c.f.s
	for i := len(d.Chain) - 1; i >= 0; i-- {
		level := d.Chain[i]
		a, ok := level.Statement.(*jomini.Assignment)
		if !ok || jomini.BlockOf(a.Value) == nil {
			continue
		}
		scope := level.Scope
		switch level.Key.Type {
		case schema.TypeToken:
			if _, _, _, out := tokenDocs(s.Install.Vocab, level.Key.Name); out != "" {
				scope = out
			}
		case schema.TypeScopePath:
			scope = s.Install.Vocab.Links[strings.ToLower(a.Key.Text)].Out
		case schema.TypeRef:
			if k := s.Schema.Kinds[level.Key.Kind]; k != nil && k.ScopeType != "" {
				scope = k.ScopeType
			}
		}
		if level.Below != nil && level.Below.BodyScope != "" {
			scope = level.Below.BodyScope
		}
		return &level, level.Below, scope
	}
	return nil, nil, ""
}

// keys offers what may be written as a key here.
func (c *completer) keys(d *index.Descent) {
	s := c.f.s
	level, field, scope := c.enclosing(d)
	if level == nil {
		for _, k := range c.f.place.Kinds {
			c.cited(k.Name, "", 2)
		}
		return
	}

	// a block nothing classifies offers what the body around it offers
	body := field
	for i := len(d.Chain) - 1; body == nil && i >= 0; i-- {
		if d.Chain[i].Below != nil && d.Chain[i].Below.Class == schema.ClassBody {
			body = d.Chain[i].Below
		}
	}
	if field != nil && field.Class != schema.ClassBody {
		for name := range field.Members {
			if !strings.HasPrefix(name, "<") {
				c.add(name, schema.Value{Type: schema.TypeMember, Name: name}, 1)
			}
		}
		if field.Defines != "" {
			c.cited(field.Defines, "", 2)
		}
		if field.Class == schema.ClassDatabase && level.Kind != nil {
			c.cited(level.Kind.Name, "", 2)
		}
		return
	}
	if body == nil {
		return
	}
	if body.Body&schema.BodyEffect != 0 {
		c.tokens(s.Install.Vocab.Effects, scope)
	}
	if body.Body&schema.BodyTrigger != 0 {
		c.tokens(s.Install.Vocab.Triggers, scope)
	}
	for name, link := range s.Install.Vocab.Links {
		if !link.Prefix && (len(link.In) == 0 || scope == "" || slices.Contains(link.In, scope)) {
			c.add(name, schema.Value{Type: schema.TypeScopePath, Declared: true}, 2)
		}
	}
	for _, k := range s.Schema.Kinds {
		if k.Macro&body.Body != 0 {
			c.cited(k.Name, "", 2)
		}
	}
}

// isWord reports a byte a name is spelled with.
func isWord(c byte) bool {
	return c == '_' || c == '.' || c == '@' || c == '$' || ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z') || ('0' <= c && c <= '9')
}

// tokens offers a vocabulary's tokens, those usable in the scope first.
func (c *completer) tokens(tokens map[string]schema.EngineToken, scope string) {
	for name, t := range tokens {
		rank := 2
		if scope == "" || schema.UsableIn(t.In, scope) {
			rank = 0
		}
		c.add(name, schema.Value{Type: schema.TypeToken, Name: name, Declared: true}, rank)
	}
}

// cited offers a kind's definitions, each spelled with the prefix (trait:brave) where one was typed.
func (c *completer) cited(kind, prefix string, rank int) {
	for _, x := range c.f.s.All() {
		for _, d := range x.Defs {
			if d.File >= 0 && d.Kind == kind {
				c.add(prefix+d.Name, schema.Value{Type: schema.TypeRef, Kind: kind, Name: d.Name, Parent: d.Parent}, rank)
			}
		}
	}
}

// values offers what the field of the statement being written accepts. The statement may not have parsed yet (hidden = ), so its key is read from the text before the operator and its field from the block around the cursor.
func (c *completer) values(d *index.Descent, before string) {
	s := c.f.s
	if len(d.Chain) == 0 {
		return
	}
	level := d.Chain[len(d.Chain)-1]
	field := level.Below
	if _, bare := level.Statement.(*jomini.ValueStmt); bare {
		field = level.Field
	}
	key, _, _ := c.f.parsed.TokenAt(len(strings.TrimRight(before, " \t")) - 1)
	if a, ok := level.Statement.(*jomini.Assignment); !ok || a.Key.Text != key {
		// the block's statement is a name token whose member this key is: the names the member reads
		if level.Key.Type == schema.TypeToken {
			for _, row := range schema.NameTokensOf(level.Key.Name) {
				if !row.Defines && row.Member == key {
					c.cited(row.Kind, "", 2)
				}
			}
		}
		r := s.Reader()
		v := r.Key(level.Below, key)
		level, field = index.Level{Kind: level.Kind, Field: level.Below, Key: v, Scope: level.Scope}, r.Below(level.Kind, level.Below, key, v)
	}

	// a cite: the kind the prefix belongs to, spelled whole
	if prefix, _, ok := strings.Cut(c.prefix, ":"); ok {
		for _, k := range s.Schema.ByPrefix(prefix) {
			c.cited(k.Name, prefix+":", 2)
		}
		return
	}
	if level.Key.Type == schema.TypeToken {
		for _, row := range schema.NameTokensOf(level.Key.Name) {
			if !row.Defines && row.Member == "" {
				c.cited(row.Kind, "", 2)
			}
		}
	}
	if field == nil {
		return
	}
	for _, a := range field.Accepts {
		switch a.Type {
		case schema.TypeRef:
			c.cited(a.Kind, "", 2)
		case schema.TypeEnum:
			for _, w := range a.Enum {
				c.add(w, schema.Value{Type: schema.TypeEnum, Name: w}, 1)
			}
		case schema.TypeBool:
			c.add("yes", schema.Value{Type: schema.TypeBool}, 1)
			c.add("no", schema.Value{Type: schema.TypeBool}, 1)
		case schema.TypeLoc:
			for _, key := range s.LocKeys(c.prefix, maxComplete+1) {
				c.add(key, schema.Value{Type: schema.TypeLoc, Name: key}, 3)
			}
		}
	}
}

// Resolve is an item's documentation, fetched when the editor shows it: a token's prose, a definition's label or comment, a loc key's text.
func Resolve(s *session.Session, path string, item Item) (string, error) {
	s.RLock()
	defer s.RUnlock()
	v := item.Value
	switch v.Type {
	case schema.TypeToken:
		docs, usage, _, _ := tokenDocs(s.Install.Vocab, v.Name)
		return strings.TrimSpace(docs + "\n" + usage), nil
	case schema.TypeScopePath:
		return s.Install.Vocab.Links[item.Label].Doc, nil
	case schema.TypeLoc:
		if said, ok := s.Loc(v.Name, ""); ok {
			return said.Value, nil
		}
	case schema.TypeRef:
		f, err := open(s, path)
		if err != nil {
			return "", err
		}
		card := &Inspect{}
		f.defined(card, v)
		return strings.TrimSpace(card.Label + "\n" + card.Docs), nil
	}
	return "", nil
}
