// dds.go saves the pictures of a mod as the DDS files the game reads.

package draw

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/samber/lo"

	"paradox-modding-tools/internal/files"
	"paradox-modding-tools/internal/game"
	"paradox-modding-tools/internal/index"
	"paradox-modding-tools/internal/texture"
	"paradox-modding-tools/services/lsp"
	"paradox-modding-tools/services/workspace"
)

// A PictureFiles is the pictures Save as DDS saves, with the DDS whose settings each keeps, by the picture's path.
type PictureFiles struct {
	Pictures   []workspace.Place `json:"pictures"`
	References map[string]DDS    `json:"references"`
}

// A DDS is one DDS file whose settings a picture keeps: where it is and what its header says.
type DDS struct {
	Place  workspace.Place `json:"place"`
	Header texture.Header  `json:"header"`
}

// FindPictureFiles returns the pictures Save as DDS saves for the given path: the picture itself, or for a folder the
// images directly in it that are not DDS files. Each keeps the settings of the DDS beside it, else of the game's own
// DDS at the place its DDS will have. A picture with neither has no reference. It returns an error if no workspace is
// open or a folder is outside it or cannot be read.
func (drw *Service) FindPictureFiles(path string) (PictureFiles, error) {
	ws, err := drw.Workspace.Session()
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
	isGame := func(place workspace.Place) bool { return place.Origin == game.OriginVanilla }
	references := map[string]DDS{}
	for _, picture := range pictures {
		target := toDDSPath(picture.Path)
		place, ok := ws.Locate(mods, target), files.IsFile(target)
		if !ok {
			place, ok = lo.Find(drw.Workspace.ListCopies(target), isGame)
		}
		if !ok {
			continue
		}
		if header, err := drw.Workspace.ReadHeader(place.Path); err == nil {
			references[picture.Path] = DDS{Place: place, Header: header}
		}
	}
	return PictureFiles{Pictures: pictures, References: references}, nil
}

// WriteDDS writes each picture beside itself as a DDS, compressed in BC3 blocks or not, with or without mip levels,
// replacing a DDS of that name. It returns the files written, and on an error the ones written before it.
func (drw *Service) WriteDDS(paths []string, compressed, mipmaps bool) ([]string, error) {
	ddsPaths := []string{}
	for _, path := range paths {
		dds, err := encodePicture(path, compressed, mipmaps)
		if err == nil {
			err = drw.Workspace.WriteBytes(toDDSPath(path), dds)
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

// listPictureFiles returns the images directly in a folder that are not DDS files. It returns an error if the folder is
// outside the workspace or cannot be read.
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

// encodePicture reads a picture file and encodes it as a DDS. It returns an error if the file cannot be read, decoded
// or encoded.
func encodePicture(path string, compressed, mipmaps bool) ([]byte, error) {
	file, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	picture, err := texture.Decode(file, 0)
	if err != nil {
		return nil, err
	}
	return texture.EncodeDDS(picture, compressed, mipmaps)
}
