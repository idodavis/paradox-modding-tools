// Package schema models the language of one game version: which databases exist, what each key takes and which
// commands run in which scope. It builds the model from the dumps, the docs and the game's own files, and it owns the
// one walk over a file that every reader of script is a visitor on.
package schema

import (
	"cmp"
	"maps"
	"path"
	"slices"
	"strings"
	"sync/atomic"
	"time"
	"unicode"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/jomini"
)

// A Claim is one fact about a field as its best source states it.
type Claim[T any] struct {
	Value  T
	Source game.Source // game.SourceNone when no source states it
}

// IsStated reports whether any source states the claim.
func (claim Claim[T]) IsStated() bool { return claim.Source != game.SourceNone }

// A ValueType says what kind of value a field takes.
type ValueType string

// The types a field's value can have.
const (
	ValueUnknown   ValueType = "unknown"
	ValueBool      ValueType = "bool"
	ValueNumber    ValueType = "number"
	ValueDate      ValueType = "date"
	ValueText      ValueType = "text"
	ValueReference ValueType = "reference" // a name of Expected.Database
	ValueTarget    ValueType = "target"    // a link to Expected.Scope, or a name of Expected.Database where it is set
	ValueChoice    ValueType = "choice"    // one of Expected.Choices
	ValueColor     ValueType = "color"
	ValueBlock     ValueType = "block"
	ValueCall      ValueType = "call" // a data call
)

// Expected is what a field's value must be.
type Expected struct {
	Type     ValueType `json:"type"`
	Database string    `json:"database,omitempty"`
	Scope    string    `json:"scope,omitempty"` // the scope a target's link leads to
	Choices  []string  `json:"choices,omitempty"`
	Values   bool      `json:"values,omitempty"` // a number written as a script value too, as a docs' type term says
	Nested   bool      `json:"nested,omitempty"` // a name of a database nested in Database too, as a language's dialect
}

// Vanilla is what the game's own files write at a field.
type Vanilla struct {
	Count       int            // how many times the field is written
	Blocks      int            // how many of those writes are a block
	Colors      int            // how many of those blocks are a color
	Definitions int            // how many plain definitions of its database write it
	Values      map[string]int // each value written, with its count
	Shapes      map[string]int // each shape a key written directly in its blocks tells, with its count
	Proofs      map[string]int // each set of shapes a call in its blocks allows, joined by spaces, with its count
}

// Claims holds every fact the sources state about one field.
type Claims struct {
	Value    Claim[Expected]
	Shape    Claim[string] // a game.Shapes key
	Scope    Claim[string]
	Required Claim[string] // optional, always or conditional
	Default  Claim[string]
	Blurb    Claim[string]
	Keys     Claim[string] // the database whose definitions its block's keys name, as a culture's opinions name cultures
}

// A Field is one key that may be written in a definition, with what it takes and the fields that may be written
// inside it. A database's root field is the whole definition block.
type Field struct {
	Children map[string]*Field // by written key, or by a pattern such as <number> or <any>
	Claims
	Source  game.Source // the highest-ranked declared source that writes the field; SourceNone for none
	Vanilla Vanilla
}

// A Command is one effect, trigger or link the dumps declare.
type Command struct {
	Declared  []string // what declares it: game.DeclaredEffect, DeclaredTrigger, DeclaredLink, in dump order
	Scopes    []string // the scopes it runs in; empty for any
	Target    string   // the scope a link leads to; "" for none
	Arguments *Field   // the keys it takes, as fields
	// ArgumentKey is the first key of the first block form its usage writes, which the argument in parentheses after
	// its name stands for, as the court position of aptitude(court_jester_court_position) does. It is "" where its
	// usage writes none.
	ArgumentKey   string
	Blurb         string
	Usage         string    // the dump's usage lines, which show how it is written; "" if the dump gives none
	Examples      []Example // the first places the game's own files write it, by path and then line
	VanillaScopes []string  // the scopes the game's own files run it in, sorted
	Wild          bool      // a link that is a pattern, so it is never matched by a written name
	Bare          bool      // a link the dumps let script write as its name alone, not only as name:data
	Lists         bool      // it iterates a list and has no target, so the scope inside its block is unknown
	Patterned     bool      // some pattern lists it
}

// An Example is one place where the game's own files write a command or call a data function: the file's path and
// the line, counted from zero.
type Example struct {
	Path string
	Line int
}

// A CommandPattern is a family of commands that the dumps declare once for each definition of a database, as
// has_relation_* is for the scripted relations. The name of a definition stands in the place of the star.
type CommandPattern struct {
	Key      string   // the commands' name with a star in the place of a definition's name
	Database string   // the database whose definitions' names fill the star
	Commands []string // the commands the dumps declare by it, one for each definition, sorted
}

