// Package schema models the language of one game version: which databases exist, what each key takes and which
// commands run in which scope. It is built once per install from the dumps, the docs and a count of the game's own
// files, then cached. It also owns the one walk over a file; every reader of script is a visitor on it.
package schema

import (
	"maps"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/jomini"
)

// A Claim is one fact about a field as its best source states it. Source is game.SourceNone when no source states
// it.
type Claim[T any] struct {
	Value  T
	Source game.Source
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
	ValueChoice    ValueType = "choice"    // one of Expected.Choices
	ValueColor     ValueType = "color"
	ValueBlock     ValueType = "block"
	ValueCall      ValueType = "call" // a data call
)

// Expected says what a field's value must be: its type, the database a reference names, or the choices allowed.
type Expected struct {
	Type     ValueType `json:"type"`
	Database string    `json:"database,omitempty"`
	Choices  []string  `json:"choices,omitempty"`
}

// A Requirement says whether a field must be written. Level is optional, always or conditional, and Condition
// holds the docs' wording for a conditional one.
type Requirement struct {
	Level     string `json:"level"` // optional, always, conditional
	Condition string `json:"condition,omitempty"`
}

// Vanilla is what the game's own files write at a field: how many times, how many of those as a block or as a
// color, each value with its count, and each shape its blocks were written in with its count.
type Vanilla struct {
	Count  int
	Blocks int
	Colors int
	Values map[string]int
	Shapes map[string]int
}

// Claims holds every fact the sources state about one field.
type Claims struct {
	Value    Claim[Expected]
	Shape    Claim[string] // a game.Shapes key
	Scope    Claim[string]
	Required Claim[Requirement]
	Default  Claim[string]
	Blurb    Claim[string]
}

// A Field is one key that may be written in a definition, with what it takes and the fields that may be written
// inside it. A database's root field is the whole definition block.
type Field struct {
	Children map[string]*Field // by written key, or by a form: <number>, <any>
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
	Blurb     string
	Global    bool // a link that reads the same from any scope
	Wild      bool // a link that is a pattern, so it is never matched by a written name
	Lists     bool // it iterates a list and has no target, so the scope inside its block is unknown
}

// A Database is one database of the game: the field tree of its definitions and the game's rows that govern it.
type Database struct {
	Name       string
	Folder     string // the folder or file its definitions are written in; "" for a nested database
	Definition *Field
	Row        game.Database   // where its definitions are written and how each is named
	Callable   game.Callable   // the words that call its definitions; zero when nothing calls them
	Precedence game.Precedence // how several definitions of one name combine
	Documented map[string]bool // every key the docs mention for its definitions
}

// A ScopeType is one kind of scope the game runs script in, and what script can do there.
type ScopeType struct {
	Name      string
	Variables bool
	Triggers  bool
	Effects   bool
}

// A DataFunction is one function of a data type, as the data types dump declares it.
type DataFunction struct {
	Name      string
	Returns   string
	Arguments []string
	Blurb     string
	Expansion string // a data binding macro's text, its parameters as Arguments names them; "" for a function the game has itself
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
	Name  string
	Scope string
	Blurb string
}

// A CustomLoc is one custom localization the dumps declare, with its scope.
type CustomLoc struct {
	Name  string
	Scope string
}

// A ScriptedDefinition is a scripted effect, trigger, value or list the game defines, with the parameters it
// takes.
type ScriptedDefinition struct {
	Database   string
	Parameters *Field // nil when the database's definitions take no parameters
}

// A SourceState says where one source's data came from and how fresh it is.
type SourceState struct {
	Origin    string    `json:"origin"` // yours, install, shipped, none
	WrittenAt time.Time `json:"writtenAt"`
	Stale     bool      `json:"stale"` // older than the install
}

// A Schema is the language of one game version, as built from the dumps, the docs and the game's own files.
type Schema struct {
	Game                string
	Version             string
	Sources             map[game.Source]SourceState
	Databases           map[string]*Database
	Callees             map[string][]string // by caller: the databases a call through it resolves in
	Commands            map[string]*Command
	Scopes              map[string]ScopeType
	Saved               map[string]bool // scope names the game saves from code
	Modifiers           map[string]Modifier
	OnActions           map[string]OnAction
	DataTypes           map[string]DataType
	CustomLoc           map[string]CustomLoc
	Prefixes            map[string]string // by link prefix: the database its names are in; "" for none
	TypeDatabases       map[string]string // by the words of a data type: the database of its names; "" where several are
	ScriptedDefinitions map[string]ScriptedDefinition
	WidgetTypes         map[string]bool
	Lists               string   // the database of scripted lists, or "" if the game has none
	Functions           string   // the database of data function macros, or "" if the game has none
	Events              string   // the database of events, or "" if the game has none
	Concepts            string   // the database of game concepts, or "" if the game has none
	Effects             string   // the database of scripted effects, or "" if the game has none
	FireDatabases       []string // the databases that fire and are fired: those whose row houses a game.FireHouses word
}

