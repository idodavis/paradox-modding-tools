package index

import (
	"fmt"
	"path/filepath"
	"time"

	"paradox-modding-tools/services/app"
)

// CachePath returns the folder of an install's caches. It sits in a folder of the game's own, and its name is built
// from everything the caches depend on: the install's version, the dumps' time and the app's version. So a change
// to any of them means a different folder and a fresh scan, and the game's older caches are the folder's siblings.
func CachePath(cacheDir, gameID, version string, dumpsAt time.Time, appVersion string) string {
	return filepath.Join(cacheDir, gameID, fmt.Sprintf("%s-%d-%s", version, dumpsAt.Unix(), appVersion))
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
