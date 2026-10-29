package index

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

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
	schema      *schema.Schema
	index       *Index
	file        int32
	occurrences []Occurrence
	definitions []int32 // the positions of the definitions the walk is inside, innermost last
}

// Scan builds the index of an install's files, in parallel. The roots are the game's, then each DLC's, in load
// order, and the listing is the files under them, as ListFiles returns. The progress function is called with the
// name of each stage, and again with the percent of the files done while the files are indexed.
func Scan(schema *schema.Schema, roots []files.Root, listing []files.File, progress func(stage string, percent int)) *Index {
	index := newIndex(schema, nil, roots, listing)
	progress("indexing", 0)
	index.scanFiles(schema, func(percent int) { progress("indexing", percent) })
	progress("postings", 0)
	index.buildPostings()
	return index
}

// LoadIndex loads an install's index from its cache file. It returns false if the file is missing or cannot be read.
func LoadIndex(path string) (*Index, bool) {
	index, ok := files.LoadGob[*Index](path)
	if !ok {
		return nil, false
	}
	index.buildTables()
	index.buildPaths()
	index.buildPostings()
	return index, true
}

// ParseFile reads the file with the given path and parses it as script. It returns an error if the file cannot be
// read.
func ParseFile(path string) (*jomini.Tree, error) {
	text, err := files.ReadText(path)
	if err != nil {
		return nil, err
	}
	return jomini.Parse(text.Text), nil
}

// ScanMods builds the index of a workspace's mods on top of the install's index, so that a lookup in the mods'
// index falls through to the install's. It first gives the schema the mods' scripted definitions. The replaced
// folders are given per root, as each mod's replace_path lists them. The index records every language the mods write,
// even one the install does not. It returns an error if a root cannot be walked.
func ScanMods(schema *schema.Schema, install *Index, roots []files.Root, replaced [][]string) (*Index, error) {
	listing, err := ListFiles(roots)
	if err != nil {
		return nil, err
	}
	schema.SetScripted(listing, ParseFile)
	index := newIndex(schema, install, roots, listing)
	index.scanFiles(schema, func(int) {})
	index.buildPostings()
	index.Replaced = slices.Concat(make([][]string, len(install.Roots)), replaced)
	return index, nil
}

// ScanFiles scans the given files again after they changed, reading each from disk, and first gives the schema the
// scripted definitions they declare now. A file that no longer exists is dropped with its occurrences. It returns an
// error, and changes nothing, if any of the files is the install's.
func (index *Index) ScanFiles(schema *schema.Schema, changed []files.File) error {
	installFiles := index.countInstallFiles()
	for _, file := range changed {
		if position, known := index.FindFile(file.Path); known && position < installFiles {
			return fmt.Errorf("%s is a file of the install", file.Path)
		}
	}
	schema.SetScripted(changed, ParseFile)
	var lock sync.RWMutex
	for _, file := range changed {
		position, known := index.FindFile(file.Path)
		_, err := os.Stat(file.Path)
		gone := errors.Is(err, fs.ErrNotExist)
		if !known && gone {
			continue
		}
		if !known {
			// A file gone before keeps its place, which a file of the same path takes again.
			slot := slices.IndexFunc(index.Files, func(other files.File) bool { return other.Key == file.Key })
			if slot < 0 {
				index.Files = append(index.Files, file)
				index.Occurrences = append(index.Occurrences, nil)
				index.Texts = append(index.Texts, nil)
				slot = len(index.Files) - 1
				index.buildGroup(int32(slot))
			}
			position = int32(slot)
		}
		index.Occurrences[position], index.Texts[position] = nil, nil
		if gone {
			delete(index.byKey, file.Key)
		} else {
			index.buildFile(&lock, schema, position)
			index.byKey[file.Key] = position
		}
	}
	index.buildPostings()
	return nil
}

// Clone returns a copy of an index that ScanFiles can change while the original is still being read. The copy has
// its own lists of files, occurrences and texts, its own paths and its own tables, which are what ScanFiles writes.
// It shares the rest with the original.
func (index *Index) Clone() *Index {
	next := *index
	next.Files = slices.Clone(index.Files)
	next.Occurrences = slices.Clone(index.Occurrences)
	next.Texts = slices.Clone(index.Texts)
	next.byKey = maps.Clone(index.byKey)
	next.byRel = maps.Clone(index.byRel)
	next.cloneTables(index)
	return &next
}