// A Database is one database of the game: the field tree of its definitions and the game's rows that govern it.
type Database struct {
	Name       string
	Nested     []string // the databases whose definitions are keys inside its own
	Definition *Field
	Row        game.Database   // where its definitions are written and how each is named
	Callable   game.Callable   // the words that call its definitions; zero when nothing calls them
	Precedence game.Precedence // how several definitions of one name combine
	Documented map[string]bool // every key the docs mention for its definitions
}

// A ScopeType is one kind of scope the game runs script in, and what script can do there.
type ScopeType struct {
	Name     string
	Triggers bool
	Effects  bool
}

// A DataFunction is one function of a data type, as the data types dump declares it.
type DataFunction struct {
	Name      string
	Returns   string
	Arguments []string
	Blurb     string
	// Expansion is the text of a data binding macro. It is "" for a function the game has itself.
	Expansion string
	Examples  []Example // the first places the game's own files call it, by path and then line
}

// A DataType is one type of the data binding language, with its functions.
type DataType struct {
	Name      string
	Functions map[string]DataFunction
}

// A Modifier is one modifier the dumps declare. It may be written as a key inside a modifier block.
type Modifier struct {
	Name  string
	Blurb string
}

// An OnAction is one on_action the dumps declare, with the scope it runs in.
type OnAction struct {
	Name      string
	Scope     string
	Hardcoded bool // the game calls it from its own code, as the dumps' "From Code: Yes" says
}

// A ScriptedDefinition is a scripted effect, trigger, value or list the game or a mod defines, with the parameters it
// takes.
type ScriptedDefinition struct {
	Database   string
	Parameters *Field            // nil when the database's definitions take no parameters
	Saves      map[string]string // by parameter: the link its body saves the argument's name under, such as scope
	relays     []relay           // the parameters its body passes on to calls of other scripted definitions
}

// A relay is one parameter that a scripted definition's body passes on as an argument of a call to another one.
type relay struct {
	Callee    string // the called definition's name as written
	Argument  string // the argument's key in the call
	Parameter string // the parameter of the body that the argument writes
}

// modScripted is what the files of the workspace's mods declare: each file's declarer, and the scripted
// definitions and the modifier types of all of them together.
type modScripted struct {
	byFile    map[string]*declarer          // by a file's key
	all       map[string]ScriptedDefinition // as ScriptedDefinitions holds them
	modifiers map[string]*Field             // as ModifierFields holds them
}

// A SourceState says where one source's data came from and how fresh it is.
type SourceState struct {
	Origin    string    `json:"origin"` // install, shipped, none
	WrittenAt time.Time `json:"writtenAt"`
	Stale     bool      `json:"stale"`   // a dump file read is older than the install
	Folders   []string  `json:"folders"` // the install's folders its files were read from; none for the shipped backup
}

// A Schema is the language of one game version, as built from the dumps, the docs and the game's own files.
type Schema struct {
	Game      string
	Version   string // the install's version; Build leaves it "", as only the install's record may know it
	Dumps     SourceState
	Databases map[string]*Database
	// Setups holds the folders its bookmarks name for its start setups, without their stage. A game with a single start
	// has none.
	Setups              []string
	Callees             map[string][]string // by caller: the databases a call through it resolves in
	Commands            map[string]*Command
	Scopes              map[string]ScopeType
	Saved               map[string]bool // scope names the game saves from code
	Modifiers           map[string]Modifier
	OnActions           map[string]OnAction
	DataTypes           map[string]DataType
	Prefixes            map[string]string // by link prefix: the database its names are in; "" for none
	TypeDatabases       map[string]string // by the words of a data type: the database of its names; "" where several are
	InlineDatabases     map[string]string // by a key that declares a definition inline in an events file: its database
	ModifierFields      map[string]*Field // by modifier type the game defines: what its name takes in a modifiers block
	ScriptedDefinitions map[string]ScriptedDefinition
	WidgetTypes         map[string]bool
	// UnlistedCalls are the steps of data calls that no dump declares for the type before them and the game's own
	// localization and GUI text write all the same, keyed Type.step.
	UnlistedCalls map[string]bool
	// CommandPatterns are the patterns the dumps name commands by, one for each database that makes one, sorted by key
	// and database.
	CommandPatterns []CommandPattern
	// TopFolders holds the first folder of each of the game's files, less the stage folder that opens its path, in
	// lower case: the folders under which the game reads a mod's files.
	TopFolders    map[string]bool
	Lists         string   // the database of scripted lists, or "" if the game has none
	Events        string   // the database of events, or "" if the game has none
	Messages      string   // the database of the messages an effect sends the player, or "" if the game has none
	Concepts      string   // the database of game concepts, or "" if the game has none
	ModifierTypes string   // the database of modifier types, or "" if the game has none
	Effects       string   // the database of scripted effects, or "" if the game has none
	Values        string   // the database of script values: the callable whose definitions hold a value; "" if none
	FireDatabases []string // the databases that are fired: those whose row holds a game.FireContents word
	// mods is the mods' scripted definitions, which a call finds after the game's. No cache keeps them, and a rescan
	// puts a new table in place.
	mods atomic.Pointer[modScripted]
}

