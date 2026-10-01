// Package schema is what the engine knows about a game's language: what the game declares in its
// console dumps (dumps.go) and in the docs it ships beside its script (docs.go), the databases and
// keys built from those and from vanilla, and Reader, the one place that decides what a written word
// is (reader.go). schema.go holds the model's types and the engine's one hand table.
package schema

import (
	"cmp"
	"maps"
	"path"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/loc"
	"paradox-modding-tools/services/internal/lang/source"
)

// Schema is what one install at one version contains: every database and what is written in it.
type Schema struct {
	Game         string               // ck3 | vic3 | eu5
	Version      string               // the install's version
	EntryModes   []game.EntryMode     // the words a mod may write before an entry's name, from the registry
	Dumps        *Dumps               // what the game's console dumps declare; empty, never nil, with none
	DumpsFrom    DumpsFrom            // whose dumps each set is
	DumpsVersion string               // the game version of the shipped dumps, when they are used
	Docs         Docs                 // what the game's docs say, by folder
	Wiki         Wiki                 // what the wiki says that the docs miss
	Databases    map[string]*Database // every database, by name
	Arguments    map[string]*Key      // a command's name to what is written after it
	Unread       Unread               // what the scan saw and nothing explained

	byPrefix  map[string][]*Database
	byKeyword map[string]*Database
	byFolder  map[string][]*Database
	scripted  []*Database
	added     map[string]bool // a workspace schema's databases its own build added, which Settle settles
}

// DumpsFrom says whose each dump set is: "yours" (the user's, not older than the install),
// "shipped" (PMT's copy) or "none".
type DumpsFrom struct {
	ScriptDocs string `json:"scriptDocs"`
	DataTypes  string `json:"dataTypes"`
}

// Database is one database of the game: every entry of one type, as brave is one of traits.
type Database struct {
	Name       string           // traits, gui.type, asset.pdxmesh, religion_types.faiths, history/titles
	Format     source.Format    // the format of the files its entries are read from
	Folders    []string         // folders or files from a root with its entries; none when nested or script-made
	Naming     Naming           // where an entry's name is written
	Parent     string           // nested: the database whose entries contain this one's
	Prefixes   []string         // the prefixes that name an entry of it: character; title; scope
	Scope      string           // the scope an entry is when script runs on it: characters to character
	LocKeys    []loc.Convention // the loc keys an entry conventionally has, each with its coverage
	Files      []FilePattern    // the files the game finds by an entry's name, each with its coverage
	HistoryOf  []string         // a history database: the databases whose entries its entries are named after
	CalledIn   []Block          // a scripted database: the blocks that call an entry by writing its name as a key
	Parameters bool             // a scripted database: a call passes $NAME$ parameters
	Fired      bool             // its entries are fired: events and on_actions
	Root       Root             // what the wiki says decides an entry's starting scope
	Entry      *Key             // what an entry holds: its keys for a block, its takes for one word
}

// Naming is where the name of an entry is written. Form is one of key (brave = { }), number
// (186004 = { }), event_id (birth.1001 = { }), constant (@x = 5), value (pdxmesh = { name = "x" }),
// declaration (type x = button { }), file (history/cultures/afghan.txt), column (a table's), line (a
// list file's), parameter ($NAME$ in a scripted entry's text), command (a command's argument) and none.
type Naming struct {
	Form    string
	Keyword string // value, declaration: the word opening an entry; key: a word declaring one inline anywhere
	NameKey string // value: the key whose value is the name
	Column  int    // column: the column that holds the name
}

// FilePattern is a file the game finds by a definition's name, never named in script.
type FilePattern struct {
	Pattern  string  // a path from a root with {id} for the definition's name: gfx/interface/icons/traits/{id}.dds
	Coverage float64 // the share of the install's definitions that have the file
}

// Key is one position below an entry: its name, and what the facts say is written there.
type Key struct {
	Name     string            // the key as written, or a position: <number> <date> <prefixed> <name> <word> <0> <1>
	Source   Source            // where the fact comes from, the first in the owner's order that says it
	Doc      string            // what the docs say of it
	Default  string            // the value it takes when none is written, where the docs say
	Required Required          // whether an entry may leave it out, where the docs say
	When     string            // RequiredWhen: the condition, "for letter events"
	Written  Written           // what is written at it: a word, a list or a block
	Block    Block             // WrittenBlock: what its statements are
	Scope    string            // WrittenBlock: the scope its statements run in; "" where none is stated
	Takes    []Take            // WrittenWord, WrittenList: what a word here may be; declared first
	Defines  string            // the nested database whose entries are this block's keys, or a list's words
	Names    string            // the database whose entries this block's keys name: goods = { wheat = 0.5 }
	Keys     map[string]*Key   // the keys below, by Key.Name
	Saved    map[string]string // the saved scopes the docs say the block starts with, to their scope
	Page     string            // SourceWiki: the wiki page the fact comes from
	Revision int               // SourceWiki: that page's revision
}

// Written is what is written at a key.
type Written uint8

const (
	WrittenNone  Written = iota
	WrittenWord          // one word: is_shown = yes
	WrittenList          // a block of bare words: categories = { a b c }
	WrittenBlock         // a block of keys: portrait = { … }, faiths = { catholic = { … } }, trigger = { … }
)

// Block is what a block's statements are, where they are commands. A block holds one kind: of the
// blocks the three games' dumps and docs describe, none holds two.
type Block uint8

const (
	BlockNone      Block = iota
	BlockTriggers        // limit = { <triggers> }
	BlockEffects         // immediate = { <effects> }
	BlockModifiers       // modifier = { <modifiers> }
	BlockValues          // script-value arithmetic: add, multiply, min, max
)

// Required is whether an entry may leave a key out, as the docs say it, weakest first.
type Required uint8

const (
	RequiredUnstated Required = iota
	RequiredOptional          // "optional", [Optional], or a default the docs give
	RequiredWhen              // required under a condition, Key.When: "required for letter events"
	RequiredAlways            // "This field is required.", "Must be set."
)

// Take is one type a word at a key may be, declared by the game or seen in vanilla.
type Take struct {
	Type     WordType
	Database string   // WordReference: the database the word names; in a doc template, its folder
	Enum     []string // WordEnum: the set of words
	Source   Source   // where the take comes from
	Guessed  bool     // read from an example or an open list: shown, never accused on
}

// Source is where a fact comes from, in the owner's order of trust when two say different things.
type Source uint8

const (
	SourceVanilla  Source = iota // what the install's files write
	SourceDumps                  // the game's console dumps
	SourceDocs                   // the game's docs: .info, readmes, .md, script headers
	SourceWiki                   // the game's wiki, for what the docs miss
	SourceRegistry               // a row of game.go
)

// Declared says the game itself states the fact: only such a fact, when not guessed, may accuse.
func (source Source) Declared() bool {
	return source == SourceDumps || source == SourceDocs || source == SourceRegistry
}

// WordType is what one written word is.
type WordType uint8

