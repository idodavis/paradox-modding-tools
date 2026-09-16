// bind_test.go checks that the declared type system binds to the harvested
// databases. The synthetic rows run everywhere; the real-install rows walk an
// actual game, are gated on PMT_TEST_INSTALL_{CK3,EU5,VIC3}, and skip under
// -short. With PMT_TEST_TIER=full a missing install FAILS rather than skips: a
// tier that can silently be empty is not a tier.

package derived

import (
	"context"
	"os"
	"testing"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/lang/declared"
)

func TestBindKinds(t *testing.T) {
	// bindKinds matches by exact folder name, singular or plural, with one exception: a `_type` scope binds to its stem's folder when its own name matches nothing -- Vic3's `building_type` names `buildings`. Anything with no matching folder stays unbound rather than guessed.
	for _, tt := range []struct {
		name       string
		links      map[string]declared.ScopeLink
		defs       []Def
		wantPrefix map[string]string
		wantScope  map[string]string
		unbound    []string
	}{
		{
			name: "exact folder name",
			links: map[string]declared.ScopeLink{
				"cu": {Global: true, Data: true, Out: "culture"},
				"bg": {Global: true, Data: true, Out: "building_group"},
				"zz": {Global: true, Data: true, Out: "nothing_at_all"},
			},
			defs: []Def{
				{Kind: "cultures", Key: "english"},
				{Kind: "building_groups", Key: "bg_mining"},
				{Kind: "decisions", Key: "some_decision"},
			},
			wantPrefix: map[string]string{"cu": "cultures", "bg": "building_groups"},
			wantScope:  map[string]string{"cultures": "culture"},
			unbound:    []string{"zz"},
		},
		{
			name:       "singular scope, plural folder",
			links:      map[string]declared.ScopeLink{"trait": {Global: true, Data: true, Out: "trait"}},
			defs:       []Def{{Kind: "traits", Key: "brave"}},
			wantPrefix: map[string]string{"trait": "traits"},
			wantScope:  map[string]string{"traits": "trait"},
		},
		{
			name:    "no folder of a matching name stays unbound",
			links:   map[string]declared.ScopeLink{"xx": {Global: true, Data: true, Out: "widget"}},
			defs:    []Def{{Kind: "gadgets", Key: "a"}, {Kind: "gadgets", Key: "b"}},
			unbound: []string{"xx"},
		},
		{
			name:       "a _type scope binds to its stem's folder",
			links:      map[string]declared.ScopeLink{"bt": {Global: true, Data: true, Out: "building_type"}},
			defs:       []Def{{Kind: "buildings", Key: "building_port"}},
			wantPrefix: map[string]string{"bt": "buildings"},
			wantScope:  map[string]string{"buildings": "building_type"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			prefixKind, kindScope := bindKinds(&declared.Schema{Links: tt.links}, newDefIndex(tt.defs))
			for prefix, want := range tt.wantPrefix {
				if got := prefixKind[prefix]; got != want {
					t.Errorf("prefix %q -> %q, want %q", prefix, got, want)
				}
			}
			for kind, want := range tt.wantScope {
				if got := kindScope[kind]; got != want {
					t.Errorf("kindScope[%s] = %q, want %q", kind, got, want)
				}
			}
			for _, prefix := range tt.unbound {
				if got, ok := prefixKind[prefix]; ok {
					t.Errorf("%s bound to %q with no matching folder", prefix, got)
				}
			}
		})
	}

	// The real installs, through the real pipeline: nested databases (CK3 faiths, sub-tier titles) only become defs after resolveNests, and collectExtracts is what sequences that against the bind, including the rebind afterward.
	for _, g := range []struct {
		id, installEnv, docsEnv string
		wantPrefix              map[string]string
	}{
		{"ck3", "PMT_TEST_INSTALL_CK3", "PMT_TEST_DOCS_CK3", map[string]string{
			"culture": "cultures",
			// Faiths are nested under religions, so the database kind is the nested child name rather than a folder.
			"faith": "faith",
			"trait": "traits",
		}},
		{"vic3", "PMT_TEST_INSTALL_VIC3", "PMT_TEST_DOCS_VIC3", map[string]string{
			"cu": "cultures", "bg": "building_groups", "bt": "buildings",
		}},
		{"eu5", "PMT_TEST_INSTALL_EU5", "PMT_TEST_DOCS_EU5", map[string]string{
			"goods": "goods", "building_type": "building_types",
		}},
	} {
		t.Run("real/"+g.id, func(t *testing.T) {
			if testing.Short() {
				t.Skip("walks a full game install")
			}
			install := os.Getenv(g.installEnv)
			if install == "" {
				if os.Getenv("PMT_TEST_TIER") == "full" {
					t.Fatalf("PMT_TEST_TIER=full but %s not set", g.installEnv)
				}
				t.Skipf("%s not set", g.installEnv)
			}
			if _, err := os.Stat(install); err != nil {
				t.Skipf("%s: %v", g.installEnv, err)
			}
			// A single directory cannot express the layout: CK3 dumps all six files to logs/, Vic3 and EU5 write five to docs/ and leave event_scopes.log in logs/. PMT_TEST_DOCS_* is an override added to the list, never a replacement.
			dirs := scriptDocsDirs(g.id, install)
			if docs := os.Getenv(g.docsEnv); docs != "" {
				dirs = append(dirs, docs)
			}
			s := declared.ReadSchema(dirs)
			if s == nil || len(s.Effects) == 0 || len(s.Triggers) == 0 || len(s.Links) == 0 {
				t.Fatalf("%s: incomplete type system from %v; set %s if the dumps are elsewhere",
					g.id, dirs, g.docsEnv)
			}
			inv := gather(scriptRoots(game.Get(g.id), install))
			files := append(append([]fileRef{}, inv.script...), inv.gui...)
			if len(files) == 0 {
				t.Fatalf("no script files under %s", install)
			}
			acc, derived, err := collectExtracts(context.Background(), g.id, files, nil, s, nil)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("%s: %d files, %d defs, %d databases typed",
				g.id, len(files), len(acc.defs), len(derived.KindScope))
			for prefix, want := range g.wantPrefix {
				if got := derived.PrefixKinds[prefix]; got != want {
					t.Errorf("prefix %q -> %q, want %q", prefix, got, want)
				}
			}
			if len(derived.KindScope) == 0 {
				t.Error("no databases typed; expected the schema to bind at least a few")
			}
		})
	}
}
