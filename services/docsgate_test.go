// agentsgate_test.go holds AGENTS.md against the code it describes.
//
// AGENTS.md is read as ground truth by every contributor and every AI session,
// so a wrong line there does not stay one wrong line — it propagates into every
// decision made after it. It claimed DeleteWorkspace had been removed while two
// Vue pages were calling it, and it named cache format 19 for a cache at 35.
// Neither is the sort of line anyone re-reads, and both are trivially checkable.
//
// It lives in services because the format constants are behind services/internal,
// which only this package's tree may import.

package services

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"paradox-modding-tools/services/internal/catalog"
)

func readAgentsMD(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// servicesBullet is the "Wails registered services" line, where every claim
// about what is and is not bound lives.
func servicesBullet(t *testing.T, md string) string {
	t.Helper()
	for line := range strings.SplitSeq(md, "\n") {
		if strings.HasPrefix(line, "- **Wails registered services:**") {
			return line
		}
	}
	t.Fatal("AGENTS.md no longer has a 'Wails registered services' bullet, " +
		"so this test cannot check what it claims")
	return ""
}

var backticked = regexp.MustCompile("`([A-Za-z][A-Za-z0-9_]*)`")

// absentRPCClaims are the names AGENTS.md says the frontend cannot call: the
// "Removed Wails RPCs" list plus the "no `X`" mentions on the same bullet.
// Lowercase matches are skipped — `go:embed` and friends are prose, not RPCs.
func absentRPCClaims(t *testing.T, md string) []string {
	t.Helper()
	line := servicesBullet(t, md)
	var out []string
	add := func(seg string) {
		for _, m := range backticked.FindAllStringSubmatch(seg, -1) {
			if n := m[1]; n[0] >= 'A' && n[0] <= 'Z' {
				out = append(out, n)
			}
		}
	}
	// Only the name right after "no ", not the rest of the sentence.
	for _, seg := range strings.Split(line, "no ")[1:] {
		add(seg[:min(len(seg), 40)])
	}
	if i := strings.Index(line, "Removed Wails RPCs:"); i >= 0 {
		seg := line[i:]
		if j := strings.Index(seg, "("); j >= 0 {
			seg = seg[:j]
		}
		add(seg)
	}
	if len(out) == 0 {
		t.Fatal("parsed no absent-RPC claims from AGENTS.md: the bullet's " +
			"wording changed and this test is no longer checking anything")
	}
	return out
}

var tsExport = regexp.MustCompile(`(?m)^export function ([A-Za-z][A-Za-z0-9_]*)\(`)

// boundRPCs reads the generated bindings, which are the honest answer to "what
// can the frontend call" — they are the files the frontend imports.
func boundRPCs(t *testing.T) map[string]string {
	t.Helper()
	dir := filepath.Join("..", "frontend", "bindings", "paradox-modding-tools", "services")
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Skipf("bindings not generated: %v", err)
	}
	out := map[string]string{}
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".ts") || e.Name() == "models.ts" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range tsExport.FindAllStringSubmatch(string(raw), -1) {
			out[m[1]] = e.Name()
		}
	}
	if len(out) == 0 {
		t.Fatal("parsed no exports from the bindings: their shape changed")
	}
	return out
}

func TestAgentsMDAbsentRPCsAreActuallyAbsent(t *testing.T) {
	bound := boundRPCs(t)
	claims := absentRPCClaims(t, readAgentsMD(t))
	t.Logf("checking %d absent-RPC claims against %d bound RPCs",
		len(claims), len(bound))
	for _, name := range claims {
		if file, hit := bound[name]; hit {
			t.Errorf("AGENTS.md says %s is not bound, but %s exports it", name, file)
		}
	}
}