// The origins of a source's data: the install, the shipped backup, or none.
const (
	originNone    = "none"
	originInstall = "install"
	originShipped = "shipped"
)

// Build returns the schema of a game version, from an install's dumps, docs and own files. It returns an error if a
// dump or a doc cannot be read, or no file can be parsed.
func Build(row *game.Game, installPath string, listing []files.File, parse func(path string) (*jomini.Tree, error)) (*Schema, error) {
	listing = slices.DeleteFunc(slices.Clone(listing), func(file files.File) bool {
		return strings.HasPrefix(file.Rel, game.DLCFolder+"/")
	})
	schema := &Schema{
		Game:      row.ID,
		Databases: map[string]*Database{},
		Callees:   map[string][]string{},
		Commands:  map[string]*Command{},
		Prefixes: map[string]string{
			game.SavedScopes: game.SavedScopes, game.Variables: game.Variables,
			game.LocalVariables: game.LocalVariables, game.GlobalVariables: game.GlobalVariables,
		},
		TypeDatabases:       map[string]string{},
		InlineDatabases:     map[string]string{},
		ModifierFields:      map[string]*Field{},
		ScriptedDefinitions: map[string]ScriptedDefinition{},
		WidgetTypes:         map[string]bool{},
		UnlistedCalls:       map[string]bool{},
		TopFolders:          map[string]bool{},
	}
	for _, file := range listing {
		_, rest := row.SplitStage(strings.ToLower(file.Rel))
		if folder, _, nested := strings.Cut(rest, "/"); nested {
			schema.TopFolders[folder] = true
		}
	}
	schema.Setups = listSetups(row, listing, parse)
	folders := listFolders(row, listing)
	// Dumps first, then docs, then the game's own files. Last, a blurb the docs give a key is shared with the same key
	// elsewhere in its database.
	schema.buildDatabases(row, folders)
	if err := schema.buildDumps(row, installPath); err != nil {
		return nil, err
	}
	if err := schema.buildDocs(listing, folders); err != nil {
		return nil, err
	}
	if err := schema.CountVanilla(listing, parse); err != nil {
		return nil, err
	}
	schema.buildDocBlurbs()
	return schema, nil
}

// listSetups returns the folders that the game's bookmarks name as the folders of their start setups, sorted and each
// once. It returns none for a game with no bookmark folder. A bookmark file that cannot be parsed names none.
func listSetups(row *game.Game, listing []files.File, parse func(path string) (*jomini.Tree, error)) []string {
	var setups []string
	for _, file := range listing {
		if _, rel := row.SplitStage(file.Rel); row.BookmarkFolder == "" || !strings.HasPrefix(rel, row.BookmarkFolder+"/") {
			continue
		}
		tree, err := parse(file.Path)
		if err != nil {
			continue
		}
		for bookmark := range tree.Children(0) {
			if setup, ok := tree.FindChildValue(bookmark, row.SetupKey); ok && setup != "" {
				setups = append(setups, setup)
			}
		}
	}
	slices.Sort(setups)
	return slices.Compact(setups)
}

// listFolders returns the folder of each database the game's files make: the folder each script file belongs to,
// and every folder or file a row names that the listing has.
func listFolders(row *game.Game, listing []files.File) []string {
	folders := map[string]bool{}
	present := map[string]bool{}
	for _, file := range listing {
		_, rel := row.SplitStage(file.Rel)
		for dir := rel; dir != "."; dir = path.Dir(dir) {
			present[dir] = true
		}
		if files.Classify(rel) == files.FormatScript {
			folders[findFolder(row, rel)] = true
		}
	}
	for _, folder := range row.ListDatabaseFolders() {
		if present[folder] {
			folders[folder] = true
		}
	}
	delete(folders, ".")
	return slices.Sorted(maps.Keys(folders))
}

// findFolder returns the folder of the database that a script file belongs to: the file itself if a row names it,
// else the nearest folder above it that a row names, else the file's own folder.
func findFolder(row *game.Game, rel string) string {
	if _, ok := row.FindDatabase(rel); ok {
		return rel
	}
	for dir := path.Dir(rel); dir != "."; dir = path.Dir(dir) {
		if _, ok := row.FindDatabase(dir); ok {
			return dir
		}
	}
	return path.Dir(rel)
}

