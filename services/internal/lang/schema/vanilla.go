// vanilla.go is the one read of vanilla's script: every script file whose extension has a FileTypes
// row, parsed once, a reader per CPU. What it keeps serves the doc reader (headers, top names and
// inner keys), the build (what each database's entries write, by position) and the wiki's placement.
package schema

import (
	"cmp"
	"encoding/json"
	"maps"
	"os"
	"path"
	"runtime"
	"slices"
	"strings"
	"sync"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/source"
)

// maxValues is how many distinct words one position keeps. It bounds memory and decides nothing: a
// position past it holds more words than any closed list the docs write.
const maxValues = 512

// Scripts is what the one read of vanilla's script files keeps.
type Scripts struct {
	Groups      map[string][]string        // database name to its folders or files (Groups)
	Entries     map[string]*Shape          // what each database's entries write, by database name
	Names       map[string]map[string]bool // each database's entry names, as written
	Headers     map[string]string          // each .txt file's comment lines before its first line of script, by rel
	Tops        map[string]map[string]bool // each folder's entry names, lower-cased, by lower-cased folder
	Inner       map[string]map[string]bool // each folder's keys at any depth inside its entries, lower-cased
	Prefixed    map[string]map[string]bool // a prefix to the names written after it
	EntryModes  map[string]int             // the MODE: words written before names
	ParseErrors []Tally                    // files that did not parse cleanly, by rel
	Sounds      []string                   // EU5's Wwise event names

	info *game.GameInfo // the registry rows the read followed, which the doc reader follows too
}

// Shape is what vanilla writes at one position: how often each form, the words, and the positions
// below it. At a Nested row's position the keys are names, kept as a set, and what their blocks
// hold is the position <name> below.
type Shape struct {
	Words, Lists, Blocks int               // a word, a block of bare words, a block of statements
	Values               map[string]bool   // the words written as its value or in its list; nil past maxValues
	Many                 bool              // more than maxValues distinct words were written here
	Keys                 map[string]*Shape // the positions below, by key; <number>, <date>, <prefixed> by spelling
	Names                map[string]bool   // at a Nested row's position: the keys written there
}

// Groups is the databases of the .txt folders, each to its folders or files. An Events row's tree is
// one database; under common/ a folder joins the nearest folder above it that holds .txt, never
// common; every other folder is its own. A top-level folder's own files are one database when no
// folder below it holds .txt, else each file is one, named by its stem. A database is named by its
// last segment, two of one name both by the folder above too; a History or Unnamed row's by its
// path.
func Groups(files []source.File, info *game.GameInfo) map[string][]string {
	holds, spelled := map[string]bool{}, map[string]string{}
	for _, file := range files {
		if dir := path.Dir(file.Rel); isScriptText(file) && dir != "." {
			holds[strings.ToLower(dir)], spelled[strings.ToLower(dir)] = true, dir
		}
	}

	// each folder, or a top-level folder's file, to the folder or file its database is named by
	tops := map[string][]string{}
	for folder := range holds {
		row, at := FolderRow(info, folder)
		switch {
		case row.Events:
			tops[at] = append(tops[at], spelled[folder])
		case !strings.Contains(folder, "/"):
			below := slices.ContainsFunc(slices.Collect(maps.Keys(holds)), func(other string) bool { return strings.HasPrefix(other, folder+"/") })
			if !below {
				tops[folder] = append(tops[folder], spelled[folder])
				continue
			}
			for _, file := range files {
				if isScriptText(file) && strings.EqualFold(path.Dir(file.Rel), folder) {
					tops[strings.ToLower(file.Rel)] = append(tops[strings.ToLower(file.Rel)], file.Rel)
				}
			}
		case strings.HasPrefix(folder, "common/"):
			top := folder
			for above := path.Dir(folder); above != "common"; above = path.Dir(above) {
				if holds[above] {
					top = above
				}
			}
			tops[top] = append(tops[top], spelled[folder])
		default:
			tops[folder] = append(tops[folder], spelled[folder])
		}
	}

	// the names: the last segment, the folder above it too where two share one
	named := map[string][]string{}
	for top := range tops {
		if row, _ := FolderRow(info, top); row.History || row.Unnamed {
			continue
		}
		base := strings.TrimSuffix(path.Base(top), ".txt")
		named[base] = append(named[base], top)
	}
	groups := map[string][]string{}
	for top, folders := range tops {
		name := top
		if row, _ := FolderRow(info, top); row.Events {
			name = path.Base(top)
		} else if base := strings.TrimSuffix(path.Base(top), ".txt"); !row.History && !row.Unnamed {
			name = base
			if len(named[base]) > 1 {
				name = path.Base(path.Dir(top)) + "/" + base
			}
		}
		slices.Sort(folders)
		groups[name] = slices.Compact(folders)
	}
	return groups
}

