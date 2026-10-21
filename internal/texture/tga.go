package texture

import "image"

// decodeTGA decodes a TGA to RGBA. It handles raw and run-length encoded data from 8 to 32 bits per pixel.
func decodeTGA(bytes []byte) (image.Image, error) { panic("stub") }

// decodeTGAHeader reads a TGA header. A TGA has no magic bytes, so it is recognized by a consistent header and by
// its footer when it has one. It returns false if the bytes are not a TGA.
func decodeTGAHeader(bytes []byte) (Header, bool) { panic("stub") }