// ListFiles lists the files under the roots, root by root in the order given. A file under a root inside another one
// is listed once, under the inner root. It returns an error if a root cannot be walked.
func ListFiles(roots []files.Root) ([]files.File, error) {
	keys := listRootKeys(roots)
	var listing []files.File
	for position, root := range roots {
		list, err := files.Walk(root)
		if err != nil {
			return nil, err
		}
		// A file under a root inside this one belongs to the inner root.
		listing = append(listing, lo.Filter(list, func(file files.File, _ int) bool {
			owner, ok := findRoot(keys, file.Key)
			return ok && owner == position
		})...)
	}
	return listing, nil
}

// listRootKeys returns the key of each root's folder, ending in the slash that opens the paths under it.
func listRootKeys(roots []files.Root) []string {
	return lo.Map(roots, func(root files.Root, _ int) string { return files.ToKey(root.Dir) + "/" })
}

// findRoot returns the position of the innermost root that holds a file, given the keys of the roots' folders and
// the file's key. It returns false if no root holds the file.
func findRoot(keys []string, fileKey string) (int, bool) {
	position, length, ok := 0, 0, false
	for candidate, key := range keys {
		if len(key) > length && strings.HasPrefix(fileKey, key) {
			position, length, ok = candidate, len(key), true
		}
	}
	return position, ok
}

// newIndex returns an index of the given files with nothing found yet. Over an install's index it starts with the
// install's files, roots and what they hold, and copies of its tables, so that a file's position and an ordinal
// mean the same in both.
func newIndex(schema *schema.Schema, install *Index, roots []files.Root, listing []files.File) *Index {
	index := &Index{Game: schema.Game, Files: listing, Roots: roots, install: install}
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
		index.cloneTables(install)
	}
	index.buildPaths()
	return index
}

// listTables returns the four tables of the index.
func (index *Index) listTables() []*Table {
	return []*Table{&index.Databases, &index.Names, &index.Modes, &index.Languages}
}

// cloneTables gives the index copies of the tables of a source, which it can intern into without changing the
// source's.
func (index *Index) cloneTables(source *Index) {
	from := source.listTables()
	for position, table := range index.listTables() {
		*table = from[position].clone()
	}
}

// buildTables builds each table's lookup of its texts, and puts the empty text at ordinal 0 of a table that has none
// yet.
func (index *Index) buildTables() {
	for _, table := range index.listTables() {
		table.ordinals = make(map[string]uint32, len(table.Texts))
		for ordinal, text := range table.Texts {
			table.ordinals[text] = uint32(ordinal)
		}
		table.Intern("")
	}
}

// buildPaths fills the map that finds a file by its key, the keys of the roots' folders, and the groups of files by
// their paths.
func (index *Index) buildPaths() {
	index.rootKeys = listRootKeys(index.Roots)
	index.byKey = make(map[string]int32, len(index.Files))
	index.byRel = make(map[string][]int32, len(index.Files))
	for position, file := range index.Files {
		index.byKey[file.Key] = int32(position)
		index.buildGroup(int32(position))
	}
}

// buildGroup adds the file at a position to the group of files at its path, in load order. The group is replaced,
// not changed, since a clone of the index may still read it.
func (index *Index) buildGroup(position int32) {
	key := toRelKey(game.LookupGame(index.Game), index.Files[position].Rel)
	group := index.byRel[key]
	at, _ := slices.BinarySearchFunc(group, position, index.compareLoaded)
	index.byRel[key] = slices.Concat(group[:at], []int32{position}, group[at:])
}

// compareLoaded orders two files, given by their positions in Files, the way the game loads them: by their roots in
// load order, then by their stages, then as listed.
func (index *Index) compareLoaded(a, b int32) int {
	first, _ := findRoot(index.rootKeys, index.Files[a].Key)
	second, _ := findRoot(index.rootKeys, index.Files[b].Key)
	row := game.LookupGame(index.Game)
	return cmp.Or(cmp.Compare(first, second), cmp.Compare(index.classifyStage(row, a), index.classifyStage(row, b)),
		cmp.Compare(a, b))
}

