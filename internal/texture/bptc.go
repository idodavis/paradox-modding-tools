package texture

import (
	"math"
	"math/bits"
	"slices"
	"strconv"
	"strings"
)

// weights are the interpolation weights of a BC6H or BC7 index, by the index's bits and then by its value.
var weights = [5][]int{
	2: {0, 21, 43, 64},
	3: {0, 9, 18, 27, 37, 46, 55, 64},
	4: {0, 4, 9, 13, 17, 21, 26, 30, 34, 38, 43, 47, 51, 55, 60, 64},
}

// bc7Modes are the eight BC7 block modes: the subsets, the bits of the partition, rotation and index selection
// fields, the bits of a color and of an alpha endpoint, whether each endpoint or each subset carries a P-bit, and
// the bits of an index and of a secondary index.
var bc7Modes = [8]struct {
	subsets, partitionBits, rotationBits, selectionBits int
	colorBits, alphaBits, endpointPBits, sharedPBits    int
	indexBits, indexBits2                               int
}{
	{3, 4, 0, 0, 4, 0, 1, 0, 3, 0},
	{2, 6, 0, 0, 6, 0, 0, 1, 3, 0},
	{3, 6, 0, 0, 5, 0, 0, 0, 2, 0},
	{2, 6, 0, 0, 7, 0, 1, 0, 2, 0},
	{1, 0, 2, 1, 5, 6, 0, 0, 2, 3},
	{1, 0, 2, 0, 7, 8, 0, 0, 2, 2},
	{1, 0, 0, 0, 7, 7, 1, 0, 4, 0},
	{2, 6, 0, 0, 5, 5, 1, 0, 2, 0},
}

