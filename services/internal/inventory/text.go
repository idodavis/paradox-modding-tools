// text.go converts between a file's bytes and its text: UTF-8 with or without a byte order mark, and
// Windows-1252 where the bytes are not UTF-8.

package inventory

import (
	"bytes"
	"fmt"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"

	"paradox-modding-tools/services/internal/jomini"
)

// DecodeText is a file's bytes as text, for the engine and the editor alike: a leading BOM taken off (bom), and bytes
// that are not valid UTF-8 read as Windows-1252, the games' old encoding (legacy). Line ends are kept.
func DecodeText(raw []byte) (text string, bom, legacy bool) {
	body, bom := bytes.CutPrefix(raw, []byte(jomini.BOM))
	if utf8.Valid(body) {
		return string(body), bom, false
	}
	text, _ = charmap.Windows1252.NewDecoder().String(string(body))
	return text, bom, true
}

// EncodeText is text as a file's bytes: the BOM when it had one, and Windows-1252 when it was read so; a character
// Windows-1252 has no byte for is an error, never a silent change.
func EncodeText(text string, bom, legacy bool) ([]byte, error) {
	if legacy {
		var err error
		if text, err = charmap.Windows1252.NewEncoder().String(text); err != nil {
			return nil, fmt.Errorf("the file is Windows-1252, which has no byte for a character in it: %w", err)
		}
	}
	if bom {
		text = jomini.BOM + text
	}
	return []byte(text), nil
}
