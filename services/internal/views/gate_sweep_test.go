// gate_sweep_test.go is the engine scoreboard: every Workspace Health check run
// against every workshop mod on this machine, so "the engine got better" is a
// number rather than a feeling.
//
// It is deliberately excluded from `task test` and `task test:full` — it walks
// three installs and 31 mods. Run it with `task test:sweep` when judging engine
// quality.
//
// The counts are NOT targets to drive to zero. Workshop mods go stale, and a mod
// referencing something a patch removed produces a correct finding. Rows are
// classified against testdata/health-baseline.json: a row listed there is a
// known-genuine defect with a stated reason, and anything else is a candidate
// false positive to investigate. To call a row false you must be able to point
// at where the thing IS defined, or name the engine mechanism that consumes it
// without a citation — a row nobody can explain is a candidate TRUE positive and
// gets baselined, never filtered away.

package views

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"testing"
	"time"

	"paradox-modding-tools/services/internal/lang/derived"
	"paradox-modding-tools/services/internal/lang/jomini"
	"paradox-modding-tools/services/internal/lang/lsp"
	"paradox-modding-tools/services/internal/lang/session"
)

const workshopRoot = `C:\Program Files (x86)\Steam\steamapps\workshop\content`

// sweepGames maps each supported game to its Steam workshop app id.
var sweepGames = []struct{ game, appID, installEnv string }{
	{"ck3", "1158310", "PMT_TEST_INSTALL_CK3"},
	{"vic3", "529340", "PMT_TEST_INSTALL_VIC3"},
	{"eu5", "3450310", "PMT_TEST_INSTALL_EU5"},
}

// totalConversions are reported separately. A mod that only adds events exercises
// almost nothing; these redefine the game and are where assumptions break.
var totalConversions = map[string]string{
	"1158310/2962333032": "A Game of Thrones",
	"1158310/2243307127": "The Fallen Eagle",
	"529340/3199730217":  "Hail",
	"3450310/3618199037": "Basileia Romaion: 1337",
}

// baselineEntry is one finding already reviewed and judged genuine.
type baselineEntry struct {
	Type   string `json:"type"` // dangling, missing, orphaned, untranslated, …
	Kind   string `json:"kind,omitempty"`
	Name   string `json:"name"`
	Reason string `json:"reason"` // why this is a real defect, not an engine fault
}

// baselineFile is keyed "<appID>/<modID>". Shape on disk:
//
//	{
//	  "1158310/2962333032": [
//	    {"type": "dangling", "kind": "traits", "name": "brave",
//	     "reason": "removed in CK3 1.13; the mod is a version behind"}
//	  ]
//	}
//
// An empty "kind" matches any kind for that name. Add an entry only once the
// finding has been shown to be a real defect in the mod — see the file comment.
type baselineFile map[string][]baselineEntry

func loadBaseline(t *testing.T) baselineFile {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "health-baseline.json"))
	if os.IsNotExist(err) {
		return baselineFile{}
	}
	if err != nil {
		t.Fatalf("read baseline: %v", err)
	}
	var b baselineFile
	if err := json.Unmarshal(raw, &b); err != nil {
		t.Fatalf("parse baseline: %v", err)
	}
	return b
}

func (b baselineFile) known(mod string, r HealthRow) bool {
	for _, e := range b[mod] {
		if e.Type == r.Type && e.Name == r.Name &&
			(e.Kind == "" || e.Kind == r.Kind) {
			return true
		}
	}
	return false
}

// modScore is one mod's findings, split by Health row type.
type modScore struct {
	mod, name    string
	byType       map[string]int
	diags        map[string]int // diagnostic code -> count, over the sampled files
	diagFiles    int
	unclassified []HealthRow
	rates        answerRates
	took         time.Duration
	peakHeapMB   uint64
}

// answerRates is how often the LSP has something to say. "Named" is the floor —
// the card identifies what the token is; "described" means it also explains it,
// which is the whole point for a new modder.
type answerRates struct {
	hoverAsked, hoverNamed, hoverDescribed, hoverSigned int
	compAsked, compAnswered                             int
}

