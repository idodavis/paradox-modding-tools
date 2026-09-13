// cursor.go reads the cursor's context: the CST probe at a script position
// (complete, signature, Inspect), and the scope type in force there.
//
// Every scope transition it applies is DECLARED by the game — scope links carry
// Input and Output scopes, and iterators are effects whose Supported Targets
// names the scope of each element. Nothing here is inferred from name shape.

package lsp

import (
	"paradox-modding-tools/services/internal/lang/session"
	"regexp"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/declared"
	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/loc"
)

// Cursor is the CST probe at a script position. Complete and signature use
// the slot fields; Inspect uses the same probe for identity.
//
// Slot is the effect/trigger context inherited from the nearest enclosing block
// that opens one. SlotKey names only the immediately enclosing block, which
// loses the context inside a scope change: `immediate = { capital_county = { …`
// is still an effect slot even though `capital_county` is not a slot keyword.
type Cursor struct {
	Word, SlotKey, Kind, Src string
	Slot                     string
	InKey                    bool
	Off, Start               int
}

type probe struct {
	src, word       string
	off, start, end int
	res             jomini.Result
	kind            string
	assign          *jomini.Assignment
	saveName        string
	saveOK          bool
	inKey           bool
	slotKey         string
	// inNested is true when the enclosing block is deeper than the definition.
	inNested bool
	// blockPath is the keys of the enclosing assignments, outermost first, so a
	// card can look past blocks that group without containing.
	blockPath []string
	// slotAssign is the assignment slotKey names, kept so a card can read the
	// siblings of the thing under the cursor — what a weight is a share of.
	slotAssign *jomini.Assignment
	slot       string
	paramOwner string
}

// CursorAt returns the CST probe at (line, UTF-8 column).
func CursorAt(s *session.Session, path string, line, col int) (Cursor, bool) {
	p, ok := probeAt(s, path, line, col)
	if !ok {
		return Cursor{}, false
	}
	return Cursor{
		Word: p.word, SlotKey: p.slotKey, Slot: p.slot, Kind: p.kind, Src: p.src,
		InKey: p.inKey, Off: p.off, Start: p.start,
	}, true
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
	if name, pStart, pEnd, ok := jomini.ScriptParamSpan(src, off); ok {
		word, start, end = name, pStart, pEnd
	}
	at := probe{src: src, off: off, res: res, word: word, start: start, end: end}
	rel := path
	if _, r, ok := s.Locate(path); ok {
		rel = r
	}
	at.kind = game.MatchExtract(s.GameID, rel).Kind
	if res.Root == nil || s.KindFor(path) == "loc" {
		return at, true
	}
	chain := jomini.NodeAtOffset(res.Root, off)
	if len(chain) > 0 {
		if a, ok := chain[0].(*jomini.Assignment); ok && !a.Key.Quoted {
			if !s.IsLocalDefFile(path, at.kind) {
				if d := s.Resolve(a.Key.Text); d != nil && d.Kind != "" && d.Kind != "loc_key" {
					at.kind = d.Kind
				}
			}
		}
		fillCompleteSlot(&at, chain, off)
		at.slot = inheritedSlot(chain, off)
		if key := pendingAssignKey(at.src, off); key != "" {
			at.slotKey = key
			at.inKey = false
		}
		at.saveName, at.saveOK = saveScopeIn(chain, off)
		at.paramOwner = paramOwnerIn(s, chain)
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
		if jomini.IsMacroKind(jomini.CanonicalKind(key)) || s.IsMacroDef(key) {
			return key
		}
		d := s.Resolve(key)
		if d != nil && jomini.IsMacroKind(d.Kind) {
			return key
		}
	}
	return ""
}

