// reader.go is the one place that decides what a written word is, for the harvest and the editor
// alike. It holds no rule of its own about a game: it looks the word up, in a fixed order, in
// what its spelling tells, what the dumps declare, what the schema says of its key,
// and what exists. A Value says which it rested on, because only a declared fact may accuse.

package schema

import (
	"cmp"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/source"
)

// Reader decides what a written word is. Its fields are never nil.
type Reader struct {
	Schema *Schema
	Found  Defined
}

// Defined answers whether something exists: the session from the workspace and the install, the
// scan from its index.
type Defined interface {
	Def(database, name string) bool // a definition of that database with that name exists
	Loc(key string) bool            // a localization key exists
	File(name string) bool          // a file exists by path from a root, by bare name, or by the name under another extension
	DefinedBy(name string) string   // the one loaded database defining the name; "" for none or several
}

// Value reads a word right of an operator, or bare in a list, at key (nil where nothing is known).
// The first step that holds decides.
func (reader Reader) Value(key *Key, text string, quoted bool) Word {
	// 1. nothing written
	if text == "" && !quoted {
		return Word{}
	}

	// 2. decided by spelling alone: @name, $NAME$, [ … ], file:/path
	if name, ok := jomini.Constant(text); ok {
		return Word{Type: WordConstant, Name: name}
	}
	if spans := jomini.Interpolations(text); len(spans) > 0 {
		return Word{Type: WordParameter, Name: spans[0].Name}
	}
	if strings.HasPrefix(text, "[") && strings.HasSuffix(text, "]") {
		return Word{Type: WordDataCall}
	}
	if name, ok := strings.CutPrefix(text, "file:/"); ok {
		return Word{Type: WordFile, Name: name}
	}

	// 3. a prefixed word with no chain after it; a command's call form, name(<a>|<b>)
	if word, ok := reader.prefixed(text); ok {
		return word
	}
	if name, _, ok := strings.Cut(text, "("); ok && strings.HasSuffix(text, ")") {
		dumps := reader.Schema.Dumps
		if dumps.Effects[name].Call != "" || dumps.Triggers[name].Call != "" {
			return Word{Type: WordCommand, Name: name, Declared: true}
		}
	}

	// 4. the key's takes, in their order, each confirmed; a number that names a definition the
	// key also takes is that reference (a barony's province = 2333)
	if key != nil {
		for _, take := range key.Takes {
			word, ok := Word{Type: take.Type, Declared: take.Source.Declared() && !take.Guessed}, false
			switch take.Type {
			case WordNumber:
				ok = !quoted && jomini.Literal(text) == jomini.LitNumber
				names := func(ref Take) bool { return ref.Type == WordReference && reader.Found.Def(ref.Database, text) }
				if at := slices.IndexFunc(key.Takes, names); ok && at >= 0 {
					named := key.Takes[at]
					return Word{Type: WordReference, Database: named.Database, Name: text, Declared: named.Source.Declared() && !named.Guessed}
				}
			case WordBool:
				ok = !quoted && (text == "yes" || text == "no")
			case WordDate:
				ok = jomini.Literal(text) == jomini.LitDate
			case WordString:
				ok = quoted
			case WordReference:
				word.Database, word.Name, ok = take.Database, text, reader.Found.Def(take.Database, text)
			case WordEnum:
				ok = slices.Contains(take.Enum, text)
			case WordLoc:
				word.Name, ok = text, reader.Found.Loc(text)
			case WordFile:
				word.Name, ok = text, reader.Found.File(text)
			}
			if ok {
				return word
			}
		}
	}

	// 5. a walk between scopes; the editor walks the steps
	if !quoted {
		if walked, ok := reader.scopePath(text); ok {
			return walked
		}
	}

	// 6. a number at a key the dumps declare an event target of one output scope is a guess at the one
	// database defining it, where that database is of the scope (father = 186004, a character)
	if key != nil && !quoted && jomini.Literal(text) == jomini.LitNumber {
		if target := reader.Schema.Dumps.EventTargets[strings.ToLower(key.Name)]; len(target.Out) == 1 {
			database := reader.Schema.Databases[reader.Found.DefinedBy(text)]
			if database != nil && database.Scope == target.Out[0] {
				return Word{Type: WordReference, Database: database.Name, Name: text, Guessed: true}
			}
		}
	}

	// 7. by spelling, where no key said otherwise; a dotted number one database defines goes on to 8
	// (Vic3's event 1848.4)
	switch kind := jomini.Literal(text); {
	case kind == jomini.LitDate:
		return Word{Type: WordDate}
	case kind == jomini.LitNumber && !quoted && (!jomini.IsEventID(text) || reader.Found.DefinedBy(text) == ""):
		return Word{Type: WordNumber}
	case !quoted && (text == "yes" || text == "no"):
		return Word{Type: WordBool}
	}

	// 8. a name-shaped word, quoted or not, is a guessed reference when one database defines it, else a
	// guessed loc reference when the key exists; a word in a known file format that a file answers is a
	// guessed file; else an unquoted name-shaped word is unresolved, and any other is text
	name := jomini.IsDefName(text)
	if name {
		if database := reader.Found.DefinedBy(text); database != "" {
			return Word{Type: WordReference, Database: database, Name: text, Guessed: true}
		}
		if reader.Found.Loc(text) {
			return Word{Type: WordLoc, Name: text, Guessed: true}
		}
	}
	if source.Classify("", text) != source.Unknown && reader.Found.File(text) {
		return Word{Type: WordFile, Name: text, Guessed: true}
	}
	if name && !quoted {
		return Word{Type: WordUnknown, Name: text}
	}
	return Word{Type: WordString}
}