// buildDatabases builds the game's databases, one for each folder and nesting, and those the GUI files declare.
func (schema *Schema) buildDatabases(row *game.Game, folders []string) {
	for _, folder := range folders {
		rule, _ := row.FindDatabase(folder)
		if slices.Contains(schema.Setups, folder) {
			rule, _ = row.FindDatabase(game.SetupFolder)
		}
		rule.Folder = folder
		schema.buildDatabase(row, game.ToDatabase(folder), rule)
	}
	for _, nesting := range slices.Concat(row.ListNestings(), row.ListSetupNestings(schema.Setups...)) {
		if nesting.Database == "" || nesting.Parent == game.SetupFolder {
			continue
		}
		label := game.ToDatabase(nesting.Parent) + "." + nesting.Database
		schema.buildDatabase(row, label, game.Database{Target: nesting.Target})
		if parent := schema.Databases[game.ToDatabase(nesting.Parent)]; parent != nil && !slices.Contains(parent.Nested, label) {
			parent.Nested = append(parent.Nested, label)
		}
	}
	for _, declaring := range game.GUI.Declares {
		schema.buildDatabase(row, game.GUI.DatabasePrefix+declaring, game.Database{})
	}
	schema.buildDatabase(row, game.GUI.WidgetDatabase, game.Database{})
	// a text names a format or an icon in any case
	schema.buildDatabase(row, game.GUI.TextFormats, game.Database{FoldsCase: true})
	schema.buildDatabase(row, game.GUI.TextIcons, game.Database{FoldsCase: true})
	housed := map[game.Contents][]string{}
	callables := map[string]*string{"effects": &schema.Effects, "value": &schema.Values}
	for label, database := range schema.Databases {
		housed[database.Row.Contents] = append(housed[database.Row.Contents], label)
		if best, ok := callables[database.Callable.Shape]; ok && (*best == "" || label < *best) {
			*best = label
		}
		for _, prefix := range database.Row.Prefixes {
			schema.Prefixes[prefix] = label
		}
	}
	houses := map[game.Contents]*string{
		game.ContentsEvents: &schema.Events, game.ContentsMessages: &schema.Messages, game.ContentsLists: &schema.Lists,
		game.ContentsConcepts: &schema.Concepts, game.ContentsModifiers: &schema.ModifierTypes,
	}
	for purpose, house := range houses {
		if len(housed[purpose]) == 1 {
			*house = housed[purpose][0]
		}
	}
	for _, house := range game.FireContents {
		schema.FireDatabases = append(schema.FireDatabases, housed[house]...)
	}
	slices.Sort(schema.FireDatabases)
	for _, widget := range slices.Concat(slices.Collect(maps.Keys(game.GUI.Types)), row.GUI.Undeclared) {
		schema.WidgetTypes[widget] = true
	}
	schema.listCallees()
}

// buildDatabase builds one database from its row. It does nothing if the schema has it already. A database of records
// named for another's definitions, such as the history of titles, gives no data type its words, since those words
// name the other's definitions.
func (schema *Schema) buildDatabase(row *game.Game, label string, rule game.Database) {
	if schema.Databases[label] != nil {
		return
	}
	schema.Databases[label] = &Database{
		Name: label, Definition: &Field{}, Row: rule, Callable: game.Callables[label],
		Precedence: row.LookupPrecedence(label, rule.Folder), Documented: map[string]bool{},
	}
	for _, key := range rule.DeclaringKeys {
		schema.InlineDatabases[key] = label
	}
	if rule.Target != "" {
		return
	}
	phrase := game.DescribeDefinition(label, false)
	if named, ok := schema.TypeDatabases[phrase]; ok && !isNamedExactly(label, phrase) {
		label = ""
		if isNamedExactly(named, phrase) {
			label = named
		}
	}
	schema.TypeDatabases[phrase] = label
}

// isNamedExactly reports whether a database's own name is the given words of a data type, with an underscore for each
// space: the database institution for the words "institution", where setup/1337.institution has them too.
func isNamedExactly(label, phrase string) bool {
	return label == strings.ReplaceAll(phrase, " ", "_")
}

// listCallees lists, for each word a call is made with, the databases whose definitions it can call.
func (schema *Schema) listCallees() {
	for label, callable := range game.Callables {
		if schema.Databases[label] == nil {
			continue
		}
		for _, caller := range callable.Callers {
			schema.Callees[caller] = append(schema.Callees[caller], label)
		}
	}
	for _, labels := range schema.Callees {
		slices.Sort(labels)
	}
}

// FindDatabase returns the database that a file belongs to, going by its relative path and the game's folder
// rules. It returns false if no database claims the file.
func (schema *Schema) FindDatabase(rel string) (*Database, bool) {
	_, rel = game.LookupGame(schema.Game).SplitStage(rel)
	for folder := rel; folder != "."; folder = path.Dir(folder) {
		if database, ok := schema.Databases[game.ToDatabase(folder)]; ok && database.Row.Folder == folder {
			return database, true
		}
	}
	return nil, false
}