const (
	WordNone WordType = iota // nothing written
	WordNumber
	WordBool
	WordDate
	WordString
	WordColor
	WordLoc       // a localization key
	WordReference // the name of a definition, written bare, quoted, or as a prefixed word
	WordEnum      // one of a closed set of words that no database defines
	WordFile      // a path to a file of the install or a mod, in full from a root or by file name alone
	WordScopePath // a walk between scopes: root; liege.primary_title; scope:target.culture
	WordConstant  // @name
	WordParameter // $NAME$
	WordDataCall  // [ … ]
	WordKey       // a key only: a fixed key of the block it sits in
	WordCommand   // a key only: an effect, trigger or modifier the game declares
	WordUnknown   // a name-shaped word nothing claims
)

// Word is what one written word turned out to be.
type Word struct {
	Type WordType `json:"type"`
	// WordReference: the database of the entry named; "" for a declared prefix no database holds
	Database string `json:"database"`
	// WordReference/Loc/File/Constant/Parameter: the name looked up, without quotes, prefix or parent
	Name string `json:"name"`
	// WordReference: the enclosing entry the word also names; define:NGame|START_DATE gives NGame
	Parent string `json:"parent"`
	// the type rests on something the game declares; only then may a diagnostic accuse on it
	Declared bool `json:"declared"`
	// the type is a guess: a name one database defines, or a loc key that exists; never accused on
	Guessed bool `json:"guessed"`
}

// Unread is what a scan saw and nothing explained: the sign a game update changed the language.
type Unread struct {
	ParseErrors []Tally `json:"parseErrors"` // vanilla files that did not parse cleanly, by relative path
	Extensions  []Tally `json:"extensions"`  // file extensions with no format
	Folders     []Tally `json:"folders"`     // top-level folders with no rule
	EntryModes  []Tally `json:"entryModes"`  // MODE: words on a name that the registry gives no meaning
	Prefixes    []Tally `json:"prefixes"`    // prefix spellings the dumps do not declare and no database holds
	DumpLabels  []Tally `json:"dumpLabels"`  // labels in a dump the dump reader does not know
	DocLines    []Tally `json:"docLines"`    // lines of the docs no form reads, by file:line
}

// Tally is one unexplained thing and how often it was seen.
type Tally struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// NameCommand is one command and one place in it that holds a name. A command written with one word
// takes that word as the name; NameKey says where the name is when the command takes a block.
type NameCommand struct {
	Command  string // set_variable, has_variable, save_scope_as, add_to_list, every_in_list
	Database string // the script-made database the name belongs to, one of ScriptMade
	Defines  bool   // the command creates the name. false: it reads, changes or removes one
	NameKey  string // the key holding the name in a block: name, list, variable, flag; "" when it takes no block
}

// typeNames spells each WordType as the event detail's groups and the diagnostics show it.
var typeNames = [...]string{
	"none", "number", "bool", "date", "string", "color", "loc", "reference", "enum", "file", "scope_path", "constant",
	"parameter", "data_call", "key", "command", "unknown",
}

// ScriptMade is the script-made databases, each with the declared prefix that names its entries, or
// "" when none does.
var ScriptMade = map[string]string{
	"saved_scopes": "scope", "variables": "var", "local_variables": "local_var", "global_variables": "global_var",
	"lists": "", "flags": "",
}

// variableCommands is the commands of a variable scope, spelled for the plain one; the local and
// global scopes put their word before "variable": set_local_variable. A variable list or map
// holds several targets, but its name is a variable too.
var variableCommands = []NameCommand{
	{Command: "set_variable", Defines: true, NameKey: "name"}, {Command: "change_variable", NameKey: "name"},
	{Command: "clamp_variable", NameKey: "name"}, {Command: "round_variable", NameKey: "name"},
	{Command: "has_variable"}, {Command: "remove_variable"},

	{Command: "add_to_variable_list", Defines: true, NameKey: "name"}, {Command: "remove_list_variable", NameKey: "name"},
	{Command: "clear_variable_list"}, {Command: "has_variable_list"},
	{Command: "variable_list_size", NameKey: "name"}, {Command: "is_target_in_variable_list", NameKey: "name"},
	{Command: "sort_variable_list", NameKey: "name"},

	{Command: "add_to_variable_map", Defines: true, NameKey: "name"}, {Command: "remove_from_variable_map", NameKey: "name"},
	{Command: "clear_variable_map"}, {Command: "has_variable_map"},
	{Command: "variable_map_size", NameKey: "name"}, {Command: "is_key_in_variable_map", NameKey: "name"},
	{Command: "is_value_in_variable_map", NameKey: "name"},
	{Command: "any_key_in_variable_map", NameKey: "variable"}, {Command: "every_key_in_variable_map", NameKey: "variable"},
	{Command: "ordered_key_in_variable_map", NameKey: "variable"}, {Command: "random_key_in_variable_map", NameKey: "variable"},
}

// nameCommands is the engine's one hand table, shared by the three games: the effects and triggers
// that make or read a name while script runs. A saved scope, a variable, a list and a character
// flag have no folder and no definition, so nothing can learn them; every row is a command some
// game declares. nameCommandsByCommand is the same rows by command.
var nameCommands, nameCommandsByCommand = func() ([]NameCommand, map[string][]NameCommand) {
	rows := []NameCommand{
		{Command: "save_scope_as", Database: "saved_scopes", Defines: true},
		{Command: "save_temporary_scope_as", Database: "saved_scopes", Defines: true},
		{Command: "save_scope_value_as", Database: "saved_scopes", Defines: true, NameKey: "name"},
		{Command: "save_temporary_scope_value_as", Database: "saved_scopes", Defines: true, NameKey: "name"},
		{Command: "save_opinion_value_as", Database: "saved_scopes", Defines: true, NameKey: "name"},
		{Command: "save_temporary_opinion_value_as", Database: "saved_scopes", Defines: true, NameKey: "name"},
		{Command: "clear_saved_scope", Database: "saved_scopes"},

		{Command: "add_to_list", Database: "lists", Defines: true},
		{Command: "add_to_temporary_list", Database: "lists", Defines: true, NameKey: "name"},
		{Command: "remove_from_list", Database: "lists"}, {Command: "is_in_list", Database: "lists"},

		// CK3 alone: the variables of the dead, several names under one command, and character flags
		{Command: "set_dead_character_variable", Database: "variables", Defines: true, NameKey: "name"},
		{Command: "has_dead_character_variable", Database: "variables"},
		{Command: "remove_dead_character_variable", Database: "variables"},
		{Command: "has_all_variables", Database: "variables", NameKey: "name"},
		{Command: "has_none_of_variables", Database: "variables", NameKey: "name"},
		{Command: "add_character_flag", Database: "flags", Defines: true, NameKey: "flag"},
		{Command: "has_character_flag", Database: "flags"}, {Command: "remove_character_flag", Database: "flags"},
		{Command: "add_dead_character_flag", Database: "flags", Defines: true, NameKey: "flag"},
		{Command: "has_dead_character_flag", Database: "flags"},
	}

	// every variable command, in its three scopes
	for _, scope := range []string{"variable", "local_variable", "global_variable"} {
		for _, row := range variableCommands {
			row.Command, row.Database = strings.Replace(row.Command, "variable", scope, 1), scope+"s"
			rows = append(rows, row)
		}
	}

	// a list iterator reads a list at list = name and a variable list at variable = name; the
	// local and global ones read only variable lists
	for _, prefix := range iteratorPrefixes {
		rows = append(rows,
			NameCommand{Command: prefix + "in_list", Database: "lists", NameKey: "list"},
			NameCommand{Command: prefix + "in_list", Database: "variables", NameKey: "variable"},
			NameCommand{Command: prefix + "in_local_list", Database: "local_variables", NameKey: "variable"},
			NameCommand{Command: prefix + "in_global_list", Database: "global_variables", NameKey: "variable"})
	}

	byCommand := map[string][]NameCommand{}
	for _, row := range rows {
		byCommand[row.Command] = append(byCommand[row.Command], row)
	}
	return rows, byCommand
}()

