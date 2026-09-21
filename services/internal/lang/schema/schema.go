// Package schema is what the engine knows about a game's language: the vocabulary the game declares in its console dumps (vocab.go), the prose it ships beside its script (info.go), the hand table of name tokens (nametokens.go), the schema a scan learned from the install (this file, read and written as text by file.go), and the one reader that decides what a written word is (reader.go). Nothing here parses a game file or walks an install: the learner fills a Schema, and everything above looks things up in it.
//
// schema.go holds the learned types. A Schema is never edited by hand: a wrong one is a learner bug. A fact has one field; the lookups in the other direction (prefix to kinds, keyword to kind, folder to kinds) are built on first use and never stored or written.
package schema

import (
	"maps"
	"path"
	"slices"
	"strings"
	"sync"

	"paradox-modding-tools/services/internal/lang/loc"
	"paradox-modding-tools/services/internal/source"
)

// Schema is what one install at one version was seen to contain: every database and what is written inside it.
type Schema struct {
	Game      string            // ck3 | vic3 | eu5
	Version   string            // the install's version
	Kinds     map[string]*Kind  // every database, by name
	Arguments map[string]*Field // effect or trigger name to what is written after it, learned once from every use
	Drift     Drift             // what the scan saw and nothing explained

	once      sync.Once
	byPrefix  map[string][]*Kind
	byKeyword map[string]*Kind
	byFolder  map[string][]*Kind
	macros    []*Kind
}

// Kind is one database of the game: all the definitions of one type. brave, cynical and lustful are definitions of the kind traits.
type Kind struct {
	Name          string           // traits, gui.type, models.pdxmesh, religions.faiths, history/titles
	Format        source.Format    // the format of the files its definitions are read from
	Folders       []string         // where its definitions are, from a root: a folder (common/traits), or one file where a folder's files each hold a different kind (map_data/definition.csv). None for a nested or script-made kind
	Naming        Naming           // where a definition's name is written
	Parent        string           // nested kind: the kind whose definitions contain this kind's
	Subject       string           // subject kind: the kind its entries are about; it defines nothing
	Prefixes      []string         // the prefixes that cite a definition of this kind: character; title; scope
	ScopeType     string           // the scope type a definition is when script runs on it: characters to character
	LabelMember   string           // the member whose value is a definition's human name: characters to name
	LocTemplates  []loc.Convention // the loc keys a definition conventionally has, over its name, each with its coverage
	FileTemplates []FileTemplate   // the files the game finds by a definition's name, each with its coverage
	Macro         BodyClass        // the classes of body in which a definition is invoked by writing its name as a key
	Params        bool             // a definition's text holds $NAME$ parameters that the invoking site fills
	Shape         *Field           // what a definition holds: its members when it is a block, its accepts when it is one value
}

// Naming is where the name of a definition is written. Form is one of namingForms.
type Naming struct {
	Form    string
	Keyword string // member, declaration: the word that opens a definition. key: a word that also declares one inline in any script file
	Member  string // member: the member whose value is the name
	Column  int    // column: the column that holds the name
}

// namingForms is every way a definition is named: brave = { } is key, 186004 = { } number, birth.1001 = { } event_id, @x = 5 constant, pdxmesh = { name = "x" } member, type x = button { } declaration, history/cultures/afghan.txt file, a table's column, a list file's line, a loc key matching the kind's template, a bare word of a list, a name token's argument, and none where the files define nothing.
var namingForms = []string{"key", "number", "event_id", "constant", "member", "declaration", "file", "column", "line", "loc_key", "word", "token", "none"}

// Field is one position below a definition, learned from every place the install writes it under the same parent.
type Field struct {
	Name      string            // the key as written (trigger, on_accept, texture), or a position: <number> <date> <cite> <name> <word> <0> <1>
	Count     int               // how many times the install writes it
	Class     Class             // what is written at it
	Body      BodyClass         // ClassBody: which vocabularies its statements come from
	BodyScope string            // ClassBody: the scope type its statements run in; "" where the install's tokens disagree or state none
	Accepts   []Accept          // ClassValue, ClassList: every type of value seen or declared, at most one per type and kind; declared first, then most frequent first
	Ops       []string          // the operators written after this key other than =
	Defines   string            // the nested kind whose definitions are this block's name keys, or a list's bare words
	KeyedBy   string            // the kind this block's name keys cite, when the definitions are made elsewhere: goods = { wheat = 0.5 }
	Members   map[string]*Field // the fields below, by Field.Name
}

// Class is what is written at a field.
type Class uint8

const (
	ClassNone      Class = iota
	ClassValue           // one word: is_shown = yes
	ClassList            // a block of bare words: categories = { a b c }
	ClassStructure       // a block of fixed member names: portrait = { … }
	ClassDatabase        // a block whose every key is a name: faiths = { catholic = { … } }
	ClassBody            // a block of statements from the vocabulary: trigger = { … }
)

var classNames = [...]string{"", "value", "list", "structure", "database", "body"}

func (c Class) String() string { return classNames[c] }

// BodyClass says which vocabularies a body's statements come from, as bits.
type BodyClass uint8

const (
	BodyTrigger BodyClass = 1 << iota
	BodyEffect
	BodyModifier
	BodyMath // script-value arithmetic: add, multiply, min, max
)

var bodyNames = [...]string{"trigger", "effect", "modifier", "math"}

