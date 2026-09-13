// encoding.go decodes raw file bytes to text for parsing: it strips a UTF-8 BOM
// and CR, and falls back to a latin1 decode for files that are not valid UTF-8.

package jomini

import (
	"strings"
	"unicode/utf8"
)

// UTF-8 byte-order-mark bytes. Paradox files are UTF-8, frequently with a BOM.
var utf8BOM = [3]byte{0xEF, 0xBB, 0xBF}

// HasUTF8BOM reports whether raw begins with a UTF-8 byte-order mark.
func HasUTF8BOM(raw []byte) bool {
	return len(raw) >= 3 && raw[0] == utf8BOM[0] && raw[1] == utf8BOM[1] && raw[2] == utf8BOM[2]
}

// Normalize drops CR so Windows CRLF files share offsets with LF editor buffers.
func Normalize(s string) string {
	return strings.ReplaceAll(s, "\r", "")
}

// Decode turns raw file bytes into text for parsing. It strips a leading UTF-8
// BOM and CR, and returns hadBOM. If the bytes are not valid UTF-8 (typically
// hand-edited Latin-1/Windows-1252 files) it falls back to a latin1 decode so
// callers get usable text rather than U+FFFD replacement characters.
func Decode(raw []byte) (text string, hadBOM bool) {
	hadBOM = HasUTF8BOM(raw)
	body := raw
	if hadBOM {
		body = raw[3:]
	}
	if utf8.Valid(body) {
		return Normalize(string(body)), hadBOM
	}
	return Normalize(latin1Decode(raw)), false
}

// latin1Decode maps each byte 1:1 to U+00xx. Used only when raw is not valid UTF-8.
func latin1Decode(raw []byte) string {
	var b strings.Builder
	b.Grow(len(raw))
	for _, c := range raw {
		b.WriteRune(rune(c))
	}
	return b.String()
}
