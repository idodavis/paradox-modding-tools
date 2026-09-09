// complete.go serves completion for script, loc, mod, and metadata files.
package lsp

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"paradox-modding-tools/services/internal/catalog"
	"paradox-modding-tools/services/internal/modfile"
	"paradox-modding-tools/services/internal/parser/jomini"
	"paradox-modding-tools/services/internal/parser/loc"
	"paradox-modding-tools/services/internal/session"
)

const maxComplete = 80

// Complete returns completion items at (line, UTF-8 column).
func Complete(s *session.Session, path string, line, col int) []CompletionItem {
	src := s.FileText(path)
	stamp := func(items []CompletionItem) []CompletionItem {
		return stampRange(src, line, col, items)
	}
	if s.KindFor(path) == "mod" {
		return stamp(prefixFilter(modItems(), completePrefix(s, path, line, col)))
	}
	if isMetaFile(s, path) {
		return stamp(prefixFilter(metaItems(s), completePrefix(s, path, line, col)))
	}
	if s.KindFor(path) == "loc" || s.KindFor(path) == "gui" {
		if items := dataFnItems(s, path, line, col); len(items) > 0 {
			return stamp(items)
		}
		if s.KindFor(path) == "loc" {
			return stamp(capComplete(locItems(s, completePrefix(s, path, line, col))))
		}
	}
	at, ok := s.CursorAt(path, line, col)
	if !ok {
		return nil
	}
	prefix := ""
	if at.Word != "" && at.Start < at.Off {
		prefix = at.Src[at.Start:at.Off]
	}
	scope := s.ScopeAt(path, line, col)
	items := scriptItems(s, path, at, prefix, scope)
	Rank(s, at.Kind, scope, items)
	return stamp(capComplete(items))
}

func stampRange(src string, line, col int, items []CompletionItem) []CompletionItem {
	if len(items) == 0 || src == "" {
		return items
	}
	li := jomini.NewLineIndex(src)
	off := li.OffsetAt(line, col)
	_, start, end := jomini.Result{Src: src}.TokenAt(off)
	if start == end {
		start, end = off, off
	}
	start = citeStart(src, start, items)
	rg := byteRange(li, start, end)
	for i := range items {
		items[i].Range = rg
	}
	return items
}

// citeStart widens the replaced span leftwards over a `name:` the offered
// citations already contain, so accepting one replaces the whole expression.
//
// The word scan behind TokenAt stops at `:` even though the lexer does not, so
// with `culture_group:tur` typed it reports only `tur`. Replacing just that and
// inserting `culture_group:turkic_group` produced
// `culture_group:culture_group:turkic_group`.
//
// The widening is exact rather than heuristic: it happens only when the text
// immediately before the span is literally the label's own prefix plus a colon,
// so the only characters ever swallowed are ones the label puts back.
func citeStart(src string, start int, items []CompletionItem) int {
	if len(items) == 0 || start <= 0 || src[start-1] != ':' {
		return start
	}
	cite, _, ok := strings.Cut(items[0].Label, ":")
	if !ok || cite == "" {
		return start
	}
	lead := start - 1 - len(cite)
	if lead < 0 || !strings.EqualFold(src[lead:start-1], cite) {
		return start
	}
	return lead
}

func completePrefix(s *session.Session, path string, line, col int) string {
	src := s.FileText(path)
	if src == "" {
		return ""
	}
	off := jomini.NewLineIndex(src).OffsetAt(line, col)
	word, start, _ := jomini.Result{Src: src}.TokenAt(off)
	if word != "" && start < off {
		return src[start:off]
	}
	return ""
}

func prefixFilter(items []CompletionItem, prefix string) []CompletionItem {
	if prefix == "" {
		return capComplete(items)
	}
	out := make([]CompletionItem, 0, min(len(items), maxComplete))
	for _, it := range items {
		if lowerPrefix(it.Label, prefix) {
			out = append(out, it)
			if len(out) >= maxComplete {
				break
			}
		}
	}
	return out
}

