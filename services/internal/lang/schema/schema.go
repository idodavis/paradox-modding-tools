// Package schema is what the engine knows about a game's language: the vocabulary the game
// declares in its console dumps (vocab.go), the prose it ships beside its script (info.go), the
// schema a scan learns and the text file it is written to (file.go), and Reader, the one place
// that decides what a written word is (reader.go). schema.go holds the model's types and the
// engine's one hand table.
package schema

import (
	"maps"
	"path"
	"slices"
	"strings"
	"sync"

	"paradox-modding-tools/services/internal/lang/loc"
	"paradox-modding-tools/services/internal/lang/source"
)

// Schema is what one install at one version contains: every database and what is written in it.
type Schema struct {
	Game      string            // ck3 | vic3 | eu5
	Version   string            // the install's version
	Kinds     map[string]*Kind  // every database, by name
	Arguments map[string]*Field // effect or trigger name to what is written after it, learned from every use
	Drift     Drift             // what the scan saw and nothing explained

	once      sync.Once
	byPrefix  map[string][]*Kind
	byKeyword map[string]*Kind
	byFolder  map[string][]*Kind
	macros    []*Kind
}

// Kind is one database of the game: every definition of one type, as brave is one of traits.
type Kind struct {
	Name          string           // traits, gui.type, models.pdxmesh, religions.faiths, history/titles
	Format        source.Format    // the format of the files its definitions are read from
	Folders       []string         // folders or files from a root with its definitions; none when nested or script-made
	Naming        Naming           // where a definition's name is written
	Parent        string           // nested kind: the kind whose definitions contain this kind's
	Subject       string           // subject kind: the kind its entries are about; it defines nothing
	Prefixes      []string         // the prefixes that cite a definition of this kind: character; title; scope
	ScopeType     string           // the scope type a definition is when script runs on it: characters to character
	LabelMember   string           // the member whose value is a definition's human name: characters to name
	LocTemplates  []loc.Convention // the loc keys a definition conventionally has, each with its coverage
	FileTemplates []FileTemplate   // the files the game finds by a definition's name, each with its coverage
	Macro         BodyClass        // the classes of body in which a definition is invoked by writing its name as a key
	Params        bool             // a definition's text holds $NAME$ parameters that the invoking site fills
	Shape         *Field           // what a definition holds: its members for a block, its accepts for one value
}

// Naming is where the name of a definition is written. Form is one of namingForms.
type Naming struct {
	Form    string
	Keyword string // member, declaration: the word opening a definition; key: a word declaring one inline anywhere
	Member  string // member: the member whose value is the name
	Column  int    // column: the column that holds the name
}

// FileTemplate is a file the game finds by a definition's name, never named in script.
type FileTemplate struct {
	Pattern  string  // a path from a root with {id} for the definition's name: gfx/interface/icons/traits/{id}.dds
	Coverage float64 // the share of the install's definitions that have the file
}

