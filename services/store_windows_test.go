//go:build windows

package services

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// config.json must survive a save while something holds it open. A Windows
// replace-rename returns ERROR_ACCESS_DENIED for any open destination, so the
// bare rename left every workspace change unsaved whenever an antivirus or the
// search indexer had the file. F.1 refuses to write over a config it could not
// read; this is the other half — writing one it can read.
func TestStoreSavesOverAConfigAnAntivirusHasOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	s := newStore(path)
	if err := s.Mutate(func(c *Config) error {
		c.Workspaces = append(c.Workspaces, Workspace{ID: "one", GameID: "ck3"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}

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
	defer func() { _ = syscall.CloseHandle(h) }()

	// Confirm the premise, or this passes for the wrong reason.
	probe := path + ".probe"
	if err := os.WriteFile(probe, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(probe, path); err == nil {
		_ = os.Remove(probe)
		t.Skip("this Windows build replaces an open file; the bug cannot occur here")
	}
	_ = os.Remove(probe)

	if err := s.Mutate(func(c *Config) error {
		c.Workspaces = append(c.Workspaces, Workspace{ID: "two", GameID: "vic3"})
		return nil
	}); err != nil {
		t.Fatalf("second save could not land: %v", err)
	}
	if got := len(newStore(path).cfg.Workspaces); got != 2 {
		t.Errorf("reloaded %d workspaces, want 2", got)
	}
	if _, err := os.Stat(path + ".tmp"); err == nil {
		t.Error("left a .tmp behind: the rename never landed")
	}
}
