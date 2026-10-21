package texture

import (
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"slices"
)

// decodeTGA decodes a TGA to RGBA. It handles raw and run-length encoded data from 8 to 32 bits per pixel.
func decodeTGA(bytes []byte) (image.Image, error) {
	header, ok := decodeTGAHeader(bytes)
	if !ok {
		return nil, errors.New("not a TGA")
	}
	imageType, descriptor := bytes[2], bytes[17]
	size := (int(bytes[16]) + 7) / 8
	palette, offset, err := readTGAPalette(bytes, 18+int(bytes[0]))
	if err != nil {
		return nil, err
	}
	count := header.Width * header.Height
	pixels, err := readTGAPixels(bytes[offset:], count*size, size, imageType >= 9)
	if err != nil {
		return nil, err
	}
	nrgba := image.NewNRGBA(image.Rect(0, 0, header.Width, header.Height))
	for i := range count {
		color, err := toColor(pixels[i*size:(i+1)*size], imageType&7, palette)
		if err != nil {
			return nil, err
		}
		x, y := i%header.Width, i/header.Width
		if descriptor&16 != 0 {
			x = header.Width - 1 - x
		}
		if descriptor&32 == 0 {
			y = header.Height - 1 - y
		}
		copy(nrgba.Pix[nrgba.PixOffset(x, y):], color[:])
	}
	return nrgba, nil
}

// decodeTGAHeader reads a TGA header. A TGA has no magic bytes, so it is recognized by a consistent header: a known
// image type and pixel depth and a nonzero size. It returns false if the bytes are not a TGA.
func decodeTGAHeader(bytes []byte) (Header, bool) {
	if len(bytes) < 18 {
		return Header{}, false
	}
	width, height := int(binary.LittleEndian.Uint16(bytes[12:])), int(binary.LittleEndian.Uint16(bytes[14:]))
	if !slices.Contains([]byte{1, 2, 3, 9, 10, 11}, bytes[2]) || !slices.Contains([]byte{8, 15, 16, 24, 32}, bytes[16]) ||
		width == 0 || height == 0 {
		return Header{}, false
	}
	return Header{Format: "tga", Width: width, Height: height, Mipmaps: 1}, true
}

// readTGAPalette reads the color map of an image that has one, starting at the given offset. It returns the
// colors by the index the pixels write, and the offset where the pixels begin. An image without a color map gets
// nil. It returns an error if the bytes end before the map does.
func readTGAPalette(bytes []byte, offset int) ([][4]uint8, int, error) {
	if offset > len(bytes) {
		return nil, 0, errTruncated
	}
	if bytes[1] != 1 {
		return nil, offset, nil
	}
	first, length := int(binary.LittleEndian.Uint16(bytes[3:])), int(binary.LittleEndian.Uint16(bytes[5:]))
	size := (int(bytes[7]) + 7) / 8
	if len(bytes) < offset+length*size {
		return nil, 0, errTruncated
	}
	palette := make([][4]uint8, first+length)
	for i := range length {
		color, err := toColor(bytes[offset+i*size:offset+(i+1)*size], 2, nil)
		if err != nil {
			return nil, 0, err
		}
		palette[first+i] = color
	}
	return palette, offset + length*size, nil
}

// readTGAPixels reads the given count of pixel bytes, each pixel of the given size, unpacking run-length packets
// when the image is encoded. It returns an error if the data ends before the last pixel.
func readTGAPixels(data []byte, total, size int, encoded bool) ([]byte, error) {
	if !encoded {
		if len(data) < total {
			return nil, errTruncated
		}
		return data[:total], nil
	}
	if total > 128*len(data) {
		// A packet of two bytes unpacks to 128 pixels at most, so the image cannot be larger than this.
		return nil, errTruncated
	}
	pixels := make([]byte, 0, total)
	for len(pixels) < total {
		if len(data) == 0 {
			return nil, errTruncated
		}
		count := int(data[0]&127) + 1
		if data[0]&128 != 0 {
			if len(data) < 1+size {
				return nil, errTruncated
			}
			for range count {
				pixels = append(pixels, data[1:1+size]...)
			}
			data = data[1+size:]
			continue
		}
		if len(data) < 1+count*size {
			return nil, errTruncated
		}
		pixels = append(pixels, data[1:1+count*size]...)
		data = data[1+count*size:]
	}
	return pixels[:total], nil
}

// toColor converts one stored pixel to a color by the image type, its run-length bit cleared. A color-mapped
// pixel is an index into the given palette, a truecolor pixel is 5-5-5, BGR or BGRA by its size, and a gray
// pixel is one byte with an alpha byte after it when it has two. A 5-5-5 pixel's attribute bit is not alpha. It
// returns an error for a pixel size the type does not define, or an index past the palette.
func toColor(pixel []byte, imageType byte, palette [][4]uint8) ([4]uint8, error) {
	switch {
	case imageType == 1 && len(pixel) <= 2:
		index := int(pixel[0])
		if len(pixel) == 2 {
			index = int(binary.LittleEndian.Uint16(pixel))
		}
		if index >= len(palette) {
			return [4]uint8{}, fmt.Errorf("TGA color-map index %d past its %d entries", index, len(palette))
		}
		return palette[index], nil
	case imageType == 3 && len(pixel) <= 2:
		alpha := uint8(255)
		if len(pixel) == 2 {
			alpha = pixel[1]
		}
		return [4]uint8{pixel[0], pixel[0], pixel[0], alpha}, nil
	case imageType == 2 && len(pixel) == 2:
		value := uint32(binary.LittleEndian.Uint16(pixel))
		return [4]uint8{toByte(value>>10&31, 5), toByte(value>>5&31, 5), toByte(value&31, 5), 255}, nil
	case imageType == 2 && len(pixel) == 3:
		return [4]uint8{pixel[2], pixel[1], pixel[0], 255}, nil
	case imageType == 2 && len(pixel) == 4:
		return [4]uint8{pixel[2], pixel[1], pixel[0], pixel[3]}, nil
	}
	return [4]uint8{}, fmt.Errorf("TGA type %d with %d bytes per pixel is not supported", imageType, len(pixel))
}
