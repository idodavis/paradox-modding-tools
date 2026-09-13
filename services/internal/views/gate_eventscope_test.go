// gate_eventscope_test.go lints EVERY vanilla events file for wrong-scope.
//
// Separate from the 400-file sample in gate_vanilla_test.go because deriving an
// event root from the on_actions that fire it newly scopes thousands of event
// bodies -- 4,376 on CK3 alone -- and a sample can miss the ones that matter.

package views

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"paradox-modding-tools/services/internal/lang/derived"
	"paradox-modding-tools/services/internal/lang/lsp"
	"paradox-modding-tools/services/internal/lang/session"
)

// Every vanilla events/ file, not a 400-file sample: deriving event roots from
// on_actions newly scopes thousands of event bodies, and the gate's sample may
// not reach them.
func TestVanillaEventScopeClean(t *testing.T) {
	if testing.Short() || os.Getenv("PMT_VANILLA_GATE") == "" {
		t.Skip("walks every vanilla events file; set PMT_VANILLA_GATE=1")
	}
	for _, g := range []struct{ id, root, evdir string }{
		{"ck3", `C:\Program Files (x86)\Steam\steamapps\common\Crusader Kings III`, "game/events"},
		{"vic3", `C:\Program Files (x86)\Steam\steamapps\common\Victoria 3`, "game/events"},
		{"eu5", `C:\Program Files (x86)\Steam\steamapps\common\Europa Universalis V`, "game/in_game/events"},
	} {
		t.Run(g.id, func(t *testing.T) {
			if _, err := os.Stat(g.root); err != nil {
				t.Skip("no install")
			}
			id := "evt-" + g.id
			t.Cleanup(func() { _ = derived.DropVanillaFiles(id) })
			cache, vloc, err := derived.Scan(context.Background(), derived.ScanRequest{
				InstallID: id, GameID: g.id, InstallPath: g.root, Version: "p", LocLang: "english",
			})
			if err != nil {
				t.Fatalf("scan: %v", err)
			}
			// Point a mod at the install's own events folder so they are linted.
			src := filepath.Join(g.root, filepath.FromSlash(g.evdir))
			if _, err := os.Stat(src); err != nil {
				t.Skipf("no events dir: %v", src)
			}
			mod := t.TempDir()
			dst := filepath.Join(mod, "events")
			_ = os.MkdirAll(dst, 0o755)
			n := 0
			_ = filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
				if err != nil || d.IsDir() || !strings.HasSuffix(p, ".txt") || n >= 250 {
					return nil
				}
				b, e := os.ReadFile(p)
				if e != nil {
					return nil
				}
				n++
				_ = os.WriteFile(filepath.Join(dst, d.Name()), b, 0o644)
				return nil
			})
			s := session.NewWithLoc("evt", g.id, "english", cache, vloc,
				[]derived.ModInput{{Origin: "mod", Root: mod, Name: "v"}})
			defer s.Close()
			counts := map[string]int{}
			ex := map[string]string{}
			_ = filepath.WalkDir(dst, func(p string, d os.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return nil
				}
				txt := s.FileText(p)
				if txt == "" {
					return nil
				}
				s.DidOpen(p, txt)
				defer s.DidClose(p)
				for _, dg := range lsp.Diagnose(s, p) {
					if dg.Code != "wrong-scope" {
						continue
					}
					counts[dg.Code]++
					if ex[dg.Message] == "" {
						ex[dg.Message] = fmt.Sprintf("%s:%d", d.Name(), dg.Range.Start.Line+1)
					}
				}
				return nil
			})
			t.Logf("%s: %d event files, wrong-scope = %d", g.id, n, counts["wrong-scope"])
			// Vanilla is correct by definition, so any wrong-scope report over
			// it is a false positive. This was a t.Logf and nothing else: a
			// check firing on every vanilla event file passed a test named
			// "…Clean". The samples below say which messages, and are why the
			// count alone was not thought to need asserting.
			if counts["wrong-scope"] > 0 {
				t.Errorf("%s: wrong-scope fires %d times on vanilla events — "+
					"a check that flags the game's own script must not ship",
					g.id, counts["wrong-scope"])
			}
			if n == 0 {
				t.Errorf("%s: no event files walked; a pass here means nothing", g.id)
			}
			i := 0
			for m, f := range ex {
				if i >= 6 {
					break
				}
				t.Logf("   %s  (%s)", m, f)
				i++
			}
		})
	}
}
