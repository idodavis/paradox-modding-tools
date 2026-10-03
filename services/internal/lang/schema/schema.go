// Package schema is the one position tree every source writes into, and the one rule that decides
// whether a claim in it may accuse a user's code.
package schema

import (
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
	SourceRegistry        // game.go's rows, such as an entry's block kind: declared and always fresh
	SourceDumps
	SourceVanilla
	SourceDocs
	SourceWiki
)

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

// Value is what a declared source says a position holds.
type Value struct {
	Type     WordType
	Database string   // WordReference: the database the word names
	Words    []string // a declared word list, sorted; empty when none
}

// Block is the kind of content a block position holds.
type Block uint8

const (
	BlockUnknown Block = iota
	BlockFields
	BlockTriggers
	BlockEffects
)

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
	Block    Said[Block]
	Scope    Said[string]
	Required Said[Required]
	Default  Said[string]
	Blurb    Said[string]
	Seen     Seen
}

// Kinds is the set of roles one command name plays, as bits.
type Kinds uint8

const (
	KindEffect Kinds = 1 << iota
	KindTrigger
	KindLink
)

// Command is an effect, trigger or link from the dumps.
type Command struct {
	Kind   Kinds
	Scopes []string // where it runs; empty: any
	Out    string   // where it leads; "" none
	Args   *Key     // its usage template as positions
	Blurb  string
	Global bool // a link the dumps call global: it reads the same from any scope
}

// Database is one script folder's entries: the entry position tree and the folder's registry row.
type Database struct {
	Name   string
	Entry  *Key
	Naming game.Folder
	Fields map[string]bool // key names a declared source states in the entry tree (D-21)
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
	Unread        []string // doc and wiki lines no rule reads, for language health
}

// Link is the declared link a key reads through: its first link of a chain (root.liege), by name or
// by the prefix before ':' (c:FRA, scope:x); nil for none. Names are held lower-case (D-26).
func (schema *Schema) Link(key string) *Command {
	first, _, _ := strings.Cut(key, ".")
	prefix, _, _ := strings.Cut(first, ":")
	for _, name := range []string{first, prefix} {
		if command := schema.Commands[name]; command != nil && command.Kind&KindLink != 0 {
			return command
		}
	}
	return nil
}

// Named is the database a word names (D-15): the one called by the word or the word + "s", else
// the one whose last folder is either; never a history database; "" for none or several.
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

// Slot is where a key under a position reads, and its command (D-17): a stated field (a usage's
// argument, D-34) first; create makes a missing field; triggers/effects hold stated fields (D-24).
func (schema *Schema) Slot(under *Key, name string, create bool) (*Key, *Command) {
	field := under.At(name)
	if under.Block.Value != BlockFields && (field == nil || !field.Stated()) {
		lower := strings.ToLower(name)
		if command := schema.Commands[lower]; command != nil && command.Kind&KindLink == 0 {
			return command.Args, command
		}
		if link := schema.Link(lower); link != nil {
			return nil, link
		}
	}
	if create {
		return under.Below(name), nil
	}
	if field != nil && (under.Block.Value == BlockTriggers || under.Block.Value == BlockEffects) && !field.Stated() {
		return nil, nil
	}
	return field, nil
}

// Stated is whether a declared source (dumps, docs or wiki) states a claim at the position.
func (key *Key) Stated() bool {
	froms := []Source{key.Value.From, key.Block.From, key.Scope.From, key.Required.From, key.Default.From, key.Blurb.From}
	return slices.ContainsFunc(froms, func(from Source) bool {
		return from == SourceDumps || from == SourceDocs || from == SourceWiki
	})
}

// Claim names which claim of a position, or of the dumps, a diagnostic wants to accuse with.
type Claim uint8

const (
	ClaimValue Claim = iota
	ClaimRequired
	ClaimCommand
)

