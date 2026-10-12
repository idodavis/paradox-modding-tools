# texture

Decodes the games' images that a browser cannot draw, and reads an image's header without decoding it.

| file | holds |
|---|---|
| `decode.go` | `Decode`: DDS (BC1, BC2, BC3, BC7, and an uncompressed surface by its channel masks) and TGA (true-color or grey, raw or run-length) |
| `info.go` | `Info`: an image file's size and format from its header, as `Facts` |

## Decisions

- **Pure Go.** Windows builds run with `CGO_ENABLED=0`, so no decoder links a C library.
- **The header comes first.** `Info` answers what a hover or the explorer needs (size, format) without
  decoding a pixel.
- **A large surface is bounded** by `MaxSide`: a DDS bigger than it is decoded at the biggest mip level that fits.