func capComplete(items []CompletionItem) []CompletionItem {
	if len(items) > maxComplete {
		return items[:maxComplete]
	}
	return items
}

func item(label, detail string) CompletionItem {
	return CompletionItem{Label: label, Detail: detail, Kind: "property"}
}

func valItem(label, detail string) CompletionItem {
	return CompletionItem{Label: label, Detail: detail, Kind: "value"}
}

func documented(s *session.Session, label, detail, kind string) CompletionItem {
	it := CompletionItem{
		Label: label, Detail: detail, Kind: "property",
		Documentation: completeDoc(s, label, kind),
	}
	if kind != "" {
		if s.StructureBlock(kind, label) {
			it.InsertText = label + " = { $0 }"
		} else {
			it.InsertText = label + " = "
		}
	}
	return it
}

func documentedVal(s *session.Session, label, detail, kind string) CompletionItem {
	return CompletionItem{
		Label: label, Detail: detail, Kind: "value",
		Documentation: completeDoc(s, label, kind),
	}
}

func completeDoc(s *session.Session, label, kind string) string {
	doc := s.FieldDoc(label, kind)
	if u := s.TokenUsage(label); u != "" {
		if doc != "" {
			doc += "\n\n"
		}
		doc += "```\n" + u + "\n```"
	}
	return doc
}

func modItems() []CompletionItem {
	out := make([]CompletionItem, 0, len(modfile.DescriptorModKeys))
	for _, k := range modfile.DescriptorModKeys {
		out = append(out, item(k, "mod descriptor"))
	}
	return out
}

func metaItems(s *session.Session) []CompletionItem {
	keys := s.Vocab("meta")
	if len(keys) == 0 {
		return nil
	}
	out := make([]CompletionItem, 0, len(keys))
	for _, k := range keys {
		out = append(out, item(k, "metadata"))
	}
	return out
}

func dataFnItems(s *session.Session, path string, line, col int) []CompletionItem {
	src := s.FileText(path)
	if src == "" {
		return nil
	}
	off := jomini.NewLineIndex(src).OffsetAt(line, col)
	if off < 0 || off > len(src) {
		return nil
	}
	open := strings.LastIndex(src[:off], "[")
	if open < 0 {
		return nil
	}
	if strings.Contains(src[open:off], "]") {
		return nil
	}
	prefix := src[open+1 : off]
	var out []CompletionItem
	for _, k := range s.Vocab("datafunction") {
		if !lowerPrefix(k, prefix) {
			continue
		}
		out = append(out, documentedVal(s, k, "data function", ""))
		if len(out) >= maxComplete {
			break
		}
	}
	return out
}

func locItems(s *session.Session, prefix string) []CompletionItem {
	seen := map[string]bool{}
	var out []CompletionItem
	add := func(k string) {
		if len(out) >= maxComplete || k == "" || seen[k] || !lowerPrefix(k, prefix) {
			return
		}
		seen[k] = true
		out = append(out, documentedVal(s, k, "loc key", ""))
	}
	for _, k := range s.LocKeys(prefix, maxComplete) {
		add(k)
	}
	return out
}

