package texture

import (
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"golang.org/x/sync/errgroup"
)

// fixtures is the folder of the test install's images.
const fixtures = "../testdata/ck3/game/gfx"

// tgaFooter ends a TGA written with the format's footer.
const tgaFooter = "TRUEVISION-XFILE.\x00"

// fixtureHeader is the header of every DDS fixture, one BC1 block, and of the simplest cases.
var fixtureHeader = buildDDSHeader(4, 4, 1, "BC1")

// The colors the cases are drawn in.
var (
	black       = [4]uint8{0, 0, 0, 255}
	red         = [4]uint8{255, 0, 0, 255}
	green       = [4]uint8{0, 255, 0, 255}
	blue        = [4]uint8{0, 0, 255, 255}
	white       = [4]uint8{255, 255, 255, 255}
	transparent = [4]uint8{}
)

// bc1FourColors is a BC1 block of red above blue, its rows at each index in turn.
var bc1FourColors = []byte{0x00, 0xF8, 0x1F, 0x00, 0x00, 0x55, 0xAA, 0xFF}

// bc1ThreeColors is a BC1 block of blue not above red, its rows at each index in turn.
var bc1ThreeColors = []byte{0x1F, 0x00, 0x00, 0xF8, 0x00, 0x55, 0xAA, 0xFF}

// bc4Eight is a BC4 block of 255 above 0 with each pixel's index its position in its half.
var bc4Eight = []byte{0xFF, 0x00, 0x88, 0xC6, 0xFA, 0x88, 0xC6, 0xFA}

// bc4Six is a BC4 block of 0 not above 255 with the same indices.
var bc4Six = []byte{0x00, 0xFF, 0x88, 0xC6, 0xFA, 0x88, 0xC6, 0xFA}

// bc4EightValues and bc4SixValues are what the two BC4 blocks decode to over one half of their pixels.
var (
	bc4EightValues = []uint8{255, 0, 219, 182, 146, 109, 73, 36}
	bc4SixValues   = []uint8{0, 255, 51, 102, 153, 204, 0, 255}
)

// ramp is the index fields of a block whose pixel i has index i, pixel 0 the anchor.
var ramp = [][2]int{
	{0, 3}, {1, 4}, {2, 4}, {3, 4}, {4, 4}, {5, 4}, {6, 4}, {7, 4},
	{8, 4}, {9, 4}, {10, 4}, {11, 4}, {12, 4}, {13, 4}, {14, 4}, {15, 4},
}

// bc7Mode6 is a BC7 mode 6 block fading from red to blue over the ramp, with both P-bits set.
var bc7Mode6 = encodeBlock(slices.Concat([][2]int{
	{64, 7}, {127, 7}, {0, 7}, {0, 7}, {0, 7}, {0, 7}, {127, 7}, {127, 7}, {127, 7}, {1, 1}, {1, 1},
}, ramp))

// bc7Fade is what bc7Mode6 decodes to.
var bc7Fade = [][4]uint8{
	{255, 1, 1, 255}, {239, 1, 17, 255}, {219, 1, 37, 255}, {203, 1, 53, 255},
	{188, 1, 68, 255}, {172, 1, 84, 255}, {152, 1, 104, 255}, {136, 1, 120, 255},
	{120, 1, 136, 255}, {104, 1, 152, 255}, {84, 1, 172, 255}, {68, 1, 188, 255},
	{53, 1, 203, 255}, {37, 1, 219, 255}, {17, 1, 239, 255}, {1, 1, 255, 255},
}

