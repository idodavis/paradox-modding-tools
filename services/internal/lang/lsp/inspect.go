// inspect.go resolves the cursor and fills the hover/F12 card. Not a Wails RPC.

package lsp

import (
	"paradox-modding-tools/services/internal/lang/session"
	"slices"
	"strconv"
	"strings"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/declared"
	"paradox-modding-tools/services/internal/lang/derived"
	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/loc"
)

const inspectValueCap = 8

// InspectValue is one unique harvested RHS or save-site expression.
type InspectValue struct {
	Text  string
	Count int
}

// Inspect is the hover/F12 payload. Kind/Key/Hint/Docs/Body are the card;
// empty Kind means hover should hide. RawKind/Name/Def drive F12 and refs.
type Inspect struct {
	Kind, Key, Hint, Docs, Body, Owner, Usage  string
	Values                                     []InspectValue
	More                                       int
	Origin, OriginName, Rel, Path              string
	Line, Col                                  int
	VanillaOriginName, VanillaRel, VanillaPath string
	VanillaLine, VanillaCol                    int

	// Scope is the scope type in force where the cursor sits, and ScopeOut the
	// type the token under it moves to when it is a scope link. "What scope am I
	// in?" is the question a new modder cannot answer from the file, and the
	// engine already computes it for completion -- it was simply never shown.
	Scope, ScopeOut string
	// Accepts states what this key's value may be. For the many fields no source
	// documents, what a field accepts is knowable even when what it means is not.
	Accepts string
	// Via names the resolution step that claimed this word, so a card that is
	// wrong can say which source claimed it. See resolve.go.
	Via string

	RawKind, Name string
	Def           *derived.Def
	FieldKey      bool
	Local         bool
	SpanStart     int
	SpanEnd       int
	AssignStart   int
	AssignEnd     int
	AssignLine    int
}

type identity struct {
	path, name, kind, owner, locFilter, extractKind string
	// block is the key of the enclosing block, when the cursor sits inside one.
	// What lets a card tell `trigger` in a triggered_desc from `trigger` in a
	// portrait, which the install documents once, by name, for both.
	block string
	// docs is prose the identify step worked out itself, for the cards whose
	// content is computed from the cursor's surroundings rather than looked up.
	docs string
	// via names the wordResolver that produced this identity, so a wrong card
	// can say which source claimed the word instead of leaving it to be guessed.
	via                                string
	def                                *derived.Def
	fieldKey, local                    bool
	spanStart, spanEnd                 int
	assignStart, assignEnd, assignLine int
}

// nestedBlock returns the block that actually governs the cursor, or empty at a
// definition's own level.
//
// Logic operators and scope switches are skipped. `OR = { … }` and
// `scope:secondary_recipient = { … }` group and redirect; neither changes which
// keys are legal, so neither is what a key belongs to. Using them produced cards
// reading "OR key" and "scope:secondary recipient key", which name a construct
// rather than a container.
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

// transparentBlock reports a block that groups or redirects without changing
// which keys may be written inside it.
func transparentBlock(key string) bool {
	if strings.ContainsRune(key, ':') {
		return true // a scope switch: scope:x, culture:y
	}
	switch strings.ToLower(key) {
	case "or", "and", "not", "nor", "nand", "trigger_if", "trigger_else",
		"trigger_else_if", "if", "else", "else_if", "root", "prev", "this":
		return true
	}
	return false
}

// PrettyKind is the Kind line: CanonicalKind, then underscores to spaces.
func PrettyKind(kind string) string {
	k := jomini.CanonicalKind(kind)
	if k == "" {
		k = kind
	}
	return strings.ReplaceAll(k, "_", " ")
}

// Inspect resolves the identifier at (line, UTF-8 column) and fills the card.
func InspectAt(s *session.Session, path string, line, col int) *Inspect {
	id, ok := identify(s, path, line, col)
	if !ok {
		return nil
	}
	ins := fillCard(s, id)
	if ins == nil {
		ins = &Inspect{}
	}
	ins.RawKind = id.kind
	ins.Name = id.name
	ins.Via = id.via
	fillScope(s, ins, path, line, col, id)
	ins.Def = id.def
	ins.FieldKey = id.fieldKey
	ins.Local = id.local
	ins.SpanStart = id.spanStart
	ins.SpanEnd = id.spanEnd
	ins.AssignStart = id.assignStart
	ins.AssignEnd = id.assignEnd
	ins.AssignLine = id.assignLine
	if ins.Owner == "" {
		ins.Owner = id.owner
	}
	return ins
}