// Field is one position below a definition, declared by the game's documentation or learned from
// every use of it under the same parent.
type Field struct {
	Name      string            // the key as written, or a position: <number> <date> <cite> <name> <word> <0> <1>
	Count     int               // how many times the install writes it
	Evidence  Evidence          // declared where the documentation writes the position, else observed
	Doc       string            // what the documentation says of it
	Default   string            // the value it takes when none is written, where the documentation says
	Need      Need              // whether a definition may leave it out, where the documentation says
	Class     Class             // what is written at it
	Body      BodyClass         // ClassBody: which vocabularies its statements come from
	BodyScope string            // ClassBody: the scope type its statements run in; "" where tokens disagree or state none
	Accepts   []Accept          // ClassValue, ClassList: each type and kind seen or declared; declared first, then by use
	Ops       []string          // the operators written after this key other than =
	Defines   string            // the nested kind whose definitions are this block's name keys, or a list's bare words
	KeyedBy   string            // the kind this block's name keys cite when defined elsewhere: goods = { wheat = 0.5 }
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

// BodyClass says which vocabularies a body's statements come from, as bits.
type BodyClass uint8

const (
	BodyTrigger BodyClass = 1 << iota
	BodyEffect
	BodyModifier
	BodyMath // script-value arithmetic: add, multiply, min, max
)

// Need is whether a definition may leave a field out, as the documentation says it.
type Need struct {
	Level    NeedLevel
	When     string   // NeedRequiredWhen: the condition, "for letter events"
	Evidence Evidence // declared where the documentation says it, inferred where a default implies it
}

// NeedLevel is how firmly a definition needs a field.
type NeedLevel uint8

const (
	NeedUnstated     NeedLevel = iota
	NeedOptional               // "optional", [Optional], or a default the documentation gives
	NeedRequiredWhen           // required under a condition: "required for letter events"
	NeedRequired               // "This field is required.", "Must be set."
)

// Accept is one type of value seen at a field, or declared for it by the game's documentation.
type Accept struct {
	Type     ValueType
	Kind     string   // TypeRef: the kind the word names; in a documentation template, the kind's folder
	Enum     []string // TypeEnum: the set of words
	Count    int      // how many times the install wrote this type here; 0 when only declared
	Evidence Evidence // a declared type outranks what was seen; an inferred one never accuses
}

// Evidence is how the schema knows a fact: seen in the install, stated by the game, or guessed.
// Only a declared fact may accuse; an inferred one is shown with a label that says so.
type Evidence uint8

const (
	Observed Evidence = iota // seen in the install's files
	Declared                 // stated by the game: its dumps or its documentation
	Inferred                 // guessed from one word: an example's kind, the words of an open list
)

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

// Value is what one written word turned out to be.
type Value struct {
	Type ValueType `json:"type"`
	// TypeRef: the kind of the definition named; "" for a declared prefix no kind holds
	Kind string `json:"kind"`
	// TypeRef/Loc/File/Constant/Param: the name looked up, without quotes, prefix or parent
	Name string `json:"name"`
	// TypeRef: the enclosing definition the word also names; define:NGame|START_DATE gives NGame
	Parent string `json:"parent"`
	// the type rests on something the game declares; only then may a diagnostic accuse on it
	Declared bool `json:"declared"`
}

// Drift is what a scan saw and nothing explained: the sign a game update changed the language.
type Drift struct {
	ParseErrors  []Tally `json:"parseErrors"`  // vanilla files that did not parse cleanly, by relative path
	Extensions   []Tally `json:"extensions"`   // file extensions with no format
	Folders      []Tally `json:"folders"`      // top-level folders with no rule
	EntryModes   []Tally `json:"entryModes"`   // MODE: words on a name that the registry gives no meaning
	Prefixes     []Tally `json:"prefixes"`     // prefix spellings the vocabulary does not declare and no kind holds
	DumpLabels   []Tally `json:"dumpLabels"`   // labels in a dump the vocabulary reader does not know
	DocLines     []Tally `json:"docLines"`     // lines of the documentation no form reads, by file:line
	NameTokens   []Tally `json:"nameTokens"`   // tokens outside the hand table valued with a script-made kind's cites
	Unclassified []Tally `json:"unclassified"` // engine tokens the install writes that learned no entry in Arguments
}

// Tally is one unexplained thing and how often it was seen.
type Tally struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// NameToken is one token and one place in it that holds a name. A token written with one word
// takes that word as the name; Member says where the name is when the token takes a block.
type NameToken struct {
	Token   string // set_variable, has_variable, save_scope_as, add_to_list, every_in_list
	Kind    string // the script-made kind the name belongs to, one of ScriptKinds
	Defines bool   // the token creates the name. false: it reads, changes or removes one
	Member  string // the member holding the name in a block: name, list, variable, flag; "" when it takes no block
}

// namingForms is every way a definition is named: key (brave = { }), number (186004 = { }),
// event_id (birth.1001 = { }), constant (@x = 5), member (pdxmesh = { name = "x" }), declaration
// (type x = button { }), file (history/cultures/afghan.txt), a table's column, a list file's
// line, param ($NAME$ in a macro's text), token (a name token's argument), and none.
var namingForms = []string{
	"key", "number", "event_id", "constant", "member", "declaration", "file", "column", "line", "param", "token", "none",
}

// The names of the enums' values, as the schema file writes them.
var (
	classNames    = [...]string{"", "value", "list", "structure", "database", "body"}
	bodyNames     = [...]string{"trigger", "effect", "modifier", "math"}
	needNames     = [...]string{"", "optional", "required_when", "required"}
	evidenceNames = [...]string{"observed", "declared", "inferred"}
	typeNames     = [...]string{
		"none", "number", "bool", "date", "string", "color", "loc", "ref", "enum", "file", "scope_path", "constant", "param",
		"call", "member", "token", "unresolved",
	}
)

// ScriptKinds is the script-made kinds the learner makes, each with the declared prefix that
// cites its names, or "" when none does.
var ScriptKinds = map[string]string{
	"saved_scopes": "scope", "variables": "var", "local_variables": "local_var", "global_variables": "global_var",
	"lists": "", "flags": "",
}

// variableTokens is the tokens of a variable scope, spelled for the plain one; the local and
// global scopes put their word before "variable": set_local_variable. A variable list or map
// holds several targets, but its name is a variable too.
var variableTokens = []NameToken{
	{Token: "set_variable", Defines: true, Member: "name"}, {Token: "change_variable", Member: "name"},
	{Token: "clamp_variable", Member: "name"}, {Token: "round_variable", Member: "name"},
	{Token: "has_variable"}, {Token: "remove_variable"},

	{Token: "add_to_variable_list", Defines: true, Member: "name"}, {Token: "remove_list_variable", Member: "name"},
	{Token: "clear_variable_list"}, {Token: "has_variable_list"},
	{Token: "variable_list_size", Member: "name"}, {Token: "is_target_in_variable_list", Member: "name"},
	{Token: "sort_variable_list", Member: "name"},

	{Token: "add_to_variable_map", Defines: true, Member: "name"}, {Token: "remove_from_variable_map", Member: "name"},
	{Token: "clear_variable_map"}, {Token: "has_variable_map"},
	{Token: "variable_map_size", Member: "name"}, {Token: "is_key_in_variable_map", Member: "name"},
	{Token: "is_value_in_variable_map", Member: "name"},
	{Token: "any_key_in_variable_map", Member: "variable"}, {Token: "every_key_in_variable_map", Member: "variable"},
	{Token: "ordered_key_in_variable_map", Member: "variable"}, {Token: "random_key_in_variable_map", Member: "variable"},
}

// nameTokens is the engine's one hand table, shared by the three games: the effects and triggers
// that make or read a name while script runs. A saved scope, a variable, a list and a character
// flag have no folder and no definition, so nothing can learn them; every row is a token some
// game declares. nameTokensByToken is the same rows by token.
var nameTokens, nameTokensByToken = func() ([]NameToken, map[string][]NameToken) {
	rows := []NameToken{
		{Token: "save_scope_as", Kind: "saved_scopes", Defines: true},
		{Token: "save_temporary_scope_as", Kind: "saved_scopes", Defines: true},
		{Token: "save_scope_value_as", Kind: "saved_scopes", Defines: true, Member: "name"},
		{Token: "save_temporary_scope_value_as", Kind: "saved_scopes", Defines: true, Member: "name"},
		{Token: "save_opinion_value_as", Kind: "saved_scopes", Defines: true, Member: "name"},
		{Token: "save_temporary_opinion_value_as", Kind: "saved_scopes", Defines: true, Member: "name"},
		{Token: "clear_saved_scope", Kind: "saved_scopes"},

		{Token: "add_to_list", Kind: "lists", Defines: true},
		{Token: "add_to_temporary_list", Kind: "lists", Defines: true, Member: "name"},
		{Token: "remove_from_list", Kind: "lists"}, {Token: "is_in_list", Kind: "lists"},

		// CK3 alone: the variables of the dead, several names under one token, and character flags
		{Token: "set_dead_character_variable", Kind: "variables", Defines: true, Member: "name"},
		{Token: "has_dead_character_variable", Kind: "variables"},
		{Token: "remove_dead_character_variable", Kind: "variables"},
		{Token: "has_all_variables", Kind: "variables", Member: "name"},
		{Token: "has_none_of_variables", Kind: "variables", Member: "name"},
		{Token: "add_character_flag", Kind: "flags", Defines: true, Member: "flag"},
		{Token: "has_character_flag", Kind: "flags"}, {Token: "remove_character_flag", Kind: "flags"},
		{Token: "add_dead_character_flag", Kind: "flags", Defines: true, Member: "flag"},
		{Token: "has_dead_character_flag", Kind: "flags"},
	}

	// every variable token, in its three scopes
	for _, scope := range []string{"variable", "local_variable", "global_variable"} {
		for _, row := range variableTokens {
			row.Token, row.Kind = strings.Replace(row.Token, "variable", scope, 1), scope+"s"
			rows = append(rows, row)
		}
	}

	// a list iterator reads a list at list = name and a variable list at variable = name; the
	// local and global ones read only variable lists
	for _, prefix := range iteratorPrefixes {
		rows = append(rows,
			NameToken{Token: prefix + "in_list", Kind: "lists", Member: "list"},
			NameToken{Token: prefix + "in_list", Kind: "variables", Member: "variable"},
			NameToken{Token: prefix + "in_local_list", Kind: "local_variables", Member: "variable"},
			NameToken{Token: prefix + "in_global_list", Kind: "global_variables", Member: "variable"})
	}

	byToken := map[string][]NameToken{}
	for _, row := range rows {
		byToken[row.Token] = append(byToken[row.Token], row)
	}
	return rows, byToken
}()

func (class Class) String() string { return classNames[class] }

func (level NeedLevel) String() string { return needNames[level] }

func (evidence Evidence) String() string { return evidenceNames[evidence] }

func (valueType ValueType) String() string { return typeNames[valueType] }

// lookups builds the reverse lookups once: prefix, keyword and folder to kinds, and the macros.
// Kinds go in name order, so a shared prefix or keyword always answers the same way.
func (schema *Schema) lookups() {
	schema.once.Do(func() {
		schema.byPrefix, schema.byKeyword, schema.byFolder = map[string][]*Kind{}, map[string]*Kind{}, map[string][]*Kind{}
		for _, name := range slices.Sorted(maps.Keys(schema.Kinds)) {
			kind := schema.Kinds[name]
			for _, prefix := range kind.Prefixes {
				schema.byPrefix[prefix] = append(schema.byPrefix[prefix], kind)
			}
			if kind.Naming.Keyword != "" && schema.byKeyword[kind.Naming.Keyword] == nil {
				schema.byKeyword[kind.Naming.Keyword] = kind
			}
			for _, folder := range kind.Folders {
				schema.byFolder[strings.ToLower(folder)] = append(schema.byFolder[strings.ToLower(folder)], kind)
			}
			if kind.Macro != 0 {
				schema.macros = append(schema.macros, kind)
			}
		}
	})
}

// KindsAt is the kinds a file at rel, a path from a root, may define: those listing the file
// itself, else those of its nearest listed folder, so a mod's new subfolder belongs to the
// database above it.
func (schema *Schema) KindsAt(rel string) []*Kind {
	schema.lookups()
	for at := strings.ToLower(rel); at != "." && at != "/" && at != ""; at = path.Dir(at) {
		if kinds := schema.byFolder[at]; kinds != nil {
			return kinds
		}
	}
	return nil
}

// ByPrefix is the kinds a prefix cites, in name order; none when the prefix belongs to no kind.
func (schema *Schema) ByPrefix(prefix string) []*Kind {
	schema.lookups()
	return schema.byPrefix[prefix]
}

// ByKeyword is the kind whose definitions a keyword opens: scripted_trigger, type, pdxmesh.
func (schema *Schema) ByKeyword(word string) *Kind {
	schema.lookups()
	return schema.byKeyword[word]
}

// NameTokensOf is a token's rows, one per member holding a name; none when it makes or reads none.
func NameTokensOf(token string) []NameToken {
	return nameTokensByToken[token]
}
