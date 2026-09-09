// derive.go works out what the install implies but never states: fire keys,
// nested databases, setup wrappers, field value kinds, loc conventions, and —
// only when script_docs is absent — prefixes and engine tokens. Everything the
// game declares outright comes from schema.go instead.
//
// These derivations resolve conflicts by dropping, so a single odd usage
// anywhere in the corpus silently deletes a fact. That is tolerable for the
// conventions left here and was not tolerable for the type system, which is why
// the type system is now read rather than inferred.
//
// Scan persists the results; BuildIndex merges mod overlays on top.

package catalog

import (
	"cmp"
	"maps"
	"slices"
	"strings"
	"sync"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/parser/jomini"
	"paradox-modding-tools/services/internal/parser/loc"
)

// Derived is the in-memory overlay ExtractParsed / extractUses consume.
// PrefixKinds and KindScope normally come from bindKinds joining the game's
// declared types to the harvested databases; the rest is inferred from the
// corpus. PrefixKinds falls back to inference only without script_docs.
type Derived struct {
	PrefixKinds     map[string]string
	KindScope       map[string]string
	FireKeys        map[string]string
	NestedShapes    []NestedShape
	Wrappers        map[string]bool
	FieldValueKinds map[string]string
	LocAffixes      map[string][]LocAffix
	LocFields       map[string]bool
	LocListFields   map[string]bool
}

// derivedFromCache copies the persisted derived maps so a live reindex can
// overlay one file onto them. Nil cache yields empty derived.
func derivedFromCache(c *VanillaCache) *Derived {
	v := &Derived{
		PrefixKinds:     map[string]string{},
		KindScope:       map[string]string{},
		FireKeys:        map[string]string{},
		Wrappers:        map[string]bool{},
		FieldValueKinds: map[string]string{},
		LocAffixes:      map[string][]LocAffix{},
		LocFields:       map[string]bool{},
		LocListFields:   map[string]bool{},
	}
	if c == nil {
		return v
	}
	maps.Copy(v.KindScope, c.KindScope)
	maps.Copy(v.PrefixKinds, c.PrefixKinds)
	maps.Copy(v.FireKeys, c.FireKeys)
	maps.Copy(v.FieldValueKinds, c.FieldValueKinds)
	maps.Copy(v.LocAffixes, c.LocAffixes)
	for _, f := range c.LocFields {
		v.LocFields[f] = true
	}
	for _, f := range c.LocListFields {
		v.LocListFields[f] = true
	}
	for _, w := range c.Wrappers {
		v.Wrappers[w] = true
	}
	v.NestedShapes = slices.Clone(c.NestedShapes)
	return v
}

func (v *Derived) fireKind(key string) string {
	if v == nil || loc.Classify(key) != loc.PropNone {
		return ""
	}
	if k := v.FireKeys[key]; k != "" {
		return k
	}
	return v.FireKeys[strings.ToLower(key)]
}

func (v *Derived) fieldKind(key string) string {
	if v == nil {
		return ""
	}
	if k := v.FieldValueKinds[key]; k != "" {
		return k
	}
	return v.FieldValueKinds[strings.ToLower(key)]
}

func (v *Derived) locAffixes(kind string) []LocAffix {
	if v == nil {
		return nil
	}
	if a := v.LocAffixes[kind]; len(a) > 0 {
		return a
	}
	return v.LocAffixes[jomini.CanonicalKind(kind)]
}

// LocAffix is one derived localization naming convention for a kind: the key
// for a definition of that kind is Pre + <def id> + Suf. Both halves may be
// empty, which is the convention that a definition's key is simply its id.
type LocAffix struct {
	Pre  string `json:"pre,omitempty"`
	Suf  string `json:"suf,omitempty"`
	Defs int    `json:"defs"` // definitions of the kind that carry this key
	Of   int    `json:"of"`   // definitions of the kind in the install
}

// Coverage is the share of the kind's definitions carrying this key, percent.
//
// Consumers pick their own floor, because the two questions asked of a
// convention are not equally demanding: deciding a key is not orphaned only
// needs the convention to be plausible, while telling a modder a key is missing
// asserts the game requires it.
func (a LocAffix) Coverage() int {
	if a.Of == 0 {
		return 0
	}
	return a.Defs * 100 / a.Of
}

// Key applies the convention to a definition id.
func (a LocAffix) Key(id string) string { return a.Pre + id + a.Suf }