func (wordType WordType) String() string { return typeNames[wordType] }

// BuildLookups builds the reverse lookups: prefix, keyword and folder to databases, and the scripted
// ones. Databases go in name order, so a shared prefix or keyword always answers the same way. The
// schema is read-only after, and shared with no lock.
func (schema *Schema) BuildLookups() {
	schema.byPrefix, schema.byKeyword, schema.byFolder, schema.scripted = map[string][]*Database{}, map[string]*Database{}, map[string][]*Database{}, nil
	for _, name := range slices.Sorted(maps.Keys(schema.Databases)) {
		database := schema.Databases[name]
		for _, prefix := range database.Prefixes {
			schema.byPrefix[prefix] = append(schema.byPrefix[prefix], database)
		}
		if database.Naming.Keyword != "" && schema.byKeyword[database.Naming.Keyword] == nil {
			schema.byKeyword[database.Naming.Keyword] = database
		}
		for _, folder := range database.Folders {
			schema.byFolder[strings.ToLower(folder)] = append(schema.byFolder[strings.ToLower(folder)], database)
		}
		if len(database.CalledIn) > 0 {
			schema.scripted = append(schema.scripted, database)
		}
	}
}

// DatabasesAt is the databases a file at rel, a path from a root, may define: those listing the
// file itself, else those of its nearest listed folder, so a mod's new subfolder belongs to the
// database above it.
func (schema *Schema) DatabasesAt(rel string) []*Database {
	for at := strings.ToLower(rel); at != "." && at != "/" && at != ""; at = path.Dir(at) {
		if databases := schema.byFolder[at]; databases != nil {
			return databases
		}
	}
	return nil
}

// ByPrefix is the databases a prefix names, in name order; none when the prefix belongs to no
// database.
func (schema *Schema) ByPrefix(prefix string) []*Database {
	return schema.byPrefix[prefix]
}

// ByKeyword is the database whose definitions a keyword opens: scripted_trigger, type, pdxmesh.
func (schema *Schema) ByKeyword(word string) *Database {
	return schema.byKeyword[word]
}

// NameCommandsOf is a command's rows, one per key holding a name; none when it makes or reads none.
func NameCommandsOf(command string) []NameCommand {
	return nameCommandsByCommand[command]
}

// tallies is counts as tallies, by name.
func tallies(counts map[string]int) []Tally {
	var out []Tally
	for _, name := range slices.Sorted(maps.Keys(counts)) {
		out = append(out, Tally{Name: name, Count: counts[name]})
	}
	return out
}

// Input is what one schema is built from: the registry's rows, the files, the one read of their
// script, and the layers. A workspace's build gives Base, the install's schema.
type Input struct {
	Game    *game.GameInfo
	Version string
	Files   []source.File
	Scripts *Scripts
	Dumps   *Dumps
	Docs    Docs
	Wiki    Wiki
	Base    *Schema
}

// builder is one build: the schema, what it is built from, the databases it adds, each entry name's
// databases, and the positions of names whose owner is settled once every nested database is made.
type builder struct {
	schema *Schema
	input  Input
	added  map[string]bool
	owners map[string]map[string]bool
	named  []struct {
		key      *Key
		database *Database
		names    []string
	}
	calledIn map[string][]Block // a scripted entry's name to the blocks it is called in
}

// Build is a schema: the databases of the folders, file types, tables and lists, each entry what the
// docs, the wiki and vanilla write at it. With Base, the install's databases are kept and only a
// folder they lack adds one.
func Build(input Input) *Schema {
	schema := &Schema{
		Game: input.Game.ID, Version: input.Version, EntryModes: input.Game.EntryModes, Dumps: cmp.Or(input.Dumps, &Dumps{}),
		Docs: input.Docs, Wiki: input.Wiki, Databases: map[string]*Database{}, Arguments: map[string]*Key{},
	}
	build := &builder{schema: schema, input: input, added: map[string]bool{}}
	if input.Base != nil {
		maps.Copy(schema.Databases, input.Base.Databases)
		maps.Copy(schema.Arguments, input.Base.Arguments)
		schema.DumpsFrom, schema.DumpsVersion, schema.added = input.Base.DumpsFrom, input.Base.DumpsVersion, build.added
	}
	build.databases()
	build.entries()
	build.blocks()
	build.hoist()
	build.lift()
	build.takes()
	schema.BuildLookups()
	build.unread()
	return schema
}

