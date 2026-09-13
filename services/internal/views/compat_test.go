// compat_test.go covers preview-order LIOS flips, depends, and dangling refs.

package views

import (
	"fmt"
	"strings"
	"testing"

	"paradox-modding-tools/services/internal/lang/declared"
	"paradox-modding-tools/services/internal/lang/derived"
	"paradox-modding-tools/services/internal/lang/session"
)

// twoModTrees builds the two-mod workspace with a minimal declared type system.
//
// The schema is not decoration. Since T-3 a dangling row requires the reference
// to rest on something the game DECLARES, so a fixture with no schema produces
// no dangling rows at all — correctly, because without script_docs PMT cannot
// know `trigger_event` is real. CK3 declares it, so the fixture says so.
func twoModTrees(t *testing.T, aFiles, bFiles map[string]string) *session.Session {
	t.Helper()
	return twoModTreesWithCache(t, &derived.VanillaCache{
		Schema: &declared.Schema{Effects: map[string]declared.EngineToken{
			"trigger_event": {Target: "event"},
		}},
	}, aFiles, bFiles)
}

func twoModTreesWithCache(
	t *testing.T, cache *derived.VanillaCache, aFiles, bFiles map[string]string,
) *session.Session {
	t.Helper()
	a, b := t.TempDir(), t.TempDir()
	write(t, a, "descriptor.mod", "name = \"a\"\n")
	write(t, b, "descriptor.mod", "name = \"b\"\n")
	for rel, body := range aFiles {
		write(t, a, rel, body)
	}
	for rel, body := range bFiles {
		write(t, b, rel, body)
	}
	return session.NewWithLoc("ws", "ck3", "english", cache, nil, []derived.ModInput{
		{Origin: "modA", Root: a, Order: 0, Name: "A"},
		{Origin: "modB", Root: b, Order: 1, Name: "B"},
	})
}

func TestCompatibilityOrderFlip(t *testing.T) {
	t.Parallel()
	s := twoMods(t, "ck3",
		"brave = { category = personality }\n",
		"brave = { category = personality }\n")
	got := Compatibility(s, nil)
	hit := findCompat(got.Rows, compatConflict, "brave")
	if hit == nil || hit.Winner != "modB" {
		t.Fatalf("default order winner = %+v", hit)
	}
	flipped := Compatibility(s, []string{"modB", "modA"})
	hit = findCompat(flipped.Rows, compatConflict, "brave")
	if hit == nil || hit.Winner != "modA" {
		t.Fatalf("reversed order winner = %+v", hit)
	}
}

func TestCompatibilityDependsAndDangling(t *testing.T) {
	t.Parallel()
	s := twoModTrees(t,
		map[string]string{
			"events/a.txt": `namespace = t
t.1 = {
	type = character_event
	immediate = {
		trigger_event = t.2
		trigger_event = t.ghost
	}
}
`,
		},
		map[string]string{
			"events/b.txt": `namespace = t
t.2 = { type = character_event }
`,
		})
	got := Compatibility(s, nil)
	dep := findCompat(got.Rows, compatDepends, "t.2")
	if dep == nil || dep.From != "modA" || dep.To != "modB" {
		t.Fatalf("depends = %+v rows=%d defs=%d refs=%d",
			dep, len(got.Rows), len(s.ModDefsOf("t.2")), len(s.RefsTo("t.2")))
	}
	dang := findCompat(got.Rows, compatDangling, "t.ghost")
	if dang == nil || dang.From != "modA" || dang.Refs < 1 {
		t.Fatalf("dangling = %+v", dang)
	}
	if got.Depends < 1 || got.Dangling < 1 {
		t.Fatalf("counts depends=%d dangling=%d", got.Depends, got.Dangling)
	}
}