// classifyStage returns where the stage of the file at a position comes in the given game's order of stages: 0 for a
// file under no stage, then 1 and up for the stages in the order the game lists them.
func (index *Index) classifyStage(row *game.Game, position int32) int {
	stage, _ := schema.SplitStage(row, strings.ToLower(index.Files[position].Rel))
	if stage == "" {
		return 0
	}
	return 1 + slices.Index(row.StageRoots, stage)
}

// scanFiles builds the occurrences of every file of the index's own, in parallel on every core but two. The progress
// function is called with each whole percent of the files that is done.
func (index *Index) scanFiles(schema *schema.Schema, progress func(percent int)) {
	var group errgroup.Group
	var lock sync.RWMutex
	var done atomic.Int64
	first := int(index.countInstallFiles())
	total := int64(len(index.Files) - first)
	group.SetLimit(files.Workers)
	for position := first; position < len(index.Files); position++ {
		group.Go(func() error {
			index.buildFile(&lock, schema, int32(position))
			if count := done.Add(1); count*100/total > (count-1)*100/total {
				progress(int(count * 100 / total))
			}
			return nil
		})
	}
	_ = group.Wait() // no file's task fails: one that cannot be read has no occurrences
}

// buildFile builds the occurrences of one file, and the texts of a localization file's messages. A format that has
// no occurrences, and a file that cannot be read, get none.
func (index *Index) buildFile(lock *sync.RWMutex, schema *schema.Schema, position int32) {
	// The file is built against tables of its own, so that building takes no lock; its ordinals move to the index's
	// tables afterwards, which takes the lock a few times for the whole file, not once for each occurrence.
	tables := index.makeTables()
	var occurrences []Occurrence
	var texts []string
	switch format := index.Files[position].Format; {
	case format == files.FormatLocalization:
		occurrences, texts = tables.buildMessages(schema, position, index.readSource(position))
	case format == files.FormatTable:
		occurrences = tables.buildRows(schema, position, index.readSource(position))
	case format.IsScript():
		visitor := &collector{schema: schema, index: tables, file: position}
		schema.Walk(jomini.Parse(index.readSource(position)), index.Files[position], visitor)
		occurrences = visitor.occurrences
	default:
		return
	}
	index.Occurrences[position], index.Texts[position] = occurrences, texts
	index.internFile(lock, tables, occurrences)
}

// readSource returns the text of the file at a position. A file that cannot be read gives "".
func (index *Index) readSource(position int32) string {
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
	var parent int32
	nested := len(collector.definitions) > 0
	if nested {
		parent = collector.definitions[len(collector.definitions)-1]
	}
	if definition := context.Definition; context.Defines {
		occurrence := index.makeOccurrence(RoleDefines, definition.Database, definition.Name, definition.Span)
		occurrence.Mode = uint8(index.Modes.Intern(definition.Mode))
		occurrence.Priority = int16(definition.Priority)
		occurrence.Local, occurrence.Alias = definition.Local, definition.Alias
		occurrence.Line = int32(tree.Lines.ToPosition(definition.Span.Start).Line)
		occurrence.Parent, occurrence.Nested = parent, nested
		occurrence.Statement = definition.Statement
		parent, nested = int32(len(collector.occurrences)), true
		collector.occurrences = append(collector.occurrences, occurrence)
		collector.definitions = append(collector.definitions, parent)
	}
	for _, reference := range context.References {
		occurrence := index.makeOccurrence(RoleRefers, reference.Database, reference.Name, reference.Span)
		occurrence.Line = int32(tree.Lines.ToPosition(reference.Span.Start).Line)
		occurrence.Parent, occurrence.Nested, occurrence.Firing = parent, nested, collector.isFiring(context, reference)
		occurrence.Statement = statement
		collector.occurrences = append(collector.occurrences, occurrence)
	}
	return true
}