// bc7Mode1 is a BC7 mode 1 block in partition 0: the left half red to green with its P-bit set, the right half
// blue to white without, pixel 0 at index 2 and the anchor pixel 15 at index 3.
var bc7Mode1 = encodeBlock([][2]int{
	{2, 2}, {0, 6},
	{63, 6}, {0, 6}, {0, 6}, {63, 6},
	{0, 6}, {63, 6}, {0, 6}, {63, 6},
	{0, 6}, {0, 6}, {63, 6}, {63, 6},
	{1, 2},
	{2, 2}, {0, 3}, {0, 3}, {0, 3}, {0, 3}, {0, 3}, {0, 3}, {0, 3},
	{0, 3}, {0, 3}, {0, 3}, {0, 3}, {0, 3}, {0, 3}, {0, 3}, {3, 2},
})

// bc7Halves is what bc7Mode1 decodes to.
var bc7Halves = [][4]uint8{
	{184, 73, 2, 255}, {255, 2, 2, 255}, {0, 0, 253, 255}, {0, 0, 253, 255},
	{255, 2, 2, 255}, {255, 2, 2, 255}, {0, 0, 253, 255}, {0, 0, 253, 255},
	{255, 2, 2, 255}, {255, 2, 2, 255}, {0, 0, 253, 255}, {0, 0, 253, 255},
	{255, 2, 2, 255}, {255, 2, 2, 255}, {0, 0, 253, 255}, {107, 107, 253, 255},
}

// bc6hMode3 is a BC6H mode 3 block, ten bits per endpoint and no deltas, from full red to full green over the
// ramp.
var bc6hMode3 = encodeBlock(slices.Concat([][2]int{
	{3, 5}, {1023, 10}, {0, 10}, {0, 10}, {0, 10}, {1023, 10}, {0, 10},
}, ramp))

// bc6hFade is what bc6hMode3 decodes to: the half floats past 1 clamp, so most of the ramp is full.
var bc6hFade = [][4]uint8{
	{255, 0, 0, 255}, {255, 0, 0, 255}, {255, 0, 0, 255}, {255, 1, 0, 255},
	{255, 2, 0, 255}, {255, 9, 0, 255}, {255, 51, 0, 255}, {255, 195, 0, 255},
	{195, 255, 0, 255}, {51, 255, 0, 255}, {9, 255, 0, 255}, {2, 255, 0, 255},
	{1, 255, 0, 255}, {0, 255, 0, 255}, {0, 255, 0, 255}, {0, 255, 0, 255},
}

// bc6hMode15 is a BC6H mode 15 block, sixteen bits per endpoint with four-bit deltas and the high bits stored
// reversed: green 0x739D, which is one half, and blue full, its delta -1; the second half of the pixels at the
// delta endpoint.
var bc6hMode15 = encodeBlock([][2]int{
	{15, 5}, {0, 10}, {925, 10}, {1023, 10}, {0, 4}, {0, 6}, {0, 4}, {14, 6}, {15, 4}, {63, 6},
	{0, 3}, {0, 4}, {0, 4}, {0, 4}, {0, 4}, {0, 4}, {0, 4}, {0, 4},
	{15, 4}, {15, 4}, {15, 4}, {15, 4}, {15, 4}, {15, 4}, {15, 4}, {15, 4},
})

// rgba8 is four pixels written as bytes of red, green, blue and alpha.
var rgba8 = []byte{255, 0, 0, 255, 0, 255, 0, 128, 0, 0, 255, 0, 255, 255, 255, 255}

// rgba8Colors is what rgba8 decodes to.
var rgba8Colors = [][4]uint8{red, {0, 255, 0, 128}, {0, 0, 255, 0}, white}

