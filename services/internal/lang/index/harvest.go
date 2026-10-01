// harvest.go reads one file into records. It decides nothing about the language: which databases a
// file may define is the schema's and the registry's (DatabasesAt, ByKeyword, FolderRow, FileTypes,
// Key.Defines), what a written word is is the reader's (Key, Value, Below), and which commands make
// a name is the schema's hand table (NameCommandsOf). The harvest only walks, and writes down where
// things are.
//
// A scan harvests every file twice. With no Found the reader cannot say what a word names, so the
// first walk keeps definitions alone; the second, with every definition of the install behind
// Found, keeps the references and edges.
package index

import (
	"cmp"
	"maps"
	"path"
	"regexp"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/loc"
	"paradox-modding-tools/services/internal/lang/schema"
	"paradox-modding-tools/services/internal/lang/source"
)

// nothing is a Found that has seen nothing: the first walk's.
type nothing struct{}

func (nothing) Def(database, name string) bool { return false }
func (nothing) Loc(key string) bool            { return false }
func (nothing) File(name string) bool          { return false }
func (nothing) DefinedBy(name string) string   { return "" }

// locMarks is a loc value's texticon, @name!, and its text format, #name.
var locMarks = regexp.MustCompile(`@(\w+)!|#(\w+)`)

// walk is one file being harvested.
type walk struct {
	reader        schema.Reader
	records       *Records
	lines         *jomini.LineIndex
	refs          bool           // the second walk: references and edges are kept
	info          *game.GameInfo // the game's registry; nil for none
	row           game.Folder    // the registry's row for the file's folder
	ext           string         // the file's extension, lower-cased, without its dot: gui
	fileType      game.FileType  // the registry's row for the extension
	database      string         // the database of the top-level definition being walked, and its name
	top           string
	saved         map[string]string // the scopes saved so far in the definition, by name, to the scope saved
	around        string            // the nested definition the walk is inside, "" at the top one's own level
	met           map[string]bool   // the parameters of the scripted effect being walked that have been met
	hasParameters bool              // its database takes parameters
	call          bool              // the word being written down is a scripted effect written as a key
	reused        *schema.Key       // the key a scripted effect's block is read at, whose keys got their scopes outside the call
	body          bool              // the block being walked is a trigger, effect or modifier block, or inside one
	listed        bool              // the word is a list entry: a bare word, or a number's value (100 = some_event)
	at            int               // a probe's offset: the walk takes only the statements around it; -1 for a whole file
	probe         *Descent
	visit         func(Level) // a visit: called at every statement of the whole file
}

// Descent is what the one descent found on its way to an offset of a file: the definition the
// offset is inside, every statement around it with the key it stands at, and the scopes inside the
// innermost block that holds the offset.
type Descent struct {
	Database string // the database of the definition the offset is inside; "" outside one
	Top      string // that definition's name
	Chain    []Level
	Scopes   Scopes
}

// Scopes is the scopes a statement runs in: its entry's root, this, the one before the last move, the
// saved ones by name to their scope, and whether a scope rests on a guess.
type Scopes struct {
	Root    string            `json:"root,omitempty"`
	This    string            `json:"this,omitempty"`
	Prev    string            `json:"prev,omitempty"`
	Saved   map[string]string `json:"saved,omitempty"`
	Guessed bool              `json:"guessed,omitempty"`
}

// Level is one statement around the offset: what its key is, the key it is written at and the
// key below it, and the scopes it runs in.
type Level struct {
	Statement jomini.Statement
	Database  string      // the database whose shape At belongs to; a nested database's inside its parent's
	At        *schema.Key // the key the statement is written at
	Below     *schema.Key // the key under its key
	Key       schema.Word // what the key is; TypeNone for a bare word
	Scopes    Scopes      // the scopes the statement runs in
	Via       string      // the nearest command around it
}

// contains reports whether a statement, or a bare word declaring the statement after it, holds it.
func (walk *walk) contains(stmts []jomini.Statement, i int) bool {
	if walk.at < 0 {
		return true
	}
	span := stmts[i].NodeRange()
	if span.Start <= walk.at && walk.at <= span.End {
		return true
	}
	_, word := stmts[i].(*jomini.ValueStmt)
	if next := i + 1; word && next < len(stmts) {
		span = stmts[next].NodeRange()
		return span.Start <= walk.at && walk.at <= span.End
	}
	return false
}

