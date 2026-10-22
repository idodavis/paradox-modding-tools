package index

import (
	"fmt"
	"maps"
	"runtime"
	"slices"
	"strings"
	"sync"

	"github.com/samber/lo"
	"golang.org/x/sync/errgroup"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/loc"
	"paradox-modding-tools/internal/schema"
)

// A collector is the scan's visitor. It turns the definition and the references of each statement into
// occurrences.
type collector struct {
	index       *Index
	file        int32
	occurrences []Occurrence
	definitions []int32 // the positions of the definitions the walk is inside, innermost last
}

// Scan builds an index of every file under the given roots, in parallel. A script, GUI or asset file goes through
// the schema's walk with the collecting visitor, and a localization file through the loc parser. The roots are
// the game's, then each DLC's, then each mod's, in load order, and the progress function is called at each stage.
// A DLC's folder is a root inside the game's, and a file under both is the DLC's alone.
func Scan(game *game.Game, schema *schema.Schema, roots []files.Root, progress func(stage string)) (*Index, error) {
	index, err := scanRoots(schema, nil, roots, progress)
	if err != nil {
		return nil, err
	}
	index.Game = game.ID
	return index, nil
}

// ScanMods builds the index of a workspace's mods on top of the install's index, so that a lookup in the mods'
// index falls through to the install's. The replaced folders are given per root, as each mod's replace_path lists
// them. The index records every language the mods write, even one the install does not.
func ScanMods(game *game.Game, schema *schema.Schema, install *Index, roots []files.Root, replaced [][]string) (*Index, error) {
	index, err := scanRoots(schema, install, roots, func(string) {})
	if err != nil {
		return nil, err
	}
	index.Game, index.Replaced = game.ID, slices.Concat(make([][]string, len(install.Roots)), replaced)
	return index, nil
}

// ScanFile walks one file again after it changed and replaces its occurrences and postings. A nil tree means the
// file was deleted, and its occurrences are dropped.
func (index *Index) ScanFile(schema *schema.Schema, file files.File, tree *jomini.Tree) error {
	position, known := index.FindFile(file.Path)
	if known && position < index.countInstallFiles() {
		return fmt.Errorf("%s is a file of the install", file.Path)
	}
	gone := tree == nil
	if !known && gone {
		return nil
	}
	if !known {
		// a file gone before keeps its place, which a file of the same path takes again
		slot := slices.IndexFunc(index.Files, func(other files.File) bool { return other.Key == file.Key })
		if slot < 0 {
			index.Files = append(index.Files, file)
			index.Occurrences = append(index.Occurrences, nil)
			index.Texts = append(index.Texts, nil)
			slot = len(index.Files) - 1
		}
		position = int32(slot)
	}
	index.Occurrences[position], index.Texts[position] = nil, nil
	if gone {
		delete(index.byKey, file.Key)
	} else {
		index.buildFile(&sync.RWMutex{}, schema, position, tree)
		index.byKey[file.Key] = position
	}
	index.buildPostings()
	return nil
}

// scanRoots lists the files under the roots and builds the index of them, over the install's index when one is
// given. The progress function is told each stage. It returns an error if a root cannot be walked.
func scanRoots(schema *schema.Schema, install *Index, roots []files.Root, progress func(stage string)) (*Index, error) {
	progress("listing")
	listing, err := listFiles(roots)
	if err != nil {
		return nil, err
	}
	index := newIndex(schema, install, roots, listing)
	progress("indexing")
	index.scanFiles(schema)
	progress("postings")
	index.buildPostings()
	return index, nil
}

// listFiles lists the files under the roots, root by root in the order given. It returns an error if a root cannot
// be walked.
func listFiles(roots []files.Root) ([]files.File, error) {
	keys := listRootKeys(roots)
	var listing []files.File
	for position, root := range roots {
		list, err := files.Walk(root, nil)
		if err != nil {
			return nil, err
		}
		// a file under a root inside this one belongs to the inner root
		listing = append(listing, lo.Filter(list, func(file files.File, _ int) bool {
			owner, _ := findRoot(keys, file)
			return owner == position
		})...)
	}
	return listing, nil
}

// listRootKeys returns the key of each root's folder, ending in the slash that opens the paths under it.
func listRootKeys(roots []files.Root) []string {
	return lo.Map(roots, func(root files.Root, _ int) string { return files.ToKey(root.Dir) + "/" })
}