// ddsCases are the DDS files the decoder is tried on, each a header, a pixel format with any DX10 header after
// it, its data, and the pixels it decodes to.
var ddsCases = []struct {
	name   string
	header Header
	format []byte
	data   []byte
	pixels [][4]uint8
}{
	{"BC1 four colors", fixtureHeader, encodeFourCC("DXT1"), bc1FourColors,
		buildRows(red, blue, [4]uint8{170, 0, 85, 255}, [4]uint8{85, 0, 170, 255})},
	{"BC1 three colors", fixtureHeader, encodeFourCC("DXT1"), bc1ThreeColors,
		buildRows(blue, red, [4]uint8{128, 0, 128, 255}, transparent)},
	{"BC1 past the edge", buildDDSHeader(5, 3, 1, "BC1"), encodeFourCC("DXT1"),
		slices.Concat(bc1FourColors, make([]byte, 8)),
		[][4]uint8{red, red, red, red, black, blue, blue, blue, blue, black,
			{170, 0, 85, 255}, {170, 0, 85, 255}, {170, 0, 85, 255}, {170, 0, 85, 255}, black}},
	{"BC1 with mipmaps", buildDDSHeader(4, 4, 3, "BC1"), encodeFourCC("DXT1"),
		slices.Concat(bc1FourColors, make([]byte, 16)),
		buildRows(red, blue, [4]uint8{170, 0, 85, 255}, [4]uint8{85, 0, 170, 255})},
	{"BC2", buildDDSHeader(4, 4, 1, "BC2"), encodeFourCC("DXT3"),
		slices.Concat([]byte{0x00, 0x00, 0x55, 0x55, 0xAA, 0xAA, 0xFF, 0xFF}, bc1ThreeColors),
		buildRows([4]uint8{0, 0, 255, 0}, [4]uint8{255, 0, 0, 85}, [4]uint8{85, 0, 170, 170}, [4]uint8{170, 0, 85, 255})},
	{"BC3", buildDDSHeader(4, 4, 1, "BC3"), encodeFourCC("DXT5"), slices.Concat(bc4Eight, bc1ThreeColors),
		buildAlphas(buildRows(blue, red, [4]uint8{85, 0, 170, 255}, [4]uint8{170, 0, 85, 255}), bc4EightValues)},
	{"BC4 eight values", buildDDSHeader(4, 4, 1, "BC4"), encodeFourCC("ATI1"), bc4Eight,
		buildChannels(bc4EightValues, nil)},
	{"BC4 six values", buildDDSHeader(4, 4, 1, "BC4"), encodeFourCC("BC4U"), bc4Six,
		buildChannels(bc4SixValues, nil)},
	{"BC5", buildDDSHeader(4, 4, 1, "BC5"), encodeFourCC("ATI2"), slices.Concat(bc4Eight, bc4Six),
		buildChannels(bc4EightValues, bc4SixValues)},
	{"BC6H mode 3", buildDDSHeader(4, 4, 1, "BC6H"), encodeDX10(95), bc6hMode3, bc6hFade},
	{"BC6H mode 15", buildDDSHeader(4, 4, 1, "BC6H"), encodeDX10(95), bc6hMode15,
		slices.Repeat([][4]uint8{{0, 128, 255, 255}}, 16)},
	{"BC7 mode 6", buildDDSHeader(4, 4, 1, "BC7"), encodeDX10(98), bc7Mode6, bc7Fade},
	{"BC7 mode 1", buildDDSHeader(4, 4, 1, "BC7"), encodeDX10(98), bc7Mode1, bc7Halves},
	{"RGBA8", buildDDSHeader(2, 2, 1, ""),
		encodePixelFormat(ddsRGB|1, "", 32, [4]uint32{0xff, 0xff00, 0xff0000, 0xff000000}), rgba8, rgba8Colors},
	{"BGRA8", buildDDSHeader(2, 2, 1, ""),
		encodePixelFormat(ddsRGB|1, "", 32, [4]uint32{0xff0000, 0xff00, 0xff, 0xff000000}),
		[]byte{0, 0, 255, 255, 0, 255, 0, 128, 255, 0, 0, 0, 255, 255, 255, 255}, rgba8Colors},
	{"RGB8", buildDDSHeader(2, 2, 1, ""), encodePixelFormat(ddsRGB, "", 24, [4]uint32{0xff0000, 0xff00, 0xff, 0}),
		[]byte{0, 0, 255, 0, 255, 0, 255, 0, 0, 255, 255, 255}, [][4]uint8{red, green, blue, white}},
	{"A8", buildDDSHeader(2, 2, 1, ""), encodePixelFormat(ddsAlpha, "", 8, [4]uint32{0, 0, 0, 0xff}),
		[]byte{0, 128, 255, 64}, [][4]uint8{{0, 0, 0, 0}, {0, 0, 0, 128}, {0, 0, 0, 255}, {0, 0, 0, 64}}},
	{"L8", buildDDSHeader(2, 2, 1, ""), encodePixelFormat(ddsLuminance, "", 8, [4]uint32{0xff, 0, 0, 0}),
		[]byte{0, 128, 255, 64}, [][4]uint8{black, {128, 128, 128, 255}, white, {64, 64, 64, 255}}},
	{"R5G6B5", buildDDSHeader(2, 2, 1, ""), encodePixelFormat(ddsRGB, "", 16, [4]uint32{0xf800, 0x7e0, 0x1f, 0}),
		[]byte{0x00, 0xF8, 0xE0, 0x07, 0x1F, 0x00, 0x10, 0x84}, [][4]uint8{red, green, blue, {132, 130, 132, 255}}},
	{"RGBA8 with a DX10 header", buildDDSHeader(2, 2, 1, ""), encodeDX10(28), rgba8, rgba8Colors},
}

