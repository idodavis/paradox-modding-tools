// scope_test.go covers the scope walk against a miniature declared type system
// shaped exactly like a real one: scope types from event_scopes.log, links with
// Input/Output, and iterators as effects carrying Supported Targets.

package session_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"paradox-modding-tools/services/internal/catalog"
	"paradox-modding-tools/services/internal/session"
)

// ck3Schema mirrors the shape of a real CK3 dump, trimmed to what the walk needs.
func ck3Schema() *catalog.Schema {
	return &catalog.Schema{
		Scopes: map[string]catalog.ScopeType{
			"none":         {ChangeScopes: true},
			"character":    {ChangeScopes: true, ExecuteEffects: true, EvaluateTriggers: true},
			"landed_title": {ChangeScopes: true, ExecuteEffects: true, EvaluateTriggers: true},
			"culture":      {ChangeScopes: true, ExecuteEffects: true, EvaluateTriggers: true},
			"value":        {},
		},
		Links: map[string]catalog.ScopeLink{
			"liege":          {In: []string{"character"}, Out: "character"},
			"capital_county": {In: []string{"character"}, Out: "landed_title"},
			"holder":         {In: []string{"landed_title"}, Out: "character"},
			"culture":        {Global: true, Data: true, Out: "culture"},
		},
		Effects: map[string]catalog.EngineToken{
			"add_gold":       {In: []string{"character"}},
			"add_title_law":  {In: []string{"landed_title"}},
			"every_vassal":   {In: []string{"character"}, Target: "character"},
			"set_culture":    {In: []string{"character"}, Target: "culture"},
			"add_prestige":   {In: []string{"character"}},
			"destroy_title":  {In: []string{"landed_title"}},
			"change_culture": {In: []string{"culture"}},
		},
		Triggers: map[string]catalog.EngineToken{
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

func scopeSession(t *testing.T, rel, body string) (*session.Session, string) {
	t.Helper()
	return scopeSessionWith(t, ck3Schema(), rel, body)
}

func scopeSessionWith(
	t *testing.T, schema *catalog.Schema, rel, body string,
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
	cache := &catalog.VanillaCache{Schema: schema}
	catalog.PrepareCache(cache)
	s := session.NewWithLoc("ws", "ck3", "english", cache, nil,
		[]catalog.ModInput{{Origin: "mod", Root: root, Order: 0}})
	s.DidOpen(p, body)
	return s, p
}

// at returns the 0-based line/col of the first occurrence of needle.
func at(body, needle string) (int, int) {
	i := indexOf(body, needle)
	if i < 0 {
		return -1, -1
	}
	line, last := 0, -1
	for j := 0; j < i; j++ {
		if body[j] == '\n' {
			line++
			last = j
		}
	}
	return line, i - last - 1
}

func indexOf(hay, needle string) int {
	for i := 0; i+len(needle) <= len(hay); i++ {
		if hay[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
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
	if got := s.ScopeAt(p, line, col); got != "character" {
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
	sc.Scopes["faith"] = catalog.ScopeType{
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
	cache := &catalog.VanillaCache{Schema: sc}
	catalog.PrepareCache(cache)
	s := session.NewWithLoc("ws", "ck3", "english", cache, nil,
		[]catalog.ModInput{{Origin: "mod", Root: root, Order: 0}})
	s.DidOpen(ep, eventBody)

	line, col := at(eventBody, "add_gold = 1")
	if got := s.ScopeAt(ep, line, col); got != "faith" {
		t.Errorf("top-level fire: scope = %q, want faith", got)
	}
	line, col = at(eventBody, "add_gold = 2")
	if got := s.ScopeAt(ep, line, col); got != "" {
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
	sc.Links["modifier"] = catalog.ScopeLink{Global: true, Out: "value, boolean"}
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
	if got := s.ScopeAt(p, line, col); got != "character" {
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
	if got := s.ScopeAt(p, line, col); got != "landed_title" {
		t.Errorf("at holder key, scope = %q, want landed_title", got)
	}
	line, col = at(body, "add_gold")
	if got := s.ScopeAt(p, line, col); got != "character" {
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
	if got := s.ScopeAt(p, line, col); got != "character" {
		t.Errorf("inside every_vassal, scope = %q, want character", got)
	}
	line, col = at(body, "is_adult")
	if got := s.ScopeAt(p, line, col); got != "character" {
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
	if got := s.ScopeAt(p, line, col); got != "character" {
		t.Errorf("inside prev, scope = %q, want character", got)
	}
	line, col = at(body, "add_prestige")
	if got := s.ScopeAt(p, line, col); got != "character" {
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
	if got := s.ScopeAt(p, line, col); got != "" {
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
	if got := s.ScopeAt(p, line, col); got != "" {
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
		if got := s.ScopeAt(p, line, col); got != "" {
			t.Errorf("type = %s gave scope %q; event type is not a scope", typ, got)
		}
	}
}

func TestTokensInScope(t *testing.T) {
	s, _ := scopeSession(t, "events/x.txt", "namespace = t\n")

	got := s.TokensInScope("landed_title", "effect", "", 100)
	if !contains(got, "add_title_law") {
		t.Errorf("landed_title effects = %v, want add_title_law", got)
	}
	if contains(got, "add_gold") {
		t.Errorf("add_gold is character-only but offered in landed_title: %v", got)
	}

	trig := s.TokensInScope("landed_title", "trigger", "", 100)
	if !contains(trig, "has_title_law") || contains(trig, "is_adult") {
		t.Errorf("landed_title triggers = %v", trig)
	}

	// An unknown scope must not hide anything.
	all := s.TokensInScope("", "effect", "", 100)
	if !contains(all, "add_gold") || !contains(all, "add_title_law") {
		t.Errorf("unknown scope filtered: %v", all)
	}

	if pre := s.TokensInScope("character", "effect", "add_", 100); !contains(pre, "add_gold") ||
		contains(pre, "every_vassal") {
		t.Errorf("prefix filter = %v", pre)
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
	s := session.NewWithLoc("ws", "ck3", "english", nil, nil,
		[]catalog.ModInput{{Origin: "mod", Root: root, Order: 0}})
	s.DidOpen(p, body)
	if s.HasSchema() {
		t.Error("HasSchema true with no cache")
	}
	if got := s.SchemaSource(); got != catalog.SchemaMissing {
		t.Errorf("SchemaSource = %q, want missing", got)
	}
	line, col := at(body, "add_gold")
	if got := s.ScopeAt(p, line, col); got != "" {
		t.Errorf("ScopeAt = %q with no schema, want empty", got)
	}
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
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

// The scope model must not accuse the game's own script of being wrong.
// Vanilla is the answer key: every wrong-scope report over it is a false
// positive. This is the test that keeps the diagnostic honest as the schema
// changes across patches.
func checkNoFalsePositives(t *testing.T, gameID, installEnv, docsEnv, scriptRoot string) {
	t.Helper()
	install, docs := realInstall(t, installEnv, docsEnv)
	schema := catalog.ReadSchema([]string{docs})
	if schema == nil {
		t.Fatalf("no schema under %s", docs)
	}
	root := filepath.Join(install, scriptRoot)
	cache := &catalog.VanillaCache{Schema: schema}
	catalog.PrepareCache(cache)

	s := session.NewWithLoc("ws", gameID, "english", cache, nil,
		[]catalog.ModInput{{Origin: "mod", Root: root, Order: 0}})

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
		for _, m := range s.ScopeMisuses(p) {
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

func TestScopeNoFalsePositivesCK3(t *testing.T) {
	checkNoFalsePositives(t, "ck3", "PMT_TEST_INSTALL_CK3", "PMT_TEST_DOCS_CK3", "game")
}

func TestScopeNoFalsePositivesVic3(t *testing.T) {
	checkNoFalsePositives(t, "vic3", "PMT_TEST_INSTALL_VIC3", "PMT_TEST_DOCS_VIC3", "game")
}

func TestScopeNoFalsePositivesEU5(t *testing.T) {
	checkNoFalsePositives(t, "eu5", "PMT_TEST_INSTALL_EU5", "PMT_TEST_DOCS_EU5", "game")
}

// corpusEnv skips when the real-install tier was not requested, and FAILS when
// it was requested but the environment cannot serve it.
//
// A tier that can silently be empty is not a tier. With PMT_TEST_TIER unset a
// missing variable means "not run" and a skip is right; with PMT_TEST_TIER=full
// it means "cannot run", and a green suite that quietly executed none of the
// corpus tests is indistinguishable from a correct one. A typo in
// PMT_TEST_DOCS_CK3 used to produce exactly that.
func corpusEnv(t *testing.T, format string, args ...any) {
	t.Helper()
	if os.Getenv("PMT_TEST_TIER") == "full" {
		t.Fatalf("PMT_TEST_TIER=full but "+format, args...)
	}
	t.Skipf(format, args...)
}
