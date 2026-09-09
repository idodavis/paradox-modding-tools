// cursor.go is the CST probe at a script position (complete, signature, Inspect).

package session

import (
	"regexp"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/parser/jomini"
	"paradox-modding-tools/services/internal/parser/loc"
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
func (s *Session) CursorAt(path string, line, col int) (Cursor, bool) {
	p, ok := s.probeAt(path, line, col)
	if !ok {
		return Cursor{}, false
	}
	return Cursor{
		Word: p.word, SlotKey: p.slotKey, Slot: p.slot, Kind: p.kind, Src: p.src,
		InKey: p.inKey, Off: p.off, Start: p.start,
	}, true
}

func (s *Session) probeAt(path string, line, col int) (probe, bool) {
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
			if !s.isLocalDefFile(path, at.kind) {
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
		at.paramOwner = s.paramOwnerIn(chain)
	}
	return at, true
}

func (s *Session) paramOwnerIn(chain []jomini.Statement) string {
	for i := len(chain) - 1; i >= 0; i-- {
		a, ok := chain[i].(*jomini.Assignment)
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
	for i := len(chain) - 1; i >= 0; i-- {
		a, ok := chain[i].(*jomini.Assignment)
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
			// Deeper than the definition itself. A field directly under
			// `ns.1 = { … }` has the definition as its block, and that is the
			// position the install's documentation is written for; a field under
			// a block under the definition is not.
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

func (s *Session) isLocalDefFile(path, extractKind string) bool {
	if extractKind == "mod_descriptor" {
		return true
	}
	if s.KindFor(path) != "meta" {
		return false
	}
	l := strings.ToLower(strings.ReplaceAll(path, "\\", "/"))
	return strings.Contains(l, "/.metadata/") && strings.HasSuffix(l, "/metadata.json")
}

func inComment(res jomini.Result, off int) bool {
	for _, c := range res.Comments {
		if off >= c.Range.Start && off < c.Range.End {
			return true
		}
	}
	return false
}

// locHit is what the cursor sits on inside a loc file: an entry's own key, or an
// interpolation inside a value. Engine marks an interpolation the game resolves
// itself (`[GetPlayer.GetName]`), which names no loc key.
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