// bc6hModes are the BC6H block modes by mode number: whether the other endpoints are deltas from the first, the
// subsets, the bits of the partition field, the bits of an index, the bits of an endpoint, the bits of each
// channel's delta, and the fields of the mode and the endpoints in the order the block stores them, as the
// specification writes them. A field written high bit first is stored reversed. A mode the format reserves has
// no fields.
var bc6hModes = [32]struct {
	transformed                                     bool
	subsets, partitionBits, indexBits, endpointBits int
	deltaBits                                       [3]int
	fields                                          string
}{
	0: {true, 2, 5, 3, 10, [3]int{5, 5, 5}, "m[1:0],g2[4],b2[4],b3[4],r0[9:0],g0[9:0],b0[9:0],r1[4:0],g3[4]," +
		"g2[3:0],g1[4:0],b3[0],g3[3:0],b1[4:0],b3[1],b2[3:0],r2[4:0],b3[2],r3[4:0],b3[3]"},
	1: {true, 2, 5, 3, 7, [3]int{6, 6, 6}, "m[1:0],g2[5],g3[4],g3[5],r0[6:0],b3[0],b3[1],b2[4],g0[6:0],b2[5]," +
		"b3[2],g2[4],b0[6:0],b3[3],b3[5],b3[4],r1[5:0],g2[3:0],g1[5:0],g3[3:0],b1[5:0],b2[3:0],r2[5:0],r3[5:0]"},
	2: {true, 2, 5, 3, 11, [3]int{5, 4, 4}, "m[4:0],r0[9:0],g0[9:0],b0[9:0],r1[4:0],r0[10],g2[3:0],g1[3:0]," +
		"g0[10],b3[0],g3[3:0],b1[3:0],b0[10],b3[1],b2[3:0],r2[4:0],b3[2],r3[4:0],b3[3]"},
	6: {true, 2, 5, 3, 11, [3]int{4, 5, 4}, "m[4:0],r0[9:0],g0[9:0],b0[9:0],r1[3:0],r0[10],g3[4],g2[3:0]," +
		"g1[4:0],g0[10],g3[3:0],b1[3:0],b0[10],b3[1],b2[3:0],r2[3:0],b3[0],b3[2],r3[3:0],g2[4],b3[3]"},
	10: {true, 2, 5, 3, 11, [3]int{4, 4, 5}, "m[4:0],r0[9:0],g0[9:0],b0[9:0],r1[3:0],r0[10],b2[4],g2[3:0]," +
		"g1[3:0],g0[10],b3[0],g3[3:0],b1[4:0],b0[10],b2[3:0],r2[3:0],b3[1],b3[2],r3[3:0],b3[4],b3[3]"},
	14: {true, 2, 5, 3, 9, [3]int{5, 5, 5}, "m[4:0],r0[8:0],b2[4],g0[8:0],g2[4],b0[8:0],b3[4],r1[4:0],g3[4]," +
		"g2[3:0],g1[4:0],b3[0],g3[3:0],b1[4:0],b3[1],b2[3:0],r2[4:0],b3[2],r3[4:0],b3[3]"},
	18: {true, 2, 5, 3, 8, [3]int{6, 5, 5}, "m[4:0],r0[7:0],g3[4],b2[4],g0[7:0],b3[2],g2[4],b0[7:0],b3[3]," +
		"b3[4],r1[5:0],g2[3:0],g1[4:0],b3[0],g3[3:0],b1[4:0],b3[1],b2[3:0],r2[5:0],r3[5:0]"},
	22: {true, 2, 5, 3, 8, [3]int{5, 6, 5}, "m[4:0],r0[7:0],b3[0],b2[4],g0[7:0],g2[5],g2[4],b0[7:0],g3[5]," +
		"b3[4],r1[4:0],g3[4],g2[3:0],g1[5:0],g3[3:0],b1[4:0],b3[1],b2[3:0],r2[4:0],b3[2],r3[4:0],b3[3]"},
	26: {true, 2, 5, 3, 8, [3]int{5, 5, 6}, "m[4:0],r0[7:0],b3[1],b2[4],g0[7:0],b2[5],g2[4],b0[7:0],b3[5]," +
		"b3[4],r1[4:0],g3[4],g2[3:0],g1[4:0],b3[0],g3[3:0],b1[5:0],b2[3:0],r2[4:0],b3[2],r3[4:0],b3[3]"},
	30: {false, 2, 5, 3, 6, [3]int{6, 6, 6}, "m[4:0],r0[5:0],g3[4],b3[0],b3[1],b2[4],g0[5:0],g2[5],b2[5]," +
		"b3[2],g2[4],b0[5:0],g3[5],b3[3],b3[5],b3[4],r1[5:0],g2[3:0],g1[5:0],g3[3:0],b1[5:0],b2[3:0],r2[5:0]," +
		"r3[5:0]"},
	3: {false, 1, 0, 4, 10, [3]int{10, 10, 10}, "m[4:0],r0[9:0],g0[9:0],b0[9:0],r1[9:0],g1[9:0],b1[9:0]"},
	7: {true, 1, 0, 4, 11, [3]int{9, 9, 9}, "m[4:0],r0[9:0],g0[9:0],b0[9:0],r1[8:0],r0[10],g1[8:0],g0[10]," +
		"b1[8:0],b0[10]"},
	11: {true, 1, 0, 4, 12, [3]int{8, 8, 8}, "m[4:0],r0[9:0],g0[9:0],b0[9:0],r1[7:0],r0[10:11],g1[7:0]," +
		"g0[10:11],b1[7:0],b0[10:11]"},
	15: {true, 1, 0, 4, 16, [3]int{4, 4, 4}, "m[4:0],r0[9:0],g0[9:0],b0[9:0],r1[3:0],r0[10:15],g1[3:0]," +
		"g0[10:15],b1[3:0],b0[10:15]"},
}

// bc6hFields are the fields of each BC6H mode parsed from its row: for each, the endpoint it fills, the channel,
// and the high and low bit of the field, in the order the block stores them. The mode field has endpoint -1.
var bc6hFields = parseBC6HFields()