// level records a statement around the probe's offset, or hands every statement to the visit.
func (walk *walk) level(st jomini.Statement, database *schema.Database, key, below *schema.Key, word schema.Word, scopes Scopes, via string) {
	if walk.visit == nil && walk.probe == nil {
		return
	}
	scopes.Saved = walk.saved
	level := Level{Statement: st, At: key, Below: below, Key: word, Scopes: scopes, Via: via}
	if database != nil {
		level.Database = database.Name
	}
	if walk.visit != nil {
		walk.visit(level)
	}
	if walk.probe != nil && walk.at >= 0 {
		span := st.NodeRange()
		if span.Start <= walk.at && walk.at <= span.End {
			walk.probe.Chain = append(walk.probe.Chain, level)
		}
	}
}

// Harvest reads one file from its text, which may be an open buffer's. With reader.Found nil it
// keeps definitions alone.
func Harvest(file source.File, text string, reader schema.Reader) *Records {
	return newWalk(file, reader, -1).read(text).records
}

// Probe makes the same descent as Harvest along the one chain of statements that holds the offset,
// and gives what it passed. It takes the session's parse, so an editor request parses nothing.
func Probe(file source.File, parsed *jomini.Result, reader schema.Reader, offset int) *Descent {
	word := newWalk(file, reader, offset)
	word.probe = &Descent{}
	word.lines = parsed.Lines()
	word.file(parsed.Root)
	word.probe.Database, word.probe.Top = word.database, word.top
	return word.probe
}

// Visit makes the same descent as Harvest over a whole file, hands every statement to visit with
// the key it stands at, and gives the file's records. It takes the session's parse too.
func Visit(file source.File, parsed *jomini.Result, reader schema.Reader, visit func(Level)) *Records {
	word := newWalk(file, reader, -1)
	word.visit = visit
	word.lines = parsed.Lines()
	word.file(parsed.Root)
	return word.records
}

// newWalk is a walk of one file, whole or to one offset, with the registry's rows for its folder
// and extension.
func newWalk(file source.File, reader schema.Reader, at int) *walk {
	word := &walk{reader: reader, records: &Records{File: file}, refs: reader.Found != nil, at: at}
	word.ext = strings.TrimPrefix(strings.ToLower(path.Ext(file.Rel)), ".")
	if word.info = game.Get(reader.Schema.Game); word.info != nil {
		word.row, _ = schema.FolderRow(word.info, file.Rel)
		word.fileType = word.info.FileTypes["."+word.ext]
	}
	if !word.refs {
		word.reader.Found = nothing{}
	}
	return word
}

// Readable reports a file the harvest opens: script, loc, a table, a list, and a JSON file a
// database lists (EU5's Wwise manifest); images, meshes and sounds are listed, never read.
func Readable(file source.File, schema *schema.Schema) bool {
	switch file.Format {
	case source.Script, source.Loc, source.Table, source.List:
		return true
	case source.JSON:
		for _, database := range schema.DatabasesAt(file.Rel) {
			if database.Format == source.JSON {
				return true
			}
		}
	}
	return false
}

// read walks the file's text by its format.
func (walk *walk) read(text string) *walk {
	file := walk.records.File
	if !Readable(file, walk.reader.Schema) {
		return walk
	}
	walk.lines = jomini.NewLineIndex(text)

	switch file.Format {
	case source.Loc:
		walk.records.Loc = loc.Parse(text).Entries
		for _, entry := range walk.records.Loc {
			for _, in := range loc.Interps(entry.Value, entry.ValueRange.Start) {
				if walk.reader.Found.Loc(in.Key) {
					walk.ref(schema.Word{Type: schema.WordLoc, Name: in.Key}, in.KeyRange.Start, "")
				}
			}
			for _, mark := range locMarks.FindAllStringSubmatchIndex(entry.Value, -1) {
				database, start, end := "gui.texticon", mark[2], mark[3]
				if start < 0 {
					database, start, end = "gui.format", mark[4], mark[5]
				}
				walk.ref(schema.Word{Type: schema.WordReference, Database: database, Name: entry.Value[start:end]}, entry.ValueRange.Start+start, "")
			}
		}
	case source.Table, source.List:
		walk.rows(text)
	case source.JSON:
		for _, database := range walk.reader.Schema.DatabasesAt(file.Rel) {
			for _, name := range schema.SoundEvents([]byte(text)) {
				walk.def(Def{Database: database.Name, Name: name}, 0)
			}
		}
	case source.Script:
		parsed := jomini.Parse(text)
		walk.lines = parsed.Lines()
		walk.file(parsed.Root)
	}
	return walk
}

