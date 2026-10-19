// info.go reads the header of an image file once: its size and format for Info, and for Decode where the pixels start
// and how they are laid out, without decoding a pixel.

package texture

import (
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Facts is an image file's header facts.
type Facts struct {
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Format    string `json:"format"` // DXT5, BC7, RGBA8, png, jpeg, TGA 32-bit
	Mips      int    `json:"mips"`   // mip levels a DDS holds; 0 for the others
	container container
}

// container is how an image file is stored, which its own bytes say: the layout Decode reads its pixels from.
type container uint8

// header is an image file's first bytes, read once: the Facts Info gives, and what Decode needs to find and read the
// pixels of a DDS or a TGA.
type header struct {
	Facts
	offset        int       // where the pixels start in the file
	depth         int       // bits a pixel of an uncompressed DDS or a TGA
	masks         [4]uint32 // an uncompressed DDS's channels under their masks: red, green, blue, alpha
	imageType     byte      // a TGA's: 2 and 3 raw, 10 and 11 run-length, 3 and 11 grey
	descriptor    byte      // a TGA's: bit 5 set is a top row first
	isColorMapped bool      // a TGA that holds a color map, which Decode does not draw
}

// The containers: DDS and TGA are decoded here, any other image by the standard library.
const (
	standardContainer container = iota
	ddsContainer
	tgaContainer
)

// The DDS pixel format's flags.
const (
	hasAlpha  = 0x1
	alphaOnly = 0x2
	isFourCC  = 0x4
	isRGB     = 0x40
	luminance = 0x20000
)

// d3d names the pre-DX10 formats a DDS writes as a number in place of a FourCC's letters.
var d3d = map[uint32]string{36: "RGBA16", 111: "R16F", 112: "RG16F", 113: "RGBA16F", 114: "R32F", 115: "RG32F", 116: "RGBA32F"}

// dxgi names the DXGI formats the three games use in a DX10 header.
var dxgi = map[uint32]string{
	2: "RGBA32F", 10: "RGBA16F", 28: "RGBA8", 29: "RGBA8 sRGB", 56: "R16", 61: "R8",
	71: "BC1", 72: "BC1 sRGB", 74: "BC2", 75: "BC2 sRGB", 77: "BC3", 78: "BC3 sRGB", 80: "BC4", 83: "BC5",
	87: "BGRA8", 91: "BGRA8 sRGB", 95: "BC6H", 96: "BC6H", 98: "BC7", 99: "BC7 sRGB",
}

// Info reads the header of the image at path.
func Info(path string) (Facts, error) {
	file, err := os.Open(path)
	if err != nil {
		return Facts{}, err
	}
	defer func() { _ = file.Close() }()
	head, err := parseHeader(file, path)
	return head.Facts, err
}

// parseHeader reads the header of the image source holds, which path names. It goes by the file's magic, not its name,
// because the games ship a PNG named .dds; TGA has no magic, so it goes by name.
func parseHeader(source io.ReadSeeker, path string) (header, error) {
	var magic [4]byte
	_, _ = io.ReadFull(source, magic[:]) // a short file falls through to image.DecodeConfig, which reports it
	if _, err := source.Seek(0, io.SeekStart); err != nil {
		return header{}, err
	}
	switch {
	case string(magic[:]) == "DDS ":
		return ddsHeader(source)
	case strings.EqualFold(filepath.Ext(path), ".tga"):
		return tgaHeader(source)
	}

	cfg, format, err := image.DecodeConfig(source)
	if err != nil {
		return header{}, err
	}
	return header{Facts: Facts{Width: cfg.Width, Height: cfg.Height, Format: format}}, nil
}

// ddsHeader reads the DDS magic, its 124 byte header, and a DX10 header when the FourCC says DX10.
func ddsHeader(source io.Reader) (header, error) {
	var head [148]byte
	n, _ := io.ReadFull(source, head[:])
	if n < 128 || string(head[:4]) != "DDS " {
		return header{}, errors.New("not a DDS file")
	}
	dx10 := u32(head[:], 80)&isFourCC != 0 && string(head[84:88]) == "DX10" && n == 148
	surface := header{
		Facts:  Facts{Height: int(u32(head[:], 12)), Width: int(u32(head[:], 16)), Mips: int(u32(head[:], 28)), container: ddsContainer},
		offset: 128,
	}
	if dx10 {
		surface.offset = 148
	}
	surface.Format = ddsFormat(head[:], dx10)
	surface.depth, surface.masks = ddsChannels(surface.Format, head[:], dx10)
	return surface, nil
}

// ddsFormat is the name of a DDS's pixel format: a DX10 header's DXGI format, a FourCC (an older format by number, or
// letters), else the channels of an uncompressed surface.
func ddsFormat(head []byte, dx10 bool) string {
	flags, bits := u32(head, 80), u32(head, 88)
	switch {
	case dx10:
		if format := dxgi[u32(head, 128)]; format != "" {
			return format
		}
		return fmt.Sprintf("DXGI %d", u32(head, 128))
	case flags&isFourCC != 0 && u32(head, 84) < 256:
		if format := d3d[u32(head, 84)]; format != "" {
			return format
		}
		return fmt.Sprintf("D3DFMT %d", u32(head, 84))
	case flags&isFourCC != 0:
		return strings.TrimRight(string(head[84:88]), "\x00 ")
	case flags&isRGB != 0 && flags&hasAlpha != 0:
		return fmt.Sprintf("RGBA%d", bits/4)
	case flags&isRGB != 0 && bits == 32:
		return "RGBX8"
	case flags&isRGB != 0:
		return fmt.Sprintf("RGB%d", bits/3)
	}
	return fmt.Sprintf("%d-bit", bits)
}

// ddsChannels is an uncompressed surface's bits a pixel and its channel masks (red, green, blue, alpha), read from the
// header: alpha only where its flags say so and a luminance's grey in all three; a DX10 surface's by its byte order.
func ddsChannels(format string, head []byte, dx10 bool) (depth int, masks [4]uint32) {
	flags := u32(head, 80)
	depth, masks = int(u32(head, 88)), [4]uint32{u32(head, 92), u32(head, 96), u32(head, 100), u32(head, 104)}
	switch {
	case dx10 && strings.HasPrefix(format, "RGBA8"):
		depth, masks = 32, [4]uint32{0xff, 0xff00, 0xff0000, 0xff000000}
	case dx10 && strings.HasPrefix(format, "BGRA8"):
		depth, masks = 32, [4]uint32{0xff0000, 0xff00, 0xff, 0xff000000}
	case dx10:
		depth = 0
	case flags&luminance != 0:
		masks[1], masks[2] = masks[0], masks[0]
	}
	if !dx10 && flags&(hasAlpha|alphaOnly) == 0 {
		masks[3] = 0
	}
	return depth, masks
}

// tgaHeader reads the 18 byte TGA header.
func tgaHeader(source io.Reader) (header, error) {
	var head [18]byte
	if _, err := io.ReadFull(source, head[:]); err != nil {
		return header{}, fmt.Errorf("not a TGA file: %w", err)
	}
	width, height := binary.LittleEndian.Uint16(head[12:]), binary.LittleEndian.Uint16(head[14:])
	return header{
		Facts:  Facts{Width: int(width), Height: int(height), Format: fmt.Sprintf("TGA %d-bit", head[16]), container: tgaContainer},
		offset: 18 + int(head[0]), depth: int(head[16]), imageType: head[2], descriptor: head[17], isColorMapped: head[1] != 0,
	}, nil
}

// u32 is the little-endian number at offset of a header.
func u32(head []byte, offset int) uint32 {
	return binary.LittleEndian.Uint32(head[offset:])
}

// Label is the format as the image panel names it: DDS BC7, TGA 32-bit, PNG; "" for an image with no header facts.
func (facts Facts) Label() string {
	switch facts.container {
	case ddsContainer:
		return "DDS " + facts.Format
	case tgaContainer:
		return facts.Format
	}
	return strings.ToUpper(facts.Format)
}

// NeedsDecoding is whether a browser cannot draw the image: its header is a DDS's or a TGA's.
func (facts Facts) NeedsDecoding() bool {
	return facts.container != standardContainer
}
