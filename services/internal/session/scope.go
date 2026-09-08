// scope.go tracks the scope type in force at a position in script. Every
// transition it applies is declared by the game: scope links carry Input and
// Output scopes, and iterators are effects whose Supported Targets names the
// scope of each element. Nothing here is inferred from name shape.

package session

import (
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/catalog"
	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/parser/jomini"
)

// schemaLocked returns the declared type system, or nil when the user has not
// run script_docs. Callers must degrade rather than guess.
func (s *Session) schemaLocked() *catalog.Schema {
	if s.cache == nil {
		return nil
	}
	return s.cache.Schema
}

// HasSchema reports whether a declared type system is loaded.
func (s *Session) HasSchema() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return !s.schemaLocked().Empty()
}

// DeclaresEngineName reports a name the game declares as part of its engine API.
// Such a name resolves to no def, but it is not a missing object — Workspace
// Health reported CK3's own trigger `current_military_strength` as a dangling
// reference 25 times in one real mod.
func (s *Session) DeclaresEngineName(key string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.schemaLocked().HasName(key)
}

// SchemaSource says whether the type system came from live dumps, PMT's
// archive, or is absent. Used by the Workspace Health strip.
func (s *Session) SchemaSource() catalog.SchemaSource {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sc := s.schemaLocked()
	if sc.Empty() {
		return catalog.SchemaMissing
	}
	if sc.Source == "" {
		return catalog.SchemaLive
	}
	return sc.Source
}

func (s *Session) scopeOfKindLocked(kind string) string {
	if s.cache == nil || len(s.cache.KindScope) == 0 {
		return ""
	}
	if sc := s.cache.KindScope[kind]; sc != "" {
		return sc
	}
	return s.cache.KindScope[jomini.CanonicalKind(kind)]
}

// engineToken returns the declared effect or trigger named key, plus which of
// the two it is.
func (s *Session) engineToken(key string) (catalog.EngineToken, string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.schemaLocked().Token(key)
}

// modifierToken returns the declared modifier named key. EU5 documents 2,436 of
// them with a category and no prose at all, so Area is usually the only thing
// the game says about one.
func (s *Session) modifierToken(key string) (catalog.Modifier, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sc := s.schemaLocked()
	if sc == nil {
		return catalog.Modifier{}, false
	}
	m, ok := sc.Modifiers[strings.ToLower(key)]
	return m, ok
}

// scopeLink returns the declared scope link named key.
func (s *Session) scopeLink(key string) (catalog.ScopeLink, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sc := s.schemaLocked()
	if sc == nil {
		return catalog.ScopeLink{}, false
	}
	l, ok := sc.Links[strings.ToLower(key)]
	return l, ok
}

