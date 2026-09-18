// probe.go is the one descent every request takes: probeAt reads a script
// position once (the word, the assignment and slot the cursor sits at) and
// walks the learned tree down to it, carrying the position, the scope type in
// force, the body class and the macro parameters. Diagnostics take the same
// walk over a whole file. identify then names what the cursor is on, in one
// fixed order, so a new source cannot quietly outrank a declared one.

package lsp

import (
	"regexp"
	"slices"
	"strconv"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/declared"
	"paradox-modding-tools/services/internal/lang/index"
	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/loc"
	"paradox-modding-tools/services/internal/lang/session"
)

// probe is the one read of a script position every request shares.
type probe struct {
	src, word       string
	off, start, end int
	res             jomini.Result
	param           bool // word/start/end are a `$PARAM$` span rather than a token
	kind            string
	assign          *jomini.Assignment
	saveName        string
	saveOK          bool
	inKey           bool
	slotKey         string
	fileKind        string
	inNested        bool     // the enclosing block is deeper than the definition
	blockPath       []string // the keys of the enclosing assignments, outermost first
	slotAssign      *jomini.Assignment
	scope           string // the scope type in force, "" when undetermined
	chain           []jomini.Statement
	paramOwner      string
	// segs are the enclosing block keys below the definition; hang is the learned position they address, and cell the node of slotKey below it.
	segs       []string
	hang, cell *index.Node
}

// walk is one descent's state below a definition: the learned position, the scope stack (so `prev` and `root` resolve), the body class and the parameters of the token whose block this is. Every scope transition is DECLARED by the game; nothing is inferred from name shape. dead marks a transition nothing explains: the position keeps descending, the scope is unknown from there on. rooted records that the bottom of the stack is the real root scope, which only an on_action declares.
type walk struct {
	s      *session.Session
	sc     *declared.Schema
	hang   *index.Node
	stack  []string
	rooted bool
	body   uint8
	params map[string]bool
	dead   bool
}

func (w walk) cur() string {
	if len(w.stack) == 0 {
		return ""
	}
	return w.stack[len(w.stack)-1]
}

// rootWalk seeds a walk at one top-level definition. Only on_actions declare their scope; an event's is read from the on_actions that fire it, since its `type` names a presentation window, not a scope.
func rootWalk(s *session.Session, kind string, def jomini.Statement) walk {
	w := walk{s: s, sc: s.Install.Declared, hang: s.Install.Tree.Root(kind, false)}
	w.body = bodyOf(w.hang)
	a, ok := def.(*jomini.Assignment)
	if !ok || a.Key.Quoted {
		w.dead = true
		return w
	}
	scope := ""
	switch kind {
	case "on_action":
		scope = w.sc.OnActions[strings.ToLower(a.Key.Text)]
	case "event":
		scope = eventRootScope(s, w.sc, a.Key.Text)
	}
	if scope != "" && scope != "none" {
		w.stack, w.rooted = []string{scope}, true
	}
	return w
}

// eventRootScope is the scope an event runs in, read from the on_actions that fire it; several disagreeing is no answer.
func eventRootScope(s *session.Session, sc *declared.Schema, event string) string {
	found := ""
	for _, e := range s.Edges(event, false) {
		if e.Rescoped {
			continue
		}
		scope := sc.OnActions[strings.ToLower(e.From)]
		if scope == "" || scope == "none" {
			continue
		}
		if found != "" && found != scope {
			return ""
		}
		found = scope
	}
	return found
}

// bodyOf is the declared token classes a block holds, 0 where it is not a body.
func bodyOf(n *index.Node) uint8 {
	if n != nil && n.Class == index.Body {
		return n.Body
	}
	return 0
}

// into is the walk one block key deeper. The position steps whatever happens; the body class is inherited until a block opens its own; the scope follows the key, which may be a dotted chain hopping once per segment, and any segment nothing explains kills the scope rather than reporting a wrong one. A date is not a chain.
func (w walk) into(key string, quoted bool) walk {
	name, _, next := w.s.Install.Tree.Step(w.hang, key, quoted)
	n := walk{s: w.s, sc: w.sc, hang: next, stack: slices.Clone(w.stack), rooted: w.rooted, body: w.body, dead: w.dead}
	if b := bodyOf(next); b != 0 {
		n.body = b
	}
	n.params = w.sc.TokenParams(strings.ToLower(game.KeyIdentity("", key)))
	if n.dead || quoted {
		return n
	}
	segs := []string{key}
	if strings.Contains(key, ".") && !jomini.IsDateLiteral(key) {
		segs = strings.Split(key, ".")
	}
	for _, seg := range segs {
		if seg == "" || !n.hop(seg, name, next) {
			n.stack, n.dead = nil, true
			return n
		}
	}
	return n
}