// fillScope records the scope context around the cursor.
//
// Called from Inspect rather than from fillCard because it needs the cursor
// position, and because it holds no lock of its own: ScopeAt and scopeLink each
// take the read lock, so doing this inside a locked card builder would deadlock
// the way a locked model lookup did inside EachLoc.
func fillScope(s *session.Session, ins *Inspect, path string, line, col int, id identity) {
	ins.Scope = ScopeAt(s, path, line, col)
	// A scope link is the one token whose whole purpose is to move somewhere
	// else, so the transition is the useful thing to show -- "holder" answers
	// "character" only once you know it landed you in one.
	if l, ok := s.Schema().Link(id.name); ok && l.Out != "" {
		ins.ScopeOut = l.Out
	}
}

// inspectOf fills Kind/Hint/Docs for a resolved identity.
func inspectOf(s *session.Session, kind, key, docs string) *Inspect {
	out := &Inspect{Kind: PrettyKind(kind), Key: key, Docs: docs, RawKind: kind, Name: key}
	if docs == "" {
		out.Hint = composeHint(s, kind, key)
	}
	return out
}

// Session.IsMacroKind is deliberately gone: it took the read lock and then
// answered from jomini alone, reading no session state. Callers use
// jomini.IsMacroKind directly.
//
// The lock was the reason to remove it rather than a reason to keep it. A method
// that locks but reads nothing invites being called from inside a locked
// section, and Go's RWMutex deadlocks on a recursive RLock as soon as a writer
// is queued behind it — which a background rescan is.

func identify(s *session.Session, path string, line, col int) (identity, bool) {
	if s.KindFor(path) == "loc" {
		return identifyLoc(s, path, line, col)
	}
	at, ok := probeAt(s, path, line, col)
	if !ok {
		return identity{}, false
	}
	if name, ok := scopeRefAt(at.res, at.off); ok {
		return ephemeralID(s, path, name, "saved_scope"), true
	}
	if at.saveOK {
		return ephemeralID(s, path, at.saveName, "saved_scope"), true
	}
	if name, pStart, pEnd, ok := jomini.ScriptParamSpan(at.src, at.off); ok {
		d := s.ResolveOfKindOwner(name, "script_param", at.paramOwner)
		if d == nil {
			origin, _, _ := s.Locate(path)
			d = &derived.Def{
				Kind: "script_param", Key: name, Path: path, Origin: origin,
				Start: pStart, End: pEnd, OwnerKey: at.paramOwner,
				Line: at.res.Lines().PositionAt(pStart).Line,
			}
		}
		return identity{
			path: path, name: name, kind: "script_param",
			def: d, owner: at.paramOwner,
		}, true
	}
	if at.paramOwner != "" {
		if d := s.ResolveOfKindOwner(at.word, "script_param", at.paramOwner); d != nil {
			return identity{
				path: path, name: at.word, kind: "script_param",
				def: d, owner: at.paramOwner,
			}, true
		}
	}
	if id, ok := macroCallIdentity(s, path, at); ok {
		return id, true
	}
	if a := at.assign; a != nil {
		if p, ok := jomini.ParsePrefixed(a.Key.Text); ok && jomini.IsSavedScopePrefix(p) {
			return ephemeralID(s, path, p.Name, "saved_scope"), true
		}
		if s.IsLocalDefFile(path, at.kind) {
			return identity{
				path: path, name: a.Key.Text, kind: at.kind, local: true,
				extractKind: at.kind,
				assignStart: a.Key.Range.Start, assignEnd: a.Key.Range.End,
				assignLine: at.res.Lines().PositionAt(a.Key.Range.Start).Line,
			}, true
		}
		// A bare number on the left of an `=` is a weight, not a name.
		// `random_list = { 95 = {…} 5 = {…} }` is a 95% branch, but CK3 also
		// keys map positions by province id, so `95` resolves to a map_data
		// definition and the card offered "Amiens" for a probability. The files
		// that genuinely define numeric-keyed rows are claimed by
		// IsLocalDefFile above, so past this point a numeric key is a weight.
		if isWeightKey(a.Key.Text) {
			if share := weightShare(at.slotAssign, at.slotKey, a.Key.Text); share != "" {
				return identity{
					path: path, name: a.Key.Text, kind: "weight", docs: share,
				}, true
			}
			// Numeric but not a distribution — a Vic3 gene block writes
			// `20 = empty`, an index rather than a probability. Saying nothing
			// is the honest answer; it is the wrong answer that was the bug.
			return identity{}, false
		}
		// A dotted key is a scope chain, not a name: `holder.culture = { … }`
		// walks to the holder and then to their culture. Resolve the segment
		// under the cursor, so hovering `holder` explains the hop it makes
		// rather than producing no card at all.
		if seg, ok := dottedSegmentAt(a.Key.Text, at.off-a.Key.Range.Start); ok {
			if id, found := segmentIdentity(s, path, seg); found {
				return id, true
			}
		}
		if s.Schema().HasName(a.Key.Text) {
			if ck := s.VocabRole(a.Key.Text); ck != "" {
				return identity{
					path: path, name: a.Key.Text, kind: ck, via: "vocabulary",
				}, true
			}
		}
		docs := s.KindFieldDoc(a.Key.Text, at.kind)
		block := nestedBlock(at)
		// A key the install writes inside this block earns a card even when the
		// kind neither documents it nor lists it: both of those are depth-1, so
		// `instance` inside `colored_emblem` inside a coat of arms produced
		// nothing at all — 740 of 884 unnamed hovers on one conversion.
		if docs != "" || s.IsStructKey(at.kind, a.Key.Text) ||
			s.IsNestedMember(block, a.Key.Text) ||
			loc.Classify(a.Key.Text) != loc.PropNone {
			return identity{
				path: path, name: a.Key.Text, kind: at.kind, fieldKey: true,
				extractKind: at.kind, block: block,
			}, true
		}
	}
	if at.word == "" {
		return identity{}, false
	}
	if jomini.IsLiteralValue(at.word) {
		// One exception, and only where a person declared it: a total conversion
		// offsets its years so the engine's arithmetic works, and 8209.2.1 is
		// read by a human who would otherwise subtract 7,782 in their head. With
		// no calendar declared this stays exactly as it was — silent.
		if era := derived.EraDate(at.word, s.CalendarFor(path)); era != "" {
			return identity{
				path: path, name: at.word, kind: "date", docs: era,
			}, true
		}
		return identity{}, false
	}
	// Everything past this point is a question about which SOURCE gets to name
	// the word, not about where the cursor is. That order lives in resolve.go
	// as data so a new source cannot quietly outrank a declared one.
	return resolveWord(s, path, at)
}