// FindDatabaseByClass returns the database that a class in the dumps refers to, such as CTraitDatabase or
// CDecisionTypeDatabase. It returns false if there is no such database.
func (schema *Schema) FindDatabaseByClass(class string) (*Database, bool) {
	words := strings.TrimPrefix(class, "C")
	if lower := strings.ToLower(words); strings.HasSuffix(lower, "database") {
		words = words[:len(words)-len("database")]
	}
	label, ok := schema.ResolveTypeDatabase(words)
	if folder, listed := game.ClassDatabases[class]; listed {
		label, ok = game.ToDatabase(folder), true
	}
	database := schema.Databases[label]
	return database, ok && database != nil
}

// FindCommand returns the command a key names, as written or else in lower case. It returns false if the dumps declare
// none.
func (schema *Schema) FindCommand(key string) (*Command, bool) {
	command, ok := schema.Commands[key]
	if !ok {
		command, ok = schema.Commands[strings.ToLower(key)]
	}
	return command, ok
}

// findKeyCall returns the call that a key writes as a command's name with one argument in parentheses, as
// "squared_distance(root)" does, and the command the name is. The spans are in the file's offsets, counted from
// the offset where the key starts in it. It returns false for any other key, and for a name that is no command.
func (schema *Schema) findKeyCall(key string, start int) (jomini.Step, *Command, bool) {
	if !strings.HasSuffix(key, ")") {
		return jomini.Step{}, nil, false
	}
	call, ok := jomini.ParseCall(key, start)
	if !ok || len(call.Steps) != 1 || len(call.Steps[0].Arguments) != 1 || call.Span.End != start+len(key) {
		return jomini.Step{}, nil, false
	}
	command, ok := schema.FindCommand(call.Steps[0].Text)
	return call.Steps[0], command, ok
}

// findStatementCall returns the call that a statement's key writes as a command's name with one argument in
// parentheses, and the command the name is, as findKeyCall does. A key compared with a block, as
// "list_size(x)" < { value = … } is, writes none: the game reads that block as a value it works out.
func (schema *Schema) findStatementCall(tree *jomini.Tree, statement int32) (jomini.Step, *Command, bool) {
	current := tree.Statements[statement]
	if operator := tree.Text(current.Op); current.IsBlock() && operator != "=" && operator != "?=" {
		return jomini.Step{}, nil, false
	}
	span := tree.UnquotedSpan(current.Key)
	return schema.findKeyCall(tree.Text(span), span.Start)
}

// findCallee returns the command a statement's key calls: the command of a key that writes a call, as
// findStatementCall finds one, else the key itself.
func (schema *Schema) findCallee(tree *jomini.Tree, statement int32) string {
	if step, _, ok := schema.findStatementCall(tree, statement); ok {
		return step.Text
	}
	return tree.Unquoted(tree.Statements[statement].Key)
}

// findStar returns where the text that stands in the place of the pattern's star starts and ends in a key, which
// is in lower case. It returns false if the key does not fit the pattern or leaves nothing for the star, and where
// the text there holds a dot, as a chain of links does, since no definition's name holds one.
func (pattern CommandPattern) findStar(key string) (start, end int, fits bool) {
	prefix, suffix, _ := strings.Cut(pattern.Key, "*")
	start, end = len(prefix), len(key)-len(suffix)
	return start, end, end > start && !strings.Contains(key[start:end], ".") && strings.HasPrefix(key, prefix) &&
		strings.HasSuffix(key, suffix)
}

// findPatterns returns the command patterns that make a key. A command the dumps declare is made by the patterns
// that list it. Any other key is made by the patterns it fits that leave the least text in the place of their
// star, and by the first of them in the table's order where several fit equally; every database of that pattern
// makes it. It returns none for a key no pattern makes.
func (schema *Schema) findPatterns(key string) []CommandPattern {
	key = strings.ToLower(key)
	command := schema.Commands[key]
	if !strings.Contains(key, "_") || command != nil && !command.Patterned {
		return nil
	}
	var best []CommandPattern
	shortest := len(key)
	for _, pattern := range schema.CommandPatterns {
		start, end, fits := pattern.findStar(key)
		switch {
		case !fits:
		case command != nil:
			if _, listed := slices.BinarySearch(pattern.Commands, key); listed {
				best = append(best, pattern)
			}
		case end-start < shortest:
			best, shortest = []CommandPattern{pattern}, end-start
		case end-start == shortest && pattern.Key == best[0].Key:
			best = append(best, pattern)
		}
	}
	return best
}

