// dds.go saves a mod's pictures as the DDS files the game reads, and the PNGs
// the page makes.

package draw

import (
	"bytes"
	"cmp"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/texture"
	"paradox-modding-tools/services/lsp"
	"paradox-modding-tools/services/workspace"
)

// PictureFiles is the pictures Save as DDS saves, with what it offers for
// them.
type PictureFiles struct {
	Pictures []workspace.Place `json:"pictures"`

	// Names holds the file name each picture's DDS gets, in the order of
	// Pictures.
	Names []string `json:"names"`

	// References holds, by picture path, the DDS whose settings the picture
	// keeps.
	References map[string]DDS `json:"references"`

	// Formats are the formats offered, then the format of each replaced DDS
	// that is not among them.
	Formats []DDSFormat `json:"formats"`

	// Format and Mipmaps are the settings to start with: those of the DDS
	// files the pictures replace where every picture replaces one and they
	// agree, else the first format without mip levels. Matched is how many
	// DDS files they came from, 0 for none.
	Format  uint32 `json:"format"`
	Mipmaps bool   `json:"mipmaps"`
	Matched int    `json:"matched"`
}

// DDS is a DDS file whose settings a picture keeps.
type DDS struct {
	Place  workspace.Place `json:"place"`
	Header texture.Header  `json:"header"`
}

