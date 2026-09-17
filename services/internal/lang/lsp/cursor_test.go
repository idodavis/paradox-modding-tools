// cursor_test.go covers ScopeAt, event root scopes, and scope-aware completion: with the game's declared type
// system loaded, only effects and triggers the game says are usable in the
// current scope are offered.

package lsp

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"paradox-modding-tools/services/internal/lang/declared"
	"paradox-modding-tools/services/internal/lang/derived"
	"paradox-modding-tools/services/internal/lang/session"
)

func scopedCache() *vanillaCache {
	c := &vanillaCache{
		Schema: &declared.Schema{
			Scopes: map[string]declared.ScopeType{
				"character":    {ChangeScopes: true, ExecuteEffects: true},
				"landed_title": {ChangeScopes: true, ExecuteEffects: true},
			},
			Links: map[string]declared.ScopeLink{
				"capital_county": {In: []string{"character"}, Out: "landed_title"},
				"holder":         {In: []string{"landed_title"}, Out: "character"},
			},
			Effects: map[string]declared.EngineToken{
				"add_gold":      {In: []string{"character"}, Doc: "Adds gold"},
				"add_title_law": {In: []string{"landed_title"}, Doc: "Adds a title law"},
				"every_vassal":  {In: []string{"character"}, Target: "character"},
			},
			Triggers: map[string]declared.EngineToken{
				"is_adult":      {In: []string{"character"}},
				"has_title_law": {In: []string{"landed_title"}},
			},
			// on_birth is a character on_action, per on_actions.log; that is
			// what seeds the walk's root scope.
			OnActions: map[string]string{"on_birth": "character"},
		},
	}
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
	c := &vanillaCache{
		Vocabulary: []string{"add_gold", "add_title_law", "is_adult"},
	}
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

func TestScopeAtOnActionRoot(t *testing.T) {
	body := `on_birth = {
	effect = {
		add_gold = 5
	}
}
`
	s, p := scopeSession(t, "common/on_action/x.txt", body)
	line, col := at(body, "add_gold")
	if got := ScopeAt(s, p, line, col); got != "character" {
		t.Errorf("ScopeAt = %q, want character", got)
	}
}

// An event's root scope comes from the on_action that fires it — but only when
// the fire site is at the on_action's own scope level.
//
// CK3's on_faith_monthly is faith-scoped and reaches
// `trigger_event = global_religion.1000` fourteen blocks deep, inside
// character-scoped iterators, so the event runs on a character. Reading the
// on_action's declaration through that nesting reported 24 lines of correct
// vanilla CK3 as wrong-scope. The edge itself is right and the Event Graph is
// unaffected; only the scope inference has to decline.
//
// This became reachable when defContainers started attributing an edge to the
// object that encloses it instead of to a saved scope (plan I.1b): before that,
// the nested fire site produced an edge from `scope:ruler_target`, which names
// no on_action, so no root scope was derived and the wrong premise never fired.
func TestEventRootScopeIgnoresRescopedFireSite(t *testing.T) {
	sc := ck3Schema()
	sc.OnActions["on_faith_monthly"] = "faith"
	sc.Scopes["faith"] = declared.ScopeType{
		ChangeScopes: true, ExecuteEffects: true, EvaluateTriggers: true,
	}
	root := t.TempDir()
	write := func(rel, body string) string {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	// Fired at the on_action's own level, and again from inside an iterator.
	write("common/on_action/faith.txt", `on_faith_monthly = {
	events = {
		faith_at_root.1
	}
	effect = {
		every_vassal = {
			trigger_event = faith_nested.1
		}
	}
}
`)
	eventBody := `namespace = faith_at_root

faith_at_root.1 = {
	type = character_event
	immediate = {
		add_gold = 1
	}
}

namespace = faith_nested

faith_nested.1 = {
	type = character_event
	immediate = {
		add_gold = 2
	}
}
`
	ep := write("events/faith.txt", eventBody)
	if err := os.WriteFile(filepath.Join(root, "descriptor.mod"),
		[]byte("name=\"t\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cache := &vanillaCache{Schema: sc}
	s := session.New("ws", "ck3", "english", toIndex(cache, nil),
		[]derived.ModInput{{Origin: "mod", Root: root, Order: 0}})
	s.DidOpen(ep, eventBody)

	line, col := at(eventBody, "add_gold = 1")
	if got := ScopeAt(s, ep, line, col); got != "faith" {
		t.Errorf("top-level fire: scope = %q, want faith", got)
	}
	line, col = at(eventBody, "add_gold = 2")
	if got := ScopeAt(s, ep, line, col); got != "" {
		t.Errorf("fire site nested under every_vassal: scope = %q, want none", got)
	}
}

// A link that declares several output types names no scope, so the walk stops
// rather than adopting the list as a scope name. EU5's `modifier` link outputs
// "value, boolean", and every weight block in every game opens with
// `modifier = { … }` — three lines of correct vanilla EU5 were reported as
// wrong-scope in a scope called "value, boolean".
func TestScopeAtMultiOutputLinkIsNotAHop(t *testing.T) {
	sc := ck3Schema()
	sc.Links["modifier"] = declared.ScopeLink{Global: true, Out: "value, boolean"}
	body := `on_birth = {
	weight_multiplier = {
		modifier = {
			add_gold = 5
		}
	}
}
`
	s, p := scopeSessionWith(t, sc, "common/on_action/x.txt", body)
	line, col := at(body, "add_gold")
	// modifier is scope-neutral, so the character root still holds. What must
	// NOT happen is the link being taken and the scope becoming its output list.
	if got := ScopeAt(s, p, line, col); got != "character" {
		t.Errorf("ScopeAt = %q, want character", got)
	}
}

func TestScopeAtFollowsLinks(t *testing.T) {
	body := `on_birth = {
	effect = {
		capital_county = {
			holder = {
				add_gold = 50
			}
		}
	}
}
`
	s, p := scopeSession(t, "common/on_action/x.txt", body)

	line, col := at(body, "holder = {")
	if got := ScopeAt(s, p, line, col); got != "landed_title" {
		t.Errorf("at holder key, scope = %q, want landed_title", got)
	}
	line, col = at(body, "add_gold")
	if got := ScopeAt(s, p, line, col); got != "character" {
		t.Errorf("inside holder, scope = %q, want character", got)
	}
}

// Iterators change scope through Supported Targets, not through their prefix.
func TestScopeAtIterator(t *testing.T) {
	body := `on_birth = {
	effect = {
		every_vassal = {
			limit = { is_adult = yes }
			add_prestige = 10
		}
	}
}
`
	s, p := scopeSession(t, "common/on_action/x.txt", body)
	line, col := at(body, "add_prestige")
	if got := ScopeAt(s, p, line, col); got != "character" {
		t.Errorf("inside every_vassal, scope = %q, want character", got)
	}
	line, col = at(body, "is_adult")
	if got := ScopeAt(s, p, line, col); got != "character" {
		t.Errorf("inside limit, scope = %q, want character", got)
	}
}

func TestScopeAtPrevAndRoot(t *testing.T) {
	body := `on_birth = {
	effect = {
		capital_county = {
			prev = {
				add_gold = 1
			}
			root = {
				add_prestige = 1
			}
		}
	}
}
`
	s, p := scopeSession(t, "common/on_action/x.txt", body)
	line, col := at(body, "add_gold")
	if got := ScopeAt(s, p, line, col); got != "character" {
		t.Errorf("inside prev, scope = %q, want character", got)
	}
	line, col = at(body, "add_prestige")
	if got := ScopeAt(s, p, line, col); got != "character" {
		t.Errorf("inside root, scope = %q, want character", got)
	}
}

// An on_action the game declares as "none" carries no usable root scope.
func TestScopeAtOnActionNoneScope(t *testing.T) {
	body := `on_game_started = {
	effect = {
		add_gold = 5
	}
}
`
	s, p := scopeSession(t, "common/on_action/x.txt", body)
	line, col := at(body, "add_gold")
	if got := ScopeAt(s, p, line, col); got != "" {
		t.Errorf("ScopeAt = %q, want empty for Expected Scope none", got)
	}
}

// An unknown transition must yield "", meaning "do not filter" rather than a
// wrong scope that would hide valid completions.
func TestScopeAtUnknownStopsWalk(t *testing.T) {
	body := `on_birth = {
	effect = {
		some_unknown_block = {
			add_gold = 1
		}
	}
}
`
	s, p := scopeSession(t, "common/on_action/x.txt", body)
	line, col := at(body, "add_gold")
	if got := ScopeAt(s, p, line, col); got != "" {
		t.Errorf("ScopeAt = %q, want empty for an unknown transition", got)
	}
}

// An event's `type` names its presentation window, not its scope. CK3
// activity_event and letter_event bodies are character-scoped, so reading the
// type as a scope produced thousands of false reports over vanilla. Events must
// stay unscoped until their root is derived from the on_actions that fire them.
func TestScopeAtEventTypeIsNotAScope(t *testing.T) {
	for _, typ := range []string{"character_event", "activity_event", "letter_event"} {
		body := "namespace = t\n\nt.1 = {\n\ttype = " + typ +
			"\n\timmediate = {\n\t\tadd_gold = 1\n\t}\n}\n"
		s, p := scopeSession(t, "events/x.txt", body)
		line, col := at(body, "add_gold")
		if got := ScopeAt(s, p, line, col); got != "" {
			t.Errorf("type = %s gave scope %q; event type is not a scope", typ, got)
		}
	}
}

func TestSchemaAbsentDisablesScopes(t *testing.T) {
	root := t.TempDir()
	body := "namespace = t\n\nt.1 = {\n\ttype = character_event\n\timmediate = { add_gold = 1 }\n}\n"
	p := filepath.Join(root, "events", "x.txt")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	s := session.New("ws", "ck3", "english", nil,
		[]derived.ModInput{{Origin: "mod", Root: root, Order: 0}})
	s.DidOpen(p, body)
	if !s.Schema().Empty() {
		t.Error("HasSchema true with no cache")
	}
	if got := s.Schema().SourceKind(); got != declared.SchemaMissing {
		t.Errorf("SchemaSource = %q, want missing", got)
	}
	line, col := at(body, "add_gold")
	if got := ScopeAt(s, p, line, col); got != "" {
		t.Errorf("ScopeAt = %q with no Schema, want empty", got)
	}
}

func checkNoFalsePositives(t *testing.T, gameID, installEnv, docsEnv, scriptRoot string) {
	t.Helper()
	install, docs := realInstall(t, installEnv, docsEnv)
	Schema := declared.ReadSchema([]string{docs})
	if Schema == nil {
		t.Fatalf("no Schema under %s", docs)
	}
	root := filepath.Join(install, scriptRoot)
	cache := &vanillaCache{Schema: Schema}

	s := session.New("ws", gameID, "english", toIndex(cache, nil),
		[]derived.ModInput{{Origin: "mod", Root: root, Order: 0}})

	// Walk the whole script root rather than assuming a fixed layout: EU5 nests
	// events/common under game/in_game/, not game/ directly.
	var files []string
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(strings.ToLower(d.Name()), ".txt") {
			return nil
		}
		slash := strings.ReplaceAll(p, "\\", "/")
		if strings.Contains(slash, "/events/") ||
			strings.Contains(slash, "/on_action/") || strings.Contains(slash, "/on_actions/") {
			files = append(files, p)
		}
		return nil
	})
	if len(files) == 0 {
		t.Skipf("no vanilla script under %s", root)
	}

	flagged := 0
	seen := map[string]int{}
	for _, p := range files {
		for _, m := range ScopeMisuses(s, p) {
			flagged++
			seen[m.Key+" in "+m.Scope]++
		}
	}
	t.Logf("%s: scanned %d files, %d scope reports", gameID, len(files), flagged)
	shown := 0
	for k, n := range seen {
		if shown >= 15 {
			break
		}
		t.Logf("  %s x%d", k, n)
		shown++
	}
	if flagged > 0 {
		t.Errorf("%s: %d false positives over vanilla script", gameID, flagged)
	}
}

// The scope model must not accuse the game's own script of being wrong.
// Vanilla is the answer key: every wrong-scope report over it is a false
// positive. This is the test that keeps the diagnostic honest as the Schema
// changes across patches.
func TestScopeNoFalsePositivesCK3(t *testing.T) {
	checkNoFalsePositives(t, "ck3", "PMT_TEST_INSTALL_CK3", "PMT_TEST_DOCS_CK3", "game")
}

func TestScopeNoFalsePositivesVic3(t *testing.T) {
	checkNoFalsePositives(t, "vic3", "PMT_TEST_INSTALL_VIC3", "PMT_TEST_DOCS_VIC3", "game")
}

func TestScopeNoFalsePositivesEU5(t *testing.T) {
	checkNoFalsePositives(t, "eu5", "PMT_TEST_INSTALL_EU5", "PMT_TEST_DOCS_EU5", "game")
}

// writeFiles writes a fixture tree and returns its root. A copy of session's,
// because a test fixture belongs to the package that uses it.
func writeFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// TestDottedSegmentAt covers splitting a scope chain so each hop can be named.
// `holder.culture = { … }` walks to the holder and then to their culture, and a
// reader hovering either half is asking about that half; before this the whole
// string resolved to nothing and no card appeared at all.
func lineColOf(src, needle string) (int, int) {
	for i, ln := range strings.Split(src, "\n") {
		if c := strings.Index(ln, needle); c >= 0 {
			return i, c
		}
	}
	return 0, 0
}

func scopeSession(t *testing.T, rel, body string) (*session.Session, string) {
	t.Helper()
	return scopeSessionWith(t, ck3Schema(), rel, body)
}

// at returns the 0-based line/col of the first occurrence of needle.
func at(body, needle string) (int, int) {
	i := indexOf(body, needle)
	if i < 0 {
		return -1, -1
	}
	line, last := 0, -1
	for j := range i {
		if body[j] == '\n' {
			line++
			last = j
		}
	}
	return line, i - last - 1
}

// ck3Schema mirrors the shape of a real CK3 dump, trimmed to what the walk needs.
func ck3Schema() *declared.Schema {
	return &declared.Schema{
		Scopes: map[string]declared.ScopeType{
			"none":         {ChangeScopes: true},
			"character":    {ChangeScopes: true, ExecuteEffects: true, EvaluateTriggers: true},
			"landed_title": {ChangeScopes: true, ExecuteEffects: true, EvaluateTriggers: true},
			"culture":      {ChangeScopes: true, ExecuteEffects: true, EvaluateTriggers: true},
			"value":        {},
		},
		Links: map[string]declared.ScopeLink{
			"liege":          {In: []string{"character"}, Out: "character"},
			"capital_county": {In: []string{"character"}, Out: "landed_title"},
			"holder":         {In: []string{"landed_title"}, Out: "character"},
			"culture":        {Global: true, Data: true, Out: "culture"},
		},
		Effects: map[string]declared.EngineToken{
			"add_gold":       {In: []string{"character"}},
			"add_title_law":  {In: []string{"landed_title"}},
			"every_vassal":   {In: []string{"character"}, Target: "character"},
			"set_culture":    {In: []string{"character"}, Target: "culture"},
			"add_prestige":   {In: []string{"character"}},
			"destroy_title":  {In: []string{"landed_title"}},
			"change_culture": {In: []string{"culture"}},
		},
		Triggers: map[string]declared.EngineToken{
			"is_adult":      {In: []string{"character"}},
			"any_vassal":    {In: []string{"character"}, Target: "character"},
			"has_title_law": {In: []string{"landed_title"}},
		},
		OnActions: map[string]string{
			"on_birth":        "character",
			"on_game_started": "none",
		},
	}
}

func scopeSessionWith(
	t *testing.T, Schema *declared.Schema, rel, body string,
) (*session.Session, string) {
	t.Helper()
	root := t.TempDir()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "descriptor.mod"), []byte("name=\"t\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cache := &vanillaCache{Schema: Schema}
	s := session.New("ws", "ck3", "english", toIndex(cache, nil),
		[]derived.ModInput{{Origin: "mod", Root: root, Order: 0}})
	s.DidOpen(p, body)
	return s, p
}

func indexOf(hay, needle string) int {
	for i := 0; i+len(needle) <= len(hay); i++ {
		if hay[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

func TestTokensInScope(t *testing.T) {
	s, _ := scopeSession(t, "events/x.txt", "namespace = t\n")

	got := s.Schema().TokensInScope("landed_title", "effect", "", 100)
	if !contains(got, "add_title_law") {
		t.Errorf("landed_title effects = %v, want add_title_law", got)
	}
	if contains(got, "add_gold") {
		t.Errorf("add_gold is character-only but offered in landed_title: %v", got)
	}

	trig := s.Schema().TokensInScope("landed_title", "trigger", "", 100)
	if !contains(trig, "has_title_law") || contains(trig, "is_adult") {
		t.Errorf("landed_title triggers = %v", trig)
	}

	// An unknown scope must not hide anything.
	all := s.Schema().TokensInScope("", "effect", "", 100)
	if !contains(all, "add_gold") || !contains(all, "add_title_law") {
		t.Errorf("unknown scope filtered: %v", all)
	}

	if pre := s.Schema().TokensInScope("character", "effect", "add_", 100); !contains(pre, "add_gold") ||
		contains(pre, "every_vassal") {
		t.Errorf("prefix filter = %v", pre)
	}
}

func contains(list []string, want string) bool {
	return slices.Contains(list, want)
}

// checks the scope walk against real vanilla script. Vanilla
// is correct by definition, so any wrong-scope report over it is a false
// positive. Gated on PMT_TEST_INSTALL_CK3 + PMT_TEST_DOCS_CK3; slow.
func realInstall(t *testing.T, installEnv, docsEnv string) (install, docs string) {
	t.Helper()
	if testing.Short() {
		t.Skip("walks a full game install")
	}
	install, docs = os.Getenv(installEnv), os.Getenv(docsEnv)
	if install == "" || docs == "" {
		corpusEnv(t, "%s / %s not set", installEnv, docsEnv)
	}
	if _, err := os.Stat(install); err != nil {
		t.Skipf("%s: %v", installEnv, err)
	}
	return install, docs
}

func corpusEnv(t *testing.T, format string, args ...any) {
	t.Helper()
	if os.Getenv("PMT_TEST_TIER") == "full" {
		t.Fatalf("PMT_TEST_TIER=full but "+format, args...)
	}
	t.Skipf(format, args...)
}