func scriptItems(
	s *session.Session, path string, at session.Cursor, prefix, scope string,
) []CompletionItem {
	if !at.InKey {
		if at.SlotKey == "" {
			return rootItems(s, at.Kind, prefix)
		}
		return valueItems(s, at.Kind, at.SlotKey, prefix)
	}
	// Prefer the inherited slot: inside `immediate = { capital_county = { … } }`
	// the effect context still holds even though capital_county is not a slot.
	slot := at.Slot
	if slot == "" {
		slot = jomini.ScriptSlot(at.SlotKey)
	}
	switch slot {
	case "trigger", "effect":
		return vocabAndCallDefs(s, prefix, slot, scope)
	default:
		// Structure first, vocabulary behind it. A block PMT cannot name a slot
		// for is still usually a trigger or effect context: a decision's
		// `is_shown` holds triggers, and offering only the decision's own keys
		// meant `current_year` was never suggested no matter how much of it was
		// typed. Rank puts structure keys ahead of tokens, so the block's own
		// members stay first and the rest is there when nothing else fits.
		items := structureItems(s, path, at.Kind, at.SlotKey, prefix)
		if len(items) > 0 {
			return items
		}
		// Only when structure has nothing to say. With an empty prefix the
		// block's own members ARE the answer -- inside `triggered_desc = { … }`
		// that is `desc` and `trigger`, and appending the whole vocabulary
		// there is the noise this nesting work removed. But a reader four
		// characters into `current_year` inside a decision's is_shown matched
		// none of them and got nothing at all, and nothing is always worse.
		return vocabAndCallDefs(s, prefix, "", scope)
	}
}

func rootItems(s *session.Session, kind, prefix string) []CompletionItem {
	var out []CompletionItem
	ck := jomini.CanonicalKind(kind)
	if jomini.IsMacroKind(ck) && lowerPrefix(ck, prefix) {
		out = append(out, item(ck, kind))
	}
	out = append(out, defsOfType(s, prefix, ck)...)
	// Last: a skeleton is the answer when nothing else is, and someone part-way
	// through a name wants the name.
	out = append(out, snippetItem(s, kind, prefix)...)
	return out
}

// valueSource is one named source that may know what a slot holds.
//
// The order is the same invariant identify follows and is written out for the
// same reason: this is the second place it lives, and the two drifting apart is
// how a slot would come to complete differently from the way it hovers. See
// session/resolve.go.
type valueSource struct {
	name string
	// items returns ok=false to decline, which passes the slot to the next
	// source. ok=true with no items is a real answer -- "the game says this slot
	// holds kind X and nothing of kind X matches" -- and must not fall through,
	// or a slot whose type is declared would be offered values of some other
	// type inferred from the install.
	items func(s *session.Session, kind, parentKey, prefix string) ([]CompletionItem, bool)
}