// isScriptText reports a script file with the .txt extension.
func isScriptText(file source.File) bool {
	return file.Format == source.Script && strings.EqualFold(path.Ext(file.Rel), ".txt")
}

// FolderRow is the registry's row for a path, the longest at or above it, and the row's path.
func FolderRow(info *game.GameInfo, rel string) (game.Folder, string) {
	for at := strings.ToLower(rel); at != "." && at != "/" && at != ""; at = path.Dir(at) {
		if row, ok := info.Folders[at]; ok {
			return row, at
		}
	}
	return game.Folder{}, ""
}

// ReadScripts parses every script file whose extension has a FileTypes row once, a reader per CPU,
// each keeping its own partial result; the partials merge once when all are done.
func ReadScripts(files []source.File, info *game.GameInfo) *Scripts {
	groups := Groups(files, info)
	groupOf := map[string]string{}
	for name, folders := range groups {
		for _, folder := range folders {
			groupOf[strings.ToLower(folder)] = name
		}
	}

	work := make(chan source.File)
	partials := make([]*Scripts, runtime.NumCPU())
	var wg sync.WaitGroup
	for i := range partials {
		partials[i] = newScripts()
		wg.Go(func() {
			for file := range work {
				partials[i].read(file, info, groupOf)
			}
		})
	}
	for _, file := range files {
		_, typed := info.FileTypes[strings.ToLower(path.Ext(file.Rel))]
		if file.Format == source.Script && typed {
			work <- file
		}
	}
	close(work)
	wg.Wait()

	scripts := newScripts()
	scripts.Groups, scripts.info = groups, info
	for _, partial := range partials {
		scripts.merge(partial)
	}
	slices.SortFunc(scripts.ParseErrors, func(first, second Tally) int { return strings.Compare(first.Name, second.Name) })

	// EU5 lists its Wwise events in a manifest no FileTypes row reads
	for _, file := range files {
		if !strings.EqualFold(path.Base(file.Rel), "SoundbanksInfo.json") {
			continue
		}
		if raw, err := os.ReadFile(file.Path); err == nil {
			scripts.Sounds = append(scripts.Sounds, SoundEvents(raw)...)
		}
	}
	slices.Sort(scripts.Sounds)
	scripts.Sounds = slices.Compact(scripts.Sounds)
	return scripts
}

func newScripts() *Scripts {
	return &Scripts{
		Entries: map[string]*Shape{}, Names: map[string]map[string]bool{}, Headers: map[string]string{}, Tops: map[string]map[string]bool{}, Inner: map[string]map[string]bool{},
		Prefixed: map[string]map[string]bool{}, EntryModes: map[string]int{},
	}
}

// SoundEvents is the event names of a Wwise manifest, decoded into the names alone.
func SoundEvents(raw []byte) []string {
	var manifest struct {
		SoundBanksInfo struct {
			SoundBanks []struct {
				Events []struct{ Name string }
			}
		}
	}
	if json.Unmarshal(raw, &manifest) != nil {
		return nil
	}
	var names []string
	for _, bank := range manifest.SoundBanksInfo.SoundBanks {
		for _, event := range bank.Events {
			names = append(names, event.Name)
		}
	}
	return names
}

// scriptWalk is one file being read.
type scriptWalk struct {
	scripts *Scripts
	info    *game.GameInfo
	groupOf map[string]string // a folder or file, lower-cased, to its database
	folder  string            // the file's folder, lower-cased
	nested  []game.Nested     // the Nested rows that hold for the file's database
	inner   map[string]bool
}

