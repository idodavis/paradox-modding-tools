// passes.go is one parsed file and the three walks over it: the definitions
// it declares and the ten grammar rows that need no global knowledge; its
// values folded into the schema tree once every definition is known; and the
// references and edges the settled tree says those values are. Every walk is
// one loop over the statements in pre-order, addressing the tree by one
// descent, and nothing is kept between walks but the block skeleton the
// nested join reads.

package index

import (
	"path/filepath"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/declared"
	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/loc"
)

// Fact is one statement: an assignment (Key set) or a bare value (Key ""). Start/End span the key, or the bare value; VStart/VEnd the scalar right-hand side; Stop is where the whole statement ends. The children of a block are the facts whose Parent is its index, contiguous after it.
type Fact struct {
	Key                 string
	Op                  string
	Value               string
	Parent              int32
	Line, Start, End    int32
	VLine, VStart, VEnd int32
	Stop                int32
	Block               bool
	Quoted, VQuoted     bool
	Depth               uint8
}

// File is one script file: where it came from, what it defines, and its statements.
type File struct {
	GameID, Path, Rel, Origin string
	Rule                      game.ExtractRule
	Order                     int
	Kind                      string // the definition kind of the file; event for event files
	Data                      bool   // a data-tier kind, where a number may name a row
	Src                       string
	Facts                     []Fact
	names, empty, litList     []bool
}

// NewFile describes a file before it is parsed.
func NewFile(gameID, path, rel, origin string, order int) *File {
	f := &File{GameID: gameID, Path: filepath.Clean(path), Rel: rel, Origin: origin, Order: order,
		Rule: game.MatchExtract(gameID, rel)}
	f.Kind = f.Rule.Kind
	if f.Rule.Mode == game.ModeEventID {
		f.Kind = jomini.KindEvent
	}
	f.Data = game.IsDataKind(gameID, f.Kind)
	return f
}

// Parse fills the facts from text, or from a result the editor already has.
func (f *File) Parse(text string, res jomini.Result) *File {
	if res.Root == nil {
		res = jomini.Parse(text)
	}
	f.Src = res.Src
	if res.Root == nil {
		return f
	}
	f.Facts = make([]Fact, 0, len(res.Src)/12+8)
	src, line, at := res.Src, int32(0), 0
	lineAt := func(off int32) int32 {
		for ; at < int(off) && at < len(src); at++ {
			if c := src[at]; c == '\n' || (c == '\r' && (at+1 == len(src) || src[at+1] != '\n')) {
				line++
			}
		}
		return line
	}
	var open []int32
	jomini.Walk(res.Root, func(st jomini.Statement, depth int, _ *jomini.Block) bool {
		k := Fact{Parent: -1, Depth: uint8(min(depth, 255)), Stop: int32(st.NodeRange().End)}
		if depth > 0 && depth <= len(open) {
			k.Parent = open[depth-1]
		}
		var val jomini.Value
		switch n := st.(type) {
		case *jomini.Assignment:
			k.Key, k.Op, k.Quoted = n.Key.Text, n.Op, n.Key.Quoted
			k.Line, k.Start, k.End = lineAt(int32(n.Key.Range.Start)), int32(n.Key.Range.Start), int32(n.Key.Range.End)
			val = n.Value
		case *jomini.ValueStmt:
			k.Line, k.Start, k.End = lineAt(int32(n.Range.Start)), int32(n.Range.Start), int32(n.Range.End)
			val = n.Value
		}
		switch v := val.(type) {
		case *jomini.Scalar:
			k.Value, k.VQuoted = v.Text, v.Quoted
			k.VLine, k.VStart, k.VEnd = lineAt(int32(v.Range.Start)), int32(v.Range.Start), int32(v.Range.End)
			if k.Key == "" {
				k.Quoted, k.Line, k.Start, k.End = v.Quoted, k.VLine, k.VStart, k.VEnd
			}
		case *jomini.TaggedBlock, *jomini.Block:
			k.Block = true
		}
		for len(open) <= depth {
			open = append(open, -1)
		}
		open[depth] = int32(len(f.Facts))
		f.Facts = append(f.Facts, k)
		return true
	})
	f.shapes()
	return f
}

// shapes marks every block that holds keyed children, every empty one, and every list whose bare members are all numbers: `color = { 120 30 40 }` is a literal, not a list of references.
func (f *File) shapes() {
	n := len(f.Facts)
	f.names, f.empty, f.litList = make([]bool, n), make([]bool, n), make([]bool, n)
	any, nonNum := make([]bool, n), make([]bool, n)
	for i := range f.Facts {
		p := f.Facts[i].Parent
		if p < 0 {
			continue
		}
		any[p] = true
		if f.Facts[i].Key != "" {
			f.names[p], nonNum[p] = true, true
		} else if v := f.Facts[i].Value; !(v != "" && jomini.IsLiteralValue(v) && !jomini.IsDateLiteral(v)) {
			nonNum[p] = true
		}
	}
	for i := range f.Facts {
		f.empty[i], f.litList[i] = !any[i], any[i] && !nonNum[i]
	}
}

