// scope_test.go covers scope-aware completion: with the game's declared type
// system loaded, only effects and triggers the game says are usable in the
// current scope are offered.

package lsp

import (
	"strings"
	"testing"

	"paradox-modding-tools/services/internal/catalog"
	"paradox-modding-tools/services/internal/session"
)

func scopedCache() *catalog.VanillaCache {
	c := &catalog.VanillaCache{
		Schema: &catalog.Schema{
			Scopes: map[string]catalog.ScopeType{
				"character":    {ChangeScopes: true, ExecuteEffects: true},
				"landed_title": {ChangeScopes: true, ExecuteEffects: true},
			},
			Links: map[string]catalog.ScopeLink{
				"capital_county": {In: []string{"character"}, Out: "landed_title"},
				"holder":         {In: []string{"landed_title"}, Out: "character"},
			},
			Effects: map[string]catalog.EngineToken{
				"add_gold":      {In: []string{"character"}, Doc: "Adds gold"},
				"add_title_law": {In: []string{"landed_title"}, Doc: "Adds a title law"},
				"every_vassal":  {In: []string{"character"}, Target: "character"},
			},
			Triggers: map[string]catalog.EngineToken{
				"is_adult":      {In: []string{"character"}},
				"has_title_law": {In: []string{"landed_title"}},
			},
			// on_birth is a character on_action, per on_actions.log; that is
			// what seeds the walk's root scope.
			OnActions: map[string]string{"on_birth": "character"},
		},
	}
	catalog.PrepareCache(c)
	return c
}

func has(items []CompletionItem, want string) bool {
	for _, it := range items {
		if it.Label == want {
			return true
		}
	}
	return false
}

func scopedSession(t *testing.T, body string) (*session.Session, string) {
	t.Helper()
	s, root := buildSession(t, "ck3",
		map[string]string{"common/on_action/x.txt": body}, scopedCache(), nil)
	return s, root + "/common/on_action/x.txt"
}

func TestCompleteFiltersByScope(t *testing.T) {
	body := `on_birth = {
	effect = {
		capital_county = {
			add_
		}
	}
}
`
	s, p := scopedSession(t, body)
	line, col := lineCol(body, "add_")
	items := Complete(s, p, line, col+len("add_"))

	if !has(items, "add_title_law") {
		t.Errorf("landed_title scope missing add_title_law: %v", labels(items))
	}
	// Ranked below, not removed. Hiding is only safe while the scope walk is
	// right, and a modder cannot tell a hidden token from one PMT never heard
	// of. Bottom of the list says "the game says this does not belong here"
	// while still being one keystroke away.
	if !has(items, "add_gold") {
		t.Errorf("add_gold should still be offered, ranked last: %v", labels(items))
	}
	if !ranksBefore(items, "add_title_law", "add_gold") {
		t.Errorf("scope-valid token not ranked first: %v", labels(items))
	}
}

// ranksBefore reports whether first appears ahead of second.
func ranksBefore(items []CompletionItem, first, second string) bool {
	fi, si := -1, -1
	for i, it := range items {
		if it.Label == first && fi < 0 {
			fi = i
		}
		if it.Label == second && si < 0 {
			si = i
		}
	}
	return fi >= 0 && si >= 0 && fi < si
}

// The same file, one block deeper, flips back to character through holder.
func TestCompleteFollowsScopeBackOut(t *testing.T) {
	body := `on_birth = {
	effect = {
		capital_county = {
			holder = {
				add_
			}
		}
	}
}
`
	s, p := scopedSession(t, body)
	line, col := lineCol(body, "add_")
	items := Complete(s, p, line, col+len("add_"))

	if !has(items, "add_gold") {
		t.Errorf("holder returns to character, want add_gold: %v", labels(items))
	}
	if !ranksBefore(items, "add_gold", "add_title_law") {
		t.Errorf("holder returns to character; add_gold should rank first: %v",
			labels(items))
	}
}

