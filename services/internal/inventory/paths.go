// paths.go is how paths are compared, joined, and taken back to a root.

package inventory

import (
	"path/filepath"
	"runtime"
	"strings"
)

// PathKey is the key a file on this machine is known by: its path cleaned, and on Windows, where the file system
// ignores case, lower-cased; elsewhere two paths that differ only by case are two files.
func PathKey(path string) string {
	path = filepath.Clean(path)
	if runtime.GOOS == "windows" {
		return strings.ToLower(path)
	}
	return path
}

// Join is the path of rel, written with slashes, under root.
func Join(root, rel string) string {
	return filepath.Join(root, filepath.FromSlash(rel))
}

// RelOf is path's rel under root, written with slashes; false for a path that is not inside root.
func RelOf(root, path string) (string, bool) {
	rel, err := filepath.Rel(root, path)
	return filepath.ToSlash(rel), err == nil && filepath.IsLocal(rel)
}