// Children walks the direct children of fact i.
func (f *File) Children(i int32, fn func(j int32, c *Fact) bool) {
	for j := i + 1; j < int32(len(f.Facts)) && f.Facts[j].Parent >= i; j++ {
		if f.Facts[j].Parent == i && !fn(j, &f.Facts[j]) {
			return
		}
	}
}

// At is the innermost fact whose statement contains offset, with its ancestors first.
func (f *File) At(offset int) []int32 {
	var chain []int32
	for i := range f.Facts {
		k := &f.Facts[i]
		if int(k.Start) > offset {
			break
		}
		for len(chain) > 0 && f.Facts[chain[len(chain)-1]].Stop < int32(offset) {
			chain = chain[:len(chain)-1]
		}
		if len(chain) == 0 && k.Parent >= 0 || len(chain) > 0 && k.Parent != chain[len(chain)-1] {
			continue
		}
		if int(k.Stop) >= offset {
			chain = append(chain, int32(i))
		}
	}
	return chain
}

func (f *File) def(kind, key string, line, start, end int32) Def {
	return Def{Kind: jomini.CanonicalKind(kind), Key: strings.Clone(key), Path: f.Path,
		Line: int(line), Start: int(start), End: int(end), Origin: f.Origin}
}

func (f *File) ref(kind, key string, line, start, end int32) Ref {
	return Ref{Key: strings.Clone(key), Kind: kind, Path: f.Path,
		Line: int(line), Start: int(start), End: int(end)}
}

// topDef is the definition a top-level statement declares, by rows 1 and 2, or "".
func (f *File) topDef(i int32) string {
	k := &f.Facts[i]
	if k.Depth != 0 || !k.Block || k.Quoted || (k.Op != "=" && k.Op != "?=") {
		return ""
	}
	switch f.Rule.Mode {
	case game.ModeTopLevelKey:
		if k.Key == "namespace" {
			return ""
		}
		if name := game.KeyIdentity(f.GameID, k.Key); jomini.IsDefName(name) && !jomini.IsDateLiteral(name) {
			return name
		}
	case game.ModeEventID:
		if jomini.IsEventID(k.Key) {
			return k.Key
		}
	}
	return ""
}

// -------------------------------------------------------------------- pass 1

// blk is one keyed block below the top level: the skeleton the nested join reads.
type blk struct {
	parent       int32
	key          string
	quoted       bool
	names        bool
	line, at, to int32
}

// skel is what pass 1 keeps of a file for the join: its blocks, its citations, its wrapper candidates and its local-database counts.
type skel struct {
	blks  []blk
	cites map[string]string   // cited id → prefix
	wraps map[string][]string // top-level key → the ids its members cite
	local map[string][2]int32 // definition-body field → [values naming a sibling block, values]
}

