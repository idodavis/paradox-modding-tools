// colors.go reads the color blocks script writes and writes a color back the way the block wrote it.

package game

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strconv"

	"paradox-modding-tools/internal/jomini"
)

// A Color is one way script writes a color, such as an rgb block or a hex number.
type Color struct {
	Tag   string  // the tag before the block (rgb { 255 0 0 }); "" means none
	HSV   bool    // hue, saturation, value; else red, green, blue
	Hex   bool    // one hex word, given as its bytes
	Hue   float64 // what the hue is out of; 0 means 1
	Parts float64 // what the other parts are out of; 0 means 1
}

// Colors lists the ways script writes a color. A part of rgb, or of a block with no tag, is out of 255, or out of
// 1 when red, green and blue are each at most 1. A fourth part is the alpha.
var Colors = []Color{
	{Tag: "rgb"}, {Tag: ""}, {Tag: "hsv", HSV: true}, {Tag: "hsv360", HSV: true, Hue: 360, Parts: 100}, {Tag: "hex", Hex: true},
}

// An RGBA is a color as red, green, blue and alpha channels, each from 0 to 1.
type RGBA struct {
	R float64 `json:"r"` // red, 0 to 1
	G float64 `json:"g"` // green, 0 to 1
	B float64 `json:"b"` // blue, 0 to 1
	A float64 `json:"a"` // alpha, 0 to 1
}

// ParseColor returns the color that a block's tag and numbers write. The tag must match a row of Colors exactly. A
// hex row takes one number, the bytes of red, green, blue and alpha. It returns false for a tag no row has and for
// fewer than three numbers, a hex row's one number apart.
func ParseColor(tag string, parts []float64) (RGBA, bool) {
	row, known := lookupColorRow(tag)
	if known && row.Hex && len(parts) == 1 {
		word := uint32(parts[0])
		return RGBA{float64(word>>24) / 255, float64(word>>16&0xff) / 255, float64(word>>8&0xff) / 255, float64(word&0xff) / 255}, true
	}
	if !known || len(parts) < 3 {
		return RGBA{}, false
	}
	scale := findScale(row, parts)
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

// ToColor returns the color a statement writes as a color block. It returns false if the statement is no color block
// or its tag and parts make no color.
func ToColor(tree *jomini.Tree, statement int32) (RGBA, bool) {
	tag, parts, ok := jomini.ColorParts(tree, statement)
	if !ok {
		return RGBA{}, false
	}
	return ParseColor(tag, parts)
}

// EncodeColor returns the numbers that write a color the way an existing block wrote its own: in the same scale, and
// with an alpha only if that block had one. The tag and the parts are that block's. A hex row gives one 0x number;
// other rows give two decimals when out of 1 and whole numbers otherwise. It returns false for a tag no row has and
// for fewer than three parts.
func EncodeColor(tag string, parts []float64, color RGBA) ([]string, bool) {
	row, known := lookupColorRow(tag)
	if !known || len(parts) < 3 {
		return nil, false
	}
	scale := findScale(row, parts)
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

// ToCSS returns the color as rgba(red, green, blue, alpha), the first three out of 255 and the alpha out of 1.
func (color RGBA) ToCSS() string {
	return fmt.Sprintf("rgba(%d, %d, %d, %s)", toByte(color.R), toByte(color.G), toByte(color.B),
		strconv.FormatFloat(color.A, 'f', -1, 64))
}

// lookupColorRow returns the Colors row for a tag, matched exactly, or false if there is none.
func lookupColorRow(tag string) (Color, bool) {
	i := slices.IndexFunc(Colors, func(row Color) bool { return row.Tag == tag })
	if i < 0 {
		return Color{}, false
	}
	return Colors[i], true
}

// findScale returns what a block's parts are out of: an hsv row's own scale, else 255, or 1 when red, green and
// blue are each at most 1. A hex row's bytes are always out of 255.
func findScale(row Color, parts []float64) float64 {
	switch {
	case row.HSV:
		return cmp.Or(row.Parts, 1)
	case !row.Hex && max(parts[0], parts[1], parts[2]) <= 1:
		return 1
	}
	return 255
}

// hsvToRGB converts a hue, saturation and value to red, green and blue, each from 0 to 1.
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

// rgbToHSV converts red, green and blue to a hue, saturation and value, each from 0 to 1.
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

// clamp limits a part to the range 0 to 1.
func clamp(part float64) float64 { return min(max(part, 0), 1) }

// toByte returns a part from 0 to 1 as a byte, rounded.
func toByte(part float64) int { return int(part*255 + 0.5) }