// def writes one definition down at a byte offset.
func (walk *walk) def(def Def, offset int) {
	at := walk.lines.PositionAt(offset)
	def.Line, def.Col = int32(at.Line), int32(at.Character)
	walk.records.Defs = append(walk.records.Defs, def)
}

// ref writes a reference down, and an edge where the site fires or invokes what it names: a
// scripted effect or trigger written as a key, or a fired definition named at a declared effect
// (trigger_event = x) or as a list entry outside any body (an on_action's events = { a b } and
// random_events = { 100 = a }; holder = 168673 is no list entry).
func (walk *walk) ref(word schema.Word, offset int, via string) {
	if !walk.refs || word.Name == "" || (word.Type != schema.WordReference && word.Type != schema.WordLoc && word.Type != schema.WordFile) || (word.Type == schema.WordReference && word.Database == "") {
		return
	}
	// a guess at the entry being walked is its own name (name = v_temple_mesh in its block)
	if word.Guessed && word.Database == walk.database && word.Name == walk.top {
		return
	}
	at := walk.lines.PositionAt(offset)
	walk.records.Refs = append(walk.records.Refs, Ref{Type: word.Type, Database: word.Database, Name: word.Name, Parent: word.Parent, Declared: word.Declared, Line: int32(at.Line), Col: int32(at.Character)})
	_, effect := walk.reader.Schema.Dumps.Effects[via]
	fired := walk.reader.Schema.Databases[word.Database] != nil && walk.reader.Schema.Databases[word.Database].Fired
	if word.Type == schema.WordReference && walk.top != "" && (walk.call || (fired && (effect || (walk.listed && !walk.body)))) {
		walk.records.Edges = append(walk.records.Edges, Edge{FromDatabase: walk.database, From: walk.top, ToDatabase: word.Database, To: word.Name, Via: via, Line: int32(at.Line)})
	}
}

// rows reads a table or a list: the database that lists this file names its definitions by a
// column, or by what follows the GUID on a line.
func (walk *walk) rows(text string) {
	for _, database := range walk.reader.Schema.DatabasesAt(walk.records.File.Rel) {
		offset := 0
		for _, line := range strings.SplitAfter(text, "\n") {
			row := strings.TrimSpace(line)
			cells := strings.Split(row, ";")
			switch {
			case row == "" || strings.HasPrefix(row, "#"):
			case database.Naming.Form == "column" && database.Naming.Column < len(cells):
				walk.def(Def{Database: database.Name, Name: strings.TrimSpace(cells[database.Naming.Column])}, offset)
			case database.Naming.Form == "line":
				_, name, _ := strings.Cut(row, " ")
				walk.def(Def{Database: database.Name, Name: strings.TrimSpace(name)}, offset)
			}
			offset += len(line)
		}
	}
}

// file walks a script file: a ByFile history file is one entry named by its stem, the history of
// another database's entry where it has an Of; a history folder with no Of and an Unnamed one have
// no entry at their top; any other file is read by its top level.
func (walk *walk) file(root *jomini.Root) {
	var database *schema.Database
	if databases := walk.reader.Schema.DatabasesAt(walk.records.File.Rel); len(databases) > 0 {
		database = databases[0]
	}
	whole := &jomini.Block{Statements: root.Statements}
	stem := strings.TrimSuffix(path.Base(walk.records.File.Rel), path.Ext(walk.records.File.Rel))
	walk.saved = map[string]string{}
	switch {
	case database != nil && database.Naming.Form == "file" && len(database.HistoryOf) > 0:
		walk.database, walk.top = database.Name, stem
		walk.ref(walk.historyOf(database, stem), 0, "")
		walk.inside(database, database.Entry, whole, Scopes{}, "")
	case database != nil && database.Naming.Form == "file":
		walk.database, walk.top = database.Name, stem
		walk.def(Def{Database: database.Name, Name: stem}, 0)
		walk.inside(database, database.Entry, whole, Scopes{}, "")
	case database != nil && (walk.row.Unnamed || walk.row.History && len(walk.row.Of) == 0):
		walk.database, walk.top = database.Name, ""
		walk.inside(database, database.Entry, whole, Scopes{}, "")
	default:
		walk.tops(database, root.Statements)
	}
}

