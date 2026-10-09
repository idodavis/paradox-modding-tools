// Package schema is the position tree every source writes into, and the rule that decides whether
// a claim in it may accuse a user's code.
package schema

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"path"
	"slices"
	"strings"
	"time"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/inventory"
	"paradox-modding-tools/services/internal/jomini"
)

// Source is who stated a claim, listed in trust order.
type Source uint8

const (
	SourceNone     Source = iota
	SourceRegistry        // game's rows and the language rules: they classify, never accuse
	SourceDumps
	SourceVanilla
	SourceDocs
	SourceWiki
)

// Declared is whether a source declares: the dumps, the docs or the wiki.
func (from Source) Declared() bool {
	return from == SourceDumps || from == SourceDocs || from == SourceWiki
}

// Claim is a claim and the source that stated it; From is SourceNone while the claim is empty.
type Claim[T any] struct {
	Value  T
	Source Source
}

// WordType is the kind of word a position holds; WordBlock is a doc's block, whose kind none names.
type WordType uint8

const (
	WordUnknown WordType = iota
	WordNumber
	WordBool
	WordDate
	WordText
	WordReference
	WordLoc
	WordFile
	WordScope
	WordBlock
)

// Expected is what a source says a position holds.
type Expected struct {
	Type     WordType
	Database string   // WordReference: the database the word names
	Words    []string // a declared word list, sorted; empty when none
}

// Level is how strongly a position is required inside its parent.
type Level uint8

// The levels a position's requirement can have.
const (
	LevelUnknown Level = iota
	LevelOptional
	LevelAlways
	LevelConditional
)

// Requirement is a position's need, with the condition text for NeedWhen.
type Requirement struct {
	Level Level
	When  string
}

// Forms is the set of written forms seen at a position, as bits.
type Forms uint16

const (
	FormNumber Forms = 1 << iota
	FormBool
	FormDate
	FormWord
	FormText
	FormBlock
	FormCommand // a declared command is written directly in this block
	FormTrigger // a command that is only a trigger is written directly in this block
	FormEffect  // a command that is only an effect is written directly in this block
)

// formOf is the form a word is written in.
func formOf(written *jomini.Scalar) Forms {
	switch literal := jomini.Literal(written.Text); {
	case written.Quoted:
		return FormText
	case literal == jomini.LitNumber:
		return FormNumber
	case literal == jomini.LitDate:
		return FormDate
	case slices.Contains(game.Bools, written.Text):
		return FormBool
	}
	return FormWord
}

// Evidence is vanilla's evidence at a position; it is never a claim.
type Evidence struct {
	In     int            // how many of the parent's written blocks hold this key
	Blocks int            // how many times this position was written as a block
	Forms  Forms          // the forms written here
	Values map[string]int // words written, with counts
}

// Position is one position: its claims, each with its source, and vanilla's evidence.
type Position struct {
	Children map[string]*Position // below it, by written name or form
	Value    Claim[Expected]
	Block    Claim[game.Block]
	Scope    Claim[string]
	Required Claim[Requirement]
	Default  Claim[string]
	Blurb    Claim[string]
	// DeclaredBy is the first declared source that writes the position: the dumps anywhere, the docs
	// and wiki only where vanilla writes it too.
	DeclaredBy Source
	Evidence   Evidence
}

// Command is an effect, trigger or link from the dumps.
type Command struct {
	Kind   game.Kinds
	Scopes []string  // where it runs; empty: any
	Out    string    // where it leads; "" none
	Args   *Position // its usage template as positions
	Blurb  string
	Global bool // a link the dumps call global: it reads the same from any scope
	Wild   bool // a link the dumps call Wild Card: a pattern, never read from a written name
	Lists  bool // it iterates a list and has no target: its block's scope is unknown
}

// Database is one script folder's entries: the entry position tree and the folder's registry row.
type Database struct {
	Name   string
	Entry  *Position
	Row    game.Folder
	Fields map[string]bool // every key name the docs and wiki write in its entries
}

// SourceState is where a source's data came from and whether it is out of date.
type SourceState struct {
	From    string    // "yours", "install", "shipped" or "none"
	Written time.Time // when the source was written, by its oldest file; zero when unknown
	Stale   bool
}

// Scope is one scope type the dumps declare; each flag is false only where the dump says no.
type Scope struct {
	Name                         string
	Variables, Triggers, Effects bool // stores variables, evaluates triggers, executes effects
}

