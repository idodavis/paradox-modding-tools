// cursor.go reads the cursor's context once: probeAt is the single probe every
// request takes at a script position -- word, the learned node the position
// hangs from, the block chain, and the scope type in force there. Every scope
// transition it applies is DECLARED by the game; nothing is inferred from name
// shape.

package lsp

import (
	"regexp"
	"slices"
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
	if len(chain) > 0 {
		if a, ok := chain[0].(*jomini.Assignment); ok && !a.Key.Quoted && !s.IsLocalDefFile(path, at.kind) {
			if d := s.Resolve(a.Key.Text); d != nil && d.Kind != "" && d.Kind != "loc_key" {
				at.kind = d.Kind
			}
		}
		fillCompleteSlot(&at, chain, off)
		at.scope = scopeIn(s, at.fileKind, chain, off)
		if key := pendingAssignKey(at.src, off); key != "" {
			at.slotKey = key
			at.inKey = false
		}
		at.saveName, at.saveOK = saveScopeIn(chain, off)
		at.paramOwner = paramOwnerIn(s, chain)
		at.segs = cellSegs(&at, chain, off)
		at.hang = nodeAt(s, at.fileKind, at.segs)
		if at.slotKey != "" {
			at.cell = childOf(s, at.hang, at.slotKey)
		}
	}
	return at, true
}

// nodeAt descends a kind's tree by the enclosing block keys: the install's tree, then the workspace's own where the install never wrote there.
func nodeAt(s *session.Session, kind string, segs []string) *index.Node {
	for _, sc := range []*index.Tree{&s.Install.Tree, &s.Work.Tree} {
		n := sc.Root(kind, false)
		for _, seg := range segs {
			if n == nil {
				break
			}
			_, _, n = sc.Step(n, seg, false)
		}
		if n != nil {
			return n
		}
	}
	return nil
}