func TestCompleteTriggersByScope(t *testing.T) {
	body := `on_birth = {
	trigger = {
		capital_county = {
			ha
		}
	}
}
`
	s, p := scopedSession(t, body)
	line, col := lineCol(body, "\t\t\tha")
	items := Complete(s, p, line, col+len("\t\t\tha"))
	if !has(items, "has_title_law") {
		t.Errorf("want has_title_law in landed_title trigger: %v", labels(items))
	}
	// is_adult is absent here because of the "ha" prefix, not the scope — the
	// assertion that used to sit here passed for that reason and tested nothing.
	// The scope contract is ranking, and it is pinned in the effect tests above.
}

// Without a schema there is no engine API to offer, but what the walk harvested
// from the corpus must still complete, and must complete unfiltered: a missing
// type system may narrow what PMT knows, never hide what it does know.
func TestCompleteUnfilteredWithoutSchema(t *testing.T) {
	body := `on_birth = {
	effect = {
		capital_county = {
			add_
		}
	}
}
`
	c := &catalog.VanillaCache{
		Vocabulary: []string{"add_gold", "add_title_law", "is_adult"},
	}
	catalog.PrepareCache(c)
	s, root := buildSession(t, "ck3", map[string]string{"common/on_action/x.txt": body}, c, nil)
	line, col := lineCol(body, "add_")
	items := Complete(s, root+"/common/on_action/x.txt", line, col+len("add_"))
	if !has(items, "add_gold") || !has(items, "add_title_law") {
		t.Errorf("no schema must not filter: %v", labels(items))
	}
}

// An unknown scope also falls back to the full vocabulary.
func TestCompleteUnknownScopeUnfiltered(t *testing.T) {
	body := `on_birth = {
	effect = {
		mystery_block = {
			add_
		}
	}
}
`
	s, p := scopedSession(t, body)
	line, col := lineCol(body, "add_")
	items := Complete(s, p, line, col+len("add_"))
	if !has(items, "add_gold") || !has(items, "add_title_law") {
		t.Errorf("unknown scope must not filter: %v", labels(items))
	}
}

// Hover on an engine token states what the game declares: what it is, the
// scope it runs on, and the type of its argument. The old card showed vote
// metadata instead.
func TestHoverNamesDeclaredSignature(t *testing.T) {
	body := `on_birth = {
	effect = {
		add_gold = 100
	}
}
`
	s, p := scopedSession(t, body)
	line, col := lineCol(body, "add_gold")
	wantHover(t, s, p, line, col+2, "Effect", "runs on character", "Adds gold")
}

func TestHoverNamesIteratorTarget(t *testing.T) {
	body := `on_birth = {
	effect = {
		every_vassal = {
			add_gold = 1
		}
	}
}
`
	s, p := scopedSession(t, body)
	line, col := lineCol(body, "every_vassal")
	wantHover(t, s, p, line, col+2, "runs on character", "takes a character")
}

// A scope link hover names where it comes from and what it yields.
func TestHoverNamesScopeLink(t *testing.T) {
	body := `on_birth = {
	effect = {
		capital_county = {
			add_title_law = x
		}
	}
}
`
	s, p := scopedSession(t, body)
	line, col := lineCol(body, "capital_county")
	wantHover(t, s, p, line, col+2, "from character", "yields landed title")
}

func TestWrongScopeDiagnostic(t *testing.T) {
	body := `on_birth = {
	effect = {
		capital_county = {
			add_gold = 100
		}
	}
}
`
	s, p := scopedSession(t, body)
	diags := Diagnose(s, p)
	if !hasDiag(diags, "wrong-scope") {
		t.Fatalf("want wrong-scope for a character effect in a title block: %+v", diags)
	}
}

func TestWrongScopeSilentWhenCorrect(t *testing.T) {
	body := `on_birth = {
	effect = {
		capital_county = {
			add_title_law = x
		}
		add_gold = 5
	}
}
`
	s, p := scopedSession(t, body)
	if diags := Diagnose(s, p); hasDiag(diags, "wrong-scope") {
		t.Errorf("false positive on correct script: %+v", diags)
	}
}