// partitions2 and partitions3 say which subset each pixel of a block is in, by partition and then by pixel, for
// blocks of two subsets and of three. A BC6H partition is one of the first 32 of two subsets.
var partitions2 = [64][16]uint8{
	{0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 1, 1},
	{0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0, 1},
	{0, 1, 1, 1, 0, 1, 1, 1, 0, 1, 1, 1, 0, 1, 1, 1},
	{0, 0, 0, 1, 0, 0, 1, 1, 0, 0, 1, 1, 0, 1, 1, 1},
	{0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 1, 1},
	{0, 0, 1, 1, 0, 1, 1, 1, 0, 1, 1, 1, 1, 1, 1, 1},
	{0, 0, 0, 1, 0, 0, 1, 1, 0, 1, 1, 1, 1, 1, 1, 1},
	{0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 1, 1, 0, 1, 1, 1},
	{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 1, 1},
	{0, 0, 1, 1, 0, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1},
	{0, 0, 0, 0, 0, 0, 0, 1, 0, 1, 1, 1, 1, 1, 1, 1},
	{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 1, 1, 1},
	{0, 0, 0, 1, 0, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1},
	{0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 1, 1, 1, 1, 1},
	{0, 0, 0, 0, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1},
	{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 1},
	{0, 0, 0, 0, 1, 0, 0, 0, 1, 1, 1, 0, 1, 1, 1, 1},
	{0, 1, 1, 1, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0},
	{0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 1, 1, 1, 0},
	{0, 1, 1, 1, 0, 0, 1, 1, 0, 0, 0, 1, 0, 0, 0, 0},
	{0, 0, 1, 1, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0},
	{0, 0, 0, 0, 1, 0, 0, 0, 1, 1, 0, 0, 1, 1, 1, 0},
	{0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 1, 1, 0, 0},
	{0, 1, 1, 1, 0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 0, 1},
	{0, 0, 1, 1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0, 0},
	{0, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0, 1, 1, 0, 0},
	{0, 1, 1, 0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 1, 1, 0},
	{0, 0, 1, 1, 0, 1, 1, 0, 0, 1, 1, 0, 1, 1, 0, 0},
	{0, 0, 0, 1, 0, 1, 1, 1, 1, 1, 1, 0, 1, 0, 0, 0},
	{0, 0, 0, 0, 1, 1, 1, 1, 1, 1, 1, 1, 0, 0, 0, 0},
	{0, 1, 1, 1, 0, 0, 0, 1, 1, 0, 0, 0, 1, 1, 1, 0},
	{0, 0, 1, 1, 1, 0, 0, 1, 1, 0, 0, 1, 1, 1, 0, 0},
	{0, 1, 0, 1, 0, 1, 0, 1, 0, 1, 0, 1, 0, 1, 0, 1},
	{0, 0, 0, 0, 1, 1, 1, 1, 0, 0, 0, 0, 1, 1, 1, 1},
	{0, 1, 0, 1, 1, 0, 1, 0, 0, 1, 0, 1, 1, 0, 1, 0},
	{0, 0, 1, 1, 0, 0, 1, 1, 1, 1, 0, 0, 1, 1, 0, 0},
	{0, 0, 1, 1, 1, 1, 0, 0, 0, 0, 1, 1, 1, 1, 0, 0},
	{0, 1, 0, 1, 0, 1, 0, 1, 1, 0, 1, 0, 1, 0, 1, 0},
	{0, 1, 1, 0, 1, 0, 0, 1, 0, 1, 1, 0, 1, 0, 0, 1},
	{0, 1, 0, 1, 1, 0, 1, 0, 1, 0, 1, 0, 0, 1, 0, 1},
	{0, 1, 1, 1, 0, 0, 1, 1, 1, 1, 0, 0, 1, 1, 1, 0},
	{0, 0, 0, 1, 0, 0, 1, 1, 1, 1, 0, 0, 1, 0, 0, 0},
	{0, 0, 1, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1, 1, 0, 0},
	{0, 0, 1, 1, 1, 0, 1, 1, 1, 1, 0, 1, 1, 1, 0, 0},
	{0, 1, 1, 0, 1, 0, 0, 1, 1, 0, 0, 1, 0, 1, 1, 0},
	{0, 0, 1, 1, 1, 1, 0, 0, 1, 1, 0, 0, 0, 0, 1, 1},
	{0, 1, 1, 0, 0, 1, 1, 0, 1, 0, 0, 1, 1, 0, 0, 1},
	{0, 0, 0, 0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 0, 0, 0},
	{0, 1, 0, 0, 1, 1, 1, 0, 0, 1, 0, 0, 0, 0, 0, 0},
	{0, 0, 1, 0, 0, 1, 1, 1, 0, 0, 1, 0, 0, 0, 0, 0},
	{0, 0, 0, 0, 0, 0, 1, 0, 0, 1, 1, 1, 0, 0, 1, 0},
	{0, 0, 0, 0, 0, 1, 0, 0, 1, 1, 1, 0, 0, 1, 0, 0},
	{0, 1, 1, 0, 1, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1, 1},
	{0, 0, 1, 1, 0, 1, 1, 0, 1, 1, 0, 0, 1, 0, 0, 1},
	{0, 1, 1, 0, 0, 0, 1, 1, 1, 0, 0, 1, 1, 1, 0, 0},
	{0, 0, 1, 1, 1, 0, 0, 1, 1, 1, 0, 0, 0, 1, 1, 0},
	{0, 1, 1, 0, 1, 1, 0, 0, 1, 1, 0, 0, 1, 0, 0, 1},
	{0, 1, 1, 0, 0, 0, 1, 1, 0, 0, 1, 1, 1, 0, 0, 1},
	{0, 1, 1, 1, 1, 1, 1, 0, 1, 0, 0, 0, 0, 0, 0, 1},
	{0, 0, 0, 1, 1, 0, 0, 0, 1, 1, 1, 0, 0, 1, 1, 1},
	{0, 0, 0, 0, 1, 1, 1, 1, 0, 0, 1, 1, 0, 0, 1, 1},
	{0, 0, 1, 1, 0, 0, 1, 1, 1, 1, 1, 1, 0, 0, 0, 0},
	{0, 0, 1, 0, 0, 0, 1, 0, 1, 1, 1, 0, 1, 1, 1, 0},
	{0, 1, 0, 0, 0, 1, 0, 0, 0, 1, 1, 1, 0, 1, 1, 1},
}

