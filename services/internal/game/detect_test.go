// detect_test.go covers version reading, user-data-dir discovery (including
// Steam library dedup), and install-staleness detection.

package game

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func writeLauncher(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "launcher"), 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "launcher", "launcher-settings.json")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestReadGameVersion(t *testing.T) {
	if got := ReadGameVersion(writeLauncher(t,
		`{ "rawVersion": "1.12.5", "version": "Scythe v1.12.5" }`)); got != "1.12.5" {
		t.Fatalf("version = %q want 1.12.5", got)
	}
	if got := ReadGameVersion(t.TempDir()); got != "" {
		t.Fatalf("missing file version = %q want empty", got)
	}
	if got := ReadGameVersion(writeLauncher(t,
		`{ "version": "1.13.11 (Chamomile)" }`)); got != "1.13.11" {
		t.Fatalf("codename strip = %q want 1.13.11", got)
	}
}

func TestUserDataDir(t *testing.T) {
	t.Run("game_data_path", func(t *testing.T) {
		ud := t.TempDir()
		raw, err := json.Marshal(map[string]string{"gameDataPath": ud})
		if err != nil {
			t.Fatal(err)
		}
		install := writeLauncher(t, string(raw))
		got := UserDataDir("ck3", install)
		if got != filepath.Clean(ud) {
			t.Fatalf("UserDataDir = %q want %q", got, ud)
		}
	})

	t.Run("xdg_beats_missing_documents", func(t *testing.T) {
		if runtime.GOOS != "linux" {
			t.Skip("XDG is Linux")
		}
		home := t.TempDir()
		t.Setenv("HOME", home)
		xdgHome := filepath.Join(home, "xdg")
		t.Setenv("XDG_DATA_HOME", xdgHome)
		want := filepath.Join(xdgHome, "Paradox Interactive", "Crusader Kings III")
		if err := os.MkdirAll(want, 0o755); err != nil {
			t.Fatal(err)
		}
		got := UserDataDir("ck3", "")
		if got != want {
			t.Fatalf("UserDataDir = %q want %q", got, want)
		}
	})

	t.Run("proton_when_xdg_absent", func(t *testing.T) {
		if runtime.GOOS != "linux" {
			t.Skip("Proton prefixes are Linux")
		}
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("XDG_DATA_HOME", "")
		want := filepath.Join(
			home, ".local", "share", "Steam", "steamapps", "compatdata", "1158310",
			"pfx", "drive_c", "users", "steamuser",
			"Documents", "Paradox Interactive", "Crusader Kings III",
		)
		if err := os.MkdirAll(want, 0o755); err != nil {
			t.Fatal(err)
		}
		got := UserDataDir("ck3", "")
		if got != want {
			t.Fatalf("UserDataDir = %q want %q", got, want)
		}
	})

	// steamLibraries resolves symlinks BEFORE dedup: ~/.steam/steam is usually a link to ~/.local/share/Steam.
	t.Run("symlinked_root_dedup", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("the ~/.steam/steam convention is Linux/macOS")
		}
		home := t.TempDir()
		t.Setenv("HOME", home)
		real := filepath.Join(home, ".local", "share", "Steam")
		if err := os.MkdirAll(real, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(home, ".steam"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(real, filepath.Join(home, ".steam", "steam")); err != nil {
			t.Skipf("cannot symlink here: %v", err)
		}
		got := steamLibraries()
		n := 0
		for _, p := range got {
			if p == real {
				n++
			}
		}
		if n != 1 {
			t.Fatalf("steamLibraries() = %v, want %q exactly once", got, real)
		}
	})
}

// InstallUpdatedAt decides whether generated script_docs still describe the
// installed build, so it must answer for every game. The obvious source,
// launcher/launcher-settings.json, does not exist on EU5 and was 11 days stale
// on CK3; the binaries are what actually track the update.
func TestInstallUpdatedAt(t *testing.T) {
	if got := InstallUpdatedAt(""); !got.IsZero() {
		t.Errorf("empty path = %v, want zero", got)
	}
	if got := InstallUpdatedAt(filepath.Join(t.TempDir(), "nope")); !got.IsZero() {
		t.Errorf("missing install = %v, want zero", got)
	}

	root := t.TempDir()
	bin := filepath.Join(root, "binaries")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	// A launcher file far in the future must not win: the binaries decide.
	launcher := filepath.Join(root, "launcher")
	if err := os.MkdirAll(launcher, 0o755); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(72 * time.Hour)
	settings := filepath.Join(launcher, "launcher-settings.json")
	if err := os.WriteFile(settings, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(settings, future, future); err != nil {
		t.Fatal(err)
	}

	exe := filepath.Join(bin, "game.exe")
	if err := os.WriteFile(exe, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(exe, old, old); err != nil {
		t.Fatal(err)
	}
	got := InstallUpdatedAt(root)
	if got.Sub(old).Abs() > time.Minute {
		t.Fatalf("InstallUpdatedAt = %v, want the binary's %v", got, old)
	}

	// A downgrade rewrites the binaries, so the timestamp moves forward even
	// though the version went backwards. That is the behaviour we want.
	now := time.Now()
	if err := os.Chtimes(exe, now, now); err != nil {
		t.Fatal(err)
	}
	if after := InstallUpdatedAt(root); !after.After(got) {
		t.Errorf("rewriting the binary did not move the timestamp: %v then %v", got, after)
	}

	// No binaries directory: fall back to the install root's own files.
	flat := t.TempDir()
	if err := os.WriteFile(filepath.Join(flat, "game.exe"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if InstallUpdatedAt(flat).IsZero() {
		t.Error("no binaries/ dir should fall back to the install root")
	}
}
