// paths.go compares and joins paths and ranks search matches.

package files

import (
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// ToKey returns the key a path is compared by: lower case, with slashes.
func ToKey(path string) string {
	return strings.ToLower(toSlash(path))
}

// ToRel returns a path below a root, or false if not under it.
func ToRel(root, path string) (string, bool) {
	key := ToKey(root)
	if !strings.HasPrefix(ToKey(path), key+"/") {
		return "", false
	}

	// Cut by segment count, because lower-casing can change a text's length.
	segments := strings.Count(key, "/") + 1

	return strings.SplitN(toSlash(path), "/", segments+1)[segments], true
}

// Join joins a root and a relative path for this platform.
func Join(root, rel string) string {
	return filepath.Join(root, filepath.FromSlash(rel))
}

// IsUnder reports whether a path is under a root.
func IsUnder(root, path string) bool {
	_, ok := ToRel(root, path)

	return ok
}

// ToSlug turns a text into lower-case ASCII words joined by `_`. It returns
// "" if the text has no letter or digit.
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
			// A letter with no plain form is dropped, not a break.

		default:
			separate = true
		}
	}

	return slug.String()
}

// ClassifyMatch ranks a lower-case path against a lower-case query: 0 if the
// last element is the query, 1 if it starts with it, 2 if the path holds it,
// 3 if it holds its letters in order, -1 for none.
func ClassifyMatch(path, query string) int {
	last := path[strings.LastIndexByte(path, '/')+1:]

	switch {
	case last == query:
		return 0
	case strings.HasPrefix(last, query):
		return 1
	case strings.Contains(path, query):
		return 2
	}

	matched := 0
	for _, character := range path {
		if wanted, size := utf8.DecodeRuneInString(query[matched:]); matched < len(query) && character == wanted {
			matched += size
		}
	}

	if matched == len(query) {
		return 3
	}

	return -1
}

func toSlash(path string) string {
	return filepath.ToSlash(filepath.Clean(strings.ReplaceAll(path, `\`, "/")))
}

// IsFolder reports whether a path is a folder.
func IsFolder(path string) bool {
	stat, err := os.Stat(path)

	return err == nil && stat.IsDir()
}

// IsFile reports whether a path is a file.
func IsFile(path string) bool {
	stat, err := os.Stat(path)

	return err == nil && !stat.IsDir()
}
