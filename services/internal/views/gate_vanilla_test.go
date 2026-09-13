// gate_vanilla_test.go is the ship condition for every semantic diagnostic:
// zero findings on the game's own files.
//
// Vanilla is the one corpus where a finding is unambiguously the engine's fault.
// Paradox ships script the game runs, so a check that fires there is telling a
// modder that correct script is wrong — which is worse than the check not
// existing, because it teaches them to ignore the panel.
//
// Diagnose deliberately stays silent on install files, so the gate copies a
// sample of them into a temporary mod. The install still supplies the cache, so
// every definition resolves exactly as it does in a real workspace; only the
// sampled files are judged.
//
// Copied rather than loading the install as a mod: that was the first design and
// it did not finish in thirty minutes, because indexing forty thousand files is
// the cost, not diagnosing them.
//
// Opt-in: it scans an install. Run with PMT_VANILLA_GATE=1.
package views

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"paradox-modding-tools/services/internal/lang/derived"
	"paradox-modding-tools/services/internal/lang/lsp"
	"paradox-modding-tools/services/internal/lang/session"
)

// gateFiles bounds the sample. The question is whether a check fires at all, and
// several hundred files of real script settles that.
const gateFiles = 400

// semanticCodes are the checks this gate judges. Parse errors are excluded: a
// parser disagreement with vanilla is a parser bug, tracked separately, and
// lumping it in here would hide whether a semantic check is clean.
// duplicate-definition and missing-required-loc are absent on purpose: both are
// marked experimental or known-ambiguous and are already off or documented as
// such. Listing them here would keep the gate permanently red and stop it
// guarding the checks that ARE clean.
var semanticCodes = map[string]bool{
	"duplicate-definition": true,
	"unknown-key":          true,
	"unresolved-ref":       true,
	"wrong-value-type":     true,
	"wrong-scope":          true,
	"unknown-event":        true,
	"required-loc":         true,
}

// knownVanillaFindings records findings that are TRUE on the game's own files,
// keyed "<game>/<code>" with the number reviewed and accepted.
//
// The gate's premise is that a finding on vanilla is the engine's fault, and it
// held for every check until duplicate-definition. Paradox ships real duplicates:
// Victoria 3 defines 14 coat-of-arms keys twice, `SCA` at 02_countries.txt:20690
// and again at 02_countries_2.txt:1, verified independently of PMT by scanning
// the files directly. Load order decides which survives, exactly as the message
// says, and a modder inheriting that file wants to know.
//
// A ceiling, not a filter: the findings are still reported and counted, and one
// more than recorded here fails. Add an entry only after tracing the finding to
// the game's own files and writing down what was found.
var knownVanillaFindings = map[string]int{
	// 10 cross-file coat-of-arms duplicates; 14 exist counting same-file pairs,
	// which this check does not report.
	"vic3/duplicate-definition": 10,
}