// ScriptedEntry is a scripted entry as a call reads it: at its own parameters, or at its database's entry
// where the database takes none. It names the database, so no entry tree is held twice.
type ScriptedEntry struct {
	Database   string
	Parameters *Position // nil where the database takes no parameters
}

// Schema is everything the sources state about one game version.
type Schema struct {
	Game, Version   string
	Sources         map[Source]SourceState
	Databases       map[string]*Database
	Callees         map[string][]string // CalledIn word → the databases a call by it resolves in, in name order
	Commands        map[string]*Command
	Scopes          map[string]Scope
	Saved           map[string]bool // scope names the game saves from code
	Modifiers       map[string]Modifier
	OnActions       map[string]OnAction
	DataTypes       map[string]DataType
	CustomLoc       map[string]CustomLoc
	Prefixes        map[string]string        // a link prefix → the database the names written after it are in; "" none
	ScriptedEntries map[string]ScriptedEntry // vanilla's scripted entries by calling word and name (word:name)
	Lists           string                   // the database of scripted lists (a folder row Iterated); "" none
}

// Link is the declared link a lower-case key starts with: the first link of a chain (root.liege),
// by name or by the prefix before ':' (c:FRA, scope:x), never a Wild Card link; nil for none.
func (schema *Schema) Link(key string) *Command {
	first, _, _ := strings.Cut(key, ".")
	prefix, _, _ := strings.Cut(first, ":")
	for _, name := range []string{first, prefix} {
		if command := schema.Commands[name]; command != nil && command.Kind&game.KindLink != 0 && !command.Wild {
			return command
		}
	}
	return nil
}

// DatabaseNamed is the database a word names: the one called by the word or the word + "s", else the one
// whose last folder is either; never a history database; "" for none or several.
func (schema *Schema) DatabaseNamed(word string) string {
	var named []string
	for _, name := range []string{word, word + "s"} {
		if database := schema.Databases[name]; database != nil && !database.Row.History {
			named = append(named, name)
		}
	}
	exact := len(named) > 0
	for name, database := range schema.Databases {
		last := name[strings.LastIndexByte(name, '/')+1:]
		if !exact && (last == word || last == word+"s") && !database.Row.History {
			named = append(named, name)
		}
	}
	if len(named) != 1 {
		return ""
	}
	return named[0]
}