// ResolveLink returns the scope that a link leads to when followed from a given scope. It returns false if the
// link is unknown, wild or leads nowhere.
func (schema *Schema) ResolveLink(link, scope string) (string, bool) {
	command, ok := schema.Commands[link]
	if !ok || command.Wild || !slices.Contains(command.Declared, game.DeclaredLink) || command.Target == "" {
		return "", false
	}
	if scope != "" && len(command.Scopes) > 0 && !slices.Contains(command.Scopes, scope) {
		return "", false
	}
	return command.Target, true
}

// isScope reports whether a name is a scope type that script runs in: the dumps declare triggers or effects for it.
// It returns false for the words the dumps use for no scope, such as none, which they declare like a scope type.
func (schema *Schema) isScope(scope string) bool {
	scopeType := schema.Scopes[scope]
	return (scopeType.Triggers || scopeType.Effects) && !slices.Contains(game.DumpPhrases.NoScope, scope)
}

// IsLink reports whether a key or a value starts with a scope link, ignoring case, as scope:target and prev do.
func (schema *Schema) IsLink(key string) bool {
	head, prefixed := splitLinkHead(key)
	command := schema.Commands[head]
	if prefixed {
		_, reserved := schema.Prefixes[head]
		return reserved || command != nil && command.Wild
	}
	return command != nil && !command.Wild && slices.Contains(command.Declared, game.DeclaredLink)
}

// isKeyLink reports whether a key starts with a scope link, as IsLink does. A link that requires data, as EU5's
// modifier:<key> does, is none where a key writes it alone.
func (schema *Schema) isKeyLink(key string) bool {
	head, prefixed := splitLinkHead(key)
	return schema.IsLink(key) && (prefixed || schema.Commands[head].Bare)
}

// splitLinkHead returns the first link of a chain, in lower case. For a link written with a prefix, as scope:target
// is, it returns the prefix alone, and true.
func splitLinkHead(key string) (string, bool) {
	head := strings.ToLower(jomini.SplitChain(key)[0])
	if prefix, _, ok := jomini.SplitPrefixed(head); ok {
		return prefix, true
	}
	return head, false
}

// ResolveCallTypes returns the type each step of a data call leads to. It stops at a step no dump declares, which
// gets "".
func (schema *Schema) ResolveCallTypes(call jomini.Call, datacontext string) []string {
	_, functions := schema.listCallOwners(call, datacontext)
	types := make([]string, len(functions))
	for i, function := range functions {
		types[i] = function.Returns
	}
	return types
}

// listCallOwners returns the data type that declares each step of a data call, and the function the step calls. It
// stops at a step no dump declares, which gets "" and no function. A first step that names a data type is owned by
// that type and leads to it, as a function returning it would, though it calls none.
func (schema *Schema) listCallOwners(call jomini.Call, datacontext string) ([]string, []DataFunction) {
	var owners []string
	var functions []DataFunction
	current := datacontext
	for i, step := range call.Steps {
		function, ok := schema.DataTypes[current].Functions[step.Text]
		if !ok && i == 0 {
			current = game.GlobalType
			function, ok = schema.DataTypes[current].Functions[step.Text]
			if _, named := schema.DataTypes[step.Text]; !ok && named {
				function, ok, current = DataFunction{Returns: step.Text}, true, step.Text
			}
		}
		if !ok {
			return append(owners, ""), append(functions, DataFunction{})
		}
		owners, functions = append(owners, current), append(functions, function)
		current = function.Returns
	}
	return owners, functions
}

// FindFunction returns the data function one step of a data call calls. It returns false if the dumps declare none.
func (schema *Schema) FindFunction(call jomini.Call, datacontext string, step int) (DataFunction, bool) {
	owners, _ := schema.listCallOwners(call, datacontext)
	if step < 0 || step >= len(owners) || owners[step] == "" {
		return DataFunction{}, false
	}
	function, ok := schema.DataTypes[owners[step]].Functions[call.Steps[step].Text]
	return function, ok
}

// FindUnlisted returns the first step of a data call that the dumps do not declare for the type before it, and that
// type: the data context's, or Global's, for the first step. It returns false if the dumps declare every step, or the
// type before the step is unknown.
func (schema *Schema) FindUnlisted(call jomini.Call, datacontext string) (int, string, bool) {
	owners, functions := schema.listCallOwners(call, datacontext)
	step := slices.Index(owners, "")
	if step < 0 {
		return 0, "", false
	}
	before := cmp.Or(datacontext, game.GlobalType)
	if step > 0 {
		before = functions[step-1].Returns
	}
	return step, before, before != ""
}