// The origins of a source's data: the install, the shipped backup, or none.
const (
	originNone    = "none"
	originInstall = "install"
	originShipped = "shipped"
)

// Build makes the schema of a game version from an install. It reads the dumps, falls back to the shipped backup
// for any dump file that is missing, reads the docs, and then counts what the game's own files write, parsing each
// file with the given function. Last, a key the docs explain in one place lends its blurb to the same key elsewhere
// in its database.
func Build(game *game.Game, installPath string, vanilla []string, parse func(path string) (*jomini.Tree, error)) (*Schema, error) {
	schema := newSchema(game, installPath)
	folders := listFolders(game, vanilla)
	schema.buildDatabases(game, folders)
	if err := schema.buildDumps(game, installPath); err != nil {
		return nil, err
	}
	if err := schema.buildDocs(installPath, folders); err != nil {
		return nil, err
	}
	if err := schema.CountVanilla(vanilla, parse); err != nil {
		return nil, err
	}
	schema.buildDocBlurbs()
	return schema, nil
}

// newSchema returns a schema of the given game with the tables it builds itself empty; the dumps give the others.
// The prefixes script saves names under are known from the start.
func newSchema(row *game.Game, installPath string) *Schema {
	return &Schema{
		Game:      row.ID,
		Version:   game.LoadGameVersion(row.ID, installPath),
		Sources:   map[game.Source]SourceState{},
		Databases: map[string]*Database{},
		Callees:   map[string][]string{},
		Commands:  map[string]*Command{},
		Prefixes: map[string]string{
			game.SavedScopes: game.SavedScopes, game.Variables: game.Variables,
			game.LocalVariables: game.LocalVariables, game.GlobalVariables: game.GlobalVariables,
		},
		TypeDatabases:       map[string]string{},
		ScriptedDefinitions: map[string]ScriptedDefinition{},
		WidgetTypes:         map[string]bool{},
	}
}

// listFolders returns the folder of each database the game's files make: the folder each script file belongs to,
// and every folder or file a row names that the install has.
func listFolders(row *game.Game, vanilla []string) []string {
	folders := map[string]bool{}
	var rels []string
	for _, file := range vanilla {
		rel, ok := trimScriptRoot(file)
		if !ok {
			continue
		}
		_, rel = SplitStage(row, rel)
		rels = append(rels, rel)
		if files.Classify(rel) == files.FormatScript {
			folders[findFolder(row, rel)] = true
		}
	}
	for _, folder := range slices.Concat(slices.Collect(maps.Keys(game.Databases)), slices.Collect(maps.Keys(row.Databases))) {
		if slices.ContainsFunc(rels, func(rel string) bool { return rel == folder || strings.HasPrefix(rel, folder+"/") }) {
			folders[folder] = true
		}
	}
	delete(folders, ".")
	return slices.Sorted(maps.Keys(folders))
}

// trimScriptRoot returns the path of a file under one of an install's roots, relative to the root and with the
// folder of a DLC cut off. It returns false for a path under no root.
func trimScriptRoot(file string) (string, bool) {
	slashed := filepath.ToSlash(file)
	cut, width := -1, 0
	for _, root := range game.InstallRoots {
		marker := "/" + root + "/"
		if at := strings.Index(slashed, marker); at >= 0 && (cut < 0 || at < cut) {
			cut, width = at, len(marker)
		}
	}
	if cut < 0 {
		return "", false
	}
	rel := slashed[cut+width:]
	if after, ok := strings.CutPrefix(rel, game.DLCFolder+"/"); ok {
		_, rel, _ = strings.Cut(after, "/")
	}
	return rel, true
}

