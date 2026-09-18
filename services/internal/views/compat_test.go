// compat_test.go covers preview-order LIOS flips, depends, and dangling refs.

package views

import (
	"fmt"
	"strings"
	"testing"

	"paradox-modding-tools/services/internal/lang/index"
	"paradox-modding-tools/services/internal/lang/session"
)

func twoModTrees(t *testing.T, aFiles, bFiles map[string]string) *session.Session {
	t.Helper()
	a, b := oneMod(t, "ck3", aFiles), oneMod(t, "ck3", bFiles)
	a.Origin, a.Name = "modA", "A"
	b.Origin, b.Name, b.Order = "modB", "B", 1
	return buildSession(t, "ck3", a, b)
}

func TestCompatibilityOrderFlip(t *testing.T) {
	s := twoMods(t, "ck3", "brave_m = { category = personality }\n", "brave_m = { category = personality }\n")
	if hit := findCompat(Compatibility(s, nil).Rows, compatConflict, "brave_m"); hit == nil || hit.Winner != "modB" {
		t.Fatalf("default order winner = %+v", hit)
	}
	if hit := findCompat(Compatibility(s, []string{"modB", "modA"}).Rows, compatConflict, "brave_m"); hit == nil || hit.Winner != "modA" {
		t.Fatalf("reversed order winner = %+v", hit)
	}
}

func TestCompatibilityDependsAndDangling(t *testing.T) {
	s := twoModTrees(t,
		map[string]string{"events/a.txt": "namespace = t\nt.1 = {\n\ttype = character_event\n\timmediate = {\n\t\ttrigger_event = t.2\n\t\ttrigger_event = t.ghost\n\t}\n}\n"},
		map[string]string{"events/b.txt": "namespace = t\nt.2 = { type = character_event }\n"})
	got := Compatibility(s, nil)
	if dep := findCompat(got.Rows, compatDepends, "t.2"); dep == nil || dep.From != "modA" || dep.To != "modB" {
		t.Fatalf("depends = %+v rows=%d", dep, len(got.Rows))
	}
	if dang := findCompat(got.Rows, compatDangling, "t.ghost"); dang == nil || dang.From != "modA" || dang.Refs < 1 {
		t.Fatalf("dangling = %+v", dang)
	}
	if got.Depends < 1 || got.Dangling < 1 {
		t.Fatalf("counts depends=%d dangling=%d", got.Depends, got.Dangling)
	}
}

// The KPI is how many there are, not how many the table can show.
func TestCompatibilityCountsAreNotTheRowBudget(t *testing.T) {
	const want = compatRefCap + 37
	var body strings.Builder
	body.WriteString("namespace = t\nt.1 = {\n\ttype = character_event\n\timmediate = {\n")
	for i := range want {
		fmt.Fprintf(&body, "\t\ttrigger_event = t.%d\n", 9000+i)
	}
	body.WriteString("\t}\n}\n")
	got := Compatibility(twoModTrees(t, map[string]string{"events/a.txt": body.String()}, nil), nil)
	rows := 0
	for _, r := range got.Rows {
		if r.Type == compatDangling {
			rows++
		}
	}
	if rows != compatRefCap || got.Dangling != want {
		t.Errorf("dangling rows = %d (want %d), KPI = %d (want %d)", rows, compatRefCap, got.Dangling, want)
	}
}

func twoMods(t *testing.T, game, aBody, bBody string) *session.Session {
	t.Helper()
	a := oneMod(t, game, map[string]string{"common/traits/a.txt": aBody})
	b := oneMod(t, game, map[string]string{"common/traits/b.txt": bBody})
	a.Origin, b.Origin, b.Order = "modA", "modB", 1
	return buildSession(t, game, a, b)
}

func TestCompatibilityContests(t *testing.T) {
	for _, tt := range []struct {
		name, game, a, b, rule, winner string
		sites                          int
	}{
		{"LIOS last mod", "ck3", "brave_m = { category = personality }\n", "brave_m = { category = personality }\n", "LIOS", "modB", 0},
		{"INJECT REPLACE collide", "eu5", "INJECT:brave_m = { category = personality }\n", "REPLACE:brave_m = { category = personality }\n", "", "modB", 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			hit := findCompat(Compatibility(twoMods(t, tt.game, tt.a, tt.b), nil).Rows, compatConflict, "brave_m")
			if hit == nil || hit.Overlay {
				t.Fatalf("brave_m row = %+v", hit)
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
	s := twoMods(t, "ck3", "brave_m = { category = personality }\n", "brave_m = { category = personality }\n")
	path := s.ModDefs("brave_m")[0].Path
	got := overrideSites(s, []index.Def{
		{Origin: "modA", Path: path, Line: 1}, {Origin: "modA", Path: path, Line: 1}, {Origin: "modA", Path: path, Line: 9},
	})
	if len(got) != 2 || got[0].Line == got[1].Line {
		t.Fatalf("sites = %+v, want 2 distinct lines", got)
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
