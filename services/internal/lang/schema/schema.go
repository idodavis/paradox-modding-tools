// Package schema is the one position tree every source writes into, and the one rule that decides
// whether a claim in it may accuse a user's code.
package schema

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"os"
	"path"
	"slices"
	"strings"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/source"
	"paradox-modding-tools/services/internal/wiki"
)

// Source is who stated a claim, listed in trust order.
type Source uint8

const (
	SourceNone     Source = iota
	SourceRegistry        // game's rows and the language rules: declared and always fresh
	SourceDumps
	SourceVanilla
	SourceDocs
	SourceWiki
)

// Declared is whether a source declares: the dumps, the docs or the wiki.
func (from Source) Declared() bool {
	return from == SourceDumps || from == SourceDocs || from == SourceWiki
}

// Said is a claim and the source that stated it; From is SourceNone while the claim is empty.
type Said[T any] struct {
	Value T
	From  Source
}

// WordType is the kind of word a position holds.
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
)

// Value is what a source says a position holds.
type Value struct {
	Type     WordType
	Database string   // WordReference: the database the word names
	Words    []string // a declared word list, sorted; empty when none
}

// Need is how strongly a position is required inside its parent.
type Need uint8

const (
	NeedUnknown Need = iota
	NeedOptional
	NeedAlways
	NeedWhen
)

// Required is a position's need, with the condition text for NeedWhen.
type Required struct {
	Need Need
	When string
}

// Forms is the set of written forms seen at a position, as bits.
type Forms uint8

const (
	FormNumber Forms = 1 << iota
	FormBool
	FormDate
	FormWord
	FormText
	FormBlock
	FormCommand // a declared command is written directly in this block
)

// Seen is vanilla's evidence at a position; it is never a claim.
type Seen struct {
	In     int            // how many of the parent's written blocks hold this key
	Blocks int            // how many times this position was written as a block
	Forms  Forms          // the forms written here
	Values map[string]int // words written, with counts
}

// Key is one position: its claims, each with its source, and vanilla's evidence.
type Key struct {
	Keys     map[string]*Key // below it, by written name or form
	Value    Said[Value]
	Block    Said[game.Block]
	Scope    Said[string]
	Required Said[Required]
	Default  Said[string]
	Blurb    Said[string]
	// Written is the first declared source that writes the position: the dumps anywhere, the docs
	// and wiki only where vanilla writes it too.
	Written Source
	Seen    Seen
}

// Command is an effect, trigger or link from the dumps.
type Command struct {
	Kind   game.Kinds
	Scopes []string // where it runs; empty: any
	Out    string   // where it leads; "" none
	Args   *Key     // its usage template as positions
	Blurb  string
	Global bool // a link the dumps call global: it reads the same from any scope
	Wild   bool // a link the dumps call Wild Card: a pattern, never read from a written name
	Lists  bool // it iterates a list and has no target: its block's scope is unknown
}

// Database is one script folder's entries: the entry position tree and the folder's registry row.
type Database struct {
	Name   string
	Entry  *Key
	Naming game.Folder
	Fields map[string]bool // every key name the docs and wiki write in its entries
}

// Origin is where a source's data came from and whether it is out of date.
type Origin struct {
	From    string    // "yours", "install", "shipped" or "none"
	Written time.Time // when the source was written, by its oldest file; zero when unknown
	Stale   bool
}

// Scope is one scope type the dumps declare; each flag is false only where the dump says no.
type Scope struct {
	Name                         string
	Variables, Triggers, Effects bool // stores variables, evaluates triggers, executes effects
}

// Schema is everything the sources state about one game version.
type Schema struct {
	Game, Version string
	Sources       map[Source]Origin
	Databases     map[string]*Database
	Callees       map[string][]string // CalledIn word → the databases a call by it resolves in, in name order
	Commands      map[string]*Command
	Scopes        map[string]Scope
	Saved         map[string]bool // scope names the game saves from code
	Modifiers     map[string]Modifier
	OnActions     map[string]OnAction
	DataTypes     map[string]DataType
	CustomLoc     map[string]CustomLoc
	Prefixes      map[string]string // a link prefix → the database the names written after it are in; "" none
	Macros        map[string]*Key   // vanilla's scripted entries by name, each with its calls' parameters
	Lists         string            // the database of scripted lists (a folder row Iterated); "" none
}