// No type system means no scope claims at all.
func TestWrongScopeSilentWithoutSchema(t *testing.T) {
	body := `on_birth = {
	effect = {
		capital_county = {
			add_gold = 100
		}
	}
}
`
	c := &catalog.VanillaCache{Vocabulary: []string{"add_gold"}}
	catalog.PrepareCache(c)
	s, root := buildSession(t, "ck3", map[string]string{"common/on_action/x.txt": body}, c, nil)
	if diags := Diagnose(s, root+"/common/on_action/x.txt"); hasDiag(diags, "wrong-scope") {
		t.Errorf("claimed a scope error with no schema: %+v", diags)
	}
}

// An unknown block stops the walk, so nothing below it is judged.
func TestWrongScopeSilentAfterUnknownBlock(t *testing.T) {
	body := `on_birth = {
	effect = {
		mystery_block = {
			add_title_law = x
		}
	}
}
`
	s, p := scopedSession(t, body)
	if diags := Diagnose(s, p); hasDiag(diags, "wrong-scope") {
		t.Errorf("judged script under an unknown transition: %+v", diags)
	}
}

// The suppression comment applies to the new code like any other.
func TestWrongScopeSuppressed(t *testing.T) {
	body := `on_birth = {
	effect = {
		capital_county = {
			add_gold = 100 # pmt:ignore wrong-scope
		}
	}
}
`
	s, p := scopedSession(t, body)
	if diags := Diagnose(s, p); hasDiag(diags, "wrong-scope") {
		t.Errorf("suppression ignored: %+v", diags)
	}
}

// Scripted macros defined in the mod stay available alongside engine tokens.
func TestCompleteKeepsCallDefs(t *testing.T) {
	body := `on_birth = {
	effect = {
		add_
	}
}
`
	s, root := buildSession(t, "ck3", map[string]string{
		"common/on_action/x.txt":        body,
		"common/scripted_effects/e.txt": "add_my_custom_effect = {\n\tadd_gold = 1\n}\n",
	}, scopedCache(), nil)
	line, col := lineCol(body, "\t\tadd_")
	items := Complete(s, root+"/common/on_action/x.txt", line, col+len("\t\tadd_"))
	if !has(items, "add_gold") {
		t.Errorf("engine token missing: %v", labels(items))
	}
	found := false
	for _, l := range labels(items) {
		if strings.HasPrefix(l, "add_my_custom") {
			found = true
		}
	}
	if !found {
		t.Errorf("mod scripted_effect missing: %v", labels(items))
	}
}

// TestWrongScopeOutsideOnActions is the bug every other test in this file
// missed by construction: they all build a common/on_action file, which is the
// only kind that declares its own starting scope. ScopeMisuses bailed on an
// empty root, so the check ran there and NOWHERE else -- not in an event, a
// decision, a scripted effect or a trait, on any of the three games.
//
// ScopeAt was fixed for exactly this and says so in a comment; ScopeMisuses,
// twenty lines away, was not.
func TestWrongScopeOutsideOnActions(t *testing.T) {
	// every_vassal declares Targets: character, so it establishes a character
	// scope with nothing before it. add_title_law is landed_title only.
	const body = "namespace = test\n\ntest.1 = {\n" +
		"\timmediate = {\n\t\tevery_vassal = {\n\t\t\tadd_title_law = x\n\t\t}\n\t}\n}\n"
	s, root := buildSession(t, "ck3",
		map[string]string{"events/x.txt": body}, scopedCache(), nil)
	diags := Diagnose(s, root+"/events/x.txt")
	if !hasDiag(diags, "wrong-scope") {
		t.Fatalf("no wrong-scope inside an event body: %+v", diags)
	}
}

