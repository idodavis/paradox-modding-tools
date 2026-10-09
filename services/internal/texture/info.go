// info.go reads the header of an image file: its size and format, without decoding a pixel.

package texture

import (
	"encoding/binary"
	"fmt"
	"image"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Facts is an image file's header facts.
type Facts struct {
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Format string `json:"format"` // DXT5, BC7, RGBA8, png, jpeg, TGA 32-bit
	Mips   int    `json:"mips"`   // mip levels a DDS holds; 0 for the others
}

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
	defer file.Close()

	// by magic, not name: the games ship a PNG named .dds. TGA has no magic, so it goes by name
	var magic [4]byte
	io.ReadFull(file, magic[:])
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return Facts{}, err
	}
	switch {
	case string(magic[:]) == "DDS ":
		return ddsInfo(file)
	case strings.EqualFold(filepath.Ext(path), ".tga"):
		return tgaInfo(file)
	}

	cfg, format, err := image.DecodeConfig(file)
	if err != nil {
		return Facts{}, err
	}
	return Facts{Width: cfg.Width, Height: cfg.Height, Format: format}, nil
}

// ddsInfo reads the DDS magic, its 124 byte header, and a DX10 header when the FourCC says DX10.
func ddsInfo(in io.Reader) (Facts, error) {
	var head [148]byte
	n, _ := io.ReadFull(in, head[:])
	if n < 128 || string(head[:4]) != "DDS " {
		return Facts{}, fmt.Errorf("not a DDS file")
	}
	u32 := func(off int) uint32 { return binary.LittleEndian.Uint32(head[off:]) }
	info := Facts{Height: int(u32(12)), Width: int(u32(16)), Mips: int(u32(28))}

	// the pixel format: a FourCC (DX10 for the next header, an older format by number, or
	// letters), else the channels of an uncompressed surface
	flags, fourCC, bits := u32(80), string(head[84:88]), u32(88)
	const hasAlpha, isFourCC, isRGB = 0x1, 0x4, 0x40
	switch {
	case flags&isFourCC != 0 && fourCC == "DX10" && n == 148:
		info.Format = dxgi[u32(128)]
		if info.Format == "" {
			info.Format = fmt.Sprintf("DXGI %d", u32(128))
		}
	case flags&isFourCC != 0 && u32(84) < 256:
		info.Format = d3d[u32(84)]
		if info.Format == "" {
			info.Format = fmt.Sprintf("D3DFMT %d", u32(84))
		}
	case flags&isFourCC != 0:
		info.Format = strings.TrimRight(fourCC, "\x00 ")
	case flags&isRGB != 0 && flags&hasAlpha != 0:
		info.Format = fmt.Sprintf("RGBA%d", bits/4)
	case flags&isRGB != 0 && bits == 32:
		info.Format = "RGBX8"
	case flags&isRGB != 0:
		info.Format = fmt.Sprintf("RGB%d", bits/3)
	default:
		info.Format = fmt.Sprintf("%d-bit", bits)
	}
	return info, nil
}

// tgaInfo reads the 18 byte TGA header.
func tgaInfo(in io.Reader) (Facts, error) {
	var head [18]byte
	if _, err := io.ReadFull(in, head[:]); err != nil {
		return Facts{}, fmt.Errorf("not a TGA file: %w", err)
	}
	width, height := binary.LittleEndian.Uint16(head[12:]), binary.LittleEndian.Uint16(head[14:])
	return Facts{Width: int(width), Height: int(height), Format: fmt.Sprintf("TGA %d-bit", head[16])}, nil
}
