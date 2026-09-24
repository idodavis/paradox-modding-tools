// harvest.go reads one file into records. It decides nothing about the language: which kinds a file
// may define is the schema's (KindsAt, ByKeyword, Field.Defines), what a written word is is the
// reader's (Key, Value, Below), and which tokens make a name is the schema's hand table
// (NameTokensOf). The harvest only walks, and writes down where things are.
//
// A scan harvests every file twice. With no Found the reader cannot say what a word names, so the
// first walk keeps definitions alone; the second, with every definition of the install behind
// Found, keeps the references and edges.
package index

import (
	"cmp"
	"maps"
	"path"
	"slices"
	"strings"
	"sync"

	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/loc"
	"paradox-modding-tools/services/internal/lang/schema"
	"paradox-modding-tools/services/internal/lang/source"
)

// nothing is a Found that has seen nothing: the first walk's.
type nothing struct{}

func (nothing) Def(kind, name string) bool { return false }
func (nothing) Loc(key string) bool        { return false }
func (nothing) File(name string) bool      { return false }

// fired caches, per schema, the kinds whose definitions can be fired: those that are an effect body
// or hold one directly (an event's immediate, an on_action's effect, a law's on_pass). A
// character's history holds one two blocks down, under a date, and nothing fires a character.
var fired sync.Map

// firedKinds is the kinds of sch that an edge may point at.
func firedKinds(sch *schema.Schema) map[string]bool {
	if known, ok := fired.Load(sch); ok {
		return known.(map[string]bool)
	}
	runs := func(field *schema.Field) bool {
		return field != nil && field.Class == schema.ClassBody && field.Body&schema.BodyEffect != 0
	}
	out := map[string]bool{}
	for name, kind := range sch.Kinds {
		out[name] = kind.Shape != nil && (runs(kind.Shape) || slices.ContainsFunc(slices.Collect(maps.Values(kind.Shape.Members)), runs))
	}
	fired.Store(sch, out)
	return out
}

// walk is one file being harvested.
type walk struct {
	reader  schema.Reader
	records *Records
	lines   *jomini.LineIndex
	refs    bool            // the second walk: references and edges are kept
	fired   map[string]bool // the kinds an edge may point at
	kind    string          // the kind of the top-level definition being walked, and its name
	top     string
	met     map[string]bool // the parameters of the macro being walked that have been met
	isMacro bool            // its kind takes parameters
	call    bool            // the word being written down is a macro written as a key
	body    bool            // the block being walked is a body, or inside one
	listed  bool            // the word is a list entry: a bare word, or a number's value (100 = some_event)
	at      int             // a probe's offset: the walk takes only the statements around it; -1 for a whole file
	probe   *Descent
	visit   func(Level) // a visit: called at every statement of the whole file
}

// Descent is what the one descent found on its way to an offset of a file: the definition the
// offset is inside, and every statement around it with the field it stands at.
type Descent struct {
	Kind  string // the kind of the definition the offset is inside; "" outside one
	Top   string // that definition's name
	Chain []Level
}

