// paths.go is how paths are compared, joined and taken back to a root, and the name of a file or folder PMT makes from
// a user's text.

package inventory

import (
	"path/filepath"
	"runtime"
	"strings"
	"unicode"
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

// Slug is a name fit for a file or folder, made from a user's text: lower case, its letters and digits kept
// whatever their script, and its dots (1.14.1 stays 1.14.1); every run of other characters, which a file name cannot
// hold or should not, one "_"; the ends trimmed of dots (a trailing one is illegal on Windows, a leading one hides the
// file) and of "_". A text that leaves nothing gives "".
func Slug(text string) string {
	var builder strings.Builder
	wroteUnderscore := false
	for _, char := range strings.ToLower(strings.TrimSpace(text)) {
		if unicode.IsLetter(char) || unicode.IsDigit(char) || char == '.' {
			builder.WriteRune(char)
			wroteUnderscore = false
			continue
		}
		if !wroteUnderscore {
			builder.WriteByte('_')
			wroteUnderscore = true
		}
	}
	return strings.Trim(builder.String(), "._")
}