func (a answerRates) String() string {
	pct := func(n, of int) string {
		if of == 0 {
			return "n/a"
		}
		return itoa(n*100/of) + "%"
	}
	return "hover named=" + pct(a.hoverNamed, a.hoverAsked) +
		" described=" + pct(a.hoverDescribed, a.hoverAsked) +
		" signed=" + pct(a.hoverSigned, a.hoverAsked) +
		" (n=" + itoa(a.hoverAsked) + ")" +
		" | completion answered=" + pct(a.compAnswered, a.compAsked) +
		" (n=" + itoa(a.compAsked) + ")"
}

// sampleAnswers walks a mod's script and asks the LSP the two questions a user
// asks constantly: "what is this?" and "what can go here?".
//
// Sampling is deterministic — lexical file order, every sampleStride-th token —
// so a rate moving between runs means the engine moved, not the sample.
func sampleAnswers(s *session.Session, root string) answerRates {
	var a answerRates
	files := 0
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || files >= sampleFiles {
			return nil
		}
		if !strings.HasSuffix(p, ".txt") || !strings.Contains(p, "common") {
			return nil
		}
		src := s.FileText(p)
		if src == "" {
			return nil
		}
		files++
		// Open the file first. Hover and Complete both reach probeAt, which
		// re-reads and re-parses any file that is not an open buffer — so
		// sampling fifty positions meant fifty parses of the same file. It is
		// also the honest measurement: a user hovering a file has it open.
		s.DidOpen(p, src)
		defer s.DidClose(p)
		li := jomini.NewLineIndex(src)
		res := jomini.Parse(src)
		n := 0
		jomini.Walk(res.Root, func(st jomini.Statement, _ int, _ *jomini.Block) bool {
			asn, ok := st.(*jomini.Assignment)
			if !ok || asn.Key.Quoted {
				return true
			}
			n++
			if n%sampleStride != 0 {
				return true
			}
			// "What is this?" — asked on the key.
			pos := li.PositionAt(asn.Key.Range.Start)
			a.hoverAsked++
			if h := lsp.Hover(s, p, pos.Line, pos.Character); h != nil {
				if h.Kind != "" || h.Key != "" {
					a.hoverNamed++
				}
				if h.Docs != "" || h.Hint != "" {
					a.hoverDescribed++
				}
				if h.Usage != "" {
					a.hoverSigned++
				}
			}
			// "What can go here?" — asked at the value position, but only where
			// a name is expected. For a boolean or numeric field the yes/no
			// fallback IS the right answer, and counting those as unanswered
			// would understate the engine and inflate every later improvement.
			if sc, ok := asn.Value.(*jomini.Scalar); ok && !sc.Quoted &&
				sc.Text != "" && !jomini.IsLiteralValue(sc.Text) {
				vp := li.PositionAt(sc.Range.Start)
				a.compAsked++
				if answered(lsp.Complete(s, p, vp.Line, vp.Character)) {
					a.compAnswered++
				}
			}
			return true
		})
		return nil
	})
	return a
}

const (
	sampleFiles  = 40 // per conversion; lexical order, so the sample is stable
	sampleStride = 7  // every Nth assignment, to spread across a file
)

// answered reports a completion list that says more than "yes or no". Falling
// through to the boolean fallback is the signal that nothing was known.
func answered(items []lsp.CompletionItem) bool {
	for _, it := range items {
		if it.Label != "yes" && it.Label != "no" {
			return true
		}
	}
	return false
}

// TestSweepWorkspaceHealth runs each game as a subtest so `go test -v` streams a
// game's results the moment it finishes, rather than buffering every log line
// until the whole sweep completes. On a multi-minute run that is the difference
// between watching progress and staring at nothing.
func TestSweepWorkspaceHealth(t *testing.T) {
	if testing.Short() || os.Getenv("PMT_TEST_SWEEP") == "" {
		t.Skip("~9min; run `task test:sweep`")
	}
	base := loadBaseline(t)
	for _, g := range sweepGames {
		t.Run(g.game, func(t *testing.T) { sweepGame(t, g.game, g.appID, g.installEnv, base) })
	}
}

