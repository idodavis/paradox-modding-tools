// persist.go loads and saves wiki-docs / wiki-patches sidecars.
package wiki

import (
	"path/filepath"
	"sync"

	"paradox-modding-tools/services/internal/lang/derived"
)

type sidecarKind string

const (
	kindGuides  sidecarKind = "wiki-docs"
	kindPatches sidecarKind = "wiki-patches"
)

var sidecarKinds = []sidecarKind{kindGuides, kindPatches}

var (
	cacheDirFn = derived.CacheDir
	memMu      sync.Mutex
	mem        = map[sidecarKind]map[string]*Sidecar{
		kindGuides:  {},
		kindPatches: {},
	}
)

func sidecarPath(k sidecarKind, gameID string) (string, error) {
	dir, err := cacheDirFn()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, string(k)+"-"+gameID+".json"), nil
}

func forget(gameID string) {
	memMu.Lock()
	defer memMu.Unlock()
	for _, k := range sidecarKinds {
		delete(mem[k], gameID)
	}
}

// ForgetAll drops every in-memory wiki sidecar. Disk files are not touched.
// Call after wiping derived.CacheDir so Needed/HasSidecar do not reuse RAM.
func ForgetAll() {
	memMu.Lock()
	defer memMu.Unlock()
	for _, k := range sidecarKinds {
		mem[k] = map[string]*Sidecar{}
	}
}

func remember(k sidecarKind, gameID string, f *Sidecar) {
	memMu.Lock()
	defer memMu.Unlock()
	mem[k][gameID] = f
}

func loadJSON(path string) (*Sidecar, error) {
	return derived.LoadJSON[Sidecar](path, FormatVersion)
}

func saveJSON(path string, f *Sidecar) error {
	f.FormatVersion = FormatVersion
	return derived.SaveJSON(path, f)
}

func loadMem(k sidecarKind, gameID string) (*Sidecar, error) {
	memMu.Lock()
	if f := mem[k][gameID]; f != nil {
		memMu.Unlock()
		return f, nil
	}
	memMu.Unlock()
	path, err := sidecarPath(k, gameID)
	if err != nil {
		return nil, err
	}
	f, err := loadJSON(path)
	if err != nil {
		return nil, err
	}
	remember(k, gameID, f)
	return f, nil
}

// LoadGuides reads wiki-docs-{gameId}.json. Format mismatch or missing → error.
func LoadGuides(gameID string) (*Sidecar, error) { return loadMem(kindGuides, gameID) }

// LoadPatches reads wiki-patches-{gameId}.json.
func LoadPatches(gameID string) (*Sidecar, error) { return loadMem(kindPatches, gameID) }

func save(k sidecarKind, f *Sidecar) error {
	path, err := sidecarPath(k, f.GameID)
	if err != nil {
		return err
	}
	if err := saveJSON(path, f); err != nil {
		return err
	}
	remember(k, f.GameID, f)
	return nil
}

func saveGuides(f *Sidecar) error  { return save(kindGuides, f) }
func savePatches(f *Sidecar) error { return save(kindPatches, f) }

// HasSidecar reports whether a loadable wiki-docs sidecar exists for gameID.
func HasSidecar(gameID string) bool {
	_, err := LoadGuides(gameID)
	return err == nil
}