// findRoot returns the position of the innermost root that holds a file, given the keys of the roots' folders. It
// returns false if no root holds the file.
func findRoot(keys []string, file files.File) (int, bool) {
	position, length := -1, 0
	for candidate, key := range keys {
		if len(key) > length && strings.HasPrefix(file.Key, key) {
			position, length = candidate, len(key)
		}
	}
	return position, position >= 0
}

// newIndex returns an index of the given files with nothing found yet. Over an install's index it starts with the
// install's files, roots and what they hold, and copies of its tables, so that a file's position and an ordinal
// mean the same in both.
func newIndex(schema *schema.Schema, install *Index, roots []files.Root, listing []files.File) *Index {
	index := &Index{Files: listing, Roots: roots, install: install}
	var occurrences [][]Occurrence
	var texts [][]string
	if install != nil {
		index.Files, index.Roots = slices.Concat(install.Files, listing), slices.Concat(install.Roots, roots)
		occurrences, texts = install.Occurrences, install.Texts
	}
	index.Occurrences = slices.Concat(occurrences, make([][]Occurrence, len(listing)))
	index.Texts = slices.Concat(texts, make([][]string, len(listing)))
	for name, database := range schema.Databases {
		if database.IsCaseless() {
			index.Caseless = append(index.Caseless, name)
		}
	}
	slices.Sort(index.Caseless)
	if install == nil {
		index.buildTables()
	} else {
		from := install.listTables()
		for position, table := range index.listTables() {
			*table = Table{Texts: slices.Clone(from[position].Texts), ordinals: maps.Clone(from[position].ordinals)}
		}
	}
	index.buildPaths()
	return index
}

// listTables returns the six tables of the index.
func (index *Index) listTables() []*Table {
	return []*Table{&index.Databases, &index.Names, &index.Callers, &index.Modes, &index.Languages, &index.Scopes}
}

// buildTables makes every table of the index find its texts, and puts the empty text at ordinal 0 of a table that
// has none yet.
func (index *Index) buildTables() {
	for _, table := range index.listTables() {
		table.ordinals = make(map[string]uint32, len(table.Texts))
		for ordinal, text := range table.Texts {
			table.ordinals[text] = uint32(ordinal)
		}
		table.Intern("")
	}
}

// buildPaths fills the map that finds a file by its key.
func (index *Index) buildPaths() {
	index.byKey = make(map[string]int32, len(index.Files))
	for position, file := range index.Files {
		index.byKey[file.Key] = int32(position)
	}
}

// scanFiles builds the occurrences of every file of the index's own, in parallel on every core but two.
func (index *Index) scanFiles(schema *schema.Schema) {
	var group errgroup.Group
	var lock sync.RWMutex
	group.SetLimit(max(runtime.NumCPU()-2, 1))
	for position := int(index.countInstallFiles()); position < len(index.Files); position++ {
		group.Go(func() error {
			index.buildFile(&lock, schema, int32(position), nil)
			return nil
		})
	}
	_ = group.Wait() // no file's task fails: one that cannot be read has no occurrences
}

// buildFile builds the occurrences of one file, and the texts of a localization file's messages. A format that has
// no occurrences, and a file that cannot be read, get none.
func (index *Index) buildFile(lock *sync.RWMutex, schema *schema.Schema, position int32, tree *jomini.Tree) {
	// The file is built against tables of its own, so that building takes no lock; its ordinals move to the index's
	// tables afterwards, which takes the lock a few times for the whole file, not once for each occurrence.
	tables := index.makeTables()
	var occurrences []Occurrence
	var texts []string
	switch index.Files[position].Format {
	case files.FormatLocalization:
		occurrences, texts = tables.buildMessages(schema, position, index.readSource(position, tree))
	case files.FormatTable:
		occurrences = tables.buildRows(schema, position, index.readSource(position, tree))
	case files.FormatScript, files.FormatGUI, files.FormatAsset, files.FormatFont, files.FormatGFX:
		if tree == nil {
			tree = jomini.Parse(index.readSource(position, nil))
		}
		visitor := &collector{index: tables, file: position}
		schema.Walk(tree, index.Files[position], visitor)
		occurrences = visitor.occurrences
	default:
		return
	}
	index.Occurrences[position], index.Texts[position] = occurrences, texts
	index.internFile(lock, tables, occurrences)
}

// readSource returns the text of the file at a position: its tree's, if it has one, else what the file holds. A
// file that cannot be read gives "".
func (index *Index) readSource(position int32, tree *jomini.Tree) string {
	if tree != nil {
		return tree.Src
	}
	text, _ := files.ReadText(index.Files[position].Path)
	return text.Text
}