var partitions3 = [64][16]uint8{
	{0, 0, 1, 1, 0, 0, 1, 1, 0, 2, 2, 1, 2, 2, 2, 2},
	{0, 0, 0, 1, 0, 0, 1, 1, 2, 2, 1, 1, 2, 2, 2, 1},
	{0, 0, 0, 0, 2, 0, 0, 1, 2, 2, 1, 1, 2, 2, 1, 1},
	{0, 2, 2, 2, 0, 0, 2, 2, 0, 0, 1, 1, 0, 1, 1, 1},
	{0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 2, 2, 1, 1, 2, 2},
	{0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 2, 2, 0, 0, 2, 2},
	{0, 0, 2, 2, 0, 0, 2, 2, 1, 1, 1, 1, 1, 1, 1, 1},
	{0, 0, 1, 1, 0, 0, 1, 1, 2, 2, 1, 1, 2, 2, 1, 1},
	{0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 1, 2, 2, 2, 2},
	{0, 0, 0, 0, 1, 1, 1, 1, 1, 1, 1, 1, 2, 2, 2, 2},
	{0, 0, 0, 0, 1, 1, 1, 1, 2, 2, 2, 2, 2, 2, 2, 2},
	{0, 0, 1, 2, 0, 0, 1, 2, 0, 0, 1, 2, 0, 0, 1, 2},
	{0, 1, 1, 2, 0, 1, 1, 2, 0, 1, 1, 2, 0, 1, 1, 2},
	{0, 1, 2, 2, 0, 1, 2, 2, 0, 1, 2, 2, 0, 1, 2, 2},
	{0, 0, 1, 1, 0, 1, 1, 2, 1, 1, 2, 2, 1, 2, 2, 2},
	{0, 0, 1, 1, 2, 0, 0, 1, 2, 2, 0, 0, 2, 2, 2, 0},
	{0, 0, 0, 1, 0, 0, 1, 1, 0, 1, 1, 2, 1, 1, 2, 2},
	{0, 1, 1, 1, 0, 0, 1, 1, 2, 0, 0, 1, 2, 2, 0, 0},
	{0, 0, 0, 0, 1, 1, 2, 2, 1, 1, 2, 2, 1, 1, 2, 2},
	{0, 0, 2, 2, 0, 0, 2, 2, 0, 0, 2, 2, 1, 1, 1, 1},
	{0, 1, 1, 1, 0, 1, 1, 1, 0, 2, 2, 2, 0, 2, 2, 2},
	{0, 0, 0, 1, 0, 0, 0, 1, 2, 2, 2, 1, 2, 2, 2, 1},
	{0, 0, 0, 0, 0, 0, 1, 1, 0, 1, 2, 2, 0, 1, 2, 2},
	{0, 0, 0, 0, 1, 1, 0, 0, 2, 2, 1, 0, 2, 2, 1, 0},
	{0, 1, 2, 2, 0, 1, 2, 2, 0, 0, 1, 1, 0, 0, 0, 0},
	{0, 0, 1, 2, 0, 0, 1, 2, 1, 1, 2, 2, 2, 2, 2, 2},
	{0, 1, 1, 0, 1, 2, 2, 1, 1, 2, 2, 1, 0, 1, 1, 0},
	{0, 0, 0, 0, 0, 1, 1, 0, 1, 2, 2, 1, 1, 2, 2, 1},
	{0, 0, 2, 2, 1, 1, 0, 2, 1, 1, 0, 2, 0, 0, 2, 2},
	{0, 1, 1, 0, 0, 1, 1, 0, 2, 0, 0, 2, 2, 2, 2, 2},
	{0, 0, 1, 1, 0, 1, 2, 2, 0, 1, 2, 2, 0, 0, 1, 1},
	{0, 0, 0, 0, 2, 0, 0, 0, 2, 2, 1, 1, 2, 2, 2, 1},
	{0, 0, 0, 0, 0, 0, 0, 2, 1, 1, 2, 2, 1, 2, 2, 2},
	{0, 2, 2, 2, 0, 0, 2, 2, 0, 0, 1, 2, 0, 0, 1, 1},
	{0, 0, 1, 1, 0, 0, 1, 2, 0, 0, 2, 2, 0, 2, 2, 2},
	{0, 1, 2, 0, 0, 1, 2, 0, 0, 1, 2, 0, 0, 1, 2, 0},
	{0, 0, 0, 0, 1, 1, 1, 1, 2, 2, 2, 2, 0, 0, 0, 0},
	{0, 1, 2, 0, 1, 2, 0, 1, 2, 0, 1, 2, 0, 1, 2, 0},
	{0, 1, 2, 0, 2, 0, 1, 2, 1, 2, 0, 1, 0, 1, 2, 0},
	{0, 0, 1, 1, 2, 2, 0, 0, 1, 1, 2, 2, 0, 0, 1, 1},
	{0, 0, 1, 1, 1, 1, 2, 2, 2, 2, 0, 0, 0, 0, 1, 1},
	{0, 1, 0, 1, 0, 1, 0, 1, 2, 2, 2, 2, 2, 2, 2, 2},
	{0, 0, 0, 0, 0, 0, 0, 0, 2, 1, 2, 1, 2, 1, 2, 1},
	{0, 0, 2, 2, 1, 1, 2, 2, 0, 0, 2, 2, 1, 1, 2, 2},
	{0, 0, 2, 2, 0, 0, 1, 1, 0, 0, 2, 2, 0, 0, 1, 1},
	{0, 2, 2, 0, 1, 2, 2, 1, 0, 2, 2, 0, 1, 2, 2, 1},
	{0, 1, 0, 1, 2, 2, 2, 2, 2, 2, 2, 2, 0, 1, 0, 1},
	{0, 0, 0, 0, 2, 1, 2, 1, 2, 1, 2, 1, 2, 1, 2, 1},
	{0, 1, 0, 1, 0, 1, 0, 1, 0, 1, 0, 1, 2, 2, 2, 2},
	{0, 2, 2, 2, 0, 1, 1, 1, 0, 2, 2, 2, 0, 1, 1, 1},
	{0, 0, 0, 2, 1, 1, 1, 2, 0, 0, 0, 2, 1, 1, 1, 2},
	{0, 0, 0, 0, 2, 1, 1, 2, 2, 1, 1, 2, 2, 1, 1, 2},
	{0, 2, 2, 2, 0, 1, 1, 1, 0, 1, 1, 1, 0, 2, 2, 2},
	{0, 0, 0, 2, 1, 1, 1, 2, 1, 1, 1, 2, 0, 0, 0, 2},
	{0, 1, 1, 0, 0, 1, 1, 0, 0, 1, 1, 0, 2, 2, 2, 2},
	{0, 0, 0, 0, 0, 0, 0, 0, 2, 1, 1, 2, 2, 1, 1, 2},
	{0, 1, 1, 0, 0, 1, 1, 0, 2, 2, 2, 2, 2, 2, 2, 2},
	{0, 0, 2, 2, 0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 2, 2},
	{0, 0, 2, 2, 1, 1, 2, 2, 1, 1, 2, 2, 0, 0, 2, 2},
	{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 2, 1, 1, 2},
	{0, 0, 0, 2, 0, 0, 0, 1, 0, 0, 0, 2, 0, 0, 0, 1},
	{0, 2, 2, 2, 1, 2, 2, 2, 0, 2, 2, 2, 1, 2, 2, 2},
	{0, 1, 0, 1, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2},
	{0, 1, 1, 1, 2, 0, 1, 1, 2, 2, 0, 1, 2, 2, 2, 0},
}