// Key reads a word on the left, inside the block of key parent (nil where nothing is known).
// Below gives the key under it.
func (reader Reader) Key(parent *Key, text string) Word {
	// 1. a fixed key of the parent
	if parent != nil && parent.Keys[text] != nil {
		return Word{Type: WordKey, Name: text}
	}

	// 2. by spelling: a number, date, prefixed word, or path (CK3 widgets:
	// gui/new_window.gui = my_window)
	switch jomini.Literal(text) {
	case jomini.LitNumber:
		return Word{Type: WordNumber}
	case jomini.LitDate:
		return Word{Type: WordDate}
	}
	if word, ok := reader.prefixed(text); ok {
		return word
	}
	if strings.Contains(text, "/") {
		return Word{Type: WordFile, Name: text}
	}

	// 3. in a body: a command of one of its classes, in any case (AND is the trigger and); a walk
	// between scopes; a scripted effect or trigger of one of its classes, as written or with an
	// iterator prefix removed (every_adult_child is the scripted list adult_child)
	if parent != nil && parent.Block != BlockNone {
		command, dumps := strings.ToLower(text), reader.Schema.Dumps
		_, effect := dumps.Effects[command]
		_, trigger := dumps.Triggers[command]
		_, modifier := dumps.Modifiers[command]
		if (effect && parent.Block == BlockEffects) || (trigger && parent.Block == BlockTriggers) ||
			(modifier && parent.Block == BlockModifiers) {
			return Word{Type: WordCommand, Name: command, Declared: true}
		}
		if walked, ok := reader.scopePath(text); ok {
			return walked
		}

		for _, name := range append([]string{text}, stems(text)...) {
			for _, database := range reader.Schema.scripted {
				if slices.Contains(database.CalledIn, parent.Block) && reader.Found.Def(database.Name, name) {
					return Word{Type: WordReference, Database: database.Name, Name: name}
				}
			}
		}
	}

	// 4. a name of the database the parent's keys define or prefix
	if parent != nil {
		database := cmp.Or(parent.Defines, parent.Names)
		if database != "" && reader.Found.Def(database, text) {
			return Word{Type: WordReference, Database: database, Name: text}
		}
	}

	// 5. a GUI type's instance: its names are written only in .gui files
	if reader.Found.DefinedBy(text) == "gui.type" {
		return Word{Type: WordReference, Database: "gui.type", Name: text, Guessed: true}
	}

	// 6. nothing claims it
	return Word{Type: WordUnknown, Name: text}
}

