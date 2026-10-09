// build.go builds the schema: one function per source, in trust order, each stating what it reads, what it
// writes and why it comes where it does.

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

// example is a doc or wiki example, normalized and parsed once, with the database it documents.
type example struct {
	database string
	parsed   jomini.Result
	from     Source
}

// Build reads every source into the schema, in trust order: the databases the install holds, the dumps, the
// docs' and wiki's key names, vanilla and the value types it gives, then the docs' and wiki's claims. A claim
// keeps the first source that states it (SetFirst). No two positions share a subtree, since the cache writes
// a shared pointer once per reference (a ScriptedEntry names its database). Every string written into a map
// or a position is cloned, so the schema never pins a file's text.
func Build(ctx context.Context, input Input) (*Schema, error) {
	info := game.Get(input.Game)
	if info == nil {
		return nil, fmt.Errorf("unknown game %q", input.Game)
	}
	s := &Schema{
		Game: input.Game, Version: input.Version, Sources: map[Source]SourceState{}, Databases: map[string]*Database{},
	}
	s.declareDatabases(input.Install, info)
	s.readDumpsOrShipped(input)
	if input.DumpsOnly {
		return s, nil
	}
	examples, err := s.parseDocsAndExamples(ctx, input)
	if err != nil {
		return nil, err
	}
	s.collectDocFields(examples)
	names, err := s.recordVanilla(ctx, input.Install, info)
	if err != nil {
		return nil, err
	}
	s.inferTypesAndPrefixes(names, input.LocKeys)
	for _, written := range examples {
		s.applyDocClaims(&written.parsed, s.Databases[written.database], written.from)
	}
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
func (s *Schema) declare(name string, row game.Folder) {
	database := &Database{Name: name, Entry: &Position{}, Row: row, Fields: map[string]bool{}}
	SetFirst(&database.Entry.Block, cmp.Or(row.Body, game.BlockFields), SourceRegistry)
	for _, at := range row.Refs {
		position := database.Entry
		for step := range strings.SplitSeq(at, "/") {
			position = position.EnsureChild(step)
		}
		SetFirst(&position.Value, Expected{Type: WordReference, Database: name}, SourceRegistry)
	}
	s.Databases[name] = database
}

// declareDatabases declares the databases the install's folders and the game's rows make, then the calls
// that resolve in them. It comes first because every later step reads databases.
func (s *Schema) declareDatabases(install []inventory.File, info *game.GameInfo) {
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
		if at, name := s.folder(rel); at != "." && s.Databases[name] == nil {
			s.declare(name, info.Folders[at])
		}
	}

	// a nested row that sets up entries defined elsewhere makes a history database of its keys
	for _, row := range info.Nested {
		if row.Of != "" {
			s.declare(NameOf(row.Parent)+"."+row.Database, game.Folder{History: true, Of: row.Of})
		}
	}

	// a row named after a file type's own database (gui.type) declares it, with the type's Body
	for at, row := range info.Folders {
		if extension, _, cut := strings.Cut(at, "."); cut && game.FileTypes["."+extension].Body != 0 {
			row.Body = game.FileTypes["."+extension].Body
			s.declare(at, row)
		}
	}

	// the databases a call by each CalledIn word resolves in, in name order; AnyCall every scripted folder's
	// (a file type's own database answers only its own words)
	s.Callees = map[string][]string{}
	for _, name := range slices.Sorted(maps.Keys(s.Databases)) {
		called := s.Databases[name].Row.CalledIn
		if len(called) > 0 && !strings.Contains(name, ".") {
			called = append(slices.Clone(called), game.AnyCall)
		}
		for _, word := range called {
			s.Callees[word] = append(s.Callees[word], name)
		}
	}
	s.Lists = cmp.Or(s.DatabasesWhere(func(row game.Folder) bool { return row.Iterated })...)
}

// readDumpsOrShipped reads the dump files into the schema, or takes a shipped dump part whole. The dumps
// are stale when older than the install's last update.
func (s *Schema) readDumpsOrShipped(input Input) {
	if shipped := input.Shipped; shipped != nil {
		s.Commands, s.Scopes, s.Saved = shipped.Commands, shipped.Scopes, shipped.Saved
		s.Modifiers, s.OnActions = shipped.Modifiers, shipped.OnActions
		s.DataTypes, s.CustomLoc = shipped.DataTypes, shipped.CustomLoc
		s.Sources[SourceDumps] = shipped.Sources[SourceDumps]
		return
	}
	written := s.readDumps(input.Dumps)
	s.Sources[SourceDumps] = SourceState{From: "none"}
	if !written.IsZero() {
		s.Sources[SourceDumps] = SourceState{From: "yours", Written: written, Stale: written.Before(input.Updated)}
	}
}

// parseDocsAndExamples normalizes and parses each doc and wiki example once, with its database. A doc is
// read into its own folder's database, or the folder beside it that it is named after
// (history/_characters.info); one under a row above its folder is not read. The wiki's examples follow the
// docs', so a claim keeps the first source in trust order. A sanitized wiki page keeps no version label, so
// the wiki is never stale.
func (s *Schema) parseDocsAndExamples(ctx context.Context, input Input) (examples []example, err error) {
	for _, file := range input.Install {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if file.Format != inventory.Prose {
			continue
		}
		at, name := s.folder(file.Rel)
		stem := strings.TrimPrefix(strings.TrimSuffix(path.Base(file.Rel), path.Ext(file.Rel)), "_")
		if beside := NameOf(path.Join(path.Dir(file.Rel), stem)); s.Databases[beside] != nil {
			at, name = path.Dir(file.Rel), beside
		}
		if s.Databases[name] == nil || at != path.Dir(file.Rel) {
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
		examples = append(examples, example{name, jomini.Parse(normalize(lines)), SourceDocs})
	}
	s.Sources[SourceDocs] = SourceState{From: "install", Written: input.Updated}

	s.Sources[SourceWiki] = SourceState{From: "none"}
	if !input.WikiWritten.IsZero() {
		s.Sources[SourceWiki] = SourceState{From: "yours", Written: input.WikiWritten}
	}
	for _, name := range slices.Sorted(maps.Keys(s.Databases)) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if s.Databases[name].Row.History {
			continue
		}
		for _, text := range input.WikiExamples[name] {
			examples = append(examples, example{name, jomini.Parse(normalize(strings.Split(text, "\n"))), SourceWiki})
		}
	}
	return examples, nil
}