// The KPI is how many there are, not how many the table can show. Counting the
// slice reported "500 dangling" for a total conversion with thousands — the same
// fault loccoverage.go had already fixed and commented 180 lines away.
func TestCompatibilityCountsAreNotTheRowBudget(t *testing.T) {
	t.Parallel()
	const want = compatRefCap + 37
	var body strings.Builder
	body.WriteString("namespace = t\nt.1 = {\n\ttype = character_event\n\timmediate = {\n")
	for i := range want {
		fmt.Fprintf(&body, "\t\ttrigger_event = t.%d\n", 9000+i)
	}
	body.WriteString("\t}\n}\n")

	s := twoModTrees(t, map[string]string{"events/a.txt": body.String()}, nil)
	got := Compatibility(s, nil)
	rows := 0
	for _, r := range got.Rows {
		if r.Type == compatDangling {
			rows++
		}
	}
	if rows != compatRefCap {
		t.Errorf("dangling rows = %d, want the full budget of %d", rows, compatRefCap)
	}
	if got.Dangling != want {
		t.Errorf("Dangling KPI = %d, want %d (the count, not the %d rows shown)",
			got.Dangling, want, rows)
	}
}

func twoMods(t *testing.T, game, aBody, bBody string) *session.Session {
	t.Helper()
	a, b := t.TempDir(), t.TempDir()
	desc, body := "descriptor.mod", "name = \"t\"\n"
	if game != "ck3" {
		desc, body = ".metadata/metadata.json", `{"name":"t"}`
	}
	write(t, a, desc, body)
	write(t, b, desc, body)
	write(t, a, "common/traits/a.txt", aBody)
	write(t, b, "common/traits/b.txt", bBody)
	return session.NewWithLoc("ws", game, "english", nil, nil, []derived.ModInput{
		{Origin: "modA", Root: a, Order: 0},
		{Origin: "modB", Root: b, Order: 1},
	})
}

func TestCompatibilityContests(t *testing.T) {
	tests := []struct {
		name, game, a, b, rule, winner string
		sites                          int
	}{
		{"LIOS last mod", "ck3",
			"brave = { category = personality }\n",
			"brave = { category = personality }\n",
			"LIOS", "modB", 0},
		{"INJECT REPLACE collide", "eu5",
			"INJECT:brave = { category = personality }\n",
			"REPLACE:brave = { category = personality }\n",
			"", "modB", 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Compatibility(twoMods(t, tt.game, tt.a, tt.b), nil)
			hit := findCompat(got.Rows, compatConflict, "brave")
			if hit == nil || hit.Overlay {
				t.Fatalf("brave row = %+v", hit)
			}
			if tt.rule != "" && (hit.Rule != tt.rule || hit.Winner != tt.winner) {
				t.Fatalf("rule=%s winner=%s", hit.Rule, hit.Winner)
			}
			if tt.sites > 0 {
				n := 0
				for _, site := range hit.Sites {
					if site.Origin != "" {
						n++
					}
					if site.Path != "" && site.Rel == "" {
						t.Fatalf("site missing rel: %+v", site)
					}
				}
				if n != tt.sites {
					t.Fatalf("mod sites = %d, want %d", n, tt.sites)
				}
			}
		})
	}
}

func TestOverrideSitesDedupe(t *testing.T) {
	t.Parallel()
	s := twoMods(t, "ck3",
		"brave = { category = personality }\n",
		"brave = { category = personality }\n")
	path := s.ModDefsOf("brave")[0].Path
	twins := []derived.Def{
		{Origin: "modA", Path: path, Line: 1},
		{Origin: "modA", Path: path, Line: 1},
		{Origin: "modA", Path: path, Line: 9},
	}
	got := overrideSites(s, twins)
	if len(got) != 2 {
		t.Fatalf("sites = %d, want 2 (same-line twin dropped)", len(got))
	}
	if got[0].Line == got[1].Line {
		t.Fatalf("expected distinct lines, got %+v", got)
	}
}

func findCompat(rows []HealthRow, typ, name string) *HealthRow {
	for i := range rows {
		if rows[i].Type == typ && rows[i].Name == name {
			return &rows[i]
		}
	}
	return nil
}