const (
	// minAffixDefs is how many definitions must share a shape before it is a
	// convention rather than a coincidence. Same rule as minFieldHits and
	// minOptionRefs: a ratio with no floor lets one accident decide.
	minAffixDefs = 8
	// minAffixCoverage is the share of a kind that must carry it.
	minAffixCoverage = 20
	// minAffixDefsStrong admits a convention that covers only a slice of its
	// kind but is shared by a great many definitions anyway.
	//
	// Coverage alone cannot see these. CK3 portrait modifiers are keyed
	// `PORTRAIT_MODIFIER_custom_clothes_<accessory>`, and there is one such
	// prefix per portrait group — clothes, headgear, legwear, hair, cloaks,
	// jewelry — so each covers roughly a tenth of the `accessories` kind and
	// none reaches a fifth. They were 994 of A Game of Thrones' uncited orphans.
	// A shape 410 distinct definitions share is not a coincidence whatever
	// fraction of its kind that happens to be.
	minAffixDefsStrong = 50
	// maxAffixesPerKind bounds what one kind contributes. State regions produce
	// a convention per hub type and accessories one per portrait group, so the
	// cap has to clear a real fan-out before it starts discarding evidence.
	maxAffixesPerKind = 16
	// affixKeyTokens caps span enumeration per key. Spans are quadratic in
	// tokens and a twenty-token key is a sentence, not a naming convention.
	affixKeyTokens = 10

	// LocConventionUsed is the floor for "the engine may consume this key" —
	// what suppressing a false "orphaned" row needs.
	LocConventionUsed = minAffixCoverage
	// LocConventionStored is the floor for expanding a convention forwards into
	// stored references, which is what makes the key navigable and countable as
	// used without a reverse lookup. Kept separate from LocConventionRequired
	// because "worth storing" and "the game demands it" are different claims;
	// while they shared a constant, tightening the diagnostic silently changed
	// which keys counted as used.
	LocConventionStored = 90
	// LocConventionRequired is the floor for "the game expects this key", which
	// is a claim strong enough to put a warning in a modder's editor.
	//
	// It is 100 because nothing less passes the gate the diagnostics are held
	// to: zero findings on vanilla. Vanilla is complete by construction, so for
	// each convention `Of - Defs` is exactly how many vanilla definitions the
	// check would warn about. Measured:
	//
	//	floor  CK3 conventions / vanilla warnings   Vic3        EU5
	//	  90       197 / 475                     164 / 119   301 / 270
	//	  95       175 / 230                     160 /  55   287 / 224
	//	  99       137 /  16                     143 /  13   253 /  71
	//	 100       126 /   0                     135 /   0   244 /   0
	//
	// The cost is 19-36% of the conventions and it buys away every false
	// positive. A shape nine definitions in ten share is a habit: EU5 gives 90%
	// of diplomatic actions a `WE_PERFORM_<id>_ACTION_BTN3` key, and an action
	// with two buttons is not defective.
	LocConventionRequired = 100
)