func sweepGame(t *testing.T, game, appID, installEnv string, base baselineFile) {
	install := os.Getenv(installEnv)
	if install == "" {
		corpusEnv(t, "%s not set", installEnv)
	}
	ents, err := os.ReadDir(filepath.Join(workshopRoot, appID))
	if err != nil {
		t.Skipf("no workshop content (%v)", err)
	}
	id := "sweep-" + game
	t.Cleanup(func() { _ = derived.DropVanillaFiles(id) })
	scanStart := time.Now()
	cache, vloc, err := derived.Scan(context.Background(), derived.ScanRequest{
		InstallID: id, GameID: game, InstallPath: install,
		Version: "sweep", LocLang: "english",
	})
	if err != nil {
		t.Fatalf("%s scan: %v", game, err)
	}
	scanTook := time.Since(scanStart)

	// The four total conversions plus two small mods per game. All 31 took ~7
	// minutes and the extra 24 are small mods that add events; the conversions
	// are where the engine actually breaks. PMT_SWEEP_ALL=1 runs everything.
	var dirs []os.DirEntry
	small := 0
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		if totalConversions[appID+"/"+e.Name()] == "" && os.Getenv("PMT_SWEEP_ALL") == "" {
			if small >= 2 {
				continue
			}
			small++
		}
		dirs = append(dirs, e)
	}
	// Serial, and the memory is handed back between mods.
	//
	// Mods are independent and this was parallel at first, but memory is the
	// binding constraint, not CPU: each session holds its whole mod index, A
	// Game of Thrones alone is 21,499 files, and Session.Close only stops the
	// watcher — the indexes wait on the collector. Two workers reached 9 GB on
	// a 31 GB machine, which is not acceptable for a tool meant to run while
	// someone is using their computer. Serial plus FreeOSMemory keeps the
	// working set to one mod, and the sweep was never CPU-bound anyway.
	scores := make([]modScore, len(dirs))
	for i, e := range dirs {
		modStart := time.Now()
		key := appID + "/" + e.Name()
		scores[i] = func() modScore {
			s := session.NewWithLoc("sweep", game, "english", cache, vloc,
				[]derived.ModInput{{
					Origin: "mod",
					Root:   filepath.Join(workshopRoot, appID, e.Name()),
					Name:   e.Name(),
				}})
			defer s.Close()
			sc := modScore{mod: key, name: totalConversions[key], byType: map[string]int{}}
			rep := Health(s, nil)
			// Counts come from the report's own totals, which are uncapped.
			// Summing Rows instead measures the capped view — healthLocCap keeps
			// 500 loc rows per language — so fixing one category frees budget
			// for another and the fix reads as a regression in whatever moves
			// into the freed slots. Rows are still the source for the detail
			// below, where being capped only costs examples.
			sc.byType["dangling"] = rep.Dangling
			sc.byType["missing"] = rep.Missing
			sc.byType["orphaned"] = rep.Orphaned
			sc.byType["untranslated"] = rep.Untranslated
			sc.byType["conflict"] = rep.Conflicts
			sc.byType["override"] = rep.Overrides
			sc.byType["depends"] = rep.Depends
			for _, r := range rep.Rows {
				if isFault(r.Type) && !base.known(key, r) {
					sc.unclassified = append(sc.unclassified, r)
				}
			}
			// Answer rates are sampled on the total conversions only. They are
			// the corpus that exercises the engine, and sampling every mod
			// would cost far more than it tells us.
			if sc.name != "" {
				sc.rates = sampleAnswers(s, filepath.Join(workshopRoot, appID, e.Name()))
			}
			// Diagnostics belong on the scoreboard for the same reason the
			// Health rows do: a check that fires on correct script teaches a new
			// modder that correct script is wrong. Counted per code so a change
			// to one check is visible instead of averaged away.
			sc.diags, sc.diagFiles = sampleDiagnostics(
				s, filepath.Join(workshopRoot, appID, e.Name()))
			sc.took = time.Since(modStart)
			var ms runtime.MemStats
			runtime.ReadMemStats(&ms)
			sc.peakHeapMB = ms.HeapAlloc / (1 << 20)
			return sc
		}()
		debug.FreeOSMemory()
	}
	t.Logf("SWEEP %s: install scan %s, %d mods in %s",
		game, scanTook.Round(time.Millisecond*100),
		len(dirs), (time.Since(scanStart) - scanTook).Round(time.Millisecond*100))
	// The corpus is not frozen: Steam updates workshop mods underneath us. One
	// Victoria 3 mod gained 320 files mid-session, which moved `missing` by 445
	// and changed the sampled token count — movement that looks exactly like an
	// engine regression and is not one. Two runs are only comparable when this
	// line matches, so print it beside the counts rather than leaving the next
	// reader to wonder.
	t.Logf("SWEEP %s  corpus: %s", game, corpusStamp(dirs, appID))
	reportGame(t, game, scores)
}

