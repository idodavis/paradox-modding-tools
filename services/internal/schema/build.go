// build.go is the pipeline: Build makes one game version's schema, each source in turn. Declaring the databases,
// and the one pick of the database that houses each special thing, live here.

package schema

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
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
	Game      string
	Version   string
	Install   []inventory.File // the install's inventory, in load order
	Dumps     []inventory.File // the dump folders' inventory
	Updated   time.Time        // when the install was last updated (game.ReadInstallUpdatedAt)
	LocKeys   map[string]bool  // the install's localization keys, english and the workspace language
	DumpsOnly bool             // stop after the databases and dumps: the shipped schema's part
	Shipped   *Schema          // a shipped dump part, taken in place of reading the dump files

	// Wiki returns the wiki's script examples by database and its sidecar's time; nil for none.
	Wiki func(databases []string) (map[string][]string, time.Time, error)
}

// Build reads every source into the schema in the order its data needs: databases, docs, vanilla, inferred types,
// then the docs' claims where vanilla writes. No two positions share a subtree, and every kept string is cloned.
func Build(ctx context.Context, input Input) (*Schema, error) {
	info := game.Lookup(input.Game)
	if info == nil {
		return nil, fmt.Errorf("unknown game %q", input.Game)
	}
	schema := &Schema{
		Game: input.Game, Version: input.Version, Sources: map[game.Source]SourceState{}, Databases: map[string]*Database{},
	}
	schema.declareDatabases(input.Install, info) // 1: the registry's rows for the folders the install holds
	schema.dumpsOrShipped(input)                 // 2: the dumps parsed, or the shipped backup
	if input.DumpsOnly {
		return schema, nil
	}
	schema.stampKeywords()                    // 3: the keywords' shapes under the commands that hold them
	docs, err := schema.parseDocs(ctx, input) // 4: the docs and the wiki's examples parsed, every key they write noted
	if err != nil {
		return nil, err
	}
	names, err := schema.countVanilla(ctx, input.Install, info) // 5: what vanilla writes, counted
	if err != nil {
		return nil, err
	}
	schema.inferTypesAndPrefixes(names, input.LocKeys) // 6: a kind where nothing declares one; each prefix's database
	schema.mergeDocs(docs)                             // 7: the docs' claims, merged where vanilla writes
	return schema, nil
}

// stampKeywords claims for the registry the shape a keyword's block holds under its command's arguments (random_list's
// <number> branches hold effects), making the position if missing.
func (schema *Schema) stampKeywords() {
	for _, row := range game.Keywords {
		if command := schema.Commands[row.Command]; row.Command != "" && command != nil {
			command.Args.EnsureChild(row.Key).Shape.Set(row.Shape, game.SourceRegistry)
		}
	}
}

// declareDatabase makes a database: its entry holds fields unless its rule's Shape says otherwise, and each of the
// rule's References names entries of its database, or of the database itself where it names none.
func (schema *Schema) declareDatabase(name string, rule game.DatabaseRule) {
	database := &Database{Name: name, Entry: &Position{}, Rule: rule, Fields: map[string]bool{}}
	database.Entry.Shape.Set(cmp.Or(rule.Shape, game.ShapeFields), game.SourceRegistry)
	for _, reference := range rule.References {
		position := database.Entry
		for step := range strings.SplitSeq(reference.KeyPath, "/") {
			position = position.EnsureChild(step)
		}
		position.Value.Set(Expected{Type: ValueReference, Database: cmp.Or(reference.Database, name)}, game.SourceRegistry)
	}
	schema.Databases[name] = database
}

// declareDatabases declares the databases the install's folders and the game's rules make, the calls that resolve in
// them, and the database that houses each special thing (events, lists...). It comes first: later steps read them.
func (schema *Schema) declareDatabases(install []inventory.File, info *game.Game) {
	// each script file's folder is a database; a file at the root belongs to none, and one whose file type
	// names its own entries (.asset, .gui) makes its folder no database. Shallow folders first, so a common/
	// subfolder finds its ancestor's database.
	var scripts []string
	for _, file := range install {
		fileType := game.FindRuleByExtension(strings.ToLower(path.Ext(file.Rel)))
		if file.Format == inventory.Script && fileType.Extension != "" && fileType.NameKey == "" && fileType.DeclaringKeys == nil {
			scripts = append(scripts, file.Rel)
		}
	}
	slices.SortStableFunc(scripts, func(first, second string) int {
		return cmp.Compare(strings.Count(first, "/"), strings.Count(second, "/"))
	})
	for _, rel := range scripts {
		if folder, name := schema.folder(rel); folder != "." && schema.Databases[name] == nil {
			rule, _ := info.FindRule(folder)
			rule.Name, rule.Path = name, folder
			schema.declareDatabase(name, rule)
		}
	}
	folders := map[string]bool{} // every folder the install holds, and every file, as a rule's Path names them
	for _, file := range install {
		folders[file.Rel] = true
		for dir := path.Dir(file.Rel); dir != "."; dir = path.Dir(dir) {
			folders[dir] = true
		}
	}
	for _, rule := range info.Rules {
		switch {
		case rule.Extension != "": // a file type's rule declares no database
		case rule.ParentPath != "" && rule.Target != "":
			schema.declareDatabase(rule.Name, rule) // a nested database that sets up entries defined elsewhere
		case rule.Path == "" && rule.ParentPath == "":
			extension, _, _ := strings.Cut(rule.Name, ".") // a file type's own database (gui.type)
			rule.Shape = game.FindRuleByExtension("." + extension).Shape
			schema.declareDatabase(rule.Name, rule)
		case rule.Path != "" && rule.Name != "" && schema.Databases[rule.Name] == nil && folders[rule.Path]:
			schema.declareDatabase(rule.Name, rule) // a folder with no script file (reader_export): its prose documents it
		}
	}

	// the databases a call by each caller word resolves in, in name order; AnyCall every scripted folder's (a
	// file type's own database answers only its own words)
	schema.Callees = map[string][]string{}
	for _, name := range slices.Sorted(maps.Keys(schema.Databases)) {
		callers := schema.Databases[name].Rule.CallerWords
		if len(callers) > 0 && !strings.Contains(name, ".") {
			callers = append(slices.Clone(callers), game.AnyCall)
		}
		for _, word := range callers {
			schema.Callees[word] = append(schema.Callees[word], name)
		}
	}
	schema.Lists = schema.findHousingDatabase("lists")
	schema.Functions = schema.findHousingDatabase("functions")
	schema.Events = schema.findHousingDatabase("events")
	schema.Concepts = schema.findHousingDatabase("concepts")
}

// findHousingDatabase returns the database that houses a thing among the databases declared, by FindHousingDatabase.
func (schema *Schema) findHousingDatabase(houses string) string {
	rules := make([]game.DatabaseRule, 0, len(schema.Databases))
	for _, database := range schema.Databases {
		rules = append(rules, database.Rule)
	}
	return FindHousingDatabase(rules, houses)
}

// FindHousingDatabase returns the database that houses a thing (events, on_actions, lists...) among rules: the first
// by name, "" where none. Two such databases are logged, and the first stands, so every reader finds the same one.
func FindHousingDatabase(rules []game.DatabaseRule, houses string) string {
	var names []string
	for _, rule := range rules {
		if rule.Houses == houses {
			names = append(names, rule.Name)
		}
	}
	slices.Sort(names)
	names = slices.Compact(names)
	if len(names) > 1 {
		slog.Warn("a houses has several databases", "houses", houses, "databases", names)
	}
	return cmp.Or(names...)
}
