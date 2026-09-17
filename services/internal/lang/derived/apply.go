// apply.go is the two phases. Phase A walks one file's facts once, emits the
// ten fixed grammar rows that need no global knowledge, and records every
// remaining value as an occurrence in flat interned arrays. Phase B runs when
// the definition and localization indexes are complete: it folds the paths,
// joins the nested databases, folds again, classifies every cell, and resolves
// the recorded occurrences into references and edges. Nothing here votes.

package derived

import (
	"context"
	"slices"
	"strings"
	"sync"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/declared"
	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/loc"
)

// EdgeKindCall marks a scripted-effect invocation (not drawn on the canvas).
const EdgeKindCall = "call"

// EdgeKindVia marks a hop synthesized through a chain of macro calls.
const EdgeKindVia = "via"

var slotNames = [...]string{"effect", "trigger", "option"}

// pnode is one enclosing block: its parent node and the key as written. The fold turns a chain of these into a Cell.Path, twice -- once before the nested join and once after it, when a block key that has become a definition folds to <kind>.
type pnode struct {
	parent int32
	key    string
	quoted bool
}

// occ is one recorded value: which cell it sat at, what was written, and the two token spans a reference or a call is stamped at. The kind is the file's, so it is not repeated here.
type occ struct {
	key, value  string
	node        int32 // enclosing block, index into fileRecs.nodes; -1 at the definition's own level
	from        int32 // enclosing definition, index into fileRecs.defKeys; -1 outside one
	name        int32 // option name, index into fileRecs.names; -1
	kline, kat  int32
	vline, vat  int32
	flags, slot uint8
}

const (
	oQuoted   uint8 = 1 << iota // the key is quoted
	oVQuoted                    // the value is quoted
	oBlock                      // the right-hand side is a block
	oNames                      // the block holds keyed children
	oEmpty                      // the block holds nothing
	oRescoped                   // an ancestor key changed scope
	oDeclared                   // the key is a name the game declares
	oTop                        // the statement is the file's own top level
)

// tally is the occurrences at one cell that can never become a reference -- literals, stop words and citations already emitted -- kept as a count rather than a record.
type tally struct {
	node  int32
	key   string
	label string
	n     int32
	top   bool
}

// wrapCand is a top-level block whose every member is a typed cite; it is a wrapper if the corpus defines all of them.
type wrapCand struct {
	key string
	ids []string
}

// fileRecs is one file's Phase A output beside its records: the block chain every occurrence hangs from, the definitions it can belong to, and the occurrences themselves. It holds no facts, so the parse tree and the file's text go as soon as the file has been read.
type fileRecs struct {
	fileID
	nodes   []pnode
	defKeys []string
	names   []string
	occs    []occ
	tallies []tally
	cites   []typedCite
	wraps   []wrapCand
	// paths, keys and tkeys are the fold's output, rebuilt on each fold.
	paths, keys, tkeys []string
	kind               string
	data               bool // the file's kind is a data-tier database, where a number may name a row
}

// FileHarvest is everything one file emits: the records the rows made outright, and the occurrences Phase B still owes an answer.
type FileHarvest struct {
	fileID
	Defs  []Def
	Refs  []Ref
	Edges []Edge
	Loc   LocDelta
	recs  *fileRecs
	// dataFns are the `GetX`/`SetX` names this file's source mentions, harvested only when the install ships no data-type dump.
	dataFns map[string]bool
}

// ---------------------------------------------------------------- phase A

// applyCtx is the per-file state the pre-order walk supplies for free: the enclosing definition, the path node, the slot and the file's own macro set.
type applyCtx struct {
	ff    *FileFacts
	h     *FileHarvest
	r     *fileRecs
	sc    *declared.Schema
	kind  string
	nsKey string

	nodeAt   []int32 // the path node a fact's cell hangs from
	nodeIdx  []int32 // the node this fact opened, -1 when it opened none
	encl     []int32
	preset   []bool
	slot     []uint8
	optionAt []int32
	rescoped []bool
	fdepth   []int32
	object   []bool
	empty    []bool
	litList  []bool
	defKeyID map[string]int32
	nameID   map[string]int32
	macros   map[string]bool
	params   map[string]map[string]bool
	seenPar  map[string]bool
	tallyID  map[tallyKey]int32
}

type tallyKey struct {
	node  int32
	key   string
	label string
	top   bool
}

// Apply is Phase A over one parsed file.
func Apply(ff FileFacts, sc *declared.Schema) *FileHarvest {
	h := &FileHarvest{fileID: ff.fileID}
	kind := ff.Rule.Kind
	if ff.Rule.Mode == game.ModeEventID {
		kind = jomini.KindEvent
	}
	r := &fileRecs{fileID: ff.fileID, kind: kind,
		data: game.IsDataKind(ff.GameID, kind)}
	h.recs = r
	n := len(ff.Facts)
	c := &applyCtx{
		ff: &ff, h: h, r: r, sc: sc, kind: kind,
		nodeAt: make([]int32, n), nodeIdx: make([]int32, n),
		encl: make([]int32, n), preset: make([]bool, n),
		slot: make([]uint8, n), optionAt: make([]int32, n),
		rescoped: make([]bool, n), fdepth: make([]int32, n),
		defKeyID: map[string]int32{}, nameID: map[string]int32{},
		macros: map[string]bool{}, params: map[string]map[string]bool{},
		seenPar: map[string]bool{}, tallyID: map[tallyKey]int32{},
	}
	c.object, c.empty, c.litList = blockShapes(ff.Facts)
	for i := range ff.Facts {
		f := &ff.Facts[i]
		c.prologue(int32(i), f)
		c.rows(int32(i), f)
		c.record(int32(i), f)
	}
	// One more walk for the row whose set is complete only when the file is: an argument key may sit in a call written above the macro it fills.
	c.argRefs()
	r.occs = slices.Clip(r.occs)
	r.nodes = slices.Clip(r.nodes)
	return h
}

// blockShapes marks every block that holds keyed children, every empty one, and every list whose bare members are all numbers -- `color = { 120 30 40 }` is a literal, not a list of references.
func blockShapes(facts []Fact) (names, empty, litList []bool) {
	names = make([]bool, len(facts))
	empty = make([]bool, len(facts))
	litList = make([]bool, len(facts))
	any := make([]bool, len(facts))
	nonNum := make([]bool, len(facts))
	for i := range facts {
		p := facts[i].Parent
		if p < 0 {
			continue
		}
		any[p] = true
		if facts[i].Key != "" {
			names[p] = true
			nonNum[p] = true
			continue
		}
		if !isNumberLike(facts[i].Value) {
			nonNum[p] = true
		}
	}
	for i := range facts {
		empty[i] = !any[i]
		litList[i] = any[i] && !nonNum[i]
	}
	return names, empty, litList
}

