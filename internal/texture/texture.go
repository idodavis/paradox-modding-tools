// Package texture reads the images the games ship and writes pictures as DDS
// files. It reads an image's header without its pixels, decodes one mip level
// of it, and finds the image file that a path written in script names.
//
// The bytes say the format, whatever the extension:
//
//   - A DDS opens with "DDS ". dds-go reads its headers and decodes its
//     levels, in every format dds-go knows: BC1 to BC7, the uncompressed
//     formats and the float ones.
//   - A PNG and a JPEG open with their own magic bytes. The standard library
//     reads the header of both and decodes a PNG. A JPEG is read for its
//     header alone.
//   - Any other bytes are a TGA when their first 18 bytes are a TGA header of
//     true color or gray, raw or run-length encoded, of 8 to 32 bits a pixel,
//     with a size. pdx-asset-go's texture package decodes it.
//
// A legacy DDS whose pixels only bit masks describe, such as one of 24 bits a
// pixel, has no DXGI format, so its header names none.
//
// Resolve tries a path as script writes it, then with each other extension
// the game accepts, .dds, .png and .tga in that order.
//
// # Robustness
//
// Decode and EncodeDDS refuse an image past 16,384 pixels on a side, which is
// Direct3D's limit. Decode refuses a level the image does not have, a DDS
// format dds-go does not decode, with an error that names the format, and
// data that ends before its last pixel. ReadHeader and Decode refuse a TGA of
// palette indices, which pdx-asset-go does not read, and bytes that are not a
// DDS, TGA, PNG or JPEG.
package texture

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // registers JPEG with image.DecodeConfig
	"image/png"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	dds "github.com/kaiser-chris/dds-go"
	pdxtexture "github.com/kaiser-chris/pdx-asset-go/texture"
)

// Header is what an image file says about itself before its pixels.
type Header struct {
	// Format is dds, tga, png or jpeg.
	Format string `json:"format"`

	Width  int `json:"width"`
	Height int `json:"height"`

	// Mipmaps is how many mip levels the image has. It is 1 for all but a DDS.
	Mipmaps int `json:"mipmaps"`

	// PixelFormat is a DDS's DXGI format as dds-go names it, such as
	// BC7_UNORM, and DXGI its number. Both are empty for other images and for
	// a DDS described only by bit masks.
	PixelFormat string `json:"pixelFormat"`
	DXGI        uint32 `json:"dxgi"`

	Bytes int64 `json:"bytes"`
}

// tgaHeaderSize is how many bytes tell the formats apart.
const tgaHeaderSize = 18

// extensions are the image extensions the game accepts, in Resolve's order.
var extensions = []string{".dds", ".png", ".tga"}

var errUnknown = errors.New("not a DDS, TGA, PNG or JPEG")

// ReadHeader reads an image file's header without its pixels. It returns an
// error if the file cannot be read or is no image PMT reads.
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

	header, err := decodeHeader(file)
	if err != nil {
		return Header{}, fmt.Errorf("%s: %w", path, err)
	}

	header.Bytes = stat.Size()

	return header, nil
}

var (
	ddsMagic  = []byte("DDS ")
	pngMagic  = []byte("\x89PNG\r\n\x1a\n")
	jpegMagic = []byte("\xff\xd8\xff")
)

// decodeHeader decodes an image header from the reader's start, as its first
// bytes say. It returns errUnknown if they are no known format.
func decodeHeader(reader io.ReadSeeker) (Header, error) {
	head := make([]byte, tgaHeaderSize)

	// Short input is read as far as it goes.
	count, err := io.ReadFull(reader, head)
	short := errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)
	if err != nil && !short {
		return Header{}, err
	}

	head = head[:count]

	if _, err := reader.Seek(0, io.SeekStart); err != nil {
		return Header{}, err
	}

	switch {
	case bytes.HasPrefix(head, ddsMagic):
		return decodeDDSHeader(reader)

	case bytes.HasPrefix(head, pngMagic), bytes.HasPrefix(head, jpegMagic):
		config, format, err := image.DecodeConfig(reader)
		if err != nil {
			return Header{}, err
		}

		return Header{Format: format, Width: config.Width, Height: config.Height, Mipmaps: 1}, nil
	}

	return decodeTGAHeader(head)
}

func decodeDDSHeader(reader io.Reader) (Header, error) {
	file, err := dds.ReadHeader(reader)
	if err != nil {
		return Header{}, err
	}

	header := Header{Format: "dds", Width: file.Width, Height: file.Height, Mipmaps: file.MipLevels}
	if !file.Masked() {
		header.PixelFormat, header.DXGI = file.Format.String(), uint32(file.Format)
	}

	return header, nil
}

