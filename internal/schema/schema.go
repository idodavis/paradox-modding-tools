// Package schema models the language of one game version, built from its dumps, docs and own files. It owns the one
// walk over a file that every reader of script is a visitor on.
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

// A Claim is one fact about a field, as its best source states it.
type Claim[T any] struct {
	Value  T
	Source game.Source // game.SourceNone when no source states it
}

// IsStated reports whether any source states the claim.
func (claim Claim[T]) IsStated() bool { return claim.Source != game.SourceNone }

// A ValueType is the type of value a field takes.
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
	Values   bool      `json:"values,omitempty"` // a number that may be a script value too
	Nested   bool      `json:"nested,omitempty"` // a name of a database nested in Database too
}

// Vanilla is what the game's own files write at a field.
type Vanilla struct {
	Count       int            // how many times the field is written
	Blocks      int            // how many of those writes are a block
	Colors      int            // how many of those blocks are a color
	Definitions int            // how many plain definitions of its database write it
	Values      map[string]int // the count of each value written
	Shapes      map[string]int // the count of each shape that a key written directly in its blocks tells
	Proofs      map[string]int // the count of each set of shapes that a call in its blocks allows, joined by spaces
}

// Claims holds every fact the sources state about one field.
type Claims struct {
	Value    Claim[Expected]
	Shape    Claim[string] // a game.Shapes key
	Required Claim[string] // optional, always or conditional
	Default  Claim[string]
	Blurb    Claim[string]
	Keys     Claim[string] // the database that its block's keys name
}

// A Field is one key that may be written in a definition, with what it takes and the fields that may be written
// inside it. A database's root field is the whole definition block.
type Field struct {
	Children map[string]*Field // by written key, or by a pattern such as <number> or <any>
	Claims
	Source  game.Source // the highest-ranked declared source that writes the field; SourceNone if none
	Vanilla Vanilla
}

// A Command is one effect, trigger or link the dumps declare.
type Command struct {
	Declared []string // what declares it: game.DeclaredEffect, DeclaredTrigger, DeclaredLink, in dump order
	Scopes   []string // the scopes the dumps say it runs in; empty if they name none
	Target   string   // the scope a link leads to; "" for none
	// Arguments are the keys it takes, as fields, by declaration: an effect and a trigger of one name each take their
	// own.
	Arguments map[string]*Field
	// Union is the keys of its effect and its trigger together, which it takes in a block that allows either. It is
	// nil unless the dumps declare it as both.
	Union *Field
	// ArgumentKey is the key that the argument in parentheses after its name stands for, as in
	// aptitude(court_jester_court_position); "" if none.
	ArgumentKey string
	Blurb       string
	Usage       string    // the dump's usage lines; "" if none
	Examples    []Example // the first places the game's own files write it, by path and then line
	Wild        bool      // a link that is a pattern, so it is never matched by a written name
	Bare        bool      // a link that script may write as its name alone, not only as name:data
	Lists       bool      // it iterates a list and has no target, so the scope inside its block is unknown
	Patterned   bool      // some pattern lists it
}

// ArgumentsOf returns the keys a command takes under the given declaration, or under its first one if the dumps do not
// declare it as the given one. It returns nil if it takes none there.
func (command *Command) ArgumentsOf(declaration string) *Field {
	return command.Arguments[command.resolveDeclaration(declaration)]
}

// resolveDeclaration returns the given declaration if the dumps declare the command as it or as nothing, else the first
// one they declare it as.
func (command *Command) resolveDeclaration(declaration string) string {
	if slices.Contains(command.Declared, declaration) || len(command.Declared) == 0 {
		return declaration
	}
	return command.Declared[0]
}

// An Example is one place where the game's own files write a command or call a data function. Its line counts from
// zero.
type Example struct {
	Path string
	Line int
}

// A CommandPattern is a family of commands that the dumps declare once for each definition of a database, as
// has_relation_* is for the scripted relations.
type CommandPattern struct {
	Key      string   // the commands' name with a star in the place of a definition's name
	Database string   // the database whose definitions' names fill the star
	Commands []string // the commands the dumps declare by it, sorted
}

