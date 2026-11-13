// scan.go builds an index from the mounts of an install's files and of the
// mods' files: it scans each file for the definitions and references it
// holds, and interns the names in the index's tables.

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
	"time"

	"golang.org/x/sync/errgroup"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/jomini"
	"paradox-modding-tools/internal/lang"
	"paradox-modding-tools/internal/loc"
)

// collector turns the definition and the references of each statement of a
// walked file into occurrences.
type collector struct {
	schema *lang.Schema

	// index holds the tables the file is built against.
	index *Index

	occurrences []Occurrence

	// holders holds, for each statement read so far, where the occurrence of
	// the innermost definition around its block is. It is -1 for none.
	holders []int32

	// texts holds, by the occurrence of a definition, the statements that
	// write the keys of its title and of its description. A statement of 0
	// is none.
	texts map[int32][2]int32
}

// contextPool reuses the walk's lists of contexts across files, so a scan
// allocates one per worker and not one per file.
var contextPool = sync.Pool{New: func() any { return new([]lang.Context) }}

// Scan builds the index of an install's files, in parallel. The progress
// function gets each stage's name and its percent done, or -1 for a stage
// that reports none.
//
// Localization is scanned in the base language only: most of the game's
// messages are in the other languages, and a workspace needs at most one of
// them, which ScanTranslation adds.
func Scan(schema *lang.Schema, mounts *files.Mounts, progress func(stage string, percent int)) *Index {
	index := newIndex(schema, nil, mounts)
	index.Stamp = time.Now().UnixNano()

	var positions []int32
	for position, file := range index.Files {
		if file.Format != files.FormatLocalization || slices.Contains([]string{"", loc.BaseLanguage}, findLanguage(file)) {
			positions = append(positions, int32(position))
		}
	}

	const stage = "Reading the game's files"
	progress(stage, 0)
	index.scanFiles(schema, positions, func(percent int) { progress(stage, percent) })

	progress("Indexing names", -1)
	index.buildPostings()

	return index
}

// findLanguage returns the language a localization file's path names, or ""
// for none.
func findLanguage(file files.File) string {
	return cmp.Or(loc.FindSuffixLanguage(file.Path), loc.FindFolderLanguage(file.Path))
}

// ScanMods builds the index of a workspace's mods on top of the install's.
// The progress function gets the percent of the files scanned. It returns an
// error if a root cannot be walked.
func ScanMods(schema *lang.Schema, install *Index, roots []files.Root, progress func(percent int)) (*Index, error) {
	mounts, err := install.Mount(roots)
	if err != nil {
		return nil, err
	}

	unread := listUnread(mounts)
	schema.SetScripted(mounts.Files[len(install.Files):], unread, ParseFile)

	index := newIndex(schema, install, mounts)
	index.unread = unread
	index.scanFiles(schema, index.ListOwnFiles(), progress)
	index.buildPostings()

	return index, nil
}

// ScanFiles scans changed files again from disk, and drops a file that no
// longer exists. The schema learns the files' scripted definitions after the
// files are listed, so it knows which ones the game reads, and before any is
// walked, since the walk types calls by them. It returns an error, and
// changes nothing, if a file is the install's.
func (i *Index) ScanFiles(schema *lang.Schema, changed []files.File) error {
	installFiles := i.countInstallFiles()
	for _, file := range changed {
		if position, ok := i.FindFile(file.Path); ok && position < installFiles {
			return fmt.Errorf("%s is a file of the install", file.Path)
		}
	}

	var positions []int32
	relisted := false

	for _, file := range changed {
		position, known := i.FindFile(file.Path)
		_, err := os.Stat(file.Path)
		deleted := errors.Is(err, fs.ErrNotExist)
		if !known && deleted {
			continue
		}

		if !known {
			position = i.Add(file)
			if int(position) == len(i.Occurrences) {
				i.Occurrences = append(i.Occurrences, nil)
				i.Texts = append(i.Texts, nil)
			}
		}

		relisted = relisted || !known || deleted
		i.Occurrences[position], i.Texts[position] = nil, nil
		if deleted {
			i.Unlist(file.Key)
		} else {
			positions = append(positions, position)
		}
	}

	// Which files the game reads changes only when one is listed or dropped.
	if relisted {
		i.unread = listUnread(i.Mounts)
	}

	schema.SetScripted(changed, i.unread, ParseFile)

	var lock sync.RWMutex

	for _, position := range positions {
		i.buildFile(&lock, schema, position)
	}

	i.buildPostings()

	return nil
}

