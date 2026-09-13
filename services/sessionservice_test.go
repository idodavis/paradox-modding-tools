// sessionservice_test.go covers the background rescan queue. Scanning is the largest
// allocation in the app, so only one runs at a time -- but a scan that has to
// wait must WAIT, not be dropped.

package services

import (
	"context"
	"testing"
)

// TestAutoScanQueueDefersInsteadOfDropping is the bug: bumping
// CacheFormatVersion invalidates every cache on disk, three workspaces then all
// want a rescan, and only the first got one. The other two were left with no
// vanilla model, reporting cacheStale false, with every check that needs vanilla
// silently answering nothing.
func TestAutoScanQueueDefersInsteadOfDropping(t *testing.T) {
	s := &SessionService{}

	if !s.queueAutoScan("ck3") {
		t.Fatalf("first caller should start draining")
	}
	// Two more arrive while ck3 is walking.
	if s.queueAutoScan("vic3") {
		t.Errorf("second caller must not start a concurrent scan")
	}
	if s.queueAutoScan("eu5") {
		t.Errorf("third caller must not start a concurrent scan")
	}
	// The UI polls health, so the same ids arrive again and must not stack.
	s.queueAutoScan("vic3")
	s.queueAutoScan("ck3") // already running
	if got := len(s.autoQueue); got != 2 {
		t.Fatalf("queue = %v, want exactly vic3 and eu5", s.autoQueue)
	}

	if got := s.nextAutoScan(); got != "vic3" {
		t.Errorf("next = %q, want vic3", got)
	}
	if got := s.nextAutoScan(); got != "eu5" {
		t.Errorf("next = %q, want eu5", got)
	}
	if got := s.nextAutoScan(); got != "" {
		t.Errorf("next = %q, want empty once drained", got)
	}
}

// A manual rescan holds scanCancel; an automatic one must queue behind it
// rather than cancel it out from under the user.
func TestAutoScanQueuesBehindManualScan(t *testing.T) {
	s := &SessionService{}
	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.scanCancel = cancel

	if s.queueAutoScan("vic3") {
		t.Fatalf("must not start while a manual scan holds the slot")
	}
	if len(s.autoQueue) != 1 || s.autoQueue[0] != "vic3" {
		t.Fatalf("queue = %v, want [vic3]", s.autoQueue)
	}
}