// ConventionKeys expands a kind's derived conventions for one definition,
// keeping those that hold for at least minCoverage percent of the kind.
func ConventionKeys(id string, affixes []LocAffix, minCoverage int) []string {
	if id == "" || len(affixes) == 0 {
		return nil
	}
	out := make([]string, 0, len(affixes))
	for _, a := range affixes {
		if a.Coverage() >= minCoverage {
			out = append(out, a.Key(id))
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// ConventionOwner reports the definition a convention key names: the key
// decomposes as <pre><id><suf> where pre/suf is a convention of a kind that
// defines id. eachDefKind visits the kinds that define a given id.
//
// This is the reverse of ConventionKeys and it is a lookup rather than stored
// references on purpose. Expanding forwards would mean storing one reference
// per definition per convention — hundreds of thousands on a large install — to
// answer a question that is only ever asked of the few keys that look orphaned.
// Cheap discriminator first: decide before you materialise.
//
// eachDefKind is a visitor rather than a `[]string` return because it is called
// for every span of every candidate key. Handing back a slice would allocate
// once per definition found, on a path that runs tens of thousands of times
// during one Workspace Health pass.
func ConventionOwner(
	affixes map[string][]LocAffix, key string,
	eachDefKind func(id string, visit func(kind string) bool),
) (id, kind string, ok bool) {
	if key == "" || len(affixes) == 0 {
		return "", "", false
	}
	// A key past the token cap is rejected anyway, so the offsets of one that is
	// not fit in a fixed buffer and cost nothing.
	var buf [affixKeyTokens + 2]int
	offs := tokenOffsets(key, buf[:0])
	if len(offs) > affixKeyTokens+1 {
		return "", "", false
	}
	// One closure for the whole call, reading pre/suf/hit rather than capturing
	// them fresh. Built inside the span loop it was allocated once per span —
	// twenty-odd allocations to answer one key, on a path that runs for every
	// key that survived every other orphan filter.
	var pre, suf, hit string
	visit := func(k string) bool {
		for _, a := range affixes[jomini.CanonicalKind(k)] {
			if a.Pre == pre && a.Suf == suf {
				hit = k
				return false
			}
		}
		return true
	}
	// Longest span first: the definition id is the largest part of the key, and
	// a shorter span that also happens to name something would be the wrong
	// owner to report.
	for n := len(offs) - 1; n > 0; n-- {
		for i := 0; i+n < len(offs); i++ {
			j := i + n
			span := key[offs[i] : offs[j]-1]
			pre, suf, hit = key[:offs[i]], key[offs[j]-1:], ""
			eachDefKind(span, visit)
			if hit != "" {
				return span, hit, true
			}
		}
	}
	return "", "", false
}

func (v *Derived) mergeLocal(local *Derived) {
	if local == nil {
		return
	}
	v.PrefixKinds = mergeStringMap(v.PrefixKinds, local.PrefixKinds)
	// KindScope travels with PrefixKinds: one bindKinds call produces both, and
	// they are written on adjacent lines at the cache. Dropping it here left a
	// cache with PrefixKinds populated and KindScope empty whenever `rebind`
	// took one of its early returns — and session/scope.go treats an empty
	// KindScope as a silent fallback, so scope checking switched itself off with
	// no error anywhere.
	v.KindScope = mergeStringMap(v.KindScope, local.KindScope)
	v.FireKeys = mergeStringMap(v.FireKeys, local.FireKeys)
	v.FieldValueKinds = mergeStringMap(v.FieldValueKinds, local.FieldValueKinds)
	for kind, list := range local.LocAffixes {
		if len(list) > 0 {
			v.LocAffixes[kind] = list
		}
	}
	for f := range local.LocFields {
		v.LocFields[f] = true
	}
	for f := range local.LocListFields {
		v.LocListFields[f] = true
	}
	for k, ok := range local.Wrappers {
		if ok {
			v.Wrappers[k] = true
		}
	}
	v.NestedShapes = mergeShapes(v.NestedShapes, local.NestedShapes)
}

// mergeStringMap overlays mods on vanilla; a value conflict drops the key.
func mergeStringMap(vanilla, mods map[string]string) map[string]string {
	if len(vanilla) == 0 && len(mods) == 0 {
		return nil
	}
	out := maps.Clone(vanilla)
	if out == nil {
		out = map[string]string{}
	}
	for k, v := range mods {
		if prev, ok := out[k]; ok && prev != v {
			delete(out, k)
			continue
		}
		out[k] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func mergeShapes(a, b []NestedShape) []NestedShape {
	if len(a) == 0 {
		return slices.Clone(b)
	}
	seen := map[string]bool{}
	var out []NestedShape
	add := func(s NestedShape) {
		k := s.ParentKind + "\x00" + s.ChildKind + "\x00" + s.GroupKey
		if seen[k] {
			return
		}
		seen[k] = true
		out = append(out, s)
	}
	for _, s := range a {
		add(s)
	}
	for _, s := range b {
		add(s)
	}
	return out
}

func collectFieldCites(root *jomini.Root) []typedCite {
	if root == nil {
		return nil
	}
	var out []typedCite
	jomini.Walk(root, func(st jomini.Statement, _ int, _ *jomini.Block) bool {
		a, ok := st.(*jomini.Assignment)
		if !ok || a.Key.Quoted {
			return true
		}
		sc, ok := a.Value.(*jomini.Scalar)
		if !ok || sc.Quoted || sc.Text == "" || jomini.SkipObjectRHS(sc.Text) {
			return true
		}
		if _, _, ok := game.ParseTyped("", sc.Text); ok {
			return true
		}
		if _, ok := jomini.ParsePrefixed(sc.Text); ok {
			return true
		}
		if !jomini.IsDefName(sc.Text) || jomini.IsStop(sc.Text) {
			return true
		}
		out = append(out, typedCite{prefix: a.Key.Text, id: sc.Text})
		return true
	})
	return out
}

// fireCount is the evidence for one assignment key: how often its RHS named an
// event or on_action, against how often it named anything at all. The
// denominator is what stops a key being called a fire site on one coincidence.
type fireCount struct {
	kinds map[string]int
	named int
}

// deriveFireKeys counts, per assignment key, how many of the names on its RHS
// are harvested event or on_action ids (or the event-id form `ns.1`).
func deriveFireKeys(root *jomini.Root, eventIDs, onActionIDs map[string]bool) map[string]*fireCount {
	if root == nil {
		return nil
	}
	out := map[string]*fireCount{}
	var walk func([]jomini.Statement)
	walk = func(stmts []jomini.Statement) {
		for _, st := range stmts {
			a, ok := st.(*jomini.Assignment)
			if !ok {
				if vs, ok := st.(*jomini.ValueStmt); ok {
					if b := jomini.BlockOf(vs.Value); b != nil {
						walk(b.Statements)
					}
				}
				continue
			}
			if !a.Key.Quoted && !jomini.IsStop(a.Key.Text) &&
				loc.Classify(a.Key.Text) == loc.PropNone &&
				!strings.EqualFold(a.Key.Text, "id") {
				for _, to := range fireTargets(a.Value) {
					m := out[a.Key.Text]
					if m == nil {
						m = &fireCount{kinds: map[string]int{}}
						out[a.Key.Text] = m
					}
					m.named++
					if kind := classifyFireTarget(to, eventIDs, onActionIDs); kind != "" {
						m.kinds[kind]++
					}
				}
			}
			if b := jomini.BlockOf(a.Value); b != nil {
				walk(b.Statements)
			}
		}
	}
	walk(root.Statements)
	return out
}

// fireTargets is the assignment's own RHS: a scalar, list values, or
// inner id=/digit keys. Nested fire sites stay on their own keys.
func fireTargets(v jomini.Value) []string {
	var out []string
	if sc, ok := v.(*jomini.Scalar); ok {
		if !sc.Quoted && fireTargetOK(sc.Text) {
			out = append(out, sc.Text)
		}
		return out
	}
	b := jomini.BlockOf(v)
	if b == nil {
		return out
	}
	for _, st := range b.Statements {
		switch n := st.(type) {
		case *jomini.ValueStmt:
			if sc, ok := n.Value.(*jomini.Scalar); ok && !sc.Quoted &&
				fireTargetOK(sc.Text) {
				out = append(out, sc.Text)
			}
		case *jomini.Assignment:
			key := strings.ToLower(n.Key.Text)
			if sc, ok := n.Value.(*jomini.Scalar); ok && !sc.Quoted &&
				fireTargetOK(sc.Text) && (key == "id" || isDigitKey(key)) {
				out = append(out, sc.Text)
			}
		}
	}
	return out
}

func classifyFireTarget(s string, eventIDs, onActionIDs map[string]bool) string {
	if eventIDs[s] || jomini.IsEventID(s) {
		return "event"
	}
	if onActionIDs[s] {
		return "on_action"
	}
	return ""
}

const (
	// fireKeyCoverage is the share of a key's named RHS values that must be
	// events or on_actions before the key is treated as firing them.
	//
	// Unanimity alone was not enough, the same flaw the field typing had: names
	// that matched nothing were not counted, so one coincidence made a key a
	// fire site and every later use of it emitted a reference that could never
	// resolve. On one real Vic3 mod that produced 4,033 false dangling rows
	// under kind `on_action`, for keys like `alert` and `character`.
	fireKeyCoverage = 50
	// minFireNames is how many names a key must carry before coverage is worth
	// consulting at all.
	//
	// This matters more here than elsewhere, because for fire keys a *low* hit
	// rate is often the very thing worth reporting: a mod whose `trigger_event`
	// targets mostly do not exist has broken references, and silencing the key
	// would hide exactly the defect Workspace Health is for. Coverage may only
	// veto a key once there is enough evidence that a low rate means "this is
	// not a fire key" rather than "this mod is broken".
	minFireNames = 8
)

func resolveFireKinds(counts map[string]*fireCount) map[string]string {
	out := map[string]string{}
	for key, c := range counts {
		if c == nil || len(c.kinds) != 1 {
			continue
		}
		hits := 0
		for _, n := range c.kinds {
			hits += n
		}
		if c.named >= minFireNames && hits*100 < c.named*fireKeyCoverage {
			continue
		}
		for k := range c.kinds {
			out[key] = k
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func mergeFireCounts(dst, src map[string]*fireCount) map[string]*fireCount {
	if dst == nil {
		dst = map[string]*fireCount{}
	}
	for key, c := range src {
		m := dst[key]
		if m == nil {
			m = &fireCount{kinds: map[string]int{}}
			dst[key] = m
		}
		m.named += c.named
		for k, n := range c.kinds {
			m.kinds[k] += n
		}
	}
	return dst
}

func eventOnActionIDs(defs []Def) (events, onActions map[string]bool) {
	events, onActions = map[string]bool{}, map[string]bool{}
	for _, d := range defs {
		switch jomini.CanonicalKind(d.Kind) {
		case "event":
			events[d.Key] = true
		case "on_action":
			onActions[d.Key] = true
		}
	}
	return events, onActions
}
func nestedChildKind(gameID, prefix, id string, prefixKinds map[string]string) string {
	if prefixKinds != nil {
		if k := prefixKinds[prefix]; k != "" {
			return k
		}
	}
	if k, _, ok := game.ParseTyped(gameID, prefix+":"+id); ok {
		return k
	}
	return jomini.CanonicalKind(prefix)
}

func addNestedCite(
	dst map[string][]string, defKeys map[string]bool, gameID string,
	c typedCite, prefixKinds map[string]string,
) {
	if c.id == "" || defKeys[c.id] || jomini.IsStop(c.id) {
		return
	}
	childKind := nestedChildKind(gameID, c.prefix, c.id, prefixKinds)
	if childKind == "" || childKind == "type" {
		return
	}
	for _, have := range dst[c.id] {
		if have == childKind {
			return
		}
	}
	dst[c.id] = append(dst[c.id], childKind)
}

// walkNestedWanted visits nested object keys (depth > 0) that are in want.
func walkNestedWanted(
	gameID, rel string, root *jomini.Root, want map[string][]string,
	fn func(parentKind, groupKey, childKey, childKind string),
) {
	if root == nil || len(want) == 0 {
		return
	}
	parentKind := game.MatchExtract(gameID, rel).Kind
	var walk func(stmts []jomini.Statement, wrapper string, depth int)
	walk = func(stmts []jomini.Statement, wrapper string, depth int) {
		for _, st := range stmts {
			a, ok := st.(*jomini.Assignment)
			if !ok {
				if vs, ok := st.(*jomini.ValueStmt); ok {
					if b := jomini.BlockOf(vs.Value); b != nil {
						walk(b.Statements, wrapper, depth+1)
					}
				}
				continue
			}
			name := game.KeyIdentity(gameID, a.Key.Text)
			b := jomini.BlockOf(a.Value)
			if b != nil && depth > 0 {
				for _, ck := range want[name] {
					fn(parentKind, wrapper, name, ck)
				}
			}
			if b != nil {
				next := wrapper
				if depth == 0 {
					next = ""
				} else if wrapper == "" {
					next = name
				}
				walk(b.Statements, next, depth+1)
			}
		}
	}
	walk(root.Statements, "", 0)
}

// deriveNestedShapes records parent→child harvest patterns from unresolved cites.
// One CST walk per file (index unresolved ids, then visit matching nested keys).
// The child kind must be something the game declares as a type. Without that
// test the pass harvests any repeated inner key as a database: on CK3 it minted
// 310,044 defs of kinds named `SKILL`, `TIER`, `NUMBER` and `$SKILL$` — script
// *parameter* names — which was 64% of the entire model. EU5 produced 35,458
// under `CALL_FOR_PEACE_WARSCORE_LIMIT` and `var:chinese_expedition_opinion`.
//
// Filtering on Schema.Declares leaves CK3 6 shapes, Vic3 4, EU5 15 — including
// `religion_types -> faith`, the case this pass exists for. Anything dropped
// either was not an object at all, or already has its own folder and needs no
// nested harvest (EU5 `production_methods` is `in_game/common/production_methods`).
func deriveNestedShapes(
	gameID string, corp corpus, defs []Def,
	prefixKinds map[string]string, schema *Schema,
	cites, fieldCites []typedCite,
) []NestedShape {
	defKeys := defKeySet(defs)
	idChild := map[string][]string{}
	for _, c := range cites {
		addNestedCite(idChild, defKeys, gameID, c, prefixKinds)
	}
	for _, c := range fieldCites {
		addNestedCite(idChild, defKeys, gameID, c, prefixKinds)
	}
	if len(idChild) == 0 {
		return nil
	}
	type acc struct {
		parent, group string
		ok, clash     bool
		letters       map[byte]bool
	}
	byChild := map[string]*acc{}
	// A second walk: where those ids sit is only answerable once every citation
	// is known, so this cannot fold into the pass above.
	var mu sync.Mutex
	_ = corp.walk(func(f fileRef, res jomini.Result) error {
		mu.Lock()
		defer mu.Unlock()
		walkNestedWanted(gameID, f.rel, res.Root, idChild,
			func(parent, group, childKey, childKind string) {
				a := byChild[childKind]
				if a == nil {
					a = &acc{
						parent: parent, group: group, ok: true,
						letters: map[byte]bool{},
					}
					byChild[childKind] = a
				}
				if a.parent != parent {
					a.clash = true
				}
				if a.group != group {
					a.group = ""
				}
				if a.letters != nil && len(childKey) > 2 && childKey[1] == '_' {
					a.letters[childKey[0]] = true
				} else if a.letters != nil {
					a.letters = nil
				}
			})
		return nil
	})
	var out []NestedShape
	for child, a := range byChild {
		if !a.ok || a.clash || a.parent == "" {
			continue
		}
		s := NestedShape{ParentKind: a.parent, ChildKind: child, GroupKey: a.group}
		if len(a.letters) > 0 {
			var b strings.Builder
			keys := slices.Collect(maps.Keys(a.letters))
			slices.Sort(keys)
			for _, c := range keys {
				b.WriteByte(c)
			}
			s.KeyPrefix = b.String()
		}
		if !schema.Declares(s.ChildKind) {
			continue
		}
		out = append(out, s)
	}
	slices.SortFunc(out, func(a, b NestedShape) int {
		if a.ParentKind != b.ParentKind {
			return strings.Compare(a.ParentKind, b.ParentKind)
		}
		return strings.Compare(a.ChildKind, b.ChildKind)
	})
	return out
}

func keyPrefixOK(key, prefixSet string) bool {
	if prefixSet == "" || len(key) < 3 || key[1] != '_' {
		return prefixSet == ""
	}
	return strings.IndexByte(prefixSet, key[0]) >= 0
}

func looksLikeObject(v jomini.Value) bool {
	b := jomini.BlockOf(v)
	if b == nil {
		return false
	}
	if len(b.Statements) == 0 {
		return true
	}
	for _, st := range b.Statements {
		if _, ok := st.(*jomini.Assignment); ok {
			return true
		}
	}
	return false
}

func hasCitedDescendant(gameID string, b *jomini.Block, cited map[string]bool) bool {
	if b == nil {
		return false
	}
	for _, st := range b.Statements {
		a, ok := st.(*jomini.Assignment)
		if !ok {
			continue
		}
		name := game.KeyIdentity(gameID, a.Key.Text)
		if cited[name] {
			return true
		}
		if inner := jomini.BlockOf(a.Value); inner != nil &&
			hasCitedDescendant(gameID, inner, cited) {
			return true
		}
	}
	return false
}

// applyNestedShape harvests child defs from an already-parsed parent file.
func applyNestedShape(
	gameID, path, origin string, root *jomini.Root, li *jomini.LineIndex,
	shape NestedShape, cited map[string]bool,
) []Def {
	if root == nil {
		return nil
	}
	if shape.IsOption {
		// An option database knows its own rows by name, and its rows are not
		// all blocks — a cultural parameter is `name = yes`. It gets its own
		// walk rather than bending the citation rules below.
		return applyOptionShape(gameID, path, origin, root, li, shape)
	}
	var defs []Def
	add := func(a *jomini.Assignment) {
		name := game.KeyIdentity(gameID, a.Key.Text)
		if !jomini.IsDefName(name) || jomini.IsStop(name) {
			return
		}
		if !looksLikeObject(a.Value) {
			return
		}
		d := makeDef(shape.ChildKind, name, path, origin, a.Key.Range, li)
		d.Nested = true
		defs = append(defs, d)
	}
	if shape.GroupKey != "" {
		var walk func([]jomini.Statement)
		walk = func(stmts []jomini.Statement) {
			for _, st := range stmts {
				a, ok := st.(*jomini.Assignment)
				if !ok {
					if vs, ok := st.(*jomini.ValueStmt); ok {
						if b := jomini.BlockOf(vs.Value); b != nil {
							walk(b.Statements)
						}
					}
					continue
				}
				if strings.EqualFold(a.Key.Text, shape.GroupKey) {
					if b := jomini.BlockOf(a.Value); b != nil {
						for _, inner := range b.Statements {
							ia, ok := inner.(*jomini.Assignment)
							if ok && !ia.Key.Quoted {
								add(ia)
							}
						}
					}
				}
				if b := jomini.BlockOf(a.Value); b != nil {
					walk(b.Statements)
				}
			}
		}
		walk(root.Statements)
		return defs
	}
	var walk func(stmts []jomini.Statement, depth int)
	walk = func(stmts []jomini.Statement, depth int) {
		for _, st := range stmts {
			a, ok := st.(*jomini.Assignment)
			if !ok {
				if vs, ok := st.(*jomini.ValueStmt); ok {
					if b := jomini.BlockOf(vs.Value); b != nil {
						walk(b.Statements, depth+1)
					}
				}
				continue
			}
			b := jomini.BlockOf(a.Value)
			if b != nil && depth > 0 {
				name := game.KeyIdentity(gameID, a.Key.Text)
				okKey := keyPrefixOK(name, shape.KeyPrefix)
				if shape.KeyPrefix != "" {
					if okKey {
						add(a)
					}
				} else if cited[name] || hasCitedDescendant(gameID, b, cited) {
					add(a)
				}
				walk(b.Statements, depth+1)
				continue
			}
			if b != nil {
				walk(b.Statements, depth+1)
			}
		}
	}
	walk(root.Statements, 0)
	return defs
}

func citedIDsForKind(cites []typedCite, prefixKinds map[string]string, childKind string) map[string]bool {
	out := map[string]bool{}
	ck := jomini.CanonicalKind(childKind)
	for _, c := range cites {
		k := ""
		if prefixKinds != nil {
			k = prefixKinds[c.prefix]
		}
		if k == "" {
			k = jomini.CanonicalKind(c.prefix)
		}
		if jomini.CanonicalKind(k) == ck {
			out[c.id] = true
		}
	}
	return out
}

// deriveWrappers marks top-level blocks whose inners are typed cites to existing
// defs (Vic3 COUNTRIES = { c:USA ?= { } }). Callers drop defs with these keys,
// so a false positive deletes a real object from the model.
//
// A `keyword name = { }` pair is never a wrapper, however its body is shaped.
// CK3 declares inline macros that way — `scripted_effect foo = { culture:x = {…} }`
// — and matching them on shape alone deleted 28 vanilla macros from the CK3
// model, breaking go-to-definition on every one. The keyword states what the
// block is, so the declaration wins over the shape.
func deriveWrappers(gameID, rel string, root *jomini.Root, defKeys map[string]bool) []string {
	if root == nil {
		return nil
	}
	// A folder that declares named script bodies has already said what its
	// top-level keys are, and those bodies are routinely a single typed cite
	// (`title:h_china.holder ?= { … }`) — the wrapper shape exactly. Running the
	// heuristic there deleted 24 CK3 and 20 EU5 vanilla macros from the model.
	if jomini.DeclaresNamedBodies(game.MatchExtract(gameID, rel).Kind) {
		return nil
	}
	var out []string
	for i, st := range root.Statements {
		a, ok := st.(*jomini.Assignment)
		if !ok || a.Key.Quoted {
			continue
		}
		if keywordDeclared(root.Statements, i) {
			continue
		}
		b := jomini.BlockOf(a.Value)
		if b == nil {
			continue
		}
		any, all := false, true
		for _, inner := range b.Statements {
			ia, ok := inner.(*jomini.Assignment)
			if !ok || ia.Key.Quoted {
				continue
			}
			any = true
			kind, id, ok := game.ParseTyped(gameID, ia.Key.Text)
			if !ok {
				if sp, sok := jomini.TypedSpanAt(ia.Key.Text, 0); sok {
					kind, id, ok = game.ParseTyped(gameID, sp.Prefix+":"+sp.ID)
				}
			}
			if !ok || kind == "" || !defKeys[id] {
				all = false
				break
			}
		}
		if any && all {
			out = append(out, a.Key.Text)
		}
	}
	return out
}

// keywordDeclared reports that stmts[i] is the named half of a `keyword name = { }`
// pair, the form extractKeywordName harvests as a def. This is the same pairing
// rule, read from the same statement list, so the two cannot disagree.
func keywordDeclared(stmts []jomini.Statement, i int) bool {
	if i == 0 {
		return false
	}
	mv, ok := stmts[i-1].(*jomini.ValueStmt)
	if !ok {
		return false
	}
	sc, ok := mv.Value.(*jomini.Scalar)
	if !ok || sc.Quoted {
		return false
	}
	return game.DeclKeywordKind(sc.Text) != ""
}

// defKeySet is the set of names already spoken for by a real definition. Passes
// that invent a database from unexplained citations subtract it first, so a name
// with a home is never handed a second one.
func defKeySet(defs []Def) map[string]bool {
	out := map[string]bool{}
	for _, d := range defs {
		if IsCitableDef(d) {
			out[d.Key] = true
		}
	}
	return out
}

func dropWrapperDefs(defs []Def, wrappers map[string]bool) []Def {
	if len(wrappers) == 0 {
		return defs
	}
	out := defs[:0]
	for _, d := range defs {
		if wrappers[d.Key] || wrappers[strings.ToLower(d.Key)] {
			continue
		}
		out = append(out, d)
	}
	return out
}

// deriveLocAffixes reads the install's own localization to work out how each
// kind names its keys.
//
// It replaced a three-way vote between `id`, `id_desc` and `kind_id`. Those are
// three real conventions, but they are three of many — Victoria 3 alone writes
// `ACHIEVEMENT_<id>`, `ACHIEVEMENT_DESC_<id>`, `notification_<id>_tooltip`,
// `HUB_NAME_<state>_farm`, `SINGULAR_DEMONYM_<culture>` — and picking one
// winner per kind left every other key for that kind looking defined-but-unused.
// Nothing about those shapes is guessable, which is exactly why they are read
// from the corpus rather than listed in code: a game update that invents a new
// one is picked up by the next scan.
//
// The method is the same as every other derivation here. Take each localization
// key, find the spans of it that name a definition, and record the surrounding
// prefix and suffix as a candidate convention for that definition's kind. A
// candidate becomes a convention when enough distinct definitions share it.
func deriveLocAffixes(defs []Def, locKeys map[string]bool) map[string][]LocAffix {
	if len(locKeys) == 0 || len(defs) == 0 {
		return nil
	}
	kindsOf := make(map[string][]string, len(defs))
	seen := map[string]map[string]bool{}
	total := map[string]int{}
	for _, d := range defs {
		k := jomini.CanonicalKind(d.Kind)
		if d.Key == "" || !jomini.IsObjectKind(k) {
			continue
		}
		ids := seen[k]
		if ids == nil {
			ids = map[string]bool{}
			seen[k] = ids
		}
		if !ids[d.Key] {
			ids[d.Key] = true
			total[k]++
		}
		if !slices.Contains(kindsOf[d.Key], k) {
			kindsOf[d.Key] = append(kindsOf[d.Key], k)
		}
	}
	return affixesFrom(kindsOf, total, locKeys)
}

// affixesFrom is the derivation itself: given names that belong to a kind, and
// how many names each kind has, find the prefix/suffix shapes that turn enough
// of them into real localization keys.
// LocKeyAffix is a loc key shape derived from another loc key rather than from
// a definition. EU5 writes NOT_<key> for the negated form of a trigger tooltip,
// so NOT_POP_LITERACY_TRIGGER is consumed by the engine even though nothing
// cites it and no definition is named POP_LITERACY_TRIGGER — the stem is
// itself a key. Vanilla EU5 has 69,565 keys reading as orphaned and this family
// is its largest cluster; see GAME-SYNTAX §12.
type LocKeyAffix struct {
	Pre string `json:"pre,omitempty"`
	Suf string `json:"suf,omitempty"`
	// Keys carrying this shape whose stem is itself a loc key, out of Of, every
	// key carrying it. The ratio is the evidence: a real derivation resolves
	// nearly always, a coincidence resolves sometimes.
	Keys int `json:"keys"`
	Of   int `json:"of"`
}

// maxAffixStrip bounds how many whole tokens may be taken off each end when
// looking for a stem. Two covers OTHER_PERFORMS_<key> and <key>_option_desc;
// going wider costs a multiple of the key set per token for shapes no game was
// observed to use.
const maxAffixStrip = 2

// deriveLocKeyAffixes finds the shapes that build one loc key out of another.
//
// Linear in the key set rather than quadratic like affixesFrom: the stem of a
// derived key is the key minus whole tokens off each end, so a bounded strip
// finds it directly and there is no need to enumerate every span. Both counts
// are collected here and the floor is applied in acceptLocKeyAffix, so the
// evidence and the judgement stay separable.
func deriveLocKeyAffixes(locKeys map[string]bool) []LocKeyAffix {
	if len(locKeys) == 0 {
		return nil
	}
	type shape struct{ pre, suf string }
	hits := map[shape]int{}
	total := map[shape]int{}
	var offs []int
	for key := range locKeys {
		offs = tokenOffsets(key, offs[:0])
		n := len(offs) - 1 // tokens in key
		if n < 2 || len(offs) > affixKeyTokens+1 {
			continue
		}
		for pre := 0; pre <= maxAffixStrip && pre < n; pre++ {
			for suf := 0; suf+pre <= maxAffixStrip && pre+suf < n; suf++ {
				if pre == 0 && suf == 0 {
					continue
				}
				lo, hi := offs[pre], offs[n-suf]
				sh := shape{key[:lo], key[hi-1:]}
				total[sh]++
				if locKeys[key[lo:hi-1]] {
					hits[sh]++
				}
			}
		}
	}
	out := make([]LocKeyAffix, 0, 16)
	for sh, of := range total {
		a := LocKeyAffix{Pre: sh.pre, Suf: sh.suf, Keys: hits[sh], Of: of}
		if acceptLocKeyAffix(a) {
			out = append(out, a)
		}
	}
	slices.SortFunc(out, func(a, b LocKeyAffix) int {
		if c := cmp.Compare(b.Keys, a.Keys); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Pre, b.Pre); c != 0 {
			return c
		}
		return cmp.Compare(a.Suf, b.Suf)
	})
	return out
}

const (
	// minKeyAffixKeys is how many keys must resolve through a shape before it
	// counts as a convention. Higher than minAffixDefs because the denominator
	// here is the whole key set rather than one kind's definitions, so
	// coincidences are correspondingly easier to find: CK3 has 287,896 english
	// keys, and 29,647 distinct prefix shapes among the unexplained ones alone.
	minKeyAffixKeys = 20
	// minKeyAffixCoverage is the share of keys carrying a shape whose stem is
	// itself a key. This is the evidence that does not scale with volume: a real
	// derivation resolves nearly always — every NOT_<key> has its <key> — while
	// a coincidental shape like _desc resolves only where the stem happens to
	// exist as well. Set high on purpose. Under-reporting an orphan costs a
	// modder some dead lines; a shape accepted here explains keys away, and the
	// cheapest way to lose a real finding is to accept a shape that half works.
	minKeyAffixCoverage = 80
)

// acceptLocKeyAffix decides whether one derived shape is real rather than a
// coincidence. Kept separate from the counting in deriveLocKeyAffixes so the
// evidence and the judgement can be recalibrated independently — the ratio is
// the discriminator, and the count floor stops one lucky pair clearing it the
// way body_part cleared 50% coverage on 1 of 2 values.
func acceptLocKeyAffix(a LocKeyAffix) bool {
	if a.Keys < minKeyAffixKeys || a.Of == 0 {
		return false
	}
	return a.Keys*100 >= a.Of*minKeyAffixCoverage
}

// KeyAffixOwner reports the loc key that key is derived from, if any. isKey is
// asked last because it is the only part that touches the session's own keys.
func KeyAffixOwner(
	affixes []LocKeyAffix, key string, isKey func(string) bool,
) (stem string, ok bool) {
	for _, a := range affixes {
		if len(key) <= len(a.Pre)+len(a.Suf) {
			continue
		}
		if !strings.HasPrefix(key, a.Pre) || !strings.HasSuffix(key, a.Suf) {
			continue
		}
		s := key[len(a.Pre) : len(key)-len(a.Suf)]
		if isKey(s) {
			return s, true
		}
	}
	return "", false
}

func affixesFrom(
	kindsOf map[string][]string, total map[string]int, locKeys map[string]bool,
) map[string][]LocAffix {
	if len(kindsOf) == 0 {
		return nil
	}
	// Cheap discriminator first: a span can only be one of these names if its
	// first token starts one. Without this, every key pays for every span of
	// itself.
	firstTok := make(map[string]bool, len(kindsOf))
	for id := range kindsOf {
		if i := strings.IndexByte(id, '_'); i > 0 {
			firstTok[id[:i]] = true
		} else {
			firstTok[id] = true
		}
	}

	// One count, not a set of ids: pre+id+suf reconstructs the key, so a given
	// key can contribute to one convention at most once.
	type candidate struct{ kind, pre, suf string }
	hits := map[candidate]int{}
	var offs []int
	for key := range locKeys {
		offs = tokenOffsets(key, offs[:0])
		if len(offs) > affixKeyTokens+1 {
			continue
		}
		for i := 0; i+1 < len(offs); i++ {
			if !firstTok[key[offs[i]:offs[i+1]-1]] {
				continue
			}
			for j := len(offs) - 1; j > i; j-- {
				span := key[offs[i] : offs[j]-1]
				kinds := kindsOf[span]
				if len(kinds) == 0 {
					continue
				}
				pre, suf := key[:offs[i]], key[offs[j]-1:]
				for _, k := range kinds {
					hits[candidate{k, pre, suf}]++
				}
			}
		}
	}

	out := map[string][]LocAffix{}
	for c, n := range hits {
		of := total[c.kind]
		if n < minAffixDefs || of == 0 {
			continue
		}
		if n*100 < of*minAffixCoverage && n < minAffixDefsStrong {
			continue
		}
		out[c.kind] = append(out[c.kind], LocAffix{Pre: c.pre, Suf: c.suf, Defs: n, Of: of})
	}
	for kind, list := range out {
		slices.SortFunc(list, func(a, b LocAffix) int {
			if c := cmp.Compare(b.Defs, a.Defs); c != 0 {
				return c // best evidence first
			}
			// Then the plainest shape, so a hover reading the first hit gets
			// the definition's name rather than one of its decorations.
			if c := cmp.Compare(len(a.Pre)+len(a.Suf), len(b.Pre)+len(b.Suf)); c != 0 {
				return c
			}
			if c := cmp.Compare(a.Pre, b.Pre); c != 0 {
				return c
			}
			return cmp.Compare(a.Suf, b.Suf)
		})
		if len(list) > maxAffixesPerKind {
			list = list[:maxAffixesPerKind]
		}
		out[kind] = list
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// deriveLocMemberAffixes is deriveLocAffixes for names that are block members of
// a definition rather than definitions themselves.
//
// CK3 game rules are the case that forces it. A rule is a definition and gets
// `rule_<id>`, which the definition-keyed pass already covers — 77 of vanilla's
// 78 rules. But a rule's *options* are nested one level inside it and are never
// harvested as definitions, and vanilla writes 590 `setting_<option>` /
// `setting_<option>_desc` keys for its 380 options. Nothing owned any of them,
// so every one read as an orphaned key, in vanilla and in every mod that adds a
// rule.
//
// Kept in its own map rather than merged into LocAffixes, because the two must
// not cross-apply. Member names include ordinary field names — `default`,
// `name`, `trigger` — and letting a definition-keyed convention like
// `<id>_desc` match one of those would explain away real orphans.
func deriveLocMemberAffixes(
	structures map[string][]string, locKeys map[string]bool,
) map[string][]LocAffix {
	if len(locKeys) == 0 || len(structures) == 0 {
		return nil
	}
	names := make(map[string][]string, len(structures))
	total := make(map[string]int, len(structures))
	for kind, keys := range structures {
		k := jomini.CanonicalKind(kind)
		if k == "" {
			continue
		}
		total[k] += len(keys)
		for _, key := range keys {
			if key == "" {
				continue
			}
			if !slices.Contains(names[key], k) {
				names[key] = append(names[key], k)
			}
		}
	}
	return affixesFrom(names, total, locKeys)
}

// tokenOffsets records where each underscore-delimited token of key starts,
// plus a sentinel one past the end, so a span of tokens i..j-1 is
// key[offs[i]:offs[j]-1] with no allocation. Reusing the caller's slice matters:
// this runs over every localization key in the install.
func tokenOffsets(key string, offs []int) []int {
	offs = append(offs, 0)
	for i := 0; i < len(key); i++ {
		if key[i] == '_' {
			offs = append(offs, i+1)
		}
	}
	return append(offs, len(key)+1)
}

func locKeySet(sites map[string]LocEntry) map[string]bool {
	out := map[string]bool{}
	for k := range sites {
		out[k] = true
	}
	return out
}

// harvestGetSet collects `GetX` / `SetX` data-function names used anywhere in a
// script file. It runs over the whole source of every file in the install —
// 180 MB on CK3 — so it scans for the two three-byte prefixes directly instead
// of running `\b((?:Get|Set)[A-Za-z0-9_]+)\b` over all of it, which cost 12% of
// the entire scan. Same matches, including two names in one dotted chain
// (`[GetPlayer.GetName]` yields both).
func harvestGetSet(src string) map[string]bool {
	var out map[string]bool
	for i := 0; i+3 < len(src); i++ {
		if c := src[i]; c != 'G' && c != 'S' {
			continue
		}
		if src[i+1] != 'e' || src[i+2] != 't' {
			continue
		}
		if i > 0 && isDataFnByte(src[i-1]) {
			continue // mid-word: the regex's leading \b
		}
		j := i + 3
		for j < len(src) && isDataFnByte(src[j]) {
			j++
		}
		if j == i+3 {
			continue // bare "Get"/"Set": the pattern needs at least one more byte
		}
		if out == nil {
			out = map[string]bool{}
		}
		out[src[i:j]] = true
		i = j - 1
	}
	return out
}

func isDataFnByte(c byte) bool {
	return c == '_' || (c >= '0' && c <= '9') ||
		(c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
}

// locField reports a script property the corpus shows holds localization keys,
// beyond the engine property names parser/loc lists outright.
func (v *Derived) locField(key string) bool {
	if v == nil {
		return false
	}
	return v.LocFields[key] || v.LocFields[strings.ToLower(key)]
}

// MemberConventionOwner is ConventionOwner for names that are block members of a
// definition rather than definitions themselves — `setting_normal_difficulty`
// naming an option inside a CK3 game rule.
//
// It does not enumerate spans. A member convention is always a whole-key match:
// strip the prefix and suffix, and ask whether what is left is a member of that
// kind. That is a handful of string comparisons per convention, against a set of
// conventions that is small because few kinds have one at all.
func MemberConventionOwner(
	affixes map[string][]LocAffix, key string, isMember func(kind, name string) bool,
) (name, kind string, ok bool) {
	if key == "" || len(affixes) == 0 {
		return "", "", false
	}
	for k, list := range affixes {
		for _, a := range list {
			if len(key) <= len(a.Pre)+len(a.Suf) ||
				!strings.HasPrefix(key, a.Pre) || !strings.HasSuffix(key, a.Suf) {
				continue
			}
			stem := key[len(a.Pre) : len(key)-len(a.Suf)]
			if isMember(k, stem) {
				return stem, k, true
			}
		}
	}
	return "", "", false
}

// locListField reports a property whose LIST members are localization keys —
// a culture's `male_names`, its `cadet_dynasty_names`.
func (v *Derived) locListField(key string) bool {
	if v == nil {
		return false
	}
	return v.LocListFields[key] || v.LocListFields[strings.ToLower(key)]
}