// tops walks a file's top level, or a wrapper's keys in its place: constants, declarations, a file
// type's blocks named by its NameKey, and the entries of database by its naming form; a history
// database's entries name the entries of the one it is the history of.
func (walk *walk) tops(database *schema.Database, stmts []jomini.Statement) {
	for i := 0; i < len(stmts); i++ {
		if !walk.contains(stmts, i) {
			continue
		}
		for next := walk.declared(true, stmts, i); next != i; next = walk.declared(true, stmts, i) {
			i = next
		}
		if i >= len(stmts) {
			break
		}
		st, ok := stmts[i].(*jomini.Assignment)
		if !ok {
			continue
		}
		mode, key := walk.mode(st.Key.Text)
		block := jomini.BlockOf(st.Value)
		if name, isConstant := jomini.Constant(key); isConstant {
			walk.def(Def{Database: "constants", Name: name}, st.Key.Range.Start)
			walk.level(st, nil, nil, nil, schema.Word{Type: schema.WordConstant, Name: name}, Scopes{}, "")
			continue
		}
		if block != nil && slices.Contains(walk.row.Wrappers, strings.ToLower(key)) {
			walk.level(st, nil, nil, nil, schema.Word{Type: schema.WordKey, Name: key}, Scopes{}, "")
			walk.tops(database, block.Statements)
			continue
		}

		typed := walk.reader.Schema.Databases[walk.ext+"."+strings.ToLower(key)]
		form := ""
		if database != nil {
			form = database.Naming.Form
		}
		switch {
		case typed != nil && block != nil && walk.fileType.NameKey != "":
			walk.define(typed, walk.wordAt(block, walk.fileType.NameKey), mode, st, block)
		case database != nil && len(database.HistoryOf) > 0:
			walk.database, walk.top, walk.saved = database.Name, key, map[string]string{}
			word := walk.historyOf(database, key)
			walk.ref(word, st.Key.Range.Start, "")
			walk.level(st, database, nil, database.Entry, word, Scopes{}, "")
			walk.inside(database, database.Entry, block, Scopes{}, "")
		case form == "value" && block != nil:
			// a macro's definition = "GetString_int32(Arg0)" defines GetString_int32
			name, _, _ := strings.Cut(walk.wordAt(block, database.Naming.NameKey), "(")
			walk.define(database, name, mode, st, block)
		case (form == "key" || form == "event_id" && jomini.IsEventID(key) || form == "number" && jomini.Literal(key) == jomini.LitNumber) && !st.Key.Quoted:
			walk.define(database, key, mode, st, block)
			if block == nil {
				walk.value(database.Entry, st, "")
			}
		default:
			walk.database, walk.top, walk.saved = "", "", map[string]string{}
			walk.level(st, nil, nil, nil, schema.Word{Type: schema.WordUnknown, Name: key}, Scopes{}, "")
			walk.inside(nil, nil, block, Scopes{}, "")
		}
	}
}

// mode splits an entry mode off a key, only where it is one the game declares: EU5's
// INJECT_OR_CREATE:headwear, never IDEAL_UNIFORM:torsos or a mod's C:NOR.
func (walk *walk) mode(key string) (string, string) {
	mode, name := jomini.Mode(key)
	if !slices.ContainsFunc(walk.reader.Schema.EntryModes, func(known game.EntryMode) bool { return known.Word == mode }) {
		return "", key
	}
	return mode, name
}

// historyOf is the entry a history entry is named after: of the first of HistoryOf's databases
// that has the name, else of the first.
func (walk *walk) historyOf(database *schema.Database, name string) schema.Word {
	of := database.HistoryOf[0]
	for _, candidate := range database.HistoryOf {
		if walk.reader.Found.Def(candidate, name) {
			of = candidate
			break
		}
	}
	return schema.Word{Type: schema.WordReference, Database: of, Name: name}
}

// declared reads a declaration at i, a bare word the schema knows before a named block
// (type x = button { }, scripted_trigger x = { }), and gives the position to go on from. A
// database named by key is declared so at the top of a file only, and a file type's in its own
// files. A file type's Uses word (blockoverride "x" { }) names an entry of its database, and the
// block is walked next as any other.
func (walk *walk) declared(top bool, stmts []jomini.Statement, i int) int {
	if i+1 >= len(stmts) {
		return i
	}
	word, isWord := stmts[i].(*jomini.ValueStmt)
	if !isWord {
		return i
	}
	scalar, _ := word.Value.(*jomini.Scalar)
	next, _ := stmts[i+1].(*jomini.Assignment)
	if scalar == nil || scalar.Quoted || next == nil || jomini.BlockOf(next.Value) == nil {
		return i
	}
	if used := walk.fileType.Uses[scalar.Text]; used != "" {
		walk.ref(schema.Word{Type: schema.WordReference, Database: walk.ext + "." + used, Name: next.Key.Text}, next.Key.Range.Start, scalar.Text)
		return i + 1
	}
	database := walk.reader.Schema.ByKeyword(scalar.Text)
	declaration := database != nil && database.Naming.Form == "declaration"
	if database == nil || !declaration && !top || declaration && !strings.HasPrefix(database.Name, walk.ext+".") {
		return i
	}
	outerKind, outerTop, outerSaved := walk.database, walk.top, walk.saved
	walk.define(database, next.Key.Text, "", next, jomini.BlockOf(next.Value))
	if !top {
		walk.database, walk.top, walk.saved = outerKind, outerTop, outerSaved
	}
	return i + 2
}