// defs is pass 1: the ten grammar rows, over one file.
func (f *File) defs(decl *declared.Schema) (*Harvest, *skel) {
	h := &Harvest{Path: f.Path, Rel: f.Rel, Origin: f.Origin, Order: f.Order, Kind: f.Kind}
	sk := &skel{cites: map[string]string{}, wraps: map[string][]string{}, local: map[string][2]int32{}}
	n := int32(len(f.Facts))
	blkAt := make([]int32, n)
	macros := map[string]bool{}            // this file's own macro-shaped definitions, the owners of $PARAM$
	params := map[string]map[string]bool{} // macro → its parameters
	seen := map[string]bool{}              // macro\x00param, so the first sighting alone is the definition
	blocks := map[int32]map[string]bool{}  // top-level definition → its block keys, for the local-database rule
	nsKey := ""
	addDef := func(d Def) {
		h.Defs = append(h.Defs, d)
		if IsCitableDef(d) && jomini.CanonicalKind(d.Kind) != jomini.KindNamespace && d.Kind != jomini.KindGUIType {
			macros[d.Key] = true
		}
	}
	for i := int32(0); i < n; i++ {
		k := &f.Facts[i]
		blkAt[i] = -1
		if k.Block && k.Depth > 0 && k.Key != "" {
			p := int32(-1)
			if k.Parent >= 0 {
				p = blkAt[k.Parent]
			}
			blkAt[i] = int32(len(sk.blks))
			sk.blks = append(sk.blks, blk{parent: p, key: k.Key, quoted: k.Quoted, names: f.names[i],
				line: k.Line, at: k.Start, to: k.End})
		}
		// 1, 2: a top-level `name = { }` in a kind's folder, `namespace = x` and `ns.1 = { }`.
		if name := f.topDef(i); name != "" {
			kind := f.Kind
			d := f.def(kind, name, k.Line, k.Start, k.End)
			if docWorthyKind(kind) {
				d.Doc = leadingComment(f.Src, int(k.Start))
			}
			addDef(d)
			if f.Rule.Mode == game.ModeEventID && nsKey != "" {
				h.Refs = append(h.Refs, f.ref(jomini.KindNamespace, nsKey, k.Line, k.Start, k.End))
			}
			blocks[i] = map[string]bool{}
		} else if f.Rule.Mode == game.ModeEventID && k.Depth == 0 && !k.Quoted && k.Key == "namespace" &&
			!k.VQuoted && jomini.IsDefName(k.Value) {
			addDef(f.def(jomini.KindNamespace, k.Value, k.VLine, k.VStart, k.VEnd))
			nsKey = k.Value
		}
		// 3: `type NAME = { }`, `template NAME`, `scripted_trigger NAME` name the statement after them.
		if k.Key == "" && !k.Quoted && f.Rule.Mode != game.ModeTopLevelKey && f.Rule.Mode != game.ModeNone {
			if kind := jomini.DeclKeywordKind(k.Value); kind != "" && kind != "types" && i+1 < n {
				if nx := &f.Facts[i+1]; nx.Parent == k.Parent && nx.Key != "" && !nx.Quoted && jomini.IsDefName(nx.Key) {
					d := f.def(kind, nx.Key, nx.Line, nx.Start, nx.End)
					d.Local = jomini.DeclIsLocal(k.Value)
					if f.Rule.Mode != game.ModeGUIType && docWorthyKind(d.Kind) {
						d.Doc = leadingComment(f.Src, int(nx.Start))
					}
					addDef(d)
				}
			}
		}
		// 4: the script-name table: `set_variable` and its siblings define an ephemeral name, `has_variable` and its own refer to one.
		if k.Key != "" && !k.Quoted {
			if r, ok := jomini.ScriptName(k.Key); ok {
				if name, line, start, end, found := f.scriptNameSpan(i, r.InnerKey); found {
					if !r.IsDef {
						h.Refs = append(h.Refs, f.ref(r.Kind, name, line, start, end))
					} else {
						d := f.def(r.Kind, name, line, start, end)
						d.Nested, d.Value = true, f.scriptNameValue(i, r)
						h.note(r.Kind, "", name, d.Value)
						h.Defs = append(h.Defs, d)
					}
				}
			}
		}
		// 5: `$PARAM$` inside a macro body: the first sighting is the parameter, the rest use it.
		if k.Key != "" && !k.Quoted && len(macros) > 0 {
			owner := i
			for f.Facts[owner].Parent >= 0 {
				owner = f.Facts[owner].Parent
			}
			if ok := f.Facts[owner].Block && macros[f.Facts[owner].Key]; ok {
				ownerKey := f.Facts[owner].Key
				if params[ownerKey] == nil {
					params[ownerKey] = map[string]bool{}
				}
				record := func(text string, base, line int32) {
					for _, sp := range dollarParamsIn(text, base) {
						params[ownerKey][sp.name] = true
						if id := ownerKey + "\x00" + sp.name; !seen[id] {
							seen[id] = true
							d := f.def(jomini.KindScriptParam, sp.name, line, sp.start, sp.end)
							d.OwnerKey = ownerKey
							h.Defs = append(h.Defs, d)
						} else {
							r := f.ref(jomini.KindScriptParam, sp.name, line, sp.start, sp.end)
							r.OwnerKey = ownerKey
							h.Refs = append(h.Refs, r)
						}
					}
				}
				record(k.Key, k.Start, k.Line)
				if !k.Block && !k.VQuoted {
					record(k.Value, k.VStart, k.VLine)
				}
			}
		}
		// 6: every `prefix:id` in a key or an unquoted value resolves through the declared prefixes.
		if k.Key != "" && !k.Quoted {
			f.cites(h, sk, k.Key, k.Start, k.Line)
		}
		if !k.Block && !k.VQuoted && k.Value != "" {
			f.cites(h, sk, k.Value, k.VStart, k.VLine)
		}
		// 7: a top-level block whose keyed children are all cites is a container, not a definition.
		if k.Depth == 0 && k.Block && k.Key != "" && !k.Quoted && !jomini.DeclaresNamedBodies(f.Kind) && !f.afterKeyword(i) {
			if ids, all := f.citedMembers(i); all {
				sk.wraps[k.Key] = ids
			}
		}
		// The local-database rule's evidence: a definition's own block keys, and which of its fields name one.
		if k.Depth == 1 && k.Parent >= 0 {
			if set := blocks[k.Parent]; set != nil && k.Block && k.Key != "" && !k.Quoted {
				set[k.Key] = true
			}
		}
	}
	for i := range f.Facts {
		k := &f.Facts[i]
		if k.Depth != 1 || k.Block || k.Key == "" || k.Quoted || k.Value == "" {
			continue
		}
		if set := blocks[k.Parent]; set != nil {
			c := sk.local[k.Key]
			c[1]++
			if set[k.Value] {
				c[0]++
			}
			sk.local[k.Key] = c
		}
	}
	f.argRefs(h, macros, params)
	return h, sk
}