// A Database is one database of the game: the field tree of its definitions and the game's rows that govern it.
type Database struct {
	Name       string
	Nested     []string // the databases whose definitions are keys inside its own
	Definition *Field
	Row        game.Database   // where its definitions are written and how each is named
	Callable   game.Callable   // what calls its definitions; zero if nothing does
	Precedence game.Precedence // how several definitions of one name combine
	Documented map[string]bool // every key the docs mention for its definitions
	Readme     string          // the game doc its blurb was read from, else the first that describes it all; "" if none
}

// A ScopeType is one type of scope the game runs script in, and what script can do there.
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
	Expansion string    // the text of a data binding macro; "" for a function the game has itself
	Promote   bool      // the dumps define it as a promote, which leads to an object of its type rather than a value
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
	Parameters *Field            // nil if the database's definitions take no parameters
	Saves      map[string]string // by parameter: the link its body saves the argument's name under, such as scope
	relays     []relay           // the parameters its body passes on to calls of other scripted definitions
}

// A relay is one parameter that a scripted definition's body passes on as an argument of a call to another one.
type relay struct {
	Callee    string // the called definition's name as written
	Argument  string // the argument's key in the call
	Parameter string // the parameter of the body that the argument writes
}

// modScripted is what the files of the workspace's mods declare, by file and all together.
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
	Version   string // the install's version; Build leaves it ""
	Dumps     SourceState
	Databases map[string]*Database
	// Setups holds the folders that the game's bookmarks name for their start setups, without the stage folder. A game
	// with a single start has none.
	Setups   []string
	Callees  map[string][]string // by caller: the databases a call through it resolves in
	Commands map[string]*Command
	Scopes   map[string]ScopeType
	Saved    map[string]bool // scope names the game saves from code
	// Customs are the customizable localizations the dumps name, though the game's files may define none.
	Customs             map[string]bool
	Modifiers           map[string]Modifier
	OnActions           map[string]OnAction
	DataTypes           map[string]DataType
	Prefixes            map[string]string // by link prefix: the database its names are in; "" for none
	TypeDatabases       map[string]string // by the phrase of a data type: the database of its names; "" if several
	InlineDatabases     map[string]string // by a key that declares a definition inline in an events file: its database
	ModifierFields      map[string]*Field // by modifier type the game defines: what its name takes in a modifiers block
	ScriptedDefinitions map[string]ScriptedDefinition
	WidgetTypes         map[string]bool
	// UnlistedCalls holds the data call steps that no dump declares for the type before them, though the game's own
	// localization and GUI text write them. A key is Type.step.
	UnlistedCalls map[string]bool
	// CommandPatterns holds the patterns the dumps name commands by, sorted by key and database.
	CommandPatterns []CommandPattern
	// TopFolders holds the first folder of each of the game's files in lower case, without the stage folder: the
	// folders under which the game reads a mod's files.
	TopFolders    map[string]bool
	Lists         string   // the database of scripted lists; "" if none
	Events        string   // the database of events; "" if none
	Messages      string   // the database of the messages an effect sends the player; "" if none
	Concepts      string   // the database of game concepts; "" if none
	ModifierTypes string   // the database of modifier types; "" if none
	Effects       string   // the database of scripted effects; "" if none
	Values        string   // the database of script values; "" if none
	FireDatabases []string // the databases that are fired: those whose row's contents are one of game.FireContents
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
	// the count reads the unions as every later walk does, so they are built before it and again after
	schema.buildDatabases(row, folders)
	if err := schema.buildDumps(row, installPath); err != nil {
		return nil, err
	}
	if err := schema.buildDocs(installPath, listing, folders); err != nil {
		return nil, err
	}
	schema.buildUnions()
	if err := schema.CountVanilla(listing, parse); err != nil {
		return nil, err
	}
	schema.buildUnions()
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
		label := nesting.Label()
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
	for label, database := range schema.Databases {
		housed[database.Row.Contents] = append(housed[database.Row.Contents], label)
		switch { // the effects and value callables are the first label in order with that shape
		case database.Callable.Shape == "effects" && (schema.Effects == "" || label < schema.Effects):
			schema.Effects = label
		case database.Callable.Shape == "value" && (schema.Values == "" || label < schema.Values):
			schema.Values = label
		}
		for _, prefix := range database.Row.Prefixes {
			schema.Prefixes[prefix] = label
		}
	}
	findOnly := func(contents game.Contents) string {
		if len(housed[contents]) == 1 {
			return housed[contents][0]
		}
		return ""
	}
	schema.Events, schema.Messages, schema.Lists = findOnly(game.ContentsEvents), findOnly(game.ContentsMessages),
		findOnly(game.ContentsLists)
	schema.Concepts, schema.ModifierTypes = findOnly(game.ContentsConcepts), findOnly(game.ContentsModifiers)
	for _, house := range game.FireContents {
		schema.FireDatabases = append(schema.FireDatabases, housed[house]...)
	}
	slices.Sort(schema.FireDatabases)
	for _, widget := range slices.Concat(slices.Collect(maps.Keys(game.GUI.Types)), row.GUI.Undeclared) {
		schema.WidgetTypes[widget] = true
	}
	schema.listCallees()
}