// makeTables returns an index that has the same files and caseless databases as this one and tables of its own.
func (index *Index) makeTables() *Index {
	tables := &Index{Files: index.Files, Caseless: index.Caseless}
	tables.buildTables()
	return tables
}

// Enter records the statement's definition and references.
func (collector *collector) Enter(tree *jomini.Tree, statement int32, context *schema.Context) bool {
	index := collector.index
	parent := int32(-1)
	if open := collector.definitions; len(open) > 0 {
		parent = open[len(open)-1]
	}
	if definition := context.Definition; context.Defines {
		occurrence := index.makeOccurrence(RoleDefines, definition.Database, definition.Name, definition.Span)
		occurrence.Mode = uint8(index.Modes.Intern(definition.Mode))
		occurrence.Priority = int16(definition.Priority)
		occurrence.Local = definition.Local
		occurrence.Scope = uint8(index.Scopes.Intern(context.Root))
		occurrence.Line = int32(tree.Lines.ToPosition(definition.Span.Start).Line)
		occurrence.Parent = parent
		parent = int32(len(collector.occurrences))
		collector.occurrences = append(collector.occurrences, occurrence)
		collector.definitions = append(collector.definitions, parent)
	}
	for _, reference := range context.References {
		occurrence := index.makeOccurrence(RoleRefers, reference.Database, reference.Name, reference.Span)
		occurrence.Caller = uint8(index.Callers.Intern(reference.Caller))
		occurrence.Scope = uint8(index.Scopes.Intern(context.Scope))
		occurrence.Line = int32(tree.Lines.ToPosition(reference.Span.Start).Line)
		occurrence.Parent = parent
		collector.occurrences = append(collector.occurrences, occurrence)
	}
	return true
}

// Leave closes the definition that the statement makes, if it makes one. A definition named by its file is
// written by no statement, so it stays open to the end of the file.
func (collector *collector) Leave(tree *jomini.Tree, statement int32, context *schema.Context) {
	if context.Defines && !context.Definition.Span.IsEmpty() {
		collector.definitions = collector.definitions[:len(collector.definitions)-1]
	}
}

// internFile moves the ordinals of a file's occurrences from the tables the file was built against to the index's
// own tables. A file with no occurrences takes no lock.
func (index *Index) internFile(lock *sync.RWMutex, tables *Index, occurrences []Occurrence) {
	if len(occurrences) == 0 {
		return
	}
	databases := internAll(lock, &index.Databases, tables.Databases.Texts)
	names := internAll(lock, &index.Names, tables.Names.Texts)
	callers := internAll(lock, &index.Callers, tables.Callers.Texts)
	modes := internAll(lock, &index.Modes, tables.Modes.Texts)
	languages := internAll(lock, &index.Languages, tables.Languages.Texts)
	scopes := internAll(lock, &index.Scopes, tables.Scopes.Texts)
	for position := range occurrences {
		occurrence := &occurrences[position]
		occurrence.Database = uint16(databases[occurrence.Database])
		occurrence.Name = names[occurrence.Name]
		occurrence.Caller = uint8(callers[occurrence.Caller])
		occurrence.Mode = uint8(modes[occurrence.Mode])
		occurrence.Language = uint8(languages[occurrence.Language])
		occurrence.Scope = uint8(scopes[occurrence.Scope])
	}
}

// internAll returns the ordinal in a table of each of the given texts, adding the ones the table lacks.
func internAll(lock *sync.RWMutex, table *Table, texts []string) []uint32 {
	// Most texts are in the table already, so they are looked up under a shared lock, and only the missing ones are
	// added under the whole lock. An added text is a copy, so that it does not keep its file's text alive.
	ordinals := make([]uint32, len(texts))
	var missing []int
	lock.RLock()
	for position, text := range texts {
		if ordinal, ok := table.Lookup(text); ok {
			ordinals[position] = ordinal
		} else {
			missing = append(missing, position)
		}
	}
	lock.RUnlock()
	if len(missing) == 0 {
		return ordinals
	}
	lock.Lock()
	defer lock.Unlock()
	for _, position := range missing {
		ordinals[position] = table.Intern(strings.Clone(texts[position]))
	}
	return ordinals
}

// makeOccurrence returns an occurrence of the given role, database and name written at the given span, at the top
// of its file until its parent is set.
func (index *Index) makeOccurrence(role Role, database, name string, span jomini.Span) Occurrence {
	return Occurrence{
		Span: span, Database: uint16(index.Databases.Intern(database)),
		Name: index.Names.Intern(index.toCaseless(database, name)), Role: role, Parent: -1,
	}
}