func identifyLoc(s *session.Session, path string, line, col int) (identity, bool) {
	src := s.FileText(path)
	if src == "" {
		return identity{}, false
	}
	off := jomini.NewLineIndex(src).OffsetAt(line, col)
	hit, ok := locHitAt(src, off)
	if !ok {
		return identity{}, false
	}
	if hit.engine {
		return identity{
			path: path, name: hit.key, kind: "loc_value", locFilter: hit.filter,
		}, true
	}
	id := identity{
		path: path, name: hit.key, kind: "loc_key",
		spanStart: hit.start, spanEnd: hit.end,
	}
	if file, ln, _, origin, ok := s.LocSite(hit.key); ok {
		id.def = &derived.Def{
			Kind: "loc_key", Key: hit.key, Path: file, Line: ln, Origin: origin,
		}
	}
	return id, true
}

func ephemeralID(s *session.Session, path, name, kind string) identity {
	if name == "" || kind == "" {
		return identity{}
	}
	return identity{
		path: path, name: name, kind: kind,
		def: s.ResolveOfKind(name, kind),
	}
}

func macroCallIdentity(s *session.Session, path string, at probe) (identity, bool) {
	kind, name, a, ok := macroCallAt(at.res, at.off)
	if !ok || name == "" {
		return identity{}, false
	}
	d := s.ResolveOfKind(name, kind)
	if d != nil {
		ck := jomini.CanonicalKind(kind)
		for _, cand := range s.ModDefsOf(name) {
			if jomini.CanonicalKind(cand.Kind) == ck && derived.SamePath(cand.Path, path) {
				c := cand
				d = &c
				break
			}
		}
	}
	if d == nil && a != nil {
		origin, _, _ := s.Locate(path)
		d = &derived.Def{
			Kind: kind, Key: name, Path: path, Origin: origin,
			Start: a.Key.Range.Start, End: a.Key.Range.End,
			Line: at.res.Lines().PositionAt(a.Key.Range.Start).Line,
		}
	}
	return identity{path: path, name: name, kind: kind, def: d}, true
}