// corpusStamp fingerprints the workshop content a run measured: how many mods,
// and the newest file mtime across them.
func corpusStamp(dirs []os.DirEntry, appID string) string {
	var newest time.Time
	files := 0
	for _, e := range dirs {
		_ = filepath.WalkDir(filepath.Join(workshopRoot, appID, e.Name()),
			func(_ string, d os.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return nil
				}
				files++
				if fi, err := d.Info(); err == nil && fi.ModTime().After(newest) {
					newest = fi.ModTime()
				}
				return nil
			})
	}
	return itoa(len(dirs)) + " mods, " + itoa(files) + " files, newest " +
		newest.UTC().Format("2006-01-02T15:04Z")
}

// isFault reports the row types that assert something is wrong. Overrides,
// conflicts and dependencies describe a workspace; they are not defects.
// isFault separates the rows that accuse the modder of a defect from the ones
// that report hygiene. "your script references something that does not exist"
// must be right, and is bounded — 186 dangling rows across all three games.
// "you have localization nothing uses" is a cleanup list: over-reporting it is
// noise in a count, not a false accusation, and driving it to zero would mean
// tuning the engine until it hides real findings. Orphaned and untranslated are
// still counted and still shown, under their own filters; they just do not gate
// the phase. See GAME-SYNTAX §13.
func isFault(rowType string) bool {
	switch rowType {
	case "dangling", "missing":
		return true
	}
	return false
}

// assertDanglingCeiling is the sweep's one gate, and it covers dangling only.
//
// V.3 called this test out for counting `unclassified` and never asserting it.
// Asserting it whole is not available: `missing` is thousands of rows and moves
// whenever a workshop mod ships a new string, so a ratchet on it fails on other
// people's updates rather than on a PMT change. `dangling` is the other half of
// isFault and it is bounded — 182 rows across all three games — and it is the
// row that accuses the modder of referencing something that does not exist.
//
// A ceiling rather than zero because the backlog is real and unreviewed: the
// classification work is Phase 7's. A ceiling still makes a NEW dangling row a
// failure, which is what the baseline machinery exists for. Lower it as rows
// are classified; raising it needs a reason in the commit.
func assertDanglingCeiling(t *testing.T, game string, scores []modScore) {
	t.Helper()
	path := filepath.Join("testdata", "health-ceilings.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v (the ceiling is part of the gate, not optional)", path, err)
	}
	var ceil map[string]int
	if err := json.Unmarshal(raw, &ceil); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	limit, ok := ceil[game]
	if !ok {
		t.Fatalf("%s has no ceiling in %s", game, path)
	}
	n := 0
	for _, sc := range scores {
		for _, r := range sc.unclassified {
			if r.Type == "dangling" {
				n++
			}
		}
	}
	t.Logf("SWEEP %s: %d unclassified dangling rows, ceiling %d", game, n, limit)
	if n > limit {
		t.Errorf("%s: %d unclassified dangling rows against a ceiling of %d — "+
			"either the engine regressed, or the rows are genuine mod defects "+
			"and belong in testdata/health-baseline.json with a reason",
			game, n, limit)
	}
}