func isNumberLike(s string) bool {
	return s != "" && jomini.IsLiteralValue(s) && !jomini.IsDateLiteral(s)
}

// prologue is the context the enclosing blocks decide, carried forward as the facts arrive so nothing walks back up its parents.
func (c *applyCtx) prologue(i int32, f *Fact) {
	c.nodeAt[i], c.nodeIdx[i], c.optionAt[i] = -1, -1, -1
	if !c.preset[i] {
		c.encl[i] = -1
	}
	c.slot[i], c.rescoped[i], c.fdepth[i] = 0, false, 0
	if p := f.Parent; p >= 0 {
		if !c.preset[i] {
			c.encl[i] = c.encl[p]
		}
		c.nodeAt[i] = c.nodeIdx[p]
		c.slot[i], c.optionAt[i] = c.slot[p], c.optionAt[p]
		c.rescoped[i], c.fdepth[i] = c.rescoped[p], c.fdepth[p]
	}
	if f.Block && f.Depth > 0 {
		c.nodeIdx[i] = int32(len(c.r.nodes))
		c.r.nodes = append(c.r.nodes,
			pnode{parent: c.nodeAt[i], key: f.Key, quoted: f.Quoted})
	}
	if f.Key == "" {
		return
	}
	if c.fdepth[i] >= 1 && !jomini.IsScopeNeutral(f.Key) {
		c.rescoped[i] = true
	}
	c.fdepth[i]++
	if strings.EqualFold(f.Key, "option") {
		c.slot[i], c.optionAt[i] = 2, i
	} else if slot := jomini.ScriptSlot(f.Key); slot != "" {
		c.optionAt[i] = -1
		if slot == "trigger" {
			c.slot[i] = 1
		} else {
			c.slot[i] = 0
		}
	}
}

// rows are the ten fixed grammar rows of the spec's §4, in the order a file meets them.
func (c *applyCtx) rows(i int32, f *Fact) {
	ff := c.ff
	switch {
	// 1 a top-level `name = { }` in a kind's folder is a definition of that kind.
	case f.Depth == 0 && f.Block && ff.Rule.Mode == game.ModeTopLevelKey &&
		(f.Op == "=" || f.Op == "?=") && !f.Quoted && f.Key != "namespace":
		if name := game.KeyIdentity(ff.GameID, f.Key); jomini.IsDefName(name) &&
			!jomini.IsDateLiteral(name) {
			c.def(c.kind, name, i, f.Line, f.Start, f.End, true, false, false)
		}
	// 2 `namespace = x` declares the file's event-id prefix, and `ns.123 = { }` is an event that refers to it.
	case ff.Rule.Mode == game.ModeEventID && f.Depth == 0 && (f.Op == "=" || f.Op == "?="):
		if !f.Quoted && f.Key == "namespace" && !f.VQuoted && jomini.IsDefName(f.Value) {
			c.def(jomini.KindNamespace, f.Value, i, f.VLine, f.VStart, f.VEnd, true, false, false)
			c.nsKey = f.Value
		} else if !f.Quoted && jomini.IsEventID(f.Key) {
			c.def(jomini.KindEvent, f.Key, i, f.Line, f.Start, f.End, true, false, false)
			if c.nsKey != "" {
				c.h.Refs = append(c.h.Refs,
					ff.ref(jomini.KindNamespace, c.nsKey, f.Line, f.Start, f.End))
			}
		}
	}
	// 3 `type NAME = { }`, `template NAME`, `local_template` and the `scripted_*` keywords name the statement that follows them.
	if f.Key == "" && !f.Quoted && ff.Rule.Mode != game.ModeTopLevelKey {
		c.keywordName(i, f)
	}
	// 4 the script-name table: `set_variable` and its twenty siblings define an ephemeral name, `has_variable` and its own refer to one.
	if f.Key != "" && !f.Quoted {
		c.scriptName(i, f)
	}
	// 5 `$PARAM$` inside a macro body: the first sighting declares the parameter, the rest use it.
	c.dollarParams(i, f)
	// 6 every `prefix:id` span in a key or an unquoted value resolves through the declared prefixes.
	c.cites(f)
	// 7 a top-level block whose keyed children are all cites is a transparent container, not a definition.
	if f.Depth == 0 && f.Block && f.Key != "" && !f.Quoted &&
		!jomini.DeclaresNamedBodies(c.kind) && !c.afterKeyword(i) {
		c.wrapCandidate(i, f)
	}
}

// keywordName is row 3: a bare declaration keyword whose next sibling's key is the name it declares.
func (c *applyCtx) keywordName(i int32, f *Fact) {
	kind := jomini.DeclKeywordKind(f.Value)
	if kind == "" || kind == "types" {
		return
	}
	facts := c.ff.Facts
	j := i + 1
	if j >= int32(len(facts)) {
		return
	}
	n := &facts[j]
	if n.Parent != f.Parent || n.Key == "" || n.Quoted || !jomini.IsDefName(n.Key) {
		return
	}
	d := c.ff.def(kind, n.Key, n.Line, n.Start, n.End)
	d.Local = jomini.DeclIsLocal(f.Value)
	if c.ff.Rule.Mode != game.ModeGUIType && docWorthyKind(d.Kind) {
		d.Doc = leadingComment(c.ff.Src, d.Start)
	}
	c.addDef(d, j)
}

// scriptName is rows 4 and 10 of the table jomini keeps: the twenty-one keys that name an ephemeral value, as a definition or as a use of one.
func (c *applyCtx) scriptName(i int32, f *Fact) {
	r, ok := jomini.ScriptName(f.Key)
	if !ok {
		return
	}
	name, line, start, end, found := c.scriptNameSpan(i, r.InnerKey)
	if !found || name == "" {
		return
	}
	if !r.IsDef {
		c.h.Refs = append(c.h.Refs, c.ff.ref(r.Kind, name, line, start, end))
		return
	}
	d := c.ff.def(r.Kind, name, line, start, end)
	d.Nested = true
	d.Value = c.scriptNameValue(i, r)
	c.addDef(d, -1)
}