// listUnread returns the keys of the mounted files the game does not read.
func listUnread(mounts *files.Mounts) map[string]bool {
	unread := map[string]bool{}
	for position, file := range mounts.Files {
		if !mounts.IsLoaded(int32(position)) || mounts.IsUnmounted(int32(position)) {
			unread[file.Key] = true
		}
	}

	return unread
}

// Clone returns a copy of an index that ScanFiles can change while the
// original is still being read.
func (i *Index) Clone() *Index {
	next := *i
	next.Mounts = i.Mounts.Clone()
	next.Occurrences = slices.Clone(i.Occurrences)
	next.Texts = slices.Clone(i.Texts)
	next.Unmarked = maps.Clone(i.Unmarked)
	next.cloneTables(i)

	return &next
}

// LoadIndex loads an install's index from its cache file. The cache holds
// only exported fields, so the lookups and postings are built again. It
// returns false if the file is missing, cannot be read or holds no mounts.
func LoadIndex(path string) (*Index, bool) {
	index, ok := files.LoadGob[*Index](path)
	if !ok || index.Mounts == nil {
		return nil, false
	}

	index.Mounts = files.NewMounts(index.Roots, index.Files)
	index.buildTables()
	index.buildPostings()

	return index, true
}

// ParseFile reads a file and parses it as script. It returns an error if the
// file cannot be read.
func ParseFile(path string) (*jomini.Tree, error) {
	text, err := files.ReadText(path)
	if err != nil {
		return nil, err
	}

	return jomini.Parse(text.Text), nil
}

// newIndex returns an index of the mounts with nothing scanned yet. Built on
// an install's index, it starts with the install's occurrences and copies of
// its tables, so a position and an ordinal mean the same in both.
func newIndex(schema *lang.Schema, install *Index, mounts *files.Mounts) *Index {
	index := &Index{
		Mounts: mounts, Game: schema.Game, install: install,
		Occurrences: make([][]Occurrence, len(mounts.Files)), Texts: make([][]string, len(mounts.Files)),
		Unmarked: map[int32]bool{},
	}

	for name, database := range schema.Databases {
		if database.IsCaseless() {
			index.Caseless = append(index.Caseless, name)
		}
	}

	slices.Sort(index.Caseless)

	if install == nil {
		index.buildTables()

		return index
	}

	copy(index.Occurrences, install.Occurrences)
	copy(index.Texts, install.Texts)
	index.cloneTables(install)

	return index
}

// buildTables builds each table's lookup of its texts. A new table gets the
// empty text at ordinal 0, which an occurrence reads as none.
func (i *Index) buildTables() {
	for _, table := range i.listTables() {
		table.ordinals = make(map[string]uint32, len(table.Texts))
		for ordinal, text := range table.Texts {
			table.ordinals[text] = uint32(ordinal)
		}

		table.Intern("")
	}
}

// cloneTables copies the source index's tables into this one, so interning
// here leaves the source unchanged.
func (i *Index) cloneTables(source *Index) {
	originals := source.listTables()
	for position, table := range i.listTables() {
		*table = Table{Texts: slices.Clone(originals[position].Texts), ordinals: maps.Clone(originals[position].ordinals)}
	}
}

func (i *Index) listTables() []*Table {
	return []*Table{&i.Databases, &i.Names, &i.Modes, &i.Languages}
}

