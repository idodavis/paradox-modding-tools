// harvest.go reads one file into records. It decides nothing about the language: which kinds a file may define is the schema's (KindsAt, ByKeyword, Field.Defines), what a written word is is the reader's (Key, Value, Below), and which tokens make a name is the schema's one hand table. The harvest only walks, and writes down where things are.
//
// A scan harvests every file twice. With no Found the reader cannot say what a word names, so the first walk keeps definitions alone; the second, with every definition of the install behind Found, keeps the references and edges.
package index

import (
	"maps"
	"path"
	"slices"
	"strings"
	"sync"

	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/loc"
	"paradox-modding-tools/services/internal/lang/schema"
	"paradox-modding-tools/services/internal/source"
)

// nothing is a Found that has seen nothing: the first walk's.
type nothing struct{}

func (nothing) Def(kind, name string) bool { return false }
func (nothing) Loc(key string) bool        { return false }
func (nothing) File(name string) bool      { return false }

// fired is, per schema, the kinds a definition of which can be fired: those whose definitions are an effect body or hold one directly (an event's immediate, an on_action's effect, a law's on_pass). A character's history holds one two blocks down, under a date, and nothing fires a character. Worked out once for each schema.
var fired sync.Map

// firedKinds is the kinds of s that an edge may point at.
func firedKinds(s *schema.Schema) map[string]bool {
	if known, ok := fired.Load(s); ok {
		return known.(map[string]bool)
	}
	runs := func(f *schema.Field) bool {
		return f != nil && f.Class == schema.ClassBody && f.Body&schema.BodyEffect != 0
	}
	out := map[string]bool{}
	for name, k := range s.Kinds {
		out[name] = k.Shape != nil && (runs(k.Shape) || slices.ContainsFunc(slices.Collect(maps.Values(k.Shape.Members)), runs))
	}
	fired.Store(s, out)
	return out
}

// walk is one file being harvested.
type walk struct {
	r       schema.Reader
	h       *Records
	lines   *jomini.LineIndex
	refs    bool            // the second walk: references and edges are kept
	fired   map[string]bool // the kinds an edge may point at
	kind    string          // the kind of the top-level definition being walked, and its name
	top     string
	met     map[string]bool // the parameters of the macro being walked that have been met
	isMacro bool            // its kind takes parameters
	call    bool            // the word being written down is a macro written as a key
	body    bool            // the block being walked is a body, or inside one
	listed  bool            // the word being written down is an entry of a list: a bare word, or the value of a number (100 = some_event)
	at      int             // a probe: the offset the walk descends to, taking only the statements around it; -1 for a whole file
	probe   *Descent
	visit   func(Level) // a visit: called at every statement of the whole file
}

// Descent is what the one descent found on its way to an offset of a file: the definition the offset is inside, and every statement around it with the field it stands at.
type Descent struct {
	Kind  string // the kind of the definition the offset is inside; "" outside one
	Top   string // that definition's name
	Chain []Level
}

// Level is one statement around the offset: what its key is, the field it is written at and the field below it, and the scope its block runs in.
type Level struct {
	Statement jomini.Statement
	Kind      *schema.Kind  // the kind whose shape Field belongs to; a nested kind's inside its parent's
	Field     *schema.Field // the field the statement is written at
	Below     *schema.Field // the field under its key
	Key       schema.Value  // what the key is; TypeNone for a bare word
	Scope     string        // the scope type the statement runs in
	Via       string        // the nearest token around it
}

// contains reports whether a statement, or a bare word that declares the statement after it, holds the offset.
func (w *walk) contains(stmts []jomini.Statement, i int) bool {
	if w.at < 0 {
		return true
	}
	r := stmts[i].NodeRange()
	if r.Start <= w.at && w.at <= r.End {
		return true
	}
	_, word := stmts[i].(*jomini.ValueStmt)
	if next := i + 1; word && next < len(stmts) {
		r = stmts[next].NodeRange()
		return r.Start <= w.at && w.at <= r.End
	}
	return false
}

