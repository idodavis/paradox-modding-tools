// colors.go reads color blocks and writes a color back as its block wrote it.

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

// Color is one way script writes a color, such as rgb or hex.
type Color struct {
	// Tag is the word before the block, as rgb in rgb { 255 0 0 }. It is ""
	// for none.
	Tag string

	// HSV is set for hue, saturation and value, not red, green and blue.
	HSV bool

	// Hex is set for one part of six or eight hex digits.
	Hex bool

	// Hue and Parts are what the hue and the other parts are out of. Each is
	// 1 where it is 0.
	Hue   float64
	Parts float64
}

// TagHSV360 is the tag of a hue out of 360, saturation and value out of 100.
const TagHSV360 = "hsv360"

// Colors lists the color tags, one row each.
var Colors = []Color{
	{Tag: "rgb"},
	{Tag: ""},
	{Tag: "hsv", HSV: true},
	{Tag: TagHSV360, HSV: true, Hue: 360, Parts: 100},
	{Tag: "hex", Hex: true},
}

// RGBA is a color as red, green, blue and alpha, each from 0 to 1.
type RGBA struct {
	R float64 `json:"r"`
	G float64 `json:"g"`
	B float64 `json:"b"`
	A float64 `json:"a"`
}

// ColorForm is how to write a color with no block to copy.
type ColorForm struct {
	Tag string

	// Example is an example block's parts, giving the scale and whether it
	// has an alpha.
	Example []string
}

// ParseColor returns the color a block writes. It returns false for an unknown
// tag, a part that is no number and too few numbers.
func ParseColor(tag string, parts []string) (RGBA, bool) {
	row, known := lookupColorRow(tag)
	numbers, ok := toNumbers(row, parts)
	if !known || !ok || len(numbers) < 3 {
		return RGBA{}, false
	}

	scale := findScale(row, numbers)
	red, green, blue := numbers[0]/scale, numbers[1]/scale, numbers[2]/scale
	if row.HSV {
		red, green, blue = hsvToRGB(numbers[0]/cmp.Or(row.Hue, 1), numbers[1]/scale, numbers[2]/scale)
	}

	alpha := 1.0
	if len(numbers) > 3 {
		alpha = numbers[3] / scale
	}

	return RGBA{R: clamp(red), G: clamp(green), B: clamp(blue), A: clamp(alpha)}, true
}

// ToColor returns the color a statement writes, or false if it writes none.
func ToColor(tree *Tree, statement int32) (RGBA, bool) {
	tag, parts, ok := ColorParts(tree, statement)
	if !ok {
		return RGBA{}, false
	}

	return ParseColor(tag, parts)
}

// ColorParts returns the tag and the parts of a block such as rgb { 1 2 3 }. It
// returns false if the block is not made of numbers, or an untagged one has
// other than three or four.
func ColorParts(tree *Tree, statement int32) (tag string, parts []string, ok bool) {
	if !tree.Statements[statement].IsBlock() {
		return "", nil, false
	}

	for child := range tree.Children(statement) {
		if !tree.Statements[child].IsListed() {
			return "", nil, false
		}

		parts = append(parts, tree.Text(tree.Statements[child].Value))
	}

	tag = tree.Text(tree.Statements[statement].Tag)
	row, _ := lookupColorRow(tag)
	if _, ok := toNumbers(row, parts); !ok || tag == "" && (len(parts) < 3 || len(parts) > 4) {
		return "", nil, false
	}

	return tag, parts, true
}

// EncodeColor returns the parts that write a color as the block of the given
// tag and parts wrote its own. It returns false for an unknown tag and for
// parts that are no color.
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

	// Parts out of 1 keep two decimals, the others are whole.
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

// ToCSS returns the color as rgba(red, green, blue, alpha), the first three as
// bytes.
func (r RGBA) ToCSS() string {
	return fmt.Sprintf("rgba(%d, %d, %d, %s)", toByte(r.R), toByte(r.G), toByte(r.B),
		strconv.FormatFloat(r.A, 'f', -1, 64))
}

func lookupColorRow(tag string) (Color, bool) {
	i := slices.IndexFunc(Colors, func(row Color) bool { return row.Tag == tag })
	if i < 0 {
		return Color{}, false
	}

	return Colors[i], true
}

// toNumbers returns the numbers a block's parts write, a hex block's one part
// giving its bytes. It returns false for a part that is no number and for a
// hex part that is not six or eight digits.
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

// findScale returns what a block's numbers are out of: an hsv row's scale, else
// 1 if red, green and blue are at most 1, else 255.
func findScale(row Color, numbers []float64) float64 {
	switch {
	case row.HSV:
		return cmp.Or(row.Parts, 1)
	case !row.Hex && max(numbers[0], numbers[1], numbers[2]) <= 1:
		return 1
	}

	return 255
}

// hsvToRGB converts hue, saturation and value to RGB.
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

// rgbToHSV converts RGB to hue, saturation and value.
func rgbToHSV(red, green, blue float64) (hue, saturation, value float64) {
	value = max(red, green, blue)
	chroma := value - min(red, green, blue)
	if value > 0 {
		saturation = chroma / value
	}

	switch {
	// A gray has no hue.
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

func clamp(part float64) float64 { return min(max(part, 0), 1) }

func toByte(part float64) int { return int(part*255 + 0.5) }