// tgaCases are the TGA files the decoder is tried on, each its size, its bytes and the pixels it decodes to.
var tgaCases = []struct {
	name          string
	width, height int
	file          []byte
	pixels        [][4]uint8
}{
	{"raw 24-bit from the bottom", 2, 2,
		[]byte{0, 0, 2, 0, 0, 0, 0, 0, 0, 0, 0, 0, 2, 0, 2, 0, 24, 0,
			0, 0, 255, 0, 255, 0, 255, 0, 0, 255, 255, 255},
		[][4]uint8{blue, white, red, green}},
	{"run-length 32-bit from the top with a footer", 2, 2,
		slices.Concat([]byte{0, 0, 10, 0, 0, 0, 0, 0, 0, 0, 0, 0, 2, 0, 2, 0, 32, 0x28,
			0x81, 0, 0, 255, 255, 0x01, 0, 255, 0, 128, 255, 0, 0, 0}, make([]byte, 8), []byte(tgaFooter)),
		[][4]uint8{red, red, {0, 255, 0, 128}, {0, 0, 255, 0}}},
	{"color-mapped 8-bit", 2, 1,
		[]byte{0, 1, 1, 0, 0, 2, 0, 24, 0, 0, 0, 0, 2, 0, 1, 0, 8, 0x20, 0, 0, 255, 255, 0, 0, 0, 1},
		[][4]uint8{red, blue}},
	{"run-length color-mapped 8-bit", 2, 1,
		[]byte{0, 1, 9, 0, 0, 2, 0, 24, 0, 0, 0, 0, 2, 0, 1, 0, 8, 0x20, 0, 0, 255, 255, 0, 0, 0x81, 1},
		[][4]uint8{blue, blue}},
	{"gray 8-bit", 2, 1,
		[]byte{0, 0, 3, 0, 0, 0, 0, 0, 0, 0, 0, 0, 2, 0, 1, 0, 8, 0x20, 0, 200},
		[][4]uint8{black, {200, 200, 200, 255}}},
	{"16-bit from the right", 2, 1,
		[]byte{0, 0, 2, 0, 0, 0, 0, 0, 0, 0, 0, 0, 2, 0, 1, 0, 16, 0x30, 0x00, 0x7C, 0xE0, 0x03},
		[][4]uint8{green, red}},
	{"with an id", 1, 1,
		[]byte{3, 0, 2, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 1, 0, 24, 0x20, 'a', 'b', 'c', 255, 255, 255},
		[][4]uint8{white}},
}

