// bind_test.go checks that the declared type system binds to the harvested
// databases. The synthetic cases run everywhere. The real-install cases walk an
// actual game and are gated on PMT_TEST_INSTALL_{CK3,EU5,VIC3} plus the matching
// PMT_TEST_DOCS_*; they parse the whole script root, so they are slow and skip
// under -short.

package derived

import (
	"context"
	"os"
	"testing"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/declared"
)

// bindKinds matches by exact folder name now, singular or plural, only.
func TestBindKindsNameMatch(t *testing.T) {
	s := &declared.Schema{Links: map[string]declared.ScopeLink{
		"cu": {Global: true, Data: true, Out: "culture"},
		"bg": {Global: true, Data: true, Out: "building_group"},
		// Declared, but no folder of that name exists (singular, plural or bare).
		"zz": {Global: true, Data: true, Out: "nothing_at_all"},
	}}
	defs := []Def{
		{Kind: "cultures", Key: "english"},
		{Kind: "building_groups", Key: "bg_mining"},
		{Kind: "decisions", Key: "some_decision"},
	}
	prefixKind, kindScope := bindKinds(s, newDefIndex(defs))
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
		t.Error("zz bound with no matching folder")
	}
}

// A singular scope matches a plural folder, deterministically.
func TestBindKindsSingularPluralMatch(t *testing.T) {
	s := &declared.Schema{Links: map[string]declared.ScopeLink{
		"trait": {Global: true, Data: true, Out: "trait"},
	}}
	defs := []Def{{Kind: "traits", Key: "brave"}}
	prefixKind, kindScope := bindKinds(s, newDefIndex(defs))
	if prefixKind["trait"] != "traits" {
		t.Fatalf("trait -> %q, want traits", prefixKind["trait"])
	}
	if kindScope["traits"] != "trait" {
		t.Errorf("kindScope = %v", kindScope)
	}
}

// A scope with no folder of a matching name stays unbound rather than guessed.
func TestBindKindsNoMatchStaysUnbound(t *testing.T) {
	s := &declared.Schema{Links: map[string]declared.ScopeLink{
		"xx": {Global: true, Data: true, Out: "widget"},
	}}
	defs := []Def{{Kind: "gadgets", Key: "a"}, {Kind: "gadgets", Key: "b"}}
	prefixKind, _ := bindKinds(s, newDefIndex(defs))
	if got, ok := prefixKind["xx"]; ok {
		t.Errorf("xx bound to %q with no folder named widget/widgets", got)
	}
}

// A `_type` scope binds to its stem's folder when its own name matches nothing:
// Vic3's `building_type` names the `buildings` folder, not `building_types`.
func TestBindKindsTypeSuffixMatchesStem(t *testing.T) {
	s := &declared.Schema{Links: map[string]declared.ScopeLink{
		"bt": {Global: true, Data: true, Out: "building_type"},
	}}
	defs := []Def{{Kind: "buildings", Key: "building_port"}}
	prefixKind, kindScope := bindKinds(s, newDefIndex(defs))
	if prefixKind["bt"] != "buildings" {
		t.Fatalf("bt -> %q, want buildings", prefixKind["bt"])
	}
	if kindScope["buildings"] != "building_type" {
		t.Errorf("kindScope = %v", kindScope)
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
func realSchema(t *testing.T, gameID, install, docs, docsEnv string) *declared.Schema {
	t.Helper()
	dirs := scriptDocsDirs(gameID, install)
	if docs != "" {
		dirs = append(dirs, docs)
	}
	s := declared.ReadSchema(dirs)
	if s == nil || len(s.Effects) == 0 || len(s.Triggers) == 0 || len(s.Links) == 0 {
		t.Fatalf("%s: incomplete type system from %v (effects/triggers live in "+
			"docs/, event_scopes.log in logs/); set %s if the dumps are elsewhere",
			gameID, dirs, docsEnv)
	}
	return s
}

// corpusEnv skips when the real-install tier was not requested, and FAILS when
// it was requested but the environment cannot serve it.
//
// A tier that can silently be empty is not a tier. With PMT_TEST_TIER unset a
// missing variable means "not run" and a skip is right; with PMT_TEST_TIER=full
// it means "cannot run", and a green suite that quietly executed none of the
// corpus tests is indistinguishable from a correct one.
func corpusEnv(t *testing.T, format string, args ...any) {
	t.Helper()
	if os.Getenv("PMT_TEST_TIER") == "full" {
		t.Fatalf("PMT_TEST_TIER=full but "+format, args...)
	}
	t.Skipf(format, args...)
}

func realBind(t *testing.T, g realGame) (map[string]string, map[string]string) {
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
	// what sequences that against the bind, including the rebind afterward.
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
	t.Logf("%s: %d files, %d defs, %d databases typed",
		g.id, len(files), len(acc.defs), len(derived.KindScope))
	return derived.PrefixKinds, derived.KindScope
}

func checkBindings(t *testing.T, g realGame) {
	t.Helper()
	prefixKind, kindScope := realBind(t, g)
	for prefix, wantKind := range g.wantPrefix {
		if got := prefixKind[prefix]; got != wantKind {
			t.Errorf("prefix %q -> %q, want %q", prefix, got, wantKind)
		}
	}
	if len(kindScope) == 0 {
		t.Error("no databases typed; expected the schema to bind at least a few")
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
