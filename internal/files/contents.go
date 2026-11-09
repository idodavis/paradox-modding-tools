// contents.go reads and writes the contents of files: text in its encoding,
// atomic writes, gob caches, a table's rows and zips.

package files

import (
	"archive/zip"
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

// Encoding says how a text file was stored on disk.
type Encoding string

// The encodings a text file is stored in.
const (
	EncodingUTF8        Encoding = "utf-8"
	EncodingUTF8BOM     Encoding = "utf-8-bom"
	EncodingWindows1252 Encoding = "windows-1252"
)

// TextLimit is the largest file read as text. A bigger file is treated as
// binary.
const TextLimit = 32 << 20

// bom is the byte order mark that opens a UTF-8 file stored with one.
const bom = "\xEF\xBB\xBF"

// Text is the text of a file. It remembers how the file was stored, so it can
// be written back the same way.
type Text struct {
	Text     string   `json:"text"`
	Encoding Encoding `json:"encoding"`
}

// Row is one row of a table file.
type Row struct {
	// Name is the row's first column, which names it.
	Name string

	// Start is the offset in the text where the row starts, and Line is its
	// line, counting from 0.
	Start, Line int
}

// ErrBinary is the error for a file that is not text: its bytes are binary,
// or it is over TextLimit.
var ErrBinary = errors.New("binary")

// ReadText reads a file into its text. It returns an error wrapping ErrBinary
// for a file over TextLimit and for one that is binary.
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

// LoadText reads a file into its text as ReadText does. A missing file gives
// empty text and no error.
func LoadText(path string) (Text, error) {
	text, err := ReadText(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Text{}, nil
	}

	return text, err
}

// DecodeText decodes the bytes of a text file. Bytes that are not UTF-8 are
// read as Windows-1252.
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

// Encode returns the bytes of the text in its encoding. A character
// Windows-1252 cannot hold becomes `?`.
func (t Text) Encode() []byte {
	switch t.Encoding {
	case EncodingUTF8BOM:
		return []byte(bom + t.Text)

	case EncodingWindows1252:
		content := make([]byte, 0, len(t.Text))
		for _, r := range t.Text {
			code, ok := charmap.Windows1252.EncodeRune(r)
			if !ok {
				code = '?'
			}

			content = append(content, code)
		}

		return content
	}

	return []byte(t.Text)
}

// WriteText writes the text to the file at the given path, in the text's
// encoding. It writes as WriteAtomic does.
func WriteText(path string, text Text) error {
	return WriteAtomic(path, text.Encode())
}

// WriteAtomic writes the bytes to the file at the given path, replacing the
// file whole or not at all. It makes the file's folder first.
func WriteAtomic(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

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

// LoadGob loads a value from a gob cache file. It returns the zero value and
// false if the file is missing or does not decode. A file that makes the
// decoder panic does not decode either: gob panics, rather than returning an
// error, where a field of a cache saved by an older type reaches through a
// nil embedded pointer.
func LoadGob[T any](path string) (value T, ok bool) {
	var zero T

	content, err := os.ReadFile(path)
	if err != nil {
		return zero, false
	}

	defer func() {
		if recover() != nil {
			value, ok = zero, false
		}
	}()

	if err := gob.NewDecoder(bytes.NewReader(content)).Decode(&value); err != nil {
		return zero, false
	}

	return value, true
}

// SaveGob saves a value as a gob cache file at the given path. It writes as
// WriteAtomic does.
func SaveGob(path string, value any) error {
	var buffer bytes.Buffer
	if err := gob.NewEncoder(&buffer).Encode(value); err != nil {
		return err
	}

	return WriteAtomic(path, buffer.Bytes())
}

// The mark between the columns of a table file's row, and the mark that opens
// a comment row, as CK3's map_data/definition.csv writes them.
const (
	tableSeparator = ";"
	tableComment   = "#"
)

// ListRows returns the rows of a table file's text, in order. A row whose
// first column is empty, or which starts with the comment mark, is left out.
func ListRows(text string) []Row {
	var rows []Row
	start, number := 0, 0

	for line := range strings.Lines(text) {
		column, _, _ := strings.Cut(strings.TrimRight(line, "\r\n"), tableSeparator)
		if column != "" && !strings.HasPrefix(column, tableComment) {
			rows = append(rows, Row{Name: column, Start: start, Line: number})
		}

		start += len(line)
		number++
	}

	return rows
}

// Unzip writes the files of a zip into the given folder, without a top folder
// that holds every file, and skips links. It returns an error if a file
// cannot be written or the zip names a path outside the folder.
func Unzip(archive *zip.Reader, dir string) error {
	top := FindZipFolder(archive)

	for _, entry := range listZipFiles(archive) {
		target := Join(dir, strings.TrimPrefix(entry.Name, top+"/"))
		if !IsUnder(dir, target) {
			return fmt.Errorf("the zip names %s, which is outside its own folder", entry.Name)
		}

		if err := writeEntry(entry, target); err != nil {
			return err
		}
	}

	return nil
}

// FindZipFolder returns the top folder that holds every file of a zip, or ""
// if no one folder does.
func FindZipFolder(archive *zip.Reader) string {
	entries := listZipFiles(archive)
	if len(entries) == 0 {
		return ""
	}

	top, _, _ := strings.Cut(entries[0].Name, "/")
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name, top+"/") {
			return ""
		}
	}

	return top
}

// listZipFiles returns the files of a zip, leaving out its folders and links.
func listZipFiles(archive *zip.Reader) []*zip.File {
	var entries []*zip.File

	for _, entry := range archive.File {
		if !entry.FileInfo().IsDir() && entry.Mode()&fs.ModeSymlink == 0 {
			entries = append(entries, entry)
		}
	}

	return entries
}

// writeEntry writes one file of a zip to disk, making its folder first.
func writeEntry(entry *zip.File, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}

	reader, err := entry.Open()
	if err != nil {
		return err
	}
	defer func() { _ = reader.Close() }()

	file, err := os.Create(target)
	if err != nil {
		return err
	}

	_, err = io.Copy(file, reader)

	return errors.Join(err, file.Close())
}