// level records a statement around the probe's offset, or hands every statement to the visit.
func (w *walk) level(st jomini.Statement, k *schema.Kind, field, below *schema.Field, v schema.Value, scope, via string) {
	if w.visit != nil {
		w.visit(Level{Statement: st, Kind: k, Field: field, Below: below, Key: v, Scope: scope, Via: via})
	}
	if w.probe != nil && w.at >= 0 {
		r := st.NodeRange()
		if r.Start <= w.at && w.at <= r.End {
			w.probe.Chain = append(w.probe.Chain, Level{Statement: st, Kind: k, Field: field, Below: below, Key: v, Scope: scope, Via: via})
		}
	}
}

// Harvest reads one file from its text, which an open buffer has and the disk may not. With r.Found nil it keeps definitions alone.
func Harvest(f source.File, text string, r schema.Reader) *Records {
	return newWalk(f, r, -1).read(text).h
}

// Probe makes the same descent as Harvest, along the one chain of statements that holds the offset, and gives what it passed.
func Probe(f source.File, text string, r schema.Reader, offset int) *Descent {
	w := newWalk(f, r, offset)
	w.probe = &Descent{}
	w.read(text)
	w.probe.Kind, w.probe.Top = w.kind, w.top
	return w.probe
}

// Visit makes the same descent as Harvest over a whole file and hands every statement to visit with the field it stands at, then gives the file's records.
func Visit(f source.File, text string, r schema.Reader, visit func(Level)) *Records {
	w := newWalk(f, r, -1)
	w.visit = visit
	return w.read(text).h
}

// newWalk is a walk of one file, whole or to one offset.
func newWalk(f source.File, r schema.Reader, at int) *walk {
	w := &walk{r: r, h: &Records{File: f}, refs: r.Found != nil, fired: firedKinds(r.Schema), at: at}
	if !w.refs {
		w.r.Found = nothing{}
	}
	return w
}

// Readable reports a format the harvest opens; an image, a mesh or a sound is in the inventory and never read.
func Readable(format source.Format) bool {
	return format == source.Script || format == source.Loc || format == source.Table || format == source.List
}

// read walks the file's text by its format.
func (w *walk) read(text string) *walk {
	f := w.h.File
	if !Readable(f.Format) {
		return w
	}
	w.lines = jomini.NewLineIndex(text)

	switch f.Format {
	case source.Loc:
		w.h.Loc = loc.Parse(text).Entries
		for _, entry := range w.h.Loc {
			for _, in := range loc.Interps(entry.Value, entry.ValueRange.Start) {
				if w.r.Found.Loc(in.Key) {
					w.ref(schema.Value{Type: schema.TypeLoc, Name: in.Key}, in.KeyRange.Start, "")
				}
			}
		}
	case source.Table, source.List:
		w.rows(text)
	case source.Script:
		w.file(jomini.Parse(text).Root)
	}
	return w
}

// def writes one definition down at a byte offset.
func (w *walk) def(d Def, offset int) {
	at := w.lines.PositionAt(offset)
	d.Line, d.Col = int32(at.Line), int32(at.Character)
	w.h.Defs = append(w.h.Defs, d)
}

// ref writes a reference down, and an edge where the site fires or invokes what it names: a macro written as a key; or a definition that can be fired, named at a declared effect (trigger_event = x) or as an entry of a list outside any body (an on_action's events = { a b } and random_events = { 100 = a }; holder = 168673 is no entry of a list).
func (w *walk) ref(v schema.Value, offset int, via string) {
	if !w.refs || v.Name == "" || (v.Type != schema.TypeRef && v.Type != schema.TypeLoc && v.Type != schema.TypeFile) || (v.Type == schema.TypeRef && v.Kind == "") {
		return
	}
	at := w.lines.PositionAt(offset)
	w.h.Refs = append(w.h.Refs, Ref{Type: v.Type, Kind: v.Kind, Name: v.Name, Parent: v.Parent, Declared: v.Declared, Line: int32(at.Line), Col: int32(at.Character)})
	_, effect := w.r.Vocab.Effects[via]
	if v.Type == schema.TypeRef && w.top != "" && (w.call || (w.fired[v.Kind] && (effect || (w.listed && !w.body)))) {
		w.h.Edges = append(w.h.Edges, Edge{FromKind: w.kind, From: w.top, ToKind: v.Kind, To: v.Name, Via: via, Line: int32(at.Line)})
	}
}

