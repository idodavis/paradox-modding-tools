package files

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
)

// An Encoding says how a text file was stored on disk.
type Encoding string

// The encodings a text file is stored in.
const (
	EncodingUTF8        Encoding = "utf-8"
	EncodingUTF8BOM     Encoding = "utf-8-bom"
	EncodingWindows1252 Encoding = "windows-1252"
)

// TextLimit is the largest file read as text. A bigger file is treated as binary.
const TextLimit = 32 << 20

// bom is the byte order mark that opens a UTF-8 file stored with one.
const bom = "\xEF\xBB\xBF"

// A Text is a file's text together with the encoding it was stored in, so it can be written back the same way.
type Text struct {
	Text     string   `json:"text"`
	Encoding Encoding `json:"encoding"`
}

// ReadText reads a file into its text. The encoding comes from a byte order mark if there is one. Otherwise the
// bytes are taken as UTF-8 if they are valid UTF-8, and as Windows-1252 if not.
func ReadText(path string) (Text, error) {
	stat, err := os.Stat(path)
	if err != nil {
		return Text{}, err
	}
	if stat.Size() > TextLimit {
		return Text{}, fmt.Errorf("%s is %d bytes, over the %d-byte limit for text, so it is binary",
			path, stat.Size(), TextLimit)
	}
	bytes, err := os.ReadFile(path)
	if err != nil {
		return Text{}, err
	}
	return DecodeText(bytes), nil
}

// DecodeText decodes bytes already in memory the same way ReadText decodes a file.
func DecodeText(bytes []byte) Text {
	text := string(bytes)
	switch {
	case strings.HasPrefix(text, bom):
		return Text{Text: text[len(bom):], Encoding: EncodingUTF8BOM}
	case utf8.ValidString(text):
		return Text{Text: text, Encoding: EncodingUTF8}
	}
	decoded, _ := charmap.Windows1252.NewDecoder().Bytes(bytes)
	return Text{Text: string(decoded), Encoding: EncodingWindows1252}
}

// Encode returns the bytes to store the text as, in its own encoding, with the byte order mark if it had one.
func (text Text) Encode() []byte {
	switch text.Encoding {
	case EncodingUTF8BOM:
		return []byte(bom + text.Text)
	case EncodingWindows1252:
		bytes := make([]byte, 0, len(text.Text))
		for _, r := range text.Text {
			code, ok := charmap.Windows1252.EncodeRune(r)
			if !ok {
				code = '?'
			}
			bytes = append(bytes, code)
		}
		return bytes
	}
	return []byte(text.Text)
}

// WriteText writes a text to a file in its own encoding. The write is atomic, as WriteAtomic is.
func WriteText(path string, text Text) error {
	return WriteAtomic(path, text.Encode())
}

// WriteAtomic writes bytes to a file by writing a temporary file beside it and renaming that over it, so the file is
// replaced whole or not at all. A failed write leaves no temporary file behind.
func WriteAtomic(path string, bytes []byte) error {
	temp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	_, err = temp.Write(bytes)
	if err = errors.Join(err, temp.Close()); err == nil {
		err = os.Rename(temp.Name(), path)
	}
	if err != nil {
		return errors.Join(err, os.Remove(temp.Name()))
	}
	return nil
}