// read reads one file into the partial result.
func (scripts *Scripts) read(file source.File, info *game.GameInfo, groupOf map[string]string) {
	text, err := file.Text()
	if err != nil {
		return
	}
	parsed := jomini.Parse(text)
	if len(parsed.Errors) > 0 {
		scripts.ParseErrors = append(scripts.ParseErrors, Tally{Name: file.Rel, Count: len(parsed.Errors)})
	}
	ext := strings.ToLower(path.Ext(file.Rel))
	fileType := info.FileTypes[ext]
	folder := strings.ToLower(path.Dir(file.Rel))
	walk := &scriptWalk{scripts: scripts, info: info, groupOf: groupOf, folder: folder}

	// a .txt file's header and database; any other extension names its databases by its blocks
	if ext != ".txt" {
		walk.typed(strings.TrimPrefix(ext, "."), fileType, parsed.Root.Statements, true)
		return
	}
	scripts.Headers[file.Rel] = header(text)
	database := groupOf[strings.ToLower(file.Rel)]
	for at := folder; database == "" && at != "." && at != "/"; at = path.Dir(at) {
		database = groupOf[at]
	}
	row, _ := FolderRow(info, file.Rel)
	for _, nested := range info.Nested {
		if parent := strings.ToLower(nested.Parent); strings.HasPrefix(strings.ToLower(file.Rel)+"/", parent+"/") {
			walk.nested = append(walk.nested, nested)
		}
	}
	if scripts.Inner[folder] == nil {
		scripts.Inner[folder], scripts.Tops[folder] = map[string]bool{}, map[string]bool{}
	}
	walk.inner = scripts.Inner[folder]

	// a ByFile history folder's file is one entry, named by its stem; a history folder with no Of and
	// an Unnamed one hold no entries at their top, so each file is one unnamed block
	whole := &jomini.Block{Statements: parsed.Root.Statements}
	switch {
	case row.ByFile:
		walk.entry(database, strings.TrimSuffix(path.Base(file.Rel), path.Ext(file.Rel)), whole)
		return
	case row.Unnamed || row.History && len(row.Of) == 0:
		walk.entry(database, "", whole)
		return
	}
	walk.top(database, row, parsed.Root.Statements)
}

// header is a file's comment lines before its first line of script, with its blank lines; a file
// whose comments run to its end is commented-out script and has none.
func header(text string) string {
	end := 0
	for line := range strings.SplitSeq(text, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			return strings.TrimRight(text[:end], "\n")
		}
		end += len(line) + 1
	}
	return ""
}

// top reads a .txt file's top level: each statement an entry of the database, a wrapper's keys
// entries in its place, a keyword declaring an entry of the keyword's database, and constants none.
func (walk *scriptWalk) top(database string, row game.Folder, stmts []jomini.Statement) {
	for i := 0; i < len(stmts); i++ {
		switch st := stmts[i].(type) {
		case *jomini.ValueStmt:
			// scripted_trigger x = { } in any file declares an entry of the keyword's database
			scalar, ok := st.Value.(*jomini.Scalar)
			if !ok || i+1 >= len(stmts) {
				continue
			}
			next, ok := stmts[i+1].(*jomini.Assignment)
			if keyword := walk.keywordDatabase(scalar.Text); ok && keyword != "" {
				walk.entry(keyword, next.Key.Text, next.Value)
				i++
			}
		case *jomini.Assignment:
			if _, constant := jomini.Constant(st.Key.Text); constant {
				continue
			}
			block := jomini.BlockOf(st.Value)
			if slices.Contains(row.Wrappers, strings.ToLower(st.Key.Text)) && block != nil {
				walk.top(database, row, block.Statements)
				continue
			}
			name := st.Key.Text
			if row.NameKey != "" && block != nil {
				name = valueOf(block, row.NameKey)
			}
			walk.entry(database, name, st.Value)
		}
	}
}

// keywordDatabase is the database a Folders row's Keyword declares entries of, "" for a word no
// row has.
func (walk *scriptWalk) keywordDatabase(word string) string {
	for folder, row := range walk.info.Folders {
		if row.Keyword != "" && row.Keyword == word {
			return walk.groupOf[folder]
		}
	}
	return ""
}

