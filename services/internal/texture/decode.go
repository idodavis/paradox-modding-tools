// decode.go decodes the games' images a browser cannot draw: DDS (BC1, BC2, BC3, BC7, and an uncompressed surface by
// its channel masks) and TGA (true-color or grey, raw or run-length). BC7's tables are its spec's
// (ARB_texture_compression_bptc).

package texture

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	_ "image/png"
	"math/bits"
	"os"
	"path/filepath"
	"strings"
)

// blockFormat is a block-compressed format: the bytes of each 4×4 block, and its decoder.
type blockFormat struct {
	size   int
	decode func(block []byte, out *[16]color.NRGBA)
}

// bitReader is the bits of one BC7 block, read lowest bit first from a cursor.
type bitReader struct {
	block [17]byte // the block and a spare byte past its end, which a take across the last byte boundary touches
	pos   int      // the cursor, in bits
}

// bc7Mode is each BC7 mode's subsets, partition, rotation and index-selection bits, color and alpha bits, a p-bit an
// endpoint or a subset, and its index bits (a second set for alpha where it has one).
type bc7Mode struct{ ns, pb, rb, isb, cb, ab, epb, spb, ib, ib2 int }

// bc7State is a BC7 block part-way through its decoding: its bits, its mode, its endpoints, the choices that hold for
// all 16 pixels, the weights its index bits pick from, and where the next pixel's color and alpha indices start (the
// color indices of the 16 pixels come first, then the alpha ones).
type bc7State struct {
	stream                         bitReader
	mode                           bc7Mode
	ends                           [6][4]int
	partition, rotation, selection int
	colorWeights, alphaWeights     []int
	colorPos, alphaPos             int
}

// MaxSide is the most pixels a side of a DDS is decoded at: a bigger one is decoded at its biggest mip level that fits.
const MaxSide = 4096

var (
	bc1Format = blockFormat{8, func(b []byte, out *[16]color.NRGBA) { bc1Color(b, out, false) }}
	bc2Format = blockFormat{16, bc2}
	bc3Format = blockFormat{16, bc3}
	bc7Format = blockFormat{16, bc7}
	// ddsBlocks is the block formats it decodes, by Info's name
	ddsBlocks = map[string]blockFormat{
		"DXT1": bc1Format, "BC1": bc1Format, "BC1 sRGB": bc1Format,
		"DXT3": bc2Format, "BC2": bc2Format, "BC2 sRGB": bc2Format,
		"DXT5": bc3Format, "BC3": bc3Format, "BC3 sRGB": bc3Format,
		"BC7": bc7Format, "BC7 sRGB": bc7Format,
	}
)

// bc7Modes is the eight modes' layouts, by the mode's number.
var bc7Modes = [8]bc7Mode{
	{3, 4, 0, 0, 4, 0, 1, 0, 3, 0},
	{2, 6, 0, 0, 6, 0, 0, 1, 3, 0},
	{3, 6, 0, 0, 5, 0, 0, 0, 2, 0},
	{2, 6, 0, 0, 7, 0, 1, 0, 2, 0},
	{1, 0, 2, 1, 5, 6, 0, 0, 2, 3},
	{1, 0, 2, 0, 7, 8, 0, 0, 2, 2},
	{1, 0, 0, 0, 7, 7, 1, 0, 4, 0},
	{2, 6, 0, 0, 5, 5, 1, 0, 2, 0},
}