// anchors2 is the anchor pixel of the second subset of a two-subset partition, and anchors3a and anchors3b the
// anchors of the second and third subsets of a three-subset one. The first subset's anchor is always pixel 0.
var anchors2 = [64]uint8{
	15, 15, 15, 15, 15, 15, 15, 15,
	15, 15, 15, 15, 15, 15, 15, 15,
	15, 2, 8, 2, 2, 8, 8, 15,
	2, 8, 2, 2, 8, 8, 2, 2,
	15, 15, 6, 8, 2, 8, 15, 15,
	2, 8, 2, 2, 2, 15, 15, 6,
	6, 2, 6, 8, 15, 15, 2, 2,
	15, 15, 15, 15, 15, 2, 2, 15,
}

var anchors3a = [64]uint8{
	3, 3, 15, 15, 8, 3, 15, 15,
	8, 8, 6, 6, 6, 5, 3, 3,
	3, 3, 8, 15, 3, 3, 6, 10,
	5, 8, 8, 6, 8, 5, 15, 15,
	8, 15, 3, 5, 6, 10, 8, 15,
	15, 3, 15, 5, 15, 15, 15, 15,
	3, 15, 5, 5, 5, 8, 5, 10,
	5, 10, 8, 13, 15, 12, 3, 3,
}

