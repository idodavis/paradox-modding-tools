// Package texture decodes DDS, TGA and PNG images to pixels and reads an image's header without decoding it. It also
// finds the image file that a path written in script names.
package texture

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// A Header is what an image file says about itself before its pixels.
type Header struct {
	Format      string `json:"format"` // dds, tga, png, jpeg
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	Mipmaps     int    `json:"mipmaps"`
	Compression string `json:"compression"` // BC1 to BC7, or "" for uncompressed
	Bytes       int64  `json:"bytes"`       // the file's size
}

// The magic bytes that open a DDS, a PNG and a JPEG.
const (
	ddsMagic  = "DDS "
	pngMagic  = "\x89PNG\r\n\x1a\n"
	jpegMagic = "\xff\xd8\xff"
)

// sideLimit is the most pixels an image may have on a side, Direct3D's limit on a texture. A header past it is
// corrupt, not a picture.
const sideLimit = 16384

// extensions are the image extensions the game accepts, in the order they are tried.
var extensions = []string{".dds", ".png", ".tga"}

// errTruncated is the error for an image whose data ends before its last pixel.
var errTruncated = errors.New("the image data ends before its last pixel")

// ReadHeader reads an image file's header and size without decoding its pixels. The file's bytes say which format it
// is, whatever its extension. It returns an error if the file cannot be read or is not a DDS, TGA, PNG or JPEG.
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
	if !ok && hasMagic(head[:count], jpegMagic) {
		header, ok = decodeJPEGHeader(file)
	}
	if !ok {
		return Header{}, fmt.Errorf("%s: not a DDS, TGA, PNG or JPEG", path)
	}
	header.Bytes = stat.Size()
	return header, nil
}

// decodeJPEGHeader decodes the header of a JPEG file. A JPEG keeps its size past its first bytes, so it reads from the
// start of the file. It returns false if the file is not a JPEG that can be read.
func decodeJPEGHeader(file io.ReadSeeker) (Header, bool) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return Header{}, false
	}
	config, err := jpeg.DecodeConfig(file)
	return Header{Format: "jpeg", Width: config.Width, Height: config.Height, Mipmaps: 1}, err == nil
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

// Decode decodes one mip level of an image to pixels, 0 being the whole image. It returns an error if the image or
// the level cannot be read.
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

// Resolve finds the file a path in script names, trying each image extension the game accepts. It returns false if
// none exists.
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