// databases adds one database per .txt group, per file type's keyword and named block, the map
// table, each list file, EU5's sound events, and on an install the script-made ones.
func (build *builder) databases() {
	schema, input := build.schema, build.input
	held := map[string]bool{}
	for _, database := range schema.Databases {
		for _, folder := range database.Folders {
			held[strings.ToLower(folder)] = true
		}
	}
	add := func(database *Database) {
		taken := slices.ContainsFunc(database.Folders, func(folder string) bool { return held[strings.ToLower(folder)] })
		if schema.Databases[database.Name] == nil && !taken {
			schema.Databases[database.Name], build.added[database.Name] = database, true
		}
	}

	for _, name := range slices.Sorted(maps.Keys(input.Scripts.Groups)) {
		folders := input.Scripts.Groups[name]
		row, _ := FolderRow(input.Game, folders[0])
		database := &Database{
			Name: name, Format: source.Script, Folders: folders, HistoryOf: row.Of, Parameters: row.Parameters, Fired: row.Events || row.OnActions,
			Naming: Naming{Form: "key", Keyword: row.Keyword, NameKey: row.NameKey},
		}
		names := input.Scripts.Names[name]
		switch {
		case row.Events:
			database.Naming.Form = "event_id"
		case row.Unnamed:
			database.Naming.Form = "none"
		case row.ByFile:
			database.Naming.Form = "file"
		case row.NameKey != "":
			database.Naming.Form = "value"
		case len(names) > 0 && !slices.ContainsFunc(slices.Collect(maps.Keys(names)), func(name string) bool { return jomini.Literal(name) != jomini.LitNumber }):
			database.Naming.Form = "number"
		}
		for _, word := range row.CalledIn {
			database.CalledIn = append(database.CalledIn, map[string]Block{"effect": BlockEffects, "trigger": BlockTriggers, "value": BlockValues}[word])
		}
		for _, folder := range folders {
			if root, ok := input.Wiki.Roots[strings.ToLower(folder)]; ok {
				database.Root = root
			}
		}
		add(database)
	}

	// a file type's databases span the root: gui.type, gui.window, gui.texticon, asset.pdxmesh
	for _, ext := range slices.Sorted(maps.Keys(input.Game.FileTypes)) {
		fileType, prefix := input.Game.FileTypes[ext], strings.TrimPrefix(ext, ".")+"."
		for _, keyword := range fileType.Keywords {
			add(&Database{Name: prefix + keyword, Format: source.Script, Naming: Naming{Form: "declaration", Keyword: keyword}})
		}
		for _, key := range slices.Sorted(maps.Keys(fileType.NameKeyOf)) {
			add(&Database{Name: prefix + key, Format: source.Script, Naming: Naming{Form: "value", Keyword: key, NameKey: fileType.NameKeyOf[key]}})
		}
		if fileType.WidgetKey != "" {
			add(&Database{Name: prefix + "widget", Format: source.Script, Naming: Naming{Form: "value", NameKey: fileType.WidgetKey}})
		}
	}
	for _, name := range slices.Sorted(maps.Keys(input.Scripts.Entries)) {
		if ext, key, typed := strings.Cut(name, "."); typed && input.Game.FileTypes["."+ext].NameKey != "" {
			add(&Database{Name: name, Format: source.Script, Naming: Naming{Form: "value", Keyword: key, NameKey: input.Game.FileTypes["."+ext].NameKey}})
		}
	}

	// the map table, the list files, EU5's Wwise events, and the script-made databases
	if table := input.Game.MapTable; table.Database != "" {
		add(&Database{Name: table.Database, Format: source.Table, Folders: []string{table.File}, Naming: Naming{Form: "column"}})
	}
	for _, file := range input.Files {
		stem := strings.ToLower(strings.TrimSuffix(path.Base(file.Rel), path.Ext(file.Rel)))
		switch {
		case file.Format == source.List:
			add(&Database{Name: path.Base(strings.ToLower(path.Dir(file.Rel))) + "." + stem, Format: source.List, Folders: []string{file.Rel}, Naming: Naming{Form: "line"}})
		case len(input.Scripts.Sounds) > 0 && strings.EqualFold(path.Base(file.Rel), "SoundbanksInfo.json"):
			add(&Database{Name: "sound.events", Format: source.JSON, Folders: []string{file.Rel}, Naming: Naming{Form: "line"}})
		}
	}
	if input.Base == nil {
		for _, name := range slices.Sorted(maps.Keys(ScriptMade)) {
			made := &Database{Name: name, Format: source.Script, Naming: Naming{Form: "command"}}
			if prefix := ScriptMade[name]; prefix != "" {
				made.Prefixes = []string{prefix}
			}
			add(made)
		}
		add(&Database{Name: "constants", Format: source.Script, Naming: Naming{Form: "constant"}})
		add(&Database{Name: "params", Format: source.Script, Naming: Naming{Form: "parameter"}})
	}
}

// entries builds each added database's entry from its folders' docs, the wiki's, and vanilla's shape.
func (build *builder) entries() {
	build.owners = map[string]map[string]bool{}
	for database, names := range build.input.Scripts.Names {
		if held := build.schema.Databases[database]; held != nil && len(held.HistoryOf) > 0 {
			continue
		}
		for name := range names {
			build.own(name, database)
		}
		var nested func(shape *Shape)
		nested = func(shape *Shape) {
			for name := range shape.Names {
				build.own(name, database)
			}
			for _, below := range shape.Keys {
				nested(below)
			}
		}
		if shape := build.input.Scripts.Entries[database]; shape != nil {
			nested(shape)
		}
	}

	for _, name := range slices.Sorted(maps.Keys(build.added)) {
		database := build.schema.Databases[name]
		docs, wiki, shape := folderDocs(build.input.Docs, database), folderDocs(build.input.Wiki.Docs, database), build.input.Scripts.Entries[name]
		if docs != nil || wiki != nil || shape != nil {
			database.Entry = build.key(database, nil, "", docs, wiki, shape)
		}

		// a scripted row says what block its entry is
		if row, _ := FolderRow(build.input.Game, strings.Join(database.Folders[:min(1, len(database.Folders))], "")); len(row.Body) > 0 && database.Entry != nil {
			database.Entry.Written = WrittenBlock
			database.Entry.Block = map[string]Block{"effect": BlockEffects, "trigger": BlockTriggers, "value": BlockValues}[row.Body[0]]
		}
	}

	// a nested database now owns its names, so a position naming them names it (the map's provinces)
	for _, named := range build.named {
		named.key.Names = cmp.Or(build.owner(named.database, named.names), named.key.Names)
	}
}

// own records that a database has an entry of a name.
func (build *builder) own(name, database string) {
	if build.owners[name] == nil {
		build.owners[name] = map[string]bool{}
	}
	build.owners[name][database] = true
}

// folderDocs is the entry the docs write for the first of a database's folders that has any.
func folderDocs(docs Docs, database *Database) *Key {
	for _, folder := range database.Folders {
		if folderDocs := docs[strings.ToLower(folder)]; folderDocs != nil {
			return folderDocs.Entry
		}
	}
	return nil
}

// key is what the layers say at one position of a database's entries: vanilla's shape, then the docs'
// key, then the wiki's, each fact by the owner's order. A doc word list is closed only where vanilla
// writes no other word at it.
func (build *builder) key(database *Database, at []string, name string, docs, wiki *Key, shape *Shape) *Key {
	key := &Key{Name: name, Source: SourceDocs}
	switch {
	case shape != nil:
		key.Source, key.Written = SourceVanilla, shape.written()
	case docs == nil && wiki != nil:
		key.Source, key.Page, key.Revision = SourceWiki, wiki.Page, wiki.Revision
	}
	for _, layer := range []*Key{docs, wiki} {
		if layer != nil {
			layer.fill(key)
		}
	}
	for i, take := range key.Takes {
		if shape != nil && take.Type == WordEnum && (shape.Many || slices.ContainsFunc(slices.Collect(maps.Keys(shape.Values)), func(word string) bool { return !slices.Contains(take.Enum, word) })) {
			key.Takes[i].Guessed = true
		}
	}

	// the keys below: a doc's placeholder or a Nested row makes a position of names
	children, declared, placeholder := map[string]bool{}, map[string]bool{}, ""
	for _, layer := range []*Key{docs, wiki} {
		for child := range layer.keys() {
			if strings.HasPrefix(child, "<") && !slices.Contains([]string{"<number>", "<date>", "<prefixed>", "<name>"}, child) {
				placeholder = child
			} else {
				children[child], declared[child] = true, true
			}
		}
	}
	for child := range shape.keys() {
		children[child] = true
	}
	if placeholder != "" || shape != nil && shape.Names != nil {
		build.nest(database, at, key, placeholder, declared, docs, wiki, shape, children)
	}
	for _, child := range slices.Sorted(maps.Keys(children)) {
		if key.Keys == nil {
			key.Keys = map[string]*Key{}
		}
		key.Keys[child] = build.key(database, append(at[:len(at):len(at)], strings.ToLower(child)), child, docs.below(child), wiki.below(child), shape.below(child))
	}
	return key
}