var anchors3b = [64]uint8{
	15, 8, 8, 3, 15, 15, 3, 8,
	15, 15, 15, 15, 15, 15, 15, 8,
	15, 8, 15, 3, 15, 8, 15, 8,
	3, 15, 6, 10, 15, 15, 10, 8,
	15, 3, 15, 10, 10, 8, 9, 10,
	6, 15, 8, 15, 3, 6, 6, 8,
	15, 3, 15, 15, 15, 15, 15, 15,
	15, 15, 15, 15, 3, 15, 15, 8,
}

// decodeBC6H decodes one BC6H block in any of its fourteen modes, each channel's half float clamped to 0 to 1
// and scaled to a byte, with alpha 255. A block in a reserved mode decodes to black. It takes whether the format
// is the signed one, which changes how the endpoints are read and scaled.
func decodeBC6H(block []byte, pixels *[16][4]uint8, signed bool) {
	mode := int(block[0] & 3)
	if mode >= 2 {
		mode = int(block[0] & 31)
	}
	row := bc6hModes[mode]
	for i := range 16 {
		pixels[i] = [4]uint8{0, 0, 0, 255}
	}
	if row.fields == "" {
		return
	}
	endpoints, position := readBC6HEndpoints(block, mode, signed)
	partition := int(readBits(block, position, row.partitionBits))
	position += row.partitionBits
	indices, _ := readIndices(block, position, row.indexBits, lookupAnchors(row.subsets, partition))
	var palettes [2][16][3]uint16
	for subset := range row.subsets {
		pair := [2][3]int{endpoints[2*subset], endpoints[2*subset+1]}
		palettes[subset] = resolveBC6HPalette(pair, mode, signed)
	}
	for i := range 16 {
		half := palettes[lookupSubset(row.subsets, partition, i)][indices[i]]
		for c := range 3 {
			pixels[i][c] = uint8(min(max(toFloat(half[c]), 0), 1)*255 + 0.5)
		}
	}
}

// readBC6HEndpoints reads the endpoints of a BC6H block as the mode's fields lay them out, sign-extends what the
// mode or the signed format stores signed, and adds the first endpoint to the others when the mode stores them
// as deltas. It returns the endpoints, by endpoint and then by channel, and the bit position after the fields.
func readBC6HEndpoints(block []byte, mode int, signed bool) ([4][3]int, int) {
	row := bc6hModes[mode]
	var endpoints [4][3]int
	position := 0
	for _, field := range bc6hFields[mode] {
		endpoint, channel, high, low := field[0], field[1], field[2], field[3]
		count := max(high, low) - min(high, low) + 1
		for i := range count {
			if endpoint >= 0 {
				// The first bit read lands at the field's low end, or at its high end when the field is reversed.
				target := low + i
				if high < low {
					target = low - i
				}
				endpoints[endpoint][channel] |= int(readBits(block, position+i, 1)) << target
			}
		}
		position += count
	}
	for c := range 3 {
		if signed {
			endpoints[0][c] = toSigned(endpoints[0][c], row.endpointBits)
		}
		for e := 1; e < 2*row.subsets; e++ {
			if signed || row.transformed {
				endpoints[e][c] = toSigned(endpoints[e][c], row.deltaBits[c])
			}
			if row.transformed {
				endpoints[e][c] = (endpoints[0][c] + endpoints[e][c]) & (1<<row.endpointBits - 1)
			}
			if row.transformed && signed {
				endpoints[e][c] = toSigned(endpoints[e][c], row.endpointBits)
			}
		}
	}
	return endpoints, position
}