// define writes a definition of database down and walks what it holds from its root scope.
func (walk *walk) define(database *schema.Database, name, mode string, st *jomini.Assignment, block *jomini.Block) {
	if name == "" {
		return
	}
	walk.database, walk.top, walk.met, walk.hasParameters = database.Name, name, map[string]bool{}, database.Parameters
	walk.body, walk.saved = database.Entry != nil && database.Entry.Block != schema.BlockNone, map[string]string{}
	walk.def(Def{Database: database.Name, Name: name, Mode: mode}, st.Key.Range.Start)
	root, guessed := walk.root(database, name, block)
	scopes := Scopes{Root: root, This: root, Guessed: guessed}
	walk.level(st, database, nil, database.Entry, schema.Word{Type: schema.WordReference, Database: database.Name, Name: name}, scopes, "")
	walk.inside(database, database.Entry, block, scopes, "")
}

// root is the scope an entry starts in: the docs' root on its database's entry; an on_action's
// declared scope; an event's scope key, then that key's documented default; else the wiki's Root,
// its key's value looked up, else the longest declared scope the value starts with before a _ (Vic3
// country_event), which is a guess.
func (walk *walk) root(database *schema.Database, name string, block *jomini.Block) (string, bool) {
	dumps, entry := walk.reader.Schema.Dumps, database.Entry
	if entry != nil && walk.scoped(entry.Scope) != "" {
		return entry.Scope, false
	}
	if action, ok := dumps.OnActions[name]; walk.row.OnActions && ok && walk.scoped(action.Scope) != "" {
		return action.Scope, false
	}
	if walk.row.Events {
		if scope := walk.wordAt(block, "scope"); walk.scoped(scope) != "" {
			return scope, false
		}
		if entry != nil && entry.Keys["scope"] != nil && walk.scoped(entry.Keys["scope"].Default) != "" {
			return entry.Keys["scope"].Default, false
		}
	}
	if database.Root.Key == "" {
		return "", false
	}
	value := walk.wordAt(block, database.Root.Key)
	if scope, ok := database.Root.Scopes[value]; ok {
		return scope, true
	}
	longest := ""
	for scope := range dumps.Scopes {
		if strings.HasPrefix(value, scope+"_") && len(scope) > len(longest) {
			longest = scope
		}
	}
	return longest, longest != ""
}

// wordAt is the word a block writes at one of its keys, "" where it writes none.
func (walk *walk) wordAt(block *jomini.Block, key string) string {
	if block == nil || key == "" {
		return ""
	}
	for _, st := range block.Statements {
		if assignment, ok := st.(*jomini.Assignment); ok && assignment.Key.Text == key {
			if scalar, ok := assignment.Value.(*jomini.Scalar); ok {
				return scalar.Text
			}
		}
	}
	return ""
}

// inside walks one block of a definition of database, at the key that block is an instance of.
// scopes are the scopes its statements run in, with the ones the docs say the block saves; via is
// the nearest command around it.
func (walk *walk) inside(database *schema.Database, key *schema.Key, block *jomini.Block, scopes Scopes, via string) {
	if block == nil {
		return
	}
	if key != nil {
		maps.Copy(walk.saved, key.Saved)
	}
	// the offset is inside the block, whose closing brace may not be written yet (-1)
	if walk.probe != nil && walk.at > block.OpenBrace && (walk.at <= block.CloseBrace || block.CloseBrace < 0) {
		walk.probe.Scopes, walk.probe.Scopes.Saved = scopes, walk.saved
	}
	stmts := block.Statements
	for i := 0; i < len(stmts); i++ {
		if !walk.contains(stmts, i) {
			continue
		}
		for next := walk.declared(false, stmts, i); next != i; next = walk.declared(false, stmts, i) {
			i = next
		}
		if i >= len(stmts) {
			break
		}
		switch st := stmts[i].(type) {
		case *jomini.ValueStmt:
			if scalar, ok := st.Value.(*jomini.Scalar); ok {
				walk.parameters(scalar.Text, scalar.Range.Start)
				walk.listed = true
				walk.ref(walk.word(key, "", scalar), scalar.Range.Start, via)
				walk.listed = false
			}
			walk.level(st, database, key, nil, schema.Word{}, scopes, via)
			walk.inside(database, nil, jomini.BlockOf(st.Value), scopes, via)
		case *jomini.Assignment:
			walk.statement(database, key, st, scopes, via)
		}
	}
}