// cellSegs is the enclosing block keys below the definition's own; a bare list member sits at the cell of the block key that holds it, so that key is not a segment.
func cellSegs(at *probe, chain []jomini.Statement, off int) []string {
	var segs []string
	for _, st := range chain {
		a, ok := st.(*jomini.Assignment)
		if !ok {
			continue
		}
		if b := jomini.ChildBlock(a); b != nil && off >= b.Range.Start && off <= b.Range.End {
			segs = append(segs, a.Key.Text)
		}
	}
	if len(segs) == 0 {
		return nil
	}
	segs = segs[1:]
	if n := len(segs); n > 0 && !at.inKey && segs[n-1] == at.slotKey {
		segs = segs[:n-1]
	}
	return segs
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

// scopeIn walks the probe's own chain for the scope type in force at off. A missing root is not a dead end: a declared link or iterator states the scope it produces regardless of what preceded it, so the walk continues and reports a scope only once something declared establishes one.
func scopeIn(s *session.Session, kind string, chain []jomini.Statement, off int) string {
	sc := s.Install.Declared
	if sc.Empty() {
		return ""
	}
	top, ok := chain[0].(*jomini.Assignment)
	if !ok || top.Key.Quoted {
		return ""
	}
	w := defRootScope(s, sc, kind, top)
	w.s, w.kind, w.hang = s, kind, s.Install.Tree.Root(kind, false)
	for _, st := range chain[1:] {
		a, ok := st.(*jomini.Assignment)
		if !ok || a.Key.Quoted {
			continue
		}
		// Only a block the cursor is actually inside changes the scope.
		if b := jomini.ChildBlock(a); b == nil || off < b.Range.Start || off > b.Range.End {
			continue
		}
		if !w.step(sc, a.Key.Text) {
			return ""
		}
	}
	return w.cur()
}

// scopeWalk carries the scope stack so `prev` and `root` resolve, and the learned node of the block it is inside, because the block's class says whether entering it changes the scope. rooted records that the bottom of the stack is the real root scope, which only an on_action declares.
type scopeWalk struct {
	s      *session.Session
	kind   string
	hang   *index.Node
	stack  []string
	rooted bool
}

func (w *scopeWalk) cur() string {
	if len(w.stack) == 0 {
		return ""
	}
	return w.stack[len(w.stack)-1]
}

func (w *scopeWalk) push(scope string) { w.stack = append(w.stack, scope) }

// step applies one block key, which may be a dotted chain that hops several times; any segment failing fails the whole chain, and false stops the walk rather than reporting a wrong scope. A date is not a chain.
func (w *scopeWalk) step(sc *declared.Schema, key string) bool {
	name, _, next := w.s.Install.Tree.Step(w.hang, key, false)
	if !strings.Contains(key, ".") || jomini.IsDateLiteral(key) {
		if !w.stepOne(sc, key, name, next) {
			return false
		}
	} else {
		for seg := range strings.SplitSeq(key, ".") {
			if seg == "" || !w.stepOne(sc, seg, name, next) {
				return false
			}
		}
	}
	w.hang = next
	return true
}

func (w *scopeWalk) stepOne(sc *declared.Schema, key, name string, next *index.Node) bool {
	k := strings.ToLower(game.KeyIdentity("", key))
	switch k {
	case "this":
		return true
	case "root":
		if !w.rooted || len(w.stack) == 0 {
			return false
		}
		w.push(w.stack[0])
		return true
	case "prev":
		if len(w.stack) < 2 {
			return false
		}
		w.push(w.stack[len(w.stack)-2])
		return true
	}
	// A declared scope link is a hop to its output type, taken on the game's word when the current scope is unknown; only a link naming one output type, and only into a scope script runs in: CK3 declares `modifier` with output `value`, and every weight block opens with one.
	if l, ok := sc.Links[k]; ok && l.Out != "" && !strings.Contains(l.Out, ",") && sc.Scopes[l.Out].ChangeScopes {
		if len(l.In) == 0 || w.cur() == "" || slices.Contains(l.In, w.cur()) {
			w.push(l.Out)
			return true
		}
	}
	// An iterator's Supported Targets is the element scope.
	if t, _, ok := sc.Token(k); ok && t.Target != "" {
		if (w.cur() == "" || declared.UsableIn(t.In, w.cur())) && sc.Scopes[t.Target].ChangeScopes {
			w.push(t.Target)
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

// ScopeMisuse is one engine token used where the game does not allow it.
type ScopeMisuse struct {
	Key, Scope string
	Allowed    []string
	Start, End int
}

// ScopeMisuses reports engine tokens used outside their declared scopes. It stays silent unless a type system is loaded, the token is declared by the game, and the scope at that point is known.
func ScopeMisuses(s *session.Session, path string, res jomini.Result) []ScopeMisuse {
	if res.Root == nil {
		return nil
	}
	sc := s.Install.Declared
	if sc.Empty() {
		return nil
	}
	rel := path
	if _, r, ok := s.Locate(path); ok {
		rel = r
	}
	kind := game.MatchExtract(s.GameID, rel).Kind
	var out []ScopeMisuse
	for _, st := range res.Root.Statements {
		a, ok := st.(*jomini.Assignment)
		if !ok || a.Key.Quoted {
			continue
		}
		b := jomini.ChildBlock(a)
		if b == nil {
			continue
		}
		w := defRootScope(s, sc, kind, a)
		w.s, w.kind, w.hang = s, kind, s.Install.Tree.Root(kind, false)
		WalkMisuse(sc, b, w, bodyAt(w), nil, &out)
	}
	return out
}

// defRootScope seeds a walk from one top-level definition. Only on_actions declare their scope; an event's is read from the on_actions that fire it, since its `type` names a presentation window, not a scope.
func defRootScope(s *session.Session, sc *declared.Schema, kind string, def *jomini.Assignment) scopeWalk {
	scope := ""
	switch kind {
	case "on_action":
		scope = sc.OnActions[strings.ToLower(def.Key.Text)]
	case "event":
		scope = eventRootScope(s, sc, def.Key.Text)
	}
	if scope == "" || scope == "none" {
		return scopeWalk{}
	}
	return scopeWalk{stack: []string{scope}, rooted: true}
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

// bodyAt is the declared token classes the block at the walk's position holds, 0 where the block is not a body.
func bodyAt(w scopeWalk) uint8 {
	if w.hang != nil && w.hang.Class == index.Body {
		return w.hang.Body
	}
	return 0
}

func WalkMisuse(sc *declared.Schema, b *jomini.Block, w scopeWalk, body uint8, params map[string]bool, out *[]ScopeMisuse) {
	for _, st := range b.Statements {
		a, ok := st.(*jomini.Assignment)
		if !ok || a.Key.Quoted {
			continue
		}
		key := a.Key.Text
		lk := strings.ToLower(game.KeyIdentity("", key))
		// An argument the enclosing token declares is not a statement in the scope that token produces; only the judgement is skipped, never the descent. A mod macro sharing a name with an engine token wins.
		if !params[lk] && w.s.Work.Tree.Macros[key] == "" && body != 0 && w.cur() != "" {
			if t, role, ok := sc.Token(lk); ok && body&roleBit(role) != 0 && !declared.UsableIn(t.In, w.cur()) {
				*out = append(*out, ScopeMisuse{Key: key, Scope: w.cur(), Allowed: t.In,
					Start: a.Key.Range.Start, End: a.Key.Range.End})
			}
		}
		child := jomini.ChildBlock(a)
		if child == nil {
			continue
		}
		next := scopeWalk{s: w.s, kind: w.kind, hang: w.hang, stack: slices.Clone(w.stack), rooted: w.rooted}
		if !next.step(sc, key) {
			continue
		}
		// The body class is inherited until a block opens one of its own.
		nextBody := body
		if b := bodyAt(next); b != 0 {
			nextBody = b
		}
		WalkMisuse(sc, child, next, nextBody, sc.TokenParams(lk), out)
	}
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