// hop applies one segment of a block key to the scope stack.
func (w *walk) hop(key, name string, next *index.Node) bool {
	k := strings.ToLower(game.KeyIdentity("", key))
	switch k {
	case "this":
		return true
	case "root":
		if !w.rooted || len(w.stack) == 0 {
			return false
		}
		w.stack = append(w.stack, w.stack[0])
		return true
	case "prev":
		if len(w.stack) < 2 {
			return false
		}
		w.stack = append(w.stack, w.stack[len(w.stack)-2])
		return true
	}
	// A declared scope link is a hop to its output type, taken on the game's word when the current scope is unknown; only a link naming one output type, and only into a scope script runs in: CK3 declares `modifier` with output `value`, and every weight block opens with one.
	if l, ok := w.sc.Links[k]; ok && l.Out != "" && !strings.Contains(l.Out, ",") && w.sc.Scopes[l.Out].ChangeScopes {
		if len(l.In) == 0 || w.cur() == "" || slices.Contains(l.In, w.cur()) {
			w.stack = append(w.stack, l.Out)
			return true
		}
	}
	// An iterator's Supported Targets is the element scope.
	if t, _, ok := w.sc.Token(k); ok && t.Target != "" {
		if (w.cur() == "" || declared.UsableIn(t.In, w.cur())) && w.sc.Scopes[t.Target].ChangeScopes {
			w.stack = append(w.stack, t.Target)
			return true
		}
	}
	// An iterator the game does not declare still changes scope, to a type nothing here can name.
	if isIteratorKey(k) {
		return false
	}
	// A key the fold turns into a class is a rescope or a position, never a section of the current scope: `scope:activity = { … }` enters the activity it names.
	if name != key {
		return false
	}
	// So is a key that names an object: Victoria 3's `create_character = { … on_created = { … } }` runs on the character it just made.
	if w.s.Resolve(key) != nil {
		return false
	}
	// A body runs in the scope around it, so entering one changes nothing; the install's answer alone, since a block only the mod wrote would be classed from its own contents.
	return next != nil && next.Class == index.Body
}

// isIteratorKey reports the four list-iteration prefixes.
func isIteratorKey(k string) bool {
	return strings.HasPrefix(k, "any_") || strings.HasPrefix(k, "every_") ||
		strings.HasPrefix(k, "random_") || strings.HasPrefix(k, "ordered_")
}

func roleBit(role string) uint8 {
	if role == "effect" {
		return index.BodyEffect
	}
	return index.BodyTrigger
}

func probeAt(s *session.Session, path string, line, col int) (probe, bool) {
	src := s.FileText(path)
	if src == "" {
		return probe{}, false
	}
	res := s.Parsed(path)
	off := res.Lines().OffsetAt(line, col)
	if inComment(res, off) {
		return probe{}, false
	}
	word, start, end := res.TokenAt(off)
	name, pStart, pEnd, param := jomini.ScriptParamSpan(src, off)
	if param {
		word, start, end = name, pStart, pEnd
	}
	at := probe{src: src, off: off, res: res, word: word, start: start, end: end, param: param}
	rel := path
	if _, r, ok := s.Locate(path); ok {
		rel = r
	}
	at.kind = game.MatchExtract(s.GameID, rel).Kind
	at.fileKind = at.kind
	if res.Root == nil || s.KindFor(path) == "loc" {
		return at, true
	}
	chain := jomini.NodeAtOffset(res.Root, off)
	at.chain = chain
	if len(chain) == 0 {
		return at, true
	}
	if a, ok := chain[0].(*jomini.Assignment); ok && !a.Key.Quoted && !s.IsLocalDefFile(path, at.kind) {
		if d := s.Resolve(a.Key.Text); d != nil && d.Kind != "" && d.Kind != "loc_key" {
			at.kind = d.Kind
		}
	}
	fillCompleteSlot(&at, chain, off)
	if key := pendingAssignKey(at.src, off); key != "" {
		at.slotKey = key
		at.inKey = false
	}
	at.saveName, at.saveOK = saveScopeIn(chain, off)
	at.paramOwner = paramOwnerIn(s, chain)
	// The descent: every block below the definition the cursor is actually inside.
	w := rootWalk(s, at.fileKind, chain[0])
	prev := w.hang
	for _, st := range chain[1:] {
		a, ok := st.(*jomini.Assignment)
		if !ok {
			continue
		}
		if b := jomini.ChildBlock(a); b == nil || off < b.Range.Start || off > b.Range.End {
			continue
		}
		at.segs = append(at.segs, a.Key.Text)
		prev = w.hang
		w = w.into(a.Key.Text, a.Key.Quoted)
	}
	at.scope, at.hang = w.cur(), w.hang
	// A bare list member sits at the cell of the block key that holds it, so that key is not a segment.
	if n := len(at.segs); n > 0 && !at.inKey && at.segs[n-1] == at.slotKey {
		at.hang, at.segs = prev, at.segs[:n-1]
	}
	// The workspace's own tree where the install never wrote there.
	if at.hang == nil {
		at.hang = s.Work.Tree.At(at.fileKind, at.segs...)
	}
	if at.slotKey != "" {
		at.cell = childOf(s, at.hang, at.slotKey)
	}
	return at, true
}

