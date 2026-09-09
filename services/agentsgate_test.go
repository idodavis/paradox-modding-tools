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
