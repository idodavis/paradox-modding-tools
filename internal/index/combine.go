package index

import (
	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
)

// A Winner is the definition the game keeps when several share a name, and the reason it wins.
type Winner struct {
	Site   Site   `json:"site"`
	Reason string `json:"reason"` // order, mode, priority, merge, replace
	Merged []Site `json:"merged,omitempty"`
}

// FindWinner decides which of several definitions of one name the game keeps, by the database's precedence: the
// first or the last loaded, the highest override priority, the entry modes in the game's order, or all of them
// merged.
func (index *Index) FindWinner(sites []Site, precedence game.Precedence) Winner { panic("stub") }

// FindLoaded returns the file the game actually reads for a relative path: the last one in load order, except
// that a replace_path drops the earlier sources' files in that folder. It also returns the files the loaded one
// hides, and false if no file has that path.
func (index *Index) FindLoaded(rel string) (loaded files.File, hidden []files.File, ok bool) {
	panic("stub")
}

// IsLoaded reports whether the game reads a file at all.
func (index *Index) IsLoaded(file int32) bool { panic("stub") }

// compareLoadOrder orders two relative paths the way the game loads files: a folder's files come before its
// subfolders', and names are compared byte by byte, case included.
func compareLoadOrder(a, b string) int { panic("stub") }
