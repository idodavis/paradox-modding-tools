package texture

import (
	"encoding/binary"
	"fmt"
	"image"
	"math/bits"
)

// The flags of a DDS pixel format: the data is named by a FourCC, has color masks, has a luminance mask or has
// only an alpha mask.
const (
	ddsFourCC    = 0x4
	ddsRGB       = 0x40
	ddsLuminance = 0x20000
	ddsAlpha     = 0x2
)

// fourCCs are the DXGI formats the FourCC of a pixel format without a DX10 header stands for.
var fourCCs = map[string]uint32{
	"DXT1": 70, "DXT3": 73, "DXT5": 76, "ATI1": 79, "BC4U": 79, "ATI2": 82, "BC5U": 82,
}

// dxgiCompressions are the block compressions, each by the DXGI formats from its typeless one to its last variant.
var dxgiCompressions = []struct {
	first, last uint32
	compression string
}{
	{70, 72, "BC1"}, {73, 75, "BC2"}, {76, 78, "BC3"}, {79, 81, "BC4"}, {82, 84, "BC5"},
	{94, 96, "BC6H"}, {97, 99, "BC7"},
}

// dxgiMasks are the uncompressed DXGI formats PMT decodes, each as its bits per pixel and then its red, green,
// blue and alpha masks.
var dxgiMasks = map[uint32][5]uint32{
	28: {32, 0xff, 0xff00, 0xff0000, 0xff000000}, // R8G8B8A8_UNORM
	29: {32, 0xff, 0xff00, 0xff0000, 0xff000000}, // R8G8B8A8_UNORM_SRGB
	61: {8, 0xff, 0, 0, 0},                       // R8_UNORM
	65: {8, 0, 0, 0, 0xff},                       // A8_UNORM
	85: {16, 0xf800, 0x7e0, 0x1f, 0},             // B5G6R5_UNORM
	86: {16, 0x7c00, 0x3e0, 0x1f, 0x8000},        // B5G5R5A1_UNORM
	87: {32, 0xff0000, 0xff00, 0xff, 0xff000000}, // B8G8R8A8_UNORM
	88: {32, 0xff0000, 0xff00, 0xff, 0},          // B8G8R8X8_UNORM
	91: {32, 0xff0000, 0xff00, 0xff, 0xff000000}, // B8G8R8A8_UNORM_SRGB
	93: {32, 0xff0000, 0xff00, 0xff, 0},          // B8G8R8X8_UNORM_SRGB
}

// decodeDDS decodes the first mipmap of a DDS with the given header to RGBA. It handles BC1 to BC7 and uncompressed
// data.
func decodeDDS(bytes []byte, header Header) (image.Image, error) {
	data := bytes[128:]
	dxgi, dx10 := lookupDXGI(bytes)
	if dx10 {
		data = bytes[148:]
	}
	switch header.Compression {
	case "BC1":
		return decodeBlocks(data, header, 8, decodeBC1)
	case "BC2":
		return decodeBlocks(data, header, 16, decodeBC2)
	case "BC3":
		return decodeBlocks(data, header, 16, decodeBC3)
	case "BC4":
		return decodeBlocks(data, header, 8, decodeBC4)
	case "BC5":
		return decodeBlocks(data, header, 16, decodeBC5)
	case "BC6H":
		signed := dxgi == 96
		return decodeBlocks(data, header, 16, func(block []byte, pixels *[16][4]uint8) {
			decodeBC6H(block, pixels, signed)
		})
	case "BC7":
		return decodeBlocks(data, header, 16, decodeBC7)
	}
	depth, masks := resolveDDSMasks(bytes)
	return decodeMasked(data, header, depth, masks)
}

// decodeDDSHeader decodes a DDS header, the DX10 extension included. It returns false if the bytes are not a DDS
// that PMT decodes.
func decodeDDSHeader(bytes []byte) (Header, bool) {
	if !hasMagic(bytes, ddsMagic) || len(bytes) < 128 || binary.LittleEndian.Uint32(bytes[4:]) != 124 {
		return Header{}, false
	}
	compression, ok := classifyDDS(bytes)
	if !ok {
		return Header{}, false
	}
	header := Header{
		Format:      "dds",
		Width:       int(binary.LittleEndian.Uint32(bytes[16:])),
		Height:      int(binary.LittleEndian.Uint32(bytes[12:])),
		Mipmaps:     max(1, int(binary.LittleEndian.Uint32(bytes[28:]))),
		Compression: compression,
	}
	return header, true
}