// DatabasesWhere is the databases whose registry row passes a test (Events, Concepts, a CalledIn word), in
// name order.
func (schema *Schema) DatabasesWhere(flag func(row game.Folder) bool) (names []string) {
	for name, database := range schema.Databases {
		if flag(database.Row) {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}

// Check names which claim of a position, or of the dumps, a diagnostic wants to accuse with.
type Check uint8

// The claims a diagnostic can accuse with.
const (
	CheckValue Check = iota
	CheckRequired
	CheckCommand
)

// MayAccuse is whether a claim may accuse a user's code: a declared source states it, the source is
// fresh, and vanilla's evidence at the position does not contradict it.
func (schema *Schema) MayAccuse(claim Check, key, parent *Position) bool {
	// only dumps, docs and wiki have Sources entries: the registry classifies and vanilla is
	// evidence, so neither accuses; nor does a stale source
	from := SourceDumps
	switch claim {
	case CheckValue:
		from = key.Value.Source
	case CheckRequired:
		from = key.Required.Source
	}
	if origin, found := schema.Sources[from]; !found || origin.Stale {
		return false
	}
	// a docs or wiki claim, and a declared word list from any source, accuses only at a position
	// vanilla also writes
	listed := claim == CheckValue && len(key.Value.Value.Words) > 0
	if (from == SourceDocs || from == SourceWiki || listed) && key.Evidence.In == 0 {
		return false
	}

	switch claim {
	case CheckValue:
		var allowed Forms
		switch key.Value.Value.Type {
		case WordNumber:
			allowed = FormNumber
		case WordBool:
			allowed = FormBool
		case WordDate:
			allowed = FormDate
		case WordText, WordReference, WordLoc, WordFile, WordScope:
			allowed = FormWord | FormText
		default:
			// a declared word list with no type is a word value
			if len(key.Value.Value.Words) == 0 {
				return false
			}
			allowed = FormWord | FormText
		}
		// the command forms mark the block, not a value
		if key.Evidence.Forms&^(allowed|FormCommand|FormTrigger|FormEffect) != 0 {
			return false
		}
		if words := key.Value.Value.Words; len(words) > 0 {
			for word := range key.Evidence.Values {
				if _, inList := slices.BinarySearch(words, word); !inList {
					return false
				}
			}
		}
		return true
	case CheckRequired:
		// Without a parent the claim cannot be checked against vanilla.
		return parent != nil && key.Required.Value.Level == LevelAlways && key.Evidence.In >= parent.Evidence.Blocks
	}
	return true
}

// Child is the position under key for a written name, nil when missing or under no position; it never
// makes one.
func (key *Position) Child(name string) *Position {
	if key == nil {
		return nil
	}
	return key.Children[collapse(name)]
}

// EnsureChild is At, made when missing; its name is cloned, so a position never pins a source's text.
func (key *Position) EnsureChild(name string) *Position {
	if child := key.Child(name); child != nil {
		return child
	}
	if key.Children == nil {
		key.Children = map[string]*Position{}
	}
	child := &Position{}
	key.Children[strings.Clone(collapse(name))] = child
	return child
}

// collapse is the name a written key is held under, by written form only: a number <number>, a
// date <date>, prefix:x <prefix:>, any other name as written.
func collapse(name string) string {
	switch jomini.Literal(name) {
	case jomini.LitNumber:
		return "<number>"
	case jomini.LitDate:
		return "<date>"
	}
	if prefixed, isPrefixed := jomini.SplitPrefixed(name); isPrefixed {
		return "<" + prefixed.Prefix + ":>"
	}
	return name
}

// SetFirst writes a claim only when it is empty: sources write in trust order, so the first one wins.
func SetFirst[T any](claim *Claim[T], value T, from Source) {
	if claim.Source == SourceNone {
		claim.Value, claim.Source = value, from
	}
}

// Input is what the schema stage reads.
type Input struct {
	Game, Version string
	Install       []inventory.File    // the install's inventory, in load order
	Dumps         []inventory.File    // the dump folders' inventory
	WikiExamples  map[string][]string // the wiki's script examples by database name, from wiki.ScriptExamples
	WikiWritten   time.Time           // the wiki sidecar's time; zero for none
	Updated       time.Time           // game.InstallUpdatedAt
	LocKeys       map[string]bool     // the install's localization keys, english and the workspace language
	DumpsOnly     bool                // stop after the databases and dumps: the shipped schema's part
	Shipped       *Schema             // a shipped dump part, taken in place of reading the dump files
}

// DatabaseNames returns the names of the databases Build will declare for an input: what the install's
// folders and the game's rows make, before any source is read. The scan asks the wiki for examples by them.
func DatabaseNames(input Input) []string {
	info := game.Get(input.Game)
	if info == nil {
		return nil
	}
	schema := &Schema{Game: input.Game, Databases: map[string]*Database{}}
	schema.declareDatabases(input.Install, info)
	return slices.Sorted(maps.Keys(schema.Databases))
}

// declare makes a database. Its entry block is a fields block unless its folder row's Body says what
// the entry is; each of its Refs names its entries.
func (schema *Schema) declare(name string, row game.Folder) {
	database := &Database{Name: name, Entry: &Position{}, Row: row, Fields: map[string]bool{}}
	SetFirst(&database.Entry.Block, cmp.Or(row.Body, game.BlockFields), SourceRegistry)
	for _, at := range row.Refs {
		position := database.Entry
		for step := range strings.SplitSeq(at, "/") {
			position = position.EnsureChild(step)
		}
		SetFirst(&position.Value, Expected{Type: WordReference, Database: name}, SourceRegistry)
	}
	schema.Databases[name] = database
}

// declareDatabases declares the databases the install's folders and the game's rows make, and the
// calls that resolve in them.
func (schema *Schema) declareDatabases(install []inventory.File, info *game.GameInfo) {
	// 1. databases: each script file's, by folder; a file at the root belongs to none, and one whose
	// file type names its own entries (.asset, .gui) or none makes its folder no database
	var scripts []string
	for _, file := range install {
		fileType, typed := game.FileTypes[strings.ToLower(path.Ext(file.Rel))]
		if file.Format == inventory.Script && typed && fileType.NameKey == "" && fileType.Keywords == nil {
			scripts = append(scripts, file.Rel)
		}
	}

	// shallow folders first, so a common/ subfolder finds its ancestor's database
	slices.SortStableFunc(scripts, func(first, second string) int {
		return cmp.Compare(strings.Count(first, "/"), strings.Count(second, "/"))
	})
	for _, rel := range scripts {
		if at, name := schema.folder(rel); at != "." && schema.Databases[name] == nil {
			schema.declare(name, info.Folders[at])
		}
	}

	// a nested row that sets up entries defined elsewhere makes a history database of its keys
	for _, row := range info.Nested {
		if row.Of != "" {
			schema.declare(strings.TrimPrefix(row.Parent, "common/")+"."+row.Database, game.Folder{History: true, Of: row.Of})
		}
	}

	// a row named after a file type's own database (gui.type) declares it, with the type's Body
	for at, row := range info.Folders {
		if ext, _, cut := strings.Cut(at, "."); cut && game.FileTypes["."+ext].Body != 0 {
			row.Body = game.FileTypes["."+ext].Body
			schema.declare(at, row)
		}
	}

	// the databases a call by each CalledIn word resolves in, in name order; AnyCall every scripted
	// folder's (a file type's own database answers only its own words)
	schema.Callees = map[string][]string{}
	for _, name := range slices.Sorted(maps.Keys(schema.Databases)) {
		called := schema.Databases[name].Row.CalledIn
		if len(called) > 0 && !strings.Contains(name, ".") {
			called = append(slices.Clone(called), game.AnyCall)
		}
		for _, word := range called {
			schema.Callees[word] = append(schema.Callees[word], name)
		}
	}
	schema.Lists = cmp.Or(schema.DatabasesWhere(func(row game.Folder) bool { return row.Iterated })...)
}

// Build reads every source into the schema: the databases the install holds, the dumps, the docs'
// and wiki's key names, vanilla and the value types it gives, then the docs' and wiki's claims, in
// that order, so a claim keeps the first source that states it.
func Build(ctx context.Context, input Input) (*Schema, error) {
	info := game.Get(input.Game)
	if info == nil {
		return nil, fmt.Errorf("unknown game %q", input.Game)
	}
	schema := &Schema{
		Game: input.Game, Version: input.Version, Sources: map[Source]SourceState{}, Databases: map[string]*Database{},
	}

	schema.declareDatabases(input.Install, info)

	// 2. dumps: read from the dump files, or taken whole from a shipped dump part
	if shipped := input.Shipped; shipped != nil {
		schema.Commands, schema.Scopes, schema.Saved = shipped.Commands, shipped.Scopes, shipped.Saved
		schema.Modifiers, schema.OnActions = shipped.Modifiers, shipped.OnActions
		schema.DataTypes, schema.CustomLoc = shipped.DataTypes, shipped.CustomLoc
		schema.Sources[SourceDumps] = shipped.Sources[SourceDumps]
	} else {
		written := schema.readDumps(input.Dumps)
		schema.Sources[SourceDumps] = SourceState{From: "none"}
		if !written.IsZero() {
			schema.Sources[SourceDumps] = SourceState{From: "yours", Written: written, Stale: written.Before(input.Updated)}
		}
	}
	if input.DumpsOnly {
		return schema, nil
	}

	// 3. docs and wiki examples, each normalized and parsed once and kept with its database and
	// source: a doc is read into its own folder's database; one under a row above its folder is not
	type parsedExample struct {
		database string
		parsed   jomini.Result
		from     Source
	}
	var examples []parsedExample
	for _, file := range input.Install {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if file.Format != inventory.Prose {
			continue
		}
		// a doc documents its own folder's database, or the folder beside it it is named after
		// (history/_characters.info)
		at, name := schema.folder(file.Rel)
		stem := strings.TrimPrefix(strings.TrimSuffix(path.Base(file.Rel), path.Ext(file.Rel)), "_")
		if beside := path.Join(path.Dir(file.Rel), stem); schema.Databases[NameOf(beside)] != nil {
			at, name = path.Dir(file.Rel), NameOf(beside)
		}
		if schema.Databases[name] == nil || at != path.Dir(file.Rel) {
			continue
		}
		text, err := file.Text()
		if err != nil {
			continue
		}
		lines := strings.Split(text, "\n")
		if strings.EqualFold(path.Ext(file.Rel), ".md") {
			lines = markdown(text)
		}
		examples = append(examples, parsedExample{name, jomini.Parse(normalize(lines)), SourceDocs})
	}
	schema.Sources[SourceDocs] = SourceState{From: "install", Written: input.Updated}

	// the wiki: each database's own named pages' <pre> examples; a history database names none. A
	// sanitized page keeps no version label, so the wiki is never stale.
	schema.Sources[SourceWiki] = SourceState{From: "none"}
	if !input.WikiWritten.IsZero() {
		schema.Sources[SourceWiki] = SourceState{From: "yours", Written: input.WikiWritten}
	}
	for _, name := range slices.Sorted(maps.Keys(schema.Databases)) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if schema.Databases[name].Row.History {
			continue
		}
		for _, text := range input.WikiExamples[name] {
			examples = append(examples, parsedExample{name, jomini.Parse(normalize(strings.Split(text, "\n"))), SourceWiki})
		}
	}

	// 4. each database's Fields: every key name its docs and wiki examples write, at any depth
	for _, example := range examples {
		fields := schema.Databases[example.database].Fields
		jomini.Walk(example.parsed.Root, func(statement jomini.Statement, _ int, _ *jomini.Block) bool {
			if assign, isAssign := statement.(*jomini.Assignment); isAssign {
				for _, name := range alternatives(assign.Key.Text) {
					fields[collapse(name)] = true
				}
			}
			return true
		})
	}

	// 5. vanilla's scripted entries first, so observe reads their names as calls (StepKnownCall);
	// then every file's entries and evidence, in file order, making each database Entries names
	names := map[string]map[string]bool{}
	schema.ScriptedEntries = map[string]ScriptedEntry{}

	// macro keeps a called entry by each word its database is called by (word:name); one of a
	// database with no parameters reads at its database's entry (every GUI type's block is a widget)
	macro := func(entry Entry) {
		database := schema.Databases[entry.Database]
		if database == nil || len(database.Row.CalledIn) == 0 {
			return
		}
		called, words := ScriptedEntry{Database: database.Name}, database.Row.CalledIn
		if database.Row.Parameters {
			called.Parameters = &Position{}
		}
		if !strings.Contains(entry.Database, ".") {
			words = append(slices.Clone(words), game.AnyCall)
		}
		for _, word := range words {
			schema.ScriptedEntries[word+":"+strings.Clone(entry.Name)] = called
		}
	}
	var files []inventory.File
	parsedFirst := map[string]jomini.Result{} // the called folders' and GUI files' parses, read once
	for _, file := range input.Install {
		at, _ := schema.folder(file.Rel)
		gui := game.FileTypes[strings.ToLower(path.Ext(file.Rel))].Keywords != nil
		if file.Format == inventory.Script && (len(info.Folders[at].CalledIn) > 0 || gui) {
			text, _ := file.Text()
			parsed := jomini.Parse(text)
			schema.FindEntries(file, &parsed, macro)
			parsedFirst[file.Path] = parsed
		}
		if file.Format == inventory.Script || file.Format == inventory.Table || file.Format == inventory.List {
			files = append(files, file)
		}
	}
	err := inventory.Ordered(ctx, files, func(file inventory.File) (result jomini.Result) {
		if parsed, found := parsedFirst[file.Path]; found {
			return parsed
		}
		if file.Format == inventory.Script {
			text, _ := file.Text()
			result = jomini.Parse(text)
		}
		return result
	}, func(file inventory.File, result jomini.Result) {
		var parsed *jomini.Result
		if file.Format == inventory.Script {
			parsed = &result
		}
		var entries []Entry
		schema.FindEntries(file, parsed, func(entry Entry) { entries = append(entries, entry) })
		for _, entry := range entries {
			if schema.Databases[entry.Database] == nil {
				ext, _, _ := strings.Cut(entry.Database, ".") // a file type's database holds its Body (gui.x)
				schema.declare(entry.Database, game.Folder{Body: game.FileTypes["."+ext].Body})
			}
			if entry.Name != "" {
				if names[entry.Database] == nil {
					names[entry.Database] = map[string]bool{}
				}
				names[entry.Database][entry.Name] = true
			}
			if entry.Local { // inline in another folder's file
				macro(entry)
			}
		}
		schema.observe(entries)
	})
	if err != nil {
		return nil, err
	}

	// 6. vanilla's value types, before the docs' claims; a link prefix names the database of the
	// names script makes, else vanilla's, else the one its own name names
	schema.Prefixes = map[string]string{}
	for _, row := range game.NameCommands {
		schema.Prefixes[row.Database] = row.Database
	}
	schema.typeValues(names, input.LocKeys)
	for name := range schema.Commands {
		if schema.Link(name) != nil && schema.Prefixes[name] == "" {
			schema.Prefixes[name] = schema.DatabaseNamed(name)
		}
	}

	// 7. the docs' claims, then the wiki's, joined where vanilla writes each key
	for _, example := range examples {
		schema.join(&example.parsed, schema.Databases[example.database], example.from)
	}
	return schema, nil
}