// valueSources is the order, most authoritative first. Declared beats derived:
// the game states the argument type of 1,694 CK3 / 3,834 Vic3 / 1,742 EU5
// tokens in Supported Targets, and until that was read here
// `has_culture_group = ` fell through to install-derived field typing and, when
// that had too little evidence to clear its coverage floor, all the way to
// offering `yes` and `no`.
var valueSources = []valueSource{
	// The text itself names the database. `this = title:k_england` says which
	// rows it wants, whatever the field's declared type is or is not, and every
	// other source offers bare ids that the typed `title:` can never match --
	// so half of all value positions in a real mod completed to nothing.
	{"cited-prefix", func(s *session.Session, _, _, prefix string) ([]CompletionItem, bool) {
		cite, _, ok := strings.Cut(prefix, ":")
		if !ok || cite == "" {
			return nil, false
		}
		// Ephemeral kinds included, unlike the diagnostics. A saved scope cannot
		// be JUDGED -- the script invents it -- but the ones already saved in
		// this workspace are exactly what a reader typing `scope:` wants, and
		// they are harvested as definitions like anything else.
		// A bound database first, then the language prefixes. `scope:` is not a
		// database PMT bound to anything -- it is Clausewitz grammar -- so the
		// session's table says nothing about it while jomini's does, and every
		// `scope:` a reader typed completed to nothing.
		kind := s.PrefixKind(cite)
		if kind == "" {
			kind = jomini.PrefixKind(cite)
		}
		if kind == "" || !s.KindHasDefs(kind) {
			return nil, false
		}
		return citedDefsOfType(s, prefix, cite, kind), true
	}},
	{"fire-key", func(s *session.Session, _, parentKey, prefix string) ([]CompletionItem, bool) {
		fk := s.FireKind(parentKey)
		if fk == "" {
			return nil, false
		}
		return defsOfType(s, prefix, fk), true
	}},
	{"localization", func(s *session.Session, _, parentKey, prefix string) ([]CompletionItem, bool) {
		if loc.Classify(parentKey) == loc.PropNone {
			return nil, false
		}
		return locItems(s, prefix), true
	}},
	{"slot-declared", func(s *session.Session, _, parentKey, prefix string) ([]CompletionItem, bool) {
		r, ok := jomini.ScriptName(parentKey)
		if !ok {
			return nil, false
		}
		return defsOfType(s, prefix, r.Kind), true
	}},
	// The only source that declines on an empty result: a token with no
	// declared target type yields nothing here and has not answered.
	{"target-declared", func(s *session.Session, _, parentKey, prefix string) ([]CompletionItem, bool) {
		items := targetItems(s, parentKey, prefix)
		return items, len(items) > 0
	}},
	// A closed set the game states in its own documentation. Above slot-derived
	// for the usual reason, and it declines on an empty set rather than on an
	// empty result: "the docs name no type for this field" is not an answer.
	{"slot-declared-doc", func(s *session.Session, kind, parentKey, prefix string) ([]CompletionItem, bool) {
		dt := s.DeclaredFieldType(kind, parentKey)
		if len(dt.Values) == 0 {
			return nil, false
		}
		return enumItems(dt.Values, prefix), true
	}},
	{"slot-derived", func(s *session.Session, _, parentKey, prefix string) ([]CompletionItem, bool) {
		k := s.FieldValueKind(parentKey)
		if k == "" {
			return nil, false
		}
		return defsOfType(s, prefix, k), true
	}},
	// Declining on an empty result, unlike the declared sources above. "The game
	// says this slot holds kind X and nothing of kind X matches" is a real
	// answer; "this macro declares no parameters" is not an answer to what value
	// belongs here. A scripted trigger invoked as `x = yes` has no parameters at
	// all, and claiming the slot swallowed the boolean it actually takes -- 17
	// of 24 empty value positions on a real Victoria 3 mod.
	{"macro-params", func(s *session.Session, _, parentKey, prefix string) ([]CompletionItem, bool) {
		if !s.IsMacroDef(parentKey) && !jomini.IsMacroKind(parentKey) {
			return nil, false
		}
		items := scriptParamItems(s, parentKey, prefix)
		return items, len(items) > 0
	}},
	{"macro-call-params", func(s *session.Session, _, parentKey, prefix string) ([]CompletionItem, bool) {
		if !macroCallParent(s, parentKey) {
			return nil, false
		}
		items := scriptParamItems(s, parentKey, prefix)
		return items, len(items) > 0
	}},
	{"field-enums", func(s *session.Session, kind, parentKey, prefix string) ([]CompletionItem, bool) {
		enums := s.FieldEnums(kind, parentKey)
		if len(enums) == 0 {
			return nil, false
		}
		return enumItems(enums, prefix), true
	}},
}

func valueItems(s *session.Session, kind, parentKey, prefix string) []CompletionItem {
	for _, src := range valueSources {
		if items, ok := src.items(s, kind, parentKey, prefix); ok {
			return items
		}
	}
	return valueFallback(prefix)
}

// targetItems completes the value of a token whose argument type the game
// declares, e.g. `has_culture_group = ` where EU5 states
// "Supported Targets: culture_group".
//
// Which form to offer is itself declared, not guessed. A scope type that is a
// global link requiring data is written `prefix:id`, so the whole citation is
// offered and one accept produces a valid value. A type with no such link is
// written as a bare key of the database bound to it. A type with neither — a
// value, a boolean — yields nothing and the caller falls through.
func targetItems(s *session.Session, parentKey, prefix string) []CompletionItem {
	target := s.TokenTarget(parentKey)
	if target == "" {
		return nil
	}
	cite, kind := s.CiteFormOfScope(target)
	if kind == "" {
		return nil
	}
	if cite == "" {
		return defsOfType(s, prefix, kind)
	}
	return citedDefsOfType(s, prefix, cite, kind)
}