// classifyDDS names the compression a DDS pixel format declares: BC1 to BC7, or "" for uncompressed data read
// through masks. It returns false for a format PMT does not decode.
func classifyDDS(bytes []byte) (string, bool) {
	flags := binary.LittleEndian.Uint32(bytes[80:])
	if flags&ddsFourCC == 0 {
		depth := binary.LittleEndian.Uint32(bytes[88:])
		return "", flags&(ddsRGB|ddsLuminance|ddsAlpha) != 0 && depth%8 == 0 && depth >= 8 && depth <= 32
	}
	dxgi, ok := lookupDXGI(bytes)
	if !ok {
		if dxgi, ok = fourCCs[string(bytes[84:88])]; !ok {
			return "", false
		}
	}
	for _, row := range dxgiCompressions {
		if row.first <= dxgi && dxgi <= row.last {
			return row.compression, true
		}
	}
	_, ok = dxgiMasks[dxgi]
	return "", ok
}

// lookupDXGI returns the DXGI format of a DDS with a DX10 header. It returns false when the pixel format names no
// DX10 header or the bytes end before it.
func lookupDXGI(bytes []byte) (uint32, bool) {
	if binary.LittleEndian.Uint32(bytes[80:])&ddsFourCC == 0 || string(bytes[84:88]) != "DX10" || len(bytes) < 148 {
		return 0, false
	}
	return binary.LittleEndian.Uint32(bytes[128:]), true
}

// describeDDSFormat names the format a DDS header declares, for an error about it: its DXGI format, its FourCC
// as text or as the number a Direct3D 9 format code is, or its bits per pixel.
func describeDDSFormat(bytes []byte) string {
	if dxgi, ok := lookupDXGI(bytes); ok {
		return fmt.Sprintf("DXGI format %d", dxgi)
	}
	if binary.LittleEndian.Uint32(bytes[80:])&ddsFourCC != 0 {
		fourCC := bytes[84:88]
		for _, symbol := range fourCC {
			if symbol < '0' || symbol > 'z' {
				return fmt.Sprintf("format %d", binary.LittleEndian.Uint32(fourCC))
			}
		}
		return fmt.Sprintf("format %q", fourCC)
	}
	return fmt.Sprintf("pixel format of %d bits per pixel", binary.LittleEndian.Uint32(bytes[88:]))
}

// decodeBlocks decodes a block-compressed image one 4 by 4 block at a time, each block through the given decoder.
// The block size is in bytes. A block's pixels past the right or bottom edge are dropped. It returns an error if the
// data ends before the last block.
func decodeBlocks(
	data []byte, header Header, blockBytes int, decodeBlock func(block []byte, pixels *[16][4]uint8),
) (image.Image, error) {
	wide, high := (header.Width+3)/4, (header.Height+3)/4
	if len(data) < wide*high*blockBytes {
		return nil, errTruncated
	}
	nrgba := image.NewNRGBA(image.Rect(0, 0, header.Width, header.Height))
	var pixels [16][4]uint8
	for row := range high {
		for column := range wide {
			decodeBlock(data[(row*wide+column)*blockBytes:], &pixels)
			for i, pixel := range pixels {
				x, y := column*4+i%4, row*4+i/4
				if x < header.Width && y < header.Height {
					copy(nrgba.Pix[nrgba.PixOffset(x, y):], pixel[:])
				}
			}
		}
	}
	return nrgba, nil
}

// decodeBC1 decodes one BC1 block: four colors interpolated from its two, or three and a transparent black when
// the first color is not above the second.
func decodeBC1(block []byte, pixels *[16][4]uint8) {
	palette := resolveBC1Palette(block)
	if binary.LittleEndian.Uint16(block) <= binary.LittleEndian.Uint16(block[2:]) {
		for c := range 3 {
			palette[2][c] = uint8((int(palette[0][c]) + int(palette[1][c]) + 1) / 2)
		}
		palette[3] = [4]uint8{}
	}
	decodeBC1Indices(block[4:], palette, pixels)
}

// resolveBC1Palette builds the four colors of a BC1 color block: its two 5-6-5 colors scaled to bytes and the
// two a third and two thirds of the way between them.
func resolveBC1Palette(block []byte) [4][4]uint8 {
	var palette [4][4]uint8
	for e := range 2 {
		packed := uint32(binary.LittleEndian.Uint16(block[2*e:]))
		palette[e] = [4]uint8{toByte(packed>>11, 5), toByte(packed>>5&63, 6), toByte(packed&31, 5), 255}
	}
	for c := range 3 {
		palette[2][c] = uint8((2*int(palette[0][c]) + int(palette[1][c]) + 1) / 3)
		palette[3][c] = uint8((int(palette[0][c]) + 2*int(palette[1][c]) + 1) / 3)
	}
	palette[2][3], palette[3][3] = 255, 255
	return palette
}

// toByte scales a value of the given bits to a byte, rounding to the nearest.
func toByte(value uint32, width int) uint8 {
	maximum := uint64(1)<<width - 1
	return uint8((uint64(value)*255 + maximum/2) / maximum)
}

// decodeBC1Indices picks each pixel's color from a palette by its two-bit index.
func decodeBC1Indices(indices []byte, palette [4][4]uint8, pixels *[16][4]uint8) {
	for i := range 16 {
		pixels[i] = palette[indices[i/4]>>(2*(i%4))&3]
	}
}

