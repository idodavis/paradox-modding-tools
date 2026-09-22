// sessionservice_test.go covers a session built from the fixture install (scanned once, then loaded from its cache), the health strip, what makes a cache stale, and the scan queue: one at a time, a manual scan waited for, an automatic one queued once and never dropped.
package services

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"paradox-modding-tools/services/internal/lang/index"
)

// fixtureService is a session service over a store holding the ck3 fixture install and one workspace with one mod, its cache folder sent to a temporary directory.
func fixtureService(t *testing.T) (*SessionService, string) {
	t.Helper()
	t.Setenv("APPDATA", t.TempDir())
	install, _ := filepath.Abs(filepath.Join("internal", "lang", "testdata", "ck3"))
	mod := t.TempDir()
	if err := os.CopyFS(mod, os.DirFS(filepath.Join("internal", "lang", "testdata", "mods", "ck3"))); err != nil {
		t.Fatal(err)
	}
	store := newStore(filepath.Join(t.TempDir(), "config.json"))
	if err := store.Mutate(func(c *Config) error {
		c.Installs = append(c.Installs, GameInstall{ID: "i1", GameID: "ck3", Name: "CK3", Path: install, Version: "test"})
		c.Workspaces = append(c.Workspaces, Workspace{ID: "ws", GameID: "ck3", Name: "W", InstallID: "i1", Mods: []WorkspaceMod{{ID: "m", Name: "M", Path: mod}}})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	s := &SessionService{Store: store}
	s.Scan = func(ctx context.Context, req index.Request) (*index.Install, error) {
		req.Docs = []string{filepath.Join(req.Dir, "docs")}
		return index.Scan(ctx, req)
	}
	return s, install
}

func TestEnsureSession(t *testing.T) {
	s, _ := fixtureService(t)
	before, err := s.GetLanguageHealth("ws")
	if err != nil || before.Ready || !before.CacheStale || !before.InstallOk || before.GameID != "ck3" {
		t.Fatalf("before the first build: no session, no cache: %+v (%v)", before, err)
	}

	h, err := s.EnsureSession("ws")
	if err != nil || !h.Ready || h.Defs == 0 || h.Kinds == 0 || h.CacheStale || h.ScannedAt == "" || h.Scanning {
		t.Fatalf("a session built from a scan, its cache written: %+v (%v)", h, err)
	}
	live := s.pool().Get("ws")
	if live == nil || len(live.Mods) != 1 || !live.Workspace.Def("traits", "bold") {
		t.Error("the mod is indexed over the install")
	}

	// dropped and asked again, the session comes from the cache, not a scan
	scans := int32(0)
	s.Scan = func(ctx context.Context, req index.Request) (*index.Install, error) {
		atomic.AddInt32(&scans, 1)
		return nil, os.ErrNotExist
	}
	s.pool().DropAll()
	if h, err := s.EnsureSession("ws"); err != nil || !h.Ready || scans != 0 {
		t.Errorf("the cache serves the second build: %+v (%v), %d scans", h, err, scans)
	}
	if _, err := s.EnsureSession("nowhere"); err == nil {
		t.Error("a workspace the store lacks")
	}
}

func TestPredatesInstall(t *testing.T) {
	install := t.TempDir()
	bin := filepath.Join(install, "binaries")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(bin, "game.exe")
	if err := os.WriteFile(exe, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	scanned := time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339)
	if err := os.Chtimes(exe, time.Now(), time.Now()); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		what, scannedAt, install string
		stale                    bool
	}{
		{"a binary newer than the scan: the game was patched since", scanned, install, true},
		{"a scan after the last patch is current", time.Now().UTC().Add(2 * time.Hour).Format(time.RFC3339), install, false},
		{"no scan timestamp is not stale: guessing would rescan on every poll", "", install, false},
		{"an unreadable install is not stale", scanned, filepath.Join(install, "gone"), false},
	} {
		if got := predatesInstall(tt.scannedAt, tt.install); got != tt.stale {
			t.Errorf("%s: %v", tt.what, got)
		}
	}
}

func TestScanQueue(t *testing.T) {
	s, _ := fixtureService(t)
	var mu sync.Mutex
	var order []string
	release := make(chan struct{})
	s.Scan = func(ctx context.Context, req index.Request) (*index.Install, error) {
		<-release
		mu.Lock()
		order = append(order, req.Version)
		mu.Unlock()
		return nil, os.ErrNotExist
	}
	if err := s.Store.Mutate(func(c *Config) error {
		c.Installs = append(c.Installs, GameInstall{ID: "i2", GameID: "ck3", Path: t.TempDir(), Version: "two"}, GameInstall{ID: "i3", GameID: "ck3", Path: t.TempDir(), Version: "three"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// the first automatic scan runs; the second and third queue behind it, each once; the manual one waits for the slot
	s.QueueScan("i1")
	s.QueueScan("i2")
	s.QueueScan("i3")
	s.QueueScan("i2")
	queued, running := s.Queued()
	if !running || len(queued) != 3 {
		t.Fatalf("one runs, three are queued once each: %v %v", queued, running)
	}
	done := make(chan error, 1)
	go func() { done <- s.RebuildInstallSemantics("i1") }()
	time.Sleep(20 * time.Millisecond)
	select {
	case <-done:
		t.Fatal("a manual scan started while another ran")
	default:
	}
	for range 4 {
		release <- struct{}{}
	}
	<-done
	deadline := time.Now().Add(2 * time.Second)
	for {
		queued, running := s.Queued()
		if len(queued) == 0 && !running {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the queue never drained: %v %v", queued, running)
		}
		time.Sleep(5 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(order) != 4 || order[0] != "test" || order[1] != "two" || order[2] != "three" || order[3] != "test" {
		t.Errorf("scans run one at a time, the queue in order, the manual one when the slot is free: %v", order)
	}
}
