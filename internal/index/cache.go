package index

import "time"

// CachePath returns the path of an install's cache file. The name is built from everything the index depends on:
// the game, the install's version, the dumps' time and the app's version, so a change to any of them means a
// different file and a fresh scan.
func CachePath(cacheDir, gameID, version string, dumpsAt time.Time, appVersion string) string {
	panic("stub")
}

// LoadIndex reads an install's index from its cache file, or returns false if there is none.
func LoadIndex(path string) (*Index, bool) { panic("stub") }

// SaveIndex writes an install's index to its cache file.
func SaveIndex(path string, index *Index) error { panic("stub") }