func paramOwnerIn(s *session.Session, chain []jomini.Statement) string {
	for _, c := range slices.Backward(chain) {
		a, ok := c.(*jomini.Assignment)
		if !ok || a.Key.Quoted || strings.Contains(a.Key.Text, "$") {
			continue
		}
		key := a.Key.Text
		if jomini.IsMacroKind(jomini.CanonicalKind(key)) || s.Work.Tree.Macros[key] != "" {
			return key
		}
		if d := s.Resolve(key); d != nil && jomini.IsMacroKind(d.Kind) {
			return key
		}
	}
	return ""
}

func fillCompleteSlot(at *probe, chain []jomini.Statement, off int) {
	var assigns []*jomini.Assignment
	for _, st := range chain {
		if a, ok := st.(*jomini.Assignment); ok && !a.Key.Quoted {
			assigns = append(assigns, a)
		}
	}
	if len(assigns) == 0 {
		return
	}
	inner := assigns[len(assigns)-1]
	if off >= inner.Key.Range.Start && off < inner.Key.Range.End {
		at.assign = inner
		at.inKey = true
		if len(assigns) >= 2 {
			at.slotAssign = assigns[len(assigns)-2]
			at.slotKey = at.slotAssign.Key.Text
			at.inNested = len(assigns) > 2
			for _, x := range assigns[:len(assigns)-1] {
				at.blockPath = append(at.blockPath, x.Key.Text)
			}
		}
		return
	}
	innerSt := chain[len(chain)-1]
	if vs, ok := innerSt.(*jomini.ValueStmt); ok {
		at.slotKey = inner.Key.Text
		if sc, ok := vs.Value.(*jomini.Scalar); ok && !sc.Quoted {
			keyLine := at.res.Lines().PositionAt(inner.Key.Range.Start).Line
			if at.res.Lines().PositionAt(sc.Range.Start).Line != keyLine {
				at.inKey = true
			}
		}
		return
	}
	if sc, ok := inner.Value.(*jomini.Scalar); ok && off >= sc.Range.Start && off <= sc.Range.End {
		at.slotKey = inner.Key.Text
		return
	}
	if b := jomini.BlockOf(inner.Value); b != nil && off >= b.Range.Start && off <= b.Range.End {
		at.inKey = true
		at.slotKey = inner.Key.Text
		return
	}
	if off >= inner.Key.Range.End {
		at.slotKey = inner.Key.Text
		end := min(off, len(at.src))
		if from := inner.Key.Range.End; from < end && !strings.Contains(at.src[from:end], "=") {
			at.inKey = true
		}
	}
}