// The spec's tables: each partition's subset of a pixel (two subsets 1 bit a pixel, three 2 bits), the second
// subset's anchor pixel, the three-subset partitions' second and third anchors, and the interpolation weights.
var (
	bc7Subsets2 = [64]uint16{
		0xcccc, 0x8888, 0xeeee, 0xecc8, 0xc880, 0xfeec, 0xfec8, 0xec80, 0xc800, 0xffec,
		0xfe80, 0xe800, 0xffe8, 0xff00, 0xfff0, 0xf000, 0xf710, 0x008e, 0x7100, 0x08ce,
		0x008c, 0x7310, 0x3100, 0x8cce, 0x088c, 0x3110, 0x6666, 0x366c, 0x17e8, 0x0ff0,
		0x718e, 0x399c, 0xaaaa, 0xf0f0, 0x5a5a, 0x33cc, 0x3c3c, 0x55aa, 0x9696, 0xa55a,
		0x73ce, 0x13c8, 0x324c, 0x3bdc, 0x6996, 0xc33c, 0x9966, 0x0660, 0x0272, 0x04e4,
		0x4e40, 0x2720, 0xc936, 0x936c, 0x39c6, 0x639c, 0x9336, 0x9cc6, 0x817e, 0xe718,
		0xccf0, 0x0fcc, 0x7744, 0xee22,
	}
	bc7Subsets3 = [64]uint32{
		0xaa685050, 0x6a5a5040, 0x5a5a4200, 0x5450a0a8, 0xa5a50000, 0xa0a05050, 0x5555a0a0,
		0x5a5a5050, 0xaa550000, 0xaa555500, 0xaaaa5500, 0x90909090, 0x94949494, 0xa4a4a4a4,
		0xa9a59450, 0x2a0a4250, 0xa5945040, 0x0a425054, 0xa5a5a500, 0x55a0a0a0, 0xa8a85454,
		0x6a6a4040, 0xa4a45000, 0x1a1a0500, 0x0050a4a4, 0xaaa59090, 0x14696914, 0x69691400,
		0xa08585a0, 0xaa821414, 0x50a4a450, 0x6a5a0200, 0xa9a58000, 0x5090a0a8, 0xa8a09050,
		0x24242424, 0x00aa5500, 0x24924924, 0x24499224, 0x50a50a50, 0x500aa550, 0xaaaa4444,
		0x66660000, 0xa5a0a5a0, 0x50a050a0, 0x69286928, 0x44aaaa44, 0x66666600, 0xaa444444,
		0x54a854a8, 0x95809580, 0x96969600, 0xa85454a8, 0x80959580, 0xaa141414, 0x96960000,
		0xaaaa1414, 0xa05050a0, 0xa0a5a5a0, 0x96000000, 0x40804080, 0xa9a8a9a8, 0xaaaaaa44,
		0x2a4a5254,
	}
	bc7Anchor2 = [64]int{
		15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15,
		15, 2, 8, 2, 2, 8, 8, 15, 2, 8, 2, 2, 8, 8, 2, 2,
		15, 15, 6, 8, 2, 8, 15, 15, 2, 8, 2, 2, 2, 15, 15, 6,
		6, 2, 6, 8, 15, 15, 2, 2, 15, 15, 15, 15, 15, 2, 2, 15,
	}
	bc7Anchor3a = [64]int{
		3, 3, 15, 15, 8, 3, 15, 15, 8, 8, 6, 6, 6, 5, 3, 3,
		3, 3, 8, 15, 3, 3, 6, 10, 5, 8, 8, 6, 8, 5, 15, 15,
		8, 15, 3, 5, 6, 10, 8, 15, 15, 3, 15, 5, 15, 15, 15, 15,
		3, 15, 5, 5, 5, 8, 5, 10, 5, 10, 8, 13, 15, 12, 3, 3,
	}
	bc7Anchor3b = [64]int{
		15, 8, 8, 3, 15, 15, 3, 8, 15, 15, 15, 15, 15, 15, 15, 8,
		15, 8, 15, 3, 15, 8, 15, 8, 3, 15, 6, 10, 15, 15, 10, 8,
		15, 3, 15, 10, 10, 8, 9, 10, 6, 15, 8, 15, 3, 6, 6, 8,
		15, 3, 15, 15, 15, 15, 15, 15, 15, 15, 15, 15, 3, 15, 15, 8,
	}
	bc7Weights = map[int][]int{
		2: {0, 21, 43, 64},
		3: {0, 9, 18, 27, 37, 46, 55, 64},
		4: {0, 4, 9, 13, 17, 21, 26, 30, 34, 38, 43, 47, 51, 55, 60, 64},
	}
)