// nest reads a position whose keys may be names. Vanilla confirms it by writing a key there the docs
// do not declare, a number or a date never; the keys then name the one other database that has them
// all as entries, else define a nested database's, named <database>.<word> by the row's word, the
// position's own key or the placeholder's word (setting_name gives setting); self is the database's
// own entries. What their blocks hold is the position <name>, or the nested database's entry.
func (build *builder) nest(database *Database, at []string, key *Key, placeholder string, declared map[string]bool, docs, wiki *Key, shape *Shape, children map[string]bool) {
	delete(children, placeholder)
	names := shape.keys()
	if shape != nil && shape.Names != nil {
		names = shape.Names
	}
	var undeclared []string
	content := &Shape{}
	for name := range names {
		if position := positionOf(name); !declared[name] && position != "<number>" && position != "<date>" && name != "<number>" && name != "<date>" && name != "<name>" {
			undeclared = append(undeclared, name)
			if shape.Names == nil {
				content.merge(shape.Keys[name])
				delete(children, name)
			}
		}
	}
	if shape != nil && shape.Names != nil {
		content = shape.Keys["<name>"]
		delete(children, "<name>")
	}
	if len(undeclared) == 0 {
		// a placeholder over only numbers or dates is that position (infection_levels = { 10 = { } })
		if placeholder != "" {
			position := "<name>"
			for _, written := range []string{"<number>", "<date>"} {
				if children[written] {
					position = written
				}
			}
			delete(children, position)
			key.child(position)
			key.Keys[position] = build.key(database, append(at[:len(at):len(at)], "*"), position, docs.below(placeholder), wiki.below(placeholder), shape.below(position))
		}
		return
	}

	slices.Sort(undeclared)
	row := build.row(database, at)
	if owner := build.owner(database, undeclared); row.Database == "" && owner != "" {
		key.Names = owner
		build.named = append(build.named, struct {
			key      *Key
			database *Database
			names    []string
		}{key, database, undeclared})
		key.child("<name>")
		key.Keys["<name>"] = build.key(database, append(at[:len(at):len(at)], "*"), "<name>", docs.below(placeholder), wiki.below(placeholder), content)
		return
	}
	own := key.Name
	if strings.HasPrefix(own, "<") {
		own = ""
	}
	word := strings.ReplaceAll(strings.ToLower(strings.Trim(placeholder, "<>")), " ", "_")
	word = cmp.Or(row.Database, own, placeholderSuffix.ReplaceAllString(word, ""))
	root := database
	for root.Parent != "" && build.schema.Databases[root.Parent] != nil {
		root = build.schema.Databases[root.Parent]
	}
	if word == "self" {
		key.Defines = root.Name
		return
	}
	nested := root.Name + "." + word
	key.Defines = nested
	for _, name := range undeclared {
		delete(build.owners[name], root.Name)
		build.own(name, nested)
	}
	if build.schema.Databases[nested] == nil {
		made := &Database{Name: nested, Format: database.Format, Parent: database.Name, Naming: Naming{Form: "key"}}
		build.schema.Databases[nested], build.added[nested] = made, true
		made.Entry = build.key(made, append(at[:len(at):len(at)], "*"), "", docs.below(placeholder), wiki.below(placeholder), content)
	}
}

// placeholderSuffix is what a placeholder's word ends in that no database is named by.
var placeholderSuffix = regexp.MustCompile(`_(?:name|key|\d+)$`)

// row is the Nested row that names a position of a database's entries, by its folders or its root's:
// a row at or above one of them, or on a file inside one (map_data/definitions.txt in map_data).
func (build *builder) row(database *Database, at []string) game.Nested {
	for database.Parent != "" && build.schema.Databases[database.Parent] != nil {
		database = build.schema.Databases[database.Parent]
	}
	for _, row := range build.input.Game.Nested {
		parent := strings.ToLower(row.Parent)
		under := slices.ContainsFunc(database.Folders, func(folder string) bool {
			folder = strings.ToLower(folder)
			return folder == parent || strings.HasPrefix(folder, parent+"/") || strings.HasPrefix(parent, folder+"/")
		})
		if under && pathMatches(row.Path, at) {
			return row
		}
	}
	return game.Nested{}
}

// owner is the one database, other than the one being built and its own, whose entries include
// every name; "" for none or several. A History database with an Of defines none.
func (build *builder) owner(database *Database, names []string) string {
	for database.Parent != "" && build.schema.Databases[database.Parent] != nil {
		database = build.schema.Databases[database.Parent]
	}
	counts := map[string]int{}
	for _, name := range names {
		for owner := range build.owners[name] {
			counts[owner]++
		}
	}
	found := ""
	for owner, count := range counts {
		if count == len(names) && owner != database.Name {
			if found != "" {
				return ""
			}
			found = owner
		}
	}
	return found
}

// keys is the names of a key's positions; none for no key.
func (key *Key) keys() map[string]bool {
	out := map[string]bool{}
	if key != nil {
		for name := range key.Keys {
			out[name] = true
		}
	}
	return out
}

// below is the key of that name under a key; nil for no key.
func (key *Key) below(name string) *Key {
	if key == nil {
		return nil
	}
	return key.Keys[name]
}

// fill gives a key the facts a layer states that it lacks: prose, default, need, written form,
// block, scope, saved scopes and takes.
func (layer *Key) fill(key *Key) {
	key.Doc, key.Default, key.Scope = cmp.Or(key.Doc, layer.Doc), cmp.Or(key.Default, layer.Default), cmp.Or(key.Scope, layer.Scope)
	if key.Required == RequiredUnstated {
		key.Required, key.When = layer.Required, layer.When
	}
	key.Written, key.Block = cmp.Or(key.Written, layer.Written), cmp.Or(key.Block, layer.Block)
	if key.Saved == nil {
		key.Saved = layer.Saved
	}
	for _, take := range layer.Takes {
		take.Enum = slices.Clone(take.Enum)
		mergeTake(key, take)
	}
}

// join adds what another place writes at the same key: its facts where this has none, and its keys.
func (key *Key) join(other *Key) {
	other.fill(key)
	for name, below := range other.Keys {
		if held := key.Keys[name]; held != nil {
			held.join(below)
			continue
		}
		if key.Keys == nil {
			key.Keys = map[string]*Key{}
		}
		key.Keys[name] = below
	}
}

// clone is a key and every key below it, copied.
func (key *Key) clone() *Key {
	copied := *key
	copied.Takes, copied.Keys = slices.Clone(key.Takes), nil
	for name, below := range key.Keys {
		copied.child(name)
		copied.Keys[name] = below.clone()
	}
	return &copied
}