// DDSFormat is one pixel format a picture can be saved in.
type DDSFormat struct {
	DXGI        uint32 `json:"dxgi"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

// ddsFormats are the formats Save as DDS offers, the default first.
var ddsFormats = []DDSFormat{
	{DXGI: 87, Label: "B8G8R8A8", Description: "Uncompressed and exact. Four bytes a pixel."},
	{DXGI: 71, Label: "BC1", Description: "An eighth of the size. Each pixel is opaque or clear."},
	{DXGI: 77, Label: "BC3", Description: "A quarter of the size, with full alpha."},
	{DXGI: 98, Label: "BC7", Description: "A quarter of the size, and much closer to the picture than BC3."},
}

// FindPictureFiles returns the pictures Save as DDS saves for a path: the
// picture itself, or a folder's images that are not DDS files. Each keeps the
// settings of the DDS beside it, else of the game's own DDS where its DDS
// will be. It returns an error if no workspace is open or a folder is outside
// it or cannot be read.
func (s *Service) FindPictureFiles(path string) (PictureFiles, error) {
	ws, err := s.Workspace.Session()
	if err != nil {
		return PictureFiles{}, err
	}

	mods := ws.Mods.Load()
	pictures := []workspace.Place{ws.Locate(mods, path)}
	if files.IsFolder(path) {
		if pictures, err = listPictureFiles(ws, mods, path); err != nil {
			return PictureFiles{}, err
		}
	}

	pictureFiles := PictureFiles{
		Pictures: pictures, Names: make([]string, len(pictures)), References: map[string]DDS{},
		Formats: slices.Clone(ddsFormats), Format: ddsFormats[0].DXGI,
	}
	for position, picture := range pictures {
		target := toDDSPath(picture.Path)
		pictureFiles.Names[position] = filepath.Base(target)
		place, ok := ws.Locate(mods, target), files.IsFile(target)
		if !ok {
			copies := s.Workspace.ListCopies(target)
			original := slices.IndexFunc(copies, func(place workspace.Place) bool { return place.Origin == files.OriginVanilla })
			if original < 0 {
				continue
			}

			place = copies[original]
		}

		if header, err := s.Workspace.ReadHeader(place.Path); err == nil {
			pictureFiles.References[picture.Path] = DDS{Place: place, Header: header}
		}
	}

	matchSettings(&pictureFiles)

	return pictureFiles, nil
}

// matchSettings offers the format of each replaced DDS that is not offered
// yet, and starts from the replaced files' settings where every picture
// replaces one and they agree. A DDS that names no format counts as the
// first format.
func matchSettings(pictureFiles *PictureFiles) {
	format, mipmaps, agreed, count := ddsFormats[0].DXGI, false, true, 0
	for _, picture := range pictureFiles.Pictures {
		reference, ok := pictureFiles.References[picture.Path]
		if !ok {
			agreed = false

			continue
		}

		header := reference.Header
		isListed := slices.ContainsFunc(pictureFiles.Formats, func(row DDSFormat) bool { return row.DXGI == header.DXGI })
		if header.DXGI != 0 && !isListed {
			pictureFiles.Formats = append(pictureFiles.Formats, DDSFormat{
				DXGI: header.DXGI, Label: header.PixelFormat, Description: "The format of the DDS it replaces.",
			})
		}

		own, ownMipmaps := cmp.Or(header.DXGI, ddsFormats[0].DXGI), header.Mipmaps > 1
		if count == 0 {
			format, mipmaps = own, ownMipmaps
		}

		agreed = agreed && own == format && ownMipmaps == mipmaps
		count++
	}

	if agreed && count > 0 {
		pictureFiles.Format, pictureFiles.Mipmaps, pictureFiles.Matched = format, mipmaps, count
	}
}

// listPictureFiles returns the images directly in a folder that are not DDS
// files. It returns an error if the folder is outside the workspace or
// cannot be read.
func listPictureFiles(ws *workspace.Session, mods *index.Index, folder string) ([]workspace.Place, error) {
	if _, ok := mods.FindRoot(folder); !ok {
		return nil, fmt.Errorf("%s is outside the workspace", folder)
	}

	entries, err := os.ReadDir(folder)
	if err != nil {
		return nil, err
	}

	pictures := []workspace.Place{}
	for _, entry := range entries {
		if !entry.IsDir() && texture.IsImage(entry.Name()) && !strings.EqualFold(filepath.Ext(entry.Name()), ".dds") {
			pictures = append(pictures, ws.Locate(mods, filepath.Join(folder, entry.Name())))
		}
	}

	return pictures, nil
}

// toDDSPath returns the path of the DDS a picture is saved as.
func toDDSPath(path string) string {
	return strings.TrimSuffix(path, filepath.Ext(path)) + ".dds"
}

// WriteDDS writes each picture beside itself as a DDS in a DXGI format, such
// as 98 for BC7_UNORM, replacing a DDS of that name. It returns the files
// written, and on an error those written before it.
func (s *Service) WriteDDS(paths []string, dxgi uint32, mipmaps bool) ([]string, error) {
	ddsPaths := []string{}

	for _, path := range paths {
		dds, err := encodePicture(path, dxgi, mipmaps)
		if err == nil {
			err = s.Workspace.WriteBytes(toDDSPath(path), dds)
		}

		if err != nil {
			failure := fmt.Errorf("%s was not saved: %w", filepath.Base(path), err)
			if len(ddsPaths) > 0 {
				failure = fmt.Errorf("%w (%s saved before it)", failure, lsp.Plural(len(ddsPaths), "picture", "pictures"))
			}

			return ddsPaths, failure
		}

		ddsPaths = append(ddsPaths, toDDSPath(path))
	}

	return ddsPaths, nil
}

// encodePicture reads a picture file and encodes it as a DDS in a DXGI
// format.
func encodePicture(path string, dxgi uint32, mipmaps bool) ([]byte, error) {
	file, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	picture, err := texture.Decode(file, 0)
	if err != nil {
		return nil, err
	}

	return texture.EncodeDDS(picture, dxgi, mipmaps)
}

// ExportPNG writes a PNG the page made to a file. It returns an error if the
// data is not a PNG or the file cannot be written.
func (s *Service) ExportPNG(target string, data []byte) error {
	if _, err := png.DecodeConfig(bytes.NewReader(data)); err != nil {
		return fmt.Errorf("the picture is not a PNG: %w", err)
	}

	return os.WriteFile(target, data, 0o644)
}
