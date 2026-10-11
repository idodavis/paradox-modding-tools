// search_glob.go is a folder's include and exclude globs as the search reads them.

package pages

import (
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// TextSearchFolder is one workspace folder plus optional glob filters.
type TextSearchFolder struct {
	Path     string   `json:"path"`
	Origin   string   `json:"origin,omitempty"` // told back on its hits
	Includes []string `json:"includes,omitempty"`
	Excludes []string `json:"excludes,omitempty"`
}

// globSet is a folder's filters as patterns for doublestar: the paths to keep and the paths to drop.
type globSet struct {
	includes []string
	excludes []string
}

// folderGlobs is a folder's filters as rg reads them: a glob with no ** matches at any depth, and a leading "!"
// makes an include an exclude.
func folderGlobs(folder TextSearchFolder) globSet {
	var globs globSet
	add := func(glob string, exclude bool) {
		glob = strings.TrimSpace(glob)
		if rest, negated := strings.CutPrefix(glob, "!"); negated {
			glob, exclude = strings.TrimSpace(rest), true
		}
		if glob == "" {
			return
		}
		if !strings.Contains(glob, "**") {
			glob = "**/" + strings.TrimPrefix(glob, "/")
		}
		if exclude {
			globs.excludes = append(globs.excludes, glob)
		} else {
			globs.includes = append(globs.includes, glob)
		}
	}
	for _, include := range folder.Includes {
		add(include, false)
	}
	for _, exclude := range folder.Excludes {
		add(exclude, true)
	}
	return globs
}

// pathMatchesGlobs is whether a path from the folder's root is kept: no exclude matches it, and with includes, one
// matches it.
func pathMatchesGlobs(rel string, globs globSet) bool {
	rel = filepath.ToSlash(rel)
	for _, exclude := range globs.excludes {
		if matches, _ := doublestar.Match(exclude, rel); matches {
			return false
		}
	}
	if len(globs.includes) == 0 {
		return true
	}
	for _, include := range globs.includes {
		if matches, _ := doublestar.Match(include, rel); matches {
			return true
		}
	}
	return false
}

// folderPath is a folder's path as the system writes it; "" for a folder with none.
func folderPath(folder TextSearchFolder) string {
	path := filepath.Clean(filepath.FromSlash(folder.Path))
	if path == "" || path == "." {
		return ""
	}
	return path
}
