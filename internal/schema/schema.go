// Package schema models the language of one game version: which databases exist, what each key takes and which
// commands run in which scope. It builds the model from the dumps, the docs and the game's own files, and it owns the
// one walk over a file that every reader of script is a visitor on.
package schema

import (
	"fmt"
	"maps"
	"path"
	"slices"
	"strings"
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
	ValueChoice    ValueType = "choice"    // one of Expected.Choices
	ValueColor     ValueType = "color"
	ValueBlock     ValueType = "block"
	ValueCall      ValueType = "call" // a data call
)

// Expected is what a field's value must be.
type Expected struct {
	Type     ValueType `json:"type"`
	Database string    `json:"database,omitempty"`
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
	Declared      []string // what declares it: game.DeclaredEffect, DeclaredTrigger, DeclaredLink, in dump order
	Scopes        []string // the scopes it runs in; empty for any
	Target        string   // the scope a link leads to; "" for none
	Arguments     *Field   // the keys it takes, as fields
	Blurb         string
	Usage         string    // the dump's usage lines, which show how it is written; "" if the dump gives none
	Examples      []Example // the first places the game's own files write it, by path and then line
	VanillaScopes []string  // the scopes the game's own files run it in, sorted
	Wild          bool      // a link that is a pattern, so it is never matched by a written name
	Lists         bool      // it iterates a list and has no target, so the scope inside its block is unknown
}

// An Example is one place where the game's own files write a command or call a data function: the file's path and
// the line, counted from zero.
type Example struct {
	Path string
	Line int
}

// A Database is one database of the game: the field tree of its definitions and the game's rows that govern it.
type Database struct {
	Name       string
	Folder     string   // the folder or file its definitions are written in; "" for a nested database
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
	Origin    string    `json:"origin"` // install, shipped, none
	WrittenAt time.Time `json:"writtenAt"`
	Stale     bool      `json:"stale"`   // older than the install
	Folders   []string  `json:"folders"` // the install's folders its files were read from; none for the shipped backup
}

// A Schema is the language of one game version, as built from the dumps, the docs and the game's own files.
type Schema struct {
	Game                string
	Version             string // the install's version; Build leaves it "", as only the install's record may know it
	Dumps               SourceState
	Databases           map[string]*Database
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
	ScriptedDefinitions map[string]ScriptedDefinition
	WidgetTypes         map[string]bool
	Lists               string   // the database of scripted lists, or "" if the game has none
	Events              string   // the database of events, or "" if the game has none
	Concepts            string   // the database of game concepts, or "" if the game has none
	ModifierTypes       string   // the database of modifier types, or "" if the game has none
	Effects             string   // the database of scripted effects, or "" if the game has none
	Values              string   // the database of script values: the callable whose definitions hold a value; "" if none
	FireDatabases       []string // the databases that are fired: those whose row houses a game.FireHouses word
}

// The origins of a source's data: the install, the shipped backup, or none.
const (
	originNone    = "none"
	originInstall = "install"
	originShipped = "shipped"
)