// buildDatabase adds one database built from its row. It does nothing if the schema has it already. A database with a
// target, such as the history of titles, names no data type, since its definitions are the target's.
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
	// of two databases with the same phrase, the one named exactly by it keeps it, else neither
	phrase := game.DescribeDefinition(label, false)
	owner := label
	if earlier, ok := schema.TypeDatabases[phrase]; ok && !isNamedExactly(label, phrase) {
		owner = ""
		if isNamedExactly(earlier, phrase) {
			owner = earlier
		}
	}
	schema.TypeDatabases[phrase] = owner
}

// isNamedExactly reports whether a database's name is the given phrase with underscores. The database institution is
// named exactly by "institution", and setup/1337.institution is not.
func isNamedExactly(label, phrase string) bool {
	return label == strings.ReplaceAll(phrase, " ", "_")
}

// listCallees fills Callees: for each caller, the databases whose definitions it can call.
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

// FindDatabase returns the database that a file belongs to, going by its relative path and the game's folder rules. It
// returns false if no database claims the file.
func (schema *Schema) FindDatabase(rel string) (*Database, bool) {
	_, rel = game.LookupGame(schema.Game).SplitStage(rel)
	for folder := rel; folder != "."; folder = path.Dir(folder) {
		if database, ok := schema.Databases[game.ToDatabase(folder)]; ok && database.Row.Folder == folder {
			return database, true
		}
	}
	return nil, false
}

// FindDatabaseByClass returns the database that a class in the dumps refers to, such as CTraitDatabase. It returns
// false if there is none.
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

// findKeyCall parses a key written as a command's name with one argument in parentheses, such as
// squared_distance(root), and returns the call's step and the command. The step's spans are offsets in the file, where
// the key starts at the given offset. It returns false for any other key, and when the name is no command.
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

// findStatementCall does what findKeyCall does for a statement's key. A key compared with a block, such as
// list_size(x) < { value = … }, writes no call: the game reads that block as a value to work out.
func (schema *Schema) findStatementCall(tree *jomini.Tree, statement int32) (jomini.Step, *Command, bool) {
	current := tree.Statements[statement]
	if operator := tree.Text(current.Op); current.IsBlock() && operator != "=" && operator != "?=" {
		return jomini.Step{}, nil, false
	}
	span := tree.UnquotedSpan(current.Key)
	return schema.findKeyCall(tree.Text(span), span.Start)
}

// findCallee returns the name a statement's key calls: the command's name for a key with an argument in parentheses,
// else the unquoted key.
func (schema *Schema) findCallee(tree *jomini.Tree, statement int32) string {
	if step, _, ok := schema.findStatementCall(tree, statement); ok {
		return step.Text
	}
	return tree.Unquoted(tree.Statements[statement].Key)
}

