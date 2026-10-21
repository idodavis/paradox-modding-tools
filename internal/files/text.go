package files

import "golang.org/x/text/encoding/charmap"

// Windows-1252 decoding comes from this library.
var _ = charmap.Windows1252

// An Encoding says how a text file was stored on disk.
type Encoding string

const (
	EncodingUTF8        Encoding = "utf-8"
	EncodingUTF8BOM     Encoding = "utf-8-bom"
	EncodingWindows1252 Encoding = "windows-1252"
)

// TextLimit is the largest file read as text. A bigger file is treated as binary.
const TextLimit = 32 << 20

// A Text is a file's text together with the encoding it was stored in, so it can be written back the same way.
type Text struct {
	Text     string   `json:"text"`
	Encoding Encoding `json:"encoding"`
}

// ReadText reads a file into its text. The encoding comes from a byte order mark if there is one. Otherwise the
// bytes are taken as UTF-8 if they are valid UTF-8, and as Windows-1252 if not.
func ReadText(path string) (Text, error) { panic("stub") }

// DecodeText decodes bytes already in memory the same way ReadText decodes a file.
func DecodeText(bytes []byte) Text { panic("stub") }

// Encode returns the bytes to store the text as, in its own encoding, with the byte order mark if it had one.
func (text Text) Encode() []byte { panic("stub") }

// WriteText writes a text to a file in its own encoding. The write is atomic: the file is replaced whole or not
// at all.
func WriteText(path string, text Text) error { panic("stub") }
