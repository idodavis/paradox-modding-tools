// store_test.go covers JSON config round-trip and version-mismatch wipe.

package services

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	s := newStore(path)
	if err := s.Mutate(func(c *Config) error {
		c.Installs = append(c.Installs, GameInstall{ID: "i1", Name: "CK3"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	s2 := newStore(path)
	var n int
	s2.Read(func(c *Config) { n = len(c.Installs) })
	if n != 1 {
		t.Fatalf("got %d installs", n)
	}

	stale := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(stale, []byte(`{"formatVersion":99,"installs":[{"id":"x"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	var wiped int
	newStore(stale).Read(func(c *Config) { wiped = len(c.Installs) })
	if wiped != 0 {
		t.Fatalf("want empty, got %d", wiped)
	}
}

// TestStoreLoadFailureDoesNotWipe pins the three-way split in newStore.
//
// A config that cannot be read is NOT a first run. Before this, every load
// error fell through to emptyConfig(), so an antivirus hold or a permission
// blip presented as a fresh install and the next Mutate wrote that empty config
// over the real one — every install, workspace and mod, with no backup.
//
// A version mismatch is the opposite case and must still wipe: the bump is
// deliberate and the config is small and re-enterable.
func TestStoreLoadFailureDoesNotWipe(t *testing.T) {
	t.Run("unreadable config refuses to write", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.json")
		// Valid version, corrupt body: readable, parseable header, and the
		// unmarshal fails. Stands in for any non-reset read failure.
		body := fmt.Sprintf(`{"formatVersion":%d,"installs":"not-an-array"}`, configFormatVersion)
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		s := newStore(path)
		if s.loadErr == nil {
			t.Fatal("want loadErr set for a corrupt config")
		}
		if err := s.Mutate(func(c *Config) error {
			c.Installs = append(c.Installs, GameInstall{ID: "new"})
			return nil
		}); err == nil {
			t.Fatal("Mutate must refuse while the config is unreadable")
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(raw) != body {
			t.Fatalf("config was overwritten:\n got %s\nwant %s", raw, body)
		}
	})

	t.Run("version mismatch still wipes and stays writable", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.json")
		if err := os.WriteFile(path, []byte(`{"formatVersion":99,"installs":[{"id":"x"}]}`), 0o644); err != nil {
			t.Fatal(err)
		}
		s := newStore(path)
		if s.loadErr != nil {
			t.Fatalf("a version bump is deliberate, not a failure: %v", s.loadErr)
		}
		if err := s.Mutate(func(c *Config) error { return nil }); err != nil {
			t.Fatalf("Mutate after a version reset: %v", err)
		}
	})

	t.Run("missing config is a first run", func(t *testing.T) {
		s := newStore(filepath.Join(t.TempDir(), "config.json"))
		if s.loadErr != nil {
			t.Fatalf("absent config is not a failure: %v", s.loadErr)
		}
		if err := s.Mutate(func(c *Config) error { return nil }); err != nil {
			t.Fatalf("Mutate on first run: %v", err)
		}
	})
}

func TestConvertMarkdown(t *testing.T) {
	r := &ReleaseService{}
	got := r.Convert("# Hi\n", "bbcode")
	if !strings.Contains(got.Text, "[h1]Hi[/h1]") {
		t.Fatalf("%q", got.Text)
	}
}

func TestSteamPublishError(t *testing.T) {
	got := steamPublishError("SubmitItemUpdate EResult 9")
	if got == "" || !strings.Contains(got, "EResult 9") {
		t.Fatalf("eresult 9: %q", got)
	}
	msg := steamPublishError("SubmitItemUpdate EResult 25")
	if msg == "" || !strings.Contains(msg, "1 MB") {
		t.Fatalf("eresult 25: %q", msg)
	}
	if steamPublishError("SubmitItemUpdate EResult 2") != "" {
		t.Fatal("eresult 2 should not map")
	}
}

func TestSteamEResultCode(t *testing.T) {
	code, ok := steamEResultCode("CreateItem EResult 25")
	if !ok || code != 25 {
		t.Fatalf("got %d %v", code, ok)
	}
	if _, ok := steamEResultCode("no code here"); ok {
		t.Fatal("expected miss")
	}
}

func TestBumpVersion(t *testing.T) {
	r := &ReleaseService{}
	if got := r.BumpVersion("1.2.0", "patch"); got != "1.2.1" {
		t.Fatalf("%s", got)
	}
	if got := r.BumpVersion("1.2.3", "minor"); got != "1.3.0" {
		t.Fatalf("%s", got)
	}
}

// covers PMT-owned path checks for ResetData wipes.
func TestOwnedByPMT(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	inside := filepath.Join(root, "workspaces", "ws1")
	if !ownedByPMT(root, inside) {
		t.Fatal("want subdirectory owned")
	}
	if ownedByPMT(root, root) {
		t.Fatal("root itself must not be owned")
	}
	if ownedByPMT(root, "") {
		t.Fatal("empty path")
	}
	outside := t.TempDir()
	if ownedByPMT(root, outside) {
		t.Fatal("foreign temp dir must not be owned")
	}
	escape := filepath.Join(root, "..", filepath.Base(outside))
	if ownedByPMT(root, escape) {
		t.Fatal("path escaping root must not be owned")
	}
}

func TestRemoveOwnedDirs(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	owned := filepath.Join(root, "workspaces", "ws1")
	if err := os.MkdirAll(owned, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(owned, "x.txt")
	if err := os.WriteFile(marker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	keep := filepath.Join(outside, "keep.txt")
	if err := os.WriteFile(keep, []byte("k"), 0o644); err != nil {
		t.Fatal(err)
	}
	removeOwnedDirs(root, []string{owned, outside, "", owned})
	if _, err := os.Stat(owned); !os.IsNotExist(err) {
		t.Fatalf("owned dir still present: %v", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("outside file was deleted: %v", err)
	}
}