// scriptNameSpan is the name token span of a ScriptName assignment. A scalar RHS is the name; a block uses InnerKey (flag/name) when the rule sets one.
func (c *applyCtx) scriptNameSpan(i int32, innerKey string) (string, int32, int32, int32, bool) {
	facts := c.ff.Facts
	f := &facts[i]
	if !f.Block && !f.VQuoted && f.Value != "" {
		return f.Value, f.VLine, f.VStart, f.VEnd, true
	}
	if innerKey == "" || !f.Block {
		return "", 0, 0, 0, false
	}
	for j := i + 1; j < int32(len(facts)) && facts[j].Parent >= i; j++ {
		ch := &facts[j]
		if ch.Parent != i || ch.Quoted || ch.Key != innerKey {
			continue
		}
		if ch.Block || ch.VQuoted || ch.Value == "" {
			return "", 0, 0, 0, false
		}
		return ch.Value, ch.VLine, ch.VStart, ch.VEnd, true
	}
	return "", 0, 0, 0, false
}

// scriptNameValue is the hover RHS for a ScriptName def. Scalar saves (save_scope_as) have no value; their hover body is the ancestor scope expr.
func (c *applyCtx) scriptNameValue(i int32, r jomini.ScriptNameRule) string {
	facts := c.ff.Facts
	f := &facts[i]
	if r.Kind == "saved_scope" && r.InnerKey == "" {
		return ""
	}
	if !f.Block {
		return "yes"
	}
	for j := i + 1; j < int32(len(facts)) && facts[j].Parent >= i; j++ {
		ch := &facts[j]
		if ch.Parent == i && !ch.Quoted && ch.Key == "value" &&
			!ch.Block && !ch.VQuoted && ch.Value != "" {
			return ch.Value
		}
	}
	return "yes"
}

// dollarParams is row 5: the first `$NAME$` in a macro body is the parameter, the rest are uses of it.
func (c *applyCtx) dollarParams(i int32, f *Fact) {
	if f.Key == "" || f.Quoted || len(c.macros) == 0 {
		return
	}
	facts := c.ff.Facts
	owner := i
	for facts[owner].Parent >= 0 {
		owner = facts[owner].Parent
	}
	if !facts[owner].Block || !c.macros[facts[owner].Key] {
		return
	}
	ownerKey := facts[owner].Key
	names := c.params[ownerKey]
	if names == nil {
		names = map[string]bool{}
		c.params[ownerKey] = names
	}
	record := func(text string, base, line int32) {
		for _, sp := range dollarParamsIn(text, base) {
			names[sp.name] = true
			id := ownerKey + "\x00" + sp.name
			if !c.seenPar[id] {
				c.seenPar[id] = true
				d := c.ff.def(jomini.KindScriptParam, sp.name, line, sp.start, sp.end)
				d.OwnerKey = ownerKey
				c.h.Defs = append(c.h.Defs, d)
				continue
			}
			r := c.ff.ref(jomini.KindScriptParam, sp.name, line, sp.start, sp.end)
			r.OwnerKey = ownerKey
			c.h.Refs = append(c.h.Refs, r)
		}
	}
	record(f.Key, f.Start, f.Line)
	if !f.Block && !f.VQuoted {
		record(f.Value, f.VStart, f.VLine)
	}
}

// argRefs completes row 5: an argument key inside a call to macro M that is one of M's parameters. It runs after the pass because a call may be written above the macro it fills.
func (c *applyCtx) argRefs() {
	if len(c.params) == 0 {
		return
	}
	facts := c.ff.Facts
	call := make([]int32, len(facts))
	for i := range facts {
		f := &facts[i]
		ck := int32(-1)
		if f.Parent >= 0 {
			ck = call[f.Parent]
		}
		if f.Key != "" && !f.Quoted {
			if ck >= 0 {
				if owner := facts[ck].Key; c.params[owner][f.Key] {
					r := c.ff.ref(jomini.KindScriptParam, f.Key, f.Line, f.Start, f.End)
					r.OwnerKey = owner
					c.h.Refs = append(c.h.Refs, r)
				}
			}
			if c.macros[f.Key] {
				ck = int32(i)
			}
		}
		call[i] = ck
	}
}

// cites is row 6: every `prefix:id` span in a key or an unquoted value, recorded for the nested join and emitted as a reference.
func (c *applyCtx) cites(f *Fact) {
	add := func(text string, base, line int32) {
		for _, sp := range jomini.TypedSpans(text) {
			if jomini.PrefixKind(sp.Prefix) != "" {
				continue
			}
			c.r.cites = append(c.r.cites, typedCite{prefix: sp.Prefix, id: sp.ID})
		}
		if p, ok := jomini.ParsePrefixed(text); ok {
			kind := jomini.PrefixKind(p.Prefix)
			if kind == "" {
				kind = p.Prefix
			}
			at := base + int32(p.NameOff)
			c.h.Refs = append(c.h.Refs,
				c.ff.ref(kind, p.Name, line, at, at+int32(len(p.Name))))
			return
		}
		if kind, id, ok := game.ParseTyped(c.ff.GameID, text); ok {
			c.h.Refs = append(c.h.Refs, c.ff.ref(kind, id, line, base, base+int32(len(text))))
		}
	}
	if f.Key != "" && !f.Quoted {
		add(f.Key, f.Start, f.Line)
	}
	if !f.Block && !f.VQuoted && f.Value != "" {
		add(f.Value, f.VStart, f.VLine)
	}
}

// afterKeyword reports a statement introduced by a bare declaration keyword, which declares a named body rather than opening a container.
func (c *applyCtx) afterKeyword(i int32) bool {
	if i == 0 {
		return false
	}
	p := &c.ff.Facts[i-1]
	if p.Key != "" || p.Quoted || p.Parent != c.ff.Facts[i].Parent {
		return false
	}
	k := jomini.DeclKeywordKind(p.Value)
	return k != "" && k != "types"
}