// SplitStage splits a path into the stage folder that opens it and the rest, for the games that keep their files
// under stages. A path under no stage, or a game with none, gives "" and the path as it is.
func SplitStage(row *game.Game, rel string) (stage, rest string) {
	if row == nil {
		return "", rel
	}
	stage, rest, ok := strings.Cut(rel, "/")
	if ok && slices.Contains(row.StageRoots, stage) {
		return stage, rest
	}
	return "", rel
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

// buildDatabases builds the databases of the game: one for each given folder, one for each nesting, and the ones
// GUI files declare. It names the database that houses the game's events, lists, data function macros or concepts
// where one alone does, fills the widget types the game draws, and lists the databases each caller calls.
func (schema *Schema) buildDatabases(row *game.Game, folders []string) {
	for _, folder := range folders {
		schema.buildDatabase(row, game.ToDatabase(folder), folder)
	}
	for _, nesting := range slices.Concat(row.Nestings, game.Nestings) {
		if nesting.Database != "" {
			schema.buildDatabase(row, game.ToDatabase(nesting.Parent)+"."+nesting.Database, "")
		}
	}
	for _, declaring := range game.GUI.Declares {
		schema.buildDatabase(row, game.GUI.DatabasePrefix+declaring, "")
	}
	schema.buildDatabase(row, game.GUI.WidgetDatabase, "")
	housed := map[string][]string{}
	for label, database := range schema.Databases {
		housed[database.Row.Houses] = append(housed[database.Row.Houses], label)
		if database.Callable.Shape == "effects" && (schema.Effects == "" || label < schema.Effects) {
			schema.Effects = label
		}
	}
	houses := map[string]*string{"events": &schema.Events, "lists": &schema.Lists, "functions": &schema.Functions, "concepts": &schema.Concepts}
	for purpose, house := range houses {
		if len(housed[purpose]) == 1 {
			*house = housed[purpose][0]
		}
	}
	for _, house := range game.FireHouses {
		schema.FireDatabases = append(schema.FireDatabases, housed[house]...)
	}
	slices.Sort(schema.FireDatabases)
	for _, widget := range slices.Concat(slices.Collect(maps.Keys(game.GUI.Types)), row.GUIUndeclared) {
		schema.WidgetTypes[widget] = true
	}
	schema.listCallees()
}

// buildDatabase builds a database with the game's rows for it, unless the schema has it already: one a folder holds,
// or, for no folder, one nested in another's. It records the database under the words a data type names it by,
// where no other database has them.
func (schema *Schema) buildDatabase(row *game.Game, label, folder string) {
	if schema.Databases[label] != nil {
		return
	}
	rule, _ := row.FindDatabase(folder)
	schema.Databases[label] = &Database{
		Name: label, Folder: folder, Definition: &Field{}, Row: rule, Callable: game.Callables[label],
		Precedence: row.LookupPrecedence(label, folder), Documented: map[string]bool{},
	}
	phrase := game.DescribeDefinition(label, false)
	if _, taken := schema.TypeDatabases[phrase]; taken {
		label = ""
	}
	schema.TypeDatabases[phrase] = label
}

// listCallees lists, for each word a call is made with, the databases whose definitions it can call. game.AnyCall
// lists them all.
func (schema *Schema) listCallees() {
	for label, callable := range game.Callables {
		if schema.Databases[label] == nil {
			continue
		}
		for _, caller := range callable.Callers {
			schema.Callees[caller] = append(schema.Callees[caller], label)
		}
		if len(callable.Callers) > 0 {
			schema.Callees[game.AnyCall] = append(schema.Callees[game.AnyCall], label)
		}
	}
	for _, labels := range schema.Callees {
		slices.Sort(labels)
	}
}

// FindDatabase returns the database that a file belongs to, going by its relative path and the game's folder
// rules. It returns false if no database claims the file.
func (schema *Schema) FindDatabase(rel string) (*Database, bool) {
	_, rel = SplitStage(game.LookupGame(schema.Game), rel)
	for folder := rel; folder != "."; folder = path.Dir(folder) {
		if database, ok := schema.Databases[game.ToDatabase(folder)]; ok && database.Folder == folder {
			return database, true
		}
	}
	return nil, false
}

// FindDatabaseByClass returns the database that a class name in the dumps refers to, such as CTraitDatabase. It
// returns false if there is none.
func (schema *Schema) FindDatabaseByClass(class string) (*Database, bool) {
	label, ok := schema.ResolveTypeDatabase(strings.TrimSuffix(strings.TrimPrefix(class, "C"), "Database"))
	if folder, listed := game.ClassDatabases[class]; listed {
		label, ok = game.ToDatabase(folder), true
	}
	database := schema.Databases[label]
	return database, ok && database != nil
}

// ResolveLink returns the scope that a link leads to when followed from a given scope. It returns false if the
// link is unknown or leads nowhere.
func (schema *Schema) ResolveLink(link, scope string) (string, bool) {
	command, ok := schema.Commands[link]
	if !ok || !slices.Contains(command.Declared, game.DeclaredLink) || command.Target == "" {
		return "", false
	}
	if scope != "" && len(command.Scopes) > 0 && !slices.Contains(command.Scopes, scope) {
		return "", false
	}
	return command.Target, true
}

// ResolveCallTypes returns the type that each step of a data call leads to, starting from the block's data
// context. A step no dump declares gets "", and the steps after it are left out.
func (schema *Schema) ResolveCallTypes(call jomini.Call, datacontext string) []string {
	var types []string
	current := datacontext
	for i, step := range call.Steps {
		function, ok := schema.DataTypes[current].Functions[step.Text]
		if !ok && i == 0 {
			function, ok = schema.DataTypes[game.GlobalType].Functions[step.Text]
		}
		if !ok {
			return append(types, "")
		}
		current = function.Returns
		types = append(types, current)
	}
	return types
}

// ResolveTypeDatabase returns the database that holds the names of a data type: the one named in the singular by the
// type's words. It returns false if none is, or several are.
func (schema *Schema) ResolveTypeDatabase(dataType string) (string, bool) {
	label := schema.TypeDatabases[toPhrase(dataType)]
	return label, label != ""
}

// toPhrase returns the words of a text written in capitals and underscores, in lower case and apart by spaces.
func toPhrase(text string) string {
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

// MatchValue reports whether a written value fits what a field takes.
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
		return schema.matchChoice(expected, unquoted)
	case ValueReference:
		return jomini.IsIdentifier(unquoted)
	case ValueColor:
		tree := jomini.Parse("color = " + text)
		_, _, ok := jomini.ColorParts(tree, 0)
		return len(tree.Statements) > 0 && ok
	case ValueText:
		return text != unquoted || jomini.IsIdentifier(text)
	case ValueBlock:
		return strings.Contains(text, "{")
	case ValueCall:
		_, ok := jomini.ParseCall(strings.TrimSuffix(strings.TrimPrefix(unquoted, "["), "]"), 0)
		return ok && strings.HasPrefix(unquoted, "[")
	}
	return true
}