// rows reads a table or a list: the kind that lists this file names its definitions by a column, or by what follows the GUID on a line.
func (w *walk) rows(text string) {
	for _, k := range w.r.Schema.KindsAt(w.h.File.Rel) {
		offset := 0
		for _, line := range strings.SplitAfter(text, "\n") {
			row := strings.TrimSpace(line)
			cells := strings.Split(row, ";")
			switch {
			case row == "" || strings.HasPrefix(row, "#"):
			case k.Naming.Form == "column" && k.Naming.Column < len(cells):
				w.def(Def{Kind: k.Name, Name: strings.TrimSpace(cells[k.Naming.Column])}, offset)
			case k.Naming.Form == "line":
				_, name, _ := strings.Cut(row, " ")
				w.def(Def{Kind: k.Name, Name: strings.TrimSpace(name)}, offset)
			}
			offset += len(line)
		}
	}
}

// file walks a script file's top level: constants, declarations, and the names of the kinds the schema says a file at this path may define.
func (w *walk) file(root *jomini.Root) {
	var named, about *schema.Kind
	byWord := map[string]*schema.Kind{}
	for _, k := range w.r.Schema.KindsAt(w.h.File.Rel) {
		switch {
		case k.Naming.Form == "member":
			byWord[k.Naming.Keyword] = k
		case k.Subject != "":
			about = k
		case k.Naming.Form == "key" || k.Naming.Form == "number" || k.Naming.Form == "event_id":
			named = k
		}
	}
	if about != nil && about.Naming.Form == "file" {
		base := path.Base(w.h.File.Rel)
		w.ref(schema.Value{Type: schema.TypeRef, Kind: about.Subject, Name: strings.TrimSuffix(base, path.Ext(base))}, 0, "")
	}

	stmts := root.Statements
	for i := 0; i < len(stmts); i++ {
		if !w.contains(stmts, i) {
			continue
		}
		for next := w.declared(true, stmts, i); next != i; next = w.declared(true, stmts, i) {
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
			w.def(Def{Kind: "constants", Name: name}, st.Key.Range.Start)
			w.level(st, nil, nil, nil, schema.Value{Type: schema.TypeConstant, Name: name}, "", "")
			continue
		}

		switch k := byWord[key]; {
		case k != nil && block != nil:
			w.define(k, w.member(block, k.Naming.Member), mode, st, block)
		case about != nil && about.Naming.Form != "file":
			w.kind, w.top = about.Name, key
			cited := schema.Value{Type: schema.TypeRef, Kind: about.Subject, Name: key}
			w.ref(cited, st.Key.Range.Start, "")
			w.level(st, about, nil, about.Shape, cited, "", "")
			w.inside(about, about.Shape, block, "", "")
		case named != nil && !st.Key.Quoted && (named.Naming.Form != "event_id" || jomini.IsEventID(key)) && (named.Naming.Form != "number" || jomini.Literal(key) == jomini.LitNumber):
			w.define(named, key, mode, st, block)
			if block == nil {
				w.value(named, named.Shape, st, "")
			}
		default:
			w.kind, w.top = "", ""
			w.level(st, nil, nil, nil, schema.Value{Type: schema.TypeUnresolved, Name: key}, "", "")
			w.inside(nil, nil, block, "", "")
		}
	}
}

// declared reads a declaration at i, a bare word before a named block where the schema knows the word (type x = button { }, scripted_trigger x = { }), and gives the position to go on from. A kind named by key is declared so at the top of a file only.
func (w *walk) declared(top bool, stmts []jomini.Statement, i int) int {
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
	k := w.r.Schema.ByKeyword(scalar.Text)
	if k == nil || (k.Naming.Form != "declaration" && !top) {
		return i
	}
	kind, name := w.kind, w.top
	w.define(k, next.Key.Text, "", next, jomini.BlockOf(next.Value))
	if !top {
		w.kind, w.top = kind, name
	}
	return i + 2
}

