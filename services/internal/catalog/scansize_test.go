// scansize_test.go scans a real install and holds the on-disk model to a size
// ceiling. Before path interning the CK3 model was 813 MB, which had to be
// deserialized on every session open. Gated on PMT_TEST_INSTALL_*; slow.

package catalog

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// cacheCeilingMB is the size a real install's model must stay under. CK3, the
// largest, measures ~118 MB; the headroom absorbs content growth across patches
// while still catching a return to the old 813 MB.
const cacheCeilingMB = 160

// heapCeilingGB caps peak heap during a scan. Retaining every parse tree cost
// 4.3 GB and drove CK3 to an 8.06 GB peak; streaming the corpus brought that to
// 2.15 GB, with EU5 the worst at 3.46 GB because a few of its files are huge.
// The ceiling catches a return to retaining trees, not ordinary drift.
const heapCeilingGB = 5.0

// peakHeapDuring samples the heap while fn runs. Sampling on a ticker rather
// than a busy loop matters: a busy loop competes with the scan and distorts it.
func peakHeapDuring(fn func()) float64 {
	var peak uint64
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		var m runtime.MemStats
		tk := time.NewTicker(10 * time.Millisecond)
		defer tk.Stop()
		for {
			select {
			case <-stop:
				return
			case <-tk.C:
				runtime.ReadMemStats(&m)
				if m.HeapAlloc > peak {
					peak = m.HeapAlloc
				}
			}
		}
	}()
	fn()
	close(stop)
	<-done
	return float64(peak) / (1 << 30)
}

func scanRealInstall(t *testing.T, gameID, installEnv string) {
	t.Helper()
	if testing.Short() {
		t.Skip("scans a full game install")
	}
	install := os.Getenv(installEnv)
	if install == "" {
		corpusEnv(t, "%s not set", installEnv)
	}
	if _, err := os.Stat(install); err != nil {
		t.Skipf("%s: %v", installEnv, err)
	}

	id := "sizetest-" + gameID
	t.Cleanup(func() { _ = DropVanillaFiles(id) })

	var c *VanillaCache
	var err error
	peak := peakHeapDuring(func() {
		c, _, err = Scan(context.Background(), ScanRequest{
			InstallID: id, GameID: gameID, InstallPath: install,
			Version: "sizetest", LocLang: "english",
		})
	})
	t.Logf("%s: peak heap %.2f GB", gameID, peak)
	if peak > heapCeilingGB {
		t.Errorf("%s scan peaked at %.2f GB, ceiling %.1f GB — is the corpus being retained?",
			gameID, peak, heapCeilingGB)
	}
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if err := SaveCache(c); err != nil {
		t.Fatalf("SaveCache: %v", err)
	}

	dir, err := CacheDir()
	if err != nil {
		t.Fatal(err)
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var model, loc int64
	locFiles := 0
	for _, e := range ents {
		name := e.Name()
		if !strings.Contains(name, id) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if strings.HasPrefix(name, "vanilla-loc-") {
			loc += info.Size()
			locFiles++
			continue
		}
		if strings.HasPrefix(name, "vanilla-") {
			model += info.Size()
		}
	}
	mb := func(n int64) float64 { return float64(n) / (1 << 20) }
	t.Logf("%s: %d defs, %d edges, %d callRefs, %d locRefs",
		gameID, len(c.Defs), len(c.Edges), len(c.CallRefs), len(c.LocRefs))
	byEdgeKind := map[string]int{}
	for _, e := range c.Edges {
		byEdgeKind[e.Kind]++
	}
	t.Logf("%s: edges by kind %v", gameID, byEdgeKind)
	byRefKind := map[string]int{}
	for _, r := range c.CallRefs {
		byRefKind[r.Kind]++
	}
	t.Logf("%s: callRefs by kind %v", gameID, byRefKind)
	t.Logf("%s: model %.1f MB, loc %.1f MB across %d sidecars, total %.1f MB",
		gameID, mb(model), mb(loc), locFiles, mb(model+loc))

	if locFiles > 2 {
		t.Errorf("%d loc sidecars written; expected at most default + english", locFiles)
	}
	if got := mb(model); got > cacheCeilingMB {
		t.Errorf("%s model is %.1f MB, ceiling is %d MB", gameID, got, cacheCeilingMB)
	}
	// The interning table must actually be gone after a load.
	loaded, err := LoadCache(id, "sizetest")
	if err != nil {
		t.Fatalf("LoadCache: %v", err)
	}
	if len(loaded.Paths) != 0 {
		t.Errorf("loaded cache still carries a Paths table")
	}
	if len(loaded.Defs) != len(c.Defs) {
		t.Errorf("round trip lost defs: %d -> %d", len(c.Defs), len(loaded.Defs))
	}
	// Paths must come back as real locations, not indices.
	for _, d := range loaded.Defs {
		if d.Path != "" && !filepath.IsAbs(d.Path) {
			t.Fatalf("def path did not expand: %+v", d)
		}
		break
	}
}

func TestScanSizeCK3(t *testing.T)  { scanRealInstall(t, "ck3", "PMT_TEST_INSTALL_CK3") }
func TestScanSizeVic3(t *testing.T) { scanRealInstall(t, "vic3", "PMT_TEST_INSTALL_VIC3") }
func TestScanSizeEU5(t *testing.T)  { scanRealInstall(t, "eu5", "PMT_TEST_INSTALL_EU5") }

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