// A format bump invalidates the sentence describing it and nothing notices: the
// constant is in Go, the claim is in prose, and the artefact they both describe
// is on a user's disk.
func TestAgentsMDFormatVersions(t *testing.T) {
	md := readAgentsMD(t)
	cases := []struct {
		what, filename string
		want           int
	}{
		{"VanillaCache", `vanilla-\{installId\}-\{version\}\.json`, catalog.CacheFormatVersion},
		{"vanilla loc sidecar", `vanilla-loc-\{installId\}-\{version\}-\{lang\}\.json`, catalog.LocFormatVersion},
		{"app config", `config\.json`, configFormatVersion},
	}
	for _, c := range cases {
		t.Run(c.what, func(t *testing.T) {
			// The version is stated within a clause or so of the filename.
			re := regexp.MustCompile("`" + c.filename +
				"`[^\n]{0,60}?[Ff]ormat(?: ?[Vv]ersion)?`? (\\d+)")
			m := re.FindStringSubmatch(md)
			if m == nil {
				t.Fatalf("AGENTS.md no longer states a format version near %s. "+
					"Restore it or delete this case — do not leave the claim "+
					"unchecked, which is how it reached 19 for a cache at %d",
					c.what, c.want)
			}
			if got, _ := strconv.Atoi(m[1]); got != c.want {
				t.Errorf("AGENTS.md says format %d, code says %d", got, c.want)
			}
		})
	}
}

// --- GAME-SYNTAX.md ----------------------------------------------------------