// typed reads a file of an extension with a NameKey (.gui, .asset, .font): a top block named by it is
// an entry of <ext>.<key>; a declaring keyword's block of <ext>.<keyword>, and a NameKeyOf key's of
// <ext>.<key>, at any depth.
func (walk *scriptWalk) typed(ext string, fileType game.FileType, stmts []jomini.Statement, top bool) {
	for i := 0; i < len(stmts); i++ {
		switch st := stmts[i].(type) {
		case *jomini.ValueStmt:
			scalar, ok := st.Value.(*jomini.Scalar)
			if !ok || i+1 >= len(stmts) || !slices.Contains(fileType.Keywords, scalar.Text) {
				continue
			}
			if next, ok := stmts[i+1].(*jomini.Assignment); ok {
				walk.entry(ext+"."+scalar.Text, next.Key.Text, next.Value)
				i++
			}
		case *jomini.Assignment:
			block := jomini.BlockOf(st.Value)
			if block == nil {
				continue
			}
			key := strings.ToLower(st.Key.Text)
			switch {
			case fileType.NameKeyOf[key] != "":
				walk.entry(ext+"."+key, valueOf(block, fileType.NameKeyOf[key]), st.Value)
			case top && fileType.NameKey != "" && valueOf(block, fileType.NameKey) != "":
				walk.entry(ext+"."+key, valueOf(block, fileType.NameKey), st.Value)
			}
			walk.typed(ext, fileType, block.Statements, false)
		}
	}
}

// valueOf is the word a block writes at key, "" where it writes none.
func valueOf(block *jomini.Block, key string) string {
	for _, st := range block.Statements {
		if assignment, ok := st.(*jomini.Assignment); ok && strings.EqualFold(assignment.Key.Text, key) {
			if scalar, ok := assignment.Value.(*jomini.Scalar); ok {
				return scalar.Text
			}
		}
	}
	return ""
}

// entry reads one entry of a database: its name, and what it writes into the database's shape.
func (walk *scriptWalk) entry(database, name string, value jomini.Value) {
	if database == "" {
		return
	}
	mode, name := jomini.Mode(name)
	if mode != "" {
		walk.scripts.EntryModes[mode]++
	}
	if walk.scripts.Tops[walk.folder] != nil && name != "" {
		walk.scripts.Tops[walk.folder][strings.Clone(strings.ToLower(name))] = true
	}
	if name != "" {
		if walk.scripts.Names[database] == nil {
			walk.scripts.Names[database] = map[string]bool{}
		}
		walk.scripts.Names[database][strings.Clone(name)] = true
	}
	shape := walk.scripts.Entries[database]
	if shape == nil {
		shape = &Shape{}
		walk.scripts.Entries[database] = shape
	}
	walk.value(shape, value, nil)
}

// value reads what is written at one position.
func (walk *scriptWalk) value(shape *Shape, value jomini.Value, at []string) {
	switch value := value.(type) {
	case *jomini.Scalar:
		shape.Words++
		shape.word(value.Text)
		walk.prefixed(value.Text)
	case *jomini.Block, *jomini.TaggedBlock:
		block := jomini.BlockOf(value)
		bare := len(block.Statements) > 0 && !slices.ContainsFunc(block.Statements, func(st jomini.Statement) bool {
			_, isAssignment := st.(*jomini.Assignment)
			return isAssignment
		})
		if bare {
			shape.Lists++
		} else {
			shape.Blocks++
		}
		walk.block(shape, block.Statements, at)
	}
}

// block reads a block's statements into the position that holds them; at a Nested row's position its
// keys are names, and their blocks the position <name>.
func (walk *scriptWalk) block(shape *Shape, stmts []jomini.Statement, at []string) {
	named := walk.isNested(at)
	for _, st := range stmts {
		switch st := st.(type) {
		case *jomini.ValueStmt:
			if scalar, ok := st.Value.(*jomini.Scalar); ok {
				shape.word(scalar.Text)
				walk.prefixed(scalar.Text)
			}
		case *jomini.Assignment:
			key := strings.ToLower(st.Key.Text)
			if walk.inner != nil && !walk.inner[key] {
				walk.inner[strings.Clone(key)] = true
			}
			walk.prefixed(st.Key.Text)
			position, step := positionOf(st.Key.Text), key
			if named {
				if shape.Names == nil {
					shape.Names = map[string]bool{}
				}
				shape.Names[strings.Clone(st.Key.Text)] = true
				position, step = "<name>", "*"
			}
			if shape.Keys == nil {
				shape.Keys = map[string]*Shape{}
			}
			below := shape.Keys[position]
			if below == nil {
				below = &Shape{}
				shape.Keys[strings.Clone(position)] = below
			}
			walk.value(below, st.Value, append(at[:len(at):len(at)], step))
		}
	}
}