// scanFiles builds the occurrences of the files at the given positions in
// Files, in parallel. The progress function gets each whole percent done.
func (i *Index) scanFiles(schema *lang.Schema, positions []int32, progress func(percent int)) {
	var group errgroup.Group
	var lock sync.RWMutex
	var done atomic.Int64
	total := int64(len(positions))
	group.SetLimit(files.Workers)

	for _, position := range positions {
		group.Go(func() error {
			i.buildFile(&lock, schema, position)
			if count := done.Add(1); count*100/total > (count-1)*100/total {
				progress(int(count * 100 / total))
			}

			return nil
		})
	}

	// No file's task fails: one that cannot be read has no occurrences.
	_ = group.Wait()
}

// buildFile builds the occurrences of one file, and the texts of a
// localization file's messages. A file that cannot be read gets none.
func (i *Index) buildFile(lock *sync.RWMutex, schema *lang.Schema, position int32) {
	// The file is built against tables of its own so that building takes no
	// lock; its ordinals move to the index's tables afterwards.
	tables := &Index{Mounts: i.Mounts, Game: i.Game, Caseless: i.Caseless}
	tables.buildTables()
	var occurrences []Occurrence
	var texts []string

	switch format := i.Files[position].Format; {
	case format == files.FormatLocalization:
		source, _ := files.ReadText(i.Files[position].Path)
		occurrences, texts = tables.buildMessages(schema, position, source.Text)
		lock.Lock()
		if source.Encoding == files.EncodingUTF8BOM {
			delete(i.Unmarked, position)
		} else {
			i.Unmarked[position] = true
		}
		lock.Unlock()

	case format == files.FormatTable:
		occurrences = tables.buildRows(schema, position, i.readSource(position))
	case format.IsScript():
		collector := &collector{schema: schema, index: tables}
		occurrences = collector.collect(jomini.Parse(i.readSource(position)), i.Files[position])
	default:
		return
	}

	i.Occurrences[position], i.Texts[position] = occurrences, texts
	i.internFile(lock, tables, occurrences)
}

// readSource returns the text of a file, given by its position in Files. A
// file that cannot be read gives "".
func (i *Index) readSource(position int32) string {
	text, _ := files.ReadText(i.Files[position].Path)

	return text.Text
}

// buildMessages builds the occurrences of a localization file: each message,
// followed by what its text refers to. The texts run beside the occurrences.
func (i *Index) buildMessages(schema *lang.Schema, position int32, source string) ([]Occurrence, []string) {
	file := loc.Parse(source, i.Files[position].Path)
	language := uint8(i.Languages.Intern(file.Language))
	var occurrences []Occurrence
	var texts []string

	for position, context := range schema.WalkMessages(file) {
		message, parent := file.Messages[position], int32(len(occurrences))
		occurrence := i.makeOccurrence(RoleLocalizes, lang.Localization, message.Key, message.KeySpan)
		occurrence.Language = language
		occurrence.Line = int32(file.Lines.ToPosition(message.Span.Start).Line)
		occurrences = append(occurrences, occurrence)
		texts = append(texts, strings.Clone(message.Text))

		for _, reference := range context.References {
			occurrence := i.makeOccurrence(RoleRefers, reference.Database, reference.Name, reference.Span)
			occurrence.Line = int32(file.Lines.ToPosition(reference.Span.Start).Line)
			occurrence.Parent, occurrence.Language = parent, language
			occurrences = append(occurrences, occurrence)
			texts = append(texts, "")
		}
	}

	return occurrences, texts
}

// buildRows builds a definition for each row of a table file, named by its
// first column. A table whose database is not defined by its rows has none.
func (i *Index) buildRows(schema *lang.Schema, position int32, source string) []Occurrence {
	database, ok := schema.FindDatabase(i.Files[position])
	if !ok || database.Declaration.DefinedBy != lang.DefinedByColumn {
		return nil
	}

	var occurrences []Occurrence

	for _, row := range files.ListRows(source) {
		span := jomini.Span{Start: row.Start, End: row.Start + len(row.Name)}
		occurrence := i.makeOccurrence(RoleDefines, database.Name, row.Name, span)
		occurrence.Line, occurrence.Own = int32(row.Line), true
		occurrences = append(occurrences, occurrence)
	}

	return occurrences
}