// argRefs completes row 5: an argument key inside a call to macro M that is one of M's parameters, after the pass because a call may sit above the macro it fills; and the value each call passes, for the parameter's card.
func (f *File) argRefs(h *Harvest, macros map[string]bool, params map[string]map[string]bool) {
	call := make([]int32, len(f.Facts))
	for i := range f.Facts {
		k := &f.Facts[i]
		ck := int32(-1)
		if k.Parent >= 0 {
			ck = call[k.Parent]
		}
		if k.Key != "" && !k.Quoted {
			if ck >= 0 && params[f.Facts[ck].Key][k.Key] {
				r := f.ref(jomini.KindScriptParam, k.Key, k.Line, k.Start, k.End)
				r.OwnerKey = f.Facts[ck].Key
				h.Refs = append(h.Refs, r)
			}
			if !k.Block && !k.VQuoted && k.Value != "" && k.Parent >= 0 {
				if owner := f.Facts[k.Parent].Key; owner != "" && (params[owner][k.Key] || isMacroArgKey(k.Key)) {
					h.note(jomini.KindScriptParam, owner, k.Key, k.Value)
				}
			}
			if macros[k.Key] {
				ck = int32(i)
			}
		}
		call[i] = ck
	}
}

// cites is row 6 over one token.
func (f *File) cites(h *Harvest, sk *skel, text string, base, line int32) {
	for _, sp := range jomini.TypedSpans(text) {
		if jomini.PrefixKind(sp.Prefix) == "" {
			sk.cites[sp.ID] = sp.Prefix
		}
	}
	if p, ok := jomini.ParsePrefixed(text); ok {
		kind := jomini.PrefixKind(p.Prefix)
		if kind == "" {
			kind = p.Prefix
		}
		at := base + int32(p.NameOff)
		h.Refs = append(h.Refs, f.ref(kind, p.Name, line, at, at+int32(len(p.Name))))
		return
	}
	if kind, id, ok := game.ParseTyped(f.GameID, text); ok {
		h.Refs = append(h.Refs, f.ref(kind, id, line, base, base+int32(len(text))))
	}
}

// scriptNameSpan is the name token of a script-name assignment: the scalar RHS, or the InnerKey field inside a block.
func (f *File) scriptNameSpan(i int32, innerKey string) (name string, line, start, end int32, ok bool) {
	k := &f.Facts[i]
	if !k.Block && !k.VQuoted && k.Value != "" {
		return k.Value, k.VLine, k.VStart, k.VEnd, true
	}
	if innerKey == "" || !k.Block {
		return "", 0, 0, 0, false
	}
	f.Children(i, func(_ int32, c *Fact) bool {
		if c.Quoted || c.Key != innerKey {
			return true
		}
		if !c.Block && !c.VQuoted && c.Value != "" {
			name, line, start, end, ok = c.Value, c.VLine, c.VStart, c.VEnd, true
		}
		return false
	})
	return
}

// scriptNameValue is the hover RHS for a script-name def: a scalar save has none of its own, so its value is what it was saved from -- the innermost enclosing iterator or scope switch.
func (f *File) scriptNameValue(i int32, r jomini.ScriptNameRule) string {
	k := &f.Facts[i]
	if r.Kind == "saved_scope" && r.InnerKey == "" {
		for j := k.Parent; j >= 0; j = f.Facts[j].Parent {
			key := f.Facts[j].Key
			if key == "" || f.Facts[j].Quoted || jomini.IsSaveScopeKey(key) || jomini.IsSaveScopeValueKey(key) {
				continue
			}
			low := strings.ToLower(key)
			if strings.HasPrefix(low, "any_") || strings.HasPrefix(low, "every_") ||
				strings.HasPrefix(low, "random_") || strings.HasPrefix(low, "ordered_") {
				out := key
				f.Children(j, func(_ int32, c *Fact) bool {
					if !c.Quoted && strings.EqualFold(c.Key, "list") && !c.Block && !c.VQuoted && c.Value != "" {
						out = key + " - list=" + c.Value
						return false
					}
					return true
				})
				return out
			}
			if low == "root" || strings.Contains(key, ":") {
				return key
			}
		}
		return "root"
	}
	if !k.Block {
		return "yes"
	}
	out := "yes"
	f.Children(i, func(_ int32, c *Fact) bool {
		if !c.Quoted && c.Key == "value" && !c.Block && !c.VQuoted && c.Value != "" {
			out = c.Value
			return false
		}
		return true
	})
	return out
}