// wrapCandidate is row 7 (Vic3's `COUNTRIES = { c:USA ?= { } }`).
func (c *applyCtx) wrapCandidate(i int32, f *Fact) {
	facts := c.ff.Facts
	w, any := wrapCand{key: f.Key}, false
	for j := i + 1; j < int32(len(facts)) && facts[j].Parent >= i; j++ {
		ch := &facts[j]
		if ch.Parent != i || ch.Key == "" || ch.Quoted {
			continue
		}
		any = true
		kind, id, ok := game.ParseTyped(c.ff.GameID, ch.Key)
		if !ok {
			if sp, sok := jomini.TypedSpanAt(ch.Key, 0); sok {
				kind, id, ok = game.ParseTyped(c.ff.GameID, sp.Prefix+":"+sp.ID)
			}
		}
		if !ok || kind == "" {
			return
		}
		w.ids = append(w.ids, id)
	}
	if any {
		c.r.wraps = append(c.r.wraps, w)
	}
}

// def emits one definition and binds the facts under it to it, so an edge's From is the enclosing definition rather than the nearest one by line.
func (c *applyCtx) def(kind, name string, i, line, start, end int32, doc, nested, local bool) {
	d := c.ff.def(kind, name, line, start, end)
	d.Nested, d.Local = nested, local
	if doc && docWorthyKind(d.Kind) {
		d.Doc = leadingComment(c.ff.Src, d.Start)
	}
	c.addDef(d, i)
}

// addDef records a definition and, with it, the two things the pre-order walk then supplies for free: the file's macro set, and the enclosing definition every edge and call site is filed under.
func (c *applyCtx) addDef(d Def, site int32) {
	c.h.Defs = append(c.h.Defs, d)
	if !IsCitableDef(d) {
		return
	}
	k := jomini.CanonicalKind(d.Kind)
	if k != jomini.KindNamespace && k != jomini.KindGUIType {
		c.macros[d.Key] = true
	}
	if site < 0 || !jomini.IsObjectKind(d.Kind) {
		return
	}
	id, ok := c.defKeyID[d.Key]
	if !ok {
		id = int32(len(c.r.defKeys))
		c.r.defKeys = append(c.r.defKeys, d.Key)
		c.defKeyID[d.Key] = id
	}
	c.encl[site], c.preset[site] = id, true
}

// record files one statement as an occurrence or, when it can never be a reference, as a count at its cell.
func (c *applyCtx) record(i int32, f *Fact) {
	// A top-level key is a definition the rows already emitted; its body is still one cell per kind, which is what the join reaches through.
	if f.Depth == 0 {
		if f.Block && f.Key != "" && !f.Quoted && c.object[i] {
			c.r.occs = append(c.r.occs, occ{
				node: -1, from: -1, name: -1, flags: oTop | oBlock | oNames})
		}
		return
	}
	key, kline, kat, kq := f.Key, f.Line, f.Start, f.Quoted
	node := c.nodeAt[i]
	if key == "" {
		// A bare value is folded into the cell of the block key that holds it.
		if p := f.Parent; p >= 0 {
			key, kline, kat, kq = c.ff.Facts[p].Key, c.ff.Facts[p].Line, c.ff.Facts[p].Start, c.ff.Facts[p].Quoted
			node = c.nodeAt[p]
			if c.litList[p] {
				return // `color = { 120 30 40 }` is a literal, not a list of references
			}
		}
	}
	flags := uint8(0)
	if f.Depth == 0 {
		flags |= oTop
	}
	if kq {
		flags |= oQuoted
	}
	if f.Block {
		flags |= oBlock
		if c.object[i] {
			flags |= oNames
		}
		if c.empty[i] {
			flags |= oEmpty
		}
	}
	if f.VQuoted {
		flags |= oVQuoted
	}
	if c.rescoped[i] {
		flags |= oRescoped
	}
	if f.Block {
		// A block is kept whole: its key may be a nested definition or a macro invocation, and its shape is the cell's label.
		c.occs(i, key, "", node, kline, kat, f.VLine, f.VStart, flags)
		return
	}
	v, vq := f.Value, f.VQuoted
	if f.Key == "" {
		v, vq = f.Value, f.Quoted
	}
	// A declared localization property holds a key wherever it is written, and loc keys are routinely dotted, so those values are kept whatever shape they wear.
	label, keep := valueShape(c.ff.GameID, v, vq, c.r.data)
	if keep || loc.Classify(key) != loc.PropNone && loc.LooksLikeKey(v) {
		c.occs(i, key, v, node, kline, kat, f.VLine, f.VStart, flags)
		return
	}
	c.tally(node, key, label, 1, flags&oTop != 0)
}

func (c *applyCtx) occs(i int32, key, value string, node, kline, kat, vline, vat int32, flags uint8) {
	// Only a record ever reads it, and the lookup is one of the hottest in the pass.
	if key != "" && c.sc.HasName(key) {
		flags |= oDeclared
	}
	name := int32(-1)
	if c.optionAt[i] >= 0 && c.slot[i] == 2 {
		name = c.optionName(c.optionAt[i])
	}
	c.r.occs = append(c.r.occs, occ{
		key: key, value: value, node: node, from: c.encl[i], name: name,
		kline: kline, kat: kat, vline: vline, vat: vat,
		flags: flags, slot: c.slot[i],
	})
}

func (c *applyCtx) tally(node int32, key, label string, n int32, top bool) {
	k := tallyKey{node: node, key: key, label: label, top: top}
	if at, ok := c.tallyID[k]; ok {
		c.r.tallies[at].n += n
		return
	}
	c.tallyID[k] = int32(len(c.r.tallies))
	c.r.tallies = append(c.r.tallies,
		tally{node: node, key: key, label: label, n: n, top: top})
}

func (c *applyCtx) optionName(at int32) int32 {
	facts := c.ff.Facts
	name := ""
	for j := at + 1; j < int32(len(facts)) && facts[j].Parent >= at; j++ {
		ch := &facts[j]
		if ch.Parent == at && ch.Key != "" && strings.EqualFold(ch.Key, "name") && !ch.Block {
			name = ch.Value
			break
		}
	}
	if name == "" {
		return -1
	}
	id, ok := c.nameID[name]
	if !ok {
		id = int32(len(c.r.names))
		c.r.names = append(c.r.names, name)
		c.nameID[name] = id
	}
	return id
}

