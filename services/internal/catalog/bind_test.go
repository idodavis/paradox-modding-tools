// bind_test.go checks that the declared type system binds to the harvested
// databases. The synthetic cases run everywhere. The real-install cases walk an
// actual game and are gated on PMT_TEST_INSTALL_{CK3,EU5,VIC3} plus the matching
// PMT_TEST_DOCS_*; they parse the whole script root, so they are slow and skip
// under -short.

package catalog

import (
	"context"
	"os"
	"slices"
	"sync"
	"testing"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/parser/jomini"
)

func TestBindKindsJoin(t *testing.T) {
	s := &Schema{Links: map[string]ScopeLink{
		"cu": {Global: true, Data: true, Out: "culture"},
		"bg": {Global: true, Data: true, Out: "building_group"},
		// Declared but never cited in this corpus, and no folder matches.
		"zz": {Global: true, Data: true, Out: "nothing_at_all"},
	}}
	defs := []Def{
		{Kind: "cultures", Key: "english"},
		{Kind: "cultures", Key: "french"},
		{Kind: "cultures", Key: "german"},
		{Kind: "building_groups", Key: "bg_mining"},
		{Kind: "building_groups", Key: "bg_farming"},
		{Kind: "building_groups", Key: "bg_arts"},
		{Kind: "decisions", Key: "some_decision"},
	}
	cites := []typedCite{
		{"cu", "english"}, {"cu", "french"}, {"cu", "german"},
		{"bg", "bg_mining"}, {"bg", "bg_farming"}, {"bg", "bg_arts"},
	}

	prefixKind, kindScope, bound := bindKinds(s, newDefIndex(defs), cites)
	if prefixKind["cu"] != "cultures" {
		t.Errorf("cu -> %q, want cultures", prefixKind["cu"])
	}
	if prefixKind["bg"] != "building_groups" {
		t.Errorf("bg -> %q, want building_groups", prefixKind["bg"])
	}
	if kindScope["cultures"] != "culture" {
		t.Errorf("kindScope[cultures] = %q, want culture", kindScope["cultures"])
	}
	if _, ok := prefixKind["zz"]; ok {
		t.Error("zz bound with no evidence")
	}
	for _, b := range bound {
		if b.Prefix == "cu" && b.Coverage != 1 {
			t.Errorf("cu coverage = %v, want 1", b.Coverage)
		}
	}
}

// Too few citations must leave the type unbound rather than guess.
func TestBindKindsEvidenceFloor(t *testing.T) {
	s := &Schema{Links: map[string]ScopeLink{
		"xx": {Global: true, Data: true, Out: "widget"},
	}}
	defs := []Def{{Kind: "gadgets", Key: "a"}, {Kind: "gadgets", Key: "b"}}
	cites := []typedCite{{"xx", "a"}, {"xx", "b"}}
	prefixKind, _, _ := bindKinds(s, newDefIndex(defs), cites)
	if _, ok := prefixKind["xx"]; ok {
		t.Errorf("bound on %d cites, floor is %d", len(cites), minBindCites)
	}
}

// With no citations at all, a singular/plural name match is still deterministic.
func TestBindKindsNameFallback(t *testing.T) {
	s := &Schema{Links: map[string]ScopeLink{
		"trait": {Global: true, Data: true, Out: "trait"},
	}}
	defs := []Def{{Kind: "traits", Key: "brave"}}
	prefixKind, kindScope, bound := bindKinds(s, newDefIndex(defs), nil)
	if prefixKind["trait"] != "traits" {
		t.Fatalf("trait -> %q, want traits", prefixKind["trait"])
	}
	if kindScope["traits"] != "trait" {
		t.Errorf("kindScope = %v", kindScope)
	}
	if len(bound) != 1 || !bound[0].ByName {
		t.Errorf("want a name-matched binding, got %+v", bound)
	}
}