func fillCard(s *session.Session, id identity) *Inspect {
	switch {
	case id.kind == "loc_value":
		ins := inspectOf(s, "loc_value", "$"+id.name+"$", "")
		ins.Body = "format " + id.locFilter
		return ins
	case id.kind == "script_param":
		ins := inspectOf(s, "script_param", "$"+id.name+"$", "")
		owner := id.owner
		if owner == "" && id.def != nil {
			owner = id.def.OwnerKey
		}
		ins.Owner = owner
		ins.Values, ins.More = rankInspectValues(scriptParamCounts(s, id.name, owner))
		if id.def != nil {
			attachSite(s, ins, id.def.Path, id.def.Line, id.def.Origin, id.def.Start)
		}
		return ins
	case id.kind == "date":
		return &Inspect{
			Kind: "date", Key: id.name, Docs: id.docs,
			RawKind: id.kind, Name: id.name,
		}
	case id.kind == "weight":
		return &Inspect{
			Kind: "weight", Key: id.name, Docs: id.docs,
			RawKind: id.kind, Name: id.name,
		}
	case jomini.IsEphemeral(id.kind):
		return ephemeralCard(s, id.name, id.kind, id.def)
	case id.local:
		return localAssignCard(s, id)
	case id.fieldKey:
		docs := s.FieldDoc(id.name, id.extractKind)
		// Name the block the key is actually in. "event key" is true of
		// `type` and of a `trigger` four levels down, which tells a reader
		// nothing about where they are — and where they are is what decides
		// what the key means.
		label, suffix := id.extractKind, " key"
		if id.block != "" {
			label = id.block
			// A block that is itself an effect or trigger contains arguments,
			// not keys: "set variable key" and "add key" read as though
			// set_variable were a container.
			if s.Schema().HasName(id.block) {
				suffix = " argument"
			}
		}
		ins := inspectOf(s, label, id.name, docs)
		ins.Kind += suffix
		// composeHint describes the KIND — its scope type, how it is cited, how
		// its localization keys are named. None of that is about the field under
		// the cursor, and inspectOf attaches it whenever the field itself has no
		// documentation. Hovering `color2` inside a coat of arms therefore
		// reported the coat of arms' localization convention as though it
		// described `color2`. The Kind line already says which object this is a
		// key of; anything more has to be about the key.
		ins.Hint = ""
		// A key can be a declared token as well: `add_to_list` reaches this
		// branch because it is also a structure key of the kind being edited,
		// and the signature the game states for it was being dropped on the
		// floor — 58 of CK3's 1,201 sampled hovers that had one.
		ins.Usage = s.TokenUsage(id.name)
		ins.Accepts = fieldAccepts(s, id.extractKind, id.name)
		return vanillaSite(s, ins)
	case id.def != nil && id.def.Kind == "loc_key":
		return locCard(s, id.name)
	case id.def != nil:
		return defCard(s, id.name, id.def)
	case id.kind != "":
		ins := inspectOf(s, id.kind, id.name, docsOnly(s, id.name, id.kind))
		ins.Usage = s.TokenUsage(id.name)
		ins.Accepts = fieldAccepts(s, id.kind, id.name)
		return vanillaSite(s, ins)
	default:
		return nil
	}
}

func defCard(s *session.Session, word string, d *derived.Def) *Inspect {
	docs := docsOnly(s, word, d.Kind)
	// A named script body has no other description: the author's comment above
	// it is the only one there will ever be.
	if docs == "" && d.Doc != "" {
		docs = d.Doc
	}
	ins := inspectOf(s, d.Kind, d.Key, docs)
	ins.Usage = s.TokenUsage(word)
	if ins.Usage == "" {
		ins.Usage = s.MacroUsage(d)
	}
	// First hit wins: conventions are sorted by coverage, then by the plainest shape.
	for _, key := range s.ConventionLocKeys(d.Kind, d.Key) {
		if text, ok := s.DefaultLoc(key); ok && text != "" {
			ins.Body = unescapeLocDisplay(text)
			break
		}
	}
	attachSite(s, ins, d.Path, d.Line, d.Origin, d.Start)
	attachOverlay(s, ins, d)
	return ins
}

// MacroUsage builds the call signature of a scripted macro from the $PARAM$
// names harvested inside its own body.
//
// No dump can supply this: script_docs documents the engine API, and of CK3's
// 11,708 scripted macros exactly 4 appear there — name collisions with real
// engine tokens. The definition is the only source, and it is a good one:
// 2,536 CK3 macros, 468 EU5 and 44 Vic3 take parameters, and before this every
// one of them got the same fixed string on its card.
//

func ephemeralCard(s *session.Session, name, kind string, d *derived.Def) *Inspect {
	if name == "" || kind == "" {
		return nil
	}
	key := name
	if kind == "saved_scope" {
		key = "scope:" + name
	}
	ins := inspectOf(s, kind, key, "")
	ins.Values, ins.More = rankInspectValues(ephemeralCounts(s, name, kind))
	if d != nil {
		attachSite(s, ins, d.Path, d.Line, d.Origin, d.Start)
	}
	return ins
}