// afterKeyword reports a statement introduced by a bare declaration keyword.
func (f *File) afterKeyword(i int32) bool {
	if i == 0 {
		return false
	}
	p := &f.Facts[i-1]
	if p.Key != "" || p.Quoted || p.Parent != f.Facts[i].Parent {
		return false
	}
	k := jomini.DeclKeywordKind(p.Value)
	return k != "" && k != "types"
}

// citedMembers are the ids a block's keyed children cite, and whether every child is a cite.
func (f *File) citedMembers(i int32) (ids []string, all bool) {
	all = true
	f.Children(i, func(_ int32, c *Fact) bool {
		if c.Key == "" || c.Quoted {
			return true
		}
		kind, id, ok := game.ParseTyped(f.GameID, c.Key)
		if !ok {
			if sp, sok := jomini.TypedSpanAt(c.Key, 0); sok {
				kind, id, ok = game.ParseTyped(f.GameID, sp.Prefix+":"+sp.ID)
			}
		}
		if !ok || kind == "" {
			all = false
			return false
		}
		ids = append(ids, id)
		return true
	})
	return ids, all && len(ids) > 0
}

// optionName is the `name = X` of an option block, for the edge it fires.
func (f *File) optionName(i int32) string {
	name := ""
	f.Children(i, func(_ int32, c *Fact) bool {
		if c.Key != "" && strings.EqualFold(c.Key, "name") && !c.Block {
			name = c.Value
			return false
		}
		return true
	})
	return name
}

// ---------------------------------------------------------------- the descent

// descent addresses one file against several trees at once, the guide first and the tree being learned last: per fact, the node its key names in each tree and the position its children hang from.
type descent struct {
	f      *File
	trees  []*Tree
	create bool      // make nodes in the last tree
	cell   [][]*Node // per tree, per fact
	hang   [][]*Node
	row    []bool // the fact's key folded to a row
}

func newDescent(f *File, trees []*Tree, create bool) *descent {
	d := &descent{f: f, trees: trees, create: create,
		cell: make([][]*Node, len(trees)), hang: make([][]*Node, len(trees)), row: make([]bool, len(f.Facts))}
	for t := range trees {
		d.cell[t], d.hang[t] = make([]*Node, len(f.Facts)), make([]*Node, len(f.Facts))
	}
	return d
}

// step places fact i. A top-level block hangs its children from the kind's root; below, the parent's hang decides, and a database's rows share the body of the kind they define.
func (d *descent) step(i int32) {
	k := &d.f.Facts[i]
	last := len(d.trees) - 1
	if k.Depth == 0 {
		if k.Block {
			for t, s := range d.trees {
				d.hang[t][i] = s.Root(d.f.Kind, d.create && t == last)
				d.cell[t][i] = d.hang[t][i]
			}
		}
		return
	}
	p := k.Parent
	if k.Key == "" {
		for t := range d.trees {
			d.cell[t][i] = d.cell[t][p]
		}
		return
	}
	rows := false
	for t := range d.trees {
		if h := d.hang[t][p]; h != nil && h.Flags&Rows != 0 {
			rows = true
		}
	}
	for t, s := range d.trees {
		h := d.hang[t][p]
		if h == nil {
			continue
		}
		if k.Block && rows && h.Flags&Rows == 0 && t == last && d.create {
			// The guide's mark carried over to the tree being learned, at the coordinates it will be read at.
			for g := 0; g < t; g++ {
				if gh := d.hang[g][p]; gh != nil && gh.Flags&Rows != 0 {
					h.Flags, h.Def = h.Flags|Rows, gh.Def
				}
			}
		}
		name := s.name(k.Key, k.Quoted, k.Block, rows)
		n := h.Kids[name]
		if n == nil && d.create && t == last {
			if h.Kids == nil {
				h.Kids = map[string]*Node{}
			}
			n = &Node{}
			h.Kids[strings.Clone(name)] = n
		}
		d.cell[t][i] = n
		if n == nil || !k.Block {
			continue
		}
		if name == SegKind {
			d.row[i] = true
			d.hang[t][i] = s.body(h, n, d.create && t == last)
		} else {
			d.hang[t][i] = n
		}
	}
}