// toCaseless returns a text as the index stores it for a database: in lower case where the database's names match
// without regard to case, else as it is.
func (index *Index) toCaseless(database, text string) string {
	if slices.Contains(index.Caseless, database) {
		return strings.ToLower(text)
	}
	return text
}

// buildMessages builds the occurrences of a localization file: each message, followed by what its text refers to.
// The texts run parallel to the occurrences.
func (index *Index) buildMessages(schema *schema.Schema, position int32, source string) ([]Occurrence, []string) {
	file := loc.Parse(source, index.Files[position].Path)
	language := uint8(index.Languages.Intern(file.Language))
	// A game's own file keeps its texts in the base language only, and a mod's in every language.
	keep := file.Language == game.BaseLanguage || index.Files[position].Origin != game.OriginVanilla
	var occurrences []Occurrence
	var texts []string
	for _, message := range file.Messages {
		parent := int32(len(occurrences))
		occurrence := index.makeOccurrence(RoleLocalizes, game.Localization, message.Key, message.KeySpan)
		occurrence.Language = language
		occurrence.Line = int32(file.Lines.ToPosition(message.Span.Start).Line)
		occurrences = append(occurrences, occurrence)
		if keep {
			texts = append(texts, strings.Clone(message.Text))
		}
		for _, reference := range listTextReferences(schema.Concepts, message) {
			occurrence := index.makeOccurrence(RoleRefers, reference.Database, reference.Name, reference.Span)
			occurrence.Line = int32(file.Lines.ToPosition(reference.Span.Start).Line)
			occurrence.Parent = parent
			occurrences = append(occurrences, occurrence)
			if keep {
				texts = append(texts, "")
			}
		}
	}
	return occurrences, texts
}

// listTextReferences returns what a message's text refers to: the localization keys written between dollar signs,
// and the game concepts that its data calls name, which are names in the given database.
func listTextReferences(concepts string, message loc.Message) []schema.Reference {
	var references []schema.Reference
	for _, segment := range loc.Segments(message.Text, message.TextSpan.Start) {
		switch segment.Markup {
		case loc.MarkupKey:
			start := segment.Span.Start + 1
			span := jomini.Span{Start: start, End: start + len(segment.Text)}
			references = append(references, schema.Reference{Database: game.Localization, Name: segment.Text, Span: span})
		case loc.MarkupCall:
			if call, ok := jomini.ParseCall(segment.Text, segment.Span.Start+1); ok && concepts != "" {
				references = append(references, listConcepts(concepts, call)...)
			}
		}
	}
	return references
}

// listConcepts returns the game concepts that a data call of a localization text names, as references to the
// given database. A call of one step with no arguments and the concept format after its bar names its step. A step
// written as the concept call names its first quoted argument.
func listConcepts(database string, call jomini.Call) []schema.Reference {
	onlyStep := len(call.Steps) == 1 && len(call.Steps[0].Arguments) == 0
	if onlyStep && strings.EqualFold(call.Suffix, game.ConceptCalls.Format) {
		return []schema.Reference{{Database: database, Name: call.Steps[0].Text, Span: call.Steps[0].Span}}
	}
	var references []schema.Reference
	for _, step := range call.Steps {
		if step.Text != game.ConceptCalls.Call {
			continue
		}
		position := slices.IndexFunc(step.Arguments, func(argument jomini.Argument) bool { return argument.Quoted })
		if position < 0 {
			continue
		}
		argument := step.Arguments[position]
		span := jomini.Span{Start: argument.Span.Start + 1, End: argument.Span.End - 1}
		references = append(references, schema.Reference{Database: database, Name: argument.Text, Span: span})
	}
	return references
}

// buildRows builds the occurrences of a table file: a definition for each row, named by the row's first column. A
// table that its database does not name by column has none.
func (index *Index) buildRows(schema *schema.Schema, position int32, source string) []Occurrence {
	database, ok := schema.FindDatabase(index.Files[position].Rel)
	if !ok || database.Row.NamedBy != "column" {
		return nil
	}
	var occurrences []Occurrence
	offset, line := 0, 0
	for row := range strings.Lines(source) {
		name, _, _ := strings.Cut(strings.TrimRight(row, "\r\n"), game.TableSyntax.Separator)
		if name != "" && !strings.HasPrefix(name, game.TableSyntax.Comment) {
			span := jomini.Span{Start: offset, End: offset + len(name)}
			occurrence := index.makeOccurrence(RoleDefines, database.Name, name, span)
			occurrence.Line = int32(line)
			occurrences = append(occurrences, occurrence)
		}
		offset += len(row)
		line++
	}
	return occurrences
}
