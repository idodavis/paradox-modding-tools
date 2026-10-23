// Package texture decodes DDS and TGA images to pixels, reads an image's header without decoding it, and finds
// the image file that a path written in script names.
package texture

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// A Header is what an image file says about itself before its pixels.
type Header struct {
	Format      string `json:"format"` // dds, tga, png
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	Mipmaps     int    `json:"mipmaps"`
	Compression string `json:"compression"` // BC1 to BC7, or "" for uncompressed
}

// The first bytes of a DDS and of a PNG.
const (
	ddsMagic = "DDS "
	pngMagic = "\x89PNG\r\n\x1a\n"
)

// sideLimit is the most pixels an image may have on a side, Direct3D's limit on a texture. A header past it is
// corrupt, not a picture.
const sideLimit = 16384

// extensions are the image extensions the game accepts, in the order Resolve tries them.
var extensions = []string{".dds", ".png", ".tga"}

// errTruncated is the error for an image whose data ends before its last pixel.
var errTruncated = errors.New("the image data ends before its last pixel")

// ReadHeader reads an image file's header without decoding its pixels.
func ReadHeader(path string) (Header, error) {
	file, err := os.Open(path)
	if err != nil {
		return Header{}, err
	}
	head := make([]byte, 256)
	count, err := io.ReadFull(file, head)
	_ = file.Close()
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return Header{}, err
	}
	header, ok := DecodeHeader(head[:count])
	if !ok {
		return Header{}, fmt.Errorf("%s: not a DDS, TGA or PNG", path)
	}
	return header, nil
}

// DecodeHeader reads the header from the first bytes of an image. It returns false if the bytes are not an image
// format PMT knows.
func DecodeHeader(file []byte) (Header, bool) {
	switch {
	case hasMagic(file, ddsMagic):
		return decodeDDSHeader(file)
	case hasMagic(file, pngMagic):
		if len(file) < 24 {
			return Header{}, false
		}
		width, height := binary.BigEndian.Uint32(file[16:]), binary.BigEndian.Uint32(file[20:])
		return Header{Format: "png", Width: int(width), Height: int(height), Mipmaps: 1}, true
	}
	return decodeTGAHeader(file)
}

// Decode decodes an image's bytes to pixels. The format is read from the bytes: DDS, TGA or PNG.
func Decode(file []byte) (image.Image, error) {
	header, ok := DecodeHeader(file)
	switch {
	case ok && (header.Width > sideLimit || header.Height > sideLimit):
		return nil, fmt.Errorf("%d by %d pixels is past the %d limit on a side", header.Width, header.Height, sideLimit)
	case hasMagic(file, ddsMagic):
		return decodeDDS(file)
	case hasMagic(file, pngMagic):
		return png.Decode(bytes.NewReader(file))
	case ok:
		return decodeTGA(file)
	}
	return nil, errors.New("not a DDS, TGA or PNG")
}

// Resolve finds the file that a path written in script names. It tries the path as written, then with each image
// extension the game accepts in place of the written one. A leading slash is ignored. The lookup function says
// whether a relative path exists in the workspace and where.
func Resolve(written string, lookup func(rel string) (path string, ok bool)) (path string, ok bool) {
	written = strings.TrimPrefix(written, "/")
	if path, ok = lookup(written); ok {
		return path, true
	}
	extension := filepath.Ext(written)
	stem := strings.TrimSuffix(written, extension)
	for _, candidate := range extensions {
		if strings.EqualFold(candidate, extension) {
			continue
		}
		if path, ok = lookup(stem + candidate); ok {
			return path, true
		}
	}
	return "", false
}

// hasMagic reports whether a file begins with the given magic.
func hasMagic(file []byte, magic string) bool {
	return len(file) >= len(magic) && string(file[:len(magic)]) == magic
}
