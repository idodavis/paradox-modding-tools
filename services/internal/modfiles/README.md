# modfiles

A mod's own Paradox files: the descriptor its game wants (`descriptor.mod`, or `.metadata/metadata.json`), the thumbnail
where that game keeps it, and the skeleton written for a new mod.

| file | holds |
|---|---|
| `modfiles.go` | `LoadDescriptor` and `WriteDescriptor`; `Thumbnail` and `CopyThumbnail`; `Create`, `DefaultModParent`; `ListingFields`, the fields both descriptor kinds share |

## Decisions

- **Which descriptor and which thumbnail folder a game uses are rows in `game`.** This package reads them; it does not
  tell the games apart.
- **A `descriptor.mod` is script,** so it is read with `jomini`. A write edits the text by range, so a comment or a line
  the app does not know stays as written.
- **What only the Steam Workshop reads is `steam`'s,** apart from a metadata.json mod's Workshop id, which the
  descriptor has no key for and this package keeps in `workshop/steam-item.txt`.
