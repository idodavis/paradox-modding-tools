// paths.go turns paths into keys and relative paths and back, and turns a user's text into a file name.

package files

import (
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// ToKey returns the key a path is compared by: cleaned, lower-cased, with forward slashes.
func ToKey(path string) string {
	return strings.ToLower(toSlash(path))
}

// ToRel returns the path relative to the given root, with forward slashes. It returns false if the path is not under
// the root.
func ToRel(root, path string) (string, bool) {
	key := ToKey(root)
	if !strings.HasPrefix(ToKey(path), key+"/") {
		return "", false
	}
	// The root is cut off by segment count, because lower-casing can change a text's length.
	segments := strings.Count(key, "/") + 1
	return strings.SplitN(toSlash(path), "/", segments+1)[segments], true
}

// Join joins a root and a relative path written with forward slashes into a path for this platform.
func Join(root, rel string) string {
	return filepath.Join(root, filepath.FromSlash(rel))
}

// IsUnder reports whether the path is under the given root, comparing their keys.
func IsUnder(root, path string) bool {
	_, ok := ToRel(root, path)
	return ok
}

// ToSlug turns a user's text into a file or folder name: lower case, ASCII only, words joined by `_`. It returns ""
// if the text has no letter or digit.
func ToSlug(text string) string {
	text, _, _ = transform.String(transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn))), text)
	var slug strings.Builder
	separate := false
	for _, r := range text {
		switch {
		case r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r)):
			if separate && slug.Len() > 0 {
				slug.WriteByte('_')
			}
			separate = false
			slug.WriteRune(unicode.ToLower(r))
		case unicode.IsLetter(r):
			// A letter with no plain form is dropped without breaking its word.
		default:
			separate = true
		}
	}
	return slug.String()
}

// toSlash returns a path cleaned and with forward slashes, from either platform's spelling.
func toSlash(path string) string {
	return filepath.ToSlash(filepath.Clean(strings.ReplaceAll(path, `\`, "/")))
}

// IsFolder reports whether a path is a folder that exists.
func IsFolder(path string) bool {
	stat, err := os.Stat(path)
	return err == nil && stat.IsDir()
}

// IsFile reports whether a path is an existing file. A folder is not one.
func IsFile(path string) bool {
	stat, err := os.Stat(path)
	return err == nil && !stat.IsDir()
}
