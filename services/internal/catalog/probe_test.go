//go:build probe

// probe_test.go: opt-in measurements over the real installs (`task test:probe`).
// scanbench_test.go and views/eventscope_gate_test.go stay separate — they are
// gated by flag and env, not by build tag, so folding them in would hide them.

package catalog

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"maps"
	"os"
	"reflect"
	"slices"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/bytedance/sonic"

	"paradox-modding-tools/services/internal/game"
	"paradox-modding-tools/services/internal/parser/jomini"
)

type optionProbeGame struct{ id, inst, docs string }

var optionProbeGames = []optionProbeGame{
	{"ck3", "PMT_TEST_INSTALL_CK3", "PMT_TEST_DOCS_CK3"},
	{"vic3", "PMT_TEST_INSTALL_VIC3", "PMT_TEST_DOCS_VIC3"},
	{"eu5", "PMT_TEST_INSTALL_EU5", "PMT_TEST_DOCS_EU5"},
}

// optionProbeScan runs the real pipeline and returns the option shapes it
// shipped, the owner stats behind them, and the defs they minted.
func optionProbeScan(t *testing.T, g optionProbeGame) (
	*Schema, []NestedShape, map[optionOwner]*ownerKeys, []Def, *Derived,
) {
	t.Helper()
	install := os.Getenv(g.inst)
	if install == "" {
		t.Skipf("%s unset", g.inst)
	}
	s := realSchema(t, g.id, install, os.Getenv(g.docs), g.docs)
	info := game.Get(g.id)
	inv := gather(scriptRoots(info, install))
	files := append(append([]fileRef{}, inv.script...), inv.gui...)
	acc, derived, _, err := collectExtracts(context.Background(), g.id, files, true, nil, s, nil)
	if err != nil {
		t.Fatal(err)
	}
	// corp.walk is concurrent, so addOptionOwners takes the lock deriveAll
	// gives it.
	owners := map[optionOwner]*ownerKeys{}
	var mu sync.Mutex
	corp := corpus{ctx: context.Background(), gameID: g.id, files: files}
	if err := corp.walk(func(f fileRef, res jomini.Result) error {
		mu.Lock()
		defer mu.Unlock()
		addOptionOwners(g.id, f.rel, res.Root, owners)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var opts []NestedShape
	for _, sh := range derived.NestedShapes {
		if sh.IsOption {
			opts = append(opts, sh)
		}
	}
	return s, opts, owners, acc.defs, derived
}

// TestOptionShapeProbe reports every option database the pass ships, largest
// first, so a shape appearing or vanishing after a patch is visible.
func TestOptionShapeProbe(t *testing.T) {
	for _, g := range optionProbeGames {
		t.Run(g.id, func(t *testing.T) {
			_, opts, owners, defs, _ := optionProbeScan(t, g)
			byKind := map[string]int{}
			for _, d := range defs {
				byKind[d.Kind]++
			}
			sort.Slice(opts, func(i, j int) bool {
				return byKind[opts[i].ChildKind] > byKind[opts[j].ChildKind]
			})
			t.Logf("%s: %d defs, %d option shapes", g.id, len(defs), len(opts))
			for _, sh := range opts {
				o := optionOwner{sh.ParentKind, sh.GroupKey}
				per := 0.0
				if w := owners[o]; w != nil && w.parents > 0 {
					per = float64(len(w.keys)) / float64(w.parents)
				}
				t.Logf("  %-28s parent=%-22s defs=%-6d keys/parent=%.1f",
					sh.ChildKind, sh.ParentKind, byKind[sh.ChildKind], per)
			}
		})
	}
}

// TestOptionKeysPerParentBand is what keeps maxOptionKeysPerParent from being a
// magic number: it asserts the cap still sits clear of every shape the pass
// ships. The largest genuine shape measured 90.5 keys per parent against a cap
// of 250, so a patch would have to nearly triple a real database before the cap
// started cutting into one — and this fails when it does.
func TestOptionKeysPerParentBand(t *testing.T) {
	const margin = 2 // the cap must be at least this many times the largest real shape
	for _, g := range optionProbeGames {
		t.Run(g.id, func(t *testing.T) {
			_, opts, owners, _, _ := optionProbeScan(t, g)
			worst, worstOf := 0.0, ""
			for _, sh := range opts {
				w := owners[optionOwner{sh.ParentKind, sh.GroupKey}]
				if w == nil || w.parents == 0 {
					continue
				}
				if per := float64(len(w.keys)) / float64(w.parents); per > worst {
					worst, worstOf = per, sh.ParentKind+"/"+sh.ChildKind
				}
			}
			t.Logf("%s: largest shipped shape is %s at %.1f keys/parent, cap %d",
				g.id, worstOf, worst, maxOptionKeysPerParent)
			if worst*margin > maxOptionKeysPerParent {
				t.Errorf("%s: %s at %.1f keys/parent leaves no margin under the "+
					"cap of %d; a real database has grown toward the blob "+
					"threshold, so re-measure before raising the cap",
					g.id, worstOf, worst, maxOptionKeysPerParent)
			}
		})
	}
}

// TestOptionRowsAreNotDeclared is T-0's structural half: a name the game
// declares as an effect or trigger is a statement, never a database row. It
// held 639 CK3 defs and 2 EU5 ones, each of which outranked the declared token
// in resolveDefinition.
func TestOptionRowsAreNotDeclared(t *testing.T) {
	for _, g := range optionProbeGames {
		t.Run(g.id, func(t *testing.T) {
			s, opts, _, defs, _ := optionProbeScan(t, g)
			kinds := map[string]bool{}
			for _, sh := range opts {
				kinds[sh.ChildKind] = true
			}
			var bad []string
			for _, d := range defs {
				if !kinds[d.Kind] {
					continue
				}
				if _, bag, ok := s.Token(d.Key); ok {
					bad = append(bad, d.Kind+"/"+d.Key+" ("+bag+")")
				}
			}
			if len(bad) > 0 {
				sort.Strings(bad)
				t.Errorf("%s: %d option rows are declared engine tokens: %v",
					g.id, len(bad), bad[:min(8, len(bad))])
			}
		})
	}
}

// probeFiles is the install's script + gui inventory, for probes that need a
// second corpus walk.
func probeFiles(t *testing.T, g optionProbeGame) []fileRef {
	t.Helper()
	install := os.Getenv(g.inst)
	if install == "" {
		t.Skipf("%s unset", g.inst)
	}
	inv := gather(scriptRoots(game.Get(g.id), install))
	return append(append([]fileRef{}, inv.script...), inv.gui...)
}

// TestBindByNameEvidence reports every binding made by folder name rather than
// by citation, with the number of definitions behind it. nameMatchKind accepts
// a folder holding ONE key, which is the body_part failure mode — a rule with a
// ratio and no minimum evidence — on the path that decides PrefixKinds and
// KindScope.
func TestBindByNameEvidence(t *testing.T) {
	for _, g := range optionProbeGames {
		t.Run(g.id, func(t *testing.T) {
			s, _, _, defs, _ := optionProbeScan(t, g)
			keysByKind := map[string]map[string]bool{}
			for _, d := range defs {
				if !IsCitableDef(d) {
					continue
				}
				k := jomini.CanonicalKind(d.Kind)
				if keysByKind[k] == nil {
					keysByKind[k] = map[string]bool{}
				}
				keysByKind[k][d.Key] = true
			}
			var cites []typedCite
			var mu sync.Mutex
			corp := corpus{ctx: context.Background(), gameID: g.id, files: probeFiles(t, g)}
			if err := corp.walk(func(_ fileRef, res jomini.Result) error {
				c := collectTypedCites(res.Root)
				mu.Lock()
				defer mu.Unlock()
				cites = append(cites, c...)
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			_, _, bound := bindKinds(s, newDefIndex(defs), cites)

			type row struct {
				scope, kind string
				keys, cites int
			}
			var byName []row
			for _, b := range bound {
				if !b.ByName {
					continue
				}
				byName = append(byName, row{b.Scope, b.Kind, len(keysByKind[b.Kind]), b.Cites})
			}
			sort.Slice(byName, func(i, j int) bool { return byName[i].keys < byName[j].keys })
			t.Logf("%s: %d bindings, %d by name", g.id, len(bound), len(byName))
			for _, r := range byName {
				t.Logf("  scope=%-24s kind=%-24s keys=%-6d cites=%d",
					r.scope, r.kind, r.keys, r.cites)
			}
		})
	}
}

func TestDefIndexMatchesTheLoopsItReplaced(t *testing.T) {
	for _, g := range optionProbeGames {
		t.Run(g.id, func(t *testing.T) {
			_, _, _, defs, _ := optionProbeScan(t, g)
			idx := newDefIndex(defs)

			// defKeySet, verbatim from derive.go before the change.
			wantKeys := map[string]bool{}
			for _, d := range defs {
				if IsCitableDef(d) {
					wantKeys[d.Key] = true
				}
			}
			if got := idx.citableKeys(); !reflect.DeepEqual(got, wantKeys) {
				t.Errorf("citableKeys: %d, want %d", len(got), len(wantKeys))
			}

			// bindKinds' keysByKind.
			wantByKind := map[string]map[string]bool{}
			for _, d := range defs {
				if !IsCitableDef(d) {
					continue
				}
				k := jomini.CanonicalKind(d.Kind)
				if wantByKind[k] == nil {
					wantByKind[k] = map[string]bool{}
				}
				wantByKind[k][d.Key] = true
			}
			if !reflect.DeepEqual(idx.keysByKind, wantByKind) {
				t.Errorf("keysByKind: %d kinds, want %d", len(idx.keysByKind), len(wantByKind))
			}

			// deriveFieldValueKinds' kindsByKey. Order matters: the pass reads
			// kinds[0] when there is exactly one, so a differently ordered slice
			// of the same kinds would still be a behaviour change.
			wantKindsByKey := map[string][]string{}
			for _, d := range defs {
				if !IsCitableDef(d) {
					continue
				}
				k := jomini.CanonicalKind(d.Kind)
				if !slices.Contains(wantKindsByKey[d.Key], k) {
					wantKindsByKey[d.Key] = append(wantKindsByKey[d.Key], k)
				}
			}
			if !reflect.DeepEqual(idx.kindsByKey, wantKindsByKey) {
				t.Errorf("kindsByKey: %d keys, want %d", len(idx.kindsByKey), len(wantKindsByKey))
			}

			// deriveSnippets' total: raw kind, definitions not distinct keys.
			wantDefsByKind := map[string]int{}
			for _, d := range defs {
				wantDefsByKind[d.Kind]++
			}
			if !reflect.DeepEqual(idx.defsByKind, wantDefsByKind) {
				t.Errorf("defsByKind: %d kinds, want %d", len(idx.defsByKind), len(wantDefsByKind))
			}

			// deriveLocAffixes' kindsOf and total.
			wantKindsOf := map[string][]string{}
			seen := map[string]map[string]bool{}
			wantTotal := map[string]int{}
			for _, d := range defs {
				k := jomini.CanonicalKind(d.Kind)
				if d.Key == "" || !jomini.IsObjectKind(k) {
					continue
				}
				if seen[k] == nil {
					seen[k] = map[string]bool{}
				}
				if !seen[k][d.Key] {
					seen[k][d.Key] = true
					wantTotal[k]++
				}
				if !slices.Contains(wantKindsOf[d.Key], k) {
					wantKindsOf[d.Key] = append(wantKindsOf[d.Key], k)
				}
			}
			gotKindsOf, gotTotal := idx.objectNames()
			if !reflect.DeepEqual(gotKindsOf, wantKindsOf) {
				t.Errorf("objectNames kindsOf: %d, want %d", len(gotKindsOf), len(wantKindsOf))
			}
			if !reflect.DeepEqual(gotTotal, wantTotal) {
				t.Errorf("objectNames total: %v vs %v", len(gotTotal), len(wantTotal))
			}

			// eventOnActionIDs.
			wantEv, wantOA := map[string]bool{}, map[string]bool{}
			for _, d := range defs {
				switch jomini.CanonicalKind(d.Kind) {
				case "event":
					wantEv[d.Key] = true
				case "on_action":
					wantOA[d.Key] = true
				}
			}
			if !reflect.DeepEqual(idx.keysOfKind("event"), wantEv) {
				t.Errorf("event ids: %d, want %d", len(idx.keysOfKind("event")), len(wantEv))
			}
			if !reflect.DeepEqual(idx.keysOfKind("on_action"), wantOA) {
				t.Errorf("on_action ids: %d, want %d", len(idx.keysOfKind("on_action")), len(wantOA))
			}
			t.Logf("%s: %d defs, %d citable keys, %d kinds — all six summaries match",
				g.id, len(defs), len(idx.kindsByKey), len(idx.keysByKind))
		})
	}
}

func TestModelFingerprint(t *testing.T) {
	for _, g := range optionProbeGames {
		t.Run(g.id, func(t *testing.T) {
			_, _, _, defs, derived := optionProbeScan(t, g)
			sum := func(label string, v any) {
				raw, err := sonic.Marshal(v)
				if err != nil {
					t.Fatal(err)
				}
				h := sha256.Sum256(raw)
				t.Logf("  %-14s %s  (%d bytes)", label, hex.EncodeToString(h[:8]), len(raw))
			}

			sorted := slices.Clone(defs)
			slices.SortFunc(sorted, func(a, b Def) int {
				return cmp.Or(
					cmp.Compare(a.Kind, b.Kind),
					cmp.Compare(a.Key, b.Key),
					cmp.Compare(a.Path, b.Path),
					cmp.Compare(a.Line, b.Line),
					cmp.Compare(a.Origin, b.Origin),
				)
			})
			t.Logf("%s: %d defs", g.id, len(defs))
			sum("defs", sorted)
			sum("prefixKinds", sortedPairs(derived.PrefixKinds))
			sum("kindScope", sortedPairs(derived.KindScope))
			sum("fireKeys", sortedPairs(derived.FireKeys))
			sum("fieldKinds", sortedPairs(derived.FieldValueKinds))
			sum("wrappers", slices.Sorted(maps.Keys(derived.Wrappers)))
			sum("nestedShapes", derived.NestedShapes) // already sorted by the pass
			sum("locFields", slices.Sorted(maps.Keys(derived.LocFields)))
		})
	}
}

// sortedPairs flattens a map to key,value,key,value in key order.
func sortedPairs(m map[string]string) []string {
	out := make([]string, 0, len(m)*2)
	for _, k := range slices.Sorted(maps.Keys(m)) {
		out = append(out, k, m[k])
	}
	return out
}

func BenchmarkDefSummaries(b *testing.B) {
	t := &testing.T{}
	_, _, _, defs, _ := optionProbeScan(t, optionProbeGames[0])
	if t.Failed() || len(defs) == 0 {
		b.Skip("no CK3 install")
	}
	b.Logf("%d defs", len(defs))

	// What collectExtracts and deriveAll did before II.2: five separate walks
	// of the whole slice, each building one summary.
	b.Run("before/5walks", func(b *testing.B) {
		for b.Loop() {
			keys := map[string]bool{}
			for _, d := range defs {
				if IsCitableDef(d) {
					keys[d.Key] = true
				}
			}
			ev, oa := map[string]bool{}, map[string]bool{}
			for _, d := range defs {
				switch jomini.CanonicalKind(d.Kind) {
				case "event":
					ev[d.Key] = true
				case "on_action":
					oa[d.Key] = true
				}
			}
			byKind := map[string]map[string]bool{}
			for _, d := range defs {
				if !IsCitableDef(d) {
					continue
				}
				k := jomini.CanonicalKind(d.Kind)
				if byKind[k] == nil {
					byKind[k] = map[string]bool{}
				}
				byKind[k][d.Key] = true
			}
			kindsByKey := map[string][]string{}
			for _, d := range defs {
				if !IsCitableDef(d) {
					continue
				}
				k := jomini.CanonicalKind(d.Kind)
				seen := false
				for _, have := range kindsByKey[d.Key] {
					if have == k {
						seen = true
						break
					}
				}
				if !seen {
					kindsByKey[d.Key] = append(kindsByKey[d.Key], k)
				}
			}
			total := map[string]int{}
			for _, d := range defs {
				total[d.Kind]++
			}
		}
	})

	b.Run("after/1walk", func(b *testing.B) {
		for b.Loop() {
			x := newDefIndex(defs)
			_ = x.citableKeys()
			_ = x.keysOfKind("event")
			_ = x.keysOfKind("on_action")
		}
	})
}

// BenchmarkCorpusWalk is what II.4's fold is worth: one re-read and re-parse of
// the whole install, which is what a corpus walk costs.
func BenchmarkCorpusWalk(b *testing.B) {
	t := &testing.T{}
	files := probeFiles(t, optionProbeGames[0])
	if t.Failed() || len(files) == 0 {
		b.Skip("no CK3 install")
	}
	b.Logf("%d files", len(files))
	corp := corpus{ctx: context.Background(), gameID: "ck3", files: files}
	for b.Loop() {
		n := 0
		_ = corp.walk(func(_ fileRef, res jomini.Result) error {
			n++
			return nil
		})
	}
}

// BenchmarkWalkSplit separates the read half of a corpus walk from the parse
// half, because CPU-sample share cannot answer it: reads run on 8 workers and
// block in syscalls, so their share of samples is not their share of wall time.
func BenchmarkWalkSplit(b *testing.B) {
	t := &testing.T{}
	files := probeFiles(t, optionProbeGames[0])
	if t.Failed() || len(files) == 0 {
		b.Skip("no CK3 install")
	}
	corp := corpus{ctx: context.Background(), gameID: "ck3", files: files}
	b.Run("readonly", func(b *testing.B) {
		for b.Loop() {
			_ = walkFiles(corp.ctx, corp.files, func(f fileRef) error {
				raw, err := os.ReadFile(f.abs)
				if err != nil {
					return nil
				}
				_, _ = jomini.Decode(raw)
				return nil
			})
		}
	})
	b.Run("read+parse", func(b *testing.B) {
		for b.Loop() {
			_ = corp.walk(func(_ fileRef, _ jomini.Result) error { return nil })
		}
	})
}

// TestScanPhaseTimes is the wall-clock breakdown S1 needs. A CPU profile ranks
// samples, which on a parallel syscall-heavy stage is a different question —
// see the S1b refutation. OnProgress already marks the phase boundaries, so
// this needs no production change.
func TestScanPhaseTimes(t *testing.T) {
	for _, g := range optionProbeGames {
		t.Run(g.id, func(t *testing.T) {
			install := os.Getenv(g.inst)
			if install == "" {
				t.Skipf("%s unset", g.inst)
			}
			type phase struct {
				name string
				took time.Duration
			}
			var phases []phase
			last, start := time.Now(), time.Now()
			id := "phase-" + g.id
			t.Cleanup(func() { _ = DropVanillaFiles(id) })
			c, _, err := Scan(context.Background(), ScanRequest{
				InstallID: id, GameID: g.id, InstallPath: install,
				Version: "phase", LocLang: "english",
				OnProgress: func(_ int, msg string) {
					phases = append(phases, phase{msg, time.Since(last)})
					last = time.Now()
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			total := time.Since(start)
			// The phase named by a callback is the one STARTING, so each
			// duration belongs to the phase before it.
			for i := 1; i < len(phases); i++ {
				t.Logf("  %-22s %7.0f ms  %4.1f%%", phases[i-1].name,
					float64(phases[i].took.Milliseconds()),
					100*phases[i].took.Seconds()/total.Seconds())
			}
			t.Logf("  %-22s %7.0f ms  (%d defs)", "TOTAL", float64(total.Milliseconds()), len(c.Defs))
		})
	}
}