// inheritedSlot walks outward from the cursor for the nearest block that opens
// a trigger or effect slot. Only blocks the cursor is actually inside count, so
// sitting on a key does not inherit that key's own slot.
func inheritedSlot(chain []jomini.Statement, off int) string {
	for _, c := range slices.Backward(chain) {
		a, ok := c.(*jomini.Assignment)
		if !ok || a.Key.Quoted {
			continue
		}
		b := jomini.ChildBlock(a)
		if b == nil || off < b.Range.Start || off > b.Range.End {
			continue
		}
		if slot := jomini.ScriptSlot(a.Key.Text); slot != "" {
			return slot
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
			valLine := at.res.Lines().PositionAt(sc.Range.Start).Line
			if valLine != keyLine {
				at.inKey = true
			}
		}
		return
	}
	if sc, ok := inner.Value.(*jomini.Scalar); ok &&
		off >= sc.Range.Start && off <= sc.Range.End {
		at.slotKey = inner.Key.Text
		return
	}
	if b := jomini.BlockOf(inner.Value); b != nil &&
		off >= b.Range.Start && off <= b.Range.End {
		at.inKey = true
		at.slotKey = inner.Key.Text
		return
	}
	if off >= inner.Key.Range.End {
		at.slotKey = inner.Key.Text
		end := min(off, len(at.src))
		from := inner.Key.Range.End
		if from < end && !strings.Contains(at.src[from:end], "=") {
			at.inKey = true
		}
	}
}

