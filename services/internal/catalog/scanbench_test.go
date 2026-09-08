// scanbench_test.go is the scan performance baseline. Opt-in (-bench), skipped
// without an install. Scans are the slowest thing PMT does and the number here
// is what any performance change has to move.
//
//	go test ./internal/catalog/ -run XXX -bench BenchmarkScanCK3 -benchtime 3x

package catalog

import (
	"context"
	"os"
	"testing"
)

func BenchmarkScanCK3(b *testing.B) {
	root := `C:\Program Files (x86)\Steam\steamapps\common\Crusader Kings III`
	if _, err := os.Stat(root); err != nil {
		b.Skip("no install")
	}
	b.ReportAllocs()
	for b.Loop() {
		id := "bench"
		_, _, err := Scan(context.Background(), ScanRequest{
			InstallID: id, GameID: "ck3", InstallPath: root, Version: "b", LocLang: "english",
		})
		if err != nil {
			b.Fatal(err)
		}
		_ = DropVanillaFiles(id)
	}
}