// written is the form most often written at a position.
func (shape *Shape) written() Written {
	switch {
	case shape.Blocks > 0 && shape.Blocks >= shape.Lists && shape.Blocks >= shape.Words:
		return WrittenBlock
	case shape.Lists > 0 && shape.Lists >= shape.Words:
		return WrittenList
	case shape.Words > 0:
		return WrittenWord
	}
	return WrittenNone
}

// keys is the names of a shape's positions; none for no shape.
func (shape *Shape) keys() map[string]bool {
	out := map[string]bool{}
	if shape != nil {
		for name := range shape.Keys {
			out[name] = true
		}
	}
	return out
}

// below is the position of that name under a shape; nil for no shape.
func (shape *Shape) below(name string) *Shape {
	if shape == nil {
		return nil
	}
	return shape.Keys[name]
}

// each calls visit on every key of the databases the build added, and on an install of Arguments,
// each key before the keys below it.
func (build *builder) each(visit func(key *Key)) {
	var walk func(key *Key)
	walk = func(key *Key) {
		visit(key)
		for _, name := range slices.Sorted(maps.Keys(key.Keys)) {
			if below := key.Keys[name]; below != nil {
				walk(below)
			}
		}
	}
	for _, name := range slices.Sorted(maps.Keys(build.added)) {
		if entry := build.schema.Databases[name].Entry; entry != nil {
			walk(entry)
		}
	}
	if build.input.Base == nil {
		for _, name := range slices.Sorted(maps.Keys(build.schema.Arguments)) {
			walk(build.schema.Arguments[name])
		}
	}
}

// blocks gives each block no doc or dump describes the block type its keys share, and the one scope
// every command in it may run in. Then in every trigger, effect or modifier block a command's key
// moves to Arguments (on an install; a workspace drops it), a modifier's and a scripted entry's key
// is dropped, and an event target's block, a prefixed name's or a parameter's merges into the block
// itself; what the docs or dumps declare stays. Arguments starts from a copy of every command's
// usage, and moves repeat until nothing moves.
func (build *builder) blocks() {
	schema, dumps := build.schema, build.schema.Dumps
	if build.input.Base == nil {
		for _, commands := range []map[string]Command{dumps.Effects, dumps.Triggers} {
			for name, command := range commands {
				if command.Block != nil && schema.Arguments[name] == nil {
					schema.Arguments[name] = command.Block.clone()
				}
			}
		}
	}
	build.calledIn = map[string][]Block{}
	for _, database := range schema.Databases {
		for name := range build.input.Scripts.Names[database.Name] {
			if len(database.CalledIn) > 0 {
				build.calledIn[strings.ToLower(name)] = database.CalledIn
			}
		}
	}
	build.each(func(key *Key) {
		if key.Written == WrittenBlock && key.Block == BlockNone && key.Source == SourceVanilla && key.Name != "" {
			block, scope := build.blockOf(key)
			key.Block, key.Scope = block, cmp.Or(key.Scope, scope)
		}
	})
	build.lift()
}

// lift moves what a trigger, effect or modifier block holds out of it until nothing moves; it runs
// again after the hoist, which may merge an untyped block into a typed one.
func (build *builder) lift() {
	schema, dumps := build.schema, build.schema.Dumps
	for moved := true; moved; {
		moved = false
		build.each(func(key *Key) {
			if key.Block == BlockNone {
				return
			}
			for _, name := range slices.Sorted(maps.Keys(key.Keys)) {
				child, lower := key.Keys[name], strings.ToLower(name)
				target, prefixed := dumps.targets(name)
				_, modifier := dumps.Modifier(lower)
				_, effect := dumps.command(dumps.Effects, lower)
				_, trigger := dumps.command(dumps.Triggers, lower)
				switch {
				case child.Source != SourceVanilla:
					continue
				case dumps.declares(lower) && build.input.Base == nil:
					if schema.Arguments[lower] == nil {
						schema.Arguments[lower] = &Key{Name: lower}
					}
					schema.Arguments[lower].join(child)
				case effect, trigger, modifier, len(build.called(lower)) > 0:
				case target && child.Written == WrittenBlock, prefixed, len(jomini.Interpolations(name)) > 0:
					key.join(&Key{Keys: child.Keys})
				default:
					continue
				}
				delete(key.Keys, name)
				moved = true
			}
		})
	}
}

// called is the blocks a scripted entry is called in, by its name or with an iterator prefix removed
// (every_adult_child is the scripted list adult_child).
func (build *builder) called(name string) []Block {
	blocks := build.calledIn[name]
	for _, stem := range stems(name) {
		blocks = append(blocks, build.calledIn[stem]...)
	}
	return blocks
}

// blockOf is what a block's keys share when every one is a declared effect, trigger or modifier, an
// event target, a prefixed name, a $PARAMETER$ or a scripted entry's name, and the one scope every
// command there may run in; none where a key is anything else or they share no one block type.
func (build *builder) blockOf(key *Key) (Block, string) {
	dumps := build.schema.Dumps
	shared := map[Block]bool{BlockEffects: true, BlockTriggers: true, BlockModifiers: true}
	var scopes map[string]bool
	for name := range key.Keys {
		lower := strings.ToLower(name)
		effect, isEffect := dumps.command(dumps.Effects, lower)
		trigger, isTrigger := dumps.command(dumps.Triggers, lower)
		_, modifier := dumps.Modifier(lower)
		target, prefixed := dumps.targets(name)
		can := map[Block]bool{}
		switch {
		case isEffect || isTrigger:
			can[BlockEffects], can[BlockTriggers] = isEffect, isTrigger
			if in := slices.DeleteFunc(append(slices.Clone(effect.In), trigger.In...), func(scope string) bool { return scope == "none" }); len(in) > 0 {
				scopes = intersect(scopes, in)
			}
		case modifier:
			can[BlockModifiers] = true
		case len(build.called(lower)) > 0:
			for _, block := range build.called(lower) {
				can[block] = true
			}
		case target, prefixed, len(jomini.Interpolations(name)) > 0:
			continue
		default:
			return BlockNone, ""
		}
		maps.DeleteFunc(shared, func(block Block, _ bool) bool { return !can[block] })
	}
	if len(shared) != 1 {
		return BlockNone, ""
	}
	scope := ""
	if len(scopes) == 1 {
		scope = slices.Collect(maps.Keys(scopes))[0]
	}
	return slices.Collect(maps.Keys(shared))[0], scope
}

// intersect is the scopes both sets hold; the first set nil stands for every scope.
func intersect(scopes map[string]bool, in []string) map[string]bool {
	out := map[string]bool{}
	for _, scope := range in {
		if scopes == nil || scopes[scope] {
			out[scope] = true
		}
	}
	return out
}