// Level is one statement around the offset: what its key is, the field it is written at and the
// field below it, and the scope its block runs in.
type Level struct {
	Statement jomini.Statement
	Kind      *schema.Kind  // the kind whose shape Field belongs to; a nested kind's inside its parent's
	Field     *schema.Field // the field the statement is written at
	Below     *schema.Field // the field under its key
	Key       schema.Value  // what the key is; TypeNone for a bare word
	Scope     string        // the scope type the statement runs in
	Via       string        // the nearest token around it
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
func (walk *walk) level(st jomini.Statement, kind *schema.Kind, field, below *schema.Field, value schema.Value, scope, via string) {
	if walk.visit != nil {
		walk.visit(Level{Statement: st, Kind: kind, Field: field, Below: below, Key: value, Scope: scope, Via: via})
	}
	if walk.probe != nil && walk.at >= 0 {
		span := st.NodeRange()
		if span.Start <= walk.at && walk.at <= span.End {
			walk.probe.Chain = append(walk.probe.Chain, Level{Statement: st, Kind: kind, Field: field, Below: below, Key: value, Scope: scope, Via: via})
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
	word.probe.Kind, word.probe.Top = word.kind, word.top
	return word.probe
}

// Visit makes the same descent as Harvest over a whole file, hands every statement to visit with
// the field it stands at, and gives the file's records. It takes the session's parse too.
func Visit(file source.File, parsed *jomini.Result, reader schema.Reader, visit func(Level)) *Records {
	word := newWalk(file, reader, -1)
	word.visit = visit
	word.lines = parsed.Lines()
	word.file(parsed.Root)
	return word.records
}

// newWalk is a walk of one file, whole or to one offset.
func newWalk(file source.File, reader schema.Reader, at int) *walk {
	word := &walk{reader: reader, records: &Records{File: file}, refs: reader.Found != nil, fired: firedKinds(reader.Schema), at: at}
	if !word.refs {
		word.reader.Found = nothing{}
	}
	return word
}

// Readable reports a format the harvest opens; images, meshes and sounds are listed, never read.
func Readable(format source.Format) bool {
	return format == source.Script || format == source.Loc || format == source.Table || format == source.List
}

// read walks the file's text by its format.
func (walk *walk) read(text string) *walk {
	file := walk.records.File
	if !Readable(file.Format) {
		return walk
	}
	walk.lines = jomini.NewLineIndex(text)

	switch file.Format {
	case source.Loc:
		walk.records.Loc = loc.Parse(text).Entries
		for _, entry := range walk.records.Loc {
			for _, in := range loc.Interps(entry.Value, entry.ValueRange.Start) {
				if walk.reader.Found.Loc(in.Key) {
					walk.ref(schema.Value{Type: schema.TypeLoc, Name: in.Key}, in.KeyRange.Start, "")
				}
			}
		}
	case source.Table, source.List:
		walk.rows(text)
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

// ref writes a reference down, and an edge where the site fires or invokes what it names: a macro
// written as a key, or a fireable definition named at a declared effect (trigger_event = x) or as a
// list entry outside any body (an on_action's events = { a b } and random_events = { 100 = a };
// holder = 168673 is no list entry).
func (walk *walk) ref(value schema.Value, offset int, via string) {
	if !walk.refs || value.Name == "" || (value.Type != schema.TypeRef && value.Type != schema.TypeLoc && value.Type != schema.TypeFile) || (value.Type == schema.TypeRef && value.Kind == "") {
		return
	}
	at := walk.lines.PositionAt(offset)
	walk.records.Refs = append(walk.records.Refs, Ref{Type: value.Type, Kind: value.Kind, Name: value.Name, Parent: value.Parent, Declared: value.Declared, Line: int32(at.Line), Col: int32(at.Character)})
	_, effect := walk.reader.Vocab.Effects[via]
	if value.Type == schema.TypeRef && walk.top != "" && (walk.call || (walk.fired[value.Kind] && (effect || (walk.listed && !walk.body)))) {
		walk.records.Edges = append(walk.records.Edges, Edge{FromKind: walk.kind, From: walk.top, ToKind: value.Kind, To: value.Name, Via: via, Line: int32(at.Line)})
	}
}

// rows reads a table or a list: the kind that lists this file names its definitions by a column, or
// by what follows the GUID on a line.
func (walk *walk) rows(text string) {
	for _, kind := range walk.reader.Schema.KindsAt(walk.records.File.Rel) {
		offset := 0
		for _, line := range strings.SplitAfter(text, "\n") {
			row := strings.TrimSpace(line)
			cells := strings.Split(row, ";")
			switch {
			case row == "" || strings.HasPrefix(row, "#"):
			case kind.Naming.Form == "column" && kind.Naming.Column < len(cells):
				walk.def(Def{Kind: kind.Name, Name: strings.TrimSpace(cells[kind.Naming.Column])}, offset)
			case kind.Naming.Form == "line":
				_, name, _ := strings.Cut(row, " ")
				walk.def(Def{Kind: kind.Name, Name: strings.TrimSpace(name)}, offset)
			}
			offset += len(line)
		}
	}
}

// file walks a script file's top level: constants, declarations, and the names of the kinds the
// schema says a file at this path may define.
func (walk *walk) file(root *jomini.Root) {
	var named, about *schema.Kind
	byWord := map[string]*schema.Kind{}
	for _, kind := range walk.reader.Schema.KindsAt(walk.records.File.Rel) {
		switch {
		case kind.Naming.Form == "member":
			byWord[kind.Naming.Keyword] = kind
		case kind.Subject != "":
			about = kind
		case kind.Naming.Form == "key" || kind.Naming.Form == "number" || kind.Naming.Form == "event_id":
			named = kind
		}
	}
	if about != nil && about.Naming.Form == "file" {
		base := path.Base(walk.records.File.Rel)
		walk.ref(schema.Value{Type: schema.TypeRef, Kind: about.Subject, Name: strings.TrimSuffix(base, path.Ext(base))}, 0, "")
	}

	stmts := root.Statements
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
		mode, key := jomini.Mode(st.Key.Text)
		block := jomini.BlockOf(st.Value)
		if name, isConstant := jomini.Constant(key); isConstant {
			walk.def(Def{Kind: "constants", Name: name}, st.Key.Range.Start)
			walk.level(st, nil, nil, nil, schema.Value{Type: schema.TypeConstant, Name: name}, "", "")
			continue
		}

		switch kind := byWord[key]; {
		case kind != nil && block != nil:
			walk.define(kind, walk.member(block, kind.Naming.Member), mode, st, block)
		case about != nil && about.Naming.Form != "file":
			walk.kind, walk.top = about.Name, key
			cited := schema.Value{Type: schema.TypeRef, Kind: about.Subject, Name: key}
			walk.ref(cited, st.Key.Range.Start, "")
			walk.level(st, about, nil, about.Shape, cited, "", "")
			walk.inside(about, about.Shape, block, "", "")
		case named != nil && !st.Key.Quoted && (named.Naming.Form != "event_id" || jomini.IsEventID(key)) && (named.Naming.Form != "number" || jomini.Literal(key) == jomini.LitNumber):
			walk.define(named, key, mode, st, block)
			if block == nil {
				walk.value(named.Shape, st, "")
			}
		default:
			walk.kind, walk.top = "", ""
			walk.level(st, nil, nil, nil, schema.Value{Type: schema.TypeUnresolved, Name: key}, "", "")
			walk.inside(nil, nil, block, "", "")
		}
	}
}

// declared reads a declaration at i, a bare word the schema knows before a named block
// (type x = button { }, scripted_trigger x = { }), and gives the position to go on from. A kind
// named by key is declared so at the top of a file only.
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
	kind := walk.reader.Schema.ByKeyword(scalar.Text)
	if kind == nil || (kind.Naming.Form != "declaration" && !top) {
		return i
	}
	outerKind, outerTop := walk.kind, walk.top
	walk.define(kind, next.Key.Text, "", next, jomini.BlockOf(next.Value))
	if !top {
		walk.kind, walk.top = outerKind, outerTop
	}
	return i + 2
}

// define writes a definition of kind down and walks what it holds.
func (walk *walk) define(kind *schema.Kind, name, mode string, st *jomini.Assignment, block *jomini.Block) {
	if name == "" {
		return
	}
	walk.kind, walk.top, walk.met, walk.isMacro, walk.body = kind.Name, name, map[string]bool{}, kind.Params, kind.Shape != nil && kind.Shape.Class == schema.ClassBody
	walk.def(Def{Kind: kind.Name, Name: name, Mode: mode, Label: walk.member(block, kind.LabelMember)}, st.Key.Range.Start)
	scope := ""
	if kind.Shape != nil {
		scope = walk.scoped(kind.Shape.BodyScope)
	}
	walk.level(st, kind, nil, kind.Shape, schema.Value{Type: schema.TypeRef, Kind: kind.Name, Name: name}, scope, "")
	walk.inside(kind, kind.Shape, block, scope, "")
}

// member is the word a block writes at one of its members, "" where it writes none.
func (walk *walk) member(block *jomini.Block, key string) string {
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

// inside walks one block of a definition of kind, at the field that block is an instance of. scope
// is the scope type its statements run in where that is known; via is the nearest token around it.
func (walk *walk) inside(kind *schema.Kind, field *schema.Field, block *jomini.Block, scope, via string) {
	if block == nil {
		return
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
				walk.ref(walk.reader.Value(field, scalar.Text, scalar.Quoted), scalar.Range.Start, via)
				walk.listed = false
			}
			walk.level(st, kind, field, nil, schema.Value{}, scope, via)
			walk.inside(kind, nil, jomini.BlockOf(st.Value), scope, via)
		case *jomini.Assignment:
			walk.statement(kind, field, st, scope, via)
		}
	}
}

// statement reads one key and what is written under it.
func (walk *walk) statement(kind *schema.Kind, field *schema.Field, st *jomini.Assignment, scope, via string) {
	key, block := st.Key.Text, jomini.BlockOf(st.Value)
	value := walk.reader.Key(field, key)
	below := walk.reader.Below(kind, field, key, value)
	walk.parameters(key, st.Key.Range.Start)
	walk.level(st, kind, field, below, value, scope, via)

	// a name of the nested kind defined here: a faith in a religion, a setting in a game rule
	if field != nil && field.Defines != "" && !st.Key.Quoted && (value.Type == schema.TypeUnresolved || (value.Type == schema.TypeRef && value.Kind == field.Defines)) && jomini.IsDefName(key) {
		walk.def(Def{Kind: field.Defines, Name: key, Parent: walk.top}, st.Key.Range.Start)
		if walk.probe != nil && len(walk.probe.Chain) > 0 && walk.probe.Chain[len(walk.probe.Chain)-1].Statement == st {
			last := &walk.probe.Chain[len(walk.probe.Chain)-1]
			last.Key, last.Kind = schema.Value{Type: schema.TypeRef, Kind: field.Defines, Name: key, Parent: walk.top}, walk.reader.Schema.Kinds[field.Defines]
		}
		walk.inside(walk.reader.Schema.Kinds[field.Defines], below, block, scope, via)
		return
	}

	moved := false
	switch value.Type {
	case schema.TypeToken:
		// a token with no declared output is transparent: its block runs where it stands, and no
		// learned body scope says otherwise
		via, moved = value.Name, true
		walk.names(value.Name, st, scope)
		if token, ok := walk.reader.Vocab.Effects[value.Name]; ok && token.Out != "" {
			scope, moved = walk.scoped(token.Out), true
		} else if token, ok := walk.reader.Vocab.Triggers[value.Name]; ok && token.Out != "" {
			scope, moved = walk.scoped(token.Out), true
		} else if block != nil && slices.ContainsFunc(schema.NameTokensOf(value.Name), func(row schema.NameToken) bool { return row.Member != "" && !row.Defines }) {
			// an iterator over a script-filled list runs in the list's scope, which nothing declares
			scope, moved = "", true
		}
	case schema.TypeScopePath:
		scope, moved = walk.leads(key), true
	case schema.TypeUnresolved:
		// a key nothing reads leads to a block whose scope nothing knows
		scope, moved = "", block != nil
	case schema.TypeMember:
		// a learned member written with a block under a name the dump declares (root,
		// any_neighboring_county in a weight's modifier) moves the scope as the declaration says; a
		// wild card (value) is no hop
		if link, isLink := walk.reader.Vocab.Links[strings.ToLower(key)]; block != nil && isLink && !link.Wild {
			scope, moved = walk.scoped(link.Out), true
		} else if _, effect := walk.reader.Vocab.Effects[strings.ToLower(key)]; block != nil && effect {
			scope, moved = walk.scoped(walk.reader.Vocab.Effects[strings.ToLower(key)].Out), true
		} else if _, trigger := walk.reader.Vocab.Triggers[strings.ToLower(key)]; block != nil && trigger {
			scope, moved = walk.scoped(walk.reader.Vocab.Triggers[strings.ToLower(key)].Out), true
		}
	case schema.TypeRef:
		called := walk.reader.Schema.Kinds[value.Kind]
		walk.call = called != nil && called.Macro != 0
		walk.ref(value, st.Key.Range.Start, key)
		if walk.call && called.Params {
			walk.call = false
			walk.arguments(value.Name, block)
			return
		}
		if walk.call {
			// a macro with no parameters is followed by more of the body it is written in:
			// every_adult_child = { add_gold = 1 }
			walk.call, below = false, field
		}
		if cite, isCite := jomini.Cite(key); isCite {
			scope, moved = walk.scoped(walk.reader.Vocab.Links[cite.Prefix].Out), true
		} else if cited := walk.reader.Schema.Kinds[value.Kind]; cited != nil {
			scope, moved = walk.scoped(cited.ScopeType), true
		}
	}
	// a learned body scope fills in only a declared scope type, and only where nothing declared moved
	// it
	if !moved && below != nil && below != field && walk.reader.Vocab.IsScopeType(below.BodyScope) {
		scope = below.BodyScope
	}
	if _, effect := walk.reader.Vocab.Effects[via]; !effect && value.Type != schema.TypeToken && value.Type != schema.TypeNumber {
		if _, trigger := walk.reader.Vocab.Triggers[via]; !trigger {
			via = key
		}
	}

	if block == nil {
		walk.value(below, st, via)
		return
	}
	body := walk.body
	walk.body = body || (below != nil && below.Class == schema.ClassBody)
	walk.inside(kind, below, block, scope, via)
	walk.body = body
}

// scoped is a scope type the dump declares, or "": a link or token whose output is a value, a
// boolean or an unlisted name leads to a block whose scope nothing knows, and nothing accuses
// there.
func (walk *walk) scoped(out string) string {
	if walk.reader.Vocab.IsScopeType(out) {
		return out
	}
	return ""
}

// leads is the scope type a scope path leads to: the declared output of its last step, a link or a
// cite's prefix; "" where the last step is a saved scope or a word the dump does not declare.
func (walk *walk) leads(path string) string {
	last := path[strings.LastIndexByte(path, '.')+1:]
	if cite, isCite := jomini.Cite(last); isCite {
		last = cite.Prefix
	}
	link := walk.reader.Vocab.Links[strings.ToLower(last)]
	if link.Wild {
		return ""
	}
	return walk.scoped(link.Out)
}

// value reads the word written as a statement's value at its field.
func (walk *walk) value(field *schema.Field, st *jomini.Assignment, via string) {
	scalar, ok := st.Value.(*jomini.Scalar)
	if !ok {
		return
	}
	via = cmp.Or(via, st.Key.Text)
	walk.parameters(scalar.Text, scalar.Range.Start)
	walk.listed = jomini.Literal(st.Key.Text) == jomini.LitNumber
	walk.ref(walk.reader.Value(field, scalar.Text, scalar.Quoted), scalar.Range.Start, via)
	walk.listed = false
}

// names reads a name token: save_scope_as = target makes a saved scope, has_variable = x reads a
// variable, and a token written with a block holds the name at the member its row gives.
func (walk *walk) names(token string, st *jomini.Assignment, scope string) {
	for _, row := range schema.NameTokensOf(token) {
		scalar, _ := st.Value.(*jomini.Scalar)
		name, offset := "", st.Key.Range.Start
		if scalar != nil {
			name, offset = scalar.Text, scalar.Range.Start
		} else if block := jomini.BlockOf(st.Value); block != nil && row.Member != "" {
			name = walk.member(block, row.Member)
		}
		if !jomini.IsDefName(name) {
			continue
		}
		if row.Defines {
			made := Def{Kind: row.Kind, Name: name, Parent: walk.top}
			if row.Kind == "saved_scopes" {
				made.ScopeType = scope
			}
			walk.def(made, offset)
		} else {
			walk.ref(schema.Value{Type: schema.TypeRef, Kind: row.Kind, Name: name}, offset, token)
		}
	}
}

// arguments reads the block of a macro call: each key is a parameter of the macro called, and each
// value is whatever the macro's text makes of it, which nothing here can know.
func (walk *walk) arguments(macro string, block *jomini.Block) {
	if block == nil {
		return
	}
	for _, st := range block.Statements {
		if assignment, ok := st.(*jomini.Assignment); ok {
			argument := schema.Value{Type: schema.TypeRef, Kind: "params", Name: assignment.Key.Text, Parent: macro}
			walk.ref(argument, assignment.Key.Range.Start, macro)
			walk.level(assignment, nil, nil, nil, argument, "", macro)
			walk.inside(nil, nil, jomini.BlockOf(assignment.Value), "", macro)
		}
	}
}

// parameters reads the $NAME$ parameters in a word of a macro's text: the first sighting of a name
// is its definition, every later one a reference to it.
func (walk *walk) parameters(text string, offset int) {
	if !walk.isMacro || !strings.Contains(text, "$") {
		return
	}
	for _, span := range jomini.Interpolations(text) {
		if walk.met[span.Name] {
			walk.ref(schema.Value{Type: schema.TypeRef, Kind: "params", Name: span.Name, Parent: walk.top}, offset+span.Start, "")
		} else {
			walk.met[span.Name] = true
			walk.def(Def{Kind: "params", Name: span.Name, Parent: walk.top}, offset+span.Start)
		}
	}
}
