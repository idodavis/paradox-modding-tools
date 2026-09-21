// image.go reads what an image file says about itself in its header: size, pixel format, mip levels. It never decodes pixels, so it is cheap enough to run when a hover card is built and nothing is cached. DDS is read by hand because the standard library has no reader for it and the header is 148 bytes at most; PNG and JPEG go through the standard decoders' config; TGA is an 18 byte header.
package source

import (
	"encoding/binary"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ImageInfo is an image file's header facts.
type ImageInfo struct {
	Width, Height int
	Format        string // DXT5, BC7, RGBA8, png, jpeg, TGA 32-bit
	Mips          int    // mip levels a DDS holds; 0 for the others
}

// d3d names the formats older than DX10 that a DDS writes as a number where a FourCC's letters would be.
var d3d = map[uint32]string{36: "RGBA16", 111: "R16F", 112: "RG16F", 113: "RGBA16F", 114: "R32F", 115: "RG32F", 116: "RGBA32F"}

// dxgi names the DXGI formats the three installs use in a DX10 header.
var dxgi = map[uint32]string{
	2: "RGBA32F", 10: "RGBA16F", 28: "RGBA8", 29: "RGBA8 sRGB", 56: "R16", 61: "R8",
	71: "BC1", 72: "BC1 sRGB", 74: "BC2", 75: "BC2 sRGB", 77: "BC3", 78: "BC3 sRGB", 80: "BC4", 83: "BC5",
	87: "BGRA8", 91: "BGRA8 sRGB", 95: "BC6H", 96: "BC6H", 98: "BC7", 99: "BC7 sRGB",
}

// ReadImageInfo reads the header of the image at path.
func ReadImageInfo(path string) (ImageInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return ImageInfo{}, err
	}
	defer f.Close()

	// by what the file says it is, not by its name: the games ship a PNG named .dds. TGA has no magic, so it goes by name.
	var magic [4]byte
	io.ReadFull(f, magic[:])
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return ImageInfo{}, err
	}
	switch {
	case string(magic[:]) == "DDS ":
		return ddsInfo(f)
	case strings.EqualFold(filepath.Ext(path), ".tga"):
		return tgaInfo(f)
	}

	cfg, format, err := image.DecodeConfig(f)
	if err != nil {
		return ImageInfo{}, err
	}
	return ImageInfo{Width: cfg.Width, Height: cfg.Height, Format: format}, nil
}

// ddsInfo reads the DDS magic, its 124 byte header, and the DX10 header when the pixel format points at one.
func ddsInfo(r io.Reader) (ImageInfo, error) {
	var head [148]byte
	n, _ := io.ReadFull(r, head[:])
	if n < 128 || string(head[:4]) != "DDS " {
		return ImageInfo{}, fmt.Errorf("not a DDS file")
	}
	u32 := func(off int) uint32 { return binary.LittleEndian.Uint32(head[off:]) }
	info := ImageInfo{Height: int(u32(12)), Width: int(u32(16)), Mips: int(u32(28))}

	// the pixel format: a FourCC (DX10 pointing at the next header, an older format by number, or letters), else the channels of an uncompressed surface
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
func tgaInfo(r io.Reader) (ImageInfo, error) {
	var head [18]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return ImageInfo{}, fmt.Errorf("not a TGA file: %w", err)
	}
	width, height := binary.LittleEndian.Uint16(head[12:]), binary.LittleEndian.Uint16(head[14:])
	return ImageInfo{Width: int(width), Height: int(height), Format: fmt.Sprintf("TGA %d-bit", head[16])}, nil
}