// headerCases are bytes the sniff is tried on, with the header it reads or false. The TGA headers are copies of
// the first case's, each spoiled in one field.
var headerCases = []struct {
	name   string
	bytes  []byte
	header Header
	ok     bool
}{
	{"nothing", nil, Header{}, false},
	{"text", []byte("namespace = test_events\n\n"), Header{}, false},
	{"DDS cut short", []byte("DDS \n"), Header{}, false},
	{"PNG cut short", []byte(pngMagic + "\x00\x00\x00\x0dIHDR"), Header{}, false},
	{"TGA of a type PMT does not decode", buildSpoiledHeader(2, 7), Header{}, false},
	{"TGA of a depth PMT does not decode", buildSpoiledHeader(16, 12), Header{}, false},
	{"TGA without a width", buildSpoiledHeader(12, 0), Header{}, false},
	{"TGA by its footer alone", slices.Concat(buildSpoiledHeader(2, 7), make([]byte, 8), []byte(tgaFooter)),
		Header{}, false},
}

// decodeCases are bytes the decoder refuses, with the words of its error.
var decodeCases = []struct {
	name  string
	bytes []byte
	words string
}{
	{"text", []byte("namespace = test_events\n\n"), "not a DDS, TGA or PNG"},
	{"DDS cut short", []byte("DDS \n"), errTruncated.Error()},
	{"DDS cut inside its DX10 header", encodeDDS(fixtureHeader, encodeDX10(98), nil)[:140], errTruncated.Error()},
	{"DDS of a FourCC PMT does not decode", encodeDDS(fixtureHeader, encodeFourCC("DXT2"), nil),
		`format "DXT2"`},
	{"DDS of a Direct3D 9 format code", encodeDDS(fixtureHeader, encodeFourCC("q\x00\x00\x00"), nil),
		"format 113"},
	{"DDS of a DXGI format PMT does not decode", encodeDDS(fixtureHeader, encodeDX10(10), nil),
		"DXGI format 10"},
	{"DDS of a depth PMT does not decode",
		encodeDDS(fixtureHeader, encodePixelFormat(ddsRGB, "", 48, [4]uint32{}), nil), "48 bits"},
	{"DDS cut before its last block",
		encodeDDS(fixtureHeader, encodeFourCC("DXT1"), bc1FourColors[:4]), errTruncated.Error()},
	{"DDS past the limit on a side",
		encodeDDS(buildDDSHeader(20000, 4, 1, "BC1"), encodeFourCC("DXT1"), nil), "past the 16384 limit"},
	{"TGA past the limit on a side",
		[]byte{0, 0, 2, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xFF, 0xFF, 0xFF, 0xFF, 24, 0x20, 7, 7, 7}, "past the 16384 limit"},
	{"TGA of depth 0 with a footer",
		slices.Concat([]byte{0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 1, 0, 0, 0}, make([]byte, 8), []byte(tgaFooter)),
		"not a DDS, TGA or PNG"},
	{"TGA cut before its last pixel", tgaCases[0].file[:24], errTruncated.Error()},
	{"TGA cut inside a packet", tgaCases[1].file[:21], errTruncated.Error()},
	{"TGA truecolor of 8 bits", []byte{0, 0, 2, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 1, 0, 8, 0x20, 7}, "not supported"},
}

// encodeDDS writes a DDS file in memory: the header for the given one, the given pixel format with any DX10
// header it carries, then the data.
func encodeDDS(header Header, pixelFormat, data []byte) []byte {
	file := make([]byte, 128)
	copy(file, ddsMagic)
	binary.LittleEndian.PutUint32(file[4:], 124)
	// The flags for caps, height, width, the pixel format and the mipmap count.
	binary.LittleEndian.PutUint32(file[8:], 0x21007)
	binary.LittleEndian.PutUint32(file[12:], uint32(header.Height))
	binary.LittleEndian.PutUint32(file[16:], uint32(header.Width))
	binary.LittleEndian.PutUint32(file[28:], uint32(header.Mipmaps))
	copy(file[76:], pixelFormat[:32])
	return slices.Concat(file, pixelFormat[32:], data)
}

