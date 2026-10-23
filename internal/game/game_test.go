package game

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"paradox-modding-tools/internal/jomini"
)

func writeLauncher(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "launcher"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "launcher", "launcher-settings.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestLoadGameVersion(t *testing.T) {
	if got := LoadGameVersion("ck3", writeLauncher(t,
		`{ "rawVersion": "1.12.5", "version": "Scythe v1.12.5" }`)); got != "1.12.5" {
		t.Fatalf("version = %q want 1.12.5", got)
	}
	// the user-data folder is an empty one of the test's, so this machine's own revision log is not read
	empty, err := json.Marshal(map[string]string{"gameDataPath": t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if got := LoadGameVersion("ck3", writeLauncher(t, string(empty))); got != "" {
		t.Fatalf("missing file version = %q want empty", got)
	}
	if got := LoadGameVersion("ck3", writeLauncher(t,
		`{ "version": "1.13.11 (Chamomile)" }`)); got != "1.13.11" {
		t.Fatalf("codename strip = %q want 1.13.11", got)
	}
}

// TestUserDataDirLauncherSetting holds the case every OS shares: the launcher's own setting names the user-data folder.
func TestUserDataDirLauncherSetting(t *testing.T) {
	t.Run("game_data_path", func(t *testing.T) {
		userData := t.TempDir()
		raw, err := json.Marshal(map[string]string{"gameDataPath": userData})
		if err != nil {
			t.Fatal(err)
		}
		install := writeLauncher(t, string(raw))
		got := FindUserDataDir("ck3", install)
		if got != filepath.Clean(userData) {
			t.Fatalf("FindUserDataDir = %q want %q", got, userData)
		}
	})
}

// TestUserDataDirLinux holds the cases that follow Linux's conventions: XDG and Proton.
func TestUserDataDirLinux(t *testing.T) {
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
		got := FindUserDataDir("ck3", "")
		if got != want {
			t.Fatalf("FindUserDataDir = %q want %q", got, want)
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
		got := FindUserDataDir("ck3", "")
		if got != want {
			t.Fatalf("FindUserDataDir = %q want %q", got, want)
		}
	})
}

// findSteamLibraries resolves links before dedup: ~/.steam/steam usually links ~/.local/share/Steam.
func TestSteamLibrariesLinkedRoots(t *testing.T) {
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
	got := findSteamLibraries()
	count := 0
	for _, library := range got {
		if library == real {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("findSteamLibraries() = %v, want %q exactly once", got, real)
	}
}

// ReadInstallUpdatedAt must answer for every game, so the binaries decide, not
// launcher/launcher-settings.json, which EU5 lacks and which lags the update on CK3.
func TestInstallUpdatedAt(t *testing.T) {
	if got := ReadInstallUpdatedAt(""); !got.IsZero() {
		t.Errorf("empty path = %v, want zero", got)
	}
	if got := ReadInstallUpdatedAt(filepath.Join(t.TempDir(), "nope")); !got.IsZero() {
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
	got := ReadInstallUpdatedAt(root)
	if got.Sub(old).Abs() > time.Minute {
		t.Fatalf("ReadInstallUpdatedAt = %v, want the binary's %v", got, old)
	}

	// A downgrade rewrites the binaries, so the timestamp moves forward even
	// though the version went backwards.
	now := time.Now()
	if err := os.Chtimes(exe, now, now); err != nil {
		t.Fatal(err)
	}
	if updated := ReadInstallUpdatedAt(root); !updated.After(got) {
		t.Errorf("rewriting the binary did not move the timestamp: %v then %v", got, updated)
	}

	// No binaries directory: fall back to the install root's own files.
	flat := t.TempDir()
	if err := os.WriteFile(filepath.Join(flat, "game.exe"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if ReadInstallUpdatedAt(flat).IsZero() {
		t.Error("no binaries/ dir should fall back to the install root")
	}
}

func TestColorOf(t *testing.T) {
	tree := jomini.Parse("a = rgb { 255 0 0 }\nb = rgb { 1 0.5 0 }\nc = hsv { 0.5 1 1 }\nd = hsv360 { 180 100 100 }\n" +
		"e = hex { 0xff8000ff }\nf = rgb { 255 0 }\ng = { 1 2 3 }\nh = foo { 1 2 3 }\n")
	wants := []string{"#ff0000", "#ff8000", "#00ffff", "#00ffff", "#ff8000", "", "#010203", ""}
	i := 0
	for statement := range tree.Children(0) {
		want := wants[i]
		i++
		key := tree.Text(tree.Statements[statement].Key)
		tag, parts, isColor := jomini.ColorParts(tree, statement)
		color, known := ParseColor(tag, parts)
		if got := color.ToHex(); !isColor || !known {
			got = ""
		} else if got != want {
			t.Errorf("%s: %q, want %q", key, got, want)
		} else {
			continue
		}
		if want != "" || isColor && known {
			t.Errorf("%s: no color, want %q", key, want)
		}
	}
}