var pendingAssignRe = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_]*)\s*=\s*$`)

func pendingAssignKey(src string, off int) string {
	if off < 0 {
		return ""
	}
	if off > len(src) {
		off = len(src)
	}
	lineStart := strings.LastIndex(src[:off], "\n") + 1
	m := pendingAssignRe.FindStringSubmatch(src[lineStart:off])
	if m == nil {
		return ""
	}
	return m[1]
}

func saveScopeIn(chain []jomini.Statement, off int) (name string, ok bool) {
	for _, n := range chain {
		a, isA := n.(*jomini.Assignment)
		if !isA || a.Key.Quoted {
			continue
		}
		if jomini.IsSaveScopeKey(a.Key.Text) {
			sc, isS := a.Value.(*jomini.Scalar)
			if isS && !sc.Quoted && off >= sc.Range.Start && off <= sc.Range.End {
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
			sc, isS := ca.Value.(*jomini.Scalar)
			if isS && !sc.Quoted && off >= sc.Range.Start && off <= sc.Range.End {
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

// locHitAt resolves the cursor with a single loc parse. Hover used to run two —
// one to ask whether the cursor was on an engine value, another to find the key.
func locHitAt(src string, off int) (locHit, bool) {
	res := loc.Parse(src)
	for _, e := range res.Entries {
		if off >= e.KeyRange.Start && off <= e.KeyRange.End {
			return locHit{key: e.Key, start: e.KeyRange.Start, end: e.KeyRange.End}, true
		}
		for _, ip := range loc.Interps(e.Value, e.ValueRange.Start) {
			if off < ip.WrapRange.Start || off >= ip.WrapRange.End {
				continue
			}
			return locHit{
				key: ip.Key, filter: ip.Filter,
				start: ip.KeyRange.Start, end: ip.KeyRange.End,
				engine: loc.IsLocEngineValue(ip.Key, ip.Filter),
			}, true
		}
	}
	if len(res.Entries) > 0 {
		return locHit{}, false
	}
	// No parsable entry yet — mid-typing, or a file with only a header. Fall
	// back to the raw token so hover still resolves.
	word, wStart, wEnd := jomini.Result{Src: src}.TokenAt(off)
	if word == "" {
		return locHit{}, false
	}
	return locHit{key: word, start: wStart, end: wEnd}, true
}

func macroCallAt(res jomini.Result, off int) (kind, name string, assign *jomini.Assignment, ok bool) {
	if res.Root == nil {
		return "", "", nil, false
	}
	var walk func([]jomini.Statement) bool
	walk = func(stmts []jomini.Statement) bool {
		var marker string
		var markStart, markEnd int
		for _, st := range stmts {
			if vs, ok := st.(*jomini.ValueStmt); ok {
				if sc, ok := vs.Value.(*jomini.Scalar); ok && !sc.Quoted &&
					!jomini.IsStop(sc.Text) {
					marker, markStart, markEnd = sc.Text, sc.Range.Start, sc.Range.End
				} else {
					marker = ""
				}
				if b := jomini.BlockOf(vs.Value); b != nil && walk(b.Statements) {
					return true
				}
				continue
			}
			a, isA := st.(*jomini.Assignment)
			if !isA {
				marker = ""
				continue
			}
			if marker != "" && !a.Key.Quoted {
				onMark := off >= markStart && off < markEnd
				onName := off >= a.Key.Range.Start && off < a.Key.Range.End
				if onMark || onName {
					kind, name, assign, ok = marker, a.Key.Text, a, true
					return true
				}
			}
			marker = ""
			if b := jomini.BlockOf(a.Value); b != nil && walk(b.Statements) {
				return true
			}
		}
		return false
	}
	ok = walk(res.Root.Statements)
	return kind, name, assign, ok
}

// scopeRefAt resolves `scope:name` at the cursor. It takes the parsed Result
// rather than the text because TokenAt re-tokenizes the whole file when the
// Result carries no token stream, and this asks up to three times.
func scopeRefAt(res jomini.Result, off int) (name string, ok bool) {
	src := res.Src
	word, wStart, wEnd := res.TokenAt(off)
	if word == "" {
		return "", false
	}
	if wEnd < len(src) && src[wEnd] == ':' && word == jomini.ScopePrefix {
		n, _, _ := res.TokenAt(wEnd + 1)
		if n == "" {
			return "", false
		}
		return n, true
	}
	if wStart > 0 && src[wStart-1] == ':' {
		pre, _, _ := res.TokenAt(wStart - 2)
		if pre == jomini.ScopePrefix {
			return word, true
		}
	}
	p, pok := jomini.ParsePrefixed(word)
	if pok && jomini.IsSavedScopePrefix(p) {
		return p.Name, true
	}
	return "", false
}

// ScopeAt returns the scope type in force at (line, UTF-8 column), or "" when
// it cannot be determined. An empty result means "do not filter", never
// "nothing is valid".
func ScopeAt(s *session.Session, path string, line, col int) string {
	res := s.Parsed(path)
	if res.Root == nil {
		return ""
	}
	off := res.Lines().OffsetAt(line, col)
	chain := jomini.NodeAtOffset(res.Root, off)
	if len(chain) == 0 {
		return ""
	}
	if s.Schema().Empty() {
		return ""
	}
	top, ok := chain[0].(*jomini.Assignment)
	if !ok || top.Key.Quoted {
		return ""
	}
	rel := path
	if _, r, ok := s.Locate(path); ok {
		rel = r
	}
	// A missing root is not a dead end. Only on_action files declare their own
	// starting scope, so bailing here meant every event, decision, scripted
	// effect and modifier body reported nothing at all -- which is most of what
	// anyone hovers. A declared scope link or iterator states the scope it
	// produces regardless of what preceded it, so the walk continues and
	// reports a scope only once something declared establishes one.
	w := defRootScopeLocked(s, s.Schema(), game.MatchExtract(s.GameID, rel).Kind, top)
	for _, st := range chain[1:] {
		a, ok := st.(*jomini.Assignment)
		if !ok || a.Key.Quoted {
			continue
		}
		// Only a block the cursor is actually inside changes the scope. Sitting
		// on `holder` itself asks what scope `holder` is used in, which is the
		// scope outside its block.
		b := jomini.ChildBlock(a)
		if b == nil || off < b.Range.Start || off > b.Range.End {
			continue
		}
		if !w.step(s.Schema(), a.Key.Text) {
			return ""
		}
	}
	return w.cur()
}

// scopeWalk carries the scope stack so `prev` and `root` resolve properly.
type scopeWalk struct {
	stack []string
	// rooted records that the BOTTOM of the stack is the real root scope,
	// which only an on_action declares. Without it the stack bottom is simply
	// the first hop the walk happened to take, and `root` must not pretend
	// otherwise: CK3's camp_party_actions.txt writes
	// `random_attending_character = { limit = { root = { any_attending_character`,
	// where root is the ACTIVITY the pulse action runs on. Reading it as
	// stack[0] answered "character" and reported nine pieces of correct vanilla
	// script as wrong-scope.
	rooted bool
}

func (w *scopeWalk) cur() string {
	if len(w.stack) == 0 {
		return ""
	}
	return w.stack[len(w.stack)-1]
}

func (w *scopeWalk) push(scope string) { w.stack = append(w.stack, scope) }

// step applies one block key. It reports false when the transition is unknown,
// which stops the walk rather than reporting a wrong scope.
// step follows a key, which may be a dotted chain: `root.liege.primary_title`
// hops three times. Splitting here rather than at each caller means hover and
// wrong-scope cannot disagree about where a chain lands.
//
// Any segment failing fails the whole chain. Callers clone the walk and discard
// it on false, so a partly-applied chain never leaks -- and stopping is the
// right answer, because a chain PMT can only half-follow ends somewhere it
// cannot name.
func (w *scopeWalk) step(sc *declared.Schema, key string) bool {
	// A date is not a chain. History files key their blocks by one --
	// `867.1.1 = { … }` -- and splitting that steps through "867", "1", "1".
	if !strings.Contains(key, ".") || jomini.IsDateLiteral(key) {
		return w.stepOne(sc, key)
	}
	for seg := range strings.SplitSeq(key, ".") {
		if seg == "" || !w.stepOne(sc, seg) {
			return false
		}
	}
	return true
}

func (w *scopeWalk) stepOne(sc *declared.Schema, key string) bool {
	k := strings.ToLower(game.KeyIdentity("", key))
	switch k {
	case "this":
		return true
	case "root":
		// Only when the bottom of the stack really is the root. Otherwise the
		// scope is one PMT cannot name, and the walk stops rather than guess.
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
	// A declared scope link is a hop to its output type.
	// An unknown current scope cannot contradict the declared input list, so the
	// hop is taken on the game's word. Whether the link was legal where the
	// modder used it is wrong-scope's question, not this one.
	//
	// A link declaring several output types names no single scope, so it is not
	// a hop this walk can take — the same answer it gives everywhere else it
	// cannot name a scope. EU5 declares `modifier` with "Output Scopes: value,
	// boolean", and every weight block in all three games opens with
	// `modifier = { … }`, so entering it put correct country-scoped script in a
	// scope named after two primitives. Tried and rejected as the fix: also
	// refusing links that require data. It was not needed — the comma alone
	// clears all three installs — and it would have cost `culture = { … }`,
	// which is both a data citation and a real hop.
	if l, ok := sc.Links[k]; ok && l.Out != "" && !strings.Contains(l.Out, ",") {
		if len(l.In) == 0 || w.cur() == "" || slices.Contains(l.In, w.cur()) {
			w.push(l.Out)
			return true
		}
	}
	// Iterators are effects/triggers whose Supported Targets is the element
	// scope: every_vassal is "Supported Scopes: character, Targets: character".
	if t, _, ok := sc.Token(k); ok && t.Target != "" {
		if (w.cur() == "" || declared.UsableIn(t.In, w.cur())) &&
			sc.Scopes[t.Target].ChangeScopes {
			w.push(t.Target)
			return true
		}
	}
	// An iterator the game does not declare still changes scope, just to a type
	// we cannot name. ScriptSlot recognises the any_/every_/random_/ordered_
	// prefixes as slots, so without this the walk would carry the outer scope
	// into the body and judge it against the wrong type. CK3's
	// any_held_county is undeclared and sits inside character-scoped
	// on_actions, which is exactly this case.
	if isIteratorKey(k) {
		return false
	}
	// Structural containers leave the scope alone.
	if scopeNeutral(k) {
		return true
	}
	return false
}

func scopeNeutral(k string) bool {
	return jomini.IsScopeNeutral(k) || jomini.ScriptSlot(k) != ""
}

// ScopeMisuse is one engine token used where the game does not allow it.
type ScopeMisuse struct {
	Key, Scope string
	Allowed    []string
	Start, End int
}

// ScopeMisuses reports engine tokens used outside their declared scopes.
// It stays silent unless all three are certain: a type system is loaded, the
// token is declared by the game, and the scope at that point is known. An
// unknown transition stops the descent rather than guessing.
func ScopeMisuses(s *session.Session, path string) []ScopeMisuse {
	res := s.Parsed(path)
	if res.Root == nil {
		return nil
	}
	sc := s.Schema()
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
		// A missing root is not a dead end -- the same rule ScopeAt states a few
		// functions down, and it was fixed there and not here. Only on_action
		// files declare a starting scope, so bailing on an empty one meant
		// wrong-scope never looked inside an event, a decision, a scripted
		// effect or a trait: it ran in common/on_action and nowhere else, on
		// every game.
		//
		// Nothing is loosened by continuing. step() already takes a declared hop
		// on the game's word when the current scope is unknown, and WalkMisuse
		// reports only once w.cur() is non-empty, so a misuse is still judged
		// against a scope something DECLARED rather than one PMT assumed.
		w := defRootScopeLocked(s, sc, kind, a)
		WalkMisuse(s, sc, b, w, "", nil, &out)
	}
	return out
}

// defRootScopeLocked seeds a walk from one top-level definition.
//
// Only on_actions are used, because only they declare their scope
// (`Expected Scope` in on_actions.log). An event's `type` field names its
// presentation window, not its scope: CK3 activity_event, letter_event and
// court_event bodies are all character-scoped. Reading `type = activity_event`
// as an activity scope produced 2,074 false reports over vanilla CK3 events
// alone. Deriving event scopes from the on_actions that fire them is the
// correct route and is not implemented yet.
func defRootScopeLocked(
	s *session.Session,
	sc *declared.Schema, kind string, def *jomini.Assignment,
) scopeWalk {
	if kind == "on_action" {
		if scope := sc.OnActions[strings.ToLower(def.Key.Text)]; scope != "" && scope != "none" {
			return scopeWalk{stack: []string{scope}, rooted: true}
		}
		return scopeWalk{}
	}
	if kind == "event" {
		if scope := s.EventRootScope(sc, def.Key.Text); scope != "" {
			return scopeWalk{stack: []string{scope}, rooted: true}
		}
	}
	return scopeWalk{}
}

func WalkMisuse(
	s *session.Session,
	sc *declared.Schema, b *jomini.Block, w scopeWalk, slot string,
	params map[string]bool, out *[]ScopeMisuse,
) {
	for _, st := range b.Statements {
		a, ok := st.(*jomini.Assignment)
		if !ok || a.Key.Quoted {
			continue
		}
		key := a.Key.Text
		lk := strings.ToLower(game.KeyIdentity("", key))
		// An argument the enclosing token declares is not a statement in the
		// scope that token produces. CK3 documents
		// `any_trait_in_category = { category = fame/health/etc }`, and
		// `category` is separately a declared trigger on artifact -- so judging
		// it reported correct vanilla script as wrong-scope.
		//
		// Only the JUDGEMENT is skipped, never the descent: `limit` is a
		// declared argument of every iterator, and skipping the statement whole
		// would stop the walk entering the one block most misuses sit in.
		if !params[lk] {
			// A mod macro may share a name with an engine token; the macro wins.
			if shadowed := s.IsMacroDef(key); !shadowed && slot != "" && w.cur() != "" {
				if t, role, ok := sc.Token(lk); ok && role == slot &&
					!declared.UsableIn(t.In, w.cur()) {
					*out = append(*out, ScopeMisuse{
						Key: key, Scope: w.cur(), Allowed: t.In,
						Start: a.Key.Range.Start, End: a.Key.Range.End,
					})
				}
			}
		}
		child := jomini.ChildBlock(a)
		if child == nil {
			continue
		}
		next := scopeWalk{stack: slices.Clone(w.stack), rooted: w.rooted}
		if !next.step(sc, key) {
			continue
		}
		nextSlot := slot
		if ns := jomini.ScriptSlot(lk); ns != "" {
			nextSlot = ns
		}
		WalkMisuse(s, sc, child, next, nextSlot, sc.TokenParams(lk), out)
	}
}