// ResolveMacro returns a data call with its data binding macros expanded. A macro that expands into itself gives an
// empty call.
func (schema *Schema) ResolveMacro(call jomini.Call, datacontext string) jomini.Call {
	expanded := map[string]bool{}
	for len(call.Steps) > 0 {
		first := call.Steps[0]
		function := schema.DataTypes[game.GlobalType].Functions[first.Text]
		_, own := schema.DataTypes[datacontext].Functions[first.Text]
		expansion, ok := jomini.ParseCall(function.Expansion, 0)
		if own || !ok {
			return call
		}
		if expanded[first.Text] {
			return jomini.Call{}
		}
		expanded[first.Text] = true
		given := map[string]jomini.Argument{}
		for i, parameter := range function.Arguments {
			if i < len(first.Arguments) {
				given[parameter] = first.Arguments[i]
			}
		}
		next := jomini.SubstituteCall(expansion, given)
		next.Steps = append(next.Steps, call.Steps[1:]...)
		next.Span, next.Suffix = call.Span, call.Suffix
		call = next
	}
	return call
}

// ResolveDatacontext returns the data context type of a GUI block, from the data call its datacontext key writes and
// the data context of the block that holds it. It returns "" if the dumps do not resolve the call.
func (schema *Schema) ResolveDatacontext(call jomini.Call, datacontext string) string {
	types := schema.ResolveCallTypes(schema.ResolveMacro(call, datacontext), datacontext)
	if len(types) == 0 {
		return ""
	}
	return types[len(types)-1]
}

// ListCallReferences returns the names a data call's quoted arguments refer to, with the database of each.
func (schema *Schema) ListCallReferences(call jomini.Call, datacontext string) []Reference {
	var references []Reference
	call = schema.ResolveMacro(call, datacontext)
	types := schema.ResolveCallTypes(call, datacontext)
	for i, step := range call.Steps {
		targets, listed := game.GUI.CallTargets[step.Text]
		typed := ""
		if i < len(types) && types[i] != "" {
			typed, _ = schema.ResolveTypeDatabase(types[i])
		}
		for position, argument := range step.Arguments {
			database := typed
			if listed {
				database = ""
				if position < len(targets) {
					database = targets[position]
				}
			}
			if argument.Call != nil {
				references = append(references, schema.ListCallReferences(*argument.Call, "")...)
			}
			reference := Reference{Database: database, Name: argument.Text, Span: argument.Span}
			// A nested definition's holder is the argument before it, as in GetDefine('NAI', 'X').
			if listed && position > 0 {
				reference.Name = schema.JoinNested(database, step.Arguments[position-1].Text, argument.Text)
			}
			// An argument at a position the row leaves empty is no name.
			if argument.Quoted && !argument.Span.IsEmpty() && (!listed || database != "") {
				references = append(references, reference)
			}
		}
	}
	return references
}

// JoinNested returns the name of a nested definition from its holder's name and its key, such as NAI|X. Without a
// separator or a holder it is the key.
func (schema *Schema) JoinNested(database, holder, key string) string {
	separator := schema.findSeparator(database)
	if separator == "" || holder == "" {
		return key
	}
	return holder + separator + key
}

// SplitNested returns the key of a definition's name in a nested database: the part after the holding database's
// separator, X for NAI|X. Any other name is its own key.
func (schema *Schema) SplitNested(database, nested string) string {
	separator := schema.findSeparator(database)
	if _, key, ok := strings.Cut(nested, separator); ok && separator != "" {
		return key
	}
	return nested
}

// findSeparator returns the separator that the database holding a nested database writes. It returns "" if the
// database is not nested, or its holder writes none.
func (schema *Schema) findSeparator(database string) string {
	dot := strings.LastIndex(database, ".")
	if dot < 0 {
		return ""
	}
	if holder := schema.Databases[database[:dot]]; holder != nil {
		return holder.Row.Separator
	}
	return ""
}

// ResolveTypeDatabase returns the database that holds a data type's names, such as governments for GovernmentType. It
// returns false if there is not exactly one.
func (schema *Schema) ResolveTypeDatabase(dataType string) (string, bool) {
	phrase := ToPhrase(dataType)
	label, ok := schema.TypeDatabases[phrase]
	if stem, isTyped := strings.CutSuffix(phrase, " type"); !ok && isTyped {
		label = schema.TypeDatabases[stem]
	}
	return label, label != ""
}

// ToPhrase returns a CamelCase or snake_case text as lower-case words separated by spaces.
func ToPhrase(text string) string {
	var words strings.Builder
	var previous rune
	for _, r := range text {
		if unicode.IsUpper(r) && (unicode.IsLower(previous) || unicode.IsDigit(previous)) {
			words.WriteByte(' ')
		}
		if r == '_' {
			r = ' '
		}
		words.WriteRune(unicode.ToLower(r))
		previous = r
	}
	return words.String()
}