// citedDefsOfType offers `prefix:id` values. The user may be part-way through
// either half, so the typed text is matched against the whole citation and
// against the bare id — typing `turkic` still finds
// `culture_group:turkic_group`.
func citedDefsOfType(s *session.Session, typed, cite, kind string) []CompletionItem {
	idPrefix := typed
	if i := strings.IndexByte(typed, ':'); i >= 0 {
		if !lowerPrefix(cite, typed[:i]) {
			return nil
		}
		idPrefix = typed[i+1:]
	}
	seen := map[string]bool{}
	var out []CompletionItem
	// The same filter-before-the-limit rule as defsOfType, and the same symptom
	// when it was broken: `has_culture_group = ` on EU5 declares its target,
	// binds it to `culture_groups`, and still offered nothing, because the first
	// maxComplete definitions of any kind held none of them.
	for _, d := range s.FindDefsOf(idPrefix, kind, maxComplete, true, true) {
		if d.Key == "" {
			continue
		}
		label := cite + ":" + d.Key
		if seen[label] || !lowerPrefix(label, typed) && !lowerPrefix(d.Key, idPrefix) {
			continue
		}
		seen[label] = true
		out = append(out, documentedVal(s, label, d.Kind, d.Kind))
		if len(out) >= maxComplete {
			break
		}
	}
	return out
}

// scriptParamItems offers the parameters of the macro being called.
//
// The owner filter must happen before the limit. Asking for the first
// maxComplete script params and then keeping this macro's returned whichever
// params map iteration reached first — across 2,536 different CK3 macros — so
// in practice the list was empty for the macro under the cursor.
func scriptParamItems(s *session.Session, macroKey, prefix string) []CompletionItem {
	var out []CompletionItem
	for _, name := range s.FindParamsOf(macroKey, maxComplete) {
		if !lowerPrefix(name, prefix) {
			continue
		}
		out = append(out, valItem(name, "script parameter"))
		if len(out) >= maxComplete {
			break
		}
	}
	return out
}

func macroCallParent(s *session.Session, key string) bool {
	d := s.Resolve(key)
	if d == nil {
		return false
	}
	return jomini.IsMacroKind(d.Kind)
}

func enumItems(vals []string, prefix string) []CompletionItem {
	var out []CompletionItem
	for _, v := range vals {
		if !lowerPrefix(v, prefix) {
			continue
		}
		out = append(out, valItem(v, "value"))
		if len(out) >= maxComplete {
			break
		}
	}
	return out
}

func valueFallback(prefix string) []CompletionItem {
	seen := map[string]bool{}
	var out []CompletionItem
	add := func(label, detail string) {
		if len(out) >= maxComplete || label == "" || seen[label] ||
			!lowerPrefix(label, prefix) {
			return
		}
		seen[label] = true
		out = append(out, valItem(label, detail))
	}
	add("yes", "bool")
	add("no", "bool")
	return out
}

// structureItems offers the keys that belong where the cursor is.
//
// block is the key of the enclosing block. When the install evidenced what that
// block holds, its members come first and the kind's top-level keys are dropped:
// inside `triggered_desc = { … }` an event's `option` and `theme` are not
// offered, because they cannot go there. Without evidence for the block this
// falls back to the kind exactly as before, so nothing is lost where the model
// is thin.
func structureItems(s *session.Session, path, kind, block, prefix string) []CompletionItem {
	seen := map[string]bool{}
	var out []CompletionItem
	add := func(label, detail string) {
		if len(out) >= maxComplete || label == "" || seen[label] || !lowerPrefix(label, prefix) {
			return
		}
		seen[label] = true
		out = append(out, documented(s, label, detail, kind))
	}
	// When the install evidenced what this block holds, that is the answer and
	// the kind's own keys are not: inside `triggered_desc = { … }` an event's
	// `option` and `theme` cannot go there. Returning empty when the prefix
	// matches none of them is deliberate — the caller widens to the vocabulary,
	// which is where a half-typed `current_year` lives. Falling back to the
	// KIND instead would answer with a near-miss from the wrong set and stop
	// the caller ever widening.
	if members := s.NestedMembers(block); len(members) > 0 {
		for _, k := range members {
			add(k, block)
		}
		return out
	}
	for _, k := range s.Structures(kind) {
		add(k, kind)
	}
	if s.KindFor(path) != "gui" {
		return out
	}
	for _, k := range s.Vocab("gui_type") {
		add(k, "gui type")
	}
	for _, k := range s.Vocab("gui_prop") {
		add(k, "gui")
		if len(out) >= maxComplete {
			break
		}
	}
	return out
}