func reportGame(t *testing.T, game string, scores []modScore) {
	t.Helper()
	sort.Slice(scores, func(i, j int) bool {
		return len(scores[i].unclassified) > len(scores[j].unclassified)
	})

	totals := map[string]int{}
	diagTotals := map[string]int{}
	clean, unclassified, diagFiles := 0, 0, 0
	var peakHeapMB uint64
	for _, sc := range scores {
		for k, v := range sc.byType {
			totals[k] += v
		}
		for code, n := range sc.diags {
			diagTotals[code] += n
		}
		diagFiles += sc.diagFiles
		if sc.peakHeapMB > peakHeapMB {
			peakHeapMB = sc.peakHeapMB
		}
		if len(sc.unclassified) == 0 {
			clean++
		}
		unclassified += len(sc.unclassified)
	}
	t.Logf("SWEEP %s: %d mods, %d with nothing unclassified, %d unclassified rows",
		game, len(scores), clean, unclassified)
	assertDanglingCeiling(t, game, scores)
	// Live heap after a mod, not process RSS: RSS also holds memory the
	// collector has freed but not returned, which says more about GOGC than
	// about what a workspace costs. Phase 9 wants to build a second model while
	// one is live, so this number is the budget that decision is made against.
	t.Logf("SWEEP %s  live heap after a mod, worst: %d MB", game, peakHeapMB)
	t.Logf("SWEEP %s  diagnostics over %d sampled files: %s",
		game, diagFiles, fmtCounts(diagTotals))
	// Totals are distinct problems, from the report's uncapped counters. The
	// per-cluster lines below count references instead, so a row citing one
	// name 859 times reads as 859 there and as 1 here — different questions.
	t.Logf("SWEEP %s  totals (distinct): %s", game, fmtCounts(totals))

	// Total conversions first, then the worst of the rest.
	for _, sc := range scores {
		if sc.name == "" {
			continue
		}
		t.Logf("SWEEP %s  [conversion] %s (%s): %s | %d unclassified",
			game, sc.name, sc.took.Round(time.Millisecond*100),
			fmtCounts(sc.byType), len(sc.unclassified))
		t.Logf("SWEEP %s        %s", game, sc.rates)
		logWorst(t, game, sc)
	}
	shown := 0
	for _, sc := range scores {
		if sc.name != "" || len(sc.unclassified) == 0 || shown >= 5 {
			continue
		}
		t.Logf("SWEEP %s  %s: %s | %d unclassified",
			game, sc.mod, fmtCounts(sc.byType), len(sc.unclassified))
		logWorst(t, game, sc)
		shown++
	}
}

// logWorst prints the biggest unclassified clusters. Grouping by kind is what
// makes a cause visible; grouping by name only ever shows symptoms.
func logWorst(t *testing.T, game string, sc modScore) {
	t.Helper()
	// Loc findings are per language, so the language is part of the cluster: a
	// key "missing" in twelve languages and one missing in english are entirely
	// different problems, and merging them hides which.
	byCluster := map[string]int{}
	langs := map[string]map[string]bool{}
	for _, r := range sc.unclassified {
		key := r.Type + " " + r.Kind + "/" + r.Name
		byCluster[key] += max(r.Refs, 1)
		if r.Language != "" {
			if langs[key] == nil {
				langs[key] = map[string]bool{}
			}
			langs[key][r.Language] = true
		}
	}
	type kv struct {
		k string
		n int
	}
	rows := make([]kv, 0, len(byCluster))
	for k, n := range byCluster {
		rows = append(rows, kv{k, n})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].n != rows[j].n {
			return rows[i].n > rows[j].n
		}
		return rows[i].k < rows[j].k
	})
	if len(rows) > 6 {
		rows = rows[:6]
	}
	for _, r := range rows {
		t.Logf("SWEEP %s        %5d  %s%s", game, r.n, r.k, fmtLangs(langs[r.k]))
	}
}

func fmtCounts(m map[string]int) string {
	keys := make([]string, 0, len(m))
	for k, v := range m {
		if v == 0 {
			continue // a category with nothing to say is noise
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+itoa(m[k]))
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, " ")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// fmtLangs names the languages a loc finding spans. "all 12 langs" is the tell
// for an inherit failure — the key exists, but the vanilla sidecar for that
// language does not, so every translation reports it missing.
func fmtLangs(set map[string]bool) string {
	if len(set) == 0 {
		return ""
	}
	out := make([]string, 0, len(set))
	for l := range set {
		out = append(out, l)
	}
	sort.Strings(out)
	if len(out) > 3 {
		return "  [" + itoa(len(out)) + " langs: " + strings.Join(out[:3], ",") + ",…]"
	}
	return "  [" + strings.Join(out, ",") + "]"
}

// sampleDiagnostics counts published diagnostics by code over the same
// deterministic file sample the answer rates use, so the two move together and
// a run is comparable to the last one.
//
// The count is the point, not the total: `required-loc` asserts the game DEMANDS
// a key, which is a strong enough claim to put a warning in someone's editor, so
// its volume is the check on the coverage floor that decides which naming
// conventions are required rather than merely common.
func sampleDiagnostics(s *session.Session, root string) (map[string]int, int) {
	out := map[string]int{}
	files := 0
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || files >= sampleFiles {
			return nil
		}
		if !strings.HasSuffix(p, ".txt") || !strings.Contains(p, "common") {
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
			out[dg.Code]++
		}
		return nil
	})
	return out, files
}

