package texture

import (
	"encoding/binary"
	"errors"
	"image"
	"image/draw"
	"math"
	"math/bits"
	"sort"
)

// dxgiBGRA is B8G8R8A8_UNORM, the uncompressed format EncodeDDS writes.
const dxgiBGRA = 87

// firstShares are how much of a BC1 palette's first color each of its four colors holds, by index.
var firstShares = [4]float64{1, 0, 2.0 / 3, 1.0 / 3}

// channels565 are the bits and the shift of red, green and blue in a 5-6-5 color.
var channels565 = [3][2]int{{5, 11}, {6, 5}, {5, 0}}

// EncodeDDS writes a picture as a DDS file, in BC3 blocks when compressed and else uncompressed. With mipmaps, the
// mip levels follow the picture down to one pixel; without, the picture is the only level. It returns an error for a
// picture with no pixels or a side past Direct3D's limit.
func EncodeDDS(picture image.Image, compressed, mipmaps bool) ([]byte, error) {
	size := picture.Bounds().Size()
	if size.X < 1 || size.Y < 1 {
		return nil, errors.New("the picture has no pixels")
	}
	if err := checkSides(size.X, size.Y); err != nil {
		return nil, err
	}
	levels := 1
	if mipmaps {
		levels = bits.Len(uint(max(size.X, size.Y)))
	}
	file := encodeDDSHeader(size, levels, compressed)
	// a picture already in the pixels the encoder reads is not copied
	level, ok := picture.(*image.NRGBA)
	if !ok || level.Rect.Min != (image.Point{}) {
		level = image.NewNRGBA(image.Rectangle{Max: size})
		draw.Draw(level, level.Rect, picture, picture.Bounds().Min, draw.Src)
	}
	for i := range levels {
		if i > 0 {
			level = buildMip(level)
		}
		if compressed {
			file = append(file, encodeBC3(level)...)
		} else {
			file = append(file, encodeBGRA(level)...)
		}
	}
	return file, nil
}

// encodeDDSHeader writes the magic and the header of a DDS with the given size and number of mip levels, as BC3 or
// uncompressed.
func encodeDDSHeader(size image.Point, levels int, compressed bool) []byte {
	file := make([]byte, ddsDataOffset)
	copy(file, ddsMagic)
	put := func(offset int, value uint32) { binary.LittleEndian.PutUint32(file[offset:], value) }
	put(ddsSizeOffset, ddsHeaderSize)
	put(ddsFlagsOffset, ddsHeaderFlags)
	put(ddsHeightOffset, uint32(size.Y))
	put(ddsWidthOffset, uint32(size.X))
	put(ddsMipmapsOffset, uint32(levels))
	put(ddsFormatOffset, ddsFormatSize)
	caps := uint32(ddsTexture)
	if levels > 1 {
		caps |= ddsChain
	}
	put(ddsCapsOffset, caps)
	if compressed {
		put(ddsFormatFlagsOffset, ddsFourCC)
		copy(file[ddsFourCCOffset:], "DXT5")
		return file
	}
	put(ddsFormatFlagsOffset, ddsRGB|ddsAlphaPixels)
	row := dxgiMasks[dxgiBGRA]
	put(ddsDepthOffset, row[0])
	for c, mask := range row[1:] {
		put(ddsMasksOffset+4*c, mask)
	}
	return file
}