// Link is the declared link a key starts with: the first link of a chain (root.liege), by name or by
// the prefix before ':' (c:FRA, scope:x), never a Wild Card link; nil for none. key is lower-case.
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

// Named is the database a word names: the one called by the word or the word + "s", else the one
// whose last folder is either; never a history database; "" for none or several.
func (schema *Schema) Named(word string) string {
	var named []string
	for _, name := range []string{word, word + "s"} {
		if database := schema.Databases[name]; database != nil && !database.Naming.History {
			named = append(named, name)
		}
	}
	exact := len(named) > 0
	for name, database := range schema.Databases {
		last := name[strings.LastIndexByte(name, '/')+1:]
		if !exact && (last == word || last == word+"s") && !database.Naming.History {
			named = append(named, name)
		}
	}
	if len(named) != 1 {
		return ""
	}
	return named[0]
}

// Flagged is the databases whose registry row passes a test (Events, Concepts, a CalledIn word), in
// name order.
func (schema *Schema) Flagged(flag func(row game.Folder) bool) (names []string) {
	for name, database := range schema.Databases {
		if flag(database.Naming) {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}

// Claim names which claim of a position, or of the dumps, a diagnostic wants to accuse with.
type Claim uint8

const (
	ClaimValue Claim = iota
	ClaimRequired
	ClaimCommand
)

// Speaks is whether a claim may accuse a user's code: a declared source states it, the source is
// fresh, and vanilla's evidence at the position does not contradict it.
func (schema *Schema) Speaks(claim Claim, key, parent *Key) bool {
	// only dumps, docs and wiki have Sources entries: the registry classifies and vanilla is
	// evidence, so neither accuses; nor does a stale source
	from := SourceDumps
	switch claim {
	case ClaimValue:
		from = key.Value.From
	case ClaimRequired:
		from = key.Required.From
	}
	if origin, found := schema.Sources[from]; !found || origin.Stale {
		return false
	}
	// a docs or wiki claim, and a declared word list from any source, accuses only at a position
	// vanilla also writes
	listed := claim == ClaimValue && len(key.Value.Value.Words) > 0
	if (from == SourceDocs || from == SourceWiki || listed) && key.Seen.In == 0 {
		return false
	}

	switch claim {
	case ClaimValue:
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
		if key.Seen.Forms&^(allowed|FormCommand) != 0 { // FormCommand marks the block, not a value form
			return false
		}
		if words := key.Value.Value.Words; len(words) > 0 {
			for word := range key.Seen.Values {
				if _, inList := slices.BinarySearch(words, word); !inList {
					return false
				}
			}
		}
		return true
	case ClaimRequired:
		// Without a parent the claim cannot be checked against vanilla.
		return parent != nil && key.Required.Value.Need == NeedAlways && key.Seen.In >= parent.Seen.Blocks
	}
	return true
}

// At is the position under key for a written name, nil when missing or under no position; it never
// makes one.
func (key *Key) At(name string) *Key {
	if key == nil {
		return nil
	}
	return key.Keys[collapse(name)]
}

// Below is At, made when missing; its name is cloned, so a position never pins a source's text.
func (key *Key) Below(name string) *Key {
	if child := key.At(name); child != nil {
		return child
	}
	if key.Keys == nil {
		key.Keys = map[string]*Key{}
	}
	child := &Key{}
	key.Keys[strings.Clone(collapse(name))] = child
	return child
}

// collapse is the name a written key is held under, by written form only (jomini's readers): a
// number <number>, a date <date>, prefix:x <prefix:>, any other name as written.
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

// Set writes a claim only when it is empty: sources write in trust order, so the first one wins.
func Set[T any](claim *Said[T], value T, from Source) {
	if claim.From == SourceNone {
		claim.Value, claim.From = value, from
	}
}

// Input is what the schema stage reads.
type Input struct {
	Game, Version string
	Install       []source.File   // the install's inventory, in load order
	Dumps         []source.File   // the dump folders' inventory
	Wiki          string          // the wiki sidecar's path
	Updated       time.Time       // game.InstallUpdatedAt
	LocKeys       map[string]bool // the install's localization keys, english and the workspace language
	DumpsOnly     bool            // stop after the databases and dumps: the shipped schema's part
	Shipped       *Schema         // a shipped dump part, taken in place of reading the dump files
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
		Game: input.Game, Version: input.Version, Sources: map[Source]Origin{}, Databases: map[string]*Database{},
	}

	// a database's entry block is a fields block unless its folder row's Body says what the entry
	// is; each of its Refs names its entries
	declare := func(name string, row game.Folder) {
		database := &Database{Name: name, Entry: &Key{}, Naming: row, Fields: map[string]bool{}}
		Set(&database.Entry.Block, cmp.Or(row.Body, game.BlockFields), SourceRegistry)
		for _, at := range row.Refs {
			position := database.Entry
			for step := range strings.SplitSeq(at, "/") {
				position = position.Below(step)
			}
			Set(&position.Value, Value{Type: WordReference, Database: name}, SourceRegistry)
		}
		schema.Databases[name] = database
	}

	// 1. databases: each script file's, by folder; a file at the root belongs to none
	first := map[string]string{}
	for _, file := range input.Install {
		if file.Format != source.Script {
			continue
		}
		at, name := schema.folder(file.Rel)
		if at != "." && schema.Databases[name] == nil {
			declare(name, info.Folders[at])
			first[name] = file.Rel
		}
	}

	// the databases a call by each CalledIn word resolves in, in name order; AnyCall every one
	schema.Callees = map[string][]string{}
	for _, name := range slices.Sorted(maps.Keys(schema.Databases)) {
		if called := schema.Databases[name].Naming.CalledIn; len(called) > 0 {
			for _, word := range append(slices.Clone(called), game.AnyCall) {
				schema.Callees[word] = append(schema.Callees[word], name)
			}
		}
	}
	schema.Lists = cmp.Or(schema.Flagged(func(row game.Folder) bool { return row.Iterated })...)

	// 2. dumps: read from the dump files, or taken whole from a shipped dump part
	if shipped := input.Shipped; shipped != nil {
		schema.Commands, schema.Scopes, schema.Saved = shipped.Commands, shipped.Scopes, shipped.Saved
		schema.Modifiers, schema.OnActions = shipped.Modifiers, shipped.OnActions
		schema.DataTypes, schema.CustomLoc = shipped.DataTypes, shipped.CustomLoc
		schema.Sources[SourceDumps] = shipped.Sources[SourceDumps]
	} else {
		written := schema.readDumps(input.Dumps)
		schema.Sources[SourceDumps] = Origin{From: "none"}
		if !written.IsZero() {
			schema.Sources[SourceDumps] = Origin{From: "yours", Written: written, Stale: written.Before(input.Updated)}
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
		if file.Format != source.Info {
			continue
		}
		at, name := schema.folder(file.Rel)
		if schema.Databases[name] == nil || at != path.Dir(file.Rel) {
			continue
		}
		text, err := file.Text()
		if err != nil {
			continue
		}
		markdown := strings.EqualFold(path.Ext(file.Rel), ".md")
		examples = append(examples, parsedExample{name, jomini.Parse(normalize(text, markdown, false)), SourceDocs})
	}
	schema.Sources[SourceDocs] = Origin{From: "install", Written: input.Updated}

	// the wiki: each database's own named pages' <pre> examples; a history database names none. A
	// sanitized page keeps no version label, so the wiki is never stale.
	schema.Sources[SourceWiki] = Origin{From: "none"}
	if sidecar, err := os.Stat(input.Wiki); err == nil {
		schema.Sources[SourceWiki] = Origin{From: "yours", Written: sidecar.ModTime()}
	}
	for _, name := range slices.Sorted(maps.Keys(schema.Databases)) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if schema.Databases[name].Naming.History {
			continue
		}
		for _, page := range wiki.NamedPages(input.Game, name, first[name]) {
			document, err := html.Parse(strings.NewReader(page.HTML))
			if err != nil {
				continue
			}
			for node := range document.Descendants() {
				if node.DataAtom != atom.Pre {
					continue
				}
				var text strings.Builder
				for inner := range node.Descendants() {
					if inner.Type == html.TextNode {
						text.WriteString(inner.Data)
					}
				}
				parsed := jomini.Parse(normalize(text.String(), false, false))
				examples = append(examples, parsedExample{name, parsed, SourceWiki})
			}
		}
	}

	// 4. each database's Fields: every key name its docs and wiki examples write, at any depth
	for _, example := range examples {
		fields := schema.Databases[example.database].Fields
		var gather func(statements []jomini.Statement)
		gather = func(statements []jomini.Statement) {
			for _, statement := range statements {
				if assign, isAssign := statement.(*jomini.Assignment); isAssign {
					for _, name := range alternatives(assign.Key.Text) {
						fields[collapse(name)] = true
					}
				}
				if block := jomini.ChildBlock(statement); block != nil {
					gather(block.Statements)
				}
			}
		}
		gather(example.parsed.Root.Statements)
	}

	// 5. vanilla's scripted entries by name first, so observe reads a key naming one as a call and
	// never as an argument (game.StepKnownCall); then workers parse ahead (not tables or sound lists),
	// and Entries and observe run in file order, every database Entries names is made, and each
	// database's entry names are kept
	names := map[string]map[string]bool{}
	schema.Macros = map[string]*Key{}
	var files []source.File
	for _, file := range input.Install {
		if at, _ := schema.folder(file.Rel); file.Format == source.Script && len(info.Folders[at].CalledIn) > 0 {
			text, _ := file.Text()
			parsed := jomini.Parse(text)
			schema.Entries(file, &parsed, func(entry Entry) { schema.Macros[strings.Clone(entry.Name)] = &Key{} })
		}
		if file.Format == source.Script || file.Format == source.Table || file.Format == source.List {
			files = append(files, file)
		}
	}
	err := source.Ordered(ctx, files, func(file source.File) (result jomini.Result) {
		if file.Format == source.Script {
			text, _ := file.Text()
			result = jomini.Parse(text)
		}
		return result
	}, func(file source.File, result jomini.Result) {
		var parsed *jomini.Result
		if file.Format == source.Script {
			parsed = &result
		}
		var entries []Entry
		schema.Entries(file, parsed, func(entry Entry) { entries = append(entries, entry) })
		for _, entry := range entries {
			if schema.Databases[entry.Database] == nil {
				declare(entry.Database, game.Folder{})
			}
			if entry.Name != "" {
				if names[entry.Database] == nil {
					names[entry.Database] = map[string]bool{}
				}
				names[entry.Database][entry.Name] = true
			}
			if entry.Local && schema.Macros[entry.Name] == nil { // inline in another folder's file
				schema.Macros[strings.Clone(entry.Name)] = &Key{}
			}
		}
		schema.observe(entries)
	})
	if err != nil {
		return nil, err
	}

	// 6. the types vanilla's values give, before the docs' claims so the trust order holds; a link
	// prefix names the database of the names script makes, else vanilla's, else the database its
	// link's own name names
	schema.Prefixes = map[string]string{}
	for _, row := range game.NameCommands {
		schema.Prefixes[row.Database] = row.Database
	}
	schema.typeValues(names, input.LocKeys)
	for name := range schema.Commands {
		if schema.Link(name) != nil && schema.Prefixes[name] == "" {
			schema.Prefixes[name] = schema.Named(name)
		}
	}

	// 7. the docs' claims, then the wiki's, joined where vanilla writes each key
	for _, example := range examples {
		schema.join(&example.parsed, schema.Databases[example.database], example.from)
	}
	return schema, nil
}
