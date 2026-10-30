# texture

The package decodes DDS, TGA and PNG images to pixels, writes pictures as DDS files, and reads an image's header
without decoding it. It also finds the image file that a path written in script names.

No maintained Go decoder reads the DDS and TGA formats the games ship, so the decoders are this package's own, and so
is the encoder. PNG is the standard library's. The package knows no workspace: the lookup of a written path is a
function its caller passes.

| file | holds |
|---|---|
| `texture.go` | `Header`, `ReadHeader`, `Decode`; the resolver of a written path, `Resolve`, and `IsImage` |
| `dds.go` | the DDS container, legacy and DX10: its header's offsets and flags, its pixel formats, its mip levels; the BC1 to BC5 decoders and the uncompressed formats by their masks |
| `encode.go` | `EncodeDDS`: the header, the mip levels made in linear light, uncompressed pixels and BC3 blocks |
| `bptc.go` | the BC6H and BC7 decoders, with their modes, partitions and anchors as tables |
| `tga.go` | TGA, raw and run-length encoded, from 8 to 32 bits a pixel, with its palette |

## Headers and pixels

A `Header` is what an image file says about itself before its pixels: its format, its size, its mip levels and its
block compression. `ReadHeader` reads only the start of a file, and adds the file's size in bytes.

`Decode` takes an image's bytes and one mip level: 0 is the whole image, and each level after it is half the size of
the one before. The bytes say which format it is. A DDS opens with its magic, a PNG with its own, and anything else is
read as a TGA. Only a DDS has more than one level. `Decode` refuses an image past Direct3D's limit on a side, a level
the image does not have, an image whose data ends before its last pixel, and a DDS pixel format it does not decode,
which the error names.

## Writing a DDS

`EncodeDDS` writes a picture as a DDS: uncompressed B8G8R8A8, which is exact, or BC3, a quarter of the size. The games
read either, and they mix the two in one folder, so the choice decides a picture's size and sharpness, never whether
it shows. With mipmaps, every level down to one pixel follows the picture. Each level is half the one before, a pixel
the average of the four it covers, its colors mixed in linear light and its alpha as it is.

A BC3 block is an alpha block, its highest and lowest alpha and each pixel's nearest value between them, then a color
block in BC1's four-color mode. The color block starts from the block's two pixels farthest apart, takes each pixel's
nearest palette color, and fits the two colors to those once by least squares. Both palettes are the decoder's own, so
a block decodes to the colors it was fitted to. PMT writes no BC1 and no BC7.

## Resolving a written path

`Resolve` finds the file that a path written in script names. It tries the path as written, then with each other image
extension the game accepts, `.dds`, `.png` and `.tga` in that order, in place of the written one. A leading slash is
ignored. The lookup function the caller passes says whether a relative path exists in the workspace and where, so the
game's own rule for which file is read stays `index`'s, in `index.ResolveTexture`. `IsImage` reports whether a written
path ends in one of those extensions.

## Terms

- **Header**: what an image file says about itself before its pixels.
- **Mip level**: one of a DDS's smaller copies of its image, each half the size of the one before.
- **Compression**: the block compression of a DDS, BC1 to BC7, or none.