func locCard(s *session.Session, key string) *Inspect {
	text, _ := s.DefaultLoc(key)
	file, line, start, origin, ok := s.LocSite(key)
	if text == "" && !ok {
		return nil
	}
	ins := inspectOf(s, "loc_key", key, "")
	ins.Body = unescapeLocDisplay(text)
	if ok {
		// start, not 0. attachSite only computes Col when start > 0, and
		// languageClient gates the whole selection object on Col — so passing 0
		// shipped the right Line and then dropped it, opening every
		// localization hover at line 1. Vanilla keys still pass 0 because the
		// sidecar's LocEntry has no offset to give; that half needs a
		// LocFormatVersion bump and lands with the interning change.
		attachSite(s, ins, file, line, origin, start)
		attachOverlay(s, ins, &derived.Def{
			Kind: "loc_key", Key: key, Path: file, Line: line, Origin: origin,
		})
	}
	return ins
}

func localAssignCard(s *session.Session, id identity) *Inspect {
	docs := s.FieldDoc(id.name, id.extractKind)
	if docs == "" && !s.IsStructKey(id.extractKind, id.name) {
		return nil
	}
	ins := inspectOf(s, id.extractKind, id.name, docs)
	ins.Kind += " key"
	origin, _, _ := s.Locate(id.path)
	attachSite(s, ins, id.path, id.assignLine, origin, id.assignStart)
	return ins
}

func scriptParamCounts(s *session.Session, name, owner string) map[string]int {
	counts := map[string]int{}
	if owner != "" {
		for _, r := range s.RefsTo(owner) {
			if !jomini.IsMacroKind(r.Kind) || r.Path == "" {
				continue
			}
			if v := scalarAt(s, r.Path, r.Start, owner, name); v != "" {
				counts[v]++
			}
		}
		if len(counts) > 0 {
			return counts
		}
	}
	for _, r := range s.RefsTo(name) {
		if r.Kind != "script_param" || r.Path == "" {
			continue
		}
		if owner != "" && r.OwnerKey != "" && r.OwnerKey != owner {
			continue
		}
		if v := scalarAt(s, r.Path, r.Start, name, ""); v != "" {
			counts[v]++
		}
	}
	return counts
}

func scalarAt(s *session.Session, path string, start int, key, param string) string {
	res := s.Parsed(path)
	if res.Root == nil {
		return ""
	}
	chain := jomini.NodeAtOffset(res.Root, start)
	for _, c := range slices.Backward(chain) {
		a, ok := c.(*jomini.Assignment)
		if !ok || a.Key.Quoted || a.Key.Text != key {
			continue
		}
		v := a.Value
		if param != "" {
			b := jomini.BlockOf(v)
			if b == nil {
				return ""
			}
			v = nil
			for _, st := range b.Statements {
				ia, ok := st.(*jomini.Assignment)
				if ok && !ia.Key.Quoted && ia.Key.Text == param {
					v = ia.Value
					break
				}
			}
			if v == nil {
				return ""
			}
		}
		if sc, ok := v.(*jomini.Scalar); ok && !sc.Quoted {
			return sc.Text
		}
		return ""
	}
	return ""
}

func ephemeralCounts(s *session.Session, name, kind string) map[string]int {
	counts := map[string]int{}
	ck := jomini.CanonicalKind(kind)
	for _, d := range s.FindDefsOf(name, kind, 256, false, true) {
		if d.Key != name || jomini.CanonicalKind(d.Kind) != ck {
			continue
		}
		v := d.Value
		if v == "" && ck == "saved_scope" {
			v = scopeTargetExpr(s, &d)
		}
		if v != "" {
			counts[v]++
		}
	}
	return counts
}

func scopeTargetExpr(s *session.Session, d *derived.Def) string {
	res := s.Parsed(d.Path)
	if res.Root == nil || d.Start <= 0 {
		return "root"
	}
	chain := jomini.NodeAtOffset(res.Root, d.Start)
	for _, c := range slices.Backward(chain) {
		a, ok := c.(*jomini.Assignment)
		if !ok || a.Key.Quoted {
			continue
		}
		k := a.Key.Text
		if jomini.IsSaveScopeKey(k) || jomini.IsSaveScopeValueKey(k) {
			continue
		}
		low := strings.ToLower(k)
		if isIteratorKey(low) {
			return iteratorLabel(a)
		}
		if jomini.IsStop(k) || jomini.ScriptSlot(k) != "" {
			if low == "root" {
				return "root"
			}
			continue
		}
		if strings.Contains(k, ":") || jomini.ScriptSlot(k) != "" {
			return k
		}
	}
	return "root"
}

func isIteratorKey(k string) bool {
	return strings.HasPrefix(k, "any_") || strings.HasPrefix(k, "every_") ||
		strings.HasPrefix(k, "random_") || strings.HasPrefix(k, "ordered_")
}