// vocabAndCallDefs offers the engine tokens legal in the current block, then
// the mod's own scripted_* macros. When the game's schema is loaded and the
// scope is known, tokens are filtered to those the game declares usable there:
// inside a landed_title block, character-only effects are not offered. An
// unknown scope falls back to the whole vocabulary rather than hiding anything.
func vocabAndCallDefs(s *session.Session, prefix, vocabKind, scope string) []CompletionItem {
	seen := map[string]bool{}
	var out []CompletionItem
	add := func(label, detail string) {
		if len(out) >= maxComplete || label == "" || seen[label] || !lowerPrefix(label, prefix) {
			return
		}
		seen[label] = true
		out = append(out, documented(s, label, detail, ""))
	}
	tokens := s.Vocab(vocabKind)
	if len(tokens) == 0 {
		// The effect and trigger lists come from script_docs. Without it there
		// is no engine API to narrow to, so offer what the walk harvested from
		// the corpus instead of nothing at all.
		tokens = s.Vocab("vocabulary")
	}
	// Scope-valid tokens go first, but nothing is removed. Replacing the list
	// with the scope-valid one hid everything else, which is only safe while the
	// scope walk is right — and the walk now answers in far more places than it
	// did, so a wrong answer would silently drop correct tokens rather than
	// merely mis-order them. Ordering here rather than in Rank because the
	// maxComplete cap is applied while the list is built: sorting afterwards
	// would rank items the cap had already thrown away.
	if scope != "" && s.HasSchema() {
		tokens = scopeFirst(tokens, s.ScopeTokens(scope))
	}
	for _, k := range tokens {
		add(k, vocabKind)
		if len(out) >= maxComplete {
			return out
		}
	}
	for _, d := range s.FindMacroDefs(prefix, maxComplete) {
		add(d.Key, d.Kind)
		if len(out) >= maxComplete {
			break
		}
	}
	return out
}

func defsOfType(s *session.Session, prefix, defType string) []CompletionItem {
	seen := map[string]bool{}
	var out []CompletionItem
	add := func(key, kind string) {
		if key == "" || seen[key] || !lowerPrefix(key, prefix) {
			return
		}
		seen[key] = true
		out = append(out, documentedVal(s, key, kind, kind))
	}
	// FindDefsOf, which applies the kind before the limit. The ephemeral branch
	// always did; everything else took the first maxComplete definitions of any
	// kind and then discarded the ones that did not match, so on a workspace
	// with two hundred thousand definitions the answer was whatever the index
	// reached first — almost never the kind asked for.
	//
	// This is why completion fell through to `yes`/`no` even when the type was
	// perfectly well known: on Victoria 3 the field kind was right for 203 of
	// 496 unanswered value positions — `interest_groups`, `cultures`,
	// `religions`, `ideologies` — and the lookup returned nothing anyway.
	// Cheap discriminator first: filter before the limit.
	for _, d := range s.FindDefsOf(prefix, defType, maxComplete, true, true) {
		add(d.Key, d.Kind)
		if len(out) >= maxComplete {
			break
		}
	}
	return out
}

