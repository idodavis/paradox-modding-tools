// Package texture decodes DDS, TGA and PNG images to pixels and reads an image's header without decoding it. It also
// finds the image file that a path written in script names.
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
	"slices"
	"strings"
)

// A Header is what an image file says about itself before its pixels.
type Header struct {
	Format      string `json:"format"` // dds, tga, png
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	Mipmaps     int    `json:"mipmaps"`
	Compression string `json:"compression"` // BC1 to BC7, or "" for uncompressed
	Bytes       int64  `json:"bytes"`       // the file's size
}

// The magic bytes that open a DDS and a PNG.
const (
	ddsMagic = "DDS "
	pngMagic = "\x89PNG\r\n\x1a\n"
)

// sideLimit is the most pixels an image may have on a side, Direct3D's limit on a texture. A header past it is
// corrupt, not a picture.
const sideLimit = 16384

// extensions are the image extensions the game accepts, in the order they are tried.
var extensions = []string{".dds", ".png", ".tga"}

// errTruncated is the error for an image whose data ends before its last pixel.
var errTruncated = errors.New("the image data ends before its last pixel")

// ReadHeader reads an image file's header and size without decoding its pixels. It returns an error if the file
// cannot be read or is not a DDS, TGA or PNG.
func ReadHeader(path string) (Header, error) {
	file, err := os.Open(path)
	if err != nil {
		return Header{}, err
	}
	defer func() { _ = file.Close() }()
	stat, err := file.Stat()
	if err != nil {
		return Header{}, err
	}
	head := make([]byte, 256)
	count, err := io.ReadFull(file, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return Header{}, err
	}
	header, ok := decodeHeader(head[:count])
	if !ok {
		return Header{}, fmt.Errorf("%s: not a DDS, TGA or PNG", path)
	}
	header.Bytes = stat.Size()
	return header, nil
}

// decodeHeader decodes the header from the first bytes of an image. It returns false if the bytes are not an image
// format PMT knows.
func decodeHeader(file []byte) (Header, bool) {
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

// Decode decodes one mip level of an image's bytes to pixels: 0 is the whole image, and each level after it is half
// the size of the one before. The bytes say which format it is: DDS, TGA or PNG, and only a DDS has more than one
// level. It returns an error for an unknown or unsupported format, for an image whose data ends early, for one past
// the size limit and for a level the image does not have.
func Decode(file []byte, level int) (image.Image, error) {
	header, ok := decodeHeader(file)
	switch {
	case ok && (header.Width > sideLimit || header.Height > sideLimit):
		return nil, fmt.Errorf("%d by %d pixels is past the %d limit on a side", header.Width, header.Height, sideLimit)
	case ok && (level < 0 || level >= header.Mipmaps):
		return nil, fmt.Errorf("the image has %d levels, so no level %d", header.Mipmaps, level)
	case ok && header.Format == "dds":
		return decodeDDS(file, header, level)
	case ok && header.Format == "tga":
		return decodeTGA(file, header)
	case hasMagic(file, pngMagic):
		return png.Decode(bytes.NewReader(file))
	case hasMagic(file, ddsMagic):
		if len(file) < 128 || (string(file[84:88]) == "DX10" && len(file) < 148) {
			return nil, errTruncated
		}
		return nil, fmt.Errorf("DDS %s is not supported", describeDDSFormat(file))
	}
	return nil, errors.New("not a DDS, TGA or PNG")
}

// Resolve finds the file that a path written in script names. It tries the path as written, then with each image
// extension the game accepts in place of the written one. A leading slash is ignored. The lookup function says
// whether a relative path exists in the workspace and where. It returns false if none of the paths exists.
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

// IsImage reports whether a path written in script ends in the extension of an image the game accepts, in any case.
func IsImage(written string) bool {
	return slices.ContainsFunc(extensions, func(extension string) bool {
		return strings.EqualFold(filepath.Ext(written), extension)
	})
}

// hasMagic reports whether a file begins with the given magic.
func hasMagic(file []byte, magic string) bool {
	return len(file) >= len(magic) && string(file[:len(magic)]) == magic
}
