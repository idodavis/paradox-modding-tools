// text.go is a file's text: the one reader and the one writer, which take a file's bytes to its text and back the way
// the file was stored, and the one cap on how much text a file may hold.

package inventory

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"

	"paradox-modding-tools/services/internal/appdata"
	"paradox-modding-tools/services/internal/jomini"
)

// Text is a file's text and how the file stored it, so that SaveText writes it back the same way.
type Text struct {
	Content string // the text without its byte order mark, its line endings as the file wrote them
	BOM     bool   // the file began with a UTF-8 byte order mark
	Legacy  bool   // the file was Windows-1252, the games' old encoding, not UTF-8
}

// MaxTextBytes is the largest text file LoadText takes: the one cap of the editor, a search, the scan and every file
// PMT reads for itself. The largest text file the three installs ship is 56 MB (EU5's generated map objects).
const MaxTextBytes = 64 << 20

// ErrBinary and ErrTooLarge are why LoadText and TextOf give no text for a file.
var (
	ErrBinary   = errors.New("binary file: the editor opens script, localization and other text files only")
	ErrTooLarge = errors.New("file too large to open")
)

// Text is the file's text (LoadText), as the scan reads it.
func (file File) Text() (Text, error) {
	return LoadText(file.Path)
}

// LoadText is the text of the file at path, for the editor, a search, the scan and every file PMT reads for itself;
// ErrTooLarge past MaxTextBytes. It reads the file's head first and stops there for a binary file (a search meets a
// mod's meshes, banks and images by the thousand), and reads the rest only for text.
func LoadText(path string) (Text, error) {
	file, err := os.Open(path)
	if err != nil {
		return Text{}, err
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	switch {
	case err != nil:
		return Text{}, err
	case info.IsDir():
		return Text{}, fmt.Errorf("%s is a folder", path)
	case info.Size() > MaxTextBytes:
		return Text{}, fmt.Errorf("%w (%d bytes, max %d)", ErrTooLarge, info.Size(), MaxTextBytes)
	}

	raw := make([]byte, info.Size())
	count, err := io.ReadFull(file, raw[:min(BinaryHead, len(raw))])
	if IsBinary(raw[:count]) {
		return Text{}, ErrBinary
	}
	if err == nil {
		var rest int
		rest, err = io.ReadFull(file, raw[count:])
		count += rest
	}
	if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
		err = nil // the file is shorter than Stat said: what is there is its text
	}
	if err != nil {
		return Text{}, err
	}
	return TextOf(raw[:count])
}

// TextOf is a file's bytes as text: a leading byte order mark taken off, and bytes that are not valid UTF-8 read as
// Windows-1252. Bytes that are binary (Binary) are ErrBinary.
func TextOf(raw []byte) (Text, error) {
	if IsBinary(raw) {
		return Text{}, ErrBinary
	}
	body, bom := bytes.CutPrefix(raw, []byte(jomini.BOM))
	if utf8.Valid(body) {
		return Text{Content: string(body), BOM: bom}, nil
	}
	content, _ := charmap.Windows1252.NewDecoder().String(string(body))
	return Text{Content: content, BOM: bom, Legacy: true}, nil
}

// SaveText writes text to the file at path, whole or not at all (appdata.Write), as Bytes stores it.
func SaveText(path string, text Text) error {
	data, err := text.Bytes()
	if err != nil {
		return err
	}
	return appdata.WriteBytes(path, data)
}

// Bytes is the text as a file stores it: the byte order mark where it had one, and Windows-1252 where it was read so. A
// character Windows-1252 has no byte for is an error, never a silent change.
func (text Text) Bytes() ([]byte, error) {
	content := text.Content
	if text.Legacy {
		var err error
		if content, err = charmap.Windows1252.NewEncoder().String(content); err != nil {
			return nil, fmt.Errorf("the file is Windows-1252, which has no byte for a character in it: %w", err)
		}
	}
	if text.BOM {
		content = jomini.BOM + content
	}
	return []byte(content), nil
}