// statement reads one key and what is written under it, where it moves the scope from scopes.This.
func (walk *walk) statement(database *schema.Database, at *schema.Key, st *jomini.Assignment, scopes Scopes, via string) {
	key, block := st.Key.Text, jomini.BlockOf(st.Value)
	dumps, scope := walk.reader.Schema.Dumps, scopes.This
	word := walk.reader.Key(at, key)
	// a body's key named like an event target (a message's title = some_key) is a walk between
	// scopes where a block is written under it
	if _, isLink := dumps.EventTargets[key]; isLink && block != nil && word.Type == schema.WordKey && at.Block != schema.BlockNone {
		word = schema.Word{Type: schema.WordScopePath, Declared: true}
	}
	below := walk.reader.Below(database, at, key, word)
	walk.parameters(key, st.Key.Range.Start)
	walk.level(st, database, at, below, word, scopes, via)

	// a file type's block that names an entry: a NameKeyOf key's (texticon = { icon = x }), else a
	// widget below the top (a .gui block with a name)
	if block != nil && walk.fileType.NameKey != "" {
		lower := strings.ToLower(key)
		typed, nameKey := walk.ext+"."+lower, walk.fileType.NameKeyOf[lower]
		if nameKey == "" {
			typed, nameKey = walk.ext+".widget", walk.fileType.WidgetKey
		}
		if name := walk.wordAt(block, nameKey); name != "" {
			walk.def(Def{Database: typed, Name: name}, st.Key.Range.Start)
		}
	}

	// a name of the nested database defined here, after any entry mode a mod writes: a faith in a
	// religion, a setting in a game rule, EU5's INJECT_OR_CREATE:headwear in a gene group
	// Its parent is the definition it sits in: a county's is its kingdom, not its empire
	mode, name := walk.mode(key)
	if at != nil && at.Defines != "" && !st.Key.Quoted && (word.Type == schema.WordUnknown || (word.Type == schema.WordReference && word.Database == at.Defines)) && jomini.IsDefName(name) {
		parent := cmp.Or(walk.around, walk.top)
		walk.def(Def{Database: at.Defines, Name: name, Mode: mode, Parent: parent}, st.Key.Range.Start)
		if walk.probe != nil && len(walk.probe.Chain) > 0 && walk.probe.Chain[len(walk.probe.Chain)-1].Statement == st {
			last := &walk.probe.Chain[len(walk.probe.Chain)-1]
			last.Key, last.Database = schema.Word{Type: schema.WordReference, Database: at.Defines, Name: name, Parent: parent}, at.Defines
		}
		around := walk.around
		walk.around = name
		walk.inside(walk.reader.Schema.Databases[at.Defines], below, block, scopes, via)
		walk.around = around
		return
	}

	moved, reused, guessed := false, (*schema.Key)(nil), scopes.Guessed
	switch word.Type {
	case schema.WordCommand:
		// a command with no declared output is transparent: its block runs where it stands, and no
		// block scope the schema holds says otherwise
		via, moved = word.Name, true
		walk.names(word.Name, st, scope)
		if command, ok := dumps.Effects[word.Name]; ok && command.Out != "" {
			scope, moved = walk.scoped(command.Out), true
		} else if command, ok := dumps.Triggers[word.Name]; ok && command.Out != "" {
			scope, moved = walk.scoped(command.Out), true
		} else if block != nil && slices.ContainsFunc(schema.NameCommandsOf(word.Name), func(row schema.NameCommand) bool { return row.NameKey != "" && !row.Defines }) {
			// an iterator over a script-filled list runs in the list's scope, which nothing declares
			scope, moved = "", true
		}
	case schema.WordScopePath:
		scope, moved = walk.leads(key, scopes), true
	case schema.WordUnknown:
		// a key nothing reads leads to a block whose scope nothing knows
		scope, moved = "", block != nil
	case schema.WordKey:
		// a key written with a block under a name the dump declares (root, any_neighboring_county in
		// a weight's modifier) moves the scope as the declaration says; a wild card (value) is no hop
		if target, isLink := dumps.EventTargets[strings.ToLower(key)]; block != nil && isLink && !target.Wild {
			scope, moved = walk.scoped(target.Out...), true
		} else if _, effect := dumps.Effects[strings.ToLower(key)]; block != nil && effect {
			scope, moved = walk.scoped(dumps.Effects[strings.ToLower(key)].Out), true
		} else if _, trigger := dumps.Triggers[strings.ToLower(key)]; block != nil && trigger {
			scope, moved = walk.scoped(dumps.Triggers[strings.ToLower(key)].Out), true
		}
	case schema.WordReference:
		called := walk.reader.Schema.Databases[word.Database]
		walk.call = called != nil && len(called.CalledIn) > 0
		walk.ref(word, st.Key.Range.Start, key)
		if walk.call && called.Parameters {
			walk.call = false
			walk.arguments(word.Name, block)
			return
		}
		if walk.call {
			// a scripted effect with no parameters is followed by more of the body it is written in:
			// every_adult_child = { add_gold = 1 }
			walk.call, below, reused = false, at, at
		}
		if prefixed, isPrefixed := jomini.SplitPrefixed(key); isPrefixed {
			scope, moved = walk.scoped(dumps.DataTargets[prefixed.Prefix].Out...), true
		} else if database := walk.reader.Schema.Databases[word.Database]; database != nil {
			scope, moved, guessed = walk.scoped(database.Scope), true, guessed || word.Guessed
		}
	}
	// a block's own scope fills in only a declared scope type, only where nothing declared moved it,
	// and not in a scripted effect's block, where the key's keys got it around the call
	if !moved && below != nil && below != at && at != walk.reused && dumps.IsScope(below.Scope) {
		scope = below.Scope
	}
	if _, effect := dumps.Effects[via]; !effect && word.Type != schema.WordCommand && word.Type != schema.WordNumber {
		if _, trigger := dumps.Triggers[via]; !trigger {
			via = key
		}
	}

	if block == nil {
		// a file type's Uses key names an entry of its database (using = x a template, else a type)
		scalar, _ := st.Value.(*jomini.Scalar)
		if used := walk.fileType.Uses[strings.ToLower(key)]; used != "" && scalar != nil {
			named := walk.ext + "." + used
			if !walk.reader.Found.Def(named, scalar.Text) && walk.reader.Found.Def(walk.ext+".type", scalar.Text) {
				named = walk.ext + ".type"
			}
			walk.ref(schema.Word{Type: schema.WordReference, Database: named, Name: scalar.Text}, scalar.Range.Start, key)
			return
		}
		walk.value(below, st, via)
		return
	}
	inner := scopes
	if moved || scope != scopes.This {
		inner.Prev, inner.This, inner.Guessed = scopes.This, scope, guessed
	}
	body, outer := walk.body, walk.reused
	walk.body, walk.reused = body || (below != nil && below.Block != schema.BlockNone), cmp.Or(reused, outer)
	walk.inside(database, below, block, inner, via)
	walk.body, walk.reused = body, outer
}

