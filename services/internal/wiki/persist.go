// persist.go loads and saves wiki-docs / wiki-patches sidecars with sonic.
package wiki

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/bytedance/sonic"

	"paradox-modding-tools/services/internal/catalog"
)

var (
	cacheDirFn = catalog.CacheDir
	memMu      sync.Mutex
	memGuides  = map[string]*Sidecar{}
	memPatches = map[string]*Sidecar{}
)

func sidecarPath(kind, gameID string) (string, error) {
	dir, err := cacheDirFn()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, kind+"-"+gameID+".json"), nil
}

func guidesPath(gameID string) (string, error)  { return sidecarPath("wiki-docs", gameID) }
func patchesPath(gameID string) (string, error) { return sidecarPath("wiki-patches", gameID) }

func forget(gameID string) {
	memMu.Lock()
	defer memMu.Unlock()
	delete(memGuides, gameID)
	delete(memPatches, gameID)
}

// ForgetAll drops every in-memory wiki sidecar. Disk files are not touched.
// Call after wiping catalog.CacheDir so Needed/HasSidecar do not reuse RAM.
func ForgetAll() {
	memMu.Lock()
	defer memMu.Unlock()
	memGuides = map[string]*Sidecar{}
	memPatches = map[string]*Sidecar{}
}

func remember(mem map[string]*Sidecar, gameID string, f *Sidecar) {
	memMu.Lock()
	defer memMu.Unlock()
	mem[gameID] = f
}

func loadJSON(path string) (*Sidecar, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	n, err := sonic.Get(raw, "formatVersion")
	got, nerr := n.Int64()
	if err != nil || nerr != nil {
		return nil, fmt.Errorf("missing formatVersion")
	}
	if int(got) != FormatVersion {
		return nil, fmt.Errorf("format %d != %d", got, FormatVersion)
	}
	var dst Sidecar
	if err := sonic.Unmarshal(raw, &dst); err != nil {
		return nil, err
	}
	return &dst, nil
}

func saveJSON(path string, f *Sidecar) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f.FormatVersion = FormatVersion
	raw, err := sonic.Marshal(f)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	// Rename first; remove only if it fails. See services/store.go: a Windows
	// replace-rename over an open destination returns ERROR_ACCESS_DENIED,
	// while unlinking the name succeeds.
	if err := os.Rename(tmp, path); err == nil {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(tmp, path)
}

func loadMem(gameID string, mem map[string]*Sidecar, pathFn func(string) (string, error)) (*Sidecar, error) {
	memMu.Lock()
	if f := mem[gameID]; f != nil {
		memMu.Unlock()
		return f, nil
	}
	memMu.Unlock()
	path, err := pathFn(gameID)
	if err != nil {
		return nil, err
	}
	f, err := loadJSON(path)
	if err != nil {
		return nil, err
	}
	remember(mem, gameID, f)
	return f, nil
}

// LoadGuides reads wiki-docs-{gameId}.json. Format mismatch or missing → error.
func LoadGuides(gameID string) (*Sidecar, error) {
	return loadMem(gameID, memGuides, guidesPath)
}

// LoadPatches reads wiki-patches-{gameId}.json.
func LoadPatches(gameID string) (*Sidecar, error) {
	return loadMem(gameID, memPatches, patchesPath)
}

func saveGuides(f *Sidecar) error {
	path, err := guidesPath(f.GameID)
	if err != nil {
		return err
	}
	if err := saveJSON(path, f); err != nil {
		return err
	}
	remember(memGuides, f.GameID, f)
	return nil
}

func savePatches(f *Sidecar) error {
	path, err := patchesPath(f.GameID)
	if err != nil {
		return err
	}
	if err := saveJSON(path, f); err != nil {
		return err
	}
	remember(memPatches, f.GameID, f)
	return nil
}

func present(gameID string, mem map[string]*Sidecar, pathFn func(string) (string, error)) bool {
	memMu.Lock()
	if mem[gameID] != nil {
		memMu.Unlock()
		return true
	}
	memMu.Unlock()
	path, err := pathFn(gameID)
	if err != nil {
		return false
	}
	_, err = loadJSON(path)
	return err == nil
}

func hasGuides(gameID string) bool  { return present(gameID, memGuides, guidesPath) }
func hasPatches(gameID string) bool { return present(gameID, memPatches, patchesPath) }

// HasSidecar reports whether a loadable wiki-docs sidecar exists for gameID.
func HasSidecar(gameID string) bool { return hasGuides(gameID) }