// buildDDSHeader is the header of a DDS of the given size, mipmap count and compression.
func buildDDSHeader(width, height, mipmaps int, compression string) Header {
	return Header{Format: "dds", Width: width, Height: height, Mipmaps: mipmaps, Compression: compression}
}

// encodePixelFormat writes a DDS pixel format: its flags, its FourCC, its bits per pixel and its masks.
func encodePixelFormat(flags uint32, fourCC string, depth uint32, masks [4]uint32) []byte {
	format := make([]byte, 32)
	binary.LittleEndian.PutUint32(format, 32)
	binary.LittleEndian.PutUint32(format[4:], flags)
	copy(format[8:], fourCC)
	binary.LittleEndian.PutUint32(format[12:], depth)
	for i, mask := range masks {
		binary.LittleEndian.PutUint32(format[16+4*i:], mask)
	}
	return format
}

// encodeFourCC writes the pixel format of a DDS named by a FourCC.
func encodeFourCC(fourCC string) []byte {
	return encodePixelFormat(ddsFourCC, fourCC, 0, [4]uint32{})
}

// encodeDX10 writes the pixel format of a DDS with a DX10 header, then the header naming the given DXGI format.
func encodeDX10(dxgi uint32) []byte {
	header := make([]byte, 20)
	binary.LittleEndian.PutUint32(header, dxgi)
	binary.LittleEndian.PutUint32(header[4:], 3)
	binary.LittleEndian.PutUint32(header[12:], 1)
	return slices.Concat(encodeFourCC("DX10"), header)
}

// encodeBlock packs fields into a 16-byte block, the lowest bit first, each field its value and then its bits.
func encodeBlock(fields [][2]int) []byte {
	block := make([]byte, 16)
	position := 0
	for _, field := range fields {
		for i := range field[1] {
			block[position/8] |= byte(field[0]>>i&1) << (position % 8)
			position++
		}
	}
	return block
}

// buildSpoiledHeader copies the header of the first TGA case with one byte changed.
func buildSpoiledHeader(offset int, value byte) []byte {
	header := slices.Clone(tgaCases[0].file[:18])
	header[offset] = value
	return header
}

// buildRows lists the pixels of a block whose rows are each one of the given colors.
func buildRows(colors ...[4]uint8) [][4]uint8 {
	var pixels [][4]uint8
	for _, color := range colors {
		pixels = append(pixels, color, color, color, color)
	}
	return pixels
}

// buildAlphas gives pixels the given alphas, repeated over them.
func buildAlphas(pixels [][4]uint8, alphas []uint8) [][4]uint8 {
	for i := range pixels {
		pixels[i][3] = alphas[i%len(alphas)]
	}
	return pixels
}

// buildChannels lists sixteen pixels with the given reds and greens, repeated over them, no blue and full alpha.
// Nil greens are 0.
func buildChannels(reds, greens []uint8) [][4]uint8 {
	pixels := make([][4]uint8, 16)
	for i := range pixels {
		pixels[i] = [4]uint8{reds[i%len(reds)], 0, 0, 255}
		if greens != nil {
			pixels[i][1] = greens[i%len(greens)]
		}
	}
	return pixels
}

// comparePixels checks a decoded image against the pixels it should hold, in row order, each channel within one
// unit.
func comparePixels(t *testing.T, name string, decoded image.Image, pixels [][4]uint8) {
	t.Helper()
	width, height := decoded.Bounds().Dx(), decoded.Bounds().Dy()
	if width*height != len(pixels) {
		t.Errorf("%s: decoded %d by %d, want %d pixels", name, width, height, len(pixels))
		return
	}
	for i, pixel := range pixels {
		x, y := i%width, i/width
		nrgba := color.NRGBAModel.Convert(decoded.At(x, y)).(color.NRGBA)
		have := [4]uint8{nrgba.R, nrgba.G, nrgba.B, nrgba.A}
		for c := range 4 {
			if max(have[c], pixel[c])-min(have[c], pixel[c]) > 1 {
				t.Errorf("%s: pixel %d,%d is %v, want %v", name, x, y, have, pixel)
				break
			}
		}
	}
}