// A prefix whose citations mostly miss every database does NOT stay unbound: it
// falls through to an unfloored singular/plural name match. See I.2g.
func TestBindKindsBelowFloorFallsBackToNameMatch(t *testing.T) {
	s := &Schema{Links: map[string]ScopeLink{
		"p": {Global: true, Data: true, Out: "province"},
	}}
	defs := []Def{{Kind: "provinces", Key: "known"}}
	// p:hex ids are engine-side, not database rows.
	cites := []typedCite{
		{"p", "known"}, {"p", "a1b2"}, {"p", "c3d4"}, {"p", "e5f6"}, {"p", "0089"},
	}
	prefixKind, _, _ := bindKinds(s, newDefIndex(defs), cites)
	// One of five hits is well under minBindCites, so the citation branch
	// declines — and control then falls through to nameMatchKind, which binds on
	// a SINGLE definition in a folder whose name matches the scope type.
	//
	// The previous version of this test asserted nothing: its only check was a
	// t.Logf, so a test named "…BelowFloorUnbound" whose own comment said "binds
	// by name instead" could not fail either way.
	//
	// The fallback is correct and stays. A floor for nameMatchKind was proposed
	// and then refuted by measurement: across all three vanilla installs every
	// one of the 18 name-matched bindings is a real folder — struggle/struggles,
	// road_type/road_types, province/provinces — so a floor would have deleted
	// legitimate bindings and caught no guesses. minBindCites' comment was what
	// was wrong, and it has been corrected.
	if got := prefixKind["p"]; got != "provinces" {
		t.Errorf("p -> %q, want %q (unfloored name fallback; see plan I.2g)",
			got, "provinces")
	}
}

// --- Real-install tier -------------------------------------------------------

type realGame struct {
	id, installEnv, docsEnv string
	// wantPrefix is prefix -> expected database kind.
	wantPrefix map[string]string
}

// realSchema reads a real game's type system the way the app does, from every
// directory scriptDocsDirs names.
//
// A single directory cannot express the layout. CK3 dumps all six files to
// `logs/`; Vic3 and EU5 write five to `docs/` and leave only `event_scopes.log`
// in `logs/`. So PMT_TEST_DOCS_* pointed at either one yields a schema that is
// non-nil and half missing — and a tier measuring the derived half against a
// partial declared half reports confident, wrong numbers. The env var is kept
// as an override, and added to the list rather than replacing it.
func realSchema(t *testing.T, gameID, install, docs, docsEnv string) *Schema {
	t.Helper()
	dirs := scriptDocsDirs(gameID, install)
	if docs != "" {
		dirs = append(dirs, docs)
	}
	s := ReadSchema(dirs)
	if s == nil || len(s.Effects) == 0 || len(s.Triggers) == 0 || len(s.Links) == 0 {
		t.Fatalf("%s: incomplete type system from %v (effects/triggers live in "+
			"docs/, event_scopes.log in logs/); set %s if the dumps are elsewhere",
			gameID, dirs, docsEnv)
	}
	return s
}

