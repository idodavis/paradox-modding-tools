// inspect.go resolves the cursor and fills the hover/F12 card. Not a Wails RPC.

package session

import (
	"slices"
	"strconv"
	"strings"

	"paradox-modding-tools/services/internal/catalog"
	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/parser/jomini"
	"paradox-modding-tools/services/internal/parser/loc"
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
	Def           *catalog.Def
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
	def                                *catalog.Def
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
func (s *Session) Inspect(path string, line, col int) *Inspect {
	id, ok := s.identify(path, line, col)
	if !ok {
		return nil
	}
	ins := s.fillCard(id)
	if ins == nil {
		ins = &Inspect{}
	}
	ins.RawKind = id.kind
	ins.Name = id.name
	ins.Via = id.via
	s.fillScope(ins, path, line, col, id)
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
// the way LocConventionOwner did inside EachLoc.
func (s *Session) fillScope(ins *Inspect, path string, line, col int, id identity) {
	ins.Scope = s.ScopeAt(path, line, col)
	// A scope link is the one token whose whole purpose is to move somewhere
	// else, so the transition is the useful thing to show -- "holder" answers
	// "character" only once you know it landed you in one.
	if l, ok := s.scopeLink(id.name); ok && l.Out != "" {
		ins.ScopeOut = l.Out
	}
}

// inspectOf fills Kind/Hint/Docs for a resolved identity.
func (s *Session) inspectOf(kind, key, docs string) *Inspect {
	out := &Inspect{Kind: PrettyKind(kind), Key: key, Docs: docs, RawKind: kind, Name: key}
	if docs == "" {
		out.Hint = s.composeHint(kind, key)
	}
	return out
}

// composeHint is the fallback subtitle when a kind has no prose of its own: the
// declared scope type first, then how the object is cited, nested and localized.
// Empty when nothing is known.
func (s *Session) composeHint(kind, _ string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
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
	if scope := s.scopeOfKindLocked(ck); scope != "" {
		parts = append(parts, PrettyKind(scope)+" scope")
	}
	if s.cache != nil {
		if p := s.cache.KindInfo[ck]; p != "" {
			parts = append(parts, firstProse(p))
		} else if p := s.cache.KindInfo[kind]; p != "" {
			parts = append(parts, firstProse(p))
		}
		if prefix := s.kindPrefix[ck]; prefix != "" {
			parts = append(parts, "cite `"+prefix+":id`")
		}
		for _, sh := range s.cache.NestedShapes {
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
			// macroUsage and shown as a code block; this is only the shape note
			// for one that takes none.
			parts = append(parts, "invoked by name")
		}
	}
	if game.IsFIOS(s.GameID, ck) || game.IsFIOS(s.GameID, kind) {
		parts = append(parts, "FIOS")
	}
	// The entry-mode prefixes used to be appended here. They are a property of
	// the game, not of the thing under the cursor, so they appeared on every
	// EU5 card and on many were the entire hint — "INJECT:/REPLACE:" as the sole
	// content of a modifier's hover. A line that is always present says nothing.
	return strings.Join(parts, " · ")
}

// Session.IsMacroKind is deliberately gone: it took the read lock and then
// answered from jomini alone, reading no session state. Callers use
// jomini.IsMacroKind directly.
//
// The lock was the reason to remove it rather than a reason to keep it. A method
// that locks but reads nothing invites being called from inside a locked
// section, and Go's RWMutex deadlocks on a recursive RLock as soon as a writer
// is queued behind it — which a background rescan is.

// IsMacroDef reports that key is a harvested scripted-macro definition.
func (s *Session) IsMacroDef(key string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.macroDefKinds[key]
	return ok
}

// FireKind returns the derived fire target kind for an assignment key.
// Loc properties (title, desc, …) never fire.
func (s *Session) FireKind(key string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if loc.Classify(key) != loc.PropNone {
		return ""
	}
	if s.cache != nil {
		if k := s.cache.FireKeys[key]; k != "" {
			return k
		}
		if k := s.cache.FireKeys[strings.ToLower(key)]; k != "" {
			return k
		}
	}
	if k := s.modFireKeys[key]; k != "" {
		return k
	}
	return s.modFireKeys[strings.ToLower(key)]
}

// ConventionLocKeys expands the derived loc conventions for one def, keeping
// those that hold for at least minCoverage percent of the kind. Callers pass
// catalog.LocConventionUsed to ask what the engine may read, or
// LocConventionRequired to ask what it demands.
func (s *Session) ConventionLocKeys(kind, id string, minCoverage int) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.cache == nil {
		return nil
	}
	// A kind harvested under its plural or prefixed spelling shares the
	// canonical kind's affixes; the exact spelling wins where it has any.
	affixes := s.cache.LocAffixes[kind]
	if len(affixes) == 0 {
		affixes = s.cache.LocAffixes[jomini.CanonicalKind(kind)]
	}
	return catalog.ConventionKeys(id, affixes, minCoverage)
}

// LocConventionOwner reports the definition a loc key names by convention —
// `ACHIEVEMENT_DESC_<id>` naming the achievement, `notification_<id>_tooltip`
// naming the message. A key with an owner is consumed by the engine even though
// nothing cites it, which is what stops it reading as orphaned.
func (s *Session) LocConventionOwner(key string) (id, kind string, ok bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.locConventionOwnerLocked(key)
}

// LocKeyExplained reports whether anything in the model can account for key,
// without saying what. It is deliberately more permissive than
// LocConventionOwner and answers a different question.
//
// The two have opposite failure costs. Hover and go-to-definition need to know
// WHICH definition owns a key, so a wrong answer sends the reader to the wrong
// file — that path stays strict. The orphan check only needs to know whether
// anything consumes the key at all, and a wrong answer there tells a modder
// their correct localization is dead. Returning a bool and nothing else is what
// keeps the permissive side from leaking into the strict one: it cannot produce
// a link because it does not produce an identity.
//
// Its failure mode is a genuine orphan going unreported, which is the direction
// to fail in.
func (s *Session) LocKeyExplained(key string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, _, ok := s.locConventionOwnerLocked(key); ok {
		return true
	}
	// Anything in script that names this key accounts for it, whatever kind of
	// reference the harvester filed it under.
	//
	// UsedLocKeys counts only refs of kind loc / loc-broad / loc-convention,
	// but go-to-references matches by name across every kind — which is why
	// F12 on `friend_after_subjugation` finds its uses while the orphan check
	// called it dead. The engine reads an object's id and its localization by
	// the same name, so a cited id is a used key; asking a narrower question
	// here than navigation already answers is what produced the false positive.
	if len(s.refsByKey[key]) > 0 || len(s.cacheRefsByKey[key]) > 0 {
		return true
	}
	if s.cache == nil {
		return false
	}
	// The same convention, but the stem only has to be a name script uses — not
	// a definition. A game rule category is declared by the rules that cite it
	// (`category = my_category`) and may never be a def of its own, so
	// `game_rule_category_my_category` had no owner and read as an orphan.
	// Strict resolution keeps asking for a def, because it has to name a file
	// to open; this side only has to say "something accounts for this".
	if _, _, ok := catalog.ConventionOwner(
		s.cache.LocAffixes, key, s.nameKindsLocked); ok {
		return true
	}
	if len(s.cache.LocKeyAffixes) == 0 {
		return false
	}
	_, ok := catalog.KeyAffixOwner(s.cache.LocKeyAffixes, key, s.isLocKeyLocked)
	return ok
}

// nameKindsLocked visits the kinds anything in the model files under id --
// definitions first, then the kinds references were harvested as.
//
// Permissive by design and used only by LocKeyExplained. defKindsLocked stays
// the strict version: it backs resolution that must name a file to open, and a
// reference is not a place to open.
func (s *Session) nameKindsLocked(id string, visit func(kind string) bool) {
	stop := false
	s.defKindsLocked(id, func(kind string) bool {
		if !visit(kind) {
			stop = true
			return false
		}
		return true
	})
	if stop {
		return
	}
	seen := map[string]bool{}
	for _, group := range [][]catalog.Ref{s.refsByKey[id], s.cacheRefsByKey[id]} {
		for _, r := range group {
			if r.Kind == "" || seen[r.Kind] {
				continue
			}
			seen[r.Kind] = true
			if !visit(r.Kind) {
				return
			}
		}
	}
}

// isLocKeyLocked reports whether key is defined in any language, workspace or
// install. Any language: a derived shape is a property of the key set, and a
// mod that ships only french still proves the stem exists.
func (s *Session) isLocKeyLocked(key string) bool {
	for _, m := range s.locByLang {
		if _, ok := m[key]; ok {
			return true
		}
	}
	if s.vanillaLoc != nil {
		_, ok := s.vanillaLoc.Sites[key]
		return ok
	}
	return false
}

func (s *Session) locConventionOwnerLocked(key string) (id, kind string, ok bool) {
	if s.cache == nil || len(s.cache.LocAffixes) == 0 {
		return "", "", false
	}
	if id, kind, ok := catalog.ConventionOwner(
		s.cache.LocAffixes, key, s.defKindsLocked); ok {
		return id, kind, true
	}
	// Then the names that are members of a definition rather than definitions
	// themselves. A CK3 game rule is a definition and gets `rule_<id>`, but its
	// options are nested inside it and never become definitions — and vanilla
	// writes 590 `setting_<option>` keys for its 380 options.
	return catalog.MemberConventionOwner(
		s.cache.LocMemberAffixes, key, s.isMemberLocked)
}

// isMemberLocked reports name as a block-member key of kind, across the install
// and the mods. Read-only: the sets are built by rebuildMemberSetsLocked.
func (s *Session) isMemberLocked(kind, name string) bool {
	if kind == "" || name == "" {
		return false
	}
	// Member names are stored lowercased by harvestStructVocab; the stem comes
	// off a localization key verbatim.
	return s.memberSets[kind][strings.ToLower(name)]
}

// rebuildMemberSetsLocked indexes block-member names for the kinds that carry a
// localization member convention, and only those — 22 kinds on CK3 rather than
// the 218 the install has structures for.
//
// Built up front rather than on first use so LocConventionOwner can stay a read
// lock. Filling a cache inside a query meant that query took the write lock,
// which deadlocks the moment anything calls it from inside EachLoc or any other
// read-locked walk. A lookup should not be able to do that.
func (s *Session) rebuildMemberSetsLocked() {
	s.memberSets = nil
	if s.cache == nil || len(s.cache.LocMemberAffixes) == 0 {
		return
	}
	s.memberSets = make(map[string]map[string]bool, len(s.cache.LocMemberAffixes))
	for kind := range s.cache.LocMemberAffixes {
		set := map[string]bool{}
		for _, k := range s.cache.Structures[kind] {
			set[k] = true
		}
		for _, k := range s.modMembers[kind] {
			set[k] = true
		}
		s.memberSets[kind] = set
	}
}

// defKindsLocked visits the kinds that define id, stopping when visit returns
// false. ConventionOwner calls it for every span of every candidate key, so it
// reads the indexes in place — no defsOf copy, and no result slice.
func (s *Session) defKindsLocked(id string, visit func(kind string) bool) {
	if id == "" {
		return
	}
	for _, defs := range [2][]catalog.Def{s.defsByKey[id], s.cacheByKey[id]} {
		for _, d := range defs {
			k := jomini.CanonicalKind(d.Kind)
			if !jomini.IsObjectKind(k) {
				continue
			}
			if !visit(k) {
				return
			}
		}
	}
}

func (s *Session) identify(path string, line, col int) (identity, bool) {
	if s.KindFor(path) == "loc" {
		return s.identifyLoc(path, line, col)
	}
	at, ok := s.probeAt(path, line, col)
	if !ok {
		return identity{}, false
	}
	if name, ok := scopeRefAt(at.res, at.off); ok {
		return s.ephemeralID(path, name, "saved_scope"), true
	}
	if at.saveOK {
		return s.ephemeralID(path, at.saveName, "saved_scope"), true
	}
	if name, pStart, pEnd, ok := jomini.ScriptParamSpan(at.src, at.off); ok {
		d := s.resolveOfKindOwner(name, "script_param", at.paramOwner)
		if d == nil {
			origin, _, _ := s.Locate(path)
			d = &catalog.Def{
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
		if d := s.resolveOfKindOwner(at.word, "script_param", at.paramOwner); d != nil {
			return identity{
				path: path, name: at.word, kind: "script_param",
				def: d, owner: at.paramOwner,
			}, true
		}
	}
	if id, ok := s.macroCallIdentity(path, at); ok {
		return id, true
	}
	if a := at.assign; a != nil {
		if p, ok := jomini.ParsePrefixed(a.Key.Text); ok && jomini.IsSavedScopePrefix(p) {
			return s.ephemeralID(path, p.Name, "saved_scope"), true
		}
		if s.isLocalDefFile(path, at.kind) {
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
		// isLocalDefFile above, so past this point a numeric key is a weight.
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
			if id, found := s.segmentIdentity(path, seg); found {
				return id, true
			}
		}
		// What the game DECLARES the key to be wins over what the install walk
		// inferred about the kind it sits in. `every_vassal = { … }` inside an
		// event's immediate block is an effect the game documents; claimed as a
		// field key it came back as "immediate key" with no documentation at
		// all, and `name` inside `set_variable = { … }` came back as
		// "set variable key". Both lost a declared answer to a derived one,
		// which is the fault this whole engine is organised against.
		//
		// Only names the game's own script_docs declare. A name merely harvested
		// from the corpus is derived too, and must not outrank a field.
		// This one stays where it is: it is positional, not precedence. Whether a
		// key is a declared token or the enclosing kind's field has to be
		// decided before the field-key branch below, and resolveWord runs after
		// identify has finished. It carries the vocabulary resolver's `via`
		// because that is the source the answer came from — via names the
		// source, not the function that reached it.
		if s.DeclaresEngineName(a.Key.Text) {
			if ck := s.vocabRole(a.Key.Text); ck != "" {
				return identity{
					path: path, name: a.Key.Text, kind: ck, via: "vocabulary",
				}, true
			}
		}
		docs := s.kindFieldDoc(a.Key.Text, at.kind)
		block := nestedBlock(at)
		// A key the install writes inside this block earns a card even when the
		// kind neither documents it nor lists it: both of those are depth-1, so
		// `instance` inside `colored_emblem` inside a coat of arms produced
		// nothing at all — 740 of 884 unnamed hovers on one conversion.
		if docs != "" || s.isStructKey(at.kind, a.Key.Text) ||
			s.IsNestedMember(block, a.Key.Text) ||
			loc.Classify(a.Key.Text) != loc.PropNone {
			return identity{
				path: path, name: a.Key.Text, kind: at.kind, fieldKey: true,
				extractKind: at.kind, block: block,
			}, true
		}
		// Nothing else here. What used to follow was a hand-written
		// definition -> macro-kind -> vocabulary ladder, which is a subset of
		// resolve.go's order written a second time — and written in the wrong
		// order: it asked macro-kind, derived from a folder name, BEFORE
		// vocabulary, declared by script_docs. That is the one inversion
		// resolve.go exists to prevent, and no test could see it because the
		// order tests compare indices in wordResolvers rather than asking which
		// resolver claimed a word.
		//
		// Falling through to resolveWord runs the canonical order and stamps
		// `via`, so a wrong card can now name the step that produced it.
	}
	if at.word == "" {
		return identity{}, false
	}
	// A literal is a value, not a name, and resolving one against the databases
	// finds whatever happens to share the spelling. That is not a rare accident:
	// 68,109 of CK3's 215,448 definitions are keyed by a bare number — 38,020
	// characters, 10,887 provinces, 4,223 dynasties — so on one conversion 4,044
	// arithmetic operands (`value = 5`, `add = 10`, `weight = 100`, `days`,
	// `MIN`) resolved to province ids. `is_ai = yes` reached the localization
	// lookup the same way, and vanilla defines a key called `yes`.
	//
	// The primitive set is small and closed — number, date, boolean — and
	// getting it complete is the whole defence. Dates were missing from it,
	// which is why `game_start_date < 1178.1.1` hovered as a struggle: CK3 keys
	// its history files by date.
	//
	// Unconditional, with no slot required. Measured on a 21,499-file
	// conversion, literals sitting in a slot whose type the engine knows number
	// **zero**, so this costs nothing; requiring a slot only left the positions
	// without one exposed. Assignment keys never reach here — the at.assign
	// branch above claims them — so a `random_list` weight and a numeric key in
	// the file that defines it are unaffected.
	if jomini.IsLiteralValue(at.word) {
		// One exception, and only where a person declared it: a total conversion
		// offsets its years so the engine's arithmetic works, and 8209.2.1 is
		// read by a human who would otherwise subtract 7,782 in their head. With
		// no calendar declared this stays exactly as it was — silent.
		if era := catalog.EraDate(at.word, s.CalendarFor(path)); era != "" {
			return identity{
				path: path, name: at.word, kind: "date", docs: era,
			}, true
		}
		return identity{}, false
	}
	// Everything past this point is a question about which SOURCE gets to name
	// the word, not about where the cursor is. That order lives in resolve.go
	// as data so a new source cannot quietly outrank a declared one.
	return s.resolveWord(path, at)
}

func (s *Session) identifyLoc(path string, line, col int) (identity, bool) {
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
		id.def = &catalog.Def{
			Kind: "loc_key", Key: hit.key, Path: file, Line: ln, Origin: origin,
		}
	}
	return id, true
}

func (s *Session) ephemeralID(path, name, kind string) identity {
	if name == "" || kind == "" {
		return identity{}
	}
	return identity{
		path: path, name: name, kind: kind,
		def: s.resolveOfKind(name, kind),
	}
}

func (s *Session) macroCallIdentity(path string, at probe) (identity, bool) {
	kind, name, a, ok := macroCallAt(at.res, at.off)
	if !ok || name == "" {
		return identity{}, false
	}
	d := s.resolveOfKind(name, kind)
	if d != nil {
		ck := jomini.CanonicalKind(kind)
		for _, cand := range s.ModDefsOf(name) {
			if jomini.CanonicalKind(cand.Kind) == ck && SamePath(cand.Path, path) {
				c := cand
				d = &c
				break
			}
		}
	}
	if d == nil && a != nil {
		origin, _, _ := s.Locate(path)
		d = &catalog.Def{
			Kind: kind, Key: name, Path: path, Origin: origin,
			Start: a.Key.Range.Start, End: a.Key.Range.End,
			Line: at.res.Lines().PositionAt(a.Key.Range.Start).Line,
		}
	}
	return identity{path: path, name: name, kind: kind, def: d}, true
}

func (s *Session) fillCard(id identity) *Inspect {
	switch {
	case id.kind == "loc_value":
		ins := s.inspectOf("loc_value", "$"+id.name+"$", "")
		ins.Body = "format " + id.locFilter
		return ins
	case id.kind == "script_param":
		ins := s.inspectOf("script_param", "$"+id.name+"$", "")
		owner := id.owner
		if owner == "" && id.def != nil {
			owner = id.def.OwnerKey
		}
		ins.Owner = owner
		ins.Values, ins.More = rankInspectValues(s.scriptParamCounts(id.name, owner))
		if id.def != nil {
			s.attachSite(ins, id.def.Path, id.def.Line, id.def.Origin, id.def.Start)
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
		return s.ephemeralCard(id.name, id.kind, id.def)
	case id.local:
		return s.localAssignCard(id)
	case id.fieldKey:
		docs := s.FieldDocIn(id.name, id.extractKind, id.block)
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
			if s.DeclaresEngineName(id.block) {
				suffix = " argument"
			}
		}
		ins := s.inspectOf(label, id.name, docs)
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
		ins.Accepts = s.fieldAccepts(id.extractKind, id.name)
		return s.vanillaSite(ins)
	case id.def != nil && id.def.Kind == "loc_key":
		return s.locCard(id.name)
	case id.def != nil:
		return s.defCard(id.name, id.def)
	case id.kind != "":
		ins := s.inspectOf(id.kind, id.name, s.docsOnly(id.name, id.kind))
		ins.Usage = s.TokenUsage(id.name)
		ins.Accepts = s.fieldAccepts(id.kind, id.name)
		return s.vanillaSite(ins)
	default:
		return nil
	}
}

func (s *Session) defCard(word string, d *catalog.Def) *Inspect {
	docs := s.docsOnly(word, d.Kind)
	// A named script body has no other description: the author's comment above
	// it is the only one there will ever be.
	if docs == "" && d.Doc != "" {
		docs = d.Doc
	}
	ins := s.inspectOf(d.Kind, d.Key, docs)
	ins.Usage = s.TokenUsage(word)
	if ins.Usage == "" {
		ins.Usage = s.macroUsage(d)
	}
	// The card's prose comes from the def's own localization, found through
	// whichever naming convention the kind actually follows. First hit wins:
	// the conventions are already ranked by how well they hold for the kind.
	for _, key := range s.ConventionLocKeys(d.Kind, d.Key, catalog.LocConventionUsed) {
		if text, ok := s.DefaultLoc(key); ok && text != "" {
			ins.Body = unescapeLocDisplay(text)
			break
		}
	}
	s.attachSite(ins, d.Path, d.Line, d.Origin, d.Start)
	s.attachOverlay(ins, d)
	return ins
}

// macroUsage builds the call signature of a scripted macro from the $PARAM$
// names harvested inside its own body.
//
// No dump can supply this: script_docs documents the engine API, and of CK3's
// 11,708 scripted macros exactly 4 appear there — name collisions with real
// engine tokens. The definition is the only source, and it is a good one:
// 2,536 CK3 macros, 468 EU5 and 44 Vic3 take parameters, and before this every
// one of them got the same fixed string on its card.
//
// A macro with no parameters gets nothing here; `invoked by name` in the hint
// already says all there is to say about it.
func (s *Session) macroUsage(d *catalog.Def) string {
	if d == nil || d.Key == "" || !jomini.IsMacroKind(d.Kind) {
		return ""
	}
	names := s.FindParamsOf(d.Key, maxMacroParams)
	if len(names) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(d.Key)
	b.WriteString(" = {\n")
	for _, n := range names {
		b.WriteString("\t")
		b.WriteString(n)
		b.WriteString(" = <value>\n")
	}
	b.WriteString("}")
	return b.String()
}

// maxMacroParams caps one macro's rendered signature. The largest measured on
// the three installs takes 6.
const maxMacroParams = 32

func (s *Session) ephemeralCard(name, kind string, d *catalog.Def) *Inspect {
	if name == "" || kind == "" {
		return nil
	}
	key := name
	if kind == "saved_scope" {
		key = "scope:" + name
	}
	ins := s.inspectOf(kind, key, "")
	ins.Values, ins.More = rankInspectValues(s.ephemeralCounts(name, kind))
	if d != nil {
		s.attachSite(ins, d.Path, d.Line, d.Origin, d.Start)
	}
	return ins
}

func (s *Session) locCard(key string) *Inspect {
	text, _ := s.DefaultLoc(key)
	file, line, start, origin, ok := s.LocSite(key)
	if text == "" && !ok {
		return nil
	}
	ins := s.inspectOf("loc_key", key, "")
	ins.Body = unescapeLocDisplay(text)
	if ok {
		// start, not 0. attachSite only computes Col when start > 0, and
		// languageClient gates the whole selection object on Col — so passing 0
		// shipped the right Line and then dropped it, opening every
		// localization hover at line 1. Vanilla keys still pass 0 because the
		// sidecar's LocEntry has no offset to give; that half needs a
		// LocFormatVersion bump and lands with the interning change.
		s.attachSite(ins, file, line, origin, start)
		s.attachOverlay(ins, &catalog.Def{
			Kind: "loc_key", Key: key, Path: file, Line: line, Origin: origin,
		})
	}
	return ins
}

func (s *Session) localAssignCard(id identity) *Inspect {
	docs := s.FieldDoc(id.name, id.extractKind)
	if docs == "" && !s.isStructKey(id.extractKind, id.name) {
		return nil
	}
	ins := s.inspectOf(id.extractKind, id.name, docs)
	ins.Kind += " key"
	origin, _, _ := s.Locate(id.path)
	s.attachSite(ins, id.path, id.assignLine, origin, id.assignStart)
	return ins
}

func (s *Session) scriptParamCounts(name, owner string) map[string]int {
	counts := map[string]int{}
	if owner != "" {
		for _, r := range s.RefsTo(owner) {
			if !jomini.IsMacroKind(r.Kind) || r.Path == "" {
				continue
			}
			if v := s.callArgScalar(r.Path, r.Start, owner, name); v != "" {
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
		if v := s.assignScalarAt(r.Path, r.Start, name); v != "" {
			counts[v]++
		}
	}
	return counts
}

func (s *Session) callArgScalar(path string, start int, callKey, param string) string {
	res := s.Parsed(path)
	if res.Root == nil {
		return ""
	}
	chain := jomini.NodeAtOffset(res.Root, start)
	for i := len(chain) - 1; i >= 0; i-- {
		a, ok := chain[i].(*jomini.Assignment)
		if !ok || a.Key.Quoted || a.Key.Text != callKey {
			continue
		}
		b := jomini.BlockOf(a.Value)
		if b == nil {
			return ""
		}
		for _, st := range b.Statements {
			ia, ok := st.(*jomini.Assignment)
			if !ok || ia.Key.Quoted || ia.Key.Text != param {
				continue
			}
			if sc, ok := ia.Value.(*jomini.Scalar); ok && !sc.Quoted {
				return sc.Text
			}
			return ""
		}
		return ""
	}
	return ""
}

func (s *Session) assignScalarAt(path string, start int, key string) string {
	res := s.Parsed(path)
	if res.Root == nil {
		return ""
	}
	chain := jomini.NodeAtOffset(res.Root, start)
	for i := len(chain) - 1; i >= 0; i-- {
		a, ok := chain[i].(*jomini.Assignment)
		if !ok || a.Key.Quoted || a.Key.Text != key {
			continue
		}
		if sc, ok := a.Value.(*jomini.Scalar); ok && !sc.Quoted {
			return sc.Text
		}
		return ""
	}
	return ""
}

func (s *Session) ephemeralCounts(name, kind string) map[string]int {
	counts := map[string]int{}
	ck := jomini.CanonicalKind(kind)
	for _, d := range s.FindDefsOf(name, kind, 256, false, true) {
		if d.Key != name || jomini.CanonicalKind(d.Kind) != ck {
			continue
		}
		v := d.Value
		if v == "" && ck == "saved_scope" {
			v = s.scopeTargetExpr(&d)
		}
		if v != "" {
			counts[v]++
		}
	}
	return counts
}

func (s *Session) scopeTargetExpr(d *catalog.Def) string {
	res := s.Parsed(d.Path)
	if res.Root == nil || d.Start <= 0 {
		return "root"
	}
	chain := jomini.NodeAtOffset(res.Root, d.Start)
	for i := len(chain) - 1; i >= 0; i-- {
		a, ok := chain[i].(*jomini.Assignment)
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

func (s *Session) docsOnly(word, kind string) string {
	doc := s.FieldDoc(word, kind)
	if doc == "" {
		// Install prose and dump prose both land in TokenDoc during a scan, but
		// the schema carries its own description; use it rather than relying on
		// that map having been filled.
		doc = s.schemaDoc(word)
	}
	line := s.schemaSignature(word)
	if line == "" {
		// No declared type system: fall back to the raw scopes text.
		if sc := s.tokenScopes(word); sc != "" {
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

// schemaDoc returns the description the game ships for an engine token or link.
func (s *Session) schemaDoc(word string) string {
	if t, _, ok := s.engineToken(word); ok && t.Doc != "" {
		return t.Doc
	}
	if l, ok := s.scopeLink(word); ok {
		return l.Doc
	}
	return ""
}

// schemaSignature renders what the game itself declares about an engine token
// or a scope link: where it runs, and what it takes or yields. This replaces
// the old raw "character; targets: culture" scopes string.
func (s *Session) schemaSignature(word string) string {
	if t, role, ok := s.engineToken(word); ok {
		parts := []string{strings.ToUpper(role[:1]) + role[1:]}
		if in := scopeList(t.In); in != "" {
			parts = append(parts, "runs on "+in)
		}
		if t.Target != "" {
			parts = append(parts, "takes a "+PrettyKind(t.Target))
		}
		return strings.Join(parts, " · ")
	}
	if l, ok := s.scopeLink(word); ok && l.Out != "" {
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
	if m, ok := s.modifierToken(word); ok {
		return modifierSignature(m)
	}
	return ""
}

// modifierSignature renders the one line a declared modifier gets on its hover
// card, from what script_docs actually states about it.
//
// This is the last unrendered part of the schema. A modifier's `Area` is the
// category list the game prints on the header line — CK3 "character and
// province", EU5 "location, all", Vic3 a Mask — and for EU5 it is the *only*
// thing declared: all 2,436 modifiers carry a category and none carry prose.
// Before this the card for `local_trades_per_burgher` was empty.
func modifierSignature(m catalog.Modifier) string {
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
		for _, part := range strings.Split(strings.TrimSpace(f), " and ") {
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

func (s *Session) attachOverlay(ins *Inspect, d *catalog.Def) {
	if ins == nil || d == nil || isVanillaOrigin(d.Origin) || d.Kind == "mod_descriptor" {
		return
	}
	var v *catalog.Def
	for _, cand := range s.VanillaDefs(d.Key) {
		if cand.Kind == d.Kind {
			c := cand
			v = &c
			break
		}
	}
	if v == nil || v.Path == "" || SamePath(v.Path, d.Path) {
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

func (s *Session) vanillaSite(ins *Inspect) *Inspect {
	if ins == nil {
		return nil
	}
	ins.Origin = game.OriginVanilla
	ins.OriginName = s.OriginName(game.OriginVanilla)
	return ins
}

func (s *Session) attachSite(ins *Inspect, path string, line int, origin string, start int) {
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

// resolveNonLoc resolves a word to a scripted object, preferring one that can
// actually be referenced over a localization entry or an event namespace.
//
// A namespace declares an id prefix for events; nothing points at one. But it is
// a definition like any other in the index, so load order decided the tie: a mod
// writing `namespace = conqueror` outranked vanilla's `conqueror` trait, and
// `add_trait = conqueror` hovered as the namespace. What kind of thing a name
// denotes is not something override order gets to decide. A namespace still
// resolves when nothing else of that name does, so hovering one still works.
func (s *Session) resolveNonLoc(word string) *catalog.Def {
	if d := s.ResolveMatching(word, func(d catalog.Def) bool {
		return jomini.IsObjectKind(d.Kind)
	}); d != nil {
		return d
	}
	// Second pass drops only the namespace exclusion, so a namespace still
	// resolves when nothing else of that name does.
	return s.ResolveMatching(word, catalog.IsCitableDef)
}

func (s *Session) resolveOfKind(word, kind string) *catalog.Def {
	return s.resolveOfKindOwner(word, kind, "")
}

func (s *Session) resolveOfKindOwner(word, kind, owner string) *catalog.Def {
	if word == "" || kind == "" {
		return nil
	}
	ck := jomini.CanonicalKind(kind)
	return s.ResolveMatching(word, func(d catalog.Def) bool {
		if jomini.CanonicalKind(d.Kind) != ck {
			return false
		}
		if owner != "" && d.OwnerKey != "" && d.OwnerKey != owner {
			return false
		}
		return true
	})
}

func (s *Session) locDefined(key string) bool {
	if _, ok := s.DefaultLoc(key); ok {
		return true
	}
	_, _, _, _, ok := s.LocSite(key)
	return ok
}

func (s *Session) isStructKey(kind, key string) bool {
	sk, _, _ := s.MemberSets(kind)
	return sk[key] || sk[strings.ToLower(key)]
}

// vocabRole names an engine token's role. The declared type system wins over
// the harvested vocabulary: it distinguishes effects, triggers and scope links,
// which a flat name list cannot.
func (s *Session) vocabRole(word string) string {
	if _, role, ok := s.engineToken(word); ok {
		return role
	}
	if l, ok := s.scopeLink(word); ok && l.Out != "" {
		return "scope_link"
	}
	// A modifier is declared too. Without this it only hovered where the install
	// happened to also define it as a row (EU5's modifier_type_definitions), so
	// the same token in a mod's own file produced no card at all.
	if _, ok := s.modifierToken(word); ok {
		return "modifier"
	}
	_, ef, tr := s.MemberSets("")
	if ef[word] || ef[strings.ToLower(word)] {
		return "effect"
	}
	if tr[word] || tr[strings.ToLower(word)] {
		return "trigger"
	}
	return ""
}

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
	// One number is not a distribution, and a block of zeroes has no share.
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

// maxAcceptedValues bounds the enumeration on a card. A closed set is the point;
// a hundred of them is a wall of text that teaches nothing.
const maxAcceptedValues = 8

// fieldAccepts states what a field's value may be, in one line.
//
// This is the answer for the fields that have no prose anywhere — 28% of them
// on EU5 — where the card previously borrowed the enclosing kind's description
// and said something misleading instead. What a field accepts is often knowable
// even when nothing documents what it means.
//
// The order is the same invariant lsp/valueSources follows for completion, and
// for the same reason: declared beats derived, so a slot the game types is not
// described by whatever PMT inferred about it. The two are separate lists
// because one produces prose and the other produces items; they must not
// disagree about precedence.
func (s *Session) fieldAccepts(kind, field string) string {
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
	// A field holding a localization key takes any key, not one of a set. The
	// enum harvested for it is just every key that field has ever held, which
	// for an event's `desc` is the descriptions of every unrelated event in the
	// install — accurate, and useless.
	if loc.Classify(field) != loc.PropNone {
		return "accepts a localization key"
	}
	// A closed set is the most useful thing of all when it really is closed. A
	// set that does not fit on the card is not an enumeration, it is a sample of
	// a free-form field, and listing eight of forty-one values suggests a
	// constraint that does not exist.
	enums := s.FieldEnums(kind, field)
	if len(enums) == 0 || len(enums) > maxAcceptedValues {
		return ""
	}
	return "one of: " + strings.Join(enums, ", ")
}

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
func (s *Session) segmentIdentity(path, seg string) (identity, bool) {
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
