package files

// ToKey returns the key a path is compared by: cleaned, lower-cased, with forward slashes.
func ToKey(path string) string { panic("stub") }

// ToRel returns a path relative to a root, with forward slashes. It returns false if the path is not under the
// root.
func ToRel(root, path string) (string, bool) { panic("stub") }

// Join joins a root and a relative path written with forward slashes into a path for this platform.
func Join(root, rel string) string { panic("stub") }

// IsUnder reports whether a path lies inside a root, comparing their keys.
func IsUnder(root, path string) bool { panic("stub") }

// ToSlug turns a user's text into a file or folder name: lower case, ASCII only, words joined by `_`.
func ToSlug(text string) string { panic("stub") }