func realBind(t *testing.T, g realGame) (map[string]string, map[string]string, []KindBinding) {
	t.Helper()
	if testing.Short() {
		t.Skip("walks a full game install")
	}
	install := os.Getenv(g.installEnv)
	// The docs env is an override now, not a requirement: scriptDocsDirs finds
	// the dumps from the install the same way the app does.
	docs := os.Getenv(g.docsEnv)
	if install == "" {
		corpusEnv(t, "%s not set", g.installEnv)
	}
	if _, err := os.Stat(install); err != nil {
		t.Skipf("%s: %v", g.installEnv, err)
	}
	s := realSchema(t, g.id, install, docs, g.docsEnv)

	// Go through the real pipeline: nested databases (CK3 faiths, sub-tier
	// titles) only become defs after applyDerivedDefs, and collectExtracts is
	// what sequences that against the bind.
	info := game.Get(g.id)
	inv := gather(scriptRoots(info, install))
	files := append(append([]fileRef{}, inv.script...), inv.gui...)
	if len(files) == 0 {
		t.Fatalf("no script files under %s", install)
	}
	acc, derived, _, err := collectExtracts(context.Background(), g.id, files, true, nil, s, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s: %d files, %d defs", g.id, len(files), len(acc.defs))

	// Recompute the bindings from the same inputs so the evidence is visible.
	var cites []typedCite
	var citeMu sync.Mutex
	corp := corpus{ctx: context.Background(), gameID: g.id, files: files}
	if err := corp.walk(func(_ fileRef, res jomini.Result) error {
		c := collectTypedCites(res.Root)
		citeMu.Lock()
		defer citeMu.Unlock()
		cites = append(cites, c...)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	_, _, bound := bindKinds(s, newDefIndex(acc.defs), cites)
	logUnexplained(t, acc.defs, cites, bound)
	joined, named := 0, 0
	for _, b := range bound {
		if b.ByName {
			named++
		} else {
			joined++
		}
	}
	t.Logf("bound %d types (%d by citation join, %d by name), %d databases typed",
		len(bound), joined, named, len(derived.KindScope))
	return derived.PrefixKinds, derived.KindScope, bound
}

// logUnexplained samples the citations a weak binding could not account for.
// A weak join usually means either a harvest gap or references the game
// resolves at runtime; the sample says which.
func logUnexplained(t *testing.T, defs []Def, cites []typedCite, bound []KindBinding) {
	t.Helper()
	known := map[string]bool{}
	for _, d := range defs {
		known[d.Key] = true
	}
	byPrefix := map[string]map[string]bool{}
	for _, c := range cites {
		if byPrefix[c.prefix] == nil {
			byPrefix[c.prefix] = map[string]bool{}
		}
		byPrefix[c.prefix][c.id] = true
	}
	for _, b := range bound {
		if b.ByName || b.Coverage >= 0.8 || b.Cites < 20 {
			continue
		}
		var miss []string
		for id := range byPrefix[b.Prefix] {
			if !known[id] {
				miss = append(miss, id)
			}
		}
		slices.Sort(miss)
		if len(miss) > 12 {
			miss = miss[:12]
		}
		t.Logf("  %s: %d ids match no def at all; sample %v", b.Prefix, len(miss), miss)
	}
}

func checkBindings(t *testing.T, g realGame) {
	t.Helper()
	prefixKind, kindScope, bound := realBind(t, g)
	for prefix, wantKind := range g.wantPrefix {
		if got := prefixKind[prefix]; got != wantKind {
			t.Errorf("prefix %q -> %q, want %q", prefix, got, wantKind)
		}
	}
	// A citation-joined binding should explain most of what it claims. These
	// are declared types matched against the folder holding them, so a weak
	// join means the database is split or the wrong folder won.
	for _, b := range bound {
		if !b.ByName && b.Cites >= 20 && b.Coverage < 0.8 {
			t.Errorf("weak join %s -> %v: %.2f over %d cites",
				b.Prefix, b.Kinds, b.Coverage, b.Cites)
		}
		if len(b.Kinds) > 1 {
			t.Logf("%s (%s) spans %v; owners %v: %.2f over %d cites",
				b.Prefix, b.Scope, b.Kinds, b.Owners(), b.Coverage, b.Cites)
		}
	}
	if len(kindScope) < 10 {
		t.Errorf("only %d databases typed; expected the schema to cover many", len(kindScope))
	}
}

func TestBindRealCK3(t *testing.T) {
	checkBindings(t, realGame{
		id: "ck3", installEnv: "PMT_TEST_INSTALL_CK3", docsEnv: "PMT_TEST_DOCS_CK3",
		wantPrefix: map[string]string{
			"culture": "cultures",
			// Faiths are nested under religions, so the database kind is the
			// nested child name rather than a folder.
			"faith": "faith",
			"trait": "traits",
		},
	})
}

func TestBindRealVic3(t *testing.T) {
	checkBindings(t, realGame{
		id: "vic3", installEnv: "PMT_TEST_INSTALL_VIC3", docsEnv: "PMT_TEST_DOCS_VIC3",
		wantPrefix: map[string]string{
			"cu": "cultures",
			"bg": "building_groups",
			"bt": "buildings",
		},
	})
}

func TestBindRealEU5(t *testing.T) {
	checkBindings(t, realGame{
		id: "eu5", installEnv: "PMT_TEST_INSTALL_EU5", docsEnv: "PMT_TEST_DOCS_EU5",
		wantPrefix: map[string]string{
			"goods":         "goods",
			"building_type": "building_types",
		},
	})
}
