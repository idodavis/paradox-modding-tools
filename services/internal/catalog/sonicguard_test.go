//go:build go1.28

// sonicguard_test.go compiles only once the sonic fast path has been lost.
//
// sonic's own constraint is `(amd64 && go1.17 && !go1.28) || (arm64 && go1.20 &&
// !go1.28)`. The moment `go1.28` is satisfied, every sonic.Marshal and
// sonic.Get in persist.go silently becomes encoding/json — no code change, no
// build error, no log line. What breaks is not correctness but the measurement
// the storage design rests on: T-4 kept the cache layer on a 148 ms parse that
// only the fast path delivers.
//
// This file carries the SAME constraint as sonic's, inverted, so it fires
// exactly when sonic flips and never earlier — whether the language version is
// raised by go.mod's `go` directive or by anything else. CGO is not involved:
// it appears in neither of sonic's tags, so CGO_ENABLED=1 on Linux does not
// move it to the fallback. The Go version does.

package catalog

import "testing"

func TestSonicFastPathStillCompiled(t *testing.T) {
	t.Fatal("the Go language version reached 1.28, so github.com/bytedance/sonic " +
		"has fallen back to encoding/json. Re-measure the cache parse before " +
		"trusting T-4's storage verdict, then either pin the version back or " +
		"delete this guard deliberately.")
}