// readBits reads a count of bits of a block from the given bit position, the lowest first.
func readBits(block []byte, position, count int) uint32 {
	var value uint32
	for i := range count {
		bit := position + i
		value |= uint32(block[bit/8]>>(bit%8)&1) << i
	}
	return value
}

// toSigned reads a value of the given bits as two's complement.
func toSigned(value, width int) int {
	if value&(1<<(width-1)) != 0 {
		return value - 1<<width
	}
	return value
}

// readIndices reads the sixteen indices of a block from the given bit position, each of the given bits except
// the anchors, pixel 0 and the given others, which have one bit fewer. It returns the indices and the bit
// position after them.
func readIndices(block []byte, position, width int, anchors []int) ([16]uint8, int) {
	var indices [16]uint8
	for i := range 16 {
		count := width
		if i == 0 || slices.Contains(anchors, i) {
			count--
		}
		indices[i] = uint8(readBits(block, position, count))
		position += count
	}
	return indices, position
}

// lookupAnchors lists the anchor pixels of a partition's subsets after the first, whose anchor is pixel 0.
func lookupAnchors(subsets, partition int) []int {
	switch subsets {
	case 2:
		return []int{int(anchors2[partition])}
	case 3:
		return []int{int(anchors3a[partition]), int(anchors3b[partition])}
	}
	return nil
}

// resolveBC6HPalette builds the colors a subset's indices pick: the two endpoints unquantized to sixteen bits,
// interpolated with the mode's weights, and scaled to half floats.
func resolveBC6HPalette(endpoints [2][3]int, mode int, signed bool) [16][3]uint16 {
	row := bc6hModes[mode]
	var palette [16][3]uint16
	for c := range 3 {
		first := toUnquantized(endpoints[0][c], row.endpointBits, signed)
		second := toUnquantized(endpoints[1][c], row.endpointBits, signed)
		for i, weight := range weights[row.indexBits] {
			palette[i][c] = toHalf((first*(64-weight)+second*weight+32)>>6, signed)
		}
	}
	return palette
}

// toUnquantized scales an endpoint of the given bits to the range the interpolation works in: -0x7FFF to 0x7FFF
// for the signed format, 0 to 0xFFFF for the unsigned one.
func toUnquantized(value, width int, signed bool) int {
	if !signed {
		switch {
		case width >= 15:
			return value
		case value == 0:
			return 0
		case value == 1<<width-1:
			return 0xFFFF
		}
		return (value<<15 + 0x4000) >> (width - 1)
	}
	if width >= 16 {
		return value
	}
	sign, magnitude := 1, value
	if value < 0 {
		sign, magnitude = -1, -value
	}
	switch {
	case magnitude == 0:
		return 0
	case magnitude >= 1<<(width-1)-1:
		return sign * 0x7FFF
	}
	return sign * ((magnitude<<15 + 0x4000) >> (width - 1))
}

// toHalf scales an interpolated value to the bits of a half float: by 31/64 for the unsigned format, by 31/32
// with the sign bit set apart for the signed one.
func toHalf(value int, signed bool) uint16 {
	if !signed {
		return uint16(value * 31 >> 6)
	}
	if value < 0 {
		return 0x8000 | uint16(-value*31>>5)
	}
	return uint16(value * 31 >> 5)
}

// lookupSubset returns the subset a pixel of a block is in.
func lookupSubset(subsets, partition, pixel int) int {
	switch subsets {
	case 2:
		return int(partitions2[partition][pixel])
	case 3:
		return int(partitions3[partition][pixel])
	}
	return 0
}

// toFloat converts the bits of a half float to a float.
func toFloat(half uint16) float32 {
	sign := uint32(half>>15) << 31
	exponent := int(half>>10) & 31
	mantissa := uint32(half & 1023)
	switch exponent {
	case 0:
		value := float32(math.Ldexp(float64(mantissa), -24))
		return math.Float32frombits(sign | math.Float32bits(value))
	case 31:
		return math.Float32frombits(sign | 0x7F800000 | mantissa<<13)
	}
	return math.Float32frombits(sign | uint32(exponent+112)<<23 | mantissa<<13)
}