// buildMip makes the mip level after a picture: half its size on each side, rounded down and never under a
// pixel. Each pixel is the average of the two by two pixels it covers, their colors mixed in linear light and their
// alpha as it is. A side of one pixel uses that pixel twice.
func buildMip(picture *image.NRGBA) *image.NRGBA {
	var linear [256]float64
	for value := range linear {
		linear[value] = toLinear(uint8(value))
	}
	size := picture.Rect.Size()
	next := image.NewNRGBA(image.Rect(0, 0, max(1, size.X/2), max(1, size.Y/2)))
	for y := range next.Rect.Dy() {
		for x := range next.Rect.Dx() {
			var sums [4]float64
			for _, step := range [4]image.Point{{0, 0}, {1, 0}, {0, 1}, {1, 1}} {
				pixel := picture.Pix[picture.PixOffset(min(2*x+step.X, size.X-1), min(2*y+step.Y, size.Y-1)):]
				for c := range 3 {
					sums[c] += linear[pixel[c]]
				}
				sums[3] += float64(pixel[3])
			}
			out := next.Pix[next.PixOffset(x, y):]
			for c := range 3 {
				out[c] = toSRGB(&linear, sums[c]/4)
			}
			out[3] = uint8(math.Round(sums[3] / 4))
		}
	}
	return next
}

// toLinear turns an sRGB channel value into linear light, from 0 to 1.
func toLinear(value uint8) float64 {
	x := float64(value) / 255
	if x <= 0.04045 {
		return x / 12.92
	}
	return math.Pow((x+0.055)/1.055, 2.4)
}

// toSRGB returns the sRGB channel value whose linear light is nearest the given light, from the table of every
// value's linear light.
func toSRGB(linear *[256]float64, light float64) uint8 {
	above := min(sort.SearchFloat64s(linear[:], light), 255)
	if above > 0 && light-linear[above-1] < linear[above]-light {
		return uint8(above - 1)
	}
	return uint8(above)
}

// encodeBGRA writes a picture's pixels uncompressed, row by row: blue, green, red and alpha.
func encodeBGRA(picture *image.NRGBA) []byte {
	size := picture.Rect.Size()
	data := make([]byte, 0, size.X*size.Y*4)
	for y := range size.Y {
		row := picture.Pix[y*picture.Stride : y*picture.Stride+size.X*4]
		for x := 0; x < len(row); x += 4 {
			data = append(data, row[x+2], row[x+1], row[x], row[x+3])
		}
	}
	return data
}

// encodeBC3 writes a picture as BC3 blocks of 4 by 4 pixels, row by row, each an alpha block and then a color block.
// A block past the right or bottom edge repeats the last column or row.
func encodeBC3(picture *image.NRGBA) []byte {
	size := picture.Rect.Size()
	// a block of 16 pixels is 16 bytes: a byte for each pixel of the size rounded up to whole blocks
	data := make([]byte, 0, (size.X+3)/4*4*((size.Y+3)/4*4))
	var block [16][4]uint8
	for top := 0; top < size.Y; top += 4 {
		for left := 0; left < size.X; left += 4 {
			for i := range block {
				x, y := min(left+i%4, size.X-1), min(top+i/4, size.Y-1)
				block[i] = [4]uint8(picture.Pix[picture.PixOffset(x, y):])
			}
			data = append(data, encodeAlphaBlock(&block)...)
			data = append(data, encodeColorBlock(&block)...)
		}
	}
	return data
}

// encodeAlphaBlock writes the alpha of a block as a BC4 block: its highest and lowest alpha, then the index of each
// pixel's nearest value in the palette the two make.
func encodeAlphaBlock(block *[16][4]uint8) []byte {
	highest, lowest := block[0][3], block[0][3]
	for _, pixel := range block {
		highest, lowest = max(highest, pixel[3]), min(lowest, pixel[3])
	}
	palette := resolveBC4Palette(int(highest), int(lowest))
	var indices uint64
	for i, pixel := range block {
		alpha, nearest, best := int(pixel[3]), 0, math.MaxInt
		for candidate, value := range palette {
			if distance := max(value-alpha, alpha-value); distance < best {
				nearest, best = candidate, distance
			}
		}
		indices |= uint64(nearest) << (3 * i)
	}
	out := binary.LittleEndian.AppendUint64(nil, indices<<16)
	out[0], out[1] = highest, lowest
	return out
}