// findStar returns the start and end of the text in a lower-case key that fills the pattern's star. It returns false if
// the key does not fit the pattern, leaves nothing for the star, or has a dot there. A dot means a chain of links, and
// no definition's name holds one.
func (pattern CommandPattern) findStar(key string) (start, end int, fits bool) {
	prefix, suffix, _ := strings.Cut(pattern.Key, "*")
	start, end = len(prefix), len(key)-len(suffix)
	return start, end, end > start && !strings.Contains(key[start:end], ".") && strings.HasPrefix(key, prefix) &&
		strings.HasSuffix(key, suffix)
}

// findPatterns returns the command patterns that make a key, or none. A command the dumps declare is made by the
// patterns that list it, and any other key by the fitting pattern with the shortest star, the first on a tie, in
// every database that has it.
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
		case !fits: // the key does not fit this pattern
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

// ResolveLink returns the scope that a link leads to from the given scope. It returns false if the link is unknown,
// wild, leads nowhere or does not run in that scope.
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

// isScope reports whether a name is a scope type that script runs in, meaning the dumps declare triggers or effects for
// it. It is false for names the dumps use for no scope, such as none, though they declare them like one.
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

// isKeyLink reports whether a key starts with a scope link, as IsLink does, but a link that requires data, as EU5's
// modifier:<key> does, is none when written alone.
func (schema *Schema) isKeyLink(key string) bool {
	head, prefixed := splitLinkHead(key)
	return schema.IsLink(key) && (prefixed || schema.Commands[head].Bare)
}

// splitLinkHead returns the first link of a chain in lower case, or only the prefix of one such as scope:target, with
// true.
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

// listCallOwners returns the data type that declares each step of a data call, and the function each step calls. It
// stops at a step no dump declares, which gets "" and an empty function. A first step that names a data type is owned
// by that type and leads to it, as a function returning it would, though it calls no function.
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

// ReturnType returns the name of the type a data function returns. It returns "" if the dumps do not name the type.
func ReturnType(function DataFunction) string {
	if function.Returns == game.DumpPhrases.Unnamed {
		return ""
	}
	return function.Returns
}

// FindUnlisted returns the position of the first step of a data call that the dumps do not declare, and the type before
// it, which for the first step is the data context's type, else Global. It returns false if the dumps declare every
// step or the type before the step is unknown.
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

// ResolveMacro returns a data call with its leading macros expanded. The data context's own functions are never
// macros. A macro that expands into itself gives an empty call.
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
		bindings := map[string]jomini.Argument{}
		for i, parameter := range function.Arguments {
			if i < len(first.Arguments) {
				bindings[parameter] = first.Arguments[i]
			}
		}
		next := jomini.SubstituteCall(expansion, bindings)
		next.Steps = append(next.Steps, call.Steps[1:]...)
		next.Span, next.Suffix = call.Span, call.Suffix
		call = next
	}
	return call
}

// ResolveDatacontext returns the data context type of a GUI block, given the call its datacontext key writes and the
// data context around it. It returns "" if the dumps do not resolve the call.
func (schema *Schema) ResolveDatacontext(call jomini.Call, datacontext string) string {
	types := schema.ResolveCallTypes(schema.ResolveMacro(call, datacontext), datacontext)
	if len(types) == 0 {
		return ""
	}
	return types[len(types)-1]
}

// ListCallReferences returns a reference for each quoted argument of a data call, calls inside its arguments included.
// Its database is the one game.GUI.CallTargets lists for the argument's position, or none, else the saved scopes for a
// promote of game.TopScopeType that leads to a scope, else the step's type's.
func (schema *Schema) ListCallReferences(call jomini.Call, datacontext string) []Reference {
	var references []Reference
	call = schema.ResolveMacro(call, datacontext)
	owners, functions := schema.listCallOwners(call, datacontext)
	for i, step := range call.Steps {
		targets, listed := game.GUI.CallTargets[step.Text]
		typed := ""
		switch {
		case i >= len(functions) || functions[i].Returns == "":
		case owners[i] == game.TopScopeType && functions[i].Promote && schema.isScopeType(functions[i].Returns):
			typed = game.SavedScopes
		default:
			typed, _ = schema.ResolveTypeDatabase(functions[i].Returns)
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
			// a nested definition's holder is the argument before it, as in GetDefine('NAI', 'X')
			if listed && position > 0 {
				reference.Name = schema.JoinNested(database, step.Arguments[position-1].Text, argument.Text)
			}
			// a position the row leaves empty names nothing
			if argument.Quoted && !argument.Span.IsEmpty() && (!listed || database != "") {
				references = append(references, reference)
			}
		}
	}
	return references
}