// decodeBC7 decodes one BC7 block in any of its eight modes. A block in the reserved mode decodes to zeros.
func decodeBC7(block []byte, pixels *[16][4]uint8) {
	mode := bits.TrailingZeros8(block[0])
	if mode == 8 {
		*pixels = [16][4]uint8{}
		return
	}
	row := bc7Modes[mode]
	position := mode + 1
	partition := int(readBits(block, position, row.partitionBits))
	position += row.partitionBits
	rotation := int(readBits(block, position, row.rotationBits))
	position += row.rotationBits
	selection := readBits(block, position, row.selectionBits)
	position += row.selectionBits
	endpoints, position := readBC7Endpoints(block, position, mode)
	anchors := lookupAnchors(row.subsets, partition)
	primary, position := readIndices(block, position, row.indexBits, anchors)
	colorIndices, colorBits, alphaIndices, alphaBits := primary, row.indexBits, primary, row.indexBits
	if row.indexBits2 > 0 {
		secondary, _ := readIndices(block, position, row.indexBits2, anchors)
		alphaIndices, alphaBits = secondary, row.indexBits2
		if selection == 1 {
			colorIndices, colorBits, alphaIndices, alphaBits = secondary, row.indexBits2, primary, row.indexBits
		}
	}
	for i := range 16 {
		subset := lookupSubset(row.subsets, partition, i)
		first, second := endpoints[2*subset], endpoints[2*subset+1]
		for c := range 4 {
			weight := weights[colorBits][colorIndices[i]]
			if c == 3 {
				weight = weights[alphaBits][alphaIndices[i]]
			}
			pixels[i][c] = uint8((int(first[c])*(64-weight) + int(second[c])*weight + 32) >> 6)
		}
		if rotation > 0 {
			pixels[i][3], pixels[i][rotation-1] = pixels[i][rotation-1], pixels[i][3]
		}
	}
}

// readBC7Endpoints reads the endpoints of a BC7 block at the given bit position: each subset's two colors with
// their alpha, the P-bits applied and the bits replicated to eight. A mode without alpha gets 255. It returns the
// endpoints, by endpoint and then by channel, and the bit position after them.
func readBC7Endpoints(block []byte, position, mode int) ([6][4]uint8, int) {
	row := bc7Modes[mode]
	count := 2 * row.subsets
	widths := [4]int{row.colorBits, row.colorBits, row.colorBits, row.alphaBits}
	var values [6][4]int
	for c, width := range widths {
		for e := range count {
			values[e][c] = int(readBits(block, position, width))
			position += width
		}
	}
	pBits := [6]int{-1, -1, -1, -1, -1, -1}
	for e := range count {
		if row.endpointPBits == 1 {
			pBits[e] = int(readBits(block, position+e, 1))
		}
		if row.sharedPBits == 1 {
			pBits[e] = int(readBits(block, position+e/2, 1))
		}
	}
	position += row.endpointPBits*count + row.sharedPBits*row.subsets
	var endpoints [6][4]uint8
	for e := range count {
		for c, width := range widths {
			if width == 0 {
				endpoints[e][c] = 255
				continue
			}
			value := values[e][c]
			if pBits[e] >= 0 {
				width, value = width+1, value<<1|pBits[e]
			}
			endpoints[e][c] = uint8(value<<(8-width) | value>>(2*width-8))
		}
	}
	return endpoints, position
}

// parseBC6HFields parses the field strings of every BC6H mode, such as "r0[9:0]" or "g2[4]", into the endpoint
// and channel each fills and its high and low bit, equal for one bit and swapped when the field is stored
// reversed. The mode field gets endpoint -1; a reserved mode gets no fields.
func parseBC6HFields() [32][][4]int {
	var modes [32][][4]int
	for mode, row := range bc6hModes {
		if row.fields == "" {
			continue
		}
		for field := range strings.SplitSeq(row.fields, ",") {
			variable, bounds, _ := strings.Cut(strings.TrimSuffix(field, "]"), "[")
			first, second, ok := strings.Cut(bounds, ":")
			if !ok {
				second = first
			}
			high, _ := strconv.Atoi(first)
			low, _ := strconv.Atoi(second)
			parsed := [4]int{-1, 0, high, low}
			if variable != "m" {
				parsed[0], parsed[1] = int(variable[1]-'0'), strings.IndexByte("rgb", variable[0])
			}
			modes[mode] = append(modes[mode], parsed)
		}
	}
	return modes
}