// define writes a definition of k down and walks what it holds.
func (w *walk) define(k *schema.Kind, name, mode string, st *jomini.Assignment, block *jomini.Block) {
	if name == "" {
		return
	}
	w.kind, w.top, w.met, w.isMacro, w.body = k.Name, name, map[string]bool{}, k.Params, k.Shape != nil && k.Shape.Class == schema.ClassBody
	w.def(Def{Kind: k.Name, Name: name, Mode: mode, Label: w.member(block, k.LabelMember)}, st.Key.Range.Start)
	scope := ""
	if k.Shape != nil {
		scope = k.Shape.BodyScope
	}
	w.level(st, k, nil, k.Shape, schema.Value{Type: schema.TypeRef, Kind: k.Name, Name: name}, scope, "")
	w.inside(k, k.Shape, block, scope, "")
}

// member is the word a block writes at one of its members, "" where it writes none.
func (w *walk) member(block *jomini.Block, member string) string {
	if block == nil || member == "" {
		return ""
	}
	for _, st := range block.Statements {
		if a, ok := st.(*jomini.Assignment); ok && a.Key.Text == member {
			if scalar, ok := a.Value.(*jomini.Scalar); ok {
				return scalar.Text
			}
		}
	}
	return ""
}

// inside walks one block of a definition of kind k, at the field that block is an instance of. scope is the scope type its statements run in where that is known; via is the nearest token around it.
func (w *walk) inside(k *schema.Kind, field *schema.Field, block *jomini.Block, scope, via string) {
	if block == nil {
		return
	}
	stmts := block.Statements
	for i := 0; i < len(stmts); i++ {
		if !w.contains(stmts, i) {
			continue
		}
		for next := w.declared(false, stmts, i); next != i; next = w.declared(false, stmts, i) {
			i = next
		}
		if i >= len(stmts) {
			break
		}
		switch st := stmts[i].(type) {
		case *jomini.ValueStmt:
			if scalar, ok := st.Value.(*jomini.Scalar); ok {
				w.parameters(scalar.Text, scalar.Range.Start)
				w.listed = true
				w.ref(w.r.Value(field, scalar.Text, scalar.Quoted), scalar.Range.Start, via)
				w.listed = false
			}
			w.level(st, k, field, nil, schema.Value{}, scope, via)
			w.inside(k, nil, jomini.BlockOf(st.Value), scope, via)
		case *jomini.Assignment:
			w.statement(k, field, st, scope, via)
		}
	}
}

// statement reads one key and what is written under it.
func (w *walk) statement(k *schema.Kind, field *schema.Field, st *jomini.Assignment, scope, via string) {
	key, block := st.Key.Text, jomini.BlockOf(st.Value)
	v := w.r.Key(field, key)
	below := w.r.Below(k, field, key, v)
	w.parameters(key, st.Key.Range.Start)
	w.level(st, k, field, below, v, scope, via)

	// a name of the nested kind this position defines: a faith inside a religion, a setting inside a game rule
	if field != nil && field.Defines != "" && !st.Key.Quoted && (v.Type == schema.TypeUnresolved || (v.Type == schema.TypeRef && v.Kind == field.Defines)) && jomini.IsDefName(key) {
		w.def(Def{Kind: field.Defines, Name: key, Parent: w.top}, st.Key.Range.Start)
		if w.probe != nil && len(w.probe.Chain) > 0 && w.probe.Chain[len(w.probe.Chain)-1].Statement == st {
			last := &w.probe.Chain[len(w.probe.Chain)-1]
			last.Key, last.Kind = schema.Value{Type: schema.TypeRef, Kind: field.Defines, Name: key, Parent: w.top}, w.r.Schema.Kinds[field.Defines]
		}
		w.inside(w.r.Schema.Kinds[field.Defines], below, block, scope, via)
		return
	}

	switch v.Type {
	case schema.TypeToken:
		via = v.Name
		w.names(v.Name, st, scope)
		if token, ok := w.r.Vocab.Effects[v.Name]; ok && token.Out != "" {
			scope = token.Out
		} else if token, ok := w.r.Vocab.Triggers[v.Name]; ok && token.Out != "" {
			scope = token.Out
		}
	case schema.TypeScopePath:
		scope = w.r.Vocab.Links[key].Out
	case schema.TypeRef:
		called := w.r.Schema.Kinds[v.Kind]
		w.call = called != nil && called.Macro != 0
		w.ref(v, st.Key.Range.Start, key)
		if w.call && called.Params {
			w.call = false
			w.arguments(v.Name, block)
			return
		}
		if w.call {
			// a macro that takes no parameters is followed by more of the body it is written in: every_adult_child = { add_gold = 1 }
			w.call, below = false, field
		}
		if cited := w.r.Schema.Kinds[v.Kind]; cited != nil {
			scope = cited.ScopeType
		}
	}
	if below != nil && below != field && below.BodyScope != "" {
		scope = below.BodyScope
	}
	if _, effect := w.r.Vocab.Effects[via]; !effect && v.Type != schema.TypeToken && v.Type != schema.TypeNumber {
		if _, trigger := w.r.Vocab.Triggers[via]; !trigger {
			via = key
		}
	}

	if block == nil {
		w.value(k, below, st, via)
		return
	}
	body := w.body
	w.body = body || (below != nil && below.Class == schema.ClassBody)
	w.inside(k, below, block, scope, via)
	w.body = body
}