// hoist merges a block key that can hold itself (it lies on a cycle of what holds what in one entry:
// a GUI vbox in an hbox in a vbox) from every place into one key at the entry's top, where
// Reader.Below looks last.
func (build *builder) hoist() {
	for _, name := range slices.Sorted(maps.Keys(build.added)) {
		entry := build.schema.Databases[name].Entry
		if entry == nil {
			continue
		}
		holds := map[string]map[string]bool{}
		var edges func(key *Key)
		edges = func(key *Key) {
			for child, below := range key.Keys {
				// a position of names (countries = { countries = { SWE = { } } }, <name>) holds no key of itself
				named := key.Names != "" || key.Defines != "" || below.Names != "" || below.Defines != ""
				if key != entry && !named && !strings.HasPrefix(child, "<") {
					if holds[key.Name] == nil {
						holds[key.Name] = map[string]bool{}
					}
					holds[key.Name][child] = true
				}
				edges(below)
			}
		}
		edges(entry)
		cyclic := cycles(holds)
		for len(cyclic) > 0 {
			var found []*Key
			var lift func(key *Key)
			lift = func(key *Key) {
				for child, below := range key.Keys {
					if cyclic[child] && key != entry {
						found = append(found, below)
						delete(key.Keys, child)
					} else {
						lift(below)
					}
				}
			}
			lift(entry)
			if len(found) == 0 {
				break
			}
			for _, below := range found {
				if top := entry.Keys[below.Name]; top != nil {
					top.join(below)
				} else {
					entry.Keys[below.Name] = below
				}
			}
		}
	}
}

// cycles is the key names on a cycle of what holds what: the strongly connected components of two or
// more names, or of one holding itself (Tarjan's).
func cycles(holds map[string]map[string]bool) map[string]bool {
	index, low, onStack, out := map[string]int{}, map[string]int{}, map[string]bool{}, map[string]bool{}
	var stack []string
	var visit func(name string)
	visit = func(name string) {
		index[name], low[name] = len(index), len(index)
		stack, onStack[name] = append(stack, name), true
		for next := range holds[name] {
			if _, seen := index[next]; !seen {
				visit(next)
				low[name] = min(low[name], low[next])
			} else if onStack[next] {
				low[name] = min(low[name], index[next])
			}
		}
		if low[name] != index[name] {
			return
		}
		var component []string
		for top := ""; top != name; {
			top, stack = stack[len(stack)-1], stack[:len(stack)-1]
			onStack[top], component = false, append(component, top)
		}
		if len(component) > 1 || holds[name][name] {
			for _, member := range component {
				out[member] = true
			}
		}
	}
	for _, name := range slices.Sorted(maps.Keys(holds)) {
		if _, seen := index[name]; !seen {
			visit(name)
		}
	}
	return out
}

// takes joins the takes that name a database by words to the database: a dump's class or tag, a
// doc's type words (a guess) or folder; one no database answers goes. The map table's keys take its
// database, and the sound keys the sound database, as a guess.
func (build *builder) takes() {
	schema, info := build.schema, build.input.Game
	sound := ""
	for _, name := range slices.Sorted(maps.Keys(schema.Databases)) {
		if strings.HasPrefix(name, "sound.") && sound != "sound.events" {
			sound = name
		}
	}
	named := map[string]string{} // each set of words once
	build.each(func(key *Key) {
		kept := key.Takes[:0]
		for _, take := range key.Takes {
			if take.Type == WordReference && take.Database != "" && schema.Databases[take.Database] == nil {
				words := take.Database
				if _, ok := named[words]; !ok {
					named[words] = schema.DatabaseNamed(words)
				}
				if take.Database = named[words]; take.Database == "" {
					continue
				}
				take.Guessed = take.Guessed || take.Source == SourceDocs || take.Source == SourceWiki
			}
			kept = append(kept, take)
		}
		key.Takes = kept
		lower := strings.ToLower(key.Name)
		if slices.Contains(info.MapTable.Keys, lower) {
			mergeTake(key, Take{Type: WordReference, Database: info.MapTable.Database, Source: SourceRegistry})
		}
		if sound != "" && slices.Contains(info.SoundKeys, lower) {
			mergeTake(key, Take{Type: WordReference, Database: sound, Source: SourceRegistry, Guessed: true})
		}
	})
}

// DatabaseNamed is the one database words name: a folder by the database that lists it; else X, the
// words or a class name without C and a trailing Database in snake case, a trailing key, tag, id or
// name off. Its candidates are the words, then the words without a trailing type or types, each with
// leading words dropped one at a time (never down to type alone), each as written, with s added and
// with a final y made ies; the first naming exactly one database wins.
func (schema *Schema) DatabaseNamed(words string) string {
	if strings.Contains(words, "/") {
		for _, name := range slices.Sorted(maps.Keys(schema.Databases)) {
			if slices.ContainsFunc(schema.Databases[name].Folders, func(folder string) bool { return strings.EqualFold(folder, words) }) {
				return name
			}
		}
		return ""
	}
	if class, ok := strings.CutPrefix(words, "C"); ok && (strings.HasSuffix(class, "Database") || strings.HasSuffix(class, "DataBase")) {
		var snake strings.Builder
		for i, char := range class[:len(class)-len("Database")] {
			if unicode.IsUpper(char) && i > 0 {
				snake.WriteByte('_')
			}
			snake.WriteRune(unicode.ToLower(char))
		}
		words = snake.String()
	}
	parts := strings.Fields(strings.ReplaceAll(strings.ToLower(words), "_", " "))
	if last := len(parts) - 1; last > 0 && slices.Contains([]string{"key", "tag", "id", "name"}, parts[last]) {
		parts = parts[:last]
	}
	bases := [][]string{parts}
	if last := len(parts) - 1; last > 0 && (parts[last] == "type" || parts[last] == "types") {
		bases = append(bases, parts[:last])
	}
	for _, base := range bases {
		for drop := range base {
			if candidate := base[drop:]; len(candidate) > 1 || candidate[0] != "type" {
				word := strings.Join(candidate, "_")
				forms := []string{word, word + "s"}
				if stem, ok := strings.CutSuffix(word, "y"); ok {
					forms = append(forms, stem+"ies")
				}
				for _, form := range forms {
					if name := schema.databaseCalled(form); name != "" {
						return name
					}
				}
			}
		}
	}
	return ""
}

// databaseCalled is the one database a word names by its whole name, the part after its last dot, or
// one segment of a clash-named one (court_positions/types, never a History one named by its folder's
// path); "" for none or several.
func (schema *Schema) databaseCalled(word string) string {
	found := ""
	for name, database := range schema.Databases {
		clash := strings.Contains(name, "/") && !slices.ContainsFunc(database.Folders, func(folder string) bool { return strings.EqualFold(folder, name) })
		if name == word || name[strings.LastIndexByte(name, '.')+1:] == word || clash && slices.Contains(strings.Split(name, "/"), word) {
			if found != "" {
				return ""
			}
			found = name
		}
	}
	return found
}