// valueShape is one syntactic pass per value: the label no lookup can change, and whether the value is worth a record -- a quantity, a flag, a stop word or a citation already emitted is counted at its cell and dropped.
func valueShape(gameID, v string, quoted, dataKind bool) (label string, keep bool) {
	if v == "" {
		return LabelLiteral, false
	}
	if quoted {
		return LabelLiteral, true // still a possible localization key
	}
	if jomini.IsStop(v) {
		// `yes` and `no` are stop words, and a macro invoked with a scalar right-hand side is written `= yes`.
		return LabelStop, strings.EqualFold(v, "yes") || strings.EqualFold(v, "no")
	}
	if kind, _, ok := game.ParseTyped(gameID, v); ok {
		return LabelCite + jomini.CanonicalKind(kind), false
	}
	if p, ok := jomini.ParsePrefixed(v); ok {
		return LabelCite + jomini.PrefixKind(p.Prefix), false
	}
	if jomini.IsDateLiteral(v) || strings.ContainsAny(v, "$@|;") {
		return LabelLiteral, false
	}
	// A quantity is the bulk of an install's values and can never be a reference -- except in a data-tier file, where a row is named by its number.
	if jomini.IsLiteralValue(v) {
		return LabelLiteral, dataKind
	}
	return LabelLiteral, jomini.IsDefName(v)
}

// ---------------------------------------------------------------- the fold

// fold rebuilds one file's path strings from its node chain. isDef grows between the two folds: after the nested join a block key that has become a definition folds to <kind>.
func (r *fileRecs) fold(isDef func(string) bool, keys bool) {
	if cap(r.paths) < len(r.nodes) {
		r.paths = make([]string, len(r.nodes))
	}
	r.paths = r.paths[:len(r.nodes)]
	for i, n := range r.nodes {
		seg := foldSeg(r.GameID, n.key, n.quoted, isDef)
		if n.parent < 0 {
			r.paths[i] = seg
			continue
		}
		r.paths[i] = r.paths[n.parent] + "." + seg
	}
	// The key half folds by the same rule, and only the second fold needs it: the join reads the keys as the corpus writes them.
	if !keys {
		return
	}
	if cap(r.keys) < len(r.occs) {
		r.keys = make([]string, len(r.occs))
	}
	r.keys = r.keys[:len(r.occs)]
	for i := range r.occs {
		o := &r.occs[i]
		r.keys[i] = foldKey(r.GameID, o.key,
			o.flags&oBlock != 0, o.flags&oQuoted != 0, isDef)
	}
	if cap(r.tkeys) < len(r.tallies) {
		r.tkeys = make([]string, len(r.tallies))
	}
	r.tkeys = r.tkeys[:len(r.tallies)]
	for i := range r.tallies {
		// A tallied record is a scalar or a list, never a keyed block.
		r.tkeys[i] = foldKey(r.GameID, r.tallies[i].key, false, false, isDef)
	}
}

func (r *fileRecs) pathOf(node int32) string {
	if node < 0 || int(node) >= len(r.paths) {
		return ""
	}
	return r.paths[node]
}

// keyAt re-spells the keys that sit at a database position, which is known only once every file has folded.
func (r *fileRecs) keyAt(db map[string]bool) {
	for i := range r.occs {
		o := &r.occs[i]
		if o.flags&oBlock == 0 || o.flags&oQuoted != 0 || r.keys[i] != o.key {
			continue
		}
		if db[r.kind+"\x00"+r.cellPath(o.node, o.flags&oTop != 0)] &&
			!jomini.IsStop(o.key) {
			r.keys[i] = segKind
		}
	}
}

// cellPath is where one record's cell sits: the definition's own body, or the folded chain of blocks below it.
func (r *fileRecs) cellPath(node int32, top bool) string {
	if top {
		return segTop
	}
	return r.pathOf(node)
}

// ---------------------------------------------------------------- phase B

