// build.go is the pipeline: Build makes one game version's schema in seven steps. Step 1, declaring the
// databases, lives here.

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
)

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

// Build reads every source into the schema. A claim keeps its highest-ranked source (Claim.Set), so the steps
// follow their data, not the ranks: the databases first, since every step reads them; the docs' key names
// before vanilla, for the entry detector; vanilla whole before its types are inferred; the docs' claims after
// vanilla, since they merge only where vanilla writes. No two positions share a subtree, since the cache
// writes a shared pointer once per reference (a ScriptedEntry names its database). Every string written into
// a map or a position is cloned, so the schema never pins a file's text.
func Build(ctx context.Context, input Input) (*Schema, error) {
	info := game.Get(input.Game)
	if info == nil {
		return nil, fmt.Errorf("unknown game %q", input.Game)
	}
	s := &Schema{
		Game: input.Game, Version: input.Version, Sources: map[Source]SourceState{}, Databases: map[string]*Database{},
	}
	s.declareDatabases(input.Install, info) // 1: the registry's rows for the folders the install holds
	s.dumpsOrShipped(input)                 // 2: the dumps parsed, or the shipped backup
	if input.DumpsOnly {
		return s, nil
	}
	examples, err := s.parseDocsAndExamples(ctx, input) // 3: the docs and the wiki's examples, parsed
	if err != nil {
		return nil, err
	}
	s.collectDocFields(examples)                           // 4: every key the docs write, for the entry detector
	names, err := s.countVanilla(ctx, input.Install, info) // 5: what vanilla writes, counted
	if err != nil {
		return nil, err
	}
	s.inferTypesAndPrefixes(names, input.LocKeys) // 6: a kind where nothing declares one; each prefix's database
	s.applyExamples(examples)                     // 7: the docs' claims, filling what is left
	return s, nil
}

// DatabaseNames returns the names of the databases Build will declare for an input: what the install's
// folders and the game's rows make, before any source is read. The scan asks the wiki for examples by them.
func DatabaseNames(input Input) []string {
	info := game.Get(input.Game)
	if info == nil {
		return nil
	}
	s := &Schema{Game: input.Game, Databases: map[string]*Database{}}
	s.declareDatabases(input.Install, info)
	return slices.Sorted(maps.Keys(s.Databases))
}

// declare makes a database. Its entry block is a fields block unless its folder row's Body says what the
// entry is; each of the row's Refs names the database's entries.
func (schema *Schema) declare(name string, row game.Folder) {
	database := &Database{Name: name, Entry: &Position{}, Row: row, Fields: map[string]bool{}}
	database.Entry.Block.Set(cmp.Or(row.Body, game.BlockFields), SourceRegistry)
	for _, at := range row.Refs {
		position := database.Entry
		for step := range strings.SplitSeq(at, "/") {
			position = position.EnsureChild(step)
		}
		position.Value.Set(Expected{Kind: ExpectsReference, Database: name}, SourceRegistry)
	}
	schema.Databases[name] = database
}

// declareDatabases declares the databases the install's folders and the game's rows make, then the calls
// that resolve in them. It comes first because every later step reads databases.
func (schema *Schema) declareDatabases(install []inventory.File, info *game.GameInfo) {
	// each script file's folder is a database; a file at the root belongs to none, and one whose file type
	// names its own entries (.asset, .gui) makes its folder no database. Shallow folders first, so a common/
	// subfolder finds its ancestor's database.
	var scripts []string
	for _, file := range install {
		fileType, typed := game.FileTypes[strings.ToLower(path.Ext(file.Rel))]
		if file.Format == inventory.Script && typed && fileType.NameKey == "" && fileType.Keywords == nil {
			scripts = append(scripts, file.Rel)
		}
	}
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
			schema.declare(NameOf(row.Parent)+"."+row.Database, game.Folder{History: true, Of: row.Of})
		}
	}

	// a row named after a file type's own database (gui.type) declares it, with the type's Body
	for at, row := range info.Folders {
		if extension, _, cut := strings.Cut(at, "."); cut && game.FileTypes["."+extension].Body != 0 {
			row.Body = game.FileTypes["."+extension].Body
			schema.declare(at, row)
		}
	}

	// the databases a call by each CalledIn word resolves in, in name order; AnyCall every scripted folder's
	// (a file type's own database answers only its own words)
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
