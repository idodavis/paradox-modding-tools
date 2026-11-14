// contents.go reads and writes text, gob caches, table rows and zips, and
// counts the lines a change of text adds and removes.

package files

import (
	"archive/zip"
	"bytes"
	"encoding/gob"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/sergi/go-diff/diffmatchpatch"
	"golang.org/x/text/encoding/charmap"
)

// Encoding is how a text file is stored.
type Encoding string

// The encodings of a text file.
const (
	EncodingUTF8        Encoding = "utf-8"
	EncodingUTF8BOM     Encoding = "utf-8-bom"
	EncodingWindows1252 Encoding = "windows-1252"
)

// TextLimit is the largest file read as text. A bigger one is binary.
const TextLimit = 32 << 20

const bom = "\xEF\xBB\xBF"

// Text is a file's text and its encoding, so it is written back the same way.
type Text struct {
	Text     string   `json:"text"`
	Encoding Encoding `json:"encoding"`
}

// Row is one row of a table file.
type Row struct {
	// Name is the row's first column.
	Name string

	// Start is the row's offset in the text, and Line its line from 0.
	Start, Line int
}

// ErrBinary is the error for a binary file or one over TextLimit.
var ErrBinary = errors.New("binary")

// ReadText reads a text file. It returns an error wrapping ErrBinary for a
// binary file or one over TextLimit.
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

// LoadText is ReadText, but a missing file gives empty text and no error.
func LoadText(path string) (Text, error) {
	text, err := ReadText(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Text{}, nil
	}

	return text, err
}

// DecodeText decodes text bytes, as Windows-1252 if they are not UTF-8.
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

// Encode returns the text's bytes. Windows-1252 writes a character it cannot
// hold as `?`.
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

// WriteText writes the text to a file in its encoding, atomically.
func WriteText(path string, text Text) error {
	return WriteAtomic(path, text.Encode())
}

// WriteAtomic replaces a file whole or not at all, making its folder first.
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

// LoadGob loads a gob cache file. It returns false if the file is missing,
// does not decode, or was saved from another shape of the type, which gob
// would fill with holes.
func LoadGob[T any](path string) (T, bool) {
	var zero, value T

	content, err := os.ReadFile(path)
	if err != nil {
		return zero, false
	}

	decoder := gob.NewDecoder(bytes.NewReader(content))
	var shape uint64
	if err := decoder.Decode(&shape); err != nil || shape != hashShape(reflect.TypeFor[T]()) {
		return zero, false
	}

	if err := decoder.Decode(&value); err != nil {
		return zero, false
	}

	return value, true
}

// SaveGob saves a value as a gob cache file.
func SaveGob[T any](path string, value T) error {
	var buffer bytes.Buffer
	encoder := gob.NewEncoder(&buffer)
	if err := errors.Join(encoder.Encode(hashShape(reflect.TypeFor[T]())), encoder.Encode(value)); err != nil {
		return err
	}

	return WriteAtomic(path, buffer.Bytes())
}

// hashShape returns a number that changes with what gob stores of a type.
func hashShape(root reflect.Type) uint64 {
	hash := fnv.New64a()
	visited := map[reflect.Type]bool{}

	var write func(current reflect.Type)
	write = func(current reflect.Type) {
		_, _ = io.WriteString(hash, current.String()+" "+current.Kind().String()+";")
		if visited[current] {
			return
		}

		visited[current] = true
		switch current.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Array:
			write(current.Elem())

		case reflect.Map:
			write(current.Key())
			write(current.Elem())

		case reflect.Struct:
			for position := range current.NumField() {
				if field := current.Field(position); field.IsExported() {
					_, _ = io.WriteString(hash, field.Name+":")
					write(field.Type)
				}
			}
		}
	}
	write(root)

	return hash.Sum64()
}

// The marks of a table's columns and comment rows, as in CK3's
// map_data/definition.csv.
const (
	tableSeparator = ";"
	tableComment   = "#"
)

// ListRows returns the rows of a table's text. It skips a row with an empty
// first column or a comment mark.
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

// CountLines returns how many lines a change from one text to another adds and
// removes.
func CountLines(before, after string) (added, removed int) {
	// go-diff's line mode turns each line into one rune.
	diff := diffmatchpatch.New()
	first, second, _ := diff.DiffLinesToRunes(before, after)
	for _, piece := range diff.DiffMainRunes(first, second, false) {
		switch piece.Type {
		case diffmatchpatch.DiffInsert:
			added += utf8.RuneCountInString(piece.Text)
		case diffmatchpatch.DiffDelete:
			removed += utf8.RuneCountInString(piece.Text)
		}
	}

	return added, removed
}

// Unzip writes a zip's files into a folder, without their shared top folder,
// and skips links. It returns an error if a name leaves the folder.
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

// FindZipFolder returns the folder that holds every file of a zip, or "".
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

func listZipFiles(archive *zip.Reader) []*zip.File {
	var entries []*zip.File

	for _, entry := range archive.File {
		if !entry.FileInfo().IsDir() && entry.Mode()&fs.ModeSymlink == 0 {
			entries = append(entries, entry)
		}
	}

	return entries
}

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
