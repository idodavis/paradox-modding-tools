// reader.go is the one place in the engine that decides what a written word is. Everything else asks it: the harvest for every value and every key of every file, the editor for the word under the cursor. It holds no rule of its own about a game. It looks the word up, in a fixed order, in what the parser can tell from spelling, what the vocabulary declares, what the schema learned for the field the word sits at, and what exists. A Value says which of those it rested on, because only a declared fact lets a diagnostic accuse.
package schema

import (
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/lang/jomini"
)

// Reader decides what a written word is. Schema, Vocab and Found are never nil; a game with no dumps has an empty Vocab.
type Reader struct {
	Schema *Schema
	Vocab  *Vocab
	Found  Defined
}

// Defined answers whether something exists. The session answers from the workspace and the install together; the learner answers from its own first pass over the files.
type Defined interface {
	Def(kind, name string) bool // a definition of that kind with that name exists
	Loc(key string) bool        // a localization key exists
	File(name string) bool      // a file exists: a path from a root, a bare file name, or the same name under another extension
}

// Value reads a word on the right of an operator, or a bare word in a list, at the field f, which is nil where nothing was learned. The first step that holds decides.
func (r Reader) Value(f *Field, text string, quoted bool) Value {
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
	if v, ok := r.cite(text); ok {
		return v
	}

	// 4. the field's accepts, in their order, each confirmed
	if f != nil {
		for _, a := range f.Accepts {
			v, ok := Value{Type: a.Type, Declared: a.Declared}, false
			switch a.Type {
			case TypeNumber:
				ok = !quoted && jomini.Literal(text) == jomini.LitNumber
			case TypeBool:
				ok = !quoted && (text == "yes" || text == "no")
			case TypeDate:
				ok = jomini.Literal(text) == jomini.LitDate
			case TypeString:
				ok = quoted
			case TypeRef:
				v.Kind, v.Name, ok = a.Kind, text, r.Found.Def(a.Kind, text)
			case TypeEnum:
				ok = slices.Contains(a.Enum, text)
			case TypeLoc:
				v.Name, ok = text, r.Found.Loc(text)
			case TypeFile:
				v.Name, ok = text, r.Found.File(text)
			}
			if ok {
				return v
			}
		}
	}

	// 5. a walk between scopes: its first step is a declared link, or a cite with a chain after it. The steps are walked by the editor, not here
	if first, _, _ := strings.Cut(text, "."); !quoted {
		if _, isLink := r.Vocab.Links[first]; isLink {
			return Value{Type: TypeScopePath, Declared: true}
		}
		if c, ok := jomini.Cite(text); ok && c.Chain != "" && (r.Vocab.Links[c.Prefix].Prefix || len(r.Schema.ByPrefix(c.Prefix)) > 0) {
			return Value{Type: TypeScopePath, Declared: r.Vocab.Links[c.Prefix].Prefix}
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

// Key reads a word on the left, inside the block of the field parent, which is nil where nothing was learned. The field below the key is then: the member; the position its spelling selects (<number>, <date>, <cite>); for a token, Schema.Arguments[Value.Name]; for a name under Defines, the nested kind's Shape.
func (r Reader) Key(parent *Field, text string) Value {
	// 1. a fixed member of the parent
	if parent != nil && parent.Members[text] != nil {
		return Value{Type: TypeMember, Name: text}
	}

	// 2. by spelling: a number, a date, a cite, a path (CK3 scripted widgets: gui/new_window.gui = my_window)
	switch jomini.Literal(text) {
	case jomini.LitNumber:
		return Value{Type: TypeNumber}
	case jomini.LitDate:
		return Value{Type: TypeDate}
	}
	if v, ok := r.cite(text); ok {
		return v
	}
	if strings.Contains(text, "/") {
		return Value{Type: TypeFile, Name: text}
	}

	// 3. in a body: a token of one of its classes, written in any case (AND is the trigger and); a walk between scopes; a macro of one of its classes, as written and then with an iterator prefix removed (every_adult_child is the scripted list adult_child)
	if parent != nil && parent.Class == ClassBody {
		token := strings.ToLower(text)
		_, effect := r.Vocab.Effects[token]
		_, trigger := r.Vocab.Triggers[token]
		_, modifier := r.Vocab.Modifiers[token]
		if (effect && parent.Body&BodyEffect != 0) || (trigger && parent.Body&BodyTrigger != 0) || (modifier && parent.Body&BodyModifier != 0) {
			return Value{Type: TypeToken, Name: token, Declared: true}
		}

		first, _, _ := strings.Cut(text, ".")
		if _, isLink := r.Vocab.Links[first]; isLink {
			return Value{Type: TypeScopePath, Declared: true}
		}

		r.Schema.lookups()
		for _, name := range append([]string{text}, stems(text)...) {
			for _, k := range r.Schema.macros {
				if k.Macro&parent.Body != 0 && r.Found.Def(k.Name, name) {
					return Value{Type: TypeRef, Kind: k.Name, Name: name}
				}
			}
		}
	}

	// 4. a name of the kind the parent's keys define or cite
	if parent != nil {
		kind := parent.Defines
		if kind == "" {
			kind = parent.KeyedBy
		}
		if kind != "" && r.Found.Def(kind, text) {
			return Value{Type: TypeRef, Kind: kind, Name: text}
		}
	}

	// 5. nothing claims it
	return Value{Type: TypeUnresolved, Name: text}
}

// Below is the field under a key that Key read as v, inside a definition of kind: the member; the position a number, a date or a cite selects; for a token, what the game's tokens take wherever they stand; under a field whose keys are a nested kind's names, that kind's shape; else the position of names. In a body a scope change (a link, a cite, a $PARAM$) changes the scope and not the field, so the field below it is the body itself. A key that can hold itself is learned once, at the top of its kind (a GUI vbox inside an hbox inside a vbox), and is looked for there last. nil where nothing was learned.
func (r Reader) Below(kind *Kind, parent *Field, key string, v Value) *Field {
	if v.Type == TypeToken {
		return r.Schema.Arguments[v.Name]
	}
	if parent == nil {
		return nil
	}
	_, cited := jomini.Cite(key)
	position := map[ValueType]string{TypeNumber: "<number>", TypeDate: "<date>"}[v.Type]
	switch {
	case v.Type == TypeMember:
		return parent.Members[key]
	case parent.Class == ClassBody && (v.Type == TypeScopePath || cited || len(jomini.Interpolations(key)) > 0):
		return parent
	case cited:
		position = "<cite>"
	case position == "" && parent.Defines != "" && r.Schema.Kinds[parent.Defines] != nil:
		return r.Schema.Kinds[parent.Defines].Shape
	case position == "":
		position = "<name>"
	}
	if f := parent.Members[position]; f != nil || kind == nil || kind.Shape == nil {
		return f
	}
	return kind.Shape.Members[key]
}

// cite reads a word written prefix:name with no chain after it, when the prefix is one the vocabulary declares or one that belongs to a kind. Where several kinds share the prefix, the one that has the definition is named, else the first. define:NGame|START_DATE names START_DATE, inside NGame. A declared prefix that belongs to no kind gives a reference with no kind, which nothing judges: flag:some_flag is a plain value.
func (r Reader) cite(text string) (Value, bool) {
	c, ok := jomini.Cite(text)
	kinds, declared := r.Schema.ByPrefix(c.Prefix), r.Vocab.Links[c.Prefix].Prefix
	if !ok || c.Chain != "" || (len(kinds) == 0 && !declared) {
		return Value{}, false
	}

	v := Value{Type: TypeRef, Name: c.Name, Declared: declared}
	if c.Member != "" {
		v.Name, v.Parent = c.Member, c.Name
	}
	for i, k := range kinds {
		has := r.Found.Def(k.Name, v.Name)
		if has || i == 0 {
			v.Kind = k.Name
		}
		if has {
			break
		}
	}
	return v, true
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
