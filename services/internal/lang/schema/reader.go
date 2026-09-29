// reader.go is the one place that decides what a written word is, for the harvest and the editor
// alike. It holds no rule of its own about a game: it looks the word up, in a fixed order, in
// what its spelling tells, what the vocabulary declares, what the schema learned for its field,
// and what exists. A Value says which it rested on, because only a declared fact may accuse.

package schema

import (
	"cmp"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/lang/jomini"
)

// Reader decides what a written word is. Its fields are never nil; with no dumps Vocab is empty.
type Reader struct {
	Schema *Schema
	Vocab  *Vocab
	Found  Defined
}

// Defined answers whether something exists: the session from the workspace and the install, the
// learner from its own first pass over the files.
type Defined interface {
	Def(kind, name string) bool // a definition of that kind with that name exists
	Loc(key string) bool        // a localization key exists
	File(name string) bool      // a file exists by path from a root, by bare name, or by the name under another extension
}

// Value reads a word right of an operator, or bare in a list, at field (nil where nothing was
// learned). The first step that holds decides.
func (reader Reader) Value(field *Field, text string, quoted bool) Value {
	// 1. nothing written
	if text == "" && !quoted {
		return Value{}
	}

	// 2. decided by spelling alone: @name, $NAME$, [ … ], file:/path
	if name, ok := jomini.Constant(text); ok {
		return Value{Type: TypeConstant, Name: name}
	}
	if spans := jomini.Interpolations(text); len(spans) > 0 {
		return Value{Type: TypeParam, Name: spans[0].Name}
	}
	if strings.HasPrefix(text, "[") && strings.HasSuffix(text, "]") {
		return Value{Type: TypeCall}
	}
	if name, ok := strings.CutPrefix(text, "file:/"); ok {
		return Value{Type: TypeFile, Name: name}
	}

	// 3. a cite with no chain after it
	if cited, ok := reader.cite(text); ok {
		return cited
	}

	// 4. the field's accepts, in their order, each confirmed; a number that names a definition the
	// field also takes is that link (a barony's province = 2333)
	if field != nil {
		for _, accept := range field.Accepts {
			value, ok := Value{Type: accept.Type, Declared: accept.Evidence == Declared}, false
			switch accept.Type {
			case TypeNumber:
				ok = !quoted && jomini.Literal(text) == jomini.LitNumber
				names := func(ref Accept) bool { return ref.Type == TypeRef && reader.Found.Def(ref.Kind, text) }
				if at := slices.IndexFunc(field.Accepts, names); ok && at >= 0 {
					named := field.Accepts[at]
					return Value{Type: TypeRef, Kind: named.Kind, Name: text, Declared: named.Evidence == Declared}
				}
			case TypeBool:
				ok = !quoted && (text == "yes" || text == "no")
			case TypeDate:
				ok = jomini.Literal(text) == jomini.LitDate
			case TypeString:
				ok = quoted
			case TypeRef:
				value.Kind, value.Name, ok = accept.Kind, text, reader.Found.Def(accept.Kind, text)
			case TypeEnum:
				ok = slices.Contains(accept.Enum, text)
			case TypeLoc:
				value.Name, ok = text, reader.Found.Loc(text)
			case TypeFile:
				value.Name, ok = text, reader.Found.File(text)
			}
			if ok {
				return value
			}
		}
	}

	// 5. a walk between scopes; the editor walks the steps
	if !quoted {
		if walked, ok := reader.scopePath(text); ok {
			return walked
		}
	}

	// 6. by spelling, where no field said otherwise
	switch kind := jomini.Literal(text); {
	case kind == jomini.LitDate:
		return Value{Type: TypeDate}
	case kind == jomini.LitNumber && !quoted:
		return Value{Type: TypeNumber}
	case !quoted && (text == "yes" || text == "no"):
		return Value{Type: TypeBool}
	}

	// 7. a quoted word is text; a name-shaped word nothing claimed is unresolved
	if !quoted && jomini.IsDefName(text) {
		return Value{Type: TypeUnresolved, Name: text}
	}
	return Value{Type: TypeString}
}