// Decode decodes the image at path: a DDS at its first mip level, or the biggest within MaxSide; a TGA; anything
// else by its magic (the games ship a PNG named .dds). A DDS format it does not decode is an error naming the format.
func Decode(path string) (image.Image, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	switch {
	case bytes.HasPrefix(data, []byte("DDS ")):
		return decodeDDS(data)
	case strings.EqualFold(filepath.Ext(path), ".tga"):
		return decodeTGA(data)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	return img, err
}

// decodeDDS decodes a DDS's first face at the first mip level within MaxSide.
func decodeDDS(data []byte) (image.Image, error) {
	info, err := ddsInfo(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	offset := 128
	if string(data[84:88]) == "DX10" {
		offset = 148
	}
	depth, masks := ddsChannels(info, data, offset == 148)

	block, compressed := ddsBlocks[info.Format]
	if !compressed && (depth == 0 || depth%8 != 0 || depth > 32 || masks == [4]uint32{}) {
		return nil, fmt.Errorf("DDS %s is not shown", info.Format)
	}
	size := func(width, height int) int {
		if compressed {
			return max(1, (width+3)/4) * max(1, (height+3)/4) * block.size
		}
		return width * height * depth / 8
	}
	width, height := info.Width, info.Height
	for level := 1; level < info.Mips && max(width, height) > MaxSide; level++ {
		offset += size(width, height)
		width, height = max(1, width/2), max(1, height/2)
	}
	if width <= 0 || height <= 0 || offset+size(width, height) > len(data) {
		return nil, errors.New("the DDS file is shorter than its header says")
	}

	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	if compressed {
		drawBlocks(img, data[offset:], block)
	} else {
		drawUncompressed(img, data[offset:], depth, masks)
	}
	return img, nil
}

// ddsChannels is an uncompressed surface's bits a pixel and its channel masks (red, green, blue, alpha), read from
// the header in data: alpha only where its flags say so and a luminance's grey in all three; a DX10 surface's by its
// byte order.
func ddsChannels(info Facts, data []byte, dx10 bool) (depth int, masks [4]uint32) {
	const hasAlpha, alphaOnly, luminance = 0x1, 0x2, 0x20000
	u32 := func(offset int) uint32 { return binary.LittleEndian.Uint32(data[offset:]) }
	flags := u32(80)
	depth, masks = int(u32(88)), [4]uint32{u32(92), u32(96), u32(100), u32(104)}
	switch {
	case dx10 && strings.HasPrefix(info.Format, "RGBA8"):
		depth, masks = 32, [4]uint32{0xff, 0xff00, 0xff0000, 0xff000000}
	case dx10 && strings.HasPrefix(info.Format, "BGRA8"):
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

// drawUncompressed fills img from an uncompressed surface of depth bits a pixel, each channel under its mask.
func drawUncompressed(img *image.NRGBA, src []byte, depth int, masks [4]uint32) {
	width, step := img.Bounds().Dx(), depth/8
	for i := range width * img.Bounds().Dy() {
		var raw [4]byte
		copy(raw[:], src[i*step:i*step+step])
		value := binary.LittleEndian.Uint32(raw[:])
		img.SetNRGBA(i%width, i/width, color.NRGBA{channel(value, masks[0], 0), channel(value, masks[1], 0), channel(value, masks[2], 0), channel(value, masks[3], 255)})
	}
}

// drawBlocks fills img from block-compressed src, row by row of 4×4 blocks; the pixels of a block past the image's
// edge are dropped.
func drawBlocks(img *image.NRGBA, src []byte, block blockFormat) {
	width, height := img.Bounds().Dx(), img.Bounds().Dy()
	var pixels [16]color.NRGBA
	for blockRow := range (height + 3) / 4 {
		for blockColumn := range (width + 3) / 4 {
			block.decode(src[:block.size], &pixels)
			src = src[block.size:]
			for i, pixel := range pixels {
				if column, row := blockColumn*4+i%4, blockRow*4+i/4; column < width && row < height {
					img.SetNRGBA(column, row, pixel)
				}
			}
		}
	}
}

// channel is the 8-bit value of a pixel's channel under its mask; none is the fallback.
func channel(value, mask uint32, none uint8) uint8 {
	if mask == 0 {
		return none
	}
	width := bits.OnesCount32(mask)
	return uint8(uint64((value&mask)>>bits.TrailingZeros32(mask)) * 255 / (1<<width - 1))
}

// bc1Color decodes a BC1 color block: two RGB565 endpoints and a 2-bit index a pixel. Its four colors are the
// endpoints and the two between, but where the first endpoint is not above the second in a block of its own the
// third is halfway and the fourth clear; in BC2 and BC3 (opaque) it is always four.
func bc1Color(src []byte, out *[16]color.NRGBA, opaque bool) {
	first, second := binary.LittleEndian.Uint16(src), binary.LittleEndian.Uint16(src[2:])
	palette := [4]color.NRGBA{rgb565(first), rgb565(second)}
	if first > second || opaque {
		palette[2], palette[3] = mix(palette[0], palette[1], 2, 3), mix(palette[0], palette[1], 1, 3)
	} else {
		palette[2] = mix(palette[0], palette[1], 1, 2)
	}
	for i := range out {
		out[i] = palette[src[4+i/4]>>(2*(i%4))&3]
	}
}

// rgb565 is a 16-bit RGB565 color as 8-bit channels, opaque.
func rgb565(packed uint16) color.NRGBA {
	red, green, blue := uint8(packed>>8)&0xf8, uint8(packed>>3)&0xfc, uint8(packed<<3)
	return color.NRGBA{red | red>>5, green | green>>6, blue | blue>>5, 255}
}

// mix is the color part/whole of the way from second to first, a channel at a time, opaque.
func mix(first, second color.NRGBA, part, whole int) color.NRGBA {
	blend := func(one, other uint8) uint8 { return uint8((part*int(one) + (whole-part)*int(other)) / whole) }
	return color.NRGBA{blend(first.R, second.R), blend(first.G, second.G), blend(first.B, second.B), 255}
}

// bc2 is a 4-bit alpha a pixel, then a BC1 color block.
func bc2(src []byte, out *[16]color.NRGBA) {
	bc1Color(src[8:], out, true)
	for i := range out {
		alpha := src[i/2] >> (4 * (i % 2)) & 0xf
		out[i].A = alpha<<4 | alpha
	}
}

// bc3 is two alpha endpoints and a 3-bit index a pixel, then a BC1 color block.
func bc3(src []byte, out *[16]color.NRGBA) {
	bc1Color(src[8:], out, true)
	alpha0, alpha1 := int(src[0]), int(src[1])
	alpha := [8]uint8{uint8(alpha0), uint8(alpha1)}
	for k := 2; k < 8; k++ {
		if alpha0 > alpha1 {
			alpha[k] = uint8(((8-k)*alpha0 + (k-1)*alpha1) / 7)
		} else if k < 6 {
			alpha[k] = uint8(((6-k)*alpha0 + (k-1)*alpha1) / 5)
		}
	}
	if alpha0 <= alpha1 {
		alpha[7] = 255
	}
	var index uint64
	for i, octet := range src[2:8] {
		index |= uint64(octet) << (8 * i)
	}
	for i := range out {
		out[i].A = alpha[index>>(3*i)&7]
	}
}

// bc7 decodes a BC7 block: its mode is the count of zero bits before the first one; a block with none is black.
func bc7(src []byte, out *[16]color.NRGBA) {
	var state bc7State
	copy(state.stream.block[:], src)
	mode := bits.TrailingZeros8(state.stream.block[0])
	if mode > 7 {
		*out = [16]color.NRGBA{}
		for i := range out {
			out[i].A = 255
		}
		return
	}
	state.stream.pos = mode + 1
	state.mode = bc7Modes[mode]
	state.partition = state.stream.take(state.mode.pb)
	state.rotation = state.stream.take(state.mode.rb)
	state.selection = state.stream.take(state.mode.isb)
	state.ends = bc7Endpoints(&state.stream, state.mode)

	// each pixel's color and alpha indices; a subset's anchor pixel has one bit fewer
	state.colorPos, state.alphaPos = state.stream.pos, state.stream.pos+16*state.mode.ib-state.mode.ns
	state.colorWeights, state.alphaWeights = bc7Weights[state.mode.ib], bc7Weights[state.mode.ib]
	if state.mode.ab > 0 && state.mode.ib2 > 0 {
		state.alphaWeights = bc7Weights[state.mode.ib2]
	}
	for i := range out {
		out[i] = bc7Pixel(&state, i)
	}
}

// bc7Endpoints takes a block's endpoints from stream, channel by channel, then their p-bits, and expands them to 8
// bits.
func bc7Endpoints(stream *bitReader, mode bc7Mode) [6][4]int {
	var ends [6][4]int
	count := mode.ns * 2
	for c, width := range []int{mode.cb, mode.cb, mode.cb, mode.ab} {
		for e := range count {
			if width == 0 {
				ends[e][c] = 255
			} else {
				ends[e][c] = stream.take(width)
			}
		}
	}
	colorBits, alphaBits := mode.cb, mode.ab
	if mode.epb+mode.spb > 0 {
		colorBits++
		if alphaBits > 0 {
			alphaBits++
		}
	}
	// a p-bit is each channel's lowest bit: one an endpoint, or one a subset that its two endpoints share
	pBit := 0
	for e := range count {
		switch {
		case mode.epb > 0, mode.spb > 0 && e%2 == 0:
			pBit = stream.take(1)
		case mode.spb == 0:
			continue
		}
		for c := range 4 {
			if c < 3 || mode.ab > 0 {
				ends[e][c] = ends[e][c]<<1 | pBit
			}
		}
	}
	for e := range count {
		for c := range 4 {
			width := colorBits
			if c == 3 {
				width = alphaBits
			}
			if width > 0 {
				value := ends[e][c] << (8 - width) & 0xff
				ends[e][c] = value | value>>width
			}
		}
	}
	return ends
}

// bc7Pixel is pixel i of a block: its subset's two endpoints, mixed by its color and alpha indices (the same index for
// both unless the mode has a second set for alpha), with the block's channel rotation applied.
func bc7Pixel(state *bc7State, i int) color.NRGBA {
	mode, stream := state.mode, &state.stream
	subset := 0
	switch mode.ns {
	case 2:
		subset = int(bc7Subsets2[state.partition] >> i & 1)
	case 3:
		subset = int(bc7Subsets3[state.partition] >> (2 * i) & 3)
	}
	ib := mode.ib
	if i == 0 || mode.ns == 2 && i == bc7Anchor2[state.partition] || mode.ns == 3 && (i == bc7Anchor3a[state.partition] || i == bc7Anchor3b[state.partition]) {
		ib--
	}
	stream.pos = state.colorPos
	colorIndex := stream.take(ib)
	state.colorPos = stream.pos
	colorWeight, alphaWeight := state.colorWeights[colorIndex], state.colorWeights[colorIndex]
	if mode.ab > 0 && mode.ib2 > 0 {
		ib2 := mode.ib2
		if i == 0 {
			ib2--
		}
		stream.pos = state.alphaPos
		alphaIndex := stream.take(ib2)
		state.alphaPos = stream.pos
		colorWeight, alphaWeight = state.colorWeights[colorIndex], state.alphaWeights[alphaIndex]
		if state.selection > 0 {
			colorWeight, alphaWeight = state.alphaWeights[alphaIndex], state.colorWeights[colorIndex]
		}
	}
	low, high := state.ends[subset*2], state.ends[subset*2+1]
	lerp := func(index, weight int) uint8 { return uint8(((64-weight)*low[index] + weight*high[index] + 32) >> 6) }
	pixel := [4]uint8{lerp(0, colorWeight), lerp(1, colorWeight), lerp(2, colorWeight), lerp(3, alphaWeight)}
	if state.rotation > 0 {
		pixel[state.rotation-1], pixel[3] = pixel[3], pixel[state.rotation-1]
	}
	return color.NRGBA{pixel[0], pixel[1], pixel[2], pixel[3]}
}

// take is the next count bits of the block, lowest first, and moves the cursor past them; none is 0.
func (bitReader *bitReader) take(count int) int {
	if count == 0 {
		return 0
	}
	value := (int(bitReader.block[bitReader.pos/8]) | int(bitReader.block[bitReader.pos/8+1])<<8) >> (bitReader.pos % 8) & (1<<count - 1)
	bitReader.pos += count
	return value
}

// decodeTGA decodes a true-color (24 or 32 bits) or grey (8 bits) TGA, raw or run-length, from either origin.
func decodeTGA(data []byte) (image.Image, error) {
	if len(data) < 18 {
		return nil, errors.New("not a TGA file")
	}
	kind, depth, desc := data[2], int(data[16]), data[17]
	width, height := int(binary.LittleEndian.Uint16(data[12:])), int(binary.LittleEndian.Uint16(data[14:]))
	grey := kind == 3 || kind == 11
	if data[1] != 0 || kind != 2 && kind != 3 && kind != 10 && kind != 11 || grey && depth != 8 || !grey && depth != 24 && depth != 32 {
		return nil, fmt.Errorf("TGA type %d at %d bits is not shown", kind, depth)
	}
	step, src := depth/8, data[min(18+int(data[0]), len(data)):]
	pixels := src
	if kind >= 10 {
		// packets: a count, then that many pixels, or one pixel repeated that many times
		pixels = make([]byte, 0, width*height*step)
		for len(pixels) < width*height*step && len(src) > 0 {
			head := int(src[0])
			count := head&0x7f + 1
			src = src[1:]
			if head&0x80 == 0 {
				count = min(count*step, len(src))
				pixels, src = append(pixels, src[:count]...), src[count:]
				continue
			}
			if len(src) < step {
				break
			}
			for range count {
				pixels = append(pixels, src[:step]...)
			}
			src = src[step:]
		}
	}
	if len(pixels) < width*height*step {
		return nil, errors.New("the TGA file is shorter than its header says")
	}
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for i := range width * height {
		column, row, pixel := i%width, i/width, pixels[i*step:]
		if desc&0x20 == 0 {
			row = height - 1 - row // its first row is the bottom one
		}
		rgba := color.NRGBA{pixel[0], pixel[0], pixel[0], 255}
		if !grey {
			rgba.R, rgba.G, rgba.B = pixel[2], pixel[1], pixel[0]
		}
		if step == 4 {
			rgba.A = pixel[3]
		}
		img.SetNRGBA(column, row, rgba)
	}
	return img, nil
}