// name is what a key is filed under in this tree; a bootstrap tree files any definition name as a row so the join can generalise a cited row to the position it was written at.
func (s *Tree) name(key string, quoted, block, rows bool) string {
	if s.byName != nil {
		rows = s.byName(game.KeyIdentity(s.gameID, key))
	}
	return s.fold(key, quoted, block && rows)
}

// body is where a row's children hang: the body of the kind the database defines, or the row cell itself for a local database and in a bootstrap tree.
func (s *Tree) body(p, row *Node, create bool) *Node {
	if s.byName == nil && p.Def != "" {
		return s.Root(p.Def, create)
	}
	return row
}

// first is the fact's cell in the first tree that has one.
func (d *descent) first(i int32) *Node {
	for t := range d.trees {
		if n := d.cell[t][i]; n != nil {
			return n
		}
	}
	return nil
}

// -------------------------------------------------------------------- pass 2

// fold is pass 2 over one file: every value folded into its node of a tree of the file's own, to be merged into the corpus's; where the guide already has the node, a workspace learns nothing.
func (f *File) fold(guide *Tree, r *resolver, install bool) *Tree {
	target := &Tree{gameID: guide.gameID, decl: guide.decl}
	trees := []*Tree{guide, target}
	d := newDescent(f, trees, true)
	last := len(trees) - 1
	var labels []string
	for i := range f.Facts {
		i := int32(i)
		d.step(i)
		k := &f.Facts[i]
		n := d.cell[last][i]
		if n == nil || !install && d.cell[0][i] != nil {
			continue
		}
		if k.Depth == 0 {
			if k.Block && f.names[i] {
				n.Flags |= Names
				n.add([]string{LabelBlock}, 1)
			}
			continue
		}
		if k.Key == "" && f.litList[k.Parent] {
			continue
		}
		if k.Block {
			if f.names[i] {
				n.Flags |= Names
			}
			n.add([]string{blockLabel(f.names[i], f.empty[i])}, 1)
			continue
		}
		v, q := k.Value, k.VQuoted
		if k.Key == "" {
			q = k.Quoted
		}
		labels = r.classify(v, q, f.Data, labels)
		n.add(labels, 1)
	}
	return target
}

func blockLabel(names, empty bool) string {
	switch {
	case names:
		return LabelBlock
	case empty:
		return LabelEmpty
	}
	return LabelList
}

// nested emits the definitions the join found in one file: a keyed block whose key is cited under a nested kind, or that sits under a database the guide marked.
func (f *File) nested(sk *skel, guide *Tree, cited map[string]string, shapes map[[2]string]bool) []Def {
	root := guide.Root(f.Kind, false)
	if root == nil {
		return nil
	}
	hang := make([]*Node, len(sk.blks))
	body := make([]bool, len(sk.blks))
	var defs []Def
	for b := range sk.blks {
		bl := &sk.blks[b]
		p, inBody := root, true
		if bl.parent >= 0 {
			p, inBody = hang[bl.parent], body[bl.parent]
		}
		if p == nil {
			continue
		}
		name := guide.name(bl.key, bl.quoted, true, p.Flags&Rows != 0)
		n := p.Kids[name]
		hang[b] = n
		if name == SegKind && n != nil {
			hang[b], body[b] = guide.body(p, n, false), guide.byName == nil && p.Def != ""
		}
		if !bl.names || bl.quoted {
			continue
		}
		id := game.KeyIdentity(f.GameID, bl.key)
		if !jomini.IsDefName(id) || jomini.IsStop(id) {
			continue
		}
		kind := cited[id]
		if kind != "" && !shapes[[2]string{f.Kind, kind}] {
			kind = ""
		}
		if kind == "" && !inBody && p.Flags&Rows != 0 {
			kind = p.Def
		}
		if kind == "" {
			continue
		}
		d := f.def(kind, id, bl.line, bl.at, bl.to)
		d.Nested = true
		defs = append(defs, d)
	}
	return defs
}

// -------------------------------------------------------------------- pass 3