func readGameSyntax(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "GAME-SYNTAX.md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// gameSyntaxHeading matches "## 3f. A thing" and captures the section id.
var gameSyntaxHeading = regexp.MustCompile(`(?m)^## ([0-9]+[a-z]?)\. `)

func gameSyntaxSections(t *testing.T) map[string]int {
	t.Helper()
	out := map[string]int{}
	for _, m := range gameSyntaxHeading.FindAllStringSubmatch(readGameSyntax(t), -1) {
		out[m[1]]++
	}
	if len(out) < 10 {
		t.Fatalf("parsed only %d sections; the heading format changed and this "+
			"test is no longer checking anything", len(out))
	}
	return out
}

// A section number has to name one section. GAME-SYNTAX carried two verbatim
// copies of "11. Primitives", the second sitting between 13 and 14 — so every
// reference to §11 was ambiguous and the duplicate was invisible to anyone
// reading top to bottom.
func TestGameSyntaxSectionNumbersAreUnique(t *testing.T) {
	for id, n := range gameSyntaxSections(t) {
		if n > 1 {
			t.Errorf("section %s appears %d times", id, n)
		}
	}
}

// gameSyntaxRef matches every spelling in use: "GAME-SYNTAX 13",
// "GAME-SYNTAX §12", "GAME-SYNTAX.md §3e".
var gameSyntaxRef = regexp.MustCompile(`GAME-SYNTAX(?:\.md)?[ ,]*(?:§ ?)?([0-9]+[a-z]?)`)

// Every cross-reference must land somewhere. These are load-bearing: the
// sections hold the measurement a constant or a check is defended by, so a
// reference to a section that does not exist is a defence nobody can find.
func TestGameSyntaxReferencesResolve(t *testing.T) {
	sections := gameSyntaxSections(t)
	checked := 0
	roots := []string{".", ".."}
	for _, root := range roots {
		err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				if d != nil && d.IsDir() && (d.Name() == "node_modules" ||
					d.Name() == ".git" || d.Name() == "bindings") {
					return filepath.SkipDir
				}
				return nil
			}
			ext := filepath.Ext(p)
			if ext != ".go" && ext != ".md" {
				return nil
			}
			// The source of truth cites its own sections constantly.
			if filepath.Base(p) == "GAME-SYNTAX.md" {
				return nil
			}
			raw, err := os.ReadFile(p)
			if err != nil {
				return nil
			}
			for _, m := range gameSyntaxRef.FindAllStringSubmatch(string(raw), -1) {
				checked++
				if sections[m[1]] == 0 {
					t.Errorf("%s cites GAME-SYNTAX %s, which has no section", p, m[1])
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if checked == 0 {
		t.Fatal("found no GAME-SYNTAX references at all; the walk is broken")
	}
	t.Logf("resolved %d references against %d sections", checked, len(sections))
}

// §7 is the index of what defends the facts in this file, and it named
// `session/scopereal_test.go` — a file that has never existed. An index nobody
// can follow is worse than no index: it reads as evidence that a check exists.
func TestGameSyntaxAssertionTableNamesRealThings(t *testing.T) {
	md := readGameSyntax(t)
	start := strings.Index(md, "## 7. ")
	if start < 0 {
		t.Fatal("GAME-SYNTAX has no section 7; this test's subject is gone")
	}
	sec := md[start:]
	if end := strings.Index(sec, "\n## "); end > 0 {
		sec = sec[:end]
	}

	files := map[string]bool{}   // both "catalog/bind_test.go" and "bind_test.go"
	testFns := map[string]bool{} // func TestX
	fnRe := regexp.MustCompile(`(?m)^func (Test[A-Za-z0-9_]*)\(`)
	err := filepath.WalkDir(".", func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") {
			return nil
		}
		slash := filepath.ToSlash(p)
		files[slash] = true
		files[filepath.Base(p)] = true
		// Also the package-qualified form the table actually uses.
		if i := strings.LastIndex(strings.TrimSuffix(slash, "/"+filepath.Base(p)), "/"); i >= 0 {
			files[slash[i+1:]] = true
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		for _, m := range fnRe.FindAllStringSubmatch(string(raw), -1) {
			testFns[m[1]] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(testFns) == 0 || len(files) == 0 {
		t.Fatal("collected nothing from the tree; the walk is broken")
	}

	checked := 0
	for _, m := range backticked.FindAllStringSubmatch(sec, -1) {
		name := m[1]
		switch {
		case strings.HasPrefix(name, "Test"):
			checked++
			if !testFns[name] {
				t.Errorf("§7 names test %s, which does not exist", name)
			}
		}
	}
	// backticked only matches bare identifiers; paths need their own pass.
	pathRe := regexp.MustCompile("`([A-Za-z0-9_./-]+[.]go)`")
	for _, m := range pathRe.FindAllStringSubmatch(sec, -1) {
		checked++
		if !files[m[1]] {
			t.Errorf("§7 names %s, which does not exist under services/", m[1])
		}
	}
	if checked == 0 {
		t.Fatal("§7's table parsed to nothing; its shape changed")
	}
	t.Logf("checked %d names in §7", checked)
}

// --- Repo shape (plan R2) ----------------------------------------------------

// maxGoFiles and maxFileLines are a RATCHET, not a target. R1 takes services
// from 154 files to ~80; until then these stop the count growing, which is what
// it has been doing — nine files were added in one 2026-09-09 session.
//
// Lower them when R1 lands. Raising either needs a reason in the commit, and
// "this is a different topic" is not one: topics are what headers are for. Split
// a file at 900 lines, at a build-tag boundary, or at a real package boundary.
const (
	maxGoFiles   = 154
	maxFileLines = 900
)

// overLongFiles are the eight files already past maxFileLines on 2026-09-09.
// Listed rather than tolerated by a raised limit, so a NEW long file fails
// while these shrink under R1. Removing a name is always allowed.
var overLongFiles = map[string]bool{
	"internal/catalog/derive.go":     true, // 1361
	"internal/catalog/extract.go":    true, // 908
	"internal/catalog/index_test.go": true, // 1077
	"internal/catalog/scan.go":       true, // 1055
	"internal/lsp/hover_test.go":     true, // 1133
	"internal/session/inspect.go":    true, // 1565
	"internal/session/query.go":      true, // 985
}

func TestRepoShape(t *testing.T) {
	var files []string
	long := map[string]int{}
	err := filepath.WalkDir(".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") {
			return nil
		}
		rel := filepath.ToSlash(p)
		files = append(files, rel)
		raw, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		if n := bytes.Count(raw, []byte("\n")); n > maxFileLines {
			long[rel] = n
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(files) > maxGoFiles {
		t.Errorf("%d .go files under services/, ceiling %d. R1 takes this to "+
			"~80; adding to it goes the wrong way. Merge into an existing file "+
			"rather than raising the number.", len(files), maxGoFiles)
	}
	for rel, n := range long {
		if !overLongFiles[rel] {
			t.Errorf("%s is %d lines, over %d, and is not one of the eight "+
				"already recorded. Split it or keep it under the limit.",
				rel, n, maxFileLines)
		}
	}
	for rel := range overLongFiles {
		if _, still := long[rel]; !still {
			t.Errorf("%s is no longer over %d — delete it from overLongFiles so "+
				"the ratchet tightens.", rel, maxFileLines)
		}
	}
}
