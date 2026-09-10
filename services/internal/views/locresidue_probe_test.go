//go:build probe

// locresidue_probe_test.go is the vanilla loc-orphan probe behind GAME-SYNTAX
// §12 — the only way to re-measure those tables after a change to the orphan
// rule. It reports and asserts nothing, so it is build-tagged out of every
// tier but `task test:probe`: a report that cannot fail must not sit where a
// gate is expected (plan V.3).

package views

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"paradox-modding-tools/services/internal/catalog"
	"paradox-modding-tools/services/internal/session"
)

// calibrates the "orphaned" rule against vanilla.
//
// Vanilla is the corpus where the loc keys and the script that cites them ship
// from the same build, so a vanilla key the rule calls orphaned is almost always
// a mechanism PMT does not model rather than a defect Paradox shipped. That
// makes the vanilla orphan rate a direct read on how much of the mod residue is
// engine fault -- which is the question the baseline cannot answer on its own.
//
// Probe, not an assertion: run with PMT_LOC_RESIDUE=1 and read the clusters.
func TestVanillaLocResidue(t *testing.T) {
	if testing.Short() || os.Getenv("PMT_LOC_RESIDUE") == "" {
		t.Skip("probe; set PMT_LOC_RESIDUE=1")
	}
	for _, g := range sweepGames {
		install := os.Getenv(g.installEnv)
		if install == "" {
			t.Logf("%s: %s not set", g.game, g.installEnv)
			continue
		}
		t.Run(g.game, func(t *testing.T) {
			id := "residue-" + g.game
			t.Cleanup(func() { _ = catalog.DropVanillaFiles(id) })
			cache, vloc, err := catalog.Scan(context.Background(), catalog.ScanRequest{
				InstallID: id, GameID: g.game, InstallPath: install,
				Version: "residue", LocLang: "english",
			})
			if err != nil {
				t.Fatalf("scan: %v", err)
			}
			s := session.NewWithLoc("residue", g.game, "english", cache, vloc, nil)
			defer s.Close()

			// Print what the rule accepted, not just what it scored. A shape
			// list that reads as nonsense is a failure even if the number fell.
			for i, af := range cache.LocKeyAffixes {
				if i >= 20 {
					t.Logf("  affix: %d accepted in total", len(cache.LocKeyAffixes))
					break
				}
				t.Logf("  affix %-24q + %-18q %6d of %6d",
					af.Pre, af.Suf, af.Keys, af.Of)
			}
			keys := s.InheritedLocKeys("english")
			used := s.UsedLocKeys()
			var residue []string
			nUsed, nConv := 0, 0
			for _, k := range keys {
				if used[k] {
					nUsed++
					continue
				}
				if s.LocKeyExplained(k) {
					nConv++
					continue
				}
				residue = append(residue, k)
			}
			pct := func(n int) string {
				if len(keys) == 0 {
					return "0%"
				}
				return fmt.Sprintf("%.1f%%", 100*float64(n)/float64(len(keys)))
			}
			t.Logf("RESIDUE %s: %d english keys | cited %d (%s) | convention %d (%s) | ORPHAN %d (%s)",
				g.game, len(keys), nUsed, pct(nUsed), nConv, pct(nConv),
				len(residue), pct(len(residue)))
			logClusters(t, "prefix", residue, func(k string) string { return headToken(k, 2) })
			logClusters(t, "suffix", residue, func(k string) string { return tailToken(k, 2) })
			convResidue(t, g.game, g.appID, cache, vloc)
		})
	}
}

// convResidue measures the same rule on the total conversions, reusing the
// install scan above rather than repeating it. The rate is the point, not the
// count: conversions are enormous, so a rate at or below vanilla's means the
// residue is the engine's gap applied to more keys, not cruft the mod added.
func convResidue(
	t *testing.T, game, appID string, cache *catalog.VanillaCache, vloc *catalog.VanillaLoc,
) {
	t.Helper()
	var ids []string
	for key := range totalConversions {
		if strings.HasPrefix(key, appID+"/") {
			ids = append(ids, strings.TrimPrefix(key, appID+"/"))
		}
	}
	sort.Strings(ids)
	for _, modID := range ids {
		s := session.NewWithLoc(game, game, "english", cache, vloc,
			[]catalog.ModInput{{
				Origin: "mod",
				Root:   filepath.Join(workshopRoot, appID, modID),
				Name:   modID,
			}})
		inherited := map[string]bool{}
		for _, k := range s.InheritedLocKeys(locSourceLang) {
			inherited[k] = true
		}
		used := s.UsedLocKeys()
		own, orphan := 0, 0
		s.EachLoc(func(lang, k string, _ catalog.LocEntry) {
			if lang != locSourceLang || inherited[k] {
				return
			}
			own++
			if used[k] || s.LocKeyExplained(k) {
				return
			}
			orphan++
		})
		rate := 0.0
		if own > 0 {
			rate = 100 * float64(orphan) / float64(own)
		}
		t.Logf("CONV %-24s own english keys %6d | orphan %6d (%.1f%%)",
			totalConversions[appID+"/"+modID], own, orphan, rate)
		s.Close()
	}
}

// headToken returns the first n underscore-separated tokens of key.
func headToken(key string, n int) string {
	p := strings.Split(key, "_")
	if len(p) <= n {
		return key
	}
	return strings.Join(p[:n], "_") + "_*"
}

// tailToken returns the last n underscore-separated tokens of key.
func tailToken(key string, n int) string {
	p := strings.Split(key, "_")
	if len(p) <= n {
		return key
	}
	return "*_" + strings.Join(p[len(p)-n:], "_")
}

func logClusters(t *testing.T, label string, keys []string, sig func(string) string) {
	t.Helper()
	n := map[string]int{}
	ex := map[string]string{}
	for _, k := range keys {
		s := sig(k)
		n[s]++
		if ex[s] == "" {
			ex[s] = k
		}
	}
	type row struct {
		sig, ex string
		n       int
	}
	rows := make([]row, 0, len(n))
	for s, c := range n {
		rows = append(rows, row{s, ex[s], c})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].n != rows[j].n {
			return rows[i].n > rows[j].n
		}
		return rows[i].sig < rows[j].sig
	})
	top := 0
	for i, r := range rows {
		if i >= 25 {
			break
		}
		top += r.n
		t.Logf("  %s %-34s %6d  e.g. %s", label, r.sig, r.n, r.ex)
	}
	t.Logf("  %s: %d clusters, top 25 cover %d of %d", label, len(rows), top, len(keys))
}