// ScopeAt returns the scope type in force at (line, UTF-8 column), or "" when
// it cannot be determined. An empty result means "do not filter", never
// "nothing is valid".
func (s *Session) ScopeAt(path string, line, col int) string {
	res := s.Parsed(path)
	if res.Root == nil {
		return ""
	}
	off := res.Lines().OffsetAt(line, col)
	chain := jomini.NodeAtOffset(res.Root, off)
	if len(chain) == 0 {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.schemaLocked().Empty() {
		return ""
	}
	top, ok := chain[0].(*jomini.Assignment)
	if !ok || top.Key.Quoted {
		return ""
	}
	rel := path
	if _, r, ok := s.locate(path); ok {
		rel = r
	}
	// A missing root is not a dead end. Only on_action files declare their own
	// starting scope, so bailing here meant every event, decision, scripted
	// effect and modifier body reported nothing at all -- which is most of what
	// anyone hovers. A declared scope link or iterator states the scope it
	// produces regardless of what preceded it, so the walk continues and
	// reports a scope only once something declared establishes one.
	w := s.defRootScopeLocked(s.schemaLocked(), game.MatchExtract(s.GameID, rel).Kind, top)
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
		if !w.step(s.schemaLocked(), a.Key.Text) {
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

// A saved scope -- `scope:my_vassal = { … }` -- is deliberately NOT entered,
// though vanilla CK3 alone opens 69,823 blocks that way, fourteen times the
// dotted chains. Resolving the name to the scope it was saved in was tried and
// measured at eight false positives over 750 vanilla files, from two causes:
//
//   - one name saved several times at different scopes in one file
//     (CK3 background_terrain_scope, four save sites), so a single-pass walk
//     picks whichever it reached first; and, not fixable by being stricter,
//   - the engine coercing an effect to a related scope. EU5 writes
//     `scope:target_location = { add_stability = … }` where add_stability
//     declares country only, and does so throughout -- the same shape as CK3's
//     set_county_faith in a province and Victoria 3's random_scope_state in a
//     state.
//
// The second is a gap in the model, not in the resolution: PMT has no declared
// source for which scopes an engine silently coerces between. Entering saved
// scopes has to wait for one, because a check that accuses correct script of
// being wrong is worse than a check that stays quiet.

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
func (w *scopeWalk) step(sc *catalog.Schema, key string) bool {
	// A date is not a chain. History files key their blocks by one --
	// `867.1.1 = { … }` -- and splitting that steps through "867", "1", "1".
	if !strings.Contains(key, ".") || jomini.IsDateLiteral(key) {
		return w.stepOne(sc, key)
	}
	for _, seg := range strings.Split(key, ".") {
		if seg == "" || !w.stepOne(sc, seg) {
			return false
		}
	}
	return true
}

func (w *scopeWalk) stepOne(sc *catalog.Schema, key string) bool {
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
	if l, ok := sc.Links[k]; ok && l.Out != "" {
		if len(l.In) == 0 || w.cur() == "" || slices.Contains(l.In, w.cur()) {
			w.push(l.Out)
			return true
		}
	}
	// Iterators are effects/triggers whose Supported Targets is the element
	// scope: every_vassal is "Supported Scopes: character, Targets: character".
	if t, _, ok := sc.Token(k); ok && t.Target != "" {
		if (w.cur() == "" || catalog.UsableIn(t.In, w.cur())) &&
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

// scopeNeutral reports block keys that group script without changing scope.
// These are Clausewitz control flow, which is language rather than data, so it
// is the one table here that cannot come from a dump.
func scopeNeutral(k string) bool {
	switch k {
	case "if", "else", "else_if", "while", "switch", "limit", "trigger_if",
		"trigger_else", "trigger_else_if", "alternative_limit",
		"and", "or", "not", "nor", "nand", "potential",
		"trigger", "immediate", "effect", "option", "after",
		"cancellation_trigger", "on_trigger_fail", "random_list", "modifier":
		return true
	}
	return jomini.ScriptSlot(k) != ""
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
func (s *Session) ScopeMisuses(path string) []ScopeMisuse {
	res := s.Parsed(path)
	if res.Root == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	sc := s.schemaLocked()
	if sc.Empty() {
		return nil
	}
	rel := path
	if _, r, ok := s.locate(path); ok {
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
		// on the game's word when the current scope is unknown, and walkMisuse
		// reports only once w.cur() is non-empty, so a misuse is still judged
		// against a scope something DECLARED rather than one PMT assumed.
		w := s.defRootScopeLocked(sc, kind, a)
		s.walkMisuse(sc, b, w, "", nil, &out)
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
func (s *Session) defRootScopeLocked(
	sc *catalog.Schema, kind string, def *jomini.Assignment,
) scopeWalk {
	if kind == "on_action" {
		if scope := sc.OnActions[strings.ToLower(def.Key.Text)]; scope != "" && scope != "none" {
			return scopeWalk{stack: []string{scope}, rooted: true}
		}
		return scopeWalk{}
	}
	if kind == "event" {
		if scope := s.eventRootScopeLocked(sc, def.Key.Text); scope != "" {
			return scopeWalk{stack: []string{scope}, rooted: true}
		}
	}
	return scopeWalk{}
}

// eventRootScopeLocked is the scope an event's body runs in, taken from the
// on_actions that fire it.
//
// An event does not declare its own scope, and its `type` does not either --
// `activity_event` and `letter_event` bodies are both character-scoped, so
// reading the type as a scope reported thousands of correct vanilla lines as
// wrong-scope. What DOES declare it is the on_action: on_actions.log states the
// scope each one runs in, and an event fired by one runs in that same scope.
//
// Measured across all three installs, every event reachable from a scoped
// on_action has exactly ONE root scope -- 4,376 on CK3, 932 on Victoria 3, 2,027
// on EU5, and not a single conflict. So a disagreement means something PMT does
// not understand, and the honest answer is no root at all rather than a pick.
//
// Edges come from the session, not the cache, so a mod's own on_action firing a
// mod's own event counts the same as vanilla's.
func (s *Session) eventRootScopeLocked(sc *catalog.Schema, event string) string {
	found := ""
	for _, e := range s.edgesByTo[event] {
		scope := sc.OnActions[strings.ToLower(e.From)]
		if scope == "" || scope == "none" {
			continue
		}
		if found == "" {
			found = scope
			continue
		}
		if found != scope {
			return ""
		}
	}
	return found
}

// walkMisuse judges each statement of a block against the scope the walk says
// it runs in. params names the arguments the enclosing token declares, which are
// consumed by that token rather than executed in the scope it produces.
func (s *Session) walkMisuse(
	sc *catalog.Schema, b *jomini.Block, w scopeWalk, slot string,
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
			if _, shadowed := s.macroDefKinds[key]; !shadowed && slot != "" && w.cur() != "" {
				if t, role, ok := sc.Token(lk); ok && role == slot &&
					!catalog.UsableIn(t.In, w.cur()) {
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
		s.walkMisuse(sc, child, next, nextSlot, sc.TokenParams(lk), out)
	}
}

// TokensInScope returns the declared effect or trigger names usable in scope,
// filtered by prefix. slot is "effect" or "trigger". An empty scope returns
// every token, since an unknown scope must not hide valid completions.
func (s *Session) TokensInScope(scope, slot, prefix string, limit int) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sc := s.schemaLocked()
	if sc == nil {
		return nil
	}
	bag := sc.Effects
	if slot == "trigger" {
		bag = sc.Triggers
	}
	lower := strings.ToLower(prefix)
	out := make([]string, 0, min(limit, len(bag)))
	for name, t := range bag {
		if prefix != "" && !strings.HasPrefix(name, lower) {
			continue
		}
		if !catalog.UsableIn(t.In, scope) {
			continue
		}
		out = append(out, name)
	}
	slices.Sort(out)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// ScopeTokens is the set of engine tokens the game declares usable in scope,
// effects and triggers together, lowercased.
//
// For ranking rather than filtering. Completion used to replace its token list
// with the scope-valid one, which hides everything else — and that is only safe
// while the scope walk is right. The walk now answers in far more places than
// it used to, so a wrong answer would silently remove correct tokens instead of
// merely mis-ordering them. Empty scope returns nil, meaning "no opinion".
func (s *Session) ScopeTokens(scope string) map[string]bool {
	if scope == "" {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	sc := s.schemaLocked()
	if sc == nil {
		return nil
	}
	out := make(map[string]bool, len(sc.Effects)+len(sc.Triggers))
	for _, bag := range []map[string]catalog.EngineToken{sc.Effects, sc.Triggers} {
		for name, t := range bag {
			if catalog.UsableIn(t.In, scope) {
				out[strings.ToLower(name)] = true
			}
		}
	}
	return out
}