func iteratorLabel(a *jomini.Assignment) string {
	k := a.Key.Text
	b := jomini.BlockOf(a.Value)
	if b == nil {
		return k
	}
	for _, st := range b.Statements {
		ia, ok := st.(*jomini.Assignment)
		if !ok || ia.Key.Quoted || !strings.EqualFold(ia.Key.Text, "list") {
			continue
		}
		if sc, ok := ia.Value.(*jomini.Scalar); ok && !sc.Quoted && sc.Text != "" {
			return k + " - list=" + sc.Text
		}
	}
	return k
}

// modifierSignature renders the one line a declared modifier gets on its hover
// card, from what script_docs actually states about it.
//
// This is the last unrendered part of the Schema. A modifier's `Area` is the
// category list the game prints on the header line — CK3 "character and
// province", EU5 "location, all", Vic3 a Mask — and for EU5 it is the *only*
// thing declared: all 2,436 modifiers carry a category and none carry prose.
// Before this the card for `local_trades_per_burgher` was empty.
func modifierSignature(m declared.Modifier) string {
	areas := modifierAreas(m.Area)
	if len(areas) == 0 {
		return ""
	}
	return "Modifier · applies to " + strings.Join(areas, " or ")
}

// modifierAreas splits a declared category list into the scope types it names.
//
// The three games phrase it differently — CK3 "character and province", EU5
// "location, all", Vic3 a Mask — so the separators of all three are accepted.
// "all" is dropped: EU5 stamps it on every one of its 2,436 modifiers, so it
// distinguishes nothing and would only push the useful half off the line.
func modifierAreas(area string) []string {
	if area == "" {
		return nil
	}
	fields := strings.FieldsFunc(area, func(r rune) bool {
		return r == ',' || r == ';' || r == '/' || r == '|'
	})
	out := make([]string, 0, len(fields))
	seen := map[string]bool{}
	for _, f := range fields {
		for part := range strings.SplitSeq(strings.TrimSpace(f), " and ") {
			p := strings.ToLower(strings.TrimSpace(part))
			if p == "" || p == "all" || p == "none" || seen[p] {
				continue
			}
			seen[p] = true
			out = append(out, PrettyKind(p))
		}
	}
	return out
}

// scopeList renders declared input scopes. "none" means unrestricted, so it
// carries no information worth showing.
func scopeList(in []string) string {
	keep := make([]string, 0, len(in))
	for _, sc := range in {
		if sc == "none" || sc == "" {
			continue
		}
		keep = append(keep, PrettyKind(sc))
	}
	return strings.Join(keep, " or ")
}

func attachOverlay(s *session.Session, ins *Inspect, d *derived.Def) {
	if ins == nil || d == nil || isVanillaOrigin(d.Origin) || d.Kind == "mod_descriptor" {
		return
	}
	var v *derived.Def
	for _, cand := range s.VanillaDefs(d.Key) {
		if cand.Kind == d.Kind {
			c := cand
			v = &c
			break
		}
	}
	if v == nil || v.Path == "" || derived.SamePath(v.Path, d.Path) {
		return
	}
	ins.VanillaPath = v.Path
	ins.VanillaRel = s.DisplayRel(v.Path)
	ins.VanillaLine = v.Line
	ins.VanillaOriginName = s.OriginName(game.OriginVanilla)
	if v.Start > 0 {
		ins.VanillaCol = s.Parsed(v.Path).Lines().PositionAt(v.Start).Character
	}
}

func isVanillaOrigin(origin string) bool {
	return origin == "" || origin == game.OriginVanilla
}

func vanillaSite(s *session.Session, ins *Inspect) *Inspect {
	if ins == nil {
		return nil
	}
	ins.Origin = game.OriginVanilla
	ins.OriginName = s.OriginName(game.OriginVanilla)
	return ins
}

func attachSite(s *session.Session, ins *Inspect, path string, line int, origin string, start int) {
	if ins == nil || path == "" {
		return
	}
	ins.Path = path
	ins.Rel = s.DisplayRel(path)
	ins.Line = line
	if origin == "" {
		origin = game.OriginVanilla
	}
	ins.Origin = origin
	ins.OriginName = s.OriginName(origin)
	if start > 0 {
		ins.Col = s.Parsed(path).Lines().PositionAt(start).Character
	}
}

// ResolveNonLoc resolves a word to a scripted object, preferring one that can
// actually be referenced over a localization entry or an event namespace.
func rankInspectValues(counts map[string]int) ([]InspectValue, int) {
	type pair struct {
		text string
		n    int
	}
	var ps []pair
	for t, n := range counts {
		if t != "" && n > 0 {
			ps = append(ps, pair{t, n})
		}
	}
	slices.SortFunc(ps, func(a, b pair) int {
		if a.n != b.n {
			return b.n - a.n
		}
		return strings.Compare(a.text, b.text)
	})
	more := 0
	if len(ps) > inspectValueCap {
		more = len(ps) - inspectValueCap
		ps = ps[:inspectValueCap]
	}
	out := make([]InspectValue, len(ps))
	for i, p := range ps {
		out[i] = InspectValue{Text: p.text, Count: p.n}
	}
	return out, more
}