// unread is what the build saw and nothing explains: parse errors, extensions with no format, top
// folders of script no database holds, entry modes the registry gives no meaning, and dump labels;
// the doc and wiki lines are the scan's.
func (build *builder) unread() {
	schema, input := build.schema, build.input
	extensions, folders, modes := map[string]int{}, map[string]int{}, map[string]int{}
	for _, file := range input.Files {
		switch first, _, nested := strings.Cut(strings.ToLower(file.Rel), "/"); {
		case file.Format == source.Unknown:
			extensions[strings.ToLower(path.Ext(file.Rel))]++
		case isScriptText(file) && nested && len(schema.DatabasesAt(file.Rel)) == 0:
			folders[first]++
		}
	}
	for mode, count := range input.Scripts.EntryModes {
		if !slices.ContainsFunc(schema.EntryModes, func(known game.EntryMode) bool { return known.Word == mode }) {
			modes[mode] += count
		}
	}
	schema.Unread = Unread{
		ParseErrors: input.Scripts.ParseErrors, Extensions: tallies(extensions), Folders: tallies(folders),
		EntryModes: tallies(modes), DumpLabels: schema.Dumps.Unknown,
	}
}

// Settle finishes a schema once the definitions walk is done: each database's loc keys and image
// files from the names it defines; each declared prefix no database holds joined to the database its
// output scope names, else to the one defining most of the names written after it, and a database's
// scope from the outputs of the prefixes joined to it; the prefixes nothing holds, unread; then the
// lookups. A workspace's schema settles only the databases its build added, and reports no prefix.
func Settle(schema *Schema, names map[string][]string, keys *loc.Keys, files []source.File, scripts *Scripts) {
	images := map[string][]string{}
	for _, file := range files {
		if file.Format == source.Image {
			rel := strings.ToLower(file.Rel)
			base := strings.TrimSuffix(path.Base(rel), path.Ext(rel))
			images[base] = append(images[base], path.Dir(file.Rel)+"/{id}"+path.Ext(rel))
		}
	}
	for name, database := range schema.Databases {
		ids := slices.Compact(slices.Sorted(slices.Values(names[name])))
		if len(ids) == 0 || schema.added != nil && !schema.added[name] {
			continue
		}
		var declared []string
		for _, folder := range database.Folders {
			if folderDocs := schema.Docs[strings.ToLower(folder)]; folderDocs != nil {
				declared = append(declared, folderDocs.LocKeys...)
			}
		}
		database.LocKeys = loc.Conventions(ids, slices.Sorted(maps.Keys(database.Entry.keys())), keys, declared)

		// an image folder most of the entries have a file in
		held := map[string]int{}
		for _, id := range ids {
			for _, pattern := range images[strings.ToLower(id)] {
				held[pattern]++
			}
		}
		database.Files = nil
		for _, pattern := range slices.Sorted(maps.Keys(held)) {
			if held[pattern]*2 > len(ids) {
				database.Files = append(database.Files, FilePattern{Pattern: pattern, Coverage: float64(held[pattern]) / float64(len(ids))})
			}
		}
		slices.SortStableFunc(database.Files, func(first, second FilePattern) int { return cmp.Compare(second.Coverage, first.Coverage) })
	}

	// a key no layer gives a take guesses one from what vanilla writes there (theme = seduction)
	definers := map[string][]string{}
	for database, ids := range names {
		if _, made := ScriptMade[database]; !made && database != "constants" && database != "params" && !strings.HasPrefix(database, "sound.") {
			for _, id := range ids {
				definers[id] = append(definers[id], database)
			}
		}
	}
	for name, database := range schema.Databases {
		if schema.added == nil || schema.added[name] {
			guess(database.Entry, scripts.Entries[name], definers, keys)
		}
	}
	if schema.added == nil {
		schema.prefixes(names, scripts)
	}
	schema.BuildLookups()
}

// guess gives a key no layer gives a take, and each key below it, the take every word vanilla writes
// there shares: one database defining each, else each a loc key; none where one is a literal or no
// name. A guess is never accused on.
func guess(key *Key, shape *Shape, definers map[string][]string, keys *loc.Keys) {
	if key == nil || shape == nil {
		return
	}
	for name, below := range key.Keys {
		guess(below, shape.below(name), definers, keys)
	}
	if len(key.Takes) > 0 || len(shape.Values) == 0 {
		return
	}
	var shared map[string]bool
	localized := true
	for word := range shape.Values {
		if !jomini.IsDefName(word) || jomini.Literal(word) != jomini.NotLiteral || word == "yes" || word == "no" {
			return
		}
		localized = localized && keys.Has(word)
		named := map[string]bool{}
		for _, database := range definers[word] {
			if shared == nil || shared[database] {
				named[database] = true
			}
		}
		shared = named
	}
	switch {
	case len(shared) == 1:
		key.Takes = []Take{{Type: WordReference, Database: slices.Collect(maps.Keys(shared))[0], Source: SourceVanilla, Guessed: true}}
	case localized:
		key.Takes = []Take{{Type: WordLoc, Source: SourceVanilla, Guessed: true}}
	}
}

// prefixes joins each declared prefix no database holds, gives a database the scope every prefix
// joined to it leads to, and reports the undeclared prefixes no database holds.
func (schema *Schema) prefixes(names map[string][]string, scripts *Scripts) {
	held, definedBy := map[string]bool{}, map[string][]string{}
	for name, database := range schema.Databases {
		for _, prefix := range database.Prefixes {
			held[prefix] = true
		}
		for _, id := range names[name] {
			definedBy[id] = append(definedBy[id], name)
		}
	}
	outs := map[string]map[string]bool{}
	for _, prefix := range slices.Sorted(maps.Keys(schema.Dumps.DataTargets)) {
		target := schema.Dumps.DataTargets[prefix]
		if held[prefix] {
			continue
		}
		name := ""
		if len(target.Out) == 1 {
			name = schema.DatabaseNamed(target.Out[0])
		}
		if name == "" {
			// the database defining most of the names written after the prefix
			counts := map[string]int{}
			for id := range scripts.Prefixed[prefix] {
				for _, database := range definedBy[id] {
					counts[database]++
				}
			}
			best := 0
			for _, database := range slices.Sorted(maps.Keys(counts)) {
				if counts[database] > best {
					name, best = database, counts[database]
				} else if counts[database] == best {
					name = ""
				}
			}
		}
		if database := schema.Databases[name]; database != nil {
			database.Prefixes, held[prefix] = append(database.Prefixes, prefix), true
			if outs[name] == nil {
				outs[name] = map[string]bool{}
			}
			outs[name][strings.Join(target.Out, ",")] = true
		}
	}
	for name, out := range outs {
		if scope := slices.Collect(maps.Keys(out)); len(scope) == 1 && schema.Dumps.IsScope(scope[0]) && schema.Databases[name].Scope == "" {
			schema.Databases[name].Scope = scope[0]
		}
	}
	unheld := map[string]int{}
	for prefix, written := range scripts.Prefixed {
		if _, declared := schema.Dumps.DataTargets[prefix]; !declared && !held[prefix] {
			unheld[prefix] = len(written)
		}
	}
	schema.Unread.Prefixes = tallies(unheld)
}