// tgaTypes are the TGA types pdx-asset-go reads, true color and gray, raw or
// run-length encoded. tgaPaletteTypes are those of palette indices, which it
// does not.
var (
	tgaTypes        = []byte{2, 3, 10, 11}
	tgaPaletteTypes = []byte{1, 9}
)

// tgaDepths are the bits a pixel may take.
var tgaDepths = []byte{8, 16, 24, 32}

// decodeTGAHeader decodes the 18 bytes a TGA opens with. A TGA has no magic
// bytes, so it is known by its header. It returns an error for palette
// indices and errUnknown for other bytes.
func decodeTGAHeader(head []byte) (Header, error) {
	if len(head) < tgaHeaderSize {
		return Header{}, errUnknown
	}

	width := int(binary.LittleEndian.Uint16(head[12:]))
	height := int(binary.LittleEndian.Uint16(head[14:]))
	known := head[1] == 0 && slices.Contains(tgaTypes, head[2]) && slices.Contains(tgaDepths, head[16]) &&
		width > 0 && height > 0

	switch {
	case head[1] == 1 && slices.Contains(tgaPaletteTypes, head[2]):
		return Header{}, errors.New("a TGA of palette indices, which PMT does not read")
	case !known:
		return Header{}, errUnknown
	}

	return Header{Format: "tga", Width: width, Height: height, Mipmaps: 1}, nil
}

// Decode decodes one mip level of an image, 0 being the whole. It returns an
// error for bytes that are not a DDS, TGA or PNG, a side past Direct3D's
// limit, a missing level or undecodable pixels.
func Decode(data []byte, level int) (image.Image, error) {
	header, err := decodeHeader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}

	if err := checkSides(header.Width, header.Height); err != nil {
		return nil, err
	}

	if level < 0 || level >= header.Mipmaps {
		return nil, fmt.Errorf("the image has %d levels, so no level %d", header.Mipmaps, level)
	}

	switch header.Format {
	case "dds":
		return decodeDDS(data, level)
	case "tga":
		return decodeTGA(data)
	case "png":
		return png.Decode(bytes.NewReader(data))
	}

	return nil, errors.New("a JPEG is read for its header alone")
}

// sideLimit is Direct3D's limit on pixels on a side.
const sideLimit = 16384

func checkSides(width, height int) error {
	if width > sideLimit || height > sideLimit {
		return fmt.Errorf("%d by %d pixels is past the %d limit on a side", width, height, sideLimit)
	}

	return nil
}

// decodeDDS decodes a level of a DDS's first image, for a cubemap its first
// face.
func decodeDDS(data []byte, level int) (image.Image, error) {
	file, err := dds.Parse(data)
	if err != nil {
		return nil, err
	}

	return file.Decode(0, level)
}

func decodeTGA(data []byte) (image.Image, error) {
	pixels, err := pdxtexture.DecodeTGA(data)
	if err != nil {
		return nil, err
	}

	// The pixels are non-premultiplied RGBA, first row on top, as an
	// image.NRGBA keeps them.
	bounds := image.Rect(0, 0, pixels.Width, pixels.Height)

	return &image.NRGBA{Pix: pixels.Data, Stride: 4 * pixels.Width, Rect: bounds}, nil
}

// EncodeDDS writes a picture as a DDS in a DXGI format, such as 98 for
// BC7_UNORM. Format 0 is R8G8B8A8_UNORM. With mipmaps, every level down to
// one pixel follows. It returns an error for an empty picture, one past
// Direct3D's limit, or a format dds-go does not write.
func EncodeDDS(picture image.Image, dxgi uint32, mipmaps bool) ([]byte, error) {
	size := picture.Bounds().Size()
	if err := checkSides(size.X, size.Y); err != nil {
		return nil, err
	}

	options := &dds.EncodeOptions{Format: dds.Format(dxgi), Quality: dds.QualityDefault}
	if mipmaps {
		options.MipLevels = dds.AllMipLevels
	}

	var file bytes.Buffer
	if err := dds.Encode(&file, picture, options); err != nil {
		return nil, err
	}

	return file.Bytes(), nil
}

// Resolve finds the image file a script path names, through a lookup of
// relative paths. It tries the path as written, then each other image
// extension, and ignores a leading slash. It returns false for none.
func Resolve(scriptPath string, lookup func(rel string) (path string, ok bool)) (path string, ok bool) {
	scriptPath = strings.TrimPrefix(scriptPath, "/")
	if path, ok = lookup(scriptPath); ok {
		return path, true
	}

	extension := filepath.Ext(scriptPath)
	stem := strings.TrimSuffix(scriptPath, extension)

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

// IsImage reports whether a script path has an image extension.
func IsImage(scriptPath string) bool {
	return slices.ContainsFunc(extensions, func(extension string) bool {
		return strings.EqualFold(filepath.Ext(scriptPath), extension)
	})
}