// guards the scoreboard's usefulness. A count that moves
// between runs cannot be a regression threshold, and CK3 orphaned came back
// 76,041 / 76,064 / 76,071 across three sweeps of the same corpus.
//
// The two halves are separated on purpose: one session built twice from one
// cache isolates Coverage, and two caches from two scans isolates the parallel
// install walk. Whichever differs is the one to fix. Both faults it has caught
// so far were real model bugs rather than reporting artifacts — see
// GAME-SYNTAX.md §6, "Scheduling must not decide anything".
//
// By default it checks one Victoria 3 conversion, which is seconds. Widen it
// when chasing something: an empty PMT_DET_MOD runs every mod of the game,
// which is ~45s for Victoria 3 and ~11min for EU5.
func TestHealthIsDeterministic(t *testing.T) {
	if testing.Short() || os.Getenv("PMT_TEST_SWEEP") == "" {
		t.Skip("scans a real install and a conversion; run `task test:sweep`")
	}
	// Defaults to a Victoria 3 conversion because it is the fastest one that
	// exercises the whole path. Point it elsewhere to chase a specific drift:
	//   PMT_DET_GAME=ck3 PMT_DET_APP=1158310 PMT_DET_MOD=2962333032
	game, appID, modID := "vic3", "529340", "3199730217"
	if v := os.Getenv("PMT_DET_GAME"); v != "" {
		game, appID, modID = v, os.Getenv("PMT_DET_APP"), os.Getenv("PMT_DET_MOD")
	}
	install := os.Getenv("PMT_TEST_INSTALL_" + strings.ToUpper(game))
	if install == "" {
		corpusEnv(t, "PMT_TEST_INSTALL_%s not set", strings.ToUpper(game))
	}
	// An empty PMT_DET_MOD checks every mod of the game. One mod passing proves
	// little: the drift chased here was visible only in a game's total, and the
	// mod carrying it was not the obvious one.
	var mods []string
	if modID != "" {
		mods = []string{modID}
	} else {
		ents, err := os.ReadDir(filepath.Join(workshopRoot, appID))
		if err != nil {
			t.Skipf("no workshop content (%v)", err)
		}
		for _, e := range ents {
			if e.IsDir() {
				mods = append(mods, e.Name())
			}
		}
	}
	scan := func(id string) (*derived.VanillaCache, *derived.VanillaLoc) {
		t.Helper()
		t.Cleanup(func() { _ = derived.DropVanillaFiles(id) })
		c, vloc, err := derived.Scan(context.Background(), derived.ScanRequest{
			InstallID: id, GameID: game, InstallPath: install,
			Version: "det", LocLang: "english",
		})
		if err != nil {
			t.Fatalf("scan %s: %v", id, err)
		}
		return c, vloc
	}
	report := func(c *derived.VanillaCache, vloc *derived.VanillaLoc, mod string) HealthReport {
		s := session.NewWithLoc("det", game, "english", c, vloc,
			[]derived.ModInput{{
				Origin: "mod", Root: filepath.Join(workshopRoot, appID, mod), Name: "m",
			}})
		defer s.Close()
		return Health(s, nil)
	}
	same := func(what, mod string, a, b HealthReport) {
		t.Helper()
		if a.Missing != b.Missing || a.Orphaned != b.Orphaned ||
			a.Untranslated != b.Untranslated || a.Dangling != b.Dangling {
			t.Errorf("%s [%s]: missing %d/%d orphaned %d/%d untranslated %d/%d dangling %d/%d",
				what, mod, a.Missing, b.Missing, a.Orphaned, b.Orphaned,
				a.Untranslated, b.Untranslated, a.Dangling, b.Dangling)
		}
	}

	c1, l1 := scan("det-1")
	c2, l2 := scan("det-2")
	for _, mod := range mods {
		same("same cache, two sessions", mod,
			report(c1, l1, mod), report(c1, l1, mod))
		same("two scans of one install", mod,
			report(c1, l1, mod), report(c2, l2, mod))
		debug.FreeOSMemory()
	}
}

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