// TestWrongScopeSilentWithoutDeclaredScope: continuing past an empty root must
// not start guessing. Where nothing declared establishes a scope there is
// nothing to contradict, so the check stays quiet.
func TestWrongScopeSilentWithoutDeclaredScope(t *testing.T) {
	const body = "namespace = test\n\ntest.1 = {\n" +
		"\timmediate = {\n\t\tadd_title_law = x\n\t}\n}\n"
	s, root := buildSession(t, "ck3",
		map[string]string{"events/x.txt": body}, scopedCache(), nil)
	if diags := Diagnose(s, root+"/events/x.txt"); hasDiag(diags, "wrong-scope") {
		t.Errorf("judged a scope nothing declared: %+v", diags)
	}
}

// TestWrongScopeRootWithoutDeclaredRoot is nine vanilla CK3 findings, all one
// shape. camp_party_actions.txt writes
//
//	effect = { random_attending_character = { limit = { root = { any_… } } } }
//
// where root is the ACTIVITY the pulse action runs on. Only an on_action
// declares its root scope, so the walk's stack bottom here is just the first hop
// it happened to take -- "character" -- and reading root as stack[0] reported
// correct vanilla script as wrong-scope. An unrooted walk must treat root as a
// scope it cannot name.
func TestWrongScopeRootWithoutDeclaredRoot(t *testing.T) {
	const body = "namespace = test\n\ntest.1 = {\n\timmediate = {\n" +
		"\t\tevery_vassal = {\n\t\t\troot = {\n\t\t\t\tadd_title_law = x\n" +
		"\t\t\t}\n\t\t}\n\t}\n}\n"
	s, root := buildSession(t, "ck3",
		map[string]string{"events/x.txt": body}, scopedCache(), nil)
	if diags := Diagnose(s, root+"/events/x.txt"); hasDiag(diags, "wrong-scope") {
		t.Errorf("judged `root` against a scope nothing declared: %+v", diags)
	}
}

// And where the root IS declared, root still resolves and is still judged.
func TestWrongScopeRootWhenDeclared(t *testing.T) {
	const body = "on_birth = {\n\teffect = {\n\t\tcapital_county = {\n" +
		"\t\t\troot = {\n\t\t\t\tadd_title_law = x\n\t\t\t}\n\t\t}\n\t}\n}\n"
	s, p := scopedSession(t, body)
	if !hasDiag(Diagnose(s, p), "wrong-scope") {
		t.Errorf("on_birth declares a character root; add_title_law is landed_title only")
	}
}

// TestWrongScopeFollowsDottedChain: `root.capital_county = { … }` is two hops,
// and the walk stopped dead at the dot. ck3-tiger models these explicitly
// (open_builder / finalize_builder); PMT was silent rather than wrong, which is
// safe and useless -- a chain is one of the commonest ways to change scope.
func TestWrongScopeFollowsDottedChain(t *testing.T) {
	// on_birth roots at character; .capital_county hops to landed_title, where
	// add_gold does not belong.
	const bad = "on_birth = {\n\teffect = {\n\t\troot.capital_county = {\n" +
		"\t\t\tadd_gold = 1\n\t\t}\n\t}\n}\n"
	s, p := scopedSession(t, bad)
	if !hasDiag(Diagnose(s, p), "wrong-scope") {
		t.Errorf("chain not followed: add_gold is character-only, root.capital_county is a title")
	}

	// The same chain with a title effect is correct and must stay silent.
	const good = "on_birth = {\n\teffect = {\n\t\troot.capital_county = {\n" +
		"\t\t\tadd_title_law = x\n\t\t}\n\t}\n}\n"
	s2, p2 := scopedSession(t, good)
	if hasDiag(Diagnose(s2, p2), "wrong-scope") {
		t.Errorf("false positive on a correct chain")
	}

	// Three hops, back to character: add_gold belongs again.
	const back = "on_birth = {\n\teffect = {\n\t\troot.capital_county.holder = {\n" +
		"\t\t\tadd_gold = 1\n\t\t}\n\t}\n}\n"
	s3, p3 := scopedSession(t, back)
	if hasDiag(Diagnose(s3, p3), "wrong-scope") {
		t.Errorf("false positive after hopping back to character")
	}
}