// resolve is pass 3 over one file: a value becomes a reference when it resolves to a member of its node's type, and a reference of kind event, on_action or a macro kind inside a definition is also an edge.
func (f *File) resolve(trees []*Tree, r *resolver, decl *declared.Schema, wrappers map[string]bool) *Harvest {
	h := &Harvest{Path: f.Path, Rel: f.Rel, Origin: f.Origin, Order: f.Order, Kind: f.Kind}
	d := newDescent(f, trees, false)
	n := len(f.Facts)
	from := make([]string, n)  // the enclosing top-level definition
	option := make([]int32, n) // the enclosing option block
	var labels []string
	for i := range f.Facts {
		i := int32(i)
		d.step(i)
		k := &f.Facts[i]
		option[i] = -1
		if k.Depth == 0 {
			if key := f.topDef(i); key != "" && !wrappers[key] {
				from[i] = key
			} else if key == "" && k.Block && k.Key != "" && !k.Quoted && f.afterKeyword(i) {
				from[i] = k.Key
			}
			continue
		}
		p := k.Parent
		from[i], option[i] = from[p], option[p]
		if strings.EqualFold(k.Key, "option") {
			option[i] = i
		}
		key, kline, kat := k.Key, k.Line, k.Start
		if k.Key == "" {
			if f.litList[p] {
				continue
			}
			key, kline, kat = f.Facts[p].Key, f.Facts[p].Line, f.Facts[p].Start
		}
		// A keyed statement inside a definition whose key names a scripted_* macro is an invocation of it.
		if k.Key != "" && !k.Quoted && from[i] != "" && from[i] != k.Key {
			if kind := r.macros[k.Key]; kind != "" {
				h.Refs = append(h.Refs, f.ref(kind, k.Key, kline, kat, kat+int32(len(k.Key))))
				h.Edges = append(h.Edges, Edge{From: from[i], To: strings.Clone(k.Key), Path: f.Path, Line: int(kline), Kind: EdgeKindCall})
			}
		}
		if k.Block || k.Value == "" {
			continue
		}
		v, q := k.Value, k.VQuoted
		if k.Key == "" {
			q = k.Quoted
		}
		labels = r.classify(v, q, f.Data, labels)
		kind := refKind(d.first(i), labels, key, v)
		if kind == "" {
			continue
		}
		ref := f.ref(kind, v, k.VLine, k.VStart, k.VStart+int32(len(v)))
		ref.Derived = !decl.HasName(key) && !isLocKind(kind)
		h.Refs = append(h.Refs, ref)
		if !isFireKind(kind) {
			continue
		}
		slot, rescoped := f.fireSite(d, decl, i, option[i])
		via := key
		if lk := strings.ToLower(key); lk == "id" || isDigits(lk) {
			if pk := f.Facts[p].Key; k.Key != "" && pk != "" {
				via = pk
			}
		}
		e := Edge{From: from[i], To: strings.Clone(v), Via: strings.Clone(via), Path: f.Path,
			Line: int(k.VLine), Kind: slot, Rescoped: rescoped}
		if option[i] >= 0 {
			e.NameKey = strings.Clone(f.optionName(option[i]))
		}
		h.Edges = append(h.Edges, e)
	}
	if len(h.Refs) == 0 && len(h.Edges) == 0 {
		return nil
	}
	return h
}

// fireSite is where one fire site sits and what it runs against: the option it belongs to or the class of the body around it, and whether an ancestor block entered a scope of its own, so the event does NOT run in the scope its source declares.
func (f *File) fireSite(d *descent, decl *declared.Schema, i, option int32) (slot string, rescoped bool) {
	for a := f.Facts[i].Parent; a >= 0 && f.Facts[a].Depth > 0; a = f.Facts[a].Parent {
		k := &f.Facts[a]
		if k.Key == "" {
			continue
		}
		// A key the fold turns into a class names the thing it enters -- `scope:activity = { … }`, a nested definition, a numbered branch -- and is never a section of the scope around it.
		if c, _ := d.trees[0].KeyClass(k.Key, k.Quoted, d.row[a]); classes[c].seg != "" || changesScope(decl, k.Key) {
			rescoped = true
			break
		}
	}
	if option >= 0 {
		return "option", rescoped
	}
	if b := d.first(f.Facts[i].Parent); b != nil && b.Body&BodyEffect == 0 && b.Body&BodyTrigger != 0 {
		return "trigger", rescoped
	}
	return "effect", rescoped
}

// changesScope reports a block key the game itself declares as a hop: a link, or a token whose target is a scope script runs in.
func changesScope(decl *declared.Schema, key string) bool {
	if decl == nil {
		return false
	}
	if l, ok := decl.Links[strings.ToLower(key)]; ok {
		return l.Out != "" && !strings.Contains(l.Out, ",") && decl.Scopes[l.Out].ChangeScopes
	}
	if tok, _, ok := decl.Token(key); ok {
		return tok.Target != "" && decl.Scopes[tok.Target].ChangeScopes
	}
	return false
}