// scopeFirst puts the tokens the game declares usable here at the front,
// keeping the rest behind them rather than dropping them. inScope nil means the
// scope is unknown, which is not the same as "nothing is valid".
func scopeFirst(tokens []string, inScope map[string]bool) []string {
	if len(inScope) == 0 {
		return tokens
	}
	valid := make([]string, 0, len(tokens))
	rest := make([]string, 0, len(tokens))
	for _, t := range tokens {
		if inScope[strings.ToLower(t)] {
			valid = append(valid, t)
		} else {
			rest = append(rest, t)
		}
	}
	return append(valid, rest...)
}

// rankSets orders completion items: the kind's own structure keys first, then
// the engine tokens the game declares usable in this scope, then everything
// else, and last the tokens the game says do not belong here.
//
// Last, not absent. A token ranked bottom is still one keystroke away if the
// scope walk got it wrong; a token filtered out is simply gone, and the modder
// has no way to tell the difference between "not valid here" and "PMT does not
// know about it".
type rankSets struct {
	structKeys, effects, triggers, inScope map[string]bool
}

func (m rankSets) rank(label string) int {
	k := strings.ToLower(label)
	isToken := m.effects[k] || m.effects[label] || m.triggers[k] || m.triggers[label]
	switch {
	case m.structKeys[k] || m.structKeys[label]:
		return 0
	case len(m.inScope) > 0 && m.inScope[k]:
		return 1
	case isToken:
		return 2
	case len(m.inScope) > 0 && isDeclaredElsewhere(m, k):
		return 4
	default:
		return 3
	}
}

// isDeclaredElsewhere reports a token the game declares but not for this scope.
func isDeclaredElsewhere(m rankSets, k string) bool {
	return m.effects[k] || m.triggers[k]
}

// Rank reorders completion items in place. An empty scope means "no opinion",
// which leaves the ordering exactly as it was before scopes were consulted.
func Rank(s *session.Session, kind, scope string, items []CompletionItem) {
	sk, ef, tr := s.MemberSets(kind)
	m := rankSets{
		structKeys: sk, effects: ef, triggers: tr,
		inScope: s.ScopeTokens(scope),
	}
	slices.SortStableFunc(items, func(a, b CompletionItem) int {
		return cmp.Compare(m.rank(a.Label), m.rank(b.Label))
	})
}

// snippetItem offers the derived skeleton for a kind as one completion.
//
// Ranked and labelled as a skeleton rather than mixed in with the kind's keys:
// someone part-way through a word wants the token, not a twelve-line block
// dropped into their file. It only appears at the top level of a file, which is
// where a new definition is written.
func snippetItem(s *session.Session, kind, prefix string) []CompletionItem {
	members := s.Snippet(kind)
	if len(members) == 0 {
		return nil
	}
	label := jomini.CanonicalKind(kind)
	if label == "" {
		label = kind
	}
	if !lowerPrefix(label, prefix) {
		return nil
	}
	return []CompletionItem{{
		Label:      label,
		Detail:     "skeleton",
		Kind:       "snippet",
		InsertText: renderSnippet(label, members),
		Documentation: "The shape " + label +
			" definitions take in this install, most-used members first.",
	}}
}

// renderSnippet writes LSP snippet text: `${n:placeholder}` tab stops in the
// order a reader fills them, and `$0` where the cursor finishes.
func renderSnippet(label string, members []catalog.SnippetMember) string {
	var b strings.Builder
	tab := 1
	fmt.Fprintf(&b, "${%d:%s_name} = {\n", tab, label)
	tab++
	for _, m := range members {
		if m.Block {
			fmt.Fprintf(&b, "\t%s = {\n\t\t$%d\n\t}\n", m.Key, tab)
		} else {
			fmt.Fprintf(&b, "\t%s = ${%d:value}\n", m.Key, tab)
		}
		tab++
	}
	b.WriteString("\t$0\n}")
	return b.String()
}