// Below is the key under a key that Key read as value, inside a definition of database: the key;
// the position a number, date or prefixed word selects; for a command, what the game's commands
// take; under a nested database's names, that database's shape; else the position of names. In a
// body a scope change (an event target, a prefixed word, a $PARAM$) keeps the body as the key. A
// key that can hold itself (a GUI vbox inside an hbox inside a vbox) is learned once, at the top of
// its database, and looked for there last. nil where nothing was learned.
func (reader Reader) Below(database *Database, parent *Key, key string, word Word) *Key {
	if word.Type == WordCommand {
		return reader.Schema.Arguments[word.Name]
	}
	if parent == nil {
		return nil
	}
	_, isPrefixed := jomini.SplitPrefixed(key)
	position := ""
	switch word.Type {
	case WordNumber:
		position = "<number>"
	case WordDate:
		position = "<date>"
	}
	switch {
	case word.Type == WordKey:
		return parent.Keys[key]
	case parent.Block != BlockNone && (word.Type == WordScopePath || isPrefixed || len(jomini.Interpolations(key)) > 0):
		return parent
	case isPrefixed:
		position = "<prefixed>"
	case position == "" && parent.Defines != "" && reader.Schema.Databases[parent.Defines] != nil:
		return reader.Schema.Databases[parent.Defines].Entry
	case position == "":
		position = "<name>"
	}
	if key := parent.Keys[position]; key != nil || database == nil || database.Entry == nil {
		return key
	}
	return database.Entry.Keys[key]
}

// prefixed reads prefix:name with no chain after it, when the prefix is declared or belongs to a
// database. Of several databases sharing the prefix, the one with the definition is named, else the
// first. define:NGame|START_DATE names START_DATE inside NGame. A declared prefix of no database
// gives a reference with no database, which nothing judges: flag:some_flag is a plain value.
func (reader Reader) prefixed(text string) (Word, bool) {
	prefixed, ok := jomini.SplitPrefixed(text)
	databases := reader.Schema.ByPrefix(prefixed.Prefix)
	_, declared := reader.Schema.Dumps.DataTargets[prefixed.Prefix]
	if !ok || prefixed.Chain != "" || (len(databases) == 0 && !declared) {
		return Word{}, false
	}

	word := Word{Type: WordReference, Name: prefixed.Name, Declared: declared}
	if prefixed.Key != "" {
		word.Name, word.Parent = prefixed.Key, prefixed.Name
	}
	for i, database := range databases {
		has := reader.Found.Def(database.Name, word.Name)
		if has || i == 0 {
			word.Database = database.Name
		}
		if has {
			break
		}
	}
	return word, true
}

// scopePath reads a walk between scopes: a declared event target first, or a prefixed word with a
// chain after it, declared where the dumps declare its prefix.
func (reader Reader) scopePath(text string) (Word, bool) {
	first, _, _ := strings.Cut(text, ".")
	if _, isLink := reader.Schema.Dumps.EventTargets[first]; isLink {
		return Word{Type: WordScopePath, Declared: true}, true
	}
	if prefixed, ok := jomini.SplitPrefixed(text); ok && prefixed.Chain != "" {
		if _, declared := reader.Schema.Dumps.DataTargets[prefixed.Prefix]; declared || len(reader.Schema.ByPrefix(prefixed.Prefix)) > 0 {
			return Word{Type: WordScopePath, Declared: declared}, true
		}
	}
	return Word{}, false
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