// scoped is the one scope type the dump declares an output to be, or "": an event target or
// command whose output is a value, a boolean, an unlisted name or several leads to a block whose
// scope nothing knows, and nothing accuses there.
func (walk *walk) scoped(outs ...string) string {
	if len(outs) == 1 && walk.reader.Schema.Dumps.IsScope(outs[0]) {
		return outs[0]
	}
	return ""
}

// leads is the scope type a scope path leads to: root, this and prev read from scopes, a saved scope
// what it was saved as, else the declared output of its last step, an event target or a prefixed
// word's prefix; "" for prev.prev, a $PARAMETER$ step, or a word the dump does not declare.
func (walk *walk) leads(path string, scopes Scopes) string {
	last, targets := path[strings.LastIndexByte(path, '.')+1:], walk.reader.Schema.Dumps.EventTargets
	switch {
	case strings.Contains(path, "$"):
		return ""
	case last == "root":
		return scopes.Root
	case last == "this":
		return scopes.This
	case last == "prev" && path == "prev":
		return scopes.Prev
	case last == "prev":
		return ""
	}
	if prefixed, isPrefixed := jomini.SplitPrefixed(last); isPrefixed {
		if prefixed.Prefix == schema.ScriptMade["saved_scopes"] {
			return walk.saved[prefixed.Name]
		}
		last, targets = prefixed.Prefix, walk.reader.Schema.Dumps.DataTargets
	}
	target := targets[strings.ToLower(last)]
	if target.Wild {
		return ""
	}
	return walk.scoped(target.Out...)
}