func TestDecodeDDS(t *testing.T) {
	for _, row := range ddsCases {
		file := encodeDDS(row.header, row.format, row.data)
		if header, ok := decodeHeader(file); !ok || header != row.header {
			t.Errorf("%s: header %+v %v, want %+v", row.name, header, ok, row.header)
		}
		decoded, err := Decode(file)
		if err != nil {
			t.Errorf("%s: %v", row.name, err)
			continue
		}
		comparePixels(t, row.name, decoded, row.pixels)
	}
}

func TestDecodeTGA(t *testing.T) {
	for _, row := range tgaCases {
		want := Header{Format: "tga", Width: row.width, Height: row.height, Mipmaps: 1}
		if header, ok := decodeHeader(row.file); !ok || header != want {
			t.Errorf("%s: header %+v %v, want %+v", row.name, header, ok, want)
		}
		decoded, err := Decode(row.file)
		if err != nil {
			t.Errorf("%s: %v", row.name, err)
			continue
		}
		comparePixels(t, row.name, decoded, row.pixels)
	}
}

func TestDecodeFixtures(t *testing.T) {
	count := 0
	err := filepath.WalkDir(fixtures, func(path string, entry os.DirEntry, err error) error {
		if err != nil || filepath.Ext(path) != ".dds" {
			return err
		}
		count++
		file, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if header, ok := decodeHeader(file); !ok || header != fixtureHeader {
			t.Errorf("%s: header %+v %v", path, header, ok)
		}
		decoded, err := Decode(file)
		if err != nil {
			return err
		}
		comparePixels(t, path, decoded, slices.Repeat([][4]uint8{black}, 16))
		return nil
	})
	if err != nil || count == 0 {
		t.Fatalf("%d DDS fixtures, %v", count, err)
	}
	png := filepath.Join(fixtures, "interface", "illustrations", "v_illustration.png")
	file, err := os.ReadFile(png)
	if err != nil {
		t.Fatal(err)
	}
	if header, ok := decodeHeader(file); !ok || header != (Header{"png", 2, 2, 1, ""}) {
		t.Errorf("%s: header %+v %v", png, header, ok)
	}
	decoded, err := Decode(file)
	if err != nil {
		t.Fatal(err)
	}
	comparePixels(t, png, decoded, slices.Repeat([][4]uint8{black}, 4))
}

func TestDecodeHeader(t *testing.T) {
	for _, row := range headerCases {
		if header, ok := decodeHeader(row.bytes); ok != row.ok || header != row.header {
			t.Errorf("%s: %+v %v, want %+v %v", row.name, header, ok, row.header, row.ok)
		}
	}
}

func TestDecodeErrors(t *testing.T) {
	for _, row := range decodeCases {
		decoded, err := Decode(row.bytes)
		if err == nil || !strings.Contains(err.Error(), row.words) {
			t.Errorf("%s: %v %v, want an error saying %q", row.name, decoded, err, row.words)
		}
	}
}

func TestReadHeader(t *testing.T) {
	header, err := ReadHeader(filepath.Join(fixtures, "interface", "v_bg.dds"))
	if err != nil || header != fixtureHeader {
		t.Errorf("v_bg.dds: %+v %v", header, err)
	}
	header, err = ReadHeader(filepath.Join(fixtures, "interface", "illustrations", "v_illustration.png"))
	if err != nil || header != (Header{"png", 2, 2, 1, ""}) {
		t.Errorf("v_illustration.png: %+v %v", header, err)
	}
	if _, err = ReadHeader("../testdata/mods/ck3/gfx/interface/icons/traits/bold.dds"); err == nil {
		t.Error("bold.dds: no error for a DDS cut short")
	}
	if _, err = ReadHeader(filepath.Join(fixtures, "missing.dds")); err == nil {
		t.Error("missing.dds: no error for a missing file")
	}
}