// isNested reports a Nested row naming the position at, a path of keys below the entry.
func (walk *scriptWalk) isNested(at []string) bool {
	return slices.ContainsFunc(walk.nested, func(row game.Nested) bool { return pathMatches(row.Path, at) })
}

// pathMatches reports a Nested row's path naming the position at: "" the entry's own block, * one
// key, a leading **/ any depth.
func pathMatches(rowPath string, at []string) bool {
	pattern := strings.Split(strings.ToLower(rowPath), "/")
	switch {
	case rowPath == "":
		return len(at) == 0
	case pattern[0] == "**":
		matched := false
		if len(at) > 0 {
			matched, _ = path.Match(pattern[len(pattern)-1], at[len(at)-1])
		}
		return matched
	case len(pattern) != len(at):
		return false
	}
	for i, segment := range pattern {
		if matched, _ := path.Match(segment, at[i]); !matched {
			return false
		}
	}
	return true
}

// positionOf is the position a key stands at: a number, a date or a prefixed word by its spelling,
// else the key itself, lower-cased.
func positionOf(key string) string {
	switch jomini.Literal(key) {
	case jomini.LitNumber:
		return "<number>"
	case jomini.LitDate:
		return "<date>"
	}
	if _, ok := jomini.SplitPrefixed(key); ok {
		return "<prefixed>"
	}
	return strings.ToLower(key)
}

// prefixed keeps the name a prefixed word writes after its prefix, the key where it names one inside
// it (define:NGame|MAX_AGE names MAX_AGE).
func (walk *scriptWalk) prefixed(word string) {
	if prefixed, ok := jomini.SplitPrefixed(word); ok {
		names := walk.scripts.Prefixed[prefixed.Prefix]
		if names == nil {
			names = map[string]bool{}
			walk.scripts.Prefixed[strings.Clone(prefixed.Prefix)] = names
		}
		names[strings.Clone(cmp.Or(prefixed.Key, prefixed.Name))] = true
	}
}

// word keeps a word written at the position, up to maxValues distinct.
func (shape *Shape) word(word string) {
	if shape.Many || shape.Values[word] {
		return
	}
	if len(shape.Values) >= maxValues {
		shape.Values, shape.Many = nil, true
		return
	}
	if shape.Values == nil {
		shape.Values = map[string]bool{}
	}
	shape.Values[strings.Clone(word)] = true
}

// merge adds another reader's partial result.
func (scripts *Scripts) merge(other *Scripts) {
	for name, shape := range other.Entries {
		if scripts.Entries[name] == nil {
			scripts.Entries[name] = &Shape{}
		}
		scripts.Entries[name].merge(shape)
	}
	maps.Copy(scripts.Headers, other.Headers)
	for _, sets := range []struct{ into, from map[string]map[string]bool }{
		{scripts.Names, other.Names}, {scripts.Tops, other.Tops}, {scripts.Inner, other.Inner}, {scripts.Prefixed, other.Prefixed},
	} {
		for name, set := range sets.from {
			if sets.into[name] == nil {
				sets.into[name] = map[string]bool{}
			}
			maps.Copy(sets.into[name], set)
		}
	}
	for mode, count := range other.EntryModes {
		scripts.EntryModes[mode] += count
	}
	scripts.ParseErrors = append(scripts.ParseErrors, other.ParseErrors...)
}

// merge adds what another reader saw at the same position.
func (shape *Shape) merge(other *Shape) {
	shape.Words, shape.Lists, shape.Blocks = shape.Words+other.Words, shape.Lists+other.Lists, shape.Blocks+other.Blocks
	shape.Many = shape.Many || other.Many
	for word := range other.Values {
		shape.word(word)
	}
	if other.Many {
		shape.Values = nil
	}
	if other.Names != nil && shape.Names == nil {
		shape.Names = map[string]bool{}
	}
	maps.Copy(shape.Names, other.Names)
	for key, below := range other.Keys {
		if shape.Keys == nil {
			shape.Keys = map[string]*Shape{}
		}
		if shape.Keys[key] == nil {
			shape.Keys[key] = &Shape{}
		}
		shape.Keys[key].merge(below)
	}
}