// encodeColorBlock writes the colors of a block as a BC1 color block, its first color above its second so that it
// reads in the four-color mode. It starts from the block's two pixels farthest apart, takes each pixel's nearest
// palette color, then fits the two colors to those once by least squares.
func encodeColorBlock(block *[16][4]uint8) []byte {
	start := findFarthest(block, block[0])
	first, second := to565(toFloats(block[findFarthest(block, block[start])])), to565(toFloats(block[start]))
	first, second = resolveEndpoints(block, resolveIndices(block, first, second))
	indices := resolveIndices(block, first, second)
	if first < second {
		// swapping the colors swaps the indices 0 with 1 and 2 with 3
		first, second, indices = second, first, indices^0x55555555
	}
	out := binary.LittleEndian.AppendUint16(nil, first)
	out = binary.LittleEndian.AppendUint16(out, second)
	return binary.LittleEndian.AppendUint32(out, indices)
}

// findFarthest returns the index of a block's pixel whose color is farthest from a color.
func findFarthest(block *[16][4]uint8, color [4]uint8) int {
	farthest, best := 0, -1
	for i, pixel := range block {
		if distance := countDistance(pixel, color); distance > best {
			farthest, best = i, distance
		}
	}
	return farthest
}

// countDistance returns the square of the distance between two colors' red, green and blue.
func countDistance(first, second [4]uint8) int {
	distance := 0
	for c := range 3 {
		difference := int(first[c]) - int(second[c])
		distance += difference * difference
	}
	return distance
}

// toFloats returns a pixel's red, green and blue.
func toFloats(pixel [4]uint8) [3]float64 {
	return [3]float64{float64(pixel[0]), float64(pixel[1]), float64(pixel[2])}
}

// to565 rounds a color, each channel from 0 to 255, to the nearest 5-6-5 color.
func to565(color [3]float64) uint16 {
	var packed uint16
	for c, channel := range channels565 {
		top := float64(int(1)<<channel[0] - 1)
		packed |= uint16(math.Round(min(max(color[c], 0), 255)*top/255)) << channel[1]
	}
	return packed
}

// resolveIndices returns the two-bit index of each pixel's nearest color in the palette of two 5-6-5 colors, the
// first pixel in the lowest bits.
func resolveIndices(block *[16][4]uint8, first, second uint16) uint32 {
	palette := resolveBC1Palette(binary.LittleEndian.AppendUint16(binary.LittleEndian.AppendUint16(nil, first), second))
	var indices uint32
	for i, pixel := range block {
		nearest, best := 0, math.MaxInt
		for candidate, color := range palette {
			if distance := countDistance(pixel, color); distance < best {
				nearest, best = candidate, distance
			}
		}
		indices |= uint32(nearest) << (2 * i)
	}
	return indices
}

// resolveEndpoints returns the two 5-6-5 colors whose palette fits a block's pixels best by least squares, given the
// palette color each pixel takes. When every pixel takes the same color of the palette, both are the block's average.
func resolveEndpoints(block *[16][4]uint8, indices uint32) (uint16, uint16) {
	var firstSquares, secondSquares, products float64
	var towardFirst, towardSecond, average [3]float64
	for i, pixel := range block {
		share := firstShares[indices>>(2*i)&3]
		firstSquares += share * share
		secondSquares += (1 - share) * (1 - share)
		products += share * (1 - share)
		for c := range 3 {
			towardFirst[c] += share * float64(pixel[c])
			towardSecond[c] += (1 - share) * float64(pixel[c])
			average[c] += float64(pixel[c]) / 16
		}
	}
	determinant := firstSquares*secondSquares - products*products
	if determinant < 1e-6 {
		return to565(average), to565(average)
	}
	var first, second [3]float64
	for c := range 3 {
		first[c] = (towardFirst[c]*secondSquares - towardSecond[c]*products) / determinant
		second[c] = (towardSecond[c]*firstSquares - towardFirst[c]*products) / determinant
	}
	return to565(first), to565(second)
}