// Key reads a word on the left, inside the block of field parent (nil where nothing was learned).
// Below gives the field under it.
func (reader Reader) Key(parent *Field, text string) Value {
	// 1. a fixed member of the parent
	if parent != nil && parent.Members[text] != nil {
		return Value{Type: TypeMember, Name: text}
	}

	// 2. by spelling: a number, date, cite, or path (CK3 widgets: gui/new_window.gui = my_window)
	switch jomini.Literal(text) {
	case jomini.LitNumber:
		return Value{Type: TypeNumber}
	case jomini.LitDate:
		return Value{Type: TypeDate}
	}
	if cited, ok := reader.cite(text); ok {
		return cited
	}
	if strings.Contains(text, "/") {
		return Value{Type: TypeFile, Name: text}
	}

	// 3. in a body: a token of one of its classes, in any case (AND is the trigger and); a walk
	// between scopes; a macro of one of its classes, as written or with an iterator prefix removed
	// (every_adult_child is the scripted list adult_child)
	if parent != nil && parent.Class == ClassBody {
		token := strings.ToLower(text)
		_, effect := reader.Vocab.Effects[token]
		_, trigger := reader.Vocab.Triggers[token]
		_, modifier := reader.Vocab.Modifiers[token]
		if (effect && parent.Body&BodyEffect != 0) || (trigger && parent.Body&BodyTrigger != 0) ||
			(modifier && parent.Body&BodyModifier != 0) {
			return Value{Type: TypeToken, Name: token, Declared: true}
		}
		if walked, ok := reader.scopePath(text); ok {
			return walked
		}

		reader.Schema.lookups()
		for _, name := range append([]string{text}, stems(text)...) {
			for _, kind := range reader.Schema.macros {
				if kind.Macro&parent.Body != 0 && reader.Found.Def(kind.Name, name) {
					return Value{Type: TypeRef, Kind: kind.Name, Name: name}
				}
			}
		}
	}

	// 4. a name of the kind the parent's keys define or cite
	if parent != nil {
		kind := cmp.Or(parent.Defines, parent.KeyedBy)
		if kind != "" && reader.Found.Def(kind, text) {
			return Value{Type: TypeRef, Kind: kind, Name: text}
		}
	}

	// 5. nothing claims it
	return Value{Type: TypeUnresolved, Name: text}
}

// Below is the field under a key that Key read as value, inside a definition of kind: the member;
// the position a number, date or cite selects; for a token, what the game's tokens take; under a
// nested kind's names, that kind's shape; else the position of names. In a body a scope change
// (a link, a cite, a $PARAM$) keeps the body as the field. A key that can hold itself (a GUI vbox
// inside an hbox inside a vbox) is learned once, at the top of its kind, and looked for there
// last. nil where nothing was learned.
func (reader Reader) Below(kind *Kind, parent *Field, key string, value Value) *Field {
	if value.Type == TypeToken {
		return reader.Schema.Arguments[value.Name]
	}
	if parent == nil {
		return nil
	}
	_, cited := jomini.Cite(key)
	position := ""
	switch value.Type {
	case TypeNumber:
		position = "<number>"
	case TypeDate:
		position = "<date>"
	}
	switch {
	case value.Type == TypeMember:
		return parent.Members[key]
	case parent.Class == ClassBody && (value.Type == TypeScopePath || cited || len(jomini.Interpolations(key)) > 0):
		return parent
	case cited:
		position = "<cite>"
	case position == "" && parent.Defines != "" && reader.Schema.Kinds[parent.Defines] != nil:
		return reader.Schema.Kinds[parent.Defines].Shape
	case position == "":
		position = "<name>"
	}
	if field := parent.Members[position]; field != nil || kind == nil || kind.Shape == nil {
		return field
	}
	return kind.Shape.Members[key]
}

// cite reads prefix:name with no chain after it, when the prefix is declared or belongs to a kind.
// Of several kinds sharing the prefix, the one with the definition is named, else the first.
// define:NGame|START_DATE names START_DATE inside NGame. A declared prefix of no kind gives a
// reference with no kind, which nothing judges: flag:some_flag is a plain value.
func (reader Reader) cite(text string) (Value, bool) {
	cited, ok := jomini.Cite(text)
	kinds := reader.Schema.ByPrefix(cited.Prefix)
	_, declared := reader.Vocab.Prefixes[cited.Prefix]
	if !ok || cited.Chain != "" || (len(kinds) == 0 && !declared) {
		return Value{}, false
	}

	value := Value{Type: TypeRef, Name: cited.Name, Declared: declared}
	if cited.Member != "" {
		value.Name, value.Parent = cited.Member, cited.Name
	}
	for i, kind := range kinds {
		has := reader.Found.Def(kind.Name, value.Name)
		if has || i == 0 {
			value.Kind = kind.Name
		}
		if has {
			break
		}
	}
	return value, true
}

// scopePath reads a walk between scopes: a declared link first, or a cite with a chain after it,
// declared where the vocabulary declares its prefix.
func (reader Reader) scopePath(text string) (Value, bool) {
	first, _, _ := strings.Cut(text, ".")
	if _, isLink := reader.Vocab.Links[first]; isLink {
		return Value{Type: TypeScopePath, Declared: true}, true
	}
	if cite, ok := jomini.Cite(text); ok && cite.Chain != "" {
		if _, declared := reader.Vocab.Prefixes[cite.Prefix]; declared || len(reader.Schema.ByPrefix(cite.Prefix)) > 0 {
			return Value{Type: TypeScopePath, Declared: declared}, true
		}
	}
	return Value{}, false
}

// stems is a word with each iterator prefix it carries removed: every_x gives x.
func stems(word string) []string {
	var out []string
	for _, prefix := range iteratorPrefixes {
		if stem, ok := strings.CutPrefix(word, prefix); ok && stem != "" {
			out = append(out, stem)
		}
	}
	return out
}