// Speaks is spec §4.3: a claim may accuse only when a declared source states it, the source is
// fresh, and vanilla's evidence at the position does not contradict it.
func (schema *Schema) Speaks(claim Claim, key, parent *Key) bool {
	// Only dumps, docs and wiki have Sources entries: the registry classifies and vanilla is
	// evidence, so neither accuses (§4.3 rule 1); nor does a stale source.
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
	// A docs or wiki claim accuses only at a position vanilla also writes (D-20).
	if (from == SourceDocs || from == SourceWiki) && key.Seen.In == 0 {
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

// At is the position under key for a written name, nil when missing; it never makes one.
func (key *Key) At(name string) *Key {
	return key.Keys[collapse(name)]
}

// Below is At, made when missing.
func (key *Key) Below(name string) *Key {
	if child := key.At(name); child != nil {
		return child
	}
	if key.Keys == nil {
		key.Keys = map[string]*Key{}
	}
	child := &Key{}
	key.Keys[collapse(name)] = child
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
	Install       []source.File // the install's inventory, in load order
	Dumps         []source.File // the dump folders' inventory
	Wiki          string        // the wiki sidecar's path
	Updated       time.Time     // game.InstallUpdatedAt
	DumpsOnly     bool          // stop after the databases and dumps: the generator's shipped part (D-27)
	Shipped       *Schema       // a shipped dump part, taken in place of reading the dump files
}

// Build is the schema stage: the databases the install holds, then the dumps, docs, wiki and
// vanilla written into them in that order, so a claim keeps the first source that states it.
func Build(ctx context.Context, input Input) (*Schema, error) {
	info := game.Get(input.Game)
	if info == nil {
		return nil, fmt.Errorf("unknown game %q", input.Game)
	}
	schema := &Schema{
		Game: input.Game, Version: input.Version, Sources: map[Source]Origin{}, Databases: map[string]*Database{},
	}

	// a database's entry block is a fields block unless its folder row's Body says what the entry
	// is (a Body of value or several words states nothing); each of its Refs names its entries
	declare := func(name string, row game.Folder) {
		database := &Database{Name: name, Entry: &Key{}, Naming: row}
		switch {
		case slices.Equal(row.Body, []string{"effect"}):
			Set(&database.Entry.Block, BlockEffects, SourceRegistry)
		case slices.Equal(row.Body, []string{"trigger"}):
			Set(&database.Entry.Block, BlockTriggers, SourceRegistry)
		case len(row.Body) == 0:
			Set(&database.Entry.Block, BlockFields, SourceRegistry)
		}
		for _, at := range row.Refs {
			position := database.Entry
			for _, step := range strings.Split(at, "/") {
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

	// the databases a call by each CalledIn word resolves in, in name order; "*" any CalledIn (D-29)
	schema.Callees = map[string][]string{}
	for _, name := range slices.Sorted(maps.Keys(schema.Databases)) {
		if called := schema.Databases[name].Naming.CalledIn; len(called) > 0 {
			for _, word := range append(slices.Clone(called), "*") {
				schema.Callees[word] = append(schema.Callees[word], name)
			}
		}
	}

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

	// 3. docs: a doc is read into its own folder's database; one under a row above its folder is not
	for _, file := range input.Install {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if file.Format != source.Info {
			continue
		}
		at, name := schema.folder(file.Rel)
		database := schema.Databases[name]
		if database == nil || at != path.Dir(file.Rel) {
			continue
		}
		text, err := file.Text()
		if err != nil {
			continue
		}
		unread := schema.template(text, strings.EqualFold(path.Ext(file.Rel), ".md"), database.Entry, SourceDocs, "")
		schema.Unread = append(schema.Unread, unread...)
	}
	schema.Sources[SourceDocs] = Origin{From: "install", Written: input.Updated}

	// 4. wiki: each database's own named pages, read as a doc's example is; a history database names
	// none. A sanitized page keeps no version label, so the wiki is never stale (§4.3 rule 2).
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
				var example strings.Builder
				for inner := range node.Descendants() {
					if inner.Type == html.TextNode {
						example.WriteString(inner.Data)
					}
				}
				unread := schema.template(example.String(), false, schema.Databases[name].Entry, SourceWiki, "")
				schema.Unread = append(schema.Unread, unread...)
			}
		}
	}

	// 5. each database's Fields (D-21), once the declared sources are read; vanilla states none
	for _, database := range schema.Databases {
		database.Fields = map[string]bool{}
		var gather func(key *Key)
		gather = func(key *Key) {
			for name, child := range key.Keys {
				if child.Stated() {
					database.Fields[name] = true
				}
				gather(child)
			}
		}
		gather(database.Entry)
	}

	// 6. vanilla: workers parse ahead (not tables or sound lists); Entries and observe run in file
	// order here, and every database Entries names is made
	var files []source.File
	for _, file := range input.Install {
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
		}
		schema.observe(entries)
	})
	if err != nil {
		return nil, err
	}
	return schema, nil
}
