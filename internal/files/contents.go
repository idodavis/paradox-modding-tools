package files

import (
	"bytes"
	"encoding/gob"
	"errors"
	"fmt"
	"io"
	"io/fs"
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

// A Text is the text of a file. It remembers how the file was stored, so it can be written back the same way.
type Text struct {
	Text     string   `json:"text"`
	Encoding Encoding `json:"encoding"`
}

// ErrBinary is the error for a file that is not text: its bytes are binary, or it is over TextLimit.
var ErrBinary = errors.New("binary")

// ReadText reads a file into its text. It returns an error wrapping ErrBinary for a file over TextLimit and for one
// that is binary.
func ReadText(path string) (Text, error) {
	file, err := os.Open(path)
	if err != nil {
		return Text{}, err
	}
	defer func() { _ = file.Close() }()
	stat, err := file.Stat()
	if err != nil {
		return Text{}, err
	}
	if stat.Size() > TextLimit {
		return Text{}, fmt.Errorf("%s is %d bytes, over the %d-byte limit for text, so it is %w",
			path, stat.Size(), TextLimit, ErrBinary)
	}
	content, err := io.ReadAll(file)
	if err != nil {
		return Text{}, err
	}
	if IsBinary(content) {
		return Text{}, fmt.Errorf("%s is %w", path, ErrBinary)
	}
	return DecodeText(content), nil
}

// LoadText reads a file into its text as ReadText does. A missing file gives empty text and no error.
func LoadText(path string) (Text, error) {
	text, err := ReadText(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Text{}, nil
	}
	return text, err
}

// DecodeText decodes bytes already in memory. A byte order mark means UTF-8 with one. Without it, valid UTF-8 is
// UTF-8 and any other bytes are Windows-1252.
func DecodeText(content []byte) Text {
	text := string(content)
	switch {
	case strings.HasPrefix(text, bom):
		return Text{Text: text[len(bom):], Encoding: EncodingUTF8BOM}
	case utf8.ValidString(text):
		return Text{Text: text, Encoding: EncodingUTF8}
	}
	decoded, _ := charmap.Windows1252.NewDecoder().Bytes(content)
	return Text{Text: string(decoded), Encoding: EncodingWindows1252}
}

// Encode returns the bytes to store the text as, in its own encoding, with the byte order mark if it had one. A
// character that Windows-1252 cannot hold becomes `?`.
func (text Text) Encode() []byte {
	switch text.Encoding {
	case EncodingUTF8BOM:
		return []byte(bom + text.Text)
	case EncodingWindows1252:
		content := make([]byte, 0, len(text.Text))
		for _, r := range text.Text {
			code, ok := charmap.Windows1252.EncodeRune(r)
			if !ok {
				code = '?'
			}
			content = append(content, code)
		}
		return content
	}
	return []byte(text.Text)
}

// WriteText writes a text to a file in its own encoding. The write is atomic, as WriteAtomic is.
func WriteText(path string, text Text) error {
	return WriteAtomic(path, text.Encode())
}

// WriteAtomic writes bytes to a file so that the file is replaced whole or not at all. It makes the file's folder
// first. A failed write leaves no temporary file behind.
func WriteAtomic(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	// The bytes go to a temporary file beside the target, which is then renamed over it.
	temp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	_, err = temp.Write(content)
	if err = errors.Join(err, temp.Close()); err == nil {
		err = os.Rename(temp.Name(), path)
	}
	if err != nil {
		return errors.Join(err, os.Remove(temp.Name()))
	}
	return nil
}

// LoadGob loads a value from a gob cache file. It returns false if the file is missing or cannot be decoded.
func LoadGob[T any](path string) (T, bool) {
	var value T
	content, err := os.ReadFile(path)
	if err != nil {
		return value, false
	}
	if err := gob.NewDecoder(bytes.NewReader(content)).Decode(&value); err != nil {
		return *new(T), false
	}
	return value, true
}

// SaveGob saves a value as a gob cache file. It writes the file as WriteAtomic does.
func SaveGob(path string, value any) error {
	var buffer bytes.Buffer
	if err := gob.NewEncoder(&buffer).Encode(value); err != nil {
		return err
	}
	return WriteAtomic(path, buffer.Bytes())
}