// Build returns the schema of a game version. It reads the dumps and docs of the given install and counts what the
// game's own files write, parsing each file of the given listing with the given function. A dump file the install
// lacks comes from the shipped backup. Files under the DLC folder are left out. The schema's version is left "". It
// returns an error if a dump or a doc cannot be read, or if no file can be parsed.
func Build(row *game.Game, installPath string, listing []files.File, parse func(path string) (*jomini.Tree, error)) (*Schema, error) {
	listing = slices.DeleteFunc(slices.Clone(listing), func(file files.File) bool {
		return strings.HasPrefix(file.Rel, game.DLCFolder+"/")
	})
	schema := newSchema(row)
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

// newSchema returns an empty schema of the given game. Only the prefixes that script saves names under are filled.
func newSchema(row *game.Game) *Schema {
	return &Schema{
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
		ScriptedDefinitions: map[string]ScriptedDefinition{},
		WidgetTypes:         map[string]bool{},
	}
}

// listFolders returns the folder of each database the game's files make: the folder each script file belongs to,
// and every folder or file a row names that the listing has.
func listFolders(row *game.Game, listing []files.File) []string {
	folders := map[string]bool{}
	present := map[string]bool{}
	for _, file := range listing {
		_, rel := SplitStage(row, file.Rel)
		for dir := rel; dir != "."; dir = path.Dir(dir) {
			present[dir] = true
		}
		if files.Classify(rel) == files.FormatScript {
			folders[findFolder(row, rel)] = true
		}
	}
	for _, folder := range slices.Concat(slices.Collect(maps.Keys(game.Databases)), slices.Collect(maps.Keys(row.Databases))) {
		if present[folder] {
			folders[folder] = true
		}
	}
	delete(folders, ".")
	return slices.Sorted(maps.Keys(folders))
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

// IsLocReplacement reports whether a file, given by its path from its root, sits in a folder whose localization
// overrides every other file's in the given language.
func IsLocReplacement(row *game.Game, rel, language string) bool {
	_, rel = SplitStage(row, rel)
	return slices.ContainsFunc(game.LocReplaceFolders, func(folder string) bool {
		return strings.HasPrefix(rel, fmt.Sprintf(folder, language)+"/")
	})
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

// buildDatabases builds the databases of the game: one for each given folder, one for each nesting and those the GUI
// files declare. It then sets the schema's names for the databases that hold events, lists, concepts, modifier types,
// scripted effects and script values, the link prefixes, the databases that fire, the widget types and the callees.
func (schema *Schema) buildDatabases(row *game.Game, folders []string) {
	for _, folder := range folders {
		rule, _ := row.FindDatabase(folder)
		schema.buildDatabase(row, game.ToDatabase(folder), folder, rule)
	}
	for _, nesting := range slices.Concat(row.Nestings, game.Nestings) {
		if nesting.Database == "" {
			continue
		}
		label := game.ToDatabase(nesting.Parent) + "." + nesting.Database
		schema.buildDatabase(row, label, "", game.Database{Target: nesting.Target})
		if parent := schema.Databases[game.ToDatabase(nesting.Parent)]; parent != nil && !slices.Contains(parent.Nested, label) {
			parent.Nested = append(parent.Nested, label)
		}
	}
	for _, declaring := range game.GUI.Declares {
		schema.buildDatabase(row, game.GUI.DatabasePrefix+declaring, "", game.Database{})
	}
	schema.buildDatabase(row, game.GUI.WidgetDatabase, "", game.Database{})
	housed := map[string][]string{}
	callables := map[string]*string{"effects": &schema.Effects, "value": &schema.Values}
	for label, database := range schema.Databases {
		housed[database.Row.Houses] = append(housed[database.Row.Houses], label)
		if best, ok := callables[database.Callable.Shape]; ok && (*best == "" || label < *best) {
			*best = label
		}
		for _, prefix := range database.Row.Prefixes {
			schema.Prefixes[prefix] = label
		}
	}
	houses := map[string]*string{
		"events": &schema.Events, "lists": &schema.Lists, "concepts": &schema.Concepts, "modifiers": &schema.ModifierTypes,
	}
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

// buildDatabase builds the database with the given label and row. It does nothing if the schema has that database
// already. It also records the keys that declare the database's definitions inline and the words a data type names it
// by. If two databases have the same words, the one whose own name is those words takes them, as isNamedExactly
// says, and else the words name neither.
func (schema *Schema) buildDatabase(row *game.Game, label, folder string, rule game.Database) {
	if schema.Databases[label] != nil {
		return
	}
	schema.Databases[label] = &Database{
		Name: label, Folder: folder, Definition: &Field{}, Row: rule, Callable: game.Callables[label],
		Precedence: row.LookupPrecedence(label, folder), Documented: map[string]bool{},
	}
	for _, key := range rule.DeclaringKeys {
		schema.InlineDatabases[key] = label
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
// space: the database institution for the words "institution", where setup/start.institution has them too.
func isNamedExactly(label, phrase string) bool {
	return label == strings.ReplaceAll(phrase, " ", "_")
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

// findList returns the command for a key such as every_country_in_<region>, which iterates over one definition of a
// database. It also returns the database and the definition's name. The dumps declare such a list only for the game's
// own definitions, so the command is the one declared for the first of those. It returns a nil command for any other
// key.
func (schema *Schema) findList(key string) (command *Command, database, name string) {
	head, name, ok := strings.Cut(key, "_in_")
	if !ok {
		return nil, "", ""
	}
	for _, candidate := range schema.Databases {
		for _, alias := range listAliases(candidate) {
			if command, ok := schema.Commands[strings.ToLower(head)+"_in_"+alias]; ok {
				return command, candidate.Name, name
			}
		}
	}
	return nil, "", ""
}

// listAliases returns the names the game's own files write at the alias keys of a database's definitions, sorted and
// without quotes.
func listAliases(database *Database) []string {
	var aliases []string
	for _, key := range database.Row.AliasKeys {
		if field := database.Definition.findChild(key); field != nil {
			aliases = append(aliases, slices.Collect(maps.Keys(field.Vanilla.Values))...)
		}
	}
	for position, alias := range aliases {
		aliases[position] = strings.Trim(alias, `"`)
	}
	slices.Sort(aliases)
	return aliases
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

// ResolveCallTypes returns the type that each step of a data call leads to, starting from the block's data
// context. A first step that names a data type leads to that type. A step no dump declares gets "", and the steps
// after it are left out.
func (schema *Schema) ResolveCallTypes(call jomini.Call, datacontext string) []string {
	owners := schema.listCallOwners(call, datacontext)
	types := make([]string, len(owners))
	for i, owner := range owners {
		types[i] = owner
		if function, ok := schema.DataTypes[owner].Functions[call.Steps[i].Text]; ok {
			types[i] = function.Returns
		}
	}
	return types
}

// listCallOwners returns, for each step of a data call, the data type that declares the function the step calls,
// starting from the given data context. The first step may also be a function of the global type, or name a data
// type, which reads the object of that type in scope and is its own owner. A step no dump declares gets "", and the
// steps after it are left out.
func (schema *Schema) listCallOwners(call jomini.Call, datacontext string) []string {
	var owners []string
	current := datacontext
	for i, step := range call.Steps {
		function, ok := schema.DataTypes[current].Functions[step.Text]
		if !ok && i == 0 {
			current = game.GlobalType
			function, ok = schema.DataTypes[current].Functions[step.Text]
			if _, named := schema.DataTypes[step.Text]; !ok && named {
				owners, current = append(owners, step.Text), step.Text
				continue
			}
		}
		if !ok {
			return append(owners, "")
		}
		owners = append(owners, current)
		current = function.Returns
	}
	return owners
}

// FindFunction returns the data function that one step of a data call calls, starting from the given data context.
// The first step falls back to the global type. It returns false if the dumps declare none, if a step before it is
// unknown, or if the step names a data type rather than calling a function.
func (schema *Schema) FindFunction(call jomini.Call, datacontext string, step int) (DataFunction, bool) {
	owners := schema.listCallOwners(call, datacontext)
	if step < 0 || step >= len(owners) || owners[step] == "" {
		return DataFunction{}, false
	}
	function, ok := schema.DataTypes[owners[step]].Functions[call.Steps[step].Text]
	return function, ok
}

// ResolveMacro returns the call that a data call stands for. A call that opens with one of the game's data binding
// macros becomes the macro's text, with the arguments filled in, followed by the rest of the call. The result is
// expanded again while it opens with a macro. A macro that expands into itself gives an empty call. Any other call is
// returned as it is. Spans the macro wrote itself are empty.
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

// ListCallReferences returns the names that the quoted arguments of a data call refer to, with the database of each.
// For a step in game.GUI.CallTargets the database comes from that row, and is none for an argument the row skips. For
// any other step it is the database that holds the type the step returns, if there is one.
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
			// An argument at a position the row leaves empty is no name.
			if argument.Quoted && !argument.Span.IsEmpty() && (!listed || database != "") {
				references = append(references, Reference{Database: database, Name: argument.Text, Span: argument.Span})
			}
		}
	}
	return references
}

// ResolveTypeDatabase returns the database that holds the names of a data type: the one named in the singular by the
// type's words, else, for a type whose last word is Type, by the words before it (GovernmentType, governments). It
// returns false if none is, or several are.
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

// MatchValue reports whether a written value fits what a field takes. A scalar never fits a color or a block.
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