// isScopeType reports whether a data type is the type of a scope the dumps declare, as Character is for the getter
// Scope.GetCharacter.
func (schema *Schema) isScopeType(dataType string) bool {
	for name, getter := range schema.DataTypes[game.AnyScopeType].Functions {
		if getter.Returns == dataType && schema.isScope(strings.ReplaceAll(ToPhrase(DropGetter(name)), " ", "_")) {
			return true
		}
	}
	return false
}

// JoinNested returns the name of a nested definition from its holder's name and its key, such as NAI|X. It is the key
// if there is no separator or holder.
func (schema *Schema) JoinNested(database, holder, key string) string {
	separator := schema.findSeparator(database)
	if separator == "" || holder == "" {
		return key
	}
	return holder + separator + key
}

// SplitNested returns the key of a definition's name in a nested database, X for NAI|X. Any other name is its own key.
func (schema *Schema) SplitNested(database, nested string) string {
	separator := schema.findSeparator(database)
	if _, key, ok := strings.Cut(nested, separator); ok && separator != "" {
		return key
	}
	return nested
}

// findSeparator returns the separator that the holder of a nested database writes, such as the | of NAI|X. It returns
// "" if there is none.
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
// returns false unless there is exactly one.
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

// DescribeStep returns one step of a data call in words, followed by its quoted arguments. The flag says whether to
// drop the getter from the step's name. A step that takes a name, as Custom('GetDaughterSon') does, reads as its last
// quoted argument without the getter: "daughter son".
func DescribeStep(step jomini.Step, reaches bool) string {
	var quoted []string
	for _, argument := range step.Arguments {
		if argument.Quoted && argument.Text != "" {
			quoted = append(quoted, argument.Text)
		}
	}
	if _, named := game.GUI.CallTargets[step.Text]; named && len(quoted) > 0 {
		return ToPhrase(DropGetter(quoted[len(quoted)-1]))
	}
	name := step.Text
	if reaches {
		name = DropGetter(name)
	}
	return strings.Join(append([]string{ToPhrase(name)}, quoted...), " ")
}

// DropGetter returns a function's name without the getter it starts with, such as FirstName for GetFirstName. A name
// that is only the getter, or where the getter is followed by a lower-case letter, is returned as it is.
func DropGetter(name string) string {
	for _, getter := range game.GUI.Getters {
		if rest, ok := strings.CutPrefix(name, getter); ok && rest != "" && unicode.IsUpper([]rune(rest)[0]) {
			return rest
		}
	}
	return name
}

// MatchValue reports whether a written value fits what a field takes. A scalar never fits a color or a block. A choice
// fits in any case if its database ignores case. A target fits a link, or a name if the field names a database.
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
	field.Required.take(other.Required)
	field.Default.take(other.Default)
	field.Blurb.take(other.Blurb)
}

// walkField calls the given function on each field below a field with its key, depth first in key order.
func walkField(field *Field, visit func(key string, child *Field)) {
	for _, key := range slices.Sorted(maps.Keys(field.Children)) {
		child := field.Children[key]
		visit(key, child)
		walkField(child, visit)
	}
}

// buildChild returns the field's child for a key, adding one if there is none. A child with no source takes the given
// source.
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

// findChild returns the field's child for a key, or nil if there is none. A nil field has none.
func (field *Field) findChild(key string) *Field {
	if field == nil {
		return nil
	}
	return field.Children[key]
}

// findPatternChild returns the field's child for a key, else its <number> child if the key is a number, else its <any>
// child. It returns nil if there is none.
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
