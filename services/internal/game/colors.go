// colors.go reads the color blocks script writes by the Colors rows, and writes a color back in a block's own form.

package game

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
)

// RGBA is a color, each part from 0 to 1.
type RGBA struct {
	R float64 `json:"r"`
	G float64 `json:"g"`
	B float64 `json:"b"`
	A float64 `json:"a"`
}

// ColorOf is the color the numbers of a color block make by the Colors row of its tag, matched without regard to
// case: false for a tag no row has and for fewer than three parts.
func ColorOf(tag string, parts []float64) (RGBA, bool) {
	row, known := colorRow(tag)
	if !known || len(parts) < 3 {
		return RGBA{}, false
	}
	scale := scaleOf(row, parts)
	var red, green, blue float64
	if row.HSV {
		red, green, blue = hsvToRGB(parts[0]/cmp.Or(row.Hue, 1), parts[1]/scale, parts[2]/scale)
	} else {
		red, green, blue = parts[0]/scale, parts[1]/scale, parts[2]/scale
	}
	alpha := 1.0
	if len(parts) > 3 {
		alpha = parts[3] / scale
	}
	return RGBA{clamp(red), clamp(green), clamp(blue), clamp(alpha)}, true
}

// PartsOf is ColorOf's inverse: the parts that write a color in the form of a block written before, by its tag's row,
// out of the scale its parts were in, with an alpha where it wrote one: a hex row's bytes as one 0x word, any other
// row's parts with two decimals where they are out of 1 and whole otherwise.
func PartsOf(tag string, parts []float64, color RGBA) ([]string, bool) {
	row, known := colorRow(tag)
	if !known || len(parts) < 3 {
		return nil, false
	}
	scale := scaleOf(row, parts)
	numbers := []float64{color.R * scale, color.G * scale, color.B * scale}
	if row.HSV {
		hue, saturation, value := rgbToHSV(color.R, color.G, color.B)
		numbers = []float64{hue * cmp.Or(row.Hue, 1), saturation * scale, value * scale}
	}
	if len(parts) > 3 {
		numbers = append(numbers, color.A*scale)
	}
	if row.Hex {
		hex := "0x"
		for _, number := range numbers {
			hex += fmt.Sprintf("%02x", int(math.Round(number)))
		}
		return []string{hex}, true
	}
	precision := 1.0
	if scale == 1 {
		precision = 100
	}
	texts := make([]string, len(numbers))
	for i, number := range numbers {
		texts[i] = strconv.FormatFloat(math.Round(number*precision)/precision, 'f', -1, 64)
	}
	return texts, true
}

// Hex is the color as #rrggbb, alpha left out.
func (color RGBA) Hex() string {
	return fmt.Sprintf("#%02x%02x%02x", byteOf(color.R), byteOf(color.G), byteOf(color.B))
}

// CSS is the color as rgba(red, green, blue, alpha), the parts of red, green and blue out of 255 and alpha out of 1.
func (color RGBA) CSS() string {
	return fmt.Sprintf("rgba(%d, %d, %d, %s)", byteOf(color.R), byteOf(color.G), byteOf(color.B),
		strconv.FormatFloat(color.A, 'f', -1, 64))
}

// colorRow is the Colors row of a tag, matched without regard to case.
func colorRow(tag string) (Color, bool) {
	i := slices.IndexFunc(Colors, func(row Color) bool { return strings.EqualFold(row.Tag, tag) })
	if i < 0 {
		return Color{}, false
	}
	return Colors[i], true
}

// scaleOf is what a block's parts are out of: a hsv row's own, else 255, or 1 where red, green and blue are each at
// most 1 (a hex row's bytes always out of 255).
func scaleOf(row Color, parts []float64) float64 {
	switch {
	case row.HSV:
		return cmp.Or(row.Parts, 1)
	case !row.Hex && max(parts[0], parts[1], parts[2]) <= 1:
		return 1
	}
	return 255
}

// hsvToRGB is the red, green and blue of a hue, saturation and value, each from 0 to 1.
func hsvToRGB(hue, saturation, value float64) (red, green, blue float64) {
	sector, fraction := int(hue*6)%6, hue*6-float64(int(hue*6))
	bottom, falling, rising := value*(1-saturation), value*(1-fraction*saturation), value*(1-(1-fraction)*saturation)
	switch sector {
	case 0:
		return value, rising, bottom
	case 1:
		return falling, value, bottom
	case 2:
		return bottom, value, rising
	case 3:
		return bottom, falling, value
	case 4:
		return rising, bottom, value
	}
	return value, bottom, falling
}

// rgbToHSV is the hue, saturation and value of a red, green and blue, each from 0 to 1.
func rgbToHSV(red, green, blue float64) (hue, saturation, value float64) {
	value = max(red, green, blue)
	chroma := value - min(red, green, blue)
	if value > 0 {
		saturation = chroma / value
	}
	switch {
	case chroma == 0:
	case value == red:
		hue = math.Mod((green-blue)/chroma+6, 6) / 6
	case value == green:
		hue = ((blue-red)/chroma + 2) / 6
	default:
		hue = ((red-green)/chroma + 4) / 6
	}
	return hue, saturation, value
}

// clamp is a part held to 0 through 1.
func clamp(part float64) float64 { return min(max(part, 0), 1) }

// byteOf is a part from 0 to 1 as a byte, rounded.
func byteOf(part float64) int { return int(part*255 + 0.5) }