// collectDocFields records in each database every key name its docs and wiki examples write, at any depth,
// for the entry detector.
func (s *Schema) collectDocFields(examples []example) {
	for _, written := range examples {
		fields := s.Databases[written.database].Fields
		jomini.Walk(written.parsed.Root, func(statement jomini.Statement, _ int, _ *jomini.Block) bool {
			if assign, isAssign := statement.(*jomini.Assignment); isAssign {
				for _, name := range alternatives(assign.Key.Text) {
					fields[positionName(name)] = true
				}
			}
			return true
		})
	}
}

// registerScriptedEntries records a scripted entry vanilla defines, under each word its database is called
// by. A database that takes parameters gives the entry its own parameters position; one that takes none
// reads at its database's entry, so no entry tree is held twice.
func (s *Schema) registerScriptedEntries(entry Entry) {
	database := s.Databases[entry.Database]
	if database == nil || len(database.Row.CalledIn) == 0 {
		return
	}
	called := ScriptedEntry{Database: database.Name}
	if database.Row.Parameters {
		called.Parameters = &Position{}
	}
	words := database.Row.CalledIn
	if !strings.Contains(database.Name, ".") {
		words = append(slices.Clone(words), game.AnyCall)
	}
	for _, word := range words {
		s.ScriptedEntries[word+":"+strings.Clone(entry.Name)] = called
	}
}

// parseScript parses a script file; an unreadable one parses as empty.
func parseScript(file inventory.File) jomini.Result {
	text, err := file.Text()
	if err != nil {
		return jomini.Parse("")
	}
	return jomini.Parse(text)
}

// recordVanilla reads the install's files for the entries they define and what they write. Scripted entries
// come first, from the scripted folders and the GUI files alone, so the walk reads their names as calls
// rather than as arguments. It returns the entry names vanilla defines, by database; a database made for a
// new name is declared on the way.
func (s *Schema) recordVanilla(ctx context.Context, install []inventory.File, info *game.GameInfo) (map[string]map[string]bool, error) {
	s.ScriptedEntries = map[string]ScriptedEntry{}
	names := map[string]map[string]bool{}
	parsedFirst := map[string]jomini.Result{}
	var files []inventory.File
	for _, file := range install {
		at, _ := s.folder(file.Rel)
		gui := game.FileTypes[strings.ToLower(path.Ext(file.Rel))].Keywords != nil
		if file.Format == inventory.Script && (len(info.Folders[at].CalledIn) > 0 || gui) {
			parsed := parseScript(file)
			for _, entry := range s.FindEntries(file, &parsed) {
				s.registerScriptedEntries(entry)
			}
			parsedFirst[file.Path] = parsed
		}
		if slices.Contains([]inventory.Format{inventory.Script, inventory.Table, inventory.List}, file.Format) {
			files = append(files, file)
		}
	}
	work := func(file inventory.File) (parsed jomini.Result) {
		if first, found := parsedFirst[file.Path]; found {
			return first
		}
		if file.Format == inventory.Script {
			return parseScript(file)
		}
		return parsed
	}
	keep := func(file inventory.File, result jomini.Result) {
		var parsed *jomini.Result
		if file.Format == inventory.Script {
			parsed = &result
		}
		entries := s.FindEntries(file, parsed)
		for _, entry := range entries {
			s.noteEntry(entry, names)
		}
		s.recordEvidence(file, parsed, entries)
	}
	return names, inventory.Ordered(ctx, files, work, keep)
}

// noteEntry makes the database of an entry that has none (a file type's database holds its Body: gui.x),
// records the name a named entry defines, and registers a scripted entry declared inline.
func (s *Schema) noteEntry(entry Entry, names map[string]map[string]bool) {
	if s.Databases[entry.Database] == nil {
		extension, _, _ := strings.Cut(entry.Database, ".")
		s.declare(entry.Database, game.Folder{Body: game.FileTypes["."+extension].Body})
	}
	if entry.Name != "" {
		if names[entry.Database] == nil {
			names[entry.Database] = map[string]bool{}
		}
		names[entry.Database][strings.Clone(entry.Name)] = true
	}
	if entry.Local {
		s.registerScriptedEntries(entry)
	}
}

// inferTypesAndPrefixes gives positions the type vanilla's words share and each link prefix the database of
// the names written after it. A prefix names the database of the names script makes (the name commands'
// first), then the one vanilla's words show, then the one its own name names. It comes before any docs claim,
// which fill only what is left.
func (s *Schema) inferTypesAndPrefixes(names map[string]map[string]bool, locKeys map[string]bool) {
	s.Prefixes = map[string]string{}
	for _, row := range game.NameCommands {
		s.Prefixes[row.Database] = row.Database
	}
	s.inferValueTypes(names, locKeys)
	for name := range s.Commands {
		if s.Link(name) != nil && s.Prefixes[name] == "" {
			s.Prefixes[name] = s.DatabaseNamed(name)
		}
	}
}