// matchChoice reports whether a text is one of the choices a field allows, without regard to case where the
// choices' database matches names that way.
func (schema *Schema) matchChoice(expected Expected, text string) bool {
	database := schema.Databases[expected.Database]
	folds := database != nil && database.IsCaseless()
	return slices.ContainsFunc(expected.Choices, func(choice string) bool {
		return choice == text || folds && strings.EqualFold(choice, text)
	})
}

// IsCaseless reports whether the database's names match without regard to case.
func (database *Database) IsCaseless() bool {
	return database.Row.FoldsCase || database.Callable.FoldsCase
}

// resolve sets the claim to a value when its source outranks the one that states it now, or when none does. A lower
// source number is a higher rank.
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

// take gives a field each claim of the given claims that it does not state, or that outranks its own.
func (field *Field) take(other Claims) {
	field.Value.take(other.Value)
	field.Shape.take(other.Shape)
	field.Scope.take(other.Scope)
	field.Required.take(other.Required)
	field.Default.take(other.Default)
	field.Blurb.take(other.Blurb)
}

// walkField calls the function on each field under a field, with its key, in order of key, and goes on under it.
func walkField(field *Field, visit func(key string, child *Field)) {
	for _, key := range slices.Sorted(maps.Keys(field.Children)) {
		child := field.Children[key]
		visit(key, child)
		walkField(child, visit)
	}
}

// buildChild returns the child of a field at a key, building an empty one when there is none.
func (field *Field) buildChild(key string) *Field {
	if field.Children == nil {
		field.Children = map[string]*Field{}
	}
	if field.Children[key] == nil {
		field.Children[key] = &Field{}
	}
	return field.Children[key]
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
	if game.IsNumberKey(key) {
		if child := field.findChild(game.NumberKey); child != nil {
			return child
		}
	}
	return field.findChild(game.AnyKey)
}
