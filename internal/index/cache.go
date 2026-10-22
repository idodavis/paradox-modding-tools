package index

import (
	"fmt"
	"path/filepath"
	"time"

	"paradox-modding-tools/services/app"
)

// CachePath returns the path of an install's cache file. The name is built from everything the index depends on:
// the game, the install's version, the dumps' time and the app's version, so a change to any of them means a
// different file and a fresh scan.
func CachePath(cacheDir, gameID, version string, dumpsAt time.Time, appVersion string) string {
	return filepath.Join(cacheDir, fmt.Sprintf("index-%s-%s-%d-%s.gob", gameID, version, dumpsAt.Unix(), appVersion))
}

// LoadIndex reads an install's index from its cache file, or returns false if there is none.
func LoadIndex(path string) (*Index, bool) {
	index, ok := app.LoadGob[*Index](path)
	if !ok {
		return nil, false
	}
	index.buildTables()
	index.buildPaths()
	index.buildPostings()
	return index, true
}

// SaveIndex writes an install's index to its cache file.
func SaveIndex(path string, index *Index) error { return app.SaveGob(path, index) }