func unescapeLocDisplay(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 >= len(s) {
			b.WriteByte(s[i])
			continue
		}
		switch s[i+1] {
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case '\\':
			b.WriteByte('\\')
		case '"':
			b.WriteByte('"')
		default:
			b.WriteByte(s[i])
			continue
		}
		i++
	}
	return strings.TrimSpace(b.String())
}

func languageKind(k string) bool {
	switch k {
	case "effect", "trigger", "scope", "field", "loc_key", "loc_value",
		"script_param", "saved_scope", "data_function", "mod_descriptor":
		return true
	}
	return false
}

func firstProse(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// isWeightKey reports a bare non-negative number used as an assignment key.
// Dates (`1066.1.1`) and event ids (`ns.1`) are not bare numbers and are
// unaffected.
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

// weightShare explains a weight as its share of the list it sits in. The
// denominator is the sum of the sibling weights, which is not always 100 —
// `random_list = { 10 = {…} 30 = {…} }` is a quarter and three quarters — so it
// is computed rather than assumed, and the list is named from the file rather
// than from any list of known weighted effects.
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
		if !ok || a.Key.Quoted || !isWeightKey(a.Key.Text) {
			continue
		}
		// Each branch of a weighted list is a body. Requiring that separates a
		// distribution from a numeric lookup table, whose entries are scalars.
		if jomini.ChildBlock(a) == nil {
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

// fieldAccepts states what a field's value may be, in one line.
//
// This is the answer for the fields that have no prose anywhere — 28% of them
// on EU5 — where the card previously borrowed the enclosing kind's description
// and said something misleading instead. What a field accepts is often knowable
// even when nothing documents what it means.
//
// dottedSegmentAt returns the segment of a dotted chain containing off, or false
// when the text is not a chain.
//
// `holder.culture` is two hops, and a reader hovering either half is asking
// about that half. Splitting is cheap and the alternative is what happened
// before: the whole string resolved to nothing and the card never appeared.
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

// segmentIdentity names one hop of a scope chain, if the game declares it.
// segmentIdentity names one segment of a dotted chain.
//
// The two named resolvers rather than a copy of their bodies: this function's
// first half was byte-identical to resolveLanguagePrefix and its second half to
// resolveVocabulary, which made it the third place the resolution order was
// written. A copy cannot be re-ordered when the canonical list is, and it
// stamped no `via`.
func segmentIdentity(s *session.Session, path, seg string) (identity, bool) {
	at := probe{word: seg}
	for _, r := range []wordResolver{
		{"language-prefix", resolveLanguagePrefix},
		{"vocabulary", resolveVocabulary},
	} {
		if id, ok := r.resolve(s, path, at); ok {
			id.via = r.name
			if id.path == "" {
				id.path = path
			}
			return id, true
		}
	}
	return identity{}, false
}

// composeHint is the fallback subtitle when a kind has no prose of its own: the
// declared scope type first, then how the object is cited, nested and localized.
// Empty when nothing is known.
func composeHint(s *session.Session, kind, _ string) string {
	ck := jomini.CanonicalKind(kind)
	if ck == "" {
		ck = kind
	}
	if languageKind(ck) {
		return ""
	}
	var parts []string
	// Lead with the declared scope type: what this object *is* to the engine
	// matters more than how PMT found it.
	if scope := s.ScopeOfKind(ck); scope != "" {
		parts = append(parts, PrettyKind(scope)+" scope")
	}
	if s.Cache() != nil {
		if p := s.Cache().KindInfo[ck]; p != "" {
			parts = append(parts, firstProse(p))
		} else if p := s.Cache().KindInfo[kind]; p != "" {
			parts = append(parts, firstProse(p))
		}
		if prefix := s.KindPrefix(ck); prefix != "" {
			parts = append(parts, "cite `"+prefix+":id`")
		}
		for _, sh := range s.Cache().NestedShapes {
			if jomini.CanonicalKind(sh.ChildKind) == ck {
				msg := "nested in " + PrettyKind(sh.ParentKind)
				if sh.GroupKey != "" {
					msg += " `" + sh.GroupKey + "`"
				}
				parts = append(parts, msg)
				break
			}
		}
		// No localization-convention line. It was tried twice — as the label
		// "loc kind_id", then as the template "loc `<id>_adj`" — and neither
		// says anything a reader can act on: the first named PMT's own
		// vocabulary, the second a placeholder. Rendering the concrete key
		// instead would only repeat what the card's Body already shows when the
		// key exists.
		//
		// Which localization keys an object should have, and which of them are
		// missing, is a list with per-key state. That belongs in the Inspector
		// pane, not in a one-line subtitle, and `required-loc` already reports
		// the ones the game demands.
		if jomini.IsMacroKind(ck) {
			// The real signature, when the macro takes parameters, is built by
			// MacroUsage and shown as a code block; this is only the shape note
			// for one that takes none.
			parts = append(parts, "invoked by name")
		}
	}
	if game.IsFIOS(s.GameID, ck) || game.IsFIOS(s.GameID, kind) {
		parts = append(parts, "FIOS")
	}
	return strings.Join(parts, " · ")
}

// schemaSignature renders what the game itself declares about an engine token
// or a scope link: where it runs, and what it takes or yields. This replaces
// the old raw "character; targets: culture" scopes string.
func schemaSignature(s *session.Session, word string) string {
	if t, role, ok := s.Schema().Token(word); ok {
		parts := []string{strings.ToUpper(role[:1]) + role[1:]}
		if in := scopeList(t.In); in != "" {
			parts = append(parts, "runs on "+in)
		}
		if t.Target != "" {
			parts = append(parts, "takes a "+PrettyKind(t.Target))
		}
		return strings.Join(parts, " · ")
	}
	if l, ok := s.Schema().Link(word); ok && l.Out != "" {
		var parts []string
		if in := scopeList(l.In); in != "" {
			parts = append(parts, "from "+in)
		} else if l.Global {
			parts = append(parts, "Global link")
		}
		parts = append(parts, "yields "+PrettyKind(l.Out))
		if l.Data {
			parts = append(parts, "cite `"+word+":id`")
		}
		return strings.Join(parts, " · ")
	}
	if m, ok := s.Schema().Modifier(word); ok {
		return modifierSignature(m)
	}
	return ""
}

// The order is the same invariant lsp/valueSources follows for completion, and
// for the same reason: declared beats derived, so a slot the game types is not
// described by whatever PMT inferred about it. The two are separate lists
// because one produces prose and the other produces items; they must not
// disagree about precedence.
func fieldAccepts(s *session.Session, kind, field string) string {
	// Declared target: the game states the argument's scope type.
	if target := s.TokenTarget(field); target != "" {
		// A bound database gives the citation form as well, which is the more
		// useful answer -- one accept produces a valid value. Its absence does
		// not weaken the declaration, though: the game still states the type,
		// and reporting it is better than falling through to something PMT
		// merely inferred about the same key.
		if cite, k := s.CiteFormOfScope(target); k != "" {
			if cite != "" {
				return "accepts a " + PrettyKind(k) + ", cited `" + cite + ":<id>`"
			}
			return "accepts a " + PrettyKind(k)
		}
		return "accepts a " + PrettyKind(target)
	}
	// Declared slot: script_docs name the database this key holds.
	if r, ok := jomini.ScriptName(field); ok && r.Kind != "" {
		return "accepts a " + PrettyKind(r.Kind)
	}
	// Declared by the game's own documentation. Above every derived source and
	// above the harvested enum, which is only ever a sample of how vanilla
	// happens to use the field; below TokenTarget and ScriptName because those
	// come from script_docs, which is the same authority in machine-readable
	// form and names a database rather than a type word.
	if dt := s.DeclaredFieldType(kind, field); dt.Known() {
		if len(dt.Values) > 0 {
			return "one of: " + strings.Join(dt.Values, ", ")
		}
		if dt.Array {
			return "accepts several " + dt.Prim + " values"
		}
		return "accepts a " + dt.Prim
	}
	// Derived slot: what the install walk inferred, behind its coverage floor.
	if k := s.FieldValueKind(field); k != "" && !jomini.IsEphemeral(k) {
		return "accepts a " + PrettyKind(k)
	}
	// A field holding a localization key takes any key, not one of a set.
	if loc.Classify(field) != loc.PropNone {
		return "accepts a localization key"
	}
	return ""
}
func docsOnly(s *session.Session, word, kind string) string {
	doc := s.FieldDoc(word, kind)
	if doc == "" {
		// Install prose and dump prose both land in TokenDoc during a scan, but
		// the Schema carries its own description; use it rather than relying on
		// that map having been filled.
		doc = s.SchemaDoc(word)
	}
	line := schemaSignature(s, word)
	if line == "" {
		// No declared type system: fall back to the raw scopes text.
		if sc := s.TokenScopes(word); sc != "" {
			line = "Scope here: " + sc
		}
	}
	if line == "" {
		return doc
	}
	if doc != "" {
		doc += "\n\n"
	}
	return doc + line
}