// decodeBC2 decodes one BC2 block: a BC1 color block always in its four-color mode, with four bits of alpha per
// pixel before it.
func decodeBC2(block []byte, pixels *[16][4]uint8) {
	decodeBC1Indices(block[12:], resolveBC1Palette(block[8:]), pixels)
	for i := range 16 {
		pixels[i][3] = (block[i/2] >> (4 * (i % 2)) & 15) * 17
	}
}

// decodeBC3 decodes one BC3 block: a BC1 color block always in its four-color mode, with a BC4 block of alpha
// before it.
func decodeBC3(block []byte, pixels *[16][4]uint8) {
	decodeBC1Indices(block[12:], resolveBC1Palette(block[8:]), pixels)
	for i, alpha := range decodeBC4Channel(block) {
		pixels[i][3] = alpha
	}
}

// decodeBC4Channel decodes one BC4 block to its sixteen values: eight interpolated from the two it stores, or six
// with 0 and 255 when the first is not above the second.
func decodeBC4Channel(block []byte) [16]uint8 {
	first, second := int(block[0]), int(block[1])
	palette := [8]int{first, second}
	if first > second {
		for i := 2; i < 8; i++ {
			palette[i] = ((8-i)*first + (i-1)*second + 3) / 7
		}
	} else {
		for i := 2; i < 6; i++ {
			palette[i] = ((6-i)*first + (i-1)*second + 2) / 5
		}
		palette[6], palette[7] = 0, 255
	}
	indices := binary.LittleEndian.Uint64(block[:8]) >> 16
	var values [16]uint8
	for i := range 16 {
		values[i] = uint8(palette[indices>>(3*i)&7])
	}
	return values
}

// decodeBC4 decodes one BC4 block to its red channel, with green and blue 0 and alpha 255.
func decodeBC4(block []byte, pixels *[16][4]uint8) {
	for i, red := range decodeBC4Channel(block) {
		pixels[i] = [4]uint8{red, 0, 0, 255}
	}
}

// decodeBC5 decodes one BC5 block to its red and green channels, with blue 0 and alpha 255.
func decodeBC5(block []byte, pixels *[16][4]uint8) {
	reds, greens := decodeBC4Channel(block[:8]), decodeBC4Channel(block[8:])
	for i := range 16 {
		pixels[i] = [4]uint8{reds[i], greens[i], 0, 255}
	}
}

// resolveDDSMasks returns the bits per pixel and the red, green, blue and alpha masks of an uncompressed DDS,
// from its DX10 header's format or from its pixel format. A luminance format gets its one mask for red, green
// and blue.
func resolveDDSMasks(bytes []byte) (int, [4]uint32) {
	if dxgi, ok := lookupDXGI(bytes); ok {
		row := dxgiMasks[dxgi]
		return int(row[0]), [4]uint32{row[1], row[2], row[3], row[4]}
	}
	depth := int(binary.LittleEndian.Uint32(bytes[88:]))
	masks := [4]uint32{
		binary.LittleEndian.Uint32(bytes[92:]),
		binary.LittleEndian.Uint32(bytes[96:]),
		binary.LittleEndian.Uint32(bytes[100:]),
		binary.LittleEndian.Uint32(bytes[104:]),
	}
	if binary.LittleEndian.Uint32(bytes[80:])&ddsLuminance != 0 {
		masks[1], masks[2] = masks[0], masks[0]
	}
	return depth, masks
}

// decodeMasked decodes uncompressed pixels of the given bits each, every channel read through its mask and
// scaled to a byte. A channel without a mask is 0, or 255 for alpha. It returns an error if the data ends before
// the last row.
func decodeMasked(data []byte, header Header, depth int, masks [4]uint32) (image.Image, error) {
	size := depth / 8
	stride := header.Width * size
	if len(data) < stride*header.Height {
		return nil, errTruncated
	}
	var shifts, widths [4]int
	for c, mask := range masks {
		shifts[c], widths[c] = bits.TrailingZeros32(mask), bits.OnesCount32(mask)
	}
	nrgba := image.NewNRGBA(image.Rect(0, 0, header.Width, header.Height))
	for y := range header.Height {
		for x := range header.Width {
			var value uint32
			for i := range size {
				value |= uint32(data[y*stride+x*size+i]) << (8 * i)
			}
			pixel := nrgba.Pix[nrgba.PixOffset(x, y):]
			// Alpha is full unless a mask says otherwise.
			pixel[3] = 255
			for c, mask := range masks {
				if mask == 0 {
					continue
				}
				if widths[c] == 8 {
					pixel[c] = uint8(value >> shifts[c])
					continue
				}
				pixel[c] = toByte((value&mask)>>shifts[c], widths[c])
			}
		}
	}
	return nrgba, nil
}
