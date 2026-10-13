// build.go is the pipeline: Build makes one game version's schema, each source in turn. Declaring the databases
// lives here.

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
	Install       []inventory.File                                                 // the install's inventory, in load order
	Dumps         []inventory.File                                                 // the dump folders' inventory
	Wiki          func(databases []string) (map[string][]string, time.Time, error) // the wiki's script examples by database and its sidecar's time; nil for none
	Updated       time.Time                                                        // game.InstallUpdatedAt
	LocKeys       map[string]bool                                                  // the install's localization keys, english and the workspace language
	DumpsOnly     bool                                                             // stop after the databases and dumps: the shipped schema's part
	Shipped       *Schema                                                          // a shipped dump part, taken in place of reading the dump files
}

// Build reads every source into the schema. A claim keeps its highest-ranked source (Claim.Set), so the steps
// follow their data, not the ranks: the databases first, since every step reads them; the docs parsed before
// vanilla, since the entry detector reads the keys they write; vanilla whole before its types are inferred; the
// docs' claims merged after vanilla, since they merge only where vanilla writes. No two positions share a
// subtree, since the cache writes a shared pointer once per reference (a ScriptedEntry names its database).
// Every string written into a map or a position is cloned, so the schema never pins a file's text.
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
	s.stampConstructs()                  // 3: the constructs' shapes under the commands that hold them
	docs, err := s.parseDocs(ctx, input) // 4: the docs and the wiki's examples parsed, every key they write noted
	if err != nil {
		return nil, err
	}
	names, err := s.countVanilla(ctx, input.Install, info) // 5: what vanilla writes, counted
	if err != nil {
		return nil, err
	}
	s.inferTypesAndPrefixes(names, input.LocKeys) // 6: a kind where nothing declares one; each prefix's database
	s.mergeDocs(docs)                             // 7: the docs' claims, merged where vanilla writes
	return s, nil
}

// stampConstructs is each construct row that names a command claiming, for the registry, the shape the
// key's block holds at the position under that command's arguments (random_list's <number> branches hold
// effects, switch's <any> cases hold the enclosing shape), made if missing.
func (schema *Schema) stampConstructs() {
	for _, row := range game.Constructs {
		if command := schema.Commands[row.Command]; row.Command != "" && command != nil {
			command.Args.EnsureChild(row.Key).Holds.Set(row.Holds, SourceRegistry)
		}
	}
}

// declareDatabase makes a database. Its entry block is a fields block unless its descriptor's Body says what
// the entry holds; each of the descriptor's Refs names the entries of its database, the database's own where it
// says none.
func (schema *Schema) declareDatabase(name string, row game.Descriptor) {
	database := &Database{Name: name, Entry: &Position{}, Descriptor: row, Fields: map[string]bool{}}
	database.Entry.Holds.Set(cmp.Or(row.Body, game.ShapeFields), SourceRegistry)
	for _, ref := range row.Refs {
		position := database.Entry
		for step := range strings.SplitSeq(ref.At, "/") {
			position = position.EnsureChild(step)
		}
		position.Value.Set(Expected{Type: ValueReference, Database: cmp.Or(ref.Database, name)}, SourceRegistry)
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
			row, _ := info.At(at)
			row.Database, row.Path = name, at
			schema.declareDatabase(name, row)
		}
	}
	for _, row := range info.Descriptors {
		switch {
		case row.Parent != "" && row.Of != "":
			schema.declareDatabase(row.Database, row) // a nested database that sets up entries defined elsewhere
		case row.Path == "" && row.Parent == "":
			extension, _, _ := strings.Cut(row.Database, ".") // a file type's own database (gui.type), with the type's Body
			row.Body = game.FileTypes["."+extension].Body
			schema.declareDatabase(row.Database, row)
		}
	}

	// the databases a call by each caller word resolves in, in name order; AnyCall every scripted folder's (a
	// file type's own database answers only its own words)
	schema.Callees = map[string][]string{}
	for _, name := range slices.Sorted(maps.Keys(schema.Databases)) {
		callers := schema.Databases[name].Descriptor.Callers
		if len(callers) > 0 && !strings.Contains(name, ".") {
			callers = append(slices.Clone(callers), game.AnyCall)
		}
		for _, word := range callers {
			schema.Callees[word] = append(schema.Callees[word], name)
		}
	}
	schema.Lists = cmp.Or(schema.DatabasesWhere(func(row game.Descriptor) bool { return row.Purpose == game.PurposeLists })...)
	schema.Events = cmp.Or(schema.DatabasesWhere(func(row game.Descriptor) bool { return row.Purpose == game.PurposeEvents })...)
	schema.Functions = cmp.Or(schema.DatabasesWhere(func(row game.Descriptor) bool { return row.Purpose == game.PurposeFunctions })...)
}