// collect walks a file and returns its occurrences: each statement's
// definition, then its references, in the tree's order.
func (c *collector) collect(tree *jomini.Tree, file files.File) []Occurrence {
	list := contextPool.Get().(*[]lang.Context)
	defer contextPool.Put(list)

	contexts, _ := c.schema.Walk(tree, file, *list)
	*list = contexts
	c.holders, c.texts = make([]int32, len(contexts)), map[int32][2]int32{}

	for position := range contexts {
		c.collectStatement(tree, int32(position), &contexts[position])
	}

	return c.occurrences
}

func (c *collector) collectStatement(tree *jomini.Tree, statement int32, context *lang.Context) {
	// The file's own statement is inside no definition.
	holder := int32(-1)
	if statement > 0 {
		holder = c.holders[tree.Statements[statement].Parent]
	}

	if definition := context.Definition; context.Defines {
		occurrence := c.index.makeOccurrence(RoleDefines, definition.Database, definition.Name, definition.Span)
		occurrence.Mode = uint8(c.index.Modes.Intern(definition.Mode))
		occurrence.Priority = int16(definition.Priority)
		occurrence.Local, occurrence.Own = definition.Local, definition.IsOwn()
		occurrence.Line = int32(tree.Lines.ToPosition(definition.Span.Start).Line)
		occurrence.Parent, occurrence.Statement = holder, definition.Statement
		holder = int32(len(c.occurrences))
		c.occurrences = append(c.occurrences, occurrence)

		title, _ := lang.FindTextKey(tree, statement, lang.TitleKey)
		description, _ := lang.FindTextKey(tree, statement, lang.DescriptionKey)
		if title+description > 0 {
			c.texts[holder] = [2]int32{title, description}
		}
	}

	c.holders[statement] = holder

	texts := c.texts[holder]
	for _, reference := range context.References {
		occurrence := c.index.makeOccurrence(RoleRefers, reference.Database, reference.Name, reference.Span)
		occurrence.Line = int32(tree.Lines.ToPosition(reference.Span.Start).Line)
		occurrence.Parent, occurrence.Statement = holder, statement
		occurrence.Firing = c.countFiring(tree, statement, context, reference)
		if holder >= 0 && reference.Database == lang.Localization {
			occurrence.Titles, occurrence.Describes = statement == texts[0], statement == texts[1]
		}

		c.occurrences = append(c.occurrences, occurrence)
	}
}

func (i *Index) makeOccurrence(role Role, database, name string, span jomini.Span) Occurrence {
	return Occurrence{
		Span: span, Database: uint16(i.Databases.Intern(database)),
		Name: i.Names.Intern(i.ToCaseless(database, name)), Role: role, Parent: -1,
	}
}

// ToCaseless returns a name as the index stores it for a database: in lower
// case where the database's names match in any letter case.
func (i *Index) ToCaseless(database, text string) string {
	if slices.Contains(i.Caseless, database) {
		return strings.ToLower(text)
	}

	return text
}

// internFile moves the ordinals of a file's occurrences from the tables the
// file was built against to the index's own tables.
func (i *Index) internFile(lock *sync.RWMutex, tables *Index, occurrences []Occurrence) {
	if len(occurrences) == 0 {
		return
	}

	databases := internAll(lock, &i.Databases, tables.Databases.Texts)
	names := internAll(lock, &i.Names, tables.Names.Texts)
	modes := internAll(lock, &i.Modes, tables.Modes.Texts)
	languages := internAll(lock, &i.Languages, tables.Languages.Texts)

	for position := range occurrences {
		occurrence := &occurrences[position]
		occurrence.Database = uint16(databases[occurrence.Database])
		occurrence.Name = names[occurrence.Name]
		occurrence.Mode = uint8(modes[occurrence.Mode])
		occurrence.Language = uint8(languages[occurrence.Language])
	}
}

// internAll returns the ordinal in a table of each of the given texts, adding
// the ones the table lacks. Most are there already, so only the missing ones
// take the write lock. An added text is copied, so that it does not keep its
// file's source alive.
func internAll(lock *sync.RWMutex, table *Table, texts []string) []uint32 {
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