// value reads the word written as a statement's value at its key.
func (walk *walk) value(key *schema.Key, st *jomini.Assignment, via string) {
	scalar, ok := st.Value.(*jomini.Scalar)
	if !ok {
		return
	}
	via = cmp.Or(via, st.Key.Text)
	walk.parameters(scalar.Text, scalar.Range.Start)
	walk.listed = jomini.Literal(st.Key.Text) == jomini.LitNumber
	walk.ref(walk.word(key, st.Key.Text, scalar), scalar.Range.Start, via)
	walk.listed = false
}

// word is what a written word is: a sound the game lists where it is an event:/ path or written at
// a sound key (CK3 lists a path's last segment alone), else the reader's reading. A sound is a guess.
func (walk *walk) word(key *schema.Key, written string, scalar *jomini.Scalar) schema.Word {
	isSound := strings.HasPrefix(scalar.Text, "event:/") || walk.info != nil && slices.Contains(walk.info.SoundKeys, strings.ToLower(written))
	if !isSound || !walk.refs {
		return walk.reader.Value(key, scalar.Text, scalar.Quoted)
	}
	for database := range walk.reader.Schema.Databases {
		for _, name := range []string{scalar.Text, "event:/" + path.Base(scalar.Text)} {
			if strings.HasPrefix(database, "sound.") && walk.reader.Found.Def(database, name) {
				return schema.Word{Type: schema.WordReference, Database: database, Name: name, Guessed: true}
			}
		}
	}
	return walk.reader.Value(key, scalar.Text, scalar.Quoted)
}

// names reads a name command: save_scope_as = target makes a saved scope, has_variable = x reads a
// variable, and a command written with a block holds the name at the key its row gives.
func (walk *walk) names(command string, st *jomini.Assignment, scope string) {
	for _, row := range schema.NameCommandsOf(command) {
		scalar, _ := st.Value.(*jomini.Scalar)
		name, offset := "", st.Key.Range.Start
		if scalar != nil {
			name, offset = scalar.Text, scalar.Range.Start
		} else if block := jomini.BlockOf(st.Value); block != nil && row.NameKey != "" {
			name = walk.wordAt(block, row.NameKey)
		}
		if !jomini.IsDefName(name) {
			continue
		}
		if row.Defines {
			made := Def{Database: row.Database, Name: name, Parent: walk.top}
			if row.Database == "saved_scopes" {
				made.Scope, walk.saved[name] = scope, scope
			}
			walk.def(made, offset)
		} else {
			walk.ref(schema.Word{Type: schema.WordReference, Database: row.Database, Name: name}, offset, command)
		}
	}
}

// arguments reads the block of a scripted effect or trigger call: each key is a parameter of the
// one called, and each value is whatever its text makes of it, which nothing here can know.
func (walk *walk) arguments(scripted string, block *jomini.Block) {
	if block == nil {
		return
	}
	for _, st := range block.Statements {
		if assignment, ok := st.(*jomini.Assignment); ok {
			argument := schema.Word{Type: schema.WordReference, Database: "params", Name: assignment.Key.Text, Parent: scripted}
			walk.ref(argument, assignment.Key.Range.Start, scripted)
			walk.level(assignment, nil, nil, nil, argument, Scopes{}, scripted)
			walk.inside(nil, nil, jomini.BlockOf(assignment.Value), Scopes{}, scripted)
		}
	}
}

// parameters reads the $NAME$ parameters in a word of a scripted effect or trigger's text: the
// first sighting of a name is its definition, every later one a reference to it.
func (walk *walk) parameters(text string, offset int) {
	if !walk.hasParameters || !strings.Contains(text, "$") {
		return
	}
	for _, span := range jomini.Interpolations(text) {
		if walk.met[span.Name] {
			walk.ref(schema.Word{Type: schema.WordReference, Database: "params", Name: span.Name, Parent: walk.top}, offset+span.Start, "")
		} else {
			walk.met[span.Name] = true
			walk.def(Def{Database: "params", Name: span.Name, Parent: walk.top}, offset+span.Start)
		}
	}
}