// learn is Phase B: fold, join the nested databases, fold again, classify every cell, resolve the records -- and for a workspace, apply the install's schema rather than learn one.
func (x *Index) learn(ctx context.Context, files []*fileRecs, locLang string, bases ...*Index) {
	s := &x.Schema
	install := locLang != ""
	kindsByKey, keysByKind := citableOver(x, bases)
	if install && !x.Declared.Empty() {
		s.Prefix, s.KindScope = bindKinds(x.Declared, keysByKind)
	}
	// The fold's definition set is the install's alone: folded against a workspace's own it addresses cells the install never wrote, which reported 183 correct EU5 keys as unknown.
	foldDefs := scriptDefs(x.GameID, kindsByKey)
	if len(bases) > 0 && bases[0] != nil {
		base, _ := bases[0].citable()
		foldDefs = base
	}
	isDef := func(name string) bool { return len(foldDefs[name]) > 0 }
	_ = walkN(ctx, len(files), func(i int) error { files[i].fold(isDef, false); return nil })
	var cites []typedCite
	for _, r := range files {
		cites = append(cites, r.cites...)
	}
	// The first fold and the skeleton it builds are only what the join reads: which cells hold names, and which keys sit under them. A workspace joins too -- a mod adds members of a nested database exactly as vanilla does -- but it learns no cell types of its own.
	shapes := &Schema{Cells: map[string]map[string]map[string]*CellType{}}
	for _, r := range files {
		for i := range r.occs {
			o := &r.occs[i]
			if o.flags&oBlock == 0 {
				continue
			}
			t := shapes.at(r.kind, r.cellPath(o.node, o.flags&oTop != 0), o.key)
			t.Names = t.Names || o.flags&oNames != 0
		}
	}
	// The install's own table, where its join marks live; empty for an install scan, which is making them.
	base := &Schema{}
	if len(bases) > 0 && bases[0] != nil {
		base = &bases[0].Schema
	}
	cited := citedKinds(cites, s.Prefix, x.Declared)
	// The join asks whether a SCRIPT definition explains the name: CK3 lists c_varanasi in history/titles and the nested title is still what a citation means.
	joinNested(shapes, cited, isDef)
	shapes.shapes()
	// A citation is read through the shapes: a cited block key is a row only where this kind of file has been shown to hold that database.
	nested := map[string]bool{}
	for _, sh := range append(slices.Clone(shapes.Nested), s.Nested...) {
		nested[sh.ParentKind+"\x00"+sh.ChildKind] = true
	}
	for _, r := range files {
		x.addNested(shapes, base, cited, nested, r)
	}
	// Row 7: a wrapper key is not a definition, and a site under one belongs to no definition.
	wrappers := map[string]bool{}
	for _, r := range files {
		for _, w := range r.wraps {
			all := true
			for _, id := range w.ids {
				if len(kindsByKey[id]) == 0 {
					all = false
					break
				}
			}
			if all {
				wrappers[w.key] = true
			}
		}
	}
	if len(wrappers) > 0 {
		x.retract(func(d Def) bool { return wrappers[d.Key] })
	}
	// The second fold has to see the definitions the join just made, or a path through one stays spelled as a name.
	kindsByKey, keysByKind = citableOver(x, bases)
	if install && !x.Declared.Empty() {
		s.Prefix, s.KindScope = bindKinds(x.Declared, keysByKind)
	}
	if s.MacroDefs == nil {
		s.MacroDefs = map[string]string{}
	}
	for k, v := range x.macroDefs() {
		s.MacroDefs[k] = v
	}
	t := &lookup{gameID: x.GameID, kindsByKey: kindsByKey,
		locKeys: locKeys(x, bases, locLang), prefix: s.Prefix, schema: x.Declared}
	for _, b := range bases {
		if b != nil {
			t.cells = append(t.cells, &b.Schema)
		}
	}
	t.cells = append(t.cells, s)
	// The second fold and the one classification: a key that became a nested definition folds to <kind> here, so paths and cells both settle.
	fold2 := isDef
	if install {
		after := scriptDefs(x.GameID, kindsByKey)
		fold2 = func(name string) bool { return len(after[name]) > 0 }
	}
	_ = walkN(ctx, len(files), func(i int) error { files[i].fold(fold2, true); return nil })
	db := databasePositions(ctx, files)
	// One shard per kind, since a file has exactly one: the writers are disjoint, the merge takes no lock, and each table folds into its shard as the file finishes.
	byKind := map[string][]*fileRecs{}
	for _, r := range files {
		byKind[r.kind] = append(byKind[r.kind], r)
	}
	if s.Cells == nil {
		s.Cells = map[string]map[string]map[string]*CellType{}
	}
	kinds := make([]string, 0, len(byKind))
	for kind := range byKind {
		kinds = append(kinds, kind)
		if s.Cells[kind] == nil {
			s.Cells[kind] = map[string]map[string]*CellType{}
		}
	}
	// A workspace records which cells each file wrote, so Drop retracts them with the file's other records; an install never drops one.
	var mu sync.Mutex
	if !install {
		x.CellsByFile = map[int32][]Cell{}
	}
	slices.Sort(kinds)
	_ = walkN(ctx, len(kinds), func(k int) error {
		shard := s.Cells[kinds[k]]
		var labels []string
		for _, r := range byKind[kinds[k]] {
			r.keyAt(db)
			into := make(map[pathKey]*CellType, len(r.occs)/8+len(r.tallies)+1)
			at := func(path, key string) *CellType {
				c := pathKey{path, key}
				cell := into[c]
				if cell == nil {
					cell = &CellType{}
					into[c] = cell
				}
				return cell
			}
			for j := range r.occs {
				o := &r.occs[j]
				cell := at(r.cellPath(o.node, o.flags&oTop != 0), r.keys[j])
				if o.flags&oBlock != 0 {
					cell.Names = cell.Names || o.flags&oNames != 0
					cell.add([]string{blockLabel(o.flags)}, 1)
					continue
				}
				labels = t.classify(o.value, o.flags&oVQuoted != 0, r.data, labels)
				cell.add(labels, 1)
			}
			for j := range r.tallies {
				ta := &r.tallies[j]
				at(r.cellPath(ta.node, ta.top), r.tkeys[j]).add([]string{ta.label}, ta.n)
			}
			if !install {
				own := make([]Cell, 0, len(into))
				for c := range into {
					own = append(own, Cell{Kind: kinds[k], Path: c.path, Key: c.key})
				}
				mu.Lock()
				x.CellsByFile[x.PathID(r.Path)] = own
				mu.Unlock()
			}
			for c, cell := range into {
				byKey := shard[c.path]
				if byKey == nil {
					byKey = map[string]*CellType{}
					shard[c.path] = byKey
				}
				if was := byKey[c.key]; was != nil {
					was.merge(cell)
				} else {
					byKey[c.key] = cell
				}
			}
		}
		for path, byKey := range shard {
			if len(byKey) == 0 {
				delete(shard, path)
				continue
			}
			for _, cell := range byKey {
				cell.settle()
			}
		}
		return nil
	})
	// A kind whose files wrote no cell got a shard when the work was handed out and must not keep one.
	for kind, byPath := range s.Cells {
		if len(byPath) == 0 {
			delete(s.Cells, kind)
		}
	}
	// The marks move onto the settled schema, whose keys the fold re-spelled, so a workspace applies marks that address the cells it reads.
	for kind, byPath := range shapes.Cells {
		for path, byKey := range byPath {
			for key, t := range byKey {
				if t.Def != "" {
					s.at(kind, path, foldKey(x.GameID, key, true, false, isDef)).Def = t.Def
				}
			}
		}
	}
	// Only an install rebuilds the list: a workspace was handed the install's and its own cells are the positions the install never wrote.
	if install {
		s.shapes()
	}
	out := make([]*FileHarvest, len(files))
	_ = walkN(ctx, len(files), func(i int) error {
		out[i] = resolve(&x.Schema, files[i], t, wrappers)
		return nil
	})
	for _, h := range out {
		if h != nil {
			x.Add(h.Path, "", h.Origin, "", h)
		}
	}
}

// scriptDefs is the definition set a path folds against: the script tier's, because an editor that has not loaded the data tier would fold every path differently.
func scriptDefs(gameID string, kindsByKey map[string][]string) map[string][]string {
	out := make(map[string][]string, len(kindsByKey))
	for key, kinds := range kindsByKey {
		for _, k := range kinds {
			if !game.IsDataKind(gameID, k) {
				out[key] = kinds
				break
			}
		}
	}
	return out
}

// databasePositions are the (kind, path) positions holding a definition key, where every block key is a row: CK3's 22,333 uncited titles are titles, not 22,333 fields of landed_titles.
func databasePositions(ctx context.Context, files []*fileRecs) map[string]bool {
	local := make([]map[string]bool, len(files))
	_ = walkN(ctx, len(files), func(i int) error {
		r := files[i]
		out := map[string]bool{}
		for j := range r.occs {
			o := &r.occs[j]
			if o.flags&oBlock != 0 && r.keys[j] == segKind {
				out[r.kind+"\x00"+r.cellPath(o.node, o.flags&oTop != 0)] = true
			}
		}
		local[i] = out
		return nil
	})
	db := map[string]bool{}
	for _, out := range local {
		for k := range out {
			db[k] = true
		}
	}
	return db
}

