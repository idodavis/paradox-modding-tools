// diagnosticsbench_test.go measures what one keystroke costs, over a real CK3
// event file rather than a fixture — the shape of the file is most of the cost.
//
// It exists because the design comment on IdeService.Diagnose was right that the
// checks must stay ignorant of configuration, and the price of achieving it by
// running them all and filtering afterwards had never been measured. It was
// 593 µs of 873 for unknownTokenDiags alone, a check that is default-off, so two
// thirds of every keystroke was discarded. Gating the off checks took the
// default-settings path from 1,499 µs to 283 µs on the same run.
//
// Skips without PMT_TEST_INSTALL_CK3 / PMT_TEST_DOCS_CK3. Run with:
//
//	go test ./internal/lsp/ -run '^$' -bench BenchmarkDiagnose

package lsp

import (
	"os"
	"path/filepath"
	"testing"

	"paradox-modding-tools/services/internal/catalog"
	"paradox-modding-tools/services/internal/session"
)

func benchSession(b *testing.B) (*session.Session, string) {
	b.Helper()
	install := os.Getenv("PMT_TEST_INSTALL_CK3")
	docs := os.Getenv("PMT_TEST_DOCS_CK3")
	if install == "" || docs == "" {
		b.Skip("env")
	}
	schema := catalog.ReadSchema([]string{docs})
	// A real, large CK3 event file, copied into a mod so diagnostics run on it.
	srcPath := filepath.Join(install, "game", "events", "global_religion_events.txt")
	raw, err := os.ReadFile(srcPath)
	if err != nil {
		b.Skip(err)
	}
	mod := b.TempDir()
	if err := os.WriteFile(filepath.Join(mod, "descriptor.mod"),
		[]byte("name = \"t\"\n"), 0o644); err != nil {
		b.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(mod, "events"), 0o755); err != nil {
		b.Fatal(err)
	}
	p := filepath.Join(mod, "events", "big.txt")
	if err := os.WriteFile(p, raw, 0o644); err != nil {
		b.Fatal(err)
	}
	cache := &catalog.VanillaCache{GameID: "ck3", InstallPath: install, Schema: schema}
	catalog.PrepareCache(cache)
	s := session.NewWithLoc("ws", "ck3", "english", cache, nil,
		[]catalog.ModInput{{Origin: "mod", Root: mod, Order: 0}})
	s.DidOpen(p, string(raw))
	b.Logf("file is %d bytes", len(raw))
	return s, p
}

func BenchmarkDiagnoseAll(b *testing.B) {
	s, p := benchSession(b)
	b.ResetTimer()
	for b.Loop() {
		_ = Diagnose(s, p)
	}
}

func BenchmarkDiagnoseUnknownTokenOnly(b *testing.B) {
	s, p := benchSession(b)
	f := diagFile{s: s, path: p, kind: s.KindFor(p), src: s.FileText(p)}
	f.refs = s.RefsInFile(p)
	f.res = s.Parsed(p)
	f.li = f.res.Lines()
	b.ResetTimer()
	for b.Loop() {
		_ = f.unknownTokenDiags()
	}
}

func BenchmarkDiagnoseDeclaredValueOnly(b *testing.B) {
	s, p := benchSession(b)
	f := diagFile{s: s, path: p, kind: s.KindFor(p), src: s.FileText(p)}
	f.refs = s.RefsInFile(p)
	f.res = s.Parsed(p)
	f.li = f.res.Lines()
	b.ResetTimer()
	for b.Loop() {
		_ = f.declaredValueDiags()
	}
}

func BenchmarkDiagnoseDefaultsEnabled(b *testing.B) {
	s, p := benchSession(b)
	on := EnabledCodes(nil) // what a user with no settings actually gets
	b.ResetTimer()
	for b.Loop() {
		_ = DiagnoseEnabled(s, p, on)
	}
}