func TestResolvePath(t *testing.T) {
	files := []string{"gfx/a.dds", "gfx/b.png", "gfx/b.tga", "gfx/c.tga", "d.dds"}
	lookup := func(rel string) (string, bool) {
		return "/game/" + rel, slices.Contains(files, strings.ToLower(rel))
	}
	for written, want := range map[string]string{
		"gfx/a.dds": "/game/gfx/a.dds", "gfx/b.dds": "/game/gfx/b.png", "gfx/b.tga": "/game/gfx/b.tga",
		"gfx/c": "/game/gfx/c.tga", "gfx/c.dds": "/game/gfx/c.tga", "d.DDS": "/game/d.DDS", "d.png": "/game/d.dds",
		"gfx/e.dds": "", "gfx/b.jpg": "/game/gfx/b.png",
	} {
		if path, ok := Resolve(written, lookup); path != want || ok != (want != "") {
			t.Errorf("%s: %q %v, want %q", written, path, ok, want)
		}
	}
}

// TestDecodeInstall decodes every DDS and TGA of each real install. A file of a format PMT does not know is listed
// by path and counted; a file whose header PMT reads but whose pixels it cannot decode fails the test. It runs
// only where PMT_TEST_INSTALL_CK3, _VIC3 or _EU5 names the install.
func TestDecodeInstall(t *testing.T) {
	if testing.Short() {
		t.Skip("real installs are skipped under -short")
	}
	for _, game := range []string{"CK3", "VIC3", "EU5"} {
		t.Run(game, func(t *testing.T) {
			decodeInstall(t, game)
		})
	}
}

// decodeInstall decodes every image of one game's install in parallel and reports what is refused, or skips
// when the game's variable is not set.
func decodeInstall(t *testing.T, game string) {
	root := os.Getenv("PMT_TEST_INSTALL_" + game)
	if root == "" {
		t.Skipf("PMT_TEST_INSTALL_%s is not set", game)
	}
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if extension := strings.ToLower(filepath.Ext(path)); extension == ".dds" || extension == ".tga" {
			paths = append(paths, path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	var group errgroup.Group
	var mutex sync.Mutex
	unknown, failed := 0, 0
	group.SetLimit(runtime.NumCPU())
	for _, path := range paths {
		group.Go(func() error {
			known, err := decodeFile(path)
			if err == nil {
				return nil
			}
			mutex.Lock()
			defer mutex.Unlock()
			if !known {
				unknown++
				t.Logf("%s: %v", path, err)
				return nil
			}
			failed++
			t.Errorf("%s: %v", path, err)
			return nil
		})
	}
	_ = group.Wait()
	t.Logf("%s: %d images, %d of formats PMT does not know, %d failed", game, len(paths), unknown, failed)
}

// decodeFile decodes one image and checks its size against the header read from its first bytes. It returns
// whether PMT knows the file's format, and the error if the file does not decode or the sizes disagree.
func decodeFile(path string) (bool, error) {
	file, err := os.ReadFile(path)
	if err != nil {
		return true, err
	}
	decoded, err := Decode(file)
	if err != nil {
		_, known := decodeHeader(file)
		return known, err
	}
	header, err := ReadHeader(path)
	if err != nil {
		return true, err
	}
	if size := decoded.Bounds().Size(); size.X != header.Width || size.Y != header.Height {
		return true, fmt.Errorf("decoded %d by %d, header says %d by %d", size.X, size.Y, header.Width, header.Height)
	}
	return true, nil
}