// pathKey addresses a cell inside one kind's shard, which is all a file needs: it has exactly one kind.
type pathKey struct{ path, key string }

// citableOver groups the definitions a fold and a classification resolve against: this index's, and the install's underneath a workspace.
func citableOver(x *Index, bases []*Index) (kindsByKey map[string][]string, keysByKind map[string]map[string]bool) {
	kindsByKey, keysByKind = x.citable()
	for _, base := range bases {
		if base == nil {
			continue
		}
		bk, bb := base.citable()
		for key, kinds := range bk {
			for _, k := range kinds {
				if !slices.Contains(kindsByKey[key], k) {
					kindsByKey[key] = append(kindsByKey[key], k)
				}
			}
		}
		for kind, keys := range bb {
			set := keysByKind[kind]
			if set == nil {
				set = map[string]bool{}
				keysByKind[kind] = set
			}
			for k := range keys {
				set[k] = true
			}
		}
	}
	return kindsByKey, keysByKind
}

// defaultLocLang is the language the harvest read refs for; english is the fallback every game ships.
func (x *Index) defaultLocLang() string {
	for lang := range x.Loc {
		if lang != "english" {
			return lang
		}
	}
	return "english"
}

// locKeys is the localization a value is classified against: the install's for a scan, the install plus the workspace's own for a mod build.
func locKeys(x *Index, bases []*Index, locLang string) map[string]bool {
	lang := locLang
	if lang == "" {
		lang = x.defaultLocLang()
		for _, base := range bases {
			if base != nil && len(base.Loc) > 0 {
				lang = base.defaultLocLang()
			}
		}
	}
	out := keySet(x.Loc[lang])
	for _, base := range bases {
		if base == nil {
			continue
		}
		for k := range base.Loc[lang] {
			out[k] = true
		}
	}
	return out
}

// at is the cell at (kind, path, key), created empty when the corpus has not reached it yet.
func (s *Schema) at(kind, path, key string) *CellType {
	if s.Cells == nil {
		s.Cells = map[string]map[string]map[string]*CellType{}
	}
	byPath := s.Cells[kind]
	if byPath == nil {
		byPath = map[string]map[string]*CellType{}
		s.Cells[kind] = byPath
	}
	byKey := byPath[path]
	if byKey == nil {
		byKey = map[string]*CellType{}
		byPath[path] = byKey
	}
	t := byKey[key]
	if t == nil {
		t = &CellType{}
		byKey[key] = t
	}
	return t
}

func blockLabel(flags uint8) string {
	switch {
	case flags&oNames != 0:
		return LabelBlock
	case flags&oEmpty != 0:
		return LabelEmpty
	default:
		return LabelList
	}
}

// addNested emits the definitions the join found: every block key at a cell the join marked is a definition of that cell's kind.
func (x *Index) addNested(skeleton, base *Schema, cited map[string]string, nested map[string]bool, r *fileRecs) {
	var defs []Def
	for i := range r.occs {
		o := &r.occs[i]
		if o.flags&(oBlock|oNames|oQuoted|oTop) != oBlock|oNames {
			continue
		}
		path := r.pathOf(o.node)
		name := game.KeyIdentity(r.GameID, o.key)
		if !jomini.IsDefName(name) || jomini.IsStop(name) {
			continue
		}
		// A cited key is a row wherever it is written, which reaches c_varanasi four blocks inside an empire; below a marked cell every block key is one, which turns CK3's 116 cited faiths into 140.
		kind := cited[name]
		if kind != "" && !nested[r.kind+"\x00"+kind] {
			kind = ""
		}
		if kind == "" && !inDefinitionBody(path) {
			if parent, ok := parentCell(skeleton, r.kind, path); ok {
				kind = parent.Def
			}
			if kind == "" {
				if parent, ok := parentCell(base, r.kind, path); ok {
					kind = parent.Def
				}
			}
		}
		if kind == "" {
			continue
		}
		d := r.def(kind, name, o.kline, o.kat, o.kat+int32(len(o.key)))
		d.Nested = true
		defs = append(defs, d)
	}
	if len(defs) > 0 {
		x.Add(r.Path, "", r.Origin, "", &FileHarvest{fileID: r.fileID, Defs: defs})
	}
}

// resolve turns one file's occurrences into the references and edges the cells say they are. A value becomes a reference when it resolves to a member of its cell's type; a reference of kind event, on_action or a macro kind inside a definition is also an edge.
func resolve(s *Schema, r *fileRecs, t *lookup, wrappers map[string]bool) *FileHarvest {
	h := &FileHarvest{fileID: r.fileID}
	var labels []string
	for i := range r.occs {
		o := &r.occs[i]
		cell := t.cell(Cell{Kind: r.kind,
			Path: r.cellPath(o.node, o.flags&oTop != 0), Key: r.keys[i]})
		from := r.defKeyOf(o.from, wrappers)
		if o.flags&oQuoted == 0 && o.key != "" {
			callRecord(s, h, r, o, from)
		}
		if o.flags&oBlock != 0 || o.value == "" {
			continue
		}
		labels = t.classify(o.value, o.flags&oVQuoted != 0, r.data, labels)
		kind := refKind(cell, labels, o.key, o.value)
		if kind == "" {
			continue
		}
		ref := r.ref(kind, o.value, o.vline, o.vat, o.vat+int32(len(o.value)))
		ref.Derived = o.flags&oDeclared == 0 && !isLocKind(kind)
		h.Refs = append(h.Refs, ref)
		if !isFireKind(kind) {
			continue
		}
		h.Edges = append(h.Edges, Edge{
			From: from, To: o.value, Via: r.viaOf(o), Path: r.Path,
			Line: int(o.vline), Kind: slotNames[o.slot],
			NameKey: r.nameOf(o.name), Rescoped: o.flags&oRescoped != 0,
		})
	}
	if len(h.Refs) == 0 && len(h.Edges) == 0 {
		return nil
	}
	return h
}