var pendingAssignRe = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_]*)\s*=\s*$`)

func pendingAssignKey(src string, off int) string {
	if off < 0 {
		return ""
	}
	off = min(off, len(src))
	lineStart := strings.LastIndex(src[:off], "\n") + 1
	if m := pendingAssignRe.FindStringSubmatch(src[lineStart:off]); m != nil {
		return m[1]
	}
	return ""
}

func saveScopeIn(chain []jomini.Statement, off int) (name string, ok bool) {
	for _, n := range chain {
		a, isA := n.(*jomini.Assignment)
		if !isA || a.Key.Quoted {
			continue
		}
		if jomini.IsSaveScopeKey(a.Key.Text) {
			if sc, isS := a.Value.(*jomini.Scalar); isS && !sc.Quoted && off >= sc.Range.Start && off <= sc.Range.End {
				return sc.Text, true
			}
			return "", false
		}
		if !jomini.IsSaveScopeValueKey(a.Key.Text) {
			continue
		}
		b := jomini.BlockOf(a.Value)
		if b == nil {
			continue
		}
		for _, st := range b.Statements {
			ca, isC := st.(*jomini.Assignment)
			if !isC || ca.Key.Text != "name" {
				continue
			}
			if sc, isS := ca.Value.(*jomini.Scalar); isS && !sc.Quoted && off >= sc.Range.Start && off <= sc.Range.End {
				return sc.Text, true
			}
		}
	}
	return "", false
}

func inComment(res jomini.Result, off int) bool {
	for _, c := range res.Comments {
		if off >= c.Range.Start && off < c.Range.End {
			return true
		}
	}
	return false
}

type locHit struct {
	key, filter string
	start, end  int
	engine      bool
}

// locHitAt resolves the cursor with a single loc parse: the key, an interpolated key inside a value, or an engine value.
func locHitAt(src string, off int) (locHit, bool) {
	res := loc.Parse(src)
	for _, e := range res.Entries {
		if off >= e.KeyRange.Start && off <= e.KeyRange.End {
			return locHit{key: e.Key, start: e.KeyRange.Start, end: e.KeyRange.End}, true
		}
		for _, ip := range loc.Interps(e.Value, e.ValueRange.Start) {
			if off >= ip.WrapRange.Start && off < ip.WrapRange.End {
				return locHit{key: ip.Key, filter: ip.Filter, start: ip.KeyRange.Start, end: ip.KeyRange.End,
					engine: loc.IsLocEngineValue(ip.Key, ip.Filter)}, true
			}
		}
	}
	if len(res.Entries) > 0 {
		return locHit{}, false
	}
	// No parsable entry yet: mid-typing, or a file with only a header.
	word, wStart, wEnd := jomini.Result{Src: src}.TokenAt(off)
	if word == "" {
		return locHit{}, false
	}
	return locHit{key: word, start: wStart, end: wEnd}, true
}

// macroCallAt reads a `kind name = { … }` declaration at the cursor: the bare scalar before the assignment in the probe's own statement list is the kind.
func macroCallAt(at probe) (kind, name string, assign *jomini.Assignment, ok bool) {
	if len(at.chain) == 0 || at.res.Root == nil {
		return "", "", nil, false
	}
	stmts := at.res.Root.Statements
	if n := len(at.chain); n > 1 {
		b := jomini.ChildBlock(at.chain[n-2])
		if b == nil {
			return "", "", nil, false
		}
		stmts = b.Statements
	}
	inner := at.chain[len(at.chain)-1]
	for i, st := range stmts {
		if st != inner {
			continue
		}
		if vs, isV := st.(*jomini.ValueStmt); isV {
			sc, isS := vs.Value.(*jomini.Scalar)
			if !isS || sc.Quoted || jomini.IsStop(sc.Text) || i+1 >= len(stmts) || at.off < sc.Range.Start || at.off >= sc.Range.End {
				break
			}
			if a, isA := stmts[i+1].(*jomini.Assignment); isA && !a.Key.Quoted {
				return sc.Text, a.Key.Text, a, true
			}
			break
		}
		a, isA := st.(*jomini.Assignment)
		if !isA || a.Key.Quoted || i == 0 || at.off < a.Key.Range.Start || at.off >= a.Key.Range.End {
			break
		}
		if vs, isV := stmts[i-1].(*jomini.ValueStmt); isV {
			if sc, isS := vs.Value.(*jomini.Scalar); isS && !sc.Quoted && !jomini.IsStop(sc.Text) {
				return sc.Text, a.Key.Text, a, true
			}
		}
		break
	}
	return "", "", nil, false
}

// scopeRefAt resolves `scope:name` at the cursor.
func scopeRefAt(at probe) (name string, ok bool) {
	src, word, wStart, wEnd := at.src, at.word, at.start, at.end
	if word == "" {
		return "", false
	}
	if wEnd < len(src) && src[wEnd] == ':' && word == jomini.ScopePrefix {
		n, _, _ := at.res.TokenAt(wEnd + 1)
		return n, n != ""
	}
	if wStart > 0 && src[wStart-1] == ':' {
		if pre, _, _ := at.res.TokenAt(wStart - 2); pre == jomini.ScopePrefix {
			return word, true
		}
	}
	if p, pok := jomini.ParsePrefixed(word); pok && jomini.IsSavedScopePrefix(p) {
		return p.Name, true
	}
	return "", false
}

// ------------------------------------------------------------- the identity

// identClass is what a card is about; identify sets it once, and the card and the navigation read it.
type identClass uint8

const (
	idNone      identClass = iota
	idLocValue             // `$GOLD|=+$`, an engine value inside a loc string
	idParam                // `$PARAM$` of a scripted macro
	idProse                // an era date or a weight: prose the probe worked out itself
	idEphemeral            // a var, flag, saved scope or global var
	idLocalDef             // a key in a file whose own rows are the definitions
	idField                // a key of the object being edited
	idLocKey               // a localization key
	idDef                  // a definition somewhere in the model
	idToken                // an engine token, link or modifier the game declares
	idSlotValue            // a value nothing names, described by the key that holds it
)

type identity struct {
	class                                           identClass
	path, name, kind, owner, locFilter, extractKind string
	// hang is the learned position the cursor sits at, which files a key or a value under it.
	hang *index.Node
	// block is the key of the enclosing block, when the cursor sits inside one: what lets a card tell `trigger` in a triggered_desc from `trigger` in a portrait, which the install documents once, by name, for both.
	block string
	// slot is the key holding the value under the cursor, for the floor card.
	slot string
	// docs is prose the identify step worked out itself.
	docs                               string
	def                                *index.Def
	fieldKey, local                    bool
	spanStart, spanEnd                 int
	assignStart, assignEnd, assignLine int
}

// identify names what the cursor is on, first answer wins. An answer with no name is a stop: something is under the cursor and nothing is claimed about it, so no card is built.
func identify(s *session.Session, path string, at probe) (identity, bool) {
	id, ok := identifyAt(s, path, at)
	if !ok || id.name == "" {
		return identity{}, false
	}
	if id.path == "" {
		id.path = path
	}
	id.class = classOf(id)
	return id, true
}

// classOf is the one order the card reads an identity in.
func classOf(id identity) identClass {
	switch {
	case id.kind == "loc_value":
		return idLocValue
	case id.kind == jomini.KindScriptParam:
		return idParam
	case id.kind == "date" || id.kind == "weight":
		return idProse
	case jomini.IsEphemeral(id.kind):
		return idEphemeral
	case id.local:
		return idLocalDef
	case id.fieldKey:
		return idField
	case id.def != nil && id.def.Kind == "loc_key":
		return idLocKey
	case id.def != nil:
		return idDef
	case id.kind != "":
		return idToken
	case id.slot != "":
		return idSlotValue
	}
	return idNone
}

func identifyAt(s *session.Session, path string, at probe) (identity, bool) {
	// A localization file: the key, an interpolated key inside a value, or an engine value like `$GOLD|=+$`.
	if s.KindFor(path) == "loc" {
		hit, ok := locHitAt(at.src, at.off)
		if !ok {
			return identity{path: path}, true
		}
		if hit.engine {
			return identity{path: path, name: hit.key, kind: "loc_value", locFilter: hit.filter}, true
		}
		id := identity{path: path, name: hit.key, kind: "loc_key", spanStart: hit.start, spanEnd: hit.end}
		if file, ln, _, origin, ok := s.LocSite(hit.key); ok {
			id.def = &index.Def{Kind: "loc_key", Key: hit.key, Path: file, Line: ln, Origin: origin}
		}
		return id, true
	}
	// `scope:name` written out in the text, and the name at a `save_scope_as = x` site.
	if name, ok := scopeRefAt(at); ok {
		return ephemeralID(s, path, name, "saved_scope"), true
	}
	if at.saveOK {
		return ephemeralID(s, path, at.saveName, "saved_scope"), true
	}
	// `$PARAM$`: a macro's own parameter has no definition outside its body, so one is synthesised at the span when the index has none.
	if at.param {
		d := s.ResolveOfKind(at.word, "script_param", at.paramOwner)
		if d == nil {
			origin, _, _ := s.Locate(path)
			d = &index.Def{Kind: "script_param", Key: at.word, Path: path, Origin: origin, Start: at.start, End: at.end, OwnerKey: at.paramOwner,
				Line: at.res.Lines().PositionAt(at.start).Line}
		}
		return identity{path: path, name: at.word, kind: "script_param", def: d, owner: at.paramOwner}, true
	}
	// A bare word that is a parameter of the macro the cursor sits inside.
	if at.paramOwner != "" {
		if d := s.ResolveOfKind(at.word, "script_param", at.paramOwner); d != nil {
			return identity{path: path, name: at.word, kind: "script_param", def: d, owner: at.paramOwner}, true
		}
	}
	// `kind name = { … }`: the name of a declaration, with the file's own body as its site.
	if kind, name, a, ok := macroCallAt(at); ok && name != "" {
		d := s.ResolveOfKind(name, kind, "")
		if d != nil {
			ck := jomini.CanonicalKind(kind)
			for _, cand := range s.ModDefs(name) {
				if jomini.CanonicalKind(cand.Kind) == ck && index.SamePath(cand.Path, path) {
					c := cand
					d = &c
					break
				}
			}
		}
		if d == nil && a != nil {
			origin, _, _ := s.Locate(path)
			d = &index.Def{Kind: kind, Key: name, Path: path, Origin: origin, Start: a.Key.Range.Start, End: a.Key.Range.End,
				Line: at.res.Lines().PositionAt(a.Key.Range.Start).Line}
		}
		return identity{path: path, name: name, kind: kind, def: d}, true
	}
	if id, ok := keyIdentity(s, path, at); ok {
		return id, true
	}
	if at.word == "" {
		return identity{path: path}, true
	}
	return valueIdentity(s, path, at)
}

// keyIdentity names the key of the assignment the cursor is on.
func keyIdentity(s *session.Session, path string, at probe) (identity, bool) {
	a := at.assign
	if a == nil {
		return identity{}, false
	}
	// `scope:x = { … }` written as a key.
	if p, ok := jomini.ParsePrefixed(a.Key.Text); ok && jomini.IsSavedScopePrefix(p) {
		return ephemeralID(s, path, p.Name, "saved_scope"), true
	}
	// A key in a file whose own rows are the definitions.
	if s.IsLocalDefFile(path, at.kind) {
		return identity{path: path, name: a.Key.Text, kind: at.kind, local: true, extractKind: at.kind,
			assignStart: a.Key.Range.Start, assignEnd: a.Key.Range.End, assignLine: at.res.Lines().PositionAt(a.Key.Range.Start).Line}, true
	}
	// A bare number on the left of an `=` is a branch of a distribution, not a name: CK3 keys map positions by province id, so `95` also resolves to a map_data row and the card offered "Amiens" for a probability. Numeric but not a distribution (Vic3's `20 = empty` is an index) stops rather than answering.
	if isWeightKey(a.Key.Text) {
		if share := weightShare(at.slotAssign, at.slotKey, a.Key.Text); share != "" {
			return identity{path: path, name: a.Key.Text, kind: "weight", docs: share}, true
		}
		return identity{path: path}, true
	}
	// `holder.culture = { … }` is two hops, and the whole string resolves to nothing; the segment under the cursor is the subject.
	if seg, ok := dottedSegmentAt(a.Key.Text, at.off-a.Key.Range.Start); ok {
		return segmentIdentity(s, path, seg)
	}
	// A key the type system names keeps its own card rather than becoming a field of the block.
	if s.Install.Declared.HasName(a.Key.Text) {
		if ck := vocabRole(s, a.Key.Text); ck != "" {
			return identity{path: path, name: a.Key.Text, kind: ck}, true
		}
	}
	// A key of the object being edited: documented, a member of the cell, or a localization property. A key naming a scripted macro is an invocation of it, never a field, whatever the cell at that position holds.
	if s.Work.Tree.Macros[a.Key.Text] != "" {
		return identity{}, false
	}
	block := nestedBlock(at)
	if kindFieldDoc(s, a.Key.Text, at.kind, block) == "" && !isMember(s, at.hang, a.Key.Text) && loc.Classify(a.Key.Text) == loc.PropNone {
		return identity{}, false
	}
	return identity{path: path, name: a.Key.Text, kind: at.kind, fieldKey: true, extractKind: at.kind, block: block, hang: at.hang}, true
}

// valueIdentity names a word that is not a key: what the game states it is (a literal, a language prefix, a citation, the slot's declared or learned type), then what the model defines, then the floor.
func valueIdentity(s *session.Session, path string, at probe) (identity, bool) {
	// A literal is not a name, with one exception where a person declared it: a total conversion offsets its years so the engine's arithmetic works, and 8209.2.1 is read by a human who would otherwise subtract 7,782 in their head.
	if jomini.IsLiteralValue(at.word) {
		if era := index.EraDate(at.word, s.CalendarFor(path)); era != "" {
			return identity{path: path, name: at.word, kind: "date", docs: era}, true
		}
		return identity{path: path}, true
	}
	// `scope:x` / `var:x`: a value that lives only for the duration of a script run, never a row in a database.
	if p, ok := jomini.ParsePrefixed(at.word); ok {
		kind := jomini.PrefixKind(p.Prefix)
		if kind == "" {
			kind = p.Prefix
		}
		return ephemeralID(s, path, p.Name, kind), true
	}
	// `culture:english`: the game naming its own database in the text, which is as declared as a reference gets.
	if sp, ok := jomini.TypedSpanAt(at.word, at.off-at.start); ok {
		kind, id, ok := game.ParseTyped(s.GameID, sp.Prefix+":"+sp.ID)
		if !ok {
			kind, id = sp.Prefix, sp.ID
		}
		if pk := s.Install.Tree.PrefixKind(sp.Prefix); pk != "" {
			kind = pk
		} else if kind == "" {
			kind = sp.Prefix
		}
		d := s.ResolveOfKind(id, kind, "")
		if d == nil {
			d = s.ResolveNonLoc(id)
		}
		return identity{path: path, name: id, kind: kind, def: d}, true
	}
	// The slot's type: what script_docs say it holds, then what the install was seen writing there, which is evidence, not a declaration.
	if at.slotKey != "" {
		if r, ok := jomini.ScriptName(at.slotKey); ok {
			if jomini.IsEphemeral(r.Kind) {
				return ephemeralID(s, path, at.word, r.Kind), true
			}
			if d := s.ResolveOfKind(at.word, r.Kind, ""); d != nil {
				return identity{path: path, name: at.word, kind: r.Kind, def: d}, true
			}
		}
		if k := childOf(s, at.hang, at.slotKey).Kind(); k != "" {
			if jomini.IsEphemeral(k) {
				return ephemeralID(s, path, at.word, k), true
			}
			// A localization slot holds a key, not an object of the field's type.
			if loc.Classify(at.slotKey) == loc.PropNone {
				if d := s.ResolveOfKind(at.word, k, ""); d != nil {
					return identity{path: path, name: at.word, kind: k, def: d}, true
				}
			}
		}
	}
	// A definition somewhere in the model. ResolveNonLoc, not Resolve: every object in these games has a loc key named after it, and load order was deciding which came back.
	if d := s.ResolveNonLoc(at.word); d != nil {
		return identity{path: path, name: d.Key, kind: d.Kind, def: d}, true
	}
	// A word the type system declares, above localization because Paradox localizes a great many ordinary words.
	if ck := vocabRole(s, at.word); ck != "" {
		return identity{path: path, name: at.word, kind: ck}, true
	}
	// A loc key, never on the left of an `=`: a key is a property name, and claiming one is loc is not the honest answer.
	if !at.inKey && s.LocDefined(at.word) {
		if file, ln, _, origin, ok := s.LocSite(at.word); ok {
			return identity{path: path, name: at.word, kind: "loc_key",
				def: &index.Def{Kind: "loc_key", Key: at.word, Path: file, Line: ln, Origin: origin}}, true
		}
	}
	if slices.Contains(s.Install.Tree.DataFunctions, at.word) {
		return identity{path: path, name: at.word, kind: "data_function"}, true
	}
	if ck := jomini.CanonicalKind(at.word); jomini.IsMacroKind(ck) {
		return identity{path: path, name: at.word, kind: ck}, true
	}
	// Documented as a field somewhere: enough to say "field" and show the prose, not enough to say what it points at.
	if fieldDoc(s, at.word, "", "") != "" {
		return identity{path: path, name: at.word, kind: "field"}, true
	}
	// The floor: the word is named and nothing is claimed about it; the key holding it lets the card say what the value is FOR.
	id := identity{path: path, name: at.word}
	if !at.inKey && at.slotKey != "" {
		id.slot, id.extractKind, id.hang, id.block = at.slotKey, at.kind, at.hang, nestedBlock(at)
	}
	return id, true
}

func ephemeralID(s *session.Session, path, name, kind string) identity {
	if name == "" || kind == "" {
		return identity{}
	}
	return identity{path: path, name: name, kind: kind, def: s.ResolveOfKind(name, kind, "")}
}

// segmentIdentity names one segment of a dotted chain: a language prefix, else a declared word.
func segmentIdentity(s *session.Session, path, seg string) (identity, bool) {
	if p, ok := jomini.ParsePrefixed(seg); ok {
		kind := jomini.PrefixKind(p.Prefix)
		if kind == "" {
			kind = p.Prefix
		}
		return ephemeralID(s, path, p.Name, kind), true
	}
	if ck := vocabRole(s, seg); ck != "" {
		return identity{path: path, name: seg, kind: ck}, true
	}
	return identity{}, false
}

// nestedBlock is the block that governs the cursor, or empty at a definition's own level. Logic operators and scope switches are skipped: `OR = { … }` and `scope:x = { … }` group and redirect; neither changes which keys are legal, so neither is what a key belongs to.
func nestedBlock(at probe) string {
	if !at.inNested {
		return ""
	}
	for i := len(at.blockPath) - 1; i >= 1; i-- {
		if !transparentBlock(at.blockPath[i]) {
			return at.blockPath[i]
		}
	}
	return ""
}

// transparentBlock reports a block that groups or redirects without changing which keys may be written inside it.
func transparentBlock(key string) bool {
	if strings.ContainsRune(key, ':') || isWeightKey(key) {
		return true
	}
	switch strings.ToLower(key) {
	case "or", "and", "not", "nor", "nand", "trigger_if", "trigger_else",
		"trigger_else_if", "if", "else", "else_if", "root", "prev", "this":
		return true
	}
	return false
}

// isWeightKey reports a bare non-negative number used as an assignment key; dates and event ids are not bare numbers.
func isWeightKey(key string) bool {
	if key == "" {
		return false
	}
	for i := 0; i < len(key); i++ {
		if key[i] < '0' || key[i] > '9' {
			return false
		}
	}
	return true
}

// weightShare explains a weight as its share of the list it sits in. The denominator is the sum of the sibling weights, which is not always 100, so it is computed rather than assumed; each branch must be a body, which separates a distribution from a numeric lookup table.
func weightShare(parent *jomini.Assignment, listName, weight string) string {
	if parent == nil {
		return ""
	}
	b := jomini.ChildBlock(parent)
	if b == nil {
		return ""
	}
	var total, self float64
	n := 0
	for _, st := range b.Statements {
		a, ok := st.(*jomini.Assignment)
		if !ok || a.Key.Quoted || !isWeightKey(a.Key.Text) || jomini.ChildBlock(a) == nil {
			continue
		}
		v, err := strconv.ParseFloat(a.Key.Text, 64)
		if err != nil {
			continue
		}
		total += v
		n++
		if a.Key.Text == weight {
			self = v
		}
	}
	if n < 2 || total <= 0 {
		return ""
	}
	out := trimFloat(self) + " of " + trimFloat(total)
	if listName != "" {
		out += " in `" + listName + "`"
	}
	return out + " — a " + trimFloat(self*100/total) + "% chance"
}

func trimFloat(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// dottedSegmentAt returns the segment of a dotted chain containing off, or false when the text is not a chain.
func dottedSegmentAt(text string, off int) (string, bool) {
	if off < 0 || off > len(text) || !strings.ContainsRune(text, '.') {
		return "", false
	}
	start := strings.LastIndexByte(text[:off], '.') + 1
	end := strings.IndexByte(text[off:], '.')
	if end < 0 {
		end = len(text)
	} else {
		end += off
	}
	seg := text[start:end]
	if seg == "" {
		return "", false
	}
	return seg, true
}
