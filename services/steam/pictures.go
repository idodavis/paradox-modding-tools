// pictures.go makes the smaller copies of a listing's pictures that Steam takes. The mod's own files stay as they are.

package steam

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	_ "image/gif" // decodes a GIF's header, so a GIF is told from a picture PMT cannot read
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"

	"github.com/samber/lo"
)

// pictureBytes is the size at which Steam refuses a picture, 1 MB.
const pictureBytes = 1 << 20

// The scale of a picture's sides at each try to make it smaller, and the JPEG quality of one with no transparency.
const (
	shrinkFactor = 0.8
	jpegQuality  = 90
)

// errGIF is the error for a GIF too big for Steam. PMT does not shrink a GIF, which would lose its animation.
var errGIF = errors.New("resize the GIF: Steam takes pictures under 1 MB")

// resolvePictures returns a listing whose pictures Steam takes, with smaller copies written into a folder. It returns
// an error if a big picture is a GIF, cannot be read or cannot be made small enough.
func resolvePictures(dir string, listing Listing) (Listing, error) {
	listing.Previews = append([]Preview{}, listing.Previews...)
	pictures := []*string{&listing.Thumbnail}
	for position := range listing.Previews {
		if listing.Previews[position].Type == previewImage {
			pictures = append(pictures, &listing.Previews[position].Path)
		}
	}
	for _, picture := range pictures {
		file, err := resolvePicture(dir, *picture)
		if err != nil {
			return Listing{}, err
		}
		*picture = file
	}
	return listing, nil
}

// resolvePicture returns a picture under Steam's limit, as it is or as a smaller copy. It returns an error if it is a
// GIF, cannot be read or cannot be made small enough.
func resolvePicture(dir, file string) (string, error) {
	if file == "" {
		return "", nil
	}
	stat, err := os.Stat(file)
	if err != nil || stat.Size() < pictureBytes {
		return file, err
	}
	content, err := os.ReadFile(file)
	if err != nil {
		return "", err
	}
	if _, format, err := image.DecodeConfig(bytes.NewReader(content)); err == nil && format == "gif" {
		return "", errGIF
	}
	picture, _, err := image.Decode(bytes.NewReader(content))
	if err != nil {
		return "", fmt.Errorf("%s is 1 MB or more, and PMT cannot read it to make it smaller: %w", file, err)
	}
	opaque, ok := picture.(interface{ Opaque() bool })
	transparent := !ok || !opaque.Opaque()
	if content, err = encodePicture(picture, transparent); err != nil {
		return "", fmt.Errorf("%s: %w", file, err)
	}
	folder, err := os.MkdirTemp(dir, "picture-")
	if err != nil {
		return "", err
	}
	name := strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
	smaller := filepath.Join(folder, name+lo.Ternary(transparent, ".png", ".jpg"))
	return smaller, os.WriteFile(smaller, content, 0o644)
}

// encodePicture encodes a picture under Steam's limit, as a PNG if it has transparency and a JPEG otherwise. It returns
// an error if even one pixel a side is too big.
func encodePicture(picture image.Image, transparent bool) ([]byte, error) {
	for smaller := picture; ; smaller = buildSmaller(smaller, shrinkFactor) {
		var buffer bytes.Buffer
		var err error
		if transparent {
			err = (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&buffer, smaller)
		} else {
			err = jpeg.Encode(&buffer, smaller, &jpeg.Options{Quality: jpegQuality})
		}
		if err != nil {
			return nil, err
		}
		if buffer.Len() < pictureBytes {
			return buffer.Bytes(), nil
		}
		if min(smaller.Bounds().Dx(), smaller.Bounds().Dy()) <= 1 {
			return nil, errors.New("the picture cannot be made smaller than 1 MB")
		}
	}
}

// buildSmaller returns a copy of a picture with its sides scaled down, each at least one pixel.
func buildSmaller(picture image.Image, scale float64) image.Image {
	bounds := picture.Bounds()
	width, height := max(int(float64(bounds.Dx())*scale), 1), max(int(float64(bounds.Dy())*scale), 1)
	smaller := image.NewRGBA(image.Rect(0, 0, width, height))
	// each pixel of the copy is the average of the pixels of the picture it covers
	for y := range height {
		top, bottom := bounds.Min.Y+y*bounds.Dy()/height, bounds.Min.Y+(y+1)*bounds.Dy()/height
		for x := range width {
			left, right := bounds.Min.X+x*bounds.Dx()/width, bounds.Min.X+(x+1)*bounds.Dx()/width
			var sums [4]uint64
			for row := top; row < bottom; row++ {
				for column := left; column < right; column++ {
					// the channels come premultiplied by the transparency, so averaging them weighs it in
					red, green, blue, alpha := picture.At(column, row).RGBA()
					sums[0] += uint64(red)
					sums[1] += uint64(green)
					sums[2] += uint64(blue)
					sums[3] += uint64(alpha)
				}
			}
			count := uint64(max((bottom-top)*(right-left), 1))
			offset := smaller.PixOffset(x, y)
			for channel, sum := range sums {
				smaller.Pix[offset+channel] = uint8((sum / count) >> 8)
			}
		}
	}
	return smaller
}