// callRecord is a keyed statement inside a definition whose key names a scripted_* macro: an invocation of it.
func callRecord(s *Schema, h *FileHarvest, r *fileRecs, o *occ, from string) {
	if from == "" || from == o.key {
		return
	}
	kind := s.MacroDefs[o.key]
	if kind == "" {
		return
	}
	h.Refs = append(h.Refs, r.ref(kind, o.key, o.kline, o.kat, o.kat+int32(len(o.key))))
	h.Edges = append(h.Edges, Edge{
		From: from, To: o.key, Path: r.Path, Line: int(o.kline), Kind: EdgeKindCall})
}

// refKind is the spec's §5 first clause: a value resolves to a member of its cell type, or to nothing. The cell's intersection decides which database, so a cell the corpus disagrees about types nothing.
func refKind(cell *CellType, labels []string, key, value string) string {
	// Declared beats derived: the engine's own localization properties hold a key wherever they are written, and a property whose value is not key-shaped holds nothing rather than falling through to the typed branch.
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
	if cell == nil {
		return ""
	}
	if isMacroArgKey(key) {
		// An all-caps argument key holds a fragment the macro pastes into a name, not an object of the field's type.
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
	// Not in the spec: at a cell that names a database and never held a literal, a name-shaped value is a reference whether or not anything defines it -- the dangling report is made of these.
	if !cell.Has(LabelLiteral) && jomini.IsDefName(value) {
		for _, k := range cell.Kinds {
			if k != LabelLoc {
				return k
			}
		}
	}
	return ""
}

func isLocKind(kind string) bool { return kind == "loc" || kind == "loc-broad" }

func isFireKind(kind string) bool {
	return kind == jomini.KindEvent || kind == "on_action" || jomini.IsMacroKind(kind)
}

// viaOf names the edge: the statement's own key, or the block above it when the key is an inline weight or id that names no site of its own (`random_events = { 100 = ns.1 }`).
func (r *fileRecs) viaOf(o *occ) string {
	if k := strings.ToLower(o.key); k == "id" || isDigitKey(k) {
		if p := r.pathOf(o.node); p != "" {
			if i := strings.LastIndexByte(p, '.'); i >= 0 {
				return p[i+1:]
			}
			return p
		}
	}
	return o.key
}

// defKeyOf is the definition a site belongs to; a wrapper key is not a definition, so a site under one belongs to nothing.
func (r *fileRecs) defKeyOf(i int32, wrappers map[string]bool) string {
	if i < 0 || int(i) >= len(r.defKeys) {
		return ""
	}
	if k := r.defKeys[i]; !wrappers[k] {
		return k
	}
	return ""
}

func (r *fileRecs) nameOf(i int32) string {
	if i < 0 || int(i) >= len(r.names) {
		return ""
	}
	return r.names[i]
}

// ---------------------------------------------------------- the localization mode

// locValueLimit caps the stored localization value; hover shows a line, not a paragraph.
const locValueLimit = 200

// ApplyLoc is the localization half of the rows: one entry is one value, a `$key$` interpolation is a reference, and a data-function name reads a key the loc files need not define.
func ApplyLoc(id fileID, text string) *FileHarvest {
	h := &FileHarvest{fileID: id}
	r := loc.Parse(text)
	vals := map[string]LocEntry{}
	for _, e := range r.Entries {
		v := e.Value
		if len(v) > locValueLimit {
			v = v[:locValueLimit]
		}
		vals[e.Key] = LocEntry{Value: v, Path: id.Path, Line: e.Line,
			Start: e.KeyRange.Start, Origin: id.Origin}
		for _, ip := range loc.Interps(e.Value, e.ValueRange.Start) {
			if loc.IsLocEngineValue(ip.Key, ip.Filter) {
				continue
			}
			h.Refs = append(h.Refs, id.ref("loc", ip.Key,
				int32(e.Line), int32(ip.KeyRange.Start), int32(ip.KeyRange.End)))
		}
		// Broad: a concept name reads a key, but the engine also takes names the loc files never define.
		for _, ip := range loc.DataFnKeys(e.Value, e.ValueRange.Start) {
			h.Refs = append(h.Refs, id.ref("loc-broad", ip.Key,
				int32(e.Line), int32(ip.KeyRange.Start), int32(ip.KeyRange.End)))
		}
	}
	h.Loc = LocDelta{Lang: loc.LanguageOf(id.Path, r.Language), Vals: vals}
	return h
}

// ---------------------------------------------------------- leading documentation

// maxDefDocLines caps one definition's harvested comment: enough for a description without pulling in a whole changelog someone parked above a macro.
const maxDefDocLines = 4

// docWorthyKind reports a definition whose declaration site is a line an author can comment. For a great many objects this is the only description that will ever exist: 4 of CK3's 11,708 scripted macros appear in script_docs, and none of its events, decisions or traits do.
func docWorthyKind(kind string) bool {
	switch jomini.CanonicalKind(kind) {
	case "loc_key", "loc_value", "mod_descriptor", "":
		return false
	}
	return !jomini.IsEphemeral(kind)
}

// leadingComment reads the comment block written directly above a definition; a blank line ends it, without which a section banner reads as documentation of whatever definition follows.
func leadingComment(src string, off int) string {
	if off > len(src) {
		return ""
	}
	line := strings.LastIndexByte(src[:off], '\n') + 1
	var rev []string
	for line > 0 && len(rev) < maxDefDocLines {
		prev := strings.LastIndexByte(src[:line-1], '\n') + 1
		body, hashed := jomini.PeelComment(src[prev : line-1])
		if !hashed {
			break // a blank line or real script ends the block
		}
		if body == "" || strings.Trim(body, "=-*_ ") == "" {
			break // a separator rule, not prose
		}
		rev = append(rev, body)
		line = prev
	}
	if len(rev) == 0 {
		return ""
	}
	slices.Reverse(rev)
	return jomini.AuthorProse(rev)
}

// --------------------------------------------------------------- small shapes

// isMacroArgKey reports the all-caps convention Paradox uses for the arguments of a scripted macro: `some_effect = { ROLE = chemist }`.
func isMacroArgKey(key string) bool {
	upper := false
	for i := range len(key) {
		c := key[i]
		switch {
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
		nameStart := j
		for j < len(text) && jomini.IsScriptParamByte(text[j], j == nameStart) {
			j++
		}
		if j == nameStart || j >= len(text) || text[j] != '$' {
			i++
			continue
		}
		out = append(out, dollarSpan{
			name:  text[nameStart:j],
			start: base + int32(i),
			end:   base + int32(j) + 1,
		})
		i = j + 1
	}
	return out
}
