//go:build windows

// persist_windows_test.go reproduces the Windows-only condition that made every
// rescan write a 99 MB .tmp and change nothing.

package catalog

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// openShareDelete opens path the way an antivirus or the search indexer does:
// sharing read, write AND delete. Go's own os.Open omits FILE_SHARE_DELETE, so
// a handle it returns blocks both replace and unlink and cannot model this.
func openShareDelete(t *testing.T, path string) syscall.Handle {
	t.Helper()
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	h, err := syscall.CreateFile(p, syscall.GENERIC_READ,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatalf("CreateFile: %v", err)
	}
	t.Cleanup(func() { _ = syscall.CloseHandle(h) })
	return h
}

// A rescan must land even while something holds the old cache open.
//
// MOVEFILE_REPLACE_EXISTING — which Go's Rename passes — returns
// ERROR_ACCESS_DENIED for any open destination, but unlinking the name succeeds
// when the holder shared delete. That asymmetry is the whole bug: dropping the
// pre-rename Remove turned every rescan into minutes of work that produced a
// .tmp beside a stale cache the engine then refused, leaving hover with no
// vanilla model and the install list still showing the old scan date.
func TestSaveJSONReplacesACacheAnAntivirusHasOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.json")
	type doc struct {
		FormatVersion int    `json:"formatVersion"`
		Mark          string `json:"mark"`
	}
	if err := saveJSON(path, doc{1, "old"}); err != nil {
		t.Fatal(err)
	}
	openShareDelete(t, path)

	// Confirm the premise rather than assume it: a plain replace must fail here,
	// or this test is passing for the wrong reason.
	tmp := path + ".probe"
	if err := os.WriteFile(tmp, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, path); err == nil {
		_ = os.Remove(tmp)
		t.Skip("this Windows build replaces an open file; the bug cannot occur here")
	}
	_ = os.Remove(tmp)

	if err := saveJSON(path, doc{1, "new"}); err != nil {
		t.Fatalf("rescan could not land: %v", err)
	}
	got, err := loadJSON[doc](path, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got.Mark != "new" {
		t.Errorf("mark = %q, want new", got.Mark)
	}
	if _, err := os.Stat(path + ".tmp"); err == nil {
		t.Error("left a .tmp behind: the rename never landed")
	}
}
