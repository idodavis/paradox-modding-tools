// filenames.go is the one rule for the name of a file or folder PMT makes from a user's text.

package inventory

import (
	"strings"
	"unicode"
)

// FileName is a name fit for a file or folder, made from a user's text: lower case, its letters and digits kept
// whatever their script, and its dots (1.14.1 stays 1.14.1); every run of other characters, which a file name cannot
// hold or should not, one "_"; the ends trimmed of dots (a trailing one is illegal on Windows, a leading one hides the
// file) and of "_". A text that leaves nothing gives "".
func FileName(text string) string {
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