// TestScopeChainStopsOnUnknownSegment: half a chain is not a scope. A segment
// PMT cannot resolve ends the walk rather than leaving the previous hop's scope
// in place to be judged against.
func TestScopeChainStopsOnUnknownSegment(t *testing.T) {
	const body = "on_birth = {\n\teffect = {\n\t\troot.not_a_link.capital_county = {\n" +
		"\t\t\tadd_gold = 1\n\t\t}\n\t}\n}\n"
	s, p := scopedSession(t, body)
	if hasDiag(Diagnose(s, p), "wrong-scope") {
		t.Errorf("judged a chain it could not follow")
	}
}

// TestScopeChainIgnoresDates: a history file keys blocks by a date, and
// splitting 867.1.1 would step through "867", "1", "1".
func TestScopeChainIgnoresDates(t *testing.T) {
	const body = "on_birth = {\n\teffect = {\n\t\t867.1.1 = {\n" +
		"\t\t\tadd_gold = 1\n\t\t}\n\t}\n}\n"
	s, p := scopedSession(t, body)
	if hasDiag(Diagnose(s, p), "wrong-scope") {
		t.Errorf("treated a date as a scope chain")
	}
}

// TestEventRootFromOnAction: an event does not declare its own scope, but the
// on_action that fires it does. on_actions.log states on_birth runs on a
// character, so a `root` block inside an event on_birth fires is a character.
//
// This is the path TestScopeAtEventTypeIsNotAScope names. Reading the event's
// `type` instead was tried and measured: activity_event and letter_event bodies
// are both character-scoped, so the type is a presentation window, not a scope.
func TestEventRootFromOnAction(t *testing.T) {
	files := map[string]string{
		"common/on_action/x.txt": "on_birth = {\n\tevents = {\n\t\tt.1\n\t}\n}\n",
		"events/y.txt": "namespace = t\n\nt.1 = {\n\timmediate = {\n" +
			"\t\troot = {\n\t\t\tadd_title_law = x\n\t\t}\n\t}\n}\n",
	}
	s, root := buildSession(t, "ck3", files, scopedCache(), nil)
	if !hasDiag(Diagnose(s, root+"/events/y.txt"), "wrong-scope") {
		t.Errorf("on_birth roots at character; add_title_law is landed_title only")
	}
}

// And an event nothing scoped fires keeps no root at all, so `root` stays a
// scope PMT cannot name and nothing is judged against it.
func TestEventWithoutOnActionHasNoRoot(t *testing.T) {
	files := map[string]string{
		"events/y.txt": "namespace = t\n\nt.1 = {\n\timmediate = {\n" +
			"\t\troot = {\n\t\t\tadd_title_law = x\n\t\t}\n\t}\n}\n",
	}
	s, root := buildSession(t, "ck3", files, scopedCache(), nil)
	if hasDiag(Diagnose(s, root+"/events/y.txt"), "wrong-scope") {
		t.Errorf("judged an event root nothing declared")
	}
}

// Two on_actions of different scopes firing one event is a disagreement PMT
// cannot resolve, so it declares no root rather than picking. Vanilla has not a
// single case across the three games, but a mod can create one.
func TestEventRootConflictYieldsNoRoot(t *testing.T) {
	c := scopedCache()
	c.Schema.OnActions["on_title_gain"] = "landed_title"
	files := map[string]string{
		"common/on_action/x.txt": "on_birth = {\n\tevents = {\n\t\tt.1\n\t}\n}\n" +
			"on_title_gain = {\n\tevents = {\n\t\tt.1\n\t}\n}\n",
		"events/y.txt": "namespace = t\n\nt.1 = {\n\timmediate = {\n" +
			"\t\troot = {\n\t\t\tadd_title_law = x\n\t\t}\n\t}\n}\n",
	}
	s, root := buildSession(t, "ck3", files, c, nil)
	if hasDiag(Diagnose(s, root+"/events/y.txt"), "wrong-scope") {
		t.Errorf("picked a root where two on_actions disagree")
	}
}
