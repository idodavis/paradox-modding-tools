package texture

import "image"

// decodeDDS decodes the first mipmap of a DDS to RGBA. It handles BC1 to BC7 and uncompressed data.
func decodeDDS(bytes []byte) (image.Image, error) { panic("stub") }

// decodeDDSHeader reads a DDS header, the DX10 extension included.
func decodeDDSHeader(bytes []byte) (Header, bool) { panic("stub") }