// DescribeStep returns one step of a data call in words, with its quoted arguments after them. A getter's leading word
// is dropped from a step that reaches an object. A step whose function takes a name, such as Custom('GetDaughterSon'),
// reads as that name in words instead, without its getter's word: "daughter son". A function that takes two names
// reads as the last.
func DescribeStep(step jomini.Step, reaches bool) string {
	var given []string
	for _, argument := range step.Arguments {
		if argument.Quoted && argument.Text != "" {
			given = append(given, argument.Text)
		}
	}
	if _, named := game.GUI.CallTargets[step.Text]; named && len(given) > 0 {
		return ToPhrase(DropGetter(given[len(given)-1]))
	}
	name := step.Text
	if reaches {
		name = DropGetter(name)
	}
	return strings.Join(append([]string{ToPhrase(name)}, given...), " ")
}

// DropGetter returns a function's name without the getter's word it starts with, such as FirstName for GetFirstName.
// A name that is the word alone, or goes on in lower case, is returned as it is.
func DropGetter(name string) string {
	for _, getter := range game.GUI.Getters {
		if rest, ok := strings.CutPrefix(name, getter); ok && rest != "" && unicode.IsUpper([]rune(rest)[0]) {
			return rest
		}
	}
	return name
}

// MatchValue reports whether a written value fits what a field takes. A scalar never fits a color or a block. A
// choice fits in any case where its database matches names without regard to case. A target fits a link, or a name
// where it names a database.
func (schema *Schema) MatchValue(expected Expected, text string) bool {
	unquoted := strings.Trim(text, `"`)
	switch expected.Type {
	case ValueBool:
		return slices.Contains(game.Bools, text)
	case ValueNumber:
		return jomini.IsNumber(text)
	case ValueDate:
		_, ok := jomini.ParseDate(text)
		return ok
	case ValueChoice:
		database := schema.Databases[expected.Database]
		folds := database != nil && database.IsCaseless()
		return slices.ContainsFunc(expected.Choices, func(choice string) bool {
			return choice == unquoted || folds && strings.EqualFold(choice, unquoted)
		})
	case ValueReference:
		return jomini.IsIdentifier(unquoted)
	case ValueTarget:
		return schema.IsLink(unquoted) || expected.Database != "" && jomini.IsIdentifier(unquoted)
	case ValueColor, ValueBlock:
		return false
	case ValueText:
		return text != unquoted || jomini.IsIdentifier(text)
	case ValueCall:
		_, ok := jomini.ParseBracketed(unquoted, 0)
		return ok
	}
	return true
}

// IsCaseless reports whether the database's names match without regard to case.
func (database *Database) IsCaseless() bool {
	return database.Row.FoldsCase || database.Callable.FoldsCase
}

// resolve sets the claim to the given value and source if the claim states nothing yet or the source outranks the one
// it states. A lower source number is a higher rank.
func (claim *Claim[T]) resolve(value T, source game.Source) {
	if !claim.IsStated() || source < claim.Source {
		*claim = Claim[T]{Value: value, Source: source}
	}
}

// take states another claim's value where its source outranks this one's, or where this one states nothing.
func (claim *Claim[T]) take(other Claim[T]) {
	if other.IsStated() {
		claim.resolve(other.Value, other.Source)
	}
}

// take gives a field each claim of the given claims that it does not state, or that outranks its own. It leaves Keys
// alone, since only the game's own files state it.
func (field *Field) take(other Claims) {
	field.Value.take(other.Value)
	field.Shape.take(other.Shape)
	field.Scope.take(other.Scope)
	field.Required.take(other.Required)
	field.Default.take(other.Default)
	field.Blurb.take(other.Blurb)
}

// walkField calls the given function on each field under a field, in key order, and then on the fields under that
// one. The function also gets the field's key.
func walkField(field *Field, visit func(key string, child *Field)) {
	for _, key := range slices.Sorted(maps.Keys(field.Children)) {
		child := field.Children[key]
		visit(key, child)
		walkField(child, visit)
	}
}

// buildChild returns the child of a field at a key, building an empty one when there is none. A child with no source
// takes the given one.
func (field *Field) buildChild(key string, source game.Source) *Field {
	if field.Children == nil {
		field.Children = map[string]*Field{}
	}
	if field.Children[key] == nil {
		field.Children[key] = &Field{}
	}
	child := field.Children[key]
	child.Source = cmp.Or(child.Source, source)
	return child
}

// findChild returns the child of a field written at a key, or nil if it has none. A nil field has none.
func (field *Field) findChild(key string) *Field {
	if field == nil {
		return nil
	}
	return field.Children[key]
}

// findPatternChild returns the child of a field written at a key, else the one written for a number where the key
// is one, else the one written for any key. It returns nil if there is none.
func (field *Field) findPatternChild(key string) *Field {
	if child := field.findChild(key); child != nil {
		return child
	}
	if jomini.IsNumberKey(key) {
		if child := field.findChild(jomini.NumberKey); child != nil {
			return child
		}
	}
	return field.findChild(game.AnyKey)
}