// value reads the word written as a statement's value at its field.
func (w *walk) value(k *schema.Kind, field *schema.Field, st *jomini.Assignment, via string) {
	scalar, ok := st.Value.(*jomini.Scalar)
	if !ok {
		return
	}
	if via == "" {
		via = st.Key.Text
	}
	w.parameters(scalar.Text, scalar.Range.Start)
	w.listed = jomini.Literal(st.Key.Text) == jomini.LitNumber
	w.ref(w.r.Value(field, scalar.Text, scalar.Quoted), scalar.Range.Start, via)
	w.listed = false
}

// names reads a name token: save_scope_as = target makes a saved scope, has_variable = x reads a variable, and a token written with a block holds the name at the member its row gives.
func (w *walk) names(token string, st *jomini.Assignment, scope string) {
	for _, row := range schema.NameTokensOf(token) {
		scalar, _ := st.Value.(*jomini.Scalar)
		name, offset := "", st.Key.Range.Start
		if scalar != nil {
			name, offset = scalar.Text, scalar.Range.Start
		} else if block := jomini.BlockOf(st.Value); block != nil && row.Member != "" {
			name = w.member(block, row.Member)
		}
		if !jomini.IsDefName(name) {
			continue
		}
		if row.Defines {
			made := Def{Kind: row.Kind, Name: name, Parent: w.top}
			if row.Kind == "saved_scopes" {
				made.ScopeType = scope
			}
			w.def(made, offset)
		} else {
			w.ref(schema.Value{Type: schema.TypeRef, Kind: row.Kind, Name: name}, offset, token)
		}
	}
}

// arguments reads the block of a macro call: each key is a parameter of the macro called, and each value is whatever the macro's text makes of it, which nothing here can know.
func (w *walk) arguments(macro string, block *jomini.Block) {
	if block == nil {
		return
	}
	for _, st := range block.Statements {
		if a, ok := st.(*jomini.Assignment); ok {
			argument := schema.Value{Type: schema.TypeRef, Kind: "params", Name: a.Key.Text, Parent: macro}
			w.ref(argument, a.Key.Range.Start, macro)
			w.level(a, nil, nil, nil, argument, "", macro)
			w.inside(nil, nil, jomini.BlockOf(a.Value), "", macro)
		}
	}
}

// parameters reads the $NAME$ parameters in a word of a macro's text: the first sighting of a name is its definition, every later one a reference to it.
func (w *walk) parameters(text string, offset int) {
	if !w.isMacro || !strings.Contains(text, "$") {
		return
	}
	for _, span := range jomini.Interpolations(text) {
		if w.met[span.Name] {
			w.ref(schema.Value{Type: schema.TypeRef, Kind: "params", Name: span.Name, Parent: w.top}, offset+span.Start, "")
		} else {
			w.met[span.Name] = true
			w.def(Def{Kind: "params", Name: span.Name, Parent: w.top}, offset+span.Start)
		}
	}
}