// FileTemplate is a file the game finds by a definition's name, never named in script.
type FileTemplate struct {
	Pattern  string  // a path from a root, with {id} where the definition's name goes: gfx/interface/icons/traits/{id}.dds
	Coverage float64 // the share of the install's definitions that have the file
}

// Accept is one type of value seen at a field, or declared for it by the game's own info file.
type Accept struct {
	Type     ValueType
	Kind     string   // TypeRef: the kind the word names
	Enum     []string // TypeEnum: the closed set of words
	Count    int      // how many times the install wrote this type here; 0 when only declared
	Declared bool     // the game's info file states this type for the field; it outranks what was seen
}

// ValueType is what one written word is.
type ValueType uint8

const (
	TypeNone ValueType = iota // nothing written
	TypeNumber
	TypeBool
	TypeDate
	TypeString
	TypeColor
	TypeLoc        // a localization key
	TypeRef        // the name of a definition, written bare, quoted, or as a cite
	TypeEnum       // one of a closed set of words that no kind defines
	TypeFile       // a path to a file of the install or a mod, in full from a root or by file name alone
	TypeScopePath  // a walk between scopes: root; liege.primary_title; scope:target.culture
	TypeConstant   // @name
	TypeParam      // $NAME$
	TypeCall       // [ … ]
	TypeMember     // a key only: a fixed member of the block it sits in
	TypeToken      // a key only: an effect, trigger or modifier the game declares
	TypeUnresolved // a name-shaped word nothing claims
)

var typeNames = [...]string{"none", "number", "bool", "date", "string", "color", "loc", "ref", "enum", "file", "scope_path", "constant", "param", "call", "member", "token", "unresolved"}

func (t ValueType) String() string { return typeNames[t] }

// Value is what one written word turned out to be.
type Value struct {
	Type     ValueType
	Kind     string // TypeRef: the kind of the definition named; "" when the prefix is declared and no kind holds its definitions
	Name     string // TypeRef, TypeLoc, TypeFile, TypeConstant, TypeParam: the name looked up, with quotes, prefix and parent removed
	Parent   string // TypeRef: the enclosing definition, when the word names it too: define:NGame|START_DATE gives NGame
	Declared bool   // the type rests on something the game declares; only then may a diagnostic accuse on it
}

// Drift is what a scan saw and nothing explained. It is the alarm for a game update that changes the language.
type Drift struct {
	ParseErrors  []Tally // vanilla files that did not parse cleanly, by relative path
	Extensions   []Tally // file extensions with no format
	Folders      []Tally // top-level folders with no rule
	EntryModes   []Tally // MODE: words on a definition's name with no meaning in the registry
	Prefixes     []Tally // prefix spellings neither declared by the vocabulary nor belonging to a kind
	DumpLabels   []Tally // labels in a dump the vocabulary reader does not know
	NameTokens   []Tally // tokens whose values are the names cited under a script-made kind's prefix, and are not in the hand table
	Unclassified []Tally // engine tokens the install writes that learned no entry in Arguments
}

// Tally is one unexplained thing and how often it was seen.
type Tally struct {
	Name  string
	Count int
}

// Field is the field at a path of member names below a definition of the kind, or nil where the install never wrote it.
func (k *Kind) Field(path ...string) *Field {
	f := k.Shape
	for _, name := range path {
		if f == nil {
			return nil
		}
		f = f.Members[name]
	}
	return f
}

// lookups builds the reverse lookups once (prefix to kinds, keyword to kind, folder to kinds, and the kinds that are macros), in kind name order so a shared prefix or keyword always answers the same way.
func (s *Schema) lookups() {
	s.once.Do(func() {
		s.byPrefix, s.byKeyword, s.byFolder = map[string][]*Kind{}, map[string]*Kind{}, map[string][]*Kind{}
		for _, name := range slices.Sorted(maps.Keys(s.Kinds)) {
			k := s.Kinds[name]
			for _, p := range k.Prefixes {
				s.byPrefix[p] = append(s.byPrefix[p], k)
			}
			if k.Naming.Keyword != "" && s.byKeyword[k.Naming.Keyword] == nil {
				s.byKeyword[k.Naming.Keyword] = k
			}
			for _, f := range k.Folders {
				s.byFolder[strings.ToLower(f)] = append(s.byFolder[strings.ToLower(f)], k)
			}
			if k.Macro != 0 {
				s.macros = append(s.macros, k)
			}
		}
	})
}

// KindsAt is the kinds a file at rel, a path from a root, may define: the kinds that list the file itself, else those of its nearest listed folder, so a mod's new subfolder belongs to the database above it. Several where a folder holds several.
func (s *Schema) KindsAt(rel string) []*Kind {
	s.lookups()
	for at := strings.ToLower(rel); at != "." && at != "/" && at != ""; at = path.Dir(at) {
		if kinds := s.byFolder[at]; kinds != nil {
			return kinds
		}
	}
	return nil
}

// ByPrefix is the kinds a prefix cites, in name order; none when the prefix belongs to no kind.
func (s *Schema) ByPrefix(prefix string) []*Kind {
	s.lookups()
	return s.byPrefix[prefix]
}

// ByKeyword is the kind a declaration keyword opens a definition of: scripted_trigger, type, pdxmesh.
func (s *Schema) ByKeyword(word string) *Kind {
	s.lookups()
	return s.byKeyword[word]
}
