# texture

Decodes the games' images that a browser cannot draw, reads an image's header without decoding it, and resolves a path
written in script, GUI or asset text to the image it names.

| file | holds |
|---|---|
| `info.go` | `Info`: an image file's size and format from its header, as `Facts`; the one header read, which `Decode` takes its layout from |
| `decode.go` | `Decode`: DDS (BC1, BC2, BC3, BC7, and an uncompressed surface by its channel masks) and TGA (true-color or grey, raw or run-length) |
| `resolve.go` | `Resolve`: the file a written path names, the extension variants tried in the game's order |

## Decisions

- **Pure Go.** Windows builds run with `CGO_ENABLED=0`, so no decoder links a C library.
- **The header is read once.** `Info` answers what a hover or the explorer needs (size, format) without decoding a
  pixel; `Decode` reads the same header through the same function, so a DDS's or a TGA's fields are written once.
  A file is what its bytes say, not what its name says: the games ship a PNG named `.dds`. `Facts.NeedsDecoding`
  tells a DDS or a TGA from what a browser draws, and `Facts.Label` names the format for the image panel.
- **A large surface is bounded** by `MaxSide`: a DDS bigger than it is decoded at the biggest mip level that fits.
- **A written path has one resolver.** `Resolve` asks its caller's lookup (the index's, which reads a rel in any case)
  for the written rel cleaned, then for the same rel under `.dds`, `.png` and `.tga`; the caller applies any icon
  folder around it.
