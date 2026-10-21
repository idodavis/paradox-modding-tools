// Package texture decodes DDS and TGA images to pixels, reads an image's header without decoding it, and finds
// the image file that a path written in script names.
package texture

import "image"

// A Header is what an image file says about itself before its pixels.
type Header struct {
	Format      string `json:"format"` // dds, tga, png
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	Mipmaps     int    `json:"mipmaps"`
	Compression string `json:"compression"` // BC1 to BC7, or "" for uncompressed
}

// ReadHeader reads an image file's header without decoding its pixels.
func ReadHeader(path string) (Header, error) { panic("stub") }

// DecodeHeader reads the header from the first bytes of an image. It returns false if the bytes are not an image
// format PMT knows.
func DecodeHeader(bytes []byte) (Header, bool) { panic("stub") }

// Decode decodes an image's bytes to pixels. The format is read from the bytes: DDS, TGA or PNG.
func Decode(bytes []byte) (image.Image, error) { panic("stub") }

// Resolve finds the file that a path written in script names. It tries the path as written, then with each image
// extension the game accepts in place of the written one. The lookup function says whether a relative path exists
// in the workspace and where.
func Resolve(written string, lookup func(rel string) (path string, ok bool)) (path string, ok bool) {
	panic("stub")
}
