// colors.go reads the color blocks script writes and writes a color back the way the block wrote it.

package jomini

import (
	"cmp"
	"encoding/hex"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
)

// A Color is one way script writes a color, such as an rgb block or a hex number.
type Color struct {
	Tag   string  // the tag before the block (rgb { 255 0 0 }); "" means none
	HSV   bool    // hue, saturation, value; else red, green, blue
	Hex   bool    // one part holding the bytes of red, green and blue, and of the alpha where it has eight digits
	Hue   float64 // what the hue is out of; 0 means 1
	Parts float64 // what the other parts are out of; 0 means 1
}

// TagHSV360 is the tag of a color written as a hue out of 360, then a saturation and a value out of 100.
const TagHSV360 = "hsv360"

// Colors lists the ways script writes a color. A part of rgb, or of a block with no tag, is out of 255, or out of
// 1 when red, green and blue are each at most 1. A fourth part is the alpha. A hex block's one part is six hex digits
// for red, green and blue, or eight with the alpha last, written with or without 0x.
var Colors = []Color{
	{Tag: "rgb"}, {Tag: ""}, {Tag: "hsv", HSV: true}, {Tag: TagHSV360, HSV: true, Hue: 360, Parts: 100}, {Tag: "hex", Hex: true},
}

// An RGBA is a color as red, green, blue and alpha channels, each from 0 to 1.
type RGBA struct {
	R float64 `json:"r"` // red, 0 to 1
	G float64 `json:"g"` // green, 0 to 1
	B float64 `json:"b"` // blue, 0 to 1
	A float64 `json:"a"` // alpha, 0 to 1
}

// A ColorForm is the way to write a color where there is no block to copy: the tag before the block, and the parts
// of an example block as written. The example gives the scale and whether an alpha is written.
type ColorForm struct {
	Tag     string
	Example []string
}

// ParseColor returns the color that a block with the given tag and parts writes, each part as written. It returns
// false for a tag no row of Colors has, for a part that is no number, and for too few numbers.
func ParseColor(tag string, parts []string) (RGBA, bool) {
	row, known := lookupColorRow(tag)
	numbers, ok := toNumbers(row, parts)
	if !known || !ok || len(numbers) < 3 {
		return RGBA{}, false
	}
	scale := findScale(row, numbers)
	var red, green, blue float64
	if row.HSV {
		red, green, blue = hsvToRGB(numbers[0]/cmp.Or(row.Hue, 1), numbers[1]/scale, numbers[2]/scale)
	} else {
		red, green, blue = numbers[0]/scale, numbers[1]/scale, numbers[2]/scale
	}
	alpha := 1.0
	if len(numbers) > 3 {
		alpha = numbers[3] / scale
	}
	return RGBA{clamp(red), clamp(green), clamp(blue), clamp(alpha)}, true
}

// ToColor returns the color a statement writes as a color block. It returns false if the statement is no color block
// or its tag and parts make no color.
func ToColor(tree *Tree, statement int32) (RGBA, bool) {
	tag, parts, ok := ColorParts(tree, statement)
	if !ok {
		return RGBA{}, false
	}
	return ParseColor(tag, parts)
}

// EncodeColor returns the parts that write the given color the way an existing block wrote its own, given that
// block's tag and parts. A hex part keeps its number of digits, and its 0x where it has one. It returns false for a
// tag no row of Colors has and for parts that write no color.
func EncodeColor(tag string, parts []string, color RGBA) ([]string, bool) {
	row, known := lookupColorRow(tag)
	written, ok := toNumbers(row, parts)
	if !known || !ok || len(written) < 3 {
		return nil, false
	}
	scale := findScale(row, written)
	numbers := []float64{color.R * scale, color.G * scale, color.B * scale}
	if row.HSV {
		hue, saturation, value := rgbToHSV(color.R, color.G, color.B)
		numbers = []float64{hue * cmp.Or(row.Hue, 1), saturation * scale, value * scale}
	}
	if len(written) > 3 {
		numbers = append(numbers, color.A*scale)
	}
	if row.Hex {
		encoded := ""
		if strings.HasPrefix(parts[0], "0x") {
			encoded = "0x"
		}
		for _, number := range numbers {
			encoded += fmt.Sprintf("%02x", int(math.Round(number)))
		}
		return []string{encoded}, true
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

// lookupColorRow returns the Colors row for a tag, or false if there is none.
func lookupColorRow(tag string) (Color, bool) {
	i := slices.IndexFunc(Colors, func(row Color) bool { return row.Tag == tag })
	if i < 0 {
		return Color{}, false
	}
	return Colors[i], true
}

// toNumbers returns the numbers that the parts of a color block of the given row write. A hex block's one part gives
// its bytes, with or without 0x before them: red, green and blue, and the alpha where it has eight digits. It returns
// false for a part that is no number, and for a hex block that is not one part of six or eight digits.
func toNumbers(row Color, parts []string) ([]float64, bool) {
	if row.Hex {
		if len(parts) != 1 {
			return nil, false
		}
		channels, err := hex.DecodeString(strings.TrimPrefix(parts[0], "0x"))
		if err != nil || len(channels) != 3 && len(channels) != 4 {
			return nil, false
		}
		numbers := make([]float64, len(channels))
		for i, channel := range channels {
			numbers[i] = float64(channel)
		}
		return numbers, true
	}
	numbers := make([]float64, len(parts))
	for i, part := range parts {
		number, ok := parseNumber(part)
		if !ok {
			return nil, false
		}
		numbers[i] = number
	}
	return numbers, true
}

// findScale returns what a block's numbers are out of: an hsv row's own scale, else 255, or 1 when red, green and
// blue are each at most 1. A hex row's bytes are always out of 255.
func findScale(row Color, numbers []float64) float64 {
	switch {
	case row.HSV:
		return cmp.Or(row.Parts, 1)
	case !row.Hex && max(numbers[0], numbers[1], numbers[2]) <= 1:
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
	case chroma == 0: // a gray has no hue
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