func TestVanillaDiagnosticsClean(t *testing.T) {
	if testing.Short() || os.Getenv("PMT_VANILLA_GATE") == "" {
		t.Skip("walks an install; set PMT_VANILLA_GATE=1")
	}
	// Asking for the gate and getting a pass in no time is the worst outcome
	// available: it reads as "vanilla is clean" when it means "nothing ran".
	// The loop below used to `continue` past a missing install, so with none set
	// this reported success having asserted nothing — the same hole the corpus
	// tier had, in the one test whose whole job is to be believed.
	ran := 0
	for _, g := range sweepGames {
		install := os.Getenv(g.installEnv)
		if install == "" {
			t.Errorf("PMT_VANILLA_GATE is set but %s is not: the gate cannot "+
				"clear a game it never scanned", g.installEnv)
			continue
		}
		ran++
		t.Run(g.game, func(t *testing.T) {
			id := "gate-" + g.game
			t.Cleanup(func() { _ = derived.DropVanillaFiles(id) })
			cache, vloc, err := derived.Scan(context.Background(), derived.ScanRequest{
				InstallID: id, GameID: g.game, InstallPath: install,
				Version: "gate", LocLang: "english",
			})
			if err != nil {
				t.Fatalf("scan: %v", err)
			}
			// Nested coverage is logged here because the scan has already
			// happened: the model that decides what completion offers inside a
			// block, and whether a field's description can be trusted, is a
			// per-game number worth seeing next to the diagnostics.
			t.Logf("NESTED %s: %d blocks", g.game, len(cache.NestedMembers))
			for _, b := range []string{"limit", "trigger", "modifier"} {
				if m := cache.NestedMembers[b]; len(m) > 0 {
					n := min(len(m), 5)
					t.Logf("  %s holds %v (+%d)", b, m[:n], len(m)-n)
				}
			}

			root := sampleIntoMod(t, install, g.game)
			s := session.NewWithLoc("gate", g.game, "english", cache, vloc,
				[]derived.ModInput{{Origin: "mod", Root: root, Name: "vanilla-sample"}})
			defer s.Close()

			counts := map[string]int{}
			examples := map[string]string{}
			files := 0
			_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
				if err != nil || d.IsDir() || !strings.HasSuffix(p, ".txt") {
					return nil
				}
				src := s.FileText(p)
				if src == "" {
					return nil
				}
				files++
				s.DidOpen(p, src)
				defer s.DidClose(p)
				for _, dg := range lsp.Diagnose(s, p) {
					counts[dg.Code]++
					if examples[dg.Code] == "" {
						examples[dg.Code] = s.DisplayRel(p) + ": " + dg.Message
					}
				}
				return nil
			})

			codes := make([]string, 0, len(counts))
			for c := range counts {
				codes = append(codes, c)
			}
			sort.Slice(codes, func(i, j int) bool {
				return counts[codes[i]] > counts[codes[j]]
			})
			t.Logf("GATE %s: %d files", g.game, files)
			for _, c := range codes {
				t.Logf("  %-24s %6d  e.g. %s", c, counts[c], examples[c])
			}
			for _, c := range codes {
				if !semanticCodes[c] {
					continue
				}
				if allow := knownVanillaFindings[g.game+"/"+c]; counts[c] <= allow {
					continue
				}
				t.Errorf("%s fires %d times on vanilla %s (%d known genuine) — "+
					"a check that flags the game's own script must not ship",
					c, counts[c], g.game, knownVanillaFindings[g.game+"/"+c])
			}
		})
	}
	if ran == 0 {
		t.Fatal("the gate scanned no game: a pass here means nothing")
	}
}

// sampleIntoMod copies gateFiles of the install's own script into a temporary
// mod and returns its root. Lexical order so two runs sample the same files.
func sampleIntoMod(t *testing.T, install, gameID string) string {
	t.Helper()
	root := t.TempDir()
	if gameID == "ck3" {
		writeGate(t, filepath.Join(root, "descriptor.mod"), "name = \"vanilla-sample\"\n")
	}
	var picked []string
	_ = filepath.WalkDir(install, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || len(picked) >= gateFiles {
			return nil
		}
		if strings.HasSuffix(p, ".txt") && strings.Contains(p, "common") {
			picked = append(picked, p)
		}
		return nil
	})
	sort.Strings(picked)
	for _, src := range picked {
		rel, err := filepath.Rel(install, src)
		if err != nil {
			continue
		}
		// Strip the leading "game/" so the copy sits where a mod's would.
		rel = strings.TrimPrefix(filepath.ToSlash(rel), "game/")
		body, err := os.ReadFile(src)
		if err != nil {
			continue
		}
		writeGate(t, filepath.Join(root, filepath.FromSlash(rel)), string(body))
	}
	return root
}

func writeGate(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestEveryShippingCheckIsGated pins that semanticCodes lists every check that
// is on by default, because the gate is what stops one reaching a modder while
// it still fires on the game's own files.
//
// unknown-token is why this test exists: it was written, registered and wired
// into Diagnose while semanticCodes -- a hand-kept list -- did not name it, so
// the gate reported 442 findings on vanilla and passed anyway.
func TestEveryShippingCheckIsGated(t *testing.T) {
	// Localization checks cannot be gated by this harness: it samples script
	// into a mod with no localization/ folder, so missing-required-loc fires
	// 1,831 times on CK3 by construction rather than by fault.
	exempt := map[string]string{
		"loc-header-mismatch":       "needs a localization folder the sample has none of",
		"missing-bom":               "same",
		"missing-descriptor":        "the sample mod is not a real mod",
		"missing-required-loc":      "same",
		"wrong-localization-folder": "same",
	}
	for _, c := range lsp.DiagnosticCodes() {
		if c.Group != lsp.GroupCheck || c.Experimental {
			continue // parse errors are the parser's; experimental codes are off
		}
		if semanticCodes[c.Code] || exempt[c.Code] != "" {
			continue
		}
		t.Errorf("%s ships on by default but no gate asserts it stays silent on "+
			"vanilla — add it to semanticCodes, or to exempt with a reason", c.Code)
	}
}