// Leave closes the definition that the statement makes, if it makes one.
func (collector *collector) Leave(tree *jomini.Tree, statement int32, context *schema.Context) {
	if context.Defines {
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
	modes := internAll(lock, &index.Modes, tables.Modes.Texts)
	languages := internAll(lock, &index.Languages, tables.Languages.Texts)
	for position := range occurrences {
		occurrence := &occurrences[position]
		occurrence.Database = uint16(databases[occurrence.Database])
		occurrence.Name = names[occurrence.Name]
		occurrence.Mode = uint8(modes[occurrence.Mode])
		occurrence.Language = uint8(languages[occurrence.Language])
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

// makeOccurrence returns an occurrence of the given role, database and name written at the given span. It sits in
// no definition and no statement.
func (index *Index) makeOccurrence(role Role, database, name string, span jomini.Span) Occurrence {
	return Occurrence{
		Span: span, Database: uint16(index.Databases.Intern(database)),
		Name: index.Names.Intern(index.ToCaseless(database, name)), Role: role,
	}
}

// ToCaseless returns a text as the index stores it for a database: in lower case where the database's names match
// without regard to case, else as it is.
func (index *Index) ToCaseless(database, text string) string {
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
		for _, reference := range ListTextReferences(schema, message) {
			occurrence := index.makeOccurrence(RoleRefers, reference.Database, reference.Name, reference.Span)
			occurrence.Line = int32(file.Lines.ToPosition(reference.Span.Start).Line)
			occurrence.Parent, occurrence.Nested, occurrence.Language = parent, true, language
			occurrences = append(occurrences, occurrence)
			if keep {
				texts = append(texts, "")
			}
		}
	}
	return occurrences, texts
}

// ListTextReferences returns what a message's text refers to: the localization keys written between dollar signs, the
// text formats and icons it names, and the names that each data call in it refers to. A call also holds the keys its
// own text writes between dollar signs.
func ListTextReferences(schema *schema.Schema, message loc.Message) (references []schema.Reference) {
	for _, segment := range loc.Segments(message.Text, message.TextSpan.Start) {
		references = append(references, listMarkup(segment)...)
		if segment.Markup != loc.MarkupCall {
			continue
		}
		if call, ok := jomini.ParseCall(segment.Text, segment.TextSpan.Start); ok {
			references = append(references, schema.ListCallReferences(call, "")...)
			references = append(references, listConcepts(schema.Concepts, call)...)
		}
		for _, inner := range loc.Segments(segment.Text, segment.TextSpan.Start) {
			if inner.Markup == loc.MarkupKey {
				references = append(references, listMarkup(inner)...)
			}
		}
	}
	return references
}

// listMarkup returns what one piece of a message's text refers to: the localization key between dollar signs, each
// text format that a format names, which semicolons separate, and the icon an icon names. Any other piece refers to
// nothing.
func listMarkup(segment loc.Segment) []schema.Reference {
	switch segment.Markup {
	case loc.MarkupKey:
		return []schema.Reference{{Database: game.Localization, Name: segment.Text, Span: segment.TextSpan}}
	case loc.MarkupIcon:
		return []schema.Reference{{Database: game.GUI.TextIcons, Name: segment.Text, Span: segment.TextSpan}}
	case loc.MarkupFormat:
		var references []schema.Reference
		start := segment.TextSpan.Start
		for part := range strings.SplitSeq(segment.Text, ";") {
			if jomini.IsIdentifier(part) {
				span := jomini.Span{Start: start, End: start + len(part)}
				references = append(references, schema.Reference{Database: game.GUI.TextFormats, Name: part, Span: span})
			}
			start += len(part) + 1
		}
		return references
	}
	return nil
}

// listConcepts returns the game concept that a data call of a localization text names in the concept format, as a
// reference to the given database. A call of one step with no arguments and the concept format after its bar names its
// step. Any other call names none.
func listConcepts(database string, call jomini.Call) []schema.Reference {
	onlyStep := len(call.Steps) == 1 && len(call.Steps[0].Arguments) == 0
	if !onlyStep || !strings.EqualFold(call.Suffix, game.ConceptFormat) {
		return nil
	}
	return []schema.Reference{{Database: database, Name: call.Steps[0].Text, Span: call.Steps[0].Span}}
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