// refKind is the reporting policy's first clause: a value resolves to a member of its node's type, or to nothing. Declared beats derived: the engine's own localization properties hold a key wherever they are written.
func refKind(cell *Node, labels []string, key, value string) string {
	switch loc.Classify(key) {
	case loc.PropStrict:
		if loc.LooksLikeKey(value) {
			return "loc"
		}
		return ""
	case loc.PropBroad:
		if loc.LooksLikeKey(value) {
			return "loc-broad"
		}
		return ""
	}
	// An all-caps argument key holds a fragment the macro pastes into a name, not an object of the field's type.
	if cell == nil || isMacroArgKey(key) {
		return ""
	}
	// A database before localization: every culture has a loc key of its own name, so `culture = english` is a culture.
	for _, k := range cell.Kinds {
		if k != LabelLoc && slices.Contains(labels, LabelDef+k) {
			return k
		}
	}
	if slices.Contains(cell.Kinds, LabelLoc) && slices.Contains(labels, LabelLoc) {
		return "loc-broad"
	}
	// At a node that names a database and never held a literal, a name-shaped value is a reference whether or not anything defines it: the dangling report is made of these.
	if !cell.Has(LabelLiteral) && jomini.IsDefName(value) {
		for _, k := range cell.Kinds {
			if k != LabelLoc {
				return k
			}
		}
	}
	return ""
}

func isFireKind(kind string) bool {
	return kind == jomini.KindEvent || kind == "on_action" || jomini.IsMacroKind(kind)
}

// ------------------------------------------------------------- localization

const locValueLimit = 200

// ApplyLoc is the localization half of the rows: one entry is one value, a `$key$` interpolation is a reference, and a data-function name reads a key the loc files need not define.
func ApplyLoc(f *File, text string) *Harvest {
	h := &Harvest{Path: f.Path, Rel: f.Rel, Origin: f.Origin, Order: f.Order, Kind: "loc"}
	r := loc.Parse(text)
	vals := map[string]LocEntry{}
	for _, e := range r.Entries {
		v := e.Value
		if len(v) > locValueLimit {
			v = v[:locValueLimit]
		}
		vals[strings.Clone(e.Key)] = LocEntry{Value: strings.Clone(v), Path: f.Path, Line: e.Line, Start: e.KeyRange.Start, Origin: f.Origin}
		for _, ip := range loc.Interps(e.Value, e.ValueRange.Start) {
			if !loc.IsLocEngineValue(ip.Key, ip.Filter) {
				h.Refs = append(h.Refs, f.ref("loc", ip.Key, int32(e.Line), int32(ip.KeyRange.Start), int32(ip.KeyRange.End)))
			}
		}
		for _, ip := range loc.DataFnKeys(e.Value, e.ValueRange.Start) {
			h.Refs = append(h.Refs, f.ref("loc-broad", ip.Key, int32(e.Line), int32(ip.KeyRange.Start), int32(ip.KeyRange.End)))
		}
	}
	h.Loc = LocDelta{Lang: loc.LanguageOf(f.Path, r.Language), Vals: vals}
	return h
}

// ---------------------------------------------------------- small shapes

const maxDefDocLines = 4

// docWorthyKind reports a definition whose declaration site is a line an author can comment; for most objects the only description that will ever exist.
func docWorthyKind(kind string) bool {
	switch jomini.CanonicalKind(kind) {
	case "loc_key", "loc_value", "mod_descriptor", "":
		return false
	}
	return !jomini.IsEphemeral(kind)
}

// leadingComment reads the comment block written directly above a definition; a blank line ends it.
func leadingComment(src string, off int) string {
	if off > len(src) {
		return ""
	}
	line := strings.LastIndexByte(src[:off], '\n') + 1
	var rev []string
	for line > 0 && len(rev) < maxDefDocLines {
		prev := strings.LastIndexByte(src[:line-1], '\n') + 1
		body, hashed := jomini.PeelComment(src[prev : line-1])
		if !hashed || body == "" || strings.Trim(body, "=-*_ ") == "" {
			break
		}
		rev = append(rev, body)
		line = prev
	}
	if len(rev) == 0 {
		return ""
	}
	slices.Reverse(rev)
	return strings.Clone(jomini.AuthorProse(rev))
}

// isMacroArgKey reports the all-caps convention for a scripted macro's arguments: `some_effect = { ROLE = chemist }`.
func isMacroArgKey(key string) bool {
	upper := false
	for i := range len(key) {
		switch c := key[i]; {
		case c >= 'A' && c <= 'Z':
			upper = true
		case c >= 'a' && c <= 'z':
			return false
		}
	}
	return upper
}

type dollarSpan struct {
	name       string
	start, end int32
}

func dollarParamsIn(text string, base int32) []dollarSpan {
	var out []dollarSpan
	for i := 0; i < len(text); {
		if text[i] != '$' {
			i++
			continue
		}
		j := i + 1
		for j < len(text) && jomini.IsScriptParamByte(text[j], j == i+1) {
			j++
		}
		if j == i+1 || j >= len(text) || text[j] != '$' {
			i++
			continue
		}
		out = append(out, dollarSpan{name: text[i+1 : j], start: base + int32(i), end: base + int32(j) + 1})
		i = j + 1
	}
	return out
}
