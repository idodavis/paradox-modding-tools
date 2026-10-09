// image.go decodes the games' images a browser cannot draw: DDS (BC1, BC2, BC3, BC7, and an uncompressed surface by
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
	"math/bits"
	"os"
	"path/filepath"
	"strings"
)

// MaxSide is the most pixels a side of a DDS is decoded at: a bigger one is decoded at its biggest mip level that fits.
const MaxSide = 4096

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

// blockFormat is a block-compressed format: the bytes of each 4×4 block, and its decoder.
type blockFormat struct {
	size   int
	decode func(block []byte, out *[16]color.NRGBA)
}

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

// decodeDDS decodes a DDS's first face at the first mip level within MaxSide.
func decodeDDS(data []byte) (image.Image, error) {
	info, err := ddsInfo(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	u32 := func(off int) uint32 { return binary.LittleEndian.Uint32(data[off:]) }
	offset := 128
	if string(data[84:88]) == "DX10" {
		offset = 148
	}

	// an uncompressed surface's channels: its masks (red, green, blue, alpha), alpha only where its flags say so and a
	// luminance's grey in all three; a DX10 one's by its byte order
	const hasAlpha, alphaOnly, luminance = 0x1, 0x2, 0x20000
	flags, depth, masks := u32(80), int(u32(88)), [4]uint32{u32(92), u32(96), u32(100), u32(104)}
	switch {
	case offset == 148 && strings.HasPrefix(info.Format, "RGBA8"):
		depth, masks = 32, [4]uint32{0xff, 0xff00, 0xff0000, 0xff000000}
	case offset == 148 && strings.HasPrefix(info.Format, "BGRA8"):
		depth, masks = 32, [4]uint32{0xff0000, 0xff00, 0xff, 0xff000000}
	case offset == 148:
		depth = 0
	case flags&luminance != 0:
		masks[1], masks[2] = masks[0], masks[0]
	}
	if offset == 128 && flags&(hasAlpha|alphaOnly) == 0 {
		masks[3] = 0
	}

	block, compressed := ddsBlocks[info.Format]
	if !compressed && (depth == 0 || depth%8 != 0 || depth > 32 || masks == [4]uint32{}) {
		return nil, fmt.Errorf("DDS %s is not shown", info.Format)
	}
	size := func(w, h int) int {
		if compressed {
			return max(1, (w+3)/4) * max(1, (h+3)/4) * block.size
		}
		return w * h * depth / 8
	}
	w, h := info.Width, info.Height
	for level := 1; level < info.Mips && max(w, h) > MaxSide; level++ {
		offset += size(w, h)
		w, h = max(1, w/2), max(1, h/2)
	}
	if w <= 0 || h <= 0 || offset+size(w, h) > len(data) {
		return nil, errors.New("the DDS file is shorter than its header says")
	}

	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	src := data[offset:]
	if !compressed {
		step := depth / 8
		for i := range w * h {
			var raw [4]byte
			copy(raw[:], src[i*step:i*step+step])
			v := binary.LittleEndian.Uint32(raw[:])
			img.SetNRGBA(i%w, i/w, color.NRGBA{channel(v, masks[0], 0), channel(v, masks[1], 0), channel(v, masks[2], 0), channel(v, masks[3], 255)})
		}
		return img, nil
	}
	var px [16]color.NRGBA
	for by := range (h + 3) / 4 {
		for bx := range (w + 3) / 4 {
			block.decode(src[:block.size], &px)
			src = src[block.size:]
			for i, c := range px {
				if x, y := bx*4+i%4, by*4+i/4; x < w && y < h {
					img.SetNRGBA(x, y, c)
				}
			}
		}
	}
	return img, nil
}

// channel is the 8-bit value of a pixel's channel under its mask; none is the fallback.
func channel(v, mask uint32, none uint8) uint8 {
	if mask == 0 {
		return none
	}
	width := bits.OnesCount32(mask)
	return uint8(uint64((v&mask)>>bits.TrailingZeros32(mask)) * 255 / (1<<width - 1))
}

// bc1Color decodes a BC1 color block: two RGB565 endpoints and a 2-bit index a pixel. Its four colors are the
// endpoints and the two between, but where c0 <= c1 in a block of its own the third is halfway and the fourth clear;
// in BC2 and BC3 (opaque) it is always four.
func bc1Color(src []byte, out *[16]color.NRGBA, opaque bool) {
	c0, c1 := binary.LittleEndian.Uint16(src), binary.LittleEndian.Uint16(src[2:])
	p := [4]color.NRGBA{rgb565(c0), rgb565(c1)}
	if c0 > c1 || opaque {
		p[2], p[3] = mix(p[0], p[1], 2, 1, 3), mix(p[0], p[1], 1, 2, 3)
	} else {
		p[2] = mix(p[0], p[1], 1, 1, 2)
	}
	for i := range out {
		out[i] = p[src[4+i/4]>>(2*(i%4))&3]
	}
}

func rgb565(v uint16) color.NRGBA {
	r, g, b := uint8(v>>8)&0xf8, uint8(v>>3)&0xfc, uint8(v<<3)
	return color.NRGBA{r | r>>5, g | g>>6, b | b>>5, 255}
}

// mix is (a*x + b*y) / d of each color channel, opaque.
func mix(x, y color.NRGBA, a, b, d int) color.NRGBA {
	at := func(u, v uint8) uint8 { return uint8((a*int(u) + b*int(v)) / d) }
	return color.NRGBA{at(x.R, y.R), at(x.G, y.G), at(x.B, y.B), 255}
}

// bc2 is a 4-bit alpha a pixel, then a BC1 color block.
func bc2(src []byte, out *[16]color.NRGBA) {
	bc1Color(src[8:], out, true)
	for i := range out {
		a := src[i/2] >> (4 * (i % 2)) & 0xf
		out[i].A = a<<4 | a
	}
}

// bc3 is two alpha endpoints and a 3-bit index a pixel, then a BC1 color block.
func bc3(src []byte, out *[16]color.NRGBA) {
	bc1Color(src[8:], out, true)
	a0, a1 := int(src[0]), int(src[1])
	alpha := [8]uint8{uint8(a0), uint8(a1)}
	for k := 2; k < 8; k++ {
		if a0 > a1 {
			alpha[k] = uint8(((8-k)*a0 + (k-1)*a1) / 7)
		} else if k < 6 {
			alpha[k] = uint8(((6-k)*a0 + (k-1)*a1) / 5)
		}
	}
	if a0 <= a1 {
		alpha[7] = 255
	}
	var index uint64
	for i, b := range src[2:8] {
		index |= uint64(b) << (8 * i)
	}
	for i := range out {
		out[i].A = alpha[index>>(3*i)&7]
	}
}

// bc7Modes is each BC7 mode's subsets, partition, rotation and index-selection bits, color and alpha bits, a p-bit an
// endpoint or a subset, and its index bits (a second set for alpha where it has one).
var bc7Modes = [8]struct{ ns, pb, rb, isb, cb, ab, epb, spb, ib, ib2 int }{
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

// bc7 decodes a BC7 block: its mode is the count of zero bits before the first one; a block with none is black.
func bc7(src []byte, out *[16]color.NRGBA) {
	var block [17]byte // a spare byte past the end, which a read across the last byte boundary touches
	copy(block[:], src)
	at := 0
	read := func(n int) int {
		if n == 0 {
			return 0
		}
		v := (int(block[at/8]) | int(block[at/8+1])<<8) >> (at % 8) & (1<<n - 1)
		at += n
		return v
	}
	mode := bits.TrailingZeros8(block[0])
	if mode > 7 {
		*out = [16]color.NRGBA{}
		for i := range out {
			out[i].A = 255
		}
		return
	}
	at = mode + 1
	m := bc7Modes[mode]
	partition, rotation, selection := read(m.pb), read(m.rb), read(m.isb)

	// the endpoints, channel by channel, then their p-bits and their expansion to 8 bits
	var ends [6][4]int
	count := m.ns * 2
	for c, width := range []int{m.cb, m.cb, m.cb, m.ab} {
		for e := range count {
			if width == 0 {
				ends[e][c] = 255
			} else {
				ends[e][c] = read(width)
			}
		}
	}
	cb, ab := m.cb, m.ab
	if m.epb+m.spb > 0 {
		cb++
		if ab > 0 {
			ab++
		}
	}
	// a p-bit is each channel's lowest bit: one an endpoint, or one a subset that its two endpoints share
	p := 0
	for e := range count {
		switch {
		case m.epb > 0, m.spb > 0 && e%2 == 0:
			p = read(1)
		case m.spb == 0:
			continue
		}
		for c := range 4 {
			if c < 3 || m.ab > 0 {
				ends[e][c] = ends[e][c]<<1 | p
			}
		}
	}
	for e := range count {
		for c := range 4 {
			width := cb
			if c == 3 {
				width = ab
			}
			if width > 0 {
				v := ends[e][c] << (8 - width) & 0xff
				ends[e][c] = v | v>>width
			}
		}
	}

	// each pixel's color and alpha indices; a subset's anchor pixel has one bit fewer
	colorAt, alphaAt := at, at+16*m.ib-m.ns
	cw, aw := bc7Weights[m.ib], bc7Weights[m.ib]
	if m.ab > 0 && m.ib2 > 0 {
		aw = bc7Weights[m.ib2]
	}
	for i := range out {
		subset := 0
		switch m.ns {
		case 2:
			subset = int(bc7Subsets2[partition] >> i & 1)
		case 3:
			subset = int(bc7Subsets3[partition] >> (2 * i) & 3)
		}
		ib := m.ib
		if i == 0 || m.ns == 2 && i == bc7Anchor2[partition] || m.ns == 3 && (i == bc7Anchor3a[partition] || i == bc7Anchor3b[partition]) {
			ib--
		}
		at = colorAt
		i0 := read(ib)
		colorAt = at
		sc, sa := cw[i0], cw[i0]
		if m.ab > 0 && m.ib2 > 0 {
			ib2 := m.ib2
			if i == 0 {
				ib2--
			}
			at = alphaAt
			i1 := read(ib2)
			alphaAt = at
			sc, sa = cw[i0], aw[i1]
			if selection > 0 {
				sc, sa = aw[i1], cw[i0]
			}
		}
		e0, e1 := ends[subset*2], ends[subset*2+1]
		lerp := func(c, s int) uint8 { return uint8(((64-s)*e0[c] + s*e1[c] + 32) >> 6) }
		px := [4]uint8{lerp(0, sc), lerp(1, sc), lerp(2, sc), lerp(3, sa)}
		if rotation > 0 {
			px[rotation-1], px[3] = px[3], px[rotation-1]
		}
		out[i] = color.NRGBA{px[0], px[1], px[2], px[3]}
	}
}

// decodeTGA decodes a true-color (24 or 32 bits) or grey (8 bits) TGA, raw or run-length, from either origin.
func decodeTGA(data []byte) (image.Image, error) {
	if len(data) < 18 {
		return nil, errors.New("not a TGA file")
	}
	kind, depth, desc := data[2], int(data[16]), data[17]
	w, h := int(binary.LittleEndian.Uint16(data[12:])), int(binary.LittleEndian.Uint16(data[14:]))
	grey := kind == 3 || kind == 11
	if data[1] != 0 || kind != 2 && kind != 3 && kind != 10 && kind != 11 || grey && depth != 8 || !grey && depth != 24 && depth != 32 {
		return nil, fmt.Errorf("TGA type %d at %d bits is not shown", kind, depth)
	}
	step, src := depth/8, data[min(18+int(data[0]), len(data)):]
	pixels := src
	if kind >= 10 {
		// packets: a count, then that many pixels, or one pixel repeated that many times
		pixels = make([]byte, 0, w*h*step)
		for len(pixels) < w*h*step && len(src) > 0 {
			head := int(src[0])
			n := head&0x7f + 1
			src = src[1:]
			if head&0x80 == 0 {
				n = min(n*step, len(src))
				pixels, src = append(pixels, src[:n]...), src[n:]
				continue
			}
			if len(src) < step {
				break
			}
			for range n {
				pixels = append(pixels, src[:step]...)
			}
			src = src[step:]
		}
	}
	if len(pixels) < w*h*step {
		return nil, errors.New("the TGA file is shorter than its header says")
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := range w * h {
		x, y, p := i%w, i/w, pixels[i*step:]
		if desc&0x20 == 0 {
			y = h - 1 - y // its first row is the bottom one
		}
		c := color.NRGBA{p[0], p[0], p[0], 255}
		if !grey {
			c.R, c.G, c.B = p[2], p[1], p[0]
		}
		if step == 4 {
			c.A = p[3]
		}
		img.SetNRGBA(x, y, c)
	}
	return img, nil
}
